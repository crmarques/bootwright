"""Export custody and effect boundary probes; every native response is synthetic."""
from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_export_native as export, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_smb_native import scope, Markers
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_client_custody import client  # pylint: disable=unused-import


def request():
    cluster = scope()
    value = {"version": export.VERSION, "cluster": "example", "controller": "controller", "name": "consumer", "filesystem": "files",
             "pool": "block", "defaultDataPool": "fs-data", "keyType": "aes", "machines": [host["machine"] for host in cluster["hosts"]],
             "providedMachines": [], "scope": cluster, "physicalHosts": False, "monitors": {"node-1": "192.0.2.1"},
             "monitoringHosts": ["192.0.2.1", "192.0.2.2"], "monitoringPort": 9283}
    value["principals"] = export.caps_for(value)
    return value


class MemoryCustody:
    """The retained data seam; real inode custody is exercised separately below."""

    def __init__(self, worker, kind, suffix, limit, decode):
        self.worker, self.name, self.decode = worker, kind + suffix, decode

    def records(self, unused):
        return None, None

    def prepare(self, scope, generate):
        if self.name not in self.worker.retained:
            self.worker.retained[self.name] = generate()
        return self.material(scope)

    def material(self, unused):
        return self.decode(self.worker.retained[self.name])


def worker(monkeypatch):
    value = request()
    native = export.ExportWorker(value, "node-1", markers=Markers())
    native.retained, native.auth, native.calls = {}, {}, []
    monkeypatch.setattr(export, "Custody", MemoryCustody)
    facts = {"leader": "node-1", "address": "192.0.2.1", "monitors": "node-1=192.0.2.1:3300",
             "monitoringEndpoint": "192.0.2.1,192.0.2.2", "monitoringPort": "9283"}
    monkeypatch.setattr(native, "prerequisites", lambda: copy.deepcopy(facts))

    def ceph(*args, **kwargs):
        native.calls.append((args, kwargs))
        if args == ("auth", "ls"):
            return {"auth_dump": list(native.auth.values())}
        if args[:2] == ("auth", "add"):
            entry = next(item for item in json.loads(next(iter(native.retained.values()))).values() if item["entity"] == args[2])
            assert entry["key"].encode() in kwargs["data"]
            assert all(entry["key"] not in arg for arg in args)
            native.auth[args[2]] = entry
            return b""
        if args[:2] == ("auth", "get"):
            return [copy.deepcopy(native.auth[args[2]])]
        raise AssertionError(args)

    monkeypatch.setattr(native, "ceph", ceph)
    return native


def test_fresh_continuation_and_observation_keep_keys(monkeypatch):
    native = worker(monkeypatch)
    first = native.execute(True, {})
    assert first["changed"] and len(native.auth) == 5
    writes = len(native.markers.writes)
    observed = native.execute(False, {})
    assert observed["private"] == first["private"] and not observed["changed"]
    assert len(native.markers.writes) == writes
    again = native.execute(True, {})
    assert again["private"] == first["private"] and not again["changed"]
    for entry in native.auth.values():
        assert entry["key"] not in json.dumps(native.markers.records)
    details = json.loads(first["private"])
    assert next(entry for entry in details if entry["name"] == "rook-ceph-operator-creds")["data"]["userID"].startswith("client.bw-")
    for entry in details:
        if entry["name"].startswith("rook-bw-"):
            assert not entry["data"]["userID"].startswith("client.")


def test_lost_native_reply_recovers_without_mint(monkeypatch):
    native = worker(monkeypatch)
    original = native.ceph
    interrupted = []

    def ceph(*args, **kwargs):
        result = original(*args, **kwargs)
        if args[:2] == ("auth", "add") and not interrupted:
            interrupted.append(True)
            raise RuntimeError("lost native reply")
        return result
    monkeypatch.setattr(native, "ceph", ceph)
    with pytest.raises(RuntimeError):
        native.execute(True, {})
    retained = copy.deepcopy(native.retained)
    native.execute(True, {})
    assert native.retained == retained and len(native.auth) == 5


def test_foreign_principal_refuses_before_custody(monkeypatch):
    native = worker(monkeypatch)
    entity = "client." + native.identifier + "-health"
    native.auth[entity] = {"entity": entity, "caps": {}, "key": "FOREIGN-KEY"}
    with pytest.raises(ceph_native.Refusal, match="export-foreign-principal"):
        native.execute(True, {})
    assert not native.retained and not native.markers.writes


def test_prerequisite_refuses_before_effect(monkeypatch):
    native = worker(monkeypatch)

    def refuse():
        raise ceph_native.Refusal("export-key-type")
    monkeypatch.setattr(native, "prerequisites", refuse)
    with pytest.raises(ceph_native.Refusal, match="export-key-type"):
        native.execute(True, {})
    assert not native.retained and not native.calls and not native.markers.writes


def test_changed_key_or_capabilities_refuse_without_write(monkeypatch):
    native = worker(monkeypatch)
    native.execute(True, {})
    next(iter(native.auth.values()))["caps"] = {"mon": "allow *"}
    writes = len(native.markers.writes)
    with pytest.raises(ceph_native.Refusal, match="export-identity"):
        native.execute(False, {})
    assert len(native.markers.writes) == writes


@pytest.mark.parametrize("kind", ["aes", "aes256k"])
def test_generated_keys_have_the_declared_encoding(kind):
    keys = [export.generated_key(kind) for ignored_index in range(4)]
    assert len(set(keys)) == 4 and all(export.key_valid(key, kind) for key in keys)
    assert all(not export.key_valid(key, "aes256k" if kind == "aes" else "aes") for key in keys)


def test_empty_custody_decoder_refuses_so_first_generation_runs():
    native = export.ExportWorker(request(), "node-1", markers=Markers())
    with pytest.raises(ceph_native.Refusal, match="export-identity"):
        native.decode_keys(b"")


def test_real_inode_custody_retains_export_keys(client):  # noqa: F811
    value = request()
    value["scope"] = {key: item for key, item in client.native.request.items() if key not in ("action", "client")}
    native = export.ExportWorker(value, "node-1", markers=client.markers)
    store = export.Custody(native, "client", "-export-consumer", 65536, native.decode_keys)
    own = {"owner": value["scope"]["owner"], "export": "consumer"}
    first = store.prepare(own, native.generate)
    assert len(first) == 5

    def forbidden():
        raise AssertionError("regenerated retained keys")
    assert store.prepare(own, forbidden) == first
    assert store.material(own) == first


def test_widened_native_caps_are_refused():
    value = request()
    value["principals"]["rbd-node"]["mon"] = "allow *"
    with pytest.raises(ceph_native.Refusal):
        export.validate(value)


def with_provided_tiebreaker(value):
    value["scope"]["hosts"].append({"name": "tie", "machine": "tie", "address": "198.51.100.10", "user": "operator", "port": 22, "devices": [],
                                    "labels": ["mon"], "location": {}, "sshKeyRef": "fleet-key", "knownHostsRef": "tie-known-hosts", "provided": True})
    value["machines"] = sorted(value["machines"] + ["tie"])
    return value


def test_the_provided_machines_are_exactly_the_scope_s_provided_hosts():
    value = with_provided_tiebreaker(request())
    for provided in ([], ["machine-1"], ["machine-1", "tie"]):
        with pytest.raises(ceph_native.Refusal):
            export.validate(dict(value, providedMachines=provided))
    export.validate(dict(value, providedMachines=["tie"]))
    with pytest.raises(ceph_native.Refusal):
        export.validate(dict(request(), providedMachines=["machine-1"]))


def prerequisite_worker(monkeypatch):
    value = request()
    records = {"pool-block.json": {"name": "block"}, "pool-id-block.json": {"id": 4},
               "filesystem-files.json": {"name": "files"}, "filesystem-id.files.json": {"id": 5}}
    native = export.ExportWorker(value, "node-1", markers=Markers(records))
    responses = {
        ("fsid",): {"fsid": value["scope"]["fsid"]},
        ("osd", "pool", "ls", "detail"): [{"pool_name": "block", "pool_id": 4, "application_metadata": {"rbd": {}}}],
        ("fs", "subvolumegroup", "ls", "files"): [{"name": "csi"}],
        ("mon", "dump"): {"auth_allowed_ciphers": [{"name": "aes", "value": 1}, {"name": "aes256k", "value": 2}]},
        ("mgr", "services"): {"prometheus": "http://192.0.2.1:9283/"},
        ("quorum_status",): {"quorum_leader_name": "node-1", "monmap": {"mons": [
            {"name": "node-1", "public_addrs": {"addrvec": [{"type": "v2", "addr": "192.0.2.1:3300/0"}]}}]}},
    }
    monkeypatch.setattr(native, "verify_tool", lambda: None)
    monkeypatch.setattr(native, "ceph", lambda *args: copy.deepcopy(responses[args]))
    monkeypatch.setattr(native, "fs_dump", lambda: {"filesystems": [{"id": 5, "mdsmap": {"fs_name": "files"}}]})
    return native, responses


def test_native_prerequisites_and_allowed_cipher_shape(monkeypatch):
    native, responses = prerequisite_worker(monkeypatch)
    assert native.prerequisites() == {"leader": "node-1", "address": "192.0.2.1", "monitors": "node-1=192.0.2.1:3300",
                                      "monitoringEndpoint": "192.0.2.1,192.0.2.2", "monitoringPort": "9283"}
    responses[("mon", "dump")] = {"auth_allowed_ciphers": [{"name": "aes256k", "value": 2}]}
    with pytest.raises(ceph_native.Refusal, match="export-key-type"):
        native.prerequisites()
    assert not native.markers.writes


@pytest.mark.parametrize("fault", ["pool-incarnation", "missing-csi", "missing-monitoring", "foreign-monitor", "foreign-manager"])
def test_missing_or_foreign_prerequisites_refuse_before_writes(monkeypatch, fault):
    native, responses = prerequisite_worker(monkeypatch)
    if fault == "pool-incarnation":
        responses[("osd", "pool", "ls", "detail")][0]["pool_id"] = 99
    elif fault == "missing-csi":
        responses[("fs", "subvolumegroup", "ls", "files")] = []
    elif fault == "missing-monitoring":
        responses[("mgr", "services")] = {}
    elif fault == "foreign-monitor":
        responses[("quorum_status",)]["quorum_leader_name"] = "foreign"
    else:
        responses[("mgr", "services")] = {"prometheus": "http://192.0.2.99:9283/"}
    with pytest.raises(ceph_native.Refusal, match="export-prerequisite"):
        native.prerequisites()
    assert not native.markers.writes


def test_rgw_pair_has_exact_caps_and_one_key(monkeypatch):
    native = worker(monkeypatch)
    uid = native.identifier + "-ops"
    info = {"user_id": uid, "caps": [{"type": key, "perm": value} for key, value in export.RGW_CAPS.items()],
            "keys": [{"user": uid, "access_key": "ACCESS-SENTINEL", "secret_key": "SECRET-SENTINEL"}]}
    assert native.rgw_pair(info) == {"accessKey": "ACCESS-SENTINEL", "secretKey": "SECRET-SENTINEL"}
    info["keys"].append(dict(info["keys"][0]))
    with pytest.raises(ceph_native.Refusal, match="export-identity"):
        native.rgw_pair(info)
    info["keys"].pop()
    info["caps"][0]["perm"] = "read"
    with pytest.raises(ceph_native.Refusal, match="export-identity"):
        native.rgw_pair(info)


def test_rgw_lost_reply_recaptures_one_pair_without_key_arguments(monkeypatch):
    native = worker(monkeypatch)
    native.export["gateway"] = {"name": "objects", "fqdn": "objects.example.test", "addresses": ["192.0.2.1"], "port": 80,
                                "realm": "objects", "zoneGroup": "objects", "zone": "objects", "indexPool": "objects.rgw.buckets.index",
                                "tls": False, "certificateRef": ""}
    native.export["principals"] = export.caps_for(native.export)
    uid = native.identifier + "-ops"
    stored = []
    calls = []

    def rgw(*args):
        calls.append(args)
        assert "ACCESS-SENTINEL" not in args and "SECRET-SENTINEL" not in args
        assert not any(arg.startswith("--access-key") or arg.startswith("--secret-key") for arg in args)
        if args[:2] == ("user", "list"):
            return [uid] if stored else []
        if args[:2] == ("user", "create"):
            assert "--gen-access-key" in args and "--gen-secret" in args
            stored.append({"user_id": uid, "caps": [{"type": key, "perm": value} for key, value in export.RGW_CAPS.items()],
                           "keys": [{"user": uid, "access_key": "ACCESS-SENTINEL", "secret_key": "SECRET-SENTINEL"}]})
            raise RuntimeError("lost RGW create reply")
        if args[:2] == ("user", "info"):
            return copy.deepcopy(stored[0])
        raise AssertionError(args)
    monkeypatch.setattr(native, "rgw", rgw)
    with pytest.raises(RuntimeError):
        native.execute(True, {})
    result = native.execute(True, {})
    assert sum(args[:2] == ("user", "create") for args in calls) == 1
    assert "SECRET-SENTINEL" in result["private"]
    assert "SECRET-SENTINEL" not in json.dumps(native.markers.records)
    before = len(native.markers.writes)
    assert native.execute(False, {})["private"] == result["private"]
    assert len(native.markers.writes) == before


def test_failed_rgw_create_retains_no_rgw_material(monkeypatch):
    native = worker(monkeypatch)
    native.export["gateway"] = {"name": "objects", "fqdn": "objects.example.test", "addresses": ["192.0.2.1"], "port": 80,
                                "realm": "objects", "zoneGroup": "objects", "zone": "objects", "indexPool": "objects.rgw.buckets.index",
                                "tls": False, "certificateRef": ""}
    native.export["principals"] = export.caps_for(native.export)

    def rgw(*args):
        if args[:2] == ("user", "list"):
            return []
        raise ceph_native.Refusal("export-prerequisite")
    monkeypatch.setattr(native, "rgw", rgw)
    with pytest.raises(ceph_native.Refusal, match="export-prerequisite"):
        native.execute(True, {})
    assert not any("export-rgw" in key for key in native.retained)
    assert "export-consumer-complete.json" not in native.markers.records
