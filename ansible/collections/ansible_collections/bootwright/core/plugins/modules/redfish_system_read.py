#!/usr/bin/python
"""Read one machine's power state, and the media it presents, through its controller.

Nothing is changed, so a read is safe to repeat and a poll may make as many as
its budget allows. It goes through the same client as every effect, and an
answer the controller does not give readably fails the read rather than reading
as empty.

Each read's worst case, from the bounds in redfish_control:

- power: one system GET under REQUEST_TIMEOUT 30 s. It never looks for media,
  so a media view that cannot be read cannot fail it.
- media: that GET plus the discovery GETs, each under the same timeout: the
  system's VirtualMedia view, each manager the system names and its view when
  that view was not read already, and each candidate device until one is
  optical. The pinned emulator answers in four GETs in all.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: redfish_system_read
short_description: Read one machine's power state and inserted media without changing it
version_added: "0.1.0"
description:
  - Reports the power state the management controller gives this system and,
    for a media read, the image its virtual-media device presents.
  - A power read reads the system alone and never looks for media.
  - A media read discovers the virtual-media device first, from the system and
    every manager it names.
  - A controller that does not answer readably fails the read rather than
    reading as empty.
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
  verification:
    description:
      - Client TLS policy; V(certificate) requires the exact declared server certificate without matching its name.
      - O(verify) must agree; V(none) requires O(verify=false) and no O(ca_data).
    type: str
    choices:
      - full
      - certificate
      - none
  ca_data:
    description:
      - Public PEM CA or server certificates that are the only anchors the controller's own
        transport is verified against.
      - Empty verifies against the system trust store. Refused beside I(verify=false).
    type: str
    default: ""
  media:
    description: Whether to discover the virtual-media device and report the image it presents.
    type: bool
    default: false
  expected_macs:
    description: Declared hardware addresses to prove before using this physical system.
    type: list
    elements: str
  pinned_uuid_base64:
    description: Recorded system UUID encoded as base64, when present.
    type: str
    default: ""
  pinned_serial_base64:
    description: Recorded system serial encoded as base64, when present.
    type: str
    default: ""
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Wait for the machine to power itself off
  bootwright.core.redfish_system_read:
    endpoint: '{{ endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
  register: machine
  retries: 30
  delay: 10
  until: (machine.power | default('')) == 'Off'

- name: Read the power state and the media the machine presents
  bootwright.core.redfish_system_read:
    endpoint: '{{ endpoint }}'
    user: '{{ user }}'
    password: '{{ password }}'
    media: true
"""

RETURN = r"""
power:
  description: The reported power state, On or Off, or the empty string when the controller reports neither.
  returned: success
  type: str
media:
  description:
    - For a media read, the image the virtual-media device presents, or the
      empty string when the controller names no image.
    - Always empty for a power read, which never looks for media.
  returned: success
  type: str
media_present:
  description: Whether an optical device holds media, including an unnamed image.
  returned: success
  type: bool
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control


def main():
    module = AnsibleModule(
        argument_spec={
            "expected_macs": {"type": "list", "elements": "str"},
            "pinned_uuid_base64": {"type": "str", "default": ""},
            "pinned_serial_base64": {"type": "str", "default": ""},
            "endpoint": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "password": {"type": "str", "required": True, "no_log": True},
            "verify": {"type": "bool", "default": True},
            "ca_data": {"type": "str", "default": ""},
            "verification": {"type": "str", "choices": ["full", "certificate", "none"]},
            "media": {"type": "bool", "default": False},
        },
        supports_check_mode=True,
    )
    params = module.params
    try:
        client = redfish_control.Client(params["endpoint"], params["user"], params["password"],
                                        verify=bool(params["verify"]), ca_data=params["ca_data"] or "", verification=params.get("verification") or "")
        if params.get("expected_macs") is not None:
            client.require_identity(params["expected_macs"], params.get("pinned_uuid_base64", ""), params.get("pinned_serial_base64", ""))
        power, present, media = read(client, bool(params["media"]))
    except redfish_control.ControllerError as failure:
        module.fail_json(msg="the management controller could not be read: %s" % failure)
    else:
        module.exit_json(changed=False, power=power, media=media, media_present=present)


def read(client, media):
    """The power state the system reports and, for a media read, the image
    the device presents; a power read reports no image."""
    power = client.power_state()
    present, image = client.media_state() if media else (False, "")
    return power, present, image


if __name__ == "__main__":
    main()
