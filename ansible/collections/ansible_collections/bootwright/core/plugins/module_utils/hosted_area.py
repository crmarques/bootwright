"""The private directories one context's hosted content lives beneath on a host.

Everything a context realizes on a host as a workload host lives beneath
ROOT/<context>/<area>/..., where every directory down to the one a role owns
is a root-owned 0700 directory, never a link. Each is reached by no-follow
descent from its proved parent, an existing one that is not private to root
refuses without repair, and a missing one is created 0700 beneath its proved
parent, top-down, never as an implicit parent. A removal takes back only empty
directories, deepest first, up to the context's own; ROOT itself is never
removed.
"""

from __future__ import annotations

import errno
import os
import re
import stat

ROOT = "/var/lib/bootwright-hosted"
AREAS = frozenset(("services", "machines", "clusters"))
NAME = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,126}[a-z0-9])?")
DEPTH = 4
MODE = 0o700
ATTEMPTS = 3
FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


class Refusal(ValueError):
    """A hosted directory is not one only root may enter, or the request names no hosted directory."""


class Vanished(Exception):
    """A directory this walk was creating beneath was removed by a sibling destroy before the walk held it."""


def unsafe(path):
    return Refusal("%s is not a directory only root may enter, so nothing beneath it was accessed: remove it, or make it a"
                   " directory owned by root with mode 0700 that is not a link" % path)


def names(context, path):
    """The validated components beneath ROOT: the context, its area and at most three more names."""
    listed = [context] + list(path)
    if (not 2 <= len(listed) <= DEPTH + 1 or listed[1] not in AREAS
            or not all(isinstance(name, str) and NAME.fullmatch(name) for name in listed)):
        raise Refusal("the hosted directory %r of context %r is not <context>/<area>/<name>... in normal form" % (path, context))
    return listed


def entered(parent, name, path, owner):
    """The descriptor of one existing hosted directory proved private to owner, or None when it is absent."""
    try:
        descriptor = os.open(name, FLAGS, dir_fd=parent)
    except FileNotFoundError:
        return None
    except OSError as error:
        if error.errno in (errno.ELOOP, errno.ENOTDIR):
            raise unsafe(path) from None
        raise
    info = os.fstat(descriptor)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != owner or stat.S_IMODE(info.st_mode) != MODE:
        os.close(descriptor)
        raise unsafe(path)
    return descriptor


def created(parent, name, path, owner):
    """Create one missing hosted directory beneath its proved parent, then prove it; return it and whether it was made."""
    try:
        os.mkdir(name, MODE, dir_fd=parent)
    except FileExistsError:
        descriptor = entered(parent, name, path, owner)
        if descriptor is None:
            raise Vanished(path) from None
        return descriptor, False
    except FileNotFoundError:
        raise Vanished(path) from None
    try:
        descriptor = os.open(name, FLAGS, dir_fd=parent)
    except FileNotFoundError:
        raise Vanished(path) from None
    try:
        os.fchmod(descriptor, MODE)
    except OSError:
        os.close(descriptor)
        raise
    os.close(descriptor)
    descriptor = entered(parent, name, path, owner)
    if descriptor is None:
        raise Vanished(path)
    return descriptor, True


def walk(context, path, root, owner, create):
    """Descend from root's parent through every component, proving each, creating a missing one when asked.

    Returns the open descriptors, the paths they name and how many were
    created; the walk stops at the first missing component when it creates
    nothing. A directory that a sibling destroy removes while a creating walk
    is beneath it restarts the walk from the root, at most ATTEMPTS times in
    all, because the sibling removed it only because it was empty.
    """
    for attempt in range(ATTEMPTS):
        try:
            return descend(context, path, root, owner, create)
        except Vanished as error:
            if attempt == ATTEMPTS - 1:
                raise unsafe(str(error)) from None
    raise AssertionError("unreachable")


def descend(context, path, root, owner, create):
    listed = [os.path.basename(root)] + names(context, path)
    current = os.path.dirname(root)
    held = [os.open(current, FLAGS)]
    paths, made = [current], 0
    try:
        for name in listed:
            current = current + "/" + name
            descriptor = entered(held[-1], name, current, owner)
            if descriptor is None and create:
                descriptor, fresh = created(held[-1], name, current, owner)
                made += fresh
            if descriptor is None:
                break
            held.append(descriptor)
            paths.append(current)
    except BaseException:
        for descriptor in held:
            os.close(descriptor)
        raise
    return held, paths, made


def target(context, path, root=ROOT):
    return "/".join([root] + names(context, path))


def ensure(context, path, root=ROOT, owner=0):
    """Prove and create every hosted directory down to path; return whether any was created."""
    walked = walk(context, path, root, owner, True)
    for descriptor in walked[0]:
        os.close(descriptor)
    return walked[2] > 0


def prove(context, path, root=ROOT, owner=0):
    """Prove every existing hosted directory down to path without creating one; return whether path exists."""
    walked = walk(context, path, root, owner, False)
    for descriptor in walked[0]:
        os.close(descriptor)
    return walked[1][-1] == target(context, path, root)


def remove(context, path, root=ROOT, owner=0):
    """Remove each empty hosted directory from path up to the context's own; return whether any was removed.

    A directory that still holds anything ends the removal, and one a sibling
    removal took first is already absent. The root and the context's parent
    are never removed.
    """
    held, paths = walk(context, path, root, owner, False)[:2]
    removed = False
    try:
        for index in range(len(held) - 1, 1, -1):
            try:
                os.rmdir(os.path.basename(paths[index]), dir_fd=held[index - 1])
            except FileNotFoundError:
                continue
            except OSError as error:
                if error.errno in (errno.ENOTEMPTY, errno.EEXIST):
                    break
                raise
            removed = True
    finally:
        for descriptor in held:
            os.close(descriptor)
    return removed
