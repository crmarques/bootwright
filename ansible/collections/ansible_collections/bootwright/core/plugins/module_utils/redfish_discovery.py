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
# Interrupted and Suspended are absent: the Task schema says either task is
# expected to restart, so it is still running.
TERMINAL_TASK_STATES = ("Completed", "Exception", "Killed", "Cancelled")
DEFAULT_PORTS = {"http": 80, "https": 443}
MESSAGE_ID = re.compile(r"[A-Za-z0-9._]+")


def resolve(base, reference):
    """Turn a path reference into an absolute URL against the controller.

    A reference that names a scheme or an authority, a network-path reference
    included, is returned as it is rather than rebased onto the endpoint, so the
    client's own authority check sees exactly what the controller returned.
    """
    reference = reference if isinstance(reference, str) else ""
    reference = reference.strip()
    if not reference:
        return ""
    try:
        parsed = urlsplit(reference)
    except ValueError:
        return reference
    if parsed.scheme or reference.startswith("//"):
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
    parameters = _parameters(action_info)
    if "Image" not in parameters or "VmmControlType" not in parameters:
        return False
    allowed = parameters["VmmControlType"].get("AllowableValues")
    if allowed is None:
        return True
    if not isinstance(allowed, list):
        return False
    return {"Connect", "Disconnect"}.issubset({v for v in allowed if isinstance(v, str)})


def parameter_required(action_info, name):
    """Whether an action's metadata marks one of its parameters as required."""
    return _parameters(action_info).get(name, {}).get("Required") is True


def _parameters(action_info):
    parameters = action_info.get("Parameters") if isinstance(action_info, dict) else None
    found = {}
    for parameter in parameters if isinstance(parameters, list) else []:
        if isinstance(parameter, dict) and isinstance(parameter.get("Name"), str):
            found[parameter["Name"]] = parameter
    return found


def reset_action(system):
    """The standard reset action a system advertises, or None."""
    for descriptor in actions(system, "#ComputerSystem.Reset"):
        if descriptor["source"] == "standard":
            return descriptor
    return None


def allowed_reset_types(system, action_info):
    """The reset types a controller advertises, or None when it names none.

    The action's own `ResetType@Redfish.AllowableValues` wins; otherwise the
    `ResetType` parameter of its fetched `@Redfish.ActionInfo` decides. None
    means neither constrains the type, which is not the same as an empty list.
    """
    descriptor = reset_action(system)
    inline = descriptor["action"].get("ResetType@Redfish.AllowableValues") if descriptor else None
    if isinstance(inline, list):
        return [value for value in inline if isinstance(value, str)]
    listed = _parameters(action_info).get("ResetType", {}).get("AllowableValues")
    if isinstance(listed, list):
        return [value for value in listed if isinstance(value, str)]
    return None


def power_on_reset_type(allowed):
    """The advertised reset type that turns a machine on, or the empty string."""
    for candidate in POWER_ON_ORDER:
        if candidate in allowed:
            return candidate
    return ""


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
    if not media_present(member):
        return ""
    return str(member.get("Image") or "")


def media_present(member):
    """Whether a member reports media inserted, whether or not it names the image."""
    return isinstance(member, dict) and _truthy(member.get("Inserted"))


def image_matches(observed, expected):
    """Whether a reported image is the one that was inserted.

    A controller may normalize what it echoes back, most often by dropping a
    default port, so scheme, host, port and path are compared rather than the
    text. An absent port is the scheme's default and nothing else: an image
    served on 8443 is not proved by an echo that names no port.
    """
    if not observed:
        return False
    if observed == expected:
        return True
    try:
        left, right = urlsplit(observed), urlsplit(expected)
        ports = (left.port, right.port)
    except ValueError:
        return False
    if not left.scheme or left.scheme != right.scheme:
        return False
    if (left.hostname or "").lower() != (right.hostname or "").lower():
        return False
    if left.path != right.path or left.query != right.query:
        return False
    default = DEFAULT_PORTS.get(left.scheme)
    observed_port, expected_port = (default if port is None else port for port in ports)
    return observed_port == expected_port


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
    polling endpoint while the task carries the state and the message. The
    headers are the client's, whose names are already lower-cased.
    """
    reference = ""
    if isinstance(response_json, dict):
        for key in ("@odata.id", "TaskMonitor"):
            if isinstance(response_json.get(key), str) and response_json[key]:
                reference = response_json[key]
                break
    if not reference and isinstance(headers, dict) and isinstance(headers.get("location"), str):
        reference = headers["location"]
    reference = reference.replace("/TaskService/TaskMonitors/", "/TaskService/Tasks/")
    return re.sub(r"/Monitor/?$", "", reference)


def task_settled(task):
    """Whether a task has reached a state it will not leave, and whether it won.

    A completed task has done what it was asked to, whatever `TaskStatus` says
    about it: a Warning reports a condition beside the outcome, and the device
    read back afterwards decides. Anything but a known terminal state, a body
    that is not a task included, has not settled yet.
    """
    state = task.get("TaskState") if isinstance(task, dict) else None
    if not isinstance(state, str) or state not in TERMINAL_TASK_STATES:
        return False, False
    return True, state == "Completed"


def message_id(body):
    """The controller's own message identifier for an outcome, or the empty string.

    It is taken from a task's first message or an error body's first extended
    information entry, and kept only when it is a bounded identifier, so what
    a controller writes there never reaches a failure line as free text.
    """
    entries = []
    if isinstance(body, dict):
        entries = body.get("Messages")
        error = body.get("error")
        if not isinstance(entries, list) and isinstance(error, dict):
            entries = error.get("@Message.ExtendedInfo")
    first = entries[0] if isinstance(entries, list) and entries else None
    value = first.get("MessageId") if isinstance(first, dict) else None
    if isinstance(value, str) and MESSAGE_ID.fullmatch(value):
        return value[:64]
    return ""


def boot_selected(system, target):
    """Whether a system reports the one-time boot device that was selected.

    `Once` proves it. `Continuous` proves it only on a controller that does not
    offer `Once` at all, because such firmware reports every override as
    continuous; one that advertises `Once` and reports `Continuous` did not
    apply what was asked.
    """
    boot = system.get("Boot") if isinstance(system, dict) else None
    if not isinstance(boot, dict) or boot.get("BootSourceOverrideTarget") != target:
        return False
    enabled = boot.get("BootSourceOverrideEnabled")
    if enabled == "Once":
        return True
    offered = boot.get("BootSourceOverrideEnabled@Redfish.AllowableValues")
    return enabled == "Continuous" and not (isinstance(offered, list) and "Once" in offered)


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
