"""One atomic publication: exclusive stage in the same directory, file and directory synced, no stage left."""

import os
import stat
from unittest import mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import atomic_publish, libvirt_nm_connections, libvirt_vlan_refusal


def synced(monkeypatch):
    calls = []
    original = os.fsync

    def record(descriptor):
        calls.append(stat.S_ISDIR(os.fstat(descriptor).st_mode))
        original(descriptor)

    monkeypatch.setattr(atomic_publish.os, "fsync", record)
    return calls


def test_a_path_publication_replaces_the_name_and_syncs_file_then_directory(tmp_path, monkeypatch):
    (tmp_path / "unit").write_bytes(b"old")
    calls = synced(monkeypatch)
    atomic_publish.publish(str(tmp_path), "unit", b"new", 0o644)
    assert (tmp_path / "unit").read_bytes() == b"new"
    assert stat.S_IMODE((tmp_path / "unit").stat().st_mode) == 0o644
    assert calls == [False, True]
    assert [path.name for path in tmp_path.iterdir()] == ["unit"]


def test_a_held_descriptor_publication_stays_relative_to_it(tmp_path, monkeypatch):
    calls = synced(monkeypatch)
    held = os.open(tmp_path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        atomic_publish.publish(held, "keyfile", b"secret", 0o600)
    finally:
        os.close(held)
    assert stat.S_IMODE((tmp_path / "keyfile").stat().st_mode) == 0o600 and calls == [False, True]


def test_the_exact_mode_holds_under_any_umask(tmp_path):
    old = os.umask(0o077)
    try:
        atomic_publish.publish(str(tmp_path), "unit", b"data", 0o644)
    finally:
        os.umask(old)
    assert stat.S_IMODE((tmp_path / "unit").stat().st_mode) == 0o644


def test_a_failed_publication_removes_its_stage_and_keeps_the_old_file(tmp_path):
    (tmp_path / "unit").write_bytes(b"old")
    with mock.patch.object(atomic_publish.os, "replace", side_effect=OSError("fixture")):
        with pytest.raises(OSError):
            atomic_publish.publish(str(tmp_path), "unit", b"new", 0o644)
    assert [path.name for path in tmp_path.iterdir()] == ["unit"] and (tmp_path / "unit").read_bytes() == b"old"


def test_a_failed_file_sync_removes_its_stage(tmp_path):
    with mock.patch.object(atomic_publish.os, "fsync", side_effect=OSError("fixture")):
        with pytest.raises(OSError):
            atomic_publish.publish(str(tmp_path), "unit", b"new", 0o644)
    assert list(tmp_path.iterdir()) == []


def test_a_linked_directory_is_never_followed(tmp_path):
    target = tmp_path / "target"
    target.mkdir()
    (tmp_path / "link").symlink_to(target)
    with pytest.raises(OSError):
        atomic_publish.publish(str(tmp_path / "link"), "unit", b"new", 0o644)
    assert list(target.iterdir()) == []


@pytest.mark.parametrize("name, prefix", [("a/b", ".stage-"), ("..", ".stage-"), ("", ".stage-"), ("unit", "stage-")])
def test_a_name_outside_the_directory_or_a_visible_stage_refuses(tmp_path, name, prefix):
    with pytest.raises(ValueError):
        atomic_publish.publish(str(tmp_path), name, b"new", 0o644, prefix)
    assert list(tmp_path.iterdir()) == []


def test_the_unit_and_keyfile_writers_that_synced_no_directory_now_sync_it(tmp_path, monkeypatch):
    calls = synced(monkeypatch)
    libvirt_vlan_refusal.write_unit(str(tmp_path), str(tmp_path / "bootwright-vlan.service"), b"[Unit]\n")
    entry = {"name": "bootwright-leg", "sections": [("connection", [("id", "bootwright-leg")])]}
    libvirt_nm_connections.write_keyfile(str(tmp_path / "connections"), entry)
    assert calls == [False, True, False, True]
    keyfile = tmp_path / "connections" / ("bootwright-leg" + libvirt_nm_connections.SUFFIX)
    assert stat.S_IMODE(keyfile.stat().st_mode) == 0o600 and keyfile.read_bytes() == libvirt_nm_connections.keyfile(entry)


def test_a_stage_already_gone_never_masks_the_original_failure(tmp_path):
    with mock.patch.object(atomic_publish.os, "replace", side_effect=OSError("original")), \
            mock.patch.object(atomic_publish.os, "unlink", side_effect=FileNotFoundError("stage")):
        with pytest.raises(OSError, match="^original$"):
            atomic_publish.publish(str(tmp_path), "unit", b"new", 0o644)
