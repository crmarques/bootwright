#!/usr/bin/python
"""Compare a running network with the declared subset of its nmstate document.

The convergence applies a declaration once and nmstate verifies the whole of
it then. Later reads compare only the declared subset, so a running network
that only adds to the declaration matches and one that drifted names each
difference.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_network_compare
short_description: Compare a running network with a declared nmstate subset
version_added: "0.1.0"
description:
  - Compares the running network nmstate reported with the declared subset of
    the desired nmstate document and names each difference.
  - Reads no file, runs no command and needs no secret.
  - An unreadable running network is a difference, never a failure.
  - Performs no change and is safe to repeat.
options:
  document:
    description: The desired nmstate document.
    type: dict
    required: true
  running:
    description: The running network nmstate reported, as JSON text.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Compare the running network with the declaration
  bootwright.core.managedos_network_compare:
    document: "{{ network_document }}"
    running: "{{ running_network.stdout }}"
"""

RETURN = r"""
matched:
  description: Whether the running network holds the declared subset.
  returned: always
  type: bool
count:
  description: The number of differences found.
  returned: always
  type: int
differences:
  description: The first differences, each printable ASCII and bounded.
  returned: always
  type: list
  elements: str
"""

import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.managedos_network import (
    MAX_RUNNING_BYTES,
    bounded_differences,
    compare,
)


def differences_of(document, running):
    """Every difference of the running network text from the document."""
    if len(running.encode("utf-8", "replace")) > MAX_RUNNING_BYTES:
        return ["the running network exceeds 1 MiB"]
    try:
        parsed = json.loads(running)
    except (ValueError, RecursionError):
        parsed = None
    if not isinstance(parsed, dict):
        return ["the running network is not a JSON object"]
    return compare(document, parsed)


def result(document, running):
    """The module result for a document and the running network text."""
    differences = differences_of(document, running)
    return {
        "changed": False,
        "matched": not differences,
        "count": len(differences),
        "differences": bounded_differences(differences),
    }


def main():
    module = AnsibleModule(
        argument_spec={
            "document": {"type": "dict", "required": True},
            "running": {"type": "str", "required": True},
        },
        supports_check_mode=True,
    )
    module.exit_json(**result(module.params["document"], module.params["running"]))


if __name__ == "__main__":
    main()
