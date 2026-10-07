"""Emit the machine power protocol on the runner's result channel.

This runs on the controller, never the host that reaches the management
controller, because the inherited result descriptor belongs to the invoking
Bootwright process. A power request is not evidence: the state published here
is the state the controller reported once the operation settled.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

# The refusals a power run names to its runner before it fails, each one the
# runner reports as its caller's own diagnostic (internal/machine/power).
REFUSALS = ("identity-mismatch",)
REPORTED = {"On": "on", "Off": "off", "": ""}
READINGS = {"On": "on", "Off": "off"}
VERBS = {"start": "on", "stop": "off", "restart": "on"}


def state(value):
    """Translate one reported Redfish state, refusing anything else."""
    if value not in REPORTED:
        raise ValueError("power state")
    return REPORTED[value]


def evidence_for(arguments):
    verb = arguments.get("verb")
    if verb not in VERBS:
        raise ValueError("verb")
    machine = str(arguments.get("machine") or "")
    if not machine:
        raise ValueError("machine")
    power, previous = state(arguments.get("power")), state(arguments.get("previous"))
    evidence = {
        "changed": bool(arguments.get("changed")),
        "machine": machine,
        "postcondition": power == VERBS[verb],
        "power": power,
        "previous": previous,
        "request": adapter_protocol.request_digest(arguments.get("digest")),
    }
    return evidence


def observed(result):
    """One controller's answer, or unknown when it did not give one.

    A controller that refused, timed out or answered with something this build
    cannot name leaves its own machine unknown, so one silent controller never
    denies the answers every other controller in the same run already gave.

    The reported state is what decides that. A reading suppresses its own task
    failure to keep polling the rest, and ansible-core rewrites `failed` to
    false when it does, so a refusal arrives here as a result carrying no
    usable state rather than as a flagged failure. The flags are still read,
    because a result that does carry one is not an answer either.
    """
    if not isinstance(result, dict):
        raise ValueError("reading")
    if result.get("failed") or result.get("unreachable"):
        return "unknown"
    return READINGS.get(result.get("power"), "unknown")


def reading_evidence_for(arguments):
    """Turn one loop of controller reads into evidence, in survey order."""
    readings = arguments.get("readings")
    if not isinstance(readings, list) or not readings:
        raise ValueError("readings")
    machines = []
    for result in readings:
        power = observed(result)
        target = result.get("item")
        if not isinstance(target, dict):
            raise ValueError("reading target")
        machine = str(target.get("object") or "")
        if not machine:
            raise ValueError("machine")
        machines.append({"machine": machine, "power": power})
    return {"machines": machines, "request": adapter_protocol.request_digest(arguments.get("digest"))}


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet.

    A reading publishes whatever each controller answered. A power verb
    publishes only the state it asked for, observed or not, because a request
    is not evidence.
    """
    if "readings" in arguments:
        return reading_evidence_for(arguments), None
    evidence = evidence_for(arguments)
    if evidence["postcondition"]:
        return evidence, None
    return evidence, "the machine did not reach the power state this operation asked for"


CAPABILITY = adapter_protocol.Capability(
    "power",
    "the machine power result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
