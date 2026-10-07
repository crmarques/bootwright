"""Emit the boot-media capability protocol on the runner's result channel.

This runs on the controller, never the artifact server's placement host,
because the inherited result and authorization descriptors belong to the
invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)


def recorded(value):
    """One receipt field, bounded and never a path or a token."""
    value = str(value or "").strip()
    if len(value) > 128:
        raise ValueError("bounded value")
    return value


def presence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    evidence = {
        "absent": False,
        "image": bool(observation.get("image")),
        "inputs": recorded(observation.get("inputs")),
        "installer": recorded(observation.get("installer")),
        "postcondition": False,
        "request": adapter_protocol.request_digest(request_digest),
        "work": bool(observation.get("work")),
    }
    evidence["postcondition"] = bool(
        evidence["image"] and evidence["inputs"] == evidence["request"] and evidence["installer"]
    )
    return evidence


def absence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    gone = not any(observation.get(name) for name in ("image", "work"))
    return {
        "absent": True,
        "image": False,
        "inputs": "",
        "installer": "",
        "postcondition": bool(gone),
        "request": adapter_protocol.request_digest(request_digest),
        "work": False,
    }


def remaining(arguments):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    observation = arguments.get("observation") or {}
    return [name for name in ("image", "work") if observation.get(name)]


def unproved(evidence):
    """What a completion proof still lacks, named for the same reason."""
    names = []
    if not evidence["image"]:
        names.append("image")
    if evidence["inputs"] != evidence["request"]:
        names.append("inputs of another request")
    if not evidence["installer"]:
        names.append("installer version")
    return names


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet."""
    request_digest = arguments.get("digest")
    if arguments.get("removed"):
        evidence = absence(arguments, request_digest)
        unmet, verb = remaining(arguments), "still present"
    else:
        evidence = presence(arguments, request_digest)
        unmet, verb = unproved(evidence), "not proved"
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, unreached("the boot media", verb, unmet)


CAPABILITY = adapter_protocol.Capability(
    "boot-media",
    "the boot-media capability result could not be published",
    completion,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
