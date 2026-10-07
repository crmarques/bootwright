#!/usr/bin/python
"""Documentation entrypoint; the paired installation action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_install_protocol
short_description: Publish one installation capability phase
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
      - The refusal a refused phase names to the runner before the run fails,
        which the runner reports as the installation's own diagnostic for its
        Machine. One of the target's pre-boot refusals C(hardware-mismatch),
        C(identity-mismatch) or C(machine-running), or C(media-changed-boot)
        or C(media-changed-tree) for a store entry that no longer has the size
        and SHA-256 the operation froze.
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
    description:
      - The bounded observation of published content.
      - Its C(treeIdentity) is the digest of the image the published tree was
        extracted from, empty or 64 lowercase hexadecimal characters, which the
        evidence carries unchanged.
    type: dict
    required: false
  marker:
    description: The install marker read from the guest through the identity operation.
    type: str
    required: false
  hostKey:
    description: The guest's SSH host public key, captured out of band.
    type: str
    required: false
  address:
    description: The address the installed guest answered on.
    type: str
    required: false
  media:
    description: The image the management controller still presents, if any.
    type: str
    required: false
  power:
    description: The power state the management controller reported.
    type: str
    required: false
  reachable:
    description:
      - Whether the fleet account answered on the host key the machine
        reported. An observation proves this the same way an apply does.
    type: bool
    required: false
  removed:
    description: Whether the completion proves removal rather than presence.
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
# Invoked by bootwright.core.managedos_install_anaconda using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.managedos_install_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
