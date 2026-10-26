"""Substitute explicitly bound proxy userinfo only while writing private inputs."""

from __future__ import annotations

from urllib.parse import quote, urlsplit, urlunsplit

from ansible.errors import AnsibleFilterError

DOCUMENTATION = r"""
name: installer_proxy_credentials
short_description: Bind installer proxy credentials without freezing their values
version_added: "0.1.0"
description:
  - Copies installer inputs and percent encodes the selected proxy credentials.
options:
  _input:
    description: Installer configuration with bare proxy endpoints.
    type: dict
    required: true
  proxy:
    description: Frozen installation proxy selection.
    type: dict
    required: true
  username:
    description: Bound username.
    type: str
    default: ''
  password:
    description: Bound password.
    type: str
    default: ''
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Bind the proxy privately
  ansible.builtin.set_fact:
    config: '{{ config | bootwright.core.installer_proxy_credentials(proxy, user, password) }}'
  no_log: true
"""

RETURN = r"""
_value:
  description: Installer configuration with bound proxy userinfo.
  type: dict
"""


def installer_proxy_credentials(config, proxy, username="", password=""):
    """Keep the request immutable and never include input in a refusal."""
    if not proxy or not proxy.get("credentialsRef"):
        return config
    try:
        result = dict(config)
        endpoints = dict(config["proxy"])
        for name in ("httpProxy", "httpsProxy"):
            parsed = urlsplit(endpoints[name])
            if not parsed.hostname or parsed.username is not None:
                raise ValueError()
            userinfo = quote(username, safe="") + ":" + quote(password, safe="") + "@"
            endpoints[name] = urlunsplit((parsed.scheme, userinfo + parsed.netloc, parsed.path, parsed.query, parsed.fragment))
        result["proxy"] = endpoints
        return result
    except (KeyError, TypeError, ValueError):
        raise AnsibleFilterError("installation proxy credential substitution failed") from None


class FilterModule:
    """Private installer input filters."""

    def filters(self):
        return {"installer_proxy_credentials": installer_proxy_credentials}
