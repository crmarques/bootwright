from __future__ import annotations

import importlib.util
import pathlib
import unittest


_HERE = pathlib.Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location(
    "_bootwright_network_filter",
    _HERE / "network.py",
)
assert _spec and _spec.loader, "could not locate network.py next to test file"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_ip_in_cidrs = _module.bootwright_ip_in_cidrs


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


if __name__ == "__main__":
    unittest.main()
