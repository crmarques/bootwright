"""Bounded read-only Redfish Storage traversal and drive classification."""

from __future__ import annotations

import re

from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery

MAX_STORAGE = 32
MAX_DRIVES = 256
MAX_SIZE = 64 << 40
SERIAL = re.compile(r"[A-Za-z0-9._:+/=-]{1,256}")
NAA = re.compile(r"[0-9a-f]{16}(?:[0-9a-f]{16})?")
PRESENT = ("Enabled", "StandbyOffline", "StandbySpare")


class Unqualified(ValueError):
    """The inventory cannot prove the requested storage facts."""


def link(value):
    reference = value.get("@odata.id") if isinstance(value, dict) else None
    if not isinstance(reference, str) or not 1 <= len(reference) <= 2048 or reference != reference.strip():
        raise Unqualified("unreadable link")
    return reference


def references(client, body, limit, doing, key="Members"):
    members = redfish_discovery.complete_members(body, limit, key)
    if members is None:
        raise Unqualified("incomplete collection")
    refs = [client._own(link(member), doing) for member in members]
    if len(set(refs)) != len(refs):
        raise Unqualified("duplicate member")
    return refs


def collection(client, reference, limit, doing):
    body, _headers = client._read(reference, doing=doing)
    return references(client, body, limit, doing)


def plain_drives(client, doing):
    """Every present direct drive. A logical layout requires another proof."""
    storage_refs = collection(client, link(client.system().get("Storage")), MAX_STORAGE, doing)
    seen = set()
    for storage_ref in storage_refs:
        storage, _headers = client._read(storage_ref, doing=doing)
        if "Volumes" in storage and collection(client, link(storage["Volumes"]), MAX_DRIVES, doing):
            raise Unqualified("logical volumes present")
        for reference in references(client, storage, MAX_DRIVES, doing, "Drives"):
            if reference in seen:
                continue
            seen.add(reference)
            if len(seen) > MAX_DRIVES:
                raise Unqualified("drive bound")
            drive, _headers = client._read(reference, doing=doing)
            kind = drive.get("@odata.type")
            if not isinstance(kind, str) or not kind.startswith("#Drive.") or not kind.endswith(".Drive"):
                raise Unqualified("drive type")
            status = drive.get("Status")
            state = status.get("State") if isinstance(status, dict) else None
            if state == "Absent":
                continue
            if state not in PRESENT:
                raise Unqualified("drive state")
            links = drive.get("Links", {})
            if not isinstance(links, dict) or links.get("Volumes") or links.get("Volumes@odata.count") or "Volumes@odata.nextLink" in links:
                raise Unqualified("drive backs a logical volume")
            yield drive


def naa(body):
    """Read the single NAA durable identifier, if it is reported."""
    identifiers = body.get("Identifiers", [])
    if not isinstance(identifiers, list) or len(identifiers) > 32:
        raise Unqualified("unreadable identifiers")
    names = []
    for item in identifiers:
        if not isinstance(item, dict):
            raise Unqualified("unreadable identifier")
        if item.get("DurableNameFormat") != "NAA":
            continue
        name = item.get("DurableName")
        if not isinstance(name, str) or NAA.fullmatch(name.lower()) is None or not int(name, 16):
            raise Unqualified("invalid NAA")
        names.append("0x" + name.lower())
    if len(names) > 1:
        raise Unqualified("ambiguous NAA")
    return names[0] if names else ""
