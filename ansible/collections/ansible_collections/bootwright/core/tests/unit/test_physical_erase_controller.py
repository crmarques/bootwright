"""Image custody never reopens a replaced operator source or publishes stages."""

import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import secrets
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as controller
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_runtime


def runtime_required_flags():
    """The boot arguments the RAM runtime requires before it starts, read from its source."""
    import ast

    tree = ast.parse(Path(physical_erase_runtime.__file__).read_text())
    for node in ast.walk(tree):
        if isinstance(node, ast.Assign) and [getattr(target, "id", "") for target in node.targets] == ["required"]:
            return set(ast.literal_eval(node.value))
    raise AssertionError("the runtime names no required boot arguments")


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
            with patch.object(controller, "directory", side_effect=AssertionError("unsupported request reached filesystem")):
                with self.assertRaises(controller.worker.Refusal):
                    controller.Controller(dict(request, version="physical-erase-v2"), "a" * 64, material)

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
                "network": "network --bootproto=static --device=eno1 --vlanid=20 --interfacename=eno1.20 --ip=192.0.2.10 "
                           "--netmask=255.255.255.0 --hostname=metal-01 --onboot=on --activate",
                "install": {
                    "bootMedia": {"name": "boot", "size": len(original), "sha256": hashlib.sha256(original).hexdigest()},
                    "installerArguments": ["ifname=eno1:52:54:00:00:00:01"],
                    "private": {"path": str(published)},
                },
            }
            token = "a" * 64
            (published / token).mkdir()
            instance.load = lambda: {"token": token}
            observed, cmdlines, removed = [], [], []

            def build(argv, **unused):
                held = argv[-2]
                observed.append(held)
                cmdlines.append(argv[argv.index("--cmdline") + 1].split())
                removed.append(argv[argv.index("--rm-args") + 1].split())
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
            self.assertEqual(cmdlines[0][0], "ifname=eno1:52:54:00:00:00:01")
            self.assertEqual(cmdlines[0][-3:], ["vlan=eno1.20:eno1", "ip=192.0.2.10:::255.255.255.0:metal-01:eno1.20:none", "rd.neednet=1"])
            # systemd's zram generator swaps on every installer boot unless
            # systemd.zram=0, and any active swap refuses the worker; the
            # media's quiet and rhgb would hide the boot on the console.
            self.assertTrue({"inst.zram=0", "systemd.zram=0"} <= set(cmdlines[0]))
            self.assertTrue({"systemd.zram", "inst.zram", "quiet", "rhgb"} <= set(removed[0]))
            self.assertTrue(runtime_required_flags() <= set(cmdlines[0]))

    def test_an_install_network_the_boot_arguments_cannot_carry_refuses_before_the_image_changes(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            (work / "erase.iso").write_bytes(b"earlier image")
            instance = controller.Controller.__new__(controller.Controller)
            instance.root = work
            instance.request = {"network": "network --bootproto=static --device=bond0 --bondslaves=eno1 "
                                           "--bondopts=mode=active-backup;arp_ip_target=192.0.2.1,192.0.2.2 --ip=192.0.2.10 "
                                           "--netmask=255.255.255.0 --hostname=metal-01",
                                "install": {"bootMedia": {"name": "boot"}}}
            instance.load = lambda: {"token": "a" * 64}
            with patch.object(controller, "native") as native, patch.object(os, "open") as opened:
                with self.assertRaisesRegex(controller.worker.Refusal, "^install-network$"):
                    instance.build()
            native.assert_not_called()
            opened.assert_not_called()
            self.assertEqual((work / "erase.iso").read_bytes(), b"earlier image")

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
            instance.poll()
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


class PollTests(unittest.TestCase):
    """One poll is one bounded snapshot: the wait, its clock and its bounds belong to the physical_erase_await action."""

    TARGETS = [{"deviceName": "/dev/sda", "method": "logicalOverwrite", "serialNumber": "s0", "sizeBytes": 8 * 1024**3, "wwn": "w0"},
               {"method": "metadataClear", "serialNumber": "s1", "sizeBytes": 4 * 1024**4}]

    def polling(self, *answers):
        import json

        instance = controller.Controller.__new__(controller.Controller)
        instance.digest = "a" * 64
        instance.worker_request = {"originalAttempt": "remove/erase/1", "targets": self.TARGETS}
        instance.request = {"deadlineSeconds": 14400}
        instance.root = Path("/nonexistent")
        instance.lock = -1
        instance.load = lambda: None
        instance.receipt = lambda: None
        stream = iter(answers)

        def session(command):
            answer = next(stream)
            if isinstance(answer, controller.ssh.Outcome):
                return answer
            return controller.ssh.Outcome(0, json.dumps(answer).encode(), False, False, None)

        instance.session = session
        return instance

    def status(self, **changes):
        return dict({"state": "partial", "request": "a" * 64, "originalAttempt": "remove/erase/1"}, **changes)

    def test_a_partial_status_answers_each_targets_written_and_read_back_bytes_of_twice_its_coverage(self):
        coverage = controller.worker.covered(self.TARGETS[1])
        progress = [{"written": 8 * 1024**3, "verified": 1024**3}, {"written": 100, "verified": 0}]
        answer = self.polling(self.status(progress=progress)).poll()
        self.assertEqual(answer, {"answer": "partial", "progress": [{"completed": 9 * 1024**3, "total": 16 * 1024**3},
                                                                    {"completed": 100, "total": 2 * coverage}]})
        self.assertLess(coverage, self.TARGETS[1]["sizeBytes"], "a metadata clear's total is its merged regions, not the disk")

    def test_malformed_progress_is_ignored_and_never_refuses(self):
        coverage = controller.worker.covered(self.TARGETS[1])
        for reported in (None, "much", [], [{"written": 1, "verified": 0}], [{"written": 1, "verified": 0}, {"written": 1}],
                         [{"written": 1, "verified": 0}, {"written": coverage + 1, "verified": 0}],
                         [{"written": -1, "verified": 0}, {"written": 0, "verified": 0}],
                         [{"written": True, "verified": 0}, {"written": 0, "verified": 0}],
                         [{"written": 1.5, "verified": 0}, {"written": 0, "verified": 0}],
                         [{"written": 1, "verified": 0, "extra": 1}, {"written": 0, "verified": 0}]):
            with self.subTest(reported=reported):
                self.assertEqual(self.polling(self.status(progress=reported)).poll(), {"answer": "partial", "progress": None})

    def test_unanswered_and_answered_sessions_and_refusals(self):
        unanswered = controller.ssh.Outcome(255, b"", False, False, "host-key")
        self.assertEqual(self.polling(unanswered).poll(), {"answer": "unanswered", "cause": "host-key"})
        self.assertEqual(self.polling(controller.ssh.Outcome(None, b"", False, True, None)).poll(), {"answer": "unanswered", "cause": "exit"})
        self.assertEqual(self.polling(controller.ssh.Outcome(1, b"", False, False, None)).poll(), {"answer": "answered"})
        self.assertEqual(self.polling(controller.ssh.Outcome(0, b"", False, False, None)).poll(), {"answer": "answered"})
        with self.assertRaisesRegex(controller.worker.Refusal, "foreign-worker"):
            self.polling(self.status(originalAttempt="remove-0/erase/1", progress=[])).poll()
        with self.assertRaisesRegex(controller.worker.Refusal, "worker-failed"):
            self.polling(self.status(state="failed")).poll()

    def test_a_complete_receipt_is_validated_and_kept_before_it_is_answered(self):
        receipt = {"state": "complete", "request": "a" * 64}
        instance = self.polling(receipt)
        with patch.object(controller.worker, "validate_receipt") as validate, patch.object(controller, "write") as write, \
                patch.object(controller.os, "fsync"):
            self.assertEqual(instance.poll(), {"answer": "complete", "receipt": receipt})
        validate.assert_called_once_with(instance.worker_request, receipt)
        self.assertEqual(write.call_args.args[0], instance.root / "receipt.json")
        kept = self.polling()
        kept.receipt = lambda: receipt
        self.assertEqual(kept.poll(), {"answer": "complete", "receipt": receipt})

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

    def prepared(self, temporary, attempt, answer=None):
        import contextlib

        answer = answer or controller.ssh.Outcome(255, b"", False, False, "refused")

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
                (controller.Controller, "session", lambda self, command, data=b"": answer),
            ):
                stack.enter_context(patch.object(target, name, value))
            instance = controller.Controller(request, "a" * 64, material, create=True)
            try:
                return instance.prepare(), instance.root, publication
            finally:
                instance.close()

    def test_a_worker_stopped_before_its_first_status_refuses_worker_failed_not_foreign(self):
        import json

        stopped = controller.ssh.Outcome(0, json.dumps({"state": "failed"}).encode(), False, False, None)
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(controller.worker.Refusal, "^worker-failed$"):
                self.prepared(temporary, "op-remove-1/erase-metal-01/1", stopped)
        polled = PollTests().polling({"state": "failed"})
        with self.assertRaisesRegex(controller.worker.Refusal, "^worker-failed$"):
            polled.poll()

    def test_a_retry_of_the_same_removal_boots_the_running_builds_kickstart_under_its_frozen_custody(self):
        import json

        with tempfile.TemporaryDirectory() as temporary:
            first, root, _publication = self.prepared(temporary, "op-remove-1/erase-metal-01/1")
            custody = (root / "custody.json").read_bytes()
            key = (root / "host-key").read_bytes()
            (root / "ks.cfg").write_text("a stale kickstart")
            again, _root, _publication = self.prepared(temporary, "op-remove-1/erase-metal-01/2")
            self.assertEqual(again["private"], first["private"])
            self.assertEqual((root / "custody.json").read_bytes(), custody)
            self.assertEqual((root / "host-key").read_bytes(), key)
            self.assertEqual((root / "ks.cfg").read_text(), "kickstart op-remove-1/erase-metal-01/1")
            self.assertEqual(json.loads(custody)["worker"]["originalAttempt"], "op-remove-1/erase-metal-01/1")

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
            superseded = json.loads((root / "superseded-op-remove-1-receipt.json").read_text())
            self.assertEqual(superseded["originalAttempt"], earlier["originalAttempt"])
            self.assertFalse((root / "receipt.json").exists())

    def test_a_completed_earlier_removal_is_set_aside_and_a_fresh_removal_boots_under_its_own_attempt(self):
        import json

        for marker in ("completed", "finalizing"):
            with self.subTest(marker=marker), tempfile.TemporaryDirectory() as temporary:
                _first, root, publication = self.prepared(temporary, "op-remove-1/erase-metal-01/1")
                earlier = json.loads((root / "custody.json").read_text())
                receipt = {"state": "complete", "originalAttempt": earlier["originalAttempt"]}
                evidence = {"removed": True, "receipt": receipt}
                (root / "receipt.json").write_text(json.dumps(receipt))
                (root / "finalizing.json").write_text(json.dumps(evidence))
                if marker == "completed":
                    (root / "completed.json").write_text(json.dumps(evidence))
                (publication / earlier["token"]).mkdir()
                for name in ("host-key", "host-key.pub", "ks.cfg", "ssh_config", "known_hosts"):
                    (root / name).unlink()
                instance = controller.Controller.__new__(controller.Controller)
                instance.root, instance.material = root, {"originalAttempt": "op-remove-2/erase-metal-01/1"}
                with patch.object(controller, "read", lambda path, limit=32768: Path(path).read_bytes()):
                    self.assertTrue(instance.earlier(), "an earlier removal's completed custody read as this removal's")

                fresh, _root, _publication = self.prepared(temporary, "op-remove-2/erase-metal-01/1")
                custody = json.loads((root / "custody.json").read_text())
                self.assertEqual((custody["originalAttempt"], custody["worker"]["originalAttempt"]), ("op-remove-2/erase-metal-01/1",) * 2)
                self.assertNotEqual(custody["token"], earlier["token"])
                self.assertIsNone(fresh["receipt"], "the fresh removal adopted the earlier receipt")
                self.assertEqual((root / "ks.cfg").read_text(), "kickstart op-remove-2/erase-metal-01/1")
                self.assertFalse((publication / earlier["token"]).exists())
                for name in ("receipt.json", "finalizing.json", "completed.json"):
                    self.assertFalse((root / name).exists(), name)
                self.assertEqual(json.loads((root / "superseded-op-remove-1-receipt.json").read_text()), receipt)
                self.assertEqual(json.loads((root / "superseded-op-remove-1-finalizing.json").read_text()), evidence)
                self.assertEqual((root / "superseded-op-remove-1-completed.json").exists(), marker == "completed")
                instance.material = {"originalAttempt": "op-remove-2/erase-metal-01/2"}
                with patch.object(controller, "read", lambda path, limit=32768: Path(path).read_bytes()):
                    self.assertFalse(instance.earlier(), "a retry of the fresh removal is its own")


VECTORS = json.loads((Path(__file__).resolve().parent / "goldens/physical_erase_relaxation.json").read_text(encoding="utf-8"))


class RelaxationTests(unittest.TestCase):
    """The worker clears what Go validates: both sides read goldens/physical_erase_relaxation.json."""

    def instance(self, **material):
        hardware = {"interfaces": [{"macAddress": "52:54:00:9A:1B:01"}], "serialNumber": "ROOT"}
        instance = object.__new__(controller.Controller)
        instance.request = {"targets": VECTORS["declared"], "deadlineSeconds": 14400, "install": {"target": {"hardware": hardware}}}
        instance.digest = "a" * 64
        instance.material = dict({"pinnedUUIDBase64": base64.b64encode(b"example-system").decode(),
                                  "rootWWNBase64": base64.b64encode(b"0x5000c500a1b2c3d4").decode()}, **material)
        return instance

    def test_the_relaxation_decodes_strictly_and_a_missing_one_is_none(self):
        self.assertEqual(controller.relaxation({}), "")
        self.assertEqual(controller.relaxation({"relaxationBase64": VECTORS["retain"]}), "retain")
        for vector in VECTORS["vectors"]:
            if vector["relaxation"]:
                self.assertEqual(controller.relaxation({"relaxationBase64": vector["relaxationBase64"]}), vector["relaxation"])
        for encoded in VECTORS["refused"] + [None, 7, ["cmV0YWlu"]]:
            with self.subTest(encoded=encoded), self.assertRaisesRegex(controller.worker.Refusal, "relaxation"):
                controller.relaxation({"relaxationBase64": encoded})

    def test_the_worker_request_holds_the_shared_vectors_relaxed_targets_and_coverage(self):
        for vector in VECTORS["vectors"]:
            material = {"relaxationBase64": vector["relaxationBase64"]} if vector["relaxation"] else {}
            with self.subTest(relaxation=vector["relaxation"]):
                instance = self.instance(**material)
                worker_request = instance.worker_for("op-remove-1/erase-metal-01/1")
                controller.worker.validate_request(worker_request)
                self.assertEqual(worker_request["targets"], vector["targets"])
                self.assertEqual([controller.worker.covered(target) for target in worker_request["targets"]], vector["coverage"])
                instance.worker_request = worker_request
                reported = [{"written": 0, "verified": 0}] * len(vector["targets"])
                self.assertEqual(instance.progress(reported), [{"completed": 0, "total": 2 * size} for size in vector["coverage"]])
        self.assertEqual(VECTORS["declared"][2]["method"], "logicalOverwrite", "relaxation copied the targets, never the frozen request")

    def test_a_retained_or_damaged_relaxation_builds_no_worker_request(self):
        for encoded in (VECTORS["retain"], VECTORS["refused"][0]):
            with self.subTest(encoded=encoded), self.assertRaisesRegex(controller.worker.Refusal, "relaxation"):
                self.instance(relaxationBase64=encoded).worker_for("op-remove-1/erase-metal-01/1")

    def test_custody_of_an_unrelaxed_attempt_is_foreign_to_a_relaxed_one(self):
        unrelaxed = self.instance(applyOperation="op-" + "0" * 32)
        state = {"request": "a" * 64, "applyOperation": "op-" + "0" * 32, "originalAttempt": "op-remove-1/erase-metal-01/1",
                 "worker": unrelaxed.worker_for("op-remove-1/erase-metal-01/1")}
        relaxed = self.instance(applyOperation="op-" + "0" * 32, relaxationBase64=VECTORS["vectors"][1]["relaxationBase64"])
        relaxed.root = Path("/nonexistent")
        with patch.object(controller, "read", return_value=json.dumps(state).encode()):
            with self.assertRaisesRegex(controller.worker.Refusal, "foreign-target"):
                relaxed.load()
            self.assertEqual(unrelaxed.worker_for(state["originalAttempt"]), state["worker"])


class WithdrawalTests(unittest.TestCase):
    """A retained erase withdraws only what this block's custody left, and never boots or reaches the BMC."""

    OPERATION = "op-" + "0" * 32
    MINE = "op-remove-2/erase-metal-01/1"

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        base = Path(temporary.name)
        self.hosted, self.publication = base / "hosted", base / "published"
        self.publication.mkdir()
        stack = contextlib.ExitStack()
        self.addCleanup(stack.close)
        for name, value in (
            ("ROOT", self.hosted),
            ("directory", lambda path, create, mode=0o700: path.mkdir(mode=0o700, exist_ok=True) if create else path.lstat()),
            ("read", lambda path, limit=32768: Path(path).read_bytes()),
        ):
            stack.enter_context(patch.object(controller, name, value))
        for name in ("session", "control", "prepare", "build", "worker_for"):
            stack.enter_context(patch.object(controller.Controller, name, side_effect=AssertionError(name + " reached by a retained erase")))
        stack.enter_context(patch.object(controller.redfish, "Client", side_effect=AssertionError("management controller contacted")))
        self.request = {"version": "physical-erase-v1",
                        "install": {"identity": {"context": "lab", "object": "metal-01"}, "private": {"path": str(self.publication)}}}

    @contextlib.contextmanager
    def opened(self, create=False):
        material = {"relaxationBase64": VECTORS["retain"], "applyOperation": self.OPERATION, "originalAttempt": self.MINE}
        instance = controller.Controller(self.request, "a" * 64, material, create=create)
        try:
            yield instance
        finally:
            instance.close()

    def custody(self, attempt, **records):
        with self.opened(create=True) as instance:
            root = instance.root
        token = secrets.token_hex(32)
        (root / "custody.json").write_text(json.dumps({"request": "b" * 64, "applyOperation": self.OPERATION, "originalAttempt": attempt, "token": token}))
        for name in controller.BOOT_PRIVATE:
            (root / name).write_text("private")
        (self.publication / token).mkdir()
        (self.publication / token / "erase.iso").write_bytes(b"private image")
        for name, value in records.items():
            (root / (name + ".json")).write_text(json.dumps(value))
        return root, token

    def test_own_custody_is_withdrawn_and_its_records_kept_and_a_repeat_changes_nothing(self):
        root, token = self.custody(self.MINE.replace("/1", "/0"), receipt={"state": "partial"})
        with self.opened() as instance:
            self.assertFalse(instance.withdrawn())
            self.assertTrue(instance.withdraw())
            self.assertFalse((self.publication / token).exists())
            self.assertEqual({path.name for path in root.iterdir()}, {"custody.json", "receipt.json"})
            self.assertTrue(instance.withdrawn())
            self.assertFalse(instance.withdraw(), "a repeated withdrawal changed something")

    def test_another_removals_custody_is_withdrawn_and_its_records_set_aside(self):
        receipt, evidence = {"state": "complete"}, {"removed": True}
        root, token = self.custody("op-remove-1/erase-metal-01/3", receipt=receipt, finalizing=evidence, completed=evidence)
        with self.opened() as instance:
            self.assertFalse(instance.withdrawn(), "another removal's custody read as this removal's withdrawal")
            self.assertTrue(instance.withdraw())
            self.assertFalse((self.publication / token).exists())
            self.assertEqual({path.name for path in root.iterdir()},
                             {"superseded-op-remove-1-receipt.json", "superseded-op-remove-1-finalizing.json", "superseded-op-remove-1-completed.json"})
            self.assertEqual(json.loads((root / "superseded-op-remove-1-receipt.json").read_text()), receipt)
            self.assertTrue(instance.withdrawn())
            self.assertFalse(instance.withdraw())

    def test_only_the_last_removals_set_aside_keep_their_superseded_records(self):
        for index in range(1, 7):
            root, _token = self.custody("op-remove-%d/erase-metal-01/1" % index, receipt={"index": index}, completed={"index": index})
            with self.opened() as instance:
                self.assertTrue(instance.withdraw())
        kept = sorted(path.name for path in root.iterdir())
        expected = sorted("superseded-op-remove-%d-%s.json" % (index, record) for index in range(3, 7) for record in ("receipt", "completed"))
        self.assertEqual(kept, expected)
        self.assertEqual(controller.SUPERSEDED_KEPT, 4)
        self.assertEqual(json.loads((root / "superseded-op-remove-6-receipt.json").read_text()), {"index": 6})

    def test_no_custody_leaves_nothing_to_withdraw(self):
        with self.assertRaises(FileNotFoundError), self.opened():
            pass
        with self.opened(create=True) as instance:
            self.assertTrue(instance.withdrawn())
            self.assertFalse(instance.withdraw())
            self.assertEqual(list(instance.root.iterdir()), [])

    def test_a_leftover_boot_private_file_or_publication_is_not_proved_withdrawn(self):
        root, token = self.custody(self.MINE)
        with self.opened() as instance:
            for name in controller.BOOT_PRIVATE:
                (root / name).unlink()
            self.assertFalse(instance.withdrawn(), "the publication is still served")
            (self.publication / token / "erase.iso").unlink()
            (self.publication / token).rmdir()
            self.assertTrue(instance.withdrawn())
            (root / "ks.cfg").write_text("private")
            self.assertFalse(instance.withdrawn(), "a boot-private file is left")

    def test_custody_naming_no_token_or_attempt_refuses_without_withdrawing(self):
        root, token = self.custody(self.MINE)
        for state in ({"token": "../escape", "originalAttempt": self.MINE}, {"token": token, "originalAttempt": "unnamed"}, ["not", "a", "record"]):
            (root / "custody.json").write_text(json.dumps(state))
            with self.subTest(state=state), self.opened() as instance, self.assertRaisesRegex(controller.worker.Refusal, "custody-token"):
                instance.withdraw()
        self.assertTrue((self.publication / token / "erase.iso").exists())
        self.assertTrue((root / "host-key").exists())


class MachinePortMaterial(unittest.TestCase):
    """The controller's account and trust are read from the material the frozen
    machine port names, never from a name this module spells itself."""

    def started(self, port):
        instance = object.__new__(controller.Controller)
        config = {"endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "tlsVerification": "full",
                  "virtualMedia": {"trust": "lent", "enableVerification": True}}
        instance.request = {"install": {"target": {"controller": config, "machinePort": port},
                                        "private": {"url": "https://artifacts.example.test/private"}}}
        instance.material = {"portUser": "/lent/user", "portPassword": "/lent/password", "portCA": "/lent/ca", "artifactCertificate": "/lent/artifact"}
        instance.worker_request = {}
        contents = {"/lent/user": b"synthetic user", "/lent/password": b"synthetic password", "/lent/ca": b"synthetic ca",
                    "/lent/artifact": b"synthetic artifact"}
        started = []
        with patch.object(controller.Controller, "load", return_value={"token": "t" * 64}), \
                patch.object(controller, "read", side_effect=lambda path: contents[str(path)]), \
                patch.object(controller.redfish, "start", side_effect=lambda factory, *arguments, **keywords: started.append(factory)), \
                patch.object(controller.redfish, "Client", side_effect=lambda *arguments, **keywords: arguments[:4]):
            instance.control("start")
            return started[0](60)

    def test_the_controller_client_reads_the_material_the_machine_port_names(self):
        port = {"userVariable": "portUser", "passwordVariable": "portPassword", "endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "verify": True}
        for ca_variable, ca in (("portCA", "synthetic ca"), ("", "")):
            with self.subTest(ca_variable=ca_variable):
                named = dict(port, caVariable=ca_variable) if ca_variable else port
                self.assertEqual(self.started(named), ("https://bmc.example.test/redfish/v1/Systems/1", "synthetic user", "synthetic password", ca))

    def test_a_request_whose_machine_port_names_no_account_is_refused(self):
        for port in (None, {}, {"userVariable": "portUser"}, {"userVariable": "", "passwordVariable": "portPassword"},
                     {"userVariable": "portUser", "passwordVariable": "portPassword", "caVariable": 1}, []):
            with self.subTest(port=port), self.assertRaisesRegex(controller.worker.Refusal, "request"):
                self.started(port)
