"""Synthetic sysfs trees qualify physical and bonded NIC identity without host reads."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity as identity

UUID = "11111111-1111-4111-8111-111111111111"
MACS = ["02:00:00:00:00:01", "02:00:00:00:00:02"]


def hardware(root, bonded=False):
    uuid = root / "class/dmi/id/product_uuid"
    uuid.parent.mkdir(parents=True)
    uuid.write_text(UUID + "\n")
    directory = root / "class/net"
    for index, name in enumerate(("Eth0", "Eth1")):
        port = directory / name
        (port / "device").mkdir(parents=True)
        (port / "address").write_text((MACS[0] if bonded else MACS[index]) + "\n")
    vlan = directory / "bond0.42"
    vlan.mkdir()
    (vlan / "address").write_text(MACS[0] + "\n")
    if bonded:
        bond = directory / "Bond0"
        (bond / "bonding").mkdir(parents=True)
        (bond / "bonding/slaves").write_text("Eth0 Eth1\n")
        for index, name in enumerate(("Eth0", "Eth1")):
            port = directory / name
            (port / "master").symlink_to("../Bond0")
            (port / "bonding_slave").mkdir()
            (port / "bonding_slave/perm_hwaddr").write_text(MACS[index] + "\n")
            (bond / ("slave_" + name)).symlink_to("../" + name)
    return directory


@pytest.mark.parametrize("bonded", [False, True])
def test_full_original_hardware_inventory_is_read_without_changes(tmp_path, bonded):
    hardware(tmp_path, bonded)
    before = {str(path): path.read_bytes() for path in tmp_path.rglob("*") if path.is_file()}
    identity.prove(UUID.upper(), MACS, tmp_path)
    assert {str(path): path.read_bytes() for path in tmp_path.rglob("*") if path.is_file()} == before


@pytest.mark.parametrize(
    "damage",
    [
        "missing-permanent",
        "changed-permanent",
        "duplicate-permanent",
        "missing-master",
        "missing-reciprocal",
        "missing-membership",
        "duplicate-membership",
        "foreign-member",
        "slave-metadata-missing",
        "physical-master",
    ],
)
def test_ambiguous_bond_identity_has_no_current_address_fallback(tmp_path, damage):
    directory = hardware(tmp_path, True)
    slave, bond = directory / "Eth1", directory / "Bond0"
    permanent = slave / "bonding_slave/perm_hwaddr"
    if damage == "missing-permanent":
        permanent.unlink()
    elif damage == "changed-permanent":
        permanent.write_text("02:00:00:00:00:ff\n")
    elif damage == "duplicate-permanent":
        permanent.write_text(MACS[0] + "\n")
    elif damage == "missing-master":
        (slave / "master").unlink()
    elif damage == "missing-reciprocal":
        (bond / "slave_Eth1").unlink()
    elif damage == "missing-membership":
        (bond / "bonding/slaves").write_text("Eth0\n")
    elif damage == "duplicate-membership":
        (bond / "bonding/slaves").write_text("Eth0 Eth1 Eth1\n")
    elif damage == "foreign-member":
        (bond / "bonding/slaves").write_text("Eth0 Eth1 bond0.42\n")
    elif damage == "slave-metadata-missing":
        permanent.unlink()
        permanent.parent.rmdir()
    else:
        (bond / "device").mkdir()
    with pytest.raises(ValueError, match="^" + identity.REFUSAL + "$"):
        identity.prove(UUID, MACS, tmp_path)


@pytest.mark.parametrize("damage", ["uuid", "missing-nic", "extra-nic", "multicast", "zero-mac", "unreadable", "oversized", "unsorted"])
def test_unknown_or_substituted_physical_hardware_refuses(tmp_path, damage):
    directory = hardware(tmp_path)
    expected = MACS
    if damage == "uuid":
        (tmp_path / "class/dmi/id/product_uuid").write_text("22222222-2222-4222-8222-222222222222\n")
    elif damage == "missing-nic":
        (directory / "Eth1/device").rmdir()
    elif damage == "extra-nic":
        (directory / "Eth2/device").mkdir(parents=True)
        (directory / "Eth2/address").write_text("02:00:00:00:00:03\n")
    elif damage in ("multicast", "zero-mac", "oversized"):
        value = "03:00:00:00:00:02" if damage == "multicast" else "00:00:00:00:00:00" if damage == "zero-mac" else "a" * 257
        (directory / "Eth1/address").write_text(value + "\n")
    elif damage == "unreadable":
        (directory / "Eth1/address").unlink()
    else:
        expected = list(reversed(MACS))
    with pytest.raises(ValueError, match="^" + identity.REFUSAL + "$"):
        identity.prove(UUID, expected, tmp_path)
