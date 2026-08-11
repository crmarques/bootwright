from __future__ import annotations

import fcntl
import importlib.util
import pathlib
import tempfile
import unittest
from unittest import mock


_MODULE_PATH = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "modules" / "claim_cas.py"
_spec = importlib.util.spec_from_file_location("_bootwright_claim_cas", _MODULE_PATH)
assert _spec and _spec.loader
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)


class ClaimCAS(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.directory.name)
        self.claim = self.root / "service" / "claim"
        self.claim.parent.mkdir()
        self.lock = self.root / "claim.lock"

    def tearDown(self):
        self.directory.cleanup()

    def mutate(
        self,
        state="present",
        desired="context-a\n",
        expected=None,
        allow_absent=False,
        create_parents=False,
    ):
        return _module.claim_cas(
            str(self.claim),
            state,
            desired if state == "present" else None,
            [] if expected is None else expected,
            allow_absent,
            str(self.lock),
            create_parents,
            str(self.root),
        )

    def test_first_writer_wins_and_same_content_is_idempotent(self):
        changed, previous, current = self.mutate(allow_absent=True)
        self.assertTrue(changed)
        self.assertEqual(previous, "")
        self.assertTrue(current)
        self.assertEqual(self.claim.read_text(), "context-a\n")
        changed, previous, current = self.mutate()
        self.assertFalse(changed)
        self.assertEqual(previous, current)

    def test_competing_absent_observation_cannot_replace_winner(self):
        self.mutate(allow_absent=True)
        with self.assertRaisesRegex(ValueError, "unexpected sha256"):
            self.mutate(desired="context-b\n", allow_absent=True)
        self.assertEqual(self.claim.read_text(), "context-a\n")

    def test_noncooperating_create_in_final_absent_window_is_not_overwritten(self):
        real_link = _module.os.link

        def competing_link(source, target, **kwargs):
            descriptor = _module.os.open(
                target,
                _module.os.O_WRONLY | _module.os.O_CREAT | _module.os.O_EXCL,
                0o600,
                dir_fd=kwargs["dst_dir_fd"],
            )
            _module.os.write(descriptor, b"foreign\n")
            _module.os.close(descriptor)
            return real_link(source, target, **kwargs)

        with mock.patch.object(_module.os, "link", side_effect=competing_link):
            with self.assertRaises(FileExistsError):
                self.mutate(allow_absent=True)
        self.assertEqual(self.claim.read_text(), "foreign\n")

    def test_existing_target_replacement_relies_on_shared_lock_cooperation(self):
        self.mutate(allow_absent=True)
        real_replace = _module.os.replace

        def noncooperating_replace(source, target, **kwargs):
            descriptor = _module.os.open(
                target,
                _module.os.O_WRONLY | _module.os.O_TRUNC,
                dir_fd=kwargs["dst_dir_fd"],
            )
            _module.os.write(descriptor, b"out-of-band-root-writer\n")
            _module.os.close(descriptor)
            return real_replace(source, target, **kwargs)

        with mock.patch.object(_module.os, "replace", side_effect=noncooperating_replace):
            changed, _, _ = self.mutate(
                desired="context-b\n",
                expected=["context-a\n"],
            )
        self.assertTrue(changed)
        self.assertEqual(self.claim.read_text(), "context-b\n")

    def test_exact_expected_content_authorizes_atomic_replacement_and_removal(self):
        self.mutate(allow_absent=True)
        changed, previous, current = self.mutate(
            desired="context-b\n", expected=["context-a\n"]
        )
        self.assertTrue(changed)
        self.assertTrue(previous)
        self.assertTrue(current)
        self.assertEqual(self.claim.read_text(), "context-b\n")
        changed, previous, current = self.mutate(
            state="absent", expected=["context-b\n"]
        )
        self.assertTrue(changed)
        self.assertTrue(previous)
        self.assertEqual(current, "")
        self.assertFalse(self.claim.exists())

    def test_refuses_wrong_expected_content_symlink_and_missing_parent(self):
        self.mutate(allow_absent=True)
        with self.assertRaisesRegex(ValueError, "unexpected sha256"):
            self.mutate(desired="context-b\n", expected=["foreign\n"])
        self.claim.unlink()
        self.claim.symlink_to(self.root / "foreign")
        with self.assertRaisesRegex(ValueError, "regular non-symlink"):
            self.mutate()
        self.claim.unlink()
        self.claim.parent.rmdir()
        with self.assertRaisesRegex(ValueError, "does not exist"):
            self.mutate(allow_absent=True)

    def test_securely_creates_missing_parents_for_present_state(self):
        self.claim.parent.rmdir()
        changed, previous, current = self.mutate(
            allow_absent=True,
            create_parents=True,
        )
        self.assertTrue(changed)
        self.assertEqual(previous, "")
        self.assertTrue(current)
        self.assertEqual(self.claim.read_text(), "context-a\n")
        self.assertEqual(self.claim.parent.stat().st_mode & 0o777, 0o700)

    def test_absent_missing_parent_does_not_create_any_parent(self):
        self.claim.parent.rmdir()
        changed, previous, current = self.mutate(
            state="absent",
            allow_absent=True,
            create_parents=True,
        )
        self.assertFalse(changed)
        self.assertEqual((previous, current), ("", ""))
        self.assertFalse(self.claim.parent.exists())
        self.assertTrue(self.lock.exists())

    def test_absent_missing_parent_is_classified_under_global_lock(self):
        self.claim.parent.rmdir()
        real_walk = _module._walk_directory
        observed_lock = []

        def require_lock(path, trusted_root, create):
            if path == str(self.claim.parent):
                with self.lock.open("r+") as stream:
                    with self.assertRaises(BlockingIOError):
                        fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                observed_lock.append(True)
            return real_walk(path, trusted_root, create)

        with mock.patch.object(_module, "_walk_directory", side_effect=require_lock):
            changed, previous, current = self.mutate(
                state="absent",
                allow_absent=True,
                create_parents=True,
            )
        self.assertFalse(changed)
        self.assertEqual((previous, current), ("", ""))
        self.assertEqual(observed_lock, [True])

    def test_refuses_symlinked_or_writable_component_during_secure_walk(self):
        self.claim.parent.rmdir()
        foreign = self.root / "foreign"
        foreign.mkdir()
        self.claim.parent.symlink_to(foreign, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "not a directory that is not a symlink"):
            self.mutate(allow_absent=True, create_parents=True)
        self.claim.parent.unlink()
        self.claim.parent.mkdir(mode=0o777)
        self.claim.parent.chmod(0o777)
        with self.assertRaisesRegex(ValueError, "non-writable directory"):
            self.mutate(allow_absent=True, create_parents=True)

    def test_refuses_while_another_process_holds_global_claim_lock(self):
        with self.lock.open("w") as stream:
            self.lock.chmod(0o600)
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, "held by another mutation"):
                self.mutate(allow_absent=True)

    def test_refuses_parent_and_lock_symlinks_wrong_mode_hardlinks_and_alias(self):
        real_parent = self.claim.parent
        linked_parent = self.root / "linked-service"
        real_parent.rename(linked_parent)
        real_parent.symlink_to(linked_parent, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "not a directory that is not a symlink"):
            self.mutate(allow_absent=True)
        real_parent.unlink()
        linked_parent.rename(real_parent)

        self.lock.write_text("")
        self.lock.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "0600 regular inode"):
            self.mutate(allow_absent=True)
        self.lock.chmod(0o600)
        lock_alias = self.root / "claim-lock-alias"
        lock_alias.hardlink_to(self.lock)
        with self.assertRaisesRegex(ValueError, "0600 regular inode"):
            self.mutate(allow_absent=True)
        lock_alias.unlink()
        self.lock.unlink()
        self.lock.symlink_to(self.root / "foreign-lock")
        with self.assertRaises(OSError):
            self.mutate(allow_absent=True)

        with self.assertRaisesRegex(ValueError, "must be different"):
            _module.claim_cas(
                str(self.claim),
                "present",
                "context-a\n",
                [],
                True,
                str(self.claim),
                False,
                str(self.root),
            )

    def test_refuses_lock_path_inode_replacement_after_flock(self):
        self.lock.write_text("")
        self.lock.chmod(0o600)
        real_flock = _module.fcntl.flock

        def replace_lock(descriptor, operation):
            result = real_flock(descriptor, operation)
            if operation & fcntl.LOCK_EX:
                self.lock.unlink()
                self.lock.write_text("")
                self.lock.chmod(0o600)
            return result

        with mock.patch.object(_module.fcntl, "flock", side_effect=replace_lock):
            with self.assertRaisesRegex(ValueError, "changed while held"):
                self.mutate(allow_absent=True)


if __name__ == "__main__":
    unittest.main()
