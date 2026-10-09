#!/usr/bin/python
"""Read one shared package tree without changing it."""
from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_tree_inspect
short_description: Read shared package tree publication evidence
version_added: "0.1.0"
description:
  - Reads the complete marker, source identity and partial publication paths.
options:
  request:
    description: The frozen shared package tree request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read the shared package tree
  bootwright.core.managedos_tree_inspect:
    request: '{{ bootwright_os_tree_request }}'
"""
RETURN = r"""
observation:
  description: Bounded publication presence and source identity.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.package_tree import observe


def main():
    module = AnsibleModule(argument_spec={"request": {"type": "dict", "required": True}}, supports_check_mode=True)
    request = module.params["request"]
    path = request["publication"]["path"]
    try:
        observation = observe(path, request["media"]["sha256"])
    except OSError:
        module.fail_json(msg="the package tree could not be inspected")
    module.exit_json(changed=False, observation=observation)


if __name__ == "__main__":
    main()
