"""Read-only BMC drive inventory that can replace a RAM inventory boot.

Only a complete, unambiguous Storage inventory of plain drives qualifies. An
answer this reader cannot prove returns None, and discovery boots instead.
"""

from __future__ import annotations

# Exact integer types reject bool in controller data.
# pylint: disable=unidiomatic-typecheck

import re

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

MAX_STORAGE = 32
MAX_DRIVES = 256
MAX_SIZE = 64 << 40
SERIAL = re.compile(r"[A-Za-z0-9._:+/=-]{1,256}\Z")
PROTOCOLS = {"SATA": "sata", "SAS": "sas", "NVMe": "nvme", "PCIe": "nvme", "USB": "usb"}
PRESENT = ("Enabled", "StandbyOffline", "StandbySpare")
DOING = "reading the BMC drive inventory"


class Unqualified(Exception):
    """The BMC inventory cannot stand in for what Linux would enumerate."""


def _link(value):
    reference = value.get("@odata.id") if isinstance(value, dict) else None
    if not isinstance(reference, str) or not 1 <= len(reference) <= 2048 or reference != reference.strip():
        raise Unqualified("link")
    return reference


def _members(client, reference, bound, key="Members"):
    body, _headers = client._read(reference, doing=DOING)
    return _listed(client, body, bound, key)


def _listed(client, body, bound, key):
    members = body.get(key)
    if not isinstance(members, list) or len(members) > bound or key + "@odata.nextLink" in body or "@odata.nextLink" in body:
        raise Unqualified("collection")
    count = body.get(key + "@odata.count", len(members))
    if type(count) is not int or count != len(members):
        raise Unqualified("collection count")
    references = [client._own(_link(member), DOING) for member in members]
    if len(set(references)) != len(references):
        raise Unqualified("duplicate member")
    return references


def _text(value):
    if not isinstance(value, str):
        return ""
    value = value.strip()
    if len(value) > 256 or any(ord(c) < 32 or ord(c) > 126 or c in "<>&" for c in value):
        return ""
    return value


def _drive(body):
    kind = body.get("@odata.type")
    if not isinstance(kind, str) or not kind.startswith("#Drive.") or not kind.endswith(".Drive"):
        raise Unqualified("drive type")
    status = body.get("Status")
    state = status.get("State") if isinstance(status, dict) else None
    if state == "Absent":
        return None
    if state not in PRESENT:
        raise Unqualified("drive state")
    links = body.get("Links") if isinstance(body.get("Links"), dict) else {}
    if links.get("Volumes") or links.get("Volumes@odata.count"):
        raise Unqualified("drive backs a logical volume")
    transport = PROTOCOLS.get(body.get("Protocol"))
    serial = body.get("SerialNumber").strip() if isinstance(body.get("SerialNumber"), str) else ""
    size = body.get("CapacityBytes")
    if transport is None or SERIAL.fullmatch(serial) is None:
        raise Unqualified("drive identity")
    if type(size) is not int or not 4096 <= size <= MAX_SIZE or size % 4096:
        raise Unqualified("drive capacity")
    # Never a WWN: a BMC NAA identifier need not be the name Linux derives.
    return {
        "controllerPCIAddress": "", "fixed": True, "hidden": False, "model": _text(body.get("Model")),
        "serialNumber": serial, "sizeBytes": size, "transport": transport, "whole": True, "writable": True,
    }


def _storage(client, reference, seen, disks):
    storage, _headers = client._read(reference, doing=DOING)
    volumes = storage.get("Volumes")
    if volumes is not None and _members(client, _link(volumes), MAX_DRIVES):
        raise Unqualified("logical volumes present")
    for drive in _listed(client, storage, MAX_DRIVES, "Drives"):
        if drive in seen:
            continue
        seen.add(drive)
        if len(seen) > MAX_DRIVES:
            raise Unqualified("drive bound")
        body, _headers = client._read(drive, doing=DOING)
        disk = _drive(body)
        if disk is not None:
            disks.append(disk)


def inventory(client):
    """Every present drive of this system's Storage, or None when unqualified."""
    try:
        storage = client.system().get("Storage")
        if storage is None:
            return None
        seen, disks = set(), []
        for reference in _members(client, _link(storage), MAX_STORAGE):
            _storage(client, reference, seen, disks)
    except (Unqualified, redfish_control.ControllerError):
        return None
    return disks or None
