#!/usr/bin/python
"""Converge or observe chrony on one provided RHEL host."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_time
short_description: Converge chrony on a provided RHEL host onto its declared time servers
version_added: "0.1.0"
description:
  - Refuses a host whose other time service is enabled or active, and installs the chrony package when it is absent.
  - Replaces the server, pool and peer lines of chrony.conf with the declared servers and keeps every other directive.
  - Records the prior file and the state of chronyd durably before any change and restores them when the clock does not synchronize.
  - Proves that timedatectl reports NTPSynchronized yes within a bounded time.
options:
  operation:
    description: Whether to converge the time service or only read it.
    type: str
    required: true
    choices:
      - apply
      - observe
  request:
    description: The SHA-256 digest of the frozen request.
    type: str
    required: true
  servers:
    description: The addresses of the time servers, in order.
    type: list
    elements: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe a provided host's time synchronization
  bootwright.core.managedos_provided_time:
    operation: observe
    request: 123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0
    servers:
      - 192.0.2.123
"""

RETURN = r"""
ok:
  description: The operation returned bounded evidence.
  returned: always
  type: bool
state:
  description: The host's state, absent, partial or complete.
  returned: on success
  type: str
servers:
  description: The declared time servers.
  returned: on success
  type: list
postcondition:
  description: chrony.conf names the servers, chronyd is enabled and active and the clock is synchronized.
  returned: on success
  type: bool
reason:
  description: A closed refusal reason.
  returned: on refusal
  type: str
detail:
  description: A closed cause, unit name or summary of the sources chronyd reports.
  returned: on refusal
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_time as worker


def execute(params):
    try:
        return dict(worker.execute(params["operation"], params["request"], params["servers"]), ok=True)
    except worker.Refused as refused:
        reason = refused.reason if refused.reason in worker.REFUSALS else "time-unavailable"
        return {"changed": False, "ok": False, "reason": reason, "detail": refused.detail or ""}
    except Exception:  # pylint: disable=broad-exception-caught
        return {"changed": False, "ok": False, "reason": "time-unavailable", "detail": "transaction"}


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe"]},
            "request": {"type": "str", "required": True},
            "servers": {"type": "list", "elements": "str", "required": True},
        },
        supports_check_mode=False,
    )
    module.exit_json(**execute(module.params))


if __name__ == "__main__":
    main()
