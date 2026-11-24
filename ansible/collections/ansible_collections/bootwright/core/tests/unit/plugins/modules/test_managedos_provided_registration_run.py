"""Drive the provided registration module's native Ansible entrypoint offline."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import managedos_provided_registration as module

INPUT = {"operation": "apply", "request": "2" * 64, "hostname": "satellite.example.test",
         "content_base_url": "https://satellite.example.test/pulp/content", "ca_bundle": "bundle"}
CONSUMER = "11111111-2222-4333-8444-555555555555"


def test_the_module_passes_the_frozen_request(monkeypatch, run_module):
    seen = []

    def execute(*args):
        seen.append(args)
        return {"changed": True, "state": "converged", "consumer": CONSUMER, "postcondition": True}

    monkeypatch.setattr(module.registration, "execute", execute)
    result = run_module(module, INPUT)
    assert seen == [("apply", INPUT["request"], INPUT["hostname"], INPUT["content_base_url"], "bundle")]
    assert result["ok"] and result["changed"] and result["state"] == "converged" and result["consumer"] == CONSUMER


def test_refusals_are_closed_and_unexpected_errors_are_hidden(monkeypatch, run_module):
    def stop(*args):
        raise module.Refused("registration-content-root", detail="absent")

    monkeypatch.setattr(module.registration, "execute", stop)
    result = run_module(module, INPUT)
    assert result["reason"] == "registration-content-root" and result["detail"] == "absent"
    assert not result["changed"] and not result["ok"]

    def unknown(*args):
        raise module.Refused("registration-input")

    monkeypatch.setattr(module.registration, "execute", unknown)
    assert run_module(module, INPUT)["reason"] == "registration-unavailable"

    def fail(*args):
        raise ValueError("private native diagnostics")

    monkeypatch.setattr(module.registration, "execute", fail)
    result = run_module(module, INPUT)
    assert result["reason"] == "registration-unavailable" and result["detail"] == "transaction"
    assert "private native diagnostics" not in repr(result)


def test_the_bundle_is_never_logged(monkeypatch, run_module):
    monkeypatch.setattr(module.registration, "execute", lambda *args: {"changed": False, "state": "absent", "consumer": "", "postcondition": False})
    result = run_module(module, dict(INPUT, ca_bundle="secret-bundle-sentinel"))
    assert "secret-bundle-sentinel" not in repr(result)
