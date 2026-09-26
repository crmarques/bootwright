"""Emit the cluster-installation capability protocol on the result channel.

This runs on the controller, never a managed host, because the inherited
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
HEX = set("0123456789abcdef")
MAX_NAMES = 128


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def bounded(value, limit=128):
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


def names(values):
    """One bounded list of node names, which carry no secret."""
    values = list(values or [])
    if len(values) > MAX_NAMES:
        raise ValueError("bounded list")
    return sorted(bounded(value) for value in values)


def evidence(arguments, request_digest, removed):
    """The bounded evidence one completion phase publishes.

    `completed` is the cluster's own report, resolved by the role's state read:
    only a real boolean true counts, so a string or a missing key is an
    installation not yet done rather than one the cluster finished.

    `ownMedia` names the nodes among `media` whose controller presents the
    image this cluster published, as the state read compared it. It carries
    names only, never the address, whose last segment is the unguessable one.
    """
    state = arguments.get("state") or {}
    found = {
        "absent": bool(removed),
        "cluster": bounded(state.get("cluster")),
        "completed": state.get("completed") is True,
        "identity": bounded(arguments.get("identity")),
        "media": names(state.get("media")),
        "missing": names(state.get("missing")),
        "ownMedia": names(state.get("ownMedia")),
        "postcondition": False,
        "powered": names(state.get("powered")),
        "release": bounded(state.get("release")),
        "request": digest(request_digest),
    }
    if removed:
        found["postcondition"] = not found["media"]
        return found
    found["postcondition"] = bool(
        found["identity"] and found["cluster"] == found["identity"]
        and found["release"] and found["completed"]
        and not found["missing"] and not found["media"]
    )
    return found


def unproved(found):
    """What a completion proof still lacks, named for a censored message.

    These are field names and node names, never credentials, so naming them is
    safe in a message `no_log` would otherwise censor with the evidence.
    """
    unmet = []
    if not found["identity"]:
        unmet.append("this operation recorded no cluster identity")
    elif found["cluster"] != found["identity"]:
        unmet.append("the cluster answering is not the one this operation installed")
    if not found["release"]:
        unmet.append("the cluster reports no release")
    if not found["completed"]:
        unmet.append("the cluster does not report its installation completed at the declared release")
    if found["missing"]:
        unmet.append("nodes missing: " + ", ".join(found["missing"]))
    if found["media"]:
        unmet.append("media still inserted on: " + ", ".join(found["media"]))
    return unmet


def publishes(found, observed):
    """Whether this phase may publish evidence proving no postcondition."""
    return bool(found["postcondition"]) or bool(observed)


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
            removed = bool(arguments.get("removed"))
            found = evidence(arguments, arguments.get("digest"), removed)
            if not publishes(found, arguments.get("observed")):
                unmet = ["media still inserted on: " + ", ".join(found["media"])] if removed else unproved(found)
                return {
                    "failed": True,
                    "msg": "the installation did not reach its postcondition; %s" % ("; ".join(unmet) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": found})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the installation capability result could not be published"}
