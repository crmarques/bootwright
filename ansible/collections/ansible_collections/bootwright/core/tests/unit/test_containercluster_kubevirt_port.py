"""The agent installation composes KubeVirt's pinned port for a pushed cluster.

A node on a KubeVirt Machine takes pushed delivery and a pinned port
(specs/substrates.md, KubeVirt machine realization; specs/openshift-clusters.md,
Nodes on KubeVirt and vSphere): every entry point the installation calls is
handed the identity the node's Machine block recorded under its position, the
kubeconfig as its user file and no password or trust bundle, and the insert is
handed the image file in the work area, the controller stage's virtctl and the
frozen media identity in place of a link. These cases render what each include
hands the port for such a node, from install-request-kubevirt-none.golden in
internal/containercluster/agentinstall/testdata, and validate it against the
KubeVirt arm's own argument specs with the validator ansible-core's prepended
task uses; a fetching node is still handed no pin and no pushed input.
"""

from __future__ import annotations

import base64
import pathlib

import pytest
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
ROLE = ROLES / "containercluster_install_agent"
LOADER = DataLoader()
INCLUDE = "ansible.builtin.include_role"
MATERIAL = "/run/bootwright/material/"
CLUSTER_UID = "0b1c2d3e-0000-4000-8000-000000000001"
VM_UID = "6a1f0000-0000-4000-8000-000000000002"
IMAGE = "/var/lib/bootwright-clusters/lab/kubevirt/agent.iso"
IDENTITY = "1e0719118245b0f464190073bb910808a6934b598f7a073d8390969c6e1e23b0"
VIRTCTL = "/var/lib/bootwright/controller/clients/area/tools/virtctl/kubevirt/v1.6.2/virtctl"


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def loaded(name):
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def specs(role):
    return LOADER.load_from_file(str(ROLES / role / "meta" / "argument_specs.yml"))["argument_specs"]


def scope(pushed=True, pinned=True):
    """Node 1 of a frozen KubeVirt cluster as the node play hands it to the role,
    or the same node of a fetching libvirt cluster."""
    node = {"address": "198.51.100.32", "machine": "kubevirt-02", "name": "master-1", "physical": False,
            "controller": {"credentialsRef": "host-kubeconfig", "endpoint": "kubevirt:bootwright-guests/kubevirt-02-6fcb7dda",
                           "tlsVerify": True, "virtualMedia": {"removeCertificate": False, "restoreVerification": False, "trust": ""}},
            "controllerKind": "api", "portPinned": True, "stateReadSeconds": 120, "substrate": "kubevirt"}
    material = {"controllerUserNode1": MATERIAL + "bmc-user-kubevirt-02", "virtctl": VIRTCTL,
                "pinnedUUIDBase64Node1": encoded(CLUSTER_UID), "pinnedSerialBase64Node1": encoded(VM_UID)}
    request = {"deliveryKind": "push", "image": {"path": IMAGE, "url": ""}, "imageIdentity": IDENTITY, "nodes": [node, node]}
    if not pushed:
        node = dict(node, controller=dict(node["controller"], endpoint="http://192.0.2.1:8000/redfish/v1/Systems/1",
                                          virtualMedia={"removeCertificate": False, "restoreVerification": False, "trust": "importCertificate"}),
                    controllerKind="emulated", stateReadSeconds=60, substrate="libvirt")
        node.pop("portPinned")
        material = {"controllerUserNode1": MATERIAL + "bmc-user", "controllerPasswordNode1": MATERIAL + "bmc-password",
                    "artifactCertificate": MATERIAL + "artifact-certificate"}
        request = {"deliveryKind": "fetch", "image": {"path": "/srv/private/clusters/sno", "url": "https://192.0.2.1/private/clusters/sno"},
                   "nodes": [node, node]}
    if not pinned:
        material = {key: value for key, value in material.items() if not key.startswith("pinned")}
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    playbook = LOADER.load_from_file(str(ROLES.parent / "playbooks/containercluster/install_apply.yml"), trusted_as_template=True)
    variables.update(next(play for play in playbook if play["name"] == "Boot node rounds")["vars"])
    variables.update({"bootwright_cluster_install_material": material, "bootwright_cluster_install_request": request,
                      "bootwright_node_index": 1, "bootwright_run_scratch": "/var/tmp/bootwright-run-scratch-1-1",
                      "bootwright_controller_host": "controller", "hostvars": {"controller": {}}})
    variables.update({"containercluster_install_agent_node": node, "containercluster_install_agent_index": 1})
    return variables


def rendered(task, variables, extra=None):
    """What one include hands the port: its own variables layered over the scope."""
    layered = dict(variables, **(extra or {}))
    layered.update(task["vars"])
    return Templar(loader=LOADER, variables=layered).template(task["vars"]["bootwright_machine_port"])


def port(operation, variables):
    include = next(task for task in loaded("port.yml") if INCLUDE in task)
    return rendered(include, variables, {"containercluster_install_agent_port_operation": operation})


def dispatched(file, entry):
    return next(task for task in walk(loaded(file))
                if INCLUDE in task and task[INCLUDE].get("tasks_from") == entry and task[INCLUDE]["name"] == "bootwright.core.substrate_machine")


def valid(entry, port_input, role="substrate_kubevirt_machine"):
    options = specs(role)[entry]["options"]
    result = ArgumentSpecValidator(options).validate({"bootwright_machine_port": port_input}, validate_role_argument_spec=True)
    assert not result.error_messages, "%s: %s" % (entry, result.error_messages)


PIN = {"pinned_uuid_base64": encoded(CLUSTER_UID), "pinned_serial_base64": encoded(VM_UID)}
ACCESS = {"endpoint": "kubevirt:bootwright-guests/kubevirt-02-6fcb7dda", "user": MATERIAL + "bmc-user-kubevirt-02",
          "password": "", "ca": "", "verify": True}


@pytest.mark.parametrize("operation, arguments", [
    ("state_read", {"media": True, "media_identity": IDENTITY}),
    ("media_insert", {"image_path": IMAGE, "virtctl": VIRTCTL, "media_identity": IDENTITY}),
    ("media_eject", {"certificate": "", "restore_verification": False, "remove_certificate": False}),
])
def test_every_port_call_hands_a_pushed_node_its_own_pin_and_kubeconfig(operation, arguments):
    handed = port(operation, scope())
    assert handed == dict(ACCESS, **PIN, **arguments), handed
    valid(operation, handed)


def test_the_boot_and_the_disk_boot_hand_a_pinned_node_its_pin():
    for file, entry, asked in (("boot.yml", "boot_media", {"installer_powers_off": False}), ("release.yml", "boot_disk", {"power_on": False})):
        handed = rendered(dispatched(file, entry), scope())
        assert handed == dict(ACCESS, **PIN, **asked), (file, handed)
        valid(entry, handed)
    proof = rendered(dispatched("boot.yml", "pre_boot"), scope())
    assert (proof["pinned_uuid_base64"], proof["pinned_serial_base64"]) == (PIN["pinned_uuid_base64"], PIN["pinned_serial_base64"])
    valid("pre_boot", proof)


# A node whose Machine recorded nothing is handed empty pins, which every
# KubeVirt entry point refuses as identity-mismatch with nothing changed.
def test_an_unpinned_node_is_handed_empty_pins_rather_than_another_nodes():
    handed = port("media_eject", scope(pinned=False))
    assert (handed["pinned_uuid_base64"], handed["pinned_serial_base64"]) == ("", "")


def test_a_fetching_node_is_handed_no_pin_and_no_pushed_input():
    variables = scope(pushed=False)
    insert = port("media_insert", variables)
    assert "image_path" not in insert and "virtctl" not in insert and "pinned_uuid_base64" not in insert
    assert insert["image_url_file"] == "/var/tmp/bootwright-run-scratch-1-1/cluster-insert-1"
    valid("media_insert", insert, "substrate_libvirt_machine")
    assert port("state_read", variables) == {
        "endpoint": "http://192.0.2.1:8000/redfish/v1/Systems/1", "user": MATERIAL + "bmc-user",
        "password": MATERIAL + "bmc-password", "ca": "", "verify": True, "media": True}
    for file, entry in (("boot.yml", "boot_media"), ("release.yml", "boot_disk")):
        handed = rendered(dispatched(file, entry), variables)
        assert "pinned_uuid_base64" not in handed and "pinned_serial_base64" not in handed


def test_a_pushed_node_presents_its_own_image_only_by_the_ports_answer():
    task = next(task for task in walk(loaded("node_state.yml")) if task.get("name") == "Prove the presented pushed image identity")
    variables = scope()
    assert Templar(loader=LOADER, variables=variables).evaluate_conditional(task["when"])
    for answer, own in (("own", True), ("foreign", False), ("none", False), ("unknown", False)):
        layered = dict(variables, containercluster_install_agent_node_read={"media": "kubevirt-02-6fcb7dda-media", "media_state": answer})
        fact = Templar(loader=LOADER, variables=layered).template(task["ansible.builtin.set_fact"]["containercluster_install_agent_node_identity"])
        assert fact == {"own": own}, answer
    assert not Templar(loader=LOADER, variables=scope(pushed=False)).evaluate_conditional(task["when"])


def test_a_pushed_cluster_reads_own_media_from_the_ports_answer_in_every_state_read():
    resolve = next(task for task in loaded("state.yml") if task.get("name") == "Resolve what the cluster and its nodes answered")
    expression = resolve["ansible.builtin.set_fact"]["containercluster_install_agent_state"]["ownMedia"]
    answers = {"results": [{"item": {"machine": "kubevirt-01"}, "media": "a", "media_state": "own"},
                           {"item": {"machine": "kubevirt-02"}, "media": "b", "media_state": "foreign"},
                           {"item": {"machine": "kubevirt-03"}, "media": "", "media_state": "none"}]}
    variables = dict(scope(), containercluster_install_agent_controllers=answers)
    assert Templar(loader=LOADER, variables=variables).template(expression) == ["kubevirt-01"]


def test_a_pushed_boot_needs_the_image_file_rather_than_a_link():
    ready = next(task for task in loaded("boot.yml") if task.get("name") == "Decide whether this cluster has an image for its nodes to boot")
    expression = ready["ansible.builtin.set_fact"]["containercluster_install_agent_image_ready"]
    for stat, expected in (({"stat": {"isreg": True}}, True), ({"stat": {"exists": False}}, False)):
        variables = dict(scope(), containercluster_install_agent_pushed_image=stat, containercluster_install_agent_url="")
        assert Templar(loader=LOADER, variables=variables).template(expression) is expected
