"""The console update pins the original account and never creates a replacement."""

from __future__ import annotations

import base64
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_admin_credentials as credentials


@pytest.fixture
def account(monkeypatch):
    current = {"kind": "Secret", "metadata": {"name": "kubeadmin", "namespace": "kube-system", "uid": "original", "resourceVersion": "1",
                                              "creationTimestamp": "2026-01-01T00:05:00Z"},
               "data": {"kubeadmin": base64.b64encode(b"$2b$10$" + b"a" * 53).decode()}}
    namespace = {"metadata": {"uid": "system", "creationTimestamp": "2026-01-01T00:00:00Z"}}
    calls = []

    def run(client, kubeconfig, argv, deadline, body=None):
        assert client == "/pinned/oc" and kubeconfig == "/private/kubeconfig" and deadline > 0
        calls.append((argv, body))
        if argv[:2] == ["get", "secret"]:
            return 0, json.dumps(current), ""
        if argv[:2] == ["get", "namespace"]:
            return 0, json.dumps(namespace), ""
        assert argv == ["patch", "secret", "kubeadmin", "--namespace", "kube-system", "--type", "json", "--patch-file", "/dev/stdin"]
        patch = json.loads(body)
        assert patch[:3] == [{"op": "test", "path": "/metadata/uid", "value": current["metadata"]["uid"]},
                             {"op": "test", "path": "/metadata/resourceVersion", "value": current["metadata"]["resourceVersion"]},
                             {"op": "test", "path": "/data/kubeadmin", "value": current["data"]["kubeadmin"]}]
        current["data"]["kubeadmin"] = patch[-1]["value"]
        current["metadata"]["resourceVersion"] = "2"
        return 0, "", ""

    monkeypatch.setattr(credentials.native, "run", run)
    return current, namespace, calls


def candidate():
    return {"password": "synthetic-password-never-in-argv", "hash": "$2b$10$" + "b" * 53}


def test_existing_account_update_is_pinned_and_repeatable(account):
    current, unused, calls = account
    update = candidate()
    update["guard"] = credentials.read("/pinned/oc", "/private/kubeconfig")
    credentials.update("/pinned/oc", "/private/kubeconfig", update)
    credentials.update("/pinned/oc", "/private/kubeconfig", update)
    assert len([argv for argv, body in calls if argv[0] == "patch"]) == 1
    assert base64.b64decode(current["data"]["kubeadmin"]).decode() == update["hash"]
    assert all(update["password"] not in " ".join(argv) for argv, body in calls)


@pytest.mark.parametrize("change", ["uid", "version", "hash", "namespace", "deleted", "late"])
def test_changed_or_disabled_account_refuses_without_mutation(account, change):
    current, namespace, calls = account
    update = candidate()
    update["guard"] = credentials.read("/pinned/oc", "/private/kubeconfig")
    if change == "uid":
        current["metadata"]["uid"] = "replacement"
    elif change == "version":
        current["metadata"]["resourceVersion"] = "later"
    elif change == "hash":
        current["data"]["kubeadmin"] = base64.b64encode(b"$2b$10$" + b"c" * 53).decode()
    elif change == "namespace":
        namespace["metadata"]["uid"] = "other"
    elif change == "deleted":
        current["metadata"]["deletionTimestamp"] = "2026-01-01T01:00:00Z"
    else:
        current["metadata"]["creationTimestamp"] = "2026-01-02T00:00:00Z"
    before = copy.deepcopy(current)
    with pytest.raises(ValueError):
        credentials.update("/pinned/oc", "/private/kubeconfig", update)
    assert current == before and not any(argv[0] == "patch" for argv, body in calls)
