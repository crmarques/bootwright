"""Drive the provided hostname module's native Ansible entrypoint offline."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import managedos_provided_hostname as module

INPUT = {"operation": "apply", "request": "2" * 64, "hostname": "witness.example.test"}


def test_the_module_passes_the_frozen_request(monkeypatch, run_module):
    seen = []

    def execute(*args):
        seen.append(args)
        return {"changed": True, "state": "complete", "hostname": INPUT["hostname"]}

    monkeypatch.setattr(module.worker, "execute", execute)
    result = run_module(module, INPUT)

    assert seen == [("apply", INPUT["request"], INPUT["hostname"])]
    assert result["ok"] and result["changed"] and result["state"] == "complete"


def test_native_refusals_are_closed_and_unexpected_errors_are_hidden(monkeypatch, run_module):
    def stop(*args):
        raise module.worker.Refused("hostname-unproved")

    monkeypatch.setattr(module.worker, "execute", stop)
    result = run_module(module, INPUT)
    assert result["reason"] == "hostname-unproved" and not result["changed"] and not result["ok"]

    def fail(*args):
        raise ValueError("private native diagnostics")

    monkeypatch.setattr(module.worker, "execute", fail)
    result = run_module(module, INPUT)
    assert result["reason"] == "hostname-write" and not result["ok"]
    assert "private native diagnostics" not in repr(result)
