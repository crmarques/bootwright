"""Discovery diagnostics expose only static classifications, never native text."""

from types import SimpleNamespace
from unittest.mock import Mock
import errno
import socket
import ssl
import urllib.error

import pytest

from ansible_collections.bootwright.core.plugins.modules import disk_discovery as module


@pytest.mark.parametrize("error", [ValueError("SECRET private-url"), KeyError("SECRET"), TypeError("SECRET"), OSError("SECRET"), FileNotFoundError("SECRET")])
def test_arbitrary_exception_and_phase_text_are_never_exposed(error):
    result = module.diagnostic(error, SimpleNamespace(phase="SECRET"))
    assert "SECRET" not in repr(result)
    assert result["discovery_phase"] == "custody"
    assert set(result) == {"discovery_failure", "discovery_phase"}


def test_fixed_refusals_and_native_tools_are_actionable_without_arguments():
    error = ValueError("missing discovery custody has unresolved external ownership")
    assert module.diagnostic(error, SimpleNamespace(phase="controller")) == {
        "discovery_failure": "external-ownership", "discovery_phase": "controller"
    }
    error = module.custody.media.CommandFailure("mkksiso", 1)
    assert module.diagnostic(error, SimpleNamespace(phase="media"))["discovery_failure"] == "native-mkksiso"
    error = module.custody.media.CommandFailure("SECRET-command", "SECRET-exit")
    assert "SECRET" not in repr(module.diagnostic(error, None))
    assert error.code == -1


def test_native_failure_drops_stdout_and_arguments(monkeypatch):
    monkeypatch.setattr(module.custody.media.bounded_process, "run", Mock(return_value=SimpleNamespace(code=7, stdout=b"SECRET")))
    with pytest.raises(module.custody.media.CommandFailure) as caught:
        module.custody.media.native(["/usr/bin/xorriso", "SECRET-url"])
    assert caught.value.tool == "xorriso" and caught.value.code == 7
    assert "SECRET" not in str(caught.value)


def test_publication_refusals_are_distinct_from_media_construction():
    result = module.diagnostic(ValueError("custody-directory"), SimpleNamespace(phase="publication"))
    assert result == {"discovery_failure": "custody-directory", "discovery_phase": "publication"}


@pytest.mark.parametrize("error, expected", [
    (ValueError("discovery publication copy"), "publication-copy"),
    (OSError(errno.ENOSPC, "SECRET"), "publication-space"),
    (OSError(errno.EDQUOT, "SECRET"), "publication-space"),
])
def test_publication_capacity_and_copy_diagnostics_are_safe_and_actionable(error, expected):
    result = module.diagnostic(error, SimpleNamespace(phase="publication"))
    assert result == {"discovery_failure": expected, "discovery_phase": "publication"}
    assert "SECRET" not in repr(result)


@pytest.mark.parametrize("error, expected", [
    (urllib.error.HTTPError("https://SECRET/private-token", 403, "SECRET", {"SECRET": "SECRET"}, None), "media-http-403"),
    (urllib.error.HTTPError("https://SECRET/private-token", 404, "SECRET", {}, None), "media-http-404"),
    (urllib.error.HTTPError("https://SECRET/private-token", 302, "SECRET", {}, None), "media-redirect"),
    (urllib.error.URLError(ssl.SSLCertVerificationError(1, "SECRET")), "media-certificate"),
    (urllib.error.URLError(ssl.SSLError(1, "SECRET")), "media-handshake"),
    (urllib.error.URLError(socket.gaierror(-2, "SECRET")), "media-dns"),
    (urllib.error.URLError(TimeoutError("SECRET")), "media-timeout"),
    (urllib.error.URLError(ConnectionRefusedError("SECRET")), "media-refused"),
    (urllib.error.URLError(OSError("SECRET")), "media-connection"),
])
def test_delivery_diagnostics_classify_causes_without_private_values(error, expected):
    result = module.diagnostic(error, SimpleNamespace(phase="media-delivery"))
    assert result == {"discovery_failure": expected, "discovery_phase": "media-delivery"}
    assert "SECRET" not in repr(result)


@pytest.mark.parametrize("status", [True, None, "SECRET", ["SECRET"], {"SECRET": "SECRET"}, 599])
def test_unrecognized_http_status_never_enters_diagnostics(status):
    error = urllib.error.HTTPError("https://SECRET/private-token", status, "SECRET", {}, None)
    assert module.delivery_code(error) == "media-http"


@pytest.mark.parametrize("cause", [
    "media-verification", "privilege", "trust-chain", "trust-inventory", "trust-metadata",
    "trust-conflict", "trust-unproved", "trust-request", "media-fetch",
])
@pytest.mark.parametrize("phase", ["media-insertion", "boot-selection", "power-on"])
def test_bmc_refusals_publish_only_the_allowlisted_cause_and_step(cause, phase):
    error = module.custody.redfish_control.ControllerError("SECRET private URL")
    error.cause = cause
    result = module.diagnostic(error, SimpleNamespace(phase=phase))
    assert result == {"discovery_failure": "bmc-" + cause, "discovery_phase": phase}
    assert "SECRET" not in repr(result)


@pytest.mark.parametrize("cause", ["SECRET", None, ["SECRET"], {"SECRET": "SECRET"}, True])
def test_untrusted_bmc_cause_metadata_is_never_published(cause):
    error = module.custody.redfish_control.ControllerError("SECRET")
    error.cause = cause
    result = module.diagnostic(error, SimpleNamespace(phase="boot"))
    assert result == {"discovery_failure": "bmc-request", "discovery_phase": "boot"}


def test_disabled_authoritative_bmc_verification_is_actionable_without_a_trust_write():
    client = module.custody.redfish_control.Client.__new__(module.custody.redfish_control.Client)
    client._manager_verification = Mock(return_value=[("https://SECRET/security", False)])
    client.fetch = Mock(side_effect=AssertionError("changed manager security"))
    with pytest.raises(module.custody.redfish_control.ControllerError) as caught:
        client._verified_managers()
    assert module.diagnostic(caught.value, SimpleNamespace(phase="media-insertion")) == {
        "discovery_failure": "bmc-media-verification", "discovery_phase": "media-insertion"
    }
    client.fetch.assert_not_called()
