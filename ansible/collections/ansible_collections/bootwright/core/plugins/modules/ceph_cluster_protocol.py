#!/usr/bin/python
"""Documentation for the controller-local Ceph protocol action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_cluster_protocol
short_description: Publish bounded core Ceph capability evidence
version_added: "0.1.0"
description:
  - Publishes the shared adapter handshake, progress and terminal evidence.
  - Runs on the controller and never exposes private provider output.
options:
  phase:
    description: Shared protocol phase.
    type: str
    required: true
    choices: [loaded, group, refused, completed]
  reason:
    description: Fixed refusal the block's caller names, published only in place of completion.
    type: str
    choices: [unsupported-release, image-pull, image-platform, image-profile, route-probe, device-identity, runtime-profile, not-ready,
      package-resolution, package-signature, package-transaction, request, artifact, ownership, evidence, material, packages, package-artifact,
      package-drift, package-journal, package-license, package-limit,
      package-timeout, package-platform, package-host, foreign-cluster, native-command, native-timeout, native-output, unknown, transport,
      remote-refusal, adapter-exception, cache-unproved, dashboard-identity, dashboard-drift, dashboard-policy, dashboard-verify, pool-identity,
      pool-drift, client-quiescence-unqualified, native-option-undeclared, call-home-unqualified, call-home-drift, config-drift, partial-cluster,
      not-removed, cluster-unavailable, device, dashboard-manager, foreign-pool, foreign-profile, foreign-rule, crush-capacity, pg-budget, foreign-rbd-image,
      rbd-image-identity, foreign-filesystem, filesystem-identity, filesystem-multiple-disabled, foreign-subvolume-group, mds-coverage,
      foreign-subvolume, subvolume-identity, foreign-image, nfs-module-disabled, nfs-pool-foreign, object-realm-foreign, object-zone-pools,
      object-endpoint-certificate, object-user-foreign, object-user-identity, object-bucket-foreign, object-bucket-identity, object-bucket-s3,
      nfs-export-foreign, nfs-export-apply, nfs-export-path-escape, ingress-firewall, service-credential-foreign,
      ssh-policy-host-excluded, ssh-policy-unavailable, tunnel-host-key-excluded, sso-unverified]
  detail:
    description: Non-secret refusal cause, a refusal name or an exception type and code site.
    type: str
  native_error:
    description: Bounded redacted native cause and selected public coordinate.
    type: dict
  group:
    description: Frozen progress group identifier.
    type: str
  status:
    description: Progress group status.
    type: str
    choices: [running, ok, failed, skipped]
  outcome:
    description: Completed native outcome.
    type: str
    choices: [changed, unchanged]
  digest:
    description: Frozen lifecycle request digest.
    type: str
  evidence:
    description: Closed non-sensitive evidence record.
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
- name: Complete the qualified execution handoff
  bootwright.core.ceph_cluster_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication itself changes no native state.
  type: bool
  returned: always
"""
