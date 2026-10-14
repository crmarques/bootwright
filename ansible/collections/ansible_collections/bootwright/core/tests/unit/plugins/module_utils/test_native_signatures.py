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
        self.raw_key = fingerprint


class PublisherSignatures(unittest.TestCase):
    def test_foreign_but_host_trusted_key_is_not_imported_or_accepted(self):
        approved, foreign = "a" * 40, "b" * 40
        host_trust = {approved, foreign}
        imported = []
        roots = []
        closed = []

        class Transaction:
            def __init__(self, root):
                self.keys = None

            def initDB(self):
                pass

            def pgpImportPubkey(self, key):
                imported.append(key)
                Transaction.trusted = key
                return 0

            def closeDB(self):
                pass

        class Base:
            def add_remote_rpms(self, sources, strict):
                return list(sources)

            def package_signature_check(self, path):
                self_outer.assertIn(path, host_trust)
                return (0 if path == Transaction.trusted else 1,)

            def close(self):
                closed.append(True)

        self_outer = self

        def setup(request, scratch, repositories, installroot):
            self.assertFalse(repositories)
            self.assertNotEqual(installroot, "/")
            self.assertTrue(installroot.startswith(scratch + "/"))
            roots.append(installroot)
            return Base()

        request = dict(
            platform=dict(os="rhel", release="9.8"),
            plan=dict(
                solver="dnf4",
                packages=[dict(signer=approved, source=dict(id="source"))],
            ),
        )
        rpm = SimpleNamespace(TransactionSet=Transaction)
        crypto = SimpleNamespace(rawkey2infos=lambda stream: [Key(approved), Key(foreign)])
        misc = SimpleNamespace(procgpgkey=lambda raw: raw)
        yum = SimpleNamespace(misc=misc)
        modules = {
            "rpm": rpm,
            "dnf": SimpleNamespace(crypto=crypto, yum=yum),
            "dnf.crypto": crypto,
            "dnf.yum": yum,
            "dnf.yum.misc": misc,
        }
        with tempfile.TemporaryDirectory() as directory, patch.object(
            native_signatures, "read_key", return_value=b"public fixture"
        ), patch.object(native_resolution, "setup_dnf4", side_effect=setup), patch.dict(
            "sys.modules", modules
        ):
            native_signatures.verify(request, directory, {"source": approved})
            with self.assertRaises(ValueError):
                native_signatures.verify(request, directory, {"source": foreign})
        self.assertEqual(imported, [approved, approved])
        self.assertEqual(len(roots), 2)
        self.assertNotEqual(roots[0], roots[1])
        self.assertEqual(closed, [True, True])

    def test_only_a_dnf4_plan_is_verified(self):
        request = dict(plan=dict(solver="dnf5", packages=[]))
        with self.assertRaises(ValueError) as refused:
            native_signatures.verify(request, "/scratch", {})
        self.assertEqual(str(refused.exception), "native publisher solver")

    def test_only_the_rhel_release_key_is_a_publisher_key(self):
        self.assertEqual(
            native_signatures.publisher_key(dict(os="rhel", release="9.8")),
            "/etc/pki/rpm-gpg/RPM-GPG-KEY-redhat-release",
        )
        for platform in (dict(os="fedora", release="44"), dict(os="centos", release="9")):
            with self.assertRaises(ValueError):
                native_signatures.publisher_key(platform)

    def test_missing_or_ambiguous_fingerprint_refuses_before_key_import(self):
        selected = "a" * 40
        for keys in ([], [Key("b" * 40)], [Key(selected), Key(selected)]):
            with self.assertRaises(ValueError):
                native_signatures.select_key(keys, selected)


if __name__ == "__main__":
    unittest.main()
