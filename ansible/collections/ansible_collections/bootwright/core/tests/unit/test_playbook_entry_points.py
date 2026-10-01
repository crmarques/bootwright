"""Every playbook validates its request before its entry point's first task.

Each playbook is one port entrypoint that validates its request
(specs/architecture.md, Playbooks). ansible-core prepends the validation of a
role's argument specification to an entry point only when the import names it
by the key meta/argument_specs.yml declares: one written with its extension
finds no specification and runs the same file unvalidated
(.agents/knowledge/ansible-role-argument-specs.md). These cases load every
playbook through ansible-core's own loader and require the first task each play
reaches to be that validation, against its entry point's own specification;
then they run every playbook's play through ansible-playbook with a request of
a version its role does not admit, and require the validation to refuse it
before any task of the entry point runs.

Loading and running a playbook needs Ansible's controller, which ansible-test
does not offer to unit tests under tests/unit/plugins, so these checks live
here.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.playbook import Playbook
from ansible.vars.manager import VariableManager

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
COLLECTIONS = COLLECTION.parents[2]
PLAYBOOKS = COLLECTION / "playbooks"
ROLES = COLLECTION / "roles"
ROLE_IMPORTS = ("ansible.builtin.import_role", "ansible.builtin.include_role")
VALIDATION = "ansible.builtin.validate_argument_spec"

# Every playbook the collection ships. Finding fewer means the walk stopped
# seeing them, which would pass these rules vacuously.
PLAYBOOK_COUNT = 33

# The version no role admits: each role admits exactly one request version,
# its own (TestRoleVersionAssertionsMatchTheirArgumentSpecs).
UNADMITTED = "unadmitted-v0"


def playbooks():
    found = sorted(path.relative_to(PLAYBOOKS).as_posix() for path in PLAYBOOKS.glob("*/*.yml"))
    assert len(found) == PLAYBOOK_COUNT, "the walk no longer sees every playbook"
    return found


def imported(playbook):
    """The one role and entry point a playbook's one play imports."""
    plays = yaml.safe_load((PLAYBOOKS / playbook).read_text())
    assert len(plays) == 1, "%s is not one play" % playbook
    found = [task[name] for task in plays[0]["tasks"] for name in ROLE_IMPORTS if name in task]
    assert len(found) == 1, "%s imports %d roles" % (playbook, len(found))
    return found[0]["name"].rsplit(".", 1)[-1], str(found[0].get("tasks_from", "main"))


def specification(role, entry):
    """The options an entry point declares, or None when its role declares none by that key."""
    declared = yaml.safe_load((ROLES / role / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    return declared[entry]["options"] if entry in declared else None


def reached(blocks):
    """Every task a play's compiled blocks reach, in order, blocks opened."""
    for block in blocks:
        for task in block.block:
            if hasattr(task, "block"):
                yield from reached([task])
            else:
                yield task


def first_task(playbook):
    """The first task other than a meta task that ansible-core loads for a playbook's play."""
    loader = DataLoader()
    plays = Playbook.load(str(PLAYBOOKS / playbook), variable_manager=VariableManager(loader=loader),
                          loader=loader).get_plays()
    assert len(plays) == 1, "%s is not one play" % playbook
    return next(task for task in reached(plays[0].compile()) if task.action != "meta")


@pytest.mark.parametrize("playbook", playbooks())
def test_the_first_task_of_every_playbook_validates_its_entry_points_arguments(playbook):
    role, entry = imported(playbook)
    options = specification(role, entry)
    assert options is not None, "%s imports %s with tasks_from %r, which its argument specs do not declare" % (
        playbook, role, entry)
    task = first_task(playbook)
    assert task.action == VALIDATION, "%s runs %r before validating its request" % (playbook, task.get_name())
    assert task.args["validate_args_context"]["name"] == role
    assert task.args["validate_args_context"]["argument_spec_name"] == entry
    assert task.args["argument_spec"] == options


def mismatched():
    """A request of a version no role admits under every request variable, with
    the material mapping and digest a run hands the adapter beside it."""
    variables = {}
    for playbook in playbooks():
        options = specification(*imported(playbook))
        for name in options or {}:
            if name.endswith("_request"):
                prefix = name[:-len("_request")]
                variables.update({name: {"version": UNADMITTED}, prefix + "_material": {}, prefix + "_digest": "c" * 64})
    return variables


def guarded(playbook):
    """A playbook's one play, named for the playbook, its tasks under a rescue
    that reports the refusal, so each play's refusal leaves the next play to run."""
    play = dict(yaml.safe_load((PLAYBOOKS / playbook).read_text())[0], name=playbook)
    play["tasks"] = [{"block": play["tasks"], "rescue": [{"name": "Report the refusal", "ansible.builtin.debug": {
        "msg": "refused"}}]}]
    return play


@pytest.fixture(scope="module")
def printed(tmp_path_factory):
    """What each play printed, by playbook, run as a runner runs one: the frozen
    request as extra variables and one target in the play's host group."""
    work = tmp_path_factory.mktemp("entry-points")
    (work / "request.json").write_text(json.dumps(mismatched()))
    (work / "plays.yml").write_text(json.dumps([guarded(playbook) for playbook in playbooks()], indent=1))
    (work / "inventory.json").write_text(json.dumps({
        "bootwright_controller": {"hosts": {"target": {}}},
        "bootwright_target": {"hosts": {"target": {"ansible_connection": "local", "ansible_python_interpreter": sys.executable}}},
    }))
    (work / "ansible.cfg").write_text("")
    environment = dict(
        os.environ,
        ANSIBLE_CONFIG=str(work / "ansible.cfg"),
        ANSIBLE_HOME=str(work / "home"),
        ANSIBLE_LOCAL_TEMP=str(work / "local"),
        ANSIBLE_COLLECTIONS_PATH=str(COLLECTIONS),
        ANSIBLE_STDOUT_CALLBACK="bootwright.core.censored",
        ANSIBLE_LOAD_CALLBACK_PLUGINS="0",
        ANSIBLE_NOCOLOR="1",
        ANSIBLE_FORCE_COLOR="0",
        LC_ALL="C.UTF-8",
    )
    ran = subprocess.run(
        [sys.executable, "-m", "ansible.cli.playbook", "-i", str(work / "inventory.json"),
         "--extra-vars", "@" + str(work / "request.json"), str(work / "plays.yml")],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=environment, cwd=str(work),
        check=False)
    output = ran.stdout.decode()
    sections = {}
    for section in output.split("PLAY [")[1:]:
        name, _banner, body = section.partition("]")
        sections[name] = body.split("PLAY RECAP", 1)[0]
    assert sorted(sections) == playbooks(), output
    return sections


@pytest.mark.parametrize("playbook", playbooks())
def test_a_request_the_role_does_not_admit_refuses_before_the_entry_points_first_task(printed, playbook):
    role, entry = imported(playbook)
    output = printed[playbook]
    ran = [section.partition("]")[0] for section in output.split("TASK [")[1:]]
    assert len(ran) == 2 and ran[1] == "Report the refusal", output
    assert ran[0].startswith("bootwright.core.%s : Validating arguments against arg spec '%s'" % (role, entry)), output
    failed = [json.loads(line.partition("FAILED! => ")[2]) for line in output.splitlines() if "FAILED! => " in line]
    assert len(failed) == 1, output
    variable = next(name for name in specification(role, entry) if name.endswith("_request"))
    assert "value of version must be one of: %s, got: %s found in %s" % (
        specification(role, entry)[variable]["options"]["version"]["choices"][0], UNADMITTED, variable
    ) in failed[0]["argument_errors"], output
