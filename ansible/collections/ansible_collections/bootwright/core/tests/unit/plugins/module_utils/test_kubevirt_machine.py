"""The KubeVirt arm over a fake host API: ownership, adoption, removal proof and the port."""

from __future__ import annotations

import base64
import copy
import json
import re

import pytest
from ansible.module_utils.common.yaml import yaml_load

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm

REGISTERED = "2026-10-28T10:00:00Z"
LATER = "2026-10-28T10:00:30Z"
EARLIER = "2026-10-27T10:00:00Z"
CA = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
VM = arm.name("lab", "master-0")


class FakeAPI:
    """An in-memory host API: objects by path, KubeVirt's template DataVolume, and garbage collection."""

    def __init__(self):
        self.objects, self.requests, self.counter = {}, [], 0
        self.unanswered, self.lose_create_reply, self.status = set(), False, None
        self.put("/version", {"gitVersion": "v1.34.2"})
        self.put("/apis/subresources.kubevirt.io/v1/version", {"gitVersion": "v1.6.0"})
        self.put("/api/v1/namespaces/kube-system", self.meta("kube-system", uid="cluster-1"))
        self.put("/api/v1/namespaces/guests", self.meta("guests"))

    def meta(self, name, uid=None, **extra):
        self.counter += 1
        metadata = {"name": name, "uid": uid or "uid-%d" % self.counter, "creationTimestamp": LATER}
        metadata.update(extra)
        return {"metadata": metadata}

    def put(self, path, body):
        self.objects[path] = body

    def __call__(self, method, target, headers, payload):
        path, ignored_mark, query = target.partition("?")
        self.requests.append((method, path))
        if self.status is not None:
            return self.status, b"{}"
        if path in self.unanswered or "*" in self.unanswered:
            raise kapi.Unanswered()
        body = json.loads(payload) if payload else None
        if method == "GET":
            if path.endswith("/persistentvolumeclaims"):
                items = [o for p, o in self.objects.items() if p.startswith(path + "/")]
                return 200, json.dumps({"items": items, "metadata": {}}).encode()
            if path in self.objects:
                return 200, json.dumps(self.objects[path]).encode()
            return 404, b'{"kind":"Status","reason":"NotFound"}'
        if method == "POST" and path.endswith("selfsubjectaccessreviews"):
            return 201, json.dumps({"status": {"allowed": True}}).encode()
        if method == "POST":
            return self.create(path, body)
        if method == "DELETE":
            return self.delete(path, body)
        if method == "PATCH":
            return self.patch(path, body)
        if method == "PUT" and path.endswith("/start"):
            vmi = path.replace("virtualmachines", "virtualmachineinstances").replace("/apis/subresources.kubevirt.io/", "/apis/kubevirt.io/")[:-len("/start")]
            self.put(vmi, {"metadata": {"name": VM}, "status": {"phase": "Running"}})
            return 202, b""
        if method == "PUT" and path.endswith("/stop"):
            vmi = path.replace("virtualmachines", "virtualmachineinstances").replace("/apis/subresources.kubevirt.io/", "/apis/kubevirt.io/")[:-len("/stop")]
            self.objects.pop(vmi, None)
            return 202, b""
        return 405, b""

    def create(self, path, body):
        target = path + "/" + body["metadata"]["name"]
        if target in self.objects:
            return 409, b'{"kind":"Status","reason":"AlreadyExists"}'
        created = copy.deepcopy(body)
        created["metadata"].update(self.meta(body["metadata"]["name"])["metadata"])
        self.put(target, created)
        if body["kind"] == "VirtualMachine":
            for template in body["spec"].get("dataVolumeTemplates", []):
                dv = copy.deepcopy(template)
                dv["metadata"].update(self.meta(template["metadata"]["name"])["metadata"])
                dv["metadata"]["ownerReferences"] = [{"controller": True, "uid": created["metadata"]["uid"]}]
                dv["status"] = {"phase": "Succeeded"}
                self.put(path.replace("kubevirt.io/v1", "cdi.kubevirt.io/v1beta1").replace("virtualmachines", "datavolumes") + "/" + dv["metadata"]["name"], dv)
                self.claim(dv)
        if body["kind"] == "DataVolume":
            created["status"] = {"phase": "UploadReady"}
            self.claim(created)
        if self.lose_create_reply:
            raise kapi.Unanswered()
        return 201, json.dumps(created).encode()

    def claim(self, dv):
        pvc = self.meta(dv["metadata"]["name"])
        pvc["metadata"]["ownerReferences"] = [{"controller": True, "uid": dv["metadata"]["uid"]}]
        self.put("/api/v1/namespaces/guests/persistentvolumeclaims/" + dv["metadata"]["name"], pvc)

    def delete(self, path, body):
        held = self.objects.get(path)
        if held is None:
            return 404, b""
        if body["preconditions"]["uid"] != held["metadata"]["uid"]:
            return 409, b'{"reason":"Conflict"}'
        self.collect(held["metadata"]["uid"])
        del self.objects[path]
        return 200, b"{}"

    def collect(self, uid):
        for path, held in list(self.objects.items()):
            if any(ref.get("uid") == uid for ref in held.get("metadata", {}).get("ownerReferences", [])):
                self.objects.pop(path, None)
                self.collect(held["metadata"]["uid"])

    def patch(self, path, operations):
        held = copy.deepcopy(self.objects[path])
        for operation in operations:
            parts = operation["path"].strip("/").split("/")
            parent = held
            for part in parts[:-1]:
                parent = parent[int(part)] if isinstance(parent, list) else parent[part]
            leaf = parts[-1]
            if operation["op"] == "test":
                value = parent[int(leaf)] if isinstance(parent, list) else parent.get(leaf)
                if value != operation["value"]:
                    return 422, b"{}"
            elif operation["op"] == "add":
                parent.append(operation["value"]) if leaf == "-" else parent.insert(int(leaf), operation["value"])
            elif operation["op"] == "remove":
                del parent[int(leaf)]
        self.objects[path] = held
        return 200, json.dumps(held).encode()


def client(api):
    return kapi.Client({"server": "https://api.host.example.test:6443", "ca": CA, "token": "t"}, transport=api)


def request(**extra):
    value = {
        "cpu": 4, "diskGiB": 120, "identity": {"block": "machine-master-0", "context": "lab", "object": "master-0"},
        "interfaces": [{"macAddress": "52:54:00:25:10:e0", "name": "enp1s0", "network": "guest-l2"}],
        "kubeconfigRef": "host-kubeconfig", "mediaVolume": VM + "-media", "memoryMiB": 16384, "name": VM, "namespace": "guests",
        "placement": {"connection": "local", "machine": "controller"}, "provider": "lab-kubevirt", "rootVolume": VM + "-root",
        "version": "machine-kubevirt-v1",
    }
    value.update(extra)
    return value


def context(receipts=(), uid="cluster-1", operation="op-1"):
    return {"operation": operation, "block": "machine-master-0", "registered": REGISTERED,
            "pinned": {"kubeSystemUID": uid, "server": "https://api.host.example.test:6443", "storageClass": "guest-block", "receipts": list(receipts)}}


def host(api, seconds=60, pin=None):
    clock = {"now": 0.0}
    return arm.Host(client(api), "guests", seconds, clock=lambda: clock["now"], sleep=lambda s: clock.update(now=clock["now"] + s), pin=pin)


def pinned(api):
    """A port host pinned to the cluster and VirtualMachine the block recorded."""
    return host(api, pin={"cluster": "cluster-1", "vm": api.objects[VM_PATH]["metadata"]["uid"]})


VM_PATH = "/apis/kubevirt.io/v1/namespaces/guests/virtualmachines/" + VM


def test_the_collection_derives_the_name_go_derives():
    assert VM == "master-0-538954ed"
    assert len(arm.name("lab", "a" * 63) + "-media") <= 63


def test_apply_creates_the_frozen_virtual_machine_once_and_records_its_uid():
    api = FakeAPI()
    changed, evidence = arm.apply(host(api), request(), context())
    vm = api.objects[VM_PATH]
    spec = vm["spec"]["template"]["spec"]
    assert changed and evidence["owned"] and evidence["postcondition"] and evidence["kubeSystemUID"] == "cluster-1"
    assert vm["spec"]["runStrategy"] == "Manual" and spec["evictionStrategy"] == "LiveMigrate"
    assert spec["domain"]["firmware"] == {"bootloader": {"efi": {"secureBoot": False}}} and "tpm" not in spec["domain"]["devices"]
    assert spec["domain"]["devices"]["interfaces"] == [{"name": "nic0", "bridge": {}, "macAddress": "52:54:00:25:10:e0"}]
    assert spec["domain"]["devices"]["disks"][0] == {"name": "root", "bootOrder": 1, "disk": {"bus": "virtio"}}
    template = vm["spec"]["dataVolumeTemplates"][0]
    assert template["metadata"]["annotations"][arm.BIND] == "true" and "accessModes" not in json.dumps(template["spec"])
    assert vm["metadata"]["labels"] == arm.labels("lab", "master-0") and arm.derives(vm, VM)
    assert vm["metadata"]["annotations"][arm.OWNER] == arm.mark("op-1", "machine-master-0", "VirtualMachine", "guests", VM)
    assert {"basis": "created", "kind": "VirtualMachine", "name": VM, "uid": vm["metadata"]["uid"]} in evidence["receipts"]
    again, replay = arm.apply(host(api), request(), context(evidence["receipts"]))
    assert not again and replay["owned"] and [m for m, p in api.requests if m == "POST" and "virtualmachines" in p] == ["POST"]


def test_a_persistent_vtpm_is_rendered_only_when_the_profile_declares_one():
    api = FakeAPI()
    arm.apply(host(api), request(tpm={"persistent": True}), context())
    assert api.objects[VM_PATH]["spec"]["template"]["spec"]["domain"]["devices"]["tpm"] == {"persistent": True}


def test_a_lost_create_reply_is_adopted_only_by_this_operations_mark():
    api = FakeAPI()
    api.lose_create_reply = True
    changed, evidence = arm.apply(host(api), request(), context())
    assert changed and evidence["owned"]
    assert [r["basis"] for r in evidence["receipts"] if r["kind"] == "VirtualMachine"] == ["mark-recovered"]
    other = FakeAPI()
    body = arm.vm_body(request(), context(operation="op-0"))
    other.create("/apis/kubevirt.io/v1/namespaces/guests/virtualmachines", body)
    with pytest.raises(kapi.Refused, match="is not this context's"):
        arm.apply(host(other), request(), context())


def test_a_same_labelled_object_this_block_did_not_create_is_foreign():
    api = FakeAPI()
    body = arm.vm_body(request(), context())
    body["metadata"]["annotations"] = {}
    api.create("/apis/kubevirt.io/v1/namespaces/guests/virtualmachines", body)
    assert arm.derives(api.objects[VM_PATH], VM)
    with pytest.raises(kapi.Refused, match="is not this context's"):
        arm.apply(host(api), request(), context())
    with pytest.raises(kapi.Refused, match="nothing was deleted"):
        arm.destroy(host(api), request(), context())
    stale = FakeAPI()
    stale.create("/apis/kubevirt.io/v1/namespaces/guests/virtualmachines", arm.vm_body(request(), context()))
    stale.objects[VM_PATH]["metadata"]["creationTimestamp"] = EARLIER
    assert arm.observe(host(stale), request(), context())["foreign"]
    receipted = FakeAPI()
    receipted.create("/apis/kubevirt.io/v1/namespaces/guests/virtualmachines", arm.vm_body(request(), context()))
    other_uid = [{"basis": "created", "kind": "VirtualMachine", "name": VM, "uid": "another"}]
    assert arm.observe(host(receipted), request(), context(other_uid))["foreign"]


def test_a_kubeconfig_that_reaches_another_cluster_never_acts_there():
    api = FakeAPI()
    with pytest.raises(kapi.Refused, match="kube-system UID differs"):
        arm.apply(host(api), request(), context(uid="cluster-0"))
    assert not [m for m, ignored_path in api.requests if m != "GET"]


def test_a_rotated_or_expired_credential_names_the_rebind():
    api = FakeAPI()
    api.status = 401
    with pytest.raises(kapi.Answered) as refused:
        arm.apply(host(api), request(), context())
    assert "--authorize rebind=" in arm.answered_reason(refused.value)


def test_an_unreachable_host_is_unknown_never_absent():
    api = FakeAPI()
    api.unanswered.add("*")
    evidence = arm.observe(host(api), request(), context())
    assert not evidence["answered"] and not evidence["absent"] and not evidence["postcondition"]
    nothing = arm.observe(host(FakeAPI()), request(), context())
    assert nothing["answered"] and nothing["absent"]


def test_removal_proves_every_owned_object_and_claim_absent():
    api = FakeAPI()
    ignored_changed, applied = arm.apply(host(api), request(), context())
    media = {"name": VM + "-media", "labels": arm.labels("lab", "master-0"),
             "ownerReferences": [{"uid": api.objects[VM_PATH]["metadata"]["uid"]}]}
    api.create("/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes",
               {"apiVersion": "cdi.kubevirt.io/v1beta1", "kind": "DataVolume", "metadata": media})
    api.put("/api/v1/namespaces/guests/persistentvolumeclaims/persistent-state-for-" + VM + "-x1",
            {"metadata": {"name": "persistent-state-for-" + VM + "-x1", "uid": "s1", "ownerReferences": [{"uid": api.objects[VM_PATH]["metadata"]["uid"]}]}})
    changed, evidence = arm.destroy(host(api), request(), context(applied["receipts"]))
    assert changed and evidence["absent"] and evidence["postcondition"] and not [v for v in evidence["volumes"] if v["present"]]
    deletes = [(m, p) for m, p in api.requests if m == "DELETE"]
    assert deletes[0][1].endswith("/datavolumes/" + VM + "-media") and deletes[1][1] == VM_PATH
    stuck = FakeAPI()
    ignored_changed, applied = arm.apply(host(stuck), request(), context())
    stuck.collect = lambda uid: None
    ignored_changed, evidence = arm.destroy(host(stuck), request(), context(applied["receipts"]))
    assert not evidence["absent"] and [v["name"] for v in evidence["volumes"] if v["present"]] == [VM + "-root", VM + "-root"]


def test_removal_refuses_a_running_machine():
    api = FakeAPI()
    ignored_changed, applied = arm.apply(host(api), request(), context())
    api.put("/apis/kubevirt.io/v1/namespaces/guests/virtualmachineinstances/" + VM, {"status": {"phase": "Running"}})
    with pytest.raises(kapi.Refused, match="is running"):
        arm.destroy(host(api), request(), context(applied["receipts"]))
    assert VM_PATH in api.objects


def test_the_port_reads_powers_and_ejects_only_a_machine_its_labels_derive():
    api = FakeAPI()
    arm.apply(host(api), request(), context())
    machine = pinned(api)
    assert arm.state_read(machine, VM)["power_state"] == "off" and arm.state_read(machine, VM)["media_state"] == "none"
    assert arm.pre_boot(machine, VM, expected=["52:54:00:25:10:E0"])["power"] == "Off"
    with pytest.raises(kapi.Refused, match="interfaces"):
        arm.pre_boot(machine, VM, expected=["52:54:00:00:00:01"])
    with pytest.raises(arm.PortRefused) as refused:
        arm.pre_boot(machine, VM, require_tpm=True)
    assert refused.value.code == "tpm-absent"
    assert arm.change_power(machine, VM, "on")["power"] == "On"
    with pytest.raises(arm.PortRefused) as running:
        arm.pre_boot(machine, VM)
    assert running.value.code == "machine-running"
    assert arm.change_power(machine, VM, "off", force=True)["power"] == "Off"
    stop = [p for m, p in api.requests if m == "PUT" and p.endswith("/stop")]
    assert stop
    api.objects[VM_PATH]["metadata"]["labels"][arm.MACHINE] = "other"
    with pytest.raises(arm.PortRefused, match="not the one this context's machine block recorded"):
        arm.state_read(machine, VM)


def test_media_is_uploaded_into_a_stopped_machine_and_ejected_by_uid():
    api = FakeAPI()
    arm.apply(host(api), request(), context())
    machine = pinned(api)
    reuse, ignored_value = arm.upload_volume(machine, VM, "digest-1", 3 * arm.GIB + 1)
    volume = api.objects["/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + VM + "-media"]
    assert not reuse and volume["spec"]["storage"]["resources"]["requests"]["storage"] == "5Gi"
    assert volume["metadata"]["annotations"][arm.MEDIA] == "digest-1" and arm.derives(volume, VM)
    volume["status"]["phase"] = "Succeeded"
    arm.attach(machine, VM)
    template = api.objects[VM_PATH]["spec"]["template"]["spec"]
    assert {"name": "media", "bootOrder": 2, "cdrom": {"bus": "sata"}} in template["domain"]["devices"]["disks"]
    assert arm.state_read(machine, VM, "digest-1")["media_state"] == "own"
    assert arm.state_read(machine, VM, "digest-2")["media_state"] == "foreign"
    assert arm.upload_volume(machine, VM, "digest-1", 1)[0]
    assert arm.frozen_difference(request(), context(), api.objects[VM_PATH]) == ""
    arm.eject(machine, VM)
    assert "/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + VM + "-media" not in api.objects
    assert arm.state_read(machine, VM)["media_state"] == "none"
    arm.change_power(machine, VM, "on")
    with pytest.raises(arm.PortRefused):
        arm.upload_volume(machine, VM, "digest-3", 1)


def proof_request(**extra):
    value = {"access": [{"group": "kubevirt.io", "resource": "virtualmachines", "verb": "create"}], "namespace": "guests",
             "networks": [{"attachment": "lab-guests", "name": "guest-l2", "namespace": "guests"}],
             "macAddresses": ["52:54:00:25:10:e0"], "storageClass": ""}
    value.update(extra)
    return value


def proved_host():
    api = FakeAPI()
    api.put("/apis/cdi.kubevirt.io/v1beta1/cdiconfigs/config", {"status": {"uploadProxyURL": "cdi-uploadproxy.apps.host.example.test"}})
    api.put("/api/v1/namespaces/openshift-config-managed/configmaps/default-ingress-cert", {"data": {"ca-bundle.crt": CA}})
    api.put("/apis/storage.k8s.io/v1/storageclasses", {"items": [
        {"metadata": {"name": "plain", "annotations": {arm.DEFAULT_CLASS: "true"}}},
        {"metadata": {"name": "virt", "annotations": {arm.DEFAULT_VIRT: "true"}}}]})
    api.put("/apis/cdi.kubevirt.io/v1beta1/storageprofiles/virt", {"status": {"claimPropertySets": [
        {"accessModes": ["ReadWriteMany"], "volumeMode": "Block"}]}})
    api.put("/apis/k8s.cni.cncf.io/v1/namespaces/guests/network-attachment-definitions/guest-l2", {"metadata": {"name": "guest-l2"}})
    return api


def test_the_provider_proof_holds_only_when_every_check_reads():
    api = proved_host()
    evidence = arm.proof(client(api), proof_request())
    assert evidence["postcondition"] and evidence["storageClass"] == "virt" and evidence["readWriteMany"]
    assert evidence["uploadProxy"] == "https://cdi-uploadproxy.apps.host.example.test" and evidence["kubeSystemUID"] == "cluster-1"
    assert evidence["networks"] == [{"attachment": "lab-guests", "present": True}] and evidence["macPool"] == "unknown"
    rwo = proved_host()
    rwo.put("/apis/cdi.kubevirt.io/v1beta1/storageprofiles/virt", {"status": {"claimPropertySets": [{"accessModes": ["ReadWriteOnce"]}]}})
    assert re.search("offers no ReadWriteMany", arm.proof(client(rwo), proof_request())["failures"][0])
    bare = proved_host()
    del bare.objects["/apis/subresources.kubevirt.io/v1/version"]
    assert "no OpenShift Virtualization" in arm.proof(client(bare), proof_request())["failures"][0]
    missing = proved_host()
    del missing.objects["/api/v1/namespaces/guests"]
    del missing.objects["/apis/k8s.cni.cncf.io/v1/namespaces/guests/network-attachment-definitions/guest-l2"]
    failures = arm.proof(client(missing), proof_request())["failures"]
    assert any("operator creates it" in f for f in failures) and any("guest-l2 does not exist" in f for f in failures)
    silent = proved_host()
    silent.unanswered.add("/apis/cdi.kubevirt.io/v1beta1/cdiconfigs/config")
    assert any("did not answer" in f for f in arm.proof(client(silent), proof_request())["failures"])
    pooled = proved_host()
    pooled.put("/api/v1/namespaces/openshift-cnv/configmaps/kubemacpool-mac-range-config",
               {"data": {"RANGE_START": "52:54:00:00:00:00", "RANGE_END": "52:54:00:ff:ff:ff"}})
    assert arm.proof(client(pooled), proof_request())["macPool"] == "inside"


KUBECONFIG = """apiVersion: v1
kind: Config
clusters:
- name: host
  cluster:
    server: https://api.host.example.test:6443
    certificate-authority-data: %s
users:
- name: admin
  user:
    token: sha256~token
contexts:
- name: admin@host
  context:
    cluster: host
    user: admin
current-context: admin@host
""" % base64.b64encode(CA.encode()).decode()


def test_one_kubeconfig_form_is_accepted_and_every_other_refused():
    connection = kapi.accepted(yaml_load(KUBECONFIG))
    assert connection["server"] == "https://api.host.example.test:6443" and connection["token"] == "sha256~token"
    for line, insert in [("    server:", "    insecure-skip-tls-verify: true\n"), ("    server:", "    proxy-url: http://proxy.example.test\n"),
                         ("    token:", "    exec:\n      command: login\n"), ("    token:", "    tokenFile: /tmp/token\n"),
                         ("    server:", "    certificate-authority: /tmp/ca\n"), ("    token:", "    client-key-data: a2V5\n"),
                         ("    server:", "    tls-server-name: other\n"), ("    token:", "    username: admin\n")]:
        text = KUBECONFIG.replace(line, insert + line, 1)
        with pytest.raises(kapi.Refused):
            kapi.accepted(yaml_load(text))
    with pytest.raises(kapi.Refused, match="exactly one cluster"):
        kapi.accepted(yaml_load(KUBECONFIG.replace("clusters:\n", "clusters:\n- name: second\n  cluster:\n    server: https://b.example.test\n", 1)))
    with pytest.raises(kapi.Refused, match="https URL"):
        kapi.accepted(yaml_load(KUBECONFIG.replace("https://api.host.example.test:6443", "http://api.host.example.test", 1)))


def test_a_claim_outliving_its_deleted_data_volume_keeps_the_removal_unproved():
    api = FakeAPI()
    ignored_changed, applied = arm.apply(host(api), request(), context())
    arm.upload_volume(pinned(api), VM, "digest-1", 1)
    media_uid = api.objects["/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + VM + "-media"]["metadata"]["uid"]
    collect = api.collect
    api.collect = lambda uid: None if uid == media_uid else collect(uid)
    ignored_changed, evidence = arm.destroy(host(api), request(), context(applied["receipts"]))
    assert not evidence["absent"] and {"kind": "PersistentVolumeClaim", "name": VM + "-media", "present": True} in evidence["volumes"]


PORTS = [
    ("pre_boot", lambda machine: arm.pre_boot(machine, VM)),
    ("state_read", lambda machine: arm.state_read(machine, VM, "digest-1")),
    ("power on", lambda machine: arm.change_power(machine, VM, "on")),
    ("power off", lambda machine: arm.change_power(machine, VM, "off", force=True)),
    ("eject", lambda machine: arm.eject(machine, VM)),
    ("insert", lambda machine: arm.upload_volume(machine, VM, "digest-2", 1)),
    ("attach", lambda machine: arm.attach(machine, VM)),
]


def presented_machine():
    api = FakeAPI()
    arm.apply(host(api), request(), context())
    arm.upload_volume(pinned(api), VM, "digest-1", 1)
    api.objects["/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + VM + "-media"]["status"]["phase"] = "Succeeded"
    arm.attach(pinned(api), VM)
    return api


@pytest.mark.parametrize("entry", [name for name, ignored_act in PORTS])
def test_every_port_entry_point_refuses_another_cluster_a_foreign_vm_or_no_record_and_changes_nothing(entry):
    act = dict(PORTS)[entry]
    for pin, refusal in [({"cluster": "cluster-0", "vm": None}, "kube-system UID differs"),
                         ({"cluster": "cluster-1", "vm": "another-uid"}, "is not the one this context's machine block recorded"),
                         ({"cluster": "", "vm": ""}, "has no cluster and UID")]:
        api = presented_machine()
        if pin["vm"] is None:
            pin = dict(pin, vm=api.objects[VM_PATH]["metadata"]["uid"])
        before = copy.deepcopy(api.objects)
        api.requests.clear()
        with pytest.raises(arm.PortRefused, match=refusal) as refused:
            act(host(api, pin=pin))
        assert refused.value.code == "identity-mismatch"
        assert api.objects == before and not [m for m, ignored_path in api.requests if m != "GET"]


def test_the_media_volume_is_owned_only_through_the_pinned_virtual_machine():
    api = presented_machine()
    volume = api.objects["/apis/cdi.kubevirt.io/v1beta1/namespaces/guests/datavolumes/" + VM + "-media"]
    assert volume["metadata"]["ownerReferences"][0]["uid"] == api.objects[VM_PATH]["metadata"]["uid"]
    volume["metadata"]["ownerReferences"] = [{"uid": "someone-else"}]
    with pytest.raises(kapi.Refused, match="not this context's"):
        arm.eject(pinned(api), VM)
    assert arm.state_read(pinned(api), VM, "digest-1")["media_state"] == "foreign"


def test_the_pinned_path_still_boots_and_ejects():
    api = presented_machine()
    machine = pinned(api)
    assert arm.state_read(machine, VM, "digest-1")["media_state"] == "own"
    assert arm.change_power(machine, VM, "on")["power"] == "On"
    assert arm.change_power(machine, VM, "off")["power"] == "Off"
    arm.eject(machine, VM)
    assert arm.state_read(machine, VM)["media_state"] == "none"
