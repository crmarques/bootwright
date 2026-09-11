"""Publisher-specific scratch keyrings do not inherit ambient host trust."""

import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import (
    native_resolution,
    native_signatures,
)


class Key:
    def __init__(self, fingerprint):
        self.fingerprint = fingerprint

    def get_fingerprint(self):
        return self.fingerprint


class PublisherSignatures(unittest.TestCase):
    def test_foreign_but_host_trusted_key_is_not_imported_or_accepted(self):
        approved, foreign = "a" * 40, "b" * 40
        host_trust = {approved, foreign}
        imported = []
        roots = []

        class Verifier:
            CheckResult_OK = 0

            def __init__(self, base):
                self.keys = set()

            def parse_key_file(self, path):
                return [Key(approved), Key(foreign)]

            def import_key(self, key):
                self.keys.add(key.fingerprint)
                imported.append(key.fingerprint)

            def check_package_signature(self, path):
                self_outer.assertIn(path, host_trust)
                return 0 if path in self.keys else 1

        self_outer = self
        library = SimpleNamespace(rpm=SimpleNamespace(RpmSignature=Verifier))

        def setup(request, scratch, repositories, installroot):
            self.assertFalse(repositories)
            self.assertNotEqual(installroot, "/")
            self.assertTrue(installroot.startswith(scratch + "/"))
            roots.append(installroot)
            return object()

        request = dict(
            platform=dict(os="fedora"),
            plan=dict(
                solver="dnf5",
                packages=[dict(signer=approved, source=dict(id="source"))],
            ),
        )
        with tempfile.TemporaryDirectory() as directory, patch.object(
            native_signatures, "read_key", return_value=b"public fixture"
        ), patch.object(native_resolution, "setup_dnf5", side_effect=setup), patch.dict(
            "sys.modules", libdnf5=library
        ):
            native_signatures.verify(request, directory, {"source": approved})
            with self.assertRaises(ValueError):
                native_signatures.verify(request, directory, {"source": foreign})
        self.assertEqual(imported, [approved, approved])
        self.assertEqual(len(roots), 2)
        self.assertNotEqual(roots[0], roots[1])

    def test_missing_or_ambiguous_fingerprint_refuses_before_key_import(self):
        selected = "a" * 40
        for keys in ([], [Key("b" * 40)], [Key(selected), Key(selected)]):
            with self.assertRaises(ValueError):
                native_signatures.select_key(keys, selected, True)


if __name__ == "__main__":
    unittest.main()
