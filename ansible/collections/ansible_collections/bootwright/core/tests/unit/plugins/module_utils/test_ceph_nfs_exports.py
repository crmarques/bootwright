"""The NFS export worker: every refusal before any write, one declarative apply, one recorded identity, and read-only observation.

The scripted monitor answers the upstream v20.2.1 shapes the worker reads:
- `nfs export apply <cluster_id> -i -` upserts by pseudo path: an existing export keeps its ID and reads `updated`, a new one takes the
  lowest free ID from 1 and reads `added` (src/pybind/mgr/nfs/export.py:830-915, :320-328); the answer lists {pseudo, state[, msg]}
  (:191-192), an `error` state exits nonzero (:194-206) and a `warning` exits zero (:579-599);
- the NFS manager derives the CephFS principal nfs.<cluster_id>.<fs_name>.<sha1[:8]> (src/pybind/mgr/nfs/export.py:930-943);
- `nfs export info <cluster_id> <pseudo>` answers {} for an absent pseudo path (src/pybind/mgr/nfs/export.py:508-520), else Export.to_dict
  with sorted protocols and transports (src/pybind/mgr/nfs/ganesha_conf.py:473-489); an RGW FSAL prints access_key_id and
  secret_access_key (:314-322);
- `nfs cluster ls` lists the nfs service IDs (src/pybind/mgr/nfs/module.py:151-155), `mgr module ls` lists `enabled_modules` and
  `always_on_modules` (src/mon/MgrMonitor.cc:1016-1050), and `fs subvolume getpath` prints the path as text
  (src/pybind/mgr/volumes/fs/volume.py:484-498).
"""

from __future__ import annotations

import contextlib
import copy
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_nfs, ceph_tunnel_staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings

FSID = settings.FSID
OWNER = settings.OWNER
UUID = "0f1e2d3c-4b5a-4697-8a1b-2c3d4e5f6a7b"
ROOT = "/volumes/csi/docs/" + UUID
PRINCIPAL = "nfs.shares.files.0a1b2c3d"
CEPHX = "AQBhc2VjcmV0LWNlcGh4LWtleS12YWx1ZQ=="
ACCESS_KEY = "AKIASYNTHETICACCESSKEY"
SECRET_KEY = "c3ludGhldGljLXNlY3JldC1hY2Nlc3Mta2V5LXZhbHVl"
ATTEMPT = "nfs-export-attempt.docs.json"
IDENTITY = "nfs-export-id.docs.json"
APPLY = ("nfs", "export", "apply", "shares", "-i", "-")


def native():
    return {"access_type": "RW", "clients": [{"access_type": "RW", "addresses": ["198.51.100.0/24"], "squash": "root_squash"}],
            "fsal": {"cmount_path": "/", "fs_name": "files", "name": "CEPH"}, "protocols": [4], "pseudo": "/docs", "security_label": True,
            "squash": "root_squash", "transports": ["TCP"]}


def export_request(subvolume=True, **changes):
    request = dict(settings.cluster(), clusterSSHTransport="verifiedTunnel", action="nfs-export")
    request["delegated"] = [{"name": "nfs.shares", "hosts": ["node-1"]}]
    nfs = {"filesystem": "files", "group": "csi", "name": "docs", "native": dict(native(), **changes), "service": "shares", "subvolume": "docs",
           "subvolumePath": ROOT}
    if not subvolume:
        nfs.update(group="", subvolume="")
        del nfs["subvolumePath"]
        nfs["native"].setdefault("path", "/")
    request["nfsExport"] = nfs
    return request


def stored(value, identifier=1, principal=PRINCIPAL):
    """What the NFS manager stores and prints for an applied export JSON."""
    result = dict(copy.deepcopy(value), export_id=identifier, cluster_id="shares")
    result["protocols"], result["transports"] = sorted(result["protocols"]), sorted(result["transports"])
    result["fsal"] = dict(result["fsal"], user_id=principal)
    return result


def frozen(request, path=ROOT):
    return dict(request["nfsExport"]["native"], path=path)


def intent(request, path=ROOT):
    nfs = request["nfsExport"]
    return {"export": "docs", "fsid": FSID, "native": frozen(request, path), "owner": OWNER, "root": nfs.get("subvolumePath", ""), "service": "shares"}


class Monitor:
    """A scripted monitor: reads answer from its tables; `nfs export apply` upserts by pseudo path as the NFS manager does."""

    def __init__(self, request, modules=("nfs",), clusters=("shares",), exports=None, getpath=ROOT + "\n", state=None, failure=None):
        self.request = request
        self.modules = list(modules)
        self.clusters = list(clusters)
        self.exports = dict(exports or {})
        self.getpath = getpath
        self.state, self.failure = state, failure
        self.log, self.mutations, self.stdin = [], [], []

    def run(self, argv, data=b"", timeout=60):
        del timeout
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append(command)
        if command == ("fsid",):
            return 0, json.dumps({"fsid": FSID}).encode()
        if command == ("mgr", "module", "ls"):
            return 0, json.dumps({"always_on_modules": ["cephadm"], "enabled_modules": self.modules}).encode()
        if command == ("nfs", "cluster", "ls"):
            return 0, json.dumps(self.clusters).encode()
        if command[:3] == ("fs", "subvolume", "getpath"):
            return 0, self.getpath.encode()
        if command[:3] == ("nfs", "export", "info"):
            assert command[3] == "shares"
            return 0, json.dumps(self.exports.get(command[4], {})).encode()
        self.mutations.append(command)
        self.stdin.append(data)
        assert command[:3] == ("nfs", "export", "apply")
        if self.failure is not None:
            return 22, ceph_native.NativeOutput(b"", self.failure)
        value = json.loads(data)
        held = self.exports.get(value["pseudo"])
        used = {entry["export_id"] for entry in self.exports.values()}
        identifier = held["export_id"] if held else min(number for number in range(1, 100) if number not in used)
        self.exports[value["pseudo"]] = stored(value, identifier)
        answer = {"pseudo": value["pseudo"], "state": self.state or ("updated" if held else "added")}
        if answer["state"] == "warning":
            answer["msg"] = "changes applied (Manual restart of NFS Pods required)"
        return 0, json.dumps([answer]).encode()


@pytest.fixture(autouse=True)
def contained_holds(monkeypatch):
    """The custody and cache holds prove host files the unit tests do not stage; the worker's own decisions still run."""
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())


def scope(request):
    """The cluster block's bootstrap scope: the planner builds an export request from the cluster request, adding only its export."""
    return ceph_tunnel_staging.recovery_scope(dict({key: value for key, value in request.items() if key != "nfsExport"}, action="cluster"))


def markers_for(tmp_path, request):
    markers = ceph_native.Markers(request, root=str(tmp_path))
    os.makedirs(markers.path, mode=0o700)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", scope(request))
    return markers


def records(markers):
    return sorted(name for name in os.listdir(markers.path) if name not in ("owner.json", "bootstrap-scope.json"))


def worker(monitor, markers):
    native_worker = ceph_native_nfs.ExportWorker(monitor.request, "node-1", run=monitor.run, markers=markers)
    native_worker.pause = lambda seconds: None
    return native_worker


def refused(monitor, markers, reason):
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$") as caught:
        worker(monitor, markers).nfs_export()
    return caught.value


def observe(monitor, markers):
    native_worker = worker(monitor, markers)
    evidence = native_worker.empty_evidence()
    return native_worker.observe_resource(evidence, True), evidence


def test_export_refuses_existing_pseudo_without_marker(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request, exports={"/docs": stored(frozen(request))})
    error = refused(monitor, markers, "nfs-export-foreign")
    assert "export /docs on nfs.shares has no creation record of this operation" in error.native_error["cause"]
    assert monitor.mutations == [] and records(markers) == []


def test_export_captures_export_id_once_and_reuses(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL}
    assert markers.read(ATTEMPT) == intent(request)
    assert worker(monitor, markers).nfs_export() == {"changed": False}
    assert monitor.mutations == [APPLY]
    monitor.exports["/docs"] = stored(frozen(request), identifier=2)
    error = refused(monitor, markers, "nfs-export-foreign")
    assert "that this operation created is gone or changed" in error.native_error["cause"]
    assert monitor.mutations == [APPLY] and markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL}
    del monitor.exports["/docs"]
    refused(monitor, markers, "nfs-export-foreign")
    assert monitor.mutations == [APPLY]


def test_export_replay_after_apply_before_id_capture(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    markers.write(ATTEMPT, intent(request))
    monitor = Monitor(request, exports={"/docs": stored(frozen(request), identifier=3)})
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert monitor.mutations == [] and markers.read(IDENTITY) == {"exportId": 3, "userId": PRINCIPAL}
    other = markers_for(tmp_path / "other", request)
    other.write(ATTEMPT, intent(request))
    monitor = Monitor(request, exports={"/docs": stored(dict(frozen(request), access_type="RO"), identifier=3)})
    error = refused(monitor, other, "nfs-export-foreign")
    assert "is not the export this operation attempted" in error.native_error["cause"]
    assert monitor.mutations == [] and other.read(IDENTITY) is None


def test_export_replay_after_attempt_before_apply_applies(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    markers.write(ATTEMPT, intent(request))
    monitor = Monitor(request)
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert monitor.mutations == [APPLY] and markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL}


def test_export_observation_drops_rgw_secret(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    rgw = dict(stored(frozen(request), identifier=4), fsal={"name": "RGW", "user_id": "objects", "access_key_id": ACCESS_KEY, "secret_access_key": SECRET_KEY})
    monitor = Monitor(request, exports={"/docs": rgw})
    native_worker = worker(monitor, markers)
    evidence = native_worker.empty_evidence()
    with pytest.raises(ceph_native.Refusal, match="^nfs-export-foreign$") as caught:
        native_worker.observe_resource(evidence, True)
    with pytest.raises(ceph_native.Refusal, match="^nfs-export-foreign$") as applied:
        worker(monitor, markers).nfs_export()
    markers.write(ATTEMPT, intent(request))
    markers.write(IDENTITY, {"exportId": 1, "userId": PRINCIPAL})
    owned = stored(frozen(request))
    owned["fsal"]["cephx_key"] = CEPHX
    monitor.exports["/docs"] = owned
    ready, observed = observe(monitor, markers)
    assert ready is True and observed["resource"] == {"id": "1", "path": "/docs"}
    assert worker(monitor, markers).nfs_export() == {"changed": False}
    published = json.dumps([evidence, observed, str(caught.value), caught.value.native_error, str(applied.value), applied.value.native_error])
    on_disk = b"".join(open(os.path.join(markers.path, name), "rb").read() for name in os.listdir(markers.path))
    for secret in (ACCESS_KEY, SECRET_KEY, CEPHX):
        assert secret not in published and secret.encode() not in on_disk


def test_export_info_drops_secrets_before_reading_them(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    seen = []
    native_worker = worker(Monitor(request, exports={"/docs": dict(stored(frozen(request)), fsal=dict(stored(frozen(request))["fsal"], cephx_key=CEPHX))}),
                           markers)
    original = native_worker.manager_retry
    native_worker.manager_retry = lambda *arguments, **options: seen.append(original(*arguments, **options)) or seen[-1]
    info = native_worker.info()
    assert "cephx_key" not in info["fsal"] and seen and "cephx_key" in seen[0]["fsal"]


def test_subvolume_path_frozen_and_escape_refused(tmp_path):
    below = export_request(path=ROOT + "/shared")
    markers = markers_for(tmp_path, below)
    monitor = Monitor(below)
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert json.loads(monitor.stdin[0])["path"] == ROOT + "/shared"
    for case, change in (("outside", {"path": "/volumes/csi/other/" + UUID}), ("sibling prefix", {"path": ROOT + "-other"}),
                         ("cmount not an ancestor", {"fsal": {"cmount_path": "/volumes/other", "fs_name": "files", "name": "CEPH"}}),
                         ("cmount a prefix only", {"fsal": {"cmount_path": ROOT[:-1], "fs_name": "files", "name": "CEPH"}})):
        request = export_request(**change)
        target = markers_for(tmp_path / case.replace(" ", "-"), request)
        monitor = Monitor(request)
        refused(monitor, target, "nfs-export-path-escape")
        assert monitor.mutations == [] and records(target) == [], case
    moved = "/volumes/csi/docs/" + UUID.replace("0f1e", "1f1e")
    request = export_request()
    held = markers_for(tmp_path / "moved", request)
    held.write(ATTEMPT, intent(request))
    request["nfsExport"]["subvolumePath"] = moved
    monitor = Monitor(request, getpath=moved + "\n")
    refused(monitor, held, "subvolume-identity")
    assert monitor.mutations == [] and held.read(IDENTITY) is None
    request = export_request()
    target = markers_for(tmp_path / "getpath", request)
    monitor = Monitor(request, getpath=moved)
    refused(monitor, target, "subvolume-identity")
    assert monitor.mutations == [] and records(target) == []
    assert ("fs", "subvolume", "getpath", "files", "docs", "--group_name", "csi") in monitor.log


def test_export_without_a_group_never_names_the_default_group(tmp_path):
    request = export_request()
    request["nfsExport"].update(group="", subvolumePath="/volumes/_nogroup/docs/" + UUID)
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request, getpath="/volumes/_nogroup/docs/" + UUID)
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert ("fs", "subvolume", "getpath", "files", "docs") in monitor.log and not any("_nogroup" in part for part in sum(monitor.log, ()))


@pytest.mark.parametrize("answer", ["warning", "updated", "exit"])
def test_export_apply_warning_state_refuses(tmp_path, answer):
    request = export_request()
    markers = markers_for(tmp_path, request)
    failure = b"Error EINVAL: Failed to apply export: path /volumes/csi/docs does not exist" if answer == "exit" else None
    monitor = Monitor(request, state=None if answer == "exit" else answer, failure=failure)
    error = refused(monitor, markers, "nfs-export-apply")
    assert monitor.mutations == [APPLY] and markers.read(IDENTITY) is None
    if answer == "exit":
        assert "does not exist" in error.native_error["cause"]
    else:
        assert '"state":"' + answer + '"' in error.native_error["cause"]


def test_export_readback_that_differs_refuses(tmp_path, monkeypatch):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    applying = monitor.run

    def drifting(argv, data=b"", timeout=60):
        result = applying(argv, data=data, timeout=timeout)
        if "apply" in argv:
            monitor.exports["/docs"]["squash"] = "no_root_squash"
        return result

    monkeypatch.setattr(monitor, "run", drifting)
    error = refused(monitor, markers, "nfs-export-apply")
    assert "reads back other than applied" in error.native_error["cause"] and markers.read(IDENTITY) is None


def test_export_checks_module_then_service_before_any_write(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request, modules=())
    refused(monitor, markers, "nfs-module-disabled")
    assert ("nfs", "cluster", "ls") not in monitor.log and monitor.mutations == [] and records(markers) == []
    monitor = Monitor(request, clusters=("other",))
    error = refused(monitor, markers, "nfs-export-apply")
    assert error.native_error["cause"] == "nfs.shares is not listed by ceph nfs cluster ls"
    assert not any(command[:3] == ("nfs", "export", "info") for command in monitor.log) and monitor.mutations == [] and records(markers) == []


def test_export_apply_sends_the_frozen_json_on_stdin_only(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).nfs_export()
    assert monitor.mutations == [APPLY]
    assert monitor.stdin == [ceph_native.canonical(frozen(request))]
    sent = json.loads(monitor.stdin[0])
    assert "export_id" not in sent and "cluster_id" not in sent and "user_id" not in sent["fsal"]
    assert not any(part in ("/docs", ROOT) for part in monitor.mutations[0])
    filesystem = export_request(subvolume=False)
    markers = markers_for(tmp_path / "filesystem", filesystem)
    monitor = Monitor(filesystem)
    worker(monitor, markers).nfs_export()
    assert json.loads(monitor.stdin[0])["path"] == "/" and not any(command[:2] == ("fs", "subvolume") for command in monitor.log)


def test_drifted_owned_export_is_not_ready_then_reapplied(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).nfs_export()
    monitor.exports["/docs"]["access_type"] = "RO"
    before = records(markers)
    ready, evidence = observe(monitor, markers)
    assert ready is False and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "1", "path": "/docs"}
    assert records(markers) == before and monitor.mutations == [APPLY]
    assert worker(monitor, markers).nfs_export() == {"changed": True}
    assert monitor.mutations == [APPLY, APPLY] and monitor.stdin[1] == ceph_native.canonical(frozen(request))
    assert markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL} and monitor.exports["/docs"]["access_type"] == "RW"
    assert observe(monitor, markers)[0] is True


@pytest.mark.parametrize("held", [{"identifier": 2}, {"principal": "nfs.shares.files.9f8e7d6c"}])
def test_observation_refuses_a_recorded_export_of_another_identity(tmp_path, held):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).nfs_export()
    before = records(markers)
    monitor.exports["/docs"] = stored(frozen(request), **held)
    with pytest.raises(ceph_native.Refusal, match="^nfs-export-foreign$") as caught:
        observe(monitor, markers)
    assert "that this operation created is gone or changed" in caught.value.native_error["cause"]
    assert records(markers) == before and markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL} and monitor.mutations == [APPLY]


@pytest.mark.parametrize("held", [{"identifier": 2}, {"principal": "nfs.shares.files.9f8e7d6c"}])
def test_reapply_that_reads_back_another_identity_refuses(tmp_path, monkeypatch, held):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    worker(monitor, markers).nfs_export()
    monitor.exports["/docs"]["access_type"] = "RO"
    applying = monitor.run

    def replacing(argv, data=b"", timeout=60):
        result = applying(argv, data=data, timeout=timeout)
        if "apply" in argv:
            monitor.exports["/docs"] = stored(json.loads(data), **held)
        return result

    monkeypatch.setattr(monitor, "run", replacing)
    error = refused(monitor, markers, "nfs-export-apply")
    assert "reads back other than applied" in error.native_error["cause"]
    assert monitor.mutations == [APPLY, APPLY] and markers.read(IDENTITY) == {"exportId": 1, "userId": PRINCIPAL}


def test_export_observe_reads_only(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    ready, evidence = observe(Monitor(request), markers)
    assert ready is False and not evidence["owned"] and not evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    markers.write(ATTEMPT, intent(request))
    monitor = Monitor(request, exports={"/docs": stored(frozen(request))})
    ready, evidence = observe(monitor, markers)
    assert ready is False and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    assert monitor.mutations == [] and records(markers) == [ATTEMPT]
    assert all(command[:2] in (("mgr", "module"), ("nfs", "export")) and command[2] in ("ls", "info") for command in monitor.log)


def test_export_unfinished_reports_its_own_attempt_record(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    monitor = Monitor(request)
    native_worker = worker(monitor, markers)
    assert native_worker.unfinished(native_worker.empty_evidence())["owned"] is False
    markers.write(ATTEMPT, intent(request))
    evidence = native_worker.unfinished(native_worker.empty_evidence())
    assert evidence["owned"] and evidence["exists"] and evidence["partial"] and monitor.log == []


def test_export_scope_is_the_clusters_bootstrap_scope(tmp_path):
    request = export_request()
    markers = markers_for(tmp_path, request)
    assert worker(Monitor(request), markers).recovery_scope() == scope(request)
    assert "nfsExport" not in scope(request)["cluster"]


def test_export_records_of_another_shape_refuse_ownership(tmp_path):
    request = export_request()
    for case, attempt, identity in (("other owner", dict(intent(request), owner="apply-999"), None),
                                    ("identity without attempt", None, {"exportId": 1, "userId": PRINCIPAL}),
                                    ("identity of another shape", intent(request), {"exportId": "1", "userId": PRINCIPAL})):
        markers = markers_for(tmp_path / case.replace(" ", "-"), request)
        if attempt is not None:
            markers.write(ATTEMPT, attempt)
        if identity is not None:
            markers.write(IDENTITY, identity)
        monitor = Monitor(request)
        refused(monitor, markers, "ownership")
        assert monitor.mutations == [], case


def changed(request, path, value):
    *parents, last = path
    target = request
    for key in parents:
        target = target[key]
    target[last] = value
    return request


def removed(request, path):
    *parents, last = path
    target = request
    for key in parents:
        target = target[key]
    del target[last]
    return request


INVALID = {
    "unknown member": lambda: changed(export_request(), ("nfsExport", "extra"), 1),
    "export_id in native": lambda: changed(export_request(), ("nfsExport", "native", "export_id"), 1),
    "cluster_id in native": lambda: changed(export_request(), ("nfsExport", "native", "cluster_id"), "shares"),
    "user_id in fsal": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "user_id"), PRINCIPAL),
    "protocols 3 and 4": lambda: changed(export_request(), ("nfsExport", "native", "protocols"), [3, 4]),
    "protocols as a float": lambda: changed(export_request(), ("nfsExport", "native", "protocols"), [4.0]),
    "transports UDP": lambda: changed(export_request(), ("nfsExport", "native", "transports"), ["UDP"]),
    "sectype krb5": lambda: changed(export_request(), ("nfsExport", "native", "sectype"), ["krb5"]),
    "subvolumePath on a filesystem binding": lambda: changed(export_request(subvolume=False), ("nfsExport", "subvolumePath"), ROOT),
    "filesystem binding without a path": lambda: removed(export_request(subvolume=False), ("nfsExport", "native", "path")),
    "subvolume binding without its path": lambda: removed(export_request(), ("nfsExport", "subvolumePath")),
    "subvolume path of another subvolume": lambda: changed(export_request(), ("nfsExport", "subvolumePath"), "/volumes/csi/other/" + UUID),
    "subvolume path of another group": lambda: changed(export_request(), ("nfsExport", "subvolumePath"), "/volumes/_nogroup/docs/" + UUID),
    "malformed subvolume path": lambda: changed(export_request(), ("nfsExport", "subvolumePath"), "/volumes/csi/docs/not-a-uuid"),
    "another fs_name": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "fs_name"), "other"),
    "RGW fsal": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "name"), "RGW"),
    "cmount not clean": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "cmount_path"), "/volumes/../x"),
    "pseudo root": lambda: changed(export_request(), ("nfsExport", "native", "pseudo"), "/"),
    "pseudo with two leading slashes": lambda: changed(export_request(), ("nfsExport", "native", "pseudo"), "//docs"),
    "unknown squash": lambda: changed(export_request(), ("nfsExport", "native", "squash"), "squash_everything"),
    "lower-case access": lambda: changed(export_request(), ("nfsExport", "native", "access_type"), "rw"),
    "security label as a string": lambda: changed(export_request(), ("nfsExport", "native", "security_label"), "true"),
    "client with an extra member": lambda: changed(export_request(), ("nfsExport", "native", "clients", 0, "extra"), 1),
    "client without addresses": lambda: changed(export_request(), ("nfsExport", "native", "clients", 0, "addresses"), []),
    "client network with host bits": lambda: changed(export_request(), ("nfsExport", "native", "clients", 0, "addresses"), ["198.51.100.5/24"]),
    "client name": lambda: changed(export_request(), ("nfsExport", "native", "clients", 0, "addresses"), ["client.example.test"]),
    "client repeated": lambda: changed(export_request(), ("nfsExport", "native", "clients"), native()["clients"] * 2),
    "group without a subvolume": lambda: changed(changed(removed(export_request(), ("nfsExport", "subvolumePath")), ("nfsExport", "subvolume"), ""),
                                                 ("nfsExport", "native", "path"), "/"),
    "service not delegated": lambda: changed(export_request(), ("delegated",), [{"name": "nfs.other", "hosts": ["node-1"]}]),
    "export on a service action": lambda: dict(export_request(), action="service"),
    "export on a cluster action": lambda: dict(export_request(), action="cluster"),
    "pool member on an export action": lambda: dict(export_request(), pool={"name": "volumes"}),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_export_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


@pytest.mark.parametrize("subvolume", [True, False])
def test_export_request_admits_each_binding(subvolume):
    request = export_request(subvolume=subvolume, sectype=["sys"], clients=[])
    assert ceph_native.validate(request) is request


def test_dispatch_routes_an_export_to_the_export_worker(monkeypatch):
    reached = []
    monkeypatch.setattr(ceph_native_nfs.ExportWorker, "nfs_export", lambda self: reached.append(type(self)) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = export_request()
    assert ceph_native.dispatch({"request": request, "host": "node-1", "phase": "nfs_export", "private": {}}) == {"changed": False}
    assert reached == [ceph_native_nfs.ExportWorker]
    for phase in ("unit", "pool", "gateway_service"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": {}})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: False)
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": request, "host": "node-1", "phase": "nfs_export", "private": {}})


def observation(ready):
    return {"version": "ceph-core-evidence-v13", "request": "", "action": "nfs-export", "owner": OWNER, "fsid": FSID, "owned": True, "exists": True,
            "ready": ready, "removed": False, "partial": not ready, "health": "HEALTH_OK", "hosts": ["node-1", "node-2", "node-3"], "monitors": [],
            "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": {"id": "1", "path": "/docs"} if ready else {"id": "", "path": ""},
            "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False}, "endpoint": {"certificateSHA256": "", "reachable": False},
            "daemons": [], "sso": "", "incarnation": "2026-01-01T00:00:00.000000Z"}


class Transport:
    def __init__(self, ready_after=1):
        self.calls, self.ready_after = [], ready_after

    def call(self, host, phase, timeout=None):
        del timeout
        self.calls.append((host["name"], phase))
        if phase == "observe":
            return dict(observation(sum(call[1] == "observe" for call in self.calls) >= self.ready_after), blocking=[])
        if phase == "prerequisites":
            return {"name": host["name"], "owned": True, "exists": True}
        return {"changed": phase == "nfs_export"}


def test_export_routes_through_the_unit_flow(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = export_request()
    transport = Transport(ready_after=2)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["changed"] is True and result["evidence"]["resource"] == {"id": "1", "path": "/docs"}
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "nfs_export"), ("node-1", "observe"), ("node-1", "observe")]
    transport = Transport()
    assert ceph_core.execute(request, "observe", "d" * 64, transport)["changed"] is False and transport.calls == [("node-1", "observe")]
