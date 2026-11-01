"""The core Ceph result publisher validates evidence without the frozen request (B1026)."""

from __future__ import annotations

import copy
import hashlib

import pytest

from ansible_collections.bootwright.core.plugins.action import ceph_cluster_protocol
from ansible_collections.bootwright.core.plugins.module_utils import ceph_native

LEAF = hashlib.sha256(b"cephadm-signed leaf for node-1").hexdigest()


def gateway_evidence(**changes):
    """A ready management gateway unit's evidence, as the worker's observation projects it."""
    value = {"version": ceph_native.EVIDENCE_VERSION, "request": "d" * 64, "action": "service", "fsid": "11111111-1111-4111-8111-111111111111",
             "owner": "apply-123", "owned": True, "exists": True, "ready": True, "removed": False, "partial": False, "health": "",
             "hosts": ["node-1"], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [], "resource": {"id": "", "path": ""},
             "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
             "endpoint": {"certificateSHA256": LEAF, "reachable": True},
             "daemons": [{"host": "node-1", "name": "mgmt-gateway.node-1", "service": "mgmt-gateway"}], "sso": "", "incarnation": ""}
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
