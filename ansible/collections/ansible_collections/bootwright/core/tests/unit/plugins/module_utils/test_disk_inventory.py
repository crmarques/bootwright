"""Discovery never opens a data disk for writing or mutates its holders."""

from contextlib import nullcontext
import os
from pathlib import Path
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_inventory as inventory


def test_only_a_read_only_descriptor_and_size_ioctl_reach_the_disk(monkeypatch):
    seen = []
    number = os.makedev(8, 0)
    info = SimpleNamespace(st_mode=stat.S_IFBLK | 0o600, st_dev=1, st_ino=2, st_rdev=number)
    flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW
    descriptors = SimpleNamespace(
        O_RDONLY=os.O_RDONLY,
        O_CLOEXEC=os.O_CLOEXEC,
        O_NOFOLLOW=os.O_NOFOLLOW,
        open=lambda path, opened_flags: seen.append((path, opened_flags)) or 123,
        fstat=lambda fd: info,
        close=seen.append,
    )
    monkeypatch.setattr(inventory, "os", descriptors)
    calls = []
    monkeypatch.setattr(inventory, "number_ioctl", lambda fd, request: (calls.append((fd, request)), 8192)[1])
    with inventory.block_descriptor(Path("/dev/sda")) as (fd, device_number):
        assert device_number == number and inventory.descriptor_size(fd) == 8192
    assert seen == [(Path("/dev/sda"), flags), 123]
    assert calls == [(123, inventory.BLKGETSIZE64)]


def test_attributes_are_bounded_and_do_not_run_commands(tmp_path):
    path = tmp_path / "serial"
    path.write_bytes(b"EXAMPLE-OS\n")
    assert inventory.attribute(path) == "EXAMPLE-OS"
    for data in (b"x" * 260, b"x\x00", b"non-ascii-\xff"):
        path.write_bytes(data)
        with pytest.raises(inventory.Refused):
            inventory.attribute(path)


def test_empty_or_unknown_inventory_never_guesses(tmp_path):
    with pytest.raises(inventory.Refused, match="no supported whole disks"):
        inventory.collect(tmp_path)
    (tmp_path / "sda").mkdir()
    with pytest.raises(inventory.Refused, match="missing disk attribute"):
        inventory.collect(tmp_path)


def test_partitions_and_hidden_nvme_paths_are_excluded(tmp_path, monkeypatch):
    for name in ("sda", "sda1", "nvme0c0n1", "nvme0n1", "loop0", "sr0"):
        (tmp_path / name).mkdir()
    (tmp_path / "sda1/partition").write_text("1\n")
    (tmp_path / "nvme0n1/hidden").write_text("1\n")
    seen = []
    monkeypatch.setattr(inventory, "read_disk", lambda path, root, single_disk: seen.append(path.name) or {"serialNumber": "EXAMPLE"})
    assert inventory.collect(tmp_path) == [{"serialNumber": "EXAMPLE"}]
    assert seen == ["sda"]


@pytest.mark.parametrize("alternate", ("mmcblk0", "dm-0", "md127", "nvme0c0n1"))
def test_single_disk_preflight_refuses_unqualified_block_devices(tmp_path, alternate):
    (tmp_path / alternate).mkdir()
    with pytest.raises(inventory.Refused, match="unexpected block device"):
        inventory.collect(tmp_path, single_disk=True)


def test_single_disk_preflight_keeps_second_scsi_disk_visible(tmp_path, monkeypatch):
    for name in ("sda", "sdb", "sda1", "sr0", "loop0", "ram0"):
        (tmp_path / name).mkdir()
    (tmp_path / "sda1/partition").write_text("1\n")
    (tmp_path / "sr0/device").mkdir()
    (tmp_path / "sr0/device/type").write_text("5\n")
    monkeypatch.setattr(inventory, "read_disk", lambda path, root, single_disk: path.name)
    assert inventory.collect(tmp_path, single_disk=True) == ["sda", "sdb"]


def test_single_disk_rejects_recycled_device_number(tmp_path, monkeypatch):
    device = tmp_path / "sda"
    device.mkdir()
    (device / "device").mkdir()
    for name, value in {"dev": "8:0", "diskseq": "11", "removable": "0", "ro": "0"}.items():
        (device / name).write_text(value)
    # The node keeps its major/minor while the sysfs identity changes during the read.
    monkeypatch.setattr(inventory, "block_descriptor", lambda path: nullcontext((123, os.makedev(8, 0))))
    monkeypatch.setattr(inventory, "number_ioctl", lambda fd, request: 11 if request == inventory.BLKGETDISKSEQ else 8192)

    def replace_same_device_number(path):
        (path / "diskseq").write_text("12")
        return "naa.5000000000000001"

    monkeypatch.setattr(inventory, "wwid", replace_same_device_number)
    with pytest.raises(inventory.Refused, match="eligibility changed"):
        inventory.read_disk(device, tmp_path, single_disk=True)
