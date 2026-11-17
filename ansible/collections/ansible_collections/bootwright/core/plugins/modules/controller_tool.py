#!/usr/bin/python
from __future__ import annotations

DOCUMENTATION = r"""
---
module: controller_tool
short_description: Prepare every frozen controller target tool
description:
  - Internal action capability invoked only by the fixed controller dependency role.
  - Verifies each retained source and its fixed executable members before exclusive publication.
  - Acquires every missing source at once, at most four together and two from one host, each under its own deadline.
  - Names an oc the release-stamp check refuses to its runner before it fails.
version_added: '0.1.0'
author: Bootwright contributors (@crmarques)
options:
  bundle:
    type: dict
    required: true
    description: Private held directory identity and publication authority.
  tools:
    type: list
    elements: dict
    required: true
    description: Each tool's exact resolved source and executable projection, in the frozen request's order.
  egress:
    type: dict
    required: true
    description: Explicit credential-free acquisition route.
  inspect_only:
    type: bool
    required: true
    description: Require existing source and executable postconditions without writes.
  trust:
    type: dict
    required: true
    description:
      - The host whose downloads verify against a lent trust anchor beside the system trust store, and the private file holding it.
      - Both are empty when the stage lent no anchor.
  acquisition:
    type: list
    elements: dict
    required: true
    description:
      - One entry per tool, in the tools' order, naming its source and the seconds its acquisition may take.
      - Each is from 1 to 7200 seconds, as the frozen request derived it from the source's bytes.
      - Bounds both the transfer and its alarm; package downloads keep their own fixed bound.
attributes:
  action:
    support: full
    description: Runs in the qualified controller process under held directory authority.
  check_mode:
    support: none
    description: Planning belongs to the Go controller service and does not invoke this capability.
  diff_mode:
    support: none
    description: Private artifacts and source details are never returned as diffs.
"""

EXAMPLES = r"""
- name: Verify every frozen tool through the controller role
  bootwright.core.controller_tool:
    bundle: '{{ bootwright_controller_request.publicationBundle }}'
    tools: '{{ bootwright_controller_request.tools }}'
    egress: '{{ bootwright_controller_request.egress }}'
    inspect_only: true
    acquisition: '{{ bootwright_controller_request.acquisition }}'
    trust: '{{ bootwright_controller_request.trust }}'
  no_log: true
"""

RETURN = r"""
evidence:
  description: Bounded source and fixed-file integrity digests, one entry per tool in the tools' order.
  returned: success
  type: list
  elements: dict
"""


def main():
    from ansible.module_utils.basic import AnsibleModule

    module = AnsibleModule(
        argument_spec=dict(
            bundle=dict(type="dict", required=True, no_log=True),
            tools=dict(type="list", elements="dict", required=True, no_log=True),
            egress=dict(type="dict", required=True, no_log=True),
            inspect_only=dict(type="bool", required=True),
            acquisition=dict(type="list", elements="dict", required=True),
            trust=dict(type="dict", required=True, no_log=True),
        ),
        supports_check_mode=False,
    )
    module.fail_json(msg="This capability requires its fixed controller action plugin.")


if __name__ == "__main__":
    main()
