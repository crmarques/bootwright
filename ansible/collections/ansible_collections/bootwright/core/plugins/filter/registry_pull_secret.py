"""Merge one explicitly selected mirror credential without replacing existing auth."""

from __future__ import annotations

import base64
import json

from ansible.errors import AnsibleFilterError

DOCUMENTATION = r"""
name: registry_pull_secret
short_description: Merge the selected mirror credential into the install pull secret
version_added: "0.1.0"
description:
  - Adds exactly one host credential, refusing an existing different entry.
  - Errors never include the secret material.
options:
  _input:
    description: The original Docker configuration JSON.
    type: str
    required: true
  mirror:
    description: The frozen mirror host and credential type.
    type: dict
    required: true
  credential:
    description: The bound Docker configuration JSON or username.
    type: str
    default: ''
  password:
    description: The bound password for usernamePassword credentials.
    type: str
    default: ''
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Merge bound credentials privately
  ansible.builtin.set_fact:
    pull: '{{ pull | bootwright.core.registry_pull_secret(mirror, username, password) }}'
  no_log: true
"""

RETURN = r"""
_value:
  description: The merged Docker configuration JSON.
  type: str
"""


def registry_pull_secret(original, mirror, credential="", password=""):
    """Keep every existing field and admit only an identical existing credential."""
    if not mirror or not mirror.get("credentialsRef"):
        return original
    try:
        config = json.loads(original)
        if not isinstance(config, dict) or not isinstance(config.get("auths"), dict):
            raise ValueError()
        host = mirror["host"]
        if mirror["credentialsType"] == "usernamePassword":
            entry = {"auth": base64.b64encode((credential + ":" + password).encode("utf-8")).decode("ascii")}
        elif mirror["credentialsType"] == "dockerConfigJson":
            entry = json.loads(credential)["auths"][host]
            if not isinstance(entry, dict) or not entry:
                raise ValueError()
        else:
            raise ValueError()
        if host in config["auths"] and config["auths"][host] != entry:
            raise AnsibleFilterError("registry pull secret conflicts with the selected mirror credential")
        config["auths"][host] = entry
        return json.dumps(config, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
    except (KeyError, TypeError, ValueError):
        raise AnsibleFilterError("registry pull secret or selected mirror credential is invalid") from None


class FilterModule:
    """The mirror credential merge used only by the media writer."""

    def filters(self):
        return {"registry_pull_secret": registry_pull_secret}
