#!/usr/bin/python
"""Documentation entrypoint; the paired action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_kubevirt_api
short_description: Run one KubeVirt arm operation against a host cluster
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which reads the
    lent kubeconfig, refuses any form but one cluster, user and context with
    an embedded authority and a client certificate or token, and speaks the
    host's Kubernetes API.
  - Block operations return the observation the protocol publishes; port
    operations return the machine port's answer.
  - root_reset deletes a kept VirtualMachine's root DataVolume by its UID, lets
    the VirtualMachine's template create it again blank and marks the
    VirtualMachine; root_read only reads that, and a reset refuses check mode.
options:
  power_only:
    description: During observation, read only exact VM and instance identities and power.
    type: bool
    required: false
  operation:
    description: The block operation or machine port entry point.
    type: str
    required: true
    choices:
      - proof
      - template_source_read
      - apply
      - observe
      - destroy
      - root_reset
      - root_read
      - pre_boot
      - state_read
      - power
      - boot_media
      - boot_disk
      - media_insert
      - media_eject
  kubeconfig:
    description: The path of the lent kubeconfig file.
    type: path
    required: true
  scratch:
    description: The run's private scratch directory.
    type: path
    required: true
  cluster_uid:
    description: The proved host cluster UID for a template source read.
    type: str
  snapshot_ref:
    description: The published VM snapshot's namespace, name, and UID for a template source read.
    type: dict
  request:
    description: The frozen request of a block operation.
    type: dict
  apply_operation:
    description: The apply operation whose owner mark a block operation writes and recovers.
    type: str
  block:
    description: The block whose owner mark a block operation writes and recovers.
    type: str
  registered:
    description: When the apply operation registered; a recovered object is created no earlier.
    type: str
  pinned:
    description: The provider proof's server, cluster identity and root class, and the block's receipts.
    type: dict
  removal:
    description: The staged destroy whose mark a root disk reset writes and its reading proves.
    type: str
  step:
    description: The step whose removal blanks the root disk, beside removal.
    type: str
  endpoint:
    description: The port target, kubevirt:<namespace>/<name>.
    type: str
  seconds:
    description: The bound of the operation's waits.
    type: int
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_kubevirt_machine role.
- name: Read the machine's power
  bootwright.core.substrate_kubevirt_api:
    operation: state_read
    kubeconfig: '{{ kubeconfig_file }}'
    scratch: '{{ run_scratch }}'
    endpoint: kubevirt:guests/node-0-0a1b2c3d
"""

RETURN = r"""
observation:
  description: A block operation's evidence before its request digest is added.
  returned: block and root disk operations
  type: dict
power:
  description: On or Off.
  returned: state_read and power
  type: str
refusal:
  description: A named port refusal, such as machine-running.
  returned: when a port proof refuses by name
  type: str
"""
