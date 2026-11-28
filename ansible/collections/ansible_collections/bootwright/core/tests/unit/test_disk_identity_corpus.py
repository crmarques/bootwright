"""The sysfs disk identity every reader yields, from the one corpus the Go and bash readers also read."""

import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from ansible_collections.bootwright.core.plugins.module_utils import disk_inventory as inventory
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker

CORPUS = json.loads((Path(__file__).resolve().parent / "goldens/disk_identity_trees.json").read_text(encoding="utf-8"))


def materialize(root, files):
    for name, content in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(bytes.fromhex(content["hex"]) if "hex" in content else content["text"].encode("utf-8"))


def yields(reader, node):
    try:
        return reader(node) or None
    except (worker.Refusal, inventory.Refused, OSError):
        return None


class DiskIdentityCorpusTests(unittest.TestCase):
    def each_node(self, check):
        for case in CORPUS["nodes"]:
            with self.subTest(case["case"]), tempfile.TemporaryDirectory() as temporary:
                node = Path(temporary)
                materialize(node, case["files"])
                check(case, node)

    def test_the_erase_worker_reads_the_corpus_serial_and_wwn(self):
        def check(case, node):
            expected = None if "worker" in case.get("stricter", {}) else case["serial"]
            self.assertEqual(yields(worker.disk_serial, node), expected)
            self.assertEqual(yields(worker.reported_wwn, node), case["wwid"])
            if case["wwn"]:
                self.assertTrue(worker.matches_wwn(case["wwid"], case["wwn"]))

        self.each_node(check)

    def test_discovery_reads_the_corpus_serial_and_wwn(self):
        def check(case, node):
            admitted = case["serial"] is not None and case["admissible"] and "inventory" not in case.get("stricter", {})
            self.assertEqual(yields(inventory.serial_number, node), case["serial"] if admitted else None)
            reported = yields(inventory.wwid, node)
            self.assertEqual(inventory.canonical_wwn(reported or ""), case["wwn"])

        self.each_node(check)

    def test_the_serial_grammar_is_the_corpus_grammar(self):
        for entry in CORPUS["grammar"]["serials"]:
            with self.subTest(entry["value"]):
                self.assertEqual(inventory.SERIAL.fullmatch(entry["value"]) is not None, entry["valid"])

    def test_the_wwn_grammar_is_the_corpus_grammar(self):
        for entry in CORPUS["grammar"]["wwns"]:
            with self.subTest(entry["wwn"]):
                observed = "naa." + entry["wwn"][2:] if entry["wwn"].startswith("0x") else entry["wwn"]
                self.assertEqual(bool(entry["wwn"]) and inventory.canonical_wwn(observed) == entry["wwn"], entry["valid"])

    def test_discovery_bounds_the_capacity_as_the_corpus_does(self):
        for entry in CORPUS["grammar"]["sizes"]:
            with self.subTest(entry["bytes"]), mock.patch.object(inventory, "number_ioctl", return_value=entry["bytes"]):
                try:
                    inventory.descriptor_size(3)
                    accepted = True
                except inventory.Refused:
                    accepted = False
                self.assertEqual(accepted, entry["valid"])
