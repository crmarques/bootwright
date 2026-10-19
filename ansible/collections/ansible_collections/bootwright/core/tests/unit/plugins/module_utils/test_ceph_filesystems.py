"""CephFS filesystems: one `fs new ... set` per filesystem, extra data pools, subvolume groups, adoption, refusals and readiness.

The scripted cluster below answers the upstream v20.2.1 shapes the worker reads; none of it qualifies the IBM image (B472):
- `fs dump` holds feature_flags.enable_multiple, standbys (each an MDS info with its name) and filesystems, each {mdsmap, id}
  (src/mds/FSMap.cc:131-176); an mdsmap holds fs_name, metadata_pool, data_pools, max_mds, standby_count_wanted,
  flags_state.allow_standby_replay and info gid_<n> -> {name, rank, state} (src/mds/MDSMap.cc:131-155, :190-273);
- `fs new <fs> <metadata> <data> set <key> <value>...` applies the pairs in the creating proposal, records the cephfs application
  key on both pools and sets pg_num_min 16, recovery_priority 5 and pg_autoscale_bias 4.0 on the metadata pool, and refuses a
  second filesystem while enable_multiple is false (src/mon/FSCommands.cc:216-221, :251-331);
- `fs add_data_pool` refuses a pool already attached, since its pool check runs before the attached no-op (FSCommands.cc:1077-1092);
- `fs subvolumegroup ls` lists {name} (src/pybind/mgr/volumes/fs/volume.py:50-60) and `info` reports uid, gid, the full st_mode,
  data_pool and bytes_quota, "infinite" for none (src/pybind/mgr/volumes/fs/operations/group.py:113-152); a first create sets the
  mode only through mkdir under the manager's umask and a repeat rewrites every attribute (group.py:236-276; fs/volume.py:1093-1111);
- MDS state names up:active and up:standby-replay come from ceph_mds_state_name (src/common/ceph_strings.cc, not downloaded).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_native_services as services_fixtures

core_request = core_fixtures.core_request

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
OWNER = "apply-123"
GIB = 1024 ** 3
FORBIDDEN = ("volume", "volumes", "--force", "--allow-dangerous-metadata-overlay", "--fscid", "--recover", "--normalization", "--casesensitive")
FORBIDDEN_PREFIXES = (("fs", "set"), ("fs", "flag"), ("fs", "rm"), ("fs", "fail"), ("fs", "subvolumegroup", "rm"), ("fs", "subvolumegroup", "resize"))
POOL_IDS = {"cephfs-metadata": 3, "cephfs-data": 4, "cephfs-extra": 5}
FS_NEW = ("fs", "new", "files", "cephfs-metadata", "cephfs-data", "set", "max_mds", "1", "allow_standby_replay", "false", "standby_count_wanted", "0")


def cluster(mds_count=None):
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": GIB}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [h["name"] for h in hosts]
    mds = {"service_type": "mds", "service_id": "files", "placement": {"hosts": names}}
    if mds_count is not None:
        mds["placement"]["count"] = mds_count
    return {"controller": "controller", "version": "ceph-core-v16", "action": "cluster", "cluster": "example", "owner": OWNER, "fsid": FSID,
            "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible",
            "packageRepository": "https://packages.example.test/native", "bootstrap": "node-1",
            "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key",
            "registryRef": "registry-key", "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"],
            "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}},
                         mds]}


def group_value(name="csi", **changes):
    group = {"gid": 0, "mode": "755", "name": name, "pool": "cephfs-data", "sizeBytes": 0, "uid": 0}
    group.update(changes)
    return group


def fs_value(**changes):
    filesystem = {"activeCount": 1, "dataPools": ["cephfs-data"], "defaultDataPool": "cephfs-data", "groups": [], "mdsService": "mds.files",
                  "metadataPool": "cephfs-metadata", "name": "files", "standbyCountWanted": 0, "standbyReplay": False}
    filesystem.update(changes)
    return filesystem


def mds_name(host, suffix="abcdef", service="files"):
    return service + "." + host + "." + suffix


class World:
    """A scripted cluster: three owned CephFS pools, the FSMap, the volumes module's groups; every effect logged."""

    def __init__(self, standbys=("node-1", "node-2", "node-3")):
        self.pools = [{"pool_name": name, "pool_id": identifier, "application_metadata": {"cephfs": {}}, "options": {}}
                      for name, identifier in POOL_IDS.items()]
        self.enable_multiple = True
        self.standbys = [{"name": mds_name(host), "rank": -1, "state": "up:standby"} for host in standbys]
        self.filesystems, self.groups, self.log, self.mutations, self.umask = [], {}, [], [], 0
        self.activate = True

    def pool(self, name):
        return next(entry for entry in self.pools if entry["pool_name"] == name)

    def entry(self, name="files"):
        return next((entry for entry in self.filesystems if entry["mdsmap"]["fs_name"] == name), None)

    def add_filesystem(self, name, metadata, data, fscid=7, max_mds=1, replay=False, wanted=0):
        mdsmap = {"fs_name": name, "metadata_pool": POOL_IDS.get(metadata, 90), "data_pools": [POOL_IDS.get(data, 91)], "max_mds": max_mds,
                  "standby_count_wanted": wanted, "flags_state": {"joinable": True, "allow_standby_replay": replay}, "info": {}, "epoch": 3}
        entry = {"mdsmap": mdsmap, "id": fscid}
        self.filesystems.append(entry)
        if metadata in POOL_IDS:
            self.pool(metadata)["application_metadata"] = {"cephfs": {"metadata": name}}
            self.pool(metadata)["options"] = {"pg_num_min": 16, "recovery_priority": 5, "pg_autoscale_bias": 4.0}
        if data in POOL_IDS:
            self.pool(data)["application_metadata"] = {"cephfs": {"data": name}}
        if self.activate:
            self.assign(entry)
        return entry

    def assign(self, entry):
        """The monitor promotes standbys: one up:active per rank, then one up:standby-replay follower per rank when allowed."""
        mdsmap = entry["mdsmap"]
        wanted = [(rank, "up:active") for rank in range(mdsmap["max_mds"])]
        if mdsmap["flags_state"]["allow_standby_replay"]:
            wanted += [(rank, "up:standby-replay") for rank in range(mdsmap["max_mds"])]
        for rank, state in wanted:
            if self.standbys:
                info = dict(self.standbys.pop(0), rank=rank, state=state)
                mdsmap["info"]["gid_" + str(4100 + len(mdsmap["info"]))] = info

    def dump(self):
        return {"epoch": 9, "default_fscid": 7, "feature_flags": {"enable_multiple": self.enable_multiple, "ever_enabled_multiple": True},
                "standbys": [dict(item, epoch=4) for item in self.standbys], "filesystems": self.filesystems, "compat": {}}

    def run(self, argv, data=b"", timeout=60):
        del data, timeout
        for word in FORBIDDEN:
            assert word not in argv, argv
        assert not any(item.startswith("--yes-i-really") for item in argv), argv
        assert argv[0] == "/usr/bin/ceph" and argv[argv.index("--format") + 1] == "json", argv
        command = tuple(argv[argv.index("--format") + 2:])
        assert not any(command[:len(prefix)] == prefix for prefix in FORBIDDEN_PREFIXES), command
        self.log.append(command)
        if command == ("osd", "pool", "ls", "detail"):
            return 0, json.dumps(self.pools).encode()
        if command == ("fs", "dump"):
            return 0, json.dumps(self.dump()).encode()
        if command[:2] == ("fs", "new"):
            self.mutations.append(command)
            name, metadata, data, pairs = command[2], command[3], command[4], dict(zip(command[6::2], command[7::2]))
            assert command[5] == "set" and set(pairs) == {"max_mds", "allow_standby_replay", "standby_count_wanted"}, command
            self.add_filesystem(name, metadata, data, fscid=7 + len(self.filesystems), max_mds=int(pairs["max_mds"]),
                                replay=pairs["allow_standby_replay"] == "true", wanted=int(pairs["standby_count_wanted"]))
            return 0, b""
        if command[:2] == ("fs", "add_data_pool"):
            self.mutations.append(command)
            entry, pool = self.entry(command[2]), self.pool(command[3])
            assert pool["pool_id"] not in entry["mdsmap"]["data_pools"], "the pool check refuses an attached pool"
            entry["mdsmap"]["data_pools"].append(pool["pool_id"])
            pool["application_metadata"] = {"cephfs": {"data": command[2]}}
            return 0, b""
        assert command[:3] == ("fs", "subvolumegroup", command[2]) and command[3] == "files", command
        return self.volumes(command)

    def volumes(self, command):
        if command[2] == "ls":
            return 0, json.dumps([{"name": name} for name in sorted(self.groups)]).encode()
        if command[2] == "info":
            return (0, json.dumps(self.groups[command[4]]).encode()) if command[4] in self.groups else (2, b"")
        assert command[2] == "create", command
        self.mutations.append(command)
        flags = dict(zip(command[5::2], command[6::2]))
        assert set(flags) <= {"--pool_layout", "--uid", "--gid", "--mode", "--size"}, command
        mode = int(flags.get("--mode", "755"), 8)
        if command[4] not in self.groups:
            mode &= ~self.umask
        size = int(flags["--size"]) if "--size" in flags else 0
        self.groups[command[4]] = {"uid": int(flags.get("--uid", 0)), "gid": int(flags.get("--gid", 0)), "mode": 0o40000 | mode,
                                   "data_pool": flags.get("--pool_layout", "cephfs-data"), "bytes_quota": size or "infinite", "bytes_used": 0,
                                   "bytes_pcent": "undefined", "created_at": "2026-10-15 00:00:00", "normalization": "none", "casesensitive": True}
        return 0, b""


class Markers:
    def __init__(self, world):
        self.records, self.writes, self.world, self.path = {}, [], world, "/private/cluster"

    @staticmethod
    def owned():
        return True

    def read(self, name="owner.json"):
        return self.records.get(name)

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = value
        self.writes.append(name)
        self.world.mutations.append(("write", name))


def worker(world, filesystem=None, mds_count=None, service_id="files"):
    request = cluster(mds_count)
    request["services"][-1]["service_id"] = service_id
    request.update(action="filesystem", filesystem=filesystem or fs_value())
    marks = Markers(world)
    for name, identifier in POOL_IDS.items():
        marks.records.update({"pool-" + name + ".json": {"name": name}, "pool-id-" + name + ".json": {"id": identifier}})
    return ceph_native_resources.ResourceWorker(request, "node-1", run=world.run, markers=marks), marks


def attempt(filesystem):
    return {"version": "ceph-filesystem-attempt-v1", "owner": OWNER, "fsid": FSID, "filesystem": filesystem}


def group_attempt(group):
    return {"version": "ceph-subvolume-group-attempt-v1", "owner": OWNER, "fsid": FSID, "filesystem": "files", "group": group}


def observe(native):
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), evidence


def effects(world):
    return [command for command in world.mutations if command[0] != "write"]


def test_fs_new_argv_uses_set_and_never_force():
    world = World()
    native, marks = worker(world)
    assert native.filesystem() == {"changed": True}
    assert effects(world) == [FS_NEW]
    order = world.mutations
    assert order.index(("write", "filesystem-files.json")) < order.index(("write", "filesystem-attempt.files.json")) < order.index(FS_NEW)
    assert order.index(FS_NEW) < order.index(("write", "filesystem-id.files.json"))
    assert marks.records["filesystem-attempt.files.json"] == attempt(fs_value()) and marks.records["filesystem-id.files.json"] == {"id": 7}
    ready, evidence = observe(native)
    assert ready is True and evidence["resource"] == {"id": "7", "path": ""} and evidence["owned"] and evidence["exists"]
    assert native.filesystem() == {"changed": False} and effects(world) == [FS_NEW]


@pytest.mark.parametrize("replay,wanted,argv", [(True, 1, ("true", "1")), (False, 2, ("false", "2"))])
def test_fs_new_sets_the_frozen_mds_settings(replay, wanted, argv):
    world = World()
    filesystem = fs_value(standbyReplay=replay, standbyCountWanted=wanted)
    native = worker(world, filesystem)[0]
    native.filesystem()
    assert effects(world) == [FS_NEW[:9] + (argv[0], "standby_count_wanted", argv[1])]


@pytest.mark.parametrize("case,reason", [("no-marker", "foreign-filesystem"), ("no-attempt", "foreign-filesystem"),
                                         ("other-attempt", "foreign-filesystem"), ("other-pools", "filesystem-identity"), ("adopt", None)])
def test_fs_new_refuses_when_name_exists_unrecorded(case, reason):
    world = World()
    native, marks = worker(world)
    if case == "other-pools":
        world.add_filesystem("files", "cephfs-metadata", "cephfs-extra")
    else:
        world.add_filesystem("files", "cephfs-metadata", "cephfs-data")
    if case != "no-marker":
        marks.records["filesystem-files.json"] = fs_value()
    if case in ("other-attempt", "other-pools", "adopt"):
        marks.records["filesystem-attempt.files.json"] = attempt(fs_value(activeCount=2) if case == "other-attempt" else fs_value())
    before = len(marks.writes)
    if reason is None:
        assert native.filesystem() == {"changed": True}
        assert marks.records["filesystem-id.files.json"] == {"id": 7} and effects(world) == []
        assert observe(native)[0] is True
        return
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.filesystem()
    assert "filesystem-id.files.json" not in marks.records and effects(world) == []
    if case == "no-marker":
        assert len(marks.writes) == before == 0


def test_fs_refuses_a_pool_another_filesystem_holds_before_any_write():
    world = World()
    world.pool("cephfs-data")["application_metadata"] = {"cephfs": {"data": "other"}}
    native, marks = worker(world)
    with pytest.raises(ceph_native.Refusal, match="^foreign-filesystem$"):
        native.filesystem()
    assert marks.writes == [] and effects(world) == []


def test_fs_add_data_pool_skips_observed():
    world = World()
    native = worker(world, fs_value(dataPools=["cephfs-data", "cephfs-extra"]))[0]
    assert native.filesystem() == {"changed": True}
    assert effects(world) == [FS_NEW, ("fs", "add_data_pool", "files", "cephfs-extra")]
    assert world.entry()["mdsmap"]["data_pools"] == [4, 5] and observe(native)[0] is True
    assert native.filesystem() == {"changed": False} and len(effects(world)) == 2


def test_fs_never_uses_volume_interface():
    world = World()
    filesystem = fs_value(dataPools=["cephfs-data", "cephfs-extra"], groups=[group_value("apps", pool="cephfs-extra", sizeBytes=GIB), group_value()])
    native = worker(world, filesystem)[0]
    native.filesystem()
    native.filesystem_ranks()
    native.filesystem_groups()
    assert observe(native)[0] is True
    assert world.log and not any("volume" in command for command in world.log)


@pytest.mark.parametrize("multiple", [False, True])
def test_fs_new_second_filesystem_requires_enable_multiple(multiple):
    world = World()
    world.add_filesystem("other", "other-metadata", "other-data")
    world.enable_multiple = multiple
    native = worker(world)[0]
    if not multiple:
        with pytest.raises(ceph_native.Refusal, match="^filesystem-multiple-disabled$"):
            native.filesystem()
        assert effects(world) == []
        return
    assert native.filesystem() == {"changed": True} and effects(world) == [FS_NEW]


@pytest.mark.parametrize("filesystem,standbys,refused", [
    (fs_value(standbyCountWanted=1), ("node-1",), True),
    (fs_value(standbyCountWanted=1), ("node-1", "node-2"), False),
    (fs_value(standbyReplay=True), ("node-1",), True),
    (fs_value(activeCount=2), ("node-1",), True),
])
def test_fs_mds_coverage_refuses_before_fs_new(filesystem, standbys, refused):
    world = World(standbys)
    world.standbys.append({"name": mds_name("node-3", service="other"), "rank": -1, "state": "up:standby"})
    native = worker(world, filesystem)[0]
    if refused:
        with pytest.raises(ceph_native.Refusal, match="^mds-coverage$"):
            native.filesystem()
        assert effects(world) == []
        return
    native.filesystem()
    assert effects(world)[0][:2] == ("fs", "new")


@pytest.mark.parametrize("change", ["replay-state", "foreign-active", "missing-follower", "follower-other-rank", "max-mds", "wanted", "pool-order",
                                    "flag", None])
def test_fs_wait_ready_requires_active_ranks_and_replay(change):
    world = World()
    world.activate = False
    filesystem = fs_value(standbyReplay=True, standbyCountWanted=1, dataPools=["cephfs-data", "cephfs-extra"])
    native, marks = worker(world, filesystem)
    entry = world.add_filesystem("files", "cephfs-metadata", "cephfs-data", replay=True, wanted=1)
    entry["mdsmap"]["data_pools"].append(5)
    world.standbys = [{"name": mds_name("node-3"), "rank": -1, "state": "up:standby"}]
    infos = {"gid_1": {"name": mds_name("node-1"), "rank": 0, "state": "up:active"},
             "gid_2": {"name": mds_name("node-2"), "rank": 0, "state": "up:standby-replay"}}
    if change == "replay-state":
        infos["gid_1"]["state"] = "up:replay"
    elif change == "foreign-active":
        infos["gid_1"]["name"] = mds_name("node-1", service="other")
    elif change == "missing-follower":
        del infos["gid_2"]
        world.standbys.append({"name": mds_name("node-2"), "rank": -1, "state": "up:standby"})
    elif change == "follower-other-rank":
        infos["gid_2"]["rank"] = 1
    elif change == "max-mds":
        entry["mdsmap"]["max_mds"] = 2
    elif change == "wanted":
        entry["mdsmap"]["standby_count_wanted"] = 0
    elif change == "pool-order":
        entry["mdsmap"]["data_pools"] = [5, 4]
    elif change == "flag":
        entry["mdsmap"]["flags_state"]["allow_standby_replay"] = False
    entry["mdsmap"]["info"] = infos
    marks.records.update({"filesystem-files.json": filesystem, "filesystem-id.files.json": {"id": 7}})
    ready, evidence = observe(native)
    assert ready is (change is None) and evidence["resource"] == {"id": "7", "path": ""}
    assert native.filesystem_ranks() == {"active": change not in ("replay-state", "foreign-active")}


@pytest.mark.parametrize("standbys, ready", [([], False), (["other"], False), (["files"], True), (["files", "other"], True)])
def test_fs_wait_ready_counts_bound_standbys_against_wanted(standbys, ready):
    world = World()
    world.activate = False
    filesystem = fs_value(standbyCountWanted=1)
    native, marks = worker(world, filesystem)
    entry = world.add_filesystem("files", "cephfs-metadata", "cephfs-data", wanted=1)
    world.standbys = [{"name": mds_name("node-" + str(index + 2), service=service), "rank": -1, "state": "up:standby"}
                      for index, service in enumerate(standbys)]
    entry["mdsmap"]["info"] = {"gid_1": {"name": mds_name("node-1"), "rank": 0, "state": "up:active"}}
    marks.records.update({"filesystem-files.json": filesystem, "filesystem-id.files.json": {"id": 7}})
    assert observe(native)[0] is ready
    assert native.filesystem_ranks() == {"active": True}


def test_fs_observe_lost_reply_is_unfinished_and_identity_changes_refuse():
    world = World()
    native, marks = worker(world)
    world.add_filesystem("files", "cephfs-metadata", "cephfs-data")
    marks.records.update({"filesystem-files.json": fs_value(), "filesystem-attempt.files.json": attempt(fs_value())})
    ready, evidence = observe(native)
    assert ready is False and evidence["resource"] == {"id": "", "path": ""} and evidence["owned"] and evidence["exists"]
    marks.records["filesystem-id.files.json"] = {"id": 8}
    for call in (observe, lambda value: value.filesystem(), lambda value: value.filesystem_ranks()):
        with pytest.raises(ceph_native.Refusal, match="^filesystem-identity$"):
            call(native)
    world.filesystems.clear()
    for call in (observe, lambda value: value.filesystem()):
        with pytest.raises(ceph_native.Refusal, match="^filesystem-identity$"):
            call(native)
    assert effects(world) == []


def test_fs_declared_change_refuses_identity_and_unowned_entry_refuses_foreign():
    world = World()
    native, marks = worker(world, fs_value(activeCount=2))
    marks.records["filesystem-files.json"] = fs_value()
    for call in (observe, lambda value: value.filesystem()):
        with pytest.raises(ceph_native.Refusal, match="^filesystem-identity$"):
            call(native)
    del marks.records["filesystem-files.json"]
    world.add_filesystem("files", "cephfs-metadata", "cephfs-data")
    with pytest.raises(ceph_native.Refusal, match="^foreign-filesystem$"):
        observe(native)
    assert effects(world) == []


def test_fs_ready_does_not_require_mds_at_cluster_block(core_request):
    services_fixtures.with_mds(core_request)
    native, calls = core_fixtures.worker(core_request, services_fixtures.mds_answers(core_request))
    result = native.observe()
    assert result["ready"] is True
    assert all("fs" not in argv for argv in calls)


def test_group_create_only_after_absence_proof():
    world = World()
    world.groups["csi"] = {"uid": 0, "gid": 0, "mode": 0o40755, "data_pool": "cephfs-data", "bytes_quota": "infinite"}
    native, marks = worker(world, fs_value(groups=[group_value()]))
    native.filesystem()
    with pytest.raises(ceph_native.Refusal, match="^foreign-subvolume-group$"):
        native.filesystem_groups()
    assert "group.files.csi.json" not in marks.records and effects(world) == [FS_NEW]
    with pytest.raises(ceph_native.Refusal, match="^foreign-subvolume-group$"):
        observe(native)
    marks.records["group.files.csi.json"] = group_value()
    with pytest.raises(ceph_native.Refusal, match="^foreign-subvolume-group$"):
        native.filesystem_groups()
    marks.records["group-attempt.files.csi.json"] = group_attempt(group_value())
    assert native.filesystem_groups() == {"changed": False} and effects(world) == [FS_NEW]
    assert observe(native)[0] is True


@pytest.mark.parametrize("group,argv", [
    (group_value(), ("--pool_layout", "cephfs-data", "--uid", "0", "--gid", "0", "--mode", "755")),
    (group_value(uid=1000, gid=2000, mode="770", sizeBytes=GIB, pool="cephfs-extra"),
     ("--pool_layout", "cephfs-extra", "--uid", "1000", "--gid", "2000", "--mode", "770", "--size", str(GIB))),
])
def test_group_create_passes_all_values_explicitly(group, argv):
    world = World()
    native, marks = worker(world, fs_value(dataPools=["cephfs-data", "cephfs-extra"], groups=[group]))
    native.filesystem()
    assert native.filesystem_groups() == {"changed": True}
    creates = [command for command in effects(world) if command[:3] == ("fs", "subvolumegroup", "create")]
    assert creates == [("fs", "subvolumegroup", "create", "files", group["name"]) + argv]
    order = world.mutations
    assert order.index(("write", "group.files.csi.json")) < order.index(("write", "group-attempt.files.csi.json")) < order.index(creates[0])
    assert marks.records["group-attempt.files.csi.json"] == group_attempt(group)
    assert observe(native)[0] is True
    assert native.filesystem_groups() == {"changed": False} and len(effects(world)) == len(creates) + 2


def test_group_first_create_under_umask_is_created_once_more_then_ready():
    world = World()
    world.umask = 0o022
    native = worker(world, fs_value(groups=[group_value(mode="777")]))[0]
    native.filesystem()
    assert native.filesystem_groups() == {"changed": True}
    creates = [command for command in effects(world) if command[:3] == ("fs", "subvolumegroup", "create")]
    assert len(creates) == 2 and creates[0] == creates[1] and world.groups["csi"]["mode"] == 0o40777
    assert observe(native)[0] is True
    assert native.filesystem_groups() == {"changed": False}


@pytest.mark.parametrize("drift", [{"uid": 5}, {"gid": 5}, {"mode": 0o40750}, {"data_pool": "cephfs-extra"}, {"bytes_quota": GIB}])
def test_group_attribute_drift_is_not_ready_never_a_refusal(drift):
    world = World()
    native = worker(world, fs_value(groups=[group_value()]))[0]
    native.filesystem()
    native.filesystem_groups()
    world.groups["csi"].update(drift)
    world.groups["undeclared"] = {"uid": 9, "gid": 9, "mode": 0o40700, "data_pool": "x", "bytes_quota": 1}
    assert observe(native)[0] is False


def test_groups_are_read_only_once_ranks_are_active():
    world = World(standbys=())
    native = worker(world, fs_value(groups=[group_value()]))[0]
    world.standbys = [{"name": mds_name("node-1"), "rank": -1, "state": "up:standby"}]
    native.filesystem()
    world.entry()["mdsmap"]["info"] = {}
    world.log.clear()
    assert observe(native)[0] is False and native.filesystem_ranks() == {"active": False}
    assert not any(command[:2] == ("fs", "subvolumegroup") for command in world.log)


@pytest.mark.parametrize("case,reason", [("recreated", "filesystem-identity"), ("other-id", "filesystem-identity"), ("gone", "filesystem-identity"),
                                         ("unmarked", "foreign-filesystem")])
def test_groups_prove_the_owned_filesystem_before_any_group_command(case, reason):
    world = World(standbys=("node-1", "node-2"))
    native, marks = worker(world, fs_value(groups=[group_value()]))
    native.filesystem()
    if case == "recreated":
        world.filesystems.clear()
        world.standbys = [{"name": mds_name("node-3"), "rank": -1, "state": "up:standby"}]
        world.add_filesystem("files", "cephfs-metadata", "cephfs-data", fscid=9)
    elif case == "other-id":
        marks.records["filesystem-id.files.json"] = {"id": 8}
    elif case == "gone":
        world.filesystems.clear()
    else:
        del marks.records["filesystem-files.json"]
    world.log.clear()
    before = list(marks.writes)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.filesystem_groups()
    assert not any(command[:2] == ("fs", "subvolumegroup") for command in world.log)
    assert marks.writes == before and effects(world) == [FS_NEW] and world.groups == {}


@pytest.mark.parametrize("change", ["name", "reserved", "unsorted", "mode", "pool", "service", "coverage", "default", "dot", "metadata-in-data"])
def test_validation_refuses_a_filesystem_go_never_freezes(change):
    filesystem, count, service = fs_value(groups=[group_value("a"), group_value("b")]), None, "files"
    if change == "name":
        filesystem.update(name="f" * 48, mdsService="mds." + "f" * 48)
        service = "f" * 48
    elif change == "reserved":
        filesystem["groups"] = [group_value("_nogroup")]
    elif change == "unsorted":
        filesystem["groups"].reverse()
    elif change == "mode":
        filesystem["groups"][0]["mode"] = "0755"
    elif change == "pool":
        filesystem["groups"][0]["pool"] = "cephfs-extra"
    elif change == "service":
        filesystem["mdsService"] = "mds.other"
    elif change == "coverage":
        filesystem["standbyCountWanted"], count = 1, 1
    elif change == "default":
        filesystem["defaultDataPool"] = "cephfs-extra"
    elif change == "dot":
        filesystem.update(name="fi.les", mdsService="mds.fi.les")
        service = "fi.les"
    else:
        filesystem["dataPools"] = ["cephfs-data", "cephfs-metadata"]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        worker(World(), filesystem, mds_count=count, service_id=service)


class Transport:
    def __init__(self, ranks):
        self.ranks, self.calls = list(ranks), []

    def call(self, host, phase):
        self.calls.append((host["name"], phase))
        if phase == "filesystem_ranks":
            return {"active": self.ranks.pop(0)}
        return {"changed": phase == "filesystem_groups"}


@pytest.mark.parametrize("groups,ranks,outcome", [
    ([group_value()], [False, False, True], ["filesystem", "filesystem_ranks", "filesystem_ranks", "filesystem_ranks", "filesystem_groups"]),
    ([], [], ["filesystem"]),
    ([group_value()], [False] * 400, "not-ready"),
])
def test_fs_apply_waits_for_ranks_before_groups(groups, ranks, outcome):
    request = dict(cluster(), action="filesystem", filesystem=fs_value(groups=groups))
    bootstrap = request["hosts"][0]
    transport, clock, pauses = Transport(ranks), [0.0], []

    def pause(seconds):
        pauses.append(seconds)
        clock[0] += seconds

    if outcome == "not-ready":
        with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
            ceph_core.apply_filesystem(request, bootstrap, transport, lambda: clock[0], pause)
        assert clock[0] == ceph_core.READINESS_TIMEOUT and "filesystem_groups" not in [phase for unused, phase in transport.calls]
        return
    changed = ceph_core.apply_filesystem(request, bootstrap, transport, lambda: clock[0], pause)
    assert [phase for unused, phase in transport.calls] == outcome and changed is bool(groups)
    assert pauses == [ceph_core.READINESS_INTERVAL] * (len(outcome) - 3 if groups else 0)


@pytest.mark.parametrize("answer", [{"active": 1}, {"active": True, "extra": 1}, []])
def test_fs_apply_refuses_a_malformed_rank_answer(answer):
    request = dict(cluster(), action="filesystem", filesystem=fs_value(groups=[group_value()]))
    transport = Transport([])
    transport.call = lambda host, phase: answer if phase == "filesystem_ranks" else {"changed": False}
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.apply_filesystem(request, request["hosts"][0], transport, lambda: 0.0, lambda seconds: None)


def core_filesystem_request(core_request, groups=()):
    services_fixtures.with_mds(core_request)
    return dict(core_request, action="filesystem", filesystem=fs_value(groups=list(groups)))


def test_fs_evidence_admits_only_a_decimal_filesystem_id(core_request):
    request = core_filesystem_request(core_request)
    value = dict(core_fixtures.observation(request), request="d" * 64, resource={"id": "7", "path": ""})
    assert ceph_core.evidence(value, "d" * 64, request) is value
    for refused in ("07", "0x7", "abc", "-1"):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(dict(value, resource={"id": refused, "path": ""}), "d" * 64, request)


@pytest.mark.parametrize("groups", [(), (group_value(),)])
def test_fs_apply_calls_the_filesystem_phases(core_request, monkeypatch, groups):
    request = core_filesystem_request(core_request, groups)
    calls = []

    class CoreTransport:
        def call(self, host, phase, timeout=None):
            del timeout
            calls.append(phase)
            if phase == "observe":
                done = "filesystem" in calls
                return dict(core_fixtures.observation(request, ready=done), request="", removed=False, exists=done, partial=not done,
                            resource={"id": "7" if done else "", "path": ""})
            if phase == "filesystem_ranks":
                return {"active": True}
            if phase == "host_state":
                return {"name": host["name"], "owned": True, "exists": True}
            return {"changed": phase == "filesystem"}

    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    result = ceph_core.execute(request, "apply", "d" * 64, CoreTransport())
    assert result["changed"] and result["evidence"]["resource"] == {"id": "7", "path": ""}
    phases = [phase for phase in calls if phase.startswith("filesystem")]
    assert phases == (["filesystem", "filesystem_ranks", "filesystem_groups"] if groups else ["filesystem"])
    assert calls.index("filesystem") > calls.index("observe")
