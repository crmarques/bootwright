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

PHASES = ("loaded", "group", "refused", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
# The refusals a power run names to its runner before it fails, each one the
# runner reports as its caller's own diagnostic (internal/machine/power).
REFUSALS = ("identity-mismatch",)
OUTCOMES = ("changed", "unchanged")
REPORTED = {"On": "on", "Off": "off", "": ""}
READINGS = {"On": "on", "Off": "off"}
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


def observed(result):
    """One controller's answer, or unknown when it did not give one.

    A controller that refused, timed out or answered with something this build
    cannot name leaves its own machine unknown, so one silent controller never
    denies the answers every other controller in the same run already gave.

    The reported state is what decides that. A reading suppresses its own task
    failure to keep polling the rest, and ansible-core rewrites `failed` to
    false when it does, so a refusal arrives here as a result carrying no
    usable state rather than as a flagged failure. The flags are still read,
    because a result that does carry one is not an answer either.
    """
    if not isinstance(result, dict):
        raise ValueError("reading")
    if result.get("failed") or result.get("unreachable"):
        return "unknown"
    return READINGS.get(result.get("power"), "unknown")


def reading_evidence_for(arguments):
    """Turn one loop of controller reads into evidence, in survey order."""
    readings = arguments.get("readings")
    if not isinstance(readings, list) or not readings:
        raise ValueError("readings")
    machines = []
    for result in readings:
        power = observed(result)
        target = result.get("item")
        if not isinstance(target, dict):
            raise ValueError("reading target")
        machine = str(target.get("object") or "")
        if not machine:
            raise ValueError("machine")
        machines.append({"machine": machine, "power": power})
    return {"machines": machines, "request": digest(arguments.get("digest"))}


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
            if phase == "refused":
                reason = arguments.get("reason")
                if reason not in REFUSALS:
                    raise ValueError("refusal reason")
                emit({"phase": "refused", "reason": reason})
                return {"changed": False}
            outcome = arguments.get("outcome")
            if outcome not in OUTCOMES:
                raise ValueError("outcome")
            if "readings" in arguments:
                emit({"phase": "completed", "outcome": outcome, "evidence": reading_evidence_for(arguments)})
                return {"changed": False}
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
