"""Emit the artifact-server capability protocol on the runner's result channel.

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

# The refusal the artifact server names to its runner before the run fails, the
# runner reporting it as the server's own diagnostic for a port: a missing
# container runtime, an unassigned address or a socket another daemon holds,
# checked before effects.
REFUSALS = (
    "container-runtime", "foreign-listener", "unassigned-bind-address", "readiness-timeout",
    "readiness-unproved", "listener-failed", "certificate-mismatch",
)


def listener_evidence(entry):
    if set(entry) != {"address", "fingerprint", "name", "port", "protocol", "status"}:
        raise ValueError("listener evidence")
    if entry["protocol"] not in ("http", "https"):
        raise ValueError("listener protocol")
    if entry["fingerprint"]:
        adapter_protocol.request_digest(entry["fingerprint"])
    if not isinstance(entry["port"], int) or isinstance(entry["port"], bool):
        raise ValueError("listener port")
    return {
        "address": str(entry["address"]),
        "fingerprint": str(entry["fingerprint"]),
        "name": str(entry["name"]),
        "port": int(entry["port"]),
        "protocol": str(entry["protocol"]),
        "status": str(entry["status"]),
    }


UNIT = managed_unit_capability("artifact-server", "the artifact server", REFUSALS, "listeners", listener_evidence)
presence, absence, completion, refused = UNIT.presence, UNIT.absence, UNIT.completion, UNIT.refused
remaining, unproved, publishes = UNIT.remaining, UNIT.unproved, UNIT.publishes
CAPABILITY = UNIT.capability


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
