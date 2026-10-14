#!/usr/bin/python
"""Documentation for the controller-local Ceph image protocol action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_image_protocol
short_description: Publish bounded Ceph image resolution evidence
version_added: "0.1.0"
description:
  - Publishes the shared handshake, a named refusal and the closed resolution.
  - Exposes no credentials, registry diagnostics or mutable tag adoption.
options:
  phase:
    description: Shared protocol phase.
    type: str
    required: true
    choices: [loaded, refused, completed]
  reason:
    description: Fixed resolution refusal name.
    type: str
    choices: [request, artifact, material, discovery, timeout, unknown, adapter-exception, image-digest-mismatch]
  detail:
    description: Non-secret refusal cause, a refusal name or an exception type and code site.
    type: str
  digest:
    description: Digest of the frozen lifecycle request.
    type: str
  outcome:
    description: A resolution changes nothing.
    type: str
    choices: [unchanged]
  evidence:
    description: The resolved immutable reference or the resolution's absence.
    type: dict
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Complete the image resolution handoff
  bootwright.core.ceph_image_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false because publication changes no native state.
  type: bool
  returned: always
"""
