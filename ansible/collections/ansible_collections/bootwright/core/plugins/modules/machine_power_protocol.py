#!/usr/bin/python
"""Documentation entrypoint; the paired power action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: machine_power_protocol
short_description: Publish one machine power phase
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
  machine:
    description: The Machine this operation acted on.
    type: str
    required: false
  verb:
    description: The power verb the frozen request carries.
    type: str
    required: false
  power:
    description: The power state the controller reported once it settled.
    type: str
    required: false
  previous:
    description: The power state the controller reported before the operation.
    type: str
    required: false
  changed:
    description: Whether the reported state differs from the state before it.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.machine_power_redfish using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.machine_power_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
