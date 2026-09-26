"""Structural rules every role task file keeps where a play cannot check them.

Each rule guards a property the syntax check and ansible-lint accept either
way, and that shows up only on a host: a frozen substrate no branch binds
falling through every one of them into the effects that follow, or reaching
them before the file that binds it is included, a step named as a proof that
changes the machine it claims only to read, or an ambient proxy variable
winning over the route a request froze.
"""

from __future__ import annotations

import ast
import pathlib
import re

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"

# A dispatch compares the frozen substrate or identity channel with one arm.
DISPATCH = re.compile(r"([A-Za-z_][\w.]*\.(?:substrate|channel))\s*==\s*['\"]([^'\"]+)['\"]")

# A guard admits a closed list of arms, or refuses everything outside one.
ADMITS = re.compile(r"^\s*([A-Za-z_][\w.]*)\s+in\s+(\[[^\]]*\])\s*$")
REFUSES = re.compile(r"^\s*([A-Za-z_][\w.]*)\s+not\s+in\s+(\[[^\]]*\])\s*$")

GUARDS = ("ansible.builtin.assert", "ansible.builtin.fail")

INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")

# Either operation leaves a machine powered off.
POWER_OFF = ("power-off", "shutdown")

PROXIES = ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY")

# The known dispatching files. Finding fewer means the walk stopped seeing them,
# which would pass every rule below vacuously.
DISPATCHING = {
    "containercluster_install_agent/tasks/boot.yml",
    "managedos_install_anaconda/tasks/boot.yml",
    "managedos_install_anaconda/tasks/identity.yml",
}

# The entrypoints that include those files after effects of their own, and the
# dispatching files each includes. Finding fewer would pass the early-guard rule
# vacuously.
CONSUMED = {
    "containercluster_install_agent/tasks/apply.yml": {"boot.yml"},
    "managedos_install_anaconda/tasks/apply.yml": {"boot.yml", "identity.yml"},
}


def task_files():
    return sorted(ROLES.glob("*/tasks/*.yml"))


def label(path):
    return path.relative_to(ROLES).as_posix()


def load(path):
    parsed = yaml.safe_load(path.read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def walk(tasks, names=()):
    """Every task with the names of the blocks around it, outermost first."""
    for task in tasks:
        current = names + (str(task.get("name", "")),)
        yield task, current
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)], current)


def conditions(value):
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        return [str(item) for item in value]
    return []


def compared_arms(task):
    """The arms each field one task's conditions compare with."""
    found = {}
    for condition in conditions(task.get("when")):
        for field, arm in DISPATCH.findall(condition):
            found.setdefault(field, set()).add(arm)
    return found


def dispatches(tasks):
    """The arms each dispatched field is compared with anywhere in one file."""
    found = {}
    for task, _names in walk(tasks):
        for field, arms in compared_arms(task).items():
            found.setdefault(field, set()).update(arms)
    return found


def iteration(task):
    """The list one task iterates and the name each item takes, or None."""
    if "loop" not in task:
        return None
    return str(task["loop"]), str((task.get("loop_control") or {}).get("loop_var", "item"))


def hands_off(task):
    """Whether one task is the qualified execution handoff, which reports that
    the entrypoint loaded and changes nothing."""
    return any(
        action.startswith("bootwright.core.")
        and action.endswith("_protocol")
        and isinstance(arguments, dict)
        and arguments.get("phase") == "loaded"
        for action, arguments in task.items()
    )


def tolerates_failure(task):
    """Whether one task is told to succeed whatever its own outcome."""
    return "failed_when" in task or task.get("ignore_errors", False) is not False


def leading_guards(tasks):
    """The arms each field is closed to before the file's first effect.

    Each is keyed by the field and the iteration of the guard that closed it,
    because a guard that loops closes the field only for the items it names.
    """
    closed = {}
    for task in tasks:
        if hands_off(task):
            continue
        action = next((name for name in GUARDS if name in task), None)
        if action is None:
            break
        # A guard that may fail without refusing closes nothing.
        if tolerates_failure(task):
            continue
        if action == "ansible.builtin.assert":
            # An assertion that may be skipped closes nothing.
            if "when" in task:
                continue
            candidates = [ADMITS.match(item) for item in conditions((task[action] or {}).get("that"))]
        else:
            candidates = [REFUSES.match(item) for item in conditions(task.get("when"))]
        for match in candidates:
            if match:
                closed[(match.group(1), iteration(task))] = set(ast.literal_eval(match.group(2)))
    return closed


def included(tasks):
    """Each task file one file includes, with every iteration it is included under."""
    found = {}
    for task, _names in walk(tasks):
        for action in INCLUDES:
            target = task.get(action)
            if isinstance(target, dict):
                target = target.get("file")
            if isinstance(target, str):
                found.setdefault(target, set()).add(iteration(task))
    return found


def redfish_operation(task):
    arguments = task.get("bootwright.core.redfish_boot")
    if not isinstance(arguments, dict):
        return None
    return str(arguments.get("operation", ""))


def test_the_known_dispatching_files_are_seen():
    seen = {label(path) for path in task_files() if dispatches(load(path))}
    assert DISPATCHING <= seen, "the walk no longer sees %s" % sorted(DISPATCHING - seen)


def test_every_dispatch_refuses_an_arm_it_does_not_bind_before_any_effect():
    problems = []
    for path in task_files():
        tasks = load(path)
        closed = leading_guards(tasks)
        for field, arms in sorted(dispatches(tasks).items()):
            if (field, None) not in closed:
                problems.append(
                    "%s dispatches on %s with no guard before its first effect, so an arm it does not bind "
                    "falls through every branch" % (label(path), field)
                )
            elif closed[(field, None)] != arms:
                problems.append(
                    "%s closes %s to %s but binds %s"
                    % (label(path), field, sorted(closed[(field, None)]), sorted(arms))
                )
    assert not problems, "\n".join(problems)


def test_every_consumer_refuses_an_arm_it_does_not_bind_before_its_own_first_effect():
    problems = []
    checked = {}
    for path in task_files():
        if path.name != "apply.yml":
            continue
        tasks = load(path)
        closed = leading_guards(tasks)
        for name, iterations in sorted(included(tasks).items()):
            target = path.parent / name
            if not target.is_file():
                continue
            arms_by_field = dispatches(load(target))
            if arms_by_field:
                checked.setdefault(label(path), set()).add(name)
            for field, arms in sorted(arms_by_field.items()):
                for each in sorted(iterations, key=str):
                    if closed.get((field, each)) != arms:
                        problems.append(
                            "%s includes %s, which dispatches on %s, without closing it to %s before its own "
                            "first effect, so an arm %s does not bind reaches those effects first"
                            % (label(path), name, field, sorted(arms), name)
                        )
    missing = {entry: sorted(names - checked.get(entry, set())) for entry, names in CONSUMED.items()}
    missing = {entry: names for entry, names in missing.items() if names}
    assert not missing, "the walk no longer sees these consumers include %s" % missing
    assert not problems, "\n".join(problems)


def test_no_proof_step_drives_a_machine():
    problems = []
    for path in task_files():
        for task, names in walk(load(path)):
            operation = redfish_operation(task)
            if operation is None or operation == "read":
                continue
            proving = [name for name in names if name.startswith("Prove")]
            if proving:
                problems.append(
                    "%s: %r performs %s through its controller; a proof reads and never drives"
                    % (label(path), proving[-1], operation or "an unnamed operation")
                )
    assert not problems, "\n".join(problems)


def test_an_installation_never_powers_off_an_operator_owned_machine():
    problems = []
    for path in sorted((ROLES / "managedos_install_anaconda" / "tasks").glob("*.yml")):
        for task, names in walk(load(path)):
            operation = redfish_operation(task)
            if operation not in POWER_OFF:
                continue
            arms = set().union(*compared_arms(task).values())
            if arms != {"libvirt"}:
                problems.append(
                    "%s: %r performs %s on %s"
                    % (label(path), names[-1], operation, sorted(arms) or "every substrate")
                )
    assert not problems, "\n".join(problems)


def test_a_frozen_proxy_route_sets_every_spelling_of_its_variables():
    problems = []
    for path in task_files():
        for task, names in walk(load(path)):
            environment = task.get("environment")
            if not isinstance(environment, dict):
                continue
            for upper in PROXIES:
                lower = upper.lower()
                if (upper in environment or lower in environment) and environment.get(upper) != environment.get(lower):
                    problems.append(
                        "%s: %r sets %s and %s differently, so the ambient spelling can win"
                        % (label(path), names[-1], upper, lower)
                    )
    assert not problems, "\n".join(problems)
