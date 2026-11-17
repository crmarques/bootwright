#!/usr/bin/python
"""Documentation entrypoint; the paired managed unit action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infra_service_protocol
short_description: Publish one managed network service or artifact server capability phase
version_added: "0.1.0"
description:
  - Runs only through the paired action on the controller, which owns the
    inherited result channel the invoking Bootwright process reads.
  - The frozen Go request and that channel own authorization and evidence.
extends_documentation_fragment:
  - bootwright.core.protocol.completion
  - bootwright.core.protocol.phase
  - bootwright.core.protocol.observed
options:
  kind:
    description:
      - The capability the phase belongs to, which names its result, its
        evidence list and the refusals it may name. C(managed-service) is a
        managed network service, whose completion carries O(answers);
        C(artifact-server) is the artifact server, whose completion carries
        O(listeners).
    type: str
    required: true
    choices:
      - managed-service
      - artifact-server
  reason:
    description:
      - The refusal a refused phase names to the runner before the run fails,
        which the runner reports as the service's own diagnostic for the port it
        names. C(foreign-listener) identifies a socket another daemon holds;
        C(unassigned-bind-address) identifies an explicit address the host
        cannot use; C(container-runtime) identifies a host whose podman does
        not run. All three are checked before effects. Readiness also names C(readiness-timeout)
        or C(listener-failed); the artifact server's names an unusable certificate
        proof C(readiness-unproved), and a valid unequal fingerprint C(certificate-mismatch).
    type: str
    required: false
  port:
    description: The port, one the frozen request binds, a refused phase names.
    type: int
    required: false
  observation:
    description: The bounded host observation completion evidence is built from.
    type: dict
    required: false
  answers:
    description: The proved answers a managed network service's completion evidence carries.
    type: list
    required: false
    elements: dict
  listeners:
    description: The proved listener answers the artifact server's completion evidence carries.
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
# Invoked by bootwright.core.infra_managed_service using its frozen request.
"""

RETURN = r"""
changed:
  description: Always false; publishing a phase changes no state.
  returned: always
  type: bool
"""
