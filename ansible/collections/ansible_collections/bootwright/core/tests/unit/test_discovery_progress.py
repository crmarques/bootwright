"""RAM discovery reports real native phases before their blocking calls."""

from pathlib import Path
from contextlib import contextmanager
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller


def boot_fixture(monkeypatch, failed=None):
    instance = controller.Controller.__new__(controller.Controller)
    events = []
    instance.report_progress = lambda group, status: events.append((group, status))
    client = Mock()
    client.inserted.return_value = ""
    instance.client = lambda: client
    instance.target = Mock(return_value="Off")
    instance.publication = lambda: (Path("/fixture"), "https://example.test/private/SECRET.iso")
    instance.publish = Mock()
    instance.booted = Mock(return_value=False)
    instance.mark_boot = Mock()
    instance.prepare_tls = Mock()
    instance.request = {"controller": {"virtualMedia": {
        "trust": "established", "enableVerification": False,
        "restoreVerification": False, "removeCertificate": False,
    }}}
    instance.material = {"artifactCertificate": "/fixture/SECRET.pem"}
    monkeypatch.setattr(controller.records, "read", lambda path: b"SECRET certificate")
    monkeypatch.setattr(controller, "listener_probe", Mock())
    for method, group in (("insert", "insert-media"), ("boot_once", "select-boot"), ("power", "power-on")):
        def effect(*args, group=group, **kwargs):
            assert events[-1] == (group, "running")
            if failed == group:
                raise controller.redfish_control.ControllerError("SECRET failure")
        getattr(client, method).side_effect = effect
    return instance, client, events


@pytest.mark.parametrize("failed", [None, "insert-media", "select-boot", "power-on"])
def test_boot_progress_is_live_and_never_reports_later_work_after_failure(monkeypatch, failed):
    instance, client, events = boot_fixture(monkeypatch, failed)
    if failed:
        with pytest.raises(controller.redfish_control.ControllerError):
            instance.start()
    else:
        instance.start()
    groups = ["verify-delivery", "insert-media", "select-boot", "power-on"]
    wanted = []
    for group in groups:
        wanted += [(group, "running"), (group, "failed" if group == failed else "ok")]
        if group == failed:
            break
    assert events == wanted
    assert "SECRET" not in repr(events)
    if failed in ("insert-media", "select-boot"):
        client.power.assert_not_called()


def test_resuming_owned_running_boot_reports_skips_without_rebooting(monkeypatch):
    instance, client, events = boot_fixture(monkeypatch)
    instance.target.return_value = "On"
    instance.booted.return_value = True
    client.inserted.return_value = instance.publication()[1]
    instance.start()
    assert events == [(group, "skipped") for group in
                      ("build-media", "publish-media", "verify-delivery", "insert-media", "select-boot", "power-on")]
    instance.publish.assert_not_called()
    client.insert.assert_not_called()
    client.power.assert_not_called()


def test_media_build_and_publication_report_before_blocking_work(tmp_path, monkeypatch):
    instance = controller.Controller.__new__(controller.Controller)
    events = []
    instance.report_progress = lambda group, status: events.append((group, status))
    instance.root = tmp_path
    instance.load = Mock()
    instance.publication = lambda: (tmp_path / "private", "SECRET")
    (tmp_path / "private").mkdir()
    instance.publication_directories = Mock()
    instance.request = {"bootMedia": {"size": 64 << 20}}
    instance.payload = lambda: {"SECRET": b"PRIVATE"}
    instance.lock = 0

    @contextmanager
    def construction(*args):
        assert events[-1] == ("build-media", "running")
        yield

    def build(*args):
        assert events[-1] == ("build-media", "running")
        return tmp_path / "image.iso"

    def publish(*args):
        assert events[-2:] == [("build-media", "ok"), ("publish-media", "running")]

    monkeypatch.setattr(controller.media, "construction", construction)
    monkeypatch.setattr(controller.media, "build", build)
    monkeypatch.setattr(controller.media, "native", Mock())
    monkeypatch.setattr(controller, "publish_image", publish)
    monkeypatch.setattr(controller.os, "fsync", Mock())
    instance.publish()
    assert events == [("build-media", "running"), ("build-media", "ok"), ("publish-media", "running"), ("publish-media", "ok")]


def apply_fixture(tmp_path, inventory, bmc_report):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = tmp_path
    events = []
    instance.report_progress = lambda group, status: events.append((group, status))

    def prepare():
        assert events[-1] == ("inspect-target", "running")

    instance.prepare = prepare
    instance.evidence = Mock(return_value={"cleaned": False})
    instance.request = {"inventory": inventory}
    instance.report = lambda: None
    instance.booted = Mock(return_value=False)

    def read_bmc():
        assert events[-1] == ("read-bmc-inventory", "running")
        return bmc_report

    def finish(complete):
        assert complete and events[-1] == ("seal-cleanup", "running")
        return {"completed": True, "cleaned": True}

    instance.read_bmc = read_bmc
    instance.start = Mock()
    instance.receive = Mock()
    instance.cleanup = Mock()
    instance.finish = finish
    return instance, events


@pytest.mark.parametrize("inventory,bmc_report", [("automatic", None), ("boot", b"never read")])
def test_apply_boot_path_reports_each_phase(tmp_path, inventory, bmc_report):
    instance, events = apply_fixture(tmp_path, inventory, bmc_report)
    assert instance.apply() == {"completed": True, "cleaned": True}
    bmc = [("read-bmc-inventory", "running"), ("read-bmc-inventory", "ok")] if inventory == "automatic" else [("read-bmc-inventory", "skipped")]
    assert events == [("inspect-target", "running"), ("inspect-target", "ok"), *bmc, ("seal-cleanup", "running"), ("seal-cleanup", "ok")]
    instance.start.assert_called_once_with()
    instance.receive.assert_called_once_with()
    instance.cleanup.assert_called_once_with()


def test_apply_bmc_inventory_skips_every_boot_phase(tmp_path):
    instance, events = apply_fixture(tmp_path, "automatic", b"qualified BMC report")
    assert instance.apply() == {"completed": True, "cleaned": True}
    skipped = [(group, "skipped") for group in controller.BOOT_GROUPS + ("await-answer", "receive-inventory", "power-off", "eject-media")]
    assert events == [("inspect-target", "running"), ("inspect-target", "ok"), ("read-bmc-inventory", "running"),
                      ("read-bmc-inventory", "ok"), *skipped, ("seal-cleanup", "running"), ("seal-cleanup", "ok")]
    instance.start.assert_not_called()
    instance.receive.assert_not_called()
    instance.cleanup.assert_not_called()


def test_a_started_boot_never_falls_back_to_the_bmc_inventory(tmp_path):
    instance, events = apply_fixture(tmp_path, "automatic", b"inventory that appeared after the boot started")
    instance.booted.return_value = True
    instance.read_bmc = Mock(side_effect=AssertionError("read BMC drives while our boot may be running"))
    assert instance.apply()["completed"]
    assert ("read-bmc-inventory", "skipped") in events
    instance.start.assert_called_once_with()
    instance.cleanup.assert_called_once_with()


def test_cleanup_brackets_verified_poweroff_and_ejection(tmp_path, monkeypatch):
    instance, client, events = boot_fixture(monkeypatch)
    instance.publication = lambda: (tmp_path / "private", "https://example.test/owned.iso")

    def eject(*args):
        assert events[-1] == ("eject-media", "running")

    client.eject.side_effect = eject
    monkeypatch.setattr(controller.records, "directory", lambda *args: None)
    instance.cleanup()
    assert events == [("power-off", "running"), ("power-off", "ok"), ("eject-media", "running"), ("eject-media", "ok")]
    client.power.assert_not_called()
