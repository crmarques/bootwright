"""The managedos_network_compare module's main() runs over a declaration and a running network, as a task would run it."""

from __future__ import annotations

import json

from ansible_collections.bootwright.core.plugins.modules import managedos_network_compare as module

DOCUMENT = {"interfaces": [{"name": "nic0", "type": "ethernet", "state": "up"}]}


def returned(result):
    return {name: result[name] for name in ("changed", "matched", "count", "differences")}


def test_a_running_network_holding_the_declared_subset_matches(run_module):
    running = json.dumps({"interfaces": [{"name": "nic0", "type": "ethernet", "state": "up", "mtu": 1500}]})
    assert returned(run_module(module, {"document": DOCUMENT, "running": running})) == {
        "changed": False, "matched": True, "count": 0, "differences": []}


def test_a_running_network_that_is_unreadable_is_a_difference_and_never_a_failed_task(run_module):
    result = run_module(module, {"document": DOCUMENT, "running": "{not json"})
    assert not result.get("failed")
    assert returned(result) == {"changed": False, "matched": False, "count": 1,
                                "differences": ["the running network is not a JSON object"]}
