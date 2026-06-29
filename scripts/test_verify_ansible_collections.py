import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


def load_verify_module():
    path = Path(__file__).with_name("verify-ansible-collections.py")
    spec = importlib.util.spec_from_file_location("verify_ansible_collections", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


verify_mod = load_verify_module()

_LOCK = """---
collections:
  - name: community.general
    version: 12.6.0
    source: https://galaxy.ansible.com/download/community-general-12.6.0.tar.gz
    filesManifestSha256: aaaa
  - name: ansible.posix
    version: 2.1.0
    source: https://galaxy.ansible.com/download/ansible-posix-2.1.0.tar.gz
    filesManifestSha256: bbbb
"""


def _write_manifest(root, namespace, name, version, sha, chksum_type="sha256"):
    coll_dir = Path(root) / "ansible_collections" / namespace / name
    coll_dir.mkdir(parents=True, exist_ok=True)
    (coll_dir / "MANIFEST.json").write_text(
        json.dumps(
            {
                "collection_info": {
                    "namespace": namespace,
                    "name": name,
                    "version": version,
                },
                "file_manifest_file": {
                    "chksum_type": chksum_type,
                    "chksum_sha256": sha,
                },
            }
        ),
        encoding="utf-8",
    )


class ParseLockTest(unittest.TestCase):
    def test_parses_name_version_and_sha(self):
        entries = verify_mod.parse_lock(_LOCK)
        self.assertEqual(len(entries), 2)
        self.assertEqual(entries[0]["name"], "community.general")
        self.assertEqual(entries[0]["version"], "12.6.0")
        self.assertEqual(entries[0]["filesManifestSha256"], "aaaa")
        # the source value keeps its embedded colons
        self.assertTrue(entries[0]["source"].startswith("https://"))
        self.assertEqual(entries[1]["name"], "ansible.posix")
        self.assertEqual(entries[1]["filesManifestSha256"], "bbbb")


class VerifyTest(unittest.TestCase):
    def test_passes_when_manifests_match(self):
        entries = verify_mod.parse_lock(_LOCK)
        with tempfile.TemporaryDirectory() as tmp:
            _write_manifest(tmp, "community", "general", "12.6.0", "aaaa")
            _write_manifest(tmp, "ansible", "posix", "2.1.0", "bbbb")
            self.assertEqual(verify_mod.verify(tmp, entries), [])

    def test_fails_on_sha_mismatch(self):
        entries = verify_mod.parse_lock(_LOCK)
        with tempfile.TemporaryDirectory() as tmp:
            _write_manifest(tmp, "community", "general", "12.6.0", "tampered")
            _write_manifest(tmp, "ansible", "posix", "2.1.0", "bbbb")
            errors = verify_mod.verify(tmp, entries)
            self.assertEqual(len(errors), 1)
            self.assertIn("community.general", errors[0])

    def test_fails_on_version_mismatch(self):
        entries = verify_mod.parse_lock(_LOCK)
        with tempfile.TemporaryDirectory() as tmp:
            _write_manifest(tmp, "community", "general", "9.9.9", "aaaa")
            _write_manifest(tmp, "ansible", "posix", "2.1.0", "bbbb")
            errors = verify_mod.verify(tmp, entries)
            self.assertTrue(any("installed version" in e for e in errors))

    def test_fails_on_missing_manifest(self):
        entries = verify_mod.parse_lock(_LOCK)
        with tempfile.TemporaryDirectory() as tmp:
            _write_manifest(tmp, "community", "general", "12.6.0", "aaaa")
            errors = verify_mod.verify(tmp, entries)
            self.assertTrue(any("missing" in e for e in errors))

    def test_fails_on_non_sha256_chksum_type(self):
        entries = verify_mod.parse_lock(_LOCK)
        with tempfile.TemporaryDirectory() as tmp:
            _write_manifest(tmp, "community", "general", "12.6.0", "aaaa", chksum_type="md5")
            _write_manifest(tmp, "ansible", "posix", "2.1.0", "bbbb")
            errors = verify_mod.verify(tmp, entries)
            self.assertEqual(len(errors), 1)
            self.assertIn("community.general", errors[0])


if __name__ == "__main__":
    unittest.main()
