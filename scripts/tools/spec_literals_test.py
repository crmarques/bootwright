"""Tests of the spec literal check; run: python3 -I -B spec_literals_test.py."""

from __future__ import annotations

import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HELPER = Path(__file__).with_name("spec_literals.py")
GIT_ENV = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
GIT_ENV.update({
    "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
    "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
})

_specification = importlib.util.spec_from_file_location("spec_literals", HELPER)
spec_literals = importlib.util.module_from_spec(_specification)
_specification.loader.exec_module(spec_literals)


class LiteralsTest(unittest.TestCase):
    def test_collects_spans_and_quantities(self):
        text = (
            "Refuses `secret.store.conflict` with ``--yes`` under `/etc/x/y`.\n"
            "A bound of 64 KiB, 5 s and 20% holds; `make docs-check`\n"
            "wraps `two\nwords`.\n"
        )
        self.assertEqual(
            set(spec_literals.literals(text)),
            {"secret.store.conflict", "--yes", "/etc/x/y", "64 KiB", "5 s", "20%",
             "make docs-check", "two words"},
        )

    def test_counts_every_occurrence(self):
        self.assertEqual(spec_literals.literals("`a` and `a`, 8 KiB and 8KiB.\n"), {"a": 2, "8 KiB": 2})

    def test_ignores_fences_and_word_parts(self):
        text = "```\n`inside` 9 KiB\n```\nThe X64 KiB and v1.2 s are no quantities.\n"
        self.assertEqual(spec_literals.literals(text), {})

    def test_a_tilde_line_inside_a_backtick_fence_does_not_close_it(self):
        text = "```\n~~~\n`inside` 9 KiB\n```\n`outside`\n~~~\n`fenced`\n~~~\n"
        self.assertEqual(spec_literals.literals(text), {"outside": 1})

    def test_spells_units_one_way(self):
        text = "Waits 5 seconds, 5 s, 2 minutes, 2 min, 30 milliseconds, 30 ms, 1 hour, 7 ns and 3 us.\n"
        self.assertEqual(
            spec_literals.literals(text),
            {"5 s": 2, "2 min": 2, "30 ms": 2, "1 hours": 1, "7 ns": 1, "3 us": 1},
        )

    def test_spells_a_quantity_one_way(self):
        self.assertEqual(spec_literals.canonical("`64KiB`"), "64 KiB")
        self.assertEqual(spec_literals.canonical(" 64   KiB "), "64 KiB")
        self.assertEqual(spec_literals.canonical("20 %"), "20%")
        self.assertEqual(spec_literals.canonical("`  TestXxx `"), "TestXxx")

    def test_reads_declared_moves(self):
        body = "Gates: ok.\n\nLiteral-moved: `a.b` -> specs/x.md#y\nLiteral-moved: 64KiB -> the limits page\nLiteral-moved: bad ->\n"
        self.assertEqual(spec_literals.declared(body), ({"a.b", "64 KiB"}, set()))
        self.assertEqual(
            spec_literals.declared("Literal-moved: specs/x.md/* -> specs/y.md\n"), (set(), {"specs/x.md"})
        )

    def test_names_only_what_no_later_page_holds(self):
        before = {"specs/a.md": "`x` `y` 8 MiB"}
        after = {"specs/a.md": "`x`", "specs/b.md": "`y`"}
        self.assertEqual(spec_literals.lost(before, after), ["8 MiB"])

    def test_names_a_literal_whose_occurrences_fell(self):
        before = {"specs/a.md": "`x` and `x`", "specs/b.md": "`x`"}
        after = {"specs/a.md": "`x`", "specs/b.md": "`x`"}
        self.assertEqual(spec_literals.lost(before, after), ["x"])
        after = {"specs/a.md": "`x`", "specs/b.md": "`x` `x`"}
        self.assertEqual(spec_literals.lost(before, after), [])

    def test_a_page_declared_moved_holds_none(self):
        before = {"specs/a.md": "`x` `y`", "specs/b.md": "`z`"}
        self.assertEqual(spec_literals.lost(before, {}, frozenset({"specs/a.md"})), ["z"])

    def test_normative_pages(self):
        for path, normative in {
            "specs/a.md": True, "specs/api/b.md": True, "specs/milestones/m1.md": False,
            "specs/deferred/d.md": False, "docs/a.md": False, "internal/specs/a.md": False,
        }.items():
            self.assertEqual(spec_literals.is_normative(path), normative, path)


class CommitTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.git("init", "-q", "-b", "main")
        self.write("specs/a.md", "# A\n\nThe `kept` and `gone` rules hold 64 KiB.\n")
        self.write("specs/b.md", "# B\n")
        self.commit("docs: start")

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args], env=GIT_ENV).decode().strip()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, message):
        self.git("add", "-A")
        self.git("-c", "commit.gpgsign=false", "commit", "-q", "-m", message)

    def refusals(self):
        done = subprocess.run(
            [sys.executable, "-I", "-B", str(HELPER), "HEAD"], cwd=self.root, env=GIT_ENV,
            capture_output=True, text=True, timeout=20,
        )
        self.assertEqual(done.returncode, 1 if done.stderr else 0, done.stderr)
        return done.stderr.splitlines()

    def test_a_dropped_literal_fails(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule holds 64 KiB.\n")
        self.commit("docs: trim")
        refusals = self.refusals()
        self.assertEqual(len(refusals), 1)
        self.assertIn("drops the literal `gone`", refusals[0])

    def test_a_dropped_quantity_fails(self):
        self.write("specs/a.md", "# A\n\nThe `kept` and `gone` rules hold 32 KiB.\n")
        self.commit("docs: change a bound")
        self.assertIn("drops the literal `64 KiB`", self.refusals()[0])

    def test_a_matching_line_passes(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule holds 64 KiB.\n")
        self.commit("docs: trim\n\nGates: ok.\n\nLiteral-moved: `gone` -> specs/b.md#gone")
        self.assertEqual(self.refusals(), [])

    def test_a_line_for_another_literal_does_not_pass(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule holds 64 KiB.\n")
        self.commit("docs: trim\n\nLiteral-moved: `kept` -> specs/b.md")
        self.assertIn("drops the literal `gone`", self.refusals()[0])

    def test_a_move_between_pages_passes(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule.\n")
        self.write("specs/b.md", "# B\n\nThe `gone` rule holds 64 KiB.\n")
        self.commit("docs: move")
        self.assertEqual(self.refusals(), [])

    def test_a_move_to_a_new_page_passes(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule.\n")
        self.write("specs/api/c.md", "# C\n\n`gone` holds 64 KiB.\n")
        self.commit("docs: split")
        self.assertEqual(self.refusals(), [])

    def test_a_literal_moved_out_of_the_normative_pages_fails(self):
        self.write("specs/a.md", "# A\n\nThe `kept` rule.\n")
        self.commit("docs: move out")
        self.assertEqual(len(self.refusals()), 2)

    def test_one_of_two_occurrences_dropped_fails(self):
        self.write("specs/a.md", "# A\n\nThe `kept` and `gone` rules hold 64 KiB.\n\nSee `kept` again.\n")
        self.commit("docs: add a second use")
        self.write("specs/a.md", "# A\n\nThe `kept` and `gone` rules hold 64 KiB.\n")
        self.commit("docs: drop the second use")
        self.assertIn("drops the literal `kept`", self.refusals()[0])

    def test_a_page_level_line_passes_a_deleted_page(self):
        (self.root / "specs/a.md").unlink()
        self.commit("docs: delete\n\nLiteral-moved: specs/a.md/* -> specs/b.md and the history")
        self.assertEqual(self.refusals(), [])

    def test_a_page_level_line_excuses_only_its_page(self):
        self.write("specs/b.md", "# B\n\n`other`\n")
        self.commit("docs: add")
        (self.root / "specs/a.md").unlink()
        self.write("specs/b.md", "# B\n")
        self.commit("docs: delete\n\nLiteral-moved: specs/a.md/* -> nowhere")
        self.assertEqual(len(self.refusals()), 1)
        self.assertIn("`other`", self.refusals()[0])

    def test_documentation_beside_a_page_is_checked(self):
        for path in ("audits/r/report.md", "docs/guide.md", ".agents/skills/x.txt", "specs/milestones/m1.md", "NOTES.md"):
            self.write("specs/a.md", "# A\n")
            self.write(path, path)
            self.commit("docs: trim " + path)
            self.assertEqual(len(self.refusals()), 3, path)
            self.git("reset", "-q", "--hard", "HEAD~1")

    def test_a_commit_touching_code_is_not_checked(self):
        self.write("specs/a.md", "# A\n")
        self.write("internal/x/x.go", "package x\n")
        self.commit("fix(x): code")
        self.assertEqual(self.refusals(), [])

    def test_a_commit_touching_a_script_beside_documentation_is_not_checked(self):
        self.write("specs/a.md", "# A\n")
        self.write("specs/milestones/m1.md", "# M1\n")
        self.write("scripts/x.sh", "true\n")
        self.commit("docs: record")
        self.assertEqual(self.refusals(), [])

    def test_a_deleted_page_loses_its_literals(self):
        (self.root / "specs/a.md").unlink()
        self.commit("docs: delete")
        self.assertEqual(len(self.refusals()), 3)

    def test_the_first_commit_is_not_checked(self):
        first = self.git("rev-list", "--max-parents=0", "HEAD")
        done = subprocess.run(
            [sys.executable, "-I", "-B", str(HELPER), first], cwd=self.root, env=GIT_ENV,
            capture_output=True, text=True, timeout=20,
        )
        self.assertEqual((done.returncode, done.stderr), (0, ""))


if __name__ == "__main__":
    unittest.main()
