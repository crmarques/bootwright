"""Tests of the focused inner loop's selection; run: python3 -I -B focus_test.py."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import os
from pathlib import Path
import signal
import tempfile
import threading
import time
import unittest
from unittest import mock

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


def group_ended(group: int, within: float = 10.0) -> bool:
    """Say whether a process group has no member left, waiting up to within."""
    deadline = time.monotonic() + within
    while time.monotonic() < deadline:
        try:
            os.killpg(group, 0)
        except ProcessLookupError:
            return True
        time.sleep(0.05)
    return False


def read_pid(path: Path, within: float = 10.0) -> int:
    """Return the process ID a check wrote to path, waiting up to within."""
    deadline = time.monotonic() + within
    while time.monotonic() < deadline:
        text = path.read_text() if path.exists() else ""
        if text.endswith("\n"):
            return int(text)
        time.sleep(0.05)
    raise AssertionError(f"no process ID in {path}")


def units_root(directory: str, status: int) -> Path:
    """Return a root whose ./scripts/ansible-check prints and exits status."""
    script = Path(directory) / "scripts" / "ansible-check"
    script.parent.mkdir()
    script.write_text(f"#!/bin/sh\necho parallel failure\nexit {status}\n")
    script.chmod(0o755)
    return Path(directory)


class LaneTest(unittest.TestCase):
    def setUp(self):
        focus._stopping.clear()

    def run_steps(self, commands: list[list[str]], root: Path | None = None, **options) -> tuple[list[str], str]:
        failed: list[str] = []
        output = io.StringIO()
        steps = [("lane", str(index), command) for index, command in enumerate(commands)]
        with contextlib.redirect_stdout(output):
            focus.run_lane(root or Path.cwd(), steps, threading.Lock(), failed, **options)
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

    def test_a_check_that_passed_only_on_the_serial_rerun_is_flaky_not_a_pass(self):
        flaky: list[str] = []
        with tempfile.TemporaryDirectory() as directory:
            failed, output = self.run_steps(
                [focus.UNITS + ["tests/unit"], ["true"]],
                root=units_root(directory, focus.FLAKY_STATUS),
                flaky=flaky,
            )
        self.assertEqual(failed, [])
        self.assertEqual(flaky, ["lane 0"])
        self.assertIn("parallel failure", output)
        self.assertIn("lane] 0 FLAKY", output)
        self.assertIn("rerun: ./scripts/ansible-check --suite units tests/unit", output)
        self.assertNotIn("0 passed", output)
        self.assertNotIn("lane] 1", output)

    def test_another_check_exiting_with_the_flaky_status_fails(self):
        flaky: list[str] = []
        failed, output = self.run_steps([["sh", "-c", f"exit {focus.FLAKY_STATUS}"]], flaky=flaky)
        self.assertEqual(failed, ["lane 0"])
        self.assertEqual(flaky, [])
        self.assertIn("lane] 0 FAILED", output)

    def test_a_descendant_that_leaves_the_group_cannot_hold_the_lane(self):
        with tempfile.TemporaryDirectory() as directory:
            pid_file = Path(directory) / "pid"
            # The setsid child leaves the check's group and keeps its output open.
            command = ["sh", "-c", f"setsid sh -c 'echo $$ > {pid_file}; exec sleep 60' & sleep 60"]
            started = time.monotonic()
            with mock.patch.object(focus, "DRAIN_TIMEOUT", 0.5):
                failed, output = self.run_steps([command], timeout=1.0)
            escaped = read_pid(pid_file)
            os.kill(escaped, signal.SIGKILL)
        self.assertLess(time.monotonic() - started, 15)
        self.assertEqual(failed, ["lane 0 timed out"])
        self.assertIn("holds its output open; stopped reading", output)

    def test_stopping_kills_the_running_check_and_starts_no_other(self):
        with tempfile.TemporaryDirectory() as directory:
            pid_file = Path(directory) / "pid"
            command = ["sh", "-c", f"echo $$ > {pid_file}; sleep 60 & sleep 60"]
            result: list[tuple[list[str], str]] = []
            lane = threading.Thread(target=lambda: result.append(self.run_steps([command, ["true"]])))
            lane.start()
            group = read_pid(pid_file)
            focus.stop_checks()
            lane.join(15)
        self.assertFalse(lane.is_alive())
        self.assertTrue(group_ended(group))
        failed, output = result[0]
        self.assertEqual(failed, ["lane 0"])
        self.assertNotIn("lane] 1", output)

    def test_an_interrupt_kills_the_check_before_it_propagates(self):
        with tempfile.TemporaryDirectory() as directory:
            pid_file = Path(directory) / "pid"
            command = ["sh", "-c", f"echo $$ > {pid_file}; sleep 60 & sleep 60"]
            def interrupted(process, *arguments, **options):
                read_pid(pid_file)
                raise KeyboardInterrupt

            started = time.monotonic()
            with mock.patch.object(focus.subprocess.Popen, "communicate", interrupted):
                with self.assertRaises(KeyboardInterrupt):
                    focus.run_command(Path.cwd(), command, 30.0)
            self.assertLess(time.monotonic() - started, 15)
            self.assertTrue(group_ended(read_pid(pid_file)))
            self.assertEqual(focus._running, set())

    def test_a_lane_that_outlasts_its_timeout_is_killed_and_names_its_command(self):
        started = time.monotonic()
        # The background sleep holds the output open beyond its shell.
        command = ["sh", "-c", "sleep 30 & sleep 30"]
        failed, output = self.run_steps([["true"], command, ["true"]], timeout=1.0)
        self.assertLess(time.monotonic() - started, 15)
        self.assertEqual(failed, ["lane 1 timed out"])
        self.assertIn("lane] 1 TIMED OUT", output)
        self.assertIn("rerun: sh -c 'sleep 30 & sleep 30'", output)
        self.assertNotIn("lane] 2", output)

    def test_the_timeout_bounds_the_whole_lane_not_each_check(self):
        failed, output = self.run_steps(
            [["sleep", "0.7"], ["sleep", "0.7"], ["true"]], timeout=1.0
        )
        self.assertEqual(failed, ["lane 1 timed out"])
        self.assertIn("lane] 0 passed", output)


class MainTest(unittest.TestCase):
    def run_main(self, command: list[str]) -> tuple[int, str]:
        output = io.StringIO()
        steps = [("text", "check", command)]
        with (
            mock.patch.object(focus, "changed_files", return_value=("base", [])),
            mock.patch.object(focus, "plan", return_value=steps),
            mock.patch.object(focus.sys, "argv", ["focus.py"]),
            contextlib.redirect_stdout(output),
        ):
            return focus.main(), output.getvalue()

    def test_a_flaky_check_exits_with_its_own_status(self):
        # The units step is the one that reports FLAKY; this command stands in for it.
        with mock.patch.object(focus, "UNITS", ["sh", "-c"]):
            status, output = self.run_main(["sh", "-c", f"exit {focus.FLAKY_STATUS}"])
        self.assertEqual(status, focus.FLAKY_STATUS)
        self.assertIn("focus: FLAKY", output)
        self.assertNotIn("focus: passed", output)

    def test_termination_kills_every_running_check(self):
        with tempfile.TemporaryDirectory() as directory:
            pid_file = Path(directory) / "pid"
            command = ["sh", "-c", f"echo $$ > {pid_file}; sleep 60 & sleep 60"]
            groups: list[int] = []

            def terminate() -> None:
                groups.append(read_pid(pid_file))
                os.kill(os.getpid(), signal.SIGTERM)

            timer = threading.Timer(0.2, terminate)
            timer.start()
            started = time.monotonic()
            status, output = self.run_main(command)
            timer.join()
        self.assertEqual(status, 130)
        self.assertLess(time.monotonic() - started, 20)
        self.assertIn("focus: interrupted", output)
        self.assertTrue(group_ended(groups[0]))
        self.assertIs(signal.getsignal(signal.SIGTERM), signal.SIG_DFL)

    def test_a_failed_check_exits_one_and_a_passing_one_zero(self):
        self.assertEqual(self.run_main(["false"])[0], 1)
        self.assertEqual(self.run_main(["true"])[0], 0)


if __name__ == "__main__":
    unittest.main()
