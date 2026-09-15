"""Emit the installation capability protocol on the runner's result channel.

This runs on the controller, never the artifact server's placement host,
because the inherited result and authorization descriptors belong to the
invoking Bootwright process.
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
MAX_MARKER = 4096
MAX_HOST_KEY = 4096
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def bounded(value, limit):
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


def presence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    power = arguments.get("power")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "address": bounded(arguments.get("address"), 128),
        "hostKey": bounded(arguments.get("hostKey"), MAX_HOST_KEY),
        "image": bool(observation.get("image")),
        "marker": bounded(arguments.get("marker"), MAX_MARKER),
        "media": bounded(arguments.get("media"), 1024),
        "postcondition": False,
        "power": str(power),
        "request": digest(request_digest),
        "tree": bool(observation.get("tree")),
    }
    evidence["postcondition"] = bool(
        evidence["marker"] and evidence["hostKey"] and evidence["address"]
        and not evidence["media"] and evidence["power"] == "On" and evidence["image"]
    )
    return evidence


def absence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    gone = not observation.get("image") and not observation.get("tree")
    return {
        "absent": True,
        "address": "",
        "hostKey": "",
        "image": False,
        "marker": "",
        "media": "",
        "postcondition": bool(gone),
        "power": "",
        "request": digest(request_digest),
        "tree": False,
    }


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported installation protocol phase"}
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
            if arguments.get("removed"):
                evidence = absence(arguments, request_digest)
            else:
                evidence = presence(arguments, request_digest)
            if not evidence["postcondition"]:
                return {"failed": True, "msg": "the installation did not reach its postcondition"}
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the installation capability result could not be published"}
