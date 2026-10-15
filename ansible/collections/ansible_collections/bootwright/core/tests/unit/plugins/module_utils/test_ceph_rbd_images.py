"""RBD images: exact create argv, the attempt record, lost-reply adoption, foreign refusal and observed shape.

The scripted `rbd` below answers the upstream v20.2.1 shapes the worker reads; none of it qualifies the IBM image (B472):
- `--size <n>B` is exact bytes and a bare number is MiB (src/common/strtol.cc:137-215; src/tools/rbd/ArgumentTypes.cc:404-421);
- each `--image-feature` value is split on `,`, `''` is a present empty mask, and an absent option takes rbd_default_features
  layering,exclusive-lock,object-map,fast-diff,deep-flatten (ArgumentTypes.cc:472-496; src/tools/rbd/Utils.cc:500-504;
  src/librbd/image/CreateRequest.cc:145-149; src/common/options/rbd.yaml.in:44-54);
- object-map adds fast-diff and a data pool adds data-pool (CreateRequest.cc:163-166, :202-212);
- `rbd info --format json` keys name, id, size, objects, order, object_size, snapshot_count, data_pool (only with one),
  block_name_prefix, format, features/op_features/flags in feature-bit order, parent only for a clone
  (src/tools/rbd/action/Info.cc:23-55, :226-238, :327-334);
- the image ID is lowercase hex of at most 14 characters (src/librbd/Utils.cc:75-93; src/include/rbd_types.h:86);
- `rbd ls --format json <pool>` lists the pool directory's names; a trashed image has left it (doc/rbd/rados-rbd-cmds.rst).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
OWNER = "apply-123"
GIB = 1024 ** 3
IMAGE_ID = "10226b8b4567"
DEFAULTS = ["layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten"]
BIT_ORDER = ("layering", "striping", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling", "data-pool")
FORBIDDEN = ("rm", "remove", "trash", "resize", "--allow-shrink", "feature", "migration", "flatten", "--force")
PREFIX = ["/usr/bin/rbd", "--conf", "/private/cluster/ceph.conf", "--keyring", "/private/cluster/ceph.client.admin.keyring"]


def cluster(count=3):
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": GIB}], "labels": [], "location": {},
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
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


def image_value(**changes):
    image = {"dataPool": "", "features": list(DEFAULTS), "name": "disk", "pool": "volumes", "sizeBytes": 10 * GIB}
    image.update(changes)
    return image


def info(image, **changes):
    """What `rbd info` reports for an image created with exactly the frozen values."""
    features = image["features"] + (["data-pool"] if image["dataPool"] else [])
    value = {"name": image["name"], "id": IMAGE_ID, "size": image["sizeBytes"], "objects": -(-image["sizeBytes"] // (4 << 20)), "order": 22,
             "object_size": 4 << 20, "snapshot_count": 0, "block_name_prefix": "rbd_data." + IMAGE_ID, "format": 2, "features": features,
             "op_features": [], "flags": []}
    if image["dataPool"]:
        value["data_pool"] = image["dataPool"]
    value.update(changes)
    return value


class World:
    """A scripted cluster: two owned pools, an image table, and every create applied and logged."""

    def __init__(self):
        self.pools = [{"pool_name": "volumes", "pool_id": 8}, {"pool_name": "ec-data", "pool_id": 9}]
        self.images, self.log, self.mutations, self.argvs = {}, [], [], []

    def run(self, argv, data=b"", timeout=60):
        del data, timeout
        for word in FORBIDDEN:
            assert word not in argv, argv
        if argv[0] == "/usr/bin/ceph":
            command = tuple(argv[argv.index("--format") + 2:])
            assert command == ("osd", "pool", "ls", "detail"), command
            self.log.append(command)
            return 0, json.dumps(self.pools).encode()
        assert argv[:5] == PREFIX, argv
        command = tuple(argv[5:])
        self.log.append(command)
        self.argvs.append(list(argv))
        if command[:3] == ("ls", "--format", "json"):
            return 0, json.dumps(sorted(name for pool, name in self.images if pool == command[3])).encode()
        if command[:3] == ("info", "--format", "json"):
            key = tuple(command[3].split("/"))
            return (0, json.dumps(self.images[key]).encode()) if key in self.images else (2, b"")
        assert command[0] == "create", command
        self.mutations.append(command)
        self.create(list(command[1:]))
        return 0, b""

    def create(self, args):
        pool, name = args.pop().split("/")
        size, image_format, data_pool, features = None, 2, "", None
        for flag, value in zip(args[::2], args[1::2]):
            if flag == "--size":
                size = int(value[:-1]) if value.endswith("B") else int(value) << 20
            elif flag == "--image-format":
                image_format = int(value)
            elif flag == "--image-feature":
                features = (features or []) + [item for item in value.split(",") if item]
            elif flag == "--data-pool":
                data_pool = value
            else:
                raise AssertionError(flag)
        features = set(DEFAULTS if features is None else features)
        if "object-map" in features:
            features.add("fast-diff")
        if data_pool:
            features.add("data-pool")
        created = info({"name": name, "features": [], "dataPool": data_pool, "sizeBytes": size}, format=image_format,
                       features=[feature for feature in BIT_ORDER if feature in features])
        self.images[(pool, name)] = created


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


def worker(world, image):
    request = cluster()
    request.update(action="rbd", rbd=image)
    marks = Markers(world)
    marks.records.update({"pool-volumes.json": {"name": "volumes"}, "pool-id-volumes.json": {"id": 8},
                          "pool-ec-data.json": {"name": "ec-data"}, "pool-id-ec-data.json": {"id": 9}})
    return ceph_native_resources.ResourceWorker(request, "node-1", run=world.run, markers=marks), marks


def attempt(image):
    return {"version": "ceph-rbd-image-attempt-v1", "owner": OWNER, "fsid": FSID, "rbd": image}


def observe(native):
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), evidence


def creates(world):
    return [argv for argv in world.argvs if argv[5] == "create"]


@pytest.mark.parametrize("data_pool", ["", "ec-data"])
def test_rbd_create_argv_exact_bytes_and_features(data_pool):
    world = World()
    image = image_value(dataPool=data_pool)
    native, marks = worker(world, image)
    assert native.rbd() == {"changed": True}
    tail = ["--data-pool", "ec-data"] if data_pool else []
    assert creates(world) == [PREFIX + ["create", "--size", "10737418240B", "--image-format", "2", "--image-feature", "layering",
                                        "--image-feature", "exclusive-lock", "--image-feature", "object-map", "--image-feature", "fast-diff",
                                        "--image-feature", "deep-flatten"] + tail + ["volumes/disk"]]
    create = world.mutations.index(next(m for m in world.mutations if m[0] == "create"))
    assert world.mutations.index(("write", "rbd-disk.json")) < world.mutations.index(("write", "rbd-attempt.disk.json")) < create
    assert create < world.mutations.index(("write", "rbd-id.disk.json"))
    assert marks.records["rbd-attempt.disk.json"] == attempt(image) and marks.records["rbd-id.disk.json"] == {"id": IMAGE_ID}
    ready, evidence = observe(native)
    assert ready is True and evidence["resource"] == {"id": IMAGE_ID, "path": ""} and evidence["owned"] and evidence["exists"]
    assert native.rbd() == {"changed": False} and len(creates(world)) == 1


def test_rbd_create_explicit_empty_passes_empty_feature():
    world = World()
    native = worker(world, image_value(features=[]))[0]
    assert native.rbd() == {"changed": True}
    argv = creates(world)[0]
    assert argv.count("--image-feature") == 1 and argv[argv.index("--image-feature") + 1] == ""
    assert world.images[("volumes", "disk")]["features"] == [] and observe(native)[0] is True


def test_rbd_refuses_foreign_image():
    world = World()
    image = image_value()
    world.images[("volumes", "disk")] = info(image)
    native, marks = worker(world, image)
    with pytest.raises(ceph_native.Refusal, match="^foreign-rbd-image$"):
        native.rbd()
    assert marks.writes == [] and world.mutations == []
    with pytest.raises(ceph_native.Refusal, match="^foreign-rbd-image$"):
        observe(native)


@pytest.mark.parametrize("case,reason", [
    ("adopt", None), ("size", "rbd-image-identity"), ("features", "rbd-image-identity"), ("data_pool", "rbd-image-identity"),
    ("no-attempt", "foreign-rbd-image"), ("other-attempt", "foreign-rbd-image"), ("snapshot", "rbd-image-identity"),
])
def test_rbd_lost_reply_adopts_only_matching_attempt(case, reason):
    world = World()
    image = image_value()
    native, marks = worker(world, image)
    drift = {"size": {"size": 20 * GIB}, "features": {"features": ["layering"]}, "data_pool": {"data_pool": "ec-data"},
             "snapshot": {"snapshot_count": 1}}.get(case, {})
    world.images[("volumes", "disk")] = info(image, **drift)
    marks.records["rbd-disk.json"] = image
    if case != "no-attempt":
        marks.records["rbd-attempt.disk.json"] = attempt(image_value(sizeBytes=GIB) if case == "other-attempt" else image)
    if reason is None:
        assert native.rbd() == {"changed": True}
        assert marks.records["rbd-id.disk.json"] == {"id": IMAGE_ID} and creates(world) == []
        assert observe(native)[0] is True
        return
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.rbd()
    assert "rbd-id.disk.json" not in marks.records and creates(world) == []


@pytest.mark.parametrize("data_pool,drift,expected", [
    ("ec-data", {}, True),
    ("ec-data", {"features": DEFAULTS}, False),
    ("", {"data_pool": "ec-data"}, False),
    ("", {"features": DEFAULTS + ["data-pool"]}, False),
    ("", {"snapshot_count": 2}, True),
    ("", {"parent": {"pool": "volumes", "image": "base", "snapshot": "s"}}, False),
    ("", {"format": 1}, False),
    ("", {"size": 10 * GIB + 1}, False),
])
def test_rbd_observe_features_include_implicit_data_pool(data_pool, drift, expected):
    world = World()
    image = image_value(dataPool=data_pool)
    native, marks = worker(world, image)
    world.images[("volumes", "disk")] = info(image, **drift)
    marks.records.update({"rbd-disk.json": image, "rbd-id.disk.json": {"id": IMAGE_ID}})
    ready, evidence = observe(native)
    assert ready is expected and evidence["resource"] == {"id": IMAGE_ID, "path": ""}


def test_rbd_observe_refuses_unrecorded_id_change():
    world = World()
    image = image_value()
    native, marks = worker(world, image)
    world.images[("volumes", "disk")] = info(image, id="bbbb")
    marks.records.update({"rbd-disk.json": image, "rbd-id.disk.json": {"id": "aaaa"}})
    for call in (observe, lambda value: value.rbd()):
        with pytest.raises(ceph_native.Refusal, match="^rbd-image-identity$"):
            call(native)
    del marks.records["rbd-id.disk.json"]
    with pytest.raises(ceph_native.Refusal, match="^rbd-image-identity$"):
        observe(native)
    marks.records["rbd-attempt.disk.json"] = attempt(image)
    ready, evidence = observe(native)
    assert ready is False and evidence["resource"] == {"id": "", "path": ""} and evidence["owned"] and evidence["exists"]
    del world.images[("volumes", "disk")]
    marks.records["rbd-id.disk.json"] = {"id": "bbbb"}
    with pytest.raises(ceph_native.Refusal, match="^rbd-image-identity$"):
        native.rbd()
    assert creates(world) == []


def test_rbd_observe_absent_pool_reports_absent_image_without_rbd_call():
    world = World()
    world.pools = [entry for entry in world.pools if entry["pool_name"] != "volumes"]
    native = worker(world, image_value())[0]
    ready, evidence = observe(native)
    assert ready is False and evidence["exists"] is False and evidence["resource"] == {"id": "", "path": ""}
    assert world.argvs == []


def test_rbd_declared_change_refuses_identity():
    world = World()
    native, marks = worker(world, image_value(sizeBytes=2 * GIB))
    created = image_value(sizeBytes=GIB)
    world.images[("volumes", "disk")] = info(created)
    marks.records.update({"rbd-disk.json": created, "rbd-attempt.disk.json": attempt(created), "rbd-id.disk.json": {"id": IMAGE_ID}})
    for call in (lambda value: value.rbd(), observe):
        with pytest.raises(ceph_native.Refusal, match="^rbd-image-identity$"):
            call(native)
    assert world.mutations == []


@pytest.mark.parametrize("case", ["missing", "pool-id", "data-missing", "data-id"])
def test_rbd_requires_owned_pools(case):
    world = World()
    native, marks = worker(world, image_value(dataPool="ec-data"))
    name = "ec-data" if case.startswith("data") else "volumes"
    if case.endswith("missing"):
        del marks.records["pool-" + name + ".json"]
    else:
        marks.records["pool-id-" + name + ".json"] = {"id": 7}
    with pytest.raises(ceph_native.Refusal, match="^pool-identity$"):
        native.rbd()
    assert world.argvs == [] and world.mutations == []


@pytest.mark.parametrize("change", [
    {"sizeBytes": 0}, {"sizeBytes": True}, {"sizeBytes": 2 ** 63}, {"sizeBytes": 1.0}, {"features": ["exclusive-lock", "layering"]},
    {"features": ["striping"]}, {"features": ["layering", "layering"]}, {"features": ["fast-diff"]}, {"features": ["exclusive-lock", "object-map"]},
    {"features": ["journaling"]}, {"dataPool": "volumes"}, {"extra": 1}, {"sizeBytes": 1073741824000001},
    {"name": "attempt.disk"}, {"pool": "a/b"}, {"features": "layering"},
])
def test_rbd_request_validation_mirrors_admission(change):
    request = cluster()
    request.update(action="rbd", rbd=image_value(**change))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)
    for extra in ({"pool": {}}, {"client": {}}, {"policy": {}}):
        request = cluster()
        request.update(action="rbd", rbd=image_value(), **extra)
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate(request)
    for action in ("cluster", "pool"):
        request = cluster()
        request.update(action=action, rbd=image_value())
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate(request)
    for admitted in (image_value(), image_value(features=[]), image_value(features=["layering"], sizeBytes=2 ** 63 - 1),
                     image_value(dataPool="ec-data", features=["exclusive-lock", "journaling"]), image_value(sizeBytes=1073741824000000)):
        request = cluster()
        request.update(action="rbd", rbd=admitted)
        assert ceph_native.validate(request) is request


def test_rbd_unfinished_reports_owned_mark_partial():
    world = World()
    image = image_value()
    native, marks = worker(world, image)
    native.daemons = lambda: []
    evidence = native.observe()
    assert not evidence["owned"] and not evidence["partial"]
    marks.records["rbd-disk.json"] = image
    evidence = native.observe()
    assert evidence["owned"] and evidence["exists"] and evidence["partial"] and not evidence["ready"]
    assert world.argvs == []
