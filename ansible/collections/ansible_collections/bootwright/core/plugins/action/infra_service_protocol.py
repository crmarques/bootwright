"""Emit the managed-service capability protocol on the runner's result channel.

This runs on the controller, never the managed host, because the inherited
result and authorization descriptors belong to the invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)
from ansible_collections.bootwright.core.plugins.module_utils.managed_unit import managed_unit_capability

# The refusals a managed service names to its runner before the run fails, each
# one the runner reports as the service's own diagnostic for a port: an
# missing container runtime, an unassigned address or a socket another daemon
# holds, checked before effects.
REFUSALS = ("container-runtime", "foreign-listener", "unassigned-bind-address", "readiness-timeout", "listener-failed")


def answer_evidence(entry):
    if set(entry) - {"fingerprint"} != {"address", "answer", "port"}:
        raise ValueError("answer evidence")
    if not isinstance(entry["port"], int) or isinstance(entry["port"], bool):
        raise ValueError("answer port")
    if not entry["answer"]:
        raise ValueError("answer")
    evidence = {
        "address": str(entry["address"]),
        "answer": str(entry["answer"]),
        "port": int(entry["port"]),
    }
    if "fingerprint" in entry:
        if not isinstance(entry["fingerprint"], str) or len(entry["fingerprint"]) != 64 or entry["fingerprint"].strip("0123456789abcdef"):
            raise ValueError("answer fingerprint")
        evidence["fingerprint"] = entry["fingerprint"]
    return evidence


UNIT = managed_unit_capability("managed-service", "the managed service", REFUSALS, "answers", answer_evidence)
presence, absence, completion, refused = UNIT.presence, UNIT.absence, UNIT.completion, UNIT.refused
remaining, unproved, publishes = UNIT.remaining, UNIT.unproved, UNIT.publishes
CAPABILITY = UNIT.capability


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
