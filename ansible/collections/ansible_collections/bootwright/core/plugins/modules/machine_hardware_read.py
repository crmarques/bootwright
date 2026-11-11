#!/usr/bin/python
"""Read the permanent hardware addresses of this host's network interfaces without changing them."""
from __future__ import annotations

DOCUMENTATION = r"""
module: machine_hardware_read
short_description: Read the permanent hardware addresses one host reports
version_added: "0.1.0"
description:
  - Lists every network interface the host's kernel names and reports the
    address each device-backed Ethernet interface carries in hardware, by
    the permanent address its driver reports, or else by its own address when
    the kernel marks that address permanent.
  - A bond, VLAN, bridge, veth, tun or other software interface, a virtual
    function and an address that was randomized or set count for nothing.
  - Changes nothing.
options: {}
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read the permanent hardware addresses this host reports
  bootwright.core.machine_hardware_read:
"""
RETURN = r"""
addresses:
  description: The permanent hardware addresses, colon-separated and lower case, sorted and once each.
  returned: success
  type: list
  elements: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.machine_hardware import Unreadable, read


def main():
    module = AnsibleModule(argument_spec={}, supports_check_mode=True)
    try:
        found = read()
    except Unreadable as failure:
        module.fail_json(msg=str(failure))
        return
    module.exit_json(changed=False, addresses=found)


if __name__ == "__main__":
    main()
