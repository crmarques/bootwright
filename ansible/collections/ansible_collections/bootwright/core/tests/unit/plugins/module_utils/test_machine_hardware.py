"""An OS-ready Machine claims its server by the addresses its NICs carry in hardware, and by no other."""

from __future__ import annotations

import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import machine_hardware as hardware


def nic(root, name, kind=1, device=True, physfn=False, assign="0", address="00:00:00:00:00:00"):
    directory = root / name
    directory.mkdir()
    (directory / "type").write_text("%d\n" % kind)
    if assign is not None:
        (directory / "addr_assign_type").write_text(assign + "\n")
    (directory / "address").write_text(address + "\n")
    if device:
        (directory / "device").mkdir()
        if physfn:
            (directory / "device" / "physfn").mkdir()
    return directory


def permanent_from(table, asked):
    def reader(name):
        asked.append(name)
        value = table.get(name)
        return bytes(int(octet, 16) for octet in value.split(":")) if value else None
    return reader


def host(tmp_path):
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "lo", kind=772, device=False, address="00:00:00:00:00:00")
    nic(root, "eno1", address="AA:BB:CC:DD:EE:01")
    nic(root, "eno2", assign="3", address="aa:bb:cc:dd:ee:01")
    nic(root, "bond0", device=False, assign="3", address="aa:bb:cc:dd:ee:01")
    nic(root, "eno1.100", device=False, assign="2", address="aa:bb:cc:dd:ee:01")
    nic(root, "br0", device=False, assign="3", address="aa:bb:cc:dd:ee:09")
    nic(root, "veth0", device=False, assign="1", address="aa:bb:cc:dd:ee:0a")
    nic(root, "tap0", device=False, assign="1", address="fe:54:00:00:00:0b")
    nic(root, "ens5f0v0", physfn=True, assign="0", address="aa:bb:cc:dd:ee:0c")
    nic(root, "wlp1s0", assign="1", address="aa:bb:cc:dd:ee:0d")
    nic(root, "enp9s0", assign="3", address="aa:bb:cc:dd:ee:0e")
    nic(root, "enp8s0", assign=None, address="aa:bb:cc:dd:ee:0f")
    (root / "bonding_masters").write_text("bond0\n")
    return root


def test_only_a_device_backed_nic_by_its_permanent_address_is_reported(tmp_path):
    asked = []
    found = hardware.read(str(host(tmp_path)), permanent_from({"eno2": "aa:bb:cc:dd:ee:02", "enp9s0": "00:00:00:00:00:00"}, asked))
    assert found == ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"]
    assert sorted(asked) == ["eno1", "eno2", "enp8s0", "enp9s0", "wlp1s0"]


def test_a_bond_member_counts_by_the_permanent_address_its_driver_reports(tmp_path):
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "eno1", assign="3", address="aa:bb:cc:dd:ee:01")
    nic(root, "eno2", assign="3", address="aa:bb:cc:dd:ee:01")
    nic(root, "bond0", device=False, assign="3", address="aa:bb:cc:dd:ee:01")
    found = hardware.read(str(root), permanent_from({"eno1": "aa:bb:cc:dd:ee:11", "eno2": "aa:bb:cc:dd:ee:12"}, []))
    assert found == ["aa:bb:cc:dd:ee:11", "aa:bb:cc:dd:ee:12"]


def test_a_virtual_guest_nic_counts_by_the_address_its_hypervisor_gave_it(tmp_path):
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "enp1s0", address="52:54:00:12:34:56")
    assert hardware.read(str(root), permanent_from({"enp1s0": "52:54:00:12:34:56"}, [])) == ["52:54:00:12:34:56"]


@pytest.mark.parametrize("raw", [b"", b"\0" * 6, b"\x01\0\0\0\0\x01", b"\xaa" * 5, b"\xaa" * 8, "aa:bb:cc:dd:ee:ff"])
def test_an_address_no_nic_carries_is_none(raw):
    assert hardware.canonical(raw) is None


@pytest.mark.parametrize("text", ["", "aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee", "00:00:00:00:00:00", "01:00:5e:00:00:01", None])
def test_a_sysfs_address_that_names_no_unicast_nic_is_none(text):
    assert hardware.parsed(text) is None


def test_a_host_whose_interfaces_cannot_be_listed_proves_nothing(tmp_path):
    with pytest.raises(hardware.Unreadable):
        hardware.read(str(tmp_path / "absent"), permanent_from({}, []))


def test_an_interface_that_cannot_be_read_proves_nothing(tmp_path, monkeypatch):
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "eno1", address="aa:bb:cc:dd:ee:01")
    real = open

    def refusing(path, *arguments, **keywords):
        if str(path).endswith(os.path.join("eno1", "address")):
            raise PermissionError(13, "denied")
        return real(path, *arguments, **keywords)

    monkeypatch.setattr("builtins.open", refusing)
    with pytest.raises(hardware.Unreadable):
        hardware.read(str(root), permanent_from({}, []))


def test_an_interface_gone_while_it_is_read_is_skipped(tmp_path):
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "eno1", address="aa:bb:cc:dd:ee:01")
    gone = nic(root, "eno2", address="aa:bb:cc:dd:ee:02")
    (gone / "type").unlink()
    assert hardware.read(str(root), permanent_from({}, [])) == ["aa:bb:cc:dd:ee:01"]


def test_more_interfaces_than_a_host_names_proves_nothing(tmp_path, monkeypatch):
    monkeypatch.setattr(hardware, "MAX_INTERFACES", 1)
    root = tmp_path / "net"
    root.mkdir()
    nic(root, "eno1", address="aa:bb:cc:dd:ee:01")
    nic(root, "eno2", address="aa:bb:cc:dd:ee:02")
    with pytest.raises(hardware.Unreadable):
        hardware.read(str(root), permanent_from({}, []))


def test_a_nic_on_usb_such_as_a_bmc_host_interface_counts_for_nothing(tmp_path):
    root = tmp_path / "net"
    root.mkdir()
    (tmp_path / "bus" / "usb").mkdir(parents=True)
    (tmp_path / "bus" / "pci").mkdir(parents=True)
    usb = nic(root, "usb0", address="aa:bb:cc:dd:ee:20")
    (usb / "device" / "subsystem").symlink_to(tmp_path / "bus" / "usb")
    pci = nic(root, "eno1", address="aa:bb:cc:dd:ee:01")
    (pci / "device" / "subsystem").symlink_to(tmp_path / "bus" / "pci")
    nic(root, "enp1s0", address="52:54:00:12:34:56")
    asked = []
    assert hardware.read(str(root), permanent_from({}, asked)) == ["52:54:00:12:34:56", "aa:bb:cc:dd:ee:01"]
    assert sorted(asked) == ["eno1", "enp1s0"]
