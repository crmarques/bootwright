"""The os module as btrfs lists a held directory descriptor.

A listing through a descriptor shows what the directory held when the
descriptor was opened or last rewound to its start, and the listing itself
rewinds, as os.listdir and os.scandir do once they have read. A test that
replaces a module's os with HeldListing fails without a rewind on any
filesystem."""

from __future__ import annotations

import contextlib
import os
import stat
from types import SimpleNamespace


class HeldListing:
    def __init__(self):
        self.held = {}

    def __getattr__(self, name):
        return getattr(os, name)

    def remember(self, descriptor):
        if stat.S_ISDIR(os.fstat(descriptor).st_mode):
            os.lseek(descriptor, 0, os.SEEK_SET)
            self.held[descriptor] = sorted(os.listdir(descriptor))

    def open(self, *args, **kwargs):
        descriptor = os.open(*args, **kwargs)
        self.remember(descriptor)
        return descriptor

    def lseek(self, descriptor, offset, whence):
        position = os.lseek(descriptor, offset, whence)
        if descriptor in self.held and (offset, whence) == (0, os.SEEK_SET):
            self.remember(descriptor)
        return position

    def listdir(self, path="."):
        if not isinstance(path, int) or path not in self.held:
            return os.listdir(path)
        names = self.held[path]
        self.remember(path)
        return list(names)

    def scandir(self, path="."):
        if not isinstance(path, int) or path not in self.held:
            return os.scandir(path)
        return contextlib.nullcontext(iter([SimpleNamespace(name=name) for name in self.listdir(path)]))

    def close(self, descriptor):
        self.held.pop(descriptor, None)
        os.close(descriptor)
