"""The erase worker never selects a target or trusts incomplete coverage."""

import unittest
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process as process
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker


class MemoryDisk:
    def __init__(self, identity, content):
        self.identity = identity
        self.content = bytearray(content)
        self.events = []
        self.corrupt = False

    def prove(self, expected):
        self.events.append("prove")
        if self.identity != expected:
            raise worker.Refusal("disk-identity")

    def write(self, offset, data):
        self.events.append("write")
        self.content[offset : offset + len(data)] = data
        return len(data)

    def flush(self):
        self.events.append("flush")

    def read(self, offset, size):
        self.events.append("read")
        return bytes([1] * size) if self.corrupt else bytes(self.content[offset : offset + size])


class EraseWorkerTests(unittest.TestCase):
    def setUp(self):
        self.target = {"deviceName": "/dev/example", "wwn": "wwn-example", "serialNumber": "serial-example", "sizeBytes": 8192}

    def test_identity_mismatch_precedes_every_write(self):
        disk = MemoryDisk(dict(self.target, serialNumber="foreign"), b"x" * 8192)
        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, self.target, lambda: None)
        self.assertNotIn("write", disk.events)

    def test_full_write_flush_and_full_readback_are_required(self):
        disk = MemoryDisk(self.target, b"x" * 8192)
        self.assertEqual(worker.overwrite(disk, self.target, lambda: None), {"written": 8192, "verified": 8192})
        self.assertEqual(disk.content, bytes(8192))
        self.assertLess(disk.events.index("flush"), disk.events.index("read"))

    def test_corrupt_readback_never_completes(self):
        disk = MemoryDisk(self.target, b"x" * 8192)
        disk.corrupt = True
        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, self.target, lambda: None)

    def test_cancellation_before_first_write_has_no_effect(self):
        disk = MemoryDisk(self.target, b"x" * 8192)

        def cancel():
            raise worker.Refusal("deadline")

        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, self.target, cancel)
        self.assertNotIn("write", disk.events)

    def test_receipt_requires_original_attempt_and_exact_all_disk_coverage(self):
        request = {"originalAttempt": "attempt-1", "request": "a" * 64, "uuid": "uuid-example", "macs": ["02:00:00:00:00:01"], "targets": [self.target]}
        receipt = worker.receipt_for(request, [{"written": 8192, "verified": 8192}])
        worker.validate_receipt(request, receipt)
        for field, value in [("originalAttempt", "attempt-2"), ("targets", []), ("state", "partial")]:
            with self.subTest(field=field), self.assertRaises(worker.Refusal):
                worker.validate_receipt(request, dict(receipt, **{field: value}))


if __name__ == "__main__":
    unittest.main()


class EraseBoundaryTests(unittest.TestCase):
    def request(self):
        return {
            "version": worker.VERSION,
            "originalAttempt": "remove/erase/1",
            "request": "a" * 64,
            "uuid": "uuid-example",
            "macs": ["02:00:00:00:00:01"],
            "deadlineSeconds": 60,
            "targets": [{"deviceName": "/dev/example", "wwn": "wwn-example", "serialNumber": "serial-example", "sizeBytes": 8192}],
        }

    def test_every_target_is_proved_before_first_write(self):
        from unittest.mock import patch

        request = self.request()
        request["targets"].append(dict(request["targets"][0], deviceName="/dev/other", wwn="other", serialNumber="other"))
        disk = MemoryDisk(request["targets"][0], b"x" * 8192)
        disk.dev = 1
        disk.close = lambda: disk.events.append("close")

        def open_disk(expected):
            if expected["deviceName"] == "/dev/other":
                raise worker.Refusal("foreign-target")
            return disk

        with patch.object(worker, "atomic"), self.assertRaises(worker.Refusal):
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None)
        self.assertNotIn("write", disk.events)
        self.assertIn("close", disk.events)

    def test_duplicate_device_aliases_refuse_before_writes(self):
        from unittest.mock import patch

        request = self.request()
        request["targets"].append(dict(request["targets"][0], deviceName="/dev/other", wwn="other", serialNumber="other"))
        disks = []

        def open_disk(expected):
            disk = MemoryDisk(expected, b"x" * 8192)
            disk.dev = 1
            disk.close = lambda: None
            disks.append(disk)
            return disk

        with patch.object(worker, "atomic"), self.assertRaises(worker.Refusal):
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None)
        self.assertTrue(all("write" not in disk.events for disk in disks))

    def test_short_write_never_reaches_flush_or_receipt(self):
        disk = MemoryDisk(self.request()["targets"][0], b"x" * 8192)
        disk.write = lambda offset, data: len(data) - 1
        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, disk.identity, lambda: None)
        self.assertNotIn("flush", disk.events)

    def test_hotplug_between_chunks_refuses(self):
        target = dict(self.request()["targets"][0], sizeBytes=2 * worker.CHUNK)
        disk = MemoryDisk(target, b"x" * target["sizeBytes"])
        initial = disk.write

        def replace(offset, data):
            result = initial(offset, data)
            disk.identity = dict(target, serialNumber="replacement")
            return result

        disk.write = replace
        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, target, lambda: None)
        self.assertEqual(disk.events.count("write"), 1)
        self.assertNotIn("flush", disk.events)

    def test_bootstrap_isolation_failure_never_starts_writer(self):
        from unittest.mock import Mock, patch
        from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot

        # The actual embedded runtime is loaded, with no __main__ entrypoint.
        import sys

        with patch.dict(sys.modules, {"physical_erase_worker": worker, "physical_erase_process": process}):
            namespace = {"__name__": "test_runtime"}
            exec(compile(boot.RUNTIME, "erase-runtime", "exec"), namespace)
        namespace["isolate"] = Mock(side_effect=worker.Refusal("installer-running"))
        with patch.object(worker, "execute") as execute, self.assertRaises(worker.Refusal):
            namespace["run"]()
        execute.assert_not_called()

    def test_boot_image_contains_only_network_and_nonreturning_pre(self):
        from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot

        rendered = boot.kickstart("network --bootproto=static --device=eno1", self.request(), "PRIVATE", "ssh-key PUBLIC")
        before, pre = rendered.split("%pre", 1)
        self.assertEqual(before.strip(), "network --bootproto=static --device=eno1")
        self.assertIn("--erroronfail", pre)
        self.assertIn("trap fail_closed EXIT HUP INT TERM", pre)
        self.assertTrue(pre.endswith("fail_closed\n%end\n"))
        self.assertNotIn("clearpart", before)
        self.assertNotIn("%post", rendered)

    def test_request_refuses_missing_or_duplicate_disk_identities(self):
        request = self.request()
        worker.validate_request(request)
        for value in [True, 0, 8193, 65 * 1024**4]:
            with self.subTest(size=value), self.assertRaises(worker.Refusal):
                changed = dict(request, targets=[dict(request["targets"][0], sizeBytes=value)])
                worker.validate_request(changed)


class WorkerLivenessTests(unittest.TestCase):
    def test_stale_partial_from_a_stopped_service_is_not_live_work(self):
        import json
        from pathlib import Path
        import sys
        import tempfile
        from unittest.mock import Mock, patch
        from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot

        with patch.dict(sys.modules, {"physical_erase_worker": worker, "physical_erase_process": process}):
            namespace = {"__name__": "test_runtime"}
            exec(compile(boot.RUNTIME, "erase-runtime", "exec"), namespace)
        with tempfile.TemporaryDirectory() as temporary:
            namespace["ROOT"] = Path(temporary)
            (Path(temporary) / "status.json").write_text(json.dumps({"state": "partial", "request": "a" * 64, "originalAttempt": "remove/erase/1"}))
            for service, expected in (("active", "partial"), ("failed", "failed"), ("inactive", "failed")):
                with self.subTest(service=service):
                    namespace["command"] = Mock(return_value=service)
                    self.assertEqual(namespace["status"]()["state"], expected)
                    self.assertEqual(namespace["command"].call_args.args[0][-1], "bootwright-erase.service")


class WorkerSourceAuthorityTests(unittest.TestCase):
    def test_standalone_worker_uses_frozen_sibling_when_an_ambient_collection_exists(self):
        import inspect
        import sys
        import types
        from unittest.mock import Mock, patch
        from ansible_collections.bootwright.core.plugins import module_utils
        from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity

        frozen = types.ModuleType("physical_machine_identity")
        exec(compile(inspect.getsource(physical_machine_identity), "frozen-identity", "exec"), frozen.__dict__)
        ambient = types.ModuleType("ambient_identity")
        ambient.prove = Mock(side_effect=AssertionError("ambient source must not identify erase targets"))
        prefix = "ansible_collections.bootwright.core.plugins.module_utils"
        modules = {"physical_machine_identity": frozen, prefix + ".physical_machine_identity": ambient}
        namespace = {"__name__": "physical_erase_worker", "__package__": ""}
        with patch.dict(sys.modules, modules), patch.object(module_utils, "physical_machine_identity", ambient):
            exec(compile(inspect.getsource(worker), "frozen-worker", "exec"), namespace)
        self.assertIs(namespace["physical_machine_identity"], frozen)
        ambient.prove.assert_not_called()

    def test_packaged_worker_keeps_the_collection_loader_as_its_source_authority(self):
        import inspect
        import sys
        import types
        from unittest.mock import patch

        ambient = types.ModuleType("physical_machine_identity")
        namespace = {"__name__": worker.__name__, "__package__": worker.__package__}
        with patch.dict(sys.modules, {"physical_machine_identity": ambient}):
            exec(compile(inspect.getsource(worker), "packaged-worker", "exec"), namespace)
        self.assertIs(namespace["physical_machine_identity"], worker.physical_machine_identity)
        self.assertIsNot(namespace["physical_machine_identity"], ambient)
