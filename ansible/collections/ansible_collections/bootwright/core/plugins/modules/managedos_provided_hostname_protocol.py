#!/usr/bin/python
"""Documentation entrypoint for the paired provided hostname action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_hostname_protocol
short_description: Publish a provided operating system hostname phase
version_added: "0.1.0"
description:
  - Runs through its paired action on the controller and emits one bounded
    hostname record to the invoking Bootwright process.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.phase
  - bootwright.core.protocol.observed
options:
  hostname:
    description: The fully qualified hostname from the frozen request.
    type: str
  answer:
    description: The bounded hostname evidence.
    type: dict
  reason:
    description: Closed protocol refusal.
    type: str
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Complete the qualified execution handoff
  bootwright.core.managedos_provided_hostname_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
