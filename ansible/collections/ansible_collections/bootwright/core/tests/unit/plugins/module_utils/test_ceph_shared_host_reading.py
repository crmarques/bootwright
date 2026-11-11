"""One `diff --observe` host reading: each host's state read once for every resource block of one reading scope, replayed in place of
each block's own host reads, with the same evidence, warnings and refusals those reads give."""

from __future__ import annotations

import contextlib
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_filesystems as fs
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_osd_variants as osd_variants
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_pools as pools

DIGEST = "d" * 64
APPLIED = "ceph-call-home-opt-in"
PENDING = "mgr.node-2.ghijkl"
NATIVE_ERROR = {"coordinate": "synthetic image; host package synthetic", "cause": "synthetic cause"}


def resources():
    """Three resource blocks of one cluster: two pools and a placement policy, each with its own request and digest."""
    base = pools.cluster(4)
    volumes = dict(copy.deepcopy(base), action="pool", pool=pools.pool_value())
    images = dict(copy.deepcopy(base), action="pool", pool=pools.pool_value(name="images"))
    policy = dict(copy.deepcopy(base), action="policy", policy=pools.policy_value())
    return [(volumes, "a" * 64), (images, "b" * 64), (policy, "c" * 64)]


class Transport:
    """Each phase's answer; a host_state read applies a specialization and node-2 reports a manager cephadm is restarting."""

    def __init__(self, refuse=None):
        self.calls = []
        self.applied = set()
        self.applied_lock = contextlib.nullcontext()
        self.refuse = refuse

    def call(self, host, phase, timeout=None):
        del timeout
        self.calls.append((host["name"], phase))
        if phase == "observe":
            return dict(pools.evidence_value(self.request["action"], "3"), hosts=[h["name"] for h in self.request["hosts"]], request="")
        assert phase == "host_state"
        if self.refuse is not None and host["name"] == "node-2":
            raise self.refuse()
        self.applied.add(APPLIED)
        state = {"name": host["name"], "owned": True, "exists": True}
        return dict(state, pending=[PENDING]) if host["name"] == "node-2" else state

    def bound(self, request):
        self.request = request
        return self


def remote_refusal():
    error = ceph_native.RemoteRefusal("native-refusal:foreign-cluster@ceph_native.host_state:1225")
    error.native_error = dict(NATIVE_ERROR)
    return error


def roundtrip(value):
    """The reading as the Go caller lends it: canonical JSON text beside the material."""
    return ceph_core.shared_hosts(json.dumps(value))


def test_a_diff_reads_each_host_once_and_every_block_of_its_scope_reads_what_its_own_reads_would():
    blocks = resources()
    leader, leader_digest = blocks[0]
    reader = Transport().bound(leader)
    reading = ceph_core.execute(leader, "observe", leader_digest, reader, reading="hosts")
    assert reading["changed"] is False and reading["evidence"]["version"] == ceph_core.HOST_READING
    assert reader.calls == [(host["name"], "host_state") for host in leader["hosts"]]
    shared = roundtrip(reading["evidence"])
    replays = []
    for request, digest in blocks:
        own = Transport().bound(request)
        expected = ceph_core.execute(request, "observe", digest, own)
        assert own.calls.count(("node-1", "observe")) == 1 and len(own.calls) == 1 + len(request["hosts"])
        replay = Transport().bound(request)
        assert ceph_core.execute(request, "observe", digest, replay, shared=shared) == expected
        assert expected["pending"] == [PENDING] and expected["evidence"]["specializations"] == [APPLIED]
        replays += replay.calls
    host_reads = [call for call in reader.calls + replays if call[1] == "host_state"]
    assert sorted(host_reads) == sorted((host["name"], "host_state") for host in leader["hosts"])


@pytest.mark.parametrize("refuse", [remote_refusal, lambda: ceph_native.Refusal("transport"), lambda: ValueError("synthetic")])
def test_a_refused_host_read_refuses_every_replaying_block_as_its_own_read_would(refuse):
    blocks = resources()
    reader = Transport(refuse).bound(blocks[0][0])
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], reader, reading="hosts")["evidence"]
    assert [entry["name"] for entry in reading["hosts"]] == ["node-1", "node-2"] and "refused" in reading["hosts"][-1]
    for request, digest in blocks:
        with pytest.raises(Exception) as own:
            ceph_core.execute(request, "observe", digest, Transport(refuse).bound(request))
        replay = Transport(refuse).bound(request)
        with pytest.raises(ceph_core.SharedRefusal) as replayed:
            ceph_core.execute(request, "observe", digest, replay, shared=roundtrip(reading))
        assert replay.calls == [("node-1", "observe")]
        assert ceph_core.refusal(replayed.value, ceph_core.CLUSTER_REASONS) == ceph_core.refusal(own.value, ceph_core.CLUSTER_REASONS)
        assert getattr(replayed.value, "native_error", None) == getattr(own.value, "native_error", None)


def test_an_unsettled_host_replays_as_the_unsettled_refusal_its_own_read_raises():
    def unsettled():
        error = ceph_native.RemoteRefusal("native-refusal:not-ready@ceph_native_observe.daemons:42")
        error.native_error = dict(NATIVE_ERROR)
        return error

    blocks = resources()
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], Transport(unsettled).bound(blocks[0][0]), reading="hosts")["evidence"]
    assert reading["hosts"][-1]["refused"]["unsettled"] is True
    for request, digest in blocks:
        with pytest.raises(ceph_core.UnsettledRefusal) as own:
            ceph_core.execute(request, "observe", digest, Transport(unsettled).bound(request))
        with pytest.raises(ceph_core.UnsettledRefusal) as replayed:
            ceph_core.execute(request, "observe", digest, Transport().bound(request), shared=roundtrip(reading))
        assert type(replayed.value) is type(own.value) and replayed.value.host == own.value.host == "node-2"
        assert replayed.value.detail == own.value.detail and replayed.value.args == own.value.args
        assert getattr(replayed.value, "native_error", None) is None and getattr(own.value, "native_error", None) is None
        assert ceph_core.refusal(replayed.value, ceph_core.CLUSTER_REASONS) == ceph_core.refusal(own.value, ceph_core.CLUSTER_REASONS)


def test_a_refusal_the_real_transport_raises_keeps_its_code_site(tmp_path):
    blocks = resources()
    key, known = tmp_path / "key", tmp_path / "known"
    key.write_text("PRIVATE-KEY-SENTINEL")
    known.write_text("".join("192.0.2.%d ssh-ed25519 PUBLIC\n" % index for index in range(1, 5)))
    key.chmod(0o600)
    known.chmod(0o600)

    def transport(request):
        def run(argv, data, timeout):
            del argv, timeout
            payload = json.loads(data)["payload"]
            if payload["phase"] == "observe":
                observed = dict(pools.evidence_value(request["action"], "3"), hosts=[h["name"] for h in request["hosts"]], request="")
                return 0, json.dumps({"ok": True, "result": observed}).encode()
            if payload["host"] == "node-2":
                return 255, b""
            return 0, json.dumps({"ok": True, "result": {"name": payload["host"], "owned": True, "exists": True}}).encode()
        return ceph_core.Transport(request, {"sshKey": str(key), "knownHosts": str(known)}, run=run)

    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], transport(blocks[0][0]), reading="hosts")["evidence"]
    refused = reading["hosts"][-1]["refused"]
    assert refused["reason"] == "transport" and refused["detail"].startswith("transport@ceph_core._call:")
    for request, digest in blocks:
        with pytest.raises(ceph_native.Refusal) as own:
            ceph_core.execute(request, "observe", digest, transport(request))
        with pytest.raises(ceph_core.SharedRefusal) as replayed:
            ceph_core.execute(request, "observe", digest, transport(request), shared=roundtrip(reading))
        assert ceph_core.refusal(replayed.value, ceph_core.CLUSTER_REASONS) == ceph_core.refusal(own.value, ceph_core.CLUSTER_REASONS)


def test_a_malformed_host_state_refuses_where_the_blocks_own_read_refuses():
    blocks = resources()

    class Malformed(Transport):
        def call(self, host, phase, timeout=None):
            result = super().call(host, phase, timeout)
            return dict(result, extra=True) if phase == "host_state" and host["name"] == "node-3" else result

    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], Malformed().bound(blocks[0][0]), reading="hosts")["evidence"]
    request, digest = blocks[2]
    with pytest.raises(ceph_native.Refusal) as own:
        ceph_core.execute(request, "observe", digest, Malformed().bound(request))
    with pytest.raises(ceph_core.SharedRefusal) as replayed:
        ceph_core.execute(request, "observe", digest, Transport().bound(request), shared=roundtrip(reading))
    assert ceph_core.refusal(replayed.value, ceph_core.CLUSTER_REASONS) == ceph_core.refusal(own.value, ceph_core.CLUSTER_REASONS)
    assert ceph_core.refusal(own.value, ceph_core.CLUSTER_REASONS)[1].startswith("evidence@ceph_core.host_entry:")


@pytest.mark.parametrize("change", [
    lambda r: r.update(fsid="5c0a4300-aa43-458c-86d7-ac2256d1fc00"),
    lambda r: r.update(image="registry.example.test/ceph@sha256:" + "b" * 64),
    lambda r: r.update(delegated=[{"name": "crash", "hosts": ["node-1"]}]),
    lambda r: r["services"][1]["placement"].update(count=3),
    lambda r: r["hosts"][1].update(address="192.0.2.99"),
    lambda r: r.update(cluster="other"),
])
def test_a_reading_of_another_scope_is_refused_before_any_read(change):
    blocks = resources()
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], Transport().bound(blocks[0][0]), reading="hosts")["evidence"]
    request, digest = copy.deepcopy(blocks[1])
    change(request)
    replay = Transport().bound(request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(request, "observe", digest, replay, shared=roundtrip(reading))
    assert replay.calls == []


def test_the_reading_scope_ignores_only_the_action_and_the_blocks_own_resource():
    blocks = resources()
    assert len({ceph_core.reading_scope(request) for request, unused in blocks}) == 1
    cluster = pools.cluster(4)
    assert ceph_core.reading_scope(cluster) == ceph_core.reading_scope(blocks[0][0])
    assert set(ceph_native.RESOURCE_MEMBERS).isdisjoint(ceph_native.SCOPE_MEMBERS | ceph_native.SCOPE_OPTIONAL | {"delegated"})


@pytest.mark.parametrize("case", ["cluster", "apply", "outputs", "both", "tiebreaker"])
def test_only_a_resource_observation_reads_or_replays_hosts(case, tmp_path):
    blocks = resources()
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], Transport().bound(blocks[0][0]), reading="hosts")["evidence"]
    request, digest = copy.deepcopy(blocks[0])
    operation, outputs, extra = "observe", None, {}
    if case == "cluster":
        request = pools.cluster(4)
    elif case == "apply":
        operation = "apply"
    elif case == "outputs":
        request.update(action="client", client={"name": "consumer", "pool": "volumes", "access": "readOnly", "dataPools": [], "filesystem": "",
                                                "group": "", "subvolume": ""})
        request.pop("pool")
        outputs = {"client": str(tmp_path / "client")}
    else:
        extra = {"reading": "hosts", "shared": roundtrip(reading)} if case == "both" else {"retirement": "tiebreaker"}
    attempts = [extra] if case == "both" else [dict(extra, reading="hosts"), dict(extra, shared=roundtrip(reading))]
    for selectors in attempts:
        transport = Transport().bound(request)
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_core.execute(request, operation, digest, transport, outputs, **selectors)
        assert transport.calls == []


@pytest.mark.parametrize("damage", [
    lambda v: v.update(version="ceph-host-reading-v2"),
    lambda v: v.update(extra=1),
    lambda v: v["hosts"].pop(),
    lambda v: v["hosts"].reverse(),
    lambda v: v["hosts"][0].update(refused={"reason": "transport", "unsettled": False}),
    lambda v: v["hosts"].__setitem__(-1, {"name": v["hosts"][-1]["name"], "refused": {"reason": "transport"}}),
    lambda v: v["hosts"].__setitem__(-1, {"name": v["hosts"][-1]["name"], "refused": {"reason": "not-ready", "unsettled": True}}),
    lambda v: v["hosts"][-1].update(state=[]),
    lambda v: v.update(specializations=["unknown-specialization"]),
    lambda v: v.update(hosts=[{"name": "node-1", "refused": {"reason": "not-a-reason", "unsettled": False}}]),
    lambda v: v.update(hosts=[{"name": "node-1", "refused": {"reason": "transport", "unsettled": False, "nativeError": {"cause": "x"}}}]),
])
def test_a_damaged_reading_is_refused(damage):
    blocks = resources()
    reading = ceph_core.execute(blocks[0][0], "observe", blocks[0][1], Transport().bound(blocks[0][0]), reading="hosts")["evidence"]
    damaged = copy.deepcopy(reading)
    damage(damaged)
    request, digest = blocks[1]
    with pytest.raises(ceph_native.Refusal):
        ceph_core.execute(request, "observe", digest, Transport().bound(request), shared=roundtrip(damaged))
    with pytest.raises(ceph_native.Refusal):
        ceph_core.shared_hosts("{")


SHARED_ACTIONS = {
    "pool": ("pool", pools.pool_value()),
    "zone-pool": ("pool", pools.pool_value(name="objects.rgw.log", object="objects", role="rgw")),
    "policy": ("policy", pools.policy_value()),
    "client": ("client", {"name": "consumer", "pool": "volumes", "access": "readOnly", "dataPools": [], "filesystem": "", "group": "",
                          "subvolume": ""}),
    "rbd": ("rbd", {"dataPool": "", "features": ["layering"], "name": "disk", "pool": "data", "sizeBytes": 1073741824}),
    "filesystem": ("filesystem", fs.fs_value()),
    "subvolume": ("subvolume", {"filesystem": "files", "gid": 0, "group": "csi", "mode": "755", "name": "docs", "pool": "cephfs-data",
                                "sizeBytes": 1073741824, "uid": 0}),
    "settings": ("settings", {"modules": ["nfs"]}),
}


class Watched(dict):
    """A request that records every member host_state reads, and any whole-request read: iteration, a view, a copy or an encoding."""

    def __init__(self, value, read):
        super().__init__(value)
        self.read = read

    def __getitem__(self, key):
        self.read.append(key)
        return super().__getitem__(key)

    def get(self, key, default=None):
        self.read.append(key)
        return super().get(key, default)

    def __contains__(self, key):
        self.read.append(key)
        return super().__contains__(key)

    def whole(self, how):
        self.read.append("*" + how)

    def __iter__(self):
        self.whole("iter")
        return super().__iter__()

    def keys(self):
        self.whole("keys")
        return super().keys()

    def items(self):
        self.whole("items")
        return super().items()

    def values(self):
        self.whole("values")
        return super().values()

    def copy(self):
        self.whole("copy")
        return super().copy()

    def __deepcopy__(self, memo):
        self.whole("deepcopy")
        return copy.deepcopy(dict(self), memo)

    def __reduce_ex__(self, protocol):
        self.whole("reduce")
        return super().__reduce_ex__(protocol)


def scoped_request(action):
    member, value = SHARED_ACTIONS[action]
    base = pools.cluster(3)
    base["services"].insert(-1, {"service_type": "mds", "service_id": "files", "placement": {"hosts": [h["name"] for h in base["hosts"]]}})
    request = dict(base, action=member, **{member: copy.deepcopy(value)})
    request["owner"], request["fsid"] = "apply-123", "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
    return request


def plain_worker(request, monkeypatch):
    native, calls = core.worker(request, {})
    native.daemons = lambda: []
    return native, calls


def osd_worker(request, monkeypatch):
    """An owned host holding an OSD of an encrypted service with DB and WAL devices: host_state proves the completed variant."""
    osd_variants.variants(request)
    native, unused = core.worker(request, {("/usr/sbin/cephadm", "ls"): [{"fsid": request["fsid"], "name": "osd.0"}]})
    native.markers.records["osd-device-intent.json"] = native.osd_device_scope()
    host = request["hosts"][0]
    rows, disks = [], []
    for index, device in enumerate(host["devices"]):
        role = ("block", "db", "wal")[index]
        uuid = "volume-" + role
        tags = {"ceph.cluster_fsid": request["fsid"], "ceph.type": role, "ceph.osd_id": "0", "ceph.osd_fsid": "6da65e47-8789-4a1c-88e8-3bbd195719a1",
                "ceph.encrypted": "1", "ceph.with_tpm": "1", "ceph.block_uuid": "volume-block", "ceph.db_uuid": "volume-db", "ceph.wal_uuid": "volume-wal"}
        rows.append({"lv_path": "/dev/ceph/" + role, "lv_uuid": uuid, "devices": device["path"] + "(0)",
                     "lv_tags": ",".join(k + "=" + v for k, v in tags.items())})
        disks.append({"path": device["path"], "type": "disk", "serial": device["serial"], "size": device["sizeBytes"],
                      "children": [{"path": "/dev/ceph/" + role, "type": "lvm", "fstype": "crypto_LUKS", "children": [
                          {"path": "/dev/mapper/" + uuid, "type": "crypt", "fstype": "ceph_bluestore"}]}]})
    calls = []

    def command(argv, **kwargs):
        del kwargs
        calls.append(argv)
        if argv[0] == "/usr/bin/lsblk":
            return osd_variants.util_linux(argv, disks)
        if argv[0] == "/usr/sbin/lvs":
            return {"report": [{"lv": rows}]}
        if argv[:3] == ["/usr/sbin/wipefs", "--no-act", "--json"]:
            return {"signatures": [{"type": "LVM2_member"}]}
        assert argv[:2] == ["/usr/sbin/cryptsetup", "luksDump"]
        return b"Tokens:\n  0: systemd-tpm2\nDigests:\n"

    native.command = command
    aliases = {"/dev/disk/by-id/virtio-" + d["serial"]: d["path"] for d in host["devices"]}
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda value: aliases.get(value, value))
    return native, calls


def tunnel_worker(request, monkeypatch):
    """A verifiedTunnel host whose manager cephadm is restarting: host_state holds the transport bundle and proves the manager mounts."""
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel, ceph_tunnel_custody
    native, unused, unused_name, calls = core.tunnel_observation_worker(request, {"state": "stopped", "container_id": "", "container_image_name": ""})
    del native.verify_transport_bundle
    native.runtime_profile = lambda: {"uid": 1000, "label": "system_u:object_r:container_file_t:s0:c1,c2", "imageID": "sha256:" + "a" * 64}
    material = {"manifest.json": "{}"}

    @contextlib.contextmanager
    def hold(markers, slot, scope):
        del markers, slot, scope
        yield {"custodyID": "custody"}, material

    def ready():
        wanted = {"scope": native.transport_scope(), "custodyID": "custody", "manifest": {}}
        native.markers.records["transport-ready.json"] = native.markers.records["transport.json"] = wanted

    monkeypatch.setattr(ceph_tunnel_custody, "hold", hold)
    monkeypatch.setattr(ceph_tunnel_custody, "manager_directory", lambda markers, uid, create=True: core.os.open("/", core.os.O_RDONLY))
    monkeypatch.setattr(ceph_tunnel_custody, "prove_materialized", lambda *args, **kwargs: None)
    monkeypatch.setattr(ceph_tunnel, "verify_bundle", lambda *args, **kwargs: None)
    native.ready = ready
    return native, calls


def recovery_worker(request, monkeypatch):
    """A verifiedTunnel bootstrap host whose bootstrap never enrolled: a resource block's host_state refuses before reading anything else."""
    request["clusterSSHTransport"] = "verifiedTunnel"
    native, calls = plain_worker(request, monkeypatch)
    native.markers.records["bootstrap-scope.json"] = {"fixture": True}
    return native, calls


@pytest.mark.parametrize("scenario", [plain_worker, osd_worker, tunnel_worker, recovery_worker])
def test_a_host_state_reads_nothing_of_the_blocks_own_resource(scenario, monkeypatch):
    """The worker's host_state, behind every shared reading, reads only the reading scope and compares the action with "cluster", for
    every resource action a shared reading serves."""
    results = []
    encoded = []
    real = json.dumps

    def dumps(value, *args, **kwargs):
        if isinstance(value, Watched):
            encoded.append(value)
        return real(value, *args, **kwargs)

    monkeypatch.setattr(json, "dumps", dumps)
    for action in SHARED_ACTIONS:
        request = scoped_request(action)
        assert ceph_native.validate(request) is request
        native, calls = scenario(request, monkeypatch)
        if hasattr(native, "ready"):
            native.ready()
        read = []
        native.request = Watched(native.request, read)
        try:
            outcome = native.host_state()
        except ceph_native.Refusal as error:
            outcome = ("refused", str(error))
        results.append((outcome, [list(argv) for argv in calls]))
        assert not encoded, "host_state encoded the whole request"
        assert not [key for key in read if key in ceph_native.RESOURCE_MEMBERS or key.startswith("*")], read
    assert all(result == results[0] for result in results), results
    expected = {plain_worker: {"exists": False}, osd_worker: {"exists": True}, tunnel_worker: {"pending": ["mgr."]}, recovery_worker: None}[scenario]
    if expected is None:
        assert results[0][0] == ("refused", "request")
    elif "pending" in expected:
        assert results[0][0]["pending"][0].startswith("mgr.")
    else:
        assert results[0][0]["exists"] is expected["exists"]


def run_module(monkeypatch, capsys, request, material, transport):
    """The real module entry point, its no_log masking included, over a fake transport."""
    from ansible.module_utils import basic
    from ansible_collections.bootwright.core.plugins.modules import ceph_cluster
    arguments = {"request": request, "operation": "observe", "digest": DIGEST, "material": material, "outputs": {}}
    monkeypatch.setattr(basic, "_ANSIBLE_ARGS", json.dumps({"ANSIBLE_MODULE_ARGS": arguments}).encode())
    monkeypatch.setattr(basic, "_ANSIBLE_PROFILE", "legacy")
    monkeypatch.setattr(ceph_core, "Transport", lambda value, lent, **keywords: transport.bound(value))
    with pytest.raises(SystemExit):
        ceph_cluster.main()
    return json.loads(capsys.readouterr().out.strip().splitlines()[-1])


def test_the_module_reads_and_replays_hosts_through_its_masked_material(monkeypatch, capsys):
    blocks = resources()
    paths = {"sshKey": "/run/bootwright/ssh-key", "knownHosts": "/run/bootwright/known-hosts"}
    reader = Transport()
    published = run_module(monkeypatch, capsys, blocks[0][0], dict(paths, reading="hosts"), reader)
    reading = published["evidence"]
    assert reader.calls == [(host["name"], "host_state") for host in blocks[0][0]["hosts"]]
    assert reading == ceph_core.host_reading(reading, DIGEST, blocks[0][0]) and "*" not in json.dumps(reading)
    text = json.dumps(reading)
    for request in [request for request, unused in blocks]:
        replay = Transport()
        result = run_module(monkeypatch, capsys, request, dict(paths, hosts=text), replay)
        assert replay.calls == [("node-1", "observe")] and result["evidence"]["specializations"] == [APPLIED]
        assert result["pending"] == [PENDING] and text not in json.dumps(result)
    oversized = run_module(monkeypatch, capsys, blocks[1][0], dict(paths, hosts=text + " " * (65537 - len(text))), Transport())
    assert oversized["failed"] is True and oversized["reason"] == "request"
    malformed = run_module(monkeypatch, capsys, blocks[1][0], dict(paths, hosts="{"), Transport())
    assert malformed["failed"] is True and malformed["reason"] == "request"
