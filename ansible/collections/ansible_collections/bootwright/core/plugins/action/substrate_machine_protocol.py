"""Emit the machine capability protocol on the runner's result channel.

This runs on the controller, never the provider host, because the inherited
result and authorization descriptors belong to the invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

POWER_STATES = ("", "On", "Off")
# The domain's own state, in libvirt's vocabulary. It is what a removal reads
# to prove the machine is not in use; the controller's power state is the
# second opinion for a host whose hypervisor will not answer, which `answered`
# reports, because an empty domain from a silent hypervisor proves nothing.
DOMAIN_STATES = ("", "running", "idle", "paused", "in shutdown", "shut off", "crashed", "pmsuspended")
MAX_DISKS = 32


def state(value):
    """Accept one domain state, refusing a word libvirt does not use."""
    value = str(value or "")
    if value not in DOMAIN_STATES:
        raise ValueError("domain state")
    return value


def disk_evidence(entry):
    if set(entry) != {"name", "present", "sizeGiB"}:
        raise ValueError("disk evidence")
    if not isinstance(entry["sizeGiB"], int) or isinstance(entry["sizeGiB"], bool):
        raise ValueError("disk size")
    return {"name": str(entry["name"]), "present": bool(entry["present"]), "sizeGiB": int(entry["sizeGiB"])}


def listener(observation):
    """Whether anything listens on the controller's socket, as observed.

    Evidence without it proves the socket neither held nor free, so an
    observation that does not report it is never published.
    """
    value = observation.get("listener")
    if not isinstance(value, bool):
        raise ValueError("listener")
    return value


def disks_of(observation):
    disks = observation.get("disks") or []
    if len(disks) > MAX_DISKS:
        raise ValueError("disk count")
    return [disk_evidence(entry) for entry in disks]


def presence(observation, power, system, request_digest):
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "answered": bool(observation.get("answered")),
        "controller": str(observation.get("controller", "")),
        "disks": disks_of(observation),
        "domain": str(observation.get("domain", "")),
        "listener": listener(observation),
        "owned": bool(observation.get("owned")),
        "postcondition": False,
        "power": str(power),
        "request": adapter_protocol.request_digest(request_digest),
        "state": state(observation.get("state")),
        "system": str(system or ""),
        "unit": str(observation.get("unit", "")),
    }
    complete = (
        bool(evidence["domain"]) and evidence["owned"] and evidence["unit"] == "active"
        and bool(evidence["controller"]) and bool(evidence["system"]) and bool(evidence["power"])
        and all(entry["present"] for entry in evidence["disks"]) and bool(evidence["disks"])
    )
    evidence["postcondition"] = complete
    return evidence


def remaining(observation):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    names = []
    for name in ("domain", "unit", "controller", "listener"):
        if observation.get(name):
            names.append(name)
    if any(entry.get("present") for entry in observation.get("disks") or []):
        names.append("disks")
    return names


def unproved(evidence):
    """What a presence proof still lacks, named for the same reason."""
    names = []
    if not evidence["domain"]:
        names.append("domain")
    if not evidence["owned"]:
        names.append("ownership")
    if evidence["unit"] != "active":
        names.append("unit")
    for name in ("controller", "system", "power"):
        if not evidence[name]:
            names.append(name)
    if not evidence["disks"] or not all(entry["present"] for entry in evidence["disks"]):
        names.append("disks")
    return names


def gone(evidence):
    """Whether the evidence proves every part of the machine gone.

    A silent hypervisor reports no domain too, so its absence is never proved
    by an empty field alone, and a socket something still listens on is not
    free to release.
    """
    return (
        evidence["answered"] is True
        and not any(evidence[name] for name in ("domain", "unit", "controller", "state", "power", "system"))
        and not any(entry["present"] for entry in evidence["disks"])
        and evidence["listener"] is False
    )


def absence(observation, power, system, request_digest):
    """Removal evidence carrying what was observed, proved only by gone()."""
    power, system = str(power or ""), str(system or "")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": True,
        "answered": bool(observation.get("answered")),
        "controller": str(observation.get("controller", "")),
        "disks": disks_of(observation),
        "domain": str(observation.get("domain", "")),
        "listener": listener(observation),
        "owned": bool(observation.get("owned")),
        "postcondition": False,
        "power": power,
        "request": adapter_protocol.request_digest(request_digest),
        "state": state(observation.get("state")),
        "system": system,
        "unit": str(observation.get("unit", "")),
    }
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
    power, system = arguments.get("power"), arguments.get("system")
    if arguments.get("removed"):
        if arguments.get("observed"):
            raise ValueError("an observation proves no removal")
        evidence = absence(observation, power, system, request_digest)
        if not evidence["answered"]:
            return evidence, ["domain"], "the hypervisor did not answer for"
        return evidence, remaining(observation), "still present"
    if arguments.get("observed"):
        evidence = absence(observation, power, system, request_digest)
        if evidence["postcondition"]:
            return evidence, [], "still present"
    evidence = presence(observation, power, system, request_digest)
    return evidence, unproved(evidence), "not proved"


def concluded(arguments):
    """The evidence one completion publishes, or the refusal naming what is unmet."""
    evidence, unmet, verb = completion(arguments)
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, unreached("the machine", verb, unmet)


CAPABILITY = adapter_protocol.Capability(
    "machine",
    "the machine capability result could not be published",
    concluded,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
