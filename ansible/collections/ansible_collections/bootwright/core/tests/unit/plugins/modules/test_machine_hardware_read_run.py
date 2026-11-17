"""The machine_hardware_read module's main() runs with the host's interfaces supplied, as a task would run it."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import machine_hardware_read as module


def test_the_addresses_the_host_reports_are_returned_without_a_change(run_module, monkeypatch):
    monkeypatch.setattr(module, "read", lambda: ["52:54:00:00:00:01", "52:54:00:00:00:02"])
    result = run_module(module, {})
    assert result["changed"] is False and result["addresses"] == ["52:54:00:00:00:01", "52:54:00:00:00:02"]


def test_interfaces_that_cannot_be_read_in_full_fail_the_task_with_the_reason(run_module, monkeypatch):
    def unreadable():
        raise module.Unreadable("the interfaces of this host could not be read in full")

    monkeypatch.setattr(module, "read", unreadable)
    result = run_module(module, {})
    assert result["failed"] is True and result["msg"] == "the interfaces of this host could not be read in full"
    assert "addresses" not in result
