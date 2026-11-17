"""The three probes the Ceph host runtime runs inside the frozen image are modules with their own tests."""

from __future__ import annotations

import base64
import contextlib
import hashlib
import inspect
import io
import json
import os
from pathlib import Path
import sys
import types

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_host_device_probe as device, ceph_host_image_probe as image, ceph_host_label_probe as label, ceph_host_runtime as runtime,
)

PROBES = (image, label, device)
SENTINEL = b"bootwright-contained-public-label-probe-v1\n"
POLICY = "/etc/crypto-policies/back-ends/openssh.config"


class Rooted:
    """A stat result owned by root, with every other field taken from the real one."""

    def __init__(self, real, **changes):
        for name in dir(real):
            if name.startswith("st_"):
                setattr(self, name, getattr(real, name))
        self.st_uid = 0
        for name, value in changes.items():
            setattr(self, name, value)


def rooted_os(**overrides):
    """The os module as a probe sees it, with files reading as root-owned."""
    proxy = types.SimpleNamespace(**vars(os))
    proxy.fstat = lambda fd: Rooted(os.fstat(fd))
    proxy.stat = lambda path, **options: Rooted(os.stat(path, **options))
    for name, value in overrides.items():
        setattr(proxy, name, value)
    return proxy


def run_main(module):
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        module.main()
    return out.getvalue()


@pytest.mark.parametrize("module", PROBES, ids=lambda module: module.__name__.rsplit(".", 1)[-1])
def test_a_probe_is_the_source_the_runtime_sends_and_does_nothing_when_imported(module, monkeypatch):
    source = Path(module.__file__).read_text()
    assert runtime.module_source(module) == source
    compile(source, module.__file__, "exec")
    monkeypatch.delitem(sys.modules, "asyncssh", raising=False)
    monkeypatch.delitem(sys.modules, "ceph_volume", raising=False)
    out = io.StringIO()
    with contextlib.redirect_stdout(out):
        exec(compile(source, module.__file__, "exec"), {"__name__": "bundled_probe"})
    assert out.getvalue() == "" and "asyncssh" not in sys.modules and "ceph_volume" not in sys.modules


def test_a_probe_run_as_the_container_script_calls_its_main(monkeypatch, tmp_path):
    source = Path(image.__file__).read_text()
    monkeypatch.setitem(sys.modules, "asyncssh", None)
    out = io.StringIO()
    with contextlib.redirect_stdout(out), pytest.raises(SystemExit) as stopped:
        exec(compile(source, image.__file__, "exec"), {"__name__": "__main__"})
    assert stopped.value.code == 1 and out.getvalue() == ""


@pytest.mark.skipif(os.getuid() == 0, reason="a root-owned fixture would be accepted")
def test_an_artifact_not_owned_by_root_is_refused(tmp_path):
    path = tmp_path / "artifact"
    path.write_bytes(b"x")
    with pytest.raises(ValueError, match="^artifact$"):
        image.file(str(path))


@pytest.mark.parametrize("mode,content", [(0o620, b"x"), (0o602, b"x"), (0o644, b"")])
def test_an_artifact_writable_by_others_or_empty_is_refused(tmp_path, monkeypatch, mode, content):
    path = tmp_path / "artifact"
    path.write_bytes(content)
    path.chmod(mode)
    monkeypatch.setattr(image, "os", rooted_os())
    with pytest.raises(ValueError, match="^artifact$"):
        image.file(str(path))


def test_an_artifact_that_is_a_symlink_or_has_another_link_is_refused(tmp_path, monkeypatch):
    target = tmp_path / "target"
    target.write_bytes(b"x")
    target.chmod(0o644)
    link = tmp_path / "link"
    link.symlink_to(target)
    monkeypatch.setattr(image, "os", rooted_os())
    with pytest.raises(OSError):
        image.file(str(link))
    os.link(target, tmp_path / "second")
    with pytest.raises(ValueError, match="^artifact$"):
        image.file(str(target))


def test_an_artifact_reports_its_digest_size_mode_and_owner_and_carries_a_payload_only_on_request(tmp_path, monkeypatch):
    path = tmp_path / "artifact"
    path.write_bytes(b"cephadm payload")
    path.chmod(0o755)
    monkeypatch.setattr(image, "os", rooted_os())
    result, body = image.file(str(path), payload=True)
    assert body == base64.b64encode(b"cephadm payload").decode()
    assert result == {"sha256": hashlib.sha256(b"cephadm payload").hexdigest(), "bytes": 15, "mode": 0o755, "uid": 0, "gid": os.getgid()}
    assert image.file(str(path))[1] is None


def test_a_payload_above_two_mebibytes_is_refused_but_the_same_artifact_is_hashed_without_one(tmp_path, monkeypatch):
    path = tmp_path / "artifact"
    path.write_bytes(b"x" * ((2 << 20) + 1))
    path.chmod(0o644)
    monkeypatch.setattr(image, "os", rooted_os())
    with pytest.raises(ValueError, match="^artifact$"):
        image.file(str(path), payload=True)
    assert image.file(str(path))[0]["bytes"] == (2 << 20) + 1


def test_an_artifact_that_changes_while_it_is_read_is_refused(tmp_path, monkeypatch):
    path = tmp_path / "artifact"
    path.write_bytes(b"x" * 10)
    path.chmod(0o644)
    calls = []

    def fstat(fd):
        calls.append(fd)
        return Rooted(os.fstat(fd), st_mtime_ns=len(calls))

    monkeypatch.setattr(image, "os", rooted_os(fstat=fstat))
    with pytest.raises(ValueError, match="^artifact$"):
        image.file(str(path))


def collection_fixture(monkeypatch, tmp_path, *, proxy=("/bin/false",), account=(167, 167), policy_path=POLICY):
    policy = tmp_path / "policy"
    policy.write_text("Ciphers aes256-gcm@openssh.com\n")
    policy.chmod(0o644)
    artifacts = {}

    def file(path, payload=False):
        artifacts[path] = {"sha256": "a" * 64, "bytes": 1, "mode": 0o755, "uid": 0, "gid": 0}
        return artifacts[path], "UEFZTE9BRA==" if payload else None

    parser = types.ModuleType("asyncssh")
    parser.SSHClientConnectionOptions = lambda **options: types.SimpleNamespace(proxy_command=list(proxy), options=options)
    monkeypatch.setitem(sys.modules, "asyncssh", parser)
    monkeypatch.setattr(image, "file", file)
    monkeypatch.setattr(image.pwd, "getpwnam", lambda name: types.SimpleNamespace(pw_uid=account[0], pw_gid=account[1]))
    real_open = os.open
    monkeypatch.setattr(image, "os", rooted_os(
        open=lambda path, *args, **options: real_open(policy if path == POLICY else path, *args, **options),
        path=types.SimpleNamespace(realpath=lambda path: policy_path)))
    return artifacts


def test_the_image_profile_names_the_artifacts_the_account_the_parser_and_the_crypto_policy(monkeypatch, tmp_path):
    artifacts = collection_fixture(monkeypatch, tmp_path)
    value = image.collect()
    assert set(artifacts) == set(runtime.ARTIFACTS)
    assert value["version"] == "ceph-host-image-v1" and value["cephUID"] == 167 and value["cephGID"] == 167
    assert value["cephadm"] == "UEFZTE9BRA==" and value["artifacts"] == artifacts
    assert value["cryptoPolicy"] == "Ciphers aes256-gcm@openssh.com\n"


@pytest.mark.parametrize("account", [(0, 0), (167, 168), (65536, 65536)])
def test_an_account_that_is_not_one_unprivileged_user_and_group_is_refused(monkeypatch, tmp_path, account):
    collection_fixture(monkeypatch, tmp_path, account=account)
    with pytest.raises(ValueError, match="^account$"):
        image.collect()


def test_a_parser_that_ignores_the_proxy_command_is_refused(monkeypatch, tmp_path):
    collection_fixture(monkeypatch, tmp_path, proxy=())
    with pytest.raises(ValueError, match="^parser$"):
        image.collect()


def test_a_crypto_policy_outside_the_policy_directories_is_refused(monkeypatch, tmp_path):
    collection_fixture(monkeypatch, tmp_path, policy_path="/tmp/openssh.config")
    with pytest.raises(ValueError, match="^policy$"):
        image.collect()


def test_main_prints_the_canonical_profile_and_hides_any_failure(monkeypatch, tmp_path):
    collection_fixture(monkeypatch, tmp_path)
    value = image.collect()
    assert run_main(image) == json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n"
    collection_fixture(monkeypatch, tmp_path, account=(0, 0))
    out = io.StringIO()
    with contextlib.redirect_stdout(out), pytest.raises(SystemExit) as stopped:
        image.main()
    assert stopped.value.code == 1 and out.getvalue() == ""


def label_fixture(monkeypatch, tmp_path, *, content=SENTINEL, mode=0o644, readonly=True, links=1):
    proof = tmp_path / "proof"
    proof.write_bytes(content)
    proof.chmod(mode)
    monkeypatch.setattr(label, "PATH", str(proof))
    closed = []
    real_fstat = os.fstat
    monkeypatch.setattr(label, "os", rooted_os(
        fstat=lambda fd: Rooted(real_fstat(fd), st_nlink=links),
        fstatvfs=lambda fd: types.SimpleNamespace(f_flag=os.ST_RDONLY if readonly else 0),
        close=lambda fd: closed.append(fd) or os.close(fd)))
    return closed


def test_the_label_probe_reports_a_readable_read_only_root_file_and_closes_it(monkeypatch, tmp_path):
    closed = label_fixture(monkeypatch, tmp_path)
    assert json.loads(run_main(label)) == {"read": True, "readOnly": True, "uid": os.getuid(), "gid": os.getgid()}
    assert len(closed) == 1


@pytest.mark.parametrize("damage,reason", [({"mode": 0o664}, "file"), ({"mode": 0o646}, "file"), ({"links": 2}, "file"),
                                           ({"content": SENTINEL + b"x"}, "bytes"), ({"content": b"other\n"}, "bytes"),
                                           ({"readonly": False}, "mount")])
def test_the_label_probe_refuses_each_unproven_file_and_still_closes_it(monkeypatch, tmp_path, damage, reason):
    closed = label_fixture(monkeypatch, tmp_path, **damage)
    with pytest.raises(ValueError, match="^" + reason + "$"):
        label.main()
    assert len(closed) == 1


@pytest.mark.skipif(os.getuid() == 0, reason="a root-owned fixture would be accepted")
def test_the_label_probe_refuses_a_file_not_owned_by_root(monkeypatch, tmp_path):
    proof = tmp_path / "proof"
    proof.write_bytes(SENTINEL)
    monkeypatch.setattr(label, "PATH", str(proof))
    with pytest.raises(ValueError, match="^file$"):
        label.main()


def test_the_label_probe_does_not_follow_a_symlink(monkeypatch, tmp_path):
    target = tmp_path / "target"
    target.write_bytes(SENTINEL)
    link = tmp_path / "proof"
    link.symlink_to(target)
    monkeypatch.setattr(label, "PATH", str(link))
    with pytest.raises(OSError):
        label.main()


def fake_ceph_volume(monkeypatch, identity="ID_SERIAL-value", asked=None):
    class UdevData:
        def __init__(self, path):
            raise AssertionError("the original udev reader ran")

    class Device:
        def _get_device_id(self):
            data = disk.UdevData(self.path)
            if asked is not None:
                asked.append((self.path, dict(data.environment)))
            return identity

    package = types.ModuleType("ceph_volume")
    util = types.ModuleType("ceph_volume.util")
    disk = types.ModuleType("ceph_volume.util.disk")
    disk.UdevData = UdevData
    module = types.ModuleType("ceph_volume.util.device")
    module.Device = Device
    package.util, util.disk, util.device = util, disk, module
    for name, value in (("ceph_volume", package), ("ceph_volume.util", util), ("ceph_volume.util.disk", disk),
                        ("ceph_volume.util.device", module)):
        monkeypatch.setitem(sys.modules, name, value)
    return disk, UdevData


def arguments(monkeypatch, value):
    monkeypatch.setattr(sys, "argv", ["probe", json.dumps(value)])


def test_the_device_probe_feeds_the_supplied_udev_properties_to_the_images_own_identity_code(monkeypatch):
    asked = []
    disk, original = fake_ceph_volume(monkeypatch, asked=asked)
    arguments(monkeypatch, {"path": "/dev/sda", "environment": {"ID_SERIAL": "S1", "ID_WWN": "0x5"}})
    assert json.loads(run_main(device)) == {"version": "ceph-host-device-id-v1", "deviceID": "ID_SERIAL-value",
                                            "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}
    assert asked == [("/dev/sda", {"ID_SERIAL": "S1", "ID_WWN": "0x5"})]
    assert disk.UdevData is original


@pytest.mark.parametrize("value", [{"path": "/dev/sda"}, {"environment": {}}, {"path": "/dev/sda", "environment": {}, "extra": 1}])
def test_the_device_probe_refuses_an_argument_that_is_not_exactly_the_path_and_environment(monkeypatch, value):
    fake_ceph_volume(monkeypatch)
    arguments(monkeypatch, value)
    with pytest.raises(AssertionError):
        device.main()


def test_the_device_probe_refuses_an_identity_that_is_not_text_and_restores_the_udev_reader(monkeypatch):
    disk, original = fake_ceph_volume(monkeypatch, identity=None)
    arguments(monkeypatch, {"path": "/dev/sda", "environment": {}})
    with pytest.raises(AssertionError):
        device.main()
    assert disk.UdevData is original


def test_the_device_probe_refuses_a_udev_read_of_another_path_and_restores_the_udev_reader(monkeypatch):
    disk, original = fake_ceph_volume(monkeypatch)
    arguments(monkeypatch, {"path": "/dev/sda", "environment": {}})
    monkeypatch.setitem(sys.modules, "ceph_volume.util.device", types.SimpleNamespace(Device=type("Device", (), {
        "_get_device_id": lambda self: disk.UdevData("/dev/sdb")})))
    with pytest.raises(AssertionError):
        device.main()
    assert disk.UdevData is original


def test_every_probe_call_site_sends_its_module_source():
    body = inspect.getsource(runtime)
    for name in ("ceph_host_image_probe", "ceph_host_label_probe", "ceph_host_device_probe"):
        assert "module_source(" + name + ")" in body
    assert "PROBE = r'''" not in body
