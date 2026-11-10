"""Every role pattern that validates a whole value ends at the end of the value.

Ansible's match and search tests, and the match and search filters, use Python
re.match and re.search, where a bare `$` also matches just before a trailing
newline, so a value such as "lab\\n" passes a check meant to admit only "lab".
The exact end anchor is `$(?!\\n)`: written inside a Jinja string literal the
two characters `\\n` are unescaped to a newline, which the pattern then refuses
after the anchor. (`\\Z` is not used because Jinja's lexer decodes string
literals with Python's unicode-escape codec, which deprecates it as an invalid
escape: a DeprecationWarning from jinja2/lexer.py.) This scan reads every YAML
file of the roles and playbooks and every role template, and fails on any
pattern with a bare `$` end anchor, written with either quote or passed through
a variable that the same file or its role's defaults and vars define, and on a
fixed-width digest pattern with no end anchor, so the gap cannot return. The
Python plugins have the same scan in test_python_pattern_anchors.py.
"""

from __future__ import annotations

import pathlib
import re

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLES = COLLECTION / "roles"
GUARDS = ("$(?!\n)", "$(?!\\n)")

CALL = r"""(?:\bis\s+(?:not\s+)?(?:match|search)|\bregex_search|['"](?:match|search)['"]\s*,)\s*\(?\s*"""
# The pattern argument of a match or search test or filter: a quoted literal, or a bare variable name.
LITERAL = re.compile(CALL + r"""(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")""", re.S)
VARIABLE = re.compile(CALL + r"""([A-Za-z_][A-Za-z0-9_]*)\s*[,)]""", re.S)
PATTERN_VARIABLE = re.compile(r"(pattern|regex)", re.I)


def role_files():
    """Every YAML file a role or a playbook holds, at any depth, and every role template."""
    found = set(ROLES.rglob("*.j2"))
    for suffix in ("*.yml", "*.yaml"):
        found |= set(ROLES.rglob(suffix)) | set((COLLECTION / "playbooks").rglob(suffix))
    return sorted(found)


def role_of(path):
    if ROLES not in path.parents:
        return None
    return ROLES / path.relative_to(ROLES).parts[0]


def strings(node, key=""):
    """Every string of a parsed YAML document with the mapping key that holds it."""
    if isinstance(node, str):
        yield key, node
    elif isinstance(node, dict):
        for name, value in node.items():
            yield from strings(value, str(name))
    elif isinstance(node, list):
        for value in node:
            yield from strings(value, key)


def texts(path):
    """Every (key, string) of a YAML file, or the raw text of a template."""
    if path.suffix == ".j2":
        return [("", path.read_text())]
    return [pair for document in yaml.safe_load_all(path.read_text()) for pair in strings(document)]


def assigned(path):
    """The static string values each key holds in this file and in its role's defaults and vars."""
    values, sources, role = {}, [path], role_of(path)
    if role:
        sources += [other for part in ("defaults", "vars") for other in sorted((role / part).rglob("*.y*ml"))]
    for source in sources:
        for key, text in texts(source):
            if "{{" not in text and "{%" not in text:
                values.setdefault(key, []).append(text)
    return values


def jinja_unescaped(pattern):
    """The pattern as the regular expression engine receives it: Jinja decodes a string literal's escapes."""
    try:
        return pattern.encode("ascii", "backslashreplace").decode("unicode-escape")
    except UnicodeDecodeError:
        return pattern


def bare_end_anchors(pattern):
    """Positions of a `$` that is neither escaped, inside a class, nor followed by the newline guard."""
    found, index, in_class = [], 0, False
    while index < len(pattern):
        character = pattern[index]
        if character == "\\":
            index += 2
            continue
        if in_class:
            in_class = character != "]"
        elif character == "[":
            in_class = True
            index += 1
            index += pattern[index:index + 1] == "^"
            index += pattern[index:index + 1] == "]"
            continue
        elif character == "$" and not pattern[index:].startswith(GUARDS):
            found.append(index)
        index += 1
    return found


def violations(path):
    """The patterns of one file that end with a bare `$`, or a fixed-width digest with no end anchor."""
    names = {} if path.suffix == ".j2" else assigned(path)
    problems = []
    for key, text in texts(path):
        patterns = [jinja_unescaped(group) for match in LITERAL.finditer(text) for group in match.groups() if group is not None]
        patterns += [value for match in VARIABLE.finditer(text) for value in names.get(match.group(1), [])]
        if PATTERN_VARIABLE.search(key) and "{{" not in text:
            patterns.append(text)
        problems += [pattern for pattern in patterns if bare_end_anchors(pattern) or pattern.endswith("{64}")]
    return problems


def test_the_scan_sees_the_roles_and_the_playbooks():
    files = role_files()
    assert any("playbooks" in path.parts for path in files) and any(path.suffix == ".j2" for path in files)
    guarded = [pattern for path in files for _key, text in texts(path)
               for pattern in LITERAL.findall(text) if "$(?!\\n)" in "".join(pattern)]
    assert len(guarded) > 50, "the scan found %d guarded patterns, so it no longer reads the roles" % len(guarded)


@pytest.mark.parametrize("source, refused", [
    ("- assert:\n    that:\n      - name is match('^[a-z]+$')\n", True),
    ("- assert:\n    that:\n      - name is match(\"^[a-z]+$\")\n", True),
    ("- assert:\n    that:\n      - name is search('^[a-z]+$')\n", True),
    ("- assert:\n    that:\n      - (name | regex_search('^[a-z]+$')) is not none\n", True),
    ("- assert:\n    that:\n      - names | select('match', '^[a-z]+$') | list | length == 1\n", True),
    ("- assert:\n    that:\n      - sha is match('[0-9a-f]{64}')\n", True),
    ("- assert:\n    that:\n      - name is match('^a\\\\\\\\$')\n", True),
    ("- vars:\n    shape: '^[a-z]+$'\n  assert:\n    that:\n      - name is match(shape)\n", True),
    ("- ansible.builtin.set_fact:\n    form: \"^[a-z]+$\"\n- assert:\n    that:\n      - name is match(form)\n", True),
    ("- assert:\n    that:\n      - name is match('^[a-z]+$(?!\\n)')\n", False),
    ("- assert:\n    that:\n      - name is match(\"^[a-z]+$(?!\\n)\")\n", False),
    ("- assert:\n    that:\n      - name is match('^a\\\\$')\n", False),
    ("- assert:\n    that:\n      - name is match('^[$]+$(?!\\n)')\n", False),
    ("- vars:\n    shape: '^[a-z]+$(?!\\n)'\n  assert:\n    that:\n      - name is match(shape)\n", False),
])
def test_the_scan_refuses_a_bare_end_anchor_however_it_is_written(tmp_path, source, refused):
    path = tmp_path / "playbooks" / "example.yml"
    path.parent.mkdir()
    path.write_text(source)
    assert bool(violations(path)) is refused, source


@pytest.mark.parametrize("path", role_files(), ids=lambda path: str(path.relative_to(COLLECTION)))
def test_a_role_pattern_ends_at_the_end_of_the_value(path):
    assert violations(path) == [], "a pattern must end with $(?!\\n), not a bare $"


@pytest.mark.parametrize("test", ["match", "search"])
@pytest.mark.parametrize("value, admitted", [("lab", True), ("lab\n", False), ("lab\n\n", False), ("lab9", False)])
@pytest.mark.parametrize("quote", ["'", '"'])
def test_the_guarded_anchor_is_exact_through_ansible_templating(test, value, admitted, quote):
    templar = Templar(loader=DataLoader(), variables={"value": value})
    expression = trust_as_template("value is %s(%s^[a-z]+$(?!\\n)%s)" % (test, quote, quote))
    assert templar.evaluate_conditional(expression) is admitted


def test_a_bare_anchor_admits_a_trailing_newline_through_ansible_templating():
    templar = Templar(loader=DataLoader(), variables={"value": "lab\n"})
    assert templar.evaluate_conditional(trust_as_template("value is match('^[a-z]+$')")) is True
