"""Bounded native error projection; private output never becomes evidence wholesale."""

from __future__ import annotations

import base64
import json
import re

MAX_COORDINATE = 512
MAX_CAUSE = 1024
MAX_INPUT = 65536
REDACTED = "[redacted]"


def private_values(value):
    """Known private payload values, including fields inside a registry authfile."""
    result = []
    if isinstance(value, dict):
        for item in value.values():
            result.extend(private_values(item))
    elif isinstance(value, list):
        for item in value:
            result.extend(private_values(item))
    elif isinstance(value, (bytes, str)) and value:
        result.append(value)
        try:
            decoded = json.loads(value)
            if isinstance(decoded, (dict, list)):
                result.extend(private_values(decoded))
        except (ValueError, UnicodeError):
            pass
        try:
            decoded = base64.b64decode(value, validate=True)
            if b":" in decoded:
                result.extend(part for part in decoded.split(b":", 1) if part)
        except (ValueError, UnicodeError):
            pass
    return result


def redact(value, secrets=(), limit=MAX_CAUSE, opaque=True):
    """Remove known material and credential-bearing native output before publication."""
    if isinstance(value, bytes):
        value = value[:MAX_INPUT].decode("utf-8", errors="replace")
    elif isinstance(value, str):
        value = value[:MAX_INPUT]
    else:
        return ""
    for private in sorted((item for item in secrets if isinstance(item, (str, bytes)) and item), key=len, reverse=True):
        if isinstance(private, bytes):
            private = private.decode("utf-8", errors="replace")
        value = value.replace(private, REDACTED)
    value = re.sub(r"-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?(?:-----END [^-\r\n]*PRIVATE KEY-----|$)", REDACTED, value, flags=re.S)
    value = re.sub(r"(?i)(https?://)[^\s/@]+:[^\s/@]+@", r"\1" + REDACTED + "@", value)
    value = re.sub(r"(?im)\b(authorization|proxy-authorization)\s*[:=]\s*[^\r\n]+", r"\1: " + REDACTED, value)
    value = re.sub(r'''(?i)(["']?(?:password|passwd|token|access_token|refresh_token|api[_-]?key|client_secret|secret|private[_-]?key|auth)'''
                   r'''["']?\s*[:=]\s*)(["'])(?:\\.|(?!\2).)*?\2''',
                   lambda match: match[1] + match[2] + REDACTED + match[2], value)
    value = re.sub(r"(?i)\b(password|passwd|token|access_token|refresh_token|api[_-]?key|client_secret|secret|keyring|private[_-]?key|auth)"
                   r"([\"']?\s*[:=]\s*[\"']?)[^\s,\"'&]+", r"\1\2" + REDACTED, value)
    # CephX keys and opaque bearer material are not public native diagnostics.
    if opaque:
        value = re.sub(r"(?<![A-Za-z0-9+/])[A-Za-z0-9+/]{32,}={0,2}(?![A-Za-z0-9+/])", REDACTED, value)
    value = " ".join("".join(char if char.isprintable() else " " for char in value).split())
    return value.encode("utf-8")[:limit].decode("utf-8", errors="ignore")


def error_detail(coordinate, output, secrets=()):
    """Project an authored public coordinate and the native tool's redacted cause."""
    return {"coordinate": redact(coordinate, secrets=secrets, limit=MAX_COORDINATE, opaque=False),
            "cause": redact(output, secrets=secrets) or "native command failed without a public error message"}


def valid(value):
    return (isinstance(value, dict) and set(value) == {"coordinate", "cause"}
            and all(isinstance(value[key], str) and 0 < len(value[key].encode("utf-8")) <= bound
                    and all(char.isprintable() for char in value[key])
                    for key, bound in (("coordinate", MAX_COORDINATE), ("cause", MAX_CAUSE))))
