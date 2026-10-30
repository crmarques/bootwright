"""A custom resource's gate holds only for its own kind, served by a Namespaced CustomResourceDefinition."""
from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused

CRD = {"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition", "namespace": "",
       "name": "foos.example.test", "namespaced": False}
WIDGET = {"apiVersion": "example.test/v1", "kind": "Widget", "name": "widget", "namespace": "widget-ns", "namespaced": True,
          "body": {"apiVersion": "example.test/v1", "kind": "Widget", "metadata": {"name": "widget", "namespace": "widget-ns"}},
          "desiredDigest": "d" * 64}


def gate(kind):
    return [{"resourceExists": dict(CRD)},
            {"statusPhase": dict(CRD, path="spec.scope", values=["Namespaced"])},
            {"statusPhase": dict(CRD, path="status.acceptedNames.kind", values=[kind])}]


class Cluster:
    def __init__(self, scope, accepted):
        self.definition = {"metadata": {"uid": "crd"}, "spec": {"scope": scope}, "status": {"acceptedNames": {"kind": accepted}}}
        self.reads, self.writes = [], []

    def read(self, obj, fields):
        self.reads.append((obj["kind"], list(fields)))
        if obj["kind"] == "CustomResourceDefinition":
            return {"answered": True, "object": self.definition}
        return {"answered": True, "object": None}

    def write(self, action, obj, extra):
        self.writes.append((action, obj["kind"], obj["name"], extra))
        raise AssertionError("a custom resource was created behind an unmet gate")


def request(kind="Widget"):
    program = {"version": model.VERSION, "steps": [{"kind": "wait", "wait": gate(kind)}, {"kind": "create", "create": WIDGET}], "checks": []}
    return {"program": program, "operation": "operation", "block": "block", "registered": "2026-01-01T00:00:00Z",
            "receipts": [], "deadline": 100}


@pytest.mark.parametrize("scope,accepted", [("Cluster", "Widget"), ("Namespaced", "Foo"), ("Cluster", "Foo"), (None, "Widget")])
def test_a_gate_the_definition_does_not_meet_stays_unmet_and_creates_nothing(scope, accepted):
    cluster = Cluster(scope, accepted)
    engine = Engine(request(), cluster.read, cluster.write, clock=lambda: 100, sleep=lambda ignored_seconds: None)
    with pytest.raises(Refused, match="deadline reached: check "):
        engine.execute()
    assert cluster.writes == []
    assert [kind for kind, ignored_fields in cluster.reads].count("Widget") == 0


def test_a_namespaced_definition_that_serves_the_resource_kind_holds_the_gate():
    cluster = Cluster("Namespaced", "Widget")
    engine = Engine(request(), cluster.read, cluster.write, clock=lambda: 100, sleep=lambda ignored_seconds: None)
    engine.checks(gate("Widget"), gate=True)
    assert engine.not_ready is False
    fields = [selected for kind, selected in cluster.reads if kind == "CustomResourceDefinition"]
    assert any("spec.scope" in selected for selected in fields)


@pytest.mark.parametrize("scope,accepted", [("Cluster", "Widget"), ("Namespaced", "Foo"), ("Namespaced", None)])
def test_observation_reads_both_paths_and_never_holds_the_gate_for_another_definition(scope, accepted):
    cluster = Cluster(scope, accepted)
    result = Engine(request(), cluster.read, cluster.write, clock=lambda: 0, sleep=lambda ignored_seconds: None).execute(observe=True)
    assert result["state"]["postcondition"] is False
    assert cluster.writes == []
    fields = {path for kind, selected in cluster.reads if kind == "CustomResourceDefinition" for path in selected}
    assert {"spec.scope", "status.acceptedNames.kind"} <= fields
    assert [item["satisfied"] for item in result["state"]["checks"]][:1] == [True]
    assert not all(item["satisfied"] for item in result["state"]["checks"])
