"""Unit tests of the sample server's signer, request validation and stores."""

from __future__ import annotations

import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location(
    "e2e_sample_server", Path(__file__).with_name("sample-server.py")
)
sample = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sample)

ACCESS = "AKIAIOSFODNN7EXAMPLE"
SECRET = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
EMPTY_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"


def object_store(region="us-east-1"):
    s3 = {
        "host": "examplebucket.s3.amazonaws.com",
        "vip": "192.0.2.10",
        "bucket": "examplebucket",
        "region": region,
        "credentials": {"accessKey": ACCESS, "secretKey": SECRET},
        "caFile": "/unused",
    }
    with mock.patch.object(sample.ssl, "create_default_context"):
        return sample.ObjectStore(s3)


class SignatureTest(unittest.TestCase):
    """AWS's published Signature Version 4 examples for Amazon S3."""

    def test_get_object_reference_vector(self):
        headers = {
            "Host": "examplebucket.s3.amazonaws.com",
            "Range": "bytes=0-9",
            "x-amz-content-sha256": EMPTY_SHA256,
            "x-amz-date": "20130524T000000Z",
        }
        value = object_store().authorization("GET", "/test.txt", headers, EMPTY_SHA256, "20130524T000000Z")
        self.assertEqual(
            value,
            f"AWS4-HMAC-SHA256 Credential={ACCESS}/20130524/us-east-1/s3/aws4_request, "
            "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, "
            "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41",
        )

    def test_put_object_reference_vector(self):
        headers = {
            "Date": "Fri, 24 May 2013 00:00:00 GMT",
            "Host": "examplebucket.s3.amazonaws.com",
            "x-amz-content-sha256": "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072",
            "x-amz-date": "20130524T000000Z",
            "x-amz-storage-class": "REDUCED_REDUNDANCY",
        }
        value = object_store().authorization(
            "PUT", "/test%24file.text", headers, headers["x-amz-content-sha256"], "20130524T000000Z"
        )
        self.assertTrue(
            value.endswith("Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"), value
        )

    def test_the_signature_follows_the_request(self):
        store = object_store()
        headers = {"host": "h", "x-amz-date": "20130524T000000Z"}
        base = store.authorization("GET", "/a", headers, EMPTY_SHA256, "20130524T000000Z")
        self.assertNotEqual(base, store.authorization("PUT", "/a", headers, EMPTY_SHA256, "20130524T000000Z"))
        self.assertNotEqual(base, store.authorization("GET", "/b", headers, EMPTY_SHA256, "20130524T000000Z"))
        self.assertNotEqual(base, object_store("eu-west-1").authorization("GET", "/a", headers, EMPTY_SHA256, "20130524T000000Z"))
        self.assertNotEqual(base, store.authorization("GET", "/a", headers, "0" * 64, "20130524T000000Z"))


class Recorded(sample.Handler):
    """A handler that answers into a list instead of a socket."""

    def __init__(self, path, headers=None, body=b""):
        self.path = path
        self.headers = headers or {}
        self.rfile = io.BytesIO(body)
        self.replies = []

    def _reply(self, status, document=None, body=None):
        self.replies.append((status, document, body))


class MemoryStore:
    def __init__(self, files=None):
        self.files = dict(files or {})

    def put(self, name, body):
        self.files[name] = body

    def get(self, name):
        if name not in self.files:
            raise FileNotFoundError(name)
        return self.files[name]

    def delete(self, name):
        if name not in self.files:
            raise FileNotFoundError(name)
        del self.files[name]


class RequestTest(unittest.TestCase):
    def setUp(self):
        self.store = MemoryStore({"present": b"data"})
        patcher = mock.patch.object(sample.Handler, "stores", {"block": self.store, "file": MemoryStore()})
        patcher.start()
        self.addCleanup(patcher.stop)

    def target(self, path):
        request = Recorded(path)
        return request._target(), request.replies

    def test_a_store_and_a_valid_name_are_the_target(self):
        for path, want in {
            "/block/present": ("block", "present"),
            "/file/a.b_c-1": ("file", "a.b_c-1"),
            "/block/x?ignored=1": ("block", "x"),
            "/block/" + "n" * 64: ("block", "n" * 64),
        }.items():
            with self.subTest(path=path):
                self.assertEqual(self.target(path), (want, []))

    def test_every_other_path_is_refused_with_a_404(self):
        for path in [
            "/block",
            "/block/",
            "/block/a/b",
            "/object/name",
            "/other/name",
            "/block/.hidden",
            "/block/-leading",
            "/block/../escape",
            "/block/a%2Fb",
            "/block/has space",
            "/block/" + "n" * 65,
            "/",
        ]:
            with self.subTest(path=path):
                target, replies = self.target(path)
                self.assertEqual(target, (None, None))
                self.assertEqual([reply[0] for reply in replies], [404])

    def test_put_stores_the_body_and_reports_its_digest(self):
        request = Recorded("/block/new", {"Content-Length": "5"}, b"hello")
        request.do_PUT()
        self.assertEqual(self.store.files["new"], b"hello")
        status, document, _ = request.replies[0]
        self.assertEqual(status, 201)
        self.assertEqual(
            document,
            {
                "store": "block",
                "name": "new",
                "bytes": 5,
                "sha256": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
            },
        )

    def test_put_refuses_a_missing_invalid_negative_or_oversized_length(self):
        for headers in [{}, {"Content-Length": "x"}, {"Content-Length": "-1"}, {"Content-Length": str(sample.MAX_BODY + 1)}]:
            with self.subTest(headers=headers):
                request = Recorded("/block/new", headers, b"data")
                request.do_PUT()
                self.assertEqual([reply[0] for reply in request.replies], [413])
                self.assertNotIn("new", self.store.files)

    def test_put_accepts_the_largest_body_length_and_an_empty_one(self):
        for length in (0, sample.MAX_BODY):
            with self.subTest(length=length):
                request = Recorded("/block/edge", {"Content-Length": str(length)}, b"x" * length)
                request.do_PUT()
                self.assertEqual(request.replies[0][0], 201)
                self.assertEqual(len(self.store.files["edge"]), length)

    def test_get_and_delete_answer_what_the_store_holds(self):
        request = Recorded("/block/present")
        request.do_GET()
        self.assertEqual(request.replies, [(200, None, b"data")])
        request = Recorded("/block/absent")
        request.do_GET()
        self.assertEqual([reply[0] for reply in request.replies], [404])
        request = Recorded("/block/present")
        request.do_DELETE()
        self.assertEqual(request.replies[0][:2], (200, {"store": "block", "name": "present", "deleted": True}))
        request = Recorded("/block/present")
        request.do_DELETE()
        self.assertEqual([reply[0] for reply in request.replies], [404])

    def test_a_failing_store_answers_502_never_a_false_success(self):
        class Failing(MemoryStore):
            def put(self, name, body):
                raise OSError("disk full")

            def get(self, name):
                raise OSError("io error")

            def delete(self, name):
                raise OSError("io error")

        with mock.patch.object(sample.Handler, "stores", {"block": Failing()}):
            for method, request in [
                ("do_PUT", Recorded("/block/n", {"Content-Length": "1"}, b"x")),
                ("do_GET", Recorded("/block/n")),
                ("do_DELETE", Recorded("/block/n")),
            ]:
                with self.subTest(method=method):
                    getattr(request, method)()
                    self.assertEqual([reply[0] for reply in request.replies], [502])

    def test_health_is_503_unless_every_store_is_ready(self):
        class Ready:
            def __init__(self, value):
                self.value = value

            def ready(self):
                return self.value

        config = {"monitors": ["192.0.2.1"], "s3": {"vip": "192.0.2.2"}, "listen": "0.0.0.0", "port": 8080}
        for ready, status in [((True, True), 200), ((True, False), 503)]:
            with self.subTest(ready=ready), mock.patch.object(sample.Handler, "config", config), mock.patch.object(
                sample.Handler, "stores", {"block": Ready(ready[0]), "file": Ready(ready[1])}
            ), mock.patch.object(sample, "source_address", return_value="192.0.2.9"):
                request = Recorded("/healthz")
                request.do_GET()
                self.assertEqual(request.replies[0][0], status)
                self.assertEqual(request.replies[0][1]["sourceAddress"]["mon"], {"192.0.2.1": "192.0.2.9"})


class DirectoryStoreTest(unittest.TestCase):
    def test_a_write_is_whole_replaces_and_leaves_no_partial_file(self):
        with tempfile.TemporaryDirectory() as root:
            store = sample.DirectoryStore(root)
            store.put("a", b"first")
            store.put("a", b"second")
            self.assertEqual(store.get("a"), b"second")
            self.assertEqual(sorted(path.name for path in Path(root).iterdir()), ["a"])
            store.delete("a")
            with self.assertRaises(FileNotFoundError):
                store.get("a")
            with self.assertRaises(FileNotFoundError):
                store.delete("a")

    def test_it_is_ready_only_on_a_mount_point(self):
        with tempfile.TemporaryDirectory() as root:
            store = sample.DirectoryStore(root)
            with mock.patch.object(sample.os.path, "ismount", return_value=False):
                self.assertFalse(store.ready())
            with mock.patch.object(sample.os.path, "ismount", return_value=True):
                self.assertTrue(store.ready())


class ConfigTest(unittest.TestCase):
    def test_the_credentials_file_joins_the_s3_section(self):
        with tempfile.TemporaryDirectory() as root:
            credentials = Path(root, "credentials.json")
            credentials.write_text(json.dumps({"accessKey": "a", "secretKey": "s"}))
            config = Path(root, "config.json")
            config.write_text(json.dumps({"s3": {"credentialsFile": str(credentials)}}))
            self.assertEqual(
                sample.load_config(str(config))["s3"]["credentials"], {"accessKey": "a", "secretKey": "s"}
            )


if __name__ == "__main__":
    unittest.main()
