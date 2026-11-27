"""The template source read proves the snapshot graph without touching the destination."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_template
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Refused, path as resource_path
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    CLAIMS, VM_PATH as TARGET_VM_PATH, FakeAPI, client, context, host, request,
)

SOURCE = {"namespace": "golden-images", "name": "rhel-9.8-v3", "uid": "snapshot-uid"}
CLUSTER = "cluster-uid"
NAMESPACE = SOURCE["namespace"]
VM_GROUP = "snapshot.kubevirt.io"
CSI_GROUP = "snapshot.storage.k8s.io"
VM_CONTENT_NAME = "vmsnapshot-content-" + SOURCE["uid"]
CSI_SNAPSHOT_NAME = "vmsnapshot-snapshot-uid-volume-root"
CSI_CONTENT_NAME = "snapcontent-root"
CLUSTER_PATH = resource_path("", "v1", "namespaces", name="kube-system")
VM_PATH = resource_path(VM_GROUP, "v1beta1", "virtualmachinesnapshots", NAMESPACE, SOURCE["name"])
VM_CONTENT_PATH = resource_path(VM_GROUP, "v1beta1", "virtualmachinesnapshotcontents", NAMESPACE, VM_CONTENT_NAME)
CSI_PATH = resource_path(CSI_GROUP, "v1", "volumesnapshots", NAMESPACE, CSI_SNAPSHOT_NAME)
CSI_CONTENT_PATH = resource_path(CSI_GROUP, "v1", "volumesnapshotcontents", name=CSI_CONTENT_NAME)


def native(kind, version, name, token, namespace=None, **fields):
    meta = {"name": name, "uid": token}
    if namespace is not None:
        meta["namespace"] = namespace
    return {"kind": kind, "apiVersion": version, "metadata": meta, **fields}


def published(api=None):
    api = api if api is not None else FakeAPI()
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
        "clusterUID": CLUSTER, "namespace": NAMESPACE, "snapshotName": SOURCE["name"], "snapshotUID": SOURCE["uid"], "snapshotContentUID": "content-uid",
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


class CloneAPI(FakeAPI):
    replace_backing = False

    def create(self, path, body):
        result = super().create(path, body)
        if self.replace_backing:
            self.objects[CSI_CONTENT_PATH]["metadata"]["uid"] = "replacement"
        return result

    def claim(self, dv):
        super().claim(dv)
        self.objects[CLAIMS + dv["metadata"]["name"]]["status"] = {"phase": "Bound"}


def clone_inputs():
    api = published(CloneAPI())
    api.objects[resource_path("storage.k8s.io", "v1", "storageclasses", name="guest-block")] = {
        "provisioner": "csi.example.test",
    }
    frozen = request(templateSnapshot=SOURCE)
    proof = context(uid=CLUSTER)
    proof["pinned"]["templateSnapshot"] = kubevirt_template.read_template_source(client(api), CLUSTER, SOURCE)
    api.requests.clear()
    return api, frozen, proof


def test_host_proof_publishes_the_resolved_backing():
    api = published()
    observation = {"kubeSystemUID": CLUSTER, "postcondition": True, "failures": []}
    action.prove_template_snapshots(client(api), {"namespace": "guests", "templateSnapshots": [SOURCE]}, observation)
    assert api.requests[0] == ("POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews")
    wire = protocol.host(observation, "a" * 64)
    assert wire["postcondition"] and wire["templateSnapshots"][0]["volumeSnapshotContentUID"] == "backing-uid"


def test_snapshot_backed_clone_recovers_lost_reply_and_removes_without_source():
    api, frozen, proof = clone_inputs()
    api.lose_create_reply = True
    changed, evidence = arm.apply(host(api), frozen, proof)
    assert changed and evidence["postcondition"] and evidence["templateSnapshot"] == proof["pinned"]["templateSnapshot"]
    assert evidence["instance"] == ""
    target = api.objects[TARGET_VM_PATH]
    assert target["spec"]["runStrategy"] == "Manual"
    assert target["spec"]["dataVolumeTemplates"][0]["spec"]["source"] == {
        "snapshot": {"namespace": NAMESPACE, "name": CSI_SNAPSHOT_NAME},
    }
    assert len([entry for entry in api.requests if entry[0] == "POST"]) == 1
    proof["pinned"]["receipts"] = evidence["receipts"]
    proof["pinned"]["templateCloneCompleted"] = True
    del api.objects[VM_PATH]
    api.requests.clear()
    assert arm.observe(host(api), frozen, proof)["postcondition"]
    changed, removed = arm.destroy(host(api), frozen, proof)
    assert changed and removed["absent"]
    assert all(NAMESPACE not in entry[1] for entry in api.requests)
    assert CSI_PATH in api.objects and CSI_CONTENT_PATH in api.objects


def test_changed_source_is_refused_before_the_clone():
    api, frozen, proof = clone_inputs()
    api.objects[CSI_CONTENT_PATH]["metadata"]["uid"] = "different-backing"
    with pytest.raises(Refused, match="backing of the template snapshot changed"):
        arm.apply(host(api), frozen, proof)
    assert all(entry[0] == "GET" for entry in api.requests)


def test_blank_reset_refuses_template_before_any_request():
    api, frozen, unused_proof = clone_inputs()
    assert unused_proof["pinned"]["templateSnapshot"]
    with pytest.raises(Refused, match="cannot be reset"):
        action.root("root_reset", client(api), {"request": frozen})
    assert api.requests == []


def test_source_changed_during_clone_leaves_the_owned_machine_stopped():
    api, frozen, proof = clone_inputs()
    api.replace_backing = True
    with pytest.raises(Refused, match="backing of the template snapshot changed"):
        arm.apply(host(api), frozen, proof)
    assert TARGET_VM_PATH in api.objects
    assert not any(entry[1].endswith("/start") for entry in api.requests)
    changed, removed = arm.destroy(host(api), frozen, proof)
    assert changed and removed["absent"] and CSI_CONTENT_PATH in api.objects


def test_clone_observation_refuses_a_replaced_root_claim():
    api, frozen, proof = clone_inputs()
    changed, evidence = arm.apply(host(api), frozen, proof)
    assert changed and evidence["postcondition"]
    proof["pinned"]["receipts"] = evidence["receipts"]
    proof["pinned"]["templateCloneCompleted"] = True
    api.objects[CLAIMS + frozen["rootVolume"]]["metadata"]["uid"] = "replacement-claim"
    with pytest.raises(Refused, match="bound clone root claim was replaced"):
        arm.observe(host(api), frozen, proof)


def test_clone_refuses_an_existing_claim_without_creating_a_vm():
    api, frozen, proof = clone_inputs()
    api.objects[CLAIMS + frozen["rootVolume"]] = api.meta(frozen["rootVolume"])
    with pytest.raises(Refused, match="exists before creating"):
        arm.apply(host(api), frozen, proof)
    assert TARGET_VM_PATH not in api.objects
    assert all(entry[0] == "GET" for entry in api.requests)
