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

    def identify(self, expected):
        self.events.append("identify")
        if self.identity != expected:
            raise worker.Refusal("disk-identity")

    def idle(self):
        self.events.append("idle")
        if getattr(self, "busy", None) is not None and self.busy():
            raise worker.Refusal("disk-mounted")

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
        self.target = {"deviceName": "/dev/example", "method": "logicalOverwrite", "wwn": "wwn-example", "serialNumber": "serial-example", "sizeBytes": 8192}

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
            "targets": [
                {"deviceName": "/dev/example", "method": "logicalOverwrite", "wwn": "wwn-example", "serialNumber": "serial-example", "sizeBytes": 8192},
            ],
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
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None, release=lambda _: None)
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
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None, release=lambda _: None)
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

    def test_a_target_names_its_disk_by_path_and_wwn_or_by_serial_number_alone(self):
        request = self.request()
        serial = {key: request["targets"][0][key] for key in ("method", "serialNumber", "sizeBytes")}
        worker.validate_request(dict(request, targets=[serial]))
        for key in ("deviceName", "wwn"):
            with self.subTest(lone=key), self.assertRaisesRegex(worker.Refusal, "^target$"):
                worker.validate_request(dict(request, targets=[dict(serial, **{key: request["targets"][0][key]})]))
        with self.assertRaisesRegex(worker.Refusal, "target-identity"):
            worker.validate_request(dict(request, targets=[serial, dict(request["targets"][0])]))


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
        runner = types.ModuleType("physical_erase_process")
        exec(compile(inspect.getsource(process), "frozen-process", "exec"), runner.__dict__)
        ambient = types.ModuleType("ambient_identity")
        ambient.prove = Mock(side_effect=AssertionError("ambient source must not identify erase targets"))
        prefix = "ansible_collections.bootwright.core.plugins.module_utils"
        modules = {"physical_machine_identity": frozen, "physical_erase_process": runner, prefix + ".physical_machine_identity": ambient}
        namespace = {"__name__": "physical_erase_worker", "__package__": ""}
        with patch.dict(sys.modules, modules), patch.object(module_utils, "physical_machine_identity", ambient):
            exec(compile(inspect.getsource(worker), "frozen-worker", "exec"), namespace)
        self.assertIs(namespace["physical_machine_identity"], frozen)
        self.assertIs(namespace["physical_erase_process"], runner)
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


class SparseDisk:
    """A large disk whose untouched bytes read as nonzero."""

    def __init__(self, identity):
        self.identity = identity
        self.writes = []
        self.reads = []
        self.events = []

    def prove(self, expected):
        if self.identity != expected:
            raise worker.Refusal("disk-identity")

    identify = prove

    def write(self, offset, data):
        self.events.append("write")
        self.writes.append((offset, len(data)))
        return len(data)

    def flush(self):
        self.events.append("flush")

    def read(self, offset, size):
        self.events.append("read")
        self.reads.append((offset, size))
        return bytes(size) if (offset, size) in self.writes else b"\xff" * size


def spans(chunks):
    merged = []
    for offset, size in sorted(chunks):
        if merged and offset == merged[-1][1]:
            merged[-1] = (merged[-1][0], offset + size)
        else:
            merged.append((offset, offset + size))
    return merged


class EraseMethodTests(unittest.TestCase):
    WINDOW = 134217728
    VECTORS = (
        (4096, [(0, 4096)], 4096),
        (WINDOW, [(0, WINDOW)], WINDOW),
        (2 * WINDOW + 4096, [(0, WINDOW), (134221824, 2 * WINDOW + 4096)], 268435456),
        (1073741824, [(0, WINDOW), (939524096, 1073741824)], 268435456),
        (1073741824 + WINDOW // 2, [(0, WINDOW), (1006632960, 1140850688)], 268435456),
        (
            2000398934016,
            [(0, WINDOW), (1073741824, 1073741824 + WINDOW), (10737418240, 10737418240 + WINDOW), (107374182400, 107374182400 + WINDOW),
             (1073741824000, 1073741824000 + WINDOW), (2000264716288, 2000398934016)],
            805306368,
        ),
        (
            16000900661248,
            [(0, WINDOW), (1073741824, 1073741824 + WINDOW), (10737418240, 10737418240 + WINDOW), (107374182400, 107374182400 + WINDOW),
             (1073741824000, 1073741824000 + WINDOW), (16000766443520, 16000900661248)],
            805306368,
        ),
    )

    def target(self, size, method="metadataClear"):
        return {"deviceName": "/dev/example", "method": method, "wwn": "wwn-example", "serialNumber": "serial-example", "sizeBytes": size}

    def test_metadata_regions_match_the_shared_vectors(self):
        for size, regions, covered in self.VECTORS:
            with self.subTest(size=size):
                self.assertEqual(worker.metadata_regions(size), regions)
                self.assertEqual(worker.covered(self.target(size)), covered)
                self.assertTrue(all(start % 4096 == 0 and end % 4096 == 0 and start < end <= size for start, end in regions))
                self.assertEqual(worker.covered(self.target(size, "logicalOverwrite")), size)

    def test_metadata_clear_zeroes_and_reads_back_exactly_its_regions(self):
        size = 2000398934016
        target = self.target(size)
        disk = SparseDisk(target)
        self.assertEqual(worker.overwrite(disk, target, lambda: None), {"written": 805306368, "verified": 805306368})
        self.assertEqual(spans(disk.writes), worker.metadata_regions(size))
        self.assertEqual(disk.reads, disk.writes)
        self.assertEqual(disk.events.count("flush"), 1)
        self.assertLess(disk.events.index("flush"), disk.events.index("read"))
        self.assertGreater(disk.events.index("flush"), len(disk.writes) - 1)

    def test_metadata_clear_refuses_an_unread_region(self):
        target = self.target(2 * self.WINDOW + 4096)
        disk = SparseDisk(target)
        disk.read = lambda offset, size: b"\xff" * size if offset >= self.WINDOW else bytes(size)
        with self.assertRaises(worker.Refusal):
            worker.overwrite(disk, target, lambda: None)

    def test_receipt_records_each_method_and_its_exact_coverage(self):
        targets = [self.target(8192, "logicalOverwrite"), dict(self.target(2000398934016), deviceName="/dev/data", wwn="data", serialNumber="data")]
        request = {"originalAttempt": "attempt-1", "request": "a" * 64, "uuid": "uuid-example", "macs": ["02:00:00:00:00:01"], "targets": targets}
        receipt = worker.receipt_for(request, [{"written": 8192, "verified": 8192}, {"written": 805306368, "verified": 805306368}])
        self.assertEqual([target["method"] for target in receipt["targets"]], ["logicalOverwrite", "metadataClear"])
        worker.validate_receipt(request, receipt)
        for coverage in ({"written": 2000398934016, "verified": 2000398934016}, {"written": 805306368, "verified": 805306367}):
            with self.subTest(coverage=coverage), self.assertRaises(worker.Refusal):
                worker.receipt_for(request, [{"written": 8192, "verified": 8192}, coverage])

    def test_request_admits_only_a_resolved_closed_method(self):
        request = EraseBoundaryTests().request()
        worker.validate_request(request)
        request["targets"][0]["method"] = "metadataClear"
        worker.validate_request(request)
        for method in ("secureErase", "", None, 1):
            with self.subTest(method=method), self.assertRaises(worker.Refusal):
                worker.validate_request(dict(request, targets=[dict(request["targets"][0], method=method)]))
        missing = dict(request["targets"][0])
        del missing["method"]
        with self.assertRaises(worker.Refusal):
            worker.validate_request(dict(request, targets=[missing]))

    def test_release_precedes_the_exclusive_opens_and_refusal_stops_before_them(self):
        from unittest.mock import patch

        request = EraseBoundaryTests().request()
        events = []

        def open_disk(expected):
            events.append("open")
            disk = MemoryDisk(expected, b"x" * 8192)
            disk.dev = 1
            disk.close = lambda: None
            return disk

        def release(targets):
            events.append("release")
            self.assertEqual(targets, request["targets"])

        with patch.object(worker, "atomic"):
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None, release=release, scan=lambda _: None)
        self.assertEqual(events, ["release", "open"])

        def refuse(targets):
            raise worker.Refusal("disk-in-use")

        events.clear()
        with patch.object(worker, "atomic") as atomic, self.assertRaises(worker.Refusal):
            worker.execute(request, "/unused", opener=open_disk, prove_hardware=lambda _: None, release=refuse)
        self.assertEqual(events, [])
        atomic.assert_not_called()


def small_chunks(case):
    from unittest.mock import patch

    patcher = patch.object(worker, "CHUNK", 64 * 1024)
    patcher.start()
    case.addCleanup(patcher.stop)


class ConcurrentEraseTests(unittest.TestCase):
    def setUp(self):
        small_chunks(self)

    def request(self, sizes=None):
        sizes = sizes or (4 * worker.CHUNK, 4 * worker.CHUNK)
        targets = [
            {"deviceName": "/dev/d%d" % index, "method": "logicalOverwrite", "wwn": "w%d" % index, "serialNumber": "s%d" % index, "sizeBytes": size}
            for index, size in enumerate(sizes)
        ]
        return dict(EraseBoundaryTests().request(), targets=targets)

    def run_erase(self, request, disks, prove=lambda _: None, clock=None, scan=None):
        from unittest.mock import patch
        import time

        def open_disk(expected):
            disk = disks[expected["deviceName"]]
            disk.dev = expected["deviceName"]
            disk.close = lambda: disk.events.append("close")
            return disk

        with patch.object(worker, "atomic") as atomic:
            try:
                erase = worker.execute(
                    request, "/unused", opener=open_disk, prove_hardware=prove, release=lambda _: None, clock=clock or time.monotonic,
                    scan=scan or (lambda opened: [disk.idle() for disk in opened]),
                )
                return erase, atomic
            except worker.Refusal as refusal:
                return refusal, atomic

    def disks(self, request):
        return {target["deviceName"]: MemoryDisk(target, b"x" * target["sizeBytes"]) for target in request["targets"]}

    def test_every_disk_completes_before_one_receipt(self):
        request = self.request()
        disks = self.disks(request)
        result, atomic = self.run_erase(request, disks)
        self.assertEqual(result, worker.receipt_for(request, [{"written": 4 * worker.CHUNK, "verified": 4 * worker.CHUNK}] * 2))
        self.assertEqual([call.args[1]["state"] for call in atomic.call_args_list], ["partial", "complete"])
        self.assertTrue(all(disk.content == bytes(len(disk.content)) and disk.events[-1] == "close" for disk in disks.values()))

    def test_one_refusal_stops_every_other_disk_and_writes_no_receipt(self):
        import threading

        request = self.request((2 * worker.CHUNK, 64 * worker.CHUNK))
        disks = self.disks(request)
        failed = threading.Event()
        short, slow = disks["/dev/d0"], disks["/dev/d1"]
        write = slow.write

        def refuse(offset, data):
            failed.set()
            return len(data) - 1

        def wait(offset, data):
            failed.wait(5)
            return write(offset, data)

        short.write, slow.write = refuse, wait
        result, atomic = self.run_erase(request, disks)
        self.assertIsInstance(result, worker.Refusal)
        self.assertEqual(str(result), "short-write")
        self.assertLess(slow.events.count("write"), 64)
        self.assertNotIn("flush", slow.events)
        self.assertEqual([call.args[1]["state"] for call in atomic.call_args_list], ["partial"])
        self.assertTrue(all(disk.events[-1] == "close" for disk in disks.values()))

    def test_the_deadline_stops_every_disk_between_chunks(self):
        request = dict(self.request(), deadlineSeconds=60)
        disks = self.disks(request)
        import itertools

        ticks = itertools.count(0, 5)
        result, atomic = self.run_erase(request, disks, clock=lambda: next(ticks))
        self.assertIsInstance(result, worker.Refusal)
        self.assertEqual(str(result), "deadline")
        self.assertTrue(all("flush" not in disk.events for disk in disks.values()))
        self.assertEqual(len(atomic.call_args_list), 1)

    def test_the_system_proof_runs_at_most_once_per_second_across_disks(self):
        import threading

        request = self.request((16 * worker.CHUNK, 16 * worker.CHUNK))
        disks = self.disks(request)
        proofs = []
        result, unused = self.run_erase(request, disks, prove=proofs.append, clock=lambda: 0.0)
        self.assertNotIsInstance(result, worker.Refusal)
        # Start, after holder release, before the first write and after the last readback.
        self.assertEqual(len(proofs), 4)
        now, lock = [0.0], threading.Lock()

        def clock():
            with lock:
                now[0] += 0.25
                return now[0]

        proofs.clear()
        result, unused = self.run_erase(request, self.disks(request), prove=proofs.append, clock=clock)
        self.assertNotIsInstance(result, worker.Refusal)
        self.assertLessEqual(len(proofs), now[0] + 4)
        self.assertGreater(len(proofs), 4)

    def test_pace_proves_on_its_interval_and_always_when_due(self):
        now, proofs = [0.0], []
        pace = worker.Pace(EraseBoundaryTests().request(), proofs.append, clock=lambda: now[0])
        for moment in (0.0, 0.5, 0.99, 1.0, 1.5, 2.1):
            now[0] = moment
            pace.checkpoint()
        self.assertEqual(len(proofs), 3)
        pace.prove()
        self.assertEqual(len(proofs), 4)
        pace.stop.set()
        with self.assertRaisesRegex(worker.Refusal, "stopped"):
            pace.checkpoint()

    def test_a_logical_overwrite_target_is_at_most_896_gib(self):
        request = EraseBoundaryTests().request()
        bound = 896 * 1024**3
        cases = (("logicalOverwrite", bound, False), ("logicalOverwrite", bound + 4096, True), ("metadataClear", bound + 4096, False))
        for method, size, refused in cases:
            changed = dict(request, targets=[dict(request["targets"][0], method=method, sizeBytes=size)])
            with self.subTest(method=method, size=size):
                if refused:
                    with self.assertRaisesRegex(worker.Refusal, "target-size"):
                        worker.validate_request(changed)
                else:
                    worker.validate_request(changed)


class PacedUsageScanTests(unittest.TestCase):
    def setUp(self):
        small_chunks(self)
        self.erase = ConcurrentEraseTests()

    def test_the_usage_scan_runs_on_the_pace_and_around_the_writes(self):
        import threading

        request = self.erase.request((16 * worker.CHUNK, 16 * worker.CHUNK))
        disks = self.erase.disks(request)
        result, unused = self.erase.run_erase(request, disks, clock=lambda: 0.0)
        self.assertNotIsInstance(result, worker.Refusal)
        for disk in disks.values():
            self.assertEqual(disk.events.count("idle"), 2)
            self.assertLess(disk.events.index("idle"), disk.events.index("write"))
            self.assertGreater(len(disk.events) - 1 - disk.events[::-1].index("idle"), len(disk.events) - 1 - disk.events[::-1].index("read"))
            self.assertGreater(disk.events.count("identify"), 2 * 16 - 1)
        now, lock = [0.0], threading.Lock()

        def clock():
            with lock:
                now[0] += 0.01
                return now[0]

        disks = self.erase.disks(request)
        result, unused = self.erase.run_erase(request, disks, clock=clock)
        self.assertNotIsInstance(result, worker.Refusal)
        for disk in disks.values():
            self.assertLessEqual(disk.events.count("idle"), now[0] + 2)
            self.assertLess(disk.events.count("idle"), disk.events.count("identify"))

    def test_a_paced_usage_refusal_stops_every_disk_with_no_receipt(self):
        import threading

        request = self.erase.request((32 * worker.CHUNK, 32 * worker.CHUNK))
        disks = self.erase.disks(request)
        mounted = disks["/dev/d0"]
        mounted.busy = lambda: mounted.events.count("write") >= 4
        now, lock = [0.0], threading.Lock()

        def clock():
            with lock:
                now[0] += 0.5
                return now[0]

        result, atomic = self.erase.run_erase(dict(request, deadlineSeconds=14400), disks, clock=clock)
        self.assertIsInstance(result, worker.Refusal)
        self.assertEqual(str(result), "disk-mounted")
        self.assertTrue(all("flush" not in disk.events and disk.events[-1] == "close" for disk in disks.values()))
        self.assertEqual([call.args[1]["state"] for call in atomic.call_args_list], ["partial"])


class BlockDiskBufferTests(unittest.TestCase):
    def test_chunks_reuse_one_aligned_buffer_and_close_releases_it(self):
        import os
        import tempfile

        with tempfile.TemporaryFile() as stream:
            stream.write(b"x" * (2 * worker.CHUNK))
            stream.flush()
            disk = worker.BlockDisk.__new__(worker.BlockDisk)
            disk.fd, disk.buffer = os.dup(stream.fileno()), None
            self.assertEqual(disk.write(worker.CHUNK, bytes(worker.CHUNK)), worker.CHUNK)
            first = disk.buffer
            self.assertEqual(disk.read(worker.CHUNK, 4096), bytes(4096))
            self.assertEqual(disk.read(0, 4096), b"x" * 4096)
            self.assertIs(disk.buffer, first)
            with self.assertRaisesRegex(worker.Refusal, "chunk"):
                disk.read(0, worker.CHUNK + 4096)
            disk.close()
            self.assertTrue(first.closed)


class UsagePassTests(unittest.TestCase):
    def setUp(self):
        small_chunks(self)
        self.erase = ConcurrentEraseTests()

    def test_each_paced_scan_is_one_usage_pass_over_every_target(self):
        for count in (1, 2, 5):
            with self.subTest(targets=count):
                request = self.erase.request(tuple([8 * worker.CHUNK] * count))
                disks = self.erase.disks(request)
                passes = []
                result, unused = self.erase.run_erase(request, disks, clock=lambda: 0.0, scan=lambda opened: passes.append(list(opened)))
                self.assertNotIsInstance(result, worker.Refusal)
                # Before the first write and after the last readback, each one pass over all targets.
                self.assertEqual(len(passes), 2)
                self.assertTrue(all(len(opened) == count for opened in passes))

    def test_usage_unions_every_targets_nodes_into_one_unused_call(self):
        from unittest.mock import patch

        class Nodes:
            def __init__(self, *nodes):
                self.listed = list(nodes)

            def nodes(self):
                return self.listed

        with patch.object(worker, "unused") as unused:
            worker.usage([Nodes("a", "a1"), Nodes("b")])
        unused.assert_called_once_with(["a", "a1", "b"])


def isolation_runtime(tmp_path, damage):
    """Execute the shipped isolation function against synthetic proc and native replies."""
    import json
    import sys
    from pathlib import Path
    from unittest.mock import Mock, patch
    from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot

    with patch.dict(sys.modules, {"physical_erase_worker": worker, "physical_erase_process": process}):
        namespace = {"__name__": "test_runtime"}
        exec(compile(boot.RUNTIME, "erase-runtime", "exec"), namespace)
    flags = ["bootwright.erase=1", "rd.live.ram=1", "rd.live.overlay.overlayfs=1", "inst.zram=0"]
    if damage in flags:
        flags.remove(damage)
    (tmp_path / "cmdline").write_text(" ".join(flags))
    proc = tmp_path / "42"
    proc.mkdir()
    command_line = (b"x" * 65537 if damage == "process-inventory" else
                    b"python\0pyanaconda.modules.storage\0" if damage == "installer-process" else b"safe\0")
    (proc / "cmdline").write_bytes(command_line)
    namespace["Path"] = lambda value: tmp_path if value == "/proc" else tmp_path / "cmdline" if value == "/proc/cmdline" else Path(value)

    def command(argv):
        if argv[0] == "/usr/bin/findmnt":
            if "--json" in argv:
                return json.dumps({"filesystems": [{
                    "fstype": "xfs" if damage == "ram-root" else "overlay",
                    "options": "upperdir=/run/upper,workdir=/run/work",
                }]})
            return "xfs" if (damage == "ram-filesystem" and argv[-1] == "/run" or
                             damage == "ram-upper" and argv[-1] == "/run/upper" or
                             damage == "ram-work" and argv[-1] == "/run/work") else "tmpfs"
        if "--property=LoadState" in argv:
            return "masked" if damage == "installer-units" else "loaded"
        if "--property=ActiveState" in argv:
            return "active" if damage == "installer-running" else "inactive"
        return ""

    namespace["command"] = Mock(side_effect=command)
    return namespace


def test_actual_isolation_refusals_never_start_writer(tmp_path):
    from unittest.mock import patch

    cases = ["ram-filesystem", "ram-root", "ram-upper", "ram-work", "installer-units", "installer-running",
             "process-inventory", "installer-process", "bootwright.erase=1", "rd.live.ram=1",
             "rd.live.overlay.overlayfs=1", "inst.zram=0"]
    for index, damage in enumerate(cases):
        root = tmp_path / str(index)
        root.mkdir()
        namespace = isolation_runtime(root, damage)
        expected = "boot-purpose" if "=" in damage else "ram-upper" if damage == "ram-work" else damage
        with patch.object(process, "run", return_value=(0, b"")), patch.object(worker, "execute") as execute:
            try:
                namespace["run"]()
            except worker.Refusal as error:
                assert str(error) == expected
            else:
                raise AssertionError("isolation accepted " + damage)
            execute.assert_not_called()


def test_actual_isolation_accepts_only_a_ram_overlay_without_installer(tmp_path):
    from unittest.mock import patch

    namespace = isolation_runtime(tmp_path, "valid")
    with patch.object(process, "run", return_value=(0, b"")):
        namespace["isolate"]()
