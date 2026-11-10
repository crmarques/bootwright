"""Emit the provider host capability protocol on the runner's result channel.

This runs on the controller, never the provider host, because the inherited
result and authorization descriptors belong to the invoking Bootwright process.
"""

from __future__ import annotations

from collections import namedtuple

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

MAX_NETWORKS = 64
MAX_SERVICES = 16
# The observation carries each network's UUID so a definition can be offered
# back to libvirt under the identity it already holds. Evidence stays narrower:
# Go validates exactly the facts below, and rejects any field it does not know.
# `mtu` is observed only for a network whose request declares one, and Go reads
# it to prove the bridge carries its frozen MTU, so it is evidence when present.
OBSERVED_NETWORK = {"answered", "autostart", "bridge", "definition", "managed", "name", "owned", "state", "uuid"}
OPTIONAL_NETWORK = {"mtu"}
OBSERVED_SERVICE = {"enabled", "name", "state"}
MAX_CONNECTIONS = 64
OBSERVED_CONNECTION = {"active", "answered", "definition", "device", "name", "owned", "present", "role", "zone"}
CONNECTION_FLAGS = ("active", "answered", "definition", "owned", "present")
OBSERVED_ROUTING = {"answered", "enabled", "forwarding", "loaded", "name", "owned", "present", "state", "table"}
ROUTING_FLAGS = ("answered", "enabled", "forwarding", "loaded", "owned", "present")
OBSERVED_VLAN_REFUSAL = {"answered", "enabled", "name", "owned", "present", "routes", "state"}
VLAN_REFUSAL_FLAGS = ("answered", "enabled", "owned", "present")


digest = adapter_protocol.request_digest


def network_evidence(entry):
    if set(entry) - OPTIONAL_NETWORK != OBSERVED_NETWORK:
        raise ValueError("network evidence")
    evidence = {
        "answered": bool(entry["answered"]),
        "autostart": bool(entry["autostart"]),
        "bridge": bool(entry["bridge"]),
        "definition": bool(entry["definition"]),
        "managed": bool(entry["managed"]),
        "name": str(entry["name"]),
        "owned": bool(entry["owned"]),
        "state": str(entry["state"]),
    }
    if "mtu" in entry:
        evidence["mtu"] = int(entry["mtu"])
    return evidence


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


def firewall_ready(observation):
    entry = observation.get("firewallReady")
    if not isinstance(entry, dict) or set(entry) != {"name", "answered", "present", "owned", "enabled", "ready", "state"}:
        raise ValueError("firewall readiness evidence")
    if not all(isinstance(entry[key], bool) for key in ("answered", "present", "owned", "enabled", "ready")):
        raise ValueError("firewall readiness flags")
    if not isinstance(entry["name"], str) or not isinstance(entry["state"], str):
        raise ValueError("firewall readiness identity")
    return dict(entry)


def firewall_realized(evidence):
    entry = evidence["firewallReady"]
    # A VLAN leg needs the trusted zone as a managed network does, so either freezes the barrier.
    if not any(network["managed"] for network in evidence["networks"]) and not evidence.get("connections"):
        return entry["answered"] and not entry["name"] and not entry["present"] and not entry["enabled"]
    return (entry["answered"] and bool(entry["name"]) and entry["present"] and entry["owned"]
            and entry["enabled"] and entry["ready"] and entry["state"] == "active")


def firewall_gone(evidence):
    entry = evidence["firewallReady"]
    return entry["answered"] and not entry["present"] and not entry["enabled"] and not entry["owned"] and not entry["ready"] and entry["state"] == ""


def flagged(entry, keys, flags, what):
    """One observed record with exactly its keys, its flags Boolean and every other value a string."""
    if not isinstance(entry, dict) or set(entry) != keys:
        raise ValueError(what)
    for key in keys:
        if key in flags and not isinstance(entry[key], bool):
            raise ValueError(what)
        if key not in flags and key != "routes" and not isinstance(entry[key], str):
            raise ValueError(what)
    return dict(entry)


def connections_of(observation):
    """The trunk and VLAN leg evidence, each exactly the fields Go validates, and NetworkManager's readiness."""
    connections = observation.get("connections") or []
    if not isinstance(connections, list) or len(connections) > MAX_CONNECTIONS:
        raise ValueError("connection count")
    ready = observation.get("networkManager")
    if not isinstance(ready, bool):
        raise ValueError("NetworkManager readiness")
    return {"connections": [flagged(entry, OBSERVED_CONNECTION, CONNECTION_FLAGS, "connection evidence") for entry in connections],
            "networkManager": ready}


def connection_realized(entry):
    return entry["answered"] and entry["present"] and entry["owned"] and entry["active"] and entry["definition"] and entry["zone"] == "trusted"


def connections_realized(evidence):
    connections = evidence["connections"]
    return (not connections or evidence["networkManager"]) and all(connection_realized(entry) for entry in connections)


def connections_left(evidence):
    return any(entry["present"] or entry["active"] or entry["owned"] for entry in evidence["connections"])


def routing_of(observation):
    return {"routing": flagged(observation.get("routing"), OBSERVED_ROUTING, ROUTING_FLAGS, "routing evidence")}


def routing_realized(evidence):
    """Whether the frozen table is held by its owned unit, or no table is frozen and none is held."""
    routing = evidence["routing"]
    if not routing["name"]:
        return routing["answered"] and not routing_left(evidence)
    return (routing["answered"] and routing["present"] and routing["owned"] and routing["enabled"]
            and routing["state"] == "active" and routing["loaded"])


def forwarding_proved(observation, evidence):
    """Whether IPv4 forwarding is on wherever the frozen table needs it.

    The observation reads the need from the frozen request; the evidence
    carries only what the host reported, which Go compares with the request.
    """
    needed = observation.get("routingForwarding", False)
    if not isinstance(needed, bool):
        raise ValueError("routing forwarding")
    return not needed or evidence["routing"]["forwarding"]


def routing_left(evidence):
    """Whether anything of the routing unit or its table remains; nft names the table only while it lists one."""
    routing = evidence["routing"]
    return bool(routing["present"] or routing["enabled"] or routing["owned"] or routing["loaded"] or routing["state"]
                or routing["table"])


def vlan_refusal_of(observation):
    entry = flagged(observation.get("vlanRefusal"), OBSERVED_VLAN_REFUSAL, VLAN_REFUSAL_FLAGS, "VLAN refusal evidence")
    if not isinstance(entry["routes"], list) or not all(isinstance(route, str) for route in entry["routes"]):
        raise ValueError("VLAN refusal evidence")
    entry["routes"] = list(entry["routes"])
    return {"vlanRefusal": entry}


def vlan_refusal_realized(evidence):
    """Whether the VLAN refusal unit is answered for and, while present, owned, enabled and active.

    Go compares its routes with the frozen subnets.
    """
    vlan = evidence["vlanRefusal"]
    return vlan["answered"] and (not vlan["present"] or (vlan["owned"] and vlan["enabled"] and vlan["state"] == "active"))


def vlan_refusal_left(evidence):
    vlan = evidence["vlanRefusal"]
    return bool(vlan["present"] or vlan["enabled"] or vlan["owned"] or vlan["routes"] or vlan["state"])


# Each resource the host block owns beside the libvirt ones, as a refusal names
# it: how its evidence is read from the observation, whether an apply left it
# realized, whether anything of it is left, and whether it was answered for.
Held = namedtuple("Held", ("name", "read", "realized", "left", "answered"))
HELD = (
    Held("connections", connections_of, connections_realized, connections_left,
         lambda evidence: all(entry["answered"] for entry in evidence["connections"])),
    Held("routing table", routing_of, routing_realized, routing_left, lambda evidence: evidence["routing"]["answered"]),
    Held("VLAN refusal", vlan_refusal_of, vlan_refusal_realized, vlan_refusal_left,
         lambda evidence: evidence["vlanRefusal"]["answered"]),
)


def held_evidence(observation):
    evidence = {}
    for held in HELD:
        evidence.update(held.read(observation))
    return evidence


def held_realized(evidence):
    return all(held.realized(evidence) for held in HELD)


def held_gone(evidence):
    return all(held.answered(evidence) and not held.left(evidence) for held in HELD)


def held_unanswered(evidence):
    return [held.name for held in HELD if not held.answered(evidence)]


def held_unproved(evidence):
    return [held.name for held in HELD if not held.realized(evidence)]


def presence(observation, request_digest):
    evidence = {
        "absent": False,
        "directory": directory(observation),
        "firewallReady": firewall_ready(observation),
        "hypervisor": bool(observation.get("hypervisor")),
        "networks": networks_of(observation),
        "pool": str(observation.get("pool", "")),
        "poolAnswered": bool(observation.get("poolAnswered")),
        "poolAutostart": bool(observation.get("poolAutostart")),
        "poolOwned": bool(observation.get("poolOwned")),
        "postcondition": False,
        "request": adapter_protocol.request_digest(request_digest),
        "services": services_of(observation),
        "uri": bool(observation.get("uri")),
    }
    evidence.update(held_evidence(observation))
    complete = (evidence["hypervisor"] and evidence["uri"] and pool_realized(evidence) and firewall_realized(evidence)
                and held_realized(evidence) and forwarding_proved(observation, evidence))
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
    if not firewall_gone({"firewallReady": firewall_ready(observation)}):
        names.append("firewall readiness")
    evidence = held_evidence(observation)
    return names + [held.name for held in HELD if held.left(evidence)]


def unproved(evidence, observation=None):
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
    if not firewall_realized(evidence):
        names.append("firewall readiness")
    if not pool_realized(evidence):
        names.append("pool")
    for entry in evidence["networks"]:
        if not entry["bridge"] or (entry["managed"] and not realized(entry)):
            names.append("networks")
            break
    names += held_unproved(evidence)
    if "routing table" not in names and not forwarding_proved(observation or {}, evidence):
        names.append("routing table")
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
    return names + held_unanswered(evidence)


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
        and firewall_gone(evidence)
        and held_gone(evidence)
    )


def absence(observation, request_digest):
    """Removal evidence carrying what was observed, proved only by gone()."""
    evidence = {
        "absent": True,
        "directory": directory(observation),
        "firewallReady": firewall_ready(observation),
        "hypervisor": bool(observation.get("hypervisor")),
        "networks": networks_of(observation),
        "pool": str(observation.get("pool", "")),
        "poolAnswered": bool(observation.get("poolAnswered")),
        "poolAutostart": bool(observation.get("poolAutostart")),
        "poolOwned": bool(observation.get("poolOwned")),
        "postcondition": False,
        "request": adapter_protocol.request_digest(request_digest),
        "services": services_of(observation),
        "uri": bool(observation.get("uri")),
    }
    evidence.update(held_evidence(observation))
    evidence["postcondition"] = gone(evidence)
    return evidence


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
    return evidence, unproved(evidence, observation), "not proved"


def concluded(arguments):
    """The evidence one completion publishes, or the refusal naming what is unmet."""
    evidence, unmet, verb = completion(arguments)
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, unreached("the provider host", verb, unmet)


def shadowed(arguments):
    """Each broader host route a managed network takes over, as a warning.

    Only an apply reports them: the apply admits such a route and says so, and
    a removal or observation has nothing to warn of.
    """
    if arguments.get("removed") or arguments.get("observed"):
        return []
    shadows = arguments.get("shadows")
    if not isinstance(shadows, list):
        return []
    return [{"code": "substrate.shadowed-route", "message": shadow} for shadow in shadows if isinstance(shadow, str)]


CAPABILITY = adapter_protocol.Capability(
    "provider host",
    "the provider host capability result could not be published",
    concluded,
    warnings=shadowed,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
