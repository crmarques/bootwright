"""The adapter result protocol every capability's action plugin publishes.

One dispatch hands the runner a capability's records: `loaded`, which waits for
the runner's acknowledgement, `group` progress, a named `refused` reason and
the terminal `completed` record. Each capability supplies only what is its own:
its name, its phases, the refusals it may name and the completion that builds
its evidence. Whether that evidence proves a postcondition is the capability's
rule and the engine's decision, never this module's.

The channel is a parameter, so each plugin hands its own module-level emit and
a test that replaces it records what the plugin would have written. This runs
in an action plugin on the controller, but it lives with the module utilities
and imports nothing from ansible.plugins, so it keeps the managed-host floor.
"""

from __future__ import annotations

PHASES = ("loaded", "group", "completed")
REFUSING_PHASES = ("loaded", "group", "refused", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
HEX = frozenset("0123456789abcdef")


def request_digest(value):
    """A frozen request digest: exactly 64 lowercase hexadecimal characters."""
    if not isinstance(value, str) or len(value) != 64 or set(value) - HEX:
        raise ValueError("digest")
    return value


def publishes(evidence, observed):
    """Whether this phase may publish evidence proving no postcondition.

    A read-only observation reports what it found, including a target that is
    part way realized, because the engine resolves an unproved effect from that
    evidence. A mutation has to reach its postcondition or fail.
    """
    return bool(evidence["postcondition"]) or bool(observed)


def unreached(subject, verb, names):
    """The refusal of a completion that proves no postcondition.

    The names are field names, never values, so the message is safe where
    `no_log` would otherwise censor it along with the evidence.
    """
    return "%s did not reach its postcondition; %s: %s" % (subject, verb, ", ".join(names) or "unknown")


class Capability:
    """What one capability's plugin hands the shared dispatch.

    `completion` takes the task's arguments and returns the evidence and either
    None, when that evidence may be published, or the message the run fails
    with. `refusals` is the tuple of reasons a refused phase may name, or a
    callable that returns the reason from the arguments and raises ValueError
    for one it refuses. An exception of a `passthrough` type fails the run with
    its own message, which names a cause the generic one would hide.
    """

    def __init__(self, name, unpublished, completion, phases=PHASES, refusals=(), passthrough=()):
        self.name = name
        self.unpublished = unpublished
        self.completion = completion
        self.phases = phases
        self.refusals = refusals
        self.passthrough = passthrough


def refusal(arguments, refusals):
    if callable(refusals):
        return refusals(arguments)
    reason = arguments.get("reason")
    if reason not in refusals:
        raise ValueError("refusal reason")
    return reason


def handed(arguments, capability, emit):
    """Hand the runner the one record this phase names, or fail with a message."""
    phase = arguments.get("phase")
    if phase == "loaded":
        emit({"phase": "loaded"}, acknowledge=True)
        return {"changed": False}
    if phase == "group":
        status = arguments.get("status")
        if status not in GROUP_STATUSES:
            raise ValueError("group status")
        emit({"phase": "group", "group": str(arguments.get("group")), "status": status})
        return {"changed": False}
    if phase == "refused":
        emit({"phase": "refused", "reason": refusal(arguments, capability.refusals)})
        return {"changed": False}
    outcome = arguments.get("outcome")
    if outcome not in OUTCOMES:
        raise ValueError("outcome")
    evidence, failure = capability.completion(arguments)
    if failure is not None:
        return {"failed": True, "msg": failure}
    emit({"phase": "completed", "outcome": outcome, "evidence": evidence})
    return {"changed": False}


def publish(arguments, capability, emit):
    """The one dispatch of a capability's protocol phase.

    A phase the capability does not name fails before anything is handed, and
    every refusal of the arguments, the evidence or the channel fails the run
    with the capability's own message rather than raising.
    """
    if arguments.get("phase") not in capability.phases:
        return {"failed": True, "msg": "unsupported %s protocol phase" % capability.name}
    try:
        return handed(arguments, capability, emit)
    except capability.passthrough as refused:
        return {"failed": True, "msg": str(refused)}
    except (ValueError, TypeError, OSError):
        return {"failed": True, "msg": capability.unpublished}
