"""Placement policies and full pools: attempt records, adoption, normalized profiles and every frozen attribute.

The scripted monitor below answers the upstream v20.2.1 shapes the worker reads; none of it qualifies the IBM image (B472):
- `osd crush rule ls|dump` and `osd crush rule create-replicated` (src/mon/MonCommands.h:633, :638-641, :774-780;
  src/crush/CrushWrapper.cc:2342-2367 and :3742-3816 for the dumped steps);
- `osd erasure-code-profile ls|get|set` (MonCommands.h:868-885). The stored profile is the normalized one
  (src/mon/OSDMonitor.cc:11676-11686): every plugin writes crush-num-failure-domains and crush-osds-per-failure-domain,
  and crush-device-class when omitted (src/erasure-code/ErasureCode.cc:44-66); jerasure reed_sol_van also writes w=8 and
  jerasure-per-chunk-alignment=false (src/erasure-code/jerasure/ErasureCodeJerasure.cc:61-77, :410-424; ErasureCodeJerasure.h:124-135);
- an erasure-coded pool create derives rule <pool> with set_chooseleaf_tries 5, set_choose_tries 100, take, chooseleaf_indep, emit
  (src/erasure-code/ErasureCode.cc:70-84; OSDMonitor.cc:13686-13694); the CLI exits with the errno, ERANGE 34 from check_pg_num
  (OSDMonitor.cc:8006-8056; src/ceph.in:1306-1310);
- `osd pool ls detail` keys (src/osd/osd_types.cc:1568-1653) and `osd tree` nodes with children and device_class.
"""

from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_resources

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
OWNER = "apply-123"
FORBIDDEN = ("--force", "--yes-i-really-mean-it", "--yes-i-really-really-mean-it", "rm", "delete", "allow_ec_optimizations")


def cluster(count=6):
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, count + 1)]
    names = [h["name"] for h in hosts]
    return {"controller": "controller", "version": "ceph-core-v8", "action": "cluster", "cluster": "example", "owner": OWNER, "fsid": FSID,
            "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible",
            "packageRepository": "https://packages.example.test/native", "bootstrap": "node-1",
            "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "native", "clusterKeyRef": "cluster-key",
            "registryRef": "registry-key", "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"],
            "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names[:3]}}, {"service_type": "mgr", "placement": {"hosts": names[:3], "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


def pool_value(kind="replicated", role="rbd", plugin="isa", device_class="", **changes):
    pool = {"application": ceph_native_resources.APPLICATIONS[role],
            "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
            "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}, "crushRule": "replicated_rule",
            "minSize": 2, "name": "volumes", "overwrites": False, "pgNum": 32, "quota": {"maxBytes": 0, "maxObjects": 0}, "role": role, "size": 2,
            "type": kind}
    if kind == "erasure":
        defaults = {"crush-num-failure-domains": "0", "crush-osds-per-failure-domain": "0"}
        if not device_class:
            defaults["crush-device-class"] = ""
        if plugin == "jerasure":
            defaults.update({"w": "8", "jerasure-per-chunk-alignment": "false"})
        pool.update(crushRule="volumes", size=6, minSize=5, overwrites=role in ("rbd", "cephfs-data"),
                    erasure={"codingChunks": 2, "dataChunks": 4, "deviceClass": device_class, "failureDomain": "host", "plugin": plugin, "profile": "volumes",
                             "profileDefaults": defaults, "root": "default", "stripeUnit": 4096, "technique": "reed_sol_van"})
    pool.update(changes)
    return pool


def policy_value(device_class="ssd", size=2, rule="ssd-hosts"):
    return {"deviceClass": device_class, "failureDomain": "host", "name": "fast", "root": "default", "rule": rule, "size": size}


def rule(identifier, name, kind=1, device_class="", chooseleaf=None):
    take = {"op": "take", "item": -1 - identifier, "item_name": ceph_native_resources.take_name(device_class)}
    if kind == 3:
        return {"rule_id": identifier, "rule_name": name, "type": 3,
                "steps": [{"op": "set_chooseleaf_tries", "num": 5}, {"op": "set_choose_tries", "num": 100}, take,
                          {"op": chooseleaf or "chooseleaf_indep", "num": 0, "type": "host"}, {"op": "emit"}]}
    return {"rule_id": identifier, "rule_name": name, "type": 1,
            "steps": [take, {"op": chooseleaf or "chooseleaf_firstn", "num": 0, "type": "host"}, {"op": "emit"}]}


def tree(classes):
    """One root `default` over a host per entry, each with one OSD per listed class."""
    nodes, osd = [{"id": -1, "name": "default", "type": "root", "children": []}], 0
    for index, host_classes in enumerate(classes):
        host = {"id": -2 - index, "name": "node-" + str(index + 1), "type": "host", "children": []}
        nodes[0]["children"].append(host["id"])
        nodes.append(host)
        for device_class in host_classes:
            host["children"].append(osd)
            nodes.append({"id": osd, "name": "osd." + str(osd), "type": "osd", "device_class": device_class})
            osd += 1
    return {"nodes": nodes, "stray": []}


def normalized(pairs):
    stored = dict(pairs)
    stored.setdefault("crush-device-class", "")
    stored.update({"crush-num-failure-domains": "0", "crush-osds-per-failure-domain": "0"})
    if stored["plugin"] == "jerasure":
        stored.update({"w": "8", "jerasure-per-chunk-alignment": "false"})
    return stored


class World:
    """A scripted monitor: reads answer from its tables, every other command is a logged effect it applies."""

    def __init__(self, classes=None):
        self.pools, self.log, self.mutations, self.create_code = [], [], [], 0
        self.rules = {"replicated_rule": rule(0, "replicated_rule")}
        self.profiles = {"default": normalized([("k", "2"), ("m", "2"), ("plugin", "isa"), ("technique", "reed_sol_van")])}
        self.tree = tree(classes or [["hdd"]] * 6)

    def run(self, argv, data=b"", timeout=60):
        del data, timeout
        for word in FORBIDDEN:
            assert word not in argv, argv
        if "--format" in argv:
            command = tuple(argv[argv.index("--format") + 2:])
        elif argv[0] == "/usr/bin/ceph":
            command = tuple(argv[argv.index("--keyring") + 2:])
        else:
            command = tuple(argv)
        self.log.append(command)
        reads = {("osd", "pool", "ls", "detail"): self.pools, ("osd", "crush", "rule", "ls"): list(self.rules),
                 ("osd", "erasure-code-profile", "ls"): list(self.profiles), ("osd", "tree"): self.tree}
        if command in reads:
            return 0, json.dumps(reads[command]).encode()
        if command[:4] == ("osd", "crush", "rule", "dump"):
            return (0, json.dumps(self.rules[command[4]]).encode()) if command[4] in self.rules else (2, b"")
        if command[:3] == ("osd", "erasure-code-profile", "get"):
            return 0, json.dumps(self.profiles[command[3]]).encode()
        return self.effect(command)

    def effect(self, command):
        self.mutations.append(command)
        if command[:4] == ("osd", "crush", "rule", "create-replicated"):
            self.rules.setdefault(command[4], rule(len(self.rules) + 2, command[4], 1, command[7] if len(command) > 7 else ""))
        elif command[:3] == ("osd", "erasure-code-profile", "set"):
            self.profiles[command[3]] = normalized(item.split("=", 1) for item in command[4:])
        elif command[:3] == ("osd", "pool", "create"):
            if self.create_code:
                return self.create_code, b""
            self.create(command)
        elif command[:3] == ("osd", "pool", "set") or command[:3] == ("osd", "pool", "set-quota"):
            self.configure(command)
        elif command[:4] == ("osd", "pool", "application", "enable"):
            self.entry(command[4])["application_metadata"].setdefault(command[5], {})
        elif command[0] == "/usr/bin/rbd":
            self.entry(command[-1])["application_metadata"].setdefault("rbd", {})
        return 0, b""

    def entry(self, name):
        return next(entry for entry in self.pools if entry["pool_name"] == name)

    def create(self, command):
        name, pgs, kind, target = command[3], int(command[4]), command[6], command[7]
        flags = list(command[8:])
        options = {}
        for flag, option in (("--pg_num_min", "pg_num_min"), ("--pg_num_max", "pg_num_max"), ("--target_size_bytes", "target_size_bytes")):
            if flag in flags:
                options[option] = int(flags[flags.index(flag) + 1])
        if "--target_size_ratio" in flags:
            options["target_size_ratio"] = float(flags[flags.index("--target_size_ratio") + 1])
        if kind == "erasure":
            profile = self.profiles[target]
            self.rules.setdefault(name, rule(len(self.rules) + 2, name, 3, profile["crush-device-class"]))
            size, minimum, width = int(profile["k"]) + int(profile["m"]), int(profile["k"]) + 1, int(profile["k"]) * int(profile["stripe_unit"])
        else:
            size, minimum, width = 3, 2, 0
        crush_rule = self.rules[name if kind == "erasure" else target]["rule_id"]
        self.pools.append({"pool_name": name, "pool_id": 8, "type": 3 if kind == "erasure" else 1, "crush_rule": crush_rule,
                           "pg_num": pgs, "pg_autoscale_mode": flags[flags.index("--autoscale_mode") + 1], "size": size, "min_size": minimum,
                           "flags_names": "hashpspool" + (",bulk" if "--bulk" in flags else ""), "quota_max_bytes": 0, "quota_max_objects": 0,
                           "erasure_code_profile": name if kind == "erasure" else "", "stripe_width": width, "options": options, "application_metadata": {}})

    def configure(self, command):
        entry, key, value = self.entry(command[3]), command[4], command[5]
        if command[2] == "set-quota":
            entry["quota_" + key] = int(value)
        elif key in ("size", "min_size"):
            entry[key] = int(value)
        elif key == "allow_ec_overwrites":
            entry["flags_names"] += ",ec_overwrites"
        elif key.endswith("_ratio"):
            entry["options"][key] = float(value)
        elif key.endswith("_size"):
            entry["options"][key] = int(value)
        else:
            entry["options"][key] = value

    def effects(self):
        """Every marker write and native effect, in order; reads excluded."""
        return list(self.mutations)


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
        self.world.log.append(("write", name))
        self.world.mutations.append(("write", name))


def resource(world, action, value, count=6):
    request = cluster(count)
    request.update({"action": action, action: value})
    marks = Markers(world)
    return ceph_native_resources.ResourceWorker(request, "node-1", run=world.run, markers=marks), marks


def pool_attempt(native):
    return {"version": "ceph-pool-attempt-v2", "owner": OWNER, "fsid": FSID, "pool": native.request["pool"]}


def policy_attempt(native):
    return {"version": "ceph-policy-attempt-v1", "owner": OWNER, "fsid": FSID, "policy": native.request["policy"]}


def ready(native):
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), evidence


PROFILE_SET = ("osd", "erasure-code-profile", "set", "volumes", "k=4", "m=2", "plugin=isa", "technique=reed_sol_van", "crush-failure-domain=host",
               "crush-root=default", "stripe_unit=4096")


def test_pool_create_erasure_sets_profile_then_pool_without_rule():
    world = World()
    native, marks = resource(world, "pool", pool_value("erasure"))
    assert native.pool() == {"changed": True}
    create = ("osd", "pool", "create", "volumes", "32", "32", "erasure", "volumes", "--autoscale_mode", "on")
    log = world.log
    assert log.index(("write", "pool-attempt-volumes.json")) < log.index(PROFILE_SET) < log.index(create) < log.index(("write", "pool-id-volumes.json"))
    assert marks.records["pool-attempt-volumes.json"] == pool_attempt(native)
    assert ("osd", "pool", "set", "volumes", "allow_ec_overwrites", "true") in log
    assert ("osd", "pool", "application", "enable", "volumes", "rbd") in log
    assert not any(entry[0] == "/usr/bin/rbd" for entry in log)
    assert ready(native)[0] is True


@pytest.mark.parametrize("foreign", ["profile", "rule"])
def test_pool_refuses_foreign_profile_and_rule(foreign):
    world = World()
    native, marks = resource(world, "pool", pool_value("erasure"))
    if foreign == "profile":
        world.profiles["volumes"] = normalized(item.split("=", 1) for item in PROFILE_SET[4:])
    else:
        world.rules["volumes"] = rule(7, "volumes", 3)
    with pytest.raises(ceph_native.Refusal, match="^foreign-" + foreign + "$"):
        native.pool()
    assert "pool-attempt-volumes.json" not in marks.records and world.effects() == [("write", "pool-volumes.json")]


@pytest.mark.parametrize("case", ["recorded", "unrecorded", "firstn"])
def test_pool_retry_after_lost_reply_adopts_only_with_attempt(case):
    world = World()
    native, marks = resource(world, "pool", pool_value("erasure"))
    world.profiles["volumes"] = normalized(item.split("=", 1) for item in PROFILE_SET[4:])
    world.rules["volumes"] = rule(7, "volumes", 3, chooseleaf="chooseleaf_firstn" if case == "firstn" else None)
    if case != "unrecorded":
        marks.records["pool-volumes.json"] = native.request["pool"]
        marks.records["pool-attempt-volumes.json"] = pool_attempt(native)
    if case == "recorded":
        assert native.pool() == {"changed": True}
        assert PROFILE_SET not in world.log
        assert sum(entry[:3] == ("osd", "pool", "create") for entry in world.log) == 1
        assert marks.records["pool-id-volumes.json"] == {"id": 8} and world.entry("volumes")["crush_rule"] == 7
        return
    with pytest.raises(ceph_native.Refusal, match="^foreign-(profile|rule)$" if case == "unrecorded" else "^foreign-rule$"):
        native.pool()
    assert not any(entry[:3] == ("osd", "pool", "create") for entry in world.log) and PROFILE_SET not in world.log


@pytest.mark.parametrize("plugin,extra,accepted", [
    ("isa", {}, True), ("jerasure", {}, True), ("isa", {"foo": "1"}, False), ("isa", {"crush-num-failure-domains": "2"}, False),
    ("jerasure", {"w": "16"}, False),
])
def test_pool_erasure_profile_accepts_plugin_added_keys(plugin, extra, accepted):
    world = World()
    native, marks = resource(world, "pool", pool_value("erasure", plugin=plugin))
    passed = [item.split("=", 1) for item in PROFILE_SET[4:]]
    passed[2] = ["plugin", plugin]
    world.profiles["volumes"] = dict(normalized(passed), **extra)
    if plugin == "jerasure":
        assert {"w": "8", "jerasure-per-chunk-alignment": "false"}.items() <= normalized(passed).items()
    marks.records["pool-volumes.json"] = native.request["pool"]
    marks.records["pool-attempt-volumes.json"] = pool_attempt(native)
    if accepted:
        assert native.pool() == {"changed": True}
        assert ready(native)[0] is True
        return
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.pool()
    assert not any(entry[:3] == ("osd", "pool", "create") for entry in world.log)


@pytest.mark.parametrize("code,reason", [(34, "pg-budget"), (22, "native-command"), (1, "native-command")])
def test_pool_create_erange_maps_to_pg_budget(code, reason):
    world = World()
    world.create_code = code
    native, marks = resource(world, "pool", pool_value())
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.pool()
    assert "pool-id-volumes.json" not in marks.records and marks.records["pool-attempt-volumes.json"] == pool_attempt(native)


@pytest.mark.parametrize("kind,role,overwrites", [
    ("erasure", "rbd", True), ("erasure", "cephfs-data", True), ("erasure", "rgw", False), ("replicated", "rbd", False), ("replicated", "cephfs-data", False),
])
def test_pool_overwrites_only_for_rbd_and_cephfs_data_ec(kind, role, overwrites):
    world = World()
    native, unused = resource(world, "pool", pool_value(kind, role=role))
    assert native.request["pool"]["overwrites"] is overwrites
    native.pool()
    assert (("osd", "pool", "set", "volumes", "allow_ec_overwrites", "true") in world.log) is overwrites
    assert ready(native)[0] is True
    if kind == "erasure" and not overwrites:
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            resource(World(), "pool", pool_value(kind, role=role, overwrites=True))
        world.entry("volumes")["flags_names"] += ",ec_overwrites"
        assert ready(native)[0] is False


def setters(log):
    return [entry for entry in log if entry[:3] in (("osd", "pool", "set"), ("osd", "pool", "set-quota"))]


def native_effects(world):
    """The native effects so far; marker writes excluded."""
    return [entry for entry in world.mutations if entry[0] != "write"]


def test_pool_quota_and_compression_setters_idempotent():
    world = World()
    compression = {"algorithm": "zstd", "maxBlob": 0, "minBlob": 0, "mode": "passive", "requiredRatio": ""}
    native, marks = resource(world, "pool", pool_value(compression=compression, quota={"maxBytes": 1073741824, "maxObjects": 0}))
    assert native.pool() == {"changed": True}
    first = setters(world.log)
    assert first == [("osd", "pool", "set", "volumes", "size", "2"), ("osd", "pool", "set-quota", "volumes", "max_bytes", "1073741824"),
                     ("osd", "pool", "set", "volumes", "compression_mode", "passive"), ("osd", "pool", "set", "volumes", "compression_algorithm", "zstd")]
    for field in ("size", "min_size", "quota_max_bytes", "quota_max_objects", "compression_mode", "compression_algorithm"):
        prefix = "pool-setting-volumes-" + field
        assert marks.writes.index(prefix + "-attempt.json") < marks.writes.index(prefix + "-complete.json")
    assert "pool-setting-volumes-application-attempt.json" not in marks.records
    observed, effects = copy.deepcopy(world.pools), native_effects(world)
    del world.log[:]
    assert native.pool() == {"changed": False}
    assert setters(world.log) == [] and world.pools == observed and native_effects(world) == effects
    assert not any(entry[:3] == ("osd", "pool", "create") for entry in world.log)
    assert ready(native)[0] is True
    options = world.entry("volumes")["options"]
    options["compression_required_ratio"] = 0.875
    assert ready(native)[0] is False
    del options["compression_required_ratio"]
    del options["compression_mode"]
    assert ready(native)[0] is False
    options["compression_mode"] = "aggressive"
    assert ready(native)[0] is False
    options["compression_mode"] = "passive"
    world.entry("volumes")["quota_max_bytes"] = 0
    assert ready(native)[0] is False


def test_pool_quota_setter_lost_success_is_read_back_without_a_second_write():
    world = World()
    native, marks = resource(world, "pool", pool_value(quota={"maxBytes": 1073741824, "maxObjects": 0}))
    original = native.ceph
    failed = False

    def ceph(*args, **kwargs):
        nonlocal failed
        result = original(*args, **kwargs)
        if args[:3] == ("osd", "pool", "set-quota") and not failed:
            failed = True
            raise ceph_native.Refusal("native-command")
        return result

    native.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.pool()
    assert "pool-setting-volumes-quota_max_bytes-attempt.json" in marks.records
    assert "pool-setting-volumes-quota_max_bytes-complete.json" not in marks.records
    assert native.pool() == {"changed": True}
    quotas = [entry for entry in world.log if entry[:3] == ("osd", "pool", "set-quota")]
    assert quotas == [("osd", "pool", "set-quota", "volumes", "max_bytes", "1073741824")]
    effects = native_effects(world)
    assert native.pool() == {"changed": False} and native_effects(world) == effects
    world.entry("volumes")["quota_max_bytes"] = 0
    assert native.pool() == {"changed": False} and native_effects(world) == effects
    assert ready(native)[0] is False


@pytest.mark.parametrize("option,value,expected", [
    ("target_size_ratio", 0.2, True), ("pg_num_min", 16, False), ("pg_num_min", 64.0, False), ("pg_num_min", True, False),
    ("target_size_ratio", 0.5, False), ("target_size_ratio", "0.2", False), ("compression_min_blob_size", 4096, False),
    ("compression_min_blob_size", "8192", False),
])
def test_pool_readiness_compares_each_set_option_value(option, value, expected):
    world = World()
    autoscale = {"bulk": False, "maxPGs": 0, "minPGs": 64, "mode": "on", "targetBytes": 0, "targetRatio": "0.2"}
    compression = {"algorithm": "", "maxBlob": 0, "minBlob": 8192, "mode": "passive", "requiredRatio": ""}
    native, unused = resource(world, "pool", pool_value(autoscale=autoscale, compression=compression, pgNum=64))
    native.pool()
    assert ("osd", "pool", "set", "volumes", "compression_min_blob_size", "8192") in world.log
    assert ready(native)[0] is True
    world.entry("volumes")["options"][option] = value
    assert ready(native)[0] is expected


def test_pool_max_pgs_below_the_default_caps_pg_num():
    world = World()
    autoscale = {"bulk": False, "maxPGs": 16, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""}
    native, unused = resource(world, "pool", pool_value(autoscale=autoscale, pgNum=16))
    native.pool()
    creates = [entry for entry in world.log if entry[:3] == ("osd", "pool", "create")]
    assert creates == [("osd", "pool", "create", "volumes", "16", "16", "replicated", "replicated_rule", "--autoscale_mode", "on", "--pg_num_max", "16")]
    assert ready(native)[0] is True
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", pool_value(autoscale=autoscale, pgNum=32))


@pytest.mark.parametrize("role,metadata,expected", [
    ("cephfs-data", {"cephfs": {"data": "fs1"}}, True), ("cephfs-data", {"cephfs": {}}, True),
    ("cephfs-data", {"cephfs": {"data": "fs1", "metadata": "fs1"}}, False), ("cephfs-data", {"cephfs": {"metadata": "fs1"}}, False),
    ("cephfs-data", {"cephfs": {"data": "fs1"}, "rbd": {}}, False), ("cephfs-data", {"cephfs": {"data": ["fs1"]}}, False), ("cephfs-data", {}, False),
    ("cephfs-metadata", {"cephfs": {"metadata": "fs1"}}, True), ("cephfs-metadata", {"cephfs": {}}, True),
    ("cephfs-metadata", {"cephfs": {"data": "fs1"}}, False),
])
def test_pool_observe_accepts_cephfs_application_key_from_fs_new(role, metadata, expected):
    world = World()
    native, unused = resource(world, "pool", pool_value(role=role))
    native.pool()
    assert ("osd", "pool", "application", "enable", "volumes", "cephfs") in world.log
    world.entry("volumes")["application_metadata"] = metadata
    assert ready(native)[0] is expected


@pytest.mark.parametrize("device_class,argv", [
    ("ssd", ("osd", "crush", "rule", "create-replicated", "ssd-hosts", "default", "host", "ssd")),
    ("", ("osd", "crush", "rule", "create-replicated", "ssd-hosts", "default", "host")),
])
def test_policy_rule_create_replicated_argv(device_class, argv):
    world = World([["ssd"], ["ssd"], ["hdd"]])
    native, marks = resource(world, "policy", policy_value(device_class), count=3)
    assert native.policy() == {"changed": True}
    assert [entry for entry in world.log if entry[:4] == ("osd", "crush", "rule", "create-replicated")] == [argv]
    log = world.log
    attempt, recorded = log.index(("write", "policy-attempt-fast.json")), log.index(("write", "policy-id-fast.json"))
    assert log.index(("osd", "crush", "rule", "ls")) < attempt < log.index(argv) < recorded
    assert marks.records["policy-attempt-fast.json"] == policy_attempt(native)
    assert marks.records["policy-id-fast.json"] == {"id": world.rules["ssd-hosts"]["rule_id"]}
    observed, evidence = ready(native)
    assert observed is True and evidence["resource"] == {"id": str(world.rules["ssd-hosts"]["rule_id"]), "path": ""}
    assert native.policy() == {"changed": False} and log.count(argv) == 1


@pytest.mark.parametrize("case", ["unrecorded", "lost-reply", "other-id", "other-steps"])
def test_policy_refuses_foreign_rule_and_adopts_only_recorded(case):
    world = World([["ssd"], ["ssd"], ["ssd"]])
    native, marks = resource(world, "policy", policy_value(), count=3)
    world.rules["ssd-hosts"] = rule(5, "ssd-hosts", 1, "ssd", chooseleaf="chooseleaf_indep" if case == "other-steps" else None)
    if case != "unrecorded":
        marks.records["policy-attempt-fast.json"] = policy_attempt(native)
    if case == "other-id":
        marks.records["policy-fast.json"] = native.request["policy"]
        marks.records["policy-id-fast.json"] = {"id": 4}
    if case == "lost-reply":
        marks.records["policy-fast.json"] = native.request["policy"]
        observed, evidence = ready(native)
        assert observed is False and evidence["resource"] == {"id": "", "path": ""} and evidence["owned"]
        assert native.policy() == {"changed": False}
        assert marks.records["policy-id-fast.json"] == {"id": 5}
        assert not any(entry[:4] == ("osd", "crush", "rule", "create-replicated") for entry in world.log)
        assert ready(native)[0] is True
        return
    writes = list(marks.writes)
    with pytest.raises(ceph_native.Refusal, match="^foreign-rule$"):
        native.policy()
    if case == "unrecorded":
        assert marks.writes == writes == [] and marks.records == {}
        with pytest.raises(ceph_native.Refusal, match="^foreign-rule$"):
            ready(native)
    assert not any(entry[:4] == ("osd", "crush", "rule", "create-replicated") for entry in world.log)


AUTOSCALE_OFF = {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "off", "targetBytes": 0, "targetRatio": ""}


@pytest.mark.parametrize("kind,autoscale,field,value,expected", [
    ("replicated", None, "type", 3, False), ("replicated", None, "crush_rule", 9, False), ("replicated", None, "size", 3, False),
    ("replicated", None, "min_size", 1, False), ("replicated", None, "pg_autoscale_mode", "off", False), ("replicated", None, "pg_num", 64, True),
    ("replicated", None, "quota_max_objects", 5, False), ("replicated", AUTOSCALE_OFF, "pg_num", 64, False),
    ("replicated", AUTOSCALE_OFF, "pg_autoscale_mode", "on", False), ("erasure", None, "type", 1, False), ("erasure", None, "crush_rule", 0, False),
    ("erasure", None, "size", 5, False), ("erasure", None, "min_size", 4, False), ("erasure", None, "erasure_code_profile", "other", False),
    ("erasure", None, "stripe_width", 8192, False), ("erasure", None, "profile", {"foo": "1"}, False), ("erasure", None, "pg_num", 128, True),
])
def test_pool_readiness_compares_every_frozen_attribute(kind, autoscale, field, value, expected):
    world = World()
    native, unused = resource(world, "pool", pool_value(kind, **({"autoscale": autoscale} if autoscale else {})))
    native.pool()
    assert ready(native)[0] is True
    if field == "profile":
        world.profiles["volumes"].update(value)
    else:
        world.entry("volumes")[field] = value
    assert ready(native)[0] is expected


def test_pool_erasure_lost_reply_refuses_another_profile():
    world = World()
    native, marks = resource(world, "pool", pool_value("erasure"))
    native.pool()
    del marks.records["pool-id-volumes.json"]
    world.entry("volumes")["erasure_code_profile"] = "other"
    with pytest.raises(ceph_native.Refusal, match="^pool-identity$"):
        native.pool()
    assert "pool-id-volumes.json" not in marks.records


def foreign_create(world):
    """A create-replicated that answers 0 over a same-name rule another actor made meanwhile (OSDMonitor.cc:11576-11582)."""
    original = world.effect

    def effect(command):
        if command[:4] != ("osd", "crush", "rule", "create-replicated"):
            return original(command)
        world.mutations.append(command)
        world.rules.setdefault(command[4], rule(9, command[4], 1, "ssd", chooseleaf="chooseleaf_indep"))
        return 0, b""
    world.effect = effect


@pytest.mark.parametrize("case", ["steps-drift", "take-drift", "id-drift", "stale-mark", "foreign-create", "id-without-rule"])
def test_policy_identity_guards_after_and_around_creation(case):
    world = World([["ssd"], ["ssd"], ["ssd"]])
    native, marks = resource(world, "policy", policy_value(), count=3)
    if case == "foreign-create":
        foreign_create(world)
    if case in ("foreign-create", "id-without-rule"):
        if case == "id-without-rule":
            marks.records["policy-fast.json"] = native.request["policy"]
            marks.records["policy-id-fast.json"] = {"id": 5}
        with pytest.raises(ceph_native.Refusal, match="^foreign-rule$"):
            native.policy()
        assert (case == "foreign-create") is any(entry[:4] == ("osd", "crush", "rule", "create-replicated") for entry in world.log)
        assert ("write", "policy-id-fast.json") not in world.log
        return
    native.policy()
    assert ready(native)[0] is True
    steps = world.rules["ssd-hosts"]["steps"]
    if case == "steps-drift":
        steps[1]["op"] = "chooseleaf_indep"
    elif case == "take-drift":
        steps[0]["item_name"] = "default"
    elif case == "stale-mark":
        marks.records["policy-fast.json"] = policy_value(size=3)
        with pytest.raises(ceph_native.Refusal, match="^foreign-rule$"):
            ready(native)
        return
    else:
        marks.records["policy-id-fast.json"] = {"id": world.rules["ssd-hosts"]["rule_id"] + 1}
        with pytest.raises(ceph_native.Refusal, match="^foreign-rule$"):
            ready(native)
        return
    assert ready(native)[0] is False


def test_crush_capacity_refuses_when_class_hosts_below_width():
    world = World([["ssd", "ssd"], ["ssd", "ssd"], ["hdd"], ["hdd"]])
    native, marks = resource(world, "policy", policy_value(size=3), count=4)
    with pytest.raises(ceph_native.Refusal, match="^crush-capacity$"):
        native.policy()
    assert marks.writes == [] and world.effects() == []
    world = World([["ssd", "ssd"]] * 5 + [["hdd", "hdd"]])
    native, marks = resource(world, "pool", pool_value("erasure", device_class="ssd"))
    with pytest.raises(ceph_native.Refusal, match="^crush-capacity$"):
        native.pool()
    assert "pool-attempt-volumes.json" not in marks.records and world.effects() == [("write", "pool-volumes.json")]
    world = World([["ssd"]] * 6)
    native, marks = resource(world, "pool", pool_value("erasure", device_class="ssd"))
    assert native.pool() == {"changed": True}
    assert PROFILE_SET[:-1] + ("crush-device-class=ssd", "stripe_unit=4096") in world.log
    assert world.rules["volumes"]["steps"][2]["item_name"] == "default~ssd" and ready(native)[0] is True


@pytest.mark.parametrize("malformed", [{"nodes": {}}, {"nodes": [{"id": -1, "name": "default", "type": "root", "children": [-9]}]},
                                       {"nodes": [{"id": -1, "name": "other", "type": "root"}]}, {"nodes": [{"id": "-1"}]}])
def test_a_malformed_osd_tree_refuses_as_native_output(malformed):
    world = World()
    world.tree = malformed
    native, marks = resource(world, "policy", policy_value(), count=3)
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.policy()
    assert marks.writes == []


def test_pool_create_passes_explicit_pg_and_autoscale():
    world = World()
    autoscale = {"bulk": True, "maxPGs": 0, "minPGs": 64, "mode": "on", "targetBytes": 0, "targetRatio": "0.2"}
    native, unused = resource(world, "pool", pool_value(autoscale=autoscale, pgNum=64))
    native.pool()
    creates = [entry for entry in world.log if entry[:3] == ("osd", "pool", "create")]
    assert creates == [("osd", "pool", "create", "volumes", "64", "64", "replicated", "replicated_rule", "--autoscale_mode", "on", "--pg_num_min", "64",
                        "--bulk", "--target_size_ratio", "0.2")]
    assert ready(native)[0] is True
    world.entry("volumes")["flags_names"] = "hashpspool"
    assert ready(native)[0] is False
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", pool_value(autoscale=autoscale, pgNum=32))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", pool_value(autoscale=dict(autoscale, targetBytes=1), pgNum=64))


def evidence_value(action, identifier):
    return {"version": ceph_native.EVIDENCE_VERSION, "request": "d" * 64, "action": action, "owner": OWNER, "fsid": FSID, "owned": True,
            "exists": True, "ready": True, "removed": False, "partial": False, "health": "HEALTH_OK", "hosts": [], "monitors": [], "monAddrs": [],
            "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": {"id": identifier, "path": ""}, "client": "",
            "dashboard": {"url": "", "certificateSHA256": "", "reachable": False}}


def test_policy_evidence_carries_rule_id():
    assert ceph_core.evidence(evidence_value("policy", "3"), "d" * 64)["resource"] == {"id": "3", "path": ""}
    for refused in (evidence_value("client", "3"), evidence_value("policy", "03"), evidence_value("rule", "3")):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(refused, "d" * 64)


def test_policy_apply_calls_the_policy_phase(monkeypatch):
    request = cluster(3)
    request.update(action="policy", policy=policy_value())
    calls = []

    class Transport:
        def call(self, host, phase, timeout=None):
            del timeout
            calls.append(phase)
            if phase == "observe":
                done = "policy" in calls
                return dict(evidence_value("policy", "3" if done else ""), request="", ready=done, exists=done, partial=not done, owned=done)
            if phase == "host_state":
                return {"name": host["name"], "owned": True, "exists": True}
            return {"changed": phase == "policy"}

    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    result = ceph_core.execute(request, "apply", "d" * 64, Transport())
    assert result["changed"] and result["evidence"]["resource"] == {"id": "3", "path": ""}
    assert calls.count("policy") == 1 and calls.index("policy") > calls.index("observe")


@pytest.mark.parametrize("change", [
    {"rule": "replicated_rule"}, {"failureDomain": "rack"}, {"root": "other"}, {"deviceClass": "ssd class"}, {"size": 1}, {"size": 4}, {"size": True},
    {"extra": 1},
])
def test_policy_validation_holds_the_frozen_contract(change):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "policy", dict(policy_value(), **change), count=3)
    for extra in ({"action": "policy", "policy": policy_value(), "pool": pool_value()}, {"policy": policy_value()},
                  {"action": "pool", "pool": pool_value(), "policy": policy_value()}):
        request = cluster(3)
        request.update(extra)
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate(request)


@pytest.mark.parametrize("owned", [True, False])
def test_a_policy_without_daemons_reports_only_its_own_retained_ownership(owned):
    world = World()
    native, marks = resource(world, "policy", policy_value(), count=3)
    native.daemons = lambda: []
    if owned:
        marks.records["policy-fast.json"] = native.request["policy"]
    evidence = native.observe()
    assert evidence["owned"] is owned and evidence["exists"] is owned and evidence["partial"] is owned and not evidence["ready"]
    assert world.effects() == []


@pytest.mark.parametrize("change", [
    {"application": "cephfs"}, {"role": "mirror"}, {"role": ["rbd"]}, {"pgNum": 33},
    {"compression": {"algorithm": "zstd", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}},
    {"compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "force", "requiredRatio": "1.5"}}, {"quota": {"maxBytes": -1, "maxObjects": 0}},
    {"autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "warn", "targetBytes": 0, "targetRatio": ""}}, {"overwrites": True}, {"size": 6},
])
def test_pool_validation_holds_the_frozen_contract(change):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", dict(pool_value(), **change))


@pytest.mark.parametrize("change", [
    {"dataChunks": 3}, {"plugin": "clay"}, {"technique": "cauchy_good"}, {"profile": "other"}, {"stripeUnit": 1000}, {"profileDefaults": {"foo": ""}},
    {"failureDomain": "osd"}, {"deviceClass": "a/b"},
])
def test_erasure_validation_holds_the_frozen_contract(change):
    pool = pool_value("erasure")
    pool["erasure"].update(change)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", pool)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", pool_value("erasure"), count=5)
    jerasure = pool_value("erasure", plugin="jerasure")
    jerasure["erasure"].update(dataChunks=2, codingChunks=2)
    jerasure.update(size=4, minSize=3)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        resource(World(), "pool", jerasure)
