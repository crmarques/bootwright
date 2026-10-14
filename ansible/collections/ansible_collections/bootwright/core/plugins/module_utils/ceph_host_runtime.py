"""Per-host Ceph preparation from the exact frozen image, without any session.

The ordinary native worker ships this module. Each phase is journaled under
root-owned markers scoped by the original apply's owner, the cluster FSID and
the host, so an interrupted call resumes from what it proved. Facts come from
the pulled image's own contained processes, never from an operator report.
The registry credential reaches the host only as a protected authfile that
exists for one pull; it never enters argv, logs, journals or evidence.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import hashlib
import inspect
import json
import os
from pathlib import Path
import re
import shutil
import stat
import tempfile
import time
import uuid

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_host_route_probe as probe, ceph_native, ceph_native_observe as observe, ceph_tunnel, ceph_tunnel_custody as custody,
)

ROOT = "/var/lib/bootwright/ceph-host"
PACKAGE_ROOT = "/var/lib/bootwright/ceph-packages"
VERSION = "ceph-host-v1"
PROFILE_VERSION = "ceph-runtime-profile-v2"
ROUTE_VERSION = "ceph-host-route-v1"
DEVICES_VERSION = "ceph-device-identities-v1"
EVIDENCE_VERSION = "ceph-host-evidence-v1"
PROFILE = "runtime-profile.json"
PAYLOAD = "cephadm-payload"
ROUTE = "route.json"
DEVICES = "device-identities.json"
CEPHADM_SHA256 = "027222324b11d1af53b72b4ebb17134f58e477f63c39e9d07be40ba1fc2aabc1"
SHARED_LABEL = "system_u:object_r:container_file_t:s0"
SENTINEL = b"bootwright-contained-public-label-probe-v1\n"
# Podman adds HOSTNAME to a started container's environment unless one is set, so the probe pins both.
PROBE_HOSTNAME = "bootwright-probe"
PROBE_ENV = frozenset(("HOME=/tmp", "PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOSTNAME=" + PROBE_HOSTNAME))
# The read-only relabelled bind the probe requests, plus the defaults podman records beside it.
BIND_OPTIONS = frozenset(("ro", "z", "rprivate", "nosuid", "nodev", "rbind"))
IMAGE = re.compile(r"cp\.icr\.io/cp/ibm-ceph/ceph-9-rhel9@sha256:[0-9a-f]{64}\Z")
IDENTIFIER = re.compile(r"[0-9a-f]{64}\Z")
DEVICE_ID = re.compile(r"[A-Za-z0-9_.:+/-]{1,512}\Z")
MAX_PAYLOAD = 2 << 20
MAX_PROBE = 4 << 20
PULL_ATTEMPTS = 3
PULL_SECONDS = 1200
PULL_MINIMUM = 60
PULL_RESERVE = 120
PULL_PAUSE = 15
PROFILE_CALL_SECONDS = 4285
PHASES = frozenset(("profile", "route", "device_identity", "host_devices", "host_observe"))
ARTIFACTS = ("/usr/sbin/cephadm", "/usr/share/ceph/mgr/cephadm/ssh.py", "/usr/share/ceph/mgr/cephadm/module.py",
             "/usr/share/ceph/mgr/cephadm/serve.py", "/usr/bin/ceph", "/usr/bin/ceph-authtool")
SOURCES = {
    "/usr/share/ceph/mgr/cephadm/ssh.py": probe.PROVIDER_SHA256,
    "/usr/share/ceph/mgr/cephadm/module.py": "fe45b0e9d91cb57cdf3a109322ba11a355119d812dabe941ef112dbfbdadceec",
    "/usr/share/ceph/mgr/cephadm/serve.py": "ae991c31e84f5f0b5607fa16c5ec66deb730915180fcf7e21767ae1fef45bc86",
}
EXPECTED_CASES = {
    "correctKey": (1, 1, 1, 1, 1, 0), "wrongKey": (1, 0, 0, 0, 0, 0),
    "unknownTarget": (0, 0, 0, 0, 0, 0), "unavailableProxy": (0, 0, 0, 0, 0, 0),
    "missingNativeConfig": (0, 0, 0, 0, 0, 0),
}
UDEV_FIELDS = frozenset(("ID_VENDOR", "ID_MODEL", "ID_MODEL_ENC", "ID_SERIAL_SHORT", "ID_SCSI_SERIAL", "ID_SERIAL"))
Refusal = ceph_native.Refusal

PROBE = r'''
import base64, hashlib, json, os, pwd, stat, tempfile
import asyncssh

def file(path, payload=False):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        before = os.fstat(handle.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or
                before.st_mode & 0o022 or before.st_nlink != 1 or not 0 < before.st_size <= 64 << 20):
            raise ValueError("artifact")
        digest, data, count = hashlib.sha256(), bytearray(), 0
        while True:
            chunk = handle.read(65536)
            if not chunk:
                break
            count += len(chunk)
            if count > before.st_size or payload and count > 2 << 20:
                raise ValueError("artifact")
            digest.update(chunk)
            if payload:
                data.extend(chunk)
        fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
        expected = tuple(getattr(before, key) for key in fields)
        if count != before.st_size or any(tuple(getattr(info, key) for key in fields) != expected
                                           for info in (os.fstat(handle.fileno()), os.stat(path, follow_symlinks=False))):
            raise ValueError("artifact")
        result = {"sha256": digest.hexdigest(), "bytes": count, "mode": stat.S_IMODE(before.st_mode),
                  "uid": before.st_uid, "gid": before.st_gid}
        return result, base64.b64encode(data).decode() if payload else None

def collect():
    account = pwd.getpwnam("ceph")
    if account.pw_uid <= 0 or account.pw_uid != account.pw_gid or account.pw_uid >= 65536:
        raise ValueError("account")
    artifacts, payload = {}, None
    paths = ("/usr/sbin/cephadm", "/usr/share/ceph/mgr/cephadm/ssh.py", "/usr/share/ceph/mgr/cephadm/module.py",
             "/usr/share/ceph/mgr/cephadm/serve.py", "/usr/bin/ceph", "/usr/bin/ceph-authtool")
    for path in paths:
        artifact, body = file(path, payload=path == "/usr/sbin/cephadm")
        artifacts[path] = artifact
        if body is not None:
            payload = body
    with tempfile.TemporaryDirectory(prefix="bw-parser-", dir="/tmp") as directory:
        path = directory + "/ssh_config"
        with open(path, "x") as destination:
            destination.write("Host *\n  ProxyCommand /bin/false\n")
        options = asyncssh.SSHClientConnectionOptions(host="192.0.2.1", username="bootwright",
                                                      client_keys=[], known_hosts=None, config=[path])
        if options.proxy_command != ["/bin/false"]:
            raise ValueError("parser")
    policy_path = os.path.realpath("/etc/crypto-policies/back-ends/openssh.config")
    if not policy_path.startswith(("/etc/crypto-policies/", "/usr/share/crypto-policies/")):
        raise ValueError("policy")
    fd = os.open(policy_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > 65536:
            raise ValueError("policy")
        policy = handle.read(65537)
        if len(policy) != info.st_size or len(policy) > 65536:
            raise ValueError("policy")
        policy = policy.decode("ascii")
    return {"version": "ceph-host-image-v1", "uid": os.getuid(), "gid": os.getgid(),
            "cephUID": account.pw_uid, "cephGID": account.pw_gid, "machine": os.uname().machine,
            "artifacts": artifacts, "cephadm": payload, "cryptoPolicy": policy}

try:
    print(json.dumps(collect(), sort_keys=True, separators=(",", ":")))
except Exception:
    raise SystemExit(1)
'''

LABEL_PROBE = r'''
import json, os, stat
path = "/run/bootwright-label/proof"
fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
try:
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_nlink != 1:
        raise ValueError("file")
    if os.read(fd, 128) != b"bootwright-contained-public-label-probe-v1\n":
        raise ValueError("bytes")
    if not os.fstatvfs(fd).f_flag & os.ST_RDONLY:
        raise ValueError("mount")
    print(json.dumps({"read": True, "readOnly": True, "uid": os.getuid(), "gid": os.getgid()}))
finally:
    os.close(fd)
'''

DEVICE_PROBE = r'''
import json,sys,types
from ceph_volume.util.device import Device
from ceph_volume.util import disk
value=json.loads(sys.argv[1])
if set(value)!={"path","environment"}:
    raise AssertionError
original=disk.UdevData
class OriginalInput:
    def __init__(self,path):
        if path!=value["path"]:
            raise AssertionError
        self.environment=dict(value["environment"])
try:
    disk.UdevData=OriginalInput
    identity=Device._get_device_id(types.SimpleNamespace(path=value["path"]))
finally:
    disk.UdevData=original
if not isinstance(identity,str):
    raise AssertionError
print(json.dumps({"version":"ceph-host-device-id-v1","deviceID":identity,
                  "dependencyInput":"ceph_volume.util.disk.UdevData.environment"},separators=(",",":")))
'''


def require(condition, reason="image-profile"):
    ceph_native.require(condition, reason)


def canonical(value):
    return ceph_native.canonical(value)


def identity(info):
    return tuple(getattr(info, key) for key in ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))


def module_source(module):
    value = getattr(module, "_BOOTWRIGHT_SOURCE", None)
    return value if isinstance(value, str) else inspect.getsource(module)


def scope(request, host):
    return {"version": VERSION, "host": host, "cluster": ceph_native.marker_identity(request)}


@contextlib.contextmanager
def journal(request, host, *, create=False, root=None, markers=None):
    """The host block's own durable markers, never the cluster block's."""
    durable = markers or ceph_native.Markers(request, root=root or ROOT)
    if create:
        durable.create()
    require(durable.owned(), "ownership")
    held = contextlib.nullcontext(True) if markers is not None else observe.fixed_directory(durable.path)
    # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
    # pylint: disable-next=contextmanager-generator-missing-cleanup
    with held as directory:
        require(directory is not None, "ownership")
        record = durable.read("scope.json")
        expected = scope(request, host)
        require(record is None and create or canonical(record) == canonical(expected), "ownership")
        if record is None:
            durable.write("scope.json", expected)
        yield durable


def prepared(request, host, root=None, markers=None):
    """A read-only probe: None when this host was never prepared for the cluster."""
    durable = markers or ceph_native.Markers(request, root=root or ROOT)
    if not durable.owned():
        return None
    record = durable.read("scope.json")
    require(canonical(record) == canonical(scope(request, host)), "ownership")
    return durable


@contextlib.contextmanager
def owned_scratch(parent, prefix):
    """A failed consumer keeps its private directory under the owned markers."""
    path = tempfile.mkdtemp(prefix=prefix, dir=parent)
    before = os.stat(path, follow_symlinks=False)
    yield path
    after = os.stat(path, follow_symlinks=False)
    require(stat.S_ISDIR(after.st_mode) and (after.st_dev, after.st_ino, after.st_uid, after.st_gid, after.st_mode) ==
            (before.st_dev, before.st_ino, before.st_uid, before.st_gid, before.st_mode))
    shutil.rmtree(path)


def discard_residue(parent, prefix):
    """A killed pull may leave its private scratch, authfile included; remove it before the next."""
    directory = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for name in os.listdir(directory):
            if name.startswith(prefix):
                info = os.stat(name, dir_fd=directory, follow_symlinks=False)
                require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid(), "ownership")
                shutil.rmtree(os.path.join(parent, name))
    finally:
        os.close(directory)


@contextlib.contextmanager
def authfile(directory, request, private):
    """Hold the original secret descriptor; never store bytes or their digest."""
    data = ceph_native.registry_material(request, private)
    path = os.path.join(directory, "auth.json")
    fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(fd, "w+b", closefd=False) as stream:
            stream.write(data)
            stream.flush()
            os.fsync(fd)
            info = os.fstat(fd)
            before = identity(info)
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600, "material")
            yield path
            require(identity(os.fstat(fd)) == before and identity(os.stat(path, follow_symlinks=False)) == before, "material")
            stream.seek(0)
            require(stream.read(len(data) + 1) == data, "material")
    finally:
        try:
            require(identity(os.stat(path, follow_symlinks=False)) == identity(os.fstat(fd)), "material")
            os.unlink(path)
        finally:
            os.close(fd)


def signature_policy():
    """Accept only the pinned repository; every other reference is rejected."""
    return canonical({"default": [{"type": "reject"}], "transports": {"docker": {ceph_native.PINNED_REPOSITORY: [{"type": "insecureAcceptAnything"}]}}})


@contextlib.contextmanager
def empty_mounts(directory):
    """Prevent Podman from copying ambient host subscription credentials."""
    path = os.path.join(directory, "mounts.conf")
    fd = os.open(path, os.O_RDONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o400)
    try:
        original = os.fstat(fd)
        fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
        expected = tuple(getattr(original, field) for field in fields)
        require(stat.S_ISREG(original.st_mode) and original.st_uid == os.geteuid() and
                stat.S_IMODE(original.st_mode) == 0o400 and original.st_nlink == 1 and original.st_size == 0)

        def prove():
            require(all(tuple(getattr(info, field) for field in fields) == expected
                        for info in (os.fstat(fd), os.stat(path, follow_symlinks=False))))

        prove()
        yield path, prove
        prove()
    finally:
        os.close(fd)


def crypto_policy(value):
    require(isinstance(value, str) and value.isascii() and len(value.encode()) <= 65536)
    try:
        policy = ceph_tunnel.crypto_policy(value)
    except Exception:
        raise Refusal("image-profile") from None
    require(isinstance(policy, str) and bool(policy))
    return policy


def decode_payload(encoded):
    require(isinstance(encoded, str) and 0 < len(encoded) <= (MAX_PAYLOAD + 2) // 3 * 4)
    try:
        data = base64.b64decode(encoded, validate=True)
    except (ValueError, TypeError):
        raise Refusal("image-profile") from None
    require(0 < len(data) <= MAX_PAYLOAD and base64.b64encode(data).decode() == encoded)
    require(hashlib.sha256(data).hexdigest() == CEPHADM_SHA256)
    return data


def validate_probe(value, user):
    require(isinstance(value, dict) and set(value) == {"version", "uid", "gid", "cephUID", "cephGID", "machine", "artifacts", "cephadm", "cryptoPolicy"})
    require(value["version"] == "ceph-host-image-v1" and value["machine"] == "x86_64")
    require(type(value["cephUID"]) is int and type(value["cephGID"]) is int and 0 < value["cephUID"] == value["cephGID"] < 65536)
    require(type(value["uid"]) is int and type(value["gid"]) is int and (value["uid"], value["gid"]) == tuple(user))
    require(isinstance(value["artifacts"], dict) and set(value["artifacts"]) == set(ARTIFACTS))
    for artifact in value["artifacts"].values():
        require(isinstance(artifact, dict) and set(artifact) == {"sha256", "bytes", "mode", "uid", "gid"})
        require(all(type(artifact[key]) is int for key in ("bytes", "mode", "uid", "gid")))
        require(0 < artifact["bytes"] <= 64 << 20 and artifact["uid"] == artifact["gid"] == 0)
        require(artifact["mode"] in (0o644, 0o755) and ceph_native.digest(artifact["sha256"]))
    payload = decode_payload(value["cephadm"])
    artifact = value["artifacts"]["/usr/sbin/cephadm"]
    require(artifact["sha256"] == CEPHADM_SHA256 and artifact["bytes"] == len(payload) and artifact["mode"] == 0o755)
    crypto_policy(value["cryptoPolicy"])
    return value


def inspect_image(image, command, reason="image-platform"):
    """A pull counts only once the exact digest and linux/amd64 are proved."""
    require(isinstance(image, str) and IMAGE.fullmatch(image) is not None and not image.endswith("0" * 64), reason)
    output = command(["/usr/bin/podman", "image", "inspect", image], 30, 256 << 10)
    try:
        value = json.loads(output, object_pairs_hook=ceph_native.unique_object)
    except (ValueError, TypeError, UnicodeError):
        raise Refusal(reason) from None
    require(isinstance(value, list) and len(value) == 1 and isinstance(value[0], dict), reason)
    item = value[0]
    require(item.get("Os") == "linux" and item.get("Architecture") == "amd64", reason)
    require(isinstance(item.get("Id"), str) and IDENTIFIER.fullmatch(item["Id"]) is not None, reason)
    require(isinstance(item.get("RepoDigests"), list) and image in item["RepoDigests"], reason)
    require(type(item.get("Size")) is int and 0 < item["Size"] <= 16 << 30, reason)
    return {"id": item["Id"], "size": item["Size"]}


def confinement(value, seconds):
    """Prove the probe's private namespaces and resource limits as created."""
    host, config = value.get("HostConfig"), value.get("Config")
    require(isinstance(host, dict) and isinstance(config, dict))
    require(config.get("OpenStdin") is False and config.get("Tty") is False)
    require(type(config.get("Timeout")) is int and config["Timeout"] == seconds)
    require(type(config.get("StopTimeout")) is int and config["StopTimeout"] == 1)
    require("EffectiveCaps" in value and "BoundingCaps" in value and value["EffectiveCaps"] in (None, []) and value["BoundingCaps"] in (None, []))
    require(isinstance(host.get("CapDrop"), list) and len(host["CapDrop"]) <= 64 and len(set(host["CapDrop"])) == len(host["CapDrop"]))
    require(all(isinstance(cap, str) and (cap == "ALL" or re.fullmatch(r"CAP_[A-Z0-9_]+", cap)) for cap in host["CapDrop"]))
    require(all(host.get(field) == "private" for field in ("PidMode", "IpcMode", "UTSMode")))
    cgroups = [host[field] for field in ("CgroupMode", "CgroupnsMode") if field in host]
    require(bool(cgroups) and all(mode == "private" for mode in cgroups))
    require(all(type(host.get(field)) is int and host[field] == expected for field, expected in
                (("Memory", 512 << 20), ("MemorySwap", 512 << 20), ("PidsLimit", 64), ("CpuPeriod", 100000), ("CpuQuota", 100000))))
    require(host.get("Devices") == [] and not host.get("VolumesFrom") and not host.get("GroupAdd") and not host.get("PortBindings"))
    tmpfs = host.get("Tmpfs")
    require(isinstance(tmpfs, dict) and set(tmpfs) == {"/tmp"} and isinstance(tmpfs["/tmp"], str) and len(tmpfs["/tmp"]) <= 1024)
    options = tmpfs["/tmp"].split(",")
    require(len(options) == len(set(options)))
    options = set(options)
    require(options <= {"rw", "noexec", "nosuid", "nodev", "size=16m", "size=16777216", "mode=1777", "mode=01777", "rprivate", "tmpcopyup"})
    require({"rw", "noexec", "nosuid", "nodev"} <= options)
    require(len(options & {"size=16m", "size=16777216"}) == len(options & {"mode=1777", "mode=01777"}) == 1)


def contained_argv(label, mounts_file, hooks, image_id, user, script, arguments=(), *, mounts=(), sysctls=()):
    """No network, no credential mounts, a closed environment, a read-only root, no capabilities, bounded resources."""
    argv = ["/usr/bin/podman", "create", "--pull=never", "--label", label, "--default-mounts-file=" + mounts_file,
            "--hooks-dir=" + hooks, "--network=none", "--read-only", "--cap-drop=all",
            "--read-only-tmpfs=false", "--health-cmd=none", "--image-volume=ignore", "--unsetenv-all",
            "--log-driver=none", "--restart=no", "--uts=private", "--hostname=" + PROBE_HOSTNAME, "--cgroupns=private",
            "--security-opt=no-new-privileges", "--pids-limit=64", "--memory=512m", "--memory-swap=512m",
            "--cpus=1", "--ipc=private", "--pid=private", "--user=" + str(user[0]) + ":" + str(user[1]),
            "--timeout=60", "--stop-timeout=1", "--workdir=/tmp",
            "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777"]
    argv += ["--env=" + value for value in sorted(PROBE_ENV)]
    argv += ["--sysctl=" + key + "=" + value for key, value in sysctls]
    argv += ["--volume=" + source + ":" + target + ":ro,z" for source, target in mounts]
    return argv + ["--entrypoint=/usr/bin/python3", image_id, "-I", "-B", "-c", script, *arguments]


class HostRuntime:
    """One host's preparation; the controller supplies only fixed phases."""

    def __init__(self, request, host, command=None, *, clock=time.monotonic, pause=time.sleep, markers=None, packages=None):
        self.request = ceph_native.validate(request)
        self.host = next((item for item in request["hosts"] if item["name"] == host), None)
        require(self.host is not None and request["action"] == "cluster", "request")
        self.run = command or ceph_native.bounded_command
        self.clock = clock
        self.pause = pause
        self.markers = markers
        self.packages = packages or ceph_native.Markers(request, root=PACKAGE_ROOT)

    def command(self, argv, timeout, limit, reason="native-command"):
        code, output = self.run(argv, timeout=timeout, limit=limit)
        require(code == 0, reason)
        return output

    def journal(self, create=False):
        return journal(self.request, self.host["name"], create=create, markers=self.markers)

    def scope(self):
        return scope(self.request, self.host["name"])

    def pull(self, durable, private, deadline):
        """Fill the content-addressed image cache with only the frozen digest."""
        image = self.request["image"]
        discard_residue(durable.path, "pull-")
        attempts = 0
        while attempts < PULL_ATTEMPTS and durable.read("pull-attempt-%d.json" % (attempts + 1)) is not None:
            record = durable.read("pull-attempt-%d.json" % (attempts + 1))
            require(canonical(record) == canonical({"scope": self.scope(), "attempt": attempts + 1}), "image-pull")
            attempts += 1
        completed = durable.read("pulled.json")
        if completed is not None:
            require(attempts > 0, "image-pull")
            current = inspect_image(image, self.command)
            require(canonical(completed) == canonical({"scope": self.scope(), "imageID": current["id"]}), "image-platform")
            return False
        if attempts:
            try:
                current = inspect_image(image, self.command)
            except Refusal:
                current = None
            if current is not None:
                durable.write("pulled.json", {"scope": self.scope(), "imageID": current["id"]})
                return True
        require(attempts < PULL_ATTEMPTS, "image-pull")
        with owned_scratch(durable.path, "pull-") as scratch, authfile(scratch, self.request, private) as auth:
            policy = os.path.join(scratch, "policy.json")
            ceph_native.private_write(policy, signature_policy())
            argv = ["/usr/bin/podman", "pull", "--signature-policy", policy, "--tls-verify=true", "--authfile", auth, image]
            while True:
                seconds = min(PULL_SECONDS, deadline - self.clock() - PULL_RESERVE)
                require(seconds >= PULL_MINIMUM, "image-pull")
                attempts += 1
                durable.write("pull-attempt-%d.json" % attempts, {"scope": self.scope(), "attempt": attempts})
                try:
                    self.command(argv, seconds, 256 << 10, "image-pull")
                    break
                except Refusal:
                    require(attempts < PULL_ATTEMPTS, "image-pull")
                    self.pause(PULL_PAUSE)
        current = inspect_image(image, self.command)
        durable.write("pulled.json", {"scope": self.scope(), "imageID": current["id"]})
        return True

    def prove_container(self, identifier, image_id, label, *, user, script, arguments=(), mounts=(), sysctls=()):
        output = self.command(["/usr/bin/podman", "container", "inspect", identifier], 20, 256 << 10)
        try:
            value = json.loads(output, object_pairs_hook=ceph_native.unique_object)
        except (ValueError, TypeError, UnicodeError):
            raise Refusal("image-profile") from None
        require(isinstance(value, list) and len(value) == 1 and isinstance(value[0], dict))
        item = value[0]
        key, expected = label.split("=", 1)
        require(item.get("Id") == identifier and item.get("Image") == image_id)
        require(isinstance(item.get("Config"), dict) and isinstance(item["Config"].get("Labels"), dict))
        require(item["Config"]["Labels"].get(key) == expected)
        config, host = item["Config"], item.get("HostConfig")
        require(isinstance(host, dict))
        require(config.get("User") == str(user[0]) + ":" + str(user[1]))
        require(config.get("Entrypoint") == ["/usr/bin/python3"] and config.get("Cmd") == ["-I", "-B", "-c", script, *arguments])
        require(config.get("WorkingDir") == "/tmp" and config.get("Hostname") == PROBE_HOSTNAME)
        require(isinstance(config.get("Env"), list) and len(config["Env"]) == len(PROBE_ENV) and set(config["Env"]) == PROBE_ENV)
        require(host.get("ReadonlyRootfs") is True and host.get("Privileged") is False and not host.get("CapAdd"))
        require(host.get("NetworkMode") == "none" and host.get("SecurityOpt") == ["no-new-privileges"])
        # Podman's inspect reports no sysctls, so the created command must name exactly the requested ones.
        created = config.get("CreateCommand")
        require(isinstance(created, list) and all(isinstance(argument, str) for argument in created))
        require([argument for argument in created if argument.startswith("--sysctl")] == ["--sysctl=" + name + "=" + setting for name, setting in sysctls])
        require(host.get("Sysctls") in (None, {}, dict(sysctls)))
        observed = item.get("Mounts")
        require(isinstance(observed, list) and len(observed) == len(mounts))
        if not mounts:
            require(not host.get("Binds"))
        else:
            require(len(mounts) in (1, 2) and len(set(mounts)) == len(mounts))
            # Podman drops z from the recorded options once the start has relabelled the source.
            state = item.get("State")
            required = {"ro"} if isinstance(state, dict) and state.get("Status") in ("running", "exited", "stopped") else {"ro", "z"}
            binds = host.get("Binds")
            require(isinstance(binds, list) and len(binds) == len(mounts) and all(isinstance(bind, str) for bind in binds))
            for bind, (source, destination) in zip(sorted(binds), sorted(mounts)):
                prefix = source + ":" + destination + ":"
                require(bind.startswith(prefix))
                options = bind[len(prefix):].split(",")
                require(len(options) == len(set(options)) and required <= set(options) <= BIND_OPTIONS)
            require(all(isinstance(mount, dict) and mount.get("Type") == "bind" and mount.get("RW") is False for mount in observed))
            require(sorted((mount.get("Source"), mount.get("Destination")) for mount in observed) == sorted(mounts))
        confinement(item, 60)

    def remove_container(self, identifier, image_id, label, **proof):
        """Our own created identifier is removed and proved absent even when its proof refused; the refusal is kept."""
        require(IDENTIFIER.fullmatch(identifier) is not None)
        refused = None
        try:
            self.prove_container(identifier, image_id, label, **proof)
        except Refusal as error:
            refused = error
        self.command(["/usr/bin/podman", "container", "rm", "--force", "--time=1", identifier], 20, 4096)
        absent = self.command(["/usr/bin/podman", "container", "ls", "--all", "--no-trunc", "--filter=id=" + identifier, "--format={{.ID}}"], 20, 4096)
        require(absent == b"")
        if refused is not None:
            raise refused

    def contained(self, image_id, user, script, arguments=(), *, mounts=(), sysctls=(), proof=lambda: None, limit=MAX_PROBE):
        """Run one fixed script in the frozen image and remove its container."""
        identifier = None
        label = "bootwright.ceph.host.probe=" + uuid.uuid4().hex
        details = {"user": user, "script": script, "arguments": tuple(arguments), "mounts": tuple(mounts), "sysctls": tuple(sysctls)}
        with tempfile.TemporaryDirectory(prefix="bootwright-host-probe-") as directory, empty_mounts(directory) as (mounts_file, prove_mounts):
            Path(directory, "hooks").mkdir(mode=0o700)
            try:
                argv = contained_argv(label, mounts_file, directory + "/hooks", image_id, user, script, arguments, mounts=mounts, sysctls=sysctls)
                proof()
                prove_mounts()
                identifier = self.command(argv, 30, 1024).decode("ascii").strip()
                prove_mounts()
                require(IDENTIFIER.fullmatch(identifier) is not None)
                self.prove_container(identifier, image_id, label, **details)
                proof()
                prove_mounts()
                output = self.command(["/usr/bin/podman", "start", "--attach", identifier], 60, limit)
                prove_mounts()
                proof()
                require(0 < len(output) <= limit)
                try:
                    return json.loads(output, object_pairs_hook=ceph_native.unique_object)
                except (ValueError, TypeError, UnicodeError):
                    raise Refusal("image-profile") from None
            finally:
                if identifier is not None:
                    self.remove_container(identifier, image_id, label, **details)

    def label(self, image_id, user, parent):
        """Prove the shared read-only label through the image's own ceph account."""
        require(self.command(["/usr/sbin/getenforce"], 10, 128).strip() == b"Enforcing")
        with owned_scratch(parent, "label-") as scratch:
            shared = os.path.join(scratch, "shared")
            os.mkdir(shared, 0o755)
            path = os.path.join(shared, "proof")
            ceph_native.private_write(path, SENTINEL)
            os.chmod(path, 0o644)
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                before = os.fstat(fd)
                observed = self.contained(image_id, user, LABEL_PROBE, mounts=((shared, "/run/bootwright-label"),), limit=4096)
                require(isinstance(observed, dict) and set(observed) == {"read", "readOnly", "uid", "gid"})
                require(observed["read"] is True and observed["readOnly"] is True and type(observed["uid"]) is int and type(observed["gid"]) is int)
                require((observed["uid"], observed["gid"]) == tuple(user))
                fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns")
                final = os.fstat(fd)
                require(tuple(getattr(before, key) for key in fields) == tuple(getattr(final, key) for key in fields))
                require(identity(final) == identity(os.stat(path, follow_symlinks=False)))
                require(os.getxattr(fd, "security.selinux").rstrip(b"\x00") == SHARED_LABEL.encode())
                os.lseek(fd, 0, os.SEEK_SET)
                require(os.read(fd, len(SENTINEL) + 1) == SENTINEL)
                return SHARED_LABEL
            finally:
                os.close(fd)

    def facts(self, image_id):
        """Two contained probes, as root and as the image's ceph account, must agree."""
        probes = []
        for user in ((0, 0), None):
            if user is None:
                user = probes[0]["cephUID"], probes[0]["cephGID"]
            probes.append(validate_probe(self.contained(image_id, user, PROBE), user))
        require({key: value for key, value in probes[0].items() if key not in ("uid", "gid")} ==
                {key: value for key, value in probes[1].items() if key not in ("uid", "gid")})
        return probes[0]

    def profile(self, private):
        """Pull the frozen digest, then prove its runtime facts in contained probes."""
        deadline = self.clock() + PROFILE_CALL_SECONDS
        require(isinstance(private, dict) and set(private) == {"registryJSON"}, "material")
        with self.journal(create=True) as durable:
            changed = self.pull(durable, private["registryJSON"], deadline)
            original = inspect_image(self.request["image"], self.command)
            require(canonical(durable.read("pulled.json")) == canonical({"scope": self.scope(), "imageID": original["id"]}), "image-platform")
            existing = durable.read(PROFILE)
            if existing is not None:
                self.current(durable)
                cephadm_payload(durable)
                return {"changed": changed}
            installed = installed_cephadm(self.request)
            probe_result = self.facts(original["id"])
            require(inspect_image(self.request["image"], self.command) == original, "image-platform")
            user = (probe_result["cephUID"], probe_result["cephGID"])
            label = self.label(original["id"], user, durable.path)
            require(inspect_image(self.request["image"], self.command) == original, "image-platform")
            payload = decode_payload(probe_result["cephadm"])
            require(hashlib.sha256(payload).hexdigest() == installed == CEPHADM_SHA256)
            write_payload(durable, payload)
            durable.write(PROFILE, {"version": PROFILE_VERSION, "scope": self.scope(), "image": self.request["image"], "host": self.host["name"],
                                    "imageID": original["id"], "uid": user[0], "gid": user[1], "policy": crypto_policy(probe_result["cryptoPolicy"]),
                                    "label": label, "cephadmSHA256": CEPHADM_SHA256,
                                    "artifacts": {path: probe_result["artifacts"][path]["sha256"] for path in ARTIFACTS}})
            self.current(durable)
            return {"changed": True}

    def current(self, durable):
        """Re-prove the retained profile against the image the host holds now."""
        return current_profile(self.request, self.host["name"], durable, self.command)

    def route_record(self, profile):
        return {"version": ROUTE_VERSION, "scope": self.scope(), "imageID": profile["imageID"], "sources": dict(SOURCES),
                "cases": {name: dict(zip(probe.COUNTERS, values)) for name, values in EXPECTED_CASES.items()}}

    def route(self):
        with self.journal() as durable:
            profile = self.current(durable)
            expected = self.route_record(profile)
            existing = durable.read(ROUTE)
            if existing is not None:
                require(canonical(existing) == canonical(expected), "route-probe")
                return {"changed": False}
            require(all(profile["artifacts"].get(path) == digest for path, digest in SOURCES.items()), "route-probe")
            user = (profile["uid"], profile["gid"])
            prepared_keys = self.contained(profile["imageID"], user, module_source(probe), ("prepare",), limit=probe.MAX_OUTPUT)
            require(isinstance(prepared_keys, dict) and set(prepared_keys) == {"schema", "ok", "synthetic"}, "route-probe")
            require(prepared_keys["schema"] == probe.PREPARE_SCHEMA and prepared_keys["ok"] is True, "route-probe")
            try:
                synthetic = probe.validate_synthetic(prepared_keys["synthetic"], user[0], user[1])
            except probe.Refusal:
                raise Refusal("route-probe") from None
            with tempfile.TemporaryDirectory(prefix="bootwright-route-probe-") as directory:
                with staged(directory, synthetic, user[0], user[1]) as (mounts, prove):
                    observed = self.contained(profile["imageID"], user, module_source(probe), ("exercise",), mounts=mounts,
                                              sysctls=(("net.ipv4.ip_unprivileged_port_start", "0"),), proof=prove, limit=probe.MAX_OUTPUT)
            cases = route_cases(observed)
            require(inspect_image(self.request["image"], self.command)["id"] == profile["imageID"], "image-platform")
            require(canonical(dict(expected, cases=cases)) == canonical(expected), "route-probe")
            durable.write(ROUTE, expected)
            return {"changed": True}

    def block_device(self, device):
        """Bind the declared path to one nonrotational disk, its device number, serial and size."""
        actual = os.path.realpath(device["path"])
        output = self.command(["/usr/bin/lsblk", "--json", "--bytes", "--paths", "--nodeps", "--output", "PATH,TYPE,SERIAL,SIZE,ROTA,MAJ:MIN", actual],
                              30, 65536, "device-identity")
        try:
            observed = json.loads(output, object_pairs_hook=ceph_native.unique_object)
        except (ValueError, TypeError, UnicodeError):
            raise Refusal("device-identity") from None
        require(isinstance(observed, dict) and isinstance(observed.get("blockdevices"), list) and len(observed["blockdevices"]) == 1, "device-identity")
        item = observed["blockdevices"][0]
        require(isinstance(item, dict) and item.get("path") == actual and item.get("type") == "disk", "device-identity")
        pair = item.get("maj:min")
        require(isinstance(pair, str) and re.fullmatch(r"[0-9]+:[0-9]+", pair) is not None, "device-identity")
        rotational = item.get("rota")
        require(type(rotational) in (bool, int) and rotational in (False, 0), "device-identity")
        require(item.get("serial") == device["serial"] and type(item.get("size")) is int and item["size"] == device["sizeBytes"], "device-identity")
        major, minor = (int(value) for value in pair.split(":"))
        require(major > 0 and minor >= 0, "device-identity")
        return {"path": device["path"], "serial": device["serial"], "sizeBytes": device["sizeBytes"], "rotational": 0, "major": major, "minor": minor}

    def udev(self, observed):
        raw = self.command(["/usr/bin/udevadm", "info", "--query=property", "--name=" + observed["path"]], 30, 128 << 10, "device-identity")
        return properties(raw, observed)

    def device_identity(self):
        with self.journal() as durable:
            profile = self.current(durable)
            existing = durable.read(DEVICES)
            rows = []
            for device in self.host["devices"]:
                observed = self.block_device(device)
                environment = self.udev(observed)
                if existing is None:
                    result = self.contained(profile["imageID"], (profile["uid"], profile["gid"]), DEVICE_PROBE,
                                            (canonical({"path": observed["path"], "environment": environment}).decode("ascii"),), limit=4096)
                    require(isinstance(result, dict) and set(result) == {"version", "deviceID", "dependencyInput"}, "device-identity")
                    require(result["version"] == "ceph-host-device-id-v1" and result["dependencyInput"] == "ceph_volume.util.disk.UdevData.environment",
                            "device-identity")
                    require(result["deviceID"] == native_form(environment), "device-identity")
                    require(canonical(self.block_device(device)) == canonical(observed) and self.udev(observed) == environment, "device-identity")
                    identifier = result["deviceID"]
                else:
                    identifier = native_form(environment)
                value = {"host": self.host["name"], "path": device["path"], "serial": device["serial"], "deviceID": identifier}
                rows.append(row(value, self.request, self.host["name"]))
            record = {"version": DEVICES_VERSION, "scope": self.scope(), "imageID": profile["imageID"], "devices": rows}
            if existing is not None:
                require(canonical(existing) == canonical(record), "device-identity")
                return {"changed": False}
            require(inspect_image(self.request["image"], self.command)["id"] == profile["imageID"], "image-platform")
            durable.write(DEVICES, record)
            return {"changed": True}

    def host_devices(self):
        """Read-only: this host's retained identities for the cluster's OSD checks."""
        durable = prepared(self.request, self.host["name"], markers=self.markers)
        require(durable is not None, "device-identity")
        return {"devices": retained_devices(self.request, self.host["name"], durable)}

    def host_observe(self):
        """Read-only evidence; absence is proved only from the absent owner record."""
        evidence = {"version": EVIDENCE_VERSION, "host": self.host["name"], "owner": self.request["owner"], "fsid": self.request["fsid"],
                    "image": self.request["image"], "ready": False, "removed": False, "imageID": "", "uid": 0, "gid": 0, "label": "",
                    "policySHA256": "", "cephadmSHA256": "", "planDigest": "", "route": False, "devices": []}
        if self.packages.owned():
            complete = self.packages.read("complete.json")
            if complete is not None:
                require(isinstance(complete, dict) and ceph_native.digest(complete.get("planDigest")), "evidence")
                evidence["planDigest"] = complete["planDigest"]
        durable = prepared(self.request, self.host["name"], markers=self.markers)
        if durable is None:
            return evidence
        if durable.read(PROFILE) is not None:
            profile = current_profile(self.request, self.host["name"], durable, self.command)
            evidence.update(imageID=profile["imageID"], uid=profile["uid"], gid=profile["gid"], label=profile["label"],
                            policySHA256=hashlib.sha256(profile["policy"].encode()).hexdigest(), cephadmSHA256=profile["cephadmSHA256"])
            route = durable.read(ROUTE)
            if route is not None:
                require(canonical(route) == canonical(self.route_record(profile)), "route-probe")
                evidence["route"] = True
            identified = durable.read(DEVICES) is not None
            if identified:
                evidence["devices"] = [{key: value[key] for key in ("path", "serial", "deviceID")}
                                       for value in retained_devices(self.request, self.host["name"], durable, profile["imageID"])]
            evidence["ready"] = bool(evidence["planDigest"] and evidence["route"] and identified)
        return evidence


def installed_cephadm(request):
    """The digest of the host's cephadm, which the signed package transaction installed and verified."""
    return hashlib.sha256(ceph_native.read_regular(request["cephadm"]["path"], maximum=16 << 20)).hexdigest()


def write_payload(durable, payload):
    """Publish the public cephadm bytes once, root-only, without replacing a name."""
    path = os.path.join(durable.path, PAYLOAD)
    if os.path.lexists(path):
        require(ceph_native.read_regular(path, maximum=MAX_PAYLOAD, private=True) == payload, "ownership")
        return
    directory = os.open(durable.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        stage = ".payload-" + uuid.uuid4().hex
        descriptor = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
        try:
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(payload)
                handle.flush()
                os.fsync(handle.fileno())
            os.link(stage, PAYLOAD, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
            os.fsync(directory)
        finally:
            os.unlink(stage, dir_fd=directory)
    finally:
        os.close(directory)


def current_profile(request, host, durable, command):
    """The retained runtime profile, refused when missing or contradicting the held image."""
    record = durable.read(PROFILE)
    fields = {"version", "scope", "image", "host", "imageID", "uid", "gid", "policy", "label", "cephadmSHA256", "artifacts"}
    require(isinstance(record, dict) and set(record) == fields, "runtime-profile")
    require(record["version"] == PROFILE_VERSION and canonical(record["scope"]) == canonical(scope(request, host)), "runtime-profile")
    require(record["image"] == request["image"] and record["host"] == host, "runtime-profile")
    require(isinstance(record["imageID"], str) and IDENTIFIER.fullmatch(record["imageID"]) is not None, "runtime-profile")
    require(type(record["uid"]) is int and type(record["gid"]) is int and 0 < record["uid"] == record["gid"] < 65536, "runtime-profile")
    require(record["label"] == SHARED_LABEL and record["cephadmSHA256"] == CEPHADM_SHA256, "runtime-profile")
    require(isinstance(record["policy"], str) and bool(record["policy"]), "runtime-profile")
    try:
        require(ceph_tunnel.crypto_policy(record["policy"]) == record["policy"], "runtime-profile")
    except ceph_native.Refusal:
        raise
    except Exception:
        raise Refusal("runtime-profile") from None
    require(isinstance(record["artifacts"], dict) and set(record["artifacts"]) == set(ARTIFACTS)
            and all(ceph_native.digest(value) for value in record["artifacts"].values()), "runtime-profile")
    require(record["artifacts"]["/usr/sbin/cephadm"] == CEPHADM_SHA256, "runtime-profile")
    require(inspect_image(request["image"], command, "runtime-profile")["id"] == record["imageID"], "runtime-profile")
    return record


def cephadm_payload(durable):
    """The retained public bytes, refused unless they are the authenticated payload."""
    data = ceph_native.read_regular(os.path.join(durable.path, PAYLOAD), maximum=MAX_PAYLOAD, private=True)
    require(hashlib.sha256(data).hexdigest() == CEPHADM_SHA256, "runtime-profile")
    return data


def route_cases(value):
    require(isinstance(value, dict) and set(value) == {"schema", "ok", "scope", "dependencyStubs", "cases"}, "route-probe")
    require(value["schema"] == probe.SCHEMA and value["ok"] is True, "route-probe")
    require(value["scope"] == "native-ssh-class-loopback" and value["dependencyStubs"] == probe.STUBS, "route-probe")
    cases = value["cases"]
    require(isinstance(cases, dict) and set(cases) == set(EXPECTED_CASES), "route-probe")
    for name, expected in EXPECTED_CASES.items():
        observed = cases[name]
        require(isinstance(observed, dict) and set(observed) == set(probe.COUNTERS), "route-probe")
        require(all(type(observed[field]) is int for field in probe.COUNTERS), "route-probe")
        require(tuple(observed[field] for field in probe.COUNTERS) == expected, "route-probe")
    return json.loads(canonical(cases))


@contextlib.contextmanager
def staged(directory, synthetic, uid, gid):
    """Retain the synthetic bytes outside the read-only mounted children."""
    request = {"version": "ceph-core-v4", "owner": "route-probe", "fsid": "11111111-1111-4111-8111-111111111111",
               "clusterSSHTransport": "verified-tunnel", "clusterSSHUser": probe.USER,
               "hosts": [{"name": probe.HOST, "address": probe.ADDRESS, "port": probe.OUTER_PORT}]}
    known = ("[" + probe.ADDRESS + "]:" + str(probe.OUTER_PORT) + " " + synthetic["keys"]["outer"]["public"] + "\n").encode()
    files = ceph_tunnel.bundle(request, synthetic["keys"]["client"]["private"].encode(), known,
                               module_source(ceph_tunnel).encode(), synthetic["cryptoPolicy"], public=synthetic["keys"]["client"]["public"])
    contents = {"bundle": files, "synthetic": {"synthetic.json": canonical(synthetic)}}
    with contextlib.ExitStack() as stack:
        retained, directories = [], []
        root = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        stack.callback(os.close, root)
        require(os.fstat(root).st_uid == os.geteuid() and stat.S_IMODE(os.fstat(root).st_mode) == 0o700, "route-probe")
        for name, values in contents.items():
            path = os.path.join(directory, name)
            os.mkdir(path, 0o700)
            os.chown(path, uid, gid)
            for filename, data in values.items():
                target = os.path.join(path, filename)
                descriptor = os.open(target, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
                stack.callback(os.close, descriptor)
                os.fchown(descriptor, uid, gid)
                with os.fdopen(descriptor, "w+b", closefd=False) as stream:
                    stream.write(data)
                    stream.flush()
                    os.fsync(descriptor)
                retained.append((target, descriptor, os.fstat(descriptor), data))
            descriptor = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
            stack.callback(os.close, descriptor)
            directories.append((name, descriptor, os.fstat(descriptor), sorted(values)))
        root_info = os.fstat(root)
        fields = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns")

        def same(left, right):
            return all(getattr(left, field) == getattr(right, field) for field in fields)

        def prove():
            require(same(root_info, os.fstat(root)) and same(root_info, os.stat(directory, follow_symlinks=False)), "route-probe")
            require(custody.listed(root) == sorted(contents), "route-probe")
            for name, descriptor, original, names in directories:
                require(same(original, os.fstat(descriptor)) and same(original, os.stat(name, dir_fd=root, follow_symlinks=False)), "route-probe")
                require(stat.S_ISDIR(original.st_mode) and original.st_uid == uid and original.st_gid == gid and stat.S_IMODE(original.st_mode) == 0o700,
                        "route-probe")
                require(custody.listed(descriptor) == names, "route-probe")
            for path, descriptor, original, data in retained:
                expected = tuple(getattr(original, field) for field in fields)
                require(all(tuple(getattr(info, field) for field in fields) == expected
                            for info in (os.fstat(descriptor), os.stat(path, follow_symlinks=False))), "route-probe")
                require(stat.S_ISREG(original.st_mode) and original.st_nlink == 1 and stat.S_IMODE(original.st_mode) == 0o600, "route-probe")
                os.lseek(descriptor, 0, os.SEEK_SET)
                require(os.read(descriptor, len(data) + 1) == data, "route-probe")

        prove()
        yield ((os.path.join(directory, "bundle"), ceph_tunnel.CONTAINER_ROOT),
               (os.path.join(directory, "synthetic"), probe.SYNTHETIC_ROOT)), prove
        prove()


def properties(raw, device):
    """Only the identity fields udev binds to the proved device number."""
    require(isinstance(raw, bytes) and 0 < len(raw) <= 128 << 10, "device-identity")
    values, numbers = {}, {}
    try:
        lines = raw.decode("ascii").splitlines()
    except UnicodeError:
        raise Refusal("device-identity") from None
    for line in lines:
        require("=" in line and len(line) <= 4096, "device-identity")
        name, value = line.split("=", 1)
        require(re.fullmatch(r"[A-Z0-9_]{1,128}", name) is not None, "device-identity")
        if name in ("MAJOR", "MINOR"):
            require(name not in numbers and re.fullmatch(r"[0-9]{1,10}", value) is not None, "device-identity")
            numbers[name] = int(value)
            continue
        if name not in UDEV_FIELDS:
            continue
        require(name not in values and 0 < len(value) <= 512 and all(" " <= character <= "~" for character in value), "device-identity")
        values[name] = value
    require(numbers == {"MAJOR": device["major"], "MINOR": device["minor"]}, "device-identity")
    selected = "ID_SCSI_SERIAL" if "ID_VENDOR" in values and "ID_SCSI_SERIAL" in values else "ID_SERIAL_SHORT" if "ID_SERIAL_SHORT" in values else "ID_SERIAL"
    require(values.get(selected) == device["serial"], "device-identity")
    native_form(values)
    return values


def native_form(values):
    """The provider's device ID spelling, admitted only where both provider algorithms agree."""
    require(isinstance(values, dict) and set(values) <= UDEV_FIELDS, "device-identity")
    require(not values.get("ID_MODEL", "").startswith("LVM"), "device-identity")
    if "ID_VENDOR" in values and "ID_MODEL" in values and "ID_SCSI_SERIAL" in values:
        parts = [values["ID_VENDOR"], values["ID_MODEL"], values["ID_SCSI_SERIAL"]]
    elif "ID_MODEL" in values and "ID_SERIAL_SHORT" in values:
        parts = [values["ID_MODEL"], values["ID_SERIAL_SHORT"]]
    else:
        require("ID_SERIAL" in values and not values["ID_SERIAL"].startswith("MTFD"), "device-identity")
        parts = [values["ID_SERIAL"]]
    require(all(parts), "device-identity")
    result = "_".join(parts).replace(" ", "_")
    require("__" not in result and DEVICE_ID.fullmatch(result) is not None, "device-identity")
    return result


def row(value, request, host):
    require(isinstance(value, dict) and set(value) == {"host", "path", "serial", "deviceID"}, "device-identity")
    wanted = [item for item in request["hosts"] if item["name"] == host]
    require(len(wanted) == 1, "device-identity")
    devices = [device for device in wanted[0]["devices"] if device["path"] == value["path"]]
    require(value["host"] == host and len(devices) == 1 and value["serial"] == devices[0]["serial"], "device-identity")
    require(isinstance(value["deviceID"], str) and DEVICE_ID.fullmatch(value["deviceID"]) is not None, "device-identity")
    return json.loads(canonical(value))


def rows(request, host, values):
    """One row per declared device, in declared order, and nothing else."""
    wanted = [item for item in request["hosts"] if item["name"] == host]
    require(len(wanted) == 1 and isinstance(values, list) and len(values) == len(wanted[0]["devices"]), "device-identity")
    result = [row(value, request, host) for value in values]
    require([value["path"] for value in result] == [device["path"] for device in wanted[0]["devices"]], "device-identity")
    return result


def retained_devices(request, host, durable, image_id=None):
    record = durable.read(DEVICES)
    require(isinstance(record, dict) and set(record) == {"version", "scope", "imageID", "devices"}, "device-identity")
    require(record["version"] == DEVICES_VERSION and canonical(record["scope"]) == canonical(scope(request, host)), "device-identity")
    require(isinstance(record["imageID"], str) and IDENTIFIER.fullmatch(record["imageID"]) is not None, "device-identity")
    require(image_id is None or record["imageID"] == image_id, "device-identity")
    return rows(request, host, record["devices"])


CACHE_FIELDS = frozenset(("dev", "inode", "uid", "gid", "mode", "size", "mtimeNS"))


def cache_require(condition):
    ceph_native.require(condition, "cache-unproved")


def cache_metadata(info, ready=False):
    result = {"dev": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
              "mode": stat.S_IMODE(info.st_mode), "size": info.st_size, "mtimeNS": info.st_mtime_ns}
    if ready:
        result["ctimeNS"] = info.st_ctime_ns
    return result


def cache_descriptor(value, ready=False):
    cache_require(isinstance(value, dict) and set(value) == CACHE_FIELDS | ({"ctimeNS"} if ready else set()))
    cache_require(all(type(item) is int for item in value.values()))
    cache_require(0 <= value["dev"] < 1 << 64 and 0 < value["inode"] < 1 << 64)
    cache_require(value["uid"] == value["gid"] == 0 and value["mode"] == 0o744 and 0 < value["size"] <= MAX_PAYLOAD)
    cache_require(0 <= value["mtimeNS"] < 1 << 63 and (not ready or 0 <= value["ctimeNS"] < 1 << 63))


def cache_exists(directory, name):
    try:
        os.stat(name, dir_fd=directory, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


class Cache:
    """Publish the exact authenticated cephadm bytes before enrollment effects.

    Matching foreign bytes never confer custody. Publication reserves its whole
    stage inode before exposing the provider's fixed name and holds that exact
    inode before every execution-capable consumer.
    """

    def __init__(self, worker):
        self.worker = worker
        self.request = worker.request
        self.host = worker.host
        self.markers = worker.markers
        durable = worker.host_preparation()
        cache_require(durable is not None)
        self.profile = current_profile(self.request, self.host["name"], durable, lambda argv, timeout, limit: worker.command(argv, timeout=timeout,
                                                                                                                             json_result=False))
        self.payload = cephadm_payload(durable)

    def scope(self):
        cache_require(self.markers.owned())
        digest = hashlib.sha256(self.payload).hexdigest()
        cache_require(digest == self.profile["cephadmSHA256"])
        return {"version": "ceph-host-cache-v1", "owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"],
                "image": self.request["image"], "name": "cephadm." + digest, "sha256": digest, "bytes": len(self.payload)}

    def directory_scope(self):
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"], "uid": self.profile["uid"]}

    def prepare_directory(self, root):
        expected = self.directory_scope()
        if self.markers.read("cache-directory.json") is not None:
            return
        name = self.request["fsid"]
        if self.host["name"] == self.request["bootstrap"]:
            self.worker.prove_mounted_seed(self.worker.daemons())
        elif not cache_exists(root, name) or self.markers.read("cache-directory-intent.json") is None:
            cache_require(not cache_exists(root, name))
            self.markers.write("cache-directory-intent.json", expected)
            os.mkdir(name, 0o700, dir_fd=root)
            os.fsync(root)
        with observe.child_directory(root, name, (observe.ROOT_UID, self.profile["uid"])) as directory:
            info = os.fstat(directory)
            self.markers.write("cache-directory.json", {"scope": expected, "identity": {"dev": info.st_dev, "inode": info.st_ino}})

    @contextlib.contextmanager
    def directory(self, create=False):
        # The wrapped contexts close their descriptors on any exit; the proof after the yield is deliberately success-only.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with observe.root_directory(self.profile["uid"]) as root:
            cache_require(root is not None)
            if create:
                self.prepare_directory(root)
            cache_require(observe.entries(root) == [self.request["fsid"]])
            # pylint: disable-next=contextmanager-generator-missing-cleanup
            with observe.child_directory(root, self.request["fsid"], (observe.ROOT_UID, self.profile["uid"])) as cluster:
                info = os.fstat(cluster)
                record = self.markers.read("cache-directory.json")
                cache_require(canonical(record) == canonical({"scope": self.directory_scope(), "identity": {"dev": info.st_dev, "inode": info.st_ino}}))
                cache_require(stat.S_IMODE(info.st_mode) == 0o700)
                yield cluster

    @contextlib.contextmanager
    def held(self, directory, name, record, *, ready=False, links=(1,), retire=False):
        cache_descriptor(record, ready=ready)
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(descriptor, "rb") as source:
            info = os.fstat(source.fileno())
            cache_require(stat.S_ISREG(info.st_mode) and info.st_nlink in links and cache_metadata(info, ready) == record)
            body = source.read(MAX_PAYLOAD + 1)
            cache_require(body == self.payload and len(body) == record["size"])
            before = cache_metadata(info, ready)
            cache_require(cache_metadata(os.fstat(source.fileno()), ready) == before)
            cache_require(cache_metadata(os.stat(name, dir_fd=directory, follow_symlinks=False), ready) == before)
            yield source
            cache_require(cache_metadata(os.fstat(source.fileno()), ready) == before)
            if retire:
                cache_require(not cache_exists(directory, name))
            else:
                cache_require(cache_metadata(os.stat(name, dir_fd=directory, follow_symlinks=False), ready) == before)

    def record(self, value, expected, ready=False):
        cache_require(isinstance(value, dict) and set(value) == {"scope", "stage", "metadata"})
        cache_require(canonical(value["scope"]) == canonical(expected))
        stage = value["stage"]
        cache_require(isinstance(stage, str) and re.fullmatch(r"\.bootwright-cephadm-[0-9a-f]{32}", stage) is not None)
        cache_descriptor(value["metadata"], ready=ready)

    def reserve(self, directory, expected):
        cache_require(not cache_exists(directory, expected["name"]))
        stage = ".bootwright-cephadm-" + uuid.uuid4().hex
        descriptor = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o744, dir_fd=directory)
        published, reserved = False, None
        try:
            with os.fdopen(descriptor, "wb") as source:
                os.fchmod(source.fileno(), 0o744)
                source.write(self.payload)
                source.flush()
                os.fsync(source.fileno())
                info = os.fstat(source.fileno())
                cache_require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1)
                reserved = {"scope": expected, "stage": stage, "metadata": cache_metadata(info)}
                self.record(reserved, expected)
                os.fsync(directory)
                self.markers.write("cache-pending.json", reserved)
                published = True
                return reserved
        finally:
            if not published:
                retained = self.markers.read("cache-pending.json")
                if reserved is not None and canonical(retained) == canonical(reserved):
                    cache_require(cache_metadata(os.stat(stage, dir_fd=directory, follow_symlinks=False)) == reserved["metadata"])
                else:
                    os.unlink(stage, dir_fd=directory)
                    os.fsync(directory)

    def publish(self, directory, pending):
        name, stage = pending["scope"]["name"], pending["stage"]
        stage_exists, final_exists = cache_exists(directory, stage), cache_exists(directory, name)
        cache_require(stage_exists or final_exists)
        if not final_exists:
            with self.held(directory, stage, pending["metadata"]):
                os.link(stage, name, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
                os.fsync(directory)
        with self.held(directory, name, pending["metadata"], links=(1, 2)):
            if stage_exists:
                with self.held(directory, stage, pending["metadata"], links=(2,), retire=True):
                    os.unlink(stage, dir_fd=directory)
                    os.fsync(directory)
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        try:
            info = os.fstat(descriptor)
            cache_require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and cache_metadata(info) == pending["metadata"])
            ready = dict(pending, metadata=cache_metadata(info, ready=True))
            self.markers.write("cache-ready.json", ready)
            return ready
        finally:
            os.close(descriptor)

    def prepare(self):
        expected = self.scope()
        pending, ready = self.markers.read("cache-pending.json"), self.markers.read("cache-ready.json")
        with self.directory(create=True) as directory:
            if ready is not None:
                self.record(ready, expected, ready=True)
                cache_require(pending is not None)
                self.record(pending, expected)
                cache_require(ready["stage"] == pending["stage"] and {key: ready["metadata"][key] for key in CACHE_FIELDS} == pending["metadata"])
                cache_require(not cache_exists(directory, ready["stage"]))
                with self.held(directory, expected["name"], ready["metadata"], ready=True):
                    return False
            if pending is None:
                pending = self.reserve(directory, expected)
            else:
                self.record(pending, expected)
            self.publish(directory, pending)
            return True

    @contextlib.contextmanager
    def execution_proof(self):
        expected = self.scope()
        ready = self.markers.read("cache-ready.json")
        self.record(ready, expected, ready=True)
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with self.directory() as directory:
            cache_require(not cache_exists(directory, ready["stage"]))
            with self.held(directory, expected["name"], ready["metadata"], ready=True):
                yield


def dispatch(envelope):
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private"}, "request")
    phase = envelope["phase"]
    require(phase in PHASES, "request")
    runtime = HostRuntime(envelope["request"], envelope["host"])
    if phase != "profile":
        require(envelope["private"] == {}, "material")
    if phase in ("profile", "route", "device_identity"):
        require(ceph_native.supported_transport(runtime.request), "unsupported-release")
    if phase == "profile":
        return runtime.profile(envelope["private"])
    return getattr(runtime, phase)()
