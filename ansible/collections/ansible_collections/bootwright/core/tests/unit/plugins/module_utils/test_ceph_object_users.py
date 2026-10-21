"""The object user worker: one RGW user created once in its gateway's zone, its generated S3 key pair retained in custody before any
result and never rotated, and read-only observation.

The scripted cluster answers the upstream v20.2.1 shapes the worker reads:
- `user create` without key arguments generates an S3 key pair (src/rgw/radosgw-admin/radosgw-admin.cc:6872-6875) of 20 characters of 0-9A-Z
  and 40 of A-Za-z0-9 (src/rgw/driver/rados/rgw_user.h:22-23, src/common/random_string.cc), then prints the user (:7445-7452, :1216-1221);
- `user info` prints RGWUserInfo::dump (src/rgw/rgw_common.cc:2837-2897), each S3 key as {user, access_key, secret_key, active,
  create_date} (:2682-2686, :3036-3050);
- `user list` prints a JSON array of user IDs (src/rgw/radosgw-admin/radosgw-admin.cc:9740-9797).
"""

from __future__ import annotations

import copy
import json
import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_object, ceph_tunnel_staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateway

ACCESS = "Q7ZK2M9X4B1R8T5W3N6P"
SECRET = "u8Rj2Lq5Zx1Vb7Nc4Hk9Tg3Ws6Fd0Ym2Pa8Ee5Oi"
OTHER_ACCESS = "A1B2C3D4E5F6G7H8J9K0"
OTHER_SECRET = "zZ9yY8xX7wW6vV5uU4tT3sS2rR1qQ0pP9oO8nN7m"
native_boundaries = gateway.native_boundaries
USER = {"displayName": "App writer", "gateway": "objects", "name": "app-writer", "realm": "objects", "zone": "objects", "zoneGroup": "objects"}


class Markers:
    """In-memory records under the fixed private FSID path that custody requires."""

    def __init__(self, request):
        self.path = os.path.join(ceph_native.ROOT, request["fsid"])
        self.records = {}

    def owned(self):
        return "owner.json" in self.records

    def read(self, name="owner.json"):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)


class Cluster(gateway.Monitor):
    """The gateway's scripted cluster that also answers `radosgw-admin user list`, `info` and `create`."""

    def __init__(self, request):
        super().__init__(request)
        self.users = {}
        self.pair = (ACCESS, SECRET)
        self.after_create = None
        self.create_failure = None

    def radosgw(self, command):
        found, values, flags = gateway.words(command)
        if found[:1] != ("user",):
            return super().radosgw(command)
        assert (values["--rgw-realm"], values["--rgw-zonegroup"], values["--rgw-zone"]) == ("objects", "objects", "objects") and not flags
        if found == ("user", "list"):
            return 0, json.dumps(sorted(self.users)).encode()
        if found == ("user", "info"):
            if values["--uid"] not in self.users:
                return 2, ceph_native.NativeOutput(b"", b"could not fetch user info: no user info saved\n")
            return 0, json.dumps(self.users[values["--uid"]]).encode()
        assert found == ("user", "create") and set(values) == {"--rgw-realm", "--rgw-zonegroup", "--rgw-zone", "--uid", "--display-name"}
        self.mutations.append(("radosgw-admin", command))
        if self.create_failure is not None:
            return self.create_failure
        uid = values["--uid"]
        self.users[uid] = info(uid, values["--display-name"], *self.pair)
        if self.after_create is not None:
            self.after_create()
        return 0, json.dumps(self.users[uid]).encode()


def info(uid, display, access, secret):
    return {"user_id": uid, "display_name": display, "email": "", "suspended": 0, "max_buckets": 1000, "subusers": [],
            "keys": [{"user": uid, "access_key": access, "secret_key": secret, "active": True, "create_date": "2026-10-21T00:00:00.000000Z"}],
            "swift_keys": [], "caps": [], "op_mask": "read, write, delete", "default_placement": "", "default_storage_class": "",
            "placement_tags": [], "bucket_quota": {}, "user_quota": {}, "temp_url_keys": [], "type": "rgw", "mfa_ids": [], "account_id": "",
            "path": "/", "create_date": "2026-10-21T00:00:00.000000Z", "tags": [], "group_ids": []}


def user_request(**members):
    base = gateway.gateway_request()
    request = {key: value for key, value in base.items() if key != "gateway"}
    request.update(action="object-user", objectUser=dict(USER, **members))
    return request


@pytest.fixture
def root(tmp_path, monkeypatch):
    """A private root-owned FSID area under a temporary `/`, as the client custody tests build it."""
    top = tmp_path / "root"
    top.mkdir(mode=0o755)
    directory = top
    for part in (ceph_native.ROOT + "/" + gateway.FSID).strip("/").split("/"):
        directory /= part
        directory.mkdir(mode=0o700 if part == gateway.FSID else 0o755)
    original_open, original_fstat = os.open, os.fstat

    def open_file(path, flags, mode=0o777, *, dir_fd=None):
        if path == "/" and dir_fd is None:
            path = str(top)
        return original_open(path, flags, mode, dir_fd=dir_fd)

    def fstat(fd):
        actual = original_fstat(fd)
        fields = {name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}
        fields.update(st_uid=0, st_gid=0)
        return SimpleNamespace(**fields)

    monkeypatch.setattr(ceph_native.os, "open", open_file)
    monkeypatch.setattr(ceph_native.os, "fstat", fstat)
    monkeypatch.setattr(ceph_native.os, "fchown", lambda fd, uid, gid: ceph_native.require(uid == gid == 0))
    return directory


@pytest.fixture
def ready(root):
    """A cluster whose gateway completed its zone and placement phases, with the user's request on the same records."""
    del root
    request = gateway.gateway_request()
    markers = Markers(request)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("bootstrap-scope.json", gateway.scope(request))
    cluster = Cluster(request)
    for phase in ("gateway_zone", "gateway_placement"):
        getattr(gateway.worker(cluster, markers), phase)()
    cluster.request = user_request()
    cluster.mutations.clear()
    cluster.log.clear()
    return cluster, markers


def user_worker(cluster, markers):
    native = ceph_native_object.UserWorker(cluster.request, "node-1", run=cluster.run, markers=markers)
    native.pause = lambda seconds: None
    return native


def material(access=ACCESS, secret=SECRET):
    return json.dumps({"accessKey": access, "secretKey": secret, "uid": "app-writer"}, sort_keys=True, separators=(",", ":"))


def held(root):
    return (root / "object-user-material-app-writer.json").read_bytes()


def creates(cluster):
    return [command for unused, command in cluster.mutations if command[:2] == ("user", "create")]


def test_user_is_created_once_without_any_key_argument_and_its_pair_retained(ready, root):
    cluster, markers = ready
    assert user_worker(cluster, markers).object_user() == {"changed": True, "private": material()}
    [create] = creates(cluster)
    assert create == ("user", "create", "--uid", "app-writer", "--display-name", "App writer",
                      "--rgw-realm", "objects", "--rgw-zonegroup", "objects", "--rgw-zone", "objects")
    assert held(root) == material().encode()
    intent = {"owner": gateway.OWNER, "fsid": gateway.FSID, "gateway": "objects", "user": "app-writer", "displayName": "App writer",
              "realm": "objects", "zoneGroup": "objects", "zone": "objects"}
    assert markers.read("object-user-intent.app-writer.json") == intent == markers.read("object-user-attempt.app-writer.json")
    assert markers.read("object-user-complete.app-writer.json") == intent
    assert user_worker(cluster, markers).object_user() == {"changed": False, "private": material()}
    assert len(creates(cluster)) == 1


def test_attempt_record_precedes_the_create_and_custody_follows_it(ready):
    cluster, markers = ready
    seen = []
    cluster.after_create = lambda: seen.append(sorted(markers.records))
    user_worker(cluster, markers).object_user()
    assert "object-user-attempt.app-writer.json" in seen[0]
    assert not any(name.startswith("object-user-reserved-") or name.startswith("object-user-id-") for name in seen[0])


def test_lost_reply_is_recovered_from_the_native_user_without_rotation(ready, root):
    cluster, markers = ready

    def crash():
        raise ceph_native.Refusal("transport")

    cluster.after_create = crash
    with pytest.raises(ceph_native.Refusal, match="^transport$"):
        user_worker(cluster, markers).object_user()
    cluster.after_create = None
    cluster.pair = (OTHER_ACCESS, OTHER_SECRET)
    assert user_worker(cluster, markers).object_user() == {"changed": False, "private": material()}
    assert len(creates(cluster)) == 1 and held(root) == material().encode()


def test_foreign_user_refuses_before_any_write(ready):
    cluster, markers = ready
    cluster.users["app-writer"] = info("app-writer", "App writer", ACCESS, SECRET)
    before = dict(markers.records)
    with pytest.raises(ceph_native.NativeFailure, match="^object-user-foreign$") as caught:
        user_worker(cluster, markers).object_user()
    assert caught.value.native_error["cause"] == "user app-writer in realm objects has no creation record of this operation"
    assert markers.records == before and creates(cluster) == []
    with pytest.raises(ceph_native.NativeFailure, match="^object-user-foreign$"):
        observe(cluster, markers)


def test_created_user_that_is_gone_refuses_and_is_never_recreated(ready):
    cluster, markers = ready
    user_worker(cluster, markers).object_user()
    del cluster.users["app-writer"]
    for phase in ("object_user", "object_user_read"):
        with pytest.raises(ceph_native.NativeFailure, match="^object-user-foreign$") as caught:
            getattr(user_worker(cluster, markers), phase)()
        assert caught.value.native_error["cause"] == "user app-writer in realm objects that this operation created is gone"
    with pytest.raises(ceph_native.NativeFailure, match="^object-user-foreign$"):
        observe(cluster, markers)
    assert len(creates(cluster)) == 1


@pytest.mark.parametrize("drift", [
    {"display_name": "Renamed"}, {"suspended": 1}, {"caps": [{"type": "users", "perm": "*"}]}, {"subusers": [{"id": "app-writer:swift"}]},
    {"swift_keys": [{"user": "app-writer:swift", "secret_key": "x"}]}, {"system": True}, {"admin": True}, {"account_id": "RGW00000000000000001"},
    {"keys": []}, {"keys": [{"user": "app-writer", "access_key": OTHER_ACCESS, "secret_key": OTHER_SECRET}]},
    {"keys": [{"user": "app-writer", "access_key": ACCESS, "secret_key": OTHER_SECRET}]},
    {"keys": [{"user": "app-writer", "access_key": ACCESS, "secret_key": SECRET},
              {"user": "app-writer", "access_key": OTHER_ACCESS, "secret_key": OTHER_SECRET}]},
])
def test_a_changed_user_refuses_and_its_keys_are_never_regenerated(ready, root, drift):
    cluster, markers = ready
    user_worker(cluster, markers).object_user()
    cluster.users["app-writer"].update(drift)
    for phase in ("object_user", "object_user_read"):
        with pytest.raises(ceph_native.Refusal, match="^object-user-identity$"):
            getattr(user_worker(cluster, markers), phase)()
    with pytest.raises(ceph_native.Refusal, match="^object-user-identity$"):
        observe(cluster, markers)
    assert len(creates(cluster)) == 1 and held(root) == material().encode()


def test_recapture_reads_the_retained_pair_and_writes_nothing(ready):
    cluster, markers = ready
    user_worker(cluster, markers).object_user()
    before, mutations = dict(markers.records), list(cluster.mutations)
    assert user_worker(cluster, markers).object_user_read() == {"private": material()}
    assert markers.records == before and cluster.mutations == mutations


def test_observation_is_read_only_and_names_only_the_user_id(ready):
    cluster, markers = ready
    proved, unused, evidence = observe(cluster, markers)
    assert not proved and evidence["resource"] == {"id": "", "path": ""} and not evidence["owned"] and not evidence["exists"]
    user_worker(cluster, markers).object_user()
    before, mutations = dict(markers.records), list(cluster.mutations)
    proved, unused, evidence = observe(cluster, markers)
    assert proved and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "app-writer", "path": ""}
    assert markers.records == before and cluster.mutations == mutations
    assert ACCESS not in json.dumps(evidence) and SECRET not in json.dumps(evidence)


def test_no_key_reaches_a_record_an_argument_or_a_native_error(ready):
    cluster, markers = ready
    user_worker(cluster, markers).object_user()
    user_worker(cluster, markers).object_user_read()
    observe(cluster, markers)
    for key in (ACCESS, SECRET):
        assert key not in json.dumps(markers.records)
        assert all(key not in " ".join(command) for unused, command, unused_data in cluster.log)
    cluster.request = user_request(name="second", displayName="Second")
    cluster.create_failure = (22, ceph_native.NativeOutput(json.dumps(info("second", "Second", ACCESS, SECRET)).encode(), b""))
    with pytest.raises(ceph_native.NativeFailure, match="^native-command$") as caught:
        user_worker(cluster, markers).object_user()
    assert ACCESS not in json.dumps(caught.value.native_error) and SECRET not in json.dumps(caught.value.native_error)


def test_user_waits_for_the_committed_period_and_the_recorded_tool(ready, monkeypatch):
    cluster, markers = ready
    committed, cluster.periods["objects"]["master_zone"] = cluster.periods["objects"]["master_zone"], ""
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        user_worker(cluster, markers).object_user()
    cluster.periods["objects"]["master_zone"] = committed
    monkeypatch.setattr(ceph_native_object.ObjectWorker, "tool_digest", lambda self: "f" * 64)
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        user_worker(cluster, markers).object_user()
    assert creates(cluster) == []


def test_recovery_scope_is_the_clusters_bootstrap_scope(ready):
    cluster, markers = ready
    assert user_worker(cluster, markers).recovery_scope() == gateway.scope(gateway.gateway_request())
    assert "objectUser" not in json.dumps(ceph_tunnel_staging.recovery_scope({k: v for k, v in cluster.request.items() if k != "objectUser"}))


@pytest.mark.parametrize("change", [
    {"name": "dashboard"}, {"name": "App"}, {"displayName": ""}, {"displayName": "line\nbreak"}, {"displayName": "x" * 257},
    {"realm": "Objects"}, {"gateway": ""},
])
def test_request_admits_only_its_closed_user(change):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(user_request(**change))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(dict(user_request(), objectUser=dict(USER, accessKey=ACCESS)))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(dict(user_request(), gateway=gateway.gateway_request()["gateway"]))
    assert ceph_native.validate(user_request(displayName="x" * 256))


def test_dispatch_routes_object_user_phases_to_the_user_worker_without_material(ready, monkeypatch):
    cluster, unused = ready
    seen = []
    monkeypatch.setattr(ceph_native_object.UserWorker, "object_user_read", lambda self: seen.append(type(self)) or {"private": material()})
    envelope = {"request": cluster.request, "host": "node-1", "phase": "object_user_read", "private": {}}
    assert ceph_native.dispatch(envelope) == {"private": material()} and seen == [ceph_native_object.UserWorker]
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch(dict(envelope, private={"key": "x"}))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(dict(envelope, phase="gateway_zone"))


def observe(cluster, markers):
    native = user_worker(cluster, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


core_request = core.core_request


def core_user_request(request):
    return dict(request, action="object-user", objectUser=dict(USER))


class Transport:
    def __init__(self, request, private=None, ready_after=0):
        self.request, self.private, self.calls, self.ready_after = request, private or material(), [], ready_after

    def call(self, host, phase, timeout=None):
        del timeout
        self.calls.append((host["name"], phase))
        if phase == "observe":
            done = self.calls.count((host["name"], "observe")) > self.ready_after
            return dict(core.observation(self.request, ready=done), request="", removed=False, exists=done, partial=not done,
                        resource={"id": "app-writer" if done else "", "path": ""})
        if phase == "object_user":
            return {"changed": True, "private": self.private}
        if phase == "object_user_read":
            return {"private": self.private}
        return {"changed": False}


def test_apply_writes_the_retained_pair_to_its_protected_output_once(core_request, tmp_path, monkeypatch):
    request = core_user_request(core_request)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    output = tmp_path / "object-user"
    transport = Transport(request)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, {"object-user": str(output)}, pause=lambda seconds: None)
    assert result["changed"] and result["evidence"]["resource"] == {"id": "app-writer", "path": ""}
    assert [phase for unused, phase in transport.calls] == ["prerequisites", "object_user", "observe"]
    assert output.read_bytes() == material().encode() and os.stat(output).st_mode & 0o077 == 0
    assert ACCESS not in json.dumps(result) and SECRET not in json.dumps(result)


def test_observe_recaptures_only_a_ready_user(core_request, tmp_path):
    request = core_user_request(core_request)
    output = tmp_path / "object-user"
    transport = Transport(request, ready_after=1)
    result = ceph_core.execute(request, "observe", "d" * 64, transport, {"object-user": str(output)})
    assert not result["evidence"]["ready"] and not output.exists() and [p for unused, p in transport.calls] == ["observe"]
    transport = Transport(request)
    ceph_core.execute(request, "observe", "d" * 64, transport, {"object-user": str(output)})
    assert [p for unused, p in transport.calls] == ["observe", "object_user_read"] and output.read_bytes() == material().encode()


@pytest.mark.parametrize("private", [
    material().replace("app-writer", "other-user"), material(access="key with space"), material()[:-1] + ',"extra":"x"}',
    json.dumps({"accessKey": ACCESS, "secretKey": SECRET, "uid": "app-writer"}), material(secret=""), "é",
])
def test_apply_refuses_any_other_produced_shape(core_request, tmp_path, monkeypatch, private):
    request = core_user_request(core_request)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    output = tmp_path / "object-user"
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.execute(request, "apply", "d" * 64, Transport(request, private=private), {"object-user": str(output)}, pause=lambda seconds: None)
    assert not output.exists()


def test_outputs_are_exactly_the_object_user_output(core_request, tmp_path, monkeypatch):
    request = core_user_request(core_request)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    for outputs in ({}, {"client": str(tmp_path / "client")}, {"object-user": str(tmp_path / "a"), "client": str(tmp_path / "b")}):
        with pytest.raises(ceph_native.Refusal):
            ceph_core.execute(request, "apply", "d" * 64, Transport(request), outputs, pause=lambda seconds: None)


def test_object_user_evidence_admits_only_its_user_id(core_request):
    request = core_user_request(core_request)
    value = dict(core.observation(request, ready=True), request="d" * 64, resource={"id": "app-writer", "path": ""})
    assert ceph_core.evidence(value, "d" * 64, request) is value
    assert ceph_core.evidence(dict(value, ready=False, partial=True, resource={"id": "", "path": ""}), "d" * 64, request)
    for refused in (dict(value, resource={"id": "other", "path": ""}), dict(value, resource={"id": "app-writer", "path": "/"}),
                    dict(value, resource={"id": ACCESS, "path": ""}), dict(value, client="client.app-writer"),
                    dict(value, daemons=[{"host": "node-1", "name": "rgw.objects.node-1.abcdef", "service": "rgw.objects"}])):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(refused, "d" * 64, request)
