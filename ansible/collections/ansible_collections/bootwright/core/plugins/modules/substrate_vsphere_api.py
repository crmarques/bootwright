#!/usr/bin/python
"""Documentation entrypoint for the controller-side vSphere action."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_vsphere_api
short_description: Execute one operation on an owned vSphere Machine
version_added: "0.1.0"
description:
  - Uses the locked controller SDK and lent files to contact the selected vCenter over verified TLS.
options:
  operation:
    description: The frozen lifecycle operation or Machine port entry point.
    type: str
    required: true
  request:
    description: Frozen Machine request for a block operation.
    type: dict
  material:
    description: Lent access file paths, creation operation and prior Machine receipt.
    type: dict
  port:
    description: Machine port request containing the exact target pin and permitted effect.
    type: dict
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Read Machine state
  bootwright.core.substrate_vsphere_api:
    operation: state_read
    port: "{{ bootwright_machine_port }}"
  no_log: true
"""

RETURN = r"""
observation:
  description: Bounded VM and VMDK evidence.
  returned: lifecycle operations
  type: dict
"""
