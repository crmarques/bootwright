"""Concurrent builders reserve peak bytes without persistent process identities."""

import errno
import fcntl
from itertools import count
import multiprocessing
import os
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import discovery_capacity as capacity


@pytest.fixture
def root(tmp_path, monkeypatch):
    original = os.fstat

    def root_owned(fd):
        fields = list(original(fd))
        fields[4] = 0
        return os.stat_result(fields)

    def directory(path, create, mode=0o700):
        if create:
            path.mkdir(mode=mode, exist_ok=True)

    monkeypatch.setattr(capacity.os, "fstat", root_owned)
    monkeypatch.setattr(capacity.records, "directory", directory)
    return tmp_path


def paths(root, size=100):
    return {root.stat().st_dev: (root, size)}


def entries(root):
    return list((root / ".discovery-builds").iterdir())


def test_live_leases_count_full_budgets_without_paths_or_process_ids(root):
    with capacity.reserve(root, paths(root)):
        with capacity.reserve(root, paths(root, 200)):
            with capacity.locked(root) as directory:
                used, builders = capacity.active(directory)
            assert builders == 2 and used == {str(root.stat().st_dev): 300}
            for path in entries(root):
                assert capacity.budgets(path.read_bytes()) in ({str(root.stat().st_dev): 100}, {str(root.stat().st_dev): 200})
                assert path.stat().st_mode & 0o777 == 0o600 and path.stat().st_nlink == 1
    assert entries(root) == []


def test_cancellation_releases_its_lease_and_a_retry_can_build(root):
    with pytest.raises(KeyboardInterrupt):
        with capacity.reserve(root, paths(root)):
            raise KeyboardInterrupt
    assert entries(root) == []
    with capacity.reserve(root, paths(root)):
        assert len(entries(root)) == 1
    assert entries(root) == []


def abandon(root, ready, partial):
    # A separate spawned process owns the real kernel lease. Root identity is
    # simulated only by the parent at inspection, not inherited through fork.
    directory = os.open(root / ".discovery-builds", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    fcntl.flock(directory, fcntl.LOCK_EX)
    if partial:
        ready.send(True)
        capacity.os.write = lambda *_args: os._exit(0)
    capacity.create(directory, {str(root.stat().st_dev): 100})
    os.close(directory)
    ready.send(True)
    os._exit(0)  # Simulate an action killed before its finally block.


@pytest.mark.parametrize("partial", (False, True))
def test_a_dead_builder_is_reclaimed_only_after_its_kernel_lock_is_free(root, partial):
    with capacity.locked(root):
        pass
    context = multiprocessing.get_context("spawn")
    receive, send = context.Pipe(duplex=False)
    process = context.Process(target=abandon, args=(root, send, partial))
    process.start()
    try:
        assert receive.poll(5) and receive.recv() is True
        process.join(timeout=5)
        assert process.exitcode == 0 and len(entries(root)) == 1
        (root / "unrelated-scratch").write_bytes(b"still allocated")
        with capacity.reserve(root, paths(root)):
            with capacity.locked(root) as directory:
                used, builders = capacity.active(directory)
            assert builders == 1 and used == {str(root.stat().st_dev): 100}
        assert entries(root) == [] and (root / "unrelated-scratch").read_bytes() == b"still allocated"
    finally:
        if process.is_alive():
            process.terminate()
        process.join(timeout=5)
        receive.close()
        send.close()


def test_an_unlocked_partial_lease_is_reaped_without_reading_its_payload(root):
    directory = root / ".discovery-builds"
    directory.mkdir()
    stage = directory / ("lease-" + "a" * 32)
    stage.write_bytes(b"incomplete metadata")
    stage.chmod(0o600)
    with capacity.reserve(root, paths(root)):
        assert not stage.exists() and len(entries(root)) == 1
    assert entries(root) == []


@pytest.mark.parametrize("data", [
    b"{}", b'{"1":true}', b'{"1":0}', b'{"1":1.5}', b'{"1":"1"}', b'{"1":1,"2":1,"3":1}',
    b'{"-1":1}', b'{"01":1}', b'{"18446744073709551616":1}', b'{"1":1099511627777}',
    b'{"1":1, "2":2}', b'{"1":1,"1":2}', b"not json",
])
def test_active_metadata_has_a_closed_bounded_canonical_shape(data):
    with pytest.raises(ValueError):
        capacity.budgets(data)


@pytest.mark.parametrize("kind", ["symlink", "directory", "fifo", "hardlink", "writable", "oversize", "owner", "unknown"])
def test_an_unsafe_entry_is_neither_read_as_free_nor_removed(root, monkeypatch, kind):
    directory = root / ".discovery-builds"
    directory.mkdir()
    path = directory / ("lease-" + "a" * 32)
    target = root / "unrelated"
    target.write_bytes(b"untouched")
    if kind == "symlink":
        path.symlink_to(target)
    elif kind == "directory":
        path.mkdir()
    elif kind == "fifo":
        os.mkfifo(path, 0o600)
    else:
        path.write_bytes(b"x" * (capacity.MAX_RECORD + 1) if kind == "oversize" else b"{}")
        path.chmod(0o666 if kind == "writable" else 0o600)
        if kind == "hardlink":
            os.link(path, root / "another-link")
        if kind == "owner":
            original = os.fstat

            def foreign(fd):
                fields = list(original(fd))
                fields[4] = 1001
                return os.stat_result(fields)

            monkeypatch.setattr(capacity.os, "fstat", foreign)
        if kind == "unknown":
            path = path.rename(directory / "unrecognized")
    with pytest.raises((ValueError, OSError)):
        with capacity.reserve(root, paths(root)):
            pytest.fail("unproved metadata admitted construction")
    assert path.is_symlink() or path.exists()
    assert target.read_bytes() == b"untouched"


@pytest.mark.parametrize("interrupted", (False, True))
@pytest.mark.parametrize("slot_limit", (False, True))
def test_waiting_is_bounded_and_never_reaps_a_live_reservation(root, monkeypatch, interrupted, slot_limit):
    with capacity.locked(root) as directory:
        name, held = capacity.create(directory, {str(root.stat().st_dev): 100})
    before = (root / ".discovery-builds" / name).read_bytes()
    clock = count()
    monkeypatch.setattr(capacity.time, "monotonic", lambda: next(clock))
    monkeypatch.setattr(capacity, "WAIT_SECONDS", 3)
    monkeypatch.setattr(capacity, "MAX_LEASES", 1 if slot_limit else 128)
    monkeypatch.setattr(capacity.time, "sleep", Mock(side_effect=KeyboardInterrupt if interrupted else None))
    available = 1000 if slot_limit else 100
    monkeypatch.setattr(capacity.os, "statvfs", lambda path: SimpleNamespace(f_bavail=available, f_frsize=1, f_bsize=1))
    try:
        with pytest.raises(KeyboardInterrupt if interrupted else OSError) as caught:
            with capacity.reserve(root, paths(root)):
                pytest.fail("construction began over another builder's reserved peak")
        if not interrupted:
            assert caught.value.errno == errno.ENOSPC
        assert len(entries(root)) == 1 and (root / ".discovery-builds" / name).read_bytes() == before
    finally:
        with capacity.locked(root) as directory:
            capacity.remove(directory, name, held)
        os.close(held)
    assert entries(root) == []


def test_short_metadata_write_leaves_no_lease_or_stage(root, monkeypatch):
    write = capacity.os.write
    monkeypatch.setattr(capacity.os, "write", lambda *args: 0)
    with pytest.raises(ValueError):
        with capacity.reserve(root, paths(root)):
            pytest.fail("short lease publication was accepted")
    assert entries(root) == []
    monkeypatch.setattr(capacity.os, "write", write)
    with capacity.reserve(root, paths(root)):
        pass


def test_lease_creation_has_no_multi_link_crash_window(root, monkeypatch):
    monkeypatch.setattr(capacity.os, "link", Mock(side_effect=AssertionError("lease publication must keep one link")))
    with capacity.reserve(root, paths(root)):
        assert len(entries(root)) == 1 and entries(root)[0].stat().st_nlink == 1
    assert entries(root) == []


def test_interrupted_release_closes_the_lock_for_the_next_admission(root, monkeypatch):
    remove = capacity.remove
    with pytest.raises(InterruptedError):
        with capacity.reserve(root, paths(root)):
            monkeypatch.setattr(capacity, "remove", Mock(side_effect=InterruptedError))
    assert len(entries(root)) == 1
    monkeypatch.setattr(capacity, "remove", remove)
    with capacity.reserve(root, paths(root)):
        assert len(entries(root)) == 1
    assert entries(root) == []


def test_non_contention_lock_errors_are_not_treated_as_live_or_stale(root, monkeypatch):
    with capacity.locked(root) as directory:
        name, held = capacity.create(directory, {str(root.stat().st_dev): 100})
    flock = capacity.fcntl.flock

    def fail_probe(fd, flags):
        if flags & fcntl.LOCK_NB:
            raise OSError(errno.EIO, "synthetic lock failure")
        return flock(fd, flags)

    monkeypatch.setattr(capacity.fcntl, "flock", fail_probe)
    try:
        with pytest.raises(OSError) as caught:
            with capacity.reserve(root, paths(root)):
                pytest.fail("an unproved lock was accepted")
        assert caught.value.errno == errno.EIO and len(entries(root)) == 1
    finally:
        monkeypatch.setattr(capacity.fcntl, "flock", flock)
        with capacity.locked(root) as directory:
            capacity.remove(directory, name, held)
        os.close(held)


def test_filesystems_account_only_their_own_active_peaks(root, monkeypatch):
    requested = {1: (root, 10), 2: (root / "served", 20)}
    monkeypatch.setattr(capacity.os, "statvfs", lambda path: SimpleNamespace(f_bavail=100, f_frsize=1, f_bsize=1))
    assert capacity.fits(requested, {"3": capacity.MAX_BYTES})
    assert not capacity.fits(requested, {"1": 91})
    assert not capacity.fits(requested, {"2": 81})
    with pytest.raises(OSError) as caught:
        capacity.fits({1: (root, 101)}, {})
    assert caught.value.errno == errno.ENOSPC


def test_an_active_invalid_record_is_never_reclaimed_as_stale(root):
    with capacity.locked(root) as directory:
        name, held = capacity.create(directory, {str(root.stat().st_dev): 100})
    path = root / ".discovery-builds" / name
    path.write_bytes(b'{"1":true}')  # A corrupted live record is not free capacity.
    try:
        with pytest.raises(ValueError):
            with capacity.reserve(root, paths(root)):
                pytest.fail("invalid active reservation was ignored")
        assert path.read_bytes() == b'{"1":true}'
    finally:
        with capacity.locked(root) as directory:
            capacity.remove(directory, name, held)
        os.close(held)


def test_release_refuses_a_replaced_inode(root):
    with capacity.locked(root) as directory:
        name, held = capacity.create(directory, {str(root.stat().st_dev): 100})
    path = root / ".discovery-builds" / name
    path.unlink()
    path.write_bytes(b"foreign replacement")
    try:
        with capacity.locked(root) as directory:
            with pytest.raises(ValueError):
                capacity.remove(directory, name, held)
        assert path.read_bytes() == b"foreign replacement"
    finally:
        os.close(held)


def test_parallel_reservations_keep_the_existing_exclusive_construction_guard(root):
    held = os.open(root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        with capacity.reserve(root, paths(root)):
            with pytest.raises(BlockingIOError):
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
    finally:
        os.close(held)
