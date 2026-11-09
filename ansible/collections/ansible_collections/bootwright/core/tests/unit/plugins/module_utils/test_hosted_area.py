"""Hosted directories are private to root, created top-down, and removed only when empty up to the context's."""

from __future__ import annotations

import errno
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import hosted_area

OWNER = os.getuid()


def root_of(tmp_path):
    (tmp_path / "lib").mkdir()
    return tmp_path / "lib" / "bootwright-hosted"


def mode(path):
    return stat.S_IMODE(os.lstat(path).st_mode)


def test_creating_a_content_area_leaves_every_ancestor_private_whatever_the_umask(tmp_path):
    root = root_of(tmp_path)
    previous = os.umask(0o277)
    try:
        assert hosted_area.ensure("lab", ["services", "dns"], str(root), OWNER) is True
    finally:
        os.umask(previous)
    for path in (root, root / "lab", root / "lab" / "services", root / "lab" / "services" / "dns"):
        assert mode(path) == 0o700, path
    assert hosted_area.ensure("lab", ["services", "dns"], str(root), OWNER) is False
    assert hosted_area.prove("lab", ["services", "dns"], str(root), OWNER) is True


def plant(root, index, unsafe, foreign):
    ancestors = [root, root / "lab", root / "lab" / "machines", root / "lab" / "machines" / "node"]
    for directory in ancestors[:index]:
        directory.mkdir(mode=0o700)
        directory.chmod(0o700)
    bad = ancestors[index]
    if unsafe == "link":
        bad.symlink_to(foreign, target_is_directory=True)
    elif unsafe == "file":
        bad.write_text("not a directory")
    else:
        bad.mkdir(mode=0o700)
        bad.chmod(0o755 if unsafe == "mode" else 0o700)
    return bad


@pytest.mark.parametrize("operation", ["ensure", "prove", "remove"])
@pytest.mark.parametrize("index", [0, 1, 2, 3])
@pytest.mark.parametrize("unsafe", ["link", "mode", "owner", "file"])
def test_an_unsafe_existing_ancestor_refuses_before_any_effect(tmp_path, monkeypatch, operation, index, unsafe):
    root = root_of(tmp_path)
    foreign = tmp_path / "foreign"
    foreign.mkdir(mode=0o700)
    (foreign / "keep").write_text("untouched")
    bad = plant(root, index, unsafe, foreign)
    if unsafe == "owner":
        planted = os.lstat(bad).st_ino
        real = os.fstat

        def foreign_owner(descriptor):
            info = real(descriptor)
            if info.st_ino != planted:
                return info
            fields = list(info)
            fields[stat.ST_UID] = OWNER + 1
            return os.stat_result(fields)

        monkeypatch.setattr(hosted_area.os, "fstat", foreign_owner)
    with pytest.raises(hosted_area.Refusal, match="only root may enter"):
        getattr(hosted_area, operation)("lab", ["machines", "node", "install"], str(root), OWNER)
    assert sorted(path.name for path in foreign.iterdir()) == ["keep"]
    if unsafe in ("mode", "owner"):
        assert sorted(path.name for path in bad.iterdir()) == []
        assert mode(bad) == (0o755 if unsafe == "mode" else 0o700)


def test_a_proof_or_removal_of_an_absent_area_creates_nothing(tmp_path):
    root = root_of(tmp_path)
    assert hosted_area.prove("lab", ["clusters", "sno"], str(root), OWNER) is False
    assert hosted_area.remove("lab", ["clusters", "sno"], str(root), OWNER) is False
    assert list((tmp_path / "lib").iterdir()) == []


def test_removal_reaches_the_context_keeps_the_root_and_stops_at_a_sibling(tmp_path):
    root = root_of(tmp_path)
    hosted_area.ensure("lab", ["machines", "a", "install"], str(root), OWNER)
    hosted_area.ensure("lab", ["machines", "b", "bmc"], str(root), OWNER)
    (root / "lab" / "machines" / "b" / "bmc" / "conf.py").write_text("kept")
    assert hosted_area.remove("lab", ["machines", "a", "install"], str(root), OWNER) is True
    assert not os.path.lexists(root / "lab" / "machines" / "a")
    assert hosted_area.remove("lab", ["machines", "b", "bmc"], str(root), OWNER) is False
    assert (root / "lab" / "machines" / "b" / "bmc" / "conf.py").read_text() == "kept"
    (root / "lab" / "machines" / "b" / "bmc" / "conf.py").unlink()
    assert hosted_area.remove("lab", ["machines", "b", "bmc"], str(root), OWNER) is True
    assert list(root.iterdir()) == []
    assert mode(root) == 0o700


def test_removal_takes_the_empty_parents_of_an_area_already_gone(tmp_path):
    root = root_of(tmp_path)
    hosted_area.ensure("lab", ["services"], str(root), OWNER)
    assert hosted_area.remove("lab", ["services", "dns"], str(root), OWNER) is True
    assert list(root.iterdir()) == []


def test_a_directory_a_sibling_removed_first_is_already_absent(tmp_path, monkeypatch):
    root = root_of(tmp_path)
    hosted_area.ensure("lab", ["machines", "a"], str(root), OWNER)
    real = os.rmdir

    def raced(name, dir_fd=None):
        if name == "a":
            real(name, dir_fd=dir_fd)
            raise FileNotFoundError(errno.ENOENT, "removed by a sibling")
        return real(name, dir_fd=dir_fd)

    monkeypatch.setattr(hosted_area.os, "rmdir", raced)
    assert hosted_area.remove("lab", ["machines", "a"], str(root), OWNER) is True
    assert list(root.iterdir()) == []


def test_any_other_removal_failure_still_fails(tmp_path, monkeypatch):
    root = root_of(tmp_path)
    hosted_area.ensure("lab", ["clusters", "sno"], str(root), OWNER)

    def denied(name, dir_fd=None):
        raise PermissionError(errno.EACCES, "denied")

    monkeypatch.setattr(hosted_area.os, "rmdir", denied)
    with pytest.raises(PermissionError):
        hosted_area.remove("lab", ["clusters", "sno"], str(root), OWNER)


@pytest.mark.parametrize("context, path", [
    ("lab", []),
    ("lab", ["logs", "x"]),
    ("lab", ["machines", "a", "b", "c", "d"]),
    ("..", ["services", "dns"]),
    ("lab", ["services", ".."]),
    ("lab", ["services", ""]),
    ("lab", ["services", "dns/../x"]),
    ("Lab", ["services", "dns"]),
    ("lab", ["services", "-dns"]),
])
def test_a_path_out_of_normal_form_refuses_before_any_access(tmp_path, context, path):
    root = root_of(tmp_path)
    for operation in (hosted_area.ensure, hosted_area.prove, hosted_area.remove):
        with pytest.raises(hosted_area.Refusal, match="normal form"):
            operation(context, path, str(root), OWNER)
    assert list((tmp_path / "lib").iterdir()) == []
