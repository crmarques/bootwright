"""Each native package streams into its own scratch file, never held whole."""

from __future__ import annotations

import hashlib
import tempfile
import tracemalloc
import types
import unittest
from pathlib import Path
from unittest import mock

from ansible_collections.bootwright.core.plugins.action import controller_packages
from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_files as files,
)

EGRESS = {"httpProxy": "", "httpsProxy": "", "noProxy": []}
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
                    packages, EGRESS, str(self.scratch)
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
            with self.assertRaises(files.Refused):
                controller_packages.stage_payloads(packages, EGRESS, str(self.scratch))
        self.assertEqual(sorted(path.name for path in self.scratch.iterdir()), ["0.rpm"])

    def test_package_bounds_are_refused(self):
        for name, packages in (
            ("oversized", [package("large.rpm", (256 << 20) + 1)]),
            ("repeated", [package("same.rpm", 3), package("same.rpm", 3)]),
        ):
            with self.subTest(name), publisher(packages), mock.patch.object(
                files, "trusted_roots", return_value=object()
            ):
                with self.assertRaises(ValueError):
                    controller_packages.stage_payloads(packages, EGRESS, str(self.scratch))


if __name__ == "__main__":
    unittest.main()
