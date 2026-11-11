#!/usr/bin/python
"""Documentation entrypoint; the paired image mirror fill action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infrastructureservices_mirror_images_protocol
short_description: Publish one image mirror fill phase
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
        which the runner reports as the fill's own diagnostic for its Registry.
        C(skopeo-missing) for a mirror host with no working skopeo,
        C(image-digest-mismatch) for a mirror holding another digest than the
        pinned one, C(image-unreachable) for an origin the mirror host could
        not copy from, or C(mirror-unreadable) for a mirror it could not read.
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
  images:
    description: The frozen request's images, in its order.
    type: list
    elements: dict
    required: false
  held:
    description: The images the mirror holds, each with its repository, digest and verification.
    type: list
    elements: dict
    required: false
  removed:
    description: Whether the completion releases the fill rather than proving presence.
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
# Invoked by bootwright.core.infrastructureservices_mirror_images using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.infrastructureservices_mirror_images_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
