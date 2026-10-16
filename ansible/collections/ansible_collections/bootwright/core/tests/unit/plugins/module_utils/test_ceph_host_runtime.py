"""Per-host Ceph preparation pulls, profiles, routes and identifies only from the frozen image.

Every provider response here is a synthetic protocol fixture. No test pulls an
image, starts a container, contacts a registry or reads a real host disk.
"""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import json
import os
import re

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime, ceph_host_route_probe as probe, ceph_native

IMAGE = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + "a" * 64
IMAGE_ID = "c" * 64
PASSWORD = "REGISTRY-PASSWORD-SENTINEL"
PAYLOAD = b"#!/usr/bin/python3\n# synthetic cephadm payload\n"


@pytest.fixture(autouse=True)
def injected_broker(monkeypatch):
    class Broker:
        environment = {}

        def __init__(self, directory, request, private, deadline, command):
            assert json.loads(private)["password"] == PASSWORD
            assert request["image"] == IMAGE

        def __enter__(self):
            return self

        def __exit__(self, *unused):
            return False

    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)


@pytest.fixture
def request_value():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/sda", "serial": "drive-scsi" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v5", "action": "cluster", "cluster": "example", "owner": "apply-123",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": IMAGE,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "5.0.3-1", "packageRepository": "https://publisher.example.test/ceph",
            "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "verified-tunnel",
            "clusterKeyRef": "cluster-key", "registryRef": "registry-key", "registry": "cp.icr.io", "callHome": "enabled",
            "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
            "services": [{"service_type": "mon", "placement": {"hosts": names}}, {"service_type": "mgr", "placement": {"hosts": names, "count": 2}},
                         {"service_type": "osd", "service_id": "data", "placement": {"hosts": names}, "spec": {"data_devices": {"paths": ["/dev/sda"]}}}]}


class Markers:
    def __init__(self, path, owned=True):
        path.mkdir(mode=0o700, parents=True)
        self.path = str(path)
        self.records = {}
        self.writes = []
        self.is_owned = owned

    def create(self):
        self.is_owned = True

    def owned(self):
        return self.is_owned

    def read(self, name="owner.json"):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)


class Clock:
    def __init__(self):
        self.value = 0.0
        self.pauses = []

    def now(self):
        return self.value

    def pause(self, seconds):
        self.pauses.append(seconds)
        self.value += seconds


class Podman:
    """A scripted engine; pulls follow the given outcomes and inspection the held image."""

    def __init__(self, request, outcomes=(), present=False):
        self.request = request
        self.outcomes = list(outcomes)
        self.present = present
        self.image = {"Id": IMAGE_ID, "Os": "linux", "Architecture": "amd64", "RepoDigests": [IMAGE], "Size": 1 << 30}
        self.calls = []
        self.authfiles = []

    def __call__(self, argv, timeout, limit):
        self.calls.append(list(argv))
        assert 0 < timeout and 0 < limit
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            assert argv[3] == IMAGE
            return (0, json.dumps([self.image]).encode()) if self.present else (125, b"")
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            auth = argv[argv.index("--authfile") + 1]
            info = os.stat(auth, follow_symlinks=False)
            with open(auth, "rb") as handle:
                self.authfiles.append((info.st_mode & 0o777, info.st_nlink, handle.read()))
            policy = argv[argv.index("--signature-policy") + 1]
            with open(policy, "rb") as handle:
                assert json.loads(handle.read()) == {"default": [{"type": "reject"}],
                                                     "transports": {"docker": {IMAGE.split("@", 1)[0]: [{"type": "insecureAcceptAnything"}]}}}
            outcome = self.outcomes.pop(0)
            if outcome == "ok":
                self.present = True
                return 0, b""
            return 125, b"private registry diagnostic " + PASSWORD.encode()
        raise AssertionError("unexpected command " + repr(argv))


def host_runtime(request, tmp_path, podman, clock=None, **options):
    clock = clock or Clock()
    markers = Markers(tmp_path / "host")
    return runtime.HostRuntime(request, "node-1", podman, clock=clock.now, pause=clock.pause, markers=markers, **options), markers, clock


def registry(request):
    return json.dumps({"url": request["registry"], "username": "registry-user", "password": PASSWORD})


def pull_argv_count(podman):
    return sum("pull" in argv for argv in podman.calls)


def test_a_pull_counts_only_once_inspection_proves_the_digest_and_platform(request_value, tmp_path):
    podman = Podman(request_value, ["ok"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    with worker.journal(create=True) as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    assert pull_argv_count(podman) == 1
    pulled = [argv for argv in podman.calls if "pull" in argv][0]
    assert pulled[:2] == ["/usr/bin/podman", "pull"] and "--tls-verify=true" in pulled and pulled[-1] == IMAGE
    assert pulled.index("--signature-policy") > pulled.index("pull") and pulled.count("--signature-policy") == 1
    assert markers.records["pulled.json"] == {"scope": runtime.scope(request_value, "node-1"), "imageID": IMAGE_ID}
    assert markers.records["pull-attempt-1.json"] == {"scope": runtime.scope(request_value, "node-1"), "attempt": 1}
    with worker.journal() as durable:
        assert worker.pull(durable, registry(request_value), 4285) is False
    assert pull_argv_count(podman) == 1


def test_pull_retries_stay_bounded_and_refuse_after_three_attempts(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "fail", "fail"])
    worker, markers, clock = host_runtime(request_value, tmp_path, podman)
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == 3 and clock.pauses == [runtime.PULL_PAUSE] * 2
    assert [name for name in markers.writes if name.startswith("pull-attempt")] == ["pull-attempt-1.json", "pull-attempt-2.json", "pull-attempt-3.json"]
    assert "pulled.json" not in markers.records
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with worker.journal() as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == 3


def test_a_pull_attempt_timeout_is_bounded_by_the_call_and_never_exceeds_1200_seconds(request_value, tmp_path):
    timeouts = []
    podman = Podman(request_value, ["ok"])

    def command(argv, timeout, limit):
        if "pull" in argv:
            timeouts.append(timeout)
        return podman(argv, timeout, limit)

    worker, unused, clock = host_runtime(request_value, tmp_path, command)
    with worker.journal(create=True) as durable:
        worker.pull(durable, registry(request_value), 4285)
    assert timeouts == [runtime.PULL_SECONDS]
    clock.value = 4285 - runtime.PULL_RESERVE - runtime.PULL_MINIMUM + 1
    other, unused_markers, unused_clock = host_runtime(request_value, tmp_path / "other", Podman(request_value, ["ok"]), clock)
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with other.journal(create=True) as durable:
            other.pull(durable, registry(request_value), 4285)


@pytest.mark.parametrize("present", [True, False])
def test_an_interrupted_pull_resumes_from_its_retained_attempts(request_value, tmp_path, present):
    podman = Podman(request_value, ["ok"], present=present)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    markers.records["pull-attempt-1.json"] = {"scope": runtime.scope(request_value, "node-1"), "attempt": 1}
    with worker.journal(create=True) as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    assert pull_argv_count(podman) == (0 if present else 1)
    assert ("pull-attempt-2.json" in markers.records) is not present
    assert markers.records["pulled.json"]["imageID"] == IMAGE_ID


def test_a_foreign_attempt_record_never_counts_toward_this_pull(request_value, tmp_path):
    podman = Podman(request_value, ["ok"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    markers.records["pull-attempt-1.json"] = {"scope": runtime.scope(dict(request_value, owner="apply-other"), "node-1"), "attempt": 1}
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == 0


@pytest.mark.parametrize("change", [{"Architecture": "arm64"}, {"Os": "windows"}, {"RepoDigests": ["cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + "b" * 64]},
                                    {"Id": "not-an-image-id"}, {"Size": 0}])
def test_a_pulled_digest_or_platform_mismatch_refuses(request_value, tmp_path, change):
    podman = Podman(request_value, ["ok"])
    podman.image.update(change)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    with pytest.raises(ceph_native.Refusal, match="^image-platform$"):
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == 1 and "pulled.json" not in markers.records


def test_native_pull_receives_only_an_empty_authfile(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "ok"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    with worker.journal(create=True) as durable:
        worker.pull(durable, registry(request_value), 4285)
    assert len(podman.authfiles) == 2
    for mode, links, data in podman.authfiles:
        assert mode == 0o600 and links == 1
        assert json.loads(data) == {"auths": {}}
    assert all(PASSWORD not in " ".join(argv) and "registry-user" not in " ".join(argv) for argv in podman.calls)
    assert PASSWORD not in json.dumps(markers.records)
    leftovers = [os.path.join(root, name) for root, unused_dirs, files in os.walk(markers.path) for name in files]
    assert leftovers == []


def test_a_killed_pulls_authfile_is_discarded_before_the_next_attempt(request_value, tmp_path):
    podman = Podman(request_value, ["ok"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    residue = os.path.join(markers.path, "pull-killed")
    os.mkdir(residue, 0o700)
    with open(os.path.join(residue, "auth.json"), "w", encoding="utf-8") as handle:
        handle.write(PASSWORD)
    markers.records["pull-attempt-1.json"] = {"scope": runtime.scope(request_value, "node-1"), "attempt": 1}
    with worker.journal(create=True) as durable:
        worker.pull(durable, registry(request_value), 4285)
    assert not os.path.exists(residue) and pull_argv_count(podman) == 1


@pytest.mark.parametrize("exit_case", ["pulled", "resumed", "exhausted"])
def test_every_pull_exit_discards_a_killed_pulls_authfile(request_value, tmp_path, exit_case):
    podman = Podman(request_value, [], present=exit_case != "exhausted")
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    used = 3 if exit_case == "exhausted" else 1
    for attempt in range(1, used + 1):
        markers.records["pull-attempt-%d.json" % attempt] = {"scope": runtime.scope(request_value, "node-1"), "attempt": attempt}
    if exit_case == "pulled":
        markers.records["pulled.json"] = {"scope": runtime.scope(request_value, "node-1"), "imageID": IMAGE_ID}
    residue = os.path.join(markers.path, "pull-killed")
    os.mkdir(residue, 0o700)
    with open(os.path.join(residue, "auth.json"), "w", encoding="utf-8") as handle:
        handle.write(PASSWORD)
    assert os.path.exists(os.path.join(residue, "auth.json"))
    with contextlib.ExitStack() as stack:
        if exit_case == "exhausted":
            stack.enter_context(pytest.raises(ceph_native.Refusal, match="^image-pull$"))
        with worker.journal(create=True) as durable:
            assert worker.pull(durable, registry(request_value), 4285) is (exit_case == "resumed")
    assert not os.path.exists(residue) and pull_argv_count(podman) == 0


def test_a_failed_pull_keeps_no_credential_on_disk(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "fail", "fail"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    with pytest.raises(ceph_native.Refusal, match="^image-pull$") as refused:
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert PASSWORD not in str(refused.value)
    for root, unused_dirs, files in os.walk(markers.path):
        assert "auth.json" not in files


def container_inspection(argv, identifier, image_id, started=False):
    """Podman 5.8's projection of the fixed create argv, as a protocol fixture.

    As podman reports it: no sysctls beyond the recorded create command, bind options
    with podman's defaults and without z once the start relabelled the source, and a
    HOSTNAME added to a started container's environment unless one is set.
    """
    values = {}
    for value in argv:
        if value.startswith("--") and "=" in value:
            key, item = value[2:].split("=", 1)
            values.setdefault(key, []).append(item)
    entry = argv.index("--entrypoint=/usr/bin/python3")
    command = argv[entry + 2:]
    volumes = values.get("volume", [])
    binds = [volume.replace(":ro,z", ":ro" if started else ":ro,z") + ",rprivate,nosuid,nodev,rbind" for volume in volumes]
    hostname = values.get("hostname", [identifier[:12]])[0]
    environment = list(values["env"])
    if started and not any(item.startswith("HOSTNAME=") for item in environment):
        environment.append("HOSTNAME=" + hostname)
    label = argv[argv.index("--label") + 1]
    key, expected = label.split("=", 1)
    return [{
        "Id": identifier, "Image": image_id, "EffectiveCaps": [], "BoundingCaps": [], "State": {"Status": "exited" if started else "created"},
        "Config": {"Labels": {key: expected}, "User": values["user"][0], "Entrypoint": ["/usr/bin/python3"], "Cmd": command, "WorkingDir": "/tmp",
                   "Hostname": hostname, "Env": environment, "OpenStdin": False, "Tty": False, "Timeout": 60, "StopTimeout": 1,
                   "CreateCommand": list(argv)},
        "HostConfig": {"ReadonlyRootfs": True, "Privileged": False, "CapAdd": [], "CapDrop": ["ALL"], "NetworkMode": "none",
                       "SecurityOpt": ["no-new-privileges"], "Binds": binds, "PidMode": "private", "IpcMode": "private",
                       "UTSMode": "private", "CgroupnsMode": "private", "Memory": 512 << 20, "MemorySwap": 512 << 20, "PidsLimit": 64,
                       "CpuPeriod": 100000, "CpuQuota": 100000, "Devices": [], "Tmpfs": {"/tmp": "rw,noexec,nosuid,nodev,size=16m,mode=1777"}},
        "Mounts": [{"Type": "bind", "RW": False, "Source": volume.split(":")[0], "Destination": volume.split(":")[1]} for volume in volumes],
    }]


class Engine:
    """A scripted container engine for contained probes."""

    def __init__(self, outputs, mutate=None, residue=False):
        self.outputs = list(outputs)
        self.mutate = mutate
        self.residue = residue
        self.calls = []
        self.created = {}
        self.started = set()
        self.removed = []

    def __call__(self, argv, timeout, limit):
        self.calls.append(list(argv))
        if argv[:2] == ["/usr/bin/podman", "create"]:
            identifier = hashlib.sha256(str(len(self.created)).encode()).hexdigest()
            self.created[identifier] = list(argv)
            return 0, identifier.encode() + b"\n"
        if argv[:3] == ["/usr/bin/podman", "container", "inspect"]:
            argv_created = self.created[argv[3]]
            image_id = argv_created[argv_created.index("--entrypoint=/usr/bin/python3") + 1]
            value = container_inspection(argv_created, argv[3], image_id, started=argv[3] in self.started)
            if self.mutate is not None:
                self.mutate(value[0])
            return 0, json.dumps(value).encode()
        if argv[:3] == ["/usr/bin/podman", "start", "--attach"]:
            self.started.add(argv[3])
            output = self.outputs.pop(0)
            return 0, output(self.created[argv[3]]) if callable(output) else output
        if argv[:3] == ["/usr/bin/podman", "container", "rm"]:
            self.removed.append(argv[-1])
            return 0, b""
        if argv[:3] == ["/usr/bin/podman", "container", "ls"]:
            return 0, (argv[5].split("=", 1)[1].encode() + b"\n") if self.residue else b""
        raise AssertionError("unexpected engine command " + repr(argv))


REQUIRED_FLAGS = ("--pull=never", "--network=none", "--read-only", "--cap-drop=all", "--unsetenv-all", "--security-opt=no-new-privileges",
                  "--pids-limit=64", "--memory=512m", "--memory-swap=512m", "--cpus=1", "--ipc=private", "--pid=private", "--uts=private",
                  "--cgroupns=private", "--image-volume=ignore", "--log-driver=none", "--restart=no", "--timeout=60", "--read-only-tmpfs=false",
                  "--hostname=bootwright-probe")


def contained_worker(request_value, tmp_path, engine):
    worker, markers, unused = host_runtime(request_value, tmp_path, engine)
    return worker, markers


def test_the_contained_probe_is_created_closed_and_proved_absent_afterwards(request_value, tmp_path):
    engine = Engine([b'{"ok":true}'])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    assert worker.contained(IMAGE_ID, (0, 0), "print(1)") == {"ok": True}
    created = [argv for argv in engine.calls if argv[:2] == ["/usr/bin/podman", "create"]][0]
    assert all(flag in created for flag in REQUIRED_FLAGS)
    assert not any(argv.startswith(("--volume", "--device", "--privileged", "--cap-add", "--env-file", "--secret")) for argv in created)
    assert {value for value in created if value.startswith("--env=")} == {"--env=HOME=/tmp", "--env=PATH=/usr/bin:/bin", "--env=LANG=C", "--env=LC_ALL=C",
                                                                          "--env=HOSTNAME=bootwright-probe"}
    mounts = created[[index for index, value in enumerate(created) if value.startswith("--default-mounts-file=")][0]].split("=", 1)[1]
    assert os.path.basename(mounts) == "mounts.conf" and not os.path.exists(mounts)
    phases = [argv[1] if argv[1] != "container" else argv[2] for argv in engine.calls]
    assert phases == ["create", "inspect", "start", "inspect", "rm", "ls"]
    assert engine.removed == list(engine.created)


def test_a_started_probe_with_mounts_and_a_sysctl_is_proved_as_podman_reports_it(request_value, tmp_path):
    shared = tmp_path / "shared"
    shared.mkdir()
    engine = Engine([b'{"ok":true}'])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    sysctls = (("net.ipv4.ip_unprivileged_port_start", "0"),)
    assert worker.contained(IMAGE_ID, (167, 167), "print(1)", mounts=((str(shared), "/run/probe"),), sysctls=sysctls) == {"ok": True}
    assert len(engine.started) == 1 and engine.removed == list(engine.created)
    reported = container_inspection(engine.created[engine.removed[0]], engine.removed[0], IMAGE_ID, started=True)[0]
    assert "HOSTNAME=bootwright-probe" in reported["Config"]["Env"] and "Sysctls" not in reported["HostConfig"]
    assert reported["HostConfig"]["Binds"] == [str(shared) + ":/run/probe:ro,rprivate,nosuid,nodev,rbind"]


BIND_DRIFT = {
    "writable": lambda value: value["HostConfig"].update(Binds=[bind.replace(":ro", ":rw") for bind in value["HostConfig"]["Binds"]]),
    "foreign-option": lambda value: value["HostConfig"].update(Binds=[bind + ",exec" for bind in value["HostConfig"]["Binds"]]),
    "unlabelled": lambda value: value["HostConfig"].update(Binds=[bind.replace(",z", "") for bind in value["HostConfig"]["Binds"]]),
    "sysctl": lambda value: value["Config"]["CreateCommand"].append("--sysctl=net.ipv4.ip_forward=1"),
    "missing-sysctl": lambda value: value["Config"].update(CreateCommand=[argument for argument in value["Config"]["CreateCommand"]
                                                                          if not argument.startswith("--sysctl")]),
}


@pytest.mark.parametrize("drift", sorted(BIND_DRIFT))
def test_a_created_probe_with_mounts_or_sysctls_refuses_their_drift(request_value, tmp_path, drift):
    shared = tmp_path / "shared"
    shared.mkdir()
    fired = []

    def mutate(value):
        if value["State"]["Status"] == "created":
            fired.append(drift)
            BIND_DRIFT[drift](value)

    engine = Engine([b'{"ok":true}'], mutate=mutate)
    worker, unused = contained_worker(request_value, tmp_path, engine)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.contained(IMAGE_ID, (167, 167), "print(1)", mounts=((str(shared), "/run/probe"),),
                         sysctls=(("net.ipv4.ip_unprivileged_port_start", "0"),))
    assert fired and not engine.started and engine.removed == list(engine.created)


INJECTIONS = {
    "network": lambda value: value["HostConfig"].update(NetworkMode="bridge"),
    "writable-root": lambda value: value["HostConfig"].update(ReadonlyRootfs=False),
    "privileged": lambda value: value["HostConfig"].update(Privileged=True),
    "capability": lambda value: value["HostConfig"].update(CapAdd=["CAP_NET_RAW"]),
    "effective-capability": lambda value: value.update(EffectiveCaps=["CAP_CHOWN"]),
    "bounding-capability": lambda value: value.update(BoundingCaps=["CAP_CHOWN"]),
    "new-privileges": lambda value: value["HostConfig"].update(SecurityOpt=[]),
    "credential-mount": lambda value: value["Mounts"].append({"Type": "bind", "RW": False, "Source": "/etc/pki/entitlement",
                                                              "Destination": "/run/secrets"}),
    "bind": lambda value: value["HostConfig"].update(Binds=["/root:/root:ro,z"]),
    "environment": lambda value: value["Config"]["Env"].append("REGISTRY_AUTH_FILE=/run/auth.json"),
    "user": lambda value: value["Config"].update(User="0:0" if value["Config"]["User"] != "0:0" else "167:167"),
    "entrypoint": lambda value: value["Config"].update(Entrypoint=["/bin/sh"]),
    "command": lambda value: value["Config"].update(Cmd=["-c", "foreign"]),
    "image": lambda value: value.update(Image="d" * 64),
    "label": lambda value: value["Config"].update(Labels={}),
    "memory": lambda value: value["HostConfig"].update(Memory=0),
    "swap": lambda value: value["HostConfig"].update(MemorySwap=-1),
    "pids": lambda value: value["HostConfig"].update(PidsLimit=0),
    "cpu": lambda value: value["HostConfig"].update(CpuQuota=0),
    "pid-namespace": lambda value: value["HostConfig"].update(PidMode="host"),
    "ipc-namespace": lambda value: value["HostConfig"].update(IpcMode="host"),
    "uts-namespace": lambda value: value["HostConfig"].update(UTSMode="host"),
    "cgroup-namespace": lambda value: value["HostConfig"].update(CgroupnsMode="host"),
    "device": lambda value: value["HostConfig"].update(Devices=[{"PathOnHost": "/dev/sda"}]),
    "tmpfs-exec": lambda value: value["HostConfig"].update(Tmpfs={"/tmp": "rw,nosuid,nodev,size=16m,mode=1777"}),
    "extra-tmpfs": lambda value: value["HostConfig"]["Tmpfs"].update({"/run": "rw"}),
    "stdin": lambda value: value["Config"].update(OpenStdin=True),
    "timeout": lambda value: value["Config"].update(Timeout=0),
    "sysctl": lambda value: value["HostConfig"].update(Sysctls={"net.ipv4.ip_forward": "1"}),
    "requested-sysctl": lambda value: value["Config"]["CreateCommand"].append("--sysctl=net.ipv4.ip_forward=1"),
    "hostname": lambda value: value["Config"].update(Hostname="foreign"),
    "hostname-environment": lambda value: value["Config"].update(Env=[item for item in value["Config"]["Env"] if not item.startswith("HOSTNAME=")]),
    "duplicate-environment": lambda value: value["Config"]["Env"].append("HOME=/tmp"),
}


@pytest.mark.parametrize("injection", sorted(INJECTIONS))
def test_each_contained_probe_configuration_drift_refuses_before_the_probe_runs(request_value, tmp_path, injection):
    fired = []

    def mutate(value):
        fired.append(injection)
        INJECTIONS[injection](value)

    engine = Engine([b'{"ok":true}'], mutate=mutate)
    worker, unused = contained_worker(request_value, tmp_path, engine)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.contained(IMAGE_ID, (167, 167), "print(1)")
    assert fired and not any(argv[:2] == ["/usr/bin/podman", "start"] for argv in engine.calls)
    assert engine.removed == list(engine.created) and engine.calls[-1][:3] == ["/usr/bin/podman", "container", "ls"]


def test_a_container_left_behind_refuses(request_value, tmp_path):
    engine = Engine([b'{"ok":true}'], residue=True)
    worker, unused = contained_worker(request_value, tmp_path, engine)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.contained(IMAGE_ID, (167, 167), "print(1)")
    assert len(engine.removed) == 1


def probe_output(payload=PAYLOAD, **changes):
    def output(created):
        user = [value for value in created if value.startswith("--user=")][0].split("=", 1)[1]
        uid, gid = (int(value) for value in user.split(":"))
        artifacts = {path: {"sha256": hashlib.sha256(payload).hexdigest() if path == "/usr/sbin/cephadm" else "e" * 64,
                            "bytes": len(payload) if path == "/usr/sbin/cephadm" else 100, "mode": 0o755, "uid": 0, "gid": 0}
                     for path in runtime.ARTIFACTS}
        value = {"version": "ceph-host-image-v1", "uid": uid, "gid": gid, "cephUID": 167, "cephGID": 167, "machine": "x86_64",
                 "artifacts": artifacts, "cephadm": base64.b64encode(payload).decode(), "cryptoPolicy": "Ciphers aes256-gcm@openssh.com\n"}
        value.update(changes)
        return json.dumps(value).encode()
    return output


@pytest.fixture
def authenticated(monkeypatch):
    digest = hashlib.sha256(PAYLOAD).hexdigest()
    monkeypatch.setattr(runtime, "installed_cephadm", lambda request: digest)
    return digest


def test_facts_come_from_two_agreeing_probes_as_root_and_as_the_image_account(request_value, tmp_path, authenticated):
    engine = Engine([probe_output(), probe_output()])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    facts = worker.facts(IMAGE_ID)
    assert facts["cephUID"] == facts["cephGID"] == 167
    users = [[value for value in argv if value.startswith("--user=")][0] for argv in engine.calls if argv[:2] == ["/usr/bin/podman", "create"]]
    assert users == ["--user=0:0", "--user=167:167"]
    assert hashlib.sha256(runtime.decode_payload(facts["cephadm"])).hexdigest() == authenticated


@pytest.mark.parametrize("changes", [{"cephUID": 0}, {"cephGID": 168}, {"machine": "aarch64"}, {"cryptoPolicy": "Include /etc/foreign\n"},
                                     {"cephadm": base64.b64encode(b"foreign cephadm").decode()}, {"version": "ceph-fixture-image-v1"}])
def test_a_contradicting_probe_refuses(request_value, tmp_path, authenticated, changes):
    del authenticated
    engine = Engine([probe_output(**changes), probe_output()])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.facts(IMAGE_ID)
    assert len(engine.removed) == 1


def test_probes_that_disagree_refuse(request_value, tmp_path, authenticated):
    del authenticated
    engine = Engine([probe_output(), probe_output(cryptoPolicy="Ciphers aes128-gcm@openssh.com\n")])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.facts(IMAGE_ID)


def profiled_runtime(request_value, tmp_path, monkeypatch, outputs, *, label=runtime.SHARED_LABEL):
    podman = Podman(request_value, ["ok"])
    engine = Engine(outputs)

    def command(argv, timeout, limit):
        if argv[:2] == ["/usr/bin/podman", "image"] or "pull" in argv:
            return podman(argv, timeout, limit)
        return engine(argv, timeout, limit)

    worker, markers, clock = host_runtime(request_value, tmp_path, command)
    monkeypatch.setattr(runtime.HostRuntime, "label", lambda self, image_id, user, parent: label)
    return worker, markers, podman, engine


def test_profile_records_the_image_runtime_and_the_authenticated_payload(request_value, tmp_path, monkeypatch, authenticated):
    worker, markers, podman, unused = profiled_runtime(request_value, tmp_path, monkeypatch, [probe_output(), probe_output()])
    assert worker.profile({"registryJSON": registry(request_value)}) == {"changed": True}
    record = markers.records[runtime.PROFILE]
    assert record["imageID"] == IMAGE_ID and record["uid"] == record["gid"] == 167 and record["label"] == runtime.SHARED_LABEL
    assert record["cephadmSHA256"] == authenticated and PASSWORD not in json.dumps(markers.records)
    assert runtime.cephadm_payload(markers) == PAYLOAD
    assert worker.profile({"registryJSON": registry(request_value)}) == {"changed": False}
    assert pull_argv_count(podman) == 1


def test_profile_authenticates_image_bytes_independently_of_installed_package(request_value, tmp_path, monkeypatch, authenticated):
    del authenticated
    monkeypatch.setattr(runtime, "installed_cephadm", lambda request: "f" * 64)
    worker, markers, unused, unused_engine = profiled_runtime(request_value, tmp_path, monkeypatch, [probe_output(), probe_output()])
    assert worker.profile({"registryJSON": registry(request_value)}) == {"changed": True}
    assert markers.records[runtime.PROFILE]["cephadmSHA256"] == hashlib.sha256(PAYLOAD).hexdigest()


@pytest.mark.parametrize("private", [{}, {"registryJSON": "x", "extra": "y"}])
def test_profile_accepts_only_the_registry_material(request_value, tmp_path, monkeypatch, private):
    worker, markers, podman, unused = profiled_runtime(request_value, tmp_path, monkeypatch, [])
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        worker.profile(private)
    assert podman.calls == [] and markers.writes == []


def test_the_label_probe_requires_enforcing_selinux(request_value, tmp_path):
    calls = []

    def command(argv, timeout, limit):
        calls.append(argv)
        return 0, b"Permissive\n"

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.label(IMAGE_ID, (167, 167), markers.path)
    assert calls == [["/usr/sbin/getenforce"]]


def test_the_label_probe_refuses_a_shared_file_without_the_container_label(request_value, tmp_path, monkeypatch):
    def command(argv, timeout, limit):
        return 0, b"Enforcing\n"

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    probed = []
    monkeypatch.setattr(worker, "contained", lambda image_id, user, script, **options: probed.append(options["mounts"]) or
                        {"read": True, "readOnly": True, "uid": 167, "gid": 167})
    monkeypatch.setattr(runtime.os, "getxattr", lambda fd, name: b"unconfined_u:object_r:user_tmp_t:s0\x00")
    with pytest.raises(ceph_native.Refusal, match="^image-profile$"):
        worker.label(IMAGE_ID, (167, 167), markers.path)
    assert len(probed) == 1 and probed[0][0][1] == "/run/bootwright-label"
    assert os.listdir(markers.path) != []


def seed_profile(markers, request_value, **changes):
    markers.records["scope.json"] = runtime.scope(request_value, "node-1")
    record = {"version": runtime.PROFILE_VERSION, "scope": runtime.scope(request_value, "node-1"), "image": IMAGE, "host": "node-1", "imageID": IMAGE_ID,
              "uid": 167, "gid": 167, "policy": "Ciphers aes256-gcm@openssh.com\n", "label": runtime.SHARED_LABEL,
              "cephadmSHA256": hashlib.sha256(PAYLOAD).hexdigest(),
              "artifacts": {path: hashlib.sha256(PAYLOAD).hexdigest() if path == "/usr/sbin/cephadm" else "e" * 64 for path in runtime.ARTIFACTS}}
    record.update(changes)
    markers.records[runtime.PROFILE] = record


def route_result(**changes):
    cases = {name: dict(zip(probe.COUNTERS, values)) for name, values in runtime.EXPECTED_CASES.items()}
    value = {"schema": probe.SCHEMA, "ok": True, "scope": "native-ssh-class-loopback", "dependencyStubs": probe.STUBS, "cases": cases}
    value.update(changes)
    return value


@pytest.mark.parametrize("case,counter", [("wrongKey", "directConnections"), ("unknownTarget", "directConnections"),
                                          ("unavailableProxy", "directConnections"), ("missingNativeConfig", "directConnections"),
                                          ("wrongKey", "effects"), ("unknownTarget", "forwardings"), ("unavailableProxy", "innerConnections"),
                                          ("missingNativeConfig", "outerAuthentications")])
def test_each_negative_route_case_with_any_contact_refuses(case, counter):
    value = route_result()
    value["cases"][case][counter] += 1
    with pytest.raises(ceph_native.Refusal, match="^route-probe$"):
        runtime.route_cases(value)
    assert runtime.route_cases(route_result()) == route_result()["cases"]


def test_route_records_only_the_expected_outcome_of_the_image_native_class(request_value, tmp_path, monkeypatch):
    if os.getuid() != os.getgid():
        pytest.skip("the synthetic bundle is owned by one uid and gid")
    podman = Podman(request_value, present=True)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    seed_profile(markers, request_value, uid=os.getuid(), gid=os.getgid())
    calls = []

    def contained(image_id, user, script, arguments=(), **options):
        calls.append((image_id, user, arguments[0], sorted(target for unused, target in options.get("mounts", ()))))
        if arguments[0] == "prepare":
            return {"schema": probe.PREPARE_SCHEMA, "ok": True, "synthetic": synthetic(user)}
        options["proof"]()
        return route_result()

    monkeypatch.setattr(worker, "contained", contained)
    monkeypatch.setattr(runtime.ceph_tunnel, "_BOOTWRIGHT_SOURCE", "fixed test helper\n", raising=False)
    assert worker.route() == {"changed": True}
    assert [call[2] for call in calls] == ["prepare", "exercise"]
    assert calls[1][3] == sorted([probe.SYNTHETIC_ROOT, runtime.ceph_tunnel.CONTAINER_ROOT])
    assert markers.records[runtime.ROUTE]["cases"] == route_result()["cases"]
    assert worker.route() == {"changed": False} and len(calls) == 2


def synthetic(user):
    keys = {}
    for index, name in enumerate(probe.KEYS):
        blob = (11).to_bytes(4, "big") + b"ssh-ed25519" + (32).to_bytes(4, "big") + bytes([index + 1]) * 32
        keys[name] = {"private": "-----BEGIN OPENSSH PRIVATE KEY-----\nsynthetic-" + name + "\n-----END OPENSSH PRIVATE KEY-----\n",
                      "public": "ssh-ed25519 " + base64.b64encode(blob).decode()}
    return {"schema": probe.SYNTHETIC_SCHEMA, "uid": user[0], "gid": user[1], "user": probe.USER, "cryptoPolicy": "", "keys": keys}


def test_a_failed_route_probe_records_nothing(request_value, tmp_path, monkeypatch):
    if os.getuid() != os.getgid():
        pytest.skip("the synthetic bundle is owned by one uid and gid")
    podman = Podman(request_value, present=True)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    seed_profile(markers, request_value, uid=os.getuid(), gid=os.getgid())
    failed = route_result()
    failed["cases"]["missingNativeConfig"]["directConnections"] = 1
    monkeypatch.setattr(worker, "contained", lambda image_id, user, script, arguments=(), **options:
                        {"schema": probe.PREPARE_SCHEMA, "ok": True, "synthetic": synthetic(user)} if arguments[0] == "prepare" else failed)
    monkeypatch.setattr(runtime.ceph_tunnel, "_BOOTWRIGHT_SOURCE", "fixed test helper\n", raising=False)
    with pytest.raises(ceph_native.Refusal, match="^route-probe$"):
        worker.route()
    assert runtime.ROUTE not in markers.records


SCSI_SSD = ("DEVNAME=/dev/sda\nMAJOR=8\nMINOR=0\nID_BUS=scsi\nID_VENDOR=QEMU\nID_MODEL=QEMU_HARDDISK\nID_MODEL_ENC=QEMU\\x20HARDDISK\n"
            "ID_SCSI_SERIAL=drive-scsi1\nID_SERIAL=0QEMU_QEMU_HARDDISK_drive-scsi1\nID_SERIAL_SHORT=drive-scsi1\n")
VIRTIO = "DEVNAME=/dev/vdb\nMAJOR=252\nMINOR=16\nID_SERIAL=disk1\n"


def observed(major=8, minor=0, serial="drive-scsi1", path="/dev/sda"):
    return {"path": path, "serial": serial, "sizeBytes": 1024 ** 3, "rotational": 0, "major": major, "minor": minor}


def test_a_scsi_ssd_yields_the_vendor_model_serial_native_form():
    values = runtime.properties(SCSI_SSD.encode(), observed())
    assert runtime.native_form(values) == "QEMU_QEMU_HARDDISK_drive-scsi1"


def test_a_virtio_disk_yields_its_serial_native_form():
    values = runtime.properties(VIRTIO.encode(), observed(252, 16, "disk1", "/dev/vdb"))
    assert runtime.native_form(values) == "disk1"


@pytest.mark.parametrize("raw,device", [
    (SCSI_SSD.replace("MINOR=0", "MINOR=16"), observed()),
    (SCSI_SSD.replace("MAJOR=8\n", ""), observed()),
    (SCSI_SSD.replace("ID_SCSI_SERIAL=drive-scsi1", "ID_SCSI_SERIAL=guessed"), observed()),
    (SCSI_SSD.replace("ID_MODEL=QEMU_HARDDISK", "ID_MODEL=QEMU__HARDDISK"), observed()),
    (SCSI_SSD.replace("ID_MODEL=QEMU_HARDDISK", "ID_MODEL=LVM PV x"), observed()),
    (SCSI_SSD + "ID_VENDOR=OTHER\n", observed()),
    (VIRTIO.replace("ID_SERIAL=disk1", "ID_SERIAL=MTFDdisk1"), observed(252, 16, "MTFDdisk1", "/dev/vdb")),
    ("not a property line\n", observed()),
])
def test_unbound_or_ambiguous_udev_properties_refuse(raw, device):
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        runtime.properties(raw.encode(), device)


def lsblk(path="/dev/sda", serial="drive-scsi1", size=1024 ** 3, rota=False, kind="disk", pair="8:0"):
    return json.dumps({"blockdevices": [{"path": path, "type": kind, "serial": serial, "size": size, "rota": rota, "maj:min": pair}]}).encode()


@pytest.mark.parametrize("change", [{"rota": True}, {"kind": "part"}, {"serial": "other"}, {"size": 1}, {"pair": "x"}, {"path": "/dev/sdb"}])
def test_the_device_must_be_the_declared_nonrotational_disk(request_value, tmp_path, monkeypatch, change):
    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: path)
    worker, unused, unused_clock = host_runtime(request_value, tmp_path, lambda argv, timeout, limit: (0, lsblk(**change)))
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        worker.block_device(request_value["hosts"][0]["devices"][0])


def identity_runtime(request_value, tmp_path, monkeypatch, sdk="QEMU_QEMU_HARDDISK_drive-scsi1", udev=SCSI_SSD):
    podman = Podman(request_value, present=True)
    engine = Engine([json.dumps({"version": "ceph-host-device-id-v1", "deviceID": sdk,
                                 "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}).encode()])
    reads = {"udev": udev}

    def command(argv, timeout, limit):
        if argv[0] == "/usr/bin/lsblk":
            return 0, lsblk()
        if argv[0] == "/usr/bin/udevadm":
            return 0, reads["udev"].encode()
        if argv[:2] == ["/usr/bin/podman", "image"]:
            return podman(argv, timeout, limit)
        return engine(argv, timeout, limit)

    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: path)
    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    seed_profile(markers, request_value)
    return worker, markers, engine, reads


def test_device_identity_comes_from_the_image_sdk_and_agrees_with_the_native_form(request_value, tmp_path, monkeypatch):
    worker, markers, engine, unused = identity_runtime(request_value, tmp_path, monkeypatch)
    assert worker.device_identity() == {"changed": True}
    record = markers.records[runtime.DEVICES]
    assert record["devices"] == [{"host": "node-1", "path": "/dev/sda", "serial": "drive-scsi1", "deviceID": "QEMU_QEMU_HARDDISK_drive-scsi1"}]
    created = [argv for argv in engine.calls if argv[:2] == ["/usr/bin/podman", "create"]][0]
    sdk_input = json.loads(created[-1])
    assert sdk_input["path"] == "/dev/sda" and "MAJOR" not in sdk_input["environment"] and sdk_input["environment"]["ID_SCSI_SERIAL"] == "drive-scsi1"
    assert worker.device_identity() == {"changed": False}
    assert len([argv for argv in engine.calls if argv[:2] == ["/usr/bin/podman", "create"]]) == 1
    assert worker.host_devices() == {"devices": record["devices"]}


def test_an_sdk_identity_that_contradicts_the_native_form_refuses(request_value, tmp_path, monkeypatch):
    worker, markers, unused, unused_reads = identity_runtime(request_value, tmp_path, monkeypatch, sdk="drive-scsi1")
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        worker.device_identity()
    assert runtime.DEVICES not in markers.records


def test_retained_identities_refuse_when_the_device_changed_since(request_value, tmp_path, monkeypatch):
    worker, markers, unused, reads = identity_runtime(request_value, tmp_path, monkeypatch)
    worker.device_identity()
    reads["udev"] = SCSI_SSD.replace("ID_VENDOR=QEMU", "ID_VENDOR=OTHER")
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        worker.device_identity()


@pytest.mark.parametrize("change", ["missing", "scope", "image", "serial", "extra"])
def test_a_missing_or_contradicting_identity_file_refuses(request_value, tmp_path, monkeypatch, change):
    worker, markers, unused, unused_reads = identity_runtime(request_value, tmp_path, monkeypatch)
    worker.device_identity()
    record = markers.records[runtime.DEVICES]
    if change == "missing":
        del markers.records[runtime.DEVICES]
    elif change == "scope":
        record["scope"] = runtime.scope(request_value, "node-2")
    elif change == "image":
        record["imageID"] = "f" * 64
    elif change == "serial":
        record["devices"][0]["serial"] = "guessed"
    else:
        record["devices"].append(dict(record["devices"][0]))
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        runtime.retained_devices(request_value, "node-1", markers, IMAGE_ID)


def test_host_observation_is_read_only_and_reports_absence_without_markers(request_value, tmp_path):
    podman = Podman(request_value, present=True)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman, packages=Markers(tmp_path / "packages", owned=False))
    markers.is_owned = False
    evidence = worker.host_observe()
    assert evidence["ready"] is False and evidence["imageID"] == "" and evidence["devices"] == [] and podman.calls == []
    assert markers.writes == []


def test_host_observation_reports_a_prepared_host(request_value, tmp_path):
    podman = Podman(request_value, present=True)
    packages = Markers(tmp_path / "packages")
    packages.records["complete.json"] = {"version": "ceph-package-complete-v1", "binding": "b" * 64, "planDigest": "d" * 64}
    worker, markers, unused = host_runtime(request_value, tmp_path, podman, packages=packages)
    seed_profile(markers, request_value)
    markers.records[runtime.ROUTE] = {"version": runtime.ROUTE_VERSION, "scope": runtime.scope(request_value, "node-1"), "imageID": IMAGE_ID,
                                      "sources": {path: "e" * 64 for path in runtime.SOURCE_PATHS}, "cases": route_result()["cases"]}
    markers.records[runtime.DEVICES] = {"version": runtime.DEVICES_VERSION, "scope": runtime.scope(request_value, "node-1"), "imageID": IMAGE_ID,
                                        "devices": [{"host": "node-1", "path": "/dev/sda", "serial": "drive-scsi1",
                                                     "deviceID": "QEMU_QEMU_HARDDISK_drive-scsi1"}]}
    evidence = worker.host_observe()
    assert evidence["ready"] is True and evidence["planDigest"] == "d" * 64 and evidence["route"] is True
    assert evidence["policySHA256"] == hashlib.sha256(b"Ciphers aes256-gcm@openssh.com\n").hexdigest()
    assert markers.writes == [] and packages.writes == []
    markers.records[runtime.ROUTE]["cases"]["wrongKey"]["directConnections"] = 1
    with pytest.raises(ceph_native.Refusal, match="^route-probe$"):
        worker.host_observe()
    markers.records[runtime.ROUTE] = "not a record"
    with pytest.raises(ceph_native.Refusal, match="^route-probe$"):
        worker.host_observe()
    podman.image["Id"] = "f" * 64
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        worker.host_observe()


@pytest.mark.parametrize("phase", ["profile", "route", "device_identity"])
def test_host_phases_refuse_an_unsupported_release_before_any_effect(request_value, phase):
    request_value["clusterSSHTransport"] = "native"
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        runtime.dispatch({"request": request_value, "host": "node-1", "phase": phase, "private": {"registryJSON": "x"} if phase == "profile" else {}})


def test_only_the_long_phase_receives_private_material(request_value):
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        runtime.dispatch({"request": request_value, "host": "node-1", "phase": "route", "private": {"registryJSON": "x"}})


class CacheWorker:
    def __init__(self, request_value, tmp_path, payload=PAYLOAD):
        self.request = request_value
        self.host = request_value["hosts"][1]
        self.markers = Markers(tmp_path / "cluster")
        self.host_markers = Markers(tmp_path / "prepared")
        self.payload = payload

    def host_preparation(self):
        return self.host_markers

    def command(self, argv, timeout=60, json_result=True):
        assert argv[:3] == ["/usr/bin/podman", "image", "inspect"] and json_result is False
        return json.dumps([{"Id": IMAGE_ID, "Os": "linux", "Architecture": "amd64", "RepoDigests": [IMAGE], "Size": 1 << 30}]).encode()


@pytest.fixture
def cache_worker(request_value, tmp_path, monkeypatch, authenticated):
    del authenticated
    worker = CacheWorker(request_value, tmp_path)
    seed_profile(worker.host_markers, request_value)
    worker.host_markers.records["scope.json"] = runtime.scope(request_value, "node-2")
    worker.host_markers.records[runtime.PROFILE].update(scope=runtime.scope(request_value, "node-2"), host="node-2")
    with open(os.path.join(worker.host_markers.path, runtime.PAYLOAD), "wb") as handle:
        handle.write(PAYLOAD)
    os.chmod(os.path.join(worker.host_markers.path, runtime.PAYLOAD), 0o600)
    directory = tmp_path / "data"
    directory.mkdir(mode=0o700)
    original = runtime.cache_descriptor

    def owned_by_this_process(value, ready=False):
        if isinstance(value, dict) and value.get("uid") == os.geteuid() and value.get("gid") == os.getegid():
            value = dict(value, uid=0, gid=0)
        original(value, ready=ready)

    monkeypatch.setattr(runtime, "cache_descriptor", owned_by_this_process)

    def held_directory(self, create=False):
        descriptor = os.open(str(directory), os.O_RDONLY | os.O_DIRECTORY)
        try:
            if create and self.markers.read("cache-directory.json") is None:
                self.markers.write("cache-directory.json", {"scope": self.directory_scope(), "identity": {}})
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(runtime.Cache, "directory", contextlib.contextmanager(held_directory))
    return worker, directory


def test_the_cache_publishes_exactly_the_authenticated_bytes_before_enrollment(cache_worker):
    worker, directory = cache_worker
    cache = runtime.Cache(worker)
    assert cache.prepare() is True
    name = "cephadm." + hashlib.sha256(PAYLOAD).hexdigest()
    assert os.listdir(directory) == [name]
    assert (directory / name).read_bytes() == PAYLOAD and os.stat(directory / name).st_mode & 0o777 == 0o744
    assert worker.markers.read("cache-ready.json")["scope"]["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
    with cache.execution_proof():
        pass
    assert runtime.Cache(worker).prepare() is False


def test_a_cache_whose_bytes_changed_refuses_before_any_execution(cache_worker):
    worker, directory = cache_worker
    runtime.Cache(worker).prepare()
    name = "cephadm." + hashlib.sha256(PAYLOAD).hexdigest()
    os.chmod(directory / name, 0o644)
    with open(directory / name, "r+b") as handle:
        handle.write(b"#!/bin/sh\nforeign\n")
    executed = []
    with pytest.raises(ceph_native.Refusal, match="^cache-unproved$"):
        with runtime.Cache(worker).execution_proof():
            executed.append(True)
    assert executed == []


def test_a_foreign_cache_name_is_never_adopted(cache_worker):
    worker, directory = cache_worker
    name = "cephadm." + hashlib.sha256(PAYLOAD).hexdigest()
    (directory / name).write_bytes(PAYLOAD)
    with pytest.raises(ceph_native.Refusal, match="^cache-unproved$"):
        runtime.Cache(worker).prepare()
    assert worker.markers.read("cache-ready.json") is None


def test_a_cache_payload_that_is_not_the_authenticated_one_refuses(cache_worker):
    worker, unused = cache_worker
    os.chmod(os.path.join(worker.host_markers.path, runtime.PAYLOAD), 0o600)
    with open(os.path.join(worker.host_markers.path, runtime.PAYLOAD), "wb") as handle:
        handle.write(b"foreign bytes")
    with pytest.raises(ceph_native.Refusal, match="^runtime-profile$"):
        runtime.Cache(worker)


def test_the_shipped_sources_carry_no_fixture_session_or_qualification_authority():
    for module in (runtime, probe):
        text = runtime.module_source(module)
        assert not re.search(r"FixtureSession|fixture_runtime|qualif", text, re.IGNORECASE)


def test_image_payload_identity_is_independent_of_host_package_payload(request_value, tmp_path, monkeypatch):
    image_payload = b"#!/usr/bin/python3\n# unrelated authenticated image build\n"
    output = json.loads(probe_output(image_payload)(["--user=0:0"]))
    # A selected image authenticates its own bytes, regardless of the host RPM.
    monkeypatch.setattr(runtime, "installed_cephadm", lambda unused: "b" * 64)
    assert runtime.validate_probe(output, (0, 0))["artifacts"]["/usr/sbin/cephadm"]["sha256"] == hashlib.sha256(image_payload).hexdigest()


def test_final_pull_failure_retains_the_native_stderr(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "fail", "fail"])

    def command(argv, timeout, limit):
        code, output = podman(argv, timeout, limit)
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            return code, ceph_native.NativeOutput(output, b"manifest unknown: authored image missing; password=synthetic-secret")
        return code, output

    worker, unused, unused_clock = host_runtime(request_value, tmp_path, command)
    with pytest.raises(ceph_native.NativeFailure) as caught:
        worker.profile({"registryJSON": registry(request_value)})
    assert "manifest unknown" in caught.value.native_error["cause"]
    assert "synthetic-secret" not in str(caught.value.native_error)
