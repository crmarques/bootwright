"""The lend's completion as the KubeVirt protocol publishes it."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_protocol as protocol

SERVER = "https://api.host.example.test:6443"


def test_the_lend_publishes_its_bounded_evidence_and_refuses_a_short_grant():
    digest = "a" * 64
    evidence, refusal = protocol.completion({"subject": "lend", "digest": digest,
                                             "observation": {"expirationSeconds": 1800, "postcondition": True, "server": SERVER}})
    assert refusal is None and evidence == {"expirationSeconds": 1800, "postcondition": True, "request": digest, "server": SERVER,
                                            "version": "substrate-access-kubevirt-evidence-v1"}
    ignored_evidence, refusal = protocol.completion({"subject": "lend", "digest": digest,
                                                     "observation": {"expirationSeconds": 900, "postcondition": False, "server": SERVER}})
    assert refusal == "the KubeVirt host access was not lent"
