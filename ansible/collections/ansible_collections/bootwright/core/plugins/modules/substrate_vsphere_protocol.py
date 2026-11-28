#!/usr/bin/python
"""Documentation entrypoint; the paired action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_vsphere_protocol
short_description: Publish one vSphere machine phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.operation_phase
options:
  observation:
    description: What the operation read from the vCenter.
    type: dict
    required: false
  observed:
    description: Whether the completion is a read-only observation, which publishes what it found.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_vsphere_machine role.
- name: Complete the qualified execution handoff
  bootwright.core.substrate_vsphere_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
