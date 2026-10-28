#!/usr/bin/python
"""Apply or prove one physical machine's declared firmware settings.

This is the firmware step of the bare-metal pre-boot proof: it runs only after
the exact target was proved off, and reports its outcome as data for the proof
to judge. Proving reads only; enforcing writes what differs, powers the machine
on into firmware setup to apply it, and powers it off again.

A SIGTERM received while enforcing is not delivered again once the step has
reported: the module reports its structured refusal through exit_json and the
process ends there, which is what the signal asked for. Re-raising the signal
before that report would replace the refusal with a killed module, and
fail_json would read to the role as a controller that did not answer.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_firmware
short_description: Apply or prove declared Secure Boot, boot mode and BIOS attributes
version_added: "0.1.0"
description:
  - Compares the declared settings with what the controller reports, by value
    and JSON type, following only the controller's own links, and with the
    value each linked settings object holds pending for a declared setting.
  - With I(firmware.mode=prove) it never writes, resets or powers anything and
    reports C(firmware-setting-mismatch) for any difference.
  - With I(firmware.mode=enforce) it refuses before any write when its own
    read of the system does not report C(PowerState) C(Off), a resource or
    settings object a write needs is unpublished, an advertised value list
    excludes a declared value, or a reset would apply a pending change the
    declaration does not name. It then writes a proved one-time BiosSetup
    boot, the boot mode, Secure Boot and the BIOS settings object, proves the
    setup boot and power C(Off) on a fresh read, powers the machine on, reads
    until the settings apply within a bound, forces it off and polls it to
    C(Off) whatever happened, and reads every declared field once more.
    Any difference left is C(firmware-setting-not-applied).
  - Each poll stops at its share of the bound, and the machine is not powered
    on once the writes before it have spent theirs. A SIGTERM while enforcing
    ends the poll under way, still forces off a machine the step powered on,
    and reports C(firmware-setting-not-applied) that the step was asked to stop.
  - A read the controller does not answer before any write fails the module
    and names no refusal, as does a declaration this step does not accept.
    Once a write was sent, any failure, a refused certificate included, is
    C(firmware-setting-not-applied) instead.
  - A refusal reports declared field paths, fixed reasons and the controller's
    message identifiers, never a value the controller reported.
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
  ca_data:
    description:
      - PEM CA certificates that are the only anchors the controller's own
        transport is verified against.
      - Empty verifies against the system trust store. Refused beside I(verify=false).
    type: str
    default: ""
  firmware:
    description:
      - The frozen declaration, C(mode), C(secureBoot), C(bootMode) and
        C(biosAttributes), each attribute a C(name) with exactly one of
        C(string), C(integer) or C(boolean). Values are passed verbatim.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Apply or prove the declared firmware settings
  bootwright.core.redfish_firmware:
    endpoint: '{{ request.controller.endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
    firmware: '{{ request.firmware }}'
"""

RETURN = r"""
changed:
  description: Whether any write or reset was sent.
  returned: success
  type: bool
refusal:
  description: The refusal the outcome names, empty when every declared field reads as declared.
  returned: success
  type: str
fields:
  description: The declared field paths that do not read as declared.
  returned: success
  type: list
  elements: str
reasons:
  description: Why the settings were not proved or applied, in fixed text and the client's failure lines.
  returned: success
  type: list
  elements: str
messages:
  description: The controller's own message identifiers for the refused or failed settings.
  returned: success
  type: list
  elements: str
power:
  description: The power state the controller last reported.
  returned: success
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_firmware


def main():
    module = AnsibleModule(
        argument_spec={
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "verify": {"type": "bool", "default": True},
            "ca_data": {"type": "str", "default": ""},
            "firmware": {"type": "dict", "required": True},
        },
        supports_check_mode=False,
    )
    try:
        client = redfish_control.Client(
            module.params["endpoint"], module.params["user"], module.params["password"],
            verify=bool(module.params["verify"]), ca_data=module.params["ca_data"] or "",
        )
        outcome = drive(client, module.params["firmware"])
    except ValueError as refused:
        module.fail_json(msg="the firmware declaration was not accepted: %s" % refused)
    except redfish_control.ControllerError as failure:
        module.fail_json(msg="the firmware settings could not be read: %s" % failure)
    else:
        module.exit_json(**outcome)


def drive(client, firmware):
    """One firmware step on one controller: its outcome as data."""
    return redfish_firmware.converge(client, firmware)


if __name__ == "__main__":
    main()
