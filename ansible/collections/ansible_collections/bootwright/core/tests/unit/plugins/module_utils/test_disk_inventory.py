"""Discovery never opens a data disk for writing or mutates its holders."""

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
    monkeypatch.setattr(inventory.os, "open", lambda path, flags: seen.append((path, flags)) or 123)
    monkeypatch.setattr(inventory.os, "fstat", lambda fd: info)
    monkeypatch.setattr(inventory.os, "close", seen.append)
    calls = []
    monkeypatch.setattr(inventory, "size_ioctl", lambda fd: (calls.append(fd), 8192)[1])
    assert inventory.descriptor_size(Path("/dev/sda")) == (8192, number)
    assert seen == [(Path("/dev/sda"), os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW), 123]
    assert calls == [123]


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
    monkeypatch.setattr(inventory, "read_disk", lambda path, root: seen.append(path.name) or {"serialNumber": "EXAMPLE"})
    assert inventory.collect(tmp_path) == [{"serialNumber": "EXAMPLE"}]
    assert seen == ["sda"]
