"""Emit the machine capability protocol on the runner's result channel.

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
POWER_STATES = ("", "On", "Off")
MAX_DISKS = 32
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def disk_evidence(entry):
    if set(entry) != {"name", "present", "sizeGiB"}:
        raise ValueError("disk evidence")
    if not isinstance(entry["sizeGiB"], int) or isinstance(entry["sizeGiB"], bool):
        raise ValueError("disk size")
    return {"name": str(entry["name"]), "present": bool(entry["present"]), "sizeGiB": int(entry["sizeGiB"])}


def presence(observation, power, system, request_digest):
    disks = observation.get("disks") or []
    if len(disks) > MAX_DISKS:
        raise ValueError("disk count")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "controller": str(observation.get("controller", "")),
        "disks": [disk_evidence(entry) for entry in disks],
        "domain": str(observation.get("domain", "")),
        "owned": bool(observation.get("owned")),
        "postcondition": False,
        "power": str(power),
        "request": digest(request_digest),
        "system": str(system or ""),
        "unit": str(observation.get("unit", "")),
    }
    complete = (
        bool(evidence["domain"]) and evidence["owned"] and evidence["unit"] == "active"
        and bool(evidence["controller"]) and bool(evidence["system"]) and bool(evidence["power"])
        and all(entry["present"] for entry in evidence["disks"]) and bool(evidence["disks"])
    )
    evidence["postcondition"] = complete
    return evidence


def remaining(observation):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    names = []
    for name in ("domain", "unit", "controller"):
        if observation.get(name):
            names.append(name)
    if any(entry.get("present") for entry in observation.get("disks") or []):
        names.append("disks")
    return names


def unproved(evidence):
    """What a presence proof still lacks, named for the same reason."""
    names = []
    if not evidence["domain"]:
        names.append("domain")
    if not evidence["owned"]:
        names.append("ownership")
    if evidence["unit"] != "active":
        names.append("unit")
    for name in ("controller", "system", "power"):
        if not evidence[name]:
            names.append(name)
    if not evidence["disks"] or not all(entry["present"] for entry in evidence["disks"]):
        names.append("disks")
    return names


def absence(observation, request_digest):
    disks = [disk_evidence(entry) for entry in observation.get("disks") or []]
    gone = (
        not observation.get("domain") and not observation.get("unit")
        and not observation.get("controller") and not any(entry["present"] for entry in disks)
    )
    return {
        "absent": True,
        "controller": "",
        "disks": [],
        "domain": "",
        "owned": False,
        "postcondition": bool(gone),
        "power": "",
        "request": digest(request_digest),
        "system": "",
        "unit": "",
    }


def publishes(evidence, observed):
    """Whether this phase may publish evidence proving no postcondition.

    A read-only observation reports what it found, including a target that is
    part way realized, because the engine resolves an unproved effect from that
    evidence. A mutation has to reach its postcondition or fail.
    """
    return bool(evidence["postcondition"]) or bool(observed)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported machine protocol phase"}
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
                unmet, verb = remaining(observation), "still present"
            else:
                evidence = presence(observation, arguments.get("power"), arguments.get("system"), request_digest)
                unmet, verb = unproved(evidence), "not proved"
            if not publishes(evidence, arguments.get("observed")):
                return {
                    "failed": True,
                    "msg": "the machine did not reach its postcondition; %s: %s" % (verb, ", ".join(unmet) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the machine capability result could not be published"}
