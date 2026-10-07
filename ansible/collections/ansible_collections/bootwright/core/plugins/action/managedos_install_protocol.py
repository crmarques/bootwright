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

PHASES = ("loaded", "group", "refused", "completed")
# The refusals this installation names to its runner before the run fails,
# each one the runner reports as the installation's own diagnostic for its
# Machine: those of the target's pre-boot proof (internal/substrate,
# preboot.go), and those of a store entry that no longer has the size and
# SHA-256 the operation froze (internal/managedos/installation, selection.go).
REFUSALS = ("hardware-mismatch", "identity-mismatch", "machine-running")
MEDIA_REFUSALS = ("media-changed-boot", "media-changed-tree")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
POWER_STATES = ("", "On", "Off")
MAX_MARKER = 4096
MAX_HOST_KEY = 4096
# Every piece of content this installation publishes, as its inspection names
# it; treeContent is anything left at the package tree's path, which a removal
# stopped part way can leave without the marker that makes the tree complete.
# treeStaging and work are what an attempt killed part way leaves: the tree it
# was extracting beneath the served root and the area it built the image in.
CONTENT = ("image", "private", "tree", "treeContent", "treeStaging", "work")
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def tree_identity(value):
    """The digest of the image the published tree was extracted from, or ''."""
    value = value or ""
    if value != "" and (not isinstance(value, str) or len(value) != 64 or set(value) - HEX):
        raise ValueError("tree identity")
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
        "treeContent": bool(observation.get("treeContent")),
        "treeIdentity": tree_identity(observation.get("treeIdentity")),
        "treeStaging": bool(observation.get("treeStaging")),
        "work": bool(observation.get("work")),
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
    return [name for name in CONTENT if observation.get(name)]


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
    gone = not any(observation.get(name) for name in CONTENT)
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
        "treeContent": False,
        "treeIdentity": "",
        "treeStaging": False,
        "work": False,
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
            if phase == "refused":
                reason = arguments.get("reason")
                if reason not in REFUSALS + MEDIA_REFUSALS:
                    raise ValueError("refusal reason")
                emit({"phase": "refused", "reason": reason})
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
