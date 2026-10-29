"""The KubeVirt arm's rules over a host cluster's API: names, ownership, the
provider proof, the VirtualMachine block and the machine port.

Every rule here is pure over a kubevirt_api.Client, so a fake API proves it:
objects are created, never applied over; a recorded UID proves ownership and
an owner mark recovers a lost create reply; a same-named object that neither
proves is foreign and refuses; only a 404 is absence, and a host that does
not answer is unknown, never absent.
"""

from __future__ import annotations

import hashlib
import json
import time
from datetime import datetime

from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Refused, Unanswered, path

OWNER = "bootwright.io/owner"
MEDIA = "bootwright.io/media-identity"
DOMAIN = "bootwright.substrate.kubevirt.owner-v1"
MANAGED = ("app.kubernetes.io/managed-by", "bootwright")
CONTEXT, MACHINE = "bootwright.io/context", "bootwright.io/machine"
BIND = "cdi.kubevirt.io/storage.bind.immediate.requested"
DEFAULT_VIRT = "storageclass.kubevirt.io/is-default-virt-class"
DEFAULT_CLASS = "storageclass.kubernetes.io/is-default-class"
SKEW_SECONDS = 5
POLL_SECONDS = 5
POWER_SECONDS = 300
VOLUME_SECONDS = 300
MAX_CLAIM_PAGES = 10
MEDIA_VOLUME = "media"
GIB = 1 << 30


def name(context, machine):
    digest = hashlib.sha256(("bootwright.substrate.kubevirt.name-v1\0" + context + "\0" + machine).encode()).hexdigest()
    return machine[:48].rstrip("-") + "-" + digest[:8]


def mark(operation, block, kind, namespace, object_name):
    return hashlib.sha256("\0".join((DOMAIN, operation, block, kind, namespace, object_name)).encode()).hexdigest()


def labels(context, machine):
    return {MANAGED[0]: MANAGED[1], CONTEXT: context, MACHINE: machine}


def derives(live, vm):
    """Whether a live object's labels derive the VirtualMachine name it is addressed by."""
    held = (live.get("metadata") or {}).get("labels") or {}
    if held.get(MANAGED[0]) != MANAGED[1] or not isinstance(held.get(CONTEXT), str) or not isinstance(held.get(MACHINE), str):
        return False
    return name(held[CONTEXT], held[MACHINE]) == vm


def timestamp(value):
    parsed = datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamp has no timezone")
    return parsed.timestamp()


def uid(live):
    return ((live or {}).get("metadata") or {}).get("uid") or ""


def ownership(kind, live, receipts, context):
    """own, foreign or absent: a receipt's UID proves it; else the mark and a creation after registration."""
    if live is None:
        return "absent"
    metadata = live.get("metadata") or {}
    held = next((r for r in receipts if r["kind"] == kind and r["name"] == metadata.get("name")), None)
    if held is not None:
        return "own" if held["uid"] and metadata.get("uid") == held["uid"] else "foreign"
    expected = mark(context["operation"], context["block"], kind, metadata.get("namespace", ""), metadata.get("name", ""))
    if (metadata.get("annotations") or {}).get(OWNER) != expected or not metadata.get("uid"):
        return "foreign"
    try:
        recent = timestamp(metadata["creationTimestamp"]) >= timestamp(context["registered"]) - SKEW_SECONDS
    except (KeyError, TypeError, ValueError):
        return "foreign"
    return "own" if recent else "foreign"


def controlled_by(live, owner_uid):
    """Whether an object's controller owner reference names exactly this owner."""
    return any(ref.get("controller") and ref.get("uid") == owner_uid
               for ref in ((live or {}).get("metadata") or {}).get("ownerReferences") or [])


def owned_by(live, owner_uids):
    return any(ref.get("uid") in owner_uids for ref in ((live or {}).get("metadata") or {}).get("ownerReferences") or [])


class Host:
    """One request's view of the host: its paths, a deadline and the cluster pin."""

    def __init__(self, client, namespace, deadline, clock=time.time, sleep=time.sleep, pin=None):
        self.client, self.namespace, self.deadline = client, namespace, deadline
        self.clock, self.sleep = clock, sleep
        self.pin = pin or {}

    def vm(self, vm=""):
        return path("kubevirt.io", "v1", "virtualmachines", self.namespace, vm)

    def vmi(self, vm):
        return path("kubevirt.io", "v1", "virtualmachineinstances", self.namespace, vm)

    def dv(self, volume=""):
        return path("cdi.kubevirt.io", "v1beta1", "datavolumes", self.namespace, volume)

    def pvc(self, claim=""):
        return path("", "v1", "persistentvolumeclaims", self.namespace, claim)

    def subresource(self, vm, verb):
        return path("subresources.kubevirt.io", "v1", "virtualmachines", self.namespace, vm, verb)

    def cluster_uid(self):
        live = self.client.get(path("", "v1", "namespaces", name="kube-system"))
        if live is None or not uid(live):
            raise Refused("the host answered without a kube-system namespace")
        return uid(live)

    def remaining(self):
        return self.deadline - self.clock()

    def wait(self, read, done, seconds):
        """Poll read until done holds or the bound ends; the last reading either way."""
        end = min(self.deadline, self.clock() + seconds)
        while True:
            value = read()
            if done(value) or self.clock() + POLL_SECONDS > end:
                return value
            self.sleep(POLL_SECONDS)


def answered_reason(error):
    if error.status in (401, 403):
        return ("the host refused the kubeconfig's credential (HTTP %d); if it was rotated or expired, continue with "
                "--authorize rebind=<kubeconfig Secret>" % error.status)
    return "the host answered HTTP %d%s" % (error.status, (" (" + error.reason + ")") if error.reason else "")


# The provider proof.

def proof(client, request, clock=time.time):
    """Every check of the provider proof, in order; a check that cannot be read is a failure naming it."""
    evidence = {"failures": [], "kubeSystemUID": "", "macPool": "unknown", "networks": [], "postcondition": False,
                "readWriteMany": False, "server": client.server, "storageClass": "", "uploadProxy": "", "version": ""}
    failures = evidence["failures"]

    def read(resource, what):
        try:
            return client.get(resource)
        except Unanswered:
            failures.append(what + " could not be read: the host did not answer")
        except Answered as error:
            failures.append(what + " could not be read: " + answered_reason(error))
        return False

    version = read("/version", "the API server version")
    if version is False:
        return evidence
    if version is None or not isinstance(version.get("gitVersion"), str):
        failures.append("the server named by the kubeconfig is not a Kubernetes API server")
        return evidence
    evidence["version"] = version["gitVersion"][:64]
    kubevirt = read("/apis/subresources.kubevirt.io/v1/version", "the KubeVirt version")
    if kubevirt is None:
        failures.append("the host cluster runs no OpenShift Virtualization: /apis/subresources.kubevirt.io/v1/version is absent")
    host = Host(client, request["namespace"], clock() + 600, clock)
    system = read(path("", "v1", "namespaces", name="kube-system"), "the kube-system namespace")
    if system:
        evidence["kubeSystemUID"] = uid(system)
    namespace = read(path("", "v1", "namespaces", name=request["namespace"]), "the namespace " + request["namespace"])
    if namespace is None:
        failures.append("the namespace %s does not exist; the operator creates it" % request["namespace"])
    config = read(path("cdi.kubevirt.io", "v1beta1", "cdiconfigs", name="config"), "the CDI configuration")
    if config:
        proxy = (config.get("status") or {}).get("uploadProxyURL") or ""
        if proxy:
            evidence["uploadProxy"] = proxy if "://" in proxy else "https://" + proxy
        else:
            failures.append("the CDI configuration publishes no upload proxy URL")
    ingress = read(path("", "v1", "configmaps", "openshift-config-managed", "default-ingress-cert"), "the ingress certificate authority")
    if ingress is None or (ingress and "-----BEGIN CERTIFICATE-----" not in ((ingress.get("data") or {}).get("ca-bundle.crt") or "")):
        failures.append("ConfigMap openshift-config-managed/default-ingress-cert publishes no ingress certificate authority")
    root_class(read, request, evidence)
    for access in request["access"]:
        allowed(client, host, access, failures)
    for network in request["networks"]:
        evidence["networks"].append(network_present(read, network, failures, client))
    evidence["macPool"] = mac_pool(client, request["macAddresses"])
    evidence["postcondition"] = not failures
    return evidence


def root_class(read, request, evidence):
    failures = evidence["failures"]
    chosen = request.get("storageClass") or ""
    if not chosen:
        classes = read(path("storage.k8s.io", "v1", "storageclasses"), "the storage classes")
        if not classes:
            return
        items = [item for item in classes.get("items") or [] if isinstance(item, dict)]
        for annotation in (DEFAULT_VIRT, DEFAULT_CLASS):
            named = [item["metadata"]["name"] for item in items if ((item.get("metadata") or {}).get("annotations") or {}).get(annotation) == "true"]
            if named:
                chosen = sorted(named)[0]
                break
        if not chosen:
            failures.append("the host declares no default virtualization or default storage class; set spec.kubevirt.storageClassRef")
            return
    evidence["storageClass"] = chosen
    profile = read(path("cdi.kubevirt.io", "v1beta1", "storageprofiles", name=chosen), "the StorageProfile of " + chosen)
    if profile is None:
        failures.append("the storage class %s has no StorageProfile" % chosen)
    if not profile:
        return
    sets = (profile.get("status") or {}).get("claimPropertySets") or []
    if not sets:
        failures.append("the StorageProfile of %s lists no claimPropertySets, so CDI cannot size a disk on it" % chosen)
        return
    evidence["readWriteMany"] = any("ReadWriteMany" in (entry.get("accessModes") or []) for entry in sets if isinstance(entry, dict))
    if not evidence["readWriteMany"]:
        failures.append("the storage class %s offers no ReadWriteMany claim, so a host drain cannot migrate a VirtualMachine on it" % chosen)


def allowed(client, host, access, failures):
    review = {"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
              "spec": {"resourceAttributes": {"namespace": host.namespace, "group": access.get("group", ""),
                                              "resource": access["resource"], "subresource": access.get("subresource", ""),
                                              "verb": access["verb"]}}}
    label = "%s %s%s" % (access["verb"], access["resource"], "/" + access["subresource"] if access.get("subresource") else "")
    try:
        status, body = client.request("POST", path("authorization.k8s.io", "v1", "selfsubjectaccessreviews"), review)
    except Unanswered:
        failures.append("the permission to %s could not be read: the host did not answer" % label)
        return
    if status not in (200, 201) or not isinstance(body, dict):
        failures.append("the permission to %s could not be read: the host answered HTTP %d" % (label, status))
    elif not (body.get("status") or {}).get("allowed"):
        failures.append("the kubeconfig's credential may not %s in namespace %s" % (label, host.namespace))


def network_present(read, network, failures, client):
    attachment = {"attachment": network["attachment"], "present": False}
    definition = read(path("k8s.cni.cncf.io", "v1", "network-attachment-definitions", network["namespace"], network["name"]),
                      "the NetworkAttachmentDefinition %s/%s" % (network["namespace"], network["name"]))
    if definition is None:
        failures.append("attachment %s: NetworkAttachmentDefinition %s/%s does not exist" % (network["attachment"], network["namespace"], network["name"]))
    if not definition:
        return attachment
    if network.get("derived"):
        try:
            derived = client.get(path("k8s.ovn.org", "v1", "clusteruserdefinednetworks", name=network["derived"]))
        except (Unanswered, Answered):
            derived = None
        conditions = ((derived or {}).get("status") or {}).get("conditions") or []
        created = [c for c in conditions if isinstance(c, dict) and c.get("type") == "NetworkCreated"]
        if created and created[0].get("status") != "True":
            failures.append("attachment %s: ClusterUserDefinedNetwork %s reports NetworkCreated %s" % (
                network["attachment"], network["derived"], str(created[0].get("status"))[:16]))
            return attachment
    attachment["present"] = True
    return attachment


def mac_pool(client, macs):
    """Where the pinned MACs fall against KubeMacPool's range, read where the credential may; never a failure."""
    try:
        config = client.get(path("", "v1", "configmaps", "openshift-cnv", "kubemacpool-mac-range-config"))
    except (Unanswered, Answered):
        return "unknown"
    data = (config or {}).get("data") or {}
    try:
        start, end = (int(data[key].replace(":", ""), 16) for key in ("RANGE_START", "RANGE_END"))
        pinned = [int(mac.replace(":", ""), 16) for mac in macs]
    except (KeyError, ValueError, AttributeError):
        return "unknown"
    if not pinned:
        return "outside"
    inside = [start <= value <= end for value in pinned]
    return "inside" if all(inside) else ("outside" if not any(inside) else "unknown")


# The VirtualMachine block.

def storage_class(request, context):
    return request.get("storageClass") or context["pinned"]["storageClass"]


def vm_body(request, context):
    """The one VirtualMachine this block creates, with its root DataVolume template."""
    vm, namespace = request["name"], request["namespace"]
    held = labels(request["identity"]["context"], request["identity"]["object"])

    def owner(kind, object_name):
        return mark(context["operation"], context["block"], kind, namespace, object_name)

    devices = {
        "disks": [{"name": "root", "bootOrder": 1, "disk": {"bus": "virtio"}}],
        "interfaces": [{"name": "nic%d" % index, "bridge": {}, "macAddress": iface["macAddress"]}
                       for index, iface in enumerate(request["interfaces"])],
    }
    if request.get("tpm") is not None:
        devices["tpm"] = {"persistent": bool(request["tpm"]["persistent"])}
    memory = "%dMi" % request["memoryMiB"]
    return {
        "apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine",
        "metadata": {"name": vm, "namespace": namespace, "labels": held, "annotations": {OWNER: owner("VirtualMachine", vm)}},
        "spec": {
            "runStrategy": "Manual",
            "dataVolumeTemplates": [{
                "metadata": {"name": request["rootVolume"], "labels": held,
                             "annotations": {OWNER: owner("DataVolume", request["rootVolume"]), BIND: "true"}},
                "spec": {"source": {"blank": {}},
                         "storage": {"resources": {"requests": {"storage": "%dGi" % request["diskGiB"]}},
                                     "storageClassName": storage_class(request, context)}},
            }],
            "template": {
                "metadata": {"labels": held},
                "spec": {
                    "domain": {"cpu": {"cores": request["cpu"], "sockets": 1, "threads": 1},
                               "memory": {"guest": memory}, "resources": {"requests": {"memory": memory}},
                               "firmware": {"bootloader": {"efi": {"secureBoot": False}}},
                               "devices": devices},
                    "evictionStrategy": "LiveMigrate",
                    "networks": [{"name": "nic%d" % index, "multus": {"networkName": iface["network"]}}
                                 for index, iface in enumerate(request["interfaces"])],
                    "volumes": [{"name": "root", "dataVolume": {"name": request["rootVolume"]}}],
                },
            },
        },
    }


def frozen_difference(request, context, live):
    """The first frozen field a live VirtualMachine differs from, ignoring the media volume the port adds."""
    from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_model import difference
    wanted = vm_body(request, context)["spec"]
    spec = json.loads(json.dumps(live.get("spec") or {}))
    template = (spec.get("template") or {}).get("spec") or {}
    devices = (template.get("domain") or {}).get("devices") or {}
    devices["disks"] = [disk for disk in devices.get("disks") or [] if disk.get("name") != MEDIA_VOLUME]
    template["volumes"] = [volume for volume in template.get("volumes") or [] if volume.get("name") != MEDIA_VOLUME]
    if request.get("tpm") is None and "tpm" in devices:
        return "spec.template.spec.domain.devices.tpm"
    for template_entry in wanted["dataVolumeTemplates"]:
        template_entry.pop("metadata")
    return difference(wanted, spec)


def receipt_for(receipts, kind, object_name):
    return next((r for r in receipts if r["kind"] == kind and r["name"] == object_name), None)


def keep(receipts, kind, object_name, live, basis):
    if receipt_for(receipts, kind, object_name) is None:
        receipts.append({"basis": basis, "kind": kind, "name": object_name, "uid": uid(live)})


def base_evidence(server):
    return {"absent": False, "answered": False, "foreign": False, "instance": "", "kubeSystemUID": "", "owned": False,
            "postcondition": False, "receipts": [], "server": server, "virtualMachine": "", "volumes": []}


def pinned(host, context):
    live = host.cluster_uid()
    expected = context["pinned"]["kubeSystemUID"]
    if expected and live != expected:
        raise Refused("the host cluster the kubeconfig reaches is not the one the provider proof recorded (kube-system UID differs)")
    return live


def read_machine(host, request, context, receipts):
    """One observation of the VirtualMachine and everything it owns; Unanswered leaves answered false."""
    evidence = base_evidence(host.client.server)
    vm = request["name"]
    try:
        evidence["kubeSystemUID"] = pinned(host, context)
        live = host.client.get(host.vm(vm))
        state = ownership("VirtualMachine", live, receipts, context)
        owners = {r["uid"] for r in receipts if r["uid"]}
        if state == "foreign":
            evidence["foreign"] = True
        elif state == "own":
            evidence["owned"], evidence["virtualMachine"] = True, vm
            keep(receipts, "VirtualMachine", vm, live, "mark-recovered")
            owners.add(uid(live))
        instance = host.client.get(host.vmi(vm))
        if instance is not None:
            evidence["instance"] = str((instance.get("status") or {}).get("phase") or "Pending")[:32]
        for volume in (request["rootVolume"], request["mediaVolume"]):
            held = host.client.get(host.dv(volume))
            mine = held is not None and (owned_by(held, owners) or ownership("DataVolume", held, receipts, context) == "own")
            if held is not None and not mine:
                evidence["foreign"] = True
            if mine:
                keep(receipts, "DataVolume", volume, held, "owner-referenced")
                owners.add(uid(held))
            evidence["volumes"].append({"kind": "DataVolume", "name": volume, "present": bool(mine)})
        evidence["volumes"].extend(claims(host, owners))
        evidence["answered"] = True
    except Unanswered:
        evidence["answered"] = False
    evidence["receipts"] = sorted(receipts, key=lambda r: (r["kind"], r["name"]))
    return evidence


def claims(host, owners):
    """Every claim in the namespace an owner reference ties to the VirtualMachine or one of its DataVolumes."""
    found, token = [], ""
    for ignored_page in range(MAX_CLAIM_PAGES):
        listing = host.client.get(host.pvc(), "limit=500" + ("&continue=" + token if token else ""))
        if listing is None:
            raise Unanswered()
        for item in listing.get("items") or []:
            if owned_by(item, owners):
                found.append({"kind": "PersistentVolumeClaim", "name": str(item["metadata"]["name"])[:253], "present": True})
        token = (listing.get("metadata") or {}).get("continue") or ""
        if not token:
            return sorted(found, key=lambda v: v["name"])
    raise Unanswered()


def settled(evidence, request):
    """Fill absent and postcondition for an observation that answered."""
    present = [v for v in evidence["volumes"] if v["present"]]
    if evidence["answered"] and not evidence["owned"] and not evidence["foreign"] and not present and not evidence["instance"]:
        evidence["absent"], evidence["postcondition"] = True, True
    elif evidence["answered"] and evidence["owned"] and not evidence["foreign"]:
        root = any(v["kind"] == "DataVolume" and v["name"] == request["rootVolume"] and v["present"] for v in evidence["volumes"])
        evidence["postcondition"] = root and receipt_for(evidence["receipts"], "VirtualMachine", request["name"]) is not None
    return evidence


def observe(host, request, context):
    return settled(read_machine(host, request, context, [dict(r) for r in context["pinned"]["receipts"]]), request)


def apply(host, request, context):
    """Create the VirtualMachine once, adopt it after a lost reply, and wait for its root disk; (changed, evidence)."""
    receipts = [dict(r) for r in context["pinned"]["receipts"]]
    vm, changed = request["name"], False
    pinned(host, context)
    live = host.client.get(host.vm(vm))
    state = ownership("VirtualMachine", live, receipts, context)
    if state == "foreign":
        raise Refused("VirtualMachine %s/%s exists and is not this context's" % (request["namespace"], vm))
    if state == "absent":
        try:
            status, created = host.client.request("POST", host.vm(), vm_body(request, context))
            if status in (200, 201) and uid(created):
                receipts.append({"basis": "created", "kind": "VirtualMachine", "name": vm, "uid": uid(created)})
                changed = True
            elif status != 409:
                raise Answered(status, created)
        except Unanswered:
            pass
        live = host.client.get(host.vm(vm))
        state = ownership("VirtualMachine", live, receipts, context)
        if state == "absent":
            raise Refused("creating VirtualMachine %s/%s was not answered and the host holds none" % (request["namespace"], vm))
        if state == "foreign":
            raise Refused("VirtualMachine %s/%s exists and is not this context's" % (request["namespace"], vm))
        if receipt_for(receipts, "VirtualMachine", vm) is None:
            receipts.append({"basis": "mark-recovered", "kind": "VirtualMachine", "name": vm, "uid": uid(live)})
            changed = True
    drift = frozen_difference(request, context, live)
    if drift:
        raise Refused("VirtualMachine %s/%s differs from its frozen request at %s, which this arm never rewrites; destroy and apply again"
                      % (request["namespace"], vm, drift))
    host.wait(lambda: host.client.get(host.dv(request["rootVolume"])),
              lambda held: held is not None and ((held.get("status") or {}).get("phase") == "Succeeded"), VOLUME_SECONDS)
    evidence = settled(read_machine(host, request, dict(context, pinned=dict(context["pinned"], receipts=receipts)), receipts), request)
    return changed, evidence


def destroy(host, request, context):
    """Delete the stopped VirtualMachine and its upload media by UID, then prove all it owned absent; (changed, evidence)."""
    receipts = [dict(r) for r in context["pinned"]["receipts"]]
    vm, changed = request["name"], False
    pinned(host, context)
    live = host.client.get(host.vm(vm))
    state = ownership("VirtualMachine", live, receipts, context)
    if state == "foreign":
        raise Refused("VirtualMachine %s/%s exists and is not this context's, so nothing was deleted" % (request["namespace"], vm))
    if state == "own" and host.client.get(host.vmi(vm)) is not None:
        raise Refused("VirtualMachine %s/%s is running; stop it before its removal" % (request["namespace"], vm))
    # Record every DataVolume's UID before anything is deleted, so the claims
    # each one owns are still found by their owner reference once it is gone.
    if read_machine(host, request, dict(context, pinned=dict(context["pinned"], receipts=receipts)), receipts)["foreign"]:
        raise Refused("an object of VirtualMachine %s/%s's names is not this context's, so nothing was deleted" % (request["namespace"], vm))
    media = host.client.get(host.dv(request["mediaVolume"]))
    if media is not None and state == "own" and owned_by(media, {uid(live)}):
        changed = delete(host, host.dv(request["mediaVolume"]), uid(media)) or changed
    if state == "own":
        keep(receipts, "VirtualMachine", vm, live, "mark-recovered")
        changed = delete(host, host.vm(vm), uid(live)) or changed
    scoped = dict(context, pinned=dict(context["pinned"], receipts=receipts))
    evidence = host.wait(lambda: settled(read_machine(host, request, scoped, receipts), request),
                         lambda observed: observed["absent"], max(1, host.remaining() - 30))
    return changed, evidence


def delete(host, resource, object_uid):
    options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": object_uid}, "propagationPolicy": "Foreground"}
    status, body = host.client.request("DELETE", resource, options)
    if status in (200, 202):
        return True
    if status in (404, 409):
        return False
    raise Answered(status, body)


# The machine port. Each entry point addresses one VirtualMachine by the
# target's endpoint and acts only on one whose labels derive its name.

class PortRefused(Refused):
    """A port refusal the consumer names to its runner by code."""

    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def endpoint(value):
    """The namespace and VirtualMachine one target endpoint names."""
    if not isinstance(value, str) or not value.startswith("kubevirt:") or value.count("/") != 1:
        raise Refused("the machine port endpoint is not kubevirt:<namespace>/<name>")
    namespace, vm = value[len("kubevirt:"):].split("/")
    if not namespace or not vm:
        raise Refused("the machine port endpoint is not kubevirt:<namespace>/<name>")
    return namespace, vm


def port_vm(host, vm):
    """The pinned VirtualMachine, re-proved before any effect: the host must be
    the cluster and the object the VirtualMachine its machine block recorded."""
    cluster, recorded = host.pin.get("cluster", ""), host.pin.get("vm", "")
    if not cluster or not recorded:
        raise PortRefused("identity-mismatch", "VirtualMachine %s/%s has no cluster and UID its machine block recorded, so nothing was done; "
                          "apply the Machine so its block records them" % (host.namespace, vm))
    if host.cluster_uid() != cluster:
        raise PortRefused("identity-mismatch", "the host cluster the kubeconfig reaches is not the one VirtualMachine %s/%s was realized on "
                          "(kube-system UID differs), so nothing was done" % (host.namespace, vm))
    live = host.client.get(host.vm(vm))
    if live is None:
        raise Refused("VirtualMachine %s/%s does not exist; apply the Machine first" % (host.namespace, vm))
    if not derives(live, vm) or uid(live) != recorded:
        raise PortRefused("identity-mismatch", "VirtualMachine %s/%s is not the one this context's machine block recorded, so nothing was done"
                          % (host.namespace, vm))
    return live


def media_of(host, live, volume):
    """The upload DataVolume, own only when an owner reference names the pinned VirtualMachine."""
    held = host.client.get(host.dv(volume))
    if held is not None and not (derives(held, live["metadata"]["name"]) and owned_by(held, {uid(live)})):
        raise Refused("DataVolume %s/%s is not this context's, so it was not touched" % (host.namespace, volume))
    return held


def instance_phase(host, vm):
    instance = host.client.get(host.vmi(vm))
    return "" if instance is None else str((instance.get("status") or {}).get("phase") or "Pending")


def power_of(phase):
    return "Off" if phase in ("", "Succeeded", "Failed") else "On"


def presented(live):
    for volume in (((live.get("spec") or {}).get("template") or {}).get("spec") or {}).get("volumes") or []:
        if volume.get("name") == MEDIA_VOLUME:
            return (volume.get("dataVolume") or {}).get("name") or ""
    return ""


def state_read(host, vm, media_identity="", media=True):
    live = port_vm(host, vm)
    phase = instance_phase(host, vm)
    result = {"power": power_of(phase), "power_state": power_of(phase).lower(), "media": "", "media_state": "unknown"}
    if not media:
        return result
    volume = presented(live)
    result["media"] = volume
    if not volume:
        result["media_state"] = "none"
        return result
    held = host.client.get(host.dv(volume))
    identity = ((held or {}).get("metadata") or {}).get("annotations", {}).get(MEDIA, "")
    mine = held is not None and derives(held, vm) and owned_by(held, {uid(live)})
    result["media_state"] = "own" if mine and media_identity and identity == media_identity else "foreign"
    return result


def pre_boot(host, vm, require_tpm=False, expected=(), firmware=None):
    if firmware:
        raise Refused("firmware settings are applied only to a bare-metal Machine, and this machine's substrate created it; remove the firmware declaration")
    live = port_vm(host, vm)
    devices = (((live.get("spec") or {}).get("template") or {}).get("spec") or {}).get("domain", {}).get("devices") or {}
    macs = sorted(str(iface.get("macAddress", "")).lower() for iface in devices.get("interfaces") or [])
    if expected and macs != sorted(mac.lower() for mac in expected):
        raise Refused("VirtualMachine %s/%s does not present the interfaces its realization derived" % (host.namespace, vm))
    if require_tpm and not (devices.get("tpm") or {}).get("persistent"):
        raise PortRefused("tpm-absent", "VirtualMachine %s/%s presents no persistent vTPM" % (host.namespace, vm))
    if instance_phase(host, vm):
        raise PortRefused("machine-running", "this machine is running, and an installation erases its disk; stop it explicitly before installing over it")
    return {"power": "Off", "power_state": "off"}


def change_power(host, vm, verb, force=False):
    """Start or stop through the subresource and poll the instance there; a stop the guest did not finish reports running."""
    port_vm(host, vm)
    if verb in ("off", "restart"):
        phase = instance_phase(host, vm)
        if phase:
            body = {"gracePeriod": 0} if force else {}
            status, answer = host.client.request("PUT", host.subresource(vm, "stop"), body)
            if status not in (200, 202, 409):
                raise Answered(status, answer)
            phase = host.wait(lambda: instance_phase(host, vm), lambda value: not value, POWER_SECONDS)
            if phase:
                raise Refused("VirtualMachine %s/%s is still running %d seconds after it was asked to stop; stop it with --force"
                              % (host.namespace, vm, POWER_SECONDS))
    if verb in ("on", "restart"):
        if not instance_phase(host, vm):
            status, answer = host.client.request("PUT", host.subresource(vm, "start"), {})
            if status not in (200, 202, 409):
                raise Answered(status, answer)
        phase = host.wait(lambda: instance_phase(host, vm), lambda value: value == "Running", POWER_SECONDS)
        if phase != "Running":
            raise Refused("VirtualMachine %s/%s did not run within %d seconds of its start" % (host.namespace, vm, POWER_SECONDS))
    phase = instance_phase(host, vm)
    return {"power": power_of(phase), "power_state": power_of(phase).lower()}


def patch_vm(host, live, operations):
    tested = [{"op": "test", "path": "/metadata/uid", "value": uid(live)}] + operations
    status, answer = host.client.request("PATCH", host.vm(live["metadata"]["name"]), tested, content_type="application/json-patch+json")
    if status != 200:
        raise Answered(status, answer)


def eject(host, vm):
    """Remove the media volume from the stopped VirtualMachine and delete the upload DataVolume by its UID."""
    live = port_vm(host, vm)
    volume = vm + "-media"
    held = media_of(host, live, volume)
    template = ((live.get("spec") or {}).get("template") or {}).get("spec") or {}
    operations = []
    for index, disk in enumerate(((template.get("domain") or {}).get("devices") or {}).get("disks") or []):
        if disk.get("name") == MEDIA_VOLUME:
            operations += [{"op": "test", "path": "/spec/template/spec/domain/devices/disks/%d/name" % index, "value": MEDIA_VOLUME},
                           {"op": "remove", "path": "/spec/template/spec/domain/devices/disks/%d" % index}]
    for index, entry in enumerate(template.get("volumes") or []):
        if entry.get("name") == MEDIA_VOLUME:
            operations += [{"op": "test", "path": "/spec/template/spec/volumes/%d/name" % index, "value": MEDIA_VOLUME},
                           {"op": "remove", "path": "/spec/template/spec/volumes/%d" % index}]
    if operations:
        patch_vm(host, live, operations)
    if held is not None:
        delete(host, host.dv(volume), uid(held))
        if host.wait(lambda: host.client.get(host.dv(volume)), lambda value: value is None, VOLUME_SECONDS) is not None:
            raise Refused("DataVolume %s/%s was not deleted within %d seconds" % (host.namespace, volume, VOLUME_SECONDS))
    return {}


def upload_volume(host, vm, media_identity, size_bytes):
    """Create, or reuse, the upload DataVolume once the machine is off; (reuse, the DataVolume)."""
    live = port_vm(host, vm)
    if instance_phase(host, vm):
        raise PortRefused("machine-running", "this machine is running; media is written only to a stopped machine")
    volume = vm + "-media"
    held = media_of(host, live, volume)
    if held is not None:
        identity = ((held.get("metadata") or {}).get("annotations") or {}).get(MEDIA, "")
        if identity == media_identity and (held.get("status") or {}).get("phase") == "Succeeded":
            return True, live
        eject(host, vm)
        live = port_vm(host, vm)
    metadata = (live.get("metadata") or {}).get("labels") or {}
    body = {"apiVersion": "cdi.kubevirt.io/v1beta1", "kind": "DataVolume",
            "metadata": {"name": volume, "namespace": host.namespace,
                         "labels": labels(metadata[CONTEXT], metadata[MACHINE]),
                         "annotations": {MEDIA: media_identity, BIND: "true"},
                         "ownerReferences": [{"apiVersion": "kubevirt.io/v1", "kind": "VirtualMachine", "name": vm, "uid": uid(live)}]},
            "spec": {"source": {"upload": {}},
                     "storage": {"resources": {"requests": {"storage": "%dGi" % (-(-size_bytes // GIB) + 1)}}}}}
    status, answer = host.client.request("POST", host.dv(), body)
    if status not in (200, 201):
        raise Answered(status, answer)
    ready = host.wait(lambda: host.client.get(host.dv(volume)),
                      lambda value: value is not None and (value.get("status") or {}).get("phase") == "UploadReady", VOLUME_SECONDS)
    if ready is None or (ready.get("status") or {}).get("phase") != "UploadReady":
        raise Refused("DataVolume %s/%s was not ready for upload within %d seconds" % (host.namespace, volume, VOLUME_SECONDS))
    return False, live


def upload_trust(host):
    """The upload proxy URL and the ingress certificate authority it is verified against, read through the verified API."""
    config = host.client.get(path("cdi.kubevirt.io", "v1beta1", "cdiconfigs", name="config"))
    proxy = ((config or {}).get("status") or {}).get("uploadProxyURL") or ""
    ingress = host.client.get(path("", "v1", "configmaps", "openshift-config-managed", "default-ingress-cert"))
    authority = ((ingress or {}).get("data") or {}).get("ca-bundle.crt") or ""
    if not proxy or "-----BEGIN CERTIFICATE-----" not in authority:
        raise Refused("the host publishes no upload proxy or no ingress certificate authority")
    return (proxy if "://" in proxy else "https://" + proxy), authority


def attach(host, vm):
    """Present the uploaded volume to the stopped VirtualMachine as a SATA cdrom second in its boot order."""
    live = port_vm(host, vm)
    volume = vm + "-media"
    held = media_of(host, live, volume)
    if held is None or (held.get("status") or {}).get("phase") != "Succeeded":
        raise Refused("DataVolume %s/%s did not complete its upload" % (host.namespace, volume))
    if presented(live) == volume:
        return {}
    patch_vm(host, live, [
        {"op": "add", "path": "/spec/template/spec/domain/devices/disks/-",
         "value": {"name": MEDIA_VOLUME, "bootOrder": 2, "cdrom": {"bus": "sata"}}},
        {"op": "add", "path": "/spec/template/spec/volumes/-", "value": {"name": MEDIA_VOLUME, "dataVolume": {"name": volume}}},
    ])
    return {}
