"""The managedos_disk_discovery module's main() runs over a stubbed discovery runtime, as a task would run it.

The runtime's custody and inventory are tested in test_disk_discovery.py and its
siblings, over a stubbed BMC. These cases hold the module's own part: the
arguments it hands the runtime and how it ends the task on the runtime's answer.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_runtime as runtime
from ansible_collections.bootwright.core.plugins.modules import managedos_disk_discovery as module

ARGUMENTS = {"operation": "observe", "request": {"version": "disk-discovery-v1"}, "digest": "d" * 64, "material": {"password": "lent-secret"}}


@pytest.fixture(name="executed")
def stubbed_runtime(monkeypatch):
    """The runtime answering from `answer` and recording the parameters it was handed."""
    state = {"answer": {"changed": False, "evidence": {"drives": 2}}, "params": []}

    def execute(params, progress=None):
        state["params"].append(params)
        return state["answer"]

    monkeypatch.setattr(runtime, "execute", execute)
    return state


def test_a_proved_inventory_ends_the_task_with_the_runtimes_evidence(executed, run_module):
    result = run_module(module, ARGUMENTS)
    assert result["changed"] is False and result["evidence"] == {"drives": 2} and not result.get("failed")
    assert executed["params"] == [ARGUMENTS]
    assert "lent-secret" not in str(result)


def test_a_failed_runtime_answer_fails_the_task_with_its_message_and_diagnostics(executed, run_module):
    executed["answer"] = {"failed": True, "changed": False, "reason": "inventory",
                          "msg": "disk discovery did not prove its bounded media, hardware identity, inventory and cleanup contract"}
    result = run_module(module, dict(ARGUMENTS, operation="apply"))
    assert result["failed"] is True and result["msg"].startswith("disk discovery did not prove") and result["reason"] == "inventory"
    assert executed["params"][0]["operation"] == "apply"


def test_an_operation_the_module_does_not_know_is_refused_before_the_runtime_runs(executed, run_module):
    result = run_module(module, dict(ARGUMENTS, operation="erase"))
    assert result["failed"] is True and "operation" in result["msg"]
    assert executed["params"] == []
