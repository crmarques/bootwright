"""The one rule that tells whether a node may have been handed a cluster's image.

A cluster's media and install blocks share one installer work area. The
installation marks it before it hands any node the image, and records each
node's insert in it before that insert, so this rule decides both the Unknown
resolution of the installation and whether the media block may discard the
area to build again (specs/openshift-clusters.md, Unknown resolution).
"""

from __future__ import annotations

import os
import shutil
import stat

# MARKER is what the installation writes into the work area before it hands
# any node the image (prepare_boot.yml). Anything at that name counts, so an
# area it cannot prove unbooted never reads as one nothing was installed from.
MARKER = ".bootwright-booted"
# VOUCH is the content this build writes at MARKER, never over an existing
# one. Only a mark holding exactly it vouches that every insert of this area
# left its INSERTED record, so any other mark reads as a node handed the image.
VOUCH = b"inserts-recorded\n"
# INSERTED names the record boot.yml writes durably for a node, before its
# insert and so before any power request, which an attempt stopped anywhere
# after it leaves behind. Any entry whose name holds it counts, a stage an
# interrupted write left included.
INSERTED = ".bootwright-inserted-"


class InsertedError(Exception):
    """The area may hold the state a node was handed, so it is kept."""


def vouches(path):
    """Whether the mark at path is a regular file holding exactly VOUCH."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except OSError:
        return False
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            return False
        return os.read(descriptor, len(VOUCH) + 1) == VOUCH
    except OSError:
        return False
    finally:
        os.close(descriptor)


def inserted(work):
    """Whether any node may have been handed this cluster's image.

    A missing area holds no record and no mark. An area that cannot be listed,
    one holding an INSERTED record, and one whose mark does not vouch for its
    records each may hide an insert, so each counts as one.
    """
    try:
        names = os.listdir(work)
    except FileNotFoundError:
        return False
    except OSError:
        return True
    if any(INSERTED in name for name in names):
        return True
    mark = os.path.join(work, MARKER)
    return os.path.lexists(mark) and not vouches(mark)


def synced(directory):
    """Make the removals already made in directory durable."""
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def discard(path):
    """Remove one entry, never following a link."""
    if stat.S_ISDIR(os.lstat(path).st_mode):
        shutil.rmtree(path)
    else:
        os.unlink(path)


def remove(work, refuse_inserted=False):
    """Remove the work area so that no step leaves it reading as never handed the image.

    What says a node was handed the image leaves last: everything else first,
    then a mark that vouches for its records, then the records, then a mark
    that vouches for none. A removal stopped at any step therefore leaves an
    area that reads as inserted for as long as it held an insert. With
    refuse_inserted, an area that reads as inserted is kept whole.
    """
    try:
        status = os.lstat(work)
    except FileNotFoundError:
        return False
    if refuse_inserted and inserted(work):
        raise InsertedError(work)
    if not stat.S_ISDIR(status.st_mode):
        os.unlink(work)
        synced(os.path.dirname(work))
        return True
    names = os.listdir(work)
    mark = os.path.join(work, MARKER)
    records = [name for name in names if INSERTED in name]
    for name in names:
        if name != MARKER and name not in records:
            discard(os.path.join(work, name))
    synced(work)
    if vouches(mark):
        os.unlink(mark)
        synced(work)
    for name in records:
        discard(os.path.join(work, name))
    synced(work)
    if os.path.lexists(mark):
        discard(mark)
        synced(work)
    os.rmdir(work)
    synced(os.path.dirname(work))
    return True
