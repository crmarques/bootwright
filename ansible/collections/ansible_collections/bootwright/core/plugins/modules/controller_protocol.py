#!/usr/bin/python
"""Documentation entrypoint; the paired controller action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: controller_protocol
short_description: Execute one private controller dependency capability
version_added: "0.1.0"
description:
  - Runs only through the paired action inside the qualified controller Python.
  - The frozen Go request and private result channel own authorization and evidence.
options:
  phase:
    description: Frozen controller phase capability or evidence.
    type: str
    required: true
  request:
    description: Frozen controller request capability or evidence.
    type: dict
    required: false
  inventory:
    description: Frozen controller inventory capability or evidence.
    type: list
    required: false
    elements: dict
  roots_ready:
    description: Frozen controller roots_ready capability or evidence.
    type: bool
    required: false
  preparation:
    description: Frozen controller preparation capability or evidence.
    type: dict
    required: false
  native_applied:
    description: Whether the frozen native transaction was acknowledged and completed.
    type: bool
    required: false
  tools:
    description: Frozen controller tools capability or evidence.
    type: list
    required: false
    elements: dict
  tools_changed:
    description: Frozen controller tools_changed capability or evidence.
    type: bool
    required: false
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
# Invoked by bootwright.core.controller_prerequisites using its frozen request.
"""

RETURN = r"""
changed:
  description: Whether the authorized dependency action changed state.
  returned: always
  type: bool
"""


def main():
    from ansible.module_utils.basic import AnsibleModule

    module = AnsibleModule(
        argument_spec={
            "phase": dict(type="str", required=True, no_log=True),
            "request": dict(type="dict", required=False, no_log=True),
            "inventory": dict(
                type="list", required=False, no_log=True, elements="dict"
            ),
            "roots_ready": dict(type="bool", required=False, no_log=True),
            "preparation": dict(type="dict", required=False, no_log=True),
            "native_applied": dict(type="bool", required=False, no_log=True),
            "tools": dict(type="list", required=False, no_log=True, elements="dict"),
            "tools_changed": dict(type="bool", required=False, no_log=True),
        },
        supports_check_mode=False,
    )
    module.fail_json(
        msg="This private capability requires its qualified controller action."
    )


if __name__ == "__main__":
    main()
