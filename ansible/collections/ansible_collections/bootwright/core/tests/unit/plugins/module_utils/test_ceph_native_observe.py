"""Synthetic daemon snapshots never launch or repair provider state."""

from __future__ import annotations

import contextlib
import copy
import inspect
import json
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_observe as observe, ceph_native_worker
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as phase_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_tunnel_native as tunnel_fixtures
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.held_listing import HeldListing

REAL_ROOT_DIRECTORY = observe.root_directory
REAL_SETTLE_PAUSE = observe.SETTLE_PAUSE
core_request = phase_fixtures.core_request


@pytest.fixture
def fixture(tmp_path, monkeypatch):
    root = tmp_path / "ceph"
    root.mkdir(mode=0o700)
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid())
    # A snapshot that never settles is retried without real pauses.
    monkeypatch.setattr(observe, "SETTLE_PAUSE", 0)

    @contextlib.contextmanager
    def root_directory(uid=None):
        descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            observe.safe_directory(os.fstat(descriptor), (os.getuid(),))
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(observe, "root_directory", root_directory)
    request = {"fsid": "11111111-1111-4111-8111-111111111111", "image": "registry.example/ceph@sha256:" + "a" * 64}
    cluster = root / request["fsid"]
    cluster.mkdir(mode=0o700)
    directory = cluster / "mgr.host.abcdef"
    directory.mkdir(mode=0o700)
    (directory / "unit.meta").write_text(json.dumps({"service_name": "mgr", "ports": [9283, 8765], "memory_request": None, "memory_limit": None,
                                                     "private": "synthetic never projected"}))
    (directory / "unit.image").write_text(request["image"] + "\n")
    container = "ceph-" + request["fsid"] + "-mgr-host-abcdef"
    unit = "ceph-" + request["fsid"] + "@mgr.host.abcdef.service"
    state = dict(Id=unit, LoadState="loaded", ActiveState="active", SubState="running", UnitFileState="enabled",
                 FragmentPath="/etc/systemd/system/ceph-" + request["fsid"] + "@.service", MainPID="123")
    container_value = [{"Name": container, "Id": "b" * 64, "Image": "c" * 64, "ImageName": request["image"], "State": {"Running": True},
                        "Secret": "never projected"}]
    image_value = [{"Id": "c" * 64, "RepoDigests": [request["image"]]}]
    calls = []

    def command(argv, **options):
        calls.append((argv, options))
        if argv[0] == "/usr/bin/systemctl":
            assert argv[-1] == unit and options == {"json_result": False}
            return "".join(name + "=" + value + "\n" for name, value in state.items()).encode()
        if argv[:2] == ["/usr/bin/podman", "ps"]:
            assert argv[2:] == ["--all", "--format", "json", "--filter", "name=^" + container + "$"] and not options
            return [{"Id": item["Id"], "Names": [container]} for item in container_value]
        if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
            assert argv[-1] == container and not options
            if not container_value:
                # cephadm's --rm container is gone: Podman exits 125 and the worker refuses the command.
                raise ceph_native.Refusal("native-command")
            return copy.deepcopy(container_value)
        assert argv == ["/usr/bin/podman", "image", "inspect", "c" * 64] and not options
        return copy.deepcopy(image_value)

    return root, directory, request, state, container_value, image_value, calls, command


def snapshot(fixture, **options):
    unused_1, unused_2, request, unused_3, unused_4, unused_5, unused_6, command = fixture
    return observe.daemons(request, options.pop("uid", os.getuid()), command, owned=options.pop("owned", True), **options)


def test_exact_daemon_projection_uses_only_bounded_inspection(fixture):
    result = snapshot(fixture)
    assert len(result) == 1 and result[0]["state"] == "running" and result[0]["enabled"] is True
    assert "Secret" not in result[0] and "private" not in result[0]
    assert "never projected" not in json.dumps(result)
    calls = fixture[-2]
    assert len(calls) == 4
    assert all(argv[0] in ("/usr/bin/systemctl", "/usr/bin/podman") for argv, unused in calls)
    assert all(not set(argv) & {"ls", "pull", "start", "restart", "reload", "exec", "--version"} for argv, unused in calls)


DELEGATED_IMAGE = "registry.example.test/haproxy@sha256:" + "b" * 64


def delegated(fixture, name, service, listed, hosts=("node-1",), unit_files=True):
    """A daemon directory another block executes, laid out as cephadm writes it: unit.meta and unit.image, a unit and a container
    named ceph-<fsid>-<type>-<daemon id> (cephadm get_unique_name with prefix=service_id, cephadm/module.py v20.2.1 L922-L966)."""
    request, command = fixture[2], fixture[-1]
    request["delegated"] = [{"name": entry, "hosts": list(hosts)} for entry in listed]
    directory = fixture[1].parent / name
    directory.mkdir(mode=0o700)
    if unit_files:
        (directory / "unit.meta").write_text(json.dumps({"service_name": service}))
        (directory / "unit.image").write_text(DELEGATED_IMAGE + "\n")
    kind, identifier = name.split(".", 1)
    unit = "ceph-" + request["fsid"] + "@" + name + ".service"
    container = ("ceph-" + request["fsid"] + "-" + kind + "-" + identifier).replace(".", "-")
    state = dict(fixture[3], Id=unit)

    def run(argv, **options):
        if argv[0] == "/usr/bin/systemctl" and argv[-1] == unit:
            return "".join(key + "=" + value + "\n" for key, value in state.items()).encode()
        if argv[:2] == ["/usr/bin/podman", "ps"] and argv[-1] == "name=^" + container + "$":
            return [{"Id": "e" * 64, "Names": [container]}] if unit_files else []
        if argv[:3] == ["/usr/bin/podman", "container", "inspect"] and argv[-1] == container:
            return [{"Name": container, "Id": "e" * 64, "Image": "f" * 64, "ImageName": DELEGATED_IMAGE, "State": {"Running": True}}]
        if argv == ["/usr/bin/podman", "image", "inspect", "f" * 64]:
            return [{"Id": "f" * 64, "RepoDigests": [DELEGATED_IMAGE]}]
        return command(argv, **options)

    return run


def remap_owner(monkeypatch, paths, uid):
    """Report uid as the owner of the given entries, as cephadm's makedirs chown leaves them; the test runs unprivileged."""
    return remap_owners(monkeypatch, {os.stat(path, follow_symlinks=False).st_ino: uid for path in paths})


def remap_owners(monkeypatch, owners, seen=None):
    """Report each mapped inode's uid as it is at every stat, so a test can chown between snapshots as cephadm does; seen,
    when given, is called with each remapped inode."""
    real_stat, real_fstat = os.stat, os.fstat

    def remap(info):
        if info.st_ino not in owners:
            return info
        if seen is not None:
            seen(info.st_ino)
        values = list(info[:10])
        values[4] = owners[info.st_ino]
        return os.stat_result(values, {name: getattr(info, name) for name in ("st_atime_ns", "st_mtime_ns", "st_ctime_ns", "st_blksize", "st_blocks")})

    monkeypatch.setattr(observe.os, "stat", lambda *args, **options: remap(real_stat(*args, **options)))
    monkeypatch.setattr(observe.os, "fstat", lambda descriptor: remap(real_fstat(descriptor)))


@pytest.mark.parametrize("holder", ["alloy", "none", "haproxy", "deploying", "occupied"])
def test_the_fsid_directory_may_belong_to_one_of_its_singleton_daemon_users(fixture, monkeypatch, holder):
    """cephadm's make_data_dir_base chowns the FSID directory to each daemon it deploys (cephadmlib/file_utils.py makedirs, v20.2.1)."""
    cluster = fixture[1].parent
    # cephadm writes into a singleton's directory only after it chowned it: a root-owned one still being deployed is empty.
    run = delegated(fixture, "alloy.node-1", "alloy", ["alloy"], unit_files=holder != "deploying")
    proxy = delegated(fixture, "haproxy.rgw.objects.node-1.abcdef", "ingress.rgw.objects", ["alloy", "ingress.rgw.objects"])
    held = [cluster] + {"alloy": [cluster / "alloy.node-1"], "none": [], "haproxy": [cluster / "haproxy.rgw.objects.node-1.abcdef"],
                        "deploying": [], "occupied": []}[holder]
    remap_owner(monkeypatch, held, os.getuid() + 473)
    if holder in ("none", "haproxy"):
        # The listed singleton's own directory is placed, under another user: nothing is still deploying.
        remap_owner(monkeypatch, [cluster / "alloy.node-1"], os.getuid() + 474)

    def command(argv, **options):
        return run(argv, **options) if "alloy" in " ".join(argv) else proxy(argv, **options)

    if holder == "alloy":
        result = observe.daemons(fixture[2], os.getuid(), command, owned=True, host="node-1")
        assert [item["name"] for item in result][:1] == ["alloy.node-1"]
    elif holder == "deploying":
        # cephadm chowned the FSID directory and has made, as root, but not yet chowned the listed alloy directory. A
        # root-owned alloy directory cephadm already wrote into (occupied) is past that step and refuses at once.
        with pytest.raises(observe.ObservationChangedRefusal, match="^native-observation-changed$"):
            observe.snapshot(fixture[2], os.getuid(), command, owned=True, host="node-1")
    else:
        with pytest.raises(observe.ObservationRefusal, match="^native-observation-unproved$") as refused:
            observe.snapshot(fixture[2], os.getuid(), command, owned=True, host="node-1")
        assert not isinstance(refused.value, observe.ObservationChangedRefusal)


@pytest.mark.parametrize("holder,admitted", [("root", True), ("singleton", True), ("none", False), ("non-singleton", False)])
def test_the_shared_cluster_directory_rule_serves_the_cephadm_cache_too(fixture, monkeypatch, holder, admitted):
    """ceph_host_runtime opens the FSID directory through this helper for its cephadm cache, so the chown rule holds there."""
    root, cluster = fixture[0], fixture[1].parent
    (cluster / "grafana.node-1").mkdir(mode=0o700)
    (cluster / "haproxy.rgw.objects.node-1.abcdef").mkdir(mode=0o700)
    held = {"root": [], "singleton": [cluster, cluster / "grafana.node-1"], "none": [cluster],
            "non-singleton": [cluster, cluster / "haproxy.rgw.objects.node-1.abcdef"]}[holder]
    remap_owner(monkeypatch, held, os.getuid() + 472)
    parent = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        if admitted:
            with observe.cluster_directory(parent, fixture[2]["fsid"], (os.getuid(),)) as directory:
                assert os.fstat(directory).st_ino == os.stat(cluster).st_ino
        else:
            with pytest.raises(observe.ObservationRefusal, match="^native-observation-unproved$"):
                with observe.cluster_directory(parent, fixture[2]["fsid"], (os.getuid(),)):
                    pass
    finally:
        os.close(parent)


def test_listed_delegated_daemon_is_reported_without_the_ceph_image(fixture):
    run = delegated(fixture, "haproxy.rgw.objects.node-1.abcdef", "ingress.rgw.objects", ["ingress.rgw.objects"])
    result = observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
    assert [item["name"] for item in result] == ["haproxy.rgw.objects.node-1.abcdef", "mgr.host.abcdef"]
    assert result[0]["state"] == "running" and result[0]["container_image_name"] == DELEGATED_IMAGE
    assert result[0]["container_image_digests"] == [DELEGATED_IMAGE] and result[1]["container_image_name"] == fixture[2]["image"]


@pytest.mark.parametrize("change", ["unlisted", "other-host", "no-host", "foreign-family"])
def test_unlisted_delegated_daemon_refuses(fixture, change):
    listed = ["rgw.objects"] if change == "unlisted" else ["ingress.rgw.objects"]
    service = "rgw.objects" if change == "foreign-family" else "ingress.rgw.objects"
    hosts = ("node-2",) if change == "other-host" else ("node-1",)
    run = delegated(fixture, "haproxy.rgw.objects.node-1.abcdef", service, listed, hosts)
    with pytest.raises(observe.ObservationRefusal):
        observe.daemons(fixture[2], os.getuid(), run, owned=True, host=None if change == "no-host" else "node-1")


def test_delegated_daemon_without_unit_files_matches_one_listed_prefix(fixture):
    run = delegated(fixture, "rgw.objects.node-1.abcdef", None, ["nfs.objects", "rgw.objects"], unit_files=False)
    result = observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
    assert result[1]["name"] == "rgw.objects.node-1.abcdef" and result[1]["state"] == "unknown"
    assert result[1]["container_image_name"] == "" and result[1]["container_id"] == ""
    (fixture[1].parent / "rgw.objects.node-1.abcdef").rmdir()
    run = delegated(fixture, "rgw.a.b.node-1.abcdef", None, ["rgw.a", "rgw.a.b"], unit_files=False)
    with pytest.raises(observe.ObservationRefusal):
        observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")


@pytest.mark.parametrize("change", ["listed", "no-unit-files", "unlisted", "other-host"])
def test_host_layout_admits_a_listed_rbd_mirror_daemon(fixture, change):
    listed = ["nfs.files"] if change == "unlisted" else ["rbd-mirror"]
    hosts = ("node-2",) if change == "other-host" else ("node-1",)
    service = None if change == "no-unit-files" else "rbd-mirror"
    run = delegated(fixture, "rbd-mirror.node-1.abcdef", service, listed, hosts, unit_files=change != "no-unit-files")
    if change in ("unlisted", "other-host"):
        with pytest.raises(observe.ObservationRefusal):
            observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
        return
    result = observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
    assert [item["name"] for item in result] == ["mgr.host.abcdef", "rbd-mirror.node-1.abcdef"]
    assert result[1]["state"] == ("unknown" if change == "no-unit-files" else "running")


@pytest.mark.parametrize("change", ["listed", "no-unit-files", "unlisted", "other-host"])
def test_host_layout_admits_a_listed_crash_daemon(fixture, change):
    assert not observe.CLUSTER_TYPES & set(observe.DELEGATED_TYPES)
    listed = ["nfs.files"] if change == "unlisted" else ["crash"]
    hosts = ("node-2",) if change == "other-host" else ("node-1",)
    service = None if change == "no-unit-files" else "crash"
    run = delegated(fixture, "crash.node-1", service, listed, hosts, unit_files=change != "no-unit-files")
    if change in ("unlisted", "other-host"):
        with pytest.raises(observe.ObservationRefusal):
            observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
        return
    result = observe.daemons(fixture[2], os.getuid(), run, owned=True, host="node-1")
    assert [item["name"] for item in result] == ["crash.node-1", "mgr.host.abcdef"]
    assert result[0]["state"] == ("unknown" if change == "no-unit-files" else "running")


def cache(fixture):
    path = fixture[1].parent / ("cephadm." + "d" * 64)
    path.write_bytes(b"synthetic provider binary which must never be read")
    path.chmod(0o744)
    return path


def test_provider_cache_is_metadata_only_and_not_daemon_evidence(fixture, monkeypatch):
    path = cache(fixture)
    original = observe.public_file

    def public_file(directory, name, owners):
        assert name != path.name
        return original(directory, name, owners)

    monkeypatch.setattr(observe, "public_file", public_file)
    assert len(snapshot(fixture)) == 1 and len(fixture[-2]) == 4
    assert path.name not in json.dumps(snapshot(fixture))


def test_nonroot_provider_cache_requires_original_authorized_account_uid(fixture, monkeypatch):
    cache(fixture)
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid() + 1)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert snapshot(fixture, cache_uid=os.getuid())[0]["state"] == "running"


@pytest.mark.parametrize("change", ["symlink", "directory", "fifo", "hardlink", "writable", "empty", "oversized"])
def test_cache_refuses_unprotected_alias_or_invalid_metadata(fixture, change):
    path = cache(fixture)
    if change in ("symlink", "directory", "fifo"):
        path.unlink()
        if change == "symlink":
            path.symlink_to(fixture[1] / "unit.meta")
        elif change == "directory":
            path.mkdir(mode=0o744)
        else:
            os.mkfifo(path, mode=0o744)
    elif change == "hardlink":
        os.link(path, path.parent / "alias")
    elif change == "writable":
        path.chmod(0o764)
    elif change == "empty":
        path.write_bytes(b"")
    else:
        with path.open("wb") as handle:
            handle.truncate(observe.MAX_CACHE + 1)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


@pytest.mark.parametrize("name", ["cephadm", "cephadm." + "A" * 64, "cephadm." + "d" * 63, "cephadm." + "d" * 64 + ".new", "other." + "d" * 64])
def test_unknown_cache_name_refuses_without_native_probe(fixture, name):
    (fixture[1].parent / name).write_bytes(b"unowned")
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


def test_cache_replacement_during_native_probe_refuses(fixture):
    path = cache(fixture)
    command = fixture[-1]

    def replace(argv, **options):
        value = command(argv, **options)
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            path.rename(fixture[0].parent / "detached")
            path.write_bytes(b"same name is not original custody")
            path.chmod(0o744)
        return value

    # Replaced during every snapshot: it never settles.
    with pytest.raises(observe.ObservationChangedRefusal):
        observe.daemons(fixture[2], os.getuid(), replace, owned=True)


@pytest.mark.parametrize("name", ["config", "crash", "removed"])
def test_auxiliary_directory_is_nofollow_metadata_only(fixture, name):
    path = fixture[1].parent / name
    path.mkdir(mode=0o700)
    (path / "private").write_bytes(b"must never be read")
    assert len(snapshot(fixture)) == 1
    (path / "private").unlink()
    path.rmdir()
    path.symlink_to(fixture[1], target_is_directory=True)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)


def test_cache_uid_lends_only_original_authorization_metadata(core_request, monkeypatch):
    markers = phase_fixtures.Markers()
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=markers)
    uid = os.getuid()
    value = {"uid": uid, "home": "/home/fixture",
             "line": ceph_native.tunnel_helpers()[0].authorization_line(tunnel_fixtures.public_key(), core_request["owner"])}
    markers.records["ssh-authorization.json"] = value
    monkeypatch.setattr(ceph_native.pwd, "getpwnam", lambda user: SimpleNamespace(pw_uid=uid, pw_dir="/home/fixture"))
    native.native_source = lambda *args, **kwargs: pytest.fail("observer read private/native authorization content")
    assert native.observer_cache_uid() == uid
    value["uid"] = float(uid)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.observer_cache_uid()


def test_worker_artifact_proof_precedes_lending_exact_image_uid(core_request, monkeypatch):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())
    proofs = []
    native.verify_tool = lambda: proofs.append("verified")
    native._profile = {"uid": 167, "gid": 167, "policy": "Ciphers aes256-gcm@openssh.com\n", "label": "fixture:shared:label:s0"}

    def daemons(request, uid, command, *, owned, cache_uid, clock, pause, host):
        assert proofs == ["verified"] and request is native.request
        assert uid == 167 and command == native.command and owned is True and cache_uid is None
        assert clock == native.clock and pause == native.pause and host == core_request["bootstrap"]
        return []

    monkeypatch.setattr(observe, "daemons", daemons)
    assert native.daemons() == []


def test_unproved_artifact_stops_before_metadata_inspection(core_request, monkeypatch):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())

    def refuse():
        raise ceph_native.Refusal("artifact")

    native.verify_tool = refuse
    monkeypatch.setattr(observe, "daemons", lambda *args, **kwargs: pytest.fail("metadata inspection"))
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        native.daemons()


def test_native_observation_refusal_does_not_fallback_or_disclose(core_request, monkeypatch):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())
    native.host_markers = phase_fixtures.Markers(owned=False)
    native.verify_tool = lambda: None
    native.command = lambda *args, **kwargs: pytest.fail("fallback native command")

    def refuse(*args, **kwargs):
        raise observe.ObservationRefusal("synthetic native output which must stay private")

    monkeypatch.setattr(observe, "daemons", refuse)
    with pytest.raises(ceph_native.Refusal, match="^native-observation-unproved$"):
        native.daemons()


def test_empty_pristine_root_needs_no_image_uid_or_effects(fixture):
    root, directory, unused_1, unused_2, unused_3, unused_4, calls, unused_5 = fixture
    for name in ("unit.meta", "unit.image"):
        (directory / name).unlink()
    directory.rmdir()
    directory.parent.rmdir()
    assert snapshot(fixture, uid=None, owned=False) == [] and calls == []


@pytest.mark.parametrize("foreign", ["22222222-2222-4222-8222-222222222222", "mon", "bootstrap-osd", "arbitrary"])
def test_foreign_layout_refuses_before_private_or_native_read(fixture, monkeypatch, foreign):
    root = fixture[0]
    (root / foreign).mkdir()
    monkeypatch.setattr(observe, "public_file", lambda *args: pytest.fail("foreign metadata read"))
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


@pytest.mark.parametrize("option", [{"uid": None}, {"uid": True}, {"uid": 0}, {"owned": False}, {"owned": 1}])
def test_unproved_owner_or_uid_never_reads_existing_daemon(fixture, monkeypatch, option):
    monkeypatch.setattr(observe, "public_file", lambda *args: pytest.fail("unbound metadata read"))
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture, **option)
    assert fixture[-2] == []


@pytest.mark.parametrize("kind", ["symlink", "fifo", "hardlink", "writable", "empty", "oversized"])
def test_metadata_descriptor_type_and_alias_refusal(fixture, kind):
    path = fixture[1] / "unit.meta"
    if kind == "symlink":
        retained = path.with_name("foreign")
        path.rename(retained)
        path.symlink_to(retained)
    elif kind == "fifo":
        path.unlink()
        os.mkfifo(path)
    elif kind == "hardlink":
        os.link(path, path.with_name("alias"))
    elif kind == "writable":
        path.chmod(0o666)
    elif kind == "empty":
        path.write_bytes(b"")
    else:
        path.write_bytes(b"x" * (observe.MAX_METADATA + 1))
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


@pytest.mark.parametrize("value", [b"not-json", b'{"service_name":"mgr","service_name":"mon"}', b'{"service_name":"osd"}', b"[]"])
def test_malformed_or_wrong_native_metadata_refuses(fixture, value):
    (fixture[1] / "unit.meta").write_bytes(value)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


@pytest.mark.parametrize("field,value",
                         [("Id", "other.service"), ("LoadState", "not-found"), ("FragmentPath", "/foreign/ceph.service"), ("UnitFileState", "masked"),
                          ("MainPID", "-1"), ("MainPID", "123\nMainPID=456"), ("ActiveState", "unknown")])
def test_foreign_or_malformed_unit_state_refuses(fixture, field, value):
    fixture[3][field] = value
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert len(fixture[-2]) == 1


@pytest.mark.parametrize("field,value",
                         [("Name", "other"), ("Id", "short"), ("Image", "short"), ("ImageName", "registry.example/other:latest"), ("State", {"Running": 1}),
                          ("State", {"Running": False})])
def test_container_identity_or_status_disagreement_refuses(fixture, field, value):
    fixture[4][0][field] = value
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)


@pytest.mark.parametrize("field,value",
                         [("Id", "d" * 64), ("RepoDigests", []), ("RepoDigests", ["registry.example/other@sha256:" + "d" * 64]), ("RepoDigests", [True])])
def test_actual_image_identity_and_digest_are_independently_proved(fixture, field, value):
    fixture[5][0][field] = value
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)


def test_stopped_daemon_is_partial_without_repair(fixture):
    fixture[3].update(ActiveState="inactive", SubState="dead", MainPID="0", UnitFileState="disabled")
    fixture[4][0]["State"]["Running"] = False
    observed = snapshot(fixture)
    assert observed[0]["state"] == "stopped" and observed[0]["enabled"] is False
    assert len(fixture[-2]) == 4


def test_failed_or_missing_container_probe_is_unknown_with_no_fallback(fixture):
    original = fixture[-1]
    calls = []

    def command(argv, **options):
        calls.append(argv)
        if argv[0] == "/usr/bin/podman":
            raise observe.ObservationRefusal("native-observation-unproved")
        return original(argv, **options)

    with pytest.raises(observe.ObservationRefusal):
        observe.daemons(fixture[2], os.getuid(), command, owned=True)
    assert len(calls) == 2


def test_metadata_replacement_during_native_snapshot_refuses(fixture):
    original = fixture[-1]

    def command(argv, **options):
        result = original(argv, **options)
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            path = fixture[1] / "unit.image"
            replacement = path.with_name("replacement")
            replacement.write_bytes(path.read_bytes())
            replacement.replace(path)
        return result

    with pytest.raises(observe.ObservationChangedRefusal):
        observe.daemons(fixture[2], os.getuid(), command, owned=True)


def test_fixed_root_named_ancestry_is_reproved_after_native_snapshot(fixture, monkeypatch):
    root = fixture[0]
    monkeypatch.setattr(observe, "DATA_ROOT", str(root))
    monkeypatch.setattr(observe, "root_directory", REAL_ROOT_DIRECTORY)
    # Loan the synthetic test tree's ancestor policy while exercising the
    # actual nofollow parent/name/inode bindings, not the borrowed-root seam.
    monkeypatch.setattr(observe, "safe_directory", lambda info, owners: observe.require(stat.S_ISDIR(info.st_mode)))
    original = fixture[-1]

    def command(argv, **options):
        value = original(argv, **options)
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            root.rename(root.with_name("detached"))
            root.mkdir(mode=0o700)
        return value

    pauses = []
    # The moved root is caught by its re-proof; the snapshot taken again proves the replacement as it is.
    assert observe.daemons(fixture[2], os.getuid(), command, owned=True, pause=pauses.append) == []
    assert pauses == [observe.SETTLE_PAUSE] and list(root.iterdir()) == []
    root.rename(root.with_name("again"))
    root.with_name("detached").rename(root)
    monkeypatch.setattr(observe, "SETTLE_ATTEMPTS", 1)
    with pytest.raises(observe.ObservationChangedRefusal):
        observe.daemons(fixture[2], os.getuid(), command, owned=True)


def test_proved_absent_final_root_needs_no_uid_and_does_not_create_it(fixture, monkeypatch):
    absent = fixture[0].with_name("absent")
    monkeypatch.setattr(observe, "DATA_ROOT", str(absent))
    monkeypatch.setattr(observe, "root_directory", REAL_ROOT_DIRECTORY)
    monkeypatch.setattr(observe, "safe_directory", lambda info, owners: observe.require(stat.S_ISDIR(info.st_mode)))
    assert observe.daemons(fixture[2], None, lambda *args, **kwargs: pytest.fail("native command"), owned=False) == []
    assert not absent.exists()


def test_container_name_collision_refuses_before_commands(fixture):
    fixture[1].rename(fixture[1].with_name("mgr.host.a.b"))
    fixture[1].with_name("mgr.host.a-b").mkdir()
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


def test_bounded_directory_scan_refuses_without_commands(fixture, monkeypatch):
    monkeypatch.setattr(observe, "MAX_ENTRIES", 1)
    (fixture[1].parent / "mgr.other").mkdir()
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


def test_entries_lists_the_held_directory_as_it_is_now(tmp_path, monkeypatch):
    held = HeldListing()
    monkeypatch.setattr(observe, "os", held)
    descriptor = held.open(str(tmp_path), os.O_RDONLY | os.O_DIRECTORY)
    try:
        assert observe.entries(descriptor) == []
        (tmp_path / "added").mkdir()
        assert held.held[descriptor] == []
        assert observe.entries(descriptor) == ["added"]
    finally:
        held.close(descriptor)


def test_selinux_directory_cephadm_makes_for_osd_and_ceph_volume_is_metadata_only(fixture, monkeypatch):
    path = fixture[1].parent / "selinux"
    path.mkdir()
    path.chmod(0o755)
    (path / "private").write_bytes(b"must never be read")
    assert len(snapshot(fixture)) == 1
    path.chmod(0o700)
    assert len(snapshot(fixture)) == 1
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid() + 1)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture, cache_uid=None)


@pytest.mark.parametrize("drift", ["writable", "setgid", "symlink", "file", "renamed"])
def test_selinux_directory_with_another_shape_or_name_refuses(fixture, drift):
    path = fixture[1].parent / "selinux"
    if drift == "symlink":
        path.symlink_to(fixture[1], target_is_directory=True)
    elif drift == "file":
        path.write_bytes(b"")
    else:
        path = path.with_name("selinux.old" if drift == "renamed" else "selinux")
        path.mkdir()
        path.chmod({"writable": 0o775, "setgid": 0o2755, "renamed": 0o755}[drift])
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)


@pytest.mark.parametrize("missing", [("unit.meta", "unit.image"), ("unit.image",)])
def test_daemon_directory_before_its_unit_files_is_not_running(fixture, missing):
    for name in missing:
        (fixture[1] / name).unlink()
    fixture[4].clear()
    observed = snapshot(fixture)
    assert observed == [{"style": "cephadm:v1", "name": "mgr.host.abcdef", "fsid": fixture[2]["fsid"],
                         "systemd_unit": "ceph-" + fixture[2]["fsid"] + "@mgr.host.abcdef", "enabled": False, "state": "unknown",
                         "container_id": "", "container_image_name": "", "container_image_id": "", "container_image_digests": []}]
    assert [argv[1] for argv, unused in fixture[-2]] == ["ps"]


def test_daemon_without_unit_files_but_with_a_container_refuses(fixture):
    (fixture[1] / "unit.image").unlink()
    pauses = []
    with pytest.raises(observe.ObservationRefusal, match="^native-observation-unproved$"):
        snapshot(fixture, pause=pauses.append)
    assert pauses == []


@pytest.mark.parametrize("unit,state", [({}, "unknown"), ({"ActiveState": "activating", "SubState": "start", "MainPID": "0"}, "unknown"),
                                        ({"ActiveState": "inactive", "SubState": "dead", "MainPID": "0"}, "stopped"),
                                        ({"ActiveState": "failed", "SubState": "failed", "MainPID": "0"}, "error")])
def test_daemon_whose_rm_container_is_gone_is_not_running(fixture, unit, state):
    fixture[3].update(unit)
    fixture[4].clear()
    observed = snapshot(fixture)
    assert observed[0]["state"] == state and observed[0]["enabled"] is True and observed[0]["container_image_name"] == fixture[2]["image"]
    assert observed[0]["container_id"] == "" and observed[0]["container_image_digests"] == []
    assert not any(argv[1:3] == ["container", "inspect"] or argv[1:3] == ["image", "inspect"] for argv, unused in fixture[-2])


def test_container_removed_between_listing_and_inspection_is_not_running(fixture):
    original = fixture[-1]

    def command(argv, **options):
        if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
            fixture[4].clear()
        return original(argv, **options)

    observed = observe.daemons(fixture[2], os.getuid(), command, owned=True)
    assert observed[0]["state"] == "unknown" and observed[0]["container_id"] == ""


def test_failed_inspection_of_a_listed_container_refuses(fixture):
    original = fixture[-1]

    def command(argv, **options):
        if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
            raise ceph_native.Refusal("native-command")
        return original(argv, **options)

    pauses = []
    with pytest.raises(observe.ObservationChangedRefusal):
        observe.daemons(fixture[2], os.getuid(), command, owned=True, pause=pauses.append)
    # A container still listed after its inspection failed is observed again, bounded, and never admitted.
    assert len(pauses) == observe.SETTLE_ATTEMPTS - 1


def test_instance_before_its_template_unit_is_installed_is_not_running(fixture):
    fixture[3].update(LoadState="not-found", ActiveState="inactive", SubState="dead", UnitFileState="", FragmentPath="", MainPID="0")
    fixture[4].clear()
    observed = snapshot(fixture)
    assert observed[0]["state"] == "unknown" and observed[0]["enabled"] is False and observed[0]["container_id"] == ""


@pytest.mark.parametrize("contradiction", ["unit-image", "unit-without-container-proof", "container-image", "container-running"])
def test_transient_states_never_admit_a_contradiction(fixture, contradiction):
    if contradiction == "unit-image":
        (fixture[1] / "unit.image").write_text("registry.example/ceph@sha256:" + "f" * 64 + "\n")
        fixture[4].clear()
    elif contradiction == "unit-without-container-proof":
        fixture[3].update(LoadState="not-found", ActiveState="inactive", SubState="dead", UnitFileState="", FragmentPath="", MainPID="0")
    elif contradiction == "container-image":
        fixture[4][0]["ImageName"] = "registry.example/ceph@sha256:" + "f" * 64
    else:
        fixture[3].update(ActiveState="inactive", SubState="dead", MainPID="0")
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)


def test_foreign_owned_or_open_daemon_directory_still_refuses_while_transient(fixture):
    for name in ("unit.meta", "unit.image"):
        (fixture[1] / name).unlink()
    fixture[4].clear()
    fixture[1].chmod(0o777)
    with pytest.raises(observe.ObservationRefusal):
        snapshot(fixture)
    assert fixture[-2] == []


@pytest.mark.parametrize("unit", [{"ActiveState": "activating", "SubState": "start", "MainPID": "0"},
                                  {"ActiveState": "deactivating", "SubState": "stop-sigterm", "MainPID": "123"}])
@pytest.mark.parametrize("running", [True, False])
def test_forking_unit_in_transition_admits_its_container_either_way(fixture, unit, running):
    fixture[3].update(unit)
    fixture[4][0]["State"]["Running"] = running
    observed = snapshot(fixture)
    assert observed[0]["state"] == "unknown" and observed[0]["container_id"] == "b" * 64


class Settling:
    """Counts snapshots by their last native read and pauses without sleeping."""

    def __init__(self, fixture, change, times=1, cost=0):
        self.fixture, self.change, self.times, self.cost = fixture, change, times, cost
        self.snapshots, self.pauses, self.now = 0, [], 0

    def command(self, argv, **options):
        value = self.fixture[-1](argv, **options)
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            self.snapshots += 1
            self.now += self.cost
            if self.snapshots <= self.times:
                self.change(self.fixture)
        return value

    def clock(self):
        return self.now

    def pause(self, seconds):
        self.pauses.append(seconds)
        self.now += seconds

    def daemons(self, **options):
        return observe.daemons(self.fixture[2], options.pop("uid", os.getuid()), self.command, owned=True, clock=self.clock, pause=self.pause, **options)


def stop_and_list(fixture):
    """cephadm adds an entry and stops the daemon after this snapshot read the unit."""
    (fixture[1].parent / "crash").mkdir(mode=0o700)
    fixture[3].update(ActiveState="inactive", SubState="dead", MainPID="0")
    fixture[4][0]["State"]["Running"] = False


def test_a_listing_changed_during_the_snapshot_settles_on_the_next(fixture):
    settling = Settling(fixture, stop_and_list)
    observed = settling.daemons()
    assert settling.snapshots == 2 and settling.pauses == [observe.SETTLE_PAUSE]
    assert observed[0]["state"] == "stopped" and observed[0]["container_id"] == "b" * 64


def rewrite(how):
    def change(fixture):
        path = fixture[1] / "unit.meta"
        content, info = path.read_bytes(), path.stat()
        if how == "replaced":
            # cephadm writes unit files to a new name and renames it over the old one: a new inode.
            path.with_name("unit.meta.new").write_bytes(content)
            path.with_name("unit.meta.new").replace(path)
        else:
            path.write_bytes(content)
        os.utime(path, ns=(info.st_atime_ns, info.st_mtime_ns + 10 ** 9))
    return change


@pytest.mark.parametrize("how", ["replaced", "rewritten"])
def test_unit_meta_rewritten_during_the_snapshot_is_taken_again(fixture, how):
    settling = Settling(fixture, rewrite(how))
    assert settling.daemons()[0]["state"] == "running"
    assert settling.snapshots == 2 and settling.pauses == [observe.SETTLE_PAUSE]


def test_a_daemon_directory_removed_after_its_listing_is_a_change(fixture, monkeypatch):
    original = observe.entries
    removed = []

    def entries(directory):
        names = original(directory)
        if "mgr.host.abcdef" in names and not removed:
            # cephadm rm-daemon moves the directory away between the listing and its open.
            fixture[1].rename(fixture[0].parent / "gone")
            removed.append(True)
        return names

    monkeypatch.setattr(observe, "entries", entries)
    settling = Settling(fixture, lambda unused: None, times=0)
    assert settling.daemons() == [] and settling.pauses == [observe.SETTLE_PAUSE]


def test_a_unit_that_moved_between_its_read_and_its_container_is_a_change(fixture):
    original = fixture[-1]
    reads = []

    def command(argv, **options):
        if argv[0] == "/usr/bin/systemctl":
            reads.append(True)
            if len(reads) == 1:
                # Read before cephadm started the unit; its container ran by the time Podman answered.
                inactive = dict(fixture[3], ActiveState="inactive", SubState="dead", MainPID="0")
                return "".join(name + "=" + value + "\n" for name, value in inactive.items()).encode()
        return original(argv, **options)

    pauses = []
    observed = observe.daemons(fixture[2], os.getuid(), command, owned=True, pause=pauses.append)
    assert observed[0]["state"] == "running" and len(reads) == 3 and pauses == [observe.SETTLE_PAUSE]


@pytest.mark.parametrize("horizon", [False, True])
def test_a_snapshot_that_never_settles_is_changed_and_bounded(fixture, monkeypatch, horizon):
    monkeypatch.setattr(observe, "SETTLE_PAUSE", REAL_SETTLE_PAUSE)
    # A slow host's snapshots stop at the horizon before the attempt bound: 20 s each reach 66 s at the third.
    settling = Settling(fixture, rewrite("replaced"), times=1000, cost=20 if horizon else 0)
    with pytest.raises(observe.ObservationChangedRefusal, match="^native-observation-changed$"):
        settling.daemons()
    attempts = 3 if horizon else observe.SETTLE_ATTEMPTS
    assert settling.snapshots == attempts and settling.pauses == [observe.SETTLE_PAUSE] * (attempts - 1)


def foreign_owner(fixture, monkeypatch):
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid() + 1)
    return {"uid": os.getuid() + 2}


def writable(fixture, unused):
    fixture[1].chmod(0o777)


def foreign_name(fixture, unused):
    (fixture[0] / "22222222-2222-4222-8222-222222222222").mkdir()


def foreign_unit_image(fixture, unused):
    (fixture[1] / "unit.image").write_text("registry.example/ceph@sha256:" + "f" * 64 + "\n")


def foreign_container_image(fixture, unused):
    fixture[4][0]["ImageName"] = "registry.example/ceph@sha256:" + "f" * 64


def container_running_under_a_stopped_unit(fixture, unused):
    fixture[3].update(ActiveState="inactive", SubState="dead", MainPID="0")


def container_without_its_unit(fixture, unused):
    fixture[3].update(LoadState="not-found", ActiveState="inactive", SubState="dead", UnitFileState="", FragmentPath="", MainPID="0")


def container_without_its_unit_files(fixture, unused):
    (fixture[1] / "unit.meta").unlink()


@pytest.mark.parametrize("contradiction", [foreign_owner, writable, foreign_name, foreign_unit_image, foreign_container_image,
                                           container_running_under_a_stopped_unit, container_without_its_unit,
                                           container_without_its_unit_files])
def test_a_contradiction_refuses_at_once_without_another_snapshot(fixture, monkeypatch, contradiction):
    options = contradiction(fixture, monkeypatch) or {}
    settling = Settling(fixture, lambda unused: None, times=0)
    with pytest.raises(observe.ObservationRefusal, match="^native-observation-unproved$") as refused:
        settling.daemons(**options)
    assert not isinstance(refused.value, observe.ObservationChangedRefusal) and settling.pauses == [] and settling.snapshots <= 1


def test_worker_maps_an_unsettled_snapshot_to_not_ready_and_a_contradiction_to_unproved(core_request, monkeypatch):
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())
    native.verify_tool = lambda: None
    native.observed_manager_uid = lambda: 167
    native.observer_cache_uid = lambda: None
    for error, reason in ((observe.ObservationChangedRefusal("native-observation-changed"), "not-ready"),
                          (observe.ObservationRefusal("native-observation-unproved"), "native-observation-unproved")):
        def refuse(*args, failure=error, **kwargs):
            raise failure

        monkeypatch.setattr(observe, "daemons", refuse)
        with pytest.raises(ceph_native.Refusal, match="^" + reason + "$") as refused:
            native.daemons()
        # The worker names the token at its site; the controller's wait polls again on exactly the not-ready one.
        cause = ceph_native_worker.cause(refused.value)
        assert cause.startswith(reason + "@ceph_native.daemons:") and ceph_native_worker.attested(cause, {"ceph_native": inspect.getsource(ceph_native)})
        assert ceph_core.unsettled(ceph_native.RemoteRefusal("native-refusal:" + cause)) is (reason == "not-ready")


NOT_LOADED = dict(LoadState="not-found", ActiveState="inactive", SubState="dead", UnitFileState="", FragmentPath="", MainPID="0")


class Transient:
    """One cephadm deploy step caught in the first `times` snapshots: counts each snapshot that observed it, pauses without sleeping."""

    def __init__(self, fixture, monkeypatch, condition, times, native=None):
        self.fixture, self.times, self.condition, self.monkeypatch, self.native = fixture, times, condition, monkeypatch, native
        self.base, self.host, self.seen, self.pauses, self.now, self.reads = fixture[-1], None, set(), [], 0, 0
        self.begin = getattr(condition, "setup", lambda state, monkeypatch: None)(self, monkeypatch) or (lambda: None)
        self.begin()

    def active(self):
        return len(self.pauses) < self.times

    def fire(self):
        self.seen.add(len(self.pauses))

    def command(self, argv, **options):
        return self.condition(self, argv, options) if self.active() else self.base(argv, **options)

    def clock(self):
        return self.now

    def pause(self, seconds):
        self.pauses.append(seconds)
        self.now += seconds
        self.reads = 0
        self.begin()

    def daemons(self):
        return observe.daemons(self.fixture[2], os.getuid(), self.command, owned=True, clock=self.clock, pause=self.pause, host=self.host)


def exited_container(state, argv, options):
    """The container exited and conmon still runs the --rm cleanup; systemd has not yet seen its unit stop."""
    value = state.base(argv, **options)
    if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
        state.fire()
        value[0]["State"]["Running"] = False
    return value


def unit_read_before_its_load(state, argv, options):
    """The unit was read before cephadm installed and started it; its container ran by the listing."""
    if argv[0] == "/usr/bin/systemctl":
        state.reads += 1
        if state.reads == 1:
            state.fire()
            return "".join(name + "=" + value + "\n" for name, value in dict(state.fixture[3], **NOT_LOADED).items()).encode()
    return state.base(argv, **options)


def unit_files_written_after_their_read(state, argv, options):
    """cephadm wrote the unit files and started the daemon between the unit files' read and the container listing."""
    if argv[:2] == ["/usr/bin/podman", "ps"] and not (state.fixture[1] / "unit.image").exists():
        state.fire()
        (state.fixture[1] / "unit.image").write_text(state.fixture[2]["image"] + "\n")
    return state.base(argv, **options)


def remove_unit_image(state, unused):
    def begin():
        if state.active():
            (state.fixture[1] / "unit.image").unlink()
    return begin


unit_files_written_after_their_read.setup = remove_unit_image


def inspection_failed_while_listed(state, argv, options):
    """Podman failed the inspection of a container its --rm cleanup is removing, and still lists it."""
    if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
        state.fire()
        raise ceph_native.Refusal("native-command")
    return state.base(argv, **options)


def failing_run(kind, monkeypatch):
    """The production runner of a real worker: a non-zero exit, unparsable output, or a bounded process failure."""
    if kind == "exit":
        return lambda argv, data=b"", timeout=60: (1, ceph_native.NativeOutput(b"", b"private native stderr"))
    if kind == "unparsable":
        return lambda argv, data=b"", timeout=60: (0, ceph_native.NativeOutput(b"private not json"))

    def run(*unused, **options):
        raise ceph_native.bounded_process.Failure(kind)

    monkeypatch.setattr(ceph_native.bounded_process, "run", run)
    return ceph_native.bounded_command


def native_failure(prefix, kind):
    """A native read whose failure the real worker raises, so its cause walks the same shipped frames as in production."""
    def condition(state, argv, options):
        if argv[:len(prefix)] == prefix:
            state.fire()
            state.native.run = failing_run(kind, state.monkeypatch)
            return state.native.command(argv, **options)
        return state.base(argv, **options)
    condition.__name__ = "native_" + kind + "_" + prefix[-1].rsplit("/", 1)[-1]
    return condition


def fsid_directory_chowned_first(state, argv, options):
    return state.base(argv, **options)


def chown_fsid_directory_first(state, monkeypatch):
    """cephadm chowned the FSID directory to alloy's user and has made, as root, but not yet chowned alloy's own directory."""
    cluster = state.fixture[1].parent
    state.base = delegated(state.fixture, "alloy.node-1", "alloy", ["alloy"], unit_files=False)
    state.host = "node-1"
    fsid, alloy = os.stat(cluster).st_ino, os.stat(cluster / "alloy.node-1").st_ino
    owners = {fsid: os.getuid() + 473}

    def seen(inode):
        if inode == fsid and alloy not in owners:
            state.fire()

    remap_owners(monkeypatch, owners, seen)

    def begin():
        if not state.active():
            owners[alloy] = os.getuid() + 473
    return begin


fsid_directory_chowned_first.setup = chown_fsid_directory_first

TRANSIENT = [(exited_container, "daemon"), (unit_read_before_its_load, "daemon"), (unit_files_written_after_their_read, "daemon"),
             (inspection_failed_while_listed, "daemon"),
             (native_failure(["/usr/bin/systemctl"], "timeout"), "native_read"),
             (native_failure(["/usr/bin/podman", "ps"], "exit"), "native_read"),
             (native_failure(["/usr/bin/podman", "ps"], "command"), "native_read"),
             (native_failure(["/usr/bin/podman", "image", "inspect"], "timeout"), "native_read"),
             (fsid_directory_chowned_first, "cluster_directory")]


@pytest.mark.parametrize("condition,site", TRANSIENT, ids=[condition.__name__ for condition, unused in TRANSIENT])
def test_a_deploy_step_caught_once_settles_on_the_next_snapshot(core_request, fixture, monkeypatch, condition, site):
    state = Transient(fixture, monkeypatch, condition, times=1, native=production(core_request))
    observed = state.daemons()
    assert state.seen == {0} and state.pauses == [observe.SETTLE_PAUSE]
    assert observed[0]["name"] in ("mgr.host.abcdef", "alloy.node-1") and observed[-1]["state"] == "running"


def production(core_request):
    return ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())


def worker(core_request, monkeypatch, fixture, command, **observation):
    """A worker whose observation reads the synthetic host through the real settle loop."""
    native = ceph_native.Worker(core_request, core_request["bootstrap"], markers=phase_fixtures.Markers())
    native.verify_tool = lambda: None
    native.observed_manager_uid = os.getuid
    native.observer_cache_uid = lambda: None
    real = observe.daemons
    monkeypatch.setattr(observe, "daemons", lambda request, uid, unused, **options: real(fixture[2], uid, command, **dict(options, **observation)))
    return native


def shipped():
    return {"ceph_native": inspect.getsource(ceph_native), "ceph_native_observe": inspect.getsource(observe)}


@pytest.mark.parametrize("condition,site", TRANSIENT, ids=[condition.__name__ for condition, unused in TRANSIENT])
def test_a_deploy_step_that_never_settles_is_not_ready_at_its_own_site(core_request, fixture, monkeypatch, condition, site):
    state = Transient(fixture, monkeypatch, condition, times=1000, native=production(core_request))
    native = worker(core_request, monkeypatch, fixture, state.command, clock=state.clock, pause=state.pause, **({"host": state.host} if state.host else {}))
    with pytest.raises(ceph_native.Refusal, match="^not-ready$") as refused:
        native.daemons()
    assert state.seen == set(range(observe.SETTLE_ATTEMPTS)) and state.pauses == [observe.SETTLE_PAUSE] * (observe.SETTLE_ATTEMPTS - 1)
    cause = ceph_native_worker.cause(refused.value)
    assert cause.startswith("not-ready@ceph_native_observe." + site + ":") and ceph_native_worker.attested(cause, shipped())
    assert ceph_core.unsettled(ceph_native.RemoteRefusal("native-refusal:" + cause))


def unreadable_unit_meta(fixture, unused):
    (fixture[1] / "unit.meta").chmod(0)


@pytest.mark.parametrize("contradiction,site", [(foreign_unit_image, "unit_files"), (container_without_its_unit, "daemon"),
                                                (unreadable_unit_meta, "public_file")])
def test_an_unproved_snapshot_names_the_site_that_refused(core_request, fixture, monkeypatch, contradiction, site):
    contradiction(fixture, monkeypatch)
    pauses = []
    native = worker(core_request, monkeypatch, fixture, fixture[-1], pause=pauses.append)
    with pytest.raises(ceph_native.Refusal, match="^native-observation-unproved$") as refused:
        native.daemons()
    cause = ceph_native_worker.cause(refused.value)
    assert pauses == [] and cause.startswith("native-observation-unproved@ceph_native_observe." + site + ":")
    assert ceph_native_worker.attested(cause, shipped()) and "never projected" not in cause


@pytest.mark.parametrize("prefix", [["/usr/bin/systemctl"], ["/usr/bin/podman", "ps"], ["/usr/bin/podman", "image", "inspect"]],
                         ids=["systemctl", "ps", "inspect"])
@pytest.mark.parametrize("kind", ["unparsable", "output"])
def test_unparsable_or_oversized_native_output_refuses_at_once(core_request, fixture, monkeypatch, prefix, kind):
    """Only a native command that failed or timed out converges; output it cannot prove is a format contradiction."""
    state = Transient(fixture, monkeypatch, native_failure(prefix, kind), times=1000, native=production(core_request))
    native = worker(core_request, monkeypatch, fixture, state.command, clock=state.clock, pause=state.pause)
    with pytest.raises(ceph_native.Refusal, match="^native-observation-unproved$") as refused:
        native.daemons()
    cause = ceph_native_worker.cause(refused.value)
    assert state.seen == {0} and state.pauses == [] and ceph_native_worker.attested(cause, shipped()) and "private" not in cause
