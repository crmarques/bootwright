#!/usr/bin/python
"""Rotate the existing installation's console administrator credential."""

# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)

from __future__ import annotations

DOCUMENTATION = r"""
module: containercluster_admin_credentials
short_description: Prepare or update the existing console administrator credential
version_added: "0.1.0"
description:
  - Uses the original installation's administrator kubeconfig and a pinned client.
  - Updates an existing account only and never creates an account.
options:
  operation:
    description: Prepare a private guard or apply a retained update.
    type: str
    required: true
    choices:
      - prepare
      - update
  client:
    description: Absolute path of the installed client.
    type: path
    required: true
  kubeconfig:
    description: Private administrator kubeconfig file.
    type: path
    required: true
  update:
    description: Private retained credential update file.
    type: path
  output:
    description: Private output file for a prepared guard.
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
import secrets

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import containercluster_admin_credentials as credentials, private_files


def main():
    module = AnsibleModule(argument_spec={
        "operation": {"type": "str", "required": True, "choices": ["prepare", "update"]},
        "client": {"type": "path", "required": True},
        "kubeconfig": {"type": "path", "required": True, "no_log": True},
        "update": {"type": "path", "no_log": True},
        "output": {"type": "path", "no_log": True},
    }, supports_check_mode=False)
    params = module.params
    try:
        if params["operation"] == "prepare":
            guard = credentials.read(params["client"], params["kubeconfig"])
            private_files.write(params["output"], json.dumps({"guard": guard, "password": secrets.token_urlsafe(24)}).encode())
        else:
            candidate = json.loads(private_files.read(params["update"], 16384))
            credentials.update(params["client"], params["kubeconfig"], candidate)
    except Exception:
        module.fail_json(msg="The existing console administrator credential could not be proved or updated")
    module.exit_json(changed=params["operation"] == "update", proved=True)


if __name__ == "__main__":
    main()
