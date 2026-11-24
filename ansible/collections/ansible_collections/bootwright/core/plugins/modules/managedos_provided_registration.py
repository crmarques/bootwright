#!/usr/bin/python
"""Converge or observe one provided RHEL host's registration onto its declared Satellite."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_registration
short_description: Point a provided RHEL host's existing registration at its declared Satellite
version_added: "0.1.0"
description:
  - Reads the host's consumer certificate and publishes the Satellite CA bundle.
  - Points the host's subscription configuration at the declared Satellite and proves its
    existing consumer there.
  - Records the prior settings and CA durably before any change and restores them when the proof fails.
  - Refreshes and regenerates the host's repositories and requires every base URL beneath the content root.
  - Never registers, unregisters or changes the host's release or enabled repositories.
options:
  operation:
    description: Whether to converge the registration or only read it.
    type: str
    required: true
    choices:
      - apply
      - observe
  request:
    description: The SHA-256 digest of the frozen request.
    type: str
    required: true
  hostname:
    description: The Satellite's host name.
    type: str
    required: true
  content_base_url:
    description: The https content root the host's repositories must be served beneath.
    type: str
    required: true
  ca_bundle:
    description: The PEM certificates of the CA that signs the Satellite's certificate.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe a provided host's Satellite registration
  bootwright.core.managedos_provided_registration:
    operation: observe
    request: 123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0
    hostname: satellite.example.com
    content_base_url: https://satellite.example.com/pulp/content
    ca_bundle: "{{ satellite_ca }}"
  no_log: true
"""

RETURN = r"""
ok:
  description: The operation returned bounded evidence.
  returned: always
  type: bool
state:
  description: The registration state, converged, unchanged, diverged or absent.
  returned: on success
  type: str
consumer:
  description: The consumer UUID of the host's certificate, empty when it has none.
  returned: on success
  type: str
postcondition:
  description: The host's registration, CA, configuration and repositories all name the Satellite.
  returned: on success
  type: bool
reason:
  description: A closed refusal reason.
  returned: on refusal
  type: str
detail:
  description: A bounded detail or cause for the refusal.
  returned: on refusal
  type: str
"""


from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_registration as registration
from ansible_collections.bootwright.core.plugins.module_utils.managedos_rhsm_worker import Refused


def execute(params):
    try:
        result = registration.execute(params["operation"], params["request"], params["hostname"], params["content_base_url"], params["ca_bundle"])
        return dict(result, ok=True)
    except Refused as refused:
        reason = refused.reason if refused.reason in registration.REFUSALS else "registration-unavailable"
        return {"changed": False, "ok": False, "reason": reason, "detail": refused.detail or refused.cause or ""}
    except Exception:  # pylint: disable=broad-exception-caught
        return {"changed": False, "ok": False, "reason": "registration-unavailable", "detail": "transaction"}


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe"]},
            "request": {"type": "str", "required": True},
            "hostname": {"type": "str", "required": True},
            "content_base_url": {"type": "str", "required": True},
            "ca_bundle": {"type": "str", "required": True, "no_log": True},
        },
        supports_check_mode=False,
    )
    module.exit_json(**execute(module.params))


if __name__ == "__main__":
    main()
