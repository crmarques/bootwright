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
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

POWER_STATES = ("", "On", "Off")
MAX_ADDRESSES = 64
IDENTITY_LIMIT = 128


def bounded(value, limit=128):
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


class UnprovableIdentity(ValueError):
    """A reported UUID or serial the evidence may not carry, named by its field
    and its controller and never by its value."""


def identity(value, field, endpoint):
    """A reported UUID or serial as a proof records it and a pin later carries it.

    It is what a management controller reported, trimmed of surrounding space,
    and every later comparison and refusal repeats it, so a value longer than
    the bound or holding any character that is not printable is refused.
    """
    value = str(value or "").strip()
    if len(value) > IDENTITY_LIMIT:
        raise UnprovableIdentity("the management controller at %s reported a %s longer than %d characters"
                                 % (endpoint, field, IDENTITY_LIMIT))
    if not value.isprintable():
        raise UnprovableIdentity("the management controller at %s reported a %s holding a character that is not "
                                 "printable" % (endpoint, field))
    return value


def presence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    addresses = observation.get("addresses") or []
    if not isinstance(addresses, list) or len(addresses) > MAX_ADDRESSES:
        raise ValueError("addresses")
    power = observation.get("power")
    if power not in POWER_STATES:
        raise ValueError("power state")
    # A refusal of the reported identity names this controller, so a
    # publication that lost it publishes nothing rather than name none.
    endpoint = str(arguments.get("endpoint") or "")
    if not endpoint.strip():
        raise ValueError("endpoint")
    uuid = identity(observation.get("uuid"), "UUID", endpoint)
    serial = identity(observation.get("serial"), "SerialNumber", endpoint)
    evidence = {
        "absent": False,
        "addresses": sorted({bounded(address, 32) for address in addresses}),
        "manufacturer": bounded(observation.get("manufacturer")),
        "model": bounded(observation.get("model")),
        "postcondition": False,
        "power": str(power),
        "request": adapter_protocol.request_digest(request_digest),
        "serial": serial,
        "uuid": uuid,
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
    them is safe in the message the attempt's retained output carries.
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
        "request": adapter_protocol.request_digest(request_digest),
        "serial": "",
        "uuid": "",
    }


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet.

    A release publishes its claim taken back. A proof publishes only when it
    holds: an observation is no exception, because a machine not proved to be
    the declared one is never reported as it.
    """
    request_digest = arguments.get("digest")
    if arguments.get("released"):
        return absence(request_digest), None
    evidence = presence(arguments, request_digest)
    if evidence["postcondition"]:
        return evidence, None
    return evidence, ("the machine was not proved to be the one this Machine declares; missing: %s"
                      % (", ".join(unproved(evidence, arguments)) or "unknown"))


CAPABILITY = adapter_protocol.Capability(
    "physical machine",
    "the physical machine result could not be published",
    completion,
    passthrough=(UnprovableIdentity,),
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
