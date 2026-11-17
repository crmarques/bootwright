#!/usr/bin/python
"""Documentation entrypoint; the paired action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_kubevirt_protocol
short_description: Publish one KubeVirt provider or machine phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.operation_phase
options:
  subject:
    description: Whether the completion is a provider proof, a machine or a lend of host access.
    type: str
    required: false
    choices:
      - host
      - machine
      - lend
  observation:
    description: What the operation read from the host cluster.
    type: dict
    required: false
  observed:
    description: Whether the completion is a read-only observation, which publishes what it found.
    type: bool
    required: false
  released:
    description: Whether a provider completion publishes its proof released by a removal that read nothing.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_kubevirt_host and substrate_kubevirt_machine roles.
- name: Complete the qualified execution handoff
  bootwright.core.substrate_kubevirt_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
