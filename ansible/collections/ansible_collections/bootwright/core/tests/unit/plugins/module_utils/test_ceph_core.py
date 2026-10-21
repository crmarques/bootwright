"""Ceph boundaries refuse unproved identity, disks, effects and disclosure."""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import inspect
import json
import os
import pathlib
import stat
import subprocess
import sys

import pytest
import yaml

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_worker, ceph_native, ceph_native_resources, ceph_native_worker, ceph_pull_broker, ceph_registry_discovery, ceph_tunnel_staging,
)
from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics, native_specializations
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_native_specializations as specs
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as tunnel_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as object_gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as service_units

PASSWORD = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"
SYNTHETIC_CA = (pathlib.Path(__file__).resolve().parents[4] / "tests" / "fixtures" / "ceph-discovery-synthetic-ca.golden").read_bytes()
EMPTY_DASHBOARD = {"url": "", "certificateSHA256": "", "reachable": False}
UNWRITTEN = {"dashboard": "/nonexistent/bootwright-dashboard-output"}


@pytest.fixture
def core_request():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [h["name"] for h in hosts]
    return {"controller": "controller", "version": "ceph-core-v26", "action": "cluster", "cluster": "example", "owner": "apply-123",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible",
            "packageRepository": "https://packages.example.test/native", "bootstrap": "node-1",
            "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
            "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


class Markers:
    def __init__(self, owned=True):
        self.records = {}
        self.published = {}
        self.is_owned = owned
        self.writes = []
        self.path = "/private/cluster"

    def owned(self):
        return self.is_owned

    def read(self, name="owner.json"):
        return self.records.get(name, self.published.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = value
        self.writes.append(name)


def worker(core_request, answers, markers=None):
    calls = []

    def run(argv, data=b"", timeout=60):
        del data, timeout
        calls.append(argv)
        if "--format" in argv:
            command = tuple(argv[argv.index("--format") + 2:])
        elif argv[0] == "/usr/bin/ceph":
            command = tuple(argv[argv.index("--keyring") + 2:])
        else:
            command = tuple(argv)
        assert command in answers, "unexpected native command: " + repr(command)
        value = answers[command]
        if isinstance(value, tuple):
            return value
        return 0, json.dumps(value).encode()

    markers = markers or Markers()
    if isinstance(markers, Markers):
        markers.published.setdefault(ceph_native.DEVICE_IDENTITIES, identities(core_request))
    if core_request["action"] == "cluster":
        # Phase tests that do not exercise the dashboard see it not yet enabled.
        answers.setdefault(("mon", "dump"), {"mons": []})
        answers.setdefault(("mgr", "services"), {})
        answers.setdefault(("config-key", "ls"), [])
    kind = ceph_native_resources.ResourceWorker if core_request["action"] in ceph_native_resources.ACTIONS else ceph_native.Worker
    native = kind(core_request, core_request["bootstrap"], run=run, markers=markers)
    native.verify_tool = lambda: {"qualified-test-artifact": True}
    # These phase tests borrow an already proved daemon snapshot. The direct
    # filesystem/systemd/Podman owner is exercised in test_ceph_native_observe.

    def daemons():
        native.verify_tool()
        result = answers.get((native.tool, "ls"), [])
        ceph_native.require(isinstance(result, list) and len(result) <= 1024, "native-output")
        for item in result:
            ceph_native.require(isinstance(item, dict) and item.get("fsid") == core_request["fsid"], "foreign-cluster")
        return result

    native.daemons = daemons
    # Actual registry custody is covered by test_ceph_registry.py.
    native.prove_registry = lambda wanted=None: None
    native.provider_registry_absent = lambda: None
    # Phase snapshots explicitly assume the selected image's license was
    # accepted; test_ceph_bootstrap_finalization exercises the real MON reader.
    native.license_accepted = lambda: True
    native.selection = specs.selection(specs.synthesized(core_request["cephadm"]["packageVersion"]))
    return native, calls


def replicated_pool(**changes):
    """The frozen replicated rbd pool the Go side sends with every optional member unset."""
    pool = {"application": "rbd", "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
            "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}, "crushRule": "replicated_rule", "filesystem": "",
            "minSize": 2, "name": "volumes", "overwrites": False, "pgNum": 32, "quota": {"maxBytes": 0, "maxObjects": 0}, "role": "rbd", "size": 2,
            "type": "replicated"}
    pool.update(changes)
    return pool


def identities(core_request, form="{serial}"):
    """What each host's preparation derived; a virtio disk's native ID is its serial."""
    return {"version": ceph_native.DEVICE_IDENTITIES_VERSION, "fsid": core_request["fsid"],
            "devices": [{"host": host["name"], "path": device["path"], "serial": device["serial"], "deviceID": form.format(serial=device["serial"])}
                        for host in core_request["hosts"] for device in host["devices"]]}


def observation(core_request, ready=False):
    return {"version": "ceph-core-evidence-v8", "request": "", "action": core_request["action"], "owner": core_request["owner"], "fsid": core_request["fsid"],
            "owned": True, "exists": ready, "ready": ready, "removed": not ready, "partial": False, "health": "HEALTH_OK" if ready else "",
            "hosts": [h["name"] for h in core_request["hosts"]] if ready else [], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [],
            "osds": [], "resource": {"id": "", "path": ""}, "client": "", "dashboard": dict(EMPTY_DASHBOARD),
            "endpoint": {"certificateSHA256": "", "reachable": False}, "daemons": []}


def failing_command(core_request, applied):
    native, unused = worker(core_request, {})
    native.run = lambda argv, data=b"", timeout=60: (1, b"synthetic native failure")
    native.selection.applied = set(applied)
    with pytest.raises(ceph_native.NativeFailure, match="^native-command$") as caught:
        native.command(["/usr/bin/ceph", "status"])
    return caught.value.native_error


def test_a_general_flow_refusal_names_only_the_image_and_package(core_request):
    detail = failing_command(core_request, ())
    assert detail["coordinate"] == core_request["image"] + "; host package " + core_request["cephadm"]["packageVersion"]
    assert detail["cause"] == "synthetic native failure"


def test_a_refusal_after_a_matched_seam_names_the_applied_ids_in_order(core_request):
    detail = failing_command(core_request, {"ceph-call-home-opt-in", "ceph-bootstrap-without-license-flag"})
    assert detail["coordinate"] == (core_request["image"] + "; host package " + core_request["cephadm"]["packageVersion"]
                                    + "; specializations ceph-bootstrap-without-license-flag,ceph-call-home-opt-in")


def test_the_applied_ids_are_truncated_before_the_image_and_package(core_request):
    base = core_request["image"] + "; host package " + core_request["cephadm"]["packageVersion"]
    ids = ["spec-" + str(i).zfill(3) + "-" + "x" * 30 for i in range(40)]
    detail = failing_command(core_request, ids)
    coordinate = detail["coordinate"]
    assert len(coordinate.encode()) <= 512 and coordinate.startswith(base + "; specializations spec-000-")
    kept = coordinate[len(base + "; specializations "):].split(",")
    assert 0 < len(kept) < len(ids) and kept == ids[:len(kept)]
    assert native_diagnostics.valid(detail)
    core_request["image"] = "registry.example.test/" + "r" * 600 + "@sha256:" + "a" * 64
    detail = failing_command(core_request, ids)
    assert detail["coordinate"] == (core_request["image"] + "; host package " + core_request["cephadm"]["packageVersion"]).encode()[:512].decode()


def test_native_count_preserves_candidate_hosts(core_request):
    assert ceph_native.validate(core_request) is core_request
    core_request["services"][1]["placement"]["count_per_host"] = 1
    ceph_native.validate(core_request)
    core_request["services"][1]["placement"]["count"] = 4
    with pytest.raises(ceph_native.Refusal, match="placement-unqualified"):
        ceph_native.validate(core_request)


@pytest.mark.parametrize("size,minimum,extra_osd,extra_mon,valid", [
    (2, 2, False, False, True),
    (3, 2, False, False, False),
    (3, 2, True, False, True),
    (3, 2, False, True, False),
    (2, 1, False, False, False),
    (2, 3, False, False, False),
])
def test_pool_replication_requires_an_additional_osd_host(core_request, size, minimum, extra_osd, extra_mon, valid):
    core_request.update(action="pool", pool=replicated_pool(size=size, minSize=minimum))
    if extra_osd or extra_mon:
        host = copy.deepcopy(core_request["hosts"][0])
        host.update(name="node-4", machine="machine-4", address="192.0.2.4")
        host["devices"] = [{"path": "/dev/vdb", "serial": "disk4", "sizeBytes": 1024 ** 3}] if extra_osd else []
        core_request["hosts"].append(host)
        if extra_osd:
            core_request["services"][2]["placement"]["hosts"].append("node-4")
    if valid:
        assert ceph_native.validate(core_request) is core_request
    else:
        with pytest.raises(ceph_native.Refusal):
            ceph_native.validate(core_request)


def test_previous_core_request_version_refuses_before_any_native_effect(core_request):
    core_request["version"] = "ceph-core-v12"
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.Worker(core_request, core_request["bootstrap"],
                           run=lambda *args, **kwargs: pytest.fail("previous request reached native effects"))


@pytest.mark.parametrize("change", ["tag", "unknown", "root-disk", "empty-serial", "overlap", "selector", "missing-host"])
def test_unqualified_or_unbounded_input_never_executes(core_request, change):
    if change == "tag":
        core_request["image"] = "registry.example.test/ceph:latest"
    elif change == "unknown":
        core_request["extraCommand"] = "/usr/bin/true"
    elif change == "root-disk":
        core_request["services"][2]["spec"]["data_devices"]["paths"] = ["/dev/vda"]
    elif change == "empty-serial":
        core_request["hosts"][0]["devices"][0]["serial"] = ""
    elif change == "overlap":
        duplicate = copy.deepcopy(core_request["services"][2])
        duplicate["service_id"] = "other"
        core_request["services"].append(duplicate)
    elif change == "selector":
        core_request["services"][2]["spec"]["data_devices"] = {"all": True}
    else:
        core_request["services"][0]["placement"]["hosts"] = ["foreign"]
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(core_request)


TAG_ONLY = "registry.example.test/ceph:v9.9.2"
MALFORMED_DIGEST = "registry.example.test/ceph@sha256:" + "g" * 64
UNSUPPORTED = [
    pytest.param({}, "^unsupported-release$", id="native-transport"),
    pytest.param({"clusterSSHTransport": "verified-tunnel", "image": TAG_ONLY}, "^(request|unsupported-release)$", id="tunnel-tag-only"),
    pytest.param({"clusterSSHTransport": "verified-tunnel", "image": MALFORMED_DIGEST}, "^(request|unsupported-release)$", id="tunnel-malformed-digest"),
]


@pytest.mark.parametrize("operation", ["apply", "destroy"])
@pytest.mark.parametrize("change,reason", UNSUPPORTED)
def test_an_unsupported_transport_or_image_refuses_before_any_transport(core_request, operation, change, reason):
    class NoTransport:
        def call(self, *args):
            pytest.fail("an unsupported transport or image performed a remote operation")

        def packages(self, *args):
            pytest.fail("an unsupported transport or image staged packages")

    core_request.update(change)
    with pytest.raises(ceph_native.Refusal, match=reason):
        ceph_core.execute(core_request, operation, "d" * 64, NoTransport())


@pytest.mark.parametrize("change,reason", UNSUPPORTED)
def test_remote_dispatch_cannot_bypass_the_supported_transport(core_request, change, reason):
    core_request.update(change)
    with pytest.raises(ceph_native.Refusal, match=reason):
        ceph_native.dispatch({"request": core_request, "host": "node-1", "phase": "claim", "private": {}})


def test_dispatch_reaches_osd_prepare_only_over_a_supported_transport(core_request, monkeypatch):
    sentinel = {"changed": False, "sentinel": "osd_prepare"}
    reached = []

    def osd_prepare(worker):
        reached.append(worker.host["name"])
        return sentinel

    monkeypatch.setattr(ceph_native.Worker, "osd_prepare", osd_prepare)
    supported = dict(core_request, clusterSSHTransport="verified-tunnel")
    assert ceph_native.dispatch({"request": supported, "host": "node-1", "phase": "osd_prepare", "private": {}}) is sentinel
    assert reached == ["node-1"]
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": core_request, "host": "node-1", "phase": "osd_prepare", "private": {}})
    assert reached == ["node-1"]


def test_dispatch_records_the_ids_its_worker_applied(core_request, monkeypatch):
    def observe(worker):
        worker.selection.applied.update(("ceph-mgr-without-license-contract", "ceph-call-home-opt-in"))
        return observation(core_request)

    monkeypatch.setattr(ceph_native.Worker, "observe", observe)
    result = ceph_native.dispatch({"request": core_request, "host": "node-1", "phase": "observe", "private": {}})
    assert result["blocking"] == [] and ceph_native.APPLIED == ["ceph-call-home-opt-in", "ceph-mgr-without-license-contract"]
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    monkeypatch.setattr(ceph_host_runtime, "dispatch", lambda envelope: {"changed": False})
    assert ceph_native.dispatch({"request": core_request, "host": "node-1", "phase": "host_observe", "private": {}}) == {"changed": False}
    assert ceph_native.APPLIED == []


def specialized_transport(core_request, tmp_path, answers):
    key = tmp_path / "key"
    known = tmp_path / "known"
    key.write_text("PRIVATE-KEY-SENTINEL")
    known.write_text("192.0.2.1 ssh-ed25519 PUBLIC")
    key.chmod(0o600)
    known.chmod(0o600)
    pending = list(answers)
    return ceph_core.Transport(core_request, {"sshKey": str(key), "knownHosts": str(known)}, run=lambda argv, data, timeout: (0, pending.pop(0)))


def test_transport_collects_applied_specializations_and_refuses_unknown_ids(core_request, tmp_path):
    answers = [b'{"ok":true,"result":{"changed":false},"specializations":["ceph-mgr-without-license-contract"]}',
               b'{"ok":true,"result":{"changed":true},"specializations":["ceph-call-home-opt-in","ceph-mgr-without-license-contract"]}',
               b'{"ok":true,"result":{"changed":false}}']
    transport = specialized_transport(core_request, tmp_path, answers)
    assert [transport.call(core_request["hosts"][0], "observe")["changed"] for unused in answers] == [False, True, False]
    assert transport.applied == {"ceph-call-home-opt-in", "ceph-mgr-without-license-contract"}
    for value in (["ceph-unknown-specialization"], [], ["ceph-mgr-without-license-contract", "ceph-call-home-opt-in"],
                  ["ceph-call-home-opt-in", "ceph-call-home-opt-in"], "ceph-call-home-opt-in", None):
        refused = specialized_transport(core_request, tmp_path, [json.dumps({"ok": True, "result": {}, "specializations": value}).encode()])
        with pytest.raises(ceph_native.Refusal, match="^transport$"):
            refused.call(core_request["hosts"][0], "observe")
        assert refused.applied == set()


class AppliedTransport:
    """Answers a read-only observation as the native phases of an execution that applied the given ids would."""

    def __init__(self, core_request, applied, observed=None):
        self.core_request, self.applied, self.observed = core_request, set(applied), observed

    def call(self, host, phase, private=None, *, timeout=660):
        if phase == "host_observe":
            return copy.deepcopy(self.observed)
        if phase == "observe":
            return observation(self.core_request)
        return {"name": host["name"], "owned": True, "exists": False}


def host_block(core_request):
    cluster = {key: value for key, value in copy.deepcopy(core_request).items() if key != "action"}
    cluster["packages"] = {"version": "ceph-packages-v3", "licenseAccepted": True, "sources": [
        {"host": host["name"], "profile": "rhel", "media": "rhel-dvd.iso", "mediaSHA256": "e" * 64, "mediaSize": 1024,
         "publicationPath": "/var/lib/bootwright-services/example/public/os/rhel/tree", "url": "https://192.0.2.10:8443/os/rhel/tree",
         "certificateRef": "serving-tls"} for host in cluster["hosts"]]}
    request = {"version": "ceph-host-v5", "cluster": cluster, "host": "node-2"}
    record = {"version": "ceph-host-evidence-v2", "host": "node-2", "owner": cluster["owner"], "fsid": cluster["fsid"], "image": cluster["image"],
              "ready": False, "removed": False, "imageID": "", "uid": 0, "gid": 0, "label": "", "policySHA256": "", "cephadmSHA256": "",
              "planDigest": "", "route": False, "devices": []}
    return request, record


def test_core_and_host_evidence_name_the_applied_specializations(core_request):
    ids = ("ceph-mgr-without-license-contract", "ceph-call-home-opt-in")
    named = ceph_core.execute(core_request, "observe", "d" * 64, AppliedTransport(core_request, ids), UNWRITTEN)["evidence"]
    assert named["specializations"] == sorted(ids) and ceph_core.evidence(named, "d" * 64, core_request) is named
    plain = ceph_core.execute(core_request, "observe", "d" * 64, AppliedTransport(core_request, ()), UNWRITTEN)["evidence"]
    assert "specializations" not in plain and {key: value for key, value in named.items() if key != "specializations"} == plain
    request, record = host_block(core_request)
    named = ceph_core.execute_host(request, "observe", "d" * 64, AppliedTransport(core_request, ids, record))["evidence"]
    assert named["specializations"] == sorted(ids) and ceph_core.evidence(named, "d" * 64, request) is named
    plain = ceph_core.execute_host(request, "observe", "d" * 64, AppliedTransport(core_request, (), record))["evidence"]
    assert "specializations" not in plain
    released = ceph_core.execute_host(request, "destroy", "d" * 64, AppliedTransport(core_request, ids, record))["evidence"]
    assert "specializations" not in released


def test_unit_and_gateway_evidence_name_the_applied_specializations(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    ids = ("ceph-mgr-without-license-contract", "ceph-call-home-opt-in")
    for request, fake in ((service_units.unit_request("direct"), service_units.Transport), (object_gateway.gateway_request(), object_gateway.Transport)):
        for operation in ("observe", "apply"):
            evidences = []
            for applied in (ids, ()):
                transport = fake(request)
                transport.applied = set(applied)
                evidences.append(ceph_core.execute(request, operation, "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)["evidence"])
            named, plain = evidences
            assert named["specializations"] == sorted(ids) and ceph_core.evidence(named, "d" * 64, request) is named
            assert "specializations" not in plain and {key: value for key, value in named.items() if key != "specializations"} == plain


def test_evidence_without_specializations_still_reads(core_request):
    core = dict(observation(core_request), request="d" * 64)
    request, record = host_block(core_request)
    host = dict(record, request="d" * 64)
    assert ceph_core.evidence(core, "d" * 64, core_request) is core and ceph_core.evidence(host, "d" * 64, request) is host
    for value in ([], None, ["ceph-mgr-without-license-contract", "ceph-call-home-opt-in"], ["ceph-call-home-opt-in", "ceph-call-home-opt-in"],
                  ["ceph-unknown-specialization"], "ceph-call-home-opt-in"):
        for record, bound in ((core, core_request), (host, request)):
            with pytest.raises(ceph_native.Refusal, match="^evidence$"):
                ceph_core.evidence(dict(record, specializations=value), "d" * 64, bound)


def test_observation_calls_only_read_phases_and_all_hosts(core_request):
    class Transport:
        def __init__(self):
            self.calls = []

        def call(self, host, phase):
            self.calls.append((host["name"], phase))
            if phase == "observe":
                return observation(core_request)
            assert phase == "host_state"
            return {"name": host["name"], "owned": True, "exists": host["name"] == "node-3"}

    transport = Transport()
    result = ceph_core.execute(core_request, "observe", "d" * 64, transport, UNWRITTEN)
    assert result["changed"] is False
    assert result["evidence"]["partial"] is True
    assert result["evidence"]["removed"] is False
    assert transport.calls == [("node-1", "observe"), ("node-1", "host_state"), ("node-2", "host_state"), ("node-3", "host_state")]


def test_absence_requires_every_host_to_answer(core_request):
    class Transport:
        def call(self, host, phase):
            if phase == "observe":
                return observation(core_request)
            if host["name"] == "node-2":
                raise ceph_native.Refusal("transport")
            return {"name": host["name"], "owned": True, "exists": False}

    with pytest.raises(ceph_native.Refusal, match="transport"):
        ceph_core.execute(core_request, "observe", "d" * 64, Transport(), UNWRITTEN)


def test_evidence_rejects_remote_secret_and_identity_substitution(core_request):
    value = observation(core_request)
    value["request"] = "d" * 64
    assert ceph_core.evidence(value, "d" * 64, core_request) is value
    value["privateKey"] = "private"
    with pytest.raises(ceph_native.Refusal, match="evidence"):
        ceph_core.evidence(value, "d" * 64, core_request)
    del value["privateKey"]
    value["owner"] = "another-operation"
    with pytest.raises(ceph_native.Refusal, match="evidence"):
        ceph_core.evidence(value, "d" * 64, core_request)


RBD_IMAGE = {"dataPool": "", "features": ["layering"], "name": "disk", "pool": "volumes", "sizeBytes": 1024 ** 3}


def test_rbd_evidence_admits_only_a_lowercase_hex_image_id(core_request):
    request = dict(core_request, action="rbd", rbd=RBD_IMAGE)
    value = dict(observation(request), request="d" * 64, resource={"id": "10226b8b4567", "path": ""})
    assert ceph_core.evidence(value, "d" * 64, request) is value
    for refused in ("ABC", "10226B8B4567", "a" * 15, "12-34"):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(dict(value, resource={"id": refused, "path": ""}), "d" * 64, request)
    pool = dict(observation(core_request), request="d" * 64, action="pool", resource={"id": "abc", "path": ""})
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(pool, "d" * 64)


def test_nfs_export_evidence_admits_only_its_export_id_and_pseudo_path(core_request):
    request = dict(core_request, action="nfs-export", nfsExport={"native": {"pseudo": "/docs"}})
    value = dict(observation(request, ready=True), request="d" * 64, resource={"id": "7", "path": "/docs"})
    assert ceph_core.evidence(value, "d" * 64, request) is value
    assert ceph_core.evidence(dict(value, ready=False, partial=True, resource={"id": "", "path": ""}), "d" * 64, request)["resource"]["id"] == ""
    refused = [dict(value, resource={"id": "0", "path": "/docs"}), dict(value, resource={"id": "x", "path": "/docs"}),
               dict(value, resource={"id": "7", "path": "/other"}), dict(value, resource={"id": "7", "path": "/"}),
               dict(value, resource={"id": "7", "path": ""}), dict(value, client="client.docs"),
               dict(value, daemons=[{"host": "node-1", "name": "nfs.shares.0.0.node-1.abcdef", "service": "nfs.shares"}])]
    for case in refused:
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(case, "d" * 64, request)


def test_rbd_apply_calls_the_rbd_phase(core_request, monkeypatch):
    request = dict(core_request, action="rbd", rbd=RBD_IMAGE)
    calls = []

    class Transport:
        def call(self, host, phase, timeout=None):
            del timeout
            calls.append(phase)
            if phase == "observe":
                done = "rbd" in calls
                return dict(observation(request, ready=done), request="", removed=False, exists=done, partial=not done,
                            resource={"id": "10226b8b4567" if done else "", "path": ""})
            if phase == "host_state":
                return {"name": host["name"], "owned": True, "exists": True}
            return {"changed": phase == "rbd"}

    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    result = ceph_core.execute(request, "apply", "d" * 64, Transport())
    assert result["changed"] and result["evidence"]["resource"] == {"id": "10226b8b4567", "path": ""}
    assert calls.count("rbd") == 1 and calls.index("rbd") > calls.index("observe")


def test_ssh_uses_only_pinned_trust_and_literal_fixed_worker(core_request):
    argv = ceph_core.ssh_argv(core_request["hosts"][0], "/private/key", "/private/known", "print('fixed')",
                              ssh_config="/private/ssh_config", host_key_algorithms="ssh-ed25519")
    assert argv[0] == "/usr/bin/ssh"
    assert argv[argv.index("-F") + 1] == "/private/ssh_config"
    for value in ("StrictHostKeyChecking=yes", "UserKnownHostsFile=/private/known", "GlobalKnownHostsFile=none", "IdentityAgent=none", "ProxyCommand=none",
                  "BatchMode=yes", "HostKeyAlgorithms=ssh-ed25519"):
        assert value in argv
    assert argv[-1].startswith("/usr/bin/sudo -n /usr/bin/python3 -I -c ")
    assert "accept-new" not in " ".join(argv)


def test_worker_budget_stays_within_a_fixed_launcher_ceiling(core_request):
    host = core_request["hosts"][0]
    session = {"ssh_config": "/private/ssh_config", "host_key_algorithms": "ssh-ed25519"}
    assert ceph_core.ssh_argv(host, "/private/key", "/private/known", "print('fixed')", 4285, worker_ceiling=4285, **session)[-1].endswith(" 4285")
    for budget, ceiling in ((641, 640), (4285, 1920), (700, 700)):
        with pytest.raises(ceph_native.Refusal, match="transport"):
            ceph_core.ssh_argv(host, "/private/key", "/private/known", "print('fixed')", budget, worker_ceiling=ceiling, **session)


def test_authorize_refuses_an_excluded_tunnel_pin_before_any_effect(core_request, tmp_path, monkeypatch):
    request = dict(core_request, clusterSSHTransport="verified-tunnel")
    markers = tunnel_native.Markers(tmp_path / request["fsid"])
    effects = []
    native = ceph_native.Worker(request, "node-1", run=lambda *args, **kwargs: effects.append("run"), markers=markers)
    native.selection = specs.selection(specs.synthesized(request["cephadm"]["packageVersion"]))
    tunnel_native.seed_profile(native)
    native._profile["policy"] = "Ciphers aes256-gcm@openssh.com\nHostKeyAlgorithms ecdsa-sha2-nistp256,rsa-sha2-512\n"
    for name in ("registry_preflight", "prepare_registry", "tunnel_policy_preflight", "transport", "tunnel_guest_policy"):
        setattr(native, name, lambda *args, name=name: effects.append(name))
    monkeypatch.setattr(ceph_native.pwd, "getpwnam", lambda user: effects.append("account") or ceph_native.require(False, "cluster-account"))
    public = tunnel_native.public_key()
    pins = {"192.0.2.1": "ecdsa-sha2-nistp256", "192.0.2.2": "ssh-ed25519", "192.0.2.3": "ssh-rsa"}
    blobs = {key_type: base64.b64encode(len(key_type).to_bytes(4, "big") + key_type.encode() + b"\0\0\0\x20" + b"k" * 32).decode()
             for key_type in pins.values()}
    known = "".join(address + " " + key_type + " " + blobs[key_type] + "\n" for address, key_type in pins.items())
    private = {"clusterPublicKey": public, "clusterPrivateKey": "private fixture", "knownHosts": known,
               "registryJSON": json.dumps({"url": request["registry"], "username": "fixture-user", "password": PASSWORD})}
    with pytest.raises(ceph_native.NativeFailure, match="^tunnel-host-key-excluded$") as refused:
        native.authorize(private)
    assert refused.value.native_error["cause"] == ("host node-2 pins a ssh-ed25519 host key, and the HostKeyAlgorithms of the selected image's SSH "
                                                   "crypto policy admit none of its algorithms")
    assert not any(blob in repr(refused.value.native_error) for blob in blobs.values()) and PASSWORD not in repr(refused.value.native_error)
    assert effects == [] and markers.writes == []
    assert ceph_core.refusal(ceph_native.RemoteRefusal("native-refusal:tunnel-host-key-excluded@ceph_native.authorize:1"),
                             ceph_core.CLUSTER_REASONS)[0] == "tunnel-host-key-excluded"
    with pytest.raises(ceph_native.NativeFailure, match="^tunnel-host-key-excluded$"):
        native.tunnel_preflight({"knownHosts": known})
    assert effects == [] and markers.writes == []
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        native.tunnel_preflight(private)


def test_an_excluded_tunnel_pin_refuses_before_any_host_is_claimed_or_authorized(core_request, monkeypatch):
    core_request["clusterSSHTransport"] = "verified-tunnel"
    package_intent(core_request)
    names = [host["name"] for host in core_request["hosts"]]
    managers = names[1:]
    for service in core_request["services"]:
        if service["service_type"] == "mgr":
            service["placement"]["hosts"] = managers
    bootstrap = core_request["hosts"][1]
    core_request["bootstrap"] = bootstrap["name"]

    class Refusing(ReadinessTransport):
        def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
            if phase == "tunnel_preflight":
                self.calls.append((host["name"], phase, timeout))
                assert set(private) == {"knownHosts"}
                if host["name"] != names[-1]:
                    return {"changed": False}
                raise ceph_native.RemoteRefusal("native-refusal:tunnel-host-key-excluded@ceph_native._tunnel_host_keys:12")
            return super().call(host, phase, private, timeout=timeout, devices=devices, service=service)

    transport = Refusing(core_request, ReadinessClock())
    monkeypatch.setattr(ceph_tunnel_staging, "prove_recovery_record", lambda *args, **kwargs: "unclaimed")
    preparations = [{"name": name, "owned": False, "exists": False, "recovery": None} for name in names]
    with pytest.raises(ceph_native.RemoteRefusal):
        ceph_core.apply_tunnel_cluster(core_request, bootstrap, transport, None, preparations)
    phases = [(host, phase) for host, phase, unused in transport.calls]
    assert [host for host, phase in phases if phase == "tunnel_preflight"] == managers
    assert not [phase for unused, phase in phases if phase in ("claim", "authorize", "bootstrap")]


def test_private_input_is_opened_without_links_and_not_in_argv(core_request, tmp_path):
    key = tmp_path / "key"
    known = tmp_path / "known"
    key.write_text("PRIVATE-KEY-SENTINEL")
    known.write_text("192.0.2.1 ssh-ed25519 PUBLIC")
    key.chmod(0o600)
    known.chmod(0o600)
    calls = []

    def run(argv, data, timeout):
        calls.append((argv, data, timeout))
        assert "PRIVATE-KEY-SENTINEL" not in repr(argv)
        assert "REGISTRY-SECRET-SENTINEL" not in repr(argv)
        envelope = json.loads(data)
        assert set(envelope) == {"sources", "payload", "budget"}
        assert envelope["payload"]["private"] == {"registryJSON": "REGISTRY-SECRET-SENTINEL"}
        assert set(envelope["sources"]) == {"ceph_native", "ceph_native_observe", "ceph_native_resources", "ceph_tunnel", "ceph_tunnel_staging",
                                            "ceph_tunnel_custody", "ceph_host_route_probe", "ceph_host_runtime", "native_diagnostics",
                                            "native_specializations", "ceph_pull_broker", "ceph_native_nfs", "ceph_native_observability",
                                            "ceph_native_units", "ceph_native_object", "ceph_smb_native"}
        assert 0 < envelope["budget"] <= min(640, timeout - 1)
        return 0, b'{"ok":true,"result":{"changed":false}}'

    transport = ceph_core.Transport(core_request, {"sshKey": str(key), "knownHosts": str(known)}, run=run)
    assert transport.call(core_request["hosts"][0], "authorize", {"registryJSON": "REGISTRY-SECRET-SENTINEL"}) == {"changed": False}
    assert len(calls) == 1 and calls[0][2] == 660
    assert transport.call(core_request["hosts"][0], "authorize", {"registryJSON": "REGISTRY-SECRET-SENTINEL"}, timeout=7.5) == {"changed": False}
    assert len(calls) == 2 and calls[1][2] == 7.5
    for timeout in (0, -1, 661, True, None, float("nan")):
        with pytest.raises(ceph_native.Refusal, match="^transport$"):
            transport.call(core_request["hosts"][0], "observe", timeout=timeout)
    assert len(calls) == 2
    link = tmp_path / "link"
    link.symlink_to(key)
    with pytest.raises(OSError):
        ceph_native.read_regular(str(link), private=True)


@pytest.mark.parametrize("mutation", ["mounted", "filesystem", "partition", "wrong-serial", "wrong-size", "signature"])
def test_device_proof_never_accepts_foreign_or_in_use_disk(core_request, mutation):
    disk = {"path": "/dev/vdb", "type": "disk", "serial": "disk1", "size": 1024 ** 3, "fstype": None, "mountpoints": [None]}
    signatures = []
    if mutation == "mounted":
        disk["mountpoints"] = ["/"]
    elif mutation == "filesystem":
        disk["fstype"] = "LVM2_member"
    elif mutation == "partition":
        disk["children"] = [{"path": "/dev/vdb1"}]
    elif mutation == "wrong-serial":
        disk["serial"] = "another-disk"
    elif mutation == "wrong-size":
        disk["size"] += 512
    else:
        signatures = [{"type": "linux_raid_member"}]
    answers = {("/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "PATH,TYPE,SERIAL,SIZE,FSTYPE,MOUNTPOINTS"): {"blockdevices": [disk]},
               ("/usr/sbin/wipefs", "--no-act", "--json", "/dev/vdb"): {"signatures": signatures}}
    native, calls = worker(core_request, answers)
    with pytest.raises(ceph_native.Refusal, match="device-"):
        native.devices()
    assert all("--no-act" in argv or argv[0] == "/usr/bin/lsblk" for argv in calls)


def test_foreign_cluster_is_not_absence(core_request):
    native, unused = worker(core_request, {("/usr/sbin/cephadm", "ls"): [{"fsid": "foreign"}]}, Markers(False))
    with pytest.raises(ceph_native.Refusal, match="foreign-cluster"):
        native.observe()


def test_fsid_json_object_is_checked_before_other_cluster_reads(core_request):
    native, calls = worker(core_request, {("/usr/sbin/cephadm", "ls"): [{"fsid": core_request["fsid"]}], ("fsid",): {"fsid": "foreign"}})
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.observe()
    assert len(calls) == 1 and calls[0][-1] == "fsid"


def test_pool_numeric_identity_must_survive_recreation(core_request):
    core_request.update(action="pool", pool=replicated_pool())
    marks = Markers()
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-id-volumes.json"] = {"id": 4}
    native, calls = worker(core_request, {("osd", "pool", "ls", "detail"): [{"pool_name": "volumes", "pool_id": 8}]}, marks)
    with pytest.raises(ceph_native.Refusal, match="pool-identity"):
        native.pool()
    assert len(calls) == 1


REPLICATED_RULE = {"rule_id": 0, "rule_name": "replicated_rule", "type": 1,
                   "steps": [{"op": "take", "item": -1, "item_name": "default"}, {"op": "chooseleaf_firstn", "num": 0, "type": "host"}, {"op": "emit"}]}


def pool_world(core_request, pools, effects):
    """A scripted native pool table; create appends one pool, as the provider does."""
    def run(argv, data=b"", timeout=60):
        del data, timeout
        command = tuple(argv)
        if "--format" in argv:
            command = tuple(argv[argv.index("--format") + 2:])
        elif argv[0] == "/usr/bin/ceph":
            command = tuple(argv[argv.index("--keyring") + 2:])
        if command == ("osd", "pool", "ls", "detail"):
            return 0, json.dumps(pools).encode()
        if command == ("osd", "crush", "rule", "dump", "replicated_rule"):
            return 0, json.dumps(REPLICATED_RULE).encode()
        if command in (("osd", "crush", "rule", "ls"), ("osd", "erasure-code-profile", "ls")):
            return 0, json.dumps(["replicated_rule"] if command[1] == "crush" else ["default"]).encode()
        effects.append(command)
        if command[:3] == ("osd", "pool", "create"):
            pools.append({"pool_name": command[3], "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": int(command[4]),
                          "size": 3, "min_size": 2, "quota_max_bytes": 0, "quota_max_objects": 0, "options": {}, "flags_names": "hashpspool",
                          "application_metadata": {}})
        if command[:3] == ("osd", "pool", "set"):
            pools[0][command[4]] = int(command[5])
        if command[0] == "/usr/bin/rbd":
            pools[0]["application_metadata"] = {"rbd": {}}
        return 0, b""
    return run


def pool_worker(core_request, marks, pools, effects):
    core_request.update(action="pool", pool=replicated_pool())
    for value in pools:
        value.setdefault("size", 3)
        value.setdefault("min_size", 2)
        value.setdefault("quota_max_bytes", 0)
        value.setdefault("quota_max_objects", 0)
        value.setdefault("options", {})
        value.setdefault("flags_names", "hashpspool")
    native = ceph_native_resources.ResourceWorker(core_request, core_request["bootstrap"], run=pool_world(core_request, pools, effects), markers=marks)
    return native


def pool_attempt(core_request):
    return {"version": "ceph-pool-attempt-v2", "owner": core_request["owner"], "fsid": core_request["fsid"], "pool": core_request["pool"]}


def test_pool_creation_journals_its_attempt_before_the_create(core_request):
    marks, pools, effects = Markers(), [], []
    native = pool_worker(core_request, marks, pools, effects)
    write = marks.write
    marks.write = lambda name, value: effects.append(("write", name)) or write(name, value)
    assert native.pool() == {"changed": True}
    creates = [index for index, effect in enumerate(effects) if effect[:3] == ("osd", "pool", "create")]
    assert len(creates) == 1
    assert effects.index(("write", "pool-attempt-volumes.json")) < creates[0] < effects.index(("write", "pool-id-volumes.json"))
    assert marks.records["pool-attempt-volumes.json"] == pool_attempt(core_request)


def test_a_lost_pool_creation_response_resumes_without_a_second_create(core_request):
    marks, effects = Markers(), []
    pools = [{"pool_name": "volumes", "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": 32}]
    native = pool_worker(core_request, marks, pools, effects)
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(core_request)
    assert native.pool() == {"changed": True}
    assert not any(effect[:3] == ("osd", "pool", "create") for effect in effects)
    assert marks.records["pool-id-volumes.json"] == {"id": 8}
    assert ("osd", "pool", "set", "volumes", "size", "2") in effects


@pytest.mark.parametrize("case", ["no-attempt", "foreign-attempt", "rule", "pg", "erasure", "two"])
def test_a_same_name_pool_without_this_operations_creation_refuses(core_request, case):
    marks, effects = Markers(), []
    pools = [{"pool_name": "volumes", "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": 32}]
    native = pool_worker(core_request, marks, pools, effects)
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(core_request)
    if case == "no-attempt":
        del marks.records["pool-attempt-volumes.json"]
    elif case == "foreign-attempt":
        marks.records["pool-attempt-volumes.json"] = dict(pool_attempt(core_request), owner="apply-other")
    elif case == "rule":
        pools[0]["crush_rule"] = 3
    elif case == "pg":
        pools[0]["pg_num"] = 64
    elif case == "erasure":
        pools[0]["type"] = 3
    else:
        pools.append(dict(pools[0], pool_id=9))
    with pytest.raises(ceph_native.Refusal, match="^pool-identity$"):
        native.pool()
    assert effects == [] and "pool-id-volumes.json" not in marks.records


def test_a_foreign_same_name_pool_never_owned_refuses_before_any_record(core_request):
    marks, effects = Markers(), []
    pools = [{"pool_name": "volumes", "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": 32}]
    native = pool_worker(core_request, marks, pools, effects)
    with pytest.raises(ceph_native.Refusal, match="^foreign-pool$"):
        native.pool()
    assert effects == [] and marks.writes == []


@pytest.mark.parametrize("mode", ["on", "warn", "off", None])
def test_a_lost_creation_resumes_after_the_autoscaler_moved_pg_num_only_when_it_is_on(core_request, mode):
    marks, effects = Markers(), []
    pools = [{"pool_name": "volumes", "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": 1}]
    if mode is not None:
        pools[0]["pg_autoscale_mode"] = mode
    native = pool_worker(core_request, marks, pools, effects)
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(core_request)
    if mode == "on":
        assert native.pool() == {"changed": True}
        assert marks.records["pool-id-volumes.json"] == {"id": 8}
        assert not any(effect[:3] == ("osd", "pool", "create") for effect in effects)
        return
    with pytest.raises(ceph_native.Refusal, match="^pool-identity$"):
        native.pool()
    assert effects == [] and "pool-id-volumes.json" not in marks.records


def test_a_pool_listing_without_pool_id_refuses_as_native_output(core_request):
    marks, effects = Markers(), []
    pools = [{"pool_name": "volumes", "pool": 8, "type": 1, "crush_rule": 0, "pg_num": 32}]
    native = pool_worker(core_request, marks, pools, effects)
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(core_request)
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.pool()
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.observe_resource(native.empty_evidence(), True)
    assert effects == [] and "pool-id-volumes.json" not in marks.records


def observed_pool(core_request, marks):
    pools = [{"pool_name": "volumes", "pool_id": 8, "type": 1, "crush_rule": 0, "pg_num": 32, "size": 2, "min_size": 2, "pg_autoscale_mode": "on",
              "flags_names": "hashpspool,selfmanaged_snaps", "quota_max_bytes": 0, "quota_max_objects": 0, "erasure_code_profile": "", "stripe_width": 0,
              "options": {}, "application_metadata": {"rbd": {}}}]
    native = pool_worker(core_request, marks, pools, [])
    marks.records["pool-volumes.json"] = core_request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(core_request)
    return native


def test_a_pool_this_operation_created_without_a_recorded_id_observes_as_unfinished(core_request):
    marks = Markers()
    native = observed_pool(core_request, marks)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is False
    assert evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    marks.records["pool-id-volumes.json"] = {"id": 8}
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is True
    assert evidence["resource"] == {"id": "8", "path": ""}


@pytest.mark.parametrize("case", ["no-attempt", "foreign-attempt", "other-id"])
def test_an_unproved_pool_id_still_refuses_observation(core_request, case):
    marks = Markers()
    native = observed_pool(core_request, marks)
    if case == "no-attempt":
        del marks.records["pool-attempt-volumes.json"]
    elif case == "foreign-attempt":
        marks.records["pool-attempt-volumes.json"] = dict(pool_attempt(core_request), owner="apply-other")
    else:
        marks.records["pool-id-volumes.json"] = {"id": 4}
    with pytest.raises(ceph_native.Refusal, match="^pool-identity$"):
        native.observe_resource(native.empty_evidence(), True)


def test_client_caps_and_legacy_hash_binding_refuse_unproved_identity(core_request, monkeypatch):
    core_request.update(action="client", client={"name": "reader", "pool": "volumes", "access": "read-only", "dataPools": [],
                                                 "filesystem": "", "group": "", "subvolume": ""})
    marks = Markers()
    marks.records["pool-volumes.json"] = {"name": "volumes"}
    marks.records["client-reader.json"] = core_request["client"]
    marks.records["client-id-reader.json"] = {"keyHash": hashlib.sha256(b"original").hexdigest()}
    native, calls = worker(core_request, {("auth", "ls"): {"auth_dump": [{"entity": "client.reader", "key": "replacement"}]}}, marks)
    monkeypatch.setattr(native, "client_scope", lambda: {"synthetic-scope": True})
    assert native.caps() == {"mon": "profile rbd", "mgr": "profile rbd-read-only pool=volumes", "osd": "profile rbd-read-only pool=volumes"}
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        native.client()
    assert calls == []


def test_destroy_never_treats_healthy_as_quiescent(core_request, monkeypatch):
    native, calls = worker(core_request, {})
    monkeypatch.setattr(native, "observe", lambda: observation(core_request, ready=True))
    with pytest.raises(ceph_native.Refusal, match="client-quiescence-unqualified"):
        native.destroy()
    assert calls == []


def test_marker_publication_is_whole_private_and_does_not_replace(core_request, tmp_path):
    markers = ceph_native.Markers(core_request, root=str(tmp_path))
    os.mkdir(markers.path, 0o700)
    markers.write("owner.json", ceph_native.marker_identity(core_request))
    assert markers.owned()
    assert os.stat(os.path.join(markers.path, "owner.json")).st_mode & 0o777 == 0o600
    assert os.stat(os.path.join(markers.path, "owner.json")).st_nlink == 1
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        markers.write("owner.json", {"foreign": True})
    assert os.listdir(markers.path) == ["owner.json"]


def test_bounded_process_drains_input_and_limits_combined_output():
    code, output = ceph_native.bounded_command([sys.executable, "-c",
                                                "import sys; data=sys.stdin.buffer.read(); sys.stderr.write('note'); sys.stdout.buffer.write(data)"],
                                               data=b"x" * 131072)
    assert code == 0 and output == b"x" * 131072
    with pytest.raises(ceph_native.Refusal, match="native-output"):
        ceph_native.bounded_command([sys.executable, "-c", "import sys; sys.stderr.write('x'*8192)"], limit=128)


def test_bounded_process_timeout_never_returns_a_native_transcript():
    with pytest.raises(ceph_native.Refusal) as caught:
        ceph_native.bounded_command([sys.executable, "-c", "import time; print('PRIVATE-SENTINEL', flush=True); time.sleep(10)"], timeout=0.05)
    assert "PRIVATE-SENTINEL" not in str(caught.value)


def test_client_recovery_reads_only_the_owned_principal(core_request, monkeypatch):
    core_request["action"] = "client"
    core_request["client"] = {"name": "consumer", "pool": "volumes", "access": "read-only", "dataPools": [], "filesystem": "", "group": "", "subvolume": ""}
    markers = Markers()
    markers.records["client-consumer.json"] = core_request["client"]
    key = "PRIVATE-KEY-SENTINEL"
    caps = {"mon": "profile rbd", "mgr": "profile rbd-read-only pool=volumes", "osd": "profile rbd-read-only pool=volumes"}
    material = [{"entity": "client.consumer", "key": key, "caps": caps}]
    native, calls = worker(core_request, {("auth", "get", "client.consumer"): copy.deepcopy(material)}, markers)
    scope = {"owner": core_request["owner"], "fsid": core_request["fsid"], "host": core_request["bootstrap"], "client": core_request["client"], "poolID": 4}
    binding = {"version": "ceph-client-auth-v1", "scope": scope, "custodyID": "a" * 32}
    markers.records["client-attempt-consumer.json"] = binding
    monkeypatch.setattr(native, "client_scope", lambda: binding["scope"])
    monkeypatch.setattr(native, "client_material", lambda scope: material)
    monkeypatch.setattr(native, "client_binding", lambda scope: binding)
    result = native.client_read()
    assert json.loads(result["private"])[0]["key"] == key
    assert len(calls) == 1
    assert markers.writes == []
    markers.records["client-attempt-consumer.json"] = dict(binding, custodyID="b" * 32)
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        native.client_read()


def test_absent_cluster_does_not_prove_unowned_pool_was_removed(core_request):
    core_request["action"] = "pool"
    core_request["pool"] = replicated_pool()
    native, unused = worker(core_request, {(core_request["cephadm"]["path"], "ls"): []})
    result = native.observe()
    assert not result["owned"]
    assert not result["removed"]


def test_owned_cluster_without_daemons_does_not_prove_cleanup(core_request):
    native, unused = worker(core_request, {(core_request["cephadm"]["path"], "ls"): []})
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"]
    assert not result["removed"]

    class Transport:
        def call(self, host, phase):
            if phase == "observe":
                return copy.deepcopy(result)
            assert phase == "host_state"
            return {"name": host["name"], "owned": True, "exists": False}

    result = ceph_core.execute(core_request, "observe", "d" * 64, Transport(), UNWRITTEN)
    assert result["evidence"]["partial"]
    assert not result["evidence"]["removed"]


@pytest.mark.parametrize("phase", ["observe", "host_state", "client_read"])
def test_native_reads_require_unchanged_artifacts_before_observation(core_request, phase):
    if phase == "client_read":
        core_request.update(action="client", client={"name": "consumer", "pool": "volumes", "access": "read-only", "dataPools": [],
                                                     "filesystem": "", "group": "", "subvolume": ""})
    native, calls = worker(core_request, {})

    def refuse_artifact():
        raise ceph_native.Refusal("artifact")

    native.verify_tool = refuse_artifact
    with pytest.raises(ceph_native.Refusal, match="artifact"):
        getattr(native, phase)()
    assert calls == []
    assert native.markers.writes == []


def test_artifact_verification_refuses_drift_and_missing_owned_manifest(core_request, monkeypatch):
    decided = tunnel_native.Decided(core_request["cephadm"]["packageVersion"])
    answers = {}
    for name in ("cephadm", "ceph-common"):
        query = ("/usr/bin/rpm", "-q", "--qf", tunnel_native.PACKAGE_QUERY, name)
        answers[query] = (0, decided.rpm(list(query)))
        answers[("/usr/bin/rpm", "--verify", "--noscripts", name)] = (0, b"")
    markers = Markers(owned=False)
    native, unused = worker(core_request, answers, markers)
    native.packages = decided
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, maximum: path.encode())
    identity = ceph_native.Worker.verify_tool(native)
    assert markers.writes == []
    markers.is_owned = True
    with pytest.raises(ceph_native.Refusal, match="artifact"):
        ceph_native.Worker.verify_tool(native)
    markers.records["artifact.json"] = identity
    assert ceph_native.Worker.verify_tool(native) == identity
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, maximum: b"replaced-native-client")
    with pytest.raises(ceph_native.Refusal, match="artifact"):
        ceph_native.Worker.verify_tool(native)
    assert markers.writes == []


def memory_config(target="2147483648"):
    return {"osd": {"osd_memory_target_autotune": "false", "osd_memory_target": target}}


def memory_database():
    return [{"section": "osd", "mask": "", "name": name, "value": value,
             "level": "basic" if name == "osd_memory_target" else "advanced", "can_update_at_runtime": name == "osd_memory_target"}
            for name, value in memory_config()["osd"].items()]


@pytest.mark.parametrize("config", [
    None, {}, {"global": {"osd_memory_target": "2147483648"}}, {"osd": {"osd_memory_target": "2147483648"}},
    {"osd": {"osd_memory_target_autotune": True, "osd_memory_target": "2147483648"}},
    {"osd": {"osd_memory_target_autotune": "true", "osd_memory_target": "2147483648"}},
    {"osd": {"osd_memory_target_autotune": "false", "osd_memory_target": "2147483648", "password": "never-echo-this"}},
    *[memory_config(value) for value in (2147483648, "2147483647", "2G", "02147483648", "18446744073709551616",
                                         "2147483648\n[global]\nauth_client_required=none")],
])
def test_unqualified_memory_input_refuses_before_native_calls(core_request, config):
    core_request["config"] = config
    with pytest.raises(ceph_native.Refusal, match="^config-unqualified$") as refused:
        worker(core_request, {})
    assert "never-echo-this" not in str(refused.value)


def test_memory_rendering_is_explicit_deterministic_and_never_changes_defaults(core_request):
    unconfigured = ceph_native.bootstrap_config(core_request)
    assert b"[osd]" not in unconfigured
    core_request["config"] = memory_config()
    rendered = ceph_native.bootstrap_config(core_request)
    assert rendered == unconfigured + b"\n[osd]\nosd_memory_target = 2147483648\nosd_memory_target_autotune = false\n"
    core_request["config"]["osd"] = dict(reversed(list(core_request["config"]["osd"].items())))
    assert ceph_native.bootstrap_config(core_request) == rendered
    assert b"osd_pool_default" not in rendered
    for target in ("2147483648", "4294967296", "18446744073709551615"):
        core_request["config"] = memory_config(target)
        assert ceph_native.validate(core_request) is core_request
    core_request["version"] = "ceph-core-v1"
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(core_request)


def test_memory_intent_is_part_of_exact_durable_owner_identity(core_request, tmp_path):
    core_request["config"] = memory_config()
    marks = ceph_native.Markers(core_request, str(tmp_path))
    os.mkdir(marks.path, 0o700)
    marks.write("owner.json", ceph_native.marker_identity(core_request))
    original = marks.read()
    assert original["config"] == memory_config()
    core_request["config"] = memory_config("4294967296")
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        marks.owned()
    assert marks.read() == original


@pytest.mark.parametrize("global_options", [
    {}, {"osd_pool_default_size": "2"}, {"osd_pool_default_size": 2, "osd_pool_default_min_size": "2"},
    {"osd_pool_default_size": "3", "osd_pool_default_min_size": "2"},
    {"osd_pool_default_size": "2", "osd_pool_default_min_size": "1"},
    {"osd_pool_default_size": "2", "osd_pool_default_min_size": "02"},
    {"osd_pool_default_size": "2", "osd_pool_default_min_size": "2", "password": "never-echo-this"},
])
def test_unqualified_pool_defaults_refuse_before_native_calls(core_request, global_options):
    core_request["config"] = {"global": global_options}
    with pytest.raises(ceph_native.Refusal, match="^config-unqualified$") as refused:
        worker(core_request, {})
    assert "never-echo-this" not in str(refused.value)


@pytest.mark.parametrize("with_memory", [False, True])
def test_bootstrap_pool_defaults_share_one_global_section_and_keep_omission(core_request, with_memory):
    original = ceph_native.bootstrap_config(core_request)
    assert b"osd_pool_default" not in original
    defaults = {"osd_pool_default_size": "2", "osd_pool_default_min_size": "2"}
    core_request["config"] = memory_config() if with_memory else {}
    core_request["config"]["global"] = defaults
    rendered = ceph_native.bootstrap_config(core_request)
    assert rendered.count(b"[global]") == 1
    expected = original + b"osd_pool_default_min_size = 2\nosd_pool_default_size = 2\n"
    if with_memory:
        expected += b"\n[osd]\nosd_memory_target = 2147483648\nosd_memory_target_autotune = false\n"
    assert rendered == expected
    records = [{"section": "global", "mask": "", "name": name, "value": value} for name, value in defaults.items()]
    if with_memory:
        records += memory_database()
    native, unused = worker(core_request, {("config", "dump"): records})
    native.config_database()
    if not with_memory:
        native.config_effective(0)
    records[0]["section"] = "osd"
    with pytest.raises(ceph_native.Refusal, match="^config-drift$"):
        native.config_database()


def test_bootstrap_consumes_private_fixed_memory_ini_and_verifies_persisted_result(core_request, tmp_path, monkeypatch):
    core_request["config"] = memory_config()
    marks = Markers()
    marks.path = str(tmp_path)
    native, calls = worker(core_request, {("fsid",): {"fsid": core_request["fsid"]}, ("config", "dump"): memory_database(),
                                          ("orch", "accept", "call-home-enabled"): (0, b"Health warning for call home enablement cleared\n")}, marks)
    monkeypatch.setattr(native, "daemons", lambda: [])
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    normal_command, rendered = native.command, []

    def command(argv, **options):
        if argv[0] == native.tool and "bootstrap" in argv:
            config = argv[argv.index("--config") + 1]
            assert os.stat(config).st_mode & 0o777 == 0o600
            with open(config, "rb") as source:
                rendered.append(source.read())
            assert "--skip-prepare-host" not in argv
            assert "--skip-dashboard" in argv and "--skip-monitoring-stack" in argv
            assert argv.count("--skip-pull") == 1 and argv.index("--skip-pull") > argv.index("bootstrap")
            assert "--no-minimize-config" not in argv
            assert all(not argument.startswith("--registry") and "registryJSON" not in argument for argument in argv)
            assert "registryJSON" not in os.listdir(os.path.dirname(config))
            return b""
        return normal_command(argv, **options)

    monkeypatch.setattr(native, "command", command)
    private = {"clusterPrivateKey": "private-key-fixture", "clusterPublicKey": "public-key-fixture", "knownHosts": "pinned-host-fixture"}
    assert native.bootstrap(private) == {"changed": True}
    assert rendered == [ceph_native.bootstrap_config(core_request)]
    assert not list(tmp_path.glob(".bootstrap-*"))
    monkeypatch.setattr(native, "daemons", lambda: [{"fsid": core_request["fsid"]}])
    assert native.bootstrap(private) == {"changed": False}
    assert len(rendered) == 1
    assert sum("dump" in argv for argv in calls) == 2
    assert [argv[argv.index("--format") + 2:] for argv in calls] == [
        ["fsid"], ["config", "dump"], ["orch", "accept", "call-home-enabled"], ["fsid"], ["config", "dump"],
    ]
    assert marks.writes == ["call-home-accepted.json"]


@pytest.mark.parametrize("mutation", ["missing", "duplicate", "scope", "host-mask", "mask-fields", "value", "type", "competing-default"])
def test_config_drift_prevents_any_osd_service_effect(core_request, mutation):
    core_request["config"] = memory_config()
    records = memory_database()
    if mutation == "missing":
        records.pop()
    elif mutation == "duplicate":
        records.append(copy.deepcopy(records[0]))
    elif mutation == "scope":
        records[0]["section"] = "osd.0"
    elif mutation == "host-mask":
        records[0]["mask"] = "host:node-1"
    elif mutation == "mask-fields":
        records[0]["location_type"] = ""
    elif mutation == "value":
        records[0]["value"] = "true"
    elif mutation == "type":
        records[0]["value"] = False
    else:
        other = copy.deepcopy(records[0])
        other["section"] = "global"
        records.append(other)
    native, calls = worker(core_request, {("fsid",): {"fsid": core_request["fsid"]}, ("config", "dump"): records})
    with pytest.raises(ceph_native.Refusal, match="^config-drift$"):
        native.services()
    assert all("apply" not in argv for argv in calls)
    assert native.markers.writes == []


def test_config_database_is_reproved_immediately_before_services(core_request):
    core_request["config"] = memory_config()
    native, calls = worker(core_request, {("fsid",): {"fsid": core_request["fsid"]}, ("config", "dump"): memory_database(),
                                          ("orch", "apply", "-i", "-"): (0, b"")})
    records = {}
    native.recovery_scope = lambda: {"cluster": copy.deepcopy(core_request)}
    native.normalized_services = lambda groups: groups
    native.service_specs = lambda: copy.deepcopy(records)

    def effect(*args, **kwargs):
        native.ceph(*args, **kwargs)
        value = json.loads(kwargs["data"])
        records[value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")] = value

    native.tunnel_effect = effect
    assert native.services() == {"changed": True}
    assert [argv[argv.index("--format") + 2:] for argv in calls[:2]] == [["fsid"], ["config", "dump"]]
    assert len(records) == 5


def test_services_preserve_native_mappings_as_separate_yaml_documents(core_request):
    original = copy.deepcopy(core_request)
    received = []
    observed = {}

    def run(argv, data=b"", timeout=60):
        del timeout
        command = argv[argv.index("--format") + 2:]
        if command == ["fsid"]:
            assert not data
            return 0, json.dumps({"fsid": core_request["fsid"]}).encode()
        if command == ["orch", "ls", "--export"]:
            assert not data
            return 0, json.dumps(list(observed.values())).encode()
        assert command == ["orch", "apply", "-i", "-"]
        documents = list(yaml.safe_load_all(data))
        assert len(documents) == 1 and isinstance(documents[0], dict)
        value = documents[0]
        observed[value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")] = value
        received.append(data)
        return 0, b"Scheduled service update\n"

    native = ceph_native.Worker(core_request, core_request["bootstrap"], run=run, markers=Markers())
    native.prove_registry = lambda: None
    native.provider_registry_absent = lambda: None
    native.recovery_scope = lambda: {"cluster": copy.deepcopy(core_request)}
    native.normalized_services = lambda groups: groups
    assert native.services() == {"changed": True}
    assert len(received) == 5 and all(value.endswith(b"\n") for value in received)
    assert core_request == original
    documents = [yaml.safe_load(value) for value in received]
    sent = [value for value in documents if value["service_type"] != "osd"]
    assert sorted(sent, key=lambda value: value["service_type"]) == sorted(
        [value for value in original["services"] if value["service_type"] != "osd"], key=lambda value: value["service_type"])
    children = sorted((value for value in documents if value["service_type"] == "osd"), key=lambda value: value["placement"]["hosts"])
    expected = []
    for host in ("node-1", "node-2", "node-3"):
        suffix = hashlib.sha256(ceph_native.canonical(["data", host])).hexdigest()[:16]
        expected.append({"service_type": "osd", "service_id": "data-" + suffix, "placement": {"hosts": [host]},
                         "spec": {"data_devices": {"paths": ["/dev/disk/by-id/virtio-disk" + host[-1]]}}})
    assert children == expected


def test_owned_bootstrap_recovers_interrupted_call_home_acknowledgement(core_request, monkeypatch):
    answers = {("fsid",): {"fsid": core_request["fsid"]},
               ("orch", "accept", "call-home-enabled"): (1, b"private-provider-diagnostic")}
    native, calls = worker(core_request, answers)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    monkeypatch.setattr(native, "daemons", lambda: [{"fsid": core_request["fsid"]}])
    with pytest.raises(ceph_native.Refusal, match="^native-command$") as refused:
        native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"})
    assert "private-provider-diagnostic" not in str(refused.value)
    assert native.markers.writes == []
    answers[("orch", "accept", "call-home-enabled")] = (0, b"Health warning for call home enablement cleared\n")
    assert native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"}) == {"changed": True}
    assert native.markers.records["call-home-accepted.json"] == {"callHome": "enabled"}
    assert native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"}) == {"changed": False}
    assert native.markers.writes == ["call-home-accepted.json"]
    assert [argv[argv.index("--format") + 2:] for argv in calls] == [
        ["fsid"], ["orch", "accept", "call-home-enabled"], ["fsid"], ["orch", "accept", "call-home-enabled"], ["fsid"],
    ]


def test_lost_call_home_completion_record_repeats_only_owned_idempotent_ack(core_request, monkeypatch):
    native, calls = worker(core_request, {("fsid",): {"fsid": core_request["fsid"]},
                                          ("orch", "accept", "call-home-enabled"): (0, b"Health warning for call home enablement cleared\n")})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    monkeypatch.setattr(native, "daemons", lambda: [{"fsid": core_request["fsid"]}])
    publish = native.markers.write

    def interrupted_write(name, value):
        assert name == "call-home-accepted.json" and value == {"callHome": "enabled"}
        raise OSError("completion publication interrupted")

    monkeypatch.setattr(native.markers, "write", interrupted_write)
    with pytest.raises(OSError, match="completion publication interrupted"):
        native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"})
    assert native.markers.records == {} and native.markers.writes == []
    monkeypatch.setattr(native.markers, "write", publish)
    assert native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"}) == {"changed": True}
    assert [argv[argv.index("--format") + 2:] for argv in calls] == [
        ["fsid"], ["orch", "accept", "call-home-enabled"], ["fsid"], ["orch", "accept", "call-home-enabled"],
    ]
    assert native.markers.writes == ["call-home-accepted.json"]


@pytest.mark.parametrize("boundary", ["unowned", "foreign-fsid", "disabled", "conflicting-acceptance"])
def test_call_home_acknowledgement_refuses_unproved_consent_or_identity(core_request, monkeypatch, boundary):
    if boundary == "disabled":
        core_request["callHome"] = "disabled"
    marks = Markers(owned=boundary != "unowned")
    if boundary == "conflicting-acceptance":
        marks.records["call-home-accepted.json"] = {"callHome": "disabled"}
    fsid = "f8082a68-c08a-489f-818d-c9c01873a0f0" if boundary == "foreign-fsid" else core_request["fsid"]
    native, calls = worker(core_request, {("fsid",): {"fsid": fsid}}, marks)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    monkeypatch.setattr(native, "daemons", lambda: [{"fsid": fsid}])
    with pytest.raises(ceph_native.Refusal, match="^(ownership|call-home-unqualified)$"):
        native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"})
    assert all("accept" not in argv for argv in calls) and marks.writes == []


@pytest.mark.parametrize("mutation", ["changed-intent", "old-owner-record"])
def test_changed_call_home_intent_cannot_reuse_durable_bootstrap_owner(core_request, tmp_path, monkeypatch, mutation):
    marks = ceph_native.Markers(core_request, str(tmp_path))
    os.mkdir(marks.path, 0o700)
    owner = ceph_native.marker_identity(core_request)
    if mutation == "old-owner-record":
        owner.pop("callHome")
    marks.write("owner.json", owner)
    original = marks.read()
    if mutation == "changed-intent":
        assert original["callHome"] == "enabled"
        core_request["callHome"] = "disabled"
    native, calls = worker(core_request, {}, marks)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.bootstrap({"clusterPrivateKey": "key-fixture", "clusterPublicKey": "public-fixture", "knownHosts": "host-fixture"})
    assert calls == [] and marks.read() == original
    assert marks.read("call-home-accepted.json") is None


@pytest.mark.parametrize("output",
                         [b"true\n", b"false", b" false\n", b"\nfalse\n", b"false\n\n", b'"false"\n', b'{"value":"false"}', b"private-native-diagnostic"])
def test_effective_memory_uses_exact_plain_native_output(core_request, output):
    core_request["config"] = memory_config()
    native, calls = worker(core_request, {("config", "show", "osd.0", "osd_memory_target"): (0, b"2147483648\n"),
                                          ("config", "show", "osd.0", "osd_memory_target_autotune"): (0, output)})
    with pytest.raises(ceph_native.Refusal, match="^config-drift$") as refused:
        native.config_effective(0)
    assert "private-native-diagnostic" not in str(refused.value)
    assert all("--format" not in argv for argv in calls)


def memory_observation_answers(core_request):
    names = [h["name"] for h in core_request["hosts"]]
    managers = [names[0].split(".", 1)[0] + ".abcdef", names[1].split(".", 1)[0] + ".ghijkl"]
    running = [{"daemon_type": "mon", "daemon_id": n.split(".", 1)[0], "hostname": n, "status": 1,
                "container_image_digests": [core_request["image"]]} for n in names]
    running += [{"daemon_type": "mgr", "daemon_id": n, "hostname": names[i], "status": 1, "container_image_digests": [core_request["image"]]} for i,
                n in enumerate(managers)]
    running += [{"daemon_type": "osd", "daemon_id": str(i), "hostname": n, "status": 1, "container_image_digests": [core_request["image"]]} for i,
                n in enumerate(names)]
    answers = {
        ("/usr/sbin/cephadm", "ls"): [{"fsid": core_request["fsid"]}], ("fsid",): {"fsid": core_request["fsid"]},
        ("config", "dump"): memory_database(),
        ("status",): {"fsid": core_request["fsid"], "health": {"status": "HEALTH_OK"},
                      "pgmap": {"num_pgs": 32, "pgs_by_state": [{"state_name": "active+clean", "count": 32}]}},
        ("mgr", "module", "ls"): {"enabled_modules": ["call_home_agent"], "always_on_modules": ["cephadm"], "force_disabled_modules": []},
        ("quorum_status",): {"quorum_names": [name.split(".", 1)[0] for name in names]},
        ("mgr", "dump"): {"active_name": managers[0], "standbys": [{"name": managers[1]}]},
        ("orch", "host", "ls"): [{"hostname": n} for n in names], ("orch", "ps"): running,
        ("osd", "metadata"): [{"hostname": n, "device_ids": "vdb=disk" + str(i + 1), "id": i} for i, n in enumerate(names)],
        ("osd", "dump"): {"osds": [{"osd": i, "up": 1, "in": 1, "uuid": "uuid-" + str(i)} for i in range(3)]},
    }
    for identifier in range(3):
        for name, value in core_request["config"]["osd"].items():
            answers[("config", "show", "osd." + str(identifier), name)] = (0, (value + "\n").encode())
    return answers


def test_observation_proves_each_owned_osd_effective_memory(core_request):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    native, calls = worker(core_request, answers)
    assert native.observe()["ready"] is True
    config_reads = [argv for argv in calls if "show" in argv]
    assert len(config_reads) == 6 and all("--format" not in argv for argv in config_reads)
    assert native.markers.writes == []
    answers[("config", "show", "osd.2", "osd_memory_target")] = (0, b"4294967296\n")
    with pytest.raises(ceph_native.Refusal, match="^config-drift$"):
        native.observe()
    # The manager serves running configuration; while it restarts the read is unfinished, and nothing it printed leaves.
    answers[("config", "show", "osd.2", "osd_memory_target")] = (1, b"private-native-error")
    result = native.observe()
    assert result["partial"] and not result["ready"] and "private-native-error" not in json.dumps(result)


@pytest.mark.parametrize("action", ["pool", "client"])
@pytest.mark.parametrize("owned", [True, False])
def test_absent_native_resource_retains_owned_cleanup_work(core_request, action, owned):
    core_request.update(action=action, config=memory_config())
    resource = (replicated_pool()
                if action == "pool" else {"name": "consumer", "pool": "volumes", "access": "read-only", "dataPools": [],
                                          "filesystem": "", "group": "", "subvolume": ""})
    core_request[action] = resource
    answers = memory_observation_answers(core_request)
    answers[("osd", "pool", "ls", "detail") if action == "pool" else ("auth", "ls")] = [] if action == "pool" else {"auth_dump": []}
    marks = Markers()
    if owned:
        marks.records[action + "-" + resource["name"] + ".json"] = copy.deepcopy(resource)
        if action == "pool":
            marks.records["pool-id-volumes.json"] = {"id": 4}
    retained = copy.deepcopy(marks.records)
    native, calls = worker(core_request, answers, marks)

    result = native.observe()
    assert result["owned"] is owned and result["exists"] is owned
    assert result["partial"] is owned and result["ready"] is False
    assert result["removed"] is False and result["resource"] == {"id": "", "path": ""} and result["client"] == ""
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)
    with pytest.raises(ceph_native.Refusal, match="^client-quiescence-unqualified$"):
        native.destroy()
    assert marks.records == retained and marks.writes == []
    assert calls


@pytest.mark.parametrize("device_ids,hostname", [
    ("vdc=disk1", "node-1"),
    ("vdb=QEMU_HARDDISK_disk1", "node-1"),
    ("vdb=virtio-disk1", "node-1"),
    ("vdb=foreign_disk1", "node-1"),
    ("vdb=disk1_extra", "node-1"),
    ("vdb=disk1,vdc=foreign", "node-1"),
    ("vdb=disk1", "foreign-host"),
])
def test_virtio_osd_metadata_requires_exact_owned_host_target_and_serial(core_request, device_ids, hostname):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("osd", "metadata")][0].update(device_ids=device_ids, hostname=hostname)
    for name in core_request["config"]["osd"]:
        del answers[("config", "show", "osd.0", name)]
    native, calls = worker(core_request, answers)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert all(osd["id"] != 0 for osd in result["osds"])
    assert len([argv for argv in calls if "show" in argv]) == 4
    assert native.markers.writes == []


def test_osd_checks_read_the_published_sdk_identity_never_the_bare_serial(core_request):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    for index, item in enumerate(answers[("osd", "metadata")]):
        item["device_ids"] = "vdb=QEMU_QEMU_HARDDISK_disk" + str(index + 1)
    marks = Markers()
    marks.published[ceph_native.DEVICE_IDENTITIES] = identities(core_request, "QEMU_QEMU_HARDDISK_{serial}")
    native, unused = worker(core_request, answers, marks)
    assert native.observe()["ready"] is True
    marks.published[ceph_native.DEVICE_IDENTITIES] = identities(core_request)
    native, unused = worker(core_request, answers, marks)
    result = native.observe()
    assert result["ready"] is False and result["osds"] == []


@pytest.mark.parametrize("change", ["missing", "serial", "host", "path", "short", "version"])
def test_a_missing_or_contradicting_identity_record_refuses_every_osd_check(core_request, change):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    marks = Markers()
    record = identities(core_request)
    if change == "missing":
        record = None
    elif change == "serial":
        record["devices"][0]["serial"] = "guessed"
    elif change == "host":
        record["devices"][0]["host"] = "node-2"
    elif change == "path":
        record["devices"][0]["path"] = "/dev/vdc"
    elif change == "short":
        record["devices"].pop()
    else:
        record["version"] = "ceph-fixture-device-identities-v1"
    marks.published[ceph_native.DEVICE_IDENTITIES] = record
    native, unused = worker(core_request, answers, marks)
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        native.observe()
    assert marks.writes == []


def test_before_identities_are_published_no_osd_may_exist(core_request):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("osd", "metadata")] = []
    answers[("osd", "dump")] = {"osds": []}
    answers[("orch", "ps")] = [daemon for daemon in answers[("orch", "ps")] if daemon["daemon_type"] != "osd"]
    marks = Markers()
    marks.published[ceph_native.DEVICE_IDENTITIES] = None
    native, unused = worker(core_request, answers, marks)
    result = native.observe()
    assert result["ready"] is False and result["osds"] == []


@pytest.mark.parametrize("device_ids", [None, 1, True, ["vdb=disk1"], {"vdb": "disk1"}])
def test_non_string_osd_device_identity_is_a_fatal_unknown_read(core_request, device_ids):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("osd", "metadata")][0]["device_ids"] = device_ids
    native, unused = worker(core_request, answers)
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.observe()
    assert native.markers.writes == []


@pytest.mark.parametrize("missing", ["active", "standby"])
def test_expected_manager_cache_lag_is_partial_read_only_evidence(core_request, missing):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("orch", "ps")] = [daemon for daemon in answers[("orch", "ps")]
                               if not (daemon["daemon_type"] == "mgr" and daemon["daemon_id"].endswith(".abcdef" if missing == "active" else ".ghijkl"))]
    native, unused = worker(core_request, answers)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert native.markers.writes == []
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)


def fqdn_core_request(request):
    changes = {host["name"]: host["name"] + ".example.test" for host in request["hosts"]}
    for host in request["hosts"]:
        host["name"] = changes[host["name"]]
    request["bootstrap"] = changes[request["bootstrap"]]
    for service in request["services"]:
        service["placement"]["hosts"] = [changes[name] for name in service["placement"]["hosts"]]
    return request


def test_native_short_daemon_ids_project_back_to_owned_fqdn_hosts(core_request):
    fqdn_core_request(core_request)
    core_request["config"] = memory_config()
    native, unused = worker(core_request, memory_observation_answers(core_request))
    result = native.observe()
    names = [host["name"] for host in core_request["hosts"]]
    assert result["ready"] is True and result["monitors"] == names
    assert result["mgrActive"] == names[0] and result["mgrStandbys"] == [names[1]]
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)
    assert native.markers.writes == []


def test_extra_expected_manager_cache_row_is_partial_without_effects(core_request):
    fqdn_core_request(core_request)
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("orch", "ps")].append({"daemon_type": "mgr", "daemon_id": "node-3.mnopqr",
                                    "hostname": core_request["hosts"][2]["name"], "status": 1,
                                    "container_image_digests": [core_request["image"]]})
    native, unused = worker(core_request, answers)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert native.markers.writes == []


@pytest.mark.parametrize("change", ["missing", "starting", "missing-image"])
def test_monitor_daemon_cache_requires_every_quorum_member_ready(core_request, change):
    fqdn_core_request(core_request)
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    monitors = answers[("orch", "ps")]
    if change == "missing":
        del monitors[2]
    elif change == "starting":
        monitors[2]["status"] = 0
    else:
        monitors[2]["container_image_digests"] = []
    native, unused = worker(core_request, answers)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert native.markers.writes == []


@pytest.mark.parametrize("change", ["foreign-mon", "fqdn-mon-id", "wrong-mon-host", "duplicate-mon", "foreign-mgr",
                                    "fqdn-mgr-id", "wrong-mgr-host", "wrong-mgr-suffix", "duplicate-mgr"])
def test_native_daemon_identity_refuses_foreign_duplicate_or_wrong_host(core_request, change):
    fqdn_core_request(core_request)
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    running = answers[("orch", "ps")]
    reason = "foreign-service"
    if change == "foreign-mon":
        answers[("quorum_status",)]["quorum_names"][0] = "foreign"
    elif change == "fqdn-mon-id":
        answers[("quorum_status",)]["quorum_names"][0] = core_request["hosts"][0]["name"]
    elif change == "wrong-mon-host":
        running[0]["hostname"] = core_request["hosts"][1]["name"]
    elif change == "duplicate-mon":
        running.append(dict(running[0]))
        reason = "native-output"
    elif change == "foreign-mgr":
        answers[("mgr", "dump")]["active_name"] = "foreign.abcdef"
    elif change == "fqdn-mgr-id":
        answers[("mgr", "dump")]["active_name"] = core_request["hosts"][0]["name"] + ".abcdef"
    elif change == "wrong-mgr-host":
        running[3]["hostname"] = core_request["hosts"][1]["name"]
    elif change == "wrong-mgr-suffix":
        answers[("mgr", "dump")]["active_name"] = "node-1.arbitrary"
    else:
        running.append(dict(running[3]))
        reason = "native-output"
    native, unused = worker(core_request, answers)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.observe()
    assert native.markers.writes == []


def test_colliding_short_host_stems_refuse_before_native_execution(core_request):
    core_request["hosts"][1]["name"] = "node-1.other.example.test"
    core_request["hosts"][0]["name"] = "node-1.example.test"
    core_request["bootstrap"] = core_request["hosts"][0]["name"]
    with pytest.raises(ceph_native.Refusal, match="^host-identity$"):
        ceph_native.validate(core_request)


@pytest.mark.parametrize("pending", ["down", "out", "starting", "missing-cache", "missing-image"])
def test_starting_osd_does_not_request_an_unavailable_effective_config(core_request, pending):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    if pending in ("down", "out"):
        answers[("osd", "dump")]["osds"][2]["up" if pending == "down" else "in"] = 0
    elif pending == "starting":
        answers[("orch", "ps")][-1]["status"] = 0
    elif pending == "missing-image":
        answers[("orch", "ps")][-1]["container_image_digests"] = []
    else:
        answers[("orch", "ps")].pop()
    for name in core_request["config"]["osd"]:
        del answers[("config", "show", "osd.2", name)]
    native, calls = worker(core_request, answers)
    result = native.observe()
    assert not result["ready"] and result["partial"]
    assert len([argv for argv in calls if "show" in argv]) == 4
    assert native.markers.writes == []


@pytest.mark.parametrize("pgmap,ready", [
    ({"num_pgs": 0, "pgs_by_state": []}, True),
    ({"num_pgs": 32, "pgs_by_state": [{"state_name": "active+clean", "count": 32}]}, True),
    ({"num_pgs": 32, "pgs_by_state": [{"state_name": "active+clean", "count": 31}, {"state_name": "peering", "count": 1}]}, False),
    ({"num_pgs": 32, "pgs_by_state": [{"state_name": "active+degraded", "count": 32}]}, False),
])
def test_readiness_requires_complete_active_clean_pg_counts(core_request, pgmap, ready):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("status",)]["pgmap"] = pgmap
    native, unused = worker(core_request, answers)
    assert native.observe()["ready"] is ready
    for action in ("pool", "client"):
        assert ceph_native.placement_groups_ready(answers[("status",)], action) is (ready and pgmap["num_pgs"] > 0)


@pytest.mark.parametrize("pgmap", [None, {}, {"num_pgs": True, "pgs_by_state": []}, {"num_pgs": -1, "pgs_by_state": []},
                                   {"num_pgs": 1, "pgs_by_state": []},
                                   {"num_pgs": 1, "pgs_by_state": [{"state_name": "active+clean", "count": True}]},
                                   {"num_pgs": 1, "pgs_by_state": [{"state_name": "", "count": 1}]}])
def test_malformed_pg_snapshot_is_unknown_instead_of_retryable(pgmap):
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        ceph_native.placement_groups_ready({"pgmap": pgmap}, "cluster")


@pytest.mark.parametrize("change", ["disabled", "forced-off", "malformed", "foreign-image"])
def test_native_readiness_refuses_declared_module_or_image_drift(core_request, change):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    modules = answers[("mgr", "module", "ls")]
    if change == "disabled":
        modules["enabled_modules"] = []
    elif change == "forced-off":
        modules["force_disabled_modules"] = ["call_home_agent"]
    elif change == "malformed":
        modules["enabled_modules"] = [True]
    else:
        answers[("orch", "ps")][-1]["container_image_digests"] = ["registry.example.test/ceph@sha256:" + "b" * 64]
    native, unused = worker(core_request, answers)
    reason = "native-output" if change == "malformed" else "artifact" if change == "foreign-image" else "call-home-drift"
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.observe()
    assert native.markers.writes == []


QUIET_MANAGER = {"enabled_modules": ["iostat", "nfs"], "always_on_modules": ["cephadm"], "force_disabled_modules": []}


def twenty_one_observation(core_request, modules=None, health=None):
    """A 20.1.0-221 cluster: no IBM license records (research ceph-mgr D2) and Call Home left at its opt-in default (D3)."""
    core_request["config"] = memory_config()
    core_request["callHome"] = "disabled"
    answers = memory_observation_answers(core_request)
    answers[("mgr", "module", "ls")] = copy.deepcopy(QUIET_MANAGER if modules is None else modules)
    if health is not None:
        answers[("status",)]["health"] = health
    native, calls = worker(core_request, answers)
    native.selection = specs.selection(specs.synthesized("20.1.0-221.el9cp"))
    native.license_accepted = ceph_native.Worker.license_accepted.__get__(native)
    return native, calls


def test_a_manager_without_the_license_contract_is_ready_without_license_keys(core_request):
    native, calls = twenty_one_observation(core_request)
    assert native.observe()["ready"] is True
    assert "IBM_LICENSE_NOT_ACCEPTED" not in native.blocking and native.markers.writes == []
    assert native.selection.applied == {"ceph-mgr-without-license-contract", "ceph-call-home-opt-in"}
    assert all("accept-license" not in argv and "enable-call-home" not in argv for argv in calls)


@pytest.mark.parametrize("field", ["enabled_modules", "always_on_modules"])
def test_an_enabled_call_home_agent_on_an_opt_in_cluster_is_drift(core_request, field):
    native, unused = twenty_one_observation(core_request, dict(QUIET_MANAGER, **{field: QUIET_MANAGER[field] + ["call_home_agent"]}))
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.observe()


@pytest.mark.parametrize("modules", [{}, dict(QUIET_MANAGER, enabled_modules=[True]), dict(QUIET_MANAGER, enabled_modules=["nfs", "nfs"])])
def test_an_opt_in_cluster_keeps_the_module_list_shape_checks(core_request, modules):
    native, unused = twenty_one_observation(core_request, modules)
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.observe()


def test_the_managers_own_license_check_still_blocks_a_cluster_without_the_contract(core_request):
    health = {"status": "HEALTH_WARN", "checks": {"IBM_LICENSE_NOT_ACCEPTED": {"severity": "HEALTH_WARN"}}}
    native, unused = twenty_one_observation(core_request, health=health)
    result = native.observe()
    assert result["ready"] is False and native.blocking == ["IBM_LICENSE_NOT_ACCEPTED"]


def test_a_cluster_without_host_preparation_refuses_runtime_profile_instead_of_naming_the_license(core_request):
    native, calls = twenty_one_observation(core_request)
    old = specs.synthesized("20.1.0-221.el9cp")
    native.selection = native_specializations.Selection({"host-cephadm": lambda: old, "image-cephadm": native.image_payload})
    native.host_preparation = lambda: None
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        native.observe()
    assert "IBM_LICENSE_NOT_ACCEPTED" not in native.blocking and native.selection.applied == set()
    assert all("inspect" not in argv for argv in calls)


class ReadinessClock:
    def __init__(self):
        self.value, self.pauses = 0, []

    def now(self):
        return self.value

    def pause(self, seconds):
        self.pauses.append(seconds)
        self.value += seconds


class ReadinessTransport:
    def __init__(self, request, clock, ready_after=3, failure=None):
        self.request, self.clock, self.ready_after, self.failure = request, clock, ready_after, failure
        self.calls, self.polls = [], 0

    def packages(self, host):
        self.calls.append((host["name"], "packages", None))
        return {"changed": False, "ready": True, "planDigest": "a" * 64}

    def bootstrap_material(self):
        return {"clusterPublicKey": "owned-cluster-public-key", "clusterPrivateKey": "owned-private-key", "knownHosts": "owned-host-keys",
                "registryJSON": "protected-registry-material"}

    def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
        del private
        self.calls.append((host["name"], phase, timeout))
        if phase == "services":
            self.devices = devices
        if phase == "host_devices":
            return {"devices": [row for row in identities(self.request)["devices"] if row["host"] == host["name"]]}
        if phase == "observe":
            if timeout is None:
                value = observation(self.request)
                value.update(owned=False, removed=False)
                return value
            self.polls += 1
            if self.failure == "unknown":
                raise ceph_native.Refusal("native-command")
            if self.failure != "slow" and timeout <= 120:
                self.clock.value += timeout
                raise ceph_native.Refusal("native-timeout")
            self.clock.value += timeout + 1 if self.failure == "slow" else 120
            value = observation(self.request, ready=True)
            if self.polls < self.ready_after:
                value.update(ready=False, partial=True)
            if self.failure == "malformed":
                value["privateKey"] = "never-reveal-this"
            if self.failure == "unowned":
                value.update(ready=False, partial=True, owned=False)
            return value
        if phase == "host_state":
            if timeout is not None and timeout <= 10:
                self.clock.value += timeout
                raise ceph_native.Refusal("native-timeout")
            self.clock.value += 10
            return {"name": host["name"], "owned": True, "exists": True}
        if phase == "dashboard":
            return {"changed": True, "private": PASSWORD}
        assert phase in ("prerequisites", "devices", "tunnel_preflight", "claim", "authorize", "bootstrap", "cache", "enroll", "services", "osd_prepare")
        return {"changed": phase not in ("prerequisites", "devices", "tunnel_preflight")}


def package_intent(core_request):
    core_request["packages"] = {"version": "ceph-packages-v3", "licenseAccepted": True, "sources": [
        {"host": host["name"], "profile": "rhel", "media": "rhel-dvd.iso", "publicationPath": "/private/example/os-tree",
         "url": "https://192.0.2.10:8443/os/rhel/tree", "certificateRef": "serving-tls"}
        for host in core_request["hosts"]]}


def test_async_apply_waits_read_only_with_shrinking_aggregate_budget(core_request, monkeypatch, tmp_path):
    package_intent(core_request)
    clock = ReadinessClock()
    transport = ReadinessTransport(core_request, clock)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    result = ceph_core.execute(core_request, "apply", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")}, now=clock.now, pause=clock.pause)
    assert result["changed"] and result["evidence"]["ready"]
    assert transport.polls == 4 and clock.value == 610 and clock.pauses == [5, 5]
    phases = [phase for unused, phase, unused in transport.calls]
    assert phases.count("bootstrap") == phases.count("enroll") == phases.count("services") == phases.count("dashboard") == 1
    last_effect, dashboard = phases.index("services"), phases.index("dashboard")
    assert set(phases[last_effect + 1:dashboard]) == set(phases[dashboard + 1:]) == {"observe", "host_state"}
    for wait in (transport.calls[last_effect + 1:dashboard], transport.calls[dashboard + 1:]):
        budgets = [timeout for unused, unused, timeout in wait]
        assert budgets[0] == 660 and budgets == sorted(budgets, reverse=True) and min(budgets) > 0
    assert "packages" not in phases and phases[:3] == ["prerequisites"] * 3
    assert (tmp_path / "dashboard").read_bytes() == PASSWORD.encode()
    assert stat.S_IMODE(os.stat(tmp_path / "dashboard").st_mode) == 0o600


@pytest.mark.parametrize("mode", ["native", "verified-tunnel"])
def test_registry_material_only_reaches_each_hosts_authorization(core_request, monkeypatch, mode, tmp_path):
    core_request["clusterSSHTransport"] = mode
    package_intent(core_request)
    clock = ReadinessClock()
    private_calls = []

    class ProjectionTransport(ReadinessTransport):
        def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
            if mode == "verified-tunnel" and phase == "prerequisites":
                self.calls.append((host["name"], phase, timeout))
                return {"name": host["name"], "owned": False, "exists": False,
                        "recovery": ceph_tunnel_staging.recovery_record(core_request, host["name"], "unclaimed")}
            if phase == "bootstrap_state":
                self.calls.append((host["name"], phase, timeout))
                value = ceph_tunnel_staging.recovery_record(core_request, host["name"], "unclaimed")
                value["evidence"] = dict(observation(core_request), owned=False, removed=False)
                return value
            if private is not None:
                private_calls.append((host["name"], phase, dict(private)))
            if phase in ("tunnel_configure", "tunnel_mount"):
                self.calls.append((host["name"], phase, timeout))
                return {"changed": True}
            return super().call(host, phase, private, timeout=timeout, devices=devices, service=service)

    transport = ProjectionTransport(core_request, clock)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    outputs = {"dashboard": str(tmp_path / "dashboard")}
    assert ceph_core.execute(core_request, "apply", "d" * 64, transport, outputs, now=clock.now, pause=clock.pause)["evidence"]["ready"]
    if mode == "verified-tunnel":
        calls = [(host, phase) for host, phase, unused in transport.calls]
        enroll = calls.index(("node-1", "enroll"))
        assert [host for host, phase in calls[:enroll] if phase == "cache"] == ["node-1", "node-2", "node-3"]
        assert calls.index(("node-1", "tunnel_mount")) < calls.index(("node-1", "cache"))
        assert transport.devices == identities(core_request)["devices"]
        assert [host for host, phase in calls if phase == "host_devices"] == ["node-1", "node-2", "node-3"]
    authorized = [(host, material) for host, phase, material in private_calls if phase == "authorize"]
    assert [host for host, unused in authorized] == [host["name"] for host in core_request["hosts"]]
    for unused, material in authorized:
        assert material["registryJSON"] == "protected-registry-material"
        expected = {"clusterPublicKey", "registryJSON"}
        if mode == "verified-tunnel":
            expected |= {"clusterPrivateKey", "knownHosts"}
        assert set(material) == expected
    for unused, phase, material in private_calls:
        if phase == "tunnel_preflight":
            assert set(material) == {"knownHosts"}
        elif phase != "authorize":
            assert phase in ("bootstrap", "tunnel_configure")
            assert set(material) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}


@pytest.mark.parametrize("failure,reason", [("unknown", "native-command"), ("malformed", "evidence"), ("unowned", "not-ready"), ("slow", "not-ready")])
def test_readiness_does_not_retry_unknown_unowned_or_late_proofs(core_request, failure, reason):
    clock = ReadinessClock()
    transport = ReadinessTransport(core_request, clock, ready_after=1, failure=failure)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$") as refused:
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert "never-reveal-this" not in str(refused.value)
    assert transport.polls == 1 and clock.pauses == []
    assert all(phase in ("observe", "host_state") for unused, phase, unused in transport.calls)


def test_readiness_timeout_never_repeats_an_effect_or_exceeds_the_budget(core_request):
    clock = ReadinessClock()
    transport = ReadinessTransport(core_request, clock, ready_after=100)
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert transport.polls == 6 and clock.value <= ceph_core.READINESS_TIMEOUT
    assert all(phase in ("observe", "host_state") for unused, phase, unused in transport.calls)
    assert all(0 < timeout <= 660 for unused, unused, timeout in transport.calls)


def test_fast_partial_observations_remain_rate_limited_until_deadline(core_request):
    clock = ReadinessClock()

    class InstantTransport:
        def __init__(self):
            self.calls = []

        def call(self, host, phase, *, timeout):
            self.calls.append((host["name"], phase, timeout))
            if phase == "observe":
                value = observation(core_request, ready=True)
                value.update(ready=False, partial=True)
                return value
            assert phase == "host_state"
            return {"name": host["name"], "owned": True, "exists": True}

    transport = InstantTransport()
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert clock.value == ceph_core.READINESS_TIMEOUT
    assert clock.pauses and all(0 < pause <= ceph_core.READINESS_INTERVAL for pause in clock.pauses)
    assert len(clock.pauses) <= ceph_core.READINESS_TIMEOUT / ceph_core.READINESS_INTERVAL + 1
    assert all(phase in ("observe", "host_state") and 0 < timeout <= 660 for unused, phase, timeout in transport.calls)


def test_partial_existing_cluster_is_never_adopted_or_rebootstrapped(core_request, monkeypatch):
    package_intent(core_request)
    clock = ReadinessClock()

    class PartialTransport(ReadinessTransport):
        def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
            if phase == "observe":
                self.calls.append((host["name"], phase, timeout))
                value = observation(self.request, ready=True)
                value.update(ready=False, partial=True)
                return value
            return super().call(host, phase, private, timeout=timeout)

    transport = PartialTransport(core_request, clock)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    with pytest.raises(ceph_native.Refusal, match="^partial-cluster$"):
        ceph_core.execute(core_request, "apply", "d" * 64, transport, UNWRITTEN, now=clock.now, pause=clock.pause)
    assert all(phase in ("prerequisites", "observe") for unused, phase, unused in transport.calls)
    assert clock.pauses == [] and clock.value == 0


def test_an_osd_the_map_holds_before_its_first_boot_is_starting_not_malformed(core_request):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    # ceph-volume runs osd new well before the daemon boots; until then the MON reports only its ID.
    answers[("osd", "metadata")][0] = {"id": 0}
    answers[("osd", "dump")]["osds"][0].update(up=0)
    answers[("orch", "ps")] = [daemon for daemon in answers[("orch", "ps")] if (daemon["daemon_type"], daemon["daemon_id"]) != ("osd", "0")]
    for name in core_request["config"]["osd"]:
        del answers[("config", "show", "osd.0", name)]
    native, unused = worker(core_request, answers)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert [osd["id"] for osd in result["osds"]] == [1, 2] and native.markers.writes == []


def health(*checks):
    return {"status": "HEALTH_ERR" if checks else "HEALTH_OK", "mutes": [],
            "checks": {name: {"severity": "HEALTH_ERR", "summary": {"message": "synthetic", "count": 1}, "muted": False} for name in checks}}


def test_call_home_egress_alone_never_blocks_readiness_and_every_other_check_is_named(core_request):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    answers[("status",)]["health"] = health("CHA_ERROR_SENDING_REPORT")
    native, unused = worker(core_request, answers)
    result = native.observe()
    assert result["ready"] is True and result["health"] == "HEALTH_ERR" and native.blocking == []
    answers[("status",)]["health"] = health("CHA_ERROR_SENDING_REPORT", "PG_DEGRADED", "OSD_DOWN")
    result = native.observe()
    assert result["ready"] is False and result["partial"] is True and native.blocking == ["OSD_DOWN", "PG_DEGRADED"]
    answers[("status",)]["health"] = {"status": "HEALTH_WARN", "checks": [], "mutes": []}
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.observe()


def test_a_readiness_timeout_names_the_checks_still_blocking(core_request):
    clock = ReadinessClock()

    class BlockedTransport:
        def call(self, host, phase, *, timeout):
            del timeout
            if phase == "observe":
                clock.value += 1
                value = observation(core_request, ready=True)
                value.update(ready=False, partial=True, blocking=["OSD_DOWN", "PG_DEGRADED"])
                return value
            return {"name": host["name"], "owned": True, "exists": True}

    with pytest.raises(ceph_native.Refusal, match="^not-ready$") as refused:
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], BlockedTransport(), clock.now, clock.pause)
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS) == ("not-ready", "not-ready:OSD_DOWN.PG_DEGRADED")
    assert ceph_core.readiness_detail(["CHECK_" + str(index).rjust(40, "X") for index in range(9)]).count(".") == 2
    assert len(ceph_core.readiness_detail(["CHECK_" + str(index).rjust(40, "X") for index in range(9)])) <= 160


UNSETTLED = "native-refusal:not-ready@ceph_native.daemons:1196"


class UnsettledTransport(ReadinessTransport):
    """A host whose snapshot cephadm keeps moving for its first polls, as the worker reports it."""

    def __init__(self, request, clock, phase, host, polls, detail=UNSETTLED):
        super().__init__(request, clock, ready_after=1)
        self.phase, self.host, self.unsettled, self.detail = phase, host, polls, detail

    def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
        if phase == self.phase and host["name"] == self.host and self.unsettled:
            self.calls.append((host["name"], phase, timeout))
            self.unsettled -= 1
            self.clock.value += 10
            raise ceph_native.RemoteRefusal(self.detail)
        return super().call(host, phase, private, timeout=timeout, devices=devices, service=service)


@pytest.mark.parametrize("phase,host", [("observe", "node-1"), ("host_state", "node-2")])
def test_an_unsettled_snapshot_is_not_ready_and_the_wait_polls_again(core_request, phase, host):
    clock = ReadinessClock()
    transport = UnsettledTransport(core_request, clock, phase, host, 2)
    observed = ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert observed["ready"] and transport.unsettled == 0 and clock.pauses == [ceph_core.READINESS_INTERVAL] * 2
    assert all(phase in ("observe", "host_state") for unused, phase, unused in transport.calls)


def test_a_host_that_never_settles_times_out_naming_it(core_request):
    clock = ReadinessClock()

    class NeverSettled:
        def __init__(self):
            self.calls = []

        def call(self, host, phase, *, timeout):
            self.calls.append((host["name"], phase, timeout))
            clock.value += 1
            if phase == "observe":
                value = observation(core_request, ready=True)
                value.update(ready=False, partial=True, blocking=["OSD_DOWN"])
                return value
            if host["name"] == "node-2":
                raise ceph_native.RemoteRefusal(UNSETTLED)
            return {"name": host["name"], "owned": True, "exists": True}

    transport = NeverSettled()
    with pytest.raises(ceph_native.Refusal, match="^not-ready$") as refused:
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS) == ("not-ready", "not-ready:unsettled@node-2.OSD_DOWN")
    assert clock.value == ceph_core.READINESS_TIMEOUT and len(clock.pauses) > 100
    assert all(phase in ("observe", "host_state") and 0 < timeout <= 660 for unused, phase, timeout in transport.calls)
    assert not any(host == "node-3" for host, unused, unused in transport.calls)
    # A read-only observation outside the wait keeps the worker's own not-ready refusal and cause.
    with pytest.raises(ceph_core.UnsettledRefusal) as observed:
        ceph_core.observe_state(core_request, "d" * 64, core_request["hosts"][0], UnsettledTransport(core_request, clock, "observe", "node-1", 1))
    assert ceph_core.refusal(observed.value, ceph_core.CLUSTER_REASONS) == ("not-ready", UNSETTLED)


@pytest.mark.parametrize("detail", ["native-refusal:native-observation-unproved@ceph_native.daemons:1198",
                                    "native-refusal:foreign-cluster@ceph_native.host_state:1225", "native-refusal:not-ready-later",
                                    "native-unknown:not-ready", "native-timeout"])
def test_a_contradiction_or_any_other_remote_refusal_still_ends_the_wait(core_request, detail):
    clock = ReadinessClock()
    transport = UnsettledTransport(core_request, clock, "host_state", "node-2", 1, detail)
    with pytest.raises(ceph_native.RemoteRefusal) as refused:
        ceph_core.wait_ready(core_request, "d" * 64, core_request["hosts"][0], transport, clock.now, clock.pause)
    assert not isinstance(refused.value, ceph_core.UnsettledRefusal) and refused.value.detail == detail
    assert transport.polls == 1 and clock.pauses == []


@pytest.mark.parametrize("command,failure", [(("orch", "ps"), (1, b"Error ENOENT: Module not found")), (("orch", "host", "ls"), (0, b"\n")),
                                             (("orch", "ps"), ceph_native.Refusal("native-timeout"))])
def test_an_orchestrator_read_while_the_manager_restarts_is_unfinished_not_refused(core_request, command, failure):
    core_request["config"] = memory_config()
    answers = memory_observation_answers(core_request)
    native, unused = worker(core_request, answers)
    run = native.run

    def restarting(argv, data=b"", timeout=60):
        if "--format" in argv and tuple(argv[argv.index("--format") + 2:]) == command:
            if isinstance(failure, Exception):
                raise failure
            return failure
        return run(argv, data=data, timeout=timeout)

    native.run = restarting
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"] and result["hosts"] == []
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)
    native.run = run
    assert native.observe()["ready"] is True


def test_the_bootstrap_call_runs_alone_under_its_own_longer_budget(core_request, tmp_path):
    key, known = tmp_path / "key", tmp_path / "known"
    for path, text in ((key, "fixture"), (known, "192.0.2.1 ssh-ed25519 PUBLIC")):
        path.write_text(text)
        path.chmod(0o600)
    calls = []

    def run(argv, data, timeout):
        calls.append((argv, json.loads(data), timeout))
        return 0, b'{"ok":true,"result":{"changed":true}}'

    transport = ceph_core.Transport(core_request, {"sshKey": str(key), "knownHosts": str(known)}, run=run)
    assert transport.call(core_request["hosts"][0], "bootstrap", {"clusterPrivateKey": "private"}) == {"changed": True}
    argv, envelope, timeout = calls[0]
    assert timeout == ceph_core.BOOTSTRAP_CALL and envelope["budget"] == ceph_core.BOOTSTRAP_BUDGET
    assert argv[-1].endswith(" " + str(ceph_core.BOOTSTRAP_BUDGET)) and "REMOTE_LIMIT=" + str(ceph_core.BOOTSTRAP_CEILING) in argv[-1]
    assert ceph_native.BOOTSTRAP_TIMEOUT + 120 <= ceph_core.BOOTSTRAP_BUDGET < ceph_core.BOOTSTRAP_CEILING <= max(ceph_core.WORKER_CEILINGS)
    transport.call(core_request["hosts"][0], "tunnel_configure")
    assert calls[1][2] == 660 and calls[1][1]["budget"] <= 640 and "REMOTE_LIMIT=" not in calls[1][0][-1]


def test_the_bootstrap_launcher_admits_only_bootstrap_within_its_ceiling():
    sources = {name: '"""Fixed fixture."""\n' for name in ceph_native_worker.MODULES}
    from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
    sources["native_diagnostics"] = inspect.getsource(native_diagnostics)
    sources["native_diagnostics"] = "def valid(value): return False\n"
    sources["ceph_native"] = "class Refusal(ValueError): pass\ndef dispatch(value):\n    return {'phase': value['phase']}\n"
    source = ceph_core.bootstrap_launcher(inspect.getsource(ceph_native_worker), inspect.getsource(ceph_host_worker))
    for phase, budget, expected in (("bootstrap", 3, {"ok": True, "result": {"phase": "bootstrap"}}), ("profile", 3, {"ok": False, "reason": "native-refusal"}),
                                    ("bootstrap", ceph_core.BOOTSTRAP_CEILING + 1, {"ok": False, "reason": "native-unknown"})):
        payload = {"sources": sources, "payload": {"phase": phase}, "budget": budget}
        result = subprocess.run([sys.executable, "-I", "-c", source, str(budget)], input=json.dumps(payload).encode(), capture_output=True, timeout=10,
                                check=False)
        assert result.returncode == 0 and result.stderr == b""
        value = json.loads(result.stdout)
        value.pop("cause", None)
        assert value == expected


def tunnel_observation_worker(core_request, manager):
    """A ready verified-tunnel snapshot whose bootstrap host holds the given manager daemon row."""
    core_request["config"] = memory_config()
    core_request["clusterSSHTransport"] = "verified-tunnel"
    answers = memory_observation_answers(core_request)
    name = "mgr." + answers[("mgr", "dump")]["active_name"]
    answers[("/usr/sbin/cephadm", "ls")] = [dict({"fsid": core_request["fsid"], "name": name, "style": "cephadm:v1"}, **manager)]
    native, calls = worker(core_request, answers)
    native.bootstrap_hold = contextlib.nullcontext
    native.verify_tunnel_configuration = lambda: None
    native.verify_transport_bundle = lambda: None
    return native, answers, name, calls


@pytest.mark.parametrize("manager", [{"state": "stopped", "container_id": "", "container_image_name": ""},
                                     {"state": "unknown", "container_id": "b" * 64, "container_image_name": "registry.example/ceph@sha256:" + "a" * 64}])
def test_observation_names_a_manager_cephadm_is_restarting_as_not_ready(core_request, manager):
    manager = dict(manager, container_image_name=core_request["image"] if manager["container_id"] else "")
    native, unused, name, calls = tunnel_observation_worker(core_request, manager)
    result = native.observe()
    assert result["owned"] and result["exists"] and result["partial"] and not result["ready"]
    assert native.blocking == [name]
    assert native.host_state() == {"name": native.host["name"], "owned": True, "exists": True, "pending": [name]}
    assert not any(argv[0] == "/usr/bin/podman" for argv in calls)


def test_observation_of_a_running_manager_whose_mount_contradicts_still_refuses(core_request):
    manager = {"state": "running", "container_id": "b" * 64, "container_image_name": core_request["image"]}
    native, answers, unused, unused_calls = tunnel_observation_worker(core_request, manager)
    answers[("/usr/bin/podman", "container", "inspect", "b" * 64)] = [{"Id": "b" * 64, "HostConfig": {"Privileged": False}, "Mounts": []}]
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.observe()
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.host_state()


@pytest.mark.parametrize("pending", [["mgr.node-2.ghijkl"], []])
def test_core_observation_carries_a_host_pending_manager_as_not_ready(core_request, pending):
    blocking = []

    class Transport:
        def call(self, host, phase):
            if phase == "observe":
                value = observation(core_request, ready=True)
                return dict(value, blocking=[])
            state = {"name": host["name"], "owned": True, "exists": True}
            return dict(state, pending=pending) if pending and host["name"] == "node-2" else state

    if not pending:
        assert ceph_core.observe_state(core_request, "d" * 64, core_request["hosts"][0], Transport(), blocking=blocking)["ready"] is True
        return
    observed = ceph_core.observe_state(core_request, "d" * 64, core_request["hosts"][0], Transport(), blocking=blocking)
    assert observed["owned"] and observed["exists"] and observed["partial"] and not observed["ready"]
    assert blocking == pending and ceph_core.readiness_detail(blocking) == "not-ready:mgr.node-2.ghijkl"


@pytest.mark.parametrize("pending", [[], ["MGR_DOWN"], ["mgr.node 2"], "mgr.node-2.ghijkl", [1]])
def test_core_observation_refuses_a_malformed_pending_list(core_request, pending):
    class Transport:
        def call(self, host, phase):
            if phase == "observe":
                return observation(core_request, ready=True)
            return {"name": host["name"], "owned": True, "exists": True, "pending": pending}

    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.observe_state(core_request, "d" * 64, core_request["hosts"][0], Transport())


def test_evidence_resource_member(core_request):
    value = dict(observation(core_request), request="d" * 64)
    assert ceph_core.evidence(copy.deepcopy(value), "d" * 64, core_request)["resource"] == {"id": "", "path": ""}
    pool = dict(value, action="pool", resource={"id": "8", "path": ""})
    assert ceph_core.evidence(copy.deepcopy(pool), "d" * 64)["resource"] == {"id": "8", "path": ""}
    legacy = {key: item for key, item in value.items() if key != "resource"}
    refused = [dict(legacy, poolID=-1), dict(value, poolID=-1), dict(value, resource={"id": "", "path": "", "zone": ""}), dict(value, resource={"id": ""}),
               dict(pool, resource={"id": 8, "path": ""}), dict(pool, resource={"id": "-1", "path": ""}), dict(pool, resource={"id": "01", "path": ""}),
               dict(pool, resource={"id": "8", "path": "/volumes"}), dict(pool, resource=None), dict(value, resource={"id": "8", "path": ""}),
               dict(value, action="client", resource={"id": "8", "path": ""})]
    for bad in refused:
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64)


def test_evidence_refuses_daemons_outside_a_service_unit(core_request):
    pool = dict(observation(core_request), request="d" * 64, action="pool", resource={"id": "8", "path": ""})
    assert ceph_core.evidence(copy.deepcopy(pool), "d" * 64)["daemons"] == []
    daemon = {"host": "node-1", "name": "nfs.files.0.0.node-1.abcdef", "service": "nfs.files"}
    for bad in (dict(pool, daemons=[daemon]), {key: item for key, item in pool.items() if key != "daemons"}, dict(pool, daemons=None)):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64)


def test_resource_phases_ship_in_their_own_module():
    modules = ceph_native_worker.MODULES
    assert modules[modules.index("ceph_native") + 1] == "ceph_native_resources"
    assert modules[modules.index("ceph_native_resources") + 1] == "ceph_native_nfs"
    assert modules[modules.index("ceph_native_nfs") + 1] == "ceph_native_observability"
    assert modules[modules.index("ceph_native_observability") + 1] == "ceph_native_units"
    assert modules[modules.index("ceph_native_units") + 1] == "ceph_native_object"
    assert modules[modules.index("ceph_native_object") + 1] == "ceph_smb_native"
    assert tuple(ceph_core.Transport.native_sources()) == modules
    phases = {"pool", "client", "client_read", "policy", "rbd", "filesystem", "filesystem_ranks", "filesystem_groups", "subvolume", "settings"}
    assert ceph_native_resources.PHASES == phases and not ceph_native_resources.PHASES & ceph_native.CORE_PHASES
    assert ceph_native_resources.READ_ONLY_PHASES == {"client_read", "filesystem_ranks"}
    assert not any(hasattr(ceph_native.Worker, name) for name in phases)
    assert all(hasattr(ceph_native_resources.ResourceWorker, name) for name in phases)


def test_dispatch_routes_resource_actions_to_the_resource_worker(monkeypatch, core_request):
    reached = []

    def recording(kind):
        class Recording:
            def __init__(self, request, host):
                self.request, self.blocking, self.selection = request, [], native_specializations.Selection({})

            def clock(self):
                return 0

            def __getattr__(self, name):
                return lambda *arguments: reached.append((kind, name)) or {}
        return Recording

    monkeypatch.setattr(ceph_native, "Worker", recording("core"))
    monkeypatch.setattr(ceph_native_resources, "ResourceWorker", recording("resource"))
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)

    def send(action, phase):
        request = dict(core_request, action=action)
        return ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": {}})

    for action, phase in (("pool", "pool"), ("pool", "observe"), ("client", "client"), ("client", "client_read"), ("client", "observe"),
                          ("policy", "policy"), ("policy", "observe")):
        send(action, phase)
    send("cluster", "observe")
    assert reached == [("resource", "pool"), ("resource", "observe"), ("resource", "client"), ("resource", "client_read"),
                       ("resource", "observe"), ("resource", "policy"), ("resource", "observe"), ("core", "observe")]
    for phase in ("pool", "client", "client_read", "policy"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            send("cluster", phase)
    assert len(reached) == 8


def test_host_profile_relays_the_bound_bundle(core_request, tmp_path):
    scope = {key: value for key, value in core_request.items() if key != "action"}
    bound = dict(scope, registryTrustBundleRef="registry-ca")
    bundle_text = SYNTHETIC_CA.decode("utf-8")
    projection = "".join(ceph_registry_discovery.certificates(SYNTHETIC_CA))
    paths = {"sshKey": "PRIVATE-KEY-SENTINEL", "knownHosts": "192.0.2.1 ssh-ed25519 PUBLIC",
             "registryJSON": json.dumps({"url": scope["registry"], "username": "registry-user", "password": PASSWORD}), "registryCA": bundle_text}
    for name, text in paths.items():
        (tmp_path / name).write_text(text)
        (tmp_path / name).chmod(0o600)
    material = {name: str(tmp_path / name) for name in paths}
    calls = []

    def run(argv, data, timeout):
        calls.append((argv, data, timeout))
        return 0, b'{"ok":true,"result":{"changed":true}}'

    assert ceph_core.Transport(bound, material, run=run, scope=True).host_profile(bound["hosts"][0]) == {"changed": True}
    argv, data, unused_timeout = calls[0]
    assert json.loads(data)["payload"]["private"] == {"registryJSON": paths["registryJSON"], "registryCA": projection}
    assert projection.isascii() and "#" not in projection and ceph_pull_broker.bundle_context(projection).cert_store_stats()["x509_ca"] == 1
    assert projection.splitlines()[1] not in " ".join(argv) and PASSWORD not in " ".join(argv)
    unbound = {name: path for name, path in material.items() if name != "registryCA"}
    for request_value, private in ((scope, material), (bound, unbound)):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            ceph_core.Transport(request_value, private, run=run, scope=True).host_profile(request_value["hosts"][0])
    assert len(calls) == 1


@pytest.mark.parametrize("kind", ["fixture", "invalid-der", "not-utf8", "no-certificate", "oversized-utf8"])
def test_every_bundle_consumer_reaches_the_same_verdict(core_request, tmp_path, kind):
    bundles = {
        "fixture": SYNTHETIC_CA,
        "invalid-der": SYNTHETIC_CA + b"-----BEGIN CERTIFICATE-----\nQUJD\n-----END CERTIFICATE-----\n",
        "not-utf8": b"# \xff\n" + SYNTHETIC_CA,
        "no-certificate": b"# caf\xc3\xa9 and no certificate block\n",
        "oversized-utf8": b"# " + b"\xc3\xa9" * (ceph_pull_broker.MAX_TRUST // 2) + b"\n" + SYNTHETIC_CA,
    }
    tmp_path.chmod(0o700)
    scope = dict({key: value for key, value in core_request.items() if key != "action"}, registryTrustBundleRef="registry-ca")
    paths = {"sshKey": b"PRIVATE-KEY-SENTINEL", "knownHosts": b"192.0.2.1 ssh-ed25519 PUBLIC", "registryCA": bundles[kind],
             "registryJSON": json.dumps({"url": scope["registry"], "username": "registry-user", "password": PASSWORD}).encode()}
    for name, data in paths.items():
        (tmp_path / name).write_bytes(data)
        (tmp_path / name).chmod(0o600)
    calls = []

    def run(argv, data, timeout):
        calls.append(json.loads(data)["payload"]["private"]["registryCA"])
        return 0, b'{"ok":true,"result":{"changed":true}}'

    transport = ceph_core.Transport(scope, {name: str(tmp_path / name) for name in paths}, run=run, scope=True)
    if kind != "fixture":
        with pytest.raises(ceph_registry_discovery.DiscoveryRefusal, match="^material$"):
            with ceph_registry_discovery.bundle_proof(str(tmp_path / "registryCA")):
                pytest.fail("discovery accepted the bundle")
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            transport.host_profile(scope["hosts"][0])
        assert calls == []
        return
    with ceph_registry_discovery.bundle_proof(str(tmp_path / "registryCA")) as (context, prove):
        prove()
        resolved = context.get_ca_certs()
    assert transport.host_profile(scope["hosts"][0]) == {"changed": True}
    assert ceph_pull_broker.bundle_context(calls[0]).get_ca_certs() == resolved


@pytest.mark.parametrize("value", ["", "Bad Name", "a\nb", 5])
def test_scope_refuses_a_bundle_reference_that_is_not_a_name(core_request, value):
    scope = {key: item for key, item in core_request.items() if key != "action"}
    assert ceph_native.validate_scope(dict(scope, registryTrustBundleRef="registry-ca")) is not None
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate_scope(dict(scope, registryTrustBundleRef=value))


def test_host_profile_refuses_an_oversized_bound_bundle_before_the_run(core_request, tmp_path):
    scope = dict({key: value for key, value in core_request.items() if key != "action"}, registryTrustBundleRef="registry-ca")
    paths = {"sshKey": "PRIVATE-KEY-SENTINEL", "knownHosts": "192.0.2.1 ssh-ed25519 PUBLIC",
             "registryJSON": json.dumps({"url": scope["registry"], "username": "registry-user", "password": PASSWORD}),
             "registryCA": "#" * (ceph_pull_broker.MAX_TRUST + 1)}
    for name, text in paths.items():
        (tmp_path / name).write_text(text)
        (tmp_path / name).chmod(0o600)
    transport = ceph_core.Transport(scope, {name: str(tmp_path / name) for name in paths}, run=lambda *args: pytest.fail("run"), scope=True)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        transport.host_profile(scope["hosts"][0])
