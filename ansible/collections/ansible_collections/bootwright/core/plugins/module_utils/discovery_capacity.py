"""Transient locked peak-space leases for concurrent private media builders."""

from __future__ import annotations

from contextlib import contextmanager
import errno
import fcntl
from itertools import islice
import json
import os
import re
import secrets
import stat
import time

from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils import private_records as records

MAX_LEASES = 128
MAX_RECORD = 4096
MAX_BYTES = 1 << 40
WAIT_SECONDS = 1800
# A Boolean is never a byte reservation.
# pylint: disable=unidiomatic-typecheck


@contextmanager
def shared(root):
    records.directory(root, False)
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    root_fd = os.open(root, flags)
    try:
        # Parallel builders share the existing construction guard; an exclusive
        # holder still excludes every builder throughout native I/O.
        fcntl.flock(root_fd, fcntl.LOCK_SH)
        yield
    finally:
        os.close(root_fd)


@contextmanager
def locked(root):
    records.directory(root, False)
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    root_fd = os.open(root, flags)
    try:
        directory = root / ".discovery-builds"
        records.directory(directory, True)
        os.fsync(root_fd)
        held = os.open(directory, flags)
        try:
            fcntl.flock(held, fcntl.LOCK_EX)
            yield held
        finally:
            os.close(held)
    finally:
        os.close(root_fd)


def open_record(directory, name):
    held = os.open(name, os.O_RDWR | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
    try:
        info = os.fstat(held)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size > MAX_RECORD):
            raise ValueError("discovery construction lease")
        return held
    except BaseException:
        os.close(held)
        raise


def budgets(data):
    value = json.loads(data)
    if (not isinstance(value, dict) or not 1 <= len(value) <= 2 or canonical(value) != data
            or any(re.fullmatch(r"[0-9]{1,20}", key) is None or str(int(key)) != key or int(key) >= 1 << 64
                   or type(size) is not int or not 0 < size <= MAX_BYTES
                   for key, size in value.items())):
        raise ValueError("discovery construction lease")
    return value


def remove(directory, name, held):
    current = os.stat(name, dir_fd=directory, follow_symlinks=False)
    opened = os.fstat(held)
    if (current.st_dev, current.st_ino) != (opened.st_dev, opened.st_ino):
        raise ValueError("discovery construction lease")
    os.unlink(name, dir_fd=directory)


def active(directory):
    with os.scandir(directory) as entries:
        names = [entry.name for entry in islice(entries, MAX_LEASES + 1)]
    if len(names) > MAX_LEASES or any(re.fullmatch(r"lease-[0-9a-f]{32}", name) is None for name in names):
        raise ValueError("discovery construction leases")
    used, count = {}, 0
    for name in names:
        held = open_record(directory, name)
        try:
            try:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                for device, size in budgets(os.read(held, MAX_RECORD + 1)).items():
                    used[device] = used.get(device, 0) + size
                count += 1
            else:
                # Only the lock proves no builder owns this transient record.
                # Already allocated scratch still counts in statvfs below.
                remove(directory, name, held)
        finally:
            os.close(held)
    os.fsync(directory)
    return used, count


def create(directory, requested):
    name = "lease-" + secrets.token_hex(16)
    held = os.open(name, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
    try:
        os.fchmod(held, 0o600)
        fcntl.flock(held, fcntl.LOCK_EX)
        data = canonical(requested)
        if os.write(held, data) != len(data):
            raise ValueError("discovery construction lease")
        os.fsync(held)
        # Registry EX hides partial metadata from every admission until its
        # descriptor is locked and complete. A crash leaves an unlocked lease.
        os.fsync(directory)
        return name, held
    except BaseException:
        try:
            remove(directory, name, held)
        finally:
            os.close(held)
        raise


def fits(paths, used):
    ready = True
    for device, (path, required) in paths.items():
        capacity = os.statvfs(path)
        available = capacity.f_bavail * (capacity.f_frsize or capacity.f_bsize)
        if available < required + used.get(str(device), 0):
            if str(device) not in used:
                raise OSError(errno.ENOSPC, "discovery construction capacity")
            ready = False
    return ready


@contextmanager
def reserve(root, paths):
    with shared(root):
        with reservation(root, paths):
            yield


@contextmanager
def reservation(root, paths):
    requested = budgets(canonical({str(device): size for device, (_path, size) in paths.items()}))
    deadline, lease = time.monotonic() + WAIT_SECONDS, None
    try:
        while lease is None:
            if time.monotonic() >= deadline:
                raise OSError(errno.ENOSPC, "discovery construction capacity")
            with locked(root) as directory:
                used, count = active(directory)
                if fits(paths, used) and count < MAX_LEASES:
                    lease = create(directory, requested)
            if lease is None:
                if time.monotonic() >= deadline:
                    raise OSError(errno.ENOSPC, "discovery construction capacity")
                time.sleep(1)
        yield
    finally:
        if lease is not None:
            name, held = lease
            try:
                with locked(root) as directory:
                    remove(directory, name, held)
                    os.fsync(directory)
            finally:
                os.close(held)
