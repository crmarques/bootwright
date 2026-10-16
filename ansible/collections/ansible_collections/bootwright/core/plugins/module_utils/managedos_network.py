"""Decide and verify post-install network convergence of a managed OS.

The convergence reads one guest state, decides the one next step of the
interruption matrix and, once applied, compares the running network with the
declared subset. Everything outside that subset nmstate itself verifies once
at apply: by default it compares the applied state with the desired state and
rolls back otherwise (nmstatectl(8)).

Every pattern is applied with fullmatch, so a value is accepted whole or not
at all. Nothing here reads a file, runs a command or raises on its input.
"""

from __future__ import annotations

import ipaddress
import json
import re
import shlex

CHECKPOINT = r"/org/freedesktop/NetworkManager/Checkpoint/[0-9]+"
BOOT_ID = r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
DIGEST = r"[0-9a-f]{64}"
RUNNING = ("active", "activating", "deactivating", "reloading", "refreshing")
STEPS = ("inspect", "reboot", "wait", "rollback", "apply", "unreadable")
KEYS = ("nmstatectl", "bootId", "unit", "rc", "checkpoint", "listed", "marker", "error")
MAX_DIFFERENCES = 16
MAX_DIFFERENCE_BYTES = 200
MAX_RUNNING_BYTES = 1 << 20
MAX_UNIT = 32
MAX_ERROR = 512
MAX_RC_DIGITS = 12
MAIN_TABLE = 254
LINK_LOCAL = ipaddress.ip_network("fe80::/10")
# nmstate reads a bond mode, and each enumerated bond option, by its name, an
# alias or its position, and shows only the name (nmstate
# rust/src/lib/ifaces/bond.rs), so both sides compare by that name.
BOND_MODES = ("balance-rr", "active-backup", "balance-xor", "broadcast", "802.3ad", "balance-tlb", "balance-alb")
BOND_MODE_ALIASES = {"lacp": "802.3ad"}
BOND_OPTION_NAMES = {
    "ad_select": ("stable", "bandwidth", "count"),
    "all_slaves_active": ("dropped", "delivered"),
    "arp_all_targets": ("any", "all"),
    "arp_validate": ("none", "active", "backup", "all", "filter", "filter_active", "filter_backup"),
    "fail_over_mac": ("none", "active", "follow"),
    "lacp_rate": ("slow", "fast"),
    "primary_reselect": ("always", "better", "failure"),
    "xmit_hash_policy": ("layer2", "layer3+4", "layer2+3", "encap2+3", "encap3+4", "vlan+srcmac"),
}
BOND_BOOLEAN_OPTIONS = ("balance_slb", "lacp_active", "tlb_dynamic_lb", "use_carrier")
BOND_OPTION_KEYS = {"balance-slb": "balance_slb"}
TRUE_WORDS = ("1", "true", "yes", "on", "y")
FALSE_WORDS = ("0", "false", "no", "off", "n")


def checkpoint_from_log(text):
    """The checkpoint path the last line of the read names, or ''."""
    if not isinstance(text, str):
        return ""
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    if lines and re.fullmatch(CHECKPOINT, lines[-1]):
        return lines[-1]
    return ""


def listed_checkpoints(text):
    """The sorted checkpoints busctl listed as an 'ao' array, or None."""
    if not isinstance(text, str):
        return None
    try:
        tokens = shlex.split(text)
    except ValueError:
        return None
    if len(tokens) < 2 or tokens[0] != "ao" or not re.fullmatch(r"[0-9]+", tokens[1]):
        return None
    paths = tokens[2:]
    if int(tokens[1]) != len(paths):
        return None
    if not all(re.fullmatch(CHECKPOINT, path) for path in paths):
        return None
    return sorted(paths)


def parse_marker(text):
    """The convergence marker {bootId, request}, or {} for anything else."""
    if not isinstance(text, str):
        return {}
    try:
        value = json.loads(text)
    except (ValueError, TypeError, RecursionError):
        return {}
    if not isinstance(value, dict) or sorted(value) != ["bootId", "request"]:
        return {}
    boot, request = value["bootId"], value["request"]
    if not isinstance(boot, str) or not isinstance(request, str):
        return {}
    if not re.fullmatch(BOOT_ID, boot) or not re.fullmatch(DIGEST, request):
        return {}
    return {"bootId": boot, "request": request}


def neutral_state(reason):
    """An invalid state that still carries every key."""
    return {
        "valid": False, "reason": reason, "nmstatectl": False, "bootId": "", "unit": "",
        "running": False, "rc": None, "checkpoint": "", "listed": None, "marker": {}, "error": "",
    }


def read_pairs(stdout):
    """The key=value pairs of a read, or the reason it is malformed."""
    if not isinstance(stdout, str):
        return None, "the network state read is not text"
    lines = stdout.split("\n")
    if lines and lines[-1] == "":
        lines.pop()
    pairs = {}
    for line in lines:
        key, separator, value = line.partition("=")
        if not separator:
            return None, "the network state read carries a line that is not key=value"
        if key not in KEYS:
            return None, "the network state read carries an unknown key %s" % printable_text(key)[:32]
        if key in pairs:
            return None, "the network state read repeats %s" % key
        pairs[key] = value
    for key in KEYS:
        if key not in pairs:
            return None, "the network state read is missing %s" % key
    return pairs, ""


def guest_state(stdout):
    """The guest's convergence state parsed from one bounded read."""
    pairs, reason = read_pairs(stdout)
    if pairs is None:
        return neutral_state(reason)
    if not re.fullmatch(BOOT_ID, pairs["bootId"]):
        return neutral_state("the network state read has a bootId that is not a boot id")
    if len(pairs["unit"]) > MAX_UNIT:
        return neutral_state("the network state read has a unit state longer than %d characters" % MAX_UNIT)
    rc = None
    if pairs["rc"] != "":
        if not re.fullmatch(r"-?[0-9]{1,%d}" % MAX_RC_DIGITS, pairs["rc"]):
            return neutral_state("the network state read has an rc that is not an integer")
        rc = int(pairs["rc"])
    return {
        "valid": True,
        "reason": "",
        "nmstatectl": pairs["nmstatectl"] == "yes",
        "bootId": pairs["bootId"],
        "unit": pairs["unit"],
        "running": pairs["unit"] in RUNNING,
        "rc": rc,
        "checkpoint": checkpoint_from_log(pairs["checkpoint"]),
        "listed": listed_checkpoints(pairs["listed"]),
        "marker": parse_marker(pairs["marker"]),
        "error": printable_text(pairs["error"])[:MAX_ERROR],
    }


def next_step(state, digest):
    """The one next step of the interruption matrix for a state and request digest."""
    if not isinstance(state, dict) or state.get("valid") is not True:
        return "unreadable"
    marker = state.get("marker")
    if isinstance(marker, dict) and marker.get("request") and marker.get("request") == digest:
        return "inspect" if marker.get("bootId") != state.get("bootId") else "reboot"
    if state.get("running"):
        return "wait"
    checkpoint = state.get("checkpoint")
    if checkpoint:
        listed = state.get("listed")
        if not isinstance(listed, list):
            return "unreadable"
        if checkpoint in listed:
            return "rollback"
    return "apply"


def printable_text(text):
    """The printable characters of a text."""
    return "".join(character for character in str(text) if character.isprintable())


def text(value):
    """A value as the text a difference names it by."""
    if value is None:
        return "absent"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float, str)):
        return str(value)
    if isinstance(value, list):
        return "[" + ", ".join(text(item) for item in value) + "]"
    return json.dumps(value, sort_keys=True, default=str)


def as_int(value):
    """An integer value, or None."""
    if isinstance(value, bool):
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def canonical_ip(value):
    """An address in its canonical text, or the value's own text."""
    try:
        return str(ipaddress.ip_address(str(value)))
    except ValueError:
        return text(value)


def canonical_network(value):
    """A destination in its canonical network text, or the value's own text."""
    try:
        return str(ipaddress.ip_network(str(value), strict=False))
    except ValueError:
        return text(value)


def mapping(value):
    return value if isinstance(value, dict) else {}


def entries(value):
    return [item for item in value if isinstance(item, dict)] if isinstance(value, list) else []


def is_link_local(address):
    try:
        parsed = ipaddress.ip_address(address)
    except ValueError:
        return False
    return parsed.version == 6 and parsed in LINK_LOCAL


def address_set(family, drop_link_local):
    found = set()
    for entry in entries(family.get("address")):
        ip = canonical_ip(entry.get("ip"))
        if drop_link_local and is_link_local(ip):
            continue
        found.add((ip, as_int(entry.get("prefix-length"))))
    return found


def address_text(found):
    return "[" + ", ".join("%s/%s" % (ip, text(prefix)) for ip, prefix in sorted(found, key=str)) + "]"


def family_differences(name, family, desired, running):
    """The differences of one address family of one interface."""
    found = []
    keys = ("enabled", "dhcp", "autoconf") if family == "ipv6" else ("enabled", "dhcp")
    for key in keys:
        if key in desired and desired[key] != running.get(key):
            found.append("interface %s: %s %s is %s, declared %s" % (
                name, family, key, text(running.get(key)), text(desired[key])))
    dynamic = desired.get("dhcp") is True or (family == "ipv6" and desired.get("autoconf") is True)
    if "address" in desired and not dynamic:
        wanted = address_set(desired, False)
        have = address_set(running, family == "ipv6")
        if wanted != have:
            found.append("interface %s: %s addresses are %s, declared %s" % (
                name, family, address_text(have), address_text(wanted)))
    return found


def port_set(aggregation):
    ports = aggregation.get("port")
    if ports is None:
        ports = aggregation.get("ports")
    return sorted(set(text(port) for port in ports)) if isinstance(ports, list) else None


def enumerated(value, names, aliases=None):
    """An enumerated value by the name nmstate shows: a position or alias
    becomes its name, and anything else keeps its own text."""
    word = text(value)
    if word in (aliases or {}):
        return aliases[word]
    if re.fullmatch(r"[0-9]+", word) and int(word) < len(names):
        return names[int(word)]
    return word


def bond_mode(value):
    return enumerated(value, BOND_MODES, BOND_MODE_ALIASES)


def bond_option(key, value):
    """A bond option's value by the text nmstate shows it as."""
    if key in BOND_OPTION_NAMES:
        return enumerated(value, BOND_OPTION_NAMES[key])
    if key in BOND_BOOLEAN_OPTIONS and not isinstance(value, bool):
        word = text(value).lower()
        if word in TRUE_WORDS:
            return "true"
        if word in FALSE_WORDS:
            return "false"
    return text(value)


def bond_options(value):
    """Bond options by the key nmstate shows each under."""
    return {BOND_OPTION_KEYS.get(key, key): option for key, option in mapping(value).items()}


def bond_differences(name, desired, running):
    """The differences of one bond's declared link aggregation."""
    found = []
    want, have = mapping(desired.get("link-aggregation")), mapping(running.get("link-aggregation"))
    if "mode" in want and bond_mode(want["mode"]) != bond_mode(have.get("mode")):
        found.append("interface %s: link-aggregation mode is %s, declared %s" % (
            name, text(have.get("mode")), text(want["mode"])))
    wanted_ports = port_set(want)
    if wanted_ports is not None and wanted_ports != port_set(have):
        found.append("interface %s: link-aggregation ports are %s, declared %s" % (
            name, text(port_set(have)), text(wanted_ports)))
    have_options = bond_options(have.get("options"))
    for key, value in bond_options(want.get("options")).items():
        if bond_option(key, value) != bond_option(key, have_options.get(key)):
            found.append("interface %s: link-aggregation option %s is %s, declared %s" % (
                name, key, text(have_options.get(key)), text(value)))
    return found


def vlan_differences(name, desired, running):
    """The differences of one vlan's declared base, id and protocol."""
    found = []
    want, have = mapping(desired.get("vlan")), mapping(running.get("vlan"))
    if text(want.get("base-iface")) != text(have.get("base-iface")):
        found.append("interface %s: vlan base-iface is %s, declared %s" % (
            name, text(have.get("base-iface")), text(want.get("base-iface"))))
    if as_int(want.get("id")) is None or as_int(want.get("id")) != as_int(have.get("id")):
        found.append("interface %s: vlan id is %s, declared %s" % (name, text(have.get("id")), text(want.get("id"))))
    if "protocol" in want and text(want["protocol"]) != text(have.get("protocol")):
        found.append("interface %s: vlan protocol is %s, declared %s" % (
            name, text(have.get("protocol")), text(want["protocol"])))
    return found


def interface_differences(name, desired, running):
    """The differences of one interface declared up."""
    found = []
    kind = desired.get("type")
    if "type" in desired and text(kind) != text(running.get("type")):
        found.append("interface %s: type is %s, declared %s" % (name, text(running.get("type")), text(kind)))
    if running.get("state") != "up":
        found.append("interface %s: state is %s, declared up" % (name, text(running.get("state"))))
    if "mtu" in desired and (as_int(desired["mtu"]) is None or as_int(desired["mtu"]) != as_int(running.get("mtu"))):
        found.append("interface %s: mtu is %s, declared %s" % (name, text(running.get("mtu")), text(desired["mtu"])))
    for family in ("ipv4", "ipv6"):
        if isinstance(desired.get(family), dict):
            found.extend(family_differences(name, family, desired[family], mapping(running.get(family))))
    if kind == "bond":
        found.extend(bond_differences(name, desired, running))
    if kind == "vlan":
        found.extend(vlan_differences(name, desired, running))
    return found


def running_interface(candidates, kind):
    for candidate in candidates:
        if candidate.get("type") == kind:
            return candidate
    return candidates[0] if candidates else None


def interfaces_differences(desired, running):
    """The differences of every declared interface, in declared order."""
    by_name = {}
    for entry in running:
        by_name.setdefault(text(entry.get("name")), []).append(entry)
    found = []
    for interface in desired:
        name = text(interface.get("name"))
        state = interface.get("state", "up")
        if state == "absent":
            if name in by_name:
                found.append("interface %s: declared absent but present" % name)
            continue
        if state != "up":
            continue
        match = running_interface(by_name.get(name, []), interface.get("type"))
        if match is None:
            found.append("interface %s: not present" % name)
            continue
        found.extend(interface_differences(name, interface, match))
    return found


def next_hop(entry):
    value = entry.get("next-hop-address")
    return canonical_ip(value) if value not in (None, "") else ""


def route_matches(want, have):
    if canonical_network(want.get("destination")) != canonical_network(have.get("destination")):
        return False
    if "next-hop-interface" in want and text(want["next-hop-interface"]) != text(have.get("next-hop-interface")):
        return False
    if next_hop(want) != next_hop(have):
        return False
    if "metric" in want and as_int(want["metric"]) != as_int(have.get("metric")):
        return False
    if "table-id" in want:
        return as_int(want["table-id"]) == as_int(have.get("table-id"))
    return have.get("table-id") is None or as_int(have.get("table-id")) == MAIN_TABLE


def route_text(entry):
    destination = entry.get("destination")
    interface = entry.get("next-hop-interface")
    return "route %s via %s on %s" % (
        canonical_network(destination) if destination is not None else "-",
        next_hop(entry) or "-",
        text(interface) if interface not in (None, "") else "-",
    )


def routes_differences(desired, running):
    """The declared routes not configured and the configured ones not declared."""
    names = set(text(item.get("name")) for item in entries(desired.get("interfaces")) if item.get("state") != "absent")
    wanted = [item for item in entries(mapping(desired.get("routes")).get("config")) if item.get("state") != "absent"]
    have = [item for item in entries(mapping(running.get("routes")).get("config"))
            if text(item.get("next-hop-interface")) in names]
    unmatched = []
    for want in wanted:
        index = next((position for position, item in enumerate(have) if route_matches(want, item)), None)
        if index is None:
            unmatched.append(want)
        else:
            have.pop(index)
    found = ["%s: not configured" % route_text(item) for item in unmatched]
    found.extend("%s: configured but not declared" % route_text(item) for item in have)
    return found


def dns_differences(desired, running):
    """The declared resolver servers and search list, each in order."""
    want = mapping(mapping(desired.get("dns-resolver")).get("config"))
    have = mapping(mapping(running.get("dns-resolver")).get("config"))
    found = []
    for key, render in (("server", canonical_ip), ("search", text)):
        if key not in want:
            continue
        wanted, present = ordered(want[key], render), ordered(have.get(key), render)
        if wanted != present:
            found.append("dns %s is %s, declared %s" % (key, text(present), text(wanted)))
    return found


def ordered(value, render):
    return [render(item) for item in value] if isinstance(value, list) else []


def compare(desired, running):
    """Every difference of the running network from the declared subset, unbounded."""
    desired = mapping(desired)
    if not isinstance(running, dict) or not isinstance(running.get("interfaces"), list):
        return ["the running network lists no interfaces"]
    found = interfaces_differences(entries(desired.get("interfaces")), entries(running["interfaces"]))
    found.extend(routes_differences(desired, running))
    found.extend(dns_differences(desired, running))
    return found


def bounded_differences(differences):
    """The first differences, each printable ASCII and bounded in bytes."""
    bounded = []
    for difference in list(differences)[:MAX_DIFFERENCES]:
        cleaned = "".join(character if " " <= character <= "~" else "?" for character in str(difference))
        bounded.append(cleaned[:MAX_DIFFERENCE_BYTES])
    return bounded
