"""Pure ownership, comparison and readiness rules for the shared object port."""
from __future__ import annotations

import hashlib
import ipaddress
import re
from datetime import datetime
from urllib.parse import urlsplit

VERSION = "cluster-objects-v2"
OWNER = "bootwright.io/owner"
DOMAIN = "bootwright.cluster-objects.owner-v1"
REF_KEYS = ("apiVersion", "kind", "namespace", "name", "namespaced")


def ref(value):
    return {key: value.get(key, False if key == "namespaced" else "") for key in REF_KEYS}


def identity(value):
    return tuple(value.get(key, "") for key in REF_KEYS[:-1])


def label(value):
    return "/".join(str(value.get(key, "")) for key in ("kind", "namespace", "name"))


def mark(operation, block, value=None):
    parts = [DOMAIN, operation, block]
    if value is not None:
        version = value["apiVersion"]
        group = version.split("/")[0] if "/" in version else ""
        parts += [group, value["kind"], value.get("namespace", ""), value["name"]]
    return hashlib.sha256("\0".join(parts).encode()).hexdigest()


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamp has no timezone")
    return parsed.timestamp()


def ownership(obj, live, context):
    if live is None:
        return "absent"
    if not isinstance(live, dict) or not live.get("metadata", {}).get("uid"):
        return "unknown"
    metadata = live["metadata"]
    receipts = context.get("receipts", [])
    receipt = next((r for r in receipts if identity(r) == identity(obj)), None)
    if receipt is not None:
        return "own" if metadata["uid"] == receipt.get("uid") else "foreign"
    if (metadata.get("annotations") or {}).get(OWNER) != mark(context["operation"], context["block"], obj):
        return "foreign"
    try:
        recent = timestamp(metadata["creationTimestamp"]) >= timestamp(context["registered"]) - 5
    except (KeyError, TypeError, ValueError):
        return "unknown"
    return "own" if recent else "foreign"


def basis(obj, live, context):
    """How ownership of a present object this block owns was established.

    A receipt whose uid the object carries proves it outright; otherwise the
    owner mark recovered it, after a receipt lost with an interrupted attempt.
    """
    receipt = next((r for r in context.get("receipts", []) if identity(r) == identity(obj)), None)
    if receipt is not None and receipt.get("uid") == (live or {}).get("metadata", {}).get("uid"):
        return "uid-matched"
    return "mark-recovered"


def difference(wanted, live, path="", strict=False):
    if isinstance(wanted, dict):
        if not isinstance(live, dict):
            return path
        if strict and set(wanted) != set(live):
            return path
        for key in sorted(wanted):
            child = (path + "." + key).lstrip(".")
            if key not in live:
                return child
            found = difference(wanted[key], live[key], child, strict)
            if found:
                return found
        return ""
    if isinstance(wanted, list):
        if not isinstance(live, list) or len(wanted) != len(live):
            return path
        for index, item in enumerate(wanted):
            found = difference(item, live[index], "%s[%d]" % (path, index), strict)
            if found:
                return found
        return ""
    if isinstance(wanted, bool):
        same_type = isinstance(live, bool)
    elif isinstance(wanted, str):
        same_type = isinstance(live, str)
    elif isinstance(wanted, (int, float)):
        same_type = isinstance(live, (int, float)) and not isinstance(live, bool)
    else:
        same_type = wanted is None and live is None
    return "" if same_type and wanted == live else path


def compare(obj, live):
    wanted = obj["body"]
    for key in sorted(wanted):
        strict = ((obj["kind"] == "Subscription" and key == "spec")
                  or (obj["kind"] in ("Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding")
                      and key in ("subjects", "rules")))
        if key not in live:
            return key
        found = difference(wanted[key], live[key], key, strict)
        if found:
            return found
    return ""


def native(value, grammar="text"):
    if not isinstance(value, str):
        return "invalid"
    value = "".join(c for c in value if c.isprintable())
    patterns = {"name": r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?",
                "version": r"[0-9]+\.[0-9]+",
                "image": r"[a-zA-Z0-9][a-zA-Z0-9./:_@+-]*"}
    valid = grammar not in patterns or re.fullmatch(patterns[grammar], value)
    if grammar == "name":
        valid = len(value) <= 253 and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", part) for part in value.split("."))
    elif grammar == "image":
        valid = image_reference(value)
    if grammar in patterns and (len(value.encode()) > 256 or not valid):
        return "invalid"
    return value.encode()[:256].decode("utf-8", "ignore")


def image_reference(value):
    if "@" in value:
        base, digest = value.split("@", 1)
        if not re.fullmatch(r"(?i)sha256:[a-f0-9]{64}", digest):
            return False
    else:
        if value.rfind(":") <= value.rfind("/"):
            return False
        base, tag = value.rsplit(":", 1)
        if tag == "latest" or not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", tag):
            return False
    if any(c in base for c in "?#@\\ \t\r\n") or "://" in base or base.startswith("/") or base.endswith("/"):
        return False
    try:
        parsed = urlsplit("https://" + base)
        if parsed.username or not parsed.hostname or (parsed.port is not None and not 0 < parsed.port <= 65535):
            return False
        try:
            ipaddress.ip_address(parsed.hostname)
        except ValueError:
            if not all(re.fullmatch(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?", part) for part in parsed.hostname.split(".")):
                return False
        return all(re.fullmatch(r"[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*", part)
                   for part in parsed.path.strip("/").split("/")) if parsed.path else True
    except ValueError:
        return False


def path_value(value, path):
    for key in path.split("."):
        if not isinstance(value, dict) or key not in value:
            return None
        value = value[key]
    return value


def condition(check, live):
    conditions = live.get("status", {}).get("conditions") or []
    generation = live.get("metadata", {}).get("generation")
    for failure in check.get("failOn", []):
        for item in conditions:
            if (item.get("type") == failure["type"] and item.get("status") == failure["status"]
                    and (not failure.get("reason") or item.get("reason") == failure["reason"])):
                return False, False, "failure condition " + native(item.get("reason", "")), True
    item = next((c for c in conditions if c.get("type") == check["type"]), {})
    observed = item.get("observedGeneration", live.get("status", {}).get("observedGeneration"))
    current = observed is None or observed == generation
    changes = [c.get("lastTransitionTime", "") for c in conditions]
    stale = bool(item.get("lastTransitionTime") and changes and item["lastTransitionTime"] < max(changes))
    ready = item.get("status") == check["status"] and current
    return ready, stale, native(item.get("reason", item.get("status", "absent"))), False
