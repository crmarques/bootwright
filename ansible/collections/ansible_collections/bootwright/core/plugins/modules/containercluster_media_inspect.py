#!/usr/bin/python
"""Observe one cluster's published boot image without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_media_inspect
short_description: Observe the boot image one container cluster publishes
version_added: "0.1.0"
description:
  - Reports whether the agent boot image is published beneath the served root,
    whether the installer's work area exists, and what the receipt says the
    published image was built from.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen boot-media request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the published boot image
  bootwright.core.containercluster_media_inspect:
    request: '{{ bootwright_cluster_media_request }}'
"""

RETURN = r"""
observation:
  description: What is published, and what the receipt says it was built from.
  returned: always
  type: dict
"""

import json
import os

from ansible.module_utils.basic import AnsibleModule

# IMAGE is the name the attempt publishes the image under, inside the
# unguessable directory it minted. RECEIPT is what that attempt recorded about
# the inputs it built from; it lives in the work area rather than beneath the
# served root, because it is this block's own evidence and not content.
IMAGE = "agent.iso"
RECEIPT = "bootwright-media.json"
MAX_RECEIPT = 4096


def published(root):
    """The one directory this block published in, or nothing.

    The directory name is the unguessable segment the attempt minted, so it is
    read here and never returned: a caller learns that an image is published,
    never where.
    """
    try:
        entries = sorted(os.listdir(root))
    except OSError:
        return None
    for entry in entries:
        candidate = os.path.join(root, entry, IMAGE)
        if os.path.isfile(candidate):
            return candidate
    return None


def receipt(path):
    """What the attempt that published recorded about its own inputs."""
    try:
        if os.path.getsize(path) > MAX_RECEIPT:
            return {}
        with open(path, "rb") as handle:
            recorded = json.loads(handle.read().decode("utf-8"))
    except (OSError, ValueError, UnicodeDecodeError):
        return {}
    if not isinstance(recorded, dict):
        return {}
    return recorded


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    request = module.params["request"]
    work = request["workRoot"]
    recorded = receipt(os.path.join(work, RECEIPT))
    observation = {
        "image": published(request["image"]["path"]) is not None,
        "inputs": str(recorded.get("inputs") or ""),
        "installer": str(recorded.get("installer") or ""),
        "work": os.path.isdir(work),
    }
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
