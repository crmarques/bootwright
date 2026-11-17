"""The one registry authentication rule set: Basic credentials, auth files and Bearer token exchange.

A credential is a non-empty user name without a colon and a non-empty
password, neither holding a NUL, CR or LF, each within its byte bound. An auth
file holds exactly one entry, the credential of its one registry authority,
and is written as a new private file. A Bearer challenge must name a realm on
the same authority and only the requested repository's pull scope; RFC 6750
error attributes are admitted and ignored. A lent auth file is proved unchanged
while a native client holds it and is removed after. A token answer is a closed JSON
object whose token is bounded and printable. The Ceph native bundle ships this
module, so it imports only the standard library, canonical_json and
private_files.
"""

from __future__ import annotations

# Exact JSON numbers exclude bool and float at this boundary.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import json
import os
import stat
import re
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import canonical_json, private_files

# A usernamePassword Secret admits a user name up to its part bound (specs/secrets.md, Bounds): 1 MiB.
MAX_USERNAME = 1 << 20
MAX_PASSWORD = 32768
MAX_CHALLENGE = 4096
MAX_TOKEN = 32768
CHALLENGE_FIELD = re.compile(r'([A-Za-z_]+)="([^"\\\x00-\x1f\x7f]*)"(?:,\s*|$)')
CHALLENGE_FIELDS = frozenset(("realm", "service", "scope", "error", "error_description", "error_uri"))
TOKEN_FIELDS = frozenset(("token", "access_token", "expires_in", "issued_at"))
TOKEN = re.compile(r"[A-Za-z0-9._~+/=-]+\Z")


# Every reason credential() and document() refuse, in words a refusal message can carry.
CREDENTIAL_RULE = ("a registry credential needs a registry address, a non-empty user name of at most 1 MiB without a colon, "
                   "and a non-empty password of at most 32 KiB, neither holding a NUL, carriage return or line feed")


class Refused(ValueError):
    """A credential, challenge or token outside its contract; the text names no value."""


def require(condition):
    if not condition:
        raise Refused("registry authentication")


def credential(username, password):
    """The base64 user:password of one valid credential."""
    require(isinstance(username, str) and 0 < len(username.encode("utf-8")) <= MAX_USERNAME
            and not any(character in username for character in ":\x00\r\n"))
    require(isinstance(password, str) and 0 < len(password.encode("utf-8")) <= MAX_PASSWORD
            and not any(character in password for character in "\x00\r\n"))
    return base64.b64encode((username + ":" + password).encode("utf-8")).decode("ascii")


def basic(username, password):
    """The Authorization header value of one valid credential."""
    return "Basic " + credential(username, password)


def decoded(auth):
    """The (user name, password) an auth entry encodes, under the same rule as credential."""
    require(isinstance(auth, str))
    try:
        text = base64.b64decode(auth, validate=True).decode("utf-8")
    except (ValueError, UnicodeError):
        raise Refused("registry authentication") from None
    username, separator, password = text.partition(":")
    require(separator and credential(username, password) == auth)
    return username, password


def document(authority, username, password):
    """The auth file object holding exactly this authority's credential."""
    require(isinstance(authority, str) and bool(authority))
    return {"auths": {authority: {"auth": credential(username, password)}}}


def write(path, authority, username, password):
    """Write the auth file of one authority as a new private file."""
    private_files.write(path, canonical_json.canonical(document(authority, username, password)))


def held(info):
    return tuple(getattr(info, key) for key in ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))


# The descriptor closes and the name is removed on any exit; the proof after the yield is deliberately success-only.
@contextlib.contextmanager
def lent(path, value):
    """Lend a new auth file holding value to one native client, proved unchanged, then removed."""
    data = canonical_json.canonical(value)
    descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        with os.fdopen(descriptor, "w+b", closefd=False) as stream:
            stream.write(data)
            stream.flush()
            os.fsync(descriptor)
            info = os.fstat(descriptor)
            before = held(info)
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600)
            yield path
            require(held(os.fstat(descriptor)) == before and held(os.stat(path, follow_symlinks=False)) == before)
            stream.seek(0)
            require(stream.read(len(data) + 1) == data)
    finally:
        try:
            require(held(os.stat(path, follow_symlinks=False)) == held(os.fstat(descriptor)))
            os.unlink(path)
        finally:
            os.close(descriptor)


def challenge(value, authority, namespace):
    """The token path and query a Bearer challenge names for this repository's pull scope."""
    scope = "repository:" + namespace + ":pull"
    require(isinstance(value, str) and len(value) <= MAX_CHALLENGE and value.startswith("Bearer "))
    remaining, fields = value[7:], {}
    while remaining:
        match = CHALLENGE_FIELD.match(remaining)
        require(match is not None and match[1] not in fields)
        fields[match[1]] = match[2]
        remaining = remaining[match.end():]
    require({"realm", "service"} <= set(fields) <= CHALLENGE_FIELDS and bool(fields["service"])
            and fields.get("scope", scope) == scope)
    realm = urllib.parse.urlsplit(fields["realm"])
    require(realm.scheme == "https" and realm.netloc == authority and realm.username is None and realm.password is None
            and realm.path.startswith("/") and not realm.query and not realm.fragment)
    return realm.path + "?" + urllib.parse.urlencode({"service": fields["service"], "scope": scope})


def unique(rows):
    value = {}
    for key, item in rows:
        require(key not in value)
        value[key] = item
    return value


def token(raw):
    """The Bearer token of one token answer."""
    require(isinstance(raw, bytes) and len(raw) <= 2 * MAX_TOKEN)
    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=unique)
    except (ValueError, UnicodeError):
        raise Refused("registry authentication") from None
    require(isinstance(value, dict) and set(value) <= TOKEN_FIELDS)
    selected = value.get("token", value.get("access_token"))
    require(isinstance(selected, str) and 0 < len(selected) <= MAX_TOKEN and TOKEN.match(selected) is not None)
    require("token" not in value or "access_token" not in value or value["token"] == value["access_token"])
    require("expires_in" not in value or type(value["expires_in"]) is int and 0 < value["expires_in"] <= 86400)
    return selected
