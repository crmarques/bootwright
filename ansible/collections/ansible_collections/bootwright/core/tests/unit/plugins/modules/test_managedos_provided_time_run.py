"""Drive the provided time module's native Ansible entrypoint offline."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import managedos_provided_time as module

INPUT = {"operation": "apply", "request": "2" * 64, "servers": ["192.0.2.123", "ntp.example.test"]}


def test_the_module_passes_the_frozen_request(monkeypatch, run_module):
    seen = []

    def execute(*args):
        seen.append(args)
        return {"changed": True, "state": "complete", "postcondition": True, "servers": INPUT["servers"]}

    monkeypatch.setattr(module.worker, "execute", execute)
    result = run_module(module, INPUT)

    assert seen == [("apply", INPUT["request"], INPUT["servers"])]
    assert result["ok"] and result["changed"] and result["state"] == "complete"


def test_native_refusals_carry_their_closed_detail_and_unexpected_errors_are_hidden(monkeypatch, run_module):
    def stop(*args):
        raise module.worker.Refused("time-service-conflict", "ntpd.service")

    monkeypatch.setattr(module.worker, "execute", stop)
    result = run_module(module, INPUT)
    assert (result["reason"], result["detail"], result["ok"], result["changed"]) == ("time-service-conflict", "ntpd.service", False, False)

    def other(*args):
        raise module.worker.Refused("surprise")

    monkeypatch.setattr(module.worker, "execute", other)
    assert run_module(module, INPUT)["reason"] == "time-unavailable"

    def fail(*args):
        raise ValueError("private native diagnostics")

    monkeypatch.setattr(module.worker, "execute", fail)
    result = run_module(module, INPUT)
    assert result["reason"] == "time-unavailable" and not result["ok"]
    assert "private native diagnostics" not in repr(result)
