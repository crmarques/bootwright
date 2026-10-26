"""Bounded runner-owned controller capability channel."""

from __future__ import annotations

import os

from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical


def emit(value, acknowledge=False):
    data = canonical(value) + b"\n"
    if len(data) > 65536:
        raise ValueError("protocol")
    try:
        pending = memoryview(data)
        while pending:
            pending = pending[os.write(3, pending) :]
    except BrokenPipeError:
        if acknowledge:
            raise ValueError("authorization") from None
    if acknowledge:
        response = bytearray()
        while len(response) < 9 and not response.endswith(b"\n"):
            part = os.read(4, 1)
            if not part:
                break
            response.extend(part)
        if response != b"proceed\n":
            raise ValueError("authorization")
