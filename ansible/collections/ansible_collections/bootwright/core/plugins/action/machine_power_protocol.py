"""Emit the machine power protocol on the runner's result channel.

This runs on the controller, never the host that reaches the management
controller, because the inherited result descriptor belongs to the invoking
Bootwright process. A power request is not evidence: the state published here
is the state the controller reported once the operation settled.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

PHASES = ("loaded", "group", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
REPORTED = {"On": "on", "Off": "off", "": ""}
VERBS = {"start": "on", "stop": "off", "restart": "on"}
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def state(value):
    """Translate one reported Redfish state, refusing anything else."""
    if value not in REPORTED:
        raise ValueError("power state")
    return REPORTED[value]


def evidence_for(arguments):
    verb = arguments.get("verb")
    if verb not in VERBS:
        raise ValueError("verb")
    machine = str(arguments.get("machine") or "")
    if not machine:
        raise ValueError("machine")
    power, previous = state(arguments.get("power")), state(arguments.get("previous"))
    evidence = {
        "changed": bool(arguments.get("changed")),
        "machine": machine,
        "postcondition": power == VERBS[verb],
        "power": power,
        "previous": previous,
        "request": digest(arguments.get("digest")),
    }
    return evidence


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported power protocol phase"}
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
            evidence = evidence_for(arguments)
            if not evidence["postcondition"]:
                return {
                    "failed": True,
                    "msg": "the machine did not reach the power state this operation asked for",
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the machine power result could not be published"}
