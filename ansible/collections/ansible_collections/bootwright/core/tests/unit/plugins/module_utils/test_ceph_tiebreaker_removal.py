"""A provided stretch tiebreaker's removal: proof before effect, one attempt record, each removal, absence and lost replies.

The host's fixed layout lives under a temporary root that `/` resolves to; the daemon inventory and cephadm's rm-cluster are
scripted. What rm-cluster removes here is the footprint specs/ceph.md names, which B488's rehearsal must still read from the
selected build (.agents/knowledge/ceph-stretch-native-facts.md).
"""

from __future__ import annotations

import copy
import hashlib
import os
import shutil
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_stretch as stretch, ceph_tunnel
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_stretch

FSID = test_ceph_stretch.FSID
OTHER_FSID = "9d2c5b1e-0f4a-4c3b-8a6d-7e5f4c3b2a10"
UID = 4242
PUBLIC = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcH"
OPERATOR_LINE = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOperatorOwnKeyOperatorOwnKeyOperatorOwnKeyA operator@example.test"
DIGEST = "d" * 64


def request():
    value = test_ceph_stretch.request()
    tie = value["hosts"][4]
    tie.update(provided=True, knownHostsRef="tie-known-hosts")
    return value


class Markers:
    """The marker directory as records; a hook may lose the reply right after one is written."""

    def __init__(self, records):
        self.records = copy.deepcopy(records)
        self.lose_after = None
        self.path = ceph_native.ROOT + "/" + FSID

    def owned(self):
        return self.records.get("owner.json") is not None

    def read(self, name="owner.json"):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)
        if name == self.lose_after:
            self.lose_after = None
            raise ceph_native.Refusal("native-unknown")


class Host:
    """The tiebreaker's native side: daemons read from its data root, and rm-cluster removing the FSID's footprint."""

    def __init__(self, root, value, markers):
        self.root, self.request, self.markers = root, value, markers
        self.host = next(host for host in value["hosts"] if host["name"] == "node-5")
        self.tool = value["cephadm"]["path"]
        self.commands, self.declared, self.lose = [], [], False
        self.styles = {}

    def path(self, *parts):
        return os.path.join(self.root, *parts)

    def daemons(self):
        directory = self.path("var/lib/ceph", FSID)
        if not os.path.isdir(directory):
            return []
        return [{"style": self.styles.get(name, "cephadm:v1"), "name": name, "fsid": FSID} for name in sorted(os.listdir(directory)) if "." in name]

    def verify_tool(self):
        return {}

    def require_declared_options(self, argv):
        self.declared.append(list(argv))

    def registry_intent(self):
        return ceph_native.Worker.registry_intent(self)

    def command(self, argv, timeout=60, json_result=True):
        assert not json_result and timeout == stretch.RM_CLUSTER_TIMEOUT
        self.commands.append(list(argv))
        assert argv == [self.tool, "rm-cluster", "--fsid", FSID, "--force"]
        for relative in ("var/lib/ceph/" + FSID, "var/log/ceph/" + FSID):
            shutil.rmtree(self.path(relative), ignore_errors=True)
        for relative in ("etc/logrotate.d/ceph-" + FSID, "etc/systemd/system/ceph-" + FSID + "@.service", "etc/systemd/system/ceph-" + FSID + ".target",
                         "etc/systemd/system/ceph.target.wants/ceph-" + FSID + ".target", "etc/ceph/ceph.conf"):
            if os.path.lexists(self.path(relative)):
                os.unlink(self.path(relative))
        if self.lose:
            self.lose = False
            raise ceph_native.Refusal("native-unknown")
        return b""


def write(path, data, mode=0o644):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "wb") as handle:
        handle.write(data)
    os.chmod(path, mode)


@pytest.fixture
def tiebreaker(tmp_path, monkeypatch):
    root = str(tmp_path / "root")
    value = request()
    line = ceph_tunnel.authorization_line(PUBLIC, value["owner"])
    for directory in ("var/lib/ceph/" + FSID + "/mon.node-5", "var/lib/ceph/" + FSID + "/crash.node-5", "var/log/ceph/" + FSID,
                      "etc/systemd/system/ceph.target.wants", "etc/ceph", "home/operator/.ssh"):
        os.makedirs(os.path.join(root, directory), exist_ok=True)
    os.chmod(os.path.join(root, "home/operator/.ssh"), 0o700)
    write(os.path.join(root, "etc/logrotate.d/ceph-" + FSID), b"rotate\n")
    for unit in ("ceph-" + FSID + "@.service", "ceph-" + FSID + ".target", "ceph.target", "sshd.service"):
        write(os.path.join(root, "etc/systemd/system", unit), b"[Unit]\n")
    write(os.path.join(root, "etc/systemd/system/ceph.target.wants/ceph-" + FSID + ".target"), b"[Unit]\n")
    write(os.path.join(root, "etc/ceph/ceph.conf"), ("[global]\nfsid = " + FSID + "\n").encode())
    write(os.path.join(root, "etc/ceph/podman-auth.json"), b'{"auths":{}}\n', 0o600)
    keys = os.path.join(root, "home/operator/.ssh/authorized_keys")
    write(keys, (OPERATOR_LINE + "\n" + line + "\n").encode(), 0o600)
    real_open, real_fstat = os.open, os.fstat
    user = set()

    def opened(path, flags, mode=0o777, *, dir_fd=None):
        if dir_fd is None and isinstance(path, str) and path.startswith("/") and not path.startswith(root):
            path = root + path if path != "/" else root
        return real_open(path, flags, mode, dir_fd=dir_fd)

    def fstat(fd):
        actual = real_fstat(fd)
        fields = {name: getattr(actual, name) for name in dir(actual) if name.startswith("st_")}
        owner = UID if (actual.st_dev, actual.st_ino) in user else 0
        fields.update(st_uid=owner, st_gid=owner)
        return SimpleNamespace(**fields)

    monkeypatch.setattr(os, "open", opened)
    monkeypatch.setattr(os, "fstat", fstat)
    monkeypatch.setattr(os, "fchown", lambda fd, uid, gid: None)
    monkeypatch.setattr(stretch.pwd, "getpwnam", lambda name: SimpleNamespace(pw_uid=UID, pw_gid=UID, pw_dir="/home/operator"))

    def own(path):
        info = os.stat(path, follow_symlinks=False)
        user.add((info.st_dev, info.st_ino))

    for relative in ("home/operator", "home/operator/.ssh", "home/operator/.ssh/authorized_keys"):
        own(os.path.join(root, relative))
    auth = os.stat(os.path.join(root, "etc/ceph/podman-auth.json"))
    metadata = {"dev": auth.st_dev, "inode": auth.st_ino, "uid": 0, "gid": 0, "mode": 0o600, "mtimeNS": auth.st_mtime_ns}
    host = Host(root, value, None)
    intent = host.registry_intent()
    records = {"owner.json": ceph_native.marker_identity(value), "ssh-authorization.json": {"uid": UID, "home": "/home/operator", "line": line},
               "registry-intent.json": intent, "registry-pending.json": {"intent": intent, "metadata": metadata},
               "registry-file.json": {"intent": intent, "metadata": dict(metadata, ctimeNS=auth.st_ctime_ns)}}
    host.markers = Markers(records)
    host.own = own
    return host


def tree(root):
    result = {}
    for directory, names, files in os.walk(root):
        for name in names + files:
            path = os.path.join(directory, name)
            result[os.path.relpath(path, root)] = None if os.path.isdir(path) else open(path, "rb").read()
    return result


def removal(host):
    return stretch.TiebreakerRemoval(host)


def assert_absent(host):
    root = host.root
    for relative in ("var/lib/ceph/" + FSID, "var/log/ceph/" + FSID, "etc/logrotate.d/ceph-" + FSID, "etc/ceph/ceph.conf", "etc/ceph/podman-auth.json",
                     "etc/systemd/system/ceph-" + FSID + ".target"):
        assert not os.path.lexists(os.path.join(root, relative)), relative
    keys = open(os.path.join(root, "home/operator/.ssh/authorized_keys"), "rb").read()
    assert keys == (OPERATOR_LINE + "\n").encode()
    assert stat_mode(os.path.join(root, "home/operator/.ssh/authorized_keys")) == 0o600
    for kept in ("etc/systemd/system/ceph.target", "etc/systemd/system/sshd.service", "var/lib/ceph", "var/log/ceph", "etc/ceph"):
        assert os.path.lexists(os.path.join(root, kept)), kept
    record = {"version": stretch.REMOVAL_VERSION, "owner": host.request["owner"], "fsid": FSID, "host": "node-5"}
    assert host.markers.records[stretch.REMOVAL_ATTEMPT] == host.markers.records[stretch.REMOVAL_COMPLETE] == record
    assert host.markers.records["owner.json"] == ceph_native.marker_identity(host.request)


def stat_mode(path):
    return os.stat(path).st_mode & 0o7777


def test_a_complete_removal_refuses_nothing_and_proves_every_part_absent(tiebreaker):
    assert removal(tiebreaker).observe() == {"removed": False, "attempted": False}
    assert removal(tiebreaker).remove() == {"changed": True, "removed": True, "attempted": True}
    assert tiebreaker.commands == [[tiebreaker.tool, "rm-cluster", "--fsid", FSID, "--force"]] == tiebreaker.declared
    assert all("--zap-osds" not in argv for argv in tiebreaker.commands)
    assert_absent(tiebreaker)
    assert removal(tiebreaker).observe() == {"removed": True, "attempted": True}
    assert removal(tiebreaker).remove() == {"changed": False, "removed": True, "attempted": True}
    assert len(tiebreaker.commands) == 1


@pytest.mark.parametrize("lost", ["attempt", "key", "credential", "rm-cluster", "completion"])
def test_a_lost_reply_at_each_effect_is_observed_first_and_repeated_only_under_its_attempt(tiebreaker, monkeypatch, lost):
    native = removal(tiebreaker)
    if lost == "attempt":
        tiebreaker.markers.lose_after = stretch.REMOVAL_ATTEMPT
    elif lost == "completion":
        tiebreaker.markers.lose_after = stretch.REMOVAL_COMPLETE
    elif lost == "rm-cluster":
        tiebreaker.lose = True
    else:
        method = "remove_key" if lost == "key" else "credential_present"
        original = getattr(stretch.TiebreakerRemoval, method)

        def once(self, *args, **kwargs):
            result = original(self, *args, **kwargs)
            if lost == "credential" and not kwargs.get("remove"):
                return result
            monkeypatch.setattr(stretch.TiebreakerRemoval, method, original)
            raise ceph_native.Refusal("native-unknown")

        monkeypatch.setattr(stretch.TiebreakerRemoval, method, once)
    with pytest.raises(ceph_native.Refusal, match="^native-unknown$"):
        native.remove()
    assert tiebreaker.markers.records[stretch.REMOVAL_ATTEMPT]["fsid"] == FSID
    observed = removal(tiebreaker).observe()
    assert observed == {"removed": lost in ("rm-cluster", "completion"), "attempted": True}
    commands = len(tiebreaker.commands)
    assert removal(tiebreaker).remove()["removed"] is True
    assert len(tiebreaker.commands) == commands + (0 if lost in ("rm-cluster", "completion") else 1)
    assert_absent(tiebreaker)


@pytest.mark.parametrize("foreign", ["owner", "missing-owner", "other-fsid", "daemon-type", "daemon-host", "legacy", "key-line", "credential",
                                     "unrecorded-credential", "linked-conf"])
def test_anything_but_this_clusters_own_footprint_refuses_tiebreaker_foreign_with_nothing_changed(tiebreaker, foreign):
    root = tiebreaker.root
    records = tiebreaker.markers.records
    if foreign == "owner":
        records["owner.json"] = dict(records["owner.json"], cluster="another")
    elif foreign == "missing-owner":
        del records["owner.json"]
    elif foreign == "other-fsid":
        os.makedirs(os.path.join(root, "var/lib/ceph", OTHER_FSID, "mon.node-5"))
    elif foreign == "daemon-type":
        os.makedirs(os.path.join(root, "var/lib/ceph", FSID, "mgr.node-5.abcdef"))
    elif foreign == "daemon-host":
        os.makedirs(os.path.join(root, "var/lib/ceph", FSID, "mon.node-1"))
    elif foreign == "legacy":
        os.makedirs(os.path.join(root, "var/lib/ceph/mon/ceph-node-5"))
    elif foreign == "key-line":
        keys = os.path.join(root, "home/operator/.ssh/authorized_keys")
        content = open(keys, "rb").read().replace(b"restrict,", b"restrict,agent-forwarding,")
        open(keys, "wb").write(content)
    elif foreign == "credential":
        path = os.path.join(root, "etc/ceph/podman-auth.json")
        os.unlink(path)
        write(path, b'{"auths":{}}\n', 0o600)
    elif foreign == "unrecorded-credential":
        for name in ("registry-file.json", "registry-pending.json"):
            del records[name]
    else:
        os.unlink(os.path.join(root, "etc/ceph/ceph.conf"))
        os.symlink("/etc/hosts", os.path.join(root, "etc/ceph/ceph.conf"))
    before, kept = tree(root), copy.deepcopy(records)
    for call in (lambda: removal(tiebreaker).observe(), lambda: removal(tiebreaker).remove()):
        with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
            call()
        assert tree(root) == before and tiebreaker.markers.records == kept and not tiebreaker.commands


def markers(host):
    return os.path.join(host.root, host.markers.path.lstrip("/"))


def unclaimed(host):
    """The tiebreaker an apply refused before its claim reached: no marker records, no key line, no FSID layout."""
    root = host.root
    host.markers.records.clear()
    for relative in ("var/lib/ceph/" + FSID, "var/log/ceph/" + FSID):
        shutil.rmtree(os.path.join(root, relative))
    for relative in ("etc/logrotate.d/ceph-" + FSID, "etc/systemd/system/ceph-" + FSID + "@.service", "etc/systemd/system/ceph-" + FSID + ".target",
                     "etc/systemd/system/ceph.target.wants/ceph-" + FSID + ".target", "etc/ceph/ceph.conf"):
        os.unlink(os.path.join(root, relative))
    write(os.path.join(root, "home/operator/.ssh/authorized_keys"), (OPERATOR_LINE + "\n").encode(), 0o600)
    host.verify_tool = host.registry_intent = None


@pytest.mark.parametrize("left", [None, "", ".ownership-k2v9x1"])
def test_a_tiebreaker_the_apply_never_claimed_is_proved_untouched_with_nothing_written(tiebreaker, left):
    unclaimed(tiebreaker)
    write(os.path.join(tiebreaker.root, "etc/ceph/ceph.conf"), ("[global]\nfsid = " + OTHER_FSID + "\n").encode())
    os.makedirs(os.path.join(tiebreaker.root, "var/lib/ceph", OTHER_FSID, "mon.node-5"))
    if left is not None:
        os.makedirs(markers(tiebreaker))
        if left:
            write(os.path.join(markers(tiebreaker), left), b"", 0o600)
    before = tree(tiebreaker.root)
    assert removal(tiebreaker).observe() == {"removed": True, "attempted": False}
    assert removal(tiebreaker).remove() == {"changed": False, "removed": True, "attempted": False}
    assert tree(tiebreaker.root) == before and tiebreaker.markers.records == {} and not tiebreaker.commands and not tiebreaker.declared


@pytest.mark.parametrize("part", ["data", "log", "logrotate", "unit", "wants", "conf", "daemon", stretch.REMOVAL_ATTEMPT, "ssh-authorization.json",
                                  "sshd-policy.json", "registry-pending.json", "linked-markers", "key-line", "sshd-drop-in", "registry-anchor"])
def test_any_part_of_the_fsid_on_an_unclaimed_tiebreaker_refuses_tiebreaker_foreign_with_nothing_changed(tiebreaker, part):
    unclaimed(tiebreaker)
    root = tiebreaker.root
    if part in ("data", "daemon"):
        os.makedirs(os.path.join(root, "var/lib/ceph", FSID, "mon.node-5" if part == "daemon" else ""))
    elif part == "log":
        os.makedirs(os.path.join(root, "var/log/ceph", FSID))
    elif part == "logrotate":
        write(os.path.join(root, "etc/logrotate.d/ceph-" + FSID), b"rotate\n")
    elif part == "unit":
        write(os.path.join(root, "etc/systemd/system/ceph-" + FSID + "@.service"), b"[Unit]\n")
    elif part == "wants":
        write(os.path.join(root, "etc/systemd/system/ceph.target.wants/ceph-" + FSID + ".target"), b"[Unit]\n")
    elif part == "conf":
        write(os.path.join(root, "etc/ceph/ceph.conf"), ("[global]\nfsid = " + FSID + "\n").encode())
    elif part == "key-line":
        line = ceph_tunnel.authorization_line(PUBLIC, tiebreaker.request["owner"])
        write(os.path.join(root, "home/operator/.ssh/authorized_keys"), (OPERATOR_LINE + "\n" + line + "\n").encode(), 0o600)
    elif part == "sshd-drop-in":
        token = hashlib.sha256((FSID + "\x00" + tiebreaker.request["owner"]).encode()).hexdigest()
        write(os.path.join(root, "etc/ssh/sshd_config.d/05-bootwright-ceph-" + token + ".conf"), b"Match User operator\n")
    elif part == "registry-anchor":
        token = hashlib.sha256((tiebreaker.request["owner"] + "\x00" + FSID).encode()).hexdigest()
        write(os.path.join(root, "etc/ceph/.bootwright-registry-" + token), b"{}", 0o600)
    elif part == "linked-markers":
        os.makedirs(os.path.join(root, "var/lib/bootwright/ceph"))
        os.symlink("/etc", markers(tiebreaker))
    else:
        write(os.path.join(markers(tiebreaker), part), b"{}", 0o600)
    before, kept = tree(root), copy.deepcopy(tiebreaker.markers.records)
    for call in (lambda: removal(tiebreaker).observe(), lambda: removal(tiebreaker).remove()):
        with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
            call()
        assert tree(root) == before and tiebreaker.markers.records == kept and not tiebreaker.commands


def test_the_controller_accepts_an_unchanged_removal_with_no_attempt_and_refuses_a_changed_one():
    value = request()
    result = ceph_core.execute(value, "destroy", DIGEST, Transport({"changed": False, "removed": True, "attempted": False}), retirement="tiebreaker")
    assert result["changed"] is False and result["evidence"]["removed"] is True and result["evidence"]["attempted"] is False
    with pytest.raises(ceph_native.Refusal, match="^not-removed$"):
        ceph_core.execute(value, "destroy", DIGEST, Transport({"changed": True, "removed": True, "attempted": False}), retirement="tiebreaker")


def test_a_cephadm_parser_without_the_removal_options_refuses_before_the_attempt(tiebreaker):
    def undeclared(argv):
        raise ceph_native.NativeFailure("native-option-undeclared", "coordinate", b"--force")

    tiebreaker.require_declared_options = undeclared
    before, kept = tree(tiebreaker.root), copy.deepcopy(tiebreaker.markers.records)
    with pytest.raises(ceph_native.Refusal, match="^native-option-undeclared$"):
        removal(tiebreaker).remove()
    assert tree(tiebreaker.root) == before and tiebreaker.markers.records == kept and not tiebreaker.commands


def test_only_the_provided_stretch_tiebreaker_runs_a_removal(tiebreaker):
    for change in (lambda host: host.pop("provided"), lambda host: host.update(name="node-6")):
        other = copy.deepcopy(tiebreaker)
        change(other.host)
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            stretch.TiebreakerRemoval(other)


class Transport:
    def __init__(self, answer):
        self.answer, self.calls = answer, []

    def call(self, host, phase, private=None, **unused):
        self.calls.append((host["name"], phase, private))
        if isinstance(self.answer, Exception):
            raise self.answer
        return self.answer


@pytest.mark.parametrize("operation,answer,removed", [("destroy", {"changed": True, "removed": True, "attempted": True}, True),
                                                      ("observe", {"removed": False, "attempted": True}, False)])
def test_the_controller_reaches_only_the_tiebreaker_and_publishes_closed_evidence(operation, answer, removed):
    value = request()
    transport = Transport(answer)
    result = ceph_core.execute(value, operation, DIGEST, transport, retirement="tiebreaker")
    phase = "tiebreaker_remove" if operation == "destroy" else "tiebreaker_observe"
    assert transport.calls == [("node-5", phase, None)]
    assert result["evidence"] == {"version": ceph_core.TIEBREAKER_EVIDENCE, "request": DIGEST, "owner": value["owner"], "fsid": FSID, "host": "node-5",
                                  "removed": removed, "attempted": True}


def test_an_unreachable_tiebreaker_is_unknown_never_absent():
    with pytest.raises(ceph_native.Refusal) as refused:
        ceph_core.execute(request(), "observe", DIGEST, Transport(ceph_native.Refusal("transport")), retirement="tiebreaker")
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == "tiebreaker-unreachable"
    remote = ceph_native.RemoteRefusal("native-refusal:tiebreaker-foreign@fixed")
    with pytest.raises(ceph_native.RemoteRefusal) as refused:
        ceph_core.execute(request(), "destroy", DIGEST, Transport(remote), retirement="tiebreaker")
    assert ceph_core.refusal(refused.value, ceph_core.CLUSTER_REASONS)[0] == "tiebreaker-foreign"
    with pytest.raises(ceph_native.Refusal, match="^not-removed$"):
        ceph_core.execute(request(), "destroy", DIGEST, Transport({"changed": True, "removed": False, "attempted": True}), retirement="tiebreaker")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(request(), "destroy", DIGEST, Transport({}), retirement="host")
    unprovided = request()
    del unprovided["hosts"][4]["provided"], unprovided["hosts"][4]["knownHostsRef"]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(unprovided, "destroy", DIGEST, Transport({}), retirement="tiebreaker")


def test_the_removal_phases_are_fixed_and_only_the_observation_is_read_only():
    assert {"tiebreaker_observe", "tiebreaker_remove"} <= ceph_native.CORE_PHASES
    assert "tiebreaker_observe" in ceph_native.CORE_READ_ONLY_PHASES and "tiebreaker_remove" not in ceph_native.CORE_READ_ONLY_PHASES
    assert {"tiebreaker-foreign", "tiebreaker-unreachable", "not-removed"} <= set(ceph_core.CLUSTER_REASONS)


def test_concurrent_authorized_keys_edit_is_preserved_before_rename(tiebreaker, monkeypatch):
    native = removal(tiebreaker)
    directory = tiebreaker.path("home/operator/.ssh")
    path = os.path.join(directory, "authorized_keys")
    with open(path, "rb") as handle:
        original = handle.read()
    concurrent = original + b"# concurrent operator edit\n"
    real_stat = os.stat
    reached = []

    def changed(name, *args, **kwargs):
        if name == "authorized_keys" and any(item.startswith(".bootwright-") for item in os.listdir(directory)):
            with open(path, "wb") as handle:
                handle.write(concurrent)
            reached.append(name)
        return real_stat(name, *args, **kwargs)

    monkeypatch.setattr(os, "stat", changed)
    with pytest.raises(ceph_native.Refusal, match="^tiebreaker-foreign$"):
        native.remove()
    assert reached == ["authorized_keys"]
    with open(path, "rb") as handle:
        assert handle.read() == concurrent
    assert not any(item.startswith(".bootwright-") for item in os.listdir(directory))
    assert stretch.REMOVAL_ATTEMPT in tiebreaker.markers.records
    assert stretch.REMOVAL_COMPLETE not in tiebreaker.markers.records and not tiebreaker.commands


def physical_removal(host):
    host.host.pop("provided", None)
    host.host.pop("knownHostsRef", None)
    host.host.update(physical=True, retainRoot=True)
    host.markers.records["owner.json"] = ceph_native.marker_identity(host.request)
    return stretch.PhysicalHostRemoval(host)


def test_retained_physical_root_removes_owned_fsid_daemons_without_zapping(tiebreaker):
    host = tiebreaker
    os.makedirs(host.path("var/lib/ceph", FSID, "osd.7"))
    os.makedirs(host.path("var/lib/ceph", FSID, "mgr.node-5.a"))
    cleanup = physical_removal(host)
    assert cleanup.observe()["removed"] is False
    assert cleanup.remove() == {"changed": True, "removed": True, "attempted": True}
    assert cleanup.observe()["removed"] is True
    assert cleanup.remove()["changed"] is False
    assert host.commands == [[host.tool, "rm-cluster", "--fsid", FSID, "--force"]]
    assert "--zap-osds" not in host.commands[0]
    assert open(host.path("home/operator/.ssh/authorized_keys"), encoding="utf-8").read() == OPERATOR_LINE + "\n"


def test_retained_physical_root_never_removes_foreign_fsid(tiebreaker):
    host = tiebreaker
    os.makedirs(host.path("var/lib/ceph", OTHER_FSID))
    with pytest.raises(ceph_native.Refusal, match="tiebreaker-foreign"):
        physical_removal(host).remove()
    assert not host.commands and stretch.REMOVAL_ATTEMPT not in host.markers.records


def test_retained_root_recovers_a_lost_native_cleanup_reply(tiebreaker):
    host = tiebreaker
    host.lose = True
    cleanup = physical_removal(host)
    with pytest.raises(ceph_native.Refusal, match="native-unknown"):
        cleanup.remove()
    assert cleanup.observe()["removed"] is True
    assert cleanup.remove()["changed"] is False and len(host.commands) == 1
