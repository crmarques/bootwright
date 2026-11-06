"""The KubeVirt arm driven by the requests Go freezes, and the reads of its proof held against the roles Go renders.

goldens/kubevirt_machine_request.json and goldens/kubevirt_host_request.json are the bytes MachineRequest.Canonical and
HostRequest.Canonical produce in internal/substrate/kubevirt (TestTheCollectionDrivesTheArmFromTheRequestsGoFreezes), and
goldens/kubevirt_host_grants.json is the role table internal/addons/packages renders for a hosted namespace guests
(TestTheHostedAccountsRolesAreTheTableTheCollectionHoldsItsReadsAgainst). Each side reads the same file, so a member renamed in Go
fails here, and a read the proof gains without a grant fails here too.
"""

from __future__ import annotations

import json
import pathlib

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    FakeAPI, client, context, proved_host)
from ansible_collections.bootwright.core.tests.unit.plugins.action.test_kubevirt_proof import ProofAPI, config

GOLDENS = pathlib.Path(__file__).resolve().parents[2] / "goldens"
NONRESOURCE = {"/version", "/apis/subresources.kubevirt.io/v1/version"}
PROFILE = "/apis/cdi.kubevirt.io/v1beta1/storageprofiles/"
NAD_PATH = "/apis/k8s.cni.cncf.io/v1/namespaces/%s/network-attachment-definitions/%s"


def golden(name):
    raw = (GOLDENS / name).read_bytes()
    value = json.loads(raw.decode("utf-8"))
    assert canonical(value) + b"\n" == raw, name + " is not the bytes Go's canonical encoding and this collection's both write"
    return value


MACHINE = golden("kubevirt_machine_request.json")
HOST = golden("kubevirt_host_request.json")
GRANTS = json.loads((GOLDENS / "kubevirt_host_grants.json").read_text(encoding="utf-8"))
VM = MACHINE["name"]
VM_PATH = "/apis/kubevirt.io/v1/namespaces/guests/virtualmachines/" + VM


def frozen_context():
    return dict(context(operation="op-1"), block=MACHINE["identity"]["block"])


def proving_host(request):
    """A host that holds everything one frozen provider request proves: its root class and each declared network."""
    api = ProofAPI(proved_host())
    api.put(PROFILE + request["storageClass"], {"status": {"claimPropertySets": [{"accessModes": ["ReadWriteMany"], "volumeMode": "Block"}]}})
    for network in request["networks"]:
        derived = network.get("derived")
        members = {"cniVersion": "0.4.0", "name": network["name"], "type": "bridge", "bridge": "br-guest"}
        if derived:
            members = {"cniVersion": "0.4.0", "name": derived, "type": "ovn-k8s-cni-overlay", "topology": "localnet",
                       "netAttachDefName": "%s/%s" % (network["namespace"], network["name"])}
            api.put("/apis/k8s.ovn.org/v1/clusteruserdefinednetworks/" + derived, {"status": {"conditions": [{"type": "NetworkCreated", "status": "True"}]}})
        api.put(NAD_PATH % (network["namespace"], network["name"]), config(**members))
    return api


def test_the_frozen_requests_are_the_bytes_of_the_shapes_the_arm_reads():
    assert set(MACHINE) >= {"cpu", "diskGiB", "identity", "interfaces", "mediaVolume", "memoryMiB", "name", "namespace", "rootVolume", "version"}
    assert set(HOST) >= {"access", "identity", "macAddresses", "namespace", "networks", "storageClass", "version"}
    assert MACHINE["name"] == arm.name(MACHINE["identity"]["context"], MACHINE["identity"]["object"])
    assert [network["network"] for network in MACHINE["interfaces"]] == ["net/guest-l2", "storage-net"]
    assert [network.get("derived", "") for network in HOST["networks"]] == ["", "storage-net"]


def test_apply_from_the_frozen_machine_request_creates_the_virtual_machine_and_proves_its_shape():
    api = FakeAPI()
    arguments = {"request": MACHINE, "apply_operation": "op-1", "block": MACHINE["identity"]["block"], "registered": "2026-10-28T10:00:00Z",
                 "pinned": frozen_context()["pinned"]}
    changed, evidence = action.block("apply", client(api), arguments)
    assert changed and evidence["owned"] and evidence["postcondition"] and not evidence["foreign"]
    assert [m for m, p in api.requests if m == "POST" and p.endswith("/virtualmachines")] == ["POST"]
    again, replay = action.block("apply", client(api), dict(arguments, pinned=dict(arguments["pinned"], receipts=evidence["receipts"])))
    assert not again and replay["postcondition"]
    assert arm.frozen_difference(MACHINE, frozen_context(), api.objects[VM_PATH]) == ""
    assert arm.frozen_difference(dict(MACHINE, cpu=MACHINE["cpu"] + 1), frozen_context(), api.objects[VM_PATH]).endswith("cores")


def test_the_virtual_machine_body_renders_every_field_of_a_two_interface_request():
    body = arm.vm_body(MACHINE, frozen_context())
    spec, template = body["spec"], body["spec"]["template"]["spec"]
    macs = [iface["macAddress"] for iface in MACHINE["interfaces"]]
    assert len(macs) == 2 and spec["runStrategy"] == "Manual" and template["evictionStrategy"] == "LiveMigrate"
    assert template["domain"]["cpu"] == {"cores": 8, "sockets": 1, "threads": 1} and MACHINE["cpu"] == 8
    assert template["domain"]["memory"] == {"guest": "16384Mi"} and template["domain"]["resources"] == {"requests": {"memory": "16384Mi"}}
    assert MACHINE["memoryMiB"] == 16384
    assert template["domain"]["firmware"] == {"bootloader": {"efi": {"secureBoot": False}}}
    assert template["domain"]["devices"]["interfaces"] == [{"name": "nic0", "bridge": {}, "macAddress": macs[0]},
                                                           {"name": "nic1", "bridge": {}, "macAddress": macs[1]}]
    assert template["networks"] == [{"name": "nic0", "multus": {"networkName": "net/guest-l2"}},
                                    {"name": "nic1", "multus": {"networkName": "storage-net"}}]
    assert template["domain"]["devices"]["disks"] == [{"name": "root", "bootOrder": 1, "disk": {"bus": "virtio"}}]
    assert template["volumes"] == [{"name": "root", "dataVolume": {"name": MACHINE["rootVolume"]}}]
    root = spec["dataVolumeTemplates"][0]
    assert len(spec["dataVolumeTemplates"]) == 1 and root["metadata"]["name"] == MACHINE["rootVolume"] and root["spec"]["source"] == {"blank": {}}
    assert root["spec"]["storage"] == {"resources": {"requests": {"storage": "120Gi"}}, "storageClassName": MACHINE["storageClass"]}
    assert MACHINE["diskGiB"] == 120 and root["metadata"]["annotations"][arm.BIND] == "true"
    assert "tpm" not in template["domain"]["devices"] and "affinity" not in template


def test_a_persistent_vtpm_and_a_spread_render_beside_the_frozen_fields():
    body = arm.vm_body(dict(MACHINE, tpm={"persistent": True}, spread="masters"), frozen_context())["spec"]["template"]
    assert body["spec"]["domain"]["devices"]["tpm"] == {"persistent": True}
    assert body["metadata"]["labels"][arm.SPREAD] == "masters" and "podAntiAffinity" in json.dumps(body["spec"]["affinity"])


def test_the_provider_proof_holds_for_the_frozen_host_request():
    api = proving_host(HOST)
    evidence = arm.proof(client(api), HOST)
    assert evidence["postcondition"] and not evidence["failures"] and evidence["storageClass"] == HOST["storageClass"]
    assert evidence["networks"] == [{"attachment": "lab-guests", "cniType": "bridge", "present": True},
                                    {"attachment": "storage", "cniType": "ovn-k8s-cni-overlay", "present": True, "topology": "localnet"}]
    reviews = [path for method, path in api.requests if method == "POST"]
    assert reviews == ["/apis/authorization.k8s.io/v1/selfsubjectaccessreviews"] * len(HOST["access"]) and len(HOST["access"]) >= 10
    gone = proving_host(HOST)
    del gone.objects[NAD_PATH % ("guests", "guest-l2")]
    assert any("lab-guests: NetworkAttachmentDefinition guests/guest-l2 does not exist" in failure for failure in arm.proof(client(gone), HOST)["failures"])


def attributes(path):
    """The (group, resource, namespace, name, verb) a GET of this path asks RBAC, as the API server derives them."""
    parts = path.strip("/").split("/")
    group, rest = ("", parts[2:]) if parts[0] == "api" else (parts[1], parts[3:])
    namespace = ""
    if rest[0] == "namespaces" and len(rest) >= 3:
        namespace, rest = rest[1], rest[2:]
    name = rest[1] if len(rest) > 1 else ""
    return group, rest[0], namespace, name, "get" if name else "list"


def grants(table, asked):
    """The rules (role kind, name, namespace, index) of the table that allow one request, by the reach RBAC gives a binding."""
    group, resource, namespace, name, verb = asked
    held = []
    for binding in table["bindings"]:
        if binding["kind"] == "RoleBinding" and binding["namespace"] != namespace:
            continue
        for role in table["roles"]:
            if (role["kind"], role["name"]) != (binding["roleKind"], binding["role"]) or (
                    role["kind"] == "Role" and role["namespace"] != binding["namespace"]):
                continue
            for index, rule in enumerate(role["rules"]):
                if group in rule["apiGroups"] and resource in rule["resources"] and verb in rule["verbs"] and (
                        not rule.get("resourceNames") or name in rule["resourceNames"]):
                    held.append((role["kind"], role["name"], role.get("namespace", ""), index))
    return held


def proof_reads():
    """Every GET of a passing proof, once with the provider's root class and once reading the host's default."""
    reads = []
    for request in (HOST, dict(HOST, storageClass="")):
        api = proving_host(HOST)
        assert arm.proof(client(api), request)["postcondition"]
        reads += [path for method, path in api.requests if method == "GET" and path not in reads]
    return reads


def test_every_read_of_the_proof_is_granted_by_a_role_the_account_holds():
    reads = proof_reads()
    resources = sorted(set(path for path in reads if path not in NONRESOURCE))
    assert NONRESOURCE <= set(reads) and len(resources) >= 10
    assert {"/apis/k8s.ovn.org/v1/clusteruserdefinednetworks/storage-net", "/api/v1/nodes", "/apis/storage.k8s.io/v1/storageclasses",
            "/api/v1/namespaces/openshift-cnv/configmaps/kubemacpool-mac-range-config"} <= set(resources)
    ungranted = [path for path in resources if not grants(GRANTS, attributes(path))]
    assert not ungranted, "the proof reads what the hosted account's roles do not grant: %s" % ungranted


def test_the_grant_table_refuses_a_read_no_rule_covers():
    for asked in (("", "configmaps", "openshift-cnv", "other", "get"), ("", "configmaps", "other", "default-ingress-cert", "get"),
                  ("", "namespaces", "", "kube-system", "list"), ("", "namespaces", "", "elsewhere", "get"),
                  ("cdi.kubevirt.io", "cdiconfigs", "", "other", "get"), ("storage.k8s.io", "storageclasses", "", "", "delete"),
                  ("k8s.cni.cncf.io", "network-attachment-definitions", "elsewhere", "guest-l2", "get")):
        assert not grants(GRANTS, asked), asked
    assert grants(GRANTS, ("k8s.cni.cncf.io", "network-attachment-definitions", "guests", "guest-l2", "get"))
    unlisted = proof_reads() + ["/apis/cdi.kubevirt.io/v1beta1/datavolumes"]
    assert [path for path in unlisted if path not in NONRESOURCE and not grants(GRANTS, attributes(path))] == ["/apis/cdi.kubevirt.io/v1beta1/datavolumes"]


def proof_rules():
    return [(role["kind"], role["name"], role.get("namespace", ""), index) for role in GRANTS["roles"] if role["name"].endswith("-proof")
            for index in range(len(role["rules"]))]


@pytest.mark.parametrize("rule", proof_rules(), ids=lambda rule: "%s-%s-%s-%d" % rule)
def test_every_rule_of_the_proof_roles_is_used_by_a_read(rule):
    reads = [attributes(path) for path in proof_reads() if path not in NONRESOURCE]
    assert [asked for asked in reads if rule in grants(GRANTS, asked)], "%s %s grants a read the proof no longer makes" % rule[:2]
