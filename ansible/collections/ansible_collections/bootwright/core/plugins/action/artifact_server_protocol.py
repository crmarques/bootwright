"""Emit the artifact-server capability protocol on the runner's result channel.

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

# The refusal the artifact server names to its runner before the run fails, the
# runner reporting it as the server's own diagnostic for a port: a socket
# something else already listens on, checked before the unit starts.
REFUSALS = ("foreign-listener",)
MAX_LISTENERS = 64


def listener_evidence(entry):
    if set(entry) != {"address", "fingerprint", "name", "port", "protocol", "status"}:
        raise ValueError("listener evidence")
    if entry["protocol"] not in ("http", "https"):
        raise ValueError("listener protocol")
    if entry["fingerprint"]:
        adapter_protocol.request_digest(entry["fingerprint"])
    if not isinstance(entry["port"], int) or isinstance(entry["port"], bool):
        raise ValueError("listener port")
    return {
        "address": str(entry["address"]),
        "fingerprint": str(entry["fingerprint"]),
        "name": str(entry["name"]),
        "port": int(entry["port"]),
        "protocol": str(entry["protocol"]),
        "status": str(entry["status"]),
    }


def presence(request, observation, listeners, request_digest):
    if len(listeners) > MAX_LISTENERS:
        raise ValueError("listener count")
    return {
        "absent": False,
        "container": str(observation.get("container", "")),
        "contentRoot": bool(observation.get("contentRoot", False)),
        "listeners": [listener_evidence(entry) for entry in listeners],
        "postcondition": observation.get("unit") == "active" and bool(observation.get("contentRoot")),
        "request": adapter_protocol.request_digest(request_digest),
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
        "container": "",
        "contentRoot": False,
        "listeners": [],
        "postcondition": bool(gone),
        "request": adapter_protocol.request_digest(request_digest),
        "unit": "",
    }


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet."""
    request_digest = arguments.get("digest")
    observation = arguments.get("observation") or {}
    if arguments.get("removed"):
        evidence = absence(observation, request_digest)
    else:
        evidence = presence(
            arguments.get("request") or {},
            observation,
            arguments.get("listeners") or [],
            request_digest,
        )
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    if arguments.get("removed"):
        return evidence, unreached("the artifact server", "still present", remaining(observation))
    return evidence, unreached("the artifact server", "not proved", unproved(observation))


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
    "artifact-server",
    "the artifact-server capability result could not be published",
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
