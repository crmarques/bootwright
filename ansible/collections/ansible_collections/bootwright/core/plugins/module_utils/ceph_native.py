"""Fixed Ceph worker, sent over the authenticated SSH channel as source.

This file deliberately imports only the target Python standard library and
the shipped canonical-JSON and bounded-process modules. Native
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
import hashlib
import ipaddress
import json
import os
import pwd
import re
import secrets
import stat
import string
import sys
import tempfile
import time
import uuid

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, held_directory, private_files, registry_auth
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

MAX_INPUT = 2 * 1024 * 1024
MAX_OUTPUT = 1024 * 1024
COMMAND_SESSION = True
ROOT = "/var/lib/bootwright/ceph"
PACKAGE_QUERY = "%{EPOCHNUM} %{VERSION} %{RELEASE} %{ARCH}"
ROOT_UID = 0
# Symbolic links one public root-only source may pass through: the crypto-policy back-ends sshd includes are one.
SOURCE_LINKS = 8
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
# A host label: a name, or `_admin`, the one special cephadm label qualified (specs/ceph-native-api.md, hosts and placement).
ADMIN_LABEL, ADMIN_ENTITY = "_admin", "client.admin"
HOST_LABEL = re.compile(r"(?:_admin|[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127})\Z")
HEX = re.compile(r"[0-9a-f]{64}\Z")
DEVICE = re.compile(r"/dev/(?:disk/by-id/|disk/by-serial/)?[a-zA-Z0-9_.:+-]{1,200}\Z")
# A native CRUSH device class name (CrushWrapper::is_valid_crush_name), bounded.
DEVICE_CLASS = re.compile(r"[A-Za-z0-9_.-]{1,64}\Z")
# The reserved form naming, by its serial number, a disk the host's Machine declares; host preparation binds it to a disk.
SERIAL_DEVICE = re.compile(r"/dev/disk/by-serial/([A-Za-z0-9._:+-]{1,128})\Z")
REGISTRY_DIRECTORY = "/etc/ceph"
REGISTRY_NAME = "podman-auth.json"
REGISTRY_FILE = REGISTRY_DIRECTORY + "/" + REGISTRY_NAME
MAX_REGISTRY = 65536
# Artifact selection is authored; the adapter independently proves its SSH
# route rather than treating a version identifier as transport trust.
DEVICE_IDENTITIES = "device-identities.json"
DEVICE_IDENTITIES_VERSION = "ceph-cluster-device-identities-v1"
HOST_PHASES = frozenset(("images", "profile", "route", "device_identity", "host_devices", "host_observe", "fips_observe"))
WORKER_DEADLINE = None

SCOPE_MEMBERS = frozenset(("controller", "version", "cluster", "owner", "fsid", "image", "cephadm", "bootstrap", "hosts", "services", "clusterSSHUser",
                           "clusterSSHTransport", "monIP", "publicNetworks", "clusterNetworks", "callHome", "release", "ansiblePackageVersion",
                           "packageRepository", "packageRepositoryKey", "clusterKeyRef", "registryRef", "registry"))
SCOPE_OPTIONAL = frozenset(("security", "auxiliary", "config", "packages", "nfsGaneshaPackageVersion", "registryTrustBundleRef",
                            "stretch", "singleHostDefaults"))
# The members that name one block's own resource beside its action; everything else a request carries is its cluster's.
RESOURCE_MEMBERS = frozenset(("pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings", "service", "gateway", "nfsExport", "objectUser",
                              "objectBucket", "nvmeof"))
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
                         "cache", "enroll", "license", "services", "stretch_locate", "stretch_enable", "dashboard", "dashboard_read", "dashboard_present",
                         "observe", "unrecorded_observe", "bootstrap_state", "host_state", "destroy", "tiebreaker_observe", "tiebreaker_remove",
                         "physical_host_observe", "physical_host_remove"))
CORE_READ_ONLY_PHASES = frozenset(("prerequisites", "devices", "tunnel_preflight", "observe", "unrecorded_observe", "bootstrap_state", "host_state",
                                   "dashboard_read", "dashboard_present", "tiebreaker_observe", "physical_host_observe"))
# The cluster observation of a read no operation records, such as `bootwright diff --observe`: the only phase that lists the cluster's
# pools and reports an OSD no declared service places instead of refusing it foreign-osd.
UNRECORDED_OBSERVE = "unrecorded_observe"
EVIDENCE_VERSION = "ceph-core-evidence-v1"
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
# cephadm bootstrap under the bootstrap call's own longer worker budget (ceph_transport.BOOTSTRAP_BUDGET), leaving its proofs time.
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
# The provider's serialized license record exceeds the MON's 64 KiB default config-key entry bound; the store refuses it
# while acceptance still reports success.
# util-linux draws lsblk's device tree in the NAME column: without it, JSON output is a flat list whose disks carry no
# children, which hides every partition, logical volume and dm-crypt mapping beneath them.
DEVICE_TREE = ("/usr/bin/lsblk", "--json", "--bytes", "--paths", "--output", "NAME,PATH,TYPE,SERIAL,SIZE,FSTYPE,MOUNTPOINTS")
# A disk this operation's own interrupted OSD creation left behind: ceph-volume names its volume group ceph-<uuid> and the
# block volume osd-block-<OSD fsid>. One bounded native zap clears it, once per disk; see owned_partial_osd.
OSD_CLEAR_VERSION = "ceph-osd-device-clear-v1"
OSD_CLEAR_SECONDS = 120
CEPH_VOLUME_GROUP = re.compile(r"ceph-[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
CEPH_VOLUME_BLOCK = re.compile(r"osd-block-[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
OSD_UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
LVM_UUID = re.compile(r"[A-Za-z0-9]{6}(?:-[A-Za-z0-9]{4}){5}-[A-Za-z0-9]{6}\Z")
LVM_DEVICES_FILE = "/etc/lvm/devices/system.devices"
PROC_ROOT = "/proc"
LICENSE_STORE_OPTION = "mon_config_key_max_entry_size"
LICENSE_STORE_FLOOR = 1 << 20
# Health checks that never block day-0 readiness: call-home egress is the operator's network, not the cluster's state.
TOLERATED_HEALTH = frozenset(("CHA_ERROR_SENDING_REPORT",))
HEALTH_CHECK = re.compile(r"[A-Z][A-Z0-9_]{0,63}\Z")
# Health checks whose detail names the pools or filesystems they are about, as Ceph tentacle (which IBM Storage Ceph 9
# ships) documents them in doc/rados/operations/health-checks.rst and doc/cephfs/health-messages.rst, with every detail
# line each one writes (src/mon/PGMap.cc, src/osd/OSDMap.cc, src/mds/MDSMap.cc and the pg_autoscaler module). Such a check
# keeps from ready only the blocks that own or depend on what it names; a line none of its patterns matches, a check the detail omits and
# a system pool other than `.rgw.root` keep every block from ready.
POOL_NAMED = r"([a-zA-Z0-9_.][a-zA-Z0-9_.-]{0,127})"
FS_NAMED = r"([a-zA-Z0-9][a-zA-Z0-9_.-]{0,127})"
COUNT = r"[0-9]{1,20}"
BYTES = r"[0-9]{1,20}(?:\.[0-9]{1,6})? (?:[KMGTPE]i)?B"
RATIO = r"[0-9]{1,20}(?:\.[0-9]{1,9})?(?:e[+-][0-9]{1,3})?"
POOL_QUOTA = (r"pool '" + POOL_NAMED + r"' has " + COUNT + r" objects \(max " + COUNT + r"\)",
              r"pool '" + POOL_NAMED + r"' has " + BYTES + r" \(max " + BYTES + r"\)")
POOL_PG_COUNT = r"Pool " + POOL_NAMED + r" has " + COUNT + r" placement groups, should have " + COUNT
SCOPED_HEALTH = {code: (kind, tuple(re.compile(pattern + r"\Z") for pattern in patterns)) for code, kind, patterns in (
    ("POOL_APP_NOT_ENABLED", "pool", (r"application not enabled on pool '" + POOL_NAMED + r"'",
                                      re.escape("use 'ceph osd pool application enable <pool-name> <app-name>', where <app-name> is "
                                                "'cephfs', 'rbd', 'rgw', or freeform for custom applications."))),
    ("POOL_FULL", "pool", (r"pool '" + POOL_NAMED + r"' is full \((?:running out of quota|no space)\)",) + POOL_QUOTA),
    ("POOL_NEAR_FULL", "pool", POOL_QUOTA),
    ("POOL_NEARFULL", "pool", (r"pool '" + POOL_NAMED + r"' is nearfull",)),
    ("POOL_BACKFILLFULL", "pool", (r"pool '" + POOL_NAMED + r"' is backfillfull",)),
    ("POOL_PG_NUM_NOT_POWER_OF_TWO", "pool", (r"pool '" + POOL_NAMED + r"' pg_num " + COUNT + r" is not a power of two",)),
    ("POOL_NO_REDUNDANCY", "pool", (r"pool '" + POOL_NAMED + r"' has no replicas configured",)),
    ("POOL_TOO_FEW_PGS", "pool", (POOL_PG_COUNT,)),
    ("POOL_TOO_MANY_PGS", "pool", (POOL_PG_COUNT,)),
    ("POOL_HAS_TARGET_SIZE_BYTES_AND_RATIO", "pool", (r"Pool " + POOL_NAMED + r" has target_size_bytes and target_size_ratio set",)),
    ("SMALLER_PGP_NUM", "pool", (r"pool " + POOL_NAMED + r" pg_num " + COUNT + r" > pgp_num " + COUNT,)),
    ("MANY_OBJECTS_PER_PG", "pool", (r"pool " + POOL_NAMED + r" objects per pg \(" + COUNT + r"\) is more than " + RATIO
                                     + r" times cluster average \(" + COUNT + r"\)",)),
    ("FS_DEGRADED", "fs", (r"fs " + FS_NAMED + r" is degraded",)),
    ("FS_WITH_FAILED_MDS", "fs", (r"fs " + FS_NAMED + r" has " + COUNT + r" failed mdss?",)),
    ("FS_INLINE_DATA_DEPRECATED", "fs", (r"fs " + FS_NAMED + r" has deprecated feature inline_data enabled\.",)),
    ("MDS_ALL_DOWN", "fs", (r"fs " + FS_NAMED + r" is offline because no MDS is active for it\.",)),
    ("MDS_DAMAGE", "fs", (r"fs " + FS_NAMED + r" mds\." + COUNT + r" is damaged",)),
    ("MDS_UP_LESS_THAN_MAX", "fs", (r"fs " + FS_NAMED + r" has " + COUNT + r" MDS online, but wants " + COUNT,)),
)}
MAX_HEALTH_DETAIL = 1024
MAX_HEALTH_MESSAGE = 1024
HEALTH_RGW_ROOT = ".rgw.root"
# A scoped check an unrecorded cluster observation reports as a note, naming one pool or filesystem it is about.
HEALTH_NOTE = re.compile(r"([A-Z][A-Z0-9_]{0,63})@(pool|fs)\.([a-zA-Z0-9_.][a-zA-Z0-9_.-]{0,127})\Z")
# When the monitor map was created, as `ceph mon dump` renders it, and the one
# UTC form evidence carries, so a rendering change between releases moves no
# identity (B867 confirms the value on a live cluster).
MONMAP_CREATED = re.compile(r"([0-9]{4}-[0-9]{2}-[0-9]{2})[T ]([0-9]{2}:[0-9]{2}:[0-9]{2})(?:\.([0-9]{1,9}))?(Z|[+-][0-9]{2}:?[0-9]{2})?\Z")
INCARNATION = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z\Z")
PENDING_DAEMON = re.compile(r"(mon|mgr)\.[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
# A never-booted OSD that names no device, by its ID and OSD fsid: the next apply purges it when an owned clear left it.
PENDING_OSD = re.compile(r"unbooted@osd\.[0-9]{1,5}\.[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
# An OSD of this cluster beyond the declared disks, which only an unrecorded observation reports, with the declared host and
# device it runs on when it names them, and keeps unready; every recorded phase still refuses it foreign-osd.
UNPLACED_OSD = re.compile(r"unplaced@osd\.[0-9]{1,5}\Z")
UNPLACED_LIMIT = 64
# The kernel name `osd metadata` gives an OSD's data device.
KERNEL_DEVICE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:+-]{0,63}\Z")
# The pools a read-only cluster observation inventories: names bounded in length and alphabet, at most POOL_LIMIT of them.
POOL_NAME = re.compile(r"[A-Za-z0-9_.][A-Za-z0-9_.-]{0,62}\Z")
POOL_LIMIT = 128
POOL_APPLICATION = re.compile(r"[A-Za-z0-9_.-]{1,32}\Z")
POOL_APPLICATIONS = 4
POOL_TYPES = {1: "replicated", 3: "erasure"}
POOL_AUTOSCALE = ("on", "off", "warn")
POOL_LISTING = 65536
INT64_MAX = (1 << 63) - 1
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
    if request.get("clusterSSHTransport") != "verifiedTunnel" or not isinstance(image, str):
        return False
    repository, separator, identifier = image.rpartition("@sha256:")
    return bool(repository and separator and HEX.fullmatch(identifier))


def tunnel_helpers():
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel, ceph_tunnel_staging
    return ceph_tunnel, ceph_tunnel_staging


def monmap_time(created):
    """A monitor map creation time in one UTC form; a rendering with no offset is UTC."""
    match = MONMAP_CREATED.fullmatch(created) if isinstance(created, str) else None
    require(match is not None, "native-output")
    day, clock, fraction, zone = match.groups()
    offset = datetime.timedelta()
    if zone not in (None, "Z"):
        hours, minutes = int(zone[1:3]), int(zone[-2:])
        require(hours <= 23 and minutes <= 59, "native-output")
        offset = datetime.timedelta(hours=hours, minutes=minutes) * (1 if zone[0] == "+" else -1)
    try:
        moment = datetime.datetime.strptime(day + "T" + clock, "%Y-%m-%dT%H:%M:%S")
    except ValueError:
        require(False, "native-output")
    moment = moment.replace(microsecond=int((fraction or "0")[:6].ljust(6, "0"))) - offset
    return moment.strftime("%Y-%m-%dT%H:%M:%S.%fZ")


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
        return canonical(registry_auth.document(authority, material["username"], material["password"])) + b"\n"
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
        registry_auth.decoded(entry["auth"])
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
def held(path, reason, owners=(0,), **rules):
    """The held_directory walk to path, its refusal this module's reason."""
    try:
        # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with held_directory.hold(path, owners, **rules) as directory:
            yield directory
    except held_directory.Refused:
        raise Refusal(reason) from None


@contextlib.contextmanager
def registry_directory(create=False):
    """Hold the fixed root-owned native directory without following any link; only it may be created."""
    # The wrapped context closes its descriptors on any exit.
    # pylint: disable-next=contextmanager-generator-missing-cleanup
    with held(REGISTRY_DIRECTORY, "registry-ownership", create=0o755 if create else None, absent=not create) as directory:
        yield directory


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


def store_bound(value):
    """A MON size option as the native CLI reports it: a bounded decimal byte count, nothing else."""
    if isinstance(value, bytes):
        try:
            value = value.decode("ascii")
        except UnicodeError:
            raise Refusal("native-output") from None
    require(isinstance(value, str) and re.fullmatch(r"[0-9]{1,12}", value.strip()) is not None, "native-output")
    return int(value.strip())


def named(value):
    return isinstance(value, str) and NAME.fullmatch(value) is not None


def host_secret_refs(hosts):
    return sorted({ref for host in hosts for ref in (host["sshKeyRef"], host.get("hostKeyRef"), host.get("knownHostsRef")) if ref})


def health_owned(request):
    """The pools and filesystems whose scoped health checks keep this block from ready: those it owns (declares or derives) and
    those its frozen request names as what it depends on. A service unit, settings, a placement policy, an object user or bucket
    (which name only their gateway) and the cluster block name none."""
    action = request["action"]
    pools, filesystems = set(), set()
    if action == "pool":
        pools = {request["pool"]["name"]}
    elif action == "rbd":
        pools = {request["rbd"]["pool"], request["rbd"]["dataPool"]}
    elif action == "filesystem":
        filesystem = request["filesystem"]
        pools, filesystems = {filesystem["metadataPool"], *filesystem["dataPools"]}, {filesystem["name"]}
    elif action == "subvolume":
        pools, filesystems = {request["subvolume"]["pool"]}, {request["subvolume"]["filesystem"]}
    elif action == "gateway":
        gateway = request["gateway"]
        pools = {gateway["indexPool"], gateway["dataPool"], gateway["dataExtraPool"], *gateway["zonePools"], HEALTH_RGW_ROOT}
    elif action == "client":
        client = request["client"]
        pools, filesystems = {client["pool"], *client["dataPools"]}, {client["filesystem"]}
    elif action == "nfs-export":
        filesystems = {request["nfsExport"]["filesystem"]}
    elif action == "nvmeof":
        nvmeof = request["nvmeof"]
        pools = {nvmeof["pool"]} | {name for item in nvmeof["namespaces"] for name in (item["image"]["pool"], item["image"]["dataPool"])}
    return pools - {""}, filesystems - {""}


def health_names(detail, codes):
    """Each scoped check's pool or filesystem names from `ceph health detail`; None for a check this detail omits or whose
    detail holds a line none of its patterns matches."""
    checks = detail.get("checks") if isinstance(detail, dict) else None
    named_by = {}
    for code in codes:
        lines = checks.get(code, {}).get("detail") if isinstance(checks, dict) and isinstance(checks.get(code), dict) else None
        names = set()
        if not isinstance(lines, list) or not lines or len(lines) > MAX_HEALTH_DETAIL:
            names = None
        for line in lines if names is not None else ():
            message = line.get("message") if isinstance(line, dict) else None
            match = None
            if isinstance(message, str) and len(message) <= MAX_HEALTH_MESSAGE:
                match = next(filter(None, (pattern.fullmatch(message) for pattern in SCOPED_HEALTH[code][1])), None)
            if match is None:
                names = None
                break
            names.update(match.groups())
        named_by[code] = names or None
    return named_by


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


def bounded_command(argv, data=b"", timeout=60, limit=MAX_OUTPUT, environment=None, cancel=None):
    """Drain both pipes within a hard byte/time budget; never invoke a shell. A set cancel event stops the command as a timeout."""
    require(argv and os.path.isabs(argv[0]), "executable")
    require(all(isinstance(v, str) and "\x00" not in v for v in argv), "executable")
    native_environment = command_environment(environment)
    try:
        code, output, stderr = bounded_process.run(argv, data, timeout, limit, environment=native_environment, session=COMMAND_SESSION, cancel=cancel)
    except bounded_process.Failure as failure:
        raise Refusal("native-" + failure.kind) from None
    return code, NativeOutput(output, stderr)


def read_fips():
    """The two files fips-mode-setup --check reads; None for one that cannot be read."""
    values = {}
    for name, path in (("kernel", "/proc/sys/crypto/fips_enabled"), ("policy", "/etc/crypto-policies/state/current")):
        try:
            if name == "kernel":
                with open(path, "rb") as stream:
                    raw = stream.read(8)
            else:
                raw = read_regular(path, maximum=4096)
            values[name] = raw.decode("ascii").strip()
        except (OSError, UnicodeError, Refusal):
            values[name] = None
    return values


def fips_proved(values):
    """FIPS mode is exactly kernel 1 and policy FIPS; FIPS:OSPP selects a subpolicy and proves nothing."""
    return values == {"kernel": "1", "policy": "FIPS"}


def prove_fips(request):
    if not request.get("security", {}).get("fips"):
        return
    values = read_fips()
    if None in values.values():
        raise NativeFailure("fips-policy", "FIPS host policy", b"the host kernel or inherited crypto policy cannot be proved")
    if not fips_proved(values):
        raise NativeFailure("fips-policy", "FIPS host policy", b"the host kernel and inherited crypto policy must both report FIPS mode")


def observe_fips(request, host):
    """A provided host of a FIPS cluster: read-only, before any preparation; refuses fips-not-enabled."""
    require(request.get("security", {}).get("fips") is True and host.get("provided") is True, "request")
    values = read_fips()
    if not fips_proved(values):
        detail = ", ".join(name + "=" + ("unreadable" if value is None else repr(value)[:40]) for name, value in sorted(values.items()))
        raise NativeFailure("fips-not-enabled", "FIPS mode of provided host " + host["machine"], detail.encode())
    return values


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
    require(required <= set(request) and not set(request) - required - SCOPE_OPTIONAL - {"delegated"} - RESOURCE_MEMBERS)
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_validate
    hosts = ceph_native_validate.scope_checks(request)
    validate_delegated(request, hosts)
    if request["action"] == "cluster":
        require(not RESOURCE_MEMBERS & set(request))
        return request
    require(request["action"] == "service" or "service" not in request)
    if request["action"] == "service":
        require(not (RESOURCE_MEMBERS - {"service"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_units
        ceph_native_units.validate(request, hosts)
        return request
    require(request["action"] == "gateway" or "gateway" not in request)
    if request["action"] == "gateway":
        require(not (RESOURCE_MEMBERS - {"gateway"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate(request, hosts)
        return request
    require(request["action"] == "object-user" or "objectUser" not in request)
    if request["action"] == "object-user":
        require(not (RESOURCE_MEMBERS - {"objectUser"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate_user(request, hosts)
        return request
    require(request["action"] == "object-bucket" or "objectBucket" not in request)
    if request["action"] == "object-bucket":
        require(not (RESOURCE_MEMBERS - {"objectBucket"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        ceph_native_object.validate_bucket(request, hosts)
        return request
    require(request["action"] == "nvmeof" or "nvmeof" not in request)
    if request["action"] == "nvmeof":
        require(not (RESOURCE_MEMBERS - {"nvmeof"}) & set(request))
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nvmeof
        ceph_native_nvmeof.validate(request, hosts)
        return request
    require(request["action"] == "nfs-export" or "nfsExport" not in request)
    if request["action"] == "nfs-export":
        require(not (RESOURCE_MEMBERS - {"nfsExport"}) & set(request))
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
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_validate
    ceph_native_validate.scope_checks(scope)
    return scope


def host_scope(request):
    """Bind the one host scope a cluster-action request or a host block's scope carries."""
    if isinstance(request, dict) and "action" in request:
        validate(request)
        require(request["action"] == "cluster")
        return {key: value for key, value in request.items() if key != "action"}
    return validate_scope(request)


def package_proxy(value):
    """A resolved CONNECT endpoint, never credentials or an ambient route."""
    import urllib.parse
    require(isinstance(value, str) and len(value) <= 2048, "packages")
    if not value:
        return None
    require(value.isascii() and not any(ord(char) <= 32 or ord(char) >= 127 for char in value), "packages")
    try:
        parsed = urllib.parse.urlsplit(value)
        require(parsed.scheme == "http" and parsed.hostname and parsed.username is None and parsed.password is None
                and parsed.path == parsed.query == parsed.fragment == "" and (parsed.port is None or 0 < parsed.port <= 65535)
                and "\\" not in value and "%" not in value, "packages")
    except ValueError:
        raise Refusal("packages") from None
    return parsed


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


def cluster_config(request):
    """The frozen options, including the explicit single-host test profile."""
    config = {section: dict(values) for section, values in request.get("config", {}).items()}
    if request.get("singleHostDefaults"):
        config.setdefault("global", {}).update(osd_crush_chooseleaf_type="0", osd_pool_default_size="2", osd_pool_default_min_size="2")
        config["mgr"] = {"mgr_standby_modules": "false"}
    return config


def bootstrap_config(request):
    """Generate deterministic INI only after the closed request validates."""
    validate(request)
    content = ("[global]\npublic_network = " + ",".join(request["publicNetworks"]) +
               "\nauth_cluster_required = cephx\nauth_service_required = cephx\nauth_client_required = cephx\n")
    config = cluster_config(request)
    for name, value in sorted(config.get("global", {}).items()):
        content += name + " = " + value + "\n"
    for section, values in sorted(config.items()):
        if section == "global":
            continue
        content += "\n[" + section + "]\n"
        for name, value in sorted(values.items()):
            content += name + " = " + value + "\n"
    return content.encode()


def admin_monitors(request):
    """The MONs an admin command names: the seed's monIP, then each MON-placed host's canonical IP inside a public network, by name.

    The retained bootstrap configuration names only the seed, so the list overrides its mon_host and never rewrites the file; it routes
    only: admin_argv requires CephX of the client, so a listener on a listed address that is not the cluster's MON is refused."""
    networks = [ipaddress.ip_network(network) for network in request["publicNetworks"]]
    placed = next(service["placement"]["hosts"] for service in request["services"] if service["service_type"] == "mon")
    result = [str(ipaddress.ip_address(request["monIP"]))]
    for host in sorted((h for h in request["hosts"] if h["name"] in placed), key=lambda h: h["name"]):
        try:
            address = ipaddress.ip_address(host["address"])
        except ValueError:
            continue
        if str(address) == host["address"] and str(address) not in result and any(address in network for network in networks):
            result.append(str(address))
    return ",".join("[" + address + "]" if ":" in address else address for address in result)


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
        require(set(value) in ({"path"}, {"path", "crush_device_class"}), "device-unqualified")
        require(isinstance(value.get("crush_device_class", ""), str) and DEVICE_CLASS.fullmatch(value.get("crush_device_class", "-")) is not None,
                "device-unqualified")
        value = value["path"]
    require(isinstance(value, str) and DEVICE.fullmatch(value) is not None, "device-unproved")
    return value


def device_class(spec, value):
    """The class cephadm gives a data path's OSD: the path record's own, else the service's (ceph-volume's precedence)."""
    if isinstance(value, dict) and value.get("crush_device_class"):
        return value["crush_device_class"]
    return spec.get("crush_device_class", "")


def class_groups(spec):
    """A drive group's data paths by the class their OSDs take, in class order."""
    groups = {}
    for path in spec["data_devices"]["paths"]:
        groups.setdefault(device_class(spec, path), []).append(path)
    return dict(sorted(groups.items()))


def osd_device_class(request, host, data_path):
    for service in request["services"]:
        if service["service_type"] == "osd" and host["name"] in service["placement"]["hosts"]:
            for path in service["spec"]["data_devices"]["paths"]:
                if selected_device(host, path)["path"] == data_path:
                    return device_class(service["spec"], path)
    raise Refusal("device-unproved")


def selected_device(host, value):
    path = device_path(value)
    # A virtio alias names a virtual disk's serial; a physical host selects only by its frozen path.
    found = [d for d in host["devices"] if path == d["path"] or not host.get("physical") and path == "/dev/disk/by-id/virtio-" + d["serial"]]
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
    if private:
        try:
            data = private_files.read(path, maximum)
        except private_files.Refused:
            raise Refusal("file") from None
        require(data, "file")
        return data
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.geteuid(), "file")
        require(info.st_mode & 0o022 == 0 and 0 < info.st_size <= maximum, "file")
        data = handle.read(maximum + 1)
        require(len(data) == info.st_size, "file")
        return data


def volume_tool_running():
    """Whether any process on the host runs ceph-volume, whose own OSD creation may be writing a disk right now."""
    names = os.listdir(PROC_ROOT)
    require(len(names) <= 1 << 20, "osd-partial")
    for name in names:
        if not name.isdigit():
            continue
        try:
            fd = os.open(os.path.join(PROC_ROOT, name, "cmdline"), os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        except (FileNotFoundError, ProcessLookupError):
            continue
        try:
            arguments = os.read(fd, 1 << 16).split(b"\0")
        except ProcessLookupError:
            continue
        finally:
            os.close(fd)
        if any(os.path.basename(argument) == b"ceph-volume" for argument in arguments):
            return True
    return False


def never_booted(item):
    """An OSD `ceph osd new` created that never booted: down, never up, and still flagged new."""
    state = item.get("state")
    return (type(item.get("up")) is int and item["up"] == 0 and type(item.get("up_from")) is int and item["up_from"] == 0
            and isinstance(state, list) and "exists" in state and "new" in state)


def pool_reading(entry):
    """The values a pool observation compares, from one `osd pool ls detail` entry; a value of another type or bound reads empty."""
    def count(field):
        value = entry.get(field)
        return value if type(value) is int and 0 <= value <= INT64_MAX else 0
    metadata = entry.get("application_metadata")
    applications = sorted(name for name in metadata if isinstance(name, str) and POOL_APPLICATION.fullmatch(name)) if isinstance(metadata, dict) else []
    mode = entry.get("pg_autoscale_mode")
    return {"name": entry["pool_name"], "type": POOL_TYPES.get(entry.get("type"), "") if type(entry.get("type")) is int else "",
            "size": count("size"), "minSize": count("min_size"), "applications": applications[:POOL_APPLICATIONS],
            "autoscale": mode if mode in POOL_AUTOSCALE else "", "maxBytes": count("quota_max_bytes"), "maxObjects": count("quota_max_objects")}


def pool_inventory(listed):
    """The bounded pool readings of one `osd pool ls detail` listing, sorted by name, and how many it leaves out."""
    require(isinstance(listed, list) and len(listed) <= POOL_LISTING and all(isinstance(entry, dict) for entry in listed), "native-output")
    named_entries = [entry for entry in listed if isinstance(entry.get("pool_name"), str) and POOL_NAME.fullmatch(entry["pool_name"])]
    names = [entry["pool_name"] for entry in named_entries]
    require(len(set(names)) == len(names), "native-output")
    kept = sorted(named_entries, key=lambda entry: entry["pool_name"])[:POOL_LIMIT]
    return [pool_reading(entry) for entry in kept], len(listed) - len(kept)


def unstarted_osd(item, osds):
    """The one map entry of an OSD whose metadata names no device and that never booted, or None: no disk proves it, and it proves no readiness."""
    entries = [value for value in osds if value.get("osd") == item.get("id")]
    if type(item.get("id")) is int and "device_ids" not in item and not item.get("devices") and len(entries) == 1 and never_booted(entries[0]):
        return entries[0]
    return None


def lvm_devices_lists(identifier):
    """Whether the host's LVM devices file holds an entry for the PV of this unhyphenated UUID."""
    try:
        content = read_regular(LVM_DEVICES_FILE, maximum=1 << 20)
    except FileNotFoundError:
        return False
    return any(b"PVID=" + identifier.encode("ascii") in line.split() for line in content.splitlines() if not line.lstrip().startswith(b"#"))


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


def dashboard_tls_decode(data):
    """A bounded native-generated pair; malformed private output never becomes a diagnostic."""
    require(isinstance(data, bytes) and 0 < len(data) <= 65536, "dashboard-tls-identity")
    try:
        pair = json.loads(data)
    except (ValueError, UnicodeError):
        raise Refusal("dashboard-tls-identity") from None
    require(isinstance(pair, dict) and set(pair) == {"cert", "key"}
            and all(isinstance(value, str) and 0 < len(value) <= 32768 for value in pair.values()), "dashboard-tls-identity")
    certificate_sha256(pair["cert"].encode("utf-8"))
    require(any(pair["key"].startswith("-----BEGIN " + label + "-----\n")
                and pair["key"].rstrip().endswith("-----END " + label + "-----")
                for label in ("RSA PRIVATE KEY", "PRIVATE KEY")), "dashboard-tls-identity")
    return pair


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
    if request.get("singleHostDefaults"):
        result["singleHostDefaults"] = True
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
        if record is not None and record != marker_identity(self.request):
            from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
            ceph_native_stretch.prove_retained(self.request, owner=record)
        require(record is None or record == marker_identity(self.request), "ownership")
        return record is not None

    def create(self):
        with held(self.path, "ownership", owners=(0, os.geteuid()), create=0o700):
            if not self.owned():
                self.write("owner.json", marker_identity(self.request))

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
        # The wrapped context closes its descriptors on any exit.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with held(self.markers.path, self.reason, final=lambda info: info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o700) as directory:
            yield directory

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
        self.bound_devices = None
        self._profile = None
        self.host_markers = None
        self.clock = time.monotonic
        self.pause = time.sleep
        self.horizon = None
        self.execution_deadline = None
        self.command_deadline = None
        self.blocking = []
        self.base_ready = False
        # Only an unrecorded observation reports what no declared service explains instead of refusing it.
        self.reading = False
        self.unplaced = []
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_monitoring_template, native_specializations
        self.selection = native_specializations.Selection(
            {"host-cephadm": self.verified_tool, "image-cephadm": self.image_payload,
             "image-monitoring": lambda: ceph_monitoring_template.image_sources(self)},
            {"image-monitoring": ceph_monitoring_template.blanket_https})
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
        if self.command_deadline is not None:
            timeout = max(1, min(timeout, self.command_deadline - self.clock()))
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

    def admin_argv(self, program):
        """A native admin client on the retained bootstrap configuration and keyring, reaching any MON admin_monitors names and
        accepting only a CephX-authenticated one: the retained file sets no client auth, whose default also admits none."""
        return [program, "--conf", os.path.join(self.markers.path, "ceph.conf"), "-m", admin_monitors(self.request),
                "--auth_client_required=cephx", "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring")]

    def ceph(self, *args, data=b"", json_result=True, secrets=(), timeout=60):
        with self.bootstrap_hold():
            return self.command([*self.admin_argv("/usr/bin/ceph"), "--format", "json", *args],
                                data=data, json_result=json_result, secrets=secrets, timeout=timeout)

    def recovery_enabled(self):
        return self.markers.read("bootstrap-scope.json") is not None

    def ceph_text(self, *args):
        """Primitive native output still holds the same original admin custody."""
        with self.bootstrap_hold():
            return self.command([*self.admin_argv("/usr/bin/ceph"), *args], json_result=False)

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

    def manager_instance(self):
        """The MON's available active manager, or None while none is available or the map cannot be read."""
        try:
            manager = self.ceph("mgr", "dump")
        except Refusal:
            return None
        if not isinstance(manager, dict) or manager.get("available") is not True or type(manager.get("active_gid")) is not int:
            return None
        return manager["active_gid"]

    def manager_declare(self, action, *args, module=None, **options):
        """Repeat an idempotent manager-served declaration, bounded, only while the manager it was sent to restarts or loads what serves
        it: a failure is the command's own answer, raised at once, only when the available active manager read before the attempt still
        holds one interval later and its orchestrator and, where named, the declaring module's read-only `module` command answer. A
        manager first read after a failed attempt is the one the next attempt asks; every command in the wait ends by its deadline."""
        deadline = self.wait_deadline()
        with self.commands_by(deadline):
            asked = self.manager_instance()
            while True:
                cause = None
                try:
                    with self.manager_served():
                        return action(*args, **options)
                except ManagerRefusal as error:
                    cause = getattr(error, "native_cause", error)
                if self.clock() + MANAGER_INTERVAL >= deadline:
                    raise cause
                self.pause(MANAGER_INTERVAL)
                answers = self.orchestrator_answers() and (module is None or self.module_answers(module))
                settled = self.manager_instance()
                if (answers and settled is not None and settled == asked) or self.clock() + MANAGER_INTERVAL >= deadline:
                    raise cause
                asked = settled

    @contextlib.contextmanager
    def commands_by(self, deadline):
        """Each native command started within ends by `deadline`, or an enclosing one's earlier deadline."""
        previous = self.command_deadline
        self.command_deadline = deadline if previous is None else min(previous, deadline)
        try:
            yield
        finally:
            self.command_deadline = previous

    def module_answers(self, command):
        """A manager module's read-only command answers: the restarted manager has loaded that module."""
        try:
            with self.manager_served():
                self.ceph(*command, json_result=False)
        except ManagerRefusal:
            return False
        return True

    def orchestrator_answers(self):
        """The active manager's orchestrator answers with cephadm: a restarted manager has finished loading it."""
        try:
            with self.manager_served():
                status = self.ceph("orch", "status")
        except ManagerRefusal:
            return False
        return isinstance(status, dict) and status.get("available") is True and status.get("backend") == "cephadm"

    def manager_wait(self, previous=None, name=None):
        """After an action that restarts the active manager: wait, bounded, until the MON maps an available active manager other
        than the restarted instance, the named one where the caller knows it, and its orchestrator answers with cephadm."""
        deadline = self.wait_deadline()
        while True:
            manager = self.ceph("mgr", "dump")
            require(isinstance(manager, dict), "native-output")
            if (manager.get("available") is True and manager.get("active_gid") != previous and name in (None, manager.get("active_name"))
                    and self.orchestrator_answers()):
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
        retained = self.markers.read("bootstrap-scope.json")
        if canonical(retained) != canonical(scope):
            from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
            ceph_native_stretch.prove_retained(self.request, scope=retained)
        require(canonical(retained) == canonical(scope), "ownership")
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
        config = cluster_config(self.request)
        if not config:
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
        except ceph_native_observe.ObservationChangedRefusal as error:
            # cephadm kept moving this host's layout through every bounded snapshot: unfinished, never foreign. Each refusal
            # keeps the observation's traceback, so its cause names the site that refused, never its values.
            raise Refusal("not-ready").with_traceback(error.__traceback__) from None
        except ceph_native_observe.ObservationRefusal as error:
            raise Refusal("native-observation-unproved").with_traceback(error.__traceback__) from None
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
        elif owned and any(str(d.get("name", "")).startswith("mgr.") for d in daemons):
            self.verify_transport_bundle()
            pending = self.verify_manager_mounts(daemons, observing=True)
            if pending:
                return {"name": self.host["name"], "owned": owned, "exists": exists, "pending": pending}
        if owned and any(str(d.get("name", "")).startswith("osd.") for d in daemons):
            self.prove_completed_osd_variants(physical)
        return {"name": self.host["name"], "owned": owned, "exists": exists}

    def prove_completed_osd_variants(self, physical=None, services=None):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).prove_completed_osd_variants(physical, services)

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
        if self.request["action"] == "cluster":
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
        unused, staging = tunnel_helpers()
        self.markers.write("bootstrap-scope.json", staging.recovery_scope(self.request))
        return {"changed": not owned}

    def prove_preparation(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_preparation()

    def preparation_state(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).preparation_state()

    def registry_intent(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).registry_intent()

    def registry_preflight(self, wanted):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).registry_preflight(wanted)

    def prepare_registry(self, wanted):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).prepare_registry(wanted)

    def prove_registry(self, wanted=None):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).prove_registry(wanted)

    def provider_registry_absent(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).provider_registry_absent()

    def remove_registry(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_registry
        return ceph_native_registry.Registry(self).remove_registry()

    def osd_device_scope(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).osd_device_scope()

    def osd_data_specs(self, actual):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).osd_data_specs(actual)

    def owned_partial_osd(self, actual, item, signatures, listed, rows, device, alias):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).owned_partial_osd(actual, item, signatures, listed, rows, device, alias)

    @staticmethod
    def osd_clear_marker(alias, stage=b"clear"):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices.osd_clear_marker(alias, stage)

    @classmethod
    def osd_cleared_marker(cls, alias):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices.osd_cleared_marker(alias)

    def purge_orphan_osds(self, orphans):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).purge_orphan_osds(orphans)

    @staticmethod
    def osd_purge_marker(osd):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices.osd_purge_marker(osd)

    @staticmethod
    def osd_identity_marker(path):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices.osd_identity_marker(path)

    def stable_device_path(self, host, device, *, submission=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).stable_device_path(host, device, submission=submission)

    def node_of(self, path, physical=None):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).node_of(path, physical)

    def physical_devices(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).physical_devices()

    def devices(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).devices()

    def osd_prepare(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).osd_prepare()

    def authorize(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).authorize(private)

    def tunnel_guest_policy(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_guest_policy()

    def tunnel_policy_preflight(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_policy_preflight()

    def transport_scope(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).transport_scope()

    def tunnel_preflight(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_preflight(private)

    def _tunnel_host_keys(self, render):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self)._tunnel_host_keys(render)

    def transport(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).transport(private)

    def bootstrap(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).bootstrap(private)

    def tunnel_bootstrap(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_bootstrap(private)

    def prove_inert_host_preparation(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_inert_host_preparation()

    def prove_bootstrap_absence(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_bootstrap_absence()

    def prove_seed(self, daemons, observing=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_seed(daemons, observing)

    def bootstrap_state(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).bootstrap_state()

    def tunnel_effect(self, *arguments, **options):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_effect(*arguments, **options)

    def cache_hold(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).cache_hold()

    def cache(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).cache()

    def configuration_intent(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).configuration_intent()

    def configuration_keys(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).configuration_keys()

    def configure_setting(self, name, wanted, command, mutate):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).configure_setting(name, wanted, command, mutate)

    def call_home_acceptance(self, optional=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).call_home_acceptance(optional)

    def initialize_call_home(self, mutate):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).initialize_call_home(mutate)

    def configure_call_home(self, mutate):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).configure_call_home(mutate)

    def configuration_complete(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).configuration_complete()

    def tunnel_configure(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_configure(private)

    def manager_scope(self, plan):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).manager_scope(plan)

    def tunnel_mount(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).tunnel_mount()

    def prove_mounted_seed(self, daemons, observing=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_mounted_seed(daemons, observing)

    def native_source(self, path, maximum=MAX_INPUT, private=True, root_only=False, allowed_uid=None):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).native_source(path, maximum, private, root_only, allowed_uid)

    def native_file(self, path, maximum=MAX_INPUT, private=True, root_only=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).native_file(path, maximum, private, root_only)

    def transport_hold(self, settle=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).transport_hold(settle)

    def verify_transport_bundle(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).verify_transport_bundle()

    def prove_mount_source(self, source, exact):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).prove_mount_source(source, exact)

    def verify_manager_mounts(self, daemons, observing=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).verify_manager_mounts(daemons, observing)

    def verify_tunnel_configuration(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_bootstrap
        return ceph_native_bootstrap.Bootstrap(self).verify_tunnel_configuration()

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
            if host["location"]:
                spec["location"] = host["location"]
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
            self.license_store_capacity()
            self.manager_retry(self.tunnel_effect, "orch", "accept-license", "--image", self.request["image"], json_result=False)
            require(self.license_accepted(), "not-ready")
            changed = True
        changed = self.admin_keyring() or changed
        return {"changed": changed}

    def admin_keyring(self):
        """A node declaring `_admin` receives the cluster configuration and admin keyring under /etc/ceph through cephadm's client-keyring
        rule, which bootstrap's --skip-admin-label leaves unset (B1074); a client.admin rule placed otherwise is not this cluster's."""
        if not any(ADMIN_LABEL in (host["labels"] or []) for host in self.request["hosts"]):
            return False
        if self.admin_keyring_held():
            return False
        self.manager_retry(self.tunnel_effect, "orch", "client-keyring", "set", ADMIN_ENTITY, "label:" + ADMIN_LABEL, json_result=False)
        require(self.admin_keyring_held(), "not-ready")
        return True

    def admin_keyring_held(self):
        rules = self.manager_retry(self.ceph, "orch", "client-keyring", "ls")
        require(isinstance(rules, list) and len(rules) <= 256 and all(isinstance(rule, dict) for rule in rules), "native-output")
        for rule in rules:
            if rule.get("entity") == ADMIN_ENTITY:
                require(rule.get("placement") in ({"label": ADMIN_LABEL}, "label:" + ADMIN_LABEL), "ownership")
                return True
        return False

    def license_store_capacity(self):
        """Raise, never lower, the MON config-key entry bound the provider's license record needs, then prove each quorum MON holds it.

        The provider's store write reports success to its caller even when the MON refuses an oversize entry, so the bound is
        proved before acceptance rather than inferred from it.
        """
        if store_bound(self.ceph_text("config", "get", "mon", LICENSE_STORE_OPTION)) < LICENSE_STORE_FLOOR:
            self.ceph("config", "set", "mon", LICENSE_STORE_OPTION, str(LICENSE_STORE_FLOOR), json_result=False)
            require(store_bound(self.ceph_text("config", "get", "mon", LICENSE_STORE_OPTION)) >= LICENSE_STORE_FLOOR, "not-ready")
        monitors = self.ceph("mon", "dump")
        require(isinstance(monitors, dict) and isinstance(monitors.get("mons"), list) and 0 < len(monitors["mons"]) <= 16
                and isinstance(monitors.get("quorum"), list), "native-output")
        names = []
        for monitor in monitors["mons"]:
            require(isinstance(monitor, dict) and named(monitor.get("name")) and type(monitor.get("rank")) is int, "native-output")
            if monitor["rank"] in monitors["quorum"]:
                names.append(monitor["name"])
        require(names, "not-ready")
        deadline = self.wait_deadline()
        for name in names:
            while True:
                held = self.ceph("tell", "mon." + name, "config", "get", LICENSE_STORE_OPTION)
                require(isinstance(held, dict) and set(held) == {LICENSE_STORE_OPTION}, "native-output")
                if store_bound(held[LICENSE_STORE_OPTION]) >= LICENSE_STORE_FLOOR:
                    break
                require(self.clock() + MANAGER_INTERVAL < deadline, "not-ready")
                self.pause(MANAGER_INTERVAL)

    def device_identities(self, publish=None):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).device_identities(publish)

    def identity_rows(self, record):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        return ceph_native_osd.Devices(self).identity_rows(record)

    def service_specs(self, limit=None):
        values = self.ceph("orch", "ls", "--export")
        width = max([len(class_groups(s["spec"])) for s in self.request["services"] if s["service_type"] == "osd"] + [1])
        limit = len(self.request["services"]) * len(self.request["hosts"]) * width if limit is None else limit
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
        if value["service_type"] == "mgr":
            helper, unused = tunnel_helpers()
            value["extra_container_args"] = ["--volume=" + os.path.join(self.markers.path, "transport") + ":" + helper.CONTAINER_ROOT + ":ro,z"]
        if value["service_type"] != "osd":
            return [value]
        result = []
        # cephadm keeps no path's class in its stored spec, so each class is one document with a spec-level class.
        groups = class_groups(value["spec"])
        for host in self.request["hosts"]:
            if host["name"] not in value["placement"]["hosts"]:
                continue
            for crush_class, paths in groups.items():
                projected = json.loads(canonical(value))
                projected["placement"] = {"hosts": [host["name"]]}
                seed = [value["service_id"], host["name"]] + ([crush_class] if len(groups) > 1 else [])
                projected["service_id"] = value["service_id"][:46] + "-" + hashlib.sha256(canonical(seed)).hexdigest()[:16]
                projected["spec"].pop("crush_device_class", None)
                if crush_class:
                    projected["spec"]["crush_device_class"] = crush_class
                for field in ("data_devices", "db_devices", "wal_devices"):
                    if field in value["spec"]:
                        projected["spec"][field]["paths"] = [
                            self.stable_device_path(host, selected_device(host, path), submission=True)
                            for path in (paths if field == "data_devices" else value["spec"][field]["paths"])
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
                            and spec.get("crush_device_class") == original["spec"].get("crush_device_class")
                            and not any(spec.get(field) for field in ("journal_devices", "data_directories"))
                            and spec.get("osds_per_device", 1) == 1, "osd-unqualified")
        if retained is None:
            self.markers.write(record, {"version": "ceph-service-inputs-v1", "scope": scope, "services": result})
        return result

    def prove_service_projections(self, projections, observed, scope):
        """Admit only descriptions synthesized from this operation's remaining seed daemons."""
        require(self.host["name"] == self.request["bootstrap"], "foreign-service")
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

    def services(self, devices=None, service=None, orphans=None):
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.prove_registry()
        self.provider_registry_absent()
        require(self.device_identities(devices) is not None, "device-identity")
        purged = self.purge_orphan_osds(orphans or [])
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
        return {"changed": changed or purged}

    def stretch_locate(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.Activation(self).locate()

    def stretch_enable(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.Activation(self).enable()

    def tiebreaker_observe(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.TiebreakerRemoval(self).observe()

    def tiebreaker_remove(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.TiebreakerRemoval(self).remove()

    def dashboard_scope(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_scope()

    def dashboard_store(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_store()

    def ceph_status(self, *args, data=b""):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).ceph_status(*args, data=data)

    def manager_containers(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).manager_containers()

    def dashboard_published(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_published()

    def dashboard_redeploy(self, scope, deadline):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_redeploy(scope, deadline)

    def dashboard_verify(self, password):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_verify(password)

    def dashboard(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard()

    def dashboard_retained(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_retained()

    def dashboard_read(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_read()

    def dashboard_present(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_present()

    def dashboard_observation(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_dashboard
        return ceph_native_dashboard.Dashboard(self).dashboard_observation()

    def monitor_addresses(self, expected, dump=None):
        """Each monitor's public v2 address from the monitor map, keyed by its declared host."""
        dump = self.ceph("mon", "dump") if dump is None else dump
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

    def monmap_incarnation(self):
        """The creation time of this cluster's monitor map in its one UTC form, with the map it was read from."""
        dump = self.ceph("mon", "dump")
        require(isinstance(dump, dict) and dump.get("fsid") == self.request["fsid"], "native-output")
        return monmap_time(dump.get("created")), dump

    def empty_evidence(self):
        evidence = {"version": EVIDENCE_VERSION, "request": "", "action": self.request["action"], "fsid": self.request["fsid"],
                    "owner": self.request["owner"], "owned": self.markers.owned(), "exists": False, "ready": False, "removed": False, "partial": False,
                    "health": "", "hosts": [], "monitors": [], "monAddrs": [], "mgrActive": "", "mgrStandbys": [], "osds": [],
                    "resource": {"id": "", "path": ""}, "client": "", "dashboard": {"url": "", "certificateSHA256": "", "reachable": False},
                    "endpoint": {"certificateSHA256": "", "reachable": False}, "daemons": [], "sso": "", "incarnation": ""}
        if self.request["action"] == "cluster" and "stretch" in self.request:
            evidence["stretch"] = {"stage": "", "enabled": False}
        return evidence

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
        """HEALTH_OK, or only checks that never block day-0 readiness or that name only pools or filesystems this block neither
        owns nor depends on; every other check is named as blocking. An unrecorded cluster observation names each scoped check it passed as a note."""
        status = health.get("status", "")
        require(status in ("HEALTH_OK", "HEALTH_WARN", "HEALTH_ERR"), "native-output")
        checks = health.get("checks", {})
        require(isinstance(checks, dict) and len(checks) <= 256 and all(isinstance(name, str) for name in checks), "native-output")
        blocking = set() if status == "HEALTH_OK" else set(checks) - TOLERATED_HEALTH
        scoped = sorted(name for name in blocking if name in SCOPED_HEALTH)
        notes = []
        if scoped:
            named_by = health_names(self.ceph("health", "detail"), scoped)
            pools, filesystems = health_owned(self.request)
            for name in scoped:
                kind, names = SCOPED_HEALTH[name][0], named_by[name]
                if names is None or names & (pools if kind == "pool" else filesystems) or any(
                        kind == "pool" and entry.startswith(".") and entry != HEALTH_RGW_ROOT for entry in names):
                    continue
                blocking.discard(name)
                notes += [name + "@" + kind + "." + entry for entry in sorted(names)]
        self.blocking = sorted(name for name in blocking if HEALTH_CHECK.fullmatch(name))[:16]
        if self.reading and self.request["action"] == "cluster":
            self.blocking = (self.blocking + [note for note in notes if HEALTH_NOTE.fullmatch(note)])[:16]
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
        evidence["exists"], (evidence["incarnation"], monmap) = True, self.monmap_incarnation()
        self.prove_registry()
        self.provider_registry_absent()
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
        self.observe_cluster_reads(evidence, expected_mons, monmap)
        evidence["removed"] = evidence["owned"] and not evidence["exists"]
        # A manager container this host is restarting is unfinished work, named beside the blocking health checks.
        self.blocking = (pending + self.blocking)[:16]
        evidence["ready"] = self.with_stretch(evidence, ready and pg_ready and healthy and license_ready and not pending)
        evidence["partial"] = evidence["exists"] and not evidence["ready"]
        return evidence

    def observe_cluster_reads(self, evidence, expected_mons, monmap):
        """A cluster block's monitor addresses and dashboard; only an unrecorded observation also lists its pools and the OSDs no
        declared service places."""
        if self.request["action"] != "cluster":
            return
        evidence["monAddrs"] = self.monitor_addresses(expected_mons, monmap)
        evidence["dashboard"] = self.dashboard_observation()
        if self.reading:
            pools, omitted = pool_inventory(self.ceph("osd", "pool", "ls", "detail"))
            evidence["inventory"] = {"pools": pools, "omitted": omitted, "osds": self.unplaced}

    def with_stretch(self, evidence, ready):
        """A stretch cluster is ready only once its activation is verified and holds; the rest of its readiness is kept for activation."""
        if self.request["action"] != "cluster" or "stretch" not in self.request:
            return ready
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        self.base_ready = ready
        evidence["stretch"], held = ceph_native_stretch.Activation(self).observe()
        return ready and held

    def published_row(self, host, device):
        """The device identity row the cluster's published record holds for one host's device; none refuses."""
        record = self.markers.read(DEVICE_IDENTITIES)
        require(record is not None, "device-identity")
        self.identity_rows(record)
        rows = [row for row in record["devices"] if row["host"] == host["name"] and row["path"] == device["path"]]
        require(len(rows) == 1, "device-identity")
        return rows[0]

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

    def unplaced_osds(self, metadata, osdmap, excluded, known_uuids, devices):
        """Each OSD of this map beyond the declared disks, by ID: its host only when the request declares it, its one data device
        only there and when no declared data device of that host, and its OSD fsid only when well formed and not yet named."""
        hosts = {host["name"]: set() for host in self.request["hosts"]}
        for host, device in devices:
            hosts[host["name"]].add(self.metadata_device_name(host, device))
        unplaced, uuids = [], set(known_uuids)
        for item in metadata:
            osd_id = item.get("id")
            if type(osd_id) is not int or not 0 <= osd_id <= 99999 or osd_id in excluded:
                continue
            device, host = item.get("devices"), item.get("hostname")
            host = host if isinstance(host, str) and host in hosts else ""
            placed = host and isinstance(device, str) and KERNEL_DEVICE.fullmatch(device) is not None and device not in hosts[host]
            entries = [entry for entry in osdmap["osds"] if entry.get("osd") == osd_id]
            uuid = entries[0].get("uuid") if len(entries) == 1 else None
            uuid = uuid if isinstance(uuid, str) and OSD_UUID.fullmatch(uuid) and uuid not in uuids else ""
            uuids.add(uuid)
            unplaced.append({"host": host, "id": osd_id, "path": "/dev/" + device if placed else "", "uuid": uuid})
        require(len(unplaced) <= UNPLACED_LIMIT, "foreign-osd")
        return sorted(unplaced, key=lambda osd: osd["id"])

    def osd_classes(self):
        """Each OSD's CRUSH device class in the OSD tree; an OSD the tree lists without one is absent."""
        tree = self.ceph("osd", "tree")
        require(isinstance(tree, dict) and isinstance(tree.get("nodes"), list) and len(tree["nodes"]) <= 65536, "native-output")
        classes = {}
        for node in tree["nodes"]:
            require(isinstance(node, dict) and type(node.get("id")) is int and isinstance(node.get("type"), str), "native-output")
            if node["type"] == "osd" and node.get("device_class") is not None:
                require(isinstance(node["device_class"], str) and node["id"] not in classes, "native-output")
                classes[node["id"]] = node["device_class"]
        return classes

    def observe_osds(self, evidence, running, ready):
        self.unplaced = []
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
        require(self.reading or len(running_osds) <= len(devices), "foreign-osd")
        if self.markers.read(DEVICE_IDENTITIES) is None:
            # Before services publish the identities no OSD may exist to prove.
            require(not metadata and not running_osds, "device-identity")
            return False
        matched, classes = set(), None
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
            matched.add(item.get("id"))
            osds = [o for o in osdmap["osds"] if o.get("osd") == item.get("id")]
            require(len(osds) == 1, "native-output")
            osd = osds[0]
            evidence["osds"].append({"host": host["name"], "path": device["path"], "id": item["id"], "uuid": osd["uuid"]})
            daemon = running_osds.get(str(item["id"]))
            osd_ready = type(osd.get("up")) is int and osd["up"] == 1 and type(osd.get("in")) is int and osd["in"] == 1
            osd_ready = (osd_ready and daemon is not None and daemon.get("hostname") == host["name"] and type(daemon.get("status")) is int and
                         daemon["status"] == 1 and self.request["image"] in (daemon.get("container_image_digests") or []))
            declared = osd_device_class(self.request, host, device["path"])
            if declared:
                # A booted OSD keeps the class it took; only one still starting may yet take the declared one.
                classes = self.osd_classes() if classes is None else classes
                realized = classes.get(item["id"])
                require(realized in (None, declared) or not osd_ready, "osd-class-drift")
                osd_ready = osd_ready and realized == declared
            ready = ready and osd_ready
            if osd_ready:
                # The manager serves each daemon's running configuration.
                with self.manager_served():
                    self.config_effective(item["id"])
        # A never-booted OSD naming no device, such as the one an interrupted creation leaves once an owned clear zapped
        # its volume, counts against no declared disk and keeps the cluster unready, so the next apply reaches its purge.
        unstarted = [entry for entry in (unstarted_osd(item, osdmap["osds"]) for item in metadata) if entry is not None]
        if self.reading:
            excluded = matched | {entry.get("osd") for entry in unstarted}
            self.unplaced = self.unplaced_osds(metadata, osdmap, excluded, [osd["uuid"] for osd in evidence["osds"]], devices)
        require(len(running_osds) <= len(devices) + len(self.unplaced), "foreign-osd")
        require(len(metadata) - len(unstarted) <= len(devices) + len(self.unplaced), "foreign-osd")
        named = ["unbooted@osd." + str(entry["osd"]) + "." + str(entry.get("uuid")) for entry in unstarted]
        named += ["unplaced@osd." + str(osd["id"]) for osd in self.unplaced]
        self.blocking = ([entry for entry in named if PENDING_OSD.fullmatch(entry) or UNPLACED_OSD.fullmatch(entry)] + self.blocking)[:16]
        return ready and not self.unplaced

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


class PhysicalHostRemovalWorker(Worker):
    """The frozen physical host scope; native removal stays FSID-bound."""

    admit = staticmethod(validate_scope)

    def physical_host_observe(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.PhysicalHostRemoval(self).observe()

    def physical_host_remove(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_stretch
        return ceph_native_stretch.PhysicalHostRemoval(self).remove()


PRIVATE_PHASES = ("tunnel_preflight", "bootstrap", "authorize", "tunnel_configure")
SERVICES_OPTIONS = ("devices", "service", "orphans")
PHYSICAL_PHASES = ("physical_host_observe", "physical_host_remove")


def services_arguments(phase, envelope):
    """The services phase's optional device, service and orphan selections, taken out of the envelope before its exact key check."""
    if phase != "services":
        return envelope, {}
    options = {name: envelope[name] for name in SERVICES_OPTIONS if name in envelope}
    envelope = {key: value for key, value in envelope.items() if key not in options}
    if "orphans" in options:
        orphans = options["orphans"]
        require(isinstance(orphans, list) and 0 < len(orphans) <= 4096 and len(set(orphans)) == len(orphans)
                and all(isinstance(value, str) and OSD_UUID.fullmatch(value) is not None for value in orphans), "request")
    return envelope, options


def bind_gateway(worker, phase, private):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
    worker.tls = ceph_native_object.material(worker.request, phase, private)


def bind_user(worker, phase, private):
    require(private == {}, "material")


def bind_bucket(worker, phase, private):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
    worker.tls = ceph_native_object.bucket_material(worker.request, phase, private)


def bind_unit(worker, phase, private):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_units
    worker.bound = ceph_native_units.material(worker.request, phase, private)


def action_routes():
    """Each request action's worker class, the phases it adds to the core phases, and how it binds its private material."""
    from ansible_collections.bootwright.core.plugins.module_utils import (
        ceph_native_nfs, ceph_native_nvmeof, ceph_native_object, ceph_native_resources, ceph_native_units,
    )
    routes = {action: (ceph_native_resources.ResourceWorker, ceph_native_resources.PHASES, None) for action in ceph_native_resources.ACTIONS}
    routes["service"] = (ceph_native_units.UnitWorker, ceph_native_units.PHASES, bind_unit)
    routes["gateway"] = (ceph_native_object.ObjectWorker, ceph_native_object.PHASES, bind_gateway)
    routes["nfs-export"] = (ceph_native_nfs.ExportWorker, ceph_native_nfs.EXPORT_PHASES, None)
    routes["nvmeof"] = (ceph_native_nvmeof.NVMeOFWorker, ceph_native_nvmeof.PHASES, None)
    routes["object-user"] = (ceph_native_object.UserWorker, ceph_native_object.USER_PHASES, bind_user)
    routes["object-bucket"] = (ceph_native_object.BucketWorker, ceph_native_object.BUCKET_PHASES, bind_bucket)
    return routes


def route(request, phase):
    """The worker class, extra phases and material binding of one request action and phase; any other action runs the core worker."""
    action = request.get("action") if isinstance(request, dict) else None
    kind, phases, bind = action_routes().get(action if isinstance(action, str) else None, (Worker, frozenset(), None))
    if phase in PHYSICAL_PHASES:
        kind = PhysicalHostRemovalWorker
    elif action == "nfs-export" and isinstance(request.get("nfsExport"), dict) and "objectUser" in request["nfsExport"]:
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object
        kind = ceph_native_object.ExportUserWorker
    return kind, phases, bind


def read_only_phases():
    from ansible_collections.bootwright.core.plugins.module_utils import (
        ceph_native_nvmeof, ceph_native_object, ceph_native_resources, ceph_native_units,
    )
    return (CORE_READ_ONLY_PHASES | ceph_native_resources.READ_ONLY_PHASES | ceph_native_object.USER_READ_ONLY_PHASES
            | ceph_native_nvmeof.READ_ONLY_PHASES | ceph_native_units.READ_ONLY_PHASES)


def run_private(worker, phase, envelope, options):
    return getattr(worker, phase)(envelope["private"])


def run_services(worker, phase, envelope, options):
    if "orphans" not in options:
        return worker.services(options.get("devices"), options.get("service"))
    return worker.services(options.get("devices"), options.get("service"), options["orphans"])


def run_observe(worker, phase, envelope, options):
    require(phase == "observe" or worker.request["action"] == "cluster", "request")
    worker.reading = phase == UNRECORDED_OBSERVE
    # The health checks that kept readiness unproved travel beside the closed evidence, for the refusal's detail only.
    result = dict(worker.observe(), blocking=list(worker.blocking))
    if worker.request["action"] == "cluster" and "stretch" in worker.request:
        # Everything but stretch activation is ready: the controller activates before its final readiness wait.
        result["activation"] = worker.base_ready and not result["ready"]
    return result


def run_plain(worker, phase, envelope, options):
    return getattr(worker, phase)()


PHASE_RUNNERS = {"services": run_services, "observe": run_observe, UNRECORDED_OBSERVE: run_observe,
                 **{phase: run_private for phase in PRIVATE_PHASES}}


def dispatch(envelope):
    APPLIED.clear()
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_replication_native
    if isinstance(envelope, dict) and envelope.get("phase") in ceph_replication_native.PHASES:
        return ceph_replication_native.dispatch(envelope)
    phase = envelope.get("phase") if isinstance(envelope, dict) else None
    if phase in HOST_PHASES:
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
        return ceph_host_runtime.dispatch(envelope)
    envelope, options = services_arguments(phase, envelope)
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private"})
    require(isinstance(envelope["private"], dict))
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_admin_credentials_native, ceph_export_native, ceph_smb_native
    for delegate in (ceph_admin_credentials_native, ceph_export_native, ceph_smb_native):
        if phase in delegate.PHASES:
            return delegate.dispatch(envelope)
    request = envelope["request"]
    kind, phases, bind = route(request, phase)
    worker = kind(request, envelope["host"])
    worker.horizon = worker.clock() + MANAGER_HORIZON
    worker.execution_deadline = WORKER_DEADLINE
    require(phase in CORE_PHASES or phase in phases)
    if phase not in read_only_phases():
        require(supported_transport(worker.request), "unsupported-release")
    if bind is not None:
        bind(worker, phase, envelope["private"])
    result = PHASE_RUNNERS.get(phase, run_plain)(worker, phase, envelope, options)
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
