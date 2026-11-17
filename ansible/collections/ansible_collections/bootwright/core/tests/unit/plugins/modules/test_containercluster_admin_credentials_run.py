"""The containercluster_admin_credentials module's main() runs over private files in a temporary directory, as a task would run it."""

from __future__ import annotations

import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_admin_credentials as account
from ansible_collections.bootwright.core.plugins.modules import containercluster_admin_credentials as module

GUARD = {"namespaceUID": "ns-1", "uid": "uid-1", "resourceVersion": "7", "hash": "aGFzaA=="}
FAILURE = "The existing console administrator credential could not be proved or updated"


@pytest.fixture(name="calls")
def stubbed_cluster(monkeypatch):
    """The cluster's bootstrap account, read and updated through oc: replaced by what it would answer, and recorded."""
    calls = []

    def read(client, kubeconfig):
        calls.append(("read", client, kubeconfig))
        return dict(GUARD)

    def update(client, kubeconfig, candidate):
        calls.append(("update", client, kubeconfig, candidate))

    monkeypatch.setattr(account, "read", read)
    monkeypatch.setattr(account, "update", update)
    return calls


def private(path, content):
    path.write_text(content, encoding="utf-8")
    os.chmod(path, 0o600)
    return str(path)


def test_prepare_writes_a_private_candidate_pinned_to_the_account_it_read(tmp_path, calls, run_module):
    output = tmp_path / "candidate.json"
    result = run_module(module, {"operation": "prepare", "client": "/usr/local/bin/oc", "kubeconfig": "/run/lent/kubeconfig", "output": str(output)})
    assert result["changed"] is False and result["proved"] is True
    candidate = json.loads(output.read_text(encoding="utf-8"))
    assert candidate["guard"] == GUARD and len(candidate["password"]) >= 24
    assert stat.S_IMODE(os.stat(output).st_mode) == 0o600
    assert calls == [("read", "/usr/local/bin/oc", "/run/lent/kubeconfig")]
    assert candidate["password"] not in json.dumps(result)


def test_update_hands_the_retained_candidate_to_the_cluster_and_reports_a_change(tmp_path, calls, run_module):
    candidate = {"guard": GUARD, "hash": "bmV3", "password": "retained"}
    retained = private(tmp_path / "update.json", json.dumps(candidate))
    result = run_module(module, {"operation": "update", "client": "/usr/local/bin/oc", "kubeconfig": "/run/lent/kubeconfig", "update": retained})
    assert result["changed"] is True and result["proved"] is True
    assert calls == [("update", "/usr/local/bin/oc", "/run/lent/kubeconfig", candidate)]


def test_a_candidate_file_any_other_account_may_read_and_a_cluster_that_refuses_fail_with_one_message(tmp_path, calls, monkeypatch, run_module):
    open_file = tmp_path / "open.json"
    open_file.write_text(json.dumps({"guard": GUARD, "hash": "bmV3"}), encoding="utf-8")
    os.chmod(open_file, 0o644)
    arguments = {"operation": "update", "client": "/usr/local/bin/oc", "kubeconfig": "/run/lent/kubeconfig", "update": str(open_file)}
    refused = run_module(module, arguments)
    assert refused["failed"] is True and refused["msg"] == FAILURE
    assert calls == []

    def unavailable(client, kubeconfig):
        raise ValueError("original account unavailable")

    monkeypatch.setattr(account, "read", unavailable)
    unreadable = run_module(module, {"operation": "prepare", "client": "/usr/local/bin/oc", "kubeconfig": "/run/lent/kubeconfig",
                                     "output": str(tmp_path / "never.json")})
    assert unreadable["failed"] is True and unreadable["msg"] == FAILURE
    assert not (tmp_path / "never.json").exists()
