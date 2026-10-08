"""A service's content root is refused unless in normal form, and its destroy resets a unit left failed.

The managed network services and the artifact server each remove one content
root recursively, so their first task refuses a root that is not exactly
<prefix>/<context>/<kind>/<name> with no empty, `.` or `..` component. Their
destroy resets the failed state systemd still lists a removed unit in, so a
destroy after a failed apply leaves no managed unit listed failed. Evaluating
those guards needs Ansible's controller (DataLoader and Templar), so these
checks live here rather than under tests/unit/plugins.
"""

from __future__ import annotations

import pytest
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.tests.unit.test_managed_service_role import (
    ARTIFACT,
    LOADER,
    MANAGED,
    ROLES,
    argv_of,
    holds,
    index_of,
    load_tasks,
    validate_of,
    variables,
)

PREFIX = "/var/lib/bootwright-services"
ARTIFACT_APPLY = dict(
    ARTIFACT,
    unit="bootwright-lab-artifact-server-media",
    image="registry.example/nginx@sha256:" + "0" * 64,
    listeners=[{"name": "http", "port": 8080, "protocol": "http"}],
)
ENTRIES = [
    ("infra_managed_service", "apply.yml", "bootwright_managed_service", MANAGED, "dns", "resolver"),
    ("infra_managed_service", "destroy.yml", "bootwright_managed_service", MANAGED, "dns", "resolver"),
    ("infra_artifact_server_nginx", "destroy.yml", "bootwright_artifact_server", ARTIFACT, "artifact-server", "media"),
    ("infra_artifact_server_nginx", "apply.yml", "bootwright_artifact_server", ARTIFACT_APPLY, "artifact-server", "media"),
]
PLANTED = [
    ("lab", "{p}/lab/{k}/.."),
    ("lab", "{p}/lab/{k}/."),
    ("lab", "{p}/lab/./{n}"),
    ("lab", "{p}/lab//{k}/{n}"),
    ("lab", "{p}/lab/{k}/{n}/"),
    ("lab", "{p}/lab/{k}/{n}/extra"),
    ("..", "{p}/../{k}/{n}"),
    ("lab/nested", "{p}/lab/nested/{k}/{n}"),
]
DESTROYS = [
    ("infra_managed_service", "bootwright_managed_service", dict(MANAGED)),
    ("infra_artifact_server_nginx", "bootwright_artifact_server", dict(ARTIFACT, unit="bootwright-lab-artifact-server-media")),
]
NOT_LOADED = "Failed to reset failed state of unit x.service: Unit x.service not loaded."


@pytest.mark.parametrize("context, planted", PLANTED, ids=[root for _context, root in PLANTED])
@pytest.mark.parametrize("role, entry, prefix, request_, kind, name", ENTRIES,
                         ids=["managed apply", "managed destroy", "artifact destroy", "artifact apply"])
def test_a_content_root_not_in_normal_form_refuses_before_any_removal(role, entry, prefix, request_, kind, name, context, planted):
    tasks = load_tasks(role, entry)
    validate = validate_of(tasks)
    assert tasks.index(validate) == 0
    assert holds(validate, variables(prefix, request_))
    root = planted.format(p=PREFIX, k=kind, n=name)
    bad = dict(request_, contentRoot=root, identity={"context": context})
    assert not holds(validate, variables(prefix, bad)), root


def role_variables(role, prefix, request_):
    found = variables(prefix, request_)
    found.update(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True))
    return found


def is_reset(task):
    return argv_of(task)[:2] == ["/usr/bin/systemctl", "reset-failed"]


def evaluate(templar, conditions):
    if not isinstance(conditions, list):
        conditions = [conditions]
    return all(templar.evaluate_conditional(trust_as_template(str(condition))) for condition in conditions)


@pytest.mark.parametrize("role, prefix, request_", DESTROYS, ids=["managed service", "artifact server"])
def test_a_destroy_resets_a_unit_it_finds_failed(role, prefix, request_):
    tasks = load_tasks(role, "destroy.yml")
    reset = index_of(tasks, is_reset, "resets the failed unit")
    stop = index_of(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "stop"], "stops the unit")
    reload_ = index_of(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "daemon-reload"], "reloads")
    show = index_of(tasks, lambda task: argv_of(task)[:3] == ["/usr/bin/systemctl", "show", "--property=ActiveState"], "observes the unit")
    proof = index_of(tasks, lambda task: task.get("name") == "Prove every owned resource is gone", "proves absence")
    assert stop < reload_ < show < reset < proof
    templar = Templar(loader=LOADER, variables=role_variables(role, prefix, request_))
    assert templar.template(tasks[reset]["ansible.builtin.command"]["argv"][2]) == request_["unit"] + ".service"
    assert templar.template(tasks[show]["ansible.builtin.command"]["argv"][-1]) == request_["unit"] + ".service"
    assert tasks[show]["register"] == role + "_listed"
    listed = Templar(loader=LOADER, variables={role + "_listed": {"stdout": "failed"}})
    assert evaluate(listed, tasks[reset]["when"])


@pytest.mark.parametrize("role", [destroy[0] for destroy in DESTROYS])
def test_a_destroy_ignores_a_unit_that_is_not_loaded(role):
    tasks = load_tasks(role, "destroy.yml")
    reset = tasks[index_of(tasks, is_reset, "resets the failed unit")]
    for shown in ("inactive", "active", ""):
        listed = Templar(loader=LOADER, variables={role + "_listed": {"stdout": shown}})
        assert not evaluate(listed, reset["when"]), shown
    for result, failed, changed in [
        ({"rc": 1, "stderr": NOT_LOADED}, False, False),
        ({"rc": 1, "stderr": "Access denied"}, True, False),
        ({"rc": 0, "stderr": ""}, False, True),
    ]:
        templar = Templar(loader=LOADER, variables={role + "_reset": result})
        assert evaluate(templar, reset["failed_when"]) is failed, result
        assert evaluate(templar, reset["changed_when"]) is changed, result


@pytest.mark.parametrize("role", [destroy[0] for destroy in DESTROYS])
def test_a_destroy_counts_a_reset_as_a_change(role):
    tasks = load_tasks(role, "destroy.yml")
    completion = tasks[index_of(tasks, lambda task: "removal evidence" in str(task.get("name", "")), "publishes the evidence")]
    module = next(value for key, value in completion.items() if key.endswith("_protocol"))
    unchanged = {"changed": False}
    base = {
        role + "_unit_removed": unchanged,
        role + "_root_removed": unchanged,
        role + "_parents": unchanged,
        role + "_before": {"observation": {"unit": ""}},
    }
    for reset, outcome in [({"changed": True}, "changed"), ({"changed": False, "skipped": True}, "unchanged")]:
        templar = Templar(loader=LOADER, variables=dict(base, **{role + "_reset": reset}))
        assert str(templar.template(module["outcome"])).strip() == outcome, reset
