"""The worker refuses ambiguous hardware and every in-use child partition."""

from pathlib import Path
import tempfile
import unittest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker


class IdentityTests(unittest.TestCase):
    def test_only_exact_scsi_naa_bytes_match_the_canonical_udev_wwn(self):
        self.assertTrue(worker.matches_wwn("naa.5000000000000001", "0x5000000000000001"))
        self.assertFalse(worker.matches_wwn("naa.5000000000000002", "0x5000000000000001"))
        self.assertFalse(worker.matches_wwn("eui.5000000000000001", "0x5000000000000001"))
        self.assertTrue(worker.matches_wwn("eui.5000000000000001", "eui.5000000000000001"))
        self.assertFalse(worker.matches_wwn("naa.5001", "0x5001"))

    def test_scsi_serial_requires_a_complete_ascii_vpd_page(self):
        with tempfile.TemporaryDirectory() as temporary:
            node = Path(temporary)
            (node / "device").mkdir()
            page = node / "device/vpd_pg80"
            serial = b"EXAMPLE-ROOT-01"
            page.write_bytes(b"\x00\x80" + len(serial).to_bytes(2, "big") + serial)
            self.assertEqual(worker.disk_serial(node), serial.decode())
            for data in (b"\x00\x80\x00\x20short", b"\x00\x83\x00\x01x"):
                page.write_bytes(data)
                with self.assertRaisesRegex(worker.Refusal, "disk-serial"):
                    worker.disk_serial(node)

    def test_a_non_ascii_serial_refuses_with_the_serial_code(self):
        with tempfile.TemporaryDirectory() as temporary:
            node = Path(temporary)
            (node / "device").mkdir()
            page = node / "device/vpd_pg80"
            page.write_bytes(b"\x00\x80\x00\x01\xff")
            with self.assertRaisesRegex(worker.Refusal, "disk-serial"):
                worker.disk_serial(node)
            page.unlink()
            (node / "device/serial").write_bytes(b"EXAMPLE-\xff1\n")
            with self.assertRaisesRegex(worker.Refusal, "disk-serial"):
                worker.disk_serial(node)

    def test_a_non_ascii_sysfs_attribute_refuses_with_the_identity_code(self):
        with tempfile.TemporaryDirectory() as temporary:
            attribute = Path(temporary) / "wwid"
            attribute.write_bytes(b"naa.\xff\n")
            with self.assertRaisesRegex(worker.Refusal, "identity"):
                worker.text(attribute)
            attribute.write_bytes(b"naa.5000000000000001\n")
            self.assertEqual(worker.text(attribute), "naa.5000000000000001")

    def test_duplicate_physical_mac_inventory_never_collapses_to_a_match(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            uuid = root / "class/dmi/id/product_uuid"
            uuid.parent.mkdir(parents=True)
            uuid.write_text("example-system\n")
            request = {"uuid": "example-system", "macs": ["02:00:00:00:00:01"]}
            for name in ("eno1", "eno2"):
                interface = root / "class/net" / name
                (interface / "device").mkdir(parents=True)
                (interface / "address").write_text("02:00:00:00:00:01\n")
            with self.assertRaisesRegex(worker.Refusal, "system-identity"):
                worker.hardware(request, root)
            (root / "class/net/eno2/address").write_text("02:00:00:00:00:02\n")
            request["macs"].append("02:00:00:00:00:02")
            worker.hardware(request, root)

    def test_mounted_partition_holders_slaves_and_active_swap_refuse(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            disk, child, proc = root / "sda", root / "sda/sda1", root / "proc"
            for node, device in ((disk, "8:0"), (child, "8:1")):
                node.mkdir(parents=True, exist_ok=True)
                (node / "dev").write_text(device + "\n")
                (node / "holders").mkdir()
                (node / "slaves").mkdir()
            namespace = proc / "1/ns/mnt"
            namespace.parent.mkdir(parents=True)
            namespace.touch()
            mounts = proc / "1/mountinfo"
            mounts.write_text("1 0 0:1 / / rw - tmpfs tmpfs rw\n")
            swaps = proc / "swaps"
            swaps.write_text("Filename\tType\tSize\tUsed\tPriority\n")
            worker.unused([disk, child], proc)
            mounts.write_text("1 0 8:1 / /mnt rw - xfs /dev/sda1 rw\n")
            with self.assertRaisesRegex(worker.Refusal, "disk-mounted"):
                worker.unused([disk, child], proc)
            mounts.write_text("1 0 0:1 / / rw - tmpfs tmpfs rw\n")
            for directory in (child / "holders", disk / "slaves"):
                entry = directory / "foreign"
                entry.touch()
                with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
                    worker.unused([disk, child], proc)
                entry.unlink()
            swaps.write_text("Filename\tType\tSize\tUsed\tPriority\n/dev/foreign partition 1024 0 -2\n")
            with self.assertRaisesRegex(worker.Refusal, "active-swap"):
                worker.unused([disk, child], proc)

    def test_undecodable_usage_inventory_refuses_instead_of_raising(self):
        with tempfile.TemporaryDirectory() as temporary:
            proc = Path(temporary) / "proc"
            namespace = proc / "1/ns/mnt"
            namespace.parent.mkdir(parents=True)
            namespace.touch()
            mounts, swaps = proc / "1/mountinfo", proc / "swaps"
            mounts.write_text("1 0 0:1 / / rw - tmpfs tmpfs rw\n")
            swaps.write_text("Filename\tType\tSize\tUsed\tPriority\n")
            worker.unused([], proc)
            for target, valid in ((mounts, mounts.read_bytes()), (swaps, swaps.read_bytes())):
                target.write_bytes(valid + b"/mnt/caf\xc3\xa9\n")
                with self.assertRaisesRegex(worker.Refusal, "usage-inventory"):
                    worker.unused([], proc)
                target.write_bytes(valid)

    def test_a_non_numeric_diskseq_refuses_with_the_identity_code(self):
        with tempfile.TemporaryDirectory() as temporary:
            attribute = Path(temporary) / "diskseq"
            attribute.write_text("12\n")
            self.assertEqual(worker.sequence_number(attribute), 12)
            for data in (b"twelve\n", b"1 2\n", b"1_2\n", b"-3\n", b"\xff\n"):
                attribute.write_bytes(data)
                with self.assertRaisesRegex(worker.Refusal, "identity"):
                    worker.sequence_number(attribute)


def block(root, name, serial=None, removable="0", ro="0", partition=False, native=False, hidden=None):
    node = root / "class/block" / name
    (node / "device").mkdir(parents=True)
    (node / "removable").write_text(removable + "\n")
    (node / "ro").write_text(ro + "\n")
    if partition:
        (node / "partition").write_text("1\n")
    if hidden is not None:
        (node / "hidden").write_text(hidden + "\n")
    if serial is not None and native:
        (node / "device/serial").write_text(serial + "  \n")
    elif serial is not None:
        data = serial.encode()
        (node / "device/vpd_pg80").write_bytes(b"\x00\x80" + len(data).to_bytes(2, "big") + data)
    return node


class SerialResolutionTests(unittest.TestCase):
    """A target declared without a path is the one whole disk reporting its serial."""

    def test_exactly_one_whole_disk_reporting_the_serial_is_resolved(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            block(root, "sda", "EXAMPLE-DATA-01")
            block(root, "sda1", "EXAMPLE-DATA-01", partition=True)
            block(root, "sdb", "EXAMPLE-DATA-02")
            unreadable = block(root, "sdc")
            (unreadable / "device/vpd_pg80").write_bytes(b"\x00\x83\x00\x01x")
            block(root, "loop0")
            self.assertEqual(worker.resolve_serial("EXAMPLE-DATA-01", root), "/dev/sda")
            block(root, "nvme0n1", "EXAMPLE-NVME-01", native=True)
            self.assertEqual(worker.resolve_serial("EXAMPLE-NVME-01", root), "/dev/nvme0n1")

    def test_no_match_and_several_matches_refuse(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            block(root, "sda", "EXAMPLE-DATA-01")
            with self.assertRaisesRegex(worker.Refusal, "disk-serial-absent"):
                worker.resolve_serial("EXAMPLE-DATA-09", root)
            block(root, "sdb", "EXAMPLE-DATA-01")
            with self.assertRaisesRegex(worker.Refusal, "disk-serial-ambiguous"):
                worker.resolve_serial("EXAMPLE-DATA-01", root)
            block(root, "nvme0n1", "EXAMPLE-NVME-01", native=True)
            block(root, "nvme0n2", "EXAMPLE-NVME-01", native=True)
            with self.assertRaisesRegex(worker.Refusal, "disk-serial-ambiguous"):
                worker.resolve_serial("EXAMPLE-NVME-01", root)

    def test_a_hidden_nvme_path_node_is_the_same_disk_as_its_head_node(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            block(root, "nvme0n1", "EXAMPLE-NVME-01", native=True, hidden="0")
            block(root, "nvme0c0n1", "EXAMPLE-NVME-01", native=True, hidden="1")
            block(root, "nvme0c1n1", "EXAMPLE-NVME-01", native=True, hidden="1")
            self.assertEqual(worker.resolve_serial("EXAMPLE-NVME-01", root), "/dev/nvme0n1")

    def test_a_path_node_not_proved_hidden_still_counts(self):
        for hidden in (None, "0", "yes", ""):
            with self.subTest(hidden=hidden), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                block(root, "nvme0n1", "EXAMPLE-NVME-01", native=True)
                block(root, "nvme0c0n1", "EXAMPLE-NVME-01", native=True, hidden=hidden)
                with self.assertRaisesRegex(worker.Refusal, "disk-serial-ambiguous"):
                    worker.resolve_serial("EXAMPLE-NVME-01", root)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            block(root, "nvme0n1", "EXAMPLE-NVME-01", native=True)
            (block(root, "nvme0c0n1", "EXAMPLE-NVME-01", native=True) / "hidden").mkdir()
            with self.assertRaisesRegex(worker.Refusal, "disk-serial-ambiguous"):
                worker.resolve_serial("EXAMPLE-NVME-01", root)

    def test_a_removable_read_only_or_other_kind_of_disk_refuses(self):
        for name, changes in (("sda", {"removable": "1"}), ("sda", {"ro": "1"}), ("vda", {}), ("sr0", {})):
            with self.subTest(name=name, changes=changes), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                block(root, name, "EXAMPLE-DATA-01", **changes)
                with self.assertRaisesRegex(worker.Refusal, "whole-block-device"):
                    worker.resolve_serial("EXAMPLE-DATA-01", root)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (block(root, "sda", "EXAMPLE-DATA-01") / "ro").unlink()
            with self.assertRaisesRegex(worker.Refusal, "whole-block-device"):
                worker.resolve_serial("EXAMPLE-DATA-01", root)

    def test_a_serial_target_reads_no_wwid_and_compares_its_size(self):
        disk = object.__new__(worker.BlockDisk)
        with tempfile.TemporaryDirectory() as temporary:
            node = block(Path(temporary), "sda", "EXAMPLE-DATA-01")
            (node / "diskseq").write_text("7\n")
            disk.fd, disk.sequence, disk.sys = None, 7, node
            sizes = {worker.BLKGETDISKSEQ: 7, worker.BLKGETSIZE64: 8192}
            original = worker.number_ioctl
            worker.number_ioctl = lambda fd, command: sizes[command]
            try:
                expected = {"method": "metadataClear", "serialNumber": "EXAMPLE-DATA-01", "sizeBytes": 8192}
                self.assertEqual(disk.identify(expected), 7)
                with self.assertRaisesRegex(worker.Refusal, "disk-identity"):
                    disk.identify(dict(expected, serialNumber="EXAMPLE-DATA-02"))
                with self.assertRaisesRegex(worker.Refusal, "disk-size"):
                    disk.identify(dict(expected, sizeBytes=12288))
                with self.assertRaises(FileNotFoundError):
                    disk.identify(dict(expected, deviceName="/dev/sda", wwn="0x5000000000000001"))
            finally:
                worker.number_ioctl = original
