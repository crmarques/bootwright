"""The erase worker releases only device-mapper stacks over its proved targets."""

from __future__ import annotations

import os
from pathlib import Path
import tempfile
import unittest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process as process
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker


class Tree:
    """A synthetic sysfs block tree and /proc with one mount namespace."""

    def __init__(self, root):
        self.root = Path(root)
        self.proc = self.root / "proc"
        (self.proc / "1/ns").mkdir(parents=True)
        (self.proc / "1/ns/mnt").write_text("")
        (self.proc / "1/mountinfo").write_text("22 1 0:21 / / rw - tmpfs tmpfs rw\n")
        (self.proc / "swaps").write_text("Filename Type Size Used Priority\n")
        self.opened = {}
        self.calls = []
        self.removed = []
        self.fail = set()

    def node(self, path, dev, name=None):
        node = self.root / "sys" / path
        for child in ("holders", "slaves"):
            (node / child).mkdir(parents=True)
        (node / "dev").write_text(dev + "\n")
        if name is not None:
            (node / "dm").mkdir()
            (node / "dm/name").write_text(name + "\n")
            self.opened[name] = 0
        return node

    def disk(self, name, major):
        disk = self.node("devices/" + name, "%d:0" % major)
        partition = self.node("devices/%s/%s1" % (name, name), "%d:1" % major)
        (partition / "partition").write_text("1\n")
        return disk, partition

    def stack(self, holder, *slaves):
        for slave in slaves:
            os.symlink(holder, slave / "holders" / holder.name)
            os.symlink(slave, holder / "slaves" / slave.name)

    def mount(self, node):
        with (self.proc / "1/mountinfo").open("a") as stream:
            stream.write("30 22 %s / /mnt rw - xfs /dev/x rw\n" % (node / "dev").read_text().strip())

    def run(self, argv, timeout=60, limit=65536):
        self.calls.append(argv[1:])
        if argv[-4] != "-j" or argv[-2] != "-m":
            raise AssertionError("a device-mapper node was not selected by its device number")
        dev = argv[-3] + ":" + argv[-1]
        holder = next(path for path in (self.root / "sys/virtual").iterdir() if (path / "dev").read_text().strip() == dev)
        name = (holder / "dm/name").read_text().strip()
        if argv[1] in self.fail:
            return 1, b""
        if argv[1] == "info":
            return 0, b"  %d\n" % self.opened[name]
        self.removed.append(name)
        for slave in list((holder / "slaves").iterdir()):
            os.unlink(slave.resolve() / "holders" / holder.name)
        return 0, b""


class HolderReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.tree = Tree(temporary.name)
        self.disk, self.partition = self.tree.disk("sda", 8)
        self.nodes = [self.disk, self.partition]

    def release(self, nodes=None):
        worker.release_holders(nodes or self.nodes, run=self.tree.run, proc=self.tree.proc)

    def removed(self):
        return self.tree.removed

    def test_clean_targets_run_no_command(self):
        self.release()
        self.assertEqual(self.tree.calls, [])

    def test_an_lv_over_a_target_partition_is_released(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "ceph--a-osd--block--b")
        self.tree.stack(lv, self.partition)
        self.release()
        name = "ceph--a-osd--block--b"
        info = ["info", "--columns", "--noheadings", "--options", "open", "-j", "253", "-m", "0"]
        self.assertEqual(self.tree.calls, [info, ["remove", "-j", "253", "-m", "0"]])
        self.assertEqual(self.removed(), [name])
        self.assertEqual(list((self.partition / "holders").iterdir()), [])

    def test_crypt_over_an_lv_is_released_deepest_first(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "ceph--a-osd--block--b")
        crypt = self.tree.node("virtual/dm-1", "253:1", "crypt-b")
        self.tree.stack(lv, self.disk)
        self.tree.stack(crypt, lv)
        self.tree.opened["ceph--a-osd--block--b"] = 1
        original = self.tree.run

        def run(argv, timeout=60, limit=65536):
            result = original(argv, timeout, limit)
            if argv[1:] == ["remove", "-j", "253", "-m", "1"]:
                self.tree.opened["ceph--a-osd--block--b"] = 0
            return result

        worker.release_holders(self.nodes, run=run, proc=self.tree.proc)
        self.assertEqual(self.removed(), ["crypt-b", "ceph--a-osd--block--b"])

    def test_an_lv_spanning_two_declared_targets_is_released(self):
        other, partition = self.tree.disk("sdb", 9)
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk, other)
        self.release([*self.nodes, other, partition])
        self.assertEqual(self.removed(), ["vg-lv"])

    def test_a_dm_with_a_foreign_slave_refuses_without_a_command(self):
        foreign, unused = self.tree.disk("sdz", 65)
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk, foreign)
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_a_stacked_node_over_a_foreign_dm_refuses(self):
        foreign = self.tree.node("virtual/dm-5", "253:5", "foreign-lv")
        top = self.tree.node("virtual/dm-6", "253:6", "top")
        self.tree.stack(top, self.disk, foreign)
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_a_mounted_dm_refuses_without_a_command(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        crypt = self.tree.node("virtual/dm-1", "253:1", "crypt")
        self.tree.stack(lv, self.disk)
        self.tree.stack(crypt, lv)
        self.tree.mount(crypt)
        with self.assertRaisesRegex(worker.Refusal, "disk-mounted"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_active_swap_refuses_without_a_command(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)
        with (self.tree.proc / "swaps").open("a") as stream:
            stream.write("/dev/dm-0 partition 1024 0 -2\n")
        with self.assertRaisesRegex(worker.Refusal, "active-swap"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_an_open_dm_refuses_before_its_removal(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)
        self.tree.opened["vg-lv"] = 1
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.removed(), [])

    def test_an_md_holder_refuses(self):
        md = self.tree.node("virtual/md0", "9:0")
        self.tree.stack(md, self.partition)
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_an_md_over_a_released_dm_refuses(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        md = self.tree.node("virtual/md0", "9:0")
        self.tree.stack(lv, self.disk)
        self.tree.stack(md, lv)
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.tree.calls, [])

    def test_a_failed_command_refuses(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)
        for command in ("info", "remove"):
            with self.subTest(command=command):
                self.tree.calls.clear()
                self.tree.fail = {command}
                with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
                    self.release()

    def test_a_bounded_process_refusal_is_disk_in_use(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)

        def timeout(argv, timeout=60, limit=65536):
            raise process.Refusal("native-timeout")

        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            worker.release_holders(self.nodes, run=timeout, proc=self.tree.proc)

    def test_a_holder_that_remains_after_removal_refuses(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)

        def ineffective(argv, timeout=60, limit=65536):
            return (0, b"0\n") if argv[1] == "info" else (0, b"")

        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            worker.release_holders(self.nodes, run=ineffective, proc=self.tree.proc)

    def test_a_node_renamed_after_the_scan_is_not_removed(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "vg-lv")
        self.tree.stack(lv, self.disk)
        stack = worker.stacked(self.nodes)
        (lv / "dm/name").write_text("renamed\n")
        original = worker.stacked
        try:
            worker.stacked = lambda nodes: stack
            with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
                self.release()
        finally:
            worker.stacked = original
        self.assertEqual(self.tree.calls, [])

    def test_an_unsafe_dm_name_refuses(self):
        lv = self.tree.node("virtual/dm-0", "253:0", "-f")
        self.tree.stack(lv, self.disk)
        with self.assertRaisesRegex(worker.Refusal, "disk-in-use"):
            self.release()
        self.assertEqual(self.tree.calls, [])


class Probe:
    def __init__(self, expected, events, dev):
        self.expected, self.events, self.dev = expected, events, dev
        events.append(("open", expected["deviceName"]))

    def nodes(self):
        return [Path("/sys") / self.expected["deviceName"]]

    def identify(self, expected):
        self.events.append(("identify", expected["deviceName"]))
        if expected != self.expected:
            raise worker.Refusal("disk-identity")

    def close(self):
        self.events.append(("close", self.expected["deviceName"]))


class PrepareTests(unittest.TestCase):
    def targets(self):
        target = {"deviceName": "/dev/a", "method": "metadataClear", "wwn": "a", "serialNumber": "a", "sizeBytes": 8192}
        return [target, dict(target, deviceName="/dev/b", wwn="b", serialNumber="b")]

    def test_every_target_is_probed_read_only_before_any_release(self):
        events, released = [], []

        def opener(expected, exclusive=True):
            self.assertFalse(exclusive)
            return Probe(expected, events, expected["deviceName"])

        def release(nodes):
            released.append(list(events))
            events.append(("release", tuple(str(node) for node in nodes)))

        worker.prepare(self.targets(), opener=opener, release=release)
        self.assertEqual(released, [[("open", "/dev/a"), ("open", "/dev/b")]])
        self.assertEqual(events[-4:], [("identify", "/dev/a"), ("identify", "/dev/b"), ("close", "/dev/a"), ("close", "/dev/b")])

    def test_an_unproved_or_duplicate_target_never_releases(self):
        events = []

        def foreign(expected, exclusive=True):
            if expected["deviceName"] == "/dev/b":
                raise worker.Refusal("disk-identity")
            return Probe(expected, events, expected["deviceName"])

        def alias(expected, exclusive=True):
            return Probe(expected, events, "same")

        for opener in (foreign, alias):
            with self.subTest(opener=opener.__name__), self.assertRaises(worker.Refusal):
                worker.prepare(self.targets(), opener=opener, release=lambda nodes: self.fail("released"))
        self.assertTrue(all(("close", name) in events for action, name in events if action == "open"))


if __name__ == "__main__":
    unittest.main()
