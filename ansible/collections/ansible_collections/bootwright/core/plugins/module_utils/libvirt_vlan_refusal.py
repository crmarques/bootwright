"""The routes a libvirt provider host refuses for the VLAN subnets its bridges carry.

A managed bridge carries a tagged Network between guests only, so the host holds
no connected route for its subnet and a guest's packet for it would follow the
host's default route out through the NAT. One owned systemd unit holds an
unreachable route per subnet instead. libvirt's network XML cannot express it:
its `<route>` element needs a gateway and adds a route through that gateway.
"""

from __future__ import annotations

import hashlib
import ipaddress
import json
import os
import re
import stat
import tempfile

from ansible_collections.bootwright.core.plugins.module_utils.libvirt_firewall_ready import vendor_dropins

SYSTEMD = "/etc/systemd/system"
SYSTEMCTL = "/usr/bin/systemctl"
IP = "/usr/sbin/ip"
WANTS = "multi-user.target.wants"
MARK = "# Bootwright owned VLAN refusal: "
# The route protocol every refusing route carries, which kernel and the routing
# daemons iproute2 names leave unused, so a route of this protocol is never one
# a daemon of the host installed.
PROTOCOL = "251"
MAX_PREFIXES = 64
# The kernel keys a route by table, destination, TOS and metric, so each unit's
# routes carry a metric of their own derived from its name: two units that refuse
# one subnet hold two routes, and a unit adds, deletes and claims only its own.
METRIC_BASE = 1 << 30
METRIC_SPAN = 1 << 30
# ip -N prints a route's type and table by number.
TYPES = {"1": "unicast", "2": "local", "3": "broadcast", "4": "anycast", "5": "multicast", "6": "blackhole",
         "7": "unreachable", "8": "prohibit", "9": "throw", "10": "nat"}
TABLES = {"253": "default", "254": "main", "255": "local"}
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}
IDENTIFIER = re.compile(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")


def unit_name(request):
    """The frozen unit, or the empty string when the provider defines no managed network and no VLAN leg."""
    name = request.get("vlanRefusalUnit", "")
    expected = ""
    if any(network.get("managed") for network in request.get("networks", [])) or request.get("legs"):
        identity = request["identity"]
        for value in (identity["context"], identity["object"]):
            if not isinstance(value, str) or not IDENTIFIER.match(value):
                raise ValueError("invalid VLAN refusal unit identity")
        expected = "bootwright-%s-%s-vlan-refusal.service" % (identity["context"], identity["object"])
    if name != expected or len(name) > 255:
        raise ValueError("the VLAN refusal unit does not match the frozen provider")
    return name


def metric(name):
    """The metric every route of the named unit carries, the same on every host and build."""
    digest = hashlib.sha256(name.encode()).digest()
    return METRIC_BASE + int.from_bytes(digest[:4], "big") % METRIC_SPAN


def canonical_order(network):
    return (network.version, int(network.network_address), network.prefixlen)


def prefixes(request):
    """The frozen subnets, each canonical and the list in canonical order."""
    values = request.get("refusedPrefixes") or []
    if not isinstance(values, list) or len(values) > MAX_PREFIXES:
        raise ValueError("invalid refused VLAN subnets")
    networks = []
    for value in values:
        network = ipaddress.ip_network(value, strict=True)
        if str(network) != value or network.prefixlen == 0 or network.is_multicast or network.is_loopback:
            raise ValueError("invalid refused VLAN subnet")
        networks.append(network)
    if networks != sorted(set(networks), key=canonical_order):
        raise ValueError("the refused VLAN subnets are not canonical")
    return networks


def route_arguments(verb, network, value):
    return [IP, "-%d" % network.version, "route", verb, "unreachable", str(network), "proto", PROTOCOL, "metric", str(value)]


def unit_content(name, networks):
    value = metric(name)
    lines = [MARK + name, "[Unit]", "Description=Bootwright refuses host routing of VLAN subnets carried on managed bridges", "",
             "[Service]", "Type=oneshot", "RemainAfterExit=yes"]
    lines += ["ExecStart=" + " ".join(route_arguments("add", network, value)) for network in networks]
    lines += ["ExecStopPost=-" + " ".join(route_arguments("del", network, value)) for network in networks]
    lines += ["", "[Install]", "WantedBy=multi-user.target", ""]
    return "\n".join(lines).encode()


def command(runner, arguments):
    return runner(arguments, check_rc=False, environ_update=ENVIRONMENT)


def inspect_files(name, networks, root=SYSTEMD, owner=0):
    """Never follow the owned file or accept another enablement target.

    Returns whether the unit file exists, whether it is this block's own (any
    set of subnets), whether it holds exactly the frozen set, and whether it is
    enabled.
    """
    for directory in (root, os.path.join(root, WANTS)):
        if os.path.lexists(directory):
            info = os.lstat(directory)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != owner or stat.S_IMODE(info.st_mode) & 0o022:
                raise ValueError("unsafe VLAN refusal unit directory")
    path = os.path.join(root, name)
    present, owned, current = False, True, False
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        pass
    else:
        try:
            info = os.fstat(fd)
            present = True
            content = os.read(fd, 16385)
            owned = (stat.S_ISREG(info.st_mode) and info.st_uid == owner and info.st_nlink == 1
                     and stat.S_IMODE(info.st_mode) == 0o644 and content.startswith((MARK + name + "\n").encode()))
            current = owned and content == unit_content(name, networks)
        finally:
            os.close(fd)
    link = os.path.join(root, WANTS, name)
    enabled = os.path.lexists(link)
    if enabled:
        info = os.lstat(link)
        owned = owned and stat.S_ISLNK(info.st_mode) and info.st_uid == owner and os.readlink(link) == path
    if os.path.lexists(path + ".d"):
        present, owned = True, False
    return present, owned, current, enabled


def host_routes(runner):
    """Every IPv4 and IPv6 route of every table, or None when a listing did not answer.

    The listing is numeric, because ip prints a protocol by the name the host's
    rt_protos gives it, and protocol 251 may have one there.
    """
    found = []
    for family, width in (("-4", 32), ("-6", 128)):
        code, output, _error = command(runner, [IP, "-N", "-json", family, "route", "show", "table", "all"])
        if code != 0 or len(output) >= (1 << 20):
            return None
        try:
            for item in json.loads(output or "[]"):
                destination = str(item.get("dst") or "")
                if destination in ("", "default"):
                    destination = "0.0.0.0/0" if width == 32 else "::/0"
                if "/" not in destination:
                    destination += "/" + str(width)
                hops = item.get("nexthops") or [item]
                found.append({
                    "network": ipaddress.ip_network(destination, strict=False),
                    "type": TYPES.get(str(item.get("type") or "unicast"), str(item.get("type") or "unicast")),
                    "protocol": str(item.get("protocol") or ""),
                    "table": TABLES.get(str(item.get("table") or "main"), str(item.get("table") or "main")),
                    "metric": int(item.get("metric") or 0),
                    "devices": [str(hop.get("dev") or "") for hop in hops],
                    "gateways": [str(hop.get("gateway") or "") for hop in hops],
                })
        except (ValueError, TypeError, AttributeError):
            return None
    return found


def refusal_shaped(route):
    return route["type"] == "unreachable" and route["protocol"] == PROTOCOL and route["table"] == "main"


def is_refusal(route, networks, value):
    return refusal_shaped(route) and route["metric"] == value and route["network"] in networks


def held(routes, networks, value):
    return sorted({route["network"] for route in routes if is_refusal(route, networks, value)}, key=canonical_order)


def conflicts(routes, networks, value, running):
    """What the host already routes inside or equal to a refused subnet, by name.

    A route in the main table through a gateway on every hop that is strictly
    broader than the subnet, the default route for one, is what the refusal
    exists to override, so it is admitted. Another unit's refusing route, of
    protocol 251 in the main table under another metric, refuses the same
    traffic and has its own kernel key, so it coexists. A refusing route under
    this unit's metric is its own only while the unit is owned and running;
    otherwise nothing proves whose it is. Every other overlapping route is
    foreign and is refused, whatever its table or type, because replacing or
    shadowing it would change what the host reaches.
    """
    found = []
    for network in networks:
        for route in routes:
            other = route["network"]
            if other.version != network.version or not other.overlaps(network):
                continue
            if refusal_shaped(route) and (route["metric"] != value or running):
                continue
            main = route["table"] == "main"
            if main and route["type"] == "unicast" and other.prefixlen < network.prefixlen and all(route["gateways"]):
                continue
            found.append("%s route %s dev %s in table %s overlaps the VLAN subnet %s a managed bridge carries" % (
                route["type"], other, ",".join(device for device in route["devices"] if device) or "none", route["table"], network))
    return sorted(set(found))


def observe(runner, request, root=SYSTEMD, owner=0):
    name = request.get("vlanRefusalUnit", "")
    result = {"name": name, "answered": True, "present": False, "owned": False, "enabled": False, "state": "", "routes": []}
    try:
        name = unit_name(request)
        networks = prefixes(request)
    except (ValueError, KeyError, TypeError):
        result["answered"] = False
        return result
    if not name:
        return result
    try:
        present, owned, _current, enabled = inspect_files(name, networks, root, owner)
        result.update(present=present or enabled, owned=owned and (present or enabled), enabled=enabled)
        code, output, _error = command(runner, [SYSTEMCTL, "show", "--property=LoadState", "--property=ActiveState",
                                                "--property=FragmentPath", "--property=DropInPaths", name])
        values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
        if code not in (0, 4) or values.get("LoadState") not in ("loaded", "not-found"):
            result["answered"] = False
        else:
            result["state"] = values.get("ActiveState", "")
            if values.get("LoadState") == "not-found" and result["state"] == "inactive":
                result["state"] = ""
            fragment = values.get("FragmentPath", "")
            if not vendor_dropins(values.get("DropInPaths")) or fragment not in ("", os.path.join(root, name)):
                result.update(present=True, owned=False)
            if not present and values.get("LoadState") == "loaded":
                result.update(present=True, owned=False)
        routes = host_routes(runner)
        if routes is None:
            result["answered"] = False
        else:
            result["routes"] = [str(network) for network in held(routes, networks, metric(name))]
    except (OSError, ValueError):
        result["answered"] = False
    return result


def require(runner, arguments):
    code, _output, _error = command(runner, arguments)
    if code != 0:
        raise ValueError("a VLAN refusal operation failed; check systemctl status of the unit and repeat")


def write_unit(root, path, content):
    fd, stage = tempfile.mkstemp(prefix=".bootwright-vlan-", dir=root)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(content)
            stream.flush()
            os.fchmod(stream.fileno(), 0o644)
            os.fsync(stream.fileno())
        os.replace(stage, path)
    finally:
        if os.path.lexists(stage):
            os.unlink(stage)


def settle(runner, request, root, owner, path):
    """Install and start the unit, or restore its routes, once nothing foreign overlaps."""
    networks = prefixes(request)
    name = unit_name(request)
    present, _owned, current, enabled = inspect_files(name, networks, root, owner)
    before = observe(runner, request, root, owner)
    active = before["state"] == "active"
    changed = False
    if present and active and not current:
        require(runner, [SYSTEMCTL, "stop", name])
        active, changed = False, True
    if not current:
        write_unit(root, path, unit_content(name, networks))
        changed = True
    reload = not current or not enabled
    if not enabled:
        wants = os.path.join(root, WANTS)
        os.makedirs(wants, mode=0o755, exist_ok=True)
        if not stat.S_ISDIR(os.lstat(wants).st_mode):
            raise ValueError("the multi-user wants directory is not a directory")
        os.symlink(path, os.path.join(wants, name))
        changed = True
    if reload:
        require(runner, [SYSTEMCTL, "daemon-reload"])
    if not active:
        require(runner, [SYSTEMCTL, "start", name])
        changed = True
    elif before["routes"] != [str(network) for network in networks]:
        require(runner, [SYSTEMCTL, "restart", name])
        changed = True
    after = observe(runner, request, root, owner)
    if not (after["answered"] and after["owned"] and after["enabled"] and after["state"] == "active"
            and after["routes"] == [str(network) for network in networks]):
        raise ValueError("the host does not hold every unreachable route for the VLAN subnets its managed bridges carry")
    return changed


def retire(runner, request, root, owner, path):
    name = unit_name(request)
    before = observe(runner, request, root, owner)
    changed = False
    if before["state"] not in ("", "inactive"):
        require(runner, [SYSTEMCTL, "stop", name])
        changed = True
    link = os.path.join(root, WANTS, name)
    if os.path.lexists(link):
        os.unlink(link)
        changed = True
    if os.path.lexists(path):
        os.unlink(path)
        changed = True
    require(runner, [SYSTEMCTL, "daemon-reload"])
    if before["state"] == "failed":
        command(runner, [SYSTEMCTL, "reset-failed", name])
    return changed


def manage(runner, request, state, root=SYSTEMD, owner=0):
    """Hold the refusing routes of the frozen subnets, or remove the unit and them.

    A frozen request with no subnet holds none, so an apply that stops refusing
    a subnet removes the unit it installed earlier.
    """
    name = unit_name(request)
    if not name:
        return False
    networks = prefixes(request)
    before = observe(runner, request, root, owner)
    if not before["answered"] or (before["present"] and not before["owned"]):
        raise ValueError("the host did not answer for the VLAN refusal unit or its routes, or the unit or its enablement is unknown or foreign: %s" % name)
    path = os.path.join(root, name)
    if state == "absent" or not networks:
        return retire(runner, request, root, owner, path)
    routes = host_routes(runner)
    if routes is None:
        raise ValueError("the host did not answer ip route show table all, so nothing proves no route overlaps a VLAN subnet to refuse")
    found = conflicts(routes, networks, metric(name), before["present"] and before["owned"] and before["state"] == "active")
    if found:
        raise ValueError("the host already routes a VLAN subnet a managed bridge carries, and a route this block does not own is never "
                         "replaced: %s; change the Network's cidr or remove the route, then repeat the apply" % "; ".join(found))
    return settle(runner, request, root, owner, path)
