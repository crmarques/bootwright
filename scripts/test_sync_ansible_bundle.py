import importlib.util
from pathlib import Path
import tempfile
import unittest


def load_sync_bundle_module():
    path = Path(__file__).with_name("sync-ansible-bundle.py")
    spec = importlib.util.spec_from_file_location("sync_ansible_bundle", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


sync_bundle = load_sync_bundle_module()


class AddTreeSymlinkTest(unittest.TestCase):
    def test_rejects_authored_source_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / "target.yml"
            target.write_text("---\n", encoding="utf-8")
            (root / "link.yml").symlink_to(target)

            with self.assertRaisesRegex(sync_bundle.BundleError, "refusing to bundle symlink"):
                sync_bundle.add_tree({}, root, Path())

    def test_skips_collection_symlink_without_following_target(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / "target.txt"
            target.write_text("secret\n", encoding="utf-8")
            (root / "link.txt").symlink_to(target)
            files = {}

            sync_bundle.add_tree(files, root, Path("collections"))

            self.assertNotIn("collections/link.txt", files)


if __name__ == "__main__":
    unittest.main()
