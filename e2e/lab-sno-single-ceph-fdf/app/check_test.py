"""Unit tests of the FDF storage check: S3 signing, redirects and persistence."""

from __future__ import annotations

import contextlib
import datetime
import importlib.util
import io
import tempfile
import types
import unittest
import urllib.request
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location("e2e_fdf_check", Path(__file__).with_name("check.py"))
check = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check)

OBJECT = b"persisted-object\n"
ENVIRONMENT = {
    "BUCKET_HOST": "rgw.example.test",
    "BUCKET_PORT": "8080",
    "BUCKET_NAME": "bucket",
    "AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE",
    "AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "CHECK_MODE": "read",
    "CHECK_OBJECT": "true",
}


class Response:
    def __init__(self, body):
        self.body = body

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def read(self, size):
        return self.body[:size]


def send(method, content=b"", environment=None):
    seen = []

    def build_opener(*handlers):
        opener = mock.Mock()
        opener.open = lambda request, timeout: seen.append((request, handlers)) or Response(OBJECT)
        return opener

    with mock.patch.dict(check.os.environ, environment or ENVIRONMENT), mock.patch.object(
        check.urllib.request, "build_opener", build_opener
    ):
        answer = check.s3_request(method, content)
    request, handlers = seen[0]
    return answer, request, {name.lower(): value for name, value in request.header_items()}, handlers


class S3RequestTest(unittest.TestCase):
    def test_the_request_is_signed_for_its_method_path_and_payload(self):
        answer, request, headers, _ = send("PUT", OBJECT)
        self.assertEqual(answer, OBJECT)
        self.assertEqual(request.get_method(), "PUT")
        self.assertEqual(request.full_url, "http://rgw.example.test:8080/bucket/fdf-check")
        self.assertEqual(request.data, OBJECT)
        self.assertEqual(headers["host"], "rgw.example.test:8080")
        self.assertIn("Credential=AKIAIOSFODNN7EXAMPLE/", headers["authorization"])
        self.assertIn("SignedHeaders=host;x-amz-content-sha256;x-amz-date", headers["authorization"])
        _, _, other, _ = send("PUT", b"another body")
        self.assertNotEqual(headers["x-amz-content-sha256"], other["x-amz-content-sha256"])
        self.assertNotEqual(headers["authorization"], other["authorization"])

    def test_a_get_sends_no_body(self):
        _, request, _, _ = send("GET", b"ignored")
        self.assertIsNone(request.data)

    def test_the_signature_is_the_sigv4_value_of_the_request(self):
        moment = datetime.datetime(2013, 5, 24, tzinfo=datetime.timezone.utc)

        class Clock(datetime.datetime):
            @classmethod
            def now(cls, tz=None):
                return moment

        environment = dict(
            ENVIRONMENT, BUCKET_HOST="examplebucket.s3.amazonaws.com", BUCKET_PORT="80", BUCKET_NAME="examplebucket"
        )
        with mock.patch.object(check.datetime, "datetime", Clock):
            _, _, headers, _ = send("GET", environment=environment)
        self.assertEqual(headers["x-amz-date"], "20130524T000000Z")
        self.assertEqual(
            headers["authorization"],
            "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, "
            "SignedHeaders=host;x-amz-content-sha256;x-amz-date, "
            "Signature=4bbbe9b6f2d9e5e2895fa91742b32522efa74ea52645165150849159b76b5bd2",
        )

    def test_a_redirect_of_a_signed_request_is_an_error(self):
        _, _, _, handlers = send("GET")
        redirect = next(handler for handler in handlers if isinstance(handler, urllib.request.HTTPRedirectHandler))
        with self.assertRaises(RuntimeError):
            redirect.redirect_request(None, None, 307, "", {}, "http://elsewhere")


class PersistenceTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.TemporaryDirectory()
        self.addCleanup(self.root.cleanup)
        for directory in ("block", "file", "tmp"):
            Path(self.root.name, directory).mkdir()
        real, root = check.pathlib.Path, self.root.name
        for patcher in (
            mock.patch.object(check, "pathlib", types.SimpleNamespace(Path=lambda path: real(root + path))),
            mock.patch.object(check.time, "sleep", side_effect=KeyboardInterrupt),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)

    def marker(self, volume):
        return Path(self.root.name, volume, "fdf-check")

    def run_check(self, **environment):
        with mock.patch.dict(check.os.environ, dict(ENVIRONMENT, **environment)), contextlib.redirect_stdout(
            io.StringIO()
        ), self.assertRaises(KeyboardInterrupt):
            check.check()

    def fail_check(self, message, **environment):
        with mock.patch.dict(check.os.environ, dict(ENVIRONMENT, **environment)), self.assertRaisesRegex(
            RuntimeError, message
        ):
            check.check()

    def test_write_then_read_passes_and_marks_the_pod_ready(self):
        with mock.patch.object(check, "s3_request", return_value=OBJECT) as s3:
            self.run_check(CHECK_MODE="write")
            self.assertEqual(s3.call_args_list[0].args, ("PUT", OBJECT))
            Path(self.root.name, "tmp", "passed").unlink()
            self.run_check(CHECK_MODE="read")
        self.assertEqual(self.marker("block").read_bytes(), b"persisted-block\n")
        self.assertEqual(self.marker("file").read_bytes(), b"persisted-file\n")
        self.assertTrue(Path(self.root.name, "tmp", "passed").exists())

    def test_a_read_of_another_content_fails_for_each_volume(self):
        for volume in ("block", "file"):
            with self.subTest(volume=volume), mock.patch.object(check, "s3_request", return_value=OBJECT):
                self.run_check(CHECK_MODE="write")
                self.marker(volume).write_bytes(b"other\n")
                self.fail_check(volume + " persistence failed")

    def test_a_missing_marker_fails_the_read(self):
        with mock.patch.object(check, "s3_request", return_value=OBJECT):
            self.run_check(CHECK_MODE="write")
            self.marker("file").unlink()
            with mock.patch.dict(check.os.environ, ENVIRONMENT), self.assertRaises(FileNotFoundError):
                check.check()

    def test_an_object_that_reads_back_differently_fails(self):
        with mock.patch.object(check, "s3_request", return_value=OBJECT):
            self.run_check(CHECK_MODE="write")
        with mock.patch.object(check, "s3_request", return_value=b"stale\n"):
            self.fail_check("object persistence failed")

    def test_the_object_is_skipped_only_when_the_case_has_none(self):
        with mock.patch.object(check, "s3_request", side_effect=AssertionError("no object expected")):
            self.run_check(CHECK_MODE="write", CHECK_OBJECT="false")


if __name__ == "__main__":
    unittest.main()
