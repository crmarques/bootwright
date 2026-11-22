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
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.operation_phase
options:
  reason:
    description:
      - The refusal a refused phase names to the runner before the run fails,
        which the runner reports as its caller's own diagnostic for it. A power
        run names C(controller-host), C(hardware-mismatch),
        C(identity-unproved) or C(identity-mismatch).
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
  media:
    description:
      - The media state the controller reported once an eject settled, as the
        machine port reads it. Only C(eject-media) supplies it.
    type: str
    required: false
    choices:
      - none
      - own
      - foreign
      - unknown
  previous_media:
    description:
      - The media state the controller reported before an eject. Only
        C(eject-media) supplies it.
    type: str
    required: false
    choices:
      - none
      - own
      - foreign
      - unknown
  changed:
    description: Whether the reported state differs from the state before it.
    type: bool
    required: false
  read_media:
    description: Publish the media state for each reading.
    type: bool
    default: false
  readings:
    description:
      - The registered per-machine controller reads a bounded reading publishes,
        in the order its survey named them. Supplying it publishes reading
        evidence in place of the evidence one power verb returns.
    type: list
    elements: dict
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.machine_power using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.machine_power_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
