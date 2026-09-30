"""Every cluster node the boot does not skip is proved before anything is inserted.

A node's boot (roles/containercluster_install_agent/tasks/boot.yml) includes its
own substrate's pre-boot proof immediately before the published image is
inserted: a machine the libvirt substrate created through the controller that
realization started, and a physical machine against the NICs its install
request froze and the identity its Machine block pinned earlier in the same
operation. Both includes sit inside the block a node already running from this
cluster's image skips, so such a node is neither proved nor given media, and
inside the block the boot phase's budget bounds, so a proof spends only what
that budget has left (specs/container-clusters.md, Boot and Budgets).

These cases find each include by its role and the tasks_from stem pre_boot, and
render its variables through the loop that includes boot.yml (apply.yml), over
material written as the runner writes it (--extra-vars @request.json, which
ansible-core loads trusted as a template). Rendering needs Ansible's controller
(DataLoader and Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import json
import pathlib

import pytest
from ansible.errors import AnsibleUndefinedVariable
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
LOADER = DataLoader()

INCLUDE = "ansible.builtin.include_role"
BOOT = "bootwright.core.redfish_boot"
ARMS = {"libvirt": "bootwright.core.substrate_libvirt_machine", "baremetal": "bootwright.core.substrate_baremetal_machine"}

ENDPOINT = "https://metal-02-bmc.lab.example.test/redfish/v1/Systems/1"
MACS = ["aa:bb:cc:dd:ee:12", "aa:bb:cc:dd:ee:11"]


def loaded(name):
    """One task file as a play loads it: its templates trusted."""
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)
            if isinstance(task, dict)]


def walk(tasks, parents=()):
    """Every task with the blocks around it, outermost first."""
    for task in tasks:
        yield task, parents
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)], parents + (task,))


def proof(arm):
    """The one include of an arm's pre-boot proof, with the blocks around it."""
    found = [(task, parents) for task, parents in walk(loaded("boot.yml"))
             if (task.get(INCLUDE) or {}).get("name") == ARMS[arm]
             and pathlib.PurePosixPath(str(task[INCLUDE].get("tasks_from", ""))).stem == "pre_boot"]
    assert len(found) == 1, "boot.yml includes the %s pre-boot proof %d times" % (arm, len(found))
    return found[0]


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def node(substrate="baremetal", hardware=None):
    """Node 1 of the frozen request as the loop hands it to boot.yml."""
    value = {
        "address": "198.51.100.42",
        "controller": {"credentialsRef": "metal-bmc", "endpoint": ENDPOINT, "tlsVerify": False},
        "machine": "metal-02", "name": "master-1", "physical": substrate == "baremetal", "substrate": substrate,
    }
    if hardware is not None:
        value["hardware"] = hardware
    return value


def rendering(tmp_path, current, index=1, material=None):
    """What boot.yml renders for one node: the role's defaults, the material the
    runner wrote, the loop's own variables and the node it is on."""
    written = tmp_path / "request.json"
    written.write_text(json.dumps({"bootwright_cluster_install_material": material or {}}))
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    # The loader caches by path, and every case writes the same path.
    variables.update(LOADER.load_from_file(str(written), cache="none", trusted_as_template=True))
    loop = [task for task in loaded("apply.yml") if task.get("ansible.builtin.include_tasks") == "boot.yml"]
    assert len(loop) == 1
    variables.update(loop[0]["vars"])
    variables.update({"containercluster_install_agent_node": current, "containercluster_install_agent_index": index})
    return Templar(loader=LOADER, variables=variables)


def material():
    """Two nodes' credentials, and a pin for each, so a node reading another's shows."""
    return {
        "controllerUserNode0": "/run/bootwright/bmc-user-metal-01",
        "controllerPasswordNode0": "/run/bootwright/bmc-password-metal-01",
        "controllerUserNode1": "/run/bootwright/bmc-user-metal-02",
        "controllerPasswordNode1": "/run/bootwright/bmc-password-metal-02",
        "pinnedUUIDBase64Node0": encoded("4c4c4544-0042-3510-8052-b4c04f4d4e31"),
        "pinnedSerialBase64Node0": encoded("CZJ2440ABC"),
        "pinnedUUIDBase64Node1": encoded("4c4c4544-0042-3510-8052-b4c04f4d4e32"),
    }


@pytest.mark.parametrize("arm", sorted(ARMS))
def test_each_arm_is_proved_before_the_insert_under_the_skip_and_the_boot_budget(arm):
    task, parents = proof(arm)
    assert len(parents) == 2, "the %s proof is not directly inside the skip and the budget" % arm
    skip, bounded = parents
    assert "containercluster_install_agent_state.powered" in str(skip.get("when", ""))
    assert "timeout" in bounded and bounded in skip["block"]
    inserts = [index for index, step in enumerate(bounded["block"]) if (step.get(BOOT) or {}).get("operation") == "insert"]
    assert len(inserts) == 1 and bounded["block"].index(task) < inserts[0]
    assert task["when"] == "containercluster_install_agent_node.substrate == '%s'" % arm


def test_a_physical_node_is_proved_against_the_nics_its_request_froze(tmp_path):
    task, _parents = proof("baremetal")
    expected = task["vars"]["substrate_baremetal_machine_target_expected"]
    hardware = {"interfaces": [{"macAddress": MACS[0], "name": "enp2s0"}, {"macAddress": MACS[1], "name": "enp1s0"}]}
    assert rendering(tmp_path, node(hardware=hardware)).template(expected) == MACS
    # A physical node frozen without hardware would prove nothing, so it fails
    # when its proof's input is rendered rather than comparing an empty set.
    with pytest.raises(AnsibleUndefinedVariable):
        rendering(tmp_path, node()).template(expected)


def test_each_proof_reaches_the_nodes_own_controller_with_its_own_credential(tmp_path):
    hardware = {"interfaces": [{"macAddress": MACS[0], "name": "enp2s0"}]}
    for arm in sorted(ARMS):
        task, _parents = proof(arm)
        prefix = task[INCLUDE]["name"].split(".")[-1] + "_target_"
        scope = rendering(tmp_path, node(arm, hardware), material=material())
        rendered = {name[len(prefix):]: scope.template(value) for name, value in task["vars"].items()}
        assert (rendered["endpoint"], rendered["verify"]) == (ENDPOINT, False), arm
        assert (rendered["user"], rendered["password"]) == (
            "/run/bootwright/bmc-user-metal-02", "/run/bootwright/bmc-password-metal-02"), arm
        assert set(rendered) == ({"endpoint", "verify", "user", "password"} if arm == "libvirt" else {
            "endpoint", "verify", "user", "password", "ca", "expected", "pinned_uuid_base64", "pinned_serial_base64"}), arm
        if arm == "baremetal":
            assert rendered["ca"] == "", "a cluster node's controller is reached through no bundle until B67"


# Each node reads only the pin under its own position in the frozen order, and
# a field its Machine block did not pin reaches the proof as nothing.
@pytest.mark.parametrize("index, uuid, serial", [
    (0, encoded("4c4c4544-0042-3510-8052-b4c04f4d4e31"), encoded("CZJ2440ABC")),
    (1, encoded("4c4c4544-0042-3510-8052-b4c04f4d4e32"), ""),
    (2, "", ""),
])
def test_each_physical_node_is_held_to_its_own_pin(tmp_path, index, uuid, serial):
    task, _parents = proof("baremetal")
    scope = rendering(tmp_path, node(hardware={"interfaces": []}), index=index, material=material())
    arguments = task["vars"]
    assert scope.template(arguments["substrate_baremetal_machine_target_pinned_uuid_base64"]) == uuid
    assert scope.template(arguments["substrate_baremetal_machine_target_pinned_serial_base64"]) == serial
