"""The one verdict of whether a management controller answers as the machine a Machine declares.

Machine power and the bare-metal target proof refuse a wrong target before a
power request or an erase from this verdict, and the physical machine
protocol publishes its proof from the same declared-set rule, so a
normalization fix lands once for all three. Every pin and identity the
callers carry arrives base64-encoded and is decoded here, where a decoded
value is data and never a template.
"""

from __future__ import annotations

import base64
import binascii

MAX_ADDRESSES = 64
ADDRESS_LIMIT = 32
IDENTITY_LIMIT = 128


def bounded(value, limit=128):
    """A reported text trimmed of surrounding space, refused past its bound."""
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


def decoded(value):
    """A base64 value as text, empty for none, or None for one that does not decode."""
    if not value:
        return ""
    try:
        return base64.b64decode(str(value), validate=True).decode("utf-8")
    except (binascii.Error, ValueError):
        return None


def listed(value):
    """A decoded comma-separated list without its empty members, or None for one that does not decode."""
    text = decoded(value)
    return None if text is None else [item for item in text.split(",") if item]


def addresses(values):
    """Reported or declared hardware addresses as one set, or None when any could not be proved one.

    Each is a string of at most ADDRESS_LIMIT characters with no surrounding
    space, and there are at most MAX_ADDRESSES of them."""
    if not isinstance(values, (list, tuple)) or len(values) > MAX_ADDRESSES:
        return None
    if any(not isinstance(value, str) or value != value.strip() or not value or len(value) > ADDRESS_LIMIT for value in values):
        return None
    return frozenset(values)


def identity(value):
    """A reported UUID or serial the evidence may carry, trimmed, or None for one it may not."""
    value = str(value or "").strip()
    return value if len(value) <= IDENTITY_LIMIT and value.isprintable() else None


def unproved(observation, expected):
    """Which declared-set rule refuses: "addresses", "identity" or "" for none.

    "addresses" the inventory names a failure or its reported addresses do not
    equal the declared ones both ways, or none is declared; "identity" the
    addresses hold but the system reports no UUID or serial the evidence may
    carry."""
    observation = observation if isinstance(observation, dict) else {}
    failures = observation.get("failures")
    reported, wanted = addresses(observation.get("addresses")), addresses(list(expected or []))
    if not (isinstance(failures, list) and not failures and wanted and reported == wanted):
        return "addresses"
    uuid, serial = identity(observation.get("uuid")), identity(observation.get("serial"))
    return "" if uuid is not None and serial is not None and (uuid or serial) else "identity"


def declared(observation, expected):
    """Whether a complete inventory reports exactly the declared address set and names the system."""
    return unproved(observation, expected) == ""


def pinned(observation, uuid_pin, serial_pin):
    """Whether the reported UUID, in any letter case or surrounding space, and the trimmed serial are the pinned ones.

    An empty pin compares nothing; a pin that does not decode never matches."""
    observation = observation if isinstance(observation, dict) else {}
    uuid, serial = decoded(uuid_pin), decoded(serial_pin)
    if uuid is None or serial is None:
        return False
    return ((not uuid or str(observation.get("uuid") or "").strip().lower() == uuid.strip().lower())
            and (not serial or str(observation.get("serial") or "").strip() == serial.strip()))


def controller_host(observation, host_uuid, host_macs):
    """Whether the machine reports the controller host's own UUID, in any letter case or space, or any of its MACs.

    A host identity that does not decode is read as matching, so the run refuses."""
    observation = observation if isinstance(observation, dict) else {}
    uuid, macs = decoded(host_uuid), listed(host_macs)
    if uuid is None or macs is None:
        return True
    if uuid and str(observation.get("uuid") or "").strip().lower() == uuid.strip().lower():
        return True
    reported = observation.get("addresses") or []
    return isinstance(reported, list) and bool(set(str(value) for value in reported) & set(macs))


def verdict(observation, expected=None, expected_base64="", pinned_uuid_base64="", pinned_serial_base64="",
            host_uuid_base64="", host_macs_base64=""):
    """Every identity rule over one observation: pinned, controllerHost, declared and the rule that refused it, unproved."""
    wanted = list(expected or [])
    if expected_base64:
        more = listed(expected_base64)
        wanted = None if more is None else wanted + more
    cause = "addresses" if wanted is None else unproved(observation, wanted)
    return {
        "pinned": pinned(observation, pinned_uuid_base64, pinned_serial_base64),
        "controllerHost": controller_host(observation, host_uuid_base64, host_macs_base64),
        "declared": cause == "",
        "unproved": cause,
    }
