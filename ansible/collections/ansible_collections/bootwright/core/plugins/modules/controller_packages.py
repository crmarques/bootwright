#!/usr/bin/python
"""Documentation entrypoint; the paired controller action owns execution."""

from __future__ import annotations

DOCUMENTATION = r"""
module: controller_packages
short_description: Execute one private controller dependency capability
version_added: "0.1.0"
description:
  - Runs only through the paired action inside the qualified controller Python.
  - The frozen Go request and private result channel own authorization and evidence.
options:
  request:
    description: Frozen controller request capability or evidence.
    type: dict
    required: true
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
            "request": dict(type="dict", required=True, no_log=True),
        },
        supports_check_mode=False,
    )
    module.fail_json(
        msg="This private capability requires its qualified controller action."
    )


if __name__ == "__main__":
    main()
