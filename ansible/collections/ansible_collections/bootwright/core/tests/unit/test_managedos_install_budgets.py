"""The managed-OS installation's six waits are budgets its request froze.

The installer's power-off, the identity answer, the fleet account's
reachability, the network's apply and reconnection, and each wait of the
restart that proves it are what an apply waits for while the machine installs,
starts and converges its network. The run's deadline is derived from those budgets, so one of these
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
# The other: after the update's reboot answered on its key, the marker is read
# again through the identity channel with one retry, a read the update bound
# allows rather than the identity budget an installation waits.
UPDATE_RETRY = {"managedos_install_anaconda_identity_retries": 1}
COUNTS = ("_retries", "_pause", "_delay", "_attempts")

# Distinct values, so each wait is proved to read its own budget.
BUDGETS = {
    "identity": {"attempts": 11, "delaySeconds": 5},
    "installer": {"attempts": 7, "delaySeconds": 3},
    "networkApply": {"attempts": 17, "delaySeconds": 4},
    "networkReconnect": {"attempts": 19, "delaySeconds": 6},
    "reachability": {"attempts": 13, "delaySeconds": 2},
    "reboot": {"attempts": 23, "delaySeconds": 7},
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


def reboot_defaults():
    """The reboot waits' defaults, which read the frozen reboot budget unless the caller narrows them."""
    defaults = LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True)
    return {name: value for name, value in defaults.items() if name.startswith("managedos_install_anaconda_reboot_")}


def port_wait(name, budgets):
    task = one(name, lambda task: (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "state_read")
    scope = dict(reboot_defaults(), bootwright_os_install_request={"budgets": budgets}, managedos_install_anaconda_port_access={})
    return Templar(loader=LOADER, variables=scope).template(task["vars"]["bootwright_machine_port"])


def test_the_installer_wait_reads_its_frozen_budget():
    wait = port_wait("await.yml", BUDGETS)
    assert wait["expected_power"] == "Off"
    assert wait["retries"] == BUDGETS["installer"]["attempts"]
    assert wait["delay"] == BUDGETS["installer"]["delaySeconds"]


def test_the_identity_wait_reads_its_frozen_budget():
    wait = one("identity_until_answered.yml", lambda task: task.get("ansible.builtin.include_tasks") == "identity.yml")
    assert rendered(wait["vars"]["managedos_install_anaconda_identity_retries"]) == BUDGETS["identity"]["attempts"]
    read = one("identity.yml", lambda task: (task.get("ansible.builtin.include_role") or {}).get(
        "name") == "bootwright.core.substrate_machine")
    assert rendered(read["vars"]["bootwright_machine_identity"]["delay"]) == BUDGETS["identity"]["delaySeconds"]


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
        if name == "update.yml" and task.get("ansible.builtin.include_tasks") == "identity.yml":
            assert counts == UPDATE_RETRY, counts
            observed += 1
            continue
        for key, value in counts.items():
            assert isinstance(value, str) and "{{" in value, "%s: %s %r" % (name, key, task.get("name"))
    assert observed == (1 if name in ("observe.yml", "update.yml") else 0), name


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


def test_every_entry_point_requires_the_frozen_network():
    specs = LOADER.load_from_file(str(ROLE / "meta" / "argument_specs.yml"))["argument_specs"]
    assert set(specs) == {"apply", "observe", "destroy"}
    for entry, spec in specs.items():
        network = spec["options"]["bootwright_os_install_request"]["options"]["network"]
        assert network["type"] == "dict" and network["required"] is True, entry
        assert {field: (option["type"], option["required"]) for field, option in network["options"].items()} == {
            "checkpointSeconds": ("int", True), "document": ("dict", True),
            "documentPath": ("str", True), "markerPath": ("str", True)}, entry


# The network convergence waits, kept apart from BUDGETS, which the argument
# specification's budgets are compared with. Distinct values, so each wait is
# proved to read its own budget.
NETWORK_BUDGETS = {
    "networkApply": {"attempts": 17, "delaySeconds": 4},
    "networkReconnect": {"attempts": 19, "delaySeconds": 6},
    "reboot": {"attempts": 23, "delaySeconds": 8},
}
CHECKPOINT_SECONDS = 41


def network_rendered(value):
    """What one templated value is for a request freezing NETWORK_BUDGETS."""
    scope = dict(reboot_defaults(), bootwright_os_install_request={"budgets": NETWORK_BUDGETS,
                                                                   "network": {"checkpointSeconds": CHECKPOINT_SECONDS}})
    return int(Templar(loader=LOADER, variables=scope).template(value))


def guest_waits(command):
    """The guest includes of network.yml and its restart running one command, with the budget each reads."""
    found = []
    for task in walk(trusted("network.yml") + trusted("reboot.yml")):
        arguments = task.get("vars") or {}
        if task.get("ansible.builtin.include_tasks") == "guest.yml" and arguments.get(
                "managedos_install_anaconda_guest_command") == "{{ managedos_install_anaconda_network_%s }}" % command:
            found.append((network_rendered(arguments.get("managedos_install_anaconda_guest_retries", "0")),
                          network_rendered(arguments.get("managedos_install_anaconda_guest_pause", "0"))))
    return found


def test_the_network_waits_read_their_frozen_budgets():
    apply, reconnect, reboot = (tuple(NETWORK_BUDGETS[name].values()) for name in ("networkApply", "networkReconnect", "reboot"))
    assert guest_waits("wait") == [apply, apply, reconnect]
    assert guest_waits("boot_id") == [reboot]
    for command in ("read", "prepare", "start", "rollback", "commit", "mark", "poweroff", "show"):
        assert set(guest_waits(command)) <= {(0, 0)}, command
    assert one("network.yml", lambda task: task.get("ansible.builtin.include_tasks") == "reboot.yml")["vars"] == {
        "managedos_install_anaconda_reboot_before": "{{ managedos_install_anaconda_network_boot_before }}"}
    poll = port_wait("reboot.yml", NETWORK_BUDGETS)
    assert poll["expected_power"] == "Off"
    assert (poll["retries"], poll["delay"]) == reboot
    pause = one("network.yml", lambda task: "ansible.builtin.pause" in task)
    assert network_rendered(pause["ansible.builtin.pause"]["seconds"]) == CHECKPOINT_SECONDS + 30
