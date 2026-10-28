"""A refused pre-boot proof reaches the operator as the installation's own diagnostic.

Each machine role's pre_boot entry point (roles/substrate_*_machine/tasks/
pre_boot.yml) proves its target immediately before media is inserted. The
result channel belongs to the installation that composed it, so the proof names
the refusal each check makes before that check runs, in a fact of its own
role, and clears it first and once the proof holds. The substrate's dispatcher
republishes the dispatched arm's fact under its own name, also when the proof
failed, and the managed-OS apply and the cluster boot name that fact to their
runner through their own protocol in
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
import json
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.plugins.loader import init_plugin_loader
from ansible.template import Templar
from ansible.utils.collection_loader import AnsibleCollectionConfig

from ansible_collections.bootwright.core.plugins.action import containercluster_install_protocol as cluster_protocol
from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as managedos_protocol

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()

ASSERT = "ansible.builtin.assert"
FAIL = "ansible.builtin.fail"
SET_FACT = "ansible.builtin.set_fact"
INCLUDE_TASKS = "ansible.builtin.include_tasks"
FIRMWARE = "bootwright.core.redfish_firmware"
FIRMWARE_RESULT = "substrate_baremetal_machine_firmware"
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


def proof(arm, name="pre_boot.yml"):
    return loaded(ROLES / ("substrate_%s_machine" % arm) / "tasks" / name)


def expanded(arm, name="pre_boot.yml"):
    """A proof's tasks in order, each included task file in place of its include."""
    for task in proof(arm, name):
        if INCLUDE_TASKS in task:
            yield {"include": task[INCLUDE_TASKS], "when": task.get("when", True), "name": task["name"]}
            yield from expanded(arm, task[INCLUDE_TASKS])
            yield {"included": task[INCLUDE_TASKS]}
        else:
            yield task


def named(arm):
    """Every value the proof of one arm sets its refusal fact to, in order, each
    included file's as often as it is included."""
    return [task[SET_FACT][FACTS[arm]] for task in expanded(arm) if FACTS[arm] in (task.get(SET_FACT) or {})]


def holds(rendering, when):
    return all(rendering.evaluate_conditional(item) for item in (when if isinstance(when, list) else [when]))


def prove(arm, registered, firmware=None, reads=None, **variables):
    """Runs one proof's facts and checks in order, as a play does, over what its
    reads registered: the facts it left, whether every check held, and the
    task files it included. Each target read registers the next of `reads`,
    or `registered` when none are given; the firmware step registers
    `firmware`."""
    facts, skipping, ran = {}, [], []
    reads = list(reads or [registered])
    results = {REGISTERED[arm]: reads[0]}
    for task in expanded(arm):
        if "included" in task:
            if skipping and skipping[-1] == task["included"]:
                skipping.pop()
            continue
        if skipping:
            continue
        rendering = Templar(loader=LOADER, variables=dict(variables, **facts, **results))
        if "include" in task:
            if holds(rendering, task["when"]):
                ran.append(task["include"])
            else:
                skipping.append(task["include"])
            continue
        assert "loop" not in task, task["name"]
        if not holds(rendering, task.get("when", True)):
            continue
        if SET_FACT in task:
            facts.update({name: rendering.template(value) for name, value in task[SET_FACT].items()})
        elif ASSERT in task:
            if not all(rendering.evaluate_conditional(item) for item in task[ASSERT]["that"]):
                return facts, False, ran
        elif FIRMWARE in task:
            assert task["register"] == FIRMWARE_RESULT and task["no_log"] is True, task["name"]
            results[FIRMWARE_RESULT] = firmware
        else:
            assert READS[arm] in task and task["register"] == REGISTERED[arm], task["name"]
            results[REGISTERED[arm]] = reads.pop(0) if len(reads) > 1 else reads[0]
    return facts, True, ran


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
    "substrate_baremetal_machine_target_firmware": {},
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
    facts, held, _ran = prove("baremetal", registered, **PHYSICAL)
    assert held is (refusal is None)
    assert facts[FACTS["baremetal"]] == (refusal or "")


EMULATED = {
    "a machine that is off": ({"changed": False, "power": "Off"}, None),
    "a controller that did not answer": ({"changed": False, "failed": False, "msg": "HTTP 401", "power": ""}, ""),
    "a machine that is running": ({"changed": False, "power": "On"}, "machine-running"),
}


@pytest.mark.parametrize("registered, refusal", EMULATED.values(), ids=EMULATED.keys())
def test_a_proof_of_a_machine_its_substrate_created_names_only_a_running_one(registered, refusal):
    facts, held, _ran = prove("libvirt", registered, **{FACTS["libvirt"]: "machine-running"})
    assert held is (refusal is None)
    assert facts[FACTS["libvirt"]] == (refusal or "")


# Each proof clears its fact before its read and once it holds, and names each
# of its refusals once. Go gives each exactly these reasons a diagnostic
# (TestEachArmsPreBootEntryPointNamesExactlyTheseRefusals), and the protocol of
# each consumer names exactly the union.
def test_each_proof_names_each_refusal_once_and_clears_it_around_its_checks():
    target = ["hardware-mismatch", "identity-mismatch", "machine-running", ""]
    firmware = ["firmware-setting-mismatch", "firmware-setting-not-applied", ""]
    assert named("baremetal") == [""] + target + firmware + target + ["tpm-absent", ""]
    assert named("libvirt") == ["", "machine-running", ""]
    union = set(named("baremetal")) | set(named("libvirt"))
    lifecycle = {
        managedos_protocol: {"foreign-installation"},
        cluster_protocol: {"media-missing", "private-delivery", "controller-read", "media-insert", "media-eject", "node-unreachable"},
    }
    for protocol in (managedos_protocol, cluster_protocol):
        assert set(protocol.REFUSALS) == (union - {""}) | lifecycle[protocol]
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


@pytest.fixture(name="plugins")
def plugins_fixture():
    """The plugin loader a rendered lookup needs."""
    if AnsibleCollectionConfig.collection_finder is None:
        init_plugin_loader()


def published(arm, facts):
    """What the dispatcher republishes once the arm's entry point ran or failed,
    or nothing when no entry point was reached."""
    if not facts:
        return {}
    dispatch = loaded(ROLES / "substrate_machine" / "tasks" / "dispatch.yml")
    [republish] = [task for task, _parents in walk(dispatch) if SET_FACT in task]
    arms = LOADER.load_from_file(str(ROLES / "substrate_machine" / "defaults" / "main.yml"))["substrate_machine_arms"]
    scope = dict(facts, substrate_machine_arms=arms, bootwright_machine_arm=arm)
    scope["substrate_machine_prefix"] = Templar(loader=LOADER, variables=scope).template(republish["vars"]["substrate_machine_prefix"])
    refusal = Templar(loader=LOADER, variables=scope).template(republish[SET_FACT]["substrate_machine_refusal"])
    return {"substrate_machine_refusal": refusal}


def managed_os():
    tasks = loaded(ROLES / "managedos_install_anaconda" / "tasks" / "apply.yml")
    rescue = rescue_of(tasks, lambda task: task.get("ansible.builtin.include_tasks") == "boot.yml", "include boot.yml")
    return naming(rescue, MANAGED_OS_PROTOCOL)


def cluster():
    tasks = loaded(ROLES / "containercluster_install_agent" / "tasks" / "boot.yml")
    rescue = rescue_of(tasks, lambda task: (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "pre_boot",
                       "include the pre-boot proof")
    assert FAIL in rescue[-1] and "when" not in rescue[-1]
    return next(task for task in rescue if "containercluster_install_agent_lifecycle_refusal" in task.get(SET_FACT, {}))


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
def test_the_managed_os_apply_names_the_refusal_its_targets_proof_left(plugins, arm, facts, refusal):
    variables = dict(published(arm, facts), bootwright_os_install_request={"identity": {"object": "rhel-01"}, "target": {"substrate": arm}})
    found = names(managed_os(), MANAGED_OS_PROTOCOL, variables)
    assert found == (None if refusal is None else {"phase": "refused", "reason": refusal})


@pytest.mark.parametrize("arm, facts, refusal", LEFT.values(), ids=LEFT.keys())
def test_the_cluster_boot_names_the_refusal_under_the_nodes_position(plugins, arm, facts, refusal):
    variables = dict(published(arm, facts), containercluster_install_agent_index=2,
                     containercluster_install_agent_node={"machine": "metal-03", "substrate": arm})
    kept = names(cluster(), SET_FACT, variables)
    phase = loaded(ROLES / "containercluster_install_agent/tasks/phase_failures.yml")
    naming_task = next(task for task in phase if CLUSTER_PROTOCOL in task)
    host = {"bootwright_node_index": 2, **(kept or {})}
    found = names(naming_task, CLUSTER_PROTOCOL, {"phase_host": "node2", "hostvars": {"node2": host}})
    assert found == (None if refusal is None else {"phase": "refused", "reason": refusal, "node": 2})


@pytest.mark.parametrize("usable,held", [(False, False), (True, True)])
def test_encrypted_physical_installation_requires_positive_tpm_proof(usable, held):
    result = inspected()
    result["tpm_usable"] = usable
    facts, accepted, _ran = prove("baremetal", result, **PHYSICAL, substrate_baremetal_machine_target_require_tpm=True)
    assert accepted is held
    assert facts[FACTS["baremetal"]] == ("" if held else "tpm-absent")


def physical(firmware):
    """The physical proof's scope with this firmware declaration."""
    variables = dict(PHYSICAL)
    variables["substrate_baremetal_machine_target_firmware"] = firmware
    return variables


def firmware_result(refusal="", changed=False, msg=None):
    """What redfish_firmware registers: data, or a failure that carries a message."""
    result = {"changed": changed, "refusal": refusal, "fields": ["firmware.secureBoot"] if refusal else [],
              "reasons": [], "messages": ["Base.1.8.PropertyModificationNeedPrivilege"] if refusal else [],
              "power": "Off"}
    if msg is not None:
        result = {"changed": False, "failed": True, "msg": msg}
    return result


# The firmware step runs only for a declaration, between the two target
# proofs; it names its mode's refusal only once the controller answered, and
# the TPM check reads the target proof made after it.
FIRMWARE_CASES = {
    "nothing declared": ({}, None, None, ["target_proof.yml"]),
    "proved as declared": ({"mode": "prove", "secureBoot": True}, firmware_result(), None,
                           ["target_proof.yml", "firmware.yml"]),
    "a proof that found differences": ({"mode": "prove", "secureBoot": True},
                                       firmware_result("firmware-setting-mismatch"), "firmware-setting-mismatch",
                                       ["target_proof.yml", "firmware.yml"]),
    "applied": ({"mode": "enforce", "secureBoot": True}, firmware_result(changed=True), None,
                ["target_proof.yml", "firmware.yml", "target_proof.yml"]),
    "not applied": ({"secureBoot": True}, firmware_result("firmware-setting-not-applied", changed=True),
                    "firmware-setting-not-applied", ["target_proof.yml", "firmware.yml"]),
    "a controller that did not answer": ({"secureBoot": True}, firmware_result(msg="reading: HTTP 503"), "",
                                         ["target_proof.yml", "firmware.yml"]),
}


@pytest.mark.parametrize("declared, result, refusal, included", FIRMWARE_CASES.values(), ids=FIRMWARE_CASES.keys())
def test_the_firmware_step_names_its_modes_refusal_between_two_target_proofs(declared, result, refusal, included):
    facts, held, ran = prove("baremetal", inspected(), firmware=result,
                             **physical(declared))
    assert ran == included
    assert held is (refusal is None)
    assert facts[FACTS["baremetal"]] == (refusal or "")


def test_the_tpm_check_reads_the_proof_made_after_a_firmware_change():
    before, after = dict(inspected(), tpm_usable=False), dict(inspected(), tpm_usable=True)
    facts, held, ran = prove("baremetal", before, firmware=firmware_result(changed=True), reads=[before, after],
                             **physical({"biosAttributes": [
                                 {"name": "TpmEnable", "boolean": True}]}),
                             substrate_baremetal_machine_target_require_tpm=True)
    assert ran == ["target_proof.yml", "firmware.yml", "target_proof.yml"]
    assert held is True and facts[FACTS["baremetal"]] == ""


def test_a_machine_left_running_by_the_firmware_step_is_refused_by_the_second_proof():
    facts, held, _ran = prove("baremetal", inspected(), firmware=firmware_result(changed=True),
                              reads=[inspected(), inspected(power="On")],
                              **physical({"secureBoot": True}))
    assert held is False and facts[FACTS["baremetal"]] == "machine-running"


def test_the_firmware_call_is_hidden_and_its_refusal_reported_beside_it():
    tasks = proof("baremetal", "firmware.yml")
    [call] = [task for task in tasks if FIRMWARE in task]
    assert call["no_log"] is True and call["failed_when"] is False
    following = tasks[tasks.index(call) + 1]
    assert ASSERT in following and "no_log" not in following
    assert FIRMWARE_RESULT + ".msg" in json.dumps(following[ASSERT])
    assert call[FIRMWARE]["firmware"] == "{{ substrate_baremetal_machine_target_firmware }}"


@pytest.mark.parametrize("declared, held", [({}, True), ({"secureBoot": True}, False)])
def test_a_machine_its_substrate_created_refuses_a_firmware_declaration(declared, held):
    facts, accepted, _ran = prove("libvirt", {"changed": False, "power": "Off"},
                                  bootwright_machine_port={"firmware": declared})
    assert accepted is held
