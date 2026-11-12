"""The NetworkManager connections one libvirt provider host owns: its trunks and VLAN legs.

A tagged managed Network is a VLAN leg holding its host address on a trunk
bridge that carries no address. Both are NetworkManager profiles, so one daemon
owns parent and legs, their boot order and their autoconnect path, and NM binds
each to firewalld's trusted zone at runtime without touching firewalld's own
configuration.

nmcli has no property for a profile's user data (its `user` setting carries
none it can set or show), so each profile is published as a keyfile that
Bootwright writes atomically and NetworkManager loads by path; nmcli only reads,
loads, activates and deletes. A profile is this provider's only when its UUID,
name, type, device and backing file are the frozen ones and its keyfile carries
each of the four user data keys with the frozen value. A keyfile whose user data
names this context and provider, but which the request no longer freezes, is
still this provider's when its name, UUID, type and device are the ones the same
derivation gives: the apply and the removal delete it, so a dropped Network
never leaves a leg up outside the routing table.
"""

from __future__ import annotations

import hashlib
import ipaddress
import os
import re
import stat
import tempfile
import time

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_nm_carrier as carrier

NMCLI = "/usr/bin/nmcli"
SYSTEMCTL = "/usr/bin/systemctl"
SERVICE = "NetworkManager.service"
CONNECTIONS = "/etc/NetworkManager/system-connections"
CARRIER_CONFIG = carrier.CONFIG
NET = "/sys/class/net"
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}
MAX_OUTPUT = 1 << 20
MAX_TRUNKS = 20
MAX_LEGS = 20
MAX_PROFILES = 1024
MAX_KEYFILE = 1 << 16
WAIT = "60"
LINK_POLLS = 20
IDENTIFIER = re.compile(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")
BRIDGE = re.compile(r"[A-Za-z0-9_-]{1,15}\Z")
DEVICE = re.compile(r"[A-Za-z0-9_.-]{1,15}\Z")
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")
MAC = re.compile(r"[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}\Z")
FORWARDS = ("", "none", "nat")
TRUNK_KEYS = frozenset(("bridge", "mac", "name", "uuid"))
LEG_KEYS = frozenset(("address", "device", "forward", "name", "network", "trunk", "uuid", "vlan"))
ZONE = "trusted"
SUFFIX = ".nmconnection"
CONNECTION_DOMAIN = "bootwright.substrate.libvirt.connection-v1"
USER_KEYS = frozenset(("bootwright.io.context", "bootwright.io.network", "bootwright.io.provider", "bootwright.io.role"))


def command(runner, arguments):
    code, output, error = runner(arguments, check_rc=False, environ_update=ENVIRONMENT)
    return code, output[:MAX_OUTPUT], error[:MAX_OUTPUT]


def identity(request):
    found = request.get("identity") or {}
    context, provider = found.get("context"), found.get("object")
    for value in (context, provider):
        if not isinstance(value, str) or not IDENTIFIER.match(value):
            raise ValueError("invalid provider identity")
    return context, provider


def trunks_of(request):
    """The frozen trunks, each validated, in request order."""
    context, provider = identity(request)
    values = request.get("trunks") or []
    if not isinstance(values, list) or len(values) > MAX_TRUNKS:
        raise ValueError("invalid trunks")
    found = []
    for value in values:
        if not isinstance(value, dict) or set(value) != TRUNK_KEYS or not all(isinstance(value[key], str) for key in TRUNK_KEYS):
            raise ValueError("invalid trunk")
        mac = value["mac"]
        if not BRIDGE.match(value["bridge"]) or not UUID.match(value["uuid"]) or not MAC.match(mac) or int(mac[:2], 16) & 1:
            raise ValueError("invalid trunk identity")
        if value["name"] != "bootwright-%s-%s-trunk-%s" % (context, provider, value["bridge"]):
            raise ValueError("the trunk connection does not match the frozen provider")
        found.append(dict(value))
    return found


def legs_of(request, trunks=None):
    """The frozen VLAN legs, each validated against its trunk, in request order."""
    context, _provider = identity(request)
    trunks = trunks_of(request) if trunks is None else trunks
    bridges = {trunk["bridge"] for trunk in trunks}
    values = request.get("legs") or []
    if not isinstance(values, list) or len(values) > MAX_LEGS:
        raise ValueError("invalid legs")
    found = []
    for value in values:
        if not isinstance(value, dict) or set(value) | {"forward"} != LEG_KEYS:
            raise ValueError("invalid leg")
        forward = value.get("forward", "")
        texts = [value[key] for key in ("address", "device", "name", "network", "trunk", "uuid")] + [forward]
        vlan = value["vlan"]
        if not all(isinstance(text, str) for text in texts) or isinstance(vlan, bool) or not isinstance(vlan, int) or not 1 <= vlan <= 4094:
            raise ValueError("invalid leg")
        if forward not in FORWARDS or not IDENTIFIER.match(value["network"]) or not UUID.match(value["uuid"]):
            raise ValueError("invalid leg identity")
        if value["name"] != "bootwright-%s-%s" % (context, value["network"]) or value["trunk"] not in bridges:
            raise ValueError("the leg connection does not match the frozen provider")
        if value["device"] != "%s.%d" % (value["trunk"], value["vlan"]) or not DEVICE.match(value["device"]):
            raise ValueError("the leg device does not name its trunk and VLAN")
        address = ipaddress.ip_interface(value["address"])
        if address.version != 4 or str(address) != value["address"] or address.network.prefixlen == 0:
            raise ValueError("invalid leg address")
        found.append(dict(value, forward=forward))
    return found


def connection_uuid(context, name):
    """The connection's UUID as Go derives it: the version-8 UUID of the SHA-256 of the domain, the context and the name."""
    digest = bytearray(hashlib.sha256(("%s\0%s\0%s" % (CONNECTION_DOMAIN, context, name)).encode()).digest()[:16])
    digest[6] = digest[6] & 0x0F | 0x80
    digest[8] = digest[8] & 0x3F | 0x80
    value = digest.hex()
    return "-".join((value[0:8], value[8:12], value[12:16], value[16:20], value[20:32]))


def unique(values, what):
    if len(values) != len(set(values)):
        raise ValueError("two %s share one identity" % what)


def wanted(request):
    """Every connection the frozen request owns: the trunks first, then the legs, each with its definition.

    Each carries its keyfile sections and the properties nmcli reports for a
    profile that carries it.
    """
    if not request.get("trunks") and not request.get("legs"):
        return []
    context, provider = identity(request)
    trunks = trunks_of(request)
    legs = legs_of(request, trunks)
    if bool(trunks) != bool(legs) or {leg["trunk"] for leg in legs} != {trunk["bridge"] for trunk in trunks}:
        raise ValueError("every trunk carries a leg and every leg a frozen trunk")
    found = [trunk_connection(context, provider, trunk) for trunk in trunks] + [leg_connection(context, provider, leg) for leg in legs]
    unique([entry["name"] for entry in found], "connections")
    unique([entry["uuid"] for entry in found], "connections")
    unique([entry["device"] for entry in found], "connections")
    return found


def user_data(context, provider, network, role):
    return {"bootwright.io.context": context, "bootwright.io.network": network,
            "bootwright.io.provider": provider, "bootwright.io.role": role}


def trunk_connection(context, provider, trunk):
    bridge, mac = trunk["bridge"], trunk["mac"].upper()
    return {
        "name": trunk["name"], "uuid": trunk["uuid"], "role": "trunk", "device": bridge, "type": "bridge", "bridge": bridge,
        "sections": [
            ("connection", [("id", trunk["name"]), ("uuid", trunk["uuid"]), ("type", "bridge"), ("interface-name", bridge),
                            ("autoconnect", "true"), ("zone", ZONE)]),
            ("bridge", [("mac-address", mac), ("stp", "false"), ("vlan-filtering", "false")]),
            ("ipv4", [("method", "disabled")]),
            ("ipv6", [("method", "disabled")]),
            ("user", sorted(user_data(context, provider, bridge, "trunk").items())),
        ],
        "properties": {
            "connection.id": trunk["name"], "connection.uuid": trunk["uuid"], "connection.type": "bridge",
            "connection.interface-name": bridge, "connection.autoconnect": "yes", "connection.zone": ZONE,
            "bridge.stp": "no", "bridge.mac-address": mac, "bridge.vlan-filtering": "no",
            "ipv4.method": "disabled", "ipv6.method": "disabled",
        },
        "fields": "connection,bridge,ipv4,ipv6",
    }


def leg_connection(context, provider, leg):
    return {
        "name": leg["name"], "uuid": leg["uuid"], "role": "leg", "device": leg["device"], "type": "vlan", "bridge": leg["trunk"],
        "network": leg["network"], "address": leg["address"], "forward": leg["forward"],
        "sections": [
            ("connection", [("id", leg["name"]), ("uuid", leg["uuid"]), ("type", "vlan"), ("interface-name", leg["device"]),
                            ("autoconnect", "true"), ("zone", ZONE)]),
            ("vlan", [("id", str(leg["vlan"])), ("parent", leg["trunk"])]),
            ("ipv4", [("address1", leg["address"]), ("method", "manual"), ("never-default", "true")]),
            ("ipv6", [("method", "disabled")]),
            ("user", sorted(user_data(context, provider, leg["network"], "leg").items())),
        ],
        "properties": {
            "connection.id": leg["name"], "connection.uuid": leg["uuid"], "connection.type": "vlan",
            "connection.interface-name": leg["device"], "connection.autoconnect": "yes", "connection.zone": ZONE,
            "vlan.parent": leg["trunk"], "vlan.id": str(leg["vlan"]), "ipv4.method": "manual",
            "ipv4.addresses": leg["address"], "ipv4.never-default": "yes", "ipv4.gateway": "", "ipv6.method": "disabled",
        },
        "fields": "connection,vlan,ipv4,ipv6",
    }


def keyfile(entry):
    """The keyfile NetworkManager loads the connection from, exactly as it is written."""
    lines = []
    for section, values in entry["sections"]:
        lines.append("[%s]" % section)
        lines += ["%s=%s" % pair for pair in values]
        lines.append("")
    return "\n".join(lines).encode()


def parse_keyfile(content):
    """The groups of one keyfile as a dict of dicts, or None when it is not one.

    GKeyFile admits comments, blank lines, groups and key=value lines only.
    """
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError:
        return None
    groups, current = {}, None
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            current = groups.setdefault(line[1:-1], {})
            continue
        key, separator, value = line.partition("=")
        if current is None or not separator:
            return None
        current[key.strip()] = value.strip()
    return groups


def carries(groups, entry):
    """Whether parsed keyfile groups set every value the definition sets, and exactly its user data."""
    if groups is None:
        return False
    for section, values in entry["sections"]:
        found = groups.get(section, {})
        if section == "user" and found != dict(values):
            return False
        if any(found.get(key) != value for key, value in values):
            return False
    return True


def marked(groups, entry):
    """Whether parsed keyfile groups name this connection and carry exactly its user data."""
    if groups is None:
        return False
    connection, user = groups.get("connection", {}), groups.get("user", {})
    expected = dict(entry["sections"][0][1])
    return (user == dict(entry["sections"][-1][1]) and connection.get("uuid") == entry["uuid"]
            and connection.get("id") == entry["name"] and connection.get("type") == expected["type"])


def keyfile_path(root, entry):
    return os.path.join(root, entry["name"] + SUFFIX)


def safe_root(root, owner):
    """Whether the connection directory exists; one another user could write is refused."""
    if not os.path.lexists(root):
        return False
    info = os.lstat(root)
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != owner or stat.S_IMODE(info.st_mode) & 0o022:
        raise ValueError("unsafe NetworkManager connection directory")
    return True


def read_keyfile(root, entry, owner=0):
    """What lies at the connection's keyfile path: (exists, safe and parsed groups or None)."""
    safe_root(root, owner)
    return read_path(keyfile_path(root, entry), owner)


def read_path(path, owner):
    """What lies at one keyfile path: (exists, safe and parsed groups or None).

    A keyfile NetworkManager trusts is a regular root file of mode 0600; one
    that is not, or a symbolic link, is never read as this provider's.
    """
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        return False, None
    except OSError:
        return True, None
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != owner or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600:
            return True, None
        content = os.read(fd, MAX_KEYFILE + 1)
    finally:
        os.close(fd)
    if len(content) > MAX_KEYFILE:
        return True, None
    return True, parse_keyfile(content)


def owned_identity(groups, context, provider, name):
    """The trunk or leg a keyfile proves this context and provider own under that name, or None.

    Its `[user]` group is exactly the four keys naming this context and
    provider, and its name, UUID, type and device are the ones the frozen
    derivation gives a connection of that role.
    """
    if groups is None:
        return None
    user, connection = groups.get("user", {}), groups.get("connection", {})
    if set(user) != USER_KEYS or user["bootwright.io.context"] != context or user["bootwright.io.provider"] != provider:
        return None
    role, network, device = user["bootwright.io.role"], user["bootwright.io.network"], connection.get("interface-name", "")
    if connection.get("id") != name or connection.get("uuid") != connection_uuid(context, name):
        return None
    if role == "trunk":
        valid = (BRIDGE.match(network) and name == "bootwright-%s-%s-trunk-%s" % (context, provider, network)
                 and connection.get("type") == "bridge" and device == network)
        kind = "bridge"
    elif role == "leg":
        vlan = groups.get("vlan", {})
        valid = (IDENTIFIER.match(network) and name == "bootwright-%s-%s" % (context, network) and connection.get("type") == "vlan"
                 and DEVICE.match(device) and BRIDGE.match(vlan.get("parent", "")) and device == "%s.%s" % (vlan.get("parent"), vlan.get("id")))
        kind = "vlan"
    else:
        return None
    if not valid:
        return None
    return {"name": name, "uuid": connection["uuid"], "role": role, "device": device, "type": kind, "network": network}


def unfrozen_keyfiles(root, owner, context, provider, frozen):
    """Every keyfile of this context and provider whose connection the request does not freeze, with the identity it proves."""
    if not safe_root(root, owner):
        return []
    prefix = "bootwright-%s-" % context
    with os.scandir(root) as listing:
        names = sorted(item.name for item in listing if item.name.startswith(prefix) and item.name.endswith(SUFFIX))
    if len(names) > MAX_PROFILES:
        raise ValueError("too many NetworkManager keyfiles of this context")
    found = []
    for filename in names:
        name = filename[:-len(SUFFIX)]
        if name in frozen:
            continue
        entry = owned_identity(read_path(os.path.join(root, filename), owner)[1], context, provider, name)
        if entry is not None:
            found.append(entry)
    return found


def split_terse(line):
    """One `nmcli -t -e yes` tabular line, split on its unescaped colons and unescaped."""
    fields, current, escaped = [], [], False
    for character in line:
        if escaped:
            current.append(character)
            escaped = False
        elif character == "\\":
            escaped = True
        elif character == ":":
            fields.append("".join(current))
            current = []
        else:
            current.append(character)
    fields.append("".join(current))
    return fields


def executable(path):
    return os.path.isfile(path) and os.access(path, os.X_OK)


def service_state(runner):
    """NetworkManager's load and active state, or None when systemd did not answer."""
    code, output, _error = command(runner, [SYSTEMCTL, "show", "--property=LoadState", "--property=ActiveState", SERVICE])
    values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
    if code not in (0, 4) or values.get("LoadState") not in ("loaded", "not-found"):
        return None
    return values.get("LoadState"), values.get("ActiveState", "")


def profiles(runner):
    """Every profile NetworkManager holds, or None when nmcli did not answer in full."""
    code, output, _error = command(runner, [NMCLI, "-t", "-e", "yes", "-f", "NAME,UUID,TYPE,DEVICE,ACTIVE,FILENAME", "connection", "show"])
    if code != 0 or len(output) >= MAX_OUTPUT:
        return None
    found = []
    for line in output.splitlines():
        if not line:
            continue
        fields = split_terse(line)
        if len(fields) != 6 or not fields[1]:
            return None
        found.append(dict(zip(("name", "uuid", "type", "device", "active", "filename"), fields)))
    if len(found) > MAX_PROFILES:
        return None
    return found


def details(runner, uuid, fields):
    """The named settings of one profile as nmcli reports them, or None when it did not answer.

    In multi-line terse output each line is one property, its name, a colon
    and its value unescaped.
    """
    code, output, _error = command(runner, [NMCLI, "-t", "-f", fields, "connection", "show", "uuid", uuid])
    if code != 0 or len(output) >= MAX_OUTPUT:
        return None
    found = {}
    for line in output.splitlines():
        key, separator, value = line.partition(":")
        if separator:
            found[key] = value
    return found


def judge(runner, entry, listing, root, owner):
    """One wanted connection as the host holds it, and what of it collides."""
    record = dict(entry, answered=True, present=False, owned=False, active=False, definition=False, zone="",
                  stale=False, collisions=[])
    path = keyfile_path(root, entry)
    exists, groups = read_keyfile(root, entry, owner)
    record["file"] = exists
    record["foreign_file"] = exists and not marked(groups, entry)
    for profile in listing:
        if profile["name"] == entry["name"] and profile["uuid"] != entry["uuid"]:
            record["collisions"].append("connection %s has UUID %s, not the frozen %s: delete or rename it" % (
                entry["name"], profile["uuid"], entry["uuid"]))
    held = [profile for profile in listing if profile["uuid"] == entry["uuid"]]
    if not held:
        if exists and not marked(groups, entry):
            record["collisions"].append("keyfile %s exists and is not this provider's connection %s" % (path, entry["name"]))
        record["stale"] = exists and marked(groups, entry)
        return record
    profile = held[0]
    shown = details(runner, entry["uuid"], entry["fields"])
    if shown is None:
        record["answered"] = False
        return record
    record["present"] = True
    record["zone"] = shown.get("connection.zone", "")
    owned = (profile["name"] == entry["name"] and profile["type"] == entry["type"] and profile["filename"] == path
             and shown.get("connection.interface-name") == entry["device"] and marked(groups, entry))
    if not owned:
        record["collisions"].append("connection UUID %s is not this provider's %s %s: delete it or change the frozen identity" % (
            entry["uuid"], entry["role"], entry["name"]))
        return record
    record["owned"] = True
    record["active"] = profile["active"] == "yes" and profile["device"] == entry["device"]
    record["definition"] = carries(groups, entry) and all(shown.get(key) == value for key, value in entry["properties"].items())
    return record


def judge_unfrozen(runner, entry, listing, root):
    """One owned keyfile the request no longer freezes as the host holds it, and what of it collides."""
    record = dict(entry, answered=True, present=False, owned=False, active=False, file=True, collisions=[])
    held = [profile for profile in listing if profile["uuid"] == entry["uuid"]]
    if not held:
        return record
    profile = held[0]
    shown = details(runner, entry["uuid"], "connection.interface-name")
    if shown is None:
        record["answered"] = False
        return record
    record["present"] = True
    if not (profile["name"] == entry["name"] and profile["type"] == entry["type"] and profile["filename"] == keyfile_path(root, entry)
            and shown.get("connection.interface-name") == entry["device"]):
        record["collisions"].append("connection UUID %s is not this provider's %s %s, which the request no longer freezes: "
                                    "delete that profile and repeat" % (entry["uuid"], entry["role"], entry["name"]))
        return record
    record["owned"] = True
    record["active"] = profile["active"] == "yes" and profile["device"] == entry["device"]
    return record


def unanswered(entry, **fields):
    record = dict(entry, answered=False, present=False, owned=False, active=False, definition=False, zone="", stale=False,
                  file=False, foreign_file=False, collisions=[])
    record.update(fields)
    return record


def foreign_devices(runner, listing, records, skipped=()):
    """Every other profile that binds a frozen device, by name, or None when nmcli did not answer.

    A profile this provider owns but no longer freezes is skipped: it is
    removed before any frozen connection changes.
    """
    ours = {record["uuid"] for record in records} | set(skipped)
    devices = {record["device"]: record for record in records}
    found = []
    for profile in listing:
        if profile["uuid"] in ours:
            continue
        shown = details(runner, profile["uuid"], "connection.interface-name")
        if shown is None:
            return None
        for device in {shown.get("connection.interface-name", ""), profile["device"]} & set(devices):
            found.append("connection %s (UUID %s) binds device %s of %s %s: delete it or change the trunk bridge" % (
                profile["name"], profile["uuid"], device, devices[device]["role"], devices[device]["name"]))
    return sorted(set(found))


def inspect(runner, request, root=None, owner=0):
    """What the host holds of every frozen connection and of every owned one it no longer freezes, read-only.

    `ready` is whether NetworkManager is active and nmcli is installed;
    `answered` whether every read answered. A host with neither the
    NetworkManager unit nor nmcli holds no profile, which is positive absence.
    `unfrozen` holds each owned keyfile the request does not freeze; NetworkManager
    is read only when one exists or a connection is frozen.
    """
    root = CONNECTIONS if root is None else root
    seen = {"ready": False, "answered": True, "records": [], "unfrozen": [], "collisions": []}
    entries = wanted(request)
    context, provider = identity(request)
    seen["carrierPolicy"] = carrier.inspect(context, provider, [entry for entry in entries if entry["role"] == "leg"],
                                            CARRIER_CONFIG, owner)
    unfrozen = unfrozen_keyfiles(root, owner, context, provider, {entry["name"] for entry in entries})
    if not entries and not unfrozen:
        return seen
    state = service_state(runner)
    installed = executable(NMCLI)
    seen["ready"] = state is not None and state[1] == "active" and installed
    if state is not None and state[0] == "not-found" and not installed:
        listing = []
    else:
        listing = profiles(runner) if installed else None
    if listing is None:
        seen["answered"] = False
        seen["records"] = [unanswered(entry) for entry in entries]
        seen["unfrozen"] = [unanswered(entry, file=True) for entry in unfrozen]
        return seen
    seen["records"] = [judge(runner, entry, listing, root, owner) for entry in entries]
    seen["unfrozen"] = [judge_unfrozen(runner, entry, listing, root) for entry in unfrozen]
    seen["answered"] = all(record["answered"] for record in seen["records"] + seen["unfrozen"])
    foreign = foreign_devices(runner, listing, seen["records"], [entry["uuid"] for entry in unfrozen])
    if foreign is None:
        seen["answered"] = False
        foreign = []
    seen["collisions"] = sorted({item for record in seen["records"] + seen["unfrozen"] for item in record["collisions"]} | set(foreign))
    return seen


def evidence(record):
    return {key: record[key] for key in ("name", "role", "device", "answered", "present", "owned", "active", "definition", "zone")}


def refusals(seen, requested):
    """Why the apply may not change any connection, before any effect."""
    found = []
    try:
        carrier.require_owned(seen["carrierPolicy"])
    except ValueError as failure:
        found.append(str(failure))
    if requested and not seen["ready"]:
        found.append("a tagged managed Network needs NetworkManager.service active and /usr/bin/nmcli installed; "
                     "Bootwright neither installs nor enables them: install NetworkManager, enable and start it, then repeat")
    if not seen["answered"]:
        found.append("NetworkManager did not answer nmcli connection show for every profile, so nothing proves a trunk or "
                     "VLAN leg free; restore it and repeat")
    return found + seen["collisions"]


def observe(runner, request, root=None, owner=0):
    """The evidence of every frozen connection, NetworkManager's readiness and why an apply would refuse.

    `connectionsAnswered` is whether every read answered, and
    `unfrozenConnections` names each owned connection the request no longer
    freezes, which the apply and the removal delete, so neither presence nor
    absence is proved while one remains. A read that fails reports each frozen
    connection unanswered, never absent.
    """
    try:
        seen = inspect(runner, request, root, owner)
    except (ValueError, KeyError, TypeError, OSError):
        try:
            entries = wanted(request)
        except (ValueError, KeyError, TypeError):
            entries = []
        return {"networkManager": False, "connections": [evidence(unanswered(entry)) for entry in entries],
                "connectionsAnswered": False, "unfrozenConnections": [],
                "carrierPolicy": {"answered": False, "definition": False, "name": "", "owned": False, "present": False},
                "refusals": ["the frozen trunks and legs could not be read"], "devices": [], "pseudo": [], "trunks": []}
    held = seen["records"] + [record for record in seen["unfrozen"] if record["owned"]]
    return {
        "networkManager": seen["ready"],
        "carrierPolicy": seen["carrierPolicy"],
        "connections": [evidence(record) for record in seen["records"]],
        "connectionsAnswered": seen["answered"],
        "unfrozenConnections": sorted(record["name"] for record in seen["unfrozen"]),
        "refusals": refusals(seen, bool(seen["records"])),
        "devices": sorted(record["device"] for record in held if record["owned"] and record["active"]),
        "pseudo": host_networks(seen["records"]),
        "trunks": [{"name": record["name"], "bridge": record["device"], "present": record["present"] or link_present(record["device"]),
                    "drifted": record["owned"] and record["active"] and not record.get("definition", True)}
                   for record in seen["records"] + seen["unfrozen"] if record["role"] == "trunk"],
    }


def host_networks(records):
    """Each trunk and leg as the host collision check reads a managed network: its device as the bridge."""
    found = []
    for record in records:
        entry = {"name": record["name"], "bridge": record["device"], "managed": True}
        if record["role"] == "leg":
            entry.update(name=record["network"], address=record["address"])
        found.append(entry)
    return found


def require(runner, arguments, what):
    code, _output, error = command(runner, arguments)
    if code != 0:
        raise ValueError("nmcli could not %s: %s" % (what, error.strip()[:512] or "exit %d" % code))


def write_keyfile(root, entry):
    """Publish the keyfile atomically; NetworkManager ignores the hidden staging name."""
    os.makedirs(root, mode=0o700, exist_ok=True)
    fd, stage = tempfile.mkstemp(prefix=".bootwright-", dir=root)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(keyfile(entry))
            stream.flush()
            os.fchmod(stream.fileno(), 0o600)
            os.fsync(stream.fileno())
        os.replace(stage, keyfile_path(root, entry))
    finally:
        if os.path.lexists(stage):
            os.unlink(stage)


def converge(runner, record, root):
    """Bring one owned connection to its frozen definition and activate it."""
    changed = False
    if not record["definition"]:
        write_keyfile(root, record)
        require(runner, [NMCLI, "connection", "load", keyfile_path(root, record)], "load connection " + record["name"])
        changed = True
    if changed or not record["active"]:
        require(runner, [NMCLI, "-w", WAIT, "connection", "up", "uuid", record["uuid"]], "activate connection " + record["name"])
        changed = True
    return changed


def guard(seen, guests, conflicts):
    """Refuse before any effect: a collision, a host link or route a connection would take, or a drifted trunk in use."""
    found = refusals(seen, True)
    if found:
        raise ValueError("the trunks and VLAN legs refuse before any change: %s" % "; ".join(found))
    collided, _shadows, answered = conflicts(host_networks(seen["records"]),
                                             {record["device"] for record in seen["records"] if record["owned"] and record["active"]})
    if not answered:
        raise ValueError("the host did not answer ip link show or ip route show table all, so nothing proves a VLAN leg free")
    if collided:
        raise ValueError("a trunk or VLAN leg would collide with the host: %s" % "; ".join(collided))
    for record in seen["records"]:
        if record["role"] == "trunk" and record["owned"] and record["active"] and not record["definition"]:
            quiet(record, guests(record["device"]), "reactivating it to apply its frozen definition")


def quiet(record, plugged, action):
    if plugged is None:
        raise ValueError("the hypervisor did not answer which domains run on trunk %s, so nothing proves %s disconnects none" % (
            record["device"], action))
    if plugged:
        raise ValueError("trunk %s carries %s, which still run on it, so %s would disconnect them; stop each Machine with "
                         "bootwright machine stop and repeat" % (record["device"], ", ".join(plugged), action))


def settle(runner, request, guests, conflicts, root, owner):
    seen = inspect(runner, request, root, owner)
    guard(seen, guests, conflicts)
    changed = reconcile_carrier(runner, request, legs_of(request), owner)
    for record in seen["records"]:
        if record["role"] == "trunk":
            changed = converge(runner, record, root) or changed
    # Reactivating a trunk takes its legs down with it, so the legs are read again.
    for record in inspect(runner, request, root, owner)["records"]:
        if record["role"] == "leg":
            changed = converge(runner, record, root) or changed
    after = inspect(runner, request, root, owner)
    missing = [record["name"] for record in after["records"]
               if not (record["answered"] and record["owned"] and record["active"] and record["definition"] and record["zone"] == ZONE)]
    if not after["answered"] or missing:
        raise ValueError("NetworkManager does not hold every trunk and VLAN leg active with its frozen definition in zone %s: %s" % (
            ZONE, ", ".join(missing) or "an nmcli read did not answer"))
    return changed


def link_present(device):
    return os.path.lexists(os.path.join(NET, device))


def links_gone(devices, sleep=time.sleep):
    """Whether NetworkManager removed each software device it created, polled within a bound."""
    for _attempt in range(LINK_POLLS):
        if not any(link_present(device) for device in devices):
            return True
        sleep(0.5)
    return False


def delete(runner, records, root):
    """Delete each connection's profile and keyfile, the legs before the trunks."""
    changed = False
    for role in ("leg", "trunk"):
        for record in records:
            if record["role"] != role:
                continue
            if record["present"]:
                require(runner, [NMCLI, "-w", WAIT, "connection", "delete", "uuid", record["uuid"]], "delete connection " + record["name"])
                changed = True
            path = keyfile_path(root, record)
            if (record["present"] or record.get("stale", True)) and os.path.lexists(path):
                os.unlink(path)
                changed = True
    return changed


def removable(seen, records, what):
    """Refuse a removal before any change: an unanswered read, or a profile or keyfile of an owned identity that is not this provider's."""
    foreign = [record["name"] for record in records if record["present"] and not record["owned"]]
    files = [record["name"] for record in records if record.get("foreign_file")]
    if not seen["answered"]:
        raise ValueError("the %s refuse removal before any change: NetworkManager did not answer nmcli connection show for "
                         "every profile" % what)
    if foreign:
        raise ValueError("the %s refuse removal before any change: a profile holding the UUID of %s is not this provider's" % (
            what, ", ".join(foreign)))
    if files:
        raise ValueError("the %s refuse removal before any change: the keyfile of %s is not this provider's, so its absence "
                         "cannot be proved; remove or rename that file and repeat" % (what, ", ".join(files)))


def remove_all(runner, seen, records, guests, root, what, kept=()):
    """Remove the given connections once nothing runs on a trunk among them, and prove each profile, keyfile and device gone."""
    removable(seen, records, what)
    for record in records:
        if record["role"] == "trunk" and (record["present"] or link_present(record["device"])):
            quiet(record, guests(record["device"]), "removing it")
    changed = delete(runner, records, root)
    devices = [record["device"] for record in records if record["device"] not in set(kept)]
    return changed, devices


def remove(runner, request, guests, root, owner, sleep=time.sleep):
    """Delete the legs, then the trunks, frozen or no longer frozen, once nothing runs on a trunk; prove each gone."""
    seen = inspect(runner, request, root, owner)
    changed, devices = remove_all(runner, seen, seen["records"] + seen["unfrozen"], guests, root, "trunks and VLAN legs")
    after = inspect(runner, request, root, owner)
    left = [record["name"] for record in after["records"] if record["present"] or record["file"]] + [
        record["name"] for record in after["unfrozen"]]
    if not after["answered"] or left:
        raise ValueError("NetworkManager still holds a trunk or VLAN leg after removal: %s" % (", ".join(left) or "an nmcli read did not answer"))
    if not links_gone(devices, sleep=sleep):
        raise ValueError("a trunk or VLAN leg device remains after its connection was deleted; check ip link show and repeat")
    return reconcile_carrier(runner, request, [], owner) or changed


def reconcile_carrier(runner, request, legs, owner):
    """Reload only native configuration, never DNS or connection profiles."""
    context, provider = identity(request)

    def reload_config():
        state = service_state(runner)
        if state is None:
            raise ValueError("NetworkManager did not answer before reloading VLAN carrier policy")
        if state[1] == "inactive" or (state[0] == "not-found" and not executable(NMCLI)):
            return
        if state[1] != "active":
            raise ValueError("NetworkManager must be active or stopped before changing VLAN carrier policy")
        require(runner, [NMCLI, "general", "reload", "conf"], "reload native configuration for VLAN carrier policy")

    return carrier.reconcile(context, provider, legs, reload_config, CARRIER_CONFIG, owner)


def prune(runner, request, guests, root, owner, sleep=time.sleep):
    """Delete each owned trunk and leg the request no longer freezes, the legs first, and prove each gone.

    It runs before the routing table is rewritten, so a dropped Network's leg
    never outlives the table that isolates it.
    """
    seen = inspect(runner, request, root, owner)
    if not seen["unfrozen"]:
        return False
    changed, devices = remove_all(runner, seen, seen["unfrozen"], guests, root, "trunks and VLAN legs no longer frozen",
                                  kept=[record["device"] for record in seen["records"]])
    after = inspect(runner, request, root, owner)
    if not after["answered"] or after["unfrozen"]:
        raise ValueError("NetworkManager still holds a trunk or VLAN leg the request no longer freezes: %s" % (
            ", ".join(record["name"] for record in after["unfrozen"]) or "an nmcli read did not answer"))
    if not links_gone(devices, sleep=sleep):
        raise ValueError("a trunk or VLAN leg device remains after its connection was deleted; check ip link show and repeat")
    return changed


def manage(runner, request, state, guests, conflicts, root=None, owner=0, sleep=time.sleep):
    """Hold every frozen trunk and VLAN leg, prune or remove them, refusing what is not this provider's.

    `pruned` removes only the owned connections the request no longer
    freezes; `present` does that first too, then holds the frozen ones;
    `absent` removes both. `guests(bridge)` names the domains plugged into a
    trunk, or None when the hypervisor did not answer; `conflicts(networks,
    ours)` is the host's link and route collision check over the trunks and legs.
    """
    root = CONNECTIONS if root is None else root
    context, provider = identity(request)
    carrier.require_owned(carrier.inspect(context, provider, legs_of(request), CARRIER_CONFIG, owner))
    if state == "absent":
        return remove(runner, request, guests, root, owner, sleep)
    changed = prune(runner, request, guests, root, owner, sleep)
    if state == "pruned":
        return changed
    if not wanted(request):
        return reconcile_carrier(runner, request, [], owner) or changed
    return settle(runner, request, guests, conflicts, root, owner) or changed
