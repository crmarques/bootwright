"""What a management controller offers, read from the controller itself.

Firmware differs widely in where virtual media lives, which action attaches it
and which reset types exist, so nothing here names a vendor. Every answer is
derived from the controller's own metadata: an action is used because the
resource advertises it and, for a vendor extension, because its
`@Redfish.ActionInfo` declares parameters this client can satisfy. A controller
that implements the specification is therefore driven by the specification, and
one that only offers an extension is driven by what it proved it accepts.
"""

from __future__ import annotations

import re
from urllib.parse import urlsplit, urlunsplit

MAC = re.compile(r"^[0-9a-f]{2}(:[0-9a-f]{2}){5}$")
MEDIA_NAMES = re.compile(r"(?i)/(cd|cd1|dvd|dvd1|virtualcd|virtualdvd|[0-9]+)$")
# Reset types that turn a machine on, in the order they are preferred. ForceOn
# is first because a controller that offers it applies power without waiting on
# an operating system that is not running yet.
POWER_ON_ORDER = ("ForceOn", "On", "PushPowerButton")
TERMINAL_TASK_STATES = ("Completed", "Exception", "Killed", "Cancelled", "Interrupted")


def resolve(base, reference):
    """Turn a member reference into an absolute URL against the controller."""
    reference = reference if isinstance(reference, str) else ""
    reference = reference.strip()
    if not reference:
        return ""
    parsed = urlsplit(reference)
    if parsed.scheme in ("http", "https") and parsed.netloc:
        return reference
    root = urlsplit(base)
    if reference.startswith("/"):
        return urlunsplit((root.scheme, root.netloc, reference, "", ""))
    return urlunsplit((root.scheme, root.netloc, root.path.rstrip("/") + "/" + reference, "", ""))


def service_root(endpoint):
    """The controller's own root, which every discovered path hangs from."""
    parsed = urlsplit(endpoint)
    return urlunsplit((parsed.scheme, parsed.netloc, "", "", ""))


def actions(resource, name):
    """Every advertised occurrence of one action, standard and vendor alike.

    A vendor puts its own actions under `Oem.<vendor>.Actions`, so collecting
    both locations finds an extension without this code knowing the vendor that
    published it.
    """
    found = []
    if not isinstance(resource, dict) or not name:
        return found
    standard = resource.get("Actions")
    if isinstance(standard, dict) and isinstance(standard.get(name), dict):
        found.append(_descriptor(standard[name], "standard", ""))
    oem = resource.get("Oem")
    if isinstance(oem, dict):
        for vendor, value in sorted(oem.items()):
            if not isinstance(vendor, str) or not isinstance(value, dict):
                continue
            container = value.get("Actions")
            if isinstance(container, dict) and isinstance(container.get(name), dict):
                found.append(_descriptor(container[name], "oem", vendor))
    return [entry for entry in found if entry["target"]]


def _descriptor(action, source, vendor):
    descriptor = {
        "target": action.get("target") if isinstance(action.get("target"), str) else "",
        "source": source,
        "vendor": vendor,
        "action": action,
    }
    info = action.get("@Redfish.ActionInfo")
    descriptor["actionInfo"] = info if isinstance(info, str) else ""
    return descriptor


def accepts_connect_disconnect(action_info):
    """Whether a vendor attach action takes the arguments this client sends.

    The extension is used only when its own metadata says it accepts an image
    and a connect instruction, so capability is proved rather than assumed. An
    action that declares no allowable values constrains nothing and is accepted.
    """
    if not isinstance(action_info, dict):
        return False
    parameters = {}
    for parameter in action_info.get("Parameters") or []:
        if isinstance(parameter, dict) and isinstance(parameter.get("Name"), str):
            parameters[parameter["Name"]] = parameter
    if "Image" not in parameters or "VmmControlType" not in parameters:
        return False
    allowed = parameters["VmmControlType"].get("AllowableValues")
    if allowed is None:
        return True
    if not isinstance(allowed, list):
        return False
    return {"Connect", "Disconnect"}.issubset({v for v in allowed if isinstance(v, str)})


def power_on_reset_type(system, action_info=None):
    """The reset type this controller advertises for turning a machine on."""
    allowable = []
    for descriptor in actions(system, "#ComputerSystem.Reset"):
        values = descriptor["action"].get("ResetType@Redfish.AllowableValues")
        allowable.extend(v for v in values or [] if isinstance(v, str))
    if isinstance(action_info, dict):
        for parameter in action_info.get("Parameters") or []:
            if isinstance(parameter, dict) and parameter.get("Name") == "ResetType":
                allowable.extend(v for v in parameter.get("AllowableValues") or [] if isinstance(v, str))
    for candidate in POWER_ON_ORDER:
        if candidate in allowable:
            return candidate
    return "On"


def media_candidates(system_members, manager_members):
    """Every distinct virtual-media member, preferring an optical device.

    One controller exposes these under the system, another under its manager,
    and a third under both, so the two views are unioned and de-duplicated on
    the resolved URL before anything is probed.
    """
    ordered = []
    for url in list(system_members) + list(manager_members):
        if url and url not in ordered:
            ordered.append(url)
    return sorted(ordered, key=lambda url: (0 if MEDIA_NAMES.search(url.rstrip("/")) else 1, url))


def optical(member):
    """Whether a probed member is the optical device installer media needs."""
    if not isinstance(member, dict):
        return False
    types = member.get("MediaTypes")
    if isinstance(types, list) and types:
        return any(isinstance(t, str) and t.upper() in ("CD", "DVD") for t in types)
    return bool(MEDIA_NAMES.search(str(member.get("@odata.id") or "").rstrip("/")))


def inserted_image(member):
    """The image a member reports presenting, or the empty string."""
    if not isinstance(member, dict) or not _truthy(member.get("Inserted")):
        return ""
    return str(member.get("Image") or "")


def image_matches(observed, expected):
    """Whether a reported image is the one that was inserted.

    A controller may normalize what it echoes back, most often by dropping a
    default port, so scheme, host and path are compared rather than the text.
    """
    if not observed:
        return False
    if observed == expected:
        return True
    try:
        left, right = urlsplit(observed), urlsplit(expected)
    except ValueError:
        return False
    if not left.scheme or left.scheme.lower() != right.scheme.lower():
        return False
    if (left.hostname or "").lower() != (right.hostname or "").lower():
        return False
    if left.path != right.path or left.query != right.query:
        return False
    return left.port == right.port or left.port is None


def transfer_protocol(image):
    """The protocol a controller is told to fetch with, from the URL itself.

    Some firmware refuses an insert whose declared protocol disagrees with the
    scheme of the image, before it creates a task to report the refusal in.
    """
    scheme = urlsplit(image).scheme.upper()
    return scheme if scheme in ("HTTP", "HTTPS", "NFS", "CIFS") else ""


def task_reference(response_json, headers):
    """The task resource an asynchronous insert reports its outcome in.

    A monitor URL is normalized to the task itself, because the monitor is a
    polling endpoint while the task carries the state and the message.
    """
    reference = ""
    if isinstance(response_json, dict):
        for key in ("@odata.id", "TaskMonitor"):
            if isinstance(response_json.get(key), str) and response_json[key]:
                reference = response_json[key]
                break
    if not reference and isinstance(headers, dict):
        for key in ("location", "Location"):
            if isinstance(headers.get(key), str) and headers[key]:
                reference = headers[key]
                break
    reference = reference.replace("/TaskService/TaskMonitors/", "/TaskService/Tasks/")
    return re.sub(r"/Monitor/?$", "", reference)


def task_settled(task):
    """Whether a task has reached a state it will not leave, and whether it won."""
    if not isinstance(task, dict):
        return True, False
    state = task.get("TaskState")
    if not isinstance(state, str) or state not in TERMINAL_TASK_STATES:
        return False, False
    return True, state == "Completed" and task.get("TaskStatus") in (None, "OK")


def canonical_mac(value):
    """One spelling for a hardware address, whatever the source used."""
    text = value.strip().lower() if isinstance(value, str) else ""
    compact = re.sub(r"[^0-9a-f]", "", text)
    if len(compact) == 12:
        text = ":".join(compact[index:index + 2] for index in range(0, 12, 2))
    else:
        text = text.replace("-", ":")
    return text if MAC.match(text) else ""


def reported_macs(members):
    """Every address the controller reports, and why any member proved none.

    A member that cannot be read proves nothing, so it is reported as a failure
    rather than silently reducing the observed set: an incomplete inventory must
    never look like a complete one that happens to be missing an address.
    """
    observed, failures = set(), []
    for index, member in enumerate(members):
        if not isinstance(member, dict):
            failures.append("member[%d] could not be read" % index)
            continue
        found = [canonical_mac(member.get(field)) for field in ("MACAddress", "PermanentMACAddress")]
        found = [address for address in found if address]
        if not found:
            failures.append("member[%d] reports no hardware address" % index)
            continue
        observed.update(found)
    return sorted(observed), failures


def _truthy(value):
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in ("1", "true", "yes", "on")
    return bool(value)
