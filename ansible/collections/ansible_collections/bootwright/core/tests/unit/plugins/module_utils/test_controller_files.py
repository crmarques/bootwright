from __future__ import annotations

import io
import os
from pathlib import Path
import tarfile
import tempfile
import types
import unittest
from unittest import mock

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)

EGRESS = {"httpProxy": "", "httpsProxy": "", "noProxy": []}


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

    def test_first_publication_replay_and_partial_recovery(self):
        data = b"qualified executable"
        definition = tool(data)
        with mock.patch.object(files, "download", return_value=data) as download:
            first = files.prepare_tool(self.location, definition, EGRESS)
        self.assertTrue(first["changed"])
        download.assert_called_once()
        paths = {
            path.relative_to(self.root).as_posix(): path.stat().st_ino
            for path in self.root.rglob("*")
            if path.is_file()
        }
        with mock.patch.object(
            files,
            "download",
            side_effect=AssertionError("retained replay acquired source"),
        ):
            second = files.prepare_tool(self.location, definition, EGRESS)
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
                files.prepare_tool(self.location, definition, EGRESS, inspect_only=True)
            recovered = files.prepare_tool(self.location, definition, EGRESS)
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
        with mock.patch.object(files, "download", return_value=b"complete source"):
            with mock.patch.object(
                files.os, "write", side_effect=OSError("injected interruption")
            ):
                with self.assertRaises(OSError):
                    files.prepare_tool(self.location, definition, EGRESS)
        self.assertEqual([path for path in self.root.rglob("*") if path.is_file()], [])
        self.assertFalse(any("tmp" in path.name for path in self.root.rglob("*")))

    def test_sealed_missing_target_is_never_refilled(self):
        definition = tool(b"complete source")
        with mock.patch.object(files, "download", return_value=b"complete source"):
            files.prepare_tool(self.location, definition, EGRESS)
        executable = self.root / definition["files"][0]["path"]
        executable.unlink()
        for writable, sealed in ((False, True), (True, True), (False, False)):
            location = dict(self.location, writable=writable, sealed=sealed)
            with mock.patch.object(
                files, "download", side_effect=AssertionError("sealed download")
            ):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(location, definition, EGRESS)
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
        with mock.patch.object(
            files,
            "download",
            side_effect=AssertionError("unattributed source acquisition"),
        ):
            with self.assertRaises(files.Refused):
                files.prepare_tool(self.location, definition, EGRESS)
        target.unlink()
        with mock.patch.object(files, "download", return_value=b"complete source"):
            files.prepare_tool(self.location, definition, EGRESS)
        target.write_bytes(b"changed content")
        with self.assertRaises(files.Refused):
            files.prepare_tool(self.location, definition, EGRESS)
        self.assertEqual(target.read_bytes(), b"changed content")

    def test_directory_substitution_and_symlink_escape_are_refused(self):
        wrong = dict(self.location, inode=self.location["inode"] + 1)
        with self.assertRaises(files.Refused):
            files.Bundle(wrong)
        definition = tool(b"bytes")
        (self.root / "sources").symlink_to("/tmp", target_is_directory=True)
        with mock.patch.object(
            files, "download", side_effect=AssertionError("unsafe acquisition")
        ):
            with self.assertRaises(OSError):
                files.prepare_tool(self.location, definition, EGRESS)

    def test_selected_archive_alias_is_flattened_and_unsafe_members_refused(self):
        data = archive(
            [("oc", b"client executable", "file"), ("kubectl", "oc", "symlink")]
        )
        definition = tool(data, "openshift-clients", "openshift", "4.20.8")
        projected = files.project(definition, data)
        self.assertEqual(set(projected.values()), {b"client executable"})
        for members in (
            [("oc", b"ok", "file"), ("kubectl", "../oc", "symlink")],
            [
                ("oc", b"ok", "file"),
                ("kubectl", "outside", "symlink"),
                ("outside", b"ok", "file"),
            ],
            [
                ("oc", b"ok", "file"),
                ("oc", b"again", "file"),
                ("kubectl", "oc", "hardlink"),
            ],
            [("oc", "kubectl", "symlink"), ("kubectl", "oc", "symlink")],
            [("../outside", b"bad", "file")],
        ):
            data = archive(members)
            with self.assertRaises(files.Refused):
                files.project(
                    tool(data, "openshift-clients", "openshift", "4.20.8"), data
                )

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
            with mock.patch.object(
                files,
                "download",
                side_effect=AssertionError("invalid input read network"),
            ):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS)
        with mock.patch.object(files, "download", return_value=b"wrong"):
            with self.assertRaises(files.Refused):
                files.prepare_tool(self.location, tool(b"bytes"), EGRESS)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_frozen_tool_requires_exact_stable_release(self):
        for version in ("latest", "v1.2.3-rc.1", "1.02.3", "1.2.3+build"):
            with self.subTest(version=version):
                definition = tool(b"bytes", "virtctl", "kubevirt", version)
                with mock.patch.object(
                    files,
                    "download",
                    side_effect=AssertionError("invalid release read network"),
                ):
                    with self.assertRaises(files.Refused):
                        files.prepare_tool(self.location, definition, EGRESS)
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
                self.assertEqual(files.download(source, egress), body)
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
                    files.download(source, EGRESS)
            self.assertEqual(
                str(error.exception),
                "approved source acquisition was refused or incomplete",
            )
        with mock.patch.object(
            files, "_download", side_effect=OSError("sensitive source or proxy detail")
        ):
            with self.assertRaises(files.Refused) as error:
                files.download(source, EGRESS)
        self.assertNotIn("sensitive", str(error.exception))

    def test_bundle_and_archive_capacity_bounds_precede_publication(self):
        definition = tool(b"complete source")
        with mock.patch.object(files, "download", return_value=b"complete source"):
            with mock.patch.object(files, "MAX_BUNDLE", 1):
                with self.assertRaises(files.Refused):
                    files.prepare_tool(self.location, definition, EGRESS)
        self.assertEqual(list(self.root.iterdir()), [])
        data = archive([("oc", b"client", "file"), ("kubectl", "oc", "symlink")])
        definition = tool(data, "openshift-clients", "openshift", "4.20.8")
        with mock.patch.object(files, "MAX_ENTRIES", 1):
            with self.assertRaises(files.Refused):
                files.project(definition, data)
        with mock.patch.object(files, "MAX_EXPANDED", 512):
            with self.assertRaises(files.Refused):
                files.project(definition, data)


if __name__ == "__main__":
    unittest.main()
