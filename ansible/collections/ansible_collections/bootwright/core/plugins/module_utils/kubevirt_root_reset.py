"""Blank one kept KubeVirt machine's root disk under the frozen request, and read its mark.

The VirtualMachine, its UID, MACs, vTPM state and claim stay as the apply left
them: only the root DataVolume is deleted, by its UID, and the
VirtualMachine's own template creates it again, blank, at its frozen size and
class, the way the apply first created it. The mark the VirtualMachine then
carries names the staged destroy, the step whose removal blanked it and the
DataVolume it blanked, so an observation tells a completed reset from one that
never ran or was cut short.
"""

from __future__ import annotations

import re

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Refused, Unanswered

MARK_KEY = "bootwright.io/root-reset"
# A mark is `<operation>/<step>`: the staged destroy's operation identity and the step's block identity.
MARK = re.compile(r"op-[0-9a-f]{32}/[a-z0-9](?:[a-z0-9-]{0,251}[a-z0-9])?\Z")
UID = re.compile(r"[A-Za-z0-9-]{1,128}\Z")
POINTER = "/metadata/annotations/" + MARK_KEY.replace("~", "~0").replace("/", "~1")


def mark_of(removal, step):
    mark = "%s/%s" % (removal, step)
    if not MARK.fullmatch(mark):
        raise ValueError("root reset mark")
    return mark


def recorded(context, kind, object_name):
    """The UID the Machine block's apply recorded for one of its objects, or the empty string."""
    held = arm.receipt_for(context["pinned"]["receipts"], kind, object_name)
    return held["uid"] if held and isinstance(held.get("uid"), str) else ""


def held_mark(live, volume):
    """The mark a VirtualMachine carries for the root DataVolume of this UID, or the empty string."""
    value = ((live.get("metadata") or {}).get("annotations") or {}).get(MARK_KEY)
    mark, ignored_slash, marked = value.rpartition("/") if isinstance(value, str) else ("", "", "")
    return mark if MARK.fullmatch(mark) and UID.fullmatch(marked) and volume and marked == arm.uid(volume) else ""


def template_of(live, request):
    """The one root DataVolume template the VirtualMachine names, or None when it names another or more."""
    spec = live.get("spec") or {}
    templates = [entry for entry in spec.get("dataVolumeTemplates") or [] if isinstance(entry, dict)]
    volumes = [entry for entry in ((spec.get("template") or {}).get("spec") or {}).get("volumes") or [] if entry.get("name") == "root"]
    if len(templates) != 1 or (templates[0].get("metadata") or {}).get("name") != request["rootVolume"]:
        return None
    if len(volumes) != 1 or (volumes[0].get("dataVolume") or {}).get("name") != request["rootVolume"]:
        return None
    return templates[0].get("spec") or {}


def blank(volume, claim, template):
    """Whether a DataVolume is the one its template states, its claim its own."""
    spec = volume.get("spec") or {}
    storage, wanted = spec.get("storage") or {}, template.get("storage") or {}
    return (spec.get("source") == template.get("source") and storage.get("resources") == wanted.get("resources")
            and storage.get("storageClassName") == wanted.get("storageClassName") and claim is not None
            and arm.controlled_by(claim, arm.uid(volume)) and not (claim.get("metadata") or {}).get("deletionTimestamp")
            and bool(bound_volume(claim)))


def bound_volume(claim):
    """The PersistentVolume a claim is bound to, or the empty string."""
    name = ((claim or {}).get("spec") or {}).get("volumeName")
    return name if isinstance(name, str) else ""


def read(host, request, context, mark):
    """The VirtualMachine's identity and power, its root DataVolume and its mark, changing nothing.

    Answered stays false when the host does not answer; owned holds only for
    this context's own VirtualMachine by the UID its apply recorded, on the
    cluster its provider proof pinned.
    """
    root = {"blank": False, "name": request["rootVolume"], "phase": "", "present": False, "storage": "", "storageClass": "", "uid": "", "volume": ""}
    observation = {"answered": False, "instance": "", "kubeSystemUID": "", "owned": False, "postcondition": False,
                   "root": root, "rootReset": "", "virtualMachine": ""}
    try:
        observe(host, request, context, observation)
    except Unanswered:
        observation["answered"] = False
    observation["postcondition"] = completed(observation, request, context, mark)
    return observation


def observe(host, request, context, observation):
    root, vm = observation["root"], request["name"]
    observation["kubeSystemUID"] = host.cluster_uid()
    live = host.client.get(host.vm(vm))
    expected = recorded(context, "VirtualMachine", vm)
    owned = (live is not None and arm.derives(live, vm) and bool(expected) and arm.uid(live) == expected
             and observation["kubeSystemUID"] == context["pinned"]["kubeSystemUID"])
    observation["instance"] = arm.phase_of(host.client.get(host.vmi(vm)))[:32]
    if owned:
        observation["owned"], observation["virtualMachine"] = True, expected
        volume = host.client.get(host.dv(request["rootVolume"]))
        template = template_of(live, request)
        if volume is not None and arm.controlled_by(volume, expected) and not (volume.get("metadata") or {}).get("deletionTimestamp"):
            storage = (volume.get("spec") or {}).get("storage") or {}
            root.update(present=True, uid=arm.uid(volume)[:128], phase=str((volume.get("status") or {}).get("phase") or "")[:32],
                        storage=str(((storage.get("resources") or {}).get("requests") or {}).get("storage") or "")[:32],
                        storageClass=str(storage.get("storageClassName") or "")[:253])
            claim = host.client.get(host.pvc(request["rootVolume"]))
            root["blank"] = template is not None and blank(volume, claim, template)
            root["volume"] = bound_volume(claim)[:253] if root["blank"] else ""
            observation["rootReset"] = held_mark(live, volume)
    observation["answered"] = True


def completed(observation, request, context, mark):
    """Whether the root disk reads blank under this mark: present, Succeeded at its frozen size and class, never the
    DataVolume the apply created, the VirtualMachine stopped."""
    root = observation["root"]
    return (observation["answered"] and observation["owned"] and observation["rootReset"] == mark and not arm.running(observation["instance"])
            and root["present"] and root["blank"] and root["phase"] == "Succeeded" and root["name"] == request["rootVolume"]
            and root["uid"] != recorded(context, "DataVolume", request["rootVolume"]) and bool(root["volume"])
            and root["storage"] == "%dGi" % request["diskGiB"] and root["storageClass"] == arm.storage_class(request, context))


def admitted(host, request, context):
    """The VirtualMachine a reset starts from, once it is this context's own, stopped, its root the one its template names."""
    vm, namespace = request["name"], request["namespace"]
    arm.pinned(host, context)
    expected = recorded(context, "VirtualMachine", vm)
    if not expected:
        raise Refused("VirtualMachine %s/%s has no UID its Machine block recorded, so its root disk was not reset" % (namespace, vm))
    live = host.client.get(host.vm(vm))
    if live is None or not arm.derives(live, vm) or arm.uid(live) != expected:
        raise Refused("VirtualMachine %s/%s is not the one this context's Machine block recorded, so its root disk was not reset" % (namespace, vm))
    arm.prove_frozen(request, context, live)
    if template_of(live, request) is None:
        raise Refused("VirtualMachine %s/%s does not name its frozen root DataVolume in its template, so its root disk was not reset" % (namespace, vm))
    if arm.running(arm.instance_phase(host, vm)):
        raise Refused("VirtualMachine %s/%s is running, so its root disk was not reset; stop it first" % (namespace, vm))
    return live


def delete_root(host, request, volume):
    """Delete the root DataVolume by its UID, its claim first; a precondition that no longer holds refuses."""
    options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": arm.uid(volume)}, "propagationPolicy": "Foreground"}
    status, body = host.client.request("DELETE", host.dv(request["rootVolume"]), options)
    if status == 409:
        raise Refused("DataVolume %s/%s changed while its reset deleted it, so the reset was not marked" % (host.namespace, request["rootVolume"]))
    if status not in (200, 202, 404):
        raise Answered(status, body)


def new_claim(claim, volume, claimed, retired):
    """Whether a claim is the recreated DataVolume's own, another than every earlier claim, bound to a new volume."""
    return (claim is not None and arm.uid(claim) not in claimed and arm.controlled_by(claim, arm.uid(volume))
            and bool(bound_volume(claim)) and bound_volume(claim) not in retired)


def recreated(host, request, deleted, claimed, retired):
    """The root DataVolume the template created again: another UID than the deleted one, Succeeded, over a claim of its
    own that is bound to another PersistentVolume than any the earlier claims held."""
    def fresh(volume):
        if volume is None or arm.uid(volume) in deleted or (volume.get("metadata") or {}).get("deletionTimestamp"):
            return False
        claim = host.client.get(host.pvc(request["rootVolume"]))
        return (volume.get("status") or {}).get("phase") == "Succeeded" and new_claim(claim, volume, claimed, retired)
    volume = host.wait(lambda: host.client.get(host.dv(request["rootVolume"])), fresh, arm.VOLUME_SECONDS)
    if not fresh(volume):
        raise Refused("the template of VirtualMachine %s/%s did not create its root DataVolume again, blank, within %d seconds, so the reset was "
                      "not marked" % (host.namespace, request["name"], arm.VOLUME_SECONDS))
    return volume


def refuse_orphaned(host, request, live, volume, claim):
    """Refuse a root claim that outlives its DataVolume: none names an owner, or CDI handed it to the VirtualMachine when it
    garbage-collected the DataVolume, so the template would not create the DataVolume again."""
    if volume is not None:
        return
    where = "PersistentVolumeClaim %s/%s" % (host.namespace, request["rootVolume"])
    if arm.controlled_by(claim, arm.uid(live)):
        raise Refused(where + " is held by its VirtualMachine since CDI garbage-collected its DataVolume, so its template would not create the "
                      "DataVolume again and the root disk was not reset")
    if not (claim.get("metadata") or {}).get("ownerReferences"):
        raise Refused(where + " outlives its DataVolume and names no owner, so its root disk was not reset")


def reset(host, request, context, removal, step):
    """Blank the root disk once, marking the VirtualMachine; (changed, the observation).

    Only this context's own VirtualMachine, stopped, is touched, and only the
    root DataVolume its template names and it controls. A root already blank
    under this mark changes nothing. Otherwise any earlier mark goes first, so a
    reset cut short never leaves a mark over a disk it did not blank, and the
    mark, naming the recreated DataVolume's UID, is written last, once that
    DataVolume reads blank under a VirtualMachine still stopped.
    """
    mark = mark_of(removal, step)
    live = admitted(host, request, context)
    before = read(host, request, context, mark)
    if completed(before, request, context, mark):
        return False, before
    deleted, claimed, retired = set(), set(), set()
    volume = host.client.get(host.dv(request["rootVolume"]))
    claim = host.client.get(host.pvc(request["rootVolume"]))
    if claim is not None:
        refuse_orphaned(host, request, live, volume, claim)
        claimed.add(arm.uid(claim))
        retired.add(bound_volume(claim))
    if volume is not None and not arm.controlled_by(volume, arm.uid(live)):
        raise Refused("DataVolume %s/%s is not controlled by its VirtualMachine, so it was not deleted" % (host.namespace, request["rootVolume"]))
    held = ((live.get("metadata") or {}).get("annotations") or {}).get(MARK_KEY)
    if held is not None:
        arm.patch_vm(host, live, [{"op": "test", "path": POINTER, "value": held}, {"op": "remove", "path": POINTER}])
    if volume is not None:
        deleted.add(arm.uid(volume))
        delete_root(host, request, volume)
    fresh = recreated(host, request, deleted, claimed, retired - {""})
    after = host.client.get(host.vm(request["name"]))
    if after is None or arm.uid(after) != arm.uid(live) or arm.running(arm.instance_phase(host, request["name"])):
        raise Refused("VirtualMachine %s/%s did not stay stopped while its root disk was blanked, so the reset was not marked"
                      % (host.namespace, request["name"]))
    claim = host.client.get(host.pvc(request["rootVolume"]))
    if (not arm.controlled_by(fresh, arm.uid(live)) or not blank(fresh, claim, template_of(after, request) or {})
            or not new_claim(claim, fresh, claimed, retired - {""})):
        raise Refused("the recreated DataVolume %s/%s is not the blank one its template states, so the reset was not marked"
                      % (host.namespace, request["rootVolume"]))
    arm.patch_vm(host, after, [{"op": "add", "path": POINTER, "value": mark + "/" + arm.uid(fresh)}])
    return True, read(host, request, context, mark)
