import importlib.util
import json
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
        entry["bootwright_ceph_settle_owned_fsid"] = {
            "ceph-a": "11111111-1111-1111-1111-111111111111",
            "ceph-b": "22222222-2222-2222-2222-222222222222",
        }[cluster]
    if completion is not None:
        entry["bootwright_ceph_destroy_completion"] = {"rc": completion}
    return entry


def _controller_record(**overrides):
    record = {
        "apiVersion": "bootwright.io/ownership/v1alpha1",
        "kind": "storage-cluster",
        "name": "ceph-a",
        "owner": "bootwright",
        "context": "lab",
        "cluster": "ceph-a",
        "host": "storage__ceph-a__seed",
        "attributes": {
            "seedHost": "storage__ceph-a__seed",
            "fsid": "11111111-1111-1111-1111-111111111111",
        },
    }
    record.update(overrides)
    return record


def _controller_evidence(raw_json, present=True, path_safe=True):
    return _MODULE.bootwright_ceph_controller_owner_evidence(
        raw_json,
        present,
        path_safe,
        "lab",
        "ceph-a",
        "storage__ceph-a__seed",
    )


def _marker_evidence(raw_json, present=True, path_safe=True):
    return _MODULE.bootwright_ceph_host_marker_evidence(raw_json, present, path_safe, "ceph-a")


class CephOwnershipEvidence(unittest.TestCase):
    def test_controller_owner_accepts_exact_identity_with_implicit_or_explicit_owner_role(self):
        for role in (None, "", "owner"):
            record = _controller_record()
            if role is not None:
                record["role"] = role
            with self.subTest(role=role):
                got = _controller_evidence(json.dumps(record))
                self.assertEqual(
                    got,
                    {
                        "present": True,
                        "readable": True,
                        "identityValid": True,
                        "fsid": "11111111-1111-1111-1111-111111111111",
                        "fsidEmpty": False,
                        "fsidValid": True,
                        "valid": True,
                        "blockers": [],
                    },
                )

    def test_controller_owner_evidence_defects_fail_closed_without_raising(self):
        exact_attributes = _controller_record()["attributes"]
        cases = (
            ("missing", None, False, False, False, False, "is missing"),
            ("empty", "", True, False, False, False, "is empty"),
            ("malformed", "{", True, False, False, False, "not valid JSON"),
            ("deep non-object", "[" * 2000 + "]" * 2000, True, False, False, False, "JSON object"),
            ("non-object", "[]", True, False, False, False, "must be a JSON object"),
            ("wrong api", _controller_record(apiVersion="bootwright.io/ownership/v2"), True, True, False, True, "apiVersion"),
            ("wrong kind", _controller_record(kind="machine"), True, True, False, True, "kind"),
            ("wrong name", _controller_record(name="ceph-b"), True, True, False, True, "name"),
            ("wrong owner", _controller_record(owner="operator"), True, True, False, True, "owner"),
            ("reference", _controller_record(role="reference"), True, True, False, True, "role"),
            ("non-string role", _controller_record(role=7), True, True, False, True, "role"),
            ("null role", _controller_record(role=None), True, True, False, True, "role"),
            ("foreign context", _controller_record(context="other"), True, True, False, True, "context"),
            ("wrong cluster", _controller_record(cluster="ceph-b"), True, True, False, True, "cluster"),
            ("wrong host", _controller_record(host="storage__ceph-a__other"), True, True, False, True, "host"),
            ("non-object attributes", _controller_record(attributes=[]), True, True, False, False, "attributes"),
            (
                "wrong seed",
                _controller_record(attributes={**exact_attributes, "seedHost": "storage__ceph-a__other"}),
                True,
                True,
                False,
                True,
                "attributes.seedHost",
            ),
            (
                "missing fsid",
                _controller_record(attributes={"seedHost": "storage__ceph-a__seed"}),
                True,
                True,
                True,
                False,
                "attributes.fsid",
            ),
            (
                "invalid fsid",
                _controller_record(attributes={**exact_attributes, "fsid": "not-a-uuid"}),
                True,
                True,
                True,
                False,
                "attributes.fsid",
            ),
            (
                "non-string fsid",
                _controller_record(attributes={**exact_attributes, "fsid": 7}),
                True,
                True,
                True,
                False,
                "attributes.fsid",
            ),
        )
        for name, evidence, present, readable, identity_valid, fsid_valid, blocker in cases:
            raw_json = evidence if isinstance(evidence, str) or evidence is None else json.dumps(evidence)
            with self.subTest(name=name):
                got = _controller_evidence(raw_json, present)
                self.assertEqual(got["present"], present)
                self.assertEqual(got["readable"], readable)
                self.assertEqual(got["identityValid"], identity_valid)
                self.assertEqual(got["fsidValid"], fsid_valid)
                self.assertFalse(got["valid"])
                self.assertTrue(any(blocker in item for item in got["blockers"]), got)

    def test_controller_and_marker_reject_unsafe_evidence_paths(self):
        controller = _controller_evidence(json.dumps(_controller_record()), path_safe=False)
        marker = _marker_evidence(
            json.dumps({"manager": "bootwright", "cluster": "ceph-a", "fsid": "11111111-1111-1111-1111-111111111111"}),
            path_safe=False,
        )
        for got in (controller, marker):
            self.assertFalse(got["readable"])
            self.assertFalse(got["valid"])
            self.assertTrue(any("regular non-symlink" in blocker for blocker in got["blockers"]), got)

    def test_controller_owner_distinguishes_an_empty_prerecord_fsid_from_an_invalid_value(self):
        exact_attributes = _controller_record()["attributes"]
        cases = (
            ("omitted", {"seedHost": "storage__ceph-a__seed"}, True),
            ("blank", {**exact_attributes, "fsid": "  "}, True),
            ("null", {**exact_attributes, "fsid": None}, False),
            ("number", {**exact_attributes, "fsid": 7}, False),
            ("array", {**exact_attributes, "fsid": []}, False),
        )
        for name, attributes, want_empty in cases:
            with self.subTest(name=name):
                got = _controller_evidence(json.dumps(_controller_record(attributes=attributes)))
                self.assertEqual(got["fsidEmpty"], want_empty)
                self.assertFalse(got["fsidValid"])
                self.assertFalse(got["valid"])

    def test_controller_owner_blockers_have_stable_field_order(self):
        got = _controller_evidence(json.dumps({}))
        self.assertEqual(
            got["blockers"],
            [
                "controller ownership record apiVersion must be bootwright.io/ownership/v1alpha1",
                "controller ownership record kind must be storage-cluster",
                "controller ownership record name must be ceph-a",
                "controller ownership record owner must be bootwright",
                "controller ownership record context must be lab",
                "controller ownership record cluster must be ceph-a",
                "controller ownership record host must be storage__ceph-a__seed",
                "controller ownership record attributes must be a JSON object",
                "controller ownership record attributes.seedHost must be storage__ceph-a__seed",
                "controller ownership record attributes.fsid must be a UUID",
            ],
        )

    def test_host_marker_accepts_only_exact_identity_and_valid_fsid(self):
        exact = {
            "manager": "bootwright",
            "cluster": "ceph-a",
            "fsid": "11111111-1111-1111-1111-111111111111",
        }
        got = _marker_evidence(json.dumps(exact))
        self.assertEqual(
            got,
            {
                "present": True,
                "readable": True,
                "identityValid": True,
                "fsid": "11111111-1111-1111-1111-111111111111",
                "fsidEmpty": False,
                "fsidValid": True,
                "valid": True,
                "blockers": [],
            },
        )
        cases = (
            ("missing", None, False, False, False, False, "is missing"),
            ("empty", "", True, False, False, False, "is empty"),
            ("malformed", "{", True, False, False, False, "not valid JSON"),
            ("deep non-object", "[" * 2000 + "]" * 2000, True, False, False, False, "JSON object"),
            ("non-object", "[]", True, False, False, False, "must be a JSON object"),
            ("wrong manager", {**exact, "manager": "other"}, True, True, False, True, "manager"),
            ("wrong cluster", {**exact, "cluster": "ceph-b"}, True, True, False, True, "cluster"),
            ("missing fsid", {"manager": "bootwright", "cluster": "ceph-a"}, True, True, True, False, "fsid"),
            ("invalid fsid", {**exact, "fsid": "not-a-uuid"}, True, True, True, False, "fsid"),
            ("non-string fsid", {**exact, "fsid": 7}, True, True, True, False, "fsid"),
        )
        for name, evidence, present, readable, identity_valid, fsid_valid, blocker in cases:
            raw_json = evidence if isinstance(evidence, str) or evidence is None else json.dumps(evidence)
            with self.subTest(name=name):
                got = _marker_evidence(raw_json, present)
                self.assertEqual(got["present"], present)
                self.assertEqual(got["readable"], readable)
                self.assertEqual(got["identityValid"], identity_valid)
                self.assertEqual(got["fsidValid"], fsid_valid)
                self.assertFalse(got["valid"])
                self.assertTrue(any(blocker in item for item in got["blockers"]), got)

    def test_argument_type_errors_are_not_reclassified_as_evidence_defects(self):
        with self.assertRaisesRegex(AnsibleFilterError, "present must be a boolean"):
            _controller_evidence("{}", "true")
        with self.assertRaisesRegex(AnsibleFilterError, "path-safe verdict must be a boolean"):
            _controller_evidence("{}", path_safe="true")
        with self.assertRaisesRegex(AnsibleFilterError, "JSON must be text or bytes"):
            _controller_evidence({})
        with self.assertRaisesRegex(AnsibleFilterError, "nonempty expected context"):
            _MODULE.bootwright_ceph_controller_owner_evidence("{}", True, True, "", "ceph-a", "seed")
        with self.assertRaisesRegex(AnsibleFilterError, "nonempty expected cluster"):
            _MODULE.bootwright_ceph_host_marker_evidence("{}", True, True, None)


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
        self.assertEqual(got["clusters"][0]["fsid"], "11111111-1111-1111-1111-111111111111")
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
        self.assertNotEqual(authorized["clusters"][0]["nodes"][0]["outcome"], "completed")

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

    def test_terminal_completed_nodes_allow_noop_but_require_consistent_fsid(self):
        missing = _entry("ceph-a", "a1", proof=0, completion=0)
        del missing["bootwright_ceph_settle_owned_fsid"]
        got = _MODULE.bootwright_storage_destroy_attestation([missing], True, False)
        self.assertEqual(got["clusters"][0]["fsid"], "")
        self.assertEqual(got["clusters"][0]["nodes"][0]["outcome"], "completed")
        mismatched = [
            _entry("ceph-a", "a1", proof=0, completion=0),
            _entry("ceph-a", "a2", proof=0, completion=0),
        ]
        mismatched[1]["bootwright_ceph_settle_owned_fsid"] = "22222222-2222-2222-2222-222222222222"
        with self.assertRaises(AnsibleFilterError):
            _MODULE.bootwright_storage_destroy_attestation(mismatched, True, False)

    def test_filter_is_registered(self):
        filters = _MODULE.FilterModule().filters()
        for name, implementation in {
            "bootwright_ceph_controller_owner_evidence": _MODULE.bootwright_ceph_controller_owner_evidence,
            "bootwright_ceph_host_marker_evidence": _MODULE.bootwright_ceph_host_marker_evidence,
            "bootwright_storage_destroy_attestation": _MODULE.bootwright_storage_destroy_attestation,
        }.items():
            with self.subTest(name=name):
                self.assertIs(filters[name], implementation)


if __name__ == "__main__":
    unittest.main()
