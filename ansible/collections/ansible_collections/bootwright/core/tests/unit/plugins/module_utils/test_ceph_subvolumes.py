"""CephFS subvolumes: absence before create, the attempt record, explicit attributes, lost-reply adoption, owner metadata and readiness.

The scripted volumes module below answers the upstream v20.2.1 shapes the worker reads; none of it qualifies the IBM image (B472):
- `fs subvolume create <vol> <sub> [--group_name] [--pool_layout] [--uid] [--gid] [--mode] [--size]` (src/pybind/mgr/volumes/module.py:139-157);
  a create on an existing subvolume rewrites uid, gid, mode, layout and quota and resets the earmark (src/pybind/mgr/volumes/fs/volume.py:249-290);
- a fresh create makes /volumes/<group or _nogroup>/<sub>/<uuid4> and sets the mode only through mkdirs under the manager's umask, a repeat
  through lchmod (src/pybind/mgr/volumes/fs/operations/versions/subvolume_v2.py:157-183, subvolume_base.py:296-300);
- `fs subvolume ls` lists {name} and answers [] for the default group before it exists, while a missing named group fails
  (src/pybind/mgr/volumes/fs/volume.py:637-649, fs/operations/group.py:61-68); an explicit `--group_name _nogroup` refuses EPERM (group.py:21-32);
- `fs subvolume info` reports path, type, state, uid, gid, the full st_mode, data_pool, bytes_quota ("infinite" for none) and pool_namespace
  (src/pybind/mgr/volumes/fs/operations/versions/subvolume_base.py:473-547);
- `fs subvolume metadata set|ls` store and list user keys in lower case (doc/cephfs/fs-volumes.rst:572-575); that `ls` answers {} for a
  subvolume with no user metadata is read from metadata_manager.py, which is not downloaded (medium confidence).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_filesystems as fs_fixtures

core_request = core_fixtures.core_request

OWNER = fs_fixtures.OWNER
FSID = fs_fixtures.FSID
GIB = 1024 ** 3
UUIDS = ("0f8fad5b-d9cb-469f-a165-70867728950e", "7c9e6679-7425-40de-944b-e07fc1f90ae7")
FORBIDDEN_WORDS = ("rm", "resize", "snapshot", "clone", "authorize", "deauthorize", "evict", "pin", "earmark", "get", "volume", "--namespace_isolated",
                   "--earmark", "--normalization", "--casesensitive")


def sub_value(**changes):
    subvolume = {"filesystem": "files", "gid": 0, "group": "", "mode": "755", "name": "docs", "pool": "cephfs-data", "sizeBytes": 0, "uid": 0}
    subvolume.update(changes)
    return subvolume


class World(fs_fixtures.World):
    """The FSMap with filesystem files (fscid 7) on the owned pools, group csi, and the volumes module's subvolumes by (group, name)."""

    def __init__(self):
        super().__init__()
        self.add_filesystem("files", "cephfs-metadata", "cephfs-data")
        self.groups["csi"] = {}
        self.subvolumes, self.umask, self.created = {}, 0o022, []
        self.on_create = None

    def run(self, argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:])
        if command[:2] != ("fs", "subvolume"):
            return super().run(argv, data, timeout)
        for word in FORBIDDEN_WORDS:
            assert word not in command, command
        self.log.append(command)
        group, words = "", list(command)
        if "--group_name" in words:
            index = words.index("--group_name")
            group = words[index + 1]
            del words[index:index + 2]
        assert group != "_nogroup" and words[3 if words[2] != "metadata" else 4] == "files", command
        if group and group not in self.groups:
            return 2, b""
        return self.volumes_call(words, group, command)

    def volumes_call(self, words, group, command):
        verb = words[2]
        if verb == "ls":
            return 0, json.dumps([{"name": name} for (owner, name) in sorted(self.subvolumes) if owner == group]).encode()
        if verb == "metadata":
            item = self.subvolumes.get((group, words[5]))
            if item is None:
                return 2, b""
            if words[3] == "ls":
                return 0, json.dumps(item["metadata"]).encode()
            assert words[3] == "set" and len(words) == 8, words
            self.mutations.append(command)
            item["metadata"][words[6].lower()] = words[7]
            return 0, b""
        if verb == "info":
            item = self.subvolumes.get((group, words[4]))
            return (0, json.dumps(item["info"]).encode()) if item is not None else (2, b"")
        assert verb == "create", words
        self.mutations.append(command)
        if self.on_create is not None:
            self.on_create()
        self.create(group, words[4], dict(zip(words[5::2], words[6::2])))
        return 0, b""

    def create(self, group, name, flags):
        assert set(flags) <= {"--pool_layout", "--uid", "--gid", "--mode", "--size"}, flags
        mode = int(flags.get("--mode", "755"), 8)
        item = self.subvolumes.get((group, name))
        if item is None:
            identifier = UUIDS[len(self.created) % len(UUIDS)]
            self.created.append(identifier)
            item = {"info": {"path": "/volumes/" + (group or "_nogroup") + "/" + name + "/" + identifier, "type": "subvolume", "state": "complete",
                             "pool_namespace": "", "features": ["snapshot-clone", "snapshot-autoprotect", "snapshot-retention"], "earmark": "",
                             "normalization": "none", "casesensitive": True, "bytes_used": 0}, "metadata": {}}
            self.subvolumes[(group, name)] = item
            mode &= ~self.umask
        item["info"].update(uid=int(flags.get("--uid", 0)), gid=int(flags.get("--gid", 0)), mode=0o40000 | mode,
                            data_pool=flags.get("--pool_layout", "cephfs-data"), bytes_quota=int(flags["--size"]) if "--size" in flags else "infinite",
                            earmark="")

    def existing(self, subvolume, identifier=UUIDS[0], **info):
        """A subvolume already in the volumes module, as a create of the frozen value made it, with optional attribute changes."""
        self.created.append(identifier)
        self.subvolumes[(subvolume["group"], subvolume["name"])] = {"info": dict({
            "path": "/volumes/" + (subvolume["group"] or "_nogroup") + "/" + subvolume["name"] + "/" + identifier, "type": "subvolume",
            "state": "complete", "pool_namespace": "", "uid": subvolume["uid"], "gid": subvolume["gid"], "mode": 0o40000 | int(subvolume["mode"], 8),
            "data_pool": subvolume["pool"], "bytes_quota": subvolume["sizeBytes"] or "infinite", "earmark": ""}, **info), "metadata": {}}
        return self.subvolumes[(subvolume["group"], subvolume["name"])]


def worker(world, subvolume=None):
    request = fs_fixtures.cluster()
    request.update(action="subvolume", subvolume=subvolume or sub_value())
    marks = fs_fixtures.Markers(world)
    for name, identifier in fs_fixtures.POOL_IDS.items():
        marks.records.update({"pool-" + name + ".json": {"name": name}, "pool-id-" + name + ".json": {"id": identifier}})
    marks.records.update({"filesystem-files.json": fs_fixtures.fs_value(groups=[fs_fixtures.group_value()]), "filesystem-id.files.json": {"id": 7},
                          "group.files.csi.json": fs_fixtures.group_value()})
    native = ceph_native_resources.ResourceWorker(request, "node-1", run=world.run, markers=marks)
    clock = [0.0]
    native.clock, native.pause = lambda: clock[0], lambda seconds: clock.__setitem__(0, clock[0] + seconds)
    return native, marks


def attempt(subvolume):
    return {"version": "ceph-subvolume-attempt-v1", "owner": OWNER, "fsid": FSID, "subvolume": subvolume}


def observe(native):
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), evidence


def creates(world):
    return [command for command in world.mutations if command[:3] == ("fs", "subvolume", "create")]


def volume_commands(world):
    return [command for command in world.log if command[:2] == ("fs", "subvolume")]


def test_subvolume_absence_proved_before_create():
    world = World()
    world.existing(sub_value())
    native, marks = worker(world)
    with pytest.raises(ceph_native.Refusal, match="^foreign-subvolume$"):
        native.subvolume()
    assert world.mutations == [] and not any(name.startswith("subvolume") for name in marks.records)
    assert volume_commands(world) == [("fs", "subvolume", "ls", "files")]


@pytest.mark.parametrize("subvolume,argv", [
    (sub_value(), ("fs", "subvolume", "create", "files", "docs", "--pool_layout", "cephfs-data", "--uid", "0", "--gid", "0", "--mode", "755")),
    (sub_value(group="csi", sizeBytes=GIB, uid=1000, gid=2000, mode="750"),
     ("fs", "subvolume", "create", "files", "docs", "--group_name", "csi", "--pool_layout", "cephfs-data", "--uid", "1000", "--gid", "2000", "--mode",
      "750", "--size", str(GIB))),
])
def test_subvolume_create_argv_explicit(subvolume, argv):
    world = World()
    native, marks = worker(world, subvolume)
    assert native.subvolume() == {"changed": True}
    assert creates(world) == [argv]
    assert marks.records["subvolume-path.docs.json"] == {"path": "/volumes/" + (subvolume["group"] or "_nogroup") + "/docs/" + UUIDS[0]}
    assert all("_nogroup" not in command for command in world.log)
    ready, evidence = observe(native)
    assert ready is True and evidence["resource"] == {"id": UUIDS[0], "path": marks.records["subvolume-path.docs.json"]["path"]}
    assert native.subvolume() == {"changed": False} and len(creates(world)) == 1


def test_subvolume_attempt_written_before_create():
    world = World()
    native, marks = worker(world)
    seen = []
    world.on_create = lambda: seen.append(marks.records.get("subvolume-attempt.docs.json"))
    native.subvolume()
    assert seen == [attempt(sub_value())]
    order = world.mutations
    assert order.index(("write", "subvolume-docs.json")) < order.index(("write", "subvolume-attempt.docs.json")) < order.index(creates(world)[0])
    assert order.index(creates(world)[0]) < order.index(("write", "subvolume-path.docs.json"))


@pytest.mark.parametrize("case,reason", [("adopt", None), ("no-attempt", "foreign-subvolume"), ("other-uid", "subvolume-identity")])
def test_subvolume_lost_reply_adopts_via_attempt_and_path(case, reason):
    world = World()
    world.existing(sub_value(), uid=1000 if case == "other-uid" else 0)
    native, marks = worker(world)
    marks.records["subvolume-docs.json"] = sub_value()
    if case != "no-attempt":
        marks.records["subvolume-attempt.docs.json"] = attempt(sub_value())
    if case == "no-attempt":
        with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
            observe(native)
    else:
        ready, evidence = observe(native)
        assert ready is False and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    if reason is not None:
        with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
            native.subvolume()
        assert "subvolume-path.docs.json" not in marks.records and [item for item in world.mutations if item[0] != "write"] == []
        return
    assert native.subvolume() == {"changed": True} and creates(world) == []
    assert marks.records["subvolume-path.docs.json"] == {"path": "/volumes/_nogroup/docs/" + UUIDS[0]}
    assert world.subvolumes[("", "docs")]["metadata"] == {"bootwright-owner": OWNER}
    assert observe(native)[0] is True


def test_subvolume_first_create_under_umask_is_created_once_more():
    world = World()
    native, marks = worker(world, sub_value(mode="777"))
    assert native.subvolume() == {"changed": True}
    assert len(creates(world)) == 2 and creates(world)[0] == creates(world)[1]
    assert world.mutations.index(creates(world)[1]) < world.mutations.index(("write", "subvolume-path.docs.json"))
    assert world.subvolumes[("", "docs")]["info"]["mode"] & 0o7777 == 0o777 and observe(native)[0] is True
    world.subvolumes[("", "docs")]["info"]["mode"] = 0o40755
    assert observe(native)[0] is False
    assert native.subvolume() == {"changed": False} and len(creates(world)) == 2
    assert marks.records["subvolume-path.docs.json"]["path"].endswith(UUIDS[0])


def test_subvolume_owner_metadata_verified():
    world = World()
    native = worker(world)[0]
    native.subvolume()
    metadata = world.subvolumes[("", "docs")]["metadata"]
    assert metadata == {"bootwright-owner": OWNER} and observe(native)[0] is True
    metadata.clear()
    assert observe(native)[0] is False
    metadata["bootwright-owner"] = "apply-other"
    assert observe(native)[0] is False
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        native.subvolume()
    assert metadata == {"bootwright-owner": "apply-other"}


@pytest.mark.parametrize("change", [{"state": "pending"}, {"type": "clone"}])
def test_subvolume_observe_state_complete_required(change):
    world = World()
    native = worker(world)[0]
    native.subvolume()
    world.subvolumes[("", "docs")]["info"].update(change)
    ready, evidence = observe(native)
    assert ready is False and evidence["owned"] and evidence["resource"]["id"] == UUIDS[0]


@pytest.mark.parametrize("change", [{"uid": 7}, {"gid": 7}, {"data_pool": "cephfs-extra"}, {"bytes_quota": GIB}, {"pool_namespace": "fsvolumens_docs"}])
def test_subvolume_attribute_drift_is_not_ready_never_a_refusal(change):
    world = World()
    native = worker(world)[0]
    native.subvolume()
    world.subvolumes[("", "docs")]["info"].update(change)
    assert observe(native)[0] is False


def test_subvolume_recorded_but_gone_refuses_identity():
    world = World()
    native, marks = worker(world)
    native.subvolume()
    del world.subvolumes[("", "docs")]
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        native.subvolume()
    assert len(creates(world)) == 1
    ready, evidence = observe(native)
    assert ready is False and evidence["owned"] and evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    assert marks.records["subvolume-path.docs.json"]["path"].endswith(UUIDS[0])


def test_subvolume_recorded_path_differs_refuses_identity():
    world = World()
    native, marks = worker(world)
    native.subvolume()
    world.existing(sub_value(), identifier=UUIDS[1])
    before = list(world.mutations)
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        native.subvolume()
    assert [item for item in world.mutations[len(before):] if item[0] != "write"] == [] and world.subvolumes[("", "docs")]["metadata"] == {}
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        observe(native)
    assert marks.records["subvolume-path.docs.json"]["path"].endswith(UUIDS[0])


def test_subvolume_observe_requires_the_recorded_fscid():
    world = World()
    native, marks = worker(world)
    native.subvolume()
    assert observe(native)[0] is True
    marks.records["filesystem-id.files.json"] = {"id": 8}
    ready, evidence = observe(native)
    assert ready is False and evidence["resource"]["id"] == UUIDS[0]


def test_subvolume_observe_refuses_a_foreign_subvolume():
    world = World()
    world.existing(sub_value())
    native, marks = worker(world)
    with pytest.raises(ceph_native.Refusal, match="^foreign-subvolume$"):
        observe(native)
    assert world.mutations == [] and not any(name.startswith("subvolume") for name in marks.records)


@pytest.mark.parametrize("case,reason", [("filesystem-marker", "filesystem-identity"), ("fscid", "filesystem-identity"),
                                         ("unattached", "filesystem-identity"), ("group-marker", "foreign-subvolume-group"),
                                         ("unowned-pool", "pool-identity"), ("group-gone", "filesystem-identity")])
def test_subvolume_parent_proved_before_any_subvolume_command(case, reason):
    world = World()
    subvolume = sub_value(group="csi", pool="cephfs-extra" if case == "unattached" else "cephfs-data")
    native, marks = worker(world, subvolume)
    if case == "filesystem-marker":
        del marks.records["filesystem-files.json"]
    elif case == "fscid":
        marks.records["filesystem-id.files.json"] = {"id": 8}
    elif case == "group-marker":
        del marks.records["group.files.csi.json"]
    elif case == "unowned-pool":
        del marks.records["pool-id-cephfs-data.json"]
    elif case == "group-gone":
        del world.groups["csi"]
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.subvolume()
    assert volume_commands(world) == [] and world.mutations == []


def test_subvolume_marker_of_another_value_refuses_identity():
    world = World()
    native, marks = worker(world)
    marks.records["subvolume-docs.json"] = sub_value(uid=5)
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        native.subvolume()
    with pytest.raises(ceph_native.Refusal, match="^subvolume-identity$"):
        observe(native)
    assert world.mutations == []


def test_subvolume_observe_never_lists_a_missing_group():
    world = World()
    del world.groups["csi"]
    native = worker(world, sub_value(group="csi"))[0]
    ready, evidence = observe(native)
    assert ready is False and not evidence["exists"] and volume_commands(world) == []


@pytest.mark.parametrize("group", ["", "csi"])
def test_subvolume_observe_never_lists_a_missing_filesystem(group):
    world = World()
    world.filesystems.remove(world.entry("files"))
    native = worker(world, sub_value(group=group))[0]
    ready, evidence = observe(native)
    assert ready is False and not evidence["exists"] and evidence["resource"] == {"id": "", "path": ""}
    assert [command for command in world.log if command[:2] in (("fs", "subvolume"), ("fs", "subvolumegroup"))] == []


@pytest.mark.parametrize("change", ["extra", "missing", "dot", "long", "nogroup", "mode", "uid", "filesystem"])
def test_validation_refuses_a_subvolume_go_never_freezes(change):
    subvolume = sub_value()
    if change == "extra":
        subvolume["namespaceIsolated"] = True
    elif change == "missing":
        del subvolume["mode"]
    elif change == "dot":
        subvolume["name"] = "do.cs"
    elif change == "long":
        subvolume["name"] = "s" * 49
    elif change == "nogroup":
        subvolume["group"] = "_nogroup"
    elif change == "mode":
        subvolume["mode"] = "0755"
    elif change == "uid":
        subvolume["uid"] = -1
    request = fs_fixtures.cluster()
    request.update(action="subvolume", subvolume=subvolume)
    if change == "filesystem":
        request["filesystem"] = fs_fixtures.fs_value()
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native_resources.ResourceWorker(request, "node-1", run=World().run, markers=fs_fixtures.Markers(World()))


def core_subvolume_request(core_request, **changes):
    return dict(core_request, action="subvolume", subvolume=sub_value(**changes))


@pytest.mark.parametrize("resource,admitted", [
    ({"id": "", "path": ""}, True),
    ({"id": UUIDS[0], "path": "/volumes/_nogroup/docs/" + UUIDS[0]}, True),
    ({"id": UUIDS[0], "path": "/volumes/csi/docs/" + UUIDS[0]}, False),
    ({"id": UUIDS[0], "path": "/srv/docs"}, False),
    ({"id": UUIDS[1], "path": "/volumes/_nogroup/docs/" + UUIDS[0]}, False),
    ({"id": UUIDS[0].upper(), "path": "/volumes/_nogroup/docs/" + UUIDS[0].upper()}, False),
    ({"id": "", "path": "/volumes/_nogroup/docs/" + UUIDS[0]}, False),
    ({"id": UUIDS[0], "path": ""}, False),
])
def test_subvolume_evidence_admits_only_the_exact_path(core_request, resource, admitted):
    request = core_subvolume_request(core_request)
    value = dict(core_fixtures.observation(request), request="d" * 64, resource=resource)
    if admitted:
        assert ceph_core.evidence(value, "d" * 64, request) is value
        return
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(value, "d" * 64, request)


def test_a_path_on_any_other_action_refuses(core_request):
    request = dict(core_request, action="rbd", rbd={"dataPool": "", "features": ["layering"], "name": "disk", "pool": "data", "sizeBytes": GIB})
    value = dict(core_fixtures.observation(request), request="d" * 64, resource={"id": "abc", "path": "/volumes/_nogroup/docs/" + UUIDS[0]})
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(value, "d" * 64, request)


def test_subvolume_apply_calls_the_subvolume_phase(core_request, monkeypatch):
    request = core_subvolume_request(core_request)
    calls, path = [], "/volumes/_nogroup/docs/" + UUIDS[0]

    class CoreTransport:
        def call(self, host, phase, timeout=None):
            del timeout
            calls.append(phase)
            if phase == "observe":
                done = "subvolume" in calls
                return dict(core_fixtures.observation(request, ready=done), request="", removed=False, exists=done, partial=not done,
                            resource={"id": UUIDS[0], "path": path} if done else {"id": "", "path": ""})
            if phase == "host_state":
                return {"name": host["name"], "owned": True, "exists": True}
            return {"changed": phase == "subvolume"}

    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    result = ceph_core.execute(request, "apply", "d" * 64, CoreTransport())
    assert result["changed"] and result["evidence"]["resource"] == {"id": UUIDS[0], "path": path}
    assert calls.count("subvolume") == 1 and calls.index("observe") < calls.index("subvolume") < len(calls) - 1
