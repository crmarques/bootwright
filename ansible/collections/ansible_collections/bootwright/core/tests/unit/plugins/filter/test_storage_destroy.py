import importlib.util
import pathlib
import sys
import types
import unittest


class _AnsibleFilterError(Exception):
    pass


if "ansible.errors" not in sys.modules:
    ansible_module = types.ModuleType("ansible")
    errors_module = types.ModuleType("ansible.errors")
    errors_module.AnsibleFilterError = _AnsibleFilterError
    ansible_module.errors = errors_module
    sys.modules["ansible"] = ansible_module
    sys.modules["ansible.errors"] = errors_module

AnsibleFilterError = sys.modules["ansible.errors"].AnsibleFilterError


_PATH = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter" / "storage_destroy.py"
_SPEC = importlib.util.spec_from_file_location("storage_destroy", _PATH)
_MODULE = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(_MODULE)


def _entry(cluster, node, unreachable=False, proof=None, completion=None):
    entry = {
        "inventory_hostname": f"storage__{cluster}__{node}",
        "bootwright_storage_cluster_name": cluster,
        "bootwright_storage_node_name": node,
        "bootwright_storage_reachable_probe": {"unreachable": unreachable},
    }
    if unreachable:
        entry["bootwright_storage_node_refusal"] = "connection timed out"
        entry["bootwright_storage_node_absent"] = True
    if proof is not None:
        entry["bootwright_ceph_sweep_verify"] = {"rc": proof, "stdout": ""}
        entry["bootwright_ceph_sweep_verify_rows"] = []
        entry["bootwright_ceph_sweep_survivors"] = []
    if completion is not None:
        entry["bootwright_ceph_destroy_completion"] = {"rc": completion}
    return entry


class StorageDestroyAttestation(unittest.TestCase):
    def test_terminal_report_separates_completed_skipped_and_incomplete_nodes(self):
        got = _MODULE.bootwright_storage_destroy_attestation(
            [
                _entry("ceph-a", "a1", proof=0, completion=0),
                _entry("ceph-a", "a2", unreachable=True),
                _entry("ceph-b", "b1", proof=1),
            ],
            True,
            True,
        )
        self.assertEqual(got["schemaVersion"], 1)
        self.assertEqual([item["name"] for item in got["clusters"]], ["ceph-a", "ceph-b"])
        self.assertEqual([item["name"] for item in got["clusters"][0]["nodes"]], ["a1", "a2"])
        self.assertEqual([item["outcome"] for item in got["clusters"][0]["nodes"]], ["completed", "skipped"])
        self.assertEqual(got["clusters"][0]["nodes"][0]["scanScope"], "all-node-pvs")
        self.assertEqual(got["clusters"][1]["nodes"][0]["outcome"], "incomplete")

    def test_preliminary_report_cannot_claim_a_reachable_node_completed(self):
        got = _MODULE.bootwright_storage_destroy_attestation(
            [_entry("ceph-a", "a1"), _entry("ceph-a", "a2", unreachable=True)],
            False,
            True,
        )
        self.assertEqual([item["outcome"] for item in got["clusters"][0]["nodes"]], ["incomplete", "skipped"])

    def test_missing_terminal_evidence_is_never_classified_unreachable(self):
        authorized = _MODULE.bootwright_storage_destroy_attestation([_entry("ceph-a", "a1")], True, True)
        self.assertEqual(authorized["clusters"][0]["nodes"][0]["outcome"], "incomplete")

    def test_explicitly_lost_node_is_skipped_only_when_authorized(self):
        lost = _entry("ceph-a", "a1", proof=0)
        lost["bootwright_ceph_destroy_completion"] = {"unreachable": True, "msg": "connection timed out"}
        authorized = _MODULE.bootwright_storage_destroy_attestation([lost], True, True)
        refused = _MODULE.bootwright_storage_destroy_attestation([lost], True, False)
        self.assertEqual(authorized["clusters"][0]["nodes"][0]["outcome"], "skipped")
        self.assertEqual(authorized["clusters"][0]["nodes"][0]["absenceClass"], "connection-lost")
        self.assertEqual(refused["clusters"][0]["nodes"][0]["outcome"], "incomplete")

    def test_initial_unreachable_node_is_not_skipped_without_authorization(self):
        got = _MODULE.bootwright_storage_destroy_attestation(
            [_entry("ceph-a", "a1", unreachable=True)], False, False
        )
        self.assertEqual(got["clusters"][0]["nodes"][0]["outcome"], "incomplete")

    def test_malformed_evidence_fails_closed(self):
        entry = _entry("ceph-a", "a1", proof=0, completion=0)
        entry["bootwright_ceph_sweep_verify"]["rc"] = "0"
        with self.assertRaises(AnsibleFilterError):
            _MODULE.bootwright_storage_destroy_attestation([entry], True, False)

    def test_filter_is_registered(self):
        self.assertIs(
            _MODULE.FilterModule().filters()["bootwright_storage_destroy_attestation"],
            _MODULE.bootwright_storage_destroy_attestation,
        )


if __name__ == "__main__":
    unittest.main()
