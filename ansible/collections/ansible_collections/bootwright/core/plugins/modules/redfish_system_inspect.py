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
    address it reports, and its power state.
  - A system that cannot be read reports an empty identity and power state,
    which prove nothing.
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
  description: The identity, addresses and power state the controller reported.
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
    module.exit_json(changed=False, observation=observe(client))


def observe(client):
    """What the controller reports about this machine, without changing it.

    The system is read once. One that cannot be read leaves the identity and
    the power state empty, which proves nothing, and the interface inventory
    then reports its own failures. Virtual media is not looked for: the proof
    does not need it, and an inspection makes no request it does not need.
    """
    try:
        identity, power = client.identity(), client.power_state()
    except redfish_control.ControllerError:
        identity, power = dict.fromkeys(("UUID", "SerialNumber", "Manufacturer", "Model"), ""), ""
    addresses, failures = client.hardware_addresses()
    return {
        "addresses": addresses,
        "failures": failures,
        "manufacturer": identity["Manufacturer"],
        "model": identity["Model"],
        "power": power,
        "serial": identity["SerialNumber"],
        "uuid": identity["UUID"],
    }


if __name__ == "__main__":
    main()
