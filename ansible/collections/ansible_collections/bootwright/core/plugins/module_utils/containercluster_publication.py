"""The layout a container cluster's reserved image publication must have.

The reserved root is <content root>/public/private/<consumer>/<object>. No
component from the artifact server's content root down may be a symbolic link.
The directories above the content root may be, and every directory the walk to
the content root passes through, links resolved, must be owned by root and
writable by neither group nor other. A refusal names the directory to fix,
which is the reserved root or a directory above it, never a token beneath it.
"""

from __future__ import annotations

import errno
import os
import stat

DIRECTORY = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
LINKS = 40
OWNER = 0


class LayoutError(ValueError):
    """A layout the walk refuses, naming the directory to fix."""

    def __init__(self, directory, problem):
        super().__init__('%s %s' % (directory, problem))
        self.directory = directory


def joined(parts):
    return '/' + '/'.join(parts)


def trusted(fd, path):
    """Refuse an ancestor directory anyone but root could change."""
    info = os.fstat(fd)
    if info.st_uid != OWNER:
        raise LayoutError(path, 'is not owned by root, so another account could redirect the publication beneath it')
    if info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        raise LayoutError(path, 'is writable by its group or by others, so another account could redirect the publication beneath it')


def ancestor(names):
    """Open a directory, following links only through root-owned directories."""
    pending, trail, links = list(names), [], 0
    fd = os.open('/', DIRECTORY)
    try:
        trusted(fd, '/')
        while pending:
            part = pending.pop(0)
            if part in ('', '.'):
                continue
            if part == '..':
                trail = trail[:-1]
                child = ancestor(trail)
            else:
                try:
                    child = os.open(part, DIRECTORY, dir_fd=fd)
                except OSError as error:
                    if error.errno not in (errno.ELOOP, errno.ENOTDIR):
                        raise
                    if not stat.S_ISLNK(os.stat(part, dir_fd=fd, follow_symlinks=False).st_mode):
                        raise
                    links += 1
                    if links > LINKS:
                        raise
                    target = os.readlink(part, dir_fd=fd)
                    pending[:0] = target.split('/')
                    if not target.startswith('/'):
                        continue
                    trail, child = [], os.open('/', DIRECTORY)
                else:
                    trail.append(part)
            os.close(fd)
            fd = child
            trusted(fd, joined(trail))
        return fd
    except Exception:
        os.close(fd)
        raise


def split(path):
    """The directories above the content root, and the content root down."""
    parts = path.split('/')
    if not path.startswith('/') or any(p in ('.', '..') for p in parts):
        raise ValueError('invalid publication root')
    parts = list(filter(None, parts))
    if len(parts) < 6 or parts[-4:-2] != ['public', 'private']:
        raise ValueError('invalid publication root')
    return parts[:-5], parts[-5:]


def step(fd, part, path):
    """Open one component from the content root down, following no link."""
    try:
        return os.open(part, DIRECTORY, dir_fd=fd)
    except OSError as error:
        if error.errno not in (errno.ELOOP, errno.ENOTDIR):
            raise
        if stat.S_ISLNK(os.stat(part, dir_fd=fd, follow_symlinks=False).st_mode):
            raise LayoutError(path, 'is a symbolic link, and no directory from the artifact server\'s content root down may be one') from None
        raise LayoutError(path, 'is not a directory') from None


def open_root(path):
    """Open the reserved root, following no link from the server's content root down."""
    above, below = split(path)
    fd = ancestor(above)
    current = joined(above)
    try:
        for part in below:
            current = current + '/' + part
            child = step(fd, part, current)
            os.close(fd)
            fd = child
        return fd
    except Exception:
        os.close(fd)
        raise


def unlinked(fd, path):
    """Refuse a symbolic link at the two levels a publication lives at."""
    for name in os.listdir(fd):
        info = os.stat(name, dir_fd=fd, follow_symlinks=False)
        if stat.S_ISLNK(info.st_mode):
            raise LayoutError(path, 'holds a symbolic link, and no entry from the artifact server\'s content root down may be one')
        if not stat.S_ISDIR(info.st_mode):
            continue
        directory = os.open(name, DIRECTORY, dir_fd=fd)
        try:
            for entry in os.listdir(directory):
                if stat.S_ISLNK(os.stat(entry, dir_fd=directory, follow_symlinks=False).st_mode):
                    raise LayoutError(path, 'holds a directory holding a symbolic link, and no entry from the '
                                            'artifact server\'s content root down may be one')
        finally:
            os.close(directory)


def prove(path, removal=False):
    """Prove the layout the reserved root's walk accepts, before anything is published or removed.

    The media block's tasks after this proof resolve paths again, so every
    directory from the content root down that exists must also be owned by root
    and writable by neither group nor other. A component missing beneath the content root is accepted, because a
    publication creates it as a plain directory. A missing content root refuses
    a publication and is accepted by a removal, which then has nothing to
    remove. Returns whether the reserved root exists.
    """
    above, below = split(path)
    content = joined(above + below[:1])
    try:
        fd = ancestor(above)
    except FileNotFoundError:
        if removal:
            return False
        raise LayoutError(content, 'does not exist, so the artifact server this cluster publishes through has not created its content root') from None
    current = joined(above)
    try:
        for depth, part in enumerate(below):
            current = current + '/' + part
            try:
                child = step(fd, part, current)
            except FileNotFoundError:
                if depth == 0 and not removal:
                    raise LayoutError(current, 'does not exist, so the artifact server this cluster publishes through '
                                               'has not created its content root') from None
                return False
            os.close(fd)
            fd = child
            trusted(fd, current)
        unlinked(fd, current)
        return True
    finally:
        os.close(fd)
