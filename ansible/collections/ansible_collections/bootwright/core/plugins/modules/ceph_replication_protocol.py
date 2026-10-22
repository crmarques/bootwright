#!/usr/bin/python
"""Documentation for the controller-local Ceph replication protocol action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_replication_protocol
short_description: Publish bounded Ceph replication capability evidence
version_added: "0.1.0"
description:
  - Publishes the shared adapter handshake, a named refusal and the closed replication resource evidence.
  - Runs on the controller and never exposes a password, private provider output or a material path.
options:
  phase:
    description: Shared protocol phase.
    type: str
    required: true
    choices: [loaded, refused, completed]
  reason:
    description: Fixed refusal the block's caller names, published only in place of completion.
    type: str
    choices: [request, artifact, ownership, evidence, material, transport, remote-refusal, adapter-exception, native-command, native-timeout,
      native-output, unknown, foreign-cluster, foreign-pool, pool-identity, foreign-image, image-identity, replication-identity,
      replication-unavailable, replication-token, ssh-policy-host-excluded, ssh-policy-unavailable]
  detail:
    description: Non-secret refusal cause, a refusal name or an exception type and code site.
    type: str
  native_error:
    description: Bounded redacted native cause and selected public coordinate.
    type: dict
  outcome:
    description: Completed native outcome.
    type: str
    choices: [changed, unchanged]
  digest:
    description: Frozen lifecycle request digest.
    type: str
  evidence:
    description: Closed non-sensitive replication resource evidence.
    type: dict
  observed:
    description: Whether this is a read-only observation.
    type: bool
    default: false
  removed:
    description: Whether this completion must prove removal.
    type: bool
    default: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Complete the replication execution handoff
  bootwright.core.ceph_replication_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication itself changes no native state.
  type: bool
  returned: always
"""
