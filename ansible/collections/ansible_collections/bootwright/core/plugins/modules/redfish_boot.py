#!/usr/bin/python
"""Read or drive one machine's power and virtual media through its controller.

Every power operation goes through the management controller, never through the
hypervisor, so a consumer takes the same path to a virtual and a physical
server. A request is not evidence: each operation polls the resource to its
expected state within a bounded window.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_boot
short_description: Read or drive one machine through its management controller
version_added: "0.1.0"
description:
  - Reads the power state and inserted media, inserts or ejects virtual media,
    sets a one-time boot device, asks the operating system to shut down, and
    powers a machine on or off.
  - The read operation performs no change and is safe to repeat.
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
  operation:
    description: What to read or drive.
    type: str
    required: true
    choices: [read, insert, eject, boot, power-on, power-off, shutdown]
  image:
    description: The media URL to insert.
    type: str
    required: false
  target:
    description: The one-time boot device a boot operation selects.
    type: str
    default: Cd
    choices: [Cd, Hdd]
  attempts:
    description: Bounded polls before the outcome is unproved.
    type: int
    default: 60
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Read the machine's power state
  bootwright.core.redfish_boot:
    endpoint: '{{ endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
    operation: read
"""

RETURN = r"""
power:
  description: The reported power state, or the empty string when unproved.
  returned: always
  type: str
media:
  description: The image the controller presents, or the empty string.
  returned: always
  type: str
"""

import urllib.error

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

MAX_ATTEMPTS = 600


def main():
    module = AnsibleModule(
        argument_spec={
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "operation": {
                "type": "str", "required": True,
                "choices": ["read", "insert", "eject", "boot", "power-on", "power-off", "shutdown"],
            },
            "image": {"type": "str", "required": False},
            "target": {"type": "str", "default": "Cd", "choices": ["Cd", "Hdd"]},
            "attempts": {"type": "int", "default": 60},
        },
        supports_check_mode=False,
    )
    endpoint = module.params["endpoint"]
    user, password = module.params["user"], module.params["password"]
    operation = module.params["operation"]
    attempts = max(1, min(int(module.params["attempts"]), MAX_ATTEMPTS))
    try:
        changed = drive(module, endpoint, user, password, operation, attempts)
        module.exit_json(
            changed=changed,
            power=redfish_control.power_state(endpoint, user, password),
            media=redfish_control.media_inserted(endpoint, user, password),
        )
    except (urllib.error.URLError, OSError, ValueError) as failure:
        module.fail_json(msg="the management controller did not complete %s: %s" % (operation, type(failure).__name__))


def drive(module, endpoint, user, password, operation, attempts):
    """Perform exactly the one operation asked for, and prove its outcome."""
    if operation == "read":
        return False
    if operation == "insert":
        image = module.params["image"]
        if not image:
            raise ValueError("image")
        if redfish_control.media_inserted(endpoint, user, password) == image:
            return False
        redfish_control.insert_media(endpoint, user, password, image)
        return True
    if operation == "eject":
        if not redfish_control.media_inserted(endpoint, user, password):
            return False
        redfish_control.eject_media(endpoint, user, password)
        return True
    if operation == "boot":
        redfish_control.boot_once(endpoint, user, password, module.params["target"])
        return True
    expected = "On" if operation == "power-on" else "Off"
    if redfish_control.power_state(endpoint, user, password) == expected:
        return False
    # A graceful request asks the operating system to stop; forcing the power
    # off does not. Both are polled to the state they asked for, so neither is
    # reported as settled before the controller says it is.
    kind = {"power-on": "On", "power-off": "ForceOff", "shutdown": "GracefulShutdown"}[operation]
    redfish_control.reset(endpoint, user, password, kind)
    if not redfish_control.await_power(endpoint, user, password, expected, attempts):
        raise ValueError("power state")
    return True


if __name__ == "__main__":
    main()
