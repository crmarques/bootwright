"""Run the shared artifact listener proof as a caller's include runs it.

Every role that proves a file is served fetches it through the
infrastructureservices_artifact_listener role's proof entry point. A test finds
the caller's include task, and this runs the proof's task file through the
collection's one interpreter over the caller's variables, the include's vars
and what ansible.builtin.uri answered, so what the fetch was handed and what
the refusal said are read as ansible-core renders them.
"""

from __future__ import annotations

import pathlib
from typing import NamedTuple

from ansible_collections.bootwright.core.tests.unit import role_tasks

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
ROLE = ROLES / "infrastructureservices_artifact_listener"
NAME = "bootwright.core.infrastructureservices_artifact_listener"
ENTRY = "proof"
URI = "ansible.builtin.uri"
INCLUDE = "ansible.builtin.include_role"


class Outcome(NamedTuple):
    fetched: dict
    refusal: str | None
    trace: list


def includes(task: dict) -> bool:
    """Whether a task includes the shared proof by its entry point."""
    included = task.get(INCLUDE)
    return isinstance(included, dict) and included.get("name") == NAME and included.get("tasks_from") == ENTRY


def proof_tasks() -> list[dict]:
    return role_tasks.role_tasks(ROLE, ENTRY + ".yml")


def opens(task: dict) -> list[dict] | None:
    """An include resolver for role_tasks.visits that opens the shared proof."""
    return proof_tasks() if includes(task) else None


def perform(include: dict, variables: dict, answer: dict, raised: dict | None = None,
            fetched: dict | None = None) -> role_tasks.Interpreter:
    """Run the proof one include starts, letting a failed task fail its caller as ansible-core does.

    The fetch answers `answer`, or, given `raised`, fails with that result past
    its failed_when, as a task the timeout keyword stopped or whose module
    raised does."""
    assert includes(include), "%r does not include the shared listener proof" % include.get("name")
    scope = dict(role_tasks.trusted(role_tasks.role_defaults(ROLE)))
    scope.update(variables)
    scope.update(role_tasks.trusted(include.get("vars") or {}))
    seen = {} if fetched is None else fetched

    def execute(task, templar):
        if URI in task:
            seen.update(templar.template(task[URI]))
            if raised is not None:
                raise role_tasks.TaskFailed(task, dict(raised))
            return dict(answer)
        return None

    interpreter = role_tasks.Interpreter(ROLE, scope, execute)
    try:
        interpreter.run(ENTRY + ".yml")
    except role_tasks.TaskFailed as failure:
        failure.trace = interpreter.trace
        raise
    return interpreter


def run(include: dict, variables: dict, answer: dict) -> Outcome:
    """Run the proof one include starts, with the fetch answering `answer`."""
    fetched: dict = {}
    try:
        trace = perform(include, variables, answer, fetched=fetched).trace
    except role_tasks.TaskFailed as failure:
        return Outcome(fetched, str(failure.result.get("msg")), failure.trace)
    return Outcome(fetched, None, trace)
