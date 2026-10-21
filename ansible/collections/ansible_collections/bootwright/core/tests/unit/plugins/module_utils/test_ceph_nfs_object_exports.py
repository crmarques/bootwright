"""The NFS export of one object user: its gateway's realm and zone made the defaults the NFS manager resolves users through, one declarative
apply, the user's key pair only in the worker's memory and the apply's stdin, and read-only observation.

The scripted cluster extends the object user's and answers the upstream v20.2.1 shapes the worker reads:
- `realm list` and `zone list` print `default_info`, the default realm's ID and that realm's default zone's ID, empty when unset
  (src/rgw/radosgw-admin/radosgw-admin.cc:5297-5325, :6447-6475, src/rgw/driver/rados/rgw_zone.cc:981-1004); `realm default` and
  `zone default` set them (:5444-5458, :6320-6339), the zone's pointer kept per realm (rgw_zone.cc:1063-1069);
- `nfs export apply` of an RGW export reads the user's first key through the default realm on add (src/pybind/mgr/nfs/export.py:263-288,
  :1015-1016) and refuses an update whose keys differ from the stored ones (:1037-1045); `nfs export info` prints RGWFSAL.to_dict with the
  keys (src/pybind/mgr/nfs/ganesha_conf.py:301-309).
"""

from __future__ import annotations

import copy
import json
import os
import uuid

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_nfs, ceph_native_object
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_users as users

native_boundaries = gateway.native_boundaries
root = users.root
ACCESS, SECRET = users.ACCESS, users.SECRET
ATTEMPT = "nfs-export-attempt.buckets.json"
IDENTITY = "nfs-export-id.buckets.json"
APPLY = ("nfs", "export", "apply", "shares", "-i", "-")
EXPORT_USER = {"gateway": "objects", "name": "app-writer", "realm": "objects", "zone": "objects", "zoneGroup": "objects"}


def native():
    return {"access_type": "RW", "clients": [{"access_type": "RW", "addresses": ["198.51.100.0/24"], "squash": "root_squash"}],
            "fsal": {"name": "RGW", "user_id": "app-writer"}, "path": "/", "protocols": [4], "pseudo": "/buckets", "security_label": True,
            "squash": "root_squash", "transports": ["TCP"]}


def export_request(**changes):
    request = {key: value for key, value in users.user_request().items() if key != "objectUser"}
    request.update(action="nfs-export", delegated=[{"name": "nfs.shares", "hosts": ["node-1"]}] + request["delegated"])
    request["nfsExport"] = {"filesystem": "", "group": "", "name": "buckets", "native": dict(native(), **changes), "objectUser": dict(EXPORT_USER),
                            "service": "shares", "subvolume": ""}
    return request


def intent(request):
    return {"export": "buckets", "fsid": gateway.FSID, "native": request["nfsExport"]["native"], "owner": gateway.OWNER, "root": "", "service": "shares"}


class Cluster(users.Cluster):
    """The object user's scripted cluster with default realm and zone pointers and the NFS manager's RGW export handling."""

    def __init__(self, request):
        super().__init__(request)
        self.default_realm, self.default_zones = "", {}
        self.exports, self.stdin, self.update_error = {}, [], None
        self.list_stderr = {}

    def run(self, argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:])
        if os.path.basename(argv[0]) != "ceph" or command[:1] not in (("nfs",), ("mgr",)):
            return super().run(argv, data=data, timeout=timeout)
        self.log.append(("ceph", command, data))
        if command == ("mgr", "module", "ls"):
            return 0, json.dumps({"always_on_modules": ["cephadm"], "enabled_modules": ["nfs"]}).encode()
        if command == ("nfs", "cluster", "ls"):
            return 0, json.dumps(["shares"]).encode()
        if command[:3] == ("nfs", "export", "info"):
            return 0, json.dumps(self.exports.get(command[4], {})).encode()
        assert command == APPLY
        self.mutations.append(("ceph", command))
        self.stdin.append(data)
        return self.apply(json.loads(data))

    def resolvable(self, uid):
        """The manager's `radosgw-admin user info` runs without a realm: it finds only users of the default realm's default zone."""
        realm = self.realms["objects"]["id"]
        return self.default_realm == realm and self.default_zones.get(realm) == self.zones["objects"]["id"] and uid in self.users

    def apply(self, value):
        fsal, held = value["fsal"], self.exports.get(value["pseudo"])
        assert fsal["name"] == "RGW" and value["path"] == "/"
        if held is None:
            if not self.resolvable(fsal["user_id"]):
                return 22, ceph_native.NativeOutput(b"", b"Error EIO: Failed to fetch key for bucket / owner " + fsal["user_id"].encode())
            key = self.users[fsal["user_id"]]["keys"][0]
            keys, identifier, state = (key["access_key"], key["secret_key"]), 1 + len(self.exports), "added"
        else:
            keys, identifier, state = (fsal.get("access_key_id"), fsal.get("secret_access_key")), held["export_id"], "updated"
            if keys != (held["fsal"]["access_key_id"], held["fsal"]["secret_access_key"]):
                return 22, ceph_native.NativeOutput(b"", b"Error EINVAL: access_key_id change is not allowed")
            if self.update_error is not None:
                return 22, ceph_native.NativeOutput(b"", self.update_error)
        stored = dict(copy.deepcopy(value), export_id=identifier, cluster_id="shares")
        stored["fsal"] = {"name": "RGW", "user_id": fsal["user_id"], "access_key_id": keys[0], "secret_access_key": keys[1]}
        self.exports[value["pseudo"]] = stored
        return 0, json.dumps([{"pseudo": value["pseudo"], "state": state}]).encode()

    def radosgw(self, command):
        found, values, unused = gateway.words(command)
        if found == ("realm", "list"):
            body = json.dumps({"default_info": self.default_realm, "realms": sorted(self.realms)}).encode()
            return 0, ceph_native.NativeOutput(body, self.list_stderr.get("realm", b""))
        if found == ("zone", "list"):
            body = json.dumps({"default_info": self.default_zones.get(self.default_realm, ""), "zones": sorted(self.zones)}).encode()
            return 0, ceph_native.NativeOutput(body, self.list_stderr.get("zone", b""))
        if found == ("realm", "default"):
            self.mutations.append(("radosgw-admin", command))
            self.default_realm = self.realms[values["--rgw-realm"]]["id"]
            return 0, b""
        if found == ("zone", "default"):
            self.mutations.append(("radosgw-admin", command))
            zone = self.zones[values["--rgw-zone"]]
            self.default_zones[zone["realm_id"]] = zone["id"]
            return 0, b""
        return super().radosgw(command)


@pytest.fixture
def ready(root):
    """The gateway's zone, the object user this operation created, and the export's request on the same records."""
    del root
    request = gateway.gateway_request()
    markers = users.Markers(request)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", gateway.scope(request))
    cluster = Cluster(request)
    for phase in ("gateway_zone", "gateway_placement"):
        getattr(gateway.worker(cluster, markers), phase)()
    cluster.request = users.user_request()
    users.user_worker(cluster, markers).object_user()
    cluster.request = export_request()
    cluster.mutations.clear()
    cluster.log.clear()
    return cluster, markers


def export_worker(cluster, markers):
    native = ceph_native_object.ExportUserWorker(cluster.request, "node-1", run=cluster.run, markers=markers)
    native.pause = lambda seconds: None
    return native


def observe(cluster, markers):
    native = export_worker(cluster, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def ids(cluster):
    return cluster.realms["objects"]["id"], cluster.zones["objects"]["id"]


def defaults(cluster):
    return [command[:2] for tool, command in cluster.mutations if tool == "radosgw-admin"]


def secrets_absent(cluster, markers, *published):
    text = json.dumps([markers.records, list(published)], default=str)
    argv = json.dumps([command for unused, command, unused_data in cluster.log])
    for secret in (ACCESS, SECRET):
        assert secret not in text and secret not in argv


def test_an_export_sets_the_zone_then_the_realm_default_and_applies_without_keys(ready):
    cluster, markers = ready
    assert export_worker(cluster, markers).nfs_export() == {"changed": True}
    assert defaults(cluster) == [("zone", "default"), ("realm", "default")]
    zone_default = next(command for tool, command in cluster.mutations if command[:2] == ("zone", "default"))
    assert gateway.words(zone_default)[1] == {"--rgw-realm": "objects", "--rgw-zone": "objects"}
    assert cluster.mutations[-1] == ("ceph", APPLY) and cluster.stdin == [ceph_native.canonical(native())]
    assert (cluster.default_realm, cluster.default_zones[cluster.default_realm]) == ids(cluster)
    assert markers.read(ATTEMPT) == intent(cluster.request) and markers.read(IDENTITY) == {"exportId": 1, "userId": "app-writer"}
    cluster.mutations.clear()
    assert export_worker(cluster, markers).nfs_export() == {"changed": False}
    assert cluster.mutations == []
    secrets_absent(cluster, markers)


def test_a_default_realm_of_another_realm_refuses_before_any_write(ready):
    cluster, markers = ready
    cluster.default_realm = str(uuid.uuid4())
    before = dict(markers.records)
    with pytest.raises(ceph_native.NativeFailure, match="^nfs-export-realm-default$") as caught:
        export_worker(cluster, markers).nfs_export()
    assert caught.value.native_error["cause"] == ("the default realm has ID " + cluster.default_realm
                                                  + ", not realm objects of StorageObjectGateway objects")
    assert cluster.mutations == [] and markers.records == before


@pytest.mark.parametrize("kind", ["realm", "zone"])
def test_a_default_read_that_wrote_to_stderr_refuses_before_any_write(ready, kind):
    """radosgw-admin reports a read error other than ENOENT on stderr, exits 0 and prints an empty default_info (radosgw-admin.cc:5299-5304)."""
    cluster, markers = ready
    realm, zone = ids(cluster)
    cluster.default_realm, cluster.default_zones[realm] = realm, zone
    cluster.list_stderr[kind] = b"failed to read " + kind.encode() + b" info: (5) Input/output error"
    before = dict(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        export_worker(cluster, markers).nfs_export()
    assert cluster.mutations == [] and markers.records == before
    stderr = cluster.list_stderr.pop(kind)
    assert export_worker(cluster, markers).nfs_export() == {"changed": True}
    cluster.list_stderr[kind] = stderr
    cluster.mutations.clear()
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        observe(cluster, markers)
    assert cluster.mutations == []


def test_with_its_realm_already_default_only_the_zone_pointer_is_set(ready):
    cluster, markers = ready
    realm, zone = ids(cluster)
    cluster.default_realm, cluster.default_zones[realm] = realm, str(uuid.uuid4())
    assert export_worker(cluster, markers).nfs_export() == {"changed": True}
    assert defaults(cluster) == [("zone", "default")] and cluster.default_zones[realm] == zone


def test_an_unproved_user_or_gateway_refuses_before_any_write(ready):
    cluster, markers = ready
    del markers.records["object-user-complete.app-writer.json"]
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        export_worker(cluster, markers).nfs_export()
    assert cluster.mutations == [] and ATTEMPT not in markers.records
    markers.records["object-user-complete.app-writer.json"] = {"owner": gateway.OWNER, "fsid": gateway.FSID, "gateway": "objects",
                                                               "user": "app-writer", "displayName": "App writer", "realm": "objects",
                                                               "zoneGroup": "objects", "zone": "objects"}
    cluster.users["app-writer"]["keys"].append({"user": "app-writer", "access_key": users.OTHER_ACCESS, "secret_key": users.OTHER_SECRET,
                                                "active": True})
    with pytest.raises(ceph_native.Refusal, match="^object-user-identity$"):
        export_worker(cluster, markers).nfs_export()
    assert cluster.mutations == [] and ATTEMPT not in markers.records


def test_an_update_carries_the_users_pair_on_stdin_only_and_redacts_it(ready):
    cluster, markers = ready
    export_worker(cluster, markers).nfs_export()
    cluster.exports["/buckets"]["access_type"] = "RO"
    proved, blocking, evidence = observe(cluster, markers)
    assert not proved and blocking == [] and evidence["resource"] == {"id": "1", "path": "/buckets"}
    assert export_worker(cluster, markers).nfs_export() == {"changed": True}
    sent = json.loads(cluster.stdin[-1])
    assert sent["fsal"] == {"name": "RGW", "user_id": "app-writer", "access_key_id": ACCESS, "secret_access_key": SECRET}
    assert {key: value for key, value in sent.items() if key != "fsal"} == {key: value for key, value in native().items() if key != "fsal"}
    assert cluster.exports["/buckets"]["access_type"] == "RW" and observe(cluster, markers)[0]
    cluster.exports["/buckets"]["access_type"] = "RO"
    cluster.update_error = b"Error EINVAL: rejected " + ACCESS.encode() + b" " + SECRET.encode()
    with pytest.raises(ceph_native.Refusal, match="^nfs-export-apply$") as caught:
        export_worker(cluster, markers).nfs_export()
    secrets_absent(cluster, markers, caught.value.native_error, evidence)


def test_an_export_holding_another_key_than_its_users_refuses(ready):
    cluster, markers = ready
    export_worker(cluster, markers).nfs_export()
    cluster.exports["/buckets"]["fsal"]["secret_access_key"] = users.OTHER_SECRET
    cluster.mutations.clear()
    with pytest.raises(ceph_native.NativeFailure, match="^nfs-export-foreign$") as caught:
        export_worker(cluster, markers).nfs_export()
    assert caught.value.native_error["cause"] == "export /buckets on nfs.shares that this operation created holds another S3 key than user app-writer"
    with pytest.raises(ceph_native.NativeFailure, match="^nfs-export-foreign$"):
        observe(cluster, markers)
    assert cluster.mutations == []
    secrets_absent(cluster, markers, caught.value.native_error)


def test_observation_proves_the_defaults_and_reads_only(ready):
    cluster, markers = ready
    proved, unused, evidence = observe(cluster, markers)
    assert not proved and evidence["resource"] == {"id": "", "path": ""}
    export_worker(cluster, markers).nfs_export()
    cluster.mutations.clear()
    proved, blocking, evidence = observe(cluster, markers)
    assert proved and blocking == [] and evidence["resource"] == {"id": "1", "path": "/buckets"}
    cluster.default_realm = ""
    proved, blocking, unused = observe(cluster, markers)
    assert not proved and blocking == ["default@realm.objects"]
    realm = ids(cluster)[0]
    cluster.default_realm, cluster.default_zones[realm] = realm, ""
    assert observe(cluster, markers)[1] == ["default@realm.objects"]
    cluster.default_realm = str(uuid.uuid4())
    with pytest.raises(ceph_native.NativeFailure, match="^nfs-export-realm-default$"):
        observe(cluster, markers)
    assert cluster.mutations == []
    assert all(ceph_native_object.PENDING.fullmatch(entry) for entry in ["default@realm.objects"])
    secrets_absent(cluster, markers, evidence)


def test_a_lost_reply_is_adopted_under_the_attempt_record(ready):
    cluster, markers = ready
    markers.write(ATTEMPT, intent(cluster.request))
    realm, zone = ids(cluster)
    cluster.default_realm, cluster.default_zones[realm] = realm, zone
    cluster.exports["/buckets"] = dict(native(), export_id=7, cluster_id="shares",
                                       fsal={"name": "RGW", "user_id": "app-writer", "access_key_id": ACCESS, "secret_access_key": SECRET})
    assert export_worker(cluster, markers).nfs_export() == {"changed": True}
    assert cluster.mutations == [] and markers.read(IDENTITY) == {"exportId": 7, "userId": "app-writer"}


def test_an_export_of_another_user_at_the_pseudo_path_is_foreign(ready):
    cluster, markers = ready
    markers.write(ATTEMPT, intent(cluster.request))
    cluster.exports["/buckets"] = dict(native(), export_id=7, cluster_id="shares",
                                       fsal={"name": "RGW", "user_id": "other", "access_key_id": ACCESS, "secret_access_key": SECRET})
    with pytest.raises(ceph_native.NativeFailure, match="^nfs-export-foreign$"):
        export_worker(cluster, markers).nfs_export()
    assert cluster.mutations == []


def changed(request, path, value):
    *parents, last = path
    target = request
    for key in parents:
        target = target[key]
    target[last] = value
    return request


INVALID = {
    "another user_id": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "user_id"), "other"),
    "a bucket path": lambda: changed(export_request(), ("nfsExport", "native", "path"), "/app-data"),
    "a CephFS member": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "cmount_path"), "/"),
    "a CEPH fsal": lambda: changed(export_request(), ("nfsExport", "native", "fsal"), {"cmount_path": "/", "fs_name": "files", "name": "CEPH"}),
    "a key in the request": lambda: changed(export_request(), ("nfsExport", "native", "fsal", "access_key_id"), ACCESS),
    "a filesystem": lambda: changed(export_request(), ("nfsExport", "filesystem"), "files"),
    "a subvolume path": lambda: changed(export_request(), ("nfsExport", "subvolumePath"), "/volumes/csi/docs/" + users.gateway.FSID),
    "a user member missing": lambda: changed(export_request(), ("nfsExport", "objectUser"), {"gateway": "objects", "name": "app-writer"}),
    "the reserved user": lambda: changed(changed(export_request(), ("nfsExport", "objectUser", "name"), "dashboard"),
                                         ("nfsExport", "native", "fsal", "user_id"), "dashboard"),
    "a realm that is no label": lambda: changed(export_request(), ("nfsExport", "objectUser", "realm"), "-objects"),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_object_export_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


def test_object_export_request_admits_and_routes_to_the_object_export_worker(monkeypatch):
    request = export_request(sectype=["sys"], clients=[])
    assert ceph_native.validate(request) is request
    reached = []
    monkeypatch.setattr(ceph_native_nfs.ExportWorker, "nfs_export", lambda self: reached.append(type(self)) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    assert ceph_native.dispatch({"request": export_request(), "host": "node-1", "phase": "nfs_export", "private": {}}) == {"changed": False}
    assert reached == [ceph_native_object.ExportUserWorker]
