#!/usr/bin/python
"""Observe one physical machine through its management controller.

This is how a physical target is proved before anything it holds is erased: the
exact ComputerSystem is read for its identity, its complete hardware inventory
is collected, and its power state is reported. Nothing is changed.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_system_inspect
short_description: Read one machine's identity, hardware and power state
version_added: "0.1.0"
description:
  - Reports the identity the controller gives this system, every hardware
    address it reports, the image its virtual media presents, and its power
    state.
  - An interface collection that cannot be read in full reports no addresses
    and names why, because a partial inventory proves nothing about which
    machine this is.
  - Performs no change and is safe to repeat.
options:
  endpoint:
    description: The exact ComputerSystem resource this machine is managed through.
    type: str
    required: true
  user:
    description: The account the controller answers.
    type: str
    required: true
  password:
    description: That account's password.
    type: str
    required: true
  verify:
    description: Whether the controller's own transport is verified.
    type: bool
    default: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Prove the machine before inserting anything
  bootwright.core.redfish_system_inspect:
    endpoint: '{{ request.controller.endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
"""

RETURN = r"""
observation:
  description: The identity, addresses, media and power state the controller reported.
  returned: always
  type: dict
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control


def main():
    module = AnsibleModule(
        argument_spec={
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "verify": {"type": "bool", "default": True},
        },
        supports_check_mode=True,
    )
    client = redfish_control.Client(
        module.params["endpoint"], module.params["user"], module.params["password"],
        verify=bool(module.params["verify"]),
    )
    identity = client.identity()
    addresses, failures = client.hardware_addresses()
    module.exit_json(changed=False, observation={
        "addresses": addresses,
        "failures": failures,
        "manufacturer": identity["Manufacturer"],
        "media": client.inserted(),
        "model": identity["Model"],
        "power": client.power_state(),
        "serial": identity["SerialNumber"],
        "uuid": identity["UUID"],
    })


if __name__ == "__main__":
    main()
