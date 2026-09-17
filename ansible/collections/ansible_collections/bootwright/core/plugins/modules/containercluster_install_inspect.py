#!/usr/bin/python
"""Observe what one cluster's installation recorded, without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_inspect
short_description: Observe one container cluster's own installation record
version_added: "0.1.0"
description:
  - Reports the cluster identity this operation's own installer recorded, and
    the address of the image its media block published.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen installation request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the installation record
  bootwright.core.containercluster_install_inspect:
    request: '{{ bootwright_cluster_install_request }}'
"""

RETURN = r"""
observation:
  description: The recorded cluster identity and the published image address.
  returned: always
  type: dict
"""

import json
import os

from ansible.module_utils.basic import AnsibleModule

# METADATA is what the installer records about the cluster it is building, and
# IMAGE the name the media block published the image under inside the
# unguessable directory that attempt minted.
METADATA = "metadata.json"
IMAGE = "agent.iso"
MAX_METADATA = 65536


def identity(path):
    """The cluster identity this operation's own installer recorded."""
    try:
        if os.path.getsize(path) > MAX_METADATA:
            return ""
        with open(path, "rb") as handle:
            recorded = json.loads(handle.read().decode("utf-8"))
    except (OSError, ValueError, UnicodeDecodeError):
        return ""
    if not isinstance(recorded, dict):
        return ""
    return str(recorded.get("clusterID") or "")


def published(root, base):
    """The address the image is published at, or nothing.

    The final segment is the unguessable one the publishing attempt minted, so
    this is the only place it is read, and every task that carries the result
    keeps it out of its own output.
    """
    try:
        entries = sorted(os.listdir(root))
    except OSError:
        return ""
    for entry in entries:
        if os.path.isfile(os.path.join(root, entry, IMAGE)):
            return base + "/" + entry + "/" + IMAGE
    return ""


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    request = module.params["request"]
    image = request["image"]
    observation = {
        "identity": identity(os.path.join(request["workRoot"], METADATA)),
        "kubeconfig": os.path.isfile(os.path.join(request["workRoot"], "auth", "kubeconfig")),
        "url": published(image["path"], image["url"]),
    }
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
