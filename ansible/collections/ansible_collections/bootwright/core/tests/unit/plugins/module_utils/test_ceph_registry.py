"""Native credentials stay on protected stdin and an exclusively owned file.

Root metadata is injected over a temporary filesystem; these tests never
contact a registry, invoke Podman, or qualify a provider artifact.
"""

from __future__ import annotations

import base64
import copy
import json
import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native


@pytest.fixture
def cluster_request():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i),
              "user": "operator", "port": 22, "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}],
              "labels": [], "location": {}, "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v7", "action": "cluster", "cluster": "example", "owner": "apply-example",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "9.1", "ansiblePackageVersion": "3.2.0",
            "packageRepository": "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64",
            "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator",
            "clusterSSHTransport": "verified-tunnel", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
            "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}},
                         {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


class Markers:
    def __init__(self):
        self.records = {}
        self.writes = []
        self.is_owned = True
        self.before = lambda name, value: None
        self.after = lambda name, value: None

    def owned(self):
        return self.is_owned

    def read(self, name):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        self.before(name, value)
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)
        self.after(name, value)


@pytest.fixture
def registry(cluster_request, tmp_path, monkeypatch):
    root = tmp_path / "root"
    root.mkdir(mode=0o755)
    (root / "etc").mkdir(mode=0o755)
    original_open, original_fstat = os.open, os.fstat

    def open_file(path, flags, mode=0o777, *, dir_fd=None):
        if path == "/" and dir_fd is None:
            path = str(root)
        return original_open(path, flags, mode, dir_fd=dir_fd)

    def fstat(fd):
        actual = original_fstat(fd)
        fields = {name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}
        fields.update(st_uid=0, st_gid=0)
        return SimpleNamespace(**fields)

    monkeypatch.setattr(ceph_native.os, "open", open_file)
    monkeypatch.setattr(ceph_native.os, "fstat", fstat)
    monkeypatch.setattr(ceph_native.os, "fchown", lambda fd, uid, gid: ceph_native.require(uid == gid == 0))
    markers = Markers()

    def forbidden(*args, **kwargs):
        del args, kwargs
        pytest.fail("registry custody reached an uninjected provider command")

    native = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=forbidden, markers=markers)
    material = ceph_native.registry_material(cluster_request,
                                             json.dumps({"url": cluster_request["registry"], "username": "synthetic-user", "password": "synthetic-password"}))
    return native, root / "etc" / "ceph", material


def teardown(native):
    native.markers.records["native-teardown.json"] = {"version": "ceph-native-teardown-v1", "owner": native.request["owner"],
                                                      "fsid": native.request["fsid"], "host": native.host["name"], "removed": True}
    native.daemons = lambda: []


def test_authfile_publication_has_only_nonsecret_custody_records(registry):
    native, directory, material = registry
    assert native.prepare_registry(material)
    native.prove_registry(material)
    assert not native.prepare_registry(material)
    assert (directory / ceph_native.REGISTRY_NAME).read_bytes() == material
    assert (directory / ceph_native.REGISTRY_NAME).stat().st_mode & 0o777 == 0o600
    assert native.markers.writes == ["registry-intent.json", "registry-pending.json", "registry-file.json"]
    assert not list(directory.glob(".bootwright-registry-*"))
    records = json.dumps(native.markers.records)
    assert "synthetic-user" not in records and "synthetic-password" not in records and "auths" not in records
    assert base64.b64encode(b"synthetic-user:synthetic-password").decode() not in records
    assert "sha256" not in records and "hash" not in records.lower()
    assert set(native.markers.records["registry-pending.json"]["metadata"]) == {"dev", "inode", "uid", "gid", "mode", "mtimeNS"}
    assert set(native.markers.records["registry-file.json"]["metadata"]) == {"dev", "inode", "uid", "gid", "mode", "mtimeNS", "ctimeNS"}


@pytest.mark.parametrize("value", [None, "", "{", "[]", '{"url":"registry.example.test","url":"registry.example.test","username":"u","password":"p"}',
                                   '{"url":"registry.example.test","username":"u","password":"p","extra":true}',
                                   '{"url":"https://registry.example.test","username":"u","password":"p"}',
                                   '{"url":"registry.example.test","username":"u:p","password":"p"}',
                                   '{"url":"registry.example.test","username":"u","password":""}',
                                   '{"url":"registry.example.test","username":"u","password":"p\\nq"}'])
def test_protected_registry_input_is_strict_and_errors_contain_no_values(cluster_request, value):
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.registry_material(cluster_request, value)


@pytest.mark.parametrize("authority", ["https://registry.example.test", "registry.example.test/path", "user@registry.example.test", "registry.example.test:0",
                                       "registry.example.test:65536", "Registry.example.test", "registry..example.test", "registry.example.test."])
def test_registry_aliases_are_not_custody_scopes(cluster_request, authority):
    cluster_request["registry"] = authority
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.registry_material(cluster_request, json.dumps({"url": authority, "username": "u", "password": "p"}))


@pytest.mark.parametrize("change", ["helper", "alias", "foreign", "empty", "token", "bad-base64", "bad-credential", "duplicate"])
def test_current_authfile_scope_rejects_foreign_entries_and_aliases(cluster_request, change):
    entry = {"auth": base64.b64encode(b"u:p").decode()}
    value = {"auths": {cluster_request["registry"]: entry}}
    if change == "helper":
        value["credsStore"] = "ambient-helper"
    elif change == "alias":
        value["auths"] = {"https://" + cluster_request["registry"]: entry}
    elif change == "foreign":
        value["auths"]["foreign.example.test"] = entry
    elif change == "empty":
        value["auths"] = {}
    elif change == "token":
        entry["identitytoken"] = "foreign-token"
    elif change == "bad-base64":
        entry["auth"] = "??"
    elif change == "bad-credential":
        entry["auth"] = base64.b64encode(b"no-colon").decode()
    data = ceph_native.canonical(value)
    if change == "duplicate":
        data = b'{"auths":{"registry.example.test":{"auth":"dTpw","auth":"dTpw"}}}'
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        ceph_native.registry_scope(cluster_request, data)


@pytest.mark.parametrize("entry", ["file", "symlink", "hardlink", "mode", "anchor", "intent-only"])
def test_preexisting_authfile_never_adopted_by_matching_bytes(registry, entry, tmp_path):
    native, directory, material = registry
    directory.mkdir()
    target = directory / ceph_native.REGISTRY_NAME
    if entry == "anchor":
        target = directory / native.registry_intent()["anchor"]
    if entry == "symlink":
        foreign = tmp_path / "foreign"
        foreign.write_bytes(material)
        target.symlink_to(foreign)
    else:
        target.write_bytes(material)
        target.chmod(0o644 if entry == "mode" else 0o600)
        if entry == "hardlink":
            os.link(target, tmp_path / "alias")
        if entry == "intent-only":
            native.markers.records["registry-intent.json"] = native.registry_intent()
    previous = target.read_bytes()
    with pytest.raises((ceph_native.Refusal, OSError)):
        native.prepare_registry(material)
    assert target.read_bytes() == previous
    assert native.markers.writes == []


@pytest.mark.parametrize("name", ["authfile", "anchor"])
@pytest.mark.parametrize("bound", [False, True])
def test_foreign_fifo_refuses_without_a_blocking_open_or_content_read(registry, monkeypatch, name, bound):
    native, directory, material = registry
    directory.mkdir()
    filename = ceph_native.REGISTRY_NAME if name == "authfile" else native.registry_intent()["anchor"]
    os.mkfifo(directory / filename, 0o600)
    if bound:
        fd = os.open(str(directory / filename), os.O_RDONLY | os.O_NONBLOCK)
        try:
            metadata = ceph_native.registry_metadata(os.fstat(fd), pending=True)
        finally:
            os.close(fd)
        intent = native.registry_intent()
        native.markers.records.update({"registry-intent.json": intent, "registry-pending.json": {"intent": intent, "metadata": metadata}})
    original_open = os.open
    opened = []

    def open_file(path, flags, *args, **kwargs):
        if path == filename:
            assert flags & os.O_NONBLOCK
            opened.append(path)
        return original_open(path, flags, *args, **kwargs)

    monkeypatch.setattr(ceph_native.os, "open", open_file)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_registry(material)
    assert opened == ([filename] if bound else [])
    assert native.markers.writes == []


@pytest.mark.parametrize("state", ["unbound", "pending", "ready"])
def test_foreign_regular_secret_is_never_read_before_custody_refusal(registry, monkeypatch, state):
    native, directory, material = registry
    if state == "unbound":
        directory.mkdir()
    else:
        native.prepare_registry(material)
        if state == "pending":
            native.markers.records.pop("registry-file.json")
    foreign = directory / "replacement"
    foreign.write_bytes(material)
    foreign.chmod(0o600)
    os.replace(foreign, directory / ceph_native.REGISTRY_NAME)
    opened = []
    fdopen = os.fdopen

    class NoSecretRead:
        def __init__(self, fd, mode):
            self.handle = fdopen(fd, mode)
            opened.append(fd)

        def __enter__(self):
            return self

        def __exit__(self, *args):
            return self.handle.__exit__(*args)

        def fileno(self):
            return self.handle.fileno()

        def read(self, *args):
            del args
            pytest.fail("foreign secret content was read before custody proof")

    monkeypatch.setattr(ceph_native.os, "fdopen", NoSecretRead)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.registry_preflight(material)
    assert len(opened) == (0 if state == "unbound" else 1)


@pytest.mark.parametrize("component", ["etc", "ceph"])
def test_registry_directory_aliases_and_writable_parents_refuse(registry, component, tmp_path):
    native, directory, material = registry
    target = directory.parent if component == "etc" else directory
    if component == "ceph":
        target.mkdir()
    target.chmod(0o777)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_registry(material)
    assert native.markers.writes == []


@pytest.mark.parametrize("state", ["anchor", "linked", "published"])
def test_pending_inode_recovers_each_global_publication_interruption(registry, monkeypatch, state):
    native, directory, material = registry
    link, fsync = os.link, os.fsync
    linked = False

    def interrupt_link(source, target, **kwargs):
        nonlocal linked
        assert native.markers.read("registry-pending.json") is not None
        if state == "anchor":
            raise OSError("synthetic interruption")
        result = link(source, target, **kwargs)
        linked = True
        return result

    def interrupt_sync(fd):
        if state == "linked" and linked:
            raise OSError("synthetic interruption")
        return fsync(fd)

    def interrupt_record(name, value):
        del value
        if state == "published" and name == "registry-file.json":
            raise OSError("synthetic interruption")

    monkeypatch.setattr(ceph_native.os, "link", interrupt_link)
    monkeypatch.setattr(ceph_native.os, "fsync", interrupt_sync)
    native.markers.before = interrupt_record
    with pytest.raises(OSError):
        native.prepare_registry(material)
    pending = native.markers.read("registry-pending.json")
    assert pending is not None and native.markers.read("registry-file.json") is None
    files = list(directory.iterdir())
    assert len(files) == (2 if state == "linked" else 1)
    assert all(path.stat().st_ino == pending["metadata"]["inode"] for path in files)
    monkeypatch.setattr(ceph_native.os, "link", link)
    monkeypatch.setattr(ceph_native.os, "fsync", fsync)
    native.markers.before = lambda name, value: None
    assert native.prepare_registry(material)
    native.prove_registry(material)
    assert not list(directory.glob(".bootwright-registry-*"))


def test_failed_pending_journal_cleans_only_its_unbound_anchor(registry):
    native, directory, material = registry

    def refuse(name, value):
        del value
        if name == "registry-pending.json":
            raise OSError("synthetic interruption")

    native.markers.before = refuse
    with pytest.raises(OSError):
        native.prepare_registry(material)
    assert native.markers.read("registry-intent.json") is not None
    assert list(directory.iterdir()) == []
    native.markers.before = lambda name, value: None
    assert native.prepare_registry(material)


def test_durable_pending_journal_is_preserved_when_acknowledgment_is_lost(registry):
    native, directory, material = registry

    def refuse(name, value):
        del value
        if name == "registry-pending.json":
            raise OSError("synthetic lost acknowledgment")

    native.markers.after = refuse
    with pytest.raises(OSError):
        native.prepare_registry(material)
    assert native.markers.read("registry-pending.json") is not None
    assert (directory / native.registry_intent()["anchor"]).exists()
    native.markers.after = lambda name, value: None
    assert native.prepare_registry(material)


@pytest.mark.parametrize("mutation", ["replacement", "bytes", "metadata", "hardlink", "record", "missing", "material"])
def test_ready_reproof_refuses_any_custody_or_protected_input_drift(registry, mutation, tmp_path):
    native, directory, material = registry
    native.prepare_registry(material)
    target = directory / ceph_native.REGISTRY_NAME
    if mutation == "replacement":
        replacement = directory / "replacement"
        replacement.write_bytes(material)
        replacement.chmod(0o600)
        os.replace(replacement, target)
    elif mutation == "bytes":
        target.write_bytes(material + b" ")
    elif mutation == "metadata":
        info = target.stat()
        os.utime(target, ns=(info.st_atime_ns, info.st_mtime_ns + 1))
    elif mutation == "hardlink":
        os.link(target, tmp_path / "foreign-link")
    elif mutation == "record":
        native.markers.records["registry-file.json"]["intent"]["host"] = "foreign-host"
    elif mutation == "missing":
        target.unlink()
    elif mutation == "material":
        material = material.replace(b"auths", b"other")
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prove_registry(material)


@pytest.mark.parametrize("field,value", [("st_uid", 1000), ("st_gid", 1000), ("st_mode", 0o100644)])
def test_current_authfile_requires_root_ownership_and_exact_private_mode(registry, monkeypatch, field, value):
    native, directory, material = registry
    native.prepare_registry(material)
    inode = (directory / ceph_native.REGISTRY_NAME).stat().st_ino
    original_fstat = os.fstat

    def fstat(fd):
        info = original_fstat(fd)
        if info.st_ino == inode:
            setattr(info, field, value)
        return info

    monkeypatch.setattr(ceph_native.os, "fstat", fstat)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prove_registry()


@pytest.mark.parametrize("record", ["registry-pending.json", "registry-file.json"])
@pytest.mark.parametrize("mutation", ["uid-bool", "dev-float", "missing", "extra", "negative", "zero-inode", "huge-inode", "wrong-mode", "bad-time"])
def test_custody_records_have_closed_integer_metadata_before_file_effect(registry, record, mutation):
    native, directory, material = registry
    native.prepare_registry(material)
    value = native.markers.records[record]["metadata"]
    if mutation == "uid-bool":
        value["uid"] = False
    elif mutation == "dev-float":
        value["dev"] = float(value["dev"])
    elif mutation == "missing":
        value.pop("gid")
    elif mutation == "extra":
        value["secretHash"] = "forbidden-digest"
    elif mutation == "negative":
        value["dev"] = -1
    elif mutation == "zero-inode":
        value["inode"] = 0
    elif mutation == "huge-inode":
        value["inode"] = 1 << 64
    elif mutation == "wrong-mode":
        value["mode"] = 0o644
    elif mutation == "bad-time":
        value["mtimeNS"] = 1 << 63
    previous = (directory / ceph_native.REGISTRY_NAME).read_bytes()
    for operation in (native.prove_registry, lambda: native.prepare_registry(material)):
        with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
            operation()
    assert (directory / ceph_native.REGISTRY_NAME).read_bytes() == previous


def test_pending_recovery_refuses_byte_identical_replacement_inode(registry, monkeypatch):
    native, directory, material = registry

    def refuse(name, value):
        del value
        if name == "registry-file.json":
            raise OSError("synthetic interruption")

    native.markers.before = refuse
    with pytest.raises(OSError):
        native.prepare_registry(material)
    replacement = directory / "replacement"
    replacement.write_bytes(material)
    replacement.chmod(0o600)
    os.replace(replacement, directory / ceph_native.REGISTRY_NAME)
    native.markers.before = lambda name, value: None
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.prepare_registry(material)


@pytest.mark.parametrize("keys", [["mgr/cephadm/registry_credentials"], ["x", "x"], {"registry_credentials": "synthetic-password"}, [False]])
def test_provider_stored_credentials_refuse_before_enrollment_or_service_effect(registry, keys):
    native, unused, material = registry
    native.prepare_registry(material)
    calls = []

    def ceph(*argv, **kwargs):
        del kwargs
        calls.append(argv)
        if argv == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if argv == ("config-key", "ls"):
            return keys
        pytest.fail("stored credential refusal reached an orchestrator effect")

    native.ceph = ceph
    for method in (native.enroll, native.services):
        with pytest.raises(ceph_native.Refusal):
            method()
    assert calls == [("fsid",), ("config-key", "ls")] * 2


@pytest.mark.parametrize("mode", ["native", "verified-tunnel"])
def test_bootstrap_refuses_registry_material_before_scratch_or_native_effect(registry, monkeypatch, mode):
    native, unused, material = registry
    native.prepare_registry(material)
    writes = list(native.markers.writes)
    native.request["clusterSSHTransport"] = mode
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    private = {"clusterPrivateKey": "private-key", "clusterPublicKey": "public-key", "knownHosts": "host-keys", "registryJSON": "SYNTHETIC-SECRET-SENTINEL"}
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        native.bootstrap(private)
    assert native.markers.writes == writes


@pytest.mark.parametrize("state", ["no-proof", "wrong-proof", "untyped-proof", "live-native", "foreign-file"])
def test_cleanup_requires_durable_native_teardown_and_current_exact_custody(registry, state):
    native, directory, material = registry
    native.prepare_registry(material)
    teardown(native)
    if state == "no-proof":
        del native.markers.records["native-teardown.json"]
    elif state == "wrong-proof":
        native.markers.records["native-teardown.json"]["fsid"] = "foreign-incarnation"
    elif state == "untyped-proof":
        native.markers.records["native-teardown.json"]["removed"] = 1
    elif state == "live-native":
        native.daemons = lambda: [{"name": "mon.node-1"}]
    elif state == "foreign-file":
        (directory / ceph_native.REGISTRY_NAME).write_bytes(b'{"auths":{"foreign.example.test":{"auth":"dTpw"}}}')
    with pytest.raises(ceph_native.Refusal):
        native.remove_registry()
    assert (directory / ceph_native.REGISTRY_NAME).exists()
    assert native.markers.read("registry-removal.json") is None


def test_owned_cleanup_and_lost_completion_are_exact_and_idempotent(registry):
    native, directory, material = registry
    native.prepare_registry(material)
    teardown(native)

    def refuse(name, value):
        del value
        if name == "registry-removed.json":
            raise OSError("synthetic interruption")

    native.markers.before = refuse
    with pytest.raises(OSError):
        native.remove_registry()
    assert not (directory / ceph_native.REGISTRY_NAME).exists()
    assert native.markers.read("registry-removal.json") == native.markers.read("registry-file.json")
    native.markers.before = lambda name, value: None
    assert native.remove_registry()
    assert not native.remove_registry()


def test_foreign_registry_cannot_appear_during_cleanup_recovery(registry):
    native, directory, material = registry
    native.prepare_registry(material)
    teardown(native)
    native.markers.records["registry-removal.json"] = native.markers.read("registry-file.json")
    (directory / ceph_native.REGISTRY_NAME).unlink()
    foreign = directory / ceph_native.REGISTRY_NAME
    foreign.write_bytes(material)
    foreign.chmod(0o600)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.remove_registry()
    assert foreign.exists()


class DeviceRead(Exception):
    pass


def reach_devices(native):
    def command(*args, **kwargs):
        del args, kwargs
        raise DeviceRead()

    native.command = command


@pytest.mark.parametrize("journal", ["claimed", "intent-only"])
def test_a_claimed_host_whose_authorization_never_published_its_registry_still_reads_its_devices(registry, journal):
    native, unused_directory, unused_material = registry
    if journal == "intent-only":
        native.markers.records["registry-intent.json"] = native.registry_intent()
    reach_devices(native)
    with pytest.raises(DeviceRead):
        native.devices()


def test_a_published_registry_is_reproved_before_any_device_read(registry):
    native, directory, material = registry
    native.prepare_registry(material)
    (directory / ceph_native.REGISTRY_NAME).write_bytes(material + b" ")
    reach_devices(native)
    with pytest.raises(ceph_native.Refusal, match="^registry-ownership$"):
        native.devices()
