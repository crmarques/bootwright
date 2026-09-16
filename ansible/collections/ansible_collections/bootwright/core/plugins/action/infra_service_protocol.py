"""Emit the managed-service capability protocol on the runner's result channel.

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
MAX_ANSWERS = 64
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def answer_evidence(entry):
    if set(entry) != {"address", "answer", "port"}:
        raise ValueError("answer evidence")
    if not isinstance(entry["port"], int) or isinstance(entry["port"], bool):
        raise ValueError("answer port")
    if not entry["answer"]:
        raise ValueError("answer")
    return {
        "address": str(entry["address"]),
        "answer": str(entry["answer"]),
        "port": int(entry["port"]),
    }


def presence(observation, answers, request_digest):
    if len(answers) > MAX_ANSWERS:
        raise ValueError("answer count")
    return {
        "absent": False,
        "answers": [answer_evidence(entry) for entry in answers],
        "container": str(observation.get("container", "")),
        "contentRoot": bool(observation.get("contentRoot", False)),
        "postcondition": observation.get("unit") == "active" and bool(observation.get("contentRoot")),
        "request": digest(request_digest),
        "unit": str(observation.get("unit", "")),
    }


def remaining(observation):
    """What an absence proof still sees, named so the refusal can say so."""
    names = ["unit"] if observation.get("unit") else []
    if observation.get("container") or observation.get("containerPresent"):
        names.append("container")
    if observation.get("contentRoot"):
        names.append("contentRoot")
    return names


def unproved(observation):
    """What a presence proof still lacks, named for the same reason."""
    names = [] if observation.get("unit") == "active" else ["unit"]
    if not observation.get("contentRoot"):
        names.append("contentRoot")
    return names


def absence(observation, request_digest):
    gone = (
        not observation.get("unit")
        and not observation.get("container")
        and not observation.get("containerPresent")
        and not observation.get("contentRoot")
    )
    return {
        "absent": True,
        "answers": [],
        "container": "",
        "contentRoot": False,
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
            return {"failed": True, "msg": "unsupported managed-service protocol phase"}
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
                evidence = presence(observation, arguments.get("answers") or [], request_digest)
            if not evidence["postcondition"]:
                unmet = remaining(observation) if arguments.get("removed") else unproved(observation)
                verb = "still present" if arguments.get("removed") else "not proved"
                return {
                    "failed": True,
                    "msg": "the managed service did not reach its postcondition; %s: %s" % (verb, ", ".join(unmet) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the managed-service capability result could not be published"}
