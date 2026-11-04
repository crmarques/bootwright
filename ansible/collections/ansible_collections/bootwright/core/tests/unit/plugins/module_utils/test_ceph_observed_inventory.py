"""An observation returns the values it compares: a pool block its pool's replica pair, applications, autoscale mode and
quota, and a cluster block the bounded pool inventory and each booted OSD on a declared host at a device no declared service places.

Native shapes: `osd pool ls detail` keys (src/osd/osd_types.cc:1568-1653); `osd metadata` names a booted OSD's data device in
`devices` (src/os/bluestore/BlueStore.cc, collect_metadata) and its host in `hostname`.
"""

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request

ADDED = "7da65e47-8789-4a1c-88e8-3bbd195719a2"


def listed(name, **changes):
    entry = {"pool_name": name, "pool_id": 8, "type": 1, "size": 3, "min_size": 2, "pg_autoscale_mode": "on", "quota_max_bytes": 0,
             "quota_max_objects": 0, "application_metadata": {"rbd": {}}}
    entry.update(changes)
    return entry


def reading_worker(core_request, pools=None, extra=None):
    core_request["config"] = fixtures.memory_config()
    answers = fixtures.memory_observation_answers(core_request)
    answers[("osd", "pool", "ls", "detail")] = pools if pools is not None else []
    for metadata, entry in extra or []:
        answers[("osd", "metadata")].append(metadata)
        answers[("osd", "dump")]["osds"].append(entry)
    native, calls = fixtures.worker(core_request, answers)
    native.reading = True
    return native, answers, calls


def added_osd(host="node-1", device="vdc", osd_id=3):
    metadata = {"id": osd_id, "hostname": host, "devices": device, "device_ids": device + "=disk9"}
    return metadata, {"osd": osd_id, "up": 1, "in": 1, "uuid": ADDED}


def test_the_pool_observation_returns_the_values_it_compares(core_request):
    core_request.update(action="pool", pool=fixtures.replicated_pool())
    marks = fixtures.Markers()
    native = fixtures.observed_pool(core_request, marks)
    marks.records["pool-id-volumes.json"] = {"id": 8}
    evidence = native.empty_evidence()
    evidence["exists"] = True
    assert native.observe_resource(evidence, True) is True
    assert evidence["pool"] == {"name": "volumes", "type": "replicated", "size": 2, "minSize": 2, "applications": ["rbd"], "autoscale": "on",
                                "maxBytes": 0, "maxObjects": 0}
    ceph_core.pool_evidence(evidence["pool"])


def test_a_changed_size_reads_back_and_keeps_the_pool_unready(core_request):
    core_request.update(action="pool", pool=fixtures.replicated_pool())
    marks = fixtures.Markers()
    native = fixtures.observed_pool(core_request, marks)
    marks.records["pool-id-volumes.json"] = {"id": 8}
    original = native.pools
    native.pools = lambda: [dict(entry, size=4, quota_max_bytes=1 << 40) for entry in original()]
    evidence = native.empty_evidence()
    assert native.observe_resource(evidence, True) is False
    assert evidence["pool"]["size"] == 4 and evidence["pool"]["maxBytes"] == 1 << 40


@pytest.mark.parametrize("entry,expected", [
    (listed("x", type=2), {"type": ""}),
    (listed("x", size="3", min_size=-1), {"size": 0, "minSize": 0}),
    (listed("x", pg_autoscale_mode="sometimes"), {"autoscale": ""}),
    (listed("x", application_metadata={"rbd": {}, "cephfs": {}, "bad name": {}}), {"applications": ["cephfs", "rbd"]}),
    (listed("x", application_metadata=[]), {"applications": []}),
    (listed("x", quota_max_objects=1 << 64), {"maxObjects": 0}),
])
def test_a_pool_value_of_another_type_or_bound_reads_empty(entry, expected):
    reading = ceph_native.pool_reading(entry)
    assert {key: reading[key] for key in expected} == expected
    ceph_core.pool_evidence(reading)


def test_the_cluster_observation_inventories_its_pools_bounded_and_sorted(core_request):
    pools = [listed(name) for name in ("zeta", ".mgr", "objects.rgw.log", "by-hand")]
    pools += [listed("x" * 64), listed("bad/name"), {"pool_id": 9}]
    native, unused, calls = reading_worker(core_request, pools)
    result = native.observe()
    inventory = result["inventory"]
    assert [pool["name"] for pool in inventory["pools"]] == [".mgr", "by-hand", "objects.rgw.log", "zeta"]
    assert inventory["omitted"] == 3 and inventory["osds"] == [] and result["ready"] is True
    assert any(argv[-4:] == ["osd", "pool", "ls", "detail"] for argv in calls)
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)


def test_the_inventory_keeps_at_most_its_bound_of_pools(core_request):
    pools = [listed("pool-" + str(index).zfill(3)) for index in range(ceph_native.POOL_LIMIT + 5)]
    native, unused, unused_calls = reading_worker(core_request, pools)
    inventory = native.observe()["inventory"]
    assert len(inventory["pools"]) == ceph_native.POOL_LIMIT and inventory["omitted"] == 5


def test_duplicate_pool_names_refuse_as_native_output(core_request):
    native, unused, unused_calls = reading_worker(core_request, [listed("same"), listed("same")])
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.observe()


def test_only_the_unrecorded_observation_lists_pools(core_request):
    native, unused, calls = reading_worker(core_request)
    native.reading = False
    assert "inventory" not in native.observe()
    assert not any(argv[-4:] == ["osd", "pool", "ls", "detail"] for argv in calls)


def test_an_osd_added_on_one_host_is_reported_and_keeps_the_cluster_unready(core_request):
    native, unused, unused_calls = reading_worker(core_request, extra=[added_osd()])
    result = native.observe()
    assert result["inventory"]["osds"] == [{"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ADDED}]
    assert [osd["id"] for osd in result["osds"]] == [0, 1, 2]
    assert result["ready"] is False and result["partial"] is True
    assert native.blocking[0] == "unplaced@osd.3" and ceph_core.blocking_entry("unplaced@osd.3")
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)


def test_every_phase_but_the_unrecorded_observation_still_refuses_an_added_osd(core_request):
    native, unused, unused_calls = reading_worker(core_request, extra=[added_osd()])
    native.reading = False
    with pytest.raises(ceph_native.Refusal, match="^foreign-osd$"):
        native.observe()


def damaged_osd(damage):
    metadata, entry = added_osd()
    if damage == "declared-device":
        metadata.update(devices="vdb", device_ids="vdb=foreign")
    elif damage == "foreign-host":
        metadata["hostname"] = "node-9"
    elif damage == "two-devices":
        metadata["devices"] = "vdc,vdd"
    elif damage == "unbooted-metadata":
        del metadata["device_ids"]
    else:
        entry["uuid"] = "not-a-uuid"
    return metadata, entry


DAMAGED = {
    "declared-device": {"host": "node-1", "id": 3, "path": "", "uuid": ADDED},
    "foreign-host": {"host": "", "id": 3, "path": "", "uuid": ADDED},
    "two-devices": {"host": "node-1", "id": 3, "path": "", "uuid": ADDED},
    "unbooted-metadata": {"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ADDED},
    "bad-uuid": {"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ""},
}


@pytest.mark.parametrize("damage", sorted(DAMAGED))
def test_an_osd_no_rule_places_reads_partial_with_what_is_known_while_unrecorded(core_request, damage):
    native, unused, unused_calls = reading_worker(core_request, extra=[damaged_osd(damage)])
    result = native.observe()
    assert result["inventory"]["osds"] == [DAMAGED[damage]]
    assert result["ready"] is False and result["partial"] is True and native.blocking[0] == "unplaced@osd.3"
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)


@pytest.mark.parametrize("damage", sorted(DAMAGED))
def test_a_recorded_observation_still_refuses_an_osd_no_rule_places(core_request, damage):
    native, unused, unused_calls = reading_worker(core_request, extra=[damaged_osd(damage)])
    native.reading = False
    with pytest.raises(ceph_native.Refusal, match="^foreign-osd$"):
        native.observe()


def test_an_unplaced_osd_never_repeats_an_osd_fsid_the_observation_already_holds(core_request):
    metadata, entry = added_osd()
    native, unused, unused_calls = reading_worker(core_request, extra=[(metadata, entry)])
    entry["uuid"] = next(osd["uuid"] for osd in native.observe()["osds"])
    result = native.observe()
    assert result["inventory"]["osds"] == [{"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ""}]
    result["request"] = "d" * 64
    ceph_core.evidence(result, "d" * 64, core_request)


def test_unplaced_osds_beyond_the_bound_refuse_foreign_osd(core_request):
    extra = [added_osd(device="vd" + str(index), osd_id=3 + index) for index in range(ceph_native.UNPLACED_LIMIT + 1)]
    native, unused, unused_calls = reading_worker(core_request, extra=extra)
    with pytest.raises(ceph_native.Refusal, match="^foreign-osd$"):
        native.observe()


def evidence_with(core_request, **members):
    value = fixtures.observation(core_request, ready=False)
    value.update(removed=False, request="d" * 64, **members)
    return value


@pytest.mark.parametrize("change", [
    lambda inventory: inventory.update(extra=1),
    lambda inventory: inventory.update(omitted=-1),
    lambda inventory: inventory["pools"].append(dict(inventory["pools"][0])),
    lambda inventory: inventory["pools"].insert(0, ceph_native.pool_reading(listed("zz"))),
    lambda inventory: inventory["pools"][0].update(name="bad/name"),
    lambda inventory: inventory["pools"][0].update(size=True),
    lambda inventory: inventory["pools"][0].update(applications=["rbd", "cephfs"]),
    lambda inventory: inventory["osds"][0].update(host="node-9"),
    lambda inventory: inventory["osds"][0].update(path="/etc/passwd"),
    lambda inventory: inventory["osds"][0].update(uuid="x"),
    lambda inventory: inventory["osds"].append(dict(inventory["osds"][0])),
    lambda inventory: inventory["osds"][0].update(host=""),
    lambda inventory: inventory["osds"][0].update(path="/dev/"),
])
def test_the_published_inventory_is_closed_and_bounded(core_request, change):
    inventory = {"pools": [ceph_native.pool_reading(listed("by-hand"))], "omitted": 0,
                 "osds": [{"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ADDED}]}
    ceph_core.evidence(evidence_with(core_request, inventory=copy.deepcopy(inventory)), "d" * 64, core_request)
    change(inventory)
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(evidence_with(core_request, inventory=inventory), "d" * 64, core_request)


@pytest.mark.parametrize("known", [
    {"host": "node-1", "path": "", "uuid": ADDED}, {"host": "", "path": "", "uuid": ""}, {"host": "node-1", "path": "/dev/vdc", "uuid": ""},
])
def test_the_published_inventory_admits_an_unplaced_osd_whose_host_device_or_fsid_is_unknown(core_request, known):
    inventory = {"pools": [], "omitted": 0, "osds": [dict({"id": 3}, **known)]}
    ceph_core.evidence(evidence_with(core_request, inventory=inventory), "d" * 64, core_request)


def test_a_pool_reading_belongs_to_the_pool_action_only(core_request):
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(evidence_with(core_request, pool=ceph_native.pool_reading(listed("volumes"))), "d" * 64, core_request)


class WorkerTransport:
    """Each phase on the fixture worker, as the native dispatch runs it: only the unrecorded observation reads unrecorded."""

    def __init__(self, native):
        self.native, self.phases = native, []

    def call(self, host, phase, timeout=None):
        del timeout
        self.phases.append(phase)
        if phase in ("observe", ceph_native.UNRECORDED_OBSERVE):
            self.native.reading = phase == ceph_native.UNRECORDED_OBSERVE
            return dict(self.native.observe(), blocking=list(self.native.blocking))
        assert phase == "host_state"
        return {"name": host["name"], "owned": True, "exists": True}


def test_a_recorded_observation_with_an_added_osd_still_refuses_foreign_osd(core_request):
    native, unused, unused_calls = reading_worker(core_request, extra=[added_osd()])
    transport = WorkerTransport(native)
    with pytest.raises(ceph_native.Refusal, match="^foreign-osd$"):
        ceph_core.execute(core_request, "observe", "d" * 64, transport, {})
    assert transport.phases == ["observe"]


def test_an_unrecorded_observation_reads_the_added_osd_partial_in_its_inventory(core_request):
    native, unused, unused_calls = reading_worker(core_request, [listed("by-hand")], extra=[added_osd()])
    transport = WorkerTransport(native)
    result = ceph_core.execute(core_request, "observe", "d" * 64, transport, {}, reading="unrecorded")
    evidence = result["evidence"]
    assert transport.phases[0] == ceph_native.UNRECORDED_OBSERVE
    assert evidence["partial"] is True and evidence["ready"] is False and result["pending"] == ["unplaced@osd.3"]
    assert evidence["inventory"]["osds"] == [{"host": "node-1", "id": 3, "path": "/dev/vdc", "uuid": ADDED}]
    assert [pool["name"] for pool in evidence["inventory"]["pools"]] == ["by-hand"]


@pytest.mark.parametrize("operation,reading,action", [
    ("apply", "unrecorded", "cluster"), ("observe", "recorded", "cluster"), ("observe", "unrecorded", "pool"), ("destroy", "unrecorded", "cluster"),
])
def test_only_a_cluster_observation_reads_unrecorded(core_request, operation, reading, action):
    core_request["action"] = action
    if action == "pool":
        core_request["pool"] = fixtures.replicated_pool()

    class Untouched:
        def call(self, *args, **kwargs):
            raise AssertionError("a refused reading reached a host")

    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(core_request, operation, "d" * 64, Untouched(), {}, reading=reading)
