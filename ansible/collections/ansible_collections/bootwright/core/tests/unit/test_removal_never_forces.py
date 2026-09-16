"""A removal proves its target is idle; it never makes it idle.

Cutting the power to a running machine to delete it takes the memory and disks
out from under whatever was using them. The engine proves every owned asset is
stopped before it registers a removal, and these roles hold the same line at
the moment of the effect, against a machine started in between. Losing either
guard is invisible to lint and to a syntax check, and shows up only as a guest
killed by a removal that should have refused.
"""

from __future__ import annotations

import pathlib

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"


def tasks(role, name):
    parsed = yaml.safe_load((ROLES / role / "tasks" / name).read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def argv_of(task):
    command = task.get("ansible.builtin.command")
    if not isinstance(command, dict):
        return []
    return [str(value) for value in command.get("argv") or []]


def test_a_machine_removal_never_forces_its_domain_off():
    for task in tasks("substrate_libvirt_machine", "destroy.yml"):
        argv = argv_of(task)
        if argv and argv[0].endswith("virsh"):
            assert "destroy" not in argv, task.get("name")


def test_a_machine_removal_refuses_a_domain_that_is_not_shut_off():
    asserted = [
        task for task in tasks("substrate_libvirt_machine", "destroy.yml")
        if "assert" in str(task.get("ansible.builtin.assert", ""))
        or isinstance(task.get("ansible.builtin.assert"), dict)
    ]
    conditions = " ".join(
        str(condition)
        for task in asserted
        for condition in task["ansible.builtin.assert"].get("that") or []
    )
    assert "shut off" in conditions


def test_a_provider_host_removal_refuses_a_network_carrying_a_guest():
    conditions = " ".join(
        str(condition)
        for task in tasks("substrate_libvirt_host", "destroy.yml")
        if isinstance(task.get("ansible.builtin.assert"), dict)
        for condition in task["ansible.builtin.assert"].get("that") or []
    )
    assert "not item.busy" in conditions
