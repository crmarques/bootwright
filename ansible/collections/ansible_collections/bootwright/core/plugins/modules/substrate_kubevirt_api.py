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
options:
  operation:
    description: The block operation or machine port entry point.
    type: str
    required: true
    choices: [proof, apply, observe, destroy, pre_boot, state_read, power, boot_media, boot_disk, media_insert, media_eject]
  kubeconfig:
    description: The path of the lent kubeconfig file.
    type: path
    required: true
  scratch:
    description: The run's private scratch directory.
    type: path
    required: true
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
  returned: block operations
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
