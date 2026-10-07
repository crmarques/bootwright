"""Structural rules every role task file keeps where a play cannot check them.

Each rule guards a property the syntax check and ansible-lint accept either
way, and that shows up only on a host: a frozen substrate no branch binds
falling through every one of them into the effects that follow, refused where
something catches the refusal, or reaching those effects before the file that
binds it is included; media inserted into a machine its substrate has not
proved; a step named as a proof that changes the machine it claims only to
read; or an ambient proxy variable winning over the route a request froze.
"""

from __future__ import annotations

import ast
import pathlib
import re

import pytest
import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"

# A dispatch compares the frozen substrate or identity channel with one arm.
DISPATCH = re.compile(r"([A-Za-z_][\w.]*\.(?:substrate|channel))\s*==\s*['\"]([^'\"]+)['\"]")

# One step of a dispatch group: a condition that is exactly one such comparison.
STEP = re.compile(r"^\s*([A-Za-z_][\w.]*\.(?:substrate|channel))\s*==\s*['\"]([^'\"]+)['\"]\s*$")

# A guard admits a closed list of arms, or refuses everything outside one.
ADMITS = re.compile(r"^\s*([A-Za-z_][\w.]*)\s+in\s+(\[[^\]]*\])\s*$")
REFUSES = re.compile(r"^\s*([A-Za-z_][\w.]*)\s+not\s+in\s+(\[[^\]]*\])\s*$")

GUARDS = ("ansible.builtin.assert", "ansible.builtin.fail")

INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")

ROLE_INCLUDES = ("ansible.builtin.include_role", "ansible.builtin.import_role")

PROXIES = ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY")

# What the terminal refusal of every dispatch ends by telling the operator.
REPLAN = "plan the installation again with this release"

# The known dispatching files, with the dispatch groups each holds: the boot
# files dispatch the pre-boot proof and the media boot, the others one entry
# point. Finding fewer means the walk stopped seeing them, which would pass
# every rule below vacuously.
GROUPS = {
    "containercluster_install_agent/tasks/boot.yml": 2,
    "containercluster_install_agent/tasks/release.yml": 1,
    "managedos_install_anaconda/tasks/await.yml": 1,
    "managedos_install_anaconda/tasks/boot.yml": 2,
    "managedos_install_anaconda/tasks/identity.yml": 1,
}

# The files that include a dispatching file, directly or through another file
# that does. Finding fewer would pass the rule that nothing catches a refusal
# vacuously.
INCLUDERS = {
    "containercluster_install_agent/tasks/apply.yml",
    "managedos_install_anaconda/tasks/apply.yml",
    "managedos_install_anaconda/tasks/identity_until_answered.yml",
    "managedos_install_anaconda/tasks/observe.yml",
}

# The files that insert virtual media. Finding fewer would pass the proof rule
# vacuously.
INSERTING = {
    "containercluster_install_agent/tasks/boot.yml",
    "managedos_install_anaconda/tasks/boot.yml",
}

# The entrypoints that include those files after effects of their own, and the
# dispatching files each includes. Finding fewer would pass the early-guard rule
# vacuously.
CONSUMED = {
    "containercluster_install_agent/tasks/apply.yml": {"boot.yml", "release.yml"},
    "managedos_install_anaconda/tasks/apply.yml": {"await.yml", "boot.yml", "identity.yml"},
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


def included_file(task):
    """The task file one task includes, or None."""
    for action in INCLUDES:
        target = task.get(action)
        if isinstance(target, dict):
            target = target.get("file")
        if isinstance(target, str):
            return target
    return None


def included(tasks):
    """Each task file one file includes, with every iteration it is included under."""
    found = {}
    for task, _names in walk(tasks):
        target = included_file(task)
        if target is not None:
            found.setdefault(target, set()).add(iteration(task))
    return found


def redfish_operation(task):
    arguments = task.get("bootwright.core.redfish_boot")
    if not isinstance(arguments, dict):
        return None
    return str(arguments.get("operation", ""))


def task_lists(tasks, around=()):
    """Every task list one file holds, with where it sits: each task list around
    it, the position of the block in that list and the section, outermost first."""
    yield tasks, around
    for position, task in enumerate(tasks):
        for section in ("block", "rescue", "always"):
            children = [child for child in task.get(section) or [] if isinstance(child, dict)]
            if children:
                yield from task_lists(children, around + ((tasks, position, section),))


def step(task):
    """The field and the one arm that alone select a task, or None."""
    found = conditions(task.get("when"))
    match = STEP.match(found[0]) if len(found) == 1 else None
    return (match.group(1), match.group(2)) if match else None


def groups(tasks):
    """Each dispatch group of one task list, a maximal run of consecutive steps
    on one field, as the field, its arms, and the positions it starts and ends at."""
    found = []
    for position, task in enumerate(tasks):
        selected = step(task)
        if selected is None:
            continue
        if found and found[-1][3] == position and found[-1][0] == selected[0]:
            found[-1][1].append(selected[1])
            found[-1][3] = position + 1
        else:
            found.append([selected[0], [selected[1]], position, position + 1])
    return [tuple(group) for group in found]


def refused(task, field):
    """The arms a fail leaves one field, refusing every other value, or None when
    the task is not a fail that refuses them whatever else happens."""
    if "ansible.builtin.fail" not in task or tolerates_failure(task):
        return None
    found = conditions(task.get("when"))
    match = REFUSES.match(found[0]) if len(found) == 1 else None
    if match is None or match.group(1) != field:
        return None
    return ast.literal_eval(match.group(2))


def closes(task, field, arms):
    """Whether a task refuses every value of one field outside exactly these arms."""
    left = refused(task, field)
    return left is not None and sorted(left) == sorted(set(arms))


def replans(task):
    message = (task.get("ansible.builtin.fail") or {}).get("msg", "")
    return " ".join(str(message).split()).endswith(REPLAN)


def unconditional_fail(task):
    return "ansible.builtin.fail" in task and "when" not in task and not tolerates_failure(task)


def catcher(around):
    """What around one task list could keep a failure in it from ending the run,
    or None: a block that tolerates failure, or one whose rescue that failure
    reaches and that can complete."""
    for parent, position, section in around:
        block = parent[position]
        rescue = [child for child in block.get("rescue") or [] if isinstance(child, dict)]
        if tolerates_failure(block):
            return "block %r tolerates failure" % block.get("name")
        if section == "block" and rescue and not unconditional_fail(rescue[-1]):
            return "the rescue of block %r can complete" % block.get("name")
    return None


def dispatch_problems(where, tasks):
    """Why a dispatch in one file might not end in a refusal that nothing catches."""
    problems = []
    for listed, around in task_lists(tasks):
        for field, arms, start, end in groups(listed):
            head = "%s: the dispatch on %s at %r" % (where, field, listed[start].get("name"))
            following = listed[end] if end < len(listed) else {}
            if refused(following, field) is None:
                problems.append(
                    "%s is not followed directly by a fail that refuses every other value and tolerates no "
                    "failure, so an arm it does not bind falls through it" % head)
            elif not closes(following, field, arms):
                problems.append("%s binds %s, but the fail after it refuses everything outside %s"
                                % (head, sorted(set(arms)), sorted(refused(following, field))))
            elif not replans(following):
                problems.append("%s ends in a fail that does not end by telling the operator to %s" % (head, REPLAN))
            caught = catcher(around)
            if caught:
                problems.append("%s sits where %s, so its refusal is caught" % (head, caught))
        for task in listed:
            if compared_arms(task) and step(task) is None:
                problems.append("%s: %r selects by %s in a condition that is not exactly one arm, so no refusal "
                                "closes it" % (where, task.get("name"), sorted(compared_arms(task))))
    return problems


def reaching(files):
    """The names of one role's task files whose run reaches a dispatch: each one
    that dispatches, and each that includes one of those."""
    found = {name for name, tasks in files.items() if dispatches(tasks)}
    while True:
        more = {name for name, tasks in files.items()
                if name not in found and any(included_file(task) in found for task, _names in walk(tasks))}
        if not more:
            return found
        found |= more


def inclusion_problems(where, tasks, reached):
    """Why the refusal of an included file that reaches a dispatch might be caught."""
    problems = []
    for listed, around in task_lists(tasks):
        for task in listed:
            target = included_file(task)
            if target not in reached:
                continue
            head = "%s: %r includes %s, whose run ends a dispatch in a refusal," % (where, task.get("name"), target)
            if tolerates_failure(task):
                problems.append("%s and tolerates its failure" % head)
            caught = catcher(around)
            if caught:
                problems.append("%s where %s" % (head, caught))
    return problems


def proves(task):
    """Whether a task includes a substrate's pre-boot proof."""
    return any(isinstance(task.get(action), dict) and task[action].get("tasks_from") == "pre_boot"
               for action in ROLE_INCLUDES)


def proof_before(listed, until, bound):
    """Whether a task list holds, before position until, a complete pre-boot
    group, one proof for each arm the file binds, followed directly by its
    refusal."""
    for field, arms, start, end in groups(listed):
        if (end < until and all(proves(task) for task in listed[start:end])
                and sorted(arms) == sorted(bound.get(field, ())) and closes(listed[end], field, arms)):
            return True
    return False


def insert_problems(where, tasks):
    """Why an insert in one file might reach a machine its substrate has not proved.

    The proof counts only in the insert's own task list or one around it, so no
    block, and no condition of one, can enclose the proof without the insert.
    """
    problems = []
    bound = dispatches(tasks)
    for listed, around in task_lists(tasks):
        for position, task in enumerate(listed):
            if redfish_operation(task) != "insert":
                continue
            scopes = [(entry[0], entry[1]) for entry in around] + [(listed, position)]
            if not any(proof_before(scope, until, bound) for scope, until in scopes):
                problems.append(
                    "%s: %r inserts media with no complete pre-boot proof and its refusal before it, in its own "
                    "task list or one around it, so a machine its substrate has not proved can be given media"
                    % (where, task.get("name")))
    return problems


def test_the_known_dispatching_files_are_seen():
    seen = {label(path) for path in task_files() if dispatches(load(path))}
    assert set(GROUPS) <= seen, "the walk no longer sees %s" % sorted(set(GROUPS) - seen)


# A refusal of a value no entry point binds must end the run: a dispatch that
# falls through every arm, or a refusal something catches, reads as a machine
# that was handled, and the effects after it run.
def test_every_dispatch_ends_in_a_refusal_nothing_catches():
    problems, seen = [], {}
    for path in task_files():
        tasks = load(path)
        problems.extend(dispatch_problems(label(path), tasks))
        seen[label(path)] = sum(len(groups(listed)) for listed, _around in task_lists(tasks))
    short = {name: seen.get(name, 0) for name, count in GROUPS.items() if seen.get(name, 0) < count}
    assert not short, "the walk no longer sees every dispatch group of %s" % short
    assert not problems, "\n".join(problems)


def test_nothing_catches_the_refusal_of_a_file_a_task_includes():
    problems, seen = [], set()
    files = {}
    for path in task_files():
        files.setdefault(path.parent, {})[path.name] = load(path)
    for directory, role in sorted(files.items()):
        reached = reaching(role)
        for name, tasks in sorted(role.items()):
            problems.extend(inclusion_problems(label(directory / name), tasks, reached))
            if any(included_file(task) in reached for task, _names in walk(tasks)):
                seen.add(label(directory / name))
    assert INCLUDERS <= seen, "the walk no longer sees %s include a dispatch" % sorted(INCLUDERS - seen)
    assert not problems, "\n".join(problems)


def test_every_insert_follows_a_complete_pre_boot_proof():
    problems, seen = [], set()
    for path in task_files():
        tasks = load(path)
        if any(redfish_operation(task) == "insert" for task, _names in walk(tasks)):
            seen.add(label(path))
        problems.extend(insert_problems(label(path), tasks))
    assert INSERTING <= seen, "the walk no longer sees %s insert media" % sorted(INSERTING - seen)
    assert not problems, "\n".join(problems)


# Synthetic task lists, shaped as the boot files are, show that each rule
# rejects what it guards against rather than passing every file it reads.
FIELD = "request.target.substrate"
ARMS = ("libvirt", "baremetal")
INSERT = {"name": "Insert the image", "bootwright.core.redfish_boot": {"operation": "insert", "image": "http://192.0.2.1/i.iso"}}
TIMED_OUT = {"name": "Report a spent budget", "ansible.builtin.fail": {"msg": "spent"},
             "when": "ansible_failed_result.timedout is defined"}
KEPT = {"name": "Keep any other failure a failure", "ansible.builtin.fail": {"msg": "not booted"}}


def entry(arm, point, when=None):
    """An include of one arm's entry point, selected by that arm alone unless told otherwise."""
    return {"name": "%s through %s" % (point, arm),
            "ansible.builtin.include_role": {"name": "bootwright.core.substrate_%s_machine" % arm, "tasks_from": point},
            "when": "%s == '%s'" % (FIELD, arm) if when is None else when}


def refusal(arms=ARMS, message="nothing was inserted or booted; " + REPLAN, **keywords):
    task = {"name": "Refuse a substrate with no binding", "ansible.builtin.fail": {"msg": message},
            "when": "%s not in %r" % (FIELD, list(arms))}
    task.update(keywords)
    return task


def proof(arms=ARMS):
    return [entry(arm, "pre_boot") for arm in arms] + [refusal(arms)]


def media(ending=None):
    return [entry(arm, "boot_media") for arm in ARMS] + [refusal() if ending is None else ending]


def within(tasks, **keywords):
    block = {"name": "Boot the node", "block": tasks}
    block.update(keywords)
    return block


@pytest.mark.parametrize("tasks", [
    proof() + [INSERT] + media(),
    [within(proof() + [INSERT] + media(), rescue=[TIMED_OUT, KEPT])],
    proof() + [within([INSERT] + media(), rescue=[KEPT])],
], ids=["flat", "under a rescue that fails", "proved around the insert"])
def test_a_proved_boot_that_ends_each_dispatch_in_a_refusal_passes_every_rule(tasks):
    assert not dispatch_problems("sound", tasks)
    assert not insert_problems("sound", tasks)


@pytest.mark.parametrize("tasks, reason", [
    (proof() + [INSERT] + media()[:-1], "is not followed directly by a fail"),
    (proof() + [INSERT] + media(refusal(("libvirt",))), "refuses everything outside ['libvirt']"),
    (proof() + [INSERT] + media(refusal(ignore_errors=True)), "is not followed directly by a fail"),
    (proof() + [INSERT] + media(refusal(failed_when=False)), "is not followed directly by a fail"),
    (proof() + [INSERT] + media(refusal(message="nothing was booted")), "does not end by telling the operator"),
    ([within(proof() + [INSERT] + media(), rescue=[KEPT, TIMED_OUT])], "the rescue of block 'Boot the node' can complete"),
    ([within(proof() + [INSERT] + media(), rescue=[{"name": "Report", "ansible.builtin.debug": {"msg": "failed"}}])],
     "the rescue of block 'Boot the node' can complete"),
    ([within(proof() + [INSERT] + media(), ignore_errors=True)], "block 'Boot the node' tolerates failure"),
    (proof() + [INSERT, entry("libvirt", "boot_media", when=FIELD + " == 'libvirt' and request.fresh")],
     "is not exactly one arm"),
], ids=["no terminal fail", "a fail that closes other arms", "a fail that ignores errors",
        "a fail that cannot fail", "a fail that does not say to plan again", "a rescue that can complete",
        "a rescue that ends in no fail", "a block that ignores errors", "a condition wider than one arm"])
def test_the_dispatch_rule_rejects(tasks, reason):
    problems = dispatch_problems("synthetic", tasks)
    assert any(reason in problem for problem in problems), problems


@pytest.mark.parametrize("tasks", [
    [INSERT] + media(),
    [INSERT] + proof() + media(),
    proof(("libvirt",)) + [INSERT] + media(),
    [entry(arm, "pre_boot", when=["%s == '%s'" % (FIELD, arm), "request.fresh"]) for arm in ARMS]
    + [refusal(), INSERT] + media(),
    [within(proof(), when="request.fresh"), INSERT] + media(),
    [within(proof()), INSERT] + media(),
    proof()[:-1] + [INSERT] + media(),
], ids=["no proof before the insert", "a proof after the insert", "a proof of one arm only",
        "a proof under a narrower condition", "a proof under a narrower block", "a proof in a block of its own",
        "a proof with no refusal"])
def test_the_insert_rule_rejects(tasks):
    problems = insert_problems("synthetic", tasks)
    assert any("inserts media with no complete pre-boot proof" in problem for problem in problems), problems


def test_a_dispatch_reaches_every_file_that_includes_it_however_deep():
    files = {"apply.yml": [{"name": "Wait", "ansible.builtin.include_tasks": "wait.yml"}],
             "wait.yml": [{"name": "Boot", "ansible.builtin.include_tasks": {"file": "boot.yml"}}],
             "boot.yml": proof() + [INSERT] + media(),
             "state.yml": [{"name": "Read", "ansible.builtin.debug": {"msg": "state"}}]}
    assert reaching(files) == {"apply.yml", "wait.yml", "boot.yml"}


@pytest.mark.parametrize("tasks, reason", [
    ([{"name": "Boot", "ansible.builtin.include_tasks": "boot.yml", "ignore_errors": True}], "tolerates its failure"),
    ([{"name": "Boot", "ansible.builtin.include_tasks": "boot.yml", "failed_when": False}], "tolerates its failure"),
    ([within([{"name": "Boot", "ansible.builtin.include_tasks": "boot.yml"}], rescue=[TIMED_OUT])],
     "the rescue of block 'Boot the node' can complete"),
], ids=["an include that ignores errors", "an include that cannot fail", "an include under a rescue that can complete"])
def test_the_inclusion_rule_rejects(tasks, reason):
    assert not inclusion_problems("sound", [{"name": "Boot", "ansible.builtin.include_tasks": "boot.yml"}], {"boot.yml"})
    problems = inclusion_problems("synthetic", tasks, {"boot.yml"})
    assert any(reason in problem for problem in problems), problems


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
            if operation is None:
                continue
            proving = [name for name in names if name.startswith("Prove")]
            if proving:
                problems.append(
                    "%s: %r performs %s through its controller; a proof reads and never drives"
                    % (label(path), proving[-1], operation or "an unnamed operation")
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


def rescue_problems(where, tasks):
    """Why a rescue in one file might complete and let the run continue past the failure it caught."""
    problems = []
    for task, _names in walk(tasks):
        if "rescue" not in task:
            continue
        rescue = [child for child in task.get("rescue") or [] if isinstance(child, dict)]
        if not rescue or not unconditional_fail(rescue[-1]):
            problems.append(
                "%s: the rescue of block %r does not end in an unconditional ansible.builtin.fail, so a failure "
                "it catches can complete" % (where, task.get("name"))
            )
    return problems


def test_every_rescue_ends_in_a_fail_nothing_skips():
    problems = []
    for path in task_files():
        problems.extend(rescue_problems(label(path), load(path)))
    assert not problems, "\n".join(problems)


RESCUED = {"name": "Run the effect", "ansible.builtin.command": {"argv": ["/usr/bin/true"]}}


@pytest.mark.parametrize("last", [
    {"name": "Refuse", "ansible.builtin.fail": {"msg": "failed"}, "when": "item is defined"},
    {"name": "Report", "ansible.builtin.debug": {"msg": "failed"}},
    {"name": "Refuse", "ansible.builtin.fail": {"msg": "failed"}, "ignore_errors": True},
], ids=["conditional fail", "debug", "tolerated fail"])
def test_the_rescue_rule_rejects(last):
    tasks = [{"name": "Guarded", "block": [RESCUED], "rescue": [{"name": "Collect", "ansible.builtin.debug": {}}, last]}]
    assert rescue_problems("fixture", tasks)


def test_the_rescue_rule_accepts():
    tasks = [{"name": "Guarded", "block": [RESCUED], "rescue": [{"name": "Refuse", "ansible.builtin.fail": {"msg": "failed"}}]}]
    assert not rescue_problems("fixture", tasks)
