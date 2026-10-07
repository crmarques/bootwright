"""The native bridge carries the helper's classified refusal, never its text
in a record: its first stderr line names the class, its second is the only
detail, and anything else is internal."""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_native as native,
)

PLATFORM = {"os": "fedora", "release": "43", "architecture": "amd64"}
POPEN = subprocess.Popen


def stub(stderr, code=1):
    """Run a stand-in helper that writes stderr and exits with code, in place
    of the provided interpreter, keeping every other launch argument."""

    def launch(arguments, **keywords):
        script = "import sys; sys.stdin.buffer.read(); sys.stderr.buffer.write(%r); sys.exit(%d)" % (stderr, code)
        return POPEN([sys.executable, "-c", script], **keywords)

    return mock.patch.object(native.subprocess, "Popen", launch)


class HelperRefusal(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.mkdtemp(prefix="bootwright-dnf-test-", dir="/tmp")
        os.chmod(self.scratch, 0o700)
        self.addCleanup(os.rmdir, self.scratch)

    def refused(self, stderr):
        with stub(stderr):
            with self.assertRaises(native.NativeRefused) as refused:
                native.invoke({"operation": "inspect", "platform": PLATFORM}, self.scratch)
        return refused.exception

    def test_a_helper_refusal_carries_its_class(self):
        refusal = self.refused(b"refused solver-conflict\nDepsolveError: x\n")
        self.assertEqual(refusal.reason, "solver-conflict")
        self.assertEqual(refusal.detail, "DepsolveError: x")
        self.assertEqual(str(refusal), "native operation refused")

    def test_an_unclassified_helper_failure_is_internal(self):
        for stderr in (
            b"",
            b"Traceback (most recent call last):\n",
            b"refused quota\nOSError: x\n",
            b"refused solver-conflict-ish\n",
        ):
            with self.subTest(stderr):
                self.assertEqual(self.refused(stderr).reason, "internal")

    def test_the_detail_is_bounded_and_holds_no_control_characters(self):
        refusal = self.refused(b"refused database\nDatabaseLocked: \x1b[31m" + b"x" * 5000 + b"\nthird\n")
        self.assertEqual(refusal.reason, "database")
        self.assertLessEqual(len(refusal.detail.encode("utf-8")), 200)
        self.assertNotIn("\x1b", refusal.detail)
        self.assertNotIn("third", refusal.detail)

    def test_an_inspection_past_its_bound_is_a_timeout(self):
        def expired(process, *_args, **_kwargs):
            raise subprocess.TimeoutExpired("helper", 120)

        with stub(b""), mock.patch.object(POPEN, "communicate", expired):
            with self.assertRaises(native.NativeRefused) as refused:
                native.invoke({"operation": "inspect", "platform": PLATFORM}, self.scratch)
        self.assertEqual(refused.exception.reason, "timeout")


if __name__ == "__main__":
    unittest.main()
