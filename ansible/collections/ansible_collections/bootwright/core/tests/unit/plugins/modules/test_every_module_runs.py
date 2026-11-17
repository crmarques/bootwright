"""Every module's main() is run by a unit test with its inputs stubbed.

Each action plugin has a stubbed run in test_every_action_runs.py. A module is
what the task finally runs, and its main() reads the task's arguments, reaches
the helpers and turns every refusal into a failed result rather than an
exception. A helper renamed, or an argument read by the wrong key, passes every
helper test and shows up only on a host. A module with a main() therefore needs
a test file that imports it and calls a main(), and a module without one is
documentation for the action plugin that runs in its place. A new module fails
here until it arrives with its stubbed run.
"""

from __future__ import annotations

import ast
import pathlib

CORE = pathlib.Path(__file__).resolve().parents[4]
MODULES = CORE / "plugins" / "modules"
TESTS = CORE / "tests" / "unit"
PACKAGE = "ansible_collections.bootwright.core.plugins.modules"


def defines_main(path):
    return any(isinstance(node, ast.FunctionDef) and node.name == "main" for node in ast.parse(path.read_text(encoding="utf-8")).body)


def imported_modules(tree):
    """The collection modules a test file imports, by the name each is bound to."""
    found = {}
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom) and node.module == PACKAGE:
            found.update({(alias.asname or alias.name): alias.name for alias in node.names})
        elif isinstance(node, ast.Import):
            found.update({alias.asname: alias.name[len(PACKAGE) + 1:] for alias in node.names
                          if alias.name.startswith(PACKAGE + ".") and alias.asname})
    return found


def parameters(tree):
    return {argument.arg for node in ast.walk(tree) if isinstance(node, ast.FunctionDef)
            for argument in node.args.args + node.args.kwonlyargs}


def runs(tree):
    """Whether the file calls main() on a module it imports, hands one to the run_module fixture, or calls main() on a parameter."""
    bound, passed = imported_modules(tree), parameters(tree)
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if isinstance(node.func, ast.Attribute) and node.func.attr == "main" and isinstance(node.func.value, ast.Name):
            if node.func.value.id in bound or node.func.value.id in passed:
                return True
        if isinstance(node.func, ast.Name) and node.func.id == "run_module" and node.args:
            return True
    return False


def stub_run_tests():
    """Each module name mapped to the test files that import it and run a main()."""
    found = {}
    for path in sorted(TESTS.rglob("test_*.py")):
        tree = ast.parse(path.read_text(encoding="utf-8"))
        if path.name != pathlib.Path(__file__).name and runs(tree):
            for name in set(imported_modules(tree).values()):
                found.setdefault(name, []).append(path.relative_to(TESTS).as_posix())
    return found


def test_every_module_with_a_main_has_a_test_that_runs_it():
    with_main = sorted(path.stem for path in MODULES.glob("*.py") if defines_main(path))
    assert len(with_main) > 40
    tested = stub_run_tests()
    missing = [name for name in with_main if name not in tested]
    assert not missing, "modules with no unit test that runs their main(): " + ", ".join(missing)


def test_a_module_without_a_main_is_documentation_for_its_action_plugin():
    actions = {path.stem for path in (CORE / "plugins" / "action").glob("*.py")}
    without_main = sorted(path.stem for path in MODULES.glob("*.py") if path.stem != "__init__" and not defines_main(path))
    assert without_main
    unpaired = [name for name in without_main if name not in actions]
    assert not unpaired, "modules with neither a main() nor an action plugin: " + ", ".join(unpaired)
