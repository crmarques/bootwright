"""Private native commands run through the bounded runner with hard capture limits and reap their descendants."""

import sys
import unittest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker


class ProcessTests(unittest.TestCase):
    def test_private_stdin_and_bounded_reply(self):
        argv = [sys.executable, "-I", "-c", "import sys;sys.stderr.write('PRIVATE');sys.stdout.buffer.write(sys.stdin.buffer.read())"]
        self.assertEqual(worker.native(argv, b"receipt", timeout=5), (0, b"receipt"))

    def test_output_flood_is_stopped_before_capture_can_grow(self):
        argv = [sys.executable, "-I", "-c", "import os;\nwhile True:os.write(1,b'x'*65536)"]
        with self.assertRaisesRegex(worker.Refusal, "native-output"):
            worker.native(argv, timeout=5, limit=16)

    def test_timeout_reaps_a_command_with_no_reply(self):
        argv = [sys.executable, "-I", "-c", "import time;time.sleep(30)"]
        with self.assertRaisesRegex(worker.Refusal, "native-timeout"):
            worker.native(argv, timeout=0.2)

    def test_a_command_that_cannot_start_is_a_native_command_refusal(self):
        with self.assertRaisesRegex(worker.Refusal, "native-command"):
            worker.native(["/nonexistent/bootwright-erase-command"])

    def test_the_command_runs_with_the_fixed_environment_cwd_and_umask(self):
        argv = [sys.executable, "-I", "-c", "import os;print(sorted(os.environ), os.getcwd(), oct(os.umask(0)))"]
        code, output = worker.native(argv, timeout=5)
        self.assertEqual((code, output), (0, b"['LC_ALL', 'PATH'] / 0o77\n"))

    def test_the_ram_image_ships_the_bounded_runner_and_runs_the_worker_alone(self):
        import base64
        import pathlib
        import subprocess
        import tempfile
        from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot

        lines = boot.kickstart("network --bootproto=dhcp", {}, "PRIVATE", "ssh-key PUBLIC").splitlines()
        prefix = "base64 --decode > " + boot.RAM + "/"
        shipped = {line[len(prefix):].split(" ", 1)[0]: base64.b64decode(lines[index + 1]) for index, line in enumerate(lines) if line.startswith(prefix)}
        self.assertIn("bounded_process.py", shipped)
        self.assertNotIn("physical_erase_process.py", shipped)
        with tempfile.TemporaryDirectory() as ram:
            for name in ("physical_erase_worker.py", "bounded_process.py", "physical_machine_identity.py"):
                pathlib.Path(ram, name).write_bytes(shipped[name])
            probe = ("import sys; sys.path.insert(0, sys.argv[1]); import physical_erase_worker as worker; "
                     "print(worker.native([sys.executable, '-I', '-c', 'print(7)'], timeout=5))")
            # pylint: disable-next=ansible-bad-function
            answer = subprocess.run([sys.executable, "-I", "-c", probe, ram], capture_output=True, timeout=30, check=False)
        self.assertEqual((answer.returncode, answer.stdout), (0, b"(0, b'7\\n')\n"), answer.stderr)
