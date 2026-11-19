#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Acquire and publish one pinned artifact mirror file."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infrastructureservices_mirror_file
short_description: Fetch one pinned file into the artifact mirror
version_added: "0.1.0"
description:
  - Reuses a file at its pinned size and digest.
  - Otherwise fetches it over verified HTTPS through the frozen route and publishes it by rename.
requirements:
  - urllib3
options:
  source:
    description: The publisher URL, expected bytes and SHA-256.
    type: dict
    required: true
  egress:
    description: The frozen proxy endpoints and bypass list.
    type: dict
    required: true
    suboptions:
      httpProxy:
        description: The proxy for HTTP requests; empty or omitted when none.
        type: str
        default: ''
      httpsProxy:
        description: The proxy for HTTPS requests; empty or omitted for direct access.
        type: str
        default: ''
      noProxy:
        description: The destinations reached directly despite the proxy.
        type: list
        elements: str
        required: true
  dest:
    description: The absolute destination beneath the proved mirror directory.
    type: path
    required: true
  seconds:
    description: The time budget for acquisition.
    type: int
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Publish one file from its frozen source
  bootwright.core.infrastructureservices_mirror_file:
    source:
      id: '{{ item.sha256 }}'
      url: '{{ item.source }}'
      bytes: '{{ item.bytes }}'
      sha256: '{{ item.sha256 }}'
    egress: '{{ bootwright_mirror_files_request.egress }}'
    dest: '{{ infrastructureservices_mirror_fill_root }}/{{ item.path }}'
    seconds: 120
"""

RETURN = r"""
changed:
  description: Whether the file was published.
  returned: success
  type: bool
"""

import os
import stat
import tempfile

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import controller_files


def held(destination, source):
    try:
        descriptor = os.open(destination, controller_files.FILE_FLAGS)
    except FileNotFoundError:
        return False
    with os.fdopen(descriptor, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("the mirror destination is not a regular file")
        if (info.st_size, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) != (source["bytes"], 0, 0, 0o644):
            return False
        try:
            return controller_files.stream_copy(stream.read, source["bytes"]) == source["sha256"]
        except controller_files.Refused:
            return False


def publish(source, egress, destination, seconds):
    controller_files.source_identity(source)
    if not os.path.isabs(destination) or os.path.normpath(destination) != destination:
        raise ValueError("the mirror destination is not an absolute normal path")
    if held(destination, source):
        return False
    descriptor, stage = tempfile.mkstemp(prefix=".download-", dir=os.path.dirname(destination))
    try:
        with os.fdopen(descriptor, "wb") as target, controller_files.Routes() as routes:
            controller_files.download(source, egress, target.fileno(), seconds, routes)
            os.fchown(target.fileno(), 0, 0)
            os.fchmod(target.fileno(), 0o644)
            os.fsync(target.fileno())
        os.replace(stage, destination)
    finally:
        if os.path.lexists(stage):
            os.unlink(stage)
    return True


# Go omits an empty proxy endpoint, so each defaults to empty and proxy_for sees the complete route.
EGRESS_OPTIONS = {
    "httpProxy": {"type": "str", "default": ""},
    "httpsProxy": {"type": "str", "default": ""},
    "noProxy": {"type": "list", "elements": "str", "required": True},
}


def main():
    module = AnsibleModule(argument_spec={
        "source": {"type": "dict", "required": True},
        "egress": {"type": "dict", "required": True, "options": EGRESS_OPTIONS},
        "dest": {"type": "path", "required": True},
        "seconds": {"type": "int", "required": True},
    })
    params = module.params
    try:
        changed = publish(params["source"], params["egress"], params["dest"], params["seconds"])
    except controller_files.AcquisitionRefused as error:
        module.fail_json(msg="the publisher's checksum or size differs from its pin" if error.reason in ("integrity", "size")
                         else "the publisher could not be read through the declared route")
    except (OSError, ValueError):
        module.fail_json(msg="the mirror file could not be proved or published")
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
