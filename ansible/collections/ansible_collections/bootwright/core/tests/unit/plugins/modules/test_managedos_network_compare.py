"""The compare module never fails on what nmstate reported; it names a difference."""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.modules.managedos_network_compare import result

DOCUMENT = {"interfaces": [{"name": "nic0", "type": "ethernet", "state": "up"}]}


@pytest.mark.parametrize("running,difference", [
    ("", "the running network is not a JSON object"),
    ("[1, 2]", "the running network is not a JSON object"),
    ("{not json", "the running network is not a JSON object"),
    ("[" * 100000 + "]" * 100000, "the running network is not a JSON object"),
    (" " * ((1 << 20) + 1), "the running network exceeds 1 MiB"),
], ids=["empty", "array", "malformed", "deep", "oversized"])
def test_an_unreadable_running_network_is_a_difference_not_a_failure(running, difference):
    assert result(DOCUMENT, running) == {"changed": False, "matched": False, "count": 1, "differences": [difference]}


def test_a_running_network_holding_the_subset_matches():
    running = json.dumps({"interfaces": [{"name": "nic0", "type": "ethernet", "state": "up", "mtu": 1500}]})
    assert result(DOCUMENT, running) == {"changed": False, "matched": True, "count": 0, "differences": []}


def test_the_count_is_every_difference_and_the_list_is_bounded():
    document = {"interfaces": [{"name": "nic%d" % index + "x" * 250, "state": "up"} for index in range(20)]}
    outcome = result(document, json.dumps({"interfaces": []}))
    assert outcome["count"] == 20 and outcome["matched"] is False
    assert len(outcome["differences"]) == 16
    assert all(len(entry.encode("utf-8")) <= 200 for entry in outcome["differences"])
