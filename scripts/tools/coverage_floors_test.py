"""Tests of the coverage floors and recorder; run: python3 -I -B coverage_floors_test.py."""

import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import textwrap
import unittest


def load(name: str):
    specification = importlib.util.spec_from_file_location(
        name, Path(__file__).with_name(name + ".py")
    )
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


coverage_floors = load("coverage_floors")
coverage_recorder = load("coverage_recorder")

MODULE = "example.test/fixture"

PROFILE = textwrap.dedent(
    f"""\
    mode: set
    {MODULE}/main.go:3.10,5.2 2 1
    {MODULE}/internal/a/a.go:3.10,5.2 3 1
    {MODULE}/internal/a/a.go:7.10,9.2 1 0
    {MODULE}/internal/a/a_linux.go:3.10,5.2 4 0
    {MODULE}/internal/a/a_linux.go:3.10,5.2 4 1
    {MODULE}/internal/b/b.go:3.10,5.2 5 0
    """
)

PLUGIN = textwrap.dedent(
    '''\
    """A plugin."""
    from __future__ import annotations

    import os

    COUNT = 0


    def decorate(function):
        return function


    @decorate
    def used(value):
        """Used."""
        global COUNT
        if value:
            COUNT += 1
        try:
            return os.path.join(
                "a",
                "b",
            )
        finally:
            COUNT -= 1


    def unused():
        return 1
    '''
)


class GoPackagesTest(unittest.TestCase):
    """A package counts its blocks once, covered when any report covered them."""

    def test_packages_of_a_profile(self):
        self.assertEqual(
            coverage_floors.go_packages(PROFILE, MODULE),
            {".": (2, 2), "internal/a": (8, 7), "internal/b": (5, 0)},
        )

    def test_a_block_outside_the_module_is_refused(self):
        with self.assertRaises(SystemExit):
            coverage_floors.go_packages("other.test/x/x.go:1.1,2.2 1 1\n", MODULE)


class RecorderTest(unittest.TestCase):
    """The recorder sees the plugins' executed lines, and the spans count them."""

    def test_recorded_lines_count_the_statements_that_ran(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "plugins/action/plugin.py"
            source.parent.mkdir(parents=True)
            source.write_text(PLUGIN, encoding="utf-8")
            outside = root / "other.py"
            outside.write_text("VALUE = 1\n", encoding="utf-8")
            recorder = coverage_recorder.Recorder(str(root / "plugins"))
            recorder.start()
            try:
                namespace: dict = {}
                exec(compile(PLUGIN, str(source), "exec"), namespace)
                namespace["used"](True)
                exec(compile("VALUE = 1\n", str(outside), "exec"), {})
            finally:
                recorder.stop()
            out = root / "out"
            out.mkdir()
            recorder.write(str(out))
            seen = coverage_floors.recorded_lines(out)
            self.assertEqual(list(seen), ["action/plugin.py"])
            # Statements: import, COUNT, three defs, two returns, if, two
            # augmented assignments, try and unused's return; unused's body
            # alone never ran.
            self.assertEqual(
                coverage_floors.file_coverage(PLUGIN, seen["action/plugin.py"]),
                (12, 11),
            )
            self.assertEqual(
                coverage_floors.collection_kinds(root / "plugins", seen),
                {"action": (12, 11)},
            )

    def test_a_held_tool_identity_is_refused(self):
        first = coverage_recorder.Recorder(tempfile.gettempdir())
        first.start()
        try:
            with self.assertRaises(RuntimeError):
                coverage_recorder.Recorder(tempfile.gettempdir()).start()
        finally:
            first.stop()

    def test_no_recorded_lines_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaises(SystemExit):
                coverage_floors.recorded_lines(Path(temporary))


class FloorsTest(unittest.TestCase):
    """Each figure holds its floor, which sits within the tolerance below it."""

    def test_planted_violations_fail_and_their_refresh_passes(self):
        measured = {"exact": 805, "slack": 900, "under": 499, "new": 333, "zero": 3}
        floors = {"exact": 80.0, "slack": 88.9, "under": 50.0, "gone": 10.0, "zero": 0.0}
        self.assertEqual(
            coverage_floors.violations(measured, floors, "Go package"),
            [
                "Go package gone has a floor but was not measured; run make cover-floors",
                "Go package new measures 33.3% and has no floor; run make cover-floors",
                "Go package slack measures 90.0%, more than 1.0 points above its "
                "floor of 88.9%; run make cover-floors to raise it to 89.5%",
                "Go package under measures 49.9%, below its floor of 50.0%; "
                "restore its tests",
            ],
        )
        raised = coverage_floors.refreshed(measured, floors)
        self.assertEqual(
            raised,
            {"exact": 80.0, "new": 32.8, "slack": 89.5, "under": 50.0, "zero": 0.0},
        )
        self.assertEqual(
            coverage_floors.violations(measured, raised, "Go package"),
            [
                "Go package under measures 49.9%, below its floor of 50.0%; "
                "restore its tests"
            ],
        )

    def test_a_floor_within_the_tolerance_passes(self):
        self.assertEqual(
            coverage_floors.violations({"a": 805, "b": 900}, {"a": 79.5, "b": 89.0}, "k"),
            [],
        )

    def test_tenths_round_down(self):
        self.assertEqual(coverage_floors.tenths(3, 2), 666)
        self.assertEqual(coverage_floors.tenths(0, 0), 1000)


class MainTest(unittest.TestCase):
    """check fails on a lowered package; refresh raises floors that then hold."""

    def test_check_and_refresh(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "go.mod").write_text(f"module {MODULE}\n\ngo 1.26\n", encoding="utf-8")
            profile = root / "go.profile"
            profile.write_text(PROFILE, encoding="utf-8")
            plugins = root / coverage_floors.COLLECTION / "plugins/filter"
            plugins.mkdir(parents=True)
            (plugins / "f.py").write_text("A = 1\nB = 2\n", encoding="utf-8")
            lines = root / "lines"
            lines.mkdir()
            (lines / "lines.1.json").write_text(
                json.dumps({"filter/f.py": [1]}), encoding="utf-8"
            )
            floors = root / "floors.json"
            floors.write_text(
                json.dumps(
                    {
                        "go": {".": 99.5, "internal/a": 87.5, "internal/b": 0.0},
                        "collection": {"filter": 49.5},
                    }
                ),
                encoding="utf-8",
            )
            summary = root / "summary.json"
            arguments = [
                "--root", str(root), "--go-profile", str(profile),
                "--collection-lines", str(lines), "--floors", str(floors),
            ]
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(
                    coverage_floors.main(["check", *arguments, "--summary", str(summary)]), 0
                )
            self.assertEqual(
                json.loads(summary.read_text(encoding="utf-8"))["go"]["internal/a"],
                {"statements": 8, "covered": 7, "percent": 87.5},
            )
            lowered = PROFILE.replace("a_linux.go:3.10,5.2 4 1", "a_linux.go:3.10,5.2 4 0")
            profile.write_text(lowered, encoding="utf-8")
            errors = io.StringIO()
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(errors):
                self.assertEqual(coverage_floors.main(["check", *arguments]), 1)
            self.assertIn("internal/a measures 37.5%, below its floor of 87.5%", errors.getvalue())
            profile.write_text(PROFILE, encoding="utf-8")
            (lines / "lines.1.json").write_text(
                json.dumps({"filter/f.py": [1, 2]}), encoding="utf-8"
            )
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(coverage_floors.main(["check", *arguments]), 1)
                self.assertEqual(coverage_floors.main(["refresh", *arguments]), 0)
                self.assertEqual(coverage_floors.main(["check", *arguments]), 0)
            self.assertEqual(
                json.loads(floors.read_text(encoding="utf-8"))["collection"], {"filter": 99.5}
            )


if __name__ == "__main__":
    unittest.main()
