#!/usr/bin/python
"""Documentation entrypoint; the paired action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_physical_protocol
short_description: Publish one physical machine phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
options:
  phase:
    description: The operation phase being published.
    type: str
    required: true
  group:
    description: The presentation group a progress phase reports.
    type: str
    required: false
  status:
    description: The status a progress phase reports for its group.
    type: str
    required: false
  outcome:
    description: The terminal outcome a completion phase publishes.
    type: str
    required: false
  digest:
    description: The frozen request digest the evidence must name.
    type: str
    required: false
  observation:
    description: What the management controller reported about the machine.
    type: dict
    required: false
  endpoint:
    description:
      - The management controller endpoint a refusal of the reported identity names.
      - Every completion that is not a release requires it, and one without it publishes nothing.
    type: str
    required: false
  expected:
    description: The hardware addresses the declaration requires this machine to report.
    type: list
    elements: str
    required: false
  released:
    description: Whether this publication proves a removal that retains the machine.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_baremetal_machine role using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.substrate_physical_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
