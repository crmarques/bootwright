#!/usr/bin/python
"""Documentation entrypoint; the paired machine action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_libvirt_machine_protocol
short_description: Publish one machine capability phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.phase
options:
  power_only:
    description: Report the domain identity and power from a native read for lifecycle admission.
    type: bool
    required: false
  observation:
    description: The bounded host observation completion evidence is built from.
    type: dict
    required: false
  power:
    description: The power state the management controller reported.
    type: str
    required: false
  system:
    description: The ComputerSystem identity the controller exposed.
    type: str
    required: false
  removed:
    description:
      - Whether the completion proves removal rather than presence. A removal
        publishes the absence form, and fails unless it proves everything
        gone. It is never combined with observed.
    type: bool
    required: false
  observed:
    description:
      - Whether this publication is a read-only observation, which may report
        evidence that proves no postcondition rather than failing.
      - The observation publishes the absence form exactly when everything it
        observed is gone, and the presence form otherwise, so a caller never
        chooses the form.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.substrate_libvirt_machine using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.substrate_libvirt_machine_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
