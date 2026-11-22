#!/usr/bin/python
"""Documentation entrypoint for the paired provided update action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_update_protocol
short_description: Publish a provided operating system update phase
version_added: "0.1.0"
description:
  - Runs through its paired action on the controller and emits one bounded
    update record to the invoking Bootwright process.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.phase
  - bootwright.core.protocol.observed
options:
  release:
    description: The RHEL major and minor release from the frozen request.
    type: str
  answer:
    description: The bounded update evidence.
    type: dict
  reason:
    description: Closed protocol refusal.
    type: str
  detail:
    description: A sanitized refusal detail.
    type: str
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Complete the qualified execution handoff
  bootwright.core.managedos_provided_update_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
