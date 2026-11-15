"""Private invocation files for explicit administrator credential updates."""

from __future__ import annotations

import os
import stat


def private_read(path, limit=16384):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(descriptor, "rb") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError("private material")
        data = handle.read(limit + 1)
        if len(data) > limit:
            raise ValueError("private material")
        return data


def private_write(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, 0o600)
    with os.fdopen(descriptor, "wb") as handle:
        info = os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError("private output")
        handle.truncate(0)
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())
