"""The Python crypto-policy reducer gives the verdict and configuration the Go reducer's corpus names.

ceph_tunnel owns the reducer and the key-type table, because it runs alone on a
host; ssh_key_policy reads both from it and only words its own refusal.
"""

import json
from pathlib import Path
import unittest
from unittest import mock

from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel, ssh_key_policy

CASES = json.loads((Path(__file__).resolve().parent / "goldens/ssh_crypto_policy_lines.json").read_text(encoding="utf-8"))["cases"]


def text(policy):
    if isinstance(policy, str):
        return policy
    return policy["head"] + policy["fill"] * (policy["total"] - len(policy["head"]) - len(policy["tail"])) + policy["tail"]


def placement_host(policy):
    try:
        return ssh_key_policy.configuration(policy)
    except ssh_key_policy.PolicyError:
        return None


def ceph_session(policy):
    try:
        return ceph_tunnel.crypto_policy(policy)
    except ceph_tunnel.TunnelRefusal:
        return None


class SSHCryptoPolicyCorpusTests(unittest.TestCase):
    def check(self, reducer):
        for case in CASES:
            with self.subTest(case["case"]):
                policy = text(case["policy"])
                if case["verdict"] == "refuse":
                    self.assertIsNone(reducer(policy))
                else:
                    expected = policy if case["config"] is None else case["config"]
                    self.assertEqual(reducer(policy), expected)

    def test_the_placement_host_reducer_follows_the_corpus(self):
        self.check(placement_host)

    def test_the_ceph_session_reducer_follows_the_corpus(self):
        self.check(ceph_session)

    def test_the_corpus_names_every_cryptographic_directive_both_reducers_allow(self):
        lines = [case["policy"] for case in CASES if case["case"].startswith("cryptographic directive ") and case["verdict"] == "accept"]
        named = {line.split()[0].lower() for line in lines}
        self.assertEqual(named, set(ssh_key_policy.DIRECTIVES))
        self.assertEqual(named, set(ceph_tunnel.CRYPTO_DIRECTIVES))

    def test_ssh_key_policy_reads_the_reducer_and_table_ceph_tunnel_owns(self):
        self.assertIs(ssh_key_policy.DIRECTIVES, ceph_tunnel.CRYPTO_DIRECTIVES)
        self.assertEqual(ssh_key_policy.ALGORITHMS, {key_type: ",".join(names) for key_type, names in ceph_tunnel.HOST_KEY_ALGORITHMS.items()})
        with mock.patch.object(ceph_tunnel, "crypto_policy", return_value="Ciphers aes256-gcm@openssh.com\n") as reducer:
            self.assertEqual(ssh_key_policy.configuration("any"), "Ciphers aes256-gcm@openssh.com\n")
        reducer.assert_called_once_with("any")
