#!/usr/bin/python
"""Documentation entrypoint; the paired artifact-server action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: artifact_server_protocol
short_description: Publish one artifact-server capability phase
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
  request:
    description: The frozen artifact-server request.
    type: dict
    required: false
  observation:
    description: The bounded host observation completion evidence is built from.
    type: dict
    required: false
  listeners:
    description: The proved listener answers completion evidence carries.
    type: list
    required: false
    elements: dict
  removed:
    description: Whether the completion publishes removal rather than presence.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.infra_artifact_server_nginx using its frozen request.
"""

RETURN = r"""
changed:
  description: Always false; publishing a phase changes no state.
  returned: always
  type: bool
"""
