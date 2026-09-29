"""Bounded runner-owned controller capability channel."""

from __future__ import annotations

import json
import os


def canonical(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=True
    ).encode()


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
