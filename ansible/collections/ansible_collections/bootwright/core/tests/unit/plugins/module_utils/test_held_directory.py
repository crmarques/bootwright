"""One held-directory walk: no link followed, every directory admitted, identities re-proved."""

import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import held_directory


@pytest.fixture(name="root")
def fixture_root(tmp_path, monkeypatch):
    """Walk beneath a private tree standing in for "/", owned by this user."""
    root = tmp_path / "root"
    root.mkdir(mode=0o755)
    original = os.open

    def open_file(path, flags, mode=0o777, *, dir_fd=None):
        if path == "/" and dir_fd is None:
            path = str(root)
        return original(path, flags, mode, dir_fd=dir_fd)

    monkeypatch.setattr(held_directory.os, "open", open_file)
    return root


OWNERS = (os.geteuid(),)


def test_an_existing_directory_is_held_by_descriptor(root):
    (root / "etc" / "ceph").mkdir(parents=True, mode=0o755)
    with held_directory.hold("/etc/ceph", OWNERS) as directory:
        info = os.fstat(directory)
    assert (info.st_dev, info.st_ino) == ((root / "etc" / "ceph").stat().st_dev, (root / "etc" / "ceph").stat().st_ino)


def test_a_link_on_the_way_is_never_followed(root, tmp_path):
    (tmp_path / "elsewhere" / "ceph").mkdir(parents=True)
    (root / "etc").symlink_to(tmp_path / "elsewhere")
    with pytest.raises(OSError):
        with held_directory.hold("/etc/ceph", OWNERS):
            pass


def test_a_link_the_follow_rule_admits_hands_back_its_resolved_path(root):
    (root / "usr" / "share").mkdir(parents=True)
    (root / "etc").mkdir()
    (root / "etc" / "back-ends").symlink_to("../usr/share")
    with pytest.raises(held_directory.Linked) as linked:
        with held_directory.hold("/etc/back-ends/x", OWNERS, follow=lambda entry: True):
            pass
    assert linked.value.path == "/usr/share"
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/etc/back-ends/x", OWNERS, follow=lambda entry: False):
            pass


@pytest.mark.parametrize("mode", [0o775, 0o757])
def test_a_group_or_other_writable_component_refuses(root, mode):
    (root / "var" / "lib").mkdir(parents=True)
    (root / "var").chmod(mode)
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/var/lib", OWNERS):
            pass


def test_the_root_itself_is_admitted(root):
    root.chmod(0o777)
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/", OWNERS):
            pass


def test_a_foreign_owner_refuses_and_the_last_component_may_admit_more(root):
    (root / "var").mkdir()
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/var", (os.geteuid() + 1,)):
            pass
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/var", (os.geteuid() + 1,), last=OWNERS):
            pass
    (root / "var").chmod(0o755)


def test_a_missing_component_refuses_unless_the_walk_creates(root):
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/var/lib/bootwright", OWNERS):
            pass
    with held_directory.hold("/var/lib/bootwright", OWNERS, create=0o700) as directory:
        assert directory is not None
    for path in (root / "var", root / "var" / "lib", root / "var" / "lib" / "bootwright"):
        assert stat.S_IMODE(path.stat().st_mode) & 0o077 == 0


def test_an_absent_last_component_yields_none_only_when_admitted(root):
    (root / "var").mkdir()
    with held_directory.hold("/var/ceph", OWNERS, absent=True) as directory:
        assert directory is None
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/missing/ceph", OWNERS, absent=True):
            pass


def test_a_component_replaced_during_the_hold_is_moved(root):
    (root / "var" / "ceph").mkdir(parents=True)
    with pytest.raises(held_directory.Moved):
        with held_directory.hold("/var/ceph", OWNERS, prove=True):
            (root / "var" / "ceph").rename(root / "var" / "old")
            (root / "var" / "ceph").mkdir()


def test_an_absent_component_that_appears_during_the_hold_is_moved(root):
    (root / "var").mkdir()
    with pytest.raises(held_directory.Moved):
        with held_directory.hold("/var/ceph", OWNERS, absent=True, prove=True):
            (root / "var" / "ceph").mkdir()


def test_the_final_proof_is_the_callers(root):
    (root / "srv").mkdir(mode=0o755)
    with pytest.raises(held_directory.Refused):
        with held_directory.hold("/srv", OWNERS, final=lambda info: stat.S_IMODE(info.st_mode) == 0o700):
            pass


@pytest.mark.parametrize("path", ["relative", "/a/../b", "/a/./b", "/a//b"])
def test_a_path_that_is_not_absolute_and_clean_refuses(root, path):
    with pytest.raises(held_directory.Refused):
        with held_directory.hold(path, OWNERS):
            pass


def test_every_descriptor_closes(root):
    (root / "a" / "b").mkdir(parents=True)
    before = set(os.listdir("/proc/self/fd"))
    with held_directory.hold("/a/b", OWNERS):
        pass
    with pytest.raises(RuntimeError):
        with held_directory.hold("/a/b", OWNERS):
            raise RuntimeError("fixture")
    assert set(os.listdir("/proc/self/fd")) == before
