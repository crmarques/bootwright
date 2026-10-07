"""Emit the provider host capability protocol on the runner's result channel.

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
MAX_NETWORKS = 64
MAX_SERVICES = 16
HEX = set("0123456789abcdef")
# The observation carries each network's UUID so a definition can be offered
# back to libvirt under the identity it already holds. Evidence stays narrower:
# Go validates exactly the facts below, and rejects any field it does not know.
OBSERVED_NETWORK = {"answered", "autostart", "bridge", "definition", "managed", "name", "owned", "state", "uuid"}
OBSERVED_SERVICE = {"enabled", "name", "state"}


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def network_evidence(entry):
    if set(entry) != OBSERVED_NETWORK:
        raise ValueError("network evidence")
    return {
        "answered": bool(entry["answered"]),
        "autostart": bool(entry["autostart"]),
        "bridge": bool(entry["bridge"]),
        "definition": bool(entry["definition"]),
        "managed": bool(entry["managed"]),
        "name": str(entry["name"]),
        "owned": bool(entry["owned"]),
        "state": str(entry["state"]),
    }


def service_evidence(entry):
    if set(entry) != OBSERVED_SERVICE:
        raise ValueError("service evidence")
    return {
        "enabled": bool(entry["enabled"]),
        "name": str(entry["name"]),
        "state": str(entry["state"]),
    }


def services_of(observation):
    services = observation.get("services") or []
    if len(services) > MAX_SERVICES:
        raise ValueError("service count")
    return [service_evidence(entry) for entry in services]


def networks_of(observation):
    networks = observation.get("networks") or []
    if len(networks) > MAX_NETWORKS:
        raise ValueError("network count")
    return [network_evidence(entry) for entry in networks]


def directory(observation):
    """Whether the pool directory exists, observed by its path.

    Evidence without it proves the directory neither present nor absent, so
    an observation that does not report it is never published.
    """
    value = observation.get("directory")
    if not isinstance(value, bool):
        raise ValueError("directory")
    return value


def realized(entry):
    """Whether a managed network is answered for, owned, active, starts with the host and carries its frozen definition."""
    return entry["answered"] and entry["owned"] and entry["state"] == "active" and entry["autostart"] and entry["definition"]


def pool_realized(evidence):
    """Whether the pool is answered for, active, starts with the host and targets the frozen directory."""
    return evidence["poolAnswered"] and evidence["pool"] == "active" and evidence["poolAutostart"] and evidence["poolOwned"]


def presence(observation, request_digest):
    evidence = {
        "absent": False,
        "directory": directory(observation),
        "hypervisor": bool(observation.get("hypervisor")),
        "networks": networks_of(observation),
        "pool": str(observation.get("pool", "")),
        "poolAnswered": bool(observation.get("poolAnswered")),
        "poolAutostart": bool(observation.get("poolAutostart")),
        "poolOwned": bool(observation.get("poolOwned")),
        "postcondition": False,
        "request": digest(request_digest),
        "services": services_of(observation),
        "uri": bool(observation.get("uri")),
    }
    complete = evidence["hypervisor"] and evidence["uri"] and pool_realized(evidence)
    for entry in evidence["services"]:
        complete = complete and entry["state"] == "active" and entry["enabled"]
    for entry in evidence["networks"]:
        complete = complete and entry["bridge"] and (not entry["managed"] or realized(entry))
    evidence["postcondition"] = complete
    return evidence


def still_defined(entry):
    """Whether a managed network the hypervisor still defines remains.

    `managed` echoes the request and is true for every network this context
    owns, before and after removal, so it proves nothing on its own. What the
    hypervisor answers for the name is the only observation of absence.
    """
    return bool(entry["managed"]) and bool(entry["state"] or entry["owned"])


def remaining(observation):
    """What an absence proof still sees, named so the refusal can say so."""
    names = ["pool"] if observation.get("pool") else []
    networks = [network_evidence(entry) for entry in observation.get("networks") or []]
    if any(still_defined(entry) for entry in networks):
        names.append("networks")
    if observation.get("directory"):
        names.append("directory")
    return names


def unproved(evidence):
    """What a presence proof still lacks, named for the same reason."""
    names = []
    if not evidence["hypervisor"]:
        names.append("hypervisor")
    if not evidence["uri"]:
        names.append("uri")
    for entry in evidence["services"]:
        if entry["state"] != "active" or not entry["enabled"]:
            names.append("services")
            break
    if not pool_realized(evidence):
        names.append("pool")
    for entry in evidence["networks"]:
        if not entry["bridge"] or (entry["managed"] and not realized(entry)):
            names.append("networks")
            break
    return names


def unanswered(evidence):
    """What the driver that owns it did not answer for, named for a refusal.

    The uri answering proves only that the hypervisor did. A managed network
    and the pool each live in a driver of their own, and one that is silent
    reports them undefined exactly as one that removed them does.
    """
    names = []
    if any(entry["managed"] and not entry["answered"] for entry in evidence["networks"]):
        names.append("networks")
    if not evidence["poolAnswered"]:
        names.append("pool")
    return names


def gone(evidence):
    """Whether the evidence proves every owned network, the pool and its directory gone.

    A connection that does not answer reports no network and no pool either,
    and neither does a driver that did not answer for one, so only a network
    and a pool their own driver answered for are proved absent.
    """
    return (
        evidence["uri"] is True
        and not unanswered(evidence)
        and evidence["pool"] == ""
        and not any(still_defined(entry) for entry in evidence["networks"])
        and evidence["directory"] is False
    )


def absence(observation, request_digest):
    """Removal evidence carrying what was observed, proved only by gone()."""
    evidence = {
        "absent": True,
        "directory": directory(observation),
        "hypervisor": bool(observation.get("hypervisor")),
        "networks": networks_of(observation),
        "pool": str(observation.get("pool", "")),
        "poolAnswered": bool(observation.get("poolAnswered")),
        "poolAutostart": bool(observation.get("poolAutostart")),
        "poolOwned": bool(observation.get("poolOwned")),
        "postcondition": False,
        "request": digest(request_digest),
        "services": services_of(observation),
        "uri": bool(observation.get("uri")),
    }
    evidence["postcondition"] = gone(evidence)
    return evidence


def publishes(evidence, observed):
    """Whether this phase may publish evidence proving no postcondition.

    A read-only observation reports what it found, including a target that is
    part way realized, because the engine resolves an unproved effect from that
    evidence. A mutation has to reach its postcondition or fail.
    """
    return bool(evidence["postcondition"]) or bool(observed)


def completion(arguments):
    """The evidence one completion publishes, and what it names when unmet.

    A removal publishes the absence form. An observation publishes it exactly
    when everything is gone and the presence form otherwise, so what is still
    there reaches the engine; asking an observation for a removal's form is a
    malformed call.
    """
    request_digest = arguments.get("digest")
    observation = arguments.get("observation") or {}
    if arguments.get("removed"):
        if arguments.get("observed"):
            raise ValueError("an observation proves no removal")
        evidence = absence(observation, request_digest)
        if not evidence["uri"]:
            return evidence, ["networks", "pool"], "the hypervisor did not answer for"
        if unanswered(evidence):
            return evidence, unanswered(evidence), "the libvirt driver that owns it did not answer for"
        return evidence, remaining(observation), "still present"
    if arguments.get("observed"):
        evidence = absence(observation, request_digest)
        if evidence["postcondition"]:
            return evidence, [], "still present"
    evidence = presence(observation, request_digest)
    return evidence, unproved(evidence), "not proved"


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported provider host protocol phase"}
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
            evidence, unmet, verb = completion(arguments)
            if not publishes(evidence, arguments.get("observed")):
                return {
                    "failed": True,
                    "msg": "the provider host did not reach its postcondition; %s: %s" % (verb, ", ".join(unmet) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the provider host capability result could not be published"}
