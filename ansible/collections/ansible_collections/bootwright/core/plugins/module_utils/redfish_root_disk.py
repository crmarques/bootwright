"""Read-only proof of one serial-selected whole drive before agent boot."""

from __future__ import annotations

# Exact integer types reject bool in controller data and frozen requests.
# pylint: disable=unidiomatic-typecheck

import re

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control


SERIAL = re.compile(r"[A-Za-z0-9._:+/=-]{1,256}")
MAX_STORAGE = 32
MAX_DRIVES = 256
MAX_SIZE = 64 << 40
REFUSAL = "the root disk serial number and exact capacity could not be proved uniquely from the complete drive inventory"


class Refused(redfish_control.ControllerError):
    """An incomplete or ambiguous inventory proves no root disk."""


def _link(value):
    if not isinstance(value, dict):
        raise Refused(REFUSAL)
    reference = value.get("@odata.id")
    if not isinstance(reference, str) or not 1 <= len(reference) <= 2048 or reference != reference.strip():
        raise Refused(REFUSAL)
    return reference


def _members(client, reference, allow_empty=False):
    body, _headers = client._read(reference, doing="reading the root disk inventory")
    members = body.get("Members")
    if (not isinstance(members, list) or not (0 if allow_empty else 1) <= len(members) <= MAX_STORAGE
            or "Members@odata.nextLink" in body or "@odata.nextLink" in body):
        raise Refused(REFUSAL)
    count = body.get("Members@odata.count", len(members))
    if type(count) is not int or count != len(members):
        raise Refused(REFUSAL)
    references = [client._own(_link(member), "reading the root disk inventory") for member in members]
    if len(set(references)) != len(references):
        raise Refused(REFUSAL)
    return references


def _drives(client):
    """Only drives linked by this ComputerSystem's complete Storage inventory."""
    seen = set()
    for reference in _members(client, _link(client.system().get("Storage"))):
        storage, _headers = client._read(reference, doing="reading the root disk inventory")
        drives = storage.get("Drives")
        if (not isinstance(drives, list) or len(drives) > MAX_DRIVES
                or "Drives@odata.nextLink" in storage or "@odata.nextLink" in storage):
            raise Refused(REFUSAL)
        count = storage.get("Drives@odata.count", len(drives))
        if type(count) is not int or count != len(drives):
            raise Refused(REFUSAL)
        for link in drives:
            reference = client._own(_link(link), "reading the root disk inventory")
            if reference in seen:
                continue
            seen.add(reference)
            if len(seen) > MAX_DRIVES:
                raise Refused(REFUSAL)
            drive, _headers = client._read(reference, doing="reading the root disk inventory")
            kind = drive.get("@odata.type")
            serial = drive.get("SerialNumber")
            if (not isinstance(kind, str) or not kind.startswith("#Drive.") or not kind.endswith(".Drive")
                    or not isinstance(serial, str) or not 1 <= len(serial) <= 256):
                raise Refused(REFUSAL)
            yield drive


def prove(client, expected):
    """Require one serial match, then prove its exact size; never write."""
    if not isinstance(expected, dict) or set(expected) != {"serialNumber", "sizeBytes"}:
        raise Refused(REFUSAL)
    serial, size = expected["serialNumber"], expected["sizeBytes"]
    if (not isinstance(serial, str) or SERIAL.fullmatch(serial) is None
            or type(size) is not int or not 4096 <= size <= MAX_SIZE or size % 4096):
        raise Refused(REFUSAL)
    # Count serial matches before comparing size. The native agent selects by
    # serial alone, so another disk with that serial and a different size is
    # still ambiguous and must not be allowed to win by discovery order.
    matches = [drive for drive in _drives(client) if drive["SerialNumber"] == serial]
    if len(matches) != 1 or type(matches[0].get("CapacityBytes")) is not int or matches[0]["CapacityBytes"] != size:
        raise Refused(REFUSAL)
