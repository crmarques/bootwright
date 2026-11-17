"""Image custody never reopens a replaced operator source or publishes stages."""

import base64
import hashlib
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as controller


class ControllerTests(unittest.TestCase):
    def test_custody_lives_beneath_the_machines_hosted_erase_directory_created_top_down(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "bootwright-hosted"
            visited = []

            def proved(path, create):
                visited.append((path, create))
                if create:
                    path.mkdir(mode=0o700)

            request = {"version": "physical-erase-v1", "install": {"identity": {"context": "lab", "object": "metal-01"}}}
            material = {"applyOperation": "op-" + "0" * 32}
            with patch.object(controller, "ROOT", root), patch.object(controller, "directory", side_effect=proved):
                instance = controller.Controller(request, "a" * 64, material, create=True)
            instance.close()
            expected = [root, root / "lab", root / "lab" / "machines", root / "lab" / "machines" / "metal-01",
                        root / "lab" / "machines" / "metal-01" / "erase", root / "lab" / "machines" / "metal-01" / "erase" / material["applyOperation"]]
            self.assertEqual(visited, [(path, True) for path in expected])
            self.assertEqual(instance.root, expected[-1])
            self.assertEqual(str(controller.ROOT), "/var/lib/bootwright-hosted")
            for version in ("physical-erase-v2", "physical-erase-v3"):
                with patch.object(controller, "directory", side_effect=AssertionError("retired request reached filesystem")):
                    with self.assertRaises(controller.worker.Refusal):
                        controller.Controller(dict(request, version=version), "a" * 64, material)

    def test_the_worker_compares_the_root_wwn_go_names_and_the_root_serial_number(self):
        hardware = {"interfaces": [{"macAddress": "52:54:00:9A:1B:01"}], "serialNumber": "ZC1A2B3C"}
        request = {"targets": [{"method": "metadataClear", "serialNumber": "ZC1A2B3C", "sizeBytes": 8192}], "deadlineSeconds": 14400,
                   "install": {"target": {"hardware": hardware}}}
        instance = object.__new__(controller.Controller)
        instance.request, instance.digest = request, "a" * 64
        instance.material = {"pinnedUUIDBase64": base64.b64encode(b"Example-System\n").decode(),
                             "rootWWNBase64": base64.b64encode(b"naa.5000c500a1b2c3d4").decode()}
        worker_request = instance.worker_for("remove/erase/1")
        controller.worker.validate_request(worker_request)
        self.assertEqual(worker_request["root"], {"wwn": "naa.5000c500a1b2c3d4", "serialNumber": "ZC1A2B3C"})
        self.assertEqual((worker_request["uuid"], worker_request["macs"]), ("example-system", ["52:54:00:9a:1b:01"]))
        del hardware["serialNumber"]
        instance.material["rootWWNBase64"] = ""
        self.assertEqual(instance.worker_for("remove/erase/1")["root"], {"wwn": "", "serialNumber": ""})
        for material in ({}, {"rootWWNBase64": "not base64!"}):
            instance.material = dict(instance.material, **material)
            if not material:
                del instance.material["rootWWNBase64"]
            with self.subTest(material=material), self.assertRaisesRegex(controller.worker.Refusal, "root"):
                instance.worker_for("remove/erase/1")

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


class UnreachableWorkerTests(unittest.TestCase):
    """An await that no session answers refuses within its bound, never after the four-hour deadline."""

    def awaiting(self, answers):
        import json

        now = [0.0]
        clock = SimpleNamespace(monotonic=lambda: now[0], sleep=lambda seconds: now.__setitem__(0, now[0] + seconds))
        instance = controller.Controller.__new__(controller.Controller)
        instance.digest = "a" * 64
        instance.worker_request = {"originalAttempt": "remove/erase/1"}
        instance.request = {"deadlineSeconds": 14400}
        instance.root = Path("/nonexistent")
        instance.lock = -1
        instance.load = lambda: None
        instance.receipt = lambda: None
        stream = iter(answers)

        def session(command):
            state = next(stream, None)
            if state is None:
                return controller.ssh.Outcome(255, b"", False, False, "refused")
            return controller.ssh.Outcome(0, json.dumps({"state": state, "request": instance.digest, "originalAttempt": "remove/erase/1"}).encode(),
                                          False, False, None)

        instance.session = session
        return instance, clock, now

    def test_a_worker_that_never_answers_refuses_after_the_startup_allowance(self):
        instance, clock, now = self.awaiting([])
        with patch.object(controller, "time", clock), self.assertRaises(controller.redfish.Refused) as caught:
            instance.await_receipt()
        self.assertEqual((str(caught.exception), caught.exception.detail), ("worker-unreachable", "refused:20min"))
        self.assertGreaterEqual(now[0], controller.FIRST_ANSWER_SECONDS)
        self.assertLess(now[0], controller.FIRST_ANSWER_SECONDS + 10)

    def test_a_worker_that_answered_and_went_silent_refuses_after_ten_minutes(self):
        instance, clock, now = self.awaiting(["partial"] * 3)
        with patch.object(controller, "time", clock), self.assertRaises(controller.redfish.Refused) as caught:
            instance.await_receipt()
        self.assertEqual((str(caught.exception), caught.exception.detail), ("worker-unreachable", "refused:10min"))
        self.assertGreaterEqual(now[0], 10 + controller.SILENCE_SECONDS)
        self.assertLess(now[0], 10 + controller.SILENCE_SECONDS + 10)

    def test_a_transient_failure_between_answers_does_not_refuse(self):
        instance, clock, _now = self.awaiting(["partial", None, "partial", "complete"])
        with (
            patch.object(controller, "time", clock),
            patch.object(controller.worker, "validate_receipt"),
            patch.object(controller, "write"),
            patch.object(controller.os, "fsync"),
        ):
            self.assertEqual(instance.await_receipt()["state"], "complete")

    def test_an_unanswered_session_is_classified_without_its_output(self):
        refused = b"ssh: connect to host 192.0.2.10 port 22: Connection refused\r\n"
        cases = (
            ((255, b"", refused), (255, "refused")),
            ((255, b"", b"Host key verification failed.\r\n"), (255, "host-key")),
            ((255, b"", b"root@192.0.2.10: Permission denied (publickey).\r\n"), (255, "auth")),
            ((255, b"", b"ssh: connect to host 192.0.2.10 port 22: No route to host\r\n"), (255, "no-route")),
            ((255, b"", b"something else\r\n"), (255, "exit")),
            ((1, b"", b""), (1, None)),
        )
        for answer, expected in cases:
            with self.subTest(answer=answer), patch.object(controller.bounded_process, "run", return_value=answer):
                outcome = controller.contact(["/usr/bin/ssh"])
                self.assertEqual((outcome.code, outcome.cause), expected)
                self.assertNotIn(b"192.0.2.10", outcome.stdout)
        with patch.object(controller.bounded_process, "run", side_effect=controller.bounded_process.Failure("timeout")):
            self.assertEqual(controller.contact(["/usr/bin/ssh"]).cause, "timeout")


class RemovalCustodyTests(unittest.TestCase):
    """Custody belongs to one removal: a fresh removal never boots under, or adopts, an earlier one's attempt."""

    OPERATION = "op-" + "0" * 32

    def prepared(self, temporary, attempt):
        import contextlib

        hosted, publication = Path(temporary) / "hosted", Path(temporary) / "published"
        publication.mkdir(exist_ok=True)
        request = {"version": "physical-erase-v1", "network": "network --bootproto=dhcp",
                   "install": {"identity": {"context": "lab", "object": "metal-01"}, "address": "192.0.2.10",
                               "private": {"path": str(publication), "url": "https://artifact.example.test/private"}}}
        material = {"applyOperation": self.OPERATION, "originalAttempt": attempt, "authorizedKey": "ssh-ed25519 AAAA fleet"}

        def keygen(argv, timeout=60):
            if argv[-2] == "-f" and "-y" not in argv:
                Path(argv[-1]).write_text("private host key " + attempt)
                Path(argv[-1] + ".pub").write_text("public host key")
            return b"ecdsa-sha2-nistp256 AAAA host"

        with contextlib.ExitStack() as stack:
            for target, name, value in (
                (controller, "ROOT", hosted),
                (controller, "directory", lambda path, create, mode=0o700: path.mkdir(mode=0o700, exist_ok=True) if create else None),
                (controller, "read", lambda path, limit=32768: Path(path).read_bytes()),
                (controller, "native", keygen),
                (controller.worker, "validate_request", lambda value: None),
                (controller.worker, "validate_receipt", lambda request, receipt: None),
                (controller.Controller, "worker_for", lambda self, original: {"originalAttempt": original, "request": self.digest}),
                (controller.ssh_key_policy, "read_policy", lambda: {}),
                (controller.ssh_key_policy, "effective_policy", lambda policy: {"configuration": "Host *\n"}),
                (controller.ssh_key_policy, "check_keys", lambda policy, keys: None),
                (controller.boot, "kickstart", lambda network, worker_request, key, authorized: "kickstart " + worker_request["originalAttempt"]),
                (controller.Controller, "session", lambda self, command, data=b"": controller.ssh.Outcome(255, b"", False, False, "refused")),
            ):
                stack.enter_context(patch.object(target, name, value))
            instance = controller.Controller(request, "a" * 64, material, create=True)
            try:
                return instance.prepare(), instance.root, publication
            finally:
                instance.close()

    def test_a_fresh_removal_withdraws_the_earlier_removals_custody_and_boots_under_its_own_attempt(self):
        import json

        with tempfile.TemporaryDirectory() as temporary:
            first, root, publication = self.prepared(temporary, "op-remove-1/erase-metal-01/1")
            earlier = json.loads((root / "custody.json").read_text())
            (publication / earlier["token"]).mkdir()
            (publication / earlier["token"] / "erase.iso").write_bytes(b"earlier private image")
            (root / "receipt.json").write_text(json.dumps({"state": "complete", "originalAttempt": earlier["originalAttempt"]}))
            again, _root, _publication = self.prepared(temporary, "op-remove-1/erase-metal-01/2")
            self.assertEqual(again["private"], first["private"], "a retry of the same removal keeps its custody")
            self.assertIsNotNone(again["receipt"])

            fresh, _root, _publication = self.prepared(temporary, "op-remove-2/erase-metal-01/1")
            custody = json.loads((root / "custody.json").read_text())
            self.assertEqual(custody["originalAttempt"], "op-remove-2/erase-metal-01/1")
            self.assertEqual(custody["worker"]["originalAttempt"], "op-remove-2/erase-metal-01/1")
            self.assertNotEqual(custody["token"], earlier["token"])
            self.assertFalse((publication / earlier["token"]).exists(), "the earlier removal's private image is still published")
            self.assertIsNone(fresh["receipt"])
            self.assertEqual((root / "host-key").read_text(), "private host key op-remove-2/erase-metal-01/1")
            self.assertEqual((root / "ks.cfg").read_text(), "kickstart op-remove-2/erase-metal-01/1")
            superseded = json.loads((root / "superseded-op-remove-1.json").read_text())
            self.assertEqual(superseded["originalAttempt"], earlier["originalAttempt"])
            self.assertFalse((root / "receipt.json").exists())

    def test_a_completed_earlier_removal_keeps_its_custody(self):
        import json

        with tempfile.TemporaryDirectory() as temporary:
            _first, root, _publication = self.prepared(temporary, "op-remove-1/erase-metal-01/1")
            earlier = (root / "custody.json").read_bytes()
            (root / "completed.json").write_text(json.dumps({"removed": True}))
            self.prepared(temporary, "op-remove-2/erase-metal-01/1")
            self.assertEqual((root / "custody.json").read_bytes(), earlier)
            self.assertTrue((root / "completed.json").exists())
