"""Native v20.2.2 RBD responses, private bootstrap transfer, identity replay and bounded two-cluster coordination.

Shapes follow src/tools/rbd/action/{Info,MirrorPool,MirrorImage}.cc; token and default-site behavior follow
src/librbd/api/Mirror.cc. Every cluster, key and address here is synthetic. No native command or network runs.
"""
from __future__ import annotations

import base64
import copy
import hashlib
import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_replication, ceph_replication_native as native
from ansible_collections.bootwright.core.plugins.modules import ceph_replication as module
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_pools as pools
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_smb_native as smb
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_client_custody import client  # pylint: disable=unused-import

pytestmark = pytest.mark.usefixtures("client")

PEER_FSID = "b75fb4b0-1ad9-4443-b354-a903813b0091"
GLOBAL_ID = "9620be03-66cf-4f9e-b726-39243d169ce1"
PEER_ID = "ddca5400-9195-4a7c-b28f-62dbd28c550a"
SECRET = "SYNTHETIC-BOOTSTRAP-KEY-SENTINEL"
DIGEST = "d" * 64


def request(mode="image", count=1):
    primary = smb.scope()
    primary.pop("auxiliary")
    secondary = copy.deepcopy(primary)
    secondary.update(cluster="secondary", fsid=PEER_FSID)
    for host in secondary["hosts"]:
        host["machine"] = "peer-" + host["machine"]
        host["address"] = host["address"].replace("192.0.2.", "198.51.100.")
    secondary["monIP"], secondary["publicNetworks"] = "198.51.100.1", ["198.51.100.0/24"]
    result = {"version": native.VERSION, "controller": "controller", "object": "replica", "mode": mode,
              "images": [{"name": "disk" + str(i), "pool": "volumes", "dataPool": "", "sizeBytes": 1048576,
                          "features": ["layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling"]} for i in range(count)]}
    for side, scope in (("primary", primary), ("secondary", secondary)):
        result[side] = {"cluster": scope["cluster"], "machines": sorted(host["machine"] for host in scope["hosts"]),
                        "pool": pools.pool_value(object=side + "-volumes"), "poolObject": side + "-volumes", "scope": scope}
    return native.validate(result)


class Monitor:
    def __init__(self, value, side, source=None):
        self.value, self.side, self.source = value, side, source
        self.mode, self.peer, self.auth, self.config = "disabled", False, False, False
        self.key = SECRET
        self.images = {image["name"]: self.image(image, i) for i, image in enumerate(value["images"])} if side == "primary" else {}
        self.calls, self.synced, self.fail_secret, self.created_tokens = [], True, False, 0

    def image(self, image, index):
        return {"name": image["name"], "id": ("a" if self.side == "primary" else "b") + str(index + 1), "size": image["sizeBytes"],
                "format": 2, "features": image["features"], "snapshot_count": 0,
                "mirroring": {"mode": "journal", "state": "disabled"}}

    def enable(self, image):
        image["mirroring"].update(state="enabled", primary=self.side == "primary", global_id=GLOBAL_ID[:-1] + image["name"][-1])

    def sync(self):
        if self.side == "secondary" and self.peer:
            for index, wanted in enumerate(self.value["images"]):
                original = self.source.images[wanted["name"]]
                if original["mirroring"]["state"] == "enabled" and wanted["name"] not in self.images:
                    current = self.image(wanted, index)
                    self.enable(current)
                    self.images[wanted["name"]] = current

    def bootstrap(self):
        return base64.b64encode(json.dumps({"fsid": self.value["primary"]["scope"]["fsid"], "client_id": "rbd-mirror-peer", "key": self.key,
                                           "mon_host": "[v2:192.0.2.1:3300/0]"}).encode()).decode()

    def run(self, argv, data=b"", timeout=60):
        self.calls.append((list(argv), data, timeout))
        args = [arg for arg in argv[5:] if arg not in native.QUIET]
        if args[:2] == ["--format", "json"]:
            args = args[2:]
        assert SECRET not in str(argv)
        self.sync()
        if args == ["fsid"]:
            result = {"fsid": self.value[self.side]["scope"]["fsid"]}
        elif args == ["osd", "pool", "ls", "detail"]:
            result = [{"pool_name": "volumes", "pool_id": 1}]
        elif args[:2] in (["auth", "get"], ["config-key", "get"]):
            present = self.auth if args[0] == "auth" else self.config
            return (0, b"present") if present else (2, b"missing")
        elif args == ["mirror", "pool", "info", "--format", "json", "volumes"]:
            result = {"mode": self.mode}
            if self.mode != "disabled":
                result.update(site_name=self.value[self.side]["scope"]["fsid"], peers=[])
            if self.peer:
                result["peers"] = [{"uuid": PEER_ID, "direction": "rx-only", "site_name": self.value["primary"]["scope"]["fsid"],
                                    "client_name": "client.rbd-mirror-peer", "mirror_uuid": ""}]
        elif args[:3] == ["mirror", "pool", "enable"]:
            self.mode = args[-1]
            if self.mode == "pool":
                for image in self.images.values():
                    self.enable(image)
            return 0, b""
        elif args[:5] == ["mirror", "pool", "peer", "bootstrap", "create"]:
            assert all(flag in argv for flag in native.QUIET)
            self.created_tokens += 1
            if self.fail_secret:
                return 1, (SECRET + self.bootstrap()).encode()
            self.auth = self.config = True
            return 0, (self.bootstrap() + "\n").encode()
        elif args[:5] == ["mirror", "pool", "peer", "bootstrap", "import"]:
            assert args[-4:] == ["--direction", "rx-only", "volumes", "-"] and all(flag in argv for flag in native.QUIET)
            assert data.decode() == self.source.bootstrap()
            self.peer = True
            self.sync()
            return 0, b""
        elif args[:1] == ["ls"]:
            result = sorted(self.images)
        elif args[:1] == ["info"]:
            result = self.images[args[-1].split("/")[-1]]
        elif args[:3] == ["mirror", "image", "enable"]:
            self.enable(self.images[args[-2].split("/")[-1]])
            return 0, b""
        elif args[:3] == ["mirror", "image", "status"]:
            image = self.images[args[-1].split("/")[-1]]
            result = {"name": image["name"], "global_id": image["mirroring"]["global_id"],
                      "state": "up+stopped" if self.side == "primary" else "up+replaying" if self.synced else "up+syncing"}
        else:
            raise AssertionError(args)
        return 0, json.dumps(result).encode()


class Transport:
    def __init__(self, value, side, monitor):
        self.value, self.side, self.monitor = value, side, monitor
        records = {"pool-volumes.json": value[side]["pool"], "pool-id-volumes.json": {"id": 1}}
        if side == "primary":
            for image in value["images"]:
                records["rbd-" + image["name"] + ".json"] = image
                records["rbd-id." + image["name"] + ".json"] = {"id": monitor.images[image["name"]]["id"]}
        self.markers, self.calls, self.interrupt = smb.Markers(records), [], None
        self.markers.path = os.path.join(ceph_native.ROOT, value[side]["scope"]["fsid"])
        self.native_counts = []

    def replication(self, host, phase, value, side, index, private, timeout=660):
        assert side == self.side and host["name"] == value[side]["scope"]["bootstrap"]
        self.calls.append((phase, index, timeout))
        start = len(self.monitor.calls)
        worker = native.ReplicationWorker(value, side, host["name"], run=self.monitor.run, markers=self.markers)
        methods = {"replication_prepare": worker.prepare, "replication_token": worker.bootstrap_token, "replication_pool_observe": worker.observe_pool}
        if phase == "replication_import":
            result = worker.import_peer(native.token(private["token"], value["primary"]["scope"]["fsid"]))
        elif phase == "replication_image":
            result = worker.enable_image(index)
        elif phase == "replication_image_observe":
            result = worker.observe_image(index)
        elif phase == "replication_commit":
            result = worker.commit(private["images"])
        else:
            result = methods[phase]()
        self.native_counts.append((phase, len(self.monitor.calls) - start))
        assert sum(call[2] for call in self.monitor.calls[start:]) <= 540
        if self.interrupt == phase:
            self.interrupt = None
            raise KeyboardInterrupt("synthetic lost reply")
        return result


def fleet(value):
    left = Monitor(value, "primary")
    right = Monitor(value, "secondary", left)
    return {"primary": Transport(value, "primary", left), "secondary": Transport(value, "secondary", right)}


@pytest.mark.parametrize("mode", ["image", "pool"])
@pytest.mark.parametrize("count", [1, 3])
def test_complete_native_replication_and_read_only_replay(mode, count):
    value = request(mode, count)
    transports = fleet(value)
    result = ceph_replication.execute(value, "apply", DIGEST, transports)
    assert result["changed"] and result["evidence"]["ready"]
    assert all(image["synced"] for image in result["evidence"]["images"])
    assert SECRET not in json.dumps(result) and SECRET not in str([t.markers.records for t in transports.values()])
    assert sum(len(t.calls) for t in transports.values()) == 8 + 3 * count
    replay = ceph_replication.execute(value, "apply", DIGEST, transports)
    assert replay == dict(result, changed=False)
    writes = sum(len(t.markers.writes) for t in transports.values())
    observed = ceph_replication.execute(value, "observe", DIGEST, transports)
    assert observed["evidence"] == result["evidence"] and not observed["changed"]
    assert sum(len(t.markers.writes) for t in transports.values()) == writes


@pytest.mark.parametrize("side,phase", [("primary", "replication_prepare"), ("secondary", "replication_prepare"), ("primary", "replication_token"),
                                        ("secondary", "replication_import"), ("primary", "replication_image"), ("primary", "replication_commit"),
                                        ("secondary", "replication_commit")])
def test_interruption_after_each_native_boundary_resumes_same_owned_objects(side, phase):
    value = request()
    transports = fleet(value)
    transports[side].interrupt = phase
    with pytest.raises(KeyboardInterrupt):
        ceph_replication.execute(value, "apply", DIGEST, transports)
    assert ceph_replication.execute(value, "apply", DIGEST, transports)["evidence"]["ready"]
    assert transports["secondary"].markers.read("replication-replica-peer.json") == {"uuid": PEER_ID}


@pytest.mark.parametrize("foreign", ["pool-mode", "image", "credential", "config"])
def test_foreign_objects_never_adopted(foreign):
    value = request()
    transports = fleet(value)
    if foreign == "pool-mode":
        transports["primary"].monitor.mode = "image"
    elif foreign == "image":
        transports["secondary"].monitor.images["disk0"] = transports["secondary"].monitor.image(value["images"][0], 0)
    else:
        setattr(transports["primary"].monitor, "auth" if foreign == "credential" else "config", True)
    with pytest.raises(ceph_native.Refusal, match="replication-identity"):
        ceph_replication.execute(value, "apply", DIGEST, transports)
    assert not transports["secondary"].monitor.peer
    assert transports["primary"].monitor.created_tokens == 0


def test_peer_unavailable_is_bounded_and_never_publishes_ready():
    value = request()
    transports = fleet(value)
    transports["secondary"].monitor.synced = False
    elapsed = [0.0]

    def pause(duration):
        elapsed[0] += duration
    with pytest.raises(ceph_native.Refusal, match="replication-unavailable"):
        ceph_replication.execute(value, "apply", DIGEST, transports, now=lambda: elapsed[0], pause=pause)
    assert elapsed[0] == 1800
    assert transports["secondary"].markers.read("replication-replica-completed.json") is None
    transports["secondary"].monitor.synced = True
    assert ceph_replication.execute(value, "apply", DIGEST, transports)["evidence"]["ready"]


def test_native_failure_discards_token_output_and_module_admits_only_two_private_keys():
    value = request()
    transports = fleet(value)
    transports["primary"].monitor.fail_secret = True
    with pytest.raises(ceph_native.Refusal) as caught:
        ceph_replication.execute(value, "apply", DIGEST, transports)
    assert str(caught.value) == "replication-token" and not hasattr(caught.value, "native_error")
    params = {"request": value, "operation": "apply", "digest": DIGEST,
              "material": {"primarySSHKey": "/private/primary", "secondarySSHKey": "/private/secondary", "knownHosts": "/private/known"}}
    assert module.admitted(params) == value
    params["material"]["token"] = "/tmp/not-permitted"
    with pytest.raises(ceph_native.Refusal, match="material"):
        module.admitted(params)


@pytest.mark.parametrize("field", ["id", "global_id"])
def test_replaced_native_image_identity_refuses_after_completion(field):
    value = request()
    transports = fleet(value)
    ceph_replication.execute(value, "apply", DIGEST, transports)
    image = transports["secondary"].monitor.images["disk0"]
    if field == "id":
        image[field] = "c1"
    else:
        image["mirroring"][field] = PEER_ID
    with pytest.raises(ceph_native.Refusal, match="replication-identity"):
        ceph_replication.execute(value, "observe", DIGEST, transports)


def test_embedded_worker_source_closure_contains_replication():
    sources = ceph_core.Transport.native_sources()
    assert "ceph_replication_native" in sources and SECRET not in str(sources)


def test_registered_worker_dispatch_accepts_controller_commit_envelope(monkeypatch):
    value = request()
    identities = [{"name": "disk0", "id": "a1", "globalID": GLOBAL_ID, "synced": True}]

    class Worker:
        def __init__(self, *_args):
            pass

        def commit(self, received):
            assert received == identities
            return {"images": received}
    monkeypatch.setattr(native, "ReplicationWorker", Worker)
    assert native.dispatch({"request": value, "host": value["primary"]["scope"]["bootstrap"], "phase": "replication_commit",
                            "private": {"images": identities}, "side": "primary", "image": -1}) == {"images": identities}


@pytest.mark.parametrize("changed", ["token", "peer"])
def test_rotated_bootstrap_token_and_recorded_peer_identity_refuse(changed):
    value = request()
    transports = fleet(value)
    ceph_replication.execute(value, "apply", DIGEST, transports)
    if changed == "token":
        transports["primary"].monitor.key = "SYNTHETIC-ROTATED-KEY"
        with pytest.raises(ceph_native.Refusal, match="replication-token"):
            ceph_replication.execute(value, "apply", DIGEST, transports)
    else:
        transports["secondary"].markers.records["replication-replica-peer.json"] = {"uuid": GLOBAL_ID}
        with pytest.raises(ceph_native.Refusal, match="replication-identity"):
            ceph_replication.execute(value, "observe", DIGEST, transports)


@pytest.mark.parametrize("field,replacement", [("fsid", PEER_FSID), ("client_id", "foreign"), ("key", "line\nfeed")])
def test_bootstrap_token_rejects_foreign_or_malformed_identity(field, replacement):
    value = request()
    content = json.loads(base64.b64decode(Monitor(value, "primary").bootstrap()))
    content[field] = replacement
    with pytest.raises(ceph_native.Refusal, match="replication-token"):
        native.token(base64.b64encode(json.dumps(content).encode()).decode(), value["primary"]["scope"]["fsid"])


def test_transport_confines_token_to_bounded_worker_stdin(monkeypatch):
    value = request()
    transport = object.__new__(ceph_core.Transport)
    captured = []

    def call(host, source, payload, timeout, **kwargs):
        captured.append((host, source, payload, timeout, kwargs))
        return {"changed": True}
    monkeypatch.setattr(transport, "_call", call)
    host = value["secondary"]["scope"]["hosts"][0]
    transport.replication(host, "replication_import", value, "secondary", private={"token": SECRET})
    unused, source, payload, timeout, kwargs = captured[0]
    assert SECRET not in source and timeout == 660 and kwargs == {"worker_budget": 640}
    assert payload["payload"]["private"] == {"token": SECRET}
    assert payload["payload"]["side"] == "secondary" and payload["budget"] == 640
    with pytest.raises(ceph_native.Refusal, match="replication-token"):
        native.token("A" * 1024, value["primary"]["scope"]["fsid"])


def test_bootstrap_retains_private_custody_without_secret_hash(client):
    value = request()
    transports = fleet(value)
    ceph_replication.execute(value, "apply", DIGEST, transports)
    primary = transports["primary"]
    material = primary.monitor.bootstrap().encode("ascii")
    records = json.dumps(primary.markers.records)
    assert hashlib.sha256(material).hexdigest() not in records
    assert material.decode() not in records and SECRET not in records
    path = client.directory / "replication-material-replica.json"
    assert path.read_bytes() == material
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    record = primary.markers.read("replication-id-replica.json")
    assert record["version"] == "ceph-replication-custody-v1"
    assert set(record) == {"version", "scope", "custodyID", "metadata"}
    metadata = ceph_native.Custody.metadata(path.stat(), complete=True)
    worker = native.ReplicationWorker(value, "primary", value["primary"]["scope"]["bootstrap"],
                                      run=primary.monitor.run, markers=primary.markers)
    assert worker.bootstrap_token() == {"token": material.decode("ascii")}
    assert primary.monitor.created_tokens == 2
    assert primary.markers.read("replication-id-replica.json") == record
    assert ceph_native.Custody.metadata(path.stat(), complete=True) == metadata


@pytest.mark.parametrize("marker", ["replication-replica-credential-attempt.json", "replication-reserved-replica.json", "replication-id-replica.json"])
def test_lost_token_custody_publication_reply_resumes_original_material(client, marker):
    value = request()
    transports = fleet(value)
    primary = transports["primary"]
    publish = primary.markers.write

    def lose_reply(name, record):
        publish(name, record)
        if name == marker:
            raise ceph_native.Refusal("synthetic-lost-reply")

    primary.markers.write = lose_reply
    with pytest.raises(ceph_native.Refusal, match="synthetic-lost-reply"):
        ceph_replication.execute(value, "apply", DIGEST, transports)
    assert primary.markers.read("replication-replica-credential-attempt.json") is not None
    path = client.directory / "replication-material-replica.json"
    original = path.read_bytes() if path.exists() else b""
    primary.markers.write = publish
    assert ceph_replication.execute(value, "apply", DIGEST, transports)["evidence"]["ready"]
    if original:
        assert path.read_bytes() == original
    assert path.read_bytes() == primary.monitor.bootstrap().encode("ascii")


@pytest.mark.parametrize("damage", ["missing", "mode", "content"])
def test_completed_token_custody_damage_refuses_without_replacement(client, damage):
    value = request()
    transports = fleet(value)
    ceph_replication.execute(value, "apply", DIGEST, transports)
    path = client.directory / "replication-material-replica.json"
    if damage == "missing":
        path.unlink()
    elif damage == "mode":
        path.chmod(0o644)
    else:
        path.write_bytes(b"damaged")
    before = path.read_bytes() if path.exists() else None
    with pytest.raises(ceph_native.Refusal, match="replication-identity"):
        ceph_replication.execute(value, "apply", DIGEST, transports)
    assert (path.read_bytes() if path.exists() else None) == before
