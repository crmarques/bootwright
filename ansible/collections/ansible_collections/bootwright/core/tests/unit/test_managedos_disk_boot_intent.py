"""A proved installation records its disk-boot intent and is never installed again.

As soon as roles/managedos_install_anaconda/tasks/apply.yml proves the
installed machine holds this installation's marker, and before it ejects the
media or withdraws the work area or any private material, it publishes the
before-state that names that marker, once per attempt; the network restart,
the update's reboot and the registration all follow it. A later attempt of
the same apply is told the installation is proved
(bootwright_os_install_material.installationProved,
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


def test_the_intent_is_recorded_where_the_marker_is_proved_before_any_withdrawal():
    tasks = list(flattened(loaded("apply.yml")))
    prove = tasks.index(named(tasks, "Prove the installed marker matches this frozen installation"))
    recorded = tasks.index(named(tasks, "Record that this installation is proved and its disk boot intended"))
    later = [tasks.index(named(tasks, name)) for name in (
        "Eject any installer media still inserted and read the machine's power",
        "Remove the work directory and the rendered kickstart with it",
        "Withdraw the private material now that the machine has it",
        "Converge the installed machine to its declared network",
        "Register the installed machine with its subscription service after its network converged",
        "Update the installed machine within its release after its registration")]
    assert prove + 1 == recorded and all(recorded < index for index in later)
    assert tasks[recorded][PROTOCOL] == {"phase": "prepared", "marker": "{{ bootwright_os_install_material.marker }}"}
    assert "when" not in tasks[recorded] and tasks[recorded]["no_log"] is True
    for name in ("network.yml", "update.yml"):
        assert sum(task.get("ansible.builtin.include_tasks") == "reboot.yml" for task in flattened(loaded(name))) == 1, name
    for name in sorted(path.name for path in TASKS.glob("*.yml")):
        records = [task for task in flattened(loaded(name)) if (task.get(PROTOCOL) or {}).get("phase") == "prepared"]
        assert len(records) == (1 if name == "apply.yml" else 0), name


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


def test_fresh_install_proves_observed_marker_before_network_or_prepared():
    tasks = list(flattened(loaded("apply.yml")))
    read = tasks.index(named(tasks, "Read the installation the machine now holds, through its own channel"))
    prove = tasks.index(named(tasks, "Prove the installed marker matches this frozen installation"))
    verify = tasks.index(named(tasks, "Prove the installed machine answers on the exact key it reported"))
    network = tasks.index(named(tasks, "Converge the installed machine to its declared network"))
    assert read < prove < verify < network
    check = tasks[prove]["ansible.builtin.assert"]["that"]
    assert "managedos_install_anaconda_marker == bootwright_os_install_material.marker" in check
