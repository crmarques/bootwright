"""Every Ceph request Go freezes passes the validator the host runs, once its prepare-time proofs are in.

The goldens are the requests the planner freezes. The capability's prepare step then completes each with what only
the live run knows (the owner and fsid of the original operation, the proved image digests, the proved subvolume
path); that completion is the one place this test supplies values, and it is all of them: any other member a golden
carries must be one the validator admits, so a member Go adds that the validator refuses fails here, not on a host.
"""
from __future__ import annotations

import copy
import json
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_registry_discovery

REQUESTS = json.loads((Path(__file__).resolve().parent / "goldens/ceph_request_goldens.json").read_text(encoding="utf-8"))["requests"]
DIGEST = "@sha256:" + "a" * 64
KEY = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/RPM-GPG-KEY-IBM-CEPH"
CORE = "ceph-core-v1"
HOST = "ceph-host-v1"
IMAGE = "ceph-image-v1"
# Requests whose validators take an already validated cluster scope or a probe, so they are not run here.
OTHER = {
    "ceph-export-request-v1": "ceph_export_native.validate",
    "ceph-replication-v1": "ceph_replication_native.validate",
    "ceph-smb-v1": "ceph_smb_native.validate",
    "storage-ceph-physical-retirement-v1": "the physical retirement proof",
}
FRAGMENTS = {"osd-class", "osd-path-class"}


SCOPE_DEFAULTS = {
    "ansiblePackageVersion": "5.0.3-1", "callHome": "enabled", "clusterNetworks": ["198.51.100.0/24"], "clusterSSHUser": "operator",
    "controller": "controller", "fsid": "11111111-2222-4333-8444-555555555555", "monIP": "192.0.2.1", "owner": "operation-1",
    "packageRepository": "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64", "packageRepositoryKey": KEY,
    "publicNetworks": ["192.0.2.0/24"], "release": "9.9.1-25074",
}


def prepared_scope(scope):
    scope = copy.deepcopy(scope)
    scope["clusterSSHTransport"] = "verifiedTunnel"
    for member, value in SCOPE_DEFAULTS.items():
        if scope.get(member) in (None, ""):
            scope[member] = value
    tool = scope["cephadm"]
    scope["cephadm"] = {"path": tool["path"] or "/usr/sbin/cephadm", "packageVersion": tool["packageVersion"] or "20.2.1-405.el9cp"}
    if "@" not in scope["image"]:
        scope["image"] += DIGEST
    for entry in scope.get("auxiliary") or []:
        if "@" not in entry["image"]:
            entry["image"] += DIGEST
    return scope


def prepared(request):
    request = prepared_scope(request)
    if any(document["service_type"] == "ingress" for document in (request.get("gateway") or {}).get("documents", [])):
        request["auxiliary"] = [{"name": name, "image": request["registry"] + "/" + name + DIGEST} for name in ("haproxy", "keepalived")]
    client = request.get("client")
    if client and client.get("subvolume") and "subvolumePath" not in client:
        # Go's prepare binds the proved path into the frozen mds grant (internal/storage/ceph/capability.go).
        client["subvolumePath"] = "/volumes/%s/%s/11111111-2222-4333-8444-555555555555" % (client["group"] or "_nogroup", client["subvolume"])
        client["caps"]["mds"] += " path=" + client["subvolumePath"]
    export = request.get("nfsExport")
    if export and export["subvolume"] and "subvolumePath" not in export:
        export["subvolumePath"] = "/volumes/%s/%s/11111111-2222-4333-8444-555555555555" % (export["group"] or "_nogroup", export["subvolume"])
    if export and "sectype" not in export["native"]:
        export["native"]["sectype"] = ["sys"]
    return request


# What the collection derived before Go froze these values (B1505). The pins below hold every golden to them, so the move changed
# no grant, budget or placement-group count; fixtures elsewhere build their frozen values from them.
def legacy_caps(client):
    """The caps `ceph fs authorize` renders for a CephFS scope (doc/cephfs/client-auth.rst); RBD's profiles otherwise."""
    if client["filesystem"]:
        grant = "r" if client["access"] == "readOnly" else "rw"
        mds = "allow " + grant + " fsname=" + client["filesystem"]
        if client["subvolume"]:
            mds += " path=" + client["subvolumePath"]
        return {"mds": mds, "mon": "allow r fsname=" + client["filesystem"], "osd": "allow " + grant + " tag cephfs data=" + client["filesystem"]}
    profile = "rbd-read-only" if client["access"] == "readOnly" else "rbd"
    osd = ", ".join("profile " + profile + " pool=" + name for name in [client["pool"]] + client["dataPools"])
    return {"mon": "profile rbd", "mgr": "profile " + profile + " pool=" + client["pool"], "osd": osd}


def legacy_export_caps(request):
    """The grants of the five Ceph CSI principals an export creates, as the export worker derived them (caps_for)."""
    health = "profile rbd-read-only"
    if "gateway" in request:
        gateway = request["gateway"]
        zone = gateway["zone"]
        health += (", allow rwx pool=" + zone + ".rgw.meta, allow r pool=.rgw.root, allow rw pool=" + zone
                   + ".rgw.control, allow rx pool=" + zone + ".rgw.log, allow x pool=" + gateway["indexPool"])
    return {
        "health": {"mon": "allow r, allow command quorum_status, allow command version", "mgr": "allow command config", "osd": health, "mds": "allow *"},
        "rbd-node": {"mon": "profile rbd, allow command 'osd blocklist'", "osd": "profile rbd pool=" + request["pool"]},
        "rbd-prov": {"mon": "profile rbd, allow command 'osd blocklist'", "mgr": "allow rw", "osd": "profile rbd pool=" + request["pool"]},
        "fs-node": {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw",
                    "osd": "allow rw tag cephfs *=" + request["filesystem"], "mds": "allow rw"},
        "fs-prov": {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw",
                    "osd": "allow rw tag cephfs metadata=" + request["filesystem"], "mds": "allow *"},
    }


def legacy_pg_num(autoscale):
    pgs = 32
    if autoscale["minPGs"] and pgs < autoscale["minPGs"]:
        pgs = autoscale["minPGs"]
    if autoscale["maxPGs"] and pgs > autoscale["maxPGs"]:
        pgs = autoscale["maxPGs"]
    return pgs


def legacy_nvmeof_budgets(frozen):
    """60 seconds per CLI command, 30 per held image probe, plus local work; the controller bound adds an observation and a retry window."""
    gateways, namespaces = len(frozen["gateways"]), len(frozen["namespaces"])
    listeners, hosts = len(frozen["listeners"]), len(frozen["hosts"])
    data_pools = sum(bool(n["image"]["dataPool"]) for n in frozen["namespaces"])
    scope = 5 + gateways + 3 * namespaces + data_pools
    apply = 60 * (scope + 10 + namespaces + listeners + hosts) + 30 * (2 + namespaces + listeners + hosts) + 60
    observe = 60 * (scope + 4 + gateways) + 2 * listeners + 60
    observation = 660 + observe + 20
    return {"apply": 660 + apply + 20 + observation + 900 + 60, "nvmeof": apply, "nvmeofObserve": observe, "observation": observation}


def pools_of(value):
    if isinstance(value, dict):
        if "autoscale" in value and "pgNum" in value:
            yield value
        for member in value.values():
            yield from pools_of(member)
    elif isinstance(value, list):
        for member in value:
            yield from pools_of(member)


def test_the_frozen_grants_budgets_and_pg_counts_equal_what_the_collection_derived():
    clients = [name for name, request in REQUESTS.items() if "client" in request]
    nvmeof = [name for name, request in REQUESTS.items() if "nvmeof" in request]
    pools = [pool for request in REQUESTS.values() for pool in pools_of(request)]
    assert len(clients) == 3 and nvmeof == ["nvmeof"] and len(pools) >= 5
    for name in clients:
        client = prepared(REQUESTS[name])["client"]
        assert client["caps"] == legacy_caps(client), name
    for name in nvmeof:
        assert REQUESTS[name]["nvmeof"]["budgets"] == legacy_nvmeof_budgets(REQUESTS[name]["nvmeof"])
    for pool in pools:
        assert pool["pgNum"] == legacy_pg_num(pool["autoscale"]), pool["name"]


def test_the_frozen_export_principal_grants_equal_what_the_export_worker_derived():
    exports = versions("ceph-export-request-v1")
    assert exports == ["export", "export-gateway", "export-tls"]
    assert any("gateway" in REQUESTS[name] for name in exports)
    for name in exports:
        assert REQUESTS[name]["principals"] == legacy_export_caps(REQUESTS[name]), name


@pytest.mark.parametrize("name, member", [("client", "caps"), ("client-filesystem", "caps"), ("client-subvolume", "caps"), ("nvmeof", "budgets")])
def test_the_validator_refuses_a_request_lacking_a_frozen_value(name, member):
    request = prepared(REQUESTS[name])
    del request["client" if member == "caps" else "nvmeof"][member]
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(request)


def versions(version):
    return sorted(name for name, request in REQUESTS.items() if request.get("version") == version)


def test_every_golden_has_a_validator_or_is_a_named_fragment():
    unnamed = {name: request.get("version") for name, request in REQUESTS.items()
               if request.get("version") not in {CORE, HOST, IMAGE, *OTHER} and name not in FRAGMENTS}
    assert unnamed == {}
    assert len(REQUESTS) >= 40 and len(versions(CORE)) >= 25 and len(versions(HOST)) == 4


@pytest.mark.parametrize("name", versions(CORE))
def test_the_validator_admits_the_core_request_go_freezes(name):
    request = prepared(REQUESTS[name])
    assert ceph_native.validate(request) is request


@pytest.mark.parametrize("name", versions(HOST))
def test_the_validator_admits_the_host_request_go_freezes(name):
    request = copy.deepcopy(REQUESTS[name])
    request["cluster"] = prepared(request["cluster"])
    assert ceph_core.host_request(request) is not None


@pytest.mark.parametrize("name", versions(IMAGE))
def test_the_validator_admits_the_image_request_go_freezes(name):
    request = copy.deepcopy(REQUESTS[name])
    assert ceph_registry_discovery.validate_request(request) is not None


def test_the_validator_refuses_a_member_no_golden_names():
    request = prepared(REQUESTS["cluster"])
    request["notARequestMember"] = True
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate(request)
