"""The MDS family joins core validation and observation through the daemon-type table."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_observe as observe
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core_fixtures

core_request = core_fixtures.core_request


def with_mds(request, fqdn=False):
    names = [host["name"] for host in request["hosts"]]
    request["services"].append({"service_type": "mds", "service_id": "files", "placement": {"hosts": names[:2]}})
    if fqdn:
        core_fixtures.fqdn_core_request(request)
    request["config"] = core_fixtures.memory_config()
    return request


def mds_daemon(request, host, identifier=None):
    """One `ceph orch ps` row as DaemonDescription.to_json emits it (orchestrator/_interface.py:1471-1500, v20.2.1)."""
    stem = host.split(".", 1)[0]
    return {"daemon_type": "mds", "daemon_id": identifier or "files." + stem + ".abcdef", "hostname": host, "status": 1,
            "container_image_digests": [request["image"]]}


def mds_answers(request):
    answers = core_fixtures.memory_observation_answers(request)
    hosts = [host["name"] for host in request["hosts"]]
    answers[("orch", "ps")] += [mds_daemon(request, hosts[0]), mds_daemon(request, hosts[1])]
    return answers


def test_validate_services_admits_mds_without_spec(core_request):
    with_mds(core_request)
    assert ceph_native.validate(core_request) is core_request


@pytest.mark.parametrize("change", ["empty-spec", "spec", "no-id", "digit-id"])
def test_validate_services_refuses_mds_spec(core_request, change):
    with_mds(core_request)
    service = core_request["services"][-1]
    if change == "empty-spec":
        service["spec"] = {}
    elif change == "spec":
        service["spec"] = {"x": 1}
    elif change == "no-id":
        del service["service_id"]
    else:
        service["service_id"] = "1files"
    with pytest.raises(ceph_native.Refusal, match="^service-unqualified$"):
        ceph_native.validate(core_request)


@pytest.mark.parametrize("fqdn", [False, True])
def test_observe_cluster_accepts_mds_daemon_on_placement_host(core_request, fqdn):
    with_mds(core_request, fqdn)
    native, unused = core_fixtures.worker(core_request, mds_answers(core_request))
    assert native.observe()["ready"] is True
    assert native.markers.writes == []


@pytest.mark.parametrize("change", ["outside-placement", "undeclared-service", "wrong-stem", "no-suffix"])
def test_observe_cluster_refuses_mds_on_foreign_host(core_request, change):
    with_mds(core_request)
    answers = mds_answers(core_request)
    running = answers[("orch", "ps")]
    hosts = [host["name"] for host in core_request["hosts"]]
    if change == "outside-placement":
        running[-1] = mds_daemon(core_request, hosts[2])
    elif change == "undeclared-service":
        running[-1] = mds_daemon(core_request, hosts[0], "other.node-1.abcdef")
    elif change == "wrong-stem":
        running[-1] = mds_daemon(core_request, hosts[0], "files.node-2.abcdef")
    else:
        running[-1] = mds_daemon(core_request, hosts[0], "files.node-1")
    native, unused = core_fixtures.worker(core_request, answers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        native.observe()
    assert native.markers.writes == []


@pytest.mark.parametrize("change", ["missing", "starting", "duplicate", "running"])
def test_cluster_ready_needs_mds_running_count_not_active(core_request, change):
    with_mds(core_request)
    answers = mds_answers(core_request)
    running = answers[("orch", "ps")]
    if change == "missing":
        del running[-1]
    elif change == "starting":
        running[-1]["status"] = 0
    elif change == "duplicate":
        running.append(dict(running[-1]))
    native, calls = core_fixtures.worker(core_request, answers)
    if change == "duplicate":
        with pytest.raises(ceph_native.Refusal, match="^native-output$"):
            native.observe()
        return
    result = native.observe()
    if change == "running":
        assert result["ready"] is True and not result["partial"]
        assert all("fs" not in argv and "mds" not in argv for argv in calls)
    else:
        assert result["exists"] and result["partial"] and not result["ready"]
    assert native.markers.writes == []


def test_host_layout_types_match_the_daemon_type_table():
    assert observe.TYPES == frozenset(ceph_native.DAEMON_TYPES)
