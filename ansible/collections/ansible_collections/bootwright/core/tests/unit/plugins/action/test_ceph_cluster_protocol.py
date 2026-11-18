"""The core Ceph result publisher validates evidence without the frozen request (B1026)."""

from __future__ import annotations

import copy
import hashlib

import pytest

from ansible_collections.bootwright.core.plugins.action import ceph_cluster_protocol
from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime, ceph_native

LEAF = hashlib.sha256(b"cephadm-signed leaf for node-1").hexdigest()


def gateway_evidence(**changes):
    """A ready management gateway unit's evidence, as the worker's observation projects it."""
    value = {"version": ceph_native.EVIDENCE_VERSION, "request": "d" * 64, "action": "service", "fsid": "11111111-1111-4111-8111-111111111111",
             "owner": "apply-123", "owned": True, "exists": True, "ready": True, "removed": False, "partial": False, "health": "",
             "hosts": ["node-1"], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": {"id": "", "path": ""},
             "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
             "endpoint": {"certificateSHA256": LEAF, "reachable": True},
             "daemons": [{"host": "node-1", "name": "mgmt-gateway.node-1", "service": "mgmt-gateway"}], "sso": "", "incarnation": "",
             "readiness": {"checks": [], "unnamed": False, "scoped": [], "pools": [], "filesystems": [], "pgs": {"total": 0, "clean": 0}, "license": ""}}
    value.update(changes)
    return value


def publish(value):
    return ceph_cluster_protocol.completion({"digest": "d" * 64, "evidence": copy.deepcopy(value), "observed": False, "removed": False})


def test_the_publisher_admits_a_reachable_management_gateway():
    published, refusal = publish(gateway_evidence())
    assert refusal is None and published["ready"] is True and published["endpoint"] == {"certificateSHA256": LEAF, "reachable": True}


@pytest.mark.parametrize("changes", [
    {"endpoint": {"certificateSHA256": "", "reachable": True}},
    {"endpoint": {"certificateSHA256": LEAF, "reachable": False}},
    {"daemons": []},
    {"daemons": [{"host": "node-1", "name": "crash.node-1", "service": "crash"}]},
], ids=["reachable-without-leaf", "leaf-without-answer", "no-daemons", "other-service"])
def test_the_publisher_refuses_endpoint_facts_no_management_gateway_explains(changes):
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        publish(gateway_evidence(**changes))


def test_an_observation_reports_each_check_that_kept_it_from_ready_as_a_warning():
    reported = ceph_cluster_protocol.pending({"observed": True, "removed": False,
                                              "pending": ["keepalived@ingress.rgw.objects.dc1", "MON_DOWN", "not a check", 7]})
    assert reported == [{"code": "storage.not-ready", "message": "not ready: keepalived@ingress.rgw.objects.dc1"},
                        {"code": "storage.not-ready", "message": "not ready: MON_DOWN"}]


def test_a_scoped_health_check_a_cluster_read_passed_is_a_health_note_not_a_readiness_entry():
    reported = ceph_cluster_protocol.pending({"observed": True, "removed": False,
                                              "pending": ["POOL_APP_NOT_ENABLED@pool.e2e-hand-pool", "FS_DEGRADED@fs.files", "POOL_FULL@pool.a b"]})
    assert reported == [{"code": "storage.health", "message": "health: POOL_APP_NOT_ENABLED names pool e2e-hand-pool"},
                        {"code": "storage.health", "message": "health: FS_DEGRADED names filesystem files"}]


@pytest.mark.parametrize("arguments", [
    {"observed": False, "removed": False, "pending": ["MON_DOWN"]},
    {"observed": True, "removed": True, "pending": ["MON_DOWN"]},
    {"observed": True, "removed": False, "pending": "MON_DOWN"},
    {"observed": True, "removed": False},
], ids=["mutation", "removal", "not-a-list", "absent"])
def test_only_a_read_only_observation_reports_not_ready_checks(arguments):
    assert ceph_cluster_protocol.pending(arguments) == []


def test_a_published_observation_carries_its_not_ready_checks():
    records = []
    arguments = {"phase": "completed", "outcome": "unchanged", "digest": "d" * 64, "observed": True, "removed": False,
                 "evidence": gateway_evidence(ready=False, partial=True, endpoint={"certificateSHA256": "", "reachable": False}),
                 "pending": ["vip@ingress.rgw.objects.dc1"]}
    result = ceph_cluster_protocol.adapter_protocol.publish(arguments, ceph_cluster_protocol.CAPABILITY, lambda record, **_: records.append(record))
    assert result == {"changed": False}
    assert records[0]["warnings"] == [{"code": "storage.not-ready", "message": "not ready: vip@ingress.rgw.objects.dc1"}]


SERIAL_ROW = {"path": "/dev/disk/by-serial/SYNTH-SERIAL-1", "serial": "SYNTH-SERIAL-1", "deviceID": "SYNTHETIC_SYNTH-SERIAL-1", "wwn": "",
              "kernelPath": "/dev/nvme0n1", "sizeBytes": 1 << 40, "rotational": False, "alias": "/dev/disk/by-id/nvme-SYNTHETIC_SYNTH-SERIAL-1"}
WWN_ROW = {"path": "/dev/disk/by-id/wwn-0x5000000000000001", "serial": "SYNTH-SERIAL-2", "deviceID": "SYNTHETIC_SYNTH-SERIAL-2",
           "wwn": "0x5000000000000001", "kernelPath": "/dev/sdb", "sizeBytes": 1 << 40, "rotational": True}


def host_evidence(*devices):
    """A ready physical host's evidence, as its verify step projects its bound disk rows."""
    return {"version": ceph_host_runtime.EVIDENCE_VERSION, "request": "d" * 64, "host": "node-1", "owner": "apply-123",
            "fsid": "11111111-1111-4111-8111-111111111111", "image": "registry.example/ceph@sha256:" + "e" * 64, "ready": True, "removed": False,
            "imageID": "a" * 64, "uid": 167, "gid": 167, "label": ceph_host_runtime.SHARED_LABEL, "policySHA256": "b" * 64, "cephadmSHA256": "c" * 64,
            "planDigest": "f" * 64, "route": True, "devices": [copy.deepcopy(device) for device in devices]}


def test_the_publisher_admits_a_physical_hosts_bound_disks():
    published, refusal = publish(host_evidence(SERIAL_ROW, WWN_ROW))
    assert refusal is None and published["devices"] == [SERIAL_ROW, WWN_ROW]


@pytest.mark.parametrize("row, changes", [
    (SERIAL_ROW, {"alias": None}),
    (WWN_ROW, {"alias": "/dev/disk/by-id/wwn-0x5000000000000001"}),
    (WWN_ROW, {"wwn": ""}),
    (SERIAL_ROW, {"wwn": "0x5 0"}),
    (SERIAL_ROW, {"alias": "/dev/disk/by-serial/SYNTH-SERIAL-1"}),
    (WWN_ROW, {"kernelPath": "/dev/dm-0"}),
    (WWN_ROW, {"sizeBytes": True}),
    (WWN_ROW, {"rotational": 1}),
    (WWN_ROW, {"serial": "has space"}),
    (WWN_ROW, {"deviceID": "a b"}),
    (WWN_ROW, {"kernelPath": None}),
], ids=["serial-without-alias", "wwn-with-alias", "wwn-without-wwn", "wwn-grammar", "alias-grammar", "kernel-path", "bool-size", "int-rotational",
        "serial-grammar", "device-id-grammar", "no-kernel-path"])
def test_the_publisher_holds_a_physical_disk_row_to_its_closed_shape(row, changes):
    damaged = {key: value for key, value in dict(row, **changes).items() if value is not None}
    with pytest.raises(ceph_native.Refusal, match="^(evidence|device-identity)$"):
        publish(host_evidence(damaged))
