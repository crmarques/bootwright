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
        client["subvolumePath"] = "/volumes/%s/%s/11111111-2222-4333-8444-555555555555" % (client["group"] or "_nogroup", client["subvolume"])
    export = request.get("nfsExport")
    if export and export["subvolume"] and "subvolumePath" not in export:
        export["subvolumePath"] = "/volumes/%s/%s/11111111-2222-4333-8444-555555555555" % (export["group"] or "_nogroup", export["subvolume"])
    if export and "sectype" not in export["native"]:
        export["native"]["sectype"] = ["sys"]
    return request


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
