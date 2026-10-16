#!/usr/bin/python
# GNU General Public License v3.0+ (see https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage only one frozen provider's firewall readiness barrier."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_firewall_ready
short_description: Manage an owned libvirt startup readiness barrier
version_added: "0.1.0"
description:
  - Installs or removes the exact frozen provider's readiness unit and enablement.
  - Refuses unknown ownership and requires an already enabled host firewall.
options:
  request:
    description: The frozen provider host request.
    type: dict
    required: true
  state:
    description: Whether to install or remove the owned barrier.
    type: str
    choices: [present, absent]
    default: present
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Install the provider readiness barrier
  bootwright.core.libvirt_firewall_ready:
    request: '{{ bootwright_substrate_host_request }}'
"""

RETURN = r"""
changed:
  description: Whether the owned readiness resources changed.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_firewall_ready import manage


def main():
    module = AnsibleModule(argument_spec={
        "request": {"type": "dict", "required": True},
        "state": {"type": "str", "choices": ["present", "absent"], "default": "present"},
    }, supports_check_mode=False)
    try:
        changed = manage(module.run_command, module.params["request"], module.params["state"])
    except (OSError, ValueError, KeyError) as failure:
        module.fail_json(msg=str(failure) if isinstance(failure, ValueError) else "the owned firewall readiness barrier could not be changed safely")
        return
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
