"""Both Python crypto-policy reducers give the verdict and configuration the Go reducer's corpus names."""

import json
from pathlib import Path
import unittest

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
