"""Native daemon projections are not stored service specifications."""

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as fixtures

cluster_request = fixtures.cluster_request
native = fixtures.native
private = fixtures.private


@pytest.fixture
def projection_world(native, private):
    fixtures.prepared_bootstrap(native, private)
    daemons, hosts, stored, effects = [], {}, {}, []
    native.daemons = lambda: copy.deepcopy(daemons)
    native.markers.records["transport-ready.json"] = {"synthetic": True}

    def bootstrap(*args, **kwargs):
        del args, kwargs
        daemons.extend(fixtures.initial_daemons(native))

    native.command = bootstrap
    native.tunnel_bootstrap(fixtures.ssh_material(private))
    intent = staging.stage_intent(native.request)
    plan = staging.initial_redeploy_plan(native.request, intent, daemons, [], fixtures.metadata("mon"), fixtures.metadata("mgr"))
    native.markers.records["tunnel-redeploy.json"] = {"scope": native.manager_scope(plan), "custodyID": "8af1a044-a7e1-4605-b20e-1f191a394d31"}
    native.markers.records["tunnel-mounted.json"] = {"intent": intent, "custodyID": "8af1a044-a7e1-4605-b20e-1f191a394d31"}
    native.markers.records["cache-ready.json"] = {"synthetic": True}
    native.verify_transport_bundle = lambda: None
    native.verify_tunnel_configuration = lambda: None
    native.verify_manager_mounts = lambda observed, observing=False: []
    native.configuration_complete = lambda: True
    native.config_database = lambda: None
    groups = [native.service_spec(value) for value in native.request["services"]]
    # Native normalized inputs are retained before the old ownership guard fails.
    native.markers.records["service-inputs.json"] = {
        "version": "ceph-service-inputs-v1",
        "scope": {"owner": native.request["owner"], "fsid": native.request["fsid"],
                  "image": native.request["image"], "inputs": groups}, "services": groups}
    descriptors = {kind: {"service_type": kind, "service_name": kind, "unmanaged": True,
                          "status": {"size": 0, "running": 1}} for kind in ("mon", "mgr")}

    def ceph(*args, **kwargs):
        del kwargs
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("orch", "host", "ls"):
            return copy.deepcopy(list(hosts.values()))
        if args == ("orch", "ls", "--export"):
            return [copy.deepcopy({key: value for key, value in row.items() if key not in ("status", "events")})
                    for row in descriptors.values()]
        if args == ("orch", "ls"):
            return ceph_native.canonical(list(descriptors.values()))
        raise AssertionError(args)

    def effect(*args, **kwargs):
        assert args == ("orch", "apply", "-i", "-")
        value = json.loads(kwargs["data"])
        if value["service_type"] == "host":
            hosts[value["hostname"]] = {"hostname": value["hostname"], "addr": value["addr"], "labels": value["labels"]}
            return
        key = value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")
        effects.append(key)
        # Independently frozen native results, not an echo of submitted input.
        expected = next(item for group in groups for item in group
                        if item["service_type"] + ("." + item["service_id"] if item.get("service_id") else "") == key)
        assert value == expected
        stored[key] = copy.deepcopy(expected)
        descriptors[key] = dict(copy.deepcopy(expected), status={"created": "2025-01-01T00:00:00Z", "size": 3, "running": 1})

    native.ceph = ceph
    native.tunnel_effect = effect
    native.enroll()
    return native, descriptors, daemons, effects


@pytest.mark.parametrize("first", [0, 1, 2])
def test_seed_projections_resume_with_retained_inputs_and_partial_peer(projection_world, first):
    worker, unused, daemons, effects = projection_world
    devices = fixtures.device_rows(worker.request)
    worker.services(devices, service=first)
    if first in (0, 1):
        # Once managed, a peer need not retain its original bootstrap identity.
        daemons[first]["name"] += ".managed"
    worker.services(devices)
    assert len(effects) == 5
    assert worker.services(devices) == {"changed": False}


@pytest.mark.parametrize("after", [False, True])
def test_projection_lost_reply_preserves_attempt_and_resumes(projection_world, after):
    worker, unused, unused_daemons, effects = projection_world
    original = worker.tunnel_effect

    def lost(*args, **kwargs):
        if after:
            original(*args, **kwargs)
        raise ceph_native.Refusal("native-command")

    worker.tunnel_effect = lost
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        worker.services(fixtures.device_rows(worker.request))
    assert worker.markers.read("service-mon-attempt.json") is not None
    worker.tunnel_effect = original
    worker.services(fixtures.device_rows(worker.request))
    assert len(effects) == 5


@pytest.mark.parametrize("mutation", ["created", "deleted", "unknown", "running", "size", "shape", "complete", "attempt", "daemon", "extra"])
def test_projection_refusals_precede_every_effect(projection_world, mutation):
    worker, rows, daemons, effects = projection_world
    if mutation in ("created", "deleted"):
        rows["mgr"]["status"][mutation] = "2025-01-01T00:00:00Z"
    elif mutation == "unknown":
        rows["alien"] = {"service_type": "alien", "status": {"size": 0, "running": 1}}
    elif mutation in ("running", "size"):
        rows["mgr"]["status"][mutation] = True
    elif mutation == "shape":
        rows["mgr"]["placement"] = {"hosts": ["node-1"]}
    elif mutation in ("complete", "attempt"):
        worker.markers.records["service-mgr-" + mutation + ".json"] = {"foreign": True}
    elif mutation == "daemon":
        daemons[1]["container_image_name"] = "foreign"
    else:
        daemons.append(dict(daemons[1], name="mgr.extra"))
    with pytest.raises((ceph_native.Refusal, staging.StageRefusal)):
        worker.services(fixtures.device_rows(worker.request), service=2)
    assert effects == []


@pytest.mark.parametrize("record", ["bootstrap-protocol.json", "bootstrap-attempt.json", "bootstrap-enrollment.json",
                                    "tunnel-stage.json", "tunnel-redeploy.json", "tunnel-mounted.json"])
def test_projection_requires_retained_bootstrap_provenance(projection_world, record):
    worker, unused, unused_daemons, effects = projection_world
    worker.markers.records.pop(record)
    with pytest.raises(ceph_native.Refusal):
        worker.services(fixtures.device_rows(worker.request))
    assert effects == []


@pytest.mark.parametrize("field,value", [("status", None), ("unmanaged", 1), ("created", None), ("deleted", None)])
def test_malformed_description_is_not_a_projection(projection_world, field, value):
    worker, rows, unused_daemons, effects = projection_world
    rows["mgr"][field] = value
    with pytest.raises(ceph_native.Refusal):
        worker.services(fixtures.device_rows(worker.request))
    assert effects == []


def test_full_description_and_export_must_match_before_effect(projection_world):
    worker, rows, unused_daemons, effects = projection_world
    original = worker.ceph

    def ceph(*args, **kwargs):
        if args == ("orch", "ls"):
            return ceph_native.canonical([rows["mon"]])
        return original(*args, **kwargs)

    worker.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="foreign-service"):
        worker.services(fixtures.device_rows(worker.request))
    assert effects == []


def test_completed_service_cannot_return_to_seed_projection(projection_world):
    worker, rows, unused_daemons, effects = projection_world
    original = copy.deepcopy(rows["mon"])
    worker.services(fixtures.device_rows(worker.request), service=0)
    rows["mon"] = original
    with pytest.raises(ceph_native.Refusal, match="foreign-service"):
        worker.services(fixtures.device_rows(worker.request), service=1)
    assert effects == ["mon"]


@pytest.mark.parametrize("proof", ["configuration_complete", "verify_transport_bundle", "verify_manager_mounts"])
def test_projection_current_proof_refusal_publishes_no_service_attempt(projection_world, proof):
    worker, unused, unused_daemons, effects = projection_world

    def refuse(*args):
        del args
        raise ceph_native.Refusal("native-tunnel-unproved")

    setattr(worker, proof, refuse)
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        worker.services(fixtures.device_rows(worker.request))
    assert worker.markers.read("service-mon-attempt.json") is None
    assert effects == []


def test_observed_owned_partial_resumes_services_without_rebootstrap(projection_world):
    worker, unused, daemons, effects = projection_world
    original = worker.ceph
    intent = staging.stage_intent(worker.request)
    answers = {
        ("status",): {"fsid": worker.request["fsid"], "health": {"status": "HEALTH_OK"},
                      "pgmap": {"num_pgs": 0, "pgs_by_state": []}},
        ("quorum_status",): {"quorum_names": [intent["monID"]]},
        ("mgr", "dump"): {"active_name": intent["mgrID"], "standbys": []},
        ("orch", "ps"): [
            {
                "daemon_type": kind,
                "daemon_id": intent[kind + "ID"],
                "hostname": worker.request["bootstrap"],
                "status": 1,
                "container_image_digests": [worker.request["image"]],
            }
            for kind in ("mon", "mgr")
        ],
        ("osd", "metadata"): [], ("osd", "dump"): {"osds": []},
        ("mon", "dump"): {"mons": [{"name": intent["monID"], "public_addrs": {
            "addrvec": [{"type": "v2", "addr": worker.request["monIP"] + ":3300/0"}]}}]},
    }

    def ceph(*args, **kwargs):
        return copy.deepcopy(answers[args]) if args in answers else original(*args, **kwargs)

    def forbidden_bootstrap(*args, **kwargs):
        del args, kwargs
        pytest.fail("continuation attempted bootstrap or another uninjected command")

    worker.ceph = ceph
    worker.command = forbidden_bootstrap
    worker.call_home_state = lambda: None
    worker.dashboard_observation = lambda: {"url": "", "certificateSHA256": "", "reachable": False}
    before = copy.deepcopy((worker.markers.records, worker.markers.writes, daemons, effects))
    evidence = worker.observe()
    assert evidence["owned"] and evidence["exists"] and evidence["partial"]
    assert not evidence["ready"]
    assert (worker.markers.records, worker.markers.writes, daemons, effects) == before
    assert worker.services(fixtures.device_rows(worker.request)) == {"changed": True}
    expected_osds = [item["service_type"] + "." + item["service_id"]
                     for item in worker.service_spec(worker.request["services"][2])]
    assert effects == ["mon", "mgr", *expected_osds]
