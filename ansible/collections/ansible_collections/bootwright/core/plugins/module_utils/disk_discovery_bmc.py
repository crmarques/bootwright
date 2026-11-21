"""Read-only BMC drive inventory that can replace a RAM inventory boot.

Only a complete, unambiguous Storage inventory of plain drives qualifies. An
answer this reader cannot prove returns None, and discovery boots instead.
"""

from __future__ import annotations

# Exact integer types reject bool in controller data.
# pylint: disable=unidiomatic-typecheck

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_storage

PROTOCOLS = {"SATA": "sata", "SAS": "sas", "NVMe": "nvme", "PCIe": "nvme", "USB": "usb"}
DOING = "reading the BMC drive inventory"


def _text(value):
    if not isinstance(value, str):
        return ""
    value = value.strip()
    if len(value) > 256 or any(ord(c) < 32 or ord(c) > 126 or c in "<>&" for c in value):
        return ""
    return value


def _drive(body):
    transport = PROTOCOLS.get(body.get("Protocol"))
    serial = body.get("SerialNumber").strip() if isinstance(body.get("SerialNumber"), str) else ""
    size = body.get("CapacityBytes")
    if transport is None or redfish_storage.SERIAL.fullmatch(serial) is None:
        raise redfish_storage.Unqualified("drive identity")
    if type(size) is not int or not 4096 <= size <= redfish_storage.MAX_SIZE or size % 4096:
        raise redfish_storage.Unqualified("drive capacity")
    # Never a WWN: a BMC NAA identifier need not be the name Linux derives.
    return {
        "controllerPCIAddress": "", "fixed": True, "hidden": False, "model": _text(body.get("Model")),
        "serialNumber": serial, "sizeBytes": size, "transport": transport, "whole": True, "writable": True,
    }


def inventory(client):
    """Every present drive of this system's Storage, or None when unqualified."""
    try:
        disks = [_drive(drive) for drive in redfish_storage.plain_drives(client, DOING)]
    except (redfish_storage.Unqualified, redfish_control.ControllerError):
        return None
    return disks or None
