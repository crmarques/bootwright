"""Fixed Ceph worker, sent over the authenticated SSH channel as source.

This file deliberately imports only the target Python standard library. Native
output is private until projected into evidence; exceptions never carry command
arguments, output, credentials or remote input. A successful process is not a
readiness or ownership proof.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import base64
import datetime
import errno
import hashlib
import hmac
import importlib
import inspect
import ipaddress
import json
import os
import pwd
import re
import secrets
import selectors
import signal
import stat
import string
import subprocess
import sys
import tempfile
import time
import uuid

MAX_INPUT = 2 * 1024 * 1024
MAX_OUTPUT = 1024 * 1024
COMMAND_SESSION = True
ROOT = "/var/lib/bootwright/ceph"
PACKAGE_QUERY = "%{EPOCHNUM} %{VERSION} %{RELEASE} %{ARCH}"
ROOT_UID = 0
# Symbolic links one public root-only source may pass through: the crypto-policy back-ends sshd includes are one.
SOURCE_LINKS = 8
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
HEX = re.compile(r"[0-9a-f]{64}\Z")
DEVICE = re.compile(r"/dev/(?:disk/by-id/)?[a-zA-Z0-9_.:+-]{1,200}\Z")
REGISTRY_DIRECTORY = "/etc/ceph"
REGISTRY_NAME = "podman-auth.json"
REGISTRY_FILE = REGISTRY_DIRECTORY + "/" + REGISTRY_NAME
MAX_REGISTRY = 65536
# Artifact selection is authored; the adapter independently proves its SSH
# route rather than treating a version identifier as transport trust.
DEVICE_IDENTITIES = "device-identities.json"
DEVICE_IDENTITIES_VERSION = "ceph-cluster-device-identities-v2"
HOST_PHASES = frozenset(("profile", "route", "device_identity", "host_devices", "host_observe"))
WORKER_DEADLINE = None

SCOPE_MEMBERS = frozenset(("controller", "version", "cluster", "owner", "fsid", "image", "cephadm", "bootstrap", "hosts", "services", "clusterSSHUser",
                           "clusterSSHTransport", "monIP", "publicNetworks", "clusterNetworks", "callHome", "release", "ansiblePackageVersion",
                           "packageRepository", "packageRepositoryKey", "clusterKeyRef", "registryRef", "registry"))
SCOPE_OPTIONAL = frozenset(("security", "auxiliary", "config", "packages", "nfsGaneshaPackageVersion", "registryTrustBundleRef"))
# cephadm's container_image_<name> settings an auxiliary image may fill, in name order.
AUXILIARY_NAMES = (
    "alertmanager", "alloy", "grafana", "haproxy", "keepalived", "loki", "nginx", "node_exporter",
    "nvmeof", "oauth2_proxy", "prometheus", "promtail", "samba", "samba_metrics", "snmp_gateway",
)
RESOURCE_ID = re.compile(r"(?:0|[1-9][0-9]{0,18})\Z")
# librbd's image ID: lowercase hex cut to RBD_MAX_IMAGE_ID_LENGTH 14 (src/librbd/Utils.cc:75-93, src/include/rbd_types.h:86).
RBD_IMAGE_ID = re.compile(r"[0-9a-f]{1,14}\Z")
# A subvolume's path is /volumes/<group or _nogroup>/<name>/<uuid4> (src/pybind/mgr/volumes/fs/operations/versions/subvolume_v2.py:167).
SUBVOLUME_UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")
SUBVOLUME_PATH = re.compile(r"/volumes/(?:_nogroup|[A-Za-z0-9][A-Za-z0-9_.-]{0,127})/[a-z0-9](?:[-a-z0-9]{0,46}[a-z0-9])?/"
                            r"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\Z")
CORE_PHASES = frozenset(("prerequisites", "claim", "devices", "osd_prepare", "tunnel_preflight", "authorize", "bootstrap", "tunnel_configure", "tunnel_mount",
                         "cache", "enroll", "license", "services", "dashboard", "dashboard_read", "observe", "bootstrap_state", "host_state", "destroy"))
CORE_READ_ONLY_PHASES = frozenset(("prerequisites", "devices", "tunnel_preflight", "observe", "bootstrap_state", "host_state", "dashboard_read"))
EVIDENCE_VERSION = "ceph-core-evidence-v11"
# The specialization ids the last dispatched phase's worker applied; the remote wrapper reads it once per process.
APPLIED = []
DASHBOARD_USER = "admin"
DASHBOARD_PORT = 8443
DASHBOARD_LENGTH = 32
DASHBOARD_ALPHABET = string.ascii_letters + string.digits + "-_."
DASHBOARD_EXCLUDED = ("osd", "host", "dashboard", "pool", "block", "nfs", "ceph", "monitors", "gateway", "logs", "crush", "maps")
DASHBOARD_CERTIFICATE = "mgr/dashboard/crt"
DASHBOARD_URL = re.compile(r"https://(?:[A-Za-z0-9.-]{1,253}|\[[0-9A-Fa-f:.]{2,45}\]):8443/\Z")
DASHBOARD_BUDGET = 540
DASHBOARD_INTERVAL = 5
# cephadm bootstrap under the bootstrap call's own longer worker budget (ceph_core.BOOTSTRAP_BUDGET), leaving its proofs time.
BOOTSTRAP_TIMEOUT = 1620
# Time-service presence is an effect boundary, not a native version compatibility table.
CEPHADM_TIME_UNITS = ("chrony.service", "chronyd.service", "systemd-timesyncd.service", "ntpd.service", "ntp.service", "ntpsec.service",
                      "openntpd.service", "timemaster.service")
# A manager restart leaves the orchestrator unanswered for a while: each wait is bounded, and every wait in one ordinary phase
# ends by one horizon inside that phase's worker budget.
MANAGER_WAIT = 300
MANAGER_INTERVAL = 5
MANAGER_HORIZON = 480
MANAGER_FAILURES = ("native-command", "native-timeout", "native-output")
# Health checks that never block day-0 readiness: call-home egress is the operator's network, not the cluster's state.
TOLERATED_HEALTH = frozenset(("CHA_ERROR_SENDING_REPORT",))
HEALTH_CHECK = re.compile(r"[A-Z][A-Z0-9_]{0,63}\Z")
PENDING_DAEMON = re.compile(r"(mon|mgr)\.[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
# Each admitted daemon type: (executor, host rule identity|request|service, counted toward readiness, image rule).
DAEMON_TYPES = {
    "mon": ("cluster", "identity", True, "ceph"),
    "mgr": ("cluster", "identity", True, "ceph"),
    "osd": ("cluster", "request", True, "ceph"),
    "crash": ("delegated", "delegated", False, "none"),
    "ceph-exporter": ("cluster", "request", True, "ceph"),
    "mds": ("cluster", "service", True, "ceph"),
    "rgw": ("delegated", "delegated", False, "none"),
    "haproxy": ("delegated", "delegated", False, "none"),
    "keepalived": ("delegated", "delegated", False, "none"),
    "nfs": ("delegated", "delegated", False, "none"),
    "nvmeof": ("delegated", "delegated", False, "none"),
    "alertmanager": ("delegated", "delegated", False, "none"),
    "alloy": ("delegated", "delegated", False, "none"),
    "grafana": ("delegated", "delegated", False, "none"),
    "loki": ("delegated", "delegated", False, "none"),
    "node-exporter": ("delegated", "delegated", False, "none"),
    "prometheus": ("delegated", "delegated", False, "none"),
    "promtail": ("delegated", "delegated", False, "none"),
    "snmp-gateway": ("delegated", "delegated", False, "none"),
    "mgmt-gateway": ("delegated", "delegated", False, "none"),
    "oauth2-proxy": ("delegated", "delegated", False, "none"),
    "smb": ("delegated", "delegated", False, "none"),
    "rbd-mirror": ("delegated", "delegated", False, "none"),
}
# A service another block executes, listed by its cephadm service_name: rgw.<id>, ingress.<id>, nfs.<id>, smb.<id> or a singleton's type,
# crash, rbd-mirror, the log collection services loki, alloy and promtail, and the management gateway's oauth2-proxy among them.
DELEGATED_SERVICE = re.compile(r"(?:(?:rgw|ingress|nfs|smb|nvmeof)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}"
                               r"|alertmanager|alloy|ceph-exporter|crash|grafana|loki|mgmt-gateway|node-exporter|oauth2-proxy|prometheus|promtail"
                               r"|rbd-mirror|snmp-gateway)\Z")


def supported_transport(request):
    image = request.get("image")
    if request.get("clusterSSHTransport") != "verified-tunnel" or not isinstance(image, str):
        return False
    repository, separator, identifier = image.rpartition("@sha256:")
    return bool(repository and separator and HEX.fullmatch(identifier))


def tunnel_helpers():
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel, ceph_tunnel_staging
    return ceph_tunnel, ceph_tunnel_staging


class Refusal(ValueError):
    """A closed, non-sensitive failure reason."""


class RemoteRefusal(Refusal):
    """A native worker's refusal, with the closed cause its endpoint named."""

    def __init__(self, detail=None):
        super().__init__("remote-refusal")
        self.detail = detail


class NativeFailure(Refusal):
    """A native failure whose public diagnostic has an explicit redaction boundary."""

    def __init__(self, reason, coordinate, output, secrets=()):
        super().__init__(reason)
        from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
        self.native_error = native_diagnostics.error_detail(coordinate, output, secrets=secrets)


class NativeOutput(bytes):
    """Private stdout and stderr, retaining the existing two-value runner port."""

    def __new__(cls, output, stderr=b""):
        value = super().__new__(cls, output)
        value.stderr = stderr
        return value


class ManagerRefusal(Refusal):
    """A manager-served command that failed while its manager may be restarting; the token stays the command's own."""


def require(condition, reason="request"):
    if not condition:
        raise Refusal(reason)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "material")
        result[key] = value
    return result


def registry_authority(request):
    value = request["registry"]
    require(isinstance(value, str) and re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?(?::[1-9][0-9]{0,4})?", value) is not None, "material")
    host, separator, port = value.partition(":")
    require(all(part and not part.startswith("-") and not part.endswith("-") for part in host.split(".")), "material")
    require(not separator or 1 <= int(port) <= 65535, "material")
    require(request["image"].split("/", 1)[0] == value, "material")
    return value


def registry_material(request, value):
    """Produce only private authfile bytes; no credential enters argv or evidence."""
    require(isinstance(value, str) and 0 < len(value.encode()) <= MAX_REGISTRY, "material")
    try:
        material = json.loads(value, object_pairs_hook=unique_object)
        require(isinstance(material, dict) and set(material) == {"url", "username", "password"}, "material")
        authority = registry_authority(request)
        require(material["url"] == authority, "material")
        username, password = material["username"], material["password"]
        require(isinstance(username, str) and 0 < len(username.encode()) <= 1024 and not any(c in username for c in ":\x00\r\n"), "material")
        require(isinstance(password, str) and 0 < len(password.encode()) <= 32768 and not any(c in password for c in "\x00\r\n"), "material")
        auth = base64.b64encode((username + ":" + password).encode()).decode("ascii")
        return canonical({"auths": {authority: {"auth": auth}}}) + b"\n"
    except (ValueError, UnicodeError, TypeError):
        raise Refusal("material") from None


def registry_scope(request, data):
    """Refuse aliases, helpers and foreign scopes in the exclusively owned file."""
    try:
        value = json.loads(data, object_pairs_hook=unique_object)
        authority = registry_authority(request)
        require(isinstance(value, dict) and set(value) == {"auths"}, "registry-ownership")
        entries = value["auths"]
        require(isinstance(entries, dict) and set(entries) == {authority}, "registry-ownership")
        entry = entries[authority]
        require(isinstance(entry, dict) and set(entry) == {"auth"} and isinstance(entry["auth"], str), "registry-ownership")
        decoded = base64.b64decode(entry["auth"], validate=True).decode("utf-8")
        username, separator, password = decoded.partition(":")
        require(separator and username and password and not any(c in decoded for c in "\x00\r\n"), "registry-ownership")
        require(len(username.encode()) <= 1024 and len(password.encode()) <= 32768, "registry-ownership")
    except (ValueError, UnicodeError, TypeError):
        raise Refusal("registry-ownership") from None


def registry_metadata(info, pending=False):
    """Non-secret descriptor custody. Never journal material or its digest."""
    result = {"dev": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
              "mode": stat.S_IMODE(info.st_mode), "mtimeNS": info.st_mtime_ns}
    if not pending:
        result["ctimeNS"] = info.st_ctime_ns
    return result


def registry_metadata_record(value, pending=False):
    fields = {"dev", "inode", "uid", "gid", "mode", "mtimeNS"}
    if not pending:
        fields.add("ctimeNS")
    require(isinstance(value, dict) and set(value) == fields and all(type(item) is int for item in value.values()), "registry-ownership")
    require(0 <= value["dev"] < 1 << 64 and 0 < value["inode"] < 1 << 64 and value["uid"] == value["gid"] == 0 and value["mode"] == 0o600, "registry-ownership")
    require(0 <= value["mtimeNS"] < 1 << 63 and (pending or 0 <= value["ctimeNS"] < 1 << 63), "registry-ownership")


@contextlib.contextmanager
def registry_directory(create=False):
    """Hold the fixed root-owned native directory without following any link."""
    directory = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for index, component in enumerate(REGISTRY_DIRECTORY.strip("/").split("/")):
            info = os.fstat(directory)
            require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "registry-ownership")
            try:
                following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            except FileNotFoundError:
                require(index == 1, "registry-ownership")
                if not create:
                    yield None
                    return
                os.mkdir(component, 0o755, dir_fd=directory)
                os.fsync(directory)
                following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            os.close(directory)
            directory = following
        info = os.fstat(directory)
        require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "registry-ownership")
        yield directory
    finally:
        os.close(directory)


def registry_exists(directory, name):
    if directory is None:
        return False
    try:
        os.stat(name, dir_fd=directory, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


def registry_read(directory, name, expected, pending=False, links=(1,)):
    if directory is None:
        return None
    registry_metadata_record(expected, pending=pending)
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink in links and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o600,
                "registry-ownership")
        # A bound path can be replaced. Prove descriptor custody before any
        # content read, including a replacement containing matching bytes.
        require(registry_metadata(info, pending=pending) == expected, "registry-ownership")
        require(0 < info.st_size <= MAX_REGISTRY, "registry-ownership")
        content = handle.read(MAX_REGISTRY + 1)
        after = os.fstat(handle.fileno())
        require(len(content) == info.st_size and registry_metadata(info) == registry_metadata(after) and info.st_nlink == after.st_nlink, "registry-ownership")
        return info, content


def named(value):
    return isinstance(value, str) and NAME.fullmatch(value) is not None


def digest(value):
    return isinstance(value, str) and HEX.fullmatch(value) is not None


def command_environment(environment):
    """Build the fixed native environment with explicitly bounded broker overrides."""
    native_environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C", "LC_ALL": "C"}
    if environment is not None:
        allowed = {"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
                   "CONTAINERS_REGISTRIES_CONF", "DOCKER_CONFIG", "HOME"}
        require(isinstance(environment, dict) and set(environment) <= allowed, "executable")
        require(all(isinstance(value, str) and len(value) <= 4096 and all(ord(char) >= 32 and ord(char) != 127 for char in value)
                    for value in environment.values()), "executable")
        for key, value in environment.items():
            if key.lower() in ("http_proxy", "https_proxy"):
                require(re.fullmatch(r"http://127\.0\.0\.1:[1-9][0-9]{0,4}", value) is not None, "executable")
            elif key.lower() == "no_proxy":
                require(value == "", "executable")
            else:
                require(os.path.isabs(value), "executable")
        native_environment.update(environment)
    return native_environment


def bounded_command(argv, data=b"", timeout=60, limit=MAX_OUTPUT, environment=None):
    """Drain both pipes within a hard byte/time budget; never invoke a shell."""
    require(argv and os.path.isabs(argv[0]), "executable")
    require(all(isinstance(v, str) and "\x00" not in v for v in argv), "executable")
    native_environment = command_environment(environment)
    owned_session = COMMAND_SESSION
    try:
        # Bounded fixed-argv helper that kills its own process group; run_command offers neither.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(
            argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=native_environment,
            start_new_session=owned_session,
        )
    except OSError:
        raise Refusal("native-command") from None
    output = bytearray()
    stderr = bytearray()
    total = 0
    pending = memoryview(data)
    deadline = time.monotonic() + timeout
    try:
        with selectors.DefaultSelector() as selector:
            for pipe in (process.stdout, process.stderr):
                os.set_blocking(pipe.fileno(), False)
                selector.register(pipe, selectors.EVENT_READ)
            if pending:
                os.set_blocking(process.stdin.fileno(), False)
                selector.register(process.stdin, selectors.EVENT_WRITE)
            else:
                process.stdin.close()
            while selector.get_map():
                remaining = deadline - time.monotonic()
                require(remaining > 0, "native-timeout")
                for key, unused in selector.select(min(remaining, 1)):
                    if key.fileobj is process.stdin:
                        try:
                            count = os.write(key.fd, pending[:65536])
                        except BrokenPipeError:
                            count = len(pending)
                        pending = pending[count:]
                        if not pending:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                    else:
                        block = os.read(key.fd, 65536)
                        if not block:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                            continue
                        total += len(block)
                        require(total <= limit, "native-output")
                        if key.fileobj is process.stdout:
                            output.extend(block)
                        else:
                            stderr.extend(block)
        # Observe exit without reaping: an exited leader still reserves the
        # group identifier until cleanup stops every inherited descendant.
        while True:
            remaining = deadline - time.monotonic()
            require(remaining > 0, "native-timeout")
            status = os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            if status is not None:
                require(status.si_pid == process.pid, "native-command")
                code = status.si_status if status.si_code == os.CLD_EXITED else -status.si_status
                require(time.monotonic() < deadline, "native-timeout")
                return code, NativeOutput(bytes(output), bytes(stderr))
            time.sleep(min(remaining, 0.02))
    except (subprocess.TimeoutExpired, OSError):
        raise Refusal("native-command") from None
    finally:
        try:
            if owned_session:
                # No poll/wait may release this PID before killpg. Verify that
                # it remains our child even on time/output/refusal paths.
                os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(process.pid, signal.SIGKILL)
            else:
                # The remote supervisor owns the shared worker group. Kill
                # only this command; it stops descendants before publication.
                with contextlib.suppress(ProcessLookupError):
                    process.kill()
            process.wait(timeout=1)
        except (subprocess.TimeoutExpired, OSError):
            raise Refusal("native-command") from None
        finally:
            for pipe in (process.stdin, process.stdout, process.stderr):
                with contextlib.suppress(OSError):
                    pipe.close()


def prove_fips(request):
    if not request.get("security", {}).get("fips"):
        return
    try:
        with open("/proc/sys/crypto/fips_enabled", "rb") as stream:
            kernel = stream.read(3)
        policy = read_regular("/etc/crypto-policies/state/current", maximum=4096).strip()
    except (OSError, Refusal):
        raise NativeFailure("fips-policy", "FIPS host policy", b"the host kernel or inherited crypto policy cannot be proved") from None
    if kernel != b"1\n" or policy.split(b":")[0] != b"FIPS":
        raise NativeFailure("fips-policy", "FIPS host policy", b"the host kernel and inherited crypto policy must both report FIPS mode")


def prove_cephx_key(request, key):
    if "security" not in request:
        return
    try:
        require(isinstance(key, str) and len(key) <= 1024)
        raw = base64.b64decode(key, validate=True)
        require(len(raw) == 28 and int.from_bytes(raw[:2], "little") == 1 and int.from_bytes(raw[10:12], "little") == 16)
    except (ValueError, TypeError, Refusal):
        raise NativeFailure("cephx-policy", "CephX key policy", b"a native key does not prove the requested aes type and 16-byte key length") from None


def validate(request):
    """Admit only the implemented core envelope, including concrete devices."""
    require(isinstance(request, dict))
    required = SCOPE_MEMBERS | {"action"}
    resources = {"pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings", "service", "gateway",
                 "nfsExport", "objectUser", "objectBucket", "nvmeof"}
    require(required <= set(request) and not set(request) - required - SCOPE_OPTIONAL - {"delegated"} - resources)
    hosts = scope_checks(request)
    validate_delegated(request, hosts)
    if request["action"] == "cluster":
        require(not resources & set(request))
        return request
    require(request["action"] == "service" or "service" not in request)
    if request["action"] == "service":
        require(not (resources - {"service"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_units
        ceph_native_units.validate(request, hosts)
        return request
    require(request["action"] == "gateway" or "gateway" not in request)
    if request["action"] == "gateway":
        require(not (resources - {"gateway"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate(request, hosts)
        return request
    require(request["action"] == "object-user" or "objectUser" not in request)
    if request["action"] == "object-user":
        require(not (resources - {"objectUser"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate_user(request, hosts)
        return request
    require(request["action"] == "object-bucket" or "objectBucket" not in request)
    if request["action"] == "object-bucket":
        require(not (resources - {"objectBucket"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate_bucket(request, hosts)
        return request
    require(request["action"] == "nvmeof" or "nvmeof" not in request)
    if request["action"] == "nvmeof":
        require(not (resources - {"nvmeof"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nvmeof
        ceph_native_nvmeof.validate(request, hosts)
        return request
    require(request["action"] == "nfs-export" or "nfsExport" not in request)
    if request["action"] == "nfs-export":
        require(not (resources - {"nfsExport"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nfs
        ceph_native_nfs.validate_export(request, hosts)
        return request
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_resources
    require(request["action"] in ceph_native_resources.ACTIONS)
    ceph_native_resources.validate(request, hosts)
    return request


def validate_delegated(request, hosts):
    """Admit the services other blocks execute: each listed once, in name order, on distinct request hosts."""
    if "delegated" not in request:
        return
    delegated = request["delegated"]
    require(isinstance(delegated, list) and 1 <= len(delegated) <= 128)
    names = [host["name"] for host in hosts]
    for entry in delegated:
        require(isinstance(entry, dict) and set(entry) == {"name", "hosts"})
        require(isinstance(entry["name"], str) and DELEGATED_SERVICE.fullmatch(entry["name"]) is not None)
        listed = entry["hosts"]
        require(isinstance(listed, list) and bool(listed) and all(isinstance(host, str) and host in names for host in listed))
        require(len(set(listed)) == len(listed))
    services = [entry["name"] for entry in delegated]
    require(services == sorted(set(services)))


def validate_auxiliary(request):
    """Admit the frozen auxiliary digests: each name once, in name order, beneath the registry authority."""
    if "auxiliary" not in request:
        return
    entries = request["auxiliary"]
    require(isinstance(entries, list) and 1 <= len(entries) <= len(AUXILIARY_NAMES))
    for entry in entries:
        require(isinstance(entry, dict) and set(entry) == {"image", "name"})
        require(isinstance(entry["name"], str) and entry["name"] in AUXILIARY_NAMES)
        image = entry["image"]
        require(isinstance(image, str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}", image) is not None)
        require(image.startswith(request["registry"] + "/"))
    order = [AUXILIARY_NAMES.index(entry["name"]) for entry in entries]
    require(order == sorted(set(order)))


def delegated_daemon(daemon, request):
    """Tolerate a daemon another block executes only when its service is listed and it runs on one of that service's hosts."""
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observe
    delegated = {entry["name"]: entry["hosts"] for entry in request.get("delegated", [])}
    service = daemon.get("service_name")
    require(isinstance(service, str) and service in delegated, "foreign-service")
    kind = daemon["daemon_type"]
    if kind in ceph_native_observe.LISTED_SINGLETONS:
        require(service == kind, "foreign-service")
    else:
        require(service.startswith(ceph_native_observe.DELEGATED_TYPES[kind] + "."), "foreign-service")
    require(daemon["hostname"] in delegated[service], "foreign-service")


def validate_scope(scope):
    """Admit one cluster's host scope: the core envelope without its resource action, pool or client."""
    require(isinstance(scope, dict))
    require(SCOPE_MEMBERS <= set(scope) and not set(scope) - SCOPE_MEMBERS - SCOPE_OPTIONAL)
    scope_checks(scope)
    return scope


def host_scope(request):
    """Bind the one host scope a cluster-action request or a host block's scope carries."""
    if isinstance(request, dict) and "action" in request:
        validate(request)
        require(request["action"] == "cluster")
        return {key: value for key, value in request.items() if key != "action"}
    return validate_scope(request)


def package_url(value):
    """A frozen package/key URL with no credentials, redirects or traversal authority."""
    import urllib.parse
    require(isinstance(value, str) and len(value) <= 2048 and not any(ord(c) <= 32 for c in value), "packages")
    try:
        parsed = urllib.parse.urlsplit(value)
        port = parsed.port
    except ValueError:
        raise Refusal("packages") from None
    require(port is None or 0 < port <= 65535, "packages")
    require(parsed.scheme == "https" and parsed.hostname and parsed.username is None and parsed.password is None and not parsed.query
            and not parsed.fragment, "packages")
    decoded = urllib.parse.unquote(parsed.path)
    require("\\" not in decoded and all(part not in (".", "..") for part in decoded.split("/")), "packages")
    return value


def scope_checks(request):
    require(request["version"] == "ceph-core-v32")
    if "security" in request:
        security = request["security"]
        require(isinstance(security, dict) and set(security) == {"fips", "cephxKeyType"})
        require(type(security["fips"]) is bool and security["cephxKeyType"] == "aes")
    require(named(request["controller"]) and named(request["cluster"]) and named(request["owner"]))
    try:
        require(str(uuid.UUID(request["fsid"])) == request["fsid"])
        ipaddress.ip_address(request["monIP"])
    except (ValueError, TypeError, AttributeError):
        raise Refusal("request") from None
    require(isinstance(request["image"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}", request["image"]) is not None)
    tool = request["cephadm"]
    require(isinstance(tool, dict) and set(tool) == {"path", "packageVersion"})
    require(tool["path"] in ("/usr/sbin/cephadm", "/usr/bin/cephadm"))
    for coordinate in (tool["packageVersion"], request["release"], request["ansiblePackageVersion"]):
        require(isinstance(coordinate, str) and 0 < len(coordinate.encode()) <= 1024 and not any(ord(char) < 32 or ord(char) == 127 for char in coordinate))
    if "nfsGaneshaPackageVersion" in request:
        coordinate = request["nfsGaneshaPackageVersion"]
        require(isinstance(coordinate, str) and 0 < len(coordinate.encode()) <= 1024 and not any(ord(char) < 32 or ord(char) == 127 for char in coordinate))
    from urllib.parse import urlsplit
    repository = urlsplit(package_url(request["packageRepository"]))
    key = urlsplit(package_url(request["packageRepositoryKey"]))
    require((repository.scheme, repository.hostname, repository.port or 443) == (key.scheme, key.hostname, key.port or 443), "artifact")
    require(named(request["clusterSSHUser"]))
    require(request["clusterSSHTransport"] in ("native", "verified-tunnel"))
    require(named(request["clusterKeyRef"]) and named(request["registryRef"]))
    require("registryTrustBundleRef" not in request or named(request["registryTrustBundleRef"]))
    require(isinstance(request["registry"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]{1,255}", request["registry"]) is not None)
    registry_authority(request)
    validate_auxiliary(request)
    require(request["callHome"] in ("enabled", "disabled"))
    for key in ("publicNetworks", "clusterNetworks"):
        require(request[key] is None or isinstance(request[key], list) and len(request[key]) <= 8)
        for network in request[key] or []:
            try:
                ipaddress.ip_network(network, strict=True)
            except (ValueError, TypeError):
                raise Refusal("request") from None
    require(bool(request["publicNetworks"]))
    validate_config(request)
    hosts, seen = validate_hosts(request)
    package = request.get("packages")
    if package is not None:
        require(isinstance(package, dict) and {"version", "licenseAccepted", "sources"} <= set(package)
                <= {"version", "licenseAccepted", "sources", "packageRepositoryTrustBundleRef"})
        require(package["version"] == "ceph-packages-v3" and package["licenseAccepted"] is True)
        require("packageRepositoryTrustBundleRef" not in package or named(package["packageRepositoryTrustBundleRef"]))
        require(isinstance(package["sources"], list) and len(package["sources"]) == len(hosts))
        names = []
        required_source = {"host", "profile", "media", "publicationPath", "url", "certificateRef"}
        for source in package["sources"]:
            require(isinstance(source, dict) and required_source <= set(source) and not set(source) - required_source - {"mediaSHA256", "mediaSize"})
            require(all(isinstance(source[k], str) and 0 < len(source[k]) <= 2048 for k in required_source))
            require(named(source["host"]) and named(source["profile"]) and named(source["certificateRef"]))
            require(("mediaSHA256" in source) == ("mediaSize" in source))
            if "mediaSHA256" in source:
                require(digest(source["mediaSHA256"]) and type(source["mediaSize"]) is int and 0 < source["mediaSize"] <= 64 << 30)
            names.append(source["host"])
        require(names == sorted(host["name"] for host in hosts))
    validate_services(request, hosts, seen)
    return hosts


def validate_config(request):
    """Admit explicit native memory and two-replica pool-default pairs."""
    if "config" not in request:
        return
    config = request["config"]
    require(isinstance(config, dict) and bool(config) and set(config) <= {"osd", "global"}, "config-unqualified")
    if "global" in config:
        global_options = config["global"]
        require(isinstance(global_options, dict) and global_options == {"osd_pool_default_size": "2", "osd_pool_default_min_size": "2"}, "config-unqualified")
    if "osd" not in config:
        return
    osd = config["osd"]
    require(isinstance(osd, dict) and set(osd) == {"osd_memory_target", "osd_memory_target_autotune"}, "config-unqualified")
    require(osd["osd_memory_target_autotune"] == "false", "config-unqualified")
    target = osd["osd_memory_target"]
    require(isinstance(target, str) and re.fullmatch(r"[1-9][0-9]{0,19}", target) is not None, "config-unqualified")
    require(2 * 1024 ** 3 <= int(target) <= 2 ** 64 - 1, "config-unqualified")


def bootstrap_config(request):
    """Generate deterministic INI only after the closed request validates."""
    validate(request)
    content = ("[global]\npublic_network = " + ",".join(request["publicNetworks"]) +
               "\nauth_cluster_required = cephx\nauth_service_required = cephx\nauth_client_required = cephx\n")
    for name, value in sorted(request.get("config", {}).get("global", {}).items()):
        content += name + " = " + value + "\n"
    for section, values in sorted(request.get("config", {}).items()):
        if section == "global":
            continue
        content += "\n[" + section + "]\n"
        for name, value in sorted(values.items()):
            content += name + " = " + value + "\n"
    return content.encode()


def validate_hosts(request):
    """Bind every eligible host and disk to one explicit identity."""
    hosts = request["hosts"]
    require(isinstance(hosts, list) and 1 <= len(hosts) <= 64)
    seen = set()
    wwns = set()
    for host in hosts:
        require(isinstance(host, dict) and set(host) >= {"name", "machine", "address", "user", "port", "devices", "labels", "location", "sshKeyRef"})
        require(not set(host) - {"name", "machine", "address", "user", "port", "devices", "labels", "location", "sshKeyRef",
                                 "hostKeyRef", "knownHosts", "physical", "hardware"})
        require(all(named(host[k]) for k in ("name", "machine", "user")))
        require(named(host["sshKeyRef"]))
        require(host["labels"] is None or isinstance(host["labels"], list) and all(named(v) for v in host["labels"]))
        require(isinstance(host["location"], dict) and all(named(k) and named(v) for k, v in host["location"].items()))
        require(not host["location"], "host-location-unqualified")
        require(not host.get("hostKeyRef") or named(host["hostKeyRef"]))
        require(not host.get("knownHosts") or isinstance(host["knownHosts"], str) and len(host["knownHosts"]) <= 65536)
        require(host["name"] not in seen)
        seen.add(host["name"])
        try:
            ipaddress.ip_address(host["address"])
        except (ValueError, TypeError):
            raise Refusal("request") from None
        require(type(host["port"]) is int and 1 <= host["port"] <= 65535)
        require(isinstance(host["devices"], list) and len(host["devices"]) <= 128)
        physical = host.get("physical", False)
        require(type(physical) is bool and ("hardware" not in host or physical))
        if physical:
            hardware = host.get("hardware")
            require(isinstance(hardware, dict) and set(hardware) == {"uuid", "macs"})
            require(isinstance(hardware["uuid"], str) and SUBVOLUME_UUID.fullmatch(hardware["uuid"]) is not None)
            require(isinstance(hardware["macs"], list) and 1 <= len(hardware["macs"]) <= 32
                    and all(isinstance(m, str) and re.fullmatch(r"[0-9a-f]{2}(?::[0-9a-f]{2}){5}", m) for m in hardware["macs"])
                    and hardware["macs"] == sorted(set(hardware["macs"])) and len(host["devices"]) <= 3)
        paths = set()
        serials = set()
        for device in host["devices"]:
            require(isinstance(device, dict) and set(device) >= {"path", "serial", "sizeBytes"}
                    and not set(device) - {"path", "serial", "sizeBytes", "rotational", "wwn"})
            require(isinstance(device["path"], str) and DEVICE.fullmatch(device["path"]) is not None)
            if physical:
                require(set(device) == {"path", "serial", "sizeBytes", "wwn"} and device["serial"] == "" and device["sizeBytes"] == 0)
                require(isinstance(device["wwn"], str) and re.fullmatch(r"0x[0-9a-f]{16}(?:[0-9a-f]{16})?", device["wwn"]) is not None)
                require(device["path"] == "/dev/disk/by-id/wwn-" + device["wwn"] and device["path"] not in paths and device["wwn"] not in wwns)
                wwns.add(device["wwn"])
                paths.add(device["path"])
                continue
            require("wwn" not in device)
            require(named(device["serial"]) and device["path"] not in paths and device["serial"] not in serials)
            paths.add(device["path"])
            serials.add(device["serial"])
            require(type(device["sizeBytes"]) is int and device["sizeBytes"] > 0)
            require(type(device.get("rotational", False)) is bool)
    require(len({host["name"].split(".", 1)[0] for host in hosts}) == len(hosts), "host-identity")
    require(request["bootstrap"] in seen)
    require(request["monIP"] == next(h["address"] for h in hosts if h["name"] == request["bootstrap"]))
    return hosts, seen


def validate_services(request, hosts, seen):
    """Preserve native placement while bounding all OSD effect targets."""
    services = request["services"]
    require(isinstance(services, list) and 2 <= len(services) <= 128)
    kinds = set()
    ids = set()
    selected_devices = set()
    for service in services:
        require(isinstance(service, dict))
        kind = service.get("service_type")
        require(kind in ("mon", "mgr", "osd", "mds"), "service-unqualified")
        kinds.add(kind)
        require(not set(service) - {"service_type", "service_id", "placement", "spec"}, "service-unqualified")
        identity = (kind, service.get("service_id", ""))
        require(identity not in ids)
        ids.add(identity)
        placement = service.get("placement")
        require(isinstance(placement, dict) and "hosts" in placement and not set(placement) - {"hosts", "count", "count_per_host"}, "placement-unqualified")
        require(isinstance(placement["hosts"], list) and bool(placement["hosts"]))
        require(all(isinstance(h, str) and h in seen for h in placement["hosts"]))
        require(len(set(placement["hosts"])) == len(placement["hosts"]))
        require("count" not in placement or type(placement["count"]) is int and 1 <= placement["count"] <= len(placement["hosts"]), "placement-unqualified")
        require("count_per_host" not in placement or type(placement["count_per_host"]) is int and placement["count_per_host"] == 1, "placement-unqualified")
        if kind == "mds":
            identifier = service.get("service_id")
            require(named(identifier) and not identifier[0].isdigit(), "service-unqualified")
            require("spec" not in service, "service-unqualified")
            continue
        if kind != "osd":
            require(not service.get("spec") and not service.get("service_id"), "service-unqualified")
            continue
        require(named(service.get("service_id")))
        require(placement.get("count", len(placement["hosts"])) == len(placement["hosts"]), "device-unproved")
        spec = service.get("spec")
        require(isinstance(spec, dict) and "data_devices" in spec
                and not set(spec) - {"data_devices", "db_devices", "wal_devices", "encrypted", "tpm2"}, "osd-unqualified")
        require(all(type(spec.get(field, False)) is bool for field in ("encrypted", "tpm2")), "osd-unqualified")
        require(not spec.get("tpm2") or spec.get("encrypted"), "osd-unqualified")
        for field in ("data_devices", "db_devices", "wal_devices"):
            if field not in spec:
                continue
            selection = spec[field]
            require(isinstance(selection, dict) and set(selection) == {"paths"}, "osd-unqualified")
            require(isinstance(selection["paths"], list) and bool(selection["paths"]))
            for name in placement["hosts"]:
                host = next(h for h in hosts if h["name"] == name)
                for path in selection["paths"]:
                    selected = selected_device(host, path)
                    require((name, selected["path"]) not in selected_devices, "device-overlap")
                    selected_devices.add((name, selected["path"]))
    require(kinds >= {"mon", "mgr"})
    require(request["bootstrap"] in next(s["placement"]["hosts"] for s in services if s["service_type"] == "mon"))
    if request["clusterSSHTransport"] == "verified-tunnel":
        helper, unused = tunnel_helpers()
        helper.targets(request)
        require(request["bootstrap"] in next(s["placement"]["hosts"] for s in services if s["service_type"] == "mgr"), "native-tunnel-unproved")
    require(selected_devices == {(h["name"], d["path"]) for h in hosts for d in h["devices"]}, "device-unproved")


def auxiliary_device_paths(request, host_name):
    host = next(h for h in request["hosts"] if h["name"] == host_name)
    return {selected_device(host, path)["path"] for service in request["services"]
            if service["service_type"] == "osd" and host_name in service["placement"]["hosts"]
            for field in ("db_devices", "wal_devices") for path in service["spec"].get(field, {}).get("paths", [])}


def osd_device_group(request, host, data_path):
    for service in request["services"]:
        if service["service_type"] != "osd" or host["name"] not in service["placement"]["hosts"]:
            continue
        spec = service["spec"]
        if data_path not in [selected_device(host, path)["path"] for path in spec["data_devices"]["paths"]]:
            continue
        return {field: [selected_device(host, path) for path in ([data_path] if field == "data_devices" else spec[field]["paths"])]
                for field in ("data_devices", "db_devices", "wal_devices") if field in spec}
    raise Refusal("device-unproved")


def device_path(value):
    if isinstance(value, dict):
        require(set(value) == {"path"}, "device-unqualified")
        value = value["path"]
    require(isinstance(value, str) and DEVICE.fullmatch(value) is not None, "device-unproved")
    return value


def selected_device(host, value):
    path = device_path(value)
    found = [d for d in host["devices"] if path in (d["path"], "/dev/disk/by-id/virtio-" + d["serial"])]
    require(len(found) == 1, "device-unproved")
    return found[0]


def placement_groups_ready(status, action):
    """Require a complete, typed PG snapshot; unfinished PGs remain partial."""
    pgmap = status.get("pgmap")
    require(isinstance(pgmap, dict), "native-output")
    count, states = pgmap.get("num_pgs"), pgmap.get("pgs_by_state")
    require(type(count) is int and count >= 0 and isinstance(states, list) and len(states) <= 128, "native-output")
    for state in states:
        require(isinstance(state, dict) and isinstance(state.get("state_name"), str) and bool(state["state_name"]), "native-output")
        require(type(state.get("count")) is int and state["count"] > 0, "native-output")
    require(sum(state["count"] for state in states) == count, "native-output")
    if count == 0:
        return action == "cluster"
    return all(state["state_name"] == "active+clean" for state in states)


def daemon_identity_host(kind, identifier, hosts):
    """Map the exact provider-generated daemon ID to one frozen host."""
    require(kind in ("mon", "mgr") and named(identifier), "foreign-service")
    matches = []
    for host in hosts:
        stem = host.split(".", 1)[0]
        if kind == "mon" and identifier == stem or kind == "mgr" and re.fullmatch(re.escape(stem) + r"\.[a-z]{6}", identifier):
            matches.append(host)
    require(len(matches) == 1, "foreign-service")
    return matches[0]


def service_daemon(kind, daemon, services):
    """The one declared service whose native daemon ID <service_id>.<host short name>.<six lowercase letters> names this daemon on one of its hosts."""
    hostname, identifier = daemon.get("hostname"), daemon.get("daemon_id")
    require(isinstance(hostname, str) and named(identifier), "foreign-service")
    stem = re.escape(hostname.split(".", 1)[0])
    matches = [s for s in services if s["service_type"] == kind
               and re.fullmatch(re.escape(s["service_id"]) + r"\." + stem + r"\.[a-z]{6}", identifier) is not None]
    require(len(matches) == 1 and hostname in matches[0]["placement"]["hosts"], "foreign-service")
    return matches[0]["service_id"]


def read_regular(path, maximum=MAX_INPUT, private=False):
    """Open once; no symlinks, non-regular files, oversized or writable inputs."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.geteuid(), "file")
        require(info.st_mode & (0o077 if private else 0o022) == 0 and 0 < info.st_size <= maximum, "file")
        data = handle.read(maximum + 1)
        require(len(data) == info.st_size, "file")
        return data


def private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, "wb") as handle:
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())


def publish_regular(directory, name, data, uid):
    """Publish complete protected bytes without replacing an existing name."""
    stage = ".bootwright-" + uuid.uuid4().hex
    fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
            os.fchown(handle.fileno(), uid, uid)
            handle.flush()
            os.fsync(handle.fileno())
        os.link(stage, name, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
        os.fsync(directory)
    finally:
        os.unlink(stage, dir_fd=directory)


def dashboard_policy(password):
    """A local copy of the 9.9.1 dashboard password policy, at least as strict as Ceph's own check."""
    if not isinstance(password, str) or len(password) != DASHBOARD_LENGTH or any(char not in DASHBOARD_ALPHABET for char in password):
        return False
    lowered = password.lower()
    if DASHBOARD_USER in lowered or any(word in lowered for word in DASHBOARD_EXCLUDED):
        return False
    for text in (password, lowered):
        for first, second, third in zip(text, text[1:], text[2:]):
            if first == second == third or ord(first) + 1 == ord(second) == ord(third) - 1 or ord(first) - 1 == ord(second) == ord(third) + 1:
                return False
    return True


def dashboard_password(choice=secrets.choice):
    """Draw again until the candidate passes the policy; a bounded number of draws."""
    for unused in range(1024):
        candidate = "".join(choice(DASHBOARD_ALPHABET) for unused in range(DASHBOARD_LENGTH))
        if dashboard_policy(candidate):
            return candidate
    raise Refusal("dashboard-policy")


def dashboard_decode(data):
    try:
        password = data.decode("ascii")
    except (UnicodeError, AttributeError):
        raise Refusal("dashboard-identity") from None
    require(dashboard_policy(password), "dashboard-identity")
    return password


def certificate_sha256(pem):
    """The SHA-256 of one PEM certificate's DER bytes."""
    require(isinstance(pem, bytes) and 0 < len(pem) <= 65536, "native-output")
    begin, end = b"-----BEGIN CERTIFICATE-----", b"-----END CERTIFICATE-----"
    require(pem.count(begin) == 1 and pem.count(end) == 1, "native-output")
    body = pem.split(begin, 1)[1].split(end, 1)[0]
    try:
        der = base64.b64decode(b"".join(body.split()), validate=True)
    except ValueError:
        raise Refusal("native-output") from None
    require(0 < len(der) <= 16384, "native-output")
    return hashlib.sha256(der).hexdigest()


def monitor_address(value):
    """The IP of a monitor's native v2 address, such as 192.0.2.1:3300 or [2001:db8::1]:3300."""
    require(isinstance(value, str) and len(value) <= 64 and ":" in value, "native-output")
    host = value.rsplit(":", 1)[0]
    if host.startswith("[") and host.endswith("]"):
        host = host[1:-1]
    try:
        return str(ipaddress.ip_address(host))
    except ValueError:
        raise Refusal("native-output") from None


def marker_identity(request):
    result = {key: request[key] for key in ("cluster", "owner", "fsid", "image", "cephadm", "bootstrap", "clusterSSHUser", "clusterSSHTransport", "callHome")}
    # Trust material can be populated after Machine installation; it is not an
    # authored replacement for the cluster's durable native identity.
    result["hosts"] = [{key: value for key, value in host.items() if key != "knownHosts"} for host in request["hosts"]]
    if "config" in request:
        result["config"] = {section: dict(values) for section, values in request["config"].items()}
    return result


class Markers:
    """Root-owned per-incarnation ownership, published before native effects."""

    def __init__(self, request, root=ROOT):
        self.request = request
        self.root = root
        self.path = os.path.join(root, request["fsid"])

    def read(self, name="owner.json"):
        try:
            return json.loads(read_regular(os.path.join(self.path, name), private=True))
        except FileNotFoundError:
            return None
        except (ValueError, OSError):
            raise Refusal("ownership") from None

    def owned(self):
        record = self.read()
        require(record is None or record == marker_identity(self.request), "ownership")
        return record is not None

    def create(self):
        # Every directory component is opened without following a link. Existing
        # ancestor permissions must exclude group/other writes.
        current = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            for component in self.path.strip("/").split("/"):
                try:
                    os.mkdir(component, 0o700, dir_fd=current)
                    os.fsync(current)
                except FileExistsError:
                    pass
                following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
                info = os.fstat(following)
                require(info.st_uid == os.geteuid() and info.st_mode & 0o022 == 0, "ownership")
                os.close(current)
                current = following
            if not self.owned():
                self.write("owner.json", marker_identity(self.request))
        finally:
            os.close(current)

    def write(self, name, value):
        existing = self.read(name)
        require(existing is None or existing == value, "ownership")
        if existing is not None:
            return
        # Stage and publish without replacing any concurrent ownership record.
        # A hard link publishes the whole file with no overwrite race. Removing
        # its private staging name leaves the sole durable link.
        fd, stage = tempfile.mkstemp(prefix=".ownership-", dir=self.path)
        try:
            with os.fdopen(fd, "wb") as handle:
                handle.write(canonical(value))
                handle.flush()
                os.fsync(handle.fileno())
            try:
                os.link(stage, os.path.join(self.path, name), follow_symlinks=False)
            except FileExistsError:
                require(self.read(name) == value, "ownership")
        finally:
            os.unlink(stage)
        fd = os.open(self.path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


class Custody:
    """Retained private material: a reservation record, one exclusive root-only 0600 file, then its identity record."""

    def __init__(self, worker, kind, suffix, limit, decode, version=None):
        self.worker = worker
        self.markers = worker.markers
        self.kind = kind
        self.suffix = suffix
        self.limit = limit
        self.decode = decode
        self.reason = kind + "-identity"
        self.version = version or "ceph-" + kind + "-custody-v1"

    def name(self, record):
        return self.kind + "-" + record + self.suffix + ".json"

    @contextlib.contextmanager
    def directory(self):
        """Hold only the fixed root-owned private FSID area, without symlinks."""
        require(self.markers.path == os.path.join(ROOT, self.worker.request["fsid"]), self.reason)
        directory = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            for component in self.markers.path.strip("/").split("/"):
                info = os.fstat(directory)
                require(info.st_uid == 0 and info.st_mode & 0o022 == 0, self.reason)
                following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
                os.close(directory)
                directory = following
            info = os.fstat(directory)
            require(info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700, self.reason)
            yield directory
        finally:
            os.close(directory)

    @staticmethod
    def metadata(info, complete=False):
        result = {"dev": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode)}
        if complete:
            result.update(sizeBytes=info.st_size, mtimeNS=info.st_mtime_ns, ctimeNS=info.st_ctime_ns)
        return result

    def metadata_record(self, value, complete=False):
        fields = {"dev", "inode", "uid", "gid", "mode"}
        if complete:
            fields |= {"sizeBytes", "mtimeNS", "ctimeNS"}
        require(isinstance(value, dict) and set(value) == fields and all(type(item) is int for item in value.values()), self.reason)
        require(0 <= value["dev"] < 1 << 64 and 0 < value["inode"] < 1 << 64 and value["uid"] == value["gid"] == 0 and value["mode"] == 0o600,
                self.reason)
        if complete:
            require(0 < value["sizeBytes"] <= self.limit and 0 <= value["mtimeNS"] < 1 << 63 and 0 <= value["ctimeNS"] < 1 << 63, self.reason)

    def records(self, scope):
        reserved = self.markers.read(self.name("reserved"))
        ready = self.markers.read(self.name("id"))
        for record, complete in ((reserved, False), (ready, True)):
            if record is None:
                continue
            require(isinstance(record, dict) and set(record) == {"version", "scope", "custodyID", "metadata"}, self.reason)
            require(record["version"] == self.version and canonical(record["scope"]) == canonical(scope), self.reason)
            require(isinstance(record["custodyID"], str) and re.fullmatch(r"[0-9a-f]{32}", record["custodyID"]) is not None, self.reason)
            self.metadata_record(record["metadata"], complete=complete)
        require(ready is None or reserved is not None and ready["custodyID"] == reserved["custodyID"] and
                all(ready["metadata"][key] == value for key, value in reserved["metadata"].items()), self.reason)
        if reserved is None:
            with self.directory() as directory:
                require(not registry_exists(directory, self.name("material")), self.reason)
        return reserved, ready

    def read(self, directory, metadata, complete=False):
        """Descriptor identity precedes every read of retained private material."""
        self.metadata_record(metadata, complete=complete)
        try:
            fd = os.open(self.name("material"), os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
        except FileNotFoundError:
            raise Refusal(self.reason) from None
        with os.fdopen(fd, "rb") as handle:
            info = os.fstat(handle.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and self.metadata(info, complete=complete) == metadata, self.reason)
            require(0 <= info.st_size <= self.limit and (not complete or info.st_size > 0), self.reason)
            data = handle.read(self.limit + 1)
            after = os.fstat(handle.fileno())
            require(len(data) == info.st_size and after.st_nlink == 1
                    and self.metadata(info, complete=True) == self.metadata(after, complete=True), self.reason)
            return info, data

    def prepare(self, scope, generate):
        """Reserve, generate once and retain; a complete retained copy is never regenerated."""
        reserved, ready = self.records(scope)
        if ready is not None:
            return self.material(scope)
        filename = self.name("material")
        with self.directory() as directory:
            if reserved is None:
                fd = os.open(filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
                created = os.fstat(fd)
                try:
                    os.fchmod(fd, 0o600)
                    os.fchown(fd, 0, 0)
                    os.fsync(fd)
                    os.fsync(directory)
                    reserved = {"version": self.version, "scope": scope, "custodyID": uuid.uuid4().hex,
                                "metadata": self.metadata(os.fstat(fd))}
                    self.markers.write(self.name("reserved"), reserved)
                    require(self.markers.read(self.name("reserved")) == reserved, self.reason)
                finally:
                    os.close(fd)
                    if self.markers.read(self.name("reserved")) is None:
                        observed = os.stat(filename, dir_fd=directory, follow_symlinks=False)
                        require(observed.st_dev == created.st_dev and observed.st_ino == created.st_ino and observed.st_nlink == 1, self.reason)
                        os.unlink(filename, dir_fd=directory)
                        os.fsync(directory)
            info, data = self.read(directory, reserved["metadata"])
            try:
                self.decode(data)
            except Refusal:
                require(self.markers.read(self.name("attempt")) is None and self.markers.read(self.name("applied")) is None, self.reason)
                data = generate()
                self.decode(data)
                fd = os.open(filename, os.O_RDWR | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
                with os.fdopen(fd, "r+b") as handle:
                    current = os.fstat(handle.fileno())
                    require(stat.S_ISREG(current.st_mode) and current.st_nlink == 1
                            and self.metadata(current, complete=True) == self.metadata(info, complete=True), self.reason)
                    handle.truncate(0)
                    handle.write(data)
                    handle.flush()
                    os.fsync(handle.fileno())
            info, retained = self.read(directory, reserved["metadata"])
            require(retained == data, self.reason)
            record = dict(reserved, metadata=self.metadata(info, complete=True))
            self.markers.write(self.name("id"), record)
        return self.material(scope)

    def material(self, scope):
        unused, ready = self.records(scope)
        require(ready is not None, self.reason)
        with self.directory() as directory:
            unused, data = self.read(directory, ready["metadata"], complete=True)
        return self.decode(data)

    def binding(self, scope, version):
        unused, ready = self.records(scope)
        require(ready is not None, self.reason)
        return {"version": version, "scope": scope, "custodyID": ready["custodyID"]}

    def prove_binding(self, binding):
        """The native effect this custody bound: its attempt, and its completion once recorded."""
        require(canonical(self.markers.read(self.name("attempt"))) == canonical(binding), self.reason)
        applied = self.markers.read(self.name("applied"))
        if applied is not None:
            require(canonical(applied) == canonical(binding), self.reason)
        return binding


class Worker:
    """One native host boundary. The controller supplies only fixed phases."""

    admit = staticmethod(validate)

    def __init__(self, request, host, run=bounded_command, markers=None):
        self.request = self.admit(request)
        self.host = next((h for h in request["hosts"] if h["name"] == host), None)
        require(self.host is not None)
        self.run = run
        self.markers = markers or Markers(request)
        self.tool = request["cephadm"]["path"]
        self._profile = None
        self.host_markers = None
        self.clock = time.monotonic
        self.pause = time.sleep
        self.horizon = None
        self.execution_deadline = None
        self.blocking = []
        from ansible_collections.bootwright.core.plugins.module_utils import native_specializations
        self.selection = native_specializations.Selection({"host-cephadm": self.verified_tool, "image-cephadm": self.image_payload})
        self._tool_bytes = None
        self.packages = None
        if self.host.get("physical"):
            from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
            ceph_host_runtime.physical_hardware(self.host)

    def native_coordinate(self):
        """The selected image and package, then as many applied specialization ids as the public bound leaves room for."""
        from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
        base = self.request["image"] + "; host package " + self.request["cephadm"]["packageVersion"]
        room, kept = native_diagnostics.MAX_COORDINATE - len((base + "; specializations ").encode()), ""
        for name in sorted(self.selection.applied):
            candidate = kept + ("," if kept else "") + name
            if len(candidate.encode()) > room:
                break
            kept = candidate
        return base + "; specializations " + kept if kept else base

    def command(self, argv, data=b"", timeout=60, json_result=True, recover=None, secrets=()):
        code, output = self.run(argv, data=data, timeout=timeout)
        if code != 0 and (recover is None or not recover(code, output)):
            raise NativeFailure("native-command", self.native_coordinate(), getattr(output, "stderr", b"") or output, secrets=(data,) + tuple(secrets))
        if not json_result:
            return output
        try:
            return json.loads(output)
        except (ValueError, UnicodeError):
            raise Refusal("native-output") from None

    def host_preparation(self):
        """This host's own preparation markers, scoped by owner, FSID and host."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        return runtime.prepared(self.request, self.host["name"], markers=self.host_markers)

    def runtime_profile(self):
        """The profile this host's preparation block proved, re-proved against the held image."""
        if self._profile is None:
            from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
            durable = self.host_preparation()
            require(durable is not None, "runtime-profile")
            self._profile = runtime.current_profile(self.request, self.host["name"], durable,
                                                    lambda argv, timeout, limit: self.command(argv, timeout=timeout, json_result=False))
        return self._profile

    def manager_uid(self):
        return self.runtime_profile()["uid"]

    def observed_manager_uid(self):
        """An observation lends no uid before preparation, and never a contradicting one."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        if self._profile is not None:
            return self._profile["uid"]
        durable = self.host_preparation()
        return None if durable is None or durable.read(runtime.PROFILE) is None else self.manager_uid()

    def manager_policy(self):
        return self.runtime_profile()["policy"]

    def manager_label(self):
        return self.runtime_profile()["label"]

    def ceph(self, *args, data=b"", json_result=True, secrets=(), timeout=60):
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            return self.command([
                "/usr/bin/ceph", "--conf", os.path.join(self.markers.path, "ceph.conf"),
                "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"),
                "--format", "json", *args,
            ], data=data, json_result=json_result, secrets=secrets, timeout=timeout)

    def recovery_enabled(self):
        return self.request["clusterSSHTransport"] == "verified-tunnel" and self.markers.read("bootstrap-scope.json") is not None

    def ceph_text(self, *args):
        """Primitive native output still holds the same original admin custody."""
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            return self.command(["/usr/bin/ceph", "--conf", os.path.join(self.markers.path, "ceph.conf"),
                                 "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"), *args], json_result=False)

    @contextlib.contextmanager
    def manager_served(self):
        """A command the active manager serves: its failed, timed-out or empty answer may be that manager restarting."""
        try:
            yield
        except Refusal as error:
            if type(error) in (Refusal, NativeFailure) and str(error) in MANAGER_FAILURES:
                transient = ManagerRefusal(str(error))
                transient.native_cause = error
                raise transient from None
            raise

    def wait_deadline(self):
        """One manager wait's end: its own bound, never past the phase's horizon."""
        deadline = self.clock() + MANAGER_WAIT
        return deadline if self.horizon is None else min(deadline, self.horizon)

    def manager_retry(self, action, *args, **options):
        """Repeat a manager-served read or idempotent declaration, bounded, while the manager that serves it restarts."""
        deadline = self.wait_deadline()
        while True:
            try:
                with self.manager_served():
                    return action(*args, **options)
            except ManagerRefusal as error:
                if self.clock() + MANAGER_INTERVAL >= deadline:
                    raise getattr(error, "native_cause", error) from None
                self.pause(MANAGER_INTERVAL)

    def manager_wait(self, previous=None, name=None):
        """After an action that restarts the active manager: wait, bounded, until the MON maps an available active manager other
        than the restarted instance, the named one where the caller knows it, and its orchestrator answers with cephadm."""
        deadline = self.wait_deadline()
        while True:
            manager = self.ceph("mgr", "dump")
            require(isinstance(manager, dict), "native-output")
            if manager.get("available") is True and manager.get("active_gid") != previous and name in (None, manager.get("active_name")):
                try:
                    with self.manager_served():
                        status = self.ceph("orch", "status")
                except ManagerRefusal:
                    status = None
                if isinstance(status, dict) and status.get("available") is True and status.get("backend") == "cephadm":
                    return manager
            require(self.clock() + MANAGER_INTERVAL < deadline, "native-timeout")
            self.pause(MANAGER_INTERVAL)

    def services_inventory(self):
        """The native empty inventory sentinel ignores its JSON format flag."""
        raw = self.ceph("orch", "ls", json_result=False)
        require(isinstance(raw, bytes) and len(raw) <= MAX_OUTPUT, "native-output")
        if raw.strip() == b"No services reported":
            return []
        try:
            value = json.loads(raw, object_pairs_hook=unique_object)
        except (ValueError, UnicodeError):
            raise Refusal("native-output") from None
        require(isinstance(value, list) and len(value) <= 1024 and all(isinstance(row, dict) for row in value), "native-output")
        return value

    def recovery_scope(self):
        unused, staging = tunnel_helpers()
        scope = staging.recovery_scope(self.request)
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def bootstrap_sources(self):
        return {"config": os.path.join(self.markers.path, "ceph.conf"),
                "keyring": os.path.join(self.markers.path, "ceph.client.admin.keyring")}

    def bootstrap_outputs(self, tighten):
        """cephadm writes its config under the caller's umask, 0644 under sudo, and its keyring 0600: each is root's own regular,
        single-link file in the private FSID area, closed to writes by others, and is made private only before its identity is
        recorded, so a recorded identity never changes."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        with custody.directory(self.markers.path) as parent:
            for path in self.bootstrap_sources().values():
                require(os.path.dirname(path) == self.markers.path, "ownership")
                descriptor = os.open(os.path.basename(path), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
                try:
                    info = os.fstat(descriptor)
                    mode = stat.S_IMODE(info.st_mode)
                    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == info.st_gid == custody.ROOT_UID
                            and mode & ~0o644 == 0 and 0 < info.st_size <= custody.MAX_MATERIAL, "ownership")
                    if tighten and mode != 0o600:
                        os.fchmod(descriptor, 0o600)
                        os.fsync(descriptor)
                finally:
                    os.close(descriptor)

    def capture_bootstrap_outputs(self):
        """An owned attempted exact seed proves the producer before private reads."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        unused, staging = tunnel_helpers()
        scope = self.recovery_scope()
        require(canonical(self.markers.read("bootstrap-attempt.json")) == canonical(scope), "ownership")
        staging.prove_seed_daemons(self.request, self.daemons())
        self.bootstrap_outputs(tighten=self.markers.read("bootstrap-output-source.json") is None)
        with contextlib.ExitStack() as stack:
            handles = {name: stack.enter_context(self.native_source(path, root_only=True)) for name, path in self.bootstrap_sources().items()}
            files = {name: custody.metadata(os.fstat(handle.fileno())) for name, handle in handles.items()}
            for value in files.values():
                custody.valid_metadata(value, uid=custody.ROOT_UID)
            record = {"scope": scope, "files": files}
            previous = self.markers.read("bootstrap-output-source.json")
            require(previous is None or canonical(previous) == canonical(record), "ownership")
            self.markers.write("bootstrap-output-source.json", record)
            material = {name: handle.read(MAX_INPUT + 1) for name, handle in handles.items()}
            for name, handle in handles.items():
                require(custody.metadata(os.fstat(handle.fileno())) == files[name] and len(material[name]) == files[name]["size"], "ownership")
            custody.retain(self.markers, "bootstrap-original", scope, material)

    @contextlib.contextmanager
    def bootstrap_hold(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        scope = self.recovery_scope()
        source = self.markers.read("bootstrap-output-source.json")
        require(isinstance(source, dict) and set(source) == {"scope", "files"} and canonical(source["scope"]) == canonical(scope), "ownership")
        require(isinstance(source["files"], dict) and set(source["files"]) == {"config", "keyring"}, "ownership")
        # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with custody.hold(self.markers, "bootstrap-original", scope) as (unused, material), contextlib.ExitStack() as stack:
            handles = {}
            for name, path in self.bootstrap_sources().items():
                handle = stack.enter_context(self.native_source(path, root_only=True))
                custody.prove(handle, source["files"][name], uid=custody.ROOT_UID)
                require(handle.read(MAX_INPUT + 1) == material[name], "ownership")
                custody.prove(handle, source["files"][name], uid=custody.ROOT_UID)
                handles[name] = handle
            yield material
            for name, handle in handles.items():
                custody.prove(handle, source["files"][name], uid=custody.ROOT_UID)
                require(custody.metadata(os.stat(self.bootstrap_sources()[name], follow_symlinks=False)) == source["files"][name], "ownership")
                handle.seek(0)
                require(handle.read(MAX_INPUT + 1) == material[name], "ownership")

    def config_database(self):
        """Prove persisted native options and refuse competing scopes/masks."""
        config = self.request.get("config")
        if config is None:
            return
        values = {name: (section, value) for section, options in config.items() for name, value in options.items()}
        observed = self.ceph("config", "dump")
        require(isinstance(observed, list), "native-output")
        selected = {}
        for entry in observed:
            require(isinstance(entry, dict), "native-output")
            name = entry.get("name")
            if name not in values:
                continue
            section, value = values[name]
            require(entry.get("section") == section and entry.get("mask") == "", "config-drift")
            require(not any(field in entry for field in ("location_type", "location_value", "device_class")), "config-drift")
            require(name not in selected and entry.get("value") == value, "config-drift")
            selected[name] = (section, entry["value"])
        require(selected == values, "config-drift")

    def config_effective(self, identifier):
        """Check provider-reported running OSD values without JSON fiction.

        Exact IBM commands return primitive text for a single-key config show;
        requesting JSON would prepend a blank line in the native Python CLI.
        Values come from the manager's daemon reports, not a process RSS cap.
        """
        if "config" not in self.request:
            return
        require(type(identifier) is int and identifier >= 0, "native-output")
        for name, value in sorted(self.request["config"].get("osd", {}).items()):
            observed = self.ceph_text("config", "show", "osd." + str(identifier), name)
            require(observed == (value + "\n").encode(), "config-drift")

    def call_home_state(self):
        """A completion record never replaces current native module proof."""
        opt_in = self.request["callHome"] != "enabled" and self.selection.variant("ceph.call-home-model") == "opt-in"
        if self.request["callHome"] != "enabled" and not opt_in:
            return
        modules = self.ceph("mgr", "module", "ls")
        require(isinstance(modules, dict), "native-output")
        selected = {}
        for field in ("enabled_modules", "always_on_modules", "force_disabled_modules"):
            value = modules.get(field)
            require(isinstance(value, list) and len(value) <= 128 and all(named(name) for name in value), "native-output")
            require(len(set(value)) == len(value), "native-output")
            selected[field] = set(value)
        enabled = selected["enabled_modules"] | selected["always_on_modules"]
        if opt_in:
            require("call_home_agent" not in enabled, "call-home-drift")
            return
        require("call_home_agent" in enabled and "call_home_agent" not in selected["force_disabled_modules"], "call-home-drift")

    def decided_package(self, name):
        """The installed identity the host's completed package decision froze for one Ceph root, as rpm prints PACKAGE_QUERY."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        packages = self.packages or Markers(self.request, root=runtime.PACKAGE_ROOT)
        decision, complete = packages.read("decision.json"), packages.read("complete.json")
        try:
            plan = dict(decision["plan"])
            planned = plan.pop("digest")
            require(decision["version"] == "ceph-package-decision-v1" and digest(planned) and hashlib.sha256(canonical(plan)).hexdigest() == planned
                    and complete == {"version": "ceph-package-complete-v1", "binding": decision["binding"], "planDigest": planned}, "artifact")
            roots = [root for root in plan["roots"] if root["key"] == "ceph" and root["package"]["name"] == name]
            require(len(roots) == 1 and roots[0]["requested"] == self.request["cephadm"]["packageVersion"], "artifact")
            package = roots[0]["package"]
            fields = (package["epoch"], package["version"], package["release"], package["architecture"])
            require(type(fields[0]) is int and fields[0] >= 0
                    and all(isinstance(value, str) and re.fullmatch(r"[!-~]{1,128}", value) for value in fields[1:]), "artifact")
        except (TypeError, KeyError, AttributeError, ValueError):
            raise Refusal("artifact") from None
        return "{} {} {} {}".format(*fields).encode("ascii")

    def verify_tool(self):
        content = read_regular(self.tool, maximum=16 * 1024 * 1024)
        installed = self.command(["/usr/bin/rpm", "-q", "--qf", PACKAGE_QUERY, "cephadm"], json_result=False)
        require(installed == self.decided_package("cephadm"), "artifact")
        self.command(["/usr/bin/rpm", "--verify", "--noscripts", "cephadm"], json_result=False)
        clients = self.command(["/usr/bin/rpm", "-q", "--qf", PACKAGE_QUERY, "ceph-common"], json_result=False)
        require(clients == self.decided_package("ceph-common"), "artifact")
        self.command(["/usr/bin/rpm", "--verify", "--noscripts", "ceph-common"], json_result=False)
        binaries = {path: hashlib.sha256(read_regular(path, maximum=64 * 1024 * 1024)).hexdigest()
                    for path in ("/usr/bin/ceph", "/usr/bin/ceph-authtool", "/usr/bin/rbd", "/usr/bin/rados")}
        identity = {"sha256": hashlib.sha256(content).hexdigest(), "packageVersion": self.request["cephadm"]["packageVersion"], "clients": binaries}
        recorded = self.markers.read("artifact.json")
        require(recorded is not None or not self.markers.owned(), "artifact")
        require(recorded is None or recorded == identity, "artifact")
        self._tool_bytes = content
        return identity

    def verified_tool(self):
        """The host cephadm bytes verify_tool proved in this worker run."""
        if self._tool_bytes is None:
            self.verify_tool()
        return self._tool_bytes

    def image_payload(self):
        """The image's cephadm payload, read only after the runtime profile binds it to the held image."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        self.runtime_profile()
        return runtime.cephadm_payload(self.host_preparation())

    def require_call_home_path(self):
        model = self.selection.variant("ceph.call-home-model")
        require(self.request["callHome"] == ("enabled" if model == "opt-out" else "disabled"), "call-home-unqualified")

    def call_home_applicable(self):
        """Opt-in Call Home left disabled has no step; every other path keeps the enabled-only guard."""
        if self.request["callHome"] == "disabled" and self.selection.variant("ceph.call-home-model") == "opt-in":
            return False
        require(self.request["callHome"] == "enabled", "call-home-unqualified")
        return True

    def require_declared_options(self, argv):
        """An anchored host parser must declare every option; an unanchored one leaves cephadm's argparse to judge."""
        names = self.selection.declared("host-cephadm")
        if names is None:
            return
        missing = sorted({token for token in argv if token.startswith("--") and token not in names})
        if missing:
            raise NativeFailure("native-option-undeclared", self.native_coordinate(), ", ".join(missing))

    def daemons(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observe
        self.verify_tool()
        try:
            daemons = ceph_native_observe.daemons(self.request, self.observed_manager_uid(), self.command, owned=self.markers.owned(),
                                                  cache_uid=self.observer_cache_uid(), clock=self.clock, pause=self.pause, host=self.host["name"])
        except ceph_native_observe.ObservationChangedRefusal:
            # cephadm kept moving this host's layout through every bounded snapshot: unfinished, never foreign.
            raise Refusal("not-ready") from None
        except ceph_native_observe.ObservationRefusal:
            raise Refusal("native-observation-unproved") from None
        require(isinstance(daemons, list) and len(daemons) <= 1024, "native-output")
        for daemon in daemons:
            require(isinstance(daemon, dict) and daemon.get("fsid") == self.request["fsid"], "foreign-cluster")
        return daemons

    def observer_cache_uid(self):
        """Lend only the account retained by our original SSH authorization."""
        value = self.markers.read("ssh-authorization.json")
        if value is None:
            return None
        helper, unused = tunnel_helpers()
        require(self.markers.owned() and isinstance(value, dict) and set(value) == {"uid", "home", "line"}, "ownership")
        account = pwd.getpwnam(self.request["clusterSSHUser"])
        require(type(value["uid"]) is int and value["uid"] == account.pw_uid > 0 and value["home"] == account.pw_dir, "ownership")
        require(isinstance(value["line"], str) and len(value["line"].split()) == 4, "ownership")
        public = " ".join(value["line"].split()[1:3])
        require(value["line"] == helper.authorization_line(public, self.request["owner"]), "ownership")
        return value["uid"]

    def host_state(self):
        prove_fips(self.request)
        owned = self.markers.owned()
        daemons = self.daemons()
        exists = bool(daemons)
        require(owned or not exists, "foreign-cluster")
        physical = {}
        if owned and exists:
            self.prove_registry()
            physical = self.physical_devices()
        if owned and self.recovery_enabled() and self.host["name"] == self.request["bootstrap"] and self.markers.read("bootstrap-enrollment.json") is None:
            self.bootstrap_state()
        elif owned and self.request["clusterSSHTransport"] == "verified-tunnel" and any(str(d.get("name", "")).startswith("mgr.") for d in daemons):
            self.verify_transport_bundle()
            pending = self.verify_manager_mounts(daemons, observing=True)
            if pending:
                return {"name": self.host["name"], "owned": owned, "exists": exists, "pending": pending}
        if owned and any(str(d.get("name", "")).startswith("osd.") for d in daemons):
            self.prove_completed_osd_variants(physical)
        return {"name": self.host["name"], "owned": owned, "exists": exists}

    def prove_completed_osd_variants(self, physical=None):
        services = [s for s in self.request["services"] if s["service_type"] == "osd" and self.host["name"] in s["placement"]["hosts"]
                    and any(s["spec"].get(k) for k in ("encrypted", "tpm2", "db_devices", "wal_devices"))]
        if not services:
            return
        require(self.markers.owned() and self.markers.read("osd-device-intent.json") == self.osd_device_scope(), "ownership")
        physical = self.physical_devices() if physical is None else physical
        observed = self.command(["/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "PATH,TYPE,SERIAL,SIZE,FSTYPE,MOUNTPOINTS"])
        output = self.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(observed, dict) and isinstance(observed.get("blockdevices"), list), "native-output")
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= 1024, "native-output")
        for service in services:
            spec, allocations = service["spec"], {}
            for field, role in (("data_devices", "block"), ("db_devices", "db"), ("wal_devices", "wal")):
                for path in spec.get(field, {}).get("paths", []):
                    device = physical.get(path) or next(d for d in self.host["devices"] if d["path"] == path)
                    disks = [d for d in observed["blockdevices"] if d.get("path") == os.path.realpath(path)]
                    require(len(disks) == 1, "device-identity")
                    disk = disks[0]
                    require(disk.get("type") == "disk" and disk.get("serial") == device["serial"]
                            and disk.get("size") == device["sizeBytes"] and not any(disk.get("mountpoints") or []), "device-identity")
                    selected = []
                    for row in rows:
                        require(isinstance(row, dict) and isinstance(row.get("devices"), str), "native-output")
                        paths = [p.strip().split("(", 1)[0] for p in row["devices"].split(",")]
                        if os.path.realpath(path) not in paths:
                            continue
                        require(paths == [os.path.realpath(path)] and isinstance(row.get("lv_tags"), str), "foreign-osd")
                        tags = {}
                        for tag in row["lv_tags"].split(","):
                            key, separator, value = tag.strip().partition("=")
                            require(separator and key not in tags, "foreign-osd")
                            tags[key] = value
                        require(tags.get("ceph.cluster_fsid") == self.request["fsid"] and tags.get("ceph.type") == role, "foreign-osd")
                        require(re.fullmatch(r"[0-9]+", tags.get("ceph.osd_id", "")) is not None
                                and re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", tags.get("ceph.osd_fsid", "")), "foreign-osd")
                        require(isinstance(row.get("lv_uuid"), str) and row["lv_uuid"] and isinstance(row.get("lv_path"), str), "foreign-osd")
                        self.prove_osd_crypto(row, tags, disk.get("children") or [], spec)
                        if role == "block":
                            retained = self.markers.read(self.osd_identity_marker(os.path.realpath(path)))
                            identity = {"path": os.path.realpath(path), "id": int(tags["ceph.osd_id"]),
                                        "uuid": tags["ceph.osd_fsid"], "lvUUID": row["lv_uuid"]}
                            require(retained is None or retained == identity, "foreign-osd")
                        selected.append((row, tags))
                    require(selected and (role != "block" or len(selected) == 1), "foreign-osd")
                    require(len(disk.get("children") or []) == len(selected), "foreign-osd")
                    allocations.setdefault(role, []).extend(selected)
            blocks = allocations["block"]
            require(len({t["ceph.osd_id"] for r, t in blocks}) == len(blocks), "foreign-osd")
            for role in ("db", "wal"):
                auxiliary = allocations.get(role, [])
                require(len({r["lv_uuid"] for r, t in auxiliary}) == len(auxiliary), "foreign-osd")
                for row, tags in auxiliary:
                    matches = [t for r, t in blocks if t["ceph.osd_id"] == tags["ceph.osd_id"]
                               and t["ceph.osd_fsid"] == tags["ceph.osd_fsid"] and t.get("ceph." + role + "_uuid") == row["lv_uuid"]]
                    require(len(matches) == 1, "foreign-osd")
                if role in allocations:
                    require(len(auxiliary) == len(blocks), "foreign-osd")

    def prove_osd_crypto(self, row, tags, children, spec):
        require(tags.get("ceph.encrypted", "0") == str(int(spec.get("encrypted", False)))
                and tags.get("ceph.with_tpm", "0") == str(int(spec.get("tpm2", False))), "foreign-osd")
        matches = [c for c in children if os.path.realpath(c.get("path", "")) == os.path.realpath(row["lv_path"])]
        require(len(matches) == 1 and matches[0].get("type") == "lvm" and not any(matches[0].get("mountpoints") or []), "foreign-osd")
        volume = matches[0]
        if spec.get("encrypted"):
            mappings = volume.get("children") or []
            require(volume.get("fstype") == "crypto_LUKS" and len(mappings) == 1, "foreign-osd")
            volume = mappings[0]
            require(volume.get("type") == "crypt"
                    and os.path.realpath(volume.get("path", "")) == os.path.realpath("/dev/mapper/" + row["lv_uuid"]), "foreign-osd")
            if spec.get("tpm2"):
                metadata = self.command(["/usr/sbin/cryptsetup", "luksDump", row["lv_path"]], json_result=False)
                require(isinstance(metadata, bytes) and len(metadata) <= 128 << 10, "native-output")
                tokens = metadata.split(b"Tokens:\n")
                require(len(tokens) == 2 and re.search(rb"(?m)^\s+[0-9]+: systemd-tpm2\s*$", tokens[1].split(b"Digests:", 1)[0]), "foreign-osd")
        require(not volume.get("children") and not any(volume.get("mountpoints") or [])
                and volume.get("fstype") in (None, "", "ceph_bluestore"), "foreign-osd")

    def prerequisites(self):
        prove_fips(self.request)
        hostname = self.command(["/usr/bin/hostname"], json_result=False).decode("ascii").strip()
        require(hostname == self.host["name"], "host-identity")
        self.verify_tool()
        for executable in ("/usr/bin/ceph", "/usr/bin/rbd", "/usr/bin/rados", "/usr/bin/podman", "/usr/sbin/lvm", "/usr/bin/systemctl"):
            require(os.path.isfile(executable) and os.access(executable, os.X_OK), "host-prerequisite")
        synchronized = self.command(["/usr/bin/timedatectl", "show", "--property=NTPSynchronized", "--value"], json_result=False)
        require(synchronized.strip() == b"yes", "host-time")
        result = {"name": self.host["name"], "owned": self.markers.owned(), "exists": bool(self.daemons())}
        if self.request["clusterSSHTransport"] == "verified-tunnel" and self.request["action"] == "cluster":
            result["recovery"] = self.preparation_state()
        return result

    def claim(self):
        prove_fips(self.request)
        existing = self.daemons()
        owned = self.markers.owned()
        require(owned or not existing, "foreign-cluster")
        identity = self.verify_tool()
        if not owned:
            self.require_call_home_path()
        self.markers.create()
        self.markers.write("artifact.json", identity)
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            unused, staging = tunnel_helpers()
            self.markers.write("bootstrap-scope.json", staging.recovery_scope(self.request))
        return {"changed": not owned}

    def prove_authorization(self):
        """Read the owned exact key line without rewriting an operator account."""
        helper, unused = tunnel_helpers()
        value = self.markers.read("ssh-authorization.json")
        require(isinstance(value, dict) and set(value) == {"uid", "home", "line"}, "ownership")
        account = pwd.getpwnam(self.request["clusterSSHUser"])
        require(type(value["uid"]) is int and value["uid"] == account.pw_uid > 0 and value["home"] == account.pw_dir, "ownership")
        require(isinstance(value["line"], str) and len(value["line"].split()) == 4, "ownership")
        public = " ".join(value["line"].split()[1:3])
        require(value["line"] == helper.authorization_line(public, self.request["owner"]), "ownership")
        with self.native_source(os.path.join(value["home"], ".ssh", "authorized_keys"), private=False, allowed_uid=account.pw_uid) as handle:
            content = handle.read(MAX_INPUT + 1)
            lines = content.decode("utf-8").splitlines()
            require([line for line in lines if public.split()[1] in line.split()] == [value["line"]], "ownership")

    def prove_preparation(self):
        scope = self.recovery_scope()
        require(canonical(self.markers.read("bootstrap-prepared.json")) == canonical(scope), "ownership")
        self.prove_registry()
        self.prove_authorization()
        self.prove_tunnel_policy()
        managers = next(service["placement"]["hosts"] for service in self.request["services"] if service["service_type"] == "mgr")
        if self.host["name"] in managers:
            self.verify_transport_bundle()

    def authorization_result(self, changed):
        if self.recovery_enabled():
            self.markers.write("bootstrap-prepared.json", self.recovery_scope())
            self.prove_preparation()
        return {"changed": changed}

    def preparation_state(self):
        unused, staging = tunnel_helpers()
        owned, daemons = self.markers.owned(), self.daemons()
        require(owned or not daemons, "foreign-cluster")
        if not owned:
            require(self.markers.read("bootstrap-scope.json") is None and self.markers.read("bootstrap-protocol.json") is None, "ownership")
            stage = "unclaimed"
        else:
            self.recovery_scope()
            if daemons:
                stage = "complete"
            elif self.markers.read("bootstrap-prepared.json") is None:
                stage = "claimed"
            else:
                self.prove_preparation()
                stage = "prepared"
        return staging.recovery_record(self.request, self.host["name"], stage)

    def registry_intent(self):
        token = hashlib.sha256((self.request["owner"] + "\x00" + self.request["fsid"]).encode()).hexdigest()
        return {"version": "ceph-registry-v1", "owner": self.request["owner"], "fsid": self.request["fsid"],
                "host": self.host["name"], "registry": registry_authority(self.request), "path": REGISTRY_FILE,
                "anchor": ".bootwright-registry-" + token}

    def registry_preflight(self, wanted):
        """No mutation or adoption before foreign account and file checks pass."""
        require(self.markers.owned(), "ownership")
        registry_scope(self.request, wanted)
        intent = self.registry_intent()
        recorded = self.markers.read("registry-intent.json")
        pending = self.markers.read("registry-pending.json")
        ready = self.markers.read("registry-file.json")
        require(recorded is None or recorded == intent, "registry-ownership")
        require(self.markers.read("registry-removed.json") is None, "registry-ownership")
        for record, is_pending in ((pending, True), (ready, False)):
            require(record is None or isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent, "registry-ownership")
            if record is not None:
                registry_metadata_record(record["metadata"], pending=is_pending)
        require(pending is None or recorded == intent, "registry-ownership")
        require(ready is None or pending is not None, "registry-ownership")
        with registry_directory() as directory:
            if pending is None:
                # An unbound foreign name authorizes no credential read.
                require(not registry_exists(directory, REGISTRY_NAME) and not registry_exists(directory, intent["anchor"]), "registry-ownership")
                return intent, pending, ready
            if ready is not None:
                require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
                final = registry_read(directory, REGISTRY_NAME, ready["metadata"])
                anchor = None
            else:
                final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True, links=(1, 2))
                anchor = registry_read(directory, intent["anchor"], pending["metadata"], pending=True, links=(1, 2))
            if ready is not None:
                require(final is not None and anchor is None, "registry-ownership")
                require(registry_metadata(final[0]) == ready["metadata"] and registry_metadata(final[0], pending=True) == pending["metadata"],
                        "registry-ownership")
                require(final[1] == wanted, "registry-ownership")
            elif pending is not None:
                require(final is not None or anchor is not None, "registry-ownership")
                for item in (final, anchor):
                    if item is not None:
                        require(registry_metadata(item[0], pending=True) == pending["metadata"] and item[1] == wanted, "registry-ownership")
                require((final is None or anchor is None) and (final or anchor)[0].st_nlink == 1 or
                        final is not None and anchor is not None and final[0].st_nlink == anchor[0].st_nlink == 2, "registry-ownership")
        return intent, pending, ready

    def prepare_registry(self, wanted):
        """Publish the native authfile from stdin using a prebound private inode.

        Once the pending binding is durable, every interruption can be resumed
        only with that inode and exact protected bytes. No secret-derived hash
        enters any journal. An unbound private anchor is never adopted.
        """
        intent, pending, ready = self.registry_preflight(wanted)
        if ready is not None:
            self.prove_registry(wanted)
            return False
        self.markers.write("registry-intent.json", intent)
        with registry_directory(create=True) as directory:
            if pending is None:
                fd = os.open(intent["anchor"], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
                created = os.fstat(fd)
                bound = False
                try:
                    with os.fdopen(fd, "wb") as handle:
                        os.fchmod(handle.fileno(), 0o600)
                        os.fchown(handle.fileno(), 0, 0)
                        handle.write(wanted)
                        handle.flush()
                        os.fsync(handle.fileno())
                        complete = registry_metadata(os.fstat(handle.fileno()))
                    os.fsync(directory)
                    item = registry_read(directory, intent["anchor"], complete)
                    require(item is not None and item[0].st_dev == created.st_dev and item[0].st_ino == created.st_ino and item[1] == wanted,
                            "registry-ownership")
                    pending = {"intent": intent, "metadata": registry_metadata(item[0], pending=True)}
                    self.markers.write("registry-pending.json", pending)
                    # A journal write may fail after its durable publication.
                    # Re-read it before deciding whether cleanup may unlink.
                    require(self.markers.read("registry-pending.json") == pending, "registry-ownership")
                    bound = True
                finally:
                    durable = self.markers.read("registry-pending.json")
                    if not bound and durable is None:
                        observed = os.stat(intent["anchor"], dir_fd=directory, follow_symlinks=False)
                        require(observed.st_dev == created.st_dev and observed.st_ino == created.st_ino and observed.st_nlink == 1, "registry-ownership")
                        os.unlink(intent["anchor"], dir_fd=directory)
                        os.fsync(directory)
            # Reprove the exact reserved inode and bytes after its journal is
            # durable, before linking it to the provider's fixed global name.
            self.registry_preflight(wanted)
            final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True, links=(1, 2))
            anchor = registry_read(directory, intent["anchor"], pending["metadata"], pending=True, links=(1, 2))
            if final is None:
                require(anchor is not None and registry_metadata(anchor[0], pending=True) == pending["metadata"], "registry-ownership")
                os.link(intent["anchor"], REGISTRY_NAME, src_dir_fd=directory, dst_dir_fd=directory, follow_symlinks=False)
                os.fsync(directory)
            if anchor is not None:
                self.registry_preflight(wanted)
                os.unlink(intent["anchor"], dir_fd=directory)
                os.fsync(directory)
            final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True)
            require(final is not None and registry_metadata(final[0], pending=True) == pending["metadata"] and final[1] == wanted, "registry-ownership")
            self.markers.write("registry-file.json", {"intent": intent, "metadata": registry_metadata(final[0])})
        self.prove_registry(wanted)
        return True

    def prove_registry(self, wanted=None):
        require(self.markers.owned(), "ownership")
        intent = self.registry_intent()
        require(self.markers.read("registry-intent.json") == intent and self.markers.read("registry-removed.json") is None, "registry-ownership")
        record = self.markers.read("registry-file.json")
        pending = self.markers.read("registry-pending.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent, "registry-ownership")
        require(isinstance(pending, dict) and set(pending) == {"intent", "metadata"} and pending["intent"] == intent, "registry-ownership")
        registry_metadata_record(record["metadata"])
        registry_metadata_record(pending["metadata"], pending=True)
        with registry_directory() as directory:
            require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None, "registry-ownership")
            require(registry_metadata(item[0]) == record["metadata"] and registry_metadata(item[0], pending=True) == pending["metadata"], "registry-ownership")
            registry_scope(self.request, item[1])
            require(wanted is None or item[1] == wanted, "registry-ownership")

    def provider_registry_absent(self):
        # Reading names avoids disclosing values. Any stored credential would
        # re-enable provider registry-login/password argv during remote pulls.
        keys = self.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 4096 and all(isinstance(key, str) and len(key) <= 4096 for key in keys), "native-output")
        require(len(set(keys)) == len(keys) and "mgr/cephadm/registry_credentials" not in keys, "registry-credentials-present")

    def remove_registry(self):
        """Internal inverse remains unreachable from the closed destroy phase."""
        proof = {"version": "ceph-native-teardown-v1", "owner": self.request["owner"], "fsid": self.request["fsid"],
                 "host": self.host["name"], "removed": True}
        teardown = self.markers.read("native-teardown.json")
        require(self.markers.owned() and isinstance(teardown, dict) and teardown == proof and type(teardown.get("removed")) is bool and not self.daemons(),
                "registry-teardown-unproved")
        record = self.markers.read("registry-file.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == self.registry_intent(), "registry-ownership")
        registry_metadata_record(record["metadata"])
        pending = self.markers.read("registry-pending.json")
        require(self.markers.read("registry-intent.json") == record["intent"] and isinstance(pending, dict) and set(pending) == {"intent", "metadata"}
                and pending["intent"] == record["intent"], "registry-ownership")
        registry_metadata_record(pending["metadata"], pending=True)
        require(pending["metadata"] == {key: value for key, value in record["metadata"].items() if key != "ctimeNS"}, "registry-ownership")
        removing, removed = self.markers.read("registry-removal.json"), self.markers.read("registry-removed.json")
        for value in (removing, removed):
            require(value is None or isinstance(value, dict) and set(value) == {"intent", "metadata"} and value["intent"] == record["intent"],
                    "registry-ownership")
            if value is not None:
                registry_metadata_record(value["metadata"])
                require(value == record, "registry-ownership")
        require(removed is None or removed == record and removing == record, "registry-ownership")
        if removing is not None:
            with registry_directory() as directory:
                require(not registry_exists(directory, record["intent"]["anchor"]), "registry-ownership")
                if not registry_exists(directory, REGISTRY_NAME):
                    self.markers.write("registry-removed.json", record)
                    return removed is None
            require(removed is None, "registry-ownership")
        self.prove_registry()
        self.markers.write("registry-removal.json", record)
        with registry_directory() as directory:
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None and registry_metadata(item[0]) == record["metadata"], "registry-ownership")
            registry_scope(self.request, item[1])
            os.unlink(REGISTRY_NAME, dir_fd=directory)
            os.fsync(directory)
        self.markers.write("registry-removed.json", record)
        return True

    def osd_device_scope(self):
        return {"version": "ceph-osd-device-intent-v1", "owner": self.request["owner"], "fsid": self.request["fsid"],
                "host": self.host["name"], "devices": self.host["devices"], "image": self.request["image"]}

    def owned_osd_device(self, actual, item, signatures):
        require(self.markers.owned() and self.markers.read("osd-device-intent.json") == self.osd_device_scope(), "device-not-empty")
        require(item.get("fstype") in (None, "", "LVM2_member") and not any(item.get("mountpoints") or []), "device-not-empty")
        require(all(isinstance(value, dict) and value.get("type") == "LVM2_member" for value in signatures), "device-not-empty")
        output = self.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= 1024, "native-output")
        candidates = []
        for row in rows:
            require(isinstance(row, dict) and isinstance(row.get("devices"), str), "native-output")
            paths = [part.strip().split("(", 1)[0] for part in row["devices"].split(",")]
            if actual not in paths:
                continue
            require(paths == [actual] and isinstance(row.get("lv_tags"), str), "foreign-osd")
            tags = {}
            for tag in row["lv_tags"].split(","):
                key, separator, value = tag.strip().partition("=")
                require(separator and key not in tags, "foreign-osd")
                tags[key] = value
            require(tags.get("ceph.cluster_fsid") == self.request["fsid"] and tags.get("ceph.type") == "block", "foreign-osd")
            require(re.fullmatch(r"[0-9]+", tags.get("ceph.osd_id", "")) is not None
                    and re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", tags.get("ceph.osd_fsid", "")), "foreign-osd")
            require(isinstance(row.get("lv_uuid"), str) and row["lv_uuid"] and isinstance(row.get("lv_path"), str), "foreign-osd")
            children = item.get("children") or []
            specs = [s["spec"] for s in self.request["services"] if s["service_type"] == "osd"
                     and self.host["name"] in s["placement"]["hosts"] and any(os.path.realpath(path) == actual for path in s["spec"]["data_devices"]["paths"])]
            require(len(specs) == 1 and len(children) == 1, "foreign-osd")
            self.prove_osd_crypto(row, tags, children, specs[0])
            candidates.append({"path": actual, "id": int(tags["ceph.osd_id"]), "uuid": tags["ceph.osd_fsid"], "lvUUID": row["lv_uuid"]})
        require(len(candidates) == 1, "foreign-osd")
        retained = self.markers.read(self.osd_identity_marker(actual))
        require(retained is None or retained == candidates[0], "foreign-osd")
        return candidates[0]

    @staticmethod
    def osd_identity_marker(path):
        return "osd-device-" + hashlib.sha256(path.encode()).hexdigest() + ".json"

    def stable_device_path(self, host, device, *, submission=False):
        """Only the authored immutable physical WWN or qualified QEMU disk identities."""
        if host.get("physical"):
            require(device.get("wwn") and device["path"] == "/dev/disk/by-id/wwn-" + device["wwn"], "device-identity")
            return device["path"]
        if re.fullmatch(r"/dev/vd[a-z]+", device["path"]):
            return "/dev/disk/by-id/virtio-" + device["serial"]
        require(re.fullmatch(r"/dev/sd[a-z]+", device["path"]) is not None, "device-identity")
        identifier = "QEMU_QEMU_HARDDISK_" + device["serial"]
        if submission:
            identities = self.device_identities()
            require(identities is not None and identities.get((host["name"], device["path"])) == identifier, "device-identity")
        return "/dev/disk/by-id/scsi-0" + identifier

    def physical_devices(self):
        if not self.host.get("physical"):
            return {}
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
        reader = ceph_host_runtime.HostRuntime(self.request, self.host["name"])
        return {value["path"]: value for value in reader.host_devices()["devices"]}

    def devices(self):
        physical = self.physical_devices()
        self.observed_device_ids = []
        if self.markers.owned() and self.markers.read("registry-file.json") is not None:
            self.prove_registry()
        observed = self.command(["/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "PATH,TYPE,SERIAL,SIZE,FSTYPE,MOUNTPOINTS"])
        require(isinstance(observed, dict) and isinstance(observed.get("blockdevices"), list), "device-unproved")
        flattened = []

        def visit(items):
            require(isinstance(items, list) and len(flattened) + len(items) <= 4096, "device-unproved")
            for item in items:
                require(isinstance(item, dict), "device-unproved")
                flattened.append(item)
                visit(item.get("children", []))

        visit(observed["blockdevices"])
        resolved = set()
        for frozen_device in self.host["devices"]:
            device = physical.get(frozen_device["path"], frozen_device)
            actual = os.path.realpath(device["path"])
            require(actual not in resolved, "device-identity")
            resolved.add(actual)
            alias = self.stable_device_path(self.host, device)
            require(os.path.realpath(alias) == actual, "device-identity")
            matches = [value for value in flattened if value.get("path") == actual]
            require(len(matches) == 1, "device-unproved")
            item = matches[0]
            require(item.get("type") == "disk" and str(item.get("serial", "")).strip() == device["serial"]
                    and item.get("size") == device["sizeBytes"], "device-identity")
            if alias.startswith("/dev/disk/by-id/scsi-"):
                from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
                raw = self.command(["/usr/bin/udevadm", "info", "--query=property", "--name=" + actual], json_result=False)
                require(isinstance(raw, bytes) and 0 < len(raw) <= 128 << 10, "device-identity")
                values = {}
                for line in raw.splitlines():
                    require(b"=" in line and len(line) <= 4096, "device-identity")
                    name, value = line.split(b"=", 1)
                    if name in (b"ID_SERIAL", b"ID_SCSI_SERIAL", b"ID_VENDOR", b"ID_MODEL"):
                        require(name not in values and all(32 <= byte <= 126 for byte in value), "device-identity")
                        values[name] = value
                environment = {name.decode("ascii"): value.decode("ascii") for name, value in values.items()}
                require(environment.get("ID_SCSI_SERIAL") == device["serial"]
                        and environment.get("ID_SERIAL") == "0QEMU_QEMU_HARDDISK_" + device["serial"]
                        and runtime.native_form(environment) == "QEMU_QEMU_HARDDISK_" + device["serial"]
                        and os.path.realpath(alias) == actual, "device-identity")
            signatures = self.command(["/usr/sbin/wipefs", "--no-act", "--json", actual])
            require(isinstance(signatures, dict) and isinstance(signatures.get("signatures"), list), "device-not-empty")
            if item.get("children") or item.get("fstype") or any(item.get("mountpoints") or []) or signatures["signatures"]:
                require(device["path"] not in auxiliary_device_paths(self.request, self.host["name"]), "device-not-empty")
                self.observed_device_ids.append(self.owned_osd_device(actual, item, signatures["signatures"]))
            else:
                require(self.markers.read(self.osd_identity_marker(actual)) is None, "foreign-osd")
        return {"devices": len(self.host["devices"])}

    def osd_prepare(self):
        require(self.markers.owned(), "ownership")
        scope = self.osd_device_scope()
        retained = self.markers.read("osd-device-intent.json")
        require(retained is None or retained == scope, "ownership")
        self.devices()
        if retained is None:
            self.markers.write("osd-device-intent.json", scope)
        changed = retained is None
        for row in self.observed_device_ids:
            marker = self.osd_identity_marker(row["path"])
            previous = self.markers.read(marker)
            require(previous is None or previous == row, "foreign-osd")
            if previous is None:
                self.markers.write(marker, row)
                changed = True
        return {"changed": changed}

    def authorize(self, private):
        """Add only the separately owned cluster key to an existing account."""
        require(self.markers.owned(), "ownership")
        expected = {"clusterPublicKey", "registryJSON"}
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            expected |= {"clusterPrivateKey", "knownHosts"}
        require(set(private) == expected, "material")
        self.tunnel_host_key_preflight(private)
        registry = registry_material(self.request, private["registryJSON"])
        public = private["clusterPublicKey"]
        require(isinstance(public, str) and len(public) <= 32768 and len(public.strip().splitlines()) == 1, "material")
        parts = public.split()
        require(len(parts) >= 2 and parts[0] in ("ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"), "material")
        try:
            blob = base64.b64decode(parts[1], validate=True)
            length = int.from_bytes(blob[:4], "big")
            require(blob[4:4 + length].decode("ascii") == parts[0], "material")
        except (ValueError, UnicodeError):
            raise Refusal("material") from None
        account = pwd.getpwnam(self.request["clusterSSHUser"])
        require(account.pw_uid > 0 and os.path.isabs(account.pw_dir), "cluster-account")
        line = "restrict " + parts[0] + " " + parts[1] + " bootwright:" + self.request["owner"]
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            helper, unused = tunnel_helpers()
            line = helper.authorization_line(public, self.request["owner"])
        identity = {"uid": account.pw_uid, "home": account.pw_dir, "line": line}
        recorded = self.markers.read("ssh-authorization.json")
        require(recorded is None or recorded == identity, "ownership")
        home = os.open(account.pw_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        directory = None
        try:
            info = os.fstat(home)
            require(info.st_uid == account.pw_uid and info.st_mode & 0o022 == 0, "cluster-account")
            try:
                directory = os.open(".ssh", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=home)
            except FileNotFoundError:
                self.registry_preflight(registry)
                if self.request["clusterSSHTransport"] == "verified-tunnel":
                    self.tunnel_policy_preflight()
                # Ownership is durable before even this non-Ceph account effect.
                self.markers.write("ssh-authorization.json", identity)
                os.mkdir(".ssh", 0o700, dir_fd=home)
                os.chown(".ssh", account.pw_uid, account.pw_gid, dir_fd=home, follow_symlinks=False)
                directory = os.open(".ssh", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=home)
            info = os.fstat(directory)
            require(info.st_uid == account.pw_uid and info.st_mode & 0o077 == 0, "cluster-account")
            try:
                fd = os.open("authorized_keys", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            except FileNotFoundError:
                current = b""
            else:
                with os.fdopen(fd, "rb") as handle:
                    info = os.fstat(handle.fileno())
                    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid in (0, account.pw_uid) and info.st_mode & 0o022 == 0
                            and info.st_size <= MAX_OUTPUT, "cluster-account")
                    current = handle.read(MAX_OUTPUT + 1)
                    require(len(current) == info.st_size, "cluster-account")
            text = current.decode("utf-8")
            matching = [entry for entry in text.splitlines() if parts[1] in entry.split()]
            require(not matching or recorded == identity and matching == [line], "foreign-authorization")
            self.registry_preflight(registry)
            if self.request["clusterSSHTransport"] == "verified-tunnel":
                self.tunnel_policy_preflight()
            registry_changed = self.prepare_registry(registry)
            if self.request["clusterSSHTransport"] == "verified-tunnel":
                managers = next(s["placement"]["hosts"] for s in self.request["services"] if s["service_type"] == "mgr")
                if self.host["name"] in managers:
                    self.transport({name: private[name] for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")})
                self.tunnel_guest_policy()
            if matching:
                return self.authorization_result(registry_changed)
            self.markers.write("ssh-authorization.json", identity)
            contents = current + (b"\n" if current and not current.endswith(b"\n") else b"") + line.encode() + b"\n"
            stage = ".bootwright-" + uuid.uuid4().hex
            fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
            try:
                with os.fdopen(fd, "wb") as handle:
                    os.fchown(handle.fileno(), account.pw_uid, account.pw_gid)
                    handle.write(contents)
                    handle.flush()
                    os.fsync(handle.fileno())
                os.rename(stage, "authorized_keys", src_dir_fd=directory, dst_dir_fd=directory)
                os.fsync(directory)
            finally:
                with contextlib.suppress(FileNotFoundError):
                    os.unlink(stage, dir_fd=directory)
            return self.authorization_result(True)
        finally:
            if directory is not None:
                os.close(directory)
            os.close(home)

    def tunnel_guest_policy(self):
        helper, unused = tunnel_helpers()
        content = helper.guest_policy(self.request["clusterSSHUser"])
        token = hashlib.sha256((self.request["fsid"] + "\x00" + self.request["owner"]).encode()).hexdigest()
        name = "05-bootwright-ceph-" + token + ".conf"
        record = {"name": name, "sha256": hashlib.sha256(content).hexdigest()}
        owned = self.markers.read("sshd-policy.json")
        require(owned is None or owned == record, "ownership")
        directory = os.open("/etc/ssh/sshd_config.d", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(directory)
            require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "cluster-account")
            read_regular("/etc/ssh/sshd_config", maximum=256 * 1024)
            try:
                existing = read_regular("/etc/ssh/sshd_config.d/" + name)
            except FileNotFoundError:
                existing = None
            require(existing is None or owned == record and existing == content, "foreign-authorization")
            self.markers.write("sshd-policy.json", record)
            if existing is None:
                publish_regular(directory, name, content, 0)
            try:
                self.prove_tunnel_policy()
            except Exception:
                if existing is None:
                    os.unlink(name, dir_fd=directory)
                    os.fsync(directory)
                raise
            self.command(["/usr/bin/systemctl", "reload", "sshd.service"], json_result=False)
            self.prove_tunnel_policy()
        finally:
            os.close(directory)

    def prove_tunnel_policy(self):
        helper, unused = tunnel_helpers()
        self.tunnel_policy_preflight()
        for client, address in sorted({(h["name"], h["address"]) for h in self.request["hosts"]} | {("localhost", "127.0.0.1")}):
            context = ("user=" + self.request["clusterSSHUser"] + ",host=" + client + ",addr=" + address +
                       ",laddr=" + self.host["address"] + ",lport=" + str(self.host["port"]))
            helper.verify_guest_policy(self.command(["/usr/sbin/sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", context], json_result=False))

    def tunnel_policy_preflight(self):
        helper, staging = tunnel_helpers()
        policy = helper.guest_policy(self.request["clusterSSHUser"])
        owned = self.markers.read("sshd-policy.json")
        token = hashlib.sha256((self.request["fsid"] + "\x00" + self.request["owner"]).encode()).hexdigest()
        expected = {"name": "05-bootwright-ceph-" + token + ".conf", "sha256": hashlib.sha256(policy).hexdigest()}
        require(owned is None or owned == expected, "ownership")
        pending = ["/etc/ssh/sshd_config"]
        seen, total = set(), 0
        while pending:
            path = pending.pop()
            require(path not in seen and len(seen) < 128, "cluster-account")
            seen.add(path)
            content = self.sshd_policy_file(path)
            total += len(content)
            require(total <= MAX_INPUT, "cluster-account")
            permitted = policy if (isinstance(owned, dict) and path == "/etc/ssh/sshd_config.d/" + owned.get("name", "") and
                                   hashlib.sha256(content).hexdigest() == owned.get("sha256")) else None
            for include in staging.sshd_includes(content, owned_policy=permitted):
                if include == "/etc/ssh/sshd_config.d/*.conf":
                    directory = os.open("/etc/ssh/sshd_config.d", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                    try:
                        info = os.fstat(directory)
                        require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "cluster-account")
                        pending.extend("/etc/ssh/sshd_config.d/" + name for name in sorted(os.listdir(directory)) if name.endswith(".conf"))
                    finally:
                        os.close(directory)
                else:
                    pending.append(include)
        self.command(["/usr/sbin/sshd", "-t", "-f", "/etc/ssh/sshd_config"], json_result=False)

    def sshd_policy_file(self, path):
        return self.native_file(path, maximum=256 * 1024, private=False, root_only=True)

    def transport_scope(self):
        helper, unused = tunnel_helpers()
        uid = self.manager_uid()
        label = self.manager_label()
        require(type(uid) is int and uid > 0 and isinstance(label, str) and label, "native-tunnel-unproved")
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"],
                "image": self.request["image"], "uid": uid, "label": label,
                "nativeUser": self.request["clusterSSHUser"], "hosts": helper.targets(self.request)}

    def tunnel_preflight(self, private):
        """Read only: the coordinator asks each manager host before it claims or authorizes any host."""
        require(self.request["clusterSSHTransport"] == "verified-tunnel" and set(private) == {"knownHosts"}, "material")
        self.tunnel_host_key_preflight(private)
        return {"changed": False}

    def tunnel_host_key_preflight(self, private):
        """Before any authorization effect, a manager host proves its tunnel client admits an algorithm of every pinned host key."""
        if self.request["clusterSSHTransport"] != "verified-tunnel":
            return
        managers = next(s["placement"]["hosts"] for s in self.request["services"] if s["service_type"] == "mgr")
        if self.host["name"] not in managers:
            return
        helper, unused = tunnel_helpers()
        require(isinstance(private["knownHosts"], str), "material")
        hosts = helper.targets(self.request)
        self._tunnel_host_keys(lambda: helper.outer_configuration(self.manager_policy(), hosts, helper.known_hosts(private["knownHosts"].encode(), hosts)))

    def _tunnel_host_keys(self, render):
        """Render tunnel material; a pinned key the image's policy excludes is a named native failure naming its host and key type."""
        helper, unused = tunnel_helpers()
        try:
            return render()
        except helper.HostKeyRefusal as error:
            raise NativeFailure("tunnel-host-key-excluded", self.native_coordinate(),
                                "host " + error.host + " pins a " + error.key_type
                                + " host key, and the HostKeyAlgorithms of the selected image's SSH crypto policy admit none of its algorithms") from None

    def transport(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        require(self.markers.owned(), "ownership")
        require(self.request["clusterSSHTransport"] == "verified-tunnel", "native-tunnel-unproved")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        scope = self.transport_scope()
        policy = self.manager_policy()
        helper, unused = tunnel_helpers()
        source = getattr(helper, "_BOOTWRIGHT_SOURCE", None) or inspect.getsource(helper)
        files = self._tunnel_host_keys(lambda: helper.bundle(self.request, private["clusterPrivateKey"].encode(), private["knownHosts"].encode(),
                                                             source.encode(), policy, public=private["clusterPublicKey"]))
        original = custody.retain(self.markers, "transport-original", scope, files)
        record = {"scope": scope, "custodyID": original["custodyID"], "manifest": json.loads(files["manifest.json"])}
        owned = self.markers.read("transport.json")
        require(owned is None or canonical(owned) == canonical(record), "ownership")
        self.markers.write("transport.json", record)
        with custody.hold(self.markers, "transport-original", scope) as (unused, material):
            directory = custody.manager_directory(self.markers, scope["uid"])
            try:
                require(set(os.listdir(directory)) <= set(material), "ownership")
                for name, data in sorted(material.items()):
                    custody.materialize(self.markers, directory, name, data, scope["uid"])
                custody.relabel_intent(self.markers, scope, scope["uid"], scope["label"])
                custody.prove_materialized(self.markers, directory, scope, material, scope["uid"], scope["label"])
                helper.verify_bundle(os.path.join(self.markers.path, "transport"), expected=material, uid=scope["uid"])
                self.markers.write("transport-ready.json", record)
            finally:
                os.close(directory)
        return {"changed": owned is None}

    def bootstrap(self, private):
        require(supported_transport(self.request), "unsupported-release")
        require(self.markers.owned(), "ownership")
        require(self.host["name"] == self.request["bootstrap"])
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.prove_registry()
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            return self.tunnel_bootstrap(private)
        if self.daemons():
            require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
            self.provider_registry_absent()
            self.config_database()
            return {"changed": self.accept_call_home()}
        # IBM enables call-home at license acceptance. Until a pinned binary's
        # suppression is qualified, refuse disabled intent before bootstrap.
        require(self.request["callHome"] == "enabled", "call-home-unqualified")
        with tempfile.TemporaryDirectory(prefix=".bootstrap-", dir=self.markers.path) as scratch:
            for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts"):
                private_write(os.path.join(scratch, name), private[name].encode())
            ssh_config = ("Host *\n  StrictHostKeyChecking yes\n  UserKnownHostsFile " + os.path.join(self.markers.path, "known_hosts") +
                          "\n  GlobalKnownHostsFile /dev/null\n  IdentitiesOnly yes\n  IdentityAgent none\n  PasswordAuthentication no\n"
                          "  KbdInteractiveAuthentication no\n  ForwardAgent no\n  ProxyCommand none\n  ProxyJump none\n")
            known_path = os.path.join(self.markers.path, "known_hosts")
            if not os.path.exists(known_path):
                private_write(known_path, private["knownHosts"].encode())
            else:
                require(read_regular(known_path, private=True) == private["knownHosts"].encode(), "host-trust")
            private_write(os.path.join(scratch, "ssh_config"), ssh_config.encode())
            private_write(os.path.join(scratch, "ceph.conf"), bootstrap_config(self.request))
            argv = [self.tool, "--image", self.request["image"], "bootstrap", "--fsid", self.request["fsid"], "--mon-ip", self.request["monIP"], "--output-dir",
                    self.markers.path, "--skip-pull", "--skip-monitoring-stack", "--skip-dashboard", "--skip-admin-label", "--allow-fqdn-hostname",
                    "--ssh-user", self.request["clusterSSHUser"], "--ssh-private-key", os.path.join(scratch, "clusterPrivateKey"), "--ssh-public-key",
                    os.path.join(scratch, "clusterPublicKey"), "--ssh-config", os.path.join(scratch, "ssh_config"), "--config",
                    os.path.join(scratch, "ceph.conf"), "--automatically-accept-license"]
            if self.request["clusterNetworks"]:
                argv.extend(["--cluster-network", ",".join(self.request["clusterNetworks"])])
            self.command(argv, timeout=600, json_result=False)
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.provider_registry_absent()
        self.config_database()
        self.accept_call_home()
        return {"changed": True}

    def tunnel_bootstrap(self, private):
        unused, staging = tunnel_helpers()
        intent = staging.stage_intent(self.request)
        require(not self.daemons(), "partial-cluster")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.prove_registry()
        require(self.markers.read("transport-ready.json") is not None, "native-tunnel-unproved")
        scope = self.recovery_scope()
        self.prove_preparation()
        require(self.markers.read("bootstrap-attempt.json") is None, "partial-cluster")
        require(self.markers.read("tunnel-stage.json") is None or self.markers.read("bootstrap-protocol.json") == scope, "ownership")
        self.prove_bootstrap_absence()
        self.markers.write("bootstrap-protocol.json", scope)
        self.markers.write("tunnel-stage.json", intent)
        with tempfile.TemporaryDirectory(prefix=".bootstrap-", dir=self.markers.path) as scratch:
            private_write(os.path.join(scratch, "ceph.conf"), bootstrap_config(self.request))
            with self.transport_hold() as original:
                helper, unused = tunnel_helpers()
                # The bundle holds known_hosts in the form transport() rendered, not the controller's bytes.
                known = helper.known_hosts(private["knownHosts"].encode(), helper.targets(self.request))
                require(private["clusterPrivateKey"].encode() == original["identity"] and known == original["known_hosts"], "ownership")
                require(helper.public_key(private["clusterPublicKey"]) == json.loads(original["manifest.json"])["identityPublicKey"], "ownership")
                # A refusal here leaves no attempt record, so the bootstrap may still be retried.
                self.prove_inert_host_preparation()
                argv = staging.bootstrap_argv(self.request, scratch, self.selection)
                self.require_call_home_path()
                self.require_declared_options(argv)
                self.markers.write("bootstrap-attempt.json", scope)
                self.command(argv, timeout=BOOTSTRAP_TIMEOUT, json_result=False, recover=self.license_call_continues)
        self.capture_bootstrap_outputs()
        return {"changed": True}

    def license_call_continues(self, code, output):
        """Only an IBM build's own zero-host license-call refusal, with the exact owned seed in place, continues bootstrap."""
        unused, staging = tunnel_helpers()
        if not staging.license_call_refusal(self.request, code, output):
            return False
        if self.selection.variant("ceph.bootstrap.license-call-refusal") != "continue-seed":
            return False
        try:
            self.prove_seed(self.daemons())
        except (ValueError, OSError):
            return False
        return True

    def prove_inert_host_preparation(self):
        """Prove host preparation inert before invoking the selected provider's host check."""
        require(os.path.isfile("/usr/sbin/lvcreate") and os.access("/usr/sbin/lvcreate", os.X_OK), "host-prerequisite")
        for unit in CEPHADM_TIME_UNITS:
            enabled = self.run(["/usr/bin/systemctl", "is-enabled", unit], timeout=30)[1].decode("ascii", "replace").strip()
            active = self.run(["/usr/bin/systemctl", "is-active", unit], timeout=30)[1].decode("ascii", "replace").strip()
            if enabled == "enabled" and active == "active":
                break
            # systemd prints nothing on stdout for a unit it does not know, or "not-found"; cephadm treats both as absent.
            require(enabled in ("", "not-found"), "host-time")
        else:
            raise Refusal("host-time")
        self.command([self.tool, "check-host", "--expect-hostname", self.host["name"]], timeout=60, json_result=False)

    def prove_bootstrap_absence(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        require(not self.daemons(), "partial-cluster")
        for name in ("bootstrap-output-source.json", "bootstrap-original-reserved.json", "bootstrap-original-ready.json",
                     "tunnel-config-intent.json", "tunnel-configured.json", "tunnel-redeploy.json", "tunnel-mounted.json", "bootstrap-enrollment.json"):
            require(self.markers.read(name) is None, "ownership")
        paths = list(self.bootstrap_sources().values()) + [os.path.join("/var/lib/ceph", self.request["fsid"])]
        paths += ["/etc/systemd/system/ceph-" + self.request["fsid"] + ".target"]
        for path in paths:
            try:
                custody.source_identity(path, 0)
            except FileNotFoundError:
                continue
            raise Refusal("partial-cluster")
        custody.source_identity("/etc/systemd/system", 0)
        directory = os.open("/etc/systemd/system", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(directory)
            require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "ownership")
            names = os.listdir(directory)
            require(len(names) <= 8192 and not any(name.startswith("ceph-" + self.request["fsid"] + "@") for name in names), "partial-cluster")
        finally:
            os.close(directory)

    def prove_seed(self, daemons, observing=False):
        unused, staging = tunnel_helpers()
        scope = self.recovery_scope()
        require(self.host["name"] == self.request["bootstrap"], "ownership")
        require(canonical(self.markers.read("bootstrap-protocol.json")) == canonical(scope) and
                canonical(self.markers.read("bootstrap-attempt.json")) == canonical(scope), "ownership")
        intent = staging.prove_seed_daemons(self.request, daemons, observing=observing)
        require(self.markers.read("tunnel-stage.json") == intent and self.markers.read("bootstrap-enrollment.json") is None, "ownership")
        self.prove_preparation()
        if self.markers.read("bootstrap-output-source.json") is None:
            # Uncaptured outputs keep cephadm's own modes; capture makes them private, and observation never does.
            self.bootstrap_outputs(tighten=False)
        else:
            for path in self.bootstrap_sources().values():
                with self.native_source(path, root_only=True):
                    pass
        if self.markers.read("bootstrap-original-ready.json") is not None:
            require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
            require(self.ceph("orch", "host", "ls") == [] and self.services_inventory() == [] and self.ceph("osd", "ls") == [], "partial-cluster")
            quorum = self.ceph("quorum_status")
            require(isinstance(quorum, dict) and quorum.get("quorum_names") == [intent["monID"]], "partial-cluster")
            mgr = self.ceph("mgr", "dump")
            require(isinstance(mgr, dict) and mgr.get("active_name") == intent["mgrID"] and mgr.get("standbys") == [], "partial-cluster")
            self.provider_registry_absent()
        return intent

    def bootstrap_state(self):
        """Read-only admission proof; an attempted empty seed never starts again."""
        unused, staging = tunnel_helpers()
        require(self.request["action"] == "cluster" and self.host["name"] == self.request["bootstrap"], "request")
        daemons, owned = self.daemons(), self.markers.owned()
        require(owned or not daemons, "foreign-cluster")
        evidence = self.empty_evidence()
        if self.markers.read("bootstrap-enrollment.json") is not None:
            require(canonical(self.markers.read("bootstrap-enrollment.json")) == canonical(self.recovery_scope()), "ownership")
            evidence = self.observe()
            stage = "complete" if evidence["ready"] else "enrolling"
        elif not daemons:
            require(self.markers.read("bootstrap-attempt.json") is None, "partial-cluster")
            if owned:
                scope = self.recovery_scope()
                protocol = self.markers.read("bootstrap-protocol.json")
                require(protocol is None or canonical(protocol) == canonical(scope), "ownership")
                require(self.markers.read("tunnel-stage.json") is None or protocol is not None
                        and self.markers.read("tunnel-stage.json") == staging.stage_intent(self.request), "ownership")
            self.prove_bootstrap_absence()
            stage = self.preparation_state()["stage"]
            if owned:
                evidence.update(exists=True, partial=True)
        else:
            self.prove_seed(daemons, observing=True)
            evidence.update(exists=True, partial=True)
            stage = "seed"
            if self.markers.read("tunnel-configured.json") is not None:
                require(self.configuration_complete(), "native-tunnel-unproved")
                stage = "configured"
            if self.markers.read("tunnel-redeploy.json") is not None:
                self.blocking = self.prove_mounted_seed(daemons, observing=True)
                if self.markers.read("tunnel-mounted.json") is not None and self.markers.read("tunnel-manager-files.json") is not None:
                    stage = "mounted"
        result = staging.recovery_record(self.request, self.host["name"], stage)
        result["evidence"] = evidence
        return result

    def tunnel_effect(self, *arguments, **options):
        proof = self.transport_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof, self.cache_hold():
            return self.ceph(*arguments, **options)

    def cache_hold(self):
        """Once published, every native SSH effect holds the exact cephadm cache inode."""
        if self.request["clusterSSHTransport"] != "verified-tunnel" or self.markers.read("cache-ready.json") is None:
            return contextlib.nullcontext()
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        return runtime.Cache(self).execution_proof()

    def cache(self):
        """Publish and prove the exact cephadm bytes on this host before enrollment."""
        require(self.markers.owned() and self.request["clusterSSHTransport"] == "verified-tunnel", "ownership")
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        if self.host["name"] == self.request["bootstrap"]:
            require(self.markers.read("tunnel-mounted.json") is not None, "native-tunnel-unproved")
        return {"changed": runtime.Cache(self).prepare()}

    def configuration_intent(self):
        helper, unused = tunnel_helpers()
        transport = self.markers.read("transport-ready.json")
        require(isinstance(transport, dict) and isinstance(transport.get("custodyID"), str), "ownership")
        return {"scope": self.recovery_scope(), "custodyID": transport["custodyID"],
                "config": hashlib.sha256(helper.native_configuration(helper.targets(self.request))).hexdigest()}

    def configuration_keys(self):
        keys = self.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 8192 and all(isinstance(key, str) for key in keys) and len(set(keys)) == len(keys), "native-output")
        require("mgr/cephadm/ssh_identity_cert" not in keys, "native-tunnel-unproved")
        return set(keys)

    def configure_setting(self, name, wanted, command, mutate):
        intent = dict(self.configuration_intent(), step=name)
        attempted = self.markers.read("tunnel-config-attempt-" + name + ".json")
        completed = self.markers.read("tunnel-config-complete-" + name + ".json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        key = "mgr/cephadm/" + name
        present = key in self.configuration_keys()
        # A matching unbound secret is foreign; never read it to infer custody.
        require(not present or attempted is not None, "ownership")
        if not present:
            require(attempted is None and completed is None, "partial-cluster")
            if not mutate:
                return False, False
            self.markers.write("tunnel-config-attempt-" + name + ".json", intent)
            self.tunnel_effect(*command, data=wanted if name != "ssh_user" else b"", json_result=False)
            require(key in self.configuration_keys(), "native-tunnel-unproved")
        # The CLI prints a stored value as it is; asking for JSON would prepend a blank line.
        value = self.ceph_text("config-key", "get", key)
        require(isinstance(value, bytes) and value in (wanted, wanted + b"\n"), "native-tunnel-unproved")
        if mutate:
            self.markers.write("tunnel-config-complete-" + name + ".json", intent)
        return True, not present

    def call_home_acceptance(self, optional=False):
        values = self.ceph("config", "dump")
        require(isinstance(values, list), "native-output")
        selected = [value for value in values if isinstance(value, dict) and value.get("name") == "mgr/cephadm/call_home_needs_acceptance"]
        if not selected and optional:
            return None
        require(len(selected) == 1 and selected[0].get("section") == "mgr" and selected[0].get("mask") == "", "call-home-drift")
        # The manager stores Python's str(bool); the MON normalizes a known option's spelling, so either one is the same flag.
        value = selected[0].get("value")
        require(isinstance(value, str) and value.lower() in ("true", "false"), "call-home-drift")
        return value.lower() == "true"

    def call_home_reissuable(self, needed):
        """An attempted enable-call-home only sets values and enables a module: before any acceptance, a missing result repeats it."""
        if needed is False or any(self.markers.read(name) is not None for name in (
                "tunnel-config-attempt-call_home.json", "tunnel-config-complete-call_home.json", "call-home-accepted.json", "tunnel-configured.json")):
            return False
        if needed is None:
            return True
        try:
            self.call_home_state()
        except Refusal as error:
            if type(error) is not Refusal or str(error) != "call-home-drift":
                raise
            return True
        return False

    def initialize_call_home(self, mutate):
        """Finish the native seed's missing Call Home initialization without re-bootstrapping."""
        if not self.call_home_applicable():
            self.call_home_state()
            return True, False
        intent = dict(self.configuration_intent(), step="call_home_initialize")
        prefix = "tunnel-config-"
        attempted = self.markers.read(prefix + "attempt-call_home_initialize.json")
        completed = self.markers.read(prefix + "complete-call_home_initialize.json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        needed = self.call_home_acceptance(optional=True)
        changed = False
        if attempted is None:
            if needed is not None:
                return True, False
            require(self.markers.read("tunnel-config-attempt-call_home.json") is None
                    and self.markers.read("tunnel-config-complete-call_home.json") is None
                    and self.markers.read("call-home-accepted.json") is None
                    and self.markers.read("tunnel-configured.json") is None, "call-home-drift")
            if not mutate:
                return False, False
            self.markers.write(prefix + "attempt-call_home_initialize.json", intent)
            self.manager_retry(self.tunnel_effect, "orch", "enable-call-home", "--raise-automation-warning", json_result=False)
            needed = self.call_home_acceptance()
            require(needed, "call-home-drift")
            changed = True
        else:
            if mutate and self.call_home_reissuable(needed):
                self.manager_retry(self.tunnel_effect, "orch", "enable-call-home", "--raise-automation-warning", json_result=False)
                needed = self.call_home_acceptance()
                require(needed, "call-home-drift")
                changed = True
            require(needed is not None, "partial-cluster")
            acceptance = dict(self.configuration_intent(), step="call_home")
            require(needed or canonical(self.markers.read("tunnel-config-attempt-call_home.json")) == canonical(acceptance), "call-home-drift")
        self.call_home_state()
        if mutate and completed is None:
            self.markers.write(prefix + "complete-call_home_initialize.json", intent)
        return True, changed

    def configure_call_home(self, mutate):
        if not self.call_home_applicable():
            return True, False
        intent = dict(self.configuration_intent(), step="call_home")
        attempted = self.markers.read("tunnel-config-attempt-call_home.json")
        completed = self.markers.read("tunnel-config-complete-call_home.json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        needed = self.call_home_acceptance()
        if attempted is None:
            require(needed and completed is None and self.markers.read("call-home-accepted.json") is None, "call-home-drift")
            if not mutate:
                return False, False
            self.markers.write("tunnel-config-attempt-call_home.json", intent)
            self.accept_call_home()
            require(not self.call_home_acceptance(), "call-home-drift")
        else:
            require(not needed, "partial-cluster")
        if mutate:
            self.markers.write("call-home-accepted.json", {"callHome": "enabled"})
            self.markers.write("tunnel-config-complete-call_home.json", intent)
        return True, attempted is None

    def configure_values(self, mutate):
        helper, unused = tunnel_helpers()
        self.provider_registry_absent()
        options = self.ceph("config", "dump")
        require(isinstance(options, list), "native-output")
        require(not any(isinstance(option, dict) and option.get("name") == "mgr/cephadm/ssh_config_file"
                        and option.get("value") not in ("", None) for option in options), "native-tunnel-unproved")
        config = helper.native_configuration(helper.targets(self.request))
        changed, complete = False, True
        pre_steps = ("--skip-pre-steps",) if self.selection.variant("ceph.mgr.set-user-pre-steps") == "skip" else ()
        with self.transport_hold() as original:
            public = json.loads(original["manifest.json"])["identityPublicKey"].encode()
            values = (("ssh_identity_key", original["identity"], ("cephadm", "set-priv-key", "-i", "-")),
                      ("ssh_identity_pub", public, ("cephadm", "set-pub-key", "-i", "-")),
                      ("ssh_config", config, ("cephadm", "set-ssh-config", "-i", "-")),
                      ("ssh_user", self.request["clusterSSHUser"].encode(), ("cephadm", "set-user", self.request["clusterSSHUser"]) + pre_steps))
            for name, wanted, command in values:
                proved, effect = self.configure_setting(name, wanted, command, mutate)
                complete, changed = complete and proved, changed or effect
            proved, effect = self.initialize_call_home(mutate)
            complete, changed = complete and proved, changed or effect
            if proved:
                proved, effect = self.configure_call_home(mutate)
                complete, changed = complete and proved, changed or effect
        self.config_database()
        return complete, changed

    def configuration_complete(self):
        intent = self.configuration_intent()
        require(canonical(self.markers.read("tunnel-config-intent.json")) == canonical(intent), "ownership")
        require(canonical(self.markers.read("tunnel-configured.json")) == canonical(intent), "ownership")
        for name in ("ssh_identity_key", "ssh_identity_pub", "ssh_config", "ssh_user") + (("call_home",) if self.call_home_applicable() else ()):
            require(canonical(self.markers.read("tunnel-config-complete-" + name + ".json")) == canonical(dict(intent, step=name)), "ownership")
        return self.configure_values(False)[0]

    def tunnel_configure(self, private):
        helper, staging = tunnel_helpers()
        require(self.markers.owned() and self.host["name"] == self.request["bootstrap"], "ownership")
        require(self.markers.read("tunnel-stage.json") == staging.stage_intent(self.request), "ownership")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.prove_seed(self.daemons())
        if self.markers.read("bootstrap-original-ready.json") is None:
            self.capture_bootstrap_outputs()
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        require(self.manager_retry(self.ceph, "orch", "host", "ls") == [] and self.manager_retry(self.services_inventory) == []
                and self.ceph("osd", "ls") == [], "foreign-host")
        self.prove_registry()
        with self.transport_hold() as original:
            require(private["clusterPrivateKey"].encode() == original["identity"], "ownership")
            require(helper.public_key(private["clusterPublicKey"]) == json.loads(original["manifest.json"])["identityPublicKey"], "ownership")
        intent = self.configuration_intent()
        self.markers.write("tunnel-config-intent.json", intent)
        complete, changed = self.configure_values(True)
        require(complete, "native-tunnel-unproved")
        self.markers.write("tunnel-configured.json", intent)
        return {"changed": changed}

    def manager_scope(self, plan):
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"],
                "image": self.request["image"], "plan": plan}

    def retain_manager_material(self, plan, files):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        scope = self.manager_scope(plan)
        if self.markers.read("initial-mgr-original-ready.json") is not None:
            return scope
        with self.native_source(files["config"]) as config, self.native_source(files["keyring"]) as keyring:
            sources = {"config": custody.metadata(os.fstat(config.fileno())), "keyring": custody.metadata(os.fstat(keyring.fileno()))}
            record = {"scope": scope, "files": sources}
            previous = self.markers.read("tunnel-mgr-source.json")
            require(previous is None or canonical(previous) == canonical(record), "ownership")
            self.markers.write("tunnel-mgr-source.json", record)
            # Durable native source identities precede the first private read.
            material = {"config": config.read(MAX_INPUT + 1), "keyring": keyring.read(MAX_INPUT + 1)}
            for name, handle in (("config", config), ("keyring", keyring)):
                require(custody.metadata(os.fstat(handle.fileno())) == sources[name], "ownership")
                require(len(material[name]) == sources[name]["size"], "ownership")
            custody.retain(self.markers, "initial-mgr-original", scope, material)
        return scope

    def prove_manager_material(self, files, material, original=False, transition=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        source = self.markers.read("tunnel-mgr-source.json")
        require(isinstance(source, dict) and set(source) == {"scope", "files"}, "ownership")
        completed = self.markers.read("tunnel-manager-files.json")
        if completed is not None:
            require(isinstance(completed, dict) and set(completed) == {"scope", "custodyID", "files"}, "ownership")
            require(canonical({key: completed[key] for key in ("scope", "custodyID")}) == canonical(self.markers.read("tunnel-redeploy.json")), "ownership")
        require(original or completed is not None or transition, "ownership")
        expected = source if original else completed
        observed = {}
        for name in ("config", "keyring"):
            with self.native_source(files[name]) as handle:
                before = custody.metadata(os.fstat(handle.fileno()))
                require(expected is None or canonical(before) == canonical(expected["files"].get(name)), "ownership")
                require(handle.read(MAX_INPUT + 1) == material[name], "ownership")
                require(custody.metadata(os.fstat(handle.fileno())) == before, "ownership")
                observed[name] = before
        return observed

    def tunnel_mount(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        unused, staging = tunnel_helpers()
        require(self.markers.owned() and self.host["name"] == self.request["bootstrap"], "ownership")
        intent = self.markers.read("tunnel-stage.json")
        require(intent == staging.stage_intent(self.request), "ownership")
        require(self.markers.read("tunnel-configured.json") is not None, "ownership")
        if self.recovery_enabled():
            self.prove_seed(self.daemons())
            require(self.configuration_complete(), "native-tunnel-unproved")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.prove_registry()
        self.provider_registry_absent()
        files = staging.initial_files(self.request)
        mon_meta = self.native_file(files["mon"]["unit.meta"], maximum=4096, private=False)
        mgr_meta = self.native_file(files["mgr"]["unit.meta"], maximum=4096, private=False)
        plan = staging.initial_redeploy_plan(self.request, intent, self.daemons(), self.manager_retry(self.ceph, "orch", "host", "ls"), mon_meta,
                                             mgr_meta)
        scope = self.retain_manager_material(plan, files["mgr"])
        with custody.hold(self.markers, "initial-mgr-original", scope) as (original, material), self.transport_hold(settle=True):
            record = {"scope": scope, "custodyID": original["custodyID"]}
            previous = self.markers.read("tunnel-redeploy.json")
            require(previous is None or canonical(previous) == canonical(record), "ownership")
            restarted = None
            if previous is None:
                self.prove_manager_material(files["mgr"], material, original=True)
                manager = self.ceph("mgr", "dump")
                require(isinstance(manager, dict) and type(manager.get("active_gid")) is int, "native-output")
                restarted = manager["active_gid"]
                self.markers.write("tunnel-redeploy.json", record)
                with tempfile.TemporaryDirectory(prefix=".tunnel-stage-", dir=self.markers.path) as scratch:
                    private_write(os.path.join(scratch, "mgr.config"), material["config"])
                    private_write(os.path.join(scratch, "mgr.keyring"), material["keyring"])
                    self.command(staging.redeploy_argv(self.request, plan, scratch), timeout=600, json_result=False)
            # An ambiguous previous deployment permits observation only.
            self.verify_manager_mounts(self.daemons())
            # The redeploy restarted the only manager; its orchestrator answers again before the proof that needs it.
            self.manager_wait(previous=restarted, name=intent["mgrID"])
            self.manager_retry(self.verify_tunnel_configuration)
            observed = self.prove_manager_material(files["mgr"], material, transition=True)
            self.markers.write("tunnel-manager-files.json", dict(record, files=observed))
            self.markers.write("tunnel-mounted.json", {"intent": intent, "custodyID": original["custodyID"]})
        return {"changed": previous is None}

    def prove_mounted_seed(self, daemons, observing=False):
        """The mounted seed; an observation returns the seed manager pending while cephadm restarts its container."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        unused, staging = tunnel_helpers()
        require(self.configuration_complete(), "native-tunnel-unproved")
        files = staging.initial_files(self.request)
        plan = staging.initial_redeploy_plan(self.request, staging.stage_intent(self.request), daemons, [],
                                             self.native_file(files["mon"]["unit.meta"], maximum=4096, private=False),
                                             self.native_file(files["mgr"]["unit.meta"], maximum=4096, private=False), observing=observing)
        scope = self.manager_scope(plan)
        with custody.hold(self.markers, "initial-mgr-original", scope) as (original, material), self.transport_hold():
            require(canonical(self.markers.read("tunnel-redeploy.json")) == canonical({"scope": scope, "custodyID": original["custodyID"]}), "ownership")
            mounted = self.markers.read("tunnel-mounted.json")
            require(mounted is None or canonical(mounted) == canonical({"intent": staging.stage_intent(self.request), "custodyID": original["custodyID"]}),
                    "ownership")
            pending = self.verify_manager_mounts(daemons, observing=observing)
            self.verify_tunnel_configuration()
            self.prove_manager_material(files["mgr"], material, transition=True)
        return pending

    @contextlib.contextmanager
    def native_source(self, path, maximum=MAX_INPUT, private=True, root_only=False, allowed_uid=None):
        """Open one proved file: every directory on the way owned by an admitted account and closed to others, and the
        file regular, admitted, closed and bounded. A public root-only source follows a root-owned symbolic link, as the
        crypto-policy back-ends sshd includes are, through the same proof and a bound, and may carry a package's hard
        links; any other source refuses a link and is the only name of its inode."""
        uid = self.manager_uid()
        require(type(uid) is int and uid > 0, "native-tunnel-unproved")
        owners = (ROOT_UID,) if root_only else (ROOT_UID, uid if allowed_uid is None else allowed_uid)
        shared = root_only and not private
        followed = 0
        while True:
            components = path.strip("/").split("/")
            require(os.path.isabs(path) and all(c not in ("", ".", "..") for c in components), "file")
            directory = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            try:
                for index, component in enumerate(components):
                    entry = os.stat(component, dir_fd=directory, follow_symlinks=False)
                    if stat.S_ISLNK(entry.st_mode):
                        require(shared and entry.st_uid == ROOT_UID and followed < SOURCE_LINKS, "file")
                        target = os.readlink(component, dir_fd=directory)
                        path = os.path.normpath(os.path.join("/" + "/".join(components[:index]), target))
                        followed += 1
                        break
                    if index < len(components) - 1:
                        following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
                        os.close(directory)
                        directory = following
                        info = os.fstat(directory)
                        require(info.st_uid in owners and info.st_mode & 0o022 == 0, "file")
                else:
                    fd = os.open(components[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
                    with os.fdopen(fd, "rb") as handle:
                        info = os.fstat(handle.fileno())
                        require(stat.S_ISREG(info.st_mode) and (info.st_nlink == 1 or shared) and info.st_uid in owners
                                and info.st_mode & (0o077 if private else 0o022) == 0 and 0 < info.st_size <= maximum, "file")
                        yield handle
                    return
            finally:
                os.close(directory)

    def native_file(self, path, maximum=MAX_INPUT, private=True, root_only=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        with self.native_source(path, maximum, private, root_only) as handle:
            before = custody.metadata(os.fstat(handle.fileno()))
            content = handle.read(maximum + 1)
            require(len(content) == before["size"] and custody.metadata(os.fstat(handle.fileno())) == before, "file")
            return content

    @contextlib.contextmanager
    def transport_hold(self, settle=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        helper, unused = tunnel_helpers()
        scope = self.transport_scope()
        record = self.markers.read("transport-ready.json")
        # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with custody.hold(self.markers, "transport-original", scope) as (original, material):
            wanted = {"scope": scope, "custodyID": original["custodyID"], "manifest": json.loads(material["manifest.json"])}
            require(canonical(record) == canonical(wanted) and canonical(self.markers.read("transport.json")) == canonical(wanted), "ownership")
            directory = custody.manager_directory(self.markers, scope["uid"], create=False)
            try:
                custody.prove_materialized(self.markers, directory, scope, material, scope["uid"], scope["label"], settle=settle)
                helper.verify_bundle(os.path.join(self.markers.path, "transport"), expected=material, uid=scope["uid"])
                yield material
                custody.prove_materialized(self.markers, directory, scope, material, scope["uid"], scope["label"], settle=settle)
            finally:
                os.close(directory)

    def verify_transport_bundle(self):
        with self.transport_hold():
            pass

    def prove_mount_source(self, source, exact):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        scope = self.transport_scope()
        identity = custody.source_identity(source, scope["uid"])
        if exact:
            recorded = self.markers.read("transport-directory.json")
            require(isinstance(recorded, dict) and set(recorded) == custody.BASE_FIELDS and all(type(item) is int for item in recorded.values()), "ownership")
            require(recorded["uid"] == recorded["gid"] == scope["uid"] and recorded["mode"] == 0o700 and identity == (recorded["dev"], recorded["inode"]),
                    "ownership")
        else:
            require(identity not in custody.protected_identities(self.markers.path), "native-tunnel-unproved")

    def verify_manager_mounts(self, daemons, observing=False):
        """Prove each running manager container of this host mounts the bundle exactly. An observation names a manager whose
        container is absent or not running, as cephadm's --rm containers are while it restarts them, as pending; an action
        requires it running. A running container that contradicts the request refuses either way."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observe
        helper, unused = tunnel_helpers()
        require(isinstance(daemons, list) and bool(daemons), "native-tunnel-unproved")
        seen, pending = set(), []
        managers = next(s["placement"]["hosts"] for s in self.request["services"] if s["service_type"] == "mgr")
        for daemon in daemons:
            require(isinstance(daemon, dict) and daemon.get("fsid") == self.request["fsid"], "native-tunnel-unproved")
            # Another block owns a delegated daemon's image; the host layout already proved its listing.
            if str(daemon.get("name", "")).split(".", 1)[0] in ceph_native_observe.DELEGATED_TYPES:
                continue
            # A daemon cephadm has not yet written unit files for names no image and runs no container.
            images = (self.request["image"],) if daemon.get("container_id") != "" else (self.request["image"], "")
            require(daemon.get("container_image_name") in images, "native-tunnel-unproved")
            if not str(daemon.get("name", "")).startswith("mgr."):
                continue
            name = daemon["name"][4:]
            require(name not in seen and daemon["name"] not in pending and daemon_identity_host("mgr", name, managers) == self.host["name"],
                    "native-tunnel-unproved")
            if daemon.get("state") != "running" or daemon.get("container_id") == "":
                require(observing and PENDING_DAEMON.fullmatch(daemon["name"]) is not None, "native-tunnel-unproved")
                pending.append(daemon["name"])
                continue
            seen.add(name)
            identifier = daemon.get("container_id")
            require(isinstance(identifier, str) and re.fullmatch(r"[0-9a-f]{12,64}", identifier) is not None, "native-tunnel-unproved")
            containers = self.command(["/usr/bin/podman", "container", "inspect", identifier])
            require(isinstance(containers, list) and len(containers) == 1 and isinstance(containers[0], dict), "native-tunnel-unproved")
            container = containers[0]
            require(isinstance(container.get("HostConfig"), dict) and container["HostConfig"].get("Privileged") is False, "native-tunnel-unproved")
            require(isinstance(container.get("Id"), str) and container["Id"].startswith(identifier), "native-tunnel-unproved")
            require(isinstance(container.get("Mounts"), list) and len(container["Mounts"]) <= 64, "native-tunnel-unproved")
            for mount in container["Mounts"]:
                require(isinstance(mount, dict) and isinstance(mount.get("Destination"), str), "native-tunnel-unproved")
                destination = mount["Destination"]
                source = mount.get("Source")
                if mount.get("Type") == "bind":
                    require(isinstance(source, str) and source.startswith("/") and source == os.path.normpath(source), "native-tunnel-unproved")
                    protected = self.markers.path
                    overlaps = source == protected or source.startswith(protected + "/") or protected.startswith(source.rstrip("/") + "/")
                    exact = source == protected + "/transport" and destination == helper.CONTAINER_ROOT and mount.get("RW") is False
                    require(not overlaps or exact, "native-tunnel-unproved")
                    self.prove_mount_source(source, exact)
                require(destination == os.path.normpath(destination) and destination.startswith("/"), "native-tunnel-unproved")
                require(not destination.startswith(helper.CONTAINER_ROOT + "/"), "native-tunnel-unproved")
                require(destination == helper.CONTAINER_ROOT or not helper.CONTAINER_ROOT.startswith(destination.rstrip("/") + "/"), "native-tunnel-unproved")
            mounts = [m for m in container["Mounts"] if isinstance(m, dict) and m.get("Destination") == helper.CONTAINER_ROOT]
            require(len(mounts) == 1 and mounts[0].get("Type") == "bind" and mounts[0].get("Source") == os.path.join(self.markers.path, "transport")
                    and mounts[0].get("RW") is False, "native-tunnel-unproved")
        require(bool(seen) or bool(pending), "native-tunnel-unproved")
        return sorted(pending)

    def verify_tunnel_configuration(self):
        helper, unused = tunnel_helpers()
        wanted = helper.native_configuration(helper.targets(self.request))
        require(self.ceph_text("cephadm", "get-ssh-config") in (wanted, wanted + b"\n"), "native-tunnel-unproved")
        require(self.ceph_text("cephadm", "get-user").strip() == self.request["clusterSSHUser"].encode(), "native-tunnel-unproved")

    def accept_call_home(self):
        """Acknowledge only the frozen enabled intent after owned FSID proof."""
        if not self.call_home_applicable():
            return False
        record = {"callHome": "enabled"}
        name = "call-home-accepted.json"
        accepted = self.markers.read(name)
        require(accepted is None or accepted == record, "ownership")
        if accepted is not None:
            return False
        # The exact IBM command removes only the automatic-enable warning and
        # persists its acceptance flag. Repeating it after a lost response is
        # idempotent; publish completion only after native success.
        self.ceph("orch", "accept", "call-home-enabled", json_result=False)
        self.markers.write(name, record)
        return True

    def enrollment_hosts(self, *, retry=True):
        hosts = self.manager_retry(self.ceph, "orch", "host", "ls") if retry else self.ceph("orch", "host", "ls")
        require(isinstance(hosts, list) and len(hosts) <= len(self.request["hosts"]), "foreign-host")
        wanted = {host["name"]: host for host in self.request["hosts"]}
        result = {}
        for host in hosts:
            require(isinstance(host, dict) and host.get("hostname") in wanted, "foreign-host")
            name = host["hostname"]
            require(name not in result and host.get("addr") == wanted[name]["address"], "foreign-host")
            labels = host.get("labels", [])
            require(isinstance(labels, list) and all(isinstance(label, str) for label in labels)
                    and len(labels) == len(set(labels)) and set(labels) <= set(wanted[name]["labels"] or []), "host-label-drift")
            result[name] = host
        return result

    def license_accepted(self):
        """Read the MON's provider record against the proved image, without native license extraction."""
        keys = self.configuration_keys()
        selected = sorted(key for key in keys if key.startswith("mgr/cephadm/") and re.search(r"_license_[0-9a-f]{32}\Z", key))
        require(len(selected) <= 128, "native-output")
        if not selected:
            return False
        image_id = self.runtime_profile()["imageID"]
        accepted, budget = False, MAX_OUTPUT
        for key in selected:
            raw = self.ceph_text("config-key", "get", key)
            require(isinstance(raw, bytes) and len(raw) <= budget, "native-output")
            budget -= len(raw)
            try:
                value = json.loads(raw, object_pairs_hook=unique_object)
            except (ValueError, UnicodeError):
                raise Refusal("native-output") from None
            require(isinstance(value, dict) and set(value) == {"ceph_version", "image_id", "accept_time", "license"}, "native-output")
            require(all(isinstance(value[name], str) and value[name] for name in value), "native-output")
            require(HEX.fullmatch(value["image_id"]) is not None and len(value["ceph_version"]) <= 1024
                    and len(value["accept_time"]) <= 128, "native-output")
            try:
                timestamp = datetime.datetime.fromisoformat(value["accept_time"])
            except ValueError:
                raise Refusal("native-output") from None
            require(timestamp.tzinfo is not None, "native-output")
            digest = hashlib.md5(value["license"].encode("utf-8"), usedforsecurity=False).hexdigest()
            expected = "mgr/cephadm/" + value["ceph_version"].replace(" ", "_") + "_license_" + digest
            require(key == expected, "native-output")
            accepted = accepted or value["image_id"] == image_id
        return accepted

    def enrollment_state(self):
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.prove_registry()
        self.provider_registry_absent()
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            require(self.markers.read("tunnel-mounted.json") is not None, "native-tunnel-unproved")
            require(self.markers.read("cache-ready.json") is not None, "cache-unproved")
            self.verify_transport_bundle()
            self.manager_retry(self.verify_tunnel_configuration)
            self.verify_manager_mounts(self.daemons())
        scope = self.recovery_scope()
        enrolled = self.markers.read("bootstrap-enrollment.json")
        require(enrolled is None or enrolled == scope, "ownership")
        inventory = self.enrollment_hosts()
        require(enrolled is not None or not inventory, "foreign-host")
        pending, completed = [], []
        for host in self.request["hosts"]:
            name = host["name"]
            spec = {"service_type": "host", "hostname": name, "addr": host["address"], "labels": host["labels"] or []}
            intent = {"scope": scope, "host": spec}
            prefix = "enrollment-" + name
            attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
            require(attempt is None or attempt == intent, "ownership")
            require(complete is None or complete == intent and attempt == intent, "ownership")
            current = inventory.get(name)
            require(current is None or attempt == intent, "foreign-host")
            exact = current is not None and set(current.get("labels", [])) == set(spec["labels"])
            require(complete is None or exact, "host-label-drift")
            (completed if exact else pending).append((prefix, intent))
        return scope, enrolled, inventory, pending, completed

    def enroll(self):
        scope, enrolled, inventory, pending, completed = self.enrollment_state()
        remaining = float("inf") if self.execution_deadline is None else self.execution_deadline - self.clock()
        # Every admitted document has its full native command allowance, plus one inventory readback.
        count = 0
        for size in range(1, len(pending) + 1):
            if size * 60 + 60 <= remaining:
                count = size
        if enrolled is None:
            self.markers.write("bootstrap-enrollment.json", scope)
        require(not pending or count > 0, "enrollment-budget")
        batch = pending[:count]
        for prefix, intent in batch:
            if self.markers.read(prefix + "-attempt.json") is None:
                self.markers.write(prefix + "-attempt.json", intent)
        if batch:
            documents = b"\n---\n".join(canonical(intent["host"]) for unused, intent in batch) + b"\n"
            self.tunnel_effect("orch", "apply", "-i", "-", data=documents, json_result=False, timeout=60 * len(batch))
            inventory = self.enrollment_hosts(retry=False)
        for prefix, intent in completed + batch:
            current = inventory.get(intent["host"]["hostname"])
            require(current is not None and set(current.get("labels", [])) == set(intent["host"]["labels"]), "not-ready")
            if self.markers.read(prefix + "-complete.json") is None:
                self.markers.write(prefix + "-complete.json", intent)
        changed = bool(batch)
        if count < len(pending):
            return {"changed": changed, "remaining": True}
        return {"changed": changed}

    def license(self):
        scope, enrolled, unused, pending, completed = self.enrollment_state()
        require(enrolled == scope and not pending, "not-ready")
        for prefix, intent in completed:
            require(self.markers.read(prefix + "-complete.json") == intent, "not-ready")
        changed = False
        if not self.license_accepted() and self.selection.variant("ceph.mgr.license-contract") == "ibm":
            self.manager_retry(self.tunnel_effect, "orch", "accept-license", "--image", self.request["image"], json_result=False)
            require(self.license_accepted(), "not-ready")
            changed = True
        return {"changed": changed}

    def device_identities(self, publish=None):
        """The cluster's OSD device identities, as each host's preparation derived them."""
        record = self.markers.read(DEVICE_IDENTITIES)
        if publish is not None:
            expected = {"version": DEVICE_IDENTITIES_VERSION, "fsid": self.request["fsid"], "devices": publish}
            self.identity_rows(expected)
            require(record is None or canonical(record) == canonical(expected), "device-identity")
            if record is None:
                require(self.markers.owned(), "ownership")
                self.markers.write(DEVICE_IDENTITIES, expected)
                record = self.markers.read(DEVICE_IDENTITIES)
        return None if record is None else self.identity_rows(record)

    def identity_rows(self, record):
        wanted = [(host["name"], device) for host in self.request["hosts"] for device in host["devices"]]
        require(isinstance(record, dict) and set(record) == {"version", "fsid", "devices"}, "device-identity")
        require(record["version"] == DEVICE_IDENTITIES_VERSION and record["fsid"] == self.request["fsid"], "device-identity")
        rows = record["devices"]
        require(isinstance(rows, list) and len(rows) == len(wanted), "device-identity")
        result = {}
        for value, (host, device) in zip(rows, wanted):
            from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
            ceph_host_runtime.row(value, self.request, host)
            require(value["path"] == device["path"], "device-identity")
            result[(host, device["path"])] = value["deviceID"]
        return result

    def service_specs(self, limit=None):
        values = self.ceph("orch", "ls", "--export")
        limit = len(self.request["services"]) * len(self.request["hosts"]) if limit is None else limit
        require(isinstance(values, list) and len(values) <= limit, "foreign-service")
        result = {}
        for value in values:
            require(isinstance(value, dict) and isinstance(value.get("service_type"), str), "native-output")
            key = value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")
            require(key not in result, "foreign-service")
            normalized = dict(value)
            require(normalized.pop("service_name", key) == key, "foreign-service")
            if normalized.get("unmanaged") is False:
                normalized.pop("unmanaged")
            result[key] = normalized
        return result

    def service_spec(self, service):
        value = json.loads(canonical(service))
        if self.request["clusterSSHTransport"] == "verified-tunnel" and value["service_type"] == "mgr":
            helper, unused = tunnel_helpers()
            value["extra_container_args"] = ["--volume=" + os.path.join(self.markers.path, "transport") + ":" + helper.CONTAINER_ROOT + ":ro,z"]
        if value["service_type"] != "osd":
            return [value]
        result = []
        for host in self.request["hosts"]:
            if host["name"] not in value["placement"]["hosts"]:
                continue
            projected = json.loads(canonical(value))
            projected["placement"] = {"hosts": [host["name"]]}
            projected["service_id"] = value["service_id"][:46] + "-" + hashlib.sha256(canonical([value["service_id"], host["name"]])).hexdigest()[:16]
            for field in ("data_devices", "db_devices", "wal_devices"):
                if field in value["spec"]:
                    projected["spec"][field]["paths"] = [
                        self.stable_device_path(host, selected_device(host, path), submission=True)
                        for path in value["spec"][field]["paths"]
                    ]
            result.append(projected)
        return result

    def normalized_services(self, groups, record="service-inputs.json"):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        scope = {"owner": self.request["owner"], "fsid": self.request["fsid"], "image": self.request["image"], "inputs": groups}
        retained = self.markers.read(record)
        if retained is not None:
            require(isinstance(retained, dict) and set(retained) == {"version", "scope", "services"}
                    and retained["version"] == "ceph-service-inputs-v1" and retained["scope"] == scope, "ownership")
            result = json.loads(canonical(retained["services"]))
        else:
            profile = self.runtime_profile()
            cluster = {key: value for key, value in self.request.items() if key not in ("service", "gateway")}
            worker = runtime.HostRuntime(dict(cluster, action="cluster"), self.host["name"], command=self.run)
            source = ("import json,sys\nfrom ceph.deployment.service_spec import ServiceSpec\n"
                      "values=json.loads(sys.argv[1])\n"
                      "print(json.dumps([[ServiceSpec.from_json(value).to_json() for value in group] for group in values]))\n")
            result = worker.contained(profile["imageID"], (profile["uid"], profile["gid"]), source, (canonical(groups).decode(),))
        require(isinstance(result, list) and len(result) == len(groups), "native-output")
        for originals, normalized in zip(groups, result):
            require(isinstance(normalized, list) and len(normalized) == len(originals), "native-output")
            for original, value in zip(originals, normalized):
                require(isinstance(value, dict), "native-output")
                key = original["service_type"] + ("." + original["service_id"] if original.get("service_id") else "")
                require(value.pop("service_name", key) == key, "foreign-service")
                if value.get("unmanaged") is False:
                    value.pop("unmanaged")
                require(all(value.get(field) == original.get(field) for field in
                            ("service_type", "service_id", "placement", "extra_container_args")), "foreign-service")
                if original["service_type"] == "osd":
                    spec = value.get("spec")
                    require(isinstance(spec, dict) and spec.get("data_devices") == original["spec"]["data_devices"]
                            and all(spec.get(field) == original["spec"].get(field) for field in ("db_devices", "wal_devices"))
                            and all(spec.get(field, False) == original["spec"].get(field, False) for field in ("encrypted", "tpm2"))
                            and not any(spec.get(field) for field in ("journal_devices", "data_directories"))
                            and spec.get("osds_per_device", 1) == 1, "osd-unqualified")
        if retained is None:
            self.markers.write(record, {"version": "ceph-service-inputs-v1", "scope": scope, "services": result})
        return result

    def prove_service_projections(self, projections, observed, scope):
        """Admit only descriptions synthesized from this operation's remaining seed daemons."""
        require(self.request["clusterSSHTransport"] == "verified-tunnel"
                and self.host["name"] == self.request["bootstrap"], "foreign-service")
        unused, staging = tunnel_helpers()
        intent = staging.stage_intent(self.request)
        for name in ("bootstrap-protocol.json", "bootstrap-attempt.json", "bootstrap-enrollment.json"):
            require(canonical(self.markers.read(name)) == canonical(scope), "ownership")
        require(self.markers.read("tunnel-stage.json") == intent, "ownership")
        redeploy = self.markers.read("tunnel-redeploy.json")
        require(isinstance(redeploy, dict) and set(redeploy) == {"scope", "custodyID"}
                and isinstance(redeploy["scope"], dict), "ownership")
        plan = redeploy["scope"].get("plan")
        require(isinstance(plan, dict) and set(plan) == {"version", "intent", "monMetadata", "mgrMetadata"}
                and plan["version"] == "ceph-tunnel-redeploy-v1" and plan["intent"] == intent
                and redeploy["scope"] == self.manager_scope(plan), "ownership")
        for kind in ("mon", "mgr"):
            staging.parse_metadata(canonical(plan[kind + "Metadata"]), kind)
        require(isinstance(redeploy["custodyID"], str) and re.fullmatch(r"[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}", redeploy["custodyID"]) is not None
                and self.markers.read("tunnel-mounted.json") == {"intent": intent, "custodyID": redeploy["custodyID"]}, "ownership")
        require(self.configuration_complete(), "native-tunnel-unproved")
        self.verify_transport_bundle()
        daemons = self.daemons()
        self.verify_manager_mounts(daemons)
        for kind in projections:
            candidates = [daemon for daemon in daemons if daemon.get("name", "").startswith(kind + ".")]
            require(len(candidates) == 1 and candidates[0]["name"] == kind + "." + intent[kind + "ID"], "foreign-service")
            staging.seed_daemon(self.request, candidates[0], observing=False)
        # describe_service includes deleted specs; --export omits them. Equal
        # complete inventories prevent a deleted or hidden peer being adopted.
        descriptions = {}
        for row in self.services_inventory():
            value = dict(row)
            status = value.pop("status", None)
            value.pop("events", None)
            require(isinstance(status, dict) and "created" not in value and "deleted" not in value
                    and "deleted" not in status, "foreign-service")
            kind = value.get("service_type")
            identifier = value.get("service_id")
            require(isinstance(kind, str) and (identifier is None or isinstance(identifier, str)), "foreign-service")
            key = kind + ("." + identifier if identifier else "")
            require(key not in descriptions and value.pop("service_name", key) == key, "foreign-service")
            if value.get("unmanaged") is False:
                value.pop("unmanaged")
            descriptions[key] = value
            if key in projections:
                require(value.get("unmanaged") is True and value == {"service_type": key, "unmanaged": True}
                        and "created" not in status
                        and set(status) <= {"size", "running", "last_refresh", "container_image_id", "container_image_name"}
                        and type(status.get("size")) is int and status["size"] == 0
                        and type(status.get("running")) is int and status["running"] == 1, "foreign-service")
                require(all(isinstance(status[field], str) for field in set(status) - {"size", "running"}), "foreign-service")
                require("container_image_name" not in status or status["container_image_name"] == self.request["image"], "foreign-service")
        require(descriptions == observed, "foreign-service")

    def owned_service_specs(self, expected, scope):
        """Check all peers and intents before publishing any new service effect."""
        observed = self.service_specs()
        require(set(observed) <= set(expected), "foreign-service")
        projections = set()
        for key, value in expected.items():
            intent = {"scope": scope, "service": value}
            attempt = self.markers.read("service-" + key + "-attempt.json")
            complete = self.markers.read("service-" + key + "-complete.json")
            require(attempt is None or attempt == intent, "ownership")
            require(complete is None or complete == intent and attempt == intent, "ownership")
            present = observed.get(key)
            require(complete is None or present == value, "foreign-service")
            if present is None:
                continue
            if key in ("mon", "mgr") and present.get("unmanaged") is True and present == {"service_type": key, "unmanaged": True}:
                require(complete is None, "foreign-service")
                projections.add(key)
            else:
                require(present == value and attempt == intent, "foreign-service")
        if projections:
            self.prove_service_projections(projections, observed, scope)
        return {key: value for key, value in observed.items() if key not in projections}

    def services(self, devices=None, service=None):
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.prove_registry()
        self.provider_registry_absent()
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            require(self.device_identities(devices) is not None, "device-identity")
            self.manager_retry(self.verify_tunnel_configuration)
        self.config_database()
        groups = self.normalized_services([self.service_spec(value) for value in self.request["services"]])
        wanted = [value for group in groups for value in group]
        keys = [value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "") for value in wanted]
        require(len(set(keys)) == len(keys), "foreign-service")
        require(service is None or type(service) is int and 0 <= service < len(groups), "request")
        selected = wanted if service is None else groups[service]
        scope = self.recovery_scope()
        observed = self.owned_service_specs(dict(zip(keys, wanted)), scope)
        pending, completions = [], []
        for value in selected:
            key = value["service_type"] + ("." + value["service_id"] if value.get("service_id") else "")
            prefix = "service-" + key
            intent = {"scope": scope, "service": value}
            attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
            require(attempt is None or attempt == intent, "ownership")
            require(complete is None or complete == intent and attempt == intent, "ownership")
            present = observed.get(key)
            require(present is None or attempt == intent and present == value, "foreign-service")
            require(complete is None or present == value, "foreign-service")
            if present is None:
                pending.append((prefix, intent))
            if complete is None:
                completions.append((prefix, intent))
        for prefix, intent in pending:
            if self.markers.read(prefix + "-attempt.json") is None:
                self.markers.write(prefix + "-attempt.json", intent)
        changed = bool(pending)
        if pending:
            documents = b"\n---\n".join(canonical(intent["service"]) for unused, intent in pending) + b"\n"
            self.tunnel_effect("orch", "apply", "-i", "-", data=documents, json_result=False)
            observed = self.service_specs()
        for prefix, intent in completions:
            require(observed.get(prefix[len("service-"):]) == intent["service"], "not-ready")
            self.markers.write(prefix + "-complete.json", intent)
        return {"changed": changed}

    def dashboard_scope(self):
        """Bind the dashboard credential to the owned cluster incarnation and its bootstrap host."""
        require(self.request["action"] == "cluster" and self.markers.owned() and self.host["name"] == self.request["bootstrap"], "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"], "user": DASHBOARD_USER}

    def dashboard_store(self):
        return Custody(self, "dashboard", "", 64, dashboard_decode)

    def ceph_status(self, *args, data=b""):
        """Only the exit status: no native output, and no input it echoes, leaves this call."""
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            code, unused = self.run(["/usr/bin/ceph", "--conf", os.path.join(self.markers.path, "ceph.conf"),
                                     "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"), *args], data=data, timeout=60)
        return code

    def dashboard_settings(self):
        """Enable the module with TLS on the fixed port; repeat nothing already in place."""
        changed = False
        modules = self.ceph("mgr", "module", "ls")
        require(isinstance(modules, dict) and isinstance(modules.get("enabled_modules"), list) and len(modules["enabled_modules"]) <= 128, "native-output")
        if "dashboard" not in modules["enabled_modules"]:
            self.ceph("mgr", "module", "enable", "dashboard", json_result=False)
            changed = True
        observed = self.ceph("config", "dump")
        require(isinstance(observed, list) and all(isinstance(entry, dict) for entry in observed), "native-output")
        current = {entry.get("name"): entry.get("value") for entry in observed if entry.get("section") == "mgr"}
        for name, value in (("mgr/dashboard/ssl", "true"), ("mgr/dashboard/ssl_server_port", str(DASHBOARD_PORT))):
            if current.get(name) != value:
                self.ceph("config", "set", "mgr", name, value, json_result=False)
                changed = True
        return changed

    def dashboard_certificate(self):
        """The SHA-256 of the stored certificate's DER bytes, or empty while none is stored."""
        keys = self.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 65536 and all(isinstance(key, str) for key in keys), "native-output")
        if DASHBOARD_CERTIFICATE not in keys:
            return ""
        return certificate_sha256(self.ceph_text("config-key", "get", DASHBOARD_CERTIFICATE))

    def dashboard_certify(self, deadline):
        """Create Ceph's self-signed certificate only while none is stored, waiting for the module to load."""
        while True:
            fingerprint = self.dashboard_certificate()
            if fingerprint:
                return fingerprint
            try:
                # Enabling the module restarts the manager; until it serves the module again its command fails or hangs.
                with self.manager_served():
                    code = self.ceph_status("dashboard", "create-self-signed-cert")
            except ManagerRefusal:
                code = None
            if code != 0:
                require(self.clock() < deadline, "native-timeout")
                self.pause(DASHBOARD_INTERVAL)

    def manager_containers(self):
        """Refreshed declared MGR containers, running state and dashboard port publication."""
        with self.manager_served():
            rows = self.ceph("orch", "ps", "--daemon_type", "mgr", "--refresh")
        require(isinstance(rows, list) and len(rows) <= 128, "native-output")
        managers = next(s["placement"]["hosts"] for s in self.request["services"] if s["service_type"] == "mgr")
        result = {}
        for row in rows:
            require(isinstance(row, dict) and row.get("daemon_type") == "mgr" and isinstance(row.get("daemon_id"), str), "native-output")
            require(daemon_identity_host("mgr", row["daemon_id"], managers) == row.get("hostname") and row["daemon_id"] not in result, "foreign-service")
            container = row.get("container_id")
            require(container is None or isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container) is not None, "native-output")
            ports = row.get("ports", [])
            require(isinstance(ports, list) and len(ports) <= 128
                    and all(type(port) is int and 1 <= port <= 65535 for port in ports), "native-output")
            result[row["daemon_id"]] = (container or "", type(row.get("status")) is int and row["status"] == 1, DASHBOARD_PORT in ports)
        require(len({row["hostname"] for row in rows}) == len(rows), "foreign-service")
        containers = [container for container, unused_up, unused_port in result.values() if container]
        require(len(set(containers)) == len(containers), "foreign-service")
        return result

    def dashboard_published(self):
        try:
            with self.manager_served():
                services = self.ceph("mgr", "services")
        except ManagerRefusal:
            return False
        require(isinstance(services, dict), "native-output")
        url = services.get("dashboard", "")
        return isinstance(url, str) and DASHBOARD_URL.fullmatch(url) is not None

    def dashboard_wait_publication(self, deadline):
        while True:
            require(self.clock() < deadline, "native-timeout")
            if self.dashboard_published():
                return
            self.pause(DASHBOARD_INTERVAL)

    def dashboard_schedule_redeploy(self, record, count, deadline):
        """Schedule once every declared MGR runs its recorded container; a container that left the record schedules nothing."""
        while True:
            self.dashboard_wait_publication(deadline)
            running = self.manager_retry(self.manager_containers)
            if any(container and container not in record["containers"] for container, unused_up, unused_port in running.values()):
                return False
            ready = len(running) == count and all(container and up for container, up, unused_port in running.values())
            if ready and self.dashboard_published():
                require(self.clock() < deadline, "native-timeout")
                self.tunnel_effect("orch", "redeploy", "mgr", json_result=False)
                return True
            self.pause(DASHBOARD_INTERVAL)

    def dashboard_repair_ports(self, scope, running, count, deadline):
        """One frozen repair pass. A target still running its recorded container without the port is requested at most once per
        call, standbys and then unattempted targets first: cephadm coalesces a pending redeploy, so a repeat restarts it at most once.
        The active MGR is failed over first, since cephadm would prepare its redeploy from the services map its failover clears, and
        only once every other missing MGR is seen replaced: cephadm runs a scheduled redeploy later, on whichever MGR is then active."""
        name = "dashboard-port-repair.json"
        record = self.markers.read(name)
        if record is None:
            record = {"version": "ceph-dashboard-port-repair-v1", "scope": scope,
                      "containers": {daemon: row[0] for daemon, row in running.items()},
                      "missing": sorted(daemon for daemon, row in running.items() if not row[2])}
            self.markers.write(name, record)
        require(isinstance(record, dict) and set(record) == {"version", "scope", "containers", "missing"}
                and record["version"] == "ceph-dashboard-port-repair-v1" and canonical(record["scope"]) == canonical(scope)
                and isinstance(record["containers"], dict) and len(record["containers"]) == count
                and all(isinstance(daemon, str) and isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container)
                        for daemon, container in record["containers"].items())
                and len(set(record["containers"].values())) == count
                and isinstance(record["missing"], list) and record["missing"]
                and all(isinstance(daemon, str) and daemon in record["containers"] for daemon in record["missing"])
                and len(set(record["missing"])) == len(record["missing"]), "dashboard-manager")
        changed, submitted, failed_over = False, set(), set()
        while True:
            intents = {}
            for daemon in record["missing"]:
                target = {"version": "ceph-dashboard-port-attempt-v1", "scope": scope,
                          "daemon": daemon, "container": record["containers"][daemon]}
                filename = "dashboard-port-" + hashlib.sha256(daemon.encode()).hexdigest() + ".json"
                attempted = self.markers.read(filename)
                require(attempted is None or canonical(attempted) == canonical(target), "dashboard-manager")
                intents[daemon] = (filename, target, attempted)
            self.dashboard_wait_publication(deadline)
            try:
                running = self.manager_containers()
            except ManagerRefusal:
                running = {}
            require(set(running) <= set(record["containers"]), "dashboard-manager")
            if not self.dashboard_published():
                self.pause(DASHBOARD_INTERVAL)
                continue
            if len(running) == count and all(container and up for container, up, unused_port in running.values()):
                missing = {daemon for daemon, row in running.items() if not row[2]}
                require(missing <= set(record["missing"]), "dashboard-manager")
                require(all(running[daemon][0] == record["containers"][daemon] for daemon in missing), "dashboard-manager")
                if not missing:
                    return changed
                manager = self.ceph("mgr", "dump")
                require(isinstance(manager, dict) and isinstance(manager.get("active_name"), str)
                        and type(manager.get("active_gid")) is int, "native-output")
                active = manager["active_name"]
                targets = sorted(missing - submitted, key=lambda daemon: (daemon == active, intents[daemon][2] is not None, daemon))
                if targets and (targets[0] != active or missing == {active}):
                    target = targets[0]
                    require(self.clock() < deadline, "native-timeout")
                    if target == active:
                        require(count >= 2, "dashboard-manager")
                        standbys = manager.get("standbys")
                        require(isinstance(standbys, list) and all(isinstance(item, dict) for item in standbys), "native-output")
                        if target not in failed_over and any(item.get("name") in record["containers"] for item in standbys):
                            failed_over.add(target)
                            self.ceph("mgr", "fail", target, json_result=False)
                            self.manager_wait(previous=manager["active_gid"])
                            changed = True
                            continue
                        self.pause(DASHBOARD_INTERVAL)
                        continue
                    filename, intent, unused_attempt = intents[target]
                    submitted.add(target)
                    proof = self.transport_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
                    with proof, self.cache_hold():
                        self.markers.write(filename, intent)
                        self.ceph("orch", "daemon", "redeploy", "mgr." + target, json_result=False)
                    changed = True
                    continue
            require(self.clock() < deadline, "native-timeout")
            self.pause(DASHBOARD_INTERVAL)

    def dashboard_redeploy(self, scope, deadline):
        """Gate redeployment on publication and prove every running MGR advertises its TLS port."""
        name, done = "dashboard-redeploy.json", "dashboard-redeployed.json"
        placement = next(s["placement"] for s in self.request["services"] if s["service_type"] == "mgr")
        count = placement.get("count", len(placement["hosts"]))
        record = self.markers.read(name)
        changed = False
        if record is None:
            running = self.manager_retry(self.manager_containers)
            require(len(running) == count and all(container and up for container, up, unused_port in running.values()), "not-ready")
            record = {"version": "ceph-dashboard-redeploy-v1", "scope": scope,
                      "containers": sorted(container for container, unused_up, unused_port in running.values())}
            self.markers.write(name, record)
            require(self.dashboard_schedule_redeploy(record, count, deadline), "dashboard-manager")
            changed = True
        require(isinstance(record, dict) and set(record) == {"version", "scope", "containers"} and record["version"] == "ceph-dashboard-redeploy-v1"
                and canonical(record["scope"]) == canonical(scope) and isinstance(record["containers"], list)
                and len(record["containers"]) == count
                and all(isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container) for container in record["containers"])
                and len(set(record["containers"])) == count, "dashboard-manager")
        completed = self.markers.read(done)
        require(completed is None or canonical(completed) == canonical(record), "dashboard-manager")
        reissued = changed
        while True:
            try:
                running = self.manager_containers()
            except ManagerRefusal:
                # The redeploy restarts the manager that serves this view: nothing proved replaced yet.
                running = {}
            replaced = len(running) == count and all(
                container and up and container not in record["containers"] for container, up, unused_port in running.values())
            if replaced and self.dashboard_published():
                if self.markers.read("dashboard-port-repair.json") is not None or not all(row[2] for row in running.values()):
                    changed = self.dashboard_repair_ports(scope, running, count, deadline) or changed
                break
            unchanged = running and all(container in record["containers"] for container, unused_up, unused_port in running.values())
            if completed is None and not reissued and unchanged:
                # A lost response before the scheduling left every MGR unreplaced;
                # a redeploy restarts them and is safe to schedule again. One replaced meanwhile shows the lost one running.
                scheduled = self.dashboard_schedule_redeploy(record, count, deadline)
                reissued, changed = True, changed or scheduled
            require(self.clock() < deadline, "native-timeout")
            self.pause(DASHBOARD_INTERVAL)
        self.markers.write(done, record)
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            self.verify_transport_bundle()
            self.manager_retry(self.verify_tunnel_configuration)
            self.verify_manager_mounts(self.daemons())
        return changed

    def dashboard_users(self):
        users = self.ceph("dashboard", "ac-user-show")
        require(isinstance(users, list) and len(users) <= 4096 and all(isinstance(user, str) for user in users), "native-output")
        return users

    def dashboard_verify(self, password):
        """Ceph keeps only a bcrypt hash; it is checked here and never leaves this worker."""
        user = self.ceph("dashboard", "ac-user-show", DASHBOARD_USER)
        require(isinstance(user, dict) and user.get("username") == DASHBOARD_USER and isinstance(user.get("password"), str), "native-output")
        try:
            # The pinned managed OS's Python 3.9 ships crypt over libxcrypt, which reads bcrypt.
            hasher = importlib.import_module("crypt")
            computed = hasher.crypt(password, user["password"])
        except (ImportError, OSError, ValueError, AttributeError):
            raise Refusal("dashboard-verify") from None
        require(isinstance(computed, str) and computed.startswith("$2"), "dashboard-verify")
        require(hmac.compare_digest(computed, user["password"]), "dashboard-drift")
        require(user.get("roles") == ["administrator"] and user.get("enabled") is True and user.get("pwdUpdateRequired") is False, "dashboard-drift")

    def dashboard(self):
        """Enable the dashboard after readiness and create its administrator from retained custody."""
        self.verify_tool()
        scope = self.dashboard_scope()
        store = self.dashboard_store()
        deadline = self.clock() + DASHBOARD_BUDGET
        password = store.prepare(scope, lambda: dashboard_password().encode("ascii"))
        binding = store.binding(scope, "ceph-dashboard-user-v1")
        attempt, applied = self.markers.read(store.name("attempt")), self.markers.read(store.name("applied"))
        require(attempt is None or canonical(attempt) == canonical(binding), "dashboard-identity")
        require(applied is None or canonical(applied) == canonical(binding), "dashboard-identity")
        changed = self.dashboard_settings()
        if not self.dashboard_certificate():
            self.dashboard_certify(deadline)
            changed = True
        changed = self.dashboard_redeploy(scope, deadline) or changed
        present = DASHBOARD_USER in self.dashboard_users()
        require(not present or attempt is not None, "dashboard-identity")
        require(present or applied is None, "dashboard-identity")
        if not present:
            self.markers.write(store.name("attempt"), binding)
            require(store.material(scope) == password, "dashboard-identity")
            code = self.ceph_status("dashboard", "ac-user-create", DASHBOARD_USER, "-i", "-", "administrator", data=password.encode("ascii"))
            require(code != errno.EINVAL, "dashboard-policy")
            require(code == 0, "native-command")
            changed = True
        self.dashboard_verify(password)
        self.markers.write(store.name("applied"), binding)
        # The password is an opaque protected output; it never enters public evidence.
        return {"changed": changed, "private": password}

    def dashboard_read(self):
        """Recapture the dashboard password read-only; an unfinished phase offers nothing."""
        self.verify_tool()
        scope = self.dashboard_scope()
        store = self.dashboard_store()
        if self.markers.read(store.name("applied")) is None:
            return {}
        store.prove_binding(store.binding(scope, "ceph-dashboard-user-v1"))
        password = store.material(scope)
        require(DASHBOARD_USER in self.dashboard_users(), "dashboard-identity")
        self.dashboard_verify(password)
        return {"private": password}

    def dashboard_observation(self):
        """The active MGR's dashboard URL and the stored certificate's fingerprint; reachability is the controller's proof."""
        services = self.ceph("mgr", "services")
        require(isinstance(services, dict), "native-output")
        url = services.get("dashboard", "")
        if not isinstance(url, str) or DASHBOARD_URL.fullmatch(url) is None:
            url = ""
        return {"url": url, "certificateSHA256": self.dashboard_certificate(), "reachable": False}

    def monitor_addresses(self, expected):
        """Each monitor's public v2 address from the monitor map, keyed by its declared host."""
        dump = self.ceph("mon", "dump")
        require(isinstance(dump, dict) and isinstance(dump.get("mons"), list) and len(dump["mons"]) <= 128, "native-output")
        result = []
        for monitor in dump["mons"]:
            require(isinstance(monitor, dict) and named(monitor.get("name")), "native-output")
            host = daemon_identity_host("mon", monitor["name"], expected)
            vector = monitor.get("public_addrs")
            require(isinstance(vector, dict) and isinstance(vector.get("addrvec"), list), "native-output")
            addresses = [item.get("addr") for item in vector["addrvec"] if isinstance(item, dict) and item.get("type") == "v2"]
            require(len(addresses) == 1 and isinstance(addresses[0], str), "native-output")
            result.append({"host": host, "address": monitor_address(addresses[0])})
        require(len({item["host"] for item in result}) == len(result), "native-output")
        return sorted(result, key=lambda item: item["host"])

    def empty_evidence(self):
        return {"version": EVIDENCE_VERSION, "request": "", "action": self.request["action"], "fsid": self.request["fsid"],
                "owner": self.request["owner"], "owned": self.markers.owned(), "exists": False, "ready": False, "removed": False, "partial": False,
                "health": "", "hosts": [], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [],
                "resource": {"id": "", "path": ""}, "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
                "endpoint": {"certificateSHA256": "", "reachable": False}, "daemons": [], "sso": ""}

    def observe(self):
        if self.recovery_enabled() and self.request["action"] == "cluster" and self.markers.read("bootstrap-enrollment.json") is None:
            return self.bootstrap_state()["evidence"]
        self.blocking = []
        evidence = self.empty_evidence()
        daemons = self.daemons()
        require(evidence["owned"] or not daemons, "foreign-cluster")
        if not daemons:
            # No daemons is not cleanup: disks, key authorization and private
            # cluster files can remain. Their absence has no qualified proof.
            return self.unfinished(evidence)
        try:
            return self.observe_cluster(evidence, daemons)
        except ManagerRefusal:
            # While the active manager restarts its orchestrator cannot answer: unfinished work, never a refusal.
            self.blocking = []
            return self.unfinished(self.empty_evidence())

    def unfinished(self, evidence):
        """Owned work without a current proof: partial, never ready."""
        if self.request["action"] in ("pool", "client", "policy", "rbd", "filesystem", "subvolume"):
            resource = self.request[self.request["action"]]
            evidence["owned"] = self.markers.read(self.request["action"] + "-" + resource["name"] + ".json") == resource
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def readiness_health(self, health):
        """HEALTH_OK, or only checks that never block day-0 readiness; every other check is named as blocking."""
        status = health.get("status", "")
        require(status in ("HEALTH_OK", "HEALTH_WARN", "HEALTH_ERR"), "native-output")
        checks = health.get("checks", {})
        require(isinstance(checks, dict) and len(checks) <= 256 and all(isinstance(name, str) for name in checks), "native-output")
        blocking = set() if status == "HEALTH_OK" else set(checks) - TOLERATED_HEALTH
        self.blocking = sorted(name for name in blocking if HEALTH_CHECK.fullmatch(name))[:16]
        return status, status == "HEALTH_OK" or bool(checks) and not blocking

    def prove_cluster_policy(self):
        prove_fips(self.request)
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        if "security" in self.request:
            keys = self.ceph("auth", "ls")
            require(isinstance(keys, dict) and isinstance(keys.get("auth_dump"), list) and keys["auth_dump"], "cephx-policy")
            for entry in keys["auth_dump"]:
                require(isinstance(entry, dict), "cephx-policy")
                prove_cephx_key(self.request, entry.get("key"))

    def observe_cluster(self, evidence, daemons):
        pending = []
        self.prove_cluster_policy()
        evidence["exists"] = True
        self.prove_registry()
        self.provider_registry_absent()
        if self.request["clusterSSHTransport"] == "verified-tunnel":
            with self.manager_served():
                self.verify_tunnel_configuration()
            if any(str(d.get("name", "")).startswith("mgr.") for d in daemons):
                self.verify_transport_bundle()
                pending = self.verify_manager_mounts(daemons, observing=True)
        self.config_database()
        status = self.ceph("status")
        require(isinstance(status, dict) and status.get("fsid") == self.request["fsid"], "native-output")
        require(isinstance(status.get("health"), dict), "native-output")
        evidence["health"], healthy = self.readiness_health(status["health"])
        license_ready = self.request["action"] != "cluster" or self.license_accepted() or self.selection.variant("ceph.mgr.license-contract") == "absent"
        if not license_ready:
            self.blocking = sorted(set(self.blocking) | {"IBM_LICENSE_NOT_ACCEPTED"})[:16]
        pg_ready = placement_groups_ready(status, self.request["action"])
        self.call_home_state()
        quorum = self.ceph("quorum_status")
        mgr = self.ceph("mgr", "dump")
        with self.manager_served():
            hosts = self.ceph("orch", "host", "ls")
        require(isinstance(quorum, dict) and isinstance(quorum.get("quorum_names"), list), "native-output")
        require(isinstance(mgr, dict) and isinstance(mgr.get("standbys"), list) and isinstance(mgr.get("active_name"), str), "native-output")
        require(all(isinstance(item, dict) and named(item.get("name")) for item in mgr["standbys"]), "native-output")
        require(isinstance(hosts, list) and all(isinstance(host, dict) and named(host.get("hostname")) for host in hosts), "native-output")
        mon_names = quorum.get("quorum_names", [])
        evidence["mgrActive"] = mgr.get("active_name", "")
        evidence["mgrStandbys"] = [m["name"] for m in mgr.get("standbys", [])]
        evidence["hosts"] = [h["hostname"] for h in hosts]
        wanted = {h["name"] for h in self.request["hosts"]}
        require(set(evidence["hosts"]) <= wanted, "foreign-host")
        mon_placement = next(s["placement"] for s in self.request["services"] if s["service_type"] == "mon")
        mgr_placement = next(s["placement"] for s in self.request["services"] if s["service_type"] == "mgr")
        expected_mons = mon_placement["hosts"]
        expected_mgrs = mgr_placement["hosts"]
        mgr_names = [evidence["mgrActive"], *evidence["mgrStandbys"]]
        require(all(named(name) for name in mon_names) and len(set(mon_names)) == len(mon_names), "native-output")
        evidence["monitors"] = [daemon_identity_host("mon", name, expected_mons) for name in mon_names]
        for name in mgr_names:
            if name:
                daemon_identity_host("mgr", name, expected_mgrs)
        require(len(set(name for name in mgr_names if name)) == len([name for name in mgr_names if name]), "native-output")
        ready = set(evidence["hosts"]) == wanted and set(evidence["monitors"]) <= set(expected_mons)
        ready = ready and len(evidence["monitors"]) == mon_placement.get("count", len(expected_mons))
        ready = ready and bool(evidence["mgrActive"]) and len(mgr_names) == mgr_placement.get("count", len(expected_mgrs))
        with self.manager_served():
            running = self.ceph("orch", "ps")
        require(isinstance(running, list), "native-output")
        served = {}
        for daemon in running:
            require(isinstance(daemon, dict) and daemon.get("daemon_type") in DAEMON_TYPES, "foreign-service")
            unused, host_rule, counted, image_rule = DAEMON_TYPES[daemon["daemon_type"]]
            require(daemon.get("hostname") in wanted, "foreign-service")
            if host_rule == "delegated":
                delegated_daemon(daemon, self.request)
            elif host_rule == "identity":
                expected = expected_mons if daemon["daemon_type"] == "mon" else expected_mgrs
                require(daemon_identity_host(daemon["daemon_type"], daemon.get("daemon_id"), expected) == daemon["hostname"], "foreign-service")
            elif host_rule == "service":
                served.setdefault(service_daemon(daemon["daemon_type"], daemon, self.request["services"]), []).append(daemon.get("daemon_id"))
            images = daemon.get("container_image_digests") or []
            require(isinstance(images, list) and all(isinstance(image, str) for image in images), "native-output")
            if image_rule == "ceph":
                require(not images or self.request["image"] in images, "artifact")
            if counted:
                ready = ready and type(daemon.get("status")) is int and daemon["status"] == 1 and self.request["image"] in images
        mon_hosts = {d["daemon_id"]: d["hostname"] for d in running if d["daemon_type"] == "mon"}
        require(len(mon_hosts) == sum(daemon["daemon_type"] == "mon" for daemon in running), "native-output")
        ready = ready and set(mon_hosts) == set(mon_names)
        mgr_hosts = {d.get("daemon_id"): d.get("hostname") for d in running if isinstance(d, dict) and d.get("daemon_type") == "mgr"}
        require(len(mgr_hosts) == sum(daemon["daemon_type"] == "mgr" for daemon in running), "native-output")
        # The MON's manager map can precede the orchestrator's asynchronous
        # daemon cache. Missing expected rows are unfinished, not foreign.
        ready = ready and set(mgr_hosts) == {name for name in mgr_names if name}
        evidence["mgrActive"] = mgr_hosts.get(evidence["mgrActive"], "")
        mapped_standbys = [mgr_hosts[name] for name in evidence["mgrStandbys"] if name in mgr_hosts]
        evidence["mgrStandbys"] = list(dict.fromkeys(mapped_standbys))
        ready = ready and len(set([evidence["mgrActive"], *mapped_standbys])) == len(mgr_names)
        require(all(name in expected_mgrs for name in [evidence["mgrActive"], *evidence["mgrStandbys"]] if name), "foreign-service")
        for service in self.request["services"]:
            if service["service_type"] == "mds":
                identifiers = served.get(service["service_id"], [])
                require(len(set(identifiers)) == len(identifiers), "native-output")
                ready = ready and len(identifiers) == service["placement"].get("count", len(service["placement"]["hosts"]))
        ready = self.observe_osds(evidence, running, ready)
        ready = self.observe_resource(evidence, ready)
        if self.request["action"] == "cluster":
            evidence["monAddrs"] = self.monitor_addresses(expected_mons)
            evidence["dashboard"] = self.dashboard_observation()
        evidence["removed"] = evidence["owned"] and not evidence["exists"]
        # A manager container this host is restarting is unfinished work, named beside the blocking health checks.
        self.blocking = (pending + self.blocking)[:16]
        evidence["ready"] = ready and pg_ready and healthy and license_ready and not pending
        evidence["partial"] = evidence["exists"] and not evidence["ready"]
        return evidence

    def metadata_device_name(self, host, device):
        if not host.get("physical"):
            return os.path.basename(device["path"])
        record = self.markers.read(DEVICE_IDENTITIES)
        self.identity_rows(record)
        found = next(row for row in record["devices"] if row["host"] == host["name"] and row["path"] == device["path"])
        return os.path.basename(found["kernelPath"])

    def expected_device_id(self, host, device):
        """The native ID the host's preparation derived through the image SDK, never a guessed serial."""
        identities = self.device_identities()
        require(identities is not None, "device-identity")
        return identities[(host["name"], device["path"])]

    def observe_osds(self, evidence, running, ready):
        running_osds = {daemon.get("daemon_id"): daemon for daemon in running if daemon["daemon_type"] == "osd"}
        require(len(running_osds) == sum(daemon["daemon_type"] == "osd" for daemon in running), "native-output")
        devices = [(h, d) for h in self.request["hosts"] for d in h["devices"] if d["path"] not in auxiliary_device_paths(self.request, h["name"])]
        metadata = self.ceph("osd", "metadata")
        osdmap = self.ceph("osd", "dump")
        # An OSD the map holds but that has not booted yet reports no metadata beyond its ID; it is starting, not malformed.
        require(isinstance(metadata, list) and all(isinstance(item, dict) and ("device_ids" not in item or isinstance(item["device_ids"], str))
                                                   for item in metadata), "native-output")
        ready = ready and all("device_ids" in item for item in metadata)
        require(isinstance(osdmap, dict) and isinstance(osdmap.get("osds"), list) and all(isinstance(item, dict) for item in osdmap["osds"]), "native-output")
        require(len(running_osds) <= len(devices), "foreign-osd")
        if self.markers.read(DEVICE_IDENTITIES) is None:
            # Before services publish the identities no OSD may exist to prove.
            require(not metadata and not running_osds, "device-identity")
            return False
        for host, device in devices:
            device_id = self.metadata_device_name(host, device) + "=" + self.expected_device_id(host, device)
            groups = osd_device_group(self.request, host, device["path"])
            group_ids = [{self.metadata_device_name(host, d) + "=" + self.expected_device_id(host, d) for d in group}
                         for group in groups.values()]
            allowed_ids = set().union(*group_ids)
            candidates = []
            for candidate in metadata:
                identifiers = candidate.get("device_ids", "").split(",")
                if (candidate.get("hostname") == host["name"] and device_id in identifiers
                        and len(identifiers) == len(set(identifiers)) == len(group_ids)
                        and set(identifiers) <= allowed_ids
                        and all(len(set(identifiers) & group) == 1 for group in group_ids)):
                    candidates.append(candidate)
            require(len(candidates) <= 1, "foreign-osd")
            if not candidates:
                ready = False
                continue
            item = candidates[0]
            osds = [o for o in osdmap["osds"] if o.get("osd") == item.get("id")]
            require(len(osds) == 1, "native-output")
            osd = osds[0]
            evidence["osds"].append({"host": host["name"], "path": device["path"], "id": item["id"], "uuid": osd["uuid"]})
            daemon = running_osds.get(str(item["id"]))
            osd_ready = type(osd.get("up")) is int and osd["up"] == 1 and type(osd.get("in")) is int and osd["in"] == 1
            osd_ready = (osd_ready and daemon is not None and daemon.get("hostname") == host["name"] and type(daemon.get("status")) is int and
                         daemon["status"] == 1 and self.request["image"] in (daemon.get("container_image_digests") or []))
            ready = ready and osd_ready
            if osd_ready:
                # The manager serves each daemon's running configuration.
                with self.manager_served():
                    self.config_effective(item["id"])
        require(len(metadata) <= len(devices), "foreign-osd")
        return ready

    def observe_resource(self, evidence, ready):
        require(self.request["action"] == "cluster")
        return ready

    def destroy(self):
        # Core teardown remains closed until B491 supplies a qualified proof of
        # client quiescence, including sessions that outlive key removal. A pool
        # being empty or the CLI returning success is not that proof.
        observation = self.observe()
        if observation["removed"]:
            return {"changed": False}
        raise Refusal("client-quiescence-unqualified")


def dispatch(envelope):
    APPLIED.clear()
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_replication_native
    if isinstance(envelope, dict) and envelope.get("phase") in ceph_replication_native.PHASES:
        return ceph_replication_native.dispatch(envelope)
    phase = envelope.get("phase") if isinstance(envelope, dict) else None
    if phase == "services" and isinstance(envelope, dict) and "devices" in envelope:
        devices = envelope["devices"]
        envelope = {key: value for key, value in envelope.items() if key != "devices"}
    else:
        devices = None
    if phase == "services" and "service" in envelope:
        service = envelope["service"]
        envelope = {key: value for key, value in envelope.items() if key != "service"}
    else:
        service = None
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private"})
    require(isinstance(envelope["private"], dict))
    if phase in HOST_PHASES:
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
        return ceph_host_runtime.dispatch(envelope)
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_export_native
    if phase in ceph_export_native.PHASES:
        return ceph_export_native.dispatch(envelope)
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_smb_native
    if phase in ceph_smb_native.PHASES:
        return ceph_smb_native.dispatch(envelope)
    from ansible_collections.bootwright.core.plugins.module_utils import (
        ceph_native_nfs, ceph_native_nvmeof, ceph_native_object, ceph_native_resources, ceph_native_units,
    )
    request = envelope["request"]
    resource = isinstance(request, dict) and request.get("action") in ceph_native_resources.ACTIONS
    unit = isinstance(request, dict) and request.get("action") == "service"
    gateway = isinstance(request, dict) and request.get("action") == "gateway"
    export = isinstance(request, dict) and request.get("action") == "nfs-export"
    nvmeof = isinstance(request, dict) and request.get("action") == "nvmeof"
    user = isinstance(request, dict) and request.get("action") == "object-user"
    bucket = isinstance(request, dict) and request.get("action") == "object-bucket"
    rgw = export and isinstance(request.get("nfsExport"), dict) and "objectUser" in request["nfsExport"]
    kind = (ceph_native_nvmeof.NVMeOFWorker if nvmeof else ceph_native_object.ObjectWorker if gateway
            else ceph_native_object.UserWorker if user else ceph_native_object.BucketWorker if bucket
            else ceph_native_units.UnitWorker if unit else ceph_native_object.ExportUserWorker if rgw else ceph_native_nfs.ExportWorker if export
            else ceph_native_resources.ResourceWorker if resource else Worker)
    worker = kind(request, envelope["host"])
    worker.horizon = worker.clock() + MANAGER_HORIZON
    worker.execution_deadline = WORKER_DEADLINE
    require(phase in CORE_PHASES or (resource and phase in ceph_native_resources.PHASES) or (unit and phase in ceph_native_units.PHASES)
            or (gateway and phase in ceph_native_object.PHASES) or (export and phase in ceph_native_nfs.EXPORT_PHASES)
            or (user and phase in ceph_native_object.USER_PHASES) or (bucket and phase in ceph_native_object.BUCKET_PHASES)
            or (nvmeof and phase in ceph_native_nvmeof.PHASES))
    if (phase not in CORE_READ_ONLY_PHASES and phase not in ceph_native_resources.READ_ONLY_PHASES
            and phase not in ceph_native_object.USER_READ_ONLY_PHASES and phase not in ceph_native_nvmeof.READ_ONLY_PHASES
            and phase not in ceph_native_units.READ_ONLY_PHASES):
        require(supported_transport(worker.request), "unsupported-release")
    if gateway:
        worker.tls = ceph_native_object.material(worker.request, phase, envelope["private"])
    if user:
        require(envelope["private"] == {}, "material")
    if bucket:
        worker.tls = ceph_native_object.bucket_material(worker.request, phase, envelope["private"])
    if unit:
        worker.bound = ceph_native_units.material(worker.request, phase, envelope["private"])
    if phase in ("tunnel_preflight", "bootstrap", "authorize", "tunnel_configure"):
        result = getattr(worker, phase)(envelope["private"])
    elif phase == "services":
        result = worker.services(devices, service)
    elif phase == "observe":
        # The health checks that kept readiness unproved travel beside the closed evidence, for the refusal's detail only.
        result = dict(worker.observe(), blocking=list(worker.blocking))
    else:
        result = getattr(worker, phase)()
    APPLIED[:] = sorted(worker.selection.applied)
    return result


def main():
    try:
        data = sys.stdin.buffer.read(MAX_INPUT + 1)
        require(len(data) <= MAX_INPUT)
        result = dispatch(json.loads(data))
        answer = {"ok": True, "result": result}
        if APPLIED:
            answer["specializations"] = list(APPLIED)
        encoded = canonical(answer)
        require(len(encoded) <= MAX_OUTPUT, "native-output")
    except Refusal as error:
        encoded = canonical({"ok": False, "reason": str(error)})
    except Exception:  # No native transcript or private input crosses this boundary.
        encoded = canonical({"ok": False, "reason": "native-unknown"})
    sys.stdout.buffer.write(encoded)


if __name__ == "__main__":
    main()
