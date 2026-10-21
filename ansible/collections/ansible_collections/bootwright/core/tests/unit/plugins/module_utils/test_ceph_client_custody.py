"""Generated Ceph client identity stays in explicit protected custody.

These temporary-filesystem tests inject native effects and root metadata; they
do not execute Ceph, acquire credentials or qualify a provider artifact.
"""

from __future__ import annotations

import base64
import copy
import contextlib
import hashlib
import json
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as tunnel_native


class Markers:
    def __init__(self, request):
        self.path = os.path.join(ceph_native.ROOT, request["fsid"])
        self.records = {"pool-volumes.json": {"name": "volumes"}, "pool-id-volumes.json": {"id": 4}}
        self.writes = []
        self.before = lambda name, value: None
        self.after = lambda name, value: None
        self.is_owned = True

    def owned(self):
        return self.is_owned

    def read(self, name):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        self.before(name, value)
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        if name in self.records:
            return
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)
        self.after(name, value)


SUBVOLUME_PATH = "/volumes/csi/docs/0f8fad5b-d9cb-469f-a165-70867728950e"
NOGROUP_PATH = "/volumes/_nogroup/docs/0f8fad5b-d9cb-469f-a165-70867728950e"
# `ceph fs dump` with the members fs_dump reads (src/mds/FSMap.cc:131-176, src/mds/MDSMap.cc:190-273).
FS_DUMP = {"epoch": 9, "feature_flags": {"enable_multiple": True, "ever_enabled_multiple": True}, "standbys": [], "compat": {},
           "filesystems": [{"id": 1, "mdsmap": {"fs_name": "files", "metadata_pool": 2, "data_pools": [3], "max_mds": 1, "standby_count_wanted": 0,
                                                "flags_state": {"allow_standby_replay": False}, "info": {}}}]}


def scoped_client(arm):
    """The frozen client of one arm: a list names an RBD pool's derived data pools, a word names a CephFS arm."""
    if arm == "filesystem":
        return {"name": "consumer", "pool": "", "access": "read-write", "dataPools": [], "filesystem": "files", "group": "", "subvolume": ""}
    if arm == "subvolume":
        return {"name": "consumer", "pool": "", "access": "read-write", "dataPools": [], "filesystem": "files", "group": "csi", "subvolume": "docs",
                "subvolumePath": SUBVOLUME_PATH}
    if arm == "subvolume-nogroup":
        return {"name": "consumer", "pool": "", "access": "read-write", "dataPools": [], "filesystem": "files", "group": "", "subvolume": "docs",
                "subvolumePath": NOGROUP_PATH}
    return {"name": "consumer", "pool": "volumes", "access": "read-write", "dataPools": list(arm), "filesystem": "", "group": "", "subvolume": ""}


@pytest.fixture
def client(tmp_path, monkeypatch, request):
    arm = getattr(request, "param", [])
    data_pools = list(arm) if isinstance(arm, list) else []
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i),
              "user": "operator", "port": 22, "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}],
              "labels": [], "location": {}, "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    request = {"controller": "controller", "version": "ceph-core-v26", "action": "client", "cluster": "example", "owner": "apply-example",
               "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
               "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
               "release": "9.1", "ansiblePackageVersion": "3.2.0",
               "packageRepository": "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64",
               "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator",
               "clusterSSHTransport": "verified-tunnel", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
               "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
               "services": [{"service_type": "mon", "placement": {"hosts": names}},
                            {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                            {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}],
               "client": scoped_client(arm)}
    root = tmp_path / "root"
    root.mkdir(mode=0o755)
    directory = root
    for part in (ceph_native.ROOT + "/" + request["fsid"]).strip("/").split("/"):
        directory /= part
        directory.mkdir(mode=0o700 if part == request["fsid"] else 0o755)
    original_open, original_fstat = os.open, os.fstat

    def open_file(path, flags, mode=0o777, *, dir_fd=None):
        if path == "/" and dir_fd is None:
            path = str(root)
        return original_open(path, flags, mode, dir_fd=dir_fd)

    def fstat(fd):
        actual = original_fstat(fd)
        fields = {name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}
        fields.update(st_uid=0, st_gid=0)
        return SimpleNamespace(**fields)

    monkeypatch.setattr(ceph_native.os, "open", open_file)
    monkeypatch.setattr(ceph_native.os, "fstat", fstat)
    monkeypatch.setattr(ceph_native.os, "fchown", lambda fd, uid, gid: ceph_native.require(uid == gid == 0))
    markers = Markers(request)
    native_pools = [{"pool_name": "volumes", "pool_id": 4, "application_metadata": {"rbd": {}}}]
    for number, name in enumerate(data_pools, start=5):
        markers.records["pool-" + name + ".json"] = {"name": name}
        markers.records["pool-id-" + name + ".json"] = {"id": number}
        native_pools.append({"pool_name": name, "pool_id": number, "application_metadata": {"rbd": {}}})
    group = "" if arm == "subvolume-nogroup" else "csi"
    path = NOGROUP_PATH if arm == "subvolume-nogroup" else SUBVOLUME_PATH
    getpath = ["fs", "subvolume", "getpath", "files", "docs"] + (["--group_name", group] if group else [])
    if not isinstance(arm, list):
        markers.records.update({"filesystem-files.json": {"name": "files"}, "filesystem-id.files.json": {"id": 1},
                                "subvolume-docs.json": {"name": "docs", "filesystem": "files", "group": group, "pool": "fs-data", "mode": "755", "uid": 0,
                                                        "gid": 0, "sizeBytes": 0},
                                "subvolume-path.docs.json": {"path": path}})
    key = base64.b64encode(b"synthetic-original-client-secret").decode("ascii")
    state = SimpleNamespace(auth=None, key=key, generated=0, added=0, calls=[], lose_add=False, fail_generation=False,
                            pool_id=4, data_pools=native_pools[1:], fsid=request["fsid"], generation_hook=lambda: None, add_hook=lambda: None,
                            fs_dump=copy.deepcopy(FS_DUMP), getpath=path + "\n")
    native = None

    def run(argv, data=b"", timeout=60):
        del timeout
        state.calls.append((list(argv), data))
        if argv[0] == "/usr/bin/ceph-authtool":
            assert argv == ["/usr/bin/ceph-authtool", "--gen-print-key", "--name", "client.consumer"]
            assert "client-reserved-consumer.json" in markers.records
            assert "client-id-consumer.json" not in markers.records
            state.generated += 1
            state.generation_hook()
            if state.fail_generation:
                raise ceph_native.Refusal("native-command")
            return 0, (state.key + "\n").encode("ascii")
        assert argv[0] == "/usr/bin/ceph"
        args = argv[argv.index("--format") + 2:]
        if args == ["fsid"]:
            value = {"fsid": state.fsid}
        elif args == ["osd", "pool", "ls", "detail"]:
            value = [{"pool_name": "volumes", "pool_id": state.pool_id, "application_metadata": {"rbd": {}}}] + copy.deepcopy(state.data_pools)
        elif args == ["fs", "dump"]:
            value = copy.deepcopy(state.fs_dump)
        elif args == getpath:
            # The volumes module answers the path as text (src/pybind/mgr/volumes/fs/volume.py:484-498), here with one trailing newline;
            # a subvolume of the default group is asked for without --group_name (an explicit _nogroup refuses EPERM).
            assert data == b""
            return 0, state.getpath.encode()
        elif args == ["auth", "ls"]:
            value = {"auth_dump": [] if state.auth is None else copy.deepcopy(state.auth)}
        elif args == ["auth", "get", "client.consumer"]:
            assert state.auth is not None
            value = copy.deepcopy(state.auth)
        elif args == ["auth", "add", "client.consumer", "-i", "-"]:
            assert "client-id-consumer.json" in markers.records
            assert "client-attempt-consumer.json" in markers.records
            expected = [{"entity": "client.consumer", "key": state.key, "caps": native.caps()}]
            assert data == native.client_keyring(expected)
            assert state.key not in " ".join(argv)
            state.add_hook()
            if state.auth is not None and state.auth != expected:
                return 17, b"PRIVATE-DIFFERENT-PRINCIPAL"
            state.auth = expected
            state.added += 1
            if state.lose_add:
                raise ceph_native.Refusal("native-command")
            return 0, b"added key for client.consumer\n"
        else:
            pytest.fail("unexpected native command: " + repr(args))
        assert data == b""
        return 0, json.dumps(value).encode()

    native = ceph_native_resources.ResourceWorker(request, request["bootstrap"], run=run, markers=markers)
    native.verify_tool = lambda: {"synthetic-artifact": True}
    native.verify_client_generator = lambda: None
    # The independently tested cluster producer proves the original admin
    # config/keyring before this isolated client-custody consumer is invoked.
    native.bootstrap_hold = contextlib.nullcontext
    return SimpleNamespace(native=native, state=state, directory=directory, root=root,
                           material_path=directory / "client-material-consumer.json", markers=markers)


def test_client_original_material_is_retained_before_native_effect_and_no_secret_digest(client):
    result = client.native.client()
    material = [{"entity": "client.consumer", "key": client.state.key, "caps": client.native.caps()}]
    assert result == {"changed": True, "private": ceph_native.canonical(material).decode()}
    assert client.material_path.read_bytes() == ceph_native.canonical(material)
    assert stat.S_IMODE(client.material_path.stat().st_mode) == 0o600
    assert client.state.generated == client.state.added == 1
    evidence = json.dumps(client.markers.records)
    assert client.state.key not in evidence
    assert hashlib.sha256(client.state.key.encode()).hexdigest() not in evidence
    assert hashlib.sha256(client.material_path.read_bytes()).hexdigest() not in evidence
    assert "keyHash" not in evidence
    assert "key" not in client.markers.records["client-id-consumer.json"]
    assert client.native.client() == dict(result, changed=False)
    assert client.state.generated == client.state.added == 1


@pytest.mark.parametrize("client", [[], ["archive", "ec-data"], "filesystem", "subvolume", "subvolume-nogroup"], indirect=True)
def test_lost_auth_creation_reply_recovers_exact_original_identity(client):
    client.state.lose_add = True
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        client.native.client()
    original = client.material_path.read_bytes()
    assert "client-applied-consumer.json" not in client.markers.records
    assert client.native.client_read()["private"].encode() == original
    client.state.lose_add = False
    assert not client.native.client()["changed"]
    assert client.state.generated == client.state.added == 1
    assert client.material_path.read_bytes() == original


@pytest.mark.parametrize("client", [["archive", "ec-data"]], indirect=True)
def test_client_caps_include_data_pools_in_sorted_order(client):
    osd = "profile rbd pool=volumes, profile rbd pool=archive, profile rbd pool=ec-data"
    assert client.native.caps() == {"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": osd}
    client.native.client()
    added = [data for argv, data in client.state.calls if argv[-5:] == ["auth", "add", "client.consumer", "-i", "-"]]
    assert len(added) == 1 and ('caps osd = "' + osd + '"').encode() in added[0]
    hosts = client.native.request["hosts"]
    ceph_native_resources.validate(copy.deepcopy(client.native.request), hosts)
    for data_pools in (["ec-data", "archive"], ["archive", "archive"], ["volumes"], ["bad,name"], None, "missing"):
        request = copy.deepcopy(client.native.request)
        if data_pools == "missing":
            del request["client"]["dataPools"]
        else:
            request["client"]["dataPools"] = data_pools
        with pytest.raises(ceph_native.Refusal, match="request"):
            ceph_native_resources.validate(request, hosts)


@pytest.mark.parametrize("client", [["archive", "ec-data"]], indirect=True)
@pytest.mark.parametrize("change", ["missing-marker", "different-id", "absent-natively"])
def test_client_data_pools_are_proved_owned_before_key_generation(client, change):
    if change == "missing-marker":
        del client.markers.records["pool-id-ec-data.json"]
    elif change == "different-id":
        client.markers.records["pool-id-archive.json"] = {"id": 9}
    else:
        client.state.data_pools = client.state.data_pools[:1]
    with pytest.raises(ceph_native.Refusal, match="pool-identity"):
        client.native.client()
    assert not [call for call in client.state.calls if call[0][0] == "/usr/bin/ceph-authtool" or "add" in call[0]]
    assert client.state.generated == client.state.added == 0
    assert "client-reserved-consumer.json" not in client.markers.records


def test_generation_interruption_has_no_auth_effect_and_reuses_owned_reservation(client):
    client.state.fail_generation = True
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        client.native.client()
    reserved = copy.deepcopy(client.markers.records["client-reserved-consumer.json"])
    assert client.material_path.read_bytes() == b""
    assert client.state.auth is None and client.state.added == 0
    assert "client-id-consumer.json" not in client.markers.records
    client.state.fail_generation = False
    client.native.client()
    assert client.markers.records["client-reserved-consumer.json"] == reserved
    assert client.state.generated == 2 and client.state.added == 1


@pytest.mark.parametrize("marker", ["client-reserved-consumer.json", "client-id-consumer.json", "client-attempt-consumer.json", "client-applied-consumer.json"])
def test_lost_durable_publication_ack_recovers_exact_state(client, marker):
    fired = False

    def after(name, value):
        nonlocal fired
        del value
        if name == marker and not fired:
            fired = True
            raise ceph_native.Refusal("injected-lost-ack")

    client.markers.after = after
    with pytest.raises(ceph_native.Refusal, match="injected-lost-ack"):
        client.native.client()
    assert fired
    original = client.material_path.read_bytes()
    client.markers.after = lambda name, value: None
    client.native.client()
    assert client.state.auth[0]["key"] == client.state.key
    if original:
        assert client.material_path.read_bytes() == original
        assert client.state.generated == 1
    assert client.state.added == 1


def test_undurable_reservation_failure_cleans_only_new_empty_file(client):
    def before(name, value):
        del value
        if name == "client-reserved-consumer.json":
            raise ceph_native.Refusal("injected-before-record")

    client.markers.before = before
    with pytest.raises(ceph_native.Refusal, match="injected-before-record"):
        client.native.client()
    assert not client.material_path.exists()
    assert client.state.generated == client.state.added == 0
    assert "client-reserved-consumer.json" not in client.markers.records


def test_complete_generated_material_recovers_before_ready_record_without_regeneration(client):
    def before(name, value):
        del value
        if name == "client-id-consumer.json":
            raise ceph_native.Refusal("injected-before-ready")

    client.markers.before = before
    with pytest.raises(ceph_native.Refusal, match="injected-before-ready"):
        client.native.client()
    original = client.material_path.read_bytes()
    assert original and client.state.added == 0
    client.markers.before = lambda name, value: None
    client.native.client()
    assert client.material_path.read_bytes() == original
    assert client.state.generated == client.state.added == 1


@pytest.mark.parametrize("change", ["key", "caps", "duplicate", "extra", "missing"])
def test_changed_native_principal_is_never_adopted_or_overwritten(client, change):
    client.native.client()
    writes = list(client.markers.writes)
    if change == "key":
        client.state.auth[0]["key"] = base64.b64encode(b"replacement-secret").decode()
    elif change == "caps":
        client.state.auth[0]["caps"]["osd"] = "allow *"
    elif change == "duplicate":
        client.state.auth.append(copy.deepcopy(client.state.auth[0]))
    elif change == "extra":
        client.state.auth[0]["extra"] = True
    else:
        client.state.auth = None
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client()
    assert client.state.generated == client.state.added == 1
    assert client.markers.writes == writes


def test_foreign_principal_matching_expected_key_and_caps_is_still_foreign(client):
    client.state.auth = [{"entity": "client.consumer", "key": client.state.key, "caps": client.native.caps()}]
    with pytest.raises(ceph_native.Refusal, match="foreign-client"):
        client.native.client()
    assert not client.material_path.exists()
    assert client.state.generated == client.state.added == 0
    assert client.markers.writes == []


def test_reserved_material_without_auth_attempt_does_not_adopt_matching_principal(client):
    scope = client.native.client_scope()
    client.markers.records["client-consumer.json"] = client.native.request["client"]
    client.native.prepare_client_material(scope)
    client.state.auth = [{"entity": "client.consumer", "key": client.state.key, "caps": client.native.caps()}]
    with pytest.raises(ceph_native.Refusal, match="foreign-client"):
        client.native.client()
    assert client.state.added == 0


@pytest.mark.parametrize("kind", ["matching", "empty", "fifo", "symlink"])
def test_unbound_material_is_refused_without_open_or_read(client, monkeypatch, kind):
    if kind == "fifo":
        os.mkfifo(client.material_path, 0o600)
    elif kind == "symlink":
        target = client.directory / "foreign.json"
        target.write_bytes(b"PRIVATE-FOREIGN")
        client.material_path.symlink_to(target)
    else:
        data = ceph_native.canonical([{"entity": "client.consumer", "key": client.state.key, "caps": client.native.caps()}]) if kind == "matching" else b""
        client.material_path.write_bytes(data)
        client.material_path.chmod(0o600)
    original_open = ceph_native.os.open

    def opening(path, flags, mode=0o777, *, dir_fd=None):
        assert path != "client-material-consumer.json", "unbound foreign material was opened"
        return original_open(path, flags, mode, dir_fd=dir_fd)

    monkeypatch.setattr(ceph_native.os, "open", opening)
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client()
    assert client.state.generated == client.state.added == 0


@pytest.mark.parametrize("kind", ["replacement", "fifo", "symlink", "hardlink", "mode", "content"])
def test_bound_material_drift_refuses_before_native_mutation(client, kind):
    client.native.client()
    original = client.material_path.read_bytes()
    if kind in ("replacement", "fifo", "symlink"):
        client.material_path.rename(client.directory / "old-material")
        if kind == "replacement":
            client.material_path.write_bytes(original)
            client.material_path.chmod(0o600)
        elif kind == "fifo":
            os.mkfifo(client.material_path, 0o600)
        else:
            client.material_path.symlink_to(client.directory / "old-material")
    elif kind == "hardlink":
        os.link(client.material_path, client.directory / "foreign-link")
    elif kind == "mode":
        client.material_path.chmod(0o640)
    else:
        client.material_path.write_bytes(original.replace(client.state.key.encode(), b"A" * len(client.state.key)))
    with pytest.raises((ceph_native.Refusal, OSError)):
        client.native.client()
    assert client.state.generated == client.state.added == 1


@pytest.mark.parametrize("record", ["client-reserved-consumer.json", "client-id-consumer.json"])
@pytest.mark.parametrize("field,value", [("uid", False), ("gid", 0.0), ("mode", 384.0), ("dev", -1), ("inode", 0), ("unknown", 1)])
def test_malformed_descriptor_records_cannot_use_python_numeric_equality(client, record, field, value):
    client.native.client()
    client.markers.records[record]["metadata"][field] = value
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client_read()
    assert client.state.generated == client.state.added == 1


@pytest.mark.parametrize("field,value", [("version", "old"), ("custodyID", "not-a-custody-id"), ("scope", {}), ("extra", True)])
def test_closed_custody_record_refuses_malformed_or_foreign_identity(client, field, value):
    client.native.client()
    client.markers.records["client-id-consumer.json"][field] = value
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client_read()


def test_scope_numeric_types_are_closed_even_when_python_values_compare_equal(client):
    client.native.client()
    client.markers.records["client-id-consumer.json"]["scope"]["poolID"] = 4.0
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client_read()


@pytest.mark.parametrize("record", ["client-reserved-consumer.json", "client-id-consumer.json", "client-attempt-consumer.json", "client-applied-consumer.json"])
@pytest.mark.parametrize("pool_id,alias", [(0, False), (1, True), (0, 0.0), (1, 1.0)])
def test_every_custody_and_effect_scope_refuses_boolean_and_float_pool_id_aliases(client, record, pool_id, alias):
    client.state.pool_id = pool_id
    client.markers.records["pool-id-volumes.json"] = {"id": pool_id}
    client.native.client()
    client.markers.records[record]["scope"]["poolID"] = alias
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client_read()
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client()
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.observe_resource({}, True)
    assert client.state.generated == client.state.added == 1


@pytest.mark.parametrize("record", ["client-reserved-consumer.json", "client-id-consumer.json", "client-attempt-consumer.json", "client-applied-consumer.json"])
@pytest.mark.parametrize("change", ["scope-extra", "client-extra", "scope-missing", "client-non-string"])
def test_closed_scope_shapes_apply_to_material_and_native_effect_records(client, record, change):
    client.native.client()
    scope = client.markers.records[record]["scope"]
    if change == "scope-extra":
        scope["extra"] = True
    elif change == "client-extra":
        scope["client"]["extra"] = True
    elif change == "scope-missing":
        del scope["host"]
    else:
        scope["client"]["name"] = 1
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client_read()
    assert client.state.generated == client.state.added == 1


def test_legacy_secret_hash_is_refused_without_adoption(client):
    client.markers.records["client-id-consumer.json"] = {"keyHash": hashlib.sha256(client.state.key.encode()).hexdigest()}
    with pytest.raises(ceph_native.Refusal, match="client-identity"):
        client.native.client()
    assert client.state.generated == client.state.added == 0
    assert not client.material_path.exists()


@pytest.mark.parametrize("changed", ["pool", "fsid", "unowned-pool"])
def test_native_cluster_and_pool_identity_are_proved_before_key_generation(client, changed):
    if changed == "pool":
        client.state.pool_id = 5
    elif changed == "fsid":
        client.state.fsid = "foreign"
    else:
        del client.markers.records["pool-volumes.json"]
    with pytest.raises(ceph_native.Refusal):
        client.native.client()
    assert client.state.generated == client.state.added == 0
    assert not client.material_path.exists()


def test_read_only_material_recapture_and_resource_observation_change_no_markers(client):
    client.native.client()
    writes = list(client.markers.writes)
    original = client.material_path.read_bytes()
    assert client.native.client_read() == {"private": original.decode()}
    evidence = {}
    assert client.native.observe_resource(evidence, True)
    assert evidence == {"owned": True, "exists": True, "client": "client.consumer"}
    assert client.markers.writes == writes
    assert client.state.generated == client.state.added == 1


def test_absent_native_client_preserves_owned_key_custody_and_cleanup_work(client):
    client.native.client()
    original = client.material_path.read_bytes()
    retained = copy.deepcopy(client.markers.records)
    writes = list(client.markers.writes)
    before = len(client.state.calls)
    client.state.auth = None

    evidence = {}
    assert client.native.observe_resource(evidence, True) is False
    assert evidence == {"owned": True, "exists": True, "client": ""}
    assert client.material_path.read_bytes() == original
    assert client.markers.records == retained and client.markers.writes == writes
    assert client.state.generated == client.state.added == 1
    assert len(client.state.calls) == before + 1
    argv, data = client.state.calls[-1]
    assert argv[argv.index("--format") + 2:] == ["auth", "ls"] and data == b""


def test_generation_reply_malformed_or_secret_error_never_appears_in_diagnostics(client):
    client.state.key = "PRIVATE-GENERATOR-SENTINEL"
    with pytest.raises(ceph_native.Refusal, match="client-identity") as caught:
        client.native.client()
    assert "PRIVATE-GENERATOR-SENTINEL" not in str(caught.value)
    assert client.state.added == 0
    assert client.material_path.read_bytes() == b""


def test_racing_foreign_auth_add_cannot_be_overwritten(client):
    foreign = [{"entity": "client.consumer", "key": base64.b64encode(b"foreign").decode(), "caps": client.native.caps()}]
    client.state.add_hook = lambda: setattr(client.state, "auth", copy.deepcopy(foreign))
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        client.native.client()
    assert client.state.auth == foreign
    assert client.state.added == 0
    assert "client-applied-consumer.json" not in client.markers.records


def test_native_effect_is_not_admitted_when_ready_publication_failed(client):
    def before(name, value):
        del value
        if name == "client-id-consumer.json":
            raise ceph_native.Refusal("injected-ready-failure")

    client.markers.before = before
    with pytest.raises(ceph_native.Refusal, match="injected-ready-failure"):
        client.native.client()
    assert client.state.added == 0 and client.state.auth is None
    assert "client-attempt-consumer.json" not in client.markers.records


def test_generator_requires_exact_package_owner_and_pins_only_binary_identity(client, monkeypatch):
    native = client.native
    calls = []
    monkeypatch.setattr(ceph_native_resources, "read_regular", lambda path, maximum: b"synthetic-authtool-binary")

    def command(argv, **kwargs):
        calls.append((argv, kwargs))
        return b"ceph-common 2 20.2.2 349.el9cp x86_64"

    native.command = command
    native.packages = tunnel_native.Decided("20.2.1-405.el9cp")
    ceph_native_resources.ResourceWorker.verify_client_generator(native)
    assert calls == [(["/usr/bin/rpm", "-qf", "--qf", "%{NAME} " + tunnel_native.PACKAGE_QUERY, "/usr/bin/ceph-authtool"], {"json_result": False})]
    record = client.markers.records["client-generator.json"]
    assert record == {"path": "/usr/bin/ceph-authtool", "packageVersion": "20.2.1-405.el9cp",
                      "sha256": hashlib.sha256(b"synthetic-authtool-binary").hexdigest()}
    native.command = lambda *args, **kwargs: b"foreign-package 2 20.2.2 349.el9cp x86_64"
    with pytest.raises(ceph_native.Refusal, match="artifact"):
        ceph_native_resources.ResourceWorker.verify_client_generator(native)


@pytest.mark.parametrize("client, path", [("subvolume", SUBVOLUME_PATH), ("subvolume-nogroup", NOGROUP_PATH)], indirect=["client"])
def test_cephfs_client_binds_proved_path(client, path):
    ceph_native_resources.validate_client(client.native.request["client"])
    assert client.native.caps()["mds"] == "allow rw fsname=files path=" + path
    client.native.client()
    added = [data for argv, data in client.state.calls if argv[-5:] == ["auth", "add", "client.consumer", "-i", "-"]]
    assert len(added) == 1 and ('caps mds = "allow rw fsname=files path=' + path + '"').encode() in added[0]
    getpath = [argv[argv.index("--format") + 2:] for argv, data in client.state.calls if "getpath" in argv]
    assert getpath and all(("--group_name" in args) == (path == SUBVOLUME_PATH) for args in getpath)


def foreign_path(client):
    client.state.getpath = "/volumes/csi/docs/1f8fad5b-d9cb-469f-a165-70867728950e\n"


def foreign_marker(client):
    client.markers.records["subvolume-path.docs.json"] = {"path": "/volumes/csi/docs/1f8fad5b-d9cb-469f-a165-70867728950e"}


def missing_subvolume(client):
    del client.markers.records["subvolume-docs.json"]


def other_group(client):
    client.markers.records["subvolume-docs.json"]["group"] = "other"


def other_filesystem_id(client):
    client.markers.records["filesystem-id.files.json"] = {"id": 2}


@pytest.mark.parametrize("client", ["subvolume"], indirect=True)
@pytest.mark.parametrize("change, reason", [(foreign_path, "subvolume-identity"), (foreign_marker, "subvolume-identity"),
                                            (missing_subvolume, "subvolume-identity"), (other_group, "subvolume-identity"),
                                            (other_filesystem_id, "filesystem-identity")])
def test_cephfs_client_refuses_an_unproved_scope_before_any_key(client, change, reason):
    change(client)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        client.native.client()
    assert not [call for call in client.state.calls if call[0][0] == "/usr/bin/ceph-authtool" or "add" in call[0]]
    assert client.state.generated == client.state.added == 0
    assert "client-reserved-consumer.json" not in client.markers.records


@pytest.mark.parametrize("client, access, caps", [
    ([], "read-write", {"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": "profile rbd pool=volumes"}),
    ("filesystem", "read-write", {"mds": "allow rw fsname=files", "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}),
    ("filesystem", "read-only", {"mds": "allow r fsname=files", "mon": "allow r fsname=files", "osd": "allow r tag cephfs data=files"}),
    ("subvolume", "read-write",
     {"mds": "allow rw fsname=files path=" + SUBVOLUME_PATH, "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}),
    ("subvolume", "read-only", {"mds": "allow r fsname=files path=" + SUBVOLUME_PATH, "mon": "allow r fsname=files", "osd": "allow r tag cephfs data=files"}),
    ("subvolume-nogroup", "read-write",
     {"mds": "allow rw fsname=files path=" + NOGROUP_PATH, "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}),
], indirect=["client"])
def test_client_caps_per_arm(client, access, caps):
    client.native.request["client"]["access"] = access
    assert client.native.caps() == caps


def shaped(arm, **changes):
    value = scoped_client(arm)
    for name, member in changes.items():
        if member is None:
            del value[name]
        else:
            value[name] = member
    return value


@pytest.mark.parametrize("value", [
    shaped("filesystem", pool="volumes"),
    shaped("subvolume", pool="volumes"),
    shaped("filesystem", dataPools=["fs-data"]),
    shaped("subvolume", dataPools=["fs-data"]),
    shaped("subvolume", subvolumePath=None),
    shaped("subvolume", subvolumePath="/volumes/csi/docs/not-a-uuid"),
    shaped("subvolume", subvolumePath="/volumes/csi/other/0f8fad5b-d9cb-469f-a165-70867728950e"),
    shaped("subvolume", subvolumePath="/volumes/_nogroup/docs/0f8fad5b-d9cb-469f-a165-70867728950e"),
    shaped("subvolume-nogroup", subvolumePath=SUBVOLUME_PATH),
    shaped("subvolume-nogroup", subvolumePath="/volumes//docs/0f8fad5b-d9cb-469f-a165-70867728950e"),
    shaped("filesystem", subvolumePath=SUBVOLUME_PATH),
    shaped([], subvolumePath=SUBVOLUME_PATH),
    shaped("filesystem", group="csi"),
    shaped("subvolume", filesystem=""),
    shaped([], filesystem="files"),
    shaped("filesystem", filesystem=None),
    shaped("filesystem", extra=""),
])
def test_client_request_shape_per_arm(value):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native_resources.validate_client(value)


@pytest.mark.parametrize("arm", [[], ["archive", "ec-data"], "filesystem", "subvolume", "subvolume-nogroup"])
def test_client_request_admits_each_arm(arm):
    ceph_native_resources.validate_client(scoped_client(arm))
