"""A refused pre-boot proof reaches the operator as the installation's own diagnostic.

Each machine role's pre_boot entry point (roles/substrate_*_machine/tasks/
pre_boot.yml) proves its target immediately before media is inserted. The
result channel belongs to the installation that composed it, so the proof names
the refusal each check makes before that check runs, in a fact of its own
role, and clears it first and once the proof holds. The managed-OS apply and
the cluster boot name that fact to their runner through their own protocol in
the rescue that keeps the failure terminal, a node's under its position in the
frozen order, and the runner reports the diagnostic Go gave that reason
(internal/substrate/preboot.go, specs/substrates.md) instead of the adapter's
failure. A read the controller did not answer names nothing: its own message,
in the retained output, is the reason.

These cases run each proof's facts and checks in order over what its read
registered, evaluating the real expressions, and render each consumer's naming
over the facts a proof left. Rendering needs Ansible's controller (DataLoader
and Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import containercluster_install_protocol as cluster_protocol
from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as managedos_protocol

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()

ASSERT = "ansible.builtin.assert"
FAIL = "ansible.builtin.fail"
SET_FACT = "ansible.builtin.set_fact"
READS = {"baremetal": "bootwright.core.redfish_system_inspect", "libvirt": "bootwright.core.redfish_system_read"}
FACTS = {arm: "substrate_%s_machine_refusal" % arm for arm in READS}
REGISTERED = {arm: "substrate_%s_machine_target" % arm for arm in READS}
MANAGED_OS_PROTOCOL = "bootwright.core.managedos_install_protocol"
CLUSTER_PROTOCOL = "bootwright.core.containercluster_install_protocol"

ENDPOINT = "https://metal-bmc.lab.example.test/redfish/v1/Systems/1"
UUID = "4c4c4544-0042-3510-8052-b4c04f4d4e31"
SERIAL = "CZJ2440ABC"
MACS = ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"]


def loaded(path):
    """A task file as a play loads it: its templates trusted."""
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks, parents=()):
    for task in tasks:
        yield task, parents
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)], parents + (task,))


def proof(arm):
    return loaded(ROLES / ("substrate_%s_machine" % arm) / "tasks" / "pre_boot.yml")


def named(arm):
    """Every value the proof of one arm sets its refusal fact to, in order."""
    return [task[SET_FACT][FACTS[arm]] for task in proof(arm) if FACTS[arm] in (task.get(SET_FACT) or {})]


def prove(arm, registered, **variables):
    """Runs one proof's facts and checks in order, as a play does, over what its
    read registered: the facts it left, and whether every check held."""
    facts = {}
    for task in proof(arm):
        assert "when" not in task and "loop" not in task, task["name"]
        rendering = Templar(loader=LOADER, variables=dict(variables, **facts, **{REGISTERED[arm]: registered}))
        if SET_FACT in task:
            facts.update({name: rendering.template(value) for name, value in task[SET_FACT].items()})
        elif ASSERT in task:
            if not all(rendering.evaluate_conditional(item) for item in task[ASSERT]["that"]):
                return facts, False
        else:
            assert READS[arm] in task and task["register"] == REGISTERED[arm], task["name"]
    return facts, True


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def inspected(**overrides):
    """What redfish_system_inspect reports for the declared, powered-off machine."""
    observation = {"addresses": list(MACS), "failures": [], "manufacturer": "Dell Inc.", "model": "PowerEdge R740",
                   "power": "Off", "serial": SERIAL, "uuid": UUID}
    observation.update(overrides)
    return {"changed": False, "observation": observation}


PHYSICAL = {
    "substrate_baremetal_machine_target_endpoint": ENDPOINT,
    "substrate_baremetal_machine_target_expected": MACS,
    "substrate_baremetal_machine_target_pinned_uuid_base64": encoded(UUID),
    "substrate_baremetal_machine_target_pinned_serial_base64": encoded(SERIAL),
    # What a node proved before this one left; every proof starts from none.
    FACTS["baremetal"]: "identity-mismatch",
}

UNANSWERED = dict(inspected(power="", serial="", uuid="", addresses=[]), msg="reading the system failed: HTTP 503")
PROVED = {
    "the declared machine, off": (inspected(), None),
    "a controller that did not answer": (UNANSWERED, ""),
    "a declared address the machine does not report": (inspected(addresses=MACS[:1]), "hardware-mismatch"),
    "an inventory read only in part": (inspected(failures=["EthernetInterfaces/2"]), "hardware-mismatch"),
    "another system at the controller": (inspected(uuid="4c4c4544-0042-3510-8052-b4c04f4d4e32"), "identity-mismatch"),
    "another serial at the controller": (inspected(serial="CZJ2440ABD"), "identity-mismatch"),
    "a machine that is running": (inspected(power="On"), "machine-running"),
}


@pytest.mark.parametrize("registered, refusal", PROVED.values(), ids=PROVED.keys())
def test_a_physical_proof_names_the_refusal_its_failed_check_makes(registered, refusal):
    facts, held = prove("baremetal", registered, **PHYSICAL)
    assert held is (refusal is None)
    assert facts[FACTS["baremetal"]] == (refusal or "")


EMULATED = {
    "a machine that is off": ({"changed": False, "power": "Off"}, None),
    "a controller that did not answer": ({"changed": False, "failed": False, "msg": "HTTP 401", "power": ""}, ""),
    "a machine that is running": ({"changed": False, "power": "On"}, "machine-running"),
}


@pytest.mark.parametrize("registered, refusal", EMULATED.values(), ids=EMULATED.keys())
def test_a_proof_of_a_machine_its_substrate_created_names_only_a_running_one(registered, refusal):
    facts, held = prove("libvirt", registered, **{FACTS["libvirt"]: "machine-running"})
    assert held is (refusal is None)
    assert facts[FACTS["libvirt"]] == (refusal or "")


# Each proof clears its fact before its read and once it holds, and names each
# of its refusals once. Go gives each exactly these reasons a diagnostic
# (TestEachArmsPreBootEntryPointNamesExactlyTheseRefusals), and the protocol of
# each consumer names exactly the union.
def test_each_proof_names_each_refusal_once_and_clears_it_around_its_checks():
    assert named("baremetal") == ["", "hardware-mismatch", "identity-mismatch", "machine-running", ""]
    assert named("libvirt") == ["", "machine-running", ""]
    union = set(named("baremetal")) | set(named("libvirt"))
    for protocol in (managedos_protocol, cluster_protocol):
        assert set(protocol.REFUSALS) == union - {""}
        assert "refused" in protocol.PHASES


def rescue_of(tasks, predicate, description):
    """The rescue of the one block around the task a predicate finds."""
    found = [parents for task, parents in walk(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    blocks = [parent for parent in found[0] if parent.get("rescue")]
    return blocks[-1]["rescue"]


def naming(rescue, protocol):
    """The rescue's naming of the refusal, and the fail that ends it."""
    assert len([task for task in rescue if protocol in task]) == 1
    task = [task for task in rescue if protocol in task][0]
    assert task[protocol]["phase"] == "refused" and task["no_log"] is True
    assert rescue.index(task) == len(rescue) - 2
    kept = rescue[-1]
    assert FAIL in kept and not {"when", "ignore_errors", "failed_when"} & set(kept)
    return task


def names(task, protocol, variables):
    """What the naming names in this scope: its arguments, or None when its
    condition leaves it out. Task variables are rendered first, as a play does."""
    rendering = Templar(loader=LOADER, variables=variables)
    scoped = dict(variables, **{name: rendering.template(value) for name, value in (task.get("vars") or {}).items()})
    rendering = Templar(loader=LOADER, variables=scoped)
    if not rendering.evaluate_conditional(task["when"]):
        return None
    return {name: rendering.template(value) for name, value in task[protocol].items()}


def managed_os():
    tasks = loaded(ROLES / "managedos_install_anaconda" / "tasks" / "apply.yml")
    rescue = rescue_of(tasks, lambda task: task.get("ansible.builtin.include_tasks") == "boot.yml", "include boot.yml")
    return naming(rescue, MANAGED_OS_PROTOCOL)


def cluster():
    tasks = loaded(ROLES / "containercluster_install_agent" / "tasks" / "boot.yml")
    rescue = rescue_of(tasks, lambda task: (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "pre_boot"
                       and task["ansible.builtin.include_role"]["name"].endswith("baremetal_machine"),
                       "include the bare-metal proof")
    return naming(rescue, CLUSTER_PROTOCOL)


# Only the fact of the target's own substrate is read, a fact the proof cleared
# names nothing, and neither does one no proof ever set, as after a failure
# before the proof ran.
LEFT = {
    "a running machine its substrate created": ("libvirt", {FACTS["libvirt"]: "machine-running"}, "machine-running"),
    "a physical machine answering as another": ("baremetal", {FACTS["baremetal"]: "identity-mismatch"},
                                                "identity-mismatch"),
    "a proof that held": ("baremetal", {FACTS["baremetal"]: ""}, None),
    "a controller that did not answer": ("libvirt", {FACTS["libvirt"]: ""}, None),
    "the other substrate's fact": ("libvirt", {FACTS["baremetal"]: "machine-running"}, None),
    "a failure before any proof": ("baremetal", {}, None),
}


@pytest.mark.parametrize("arm, facts, refusal", LEFT.values(), ids=LEFT.keys())
def test_the_managed_os_apply_names_the_refusal_its_targets_proof_left(arm, facts, refusal):
    variables = dict(facts, bootwright_os_install_request={"identity": {"object": "rhel-01"}, "target": {"substrate": arm}})
    found = names(managed_os(), MANAGED_OS_PROTOCOL, variables)
    assert found == (None if refusal is None else {"phase": "refused", "reason": refusal})


@pytest.mark.parametrize("arm, facts, refusal", LEFT.values(), ids=LEFT.keys())
def test_the_cluster_boot_names_the_refusal_under_the_nodes_position(arm, facts, refusal):
    variables = dict(facts, containercluster_install_agent_index=2,
                     containercluster_install_agent_node={"machine": "metal-03", "substrate": arm})
    found = names(cluster(), CLUSTER_PROTOCOL, variables)
    assert found == (None if refusal is None else {"phase": "refused", "reason": refusal, "node": 2})
