"""Read the exact snapshot backing a published KubeVirt template."""

from __future__ import annotations

import re

from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Refused, path
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_machine import quantity

VM_SNAPSHOT = "snapshot.kubevirt.io/v1beta1"
CSI_SNAPSHOT = "snapshot.storage.k8s.io/v1"
LABEL = re.compile(r"[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?")


def label(value):
    return isinstance(value, str) and LABEL.fullmatch(value) is not None


def name(value):
    return isinstance(value, str) and len(value) <= 253 and all(label(part) for part in value.split("."))


def read_object(client, api_version, kind, resource, object_name, namespace="", ready=True):
    """Verify the exact identity the GET address names before using any links."""
    group, version = api_version.split("/", 1) if "/" in api_version else ("", api_version)
    if not name(object_name):
        raise Refused(kind + " has no valid reference")
    value = client.get(path(group, version, resource, namespace, object_name))
    if value is None:
        raise Refused(kind + " is absent")
    meta = value.get("metadata") or {}
    if (value.get("apiVersion") != api_version or value.get("kind") != kind
            or meta.get("name") != object_name or meta.get("namespace", "") != namespace
            or not label(meta.get("uid")) or meta.get("deletionTimestamp")):
        raise Refused(kind + " has unproved identity")
    status = value.get("status") or {}
    if ready and (status.get("readyToUse") is not True or status.get("error")):
        raise Refused(kind + " is not ready")
    return value


def captured_root(snapshot, content):
    """Resolve exactly one captured persistent root and its CSI snapshot link."""
    snapshot_meta, status = snapshot["metadata"], snapshot["status"]
    source = snapshot["spec"]["source"]
    captured = content["spec"]["source"]["virtualMachine"]
    meta = captured["metadata"]
    if (content["spec"].get("virtualMachineSnapshotName") != snapshot_meta["name"]
            or source.get("apiGroup") != "kubevirt.io" or source.get("kind") != "VirtualMachine"
            or source.get("name") != meta.get("name") or meta.get("namespace") != snapshot_meta["namespace"]
            or not label(status.get("sourceUID")) or status["sourceUID"] != meta.get("uid")):
        raise Refused("the snapshot content does not identify the captured source VM")
    if "Online" in (status.get("indications") or ()) or any(
            member.get("indication") == "Online" for member in status.get("sourceIndications") or ()):
        raise Refused("the template snapshot was captured online")
    guest = captured["spec"]["template"]["spec"]
    disks, volumes = guest["domain"]["devices"]["disks"], guest["volumes"]
    backups = content["spec"].get("volumeBackups") or []
    if len(disks) != 1 or len(volumes) != 1 or len(backups) != 1:
        raise Refused("the captured VM does not have one backed root disk")
    disk, volume, backup = disks[0], volumes[0], backups[0]
    if ("disk" not in disk or not label(disk.get("name"))
            or disk["name"] != volume.get("name") or disk["name"] != backup.get("volumeName")
            or not (volume.get("persistentVolumeClaim") or volume.get("dataVolume"))
            or "tpm" in guest["domain"]["devices"]):
        raise Refused("the captured VM does not have one backed root disk without TPM")
    claim_name = (backup["persistentVolumeClaim"]["metadata"] or {}).get("name")
    source_claim = (volume.get("dataVolume") or {}).get("name") or (volume.get("persistentVolumeClaim") or {}).get("claimName")
    if not name(claim_name) or source_claim != claim_name:
        raise Refused("the captured root backup has no claim identity")
    return backup.get("volumeSnapshotName"), claim_name


def prove_backing(volume, content, csi_content, source_claim):
    reference = csi_content["spec"]["volumeSnapshotRef"]
    meta = volume["metadata"]
    owner = {"apiVersion": VM_SNAPSHOT, "kind": "VirtualMachineSnapshotContent",
             "name": content["metadata"]["name"], "uid": content["metadata"]["uid"], "controller": True}
    owners = meta.get("ownerReferences") or []
    if (len(owners) != 1 or any(owners[0].get(key) != value for key, value in owner.items())
            or volume["spec"]["source"].get("persistentVolumeClaimName") != source_claim):
        raise Refused("the CSI snapshot does not belong to the captured root backup")
    if any(reference.get(key) != meta[key] for key in ("name", "namespace", "uid")):
        raise Refused("the CSI snapshot content does not bind to the exact volume snapshot")
    driver = csi_content["spec"].get("driver")
    size = quantity(volume["status"].get("restoreSize"))
    handle = csi_content["status"].get("snapshotHandle")
    if not name(driver) or size <= 0 or not isinstance(handle, str) or not handle.strip():
        raise Refused("the CSI snapshot has no proved driver, size or backing")
    return driver, size


def read_template_source(client, cluster_uid, reference):
    """Read-only source proof. Publish identities and capacity, with no guest bytes or storage handle."""
    if (not label(cluster_uid) or not isinstance(reference, dict) or set(reference) != {"namespace", "name", "uid"}
            or not label(reference["namespace"]) or not name(reference["name"]) or not label(reference["uid"])):
        raise Refused("the template source must name one cluster UID and one VM snapshot namespace, name and UID")
    try:
        return read_published_snapshot(client, cluster_uid, reference)
    except (AttributeError, IndexError, KeyError, TypeError):
        raise Refused("the API returned a malformed template source") from None


def read_published_snapshot(client, cluster_uid, reference):
    cluster = read_object(client, "v1", "Namespace", "namespaces", "kube-system", ready=False)
    if cluster["metadata"]["uid"] != cluster_uid:
        raise Refused("the host cluster differs from the pinned cluster")
    namespace = reference["namespace"]
    snapshot = read_object(client, VM_SNAPSHOT, "VirtualMachineSnapshot", "virtualmachinesnapshots", reference["name"], namespace)
    if snapshot["metadata"]["uid"] != reference["uid"]:
        raise Refused("the VM snapshot UID differs from the published reference")
    content_name = snapshot["status"].get("virtualMachineSnapshotContentName")
    if content_name != "vmsnapshot-content-" + reference["uid"]:
        raise Refused("the VM snapshot does not identify the content generated for its UID")
    content = read_object(client, VM_SNAPSHOT, "VirtualMachineSnapshotContent", "virtualmachinesnapshotcontents", content_name, namespace)
    volume_name, claim_name = captured_root(snapshot, content)
    volume = read_object(client, CSI_SNAPSHOT, "VolumeSnapshot", "volumesnapshots", volume_name, namespace)
    backing_name = volume["status"].get("boundVolumeSnapshotContentName")
    backing = read_object(client, CSI_SNAPSHOT, "VolumeSnapshotContent", "volumesnapshotcontents", backing_name)
    driver, size = prove_backing(volume, content, backing, claim_name)
    return {"clusterUID": cluster_uid, "namespace": namespace, "snapshotName": reference["name"], "snapshotUID": reference["uid"],
            "snapshotContentUID": content["metadata"]["uid"],
            "volumeSnapshot": volume_name, "volumeSnapshotUID": volume["metadata"]["uid"],
            "volumeSnapshotContent": backing_name, "volumeSnapshotContentUID": backing["metadata"]["uid"],
            "driver": driver, "restoreBytes": size}
