"""Every plugin function stays short enough to explain itself.

A body past the limit has phases a reader must hold in their head, and the fix
is to name them. The Go rule in test/architecture/complexity_test.go holds the
same limit over production Go; this one holds it over the collection's plugins,
module utilities included.
"""

from __future__ import annotations

import ast
import pathlib

COLLECTION = pathlib.Path(__file__).resolve().parents[2]

LINE_LIMIT = 100

# The functions already past the limit. The set only shrinks: a new entry means
# a function grew past it instead of being split, and removing one is the point.
# Their split is item B47 in specs/milestones/m1.md.
AWAITING_SPLIT = {
    "plugins/action/controller_protocol.py.ActionModule.run",
}


def lengths(source, label):
    """Each function and method by qualified name, with its lines from its def
    line to its last; a nested function counts toward the one around it."""
    measured = {}

    def walk(node, prefix):
        for child in ast.iter_child_nodes(node):
            if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)):
                name = label + "." + prefix + child.name
                measured[name] = max(measured.get(name, 0), child.end_lineno - child.lineno + 1)
            elif isinstance(child, ast.ClassDef):
                walk(child, prefix + child.name + ".")
            else:
                walk(child, prefix)

    walk(ast.parse(source), "")
    return measured


def plugin_lengths():
    measured = {}
    for path in sorted((COLLECTION / "plugins").rglob("*.py")):
        measured.update(lengths(path.read_text(), path.relative_to(COLLECTION).as_posix()))
    return measured


def test_the_measure_names_each_function_and_method_and_counts_def_to_last_line():
    source = (
        "def first():\n"
        "    return 1\n"
        "\n"
        "\n"
        "class Holder:\n"
        "    def method(self):\n"
        "        def nested():\n"
        "            return 2\n"
        "\n"
        "        return nested()\n"
        "\n"
        "\n"
        "try:\n"
        "    import json\n"
        "except ImportError:\n"
        "    def fallback():\n"
        "        return None\n"
    )
    assert lengths(source, "fixture.py") == {
        "fixture.py.first": 2,
        "fixture.py.Holder.method": 5,
        "fixture.py.fallback": 2,
    }


def test_every_plugin_function_stays_within_the_line_limit():
    unlisted = [
        "%s is %d lines; split it into named phases" % (name, count)
        for name, count in sorted(plugin_lengths().items())
        if count > LINE_LIMIT and name not in AWAITING_SPLIT
    ]
    assert not unlisted, "\n".join(unlisted)


def test_every_function_awaiting_a_split_is_still_over_the_limit():
    measured = plugin_lengths()
    stale = sorted(name for name in AWAITING_SPLIT if measured.get(name, 0) <= LINE_LIMIT)
    assert not stale, "no longer over the limit or gone; remove from AWAITING_SPLIT: %s" % stale
