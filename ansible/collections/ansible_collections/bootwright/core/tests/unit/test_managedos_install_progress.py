"""Post-network progress brackets real work and reports success only after proof."""

from __future__ import annotations

import pathlib

import pytest

from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
PROTOCOL = "bootwright.core.managedos_install_protocol"
INCLUDE = "ansible.builtin.include_tasks"


def flattened(tasks):
    for task in tasks:
        if "block" in task:
            yield from flattened(task["block"])
            yield from flattened(task.get("always", []))
        else:
            yield task


def applied():
    return list(flattened(LOADER.load_from_file(str(ROLE / "tasks" / "apply.yml"), trusted_as_template=True)))


def index(tasks, predicate):
    matches = [position for position, task in enumerate(tasks) if predicate(task)]
    assert len(matches) == 1
    return matches[0]


def event(tasks, group, status):
    return index(tasks, lambda task: task.get(PROTOCOL) == {"phase": "group", "group": group, "status": status})


def test_progress_covers_post_network_work_and_success_follows_refusals():
    tasks = applied()
    network = event(tasks, "converge-network", "ok")
    phases = [
        ("verify-security", "Read whether the root disk is encrypted after its network converged",
         "Refuse a machine that does not prove FIPS mode"),
        ("register-subscription", "Refuse to register a machine that does not hold this installation's marker",
         "Refuse a machine its registration did not complete on"),
        ("update-packages", "Update the installed machine within its release after its registration",
         "Update the installed machine within its release after its registration"),
        ("verify-completion", "Prove everything this installation publishes is in place",
         "Remove private connection files"),
    ]
    previous = network
    for group, first, last in phases:
        start, end = event(tasks, group, "running"), event(tasks, group, "ok")
        first_task = index(tasks, lambda task: task["name"] == first)
        last_task = index(tasks, lambda task: task["name"] == last)
        assert previous < start < first_task <= last_task < end
        assert tasks[start]["no_log"] is True and tasks[end]["no_log"] is True
        previous = end
    completed = index(tasks, lambda task: task.get(PROTOCOL, {}).get("phase") == "completed")
    assert previous < completed


def need(**required):
    return dict(dict.fromkeys(("fips", "registration", "rootEncryption", "rootWWN", "update"), False), **required)


@pytest.mark.parametrize("frozen,expected", [
    ({"requires": need()}, set()),
    ({"diskEncryption": {}, "requires": need(rootEncryption=True)}, {"verify-security"}),
    ({"installerArguments": ["fips=1"], "requires": need(fips=True)}, {"verify-security"}),
    ({"installerArguments": ["fips=1"], "requires": need()}, set()),
    ({"registration": {}, "requires": need(registration=True)}, {"register-subscription"}),
    ({"update": {}, "requires": need(update=True)}, {"update-packages"}),
    ({"diskEncryption": {}, "installerArguments": ["fips=1"], "registration": {}, "update": {},
      "requires": need(fips=True, registration=True, rootEncryption=True, update=True)},
     {"verify-security", "register-subscription", "update-packages"}),
])
def test_progress_conditions_match_the_frozen_request(frozen, expected):
    tasks = applied()
    templar = Templar(loader=LOADER, variables={"bootwright_os_install_request": {"installerArguments": [], **frozen}})
    for group in ("verify-security", "register-subscription", "update-packages", "verify-completion"):
        for status in ("running", "ok"):
            task = tasks[event(tasks, group, status)]
            conditions = task.get("when", [])
            if isinstance(conditions, str):
                conditions = [conditions]
            enabled = all(templar.evaluate_conditional(trust_as_template(condition)) for condition in conditions)
            assert enabled == (group in expected or group == "verify-completion")
