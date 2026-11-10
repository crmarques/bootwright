"""The one nftables table a libvirt provider host owns: routing, NAT and isolation of its VLAN legs.

Each provider with a tagged managed Network holds one table,
`inet bootwright-<context>-<provider>-routing`, in one owned oneshot unit of
that name. The ruleset travels inside the unit as standard input, so `nft -f -`
replaces the whole table in one transaction at boot and at apply, before
NetworkManager brings any leg up, and stopping the unit deletes it. Firewalld's
own configuration is never changed: the table is an extra set of base chains
whose drop and reject verdicts are final whatever another table accepts.
"""

from __future__ import annotations

import ipaddress
import json
import os

from ansible_collections.bootwright.core.plugins.module_utils.libvirt_firewall_ready import vendor_dropins
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_nm_connections import DEVICE, identity, legs_of, trunks_of
from ansible_collections.bootwright.core.plugins.module_utils.libvirt_vlan_refusal import (
    SYSTEMCTL, SYSTEMD, WANTS, canonical_order, command, unit_files, write_unit)

NFT = "/usr/sbin/nft"
SYSCTL = "/usr/sbin/sysctl"
FORWARDING = "/proc/sys/net/ipv4/ip_forward"
MARK = "# Bootwright owned routing: "
TABLE_MARK = "Bootwright owned routing: "
MAX_OUTPUT = 1 << 20
MAX_DEVICES = 64
MAX_PREFIXES = 128
ROUTING_KEYS = frozenset(("forwarding", "keptPrefixes", "managedDevices", "natPrefixes", "routed", "unit"))
# Each base chain, its type, hook and numeric priority (filter is 0 and srcnat
# 100), and how many rules it holds.
CHAINS = {
    "input": ("filter", "input", -10, 1),
    "forward": ("filter", "forward", -10, 9),
    "postrouting": ("nat", "postrouting", 90, 1),
}


def executable(path):
    return os.path.isfile(path) and os.access(path, os.X_OK)


def canonical_prefixes(values, what, version=None, ordered=True):
    if not isinstance(values, list) or len(values) > MAX_PREFIXES:
        raise ValueError("invalid %s" % what)
    networks = []
    for value in values:
        network = ipaddress.ip_network(value, strict=True)
        if str(network) != value or network.prefixlen == 0 or (version and network.version != version):
            raise ValueError("invalid %s" % what)
        networks.append(network)
    if len(set(networks)) != len(networks) or (ordered and networks != sorted(networks, key=canonical_order)):
        raise ValueError("the %s are not canonical" % what)
    return networks


def intervals(networks):
    """The address intervals the networks cover, adjacent and overlapping ones merged, as auto-merge holds them."""
    merged = []
    for first, last in sorted((int(network.network_address), int(network.broadcast_address)) for network in networks):
        if merged and first <= merged[-1][1] + 1:
            merged[-1] = (merged[-1][0], max(merged[-1][1], last))
        else:
            merged.append((first, last))
    return merged


def routed_pairs(values, devices):
    """The declared pairs of leg devices in request order, each with its reverse declared too."""
    if not isinstance(values, list) or len(values) > len(devices) * len(devices):
        raise ValueError("invalid routed pairs")
    pairs = []
    for value in values:
        if not isinstance(value, dict) or set(value) != {"from", "to"}:
            raise ValueError("invalid routed pair")
        source, target = value["from"], value["to"]
        if source not in devices or target not in devices or source == target:
            raise ValueError("a routed pair names no two legs of the provider")
        pairs.append((source, target))
    if len(set(pairs)) != len(pairs) or any((target, source) not in pairs for source, target in pairs):
        raise ValueError("a routed pair is repeated or lacks its reverse")
    return pairs


def model(request):
    """The frozen table, or None when the provider has no VLAN leg.

    Every value is validated against the frozen trunks and legs, so the unit
    can only route, NAT and isolate what the request declares.
    """
    if not any(request.get(key) for key in ("legs", "routing", "trunks")):
        return None
    context, provider = identity(request)
    trunks = trunks_of(request)
    legs = legs_of(request, trunks)
    routing = request.get("routing")
    if not legs:
        if routing:
            raise ValueError("a routing table without a VLAN leg")
        return None
    if not isinstance(routing, dict) or not set(routing) <= ROUTING_KEYS:
        raise ValueError("invalid routing")
    unit = "bootwright-%s-%s-routing.service" % (context, provider)
    if routing.get("unit") != unit:
        raise ValueError("the routing unit does not match the frozen provider")
    forwarding = routing.get("forwarding", False)
    managed = routing.get("managedDevices") or []
    if not isinstance(forwarding, bool) or not isinstance(managed, list) or len(managed) > MAX_DEVICES:
        raise ValueError("invalid routing")
    # Each element is written into the ruleset quoted, so only an interface
    # name the request itself freezes may reach it.
    if any(not isinstance(device, str) or not DEVICE.match(device) for device in managed):
        raise ValueError("the managed devices are not interface names")
    devices = [leg["device"] for leg in legs]
    if managed != sorted({trunk["bridge"] for trunk in trunks} | set(devices) | managed_bridges(request)):
        raise ValueError("the managed devices are not exactly the provider's trunks, legs and managed bridges")
    routed = routed_pairs(routing.get("routed") or [], devices)
    nat_legs = [leg["device"] for leg in legs if leg["forward"] == "nat"]
    nat = canonical_prefixes(routing.get("natPrefixes") or [], "NAT prefixes", 4, ordered=False)
    kept = canonical_prefixes(routing.get("keptPrefixes") or [], "kept prefixes", 4)
    if set(nat) != {ipaddress.ip_interface(leg["address"]).network for leg in legs if leg["forward"] == "nat"}:
        raise ValueError("the NAT prefixes are not exactly those of the NAT legs")
    if not {ipaddress.ip_interface(leg["address"]).network for leg in legs} <= set(kept):
        raise ValueError("the kept prefixes omit a leg")
    if forwarding != bool(nat_legs or routed):
        raise ValueError("forwarding is required exactly by a NAT leg or a routed pair")
    return {
        "unit": unit, "table": unit[:-len(".service")], "forwarding": forwarding,
        "trunks": [trunk["bridge"] for trunk in trunks], "legs": devices, "nat_legs": nat_legs,
        "managed": list(managed), "routed": routed, "nat_prefixes": nat, "kept_prefixes": kept,
    }


def managed_bridges(request):
    """The bridge of every managed libvirt network the request freezes."""
    networks = request.get("networks") or []
    if not isinstance(networks, list):
        raise ValueError("invalid networks")
    found = set()
    for network in networks:
        if not isinstance(network, dict):
            raise ValueError("invalid network")
        if network.get("managed") is True:
            found.add(network.get("bridge"))
    return found


def set_line(name, kind, elements, interval=False):
    """One set on one line; a set with no element omits its elements clause."""
    clauses = ["type %s;" % kind] + (["flags interval;", "auto-merge;"] if interval else [])
    if elements:
        clauses.append("elements = { %s };" % ", ".join(elements))
    return "set %s { %s }" % (name, " ".join(clauses))


def ruleset(table):
    """The exact nft input the unit loads, one statement per line.

    The table is added and deleted first, so one transaction replaces it
    whether or not it exists. Base chain priorities run before libvirt's
    filter (0) and srcnat and firewalld's (filter + 10): a drop or reject here
    is final in any table, an accept only ends this table's chain. Forward
    order: anything untagged through a trunk drops, IPv6 to or from a leg is
    rejected, replies pass, declared pairs pass, anything new into a leg is
    rejected (a guest libvirt's own chain accepts out of a NAT network
    included), a NAT leg reaches every interface outside the provider's
    managed set except the kept prefixes, and anything else out of a leg is
    rejected. Only a NAT leg's prefix is masqueraded, and never towards a
    kept prefix or a managed interface.
    """
    name = table["table"]
    lines = ["table inet %s" % name, "delete table inet %s" % name, "table inet %s {" % name,
             'comment "%s%s"' % (TABLE_MARK, table["unit"])]
    lines += [set_line(set_name, "ifname", ['"%s"' % device for device in table[set_name]])
              for set_name in ("trunks", "legs", "nat_legs", "managed")]
    lines.append(set_line("routed", "ifname . ifname", ['"%s" . "%s"' % pair for pair in table["routed"]]))
    lines += [set_line(set_name, "ipv4_addr", [str(network) for network in table[set_name]], True)
              for set_name in ("nat_prefixes", "kept_prefixes")]
    lines += [
        "chain input {",
        "type filter hook input priority filter - 10; policy accept;",
        "iifname @trunks drop",
        "}",
        "chain forward {",
        "type filter hook forward priority filter - 10; policy accept;",
        "iifname @trunks drop",
        "oifname @trunks drop",
        "meta nfproto ipv6 iifname @legs reject",
        "meta nfproto ipv6 oifname @legs reject",
        "ct state established,related accept",
        "iifname . oifname @routed accept",
        "oifname @legs reject",
        "iifname @nat_legs oifname != @managed ip daddr != @kept_prefixes accept",
        "iifname @legs reject",
        "}",
        "chain postrouting {",
        "type nat hook postrouting priority srcnat - 10; policy accept;",
        "ip saddr @nat_prefixes ip daddr != @kept_prefixes oifname != @managed masquerade",
        "}",
        "}",
    ]
    return "\n".join(lines) + "\n"


def unit_content(table):
    """The owned unit: the ruleset as its standard input, loaded before the network comes up.

    systemd resolves C escapes and specifiers in StandardInputText=, and the
    ruleset carries neither a percent sign nor a backslash, so each line
    reaches nft as written. Every process of the unit reads that input, so a
    reload loads the same ruleset in place: nft applies it in one
    transaction, and one it refuses leaves the table it replaced loaded,
    while a stop deletes the table.
    """
    rules = ruleset(table)
    if "%" in rules or "\\" in rules:
        raise ValueError("the ruleset carries a character systemd would rewrite")
    name = table["table"]
    lines = [MARK + table["unit"], "[Unit]", "Description=Bootwright routes and isolates the VLAN legs on owned trunks",
             "Before=network-pre.target NetworkManager.service", "Wants=network-pre.target", "",
             "[Service]", "Type=oneshot", "RemainAfterExit=yes", "StandardInput=data"]
    lines += ["StandardInputText=" + line for line in rules.splitlines()]
    forwarding_line = "%s -w net.ipv4.ip_forward=1" % SYSCTL
    lines.append("ExecStart=%s -f -" % NFT)
    if table["forwarding"]:
        lines.append("ExecStartPost=" + forwarding_line)
    lines.append("ExecReload=%s -f -" % NFT)
    if table["forwarding"]:
        lines.append("ExecReload=" + forwarding_line)
    lines += ["ExecStopPost=-%s delete table inet %s" % (NFT, name), "", "[Install]", "WantedBy=multi-user.target", ""]
    return "\n".join(lines).encode()


def nft_json(runner, arguments):
    code, output, _error = command(runner, [NFT, "-j"] + arguments)
    if code != 0 or len(output) >= MAX_OUTPUT:
        return None
    try:
        items = json.loads(output or "{}").get("nftables")
    except (ValueError, AttributeError):
        return None
    return items if isinstance(items, list) else None


def table_listed(runner, name):
    """Whether the host holds an inet table of that name, or None when nft did not answer."""
    items = nft_json(runner, ["list", "tables"])
    if items is None:
        return None
    return any(isinstance(item, dict) and isinstance(item.get("table"), dict) and item["table"].get("family") == "inet"
               and item["table"].get("name") == name for item in items)


def element_value(item):
    """One listed set element: an interface name, a pair, or an address interval as integers; None for anything else."""
    if isinstance(item, str):
        try:
            address = int(ipaddress.IPv4Address(item))
        except ValueError:
            return item
        return (address, address)
    if isinstance(item, dict) and isinstance(item.get("concat"), list):
        return tuple(item["concat"])
    if isinstance(item, dict) and isinstance(item.get("prefix"), dict):
        network = ipaddress.IPv4Network("%s/%s" % (item["prefix"].get("addr"), item["prefix"].get("len")))
        return (int(network.network_address), int(network.broadcast_address))
    if isinstance(item, dict) and isinstance(item.get("range"), list) and len(item["range"]) == 2:
        return tuple(int(ipaddress.IPv4Address(address)) for address in item["range"])
    return None


def expected_sets(table):
    """Each set as nft lists it: its type, flags, auto-merge and elements, interfaces and pairs sorted."""
    found = {name: ("ifname", (), False, sorted(table[name], key=str)) for name in ("trunks", "legs", "nat_legs", "managed")}
    found["routed"] = (["ifname", "ifname"], (), False, sorted(table["routed"], key=str))
    for name in ("nat_prefixes", "kept_prefixes"):
        found[name] = ("ipv4_addr", ("interval",), True, intervals(table[name]))
    return found


def listed_set(entry):
    elements = entry.get("elem") or []
    if not isinstance(elements, list):
        elements = [elements]
    try:
        values = [element_value(item) for item in elements]
    except (TypeError, ValueError):
        return None
    if None in values:
        return None
    if entry.get("type") == "ipv4_addr":
        values = sorted(values)
    else:
        values = sorted(values, key=str)
    return entry.get("type"), tuple(entry.get("flags") or ()), entry.get("auto-merge") is True, values


def loaded(items, table):
    """Whether the listed table is exactly the frozen one: its mark, sets, chains, hooks, priorities and rule counts."""
    tables = [item["table"] for item in items if isinstance(item, dict) and isinstance(item.get("table"), dict)]
    if len(tables) != 1 or tables[0].get("comment") != TABLE_MARK + table["unit"]:
        return False
    sets, chains, rules = {}, {}, {}
    for item in items:
        if not isinstance(item, dict) or "metainfo" in item or "table" in item:
            continue
        kind, entry = next(iter(item.items())) if len(item) == 1 else ("", None)
        if not isinstance(entry, dict):
            return False
        if kind == "set":
            sets[entry.get("name")] = listed_set(entry)
        elif kind == "chain":
            chains[entry.get("name")] = (entry.get("type"), entry.get("hook"), entry.get("prio"), entry.get("policy"))
        elif kind == "rule":
            rules[entry.get("chain")] = rules.get(entry.get("chain"), 0) + 1
        else:
            return False
    if sets != expected_sets(table):
        return False
    if chains != {name: (kind, hook, prio, "accept") for name, (kind, hook, prio, _count) in CHAINS.items()}:
        return False
    return rules == {name: count for name, (_kind, _hook, _prio, count) in CHAINS.items()}


def forwarding():
    try:
        with open(FORWARDING, encoding="ascii") as source:
            return source.read(8).strip() == "1"
    except (OSError, ValueError):
        return False


def blank(name="", table=""):
    return {"name": name, "table": table, "answered": True, "present": False, "owned": False, "enabled": False,
            "state": "", "loaded": False, "forwarding": False}


def unit_state(runner, name, root):
    """The unit's active state and whether systemd loads only its own file, or None when systemd did not answer."""
    code, output, _error = command(runner, [SYSTEMCTL, "show", "--property=LoadState", "--property=ActiveState",
                                            "--property=FragmentPath", "--property=DropInPaths", name])
    values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
    if code not in (0, 4) or values.get("LoadState") not in ("loaded", "not-found"):
        return None
    state = values.get("ActiveState", "")
    if values.get("LoadState") == "not-found" and state == "inactive":
        state = ""
    own = vendor_dropins(values.get("DropInPaths")) and values.get("FragmentPath", "") in ("", os.path.join(root, name))
    return state, own, values.get("LoadState") == "loaded"


def inspect(runner, request, root=SYSTEMD, owner=0):
    """The frozen table's unit, enablement and live table, read-only, or a blank answer when there is none."""
    table = model(request)
    if table is None:
        return blank(), None
    return inspect_table(runner, table, root, owner), table


def inspect_table(runner, table, root=SYSTEMD, owner=0):
    """The unit, its enablement and the live table: `held` is whether the host lists the table at all."""
    result = blank(table["unit"])
    try:
        present, owned, current, enabled = unit_files(table["unit"], MARK, unit_content(table), root, owner)
    except OSError:
        # A symbolic link or another file that cannot be opened as a regular one is never the owned unit.
        present, owned, current, enabled = True, False, False, os.path.lexists(os.path.join(root, WANTS, table["unit"]))
    result.update(present=present or enabled, owned=owned and (present or enabled), enabled=enabled, current=current, held=False)
    shown = unit_state(runner, table["unit"], root)
    if shown is None:
        result["answered"] = False
        return result
    result["state"] = shown[0]
    if not shown[1] or (not present and shown[2]):
        result.update(present=True, owned=False)
    listed = table_listed(runner, table["table"])
    if listed is None:
        result["answered"] = False
        return result
    result["held"] = listed
    if listed:
        result.update(present=True, table=table["table"])
        items = nft_json(runner, ["list", "table", "inet", table["table"]])
        if items is None:
            result["answered"] = False
            return result
        result["loaded"] = loaded(items, table)
    result["forwarding"] = forwarding()
    return result


def forwarding_required(request):
    """Whether the frozen table needs IPv4 forwarding; a request that does not validate needs none it can prove."""
    try:
        table = model(request)
    except (ValueError, KeyError, TypeError):
        return False
    return bool(table and table["forwarding"])


def evidence(result):
    return {key: result[key] for key in ("name", "table", "answered", "present", "owned", "enabled", "state", "loaded", "forwarding")}


def observe(runner, request, root=SYSTEMD, owner=0):
    """The routing evidence and why an apply or removal would refuse."""
    try:
        result, table = inspect(runner, request, root, owner)
    except (ValueError, KeyError, TypeError, OSError):
        unit = (request.get("routing") or {}).get("unit") if isinstance(request.get("routing"), dict) else ""
        return dict(blank(unit if isinstance(unit, str) else ""), answered=False), ["the frozen routing table could not be read"]
    return evidence(result), refusals(result, table)


def refusals(result, table):
    if table is None:
        return []
    found = []
    if not executable(NFT):
        found.append("a tagged managed Network needs /usr/sbin/nft installed; Bootwright never installs it")
    if not result["answered"]:
        found.append("the host did not answer systemctl show or nft list for routing unit %s, so nothing proves it free; "
                     "restore them and repeat" % table["unit"])
    elif result["held"] and not (result["owned"] and result["state"] == "active"):
        found.append("nftables table inet %s exists while its owned unit %s does not run, so nothing proves it this provider's; "
                     "remove it with nft delete table inet %s and repeat" % (table["table"], table["unit"], table["table"]))
    elif result["present"] and not result["owned"]:
        found.append("routing unit %s is not this provider's, or carries a drop-in or another enablement; remove it and repeat" % table["unit"])
    return found


def require(runner, arguments):
    code, _output, _error = command(runner, arguments)
    if code != 0:
        raise ValueError("a routing table operation failed; check systemctl status of the unit and repeat")


def settle(runner, table, root, owner):
    """Install, enable and start the unit, or reload it while its file or live table drifted.

    A reload replaces the table in place and never stops the unit, whose stop
    deletes the table, so the legs are never left without one; only a unit
    that does not run is started.
    """
    name, path = table["unit"], os.path.join(root, table["unit"])
    before = inspect_table(runner, table, root, owner)
    changed = False
    if not before["current"]:
        write_unit(root, path, unit_content(table))
        changed = True
    if not before["enabled"]:
        wants = os.path.join(root, WANTS)
        os.makedirs(wants, mode=0o755, exist_ok=True)
        os.symlink(path, os.path.join(wants, name))
        changed = True
    if not before["current"] or not before["enabled"]:
        require(runner, [SYSTEMCTL, "daemon-reload"])
    if before["state"] != "active":
        require(runner, [SYSTEMCTL, "start", name])
        changed = True
    elif not before["current"] or not before["loaded"] or (table["forwarding"] and not before["forwarding"]):
        require(runner, [SYSTEMCTL, "reload", name])
        changed = True
    after = inspect_table(runner, table, root, owner)
    if not (after["answered"] and after["owned"] and after["enabled"] and after["state"] == "active" and after["loaded"]
            and (after["forwarding"] or not table["forwarding"])):
        raise ValueError("the host does not hold the frozen routing table %s under its owned unit" % table["table"])
    return changed


def retire(runner, table, root, owner):
    """Stop and remove the unit, delete the table it held, and prove both gone."""
    name, path = table["unit"], os.path.join(root, table["unit"])
    before = inspect_table(runner, table, root, owner)
    changed = False
    if before["state"] not in ("", "inactive"):
        require(runner, [SYSTEMCTL, "stop", name])
        changed = True
    link = os.path.join(root, WANTS, name)
    for owned_path in (link, path):
        if os.path.lexists(owned_path):
            os.unlink(owned_path)
            changed = True
    require(runner, [SYSTEMCTL, "daemon-reload"])
    if before["state"] == "failed":
        command(runner, [SYSTEMCTL, "reset-failed", name])
    if before["state"] == "active" and table_listed(runner, table["table"]):
        require(runner, [NFT, "delete", "table", "inet", table["table"]])
    after = inspect_table(runner, table, root, owner)
    if not after["answered"] or after["present"] or after["enabled"] or after["state"] or after["held"]:
        raise ValueError("routing unit %s or table inet %s remains after removal" % (name, table["table"]))
    return changed


def manage(runner, request, state, root=SYSTEMD, owner=0):
    """Hold the frozen table under its owned unit, or remove both, refusing what is not this provider's."""
    table = model(request)
    if table is None:
        return False
    result = inspect_table(runner, table, root, owner)
    found = refusals(result, table)
    if found:
        raise ValueError("the routing table refuses before any change: %s" % "; ".join(found))
    if state == "absent":
        return retire(runner, table, root, owner)
    return settle(runner, table, root, owner)
