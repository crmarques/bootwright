"""Each power operation asks for exactly one reset kind and proves its arrival."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.modules import redfish_boot


@pytest.fixture(name="controller")
def controller_fixture(monkeypatch):
    calls = []
    monkeypatch.setattr(redfish_control, "reset", lambda *a: calls.append(a[3]))
    monkeypatch.setattr(redfish_control, "await_power", lambda *a, **k: True)
    return calls


def state(monkeypatch, value):
    monkeypatch.setattr(redfish_control, "power_state", lambda *a, **k: value)


# A graceful stop asks the operating system to shut down; forcing the power off
# does not. The two are separate requests, never one with a silent fallback.
def test_a_graceful_stop_and_a_forced_stop_are_different_requests(monkeypatch, controller):
    state(monkeypatch, "On")
    assert redfish_boot.drive(None, "http://c/1", "u", "p", "shutdown", 3)
    assert redfish_boot.drive(None, "http://c/1", "u", "p", "power-off", 3)
    assert controller == ["GracefulShutdown", "ForceOff"]


def test_a_machine_already_in_the_requested_state_is_left_alone(monkeypatch, controller):
    state(monkeypatch, "Off")
    assert not redfish_boot.drive(None, "http://c/1", "u", "p", "power-off", 3)
    assert not redfish_boot.drive(None, "http://c/1", "u", "p", "shutdown", 3)
    state(monkeypatch, "On")
    assert not redfish_boot.drive(None, "http://c/1", "u", "p", "power-on", 3)
    assert controller == []


def test_a_state_that_never_arrives_is_unproved(monkeypatch):
    state(monkeypatch, "On")
    monkeypatch.setattr(redfish_control, "reset", lambda *a: None)
    monkeypatch.setattr(redfish_control, "await_power", lambda *a, **k: False)
    with pytest.raises(ValueError):
        redfish_boot.drive(None, "http://c/1", "u", "p", "shutdown", 3)
