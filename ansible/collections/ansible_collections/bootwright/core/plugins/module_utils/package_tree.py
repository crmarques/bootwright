"""Read the immutable package tree shared by installation consumers."""
from __future__ import annotations

import os
import stat


def identity(path):
    try:
        descriptor = os.open(os.path.join(path, ".bootwright-tree-identity"), os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        return ""
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise OSError("the tree identity is not a regular file")
        data = os.read(descriptor, 66)
    finally:
        os.close(descriptor)
    value = data.decode("ascii", "replace").strip()
    if len(data) > 65 or len(value) != 64 or set(value) - set("0123456789abcdef"):
        raise OSError("the tree identity is invalid")
    return value


def exists(path):
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        return False
    if stat.S_ISLNK(info.st_mode):
        raise OSError("a package tree path is a symbolic link")
    return True


def observe(path, frozen):
    parent = os.path.abspath(path)
    while parent != "/":
        exists(parent)
        parent = os.path.dirname(parent)
    content = exists(path)
    source = identity(path) if content else ""
    marker = os.path.join(path, ".treeinfo")
    complete = exists(marker) and stat.S_ISREG(os.stat(marker).st_mode)
    return {"content": content, "identity": source, "staging": exists(path + ".staging"),
            "tree": complete and source == frozen}
