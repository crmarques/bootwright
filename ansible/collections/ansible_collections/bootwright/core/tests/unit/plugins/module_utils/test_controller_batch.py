"""A request's target tools are prepared together: every missing source is
acquired at once, bounded in all and per host, each by a fixed worker
interpreter under its own deadline, while the evidence stays one entry per
tool in input order, as the tools prepared one after another would leave it."""

from __future__ import annotations

import contextlib
import json
from pathlib import Path
import socket
import tempfile
import threading
import time
import types
import unittest
from unittest import mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_batch as batch,
)
from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)

EGRESS = {"httpProxy": "", "httpsProxy": "", "noProxy": []}
DEADLINE = 600


class Response:
    def __init__(self, data, pause=0.0):
        self.status = 200
        self.headers = {"Content-Length": str(len(data))}
        self.data, self.offset, self.pause = data, 0, pause

    def read(self, amount, decode_content):
        if self.pause:
            time.sleep(self.pause)
        block = self.data[self.offset:self.offset + amount]
        self.offset += len(block)
        return block

    def close(self):
        pass


def in_thread(request, seconds, cancel):
    """The worker run on this thread, its request and outcome crossing JSON as
    they cross the worker's pipes; a thread holds no alarm, so serve patches
    it out once for every thread."""
    del seconds, cancel
    return batch.revived(batch.worker(json.loads(json.dumps(request))))


@contextlib.contextmanager
def serve(bodies, pause=0.0):
    """Answer each URL with its own body, a fresh response for each request,
    to workers that run on threads of this process."""
    manager = types.SimpleNamespace(
        request=lambda method, url, **kwargs: Response(bodies[url], pause), clear=lambda: None
    )
    library = types.SimpleNamespace(
        PoolManager=lambda **kwargs: manager, Timeout=lambda **kwargs: kwargs
    )
    with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
        files, "trusted_roots", return_value=object()
    ), mock.patch.object(batch, "spawn", in_thread), mock.patch.object(
        files, "alarm", lambda seconds: contextlib.nullcontext()
    ):
        yield


def tool(data, kind, host, compatibility=""):
    prefix = "/".join(part for part in ("tools", kind, compatibility, "v1.0.0") if part)
    return {
        "kind": kind,
        "version": "v1.0.0",
        "compatibility": compatibility,
        "archive": "binary",
        "source": {
            "id": "tool-" + kind + "-" + "a" * 32 + "-v1.0.0",
            "url": "https://" + host + "/" + kind,
            "bytes": len(data),
            "sha256": files.sha256(data),
        },
        "files": [{"member": kind, "path": prefix + "/" + kind}],
    }


TOOLS = [
    (b"kubectl executable", "kubectl", "dl.example.test", ""),
    (b"virtctl executable", "virtctl", "github.example.test", "kubevirt"),
]


def definitions():
    return [tool(data, kind, host, compatibility) for data, kind, host, compatibility in TOOLS]


def bodies(tools, datas):
    return {definition["source"]["url"]: data for definition, data in zip(tools, datas)}


class Bundles(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="bootwright-batch-test-", dir="/tmp")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def bundle(self, name):
        root = self.root / name
        root.mkdir(mode=0o700)
        observed = root.stat()
        return root, {"path": str(root), "device": observed.st_dev, "inode": observed.st_ino,
                      "writable": True, "sealed": False}

    def tree(self, root):
        return {path.relative_to(root).as_posix(): path.read_bytes() for path in root.rglob("*") if path.is_file()}

    def test_the_batch_leaves_what_preparing_each_tool_in_turn_leaves(self):
        tools, datas = definitions(), [data for data, _kind, _host, _compatibility in TOOLS]
        serial_root, serial = self.bundle("serial")
        together_root, together = self.bundle("together")
        with serve(bodies(tools, datas)):
            expected = [files.prepare_tool(serial, definition, EGRESS, DEADLINE) for definition in tools]
            result = batch.prepare_tools(together, tools, EGRESS, [DEADLINE] * len(tools))
        self.assertEqual(result, {"changed": True, "evidence": [entry["evidence"] for entry in expected]})
        self.assertEqual([entry["source"] for entry in result["evidence"]], [definition["source"]["id"] for definition in tools])
        self.assertEqual(self.tree(together_root), self.tree(serial_root))
        with mock.patch.object(batch, "acquire_all", side_effect=AssertionError("a retained source was acquired")):
            replay = batch.prepare_tools(together, tools, EGRESS, [DEADLINE] * len(tools))
        self.assertEqual(replay, dict(result, changed=False))

    def test_a_refused_acquisition_names_its_own_tools_source_and_class(self):
        tools, datas = definitions(), [data for data, _kind, _host, _compatibility in TOOLS]
        datas[1] = b"kubectl executablX"
        _root, location = self.bundle("refused")
        with serve(bodies(tools, datas)), self.assertRaises(batch.ToolRefused) as refused:
            batch.prepare_tools(location, tools, EGRESS, [DEADLINE] * len(tools))
        self.assertEqual(refused.exception.source, tools[1]["source"]["id"])
        self.assertIsInstance(refused.exception.error, files.AcquisitionRefused)
        self.assertEqual(refused.exception.error.reason, "integrity")
        self.assertEqual(refused.exception.error.args, ("source integrity",))

    def test_a_check_refusal_precedes_every_acquisition_and_names_its_tool(self):
        tools = definitions()
        _root, location = self.bundle("checked")
        with mock.patch.object(batch, "acquire_all", side_effect=AssertionError("acquired")), \
                self.assertRaises(batch.ToolRefused) as refused:
            batch.prepare_tools(location, tools, EGRESS, [DEADLINE, 0])
        self.assertEqual(refused.exception.source, tools[1]["source"]["id"])
        self.assertEqual(refused.exception.error.args, ("acquisition deadline",))
        for name, arguments in (
            ("no tools", ([], [])),
            ("a deadline short", (tools, [DEADLINE])),
            ("one source twice", ([tools[0], tools[0]], [DEADLINE] * 2)),
        ):
            with self.subTest(name), self.assertRaises(files.Refused):
                batch.prepare_tools(location, *arguments[:1], EGRESS, arguments[1])


class Overlap:
    """Jobs that each wait until expected jobs run at once, recording the most
    jobs and the most of their own host seen running together."""

    def __init__(self, expected, wait=5):
        self.expected, self.wait, self.lock = expected, wait, threading.Lock()
        self.running, self.most, self.most_host, self.done = [], 0, 0, []

    def job(self, host, index):
        def work(cancel):
            with self.lock:
                self.running.append(host)
            until = time.monotonic() + self.wait
            while time.monotonic() < until and not cancel.is_set():
                with self.lock:
                    self.most = max(self.most, len(self.running))
                    self.most_host = max(self.most_host, self.running.count(host))
                    enough = self.most >= self.expected
                if enough and time.monotonic() > until - self.wait + 0.3:
                    break
                time.sleep(0.01)
            with self.lock:
                self.running.remove(host)
                self.done.append(index)
            return None if enough else files.Refused("no overlap")

        return work


class AcquireAll(unittest.TestCase):
    def test_acquisitions_run_at_once_within_the_bound_and_the_per_host_limit(self):
        hosts = ["a", "a", "a", "b", "b", "c"]
        overlap = Overlap(2)
        jobs = [(host, overlap.job(host, index)) for index, host in enumerate(hosts)]
        self.assertIsNone(batch.acquire_all(jobs, limit=4, per_host=2))
        self.assertEqual(sorted(overlap.done), list(range(len(hosts))))
        self.assertLessEqual(overlap.most, 4)
        self.assertGreaterEqual(overlap.most, 2)
        self.assertEqual(overlap.most_host, 2)
        self.assertEqual(overlap.running, [])

    def test_one_host_never_holds_more_than_its_share(self):
        overlap = Overlap(3, wait=0.5)
        jobs = [("a", overlap.job("a", index)) for index in range(3)]
        found = batch.acquire_all(jobs, limit=4, per_host=2)
        self.assertEqual(overlap.most_host, 2)
        self.assertEqual(found[0], 0)
        self.assertEqual(found[1].args, ("no overlap",))

    def test_the_first_refusal_in_input_order_wins_and_no_job_starts_after_one(self):
        started, joined = threading.Event(), threading.Event()

        def slow(cancel):
            time.sleep(0.3)
            joined.set()
            return files.AcquisitionRefused("refused", reason="dns", source="tool-a", detail="gaierror: x")

        def fast(cancel):
            raise files.Refused("source integrity")

        def late(cancel):
            started.set()

        index, refusal = batch.acquire_all([("a", slow), ("b", fast), ("c", late)], limit=2, per_host=2)
        self.assertEqual(index, 0)
        self.assertIsInstance(refusal, files.AcquisitionRefused)
        self.assertEqual((refusal.args, refusal.reason, refusal.source, refusal.detail),
                         (("refused",), "dns", "tool-a", "gaierror: x"))
        self.assertTrue(joined.is_set())
        self.assertFalse(started.is_set())

    def test_an_outcome_survives_its_handoff_as_the_refusal_it_was(self):
        for error in (files.Unreleased("openshift client release"), files.Refused("source size"),
                      files.AcquisitionRefused("source response", reason="status", detail="Refused: source response"),
                      KeyError("x")):
            with self.subTest(type(error).__name__):
                def work(error=error):
                    raise error

                revived = batch.revived(json.dumps(batch.settled(work)).encode())
                if isinstance(error, files.Refused):
                    self.assertIs(type(revived), type(error))
                    self.assertEqual(revived.args, error.args)
                    self.assertEqual(getattr(revived, "reason", None), getattr(error, "reason", None))
                    self.assertEqual(getattr(revived, "detail", None), getattr(error, "detail", None))
                else:
                    self.assertEqual(revived.args, ("acquisition process",))
        self.assertIsNone(batch.revived(json.dumps(batch.settled(lambda: None)).encode()))
        self.assertEqual(batch.revived(b"{").args, ("acquisition process",))
        self.assertEqual(batch.revived(b"[]").args, ("acquisition process",))


@contextlib.contextmanager
def silent_proxy():
    """A proxy that accepts every connection and never answers."""
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(8)
    accepted = []
    stop = threading.Event()

    def accept():
        listener.settimeout(0.2)
        while not stop.is_set():
            try:
                accepted.append(listener.accept()[0])
            except OSError:
                continue

    thread = threading.Thread(target=accept, daemon=True)
    thread.start()
    try:
        yield "http://127.0.0.1:%d" % listener.getsockname()[1]
    finally:
        stop.set()
        thread.join()
        for connection in accepted:
            connection.close()
        listener.close()


class Worker(unittest.TestCase):
    """The fixed worker interpreter, run through the bounded process runner."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="bootwright-worker-test-", dir="/tmp")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name) / "bundle"
        root.mkdir(mode=0o700)
        observed = root.stat()
        self.location = {"path": str(root), "device": observed.st_dev, "inode": observed.st_ino,
                         "writable": True, "sealed": False}
        self.source = tool(b"kubectl executable", "kubectl", "dl.example.test")["source"]

    def run_worker(self, egress, seconds):
        started = time.monotonic()
        refusal = batch.acquisition(self.location, self.source, egress, seconds, {"host": "", "file": ""})(threading.Event())
        return refusal, time.monotonic() - started

    def assert_deadline(self, refusal):
        self.assertIsInstance(refusal, files.AcquisitionRefused)
        self.assertEqual((refusal.args, refusal.reason, refusal.detail),
                         (("acquisition deadline",), "timeout", "Refused: acquisition deadline"))

    @pytest.mark.serial
    def test_a_hung_download_ends_at_its_own_deadline(self):
        if not Path("/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem").is_file():
            pytest.skip("no system trust store")
        with silent_proxy() as proxy:
            refusal, elapsed = self.run_worker(dict(EGRESS, httpsProxy=proxy), 1)
        self.assert_deadline(refusal)
        self.assertLess(elapsed, 1 + batch.GRACE)

    @pytest.mark.serial
    def test_a_worker_that_outlives_its_alarm_is_killed_with_the_same_refusal(self):
        hung = "import signal, time\nsignal.signal(signal.SIGALRM, signal.SIG_IGN)\ntime.sleep(60)\n"
        with mock.patch.object(batch, "WORKER", hung), mock.patch.object(batch, "GRACE", 1):
            refusal, elapsed = self.run_worker(EGRESS, 1)
        self.assert_deadline(refusal)
        self.assertLess(elapsed, 5)

    @pytest.mark.serial
    def test_a_worker_that_ends_without_an_outcome_is_refused(self):
        for source in ("import os\nos._exit(3)\n", "import sys\nsys.stdout.write('{')\n"):
            with self.subTest(source), mock.patch.object(batch, "WORKER", source):
                refusal, _elapsed = self.run_worker(EGRESS, 1)
            self.assertEqual(type(refusal), files.Refused)
            self.assertEqual(refusal.args, ("acquisition process",))

    @pytest.mark.serial
    def test_a_canceled_worker_stops_within_a_second(self):
        hung = "import time\ntime.sleep(60)\n"
        cancel = threading.Event()
        threading.Timer(0.5, cancel.set).start()
        started = time.monotonic()
        with mock.patch.object(batch, "WORKER", hung):
            batch.acquisition(self.location, self.source, EGRESS, 60, {"host": "", "file": ""})(cancel)
        self.assertLess(time.monotonic() - started, 3)


if __name__ == "__main__":
    unittest.main()
