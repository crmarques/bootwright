"""The one reader and writer of a lent or private file: owner-only, never through a link.

A private file is a regular file with one link, owned by the effective user,
with no group or other permission bits. The reader opens it once without
following a link or blocking on a FIFO and reads at most its bound. The writer
creates a new name only, never over another file, so the effective user owns
it; it proves what it created and removes it when that proof or the write
fails, so a refused write leaves no name behind.
"""

from __future__ import annotations

import os
import stat

OPEN_READ = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
OPEN_WRITE = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC


class Refused(ValueError):
    """A private file is not within its owner, mode, link, type or size contract; it names no content."""


def private(info, owner):
    return (stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == owner
            and not info.st_mode & 0o077)


def read(path, limit, owner=None):
    """The bytes of a private file of at most limit bytes, owned by owner or else the effective user."""
    owner = os.geteuid() if owner is None else owner
    descriptor = os.open(path, OPEN_READ)
    with os.fdopen(descriptor, "rb") as handle:
        info = os.fstat(handle.fileno())
        if not private(info, owner) or info.st_size > limit:
            raise Refused("private file")
        data = handle.read(limit + 1)
        if len(data) != info.st_size:
            raise Refused("private file")
        return data


def write(path, data, mode=0o600):
    """Create path with exactly data and an owner-only mode; never replace another file."""
    if isinstance(data, str):
        data = data.encode("utf-8")
    if mode & ~0o700:
        raise Refused("private file mode")
    descriptor = os.open(path, OPEN_WRITE, mode)
    try:
        with os.fdopen(descriptor, "wb") as handle:
            info = os.fstat(handle.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_size != 0:
                raise Refused("private file")
            os.fchmod(handle.fileno(), mode)
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
    except BaseException:
        remove_created(path)
        raise


def remove_created(path):
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
