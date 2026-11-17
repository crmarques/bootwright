"""Emit a managed unit's capability protocol on the runner's result channel.

The managed network services and the artifact server run one lifecycle, and
each task names its kind, so each keeps its own capability name, evidence list
and refusals on the wire. This runs on the controller, never the managed host,
because the inherited result and authorization descriptors belong to the
invoking Bootwright process.
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
SERVICE_REFUSALS = ("container-runtime", "foreign-listener", "unassigned-bind-address", "readiness-timeout", "listener-failed")
# The artifact server names the same ones, and its readiness also a
# certificate it could not prove or one other than the bound one.
ARTIFACT_SERVER_REFUSALS = (
    "container-runtime", "foreign-listener", "unassigned-bind-address", "readiness-timeout",
    "readiness-unproved", "listener-failed", "certificate-mismatch",
)


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


SERVICE = managed_unit_capability("managed-service", "the managed service", SERVICE_REFUSALS, "answers", answer_evidence)
ARTIFACT_SERVER = managed_unit_capability(
    "artifact-server", "the artifact server", ARTIFACT_SERVER_REFUSALS, "listeners", listener_evidence)
KINDS = {"managed-service": SERVICE, "artifact-server": ARTIFACT_SERVER}


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        unit = KINDS.get(self._task.args.get("kind"))
        if unit is None:
            return {"failed": True, "msg": "unsupported managed unit protocol kind"}
        return adapter_protocol.publish(self._task.args, unit.capability, emit)
