"""Certificate-import refusals keep static causes and stop before unsafe writes."""

from __future__ import annotations

from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_remote_ca as remote


@pytest.mark.parametrize("inspect, value, cause", [
    (remote.authority, "SECRET invalid chain", "trust-chain"),
    (remote.inventory, {}, "trust-inventory"),
    (remote.inventory, {remote.INVENTORY: [{"CertId": True}]}, "trust-inventory"),
])
def test_unqualified_import_material_has_a_static_cause(inspect, value, cause):
    with pytest.raises(remote.TrustError) as caught:
        inspect(value)
    assert caught.value.cause == cause
    assert "SECRET" not in str(caught.value)


@pytest.mark.parametrize("refusal", ("verification", "metadata"))
def test_unproved_security_service_never_starts_a_trust_write(refusal):
    client = Mock()
    client._read.return_value = ({"HttpsTransferCertVerification": False}, {})
    instance = remote.RemoteCA(client)
    with pytest.raises(remote.TrustError) as caught:
        if refusal == "verification":
            instance.state("https://example.test/security")
        else:
            instance.target({}, remote.IMPORT, {"Type": "text", "Content": "SECRET"})
    assert caught.value.cause == ("media-verification" if refusal == "verification" else "trust-metadata")
    client.fetch.assert_not_called()


@pytest.mark.parametrize("status, body", [
    (401, {}), (403, {}), (400, {"Messages": {"MessageId": "Base.1.0.PropertyModificationNeedPrivilege"}}),
])
def test_certificate_import_preserves_privilege_denial_without_response_text(status, body):
    client = Mock()
    client.fetch.return_value = (status, body, {})
    with pytest.raises(remote.TrustError) as caught:
        remote.RemoteCA(client).write("https://example.test/import", {"Content": "SECRET"})
    assert caught.value.cause == "privilege"
    assert "SECRET" not in str(caught.value)


def test_unclassified_import_refusal_has_a_fixed_fallback():
    client = Mock()
    client.fetch.return_value = (500, {"SECRET": "SECRET"}, {})
    with pytest.raises(remote.TrustError) as caught:
        remote.RemoteCA(client).write("https://example.test/import", {"Content": "SECRET"})
    assert caught.value.cause == "trust-request"
    assert "SECRET" not in str(caught.value)
