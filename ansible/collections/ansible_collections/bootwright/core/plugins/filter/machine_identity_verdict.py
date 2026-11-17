"""Judge whether a management controller answers as the machine a Machine declares.

Machine power and the bare-metal target proof refuse a wrong target from this
one verdict, which module_utils/machine_identity.py owns with the declared-set
rule the physical machine protocol publishes its proof from.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: machine_identity_verdict
short_description: Judge a management controller's answer against a Machine's identity
version_added: "0.1.0"
description:
  - Reads one redfish_system_inspect observation and returns three verdicts.
  - C(pinned) the reported UUID, in any letter case or surrounding space, and
    the reported serial, trimmed, equal the pinned ones; an empty pin compares
    nothing and a pin that does not decode never matches.
  - C(controllerHost) the reported UUID, in any letter case or surrounding
    space, is the controller host's own, or a reported MAC address is one of
    its own; a host identity that does not decode reads as matching.
  - C(declared) the inventory names no failure, its reported addresses equal
    the declared ones both ways, each at most 32 characters with no
    surrounding space and at most 64 of them, at least one is declared, and
    the system reports a UUID or serial of at most 128 printable characters.
options:
  _input:
    description: The observation redfish_system_inspect returned.
    type: dict
    required: true
  expected:
    description: The declared hardware addresses.
    type: list
    elements: str
  expected_base64:
    description: The declared hardware addresses, comma-separated and base64-encoded.
    type: str
  pinned_uuid_base64:
    description: The pinned system UUID, base64-encoded.
    type: str
  pinned_serial_base64:
    description: The pinned serial number, base64-encoded.
    type: str
  host_uuid_base64:
    description: The controller host's own system UUID, base64-encoded.
    type: str
  host_macs_base64:
    description: The controller host's own MAC addresses, comma-separated and base64-encoded.
    type: str
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Refuse a machine that is not the declared one
  ansible.builtin.assert:
    that:
      - (observed.observation | bootwright.core.machine_identity_verdict(expected=declared_macs)).declared
"""

RETURN = r"""
_value:
  description: The verdicts pinned, controllerHost and declared.
  type: dict
"""

from ansible_collections.bootwright.core.plugins.module_utils.machine_identity import verdict


def machine_identity_verdict(observation, **pins):
    """The identity verdicts of one observation."""
    return verdict(observation, **pins)


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"machine_identity_verdict": machine_identity_verdict}
