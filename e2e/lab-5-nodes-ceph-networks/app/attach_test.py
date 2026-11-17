"""Unit tests of the attach steps of app-setup.sh."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location("e2e_attach", Path(__file__).with_name("attach.py"))
attach = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(attach)


class Files(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.TemporaryDirectory()
        self.addCleanup(self.root.cleanup)

    def write(self, name, text):
        path = Path(self.root.name, name)
        path.write_text(text)
        return str(path)

    def run_step(self, *argv):
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            attach.main(["attach.py", *argv])
        return out.getvalue()


class KeyringTest(Files):
    def test_the_key_of_a_keyring_file_is_printed_without_a_newline(self):
        path = self.write("a.keyring", "[client.app-block]\n\tkey = AQDabc==\n\tcaps mon = \"allow r\"\n")
        self.assertEqual(self.run_step("keyring", path), "AQDabc==")

    def test_a_bare_key_is_printed_as_the_file_text(self):
        self.assertEqual(self.run_step("keyring", self.write("b.keyring", "  AQDbare==\n")), "AQDbare==")

    def test_a_key_line_needs_its_own_line(self):
        path = self.write("c.keyring", "[client.x]\nmonkey = nope\n\tkey=AQDright==\n")
        self.assertEqual(self.run_step("keyring", path), "AQDright==")


class MonitorsTest(Files):
    def test_msgr_v1_addresses_are_chosen_from_a_dual_protocol_line(self):
        path = self.write("ceph.conf", "[global]\n\tmon_host = [v2:198.51.100.1:3300/0,v1:198.51.100.1:6789/0] [v2:198.51.100.2:3300/0,v1:198.51.100.2:6789/0]\n")
        self.assertEqual(self.run_step("monitors", path), "198.51.100.1:6789,198.51.100.2:6789\n")

    def test_bare_addresses_take_the_default_v1_port(self):
        path = self.write("ceph.conf", "mon_host = 198.51.100.1,198.51.100.2 198.51.100.3\n")
        self.assertEqual(self.run_step("monitors", path), "198.51.100.1:6789,198.51.100.2:6789,198.51.100.3:6789\n")

    def test_a_configuration_without_mon_host_refuses(self):
        with self.assertRaises(SystemExit) as stop:
            self.run_step("monitors", self.write("ceph.conf", "[global]\nfsid = x\n"))
        self.assertIn("names no mon_host", str(stop.exception.code))


class ConfigTest(Files):
    def test_the_server_configuration_names_its_stores_monitors_and_s3(self):
        etc = self.root.name
        self.run_step(
            "config", "198.51.100.1:6789,198.51.100.2:6789", "192.0.2.5", "8080", "/srv/ceph-block", "/srv/ceph-file",
            "203.0.113.9", "s3.example.test", "app-bucket", "objects", etc,
        )
        written = Path(etc, "config.json").read_text()
        self.assertEqual(
            json.loads(written),
            {
                "listen": "192.0.2.5",
                "port": 8080,
                "blockDir": "/srv/ceph-block",
                "fileDir": "/srv/ceph-file",
                "monitors": ["198.51.100.1", "198.51.100.2"],
                "s3": {
                    "vip": "203.0.113.9",
                    "host": "s3.example.test",
                    "bucket": "app-bucket",
                    "region": "objects",
                    "caFile": etc + "/rgw-ca.pem",
                    "credentialsFile": etc + "/s3.json",
                },
            },
        )
        self.assertEqual(written, json.dumps(json.loads(written), indent=2, sort_keys=True))


class MountTest(Files):
    def mount(self, result, errno=0):
        secret = self.write("secret", "  AQDsecret==\n")
        libc = mock.Mock()
        libc.mount.return_value = result
        with mock.patch.object(attach.ctypes, "CDLL", return_value=libc) as library, mock.patch.object(
            attach.ctypes, "get_errno", return_value=errno
        ):
            try:
                self.run_step("mount", "198.51.100.1:6789,198.51.100.2:6789", "/volumes/apps/app-files/x", "app-files", secret, "files", "/srv/ceph-file")
            finally:
                library.assert_called_once_with("libc.so.6", use_errno=True)
        return libc.mount.call_args.args

    def test_the_key_goes_to_mount_two_in_its_options_and_nowhere_else(self):
        source, target, kind, flags, options = self.mount(0)
        self.assertEqual(source, b"198.51.100.1:6789,198.51.100.2:6789:/volumes/apps/app-files/x")
        self.assertEqual((target, kind, flags), (b"/srv/ceph-file", b"ceph", 0))
        self.assertEqual(options, b"name=app-files,secret=AQDsecret==,mds_namespace=files")

    def test_a_failed_mount_names_the_system_error_and_no_key(self):
        with self.assertRaises(SystemExit) as stop:
            self.mount(-1, errno=13)
        self.assertIn("CephFS mount failed: Permission denied", str(stop.exception.code))
        self.assertNotIn("AQDsecret", str(stop.exception.code))


class UsageTest(unittest.TestCase):
    def test_an_unknown_step_or_a_wrong_argument_count_is_a_usage_error(self):
        for argv in (["attach.py"], ["attach.py", "other"], ["attach.py", "keyring"], ["attach.py", "keyring", "a", "b"]):
            with self.subTest(argv=argv), self.assertRaises(SystemExit) as stop:
                attach.main(argv)
            self.assertIn("usage: attach.py", str(stop.exception.code))


if __name__ == "__main__":
    unittest.main()
