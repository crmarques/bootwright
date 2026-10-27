"""One substrate-owned dispatcher answers every consumer of the machine port.

Consumers include bootwright.core.substrate_machine by an entry point key and
hand it the arm Go froze into their request; only the dispatcher's table names
an arm's role (specs/architecture.md, Roles). An arm the table does not list
ends in a named refusal before any lookup by it, and an arm added to the table
reaches every consumer, machine power included, through its own entry points.
Rendering needs Ansible's controller (DataLoader and Templar), which
ansible-test does not offer to unit tests under tests/unit/plugins, so these
checks live here.
"""

from __future__ import annotations

import pathlib
import re

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.plugins.loader import init_plugin_loader
from ansible.template import Templar
from ansible.utils.collection_loader import AnsibleCollectionConfig

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLES = COLLECTION / "roles"
DISPATCHER = ROLES / "substrate_machine"
LOADER = DataLoader()

ARM_ROLE = re.compile(r"\bsubstrate_[a-z0-9]+_machine\b")
PORT = ("pre_boot", "boot_media", "boot_disk", "identity_read", "state_read", "media_insert", "media_eject", "power")
INCLUDE = "ansible.builtin.include_role"
FAIL = "ansible.builtin.fail"
SET_FACT = "ansible.builtin.set_fact"
SYNTHETIC = "kubevirt"


@pytest.fixture(name="plugins")
def plugins_fixture():
    """The plugin loader a rendered collection filter needs."""
    if AnsibleCollectionConfig.collection_finder is None:
        init_plugin_loader()


def loaded(path):
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def table():
    return dict(LOADER.load_from_file(str(DISPATCHER / "defaults" / "main.yml"))["substrate_machine_arms"])


def specs(role):
    return LOADER.load_from_file(str(ROLES / role / "meta" / "argument_specs.yml"))["argument_specs"]


def test_no_role_outside_the_substrate_names_an_arms_machine_role():
    found = []
    for path in sorted(ROLES.glob("*/**/*.yml")):
        role = path.relative_to(ROLES).parts[0]
        if role.startswith("substrate_"):
            continue
        found.extend("%s names %s" % (path.relative_to(ROLES), name) for name in ARM_ROLE.findall(path.read_text()))
    for path in sorted((COLLECTION / "playbooks").glob("**/*.yml")):
        if path.relative_to(COLLECTION / "playbooks").parts[0] != "substrate":
            found.extend("%s names %s" % (path.relative_to(COLLECTION), name) for name in ARM_ROLE.findall(path.read_text()))
    assert not found, "\n".join(found)


def test_the_rule_sees_a_seeded_arm_role():
    assert ARM_ROLE.findall("name: bootwright.core.substrate_libvirt_machine") == ["substrate_libvirt_machine"]
    assert not ARM_ROLE.findall("name: bootwright.core.substrate_machine")


def test_the_table_binds_each_arm_to_its_own_machine_role_and_every_port_entry_point():
    arms = table()
    assert arms, "the dispatcher binds no arm"
    for arm, role in arms.items():
        assert role == "bootwright.core.substrate_%s_machine" % arm
        declared = specs(role.split(".")[-1])
        assert set(PORT) <= set(declared), arm


def test_the_dispatchers_argument_specs_carry_no_arm_choices_so_its_named_fail_refuses():
    declared = specs("substrate_machine")
    assert set(PORT) | {"admit"} == set(declared)
    for entry, spec in declared.items():
        arm = spec["options"]["bootwright_machine_arm"]
        assert arm["type"] == "str" and arm["required"] is True, entry
        assert "choices" not in arm, entry


def test_every_arm_choice_is_exactly_the_dispatch_table():
    arms = sorted(table())
    power = specs("machine_power")["power"]["options"]["bootwright_machine_power_request"]["options"]["arm"]
    assert sorted(power["choices"]) == arms and power["required"] is True
    read = specs("machine_power_read")["read"]["options"]["bootwright_machine_power_read_request"]["options"]
    target = read["targets"]["options"]["arm"]
    assert sorted(target["choices"]) == arms and target["required"] is True


@pytest.mark.parametrize("entry", PORT)
def test_each_entry_point_dispatches_under_its_own_key(entry):
    [task] = loaded(DISPATCHER / "tasks" / (entry + ".yml"))
    assert task["ansible.builtin.include_tasks"] == "dispatch.yml"
    assert task["vars"] == {"substrate_machine_entry": entry}


def dispatch():
    tasks = loaded(DISPATCHER / "tasks" / "dispatch.yml")
    refusal = next(index for index, task in enumerate(tasks) if FAIL in task)
    lookup = next(index for index, task in enumerate(tasks) if any(INCLUDE in child for child in walk([task])))
    return tasks, refusal, lookup


def scope(arm, arms=None, entry="power"):
    return {"substrate_machine_arms": table() if arms is None else arms, "bootwright_machine_arm": arm,
            "substrate_machine_entry": entry}


def test_an_unlisted_arm_is_refused_by_name_before_any_lookup(plugins):
    tasks, refusal, lookup = dispatch()
    assert refusal < lookup
    task = tasks[refusal]
    assert not {"failed_when", "ignore_errors", "rescue"} & set(task)
    rendering = Templar(loader=LOADER, variables=scope(SYNTHETIC))
    assert rendering.evaluate_conditional(task["when"]) is True
    assert " ".join(rendering.template(task[FAIL]["msg"]).split()) == (
        "the substrate has no machine port for arm 'kubevirt', so its power entry point did not run; "
        "plan the installation again with this release")
    for arm in table():
        assert Templar(loader=LOADER, variables=scope(arm)).evaluate_conditional(task["when"]) is False


def test_power_refuses_an_unlisted_arm_by_name_before_its_first_port_entry_point(plugins):
    power = loaded(ROLES / "machine_power" / "tasks" / "power.yml")
    entries = [(index, task[INCLUDE]) for index, task in enumerate(power) if INCLUDE in task]
    admit = [index for index, include in entries if include == {"name": "bootwright.core.substrate_machine", "tasks_from": "admit"}]
    port = [index for index, include in entries if include.get("tasks_from") in PORT]
    assert len(admit) == 1 and port and admit[0] < min(port)
    arm = power[admit[0]]["vars"]["bootwright_machine_arm"]
    request = {"bootwright_machine_power_request": {"arm": SYNTHETIC}}
    [refusal] = loaded(DISPATCHER / "tasks" / "admit.yml")
    rendering = Templar(loader=LOADER, variables=dict(request, substrate_machine_arms=table(),
                                                      bootwright_machine_arm=Templar(loader=LOADER, variables=request).template(arm)))
    assert rendering.evaluate_conditional(refusal["when"]) is True
    assert " ".join(rendering.template(refusal[FAIL]["msg"]).split()) == (
        "the substrate has no machine port for arm 'kubevirt', so nothing was done; plan the installation again "
        "with this release")


# A synthetic arm added to the table alone, with no consumer edit, is reached
# by machine start and stop through its own power entry point and answers
# under the substrate's names.
@pytest.mark.parametrize("verb", ["start", "stop"])
def test_a_synthetic_arm_reaches_machine_power_through_its_own_entry_points(plugins, verb):
    driven = loaded(ROLES / "machine_power" / "tasks" / (verb + ".yml"))
    [include] = [task for task in driven if INCLUDE in task]
    assert include[INCLUDE] == {"name": "bootwright.core.substrate_machine", "tasks_from": "power"}
    request = {"bootwright_machine_power_request": {"arm": SYNTHETIC}}
    arm = Templar(loader=LOADER, variables=request).template(include["vars"]["bootwright_machine_arm"])
    arms = dict(table(), **{SYNTHETIC: "bootwright.core.substrate_kubevirt_machine"})
    tasks, refusal, lookup = dispatch()
    rendering = Templar(loader=LOADER, variables=scope(arm, arms))
    assert rendering.evaluate_conditional(tasks[refusal]["when"]) is False
    [entered] = [task for task in walk([tasks[lookup]]) if INCLUDE in task]
    assert rendering.template(entered[INCLUDE]["name"]) == "bootwright.core.substrate_kubevirt_machine"
    assert rendering.template(entered[INCLUDE]["tasks_from"]) == "power"
    [published] = [task for task in walk([tasks[lookup]]) if SET_FACT in task]
    assert rendering.template(published["vars"]["substrate_machine_prefix"]) == "substrate_kubevirt_machine"
    answered = dict(scope(arm, arms), substrate_machine_prefix="substrate_kubevirt_machine",
                    substrate_kubevirt_machine_port_result={"power": "Off"})
    answer = Templar(loader=LOADER, variables=answered).template(published[SET_FACT]["substrate_machine_port_result"])
    assert answer == {"power": "Off"}
