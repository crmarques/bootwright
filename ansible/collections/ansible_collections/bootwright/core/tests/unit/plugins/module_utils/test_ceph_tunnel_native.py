"""Candidate integration keeps effects behind current ownership and tunnel proof.

Injected provider responses are protocol fixtures, never image qualification.
No test provisions a guest, invokes SSHD or contacts a registry.
"""

from __future__ import annotations

import base64
import copy
import contextlib
import hashlib
import itertools
import json
import os
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime, ceph_native, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody
from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils import native_specializations
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_native_specializations as specs

# The frozen replicated rbd pool the Go side sends with every optional member unset.
POOL = {"application": "rbd", "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
        "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}, "crushRule": "replicated_rule", "filesystem": "",
        "minSize": 2, "name": "volumes", "overwrites": False, "pgNum": 32, "quota": {"maxBytes": 0, "maxObjects": 0}, "role": "rbd", "size": 2,
        "type": "replicated"}


def public_key():
    algorithm = b"ssh-ed25519"
    value = len(algorithm).to_bytes(4, "big") + algorithm + (32).to_bytes(4, "big") + b"a" * 32
    return "ssh-ed25519 " + base64.b64encode(value).decode()


@pytest.fixture
def cluster_request():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i),
              "user": "operator", "port": 22, "devices": [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}],
              "labels": [], "location": {}, "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v11", "action": "cluster", "cluster": "example", "owner": "apply-example",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible",
            "packageRepository": "https://packages.example.test/native", "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator",
            "clusterSSHTransport": "verified-tunnel", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
            "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}},
                         {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}


class Markers:
    def __init__(self, path):
        self.path = str(path)
        path.mkdir(mode=0o700)
        self.records = {}
        self.writes = []

    def owned(self):
        return True

    def read(self, name):
        return self.records.get(name)

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)


@pytest.fixture
def native(cluster_request, tmp_path, monkeypatch):
    markers = Markers(tmp_path / cluster_request["fsid"])
    monkeypatch.setattr(ceph_tunnel_staging, "ROOT", str(tmp_path))
    monkeypatch.setattr(ceph_tunnel_custody, "ROOT_UID", os.geteuid())

    @contextlib.contextmanager
    def private_directory(path):
        assert path == markers.path
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(ceph_tunnel_custody, "directory", private_directory)

    def forbidden(*args, **kwargs):
        del args, kwargs
        pytest.fail("test reached an uninjected provider command")

    worker = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=forbidden, markers=markers)
    worker.command = forbidden
    worker.ceph = forbidden
    worker.tunnel_effect = lambda *args, **kwargs: worker.ceph(*args, **kwargs)  # pylint: disable=unnecessary-lambda
    # Actual authfile custody is exercised by test_ceph_registry.py.
    worker.prove_registry = lambda wanted=None: None
    worker.provider_registry_absent = lambda: None
    worker.selection = built(cluster_request["cephadm"]["packageVersion"])
    return worker


@pytest.fixture
def private(cluster_request):
    known = "".join(host["address"] + " " + public_key() + "\n" for host in cluster_request["hosts"])
    return {"clusterPrivateKey": "private fixture", "clusterPublicKey": public_key(), "knownHosts": known,
            "registryJSON": json.dumps({"url": cluster_request["registry"], "username": "fixture-user", "password": "fixture-password"})}


def ssh_material(private):
    return {key: private[key] for key in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")}


def staged(native):
    intent = ceph_tunnel_staging.stage_intent(native.request)
    native.markers.records["tunnel-stage.json"] = intent
    return intent


def initial_daemons(native):
    intent = ceph_tunnel_staging.stage_intent(native.request)
    return [{"fsid": native.request["fsid"], "name": kind + "." + intent[kind + "ID"], "style": "cephadm:v1",
             "container_image_name": native.request["image"], "state": "running", "enabled": True, "container_id": "b" * 64}
            for kind in ("mon", "mgr")]


def metadata(kind):
    return ceph_native.canonical({"service_name": kind, "ports": [] if kind == "mon" else [8765, 9283],
                                  "memory_request": None, "memory_limit": None})


def built(host_build, image_build=None):
    """Seams answered by synthetic zipapps holding a signed build's declared names; the sources' own proofs are exercised below."""
    host, image = (specs.synthesized(value) if isinstance(value, str) else value for value in (host_build, image_build or host_build))
    return native_specializations.Selection({"host-cephadm": lambda: host, "image-cephadm": lambda: image})


def prepared_bootstrap(native, private):
    native.markers.records["bootstrap-scope.json"] = ceph_tunnel_staging.recovery_scope(native.request)
    native.selection = built(native.request["cephadm"]["packageVersion"])
    native.prove_preparation = lambda: None
    native.prove_bootstrap_absence = lambda: None
    native.prove_inert_host_preparation = lambda: None
    native.capture_bootstrap_outputs = lambda: None
    known = ceph_tunnel.known_hosts(private["knownHosts"].encode(), ceph_tunnel.targets(native.request))
    original = {"identity": private["clusterPrivateKey"].encode(), "known_hosts": known,
                "manifest.json": ceph_native.canonical({"identityPublicKey": public_key()})}
    native.transport_hold = lambda **kwargs: contextlib.nullcontext(original)


def controller_known_hosts(private):
    """The controller renders each entry with its own line end and then one more, as boundMaterial does."""
    return dict(private, knownHosts="".join(line + "\n\n" for line in private["knownHosts"].splitlines()))


def configuration_fixture(native, private):
    """Stored native values model lost replies without executing provider code."""
    staged(native)
    prepared_bootstrap(native, private)
    native.daemons = lambda: initial_daemons(native)
    native.prove_seed = lambda daemons: None
    native.markers.records["bootstrap-original-ready.json"] = {"fixture": True}
    native.markers.records["transport-ready.json"] = {"custodyID": "d31ad0d4-2fcf-4a8b-9b92-5010197978ce"}
    state, calls = {"values": {}, "acceptance": True}, []

    def ceph(*args, **kwargs):
        calls.append((args, kwargs))
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args in (("orch", "host", "ls"), ("orch", "ls"), ("osd", "ls")):
            if args == ("orch", "ls") and kwargs.get("json_result") is False:
                return b"[]"
            return []
        if args == ("config-key", "ls"):
            return ["mgr/cephadm/" + name for name in state["values"]]
        if args[:2] == ("config-key", "get"):
            return state["values"][args[2].removeprefix("mgr/cephadm/")]
        if args == ("config", "dump"):
            return [{"name": "mgr/cephadm/call_home_needs_acceptance", "section": "mgr", "mask": "", "value": "true" if state["acceptance"] else "false"}]
        names = {"set-priv-key": "ssh_identity_key", "set-pub-key": "ssh_identity_pub", "set-ssh-config": "ssh_config", "set-user": "ssh_user"}
        if args[0] == "cephadm" and args[1] in names:
            state["values"][names[args[1]]] = args[2].encode() if args[1] == "set-user" else kwargs["data"]
            return b""
        if args == ("orch", "accept", "call-home-enabled"):
            state["acceptance"] = False
            return b""
        pytest.fail("unproved fixture command")

    native.ceph = ceph
    native.ceph_text = lambda *args: ceph(*args, json_result=False)
    native.config_database = lambda: None
    return state, calls


def seed_profile(native, uid=167):
    """The already re-proved profile one call holds; its reader is exercised below."""
    native._profile = {"uid": uid, "gid": uid, "policy": "Ciphers aes256-gcm@openssh.com\n", "label": "fixture:shared:label:s0"}


def device_rows(request):
    return [{"host": host["name"], "path": device["path"], "serial": device["serial"], "deviceID": "QEMU_QEMU_HARDDISK_" + device["serial"]}
            for host in request["hosts"] for device in host["devices"]]


class HostMarkers:
    def __init__(self, records):
        self.records = records

    def owned(self):
        return True

    def read(self, name):
        return copy.deepcopy(self.records.get(name))


def host_profile(request, name, **changes):
    record = {"version": ceph_host_runtime.PROFILE_VERSION, "scope": ceph_host_runtime.scope(request, name), "image": request["image"], "host": name,
              "imageID": "c" * 64, "uid": 167, "gid": 167, "policy": "Ciphers aes256-gcm@openssh.com\n", "label": ceph_host_runtime.SHARED_LABEL,
              "cephadmSHA256": "e" * 64,
              "artifacts": {path: "e" * 64 if path == "/usr/sbin/cephadm" else "d" * 64 for path in ceph_host_runtime.ARTIFACTS}}
    record.update(changes)
    return record


def image_inspection(request, image_id="c" * 64):
    return json.dumps([{"Id": image_id, "Os": "linux", "Architecture": "amd64", "RepoDigests": [request["image"]], "Size": 1 << 30}]).encode()


@pytest.fixture
def profiled(cluster_request, tmp_path):
    request = pinned(cluster_request)
    inspected = []

    def command(argv, timeout=60, json_result=True, data=b""):
        del timeout, data
        assert argv == ["/usr/bin/podman", "image", "inspect", request["image"]] and json_result is False
        inspected.append(argv)
        return image_inspection(request, inspected_id[0])

    inspected_id = ["c" * 64]
    worker = ceph_native.Worker(request, request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"),
                                markers=Markers(tmp_path / request["fsid"]))
    worker.command = command
    return worker, inspected, inspected_id


def pinned(request, **changes):
    request = dict(request, image="registry.example.test/ceph" + "@sha256:" + "a" * 64, registry="registry.example.test")
    request.update(changes)
    return request


def test_supported_transport_is_the_pinned_release_over_verified_tunnel_only(cluster_request):
    assert ceph_native.supported_transport(pinned(cluster_request))
    for changed in (
        pinned(cluster_request, clusterSSHTransport="native"),
        pinned(cluster_request, clusterSSHTransport=""),
        pinned(cluster_request, image="registry.example.test/ceph" + ":v9.9.1-25074"),
        pinned(cluster_request, image="registry.example.test/ceph" + "@sha256:" + "A" * 64),
        pinned(cluster_request, image="registry.example.test/ceph" + "@sha256:" + "a" * 63),
    ):
        assert not ceph_native.supported_transport(changed)
    without = pinned(cluster_request)
    del without["clusterSSHTransport"]
    assert not ceph_native.supported_transport(without)


@pytest.mark.parametrize("phase", ["claim", "authorize", "bootstrap", "enroll", "services", "pool", "client", "policy", "destroy"])
def test_remote_dispatch_refuses_an_unsupported_release_before_any_effect(cluster_request, phase):
    cluster_request["clusterSSHTransport"] = "native"
    resources = {"pool": copy.deepcopy(POOL),
                 "client": {"name": "reader", "pool": "volumes", "access": "read-only"},
                 "policy": {"deviceClass": "", "failureDomain": "host", "name": "fast", "root": "default", "rule": "fast", "size": 2}}
    if phase in resources:
        cluster_request.update({"action": phase, phase: resources[phase]})
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        ceph_native.dispatch({"request": pinned(cluster_request), "host": "node-1", "phase": phase, "private": {}})


def test_manager_runtime_fails_closed_until_host_preparation_writes_its_profile(profiled):
    worker, inspected, unused = profiled
    worker.host_markers = HostMarkers({})
    worker.host_markers.owned = lambda: False
    for read in (worker.manager_uid, worker.manager_policy, worker.manager_label, worker.transport_scope):
        with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
            read()
    assert worker.observed_manager_uid() is None and inspected == []
    worker.host_markers = HostMarkers({"scope.json": ceph_host_runtime.scope(worker.request, "node-1")})
    assert worker.observed_manager_uid() is None
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        worker.manager_uid()
    assert inspected == []
    worker.host_markers.records[ceph_host_runtime.PROFILE] = host_profile(worker.request, "node-1")
    assert worker.manager_uid() == worker.observed_manager_uid() == 167
    assert worker.manager_policy() == "Ciphers aes256-gcm@openssh.com\n"
    assert worker.manager_label() == ceph_host_runtime.SHARED_LABEL
    assert len(inspected) == 1


@pytest.mark.parametrize("changes", [
    {"image": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + "b" * 64}, {"host": "node-2"}, {"uid": 0}, {"uid": True}, {"uid": "167"}, {"gid": 168},
    {"label": ""}, {"label": "system_u:object_r:other_t:s0"}, {"policy": None}, {"policy": "Include /etc/foreign\n"}, {"version": "ceph-runtime-profile-v1"},
    {"unproved": True}, {"cephadmSHA256": "f" * 64}, {"imageID": "f" * 64}, {"scope": {"host": "node-1"}},
])
def test_a_contradicting_runtime_profile_refuses(profiled, changes):
    worker, inspected, unused = profiled
    records = {"scope.json": ceph_host_runtime.scope(worker.request, "node-1"), ceph_host_runtime.PROFILE: host_profile(worker.request, "node-1", **changes)}
    worker.host_markers = HostMarkers(records)
    for read in (worker.manager_uid, worker.manager_policy, worker.manager_label, worker.observed_manager_uid):
        with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
            read()
    assert all(argv[:3] == ["/usr/bin/podman", "image", "inspect"] for argv in inspected)


def test_the_profile_is_re_proved_against_the_image_the_host_holds_now(profiled):
    worker, inspected, inspected_id = profiled
    records = {"scope.json": ceph_host_runtime.scope(worker.request, "node-1"), ceph_host_runtime.PROFILE: host_profile(worker.request, "node-1")}
    worker.host_markers = HostMarkers(records)
    inspected_id[0] = "e" * 64
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        worker.manager_uid()
    assert len(inspected) == 1


def test_the_profile_of_another_host_or_scope_never_lends_its_runtime(profiled):
    worker, unused, unused_id = profiled
    worker.host_markers = HostMarkers({"scope.json": ceph_host_runtime.scope(worker.request, "node-2"),
                                       ceph_host_runtime.PROFILE: host_profile(worker.request, "node-1")})
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        worker.manager_uid()


def test_bootstrap_outside_manager_placement_refuses_before_worker_effects(cluster_request, tmp_path):
    cluster_request["services"][1]["placement"]["hosts"] = ["node-2", "node-3"]
    markers = Markers(tmp_path / "markers")

    def forbidden(*args, **kwargs):
        del args, kwargs
        pytest.fail("ineligible bootstrap reached a provider command")

    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        ceph_native.Worker(cluster_request, "node-1", run=forbidden, markers=markers)
    assert markers.writes == []


@pytest.fixture
def account(native, tmp_path, monkeypatch):
    if os.geteuid() == 0:
        pytest.skip("account filesystem ownership test needs a non-root test process")
    home = tmp_path / "home"
    home.mkdir(mode=0o700)
    ssh = home / ".ssh"
    ssh.mkdir(mode=0o700)
    monkeypatch.setattr(ceph_native.pwd, "getpwnam", lambda user: SimpleNamespace(pw_uid=os.getuid(), pw_gid=os.getgid(), pw_dir=str(home)))
    events = []
    native.registry_preflight = lambda wanted: None
    native.prepare_registry = lambda wanted: False
    native.tunnel_policy_preflight = lambda: events.append("preflight")
    native.transport = lambda material: events.append("bundle")
    native.tunnel_guest_policy = lambda: events.append("policy")
    return home, ssh, events


def test_foreign_same_public_key_refuses_before_secret_or_sshd_effect(native, private, account):
    unused, ssh, events = account
    existing = (public_key() + " foreign-owner\n").encode()
    (ssh / "authorized_keys").write_bytes(existing)
    with pytest.raises(ceph_native.Refusal, match="foreign-authorization"):
        native.authorize({key: private[key] for key in ("clusterPublicKey", "clusterPrivateKey", "knownHosts", "registryJSON")})
    assert events == []
    assert native.markers.writes == []
    assert (ssh / "authorized_keys").read_bytes() == existing


def test_owned_authorization_replay_rechecks_guest_policy_and_bundle(native, private, account):
    home, ssh, events = account
    material = {key: private[key] for key in ("clusterPublicKey", "clusterPrivateKey", "knownHosts", "registryJSON")}
    assert native.authorize(material) == {"changed": True}
    line = ceph_tunnel.authorization_line(public_key(), native.request["owner"])
    assert (ssh / "authorized_keys").read_text() == line + "\n"
    assert (ssh / "authorized_keys").stat().st_mode & 0o777 == 0o600
    assert native.markers.read("ssh-authorization.json")["home"] == str(home)
    assert native.authorize(material) == {"changed": False}
    assert events == ["preflight", "bundle", "policy"] * 2


def test_unsafe_account_directory_refuses_before_tunnel_effect(native, private, account):
    home, unused, events = account
    home.chmod(0o777)
    with pytest.raises(ceph_native.Refusal, match="cluster-account"):
        native.authorize({key: private[key] for key in ("clusterPublicKey", "clusterPrivateKey", "knownHosts", "registryJSON")})
    assert events == []


def test_foreign_sshd_match_in_nested_include_refuses_before_provider_command(native):
    files = {"/etc/ssh/sshd_config": b"Include /etc/ssh/sshd_config.d/site.conf\n",
             "/etc/ssh/sshd_config.d/site.conf": b"Include /etc/crypto-policies/back-ends/opensshserver.config\n",
             "/etc/crypto-policies/back-ends/opensshserver.config": b"Match LocalAddress 192.0.2.1\nPermitListen any\n"}
    reads = []

    def read(path, **kwargs):
        assert kwargs["root_only"] is True and kwargs["private"] is False
        reads.append(path)
        return files[path]

    native.native_file = read
    with pytest.raises(ceph_tunnel_staging.StageRefusal):
        native.tunnel_policy_preflight()
    assert set(reads) == set(files)


def test_owned_sshd_policy_requires_exact_protected_bytes(native):
    policy = ceph_tunnel.guest_policy(native.request["clusterSSHUser"])
    token = hashlib.sha256((native.request["fsid"] + "\x00" + native.request["owner"]).encode()).hexdigest()
    record = {"name": "05-bootwright-ceph-" + token + ".conf", "sha256": hashlib.sha256(policy).hexdigest()}
    native.markers.records["sshd-policy.json"] = record
    path = "/etc/ssh/sshd_config.d/" + record["name"]
    files = {"/etc/ssh/sshd_config": ("Include " + path + "\n").encode(), path: policy}
    native.native_file = lambda path, **kwargs: files[path]
    commands = []
    native.command = lambda argv, **kwargs: commands.append(argv)
    native.tunnel_policy_preflight()
    assert commands == [["/usr/sbin/sshd", "-t", "-f", "/etc/ssh/sshd_config"]]
    files[path] += b"PermitListen any\n"
    commands.clear()
    with pytest.raises(ceph_tunnel_staging.StageRefusal):
        native.tunnel_policy_preflight()
    assert commands == []


def test_foreign_guest_policy_record_refuses_before_config_read_or_secret_effect(native):
    native.markers.records["sshd-policy.json"] = {"name": "05-foreign.conf", "sha256": "a" * 64}
    native.native_file = lambda *args, **kwargs: pytest.fail("foreign record reached configuration reading")
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.tunnel_policy_preflight()


def test_foreign_guest_policy_refuses_before_creating_missing_account_ssh_directory(native, private, account):
    home, ssh, events = account
    ssh.rmdir()
    native.tunnel_policy_preflight = lambda: ceph_native.require(False, "foreign-authorization")
    with pytest.raises(ceph_native.Refusal, match="foreign-authorization"):
        native.authorize({key: private[key] for key in ("clusterPublicKey", "clusterPrivateKey", "knownHosts", "registryJSON")})
    assert not (home / ".ssh").exists()
    assert events == []
    assert native.markers.writes == []


def test_effective_guest_policy_drift_after_sshd_reload_never_returns_success(native, tmp_path, monkeypatch):
    policy = ceph_tunnel.guest_policy(native.request["clusterSSHUser"])
    token = hashlib.sha256((native.request["fsid"] + "\x00" + native.request["owner"]).encode()).hexdigest()
    record = {"name": "05-bootwright-ceph-" + token + ".conf", "sha256": hashlib.sha256(policy).hexdigest()}
    native.markers.records["sshd-policy.json"] = record
    directory = tmp_path / "sshd"
    directory.mkdir(mode=0o700)
    actual_open, actual_fstat = os.open, os.fstat
    descriptors = set()

    def owned_directory(path, flags, mode=0o777, **kwargs):
        descriptor = actual_open(str(directory) if path == "/etc/ssh/sshd_config.d" else path, flags, mode, **kwargs)
        if path == "/etc/ssh/sshd_config.d":
            descriptors.add(descriptor)
        return descriptor

    def owned_stat(descriptor):
        info = actual_fstat(descriptor)
        return SimpleNamespace(st_uid=0, st_mode=info.st_mode) if descriptor in descriptors else info

    monkeypatch.setattr(ceph_native.os, "open", owned_directory)
    monkeypatch.setattr(ceph_native.os, "fstat", owned_stat)
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, **kwargs: policy if path.endswith(record["name"]) else b"# root-owned fixture\n")
    effective = b"allowtcpforwarding local\npermitlisten none\npermitopen 127.0.0.1:22\nallowstreamlocalforwarding no\npermittunnel no\n"
    events = []
    state = {"reloaded": False}
    native.tunnel_policy_preflight = lambda: events.append("closure")

    def command(argv, **kwargs):
        assert kwargs["json_result"] is False
        if argv[0] == "/usr/bin/systemctl":
            assert argv == ["/usr/bin/systemctl", "reload", "sshd.service"]
            events.append("reload")
            state["reloaded"] = True
            return b""
        assert argv[:6] == ["/usr/sbin/sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", argv[-1]]
        assert ",laddr=192.0.2.1,lport=22" in argv[-1]
        events.append("effective-after" if state["reloaded"] else "effective-before")
        return effective.replace(b"permitlisten none", b"permitlisten any") if state["reloaded"] else effective

    native.command = command
    with pytest.raises(ceph_tunnel.TunnelRefusal, match="native-tunnel-unproved"):
        native.tunnel_guest_policy()
    assert events == ["closure"] + ["effective-before"] * 4 + ["reload", "closure", "effective-after"]


def test_recursive_sshd_include_cannot_loop_or_exhaust_unbounded_work(native):
    native.native_file = lambda path, **kwargs: b"Include /etc/ssh/sshd_config.d/site.conf\n"
    with pytest.raises(ceph_native.Refusal, match="cluster-account"):
        native.tunnel_policy_preflight()


def test_bootstrap_persists_frozen_intent_before_skip_ssh_command(native, private, monkeypatch):
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    checks = host_preparation(native, monkeypatch, {"chronyd.service": ("enabled", "active")})
    checked_command = native.command
    native.prove_inert_host_preparation = lambda: ceph_native.Worker.prove_inert_host_preparation(native)
    calls = []

    def command(argv, **kwargs):
        time_checks = [["/usr/bin/systemctl", "is-enabled", "chrony.service"],
                       ["/usr/bin/systemctl", "is-active", "chrony.service"],
                       ["/usr/bin/systemctl", "is-enabled", "chronyd.service"],
                       ["/usr/bin/systemctl", "is-active", "chronyd.service"]]
        if argv[:2] == [native.tool, "check-host"]:
            assert checks == time_checks
            assert native.markers.read("bootstrap-attempt.json") is None
            return checked_command(argv, **kwargs)
        assert checks == time_checks + [[native.tool, "check-host", "--expect-hostname", native.host["name"]]]
        assert native.markers.read("bootstrap-attempt.json") == ceph_tunnel_staging.recovery_scope(native.request)
        assert native.markers.read("tunnel-stage.json") == ceph_tunnel_staging.stage_intent(native.request)
        assert "--skip-ssh" in argv and "--no-cleanup-on-failure" in argv
        assert "--skip-prepare-host" not in argv
        assert all(flag not in argv for flag in ("--ssh-user", "--ssh-private-key", "--ssh-public-key", "--ssh-config", "--apply-spec"))
        assert all(not argument.startswith("--registry") and "registryJSON" not in argument for argument in argv)
        scratch = os.path.dirname(argv[argv.index("--config") + 1])
        assert os.listdir(scratch) == ["ceph.conf"]
        assert kwargs["timeout"] == ceph_native.BOOTSTRAP_TIMEOUT and kwargs["json_result"] is False
        calls.append(argv)

    native.command = command
    assert native.tunnel_bootstrap(ssh_material(private)) == {"changed": True}
    assert len(calls) == 1


def test_bootstrap_accepts_the_controllers_known_hosts_in_the_form_the_bundle_holds(native, private):
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    calls = []
    native.command = lambda argv, **kwargs: calls.append(argv)
    assert native.tunnel_bootstrap(ssh_material(controller_known_hosts(private))) == {"changed": True}
    assert len(calls) == 1


@pytest.mark.usefixtures("fact_set")
def test_bootstrap_refuses_known_hosts_naming_fewer_hosts_than_the_bundle(native, private):
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.command = lambda argv, **kwargs: pytest.fail("bootstrap ran over foreign host trust")
    fewer = dict(private, knownHosts="".join(line + "\n" for line in private["knownHosts"].splitlines()[1:]))
    with pytest.raises((ceph_native.Refusal, ceph_tunnel.TunnelRefusal)):
        native.tunnel_bootstrap(ssh_material(fewer))
    assert native.markers.read("bootstrap-attempt.json") is None


@pytest.mark.usefixtures("fact_set")
def test_partial_staged_cluster_has_no_bootstrap_retry_or_direct_fallback(native, private):
    native.daemons = lambda: initial_daemons(native)
    with pytest.raises(ceph_native.Refusal, match="partial-cluster"):
        native.tunnel_bootstrap(ssh_material(private))
    assert native.markers.writes == []


@pytest.mark.parametrize("inventory", [[{"hostname": "foreign"}], {}, None])
def test_configuration_refuses_nonempty_or_untyped_initial_inventory(native, private, inventory):
    staged(native)
    native.daemons = lambda: initial_daemons(native)
    native.prove_seed = lambda daemons: None
    native.markers.records["bootstrap-original-ready.json"] = {"fixture": True}
    calls = []

    def ceph(*args, **kwargs):
        calls.append((args, kwargs))
        return {"fsid": native.request["fsid"]} if args == ("fsid",) else inventory

    native.ceph = ceph
    with pytest.raises(ceph_native.Refusal, match="foreign-host"):
        native.tunnel_configure(ssh_material(private))
    assert [args for args, unused in calls] == [("fsid",), ("orch", "host", "ls")]


def test_configuration_imports_secrets_on_stdin_before_enrollment(native, private):
    unused, calls = configuration_fixture(native, private)
    assert native.tunnel_configure(ssh_material(private)) == {"changed": True}
    effects = [(args, kwargs) for args, kwargs in calls if args[0] == "cephadm"]
    assert [args for args, unused in effects[:2]] == [("cephadm", "set-priv-key", "-i", "-"), ("cephadm", "set-pub-key", "-i", "-")]
    assert effects[0][1]["data"] == private["clusterPrivateKey"].encode()
    assert effects[1][1]["data"] == public_key().encode()
    assert effects[-1][0] == ("cephadm", "set-user", "operator", "--skip-pre-steps")
    assert all("registry" not in " ".join(args) and private["registryJSON"].encode() != kwargs.get("data") for args, kwargs in calls)
    configuration = effects[2][1]["data"]
    assert b"ProxyCommand /usr/bin/python3 -I" in configuration and configuration.endswith(b"proxy.py reject\n")
    assert native.markers.read("tunnel-configured.json")["config"] == hashlib.sha256(configuration).hexdigest()


def manager_fixture(native, inventory=()):
    """The seed's one manager: every MON map read sees a newer instance, as each redeploy restarts it."""
    intent = ceph_tunnel_staging.stage_intent(native.request)
    generations = itertools.count(1)

    def ceph(*args, **kwargs):
        del kwargs
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("mgr", "dump"):
            return {"available": True, "active_name": intent["mgrID"], "active_gid": next(generations), "standbys": []}
        if args == ("orch", "status"):
            return {"available": True, "backend": "cephadm"}
        return list(inventory)
    return ceph


def mount_fixture(native):
    staged(native)
    native.markers.records["tunnel-configured.json"] = {"config": "fixture"}
    daemons = initial_daemons(native)
    native.daemons = lambda: daemons
    native.ceph = manager_fixture(native)
    paths = ceph_tunnel_staging.initial_files(native.request)
    contents = {paths[kind]["unit.meta"]: metadata(kind) for kind in ("mon", "mgr")}
    contents.update({paths["mgr"]["config"]: b"[global]\n", paths["mgr"]["keyring"]: b"private keyring fixture\n"})

    def read(path, **kwargs):
        if path.endswith("/unit.meta"):
            assert kwargs == {"maximum": 4096, "private": False}
        else:
            assert kwargs.get("private", True) is True
        return contents[path]

    native.native_file = read

    @contextlib.contextmanager
    def source(path, **kwargs):
        assert path in (paths["mgr"]["config"], paths["mgr"]["keyring"])
        physical = os.path.join(native.markers.path, "native-" + os.path.basename(path))
        if not os.path.exists(physical):
            with open(physical, "wb") as handle:
                handle.write(contents[path])
            os.chmod(physical, 0o600)
        with open(physical, "rb") as handle:
            yield handle

    native.native_source = source
    native.transport_hold = lambda **kwargs: contextlib.nullcontext()
    native.verify_transport_bundle = lambda: None
    native.verify_manager_mounts = lambda observed, observing=False: []
    native.verify_tunnel_configuration = lambda: None
    return daemons, contents, paths


def test_initial_mount_redeploy_preserves_native_metadata_and_retains_original_material(native):
    unused, contents, paths = mount_fixture(native)
    commands = []

    def command(argv, **kwargs):
        record = native.markers.read("tunnel-redeploy.json")
        assert set(record) == {"scope", "custodyID"}
        assert record["scope"]["plan"]["mgrMetadata"]["ports"] == [8765, 9283]
        with ceph_tunnel_custody.hold(native.markers, "initial-mgr-original", record["scope"]) as (original, material):
            assert original["custodyID"] == record["custodyID"]
            assert material == {"config": contents[paths["mgr"]["config"]], "keyring": contents[paths["mgr"]["keyring"]]}
        assert argv[3] == "deploy" and "--reconfig" not in argv
        assert argv[argv.index("--tcp-ports") + 1] == "8765 9283"
        assert "--extra-container-args=" + native.markers.path + "/transport:/run/bootwright-ceph-ssh:ro,z" in argv
        assert kwargs["timeout"] == 600
        commands.append(argv)

    native.command = command
    assert native.tunnel_mount() == {"changed": True}
    assert len(commands) == 1
    assert native.markers.read("tunnel-mounted.json")["intent"] == ceph_tunnel_staging.stage_intent(native.request)


@pytest.mark.parametrize("drift", ["mgr-id", "fsid", "image", "metadata", "inventory"])
def test_initial_redeploy_refuses_foreign_or_changed_provider_state_before_effect(native, drift):
    daemons, contents, paths = mount_fixture(native)
    if drift == "metadata":
        contents[paths["mgr"]["unit.meta"]] = b'{"service_name":"mgr","ports":[22],"memory_request":null,"memory_limit":null}'
    elif drift == "inventory":
        native.ceph = lambda *args, **kwargs: {"fsid": native.request["fsid"]} if args == ("fsid",) else [{"hostname": "node-1"}]
    else:
        field = {"mgr-id": "name", "fsid": "fsid", "image": "container_image_name"}[drift]
        daemons[1][field] = "foreign"
    with pytest.raises(ceph_tunnel_staging.StageRefusal):
        native.tunnel_mount()
    assert "tunnel-redeploy.json" not in native.markers.records


def inspect_fixture(native):
    native.prove_mount_source = lambda source, exact: None
    daemon = initial_daemons(native)[1]
    value = {"Id": daemon["container_id"], "HostConfig": {"Privileged": False},
             "Mounts": [{"Type": "bind", "Source": native.markers.path + "/transport", "Destination": ceph_tunnel.CONTAINER_ROOT, "RW": False}]}
    native.command = lambda argv, **kwargs: [value]
    return daemon, value


def test_current_manager_mount_requires_exact_owned_read_only_nonprivileged_bind(native):
    daemon, unused = inspect_fixture(native)
    native.verify_manager_mounts([daemon])


@pytest.mark.parametrize("drift", ["rw", "source", "destination", "duplicate", "privileged", "container-id", "image", "fsid", "host", "empty", "mon-only"])
def test_manager_mount_drift_has_no_current_tunnel_proof(native, drift):
    daemon, value = inspect_fixture(native)
    if drift in ("rw", "source", "destination"):
        field, replacement = {"rw": ("RW", True), "source": ("Source", "/foreign/bundle"), "destination": ("Destination", "/foreign/container")}[drift]
        value["Mounts"][0][field] = replacement
    elif drift == "duplicate":
        value["Mounts"].append(copy.deepcopy(value["Mounts"][0]))
    elif drift == "privileged":
        value["HostConfig"]["Privileged"] = True
    elif drift == "container-id":
        value["Id"] = "c" * 64
    elif drift in ("image", "fsid", "host"):
        field = {"image": "container_image_name", "fsid": "fsid", "host": "name"}[drift]
        daemon[field] = "foreign"
    observed = [] if drift == "empty" else initial_daemons(native)[:1] if drift == "mon-only" else [daemon]
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.verify_manager_mounts(observed)


@pytest.mark.parametrize("destination", ["/", "/run", "/run/bootwright-ceph-ssh/identity", "/run/bootwright-ceph-ssh/manifest.json",
                                         "/run/bootwright-ceph-ssh/", "/run/bootwright-ceph-ssh/../bootwright-ceph-ssh/identity", "relative"])
def test_parent_nested_or_noncanonical_mount_cannot_override_protected_bundle(native, destination):
    daemon, value = inspect_fixture(native)
    value["Mounts"].append({"Type": "bind", "Source": "/foreign/material", "Destination": destination, "RW": False})
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.verify_manager_mounts([daemon])


@pytest.mark.parametrize("image,container,admitted", [("", "", True), ("request", "", True), ("foreign", "", False), ("", "d" * 64, False)])
def test_manager_mount_proof_admits_only_a_daemon_without_a_container_naming_no_image(native, image, container, admitted):
    daemon, unused = inspect_fixture(native)
    pending = dict(daemon, name="osd.0", state="unknown", enabled=False, container_id=container, container_image_id="", container_image_digests=[],
                   container_image_name=native.request["image"] if image == "request" else image)
    if admitted:
        native.verify_manager_mounts([pending, daemon])
    else:
        with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
            native.verify_manager_mounts([pending, daemon])


@pytest.mark.parametrize("name,admitted", [("haproxy.rgw.objects.node-1.abcdef", True), ("mon.x", False)])
def test_manager_mounts_ignore_delegated_daemon_images(native, name, admitted):
    daemon, unused = inspect_fixture(native)
    delegated = dict(daemon, name=name, container_id="e" * 64, container_image_id="f" * 64,
                     container_image_name="registry.example.test/haproxy@sha256:" + "b" * 64)
    if admitted:
        native.verify_manager_mounts([delegated, daemon])
    else:
        with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
            native.verify_manager_mounts([delegated, daemon])


@pytest.mark.parametrize("state,container", [("stopped", ""), ("unknown", ""), ("unknown", "b" * 64), ("error", "")])
def test_observed_manager_between_containers_is_pending_while_an_action_refuses(native, state, container):
    daemon, unused = inspect_fixture(native)
    daemon.update(state=state, container_id=container, container_image_name=native.request["image"] if container else "")
    native.command = lambda *args, **kwargs: pytest.fail("a manager without a running container was inspected")
    assert native.verify_manager_mounts([daemon], observing=True) == [daemon["name"]]
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.verify_manager_mounts([daemon])


@pytest.mark.parametrize("drift", ["rw", "source", "privileged", "image"])
def test_observed_running_manager_that_contradicts_the_request_still_refuses(native, drift):
    daemon, value = inspect_fixture(native)
    if drift == "rw":
        value["Mounts"][0]["RW"] = True
    elif drift == "source":
        value["Mounts"][0]["Source"] = "/foreign/bundle"
    elif drift == "privileged":
        value["HostConfig"]["Privileged"] = True
    else:
        daemon["container_image_name"] = "foreign"
    with pytest.raises(ceph_native.Refusal, match="^native-tunnel-unproved$"):
        native.verify_manager_mounts([daemon], observing=True)


def test_unrelated_native_mount_does_not_break_protected_bundle_proof(native):
    daemon, value = inspect_fixture(native)
    value["Mounts"].append({"Type": "bind", "Source": "/var/run/ceph", "Destination": "/run/ceph", "RW": True})
    native.verify_manager_mounts([daemon])


def test_enrollment_has_no_host_effect_until_all_current_tunnel_proofs_pass(native):
    native.markers.records["tunnel-mounted.json"] = {"fixture": True}
    native.markers.records["bootstrap-scope.json"] = ceph_tunnel_staging.recovery_scope(native.request)
    events, inventory = [], []
    native.verify_transport_bundle = lambda: events.append("bundle")
    native.verify_tunnel_configuration = lambda: events.append("config")
    native.verify_manager_mounts = lambda observed: events.append("mount")
    native.daemons = lambda: initial_daemons(native)
    native.license_accepted = lambda: True

    def ceph(*args, **kwargs):
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("orch", "host", "ls"):
            assert events[:3] == ["bundle", "config", "mount"]
            return copy.deepcopy(inventory)
        assert args == ("orch", "apply", "-i", "-")
        value = json.loads(kwargs["data"])
        events.append(value["hostname"])
        inventory.append({"hostname": value["hostname"], "addr": value["addr"], "labels": value["labels"]})

    native.ceph = ceph
    native.markers.records["cache-ready.json"] = {"fixture": True}
    assert native.enroll() == {"changed": True}
    assert events == ["bundle", "config", "mount", "node-1", "node-2", "node-3"]


def test_enrollment_refuses_before_the_cephadm_cache_is_proved(native):
    native.markers.records["tunnel-mounted.json"] = {"fixture": True}
    calls = []
    native.ceph = lambda *args, **kwargs: calls.append(args) or {"fsid": native.request["fsid"]}
    with pytest.raises(ceph_native.Refusal, match="^cache-unproved$"):
        native.enroll()
    assert calls == [("fsid",)]


def test_enrollment_refuses_mount_drift_before_any_host_add(native):
    native.markers.records["tunnel-mounted.json"] = {"fixture": True}
    native.markers.records["cache-ready.json"] = {"fixture": True}
    native.verify_transport_bundle = lambda: None
    native.verify_tunnel_configuration = lambda: None
    native.daemons = lambda: initial_daemons(native)
    native.verify_manager_mounts = lambda observed: ceph_native.require(False, "native-tunnel-unproved")
    calls = []
    native.ceph = lambda *args, **kwargs: calls.append(args) or {"fsid": native.request["fsid"]}
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.enroll()
    assert calls == [("fsid",)]


def test_service_documents_add_owned_manager_mount_without_mutating_frozen_input(native):
    original = copy.deepcopy(native.request["services"])
    native.verify_tunnel_configuration = lambda: None
    native.normalized_services = lambda groups: groups
    native.recovery_scope = lambda: {"fixture": True}
    calls, inventory = [], []

    def ceph(*args, **kwargs):
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        if args == ("orch", "ls", "--export"):
            return copy.deepcopy(inventory)
        calls.append((args, kwargs))
        inventory.append(json.loads(kwargs["data"]))

    native.ceph = ceph
    native.services(device_rows(native.request))
    assert native.markers.read(ceph_native.DEVICE_IDENTITIES)["devices"] == device_rows(native.request)
    assert calls[0][0] == ("orch", "apply", "-i", "-")
    documents = [json.loads(kwargs["data"]) for args, kwargs in calls]
    manager = next(service for service in documents if service["service_type"] == "mgr")
    assert manager["extra_container_args"] == ["--volume=" + native.markers.path + "/transport:/run/bootwright-ceph-ssh:ro,z"]
    assert native.request["services"] == original


def test_service_apply_refuses_changed_native_config_without_direct_fallback(native):
    native.ceph = lambda *args, **kwargs: {"fsid": native.request["fsid"]} if args == ("fsid",) else pytest.fail("service apply preceded current config proof")
    native.verify_tunnel_configuration = lambda: ceph_native.require(False, "native-tunnel-unproved")
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.services(device_rows(native.request))


def preflight_only(native):
    def ceph(*args, **kwargs):
        del kwargs
        if args == ("fsid",):
            return {"fsid": native.request["fsid"]}
        return pytest.fail("a service effect preceded the device identity proof")
    return ceph


@pytest.mark.parametrize("change", ["missing", "serial", "path", "order", "host", "extra", "malformed"])
def test_services_refuse_missing_or_contradicting_device_identities_before_any_effect(native, change):
    native.ceph = preflight_only(native)
    rows = device_rows(native.request)
    if change == "missing":
        rows = None
    elif change == "serial":
        rows[0]["serial"] = "guessed"
    elif change == "path":
        rows[0]["path"] = "/dev/vdc"
    elif change == "order":
        rows.reverse()
    elif change == "host":
        rows[1]["host"] = "node-1"
    elif change == "extra":
        rows.append(dict(rows[0]))
    else:
        rows[0]["deviceID"] = "bad id with spaces"
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        native.services(rows)
    assert ceph_native.DEVICE_IDENTITIES not in native.markers.records


@pytest.mark.parametrize("phase", ["observe", "enroll", "devices", "cache"])
def test_only_the_services_phase_receives_device_identities(cluster_request, phase):
    envelope = {"request": pinned(cluster_request), "host": "node-1", "phase": phase, "private": {}, "devices": device_rows(cluster_request)}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch(envelope)


def test_the_services_phase_hands_its_device_identities_to_the_worker(cluster_request, monkeypatch):
    received = []
    monkeypatch.setattr(ceph_native.Worker, "services", lambda self, devices=None, service=None: received.append((devices, service)) or {"changed": True})
    envelope = {"request": pinned(cluster_request), "host": "node-1", "phase": "services", "private": {}, "devices": device_rows(cluster_request)}
    assert ceph_native.dispatch(envelope) == {"changed": True}
    assert received == [(device_rows(cluster_request), None)]


def test_published_device_identities_never_change(native):
    native.markers.records[ceph_native.DEVICE_IDENTITIES] = {"version": ceph_native.DEVICE_IDENTITIES_VERSION, "fsid": native.request["fsid"],
                                                             "devices": device_rows(native.request)}
    rows = device_rows(native.request)
    rows[2]["deviceID"] = "QEMU_QEMU_HARDDISK_other"
    native.ceph = preflight_only(native)
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        native.services(rows)


@pytest.fixture
def filesystem_uid(native, monkeypatch):
    if os.geteuid() == 0:
        pytest.skip("qualified non-root manager ownership fixture needs a non-root test process")
    uid = os.geteuid()
    seed_profile(native, uid)
    monkeypatch.setattr(ceph_tunnel, "_BOOTWRIGHT_SOURCE", "fixed test helper\n", raising=False)
    return uid


def transport_material(private):
    return {key: private[key] for key in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")}


def test_bundle_publication_is_private_owned_and_refuses_missing_completed_file(native, private, filesystem_uid):
    assert native.transport(transport_material(private)) == {"changed": True}
    path = os.path.join(native.markers.path, "transport")
    with ceph_tunnel_custody.hold(native.markers, "transport-original", native.transport_scope()) as (unused, original):
        manifest = ceph_tunnel.verify_bundle(path, expected=original, uid=filesystem_uid)
    assert manifest["owner"] == native.request["owner"]
    assert os.stat(path).st_mode & 0o777 == 0o700
    for name in ceph_tunnel.FILES | {"manifest.json"}:
        info = os.stat(os.path.join(path, name))
        assert info.st_uid == filesystem_uid and info.st_nlink == 1 and info.st_mode & 0o777 == 0o600
    os.unlink(os.path.join(path, "native_config"))
    with pytest.raises(FileNotFoundError):
        native.transport(transport_material(private))
    assert native.markers.read("transport-ready.json") is not None
    assert native.markers.read("transport-ready.json") == native.markers.read("transport.json")


@pytest.mark.parametrize("drift", ["extra-file", "wrong-bytes", "hard-link", "symlink", "shared-directory", "changed-key"])
def test_existing_bundle_drift_is_never_overwritten_or_rebound(native, private, filesystem_uid, drift):
    native.transport(transport_material(private))
    path = os.path.join(native.markers.path, "transport")
    material = transport_material(private)
    if drift == "extra-file":
        with open(os.path.join(path, "foreign"), "wb") as handle:
            handle.write(b"foreign fixture")
    elif drift == "wrong-bytes":
        with open(os.path.join(path, "identity"), "wb") as handle:
            handle.write(b"replacement fixture")
    elif drift == "hard-link":
        os.link(os.path.join(path, "identity"), os.path.join(native.markers.path, "foreign-alias"))
    elif drift == "symlink":
        os.unlink(os.path.join(path, "identity"))
        os.symlink("known_hosts", os.path.join(path, "identity"))
    elif drift == "shared-directory":
        os.chmod(path, 0o755)
    else:
        material["clusterPrivateKey"] = "different private fixture"
    with pytest.raises((ceph_native.Refusal, ceph_tunnel_custody.CustodyRefusal, OSError)):
        native.transport(material)
    assert not any(name.startswith(".bootwright-") for name in os.listdir(path))


def test_bundle_publication_lost_ready_reply_replays_only_reserved_inode(native, private, filesystem_uid):
    del filesystem_uid
    original_write = native.markers.write

    def lost(name, value):
        original_write(name, value)
        if name == "transport-file-ready-identity.json":
            raise OSError("injected lost ready acknowledgement")

    native.markers.write = lost
    with pytest.raises(OSError, match="injected lost ready"):
        native.transport(transport_material(private))
    assert native.markers.read("transport-ready.json") is None
    native.markers.write = original_write
    assert native.transport(transport_material(private)) == {"changed": False}
    native.verify_transport_bundle()


def test_missing_image_uid_refuses_before_any_bundle_effect(native, private):
    native.host_markers = HostMarkers({})
    native.host_markers.owned = lambda: False
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        native.transport(transport_material(private))
    assert native.markers.writes == []
    assert not os.path.exists(os.path.join(native.markers.path, "transport"))


@pytest.fixture
def native_path(native, tmp_path, monkeypatch, filesystem_uid):
    del filesystem_uid
    directory = tmp_path / "owned"
    directory.mkdir(mode=0o700)
    path = directory / "native"
    path.write_bytes(b"protected fixture\n")
    path.chmod(0o600)
    original = os.open

    def open_in_fixture(path, flags, mode=0o777, **kwargs):
        return original(str(tmp_path) if path == "/" else path, flags, mode, **kwargs)

    monkeypatch.setattr(ceph_native.os, "open", open_in_fixture)
    return path, "/owned/native"


def test_native_reader_accepts_non_secret_readable_metadata_and_rejects_shared_private_key(native, native_path):
    actual, path = native_path
    actual.chmod(0o644)
    assert native.native_file(path, private=False) == b"protected fixture\n"
    with pytest.raises(ceph_native.Refusal, match="file"):
        native.native_file(path)


@pytest.mark.parametrize("drift", ["directory-mode", "file-mode", "symlink", "hard-link", "empty", "oversized", "dotdot", "root-only"])
def test_native_file_reader_refuses_foreign_paths_aliases_and_permissions(native, native_path, drift):
    actual, path = native_path
    arguments = {}
    if drift == "directory-mode":
        actual.parent.chmod(0o777)
    elif drift == "file-mode":
        actual.chmod(0o666)
    elif drift == "symlink":
        actual.unlink()
        actual.symlink_to("foreign")
    elif drift == "hard-link":
        os.link(actual, actual.parent / "alias")
    elif drift == "empty":
        actual.write_bytes(b"")
    elif drift == "oversized":
        arguments["maximum"] = 1
    elif drift == "dotdot":
        path = "/owned/../native"
    else:
        arguments["root_only"] = True
    with pytest.raises((ceph_native.Refusal, OSError)):
        native.native_file(path, **arguments)


POLICY_TARGET = "/usr/share/crypto-policies/DEFAULT/opensshserver.txt"
POLICY_LINK = "/etc/crypto-policies/back-ends/opensshserver.config"


@pytest.fixture
def policy_path(native, tmp_path, monkeypatch, filesystem_uid):
    """The crypto-policy back-end as RHEL 9 ships it: a root-owned link under /etc to a hard-linked file under /usr/share."""
    del filesystem_uid
    monkeypatch.setattr(ceph_native, "ROOT_UID", os.geteuid())
    for name in ("etc", "etc/crypto-policies", "etc/crypto-policies/back-ends", "usr", "usr/share", "usr/share/crypto-policies",
                 "usr/share/crypto-policies/DEFAULT"):
        (tmp_path / name).mkdir()
        (tmp_path / name).chmod(0o755)
    target = tmp_path / POLICY_TARGET.lstrip("/")
    target.write_bytes(b"Ciphers aes256-gcm@openssh.com\n")
    target.chmod(0o644)
    os.link(target, target.parent / "openssh.txt")
    original = os.open

    def open_in_fixture(path, flags, mode=0o777, **kwargs):
        return original(str(tmp_path) if path == "/" else path, flags, mode, **kwargs)

    monkeypatch.setattr(ceph_native.os, "open", open_in_fixture)
    return tmp_path


@pytest.mark.parametrize("target", [POLICY_TARGET, "../../../usr/share/crypto-policies/DEFAULT/opensshserver.txt"])
def test_a_public_root_only_source_follows_the_root_owned_link_to_its_hard_linked_policy(native, policy_path, target):
    os.symlink(target, policy_path / POLICY_LINK.lstrip("/"))
    assert os.stat(policy_path / POLICY_TARGET.lstrip("/")).st_nlink == 2
    assert native.native_file(POLICY_LINK, private=False, root_only=True) == b"Ciphers aes256-gcm@openssh.com\n"


@pytest.mark.parametrize("links", [ceph_native.SOURCE_LINKS, ceph_native.SOURCE_LINKS + 1])
def test_a_public_root_only_source_follows_links_only_within_the_bound(native, policy_path, links):
    names = ["opensshserver.config"] + ["hop-%d" % index for index in range(1, links)]
    for name, following in zip(names, names[1:] + [POLICY_TARGET]):
        os.symlink(following, policy_path / "etc/crypto-policies/back-ends" / name)
    if links > ceph_native.SOURCE_LINKS:
        with pytest.raises(ceph_native.Refusal, match="^file$"):
            native.native_file(POLICY_LINK, private=False, root_only=True)
    else:
        assert native.native_file(POLICY_LINK, private=False, root_only=True) == b"Ciphers aes256-gcm@openssh.com\n"


@pytest.mark.parametrize("drift", ["private-source", "account-source", "writable-target-directory", "escaping-link", "private-hard-link"])
def test_every_other_source_refuses_a_link_and_a_second_name(native, policy_path, drift):
    os.symlink("../../.." if drift == "escaping-link" else POLICY_TARGET, policy_path / POLICY_LINK.lstrip("/"))
    path, arguments = POLICY_LINK, {"private": False, "root_only": True}
    if drift == "private-source":
        arguments = {"root_only": True}
    elif drift == "account-source":
        arguments = {"private": False}
    elif drift == "writable-target-directory":
        (policy_path / "usr/share/crypto-policies/DEFAULT").chmod(0o777)
    elif drift == "private-hard-link":
        (policy_path / POLICY_TARGET.lstrip("/")).chmod(0o600)
        path, arguments = POLICY_TARGET, {"root_only": True}
    with pytest.raises(ceph_native.Refusal, match="^file$"):
        native.native_file(path, **arguments)
    if drift == "private-hard-link":
        os.unlink(policy_path / "usr/share/crypto-policies/DEFAULT/openssh.txt")
        assert native.native_file(path, **arguments) == b"Ciphers aes256-gcm@openssh.com\n"


def test_stage_intent_failure_prevents_first_bootstrap_effect(native, private):
    native.daemons = lambda: []
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.markers.write = lambda name, value: ceph_native.require(False, "ownership")
    with pytest.raises(ceph_native.Refusal, match="ownership"):
        native.tunnel_bootstrap(ssh_material(private))
    assert not any(name.startswith(".bootstrap-") for name in os.listdir(native.markers.path))


@pytest.mark.parametrize("same", [True, False])
def test_atomic_stage_journal_concurrent_publication_never_overwrites_winner(native, monkeypatch, same):
    markers = ceph_native.Markers(native.request, root=os.path.dirname(native.markers.path))
    value = {"intent": ceph_tunnel_staging.stage_intent(native.request)}
    winner = value if same else {"intent": "foreign"}
    original = os.link

    def publish_competitor(source, destination, **kwargs):
        with open(destination, "xb") as handle:
            handle.write(ceph_native.canonical(winner))
        os.chmod(destination, 0o600)
        return original(source, destination, **kwargs)

    with monkeypatch.context() as patch:
        patch.setattr(ceph_native.os, "link", publish_competitor)
        if same:
            markers.write("tunnel-stage.json", value)
        else:
            with pytest.raises(ceph_native.Refusal, match="ownership"):
                markers.write("tunnel-stage.json", value)
    assert markers.read("tunnel-stage.json") == winner
    assert not any(name.startswith(".ownership-") for name in os.listdir(markers.path))


def test_atomic_stage_journal_failure_before_publication_removes_partial_record(native, monkeypatch):
    markers = ceph_native.Markers(native.request, root=os.path.dirname(native.markers.path))
    original = os.fsync

    def fail_file_sync(descriptor):
        if not os.path.isdir("/proc/self/fd/" + str(descriptor)):
            raise OSError("injected durable record failure")
        return original(descriptor)

    with monkeypatch.context() as patch:
        patch.setattr(ceph_native.os, "fsync", fail_file_sync)
        with pytest.raises(OSError, match="injected durable record failure"):
            markers.write("tunnel-stage.json", {"intent": "fixture"})
    assert markers.read("tunnel-stage.json") is None
    assert os.listdir(markers.path) == []


def test_late_stage_directory_sync_failure_keeps_exact_published_record_without_temporary_alias(native, monkeypatch):
    markers = ceph_native.Markers(native.request, root=os.path.dirname(native.markers.path))
    value = {"intent": ceph_tunnel_staging.stage_intent(native.request)}
    original = os.fsync

    def fail_directory_sync(descriptor):
        if os.path.isdir("/proc/self/fd/" + str(descriptor)):
            raise OSError("injected directory sync failure")
        return original(descriptor)

    with monkeypatch.context() as patch:
        patch.setattr(ceph_native.os, "fsync", fail_directory_sync)
        with pytest.raises(OSError, match="injected directory sync failure"):
            markers.write("tunnel-stage.json", value)
    assert markers.read("tunnel-stage.json") == value
    assert os.listdir(markers.path) == ["tunnel-stage.json"]
    assert os.stat(os.path.join(markers.path, "tunnel-stage.json")).st_nlink == 1


def test_final_mount_proof_failure_never_publishes_success_after_redeploy(native):
    mount_fixture(native)
    effects = []
    native.command = lambda argv, **kwargs: effects.append("redeploy")
    native.verify_manager_mounts = lambda observed: ceph_native.require(False, "native-tunnel-unproved")
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.tunnel_mount()
    assert effects == ["redeploy"]
    assert native.markers.read("tunnel-redeploy.json") is not None
    assert native.markers.read("tunnel-mounted.json") is None


@pytest.mark.parametrize("drift", ["direct", "unknown-host", "user"])
def test_current_native_configuration_or_user_drift_cannot_authorize_effects(native, drift):
    # Original admin descriptors are exercised by the recovery custody suite.
    native.bootstrap_hold = contextlib.nullcontext
    expected = ceph_tunnel.native_configuration(ceph_tunnel.targets(native.request))
    if drift == "direct":
        expected = expected.replace(b"ProxyCommand /usr/bin/python3 -I", b"ProxyCommand none #")
    elif drift == "unknown-host":
        expected = expected.replace(b"proxy.py reject", b"proxy.py 0")
    user = b"foreign\n" if drift == "user" else b"operator\n"
    commands = []

    def command(argv, **kwargs):
        assert kwargs["json_result"] is False
        commands.append(argv[-1])
        return expected if argv[-1] == "get-ssh-config" else user

    native.command = command
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.verify_tunnel_configuration()
    assert commands == (["get-ssh-config", "get-user"] if drift == "user" else ["get-ssh-config"])


def test_initial_manager_lost_redeploy_response_observes_exact_result_without_repeat_effect(native):
    mount_fixture(native)
    calls = []

    def lost(argv, **kwargs):
        calls.append(argv)
        raise ceph_native.Refusal("native-unknown")

    native.command = lost
    with pytest.raises(ceph_native.Refusal, match="native-unknown"):
        native.tunnel_mount()
    assert native.markers.read("tunnel-redeploy.json") is not None
    assert native.markers.read("tunnel-mounted.json") is None
    native.command = lambda *args, **kwargs: pytest.fail("redeploy repeated after ambiguous reply")
    assert native.tunnel_mount() == {"changed": False}
    assert len(calls) == 1


def test_initial_manager_ambiguous_unmounted_result_refuses_without_redeployment(native):
    mount_fixture(native)
    native.command = lambda *args, **kwargs: ceph_native.require(False, "native-unknown")
    with pytest.raises(ceph_native.Refusal):
        native.tunnel_mount()
    native.verify_manager_mounts = lambda observed: ceph_native.require(False, "native-tunnel-unproved")
    native.command = lambda *args, **kwargs: pytest.fail("ambiguous unmounted deployment repeated")
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.tunnel_mount()
    assert native.markers.read("tunnel-mounted.json") is None


def test_initial_manager_source_identity_is_durable_before_first_private_read(native):
    unused_1, unused_2, paths = mount_fixture(native)
    original_source = native.native_source
    reads = []

    @contextlib.contextmanager
    def source(path, **kwargs):
        with original_source(path, **kwargs) as handle:
            class Guard:
                def fileno(self):
                    return handle.fileno()

                def read(self, count):
                    assert native.markers.read("tunnel-mgr-source.json") is not None
                    reads.append(path)
                    return handle.read(count)

            yield Guard()

    native.native_source = source
    native.command = lambda *args, **kwargs: None
    native.tunnel_mount()
    assert set(reads) == {paths["mgr"]["config"], paths["mgr"]["keyring"]}


@pytest.mark.parametrize("replacement", ["source", "original"])
def test_initial_manager_private_material_drift_refuses_no_matching_bytes_adoption(native, replacement):
    mount_fixture(native)
    native.command = lambda *args, **kwargs: None
    native.tunnel_mount()
    path = os.path.join(native.markers.path, "native-keyring" if replacement == "source" else "initial-mgr-original.private")
    stage = path + ".replacement"
    with open(path, "rb") as source, open(stage, "wb") as destination:
        destination.write(source.read())
    os.chmod(stage, 0o600)
    os.replace(stage, path)
    with pytest.raises((ceph_native.Refusal, ceph_tunnel_custody.CustodyRefusal)):
        native.tunnel_mount()


@pytest.mark.parametrize("source", ["transport", "transport/identity", "transport-original.private", "", ".."])
@pytest.mark.parametrize("rw", [False, True])
def test_manager_container_has_no_alias_to_originals_or_protected_bundle(native, source, rw):
    daemon, value = inspect_fixture(native)
    value["Mounts"].append({"Type": "bind", "Source": os.path.normpath(os.path.join(native.markers.path, source)),
                            "Destination": "/unrelated-alias", "RW": rw})
    with pytest.raises(ceph_native.Refusal, match="native-tunnel-unproved"):
        native.verify_manager_mounts([daemon])


def test_transport_readiness_does_not_admit_bootstrap_without_original_custody(native, private, filesystem_uid):
    del filesystem_uid
    native.daemons = lambda: []
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.markers.records["bootstrap-scope.json"] = ceph_tunnel_staging.recovery_scope(native.request)
    native.prove_preparation = lambda: None
    native.command = lambda *args, **kwargs: pytest.fail("bootstrap admitted an unproved marker")
    with pytest.raises(ceph_tunnel_custody.CustodyRefusal):
        native.tunnel_bootstrap(ssh_material(private))


def host_preparation(native, monkeypatch, units, podman=b"5.8.2\n", lvcreate=True, host_check=0):
    """Answer the read-only checks as a RHEL 9 guest does, stdout only: systemd 252 prints a unit it does not know on stderr.
    Any other command fails the test."""
    calls = []

    def run(argv, data=b"", timeout=60):
        del data, timeout
        calls.append(list(argv))
        if argv[:2] == [native.tool, "check-host"]:
            return host_check, b"synthetic selected host check"
        if argv[0] == "/usr/bin/systemctl" and argv[1] in ("is-enabled", "is-active"):
            enabled, active = units.get(argv[2], (None, "inactive"))
            if argv[1] == "is-enabled":
                if enabled is None:
                    return 1, b""
                return (0 if enabled == "enabled" else 1), enabled.encode() + b"\n"
            return (0 if active == "active" else 3), active.encode() + b"\n"
        pytest.fail("host preparation proof ran a command that is not read-only: %r" % argv)

    native.run = run
    native.command = lambda argv, **kwargs: ceph_native.Worker.command(native, argv, **kwargs)
    original = os.access
    monkeypatch.setattr(ceph_native.os.path, "isfile", lambda path: lvcreate if path == "/usr/sbin/lvcreate" else os.path.exists(path))
    monkeypatch.setattr(ceph_native.os, "access", lambda path, mode: lvcreate if path == "/usr/sbin/lvcreate" else original(path, mode))
    return calls


def test_selected_cephadm_checks_the_host_after_inert_time_service_proof(native, monkeypatch):
    calls = host_preparation(native, monkeypatch, {"chronyd.service": ("enabled", "active")})
    native.prove_inert_host_preparation()
    assert calls[-1] == [native.tool, "check-host", "--expect-hostname", native.host["name"]]
    assert [argv[2] for argv in calls if argv[0] == "/usr/bin/systemctl"] == ["chrony.service"] * 2 + ["chronyd.service"] * 2


@pytest.mark.parametrize("units,podman,lvcreate,reason", [
    ({"chronyd.service": ("disabled", "inactive"), "ntpd.service": ("enabled", "active")}, b"5.8.2\n", True, "host-time"),
    ({"chrony.service": ("enabled", "inactive"), "chronyd.service": ("enabled", "active")}, b"5.8.2\n", True, "host-time"),
    ({}, b"5.8.2\n", True, "host-time"),
    ({"chronyd.service": ("enabled", "active")}, b"5.8.2\n", False, "host-prerequisite"),
])
def test_a_host_cephadm_would_change_while_preparing_it_refuses_before_any_bootstrap_record(native, monkeypatch, units, podman, lvcreate, reason):
    calls = host_preparation(native, monkeypatch, units, podman, lvcreate)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.prove_inert_host_preparation()
    assert all(argv[:2] != [native.tool, "check-host"] for argv in calls)
    assert native.markers.writes == []


def test_a_host_preparation_refusal_leaves_no_bootstrap_attempt_so_the_bootstrap_may_be_retried(native, private):
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    native.prove_inert_host_preparation = lambda: ceph_native.require(False, "host-time")
    native.command = lambda argv, **kwargs: pytest.fail("cephadm ran over a host its preparation would change")
    with pytest.raises(ceph_native.Refusal, match="^host-time$"):
        native.tunnel_bootstrap(ssh_material(private))
    assert native.markers.read("bootstrap-attempt.json") is None


def test_selected_native_host_check_failure_has_no_preparation_fallback(native, private, monkeypatch):
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    calls = host_preparation(native, monkeypatch, {"chronyd.service": ("enabled", "active")}, host_check=1)
    native.prove_inert_host_preparation = lambda: ceph_native.Worker.prove_inert_host_preparation(native)
    with pytest.raises(ceph_native.NativeFailure, match="native-command"):
        native.tunnel_bootstrap(ssh_material(private))
    assert calls == [["/usr/bin/systemctl", "is-enabled", "chrony.service"],
                     ["/usr/bin/systemctl", "is-active", "chrony.service"],
                     ["/usr/bin/systemctl", "is-enabled", "chronyd.service"],
                     ["/usr/bin/systemctl", "is-active", "chronyd.service"],
                     [native.tool, "check-host", "--expect-hostname", native.host["name"]]]
    assert native.markers.read("bootstrap-attempt.json") is None


OLD, CURRENT = "20.1.0-221.el9cp", "20.2.1-405.el9cp"


def bootstrap_world(native, private, call_home="enabled"):
    native.request["callHome"] = call_home
    native.daemons = lambda: []
    prepared_bootstrap(native, private)
    native.markers.records["transport-ready.json"] = {"fixture": True}
    calls, captured = [], []
    native.command = lambda argv, **kwargs: calls.append(argv)
    native.capture_bootstrap_outputs = lambda: captured.append(native.markers.read("bootstrap-attempt.json"))
    return calls, captured


def general_argv(native, scratch):
    intent = ceph_tunnel_staging.stage_intent(native.request)
    return [native.tool, "--image", native.request["image"], "bootstrap", "--fsid", native.request["fsid"], "--mon-id", intent["monID"],
            "--mgr-id", intent["mgrID"], "--mon-ip", native.request["monIP"], "--output-dir", native.markers.path, "--skip-ssh",
            "--skip-monitoring-stack", "--skip-dashboard", "--skip-admin-label", "--allow-fqdn-hostname", "--no-cleanup-on-failure",
            "--config", os.path.join(scratch, "ceph.conf"), "--automatically-accept-license"]


def scratch_of(argv):
    return os.path.dirname(argv[argv.index("--config") + 1])


def test_a_twenty_two_host_cephadm_bootstraps_with_todays_argv(native, private):
    calls, captured = bootstrap_world(native, private)
    assert native.tunnel_bootstrap(ssh_material(private)) == {"changed": True}
    assert calls == [general_argv(native, scratch_of(calls[0]))] and len(captured) == 1
    assert native.selection.applied == set()


def test_a_twenty_one_host_cephadm_bootstraps_without_the_license_flag_and_continues_to_capture(native, private):
    calls, captured = bootstrap_world(native, private, call_home="disabled")
    native.selection = built(OLD)
    assert native.tunnel_bootstrap(ssh_material(private)) == {"changed": True}
    assert calls == [general_argv(native, scratch_of(calls[0]))[:-1]] and "--skip-prepare-host" not in calls[0]
    assert captured == [ceph_tunnel_staging.recovery_scope(native.request)]
    assert native.selection.applied == {"ceph-bootstrap-without-license-flag", "ceph-call-home-opt-in"}
    recorded = json.dumps(native.markers.records, sort_keys=True)
    assert not any(record.id in recorded for record in native_specializations.SPECIALIZATIONS)


@pytest.mark.parametrize("shape", sorted(specs.UNREADABLE))
def test_an_unanchored_host_parser_keeps_the_flag_and_leaves_its_options_to_cephadm(native, private, shape):
    calls, captured = bootstrap_world(native, private)
    native.selection = built(specs.UNREADABLE[shape](), CURRENT)
    assert native.tunnel_bootstrap(ssh_material(private)) == {"changed": True}
    assert calls == [general_argv(native, scratch_of(calls[0]))] and len(captured) == 1


@pytest.mark.parametrize("option", ["--no-cleanup-on-failure", "--skip-admin-label", "--cluster-network"])
def test_an_anchored_parser_missing_a_bootstrap_option_refuses_before_the_attempt_record(native, private, option):
    native.request["clusterNetworks"] = ["198.51.100.0/24"]
    calls, captured = bootstrap_world(native, private)
    names = [name for name in specs.committed(CURRENT)["declared"] if name != option]
    native.selection = built(specs.zipapp(names, specs.IBM_OVERRIDE))
    with pytest.raises(ceph_native.NativeFailure, match="^native-option-undeclared$") as caught:
        native.tunnel_bootstrap(ssh_material(private))
    assert caught.value.native_error["cause"] == option and CURRENT in caught.value.native_error["coordinate"]
    assert calls == [] and captured == [] and native.markers.read("bootstrap-attempt.json") is None


def test_every_undeclared_option_is_named_once_in_order(native, private):
    calls, unused = bootstrap_world(native, private)
    names = [name for name in specs.committed(CURRENT)["declared"] if name not in ("--skip-ssh", "--mon-id")]
    native.selection = built(specs.zipapp(names, specs.IBM_OVERRIDE))
    with pytest.raises(ceph_native.NativeFailure, match="^native-option-undeclared$") as caught:
        native.tunnel_bootstrap(ssh_material(private))
    assert caught.value.native_error["cause"] == "--mon-id, --skip-ssh" and calls == []


def test_an_undeclared_option_refusal_after_matched_seams_carries_the_applied_ids(native, private):
    calls, unused = bootstrap_world(native, private, call_home="disabled")
    names = [name for name in specs.committed(OLD)["declared"] if name != "--mon-id"]
    native.selection = built(specs.zipapp(names, main=specs.parser_source(names)), OLD)
    with pytest.raises(ceph_native.NativeFailure, match="^native-option-undeclared$") as caught:
        native.tunnel_bootstrap(ssh_material(private))
    coordinate = caught.value.native_error["coordinate"]
    assert coordinate == native.native_coordinate() and native.request["image"] in coordinate and calls == []
    assert coordinate.endswith("; specializations ceph-bootstrap-without-license-flag,ceph-call-home-opt-in")


@pytest.mark.parametrize("host,image,call_home", [(CURRENT, CURRENT, "disabled"), (OLD, OLD, "enabled")])
def test_bootstrap_repeats_the_call_home_path_check_before_the_attempt_record(native, private, host, image, call_home):
    calls, captured = bootstrap_world(native, private, call_home=call_home)
    native.selection = built(host, image)
    with pytest.raises(ceph_native.Refusal, match="^call-home-unqualified$"):
        native.tunnel_bootstrap(ssh_material(private))
    assert calls == [] and captured == [] and native.markers.read("bootstrap-attempt.json") is None


class Unclaimed(Markers):
    is_owned = False

    def owned(self):
        return self.is_owned

    def create(self):
        self.is_owned = True
        self.write("owner.json", {"fixture": "owner"})


class Prepared:
    """A prepared host's retained payload and the profile that authenticates it."""

    def __init__(self, path, payload):
        path.mkdir(mode=0o700)
        self.path = str(path)
        ceph_native.private_write(os.path.join(self.path, ceph_host_runtime.PAYLOAD), payload)
        self.profile = {"version": ceph_host_runtime.PROFILE_VERSION, "cephadmSHA256": hashlib.sha256(payload).hexdigest()}

    def read(self, name):
        return copy.deepcopy(self.profile) if name == ceph_host_runtime.PROFILE else None


def claiming(request, markers, tmp_path, host, image, call_home):
    request = dict(request, callHome=call_home)
    worker = ceph_native.Worker(request, request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"), markers=markers)
    worker.daemons = lambda: []
    proofs, host_bytes = [], specs.synthesized(host)

    def verify():
        proofs.append("tool")
        worker._tool_bytes = host_bytes
        return {"fixture": "identity"}

    durable = Prepared(tmp_path / ("host-" + call_home + "-" + str(len(os.listdir(tmp_path)))), specs.synthesized(image))
    worker.verify_tool = verify
    worker.runtime_profile = lambda: proofs.append("profile")
    worker.host_preparation = lambda: durable
    return worker, proofs


@pytest.mark.parametrize("host,image,wrong,right,proofs", [
    (CURRENT, CURRENT, "disabled", "enabled", ["tool"]),
    (OLD, CURRENT, "disabled", "enabled", ["tool", "profile"]),
    (OLD, OLD, "enabled", "disabled", ["tool", "profile"]),
])
def test_claim_refuses_an_unqualified_call_home_path_before_any_cluster_marker(cluster_request, tmp_path, host, image, wrong, right, proofs):
    markers = Unclaimed(tmp_path / cluster_request["fsid"])
    worker, seen = claiming(cluster_request, markers, tmp_path, host, image, wrong)
    with pytest.raises(ceph_native.Refusal, match="^call-home-unqualified$"):
        worker.claim()
    assert markers.records == {} and not markers.owned() and seen == proofs
    worker, seen = claiming(cluster_request, markers, tmp_path, host, image, right)
    assert worker.claim() == {"changed": True}
    assert markers.writes == ["owner.json", "artifact.json", "bootstrap-scope.json"] and seen == proofs
    assert markers.records["bootstrap-scope.json"]["cluster"]["callHome"] == right


def test_an_owned_cluster_claim_leaves_its_bound_call_home_to_the_recovery_scope(cluster_request, tmp_path):
    markers = Unclaimed(tmp_path / cluster_request["fsid"])
    markers.is_owned = True
    worker, seen = claiming(cluster_request, markers, tmp_path, OLD, OLD, "enabled")
    assert worker.claim() == {"changed": False} and seen == ["tool"]


@pytest.mark.parametrize("changes", [{"image": "registry.example.test/other@sha256:" + "b" * 64}, {"imageID": "f" * 64}])
def test_an_image_payload_bound_to_another_image_refuses_runtime_profile_and_selects_nothing(cluster_request, tmp_path, changes):
    markers = Unclaimed(tmp_path / cluster_request["fsid"])
    worker, unused = claiming(cluster_request, markers, tmp_path, OLD, OLD, "disabled")
    del worker.runtime_profile
    worker.host_preparation = lambda: ceph_native.Worker.host_preparation(worker)
    worker.host_markers = HostMarkers({"scope.json": ceph_host_runtime.scope(worker.request, "node-1"),
                                       ceph_host_runtime.PROFILE: host_profile(worker.request, "node-1", **changes)})
    worker.command = lambda argv, **kwargs: image_inspection(worker.request)
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        worker.claim()
    assert worker.selection.applied == set() and markers.records == {}


def test_the_image_source_reads_the_payload_only_after_the_profile_binds_the_held_image(cluster_request, tmp_path):
    worker, proofs = claiming(cluster_request, Unclaimed(tmp_path / cluster_request["fsid"]), tmp_path, OLD, OLD, "disabled")
    durable = worker.host_preparation()
    order = []
    worker.runtime_profile = lambda: order.append("profile")
    worker.host_preparation = lambda: order.append("preparation") or durable
    assert ceph_native.Worker.image_payload(worker) == specs.synthesized(OLD) and order == ["profile", "preparation"]
    worker.runtime_profile = lambda: ceph_native.require(False, "runtime-profile")
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        ceph_native.Worker.image_payload(worker)
    assert proofs == []


PACKAGE_QUERY = "%{EPOCHNUM} %{VERSION} %{RELEASE} %{ARCH}"
SIGNED = {"cephadm": (2, "20.2.2", "349.el9cp", "noarch"), "ceph-common": (2, "20.2.2", "349.el9cp", "x86_64")}


class Decided:
    """The host package phase's frozen records, shaped as ceph_packages.prepare writes decision.json and complete.json.

    A plan root is {key, requested, package identity} (native_resolution.solve4) and the plan digest covers the plan without it
    (native_resolution.validate_plan). The rpm answers model `rpm -q --qf` and `rpm -qf --qf`, which print the header tags with no newline.
    """

    def __init__(self, requested, packages=None, complete=True):
        packages = SIGNED if packages is None else packages
        roots = [{"key": "ceph", "requested": requested, "package": {"name": name, "epoch": epoch, "version": version, "release": release,
                                                                     "architecture": architecture}}
                 for name, (epoch, version, release, architecture) in sorted(packages.items())]
        ansible = {"name": "cephadm-ansible", "epoch": 0, "version": "5.0.3", "release": "1.el9cp", "architecture": "noarch"}
        roots.append({"key": "ansible", "requested": "synthetic-ansible", "package": ansible})
        plan = {"format": "bootwright.native-plan-v1", "roots": roots, "packages": [], "actions": []}
        plan["digest"] = hashlib.sha256(ceph_native.canonical(plan)).hexdigest()
        self.records = {"decision.json": {"version": "ceph-package-decision-v1", "binding": "b" * 64, "plan": plan}}
        if complete:
            self.records["complete.json"] = {"version": "ceph-package-complete-v1", "binding": "b" * 64, "planDigest": plan["digest"]}
        self.installed = dict(packages)

    def read(self, name):
        return copy.deepcopy(self.records.get(name))

    def rpm(self, argv):
        if argv[:3] == ["/usr/bin/rpm", "-q", "--qf"] and argv[3] == PACKAGE_QUERY:
            return "{} {} {} {}".format(*self.installed[argv[4]]).encode()
        if argv[:4] == ["/usr/bin/rpm", "-qf", "--qf", "%{NAME} " + PACKAGE_QUERY] and argv[4] == "/usr/bin/ceph-authtool":
            return "ceph-common {} {} {} {}".format(*self.installed["ceph-common"]).encode()
        assert argv[:3] == ["/usr/bin/rpm", "--verify", "--noscripts"], argv
        return b""


def decided_worker(cluster_request, tmp_path, monkeypatch, decided, requested):
    cluster_request["cephadm"]["packageVersion"] = requested
    worker = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"),
                                markers=Unclaimed(tmp_path / cluster_request["fsid"]))
    worker.packages = decided
    commands = []
    worker.command = lambda argv, **kwargs: commands.append(argv) or decided.rpm(argv)
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, maximum: b"native " + path.encode())
    monkeypatch.setattr(ceph_native_resources, "read_regular", lambda path, maximum: b"native " + path.encode())
    return worker, commands


@pytest.mark.parametrize("requested", ["2:20.2.2-349.el9cp", "20.2.2", "20.2.2-349.el9cp"])
def test_installed_ceph_packages_are_the_frozen_decision_whatever_form_dnf_was_given(cluster_request, tmp_path, monkeypatch, requested):
    decided = Decided(requested)
    worker, commands = decided_worker(cluster_request, tmp_path, monkeypatch, decided, requested)
    identity = worker.verify_tool()
    assert set(identity) == {"sha256", "packageVersion", "clients"} and identity["packageVersion"] == requested
    assert [argv for argv in commands if "-q" in argv] == [["/usr/bin/rpm", "-q", "--qf", PACKAGE_QUERY, name] for name in ("cephadm", "ceph-common")]
    ceph_native_resources.ResourceWorker.verify_client_generator(worker)
    assert worker.markers.records["client-generator.json"]["packageVersion"] == requested
    assert set(worker.markers.records["client-generator.json"]) == {"path", "packageVersion", "sha256"}


@pytest.mark.parametrize("name", ["cephadm", "ceph-common"])
@pytest.mark.parametrize("drift", [(2, "20.2.2", "350.el9cp", None), (0, "20.2.2", "349.el9cp", None), (2, "20.2.2", "349.el9cp", "aarch64")])
def test_an_installed_ceph_package_other_than_the_decision_refuses_artifact(cluster_request, tmp_path, monkeypatch, name, drift):
    decided = Decided("20.2.2")
    epoch, version, release, architecture = drift
    decided.installed[name] = (epoch, version, release, architecture or SIGNED[name][3])
    worker, unused = decided_worker(cluster_request, tmp_path, monkeypatch, decided, "20.2.2")
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        worker.verify_tool()
    assert worker.markers.writes == []


def test_the_client_generator_follows_the_frozen_ceph_common_identity(cluster_request, tmp_path, monkeypatch):
    decided = Decided("20.2.2")
    worker, unused = decided_worker(cluster_request, tmp_path, monkeypatch, decided, "20.2.2")
    worker.verify_tool = lambda: None
    decided.installed["ceph-common"] = (2, "20.2.2", "350.el9cp", "x86_64")
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        ceph_native_resources.ResourceWorker.verify_client_generator(worker)
    decided.installed["ceph-common"] = SIGNED["ceph-common"]
    original = decided.rpm
    decided.rpm = lambda argv: original(argv).replace(b"ceph-common", b"ceph-base") if "-qf" in argv else original(argv)
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        ceph_native_resources.ResourceWorker.verify_client_generator(worker)
    assert "client-generator.json" not in worker.markers.records
    decided.rpm = original
    ceph_native_resources.ResourceWorker.verify_client_generator(worker)
    assert worker.markers.records["client-generator.json"]["packageVersion"] == "20.2.2"


def unfinished(decided):
    del decided.records["complete.json"]


def foreign_plan(decided):
    decided.records["complete.json"]["planDigest"] = "c" * 64


def other_binding(decided):
    decided.records["complete.json"]["binding"] = "c" * 64


def undecided(decided):
    decided.records.clear()


def tampered(decided):
    decided.records["decision.json"]["plan"]["roots"][0]["package"]["release"] = "350.el9cp"
    name = decided.records["decision.json"]["plan"]["roots"][0]["package"]["name"]
    decided.installed[name] = decided.installed[name][:2] + ("350.el9cp",) + decided.installed[name][3:]


def other_request(decided):
    plan = decided.records["decision.json"]["plan"]
    for root in plan["roots"]:
        root["requested"] = "20.2.1" if root["key"] == "ceph" else root["requested"]
    content = {key: value for key, value in plan.items() if key != "digest"}
    plan["digest"] = decided.records["complete.json"]["planDigest"] = hashlib.sha256(ceph_native.canonical(content)).hexdigest()


def missing_root(decided):
    plan = decided.records["decision.json"]["plan"]
    plan["roots"] = [root for root in plan["roots"] if root["package"]["name"] != "ceph-common"]
    content = {key: value for key, value in plan.items() if key != "digest"}
    plan["digest"] = decided.records["complete.json"]["planDigest"] = hashlib.sha256(ceph_native.canonical(content)).hexdigest()


def malformed(decided):
    decided.records["decision.json"]["plan"]["roots"] = "cephadm"


@pytest.mark.parametrize("damage", [unfinished, foreign_plan, other_binding, undecided, tampered, other_request, missing_root, malformed])
def test_an_unfinished_or_foreign_package_decision_refuses_artifact(cluster_request, tmp_path, monkeypatch, damage):
    decided = Decided("20.2.2")
    damage(decided)
    worker, unused = decided_worker(cluster_request, tmp_path, monkeypatch, decided, "20.2.2")
    with pytest.raises(ceph_native.Refusal, match="^artifact$"):
        worker.verify_tool()
    assert worker.markers.writes == []


def test_an_uninjected_worker_reads_the_decision_the_package_phase_wrote(cluster_request, tmp_path, monkeypatch):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_packages
    assert ceph_host_runtime.PACKAGE_ROOT == ceph_packages.ROOT
    decided = Decided(cluster_request["cephadm"]["packageVersion"])
    worker = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"),
                                markers=Unclaimed(tmp_path / cluster_request["fsid"]))
    assert worker.packages is None
    reads = []

    def journal(path, maximum=None, private=False):
        reads.append((path, private))
        return json.dumps(decided.records[os.path.basename(path)]).encode()

    monkeypatch.setattr(ceph_native, "read_regular", journal)
    assert worker.decided_package("cephadm") == "{} {} {} {}".format(*SIGNED["cephadm"]).encode()
    journal_dir = os.path.join(ceph_packages.ROOT, cluster_request["fsid"])
    assert reads == [(os.path.join(journal_dir, "decision.json"), True), (os.path.join(journal_dir, "complete.json"), True)]


def test_the_host_source_keeps_the_bytes_verify_tool_proved_and_verifies_once(cluster_request, tmp_path, monkeypatch):
    decided = Decided(cluster_request["cephadm"]["packageVersion"])
    worker = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"),
                                markers=Unclaimed(tmp_path / cluster_request["fsid"]))
    worker.packages = decided
    commands = []
    worker.command = lambda argv, **kwargs: commands.append(argv) or decided.rpm(argv)
    zipapp = specs.synthesized(CURRENT)
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, maximum: zipapp if path == worker.tool else b"client " + path.encode())
    identity = worker.verify_tool()
    assert identity["sha256"] == hashlib.sha256(zipapp).hexdigest() and set(identity) == {"sha256", "packageVersion", "clients"}
    assert len(commands) == 4
    assert worker.verified_tool() is zipapp and worker.verified_tool() is zipapp and len(commands) == 4
    assert worker.selection.variant("ceph.bootstrap.license-flag") == "pass" and len(commands) == 4


def test_the_host_source_verifies_the_tool_when_no_proof_has_run(cluster_request, tmp_path, monkeypatch):
    decided = Decided(cluster_request["cephadm"]["packageVersion"])
    worker = ceph_native.Worker(cluster_request, cluster_request["bootstrap"], run=lambda *args, **kwargs: pytest.fail("uninjected command"),
                                markers=Unclaimed(tmp_path / cluster_request["fsid"]))
    worker.packages = decided
    commands = []
    worker.command = lambda argv, **kwargs: commands.append(argv) or decided.rpm(argv)
    monkeypatch.setattr(ceph_native, "read_regular", lambda path, maximum: specs.synthesized(OLD) if path == worker.tool else b"client")
    assert worker.selection.variant("ceph.bootstrap.license-flag") == "omit" and len(commands) == 4
    assert worker.selection.declared("host-cephadm") == frozenset(specs.committed(OLD)["declared"]) and len(commands) == 4


FACT_SETS = {"20.2.x": (CURRENT, "enabled"), "20.1.0": (OLD, "disabled")}


@pytest.fixture(params=sorted(FACT_SETS))
def fact_set(request, native):
    """A negative safety test runs under a 20.2.x-shaped and a 20.1.0-shaped host and image, each with its qualified Call Home intent."""
    build, call_home = FACT_SETS[request.param]
    native.request["cephadm"]["packageVersion"] = build
    native.request["callHome"] = call_home
    native.selection = built(build)
    return build


def twenty_one_manager(native, modules):
    """The 20.1.0-221 MGR as its signed changes archive reads (research ceph-mgr D2-D4): set_ssh_user(user) declares no --skip-pre-steps
    (cephadm/module.py:1353-1355), orchestrator/module.py has no accept-license, enable-call-home or accept call-home-enabled, the MGR has
    no call_home_needs_acceptance option, and call_home_agent is not always on (MgrMonitor.cc:73-98)."""
    general = native.ceph

    def ceph(*args, **kwargs):
        if args == ("mgr", "module", "ls"):
            return copy.deepcopy(modules)
        if args == ("config", "dump"):
            return []
        if args[:2] in (("orch", "accept-license"), ("orch", "enable-call-home"), ("orch", "accept")) or "--skip-pre-steps" in args:
            raise ceph_native.NativeFailure("native-command", "synthetic 20.1.0 manager", b"EINVAL: invalid command")
        return general(*args, **kwargs)

    native.ceph = ceph


QUIET = {"enabled_modules": ["iostat", "nfs"], "always_on_modules": ["cephadm"], "force_disabled_modules": []}


@pytest.mark.parametrize("build", [CURRENT, "20.2.2-349.el9cp", "20.2.2-349.el10cp"])
def test_a_twenty_two_manager_receives_set_user_with_skip_pre_steps(native, private, build):
    native.request["cephadm"]["packageVersion"] = build
    unused, calls = configuration_fixture(native, private)
    assert native.tunnel_configure(ssh_material(private)) == {"changed": True}
    effects = [args for args, unused in calls if args[0] == "cephadm" or args[:2] in (("orch", "accept"), ("orch", "enable-call-home"))]
    assert effects[3] == ("cephadm", "set-user", "operator", "--skip-pre-steps") and ("orch", "accept", "call-home-enabled") in effects
    assert native.markers.read("call-home-accepted.json") == {"callHome": "enabled"} and native.selection.applied == set()


def test_a_twenty_one_manager_is_configured_without_pre_steps_or_any_call_home_step(native, private):
    native.request["cephadm"]["packageVersion"], native.request["callHome"] = OLD, "disabled"
    unused, calls = configuration_fixture(native, private)
    twenty_one_manager(native, QUIET)
    assert native.tunnel_configure(ssh_material(private)) == {"changed": True}
    effects = [args for args, unused in calls if args[0] == "cephadm" or args[:2] in (("orch", "accept"), ("orch", "enable-call-home"))]
    assert effects == [("cephadm", "set-priv-key", "-i", "-"), ("cephadm", "set-pub-key", "-i", "-"), ("cephadm", "set-ssh-config", "-i", "-"),
                       ("cephadm", "set-user", "operator")]
    assert not any("call_home" in name or "call-home" in name for name in native.markers.records)
    assert native.selection.applied == {"ceph-call-home-opt-in", "ceph-set-user-without-pre-steps"}
    assert native.configuration_complete() is True
    recorded = json.dumps(native.markers.records, sort_keys=True)
    assert not any(record.id in recorded for record in native_specializations.SPECIALIZATIONS)


@pytest.mark.parametrize("field", ["enabled_modules", "always_on_modules"])
def test_an_enabled_call_home_agent_on_an_opt_in_manager_is_drift(native, private, field):
    native.request["cephadm"]["packageVersion"], native.request["callHome"] = OLD, "disabled"
    unused, calls = configuration_fixture(native, private)
    twenty_one_manager(native, dict(QUIET, **{field: QUIET[field] + ["call_home_agent"]}))
    with pytest.raises(ceph_native.Refusal, match="^call-home-drift$"):
        native.tunnel_configure(ssh_material(private))
    assert native.markers.read("tunnel-configured.json") is None


@pytest.mark.parametrize("shape", sorted(specs.UNREADABLE))
def test_an_unanchored_image_payload_keeps_skip_pre_steps(native, private, shape):
    unused, calls = configuration_fixture(native, private)
    native.selection = built(CURRENT, specs.UNREADABLE[shape]())
    assert native.tunnel_configure(ssh_material(private)) == {"changed": True}
    assert ("cephadm", "set-user", "operator", "--skip-pre-steps") in [args for args, unused in calls]


def test_secrets_reach_the_manager_only_on_stdin_under_each_fact_set(native, private, fact_set):
    unused, calls = configuration_fixture(native, private)
    if fact_set == OLD:
        twenty_one_manager(native, QUIET)
    assert native.tunnel_configure(ssh_material(private)) == {"changed": True}
    effects = [(args, kwargs) for args, kwargs in calls if args[0] == "cephadm"]
    assert effects[0][1]["data"] == private["clusterPrivateKey"].encode() and effects[1][1]["data"] == public_key().encode()
    secrets = (private["clusterPrivateKey"], private["registryJSON"], "fixture-password")
    assert all(not any(secret in str(argument) for secret in secrets for argument in args) for args, unused in calls)
    assert not any(secret in json.dumps(native.markers.records) for secret in secrets)


@pytest.mark.usefixtures("fact_set")
def test_a_foreign_bound_setting_refuses_ownership_under_each_fact_set(native, private):
    state, calls = configuration_fixture(native, private)
    state["values"]["ssh_user"] = b"operator"
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.tunnel_configure(ssh_material(private))
    assert not any(args[0] == "cephadm" and args[1] == "set-user" for args, unused in calls)


@pytest.mark.usefixtures("fact_set")
def test_a_wrong_cluster_key_refuses_bootstrap_before_any_attempt_under_each_fact_set(native, private):
    calls, captured = bootstrap_world(native, private, call_home=native.request["callHome"])
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        native.tunnel_bootstrap(ssh_material(dict(private, clusterPrivateKey="different private fixture")))
    assert calls == [] and captured == [] and native.markers.read("bootstrap-attempt.json") is None
