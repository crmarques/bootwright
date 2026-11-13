"""Inventory boot/receipt boundaries never authorize data disk writes."""

import hashlib
import io
import os
import signal
import time
from types import SimpleNamespace
from pathlib import Path
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_worker as worker


def unpack(data):
    position, files = 0, {}
    while position < len(data):
        assert data[position : position + 6] == b"070701"
        fields = [int(data[position + 6 + index * 8 : position + 14 + index * 8], 16) for index in range(13)]
        position += 110
        name = data[position : position + fields[11] - 1].decode("ascii")
        position += fields[11]
        position += -position % 4
        content = data[position : position + fields[6]]
        position += fields[6]
        position += -position % 4
        if name == "TRAILER!!!":
            return files
        files[name] = content
    raise AssertionError("missing archive terminator")


def test_custom_init_calls_the_worker_in_the_actual_private_payload(monkeypatch):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = Path("/fixture")
    instance.binding = {"nonce": "a" * 64}
    instance.request = {"interfaces": [], "network": {}}
    instance.load = lambda: {"reportToken": "b" * 64}
    monkeypatch.setattr(controller.records, "read", lambda path: b"synthetic TLS material")
    payload = instance.payload()
    archive = unpack(boot.overlay(payload))
    init = archive["init"].decode()
    assert "python3 -I /run/discovery/disk_discovery_worker.py" in init
    assert archive["bootwright-discovery/disk_discovery_worker.py"] == payload["disk_discovery_worker.py"]
    assert "systemctl" not in init and "udevadm" not in init and "anaconda" not in init.lower()
    assert "init=/bootwright-discovery/no-fallback" in boot.ARGUMENTS
    assert "raid=noautodetect" in boot.ARGUMENTS
    mounts = [line for line in init.splitlines() if line.startswith("mount ")]
    assert len(mounts) == 4
    assert all(" -t " + kind in line for line, kind in zip(mounts, ("proc", "sysfs", "devtmpfs", "tmpfs")))
    assert "/proc/sys/kernel/modprobe" in init
    assert "/sys/bus/scsi/devices/*/modalias" in init


def test_overlay_is_deterministic_bounded_and_has_no_traversal_or_links():
    first = boot.overlay({"request.json": b"{}"})
    assert first == boot.overlay({"request.json": b"{}"})
    for name in ("../escape", "/absolute", "a/../escape", "a//b"):
        with pytest.raises(ValueError):
            boot.newc([(name, b"", 0o600)])
    with pytest.raises(ValueError):
        boot.newc([("huge", b"x" * boot.MAX_OVERLAY, 0o600)])


def test_receipt_is_durable_before_ack_and_completed_boot_never_restarts():
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = Mock()
    instance.root.__truediv__ = Mock(return_value=Mock(exists=Mock(return_value=True)))
    instance.evidence = Mock(return_value={"completed": True, "cleaned": True})
    instance.prepare = Mock(side_effect=AssertionError("recreated completed keys"))
    instance.reap = Mock()
    instance.finish_local = Mock(return_value={"completed": True, "cleaned": True})
    assert instance.apply() == {"completed": True, "cleaned": True}
    assert instance.destroy() == {"completed": True, "cleaned": True}


def test_controller_refuses_to_boot_or_stop_an_unowned_running_machine():
    instance = controller.Controller.__new__(controller.Controller)
    client = Mock()
    client.inserted.return_value = "https://example.test/another.iso"
    instance.client = lambda: client
    instance.target = lambda unused: "On"
    instance.publication = lambda: (Path("/fixture"), "https://example.test/owned.iso")
    instance.publish = Mock(side_effect=AssertionError("rebuilt while running"))
    with pytest.raises(ValueError, match="already running"):
        instance.start()
    with pytest.raises(ValueError, match="lost ownership"):
        instance.cleanup(False)
    client.power.assert_not_called()
    client.insert.assert_not_called()


def test_foreign_or_noncanonical_report_never_reaches_a_durable_receipt():
    instance = controller.Controller.__new__(controller.Controller)
    instance.binding = {"nonce": "a" * 64}
    value = {"version": "machine-disk-inventory-v1", "binding": instance.binding, "disks": [{}]}
    wire = controller.canonical_json.canonical(value)
    assert instance.validate_report(wire) == value
    for data in (wire + b"\n", wire.replace(b"a" * 64, b"b" * 64), b"x" * (controller.MAX_REPORT + 1)):
        with pytest.raises(ValueError):
            instance.validate_report(data)


def test_networking_is_fixed_argv_and_binds_only_the_proved_mac():
    network = {"address": "192.0.2.10", "prefix": 24, "interface": "eno1", "type": "ethernet", "gateway": "192.0.2.1"}
    interfaces = [{"name": "eno1", "macAddress": "02:00:00:00:00:01"}]
    commands = worker.network_commands(network, interfaces, {"02:00:00:00:00:01": "eth0"})
    assert all(argv[0] == "/usr/sbin/ip" for argv in commands)
    assert ["/usr/sbin/ip", "link", "set", "dev", "eth0", "name", "bwdisco0"] in commands
    for bad in ("eno1;touch", "-bad", "a" * 16):
        with pytest.raises(ValueError):
            worker.network_commands(dict(network, interface=bad), interfaces, {"02:00:00:00:00:01": "eth0"})


def handler(report, token="a" * 64, data=b"", path="/ack", authorization=None):
    result = worker.Handler.__new__(worker.Handler)
    result.path = path
    result.server = SimpleNamespace(report=report, token=token, acknowledged=False)
    result.headers = {"Content-Length": str(len(data)), "Authorization": authorization or "Bearer " + token}
    result.rfile = io.BytesIO(data)
    result.answer = Mock()
    return result


def test_acknowledgement_requires_authentication_and_exact_report_digest():
    report = b'{"fixture":true}'
    digest = hashlib.sha256(report).hexdigest().encode()
    for payload, authorization in ((b"bad", None), (b"f" * 64, None), (digest, "Bearer wrong")):
        request = handler(report, data=payload, authorization=authorization)
        request.do_POST()
        assert not request.server.acknowledged
        request.answer.assert_called_once_with(403)
    request = handler(report, data=digest)
    request.do_POST()
    assert request.server.acknowledged
    request.answer.assert_called_once_with(200)
    request = handler(report, path="/report", authorization="Bearer wrong")
    request.do_GET()
    request.answer.assert_called_once_with(403)


def fixture_custody(tmp_path, monkeypatch):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = tmp_path / "custody"
    instance.root.mkdir()
    instance.binding = {"nonce": "a" * 64}
    instance.digest = "b" * 64
    instance.request = {"private": {"path": str(tmp_path / "served")}}
    instance.client = Mock(return_value=Mock(inserted=Mock(return_value="")))
    instance.target = Mock(return_value="Off")
    monkeypatch.setattr(controller.records, "read", lambda path, limit=32768: path.read_bytes())
    monkeypatch.setattr(controller.records, "directory", lambda *args: None)

    def write(path, data):
        with path.open("xb") as stream:
            stream.write(data)

    monkeypatch.setattr(controller.records, "write", write)
    return instance


def test_missing_custody_cleanup_proves_no_external_effect(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    proof = instance.destroy()
    assert proof == {"version": controller.VERSION, "request": instance.digest, "completed": False, "cleaned": True}
    instance.client.return_value.power.assert_not_called()
    instance.client.return_value.eject.assert_not_called()


@pytest.mark.parametrize("boundary", ("running", "media", "publication"))
def test_lost_custody_never_mints_new_authority(tmp_path, monkeypatch, boundary):
    instance = fixture_custody(tmp_path, monkeypatch)
    if boundary == "running":
        instance.target.return_value = "On"
    elif boundary == "media":
        instance.client.return_value.inserted.return_value = "https://example.test/unknown.iso"
    else:
        parent = Path(instance.request["private"]["path"])
        parent.mkdir()
        (parent / "unknown").mkdir()
    for command in (instance.prepare, instance.destroy):
        with pytest.raises(ValueError, match="unresolved"):
            command()
    assert not (instance.root / "custody.json").exists()
    instance.client.return_value.power.assert_not_called()


@pytest.mark.parametrize("missing", (None, "server.pem", "server-key.pem", "both"))
def test_completed_cleanup_resumes_after_each_tls_deletion(tmp_path, monkeypatch, missing):
    instance = fixture_custody(tmp_path, monkeypatch)
    instance.remove_unprepared()
    tls = instance.root / "tls"
    tls.mkdir()
    for name in ("server.pem", "server-key.pem"):
        if missing not in (name, "both"):
            (tls / name).write_bytes(b"private fixture")
    instance.client = Mock(side_effect=AssertionError("revisited installed machine"))
    assert instance.destroy()["cleaned"]
    assert not tls.exists()


def test_receipt_is_committed_before_acknowledgement(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    wire = controller.canonical_json.canonical({"version": "machine-disk-inventory-v1", "binding": instance.binding, "disks": [{}]})
    instance.request["deadlineSeconds"] = 1800
    instance.session = Mock(return_value=wire)
    assert instance.receive() == wire
    assert (instance.root / "report.json").read_bytes() == wire
    instance.session.assert_called_once_with("receipt")
    instance.session = Mock(side_effect=AssertionError("refetched immutable receipt"))
    assert instance.receive() == wire


def test_total_deadline_stops_continuous_activity_and_restores_handler():
    previous = signal.getsignal(signal.SIGALRM)
    with pytest.raises(TimeoutError):
        with absolute_deadline.limited(0.02):
            while True:
                time.monotonic()
    assert signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0)
    assert signal.getsignal(signal.SIGALRM) == previous
    with absolute_deadline.limited(1):
        with pytest.raises(ValueError, match="already holds"):
            with absolute_deadline.limited(1):
                pass


def test_removed_discovery_never_recreates_private_keys(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    instance.destroy()
    instance.prepare = Mock(side_effect=AssertionError("recreated removed transport keys"))
    with pytest.raises(ValueError, match="removed"):
        instance.apply()
    assert not (instance.root / "tls").exists()


def test_interrupted_tls_stages_are_reaped_without_touching_other_records(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    stage = instance.root / (".tls-stage-" + "a" * 32)
    stage.mkdir()
    (stage / "server-key.pem").write_bytes(b"private fixture")
    (instance.root / "report.json").write_bytes(b"immutable fixture")
    instance.reap_tls_stages()
    assert not stage.exists()
    assert (instance.root / "report.json").read_bytes() == b"immutable fixture"


def test_observation_cannot_complete_before_local_cleanup_is_sealed(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    instance.remove_unprepared()
    (instance.root / "sealed.json").unlink()
    tls = instance.root / "tls"
    tls.mkdir()
    (tls / "server-key.pem").write_bytes(b"interrupted private fixture")
    assert instance.evidence() == {"version": controller.VERSION, "request": instance.digest, "completed": False, "cleaned": False}
    assert (tls / "server-key.pem").exists()
    instance.client = Mock(side_effect=AssertionError("revisited hardware after external cleanup"))
    assert instance.destroy()["cleaned"]
    assert not tls.exists()
    instance.reap = Mock(side_effect=AssertionError("observation performed local cleanup"))
    assert instance.evidence()["cleaned"]


def test_publication_deletion_is_durable_before_completion(tmp_path, monkeypatch):
    instance = fixture_custody(tmp_path, monkeypatch)
    state = {"binding": instance.binding, "token": "a" * 64, "reportToken": "b" * 64}
    (instance.root / "custody.json").write_bytes(controller.canonical_json.canonical(state))
    parent = Path(instance.request["private"]["path"])
    directory = parent / state["token"]
    directory.mkdir(parents=True)
    (directory / "discovery.iso").write_bytes(b"private image fixture")
    certificate = tmp_path / "certificate.pem"
    certificate.write_bytes(b"certificate fixture")
    instance.material = {"artifactCertificate": str(certificate)}
    instance.request["controller"] = {"virtualMedia": {"restoreVerification": False, "removeCertificate": False}}
    instance.publication = lambda: (directory, "https://example.test/private/discovery.iso")
    events = []
    fsync = controller.os.fsync

    def synced(fd):
        events.append(("fsync", os.readlink("/proc/self/fd/" + str(fd))))
        fsync(fd)

    write = controller.records.write

    def written(path, data):
        events.append(("write", path.name))
        write(path, data)

    monkeypatch.setattr(controller.os, "fsync", synced)
    monkeypatch.setattr(controller.records, "write", written)
    instance.cleanup(True)
    instance.finish(False)
    assert not directory.exists()
    assert events.index(("fsync", str(parent))) < events.index(("write", "completed.json"))


def test_publication_accepts_the_qualified_initramfs_growth(tmp_path, monkeypatch):
    instance = controller.Controller.__new__(controller.Controller)
    instance.root = tmp_path / "custody"
    instance.root.mkdir()
    image = tmp_path / "remastered.iso"
    image.write_bytes(b"synthetic private ISO")
    destination = tmp_path / "served" / "private" / "os" / "node" / "attempt"
    destination.mkdir(parents=True)
    instance.request = {"bootMedia": {"size": 64 << 20}}
    instance.load = lambda: {"token": "a" * 64}
    instance.payload = lambda: {}
    instance.publication = lambda: (destination, "https://example.test/private/discovery.iso")
    monkeypatch.setattr(controller.media, "build", lambda *args: image)
    monkeypatch.setattr(controller.media, "native", Mock(return_value=b""))
    monkeypatch.setattr(controller.records, "directory", lambda *args: None)
    original_fstat = os.fstat
    inode = image.stat().st_ino

    def remastered_size(fd):
        observed = original_fstat(fd)
        if observed.st_ino == inode:
            fields = list(observed)
            fields[6] = instance.request["bootMedia"]["size"] + controller.media.MAX_INITRD // 2
            return os.stat_result(fields)
        return observed

    monkeypatch.setattr(controller.os, "fstat", remastered_size)
    instance.lock = os.open(instance.root, os.O_RDONLY | os.O_DIRECTORY)
    try:
        instance.publish()
        assert (destination / "discovery.iso").read_bytes() == b"synthetic private ISO"
    finally:
        os.close(instance.lock)


def test_publication_still_refuses_images_beyond_the_component_bound(tmp_path):
    source, target = tmp_path / "image.iso", tmp_path / "published.iso"
    source.write_bytes(b"oversized synthetic media")
    with pytest.raises(ValueError, match="publication size"):
        controller.publish_image(source, target, source.stat().st_size - 1)
    assert not target.exists() and not list(tmp_path.glob(".stage-*"))


@pytest.mark.parametrize("source_size", [True, False, 0, -1, "1024", controller.media.MAX_SOURCE + 1])
def test_publication_derivation_refuses_invalid_source_sizes(source_size):
    with pytest.raises(ValueError, match="source size bound"):
        controller.media.publication_bound(source_size)


def test_publication_bound_comes_from_the_qualified_components():
    source_size = 64 << 20
    expected = source_size + len(controller.media.SUPPORTED_INITRDS) * (controller.media.MAX_INITRD + boot.MAX_OVERLAY)
    assert controller.media.publication_bound(source_size) == expected + controller.media.MAX_BOOT_GROWTH


def test_exact_publication_limit_is_accepted_but_the_next_byte_is_refused(tmp_path):
    source, target = tmp_path / "image.iso", tmp_path / "published.iso"
    source.write_bytes(b"synthetic media at its bound")
    controller.publish_image(source, target, source.stat().st_size)
    assert target.read_bytes() == source.read_bytes()
    target.unlink()
    with pytest.raises(ValueError, match="publication size"):
        controller.publish_image(source, target, source.stat().st_size - 1)
    assert not target.exists()


def test_publication_refuses_nonregular_and_symlink_sources(tmp_path):
    target = tmp_path / "published.iso"
    with pytest.raises(ValueError, match="publication file"):
        controller.publish_image(tmp_path, target, 1 << 20)
    link = tmp_path / "image.iso"
    link.symlink_to(tmp_path)
    with pytest.raises(OSError):
        controller.publish_image(link, target, 1 << 20)
    assert not target.exists() and not list(tmp_path.glob(".stage-*"))
