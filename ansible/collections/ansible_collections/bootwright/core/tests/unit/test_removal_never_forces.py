"""A removal proves its target is idle; it never makes it idle.

Cutting the power to a running machine to delete it takes the memory and disks
out from under whatever was using them. The engine proves every Machine a
removal would take back is stopped before it registers, and this role holds the
same line at the moment of the effect, against a machine started in between.
Losing either guard is invisible to lint and to a syntax check, and shows up
only as a guest killed by a removal that should have refused.
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


# A hypervisor that did not answer reports no domain, which every other guard
# reads as a domain that is not defined, so its refusal has to come before the
# first task that takes anything away, the controller's stop included.
def test_a_machine_removal_refuses_a_silent_hypervisor_before_it_stops_anything():
    destroy = tasks("substrate_libvirt_machine", "destroy.yml")

    def first(predicate):
        return next((index for index, task in enumerate(destroy) if predicate(task)), None)

    refusal = first(lambda task: any(
        str(condition).strip() == "substrate_libvirt_machine_before.observation.answered"
        for condition in (task.get("ansible.builtin.assert") or {}).get("that") or []
    ))
    stop = first(lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "stop"])
    effect = first(lambda task: "ansible.builtin.command" in task or "ansible.builtin.file" in task)
    assert refusal is not None, "the removal no longer refuses a hypervisor that did not answer"
    assert stop is not None, "the removal no longer stops the management controller"
    assert refusal < effect <= stop


# A connection that does not answer reports no network and no pool, which the
# host removal reads as nothing this context owns, so its refusal has to come
# before the first task that takes anything away.
def test_a_host_removal_refuses_a_silent_hypervisor_before_its_first_effect():
    destroy = tasks("substrate_libvirt_host", "destroy.yml")

    def first(predicate):
        return next((index for index, task in enumerate(destroy) if predicate(task)), None)

    refusal = first(lambda task: any(
        str(condition).strip() == "substrate_libvirt_host_before.observation.uri"
        for condition in (task.get("ansible.builtin.assert") or {}).get("that") or []
    ))
    effect = first(lambda task: "ansible.builtin.command" in task or "ansible.builtin.file" in task)
    assert refusal is not None, "the host removal no longer refuses a hypervisor that did not answer"
    assert effect is not None, "the host removal no longer takes anything away"
    assert refusal < effect


# The uri answering proves only that the hypervisor driver did. A network or the
# pool its own driver did not answer for reads as nothing defined too, so each
# refusal has to come before the first task that takes anything away.
def test_a_host_removal_refuses_what_its_drivers_did_not_answer_for_before_its_first_effect():
    destroy = tasks("substrate_libvirt_host", "destroy.yml")

    def refusal(condition):
        return next((
            index for index, task in enumerate(destroy)
            if any(str(that).strip() == condition for that in (task.get("ansible.builtin.assert") or {}).get("that") or [])
        ), None)

    effect = next(index for index, task in enumerate(destroy) if "ansible.builtin.command" in task or "ansible.builtin.file" in task)
    network = refusal("item.answered")
    pool = refusal("substrate_libvirt_host_before.observation.poolAnswered")
    assert network is not None, "the host removal no longer refuses a network its driver did not answer for"
    assert pool is not None, "the host removal no longer refuses a pool its driver did not answer for"
    assert destroy[network].get("loop") == "{{ substrate_libvirt_host_before.observation.networks }}"
    assert destroy[network].get("when") == "item.managed"
    assert max(network, pool) < effect
