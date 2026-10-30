"""Post-network progress brackets real work and reports success only after proof."""

from __future__ import annotations

import pathlib

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
PROTOCOL = "bootwright.core.managedos_install_protocol"
INCLUDE = "ansible.builtin.include_tasks"
LOADER = DataLoader()


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


@pytest.mark.parametrize("frozen,expected", [
    ({}, set()),
    ({"diskEncryption": {}}, {"verify-security"}),
    ({"installerArguments": ["fips=1"]}, {"verify-security"}),
    ({"registration": {}}, {"register-subscription"}),
    ({"update": {}}, {"update-packages"}),
    ({"diskEncryption": {}, "installerArguments": ["fips=1"], "registration": {}, "update": {}},
     {"verify-security", "register-subscription", "update-packages"}),
])
def test_progress_conditions_match_the_frozen_request(frozen, expected):
    tasks = applied()
    templar = Templar(loader=LOADER, variables={"bootwright_os_install_request": frozen})
    for group in ("verify-security", "register-subscription", "update-packages", "verify-completion"):
        for status in ("running", "ok"):
            task = tasks[event(tasks, group, status)]
            conditions = task.get("when", [])
            if isinstance(conditions, str):
                conditions = [conditions]
            enabled = all(templar.evaluate_conditional(trust_as_template(condition)) for condition in conditions)
            assert enabled == (group in expected or group == "verify-completion")
