"""Per-host Ceph preparation pulls, profiles, routes and identifies only from the frozen image.

Every provider response here is a synthetic protocol fixture. No test pulls an
image, starts a container, contacts a registry or reads a real host disk.
"""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import io
import json
import sys
import os
import re
import shutil
import signal
import ssl
import subprocess
import threading
import time
from types import SimpleNamespace
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    bounded_process, ceph_host_runtime as runtime, ceph_host_route_probe as probe, ceph_host_worker, ceph_native, ceph_native_observe as observe,
    ceph_native_worker,
)

IMAGE = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + "a" * 64
IMAGE_ID = "c" * 64
PASSWORD = "REGISTRY-PASSWORD-SENTINEL"
PAYLOAD = b"#!/usr/bin/python3\n# synthetic cephadm payload\n"

# The frozen replicated rbd pool the Go side sends with every optional member unset.
POOL = {"application": "rbd", "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
        "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}, "crushRule": "replicated_rule", "filesystem": "",
        "minSize": 2, "name": "volumes", "overwrites": False, "pgNum": 32, "quota": {"maxBytes": 0, "maxObjects": 0}, "role": "rbd", "size": 2,
        "type": "replicated"}


@pytest.fixture(autouse=True)
def injected_broker(monkeypatch):
    monkeypatch.setattr(probe, "fips_enabled", lambda: False)
    original = runtime.ceph_pull_broker.Broker

    class Broker:
        environment = {}
        failure_cause = ""

        def __init__(self, directory, request, private, deadline, command, *, trust=None):
            assert json.loads(private)["password"] == PASSWORD
            assert request["image"] == IMAGE
            assert trust is None or "registryTrustBundleRef" in request
            self.cert_directory = os.path.join(directory, "native-certs")

        def __enter__(self):
            return self

        def __exit__(self, *unused):
            return False

    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    return original


@pytest.fixture
def request_value():
    hosts = [{"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
              "devices": [{"path": "/dev/sda", "serial": "drive-scsi" + str(i), "sizeBytes": 1024 ** 3}], "labels": [], "location": {},
              "sshKeyRef": "fleet-key"} for i in range(1, 4)]
    names = [host["name"] for host in hosts]
    return {"controller": "controller", "version": "ceph-core-v1", "action": "cluster", "cluster": "example", "owner": "apply-123",
            "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": IMAGE,
            "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
            "release": "synthetic-release", "ansiblePackageVersion": "5.0.3-1", "packageRepository": "https://publisher.example.test/ceph",
            "packageRepositoryKey": "https://publisher.example.test/keys/ibm.asc",
            "bootstrap": "node-1", "monIP": "192.0.2.1", "clusterSSHUser": "operator", "clusterSSHTransport": "verifiedTunnel",
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


def filesystem_host_runtime(request, tmp_path, podman):
    parent = tmp_path / "journal"
    parent.mkdir(mode=0o700)
    markers = ceph_native.Markers(request, root=str(parent))
    Path(markers.path).mkdir(mode=0o700)
    markers.write("owner.json", ceph_native.marker_identity(request))
    markers.write("scope.json", runtime.scope(request, "node-1"))
    clock = Clock()
    worker = runtime.HostRuntime(request, "node-1", podman, clock=clock.now, pause=clock.pause, markers=markers)
    return worker, markers


BUNDLE = "-----BEGIN CERTIFICATE-----\nREGISTRY-BUNDLE-SENTINEL\n-----END CERTIFICATE-----\n"


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


def test_disk_backed_first_pull_replays_without_deleting_attempt_records(request_value, tmp_path):
    podman = Podman(request_value, ["ok"])
    worker, markers = filesystem_host_runtime(request_value, tmp_path, podman)
    with worker.journal() as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    before = {path.name: path.read_bytes() for path in Path(markers.path).iterdir()}
    residue = Path(markers.path, "pull-killed")
    residue.mkdir(mode=0o700)
    (residue / "auth.json").write_bytes(b"private fixture residue")
    with worker.journal() as durable:
        assert worker.pull(durable, "unused synthetic input", 4285) is False
    assert not residue.exists() and pull_argv_count(podman) == 1
    assert before == {path.name: path.read_bytes() for path in Path(markers.path).iterdir()}


@pytest.mark.parametrize("state", ["cached", "uncompleted", "exhausted"])
def test_disk_backed_pull_continuation_preserves_validated_records_and_cleans_killed_pull_scratch(request_value, tmp_path, state):
    podman = Podman(request_value, ["ok"] if state == "exhausted" else [], present=state != "exhausted")
    worker, markers = filesystem_host_runtime(request_value, tmp_path, podman)
    count = runtime.PULL_ATTEMPTS if state == "exhausted" else 1
    for attempt in range(1, count + 1):
        markers.write("pull-attempt-%d.json" % attempt, {"scope": worker.scope(), "attempt": attempt})
    if state == "cached":
        markers.write("pulled.json", {"scope": worker.scope(), "imageID": IMAGE_ID})
    before = {path.name: path.read_bytes() for path in Path(markers.path).iterdir()}
    residue = Path(markers.path, "pull-killed")
    residue.mkdir(mode=0o700)
    (residue / "auth.json").write_bytes(b"private fixture residue")
    with worker.journal() as durable:
        assert worker.pull(durable, registry(request_value) if state == "exhausted" else "unused synthetic input", 4285) is (state != "cached")
    assert not residue.exists() and pull_argv_count(podman) == (1 if state == "exhausted" else 0)
    assert all(Path(markers.path, name).read_bytes() == data for name, data in before.items())
    assert markers.read("pulled.json") == {"scope": worker.scope(), "imageID": IMAGE_ID}


@pytest.mark.parametrize("position", ["gapped", "out-of-range"])
@pytest.mark.parametrize("kind", ["regular", "directory", "symlink"])
def test_unvalidated_reserved_pull_entries_refuse_before_any_scratch_removal(request_value, tmp_path, position, kind):
    calls = []
    worker, markers = filesystem_host_runtime(request_value, tmp_path, lambda *args, **kwargs: calls.append(args))
    if position == "out-of-range":
        for attempt in range(1, runtime.PULL_ATTEMPTS + 1):
            markers.write("pull-attempt-%d.json" % attempt, {"scope": worker.scope(), "attempt": attempt})
    residue = Path(markers.path, "pull-killed")
    residue.mkdir(mode=0o700)
    (residue / "auth.json").write_bytes(b"retained fixture residue")
    outside = tmp_path / "outside"
    outside.write_bytes(b"outside fixture")
    attempt = 2 if position == "gapped" else runtime.PULL_ATTEMPTS + 1
    entry = Path(markers.path, "pull-attempt-%d.json" % attempt)
    if kind == "regular":
        markers.write(entry.name, {"scope": worker.scope(), "attempt": attempt})
    elif kind == "directory":
        entry.mkdir(mode=0o700)
        (entry / "payload").write_bytes(b"directory fixture")
    else:
        entry.symlink_to(outside)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        with worker.journal() as durable:
            worker.pull(durable, "unused synthetic input", 4285)
    assert calls == [] and (residue / "auth.json").read_bytes() == b"retained fixture residue"
    assert outside.read_bytes() == b"outside fixture"
    if kind == "regular":
        assert markers.read(entry.name) == {"scope": worker.scope(), "attempt": attempt}
    elif kind == "directory":
        assert (entry / "payload").read_bytes() == b"directory fixture"
    else:
        assert entry.is_symlink() and entry.readlink() == outside


@pytest.mark.parametrize("kind", ["malformed", "foreign", "directory", "symlink"])
def test_invalid_first_disk_backed_attempt_is_never_exempted_or_removed(request_value, tmp_path, kind):
    calls = []
    worker, markers = filesystem_host_runtime(request_value, tmp_path, lambda *args, **kwargs: calls.append(args))
    residue = Path(markers.path, "pull-killed")
    residue.mkdir(mode=0o700)
    (residue / "auth.json").write_bytes(b"retained fixture residue")
    entry = Path(markers.path, "pull-attempt-1.json")
    outside = tmp_path / "outside"
    outside.write_bytes(b"outside fixture")
    if kind == "malformed":
        ceph_native.private_write(str(entry), b"{invalid")
    elif kind == "foreign":
        markers.write(entry.name, {"scope": runtime.scope(dict(request_value, owner="apply-other"), "node-1"), "attempt": 1})
    elif kind == "directory":
        entry.mkdir(mode=0o700)
        (entry / "payload").write_bytes(b"directory fixture")
    else:
        entry.symlink_to(outside)
    before = entry.read_bytes() if kind in ("malformed", "foreign") else None
    expected = "image-pull" if kind == "foreign" else "ownership"
    with pytest.raises(ceph_native.Refusal, match="^" + expected + "$"):
        with worker.journal() as durable:
            worker.pull(durable, "unused synthetic input", 4285)
    assert calls == [] and (residue / "auth.json").read_bytes() == b"retained fixture residue"
    assert outside.read_bytes() == b"outside fixture"
    if kind in ("malformed", "foreign"):
        assert entry.read_bytes() == before
    elif kind == "directory":
        assert (entry / "payload").read_bytes() == b"directory fixture"
    else:
        assert entry.is_symlink() and entry.readlink() == outside


@pytest.mark.parametrize("kind", ["regular", "symlink", "foreign-directory"])
def test_unknown_scratch_candidates_still_refuse_without_removing_them(request_value, tmp_path, monkeypatch, kind):
    calls = []
    worker, markers = filesystem_host_runtime(request_value, tmp_path, lambda *args, **kwargs: calls.append(args))
    outside = tmp_path / "outside"
    outside.write_bytes(b"outside fixture")
    entry = Path(markers.path, "pull-unexpected")
    if kind == "regular":
        entry.write_bytes(b"unexpected fixture")
    elif kind == "symlink":
        entry.symlink_to(outside)
    else:
        entry.mkdir(mode=0o700)
        (entry / "payload").write_bytes(b"directory fixture")
        original = os.stat

        def foreign_owner(path, *args, **kwargs):
            info = original(path, *args, **kwargs)
            return SimpleNamespace(st_mode=info.st_mode, st_uid=os.geteuid() + 1) if path == entry.name and "dir_fd" in kwargs else info

        monkeypatch.setattr(runtime.os, "stat", foreign_owner)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        with worker.journal() as durable:
            worker.pull(durable, "unused synthetic input", 4285)
    assert calls == [] and outside.read_bytes() == b"outside fixture"
    if kind == "regular":
        assert entry.read_bytes() == b"unexpected fixture"
    elif kind == "foreign-directory":
        assert (entry / "payload").read_bytes() == b"directory fixture"
    else:
        assert entry.is_symlink() and entry.readlink() == outside


@pytest.mark.parametrize("outcome", ["ok", "fail"])
def test_pull_cert_directory_loads_native_client_identity(request_value, tmp_path, monkeypatch, injected_broker, outcome):
    """Real certificates, native directory discovery, and no registry/container effects."""
    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", injected_broker)

    class Upstream:
        def __init__(self, *unused, context=None):
            assert context is None

        def close(self):
            pass

    monkeypatch.setattr(runtime.ceph_pull_broker, "Upstream", Upstream)
    podman = Podman(request_value, [outcome] * runtime.PULL_ATTEMPTS)
    directories = []

    def command(argv, timeout, limit, **options):
        if argv[:2] == ["/usr/bin/openssl", "req"]:
            result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    timeout=timeout, check=True, env={"PATH": "/usr/bin:/bin", "LANG": "C"})
            assert len(result.stdout) + len(result.stderr) <= limit
            return 0, result.stdout
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            directory = Path(argv[argv.index("--cert-dir") + 1])
            directories.append(directory)
            # containers/image SetupCertificates scans all .key and .cert files,
            # rejecting an orphan in either direction before any registry request.
            names = {path.name for path in directory.iterdir()}
            context = ssl.create_default_context()
            for path in directory.iterdir():
                if path.suffix == ".key":
                    assert path.with_suffix(".cert").name in names
                elif path.suffix == ".cert":
                    assert path.with_suffix(".key").name in names
                    context.load_cert_chain(str(path), str(path.with_suffix(".key")))
                elif path.suffix == ".crt":
                    context.load_verify_locations(cafile=str(path))
            assert names == {"ca.crt", "client.cert", "client.key"}
            assert directory.stat().st_mode & 0o777 == 0o700
            assert all(path.stat().st_mode & 0o777 == 0o600 for path in directory.iterdir())
            assert PASSWORD not in str(options.get("environment"))
            assert all(PASSWORD.encode() not in path.read_bytes() for path in directory.iterdir())
        return podman(argv, timeout, limit)

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    with contextlib.ExitStack() as stack:
        if outcome == "fail":
            stack.enter_context(pytest.raises(ceph_native.Refusal, match="^image-pull$"))
        with worker.journal(create=True) as durable:
            assert worker.pull(durable, registry(request_value), 4285) is True
    assert len(directories) == (1 if outcome == "ok" else runtime.PULL_ATTEMPTS)
    assert all(not directory.exists() for directory in directories)
    for directory in directories:
        if directory.parent.exists():
            assert {path.name for path in directory.parent.iterdir()} == {"policy.json"}
    assert ("pulled.json" in markers.records) is (outcome == "ok")


def test_pull_retries_stay_bounded_and_refuse_after_three_attempts_in_one_invocation(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "fail", "fail", "fail"])
    worker, markers, clock = host_runtime(request_value, tmp_path, podman)
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == 3 and clock.pauses == [runtime.PULL_PAUSE] * 2
    assert [name for name in markers.writes if name.startswith("pull-attempt")] == ["pull-attempt-1.json", "pull-attempt-2.json", "pull-attempt-3.json"]
    assert "pulled.json" not in markers.records and podman.outcomes == ["fail"]


def exhausted_journal(request_value, markers):
    for attempt in range(1, runtime.PULL_ATTEMPTS + 1):
        markers.records["pull-attempt-%d.json" % attempt] = {"scope": runtime.scope(request_value, "node-1"), "attempt": attempt}


@pytest.mark.parametrize("outcomes", [["ok"], ["fail", "ok"], ["fail", "fail", "ok"]])
def test_an_exhausted_journal_from_an_earlier_invocation_lets_the_next_invocation_pull(request_value, tmp_path, outcomes):
    podman = Podman(request_value, outcomes)
    worker, markers, clock = host_runtime(request_value, tmp_path, podman)
    exhausted_journal(request_value, markers)
    with worker.journal(create=True) as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    assert pull_argv_count(podman) == len(outcomes) and clock.pauses == [runtime.PULL_PAUSE] * (len(outcomes) - 1)
    assert markers.records["pulled.json"] == {"scope": runtime.scope(request_value, "node-1"), "imageID": IMAGE_ID}
    assert sorted(name for name in markers.records if name.startswith("pull-attempt")) == ["pull-attempt-1.json", "pull-attempt-2.json", "pull-attempt-3.json"]


def test_three_failures_after_an_exhausted_journal_still_stop_the_invocation(request_value, tmp_path):
    podman = Podman(request_value, ["fail"] * (runtime.PULL_ATTEMPTS + 1))
    worker, markers, clock = host_runtime(request_value, tmp_path, podman)
    exhausted_journal(request_value, markers)
    with pytest.raises(ceph_native.Refusal, match="^image-pull$"):
        with worker.journal(create=True) as durable:
            worker.pull(durable, registry(request_value), 4285)
    assert pull_argv_count(podman) == runtime.PULL_ATTEMPTS and clock.pauses == [runtime.PULL_PAUSE] * (runtime.PULL_ATTEMPTS - 1)
    assert "pulled.json" not in markers.records and podman.outcomes == ["fail"]
    later, unused_markers, unused_clock = host_runtime(request_value, tmp_path / "later", Podman(request_value, ["ok"]))
    later.markers = markers
    with later.journal() as durable:
        assert later.pull(durable, registry(request_value), 4285) is True
    assert "pulled.json" in markers.records


@pytest.mark.parametrize("present", [True, False])
def test_a_crash_after_the_last_attempt_record_is_recoverable(request_value, tmp_path, present):
    podman = Podman(request_value, ["ok"], present=present)
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    exhausted_journal(request_value, markers)
    with worker.journal(create=True) as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    assert pull_argv_count(podman) == (0 if present else 1)
    assert markers.records["pulled.json"]["imageID"] == IMAGE_ID


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
    assert sorted(name for name in markers.records if name.startswith("pull-attempt")) == ["pull-attempt-1.json"]
    assert markers.writes.count("pull-attempt-1.json") == (0 if present else 1)
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
    podman = Podman(request_value, ["fail"] * runtime.PULL_ATTEMPTS if exit_case == "exhausted" else [], present=exit_case != "exhausted")
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
    assert not os.path.exists(residue) and pull_argv_count(podman) == (runtime.PULL_ATTEMPTS if exit_case == "exhausted" else 0)


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
def authenticated():
    return hashlib.sha256(PAYLOAD).hexdigest()


@pytest.fixture
def host_cephadm_sealed(request_value, monkeypatch):
    host = request_value["cephadm"]["path"]
    opened = []
    reader, opener, stream = ceph_native.read_regular, os.open, io.open

    def read_regular(path, *arguments, **options):
        if os.fspath(path) == host:
            opened.append(path)
            raise AssertionError("the host cephadm was read")
        return reader(path, *arguments, **options)

    def guarded_open(path, *arguments, **options):
        if os.fspath(path) == host:
            opened.append(path)
            raise AssertionError("the host cephadm was opened")
        return opener(path, *arguments, **options)

    def guarded_stream(path, *arguments, **options):
        if not isinstance(path, int) and os.fspath(path) == host:
            opened.append(path)
            raise AssertionError("the host cephadm was opened")
        return stream(path, *arguments, **options)

    monkeypatch.setattr(ceph_native, "read_regular", read_regular)
    monkeypatch.setattr(os, "open", guarded_open)
    monkeypatch.setattr(io, "open", guarded_stream)
    monkeypatch.setattr("builtins.open", guarded_stream)
    return opened


def test_facts_come_from_two_agreeing_probes_as_root_and_as_the_image_account(request_value, tmp_path, authenticated):
    engine = Engine([probe_output(), probe_output()])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    facts = worker.facts(IMAGE_ID)
    assert facts["cephUID"] == facts["cephGID"] == 167
    users = [[value for value in argv if value.startswith("--user=")][0] for argv in engine.calls if argv[:2] == ["/usr/bin/podman", "create"]]
    assert users == ["--user=0:0", "--user=167:167"]
    assert hashlib.sha256(runtime.decode_payload(facts["cephadm"])).hexdigest() == authenticated


def test_image_facts_keep_a_generated_policy_with_gssapi_key_exchange_disabled(request_value, tmp_path):
    policy = ("Ciphers aes128-ctr\nMACs hmac-sha2-256\nGSSAPIKeyExchange no\nKexAlgorithms ecdh-sha2-nistp384\n"
              "PubkeyAcceptedAlgorithms rsa-sha2-512\nCASignatureAlgorithms rsa-sha2-256\nRequiredRSASize 2048\n")
    engine = Engine([probe_output(cryptoPolicy=policy), probe_output(cryptoPolicy=policy)])
    worker, unused = contained_worker(request_value, tmp_path, engine)
    facts = worker.facts(IMAGE_ID)
    assert facts["cryptoPolicy"] == policy and runtime.crypto_policy(facts["cryptoPolicy"]) == policy
    assert len(engine.started) == len(engine.removed) == 2


@pytest.mark.parametrize("changes", [{"cephUID": 0}, {"cephGID": 168}, {"machine": "aarch64"}, {"cryptoPolicy": "Include /etc/foreign\n"},
                                     {"cryptoPolicy": "GSSAPIKeyExchange yes\n"},
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


def prepared_image(worker, private):
    """The images call, then the runtime profile call, as the host block makes them."""
    names = [entry["name"] for entry in worker.request.get("auxiliary", [])]
    changed = worker.images(private, ceph_pull())["changed"]
    if names:
        changed = worker.images(private, {"names": names, "seconds": runtime.IMAGES_CALL_SECONDS})["changed"] or changed
    profiled = worker.profile(names)
    return {"changed": changed or profiled["changed"]}


def ceph_pull(seconds=runtime.IMAGES_CALL_SECONDS):
    return {"names": [], "seconds": seconds}


def concurrent_order(values):
    """The Ceph image's own call first, then one batch whose members run at once, in name order."""
    return values[:1] + sorted(values[1:], key=str)


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
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": True}
    record = markers.records[runtime.PROFILE]
    assert record["imageID"] == IMAGE_ID and record["uid"] == record["gid"] == 167 and record["label"] == runtime.SHARED_LABEL
    assert record["cephadmSHA256"] == authenticated and PASSWORD not in json.dumps(markers.records)
    assert runtime.cephadm_payload(markers) == PAYLOAD
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": False}
    assert pull_argv_count(podman) == 1


def test_profile_authenticates_image_bytes_independently_of_installed_package(request_value, tmp_path, monkeypatch, host_cephadm_sealed):
    worker, markers, unused, unused_engine = profiled_runtime(request_value, tmp_path, monkeypatch, [probe_output(), probe_output()])
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": True}
    assert markers.records[runtime.PROFILE]["cephadmSHA256"] == hashlib.sha256(PAYLOAD).hexdigest()
    assert host_cephadm_sealed == []


@pytest.mark.parametrize("private,bound", [({}, False), ({"registryJSON": "x", "extra": "y"}, False), ({"registryJSON": "x", "registryCA": BUNDLE}, False),
                                           ({"registryJSON": "x"}, True), ({"registryJSON": "x", "registryCA": BUNDLE, "extra": "y"}, True)])
def test_profile_accepts_only_the_registry_material(request_value, tmp_path, monkeypatch, private, bound):
    if bound:
        request_value["registryTrustBundleRef"] = "registry-ca"
    worker, markers, podman, unused = profiled_runtime(request_value, tmp_path, monkeypatch, [])
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        worker.images(private, ceph_pull())
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


@pytest.mark.parametrize("phase", ["images", "profile", "route", "device_identity"])
def test_host_phases_refuse_an_unsupported_release_before_any_effect(request_value, phase):
    request_value["clusterSSHTransport"] = "native"
    with pytest.raises(ceph_native.Refusal, match="^unsupported-release$"):
        runtime.dispatch(dict({"request": request_value, "host": "node-1", "phase": phase, "private": {"registryJSON": "x"} if phase == "images" else {}},
                              **{runtime.ENVELOPE_EXTRA[phase]: ceph_pull() if phase == "images" else []} if phase in runtime.ENVELOPE_EXTRA else {}))


@pytest.mark.parametrize("phase", sorted(runtime.PHASES))
def test_the_native_dispatch_routes_every_host_phase_to_the_host_runtime(monkeypatch, phase):
    reached = []
    monkeypatch.setattr(runtime, "dispatch", lambda envelope: reached.append(envelope["phase"]) or {"changed": False})
    envelope = {"request": {}, "host": "node-1", "phase": phase, "private": {}}
    assert ceph_native.dispatch(envelope) == {"changed": False} and reached == [phase]


@pytest.mark.parametrize("phase", ["profile", "route"])
def test_only_the_long_phase_receives_private_material(request_value, phase):
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        runtime.dispatch(dict({"request": request_value, "host": "node-1", "phase": phase, "private": {"registryJSON": "x"}},
                              **{"names": []} if phase == "profile" else {}))


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


REAL_CACHE_DIRECTORY = runtime.Cache.__dict__["directory"]


@pytest.fixture
def held_cache(cache_worker, tmp_path, monkeypatch):
    """The real FSID directory proof over a synthetic data root, with the cephadm cache published in it."""
    worker, unused = cache_worker
    root = tmp_path / "ceph"
    root.mkdir(mode=0o700)
    monkeypatch.setattr(runtime.Cache, "directory", REAL_CACHE_DIRECTORY)
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid())

    @contextlib.contextmanager
    def root_directory(uid=None):
        descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            observe.safe_directory(os.fstat(descriptor), (os.getuid(),))
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(observe, "root_directory", root_directory)
    assert runtime.Cache(worker).prepare() is True
    cluster = root / worker.request["fsid"]
    assert (cluster / ("cephadm." + hashlib.sha256(PAYLOAD).hexdigest())).read_bytes() == PAYLOAD
    return worker, cluster


class Chown:
    """cephadm's make_data_dir_base chowning the FSID directory as root; the test runs unprivileged, so stat reports the moved owner."""

    def __init__(self, monkeypatch, path):
        self.inode = os.stat(path).st_ino
        self.uid = None
        self.reported = 0
        real_stat, real_fstat = os.stat, os.fstat
        monkeypatch.setattr(observe.os, "stat", lambda *args, **options: self.remap(real_stat(*args, **options)))
        monkeypatch.setattr(observe.os, "fstat", lambda descriptor: self.remap(real_fstat(descriptor)))

    def remap(self, info):
        if self.uid is None or info.st_ino != self.inode:
            return info
        self.reported += 1
        values = list(info[:10])
        values[4] = self.uid
        return os.stat_result(values, {name: getattr(info, name) for name in ("st_atime_ns", "st_mtime_ns", "st_ctime_ns", "st_blksize", "st_blocks")})


@pytest.mark.parametrize("uid,singleton", [(167, None), (472, "grafana.node-1"), (65534, None)])
def test_an_execution_proof_survives_cephadm_moving_the_fsid_directory_owner(held_cache, monkeypatch, uid, singleton):
    """A sibling block's deploy chowns the FSID directory to its daemon's user while an effect holds it, even before that daemon's own
    directory exists; the held cache inode and the directory's device, inode and mode still prove the executed bytes."""
    worker, cluster = held_cache
    chown = Chown(monkeypatch, cluster)
    executed = []
    with runtime.Cache(worker).execution_proof():
        chown.uid = uid
        if singleton is not None:
            (cluster / singleton).mkdir(mode=0o700)
        assert os.stat(cluster).st_uid == uid
        before = chown.reported
        executed.append(True)
    assert executed == [True] and chown.reported > before


def test_an_execution_proof_still_refuses_an_unproved_entry_owner(held_cache, monkeypatch):
    worker, cluster = held_cache
    chown = Chown(monkeypatch, cluster)
    chown.uid = os.getuid() + 472
    executed = []
    with pytest.raises(ceph_native.Refusal) as caught:
        with runtime.Cache(worker).execution_proof():
            executed.append(True)
    assert str(caught.value) == "cache-unproved" and not isinstance(caught.value, observe.ObservationRefusal)
    assert executed == [] and chown.reported > 0


def replace_directory(cluster):
    cluster.rename(cluster.with_name("moved"))
    cluster.mkdir(mode=0o700)


def widen_mode(cluster):
    os.chmod(cluster, 0o720)


def touch_cache(cluster):
    name = cluster / ("cephadm." + hashlib.sha256(PAYLOAD).hexdigest())
    info = os.stat(name)
    os.utime(name, ns=(info.st_atime_ns, info.st_mtime_ns + 1_000_000_000))


@pytest.mark.parametrize("change", [replace_directory, widen_mode, touch_cache])
def test_an_execution_proof_refuses_a_directory_or_cache_moved_during_the_effect(held_cache, change):
    """Each refusal is the cache's own typed one, never an observation error a worker would report as native-unknown."""
    worker, cluster = held_cache
    before = os.stat(cluster)
    with pytest.raises(ceph_native.Refusal) as caught:
        with runtime.Cache(worker).execution_proof():
            change(cluster)
            after = os.stat(cluster)
            assert (after.st_ino, after.st_mode) != (before.st_ino, before.st_mode) or change is touch_cache
    assert str(caught.value) == "cache-unproved" and not isinstance(caught.value, observe.ObservationRefusal)


def test_an_effect_failure_inside_the_execution_proof_keeps_its_own_exception(held_cache):
    worker, unused = held_cache
    with pytest.raises(observe.ObservationRefusal, match="^native-observation-unproved$"):
        with runtime.Cache(worker).execution_proof():
            raise observe.ObservationRefusal("native-observation-unproved")


def test_the_shipped_sources_carry_no_fixture_session_or_qualification_authority():
    for module in (runtime, probe):
        text = runtime.module_source(module)
        assert not re.search(r"FixtureSession|fixture_runtime|qualif", text, re.IGNORECASE)


def test_image_payload_identity_is_independent_of_host_package_payload(host_cephadm_sealed):
    image_payload = b"#!/usr/bin/python3\n# unrelated authenticated image build\n"
    output = json.loads(probe_output(image_payload)(["--user=0:0"]))
    # A selected image authenticates its own bytes, regardless of the host RPM.
    assert runtime.validate_probe(output, (0, 0))["artifacts"]["/usr/sbin/cephadm"]["sha256"] == hashlib.sha256(image_payload).hexdigest()
    assert host_cephadm_sealed == []


def test_final_pull_failure_retains_the_native_stderr(request_value, tmp_path):
    podman = Podman(request_value, ["fail", "fail", "fail"])

    def command(argv, timeout, limit):
        code, output = podman(argv, timeout, limit)
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            return code, ceph_native.NativeOutput(output, b"manifest unknown: authored image missing; password=synthetic-secret")
        return code, output

    worker, unused, unused_clock = host_runtime(request_value, tmp_path, command)
    with pytest.raises(ceph_native.NativeFailure) as caught:
        prepared_image(worker, {"registryJSON": registry(request_value)})
    assert "manifest unknown" in caught.value.native_error["cause"]
    assert "synthetic-secret" not in str(caught.value.native_error)


def test_pull_http_cause_survives_redaction_and_truncation_without_cross_invocation_state(
        request_value, tmp_path, monkeypatch, injected_broker):
    instances = []

    class Broker(injected_broker):
        environment = {}

        def __init__(self, directory, *unused, trust=None):
            assert trust is None
            self.cert_directory = os.path.join(directory, "native-certs")
            self.lock = threading.Lock()
            self.failure, self.untrusted = None, False
            self.upstream = SimpleNamespace(private=[PASSWORD])
            self.exited = False
            instances.append(self)

        def __enter__(self):
            return self

        def __exit__(self, *unused):
            self.exited = True

        @property
        def failure_cause(self):
            assert self.exited
            return super().failure_cause

    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    for invocation in range(2):
        podman = Podman(request_value, ["fail"] * runtime.PULL_ATTEMPTS)

        def command(argv, timeout, limit):
            code, output = podman(argv, timeout, limit)
            if argv[:2] == ["/usr/bin/podman", "pull"]:
                if invocation == 0:
                    response = SimpleNamespace(status=503, read=lambda unused: b"private upstream body")
                    instances[-1].response(io.BytesIO(), response, "GET", 1024, "manifest")
                stderr = ("native refused password=" + PASSWORD + "; " + "long diagnostic " * 400).encode()
                return code, ceph_native.NativeOutput(output, stderr)
            return code, output

        worker, markers, unused_clock = host_runtime(request_value, tmp_path / str(invocation), command)
        with pytest.raises(ceph_native.NativeFailure) as caught:
            with worker.journal(create=True) as durable:
                worker.pull(durable, registry(request_value), 4285)
        detail = caught.value.native_error
        assert detail["coordinate"] == IMAGE
        assert "native refused password=[redacted]" in detail["cause"]
        assert PASSWORD not in str(detail) and "private upstream body" not in str(detail)
        assert len(detail["cause"].encode()) <= 1024
        assert detail["cause"].startswith("broker observed manifest upstream HTTP 503; ") is (invocation == 0)
        assert pull_argv_count(podman) == runtime.PULL_ATTEMPTS
        assert "pulled.json" not in markers.records
    assert len(instances) == 2 and all(value.exited for value in instances)


def test_pull_failure_redacts_the_password_but_keeps_a_username_shaped_coordinate(request_value, tmp_path):
    podman = Podman(request_value, ["fail"] * runtime.PULL_ATTEMPTS)

    def command(argv, timeout, limit):
        code, output = podman(argv, timeout, limit)
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            return code, ceph_native.NativeOutput(output, ("unauthorized pulling " + IMAGE + "; password=" + PASSWORD).encode())
        return code, output

    material = json.dumps({"url": request_value["registry"], "username": "cp", "password": PASSWORD})
    worker, unused, unused_clock = host_runtime(request_value, tmp_path, command)
    with pytest.raises(ceph_native.NativeFailure) as caught:
        with worker.journal(create=True) as durable:
            worker.pull(durable, material, 4285)
    detail = caught.value.native_error
    assert detail["coordinate"] == IMAGE
    assert "unauthorized pulling cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" in detail["cause"]
    assert PASSWORD not in str(detail)


def test_successful_pull_retry_does_not_project_an_earlier_broker_refusal(request_value, tmp_path, monkeypatch):
    class Broker(runtime.ceph_pull_broker.Broker):
        @property
        def failure_cause(self):
            pytest.fail("successful pull must not project an observed refusal")

    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    podman = Podman(request_value, ["fail", "ok"])
    worker, markers, unused = host_runtime(request_value, tmp_path, podman)
    with worker.journal(create=True) as durable:
        assert worker.pull(durable, registry(request_value), 4285) is True
    assert pull_argv_count(podman) == 2 and "pulled.json" in markers.records


def test_host_runtime_binds_one_scope_from_both_blocks(request_value):
    scope = {key: value for key, value in request_value.items() if key != "action"}
    from_cluster = runtime.HostRuntime(copy.deepcopy(request_value), "node-1", packages=object())
    from_scope = runtime.HostRuntime(copy.deepcopy(scope), "node-1", packages=object())
    assert "action" not in from_cluster.request and from_cluster.request == from_scope.request == scope
    assert from_cluster.scope() == from_scope.scope() == runtime.scope(scope, "node-1")
    pool = dict(request_value, action="pool", pool=copy.deepcopy(POOL))
    for refused in (pool, dict(scope, pool=pool["pool"])):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            runtime.HostRuntime(copy.deepcopy(refused), "node-1", packages=object())


HAPROXY = "cp.icr.io/cp/ibm-ceph/haproxy-synthetic@sha256:" + "d" * 64
KEEPALIVED = "cp.icr.io/cp/ibm-ceph/keepalived-synthetic@sha256:" + "e" * 64


class ImageSet:
    """A scripted engine holding several images; each pull follows its reference's outcomes."""

    def __init__(self, outcomes):
        self.outcomes = {reference: list(values) for reference, values in outcomes.items()}
        self.present = set()
        self.calls = []
        self.policies = {}
        self.environments = []

    @staticmethod
    def identifier(reference):
        return hashlib.sha256(reference.encode()).hexdigest()

    def __call__(self, argv, timeout, limit, environment=None, cancel=None):
        self.calls.append(list(argv))
        assert 0 < timeout and 0 < limit and (cancel is None or not cancel.is_set())
        if argv[:3] == ["/usr/bin/podman", "image", "inspect"]:
            reference = argv[3]
            if reference not in self.present:
                return 125, b""
            return 0, json.dumps([{"Id": self.identifier(reference), "Os": "linux", "Architecture": "amd64", "RepoDigests": [reference],
                                   "Size": 1 << 30}]).encode()
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            reference = argv[-1]
            self.environments.append((reference, dict(environment or {})))
            with open(argv[argv.index("--signature-policy") + 1], "rb") as handle:
                self.policies[reference] = json.loads(handle.read())
            if self.outcomes[reference].pop(0) == "ok":
                self.present.add(reference)
                return 0, b""
            return 125, ceph_native.NativeOutput(b"", ("denied for registry-user password=" + PASSWORD).encode())
        raise AssertionError("unexpected command " + repr(argv))


@pytest.fixture
def auxiliary_brokers(monkeypatch):
    instances = []

    class Broker:
        failure_cause = ""

        def __init__(self, directory, request, private, deadline, command, *, image=None, trust=None):
            del deadline, command
            assert json.loads(private)["password"] == PASSWORD
            assert trust is None or "registryTrustBundleRef" in request
            self.image, self.trust = image, trust
            self.cert_directory = os.path.join(directory, "native-certs")
            self.environment = {"HTTPS_PROXY": "http://127.0.0.1:" + str(40000 + len(instances))}
            instances.append(self)

        def __enter__(self):
            return self

        def __exit__(self, *unused):
            return False

    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    return instances


def auxiliary_runtime(request_value, tmp_path, monkeypatch, outcomes, outputs=()):
    request_value["auxiliary"] = [{"name": "haproxy", "image": HAPROXY}, {"name": "keepalived", "image": KEEPALIVED}]
    images = ImageSet(outcomes)
    engine = Engine(list(outputs))

    def command(argv, timeout, limit, environment=None, cancel=None):
        if argv[:2] in (["/usr/bin/podman", "image"], ["/usr/bin/podman", "pull"]):
            return images(argv, timeout, limit, environment, cancel)
        return engine(argv, timeout, limit)

    worker, markers, unused_clock = host_runtime(request_value, tmp_path, command)
    monkeypatch.setattr(runtime.HostRuntime, "label", lambda self, image_id, user, parent: runtime.SHARED_LABEL)
    return worker, markers, images


def test_host_prepull_pulls_each_frozen_auxiliary_digest(request_value, tmp_path, monkeypatch, authenticated, auxiliary_brokers):
    del authenticated
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {IMAGE: ["ok"], HAPROXY: ["ok"], KEEPALIVED: ["ok"]},
                                                [probe_output(), probe_output()])
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": True}
    pulls = [argv for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]
    assert concurrent_order([argv[-1] for argv in pulls]) == concurrent_order([IMAGE, HAPROXY, KEEPALIVED])
    assert concurrent_order([broker.image for broker in auxiliary_brokers]) == concurrent_order([None, HAPROXY, KEEPALIVED])
    for argv in pulls[1:]:
        reference = argv[-1]
        broker = next(item for item in auxiliary_brokers if item.image == reference)
        policy, auth = argv[argv.index("--signature-policy") + 1], argv[argv.index("--authfile") + 1]
        assert argv == ["/usr/bin/podman", "pull", "--signature-policy", policy, "--tls-verify=true", "--authfile", auth,
                        "--cert-dir", broker.cert_directory, reference]
        assert os.path.basename(os.path.dirname(policy)).startswith("auxiliary-scratch-")
        assert images.policies[reference] == {"default": [{"type": "reject"}],
                                              "transports": {"docker": {reference.split("@", 1)[0]: [{"type": "insecureAcceptAnything"}]}}}
        assert (reference, broker.environment) in images.environments
    scope = runtime.scope(request_value, "node-1")
    for name, reference in (("haproxy", HAPROXY), ("keepalived", KEEPALIVED)):
        assert markers.records["auxiliary-" + name + "-pulled.json"] == {"scope": scope, "image": reference, "imageID": images.identifier(reference)}
        assert markers.records["auxiliary-" + name + "-attempt-1.json"] == {"scope": scope, "image": reference, "attempt": 1}
    assert not [name for name in os.listdir(markers.path) if name.startswith("auxiliary-scratch-")]
    images.calls.clear()
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": False}
    assert not [argv for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]
    inspected = [argv[3] for argv in images.calls if argv[:3] == ["/usr/bin/podman", "image", "inspect"]]
    assert HAPROXY in inspected and KEEPALIVED in inspected


def test_the_runtime_profile_pulls_nothing_and_needs_every_digest_the_images_call_recorded(request_value, tmp_path, monkeypatch, authenticated,
                                                                                           auxiliary_brokers):
    del authenticated, auxiliary_brokers
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {IMAGE: ["ok"], HAPROXY: ["ok"], KEEPALIVED: ["ok"]},
                                                [probe_output(), probe_output()])
    names = ["haproxy", "keepalived"]
    assert worker.images({"registryJSON": registry(request_value)}, ceph_pull()) == {"changed": True}
    assert worker.images({"registryJSON": registry(request_value)}, {"names": names, "seconds": 600}) == {"changed": True}
    assert runtime.PROFILE not in markers.records
    pulled = markers.records.pop("auxiliary-keepalived-pulled.json")
    images.calls.clear()
    with pytest.raises(ceph_native.Refusal, match="^image-platform$"):
        worker.profile(names)
    assert runtime.PROFILE not in markers.records
    markers.records["auxiliary-keepalived-pulled.json"] = pulled
    assert worker.profile(names) == {"changed": True} and worker.profile(names) == {"changed": False}
    assert not [argv for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]


def test_host_profile_without_an_auxiliary_set_pulls_only_the_ceph_image(request_value, tmp_path, monkeypatch, authenticated, auxiliary_brokers):
    del authenticated
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {IMAGE: ["ok"]}, [probe_output(), probe_output()])
    worker.request.pop("auxiliary")
    assert prepared_image(worker, {"registryJSON": registry(request_value)}) == {"changed": True}
    assert [argv[-1] for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]] == [IMAGE]
    assert {argv[3] for argv in images.calls if argv[:3] == ["/usr/bin/podman", "image", "inspect"]} == {IMAGE}
    assert [broker.image for broker in auxiliary_brokers] == [None]
    assert not [name for name in markers.records if name.startswith("auxiliary-")]


def test_auxiliary_pull_carries_no_credential_in_argv_or_environment(request_value, tmp_path, monkeypatch, auxiliary_brokers):
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch,
                                                {IMAGE: ["ok"], HAPROXY: ["fail"] * runtime.PULL_ATTEMPTS, KEEPALIVED: ["ok"]})
    with pytest.raises(ceph_native.NativeFailure, match="^image-pull$") as caught:
        prepared_image(worker, {"registryJSON": registry(request_value)})
    pulls = [argv for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]
    assert concurrent_order([argv[-1] for argv in pulls]) == concurrent_order([IMAGE] + [HAPROXY] * runtime.PULL_ATTEMPTS + [KEEPALIVED])
    assert concurrent_order([broker.image for broker in auxiliary_brokers]) == concurrent_order([None, HAPROXY, KEEPALIVED])
    assert "auxiliary-keepalived-pulled.json" in markers.records
    for argv in pulls:
        assert not any(PASSWORD in item or "registry-user" in item or item.startswith("--creds") for item in argv)
    for unused_reference, environment in images.environments:
        assert not any(PASSWORD in value or "registry-user" in value for value in environment.values())
    detail = caught.value.native_error
    assert detail["coordinate"] == HAPROXY
    assert PASSWORD not in str(detail) and "auxiliary-haproxy-pulled.json" not in markers.records
    assert markers.records["auxiliary-haproxy-attempt-3.json"]["attempt"] == 3


def test_a_retained_auxiliary_pull_of_another_reference_refuses(request_value, tmp_path, monkeypatch, auxiliary_brokers):
    del auxiliary_brokers
    worker, unused_markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {IMAGE: ["ok"], HAPROXY: ["ok"], KEEPALIVED: ["ok"]})
    images.present.update((HAPROXY, KEEPALIVED))
    with worker.journal(create=True) as durable:
        durable.write("auxiliary-haproxy-attempt-1.json", {"scope": worker.scope(), "image": HAPROXY, "attempt": 1})
        durable.write("auxiliary-haproxy-pulled.json", {"scope": worker.scope(), "image": KEEPALIVED, "imageID": images.identifier(KEEPALIVED)})
        with pytest.raises(ceph_native.Refusal, match="^image-platform$"):
            worker.pull_auxiliary(durable, registry(request_value), 4285, ["haproxy", "keepalived"])
    assert [argv[-1] for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]] == [KEEPALIVED]


@pytest.mark.parametrize("outcomes", [["ok"], ["fail", "ok"], ["fail", "fail", "ok"], ["fail"] * (runtime.PULL_ATTEMPTS + 1)])
def test_an_exhausted_auxiliary_journal_lets_the_next_invocation_pull(request_value, tmp_path, monkeypatch, auxiliary_brokers, outcomes):
    del auxiliary_brokers
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {HAPROXY: list(outcomes), KEEPALIVED: ["ok"]})
    images.present.add(KEEPALIVED)
    scope = runtime.scope(request_value, "node-1")
    for attempt in range(1, runtime.PULL_ATTEMPTS + 1):
        markers.records["auxiliary-haproxy-attempt-%d.json" % attempt] = {"scope": scope, "image": HAPROXY, "attempt": attempt}
        markers.records["auxiliary-keepalived-attempt-%d.json" % attempt] = {"scope": scope, "image": KEEPALIVED, "attempt": attempt}
    pulled = "ok" in outcomes
    with contextlib.ExitStack() as stack:
        if not pulled:
            stack.enter_context(pytest.raises(ceph_native.Refusal, match="^image-pull$"))
        with worker.journal(create=True) as durable:
            assert worker.pull_auxiliary(durable, registry(request_value), 4285, ["haproxy", "keepalived"]) is True
    pulls = [argv[-1] for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]
    assert pulls == [HAPROXY] * min(len(outcomes), runtime.PULL_ATTEMPTS)
    assert ("auxiliary-haproxy-pulled.json" in markers.records) is pulled
    assert "auxiliary-keepalived-pulled.json" in markers.records


def test_profile_passes_the_bound_bundle_to_every_broker(request_value, tmp_path, monkeypatch, authenticated, auxiliary_brokers):
    del authenticated
    request_value["registryTrustBundleRef"] = "registry-ca"
    worker, unused_markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {IMAGE: ["ok"], HAPROXY: ["ok"], KEEPALIVED: ["ok"]},
                                                       [probe_output(), probe_output()])
    assert prepared_image(worker, {"registryJSON": registry(request_value), "registryCA": BUNDLE}) == {"changed": True}
    assert concurrent_order([argv[-1] for argv in images.calls if argv[:2] == ["/usr/bin/podman", "pull"]]) == concurrent_order([IMAGE, HAPROXY, KEEPALIVED])
    assert concurrent_order([(broker.image, broker.trust) for broker in auxiliary_brokers]) == concurrent_order([(None, BUNDLE), (HAPROXY, BUNDLE),
                                                                                                                 (KEEPALIVED, BUNDLE)])
    assert all(BUNDLE not in str(argv) for argv in images.calls) and all(BUNDLE not in str(value) for value in images.environments)


PROMETHEUS = "cp.icr.io/cp/ibm-ceph/prometheus-synthetic@sha256:" + "f" * 64
PROMTAIL = "cp.icr.io/cp/ibm-ceph/promtail-synthetic@sha256:" + "0" * 64


def test_a_batch_pulls_its_images_at_once_each_with_its_own_broker_and_journal(request_value, tmp_path, monkeypatch, auxiliary_brokers):
    worker, markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {HAPROXY: ["ok"], KEEPALIVED: ["ok"], PROMETHEUS: ["ok"]})
    worker.request["auxiliary"].append({"name": "prometheus", "image": PROMETHEUS})
    together, timeouts, original = threading.Barrier(runtime.PULL_CONCURRENCY, timeout=10), [], ImageSet.__call__

    def gated(self, argv, timeout, limit, environment=None, cancel=None):
        if argv[:2] == ["/usr/bin/podman", "pull"]:
            together.wait()
            timeouts.append(timeout)
        return original(self, argv, timeout, limit, environment, cancel)

    monkeypatch.setattr(ImageSet, "__call__", gated)
    names = ["haproxy", "keepalived", "prometheus"]
    assert worker.images({"registryJSON": registry(request_value)}, {"names": names, "seconds": 900}) == {"changed": True}
    assert timeouts == [900 - runtime.PULL_RESERVE] * 3
    assert sorted(broker.image for broker in auxiliary_brokers) == sorted([HAPROXY, KEEPALIVED, PROMETHEUS])
    scope = runtime.scope(request_value, "node-1")
    for name, reference in zip(names, (HAPROXY, KEEPALIVED, PROMETHEUS)):
        assert markers.records["auxiliary-" + name + "-pulled.json"] == {"scope": scope, "image": reference, "imageID": images.identifier(reference)}
    assert IMAGE not in [argv[-1] for argv in images.calls]


class Deadline(Exception):
    """The worker's deadline, raised by its SIGALRM handler in the main thread."""


@pytest.mark.skipif(threading.current_thread() is not threading.main_thread(), reason="signals reach only the main thread")
def test_a_deadline_while_a_batch_pulls_stops_every_pull_process_and_leaves_every_broker(request_value, tmp_path, monkeypatch):
    request_value["auxiliary"] = [{"name": "haproxy", "image": HAPROXY}, {"name": "keepalived", "image": KEEPALIVED}]
    sleep, lock, started, fired = shutil.which("sleep"), threading.Lock(), threading.Event(), []
    groups, left = [], []
    assert sleep and os.path.isabs(sleep)

    class Recorded(subprocess.Popen):
        def __init__(self, *args, **options):
            super().__init__(*args, **options)
            with lock:
                groups.append(self.pid)
                if len(groups) == 2:
                    started.set()

    class Broker:
        failure_cause, environment = "", {}

        def __init__(self, directory, request, private, deadline, command, *, image=None, trust=None):
            del request, private, deadline, command, trust
            self.image, self.cert_directory = image, os.path.join(directory, "native-certs")

        def __enter__(self):
            return self

        def __exit__(self, kind, unused_value, unused_traceback):
            with lock:
                left.append((self.image, kind))
            return False

    def command(argv, timeout, limit, environment=None, cancel=None):
        del environment
        assert argv[:2] == ["/usr/bin/podman", "pull"] and cancel is not None
        # Each pull is a real process group that only the cancellation can end before its 30 s.
        return ceph_native.bounded_command([sleep, "30"], timeout=timeout, limit=limit, cancel=cancel)

    def inject():
        if started.wait(timeout=10):
            signal.pthread_kill(threading.main_thread().ident, signal.SIGALRM)

    def elapsed(signum, frame):
        del signum, frame
        fired.append(True)
        raise Deadline()

    monkeypatch.setattr(bounded_process.subprocess, "Popen", Recorded)
    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    worker, markers, unused_clock = host_runtime(request_value, tmp_path, command)
    injector = threading.Thread(target=inject)
    previous = signal.signal(signal.SIGALRM, elapsed)
    begun = time.monotonic()
    try:
        injector.start()
        with worker.journal(create=True) as durable, pytest.raises(Deadline):
            worker.pull_auxiliary(durable, registry(request_value), 4285, ["haproxy", "keepalived"])
    finally:
        injector.join(timeout=10)
        signal.signal(signal.SIGALRM, previous)
    # Well before either sleep's 30 s, so the cancellation, not completion, ended both groups.
    assert time.monotonic() - begun < 15
    assert fired == [True] and len(groups) == 2
    for group in groups:
        with pytest.raises(ProcessLookupError):
            os.killpg(group, 0)
    assert sorted(left) == sorted([(HAPROXY, ceph_native.Refusal), (KEEPALIVED, ceph_native.Refusal)])
    scope = runtime.scope(request_value, "node-1")
    for name, reference in (("haproxy", HAPROXY), ("keepalived", KEEPALIVED)):
        assert markers.records["auxiliary-" + name + "-attempt-1.json"] == {"scope": scope, "image": reference, "attempt": 1}
        assert "auxiliary-" + name + "-attempt-2.json" not in markers.records and "auxiliary-" + name + "-pulled.json" not in markers.records


def test_a_cancelled_pull_pause_ends_at_once_and_a_scripted_pause_still_counts(request_value, tmp_path):
    cancel, clock = threading.Event(), Clock()
    cancel.set()
    real = runtime.HostRuntime(request_value, "node-1", lambda *args, **options: (0, b""), markers=Markers(tmp_path / "real"))
    begun = time.monotonic()
    real.rest(runtime.PULL_PAUSE, cancel)
    assert time.monotonic() - begun < 1
    scripted, unused_markers, clock = host_runtime(request_value, tmp_path, lambda *args, **options: (0, b""), clock)
    scripted.rest(runtime.PULL_PAUSE, cancel)
    assert clock.pauses == [runtime.PULL_PAUSE]


def test_the_images_signal_leaves_a_batch_its_cancellation_before_the_supervisor_stops_the_worker():
    assert ceph_host_worker.PULL_FLOOR == runtime.PULL_MINIMUM + runtime.PULL_RESERVE
    assert runtime.PULL_CANCEL_SECONDS < ceph_host_worker.CANCEL_RESERVE < runtime.PULL_RESERVE


def test_a_supervised_images_worker_cleans_up_an_interrupted_batch_before_its_supervisor_deadline(request_value, tmp_path, monkeypatch):
    request_value["auxiliary"] = [{"name": "haproxy", "image": HAPROXY}, {"name": "keepalived", "image": KEEPALIVED}]
    sleep, records = shutil.which("sleep"), tmp_path / "records"
    assert sleep and os.path.isabs(sleep)
    records.mkdir()

    def record(name, value):
        (records / name).write_text(value)

    class Recorded(subprocess.Popen):
        def __init__(self, *args, **options):
            super().__init__(*args, **options)
            record("group-" + str(self.pid), "")

    class Broker:
        failure_cause, environment = "", {}

        def __init__(self, directory, request, private, deadline, command, *, image=None, trust=None):
            del request, private, deadline, command, trust
            self.image, self.cert_directory = image, os.path.join(directory, "native-certs")

        def __enter__(self):
            return self

        def __exit__(self, kind, unused_value, unused_traceback):
            record("broker-haproxy" if self.image == HAPROXY else "broker-keepalived", kind.__name__ if kind else "")
            return False

    def command(argv, timeout, limit, environment=None, cancel=None):
        del environment
        assert argv[:2] == ["/usr/bin/podman", "pull"] and cancel is not None
        return ceph_native.bounded_command([sleep, "30"], timeout=timeout, limit=limit, cancel=cancel)

    def dispatch(envelope):
        del envelope
        worker, unused_markers, unused_clock = host_runtime(request_value, tmp_path, command)
        try:
            with worker.journal(create=True) as durable:
                return worker.pull_auxiliary(durable, registry(request_value), 4285, ["haproxy", "keepalived"])
        except BaseException as error:
            record("interrupted", type(error).__name__)
            raise

    def load(sources):
        del sources
        return SimpleNamespace(Refusal=ceph_native.Refusal, dispatch=dispatch)

    budget = 6

    def work(started, ceiling, *, loader, reserve):
        sys.stdin = io.TextIOWrapper(io.BytesIO(json.dumps({"sources": {}, "payload": {}, "budget": budget}).encode()))
        return ceph_native_worker.work(started, ceiling, loader=loader, reserve=reserve)

    # The real launcher relationship at a test scale: the signal 3 s before the supervisor's 6 s deadline.
    monkeypatch.setattr(ceph_host_worker, "CANCEL_RESERVE", 3)
    monkeypatch.setattr(ceph_host_worker, "PULL_FLOOR", budget)
    monkeypatch.setattr(runtime, "PULL_CANCEL_SECONDS", 2)
    monkeypatch.setattr(bounded_process.subprocess, "Popen", Recorded)
    monkeypatch.setattr(runtime.ceph_pull_broker, "Broker", Broker)
    supervisor = {"work": work, "supervise": ceph_native_worker.supervise}
    started = time.monotonic()
    assert ceph_host_worker.supervised(supervisor, started, budget, load) == {"ok": False, "reason": "native-timeout"}
    # The worker answered before the supervisor's deadline, so its own cleanup, not the group kill, ended the batch.
    assert time.monotonic() - started < budget - 0.5
    assert (records / "interrupted").read_text() == "RemoteDeadline"
    assert {path.name: path.read_text() for path in records.iterdir() if path.name.startswith("broker-")} == {
        "broker-haproxy": "Refusal", "broker-keepalived": "Refusal"}
    groups = [int(path.name.split("-", 1)[1]) for path in records.iterdir() if path.name.startswith("group-")]
    assert len(groups) == 2
    for group in groups:
        # The test's commands own their sessions, so only the batch's cancellation, not the worker's group kill, stops them.
        with pytest.raises(ProcessLookupError):
            os.killpg(group, 0)


@pytest.mark.parametrize("pull", [
    {"names": ["haproxy", "keepalived", "prometheus", "promtail"], "seconds": 900},
    {"names": ["keepalived", "haproxy"], "seconds": 900},
    {"names": ["haproxy", "haproxy"], "seconds": 900},
    {"names": ["grafana"], "seconds": 900},
    {"names": ["haproxy"], "seconds": runtime.IMAGES_CALL_SECONDS + 1},
    {"names": ["haproxy"], "seconds": 0},
    {"names": ["haproxy"], "seconds": 900.0},
    {"names": ["haproxy"]},
])
def test_a_pull_outside_this_hosts_bounded_frozen_batch_or_budget_refuses_before_any_pull(request_value, tmp_path, monkeypatch, auxiliary_brokers,
                                                                                          pull):
    worker, unused_markers, images = auxiliary_runtime(request_value, tmp_path, monkeypatch, {})
    worker.request["auxiliary"] += [{"name": "prometheus", "image": PROMETHEUS}, {"name": "promtail", "image": PROMTAIL}]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        worker.images({"registryJSON": registry(request_value)}, pull)
    assert images.calls == [] and auxiliary_brokers == []


@pytest.mark.parametrize("observed", [True, False, None, "1", 2])
def test_rotational_media_identity_is_frozen(request_value, tmp_path, monkeypatch, observed):
    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: path)
    request_value["hosts"][0]["devices"][0]["rotational"] = True
    worker, unused, unused_clock = host_runtime(request_value, tmp_path, lambda argv, timeout, limit: (0, lsblk(rota=observed)))
    device = request_value["hosts"][0]["devices"][0]
    if observed is True:
        assert worker.block_device(device)["rotational"] == 1
    else:
        with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
            worker.block_device(device)


PHYSICAL_WWN = "0x5000000000000001"
PHYSICAL_PATH = "/dev/disk/by-id/wwn-" + PHYSICAL_WWN
PHYSICAL_HARDWARE = {"uuid": "11111111-1111-4111-8111-111111111111", "macs": ["02:00:00:00:00:01"]}


def physical_runtime(request_value, tmp_path, monkeypatch):
    host = request_value["hosts"][0]
    host.update(physical=True, hardware=copy.deepcopy(PHYSICAL_HARDWARE))
    host["devices"] = [{"path": PHYSICAL_PATH, "wwn": PHYSICAL_WWN, "serial": "", "sizeBytes": 0}]
    osd = request_value["services"].pop()
    osd["placement"]["hosts"] = [h["name"] for h in request_value["hosts"][1:]]
    service = {"service_type": "osd", "service_id": "physical", "placement": {"hosts": [host["name"]]},
               "spec": {"data_devices": {"paths": [PHYSICAL_PATH]}}}
    request_value["services"].extend([osd, service])
    monkeypatch.setattr(runtime, "physical_hardware", lambda host: None)
    monkeypatch.setattr(runtime, "sysfs_serial", lambda node: "serial-1" if node == "/dev/sda" else None)
    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: "/dev/sda" if path == PHYSICAL_PATH else path)
    state = {"disk": {"path": "/dev/sda", "type": "disk", "serial": "serial-1", "size": 1 << 40, "rota": True,
                      "maj:min": "8:0", "wwn": PHYSICAL_WWN, "ro": False, "rm": False},
             "udev": "MAJOR=8\nMINOR=0\nID_VENDOR=SYNTHETIC\nID_MODEL=DISK\nID_SCSI_SERIAL=serial-1\nID_WWN=" + PHYSICAL_WWN + "\n"}
    podman = Podman(request_value, present=True)

    def command(argv, timeout, limit):
        if argv[0] == "/usr/bin/lsblk":
            return 0, json.dumps({"blockdevices": [state["disk"]]}).encode()
        if argv[0] == "/usr/bin/udevadm":
            return 0, state["udev"].encode()
        return podman(argv, timeout, limit)

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    seed_profile(markers, request_value)
    result = {"version": "ceph-host-device-id-v1", "deviceID": "SYNTHETIC_DISK_serial-1",
              "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}
    monkeypatch.setattr(worker, "contained", lambda *args, **kwargs: result)
    return worker, markers, state


def test_physical_device_binds_real_serial_and_size_and_observes_without_writes(request_value, tmp_path, monkeypatch):
    worker, markers, unused = physical_runtime(request_value, tmp_path, monkeypatch)
    assert worker.device_identity() == {"changed": True}
    retained = copy.deepcopy(markers.records)
    row = retained[runtime.DEVICES]["devices"][0]
    assert row == {"host": "node-1", "path": PHYSICAL_PATH, "wwn": PHYSICAL_WWN, "serial": "serial-1", "kernelPath": "/dev/sda",
                   "deviceID": "SYNTHETIC_DISK_serial-1", "sizeBytes": 1 << 40, "rotational": True}
    assert worker.physical_readback([row]) == [row]
    assert worker.device_identity() == {"changed": False}
    assert markers.records == retained
    assert worker.request["hosts"][0]["devices"][0]["serial"] == ""


@pytest.mark.parametrize("field,value", [("wwn", "0x5000000000000002"), ("serial", "replacement"), ("size", 4096), ("rota", False),
                                         ("path", "/dev/sdb"), ("type", "part"), ("ro", True), ("rm", True), ("maj:min", "8:16")])
def test_physical_device_observation_refuses_changed_live_identity(request_value, tmp_path, monkeypatch, field, value):
    worker, markers, state = physical_runtime(request_value, tmp_path, monkeypatch)
    worker.device_identity()
    retained = copy.deepcopy(markers.records)
    state["disk"][field] = value
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        worker.physical_readback(retained[runtime.DEVICES]["devices"])
    assert markers.records == retained


def test_a_wwn_selection_refuses_as_device_root_when_only_the_sysfs_serial_is_the_roots(request_value, tmp_path, monkeypatch):
    # udev rewrites a / in a serial to _, so lsblk can disagree with the sysfs reading the installer proved the root by.
    request_value["hosts"][0]["rootSerial"] = "ROOT/1"
    worker, markers, state = physical_runtime(request_value, tmp_path, monkeypatch)
    state["disk"]["serial"] = "ROOT_1"
    state["udev"] = state["udev"].replace("ID_SCSI_SERIAL=serial-1", "ID_SCSI_SERIAL=ROOT_1")
    monkeypatch.setattr(runtime, "sysfs_serial", lambda node: "ROOT/1")
    before = copy.deepcopy(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^device-root$"):
        worker.device_identity()
    assert markers.records == before


def test_sysfs_serial_reads_the_installers_attribute_and_none_when_absent(tmp_path):
    node = tmp_path / "class/block/sdq/device"
    node.mkdir(parents=True)
    (node / "serial").write_text("ROOT/1\n")
    assert runtime.sysfs_serial("/dev/sdq", str(tmp_path)) == "ROOT/1"
    assert runtime.sysfs_serial("/dev/sdr", str(tmp_path)) is None


@pytest.mark.parametrize("root_serial,refused", [("serial-1", True), ("serial-2", False), (None, False)])
def test_a_physical_host_refuses_a_selected_disk_that_is_its_machines_root_disk_before_any_record(request_value, tmp_path, monkeypatch, root_serial, refused):
    if root_serial is not None:
        request_value["hosts"][0]["rootSerial"] = root_serial
    worker, markers, unused = physical_runtime(request_value, tmp_path, monkeypatch)
    before = copy.deepcopy(markers.records)
    if refused:
        with pytest.raises(ceph_native.Refusal, match="^device-root$"):
            worker.device_identity()
        assert markers.records == before
    else:
        assert worker.device_identity() == {"changed": True}


@pytest.mark.parametrize("damage", ["uuid", "missing-mac", "extra-mac", "unreadable"])
def test_physical_hardware_requires_original_uuid_and_complete_mac_inventory(tmp_path, damage):
    uuid = tmp_path / "class/dmi/id/product_uuid"
    uuid.parent.mkdir(parents=True)
    uuid.write_text(PHYSICAL_HARDWARE["uuid"] + "\n")
    nic = tmp_path / "class/net/eth0"
    (nic / "device").mkdir(parents=True)
    (nic / "address").write_text(PHYSICAL_HARDWARE["macs"][0] + "\n")
    host = {"physical": True, "hardware": PHYSICAL_HARDWARE}
    runtime.physical_hardware(host, str(tmp_path))
    if damage == "uuid":
        uuid.write_text("22222222-2222-4222-8222-222222222222\n")
    elif damage == "missing-mac":
        (nic / "device").rmdir()
    elif damage == "extra-mac":
        other = tmp_path / "class/net/eth1"
        (other / "device").mkdir(parents=True)
        (other / "address").write_text("02:00:00:00:00:02\n")
    else:
        uuid.unlink()
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        runtime.physical_hardware(host, str(tmp_path))


@pytest.mark.parametrize("changed", [False, True])
def test_physical_lost_sdk_reply_recovers_only_the_original_disk_binding(request_value, tmp_path, monkeypatch, changed):
    worker, markers, state = physical_runtime(request_value, tmp_path, monkeypatch)
    original = worker.contained

    def interrupted(*args, **kwargs):
        raise ceph_native.Refusal("synthetic-lost-reply")

    monkeypatch.setattr(worker, "contained", interrupted)
    with pytest.raises(ceph_native.Refusal, match="synthetic-lost-reply"):
        worker.device_identity()
    assert runtime.BINDING in markers.records and runtime.DEVICES not in markers.records
    retained = copy.deepcopy(markers.records)
    monkeypatch.setattr(worker, "contained", original)
    if changed:
        state["disk"]["size"] += 4096
        with pytest.raises(ceph_native.Refusal, match="device-identity"):
            worker.device_identity()
        assert markers.records == retained
    else:
        assert worker.device_identity() == {"changed": True}
        assert markers.records[runtime.BINDING] == retained[runtime.BINDING]


def test_physical_missing_completed_identity_cannot_relearn_a_replacement(request_value, tmp_path, monkeypatch):
    worker, markers, state = physical_runtime(request_value, tmp_path, monkeypatch)
    worker.device_identity()
    del markers.records[runtime.DEVICES]
    state["disk"]["size"] += 4096
    retained = copy.deepcopy(markers.records)
    with pytest.raises(ceph_native.Refusal, match="device-identity"):
        worker.device_identity()
    assert markers.records == retained


# The Go planner admits as many disks per physical host as install-time clearing admits, 15.
PHYSICAL_DISKS_BOUND = 15


def physical_disks_runtime(request_value, tmp_path, monkeypatch, disks):
    """One physical host selecting several WWN disks, each with its own kernel name, serial and udev identity."""
    host = request_value["hosts"][0]
    host.update(physical=True, hardware=copy.deepcopy(PHYSICAL_HARDWARE))
    wwns = ["0x50000000000001%02x" % disk for disk in range(disks)]
    host["devices"] = [{"path": "/dev/disk/by-id/wwn-" + wwn, "wwn": wwn, "serial": "", "sizeBytes": 0} for wwn in wwns]
    osd = request_value["services"].pop()
    osd["placement"]["hosts"] = [h["name"] for h in request_value["hosts"][1:]]
    service = {"service_type": "osd", "service_id": "physical", "placement": {"hosts": [host["name"]]},
               "spec": {"data_devices": {"paths": [device["path"] for device in host["devices"]]}}}
    request_value["services"].extend([osd, service])
    kernel = {device["path"]: "/dev/nvme%dn1" % disk for disk, device in enumerate(host["devices"])}
    state = {"/dev/nvme%dn1" % disk: {"path": "/dev/nvme%dn1" % disk, "type": "disk", "serial": "serial-%d" % disk, "size": 1 << 40, "rota": False,
                                      "maj:min": "259:%d" % disk, "wwn": wwn, "ro": False, "rm": False} for disk, wwn in enumerate(wwns)}
    monkeypatch.setattr(runtime, "physical_hardware", lambda host: None)
    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: kernel.get(path, path))
    podman = Podman(request_value, present=True)

    def command(argv, timeout, limit):
        if argv[0] == "/usr/bin/lsblk":
            return 0, json.dumps({"blockdevices": [state[argv[-1]]]}).encode()
        if argv[0] == "/usr/bin/udevadm":
            name = argv[-1].split("=", 1)[1]
            disk = state[kernel.get(name, name)]
            major, minor = disk["maj:min"].split(":")
            return 0, ("MAJOR=%s\nMINOR=%s\nID_MODEL=SYNTHETIC_NVME\nID_SERIAL_SHORT=%s\nID_WWN=%s\n" % (major, minor, disk["serial"], disk["wwn"])).encode()
        return podman(argv, timeout, limit)

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    seed_profile(markers, request_value)
    probes = []

    def contained(*args, **kwargs):
        environment = json.loads(args[3][0])["environment"]
        probes.append(environment)
        return {"version": "ceph-host-device-id-v1", "deviceID": runtime.native_form(environment),
                "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}

    monkeypatch.setattr(worker, "contained", contained)
    return worker, markers, state, probes


@pytest.mark.parametrize("disks", [3, 5, PHYSICAL_DISKS_BOUND])
def test_physical_host_binds_every_selected_disk_in_one_device_identity_call(request_value, tmp_path, monkeypatch, disks):
    worker, markers, state, probes = physical_disks_runtime(request_value, tmp_path, monkeypatch, disks)
    assert worker.device_identity() == {"changed": True}
    binding, rows = markers.records[runtime.BINDING]["devices"], markers.records[runtime.DEVICES]["devices"]
    assert len(probes) == disks and len(binding) == len(rows) == disks
    assert [row["path"] for row in rows] == [device["path"] for device in worker.host["devices"]]
    assert [row["kernelPath"] for row in rows] == ["/dev/nvme%dn1" % disk for disk in range(disks)]
    assert len({row["deviceID"] for row in rows}) == disks and all(row["wwn"] == device["wwn"] for row, device in zip(rows, worker.host["devices"]))
    retained = copy.deepcopy(markers.records)
    assert worker.device_identity() == {"changed": False} and markers.records == retained
    assert worker.host_devices() == {"devices": rows}
    state["/dev/nvme%dn1" % (disks - 1)]["serial"] = "replacement"
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        worker.host_devices()
    assert markers.records == retained


def test_physical_host_evidence_at_the_disk_bound_fits_its_decoder_with_the_longest_fields(request_value):
    devices = [{"path": "/dev/disk/by-id/wwn-0x" + "%032x" % disk, "serial": "S" * 128, "deviceID": "D" * 512, "wwn": "0x" + "%032x" % disk,
                "kernelPath": "/dev/nvme%dn1" % disk, "sizeBytes": (1 << 63) - 1, "rotational": False} for disk in range(PHYSICAL_DISKS_BOUND)]
    evidence = {"version": runtime.EVIDENCE_VERSION, "request": "d" * 64, "host": "h" * 253, "owner": "o" * 253, "fsid": request_value["fsid"],
                "image": IMAGE, "ready": True, "removed": False, "imageID": "c" * 64, "uid": 167, "gid": 167, "label": runtime.SHARED_LABEL,
                "policySHA256": "f" * 64, "cephadmSHA256": "e" * 64, "planDigest": "a" * 64, "route": True, "devices": devices}
    assert len(ceph_native.canonical(evidence)) <= 16000


SERIAL = "SYNTH-SERIAL-1"
SERIAL_PATH = "/dev/disk/by-serial/" + SERIAL
SERIAL_SIZE = 1 << 40
NVME_ALIAS = "/dev/disk/by-id/nvme-SYNTHETIC_NVME_" + SERIAL


def sysfs_disk(root, name, serial, removable="0", ro="0", partition=False):
    node = root / "class/block" / name
    (node / "device").mkdir(parents=True)
    (node / "device/serial").write_text(serial + "\n")
    (node / "removable").write_text(removable + "\n")
    (node / "ro").write_text(ro + "\n")
    if partition:
        (node / "partition").write_text("1\n")
    return node


def serial_runtime(request_value, tmp_path, monkeypatch, wwn=None):
    """One physical host selecting one disk by its Machine's declared serial number, resolved over a synthetic sysfs."""
    host = request_value["hosts"][0]
    host.update(physical=True, hardware=copy.deepcopy(PHYSICAL_HARDWARE))
    host["devices"] = [{"path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE}]
    osd = request_value["services"].pop()
    osd["placement"]["hosts"] = [h["name"] for h in request_value["hosts"][1:]]
    service = {"service_type": "osd", "service_id": "physical", "placement": {"hosts": [host["name"]]},
               "spec": {"data_devices": {"paths": [SERIAL_PATH]}}}
    request_value["services"].extend([osd, service])
    sysfs = tmp_path / "sys"
    sysfs_disk(sysfs, "nvme0n1", SERIAL)
    sysfs_disk(sysfs, "nvme0n1p1", SERIAL, partition=True)
    (sysfs / "class/block/sdz").mkdir()
    original = runtime.serial_disk
    monkeypatch.setattr(runtime, "serial_disk", lambda serial: original(serial, str(sysfs)))
    monkeypatch.setattr(runtime, "physical_hardware", lambda host: None)
    links = {NVME_ALIAS: "/dev/nvme0n1", NVME_ALIAS + "_1": "/dev/nvme0n1", "/dev/disk/by-id/wwn-0x5000000000000009": "/dev/nvme0n1",
             "/dev/disk/by-id/nvme-OTHER": "/dev/nvme0n1"}
    state = {"links": links, "disk": {"path": "/dev/nvme0n1", "type": "disk", "serial": SERIAL, "size": SERIAL_SIZE, "rota": False,
                                      "maj:min": "259:0", "wwn": wwn, "ro": False, "rm": False},
             "devlinks": [NVME_ALIAS + "_1", NVME_ALIAS, "/dev/disk/by-id/nvme-OTHER", "/dev/disk/by-path/pci-0000:01:00.0-nvme-1"]}
    if wwn:
        state["devlinks"].append("/dev/disk/by-id/wwn-" + wwn)
    monkeypatch.setattr(runtime.os.path, "realpath", lambda path: state["links"].get(path, path))
    podman = Podman(request_value, present=True)

    def command(argv, timeout, limit):
        if argv[0] == "/usr/bin/lsblk":
            assert argv[-1] == state["disk"]["path"]
            return 0, json.dumps({"blockdevices": [state["disk"]]}).encode()
        if argv[0] == "/usr/bin/udevadm":
            assert argv[-1] == "--name=" + state["disk"]["path"]
            major, minor = state["disk"]["maj:min"].split(":")
            wwns = "ID_WWN=%s\nID_WWN_WITH_EXTENSION=%s\n" % (state["disk"]["wwn"], state["disk"]["wwn"]) if state["disk"]["wwn"] else ""
            return 0, ("MAJOR=%s\nMINOR=%s\nID_MODEL=SYNTHETIC_NVME\nID_SERIAL_SHORT=%s\n%sDEVLINKS=%s\n"
                       % (major, minor, state["disk"]["serial"], wwns, " ".join(state["devlinks"]))).encode()
        return podman(argv, timeout, limit)

    worker, markers, unused = host_runtime(request_value, tmp_path, command)
    seed_profile(markers, request_value)

    def contained(*args, **kwargs):
        return {"version": "ceph-host-device-id-v1", "deviceID": runtime.native_form(json.loads(args[3][0])["environment"]),
                "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}

    monkeypatch.setattr(worker, "contained", contained)
    return worker, markers, state, sysfs


@pytest.mark.parametrize("wwn,alias", [(None, NVME_ALIAS), ("0x5000000000000009", "/dev/disk/by-id/wwn-0x5000000000000009")])
def test_a_serial_selection_binds_one_disk_with_its_stable_alias(request_value, tmp_path, monkeypatch, wwn, alias):
    worker, markers, unused, unused_sysfs = serial_runtime(request_value, tmp_path, monkeypatch, wwn)
    assert worker.device_identity() == {"changed": True}
    row = markers.records[runtime.DEVICES]["devices"][0]
    assert row == {"host": "node-1", "path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE, "wwn": wwn or "", "kernelPath": "/dev/nvme0n1",
                   "alias": alias, "deviceID": "SYNTHETIC_NVME_" + SERIAL, "rotational": False}
    assert markers.records[runtime.BINDING]["devices"] == [row]
    retained = copy.deepcopy(markers.records)
    assert worker.device_identity() == {"changed": False} and markers.records == retained
    assert worker.host_devices() == {"devices": [row]}
    assert worker.request["hosts"][0]["devices"] == [{"path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE}]


@pytest.mark.parametrize("damage,reason", [
    ("absent", "device-serial-absent"), ("multipath", "device-serial-ambiguous"), ("namespace", "device-serial-ambiguous"),
    ("removable", "device-serial-mismatch"), ("read-only", "device-serial-mismatch"), ("virtual", "device-serial-mismatch"),
    ("size", "device-serial-mismatch"), ("lsblk-read-only", "device-serial-mismatch"), ("no-alias", "device-alias"),
    ("foreign-alias", "device-alias"),
])
def test_a_serial_selection_refuses_before_any_record_unless_exactly_one_qualified_disk_reports_it(request_value, tmp_path, monkeypatch, damage, reason):
    worker, markers, state, sysfs = serial_runtime(request_value, tmp_path, monkeypatch)
    block = sysfs / "class/block"
    if damage == "absent":
        (block / "nvme0n1/device/serial").write_text("ANOTHER\n")
    elif damage == "multipath":
        sysfs_disk(sysfs, "sdb", SERIAL)
    elif damage == "namespace":
        sysfs_disk(sysfs, "nvme0n2", SERIAL)
    elif damage == "removable":
        (block / "nvme0n1/removable").write_text("1\n")
    elif damage == "read-only":
        (block / "nvme0n1/ro").write_text("1\n")
    elif damage == "virtual":
        (block / "nvme0n1").rename(block / "vda")
    elif damage == "size":
        state["disk"]["size"] += 4096
    elif damage == "lsblk-read-only":
        state["disk"]["ro"] = True
    elif damage == "no-alias":
        state["devlinks"] = ["/dev/disk/by-path/pci-0000:01:00.0-nvme-1", "/dev/disk/by-id/nvme-eui.0000000000000001"]
    else:
        state["links"] = {}
    before = copy.deepcopy(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        worker.device_identity()
    assert markers.records == before


@pytest.mark.parametrize("physical,root_serial", [(False, "ROOT-1"), (True, ""), (True, "ROOT 1"), (True, "R" * 257)])
def test_a_root_serial_is_admitted_only_on_a_physical_host_and_only_in_its_declared_form(request_value, tmp_path, physical, root_serial):
    request_value["hosts"][0]["rootSerial"] = root_serial
    if physical:
        request_value["hosts"][0].update(physical=True, hardware=copy.deepcopy(PHYSICAL_HARDWARE))
        request_value["hosts"][0]["devices"] = []
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        host_runtime(request_value, tmp_path, lambda argv, timeout, limit: (0, b""))


@pytest.mark.parametrize("physical,selects", [(True, False), (False, True)])
def test_a_virtio_alias_selects_a_device_by_serial_only_on_a_virtual_host(physical, selects):
    host = {"physical": physical, "devices": [{"path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE}]}
    alias = "/dev/disk/by-id/virtio-" + SERIAL
    assert ceph_native.selected_device(host, SERIAL_PATH) == host["devices"][0]
    if selects:
        assert ceph_native.selected_device(host, alias) == host["devices"][0]
    else:
        with pytest.raises(ceph_native.Refusal, match="^device-unproved$"):
            ceph_native.selected_device(host, alias)


def test_a_serial_selection_ignores_the_hidden_path_nodes_of_its_dual_port_nvme_disk(request_value, tmp_path, monkeypatch):
    worker, markers, unused, sysfs = serial_runtime(request_value, tmp_path, monkeypatch)
    (sysfs_disk(sysfs, "nvme0c0n1", SERIAL) / "hidden").write_text("1\n")
    assert worker.device_identity() == {"changed": True}
    assert markers.records[runtime.DEVICES]["devices"][0]["kernelPath"] == "/dev/nvme0n1"


@pytest.mark.parametrize("hidden", [None, "0", "yes"])
def test_a_serial_selection_counts_a_path_node_that_is_not_proved_hidden(request_value, tmp_path, monkeypatch, hidden):
    worker, markers, unused, sysfs = serial_runtime(request_value, tmp_path, monkeypatch)
    node = sysfs_disk(sysfs, "nvme0c0n1", SERIAL)
    if hidden is not None:
        (node / "hidden").write_text(hidden + "\n")
    before = copy.deepcopy(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^device-serial-ambiguous$"):
        worker.device_identity()
    assert markers.records == before


def test_a_serial_selection_of_the_machines_root_disk_refuses_as_device_root(request_value, tmp_path, monkeypatch):
    request_value["hosts"][0]["rootSerial"] = SERIAL
    worker, markers, unused, unused_sysfs = serial_runtime(request_value, tmp_path, monkeypatch)
    before = copy.deepcopy(markers.records)
    with pytest.raises(ceph_native.Refusal, match="^device-root$"):
        worker.device_identity()
    assert markers.records == before


@pytest.mark.parametrize("change", ["kernel-path", "alias", "wwn", "size", "absent"])
def test_serial_readback_refuses_any_change_of_kernel_path_alias_wwn_or_size(request_value, tmp_path, monkeypatch, change):
    worker, markers, state, sysfs = serial_runtime(request_value, tmp_path, monkeypatch)
    worker.device_identity()
    retained = copy.deepcopy(markers.records)
    block = sysfs / "class/block"
    if change == "kernel-path":
        (block / "nvme0n1").rename(block / "nvme1n1")
        state["disk"]["path"] = "/dev/nvme1n1"
        state["links"] = {link: "/dev/nvme1n1" for link in state["links"]}
    elif change == "alias":
        state["devlinks"].append("/dev/disk/by-id/nvme-A_" + SERIAL)
        state["links"]["/dev/disk/by-id/nvme-A_" + SERIAL] = "/dev/nvme0n1"
    elif change == "wwn":
        state["disk"]["wwn"] = "eui.0000000000000001"
    elif change == "size":
        state["disk"]["size"] += 4096
    else:
        (block / "nvme0n1/device/serial").write_text("ANOTHER\n")
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        worker.host_devices()
    assert markers.records == retained


def test_device_rows_hold_a_serial_selection_and_a_wwn_selection_to_their_own_key_sets(request_value):
    host = request_value["hosts"][0]
    host.update(physical=True, hardware=copy.deepcopy(PHYSICAL_HARDWARE))
    host["devices"] = [{"path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE},
                       {"path": PHYSICAL_PATH, "wwn": PHYSICAL_WWN, "serial": "", "sizeBytes": 0}]
    serial = {"host": "node-1", "path": SERIAL_PATH, "serial": SERIAL, "sizeBytes": SERIAL_SIZE, "wwn": "", "kernelPath": "/dev/nvme0n1",
              "alias": NVME_ALIAS, "deviceID": "SYNTHETIC_NVME_" + SERIAL, "rotational": False}
    wwn = {"host": "node-1", "path": PHYSICAL_PATH, "wwn": PHYSICAL_WWN, "serial": "serial-1", "kernelPath": "/dev/sda",
           "deviceID": "SYNTHETIC_DISK_serial-1", "sizeBytes": 1 << 40, "rotational": True}
    assert runtime.row(serial, request_value, "node-1") == serial and runtime.row(wwn, request_value, "node-1") == wwn
    for damaged in ({key: value for key, value in serial.items() if key != "alias"}, dict(serial, alias="/dev/nvme0n1"),
                    dict(serial, alias=SERIAL_PATH), dict(serial, serial="ANOTHER"), dict(serial, sizeBytes=SERIAL_SIZE + 4096),
                    dict(serial, wwn=None), dict(serial, wwn="bad wwn"), dict(wwn, alias=NVME_ALIAS)):
        with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
            runtime.row(damaged, request_value, "node-1")


def test_a_serial_selection_never_resolves_an_unbounded_block_class(tmp_path):
    for index in range(runtime.MAX_BLOCK_ENTRIES + 1):
        (tmp_path / "class/block" / ("loop%d" % index)).mkdir(parents=True)
    with pytest.raises(ceph_native.Refusal, match="^device-identity$"):
        runtime.serial_disk(SERIAL, str(tmp_path))


def test_physical_host_evidence_of_serial_selections_at_the_disk_bound_fits_its_decoder_with_the_longest_fields(request_value):
    devices = [{"path": "/dev/disk/by-serial/" + "S" * 127 + chr(65 + disk), "serial": "S" * 127 + chr(65 + disk), "deviceID": "D" * 512,
                "wwn": "W" * 256, "kernelPath": "/dev/nvme%dn1" % disk, "sizeBytes": (1 << 63) - 1, "rotational": False,
                "alias": "/dev/disk/by-id/" + "A" * 200} for disk in range(PHYSICAL_DISKS_BOUND)]
    evidence = {"version": runtime.EVIDENCE_VERSION, "request": "d" * 64, "host": "h" * 253, "owner": "o" * 253, "fsid": request_value["fsid"],
                "image": IMAGE, "ready": True, "removed": False, "imageID": "c" * 64, "uid": 167, "gid": 167, "label": runtime.SHARED_LABEL,
                "policySHA256": "f" * 64, "cephadmSHA256": "e" * 64, "planDigest": "a" * 64, "route": True, "devices": devices}
    assert len(ceph_native.canonical(evidence)) <= 24000 < 24576
