"""Every cluster node the boot does not skip is proved before anything is inserted.

A node's boot (roles/containercluster_install_agent/tasks/boot.yml) includes its
own substrate's pre-boot proof immediately before the published image is
inserted: a machine the libvirt substrate created through the controller that
realization started, and a physical machine against the NICs its install
request froze and the identity its Machine block pinned earlier in the same
operation. Both includes sit inside the block a node already running from this
cluster's image skips, so such a node is neither proved nor given media, and
inside the block the boot phase's budget bounds, so a proof spends only what
that budget has left (specs/openshift-clusters.md, Boot and Budgets).

These cases find each include by its role and the tasks_from stem pre_boot, and
render its variables through the loop that includes boot.yml (apply.yml), over
material the runner passes as --extra-vars @request.json. The runner marks
every string there unsafe, so ansible-core reads it as data
(test_request_strings_are_data.py); these cases load it trusted as a template
on purpose, as ansible-core would read it unmarked, so the pin's encoding alone
keeps a raw value from being evaluated. Rendering needs Ansible's controller
(DataLoader and Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import json
import pathlib

import pytest
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
             if pathlib.PurePosixPath(str(task.get(INCLUDE, {}).get("tasks_from", ""))).stem == "pre_boot"]
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
    playbook = LOADER.load_from_file(str(ROLE.parents[1] / "playbooks/containercluster/install_apply.yml"), trusted_as_template=True)
    play = next(play for play in playbook if play["name"] == "Boot node rounds")
    variables.update(play["vars"])
    variables["bootwright_node_index"] = index
    variables.update({"containercluster_install_agent_node": current, "containercluster_install_agent_index": index})
    return Templar(loader=LOADER, variables=variables)


def material():
    """Two nodes' credentials, and a pin for each, so a node reading another's shows."""
    return {
        "controllerUserNode0": "/run/bootwright/bmc-user-metal-01",
        "controllerPasswordNode0": "/run/bootwright/bmc-password-metal-01",
        "controllerUserNode1": "/run/bootwright/bmc-user-metal-02",
        "controllerPasswordNode1": "/run/bootwright/bmc-password-metal-02",
        "controllerCANode1": "/run/bootwright/bmc-ca-metal-02",
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
    inserts = [index for index, step in enumerate(bounded["block"])
               if step.get("ansible.builtin.include_tasks") == "port.yml"
               and step.get("vars", {}).get("containercluster_install_agent_port_operation") == "media_insert"]
    assert len(inserts) == 1 and bounded["block"].index(task) < inserts[0]
    assert task[INCLUDE]["name"] == "bootwright.core.substrate_machine"
    assert task["vars"]["bootwright_machine_arm"] == "{{ containercluster_install_agent_node.substrate }}"
    dispatcher = LOADER.load_from_file(str(ROLE.parent / "substrate_machine/defaults/main.yml"), trusted_as_template=True)
    assert dispatcher["substrate_machine_arms"][arm] == ARMS[arm]


def test_a_physical_node_is_proved_against_the_nics_its_request_froze(tmp_path):
    task, _parents = proof("baremetal")
    expected = task["vars"]["bootwright_machine_port"]["expected"]
    hardware = {"interfaces": [{"macAddress": MACS[0], "name": "enp2s0"}, {"macAddress": MACS[1], "name": "enp1s0"}]}
    assert rendering(tmp_path, node(hardware=hardware)).template(expected) == MACS
    # A physical node frozen without hardware would prove nothing, so it fails
    # when its proof's input is rendered rather than comparing an empty set.
    assert rendering(tmp_path, node()).template(expected) == []


def test_each_proof_reaches_the_nodes_own_controller_with_its_own_credential(tmp_path):
    hardware = {"interfaces": [{"macAddress": MACS[0], "name": "enp2s0"}]}
    for arm in sorted(ARMS):
        task, _parents = proof(arm)
        scope = rendering(tmp_path, node(arm, hardware), material=material())
        rendered = scope.template(task["vars"]["bootwright_machine_port"])
        assert (rendered["endpoint"], rendered["verify"]) == (ENDPOINT, False), arm
        assert (rendered["user"], rendered["password"]) == (
            "/run/bootwright/bmc-user-metal-02", "/run/bootwright/bmc-password-metal-02"), arm
        assert set(rendered) == {"endpoint", "verify", "user", "password", "ca", "expected",
                                 "pinned_uuid_base64", "pinned_serial_base64", "require_tpm", "firmware", "root_disk", "root_volume"}, arm
        assert rendered["ca"] == "/run/bootwright/bmc-ca-metal-02"


# A node's frozen firmware block reaches its proof whole, as the dict the
# request froze, and a node that declares none hands its proof an empty one
# (specs/substrates.md, Physical firmware).
def test_a_nodes_frozen_firmware_reaches_its_proof_whole(tmp_path):
    task, _parents = proof("baremetal")
    declared = task["vars"]["bootwright_machine_port"]["firmware"]
    firmware = {"mode": "enforce", "secureBoot": True, "bootMode": "UEFI", "biosAttributes": [
        {"name": "AcPwrRcvryUserDelay", "boolean": False}, {"name": "PowerCycleDelay", "integer": 5},
        {"name": "ProcVirtualization", "string": "Enabled"}]}
    frozen = dict(node(), firmware=firmware)
    # JSON compares type as well as value, so False never passes for 0.
    assert json.dumps(rendering(tmp_path, frozen).template(declared), sort_keys=True) == json.dumps(firmware, sort_keys=True)
    assert json.dumps(rendering(tmp_path, node()).template(declared), sort_keys=True) == json.dumps({}, sort_keys=True)


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
    arguments = task["vars"]["bootwright_machine_port"]
    assert scope.template(arguments["pinned_uuid_base64"]) == uuid
    assert scope.template(arguments["pinned_serial_base64"]) == serial


def test_a_nodes_frozen_root_disk_proof_reaches_the_pre_boot_port(tmp_path):
    task, _parents = proof("baremetal")
    expected = task["vars"]["bootwright_machine_port"]["root_disk"]
    disk = {"serialNumber": "SYNTHETIC-ROOT-01", "sizeBytes": 8192}
    assert rendering(tmp_path, node(hardware={"interfaces": [], "rootDisk": disk})).template(expected) == disk
    assert rendering(tmp_path, node()).template(expected) == {}


# The ordering invariant the Unknown resolution reads (specs/openshift-clusters.md,
# Unknown resolution): a node's insert record is written durably after its proof
# passed and before its insert, so before any power request, on every path and
# never excused, so an attempt stopped anywhere after the insert leaves it and
# one whose every proof refused leaves none.
@pytest.mark.parametrize("arm", sorted(ARMS))
def test_each_node_records_its_insert_durably_after_its_proof_and_before_the_insert(tmp_path, arm):
    task, parents = proof(arm)
    steps = parents[-1]["block"]
    records = [index for index, step in enumerate(steps) if "bootwright.core.private_file" in step
               and ".bootwright-inserted-" in str(step["bootwright.core.private_file"]["path"])]
    inserts = [index for index, step in enumerate(steps) if step.get("ansible.builtin.include_tasks") == "port.yml"
               and step.get("vars", {}).get("containercluster_install_agent_port_operation") == "media_insert"]
    boots = [index for index, step in enumerate(steps)
             if pathlib.PurePosixPath(str(step.get(INCLUDE, {}).get("tasks_from", ""))).stem == "boot_media"]
    assert len(records) == len(inserts) == len(boots) == 1
    assert steps.index(task) < records[0] and records[0] + 1 == inserts[0] < boots[0]
    record = steps[records[0]]
    assert not {"when", "failed_when", "ignore_errors", "changed_when", "loop"} & set(record)
    scope = rendering(tmp_path, node(arm))
    scope.available_variables = dict(scope.available_variables, bootwright_cluster_install_request={
        "workRoot": "/var/lib/bootwright-hosted/lab/clusters/sno"})
    path = scope.template(record["bootwright.core.private_file"]["path"])
    assert path == "/var/lib/bootwright-hosted/lab/clusters/sno/.bootwright-inserted-1"


# The mark vouches for the insert records only in the content this build writes,
# and an existing mark is never rewritten, so a mark another build wrote keeps
# reading as an area a node may have been handed the image from.
def test_the_boot_mark_vouches_for_the_insert_records_and_is_never_rewritten():
    from ansible_collections.bootwright.core.plugins.module_utils import containercluster_work_area as rule

    prepare = LOADER.load_from_file(str(ROLE / "tasks" / "prepare_boot.yml"))
    marks = [task["ansible.builtin.copy"] for task in prepare
             if str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith("/.bootwright-booted")]
    assert len(marks) == 1
    assert marks[0]["force"] is False
    assert marks[0]["content"].encode() == rule.VOUCH
