"""A proved installation records its disk-boot intent and is never installed again.

Before the network restart or the update's reboot powers the machine off,
roles/managedos_install_anaconda/tasks/reboot.yml, which both include,
publishes the before-state that names this installation's marker, once per
attempt. A later attempt of the same apply is told the
installation is proved (bootwright_os_install_material.installationProved,
internal/managedos/installation), and apply.yml then boots the installed disk
of a powered-off machine that does not answer and reads its identity, before
the installer boot, which it skips (Q66, D183).
"""

from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader

TASKS = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda" / "tasks"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.managedos_install_protocol"


def loaded(name):
    return [task for task in LOADER.load_from_file(str(TASKS / name)) if isinstance(task, dict)]


def flattened(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from flattened([child for child in task.get(section) or [] if isinstance(child, dict)])


def named(tasks, name):
    """The one task of a flattened list with this name."""
    found = [task for task in tasks if task.get("name") == name]
    assert len(found) == 1, "%d tasks named %r" % (len(found), name)
    return found[0]


def test_the_intent_is_recorded_before_the_power_off():
    for name in ("network.yml", "update.yml"):
        assert sum(task.get("ansible.builtin.include_tasks") == "reboot.yml" for task in flattened(loaded(name))) == 1, name
    tasks = list(flattened(loaded("reboot.yml")))
    recorded = tasks.index(named(tasks, "Record that this installation is proved and its disk boot intended"))
    powered = tasks.index(named(tasks, "Ask the machine to power off"))
    assert recorded < powered
    assert tasks[recorded][PROTOCOL]["phase"] == "prepared"
    assert "managedos_install_anaconda_prepared" in str(tasks[recorded]["when"])


def test_a_proved_installation_boots_its_disk_and_never_the_installer():
    every = list(flattened(loaded("apply.yml")))
    disk = named(every, "Boot the installed disk of a machine this apply already proved")
    installer = named(every, "Boot the machine from its own installer image")
    assert every.index(disk) < every.index(installer)
    assert disk["ansible.builtin.include_tasks"] == "proved_boot.yml"
    assert any("installationProved" in str(condition) for condition in disk["when"])
    assert "not (managedos_install_anaconda_was_installed | bool)" in str(installer["when"])
    inside = list(flattened(loaded("proved_boot.yml")))
    assert not any((task.get("ansible.builtin.include_tasks") or "") == "boot.yml" for task in inside)
    powered = inside.index(named(inside, "Refuse to boot a proved machine that is not powered off"))
    booted = inside.index(named(inside, "Boot the installed disk of the proved machine"))
    answered = inside.index(named(inside, "Read the proved machine through its own channel once its disk booted"))
    assert powered < booted < answered
    assert inside[booted]["ansible.builtin.include_role"]["tasks_from"] == "boot_disk"
    assert inside[-1]["ansible.builtin.set_fact"] == {"managedos_install_anaconda_was_installed": True}


def test_a_registering_attempt_publishes_its_before_state_once():
    every = list(flattened(loaded("apply.yml")))
    registering = named(every, "Publish that this installation's marker is proved before it registers")
    assert "managedos_install_anaconda_prepared" in str(registering["when"])
