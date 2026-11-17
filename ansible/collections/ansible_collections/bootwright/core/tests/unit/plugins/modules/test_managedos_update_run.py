"""The managedos_update module's main() runs with its guest session stubbed, as a task would run it.

The worker, its session and its answers are tested in test_managedos_update.py.
These cases hold the module's part: the arguments it reads and defaults, the
outcome that ends the task, and the refusal of an argument it will not carry.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import managedos_update as module

DIGEST = "0123456789abcdef" * 4
ARGUMENTS = {"operation": "inspect", "address": "192.0.2.10", "user": "fleet", "identity": "/run/lent/key", "ssh_config": "/run/lent/ssh_config",
             "known_hosts": "/run/lent/known_hosts", "host_key_algorithms": "ssh-ed25519", "policy_host_key_algorithms": "ssh-ed25519",
             "request": DIGEST, "release": "9.8", "timeout": 120}


@pytest.fixture(name="seen")
def stubbed_session(monkeypatch):
    """execute(), recording the parameters it was handed and answering from `outcome`, or raising it."""
    state = {"params": [], "outcome": {"changed": False, "ok": True, "updated": False, "evidence": {"state": "current"}}}

    def execute(params):
        state["params"].append(dict(params))
        if isinstance(state["outcome"], Exception):
            raise state["outcome"]  # pylint: disable=raising-bad-type
        return state["outcome"]

    monkeypatch.setattr(module, "execute", execute)
    return state


def test_the_defaults_reach_the_session_and_its_outcome_ends_the_task(seen, run_module):
    result = run_module(module, ARGUMENTS)
    assert result["changed"] is False and result["ok"] is True and result["evidence"] == {"state": "current"}
    (params,) = seen["params"]
    assert params["pin"] == "" and params["timeout"] == 120 and params["release"] == "9.8" and params["request"] == DIGEST


def test_an_apply_reports_the_update_the_session_made(seen, run_module):
    seen["outcome"] = {"changed": True, "ok": True, "updated": True, "evidence": {"state": "upgraded", "release": "9.8"}}
    result = run_module(module, dict(ARGUMENTS, operation="apply", pin="release", timeout=900))
    assert result["changed"] is True and result["updated"] is True and result["evidence"]["state"] == "upgraded"
    assert seen["params"][0]["operation"] == "apply" and seen["params"][0]["pin"] == "release" and seen["params"][0]["timeout"] == 900


def test_an_argument_the_module_refuses_and_an_operation_it_does_not_know_fail_the_task(seen, run_module):
    seen["outcome"] = module.ArgumentError("release is not a RHEL release")
    refused = run_module(module, dict(ARGUMENTS, release="nine"))
    assert refused["failed"] is True and refused["msg"] == "release is not a RHEL release"
    unknown = run_module(module, dict(ARGUMENTS, operation="downgrade"))
    assert unknown["failed"] is True and "operation" in unknown["msg"]
    assert len(seen["params"]) == 1


# The session's length is the frozen request's budget; the module chooses none.
def test_a_session_without_its_frozen_timeout_fails_before_the_session(seen, run_module):
    result = run_module(module, {key: value for key, value in ARGUMENTS.items() if key != "timeout"})
    assert result["failed"] is True and "timeout" in result["msg"]
    assert seen["params"] == []
