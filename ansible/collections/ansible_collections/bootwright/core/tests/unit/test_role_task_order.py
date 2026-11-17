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

from ansible_collections.bootwright.core.tests.unit import role_tasks

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"

# A registered name is referenced as a bare Jinja variable or through one of its
# result fields, anywhere in the task's own arguments and conditions.
REFERENCE = re.compile(r"\b([a-z][a-z0-9_]*)\b")


def task_files():
    return sorted(ROLES.glob("*/tasks/*.yml"))


def load(path):
    parsed = yaml.safe_load(path.read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def defined_names(task, provided=frozenset()):
    """The variables a task defines: its register and the facts it sets, less the facts something outside the file already provides."""
    names = {task["register"]} if isinstance(task.get("register"), str) else set()
    facts = task.get("ansible.builtin.set_fact")
    if isinstance(facts, dict):
        names |= {name for name in facts if name != "cacheable"} - provided
    return names


def provided_facts(path):
    """The names a fact read in this file may come from before the file sets it: the role's defaults and vars,
    and a register or set_fact in another task file of the role, which a caller or an earlier pass ran."""
    role = path.parents[1]
    names = set()
    for name in ("defaults", "vars"):
        main = role / name / "main.yml"
        if main.exists():
            names |= set(yaml.safe_load(main.read_text()) or {})
    for other in sorted(role.glob("tasks/*.yml")):
        if other != path:
            for visit in role_tasks.visits(yaml.safe_load(other.read_text()) or []):
                names |= defined_names(visit.task)
    return names


def referenced_names(task):
    """Every identifier the task mentions outside what it defines and outside the tasks nested in it."""
    ignored = ("register", "name", *role_tasks.SECTIONS)
    if "block" in task:
        ignored += ("vars",)
    body = {key: value for key, value in task.items() if key not in ignored}
    if isinstance(body.get("ansible.builtin.set_fact"), dict):
        body["ansible.builtin.set_fact"] = {name: value for name, value in body["ansible.builtin.set_fact"].items()
                                            if name == "cacheable"}
    return set(REFERENCE.findall(yaml.safe_dump(body, default_flow_style=False)))


def reads_before_definition(label, listed, provided=frozenset()):
    """Each read of a variable in the file's tasks, in the order a play reaches them, that a later task defines.

    A block's own `vars` are templated where its tasks use them, so they are
    no read at the block."""
    tasks = [visit.task for visit in role_tasks.visits(listed)]
    later = set()
    for task in tasks:
        later |= defined_names(task, provided)
    problems = []
    for index, task in enumerate(tasks):
        current = defined_names(task)
        for name in sorted((referenced_names(task) & later) - current):
            problems.append("%s: task %d (%s) reads %s, which a later task defines"
                            % (label, index + 1, task.get("name", "unnamed"), name))
        later -= current
    return problems


def test_every_role_has_task_files():
    assert task_files(), "no role task files were found to check"


def test_agent_disk_proof_is_frozen_into_the_pre_media_substrate_proof():
    tasks = role_tasks.flatten(load(ROLES / "containercluster_install_agent/tasks/boot.yml"))
    before = next(index for index, task in enumerate(tasks)
                  if task.get("ansible.builtin.include_role", {}).get("tasks_from") == "pre_boot")
    insert = next(index for index, task in enumerate(tasks)
                  if task.get("vars", {}).get("containercluster_install_agent_port_operation") == "media_insert")
    assert before < insert
    assert "'root_disk': containercluster_install_agent_node.hardware.rootDisk | default({})" in tasks[before]["vars"]["bootwright_machine_port"]
    proof = load(ROLES / "substrate_baremetal_machine/tasks/target_proof.yml")
    read = next(task for task in proof if "bootwright.core.redfish_system_inspect" in task)
    assert read["bootwright.core.redfish_system_inspect"]["root_disk"] == "{{ substrate_baremetal_machine_target_root_disk | default({}) }}"
    refused = next(task for task in proof if task.get("when") == "substrate_baremetal_machine_target.root_disk_refused | default(false) | bool")
    assert refused["ansible.builtin.set_fact"]["substrate_baremetal_machine_refusal"] == "hardware-mismatch"


def test_no_task_reads_a_variable_a_later_task_registers():
    problems = []
    for path in task_files():
        problems += reads_before_definition(path.name, yaml.safe_load(path.read_text()) or [], provided_facts(path))
    assert not problems, "\n".join(problems)


FIXTURE = """
- name: Plain
  ansible.builtin.command: /usr/bin/true
  register: first
- name: Guarded
  when: first.rc == 0
  block:
    - name: Reads a result that a later task in the block registers
      ansible.builtin.debug:
        msg: "{{ block_result.stdout }}"
    - name: Registers it
      ansible.builtin.command: /usr/bin/true
      register: block_result
    - name: Reads a fact that a later task in the block sets
      ansible.builtin.debug:
        msg: "{{ block_fact }}"
    - name: Sets it
      ansible.builtin.set_fact:
        block_fact: 1
  rescue:
    - name: Reads a result that the always section registers
      ansible.builtin.debug:
        msg: "{{ always_result }}"
  always:
    - name: Registers it
      ansible.builtin.command: /usr/bin/true
      register: always_result
- name: Reads a name its own block section nests two deep
  ansible.builtin.debug:
    msg: "{{ deep_result }}"
- name: Wrapper
  block:
    - name: Inner
      block:
        - name: Registers deep
          ansible.builtin.command: /usr/bin/true
          register: deep_result
"""

IN_ORDER = """
- name: Registers
  block:
    - name: Registers first
      ansible.builtin.command: /usr/bin/true
      register: ordered_result
    - name: Sets a fact from it
      ansible.builtin.set_fact:
        ordered_fact: "{{ ordered_result.stdout }}"
  rescue:
    - name: Reads both
      ansible.builtin.debug:
        msg: "{{ ordered_result }} {{ ordered_fact }}"
- name: Redefines its own fact from itself
  ansible.builtin.set_fact:
    ordered_fact: "{{ ordered_fact | default(0) }}"
"""


def test_a_read_before_its_register_or_set_fact_is_caught_inside_nested_sections():
    found = reads_before_definition("fixture.yml", yaml.safe_load(FIXTURE))
    assert [re.search(r"\((.*)\) reads (\w+)", problem).groups() for problem in found] == [
        ("Reads a result that a later task in the block registers", "block_result"),
        ("Reads a fact that a later task in the block sets", "block_fact"),
        ("Reads a result that the always section registers", "always_result"),
        ("Reads a name its own block section nests two deep", "deep_result"),
    ]


def test_a_fact_another_file_or_the_defaults_provide_may_be_read_before_the_file_sets_it_but_a_register_may_not():
    listed = yaml.safe_load(FIXTURE)
    assert len(reads_before_definition("fixture.yml", listed, {"block_fact"})) == 3
    registers_still_count = reads_before_definition("fixture.yml", listed, {"block_result", "always_result", "deep_result"})
    assert len(registers_still_count) == 4


def test_a_read_after_its_definition_inside_a_block_and_a_task_reading_its_own_fact_are_not_flagged():
    assert reads_before_definition("fixture.yml", yaml.safe_load(IN_ORDER)) == []
