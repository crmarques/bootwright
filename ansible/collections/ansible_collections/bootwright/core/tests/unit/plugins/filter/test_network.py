from __future__ import annotations

import importlib.util
import pathlib
import unittest


_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_network_filter",
    _FILTER_DIR / "network.py",
)
assert _spec and _spec.loader, "could not locate network.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_ip_in_cidrs = _module.bootwright_ip_in_cidrs
bootwright_normalize_ip_set = _module.bootwright_normalize_ip_set
bootwright_resolved_static_conflicts = _module.bootwright_resolved_static_conflicts
bootwright_resolved_runtime_conflicts = _module.bootwright_resolved_runtime_conflicts


class IpInCidrs(unittest.TestCase):
    def test_address_inside_a_cidr_matches(self):
        self.assertTrue(bootwright_ip_in_cidrs(["10.7.7.5"], ["10.7.7.0/25"]))

    def test_address_outside_every_cidr_does_not_match(self):
        self.assertFalse(
            bootwright_ip_in_cidrs(
                ["10.7.7.200"],
                ["10.7.7.0/25", "192.168.0.0/16"],
            )
        )

    def test_matches_when_any_address_is_in_any_cidr(self):
        self.assertTrue(
            bootwright_ip_in_cidrs(
                ["203.0.113.4", "10.200.255.9"],
                ["10.0.0.0/8", "172.16.0.0/12"],
            )
        )

    def test_empty_inputs_do_not_match(self):
        self.assertFalse(bootwright_ip_in_cidrs([], ["10.0.0.0/8"]))
        self.assertFalse(bootwright_ip_in_cidrs(["10.0.0.1"], []))

    def test_scalar_inputs_are_accepted(self):
        self.assertTrue(bootwright_ip_in_cidrs("10.0.0.1", "10.0.0.0/8"))

    def test_none_inputs_do_not_match(self):
        self.assertFalse(bootwright_ip_in_cidrs(None, None))
        self.assertFalse(bootwright_ip_in_cidrs(["10.0.0.1"], None))

    def test_malformed_entries_are_ignored(self):
        self.assertFalse(bootwright_ip_in_cidrs(["not-an-ip"], ["10.0.0.0/8"]))
        self.assertFalse(bootwright_ip_in_cidrs(["10.0.0.1"], ["garbage", ""]))
        self.assertTrue(
            bootwright_ip_in_cidrs(["", "10.0.0.1", 42], ["nope", "10.0.0.0/8"])
        )

    def test_host_bits_in_cidr_are_tolerated(self):
        self.assertTrue(bootwright_ip_in_cidrs(["10.7.2.9"], ["10.7.2.5/24"]))

    def test_ipv4_address_never_matches_ipv6_cidr(self):
        self.assertFalse(bootwright_ip_in_cidrs(["10.0.0.1"], ["fd00::/8"]))

    def test_ipv6_address_inside_ipv6_cidr_matches(self):
        self.assertTrue(bootwright_ip_in_cidrs(["fd00::1"], ["fd00::/8"]))


class NormalizeIpSet(unittest.TestCase):
    def test_canonicalizes_deduplicates_and_sorts_both_families(self):
        self.assertEqual(
            bootwright_normalize_ip_set(
                ["2001:0db8:0:0::10", "192.0.2.11", "192.0.2.10", "2001:db8::10"]
            ),
            ["192.0.2.10", "192.0.2.11", "2001:db8::10"],
        )

    def test_normalizes_ipv4_mapped_and_retains_invalid_evidence(self):
        self.assertEqual(
            bootwright_normalize_ip_set(["::ffff:192.0.2.10", "bad", None]),
            ["192.0.2.10", "invalid:bad"],
        )


class ResolvedConflicts(unittest.TestCase):
    def test_static_config_accepts_only_the_exact_owned_dropin(self):
        lines = [
            "# /etc/systemd/resolved.conf",
            "DNS=192.0.2.1",
            "# /etc/systemd/resolved.conf.d/bootwright-owned.conf",
            "DNS=192.0.2.53",
            "Domains=~apps.example.test",
        ]
        self.assertEqual(
            bootwright_resolved_static_conflicts(
                lines, "/etc/systemd/resolved.conf.d/bootwright-owned.conf"
            ),
            ["/etc/systemd/resolved.conf: DNS=192.0.2.1"],
        )

    def test_runtime_owned_route_requires_exact_global_state(self):
        self.assertEqual(
            bootwright_resolved_runtime_conflicts(
                ["Global: 192.0.2.53"],
                ["Global: ~apps.example.test", "Link 2 (eth0): corp.example.test"],
                True,
                ["192.0.2.53"],
                ["apps.example.test"],
                True,
            ),
            [],
        )

    def test_runtime_rejects_global_and_overlapping_link_routes(self):
        self.assertEqual(
            bootwright_resolved_runtime_conflicts(
                ["Global: 192.0.2.54"],
                ["Global: ~apps.example.test ~foreign.example.test", "Link 2 (eth0): ~console.apps.example.test"],
                True,
                ["192.0.2.53"],
                ["apps.example.test"],
                True,
            ),
            [
                "Link 2 (eth0) Domains=~console.apps.example.test",
                "global DNS=192.0.2.54",
                "global Domains=~apps.example.test,~foreign.example.test",
            ],
        )

    def test_unowned_route_requires_an_empty_global_slot_before_mutation(self):
        self.assertEqual(
            bootwright_resolved_runtime_conflicts(
                ["Global: 192.0.2.53"],
                ["Global: ~apps.example.test"],
                False,
                ["192.0.2.53"],
                ["apps.example.test"],
            ),
            ["global DNS=192.0.2.53", "global Domains=~apps.example.test"],
        )

    def test_owned_route_may_reconcile_stale_owned_global_values(self):
        self.assertEqual(
            bootwright_resolved_runtime_conflicts(
                ["Global: 192.0.2.54"],
                ["Global: ~old.example.test"],
                True,
                ["192.0.2.53"],
                ["apps.example.test"],
            ),
            [],
        )


if __name__ == "__main__":
    unittest.main()
