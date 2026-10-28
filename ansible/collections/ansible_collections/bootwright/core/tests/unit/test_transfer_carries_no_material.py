"""No file transfer to an execution host carries lent material.

A lent file lives on the controller. copy, unarchive and assemble without
remote_src, template, script and synchronize read or render their source on
the controller and send it to the execution host as a file transfer, which
pipelining does not cover. Lent material reaches a host only inside a module
argument, whose payload crosses the connection's standard input
(specs/security.md#sensitive-material), so a task that writes it uses
bootwright.core.private_file, which never runs async (an async module's
payload is written to a file first). A value carries lent material when it
reads a file (a file, template, unvault or pipe lookup or query, or the bytes
a slurp of a lent file registered) beside a reference to lent material,
directly or through a variable that does; a transfer's source carries it when
it names a lent file at all. A template whose source is a variable fails
closed unless the variable resolves only to templates of the role, each checked. Every role's tasks,
handlers, defaults and vars, and every playbook, are checked.
"""

from __future__ import annotations

import pathlib
import re

import pytest
from ansible.parsing.dataloader import DataLoader

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLES = COLLECTION / "roles"
PLAYBOOKS = COLLECTION / "playbooks"
LOADER = DataLoader()

PREFIXES = ("ansible.builtin.", "ansible.legacy.", "")
TRANSFERS = {prefix + name: name for prefix in PREFIXES for name in ("copy", "template", "unarchive", "assemble", "script")}
TRANSFERS.update({"ansible.posix.synchronize": "synchronize", "synchronize": "synchronize"})
TRANSFERS.update({prefix + "uri": "uri" for prefix in PREFIXES})
SLURPS = {prefix + "slurp" for prefix in PREFIXES}
WRITER = "bootwright.core.private_file"
LENT = re.compile(r"\bbootwright_\w+_material\b|\bbootwright_machine_port\b")
READ = re.compile(r"\b(?:lookup|query|q)\(\s*['\"](?:ansible\.(?:builtin|legacy)\.)?(?:file|template|unvault|pipe)['\"]")
LOOPED_READ = re.compile(r"^with_(?:file|template|unvault|pipe)$")
NAME = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*\b")
TEMPLATE_FILE = re.compile(r"[A-Za-z0-9_./-]+\.j2\b")
EXPRESSION = re.compile(r"\{\{(.*?)\}\}", re.S)
LITERAL = re.compile(r"'[^']*'|\"[^\"]*\"")
REFERENCE = re.compile(r"(?<![.|\w])[A-Za-z_][A-Za-z0-9_]*")
JINJA_WORDS = frozenset(("if", "else", "and", "or", "not", "in", "is", "true", "false", "none", "True", "False", "None", "defined"))
PLAY_SECTIONS = ("pre_tasks", "tasks", "post_tasks", "handlers")


def load(path):
    return LOADER.load_from_file(str(path), trusted_as_template=True) or []


def walk(tasks, inherited=()):
    """Each task with the variables of the blocks around it."""
    for task in tasks or []:
        if not isinstance(task, dict):
            continue
        scopes = inherited + (task.get("vars") or {},)
        if "block" in task:
            for key in ("block", "rescue", "always"):
                yield from walk(task.get(key) or [], scopes)
        else:
            yield task, scopes


def role_tasks(role):
    for directory in ("tasks", "handlers"):
        for path in sorted((role / directory).glob("*.yml")):
            for task, scopes in walk(load(path)):
                yield path, task, scopes


def playbook_tasks(path):
    for play in load(path):
        if not isinstance(play, dict):
            continue
        for section in PLAY_SECTIONS:
            for task, scopes in walk(play.get(section), (play.get("vars") or {},)):
                yield path, task, scopes


def variable_files(role):
    for directory in ("defaults", "vars"):
        base = role / directory
        yield from sorted(base.glob("*.yml"))
        yield from sorted((base / "main").glob("*.yml"))


def define(found, values):
    for key, value in values.items():
        found.setdefault(key, []).append(str(value))


def definitions(files, tasks):
    """Every definition of every variable, by name, each source's text kept
    so an override is checked as well as the value it overrides, and the
    registers of slurps with the source each read."""
    found, slurped = {}, {}
    for path in files:
        value = load(path)
        if isinstance(value, dict):
            define(found, value)
    for _path, task, scopes in tasks:
        for scope in scopes:
            define(found, scope)
        for module in ("ansible.builtin.set_fact", "set_fact"):
            facts = task.get(module) or {}
            if isinstance(facts, dict):
                define(found, facts)
        module = next((key for key in SLURPS if key in task), None)
        if module is not None and task.get("register"):
            slurped[task["register"]] = str(task[module])
    return found, slurped


def tainted(found, slurped):
    """The variables that name lent material and those that carry its bytes,
    each closed over the variables that refer to them."""
    names, reads = set(), set()
    changed = True
    while changed:
        changed = False
        for key, text in ((key, text) for key, texts in found.items() for text in texts):
            used = set(NAME.findall(text))
            if key not in names and (LENT.search(text) or used & names):
                names.add(key)
                changed = True
            if key not in reads and (READ.search(text) and (LENT.search(text) or used & names) or used & reads):
                reads.add(key)
                changed = True
        for register, source in slurped.items():
            if register not in reads and (LENT.search(source) or set(NAME.findall(source)) & names):
                reads.add(register)
                changed = True
    return names, reads


def carries(text, names, reads, slurped):
    used = set(NAME.findall(text))
    if used & reads or READ.search(text) and (LENT.search(text) or used & names):
        return True
    return "b64decode" in text and bool(used & set(slurped))


def names_lent(text, names):
    return bool(LENT.search(text) or set(NAME.findall(text)) & names)


def references(text):
    """The variables a value's expressions refer to: no attribute, filter,
    string literal or Jinja word."""
    named = set()
    for expression in EXPRESSION.findall(text):
        named |= set(REFERENCE.findall(LITERAL.sub(" ", expression))) - JINJA_WORDS
    return named


def candidate_templates(source, found):
    """The template files a variable source can name: every .j2 file the
    definitions it refers to list, closed over the variables they refer to,
    or None when the closure reaches a role variable no definition names, or a
    definition that builds a file name around an expression. A runner input
    (bootwright_*) only selects among what the role lists."""
    seen, pending, files = set(), references(source), set()
    while pending:
        name = pending.pop()
        if name in seen:
            continue
        seen.add(name)
        if name not in found:
            if name.startswith("bootwright_"):
                continue
            return None
        for text in found[name]:
            listed = TEMPLATE_FILE.findall(text)
            if listed and "{{" in text:
                return None
            files |= set(listed)
            pending |= references(text)
    return files


def template_path(base, name):
    if base is None or ".." in pathlib.PurePosixPath(name).parts or name.startswith("/"):
        return None
    return base / "templates" / name


def template_offences(label, template, base, found, names, reads, slurped):
    if "{{" not in template:
        files = {template}
    else:
        files = candidate_templates(template, found)
        if not files or not all(template_path(base, name) is not None and template_path(base, name).is_file() for name in files):
            return [label + " renders a template named by a variable"]
    if base is None:
        return []
    for name in sorted(files):
        path = template_path(base, name)
        if path is not None and path.is_file() and carries(path.read_text(), names, reads, slurped):
            return [label + " renders lent material into a transfer"]
    return []


def transfer_offences(label, kind, arguments, task, base, taint):
    found, names, reads, slurped = taint
    flagged = []
    if not isinstance(arguments, dict):
        arguments = {"cmd": arguments}
    if kind in ("copy", "unarchive", "assemble", "uri") and arguments.get("remote_src") in (True, "true", "yes"):
        return flagged
    if any(LOOPED_READ.match(key) and names_lent(str(task[key]), names) for key in task):
        flagged.append(label + " reads lent material in its loop")
    source = " ".join(str(arguments.get(key, "")) for key in ("src", "cmd", "_raw_params", "free_form"))
    if "item" in NAME.findall(source):
        source += str(task.get("loop", "")) + str(task.get("with_items", ""))
    if names_lent(source, names) or kind in ("script", "synchronize") and carries(str(arguments), names, reads, slurped):
        flagged.append(label + " transfers a lent file")
    if carries(str(arguments.get("content", "")), names, reads, slurped):
        flagged.append(label + " transfers content holding lent material")
    if kind == "template":
        flagged += template_offences(label, str(arguments.get("src", "")), base, found, names, reads, slurped)
    return flagged


def offences(tasks, files, base=None):
    tasks = list(tasks)
    found, slurped = definitions(files, tasks)
    names, reads = tainted(found, slurped)
    flagged = []
    for path, task, _scopes in tasks:
        label = "%s %s: %s" % (base.name if base is not None else path.parent.name, path.name, task.get("name"))
        if WRITER in task and "async" in task:
            flagged.append(label + " runs the private file writer async")
        module = next((key for key in TRANSFERS if key in task), None)
        if module is not None:
            flagged += transfer_offences(label, TRANSFERS[module], task[module], task, base, (found, names, reads, slurped))
    return flagged


def role_offences(role):
    return offences(role_tasks(role), variable_files(role), role)


@pytest.mark.parametrize("role", sorted(path.name for path in ROLES.iterdir() if (path / "tasks").is_dir()))
def test_no_file_transfer_carries_lent_material(role):
    assert role_offences(ROLES / role) == []


@pytest.mark.parametrize("playbook", sorted(str(path.relative_to(PLAYBOOKS)) for path in PLAYBOOKS.rglob("*.yml")))
def test_no_playbook_transfer_carries_lent_material(playbook):
    assert offences(playbook_tasks(PLAYBOOKS / playbook), ()) == []


SHAPES = """
- name: Direct read
  ansible.builtin.copy:
    content: "{{ lookup('ansible.builtin.file', bootwright_example_material.password) }}"
    dest: /x
- name: Read through a variable
  ansible.builtin.copy:
    content: '{{ example_role_key }}'
    dest: /x
- name: Read of a named path
  ansible.builtin.copy:
    content: "{{ lookup('ansible.builtin.file', example_role_path) }}"
    dest: /x
- name: Lent source
  ansible.builtin.copy:
    src: '{{ item.source }}'
    dest: /x
  loop:
    - {source: '{{ bootwright_example_material.key }}'}
- name: Legacy name
  ansible.legacy.copy:
    src: '{{ bootwright_example_material.key }}'
    dest: /x
- name: Short name
  copy:
    content: "{{ q('file', bootwright_example_material.key) }}"
    dest: /x
- name: Assembled
  ansible.builtin.assemble:
    src: '{{ example_role_path }}'
    dest: /x
- name: Scripted
  ansible.builtin.script: "{{ bootwright_example_material.key }}"
- name: Synchronized
  ansible.posix.synchronize:
    src: '{{ bootwright_example_material.key }}'
    dest: /x
- name: Template lookup
  ansible.builtin.copy:
    content: "{{ lookup('ansible.builtin.template', bootwright_example_material.key) }}"
    dest: /x
- name: Unvaulted
  ansible.builtin.copy:
    content: "{{ lookup('ansible.builtin.unvault', bootwright_example_material.key) }}"
    dest: /x
- name: Piped
  ansible.builtin.copy:
    content: "{{ lookup('ansible.builtin.pipe', 'cat ' ~ bootwright_example_material.key) }}"
    dest: /x
- name: Looped read
  ansible.builtin.copy:
    content: '{{ item }}'
    dest: /x
  with_file:
    - '{{ bootwright_example_material.key }}'
- name: Slurp a lent file
  ansible.builtin.slurp:
    src: '{{ bootwright_example_material.key }}'
  register: example_role_slurped
- name: Slurped bytes
  ansible.builtin.copy:
    content: '{{ example_role_slurped.content | b64decode }}'
    dest: /x
- name: Variable template
  ansible.builtin.template:
    src: '{{ example_role_template }}'
    dest: /x
- name: Listed template
  ansible.builtin.template:
    src: '{{ example_role_selected }}'
    dest: /x
- name: Listed lent template
  ansible.builtin.template:
    src: '{{ example_role_leaky }}'
    dest: /x
- name: Unknown variable template
  ansible.builtin.template:
    src: '{{ example_role_unknown }}'
    dest: /x
- name: Partly unknown variable template
  ansible.builtin.template:
    src: "{{ example_role_choices[example_role_unknown_kind] }}"
    dest: /x
- name: Mixed variable template
  ansible.builtin.template:
    src: '{{ example_role_mixed }}'
    dest: /x
- name: Nested listed template
  ansible.builtin.template:
    src: '{{ example_role_nested }}'
    dest: /x
- name: Nested lent template
  ansible.builtin.template:
    src: sub/leaky.conf.j2
    dest: /x
- name: Overridden read
  ansible.builtin.copy:
    content: '{{ example_role_note }}'
    dest: /x
- name: Uppercase read
  ansible.builtin.copy:
    content: '{{ example_role_Upper }}'
    dest: /x
- name: Posted
  ansible.builtin.uri:
    url: https://example.invalid/
    src: '{{ bootwright_example_material.key }}'
- name: Async writer
  bootwright.core.private_file:
    content: '{{ example_role_key }}'
    path: /x
  async: 60
- name: Value only
  ansible.builtin.copy:
    content: '{{ bootwright_example_material.marker }}'
    dest: /x
- name: On the host already
  ansible.builtin.copy:
    src: '{{ bootwright_example_material.key }}'
    dest: /x
    remote_src: true
- name: Through the payload
  bootwright.core.private_file:
    content: '{{ example_role_key }}'
    path: /x
"""

REFUSED = [
    "example_role main.yml: Direct read transfers content holding lent material",
    "example_role main.yml: Read through a variable transfers content holding lent material",
    "example_role main.yml: Read of a named path transfers content holding lent material",
    "example_role main.yml: Lent source transfers a lent file",
    "example_role main.yml: Legacy name transfers a lent file",
    "example_role main.yml: Short name transfers content holding lent material",
    "example_role main.yml: Assembled transfers a lent file",
    "example_role main.yml: Scripted transfers a lent file",
    "example_role main.yml: Synchronized transfers a lent file",
    "example_role main.yml: Template lookup transfers content holding lent material",
    "example_role main.yml: Unvaulted transfers content holding lent material",
    "example_role main.yml: Piped transfers content holding lent material",
    "example_role main.yml: Looped read reads lent material in its loop",
    "example_role main.yml: Slurped bytes transfers content holding lent material",
    "example_role main.yml: Variable template renders a template named by a variable",
    "example_role main.yml: Listed lent template renders lent material into a transfer",
    "example_role main.yml: Unknown variable template renders a template named by a variable",
    "example_role main.yml: Partly unknown variable template renders a template named by a variable",
    "example_role main.yml: Mixed variable template renders a template named by a variable",
    "example_role main.yml: Nested lent template renders lent material into a transfer",
    "example_role main.yml: Overridden read transfers content holding lent material",
    "example_role main.yml: Uppercase read transfers content holding lent material",
    "example_role main.yml: Posted transfers a lent file",
    "example_role main.yml: Async writer runs the private file writer async",
]


def test_the_rule_sees_each_shape_it_refuses(tmp_path):
    role = tmp_path / "example_role"
    (role / "tasks").mkdir(parents=True)
    (role / "defaults" / "main").mkdir(parents=True)
    (role / "vars").mkdir()
    (role / "defaults" / "main" / "keys.yml").write_text(
        "example_role_key: \"{{ lookup('ansible.builtin.file', bootwright_example_material.key) }}\"\n"
        "example_role_note: \"{{ lookup('ansible.builtin.file', bootwright_example_material.key) }}\"\n"
        "example_role_Upper: \"{{ lookup('ansible.builtin.file', bootwright_example_material.key) }}\"\n")
    (role / "vars" / "main.yml").write_text("example_role_path: '{{ bootwright_example_material.certificate }}'\n"
                                            "example_role_choices: {a: clean.conf.j2}\n"
                                            "example_role_selected: '{{ example_role_choices[bootwright_example_request.kind] }}'\n"
                                            "example_role_nested_choices: {a: sub/clean.conf.j2}\n"
                                            "example_role_nested: '{{ example_role_nested_choices[bootwright_example_request.kind] }}'\n"
                                            "example_role_mixed: '{{ bootwright_example_request.prefix }}clean.conf.j2'\n"
                                            "example_role_note: plain\n"
                                            "example_role_leaky: leaky.conf.j2\n")
    (role / "templates").mkdir()
    (role / "templates" / "clean.conf.j2").write_text("listen {{ example_role_port }}\n")
    (role / "templates" / "leaky.conf.j2").write_text("{{ lookup('ansible.builtin.file', bootwright_example_material.key) }}\n")
    (role / "templates" / "sub").mkdir()
    (role / "templates" / "sub" / "clean.conf.j2").write_text("listen {{ example_role_port }}\n")
    (role / "templates" / "sub" / "leaky.conf.j2").write_text("{{ example_role_key }}\n")
    (role / "tasks" / "main.yml").write_text(SHAPES)
    assert role_offences(role) == REFUSED


def test_handlers_and_playbooks_are_checked_too(tmp_path):
    role = tmp_path / "example_role"
    (role / "tasks").mkdir(parents=True)
    (role / "handlers").mkdir()
    (role / "tasks" / "main.yml").write_text("[]\n")
    (role / "handlers" / "main.yml").write_text("""
- name: Handler transfer
  ansible.builtin.copy:
    src: '{{ bootwright_example_material.key }}'
    dest: /x
""")
    assert role_offences(role) == ["example_role main.yml: Handler transfer transfers a lent file"]
    playbook = tmp_path / "plays" / "example.yml"
    playbook.parent.mkdir()
    playbook.write_text("""
- name: A play
  hosts: bootwright_target
  pre_tasks:
    - name: Play transfer
      ansible.builtin.copy:
        content: "{{ lookup('ansible.builtin.file', bootwright_example_material.key) }}"
        dest: /x
""")
    assert offences(playbook_tasks(playbook), ()) == ["plays example.yml: Play transfer transfers content holding lent material"]
