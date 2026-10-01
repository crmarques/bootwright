#!/usr/bin/python
"""Documentation entrypoint; the paired installation action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_install_protocol
short_description: Publish one cluster-installation capability phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
options:
  phase:
    description: The capability phase being published.
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
  reason:
    description:
      - The refusal of a node's pre-boot proof a refused phase names to the
        runner before the run fails, which the runner reports as the
        installation's own diagnostic for that node's Machine. One of
        C(hardware-mismatch), C(identity-mismatch) or C(machine-running).
    type: str
    required: false
  node:
    description: The position in the frozen request of the node a refused phase names.
    type: int
    required: false
  outcome:
    description: The terminal outcome a completion phase publishes.
    type: str
    required: false
  digest:
    description: The frozen request digest the evidence must name.
    type: str
    required: false
  identity:
    description: The cluster identity this operation's own installer recorded.
    type: str
    required: false
  state:
    description: What the cluster and its nodes answered.
    type: dict
    required: false
  removed:
    description: Whether the completion proves removal rather than presence.
    type: bool
    required: false
  restored:
    description:
      - Whether a wait of this apply replaced the installer's own kubeconfig,
        which no longer parsed, with the copy the installation keeps. Only a
        real boolean true is recorded as a restore.
    type: bool
    required: false
  observed:
    description:
      - Whether this publication is a read-only observation, which may report
        evidence that proves no postcondition rather than failing.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.containercluster_install_agent using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.containercluster_install_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
