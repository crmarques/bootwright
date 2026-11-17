"""Unit tests of the environment-labelled peer server."""

from __future__ import annotations

import importlib.util
import io
import json
import runpy
import unittest
from pathlib import Path
from unittest import mock

SERVER = Path(__file__).with_name("server.py")
SPEC = importlib.util.spec_from_file_location("e2e_peer_server", SERVER)
server = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(server)


class Recorded(server.Handler):
    def __init__(self, address):
        self.client_address = (address, 40000)
        self.wfile = io.BytesIO()
        self.sent = []

    def send_response(self, code, message=None):
        self.sent.append(("status", code))

    def send_header(self, name, value):
        self.sent.append((name, value))

    def end_headers(self):
        pass


class PeerTest(unittest.TestCase):
    def test_it_names_its_environment_and_the_address_that_reached_it(self):
        for environment in ("staging", "production"):
            with self.subTest(environment=environment), mock.patch.dict(server.os.environ, {"ENVIRONMENT": environment}):
                request = Recorded("192.0.2.60")
                request.do_GET()
                body = request.wfile.getvalue()
                self.assertEqual(json.loads(body), {"environment": environment, "source": "192.0.2.60"})
                self.assertIn(("status", 200), request.sent)
                self.assertIn(("Content-Length", str(len(body))), request.sent)

    def test_any_other_environment_refuses_to_serve(self):
        for value in (None, "", "dev", "Staging"):
            with self.subTest(value=value):
                environment = {} if value is None else {"ENVIRONMENT": value}
                with mock.patch.dict(server.os.environ, environment, clear=True), mock.patch(
                    "http.server.ThreadingHTTPServer"
                ) as serve, self.assertRaises(SystemExit) as stop:
                    runpy.run_path(str(SERVER), run_name="__main__")
                self.assertIn("ENVIRONMENT must be staging or production", str(stop.exception.code))
                serve.assert_not_called()


if __name__ == "__main__":
    unittest.main()
