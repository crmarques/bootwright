#!/usr/bin/python
"""Documentation entrypoint for the controller-only privilege proof action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: machine_privilege_protocol
short_description: Publish a lifecycle placement privilege observation
version_added: "0.1.0"
description:
  - Runs through the paired action on the controller and writes to the
    invoking process's private result channel.
options:
  phase:
    description: The read-only proof phase to report.
    required: true
    type: str
    choices:
      - loaded
      - completed
  outcome:
    description: The outcome reported by a completed read-only proof.
    type: str
    choices:
      - unchanged
  uid:
    description: The observed effective user identifier. Completion requires zero.
    type: int
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Enter the read-only privilege proof
  bootwright.core.machine_privilege_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
