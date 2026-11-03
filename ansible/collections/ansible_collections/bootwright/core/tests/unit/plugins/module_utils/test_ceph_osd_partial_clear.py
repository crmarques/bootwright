"""An intent-marked OSD disk holding only this operation's own unfinished ceph-volume volume is cleared once; any other partial
shape refuses osd-partial after read-only probes alone."""

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request

GROUP = "ceph-0f6c1c6e-3c51-4a59-9a43-7a0e5b7c2d11"
BLOCK = "osd-block-6da65e47-8789-4a1c-88e8-3bbd195719a1"
PV_UUID = "AbCdEf-1234-5678-9aBc-DeF0-1234-567890"
LV_PATH = "/dev/" + GROUP + "/" + BLOCK
MAPPER = "/dev/mapper/" + GROUP.replace("-", "--") + "-" + BLOCK.replace("-", "--")
OSD = "6da65e47-8789-4a1c-88e8-3bbd195719a1"
ALIAS = "/dev/disk/by-id/virtio-disk1"
READ_ONLY = ("/usr/bin/lsblk", "/usr/sbin/wipefs", "/usr/sbin/lvs", "/usr/sbin/pvs")


def clear_record(native, osd=OSD):
    return {"version": "ceph-osd-device-clear-v1", "owner": native.request["owner"], "fsid": native.request["fsid"],
            "host": native.host["name"], "device": ALIAS, "serial": "disk1", "sizeBytes": 1024 ** 3, "pvUUID": PV_UUID, "osdUUID": osd}


@pytest.fixture
def host(core_request, monkeypatch, tmp_path):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=fixtures.Markers())
    native.prove_registry = lambda: None
    tools = []
    native.verify_tool = lambda: tools.append(True)
    state = {"disk": {"path": "/dev/vdb", "type": "disk", "serial": "disk1", "size": 1024 ** 3, "fstype": None, "mountpoints": []},
             "signatures": [], "lvs": [], "pvs": [], "zap": 0, "zapResult": (0, b"")}
    calls = []
    aliases = {"/dev/disk/by-id/virtio-disk1": "/dev/vdb", LV_PATH: "/dev/dm-3", MAPPER: "/dev/dm-3"}
    monkeypatch.setattr(ceph_native.os.path, "realpath", lambda path: aliases.get(path, path))
    proc = tmp_path / "proc"
    proc.mkdir()
    monkeypatch.setattr(ceph_native, "PROC_ROOT", str(proc))
    devices_file = tmp_path / "system.devices"
    monkeypatch.setattr(ceph_native, "LVM_DEVICES_FILE", str(devices_file))

    def clear():
        state["disk"].update(fstype=None, children=[])
        state["signatures"][:] = []
        state["lvs"][:] = []
        state["pvs"][:] = []

    def run(argv, data=b"", timeout=60):
        del data
        calls.append((list(argv), timeout))
        if argv[0] == "/usr/bin/lsblk":
            return 0, ceph_native.canonical({"blockdevices": [copy.deepcopy(state["disk"])]})
        if argv[0] == "/usr/sbin/wipefs":
            assert "--no-act" in argv
            return 0, ceph_native.canonical({"signatures": list(state["signatures"])})
        if argv[0] == "/usr/sbin/lvs":
            return 0, ceph_native.canonical({"report": [{"lv": copy.deepcopy(state["lvs"])}]})
        if argv[0] == "/usr/sbin/pvs":
            return 0, ceph_native.canonical({"report": [{"pv": copy.deepcopy(state["pvs"])}]})
        if argv[0] == native.tool:
            state["zap"] += 1
            if state["zapResult"][0] == 0:
                clear()
            return state["zapResult"][0], ceph_native.NativeOutput(b"", state["zapResult"][1])
        if argv[0] == "/usr/sbin/lvmdevices":
            devices_file.write_bytes(b"# LVM uses devices listed in this file.\nVERSION=1.1.2\n")
            devices_file.chmod(0o644)
            return 0, b""
        raise AssertionError(argv)

    native.run = run
    return native, state, calls, tools, proc, devices_file


def partial(native, state, *, tags="ceph.type=block", volume=True):
    """An intent-marked disk whose ceph-volume creation stopped before tagging its OSD."""
    native.markers.records["osd-device-intent.json"] = native.osd_device_scope()
    state["disk"]["fstype"] = "LVM2_member"
    state["signatures"][:] = [{"type": "LVM2_member"}]
    state["pvs"][:] = [{"pv_name": "/dev/vdb", "pv_uuid": PV_UUID, "vg_name": GROUP}]
    if volume:
        state["disk"]["children"] = [{"path": MAPPER, "type": "lvm", "fstype": None, "mountpoints": []}]
        state["lvs"][:] = [{"lv_path": LV_PATH, "lv_uuid": "synthetic-lv-uuid", "devices": "/dev/vdb(0)", "lv_tags": tags}]


def zap_argv(native):
    return [native.tool, "--image", native.request["image"], "ceph-volume", "--fsid", native.request["fsid"], "--",
            "lvm", "zap", "--destroy", "/dev/vdb"]


def native_calls(calls):
    return [argv for argv, unused in calls if argv[0] not in READ_ONLY]


def test_the_owned_partial_shape_is_zapped_once_between_its_intent_and_completion(host):
    native, state, calls, tools, unused_proc, unused_file = host
    partial(native, state, tags="ceph.type=block,ceph.vdo=0,ceph.cluster_fsid=" + native.request["fsid"])
    before = set(native.markers.records)
    assert native.devices() == {"devices": 1}
    assert set(native.markers.records) == before and native_calls(calls) == []
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    intent, done = native.osd_clear_marker(ALIAS), native.osd_cleared_marker(ALIAS)
    assert native.markers.writes == [intent, done] and native.markers.records[intent] == native.markers.records[done] == clear_record(native)
    assert native_calls(calls) == [zap_argv(native)] and tools == [True]
    assert [timeout for argv, timeout in calls if argv[0] == native.tool] == [ceph_native.OSD_CLEAR_SECONDS]
    zapped = next(index for index, (argv, unused) in enumerate(calls) if argv[0] == native.tool)
    assert [argv[0] for argv, unused in calls[zapped + 1:]][:2] == ["/usr/bin/lsblk", "/usr/sbin/wipefs"]
    assert native.markers.read(native.osd_identity_marker("/dev/vdb")) is None
    assert native.osd_prepare() == {"changed": False, "orphans": [OSD]}
    assert state["zap"] == 1


def test_an_untagged_volume_is_cleared_and_names_its_osd_for_the_purge(host):
    native, state, unused_calls, unused_tools, unused_proc, unused_file = host
    partial(native, state, tags="")
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    assert state["zap"] == 1


def refuses(native, state, calls, token="osd-partial"):
    before = copy.deepcopy(native.markers.records)
    with pytest.raises(ceph_native.Refusal, match="^" + token + "$"):
        native.osd_prepare()
    assert native.markers.records == before and native_calls(calls) == [] and state["zap"] == 0


@pytest.mark.parametrize("damage", ["osd-id", "second-volume", "second-pv", "foreign-volume-group", "foreign-volume-name", "mounted",
                                    "held-volume", "inactive-volume", "undeclared", "retained-identity", "foreign-tag", "foreign-type",
                                    "volume-tool-running", "already-cleared", "orphan-pv", "other-signature-count", "volume-group-only",
                                    "db-devices", "wal-devices", "recorded-osd", "moved-disk"])
def test_every_other_partial_shape_refuses_after_read_only_probes(host, damage):
    native, state, calls, unused_tools, proc, unused_file = host
    partial(native, state)
    if damage == "osd-id":
        state["lvs"][0]["lv_tags"] += ",ceph.osd_id=3,ceph.cluster_fsid=" + native.request["fsid"]
    elif damage == "second-volume":
        state["lvs"].append(dict(state["lvs"][0], lv_path="/dev/" + GROUP + "/" + BLOCK.replace("6da6", "7da6"), lv_uuid="other"))
        state["disk"]["children"].append({"path": "/dev/mapper/other", "type": "lvm", "mountpoints": []})
    elif damage == "second-pv":
        state["pvs"].append({"pv_name": "/dev/vdz", "pv_uuid": PV_UUID.replace("A", "B"), "vg_name": GROUP})
    elif damage == "foreign-volume-group":
        state["pvs"][0]["vg_name"] = "data"
        state["lvs"][0]["lv_path"] = "/dev/data/" + BLOCK
    elif damage == "foreign-volume-name":
        state["lvs"][0]["lv_path"] = "/dev/" + GROUP + "/data"
    elif damage == "mounted":
        state["disk"]["children"][0]["mountpoints"] = ["/srv"]
    elif damage == "held-volume":
        state["disk"]["children"][0]["children"] = [{"path": "/dev/mapper/synthetic-lv-uuid", "type": "crypt", "mountpoints": []}]
    elif damage == "inactive-volume":
        state["disk"]["children"] = []
    elif damage == "undeclared":
        native.request["services"][-1]["spec"]["data_devices"]["paths"] = ["/dev/vdx"]
    elif damage == "retained-identity":
        native.markers.records[native.osd_identity_marker("/dev/vdb")] = {"path": "/dev/vdb", "id": 3, "uuid": "u", "lvUUID": "l"}
    elif damage == "foreign-tag":
        state["lvs"][0]["lv_tags"] += ",owner=someone"
    elif damage == "foreign-type":
        state["lvs"][0]["lv_tags"] = "ceph.type=db"
    elif damage == "volume-tool-running":
        (proc / "4242").mkdir()
        (proc / "4242" / "cmdline").write_bytes(b"/usr/bin/python3\0/usr/sbin/ceph-volume\0lvm\0batch\0")
    elif damage == "already-cleared":
        native.markers.records[native.osd_cleared_marker(ALIAS)] = clear_record(native)
    elif damage == "volume-group-only":
        state["lvs"][:] = []
        state["disk"]["children"] = []
    elif damage in ("db-devices", "wal-devices"):
        spec = dict(native.request["services"][-1]["spec"], **{damage.replace("-", "_"): {"paths": ["/dev/vdc"]}})
        native.osd_data_specs = lambda actual: [spec]
    elif damage == "recorded-osd":
        original = native.devices

        def devices():
            result = original()
            native.observed_device_ids.append({"path": "/dev/vdz", "id": 3, "uuid": OSD, "lvUUID": "l"})
            return result

        native.devices = devices
    elif damage == "moved-disk":
        native.markers.records[native.osd_clear_marker(ALIAS)] = dict(clear_record(native), serial="disk9")
    elif damage == "orphan-pv":
        state["pvs"][0]["vg_name"] = ""
        state["lvs"][:] = []
        state["disk"]["children"] = []
    else:
        state["signatures"].append({"type": "LVM2_member"})
    refuses(native, state, calls, "foreign-osd" if damage == "osd-id" else "osd-partial")


def test_another_clusters_volume_and_a_missing_intent_keep_their_own_refusals(host):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    partial(native, state, tags="ceph.type=block,ceph.cluster_fsid=11111111-1111-4111-8111-111111111111")
    refuses(native, state, calls, "foreign-osd")
    del native.markers.records["osd-device-intent.json"]
    with pytest.raises(ceph_native.Refusal, match="^device-not-empty$"):
        native.osd_prepare()
    assert native_calls(calls) == [] and state["zap"] == 0


def test_a_failed_zap_refuses_by_name_and_keeps_its_intent_for_the_replay(host):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    partial(native, state)
    state["zapResult"] = (1, b"--> RuntimeError: Unable to proceed")
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-failed$") as refused:
        native.osd_prepare()
    assert "Unable to proceed" in refused.value.native_error["cause"]
    intent = native.osd_clear_marker(ALIAS)
    assert native.markers.writes == [intent] and native.osd_cleared_marker(ALIAS) not in native.markers.records
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == "osd-clear-failed"
    state["zapResult"] = (0, b"")
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    assert state["zap"] == 2 and native_calls(calls) == [zap_argv(native)] * 2


@pytest.mark.parametrize("left", ["volume-group-only", "orphan-pv", "empty"])
def test_an_interrupted_clear_replays_from_its_intent_and_an_empty_disk_settles_without_a_zap(host, left):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    partial(native, state)
    record = clear_record(native)
    native.markers.records[native.osd_clear_marker(ALIAS)] = record
    state["lvs"][:] = []
    state["disk"]["children"] = []
    if left == "orphan-pv":
        state["pvs"][0]["vg_name"] = ""
    elif left == "empty":
        state["disk"]["fstype"] = None
        state["signatures"][:] = []
        state["pvs"][:] = []
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    assert state["zap"] == (0 if left == "empty" else 1)
    assert native.markers.read(native.osd_cleared_marker(ALIAS)) == record
    assert native.osd_prepare() == {"changed": False, "orphans": [OSD]} and len(native_calls(calls)) == state["zap"]


def test_a_replay_refuses_another_pv_under_a_retained_intent(host):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    partial(native, state)
    native.markers.records[native.osd_clear_marker(ALIAS)] = dict(clear_record(native), pvUUID=PV_UUID.replace("A", "Z"))
    refuses(native, state, calls)


@pytest.mark.parametrize("listed", [False, True])
def test_only_the_zapped_pvs_own_devices_file_entry_is_removed(host, listed):
    native, state, calls, unused_tools, unused_proc, devices_file = host
    identifier = PV_UUID.replace("-", "")
    other = b"IDTYPE=sys_wwid IDNAME=naa.root DEVNAME=/dev/sda2 PVID=" + b"r" * 32 + b"\n"
    entry = b"IDTYPE=devname IDNAME=/dev/vdb DEVNAME=/dev/vdb PVID=" + identifier.encode() + b"\n"
    devices_file.write_bytes(b"VERSION=1.1.1\n" + other + (entry if listed else b""))
    devices_file.chmod(0o644)
    partial(native, state)
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    removal = [argv for argv in native_calls(calls) if argv[0] == "/usr/sbin/lvmdevices"]
    assert removal == ([["/usr/sbin/lvmdevices", "--yes", "--delpvid", identifier]] if listed else [])


def test_a_devices_file_entry_that_survives_its_removal_refuses_unproved(host):
    native, state, unused_calls, unused_tools, unused_proc, devices_file = host
    identifier = PV_UUID.replace("-", "").encode()
    devices_file.write_bytes(b"VERSION=1.1.1\nIDTYPE=devname IDNAME=/dev/vdb DEVNAME=/dev/vdb PVID=" + identifier + b"\n")
    devices_file.chmod(0o644)
    original = native.run

    def run(argv, data=b"", timeout=60):
        if argv[0] == "/usr/sbin/lvmdevices":
            return 0, b""
        return original(argv, data=data, timeout=timeout)

    native.run = run
    partial(native, state)
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-unproved$"):
        native.osd_prepare()
    assert native.osd_cleared_marker(ALIAS) not in native.markers.records


def test_a_disk_that_does_not_read_empty_after_its_zap_refuses_unproved(host):
    native, state, unused_calls, unused_tools, unused_proc, unused_file = host
    partial(native, state)
    original = native.run

    def run(argv, data=b"", timeout=60):
        if argv[0] == native.tool:
            state["zap"] += 1
            state["lvs"][:] = []
            state["disk"]["children"] = []
            return 0, b""
        return original(argv, data=data, timeout=timeout)

    native.run = run
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-unproved$"):
        native.osd_prepare()
    assert native.osd_cleared_marker(ALIAS) not in native.markers.records


def test_the_partial_refusals_reach_the_cluster_block_by_name():
    for token in ("osd-partial", "osd-clear-failed", "osd-clear-unproved"):
        error = ceph_native.RemoteRefusal("native-refusal:" + token + "@ceph_native.owned_partial_osd:1")
        assert ceph_core.refusal(error, ceph_core.CLUSTER_REASONS) == (token, error.detail)


def test_a_zap_waits_for_an_unchanged_re_read_of_the_disks_volumes(host):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    partial(native, state)
    original = native.run
    reads = []

    def run(argv, data=b"", timeout=60):
        if argv[0] == "/usr/sbin/pvs":
            reads.append(True)
            if len(reads) == 2:
                state["pvs"].append({"pv_name": "/dev/vdz", "pv_uuid": PV_UUID.replace("A", "Q"), "vg_name": GROUP})
        return original(argv, data=data, timeout=timeout)

    native.run = run
    with pytest.raises(ceph_native.Refusal, match="^osd-partial$"):
        native.osd_prepare()
    assert state["zap"] == 0 and native_calls(calls) == [] and len(reads) == 2


def test_a_devices_file_entry_stays_while_a_pv_of_its_uuid_remains(host):
    native, state, calls, unused_tools, unused_proc, devices_file = host
    identifier = PV_UUID.replace("-", "").encode()
    devices_file.write_bytes(b"VERSION=1.1.1\nIDTYPE=devname IDNAME=/dev/vdb DEVNAME=/dev/vdb PVID=" + identifier + b"\n")
    devices_file.chmod(0o644)
    partial(native, state)
    original = native.run

    def run(argv, data=b"", timeout=60):
        answer = original(argv, data=data, timeout=timeout)
        if argv[0] == native.tool:
            state["pvs"][:] = [{"pv_name": "/dev/vdq", "pv_uuid": PV_UUID, "vg_name": ""}]
        return answer

    native.run = run
    with pytest.raises(ceph_native.Refusal, match="^osd-clear-unproved$"):
        native.osd_prepare()
    assert native_calls(calls) == [zap_argv(native)]


def bootstrap_worker(core_request, osds):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=fixtures.Markers())
    effects = []

    def ceph(*args, json_result=True, **kwargs):
        if args == ("osd", "dump"):
            return {"osds": copy.deepcopy(osds)}
        if args[:2] == ("osd", "purge-new"):
            effects.append(args)
            osds[:] = [item for item in osds if "osd." + str(item["osd"]) != args[2]]
            return b""
        raise AssertionError(args)

    native.ceph = ceph
    return native, effects


def osd_entry(osd_id, uuid, *, booted=False):
    return {"osd": osd_id, "uuid": uuid, "up": int(booted), "in": int(booted), "up_from": 12 if booted else 0,
            "state": ["exists", "up"] if booted else ["exists", "new"]}


def test_the_purge_removes_only_the_never_booted_osd_of_the_zapped_volume_and_settles_from_the_map(core_request):
    osds = [osd_entry(0, "11111111-1111-4111-8111-111111111111", booted=True), osd_entry(4, OSD)]
    native, effects = bootstrap_worker(core_request, osds)
    assert native.purge_orphan_osds([OSD]) is True
    assert effects == [("osd", "purge-new", "osd.4", "--yes-i-really-mean-it")]
    purge = {"version": "ceph-osd-device-clear-v1", "fsid": native.request["fsid"], "osdUUID": OSD, "id": 4}
    assert native.markers.read(native.osd_purge_marker(OSD)) == purge
    assert native.purge_orphan_osds([OSD]) is False and len(effects) == 1
    assert [item["osd"] for item in osds] == [0]


def test_a_booted_osd_of_the_zapped_volumes_fsid_is_never_purged(core_request):
    native, effects = bootstrap_worker(core_request, [osd_entry(4, OSD, booted=True)])
    with pytest.raises(ceph_native.Refusal, match="^osd-orphan-booted$"):
        native.purge_orphan_osds([OSD])
    assert effects == [] and native.markers.writes == []


def test_a_purge_the_map_does_not_show_refuses_unproved(core_request):
    native, effects = bootstrap_worker(core_request, [osd_entry(4, OSD)])
    original = native.ceph

    def ceph(*args, **kwargs):
        return b"" if args[:2] == ("osd", "purge-new") else original(*args, **kwargs)

    native.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="^osd-orphan-unproved$"):
        native.purge_orphan_osds([OSD])
    assert effects == []


@pytest.mark.parametrize("orphans", [[], ["not-a-uuid"], [OSD, OSD]])
def test_the_services_envelope_admits_only_distinct_osd_fsids(core_request, orphans):
    envelope = {"request": core_request, "host": core_request["bootstrap"], "phase": "services", "private": {}, "orphans": orphans}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(envelope)


def test_the_cluster_block_gathers_each_hosts_orphans_for_the_services_call():
    other = "7da65e47-8789-4a1c-88e8-3bbd195719a1"
    assert ceph_core.prepared_orphans([{"changed": True, "orphans": [OSD]}, {"changed": False}, {"changed": False, "orphans": [other, OSD]}]) == [OSD, other]
    assert ceph_core.prepared_orphans([{"changed": False}]) == []
    for damaged in ({"changed": True, "orphans": ["bad"]}, {"changed": True, "extra": 1}, {"orphans": [OSD]}):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.prepared_orphans([damaged])


RECREATED = "8da65e47-8789-4a1c-88e8-3bbd195719a1"


RECREATED_PV = "ZyXwVu-4321-8765-0zYx-WvU9-8765-432109"


def recreated(native, state, osd=RECREATED, *, completed=True, pv=RECREATED_PV):
    """The disk a clear zapped, on which the OSD service created a complete OSD again; without completed, the call that zapped
    it stopped before it recorded the clear's completion."""
    native.markers.records["osd-device-intent.json"] = native.osd_device_scope()
    native.markers.records[native.osd_clear_marker(ALIAS)] = clear_record(native)
    if completed:
        native.markers.records[native.osd_cleared_marker(ALIAS)] = clear_record(native)
    state["pvs"][:] = [{"pv_name": "/dev/vdb", "pv_uuid": pv, "vg_name": "ceph-new"}]
    path = "/dev/ceph-new/osd-block-" + osd
    state["disk"].update(fstype="LVM2_member", children=[{"path": path, "type": "lvm", "fstype": "ceph_bluestore", "mountpoints": []}])
    state["signatures"][:] = [{"type": "LVM2_member"}]
    state["lvs"][:] = [{"lv_path": path, "lv_uuid": "recreated-lv-uuid", "devices": "/dev/vdb(0)",
                        "lv_tags": "ceph.cluster_fsid=" + native.request["fsid"] + ",ceph.type=block,ceph.osd_id=5,ceph.osd_fsid=" + osd}]


def test_a_completed_clear_still_names_its_orphan_once_the_disk_holds_an_osd_again(host):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    recreated(native, state)
    assert native.osd_prepare() == {"changed": True, "orphans": [OSD]}
    assert native.markers.read(native.osd_identity_marker("/dev/vdb"))["uuid"] == RECREATED
    assert native.osd_prepare() == {"changed": False, "orphans": [OSD]}
    assert state["zap"] == 0 and native_calls(calls) == []


def test_an_osd_the_disk_holds_is_never_named_as_the_orphan(host):
    native, state, unused_calls, unused_tools, unused_proc, unused_file = host
    recreated(native, state, osd=OSD)
    assert native.osd_prepare() == {"changed": True}


def test_a_clear_interrupted_after_its_zap_completes_once_the_disk_holds_another_osd_and_its_orphan_is_purged(host, core_request):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    recreated(native, state, completed=False)
    before = len(native.markers.writes)
    prepared = native.osd_prepare()
    assert prepared == {"changed": True, "orphans": [OSD]}
    assert native.markers.read(native.osd_cleared_marker(ALIAS)) == clear_record(native)
    assert native.markers.writes[before:] == [native.osd_cleared_marker(ALIAS), native.osd_identity_marker("/dev/vdb")]
    assert native.osd_prepare() == {"changed": False, "orphans": [OSD]} and len(native.markers.writes) == before + 2
    assert state["zap"] == 0 and native_calls(calls) == []
    bootstrap, effects = bootstrap_worker(core_request, [osd_entry(4, OSD), osd_entry(5, RECREATED, booted=True)])
    assert bootstrap.purge_orphan_osds(ceph_core.prepared_orphans([prepared])) is True
    assert effects == [("osd", "purge-new", "osd.4", "--yes-i-really-mean-it")]


@pytest.mark.parametrize("same", ["osd-fsid", "pv"])
def test_a_clear_without_its_completion_never_completes_on_the_disks_own_osd_or_pv(host, same):
    native, state, calls, unused_tools, unused_proc, unused_file = host
    recreated(native, state, OSD if same == "osd-fsid" else RECREATED, completed=False, pv=PV_UUID if same == "pv" else RECREATED_PV)
    assert native.osd_prepare() == {"changed": True}
    assert native.markers.read(native.osd_cleared_marker(ALIAS)) is None and state["zap"] == 0 and native_calls(calls) == []


def orphan_observation(core_request, metadata=None, entry=None):
    """Every declared OSD is up and owned, and the map also holds the OSD an interrupted creation allocated as ID 3."""
    core_request["config"] = fixtures.memory_config()
    answers = fixtures.memory_observation_answers(core_request)
    answers[("osd", "metadata")].append({"id": 3} if metadata is None else metadata)
    if entry is not False:
        answers[("osd", "dump")]["osds"].append(osd_entry(3, OSD) if entry is None else entry)
    native, unused = fixtures.worker(core_request, answers)
    return native, answers


def test_the_orphan_of_a_cleared_disk_keeps_the_cluster_unready_and_the_next_services_call_purges_it(core_request):
    native, answers = orphan_observation(core_request)
    result = native.observe()
    assert result["exists"] and result["partial"] and result["ready"] is False
    assert [osd["id"] for osd in result["osds"]] == [0, 1, 2] and native.markers.writes == []
    entry = "unbooted@osd.3." + OSD
    assert native.blocking[0] == entry and ceph_core.blocking_entry(entry) and ceph_core.readiness_detail([entry]) == "not-ready:" + entry
    original = native.ceph

    def ceph(*args, **kwargs):
        if args[:2] != ("osd", "purge-new"):
            return original(*args, **kwargs)
        assert args == ("osd", "purge-new", "osd.3", "--yes-i-really-mean-it")
        answers[("osd", "metadata")].pop()
        answers[("osd", "dump")]["osds"].pop()
        return b""

    native.ceph = ceph
    assert native.purge_orphan_osds(ceph_core.prepared_orphans([{"changed": False, "orphans": [OSD]}, {"changed": False}])) is True
    assert native.observe()["ready"] is True and not any(value.startswith("unbooted@") for value in native.blocking)


@pytest.mark.parametrize("damage", ["device-ids", "devices", "booted", "was-up", "not-new", "unmapped"])
def test_every_other_osd_beyond_the_declared_disks_still_refuses_foreign_osd(core_request, damage):
    metadata, entry = None, None
    if damage == "device-ids":
        metadata = {"id": 3, "hostname": core_request["hosts"][0]["name"], "device_ids": "vdb=foreign"}
    elif damage == "devices":
        metadata = {"id": 3, "devices": "vdb"}
    elif damage == "booted":
        entry = osd_entry(3, OSD, booted=True)
    elif damage == "was-up":
        entry = dict(osd_entry(3, OSD), up_from=12)
    elif damage == "not-new":
        entry = dict(osd_entry(3, OSD), state=["exists"])
    else:
        entry = False
    native, unused = orphan_observation(core_request, metadata, entry)
    with pytest.raises(ceph_native.Refusal, match="^foreign-osd$"):
        native.observe()
