"""Tests of the focused inner loop's selection; run: python3 -I -B focus_test.py."""

from __future__ import annotations

import contextlib
import importlib.util
import io
from pathlib import Path
import tempfile
import threading
import unittest

_specification = importlib.util.spec_from_file_location(
    "focus", Path(__file__).with_name("focus.py")
)
focus = importlib.util.module_from_spec(_specification)
_specification.loader.exec_module(focus)

PACKAGE = "ansible_collections.bootwright.core"

# A collection whose tests reach its plugins directly, through another
# plugin, by naming one, or not at all.
COLLECTION = {
    "plugins/module_utils/base.py": "",
    "plugins/module_utils/middle.py": f"from {PACKAGE}.plugins.module_utils import base\n",
    "plugins/modules/runner.py": f"from {PACKAGE}.plugins.module_utils.middle import run\n",
    "plugins/modules/named.py": "",
    "tests/unit/conftest.py": "",
    "tests/unit/plugins/module_utils/test_base.py": f"import {PACKAGE}.plugins.module_utils.base\n",
    "tests/unit/plugins/modules/test_runner.py": f"from {PACKAGE}.plugins.modules import runner\n",
    "tests/unit/test_by_name.py": "PLAYBOOK = 'bootwright.core.named'\n",
    "tests/unit/test_unrelated.py": "import os\n",
    "tests/unit/helper.py": f"from {PACKAGE}.plugins.module_utils import base\n",
    "tests/unit/test_every_plugin.py": "PLUGINS = ROOT / 'plugins'\nFILES = PLUGINS.rglob('*.py')\n",
    "tests/unit/test_temporary_listing.py": "FILES = TEMPORARY.glob('*')\n",
}


class SelectionTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.collection = Path(temporary.name)
        for relative, source in COLLECTION.items():
            path = self.collection / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(source, encoding="utf-8")

    def test_a_changed_plugin_selects_the_tests_that_import_it_at_any_depth(self):
        self.assertEqual(
            focus.unit_tests(self.collection, ["plugins/module_utils/base.py"]),
            [
                "tests/unit/plugins/module_utils/test_base.py",
                "tests/unit/plugins/modules/test_runner.py",
                "tests/unit/test_every_plugin.py",
            ],
        )

    def test_a_plugin_a_test_names_selects_that_test(self):
        self.assertEqual(
            focus.unit_tests(self.collection, ["plugins/modules/named.py"]),
            ["tests/unit/test_by_name.py", "tests/unit/test_every_plugin.py"],
        )

    def test_a_changed_test_selects_itself(self):
        self.assertEqual(
            focus.unit_tests(self.collection, ["tests/unit/test_unrelated.py"]),
            ["tests/unit/test_unrelated.py"],
        )

    def test_a_deleted_plugin_selects_the_tests_that_still_import_it(self):
        (self.collection / "plugins/module_utils/base.py").unlink()
        self.assertIn(
            "tests/unit/plugins/module_utils/test_base.py",
            focus.unit_tests(self.collection, ["plugins/module_utils/base.py"]),
        )

    def test_a_file_no_import_reaches_selects_the_whole_tree(self):
        for relative in (
            "roles/storage/tasks/apply.yml",
            "playbooks/storage/apply.yml",
            "tests/unit/conftest.py",
            "tests/unit/fixtures/answer.json",
            "meta/runtime.yml",
        ):
            with self.subTest(relative=relative):
                self.assertIsNone(focus.unit_tests(self.collection, [relative]))

    def test_the_collection_readme_selects_nothing(self):
        self.assertEqual(focus.unit_tests(self.collection, ["README.md"]), [])


class ImportTest(unittest.TestCase):
    def test_module_names_follow_the_collection_layout(self):
        self.assertEqual(
            focus.module_name("plugins/module_utils/base.py"),
            f"{PACKAGE}.plugins.module_utils.base",
        )
        self.assertEqual(
            focus.module_name("plugins/module_utils/__init__.py"),
            f"{PACKAGE}.plugins.module_utils",
        )

    def test_only_absolute_collection_imports_are_names(self):
        source = (
            "import os\n"
            f"import {PACKAGE}.plugins.module_utils.a\n"
            f"from {PACKAGE}.plugins.module_utils import b, c\n"
            "from . import relative\n"
        )
        self.assertEqual(
            focus.imported_names(source),
            {
                f"{PACKAGE}.plugins.module_utils.a",
                f"{PACKAGE}.plugins.module_utils",
                f"{PACKAGE}.plugins.module_utils.b",
                f"{PACKAGE}.plugins.module_utils.c",
            },
        )

    def test_a_source_that_does_not_parse_imports_nothing(self):
        self.assertEqual(focus.imported_names("def broken(:\n"), set())


class PlanTest(unittest.TestCase):
    def names(self, changed: list[str]) -> list[str]:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in changed:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("", encoding="utf-8")
            return [lane + " " + name for lane, name, _ in focus.plan(root, "base", changed)]

    def test_a_deleted_go_file_still_vets_its_dependents(self):
        with tempfile.TemporaryDirectory() as temporary:
            names = [name for _, name, _ in focus.plan(Path(temporary), "base", ["internal/a/gone.go"])]
        self.assertEqual(names, ["whitespace", "vet of every affected package", "test of changed packages"])

    def test_an_ansible_input_outside_the_collection_runs_every_ansible_check(self):
        for path in focus.ANSIBLE_INPUTS:
            with self.subTest(path=path):
                self.assertEqual(
                    self.names([path])[-4:],
                    [
                        "ansible lint, unnarrowed",
                        "ansible syntax, unnarrowed",
                        "ansible sanity, unnarrowed",
                        "units whole tree",
                    ],
                )

    def test_a_go_change_formats_vets_and_tests(self):
        self.assertEqual(
            self.names(["internal/a/a.go"]),
            ["text whitespace", "go gofmt", "go vet of every affected package", "go test of changed packages"],
        )

    def test_guidance_runs_docs_check_without_vet(self):
        self.assertEqual(
            self.names([".agents/knowledge/x.md", "specs/y.md"]),
            ["text whitespace", "text docs-check", "go test of changed packages"],
        )

    def test_a_module_file_vets_although_no_go_file_changed(self):
        self.assertIn("go vet of every affected package", self.names(["go.mod"]))

    def test_a_role_lints_its_files_and_runs_every_unit_test_then_sanity(self):
        role = focus.COLLECTION + "/roles/storage/tasks/apply.yml"
        self.assertEqual(
            self.names([role]),
            [
                "text whitespace",
                "go test of changed packages",
                "ansible lint",
                "ansible sanity",
                "units whole tree",
            ],
        )

    def test_a_playbook_is_also_syntax_checked(self):
        playbook = focus.COLLECTION + "/playbooks/storage/apply.yml"
        self.assertIn("ansible syntax", self.names([playbook]))

    def test_the_harness_runs_its_own_tests(self):
        self.assertIn("units harness tests", self.names(["scripts/tools/ansible_check.py"]))


class LaneTest(unittest.TestCase):
    def run_steps(self, commands: list[list[str]]) -> tuple[list[str], str]:
        failed: list[str] = []
        output = io.StringIO()
        steps = [("lane", str(index), command) for index, command in enumerate(commands)]
        with contextlib.redirect_stdout(output):
            focus.run_lane(Path.cwd(), steps, threading.Lock(), failed)
        return failed, output.getvalue()

    def test_a_failure_ends_the_lane_and_names_its_command(self):
        failed, output = self.run_steps([["false"], ["true"]])
        self.assertEqual(failed, ["lane 0"])
        self.assertIn("rerun: false", output)
        self.assertNotIn("lane] 1", output)

    def test_output_that_is_not_utf8_still_fails_the_step(self):
        failed, output = self.run_steps([["sh", "-c", "printf '\\377'; exit 1"]])
        self.assertEqual(failed, ["lane 0"])
        self.assertIn("\ufffd", output)

    def test_a_command_that_cannot_start_fails_the_step(self):
        failed, _ = self.run_steps([["/nonexistent/focus-command"]])
        self.assertEqual(failed, ["lane 0"])


if __name__ == "__main__":
    unittest.main()
