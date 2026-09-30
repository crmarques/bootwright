#!/usr/bin/python
"""Drive one machine's power, boot device and virtual media through its controller.

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
- the virtual-media trust an insert sets before its first attach, and what an
  eject settles once the device is proved empty: no pause, and at most
  CERTIFICATE_MEMBERS 8 + 5 requests, each under REQUEST_TIMEOUT, plus one
  retry of a PATCH answered 412. established makes none.
- boot: at most 60 s, and 510 s with timeouts (the system read, two PATCHes and
  12 read-backs).
- power-on, power-off and shutdown: at most attempts x 2 s, and attempts x 32 s
  with timeouts, plus three requests (the system read, an ActionInfo and the
  reset).
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_boot
short_description: Drive one machine through its management controller
version_added: "0.1.0"
description:
  - Inserts or ejects virtual media, sets a one-time boot device, asks the
    operating system to shut down, and powers a machine on or off.
  - Every operation may change the machine. A read goes through
    redfish_system_read, which cannot.
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
    description: What to drive.
    type: str
    required: true
    choices: [insert, eject, boot, power-on, power-off, shutdown]
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
  ca_data:
    description:
      - PEM CA certificates that are the only anchors the controller's own
        transport is verified against.
      - Empty verifies against the system trust store. Refused beside I(verify=false).
    type: str
    default: ""
  trust:
    description:
      - How an insert makes the controller trust the server it fetches the image
        from. Applies only to insert.
      - C(established) changes nothing on the controller.
      - C(import-certificate) adds the first certificate of I(certificate) to the
        device's certificate collection unless it is there and turns
        VerifyCertificate on, with no fallback to any other trust.
      - C(disable-verification) turns VerifyCertificate off when it reads on.
    type: str
    default: established
    choices: [established, import-certificate, disable-verification]
  certificate:
    description:
      - The artifact server's certificate as PEM. Only its first certificate is
        imported or removed.
      - Required by insert under C(import-certificate) and by eject with
        I(remove_certificate).
    type: str
    default: ""
  restore_verification:
    description:
      - Whether an eject turns the device's VerifyCertificate on once the device
        is proved empty, unless it already reads on. Applies only to eject.
    type: bool
    default: false
  remove_certificate:
    description:
      - Whether an eject deletes the device certificate equal to I(certificate)
        once the device is proved empty. Applies only to eject.
    type: bool
    default: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Eject the installer media
  bootwright.core.redfish_boot:
    endpoint: '{{ endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
    operation: eject
"""

RETURN = r"""
power:
  description: The reported power state, or the empty string when unproved.
  returned: always
  type: str
media:
  description:
    - The image the virtual-media device last reported for insert and eject,
      or the empty string when it presents none or none is offered.
    - Always empty for boot and power operations, which never look for media.
  returned: always
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

MAX_ATTEMPTS = 600
# Each power operation's reset type and the state it must reach.
RESETS = {"power-on": ("On", "On"), "power-off": ("ForceOff", "Off"), "shutdown": ("GracefulShutdown", "Off")}
# Every operation, each of which may change the machine.
DRIVES = ("insert", "eject", "boot") + tuple(RESETS)
TRUSTS = (redfish_control.TRUST_ESTABLISHED, redfish_control.TRUST_IMPORT, redfish_control.TRUST_DISABLED)


def main():
    module = AnsibleModule(
        argument_spec={
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "operation": {"type": "str", "required": True, "choices": list(DRIVES)},
            "image": {"type": "str", "required": False},
            "target": {"type": "str", "default": "Cd", "choices": ["Cd", "Hdd"]},
            "attempts": {"type": "int", "default": 60},
            "verify": {"type": "bool", "default": True},
            "ca_data": {"type": "str", "default": ""},
            "trust": {"type": "str", "default": redfish_control.TRUST_ESTABLISHED, "choices": list(TRUSTS)},
            "certificate": {"type": "str", "default": ""},
            "restore_verification": {"type": "bool", "default": False},
            "remove_certificate": {"type": "bool", "default": False},
        },
        supports_check_mode=False,
    )
    params = module.params
    operation = params["operation"]
    attempts = max(1, min(int(params["attempts"]), MAX_ATTEMPTS))
    try:
        client = redfish_control.Client(params["endpoint"], params["user"], params["password"],
                                        verify=bool(params["verify"]), ca_data=params["ca_data"] or "")
        changed, power, media = drive(
            client, operation, attempts, params["image"] or "", params["target"], trust=params["trust"],
            certificate=params["certificate"] or "", restore_verification=bool(params["restore_verification"]),
            remove_certificate=bool(params["remove_certificate"]))
    except redfish_control.ControllerError as failure:
        module.fail_json(msg="the management controller did not complete %s: %s" % (operation, failure))
    else:
        module.exit_json(changed=changed, power=power, media=media)


def drive(client, operation, attempts, image="", target="Cd", trust=redfish_control.TRUST_ESTABLISHED,
          certificate="", restore_verification=False, remove_certificate=False):
    """Perform exactly the one operation asked for, prove it, and report it.

    Returns whether it changed anything, the power state the invocation's last
    system read reported, and the image the device last reported. Only insert
    and eject look for media; boot and power operations never do, so they
    report no image. Anything else is refused before a request is made: this
    module drives, and a read goes through redfish_system_read. An insert
    carries the virtual-media trust and an eject what it settles; by default
    neither asks the controller for anything more.
    """
    if operation not in DRIVES:
        raise redfish_control.ControllerError("%s is not an operation this module drives" % operation)
    if operation == "insert" and not image:
        raise redfish_control.ControllerError("insert needs an image")
    if operation == "insert" and trust == redfish_control.TRUST_IMPORT and not certificate:
        raise redfish_control.ControllerError("insert under import-certificate needs the server's certificate")
    if operation == "eject" and remove_certificate and not certificate:
        raise redfish_control.ControllerError("an eject removing a certificate needs that certificate")
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
    if operation == "insert":
        changed = client.insert(image, trust, certificate)
    else:
        changed = client.eject(restore_verification, remove_certificate, certificate)
    return changed, power, client.last_image


if __name__ == "__main__":
    main()
