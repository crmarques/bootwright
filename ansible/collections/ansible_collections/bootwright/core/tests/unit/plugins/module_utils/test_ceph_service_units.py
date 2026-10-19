"""The generic service-unit worker and its NFS hooks: every refusal before any write, one ordered batch, and read-only observation.

The scripted monitor answers the upstream v20.2.1 shapes the worker reads:
- `orch apply -i -` reads a multi-document YAML stream and applies its specifications in stream order
  (src/pybind/mgr/orchestrator/module.py:1826-1846, :1904-1916); `orch ls --export` lists each stored
  specification with its `service_name`;
- configuring an NFS service creates the `.nfs` pool and enables its `nfs` application asynchronously
  (src/pybind/mgr/cephadm/services/nfs.py:64-68, src/pybind/mgr/nfs/cluster.py:45-54); `osd pool ls detail`
  lists `pool_name`, `pool_id` and `application_metadata` (src/osd/osd_types.cc:1568-1653);
- `nfs cluster ls` lists the service IDs of the orchestrator's nfs services (src/pybind/mgr/nfs/module.py:151-155,
  src/pybind/mgr/nfs/utils.py:64-79);
- `orch ps` rows carry `daemon_type`, `daemon_id`, `hostname`, `service_name`, `status` and
  `container_image_digests` (src/pybind/mgr/orchestrator/_interface.py:1471-1500);
- `mgr module ls` lists `enabled_modules` and `always_on_modules` (src/mon/MgrMonitor.cc:1016-1050) and `config dump`
  one entry per section and option (src/mon/ConfigMonitor.cc:242-291);
- cephadm keeps generated ingress passwords in the manager store, never in the stored specification
  (src/pybind/mgr/cephadm/services/ingress.py:125-137, :278-290), so a stored specification carrying them is foreign.
"""

from __future__ import annotations

import contextlib
import copy
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_native, ceph_native_nfs, ceph_native_units, ceph_tunnel_staging,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings

FSID = settings.FSID
OWNER = settings.OWNER
HAPROXY_KEY = settings.HAPROXY_KEY
KEEPALIVED_KEY = settings.KEEPALIVED_KEY
SECRET = "Sx9-monitor-and-keepalived-value"
REACHABLE = ceph_native_nfs.reachable


def cluster():
    return dict(settings.cluster(), clusterSSHTransport="verified-tunnel")


def documents(mode):
    two = {"count": 2, "hosts": ["node-1", "node-2"]}
    if mode == "haproxy":
        ingress = {"backend_service": "nfs.files", "enable_haproxy_protocol": True, "frontend_port": 2049, "monitor_port": 9049, "virtual_ip": "192.0.2.50/24"}
        return [{"placement": dict(two), "service_id": "nfs.files", "service_type": "ingress", "spec": ingress},
                {"placement": dict(two), "service_id": "files", "service_type": "nfs", "spec": {"enable_haproxy_protocol": True, "port": 12049}}]
    if mode == "keepalive":
        return [{"placement": dict(two), "service_id": "nfs.files", "service_type": "ingress",
                 "spec": {"backend_service": "nfs.files", "keepalive_only": True, "virtual_ip": "192.0.2.50/24"}},
                {"placement": {"count": 1, "hosts": ["node-1", "node-2"]}, "service_id": "files", "service_type": "nfs",
                 "spec": {"port": 2050, "virtual_ip": "192.0.2.50"}}]
    return [{"placement": dict(two), "service_id": "files", "service_type": "nfs"}]


def unit_request(mode="haproxy"):
    unit = documents(mode)
    request = dict(cluster(), action="service", service={"documents": unit, "id": "files", "type": "nfs"})
    request["delegated"] = sorted(({"name": d["service_type"] + "." + d["service_id"], "hosts": d["placement"]["hosts"]} for d in unit),
                                  key=lambda entry: entry["name"])
    if mode == "haproxy":
        request["auxiliary"] = copy.deepcopy(settings.AUXILIARY)
    elif mode == "keepalive":
        request["auxiliary"] = [entry for entry in copy.deepcopy(settings.AUXILIARY) if entry["name"] == "keepalived"]
    return request


def name_of(document):
    return document["service_type"] + "." + document["service_id"]


def nfs_pool(identifier=9, application=True):
    return {"pool_name": ".nfs", "pool_id": identifier, "application_metadata": {"nfs": {}} if application else {}}


def held_images(request):
    return [settings.entry("mgr/cephadm/container_image_" + entry["name"], entry["image"]) for entry in request.get("auxiliary", [])]


class Monitor:
    """A scripted monitor: reads answer from its tables; `orch apply` stores its documents and, like cephadm, creates `.nfs`."""

    def __init__(self, request, modules=("nfs",), pools=None, config=None, specs=None, running=None, creates_pool=True):
        self.request = request
        self.modules = list(modules)
        self.pools = list(pools or [])
        self.config = held_images(request) if config is None else list(config)
        self.specs = dict(specs or {})
        self.running = list(running or [])
        self.clusters = []
        self.creates_pool = creates_pool
        self.log, self.mutations, self.batches, self.on_apply = [], [], [], None

    def run(self, argv, data=b"", timeout=60):
        del timeout
        assert "--force" not in argv
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append(command)
        answers = {
            ("fsid",): {"fsid": FSID},
            ("mgr", "module", "ls"): {"always_on_modules": ["cephadm"], "force_disabled_modules": [], "enabled_modules": self.modules},
            ("osd", "pool", "ls", "detail"): self.pools,
            ("config", "dump"): self.config,
            ("orch", "ls", "--export"): [dict(value, service_name=name) for name, value in self.specs.items()],
            ("orch", "ps"): self.running,
            ("nfs", "cluster", "ls"): self.clusters,
        }
        if command in answers:
            return 0, json.dumps(answers[command]).encode()
        self.mutations.append(command)
        if command == ("orch", "apply", "-i", "-"):
            if self.on_apply is not None:
                self.on_apply()
            batch = [json.loads(part) for part in data.split(b"---\n")]
            self.batches.append(batch)
            for value in batch:
                self.specs[name_of(value)] = value
                if value["service_type"] == "nfs":
                    self.clusters.append(value["service_id"])
            if self.creates_pool and not any(pool["pool_name"] == ".nfs" for pool in self.pools):
                self.pools.append(nfs_pool())
        return 0, b""


@pytest.fixture(autouse=True)
def contained_normalization(monkeypatch):
    """Identity normalization in place of the frozen image's SDK; the real record and host scope still run."""
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **options: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native_nfs, "reachable", lambda host, number: True)


def markers_for(tmp_path, request):
    markers = ceph_native.Markers(request, root=str(tmp_path))
    os.makedirs(markers.path, mode=0o700)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", scope(request))
    return markers


def records(markers):
    return sorted(os.listdir(markers.path))


def worker(monitor, markers):
    native = ceph_native_units.UnitWorker(monitor.request, "node-1", run=monitor.run, markers=markers)
    native.pause = lambda seconds: None
    return native


def scope(request):
    """The cluster block's bootstrap scope: the planner builds a unit request from the cluster request, adding only its unit."""
    cluster_request = dict({key: value for key, value in request.items() if key != "service"}, action="cluster")
    return ceph_tunnel_staging.recovery_scope(cluster_request)


def seed_records(markers, request, complete=False):
    """What an earlier attempt of this unit retained: its inputs record, the `.nfs` claim and each attempt."""
    groups = [[document] for document in request["service"]["documents"]]
    markers.write("unit-nfs-files-inputs.json", {"version": "ceph-service-inputs-v1", "services": groups,
                                                 "scope": {"owner": OWNER, "fsid": FSID, "image": request["image"], "inputs": groups}})
    markers.write("nfs-pool.json", {"owner": OWNER, "fsid": FSID, "pool": ".nfs"})
    for document in request["service"]["documents"]:
        intent = {"scope": scope(request), "service": document}
        markers.write("service-" + name_of(document) + "-attempt.json", intent)
        if complete:
            markers.write("service-" + name_of(document) + "-complete.json", intent)


def daemon(request, kind, service, host, identifier, status=1):
    images = {entry["name"]: entry["image"] for entry in request.get("auxiliary", [])}
    return {"daemon_type": kind, "daemon_id": identifier, "hostname": host, "service_name": service, "status": status,
            "container_image_digests": [request["image"] if kind == "nfs" else images[kind]]}


def test_nfs_unit_applies_nfs_and_ingress_in_one_batch(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    names = ["service-ingress.nfs.files", "service-nfs.files"]
    seen = []
    monitor.on_apply = lambda: seen.append(records(markers))
    assert worker(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")]
    assert [[name_of(value) for value in batch] for batch in monitor.batches] == [["ingress.nfs.files", "nfs.files"]]
    assert monitor.batches[0] == request["service"]["documents"]
    assert all(name + "-attempt.json" in seen[0] and name + "-complete.json" not in seen[0] for name in names)
    assert all(name + "-complete.json" in records(markers) for name in names)
    assert markers.read("nfs-pool.json") == {"owner": OWNER, "fsid": FSID, "pool": ".nfs"}
    assert markers.read("nfs-pool-id.json") == {"id": 9}
    assert "nfs-pool.json" in seen[0]


def test_nfs_unit_direct_mode_applies_only_the_nfs_document(tmp_path):
    request = unit_request("direct")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request, config=[])
    assert worker(monitor, markers).unit() == {"changed": True}
    assert monitor.batches == [request["service"]["documents"]]
    assert ("config", "dump") not in monitor.log
    assert monitor.mutations == [("orch", "apply", "-i", "-")]


def test_nfs_unit_refuses_preexisting_nfs_pool_without_marker(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    before = records(markers)
    monitor = Monitor(request, pools=[nfs_pool(3)])
    with pytest.raises(ceph_native.Refusal, match="^nfs-pool-foreign$"):
        worker(monitor, markers).unit()
    assert records(markers) == before
    assert monitor.mutations == []


def test_nfs_unit_refuses_disabled_module_naming_the_settings_block(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    before = records(markers)
    monitor = Monitor(request, modules=("iostat",))
    with pytest.raises(ceph_native.Refusal, match="^nfs-module-disabled$"):
        worker(monitor, markers).unit()
    assert records(markers) == before
    assert monitor.mutations == []
    assert not any(command[:3] == ("mgr", "module", "enable") for command in monitor.log)


def test_nfs_unit_readiness_names_missing_daemon(tmp_path):
    request = unit_request("keepalive")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    monitor.running = [daemon(request, "keepalived", "ingress.nfs.files", host, "nfs.files." + host + ".abcdef") for host in ("node-1", "node-2", "node-1")]
    monitor.running[2]["daemon_id"] = "nfs.files.node-1.ghijkl"
    native = worker(monitor, markers)
    assert native.observe_resource(native.empty_evidence(), True) is False
    assert "nfs@nfs.files" in native.blocking and "keepalived@ingress.nfs.files" in native.blocking
    monitor.running = [daemon(request, "keepalived", "ingress.nfs.files", host, "nfs.files." + host + ".abcdef") for host in ("node-1", "node-2")]
    monitor.running.append(daemon(request, "nfs", "nfs.files", "node-1", "files.0.0.node-1.mnopqr"))
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is True and native.blocking == []
    assert evidence["daemons"] == [{"host": "node-1", "name": "keepalived.nfs.files.node-1.abcdef", "service": "ingress.nfs.files"},
                                   {"host": "node-2", "name": "keepalived.nfs.files.node-2.abcdef", "service": "ingress.nfs.files"},
                                   {"host": "node-1", "name": "nfs.files.0.0.node-1.mnopqr", "service": "nfs.files"}]


def test_nfs_unit_readiness_refuses_a_daemon_on_another_image(tmp_path):
    request = unit_request("direct")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    row = daemon(request, "nfs", "nfs.files", "node-1", "files.0.0.node-1.abcdef")
    row["container_image_digests"] = ["registry.example.test/ceph@sha256:" + "f" * 64]
    monitor.running = [row]
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        native.observe_resource(native.empty_evidence(), True)


def test_nfs_unit_evidence_carries_no_password(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    stored = dict(copy.deepcopy(request["service"]["documents"][0]))
    stored["spec"] = dict(stored["spec"], monitor_password=SECRET, keepalived_password=SECRET)
    monitor = Monitor(request, specs={"ingress.nfs.files": stored})
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
        worker(monitor, markers).unit()
    assert SECRET not in repr(caught.value.__dict__) and SECRET not in str(caught.value)
    assert monitor.mutations == []
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
        native.observe_resource(native.empty_evidence(), True)
    assert SECRET not in repr(caught.value.__dict__)
    seed_records(markers, request, complete=True)
    monitor.specs = {"ingress.nfs.files": stored, "nfs.files": request["service"]["documents"][1]}
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
        native.observe_resource(native.empty_evidence(), True)
    assert SECRET not in repr(caught.value.__dict__)
    assert all(SECRET.encode() not in open(os.path.join(markers.path, name), "rb").read() for name in records(markers))


def test_nfs_unit_published_evidence_carries_no_stored_spec(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    monitor.specs["ingress.nfs.files"]["spec"]["monitor_password"] = SECRET
    monitor.running = [daemon(request, kind, "ingress.nfs.files", host, "nfs.files." + host + ".abcdef")
                       for kind in ("haproxy", "keepalived") for host in ("node-1", "node-2")]
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
        native.observe_resource(native.empty_evidence(), True)
    assert SECRET not in repr(caught.value.__dict__)
    del monitor.specs["ingress.nfs.files"]["spec"]["monitor_password"]
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    native.observe_resource(evidence, True)
    assert SECRET not in json.dumps(evidence) and set(evidence["daemons"][0]) == {"host", "name", "service"}


def test_nfs_unit_replay_after_marker_before_apply(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request)
    monitor = Monitor(request)
    assert worker(monitor, markers).unit() == {"changed": True}
    assert len(monitor.batches) == 1 and [name_of(value) for value in monitor.batches[0]] == ["ingress.nfs.files", "nfs.files"]
    assert all("service-" + name + "-complete.json" in records(markers) for name in ("ingress.nfs.files", "nfs.files"))


def test_nfs_unit_replay_after_apply_before_readiness(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request)
    specs = {name_of(document): document for document in request["service"]["documents"]}
    monitor = Monitor(request, pools=[nfs_pool(9)], specs=specs)
    monitor.clusters = ["files"]
    assert worker(monitor, markers).unit() == {"changed": False}
    assert monitor.mutations == []
    assert markers.read("nfs-pool-id.json") == {"id": 9}
    monitor.running = [daemon(request, kind, "ingress.nfs.files", host, "nfs.files." + host + ".abcdef")
                       for kind in ("haproxy", "keepalived") for host in ("node-1", "node-2")]
    monitor.running += [daemon(request, "nfs", "nfs.files", host, "files." + str(i) + ".0." + host + ".abcdef") for i, host in enumerate(("node-1", "node-2"))]
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is True
    assert evidence["owned"] and evidence["exists"] and len(evidence["daemons"]) == 6


def test_nfs_unit_refuses_an_attempted_service_stored_with_another_spec(tmp_path):
    """An attempt lost before its apply, then the service created outside with another spec: never completed, never adopted."""
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request)
    specs = {"nfs.files": dict(request["service"]["documents"][1], spec={"port": 2050})}
    before = records(markers)
    monitor = Monitor(request, specs=specs)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        worker(monitor, markers).unit()
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        native.observe_resource(native.empty_evidence(), True)
    assert records(markers) == before and monitor.mutations == []
    assert not any(name.endswith("-complete.json") for name in records(markers))


class ForeignMarkers(ceph_native.Markers):
    def owned(self):
        return False


def test_nfs_unit_refuses_a_cluster_this_operation_does_not_own_before_any_read_or_write(tmp_path):
    request = unit_request("haproxy")
    markers = ForeignMarkers(request, root=str(tmp_path))
    os.makedirs(markers.path, mode=0o700)
    monitor = Monitor(request)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker(monitor, markers).unit()
    assert monitor.log == [] and records(markers) == []


def test_nfs_unit_refuses_another_clusters_fsid_before_any_other_read(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    run = monitor.run
    other = json.dumps({"fsid": "00000000-0000-0000-0000-000000000000"}).encode()
    monitor.run = lambda argv, data=b"", timeout=60: (0, other) if argv[-1] == "fsid" else run(argv, data)
    before = records(markers)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker(monitor, markers).unit()
    assert monitor.log == [] and records(markers) == before


def test_nfs_unit_refuses_an_absent_ingress_image_setting_without_writing(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    before = records(markers)
    monitor = Monitor(request, config=[settings.entry(HAPROXY_KEY, request["auxiliary"][0]["image"])])
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        worker(monitor, markers).unit()
    assert caught.value.native_error["cause"] == KEEPALIVED_KEY
    assert "sha256" not in caught.value.native_error["cause"]
    assert records(markers) == before and monitor.mutations == []
    keepalive = unit_request("keepalive")
    monitor = Monitor(keepalive, config=held_images(keepalive))
    assert worker(monitor, markers_for(tmp_path / "keepalive", keepalive)).unit() == {"changed": True}


def test_nfs_unit_refuses_completed_service_drift(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    del monitor.specs["nfs.files"]
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        worker(monitor, markers).unit()
    assert len(monitor.batches) == 1
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        native.observe_resource(native.empty_evidence(), True)
    monitor.specs["nfs.files"] = dict(request["service"]["documents"][1], spec={"port": 2049})
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        worker(monitor, markers).unit()
    assert len(monitor.batches) == 1


def test_nfs_unit_refuses_a_replaced_nfs_pool(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request, complete=True)
    markers.write("nfs-pool-id.json", {"id": 7})
    specs = {name_of(document): document for document in request["service"]["documents"]}
    monitor = Monitor(request, pools=[nfs_pool(9)], specs=specs)
    with pytest.raises(ceph_native.Refusal, match="^nfs-pool-foreign$"):
        worker(monitor, markers).unit()
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^nfs-pool-foreign$"):
        native.observe_resource(native.empty_evidence(), True)
    monitor.pools = []
    with pytest.raises(ceph_native.Refusal, match="^nfs-pool-foreign$"):
        worker(monitor, markers).unit()
    assert monitor.mutations == []


@pytest.mark.parametrize("mode,port", [("haproxy", 2049), ("keepalive", 2050)])
def test_nfs_unit_vip_probe_targets_the_mode_port(tmp_path, monkeypatch, mode, port):
    request = unit_request(mode)
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    probes = []

    def connect(address, timeout):
        probes.append((address, timeout))
        raise ConnectionRefusedError

    monkeypatch.setattr(ceph_native_nfs, "reachable", REACHABLE)
    monkeypatch.setattr(ceph_native_nfs.socket, "create_connection", connect)
    native = worker(monitor, markers)
    assert ceph_native_nfs.readiness(native, request["service"], native.blocking) is False
    assert probes == [(("192.0.2.50", port), 5)]
    assert native.blocking == ["vip@ingress.nfs.files"]


def test_nfs_unit_observe_reads_only(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request, complete=True)
    specs = {name_of(document): document for document in request["service"]["documents"]}
    monitor = Monitor(request, pools=[nfs_pool(9)], specs=specs)
    before = records(markers)
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is False
    assert monitor.mutations == [] and records(markers) == before
    assert evidence["owned"] and evidence["exists"]


def ready_unit(tmp_path):
    """A haproxy-mode unit whose every readiness condition holds; each test below breaks exactly one."""
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).unit()
    monitor.running = [daemon(request, kind, "ingress.nfs.files", host, "nfs.files." + host + ".abcdef")
                       for kind in ("haproxy", "keepalived") for host in ("node-1", "node-2")]
    monitor.running += [daemon(request, "nfs", "nfs.files", host, "files." + str(i) + ".0." + host + ".abcdef") for i, host in enumerate(("node-1", "node-2"))]
    native = worker(monitor, markers)
    assert native.observe_resource(native.empty_evidence(), True) is True and native.blocking == []
    return markers, monitor


def observe(monitor, markers):
    native = worker(monitor, markers)
    return native.observe_resource(native.empty_evidence(), True), native.blocking


def test_nfs_unit_readiness_needs_the_cluster_listed_by_the_nfs_module(tmp_path):
    markers, monitor = ready_unit(tmp_path)
    monitor.clusters = []
    before = records(markers)
    assert observe(monitor, markers)[0] is False
    assert records(markers) == before and monitor.mutations == [("orch", "apply", "-i", "-")]


def test_nfs_unit_readiness_needs_the_recorded_pool_identity(tmp_path):
    markers, monitor = ready_unit(tmp_path)
    os.remove(os.path.join(markers.path, "nfs-pool-id.json"))
    before = records(markers)
    assert observe(monitor, markers)[0] is False
    assert records(markers) == before and markers.read("nfs-pool-id.json") is None
    assert monitor.mutations == [("orch", "apply", "-i", "-")]


def test_nfs_unit_observation_refuses_an_unclaimed_nfs_pool(tmp_path):
    markers, monitor = ready_unit(tmp_path)
    os.remove(os.path.join(markers.path, "nfs-pool.json"))
    before = records(markers)
    with pytest.raises(ceph_native.Refusal, match="^nfs-pool-foreign$"):
        observe(monitor, markers)
    assert records(markers) == before


def test_nfs_unit_settle_waits_for_the_nfs_application(tmp_path):
    request = unit_request("direct")
    markers = markers_for(tmp_path, request)
    markers.write("nfs-pool.json", {"owner": OWNER, "fsid": FSID, "pool": ".nfs"})
    monitor = Monitor(request, pools=[nfs_pool(9, application=False)], creates_pool=False)
    native = worker(monitor, markers)
    ticks = iter(range(0, 10000, 5))
    native.clock = lambda: next(ticks)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        ceph_native_nfs.settle(native, request["service"])
    assert markers.read("nfs-pool-id.json") is None
    assert monitor.log.count(("osd", "pool", "ls", "detail")) > 1 and monitor.mutations == []


def test_nfs_unit_readiness_does_not_count_a_daemon_without_image_digests(tmp_path):
    markers, monitor = ready_unit(tmp_path)
    row = next(row for row in monitor.running if row["daemon_type"] == "nfs")
    row["container_image_digests"] = []
    ready, blocking = observe(monitor, markers)
    assert ready is False and blocking == ["nfs@nfs.files"]


@pytest.mark.parametrize("foreign", ["stored", "running"])
def test_nfs_unit_observation_refuses_an_unowned_service_created_outside(tmp_path, foreign):
    request = unit_request("direct")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    if foreign == "stored":
        monitor.specs = {"nfs.files": request["service"]["documents"][0]}
    else:
        monitor.running = [daemon(request, "nfs", "nfs.files", "node-1", "files.0.0.node-1.abcdef")]
    before = records(markers)
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        native.observe_resource(native.empty_evidence(), True)
    assert records(markers) == before and monitor.mutations == []


def test_nfs_unit_readiness_needs_the_nfs_application_on_the_recorded_pool(tmp_path):
    markers, monitor = ready_unit(tmp_path)
    monitor.pools[0]["application_metadata"] = {}
    before = records(markers)
    assert observe(monitor, markers)[0] is False
    assert records(markers) == before and markers.read("nfs-pool-id.json") == {"id": 9}
    assert monitor.mutations == [("orch", "apply", "-i", "-")]


def test_nfs_unit_readiness_needs_every_completion_record(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    seed_records(markers, request)
    markers.write("nfs-pool-id.json", {"id": 9})
    specs = {name_of(document): document for document in request["service"]["documents"]}
    monitor = Monitor(request, pools=[nfs_pool(9)], specs=specs)
    monitor.clusters = ["files"]
    monitor.running = [daemon(request, kind, "ingress.nfs.files", host, "nfs.files." + host + ".abcdef")
                       for kind in ("haproxy", "keepalived") for host in ("node-1", "node-2")]
    monitor.running += [daemon(request, "nfs", "nfs.files", host, "files." + str(i) + ".0." + host + ".abcdef") for i, host in enumerate(("node-1", "node-2"))]
    before = records(markers)
    native = worker(monitor, markers)
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is False
    assert evidence["owned"] and native.blocking == []
    assert records(markers) == before and monitor.mutations == []


def test_nfs_unit_refuses_a_readback_that_differs_from_the_applied_document(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    run = monitor.run

    def rewriting(argv, data=b"", timeout=60):
        result = run(argv, data, timeout)
        if tuple(argv[-4:]) == ("orch", "apply", "-i", "-"):
            monitor.specs["nfs.files"] = dict(monitor.specs["nfs.files"], spec={"enable_haproxy_protocol": True, "port": 2049})
        return result

    monitor.run = rewriting
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        worker(monitor, markers).unit()
    assert len(monitor.batches) == 1
    assert not any(name.endswith("-complete.json") for name in records(markers))


@pytest.mark.parametrize("record", ["foreign attempt", "completion without attempt"])
def test_nfs_unit_refuses_a_service_record_that_is_not_its_intent(tmp_path, record):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    specs = {}
    if record == "foreign attempt":
        seed_records(markers, request)
        other = dict(request["service"]["documents"][1], spec={"port": 2049})
        os.remove(os.path.join(markers.path, "service-nfs.files-attempt.json"))
        markers.write("service-nfs.files-attempt.json", {"scope": scope(request), "service": other})
    else:
        seed_records(markers, request, complete=True)
        os.remove(os.path.join(markers.path, "service-nfs.files-attempt.json"))
        specs = {name_of(document): document for document in request["service"]["documents"]}
    before = records(markers)
    monitor = Monitor(request, specs=specs)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker(monitor, markers).unit()
    native = worker(monitor, markers)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.observe_resource(native.empty_evidence(), True)
    assert records(markers) == before and monitor.mutations == []


def test_unit_unfinished_reports_its_own_inputs_record(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    native = worker(Monitor(request), markers)
    assert native.unfinished(native.empty_evidence())["owned"] is False
    seed_records(markers, request)
    evidence = native.unfinished(native.empty_evidence())
    assert evidence["owned"] and evidence["exists"] and evidence["partial"]


def test_unit_scope_is_the_clusters_bootstrap_scope(tmp_path):
    request = unit_request("haproxy")
    markers = markers_for(tmp_path, request)
    assert worker(Monitor(request), markers).recovery_scope() == scope(request)
    assert "service" not in scope(request)["cluster"] and scope(request)["cluster"]["delegated"] == request["delegated"]


def without(request, path):
    *parents, last = path
    target = request
    for key in parents:
        target = target[key]
    del target[last]
    return request


def changed(request, path, value):
    *parents, last = path
    target = request
    for key in parents:
        target = target[key]
    target[last] = value
    return request


INVALID = {
    "unknown type": lambda: changed(unit_request(), ("service", "type"), "smb"),
    "id mismatch": lambda: changed(unit_request(), ("service", "id"), "other"),
    "nfs before ingress": lambda: changed(unit_request(), ("service", "documents"), list(reversed(documents("haproxy")))),
    "wrong backend": lambda: changed(unit_request(), ("service", "documents", 0, "spec", "backend_service"), "nfs.other"),
    "spec member outside the record": lambda: changed(unit_request(), ("service", "documents", 1, "spec", "enable_nfsv3"), True),
    "host outside the request": lambda: changed(unit_request("direct"), ("service", "documents", 0, "placement", "hosts"), ["node-1", "node-9"]),
    "document absent from delegated": lambda: changed(unit_request(), ("delegated",), unit_request()["delegated"][1:]),
    "ingress without its auxiliary images": lambda: without(unit_request(), ("auxiliary",)),
    "service on a pool action": lambda: dict(unit_request(), action="pool", pool={"name": "volumes"}),
    "service on a cluster action": lambda: dict(unit_request(), action="cluster"),
    "pool member on a service action": lambda: dict(unit_request(), pool={"name": "volumes"}),
    "extra unit member": lambda: changed(unit_request(), ("service", "extra"), 1),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_unit_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


@pytest.mark.parametrize("mode", ["haproxy", "keepalive", "direct"])
def test_unit_request_admits_each_mode(mode):
    request = unit_request(mode)
    assert ceph_native.validate(request) is request


def test_dispatch_routes_a_unit_to_the_unit_worker(monkeypatch):
    reached = []
    monkeypatch.setattr(ceph_native_units.UnitWorker, "unit", lambda self: reached.append(type(self)) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = unit_request()
    assert ceph_native.dispatch({"request": request, "host": "node-1", "phase": "unit", "private": {}}) == {"changed": False}
    assert reached == [ceph_native_units.UnitWorker]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch({"request": request, "host": "node-1", "phase": "pool", "private": {}})
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch({"request": dict(request, action="cluster", service=None), "host": "node-1", "phase": "unit", "private": {}})


def test_dispatch_runs_the_unit_phase_only_over_a_supported_transport(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: False)
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": unit_request(), "host": "node-1", "phase": "unit", "private": {}})


def observation(request, ready, daemons=()):
    return {"version": "ceph-core-evidence-v5", "request": "", "action": "service", "owner": OWNER, "fsid": FSID, "owned": True, "exists": True,
            "ready": ready, "removed": False, "partial": not ready, "health": "HEALTH_OK", "hosts": [h["name"] for h in request["hosts"]],
            "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": {"id": "", "path": ""}, "client": "",
            "dashboard": {"url": "", "certificateSHA256": "", "reachable": False}, "daemons": list(daemons)}


class Transport:
    def __init__(self, request, ready_after=1, blocking=(), observed=None):
        self.request, self.calls, self.ready_after, self.blocking = request, [], ready_after, list(blocking)
        self.observed = dict(observed or {})

    def call(self, host, phase, timeout=None):
        self.calls.append((host["name"], phase))
        if phase == "observe":
            assert timeout is None or 0 < timeout <= 660
            ready = sum(call[1] == "observe" for call in self.calls) >= self.ready_after
            daemons = [{"host": "node-1", "name": "nfs.files.0.0.node-1.abcdef", "service": "nfs.files"}] if ready else []
            return dict(observation(self.request, ready, daemons), blocking=[] if ready else self.blocking, **self.observed)
        if phase == "prerequisites":
            return {"name": host["name"], "owned": True, "exists": True}
        return {"changed": phase == "unit"}


def test_core_execute_runs_the_unit_on_the_bootstrap_host_only(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = unit_request("direct")
    transport = Transport(request, ready_after=2, blocking=["nfs@nfs.files", "vip@ingress.nfs.files"])
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["changed"] is True and result["evidence"]["ready"] is True
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "unit"), ("node-1", "observe"), ("node-1", "observe")]
    transport = Transport(request)
    result = ceph_core.execute(request, "observe", "d" * 64, transport)
    assert transport.calls == [("node-1", "observe")] and result["changed"] is False


def test_core_destroy_runs_the_unit_destroy_then_one_observation_on_the_bootstrap_host(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = unit_request("direct")
    removed = {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": []}
    transport = Transport(request, observed=removed)
    result = ceph_core.execute(request, "destroy", "d" * 64, transport)
    assert transport.calls == [("node-1", "destroy"), ("node-1", "observe")] and result["evidence"]["removed"] is True
    transport = Transport(request, ready_after=99, observed={"partial": True})
    with pytest.raises(ceph_native.Refusal, match="^not-removed$"):
        ceph_core.execute(request, "destroy", "d" * 64, transport)
    assert transport.calls == [("node-1", "destroy"), ("node-1", "observe")]


@pytest.mark.parametrize("lost", ["owned", "exists", "partial"])
def test_core_unit_wait_refuses_an_unowned_or_settled_unready_unit_without_polling(monkeypatch, lost):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = unit_request("direct")
    clock = iter(range(0, 100000, 300))
    transport = Transport(request, ready_after=99, observed={lost: False})
    with pytest.raises(ceph_native.Refusal, match="^not-ready$") as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    assert not isinstance(caught.value, ceph_core.ReadinessRefusal)
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "unit"), ("node-1", "observe")]


def test_core_unit_readiness_timeout_names_the_missing_daemons(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = unit_request("direct")
    clock = iter(range(0, 100000, 300))
    transport = Transport(request, ready_after=99, blocking=["nfs@nfs.files"])
    with pytest.raises(ceph_core.ReadinessRefusal) as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    assert ceph_core.refusal(caught.value, ceph_core.CLUSTER_REASONS) == ("not-ready", "not-ready:nfs@nfs.files")


def test_core_evidence_admits_only_unit_daemons_on_their_document_hosts():
    request = unit_request("haproxy")
    row = {"host": "node-1", "name": "haproxy.nfs.files.node-1.abcdef", "service": "ingress.nfs.files"}
    value = dict(observation(request, True, [row]), request="d" * 64)
    assert ceph_core.evidence(copy.deepcopy(value), "d" * 64, request)["daemons"] == [row]
    for bad in (dict(row, host="node-3"), dict(row, service="nfs.other"), dict(row, name="Haproxy"), dict(row, secret=SECRET)):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(dict(value, daemons=[bad]), "d" * 64, request)
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(dict(value, daemons=[row, row]), "d" * 64, request)
