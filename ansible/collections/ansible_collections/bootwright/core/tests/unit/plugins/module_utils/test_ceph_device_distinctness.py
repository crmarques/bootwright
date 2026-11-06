"""Observed device identities are disjoint across hosts and selector forms."""
from __future__ import annotations

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as fixtures

core_request = fixtures.core_request


def bound_rows(request, collision):
    values = []
    for index, host in enumerate(request["hosts"]):
        host["physical"] = True
        serial, wwn = "serial-%d" % index, "0x500000000000000%d" % index
        if index == 1:
            serial = "serial-0" if collision == "serial" else serial
            wwn = "0x5000000000000000" if collision == "wwn" else wwn
        path = "/dev/disk/by-serial/" + serial if index == 1 else "/dev/disk/by-id/wwn-" + wwn
        host["devices"] = [{"path": path, "serial": serial, "sizeBytes": 1024} if index == 1 else {"path": path, "wwn": wwn}]
        value = {"host": host["name"], "path": path, "serial": serial, "wwn": wwn, "sizeBytes": 1024,
                 "kernelPath": "/dev/sda", "deviceID": "SYNTHETIC_" + serial, "rotational": False}
        if index == 1:
            value["alias"] = "/dev/disk/by-id/SYNTHETIC_" + serial
        values.append(value)
    return values


@pytest.mark.parametrize("collision", [None, "serial", "wwn"])
@pytest.mark.parametrize("transport", ["native", "verifiedTunnel"])
def test_aggregate_rejects_shared_observed_disk(core_request, collision, transport):
    core_request["clusterSSHTransport"] = transport
    values = bound_rows(core_request, collision)
    calls = []

    class ReadOnlyTransport:
        def call(self, host, phase, private=None, **options):
            calls.append(phase)
            assert phase == "host_devices"
            return {"devices": [copy.deepcopy(value) for value in values if value["host"] == host["name"]]}

    if collision:
        with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
            ceph_core.device_identities(core_request, ReadOnlyTransport())
    else:
        assert ceph_core.device_identities(core_request, ReadOnlyTransport()) == values
    assert calls == ["host_devices"] * len(core_request["hosts"])


@pytest.mark.parametrize("collision", ["serial", "wwn"])
def test_service_identity_record_refuses_collision_without_publication(core_request, collision):
    native, calls = fixtures.worker(core_request, {})
    values = bound_rows(native.request, collision)
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        native.device_identities(values)
    assert not native.markers.writes and not calls


@pytest.mark.parametrize("route", ["native", "verifiedTunnel"])
def test_shared_disk_refuses_before_any_cluster_preparation_effect(core_request, monkeypatch, route):
    values = bound_rows(core_request, "wwn")
    core_request["clusterSSHTransport"] = route
    calls = []

    class Transport:
        def call(self, host, phase, private=None, **options):
            calls.append(phase)
            assert phase == "host_devices", "a mutation preceded disjoint disk proof"
            return {"devices": [value for value in values if value["host"] == host["name"]]}

    bootstrap = core_request["hosts"][0]
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        if route == "native":
            ceph_core.apply_native_cluster(core_request, bootstrap, Transport(), {"exists": False})
        else:
            monkeypatch.setattr(ceph_core.ceph_tunnel_staging, "prove_recovery_record", lambda *args, **kwargs: "unclaimed")
            monkeypatch.setattr(ceph_core, "recovery_preparations", lambda *args: {host["name"]: "unclaimed" for host in core_request["hosts"]})
            ceph_core.apply_tunnel_cluster(core_request, bootstrap, Transport(), {}, [])
    assert calls == ["host_devices"] * len(core_request["hosts"])


@pytest.mark.parametrize("route", ["native", "verifiedTunnel"])
def test_shared_disk_refuses_before_orphan_purge_or_service_write(core_request, route):
    native, calls = fixtures.worker(core_request, {})
    values = bound_rows(native.request, "serial")
    native.request["clusterSSHTransport"] = route
    native.ceph = lambda *args: {"fsid": core_request["fsid"]}
    native.prove_registry = native.provider_registry_absent = lambda: None
    native.purge_orphan_osds = lambda values: pytest.fail("purged before disjoint disk proof")
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        native.services(devices=values, orphans=["unreachable-orphan"])
    assert not native.markers.writes and not calls
