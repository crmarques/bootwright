"""Tests of the Ansible check harness, which its units suite runs."""

import contextlib
import importlib.util
import io
import os
from pathlib import Path
import string
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock
import warnings

from jinja2.nativetypes import NativeEnvironment

_specification = importlib.util.spec_from_file_location(
    "ansible_check", Path(__file__).with_name("ansible_check.py")
)
ansible_check = importlib.util.module_from_spec(_specification)
_specification.loader.exec_module(ansible_check)

# The characters tempfile draws the eight of a name's random part from.
RANDOM_CHARACTERS = string.ascii_lowercase + string.digits + "_"

# CPython's tokenizer warns, rather than refuses, when a number runs into
# "if", "in" or "is", or into one of the others where no name character
# follows it.
KEYWORDS = ("and", "else", "for", "if", "in", "is", "not", "or")


def syntax_warnings(area: Path) -> list[str]:
    """Render an ansible-core default whose home lies in the area.

    With HOME in the area, ansible-core renders each ANSIBLE_HOME default,
    such as '{{ ANSIBLE_HOME ~ "/tmp" }}', in Jinja's native environment,
    which parses the rendered path as a Python literal and keeps the text
    when it does not parse. This returns the warnings that parse printed.
    """
    template = NativeEnvironment().from_string('{{ ANSIBLE_HOME ~ "/tmp" }}')
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        rendered = template.render(ANSIBLE_HOME=str(area / ".ansible"))
    if rendered != str(area / ".ansible/tmp"):
        raise AssertionError(f"rendered {rendered!r}")
    return [
        str(warning.message)
        for warning in caught
        if issubclass(warning.category, SyntaxWarning)
    ]


def ending_keyword(character: str, keyword: str) -> str:
    """Return a random part that repeats the character up to the keyword."""
    return keyword.rjust(8, character)


def random_parts() -> list[str]:
    """Return random parts that start with each character, then a keyword."""
    parts = []
    for character in RANDOM_CHARACTERS:
        for keyword in KEYWORDS:
            parts.append((character + keyword).ljust(8, "x"))
            parts.append(ending_keyword(character, keyword))
    return parts


class CheckAreaTest(unittest.TestCase):
    def test_the_gate_runs_in_an_area_named_with_the_prefix(self):
        # The gate names the area it made and gave its processes as HOME; a
        # by-hand run of this file has none.
        named = os.environ.get(ansible_check.AREA_VARIABLE)
        if named is None:
            self.skipTest(f"{ansible_check.AREA_VARIABLE} names no area")
        area = Path(named)
        self.assertEqual(os.environ.get("HOME"), named)
        self.assertEqual(os.environ.get("TMPDIR"), named)
        self.assertTrue(area.is_dir())
        self.assertTrue(area.name.startswith(ansible_check.AREA_PREFIX), area.name)
        self.assertEqual(syntax_warnings(area), [])

    def test_the_check_area_is_named_with_the_prefix(self):
        with ansible_check.check_area() as temporary:
            area = Path(temporary)
            self.assertTrue(area.is_dir())
            self.assertTrue(area.name.startswith(ansible_check.AREA_PREFIX))
            self.assertEqual(syntax_warnings(area), [])

    def test_no_random_part_joins_the_prefix_into_a_number(self):
        for part in random_parts():
            area = Path("/tmp") / (ansible_check.AREA_PREFIX + part)
            with self.subTest(area=str(area)):
                self.assertEqual(syntax_warnings(area), [])

    def test_a_hyphen_before_the_random_part_warns(self):
        # The prefix the harness once used: a digit after its hyphen starts
        # a number, and each of these parts ran it into a keyword.
        for character in string.digits:
            for keyword in KEYWORDS:
                part = ending_keyword(character, keyword)
                area = Path("/tmp") / ("bootwright-ansible-check-" + part)
                with self.subTest(area=str(area)):
                    self.assertNotEqual(syntax_warnings(area), [])


class SelectionTest(unittest.TestCase):
    def test_no_argument_selects_every_suite_unnarrowed(self):
        self.assertEqual(
            ansible_check.selected_suites([]), (set(ansible_check.SUITES), [])
        )

    def test_a_suite_runs_alone_or_narrowed_to_paths(self):
        self.assertEqual(
            ansible_check.selected_suites(["--suite", "integration"]),
            ({"integration"}, []),
        )
        for suite in ansible_check.NARROWED:
            with self.subTest(suite=suite):
                self.assertEqual(
                    ansible_check.selected_suites(["--suite", suite, "a", "b"]),
                    ({suite}, ["a", "b"]),
                )

    def test_integration_and_unknown_forms_refuse(self):
        for arguments in (
            ["--suite", "integration", "a"],
            ["--suite", "everything"],
            ["--suite"],
            ["a"],
        ):
            with self.subTest(arguments=arguments):
                with self.assertRaises(SystemExit):
                    ansible_check.selected_suites(arguments)

    def test_targets_are_named_from_the_collection_and_stay_inside_it(self):
        with ansible_check.check_area() as temporary:
            collection = Path(temporary) / "collection"
            (collection / "tests/unit").mkdir(parents=True)
            (collection / "tests/unit/test_a.py").write_text("", encoding="utf-8")
            outside = Path(temporary) / "outside.py"
            outside.write_text("", encoding="utf-8")
            self.assertEqual(
                ansible_check.collection_targets(
                    collection, [str(collection / "tests/unit/test_a.py")]
                ),
                ["tests/unit/test_a.py"],
            )
            for path in (outside, collection / "missing.py", collection / ".."):
                with self.subTest(path=str(path)):
                    with self.assertRaises(SystemExit):
                        ansible_check.collection_targets(collection, [str(path)])


class NarrowedUnitsTest(unittest.TestCase):
    """A failure only the serial rerun clears is flaky, never a pass."""

    def narrowed(self, *statuses: int) -> tuple[int, list[list[str]]]:
        """Run narrowed_units over two files with each run exiting as given."""
        calls: list[list[str]] = []
        results = iter(statuses)

        def run(command, **_):
            calls.append(command)
            return subprocess.CompletedProcess(command, next(results))

        with ansible_check.check_area() as temporary:
            collection = Path(temporary) / "a" / "b" / "c"
            for name in ("one", "two"):
                (collection / name).mkdir(parents=True)
            output = io.StringIO()
            with (
                mock.patch.object(ansible_check.subprocess, "run", run),
                contextlib.redirect_stdout(output),
            ):
                status = ansible_check.narrowed_units(
                    ["pytest"], collection, ["one", "two"], {}
                )
        self.output = output.getvalue()
        return status, calls

    def test_a_parallel_pass_or_a_confirmed_failure_exits_as_the_run_did(self):
        self.assertEqual(
            self.narrowed(0), (0, [["pytest", "-n", "auto", "a/b/c/one", "a/b/c/two"]])
        )
        status, calls = self.narrowed(1, 1)
        self.assertEqual((status, len(calls)), (1, 2))
        self.assertNotIn("FLAKY", self.output)

    def test_a_failure_that_passes_on_the_serial_rerun_exits_flaky(self):
        status, calls = self.narrowed(1, 0)
        self.assertEqual(status, ansible_check.FLAKY_STATUS)
        self.assertEqual(len(calls), 2)
        self.assertIn("--last-failed", calls[1])
        self.assertIn("FLAKY", self.output)


class UnitsWorkersTest(unittest.TestCase):
    """The gate's ansible-test unit runs name pytest-xdist auto explicitly."""

    def pytest_workers(self, arguments: list[str]) -> str:
        """Return the -n value of the pytest command ansible-test generates."""
        executable = Path(sys.executable)
        with tempfile.TemporaryDirectory() as temporary:
            content = Path(temporary) / "ansible_collections/ns/col"
            (content / "tests/unit").mkdir(parents=True)
            (content / "tests/unit/test_a.py").write_text("def test_a(): pass\n")
            completed = subprocess.run(
                [
                    str(executable),
                    "-I",
                    str(executable.parent / "ansible-test"),
                    *arguments,
                    "--explain",
                    "--local",
                    "--python",
                    "3.13",
                    "--color",
                    "no",
                ],
                cwd=content,
                env={**os.environ, "ANSIBLE_TEST_CONTENT_ROOT": str(content)},
                capture_output=True,
                text=True,
                check=True,
            )
        commands = [
            line.split()
            for line in completed.stdout.splitlines()
            if line.startswith("Stream command: pytest ")
        ]
        self.assertEqual(len(commands), 1, completed.stdout)
        return commands[0][commands[0].index("-n") + 1]

    def test_the_gate_units_arguments_make_pytest_auto(self):
        arguments = ansible_check.units_arguments()
        self.assertEqual(arguments[0], "units")
        self.assertEqual(self.pytest_workers(arguments), "auto")

    def test_the_arguments_keep_the_floor_options_after_the_mode(self):
        arguments = ansible_check.units_arguments("--color", "no")
        self.assertEqual(
            arguments,
            [
                "units",
                "--num-workers",
                ansible_check.UNITS_WORKERS_AUTO,
                "--color",
                "no",
            ],
        )

    def test_one_worker_is_not_auto(self):
        arguments = ["units", "--num-workers", "1"]
        self.assertEqual(self.pytest_workers(arguments), "1")


class LanesTest(unittest.TestCase):
    """Lanes run beside one another, and the first that failed decides."""

    def lanes(self, *lanes) -> int:
        """Run the named lanes and keep what the run printed."""
        output = io.StringIO()
        with ansible_check.check_area() as temporary, contextlib.redirect_stdout(
            output
        ):
            status = ansible_check.run_lanes(list(lanes), Path(temporary) / "logs")
        self.output = output.getvalue()
        return status

    def test_lanes_run_beside_one_another(self):
        barrier = threading.Barrier(2, timeout=10)

        def meet(output):
            barrier.wait()

        self.assertEqual(self.lanes(("a", meet), ("b", meet)), 0, self.output)

    def test_the_first_failed_lane_decides_and_the_others_still_finish(self):
        finished = threading.Event()

        def fail(output):
            raise subprocess.CalledProcessError(3, ["check"])

        def finish(output):
            time.sleep(0.2)
            finished.set()

        self.assertEqual(self.lanes(("a", fail), ("b", finish)), 3)
        self.assertTrue(finished.is_set())
        self.assertIn("lane a: FAILED with status 3", self.output)
        self.assertIn("lane b: passed", self.output)

    def test_several_lanes_never_report_flaky(self):
        def flaky(output):
            return ansible_check.FLAKY_STATUS

        self.assertEqual(self.lanes(("a", flaky), ("b", lambda output: None)), 1)

    def test_one_lane_owns_the_terminal_and_its_status(self):
        given = []

        def flaky(output):
            given.append(output)
            return ansible_check.FLAKY_STATUS

        self.assertEqual(self.lanes(("a", flaky)), ansible_check.FLAKY_STATUS)
        self.assertEqual(given, [None])

    def test_each_lane_output_is_printed_whole_with_its_refusal(self):
        def write(output):
            output.write("one\ntwo\n")
            raise SystemExit("the reason")

        self.assertEqual(self.lanes(("a", write), ("b", lambda output: None)), 1)
        self.assertIn(
            "===== lane a =====\none\ntwo\nthe reason\n===== end of lane a =====",
            self.output,
        )

    def test_every_kind_of_failure_fails_its_lane(self):
        def code(output):
            raise SystemExit(4)

        def killed(output):
            raise subprocess.CalledProcessError(-9, ["check"])

        def broken(output):
            raise RuntimeError("unexpected")

        self.assertEqual(self.lanes(("a", code), ("b", lambda output: None)), 4)
        self.assertEqual(self.lanes(("a", killed), ("b", lambda output: None)), -9)
        self.assertEqual(self.lanes(("a", broken), ("b", lambda output: None)), 1)
        self.assertIn("RuntimeError: unexpected", self.output)

    def test_collection_lanes_write_apart(self):
        with ansible_check.check_area() as temporary:
            area = Path(temporary)
            collection = area / "source"
            for name in ("tests/unit/__pycache__", "tests/output", "roles"):
                (collection / name).mkdir(parents=True)
            (collection / "roles/main.yml").write_text("[]\n", encoding="utf-8")
            wheelhouse = area / "wheels"
            wheelhouse.mkdir()
            (wheelhouse / "get_pip_25_2.py").write_text("", encoding="utf-8")
            homes = set()
            for name in ("one", "two"):
                copy, environment = ansible_check.collection_lane(
                    area, name, collection, wheelhouse, {"HOME": str(area)}
                )
                lane = area / name
                self.assertTrue((copy / "roles/main.yml").is_file())
                self.assertFalse((copy / "tests/unit/__pycache__").exists())
                self.assertFalse((copy / "tests/output").exists())
                for variable in (
                    "HOME",
                    "TMPDIR",
                    "XDG_CACHE_HOME",
                    "ANSIBLE_LOCAL_TEMP",
                    "ANSIBLE_REMOTE_TEMP",
                    "ANSIBLE_COLLECTIONS_PATH",
                    "ANSIBLE_TEST_CONTENT_ROOT",
                ):
                    with self.subTest(lane=name, variable=variable):
                        self.assertTrue(
                            Path(environment[variable]).is_relative_to(lane)
                        )
                homes.add(environment["HOME"])
            self.assertEqual(len(homes), 2)

    def names(self, arguments: list[str], targets: list[str]) -> list[str]:
        """Return the lanes check_lanes builds for a command line."""
        selected, _ = ansible_check.selected_suites(arguments)
        run = ansible_check.Run(
            Path("/root"),
            Path("/venv/bin/python"),
            Path("/root/ansible"),
            Path("/root/ansible/collection"),
            Path("/fixtures"),
            Path("/area"),
            {},
            selected,
            targets,
        )
        with (
            mock.patch.object(
                ansible_check, "prepared_wheelhouse", lambda run_: Path("/w")
            ),
            mock.patch.object(
                ansible_check,
                "floor_interpreter",
                lambda *_: ("3.9", Path("/f/python")),
            ),
        ):
            lanes = ansible_check.check_lanes(run, [], Path("/area/inventory.json"))
        return [name for name, _ in lanes]

    def test_the_gate_runs_every_lane_and_a_narrowed_run_one(self):
        self.assertEqual(
            self.names([], []),
            ["syntax", "lint", "sanity", "units", "integration", "plain"],
        )
        self.assertEqual(self.names(["--suite", "units"], []), ["units", "plain"])
        self.assertEqual(self.names(["--suite", "units", "a"], ["a"]), ["plain"])
        self.assertEqual(self.names(["--suite", "sanity", "a"], ["a"]), ["sanity"])
        self.assertEqual(self.names(["--suite", "lint"], []), ["lint"])

    def commands(self, lane_of):
        """Run one lane with every effect replaced and return what it ran."""
        ran = []
        copy = Path("/area/lane/collections/ansible_collections/bootwright/core")

        def run(command, output, **options):
            ran.append((command[3:], options["cwd"], options["env"]["PATH"]))

        run_ = ansible_check.Run(
            Path("/root"),
            Path("/venv/bin/python"),
            Path("/root/ansible"),
            Path("/root/ansible/collection"),
            Path("/fixtures"),
            Path("/area"),
            {"PATH": "base"},
            set(ansible_check.SUITES),
            [],
        )
        with (
            mock.patch.object(ansible_check, "run", run),
            mock.patch.object(
                ansible_check,
                "collection_lane",
                lambda *_: (copy, {"PATH": "lane"}),
            ),
            mock.patch.object(
                ansible_check,
                "floor_interpreter",
                lambda *_: ("3.9", Path("/f/python")),
            ),
            mock.patch.object(
                ansible_check,
                "require_imported",
                lambda collection, minor: ran.append(("imported", collection, minor)),
            ),
            mock.patch.object(
                ansible_check,
                "require_unit_tested",
                lambda collection, minor: ran.append(
                    ("unit tested", collection, minor)
                ),
            ),
        ):
            lane_of(run_, Path("/w"))(None)
        return ran, copy

    def test_each_lane_runs_what_the_serial_gate_ran(self):
        floor_path = os.pathsep.join(["/venv/bin", "/f", "/usr/bin", "/bin"])
        local = ["--local", "--python", "3.13", "--color", "no"]
        ran, copy = self.commands(ansible_check.sanity_lane)
        self.assertEqual(
            ran,
            [
                (["sanity"] + local, copy, "lane"),
                (
                    [
                        "sanity",
                        "--test",
                        "import",
                        "--junit",
                        "--controller",
                        "origin:python=3.13",
                        "--target-python",
                        "3.9@/f/python",
                        "--color",
                        "no",
                    ],
                    copy,
                    floor_path,
                ),
                ("imported", copy, "3.9"),
            ],
        )
        ran, copy = self.commands(ansible_check.units_lane)
        self.assertEqual(
            ran,
            [
                (["units", "--num-workers", "0"] + local, copy, "lane"),
                (
                    [
                        "units",
                        "--num-workers",
                        "0",
                        "--controller",
                        "origin:python=3.13",
                        "--target-python",
                        "venv/3.9@/f/python",
                        "--color",
                        "no",
                    ],
                    copy,
                    floor_path,
                ),
                ("unit tested", copy, "3.9"),
            ],
        )
        ran, copy = self.commands(ansible_check.integration_lane)
        self.assertEqual(
            ran,
            [
                (["integration", "controller_tool"] + local, copy, "lane"),
                (["integration", "controller_supervisor"] + local, copy, "lane"),
            ],
        )

    def test_the_plain_lane_runs_one_serial_pytest_in_process(self):
        commands = []

        def run(command, **_):
            commands.append(command)
            return subprocess.CompletedProcess(command, 0)

        with ansible_check.check_area() as temporary:
            area = Path(temporary)
            collection = area / "source"
            (collection / "tests/unit").mkdir(parents=True)
            wheelhouse = area / "wheels"
            wheelhouse.mkdir()
            (wheelhouse / "get_pip_25_2.py").write_text("", encoding="utf-8")
            run_ = ansible_check.Run(
                area,
                Path("/venv/bin/python"),
                area,
                collection,
                area,
                area,
                {},
                {"units"},
                [],
            )
            with mock.patch.object(ansible_check.subprocess, "run", run):
                lane = ansible_check.plain_lane(run_, wheelhouse)
                self.assertIsNone(lane(None))
        self.assertEqual(len(commands), 1)
        self.assertEqual(commands[0][1:5], ["-P", "-s", "-m", "pytest"])
        self.assertNotIn("-n", commands[0])
        self.assertTrue(commands[0][-1].endswith("tests/unit"))


class PassKeyTest(unittest.TestCase):
    """Every input a complete run reads moves its key."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        for name, text in {
            "ansible/ansible.cfg": "[defaults]\n",
            "ansible/collections/roles/main.yml": "[]\n",
            "scripts/ansible-check": "#!/bin/sh\n",
            "scripts/cache-dir": "#!/bin/sh\n",
            "scripts/tools/ansible_check.py": "",
            "scripts/tools/ansible-requirements.txt": "a==1\n",
            "scripts/tools/focus.py": "",
        }.items():
            (self.root / name).parent.mkdir(parents=True, exist_ok=True)
            (self.root / name).write_text(text, encoding="utf-8")
        self.addCleanup(self.temporary.cleanup)

    def key(
        self, host=("host /usr/bin/a 1 1",), installed=("a==1",), interpreter="3.13.16"
    ):
        return ansible_check.pass_key(
            self.root, list(host), list(installed), interpreter
        )

    def test_the_key_moves_with_every_input(self):
        changes = {
            "a file's bytes": lambda: (self.root / "ansible/ansible.cfg").write_text(
                "x"
            ),
            "a file's mode": lambda: (self.root / "ansible/ansible.cfg").chmod(0o755),
            "a new file": lambda: (self.root / "ansible/new.yml").write_text(""),
            "an empty directory": lambda: (self.root / "ansible/empty").mkdir(),
            "a link": lambda: (self.root / "ansible/link").symlink_to("ansible.cfg"),
            "the launcher": lambda: (self.root / "scripts/ansible-check").write_text(
                "x"
            ),
            "the cache directory script": lambda: (
                self.root / "scripts/cache-dir"
            ).write_text("x"),
            "a root lint file": lambda: (self.root / ".ansible-lint").write_text(""),
            "the root ignore rules": lambda: (self.root / ".gitignore").write_text("x"),
            "a lock": lambda: (
                self.root / "scripts/tools/ansible-requirements.txt"
            ).write_text("a==2\n"),
            "a new harness file": lambda: (
                self.root / "scripts/tools/ansible_new.py"
            ).write_text(""),
        }
        for name, change in changes.items():
            with self.subTest(change=name):
                self.setUp()
                before = self.key()
                change()
                self.assertNotEqual(self.key(), before)
        before = self.key()
        self.assertNotEqual(self.key(host=["host /usr/bin/a 1 2"]), before)
        self.assertNotEqual(self.key(installed=["a==1", "b==1"]), before)
        self.assertNotEqual(self.key(interpreter="3.13.17"), before)

    def test_a_link_moves_the_key_with_its_target(self):
        (self.root / "ansible/link").symlink_to("ansible.cfg")
        before = self.key()
        (self.root / "ansible/link").unlink()
        (self.root / "ansible/link").symlink_to("collections")
        self.assertNotEqual(self.key(), before)

    def test_byte_code_pytest_caches_and_other_tools_leave_the_key(self):
        before = self.key()
        for name in (
            "ansible/__pycache__/a.pyc",
            "ansible/collections/.pytest_cache/v",
        ):
            (self.root / name).parent.mkdir(parents=True)
            (self.root / name).write_text("x", encoding="utf-8")
        (self.root / "scripts/tools/focus.py").write_text("x", encoding="utf-8")
        self.assertEqual(self.key(), before)

    def test_the_host_state_names_package_directories(self):
        self.assertIn("/usr/bin", ansible_check.HOST_DIRECTORIES)
        self.assertIn("/var/lib/rpm", ansible_check.HOST_DIRECTORIES)
        lines = ansible_check.host_state()
        self.assertTrue(lines)
        self.assertTrue(all(line.startswith("host ") for line in lines))


class RecordedGateTest(unittest.TestCase):
    """A passing complete run is reused only while every input is unchanged."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.records = Path(self.temporary.name) / "passes"
        self.runs = 0
        self.now = 1_000_000.0

    def gate(self, status=0, key=lambda: "k" * 64, quiet=lambda started: True):
        def check():
            self.runs += 1
            return status

        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            result = ansible_check.recorded_gate(
                self.records, key, quiet, check, lambda: self.now
            )
        self.output = output.getvalue()
        return result

    def test_a_pass_is_reused_while_fresh_and_says_so(self):
        self.assertEqual((self.gate(), self.runs), (0, 1))
        self.now += ansible_check.PASS_LIFETIME - 1
        self.assertEqual((self.gate(), self.runs), (0, 1))
        self.assertIn("reused, not rerun", self.output)

    def test_an_expired_pass_runs_again(self):
        self.gate()
        self.now += ansible_check.PASS_LIFETIME
        self.assertEqual((self.gate(), self.runs), (0, 2))
        self.assertNotIn("reused", self.output)

    def test_a_failure_is_never_recorded(self):
        self.assertEqual(self.gate(status=1), 1)
        self.assertEqual((self.gate(status=1), self.runs), (1, 2))
        self.assertFalse(self.records.exists())

    def test_a_key_that_moves_during_the_run_records_nothing(self):
        keys = iter(["a" * 64, "b" * 64])
        self.assertEqual(self.gate(key=lambda: next(keys)), 0)
        self.assertFalse(self.records.exists())
        self.assertIn("no pass is recorded", self.output)

    def test_a_write_since_the_start_records_nothing(self):
        started = []

        def quiet(start):
            started.append(start)
            return False

        self.assertEqual(self.gate(quiet=quiet), 0)
        self.assertEqual(started, [self.now])
        self.assertFalse(self.records.exists())

    def test_a_damaged_record_runs_the_check(self):
        self.records.mkdir()
        (self.records / ("k" * 64)).write_text("{", encoding="utf-8")
        self.assertEqual((self.gate(), self.runs), (0, 1))

    def test_a_record_that_cannot_be_written_keeps_the_pass(self):
        self.records.write_text("", encoding="utf-8")
        self.assertEqual(self.gate(), 0)
        self.assertIn("could not be recorded", self.output)

    def test_another_run_writing_the_same_key_does_not_collide(self):
        self.records.mkdir()
        (self.records / ("k" * 64 + ".partial")).write_text("", encoding="utf-8")
        self.assertEqual(self.gate(), 0)
        self.assertTrue((self.records / ("k" * 64)).is_file())

    def test_only_the_newest_passes_are_kept(self):
        self.records.mkdir()
        for index in range(3):
            old = self.records / (str(index) * 64)
            old.write_text('{"passed": 0}', encoding="utf-8")
            os.utime(old, (index + 1, index + 1))
        with mock.patch.object(ansible_check, "KEPT_PASSES", 2):
            self.gate()
        self.assertEqual(
            sorted(
                path.name
                for path in self.records.iterdir()
                if not path.name.endswith(".partial")
            ),
            ["2" * 64, "k" * 64],
        )


class UsesRecordTest(unittest.TestCase):
    """Only a complete run outside CI and without FULL reads or writes a record."""

    def test_only_the_complete_local_run_uses_the_record(self):
        self.assertTrue(ansible_check.uses_record([], {}))
        for arguments, environ in (
            ([], {"CI": "true"}),
            ([], {ansible_check.FULL_VARIABLE: "1"}),
            (["--suite", "units"], {}),
            (["--suite", "units", "a"], {}),
            ([], {"BOOTWRIGHT_ANSIBLE_NATIVE_TARGET": "1"}),
            ([], {"BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET": "1"}),
        ):
            with self.subTest(arguments=arguments, environ=environ):
                self.assertFalse(ansible_check.uses_record(arguments, environ))


class WrittenSinceTest(unittest.TestCase):
    """A write to any input after the start shows, even when undone."""

    def test_an_edit_undone_still_shows(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in ("ansible/a.yml", "scripts/ansible-check", "scripts/tools/x"):
                (root / name).parent.mkdir(parents=True, exist_ok=True)
                (root / name).write_text("a", encoding="utf-8")
            later = time.time() + 5
            self.assertFalse(ansible_check.written_since(root, later))
            path = root / "ansible/a.yml"
            status = path.stat()
            path.write_text("b", encoding="utf-8")
            path.write_text("a", encoding="utf-8")
            os.utime(path, ns=(status.st_atime_ns, status.st_mtime_ns))
            self.assertTrue(ansible_check.written_since(root, time.time()))


if __name__ == "__main__":
    unittest.main()
