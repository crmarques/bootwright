"""The ssh_key_policy module's main() runs with the host's SSH policy supplied, as a task would run it."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import ssh_key_policy as module

POLICY = {"configuration": "HostKeyAlgorithms ssh-ed25519\n", "hostKeyAlgorithms": "ssh-ed25519", "fleetKeyAlgorithms": "ssh-ed25519,rsa-sha2-512"}


@pytest.fixture(autouse=True)
def host_policy(monkeypatch):
    """The host's backend and ssh -G, replaced by the policy it would evaluate to."""
    monkeypatch.setattr(module, "read_policy", lambda: {})
    monkeypatch.setattr(module, "effective_policy", lambda data: dict(POLICY))


def test_admitted_key_types_return_the_effective_policy_and_change_nothing(run_module):
    keys = [{"secret": "installed-host-key", "type": "ssh-ed25519", "usage": "host"},
            {"secret": "fleet-key", "type": "ssh-rsa", "usage": "fleet"}]
    result = run_module(module, {"keys": keys})
    assert result["changed"] is False and not result.get("failed")
    assert {name: result[name] for name in POLICY} == POLICY


def test_a_key_type_the_host_excludes_fails_with_its_reason_naming_the_secret(run_module):
    result = run_module(module, {"keys": [{"secret": "installed-host-key", "type": "ssh-rsa", "usage": "host"}]})
    assert result["failed"] is True and result["reason"] == "ssh-policy-host-excluded"
    assert "Secret/installed-host-key" in result["msg"] and "nothing was booted" in result["msg"]


def test_an_unreadable_policy_fails_with_the_unavailable_reason(run_module, monkeypatch):
    def unavailable(data):
        raise module.PolicyError("the SSH policy could not be read")

    monkeypatch.setattr(module, "effective_policy", unavailable)
    result = run_module(module, {"keys": []})
    assert result["failed"] is True and result["reason"] == "ssh-policy-unavailable"
