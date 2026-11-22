"""Drive the provided update module's native Ansible entrypoint offline."""

from __future__ import annotations

import os

from ansible_collections.bootwright.core.plugins.modules import managedos_provided_update as module

INPUT = {"operation": "apply", "owner": "1" * 64, "request": "2" * 64, "release": "9.8"}


def test_the_module_passes_the_frozen_request(monkeypatch, run_module):
    seen = []

    def update(*args):
        seen.append(args)
        return {"changed": True, "state": "upgraded", "packages": 3, "transaction": 42, "rebooted": False}

    monkeypatch.setattr(module.update, "execute", update)
    result = run_module(module, INPUT)

    assert seen == [("apply", INPUT["owner"], INPUT["request"], "9.8")]
    assert result["ok"] and result["changed"] and result["state"] == "upgraded"
    assert result["transaction"] == 42


def test_native_output_cannot_reach_the_ansible_channels(capfd):
    with module.quiet_native():
        os.write(1, b"native output")
        os.write(2, b"native diagnostics")
    assert tuple(capfd.readouterr()) == ("", "")


def test_native_refusals_are_closed_and_unexpected_errors_are_hidden(monkeypatch, run_module):
    def stop(*args):
        raise module.Refused("update-release-drift", detail="9.9@baseos")

    monkeypatch.setattr(module.update, "execute", stop)
    result = run_module(module, INPUT)
    assert result["reason"] == "update-release-drift" and result["detail"] == "9.9@baseos"
    assert not result["changed"] and not result["ok"]

    def fail(*args):
        raise ValueError("private native diagnostics")

    monkeypatch.setattr(module.update, "execute", fail)
    result = run_module(module, INPUT)
    assert result["reason"] == "update-failed" and result["detail"] == "transaction"
    assert "private native diagnostics" not in repr(result)
