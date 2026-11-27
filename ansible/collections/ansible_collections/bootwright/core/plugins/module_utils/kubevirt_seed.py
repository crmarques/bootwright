"""Mount and withdraw a private per-clone NoCloud seed on a pinned stopped VM."""

from __future__ import annotations

import re

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Refused, Unanswered, path

VOLUME = "seed"


def clone(host, vm, *, prove_root=True):
    live = arm.port_vm(host, vm)
    spec = live["spec"]
    if spec.get("runStrategy") != "Manual" or live["metadata"].get("deletionTimestamp"):
        raise Refused("the pinned template VM has no available manual run strategy")
    if not prove_root:
        return live
    templates = spec.get("dataVolumeTemplates") or []
    if len(templates) != 1:
        raise Refused("the pinned VM does not have one snapshot-backed root template")
    template = templates[0]
    name = template["metadata"]["name"]
    source = template.get("spec", {}).get("source") or {}
    volumes = [volume for volume in spec["template"]["spec"]["volumes"] if volume.get("name") == "root"]
    if not source.get("snapshot") or volumes != [{"name": "root", "dataVolume": {"name": name}}]:
        raise Refused("the pinned VM does not mount its snapshot-backed root template")
    data_volume, claim = host.client.get(host.dv(name)), host.client.get(host.pvc(name))
    if (not data_volume or not claim or not arm.uid(data_volume) or not arm.uid(claim)
            or arm.uid(data_volume) != host.pin.get("rootVolume") or arm.uid(claim) != host.pin.get("rootClaim")
            or data_volume["metadata"].get("deletionTimestamp") or claim["metadata"].get("deletionTimestamp")
            or not arm.controlled_by(data_volume, arm.uid(live)) or not arm.controlled_by(claim, arm.uid(data_volume))
            or data_volume.get("spec", {}).get("source") != source
            or data_volume.get("status", {}).get("phase") != "Succeeded" or claim.get("status", {}).get("phase") != "Bound"):
        raise Refused("the pinned VM no longer has its recorded snapshot-backed root disk")
    return live


def owned_instance(host, vm, live):
    instance = host.client.get(host.vmi(vm))
    if instance is not None and (not arm.uid(instance) or not arm.controlled_by(instance, arm.uid(live))):
        raise Refused("a foreign instance occupies the pinned template VM's name")
    return instance


def target(host, vm, *, prove_root=True):
    live = clone(host, vm, prove_root=prove_root)
    if host.client.get(host.vmi(vm)) is not None:
        raise Refused("the pinned template VM is not stopped with a manual run strategy")
    return live


def identity(host, vm, live, operation, block):
    name = arm.media_volume(vm + "-seed", vm)
    resource = path("", "v1", "secrets", host.namespace, name)
    if (not isinstance(operation, str) or not re.fullmatch(r"op-[0-9a-f]{32}", operation)
            or not isinstance(block, str) or not re.fullmatch(r"[a-z0-9](?:[-a-z0-9]{0,251}[a-z0-9])?", block)):
        raise Refused("the template seed has no operation and block identity")
    owner = arm.mark(operation, block, "Secret", host.namespace, name)
    reference = {"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine", "name": vm, "uid": arm.uid(live)}
    meta = {"name": name, "namespace": host.namespace, "annotations": {arm.OWNER: owner}, "ownerReferences": [reference]}
    return resource, meta


def proved_secret(client, resource, meta):
    live = client.get(resource)
    if live is not None:
        actual = live["metadata"]
        if (actual.get("annotations", {}).get(arm.OWNER) != meta["annotations"][arm.OWNER]
                or any(actual.get(key) != meta[key] for key in ("name", "namespace"))
                or actual.get("ownerReferences") != meta["ownerReferences"] or not arm.uid(live)
                or actual.get("deletionTimestamp") or live.get("immutable") is not True or live.get("type") != "Opaque"):
            raise Refused("a foreign or unavailable Secret occupies the template seed name")
    return live


def slots(vm):
    spec = vm["spec"]["template"]["spec"]
    disks, volumes = spec["domain"]["devices"]["disks"], spec["volumes"]
    return indexes(disks), indexes(volumes)


def indexes(entries):
    selected = [(index, entry) for index, entry in enumerate(entries) if entry.get("name") == VOLUME]
    if len(selected) > 1:
        raise Refused("the template VM has multiple seed slots")
    return selected[0] if selected else None


def volume(name):
    return {"name": VOLUME, "cloudInitNoCloud": {"secretRef": {"name": name}, "networkDataSecretRef": {"name": name}}}


def disk():
    return {"name": VOLUME, "disk": {"bus": "virtio"}}


def matches(disk_slot, volume_slot, name):
    if bool(disk_slot) != bool(volume_slot):
        raise Refused("the template VM has an incomplete seed mount")
    if disk_slot and (disk_slot[1] != disk() or volume_slot[1] != volume(name)):
        raise Refused("the template VM already has another seed in the requested slot")
    return bool(volume_slot)


def patch(host, live, commands):
    version = live["metadata"].get("resourceVersion")
    if not isinstance(version, str) or not version:
        raise Refused("the template VM has no version for a guarded seed patch")
    arm.patch_vm(host, live, [{"op": "test", "path": "/metadata/resourceVersion", "value": version}] + commands)


def prove_mac(live, mac):
    interfaces = live["spec"]["template"]["spec"]["domain"]["devices"].get("interfaces") or []
    if len(interfaces) != 1 or interfaces[0].get("macAddress") != mac:
        raise Refused("the template seed's network does not match the pinned VM interface")


def attach(host, vm, operation, block, data, mac):
    live = target(host, vm)
    prove_mac(live, mac)
    resource, meta = identity(host, vm, live, operation, block)
    name = meta["name"]
    mounted = matches(*slots(live), name)
    secret = proved_secret(host.client, resource, meta)
    changed = False
    if secret is None:
        if mounted:
            raise Refused("the template VM mounts a missing seed Secret")
        body = {"apiVersion": "v1", "kind": "Secret", "metadata": meta, "type": "Opaque", "immutable": True, "data": data}
        try:
            status, result = host.client.request("POST", path("", "v1", "secrets", host.namespace), body)
            if status not in (201, 409):
                raise Answered(status, result)
        except Unanswered:
            pass
        secret = proved_secret(host.client, resource, meta)
        changed = True
    if secret is None or secret.get("data") != data:
        raise Refused("the template seed Secret did not hold this operation's data")
    if not mounted:
        live = target(host, vm)
        prove_mac(live, mac)
        if matches(*slots(live), name):
            raise Refused("the template VM seed mount changed during attachment")
        patch(host, live, [
            {"op": "add", "path": "/spec/template/spec/domain/devices/disks/-", "value": disk()},
            {"op": "add", "path": "/spec/template/spec/volumes/-", "value": volume(name)},
        ])
        changed = True
    live = target(host, vm)
    prove_mac(live, mac)
    current = proved_secret(host.client, resource, meta)
    if (not matches(*slots(live), name) or current is None or arm.uid(current) != arm.uid(secret)
            or current.get("data") != data):
        raise Refused("the template seed mount was not proved after attachment")
    return {"changed": changed, "secretUID": arm.uid(secret), "postcondition": True}


def prove_mounted(host, vm, live, operation, block, expected_uid):
    if not isinstance(expected_uid, str) or not expected_uid:
        raise Refused("the template seed has no recorded UID")
    resource, meta = identity(host, vm, live, operation, block)
    secret = proved_secret(host.client, resource, meta)
    if secret is None or arm.uid(secret) != expected_uid or not matches(*slots(live), meta["name"]):
        raise Refused("the pinned template VM does not mount the recorded seed Secret")


def start(host, vm, operation, block, expected_uid):
    """Start only the VM that still mounts its exact prepared seed."""
    live = clone(host, vm)
    prove_mounted(host, vm, live, operation, block, expected_uid)
    owned_instance(host, vm, live)
    return arm.change_power(host, vm, "on")


def stop(host, vm, operation, block, expected_uid, first_instance_uid):
    """Stop only the instance which supplied the recorded first-boot proof."""
    if not isinstance(first_instance_uid, str) or not first_instance_uid:
        raise Refused("the template VM has no recorded first-boot instance")
    live = clone(host, vm)
    prove_mounted(host, vm, live, operation, block, expected_uid)
    instance = owned_instance(host, vm, live)
    if instance is None:
        return {"changed": False, "postcondition": True}
    if arm.uid(instance) != first_instance_uid:
        raise Refused("the template VM instance does not match the first-boot proof")
    status, ignored = host.client.request("PUT", host.subresource(vm, "stop"), {})
    if status not in (200, 202, 409):
        raise Answered(status, ignored)
    if host.wait(lambda: owned_instance(host, vm, clone(host, vm)), lambda vmi: vmi is None, arm.POWER_SECONDS) is not None:
        raise Refused("the pinned template VM instance remains present after stop")
    return {"changed": True, "postcondition": True}


def prove_seedless(host, vm, live, operation, block):
    resource, ignored = identity(host, vm, live, operation, block)
    if any(slots(live)) or host.client.get(resource) is not None:
        raise Refused("the pinned template VM still has seed material")
    spec = live["spec"]["template"]["spec"]
    if any("cloudInitNoCloud" in entry or "cloudInitConfigDrive" in entry for entry in spec["volumes"]):
        raise Refused("the pinned template VM has another cloud-init volume")


def start_seedless(host, vm, operation, block, first_instance_uid):
    """Start after withdrawal or resume a reboot, never reuse the seeded instance."""
    if not isinstance(first_instance_uid, str) or not first_instance_uid:
        raise Refused("the template VM has no recorded first-boot instance")
    live = clone(host, vm)
    prove_seedless(host, vm, live, operation, block)
    instance = owned_instance(host, vm, live)
    if arm.uid(instance) == first_instance_uid:
        raise Refused("the seeded template instance has not been replaced")
    return arm.change_power(host, vm, "on")


def withdraw(host, vm, operation, block):
    live = target(host, vm, prove_root=False)
    resource, meta = identity(host, vm, live, operation, block)
    mounted, seeded = slots(live)
    secret = proved_secret(host.client, resource, meta)
    changed = False
    if matches(mounted, seeded, meta["name"]):
        patch(host, live, [
            {"op": "remove", "path": "/spec/template/spec/domain/devices/disks/%d" % mounted[0]},
            {"op": "remove", "path": "/spec/template/spec/volumes/%d" % seeded[0]},
        ])
        changed = True
    if any(slots(target(host, vm, prove_root=False))):
        raise Refused("the template seed mount remains present")
    if secret is not None:
        changed = arm.delete(host, resource, arm.uid(secret)) or changed
    if host.client.get(resource) is not None:
        raise Refused("the template seed Secret remains present")
    return {"changed": changed, "postcondition": True}
