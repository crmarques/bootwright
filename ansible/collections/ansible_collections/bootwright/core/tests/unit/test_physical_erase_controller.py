"""Image custody never reopens a replaced operator source or publishes stages."""

import hashlib
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as controller


class ControllerTests(unittest.TestCase):
    def test_build_keeps_the_verified_input_descriptor_through_native_readers(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            media, work, published = (root / name for name in ("media", "work", "published"))
            for directory in (media, work, published):
                directory.mkdir(mode=0o700)
            source = media / "boot"
            original = b"verified original media"
            source.write_bytes(original)
            instance = controller.Controller.__new__(controller.Controller)
            instance.root = work
            instance.request = {
                "install": {
                    "bootMedia": {"name": "boot", "size": len(original), "sha256": hashlib.sha256(original).hexdigest()},
                    "installerArguments": [],
                    "private": {"path": str(published)},
                }
            }
            token = "a" * 64
            (published / token).mkdir()
            instance.load = lambda: {"token": token}
            observed = []

            def build(argv, **unused):
                held = argv[-2]
                observed.append(held)
                source.unlink()
                source.write_bytes(b"replaced pathname")
                self.assertEqual(Path(held).read_bytes(), original)
                Path(argv[-1]).write_bytes(b"built private image")
                return b""

            real_fstat = os.fstat

            def owned(fd):
                value = real_fstat(fd)
                return SimpleNamespace(st_mode=value.st_mode, st_uid=0, st_size=value.st_size)

            with (
                patch.object(controller, "MEDIA", media),
                patch.object(controller, "directory"),
                patch.object(controller, "native", side_effect=build),
                patch.object(os, "fstat", side_effect=owned),
            ):
                instance.build()
            self.assertEqual((published / token / "erase.iso").read_bytes(), b"built private image")
            self.assertFalse((work / "erase.iso").exists())
            self.assertTrue(observed[0].startswith("/proc/" + str(os.getpid()) + "/fd/"))
            with self.assertRaises(FileNotFoundError):
                Path(observed[0]).read_bytes()

    def test_interrupted_private_write_leaves_no_record_or_owned_stage(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "custody.json"
            with patch.object(os, "write", return_value=1), self.assertRaises(controller.worker.Refusal):
                controller.write(target, b"complete record")
            self.assertFalse(target.exists())
            self.assertEqual(list(target.parent.iterdir()), [])

    def test_durable_private_write_never_replaces_an_existing_record(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "custody.json"
            controller.write(target, b"first")
            with self.assertRaises(FileExistsError):
                controller.write(target, b"replacement")
            self.assertEqual(target.read_bytes(), b"first")
            self.assertEqual(list(target.parent.iterdir()), [target])

    def test_authenticated_failed_worker_refuses_without_waiting_or_acknowledging(self):
        import json

        instance = controller.Controller.__new__(controller.Controller)
        instance.digest = "a" * 64
        instance.worker_request = {"originalAttempt": "remove/erase/1"}
        instance.request = {"deadlineSeconds": 14400}
        instance.load = lambda: None
        instance.receipt = lambda: None
        instance.session = lambda command: SimpleNamespace(
            code=0, stdout=json.dumps({"state": "failed", "request": instance.digest, "originalAttempt": "remove/erase/1"}).encode()
        )
        with patch.object(controller.time, "sleep") as sleep, self.assertRaisesRegex(controller.worker.Refusal, "worker-failed"):
            instance.await_receipt()
        sleep.assert_not_called()

    def test_interrupted_final_cleanup_retries_without_keys_or_false_completion(self):
        with tempfile.TemporaryDirectory() as temporary:
            instance = controller.Controller.__new__(controller.Controller)
            instance.root = Path(temporary)
            instance.lock = os.open(temporary, os.O_RDONLY | os.O_DIRECTORY)
            publication = instance.root / "publication"
            publication.mkdir()
            instance.request = {"install": {"private": {"path": str(publication)}}}
            instance.load = lambda: {"token": "a" * 64}
            try:
                for name in ("host-key", "host-key.pub", "ks.cfg", "ssh_config", "known_hosts"):
                    (instance.root / name).write_text("private")
                evidence = {"version": "physical-erase-v1", "request": "a" * 64, "prepared": True, "removed": True, "receipt": {"state": "complete"}}
                real_unlink = Path.unlink

                def interrupted(path, **kwargs):
                    if path.name == "ks.cfg":
                        raise OSError("injected cleanup interruption")
                    return real_unlink(path, **kwargs)

                with patch.object(controller, "directory"), patch.object(Path, "unlink", interrupted), self.assertRaises(OSError):
                    instance.finish(evidence)
                self.assertTrue((instance.root / "finalizing.json").exists())
                self.assertFalse((instance.root / "completed.json").exists())
                self.assertFalse((instance.root / "host-key").exists())
                with patch.object(controller, "directory"), patch.object(controller, "read", side_effect=lambda path: Path(path).read_bytes()):
                    instance.finish(evidence)
                    instance.finish(evidence)
                self.assertTrue((instance.root / "completed.json").exists())
                self.assertFalse((instance.root / "ks.cfg").exists())
                self.assertEqual({path.name for path in instance.root.iterdir()}, {"finalizing.json", "completed.json", "publication"})
            finally:
                os.close(instance.lock)
