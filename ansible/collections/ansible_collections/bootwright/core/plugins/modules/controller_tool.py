#!/usr/bin/python
from __future__ import annotations

DOCUMENTATION = r"""
---
module: controller_tool
short_description: Prepare one frozen controller target tool
description:
  - Internal action capability invoked only by the fixed controller dependency role.
  - Verifies the retained source and fixed executable members before exclusive publication.
version_added: '0.1.0'
author: Bootwright contributors (@crmarques)
options:
  bundle:
    type: dict
    required: true
    description: Private held directory identity and publication authority.
  tool:
    type: dict
    required: true
    description: Exact resolved source and executable projection.
  egress:
    type: dict
    required: true
    description: Explicit credential-free acquisition route.
  inspect_only:
    type: bool
    required: true
    description: Require existing source and executable postconditions without writes.
  deadline:
    type: int
    required: true
    description:
      - Seconds the source's acquisition may take, from 1 to 7200, as the frozen request derived them from its bytes.
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
- name: Verify one frozen tool through the controller role
  bootwright.core.controller_tool:
    bundle: '{{ bootwright_controller_request.publicationBundle }}'
    tool: '{{ item }}'
    egress: '{{ bootwright_controller_request.egress }}'
    inspect_only: true
    deadline: 205
  no_log: true
"""

RETURN = r"""
evidence:
  description: Bounded source and fixed-file integrity digests.
  returned: success
  type: dict
"""


def main():
    from ansible.module_utils.basic import AnsibleModule

    module = AnsibleModule(
        argument_spec=dict(
            bundle=dict(type="dict", required=True, no_log=True),
            tool=dict(type="dict", required=True, no_log=True),
            egress=dict(type="dict", required=True, no_log=True),
            inspect_only=dict(type="bool", required=True),
            deadline=dict(type="int", required=True),
        ),
        supports_check_mode=False,
    )
    module.fail_json(msg="This capability requires its fixed controller action plugin.")


if __name__ == "__main__":
    main()
