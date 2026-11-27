"""The template source read proves the snapshot graph without touching the destination."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import path
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import FakeAPI, client

SOURCE = {"namespace": "golden-images", "name": "rhel-9.8-v3", "uid": "snapshot-uid"}
CLUSTER = "cluster-uid"
NAMESPACE = SOURCE["namespace"]
VM_GROUP = "snapshot.kubevirt.io"
CSI_GROUP = "snapshot.storage.k8s.io"
VM_CONTENT_NAME = "vmsnapshot-content-" + SOURCE["uid"]
CSI_SNAPSHOT_NAME = "vmsnapshot-snapshot-uid-volume-root"
CSI_CONTENT_NAME = "snapcontent-root"
CLUSTER_PATH = path("", "v1", "namespaces", name="kube-system")
VM_PATH = path(VM_GROUP, "v1beta1", "virtualmachinesnapshots", NAMESPACE, SOURCE["name"])
VM_CONTENT_PATH = path(VM_GROUP, "v1beta1", "virtualmachinesnapshotcontents", NAMESPACE, VM_CONTENT_NAME)
CSI_PATH = path(CSI_GROUP, "v1", "volumesnapshots", NAMESPACE, CSI_SNAPSHOT_NAME)
CSI_CONTENT_PATH = path(CSI_GROUP, "v1", "volumesnapshotcontents", name=CSI_CONTENT_NAME)


def native(kind, version, name, token, namespace=None, **fields):
    meta = {"name": name, "uid": token}
    if namespace is not None:
        meta["namespace"] = namespace
    return {"kind": kind, "apiVersion": version, "metadata": meta, **fields}


def published():
    api = FakeAPI()
    api.objects[CLUSTER_PATH] = native("Namespace", "v1", "kube-system", CLUSTER)
    api.objects[VM_PATH] = native(
        "VirtualMachineSnapshot", VM_GROUP + "/v1beta1", SOURCE["name"], SOURCE["uid"], NAMESPACE,
        spec={"source": {"apiGroup": "kubevirt.io", "kind": "VirtualMachine", "name": "golden-vm"}},
        status={"sourceUID": "vm-uid", "readyToUse": True, "virtualMachineSnapshotContentName": VM_CONTENT_NAME},
    )
    root = {"domain": {"devices": {"disks": [{"name": "root", "disk": {"bus": "virtio"}}]}},
            "volumes": [{"name": "root", "dataVolume": {"name": "golden-root"}}]}
    captured = {"metadata": {"name": "golden-vm", "namespace": NAMESPACE, "uid": "vm-uid"},
                "spec": {"template": {"spec": root}}}
    api.objects[VM_CONTENT_PATH] = native(
        "VirtualMachineSnapshotContent", VM_GROUP + "/v1beta1", VM_CONTENT_NAME, "content-uid", NAMESPACE,
        spec={"virtualMachineSnapshotName": SOURCE["name"], "source": {"virtualMachine": captured},
              "volumeBackups": [{"volumeName": "root", "persistentVolumeClaim": {"metadata": {"name": "golden-root"}},
                                 "volumeSnapshotName": CSI_SNAPSHOT_NAME}]},
        status={"readyToUse": True},
    )
    volume = native(
        "VolumeSnapshot", CSI_GROUP + "/v1", CSI_SNAPSHOT_NAME, "volume-uid", NAMESPACE,
        spec={"source": {"persistentVolumeClaimName": "golden-root"}},
        status={"readyToUse": True, "restoreSize": "40Gi", "boundVolumeSnapshotContentName": CSI_CONTENT_NAME},
    )
    volume["metadata"]["ownerReferences"] = [{
        "apiVersion": VM_GROUP + "/v1beta1", "kind": "VirtualMachineSnapshotContent", "name": VM_CONTENT_NAME,
        "uid": "content-uid", "controller": True, "blockOwnerDeletion": True,
    }]
    api.objects[CSI_PATH] = volume
    api.objects[CSI_CONTENT_PATH] = native(
        "VolumeSnapshotContent", CSI_GROUP + "/v1", CSI_CONTENT_NAME, "backing-uid",
        spec={"volumeSnapshotRef": {"namespace": NAMESPACE, "name": CSI_SNAPSHOT_NAME, "uid": "volume-uid"},
              "driver": "csi.example.test"},
        status={"readyToUse": True, "snapshotHandle": "private-storage-handle"},
    )
    return api


def read(monkeypatch, fake, **changes):
    monkeypatch.setattr(fake, "settle", lambda: None)
    monkeypatch.setattr(action, "connect", lambda *_: client(fake))
    instance = object.__new__(action.ActionModule)
    instance._task = SimpleNamespace(check_mode=True, args={
        "operation": "template_source_read", "kubeconfig": "kubeconfig", "scratch": "scratch",
        "cluster_uid": CLUSTER, "snapshot_ref": SOURCE, **changes,
    })
    return instance.run()


def test_shared_snapshot_source_read_proves_only_identities(monkeypatch):
    api = published()
    report = read(monkeypatch, api)
    assert report == {"changed": False, "_ansible_no_log": True, "observation": {
        "clusterUID": CLUSTER, "namespace": NAMESPACE, "snapshotUID": SOURCE["uid"], "snapshotContentUID": "content-uid",
        "volumeSnapshot": CSI_SNAPSHOT_NAME, "volumeSnapshotUID": "volume-uid",
        "volumeSnapshotContent": CSI_CONTENT_NAME, "volumeSnapshotContentUID": "backing-uid",
        "driver": "csi.example.test", "restoreBytes": 40 * (1 << 30),
    }}
    assert api.requests == [("GET", resource) for resource in (CLUSTER_PATH, VM_PATH, VM_CONTENT_PATH, CSI_PATH, CSI_CONTENT_PATH)]


@pytest.mark.parametrize("resource,keys,value,reason", [
    (CLUSTER_PATH, ("metadata", "uid"), "wrong-cluster", "pinned cluster"),
    (VM_PATH, ("metadata", "uid"), "replacement", "published reference"),
    (VM_PATH, ("status", "readyToUse"), False, "not ready"),
    (VM_PATH, ("status", "virtualMachineSnapshotContentName"), "elsewhere", "generated for its UID"),
    (VM_PATH, ("status", "sourceUID"), "another-vm", "captured source VM"),
    (VM_PATH, ("status", "indications"), ["Online"], "captured online"),
    (VM_CONTENT_PATH, ("spec", "volumeBackups"), [], "one backed root disk"),
    (CSI_PATH, ("metadata", "ownerReferences"), [], "captured root backup"),
    (CSI_PATH, ("metadata", "ownerReferences"), ["not-an-object"], "malformed template source"),
    (CSI_PATH, ("metadata", "deletionTimestamp"), "2026-11-01T00:00:00Z", "unproved identity"),
    (CSI_CONTENT_PATH, ("spec", "volumeSnapshotRef", "uid"), "different-volume", "exact volume snapshot"),
    (CSI_CONTENT_PATH, ("status", "snapshotHandle"), "", "no proved driver, size or backing"),
])
def test_shared_source_refuses_a_broken_link_without_a_write(monkeypatch, resource, keys, value, reason):
    api = published()
    field = api.objects[resource]
    for key in keys[:-1]:
        field = field[key]
    field[keys[-1]] = value
    report = read(monkeypatch, api)
    assert report["failed"] and report["_ansible_no_log"] and "observation" not in report
    assert reason in report["msg"]
    assert all(request[0] == "GET" for request in api.requests)


def test_invalid_source_path_never_reaches_the_host(monkeypatch):
    api = published()
    report = read(monkeypatch, api, snapshot_ref=dict(SOURCE, namespace="../guests"))
    assert report["failed"] and api.requests == []


def test_silence_cannot_prove_snapshot_absence(monkeypatch):
    api = published()
    api.unanswered.add(VM_PATH)
    report = read(monkeypatch, api)
    assert report["failed"] and report["msg"] == "the host cluster did not answer"


def test_a_missing_snapshot_is_refused_before_reading_its_backing(monkeypatch):
    api = published()
    del api.objects[VM_PATH]
    report = read(monkeypatch, api)
    assert report["failed"] and report["msg"] == "VirtualMachineSnapshot is absent"
    assert api.requests == [("GET", CLUSTER_PATH), ("GET", VM_PATH)]
