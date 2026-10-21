#!/usr/bin/python
"""Documentation for the paired controller protocol action."""
from __future__ import annotations

DOCUMENTATION = r"""
module: cluster_objects_protocol
short_description: Publish the shared cluster object protocol
version_added: "0.1.0"
description:
  - The paired controller action owns the inherited result channel.
options:
  phase:
    description: The loaded or completed phase.
    type: str
    required: true
  budget:
    description: Frozen attempt budget in seconds.
    type: int
  state:
    description: Bounded object evidence.
    type: dict
  version:
    description: The consuming capability evidence version.
    type: str
  digest:
    description: Frozen request digest.
    type: str
  outcome:
    description: Changed or unchanged outcome.
    type: str
  observed:
    description: Whether this is a read-only observation.
    type: bool
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Publish object handoff
  bootwright.core.cluster_objects_protocol:
    phase: loaded
    budget: 600
"""
RETURN = r"""
changed:
  description: Always false.
  returned: always
  type: bool
"""
