from __future__ import annotations

import contextlib
import errno
import hashlib
import io
import os
from pathlib import Path
import socket
import ssl
import tarfile
import tempfile
import tracemalloc
import types
import unittest
from unittest import mock

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)

EGRESS = {"httpProxy": "", "httpsProxy": "", "noProxy": []}
DEADLINE = 600
DOWNLOAD = 300
RELEASE = "4.20.8"


def stamped(version=RELEASE, body=b"client executable"):
    """An oc body naming its release the way an operator observed a published
    one to: openshift-client-linux-amd64-rhel9-4.21.11.tar.gz (sha256
    92a17002cafdd5513abcea27ff497f5e1c8d1532ddc6f2551bdd1b1ca31a16b1) holds an oc
    with exactly one b"4.21.11\\x00" + marker[8:], no whole marker, one bare
    marker head followed by other bytes and one "!"-led copy of the marker."""
    head = files.RELEASE_MARKER[:28]
    return (
        body
        + head
        + b"other bytes"
        + b"!"
        + files.RELEASE_MARKER[1:]
        + version.encode()
        + b"\x00"
        + files.RELEASE_MARKER[len(version) + 1:]
        + b"tail"
    )


class Response:
    """A 200 response whose body is served in reads, never held whole."""

    def __init__(self, size, produce, advance=None, log=None):
        self.status = 200
        self.headers = {"Content-Length": str(size)}
        self.size, self.offset = size, 0
        self.produce, self.advance, self.log = produce, advance, log

    def read(self, amount, decode_content):
        block = self.produce(self.offset, min(amount, self.size - self.offset))
        self.offset += len(block)
        if self.log is not None:
            self.log.append(("read", amount, len(block)))
        if self.advance is not None:
            self.advance()
        return block

    def close(self):
        pass


def body(data, **kwargs):
    return Response(len(data), lambda offset, amount: data[offset:offset + amount], **kwargs)


@contextlib.contextmanager
def serve(response):
    manager = types.SimpleNamespace(
        request=lambda *args, **kwargs: response, clear=lambda: None
    )
    library = types.SimpleNamespace(
        PoolManager=lambda **kwargs: manager, Timeout=lambda **kwargs: kwargs
    )
    with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
        files, "trusted_roots", return_value=object()
    ):
        yield


def refuse_acquisition(reason):
    return mock.patch.object(files, "acquire", side_effect=AssertionError(reason))


def tool(data, kind="kubectl", compatibility="", version="v1.36.1"):
    members = ["oc", "kubectl"] if kind == "openshift-clients" else [kind]
    prefix = "/".join(part for part in ("tools", kind, compatibility, version) if part)
    return {
        "kind": kind,
        "version": version,
        "compatibility": compatibility,
        "archive": "tar.gz" if kind == "openshift-clients" else "binary",
        "source": {
            "id": "tool-" + kind + "-" + "a" * 32 + "-" + version,
            "url": "https://downloads.example.test/exact",
            "bytes": len(data),
            "sha256": files.sha256(data),
        },
        "files": [
            {"member": member, "path": prefix + "/" + member} for member in members
        ],
    }


def archive(members):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w:gz") as writer:
        for name, data, kind in members:
            member = tarfile.TarInfo(name)
            member.mode = 0o755
            if kind == "file":
                member.size = len(data)
                writer.addfile(member, io.BytesIO(data))
            else:
                member.type = tarfile.SYMTYPE if kind == "symlink" else tarfile.LNKTYPE
                member.linkname = data
                writer.addfile(member)
    return stream.getvalue()


class ControllerFilesTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(
            prefix="bootwright-tool-test-", dir="/tmp"
        )
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        observed = self.root.stat()
        self.location = {
            "path": str(self.root),
            "device": observed.st_dev,
            "inode": observed.st_ino,
            "writable": True,
            "sealed": False,
        }

    def retain(self, definition, data, root=None):
        sources = (root or self.root) / "sources"
        sources.mkdir(mode=0o700, exist_ok=True)
        retained = sources / definition["source"]["id"]
        retained.write_bytes(data)
        retained.chmod(0o600)

    def left(self, root=None):
        """Every entry under root, directories included, so an empty directory
        a refusal leaves behind is seen."""
        root = root or self.root
        return sorted(path.relative_to(root).as_posix() for path in root.rglob("*"))

    def retained_only(self, definition):
        return ["sources", "sources/" + definition["source"]["id"]]

    def sink(self):
        """A private scratch file a download streams into."""
        descriptor, path = tempfile.mkstemp(prefix="bootwright-download-", dir="/tmp")
        self.addCleanup(os.unlink, path)
        self.addCleanup(os.close, descriptor)
        return descriptor, Path(path)

    def another_bundle(self, name):
        root = self.root / name
        root.mkdir(mode=0o700)
        observed = root.stat()
        location = dict(
            self.location, path=str(root), device=observed.st_dev, inode=observed.st_ino
        )
        return root, location

    def test_first_publication_replay_and_partial_recovery(self):
        data = b"qualified executable"
        definition = tool(data)
        with serve(body(data)):
            first = files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertTrue(first["changed"])
        paths = {
            path.relative_to(self.root).as_posix(): path.stat().st_ino
            for path in self.root.rglob("*")
            if path.is_file()
        }
        with refuse_acquisition("retained replay acquired source"):
            second = files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
            self.assertFalse(second["changed"])
            self.assertEqual(first["evidence"], second["evidence"])
            self.assertEqual(
                paths,
                {
                    path.relative_to(self.root).as_posix(): path.stat().st_ino
                    for path in self.root.rglob("*")
                    if path.is_file()
                },
            )
            executable = self.root / definition["files"][0]["path"]
            executable.unlink()
            with self.assertRaises(files.Refused):
                files.prepare_tool(
                    self.location, definition, EGRESS, DEADLINE, inspect_only=True
                )
            recovered = files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
            self.assertTrue(recovered["changed"])
            self.assertEqual(executable.read_bytes(), data)
        self.assertEqual(
            (self.root / ("sources/" + definition["source"]["id"])).stat().st_mode
            & 0o7777,
            0o600,
        )
        self.assertEqual(executable.stat().st_mode & 0o7777, 0o700)

    def test_interrupted_atomic_source_write_leaves_no_named_partial_file(self):
        definition = tool(b"complete source")
        with serve(body(b"complete source")):
            with mock.patch.object(
                files.os, "write", side_effect=OSError("injected interruption")
            ):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertEqual(self.left(), [])

    def test_sealed_missing_target_is_never_refilled(self):
        definition = tool(b"complete source")
        with serve(body(b"complete source")):
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        executable = self.root / definition["files"][0]["path"]
        executable.unlink()
        for writable, sealed in ((False, True), (True, True), (False, False)):
            location = dict(self.location, writable=writable, sealed=sealed)
            with refuse_acquisition("sealed download"):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(location, definition, EGRESS, DEADLINE)
            self.assertFalse(executable.exists())

    def test_unattributed_target_and_changed_existing_target_are_refused(self):
        definition = tool(b"complete source")
        target = self.root / definition["files"][0]["path"]
        target.parent.mkdir(parents=True, mode=0o700)
        for directory in target.parents:
            if directory == self.root:
                break
            directory.chmod(0o700)
        target.write_bytes(b"complete source")
        target.chmod(0o700)
        with refuse_acquisition("unattributed source acquisition"):
            with self.assertRaises(files.Refused):
                files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        target.unlink()
        with serve(body(b"complete source")):
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        target.write_bytes(b"changed content")
        with self.assertRaises(files.Refused) as refused:
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertEqual(str(refused.exception), "existing bundle file differs")
        self.assertEqual(target.read_bytes(), b"changed content")

    def test_directory_substitution_and_symlink_escape_are_refused(self):
        wrong = dict(self.location, inode=self.location["inode"] + 1)
        with self.assertRaises(files.Refused):
            files.Bundle(wrong)
        definition = tool(b"bytes")
        (self.root / "sources").symlink_to("/tmp", target_is_directory=True)
        with refuse_acquisition("unsafe acquisition"):
            with self.assertRaises(OSError):
                files.prepare_tool(self.location, definition, EGRESS, DEADLINE)

    def test_selected_archive_alias_is_flattened_and_unsafe_members_refused(self):
        data = archive([("oc", stamped(), "file"), ("kubectl", "oc", "symlink")])
        definition = tool(data, "openshift-clients", "openshift", RELEASE)
        self.retain(definition, data)
        with refuse_acquisition("retained source acquired"):
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        for file in definition["files"]:
            self.assertEqual((self.root / file["path"]).read_bytes(), stamped())
        for members in (
            [("oc", stamped(), "file"), ("kubectl", "../oc", "symlink")],
            [
                ("oc", stamped(), "file"),
                ("kubectl", "outside", "symlink"),
                ("outside", b"ok", "file"),
            ],
            [
                ("oc", stamped(), "file"),
                ("oc", stamped(body=b"again"), "file"),
                ("kubectl", "oc", "hardlink"),
            ],
            [("oc", "kubectl", "symlink"), ("kubectl", "oc", "symlink")],
            [("../outside", b"bad", "file")],
        ):
            with self.subTest(members=[member[0] for member in members]):
                root, location = self.another_bundle(str(len(list(self.root.iterdir()))))
                data = archive(members)
                definition = tool(data, "openshift-clients", "openshift", RELEASE)
                self.retain(definition, data, root)
                with self.assertRaises(files.Refused):
                    files.prepare_tool(location, definition, EGRESS, DEADLINE)
                self.assertEqual(self.left(root), self.retained_only(definition))

    def test_invalid_projection_and_integrity_fail_before_download(self):
        for change in (
            lambda value: value["files"][0].update(path="../outside"),
            lambda value: value["source"].update(bytes=files.MAX_SOURCE + 1),
            lambda value: value.update(compatibility="openshift-virtualization"),
            lambda value: value["source"].update(
                url="https://user:password@example.test/file"
            ),
        ):
            definition = tool(b"bytes")
            change(definition)
            with refuse_acquisition("invalid input read network"):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        for deadline in (0, True, files.MAX_DEADLINE + 1, "600"):
            with refuse_acquisition("an invalid deadline read network"):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, tool(b"bytes"), EGRESS, deadline)
        self.assertEqual(list(self.root.iterdir()), [])
        with serve(body(b"wrong")):
            with self.assertRaises(files.Refused):
                files.prepare_tool(self.location, tool(b"bytes"), EGRESS, DEADLINE)
        self.assertEqual(self.left(), [])

    def test_frozen_tool_requires_exact_stable_release(self):
        for version in ("latest", "v1.2.3-rc.1", "1.02.3", "1.2.3+build"):
            with self.subTest(version=version):
                definition = tool(b"bytes", "virtctl", "kubevirt", version)
                with refuse_acquisition("invalid release read network"):
                    with self.assertRaises(files.Refused):
                        files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_proxy_bypass_never_uses_ambient_routing(self):
        target = files.endpoint("https://downloads.example.test/file")
        egress = {
            "httpProxy": "",
            "httpsProxy": "https://proxy.example.test:3128",
            "noProxy": [],
        }
        with mock.patch.dict(
            os.environ, {"HTTPS_PROXY": "http://attacker.example.test", "NO_PROXY": "*"}
        ):
            self.assertEqual(files.proxy_for(egress, target), egress["httpsProxy"])
            self.assertIsNone(files.proxy_for(EGRESS, target))
            for rule in (
                "example.test",
                ".example.test",
                "*.example.test",
                "downloads.example.test:443",
                "*",
            ):
                self.assertIsNone(files.proxy_for(dict(egress, noProxy=[rule]), target))
            self.assertEqual(
                files.proxy_for(
                    dict(egress, noProxy=["downloads.example.test:444"]), target
                ),
                egress["httpsProxy"],
            )
            with self.assertRaises(files.Refused):
                files.proxy_for(dict(egress, noProxy=["*", "https://invalid"]), target)
        ip = files.endpoint("https://192.0.2.8/file")
        self.assertIsNone(files.proxy_for(dict(egress, noProxy=["192.0.2.0/24"]), ip))

    def test_download_freezes_source_bytes_and_uses_explicit_tls_proxy(self):
        body = b"publisher bytes"
        source = tool(body)["source"]
        observed = []
        response = types.SimpleNamespace(
            status=200, headers={"Content-Length": str(len(body))}, close=lambda: None
        )
        stream = io.BytesIO(body)
        response.read = lambda amount, decode_content: stream.read(amount)

        class Manager:
            def __init__(self, *args, **kwargs):
                observed.append((args, kwargs))

            def request(self, method, endpoint, **kwargs):
                observed.append((method, endpoint, kwargs))
                return response

            def clear(self):
                pass

        library = types.SimpleNamespace(
            PoolManager=Manager, ProxyManager=Manager, Timeout=lambda **kwargs: kwargs
        )
        trust = object()
        egress = dict(EGRESS, httpsProxy="https://proxy.example.test:3128")
        with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
            files, "trusted_roots", return_value=trust
        ):
            with mock.patch.dict(
                os.environ, HTTPS_PROXY="http://attacker.example.test", NO_PROXY="*"
            ):
                descriptor, path = self.sink()
                files.download(source, egress, descriptor, DOWNLOAD, files.Routes())
                self.assertEqual(path.read_bytes(), body)
        self.assertEqual(observed[0][0], (egress["httpsProxy"],))
        self.assertIs(observed[0][1]["ssl_context"], trust)
        self.assertIs(observed[0][1]["proxy_ssl_context"], trust)
        self.assertFalse(observed[1][2]["redirect"])
        self.assertFalse(observed[1][2]["preload_content"])

    def test_download_refuses_unapproved_redirects_bounds_and_raw_errors(self):
        source = tool(b"publisher bytes")["source"]
        for status, headers, body in (
            (302, {"Location": "https://attacker.example.test/file"}, b""),
            (302, {"Location": "https://user:password@github.com/file"}, b""),
            (200, {"Content-Length": "999"}, b""),
            (200, {"Content-Encoding": "gzip"}, b""),
            (200, {}, b"changed bytes!!"),
            (200, {}, b"publisher bytes extra"),
        ):
            stream = io.BytesIO(body)
            response = types.SimpleNamespace(
                status=status,
                headers=headers,
                close=lambda: None,
                read=lambda amount, decode_content: stream.read(amount),
            )
            manager = types.SimpleNamespace(
                request=lambda *args, **kwargs: response, clear=lambda: None
            )
            library = types.SimpleNamespace(
                PoolManager=lambda **kwargs: manager, Timeout=lambda **kwargs: kwargs
            )
            with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
                files, "trusted_roots", return_value=object()
            ):
                with self.assertRaises(files.Refused) as error:
                    files.download(source, EGRESS, self.sink()[0], DOWNLOAD, files.Routes())
            self.assertEqual(
                str(error.exception),
                "approved source acquisition was refused or incomplete",
            )
        with mock.patch.object(
            files, "_download", side_effect=OSError("sensitive source or proxy detail")
        ):
            with self.assertRaises(files.Refused) as error:
                files.download(source, EGRESS, self.sink()[0], DOWNLOAD, files.Routes())
        self.assertNotIn("sensitive", str(error.exception))

    def test_bundle_and_archive_capacity_bounds_precede_publication(self):
        definition = tool(b"complete source")
        with serve(body(b"complete source")):
            with mock.patch.object(files, "MAX_BUNDLE", 1):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertEqual(list(self.root.iterdir()), [])
        data = archive([("oc", stamped(), "file"), ("kubectl", "oc", "symlink")])
        definition = tool(data, "openshift-clients", "openshift", RELEASE)
        self.retain(definition, data)
        for bound, value in (("MAX_ENTRIES", 1), ("MAX_EXPANDED", 512)):
            with mock.patch.object(files, bound, value):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
            self.assertEqual(self.left(), self.retained_only(definition))

    def test_each_acquired_chunk_is_written_before_the_next_read(self):
        data = bytes(range(256)) * (5 * files.CHUNK // 256) + b"tail"
        definition = tool(data)
        log = []
        write = os.write

        def recorded(descriptor, block):
            log.append(("write", len(block)))
            return write(descriptor, block)

        with serve(body(data, log=log)), mock.patch.object(files.os, "write", recorded):
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        self.assertTrue(all(entry[1] <= files.CHUNK for entry in log))
        # The response's last read ends the acquisition; later writes project
        # the retained source.
        last = max(index for index, entry in enumerate(log) if entry[0] == "read")
        acquisition = log[: last + 1]
        expected = []
        for entry in acquisition:
            if entry[0] == "read":
                expected += ["read", "write"] if entry[2] else ["read"]
        self.assertEqual([entry[0] for entry in acquisition], expected)
        self.assertEqual(
            sum(entry[1] for entry in acquisition if entry[0] == "write"), len(data)
        )
        self.assertEqual((self.root / definition["files"][0]["path"]).read_bytes(), data)

    def test_neither_a_source_nor_a_member_is_ever_held_whole(self):
        pattern = hashlib.sha256(b"block").digest() * (files.CHUNK // 32)
        size = 16 << 20
        digest = hashlib.sha256()
        for _index in range(size // len(pattern)):
            digest.update(pattern)
        definition = tool(b"")
        definition["source"].update(bytes=size, sha256=digest.hexdigest())

        def streamed():
            return Response(
                size, lambda offset, amount: pattern[offset % len(pattern):][:amount]
            )

        descriptor, package = self.sink()
        oc = stamped(body=pattern * (size // len(pattern)))
        data = archive([("oc", oc, "file"), ("kubectl", "oc", "symlink")])
        del oc
        clients = tool(data, "openshift-clients", "openshift", RELEASE)
        root, location = self.another_bundle("clients")
        self.retain(clients, data, root)
        del data
        for name, prepare in (
            ("acquisition", lambda: files.prepare_tool(self.location, definition, EGRESS, DEADLINE)),
            ("projection", lambda: files.prepare_tool(location, clients, EGRESS, DEADLINE)),
            ("native package", lambda: files.download(definition["source"], EGRESS, descriptor, DOWNLOAD, files.Routes())),
        ):
            with self.subTest(name), serve(streamed()):
                tracemalloc.start()
                try:
                    prepare()
                    peak = tracemalloc.get_traced_memory()[1]
                finally:
                    tracemalloc.stop()
                self.assertLess(peak, 2 << 20)
        self.assertEqual((root / clients["files"][1]["path"]).stat().st_size, size + len(stamped(body=b"")))
        self.assertEqual(package.stat().st_size, size)

    def test_a_source_deadline_replaces_the_fixed_one(self):
        data = bytes(8 * files.CHUNK)
        now = [1000.0]

        def advance():
            now[0] += 100

        with mock.patch.object(files.time, "monotonic", lambda: now[0]):
            with serve(body(data, advance=advance)):
                files.prepare_tool(self.location, tool(data), EGRESS, 1000)
            root, location = self.another_bundle("short")
            with serve(body(data, advance=advance)):
                with self.assertRaises(files.Refused) as refused:
                    files.prepare_tool(location, tool(data), EGRESS, 10)
            self.assertEqual(str(refused.exception), "acquisition deadline")
            self.assertEqual(refused.exception.reason, "timeout")
            self.assertEqual(self.left(root), [])
            with serve(body(data, advance=advance)):
                with self.assertRaises(files.Refused):
                    files.download(tool(data)["source"], EGRESS, self.sink()[0], DOWNLOAD, files.Routes())
        self.assertEqual(
            (self.root / tool(data)["files"][0]["path"]).read_bytes(), data
        )

    def test_the_alarm_takes_the_source_deadline_and_keeps_a_sooner_prior_one(self):
        # No real timer is armed: setitimer only records, and the clock moves
        # 10 seconds per response read, so a prior alarm's re-arm is exact.
        data = bytes(3 * files.CHUNK)
        handler = files.signal.getsignal(files.signal.SIGALRM)
        for prior, tool_armed, download_armed in (
            (0.0, 1000, DOWNLOAD),
            (500.0, 500.0, DOWNLOAD),
            (50.0, 50.0, 50.0),
        ):
            now, armed = [1000.0], []

            def advance():
                now[0] += 10

            def record(which, seconds, interval=0.0):
                armed.append((which, seconds, interval, now[0]))

            root, location = self.another_bundle("alarm-%d" % prior)
            with self.subTest(prior=prior), mock.patch.object(
                files.time, "monotonic", lambda: now[0]
            ), mock.patch.object(
                files.signal, "getitimer", return_value=(prior, 0.0)
            ), mock.patch.object(files.signal, "setitimer", record):
                for path, expected in (
                    (lambda: files.prepare_tool(location, tool(data), EGRESS, 1000), tool_armed),
                    (lambda: files.download(tool(data)["source"], EGRESS, self.sink()[0], DOWNLOAD, files.Routes()), download_armed),
                ):
                    del armed[:]
                    with serve(body(data, advance=advance)):
                        path()
                    real = files.signal.ITIMER_REAL
                    self.assertEqual(armed[0][:2], (real, expected))
                    self.assertEqual(armed[1][:2], (real, 0))
                    if prior:
                        elapsed = armed[2][3] - armed[0][3]
                        self.assertGreater(elapsed, 0)
                        self.assertEqual(armed[2][:3], (real, prior - elapsed, 0.0))
                    self.assertEqual(len(armed), 3 if prior else 2)
                    self.assertIs(files.signal.getsignal(files.signal.SIGALRM), handler)
            self.assertEqual(
                (root / tool(data)["files"][0]["path"]).read_bytes(), data
            )

    def test_the_stamp_scanner_counts_each_split_occurrence_once(self):
        stamp = RELEASE.encode() + b"\x00" + files.RELEASE_MARKER[len(RELEASE) + 1:]
        for pattern, stamped_count, unstamped_count in (
            (stamp, 1, 0),
            (files.RELEASE_MARKER, 0, 1),
        ):
            data = b"head" + pattern + b"tail"
            for offset in range(1, len(data)):
                for chunks in (
                    (data[:offset], data[offset:]),
                    (data[:offset], b"", data[offset:offset + 1], data[offset + 1:]),
                ):
                    scanner = files.ReleaseStamp(RELEASE)
                    for chunk in chunks:
                        scanner.update(chunk)
                    self.assertEqual(
                        (scanner.stamped.count, scanner.unstamped.count),
                        (stamped_count, unstamped_count),
                        "split at %d" % offset,
                    )
            scanner = files.ReleaseStamp(RELEASE)
            for index in range(len(data)):
                scanner.update(data[index:index + 1])
            self.assertEqual(
                (scanner.stamped.count, scanner.unstamped.count),
                (stamped_count, unstamped_count),
            )
        scanner = files.ReleaseStamp(RELEASE)
        scanner.update(stamped())
        scanner.verify()
        for content in (
            b"no release" + files.RELEASE_MARKER[28:],
            stamped() + files.RELEASE_MARKER,
            stamped(version="4.20.9"),
            stamped() + stamped(),
        ):
            scanner = files.ReleaseStamp(RELEASE)
            scanner.update(content)
            with self.assertRaises(files.Refused):
                scanner.verify()
        with self.assertRaises(files.Refused):
            files.ReleaseStamp("4" * 92)
        files.ReleaseStamp("4" * 91)

    def test_a_stamped_oc_publishes_both_clients_however_kubectl_is_archived(self):
        oc, kubectl = stamped(), b"separate kubectl executable"
        for kind, members, published in (
            ("regular", [("oc", oc, "file"), ("kubectl", kubectl, "file")], kubectl),
            ("hard link", [("oc", oc, "file"), ("kubectl", "oc", "hardlink")], oc),
            ("symlink", [("kubectl", "oc", "symlink"), ("oc", oc, "file")], oc),
        ):
            with self.subTest(kind):
                root, location = self.another_bundle(kind.replace(" ", "-"))
                data = archive(members)
                definition = tool(data, "openshift-clients", "openshift", RELEASE)
                self.retain(definition, data, root)
                result = files.prepare_tool(location, definition, EGRESS, DEADLINE)
                paths = {file["member"]: root / file["path"] for file in definition["files"]}
                self.assertEqual(paths["oc"].read_bytes(), oc)
                self.assertEqual(paths["kubectl"].read_bytes(), published)
                for path in paths.values():
                    self.assertEqual((path.stat().st_nlink, path.stat().st_mode & 0o7777), (1, 0o700))
                manifest = [
                    {"path": file["path"], "sha256": files.sha256(content), "bytes": len(content)}
                    for file, content in sorted(
                        zip(definition["files"], (oc, published)),
                        key=lambda pair: pair[0]["path"],
                    )
                ]
                self.assertTrue(result["changed"])
                self.assertEqual(result["evidence"]["files"], files.sha256(files.canonical(manifest)))
                replay = files.prepare_tool(location, definition, EGRESS, DEADLINE, inspect_only=True)
                self.assertEqual(replay, dict(result, changed=False))

    def test_an_unproved_oc_release_publishes_neither_client(self):
        for name, oc in (
            ("unstamped", b"client executable" + files.RELEASE_MARKER),
            ("another release", stamped(version="4.20.9")),
            ("stamped twice", stamped() + stamped()),
        ):
            with self.subTest(name):
                root, location = self.another_bundle(name.replace(" ", "-"))
                data = archive([("oc", oc, "file"), ("kubectl", "oc", "hardlink")])
                definition = tool(data, "openshift-clients", "openshift", RELEASE)
                with serve(body(data)):
                    with self.assertRaises(files.Refused) as refused:
                        files.prepare_tool(location, definition, EGRESS, DEADLINE)
                self.assertEqual(str(refused.exception), "openshift client release")
                self.assertEqual(self.left(root), self.retained_only(definition))

    def test_inspection_refuses_a_changed_member_and_an_unstamped_retained_oc(self):
        data = archive([("oc", stamped(), "file"), ("kubectl", "oc", "symlink")])
        definition = tool(data, "openshift-clients", "openshift", RELEASE)
        self.retain(definition, data)
        files.prepare_tool(self.location, definition, EGRESS, DEADLINE)
        oc = self.root / definition["files"][0]["path"]
        oc.write_bytes(stamped(body=b"client executablf"))
        with self.assertRaises(files.Refused) as refused:
            files.prepare_tool(self.location, definition, EGRESS, DEADLINE, inspect_only=True)
        self.assertEqual(str(refused.exception), "target executable postcondition")
        unstamped = b"client executable" + b"\x00" * len(stamped(body=b""))
        root, location = self.another_bundle("unstamped")
        data = archive([("oc", unstamped, "file"), ("kubectl", "oc", "symlink")])
        definition = tool(data, "openshift-clients", "openshift", RELEASE)
        self.retain(definition, data, root)
        for file in definition["files"]:
            target = root / file["path"]
            target.parent.mkdir(parents=True, exist_ok=True)
            for directory in target.relative_to(root).parents:
                (root / directory).chmod(0o700)
            target.write_bytes(unstamped)
            target.chmod(0o700)
        with self.assertRaises(files.Refused) as refused:
            files.prepare_tool(location, definition, EGRESS, DEADLINE, inspect_only=True)
        self.assertEqual(str(refused.exception), "openshift client release")


def trust_root(parent, bundle, mode=0o644):
    """A root holding etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem laid out
    as update-ca-trust lays it out, with bundle as its content."""
    directory = Path(parent)
    for name in ("etc", "pki", "ca-trust", "extracted", "pem"):
        directory = directory / name
        directory.mkdir()
        directory.chmod(0o755)
    target = directory / "tls-ca-bundle.pem"
    target.write_bytes(bundle)
    target.chmod(mode)
    return str(parent)


FIXTURE = Path(__file__).resolve().parent / "fixtures" / "tls-ca-bundle-utf8.pem"


class TrustedRoots(unittest.TestCase):
    """The system trust store loads its certificate blocks whatever its labels
    hold, unmocked: update-ca-trust writes a '# <label>' line before each,
    and a label may hold UTF-8 (fixture copied from Fedora 43's
    /etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem)."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="bootwright-trust-test-", dir="/tmp")
        self.addCleanup(temporary.cleanup)
        self.parent = temporary.name

    def trusted(self, bundle, mode=0o644):
        root = trust_root(self.parent, bundle, mode)
        with mock.patch.object(files, "TRUST_ROOT", root), mock.patch.object(
            files, "TRUST_OWNER", os.geteuid()
        ):
            return files.trusted_roots()

    def test_trusted_roots_loads_a_bundle_with_a_utf8_label(self):
        bundle = FIXTURE.read_bytes()
        self.assertIn("Főtanúsítvány".encode("utf-8"), bundle)
        context = self.trusted(bundle)
        self.assertIsInstance(context, ssl.SSLContext)
        self.assertEqual(context.cert_store_stats()["x509_ca"], 2)

    def test_trusted_roots_refuses_a_bundle_without_certificates(self):
        with self.assertRaises(files.Refused) as refused:
            self.trusted(b"# ISRG Root X1\n# only labels\n")
        self.assertEqual(str(refused.exception), "system TLS trust")

    def test_trusted_roots_refuses_a_group_writable_bundle(self):
        with self.assertRaises(files.Refused) as refused:
            self.trusted(FIXTURE.read_bytes(), mode=0o664)
        self.assertEqual(str(refused.exception), "system TLS trust")

    def test_trusted_roots_refuses_another_owner(self):
        trust_root(self.parent, FIXTURE.read_bytes())
        with mock.patch.object(files, "TRUST_ROOT", self.parent), mock.patch.object(
            files, "TRUST_OWNER", os.geteuid() + 1
        ):
            with self.assertRaises(files.Refused):
                files.trusted_roots()

    def test_download_uses_the_loaded_system_trust(self):
        data = b"publisher bytes"
        observed = []
        stream = io.BytesIO(data)
        response = types.SimpleNamespace(
            status=200,
            headers={"Content-Length": str(len(data))},
            close=lambda: None,
            read=lambda amount, decode_content: stream.read(amount),
        )
        manager = types.SimpleNamespace(request=lambda *args, **kwargs: response, clear=lambda: None)

        def pool(**kwargs):
            observed.append(kwargs)
            return manager

        library = types.SimpleNamespace(PoolManager=pool, Timeout=lambda **kwargs: kwargs)
        root = trust_root(self.parent, FIXTURE.read_bytes())
        descriptor, path = tempfile.mkstemp(prefix="bootwright-download-", dir="/tmp")
        self.addCleanup(os.unlink, path)
        self.addCleanup(os.close, descriptor)
        with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
            files, "TRUST_ROOT", root
        ), mock.patch.object(files, "TRUST_OWNER", os.geteuid()):
            with files.Routes() as routes:
                files.download(tool(data)["source"], EGRESS, descriptor, DOWNLOAD, routes)
        self.assertEqual(Path(path).read_bytes(), data)
        self.assertEqual(len(observed), 1)
        context = observed[0]["ssl_context"]
        self.assertIsInstance(context, ssl.SSLContext)
        self.assertEqual(context.cert_store_stats()["x509_ca"], 2)


# Stand-ins named and nested as urllib3 2.x names and nests its exceptions:
# a NameResolutionError is a NewConnectionError, which is a
# ConnectTimeoutError, which is its TimeoutError.
HTTPError = type("HTTPError", (Exception,), {})
LibraryTimeout = type("TimeoutError", (HTTPError,), {})
ConnectTimeoutError = type("ConnectTimeoutError", (LibraryTimeout,), {})
ReadTimeoutError = type("ReadTimeoutError", (LibraryTimeout,), {})
NewConnectionError = type("NewConnectionError", (ConnectTimeoutError,), {})
NameResolutionError = type("NameResolutionError", (NewConnectionError,), {})
ProxyError = type("ProxyError", (HTTPError,), {})
ProtocolError = type("ProtocolError", (HTTPError,), {})
LibrarySSLError = type("SSLError", (HTTPError,), {})


def caused(error, cause, context=False):
    try:
        try:
            raise cause
        except Exception as inner:  # pylint: disable=broad-except
            if context:
                raise error
            raise error from inner
    except Exception as outer:  # pylint: disable=broad-except
        return outer


class Classify(unittest.TestCase):
    """Each acquisition failure has one closed class, read from the error, its
    cause or its context, by the most specific type name along each MRO."""

    def test_each_class(self):
        for error, expected in (
            (NameResolutionError("private host"), "dns"),
            (socket.gaierror(-2, "Name or service not known"), "dns"),
            (ssl.SSLCertVerificationError("unknown authority"), "certificate"),
            (LibrarySSLError("handshake"), "certificate"),
            (ConnectTimeoutError("connect"), "timeout"),
            (ReadTimeoutError("read"), "timeout"),
            (TimeoutError("socket"), "timeout"),
            (files.Refused("acquisition deadline"), "timeout"),
            (ProxyError("proxy refused"), "proxy"),
            (NewConnectionError("refused"), "unreachable"),
            (ProtocolError("aborted"), "unreachable"),
            (ConnectionRefusedError(111, "refused"), "unreachable"),
            (files.Refused("source response"), "status"),
            (files.Refused("response headers"), "status"),
            (files.Refused("redirect origin"), "redirect"),
            (files.Refused("redirect count"), "redirect"),
            (files.Refused("source size"), "integrity"),
            (files.Refused("stream size"), "integrity"),
            (files.Refused("source integrity"), "integrity"),
            (files.Refused("system TLS trust"), "trust"),
            (files.Refused("system TLS trust changed"), "trust"),
            (files.Refused("artifact write"), "storage"),
            (OSError(errno.ENOSPC, "No space left on device"), "storage"),
            (OSError(errno.EDQUOT, "Disk quota exceeded"), "storage"),
            (caused(files.Refused("wrapped"), NameResolutionError("private host")), "dns"),
            (caused(files.Refused("wrapped"), ProxyError("proxy"), context=True), "proxy"),
            (ValueError("request"), None),
            (OSError("private path"), None),
            (files.Refused("relative path"), None),
        ):
            with self.subTest(repr(error)):
                self.assertEqual(files.classify(error), expected)

    def test_a_refused_download_carries_its_class_and_never_its_text(self):
        source = tool(b"publisher bytes")["source"]

        def failing(*_args, **_kwargs):
            raise NameResolutionError("private.example.test could not be resolved\nsecond line")

        manager = types.SimpleNamespace(request=failing, clear=lambda: None)
        library = types.SimpleNamespace(PoolManager=lambda **kwargs: manager, Timeout=lambda **kwargs: kwargs)
        descriptor, path = tempfile.mkstemp(prefix="bootwright-download-", dir="/tmp")
        self.addCleanup(os.unlink, path)
        self.addCleanup(os.close, descriptor)
        with mock.patch.dict("sys.modules", urllib3=library), mock.patch.object(
            files, "trusted_roots", return_value=object()
        ):
            with self.assertRaises(files.AcquisitionRefused) as refused:
                files.download(source, EGRESS, descriptor, DOWNLOAD, files.Routes())
        self.assertEqual(str(refused.exception), "approved source acquisition was refused or incomplete")
        self.assertEqual(refused.exception.reason, "dns")
        self.assertEqual(
            refused.exception.detail, "NameResolutionError: private.example.test could not be resolved"
        )
        long = files.detail(ValueError("\x1b[31m" + "x" * 400))
        self.assertEqual(len(long.encode()), files.MAX_DETAIL)
        self.assertNotIn("\x1b", long)


if __name__ == "__main__":
    unittest.main()
