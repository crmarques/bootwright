"""Private native commands have hard capture limits and reap their descendants."""

import sys
import unittest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process as process


class ProcessTests(unittest.TestCase):
    def test_private_stdin_and_bounded_reply(self):
        argv = [sys.executable, "-I", "-c", "import sys;sys.stderr.write('PRIVATE');sys.stdout.buffer.write(sys.stdin.buffer.read())"]
        self.assertEqual(process.run(argv, b"receipt", timeout=5), (0, b"receipt"))

    def test_output_flood_is_stopped_before_capture_can_grow(self):
        argv = [sys.executable, "-I", "-c", "import os;\nwhile True:os.write(1,b'x'*65536)"]
        with self.assertRaisesRegex(process.Refusal, "native-output"):
            process.run(argv, timeout=5, limit=16)

    def test_timeout_reaps_a_command_with_no_reply(self):
        argv = [sys.executable, "-I", "-c", "import time;time.sleep(30)"]
        with self.assertRaisesRegex(process.Refusal, "native-timeout"):
            process.run(argv, timeout=0.2)
