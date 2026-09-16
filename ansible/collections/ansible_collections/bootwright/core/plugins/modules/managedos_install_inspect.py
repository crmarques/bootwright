#!/usr/bin/python
"""Observe one installation's published content without changing any of it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_install_inspect
short_description: Observe the content one Bootwright installation publishes
version_added: "0.1.0"
description:
  - Reports whether the per-machine installer image, the private subtree and,
    when the profile uses one, the hosted package tree are published beneath
    the served root.
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
- name: Observe the published installer content
  bootwright.core.managedos_install_inspect:
    request: '{{ bootwright_os_install_request }}'
"""

RETURN = r"""
observation:
  description: Whether each published path exists.
  returned: always
  type: dict
"""

import os

from ansible.module_utils.basic import AnsibleModule

# TREE_MARKER is what makes a published tree complete: it is renamed into place
# last, so a fetching installer never sees a partial tree.
TREE_MARKER = ".treeinfo"


def main():
    module = AnsibleModule(
        argument_spec={"request": {"type": "dict", "required": True}},
        supports_check_mode=True,
    )
    request = module.params["request"]
    tree = request.get("tree")
    private = request.get("private")
    observation = {
        "image": os.path.isfile(request["image"]["path"]),
        # Material that only needed to exist for one boot must not outlive it,
        # so a subtree still present is unfinished work rather than a state.
        "private": bool(private) and os.path.isdir(private["path"]),
        "tree": bool(tree) and os.path.isfile(os.path.join(tree["path"], TREE_MARKER)),
    }
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
