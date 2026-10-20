"""The derived crash service unit: one singleton document on every cluster host, applied through the generic flow and read back.

The scripted monitor answers the upstream v20.2.1 shapes test_ceph_service_units and test_ceph_monitoring document, beside these:
- cephadm runs crash on the Ceph image itself (CEPH_TYPES, src/pybind/mgr/cephadm/utils.py L23-L26);
- crash is not in REQUIRES_SERVICE_ID and has no ServiceSpec subclass, so its document is the generic ServiceSpec with no service_id
  or spec (src/python-common/ceph/deployment/service_spec.py L801-L813), and cephadm names the service by its type.
The daemon_id of each scripted row is a stand-in: the crash readiness reads only a row's hostname, service_name, daemon_type, status
and container_image_digests.
"""

from __future__ import annotations

import contextlib
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_host_runtime, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

HOSTS = [host["name"] for host in units.cluster()["hosts"]]
DOCUMENT = {"placement": {"hosts": HOSTS}, "service_type": "crash"}
OTHER_IMAGE = "registry.example.test/other@sha256:" + "c" * 64


@pytest.fixture(autouse=True)
def contained_normalization(monkeypatch):
    """Identity normalization in place of the frozen image's SDK; the real record and host scope still run."""
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **options: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())


def crash_request():
    return dict(units.cluster(), action="service", service={"documents": [copy.deepcopy(DOCUMENT)], "id": "", "type": "crash"},
                delegated=[{"name": "crash", "hosts": list(HOSTS)}])


def row(host, image, status=1):
    return {"daemon_type": "crash", "daemon_id": host, "hostname": host, "service_name": "crash", "status": status,
            "container_image_digests": [image]}


def test_crash_unit_applies_once_and_reads_back(tmp_path):
    request = crash_request()
    assert len(HOSTS) >= 2 and HOSTS == sorted(HOSTS)
    assert ceph_native.validate(request) is request
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    assert units.worker(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")] and [json.loads(data) for data in monitor.stdin] == [DOCUMENT]
    held = units.records(markers)
    assert {"service-crash-attempt.json", "service-crash-complete.json", "unit-crash-inputs.json"} <= set(held)
    assert units.worker(monitor, markers).unit() == {"changed": False}
    assert monitor.mutations == [("orch", "apply", "-i", "-")]


def test_crash_unit_refuses_a_readback_that_differs(tmp_path):
    request = crash_request()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    run = monitor.run

    def rewriting(argv, data=b"", timeout=60):
        result = run(argv, data, timeout)
        if tuple(argv[-4:]) == ("orch", "apply", "-i", "-"):
            monitor.specs["crash"] = dict(monitor.specs["crash"], placement={"hosts": HOSTS[:-1]})
        return result

    monitor.run = rewriting
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        units.worker(monitor, markers).unit()
    assert monitor.mutations == [("orch", "apply", "-i", "-")]
    assert not any(name.endswith("-complete.json") for name in units.records(markers))


def test_crash_unit_ready_needs_one_crash_daemon_per_placed_host(tmp_path):
    request = crash_request()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    units.worker(monitor, markers).unit()
    monitor.running = [row(host, request["image"]) for host in HOSTS]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is True and blocking == []
    monitor.running = [row(host, request["image"]) for host in HOSTS[:-1]]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is False and blocking == ["crash@crash"] and all(ceph_core.blocking_entry(entry) for entry in blocking)
    doubled = dict(row(HOSTS[0], request["image"]), daemon_id=HOSTS[0] + "-second")
    monitor.running = [row(host, request["image"]) for host in HOSTS[:-1]] + [doubled]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is False and blocking == ["crash@crash"]
    monitor.running = [row(host, request["image"]) for host in HOSTS[:-1]] + [row(HOSTS[-1], request["image"], status=0)]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is False and blocking == ["crash@crash"]
    monitor.running = [row(host, request["image"]) for host in HOSTS[:-1]] + [row(HOSTS[-1], OTHER_IMAGE)]
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        monitoring.observing(monitor, markers)


INVALID = {
    "service_id": lambda request: request["service"]["documents"][0].update(service_id="a"),
    "spec member": lambda request: request["service"]["documents"][0].update(spec={"x": 1}),
    "networks": lambda request: request["service"]["documents"][0].update(networks=["192.0.2.0/24"]),
    "unit id": lambda request: request["service"].update(id="a"),
    "binding": lambda request: request["service"].update(bindings=[{"path": "/spec/x", "secretRef": "s", "serviceRef": "crash"}]),
    "two documents": lambda request: (request["service"]["documents"].append({"placement": {"hosts": list(HOSTS)}, "service_id": "b",
                                                                             "service_type": "nfs"}),
                                      request["delegated"].insert(0, {"name": "nfs.b", "hosts": list(HOSTS)})),
    "placement missing a host": lambda request: (request["service"]["documents"][0].update(placement={"hosts": HOSTS[:-1]}),
                                                 request["delegated"][0].update(hosts=HOSTS[:-1])),
    "placement in another order": lambda request: (request["service"]["documents"][0].update(placement={"hosts": HOSTS[::-1]}),
                                                   request["delegated"][0].update(hosts=HOSTS[::-1])),
    "count": lambda request: request["service"]["documents"][0]["placement"].update(count=1),
    "delegated hosts": lambda request: request["delegated"][0].update(hosts=HOSTS[:-1]),
}


@pytest.mark.parametrize("change", sorted(INVALID))
def test_crash_unit_request_validation(change):
    request = crash_request()
    INVALID[change](request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)
