"""Read-only proof of one serial-selected whole drive before agent boot."""

from __future__ import annotations

# Exact integer types reject bool in controller data and frozen requests.
# pylint: disable=unidiomatic-typecheck

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_storage


REFUSAL = "the root disk identity and any declared exact capacity could not be proved uniquely from the complete drive inventory"


class Refused(redfish_control.ControllerError):
    """An incomplete or ambiguous inventory proves no root disk."""


def _selector(expected):
    if not isinstance(expected, dict):
        raise Refused(REFUSAL)
    wwn = expected.get("wwn", "")
    if "wwn" in expected and (not isinstance(wwn, str) or not wwn.startswith("0x")
                              or redfish_storage.NAA.fullmatch(wwn[2:]) is None or not int(wwn, 16)):
        raise Refused(REFUSAL)
    fields = set(expected)
    if fields == {"wwn"}:
        return wwn, "", None
    if fields not in ({"serialNumber", "sizeBytes"}, {"wwn", "serialNumber", "sizeBytes"}):
        raise Refused(REFUSAL)
    serial, size = expected["serialNumber"], expected["sizeBytes"]
    if (not isinstance(serial, str) or redfish_storage.SERIAL.fullmatch(serial) is None
            or type(size) is not int or not 4096 <= size <= redfish_storage.MAX_SIZE or size % 4096):
        raise Refused(REFUSAL)
    return wwn, serial, size


def prove(client, expected):
    """Prove each authored selector uniquely and require all to name the same drive."""
    wwn, serial, size = _selector(expected)
    by_wwn, by_serial = [], []
    try:
        for index, drive in enumerate(redfish_storage.plain_drives(client, "reading the root disk inventory")):
            if wwn:
                reported = redfish_storage.naa(drive)
                if not reported:
                    raise Refused(REFUSAL)
                if reported == wwn:
                    if drive.get("Protocol") not in ("SAS", "SATA") or not _available(drive):
                        raise Refused(REFUSAL)
                    by_wwn.append(index)
            if serial:
                reported = drive.get("SerialNumber")
                if not isinstance(reported, str) or redfish_storage.SERIAL.fullmatch(reported.strip()) is None:
                    raise Refused(REFUSAL)
                if reported.strip() == serial:
                    capacity = drive.get("CapacityBytes")
                    if type(capacity) is not int or capacity != size or not _available(drive):
                        raise Refused(REFUSAL)
                    by_serial.append(index)
    except redfish_storage.Unqualified:
        raise Refused(REFUSAL) from None
    if (wwn and len(by_wwn) != 1 or serial and len(by_serial) != 1
            or wwn and serial and by_wwn != by_serial):
        raise Refused(REFUSAL)


def _available(drive):
    return drive["Status"]["State"] == "Enabled" and drive.get("HotspareType", "None") == "None"
