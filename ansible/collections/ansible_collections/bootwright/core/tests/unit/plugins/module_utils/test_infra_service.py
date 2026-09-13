"""The probe packets are built and read here, so they are tested without sockets."""

from __future__ import annotations

import struct

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.infra_service import (
    dns_answers,
    dns_query,
    probe_targets,
)


def test_dns_query_asks_one_a_record_for_the_exact_name():
    payload = dns_query("bastion.lab.example.test")
    identifier, flags, questions, answers = struct.unpack(">HHHH", payload[:8])
    assert (identifier, flags, questions, answers) == (0x4257, 0x0100, 1, 0)
    assert payload.endswith(struct.pack(">HH", 1, 1))
    assert b"\x07bastion" in payload


def test_dns_answers_counts_only_a_matching_reply():
    payload = dns_query("bastion.lab.example.test")
    reply = payload[:6] + struct.pack(">H", 2) + payload[8:]
    assert dns_answers(payload, reply) == 2
    with pytest.raises(ValueError):
        dns_answers(payload, b"\x00\x00" + reply[2:])
    with pytest.raises(ValueError):
        dns_answers(payload, b"short")


def test_probe_targets_follow_the_bind_address_then_the_endpoints():
    exact = {"bindAddress": "192.0.2.1", "endpoints": [{"address": "192.0.2.9"}]}
    assert probe_targets(exact) == ["192.0.2.1"]
    wildcard = {
        "bindAddress": "0.0.0.0",
        "endpoints": [{"address": "192.0.2.9"}, {"address": "192.0.2.1"}, {"address": "192.0.2.9"}],
    }
    assert probe_targets(wildcard) == ["192.0.2.1", "192.0.2.9"]
    assert probe_targets({"bindAddress": "::", "endpoints": []}) == []
