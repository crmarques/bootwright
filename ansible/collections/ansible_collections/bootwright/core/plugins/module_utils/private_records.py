"""Immutable bounded root-owned records and directory proofs for private custody."""

from __future__ import annotations

import os
from pathlib import Path
import secrets
import stat


class Refused(ValueError):
    """A private record is not within its owner, mode, type or size contract."""


def read(path, limit=32768):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > limit:
            raise Refused("custody-file")
        data = os.read(fd, limit + 1)
        if len(data) > limit:
            raise Refused("custody-size")
        return data
    finally:
        os.close(fd)


def write(path, data, mode=0o600):
    stage = path.parent / (".stage-" + secrets.token_hex(16))
    fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    try:
        if os.write(fd, data) != len(data):
            raise Refused("custody-write")
        os.fsync(fd)
        os.link(stage, path, follow_symlinks=False)
    finally:
        os.close(fd)
        stage.unlink()
    parent = os.open(str(Path(path).parent), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def directory(path, create, mode=0o700):
    # Every ancestor must be a real root-owned directory, never a symlink or
    # writable by another principal. Only the final component may be created.
    if create:
        try:
            os.mkdir(path, mode)
        except FileExistsError:
            pass
    for ancestor in [path, *path.parents]:
        info = ancestor.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise Refused("custody-directory")
    if mode is not None and stat.S_IMODE(path.stat().st_mode) != mode:
        raise Refused("custody-mode")
