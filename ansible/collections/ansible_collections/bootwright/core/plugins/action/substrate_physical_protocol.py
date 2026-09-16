"""Emit the physical machine protocol on the runner's result channel.

This runs on the controller, because the inherited result descriptor belongs to
the invoking Bootwright process. What it publishes is a proof rather than a
report: the controller answered as the exact machine the declaration names, and
its complete hardware inventory contained every address that declaration
requires. An inventory that could not be read in full proves nothing and is
never completed into one that was.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

PHASES = ("loaded", "group", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
POWER_STATES = ("", "On", "Off")
MAX_ADDRESSES = 64
HEX = set("0123456789abcdef")


def digest(value):
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def bounded(value, limit=128):
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


def presence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    addresses = observation.get("addresses") or []
    if not isinstance(addresses, list) or len(addresses) > MAX_ADDRESSES:
        raise ValueError("addresses")
    power = observation.get("power")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "addresses": sorted({bounded(address, 32) for address in addresses}),
        "manufacturer": bounded(observation.get("manufacturer")),
        "model": bounded(observation.get("model")),
        "postcondition": False,
        "power": str(power),
        "request": digest(request_digest),
        "serial": bounded(observation.get("serial")),
        "uuid": bounded(observation.get("uuid")),
    }
    expected = {bounded(address, 32) for address in (arguments.get("expected") or [])}
    evidence["postcondition"] = bool(
        expected
        and not (observation.get("failures") or [])
        and expected.issubset(set(evidence["addresses"]))
        and evidence["power"]
        and (evidence["uuid"] or evidence["serial"])
    )
    return evidence


def unproved(evidence, arguments):
    """What a proof still lacks, named so the refusal can say so.

    These are field names and counts, never the values themselves, so naming
    them is safe in a message `no_log` would otherwise censor with the evidence.
    """
    observation = arguments.get("observation") or {}
    names = []
    if observation.get("failures"):
        names.append("a complete hardware inventory")
    expected = {bounded(address, 32) for address in (arguments.get("expected") or [])}
    if not expected:
        names.append("any declared hardware address")
    elif not expected.issubset(set(evidence["addresses"])):
        names.append("%d of %d declared addresses" % (
            len(expected - set(evidence["addresses"])), len(expected)))
    if not evidence["power"]:
        names.append("a power state")
    if not evidence["uuid"] and not evidence["serial"]:
        names.append("a system identity")
    return names


def absence(request_digest):
    """A removal that retains the machine reports nothing about it.

    There is no absence to observe, because this block created nothing on the
    machine. What it proves is that it took only its claim back.
    """
    return {
        "absent": True,
        "addresses": [],
        "manufacturer": "",
        "model": "",
        "postcondition": True,
        "power": "",
        "request": digest(request_digest),
        "serial": "",
        "uuid": "",
    }


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = self._task.args
        phase = arguments.get("phase")
        if phase not in PHASES:
            return {"failed": True, "msg": "unsupported physical machine protocol phase"}
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
            request_digest = arguments.get("digest")
            if arguments.get("released"):
                emit({"phase": "completed", "outcome": outcome, "evidence": absence(request_digest)})
                return {"changed": False}
            evidence = presence(arguments, request_digest)
            if not evidence["postcondition"]:
                return {
                    "failed": True,
                    "msg": "the machine was not proved to be the one this Machine declares; missing: %s"
                           % (", ".join(unproved(evidence, arguments)) or "unknown"),
                }
            emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
            return {"changed": False}
        except (ValueError, TypeError, OSError):
            return {"failed": True, "msg": "the physical machine result could not be published"}
