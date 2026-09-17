#!/usr/bin/python
"""Documentation entrypoint; the paired boot-media action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_media_protocol
short_description: Publish one boot-media capability phase
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
  outcome:
    description: The terminal outcome a completion phase publishes.
    type: str
    required: false
  digest:
    description: The frozen request digest the evidence must name.
    type: str
    required: false
  observation:
    description: The bounded observation of published content.
    type: dict
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
# Invoked by bootwright.core.containercluster_media_agent using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.containercluster_media_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""
