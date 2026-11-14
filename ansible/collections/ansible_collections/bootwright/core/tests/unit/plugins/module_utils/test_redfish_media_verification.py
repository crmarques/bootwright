"""Preflight and conditional writes keep shared-manager recovery monotonic."""

from __future__ import annotations

from copy import deepcopy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_media_verification as verification

PROPERTY = "HttpsTransferCertVerification"
FIRST = "/redfish/v1/Managers/1/SecurityService"
SECOND = "/redfish/v1/Managers/2/SecurityService"
MEMBER = "/redfish/v1/Managers/1/VirtualMedia/CD"


class Controller:
    def __init__(self, values):
        self.resources = {url: {PROPERTY: value, "@odata.etag": '"initial"'} for url, value in values.items()}
        self._managers = [(url.rsplit("/", 1)[0], {"SecurityService": {"@odata.id": url}}) for url in values]
        self.writes = []
        self.denied = ""
        self.stale = False
        self.apply_write = True
        self.wanted = True

    def _own(self, link, doing):
        assert link.startswith("/redfish/v1/")
        return link

    def _read(self, url, doing):
        return deepcopy(self.resources[url]), {}

    def fetch(self, url, method, payload, headers, doing):
        assert method == "PATCH" and headers == {"If-Match": '"initial"'}
        assert list(payload.values()) == [self.wanted]
        self.writes.append((url, payload))
        if url == self.denied:
            return 400, {"Messages": {"MessageId": "Base.1.0.PropertyModificationNeedPrivilege"}}, {}
        if self.stale:
            return 412, {}, {}
        if self.apply_write:
            self.resources[url].update(payload)
        return 204, None, {}


@pytest.mark.parametrize("value", [None, 0, "false", [], {}])
def test_unknown_later_setting_refuses_before_any_write(value):
    client = Controller({FIRST: False, SECOND: value})
    with pytest.raises(verification.Refused):
        verification.configure(client, MEMBER, True)
    assert client.writes == []


@pytest.mark.parametrize("tag", [None, "", "*", '"bad\nheader"', 1])
def test_unusable_later_etag_refuses_before_any_write(tag):
    client = Controller({FIRST: False, SECOND: False})
    client.resources[SECOND]["@odata.etag"] = tag
    with pytest.raises(verification.Refused):
        verification.configure(client, MEMBER, True)
    assert client.writes == []


def test_partial_success_is_retained_and_retry_converges_without_lowering_verification():
    client = Controller({FIRST: False, SECOND: False})
    client.denied = SECOND
    with pytest.raises(verification.Refused) as caught:
        verification.configure(client, MEMBER, True)
    assert caught.value.cause == "privilege"
    assert client.resources[FIRST][PROPERTY] is True and client.resources[SECOND][PROPERTY] is False
    client.denied, client.writes = "", []
    assert verification.configure(client, MEMBER, True) is True
    assert client.writes == [(SECOND, {PROPERTY: True})]
    assert verification.configure(client, MEMBER, True) is False


def test_alias_links_are_deduplicated_before_writing():
    client = Controller({FIRST: False})
    client._managers *= 2
    client._managers[0][1]["Oem"] = {"Example": {"SecurityService": {"@odata.id": FIRST}}}
    assert verification.configure(client, MEMBER, True) is True
    assert client.writes == [(FIRST, {PROPERTY: True})]


def test_device_flag_decides_only_when_no_manager_publishes_the_switch():
    client = Controller({})
    client.resources[MEMBER] = {"VerifyCertificate": False, "@odata.etag": '"initial"'}
    assert verification.configure(client, MEMBER, True) is True
    assert client.writes == [(MEMBER, {"VerifyCertificate": True})]


@pytest.mark.parametrize("refusal", ["stale", "unproved"])
def test_no_wildcard_retry_or_success_without_readback(refusal):
    client = Controller({FIRST: False})
    client.stale = refusal == "stale"
    client.apply_write = False
    with pytest.raises(verification.Refused):
        verification.configure(client, MEMBER, True)
    assert len(client.writes) == 1


def test_security_service_discovery_has_a_fixed_bound():
    client = Controller({"/redfish/v1/Managers/%d/SecurityService" % index: False for index in range(5)})
    with pytest.raises(verification.Refused):
        verification.configure(client, MEMBER, True)
    assert client.writes == []


def test_already_disabled_verification_needs_no_etag_or_configuration_privilege():
    client = Controller({FIRST: False})
    client.denied = FIRST
    del client.resources[FIRST]["@odata.etag"]
    assert verification.configure(client, MEMBER, False) is False
    assert client.writes == []


def test_explicit_disabling_uses_a_conditional_write_and_never_imports_trust():
    client = Controller({FIRST: True})
    client.wanted = False
    assert verification.configure(client, MEMBER, False) is True
    assert client.writes == [(FIRST, {PROPERTY: False})]
    assert verification.configure(client, MEMBER, False) is False


def test_disabling_requires_the_etag_when_the_existing_setting_is_true():
    client = Controller({FIRST: True})
    del client.resources[FIRST]["@odata.etag"]
    with pytest.raises(verification.Refused):
        verification.configure(client, MEMBER, False)
    assert client.writes == []
