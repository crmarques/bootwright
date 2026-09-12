"""Emit the artifact-server capability protocol on the runner's result channel.

This runs on the controller, never the managed host, because the inherited
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
MAX_LISTENERS = 64
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def listener_evidence(entry):
    if set(entry) != {"address", "fingerprint", "name", "port", "protocol", "status"}:
        raise ValueError("listener evidence")
    if entry["protocol"] not in ("http", "https"):
        raise ValueError("listener protocol")
    if entry["fingerprint"]:
        digest(entry["fingerprint"])
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


def presence(request, observation, listeners, request_digest):
    if len(listeners) > MAX_LISTENERS:
        raise ValueError("listener count")
    return {
        "absent": False,
        "container": str(observation.get("container", "")),
        "contentRoot": bool(observation.get("contentRoot", False)),
        "listeners": [listener_evidence(entry) for entry in listeners],
        "postcondition": observation.get("unit") == "active" and bool(observation.get("contentRoot")),
        "request": digest(request_digest),
        "unit": str(observation.get("unit", "")),
    }


def absence(observation, request_digest):
    gone = (
        not observation.get("unit")
        and not observation.get("container")
        and not observation.get("containerPresent")
        and not observation.get("contentRoot")
    )
    return {
        "absent": True,
        "container": "",
        "contentRoot": False,
        "listeners": [],
        "postcondition": bool(gone),
        "request": digest(request_digest),
        "unit": "",
    }


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported artifact-server protocol phase"}
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
                evidence = presence(
                    arguments.get("request") or {},
                    observation,
                    arguments.get("listeners") or [],
                    request_digest,
                )
            if not evidence["postcondition"]:
                return {"failed": True, "msg": "the artifact server did not reach its postcondition"}
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the artifact-server capability result could not be published"}
