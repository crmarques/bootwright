"""Each native package streams into its own scratch file, never held whole."""

from __future__ import annotations

import hashlib
import tempfile
import tracemalloc
import types
import unittest
from pathlib import Path
from unittest import mock

from ansible.plugins.action import ActionBase
from ansible.utils.display import Display
from ansible_collections.bootwright.core.plugins.action import (
    controller_inventory,
    controller_packages,
    controller_tool,
)
from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)
from ansible_collections.bootwright.core.plugins.module_utils import controller_refusal
from ansible_collections.bootwright.core.plugins.module_utils.controller_native import (
    NativeRefused,
)

EGRESS = {"httpProxy": "", "httpsProxy": "", "noProxy": []}
STAGING = 600
BLOCK = hashlib.sha256(b"rpm").digest() * (files.CHUNK // 32)


def content(size):
    return (BLOCK * (size // len(BLOCK) + 1))[:size]


def package(name, size):
    digest = hashlib.sha256()
    for offset in range(0, size, len(BLOCK)):
        digest.update(BLOCK[: min(len(BLOCK), size - offset)])
    return {
        "source": {
            "id": name,
            "url": "https://cdn-ubi.redhat.com/" + name,
            "sha256": digest.hexdigest(),
            "bytes": size,
        }
    }


def acquisition(packages, seconds=300):
    return [{"source": entry["source"]["id"], "seconds": seconds} for entry in packages]


class Response:
    def __init__(self, size):
        self.status = 200
        self.headers = {"Content-Length": str(size)}
        self.size, self.offset = size, 0

    def read(self, amount, decode_content):
        count = min(amount, self.size - self.offset)
        start = self.offset % len(BLOCK)
        block = (BLOCK[start:] + BLOCK)[:count]
        self.offset += count
        return block

    def close(self):
        pass


def publisher(packages):
    sizes = {entry["source"]["url"]: entry["source"]["bytes"] for entry in packages}
    manager = types.SimpleNamespace(
        request=lambda method, url, **kwargs: Response(sizes[url]),
        clear=lambda: None,
    )
    library = types.SimpleNamespace(
        PoolManager=lambda **kwargs: manager, Timeout=lambda **kwargs: kwargs
    )
    return mock.patch.dict("sys.modules", urllib3=library)


class StagePayloads(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory(prefix="bootwright-dnf-test-", dir="/tmp")
        self.addCleanup(scratch.cleanup)
        self.scratch = Path(scratch.name)

    def test_each_package_streams_into_its_own_file(self):
        packages = [package("large.rpm", 16 << 20), package("small.rpm", 3)]
        with publisher(packages), mock.patch.object(
            files, "trusted_roots", return_value=object()
        ):
            tracemalloc.start()
            try:
                payloads = controller_packages.stage_payloads(
                    packages, acquisition(packages), STAGING, EGRESS, str(self.scratch)
                )
                peak = tracemalloc.get_traced_memory()[1]
            finally:
                tracemalloc.stop()
        self.assertLess(peak, 2 << 20)
        self.assertEqual(
            payloads,
            {"large.rpm": str(self.scratch / "0.rpm"), "small.rpm": str(self.scratch / "1.rpm")},
        )
        self.assertEqual(Path(payloads["small.rpm"]).read_bytes(), content(3))
        large = Path(payloads["large.rpm"])
        self.assertEqual((large.stat().st_size, large.stat().st_mode & 0o7777), (16 << 20, 0o600))

    def test_a_refused_package_stops_before_the_next(self):
        packages = [package("changed.rpm", 3), package("next.rpm", 3)]
        packages[0]["source"]["sha256"] = "0" * 64
        with publisher(packages), mock.patch.object(
            files, "trusted_roots", return_value=object()
        ):
            with self.assertRaises(files.AcquisitionRefused) as refused:
                controller_packages.stage_payloads(
                    packages, acquisition(packages), STAGING, EGRESS, str(self.scratch)
                )
        self.assertEqual(sorted(path.name for path in self.scratch.iterdir()), ["0.rpm"])
        self.assertEqual((refused.exception.reason, refused.exception.source), ("integrity", "changed.rpm"))

    def test_package_bounds_are_refused(self):
        for name, packages in (
            ("oversized", [package("large.rpm", (256 << 20) + 1)]),
            ("repeated", [package("same.rpm", 3), package("same.rpm", 3)]),
        ):
            with self.subTest(name), publisher(packages), mock.patch.object(
                files, "trusted_roots", return_value=object()
            ):
                with self.assertRaises(ValueError):
                    controller_packages.stage_payloads(
                        packages, acquisition(packages), STAGING, EGRESS, str(self.scratch)
                    )

    def test_native_staging_scales_with_declared_bytes(self):
        """Each package downloads under the seconds its request froze for it,
        and all of them under the staging bound the request froze, so a run
        whose packages outlast that bound refuses as a timeout naming the
        package it had reached."""
        packages = [package("large.rpm", 3), package("small.rpm", 3)]
        entries = [
            {"source": "large.rpm", "seconds": 320},
            {"source": "small.rpm", "seconds": 121},
        ]
        now, seconds = [1000.0], []

        def downloaded(_source, _egress, _descriptor, granted, _routes):
            seconds.append(granted)
            now[0] += 400

        with mock.patch.object(controller_packages.time, "monotonic", lambda: now[0]):
            with mock.patch.object(controller_packages, "download", downloaded):
                controller_packages.stage_payloads(packages, entries, 1000, EGRESS, str(self.scratch))
                self.assertEqual(seconds, [320, 121])
                for staged in self.scratch.iterdir():
                    staged.unlink()
                with self.assertRaises(files.AcquisitionRefused) as refused:
                    controller_packages.stage_payloads(packages, entries, 300, EGRESS, str(self.scratch))
        self.assertEqual((refused.exception.reason, refused.exception.source), ("timeout", "small.rpm"))
        self.assertEqual(seconds, [320, 121, 320])

    def test_staging_refuses_a_bound_its_packages_do_not_match(self):
        packages = [package("one.rpm", 3)]
        for name, entries, staging in (
            ("no entry", [], STAGING),
            ("another source", acquisition([package("two.rpm", 3)]), STAGING),
            ("no staging", acquisition(packages), 0),
            ("past the ceiling", acquisition(packages), 7201),
        ):
            with self.subTest(name), self.assertRaises(ValueError):
                controller_packages.stage_payloads(packages, entries, staging, EGRESS, str(self.scratch))

    def test_one_pool_per_route(self):
        """Packages that take one route share one pool: two direct packages
        open one PoolManager, and a proxied and a bypassed source open one
        ProxyManager and one PoolManager; each is cleared once at the end."""
        built, cleared = [], []

        def library(packages):
            sizes = {entry["source"]["url"]: entry["source"]["bytes"] for entry in packages}

            class Manager:
                def __init__(self, kind, *args, **kwargs):
                    built.append((kind, args))

                def request(self, method, url, **kwargs):
                    return Response(sizes[url])

                def clear(self):
                    cleared.append(self)

            return types.SimpleNamespace(
                PoolManager=lambda **kwargs: Manager("pool"),
                ProxyManager=lambda proxy, **kwargs: Manager("proxy", proxy),
                Timeout=lambda **kwargs: kwargs,
            )

        bypassed = package("bypassed.rpm", 3)
        bypassed["source"]["url"] = "https://mirror.example.test/bypassed.rpm"
        proxy = dict(EGRESS, httpsProxy="http://proxy.example.test:3128", noProxy=["mirror.example.test"])
        for name, packages, egress, expected in (
            ("direct", [package("one.rpm", 3), package("two.rpm", 3)], EGRESS, [("pool", ())]),
            ("proxied and bypassed", [package("one.rpm", 3), bypassed, package("two.rpm", 3)], proxy,
             [("proxy", ("http://proxy.example.test:3128",)), ("pool", ())]),
        ):
            del built[:], cleared[:]
            area = tempfile.TemporaryDirectory(prefix="bootwright-dnf-test-", dir="/tmp")
            self.addCleanup(area.cleanup)
            scratch = Path(area.name)
            with self.subTest(name), mock.patch.dict("sys.modules", urllib3=library(packages)), mock.patch.object(
                files, "trusted_roots", return_value=object()
            ) as trust:
                controller_packages.stage_payloads(packages, acquisition(packages), STAGING, egress, str(scratch))
                self.assertEqual(built, expected)
                self.assertEqual(len(cleared), len(expected))
                self.assertEqual(trust.call_count, 1)


class NamedRefusal(unittest.TestCase):
    """Each controller adapter names its refusal to the runner in one record
    before its task fails: a class, and an acquisition's source, never the
    exception's text, which reaches only the private run output."""

    def run_action(self, module, arguments, refusal, patched):
        records, warnings = [], []
        action = module.ActionModule.__new__(module.ActionModule)
        setattr(action, "_task", types.SimpleNamespace(args=arguments))
        with mock.patch.object(ActionBase, "run", return_value={}), mock.patch.object(
            module, patched, side_effect=refusal
        ), mock.patch.object(
            controller_refusal, "emit", lambda record, acknowledge=False: records.append((record, acknowledge))
        ), mock.patch.object(Display, "warning", lambda _self, text, *args, **kwargs: warnings.append(text)):
            return action.run(), records, warnings

    def test_each_adapter_names_its_refusal_before_failing(self):
        secret = "NameResolutionError: secret.example.test lookup failed"
        native = {
            "actions": [{"sourceID": "one.rpm"}], "packages": [package("one.rpm", 3)],
            "requirements": {}, "requests": {}, "digest": "d" * 64,
            "beforeSHA256": "a" * 64, "afterSHA256": "b" * 64,
        }
        request = {
            "operation": "setup", "platform": {}, "native": native, "publicationBundle": {},
            "egress": EGRESS, "acquisition": acquisition(native["packages"]), "nativeStaging": STAGING,
        }
        bundle = types.SimpleNamespace(writable=lambda: None, verify=lambda: None, close=lambda: None)
        tool = {"source": {"id": "tool-helm"}}
        tool_arguments = dict(bundle={}, tool=tool, egress=EGRESS, inspect_only=False, deadline=205)
        for name, module, arguments, patched, refusal, record, warning in (
            ("packages", controller_packages, {"request": request}, "download",
             files.AcquisitionRefused("approved source acquisition was refused or incomplete", reason="dns", detail=secret),
             {"phase": "refused", "reason": "dns", "source": "one.rpm"}, secret),
            ("tool acquisition", controller_tool, tool_arguments, "prepare_tool",
             files.AcquisitionRefused("source integrity", reason="integrity", detail=secret),
             {"phase": "refused", "reason": "integrity", "source": "tool-helm"}, secret),
            ("tool release stamp", controller_tool, tool_arguments, "prepare_tool",
             files.Unreleased("openshift client release"), {"phase": "refused", "reason": "release-stamp"}, None),
            ("inventory", controller_inventory, {"platform": {}, "plan": None}, "inspection",
             NativeRefused("database", "DatabaseLocked: secret rpmdb path"), {"phase": "refused", "reason": "database"},
             "DatabaseLocked: secret rpmdb path"),
        ):
            with self.subTest(name), mock.patch.object(controller_packages, "Bundle", return_value=bundle):
                result, records, warnings = self.run_action(module, arguments, refusal, patched)
                self.assertTrue(result["failed"])
                self.assertTrue(result["_ansible_no_log"])
                self.assertNotIn("secret", result["msg"])
                self.assertEqual(records, [(record, False)])
                self.assertNotIn("secret", repr(records))
                self.assertEqual(warnings, ["controller adapter: " + warning] if warning else [])

    def test_an_unclassified_refusal_names_nothing(self):
        for refusal in (ValueError("request"), OSError("/private/path"), NativeRefused("internal")):
            with self.subTest(repr(refusal)):
                result, records, _warnings = self.run_action(
                    controller_inventory, {"platform": {}, "plan": None}, refusal, "inspection"
                )
                self.assertTrue(result["failed"])
                expected = [({"phase": "refused", "reason": "internal"}, False)] if isinstance(refusal, NativeRefused) else []
                self.assertEqual(records, expected)


if __name__ == "__main__":
    unittest.main()
