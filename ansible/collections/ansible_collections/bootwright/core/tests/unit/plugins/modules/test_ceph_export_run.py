"""The ceph_export module's main() runs over a stubbed host transport, as a task would run it.

The native export is tested in test_ceph_export_native.py over fakes. These
cases hold the module's part: the material it admits, the private output it
writes, the evidence it returns without the credentials, and the refusal record.
"""

from __future__ import annotations

import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_export_native, ceph_native
from ansible_collections.bootwright.core.plugins.modules import ceph_export as module

DIGEST = "f" * 64
SCOPE = {"owner": "owner-1", "fsid": "fsid-1", "bootstrap": "ceph-0", "hosts": [{"name": "ceph-0"}]}
REQUEST = {"name": "share", "scope": SCOPE, "keyType": "aes", "principals": {"health": {"mon": "allow r"}}}
PRIVATE = json.dumps([{"name": "client", "kind": "cephx", "secret": "export-secret"}])


@pytest.fixture(name="host")
def stubbed_host(monkeypatch):
    """The bootstrap host's transport answering the export from `reply`, with the request validation passed through."""
    state = {"exports": [], "reply": {"changed": True, "private": PRIVATE, "leader": "ceph-0", "address": "10.0.0.5"}}

    class Transport:
        def __init__(self, request, material, **options):
            state["built"] = (request, material, options)

        def export(self, host, operation, request, private):
            state["exports"].append((host["name"], operation, private))
            if isinstance(state["reply"], Exception):
                raise state["reply"]  # pylint: disable=raising-bad-type
            return state["reply"]

    monkeypatch.setattr(ceph_core, "Transport", Transport)
    monkeypatch.setattr(ceph_export_native, "validate", lambda request: request)
    return state


def arguments(tmp_path, operation="apply"):
    return {"request": REQUEST, "operation": operation, "digest": DIGEST,
            "material": {"sshKey": "/run/lent/key", "knownHosts": "/run/lent/known_hosts"},
            "outputs": {"externalDetails": str(tmp_path / "external.json")}}


def test_an_applied_export_keeps_its_credentials_privately_and_returns_only_evidence(tmp_path, host, run_module):
    result = run_module(module, arguments(tmp_path))
    assert result["changed"] is True and not result.get("failed")
    evidence = result["evidence"]
    assert evidence["version"] == ceph_export_native.EVIDENCE_VERSION and evidence["ready"] is True and evidence["request"] == DIGEST
    assert (evidence["leader"], evidence["address"], evidence["fsid"], evidence["owner"]) == ("ceph-0", "10.0.0.5", "fsid-1", "owner-1")
    assert evidence["entries"] == {"client": "cephx"} and list(evidence["principals"]) == ["client." + ceph_export_native.prefix(REQUEST) + "-health"]
    assert "export-secret" not in json.dumps(result)
    kept = tmp_path / "external.json"
    assert kept.read_text(encoding="utf-8") == PRIVATE and stat.S_IMODE(os.stat(kept).st_mode) == 0o600
    assert host["exports"] == [("ceph-0", "export_apply", {})]
    assert host["built"][1] == {"sshKey": "/run/lent/key", "knownHosts": "/run/lent/known_hosts"}


def test_material_that_is_not_exactly_the_key_and_known_hosts_is_refused_before_the_host_is_reached(tmp_path, host, run_module):
    wrong = dict(arguments(tmp_path), material={"sshKey": "/run/lent/key"})
    result = run_module(module, wrong)
    assert result["failed"] is True and result["msg"] == "Ceph operation refused" and result["reason"] == "material"
    assert host["exports"] == [] and not (tmp_path / "external.json").exists()


def test_a_native_refusal_names_its_closed_reason_and_leaves_no_output(tmp_path, host, run_module):
    host["reply"] = ceph_native.Refusal("export-identity")
    result = run_module(module, arguments(tmp_path, "observe"))
    assert result["failed"] is True and result["reason"] == "export-identity"
    assert not (tmp_path / "external.json").exists()
    host["reply"] = {"changed": "yes", "private": PRIVATE, "leader": "ceph-0", "address": "10.0.0.5"}
    malformed = run_module(module, arguments(tmp_path))
    assert malformed["failed"] is True and malformed["reason"] == "evidence"
    assert not (tmp_path / "external.json").exists()
