#!/usr/bin/python
"""Update one owned Ceph administrator login: the dashboard account, a gateway's monitoring login or the Grafana password."""

# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_admin_credentials
short_description: Prepare or update one owned Ceph administrator login
version_added: "0.1.0"
description:
  - Uses the original frozen cluster or service unit request and verified host transport.
  - Updates the existing account and retained password only.
options:
  login:
    description: The administrator login; it equals the login material value the runner lends.
    type: str
    required: true
    choices:
      - dashboard
      - prometheus
      - alertmanager
      - grafana
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
reason:
  description: Fixed refusal the block's caller names, without native output or credentials; absent for any other failure.
  type: str
  returned: failure
"""

import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_transport, private_files

FAILURE = "The owned administrator login could not be proved or updated"
LOGINS = ["dashboard", "prometheus", "alertmanager", "grafana"]
# Each login's call bound under its Go deadline: a gateway login's proof waits for cephadm's serve loop to reconfigure its daemons.
UPDATE_TIMEOUTS = {"dashboard": 120, "prometheus": 330, "alertmanager": 330, "grafana": 270}


def login_call(transport, host, login, operation, incarnation, update_path):
    """A monitoring login's phase: the incarnation, the login, the bound leaves the phase holds and, on update, the retained candidate."""
    phase = "login_prepare" if operation == "prepare" else "login_update"
    private = dict(transport.unit_material(phase), incarnation=incarnation, login=login)
    if operation == "prepare":
        return transport.call(host, phase, private, timeout=120)
    private["update"] = json.loads(ceph_native.read_regular(update_path, maximum=16384, private=True))
    return transport.call(host, phase, private, timeout=UPDATE_TIMEOUTS[login])


def dashboard_call(transport, host, operation, incarnation, update_path):
    if operation == "prepare":
        return transport.call(host, "admin_prepare", {"incarnation": incarnation}, timeout=120)
    candidate = json.loads(ceph_native.read_regular(update_path, maximum=16384, private=True))
    return transport.call(host, "admin_update", {"update": candidate, "incarnation": incarnation}, timeout=UPDATE_TIMEOUTS["dashboard"])


def main():
    module = AnsibleModule(argument_spec={
        "login": {"type": "str", "required": True, "choices": LOGINS},
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
        login = material.pop("login")
        ceph_native.require(login == params["login"], "material")
        # The login's name is no secret and part of the reasons published, so no_log must not censor it.
        module.no_log_values.discard(login)
        transport = ceph_transport.Transport(params["request"], material)
        host = next(host for host in params["request"]["hosts"] if host["name"] == params["request"]["bootstrap"])
        if params["login"] == "dashboard":
            result = dashboard_call(transport, host, params["operation"], incarnation, update_path)
        else:
            result = login_call(transport, host, params["login"], params["operation"], incarnation, update_path)
        if params["operation"] == "prepare":
            candidate = result["private"].encode("ascii")
            ceph_native.require(len(candidate) <= 16384, "material")
            private_files.write(params["output"], candidate)
        else:
            ceph_native.require(result.get("proved") is True, "dashboard-identity" if params["login"] == "dashboard" else "monitoring-login-unproved")
    except Exception as error:
        reason = ceph_core.refusal(error, ceph_core.ADMIN_REASONS)[0]
        if reason in ceph_core.ADMIN_REASONS:
            module.fail_json(msg=FAILURE, reason=reason)
        module.fail_json(msg=FAILURE)
    module.exit_json(changed=params["operation"] == "update", proved=True)


if __name__ == "__main__":
    main()
