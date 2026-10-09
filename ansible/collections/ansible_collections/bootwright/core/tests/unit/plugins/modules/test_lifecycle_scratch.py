"""Concurrent jobs own disjoint private files and cleanup follows no links."""

import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from threading import Barrier

import pytest

from ansible_collections.bootwright.core.plugins.modules import lifecycle_scratch as scratch


def call(parent, job, scope="managedos-install", state="present"):
    root = str(parent / ("bootwright-run-scratch-%d-1" % job))
    return Path(scratch.manage(root, scope, state, str(parent), os.getuid(), remove_root=True))


def test_two_installs_keep_their_own_connection_files(tmp_path):
    ready, removed = Barrier(2), Barrier(2)

    def install(job):
        directory = call(tmp_path, job)
        for name in ("known_hosts", "ssh_config"):
            (directory / name).write_text(str(job))
        assert directory.stat().st_mode & 0o777 == 0o700
        assert directory.parent.stat().st_mode & 0o777 == 0o700
        ready.wait(timeout=5)
        if job == 1:
            call(tmp_path, job, state="absent")
            removed.wait(timeout=5)
        else:
            removed.wait(timeout=5)
            assert (directory / "known_hosts").read_text() == "2"
            assert (directory / "ssh_config").read_text() == "2"
            call(tmp_path, job, state="absent")

    with ThreadPoolExecutor(max_workers=2) as pool:
        list(pool.map(install, (1, 2)))
    assert list(tmp_path.iterdir()) == []


def test_cleanup_keeps_another_role_and_unlinks_a_link_only(tmp_path):
    first = call(tmp_path, 1, "libvirt-host")
    other = call(tmp_path, 1, "libvirt-machine")
    (other / "domain.xml").write_text("other")
    target = tmp_path / "keep"
    target.write_text("untouched")
    (first / "network.xml").symlink_to(target)
    call(tmp_path, 1, "libvirt-host", "absent")
    assert target.read_text() == "untouched"
    assert (other / "domain.xml").read_text() == "other"
    call(tmp_path, 1, "libvirt-machine", "absent")
    call(tmp_path, 1, "libvirt-machine", "absent")


@pytest.mark.parametrize("change", ["root-link", "child-link", "root-public", "child-public", "nested", "too-many"])
def test_cleanup_refuses_unsafe_directories_without_removing_files(tmp_path, change):
    directory = call(tmp_path, 1)
    target = tmp_path / "keep"
    target.mkdir(mode=0o700)
    (target / "valuable").write_text("untouched")
    if change == "root-link":
        directory.rmdir()
        directory.parent.rmdir()
        directory.parent.symlink_to(target, target_is_directory=True)
    elif change == "child-link":
        directory.rmdir()
        directory.symlink_to(target, target_is_directory=True)
    elif change == "root-public":
        directory.parent.chmod(0o755)
    elif change == "child-public":
        directory.chmod(0o755)
    elif change == "nested":
        (directory / "nested").mkdir()
    else:
        for index in range(257):
            (directory / str(index)).write_text("keep")
    with pytest.raises((ValueError, OSError)):
        call(tmp_path, 1, state="absent")
    assert (target / "valuable").read_text() == "untouched"
    assert os.path.lexists(directory.parent if change == "root-link" else directory)


@pytest.mark.parametrize("root,scope", [
    ("bootwright-run-scratch-1-1", "../other"),
    ("bootwright-run-scratch-1-1/..", "libvirt-host"),
    ("bootwright-run-scratch-1", "libvirt-host"),
    ("other", "libvirt-host"),
])
def test_only_runner_names_and_known_scopes_are_accepted(tmp_path, root, scope):
    with pytest.raises(ValueError):
        scratch.manage(str(tmp_path / root), scope, "present", str(tmp_path), os.getuid())
    assert list(tmp_path.iterdir()) == []


def test_present_never_reuses_an_existing_scope(tmp_path):
    directory = call(tmp_path, 1)
    (directory / "known_hosts").write_text("first")
    with pytest.raises(FileExistsError):
        call(tmp_path, 1)
    assert (directory / "known_hosts").read_text() == "first"


def test_local_cleanup_preserves_the_live_runners_temporary_directory(tmp_path):
    root = tmp_path / "bootwright-run-scratch-1-1"
    scope = scratch.manage(str(root), "libvirt-host", "present", str(tmp_path), os.getuid())
    Path(scope, "network.xml").write_text("private")
    scratch.manage(str(root), "libvirt-host", "absent", str(tmp_path), os.getuid())
    assert root.is_dir() and not Path(scope).exists()
    (root / "later-task").write_text("still private")
