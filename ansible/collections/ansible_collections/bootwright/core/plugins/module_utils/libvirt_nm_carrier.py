"""Persistent carrier policy for exactly one provider's static VLAN devices."""

from __future__ import annotations

import configparser
import os
import re
import secrets
import stat

CONFIG = "/etc/NetworkManager/conf.d"
MARK = "# Bootwright owned VLAN carrier policy: "
PENDING = "# Bootwright carrier reload: "
MAX_BYTES = 65536
MAX_LEGS = 20
ID = re.compile(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")
UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
DEVICE = re.compile(r"[A-Za-z0-9_-]{1,15}\.([0-9]{1,4})\Z")


def name(context, provider):
    if not all(isinstance(value, str) and ID.fullmatch(value) for value in (context, provider)):
        raise ValueError("invalid VLAN carrier policy identity")
    return "90-bootwright-%s-%s-vlan-carrier.conf" % (context, provider)


def content(context, provider, legs, pending=""):
    """Canonical bytes; the pending comment journals the configuration reload."""
    name(context, provider)
    lines = [MARK + "context=%s provider=%s" % (context, provider)]
    if pending:
        lines.append(PENDING + pending)
    for leg in sorted(legs, key=lambda entry: entry["device"]):
        lines += ["", "[device-bootwright-%s]" % leg["uuid"],
                  "match-device=interface-name:=%s" % leg["device"], "ignore-carrier=yes"]
    return ("\n".join(lines) + "\n").encode("ascii")


def parsed(data, context, provider):
    """Accept only a canonical file this owner could publish, including its journal."""
    try:
        text = data.decode("ascii")
        lines = text.splitlines()
        if not lines or lines[0] != MARK + "context=%s provider=%s" % (context, provider):
            return None
        pending = lines[1][len(PENDING):] if len(lines) > 1 and lines[1].startswith(PENDING) else ""
        if pending not in ("", "apply", "remove"):
            return None
        parser = configparser.ConfigParser(interpolation=None, strict=True)
        parser.read_string(text)
        if parser.defaults() or len(parser.sections()) > MAX_LEGS:
            return None
        legs = []
        for section in parser.sections():
            uuid = section.removeprefix("device-bootwright-")
            values = dict(parser[section])
            if not UUID.fullmatch(uuid) or set(values) != {"match-device", "ignore-carrier"}:
                return None
            device = values["match-device"].removeprefix("interface-name:=")
            found = DEVICE.fullmatch(device)
            if values["ignore-carrier"] != "yes" or not found or len(device) > 15 or not 1 <= int(found[1]) <= 4094:
                return None
            legs.append({"device": device, "uuid": uuid})
        if len({leg["device"] for leg in legs}) != len(legs) or (not legs and pending != "remove"):
            return None
        if content(context, provider, legs, pending) != data:
            return None
        return legs, pending
    except (UnicodeError, ValueError, configparser.Error):
        return None


def directory(root, owner, create=False):
    """Hold a safe directory; production also proves every fixed ancestor."""
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    descriptor = os.open("/", flags)
    parts = os.path.abspath(root).split(os.sep)[1:]
    try:
        for index, component in enumerate(parts):
            try:
                child = os.open(component, flags, dir_fd=descriptor)
            except FileNotFoundError:
                if not create or index != len(parts) - 1:
                    os.close(descriptor)
                    return None
                os.mkdir(component, 0o755, dir_fd=descriptor)
                child = os.open(component, flags, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
            info = os.fstat(descriptor)
            if (root == CONFIG or index == len(parts) - 1) and (
                    info.st_uid != owner or stat.S_IMODE(info.st_mode) & 0o022):
                raise ValueError("unsafe VLAN carrier policy directory")
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def inspect(context, provider, legs, root=CONFIG, owner=0):
    filename = name(context, provider)
    result = {"answered": True, "definition": not legs, "name": filename, "owned": False, "present": False}
    descriptor = None
    try:
        descriptor = directory(root, owner)
        if descriptor is None:
            return result
        try:
            before = os.stat(filename, dir_fd=descriptor, follow_symlinks=False)
        except FileNotFoundError:
            return result
        result["present"] = True
        if not (stat.S_ISREG(before.st_mode) and before.st_uid == owner and before.st_nlink == 1
                and stat.S_IMODE(before.st_mode) == 0o644 and before.st_size <= MAX_BYTES):
            result["definition"] = False
            return result
        fd = os.open(filename, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=descriptor)
        try:
            info = os.fstat(fd)
            data = os.read(fd, MAX_BYTES + 1)
        finally:
            os.close(fd)
        after = os.stat(filename, dir_fd=descriptor, follow_symlinks=False)
        if file_identity(before) != file_identity(info) or file_identity(before) != file_identity(after):
            raise ValueError("VLAN carrier policy changed while being read")
        result["owned"] = parsed(data, context, provider) is not None
        result["definition"] = bool(legs) and result["owned"] and data == content(context, provider, legs)
        return result
    except (OSError, ValueError):
        result.update(answered=False, definition=False)
        return result
    finally:
        if descriptor is not None:
            os.close(descriptor)


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def require_owned(observed):
    if not observed["answered"]:
        raise ValueError("the VLAN carrier policy could not be read safely")
    if observed["present"] and not observed["owned"]:
        raise ValueError("the VLAN carrier policy file is foreign; remove or rename it before retrying")


def write(descriptor, filename, data):
    stage = ".bootwright-carrier-" + secrets.token_hex(12)
    try:
        fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644, dir_fd=descriptor)
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fchmod(stream.fileno(), 0o644)
            os.fsync(stream.fileno())
        os.replace(stage, filename, src_dir_fd=descriptor, dst_dir_fd=descriptor)
        os.fsync(descriptor)
    finally:
        try:
            os.unlink(stage, dir_fd=descriptor)
        except FileNotFoundError:
            pass


def reconcile(context, provider, legs, reload_config, root=CONFIG, owner=0):
    """Publish, reload and finalize, retaining a pending comment across failure."""
    observed = inspect(context, provider, legs, root, owner)
    require_owned(observed)
    if observed["definition"]:
        return False
    descriptor = directory(root, owner, create=bool(legs))
    if descriptor is None:
        return False
    try:
        pending = "apply" if legs else "remove"
        write(descriptor, observed["name"], content(context, provider, legs, pending))
        reload_config()
        if legs:
            write(descriptor, observed["name"], content(context, provider, legs))
        else:
            os.unlink(observed["name"], dir_fd=descriptor)
            os.fsync(descriptor)
    finally:
        os.close(descriptor)
    return True
