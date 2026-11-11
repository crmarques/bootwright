#!/usr/bin/python
# GNU General Public License v3.0+ (see https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage only one frozen provider's owned nftables routing table, its ruleset file and the unit that holds it."""

from __future__ import annotations

DOCUMENTATION = r"""
module: libvirt_routing
short_description: Hold or remove an owned nftables routing table
version_added: "0.1.0"
description:
  - Installs or removes the exact frozen provider's routing unit and the owned ruleset file it loads, which routes the
    declared pairs of VLAN legs, translates the NAT legs to the uplink and refuses every other path into or out of a leg.
  - Refuses a foreign unit or ruleset file, a table of its name the owned running unit does not hold, and an unanswered
    read.
options:
  request:
    description: The frozen provider host request.
    type: dict
    required: true
  state:
    description: Whether to hold or remove the owned table.
    type: str
    choices:
      - present
      - absent
    default: present
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Hold the routing table of the VLAN legs
  bootwright.core.libvirt_routing:
    request: '{{ bootwright_substrate_host_request }}'
"""

RETURN = r"""
changed:
  description: Whether the owned unit, its ruleset file or its table changed.
  returned: always
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_routing import manage


def main():
    module = AnsibleModule(argument_spec={
        "request": {"type": "dict", "required": True},
        "state": {"type": "str", "choices": ["present", "absent"], "default": "present"},
    }, supports_check_mode=False)
    try:
        changed = manage(module.run_command, module.params["request"], module.params["state"])
    except (OSError, ValueError, KeyError, TypeError) as failure:
        module.fail_json(msg=str(failure) if isinstance(failure, ValueError) else "the owned routing table could not be changed safely")
        return
    module.exit_json(changed=changed)


if __name__ == "__main__":
    main()
