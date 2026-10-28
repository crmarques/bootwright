"""A file carrying lent material is published whole, private and only through its own path."""

import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.modules import private_file


def write(path, content, mode="0600"):
    return private_file.write(str(path), content, mode, os.getuid(), os.getgid())


def test_a_new_file_is_published_private_with_exactly_its_content(tmp_path):
    destination = tmp_path / "install-config.yaml"
    assert write(destination, "pullSecret: secret\n") is True
    assert destination.read_bytes() == b"pullSecret: secret\n"
    assert stat.S_IMODE(destination.stat().st_mode) == 0o600
    assert sorted(os.listdir(tmp_path)) == ["install-config.yaml"]


def test_a_replay_changes_nothing_and_a_difference_replaces_the_file(tmp_path):
    destination = tmp_path / "server.key"
    write(destination, "key")
    held = destination.stat().st_ino
    assert write(destination, "key") is False
    assert destination.stat().st_ino == held
    assert write(destination, "other key") is True
    assert destination.read_text() == "other key"
    destination.chmod(0o644)
    assert write(destination, "other key") is True
    assert stat.S_IMODE(destination.stat().st_mode) == 0o600
    assert write(destination, "other key", "0644") is True
    assert stat.S_IMODE(destination.stat().st_mode) == 0o644


def test_a_longer_file_holding_the_content_as_a_prefix_is_replaced(tmp_path):
    destination = tmp_path / "htpasswd"
    destination.write_text("admin:hash-and-more")
    destination.chmod(0o600)
    assert write(destination, "admin:hash") is True
    assert destination.read_text() == "admin:hash"


def test_a_link_at_the_destination_refuses_and_its_target_is_untouched(tmp_path):
    target = tmp_path / "elsewhere"
    target.write_text("untouched")
    (tmp_path / "tls.key").symlink_to(target)
    with pytest.raises(ValueError, match="symbolic link"):
        write(tmp_path / "tls.key", "key")
    assert target.read_text() == "untouched"
    assert sorted(os.listdir(tmp_path)) == ["elsewhere", "tls.key"]


def test_a_directory_at_the_destination_refuses(tmp_path):
    (tmp_path / "ks.cfg").mkdir()
    with pytest.raises(ValueError):
        write(tmp_path / "ks.cfg", "kickstart")
    assert sorted(os.listdir(tmp_path)) == ["ks.cfg"]


def test_a_failed_publication_removes_its_own_stage_and_keeps_the_old_file(tmp_path, monkeypatch):
    destination = tmp_path / "install-config.yaml"
    write(destination, "old")

    def refused(*_arguments, **_options):
        raise OSError("refused")

    monkeypatch.setattr(private_file.os, "rename", refused)
    with pytest.raises(OSError):
        write(destination, "new")
    assert destination.read_text() == "old"
    assert sorted(os.listdir(tmp_path)) == ["install-config.yaml"]


@pytest.mark.parametrize("path", ["relative/file", "/var/tmp/../etc/file", "/var/tmp/dir/"])
def test_a_destination_that_is_not_a_normalized_absolute_path_refuses(path):
    with pytest.raises(ValueError):
        private_file.write(path, "content")


def test_an_unlisted_mode_refuses(tmp_path):
    with pytest.raises(ValueError):
        write(tmp_path / "file", "content", "0666")
    assert os.listdir(tmp_path) == []


def test_a_failed_cleanup_never_hides_the_failure_that_left_the_stage(tmp_path, monkeypatch):
    destination = tmp_path / "install-config.yaml"
    write(destination, "old")

    def refused(*_arguments, **_options):
        raise OSError("the original refusal")

    def unremovable(*_arguments, **_options):
        raise PermissionError("cleanup refused")

    monkeypatch.setattr(private_file.os, "rename", refused)
    monkeypatch.setattr(private_file.os, "unlink", unremovable)
    with pytest.raises(OSError, match="the original refusal"):
        write(destination, "new")
    assert destination.read_text() == "old"


def test_stages_an_interrupted_run_left_are_swept_and_nothing_else(tmp_path):
    destination = tmp_path / "server.key"
    stale = tmp_path / ".server.key.0123456789abcdef"
    stale.write_text("material an earlier run left")
    kept = [tmp_path / ".server.key.0123456789ABCDEF", tmp_path / ".other.key.0123456789abcdef", tmp_path / ".server.key.0123"]
    for path in kept:
        path.write_text("not a stage of this destination")
    linked = tmp_path / ".server.key.fedcba9876543210"
    linked.symlink_to(tmp_path / "elsewhere")
    assert write(destination, "key") is True
    assert not stale.exists() and all(path.exists() for path in kept) and os.path.islink(linked)
    assert destination.read_text() == "key"
