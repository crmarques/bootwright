"""Publish the machine hardware read's bounded lifecycle evidence."""
from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_ADDRESSES = 256
ADDRESS = re.compile(r"[0-9a-f]{2}(:[0-9a-f]{2}){5}")


def completion(arguments):
    addresses = arguments.get("addresses") or []
    if not isinstance(addresses, list) or len(addresses) > MAX_ADDRESSES:
        raise ValueError("machine hardware addresses")
    if not all(isinstance(address, str) and ADDRESS.fullmatch(address) for address in addresses):
        raise ValueError("machine hardware address")
    removed = bool(arguments.get("removed"))
    if removed and addresses:
        raise ValueError("machine hardware release")
    evidence = {"absent": removed, "addresses": sorted(set(addresses)), "postcondition": True,
                "request": adapter_protocol.request_digest(arguments.get("digest")), "version": "machine-hardware-evidence-v1"}
    return evidence, None


CAPABILITY = adapter_protocol.Capability(
    "machine-hardware",
    "the machine hardware result could not be published",
    completion,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
