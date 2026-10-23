"""Hub package readiness, dependent approval and private bootstrap continuation."""
from __future__ import annotations

import base64
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused


def request(obj):
    return {"program": {"version": model.VERSION, "steps": [{"kind": "create", "create": obj}], "checks": []},
            "operation": "operation", "block": "hub", "registered": "2026-01-01T00:00:00Z", "receipts": [], "deadline": 100}


def object_entry(kind, name, namespace="openshift-gitops"):
    body = {"apiVersion": "argoproj.io/v1alpha1", "kind": kind,
            "metadata": {"name": name, "namespace": namespace}, "spec": {"project": "default"}}
    return {"apiVersion": "argoproj.io/v1alpha1", "kind": kind, "name": name, "namespace": namespace,
            "namespaced": True, "body": body, "desiredDigest": "d" * 64}


def owned(req, obj):
    live = copy.deepcopy(obj["body"])
    live["metadata"].update(uid="owned", creationTimestamp="2026-01-01T00:00:01Z",
                            annotations={model.OWNER: model.mark(req["operation"], req["block"], obj)})
    return live


@pytest.mark.parametrize("foreign", [False, True])
def test_bootstrap_continuation_ignores_argocd_operation_and_status(foreign):
    obj = object_entry("Application", "bootwright-bootstrap")
    req = request(obj)
    live = owned(req, obj)
    live.update(operation={"sync": {"revision": "commit"}}, status={"sync": {"status": "Synced"}})
    if foreign:
        live["metadata"]["annotations"] = {}
    writes = []
    engine = Engine(req, lambda *_: {"answered": True, "object": live}, lambda *args: writes.append(args), clock=lambda: 0)
    if foreign:
        with pytest.raises(Refused, match="foreign"):
            engine.execute()
    else:
        result = engine.execute()
        assert result["state"]["postcondition"] and not result["changed"]
    assert not writes


@pytest.mark.parametrize("fault", ["", "extra-csv", "automatic", "wrong-csv", "approved"])
def test_mce_dependent_plan_identity_and_continuation(fault):
    hub = object_entry("MultiClusterHub", "multiclusterhub", "open-cluster-management")
    req = request(hub)
    creator = owned(req, hub)
    desired = {"namespace": "multicluster-engine", "subscription": "multicluster-engine", "package": "multicluster-engine",
               "channel": "stable", "source": "catalog", "sourceNamespace": "openshift-marketplace", "csv": "engine.v1", "creator": model.ref(hub)}
    subscription = {"metadata": {"uid": "mce-sub", "creationTimestamp": "2026-01-01T00:00:02Z"},
                    "spec": {"name": "multicluster-engine", "channel": "stable", "source": "catalog", "sourceNamespace": "openshift-marketplace",
                             "startingCSV": "engine.v1", "installPlanApproval": "Manual"}}
    plan = {"metadata": {"uid": "plan", "ownerReferences": [{"kind": "Subscription", "uid": "mce-sub"}]},
            "spec": {"approval": "Manual", "approved": fault == "approved", "clusterServiceVersionNames": ["engine.v1"]},
            "status": {"bundleLookups": [{"catalogSourceRef": {"name": "catalog", "namespace": "openshift-marketplace"}}]}}
    if fault == "extra-csv":
        plan["spec"]["clusterServiceVersionNames"].append("foreign.v1")
    elif fault == "automatic":
        subscription["spec"]["installPlanApproval"] = "Automatic"
    elif fault == "wrong-csv":
        subscription["spec"]["startingCSV"] = "foreign.v1"
    writes = []

    def read(obj, ignored):
        return {"answered": True, "object": creator if obj["kind"] == "MultiClusterHub" else plan if obj["kind"] == "InstallPlan" else subscription}

    engine = Engine(req, read, lambda *args: writes.append(args) or {"accepted": True}, clock=lambda: 0)
    if fault in ("automatic", "wrong-csv"):
        with pytest.raises(Refused, match="differs"):
            engine.dependent(desired)
    else:
        live = engine.dependent(desired)
        sub = dict(model.ref(hub), kind="Subscription", name="multicluster-engine", namespace="multicluster-engine")
        if fault == "extra-csv":
            with pytest.raises(Refused, match="approval proof"):
                engine.plan(sub, live, "engine.v1", "plan", False)
        else:
            assert engine.plan(sub, live, "engine.v1", "plan", False)["uid"] == "plan"
    assert len(writes) == (1 if not fault else 0)


def test_gitops_running_server_does_not_make_pending_instance_ready():
    obj = object_entry("ArgoCD", "openshift-gitops")
    req = request(obj)
    req["program"]["steps"] = []
    req["program"]["checks"] = [{"statusPhase": dict(model.ref(obj), path=path, values=[value])}
                                for path, value in (("status.server", "Running"), ("status.phase", "Available"))]
    live = {"metadata": {"uid": "argo"}, "status": {"server": "Running", "phase": "Pending", "host": ""}}
    result = Engine(req, lambda *_: {"answered": True, "object": live}, clock=lambda: 0).execute(observe=True)
    checks = result["state"]["checks"]
    assert checks[0]["satisfied"] and not checks[1]["satisfied"]
    assert not result["state"]["postcondition"]


def test_repository_secret_stdin_and_key_only_difference(tmp_path, monkeypatch):
    paths = {}
    for part, value in (("username", "repository-user"), ("password", "private-password-marker")):
        path = tmp_path / part
        path.write_text(value)
        paths[part] = str(path)
    placement = {"secret": "login", "namespace": "openshift-gitops", "name": "bootwright-bootstrap-repository", "type": "Opaque",
                 "keys": [{"key": "username", "part": "username"}, {"key": "password", "part": "password"}],
                 "labels": {"argocd.argoproj.io/secret-type": "repository"}, "strings": {"type": "git", "url": "https://example.test/repo"}}
    obj = dict(model.ref(object_entry("Secret", placement["name"])), apiVersion="v1")
    req = request(object_entry("Application", "bootwright-bootstrap"))
    owner = model.mark(req["operation"], req["block"], obj)
    body = secrets.desired(placement, {"login": paths}, owner)
    live = copy.deepcopy(body)
    live["metadata"].update(uid="secret", creationTimestamp="2026-01-01T00:00:01Z")
    answers = iter([None, live])
    monkeypatch.setattr(secrets, "read_private", lambda _: next(answers))
    calls = []
    monkeypatch.setattr(secrets.native, "run", lambda *args: calls.append(args) or (0, b"", b""))
    params = {"object": obj, "placement": placement, "materials": {"login": paths}, "owner": owner,
              "context": req, "oc": "oc", "kubeconfig": "kubeconfig", "deadline": 100, "manager": "manager"}
    result = secrets.place(params)
    assert result["accepted"] and len(calls) == 1
    argv, stdin = calls[0][2], calls[0][-1]
    assert "--filename=-" in argv and "private-password-marker" not in str(argv)
    assert json.loads(stdin)["data"]["password"] == base64.b64encode(b"private-password-marker").decode()
    assert json.loads(stdin)["metadata"]["labels"] == placement["labels"]
    live["data"]["password"] = base64.b64encode(b"different").decode()
    monkeypatch.setattr(secrets, "read_private", lambda _: live)
    difference = secrets.place(params)
    assert difference == {"accepted": False, "difference": "data.password"}
    assert "private-password-marker" not in json.dumps([result, difference])
    assert len(calls) == 1
