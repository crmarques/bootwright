#!/usr/bin/python
"""Read or drive one machine's power and virtual media through its controller.

Every power operation goes through the management controller, never through the
hypervisor, so a consumer takes the same path to a virtual and a physical
server. A request is not evidence: each operation polls the resource to its
expected state within a bounded window, and an answer the controller does not
give readably fails the operation rather than reading as empty.

Each operation's worst case, from the bounds in redfish_control:

- insert: at most 1,880 s of pauses and attach timeouts, 3 x (MEDIA_TIMEOUT
  300 + TASK_POLLS 60 x 2 + MEDIA_PROBES 24 x 5) + 2 x (24 x 5 +
  INSERT_RETRY_DELAY 10), and at most 10,940 s when every request also times
  out, 3 x (300 + 60 x 32 + 24 x 35) + 2 x (30 + 24 x 35 + 10); plus one
  REQUEST_TIMEOUT for each discovery read and for each read before an attach
  or an eject.
- eject: at most 120 s of pauses, and 870 s when every request after discovery
  times out (the detach and 24 probes).
- boot: at most 60 s, and 510 s with timeouts (the system read, two PATCHes and
  12 read-backs).
- power-on, power-off and shutdown: at most attempts x 2 s, and attempts x 32 s
  with timeouts, plus three requests (the system read, an ActionInfo and the
  reset).
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
  verify:
    description: Whether the controller's own transport is verified.
    type: bool
    default: true
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
  description:
    - The image the virtual-media device last reported for read, insert and
      eject, or the empty string when it presents none or none is offered.
    - Always empty for boot and power operations, which never look for media.
  returned: always
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

MAX_ATTEMPTS = 600
# Each power operation's reset type and the state it must reach.
RESETS = {"power-on": ("On", "On"), "power-off": ("ForceOff", "Off"), "shutdown": ("GracefulShutdown", "Off")}


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
            "verify": {"type": "bool", "default": True},
        },
        supports_check_mode=False,
    )
    params = module.params
    operation = params["operation"]
    client = redfish_control.Client(params["endpoint"], params["user"], params["password"], verify=bool(params["verify"]))
    attempts = max(1, min(int(params["attempts"]), MAX_ATTEMPTS))
    try:
        changed, power, media = drive(client, operation, attempts, params["image"] or "", params["target"])
    except redfish_control.ControllerError as failure:
        module.fail_json(msg="the management controller did not complete %s: %s" % (operation, failure))
    else:
        module.exit_json(changed=changed, power=power, media=media)


def drive(client, operation, attempts, image="", target="Cd"):
    """Perform exactly the one operation asked for, prove it, and report it.

    Returns whether it changed anything, the power state the invocation's last
    system read reported, and the image the device last reported. Only read,
    insert and eject look for media; boot and power operations never do, so
    they report no image.
    """
    if operation == "insert" and not image:
        raise redfish_control.ControllerError("insert needs an image")
    if operation == "boot":
        client.boot_once(target)
        return True, client.last_power, ""
    if operation in RESETS:
        # A graceful request asks the operating system to stop; forcing the
        # power off does not. Both are polled to the state they asked for, so
        # neither is reported as settled before the controller says it is.
        kind, expected = RESETS[operation]
        return client.power(kind, expected, attempts), client.last_power, ""
    power = client.power_state()
    changed = False
    if operation == "insert":
        changed = client.insert(image)
    elif operation == "eject":
        changed = client.eject()
    else:
        client.inserted()
    return changed, power, client.last_image


if __name__ == "__main__":
    main()
