#!/usr/bin/python
"""Rotate the owned Ceph dashboard administrator credential."""

# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_admin_credentials
short_description: Prepare or update the owned dashboard administrator credential
version_added: "0.1.0"
description:
  - Uses the original frozen cluster request and verified host transport.
  - Updates the existing account and retained password only.
options:
  operation:
    description: Prepare a private candidate or apply its retained update.
    type: str
    required: true
    choices:
      - prepare
      - update
  request:
    description: Original frozen cluster request.
    type: dict
    required: true
  material:
    description: Private material paths.
    type: dict
    required: true
  output:
    description: Private candidate output path.
    type: path
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
# Invoked only by Bootwright's fixed credential entrypoints.
"""
RETURN = r"""
proved:
  description: Whether the requested preparation or update was proved.
  type: bool
  returned: success
"""

import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native
from ansible_collections.bootwright.core.plugins.module_utils.admin_credential_io import private_write


def main():
    module = AnsibleModule(argument_spec={
        "operation": {"type": "str", "required": True, "choices": ["prepare", "update"]},
        "request": {"type": "dict", "required": True},
        "material": {"type": "dict", "required": True, "no_log": True},
        "output": {"type": "path", "no_log": True},
    }, supports_check_mode=False)
    params = module.params
    try:
        material = dict(params["material"])
        update_path = material.pop("update", None)
        incarnation = material.pop("incarnation")
        transport = ceph_core.Transport(params["request"], material)
        host = next(host for host in params["request"]["hosts"] if host["name"] == params["request"]["bootstrap"])
        if params["operation"] == "prepare":
            result = transport.call(host, "admin_prepare", {"incarnation": incarnation}, timeout=120)
            candidate = result["private"].encode("ascii")
            ceph_native.require(len(candidate) <= 16384, "material")
            private_write(params["output"], candidate)
        else:
            candidate = json.loads(ceph_native.read_regular(update_path, maximum=16384, private=True))
            result = transport.call(host, "admin_update", {"update": candidate, "incarnation": incarnation}, timeout=120)
            ceph_native.require(result.get("proved") is True, "dashboard-identity")
    except Exception:
        module.fail_json(msg="The owned dashboard administrator credential could not be proved or updated")
    module.exit_json(changed=params["operation"] == "update", proved=True)


if __name__ == "__main__":
    main()
