"""Emit the provider host capability protocol on the runner's result channel.

This runs on the controller, never the provider host, because the inherited
result and authorization descriptors belong to the invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

PHASES = ("loaded", "group", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
MAX_NETWORKS = 64
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def network_evidence(entry):
    if set(entry) != {"bridge", "managed", "name", "owned", "state"}:
        raise ValueError("network evidence")
    return {
        "bridge": bool(entry["bridge"]),
        "managed": bool(entry["managed"]),
        "name": str(entry["name"]),
        "owned": bool(entry["owned"]),
        "state": str(entry["state"]),
    }


def presence(observation, request_digest):
    networks = observation.get("networks") or []
    if len(networks) > MAX_NETWORKS:
        raise ValueError("network count")
    evidence = {
        "absent": False,
        "hypervisor": bool(observation.get("hypervisor")),
        "networks": [network_evidence(entry) for entry in networks],
        "pool": str(observation.get("pool", "")),
        "postcondition": False,
        "request": digest(request_digest),
        "service": str(observation.get("service", "")),
        "uri": bool(observation.get("uri")),
    }
    complete = evidence["hypervisor"] and evidence["service"] == "active" and evidence["uri"] and evidence["pool"] == "active"
    for entry in evidence["networks"]:
        complete = complete and entry["bridge"] and (not entry["managed"] or (entry["owned"] and entry["state"] == "active"))
    evidence["postcondition"] = complete
    return evidence


def absence(observation, request_digest):
    networks = [network_evidence(entry) for entry in observation.get("networks") or []]
    gone = not observation.get("pool") and not any(entry["managed"] for entry in networks)
    return {
        "absent": True,
        "hypervisor": False,
        "networks": [entry for entry in networks if not entry["managed"]],
        "pool": "",
        "postcondition": bool(gone),
        "request": digest(request_digest),
        "service": "",
        "uri": False,
    }


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported provider host protocol phase"}
        try:
            if phase == "loaded":
                emit({"phase": "loaded"}, acknowledge=True)
                return {"changed": False}
            if phase == "group":
                status = arguments.get("status")
                if status not in GROUP_STATUSES:
                    raise ValueError("group status")
                emit({"phase": "group", "group": str(arguments.get("group")), "status": status})
                return {"changed": False}
            outcome = arguments.get("outcome")
            if outcome not in OUTCOMES:
                raise ValueError("outcome")
            request_digest = arguments.get("digest")
            observation = arguments.get("observation") or {}
            if arguments.get("removed"):
                evidence = absence(observation, request_digest)
            else:
                evidence = presence(observation, request_digest)
            if not evidence["postcondition"]:
                return {"failed": True, "msg": "the provider host did not reach its postcondition"}
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the provider host capability result could not be published"}
