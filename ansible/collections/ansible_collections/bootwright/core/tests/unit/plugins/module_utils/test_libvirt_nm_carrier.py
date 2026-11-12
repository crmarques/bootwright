"""Exact persistent VLAN carrier policy, guarded publication and reload recovery."""

from __future__ import annotations

import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_nm_carrier as carrier

LEGS = [{"device": "br-lab.110", "uuid": "00000000-0000-8000-8000-000000000001"},
        {"device": "br-lab.150", "uuid": "00000000-0000-8000-8000-000000000002"}]


@pytest.fixture
def policy(tmp_path):
    root = tmp_path / "conf.d"
    root.mkdir(mode=0o700)
    calls = []

    def reconcile(legs=None, reload=None):
        legs = LEGS if legs is None else legs
        return carrier.reconcile("lab", "host", legs, reload or (lambda: calls.append("reload")), str(root), os.geteuid())

    def inspect(legs=None):
        legs = LEGS if legs is None else legs
        return carrier.inspect("lab", "host", legs, str(root), os.geteuid())

    return root, root / carrier.name("lab", "host"), calls, reconcile, inspect


def test_exact_policy_replays_and_dropped_last_leg_removes_it(policy):
    root, path, calls, reconcile, inspect = policy
    unrelated = root / "operator.conf"
    unrelated.write_text("# operator\n")
    assert reconcile()
    assert path.read_bytes() == carrier.content("lab", "host", LEGS)
    assert path.stat().st_mode & 0o777 == 0o644
    assert inspect()["definition"] and inspect()["owned"]
    assert not reconcile() and calls == ["reload"]
    assert reconcile(LEGS[:1])
    assert b"br-lab.150" not in path.read_bytes()
    assert reconcile([]) and not path.exists()
    assert inspect([])["definition"] and not inspect([])["present"]
    assert not reconcile([]) and len(calls) == 3
    assert unrelated.read_text() == "# operator\n"
    assert sorted(p.name for p in root.iterdir()) == ["operator.conf"]


@pytest.mark.parametrize("removing", [False, True])
def test_reload_failure_leaves_owned_pending_policy_and_retries_even_with_same_bytes(policy, removing):
    root, path, calls, reconcile, inspect = policy
    if removing:
        reconcile()
    desired = [] if removing else LEGS

    def failed():
        raise ValueError("reload failed")

    with pytest.raises(ValueError, match="reload failed"):
        reconcile(desired, failed)
    seen = inspect(desired)
    assert seen["answered"] and seen["present"] and seen["owned"] and not seen["definition"]
    assert carrier.PENDING.encode() in path.read_bytes()
    assert reconcile(desired)
    assert inspect(desired)["definition"]
    assert inspect(desired)["present"] is not removing
    assert not any(p.name.startswith(".bootwright-") for p in root.iterdir())


@pytest.mark.parametrize("damage", ["foreign", "wildcard", "extra", "symlink", "hardlink", "mode", "huge", "directory", "parent"])
def test_foreign_or_unknown_path_refuses_before_reload_and_preserves_it(policy, damage):
    root, path, calls, reconcile, inspect = policy
    path.write_bytes(carrier.content("lab", "host", LEGS))
    path.chmod(0o644)
    if damage == "foreign":
        path.write_text("[device]\nmatch-device=*\nignore-carrier=yes\n")
    elif damage == "wildcard":
        path.write_bytes(path.read_bytes().replace(b"br-lab.110", b"*"))
    elif damage == "extra":
        path.write_bytes(path.read_bytes() + b"managed=false\n")
    elif damage == "symlink":
        path.unlink()
        path.symlink_to(root / "missing")
    elif damage == "hardlink":
        os.link(path, root / "alias")
    elif damage == "mode":
        path.chmod(0o666)
    elif damage == "huge":
        path.write_bytes(b"x" * (carrier.MAX_BYTES + 1))
    elif damage == "directory":
        path.unlink()
        path.mkdir()
    else:
        root.chmod(0o777)
    before = path.lstat()
    for desired in (LEGS, []):
        with pytest.raises(ValueError, match="policy"):
            reconcile(desired)
    assert calls == [] and path.lstat() == before
    assert not inspect()["definition"]


def test_staging_failure_retains_original_and_removes_own_stage(policy, monkeypatch):
    root, path, calls, reconcile, inspect = policy
    reconcile()
    before = path.read_bytes()

    def failed(*args, **kwargs):
        raise OSError("rename failed")

    monkeypatch.setattr(carrier.os, "replace", failed)
    with pytest.raises(OSError, match="rename failed"):
        reconcile(LEGS[:1])
    assert path.read_bytes() == before and calls == ["reload"]
    assert not any(p.name.startswith(".bootwright-") for p in root.iterdir())


def test_wrong_owner_and_symlinked_parent_are_unknown(policy):
    root, path, calls, reconcile, inspect = policy
    reconcile()
    assert not carrier.inspect("lab", "host", LEGS, str(root), os.geteuid() + 1)["answered"]
    alias = root.parent / "alias"
    alias.symlink_to(root, target_is_directory=True)
    assert not carrier.inspect("lab", "host", LEGS, str(alias), os.geteuid())["answered"]


def test_final_publication_failure_preserves_reload_journal_for_retry(policy, monkeypatch):
    root, path, calls, reconcile, inspect = policy
    original = carrier.write

    def failed_final(descriptor, filename, data):
        if carrier.PENDING.encode() not in data:
            raise OSError("final publication failed")
        return original(descriptor, filename, data)

    monkeypatch.setattr(carrier, "write", failed_final)
    with pytest.raises(OSError, match="final publication failed"):
        reconcile()
    assert inspect()["owned"] and not inspect()["definition"] and calls == ["reload"]
    monkeypatch.setattr(carrier, "write", original)
    assert reconcile() and calls == ["reload", "reload"] and inspect()["definition"]


def test_unlink_failure_leaves_removal_tombstone_for_retry(policy, monkeypatch):
    root, path, calls, reconcile, inspect = policy
    reconcile()
    original = carrier.os.unlink

    def failed_unlink(filename, **kwargs):
        if filename == path.name:
            raise OSError("unlink failed")
        return original(filename, **kwargs)

    monkeypatch.setattr(carrier.os, "unlink", failed_unlink)
    with pytest.raises(OSError, match="unlink failed"):
        reconcile([])
    assert inspect([])["owned"] and not inspect([])["definition"]
    monkeypatch.setattr(carrier.os, "unlink", original)
    assert reconcile([]) and not path.exists() and calls == ["reload"] * 3
