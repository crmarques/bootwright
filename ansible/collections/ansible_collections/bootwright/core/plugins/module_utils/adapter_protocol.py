"""The adapter result protocol every capability's action plugin publishes.

One dispatch hands the runner a capability's records: `loaded`, which waits for
the runner's acknowledgement, `group` progress, a `prepared` before-state that
also waits for it, a named `refused` reason and the terminal `completed` record. Each capability supplies only what is its own:
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
# A capability that publishes a before-state ahead of an effect.
PREPARING_PHASES = ("loaded", "group", "prepared", "refused", "completed")
GROUP_STATUSES = ("running", "ok", "failed", "skipped")
OUTCOMES = ("changed", "unchanged")
HEX = frozenset("0123456789abcdef")
# A refusal detail names a closed cause or a code site, never a value.
DETAIL = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.:@-")
MAX_DETAIL = 160
# A completed record's warnings, bounded as the Go decoder admits them
# (internal/adapterprotocol): registered codes only, at most MAX_WARNINGS,
# each message printable ASCII of at most MAX_WARNING_MESSAGE bytes and an
# optional object of a kind and a name. The dispatch drops or clips anything
# else rather than failing the completion a warning only advises on.
WARNING_CODES = ("substrate.shadowed-route",)
MAX_WARNINGS = 16
MAX_WARNING_MESSAGE = 1024
MAX_WARNING_KIND = 63
MAX_WARNING_NAME = 253
NAME = frozenset("abcdefghijklmnopqrstuvwxyz0123456789-.")
KIND = frozenset("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")
TRUNCATED = "..."
MAX_LINE = 65536


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
    `preparation` takes the arguments and returns the before-state a prepared
    phase publishes, or raises ValueError for one it refuses. `warnings` takes
    a completion's arguments and returns the advisories its record carries,
    each a mapping of `code`, `message` and optionally `object`.
    """

    def __init__(self, name, unpublished, completion, phases=PHASES, refusals=(), passthrough=(), detail=False, native_error=False,
                 preparation=None, warnings=None):
        self.name = name
        self.unpublished = unpublished
        self.completion = completion
        self.phases = phases
        self.refusals = refusals
        self.passthrough = passthrough
        self.detail = detail
        self.native_error = native_error
        self.preparation = preparation
        self.warnings = warnings


def refusal(arguments, refusals):
    if callable(refusals):
        return refusals(arguments)
    reason = arguments.get("reason")
    if reason not in refusals:
        raise ValueError("refusal reason")
    return reason


def clipped(message):
    """A warning message as the record carries it, or None: printable ASCII, bounded, a cut marked."""
    if not isinstance(message, str):
        return None
    text = "".join(character if " " <= character <= "~" else "?" for character in message).strip()
    if len(text) > MAX_WARNING_MESSAGE:
        text = text[:MAX_WARNING_MESSAGE - len(TRUNCATED)] + TRUNCATED
    return text or None


def kind_name(value):
    return (isinstance(value, str) and 0 < len(value) <= MAX_WARNING_KIND and "A" <= value[0] <= "Z"
            and not set(value) - KIND)


def object_name(value):
    return (isinstance(value, str) and 0 < len(value) <= MAX_WARNING_NAME and value[0] not in "-." and value[-1] not in "-."
            and not set(value) - NAME)


def warning(entry):
    """One warning in the record's shape, or None for one the Go decoder would refuse."""
    if not isinstance(entry, dict) or set(entry) - {"code", "message", "object"} or entry.get("code") not in WARNING_CODES:
        return None
    message = clipped(entry.get("message"))
    if message is None:
        return None
    record = {"code": entry["code"], "message": message}
    if "object" in entry:
        named = entry["object"]
        if not isinstance(named, dict) or set(named) != {"kind", "name"} or not kind_name(named["kind"]) or not object_name(named["name"]):
            return None
        record["object"] = {"kind": named["kind"], "name": named["name"]}
    return record


def counted(kept, dropped):
    """Mark the last kept warning with how many were not reported."""
    if dropped and kept:
        suffix = " (%d more not reported)" % dropped
        last = kept[-1]
        last["message"] = last["message"][:MAX_WARNING_MESSAGE - len(suffix)] + suffix
    return kept


def warnings_of(arguments, capability):
    """The warnings a completion reports: valid ones only, at most MAX_WARNINGS.

    A warning only advises, so it never fails the completion it rides on: a
    callable that raises or answers anything but a list yields none, and an
    entry the Go decoder would refuse is dropped and counted.
    """
    if capability.warnings is None:
        return []
    try:
        entries = capability.warnings(arguments)
    except Exception:  # pylint: disable=broad-exception-caught
        return []
    if not isinstance(entries, list):
        return []
    valid = [record for record in (warning(entry) for entry in entries) if record is not None]
    return counted(valid[:MAX_WARNINGS], len(entries) - min(len(valid), MAX_WARNINGS))


def completed_record(outcome, evidence, reported):
    """The completed record, carrying warnings only while its line stays within the channel's bound."""
    record = {"phase": "completed", "outcome": outcome, "evidence": evidence}
    if reported:
        from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
        warned = dict(record, warnings=reported)
        if len(canonical(warned)) + 1 <= MAX_LINE:
            return warned
    return record


def handed(arguments, capability, emit):
    """Hand the runner the one record this phase names, or fail with a message."""
    phase = arguments.get("phase")
    if phase == "loaded":
        emit({"phase": "loaded"}, acknowledge=True)
        return {"changed": False}
    if phase == "prepared":
        # The runner acknowledges only once it recorded the before-state, so
        # the effect this phase precedes runs only after that.
        if capability.preparation is None:
            raise ValueError("preparation")
        emit({"phase": "prepared", "preparation": capability.preparation(arguments)}, acknowledge=True)
        return {"changed": False}
    if phase == "group":
        status = arguments.get("status")
        if status not in GROUP_STATUSES:
            raise ValueError("group status")
        emit({"phase": "group", "group": str(arguments.get("group")), "status": status})
        return {"changed": False}
    if phase == "refused":
        record = {"phase": "refused", "reason": refusal(arguments, capability.refusals)}
        detail = arguments.get("detail")
        if capability.detail and isinstance(detail, str) and 0 < len(detail) <= MAX_DETAIL and not set(detail) - DETAIL:
            record["detail"] = detail
        if "native_error" in arguments:
            from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
            value = arguments["native_error"]
            if not capability.native_error or not native_diagnostics.valid(value):
                raise ValueError("native diagnostic")
            record["nativeError"] = value
        emit(record)
        return {"changed": False}
    outcome = arguments.get("outcome")
    if outcome not in OUTCOMES:
        raise ValueError("outcome")
    evidence, failure = capability.completion(arguments)
    if failure is not None:
        return {"failed": True, "msg": failure}
    emit(completed_record(outcome, evidence, warnings_of(arguments, capability)))
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
