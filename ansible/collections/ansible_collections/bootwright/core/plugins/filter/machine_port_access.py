"""Resolve the machine port access a frozen request names against its material.

Go freezes one machinePort object per target: the controller's endpoint, its
transport verification, the media volume a pushed image is written to, and the
names of the material variables that hold the controller's account, its trust
bundle and the identity its Machine's block pinned. The values stay in the
operation's material and never in the request; this filter is the one place a
role turns the names into the access a substrate machine port entry point
takes, so a role adds only its operation's arguments.
"""

from __future__ import annotations

from ansible.errors import AnsibleFilterError

DOCUMENTATION = r"""
name: machine_port_access
short_description: Resolve a frozen machinePort object against its material
version_added: "0.1.0"
description:
  - Returns the access every substrate machine port entry point takes.
  - The user variable must name a material entry; the password and trust
    bundle variables must when the request names them. A pin variable reads
    empty when the operation carries no recorded identity.
options:
  _input:
    description: The machinePort object the request froze.
    type: dict
    required: true
  material:
    description: The operation's material, by variable name.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# {{ request.machinePort | bootwright.core.machine_port_access(material) | combine({'power': 'off'}) }}
"""

RETURN = r"""
_value:
  description: endpoint, verify, user, password, ca, pinned_uuid_base64 and pinned_serial_base64, with verification and media_volume when frozen.
  type: dict
"""


def _named(material, port, key):
    name = port.get(key) or ""
    if not name:
        return ""
    if name not in material:
        raise AnsibleFilterError("the operation's material holds no %s the machine port names" % key)
    return material[name]


def machine_port_access(port, material):
    if not isinstance(port, dict) or not isinstance(material, dict) or not port.get("userVariable"):
        raise AnsibleFilterError("the machine port names no material to reach its controller with")
    access = {
        "endpoint": port["endpoint"],
        "verify": port["verify"],
        "user": _named(material, port, "userVariable"),
        "password": _named(material, port, "passwordVariable"),
        "ca": _named(material, port, "caVariable"),
        "pinned_uuid_base64": material.get(port.get("pinnedUUIDVariable") or "", ""),
        "pinned_serial_base64": material.get(port.get("pinnedSerialVariable") or "", ""),
    }
    if port.get("verification"):
        access["verification"] = port["verification"]
    if port.get("mediaVolume"):
        access["media_volume"] = port["mediaVolume"]
    return access


class FilterModule:
    def filters(self):
        return {"machine_port_access": machine_port_access}
