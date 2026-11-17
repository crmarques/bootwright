"""One private reader and writer: owner-only, no link, bounded, and nothing left behind on a refused write."""

import os
import stat
from unittest import mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import private_files


def test_a_written_file_is_owner_only_and_reads_back(tmp_path):
    path = tmp_path / "auth.json"
    private_files.write(str(path), '{"auths":{}}')
    info = path.lstat()
    assert stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.geteuid()
    assert private_files.read(str(path), 64) == b'{"auths":{}}'


def test_an_owner_only_executable_mode_is_kept_exactly(tmp_path):
    path = tmp_path / "oc"
    old = os.umask(0o077)
    try:
        private_files.write(str(path), b"binary", 0o700)
    finally:
        os.umask(old)
    assert stat.S_IMODE(path.lstat().st_mode) == 0o700


@pytest.mark.parametrize("mode", [0o640, 0o604, 0o2600])
def test_a_mode_beyond_the_owner_refuses_before_any_name(tmp_path, mode):
    with pytest.raises(private_files.Refused):
        private_files.write(str(tmp_path / "file"), b"data", mode)
    assert list(tmp_path.iterdir()) == []


def test_an_existing_name_is_never_replaced(tmp_path):
    path = tmp_path / "file"
    path.write_bytes(b"theirs")
    with pytest.raises(FileExistsError):
        private_files.write(str(path), b"ours")
    assert path.read_bytes() == b"theirs"


def test_a_link_is_never_followed_on_write(tmp_path):
    target = tmp_path / "target"
    (tmp_path / "link").symlink_to(target)
    with pytest.raises(FileExistsError):
        private_files.write(str(tmp_path / "link"), b"data")
    assert not target.exists()


def test_a_failed_write_removes_the_name_it_created(tmp_path):
    path = tmp_path / "file"
    with mock.patch.object(private_files.os, "fsync", side_effect=OSError("fixture")):
        with pytest.raises(OSError):
            private_files.write(str(path), b"data")
    assert list(tmp_path.iterdir()) == []


def test_a_failed_post_create_proof_removes_the_name_it_created(tmp_path):
    path = tmp_path / "file"
    original = os.fstat

    def linked(descriptor):
        info = original(descriptor)
        return os.stat_result((info.st_mode, info.st_ino, info.st_dev, 2) + tuple(info)[4:])

    with mock.patch.object(private_files.os, "fstat", side_effect=linked):
        with pytest.raises(private_files.Refused):
            private_files.write(str(path), b"data")
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("mode", [0o640, 0o604, 0o660])
def test_a_group_or_other_bit_refuses_the_read(tmp_path, mode):
    path = tmp_path / "file"
    path.write_bytes(b"data")
    path.chmod(mode)
    with pytest.raises(private_files.Refused):
        private_files.read(str(path), 64)


def test_an_owner_read_only_file_reads(tmp_path):
    path = tmp_path / "file"
    path.write_bytes(b"data")
    path.chmod(0o400)
    assert private_files.read(str(path), 64) == b"data"


def test_a_link_is_never_followed_on_read(tmp_path):
    target = tmp_path / "target"
    target.write_bytes(b"data")
    target.chmod(0o600)
    (tmp_path / "link").symlink_to(target)
    with pytest.raises(OSError):
        private_files.read(str(tmp_path / "link"), 64)


def test_a_second_hard_link_refuses_the_read(tmp_path):
    path = tmp_path / "file"
    path.write_bytes(b"data")
    path.chmod(0o600)
    os.link(path, tmp_path / "second")
    with pytest.raises(private_files.Refused):
        private_files.read(str(path), 64)


def test_a_file_of_another_owner_refuses_the_read(tmp_path):
    path = tmp_path / "file"
    path.write_bytes(b"data")
    path.chmod(0o600)
    with pytest.raises(private_files.Refused):
        private_files.read(str(path), 64, owner=os.geteuid() + 1)


def test_a_file_beyond_its_bound_refuses_the_read(tmp_path):
    path = tmp_path / "file"
    path.write_bytes(b"x" * 65)
    path.chmod(0o600)
    with pytest.raises(private_files.Refused):
        private_files.read(str(path), 64)


def test_a_fifo_refuses_without_blocking(tmp_path):
    path = tmp_path / "fifo"
    os.mkfifo(path, 0o600)
    with pytest.raises(private_files.Refused):
        private_files.read(str(path), 64)


def test_a_created_file_another_user_owns_refuses_and_is_removed(tmp_path):
    path = tmp_path / "file"
    with mock.patch.object(private_files.os, "geteuid", return_value=os.getuid() + 1):
        with pytest.raises(private_files.Refused):
            private_files.write(str(path), b"data")
    assert list(tmp_path.iterdir()) == []
