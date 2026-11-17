"""The role-test interpreter orders and decides tasks as ansible-core parses and evaluates them.

Nothing here starts a play, a worker or a connection. ansible-core's own
objects read a small fixture role (fixtures/role_tasks): Block and Task as
load_list_of_blocks builds them, each Task's inherited `when` list, and
Task._resolve_conditional, the evaluation the task executor makes. The
interpreter in role_tasks.py must list the same tasks in the same order with
the same conditions, and run or skip each as ansible-core's evaluation says.
What only a run decides (a failure, its rescue, register on skip) is held to
the behaviour ansible-core documents, and the skip result to the one its task
executor records.
"""

from __future__ import annotations

import inspect
import pathlib

import pytest
from ansible._internal import _task
from ansible.parsing.dataloader import DataLoader
from ansible.playbook.block import Block
from ansible.playbook.helpers import load_list_of_blocks
from ansible.playbook.play import Play
from ansible.playbook.task_include import TaskInclude
from ansible.template import is_trusted_as_template
from ansible.vars.manager import VariableManager

from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import SKIP, Interpreter, TaskFailed, Unmodelled

ROLE = pathlib.Path(__file__).resolve().parent / "fixtures" / "role_tasks"
COMMAND = "ansible.builtin.command"
FACT = "ansible.builtin.set_fact"

CLEAR = {"enabled": False, "outer": False, "outer_two": False, "inner": False, "explode": False, "include_inside": False,
         "nested": False, "deep_when": False, "include_it": False, "included_when": False, "import_it": False,
         "imported_when": False}


def scenario(**changed):
    return dict(CLEAR, **changed)


class Parsed:
    """The fixture role as ansible-core's loader and playbook classes read it."""

    def __init__(self):
        self.loader = DataLoader()
        self.loader.set_basedir(str(ROLE / "tasks"))
        self.manager = VariableManager(loader=self.loader)
        self.play = Play.load({"hosts": "all", "tasks": []}, loader=self.loader)

    def blocks(self, name, include=None):
        return load_list_of_blocks(self.loader.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), play=self.play,
                                   task_include=include, loader=self.loader, variable_manager=self.manager)

    def listing(self, blocks):
        """Every task in the order a play reaches them: block, rescue, always, each include followed by its file."""
        for block in blocks:
            for section in role_tasks.SECTIONS:
                for item in getattr(block, section):
                    if isinstance(item, Block):
                        yield from self.listing([item])
                        continue
                    yield item
                    if isinstance(item, TaskInclude):
                        yield from self.listing(self.blocks(item.args["_raw_params"], item))

    def decisions(self, blocks, variables):
        """What each task's own evaluation makes of it with no failure: ran or skipped; a skipped include opens nothing."""
        for block in blocks:
            for section in ("block", "always"):
                for item in getattr(block, section):
                    if isinstance(item, Block):
                        yield from self.decisions([item], variables)
                        continue
                    holds = item._resolve_conditional(item.when, variables)
                    yield item.name, "ran" if holds else "skipped"
                    if isinstance(item, TaskInclude) and holds:
                        yield from self.decisions(self.blocks(item.args["_raw_params"], item), variables)


def execute(rc=0):
    def run(task, templar):
        if COMMAND in task:
            return {"rc": rc, "stdout": "", "stderr": "", "changed": True}
        return None
    return run


def conditions(visit):
    return list(visit.conditions)


def test_the_interpreter_lists_tasks_as_ansible_core_parses_them():
    parsed = Parsed()
    expected = [(item.name, list(item.when)) for item in parsed.listing(parsed.blocks("main.yml"))]
    seen = [(visit.task["name"], conditions(visit))
            for visit in role_tasks.visits(role_tasks.role_tasks(ROLE, "main.yml"), role_tasks.opens(ROLE))
            if "block" not in visit.task and not role_tasks.is_import(visit.task)]
    assert seen == expected
    assert ("Included read", ["outer", "outer_two", "included_when"]) in seen
    assert ("Included read", ["included_when"]) in seen
    assert ("Imported read", ["import_it", "imported_when"]) in seen


def test_a_block_condition_is_inherited_by_its_rescue_always_and_nested_tasks():
    parsed = Parsed()
    inherited = {item.name: list(item.when) for item in parsed.listing(parsed.blocks("main.yml"))}
    for name in ("First inside", "After the failure", "Rescued", "Always", "Deep"):
        assert inherited[name][:2] == ["outer", "outer_two"], name
    assert inherited["Deep"] == ["outer", "outer_two", "nested", "deep_when"]


@pytest.mark.parametrize("variables", [
    scenario(),
    scenario(enabled=True),
    scenario(outer=True),
    scenario(outer=True, outer_two=True, inner=True),
    scenario(outer=True, outer_two=True, nested=True, deep_when=True, include_inside=True, included_when=True),
    scenario(outer=True, outer_two=True, include_inside=True),
    scenario(include_it=True, included_when=True),
    scenario(include_it=True),
    scenario(import_it=True, imported_when=True),
    scenario(import_it=True),
    scenario(imported_when=True),
    scenario(enabled=True, outer=True, outer_two=True, inner=True, nested=True, deep_when=True, include_inside=True,
             included_when=True, include_it=True, import_it=True, imported_when=True),
])
def test_the_interpreter_runs_and_skips_each_task_as_ansible_cores_evaluation_does(variables):
    parsed = Parsed()
    expected = [(name, outcome) for name, outcome in parsed.decisions(parsed.blocks("main.yml"), dict(variables))
                if name not in ("Judged", "Last")]
    scope = dict(variables, judged={"rc": 0})
    interpreter = Interpreter(ROLE, scope, execute())
    interpreter.run("main.yml")
    seen = [(name, outcome) for name, outcome in interpreter.trace if name not in ("Judged", "Last")]
    assert seen == expected


@pytest.mark.parametrize("rc", [0, 1, 2])
def test_changed_when_and_failed_when_are_judged_as_ansible_core_judges_them(rc):
    parsed = Parsed()
    task = next(item for item in parsed.listing(parsed.blocks("main.yml")) if item.name == "Judged")
    registered = {"judged": {"rc": rc}}
    changed = task._resolve_conditional([task.changed_when], registered)
    failed = task._resolve_conditional([task.failed_when], registered)
    scope = scenario()
    interpreter = Interpreter(ROLE, scope, execute(rc))
    if failed:
        with pytest.raises(TaskFailed):
            interpreter.run("main.yml")
    else:
        interpreter.run("main.yml")
        assert scope["last"] == rc
    assert scope["judged"]["changed"] == changed and scope["judged"].get("failed", False) == failed


def test_a_skipped_task_registers_the_result_ansible_core_records_for_it():
    recorded = inspect.getsource(_task.UnifiedTaskResult.record_conditional_false)
    assert "Conditional result was False" in recorded
    assert role_tasks.skipped_result() == {"changed": False, "skipped": True, "skip_reason": "Conditional result was False"}
    scope = scenario(outer=True, outer_two=True, inner=False)
    Interpreter(ROLE, scope, execute()).run("main.yml")
    assert scope["first"] == role_tasks.skipped_result()


def test_a_block_condition_that_turns_false_mid_block_skips_the_tasks_after_it():
    tasks = [{"name": "Guarded", "when": "gate", "block": [
        {"name": "Close", "ansible.builtin.set_fact": {"gate": False}},
        {"name": "Later", "ansible.builtin.set_fact": {"later": True}, "register": "later_result"},
    ]}]
    scope = {"gate": True}
    interpreter = Interpreter(ROLE, scope)
    interpreter.run_tasks(tasks)
    assert interpreter.trace == [("Close", "ran"), ("Later", "skipped")]
    assert "later" not in scope and scope["later_result"]["skipped"] is True


def test_a_failure_runs_the_rescue_then_always_and_the_play_goes_on():
    scope = scenario(outer=True, outer_two=True, explode=True, enabled=True)
    interpreter = Interpreter(ROLE, scope, execute())
    interpreter.run("main.yml")
    names = [name for name, outcome in interpreter.trace]
    assert interpreter.trace[names.index("Failing inside")][1] == "failed"
    assert "After the failure" not in names and "Nested block" not in names
    assert names.index("Failing inside") < names.index("Rescued") < names.index("Always") < names.index("Include")
    assert scope["rescued"] is True and scope["settled_always"] is True
    assert scope["ansible_failed_task"]["name"] == "Failing inside"
    assert scope["ansible_failed_result"]["msg"] == "boom"


def test_without_a_failure_the_rescue_does_not_run_and_always_does():
    scope = scenario(outer=True, outer_two=True)
    interpreter = Interpreter(ROLE, scope, execute())
    interpreter.run("main.yml")
    assert "rescued" not in scope and scope["settled_always"] is True


def test_an_always_task_inherits_the_block_condition():
    scope = scenario()
    interpreter = Interpreter(ROLE, scope, execute())
    interpreter.run("main.yml")
    assert ("Always", "skipped") in interpreter.trace and "settled_always" not in scope


def test_always_runs_and_the_failure_stands_when_the_rescue_fails_too():
    tasks = [{"name": "Wrapper", "block": [{"name": "Break", "ansible.builtin.fail": {"msg": "first"}}],
              "rescue": [{"name": "Break again", "ansible.builtin.fail": {"msg": "second"}}],
              "always": [{"name": "Settle", "ansible.builtin.set_fact": {"settled": True}}]}]
    scope = {}
    with pytest.raises(TaskFailed, match="second"):
        Interpreter(ROLE, scope).run_tasks(tasks)
    assert scope["settled"] is True


def test_a_failure_with_no_rescue_stops_the_run_after_always_and_ignore_errors_continues():
    tasks = [{"name": "Wrapper", "block": [{"name": "Break", "ansible.builtin.fail": {}}],
              "always": [{"name": "Settle", "ansible.builtin.set_fact": {"settled": True}}]}]
    scope = {}
    with pytest.raises(TaskFailed, match="Break"):
        Interpreter(ROLE, scope).run_tasks(tasks)
    assert scope["settled"] is True
    forgiven = [{"name": "Break", "ansible.builtin.fail": {}, "ignore_errors": True, "register": "broken"},
                {"name": "Next", "ansible.builtin.set_fact": {"next": True}}]
    scope = {}
    Interpreter(ROLE, scope).run_tasks(forgiven)
    assert scope["broken"]["failed"] is True and scope["next"] is True


def test_a_failed_assert_names_the_condition_and_a_command_fails_on_a_nonzero_rc_unless_failed_when_judges():
    scope = {"value": 1}
    with pytest.raises(TaskFailed) as raised:
        Interpreter(ROLE, scope).run_tasks([{"name": "Check", "ansible.builtin.assert": {"that": ["value == 2"]}}])
    assert raised.value.result["assertion"] == "value == 2"
    with pytest.raises(TaskFailed):
        Interpreter(ROLE, {}, execute(1)).run_tasks([{"name": "Run", COMMAND: "x"}])
    scope = {}
    Interpreter(ROLE, scope, execute(1)).run_tasks([{"name": "Run", COMMAND: "x", "register": "ran", "failed_when": "ran.rc == 2"}])
    assert scope["ran"]["failed"] is False


def test_include_vars_are_visible_to_the_file_and_to_nothing_after_it():
    scope = scenario(include_it=True, included_when=True, import_it=True, imported_when=True)
    Interpreter(ROLE, scope, execute()).run("main.yml")
    assert scope["included"] == "carried" and scope["imported"] == "carried"
    assert "from_include" not in scope and "from_import" not in scope


def test_a_loop_tests_its_condition_and_its_failed_when_for_each_item():
    seen = []

    def executor(task, templar):
        seen.append(templar.available_variables["item"])
        return {"changed": True, "n": templar.available_variables["item"]}
    tasks = [{"name": "Each", COMMAND: "x", "loop": [1, 2, 3], "when": "item != 2", "register": "each"}]
    scope = {}
    Interpreter(ROLE, scope, executor).run_tasks(tasks)
    assert seen == [1, 3]
    assert [entry.get("skipped", False) for entry in scope["each"]["results"]] == [False, True, False]
    assert scope["each"]["changed"] is True and scope["each"]["skipped"] is False


def test_a_task_the_executor_leaves_out_registers_nothing_and_an_unknown_task_is_refused():
    scope = {}
    interpreter = Interpreter(ROLE, scope, lambda task, templar: SKIP)
    interpreter.run_tasks([{"name": "Left", COMMAND: "x", "register": "left"}])
    assert scope == {} and interpreter.trace == [("Left", "left out")]
    with pytest.raises(Unmodelled, match="Strange"):
        Interpreter(ROLE, {}).run_tasks([{"name": "Strange", COMMAND: "x"}])


def test_run_stops_after_the_named_task_and_still_runs_always():
    tasks = [{"name": "Wrapper", "block": [{"name": "One", FACT: {"one": True}}, {"name": "Two", FACT: {"two": True}}],
              "always": [{"name": "Settle", FACT: {"settled": True}}]}]
    scope = {}
    assert Interpreter(ROLE, scope).run_tasks(tasks, through="One") is True
    assert "two" not in scope and scope["settled"] is True
    assert Interpreter(ROLE, {}).run_tasks(tasks, through="Missing") is False


def test_the_shared_loader_keeps_trusted_and_untrusted_readings_of_a_file_apart():
    path = str(ROLE / "tasks" / "included.yml")
    for order in ((False, True), (True, False)):
        seen = {trusted: role_tasks.LOADER.load_from_file(path, trusted_as_template=trusted) for trusted in order}
        assert not is_trusted_as_template(seen[False][0]["when"])
        assert is_trusted_as_template(seen[True][0]["when"])
