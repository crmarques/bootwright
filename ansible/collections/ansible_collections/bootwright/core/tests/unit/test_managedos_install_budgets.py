"""The managed-OS installation's three install waits are budgets its request froze.

The installer's power-off, the identity answer and the fleet account's
reachability are what an apply waits for while the machine installs and
starts. The run's deadline is derived from those budgets, so one of these
waits the role chose for itself could outlast that deadline and be killed as a
hang, and a budget the role stopped reading would leave the deadline covering
a wait nothing performs. The controller's own power and media polls and an
observation's single reachability retry are not budgets (specs/managed-os.md).
Rendering the task files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()

# The waits the role once chose for itself.
RETIRED = (
    "managedos_install_anaconda_identity_attempts",
    "managedos_install_anaconda_identity_delay",
    "managedos_install_anaconda_reachable_attempts",
    "managedos_install_anaconda_reachable_delay",
)

# The one count the role passes itself: an observation takes the answer that
# is already true, trying the fleet account once more a second later.
OBSERVATION_RETRY = {
    "managedos_install_anaconda_reachable_retries": 1,
    "managedos_install_anaconda_reachable_pause": 1,
}
COUNTS = ("_retries", "_pause", "_delay", "_attempts")

# Distinct values, so each wait is proved to read its own budget.
BUDGETS = {
    "identity": {"attempts": 11, "delaySeconds": 5},
    "installer": {"attempts": 7, "delaySeconds": 3},
    "reachability": {"attempts": 13, "delaySeconds": 2},
}


def trusted(name):
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)
            if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def one(name, predicate):
    found = [task for task in walk(trusted(name)) if predicate(task)]
    assert len(found) == 1, name
    return found[0]


def rendered(value):
    """What one templated value is for a request freezing BUDGETS."""
    scope = {"bootwright_os_install_request": {"budgets": BUDGETS}}
    return int(Templar(loader=LOADER, variables=scope).template(value))


def test_the_role_defines_none_of_the_waits_it_once_chose():
    for path in sorted(ROLE.rglob("*.yml")):
        text = path.read_text()
        for name in RETIRED:
            assert name not in text, "%s still names %s" % (path.relative_to(ROLE), name)


def test_the_installer_wait_reads_its_frozen_budget():
    wait = one("await.yml", lambda task: task.get("register") == "managedos_install_anaconda_installer")
    assert rendered(wait["retries"]) == BUDGETS["installer"]["attempts"]
    assert rendered(wait["delay"]) == BUDGETS["installer"]["delaySeconds"]


def test_the_identity_wait_reads_its_frozen_budget():
    wait = one("identity_until_answered.yml", lambda task: task.get("ansible.builtin.include_tasks") == "identity.yml")
    assert rendered(wait["vars"]["managedos_install_anaconda_identity_retries"]) == BUDGETS["identity"]["attempts"]
    for role in ("substrate_libvirt_machine", "substrate_baremetal_machine"):
        read = one("identity.yml", lambda task, role=role: (task.get("ansible.builtin.include_role") or {}).get(
            "name") == "bootwright.core." + role)
        assert rendered(read["vars"][role + "_identity_delay"]) == BUDGETS["identity"]["delaySeconds"]


def test_the_reachability_wait_reads_its_frozen_budget():
    wait = one("verify.yml", lambda task: task.get("ansible.builtin.include_tasks") == "reachable.yml")
    assert rendered(wait["vars"]["managedos_install_anaconda_reachable_retries"]) == BUDGETS["reachability"]["attempts"]
    assert rendered(wait["vars"]["managedos_install_anaconda_reachable_pause"]) == BUDGETS["reachability"]["delaySeconds"]


@pytest.mark.parametrize("name", sorted(path.name for path in (ROLE / "tasks").glob("*.yml")))
def test_no_task_waits_for_a_count_of_its_own(name):
    for task in walk(trusted(name)):
        for key in ("retries", "delay"):
            if key in task:
                assert isinstance(task[key], str) and "{{" in task[key], "%s: %s %r" % (name, key, task.get("name"))


@pytest.mark.parametrize("name", sorted(path.name for path in (ROLE / "tasks").glob("*.yml")))
def test_no_include_passes_a_count_of_its_own(name):
    observed = 0
    for task in walk(trusted(name)):
        counts = {key: value for key, value in (task.get("vars") or {}).items() if key.endswith(COUNTS)}
        if name == "observe.yml" and task.get("ansible.builtin.include_tasks") == "reachable.yml":
            assert counts == OBSERVATION_RETRY, counts
            observed += 1
            continue
        for key, value in counts.items():
            assert isinstance(value, str) and "{{" in value, "%s: %s %r" % (name, key, task.get("name"))
    assert observed == (1 if name == "observe.yml" else 0), name


def test_every_entry_point_requires_every_budget():
    specs = LOADER.load_from_file(str(ROLE / "meta" / "argument_specs.yml"))["argument_specs"]
    assert set(specs) == {"apply", "observe", "destroy"}
    for entry, spec in specs.items():
        budgets = spec["options"]["bootwright_os_install_request"]["options"]["budgets"]
        assert budgets["type"] == "dict" and budgets["required"] is True, entry
        assert set(budgets["options"]) == set(BUDGETS), entry
        for name, budget in budgets["options"].items():
            assert budget["required"] is True, (entry, name)
            assert {field: (option["type"], option["required"]) for field, option in budget["options"].items()} == {
                "attempts": ("int", True), "delaySeconds": ("int", True)}, (entry, name)
