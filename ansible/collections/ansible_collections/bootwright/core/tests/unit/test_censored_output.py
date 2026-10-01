"""Nothing a task under no_log raised reaches the adapter's output.

ansible-core censors a hidden task's result but keeps the error, warnings and
deprecations the task raised, and ansible.builtin.default prints them whole.
The shipped ansible.cfg names bootwright.core.censored as the adapter's stdout
callback instead (ansible/configuration_test.go holds it to that). These cases
run one play through ansible-playbook under each callback, with what the
runners set for the adapter's streams (ansible/ansible.cfg,
internal/reconciliation/ansiblerunner/process_linux_amd64.go): no verbosity,
no task arguments displayed, no color, and standard output and error in one
stream. The values arrive as extra variables, as a run's request does, so no
value is in the playbook text an error's origin quotes. Each shape runs once
under no_log and once in the open, and each task reads a value of its own,
because ansible-core prints a message it already printed only once per run.

Under the default every hidden shape prints the value, which proves each shape
is one that leaks; under the censored callback none does, each hidden failure
still reports that it failed with ansible-core's censored result, and a task in
the open prints exactly what the default prints.

Running a playbook needs Ansible's controller, which ansible-test does not
offer to unit tests under tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]
CALLBACKS = ("ansible.builtin.default", "bootwright.core.censored")
VALUE = "bound-material-4f1c9a"

# Each shape by what it raises, with whether it fails. The warning comes from a
# module of the play's own, because no module this collection runs is known to
# warn with a value.
SHAPES = {
    "an assertion whose message templates a value": (
        {"ansible.builtin.assert": {"that": ["false"], "fail_msg": "refused {{ material }}"}}, True),
    "a module error naming an argument": (
        {"ansible.builtin.file": {"path": "{{ material }}", "state": "file"}}, True),
    "a lookup error naming a value": (
        {"ansible.builtin.debug": {"msg": "{{ lookup('ansible.builtin.file', material) }}"}}, True),
    "a template error naming a value": (
        {"ansible.builtin.set_fact": {"read": "{{ {}[material] }}"}}, True),
    "a loop item's module error naming an argument": (
        {"ansible.builtin.file": {"path": "{{ item }}", "state": "file"}, "loop": ["{{ material }}"]}, True),
    "a module warning naming an argument": ({"warned": {"text": "{{ material }}"}}, False),
}
WARNED = """#!/usr/bin/python
from __future__ import annotations
from ansible.module_utils.basic import AnsibleModule
module = AnsibleModule(argument_spec={"text": {"type": "str", "required": True}})
module.warn("warned about " + module.params["text"])
module.exit_json(changed=False)
"""
FAILED = ("fatal: [localhost]: FAILED! => ", "failed: [localhost] (item=(censored due to no_log)) => ")


def hidden(shape):
    return "hidden: " + shape


def shown(shape):
    return "shown: " + shape


@pytest.fixture(scope="module")
def printed(tmp_path_factory):
    """What each task printed under each callback, by callback and task name."""
    work = tmp_path_factory.mktemp("censored")
    (work / "library").mkdir()
    (work / "library" / "warned.py").write_text(WARNED)
    tasks = []
    for shape, (task, _fails) in SHAPES.items():
        tasks.append(dict(task, name=hidden(shape), no_log=True, ignore_errors=True))
        tasks.append(dict(task, name=shown(shape), ignore_errors=True))
    for index, task in enumerate(tasks):
        task["vars"] = {"material": "{{ materials[%d] }}" % index}
    values = ["%s-%02d" % (VALUE, index) for index in range(len(tasks))]
    (work / "request.json").write_text(json.dumps({"materials": values}))
    (work / "play.yml").write_text(json.dumps([{
        "hosts": "localhost", "gather_facts": False, "connection": "local",
        "vars": {"ansible_python_interpreter": sys.executable}, "tasks": tasks,
    }], indent=1))
    (work / "ansible.cfg").write_text("")
    found = {}
    for callback in CALLBACKS:
        environment = dict(
            os.environ,
            ANSIBLE_CONFIG=str(work / "ansible.cfg"),
            ANSIBLE_HOME=str(work / "home"),
            ANSIBLE_LOCAL_TEMP=str(work / "local"),
            ANSIBLE_COLLECTIONS_PATH=str(COLLECTIONS),
            ANSIBLE_STDOUT_CALLBACK=callback,
            ANSIBLE_DISPLAY_ARGS_TO_STDOUT="False",
            ANSIBLE_LOAD_CALLBACK_PLUGINS="0",
            ANSIBLE_NOCOLOR="1",
            ANSIBLE_FORCE_COLOR="0",
            LC_ALL="C.UTF-8",
        )
        ran = subprocess.run(
            [sys.executable, "-m", "ansible.cli.playbook", "-i", "localhost,",
             "--extra-vars", "@" + str(work / "request.json"), str(work / "play.yml")],
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=environment,
            cwd=str(work), check=False)
        output = ran.stdout.decode()
        sections = {}
        for section in output.split("TASK [")[1:]:
            name, _banner, body = section.partition("]")
            sections[name] = body.split("PLAY RECAP", 1)[0]
        assert ran.returncode == 0 and set(sections) == {task["name"] for task in tasks}, output
        found[callback] = sections
    return found


@pytest.mark.parametrize("shape", list(SHAPES))
def test_the_default_callback_prints_the_value_a_hidden_task_raised(printed, shape):
    assert VALUE in printed["ansible.builtin.default"][hidden(shape)]


@pytest.mark.parametrize("shape", list(SHAPES))
def test_the_censored_callback_prints_nothing_a_hidden_task_raised(printed, shape):
    output = printed["bootwright.core.censored"][hidden(shape)]
    assert VALUE not in output and "[ERROR]" not in output and "[WARNING]" not in output, output
    reported = [json.loads(line.partition(" => ")[2]) for line in output.splitlines() if line.startswith(FAILED)]
    if SHAPES[shape][1]:
        assert reported and all("censored" in result for result in reported), output
    else:
        assert not reported and "ok: [localhost]" in output, output


@pytest.mark.parametrize("shape", list(SHAPES))
def test_the_censored_callback_prints_a_task_in_the_open_as_the_default_does(printed, shape):
    output = printed["bootwright.core.censored"][shown(shape)]
    assert VALUE in output
    assert output == printed["ansible.builtin.default"][shown(shape)]
