from __future__ import annotations

import base64
import urllib.parse

from ansible.errors import AnsibleFilterError

_USERINFO_SAFE = "-._~$&+,;="


def bootwright_parse_credential(slurp_result, label="credential"):
    if not isinstance(slurp_result, dict):
        raise AnsibleFilterError(
            f"{label}: expected an ansible.builtin.slurp result mapping, "
            f"got {type(slurp_result).__name__}"
        )
    content = slurp_result.get("content")
    if content is None:
        raise AnsibleFilterError(f"{label}: slurp result missing 'content' field")
    try:
        raw = base64.b64decode(content).decode("utf-8").rstrip("\r\n")
    except (ValueError, UnicodeDecodeError) as err:
        raise AnsibleFilterError(f"{label}: base64/utf-8 decode failed: {err}")
    if not raw:
        raise AnsibleFilterError(f"{label}: must be a single username:password line")
    if "\n" in raw or "\r" in raw:
        raise AnsibleFilterError(
            f"{label}: must be a single username:password line"
        )
    if ":" not in raw:
        raise AnsibleFilterError(
            f"{label}: must be a single username:password line"
        )
    username, password = raw.split(":", 1)
    if not username or not password:
        raise AnsibleFilterError(
            f"{label}: username and password must both be non-empty"
        )
    return {"username": username, "password": password}


def bootwright_proxy_userinfo(credentials, label="proxy credentials"):
    if not isinstance(credentials, dict):
        raise AnsibleFilterError(
            f"{label}: expected credential mapping, got {type(credentials).__name__}"
        )
    username = credentials.get("username")
    password = credentials.get("password")
    if not isinstance(username, str) or not isinstance(password, str):
        raise AnsibleFilterError(f"{label}: username and password must both be strings")
    if not username or not password:
        raise AnsibleFilterError(f"{label}: username and password must both be non-empty")
    return (
        urllib.parse.quote(username, safe=_USERINFO_SAFE)
        + ":"
        + urllib.parse.quote(password, safe=_USERINFO_SAFE)
        + "@"
    )


class FilterModule:
    def filters(self):
        return {
            "bootwright_parse_credential": bootwright_parse_credential,
            "bootwright_proxy_userinfo": bootwright_proxy_userinfo,
        }
