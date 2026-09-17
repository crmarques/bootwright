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
        "private": bool(observation.get("private")),
        "reachable": bool(arguments.get("reachable")),
        "request": digest(request_digest),
        "tree": bool(observation.get("tree")),
    }
    evidence["postcondition"] = bool(
        evidence["marker"] and evidence["hostKey"] and evidence["address"]
        and not evidence["media"] and evidence["power"] == "On" and evidence["image"]
        and not evidence["private"] and evidence["reachable"]
    )
    return evidence


def remaining(arguments):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    observation = arguments.get("observation") or {}
    return [name for name in ("image", "private", "tree") if observation.get(name)]


def unproved(evidence):
    """What a completion proof still lacks, named for the same reason."""
    names = [name for name in ("marker", "hostKey", "address") if not evidence[name]]
    if evidence["media"]:
        names.append("media")
    if evidence["private"]:
        names.append("private material still published")
    if evidence["power"] != "On":
        names.append("power")
    if not evidence["image"]:
        names.append("image")
    if not evidence["reachable"]:
        names.append("reachable")
    return names


def absence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    gone = not any(observation.get(name) for name in ("image", "private", "tree"))
    return {
        "absent": True,
        "address": "",
        "hostKey": "",
        "image": False,
        "marker": "",
        "media": "",
        "postcondition": bool(gone),
        "power": "",
        "private": False,
        "reachable": False,
        "request": digest(request_digest),
        "tree": False,
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
                unmet, verb = remaining(arguments), "still present"
            else:
                evidence = presence(arguments, request_digest)
                unmet, verb = unproved(evidence), "not proved"
            if not publishes(evidence, arguments.get("observed")):
                return {
                    "failed": True,
                    "msg": "the installation did not reach its postcondition; %s: %s" % (verb, ", ".join(unmet) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the installation capability result could not be published"}
