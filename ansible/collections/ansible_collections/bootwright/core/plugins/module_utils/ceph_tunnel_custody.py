"""Original tunnel and initial-MGR material custody, never secret digests.

Only the fixed private payload slots are supported. A recorded descriptor,
not a matching filename or matching bytes, permits a private read. Originals
stay outside the manager's SELinux-relabeled bind mount and remain immutable.
An interruption before the first inode reservation refuses an unbound orphan;
no provider effect has been admitted at that boundary.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import json
import os
import stat
import uuid

ROOT = "/var/lib/bootwright/ceph"
ROOT_UID = 0
MAX_MATERIAL = 4 * 1024 * 1024
SOURCE_LINKS = 8
SLOTS = {"transport-original": frozenset(("identity", "known_hosts", "outer_config", "native_config", "proxy.py", "manifest.json")),
         "initial-mgr-original": frozenset(("config", "keyring")),
         "bootstrap-original": frozenset(("config", "keyring"))}
BASE_FIELDS = frozenset(("dev", "inode", "uid", "gid", "mode"))
FULL_FIELDS = BASE_FIELDS | {"size", "mtimeNS", "ctimeNS"}


class CustodyRefusal(ValueError):
    """A fixed reason without paths, payloads or derived secret identifiers."""


def require(condition):
    if not condition:
        raise CustodyRefusal("native-tunnel-custody-unproved")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def metadata(info, reserved=False):
    value = {"dev": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode)}
    if not reserved:
        value.update(size=info.st_size, mtimeNS=info.st_mtime_ns, ctimeNS=info.st_ctime_ns)
    return value


def valid_metadata(value, uid=ROOT_UID, reserved=False):
    require(isinstance(value, dict) and set(value) == (BASE_FIELDS if reserved else FULL_FIELDS))
    require(all(type(item) is int and 0 <= item < 1 << 64 for item in value.values()))
    require(value["inode"] > 0 and value["uid"] == value["gid"] == uid and value["mode"] == 0o600)
    require(reserved or 0 < value["size"] <= MAX_MATERIAL)


@contextlib.contextmanager
def directory(path):
    require(os.path.dirname(path) == ROOT and str(uuid.UUID(os.path.basename(path))) == os.path.basename(path))
    current = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for component in path.strip("/").split("/"):
            require(component not in ("", ".", ".."))
            info = os.fstat(current)
            require(info.st_uid == ROOT_UID and info.st_mode & 0o022 == 0)
            following = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
            os.close(current)
            current = following
        info = os.fstat(current)
        require(info.st_uid == ROOT_UID and stat.S_IMODE(info.st_mode) == 0o700)
        yield current
    finally:
        os.close(current)


def listed(descriptor):
    """The sorted names a held directory holds now.

    A listing reads on from the descriptor's offset, and whether it shows an
    entry created since the descriptor was opened or last rewound is
    unspecified: btrfs omits it. Rewinding the same descriptor first lists the
    directory as it is, so the listing stays bound to the identity proved."""
    os.lseek(descriptor, 0, os.SEEK_SET)
    return sorted(os.listdir(descriptor))


def exists(parent, name):
    try:
        os.stat(name, dir_fd=parent, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False


def encode(slot, value):
    require(slot in SLOTS and isinstance(value, dict) and set(value) == SLOTS[slot])
    require(all(isinstance(item, bytes) and 0 < len(item) <= MAX_MATERIAL for item in value.values()))
    data = canonical({name: base64.b64encode(item).decode("ascii") for name, item in value.items()}) + b"\n"
    require(len(data) <= MAX_MATERIAL)
    return data


def decode(slot, data):
    try:
        value = json.loads(data)
        require(isinstance(value, dict) and set(value) == SLOTS[slot])
        result = {name: base64.b64decode(item, validate=True) for name, item in value.items()}
        require(encode(slot, result) == data)
        return result
    except (ValueError, TypeError, KeyError):
        raise CustodyRefusal("native-tunnel-custody-unproved") from None


def record(value, slot, scope, reserved=False):
    require(slot in SLOTS and isinstance(value, dict) and set(value) == {"version", "scope", "custodyID", "file"})
    require(value["version"] == "ceph-tunnel-custody-v1" and canonical(value["scope"]) == canonical(scope))
    require(isinstance(value["custodyID"], str) and str(uuid.UUID(value["custodyID"])) == value["custodyID"])
    valid_metadata(value["file"], uid=ROOT_UID, reserved=reserved)
    return value


def prove(handle, expected, reserved=False, uid=ROOT_UID):
    valid_metadata(expected, uid=uid, reserved=reserved)
    info = os.fstat(handle.fileno())
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and metadata(info, reserved) == expected)
    return info


def open_bound(parent, name, expected, reserved=False, writable=False):
    flags = os.O_RDWR if writable else os.O_RDONLY
    descriptor = os.open(name, flags | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
    handle = os.fdopen(descriptor, "r+b" if writable else "rb")
    try:
        prove(handle, expected, reserved, uid=ROOT_UID)
        return handle
    except BaseException:
        handle.close()
        raise


def retain(markers, slot, scope, material):
    """Reserve ownership before writing original bytes; completion is immutable."""
    wanted = encode(slot, material)
    reserved_name, ready_name = slot + "-reserved.json", slot + "-ready.json"
    pending, ready = markers.read(reserved_name), markers.read(ready_name)
    name = slot + ".private"
    with directory(markers.path) as parent:
        if pending is None:
            require(ready is None and not exists(parent, name))
            handle = os.fdopen(os.open(name, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent), "r+b")
            try:
                os.fchmod(handle.fileno(), 0o600)
                pending = {"version": "ceph-tunnel-custody-v1", "scope": scope, "custodyID": str(uuid.uuid4()),
                           "file": metadata(os.fstat(handle.fileno()), True)}
                markers.write(reserved_name, pending)
                os.fsync(parent)
            except BaseException:
                if markers.read(reserved_name) is None:
                    info = os.stat(name, dir_fd=parent, follow_symlinks=False)
                    if metadata(info, True) == metadata(os.fstat(handle.fileno()), True):
                        os.unlink(name, dir_fd=parent)
                        os.fsync(parent)
                handle.close()
                raise
        else:
            record(pending, slot, scope, True)
            handle = open_bound(parent, name, pending["file"], reserved=True, writable=ready is None)
        with handle:
            if ready is not None:
                record(ready, slot, scope)
                require(ready["custodyID"] == pending["custodyID"] and {key: ready["file"][key] for key in BASE_FIELDS} == pending["file"])
                info = prove(handle, ready["file"], uid=ROOT_UID)
                require(handle.read(MAX_MATERIAL + 1) == wanted)
                prove(handle, ready["file"], uid=ROOT_UID)
                require(info.st_size == len(wanted))
                return ready
            handle.seek(0)
            handle.write(wanted)
            handle.truncate()
            handle.flush()
            os.fsync(handle.fileno())
            require(handle.seek(0) == 0 and handle.read(MAX_MATERIAL + 1) == wanted)
            ready = dict(pending, file=metadata(os.fstat(handle.fileno())))
            markers.write(ready_name, ready)
            os.fsync(parent)
            return ready


@contextlib.contextmanager
def hold(markers, slot, scope):
    """Keep original descriptors alive and re-prove them around native effects."""
    pending = record(markers.read(slot + "-reserved.json"), slot, scope, True)
    ready = record(markers.read(slot + "-ready.json"), slot, scope)
    require(pending["custodyID"] == ready["custodyID"] and pending["file"] == {key: ready["file"][key] for key in BASE_FIELDS})
    with directory(markers.path) as parent, open_bound(parent, slot + ".private", ready["file"]) as handle:
        data = handle.read(MAX_MATERIAL + 1)
        require(len(data) == ready["file"]["size"])
        material = decode(slot, data)
        prove(handle, ready["file"], uid=ROOT_UID)
        yield ready, material
        prove(handle, ready["file"], uid=ROOT_UID)
        require(metadata(os.stat(slot + ".private", dir_fd=parent, follow_symlinks=False)) == ready["file"])
        handle.seek(0)
        require(handle.read(MAX_MATERIAL + 1) == data)
        prove(handle, ready["file"], uid=ROOT_UID)


def manager_directory(markers, uid, create=True):
    """An unbound existing directory is never adopted, even when empty."""
    value = markers.read("transport-directory.json")
    with directory(markers.path) as parent:
        if value is None:
            require(create)
            require(not exists(parent, "transport"))
            os.mkdir("transport", 0o700, dir_fd=parent)
            os.chown("transport", uid, uid, dir_fd=parent, follow_symlinks=False)
        descriptor = os.open("transport", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
        try:
            info = os.fstat(descriptor)
            actual = metadata(info, True)
            require(info.st_uid == info.st_gid == uid and stat.S_IMODE(info.st_mode) == 0o700)
            if value is None:
                markers.write("transport-directory.json", actual)
                os.fsync(parent)
            else:
                require(isinstance(value, dict) and set(value) == BASE_FIELDS and all(type(item) is int for item in value.values()) and actual == value)
            return descriptor
        except BaseException:
            if value is None and markers.read("transport-directory.json") is None:
                current = os.stat("transport", dir_fd=parent, follow_symlinks=False)
                if metadata(current, True) == metadata(os.fstat(descriptor), True) and not listed(descriptor):
                    os.rmdir("transport", dir_fd=parent)
                    os.fsync(parent)
            os.close(descriptor)
            raise


def materialized_metadata(markers, name, uid, handle):
    value = markers.read("transport-file-ready-" + name + ".json")
    valid_metadata(value, uid=uid)
    final = markers.read("transport-file-relabeled-" + name + ".json")
    if final is not None:
        valid_metadata(final, uid=uid)
        require({key: final[key] for key in FULL_FIELDS - {"ctimeNS"}} == {key: value[key] for key in FULL_FIELDS - {"ctimeNS"}})
        require(metadata(os.fstat(handle.fileno())) == final)
        return final
    current = metadata(os.fstat(handle.fileno()))
    if current != value:
        transition = markers.read("transport-relabel.json")
        require(isinstance(transition, dict) and transition.get("files", {}).get(name) == value)
        require({key: current[key] for key in FULL_FIELDS - {"ctimeNS"}} == {key: value[key] for key in FULL_FIELDS - {"ctimeNS"}})
        require(os.getxattr(handle.fileno(), "security.selinux") == transition.get("label", "").encode() + b"\x00")
    return current


def materialize(markers, parent, name, data, uid):
    """Record each copy's inode before writing; compare only proved descriptors."""
    require(name in SLOTS["transport-original"] and isinstance(data, bytes) and data)
    reserved_name = "transport-file-reserved-" + name + ".json"
    pending = markers.read(reserved_name)
    ready = markers.read("transport-file-ready-" + name + ".json")
    if pending is None:
        require(ready is None and markers.read("transport-ready.json") is None and not exists(parent, name))
        handle = os.fdopen(os.open(name, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=parent), "r+b")
        os.fchown(handle.fileno(), uid, uid)
        os.fchmod(handle.fileno(), 0o600)
        pending = metadata(os.fstat(handle.fileno()), True)
        try:
            markers.write(reserved_name, pending)
            os.fsync(parent)
        except BaseException:
            if markers.read(reserved_name) is None and metadata(os.stat(name, dir_fd=parent, follow_symlinks=False), True) == pending:
                os.unlink(name, dir_fd=parent)
                os.fsync(parent)
            handle.close()
            raise
    else:
        valid_metadata(pending, uid=uid, reserved=True)
        descriptor = os.open(name, (os.O_RDWR if ready is None else os.O_RDONLY) | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
        handle = os.fdopen(descriptor, "r+b" if ready is None else "rb")
    with handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and metadata(info, True) == pending)
        if ready is None:
            require(markers.read("transport-ready.json") is None)
            handle.write(data)
            handle.truncate()
            handle.flush()
            os.fsync(handle.fileno())
            ready = metadata(os.fstat(handle.fileno()))
            markers.write("transport-file-ready-" + name + ".json", ready)
            os.fsync(parent)
        expected = materialized_metadata(markers, name, uid, handle)
        handle.seek(0)
        require(handle.read(MAX_MATERIAL + 1) == data and metadata(os.fstat(handle.fileno())) == expected)
        return expected


def relabel_intent(markers, scope, uid, label):
    """Only a code-qualified shared label may advance copied-file metadata."""
    require(isinstance(label, str) and 0 < len(label) <= 256 and label.isascii() and "\x00" not in label)
    files = {name: markers.read("transport-file-ready-" + name + ".json") for name in SLOTS["transport-original"]}
    for value in files.values():
        valid_metadata(value, uid=uid)
    value = {"version": "ceph-tunnel-relabel-v1", "scope": scope, "label": label, "files": files}
    markers.write("transport-relabel.json", value)
    return value


def prove_materialized(markers, parent, scope, material, uid, label, settle=True):
    transition = markers.read("transport-relabel.json")
    require(canonical(transition) == canonical({"version": "ceph-tunnel-relabel-v1", "scope": scope, "label": label,
            "files": {name: markers.read("transport-file-ready-" + name + ".json") for name in SLOTS["transport-original"]}}))
    require(set(listed(parent)) == SLOTS["transport-original"])
    for name, data in sorted(material.items()):
        with os.fdopen(os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent), "rb") as handle:
            pending = markers.read("transport-file-reserved-" + name + ".json")
            prove(handle, pending, reserved=True, uid=uid)
            expected = materialized_metadata(markers, name, uid, handle)
            require(handle.read(MAX_MATERIAL + 1) == data and metadata(os.fstat(handle.fileno())) == expected)
            if settle and expected != markers.read("transport-file-ready-" + name + ".json"):
                markers.write("transport-file-relabeled-" + name + ".json", expected)


def source_identity(path, uid):
    """Inspect bind-source metadata without following a source-path alias. A directory component that is a root-owned
    symbolic link, as /var/run is on RHEL 9, is followed through the same walk under a bound; the identity is the
    resolved directory's. Any other link refuses."""
    require(isinstance(path, str) and path.startswith("/") and path == os.path.normpath(path))
    followed = 0
    while True:
        descriptor = os.open("/", os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            components = path.strip("/").split("/") if path != "/" else []
            for index, component in enumerate(components):
                require(component not in ("", ".", ".."))
                if index != len(components) - 1:
                    entry = os.stat(component, dir_fd=descriptor, follow_symlinks=False)
                    if stat.S_ISLNK(entry.st_mode):
                        require(entry.st_uid == ROOT_UID and followed < SOURCE_LINKS)
                        target = os.readlink(component, dir_fd=descriptor)
                        require(isinstance(target, str) and target != "")
                        path = os.path.normpath(os.path.join("/" + "/".join(components[:index]), target, *components[index + 1:]))
                        followed += 1
                        break
                flags = os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC
                if index != len(components) - 1:
                    flags |= os.O_DIRECTORY
                following = os.open(component, flags, dir_fd=descriptor)
                os.close(descriptor)
                descriptor = following
            else:
                info = os.fstat(descriptor)
                require((stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)) and info.st_uid in (ROOT_UID, uid))
                return info.st_dev, info.st_ino
        finally:
            os.close(descriptor)


def protected_identities(path):
    """Bounded metadata of original custody, copies and their enclosing roots."""
    result = set()
    ancestor = path
    while ancestor != "/":
        info = os.stat(ancestor, follow_symlinks=False)
        require(stat.S_ISDIR(info.st_mode))
        result.add((info.st_dev, info.st_ino))
        ancestor = os.path.dirname(ancestor)
    root = os.stat("/", follow_symlinks=False)
    result.add((root.st_dev, root.st_ino))
    with directory(path) as parent:
        names = os.listdir(parent)
        require(len(names) <= 256)
        for name in names:
            info = os.stat(name, dir_fd=parent, follow_symlinks=False)
            result.add((info.st_dev, info.st_ino))
        if exists(parent, "transport"):
            child = os.open("transport", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            try:
                require(set(os.listdir(child)) <= SLOTS["transport-original"])
                for name in os.listdir(child):
                    info = os.stat(name, dir_fd=child, follow_symlinks=False)
                    result.add((info.st_dev, info.st_ino))
            finally:
                os.close(child)
    return result
