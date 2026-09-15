"""Every role variable is registered before the task that reads it.

A task that reads a variable a later task registers is accepted by the syntax
check and by ansible-lint, and fails only when that path actually runs on a
host. The roles here reorder around exactly this — an image must be pulled
before a hash is derived from it, and that hash written before the unit that
mounts it — so the order is checked here rather than discovered by an operator.
"""

from __future__ import annotations

import pathlib
import re

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"

# A registered name is referenced as a bare Jinja variable or through one of its
# result fields, anywhere in the task's own arguments and conditions.
REFERENCE = re.compile(r"\b([a-z][a-z0-9_]*)\b")


def task_files():
    return sorted(ROLES.glob("*/tasks/*.yml"))


def load(path):
    parsed = yaml.safe_load(path.read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def registered_names(tasks):
    return {task["register"] for task in tasks if isinstance(task.get("register"), str)}


def referenced_names(task):
    """Every identifier the task mentions outside its own register."""
    body = dict(task)
    body.pop("register", None)
    body.pop("name", None)
    return set(REFERENCE.findall(yaml.safe_dump(body, default_flow_style=False)))


def test_every_role_has_task_files():
    assert task_files(), "no role task files were found to check"


def test_no_task_reads_a_variable_a_later_task_registers():
    problems = []
    for path in task_files():
        tasks = load(path)
        later = registered_names(tasks)
        for index, task in enumerate(tasks):
            # A name stops being "later" once its own task has run, so the
            # register of this task is removed before the next one is checked.
            current = task.get("register")
            referenced = referenced_names(task)
            for name in sorted(referenced & later):
                if name == current:
                    continue
                problems.append(
                    "%s: task %d (%s) reads %s, which a later task registers"
                    % (path.name, index + 1, task.get("name", "unnamed"), name)
                )
            if isinstance(current, str):
                later.discard(current)
    assert not problems, "\n".join(problems)
