"""Every module and module utility keeps to the managed-host Python floor.

Modules and the module utilities they import run on the managed host, under
its own interpreter, and the floor is the oldest target Python of the
ansible-core these tests run under. They therefore parse under its grammar and
defer annotation evaluation, so an annotation written with later syntax is
never evaluated there. Action plugins run only on the controller and are not
checked.

ast.parse with feature_version is best-effort. It rejects match statements,
except* and type parameter lists, but it accepts parenthesized context
managers, same-quote nested f-strings, a runtime X | None outside an
annotation and calls to library functions added after the floor. The sanity
suite of scripts/ansible-check imports each file under the real interpreter
that scripts/tools/ansible-check-floor-interpreter.json pins, which proves the
grammar and whatever runs at import, and its units suite runs the modules and
module_utils tests there, which proves the library calls those tests reach.
"""

from __future__ import annotations

import ast
import pathlib

from ansible_test._util.target.common.constants import (
    CONTROLLER_PYTHON_VERSIONS,
    REMOTE_ONLY_PYTHON_VERSIONS,
)

COLLECTION = pathlib.Path(__file__).resolve().parents[2]

# internal/controller/bundlelocal's TestTheFloorLockPinsTheCollectionsRemotePythonFloor
# holds the floor interpreter lock's minor to this value.
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


def test_the_floor_is_the_oldest_target_python_of_ansible_core():
    targets = REMOTE_ONLY_PYTHON_VERSIONS + CONTROLLER_PYTHON_VERSIONS
    oldest = min(tuple(int(part) for part in version.split(".")) for version in targets)
    assert REMOTE_PYTHON_FLOOR == oldest, "ansible-core supports targets from Python %s" % ".".join(map(str, oldest))
