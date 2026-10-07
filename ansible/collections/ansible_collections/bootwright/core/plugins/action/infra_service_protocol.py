"""Emit the managed-service capability protocol on the runner's result channel.

This runs on the controller, never the managed host, because the inherited
result and authorization descriptors belong to the invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

# The refusals a managed service names to its runner before the run fails, each
# one the runner reports as the service's own diagnostic for a port: a socket
# something else already listens on, checked before the unit starts.
REFUSALS = ("foreign-listener",)
MAX_ANSWERS = 64


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
    """Presence evidence; a service that started before a file it runs from runs an earlier one."""
    if len(answers) > MAX_ANSWERS:
        raise ValueError("answer count")
    return {
        "absent": False,
        "answers": [answer_evidence(entry) for entry in answers],
        "container": str(observation.get("container", "")),
        "contentRoot": bool(observation.get("contentRoot", False)),
        "postcondition": (
            observation.get("unit") == "active"
            and bool(observation.get("contentRoot"))
            and bool(observation.get("startedAfterFiles"))
        ),
        "request": adapter_protocol.request_digest(request_digest),
        "startedAfterFiles": bool(observation.get("startedAfterFiles", False)),
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
    if not observation.get("startedAfterFiles"):
        names.append("startedAfterFiles")
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
        "request": adapter_protocol.request_digest(request_digest),
        "startedAfterFiles": False,
        "unit": "",
    }


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet."""
    request_digest = arguments.get("digest")
    observation = arguments.get("observation") or {}
    if arguments.get("removed"):
        evidence = absence(observation, request_digest)
    else:
        evidence = presence(observation, arguments.get("answers") or [], request_digest)
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    if arguments.get("removed"):
        return evidence, unreached("the managed service", "still present", remaining(observation))
    return evidence, unreached("the managed service", "not proved", unproved(observation))


def refused(arguments):
    """The reason a refusal is named by: the refusal and the port it is about.

    Go gave a diagnostic for each port the request binds, so a reason or a port
    outside that set would break the runner's protocol and is never published.
    """
    reason, port = arguments.get("reason"), arguments.get("port")
    if reason not in REFUSALS:
        raise ValueError("refusal reason")
    if isinstance(port, str) and port.isdigit() and port == str(int(port)):
        port = int(port)
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise ValueError("refused port")
    return "%s-%d" % (reason, port)


CAPABILITY = adapter_protocol.Capability(
    "managed-service",
    "the managed-service capability result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=refused,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
