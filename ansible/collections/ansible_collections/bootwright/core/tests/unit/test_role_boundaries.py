"""Structural rules every role keeps at its boundaries, where a play cannot check them.

These are the structural rules test_role_structure.py does not hold. Each
guards a property the syntax check and ansible-lint accept either way and that
shows up only on a host: an entry point that acts before it hands the runner
its loaded record, or hands it twice; bound material or a completion's
evidence reaching the adapter's own output; an executable found through the
search path, or started through a shell or a wrapper the rule does not name; a
container that runs its image's own entrypoint, or a debugger; a poll that one
failed read ends instead of spending an attempt; and a management controller's
own refusal hidden by the no_log that protects its credential, or reported by a
step that never fires, or only after another step acted on what was refused.
"""

from __future__ import annotations

import pathlib
import re

import pytest
import yaml
from ansible.errors import AnsibleError
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLES = COLLECTION / "roles"
PLAYBOOKS = COLLECTION / "playbooks"

# Every key a task may carry beside its one action.
KEYWORDS = frozenset({
    "always", "any_errors_fatal", "args", "become", "become_user", "block", "changed_when", "check_mode",
    "delay", "delegate_to", "diff", "environment", "failed_when", "ignore_errors", "loop", "loop_control",
    "name", "no_log", "notify", "register", "rescue", "retries", "run_once", "tags", "throttle", "timeout",
    "until", "vars", "when",
})

ROLE_IMPORTS = ("ansible.builtin.import_role", "ansible.builtin.include_role")
INCLUDES = ROLE_IMPORTS + ("ansible.builtin.import_tasks", "ansible.builtin.include_tasks")

# The playbooks each import one role entry point. Finding fewer means the walk
# stopped seeing them, which would pass the handoff rule vacuously.
ENTRY_POINTS = 33

# What a completion hands the runner: the evidence it publishes, or the one
# refusal Go remedies by name.
COMPLETIONS = ("completed", "refused")

# The completions that run outside no_log, by task file and name, because the
# attempt's retained output must carry their refusal (specs/substrates.md,
# Physical machine realization). Each reads no bound material, and
# test_baremetal_replay.py runs each through ansible-playbook and proves its
# result prints the refusal and no reported value. It is exact: an entry that
# names no completion outside no_log fails.
PRINTED_COMPLETIONS = frozenset({
    ("substrate_baremetal_machine/tasks/apply.yml", "Publish bounded machine proof"),
    ("substrate_baremetal_machine/tasks/observe.yml", "Publish bounded machine observation"),
})

# A task reads a file's bytes through this lookup.
FILE_LOOKUP = re.compile(r"""lookup\(\s*['"](?:ansible\.builtin\.)?file['"]""")

# The mapping the runner writes each bound material file's path into.
MATERIAL = re.compile(r"\b[a-z][a-z0-9_]*_material\b")

# A copy or template reads the file its src names.
READS_SOURCE = ("ansible.builtin.copy", "ansible.builtin.template")

COMMANDS = ("ansible.builtin.command", "ansible.legacy.command", "command")
SHELLS = (
    "ansible.builtin.expect", "ansible.builtin.raw", "ansible.builtin.script", "ansible.builtin.shell",
    "ansible.legacy.raw", "ansible.legacy.script", "ansible.legacy.shell", "expect", "raw", "script", "shell",
)

# Every executable a role starts, by its absolute path or by the variable its
# role binds to the path the controller stage published the frozen tool at.
# The list is exact: an entry no task starts any longer fails, so it only
# shrinks, and a new executable is a decision recorded here.
EXECUTABLES = frozenset({
    "/usr/bin/chcon",
    "/usr/bin/dnf",
    "/usr/bin/getent",
    "/usr/bin/mkksiso",
    "/usr/bin/mv",
    "/usr/bin/openssl",
    "/usr/bin/podman",
    "/usr/bin/qemu-img",
    "/usr/bin/rmdir",
    "/usr/bin/ssh",
    "/usr/bin/systemctl",
    "/usr/bin/timeout",
    "/usr/bin/virsh",
    "/usr/bin/xorriso",
    "{{ containercluster_install_agent_client }}",
    "{{ containercluster_media_agent_installer }}",
})

# What GNU timeout may start, held exact the same way.
TIMEOUT = "/usr/bin/timeout"
WRAPPED = frozenset({
    "{{ containercluster_install_agent_installer }}",
    "{{ containercluster_media_agent_installer }}",
})

# The timeout options that take their value as a separate argument.
TIMEOUT_VALUES = ("-k", "-s", "--kill-after", "--signal")

# A debugger or verbose trace that serves or prints what it should not.
DEBUG = re.compile(r"(?<![\w-])--debug\b")

# The Quadlet units the roles install. Finding fewer means the walk stopped
# seeing them, which would pass the container rule vacuously.
UNITS = {
    "infra_artifact_server_nginx/templates/unit.container.j2",
    "infra_managed_service/templates/chrony.container.j2",
    "infra_managed_service/templates/dnsmasq.container.j2",
    "infra_managed_service/templates/squid.container.j2",
    "substrate_libvirt_machine/templates/unit.container.j2",
}

# The polls, by file and register. Finding fewer means the walk stopped seeing
# them, which would pass the poll rule vacuously.
POLLS = {
    ("managedos_install_anaconda/tasks/await.yml", "managedos_install_anaconda_installer"),
    ("managedos_install_anaconda/tasks/reachable.yml", "managedos_install_anaconda_reachable"),
    ("substrate_baremetal_machine/tasks/identity_read.yml", "substrate_baremetal_machine_marker_read"),
    ("substrate_libvirt_host/tasks/apply.yml", "substrate_libvirt_host_connection"),
    ("substrate_libvirt_machine/tasks/apply.yml", "substrate_libvirt_machine_controller"),
    ("substrate_libvirt_machine/tasks/identity_read.yml", "substrate_libvirt_machine_marker_read"),
}

# A management controller is reached only through these modules.
CONTROLLERS = re.compile(r"^bootwright\.core\.redfish_\w+$")

# The files that reach a management controller. Finding fewer means the walk
# stopped seeing them, which would pass the refusal rule vacuously.
CONTROLLER_FILES = {
    "containercluster_install_agent/tasks/boot.yml",
    "containercluster_install_agent/tasks/destroy.yml",
    "containercluster_install_agent/tasks/release.yml",
    "containercluster_install_agent/tasks/state.yml",
    "machine_power_read_redfish/tasks/read.yml",
    "machine_power_redfish/tasks/power.yml",
    "machine_power_redfish/tasks/start.yml",
    "machine_power_redfish/tasks/stop.yml",
    "managedos_install_anaconda/tasks/apply.yml",
    "managedos_install_anaconda/tasks/await.yml",
    "managedos_install_anaconda/tasks/boot.yml",
    "managedos_install_anaconda/tasks/observe.yml",
    "substrate_baremetal_machine/tasks/apply.yml",
    "substrate_baremetal_machine/tasks/boot_disk.yml",
    "substrate_baremetal_machine/tasks/boot_media.yml",
    "substrate_baremetal_machine/tasks/observe.yml",
    "substrate_baremetal_machine/tasks/pre_boot.yml",
    "substrate_libvirt_machine/tasks/apply.yml",
    "substrate_libvirt_machine/tasks/boot_disk.yml",
    "substrate_libvirt_machine/tasks/boot_media.yml",
    "substrate_libvirt_machine/tasks/observe.yml",
    "substrate_libvirt_machine/tasks/pre_boot.yml",
}


def task_files():
    return sorted(ROLES.glob("*/tasks/*.yml"))


def label(path):
    return path.relative_to(ROLES).as_posix()


def load(path):
    parsed = yaml.safe_load(path.read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def walk(tasks):
    """Every task of a task list in the order a play reaches it, blocks opened."""
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def action(task):
    """The one action a task runs, or None for a block."""
    found = [key for key in task if key not in KEYWORDS]
    return found[0] if len(found) == 1 else None


def arguments(task):
    value = task.get(action(task)) if action(task) else None
    return value if isinstance(value, dict) else {}


def strings(value):
    """Every string one task value holds, its mapping keys left out."""
    if isinstance(value, dict):
        for child in value.values():
            yield from strings(child)
    elif isinstance(value, list):
        for child in value:
            yield from strings(child)
    elif value is not None:
        yield str(value)


def text(value):
    return "\n".join(strings(value))


def hidden(task):
    return task.get("no_log") is True


def phase(task):
    """The phase a task hands the runner through its capability's protocol, or None."""
    name = action(task) or ""
    if name.startswith("bootwright.core.") and name.endswith("_protocol"):
        return str(arguments(task).get("phase", ""))
    return None


def entry_points():
    """The task file each playbook imports, by playbook."""
    found = {}
    for playbook in sorted(PLAYBOOKS.glob("*/*.yml")):
        for play in yaml.safe_load(playbook.read_text()) or []:
            for task in play.get("tasks") or []:
                for name in ROLE_IMPORTS:
                    imported = task.get(name)
                    if isinstance(imported, dict):
                        role = str(imported["name"]).rsplit(".", 1)[-1]
                        entry = str(imported.get("tasks_from", "main"))
                        entry = entry if entry.endswith(".yml") else entry + ".yml"
                        found[playbook.relative_to(PLAYBOOKS).as_posix()] = ROLES / role / "tasks" / entry
    return found


def included_files(tasks):
    """The task files one task list includes by path."""
    found = set()
    for task in walk(tasks):
        for name in INCLUDES[2:]:
            target = task.get(name)
            target = target.get("file") if isinstance(target, dict) else target
            if isinstance(target, str):
                found.add(target)
    return found


# Every rule below finds a task's effect through its one action, so a task
# whose keys name no single action would pass all of them unread.
def test_every_task_names_one_action_or_is_a_block():
    problems = []
    for path in task_files():
        for task in walk(load(path)):
            if "block" not in task and action(task) is None:
                problems.append("%s: %r carries %s, which name no single action"
                                % (label(path), task.get("name"), sorted(set(task) - KEYWORDS)))
    assert not problems, "\n".join(problems)


def handoff_problems(where, tasks):
    """Why an entry point might act before, or without, its loaded record.

    Only an assertion, which changes nothing, may run before the handoff, and the
    handoff itself runs unconditionally, once, among the file's own tasks.
    """
    problems = []
    for task in tasks:
        if phase(task) == "loaded":
            for keyword in ("when", "loop", "failed_when", "ignore_errors"):
                if keyword in task:
                    problems.append("%s: its loaded handoff carries %s, so it may not be handed" % (where, keyword))
            return problems
        if action(task) != "ansible.builtin.assert" or "loop" in task or "register" in task:
            problems.append("%s: %r runs before the entry point hands the runner its loaded record"
                            % (where, task.get("name")))
    return problems + ["%s hands the runner no loaded record among its own tasks" % where]


def test_every_entry_point_hands_the_runner_loaded_before_anything_else():
    entries = entry_points()
    assert len(entries) >= ENTRY_POINTS, "the walk no longer sees every playbook's entry point"
    files = {label(path) for path in entries.values()}
    problems = []
    for path in sorted(set(entries.values())):
        problems.extend(handoff_problems(label(path), load(path)))
    for path in task_files():
        tasks = load(path)
        handed = sum(phase(task) == "loaded" for task in walk(tasks))
        if label(path) in files and handed != 1:
            problems.append("%s is an entry point that hands the runner loaded %d times" % (label(path), handed))
        if label(path) not in files and handed:
            problems.append("%s hands the runner loaded, which only an entry point does, once" % label(path))
        for target in sorted(included_files(tasks)):
            if "%s/tasks/%s" % (path.parent.parent.name, target) in files:
                problems.append("%s includes the entry point %s, whose loaded record runs again" % (label(path), target))
    assert not problems, "\n".join(problems)


def derived_variables():
    """The role variables whose defaults read a material file's bytes."""
    found = set()
    for path in ROLES.glob("*/defaults/main.yml"):
        for name, value in (yaml.safe_load(path.read_text()) or {}).items():
            if FILE_LOOKUP.search(str(value)):
                found.add(name)
    return found


def receives_material(task, derived):
    """Whether one task is given the bytes of bound material: through a file
    lookup, a variable that holds one's result, or a copy of a material file.
    A path alone, or a condition over one, is not the material."""
    body = {key: value for key, value in task.items()
            if key not in ("always", "block", "loop_control", "name", "register", "rescue", "when")}
    found = text(body)
    if FILE_LOOKUP.search(found) or any(re.search(r"\b%s\b" % re.escape(name), found) for name in derived):
        return True
    return action(task) in READS_SOURCE and bool(MATERIAL.search(str(arguments(task).get("src", ""))))


def material_problems(where, tasks, derived):
    """Why bound material or a completion's evidence might reach the adapter's output."""
    problems = []
    derived = set(derived)
    for task in walk(tasks):
        name = task.get("name")
        received = receives_material(task, derived)
        if received and action(task) == "ansible.builtin.set_fact":
            derived.update(arguments(task))
        if action(task) in INCLUDES:
            if received:
                problems.append("%s: %r hands an included file material bytes, which no no_log of its own "
                                "protects; pass the path of the material file" % (where, name))
        elif received and not hidden(task):
            problems.append("%s: %r receives bound material without no_log: true" % (where, name))
        if phase(task) in COMPLETIONS and not hidden(task) and (where, name) not in PRINTED_COMPLETIONS:
            problems.append("%s: %r hands the runner a completion without no_log: true" % (where, name))
    return problems


def test_no_material_or_completion_reaches_the_adapters_own_output():
    derived = derived_variables()
    assert derived, "the walk no longer sees a default that reads material"
    problems, received, completions = [], 0, 0
    for path in task_files():
        tasks = load(path)
        problems.extend(material_problems(label(path), tasks, derived))
        received += sum(receives_material(task, derived) for task in walk(tasks))
        completions += sum(phase(task) in COMPLETIONS for task in walk(tasks))
    entries = entry_points()
    assert len(entries) >= ENTRY_POINTS, "the walk no longer sees every playbook's entry point"
    assert received and completions >= len(set(entries.values())), "the walk no longer sees material or completions"
    assert not problems, "\n".join(problems)


def test_every_printed_completion_names_a_completion_outside_no_log():
    printed = {(label(path), task.get("name")) for path in task_files() for task in walk(load(path))
               if phase(task) in COMPLETIONS and not hidden(task)}
    assert printed == PRINTED_COMPLETIONS


def command_argv(task):
    """The argument vector of a command task, or None when it has none."""
    argv = arguments(task).get("argv")
    return [str(value) for value in argv] if isinstance(argv, list) and argv else None


def wrapped(argv):
    """The command GNU timeout starts: the argument after its options and duration."""
    rest = list(argv[1:])
    while rest and rest[0].startswith("-"):
        option = rest.pop(0)
        if option in TIMEOUT_VALUES and rest:
            rest.pop(0)
    return rest[1] if len(rest) > 1 else None


def executable_problems(where, tasks, started, wrappers):
    """Why a task might start an executable the allowlist does not name."""
    problems = []
    for task in walk(tasks):
        name = action(task)
        if name in SHELLS:
            problems.append("%s: %r runs %s, which starts what a shell decides" % (where, task.get("name"), name))
        if name not in COMMANDS:
            continue
        argv = command_argv(task)
        if argv is None:
            problems.append("%s: %r gives its command no argv, so a string is split into one"
                            % (where, task.get("name")))
            continue
        started.add(argv[0])
        if argv[0] not in EXECUTABLES:
            problems.append("%s: %r starts %s, which the allowlist does not name" % (where, task.get("name"), argv[0]))
        if argv[0] == TIMEOUT:
            inner = wrapped(argv)
            wrappers.add(inner)
            if inner not in WRAPPED:
                problems.append("%s: %r has timeout start %s, which the allowlist does not name"
                                % (where, task.get("name"), inner))
    return problems


def test_every_command_starts_an_executable_the_allowlist_names():
    problems, started, wrappers = [], set(), set()
    for path in task_files():
        problems.extend(executable_problems(label(path), load(path), started, wrappers))
    assert not problems, "\n".join(problems)
    assert started == EXECUTABLES, "no task starts %s any longer: remove it" % sorted(EXECUTABLES - started)
    assert wrappers == WRAPPED, "timeout no longer starts %s: remove it" % sorted(WRAPPED - wrappers)


def unit_problems(where, unit):
    """Why a Quadlet unit might run its image's own entrypoint."""
    section, found = None, {"Entrypoint": [], "Exec": []}
    for line in unit.splitlines():
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            section = stripped
            continue
        key, separator, value = stripped.partition("=")
        if section == "[Container]" and separator and key in found:
            found[key].append(value)
    problems = []
    if len(found["Entrypoint"]) != 1 or not found["Entrypoint"][0].startswith("/"):
        problems.append("%s sets Entrypoint %r rather than one absolute path, so the image's own "
                        "entrypoint can run" % (where, found["Entrypoint"]))
    if len(found["Exec"]) != 1:
        problems.append("%s sets Exec %d times rather than once" % (where, len(found["Exec"])))
    return problems


def podman_run_problems(where, tasks):
    problems = []
    for task in walk(tasks):
        argv = command_argv(task) if action(task) in COMMANDS else None
        if argv and argv[:2] == ["/usr/bin/podman", "run"] and not any(
                value == "--entrypoint" or value.startswith("--entrypoint=") for value in argv):
            problems.append("%s: %r runs a container without --entrypoint, so its image's own entrypoint runs"
                            % (where, task.get("name")))
    return problems


def test_every_container_names_its_entrypoint_and_nothing_runs_a_debugger():
    units = sorted(ROLES.glob("*/templates/*.container.j2"))
    assert UNITS <= {label(path) for path in units}, "the walk no longer sees every Quadlet unit"
    problems, runs = [], 0
    for path in units:
        problems.extend(unit_problems(label(path), path.read_text()))
    for path in task_files():
        tasks = load(path)
        problems.extend(podman_run_problems(label(path), tasks))
        runs += sum(bool((command_argv(task) or [])[:2] == ["/usr/bin/podman", "run"])
                    for task in walk(tasks) if action(task) in COMMANDS)
    for path in sorted(ROLES.rglob("*")):
        if path.is_file() and DEBUG.search(path.read_text()):
            problems.append("%s passes --debug" % label(path))
    assert runs, "the walk no longer sees a container a task runs"
    assert not problems, "\n".join(problems)


def unguarded_reads(register, expression):
    """Each read of a registered result an until expression makes without a
    default, which a failed attempt's result may not carry."""
    found = []
    for match in re.finditer(r"\b%s\b(\s*\.\s*[A-Za-z_]\w*|\s*\[)?" % re.escape(register), expression):
        rest = expression[match.end():]
        if match.group(1) is None:
            continue
        if match.group(1).strip() == "[" or not re.match(r"\s*\|\s*default\(", rest):
            found.append(match.group(0).strip())
    return found


def poll_problems(where, tasks):
    """Why one failed read might end a poll instead of spending an attempt on it."""
    problems = []
    for task in walk(tasks):
        if "until" not in task:
            continue
        register = task.get("register")
        if not isinstance(register, str):
            problems.append("%s: %r polls without registering what it reads" % (where, task.get("name")))
            continue
        for read in unguarded_reads(register, str(task["until"])):
            problems.append("%s: %r ends its poll on %s, which a failed read does not carry, so the "
                            "expression fails instead of spending an attempt" % (where, task.get("name"), read))
    return problems


def test_every_poll_spends_an_attempt_on_a_failed_read():
    problems, seen = [], set()
    for path in task_files():
        tasks = load(path)
        problems.extend(poll_problems(label(path), tasks))
        seen.update((label(path), task.get("register")) for task in walk(tasks) if "until" in task)
    assert POLLS <= seen, "the walk no longer sees %s" % sorted(POLLS - seen)
    assert not problems, "\n".join(problems)


def reaches_controller(task):
    return bool(CONTROLLERS.match(action(task) or ""))


def names_refusal(task, register):
    """Whether a task outside no_log refuses on a registered result by the
    message the controller refused with."""
    reads = re.compile(r"\b%s\b" % re.escape(register))
    message = re.compile(r"\.msg\b|attribute=['\"]msg['\"]")
    return (not hidden(task) and action(task) in ("ansible.builtin.assert", "ansible.builtin.fail")
            and any(reads.search(value) and message.search(value) for value in strings(task)))


def publishes(task, register, looped):
    """Whether a protocol publication reads a registered result as an observation
    does: unconditionally and only through defaults, so a read that failed is
    published unproved and refuses nothing. A looped task's result always
    carries its results."""
    if phase(task) not in COMPLETIONS or "when" in task or not re.search(r"\b%s\b" % re.escape(register), text(task)):
        return False
    reads = unguarded_reads(register, text(task))
    return not [read for read in reads if not (looped and re.sub(r"\s", "", read) == register + ".results")]


def reporter(ordered, position):
    """The task that reports what the controller task at this position of a walk
    registered: the refusal or publication that runs next, with nothing but the
    protocol's group records, which neither act nor stop, between them. None
    when any other task, a block included, runs first."""
    task = ordered[position]
    for other in ordered[position + 1:]:
        if names_refusal(other, task["register"]) or publishes(other, task["register"], "loop" in task):
            return other
        if phase(other) != "group":
            return None
    return None


USER = "operator-account-7d1f"
SAID = "the management controller did not complete insert: HTTP 403 at /redfish/v1/Managers/1/VirtualMedia/Cd"
ANSWERED = {"changed": False, "media": "", "power": "Off", "observation": {"power": "Off"}}
REFUSED = {"changed": False, "failed": False, "failed_when_result": False,
           "invocation": {"module_args": {"user": USER, "password": "VALUE_SPECIFIED_IN_NO_LOG_PARAMETER"}},
           "msg": "\x1b[31m" + SAID + chr(0x202E) + "x" * 600}


def registered(task, result):
    """What one controller task registers when every read or effect it makes
    ends as this result, one per item when it loops."""
    if "loop" not in task:
        return dict(result)
    loop_var = (task.get("loop_control") or {}).get("loop_var", "item")
    return {"changed": False, "results": [dict(ANSWERED, **{loop_var: {"machine": "sno-00"}}),
                                          dict(result, **{loop_var: {"machine": "sno-01"}})]}


def conditions(value):
    """The expressions one when or that holds, each trusted as a play trusts it."""
    return [item if isinstance(item, bool) else trust_as_template(str(item))
            for item in (value if isinstance(value, list) else [value])]


def refuses(templar, task):
    """Whether one refusal task stops the play in this scope, as a play decides
    it: the task runs once, its own when holds or it has none, nothing tolerates
    its failure, and it is a fail or one of its assertions does not hold."""
    if "loop" in task or "failed_when" in task or task.get("ignore_errors", False) is not False:
        return False
    if "when" in task and not all(templar.evaluate_conditional(item) for item in conditions(task["when"])):
        return False
    if action(task) == "ansible.builtin.fail":
        return True
    return not all(templar.evaluate_conditional(item) for item in conditions(arguments(task).get("that")))


def firing_problems(where, task, refusal):
    """Why a refusal might let the play go on when the controller refused, or
    stop it when the controller answered. It decides on the controller task's
    result alone, so nothing else the play holds can silence it."""
    name, register = refusal.get("name"), task["register"]

    def scope(result):
        return Templar(loader=DataLoader(), variables={register: registered(task, result)})

    try:
        if not refuses(scope(REFUSED), refusal):
            return ["%s: %r lets the play go on when %s holds the controller's refusal" % (where, name, register)]
        if refuses(scope(ANSWERED), refusal):
            return ["%s: %r stops the play when %s holds the controller's answer" % (where, name, register)]
    except AnsibleError as error:
        return ["%s: %r decides on more than %s holds: %s" % (where, name, register, str(error).splitlines()[0])]
    return []


def refusal_problems(where, tasks):
    """Why a management controller's refusal might reach the output censored,
    or only after the play acted on it.

    A task that reaches a controller under no_log never fails itself, because
    the adapter's output then shows nothing of its result but that it failed
    (plugins/callback/censored.py). It registers the result and tolerates its
    failure, and the next task either refuses outside no_log with the
    controller's own message, stopping the play whenever the controller
    refused, or publishes the result as an observation.
    Only the protocol's group records may come between them.
    """
    problems = []
    ordered = list(walk(tasks))
    for position, task in enumerate(ordered):
        if not reaches_controller(task) or not hidden(task):
            continue
        name, register = task.get("name"), task.get("register")
        if not isinstance(register, str) or task.get("failed_when") is not False:
            problems.append("%s: %r fails under no_log itself, so the controller's refusal is censored; register "
                            "it with failed_when: false and refuse in a separate task" % (where, name))
            continue
        other = reporter(ordered, position)
        if other is None:
            problems.append("%s: %r tolerates a refusal the task after it does not report: refuse outside no_log "
                            "with %s.msg, or publish it as an observation, before any other task runs"
                            % (where, name, register))
        elif names_refusal(other, register):
            problems.extend(firing_problems(where, task, other))
    return problems


def test_no_management_controller_refusal_is_censored():
    problems, seen = [], set()
    for path in task_files():
        tasks = load(path)
        problems.extend(refusal_problems(label(path), tasks))
        if any(reaches_controller(task) for task in walk(tasks)):
            seen.add(label(path))
    assert CONTROLLER_FILES <= seen, "the walk no longer sees %s" % sorted(CONTROLLER_FILES - seen)
    for path in task_files():
        for task in walk(load(path)):
            if reaches_controller(task) and not hidden(task):
                problems.append("%s: %r reaches a controller with its credential outside no_log"
                                % (label(path), task.get("name")))
    assert not problems, "\n".join(problems)


# What each refusal names beside the controller's own message: the Machine or
# the endpoint, and a wait's budget.
ENDPOINT = "https://bmc.example.test/redfish/v1/Systems/1"
SCOPE = dict({
    "bootwright_machine_power_request": {"identity": {"object": "metal"}},
    "bootwright_os_install_request": {"identity": {"object": "rhel-01"},
                                      "budgets": {"installer": {"attempts": 180, "delaySeconds": 20}}},
    "bootwright_substrate_machine_request": {"identity": {"object": "rhel-01"}},
    "bootwright_substrate_physical_request": {"identity": {"object": "metal"}, "controller": {"endpoint": ENDPOINT}},
    "containercluster_install_agent_node": {"machine": "sno-01"},
}, **{"%s_%s_endpoint" % (role, entry): ENDPOINT
      for role in ("substrate_baremetal_machine", "substrate_libvirt_machine") for entry in ("boot", "disk", "target")})


def controller_refusals():
    """Each controller task under no_log with the refusal that reports it, by file."""
    found = []
    for path in task_files():
        ordered = list(walk(load(path)))
        for position, task in enumerate(ordered):
            if reaches_controller(task) and hidden(task) and isinstance(task.get("register"), str):
                refusal = reporter(ordered, position)
                if refusal is not None and names_refusal(refusal, task["register"]):
                    found.append((label(path), task, refusal))
    return found


# A refusal names the controller's own message, printable and bounded, and
# never the account the step read, and it stays silent when the controller
# answered.
def test_every_controller_refusal_names_the_controllers_message_and_nothing_it_was_given():
    loader = DataLoader()
    found = controller_refusals()
    assert len(found) >= 26, "the walk no longer sees the controller refusals"
    for name, task, refusal in found:
        answered = Templar(loader=loader, variables=dict(SCOPE, **{task["register"]: registered(task, ANSWERED)}))
        assert not refuses(answered, refusal), "%s: %r refuses a controller that answered" % (name, refusal["name"])
        failed = Templar(loader=loader, variables=dict(SCOPE, **{task["register"]: registered(task, REFUSED)}))
        assert refuses(failed, refusal), "%s: %r does not refuse what the controller refused" % (name, refusal["name"])
        message = failed.template(trust_as_template(arguments(refusal).get("fail_msg") or arguments(refusal)["msg"]))
        assert SAID in message and USER not in message and message.isprintable(), "%s: %r" % (name, message)
        assert len(message) < 1024, "%s: %r prints the controller's message unbounded" % (name, refusal["name"])
        if "loop" in task:
            assert "sno-01" in message and "sno-00" not in message, "%s: %r" % (name, message)


# Synthetic task files, shaped as the roles are, show that each rule rejects
# what it guards against rather than passing every file it reads.
VALIDATE = {"name": "Validate the frozen request", "ansible.builtin.assert": {"that": ["request.version == 'v1'"]},
            "no_log": True}
LOADED = {"name": "Complete the qualified execution handoff", "bootwright.core.example_protocol": {"phase": "loaded"},
          "no_log": True}
EFFECT = {"name": "Create the directory", "ansible.builtin.file": {"path": "/var/lib/example", "state": "directory"}}
COMPLETED = {"name": "Publish the evidence", "bootwright.core.example_protocol": {"phase": "completed"}, "no_log": True}


def test_an_entry_point_that_validates_then_hands_loaded_passes():
    assert not handoff_problems("sound", [VALIDATE, LOADED, EFFECT, COMPLETED])


@pytest.mark.parametrize("tasks, reason", [
    ([EFFECT, LOADED, COMPLETED], "runs before the entry point hands"),
    ([VALIDATE, dict(VALIDATE, register="validated"), LOADED], "runs before the entry point hands"),
    ([VALIDATE, dict(LOADED, when="request.fresh"), EFFECT], "carries when"),
    ([VALIDATE, dict(LOADED, ignore_errors=True), EFFECT], "carries ignore_errors"),
    ([VALIDATE, {"name": "Hand off", "block": [LOADED]}, EFFECT], "no loaded record among its own tasks"),
    ([VALIDATE, EFFECT, COMPLETED], "no loaded record among its own tasks"),
], ids=["an effect first", "a registering assertion first", "a conditional handoff", "a handoff that may fail",
        "a handoff inside a block", "no handoff"])
def test_the_handoff_rule_rejects(tasks, reason):
    problems = handoff_problems("synthetic", tasks)
    assert any(reason in problem for problem in problems), problems


READ = {"name": "Read the controller", "bootwright.core.redfish_system_read": {
    "endpoint": "https://192.0.2.1/redfish/v1/Systems/1",
    "user": "{{ lookup('ansible.builtin.file', example_material.controllerUser) }}",
    "password": "{{ lookup(\"ansible.builtin.file\", example_material.controllerPassword) }}"},
    "register": "example_read", "failed_when": False, "no_log": True}
REFUSE = {"name": "Refuse what the controller refused", "ansible.builtin.assert": {
    "that": ["example_read.msg is not defined"], "fail_msg": "{{ example_read.msg | default('') }}"}}


@pytest.mark.parametrize("tasks", [
    [READ, REFUSE, COMPLETED],
    [{"name": "Copy the key", "ansible.builtin.copy": {"src": "{{ example_material.key }}", "dest": "/k"},
      "no_log": True}],
    [{"name": "Hold the certificate", "ansible.builtin.set_fact": {"certificate": "{{ lookup('file', path) }}"},
      "no_log": True},
     {"name": "Write it", "ansible.builtin.copy": {"content": "{{ certificate }}", "dest": "/c"}, "no_log": True}],
    [{"name": "Include by path", "ansible.builtin.include_role": {"name": "example", "tasks_from": "boot"},
      "vars": {"example_user": "{{ example_material.controllerUser }}"}}],
    [{"name": "Decide by a path", "ansible.builtin.debug": {"msg": "present"},
      "when": "example_material.key | length > 0"}],
], ids=["a hidden read", "a hidden copy", "a hidden fact and its use", "an include passing a path",
        "a condition over a path"])
def test_hidden_material_passes_the_material_rule(tasks):
    assert not material_problems("sound", tasks, {"example_ca_data"})


@pytest.mark.parametrize("tasks, reason", [
    ([dict(READ, no_log=False)], "receives bound material without no_log"),
    ([{"name": "Copy the key", "ansible.builtin.copy": {"src": "{{ example_material.key }}", "dest": "/k"}}],
     "receives bound material without no_log"),
    ([{"name": "Show the anchor", "ansible.builtin.debug": {"msg": "{{ example_ca_data }}"}}],
     "receives bound material without no_log"),
    ([{"name": "Hold the certificate", "ansible.builtin.set_fact": {"certificate": "{{ lookup('file', path) }}"},
       "no_log": True},
      {"name": "Write it", "ansible.builtin.copy": {"content": "{{ certificate }}", "dest": "/c"}}],
     "receives bound material without no_log"),
    ([{"name": "Include bytes", "ansible.builtin.include_role": {"name": "example", "tasks_from": "boot"},
       "vars": {"example_user": "{{ lookup('ansible.builtin.file', example_material.controllerUser) }}"},
       "no_log": True}], "hands an included file material bytes"),
    ([dict(COMPLETED, no_log=False)], "hands the runner a completion without no_log"),
    ([{"name": "Refuse the tool", "bootwright.core.example_protocol": {"phase": "refused"}}],
     "hands the runner a completion without no_log"),
], ids=["a visible read", "a visible copy of a material file", "a visible derived default", "a visible derived fact",
        "an include passing bytes", "a visible completion", "a visible refusal record"])
def test_the_material_rule_rejects(tasks, reason):
    problems = material_problems("synthetic", tasks, {"example_ca_data"})
    assert any(reason in problem for problem in problems), problems


def command(*argv, **keywords):
    task = {"name": "Run it", "ansible.builtin.command": {"argv": list(argv)}}
    task.update(keywords)
    return task


def test_allowlisted_executables_pass_the_executable_rule():
    tasks = [command("/usr/bin/virsh", "version"),
             command(TIMEOUT, "--kill-after=5s", "60s", "{{ containercluster_media_agent_installer }}", "agent"),
             command(TIMEOUT, "-k", "5s", "60s", "{{ containercluster_install_agent_installer }}", "agent")]
    assert not executable_problems("sound", tasks, set(), set())


@pytest.mark.parametrize("task, reason", [
    (command("getent", "ahosts", "example.test"), "starts getent, which the allowlist does not name"),
    (command("/usr/local/bin/virsh", "version"), "which the allowlist does not name"),
    (command(TIMEOUT, "60s", "/usr/bin/bash", "-c", "true"), "has timeout start /usr/bin/bash"),
    (command(TIMEOUT, "-k", "5s", "60s", "rm", "-rf", "/"), "has timeout start rm"),
    ({"name": "Run it", "ansible.builtin.command": "/usr/bin/virsh version"}, "gives its command no argv"),
    ({"name": "Run it", "ansible.builtin.command": {"cmd": "/usr/bin/virsh version"}}, "gives its command no argv"),
    ({"name": "Run it", "ansible.builtin.shell": "virsh version"}, "which starts what a shell decides"),
    ({"name": "Run it", "ansible.builtin.raw": "virsh version"}, "which starts what a shell decides"),
], ids=["a name the search path resolves", "another path", "a shell under timeout", "a relative name under timeout",
        "a free-form command", "a command string", "a shell", "a raw command"])
def test_the_executable_rule_rejects(task, reason):
    problems = executable_problems("synthetic", [task], set(), set())
    assert any(reason in problem for problem in problems), problems


UNIT = "[Unit]\nDescription=example\n\n[Container]\nImage=example\nEntrypoint=/usr/sbin/example\nExec=-f /e.conf\n"


def test_a_unit_that_names_its_entrypoint_passes_the_container_rule():
    assert not unit_problems("sound", UNIT)
    assert not podman_run_problems("sound", [command("/usr/bin/podman", "run", "--entrypoint", "python3", "image")])


@pytest.mark.parametrize("unit, reason", [
    (UNIT.replace("Entrypoint=/usr/sbin/example\n", ""), "sets Entrypoint []"),
    (UNIT.replace("Entrypoint=/usr/sbin/example", "Entrypoint=example"), "rather than one absolute path"),
    (UNIT.replace("Exec=-f /e.conf\n", ""), "sets Exec 0 times"),
    (UNIT.replace("[Container]\nImage=example\nEntrypoint=/usr/sbin/example\n",
                  "[Service]\nEntrypoint=/usr/sbin/example\n[Container]\nImage=example\n"), "sets Entrypoint []"),
], ids=["no entrypoint", "a relative entrypoint", "no exec", "an entrypoint outside the container section"])
def test_the_container_rule_rejects(unit, reason):
    problems = unit_problems("synthetic", unit)
    assert any(reason in problem for problem in problems), problems


def test_the_container_rule_rejects_a_run_of_the_images_own_entrypoint_and_a_debugger():
    problems = podman_run_problems("synthetic", [command("/usr/bin/podman", "run", "--rm", "image", "--debug")])
    assert any("without --entrypoint" in problem for problem in problems), problems
    assert DEBUG.search("Exec=--config /etc/sushy/conf.py --debug")
    assert DEBUG.search("      - --debug")
    assert not DEBUG.search("Exec=--config /etc/sushy/conf.py")


POLL = {"name": "Wait for the machine", "bootwright.core.redfish_system_read": {"media": False},
        "register": "example_read", "retries": 3, "delay": 1}


@pytest.mark.parametrize("until", [
    "(example_read.power | default('')) == 'Off'",
    "example_read.rc | default(1) == 0 or (example_read.attempts | default(1) | int) > 3",
    "example_read is succeeded",
], ids=["a guarded state", "a guarded status and its last attempt", "a test of the whole result"])
def test_a_guarded_poll_passes_the_poll_rule(until):
    assert not poll_problems("sound", [dict(POLL, until=until)])


@pytest.mark.parametrize("task, reason", [
    (dict(POLL, until="example_read.power == 'Off'"), "ends its poll on example_read.power"),
    (dict(POLL, until="example_read.rc == 0"), "ends its poll on example_read.rc"),
    (dict(POLL, until="example_read['power'] == 'Off'"), "ends its poll on example_read["),
    (dict(POLL, until="(example_read.power | default('')) == 'Off' and example_read.media == ''"),
     "ends its poll on example_read.media"),
    ({"name": "Wait", "ansible.builtin.command": {"argv": ["/usr/bin/virsh"]}, "until": "true"},
     "polls without registering"),
], ids=["an unguarded state", "an unguarded status", "an unguarded index", "one read of two unguarded",
        "a poll that registers nothing"])
def test_the_poll_rule_rejects(task, reason):
    problems = poll_problems("synthetic", [task])
    assert any(reason in problem for problem in problems), problems


OBSERVATION = {"name": "Publish what was read", "bootwright.core.example_protocol": {
    "phase": "completed", "power": "{{ example_read.power | default('') }}"}, "no_log": True}
PROGRESS = {"name": "Report the read", "bootwright.core.example_protocol": {
    "phase": "group", "group": "read", "status": "running"}, "no_log": True}
LOOPED = dict(READ, loop="{{ nodes }}")
REFUSE_EACH = {"name": "Refuse each", "ansible.builtin.fail": {
    "msg": "{{ example_read.results | selectattr('msg', 'defined') | map(attribute='msg') | join('; ') }}"},
    "when": "example_read.results | selectattr('msg', 'defined') | list | length > 0"}
TOLERATED = "tolerates a refusal the task after it does not report"
STARTED = dict(READ, name="Power the machine on", register="example_started")
REFUSE_STARTED = {"name": "Refuse what the power-on refused", "ansible.builtin.assert": {
    "that": ["example_started.msg is not defined"], "fail_msg": "{{ example_started.msg | default('') }}"}}


@pytest.mark.parametrize("tasks", [
    [READ, REFUSE],
    [READ, OBSERVATION],
    [dict(READ, until="(example_read.power | default('')) == 'Off'", retries=3), REFUSE],
    [LOOPED, REFUSE_EACH],
    [LOOPED, dict(OBSERVATION, **{"bootwright.core.example_protocol": {
        "phase": "completed", "readings": "{{ example_read.results }}"}})],
    [READ, PROGRESS, REFUSE],
    [READ, dict(REFUSE, when=["example_read is not skipped"])],
], ids=["a read and its refusal", "an observation", "a poll and its refusal", "a loop and its refusal",
        "a looped observation", "a progress record before the refusal", "a refusal whose own condition holds"])
def test_a_controller_refusal_outside_no_log_passes_the_refusal_rule(tasks):
    assert not refusal_problems("sound", tasks)


@pytest.mark.parametrize("tasks, reason", [
    ([{key: value for key, value in READ.items() if key != "failed_when"}, REFUSE], "fails under no_log itself"),
    ([{key: value for key, value in READ.items() if key != "register"}, REFUSE], "fails under no_log itself"),
    ([dict(READ, failed_when="example_read.power is not defined"), REFUSE], "fails under no_log itself"),
    ([READ, dict(REFUSE, no_log=True)], TOLERATED),
    ([READ, dict(REFUSE, **{"ansible.builtin.assert": {"that": ["example_read.power == 'On'"]}})], TOLERATED),
    ([REFUSE, READ], TOLERATED),
    ([READ], TOLERATED),
    ([READ, dict(OBSERVATION, **{"bootwright.core.example_protocol": {
        "phase": "completed", "observation": "{{ example_read.observation }}"}})], TOLERATED),
    ([READ, dict(OBSERVATION, **{"bootwright.core.example_protocol": {
        "phase": "completed", "readings": "{{ example_read.results }}"}})], TOLERATED),
    ([READ, dict(OBSERVATION, when="example_read is succeeded")], TOLERATED),
    ([READ, EFFECT, REFUSE], TOLERATED),
    ([READ, STARTED, REFUSE, REFUSE_STARTED], TOLERATED),
    ([READ, dict(REFUSE, no_log=True), REFUSE], TOLERATED),
    ([READ, {"name": "Refuse a failed read", "when": "example_read is failed", "block": [REFUSE]}], TOLERATED),
    ([READ, dict(REFUSE, when="example_read is failed")],
     "lets the play go on when example_read holds the controller's refusal"),
    ([LOOPED, dict(REFUSE_EACH, when="example_read is failed")],
     "lets the play go on when example_read holds the controller's refusal"),
    ([READ, dict(REFUSE, ignore_errors=True)], "lets the play go on when example_read holds the controller's refusal"),
    ([READ, dict(REFUSE, failed_when=False)], "lets the play go on when example_read holds the controller's refusal"),
    ([READ, dict(REFUSE, loop=[])], "lets the play go on when example_read holds the controller's refusal"),
    ([READ, dict(REFUSE, when="example_checked | bool")], "decides on more than example_read holds"),
    ([LOOPED, dict(REFUSE_EACH, when="true")], "stops the play when example_read holds the controller's answer"),
], ids=["a read that fails itself", "a read that registers nothing", "a read that decides under no_log",
        "a hidden refusal", "a refusal without the controller's message", "a refusal before the read",
        "no refusal", "a publication that fails on a failed read", "a single read published as a loop",
        "a publication gated on success", "a refusal after a later effect", "a refusal after another controller step",
        "a hidden refusal before a visible one", "a refusal under a block's condition",
        "a refusal gated on is failed", "a looped refusal gated on is failed", "a refusal whose failure is ignored",
        "a refusal that never fails", "a refusal that may not run", "a refusal that decides on another fact",
        "a refusal that stops an answered read"])
def test_the_refusal_rule_rejects(tasks, reason):
    problems = refusal_problems("synthetic", tasks)
    assert any(reason in problem for problem in problems), problems
