"""Read-only correlation of an existing logical Volume to its OS-visible NAA."""

from __future__ import annotations

# Exact controller integer types exclude booleans.
# pylint: disable=unidiomatic-typecheck

import re

from ansible_collections.bootwright.core.plugins.module_utils import redfish_root_disk


ID = re.compile(r"[A-Za-z0-9_-]{1,128}")
NAA = re.compile(r"[0-9a-f]{16}(?:[0-9a-f]{16})?")
REFUSAL = (
    "the existing root volume could not be proved with one unique OS-correlatable NAA identifier and exact capacity; "
    "declare a directly mappable spec.substrate.baremetal.rootDevice.volumeRef, or select the OS-visible disk by serialNumber and sizeBytes"
)
Refused = redfish_root_disk.Refused


def _naa(body):
    identifiers = body.get("Identifiers", [])
    if not isinstance(identifiers, list) or len(identifiers) > 32:
        raise Refused(REFUSAL)
    names = []
    for item in identifiers:
        if not isinstance(item, dict):
            raise Refused(REFUSAL)
        if item.get("DurableNameFormat") != "NAA":
            continue
        name = item.get("DurableName")
        if not isinstance(name, str):
            raise Refused(REFUSAL)
        name = name.lower()
        if NAA.fullmatch(name) is None or not int(name, 16):
            raise Refused(REFUSAL)
        names.append("0x" + name)
    if len(names) > 1:
        raise Refused(REFUSAL)
    return names[0] if names else ""


def _read(client, reference, kind):
    body, _headers = client._read(reference, doing="reading the root volume inventory")
    datatype = body.get("@odata.type")
    if not isinstance(datatype, str) or not datatype.startswith("#" + kind + ".") or not datatype.endswith("." + kind):
        raise Refused(REFUSAL)
    return body


def _inventory(client):
    seen, drives, volumes = set(), [], []
    for reference in redfish_root_disk._members(client, redfish_root_disk._link(client.system().get("Storage"))):
        storage = _read(client, reference, "Storage")
        storage_id = storage.get("Id")
        if not isinstance(storage_id, str) or ID.fullmatch(storage_id) is None or storage_id in seen:
            raise Refused(REFUSAL)
        seen.add(storage_id)
        links = storage.get("Drives")
        if not isinstance(links, list) or len(links) > redfish_root_disk.MAX_DRIVES or "Drives@odata.nextLink" in storage:
            raise Refused(REFUSAL)
        count = storage.get("Drives@odata.count", len(links))
        if type(count) is not int or count != len(links):
            raise Refused(REFUSAL)
        for link in links:
            drive = _read(client, client._own(redfish_root_disk._link(link), "reading the root volume inventory"), "Drive")
            drives.append(_naa(drive))
            if len(drives) > redfish_root_disk.MAX_DRIVES:
                raise Refused(REFUSAL)
        if "Volumes" not in storage:
            continue
        volume_ids = set()
        for link in redfish_root_disk._members(client, redfish_root_disk._link(storage["Volumes"]), allow_empty=True):
            volume = _read(client, link, "Volume")
            volume_id = volume.get("Id")
            if not isinstance(volume_id, str) or ID.fullmatch(volume_id) is None or volume_id in volume_ids:
                raise Refused(REFUSAL)
            volume_ids.add(volume_id)
            volumes.append((storage_id, volume_id, _naa(volume), volume.get("CapacityBytes"), link))
            if len(volumes) > redfish_root_disk.MAX_DRIVES:
                raise Refused(REFUSAL)
    return volumes, drives


def discover(client, reference):
    """Resolve one positively named logical volume, never infer a boot target."""
    if isinstance(reference, dict) and set(reference) == {"firmware"} and type(reference["firmware"]) is bool and reference["firmware"]:
        volumes, _drives = _inventory(client)
        reference = _boot_reference(client, volumes)
    if (not isinstance(reference, dict) or set(reference) != {"storageId", "volumeId"}
            or any(not isinstance(value, str) or ID.fullmatch(value) is None for value in reference.values())):
        raise Refused(REFUSAL)
    volumes, drives = _inventory(client)
    matches = [item for item in volumes if item[:2] == (reference["storageId"], reference["volumeId"])]
    if len(matches) != 1:
        raise Refused(REFUSAL)
    storage, volume, wwn, size, _link = matches[0]
    if (not wwn or sum(item[2] == wwn for item in volumes) != 1 or wwn in drives
            or type(size) is not int or not 4096 <= size <= redfish_root_disk.MAX_SIZE or size % 4096):
        raise Refused(REFUSAL)
    return {"storageId": storage, "volumeId": volume, "wwn": wwn, "sizeBytes": size}


def _boot_reference(client, volumes):
    """First enabled persistent local-disk option, positively linked to a Volume."""
    boot = client.system().get("Boot")
    if not isinstance(boot, dict) or boot.get("BootOrderPropertySelection", "BootOrder") != "BootOrder":
        raise Refused(REFUSAL)
    order = boot.get("BootOrder")
    if (not isinstance(order, list) or not 1 <= len(order) <= 32
            or any(not isinstance(item, str) or not 1 <= len(item) <= 128 for item in order) or len(set(order)) != len(order)):
        raise Refused(REFUSAL)
    options = {}
    for link in redfish_root_disk._members(client, redfish_root_disk._link(boot.get("BootOptions"))):
        option = _read(client, link, "BootOption")
        name = option.get("BootOptionReference")
        if not isinstance(name, str) or not 1 <= len(name) <= 128 or name in options:
            raise Refused(REFUSAL)
        options[name] = option
    if any(item not in options for item in order):
        raise Refused(REFUSAL)
    for name in order:
        option = options[name]
        enabled = option.get("BootOptionEnabled")
        if type(enabled) is not bool:
            raise Refused(REFUSAL)
        if not enabled:
            continue
        alias = option.get("Alias")
        if alias in ("Pxe", "Cd", "Usb", "Diags", "BiosSetup", "UefiShell", "Utilities", "Floppy"):
            continue
        if alias != "Hdd":
            raise Refused(REFUSAL)
        related = option.get("RelatedItem")
        if not isinstance(related, list) or len(related) != 1:
            raise Refused(REFUSAL)
        target = client._own(redfish_root_disk._link(related[0]), "reading the default boot volume")
        matches = [item for item in volumes if item[4] == target]
        if len(matches) != 1:
            raise Refused(REFUSAL)
        return {"storageId": matches[0][0], "volumeId": matches[0][1]}
    raise Refused(REFUSAL)
