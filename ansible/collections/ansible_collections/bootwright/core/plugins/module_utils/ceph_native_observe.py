"""Bounded native daemon inspection without cephadm's logging side effects.

Only the existing public daemon fields are projected. This candidate does not
qualify an image, repair native state, adopt metadata or publish ownership.
The caller verifies the native executables and lends the qualified image UID.
A snapshot that cephadm moved before its re-proof is taken again, bounded; a
contradiction refuses at once.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import json
import os
import re
import stat
import time
import uuid

DATA_ROOT = "/var/lib/ceph"
UNIT_ROOT = "/etc/systemd/system"
ROOT_UID = 0
MAX_DAEMONS = 1024
MAX_ENTRIES = 4096
MAX_METADATA = 4096
CLUSTER_TYPES = frozenset(("mon", "mgr", "osd", "crash", "ceph-exporter", "mds"))
# Each daemon type another block executes, mapped to its cephadm service type (orchestrator daemon_type_to_service).
DELEGATED_TYPES = {"alertmanager": "alertmanager", "grafana": "grafana", "haproxy": "ingress", "keepalived": "ingress", "nfs": "nfs",
                   "node-exporter": "node-exporter", "prometheus": "prometheus", "rgw": "rgw", "snmp-gateway": "snmp-gateway"}
# Monitoring singletons: cephadm names the service by its type and makes each daemon's directory the image's own user's.
SINGLETONS = frozenset(("alertmanager", "grafana", "node-exporter", "prometheus", "snmp-gateway"))
# Services listed by their type alone: the singletons and the Ceph-image ceph-exporter.
LISTED_SINGLETONS = SINGLETONS | {"ceph-exporter"}
TYPES = CLUSTER_TYPES | frozenset(DELEGATED_TYPES)
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
IMAGE = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[0-9a-f]{64}\Z")
HEX = re.compile(r"[0-9a-f]{64}\Z")
CACHE = re.compile(r"cephadm\.[0-9a-f]{64}\Z")
MAX_CACHE = 16 << 20
PROPERTIES = ("Id", "LoadState", "ActiveState", "SubState", "UnitFileState", "FragmentPath", "MainPID")
# cephadm deploys and reconfigures daemons while a readiness wait observes: a moved snapshot is taken again.
SETTLE_ATTEMPTS = 10
SETTLE_PAUSE = 3
SETTLE_HORIZON = 60


class ObservationRefusal(ValueError):
    """A fixed reason which never discloses native output or file contents."""


class ObservationChangedRefusal(ObservationRefusal):
    """A re-proof found the layout moved since its first read: native convergence, never a contradiction."""


def require(condition, changed=False):
    if not condition:
        raise ObservationChangedRefusal("native-observation-changed") if changed else ObservationRefusal("native-observation-unproved")


def current(name, directory):
    """An entry's identity now; one gone since it was listed or opened moved during the snapshot."""
    try:
        return os.stat(name, dir_fd=directory, follow_symlinks=False)
    except FileNotFoundError:
        raise ObservationChangedRefusal("native-observation-changed") from None


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode))


def file_identity(info):
    return identity(info) + (info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def safe_directory(info, owners):
    require(stat.S_ISDIR(info.st_mode) and info.st_uid in owners and info.st_mode & 0o022 == 0)


@contextlib.contextmanager
def fixed_directory(path, uid=None):
    """Open every fixed ancestor without following links or trusting mode alone."""
    descriptors = [os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)]
    chain = []
    try:
        safe_directory(os.fstat(descriptors[0]), (ROOT_UID,))
        parts = path.strip("/").split("/")
        for index, part in enumerate(parts):
            require(part not in ("", ".", ".."))
            try:
                following = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=descriptors[-1])
            except FileNotFoundError:
                require(index == len(parts) - 1)
                yield None
                try:
                    os.stat(part, dir_fd=descriptors[-1], follow_symlinks=False)
                except FileNotFoundError:
                    require(all(identity(current(name, parent)) == before for parent, name, before in chain), changed=True)
                    return
                # Absent at the open and present now: created during the snapshot.
                require(False, changed=True)
            before = os.fstat(following)
            chain.append((descriptors[-1], part, identity(before)))
            descriptors.append(following)
            owners = (ROOT_UID, uid) if index == len(parts) - 1 and type(uid) is int and uid > 0 else (ROOT_UID,)
            safe_directory(before, owners)
        yield descriptors[-1]
        require(all(identity(current(name, parent)) == before for parent, name, before in chain), changed=True)
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


@contextlib.contextmanager
def root_directory(uid=None):
    # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
    # pylint: disable-next=contextmanager-generator-missing-cleanup
    with fixed_directory(DATA_ROOT, uid) as directory:
        yield directory


@contextlib.contextmanager
def child_directory(parent, name, owners, own=False):
    """A non-link directory of the given owners, or with `own` of whichever user it belongs to, re-proved unchanged after use."""
    require(isinstance(name, str) and NAME.fullmatch(name) is not None)
    before = current(name, parent)
    safe_directory(before, tuple(owners) + ((before.st_uid,) if own else ()))
    try:
        descriptor = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    except FileNotFoundError:
        raise ObservationChangedRefusal("native-observation-changed") from None
    try:
        require(identity(os.fstat(descriptor)) == identity(before), changed=True)
        yield descriptor
        require(identity(os.fstat(descriptor)) == identity(before) and identity(current(name, parent)) == identity(before), changed=True)
    finally:
        os.close(descriptor)


def entries(directory):
    # scandir stops on the bound rather than materializing an unlimited list.
    result = []
    os.lseek(directory, 0, os.SEEK_SET)
    with os.scandir(directory) as iterator:
        for item in iterator:
            require(len(result) < MAX_ENTRIES and isinstance(item.name, str))
            result.append(item.name)
    return sorted(result)


def public_file(directory, name, owners):
    """Prove the opened descriptor before reading bounded nonsecret metadata."""
    descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
    with os.fdopen(descriptor, "rb") as handle:
        before = os.fstat(handle.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid in owners and before.st_mode & 0o022 == 0
                and 0 < before.st_size <= MAX_METADATA)
        value = handle.read(MAX_METADATA + 1)
        require(len(value) == before.st_size and file_identity(os.fstat(handle.fileno())) == file_identity(before), changed=True)
        require(file_identity(current(name, directory)) == file_identity(before), changed=True)
        return value, file_identity(before)


@contextlib.contextmanager
def cache_file(directory, name, uid):
    """Prove only provider cache metadata; never read or execute the binary."""
    owners = (ROOT_UID, uid) if type(uid) is int and uid > 0 else (ROOT_UID,)
    try:
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
    except FileNotFoundError:
        raise ObservationChangedRefusal("native-observation-changed") from None
    try:
        before = os.fstat(descriptor)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid in owners)
        require(stat.S_IMODE(before.st_mode) == 0o744 and 0 < before.st_size <= MAX_CACHE)
        require(file_identity(current(name, directory)) == file_identity(before), changed=True)
        yield
        require(file_identity(os.fstat(descriptor)) == file_identity(before), changed=True)
        require(file_identity(current(name, directory)) == file_identity(before), changed=True)
    finally:
        os.close(descriptor)


def object_pairs(pairs):
    result = {}
    for name, value in pairs:
        require(name not in result)
        result[name] = value
    return result


def metadata(directory, kind, owners):
    raw, binding = public_file(directory, "unit.meta", owners)
    try:
        value = json.loads(raw, object_pairs_hook=object_pairs)
    except (ValueError, UnicodeError):
        raise ObservationRefusal("native-observation-unproved") from None
    require(isinstance(value, dict) and isinstance(value.get("service_name"), str))
    service = DELEGATED_TYPES.get(kind, kind)
    require(value["service_name"] == service or value["service_name"].startswith(service + "."))
    # Do not merge arbitrary unit.meta members into evidence as cephadm ls does.
    return binding, value["service_name"]


def unit_state(raw, unit):
    """None while the instance's template is not yet installed: cephadm writes unit files before the systemd unit."""
    require(isinstance(raw, bytes) and 0 < len(raw) <= MAX_METADATA)
    try:
        lines = raw.decode("ascii").splitlines()
    except UnicodeError:
        raise ObservationRefusal("native-observation-unproved") from None
    result = {}
    for line in lines:
        require("=" in line)
        name, value = line.split("=", 1)
        require(name in PROPERTIES and name not in result)
        result[name] = value
    require(set(result) == set(PROPERTIES) and result["Id"] == unit)
    if result["LoadState"] == "not-found":
        require(result["ActiveState"] == "inactive" and result["MainPID"] == "0" and result["FragmentPath"] == "")
        return None
    require(result["LoadState"] == "loaded")
    fsid = unit.split("@", 1)[0]
    require(result["FragmentPath"] == UNIT_ROOT + "/" + fsid + "@.service")
    require(result["MainPID"].isascii() and result["MainPID"].isdecimal() and len(result["MainPID"]) <= 10)
    require(result["UnitFileState"] in ("enabled", "disabled", "static", "indirect"))
    state = result["ActiveState"]
    require(state in ("active", "inactive", "activating", "deactivating", "failed"))
    running = state == "active" and result["SubState"] == "running" and int(result["MainPID"]) > 0
    status = "running" if running else "stopped" if state == "inactive" else "error" if state == "failed" else "unknown"
    return result["UnitFileState"] == "enabled", status, running


def container_state(value, name, image):
    """The container's identity and whether it runs; the caller binds that to its unit. A delegated daemon's image is None:
    its executing block, not this one, compares it."""
    require(isinstance(value, list) and len(value) == 1 and isinstance(value[0], dict))
    item = value[0]
    require(isinstance(item.get("Id"), str) and HEX.fullmatch(item["Id"]) is not None and item.get("Name") == name)
    require(image is None or item.get("ImageName") == image)
    require(isinstance(item.get("Image"), str) and HEX.fullmatch(item["Image"]) is not None)
    state = item.get("State")
    require(isinstance(state, dict) and type(state.get("Running")) is bool)
    return item["Id"], item["Image"], state["Running"]


def image_digests(value, identifier, image):
    require(isinstance(value, list) and len(value) == 1 and isinstance(value[0], dict))
    item = value[0]
    require(item.get("Id") in (identifier, "sha256:" + identifier))
    digests = item.get("RepoDigests")
    require(isinstance(digests, list) and 1 <= len(digests) <= 64 and all(isinstance(item, str) and IMAGE.fullmatch(item) is not None for item in digests))
    require(len(set(digests)) == len(digests) and (image is None or image in digests))
    return sorted(digests)


def unit_files(directory, kind, owners, image):
    """(None, None, None) before cephadm has written the daemon's unit files; else their bindings, service name and image.
    A cluster daemon's written image must be the request's; a delegated daemon's (image None) need only be well formed."""
    try:
        binding, service = metadata(directory, kind, owners)
        bindings = {"unit.meta": binding}
        value, bindings["unit.image"] = public_file(directory, "unit.image", owners)
    except FileNotFoundError:
        return None, None, None
    if image is not None:
        require(value.strip() == image.encode("ascii"))
        return bindings, service, image
    try:
        written = value.strip().decode("ascii")
    except UnicodeError:
        raise ObservationRefusal("native-observation-unproved") from None
    require(0 < len(written) <= 1024 and all(32 < ord(char) < 127 for char in written))
    return bindings, service, written


def delegated_service(name, service, listed):
    """The listed service a delegated daemon belongs to: cephadm names it <type>.<service_id>.<suffix> (get_unique_name with
    prefix=service_id), and its unit.meta, once written, must name that same listed service."""
    kind = name.split(".", 1)[0]
    if kind in SINGLETONS:
        matches = [entry for entry in listed if entry == kind]
    else:
        matches = [entry for entry in listed
                   if "." in entry and entry.split(".", 1)[0] == DELEGATED_TYPES[kind] and name.startswith(kind + "." + entry.split(".", 1)[1] + ".")]
    if service is None:
        require(len(matches) == 1)
        return matches[0]
    require(service in matches)
    return service


def present(command, container):
    """Whether the named container exists: cephadm runs every daemon with --rm, so a stopped one has none."""
    value = command(["/usr/bin/podman", "ps", "--all", "--format", "json", "--filter", "name=^" + container + "$"])
    require(isinstance(value, list) and len(value) <= 1)
    require(all(isinstance(item, dict) and item.get("Names") == [container] for item in value))
    return bool(value)


def daemon(directory, request, name, owners, command, listed=()):
    """A daemon of this FSID without unit files, a loaded unit or a container yet is reported not running. A delegated daemon
    must belong to a service listed for this host, and is never compared with the Ceph image."""
    kind, identifier = name.split(".", 1)
    require(kind in TYPES and NAME.fullmatch(identifier) is not None)
    image = None if kind in DELEGATED_TYPES else request["image"]
    bindings, service, written = unit_files(directory, kind, owners, image)
    if kind in DELEGATED_TYPES:
        delegated_service(name, service, listed)
    unit = "ceph-" + request["fsid"] + "@" + name + ".service"
    result = {"style": "cephadm:v1", "name": name, "fsid": request["fsid"], "systemd_unit": unit.removesuffix(".service"),
              "enabled": False, "state": "unknown", "container_id": "", "container_image_name": "" if bindings is None else written,
              "container_image_id": "", "container_image_digests": []}
    loaded, running = None, False
    argv = ["/usr/bin/systemctl", "show", "--no-pager"] + ["--property=" + field for field in PROPERTIES] + [unit]
    if bindings is not None:
        loaded = unit_state(command(argv, json_result=False), unit)
    if loaded is not None:
        result["enabled"], result["state"], running = loaded
    container = ("ceph-" + request["fsid"] + "-" + kind + "-" + identifier).replace(".", "-")
    value = None
    if present(command, container):
        # Without proved unit files and a loaded unit nothing binds a container to the request.
        require(loaded is not None)
        try:
            value = command(["/usr/bin/podman", "container", "inspect", container])
        except ValueError:
            # Podman exits 125 for a container removed since the listing; only a fresh listing proves that.
            require(not present(command, container))
    if value is None:
        if result["state"] == "running":
            result["state"] = "unknown"
    else:
        container_id, image_id, actual = container_state(value, container, image)
        # An unknown state admits either: a forking cephadm unit is activating while its container already runs, and
        # deactivating while it still runs. Otherwise the unit and its container, read at different moments, agree, or
        # the unit moved since its read; a unit read again unchanged contradicts its container.
        if result["state"] != "unknown" and actual != running:
            require(unit_state(command(argv, json_result=False), unit) == loaded, changed=True)
            require(False)
        digests = image_digests(command(["/usr/bin/podman", "image", "inspect", image_id]), image_id, image)
        result.update(container_id=container_id, container_image_id=image_id, container_image_digests=digests)
    require(bindings is None or all(file_identity(current(name, directory)) == binding for name, binding in bindings.items()), changed=True)
    return result


def daemons(request, uid, command, *, owned, cache_uid=None, clock=time.monotonic, pause=time.sleep, host=None):
    """A settled snapshot: one cephadm moved is taken again, bounded by attempts and a horizon, then refused as changed."""
    deadline = clock() + SETTLE_HORIZON
    attempt = 1
    while True:
        try:
            return snapshot(request, uid, command, owned=owned, cache_uid=cache_uid, host=host)
        except ObservationChangedRefusal:
            if attempt >= SETTLE_ATTEMPTS or clock() + SETTLE_PAUSE >= deadline:
                raise
        attempt += 1
        pause(SETTLE_PAUSE)


def listed_services(request, host):
    """The delegated services the request lists on this host; none without a host."""
    delegated = request.get("delegated", [])
    require(isinstance(delegated, list) and all(isinstance(entry, dict) and isinstance(entry.get("name"), str)
                                                and ("." in entry["name"] or entry["name"] in LISTED_SINGLETONS)
                                                and isinstance(entry.get("hosts"), list) for entry in delegated))
    return [] if host is None else [entry["name"] for entry in delegated if host in entry["hosts"]]


def snapshot(request, uid, command, *, owned, cache_uid=None, host=None):
    """Inspect only the fixed managed host layout; do not infer file custody."""
    try:
        require(type(owned) is bool and (cache_uid is None or type(cache_uid) is int and cache_uid > 0))
        require(isinstance(request, dict) and isinstance(request.get("fsid"), str) and str(uuid.UUID(request["fsid"])) == request["fsid"])
        require(isinstance(request.get("image"), str) and IMAGE.fullmatch(request["image"]) is not None)
        listed = listed_services(request, host)
        with root_directory(uid) as root:
            if root is None:
                return []
            before = entries(root)
            # All dedicated-host entries must be the exact FSID. Foreign and
            # legacy layouts refuse before opening any daemon metadata.
            require(not before or before == [request["fsid"]])
            if not before:
                return []
            require(owned)
            require(type(uid) is int and uid > 0)
            owners = (ROOT_UID, uid)
            with child_directory(root, request["fsid"], owners) as cluster, contextlib.ExitStack() as held:
                names = entries(cluster)
                caches = [name for name in names if CACHE.fullmatch(name) is not None]
                selected = [name for name in names if "." in name and name not in caches]
                require(len(selected) <= MAX_DAEMONS)
                # Native MGR stages a fixed hash-named cephadm binary. Observe
                # its inode only; it is neither daemon evidence nor execution
                # authority. Auxiliary directories likewise expose no contents.
                auxiliaries = [name for name in names if name in ("config", "crash", "removed")]
                # With SELinux enabled cephadm makes a root-owned selinux directory (0755 less the umask) for every OSD or
                # ceph-volume container, including the inventory the MGR runs on each added host.
                labels = [name for name in names if name == "selinux"]
                require(set(names) == set(selected + caches + auxiliaries + labels))
                for name in caches:
                    held.enter_context(cache_file(cluster, name, cache_uid))
                for name in auxiliaries:
                    held.enter_context(child_directory(cluster, name, owners))
                for name in labels:
                    require(stat.S_IMODE(os.fstat(held.enter_context(child_directory(cluster, name, (ROOT_UID,)))).st_mode) & ~0o755 == 0)
                require(all(NAME.fullmatch(name) is not None and name.split(".", 1)[0] in TYPES for name in selected))
                require(len({name.replace(".", "-") for name in selected}) == len(selected))
                result = []
                for name in selected:
                    with child_directory(cluster, name, owners, own=name.split(".", 1)[0] in SINGLETONS) as directory:
                        result.append(daemon(directory, request, name, owners, command, listed))
                require(entries(cluster) == names and entries(root) == before, changed=True)
                return result
    except ObservationRefusal:
        raise
    except (OSError, ValueError, TypeError, AttributeError):
        raise ObservationRefusal("native-observation-unproved") from None
