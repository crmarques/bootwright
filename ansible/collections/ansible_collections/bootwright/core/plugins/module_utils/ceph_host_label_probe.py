"""Proves the shared SELinux label is readable and read-only through the image's ceph account; the source runs alone inside the image."""

from __future__ import annotations

import json
import os
import stat
import sys

PATH = "/run/bootwright-label/proof"


def main():
    fd = os.open(PATH, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_nlink != 1:
            raise ValueError("file")
        if os.read(fd, 128) != b"bootwright-contained-public-label-probe-v1\n":
            raise ValueError("bytes")
        if not os.fstatvfs(fd).f_flag & os.ST_RDONLY:
            raise ValueError("mount")
        sys.stdout.write(json.dumps({"read": True, "readOnly": True, "uid": os.getuid(), "gid": os.getgid()}) + "\n")
    finally:
        os.close(fd)


if __name__ == "__main__":
    main()
