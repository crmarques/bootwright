"""A role reads a lent material file byte for byte.

A password Secret part is one nonempty UTF-8 line that may begin or end in
whitespace (specs/security.md, Secret parts), and the runner lends it as a file
holding exactly those bytes. ansible.builtin.file lookup right-strips what it
reads unless it is given rstrip=false, so a role that omits it sends the BMC,
libvirt or sushy a password other than the one the owner stored. Every lookup
of that plugin in a role's tasks, defaults, templates or the collection's
playbooks must therefore pass rstrip=false.
"""

from __future__ import annotations

import pathlib
import re

import pytest

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROOTS = (COLLECTION / "roles", COLLECTION / "playbooks")
SUFFIXES = {".yml", ".yaml", ".j2"}
LOOKUP = re.compile(r"""\b(?:lookup|query|q)\(\s*(['"])(?:ansible\.builtin\.)?file\1""")
EXACT = re.compile(r"""\brstrip\s*=\s*(?:false|False)\b""")


def calls(text):
    """The offset and the argument text of each call of the file lookup in the text."""
    for found in LOOKUP.finditer(text):
        depth, end = 1, found.start() + found.group(0).index("(") + 1
        quote = ""
        while depth and end < len(text):
            char = text[end]
            if quote:
                quote = "" if char == quote else quote
            elif char in "'\"":
                quote = char
            elif char == "(":
                depth += 1
            elif char == ")":
                depth -= 1
            end += 1
        yield found.start(), text[found.start():end]


def lookups():
    for root in ROOTS:
        for path in sorted(root.rglob("*")):
            if path.is_file() and path.suffix in SUFFIXES:
                text = path.read_text()
                for offset, call in calls(text):
                    yield path.relative_to(COLLECTION), text.count("\n", 0, offset) + 1, " ".join(call.split())


def test_the_scan_finds_the_roles_lookups():
    assert len(list(lookups())) > 100


@pytest.mark.parametrize("where", [
    "{{ lookup('ansible.builtin.file', path) }}",
    '{{ lookup("ansible.builtin.file", path) }}',
    "{{ lookup('file', path) }}",
    "{{ query('ansible.builtin.file', path | basename) }}",
], ids=["single quotes", "double quotes", "short name", "query"])
def test_a_lookup_without_rstrip_is_refused(where):
    (offset, call), = calls(where)
    assert not EXACT.search(call)


def test_a_lookup_with_rstrip_is_admitted():
    (offset, call), = calls("{{ lookup('ansible.builtin.file', path | default('x'), rstrip=false) }} {{ other }}")
    assert EXACT.search(call)


def test_every_file_lookup_of_a_role_keeps_the_bytes():
    refused = ["%s:%d: %s" % found for found in lookups() if not EXACT.search(found[2])]
    assert not refused, "a lent file is read right-stripped; add rstrip=false:\n" + "\n".join(refused)
