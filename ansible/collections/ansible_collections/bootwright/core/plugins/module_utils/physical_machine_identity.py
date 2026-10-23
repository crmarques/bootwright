"""Bounded, read-only kernel proof of one physical machine and its NICs."""

from __future__ import annotations

from itertools import islice
import os
from pathlib import Path
import re

MAC = re.compile(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}")
INTERFACE = re.compile(r"[A-Za-z0-9_.:-]{1,32}")
REFUSAL = "physical machine identity is unavailable, ambiguous or changed"


def _read(path, limit=256):
    with path.open("rb") as source:
        value = source.read(limit + 1)
    if not 0 < len(value) <= limit:
        raise ValueError(REFUSAL)
    return value.decode("ascii").strip()


def _mac(value):
    value = value.lower()
    if MAC.fullmatch(value) is None or value == "00:00:00:00:00:00" or int(value[:2], 16) & 1:
        raise ValueError(REFUSAL)
    return value


def _bond_address(path, directory, interface, names):
    master = path / "master"
    if not master.is_symlink() or not (path / "bonding_slave").is_dir():
        raise ValueError(REFUSAL)
    name = master.resolve(strict=True).name
    if name not in names or INTERFACE.fullmatch(name) is None:
        raise ValueError(REFUSAL)
    bond = directory / name
    if not master.samefile(bond) or (bond / "device").exists() or (bond / "bonding_slave").exists():
        raise ValueError(REFUSAL)
    members = _read(bond / "bonding/slaves", 4096).split()
    if not 1 <= len(members) <= 32 or len(members) != len(set(members)) or interface not in members:
        raise ValueError(REFUSAL)
    for member in members:
        if member not in names or INTERFACE.fullmatch(member) is None:
            raise ValueError(REFUSAL)
        slave = directory / member
        link = bond / ("slave_" + member)
        if not (slave / "device").exists() or not link.is_symlink() or not link.samefile(slave):
            raise ValueError(REFUSAL)
        if not (slave / "master").is_symlink() or not (slave / "master").samefile(bond):
            raise ValueError(REFUSAL)
    return _mac(_read(path / "bonding_slave/perm_hwaddr"))


def _addresses(root):
    directory = root / "class/net"
    with os.scandir(directory) as entries:
        interfaces = [directory / entry.name for entry in islice(entries, 257)]
    if len(interfaces) > 256:
        raise ValueError(REFUSAL)
    names = {path.name for path in interfaces}
    addresses = []
    for path in interfaces:
        if not (path / "device").exists():
            continue
        if INTERFACE.fullmatch(path.name) is None:
            raise ValueError(REFUSAL)
        if (path / "bonding_slave").exists():
            address = _bond_address(path, directory, path.name, names)
        else:
            if (path / "master/bonding").exists():
                raise ValueError(REFUSAL)
            address = _mac(_read(path / "address"))
        addresses.append(address)
    if len(addresses) != len(set(addresses)):
        raise ValueError(REFUSAL)
    return sorted(addresses)


def prove(uuid, macs, root=Path("/sys")):
    """Match the original UUID and complete physical MAC inventory, including bond slaves."""
    try:
        root = Path(root)
        if not isinstance(uuid, str) or not uuid.strip() or not isinstance(macs, list) or not 1 <= len(macs) <= 32:
            raise ValueError(REFUSAL)
        expected = [_mac(value) for value in macs if isinstance(value, str)]
        if len(expected) != len(macs) or expected != sorted(set(expected)):
            raise ValueError(REFUSAL)
        identity = uuid.strip().lower()
        before = _read(root / "class/dmi/id/product_uuid").lower()
        if before != identity or _addresses(root) != expected or _read(root / "class/dmi/id/product_uuid").lower() != before:
            raise ValueError(REFUSAL)
    except (OSError, UnicodeError, ValueError):
        raise ValueError(REFUSAL) from None
