"""The shared object port against scripted oc processes, with no cluster access."""
from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused


@pytest.fixture
def object_request():
    obj = {"apiVersion": "v1", "kind": "Namespace", "name": "example", "namespace": "", "namespaced": False,
           "body": {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "example"}}, "desiredDigest": "d" * 64}
    return {"program": {"version": model.VERSION, "steps": [{"kind": "create", "create": obj}], "checks": []},
            "operation": "operation", "block": "block", "registered": "2026-01-01T00:00:00Z", "receipts": [], "deadline": 100}


def owned(request, obj=None):
    obj = obj or request["program"]["steps"][0]["create"]
    live = copy.deepcopy(obj["body"])
    live["metadata"].update(uid="uid-1", creationTimestamp="2026-01-01T00:00:01Z",
                            annotations={model.OWNER: model.mark(request["operation"], request["block"], obj)})
    return live


class ScriptedOC:
    def __init__(self, tmp_path, answers):
        self.responses = tmp_path / "responses.json"
        self.log = tmp_path / "calls.json"
        self.responses.write_text(json.dumps(answers))
        self.log.write_text("[]")
        self.executable = tmp_path / "oc"
        script = "#!/usr/bin/python3\nimport json,sys,pathlib\n"
        script += "p=pathlib.Path(%r); l=pathlib.Path(%r)\n" % (str(self.responses), str(self.log))
        script += "answers=json.loads(p.read_text()); answer=answers.pop(0); p.write_text(json.dumps(answers))\n"
        script += "calls=json.loads(l.read_text()); calls.append({'argv':sys.argv[1:],'stdin':sys.stdin.read()})\n"
        script += "l.write_text(json.dumps(calls))\n"
        script += "sys.stdout.write(answer.get('stdout','')); sys.stderr.write(answer.get('stderr','')); sys.exit(answer.get('code',0))\n"
        self.executable.write_text(script)
        self.executable.chmod(0o700)
        self.base = {"oc": str(self.executable), "kubeconfig": str(tmp_path / "kubeconfig"), "deadline": 9999999999}

    def read(self, obj, fields):
        return native.get(dict(self.base, object=obj, fields=fields))

    def write(self, action, obj, extra):
        request = dict(self.base, object=obj, **extra)
        return native.create(request) if action == "create" else native.approve(request)

    def calls(self):
        return json.loads(self.log.read_text())


def response(value=None, **kwargs):
    return dict(stdout="" if value is None else json.dumps(value), **kwargs)


def test_absent_create_uses_frozen_identity_and_manager(tmp_path, object_request):
    live = owned(object_request)
    oc = ScriptedOC(tmp_path, [response(), response(live)])
    result = Engine(object_request, oc.read, oc.write, clock=lambda: 0).execute()
    assert result["changed"] and result["state"]["postcondition"]
    calls = oc.calls()
    assert calls[0]["argv"][3:5] == ["get", "Namespace.v1"]
    assert "--ignore-not-found=true" in calls[0]["argv"]
    assert "--filename=-" in calls[1]["argv"]
    body = json.loads(calls[1]["stdin"])
    assert body["metadata"]["annotations"][model.OWNER] == live["metadata"]["annotations"][model.OWNER]
    assert "bootwright-" + model.mark("operation", "block")[:32] in calls[1]["argv"]
    assert all("--request-timeout=30s" in c["argv"] for c in calls)
    assert [r["basis"] for r in result["state"]["receipts"]] == ["created"]


@pytest.mark.parametrize("failure", [{"code": 1}, {"code": 0, "stdout": "broken"}])
def test_lost_create_response_reprobes_owned_object(tmp_path, object_request, failure):
    oc = ScriptedOC(tmp_path, [response(), failure, response(owned(object_request))])
    result = Engine(object_request, oc.read, oc.write, clock=lambda: 0).execute()
    assert result["state"]["postcondition"]
    assert [c["argv"][3] for c in oc.calls()] == ["get", "create", "get"]
    # A create whose reply was lost is this block's by its mark alone.
    assert [r["basis"] for r in result["state"]["receipts"]] == ["mark-recovered"]


def test_a_receipt_records_the_basis_of_its_ownership(object_request):
    obj = object_request["program"]["steps"][0]["create"]
    live = owned(object_request)
    recovered = Engine(object_request, lambda *_: {"answered": True, "object": live}).execute(observe=True)
    assert [r["basis"] for r in recovered["state"]["receipts"]] == ["mark-recovered"]
    object_request["receipts"] = [dict(model.ref(obj), uid="uid-1", desiredDigest=obj["desiredDigest"],
                                       disposition="created", basis="created")]
    matched = Engine(object_request, lambda *_: {"answered": True, "object": live}).execute(observe=True)
    assert [r["basis"] for r in matched["state"]["receipts"]] == ["uid-matched"]


@pytest.mark.parametrize("reason,retry", [("InternalError", True), ("ServiceUnavailable", True), ("Invalid", False)])
def test_create_retries_only_structured_transient_status(tmp_path, object_request, reason, retry):
    refusal = response({"kind": "Status", "reason": reason}, code=1)
    oc = ScriptedOC(tmp_path, [response(), refusal, response(), response(), response(owned(object_request))])
    engine = Engine(object_request, oc.read, oc.write, clock=lambda: 0, sleep=lambda _: None)
    if retry:
        assert engine.execute()["state"]["postcondition"]
    else:
        with pytest.raises(Refused, match="create unproved"):
            engine.execute()
        assert len(oc.calls()) == 3


def test_foreign_refused_without_create(tmp_path, object_request):
    live = owned(object_request)
    live["metadata"]["annotations"] = {}
    oc = ScriptedOC(tmp_path, [response(live)])
    with pytest.raises(Refused, match="foreign"):
        Engine(object_request, oc.read, oc.write).execute()
    assert len(oc.calls()) == 1


def test_ownership_receipt_mark_and_time(object_request):
    obj = object_request["program"]["steps"][0]["create"]
    live = owned(object_request)
    assert model.ownership(obj, live, object_request) == "own"
    live["metadata"]["creationTimestamp"] = "2025-01-01T00:00:00Z"
    assert model.ownership(obj, live, object_request) == "foreign"
    object_request["receipts"] = [dict(model.ref(obj), uid="uid-1")]
    assert model.ownership(obj, live, object_request) == "own"
    live["metadata"]["uid"] = "uid-2"
    assert model.ownership(obj, live, object_request) == "foreign"


def test_observation_starts_only_get(tmp_path, object_request):
    oc = ScriptedOC(tmp_path, [response(owned(object_request))])
    result = Engine(object_request, oc.read).execute(observe=True)
    assert result["state"]["classification"] == "completed"
    assert not result["changed"]
    assert [c["argv"][3] for c in oc.calls()] == ["get"]


@pytest.mark.parametrize("kind,path", [("ClusterRoleBinding", "subjects"), ("ClusterRole", "rules"), ("Subscription", "spec")])
def test_security_sensitive_fields_compare_exactly(kind, path):
    wanted = {"kind": kind, "body": {path: {"a": 1}}}
    assert model.compare(wanted, {path: {"a": 1, "b": 2}}) == path
    assert model.difference([1], [1, 2], "items") == "items"


def test_projection_excludes_secret_values_and_installplan_steps():
    secret = native.projection({"kind": "Secret"}, ["data", "type"])
    assert 'index . "data"' not in secret
    assert "range $k,$v := .data" in secret
    plan = native.projection({"kind": "InstallPlan"}, ["spec", "metadata.ownerReferences", "status.bundleLookups"])
    assert '"plan"' not in plan
    assert '"bundleLookups"' in plan


@pytest.mark.parametrize("api_version,qualified", [("one.example/v1", "Example.v1.one.example"), ("two.example/v1", "Example.v1.two.example")])
def test_resource_qualification(api_version, qualified):
    assert native.resource({"kind": "Example", "apiVersion": api_version, "name": "example"}) == qualified


@pytest.mark.parametrize("kind", ["InstallPlan", "ClusterServiceVersion"])
def test_operator_namespace_listing_uses_native_transport(tmp_path, kind):
    oc = ScriptedOC(tmp_path, [response({"items": []})])
    obj = {"kind": kind, "apiVersion": "operators.coreos.com/v1alpha1", "namespace": "operators", "name": ""}
    assert oc.read(obj, ["spec"]) == {"answered": True, "object": {"items": []}}
    assert oc.calls()[0]["argv"][3:5] == ["get", kind + ".v1alpha1.operators.coreos.com"]


def test_lagging_condition_and_failure():
    check = {"type": "Available", "status": "True", "failOn": [{"type": "Degraded", "status": "True"}]}
    live = {"metadata": {"generation": 2}, "status": {"conditions": [{"type": "Available", "status": "True", "observedGeneration": 1}]}}
    assert not model.condition(check, live)[0]
    live["status"]["conditions"].append({"type": "Degraded", "status": "True", "reason": "Failed"})
    assert model.condition(check, live)[3]


def test_unknown_read_at_deadline_never_writes(object_request):
    writes = []
    with pytest.raises(Refused, match="read unknown"):
        Engine(object_request, lambda *_: {"answered": False}, lambda *a: writes.append(a), clock=lambda: 100).execute()
    assert not writes


def test_bounded_native_values():
    assert model.native("a\x00b") == "ab"
    assert len(model.native("é" * 200).encode()) <= 256
    assert model.native("bad name", "name") == "invalid"
    assert model.native("4.20.1", "version") == "invalid"
    assert model.native("registry.example/image@sha256:" + "a" * 64, "image") != "invalid"


@pytest.fixture
def olm_values(object_request):
    subscription = {"apiVersion": "operators.coreos.com/v1alpha1", "kind": "Subscription", "namespaced": True,
                    "namespace": "operators", "name": "example"}
    live = {"metadata": {"uid": "sub-1"}, "spec": {"source": "catalog", "sourceNamespace": "marketplace"}}
    plan = {"metadata": {"uid": "plan-1", "ownerReferences": [{"kind": "Subscription", "uid": "sub-1"}]},
            "spec": {"approval": "Manual", "approved": False, "clusterServiceVersionNames": ["example.v1"]},
            "status": {"bundleLookups": [{"catalogSourceRef": {"name": "catalog", "namespace": "marketplace"}}]}}
    return subscription, live, plan


@pytest.mark.parametrize("fault", ["extra-csv", "owner", "automatic", "catalog", "catalog-namespace", "no-bundles"])
def test_unsafe_installplan_is_never_approved(object_request, olm_values, fault):
    sub, live, plan = olm_values
    if fault == "extra-csv":
        plan["spec"]["clusterServiceVersionNames"].append("other.v1")
    elif fault == "owner":
        plan["metadata"]["ownerReferences"][0]["uid"] = "other-sub"
    elif fault == "automatic":
        plan["spec"]["approval"] = "Automatic"
    elif fault == "catalog":
        plan["status"]["bundleLookups"][0]["catalogSourceRef"]["name"] = "other"
    elif fault == "catalog-namespace":
        plan["status"]["bundleLookups"][0]["catalogSourceRef"]["namespace"] = "other"
    else:
        plan["status"]["bundleLookups"] = []
    writes = []
    engine = Engine(object_request, lambda *_: {"answered": True, "object": plan}, lambda *a: writes.append(a))
    with pytest.raises(Refused, match="approval proof"):
        engine.plan(sub, live, "example.v1", "plan", False)
    assert not writes


def test_approval_patch_tests_all_proofs(tmp_path, olm_values):
    sub, ignored_live, plan = olm_values
    oc = ScriptedOC(tmp_path, [response()])
    assert oc.write("approve", dict(sub, name="plan"), {"plan": plan})["accepted"]
    argv = oc.calls()[0]["argv"]
    patches = json.loads(argv[argv.index("--patch") + 1])
    tests = {p["path"] for p in patches if p["op"] == "test"}
    assert {"/metadata/uid", "/metadata/ownerReferences", "/spec/clusterServiceVersionNames", "/spec/approval", "/status/bundleLookups"} <= tests
    assert patches[-1] == {"op": "add", "path": "/spec/approved", "value": True}


def test_already_approved_plan_does_not_patch(object_request, olm_values):
    sub, live, plan = olm_values
    plan["spec"]["approved"] = True
    writes = []
    engine = Engine(object_request, lambda *_: {"answered": True, "object": plan}, lambda *a: writes.append(a))
    assert engine.plan(sub, live, "example.v1", "plan", False)["uid"] == "plan-1"
    assert not writes


@pytest.mark.parametrize("fault", ["", "extra-csv", "foreign", "installed", "already-approved"])
def test_operator_managed_only_approves_first_owned_exact_plan(object_request, olm_values, fault):
    sub, live, plan = olm_values
    plan["metadata"]["name"] = "first"
    if fault == "extra-csv":
        plan["spec"]["clusterServiceVersionNames"].append("other.v1")
    elif fault == "foreign":
        plan["metadata"]["ownerReferences"][0]["uid"] = "foreign"
    elif fault == "installed":
        live["status"] = {"installedCSV": "example.v1", "installPlanRef": {"name": "dependency"}}
    elif fault == "already-approved":
        plan["spec"]["approved"] = True
    writes = []
    reads = []

    def read(obj, selected):
        reads.append(obj)
        return {"answered": True, "object": {"items": [plan]} if not obj["name"] else plan}

    engine = Engine(object_request, read, lambda *args: writes.append(args) or {"accepted": True})
    if fault == "extra-csv":
        with pytest.raises(Refused, match="example.v1,other.v1"):
            engine.first_plan(sub, live, "example.v1", False)
    else:
        result = engine.first_plan(sub, live, "example.v1", False)
        assert bool(result) == (fault in ("", "already-approved"))
    assert len(writes) == (1 if not fault else 0)
    if fault == "installed":
        assert not reads


def test_operator_managed_namespace_evidence_is_bounded_and_sorted(object_request):
    plans = [{"metadata": {"name": "plan-%02d" % i}, "spec": {"clusterServiceVersionNames": ["example.v1"], "approved": True}} for i in range(20)]
    csvs = [{"metadata": {"name": "csv-%02d" % i}, "spec": {"version": "1.0.0"}, "status": {"phase": "Succeeded"}} for i in range(20)]

    def read(obj, selected):
        return {"answered": True, "object": {"items": list(reversed(plans if obj["kind"] == "InstallPlan" else csvs))}}

    evidence = Engine(object_request, read).operator_facts("operators")
    assert len(evidence["plans"]) == len(evidence["csvs"]) == 16
    assert evidence["plans"][0]["name"] == "plan-00"
    assert evidence["csvs"][-1]["name"] == "csv-15"


@pytest.mark.parametrize("field", ["name", "channel", "source", "sourceNamespace", "startingCSV", "installPlanApproval", "creationTimestamp"])
def test_dependent_refuses_wrong_spec_or_old_creation(object_request, field):
    creator = object_request["program"]["steps"][0]["create"]
    wanted = {"namespace": "dependent", "subscription": "example", "package": "package", "channel": "stable",
              "source": "catalog", "sourceNamespace": "marketplace", "csv": "example.v1", "creator": model.ref(creator)}
    live = {"metadata": {"uid": "sub", "creationTimestamp": "2026-01-01T00:00:02Z"},
            "spec": {"name": "package", "channel": "stable", "source": "catalog", "sourceNamespace": "marketplace",
                     "startingCSV": "example.v1", "installPlanApproval": "Manual"}}
    if field == "creationTimestamp":
        live["metadata"][field] = "2025-01-01T00:00:00Z"
    else:
        live["spec"][field] = "other"

    def read(obj, ignored_fields):
        return {"answered": True, "object": owned(object_request) if obj["kind"] == "Namespace" else live}
    with pytest.raises(Refused, match=field):
        Engine(object_request, read).dependent(wanted)


def test_wait_observation_reads_shared_object_once_and_keeps_all_checks(object_request):
    obj = object_request["program"]["steps"][0]["create"]
    check = {"resourceExists": model.ref(obj)}
    object_request["program"]["steps"].append({"kind": "wait", "wait": [check]})
    object_request["program"]["checks"] = [check]
    reads = []

    def read(obj, fields):
        reads.append((obj, fields))
        return {"answered": True, "object": owned(object_request)}
    result = Engine(object_request, read).execute(observe=True)
    assert len(reads) == 1
    assert [c["index"] for c in result["state"]["checks"]] == [0, 1]


@pytest.mark.parametrize("changed_labels", [False, True])
def test_secret_place_preserves_bytes_and_never_returns_values(tmp_path, object_request, changed_labels):
    import base64
    from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
    part = tmp_path / "part"
    raw = b"first line\nsecond line\n\x00"
    part.write_bytes(raw)
    obj = {"apiVersion": "v1", "kind": "Secret", "namespace": "example", "name": "credentials", "namespaced": True}
    placement = {"namespace": "example", "name": "credentials", "type": "Opaque", "secret": "input",
                 "keys": [{"key": "document", "part": "value"}], "strings": {"plain": "nonsecret"}, "labels": {"purpose": "example"}}
    owner = model.mark("operation", "block", obj)
    wanted = secrets.desired(placement, {"input": {"value": str(part)}}, owner)
    assert base64.b64decode(wanted["data"]["document"]) == raw
    live = copy.deepcopy(wanted)
    live["metadata"].update(uid="secret-1", creationTimestamp="2026-01-01T00:00:01Z")
    if changed_labels:
        live["metadata"]["labels"] = {}
    oc = ScriptedOC(tmp_path, [response(), response(), response(live)])
    params = dict(oc.base, object=obj, placement=placement, materials={"input": {"value": str(part)}}, owner=owner,
                  manager="bootwright-test", context=object_request)
    result = secrets.place(params)
    if changed_labels:
        assert not result["accepted"]
        return
    assert result == {"accepted": True, "equal": False, "uid": "secret-1", "basis": "created"}
    assert "document" not in json.dumps(result) and "first line" not in json.dumps(result)
    create = oc.calls()[1]
    assert create["argv"][-2:] == ["-o", "name"]
    assert base64.b64decode(json.loads(create["stdin"])["data"]["document"]) == raw
    assert all("first line" not in json.dumps(call["argv"]) for call in oc.calls())


def test_secret_continuation_refuses_extra_key(tmp_path, object_request):
    from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
    part = tmp_path / "part"
    part.write_bytes(b"sensitive")
    obj = {"apiVersion": "v1", "kind": "Secret", "namespace": "example", "name": "credentials", "namespaced": True}
    placement = {"namespace": "example", "name": "credentials", "type": "Opaque", "secret": "input", "keys": [{"key": "key", "part": "value"}]}
    owner = model.mark("operation", "block", obj)
    live = secrets.desired(placement, {"input": {"value": str(part)}}, owner)
    live["metadata"].update(uid="secret-1", creationTimestamp="2026-01-01T00:00:01Z")
    live["data"]["extra"] = "c2VjcmV0"
    oc = ScriptedOC(tmp_path, [response(live)])
    result = secrets.place(dict(oc.base, object=obj, placement=placement, materials={"input": {"value": str(part)}},
                                owner=owner, manager="bootwright-test", context=object_request))
    assert result == {"accepted": False, "difference": "data"}
    assert len(oc.calls()) == 1


@pytest.mark.parametrize("invalid", ["", "version", "unknown-program-field"])
def test_real_ansible_action_module_handoff(tmp_path, object_request, invalid):
    import os
    import pathlib
    import subprocess
    import sys
    import yaml
    oc = ScriptedOC(tmp_path, [response(), response(owned(object_request))])
    request = dict(object_request, **oc.base)
    collection = pathlib.Path(__file__).resolve().parents[2]
    spec = yaml.safe_load((collection / "roles/cluster_objects/meta/argument_specs.yml").read_text())["argument_specs"]["apply"]["options"]
    program = copy.deepcopy(request["program"])
    if invalid == "version":
        program["version"] = "unsupported"
    elif invalid:
        program["unknown"] = True
    arguments = {"cluster_objects_request": dict(request, version="consumer-v1", cluster="example", binding="example", budgetSeconds=60),
                 "cluster_objects_program": program, "cluster_objects_oc": oc.base["oc"],
                 "cluster_objects_material": {"kubeconfig": oc.base["kubeconfig"]}, "cluster_objects_evidence": {},
                 "cluster_objects_evidence_version": "evidence-v1", "cluster_objects_digest": "a" * 64}
    result_path = tmp_path / "result.json"
    playbook = tmp_path / "play.json"
    tasks = [
        {"name": "Validate shared role inputs", "ansible.builtin.validate_argument_spec": {
            "argument_spec": spec, "provided_arguments": arguments}},
        {"name": "Run the shared adapter", "bootwright.core.cluster_objects_write": {"request": request},
         "register": "result", "no_log": True, "failed_when": False},
        {"name": "Retain bounded evidence", "ansible.builtin.copy": {
            "content": "{{ result | to_json }}", "dest": str(result_path), "mode": "0600"}, "no_log": True},
    ]
    playbook.write_text(json.dumps([{
        "hosts": "localhost", "gather_facts": False, "connection": "local",
        "vars": {"ansible_python_interpreter": sys.executable}, "tasks": tasks,
    }]))
    config = tmp_path / "ansible.cfg"
    config.write_text("")
    environment = dict(os.environ, ANSIBLE_CONFIG=str(config), ANSIBLE_HOME=str(tmp_path / "home"),
                       ANSIBLE_LOCAL_TEMP=str(tmp_path / "local"),
                       ANSIBLE_COLLECTIONS_PATH=str(pathlib.Path(__file__).resolve().parents[5]),
                       ANSIBLE_STDOUT_CALLBACK="bootwright.core.censored")
    run = subprocess.run([sys.executable, "-m", "ansible.cli.playbook", "-i", "localhost,", str(playbook)],
                         capture_output=True, text=True, env=environment, timeout=30, check=False)
    if invalid:
        assert run.returncode != 0
        assert not oc.calls()
        return
    assert run.returncode == 0, run.stdout + run.stderr
    result = json.loads(result_path.read_text())
    assert "state" in result, result
    assert result["state"]["postcondition"]
    assert result["state"]["receipts"][0]["uid"] == "uid-1"
    assert "body" not in result and "invocation" not in result
    assert [call["argv"][3] for call in oc.calls()] == ["get", "create"]


def test_wait_recovers_transient_read_error(object_request):
    obj = object_request["program"]["steps"][0]["create"]
    object_request["program"]["checks"] = [{"resourceExists": dict(model.ref(obj), name="later")}]
    answers = [{"answered": True, "object": owned(object_request)}, {"answered": False},
               {"answered": True, "object": {"metadata": {"uid": "later"}}}]
    result = Engine(object_request, lambda *_: answers.pop(0), clock=lambda: 0, sleep=lambda seconds: None).execute()
    assert result["state"]["postcondition"]


def test_failed_create_foreign_reprobe_refuses(tmp_path, object_request):
    live = owned(object_request)
    live["metadata"]["annotations"] = {}
    oc = ScriptedOC(tmp_path, [response(), response(code=1), response(live)])
    with pytest.raises(Refused, match="foreign"):
        Engine(object_request, oc.read, oc.write).execute()
    assert [call["argv"][3] for call in oc.calls()] == ["get", "create", "get"]


def test_wait_deadline_names_every_unmet_check(object_request):
    obj = object_request["program"]["steps"][0]["create"]
    check = {"resourceExists": model.ref(obj)}
    engine = Engine(object_request, lambda *_: {"answered": True, "object": None}, clock=lambda: 100)
    with pytest.raises(Refused, match="check 0: absent; check 1: absent"):
        engine.checks([check, check])


def test_secret_material_symlink_is_not_opened(tmp_path):
    from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_secrets as secrets
    target = tmp_path / "target"
    target.write_bytes(b"private")
    link = tmp_path / "link"
    link.symlink_to(target)
    with pytest.raises(OSError):
        secrets.material(str(link))


@pytest.mark.parametrize("value", ["registry.example/Bad:1", "registry.example/a..b:1", "registry.example/image@sha256:abc",
                                   "https://credentials@example.test/image", "registry.example/image:latest", "user:password@host"])
def test_invalid_image_reference_is_never_evidence(value):
    assert model.native(value, "image") == "invalid"


@pytest.mark.parametrize("reading", ["drift", "foreign", "absent"])
def test_observation_keeps_recorded_uid_even_when_object_is_not_equal(object_request, reading):
    obj = object_request["program"]["steps"][0]["create"]
    receipt = dict(model.ref(obj), uid="uid-1", desiredDigest=obj["desiredDigest"], disposition="created")
    object_request["receipts"] = [receipt]
    live = owned(object_request)
    if reading == "drift":
        live["metadata"]["name"] = "different"
    elif reading == "foreign":
        live["metadata"]["uid"] = "uid-2"
    else:
        live = None
    result = Engine(object_request, lambda *_: {"answered": True, "object": live}).execute(observe=True)
    assert result["state"]["receipts"] == [receipt]


def test_action_failure_keeps_receipts_and_protocol_publishes_before_failure(object_request, monkeypatch):
    from types import SimpleNamespace
    from ansible_collections.bootwright.core.plugins.action import cluster_objects_write as action
    from ansible_collections.bootwright.core.plugins.action import cluster_objects_protocol as protocol
    obj = object_request["program"]["steps"][0]["create"]
    receipt = dict(model.ref(obj), uid="uid-1", desiredDigest=obj["desiredDigest"], disposition="created")
    object_request.update(oc="/oc", kubeconfig="/kubeconfig", receipts=[receipt])
    object_request["program"]["steps"] = [{"kind": "unsupported"}]
    runner = object.__new__(action.ActionModule)
    runner._task = SimpleNamespace(args={"request": object_request})
    result = runner.run(task_vars={})
    assert result["failed"] and result["state"]["receipts"] == [receipt]
    publisher = object.__new__(protocol.ActionModule)
    publisher._task = SimpleNamespace(args={"phase": "completed", "state": result["state"], "version": "evidence-v1",
                                            "digest": "a" * 64, "outcome": "unchanged", "observed": False})
    emitted = []
    monkeypatch.setattr(protocol, "emit", emitted.append)
    assert publisher.run()["failed"]
    assert emitted[0]["evidence"]["receipts"] == [receipt]
