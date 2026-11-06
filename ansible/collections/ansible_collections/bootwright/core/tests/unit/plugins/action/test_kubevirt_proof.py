"""The KubeVirt provider proof over a fake host API: what a host must hold for the arm's VirtualMachines and each attachment's kind."""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import (
    FakeAPI, client, proof_request, proved_host)

NODES = "/api/v1/nodes"
PROFILE = "/apis/cdi.kubevirt.io/v1beta1/storageprofiles/virt"
NAD = "/apis/k8s.cni.cncf.io/v1/namespaces/guests/network-attachment-definitions/"
CUDN = "/apis/k8s.ovn.org/v1/clusteruserdefinednetworks/storage-net"
DERIVED = {"attachment": "storage", "derived": "storage-net", "name": "storage-net", "namespace": "guests"}


class ProofAPI(FakeAPI):
    """The fake host with per-path answers and a node list served one page per request."""

    def __init__(self, base):
        self.__dict__.update(base.__dict__)
        self.answers, self.pages = {}, None

    def __call__(self, method, target, headers, payload):
        path, ignored_mark, query = target.partition("?")
        if path in self.answers:
            self.requests.append((method, path))
            return self.answers[path], b'{"kind":"Status","reason":"Forbidden"}'
        if method == "GET" and path == NODES and self.pages is not None:
            self.requests.append((method, path + "?" + query))
            token = query.partition("continue=")[2]
            index = int(token) if token else 0
            page = self.pages[min(index, len(self.pages) - 1)]
            following = str(index + 1) if index + 1 < len(self.pages) or self.pages[-1] is None else ""
            return 200, json.dumps({"items": page or [], "metadata": {"continue": following}}).encode()
        return FakeAPI.__call__(self, method, target, headers, payload)


class DenyingAPI(ProofAPI):
    """The fake host whose SelfSubjectAccessReview answers allowed false for the resources it is told to deny."""

    def __init__(self, base, *denied):
        ProofAPI.__init__(self, base)
        self.denied, self.reviews = set(denied), []

    def __call__(self, method, target, headers, payload):
        if method == "POST" and target.endswith("selfsubjectaccessreviews"):
            self.requests.append((method, target))
            attributes = json.loads(payload)["spec"]["resourceAttributes"]
            self.reviews.append(attributes)
            allowed = (attributes["resource"], attributes["verb"]) not in self.denied
            return 201, json.dumps({"status": {"allowed": allowed}}).encode()
        return ProofAPI.__call__(self, method, target, headers, payload)


def node(name, schedulable="true", kvm="1k"):
    allocatable = {} if kvm is None else {"devices.kubevirt.io/kvm": kvm}
    return {"metadata": {"name": name, "labels": {"kubevirt.io/schedulable": schedulable}}, "status": {"allocatable": allocatable}}


def run(api, sleep=None, **extra):
    clock = {"now": 0.0, "slept": []}

    def pause(seconds):
        clock["slept"].append(seconds)
        clock["now"] += seconds
        if sleep:
            sleep(clock)

    evidence = arm.proof(client(api), proof_request(**extra), lambda: clock["now"], pause)
    return evidence, clock["slept"]


def config(**members):
    return {"metadata": {"name": "x"}, "spec": {"config": json.dumps(members)}}


def derived_host(**members):
    api = ProofAPI(proved_host())
    body = {"cniVersion": "0.4.0", "name": "storage-net", "type": "ovn-k8s-cni-overlay", "topology": "localnet",
            "netAttachDefName": "guests/storage-net", "mtu": 1500}
    body.update(members)
    api.put(NAD + "storage-net", config(**{k: v for k, v in body.items() if v is not None}))
    api.put(CUDN, {"status": {"conditions": [{"type": "NetworkCreated", "status": "True"}]}})
    return api


def test_the_proof_records_what_the_host_offers_the_machines():
    evidence, slept = run(proved_host())
    assert evidence["postcondition"] and not evidence["failures"] and slept == []
    assert evidence["rootAccessModes"] == ["ReadWriteMany"] and evidence["rootVolumeMode"] == "Block"
    assert evidence["schedulableNodes"] == 2


def test_an_absent_cdi_configuration_or_kube_system_namespace_fails_the_proof():
    for gone, named in (("/apis/cdi.kubevirt.io/v1beta1/cdiconfigs/config", "CDI configuration"),
                        ("/api/v1/namespaces/kube-system", "kube-system")):
        api = proved_host()
        del api.objects[gone]
        evidence, ignored_slept = run(api)
        assert not evidence["postcondition"] and any(named in f and "does not exist" in f for f in evidence["failures"])


def test_two_default_classes_refuse_naming_both_unless_the_provider_names_one():
    api = proved_host()
    api.put("/apis/storage.k8s.io/v1/storageclasses", {"items": [
        {"metadata": {"name": "virt", "annotations": {arm.DEFAULT_VIRT: "true"}}},
        {"metadata": {"name": "fast", "annotations": {arm.DEFAULT_VIRT: "true"}}},
        {"metadata": {"name": "plain", "annotations": {arm.DEFAULT_CLASS: "true"}}}]})
    evidence, ignored_slept = run(api)
    assert evidence["storageClass"] == "" and any("fast, virt" in f and "storageClassRef" in f for f in evidence["failures"])
    evidence, ignored_slept = run(api, storageClass="virt")
    assert evidence["postcondition"] and evidence["storageClass"] == "virt"
    plain = proved_host()
    plain.put("/apis/storage.k8s.io/v1/storageclasses", {"items": [
        {"metadata": {"name": "virt", "annotations": {arm.DEFAULT_VIRT: "true", arm.DEFAULT_CLASS: "true"}}},
        {"metadata": {"name": "plain", "annotations": {arm.DEFAULT_CLASS: "true"}}}]})
    evidence, ignored_slept = run(plain)
    assert evidence["postcondition"] and evidence["storageClass"] == "virt"


def test_only_the_first_claim_property_set_decides_the_root():
    api = proved_host()
    api.put(PROFILE, {"status": {"claimPropertySets": [{"accessModes": ["ReadWriteOnce"], "volumeMode": "Filesystem"},
                                                       {"accessModes": ["ReadWriteMany"], "volumeMode": "Block"}]}})
    evidence, ignored_slept = run(api)
    assert evidence["rootAccessModes"] == ["ReadWriteOnce"] and evidence["rootVolumeMode"] == "Filesystem" and not evidence["readWriteMany"]
    assert any("offers no ReadWriteMany" in f and "host drain cannot migrate" in f for f in evidence["failures"])


def test_a_storage_profile_cdi_has_not_filled_yet_is_waited_for_within_its_bound():
    api = proved_host()
    del api.objects[PROFILE]

    def fills(clock):
        if clock["now"] >= 15:
            api.put(PROFILE, {"status": {"claimPropertySets": [{"accessModes": ["ReadWriteMany"], "volumeMode": "Block"}]}})

    evidence, slept = run(api, fills)
    assert evidence["postcondition"] and slept == [5, 5, 5]
    absent = proved_host()
    del absent.objects[PROFILE]
    evidence, slept = run(absent)
    assert sum(slept) <= 120 and sum(slept) >= 115 and set(slept) == {5}
    assert any("has no StorageProfile" in f for f in evidence["failures"])
    empty = proved_host()
    empty.put(PROFILE, {"status": {}})
    evidence, slept = run(empty)
    assert sum(slept) >= 115 and any("lists no claimPropertySets" in f for f in evidence["failures"])


def test_the_proof_requires_a_node_schedulable_for_virtual_machines_with_kvm():
    api = proved_host()
    api.put(NODES, {"items": [node("a", kvm="0"), node("b", kvm=None), node("c", schedulable="false"), node("d", kvm="many")],
                    "metadata": {}})
    evidence, ignored_slept = run(api)
    assert evidence["schedulableNodes"] == 0 and any("no node of the host is schedulable" in f for f in evidence["failures"])
    paged = ProofAPI(proved_host())
    paged.pages = [[node("a", kvm="0")], [node("b")], [node("c", kvm="2")]]
    evidence, ignored_slept = run(paged)
    assert evidence["postcondition"] and evidence["schedulableNodes"] == 2
    assert [p for m, p in paged.requests if p.startswith(NODES)] == [
        NODES + "?limit=500", NODES + "?limit=500&continue=1", NODES + "?limit=500&continue=2"]
    endless = ProofAPI(proved_host())
    endless.pages = [[node("a")], None]
    evidence, ignored_slept = run(endless)
    assert not evidence["postcondition"] and any("nodes" in f and "could not be read" in f for f in evidence["failures"])
    forbidden = ProofAPI(proved_host())
    forbidden.answers[NODES] = 403
    evidence, ignored_slept = run(forbidden)
    assert any("nodes" in f and "could not be read" in f for f in evidence["failures"])


def test_a_declared_attachment_must_be_a_bridge_and_its_settings_are_recorded():
    api = proved_host()
    api.put(NAD + "guest-l2", config(type="bridge", bridge="br-guest", mtu=9000, macspoofchk=True))
    evidence, ignored_slept = run(api)
    assert evidence["postcondition"]
    assert evidence["networks"] == [{"attachment": "lab-guests", "cniType": "bridge", "macSpoofCheck": True, "mtu": 9000, "present": True}]
    api.put(NAD + "guest-l2", config(type="macvlan", master="eth1"))
    evidence, ignored_slept = run(api)
    assert evidence["networks"][0]["present"] is False and evidence["networks"][0]["cniType"] == "macvlan"
    assert ("attachment lab-guests: NetworkAttachmentDefinition guests/guest-l2 is of CNI type macvlan; the arm attaches only to a "
            "Localnet ClusterUserDefinedNetwork or a bridge NetworkAttachmentDefinition") in evidence["failures"]
    for broken in ({"metadata": {}}, {"spec": {"config": "not json"}}, {"spec": {"config": "[1]"}}):
        api.put(NAD + "guest-l2", broken)
        evidence, ignored_slept = run(api)
        assert any("lab-guests" in f and "not a JSON object" in f for f in evidence["failures"])


def test_a_bridge_shipped_as_cnv_bridge_or_first_in_a_plugin_list_is_a_bridge():
    api = proved_host()
    api.put(NAD + "guest-l2", config(cniVersion="0.3.1", name="guest-l2", plugins=[
        {"type": "cnv-bridge", "bridge": "br-guest", "macspoofchk": False}, {"type": "cnv-tuning"}]))
    evidence, ignored_slept = run(api)
    assert evidence["postcondition"] and not evidence["failures"]
    assert evidence["networks"] == [{"attachment": "lab-guests", "cniType": "cnv-bridge", "macSpoofCheck": False, "present": True}]
    api.put(NAD + "guest-l2", config(plugins=[{"type": "tuning"}, {"type": "bridge"}]))
    evidence, ignored_slept = run(api)
    assert any("is of CNI type tuning" in f for f in evidence["failures"])


def test_a_derived_attachment_must_be_a_localnet_overlay_without_ipam():
    evidence, ignored_slept = run(derived_host(), networks=[DERIVED])
    assert evidence["postcondition"]
    assert evidence["networks"] == [{"attachment": "storage", "cniType": "ovn-k8s-cni-overlay", "mtu": 1500, "present": True, "topology": "localnet"}]
    for members, found in (({"subnets": "10.0.0.0/24"}, "subnets"), ({"topology": "layer2"}, "layer2"), ({"type": "bridge"}, "bridge")):
        evidence, ignored_slept = run(derived_host(**members), networks=[DERIVED])
        assert not evidence["postcondition"] and any("attachment storage" in f and found in f for f in evidence["failures"])


def test_the_cluster_user_defined_network_read_tolerates_only_a_credential_that_may_not_read_it():
    absent = derived_host()
    del absent.objects[CUDN]
    evidence, ignored_slept = run(absent, networks=[DERIVED])
    assert any("ClusterUserDefinedNetwork storage-net does not exist" in f for f in evidence["failures"])
    forbidden = derived_host()
    forbidden.answers[CUDN] = 403
    evidence, ignored_slept = run(forbidden, networks=[DERIVED])
    assert evidence["postcondition"]
    silent = derived_host()
    silent.unanswered.add(CUDN)
    evidence, ignored_slept = run(silent, networks=[DERIVED])
    assert any("ClusterUserDefinedNetwork storage-net could not be read" in f for f in evidence["failures"])


def test_the_protocol_publishes_every_field_of_the_proof():
    evidence, ignored_slept = run(derived_host(), networks=[DERIVED, proof_request()["networks"][0]])
    published, failure = protocol.completion({"subject": "host", "digest": "a" * 64, "observation": evidence, "observed": True})
    assert failure is None and published["schedulableNodes"] == 2 and published["rootVolumeMode"] == "Block"
    assert published["rootAccessModes"] == ["ReadWriteMany"] and published["networks"] == evidence["networks"]
    assert set(published) - {"absent", "request"} == set(evidence)
    with pytest.raises(ValueError):
        protocol.completion({"subject": "host", "digest": "a" * 64, "observation": dict(evidence, schedulableNodes=True)})


def test_a_denied_access_review_names_the_verb_the_resource_and_the_namespace():
    api = DenyingAPI(proved_host(), ("virtualmachines", "create"), ("virtualmachines", "update"))
    access = [{"group": "kubevirt.io", "resource": "virtualmachines", "verb": "create"},
              {"group": "subresources.kubevirt.io", "resource": "virtualmachines", "subresource": "start", "verb": "update"},
              {"group": "cdi.kubevirt.io", "resource": "datavolumes", "verb": "get"}]
    evidence, ignored_slept = run(api, access=access)
    assert [review["verb"] for review in api.reviews] == ["create", "update", "get"] and {r["namespace"] for r in api.reviews} == {"guests"}
    assert not evidence["postcondition"]
    assert evidence["failures"] == ["the kubeconfig's credential may not create virtualmachines in namespace guests",
                                    "the kubeconfig's credential may not update virtualmachines/start in namespace guests"]
    allowed, ignored_slept = run(DenyingAPI(proved_host()), access=access)
    assert allowed["postcondition"] and not allowed["failures"]


@pytest.mark.parametrize("resource, what, named", [
    ("/apis/cdi.kubevirt.io/v1beta1/cdiconfigs/config", "the CDI configuration", "the CDI configuration"),
    (PROFILE, "the StorageProfile of virt", "the StorageProfile of virt")])
def test_a_credential_the_host_refuses_on_a_cdi_object_fails_the_proof_naming_the_object_and_the_credential(resource, what, named):
    api = ProofAPI(proved_host())
    api.answers[resource] = 403
    evidence, ignored_slept = run(api)
    assert ("GET", resource) in api.requests
    assert not evidence["postcondition"] and len(evidence["failures"]) == 1
    assert evidence["failures"][0].startswith(named + " could not be read: the host refused the kubeconfig's credential (HTTP 403)")
    assert "--authorize rebind=<kubeconfig Secret>" in evidence["failures"][0] and what in evidence["failures"][0]


def test_an_ingress_certificate_authority_the_host_does_not_publish_fails_the_proof():
    path = "/api/v1/namespaces/openshift-config-managed/configmaps/default-ingress-cert"
    absent = proved_host()
    del absent.objects[path]
    bare = proved_host()
    bare.put(path, {"data": {"ca-bundle.crt": "not a certificate"}})
    empty = proved_host()
    empty.put(path, {"metadata": {"name": "default-ingress-cert"}})
    forbidden = ProofAPI(proved_host())
    forbidden.answers[path] = 403
    for api in (absent, bare, empty):
        evidence, ignored_slept = run(api)
        assert ("GET", path) in api.requests and not evidence["postcondition"]
        assert evidence["failures"] == ["ConfigMap openshift-config-managed/default-ingress-cert publishes no ingress certificate authority"]
    evidence, ignored_slept = run(forbidden)
    assert ("GET", path) in forbidden.requests and not evidence["postcondition"]
    assert evidence["failures"][0].startswith("the ingress certificate authority could not be read: the host refused the kubeconfig's credential")


@pytest.mark.parametrize("status", ["False", "Unknown"])
def test_a_cluster_user_defined_network_that_reports_no_network_created_fails_the_proof(status):
    api = derived_host()
    api.put(CUDN, {"status": {"conditions": [{"type": "Ready", "status": "True"}, {"type": "NetworkCreated", "status": status}]}})
    evidence, ignored_slept = run(api, networks=[DERIVED])
    assert ("GET", CUDN) in api.requests and not evidence["postcondition"]
    assert evidence["failures"] == ["attachment storage: ClusterUserDefinedNetwork storage-net reports NetworkCreated " + status]
    assert evidence["networks"] == [{"attachment": "storage", "cniType": "ovn-k8s-cni-overlay", "mtu": 1500, "present": False, "topology": "localnet"}]
    api.put(CUDN, {"status": {"conditions": [{"type": "Ready", "status": "False"}]}})
    evidence, ignored_slept = run(api, networks=[DERIVED])
    assert evidence["postcondition"] and evidence["networks"][0]["present"]
