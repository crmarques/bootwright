"""The lend's completion as the KubeVirt protocol publishes it."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_protocol as protocol

SERVER = "https://api.host.example.test:6443"
SECRET = "bootwright-kubevirt-lend-0123456789abcdef"


def test_the_lend_publishes_its_bounded_evidence_and_refuses_a_short_grant():
    digest = "a" * 64
    evidence, refusal = protocol.completion({"subject": "lend", "digest": digest,
                                             "observation": {"expirationSeconds": 1800, "postcondition": True, "secret": SECRET, "server": SERVER}})
    assert refusal is None and evidence == {"expirationSeconds": 1800, "postcondition": True, "request": digest, "secret": SECRET, "server": SERVER,
                                            "version": "substrate-access-kubevirt-evidence-v1"}
    ignored_evidence, refusal = protocol.completion({"subject": "lend", "digest": digest,
                                                     "observation": {"expirationSeconds": 900, "postcondition": False, "secret": SECRET, "server": SERVER}})
    assert refusal == "the KubeVirt host access was not lent"


def test_the_revoke_publishes_the_secret_it_deleted():
    digest = "a" * 64
    evidence, refusal = protocol.completion({"subject": "revoke", "digest": digest,
                                             "observation": {"expirationSeconds": 0, "postcondition": True, "secret": SECRET, "server": SERVER}})
    assert refusal is None and evidence["secret"] == SECRET and evidence["request"] == digest
    ignored_evidence, refusal = protocol.completion({"subject": "revoke", "digest": digest,
                                                     "observation": {"expirationSeconds": 0, "secret": SECRET, "server": SERVER}})
    assert refusal == "the KubeVirt host access was not revoked"
