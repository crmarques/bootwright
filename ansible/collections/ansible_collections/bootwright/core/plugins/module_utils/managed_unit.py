"""The one completion protocol of a service run as a managed container unit.

A managed network service and the artifact server report the same unit,
container, content root and start after their files, and differ only in what
their listeners answered: the list that carries it and how one entry of it is
validated.
"""

from __future__ import annotations

from types import SimpleNamespace

from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached

MAX_ENTRIES = 64


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


def ordered(evidence):
    """Evidence with its keys in one order for every kind: sorted, as the Go field names are."""
    return dict(sorted(evidence.items()))


def refused_by(refusals):
    """The reason a refusal is named by: the refusal and the port it is about.

    Go gave a diagnostic for each port the request binds, so a reason or a port
    outside that set would break the runner's protocol and is never published.
    """
    def refused(arguments):
        reason, port = arguments.get("reason"), arguments.get("port")
        if reason not in refusals:
            raise ValueError("refusal reason")
        if isinstance(port, str) and port.isdigit() and port == str(int(port)):
            port = int(port)
        if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
            raise ValueError("refused port")
        return "%s-%d" % (reason, port)
    return refused


def managed_unit_capability(name, subject, refusals, list_key, entry):
    """One managed unit's protocol: its presence, absence and completion, and the capability publishing them.

    `list_key` names the evidence list of what the listeners answered and
    `entry` validates one element of it, raising ValueError for one it refuses.
    """
    def presence(observation, entries, request_digest):
        """Presence evidence; a service that started before a file it runs from runs an earlier one."""
        if len(entries) > MAX_ENTRIES:
            raise ValueError("%s count" % list_key)
        return ordered({
            "absent": False,
            "container": str(observation.get("container", "")),
            "contentRoot": bool(observation.get("contentRoot", False)),
            list_key: [entry(item) for item in entries],
            "postcondition": (
                observation.get("unit") == "active"
                and bool(observation.get("contentRoot"))
                and bool(observation.get("startedAfterFiles"))
            ),
            "request": adapter_protocol.request_digest(request_digest),
            "startedAfterFiles": bool(observation.get("startedAfterFiles", False)),
            "unit": str(observation.get("unit", "")),
        })

    def absence(observation, request_digest):
        gone = (
            not observation.get("unit")
            and not observation.get("container")
            and not observation.get("containerPresent")
            and not observation.get("contentRoot")
        )
        return ordered({
            "absent": True,
            "container": "",
            "contentRoot": False,
            list_key: [],
            "postcondition": bool(gone),
            "request": adapter_protocol.request_digest(request_digest),
            "startedAfterFiles": False,
            "unit": "",
        })

    def completion(arguments):
        """The evidence one completion publishes, or what it names when unmet."""
        request_digest = arguments.get("digest")
        observation = arguments.get("observation") or {}
        if arguments.get("removed"):
            evidence = absence(observation, request_digest)
        else:
            evidence = presence(observation, arguments.get(list_key) or [], request_digest)
        if publishes(evidence, arguments.get("observed")):
            return evidence, None
        if arguments.get("removed"):
            return evidence, unreached(subject, "still present", remaining(observation))
        return evidence, unreached(subject, "not proved", unproved(observation))

    refused = refused_by(refusals)
    capability = adapter_protocol.Capability(
        name,
        "the %s capability result could not be published" % name,
        completion,
        phases=adapter_protocol.REFUSING_PHASES,
        refusals=refused,
    )
    return SimpleNamespace(
        presence=presence, absence=absence, completion=completion, refused=refused,
        remaining=remaining, unproved=unproved, publishes=publishes, capability=capability,
    )
