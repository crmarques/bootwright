#!/usr/bin/python
"""Run or observe the release-bound package update on one provided RHEL host."""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_provided_update
short_description: Update a provided RHEL host within one minor release
version_added: "0.1.0"
description:
  - Applies or reads the root-owned receipt for one provided host update.
  - Completes a changed transaction only after the host boot ID has changed.
options:
  operation:
    description: The receipt operation.
    type: str
    required: true
    choices:
      - apply
      - complete
      - observe
  owner:
    description: The SHA-256 identity of the original apply.
    type: str
    required: true
  request:
    description: The SHA-256 digest of the frozen request.
    type: str
    required: true
  release:
    description: The required major and minor RHEL release.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe a provided RHEL host update
  bootwright.core.managedos_provided_update:
    operation: observe
    owner: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    request: 123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0
    release: "9.8"
"""

RETURN = r"""
ok:
  description: The operation returned bounded evidence.
  returned: always
  type: bool
state:
  description: The receipt state.
  returned: on success
  type: str
packages:
  description: The number of packages installed by the transaction.
  returned: on success
  type: int
transaction:
  description: The DNF history transaction ID, or zero when nothing changed.
  returned: on success
  type: int
rebooted:
  description: A changed boot ID was proved after the transaction.
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


import contextlib
import os
import sys

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_provided_update as update
from ansible_collections.bootwright.core.plugins.module_utils.managedos_update_worker import Refused


@contextlib.contextmanager
def quiet_native():
    """Keep package manager and RPM scriptlet output out of the module channel."""
    descriptors = [os.dup(stream) for stream in (1, 2)]
    try:
        with open(os.devnull, "wb") as null:
            for stream in (1, 2):
                os.dup2(null.fileno(), stream)
        yield
    finally:
        sys.stdout.flush()
        sys.stderr.flush()
        for stream, saved in enumerate(descriptors, 1):
            os.dup2(saved, stream)
            os.close(saved)


def execute(params):
    try:
        with quiet_native():
            result = update.execute(params["operation"], params["owner"], params["request"], params["release"])
        return dict(result, ok=True)
    except Refused as refused:
        reason = refused.reason if refused.reason in update.REFUSALS else "update-failed"
        return {"changed": False, "ok": False, "reason": reason, "detail": refused.detail or refused.cause or ""}
    except Exception:  # pylint: disable=broad-exception-caught
        return {"changed": False, "ok": False, "reason": "update-failed", "detail": "transaction"}


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "complete", "observe"]},
            "owner": {"type": "str", "required": True},
            "request": {"type": "str", "required": True},
            "release": {"type": "str", "required": True},
        },
        supports_check_mode=False,
    )
    module.exit_json(**execute(module.params))


if __name__ == "__main__":
    main()
