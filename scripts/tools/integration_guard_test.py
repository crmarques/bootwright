"""Exercise real parallel sessions in disposable clones; no product effects."""

from __future__ import annotations

import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

GUARD = Path(__file__).with_name("integration_guard.py")
GIT_ENV = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
GIT_ENV.update({
    "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
    "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
    "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
})


class CoordinationTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name) / "destination with spaces"
        self.root.mkdir()
        self.git(self.root, "init", "-q", "-b", "main")
        self.git(self.root, "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "initial")
        self.first = Path(temporary.name) / "first"
        self.second = Path(temporary.name) / "second"
        for path in (self.first, self.second):
            self.git(self.root, "worktree", "add", "-q", "-b", path.name, str(path))

    def git(self, root, *args):
        return subprocess.check_output(["git", "-C", str(root), *args], env=GIT_ENV).decode().strip()

    def guard(self, root, *args):
        return subprocess.run(
            [sys.executable, "-I", "-B", str(GUARD), *args], cwd=root,
            env=GIT_ENV, capture_output=True, text=True, timeout=10,
        )

    def commit(self, root, filename):
        (root / filename).write_text(filename)
        self.git(root, "add", filename)
        self.git(root, "-c", "commit.gpgsign=false", "commit", "-q", "-m", filename)

    def running(self):
        process = subprocess.Popen(
            [sys.executable, "-I", "-B", str(GUARD), "run", "--", sys.executable,
             "-I", "-c", "import os,time; print(os.getpid(), flush=True); time.sleep(60)"],
            cwd=self.first, env=GIT_ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        self.addCleanup(self.cleanup_process, process)
        self.assertEqual(json.loads(process.stdout.readline())["reservation"], "available")
        child = int(process.stdout.readline())
        return process, child

    @staticmethod
    def cleanup_process(process):
        if process.poll() is None:
            process.terminate()
        process.communicate(timeout=10)

    def test_ready_inspection_changes_no_refs_or_working_files(self):
        before = self.git(self.root, "show-ref")
        result = self.guard(self.first, "status")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(json.loads(result.stdout)["rebase_needed"])
        self.assertEqual(before, self.git(self.root, "show-ref"))
        self.assertEqual(self.git(self.first, "status", "--porcelain"), "")

    def test_stale_branch_runs_no_expensive_command_even_when_paths_are_disjoint(self):
        self.commit(self.root, "incoming")
        self.commit(self.first, "task")
        result = self.guard(self.first, "run", "--", sys.executable, "-c", "raise AssertionError('started')")
        self.assertEqual(result.returncode, 3, result.stderr)
        self.assertTrue(json.loads(result.stdout)["rebase_needed"])
        self.git(self.first, "rebase", "main")
        self.assertEqual(self.guard(self.first, "status").returncode, 0)

    def test_destination_dirt_is_preserved_and_blocks_before_command(self):
        (self.root / "private").write_text("preserve")
        result = self.guard(self.first, "run", "--", sys.executable, "-c", "raise AssertionError('started')")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("changes", json.loads(result.stdout)["reason"])
        self.assertEqual((self.root / "private").read_text(), "preserve")

    def test_other_worktree_sees_live_owner_and_cannot_start_another_gate(self):
        process, _child = self.running()
        result = self.guard(self.second, "run", "--", sys.executable, "-c", "raise AssertionError('started')")
        self.assertEqual(result.returncode, 75, result.stderr)
        state = json.loads(result.stdout)
        self.assertEqual(state["owner"]["task_branch"], "first")
        self.assertEqual(state["owner"]["pid"], process.pid)
        process.terminate()
        process.communicate(timeout=10)
        self.assertEqual(process.returncode, 128 + signal.SIGTERM)
        self.assertEqual(self.guard(self.second, "status").returncode, 0)

    def test_child_keeps_reservation_when_supervisor_is_killed(self):
        process, child = self.running()
        try:
            process.kill()
            process.wait(timeout=10)
            self.assertEqual(self.guard(self.second, "status").returncode, 75)
        finally:
            os.killpg(child, signal.SIGKILL)
        for _ in range(100):
            if self.guard(self.second, "status").returncode == 0:
                break
            time.sleep(0.01)
        else:
            self.fail("reservation survived every owner")

    def test_failed_command_releases_reservation_and_propagates_failure(self):
        result = self.guard(self.first, "run", "--", sys.executable, "-c", "raise SystemExit(7)")
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assertEqual(self.guard(self.second, "status").returncode, 0)

    def test_reservation_covers_fast_forward_then_next_session_rebases_before_testing(self):
        self.commit(self.first, "first-change")
        source = (
            "import os,subprocess; "
            "subprocess.run(['git','-C',os.environ['BOOTWRIGHT_INTEGRATION_WORKTREE'],"
            "'merge','--ff-only', 'first'],check=True)"
        )
        result = self.guard(self.first, "run", "--", sys.executable, "-c", source)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.git(self.root, "rev-parse", "HEAD"), self.git(self.first, "rev-parse", "HEAD"))
        self.assertEqual(self.guard(self.second, "status").returncode, 3)

    def test_same_checkout_detached_and_missing_destination_fail_closed(self):
        self.assertEqual(self.guard(self.root, "status").returncode, 1)
        self.git(self.first, "checkout", "--detach", "-q")
        self.assertEqual(self.guard(self.first, "status").returncode, 1)
        self.assertEqual(self.guard(self.second, "--destination", "missing", "status").returncode, 1)


if __name__ == "__main__":
    unittest.main()
