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
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.phase
  - bootwright.core.protocol.observed
  - bootwright.core.protocol.removed
options:
  observation:
    description: The bounded observation of published content.
    type: dict
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
extends_documentation_fragment: bootwright.core.protocol
"""
