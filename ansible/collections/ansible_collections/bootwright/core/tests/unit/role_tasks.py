"""Read and run a role's task files the way ansible-core orders and decides them.

One DataLoader, one reader and one interpreter serve every role test, so a
block, a rescue or an included file means the same thing in each of them. The
interpreter models what the host does around the tasks a test stubs; it never
starts a play, a worker or a connection.

Order and conditions, as ansible-core parses them:

- a block's tasks run in order, then its rescue tasks only when one of them
  failed, then its always tasks whether or not one failed;
- a block's `when` is not tested once for the block: every task in its block,
  rescue and always sections inherits it, in front of the task's own `when`,
  and each task tests the whole list when it is reached;
- an import_tasks file's tasks inherit the `when` of the import task as a
  block's do; an include_tasks file's tasks inherit only the conditions around
  the include task, since its own `when` decides whether the file is loaded;
  an include's `vars` are visible to the file's tasks and to nothing after;
- a task whose condition is false does not run and still registers the
  skipped result ansible-core records for it (`skipped` true, `changed` false);
- `changed_when` sets `changed`, then `failed_when` replaces the verdict, both
  evaluated over the result registered under the task's `register` name; with
  no `failed_when`, a command-family task fails on a nonzero `rc`;
- a failed task registers its result before it fails the block, and
  `ignore_errors` lets the run continue past it.

What the interpreter does not model, a test states in its executor: a module's
own work, `until` retries, `delegate_to`, handlers and `meta`. tests/unit/
test_role_tasks.py holds the interpreter to ansible-core's parsed structure.
"""

from __future__ import annotations

import pathlib
from collections.abc import Callable, Iterator
from typing import Any, NamedTuple

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template


class Loader(DataLoader):
    """One DataLoader for every role test, with a file cache for each kind of reading.

    DataLoader caches a parsed file under its path alone, so whichever test
    read a file first would decide, for every later test in the process,
    whether its strings are trusted as templates."""

    def __init__(self):
        super().__init__()
        self.caches = {True: self._FILE_CACHE, False: {}}

    def load_from_file(self, file_name, cache="all", unsafe=False, json_only=False, trusted_as_template=False):
        self._FILE_CACHE = self.caches[bool(trusted_as_template)]
        return super().load_from_file(file_name, cache, unsafe, json_only, trusted_as_template)


LOADER = Loader()
SECTIONS = ("block", "rescue", "always")
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
COMMANDS = ("ansible.builtin.command", "ansible.builtin.shell", "ansible.builtin.raw", "ansible.builtin.script")
ASSERT = "ansible.builtin.assert"
FAIL = "ansible.builtin.fail"
SET_FACT = "ansible.builtin.set_fact"


GUEST_SESSION = "bootwright.core.managedos_guest_session"
SESSION_ARGUMENTS = ("ssh_config", "known_hosts", "host_key_algorithms", "policy_host_key_algorithms")


def guest_session(tasks: list[dict], module: str) -> tuple[dict, dict, dict]:
    """The open, run and close tasks of the one pinned guest session `module` runs in a task file's tasks.

    Every session file takes one shape: it opens the session, runs the one
    module over exactly the files and algorithms the open answered, and closes
    the same directory, so no file names a session file or an algorithm of
    its own."""
    opened = [task for task in tasks if GUEST_SESSION in task]
    assert len(opened) == 2, "%d session tasks" % len(opened)
    first, last = opened[0][GUEST_SESSION], opened[1][GUEST_SESSION]
    assert first["state"] == "present" and set(first) == {"state", "directory", "address", "host_key"}, first
    assert last == {"state": "absent", "directory": first["directory"]}, last
    runs = [task for task in tasks if module in task]
    assert len(runs) == 1, "%d tasks run %s" % (len(runs), module)
    assert tasks.index(opened[0]) < tasks.index(runs[0]) < tasks.index(opened[1])
    register = opened[0]["register"]
    for key in SESSION_ARGUMENTS:
        assert runs[0][module][key] == "{{ %s.%s }}" % (register, key), key
    return opened[0], runs[0], opened[1]


class Skipped:
    """What an executor returns for a task its model leaves out."""


SKIP = Skipped()


class TaskFailed(Exception):
    """A task failed, as ansible-core fails it: its block, then the play, unless a rescue or ignore_errors catches it."""

    def __init__(self, task: dict, result: dict):
        self.task = task
        self.result = result
        super().__init__("%s: %s" % (task.get("name"), result.get("msg") or result.get("stderr") or result))


class Unmodelled(Exception):
    """A task reached that neither the executor nor the interpreter models."""


class Visit(NamedTuple):
    task: dict
    conditions: tuple
    section: str
    parents: tuple = ()


def conditions_of(task: dict) -> list:
    own = task.get("when", [])
    return list(own) if isinstance(own, list) else [own]


def load_tasks(path: pathlib.Path | str) -> list[dict]:
    """A task file's tasks, as ansible-core's loader reads them."""
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) or [] if isinstance(task, dict)]


def load_mapping(path: pathlib.Path | str) -> dict:
    return dict(LOADER.load_from_file(str(path), trusted_as_template=True) or {})


def trusted(value: Any) -> Any:
    """Tasks written in a test, marked as templates the way the loader marks a file's."""
    if isinstance(value, str):
        return trust_as_template(value)
    if isinstance(value, list):
        return [trusted(item) for item in value]
    if isinstance(value, dict):
        return {key: trusted(item) for key, item in value.items()}
    return value


def role_tasks(role: pathlib.Path, name: str) -> list[dict]:
    return load_tasks(role / "tasks" / name)


def role_defaults(role: pathlib.Path) -> dict:
    return load_mapping(role / "defaults" / "main.yml")


def is_import(task: dict) -> bool:
    return "ansible.builtin.import_tasks" in task


def included_file(task: dict) -> Any:
    """The file name an include_tasks or import_tasks task names, as written, or None."""
    for module in INCLUDES:
        if module in task:
            value = task[module]
            return value.get("file", value.get("_raw_params")) if isinstance(value, dict) else value
    return None


def opens(role: pathlib.Path, only: tuple[str, ...] | None = None) -> Callable[[dict], list[dict] | None]:
    """An include resolver for flatten: the tasks of the role's own file a task includes, or None.

    Only the named files are opened when `only` is given; a file name that holds
    a template is left closed, as no variable is known to a static reading."""
    def resolve(task: dict) -> list[dict] | None:
        name = included_file(task)
        if not isinstance(name, str) or "{{" in name or (only is not None and name not in only):
            return None
        return role_tasks(role, name)
    return resolve


def visits(tasks: list[dict], include: Callable[[dict], list[dict] | None] | None = None,
           inherited: tuple = (), section: str = "block", parents: tuple = ()) -> Iterator[Visit]:
    """Every task in the order a play reaches them, each with the conditions it inherits.

    A block task is yielded before its children, in block, rescue, always
    order, each with the block tasks around it, outermost first. A task
    `include` resolves is followed by that file's tasks, which inherit what
    ansible-core gives them: an import's own conditions too, an include's only
    those around it."""
    for task in tasks or []:
        if not isinstance(task, dict):
            continue
        conditions = inherited + tuple(conditions_of(task))
        yield Visit(task, conditions, section, parents)
        opened = include(task) if include else None
        if opened:
            carried = conditions if is_import(task) else inherited
            yield from visits(opened, include, carried, section, parents)
        for name in SECTIONS:
            yield from visits(task.get(name), include, conditions, name, parents + (task,))


def flatten(tasks: list[dict], include: Callable[[dict], list[dict] | None] | None = None) -> list[dict]:
    """The tasks of visits, without their conditions."""
    return [visit.task for visit in visits(tasks, include)]


class Interpreter:
    """Run a role's task files in order over `variables`, which it updates as a host would.

    `execute(task, templar)` performs one task's module over the arguments the
    templar renders, and returns the module's result dict, SKIP to leave the
    task out of the model, or None to leave it to the interpreter, which runs
    include_tasks, import_tasks, assert, fail and set_fact itself and refuses
    any other task as Unmodelled. `omit(task)` names the tasks the model leaves
    out before any condition, loop or include of theirs is looked at; `include`
    says which include_tasks and import_tasks files are opened."""

    def __init__(self, role: pathlib.Path, variables: dict, execute: Callable[[dict, Templar], Any] | None = None,
                 include: Callable[[dict], bool] | None = None, omit: Callable[[dict], bool] | None = None):
        self.role = pathlib.Path(role)
        self.variables = variables
        self.execute = execute or (lambda task, templar: None)
        self.include = include or (lambda task: True)
        self.omit = omit or (lambda task: False)
        self.trace: list[tuple[str, str]] = []
        self.through: str | None = None

    def run(self, name: str, through: str | None = None) -> bool:
        """Run one task file, stopping after the task named `through`; whether that task was reached."""
        return self.run_tasks(role_tasks(self.role, name), through)

    def run_tasks(self, tasks: list[dict], through: str | None = None) -> bool:
        self.through = through
        try:
            self.perform(trusted(tasks), (), {}, {})
        except _Through:
            return True
        return through is None

    def templar(self, low: dict, high: dict) -> Templar:
        """Variables as a task sees them: block and task vars, then the host's facts and registers, then include and loop variables."""
        merged = {**low, **self.variables, **high}
        merged["vars"] = dict(merged)
        return Templar(loader=LOADER, variables=merged)

    def perform(self, tasks: list[dict], inherited: tuple, low: dict, high: dict) -> None:
        for task in tasks or []:
            if not isinstance(task, dict):
                continue
            conditions = inherited + tuple(conditions_of(task))
            if "block" in task:
                self.block(task, conditions, {**low, **(task.get("vars") or {})}, high)
            else:
                self.step(task, inherited, conditions, low, high)

    def block(self, task: dict, conditions: tuple, low: dict, high: dict) -> None:
        through = self.through
        try:
            try:
                self.perform(task["block"], conditions, low, high)
            except TaskFailed as failure:
                if "rescue" not in task:
                    raise
                self.variables["ansible_failed_task"] = {"name": failure.task.get("name")}
                self.variables["ansible_failed_result"] = failure.result
                self.perform(task["rescue"], conditions, low, high)
        finally:
            self.through = None
            try:
                self.perform(task.get("always"), conditions, low, high)
            finally:
                self.through = through

    def step(self, task: dict, inherited: tuple, conditions: tuple, low: dict, high: dict) -> None:
        low = {**low, **(task.get("vars") or {})}
        templar = self.templar(low, high)
        name, slot = str(task.get("name")), len(self.trace)
        if self.omit(task):
            self.trace.insert(slot, (name, "left out"))
            return
        if is_import(task):
            return self.imported(task, templar, conditions, low, high)
        if "loop" in task:
            result = self.looped(task, inherited, conditions, low, high, templar)
        elif not holds(templar, conditions):
            result = skipped_result()
        else:
            result = self.once(task, templar, low, high, inherited, conditions)
        if result is SKIP:
            self.trace.insert(slot, (name, "left out"))
            return
        outcome = "skipped" if result.get("skipped") else "failed" if result.get("failed") else "ran"
        self.trace.insert(slot, (name, outcome))
        if isinstance(task.get("register"), str):
            self.variables[task["register"]] = result
        if result.get("failed") and not self.ignores(task, templar):
            raise TaskFailed(task, result)
        if task.get("name") == self.through:
            raise _Through()

    def ignores(self, task: dict, templar: Templar) -> bool:
        flag = task.get("ignore_errors", False)
        return bool(templar.template(flag)) if isinstance(flag, str) else bool(flag)

    def looped(self, task: dict, inherited: tuple, conditions: tuple, low: dict, high: dict, templar: Templar) -> Any:
        variable = (task.get("loop_control") or {}).get("loop_var", "item")
        results = []
        for item in templar.template(task["loop"]):
            inner = {**high, variable: item}
            single = self.templar(low, inner)
            if not holds(single, conditions):
                results.append({**skipped_result(), variable: item})
                continue
            result = self.once(task, single, low, inner, inherited, conditions)
            if result is SKIP:
                return SKIP
            results.append({**result, variable: item})
        failed = any(result.get("failed") for result in results)
        return {"results": results, "changed": any(result.get("changed") for result in results),
                "skipped": bool(results) and all(result.get("skipped") for result in results),
                "failed": failed, **({"msg": "One or more items failed"} if failed else {})}

    def once(self, task: dict, templar: Templar, low: dict, high: dict, inherited: tuple, conditions: tuple) -> Any:
        claimed = self.execute(task, templar)
        if claimed is SKIP:
            return SKIP
        result = claimed if claimed is not None else self.builtin(task, templar, low, high, inherited, conditions)
        if result is SKIP:
            return SKIP
        return self.decided(task, dict(result), low, high)

    def file_of(self, task: dict, templar: Templar) -> list[dict]:
        text = templar.template(included_file(task))
        assert isinstance(text, str), "%s names no file: %r" % (task.get("name"), included_file(task))
        return role_tasks(self.role, text)

    def imported(self, task: dict, templar: Templar, conditions: tuple, low: dict, high: dict) -> None:
        """A static import is a block over the file's tasks: each inherits the import's conditions and tests them itself."""
        if not self.include(task):
            self.trace.append((str(task.get("name")), "left out"))
            return
        self.perform(self.file_of(task, templar), conditions, low, high)

    def builtin(self, task: dict, templar: Templar, low: dict, high: dict, inherited: tuple, conditions: tuple) -> Any:
        if included_file(task) is not None:
            if not self.include(task):
                return SKIP
            self.perform(self.file_of(task, templar), inherited, low, {**high, **(task.get("vars") or {})})
            return {"changed": False}
        if ASSERT in task:
            arguments = task[ASSERT]
            that = arguments["that"]
            for condition in that if isinstance(that, list) else [that]:
                if not templar.evaluate_conditional(condition):
                    message = arguments.get("fail_msg")
                    return {"failed": True, "changed": False, "assertion": condition, "evaluated_to": False,
                            "msg": templar.template(message) if message else "Assertion failed"}
            return {"changed": False}
        if FAIL in task:
            message = (task[FAIL] or {}).get("msg")
            return {"failed": True, "changed": False, "msg": templar.template(message) if message else "Failed as requested from task"}
        if SET_FACT in task:
            facts = {key: templar.template(value) for key, value in task[SET_FACT].items() if key != "cacheable"}
            self.variables.update(facts)
            return {"changed": False, "ansible_facts": facts}
        modules = [key for key in task if key.startswith(("ansible.builtin.", "bootwright."))]
        raise Unmodelled("%s: %s is neither executed by the test nor modelled by the interpreter" % (task.get("name"), modules))

    def decided(self, task: dict, result: dict, low: dict, high: dict) -> dict:
        """Apply changed_when, failed_when and the command family's rc rule to a module's result."""
        if any(module in task for module in COMMANDS) and "failed_when" not in task and result.get("rc", 0) != 0:
            result["failed"] = True
        register = task.get("register")
        for key in ("changed_when", "failed_when"):
            if key not in task:
                continue
            seen = {register: result} if isinstance(register, str) else {}
            conditions = task[key] if isinstance(task[key], list) else [task[key]]
            verdict = holds(self.templar(low, {**high, **seen}), conditions)
            result["changed" if key == "changed_when" else "failed"] = verdict
        return result


class _Through(Exception):
    """The task a test asked to stop after has completed."""


def holds(templar: Templar, conditions: Any) -> bool:
    return all(templar.evaluate_conditional(condition) for condition in conditions)


def skipped_result() -> dict:
    return {"changed": False, "skipped": True, "skip_reason": "Conditional result was False"}
