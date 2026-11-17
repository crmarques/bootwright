"""Unit tests of the sample server's client: canaries, round trip and exit code."""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
import unittest
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location("e2e_sample_client", Path(__file__).with_name("client.py"))
client = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(client)


class FakeServer:
    """An in-memory sample server answering the client's calls."""

    def __init__(self, healthy=True, sources=None):
        self.files = {}
        self.healthy = healthy
        self.sources = sources or {"mon": {}, "objectVIP": {}}
        self.corrupt = False
        self.keep_deleted = False

    def call(self, base, method, path, body=None):
        if path == "/healthz":
            document = {"ready": {}, "sourceAddress": self.sources}
            return (200 if self.healthy else 503), json.dumps(document).encode()
        if method == "PUT":
            self.files[path] = body
            return 201, json.dumps({"sha256": hashlib.sha256(body).hexdigest()}).encode()
        if method == "GET":
            if path not in self.files:
                return 404, b"{}"
            data = self.files[path]
            return 200, (data[:-1] + b"\x00" if self.corrupt and data else data)
        if method == "DELETE":
            if path not in self.files:
                return 404, b"{}"
            if not self.keep_deleted:
                del self.files[path]
            return 200, b"{}"
        raise AssertionError(method)


def run(server, *argv):
    out = io.StringIO()
    with mock.patch.object(client, "call", server.call), mock.patch.object(
        client.sys, "argv", ["client.py", "http://sample", *argv]
    ), contextlib.redirect_stdout(out):
        try:
            client.main()
            code = 0
        except SystemExit as stop:
            code = stop.code
    return code, out.getvalue()


class CanaryBytesTest(unittest.TestCase):
    def test_the_canary_is_deterministic_per_store_and_262144_bytes(self):
        blocks = {store: client.canary_bytes(store) for store in client.STORES}
        self.assertEqual(len({*blocks.values()}), 3)
        for store, data in blocks.items():
            self.assertEqual(len(data), 8192 * 32)
            self.assertEqual(data, client.canary_bytes(store))
        seed = hashlib.sha256(b"canary-lab-5-nodes-ceph-networks/block").digest()
        self.assertEqual(blocks["block"][:32], hashlib.sha256(seed + (0).to_bytes(4, "big")).digest())
        self.assertEqual(blocks["block"][-32:], hashlib.sha256(seed + (8191).to_bytes(4, "big")).digest())


class CanaryModeTest(unittest.TestCase):
    def test_a_written_canary_reads_back_intact_and_is_cleaned(self):
        server = FakeServer()
        for mode in ("write-canary", "check-canary", "clean-canary"):
            with self.subTest(mode=mode):
                code, output = run(server, mode)
                self.assertEqual(code, 0, output)
        self.assertEqual(server.files, {})

    def test_a_changed_byte_or_a_missing_canary_fails_the_check(self):
        server = FakeServer()
        run(server, "write-canary")
        server.corrupt = True
        code, output = run(server, "check-canary")
        self.assertEqual(code, 1)
        self.assertEqual(output.count("FAIL  "), 3, output)
        server.corrupt = False
        server.files.clear()
        code, output = run(server, "check-canary")
        self.assertEqual(code, 1)
        self.assertEqual(output.count("FAIL  "), 3, output)

    def test_a_canary_of_another_store_does_not_pass_for_this_one(self):
        server = FakeServer()
        run(server, "write-canary")
        server.files["/block/" + client.CANARY] = server.files["/file/" + client.CANARY]
        code, output = run(server, "check-canary")
        self.assertEqual(code, 1)
        self.assertIn("FAIL  block canary intact", output)


class RoundtripTest(unittest.TestCase):
    def test_a_faithful_server_passes_every_store(self):
        code, output = run(FakeServer(), "roundtrip")
        self.assertEqual(code, 0, output)
        self.assertNotIn("FAIL", output)
        self.assertIn("passed, 0 failed", output)

    def test_stale_reads_fail(self):
        server = FakeServer()
        server.corrupt = True
        code, output = run(server, "roundtrip")
        self.assertEqual(code, 1)
        self.assertIn("FAIL  block get 4096 B matches", output)

    def test_a_delete_that_leaves_the_object_fails(self):
        server = FakeServer()
        server.keep_deleted = True
        code, output = run(server, "roundtrip")
        self.assertEqual(code, 1)
        self.assertIn("is gone", output.split("FAIL  ", 1)[1])


class HealthAndExitTest(unittest.TestCase):
    def test_an_unready_server_fails_even_when_every_store_check_passes(self):
        code, output = run(FakeServer(healthy=False), "write-canary")
        self.assertEqual(code, 1)
        self.assertIn("FAIL  healthz answers 200 with every store ready", output)

    def test_the_expected_source_is_held_towards_every_endpoint(self):
        sources = {"mon": {"192.0.2.1": "198.51.100.5"}, "objectVIP": {"192.0.2.2": "203.0.113.5"}}
        code, output = run(FakeServer(sources=sources), "write-canary", "198.51.100.5")
        self.assertEqual(code, 1)
        self.assertIn("PASS  source towards mon 192.0.2.1 is 198.51.100.5", output)
        self.assertIn("FAIL  source towards objectVIP 192.0.2.2 is 198.51.100.5  got 203.0.113.5", output)

    def test_an_unknown_mode_refuses_before_any_call(self):
        server = FakeServer()
        with self.assertRaises(SystemExit) as stop:
            run_without_catch(server, "unknown")
        self.assertIn("unknown mode", str(stop.exception.code))


def run_without_catch(server, *argv):
    with mock.patch.object(client, "call", server.call), mock.patch.object(
        client.sys, "argv", ["client.py", "http://sample", *argv]
    ):
        client.main()


if __name__ == "__main__":
    unittest.main()
