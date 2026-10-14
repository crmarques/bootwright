"""Synthetic filesystem proof for confidential originals and manager copies."""

from __future__ import annotations

import contextlib
import copy
import hashlib
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.held_listing import HeldListing


class Markers:
    def __init__(self, path):
        self.path = str(path)
        path.mkdir(mode=0o700)
        self.records = {}

    def read(self, name):
        return self.records.get(name)

    def write(self, name, value):
        assert name not in self.records or self.records[name] == value
        self.records[name] = copy.deepcopy(value)


@pytest.fixture
def owned(tmp_path, monkeypatch):
    markers = Markers(tmp_path / "4c0a4300-aa43-458c-86d7-ac2256d1fc00")
    monkeypatch.setattr(custody, "ROOT_UID", os.geteuid())

    @contextlib.contextmanager
    def directory(path):
        assert path == markers.path
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(descriptor)
            assert info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(custody, "directory", directory)
    scope = {"owner": "apply-example", "fsid": os.path.basename(markers.path), "host": "node-1", "image": "fixture"}
    return markers, scope


def material(slot):
    return {name: ("synthetic private original " + name).encode() for name in custody.SLOTS[slot]}


@pytest.mark.parametrize("slot", sorted(custody.SLOTS))
def test_original_retained_before_effect_and_journals_contain_no_material_or_secret_digest(owned, slot):
    markers, scope = owned
    wanted = material(slot)
    value = custody.retain(markers, slot, scope, wanted)
    with custody.hold(markers, slot, scope) as (ready, retained):
        assert ready == value and retained == wanted
    evidence = custody.canonical(markers.records)
    for data in wanted.values():
        assert data not in evidence and hashlib.sha256(data).hexdigest().encode() not in evidence
    assert custody.retain(markers, slot, scope, wanted) == value


@pytest.mark.parametrize("slot", sorted(custody.SLOTS))
@pytest.mark.parametrize("foreign", ["regular", "fifo", "symlink"])
def test_unbound_foreign_material_refuses_before_any_content_read(owned, slot, foreign, monkeypatch):
    markers, scope = owned
    path = os.path.join(markers.path, slot + ".private")
    if foreign == "fifo":
        os.mkfifo(path, 0o600)
    elif foreign == "symlink":
        os.symlink("foreign", path)
    else:
        with open(path, "wb") as handle:
            handle.write(custody.encode(slot, material(slot)))
        os.chmod(path, 0o600)
    original_open = os.open

    def no_foreign_open(name, *args, **kwargs):
        assert name != slot + ".private", "unbound foreign content opened"
        return original_open(name, *args, **kwargs)

    monkeypatch.setattr(custody.os, "open", no_foreign_open)
    with pytest.raises(custody.CustodyRefusal):
        custody.retain(markers, slot, scope, material(slot))
    assert markers.records == {}


@pytest.mark.parametrize("drift", ["replace", "bytes", "mode", "hardlink", "scope", "inode-bool", "size-float"])
def test_original_drift_refuses_without_readopting_or_rebinding(owned, drift):
    markers, scope = owned
    slot = "initial-mgr-original"
    wanted = material(slot)
    custody.retain(markers, slot, scope, wanted)
    path = os.path.join(markers.path, slot + ".private")
    if drift == "replace":
        stage = path + ".replacement"
        with open(stage, "wb") as handle:
            handle.write(custody.encode(slot, wanted))
        os.chmod(stage, 0o600)
        os.replace(stage, path)
    elif drift == "bytes":
        with open(path, "wb") as handle:
            handle.write(b"replacement")
    elif drift == "mode":
        os.chmod(path, 0o644)
    elif drift == "hardlink":
        os.link(path, path + ".alias")
    elif drift == "scope":
        markers.records[slot + "-ready.json"]["scope"]["host"] = "foreign"
    else:
        field = "inode" if drift == "inode-bool" else "size"
        old = markers.records[slot + "-ready.json"]["file"][field]
        markers.records[slot + "-ready.json"]["file"][field] = True if drift == "inode-bool" else float(old)
    with pytest.raises(custody.CustodyRefusal):
        with custody.hold(markers, slot, scope):
            pytest.fail("drift admitted a private effect")


@pytest.mark.parametrize("written", [False, True])
def test_original_generation_lost_ready_ack_recovers_exact_owned_inode(owned, written):
    markers, scope = owned
    slot = "initial-mgr-original"
    actual_write = markers.write

    def interrupted(name, value):
        if name == slot + "-ready.json":
            if written:
                actual_write(name, value)
            raise OSError("lost completion acknowledgement")
        actual_write(name, value)

    markers.write = interrupted
    with pytest.raises(OSError):
        custody.retain(markers, slot, scope, material(slot))
    inode = os.stat(os.path.join(markers.path, slot + ".private")).st_ino
    markers.write = actual_write
    custody.retain(markers, slot, scope, material(slot))
    assert os.stat(os.path.join(markers.path, slot + ".private")).st_ino == inode


def test_held_original_replacement_is_detected_after_effect(owned):
    markers, scope = owned
    slot = "initial-mgr-original"
    custody.retain(markers, slot, scope, material(slot))
    path = os.path.join(markers.path, slot + ".private")
    with pytest.raises(custody.CustodyRefusal):
        with custody.hold(markers, slot, scope):
            os.unlink(path)
            with open(path, "wb") as handle:
                handle.write(custody.encode(slot, material(slot)))
            os.chmod(path, 0o600)


def copied(owned):
    markers, scope = owned
    uid = os.geteuid()
    directory = custody.manager_directory(markers, uid)
    files = material("transport-original")
    for name, data in files.items():
        custody.materialize(markers, directory, name, data, uid)
    custody.relabel_intent(markers, scope, uid, "fixture:shared:label:s0")
    return markers, scope, directory, files, uid


@pytest.mark.parametrize("transition", ["qualified", "wrong-label", "no-intent", "same-label-second-drift"])
def test_copied_file_ctime_transition_requires_owned_intent_and_exact_qualified_label(owned, monkeypatch, transition):
    markers, scope, directory, files, uid = copied(owned)
    try:
        old_fstat = os.fstat
        target = markers.read("transport-file-ready-identity.json")["inode"]
        changed = {"ctime": 1}

        def relabeled_stat(descriptor):
            info = old_fstat(descriptor)
            if info.st_ino != target:
                return info
            fields = {name: getattr(info, name) for name in ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_size", "st_mtime_ns", "st_ctime_ns",
                                                             "st_nlink")}
            fields["st_ctime_ns"] += changed["ctime"]
            return SimpleNamespace(**fields)

        monkeypatch.setattr(custody.os, "fstat", relabeled_stat)
        label = b"foreign:label:s0\x00" if transition == "wrong-label" else b"fixture:shared:label:s0\x00"
        monkeypatch.setattr(custody.os, "getxattr", lambda descriptor, name: label)
        if transition == "no-intent":
            del markers.records["transport-relabel.json"]
        if transition in ("qualified", "same-label-second-drift"):
            custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0")
            assert markers.read("transport-file-relabeled-identity.json") is not None
            if transition == "same-label-second-drift":
                changed["ctime"] = 2
                with pytest.raises(custody.CustodyRefusal):
                    custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0")
        else:
            with pytest.raises(custody.CustodyRefusal):
                custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0")
    finally:
        os.close(directory)


@pytest.mark.parametrize("foreign", ["regular", "fifo", "symlink"])
def test_unbound_matching_manager_copy_is_refused_without_secret_read(owned, foreign, monkeypatch):
    markers, unused = owned
    directory = custody.manager_directory(markers, os.geteuid())
    name = "identity"
    try:
        if foreign == "fifo":
            os.mkfifo(name, 0o600, dir_fd=directory)
        elif foreign == "symlink":
            os.symlink("foreign", name, dir_fd=directory)
        else:
            with os.fdopen(os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600, dir_fd=directory), "wb") as handle:
                handle.write(material("transport-original")[name])
        original_open = os.open

        def no_foreign_open(path, *args, **kwargs):
            assert path != name, "unbound materialized secret opened"
            return original_open(path, *args, **kwargs)

        monkeypatch.setattr(custody.os, "open", no_foreign_open)
        with pytest.raises(custody.CustodyRefusal):
            custody.materialize(markers, directory, name, material("transport-original")[name], os.geteuid())
    finally:
        os.close(directory)


@pytest.mark.parametrize("boundary", ["directory", "identity"])
def test_unacknowledged_empty_creation_cleans_exact_own_object_on_exception(owned, boundary):
    markers, unused = owned
    actual_write = markers.write
    refused_name = "transport-directory.json" if boundary == "directory" else "transport-file-reserved-identity.json"

    def fail(name, value):
        if name == refused_name:
            raise OSError("injected reservation write failure")
        actual_write(name, value)

    markers.write = fail
    if boundary == "directory":
        with pytest.raises(OSError):
            custody.manager_directory(markers, os.geteuid())
        assert not os.path.lexists(os.path.join(markers.path, "transport"))
    else:
        directory = custody.manager_directory(markers, os.geteuid())
        try:
            with pytest.raises(OSError):
                custody.materialize(markers, directory, "identity", b"private original", os.geteuid())
            assert not custody.exists(directory, "identity")
        finally:
            os.close(directory)


def test_original_reservation_exception_cleans_exact_new_empty_file(owned):
    markers, scope = owned

    def fail(name, value):
        raise OSError("injected reservation failure")

    markers.write = fail
    with pytest.raises(OSError):
        custody.retain(markers, "initial-mgr-original", scope, material("initial-mgr-original"))
    assert not os.path.lexists(os.path.join(markers.path, "initial-mgr-original.private"))


def test_metadata_source_probe_rejects_sibling_symlink_alias_to_transport(owned):
    markers, unused = owned
    directory = custody.manager_directory(markers, os.geteuid())
    os.close(directory)
    sibling = os.path.join(os.path.dirname(markers.path), "foreign-alias")
    os.symlink(os.path.join(markers.path, "transport"), sibling)
    with pytest.raises(custody.CustodyRefusal):
        custody.source_identity(sibling, os.geteuid())
    with pytest.raises(OSError):
        custody.source_identity(sibling + "/identity", os.geteuid())


def test_actual_source_inode_proof_detects_an_unrelated_name_for_same_private_file(owned):
    markers, scope = owned
    custody.retain(markers, "initial-mgr-original", scope, material("initial-mgr-original"))
    alias = os.path.join(os.path.dirname(markers.path), "unrelated-private-alias")
    os.link(os.path.join(markers.path, "initial-mgr-original.private"), alias)
    assert custody.source_identity(alias, os.geteuid()) in custody.protected_identities(markers.path)


@pytest.mark.parametrize("directory_present", [False, True])
def test_readonly_manager_directory_missing_binding_never_creates_or_adopts(owned, monkeypatch, directory_present):
    markers, unused = owned
    path = os.path.join(markers.path, "transport")
    if directory_present:
        os.mkdir(path, 0o700)
    original = copy.deepcopy(markers.records)
    monkeypatch.setattr(custody.os, "mkdir", lambda *args, **kwargs: pytest.fail("readonly proof made a directory"))
    monkeypatch.setattr(custody.os, "chown", lambda *args, **kwargs: pytest.fail("readonly proof changed ownership"))
    monkeypatch.setattr(custody.os, "fsync", lambda *args, **kwargs: pytest.fail("readonly proof published material"))
    markers.write = lambda *args: pytest.fail("readonly proof wrote a binding")
    with pytest.raises(custody.CustodyRefusal):
        custody.manager_directory(markers, os.geteuid(), create=False)
    assert markers.records == original and os.path.exists(path) == directory_present


@pytest.mark.parametrize("transition", ["qualified", "wrong-label", "no-intent", "mtime-drift"])
def test_pending_relabel_observation_is_pure_and_only_mutation_settles(owned, monkeypatch, transition):
    markers, scope, directory, files, uid = copied(owned)
    actual_stat = os.fstat
    target = markers.read("transport-file-ready-identity.json")["inode"]
    changes = {"ctime": 1}

    def relabeled_stat(descriptor):
        info = actual_stat(descriptor)
        if info.st_ino != target:
            return info
        values = {name: getattr(info, name) for name in ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_size", "st_mtime_ns", "st_ctime_ns",
                                                         "st_nlink")}
        values["st_ctime_ns"] += changes["ctime"]
        if transition == "mtime-drift":
            values["st_mtime_ns"] += 1
        return SimpleNamespace(**values)

    monkeypatch.setattr(custody.os, "fstat", relabeled_stat)
    monkeypatch.setattr(custody.os, "getxattr", lambda descriptor, name: b"foreign\x00" if transition == "wrong-label" else b"fixture:shared:label:s0\x00")
    if transition == "no-intent":
        del markers.records["transport-relabel.json"]
    previous = copy.deepcopy(markers.records)
    write = markers.write
    markers.write = lambda *args: pytest.fail("readonly relabel proof wrote a receipt")
    try:
        if transition == "qualified":
            custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0", settle=False)
        else:
            with pytest.raises(custody.CustodyRefusal):
                custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0", settle=False)
        assert markers.records == previous
        if transition == "qualified":
            markers.write = write
            custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0", settle=True)
            assert markers.read("transport-file-relabeled-identity.json") is not None
            changes["ctime"] = 2
            with pytest.raises(custody.CustodyRefusal):
                custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0", settle=False)
    finally:
        os.close(directory)


def test_held_listing_model_is_stale_until_the_descriptor_is_rewound(tmp_path):
    held = HeldListing()
    descriptor = held.open(str(tmp_path), os.O_RDONLY | os.O_DIRECTORY)
    try:
        (tmp_path / "added").mkdir()
        assert held.listdir(descriptor) == []
        assert held.listdir(descriptor) == ["added"]
        (tmp_path / "second").mkdir()
        held.lseek(descriptor, 0, os.SEEK_SET)
        assert held.listdir(descriptor) == ["added", "second"]
    finally:
        held.close(descriptor)


def test_materialized_copies_are_proved_against_the_directory_as_it_is_now(owned, monkeypatch):
    held = HeldListing()
    monkeypatch.setattr(custody, "os", held)
    markers, scope, directory, files, uid = copied(owned)
    try:
        assert held.held[directory] == []
        custody.prove_materialized(markers, directory, scope, files, uid, "fixture:shared:label:s0")
        assert markers.read("transport-ready.json") is None
    finally:
        os.close(directory)


def test_empty_creation_cleanup_does_not_remove_a_directory_that_gained_an_entry(owned, monkeypatch):
    held = HeldListing()
    monkeypatch.setattr(custody, "os", held)
    markers, unused = owned
    actual_write = markers.write

    def fail(name, value):
        if name == "transport-directory.json":
            os.mkdir(os.path.join(markers.path, "transport", "intruder"))
            raise OSError("injected reservation write failure")
        actual_write(name, value)

    markers.write = fail
    with pytest.raises(OSError, match="injected reservation write failure"):
        custody.manager_directory(markers, os.geteuid())
    assert os.listdir(os.path.join(markers.path, "transport")) == ["intruder"]


RUN_SOURCE = "/var/run/ceph/4c0a4300-aa43-458c-86d7-ac2256d1fc00"


@pytest.fixture
def host_root(tmp_path, monkeypatch):
    """RHEL 9's /var/run, a root-owned relative link to ../run, under a synthetic root; the run directory is the image account's."""
    monkeypatch.setattr(custody, "ROOT_UID", os.geteuid())
    root = tmp_path / "host"
    (root / "run/ceph" / os.path.basename(RUN_SOURCE)).mkdir(parents=True)
    (root / "var").mkdir()
    (root / "var/run").symlink_to("../run")
    original = os.open

    def open_in_fixture(path, flags, mode=0o777, **kwargs):
        return original(str(root) if path == "/" else path, flags, mode, **kwargs)

    monkeypatch.setattr(custody.os, "open", open_in_fixture)
    return root


@pytest.mark.parametrize("target", ["../run", "/run"])
def test_bind_source_under_a_root_owned_directory_link_is_the_resolved_directory(host_root, target):
    (host_root / "var/run").unlink()
    (host_root / "var/run").symlink_to(target)
    info = os.stat(host_root / RUN_SOURCE.replace("/var/run/", "run/"))
    assert custody.source_identity(RUN_SOURCE, os.geteuid()) == (info.st_dev, info.st_ino)


@pytest.mark.parametrize("links", [custody.SOURCE_LINKS, custody.SOURCE_LINKS + 1])
def test_bind_source_follows_directory_links_only_within_the_bound(host_root, links):
    (host_root / "var/run").unlink()
    names = ["run"] + ["hop-%d" % index for index in range(1, links)]
    for name, following in zip(names, names[1:] + ["../run"]):
        (host_root / "var" / name).symlink_to(following)
    if links > custody.SOURCE_LINKS:
        with pytest.raises(custody.CustodyRefusal):
            custody.source_identity(RUN_SOURCE, os.geteuid())
    else:
        info = os.stat(host_root / "run/ceph" / os.path.basename(RUN_SOURCE))
        assert custody.source_identity(RUN_SOURCE, os.geteuid()) == (info.st_dev, info.st_ino)


@pytest.mark.parametrize("drift", ["link-owner", "final-link"])
def test_bind_source_refuses_any_other_link(host_root, monkeypatch, drift):
    path = RUN_SOURCE
    if drift == "link-owner":
        monkeypatch.setattr(custody, "ROOT_UID", os.geteuid() + 1)
    elif drift == "final-link":
        (host_root / "run/alias").symlink_to("ceph/" + os.path.basename(RUN_SOURCE))
        path = "/var/run/alias"
    with pytest.raises(custody.CustodyRefusal):
        custody.source_identity(path, os.geteuid())
