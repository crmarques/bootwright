#!/usr/bin/python
"""Set or observe the hostname of one provided RHEL host."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_hostname
short_description: Set the static and kernel hostname of a provided RHEL host
version_added: "0.1.0"
description:
  - Applies or reads the declared fully qualified hostname of one provided host.
  - Proves both the static hostname and the running kernel's nodename.
options:
  operation:
    description: The operation.
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
    description: The fully qualified DNS name the host must report.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe a provided RHEL host's hostname
  bootwright.core.managedos_provided_hostname:
    operation: observe
    request: 123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0
    hostname: ceph-01.example.test
"""

RETURN = r"""
ok:
  description: The operation returned bounded evidence.
  returned: always
  type: bool
state:
  description: Whether the host reports the declared hostname.
  returned: on success
  type: str
hostname:
  description: The declared hostname.
  returned: on success
  type: str
reason:
  description: A closed refusal reason.
  returned: on refusal
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_hostname as worker


def execute(params):
    try:
        return dict(worker.execute(params["operation"], params["request"], params["hostname"]), ok=True)
    except worker.Refused as refused:
        reason = refused.reason if refused.reason in worker.REFUSALS else "hostname-write"
        return {"changed": False, "ok": False, "reason": reason}
    except Exception:  # pylint: disable=broad-exception-caught
        return {"changed": False, "ok": False, "reason": "hostname-write"}


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe"]},
            "request": {"type": "str", "required": True},
            "hostname": {"type": "str", "required": True},
        },
        supports_check_mode=False,
    )
    module.exit_json(**execute(module.params))


if __name__ == "__main__":
    main()
