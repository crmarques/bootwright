"""Native registry credential custody: the retained podman auth file, its preflight, preparation, proof and removal."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import hashlib
import ipaddress
import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    REGISTRY_FILE, REGISTRY_NAME, Refusal, canonical, publish_regular, registry_authority, registry_directory, registry_exists, registry_metadata,
    registry_metadata_record, registry_proxy, registry_read, registry_scope, require,
)

ROUTE_PARENT = "/etc/containers"
ROUTE_DIRECTORY = ROUTE_PARENT + "/containers.conf.d"
ROUTE_PREFIX = "90-bootwright-ceph-"
ROUTE_MARKER = "native-route.json"
ROUTE_VERSION = "ceph-native-route-v1"
ROUTE_LIMIT = 4096
ROUTE_HOST = re.compile(r"[a-z0-9._-]{1,253}\Z")


def route_name(request):
    token = hashlib.sha256((request["fsid"] + "\x00" + request["owner"]).encode()).hexdigest()
    return ROUTE_PREFIX + token + ".conf"


def route_endpoint(request):
    """The frozen registryProxy as podman's engine reads it, rebuilt from its parsed parts so no other byte reaches the file."""
    parsed = registry_proxy(request)
    if parsed is None:
        return None
    host = parsed.hostname
    try:
        literal = ipaddress.ip_address(host)
    except ValueError:
        require(ROUTE_HOST.fullmatch(host) is not None, "request")
        authority = host
    else:
        authority = "[" + literal.compressed + "]" if literal.version == 6 else literal.compressed
    return "http://" + authority + ("" if parsed.port is None else ":" + str(parsed.port))


def route_record(request):
    """The podman engine route a proxied cluster's host keeps, and its record; (None, None) for the direct route. The engine variables
    route only the native client's own pulls: a broker pull sets the same names itself, and podman never overrides a set variable;
    http_proxy = false keeps them out of every container."""
    endpoint = route_endpoint(request)
    if endpoint is None:
        return None, None
    content = ("# Bootwright: this host's native image pulls leave through the Ceph cluster's frozen registryProxy.\n"
               "[engine]\n"
               'env = ["HTTPS_PROXY=' + endpoint + '", "HTTP_PROXY=' + endpoint + '"]\n'
               "\n"
               "[containers]\n"
               "http_proxy = false\n").encode("ascii")
    return {"version": ROUTE_VERSION, "name": route_name(request), "sha256": hashlib.sha256(content).hexdigest()}, content


def protected_directory(fd):
    info = os.fstat(fd)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0, "registry-ownership")


@contextlib.contextmanager
def route_directory(create=False):
    """podman's root drop-in directory, root-owned and writable by root alone; with create, made beneath an equally protected parent.
    Without create, a missing directory raises FileNotFoundError for the caller to read as absence."""
    try:
        parent = os.open(ROUTE_PARENT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except FileNotFoundError:
        if create:
            raise Refusal("registry-ownership") from None
        raise
    try:
        protected_directory(parent)
        name = os.path.basename(ROUTE_DIRECTORY)
        try:
            directory = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
        except FileNotFoundError:
            if not create:
                raise
            with contextlib.suppress(FileExistsError):
                os.mkdir(name, 0o755, dir_fd=parent)
                os.fsync(parent)
            directory = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    finally:
        os.close(parent)
    try:
        protected_directory(directory)
        yield directory
    finally:
        os.close(directory)


def require_exclusive_route(directory, name):
    with os.scandir(directory) as files:
        require(not any(file.name != name and file.name.startswith(ROUTE_PREFIX) and file.name.endswith(".conf")
                        for file in files), "registry-ownership")


def route_read(directory, name):
    """A drop-in's bytes, read once without following a link, or None when the name is absent; any other kind of file refuses."""
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
    except FileNotFoundError:
        return None
    except OSError:
        raise Refusal("registry-ownership") from None
    with os.fdopen(fd, "rb") as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == 0 and info.st_mode & 0o022 == 0
                and info.st_size <= ROUTE_LIMIT, "registry-ownership")
        data = handle.read(ROUTE_LIMIT + 1)
        require(len(data) == info.st_size, "registry-ownership")
        return data


def route_present(request, recorded, remove=False):
    """Whether this cluster's podman engine route remains on the host; with remove, unlink it once its bytes prove it the one
    `recorded` binds. A file under this cluster's name that no record binds refuses."""
    expected, content = route_record(request)
    name = route_name(request)
    try:
        with route_directory() as directory:
            data = route_read(directory, name)
            if data is None:
                return False
            require(expected is not None and recorded == expected and data == content, "registry-ownership")
            if not remove:
                return True
            os.unlink(name, dir_fd=directory)
            os.fsync(directory)
            return False
    except FileNotFoundError:
        return False


def inode_record(metadata):
    return "registry-inode-" + hashlib.sha256(canonical(metadata)).hexdigest() + ".json"


def pending_registry(markers, directory, intent):
    record = markers.read("registry-pending.json")
    if record is not None or not registry_exists(directory, REGISTRY_NAME):
        return record
    # Only descriptor metadata is read until a pre-publication record binds
    # this exact inode. A foreign name or a byte-identical copy cannot bind.
    info = os.stat(REGISTRY_NAME, dir_fd=directory, follow_symlinks=False)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "registry-ownership")
    metadata = registry_metadata(info, pending=True)
    value = {"intent": intent, "metadata": metadata}
    require(markers.read(inode_record(metadata)) == value, "registry-ownership")
    return value


def publish_registry_inode(directory, markers, intent, wanted):
    # Before the descriptor is linked, a process or host death leaves no
    # named credential. After link, the original inode is already bound.
    descriptor = os.open(".", os.O_TMPFILE | os.O_RDWR | os.O_CLOEXEC, 0o600, dir_fd=directory)
    with os.fdopen(descriptor, "wb") as stream:
        os.fchmod(descriptor, 0o600)
        os.fchown(descriptor, 0, 0)
        stream.write(wanted)
        stream.flush()
        os.fsync(descriptor)
        metadata = registry_metadata(os.fstat(descriptor), pending=True)
        registry_metadata_record(metadata, pending=True)
        pending = {"intent": intent, "metadata": metadata}
        markers.write_anonymous(inode_record(metadata), pending)
        os.link("/proc/self/fd/" + str(descriptor), REGISTRY_NAME, dst_dir_fd=directory, follow_symlinks=True)
        os.fsync(directory)
        return pending


class Registry:
    """Native registry credential custody: the retained podman auth file, its preflight, preparation, proof and removal."""

    def __init__(self, worker):
        self.worker = worker

    def registry_intent(self):
        token = hashlib.sha256((self.worker.request["owner"] + "\x00" + self.worker.request["fsid"]).encode()).hexdigest()
        return {"version": "ceph-registry-v1", "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"],
                "host": self.worker.host["name"], "registry": registry_authority(self.worker.request), "path": REGISTRY_FILE,
                "anchor": ".bootwright-registry-" + token}

    def registry_preflight(self, wanted):
        """No mutation or adoption before foreign account and file checks pass."""
        require(self.worker.markers.owned(), "ownership")
        registry_scope(self.worker.request, wanted)
        intent = self.worker.registry_intent()
        recorded = self.worker.markers.read("registry-intent.json")
        ready = self.worker.markers.read("registry-file.json")
        require(recorded is None or recorded == intent, "registry-ownership")
        require(self.worker.markers.read("registry-removed.json") is None, "registry-ownership")
        with registry_directory() as directory:
            pending = pending_registry(self.worker.markers, directory, intent)
            require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
            require(pending is None or recorded == intent, "registry-ownership")
            require(ready is None or pending is not None, "registry-ownership")
            for record, is_pending in ((pending, True), (ready, False)):
                require(record is None or isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent,
                        "registry-ownership")
                if record is not None:
                    registry_metadata_record(record["metadata"], pending=is_pending)
            if pending is not None:
                final = registry_read(directory, REGISTRY_NAME, (ready if ready is not None else pending)["metadata"], pending=ready is None)
                require(final is not None and registry_metadata(final[0], pending=True) == pending["metadata"] and final[1] == wanted, "registry-ownership")
        return intent, pending, ready

    def prepare_registry(self, wanted):
        """Publish the credential using a named inode proof and anonymous contents."""
        intent, pending, ready = self.worker.registry_preflight(wanted)
        if ready is not None:
            self.worker.prove_registry(wanted)
            self.worker.markers.write_anonymous("registry-file.json", ready)
            return False
        self.worker.markers.write_anonymous("registry-intent.json", intent)
        with registry_directory(create=True) as directory:
            if pending is None:
                require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
                pending = publish_registry_inode(directory, self.worker.markers, intent, wanted)
            final = registry_read(directory, REGISTRY_NAME, pending["metadata"], pending=True)
            require(final is not None and final[1] == wanted, "registry-ownership")
            # A previous attempt may have linked the inode and stopped before
            # syncing the directory. Prove durability before completing it.
            os.fsync(directory)
            self.worker.markers.write_anonymous("registry-pending.json", pending)
            self.worker.markers.write_anonymous("registry-file.json", {"intent": intent, "metadata": registry_metadata(final[0])})
        self.worker.prove_registry(wanted)
        return True

    def prove_registry(self, wanted=None):
        require(self.worker.markers.owned(), "ownership")
        intent = self.worker.registry_intent()
        require(self.worker.markers.read("registry-intent.json") == intent and self.worker.markers.read("registry-removed.json") is None, "registry-ownership")
        record = self.worker.markers.read("registry-file.json")
        pending = self.worker.markers.read("registry-pending.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent, "registry-ownership")
        require(isinstance(pending, dict) and set(pending) == {"intent", "metadata"} and pending["intent"] == intent, "registry-ownership")
        registry_metadata_record(record["metadata"])
        registry_metadata_record(pending["metadata"], pending=True)
        with registry_directory() as directory:
            require(not registry_exists(directory, intent["anchor"]), "registry-ownership")
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None, "registry-ownership")
            require(registry_metadata(item[0]) == record["metadata"] and registry_metadata(item[0], pending=True) == pending["metadata"], "registry-ownership")
            registry_scope(self.worker.request, item[1])
            require(wanted is None or item[1] == wanted, "registry-ownership")
        self.prove_route()

    def prepare_route(self):
        """Publish this host's podman engine route before any native pull can need it; a name this cluster's record does not bind refuses.
        The record is durable before the file, and a frozen direct route keeps no record."""
        record, content = route_record(self.worker.request)
        owned = self.worker.markers.read(ROUTE_MARKER)
        require(owned is None or owned == record, "registry-ownership")
        if record is None:
            self.prove_route()
            return False
        with route_directory(create=True) as directory:
            require_exclusive_route(directory, record["name"])
            existing = route_read(directory, record["name"])
            require(existing is None or owned == record and existing == content, "registry-ownership")
            if owned is None:
                self.worker.markers.write(ROUTE_MARKER, record)
            if existing is None:
                publish_regular(directory, record["name"], content, 0)
        self.prove_route()
        return existing is None

    def prove_route(self):
        """The recorded route still holds exactly its bytes; a direct route holds no record."""
        record, content = route_record(self.worker.request)
        require(self.worker.markers.read(ROUTE_MARKER) == record, "registry-ownership")
        try:
            with route_directory() as directory:
                require_exclusive_route(directory, record["name"] if record is not None else None)
                if record is not None:
                    require(route_read(directory, record["name"]) == content, "registry-ownership")
        except FileNotFoundError:
            require(record is None, "registry-ownership")

    def provider_registry_absent(self):
        # Reading names avoids disclosing values. Any stored credential would
        # re-enable provider registry-login/password argv during remote pulls.
        keys = self.worker.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 4096 and all(isinstance(key, str) and len(key) <= 4096 for key in keys), "native-output")
        require(len(set(keys)) == len(keys) and "mgr/cephadm/registry_credentials" not in keys, "registry-credentials-present")

    def remove_registry(self):
        """Internal inverse remains unreachable from the closed destroy phase."""
        proof = {"version": "ceph-native-teardown-v1", "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"],
                 "host": self.worker.host["name"], "removed": True}
        teardown = self.worker.markers.read("native-teardown.json")
        require(self.worker.markers.owned() and isinstance(teardown, dict) and teardown == proof and type(teardown.get("removed")) is bool
                and not self.worker.daemons(), "registry-teardown-unproved")
        record = self.worker.markers.read("registry-file.json")
        require(isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == self.worker.registry_intent(), "registry-ownership")
        registry_metadata_record(record["metadata"])
        pending = self.worker.markers.read("registry-pending.json")
        require(self.worker.markers.read("registry-intent.json") == record["intent"] and isinstance(pending, dict) and set(pending) == {"intent", "metadata"}
                and pending["intent"] == record["intent"], "registry-ownership")
        registry_metadata_record(pending["metadata"], pending=True)
        require(pending["metadata"] == {key: value for key, value in record["metadata"].items() if key != "ctimeNS"}, "registry-ownership")
        removing, removed = self.worker.markers.read("registry-removal.json"), self.worker.markers.read("registry-removed.json")
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
                    self.worker.markers.write("registry-removed.json", record)
                    return removed is None
            require(removed is None, "registry-ownership")
        self.worker.prove_registry()
        self.worker.markers.write("registry-removal.json", record)
        with registry_directory() as directory:
            item = registry_read(directory, REGISTRY_NAME, record["metadata"])
            require(item is not None and registry_metadata(item[0]) == record["metadata"], "registry-ownership")
            registry_scope(self.worker.request, item[1])
            os.unlink(REGISTRY_NAME, dir_fd=directory)
            os.fsync(directory)
        self.worker.markers.write("registry-removed.json", record)
        return True
