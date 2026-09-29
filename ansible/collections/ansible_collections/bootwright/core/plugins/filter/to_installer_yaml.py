"""Write data as YAML the OpenShift installer reads back with the types it holds.

The installer reads its inputs with go.yaml.in/yaml/v2 through sigs.k8s.io/yaml.
A plain YAML 1.1 scalar there can become a number or a boolean, so every string
is written double-quoted, as JSON writes it. That reader also refuses a raw
DEL, C1 control or U+FFFE and folds a raw NEL into a space, so everything
outside printable ASCII is escaped; and it refuses the surrogate-pair escape
JSON writes for a code point above U+FFFF, so such a code point is written as
the eight-digit YAML escape it does read.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: to_installer_yaml
short_description: Write data as quoted YAML the OpenShift installer reads back unchanged
version_added: "0.1.0"
description:
  - Writes the input as indented JSON with sorted keys, which is YAML whose
    every string is double-quoted.
  - Escapes every character outside printable ASCII, and writes a code point
    above U+FFFF as the YAML escape C(\UXXXXXXXX) rather than the JSON
    surrogate pair the installer's YAML reader refuses.
  - The result is always YAML, and is JSON only while no string holds a code
    point above U+FFFF.
options:
  _input:
    description: The data to write.
    type: raw
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Write the agent configuration
  ansible.builtin.copy:
    content: '{{ agent_config | bootwright.core.to_installer_yaml }}'
    dest: /work/agent-config.yaml
    mode: '0600'
"""

RETURN = r"""
_value:
  description: The data as ASCII YAML text.
  type: str
"""

import json
import re

from ansible.errors import AnsibleFilterError
from ansible.module_utils.common.json import get_encoder

# Outside its strings, indented JSON is printable ASCII and newlines, and
# json.dumps escapes every C0 control inside one, so each match is a character
# above U+007E inside a string, where an escape reads back as the same text.
UNPRINTABLE = re.compile(r"[^\n -~]")


def _escape(match):
    point = ord(match.group())
    if 0xD800 <= point <= 0xDFFF:
        raise AnsibleFilterError("to_installer_yaml: a string holds a lone surrogate, which no installer input can carry")
    return "\\u%04x" % point if point <= 0xFFFF else "\\U%08x" % point


def to_installer_yaml(data):
    """The data as ASCII YAML text the installer reads back with every type it holds."""
    try:
        text = json.dumps(data, cls=get_encoder("tagless"), indent=2, sort_keys=True, separators=(",", ": "),
                          ensure_ascii=False, allow_nan=False)
    except (TypeError, ValueError) as failure:
        raise AnsibleFilterError("to_installer_yaml: %s" % failure) from failure
    return UNPRINTABLE.sub(_escape, text)


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"to_installer_yaml": to_installer_yaml}
