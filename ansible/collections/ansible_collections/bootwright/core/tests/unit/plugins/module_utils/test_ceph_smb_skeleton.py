"""The SMB block's module admits its completed request and refuses every run until SMB effects are realized.

Every request and evidence record here is a synthetic fixture shaped as the Go
SMB capability sends and reads it; nothing reaches a guest, a Ceph cluster or a
material file.
"""

from __future__ import annotations

import builtins
import copy
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.plugins.modules import ceph_smb

DIGEST = "d" * 64
SENTINEL = "SMB-SENTINEL-VALUE"
IMAGES = [{"image": "cp.icr.io/example/samba@sha256:" + "b" * 64, "name": "samba"},
          {"image": "cp.icr.io/example/samba-metrics@sha256:" + "c" * 64, "name": "samba_metrics"}]
SERVICE = {"clustered": False, "hosts": ["node-1"], "name": "smb.files"}


def scope():
    """The completed cluster scope the core cluster proof supplies, as the host block's tests build it."""
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/sda", "serial": "drive-scsi" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v19", "cluster": "example", "owner": "apply-123",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "cp.icr.io/example/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "synthetic-package"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible", "packageRepository": "https://publisher.example.test/ceph",
            "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "verified-tunnel",
            "clusterKeyRef": "cluster-key", "registryRef": "registry-key", "registry": "cp.icr.io", "callHome": "disabled",
            "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/sda"]}}}]}


def request(resource_type):
    """One completed request of a resource type, as execution hands it to the role."""
    base = {"cluster": "example", "controller": "controller", "machines": ["machine-1", "machine-2", "machine-3"],
            "object": "smb-" + resource_type.split(".")[-1], "resourceType": resource_type, "scope": scope(), "version": "ceph-smb-v1", "bindings": []}
    if resource_type == "ceph.smb.cluster":
        native = {"resource_type": resource_type, "cluster_id": "files", "auth_mode": "user", "placement": {"count": 1, "hosts": ["node-1"]},
                  "user_group_settings": [{"source_type": "resource", "ref": "users"}]}
        return dict(base, id="files", native=native, service=copy.deepcopy(SERVICE), clusterIDs=["files"], images=copy.deepcopy(IMAGES))
    if resource_type == "ceph.smb.share":
        native = {"resource_type": resource_type, "cluster_id": "files", "share_id": "docs", "name": SENTINEL,
                  "cephfs": {"volume": "files", "subvolume": "docs"}}
        return dict(base, id="docs", native=native, service=copy.deepcopy(SERVICE), clusterIDs=["files"], images=copy.deepcopy(IMAGES))
    if resource_type == "ceph.smb.join.auth":
        native = {"resource_type": resource_type, "auth_id": "join", "auth": {"username": "operator"}}
        return dict(base, id="join", native=native, bindings=[{"path": "/auth/password", "secretRef": "join-password"}])
    native = {"resource_type": resource_type, "users_groups_id": "users", "values": {"users": [{"name": "smbuser"}, {"name": "other"}], "groups": []}}
    bindings = [{"path": "/values/users/0/password", "secretRef": "smb-user-password"}, {"path": "/values/users/1/password", "secretRef": "smb-other-password"}]
    return dict(base, id="users", native=native, bindings=bindings)


TYPES = sorted(ceph_core.SMB_TYPES)


def material(value, operation, directory):
    """The material names the Go capability binds, each a path whose name carries the sentinel and which does not exist."""
    names = ["sshKey", "knownHosts"] + (["smbSecret" + str(index) for index in range(len(value["bindings"]))] if operation == "apply" else [])
    return {name: os.path.join(str(directory), SENTINEL + "-" + name) for name in names}


def published(monkeypatch, value, operation, bound, digest=DIGEST):
    """The one failure the module hands Ansible, with every file open refused so a read would surface."""
    reached = {}

    class Module:
        def __init__(self, **options):
            del options
            self.params = {"request": value, "operation": operation, "digest": digest, "material": bound}

        def fail_json(self, **failure):
            reached["failure"] = failure
            raise SystemExit(1)

        def exit_json(self, **result):
            raise AssertionError(result)

    def refused(*arguments, **options):
        raise AssertionError("the module read a file: %r %r" % (arguments, options))

    monkeypatch.setattr(ceph_smb, "AnsibleModule", Module)
    monkeypatch.setattr(builtins, "open", refused)
    monkeypatch.setattr(os, "open", refused)
    with pytest.raises(SystemExit):
        ceph_smb.main()
    monkeypatch.undo()
    return reached["failure"]


@pytest.mark.parametrize("resource_type", TYPES)
@pytest.mark.parametrize("operation", ["apply", "observe", "destroy"])
def test_a_valid_completed_request_refuses_smb_unrealized_without_reading_material(monkeypatch, tmp_path, resource_type, operation):
    value = request(resource_type)
    assert ceph_core.smb_request(copy.deepcopy(value)) == value
    failure = published(monkeypatch, value, operation, material(value, operation, tmp_path))
    assert failure["msg"] == "Ceph SMB operation refused" and failure["reason"] == "smb-unrealized"
    assert SENTINEL not in json.dumps(failure)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("change", [
    lambda value: value.pop("scope"),
    lambda value: value.update(unsent="member"),
    lambda value: value.update(version="ceph-smb-v0"),
    lambda value: value.pop("images"),
    lambda value: value.update(images=list(reversed(value["images"]))),
    lambda value: value.update(images=[dict(IMAGES[0], image="registry.example.test/samba@sha256:" + "b" * 64), IMAGES[1]]),
    lambda value: value.update(machines=["machine-2", "machine-1"]),
    lambda value: value.update(id="other"),
    lambda value: value.update(resourceType="ceph.smb.tls.credential"),
    lambda value: value["native"].update(resource_type="ceph.smb.cluster"),
    lambda value: value["service"].update(name="smb.other"),
    lambda value: value.update(clusterIDs=[]),
    lambda value: value.update(bindings=[{"path": "/auth/password", "secretRef": "join-password"}]),
    lambda value: value["scope"].update(version="ceph-core-v0"),
    lambda value: value["scope"].update(cluster="other"),
])
def test_a_malformed_share_request_refuses_request(monkeypatch, tmp_path, change):
    value = request("ceph.smb.share")
    change(value)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.smb_request(copy.deepcopy(value))
    failure = published(monkeypatch, value, "apply", material(request("ceph.smb.share"), "apply", tmp_path))
    assert failure["reason"] == "request" and SENTINEL not in json.dumps(failure)


@pytest.mark.parametrize("resource_type, change", [
    ("ceph.smb.join.auth", lambda value: value.update(images=IMAGES)),
    ("ceph.smb.join.auth", lambda value: value.update(bindings=[])),
    ("ceph.smb.usersgroups", lambda value: value.update(bindings=list(reversed(value["bindings"])))),
    ("ceph.smb.usersgroups", lambda value: value["bindings"][0].update(path="/values/users/01/password")),
    ("ceph.smb.usersgroups", lambda value: value.update(service=SERVICE)),
    ("ceph.smb.cluster", lambda value: value.update(bindings=[{"path": "/auth/password", "secretRef": "join-password"}])),
])
def test_each_type_keeps_its_own_members(resource_type, change):
    value = request(resource_type)
    change(value)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.smb_request(value)


@pytest.mark.parametrize("resource_type", ["ceph.smb.join.auth", "ceph.smb.usersgroups"])
def test_an_apply_without_each_bound_secret_refuses_material(monkeypatch, tmp_path, resource_type):
    value = request(resource_type)
    for missing in ["smbSecret0", "sshKey"]:
        bound = material(value, "apply", tmp_path)
        del bound[missing]
        assert published(monkeypatch, value, "apply", bound)["reason"] == "material"
    assert published(monkeypatch, value, "observe", material(value, "apply", tmp_path))["reason"] == "material"


def test_a_malformed_digest_refuses_request(monkeypatch, tmp_path):
    value = request("ceph.smb.cluster")
    assert published(monkeypatch, value, "observe", material(value, "observe", tmp_path), digest="D" * 64)["reason"] == "request"


def evidence(**changes):
    value = {"daemons": [], "exists": False, "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "owned": True, "owner": "apply-123", "partial": False,
             "ready": False, "removed": False, "request": DIGEST, "resource": "", "version": "ceph-smb-evidence-v1"}
    value.update(changes)
    return value


SHARE_DAEMONS = [{"host": "node-1", "name": "smb.files.0.0.node-1.abcdef", "service": "smb.files"}]


@pytest.mark.parametrize("value", [
    evidence(),
    evidence(removed=True),
    evidence(exists=True, ready=True, resource="ceph.smb.cluster.files"),
    evidence(exists=True, ready=True, resource="ceph.smb.share.files.docs", daemons=SHARE_DAEMONS),
    evidence(exists=True, partial=True, resource="ceph.smb.share.files.docs"),
    evidence(owned=False, exists=True, resource="ceph.smb.usersgroups.users"),
])
def test_smb_evidence_admits_the_closed_shapes(value):
    assert ceph_core.smb_evidence(copy.deepcopy(value), DIGEST, request("ceph.smb.share")) == value


@pytest.mark.parametrize("value, digest", [
    (dict(evidence(), password="x"), DIGEST),
    (evidence(owner="apply-456"), DIGEST),
    (evidence(fsid="4c0a4300-aa43-458c-86d7-ac2256d1fc01"), DIGEST),
    (evidence(version="ceph-smb-evidence-v0"), DIGEST),
    (evidence(), "e" * 64),
    (evidence(ready=True, removed=True), DIGEST),
    (evidence(ready=1), DIGEST),
    (evidence(resource="r" * 257), DIGEST),
    (evidence(daemons=[dict(SHARE_DAEMONS[0], name="smb")]), DIGEST),
    (evidence(daemons=[dict(SHARE_DAEMONS[0], extra="x")]), DIGEST),
    (evidence(daemons=SHARE_DAEMONS * 129), DIGEST),
])
def test_smb_evidence_refuses_anything_else(value, digest):
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.smb_evidence(value, digest, request("ceph.smb.share"))
