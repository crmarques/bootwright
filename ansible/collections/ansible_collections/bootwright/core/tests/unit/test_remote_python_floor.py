"""Every module and module utility keeps to the managed-host Python floor.

Modules and the module utilities they import run on the managed host, under
its own interpreter, and ansible-core 2.21 supports targets from Python 3.9.
They therefore parse under 3.9 grammar and defer annotation evaluation, so an
annotation written with later syntax is never evaluated there. Action plugins
run only on the controller and are not checked.

ast.parse with feature_version is best-effort. It rejects match statements,
except* and type parameter lists, but it accepts parenthesized context
managers, same-quote nested f-strings, a runtime X | None outside an
annotation and calls to library functions added after 3.9. Only importing
each file under a real 3.9 interpreter proves those; that interpreter-backed
import check is a follow-up.
"""

from __future__ import annotations

import ast
import pathlib

COLLECTION = pathlib.Path(__file__).resolve().parents[2]

REMOTE_PYTHON_FLOOR = (3, 9)


def remote_sources():
    """The modules and module utilities, refusing a root that finds none."""
    modules = sorted((COLLECTION / "plugins" / "modules").glob("*.py"))
    utilities = sorted((COLLECTION / "plugins" / "module_utils").rglob("*.py"))
    names = [path.name for path in utilities]
    assert modules, "no module found under %s" % COLLECTION
    assert "controller_supervisor.py" in names, "no controller_supervisor.py among %s" % names
    return modules + utilities


def defers_annotations(tree):
    for node in tree.body:
        if isinstance(node, ast.ImportFrom) and node.module == "__future__":
            if any(alias.name == "annotations" for alias in node.names):
                return True
    return False


def test_modules_parse_under_the_remote_python_floor():
    refused = []
    for path in remote_sources():
        try:
            ast.parse(path.read_text(), filename=str(path), feature_version=REMOTE_PYTHON_FLOOR)
        except SyntaxError as error:
            refused.append("%s:%s: %s" % (path.relative_to(COLLECTION), error.lineno, error.msg))
    assert not refused, "\n".join(refused)


def test_modules_defer_annotation_evaluation():
    eager = [
        path.relative_to(COLLECTION).as_posix()
        for path in remote_sources()
        if not defers_annotations(ast.parse(path.read_text(), filename=str(path)))
    ]
    assert not eager, "missing from __future__ import annotations: %s" % eager
