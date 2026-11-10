#!/usr/bin/python
# GNU General Public License v3.0+ (see https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage only one frozen provider's NetworkManager trunks and VLAN legs."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_nm_connections
short_description: Hold or remove the owned NetworkManager trunks and VLAN legs
version_added: "0.1.0"
description:
  - Publishes each frozen trunk bridge and VLAN leg as a keyfile NetworkManager loads, activates it, and proves it
    active with its frozen definition in the trusted zone; removes the legs, then the trunks, and proves them gone.
  - Refuses before any change when NetworkManager is not ready, a read does not answer, another profile holds a frozen
    name, UUID or device, a host link or route collides, or a domain runs on a trunk it would reactivate or remove.
options:
  request:
    description: The frozen provider host request.
    type: dict
    required: true
  state:
    description: Whether to hold or remove the owned connections.
    type: str
    choices:
      - present
      - absent
    default: present
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Hold the trunks and VLAN legs
  bootwright.core.libvirt_nm_connections:
    request: '{{ bootwright_substrate_host_request }}'
"""

RETURN = r"""
changed:
  description: Whether an owned connection changed.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_nm_connections import manage
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import host_conflicts, network_guests


def main():
    module = AnsibleModule(argument_spec={
        "request": {"type": "dict", "required": True},
        "state": {"type": "str", "choices": ["present", "absent"], "default": "present"},
    }, supports_check_mode=False)
    request = module.params["request"]
    run = module.run_command
    context = str((request.get("identity") or {}).get("context") or "")

    def guests(bridge):
        return network_guests(run, str(request.get("uri") or ""), "", bridge, context)

    def conflicts(networks, ours):
        return host_conflicts(run, networks, ours)

    try:
        changed = manage(run, request, module.params["state"], guests, conflicts)
    except (OSError, ValueError, KeyError, TypeError) as failure:
        module.fail_json(msg=str(failure) if isinstance(failure, ValueError) else "the owned trunks and VLAN legs could not be changed safely")
        return
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
