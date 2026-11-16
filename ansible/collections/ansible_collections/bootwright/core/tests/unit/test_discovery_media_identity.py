"""Discovery keeps the shared Redfish image identity through boot and cleanup."""

from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller

EXPECTED = "https://artifacts.example.test:8443/private/attempt-a/discovery.iso?revision=1"
ECHO = "https://artifacts.example.test/private/attempt-a/discovery.iso?revision=1"
BOUNDARIES = ("observe-on", "observe-off", "running", "resume", "start", "cleanup-on", "cleanup-off")


def fixture(tmp_path, monkeypatch, boundary, observed):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = tmp_path
    monkeypatch.setattr(controller.ssh, "validate", Mock())
    monkeypatch.setattr(controller.ssh, "policy", Mock())
    client = Mock()
    client.inserted.return_value = observed
    instance.client = Mock(return_value=client)
    power = "Off" if boundary in ("observe-off", "start", "cleanup-off") else "On"
    instance.target = Mock(return_value=power)
    if boundary == "cleanup-on":
        instance.target.side_effect = ["On", "Off"]
    instance.publication = Mock(return_value=(tmp_path / "private" / "token", EXPECTED))
    instance.booted = Mock(return_value=True)
    instance.mark_boot = Mock()
    instance.prepare_ssh = Mock()
    instance.publish = Mock()
    instance.material = {"artifactCertificate": str(tmp_path / "certificate.pem")}
    instance.request = {"controller": {"virtualMedia": {
        "trust": "importCertificate", "enableVerification": False, "restoreVerification": False, "removeCertificate": False,
    }}}
    monkeypatch.setattr(controller.records, "read", lambda path: b"synthetic certificate")
    monkeypatch.setattr(controller, "listener_probe", Mock())
    monkeypatch.setattr(controller.time, "sleep", Mock())
    return instance, client


def invoke(instance, boundary):
    if boundary.startswith("observe-"):
        instance.prove_owned_boot()
    elif boundary == "running":
        instance.owned_running(EXPECTED)
    elif boundary in ("start", "resume"):
        instance.start()
    else:
        instance.cleanup()


@pytest.mark.parametrize("boundary", BOUNDARIES)
@pytest.mark.parametrize("observed", [EXPECTED, ECHO, ECHO.replace("artifacts", "ARTIFACTS")])
def test_every_ownership_boundary_accepts_the_same_normalized_echo(tmp_path, monkeypatch, boundary, observed):
    instance, client = fixture(tmp_path, monkeypatch, boundary, observed)
    invoke(instance, boundary)
    if boundary == "cleanup-on":
        client.power.assert_called_once_with("ForceOff", "Off")
    elif boundary == "start":
        client.power.assert_called_once_with("On", "On")
        client.insert.assert_called_once_with(EXPECTED, "importCertificate", "synthetic certificate", private=True, enable_verification=False)
    else:
        client.power.assert_not_called()
    if boundary.startswith("cleanup-"):
        client.eject.assert_called_once()
    else:
        client.eject.assert_not_called()
    if boundary in ("observe-on", "observe-off", "running", "resume"):
        instance.publish.assert_not_called()
        client.insert.assert_not_called()


@pytest.mark.parametrize("boundary", BOUNDARIES)
@pytest.mark.parametrize("observed", [
    ECHO.replace("https:", "http:"), ECHO.replace("artifacts.example.test", "other.example.test"),
    ECHO.replace("attempt-a", "attempt-b"), ECHO.replace("revision=1", "revision=2"),
    EXPECTED.replace(":8443", ":9443"), None,
])
def test_foreign_or_unproved_media_refuses_before_any_bmc_mutation(tmp_path, monkeypatch, boundary, observed):
    instance, client = fixture(tmp_path, monkeypatch, boundary, observed)
    with pytest.raises(ValueError):
        invoke(instance, boundary)
    client.power.assert_not_called()
    client.insert.assert_not_called()
    client.boot_once.assert_not_called()
    client.eject.assert_not_called()


@pytest.mark.parametrize("boundary", BOUNDARIES)
def test_empty_media_is_allowed_only_while_off(tmp_path, monkeypatch, boundary):
    instance, client = fixture(tmp_path, monkeypatch, boundary, "")
    if boundary in ("observe-off", "start", "cleanup-off"):
        invoke(instance, boundary)
    else:
        with pytest.raises(ValueError):
            invoke(instance, boundary)
        client.power.assert_not_called()
        client.eject.assert_not_called()
