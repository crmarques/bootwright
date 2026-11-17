#!/usr/bin/python
"""Documentation entrypoint; the paired package-tree action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_tree_protocol
short_description: Publish one package-tree capability phase
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
  reason:
    description:
      - The refusal a refused phase names to the runner before the run fails,
        which the runner reports as the publication's own diagnostic for its
        install profile. C(media-changed-tree) for a source image that no
        longer has the size and SHA-256 the operation froze, or
        C(tree-source-drift) for a published tree extracted from another image, or
        C(artifact-server-not-applied) for an artifact server whose hosted
        directories or content root are not on the host.
    type: str
    required: false
  observation:
    description: The bounded observation of published content.
    type: dict
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.managedos_package_tree using its frozen request.
- name: Complete the qualified execution handoff
  bootwright.core.managedos_tree_protocol:
    phase: loaded
"""

RETURN = r"""
extends_documentation_fragment: bootwright.core.protocol
"""
