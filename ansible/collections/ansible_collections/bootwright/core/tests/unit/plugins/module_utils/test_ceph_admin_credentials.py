"""Credential metadata replacement is bound to expected bytes and a private regular file."""

from __future__ import annotations

import contextlib
import json
import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_admin_credentials_native as credentials, ceph_native


@pytest.mark.parametrize("case", ["valid", "changed", "symlink", "hardlink"])
def test_replace_record_is_private_and_conditional(tmp_path, monkeypatch, case):
    path = tmp_path / "id.json"
    path.write_text(json.dumps({"old": True}))
    path.chmod(0o600)
    original = os.fstat

    def fstat(fd):
        fields = {name: getattr(original(fd), name) for name in dir(original(fd)) if name.startswith("st_")}
        fields.update(st_uid=0, st_gid=0)
        return SimpleNamespace(**fields)

    @contextlib.contextmanager
    def directory():
        descriptor = os.open(tmp_path, os.O_RDONLY | os.O_DIRECTORY)
        try:
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(os, "fstat", fstat)
    store = SimpleNamespace(directory=directory, metadata=lambda info, complete=False: dict(ceph_native.Custody.metadata(info, complete), uid=0, gid=0))
    if case == "changed":
        path.write_text(json.dumps({"foreign": True}))
    elif case == "symlink":
        path.rename(tmp_path / "foreign")
        path.symlink_to(tmp_path / "foreign")
    elif case == "hardlink":
        os.link(path, tmp_path / "foreign")
    if case == "valid":
        credentials.replace_record(store, "id.json", {"old": True}, {"new": True})
        assert json.loads(path.read_text()) == {"new": True}
    else:
        before = path.read_bytes()
        with pytest.raises((ceph_native.Refusal, OSError)):
            credentials.replace_record(store, "id.json", {"old": True}, {"new": True})
        assert path.read_bytes() == before
    assert not list(tmp_path.glob(".credential-*"))


def test_dispatch_refuses_before_native_calls_when_tool_proof_fails(monkeypatch):
    class Worker:
        request = {"action": "cluster"}

        def __init__(self, request, host):
            assert request == {} and host == "bootstrap"

        def verify_tool(self):
            raise ceph_native.Refusal("cache-unproved")

        def dashboard_scope(self):
            pytest.fail("scope called before tool proof")

        def monmap_incarnation(self):
            pytest.fail("native command called before tool proof")

    monkeypatch.setattr(ceph_native, "Worker", Worker)
    with pytest.raises(ceph_native.Refusal, match="cache-unproved"):
        credentials.dispatch({"request": {}, "host": "bootstrap", "phase": "admin_prepare", "private": {"incarnation": "original"}})
