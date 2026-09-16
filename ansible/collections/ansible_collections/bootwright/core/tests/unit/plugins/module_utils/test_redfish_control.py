"""The controller contract is exercised here, so it is tested without a BMC."""

from __future__ import annotations

import pytest

import ssl
import urllib.request

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control


def test_only_a_reported_power_state_is_ever_returned(monkeypatch):
    answers = iter([{"PowerState": "On"}, {"PowerState": "Unknown"}, {}])
    monkeypatch.setattr(redfish_control, "request", lambda *a, **k: next(answers))
    assert redfish_control.power_state("http://c/1", "u", "p") == "On"
    assert redfish_control.power_state("http://c/1", "u", "p") == ""
    assert redfish_control.power_state("http://c/1", "u", "p") == ""


def test_media_is_reported_only_while_a_device_says_it_is_inserted(monkeypatch):
    monkeypatch.setattr(redfish_control, "request", lambda *a, **k: {"Inserted": True, "Image": "http://s/i.iso"})
    assert redfish_control.media_inserted("http://c/1", "u", "p") == "http://s/i.iso"
    monkeypatch.setattr(redfish_control, "request", lambda *a, **k: {"Inserted": False})
    assert redfish_control.media_inserted("http://c/1", "u", "p") == ""


def test_every_action_names_its_own_resource(monkeypatch):
    calls = []

    def record(endpoint, user, password, method="GET", path="", payload=None, **kwargs):
        calls.append((method, path, payload))
        return {}

    monkeypatch.setattr(redfish_control, "request", record)
    redfish_control.insert_media("http://c/1", "u", "p", "http://s/i.iso")
    redfish_control.eject_media("http://c/1", "u", "p")
    redfish_control.boot_once("http://c/1", "u", "p")
    redfish_control.reset("http://c/1", "u", "p", "ForceOff")
    assert calls[0][0:2] == ("POST", "/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia")
    assert calls[0][2] == {"Image": "http://s/i.iso", "Inserted": True, "WriteProtected": True}
    assert calls[1][0:2] == ("POST", "/VirtualMedia/Cd/Actions/VirtualMedia.EjectMedia")
    assert calls[2][0] == "PATCH"
    assert calls[2][2]["Boot"]["BootSourceOverrideEnabled"] == "Once"
    assert calls[3][2] == {"ResetType": "ForceOff"}


# A power request is not evidence: the outcome is whatever the resource reports
# within the bounded window, and an outcome that never arrives is unproved.
def test_a_power_outcome_is_polled_rather_than_assumed(monkeypatch):
    states = iter(["Off", "Off", "On"])
    monkeypatch.setattr(redfish_control, "power_state", lambda *a, **k: next(states))
    assert redfish_control.await_power("http://c/1", "u", "p", "On", attempts=5, sleep=lambda _: None)

    monkeypatch.setattr(redfish_control, "power_state", lambda *a, **k: "Off")
    assert not redfish_control.await_power("http://c/1", "u", "p", "On", attempts=3, sleep=lambda _: None)


def test_an_unreadable_resource_never_proves_a_state(monkeypatch):
    def refuse(*_args, **_kwargs):
        raise OSError("unreachable")

    monkeypatch.setattr(redfish_control, "power_state", refuse)
    assert not redfish_control.await_power("http://c/1", "u", "p", "On", attempts=2, sleep=lambda _: None)


def test_a_redirect_is_refused_rather_than_followed():
    handler = redfish_control.NoRedirect()
    with pytest.raises(Exception):
        handler.redirect_request(_Request(), None, 302, "moved", {}, "http://elsewhere/")


class _Request:
    full_url = "http://c/1"


# A management controller is addressed by the frozen request, so the client
# reaches it directly. urllib consults the ambient proxy environment by default
# and its bypass list does not cover a concrete controller address, so a
# proxy's own refusal would arrive looking like the controller's.
def test_the_client_never_consults_an_ambient_proxy(monkeypatch):
    monkeypatch.setenv("https_proxy", "http://proxy.example.test:3128")
    monkeypatch.setenv("http_proxy", "http://proxy.example.test:3128")

    def proxied(opener):
        return [h for h in opener.handlers
                if isinstance(h, urllib.request.ProxyHandler) and h.proxies]

    assert proxied(urllib.request.build_opener()), "the default opener is expected to proxy"
    assert not proxied(redfish_control._opener())


# Verification is on unless the declaration opts out, and the opt-out reaches
# exactly one opener rather than becoming a process-wide default.
def test_verification_is_opted_out_of_per_call():
    assert redfish_control._opener(True) is not redfish_control._opener(False)
    contexts = [h for h in redfish_control._opener(False).handlers
                if isinstance(h, urllib.request.HTTPSHandler)]
    assert contexts
    assert ssl.create_default_context().verify_mode == ssl.CERT_REQUIRED
