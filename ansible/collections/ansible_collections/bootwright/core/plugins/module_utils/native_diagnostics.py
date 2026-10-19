"""Bounded native error projection; private output never becomes evidence wholesale."""

from __future__ import annotations

import base64
import json
import re
import unicodedata

MAX_COORDINATE = 512
MAX_CAUSE = 1024
MAX_INPUT = 65536
MAX_HEAD = 256
REDACTED = "[redacted]"
SEAM = " ... "
PUBLIC_KEYS = frozenset(("url", "username"))
REPLACEMENT = "\ufffd"
UNDECODED = "[\ud800-\udfff�]+"


def private_values(value):
    """Known private payload values, including fields inside a registry authfile; registry identity stays public."""
    result = []
    if isinstance(value, dict):
        for key, item in value.items():
            if key not in PUBLIC_KEYS:
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
                result.extend(part for part in decoded.split(b":", 1)[1:] if part)
        except (ValueError, UnicodeError):
            pass
    return result


def _publishable(char):
    """A character the runner accepts on every supported Unicode table: printable and assigned in Unicode 3.2, or U+FFFD."""
    return char == REPLACEMENT or (char.isprintable() and unicodedata.ucd_3_2_0.category(char) != "Cn")


def _projected(char):
    """Keep a publishable character; a printable one newer than Unicode 3.2 becomes U+FFFD and any other becomes a space."""
    if _publishable(char):
        return char
    return REPLACEMENT if char.isprintable() else " "


def _public_run(match):
    """A content digest after 'sha256:' or a short-segment absolute path is a public coordinate, not opaque material."""
    text = match[0]
    if re.fullmatch(r"[0-9a-f]{64}", text) and match.string[:match.start()].endswith("sha256:"):
        return True
    segments = text.split("/")
    return (text.startswith("/") and len([part for part in segments if part]) >= 3 and all(len(part) < 32 for part in segments)
            and "+" not in text and not text.endswith("="))


def _opaque(match):
    """Redact an opaque run unless it has a public shape."""
    return match[0] if _public_run(match) else REDACTED


def _window(value):
    """Keep the head and the longer tail of redacted text beyond the input bound, cut on UTF-8 boundaries."""
    encoded = value.encode("utf-8")
    if len(encoded) <= MAX_INPUT:
        return value
    head = encoded[:MAX_INPUT // 4].decode("utf-8", errors="ignore")
    return head + "\n" + encoded[len(encoded) - MAX_INPUT * 3 // 4:].decode("utf-8", errors="ignore")


def _normalized(value):
    return " ".join("".join(_projected(char) for char in value).split())


def _bounded(value, limit, tail):
    """Project to at most limit bytes: the head only, or the first line's head, a seam and the terminal tail."""
    projected = _normalized(value)
    encoded = projected.encode("utf-8")
    if len(encoded) <= limit or not tail:
        return encoded[:limit].decode("utf-8", errors="ignore")
    first = _normalized(next((line for line in value.splitlines() if line.strip()), ""))
    head = encoded[:min(MAX_HEAD, limit // 4, len(first.encode("utf-8")))].decode("utf-8", errors="ignore")
    room = limit - len(head.encode("utf-8")) - len(SEAM)
    return head + SEAM + encoded[len(encoded) - room:].decode("utf-8", errors="ignore")


def _comparable(item):
    """One form for a value and its secrets: each run of undecodable bytes, lone surrogates or U+FFFD becomes one space."""
    if isinstance(item, bytes):
        item = item.decode("utf-8", errors="surrogateescape")
    return re.sub(UNDECODED, " ", item)


def redact(value, secrets=(), limit=MAX_CAUSE, opaque=True, tail=True):
    """Remove known material and credential-bearing native output over the whole input, then bound the projection."""
    if not isinstance(value, (str, bytes)):
        return ""
    value = _comparable(value)
    for private in sorted((item for item in secrets if isinstance(item, (str, bytes))), key=len, reverse=True):
        private = _comparable(private)
        if private.strip():
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
        value = re.sub(r"(?<![A-Za-z0-9+/])[A-Za-z0-9+/]{32,}={0,2}(?![A-Za-z0-9+/])", _opaque, value)
    return _bounded(_window(value), limit, tail)


def error_detail(coordinate, output, secrets=()):
    """Project an authored public coordinate and the native tool's redacted cause."""
    return {"coordinate": redact(coordinate, secrets=secrets, limit=MAX_COORDINATE, opaque=False, tail=False),
            "cause": redact(output, secrets=secrets) or "native command failed without a public error message"}


def valid(value):
    return (isinstance(value, dict) and set(value) == {"coordinate", "cause"}
            and all(isinstance(value[key], str) and 0 < len(value[key].encode("utf-8")) <= bound
                    and all(_publishable(char) for char in value[key])
                    for key, bound in (("coordinate", MAX_COORDINATE), ("cause", MAX_CAUSE))))
