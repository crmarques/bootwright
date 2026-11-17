"""The one atomic publication of a whole file under its directory.

A publication stages the bytes under a hidden name created exclusively in the
same directory, sets the exact mode, syncs the file, renames the stage over the
final name and syncs the directory, so after a crash the name holds either the
old file or the whole new one. A failed publication removes its stage. The
directory is a held descriptor or a path opened once without following a link
at its last component; every name is relative to it.
"""

from __future__ import annotations

import os
import secrets

STAGE = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC
DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def publish(directory, name, data, mode, prefix=".bootwright-"):
    """Replace directory/name with exactly data at mode; directory is a descriptor or a path."""
    if os.path.basename(name) != name or name in ("", ".", "..") or not prefix.startswith("."):
        raise ValueError("an atomic publication names one hidden stage and one entry of its directory")
    if isinstance(directory, int):
        publish_at(directory, name, data, mode, prefix)
        return
    held = os.open(directory, DIRECTORY)
    try:
        publish_at(held, name, data, mode, prefix)
    finally:
        os.close(held)


def publish_at(held, name, data, mode, prefix):
    stage = prefix + secrets.token_hex(12)
    descriptor = os.open(stage, STAGE, 0o600, dir_fd=held)
    staged = True
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fchmod(stream.fileno(), mode)
            os.fsync(stream.fileno())
        os.replace(stage, name, src_dir_fd=held, dst_dir_fd=held)
        staged = False
        os.fsync(held)
    finally:
        if staged:
            os.unlink(stage, dir_fd=held)
