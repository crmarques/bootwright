"""The managedos_registration module's main() runs with its guest session stubbed, as a task would run it.

The worker, the material rules and the session are tested in
test_managedos_registration.py. These cases hold the module's part: the
arguments it reads and defaults, the outcome that ends the task, and the
refusal of an argument the module will not carry to a guest.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import managedos_registration as module

DIGEST = "a" * 64
INSPECT = {"operation": "inspect", "address": "192.0.2.10", "user": "fleet", "identity": "/run/lent/key", "ssh_config": "/run/lent/ssh_config",
           "known_hosts": "/run/lent/known_hosts", "host_key_algorithms": "ssh-ed25519", "policy_host_key_algorithms": "ssh-ed25519", "request": DIGEST}
REGISTER = dict(INSPECT, operation="register", organization_file="/run/lent/organization", activation_key_file="/run/lent/activation",
                enable=["rhel-9-for-x86_64-baseos-rpms"], disable=["*"])


@pytest.fixture(name="seen")
def stubbed_session(monkeypatch):
    """execute(), recording the parameters it was handed and answering from `outcome`, or raising it."""
    state = {"params": [], "outcome": {"changed": True, "ok": True, "registered": True}}

    def execute(params):
        state["params"].append(dict(params))
        if isinstance(state["outcome"], Exception):
            raise state["outcome"]  # pylint: disable=raising-bad-type
        return state["outcome"]

    monkeypatch.setattr(module, "execute", execute)
    return state


def test_an_inspection_reads_its_defaults_and_returns_the_sessions_outcome(seen, run_module):
    seen["outcome"] = {"changed": False, "ok": True, "registered": False}
    result = run_module(module, INSPECT)
    assert result["changed"] is False and result["ok"] is True and result["registered"] is False
    (params,) = seen["params"]
    assert params["timeout"] == 630 and params["enable"] == [] and params["disable"] == [] and params["release"] == ""
    assert params["operation"] == "inspect" and params["identity"] == "/run/lent/key"


def test_a_registration_hands_the_session_the_material_paths_and_never_their_content(seen, run_module):
    result = run_module(module, REGISTER)
    assert result["changed"] is True and result["registered"] is True
    (params,) = seen["params"]
    assert params["organization_file"] == "/run/lent/organization" and params["activation_key_file"] == "/run/lent/activation"
    assert params["enable"] == ["rhel-9-for-x86_64-baseos-rpms"] and params["disable"] == ["*"]


def test_an_argument_the_module_refuses_fails_the_task_with_its_message(seen, run_module):
    seen["outcome"] = module.ArgumentError("organization_file is not one line")
    result = run_module(module, REGISTER)
    assert result["failed"] is True and result["msg"] == "organization_file is not one line"


def test_a_registration_without_its_material_files_fails_before_the_session(seen, run_module):
    result = run_module(module, dict(INSPECT, operation="register"))
    assert result["failed"] is True and "organization_file" in result["msg"]
    assert seen["params"] == []
