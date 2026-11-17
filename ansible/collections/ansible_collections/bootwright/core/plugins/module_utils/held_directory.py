"""The one walk to a fixed directory, held by descriptor, never through a link.

The walk opens "/" and then each component relative to the directory before it,
with O_NOFOLLOW, as internal/nativefs does. Every directory on the way, "/"
included, must be owned by an admitted account and closed to group and other
writes; the last one may admit other owners and must pass its own proof. A
missing component is created with the given mode, and the parent synced, only
when the walk creates; otherwise a missing last component yields None when the
caller admits absence, and refuses anywhere else. A walk that proves holds each
component's identity (device, inode, owner, group, mode) and, after the caller
is done, refuses when any name no longer names the directory it opened, or when
an absent last component appeared. A link on the way refuses, unless the
caller's follow rule admits it: the walk then hands back the link's resolved
path, never opening its target. The Ceph native bundle ships this module, so it
imports only the standard library.
"""

from __future__ import annotations

import contextlib
import os
import stat

DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


class Refused(ValueError):
    """A component is not an admitted directory; the text names no path."""


class Moved(Refused):
    """A held component's name no longer names what the walk opened."""


class Linked(Exception):
    """A link the follow rule admits; path is where it resolves, for the caller to walk again."""

    def __init__(self, path):
        super().__init__("held directory link")
        self.path = path


def components(path):
    if not isinstance(path, str) or not os.path.isabs(path):
        raise Refused("held directory path")
    parts = path.strip("/").split("/") if path.strip("/") else []
    if any(part in ("", ".", "..") for part in parts):
        raise Refused("held directory path")
    return parts


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode))


def admit(info, owners):
    if not stat.S_ISDIR(info.st_mode) or info.st_uid not in owners or info.st_mode & 0o022:
        raise Refused("held directory")


def child(parent, prefix, name, create, follow):
    """The opened child directory, or None when it is absent and the walk does not create."""
    if follow is not None:
        try:
            entry = os.stat(name, dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            entry = None
        if entry is not None and stat.S_ISLNK(entry.st_mode):
            if not follow(entry):
                raise Refused("held directory link")
            raise Linked(os.path.normpath(os.path.join(prefix, os.readlink(name, dir_fd=parent))))
    try:
        return os.open(name, DIRECTORY, dir_fd=parent)
    except FileNotFoundError:
        if create is None:
            return None
    try:
        os.mkdir(name, create, dir_fd=parent)
        os.fsync(parent)
    except FileExistsError:
        pass
    return os.open(name, DIRECTORY, dir_fd=parent)


def reprove(chain):
    for parent, name, before in chain:
        try:
            now = os.stat(name, dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            raise Moved("held directory moved") from None
        if identity(now) != before:
            raise Moved("held directory moved")


# The descriptors close on any exit; the re-proof after the yield is deliberately success-only.
@contextlib.contextmanager
def hold(path, owners, *, last=None, final=None, create=None, absent=False, prove=False, follow=None):
    """Yield the descriptor of the directory at path, or None for an admitted absent last component."""
    parts = components(path)
    held = [os.open("/", DIRECTORY)]
    chain = []
    try:
        admit(os.fstat(held[0]), owners)
        for index, name in enumerate(parts):
            ending = index == len(parts) - 1
            descriptor = child(held[-1], "/" + "/".join(parts[:index]), name, create, follow)
            if descriptor is None:
                if not (absent and ending):
                    raise Refused("held directory absent")
                yield None
                if prove:
                    if exists(held[-1], name):
                        raise Moved("held directory appeared")
                    reprove(chain)
                return
            held.append(descriptor)
            info = os.fstat(descriptor)
            chain.append((held[-2], name, identity(info)))
            admit(info, last if ending and last is not None else owners)
        if final is not None and not final(os.fstat(held[-1])):
            raise Refused("held directory")
        yield held[-1]
        if prove:
            reprove(chain)
    finally:
        for descriptor in reversed(held):
            os.close(descriptor)


def exists(parent, name):
    try:
        os.stat(name, dir_fd=parent, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False
