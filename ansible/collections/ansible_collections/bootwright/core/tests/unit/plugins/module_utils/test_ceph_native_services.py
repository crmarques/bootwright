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
    assert frozenset(observe.DELEGATED_TYPES) == {name for name, row in ceph_native.DAEMON_TYPES.items() if row[0] == "delegated"}


def with_delegated(request):
    names = [host["name"] for host in request["hosts"]]
    request["delegated"] = [{"name": name, "hosts": names[:2]} for name in ("ingress.rgw.objects", "nfs.files", "rgw.objects")]
    request["config"] = core_fixtures.memory_config()
    return request


def delegated_daemon(daemon_type, service, host, status=0):
    """One `ceph orch ps` row of a daemon another block executes, as DaemonDescription.to_json emits it with its service_name
    (orchestrator/_interface.py, v20.2.1); its image is that service's own, never the Ceph image."""
    identifier = service.split(".", 1)[1] + "." + host.split(".", 1)[0] + ".abcdef"
    return {"daemon_type": daemon_type, "daemon_id": identifier, "service_name": service, "hostname": host, "status": status,
            "container_image_digests": ["registry.example.test/other@sha256:" + "a" * 64]}


DELEGATED_ROWS = {"rgw": "rgw.objects", "haproxy": "ingress.rgw.objects", "keepalived": "ingress.rgw.objects", "nfs": "nfs.files"}


@pytest.mark.parametrize("daemon_type", sorted(DELEGATED_ROWS))
def test_observe_tolerates_listed_delegated_daemons(core_request, daemon_type):
    with_delegated(core_request)
    answers = core_fixtures.memory_observation_answers(core_request)
    hosts = [host["name"] for host in core_request["hosts"]]
    answers[("orch", "ps")] += [delegated_daemon(daemon_type, DELEGATED_ROWS[daemon_type], host) for host in hosts[:2]]
    native, unused = core_fixtures.worker(core_request, answers)
    assert native.observe()["ready"] is True
    assert native.markers.writes == []


@pytest.mark.parametrize("change", ["unlisted", "outside-hosts", "wrong-family", "no-service"])
def test_observe_refuses_unlisted_delegated_daemon(core_request, change):
    with_delegated(core_request)
    answers = core_fixtures.memory_observation_answers(core_request)
    hosts = [host["name"] for host in core_request["hosts"]]
    row = {"unlisted": delegated_daemon("nfs", "nfs.other", hosts[0]), "outside-hosts": delegated_daemon("nfs", "nfs.files", hosts[2]),
           "wrong-family": delegated_daemon("haproxy", "nfs.files", hosts[0]), "no-service": delegated_daemon("rgw", "rgw.objects", hosts[0])}[change]
    if change == "no-service":
        del row["service_name"]
    answers[("orch", "ps")].append(row)
    native, unused = core_fixtures.worker(core_request, answers)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        native.observe()
    assert native.markers.writes == []


def test_validate_admits_delegated_member(core_request):
    with_delegated(core_request)
    assert ceph_native.validate(core_request) is core_request


@pytest.mark.parametrize("change", ["empty", "cluster-family", "unknown-host", "duplicate-host", "no-hosts", "unsorted", "duplicate-name",
                                    "extra-key", "not-a-list"])
def test_validate_refuses_malformed_delegated(core_request, change):
    with_delegated(core_request)
    delegated = core_request["delegated"]
    names = [host["name"] for host in core_request["hosts"]]
    if change == "empty":
        core_request["delegated"] = []
    elif change == "cluster-family":
        delegated[1]["name"] = "mds.files"
    elif change == "unknown-host":
        delegated[0]["hosts"] = ["node-9"]
    elif change == "duplicate-host":
        delegated[0]["hosts"] = [names[0], names[0]]
    elif change == "no-hosts":
        delegated[0]["hosts"] = []
    elif change == "unsorted":
        delegated.reverse()
    elif change == "duplicate-name":
        delegated[1]["name"] = delegated[0]["name"]
    elif change == "extra-key":
        delegated[0]["spec"] = {}
    else:
        core_request["delegated"] = {"name": "rgw.objects", "hosts": names}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(core_request)


def test_host_scope_refuses_delegated(core_request):
    with_delegated(core_request)
    scope = {key: value for key, value in core_request.items() if key != "action"}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate_scope(scope)


AUXILIARY = [{"name": "haproxy", "image": "registry.example.test/vendor/haproxy-synthetic@sha256:" + "b" * 64},
             {"name": "keepalived", "image": "registry.example.test/vendor/keepalived-synthetic@sha256:" + "c" * 64}]


def test_validate_admits_the_frozen_auxiliary_set(core_request):
    core_request["auxiliary"] = [dict(entry) for entry in AUXILIARY]
    assert ceph_native.validate(core_request) is core_request
    scope = {key: value for key, value in core_request.items() if key != "action"}
    assert ceph_native.validate_scope(scope) is scope


@pytest.mark.parametrize("change", ["empty", "too-many", "unknown-name", "unsorted", "duplicate", "tag", "foreign-registry", "registry-prefix",
                                    "extra-key", "not-a-list", "zero-length-digest"])
def test_validate_refuses_malformed_auxiliary(core_request, change):
    auxiliary = [dict(entry) for entry in AUXILIARY]
    if change == "empty":
        auxiliary = []
    elif change == "too-many":
        auxiliary = [dict(AUXILIARY[0], name=name) for name in ceph_native.AUXILIARY_NAMES] + [AUXILIARY[0]]
    elif change == "unknown-name":
        auxiliary[0]["name"] = "base"
    elif change == "unsorted":
        auxiliary.reverse()
    elif change == "duplicate":
        auxiliary[1]["name"] = "haproxy"
    elif change == "tag":
        auxiliary[0]["image"] = "registry.example.test/vendor/haproxy-synthetic:synthetic-2"
    elif change == "foreign-registry":
        auxiliary[0]["image"] = "quay.io/ceph/haproxy@sha256:" + "b" * 64
    elif change == "registry-prefix":
        auxiliary[0]["image"] = "registry.example.testx/haproxy@sha256:" + "b" * 64
    elif change == "extra-key":
        auxiliary[0]["base"] = "registry.example.test/vendor/haproxy-synthetic"
    elif change == "not-a-list":
        auxiliary = AUXILIARY[0]
    else:
        auxiliary[0]["image"] = "registry.example.test/vendor/haproxy-synthetic@sha256:"
    core_request["auxiliary"] = auxiliary
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(core_request)
    scope = {key: value for key, value in core_request.items() if key != "action"}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate_scope(scope)
