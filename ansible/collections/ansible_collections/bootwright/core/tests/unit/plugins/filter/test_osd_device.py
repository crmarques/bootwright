from __future__ import annotations

import importlib.util
import pathlib
import sys
import unittest


if "ansible" not in sys.modules:
    ansible_module = type(sys)("ansible")
    sys.modules["ansible"] = ansible_module
if "ansible.errors" not in sys.modules:
    errors_module = type(sys)("ansible.errors")

    class _AnsibleFilterError(Exception):
        pass

    errors_module.AnsibleFilterError = _AnsibleFilterError
    sys.modules["ansible.errors"] = errors_module

_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_osd_device_filter",
    _FILTER_DIR / "osd_device.py",
)
assert _spec and _spec.loader, "could not locate osd_device.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_ceph_osd_filter_candidates = _module.bootwright_ceph_osd_filter_candidates
bootwright_json_list_probe = _module.bootwright_json_list_probe
bootwright_reclaim_device_operand = _module.bootwright_reclaim_device_operand
bootwright_reclaim_device_operand_probe = _module.bootwright_reclaim_device_operand_probe


def _device(path, *, available=False, model="DATA-100", vendor="ACME", rotational="0", size=100_000_000_000, **extra):
    return {
        "path": path,
        "available": available,
        "sys_api": {
            "model": model,
            "vendor": vendor,
            "rotational": rotational,
            "size": size,
        },
        **extra,
    }


class OSDDeviceFilterCandidates(unittest.TestCase):
    def test_and_filter_matches_substrings_rotation_and_size(self):
        got = bootwright_ceph_osd_filter_candidates(
            [_device("/dev/sdb"), _device("/dev/sdc", size=90_000_000_000)],
            [{"role": "data", "model": "DATA", "vendor": "AC", "rotational": False, "size": "100G:200G"}],
        )
        self.assertEqual(got, {"candidates": [{"path": "/dev/sdb", "roles": ["data"]}], "unknown": []})

    def test_and_intersects_while_or_unions_filters(self):
        devices = [_device("/dev/sdb", model="OTHER")]
        and_result = bootwright_ceph_osd_filter_candidates(
            devices,
            [{"role": "data", "model": "DATA", "vendor": "ACME", "filterLogic": "AND"}],
        )
        or_result = bootwright_ceph_osd_filter_candidates(
            devices,
            [{"role": "data", "model": "DATA", "vendor": "ACME", "filterLogic": "OR"}],
        )
        self.assertEqual(and_result["candidates"], [])
        self.assertEqual(or_result["candidates"], [{"path": "/dev/sdb", "roles": ["data"]}])

    def test_known_result_short_circuits_an_unavailable_attribute(self):
        device = {"path": "/dev/sdb", "available": False, "sys_api": {"model": "OTHER"}}
        and_result = bootwright_ceph_osd_filter_candidates(
            [device],
            [{"role": "data", "model": "DATA", "vendor": "ACME", "filterLogic": "AND"}],
        )
        or_result = bootwright_ceph_osd_filter_candidates(
            [device],
            [{"role": "data", "model": "OTHER", "vendor": "ACME", "filterLogic": "OR"}],
        )
        self.assertEqual(and_result, {"candidates": [], "unknown": []})
        self.assertEqual(or_result, {"candidates": [{"path": "/dev/sdb", "roles": ["data"]}], "unknown": []})

    def test_limit_does_not_hide_a_possible_all_filter_match(self):
        got = bootwright_ceph_osd_filter_candidates(
            [_device("/dev/sdb")],
            [{"role": "data", "all": True, "limit": 1}],
        )
        self.assertEqual(got["candidates"], [{"path": "/dev/sdb", "roles": ["data"]}])

    def test_available_live_excluded_and_replacement_devices_are_not_candidates(self):
        got = bootwright_ceph_osd_filter_candidates(
            [
                _device("/dev/available", available=True),
                _device("/dev/live", osd_ids=[7]),
                _device("/dev/excluded"),
                _device("/dev/replacing", being_replaced=True),
            ],
            [{"role": "data", "all": True}],
            ["/dev/excluded"],
        )
        self.assertEqual(got, {"candidates": [], "unknown": []})

    def test_multiple_roles_are_deduplicated_per_path(self):
        got = bootwright_ceph_osd_filter_candidates(
            [_device("/dev/nvme0n1")],
            [
                {"role": "data", "model": "DATA"},
                {"role": "db", "rotational": False},
            ],
        )
        self.assertEqual(got["candidates"], [{"path": "/dev/nvme0n1", "roles": ["data", "db"]}])

    def test_missing_filter_attribute_is_unknown_and_never_a_non_match(self):
        got = bootwright_ceph_osd_filter_candidates(
            [{"path": "/dev/sdb", "available": False, "sys_api": {}}],
            [{"role": "data", "model": "DATA"}],
        )
        self.assertEqual(got["candidates"], [])
        self.assertEqual(got["unknown"][0]["path"], "/dev/sdb")
        self.assertIn("model", got["unknown"][0]["reason"])

    def test_unknown_desired_filter_field_fails_closed_without_devices(self):
        got = bootwright_ceph_osd_filter_candidates(
            [],
            [{"role": "data", "model": "DATA", "serial": "unsafe-new-field"}],
        )
        self.assertEqual(got["candidates"], [])
        self.assertEqual(got["unknown"][0]["path"], "<desired>")
        self.assertIn("serial", got["unknown"][0]["reason"])

    def test_invalid_selector_shapes_fail_closed_without_devices(self):
        for device_filter, reason in (
            ({"role": "data", "limit": 1}, "limit only"),
            ({"role": "data", "all": True, "model": "DATA"}, "mutually exclusive"),
            ({"role": "db", "all": True}, "data devices"),
        ):
            with self.subTest(device_filter=device_filter):
                got = bootwright_ceph_osd_filter_candidates([], [device_filter])
                self.assertEqual(got["candidates"], [])
                self.assertIn(reason, got["unknown"][0]["reason"])

    def test_size_bounds_are_inclusive(self):
        got = bootwright_ceph_osd_filter_candidates(
            [_device("/dev/low", size=100_000_000_000), _device("/dev/high", size=200_000_000_000)],
            [{"role": "data", "size": "100G:200G"}],
        )
        self.assertEqual([item["path"] for item in got["candidates"]], ["/dev/high", "/dev/low"])


class FilterRegistration(unittest.TestCase):
    def test_filter_module_exposes_filter(self):
        registered = _module.FilterModule().filters()
        self.assertIs(registered["bootwright_ceph_osd_filter_candidates"], bootwright_ceph_osd_filter_candidates)
        self.assertIs(registered["bootwright_json_list_probe"], bootwright_json_list_probe)
        self.assertIs(registered["bootwright_reclaim_device_operand"], bootwright_reclaim_device_operand)
        self.assertIs(registered["bootwright_reclaim_device_operand_probe"], bootwright_reclaim_device_operand_probe)

    def test_json_list_probe_never_throws_on_malformed_command_output(self):
        self.assertEqual(bootwright_json_list_probe("not-json"), {"valid": False, "value": []})
        self.assertEqual(bootwright_json_list_probe('{"unexpected": true}'), {"valid": False, "value": []})
        self.assertEqual(bootwright_json_list_probe('[{"name": "host-a"}]'), {"valid": True, "value": [{"name": "host-a"}]})

    def test_reclaim_operand_preserves_and_quotes_only_at_the_command_boundary(self):
        hostile = "/dev/disk/by-id/osd '$(printf unsafe);$HOME"
        self.assertEqual(
            bootwright_reclaim_device_operand(["/dev/sdb"], [hostile, "/dev/sdb"]),
            "/dev/sdb," + hostile,
        )

    def test_reclaim_operand_fails_closed_on_empty_or_unrepresentable_paths(self):
        for preserved, runtime in (
            ([], []),
            ([], ["relative"]),
            ([], ["/dev/a,/dev/b"]),
            ([], ["/dev/a\n/dev/b"]),
            ([], ["/dev/__BOOTWRIGHT_RUNTIME_RECLAIM_DEVICES_7EF51C56__"]),
        ):
            with self.subTest(preserved=preserved, runtime=runtime):
                with self.assertRaises(_AnsibleFilterError):
                    bootwright_reclaim_device_operand(preserved, runtime)

    def test_reclaim_operand_probe_returns_actionable_failure_evidence(self):
        got = bootwright_reclaim_device_operand_probe([], ["/dev/a,/dev/b"])
        self.assertFalse(got["valid"])
        self.assertEqual(got["value"], "")
        self.assertIn("represented safely", got["reason"])


if __name__ == "__main__":
    unittest.main()
