"""An entry point reads only the bootwright_* variables its specification declares.

A role's meta/argument_specs.yml validates what an entry point declares before
its first task (specs/architecture.md, Playbooks), so a variable the entry
point reads without declaring it reaches the effects unvalidated. These cases
walk each entry point's task file and every task file it includes by a literal
name, and refuse a bootwright_* variable read there that the specification, the
role's defaults or the task files themselves do not define. The reads that
predate the rule are an exact allowance that only shrinks.
"""

from __future__ import annotations

import pathlib
import re

import pytest
import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
VARIABLE = re.compile(r"\bbootwright_[a-z][a-z0-9_]*\b")
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
LITERAL = re.compile(r"(?:\{\{ role_path \}\}/tasks/)?(\w+)\.yml")

# The reads that predate the rule, by role and entry point (B431 declares them):
# the runner passes each digest and the run scratch beside the request.
UNDECLARED_READS = {
    ("containercluster_install_agent", "apply"): {"bootwright_cluster_install_digest"},
    ("containercluster_install_agent", "observe"): {"bootwright_cluster_install_digest", "bootwright_run_scratch"},
    ("containercluster_install_agent", "destroy"): {"bootwright_cluster_install_digest", "bootwright_run_scratch"},
    ("containercluster_media_agent", "apply"): {"bootwright_cluster_media_digest"},
    ("containercluster_media_agent", "observe"): {"bootwright_cluster_media_digest"},
    ("containercluster_media_agent", "destroy"): {"bootwright_cluster_media_digest"},
    ("infra_artifact_server_nginx", "apply"): {"bootwright_artifact_server_digest"},
    ("infra_artifact_server_nginx", "observe"): {"bootwright_artifact_server_digest"},
    ("infra_artifact_server_nginx", "destroy"): {"bootwright_artifact_server_digest"},
    ("infra_managed_service", "apply"): {"bootwright_managed_service_digest"},
    ("infra_managed_service", "observe"): {"bootwright_managed_service_digest"},
    ("infra_managed_service", "destroy"): {"bootwright_managed_service_digest"},
    ("machine_power_read_redfish", "read"): {"bootwright_machine_power_read_digest"},
    ("machine_power_redfish", "power"): {"bootwright_machine_power_digest"},
    ("managedos_install_anaconda", "apply"): {"bootwright_os_install_digest", "bootwright_run_scratch"},
    ("managedos_install_anaconda", "observe"): {"bootwright_os_install_digest", "bootwright_run_scratch"},
    ("managedos_install_anaconda", "destroy"): {"bootwright_os_install_digest", "bootwright_run_scratch"},
    ("managedos_package_tree", "apply"): {"bootwright_os_tree_digest"},
    ("managedos_package_tree", "observe"): {"bootwright_os_tree_digest"},
    ("managedos_package_tree", "destroy"): {"bootwright_os_tree_digest"},
    ("substrate_baremetal_machine", "apply"): {"bootwright_substrate_physical_digest"},
    ("substrate_baremetal_machine", "observe"): {"bootwright_substrate_physical_digest"},
    ("substrate_baremetal_machine", "destroy"): {"bootwright_substrate_physical_digest"},
    ("substrate_libvirt_host", "apply"): {"bootwright_run_scratch", "bootwright_substrate_host_digest"},
    ("substrate_libvirt_host", "observe"): {"bootwright_substrate_host_digest"},
    ("substrate_libvirt_host", "destroy"): {"bootwright_substrate_host_digest"},
    ("substrate_libvirt_machine", "apply"): {"bootwright_run_scratch", "bootwright_substrate_machine_digest"},
    ("substrate_libvirt_machine", "observe"): {"bootwright_substrate_machine_digest"},
    ("substrate_libvirt_machine", "destroy"): {"bootwright_substrate_machine_digest"},
}


def tasks_of(text):
    """Every task of a task file, with the tasks inside its blocks."""
    found = []

    def visit(tasks):
        for task in tasks or []:
            if isinstance(task, dict):
                found.append(task)
                for section in ("block", "rescue", "always"):
                    visit(task.get(section))

    visit(yaml.safe_load(text))
    return found


def included_files(task):
    """The task file names a task includes, and the includes that name no literal file."""
    names, dynamic = [], []
    for action in INCLUDES:
        if action in task:
            target = task[action]
            target = target if isinstance(target, str) else (target or {}).get("file", "")
            matched = LITERAL.fullmatch(str(target))
            (names if matched else dynamic).append(matched.group(1) if matched else str(target))
    return names, dynamic


def defined_names(task):
    """The bootwright_* names a task defines itself: set_fact keys, a register, a task's vars and a loop variable."""
    names = set(task.get("ansible.builtin.set_fact") or {}) | set(task.get("vars") or {})
    names |= {task["register"]} if "register" in task else set()
    names |= {(task.get("loop_control") or {}).get("loop_var", "")}
    return {name for name in names if isinstance(name, str)}


def entry_point_reads(role, entry, root=ROLES):
    """The bootwright_* variables an entry point's task files read that nothing in them or in the role defines, and any dynamic include."""
    pending, seen, text, defined, dynamic = [entry], [], "", set(), []
    while pending:
        name = pending.pop()
        if name in seen:
            continue
        seen.append(name)
        source = (root / role / "tasks" / (name + ".yml")).read_text()
        text += source
        for task in tasks_of(source):
            names, unresolved = included_files(task)
            pending += names
            dynamic += unresolved
            defined |= defined_names(task)
    for file in ("defaults/main.yml", "vars/main.yml"):
        path = root / role / file
        if path.is_file():
            defined |= set(yaml.safe_load(path.read_text()) or {})
    specification = yaml.safe_load((root / role / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    declared = set(specification[entry].get("options") or {})
    return set(VARIABLE.findall(text)) - declared - defined, dynamic


def entry_points():
    found = []
    for path in sorted(ROLES.glob("*/meta/argument_specs.yml")):
        for entry in yaml.safe_load(path.read_text())["argument_specs"]:
            found.append((path.parents[1].name, entry))
    assert len(found) > 50, "the walk no longer sees the entry points"
    return found


@pytest.mark.parametrize("role,entry", entry_points())
def test_an_entry_point_reads_only_what_its_specification_declares(role, entry):
    reads, dynamic = entry_point_reads(role, entry)
    assert dynamic == [], "%s/%s includes a task file by a name no walk resolves: %s" % (role, entry, dynamic)
    allowed = UNDECLARED_READS.get((role, entry), set())
    assert reads - allowed == set(), "%s/%s reads %s, which its argument specification does not declare" % (
        role, entry, sorted(reads - allowed))
    assert allowed - reads == set(), "%s/%s no longer reads %s; remove them from UNDECLARED_READS" % (
        role, entry, sorted(allowed - reads))


def test_the_allowance_names_only_entry_points_that_exist():
    assert set(UNDECLARED_READS) <= set(entry_points())


def test_a_seeded_undeclared_read_is_refused_and_a_defined_one_is_not(tmp_path):
    role = tmp_path / "demo"
    (role / "tasks").mkdir(parents=True)
    (role / "meta").mkdir()
    (role / "defaults").mkdir()
    (role / "defaults" / "main.yml").write_text("bootwright_from_defaults: 1\n")
    (role / "meta" / "argument_specs.yml").write_text(
        "argument_specs:\n  apply:\n    options:\n      bootwright_demo_request: {type: dict}\n")
    (role / "tasks" / "apply.yml").write_text(
        "- ansible.builtin.set_fact: {bootwright_made: '{{ bootwright_demo_request }}'}\n"
        "- ansible.builtin.include_tasks: \"{{ role_path }}/tasks/more.yml\"\n"
        "- ansible.builtin.debug: {msg: '{{ bootwright_made }} {{ bootwright_from_defaults }}'}\n")
    (role / "tasks" / "more.yml").write_text(
        "- block:\n  - ansible.builtin.debug: {msg: '{{ bootwright_demo_digest }}'}\n"
        "- ansible.builtin.include_tasks: '{{ other }}.yml'\n")
    assert entry_point_reads("demo", "apply", tmp_path) == ({"bootwright_demo_digest"}, ["{{ other }}.yml"])
