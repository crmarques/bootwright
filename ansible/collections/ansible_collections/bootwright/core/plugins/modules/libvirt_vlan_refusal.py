#!/usr/bin/python
# GNU General Public License v3.0+ (see https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage only one frozen provider's unreachable routes for the VLAN subnets its bridges carry."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_vlan_refusal
short_description: Hold or remove an owned set of unreachable routes
version_added: "0.1.0"
description:
  - Installs or removes the exact frozen provider's refusal unit, whose unreachable routes
    keep a guest's traffic for a VLAN subnet carried on a managed bridge from leaving through the host's uplink.
  - Refuses a route the host already holds inside or equal to a subnet, and unknown ownership of the unit.
options:
  request:
    description: The frozen provider host request.
    type: dict
    required: true
  state:
    description: Whether to hold or remove the owned routes.
    type: str
    choices:
      - present
      - absent
    default: present
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Refuse the VLAN subnets the managed bridges carry
  bootwright.core.libvirt_vlan_refusal:
    request: '{{ bootwright_substrate_host_request }}'
"""

RETURN = r"""
changed:
  description: Whether the owned unit or its routes changed.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_vlan_refusal import manage


def main():
    module = AnsibleModule(argument_spec={
        "request": {"type": "dict", "required": True},
        "state": {"type": "str", "choices": ["present", "absent"], "default": "present"},
    }, supports_check_mode=False)
    try:
        changed = manage(module.run_command, module.params["request"], module.params["state"])
    except (OSError, ValueError, KeyError, TypeError) as failure:
        module.fail_json(msg=str(failure) if isinstance(failure, ValueError) else "the owned VLAN refusal routes could not be changed safely")
        return
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
