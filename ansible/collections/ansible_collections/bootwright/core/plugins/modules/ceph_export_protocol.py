#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Documentation for the controller-local Ceph export protocol action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_export_protocol
short_description: Publish bounded Ceph export capability evidence
version_added: "0.1.0"
description:
  - Publishes the shared adapter handshake, a named refusal and the closed Ceph export evidence.
  - Runs on the controller and never exposes a password, private provider output or a material path.
options:
  phase:
    description: Shared protocol phase.
    type: str
    required: true
    choices:
      - loaded
      - refused
      - completed
  reason:
    description: Fixed refusal the block's caller names, published only in place of completion.
    type: str
    choices:
      - fips-policy
      - cephx-policy
      - unsupported-release
      - runtime-profile
      - device-identity
      - cache-unproved
      - enrollment-budget
      - dashboard-identity
      - dashboard-tls-identity
      - monitoring-template-identity
      - dashboard-drift
      - dashboard-policy
      - dashboard-verify
      - pool-identity
      - pool-drift
      - client-quiescence-unqualified
      - native-option-undeclared
      - call-home-unqualified
      - dashboard-manager
      - request
      - artifact
      - ownership
      - evidence
      - material
      - packages
      - package-artifact
      - package-drift
      - package-journal
      - package-license
      - package-limit
      - package-timeout
      - package-platform
      - package-host
      - call-home-drift
      - config-drift
      - partial-cluster
      - not-ready
      - not-removed
      - cluster-unavailable
      - device
      - foreign-cluster
      - native-command
      - native-timeout
      - native-output
      - unknown
      - transport
      - remote-refusal
      - adapter-exception
      - foreign-client
      - foreign-pool
      - foreign-profile
      - foreign-rule
      - crush-capacity
      - pg-budget
      - foreign-rbd-image
      - rbd-image-identity
      - foreign-filesystem
      - filesystem-identity
      - filesystem-multiple-disabled
      - foreign-subvolume-group
      - mds-coverage
      - foreign-subvolume
      - subvolume-identity
      - foreign-image
      - nfs-module-disabled
      - nfs-pool-foreign
      - object-realm-foreign
      - object-zone-pools
      - object-endpoint-certificate
      - object-user-foreign
      - object-user-identity
      - object-bucket-foreign
      - object-bucket-identity
      - object-bucket-s3
      - nfs-export-foreign
      - nfs-export-apply
      - nfs-export-path-escape
      - nfs-export-realm-default
      - nvmeof-native
      - nvmeof-identity
      - nvmeof-foreign
      - nvmeof-budget
      - ingress-firewall
      - service-credential-foreign
      - ssh-policy-host-excluded
      - ssh-policy-unavailable
      - tunnel-host-key-excluded
      - sso-unverified
      - stretch-intent
      - stretch-interrupted
      - stretch-location
      - stretch-weight
      - stretch-rule
      - stretch-foreign
      - stretch-pools
      - stretch-drift
      - tiebreaker-foreign
      - tiebreaker-unreachable
      - device-not-empty
      - foreign-osd
      - osd-partial
      - osd-clear-failed
      - osd-clear-unproved
      - osd-orphan-booted
      - osd-orphan-unproved
      - osd-class-drift
      - gateway-dns
      - export-foreign-principal
      - export-key-type
      - export-key-type-aes
      - export-key-type-aes256k
      - export-prerequisite
      - export-identity
      - export-prerequisite-pool-identity
      - export-prerequisite-rbd-application
      - export-prerequisite-filesystem
      - export-prerequisite-csi-group
      - export-prerequisite-prometheus
      - export-prerequisite-monitor-leader
      - export-prerequisite-monitor-v2
      - export-prerequisite-monitoring-address
      - export-prerequisite-realm
      - export-prerequisite-zonegroup
      - export-prerequisite-zone
      - export-prerequisite-gateway-query
  detail:
    description: Non-secret refusal cause, a refusal name or an exception type and code site.
    type: str
  native_error:
    description: Bounded redacted native cause and selected public coordinate.
    type: dict
  outcome:
    description: Completed native outcome.
    type: str
    choices:
      - changed
      - unchanged
  digest:
    description: Frozen lifecycle request digest.
    type: str
  evidence:
    description: Closed non-sensitive Ceph export evidence.
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
- name: Complete the export execution handoff
  bootwright.core.ceph_export_protocol:
    phase: loaded
"""

RETURN = r"""
changed:
  description: Always false; publication itself changes no native state.
  type: bool
  returned: always
"""
