"""Go-rendered FDF operands across initial approval, private placement and replay."""
from __future__ import annotations

import base64
import copy
import json
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import ScriptedOC, response


@pytest.fixture
def fdf_request():
    # Copied from internal/addons/packages/testdata/fdf-gateway.golden: preserve
    # the producer's nullable keys and exact CRD gate, operand and readiness set.
    program = json.loads((Path(__file__).parent / "goldens/fdf-gateway.json").read_text())
    body = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription",
            "metadata": {"namespace": "openshift-storage", "name": "odf-operator"},
            "spec": {"name": "odf-operator", "channel": "stable-4.21", "source": "fdf-catalog",
                     "sourceNamespace": "openshift-marketplace", "startingCSV": "odf-operator.v4.21.0", "installPlanApproval": "Manual"}}
    obj = {"apiVersion": body["apiVersion"], "kind": body["kind"], "namespace": "openshift-storage", "name": "odf-operator",
           "namespaced": True, "body": body, "desiredDigest": "d" * 64}
    program["steps"].insert(0, {"kind": "subscribe", "subscribe": {"object": obj, "csv": body["spec"]["startingCSV"], "approval": "operatorManaged"}})
    return {"program": program, "operation": "apply-test", "block": "fdf", "registered": "2026-01-01T00:00:00Z",
            "receipts": [], "deadline": 100}


class FDFCluster:
    def __init__(self, request):
        self.request = request
        self.live = {}
        self.writes = []
        self.reads = []
        self.crd_reads = self.noobaa_reads = 0
        self.noobaa_ready = False
        self.subscription = request["program"]["steps"][0]["subscribe"]["object"]
        self.csv = self.subscription["body"]["spec"]["startingCSV"]
        self.plan = {"metadata": {"name": "initial", "uid": "initial-uid", "ownerReferences": [{"kind": "Subscription", "uid": "Subscription-uid"}]},
                     "spec": {"approval": "Manual", "approved": False, "clusterServiceVersionNames": [self.csv]},
                     "status": {"bundleLookups": [{"catalogSourceRef": {"name": "fdf-catalog", "namespace": "openshift-marketplace"},
                                                   "path": "registry.example.test/odf-bundle:v4.21.0"}]}}
        self.later = {"metadata": {"name": "dependency", "uid": "dependency-uid", "ownerReferences": [{"kind": "Subscription", "uid": "operator-created"}]},
                      "spec": {"approval": "Manual", "approved": False, "clusterServiceVersionNames": ["dependency.v1", "other.v1"]}}

    def owned(self, obj, body):
        value = copy.deepcopy(body)
        value["metadata"].update(uid=obj["kind"] + "-uid", creationTimestamp="2026-01-01T00:00:01Z", generation=1)
        value["metadata"].setdefault("annotations", {})[model.OWNER] = model.mark(self.request["operation"], self.request["block"], obj)
        return value

    def read(self, obj, fields):
        self.reads.append((obj["kind"], fields))
        kind = obj["kind"]
        value = self.live.get(model.identity(obj))
        if kind == "InstallPlan":
            value = {"items": [self.later, self.plan]} if not obj["name"] else self.plan
        elif kind == "ClusterServiceVersion":
            csv = {"metadata": {"name": self.csv, "uid": "csv-uid"}, "spec": {"version": "4.21.0"}, "status": {"phase": "Succeeded"}}
            value = {"items": [csv]} if not obj["name"] else csv
        elif kind == "CatalogSource":
            value = {"metadata": {"uid": "catalog-uid"}, "spec": {"image": "registry.example.test/catalog:v4.21"}}
        elif kind == "CustomResourceDefinition":
            self.crd_reads += 1
            value = None if self.crd_reads == 1 else {"metadata": {"uid": "crd"}, "status": {"conditions": [{"type": "Established", "status": "True"}]}}
        elif kind == "NooBaa":
            self.noobaa_reads += 1
            value = {"metadata": {"uid": "noobaa"}, "status": {"phase": "Ready" if self.noobaa_ready else "Configuring"}}
        elif kind == "StorageClass":
            value = {"metadata": {"name": obj["name"], "uid": "class-uid"}}
        elif kind == "Secret" and value:
            value = {key: copy.deepcopy(value[key]) for key in ("apiVersion", "kind", "metadata", "type")}
            value.update(dataKeys=["external_cluster_details"], stringsEqual={})
        return {"answered": True, "object": copy.deepcopy(value)}

    def write(self, action, obj, extra):
        self.writes.append((action, obj["kind"], obj["name"]))
        if action == "approve":
            assert obj["name"] == "initial"
            self.plan["spec"]["approved"] = True
            subscription = self.live[model.identity(self.subscription)]
            subscription["status"] = {"installedCSV": self.csv, "currentCSV": self.csv, "installPlanRef": {"name": "dependency"}}
            return {"accepted": True}
        if action == "place":
            return secrets.place(dict(extra, object=obj, oc="/fake/oc", kubeconfig="/fake/kubeconfig", deadline=100))
        live = self.owned(obj, obj["body"])
        if obj["kind"] == "StorageCluster":
            live["metadata"]["finalizers"] = ["operator-owned"]
            live["status"] = {"phase": "Ready", "observedGeneration": 1,
                              "conditions": [{"type": "Available", "status": "False"},
                                             {"type": "ReconcileComplete", "status": "True", "observedGeneration": 1}]}
        self.live[model.identity(obj)] = live
        return {"accepted": True, "object": copy.deepcopy(live)}


def private_channel(monkeypatch, tmp_path, request, cluster):
    payload = b'[{"kind":"Secret","name":"private-marker","data":{"userKey":"private-key-marker"}}]'
    path = tmp_path / "details"
    path.write_bytes(payload)
    request["materials"] = {"produced:export:export": {"value": str(path)}}
    calls = []
    monkeypatch.setattr(secrets, "read_private", lambda params: copy.deepcopy(cluster.live.get(model.identity(params["object"]))))

    def run(oc, kubeconfig, argv, deadline, body=None):
        calls.append({"argv": argv, "stdin": body})
        assert argv[0] == "create" and "--filename=-" in argv
        value = json.loads(body)
        obj = dict(apiVersion="v1", kind="Secret", namespace=value["metadata"]["namespace"], name=value["metadata"]["name"], namespaced=True)
        cluster.live[model.identity(obj)] = cluster.owned(obj, value)
        return 0, "", ""

    monkeypatch.setattr(secrets.native, "run", run)
    return payload, path, calls


def test_fdf_full_journey_and_continuation(tmp_path, monkeypatch, fdf_request):
    cluster = FDFCluster(fdf_request)
    payload, path, calls = private_channel(monkeypatch, tmp_path, fdf_request, cluster)
    pauses = []

    def pause(seconds):
        pauses.append(seconds)
        if cluster.noobaa_reads:
            cluster.noobaa_ready = True

    result = Engine(fdf_request, cluster.read, cluster.write, clock=lambda: 0, sleep=pause).execute()
    assert result["state"]["postcondition"] and result["changed"]
    assert cluster.crd_reads >= 2 and cluster.noobaa_reads >= 2 and len(pauses) >= 2
    assert [write for write in cluster.writes if write[0] == "approve"] == [("approve", "InstallPlan", "initial")]
    assert cluster.later["spec"]["approved"] is False
    assert result["state"]["olm"][0]["installPlan"]["name"] == "initial"
    assert result["state"]["olm"][0]["bundleImage"] == "registry.example.test/odf-bundle:v4.21.0"
    assert len(calls) == 1 and json.loads(calls[0]["stdin"])["data"]["external_cluster_details"] == base64.b64encode(payload).decode()
    assert "private-key-marker" not in str(calls[0]["argv"]) and "private-key-marker" not in json.dumps(result)
    cluster.writes.clear()
    repeated = Engine(fdf_request, cluster.read, cluster.write, clock=lambda: 0).execute()
    assert repeated["state"]["postcondition"] and not repeated["changed"]
    assert all(action == "place" for action, ignored_kind, ignored_name in cluster.writes) and len(calls) == 1
    assert repeated["state"]["olm"][0]["bundleImage"] == result["state"]["olm"][0]["bundleImage"]
    path.unlink()
    cluster.writes.clear()
    observed = Engine(fdf_request, cluster.read, lambda *_: pytest.fail("observation wrote"), clock=lambda: 0).execute(observe=True)
    assert observed["state"]["postcondition"] and not cluster.writes
    assert all("data" not in fields and "stringData" not in fields for kind, fields in cluster.reads if kind == "Secret")
    assert "private-key-marker" not in json.dumps(observed)
    cluster.noobaa_ready = False
    pending = Engine(fdf_request, cluster.read, clock=lambda: 0).execute(observe=True)
    assert not pending["state"]["postcondition"]
    assert any(check["lastObservation"] == "Configuring" for check in pending["state"]["checks"])


def test_fdf_differing_produced_key_reports_only_its_name(tmp_path, monkeypatch, fdf_request):
    cluster = FDFCluster(fdf_request)
    cluster.noobaa_ready = True
    ignored_payload, ignored_path, calls = private_channel(monkeypatch, tmp_path, fdf_request, cluster)
    Engine(fdf_request, cluster.read, cluster.write, clock=lambda: 0, sleep=lambda _: None).execute()
    secret = next(value for key, value in cluster.live.items() if value["kind"] == "Secret")
    secret["data"]["external_cluster_details"] = base64.b64encode(b"foreign-private-marker").decode()
    with pytest.raises(Refused, match=r"Secret/openshift-storage/rook-ceph-external-cluster-details differs at data.external_cluster_details$") as error:
        Engine(fdf_request, cluster.read, cluster.write, clock=lambda: 0).execute()
    assert "foreign-private-marker" not in str(error.value) and len(calls) == 1


def test_fdf_native_plan_listing_uses_no_object_name(tmp_path):
    oc = ScriptedOC(tmp_path, [response({"items": []})])
    obj = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "InstallPlan", "namespace": "openshift-storage", "name": "", "namespaced": True}
    assert oc.read(obj, ["spec", "status.bundleLookups"]) == {"answered": True, "object": {"items": []}}
    assert oc.calls()[0]["argv"][3:7] == ["get", "InstallPlan.v1alpha1.operators.coreos.com", "--namespace", "openshift-storage"]
