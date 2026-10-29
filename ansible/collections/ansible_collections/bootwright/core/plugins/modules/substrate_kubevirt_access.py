#!/usr/bin/python
"""Documentation entrypoint; the paired action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: substrate_kubevirt_access
short_description: Mint the access one KubeVirt provider hosted by a cluster of the context is lent
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which reads the host
    installation's administrator kubeconfig, mints one TokenRequest token of the
    hosted namespace's ServiceAccount and writes a kubeconfig carrying that
    token alone to the run's output file.
options:
  kubeconfig:
    description: The path of the lent administrator kubeconfig.
    type: path
    required: true
  scratch:
    description: The run's private scratch directory.
    type: path
    required: true
  request:
    description: The frozen lend request.
    type: dict
    required: true
  output:
    description: The output file the lent kubeconfig is written to.
    type: path
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by the substrate_kubevirt_host role's lend tasks.
- name: Mint the hosted provider's access
  bootwright.core.substrate_kubevirt_access:
    kubeconfig: '{{ admin_kubeconfig_file }}'
    scratch: '{{ run_scratch }}'
    request: '{{ lend_request }}'
    output: '{{ lend_output }}'
"""

RETURN = r"""
observation:
  description: The server the token reaches, the lifetime granted and whether it covers the request.
  returned: success
  type: dict
"""
