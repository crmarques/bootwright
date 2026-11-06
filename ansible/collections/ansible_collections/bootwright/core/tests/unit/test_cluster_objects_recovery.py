"""Pinned operators and transient placement preserve ownership without extra effects."""
from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import ScriptedOC, owned, response
from ansible_collections.bootwright.core.tests.unit.test_cluster_objects import object_request  # pylint: disable=unused-import


@pytest.mark.parametrize("observe", [False, True])
@pytest.mark.parametrize("resolution_failed", [False, True])
@pytest.mark.parametrize("dependent", [False, True])
def test_healthy_exact_pin_ignores_pending_upgrade(object_request, observe, resolution_failed, dependent):
    creator = object_request["program"]["steps"][0]["create"]
    obj = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription", "namespace": "operators",
           "name": "sample", "namespaced": True, "desiredDigest": "d" * 64,
           "body": {"spec": {"source": "catalog", "sourceNamespace": "marketplace"}, "metadata": {"name": "sample"}}}
    obj["body"]["spec"].update(name="sample", channel="stable", startingCSV="sample.v1", installPlanApproval="Manual")
    live = owned(object_request, obj)
    live["status"] = {"installedCSV": "sample.v1", "currentCSV": "sample.v2", "installPlanRef": {"name": "upgrade"}}
    if resolution_failed:
        live["status"]["conditions"] = [{"type": "ResolutionFailed", "status": "True", "message": "newer upgrade cannot resolve"}]
    upgrade = {"metadata": {"name": "upgrade", "uid": "plan-2"},
               "spec": {"approval": "Manual", "approved": False, "clusterServiceVersionNames": ["sample.v2"]}}
    object_request["program"]["steps"] = [{"kind": "subscribe", "subscribe": {"object": obj, "csv": "sample.v1", "approval": "exact"}}]
    if dependent:
        wanted = {"namespace": "operators", "subscription": "sample", "package": "sample", "channel": "stable",
                  "source": "catalog", "sourceNamespace": "marketplace", "csv": "sample.v1", "creator": model.ref(creator)}
        object_request["program"]["steps"] = [{"kind": "approveDependent", "approveDependent": wanted}]
    writes = []

    def read(ref, selected):
        if ref["kind"] == "Namespace":
            value = owned(object_request, creator)
        elif ref["kind"] == "Subscription":
            value = live
        elif ref["kind"] == "InstallPlan":
            value = {"items": [upgrade]} if not ref["name"] else upgrade
        elif ref["kind"] == "ClusterServiceVersion":
            value = {"status": {"phase": "Succeeded"}, "spec": {"version": "1.0.0"}}
        else:
            value = {"spec": {"image": "registry.example/catalog:v1"}}
        return {"answered": True, "object": value}

    result = Engine(object_request, read, lambda *args: writes.append(args), clock=lambda: 0).execute(observe)
    assert result["state"]["classification"] == "completed"
    if not dependent:
        assert result["state"]["olm"][0]["upgradePending"] is True
    assert not writes and upgrade["spec"]["approved"] is False


@pytest.mark.parametrize("reason,retry", [("InternalError", True), ("ServiceUnavailable", True), ("Forbidden", False), ("Invalid", False)])
def test_create_retries_printed_status_reason(tmp_path, object_request, reason, retry):
    oc = ScriptedOC(tmp_path, [{"code": 1, "stderr": "Error from server (%s): webhook unavailable\n" % reason}])
    obj = object_request["program"]["steps"][0]["create"]
    assert native.create(dict(oc.base, object=obj, owner="owner", manager="manager"))["retryable"] is retry
    assert len(oc.calls()) == 1


@pytest.mark.parametrize("properties,expected", [
    ('[{"type":"olm.maxOpenShiftVersion","value":"4.22"}]', "4.22"),
    ('[{"type":"olm.maxOpenShiftVersion","value":"bad"}]', "invalid"),
    ('[{"type":"olm.maxOpenShiftVersion","value":null}]', "invalid"),
    ('{', "invalid"), ('{}', "invalid"), ('[]', ""),
])
def test_maximum_version_reads_csv_properties(object_request, properties, expected):
    engine = Engine(object_request, lambda *_: {"answered": True, "object": None})
    engine.csv_readings[("operators", "sample.v1")] = {"metadata": {"annotations": {"olm.properties": properties}}}
    result = engine.olm_facts({"namespace": "operators"}, {"spec": {"source": "catalog", "sourceNamespace": "marketplace"}}, None, "sample.v1")
    assert result["maxOpenShiftVersion"] == expected
    template = native.projection({"kind": "ClusterServiceVersion"}, ["metadata.annotations"])
    assert 'olm.properties' in template


@pytest.mark.parametrize("failure", [{"unknown": True}, {"retryable": True}])
@pytest.mark.parametrize("expires", [False, True])
def test_placement_retries_to_deadline(object_request, failure, expires):
    placement = {"namespace": "operators", "name": "credentials", "type": "Opaque", "desiredDigest": "d" * 64}
    object_request["program"]["steps"] = [{"kind": "place", "place": placement}]
    writes = []
    now = [0]

    def write(*args):
        writes.append(args)
        if expires or len(writes) == 1:
            return dict(failure, accepted=False)
        return {"accepted": True, "equal": True, "uid": "secret-1", "basis": "mark-recovered"}

    engine = Engine(object_request, lambda *_: {"answered": True, "object": None}, write,
                    clock=lambda: now[0], sleep=lambda seconds: now.__setitem__(0, now[0] + seconds))
    if expires:
        with pytest.raises(Refused, match="deadline reached.*placement"):
            engine.execute()
        assert now[0] == 100
    else:
        assert engine.execute()["state"]["postcondition"]
        assert len(writes) == 2
    assert all(call[0] == "place" for call in writes)


@pytest.mark.parametrize("reason,retry", [("InternalError", True), ("ServiceUnavailable", True), ("Forbidden", False)])
def test_secret_create_retains_status_reason(tmp_path, object_request, reason, retry):
    placement = {"namespace": "operators", "name": "credentials", "type": "Opaque", "secret": "input", "keys": []}
    obj = {"apiVersion": "v1", "kind": "Secret", "namespace": "operators", "name": "credentials", "namespaced": True}
    oc = ScriptedOC(tmp_path, [response(), {"code": 1, "stderr": "Error from server (%s): unavailable\n" % reason}, response()])
    result = secrets.place(dict(oc.base, object=obj, placement=placement, materials={"input": {}}, owner="owner",
                                manager="manager", context=object_request))
    assert result.get("retryable", False) is retry
    assert [call["argv"][3] for call in oc.calls()] == ["get", "create", "get"]
    assert "unavailable" not in json.dumps(result)


@pytest.mark.parametrize("text", ["InternalError in a message", "Error from server (Forbidden): InternalError",
                                  "prefix Error from server (InternalError): denied", "Error from server (Invalid): ServiceUnavailable"])
def test_retry_does_not_match_message_text(text):
    assert not native.transient_status("", text)


@pytest.mark.parametrize("value", ["\a", "\v", "\x1b", "\U000e0001", "\u2028", "\x00\t\r\n\\\"<>&'", "é"])
def test_projection_transport_preserves_strings_and_keys(value):
    wire = ["map", {value.encode().hex(): ["list", [["string", value.encode().hex()], ["value", None]]]}]
    assert native.unproject(json.loads(json.dumps(wire))) == {value: [value, None]}


@pytest.mark.parametrize("difference", ["ownership", "data", "type"])
def test_proved_placement_mismatch_never_retries(object_request, difference):
    placement = {"namespace": "operators", "name": "credentials", "type": "Opaque", "desiredDigest": "d" * 64}
    calls = []
    engine = Engine(object_request, lambda *_: {"answered": True, "object": None},
                    lambda *args: calls.append(args) or {"accepted": False, "difference": difference}, clock=lambda: 0,
                    sleep=lambda _: pytest.fail("proved mismatch slept"))
    with pytest.raises(Refused, match="differs at " + difference):
        engine.place(placement)
    assert len(calls) == 1


def test_observing_placement_never_reads_material_or_writes(object_request):
    placement = {"namespace": "operators", "name": "credentials", "type": "Opaque", "desiredDigest": "d" * 64}
    engine = Engine(object_request, lambda *_: {"answered": True, "object": None}, lambda *_: pytest.fail("observation wrote"))
    engine.place(placement, observe=True)
    assert engine.state["observations"][0]["ownership"] == "absent"


def projected(value):
    if isinstance(value, dict):
        return ["map", {key.encode().hex(): projected(item) for key, item in value.items()}]
    if isinstance(value, list):
        return ["list", [projected(item) for item in value]]
    if isinstance(value, str):
        return ["string", value.encode().hex()]
    return ["value", value]


def test_projected_create_is_read_back_and_receipted(tmp_path, object_request):
    obj = object_request["program"]["steps"][0]["create"]
    obj["body"]["spec"] = {"\a\U000e0001": "\v\x1b\U000e0001"}
    live = owned(object_request)
    oc = ScriptedOC(tmp_path, [response(), response(projected(live))])
    result = Engine(object_request, oc.read, oc.write, clock=lambda: 0).execute()
    assert result["state"]["postcondition"]
    assert result["state"]["receipts"][0]["uid"] == "uid-1"
    assert [call["argv"][3] for call in oc.calls()] == ["get", "create"]


def test_projected_list_and_private_read_decode(tmp_path, object_request):
    value = {"metadata": {"uid": "uid-1"}, "data": {"key": "c2VjcmV0"}}
    oc = ScriptedOC(tmp_path, [response({"items": [projected(value)]}), response(projected(value))])
    obj = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "InstallPlan", "name": "", "namespace": "operators"}
    assert native.get(dict(oc.base, object=obj))["object"] == {"items": [value]}
    secret = {"apiVersion": "v1", "kind": "Secret", "name": "credentials", "namespace": "operators"}
    assert secrets.read_private(dict(oc.base, object=secret)) == value


@pytest.mark.parametrize("stage", ["private-read", "post-create-read", "create"])
def test_secret_transient_failure_recovers_without_duplicate_create(tmp_path, object_request, stage):
    placement = {"namespace": "operators", "name": "credentials", "type": "Opaque", "secret": "input", "keys": [],
                 "desiredDigest": "d" * 64}
    obj = {"apiVersion": "v1", "kind": "Secret", "namespace": "operators", "name": "credentials", "namespaced": True}
    owner = model.mark(object_request["operation"], object_request["block"], obj)
    live = secrets.desired(placement, {"input": {}}, owner)
    live["metadata"].update(uid="secret-1", creationTimestamp="2026-01-01T00:00:01Z")
    failure = {"code": 1, "stderr": "Error from server (InternalError): temporarily unavailable"}
    sequences = {"private-read": [failure, response(projected(live))],
                 "post-create-read": [response(), response(), failure, response(projected(live))],
                 "create": [response(), failure, response(), response(), response(), response(projected(live))]}
    oc = ScriptedOC(tmp_path, [response()] + sequences[stage])
    object_request["materials"] = {"input": {}}
    object_request["program"]["steps"] = [{"kind": "place", "place": placement}]
    result = Engine(object_request, oc.read, lambda action, ref, extra: secrets.place(dict(oc.base, object=ref, **extra)),
                    clock=lambda: 0, sleep=lambda _: None).execute()
    assert result["state"]["postcondition"]
    assert result["state"]["receipts"][0]["uid"] == "secret-1"
    creates = [call for call in oc.calls() if call["argv"][3] == "create"]
    assert len(creates) == {"private-read": 0, "post-create-read": 1, "create": 2}[stage]
    assert all(json.loads(call["stdin"])["metadata"]["annotations"][model.OWNER] == owner for call in creates)


@pytest.mark.parametrize("phase", ["Pending", "Failed", None])
@pytest.mark.parametrize("observe", [False, True])
def test_resolution_failed_is_not_ignored_without_healthy_pin(object_request, phase, observe):
    obj = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription", "namespace": "operators",
           "name": "sample", "namespaced": True, "desiredDigest": "d" * 64,
           "body": {"metadata": {"name": "sample"}, "spec": {"source": "catalog", "sourceNamespace": "marketplace"}}}
    live = owned(object_request, obj)
    live["status"] = {"installedCSV": "sample.v1", "conditions": [{"type": "ResolutionFailed", "status": "True"}]}
    object_request["program"]["steps"] = [{"kind": "subscribe", "subscribe": {"object": obj, "csv": "sample.v1", "approval": "exact"}}]

    def read(ref, selected):
        return {"answered": phase is not None or ref["kind"] == "Subscription",
                "object": live if ref["kind"] == "Subscription" else {"status": {"phase": phase}}}

    engine = Engine(object_request, read, lambda *_: pytest.fail("unhealthy pin wrote"))
    if observe:
        assert engine.execute(True)["state"]["classification"] == "unknown"
    else:
        with pytest.raises(Refused, match="resolution failed"):
            engine.execute()
