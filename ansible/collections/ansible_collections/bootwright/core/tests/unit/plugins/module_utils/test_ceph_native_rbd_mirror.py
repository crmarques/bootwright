"""The RBD mirror service unit: one singleton document applied through the generic flow, its daemons ready only on the Ceph image.

The scripted monitor answers the upstream v20.2.1 shapes test_ceph_service_units and test_ceph_monitoring document, beside these:
- rbd-mirror is not in REQUIRES_SERVICE_ID and has no ServiceSpec subclass, so cephadm names the service by its type and refuses a
  service_id for it (src/python-common/ceph/deployment/service_spec.py:804-813);
- cephadm gives each rbd-mirror daemon the ID `<host short name>.<six lowercase letters>` (src/pybind/mgr/cephadm/module.py:922-966)
  and runs it on the Ceph image itself.
"""

from __future__ import annotations

import contextlib
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_host_runtime, ceph_native, ceph_native_units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

DOCUMENT = {"placement": {"count": 2, "hosts": ["node-1", "node-2"]}, "service_type": "rbd-mirror"}
OTHER_IMAGE = "registry.example.test/other@sha256:" + "c" * 64


@pytest.fixture(autouse=True)
def contained_normalization(monkeypatch):
    """Identity normalization in place of the frozen image's SDK; the real record and host scope still run."""
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **options: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())


def mirror_request():
    return dict(units.cluster(), action="service", service={"documents": [copy.deepcopy(DOCUMENT)], "id": "", "type": "rbd-mirror"},
                delegated=[{"name": "rbd-mirror", "hosts": ["node-1", "node-2"]}])


def row(host, image, status=1):
    return {"daemon_type": "rbd-mirror", "daemon_id": host + ".abcdef", "hostname": host, "service_name": "rbd-mirror", "status": status,
            "container_image_digests": [image]}


def test_rbd_mirror_unit_applies_one_singleton_document(tmp_path):
    request = mirror_request()
    assert ceph_native.validate(request) is request
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    assert units.worker(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")] and [json.loads(data) for data in monitor.stdin] == [DOCUMENT]
    assert not any(argv[-2:] == ["config", "dump"] for argv in monitor.log)
    held = units.records(markers)
    assert {"service-rbd-mirror-attempt.json", "service-rbd-mirror-complete.json", "unit-rbd-mirror-inputs.json"} <= set(held)


def test_rbd_mirror_unit_ready_only_on_the_ceph_image(tmp_path):
    request = mirror_request()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    units.worker(monitor, markers).unit()
    monitor.running = [row("node-1", request["image"]), row("node-2", request["image"])]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is True and blocking == []
    monitor.running = [row("node-1", request["image"])]
    ready, blocking, unused = monitoring.observing(monitor, markers)
    assert ready is False and blocking == ["rbd-mirror@rbd-mirror"] and all(ceph_core.blocking_entry(entry) for entry in blocking)
    monitor.running = [row("node-1", request["image"]), row("node-2", OTHER_IMAGE)]
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        monitoring.observing(monitor, markers)


INVALID = {
    "service_id": lambda request: request["service"]["documents"][0].update(service_id="a"),
    "spec member": lambda request: request["service"]["documents"][0].update(spec={"mode": "x"}),
    "two documents": lambda request: request["service"]["documents"].append(dict(copy.deepcopy(DOCUMENT), service_id="b", service_type="nfs")),
    "unit id": lambda request: request["service"].update(id="a"),
    "binding": lambda request: request["service"].update(bindings=[{"path": "/spec/x", "secretRef": "s", "serviceRef": "rbd-mirror"}]),
    "other type": lambda request: request["service"]["documents"][0].update(service_type="cephfs-mirror"),
    "delegated hosts": lambda request: request["delegated"][0].update(hosts=["node-1", "node-3"]),
    "networks": lambda request: request["service"]["documents"][0].update(networks=["192.0.2.1/24"]),
}


@pytest.mark.parametrize("change", sorted(INVALID))
def test_rbd_mirror_unit_request_validation(change):
    request = mirror_request()
    INVALID[change](request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


def test_rbd_mirror_unit_admits_exact_networks():
    request = mirror_request()
    request["service"]["documents"][0]["networks"] = ["192.0.2.0/24"]
    assert ceph_native.validate(request) is request
    assert ceph_native_units.UNIT_TYPES["rbd-mirror"].images == {"rbd-mirror": None}
