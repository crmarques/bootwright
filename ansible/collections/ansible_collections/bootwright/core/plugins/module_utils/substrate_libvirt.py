"""Bounded host observation and argument vectors for one libvirt substrate.

Every effect reaches the hypervisor through virsh with an exact argument
vector: the module chooses no target, workflow or implementation, and the
frozen Go request alone decides what is realized.
"""

from __future__ import annotations

import ipaddress
import json
import os
import sys
from xml.etree import ElementTree

VIRSH = "/usr/bin/virsh"
QEMU_IMG = "/usr/bin/qemu-img"
SYSTEMCTL = "/usr/bin/systemctl"
PODMAN = "/usr/bin/podman"
RPM = "/usr/bin/rpm"

MAX_OUTPUT = 1 << 20
UNIT_STATES = ("active", "activating", "deactivating", "inactive", "failed")
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}

# DOMAIN_STATES is libvirt's own vocabulary for what a domain is doing. Only
# `shut off` means removing it interrupts nothing: a paused, suspended or
# crashed domain still holds the memory and disks it was given.
DOMAIN_STATES = ("running", "idle", "paused", "in shutdown", "shut off", "crashed", "pmsuspended")

# OWNERSHIP is the metadata namespace a realized object carries. A same-named
# object without it is foreign and is never changed or removed.
OWNERSHIP = "https://bootwright.io/substrate/v1"

# NETWORK_BRIDGE is what the network template sets on a managed bridge besides
# its name: the firewall zone its guests reach the controller's services
# through, and spanning tree on with no forwarding delay.
NETWORK_BRIDGE = {"zone": "trusted", "stp": "on", "delay": "0"}

# LOOKUP_REFUSED is how virsh reports that the connection opened and the name
# lookup failed. Every domain command resolves its argument through
# virshLookupDomainInternal, which discards libvirt's own reason and exits 1
# with `error: failed to get domain '<name>'`, while a connection that never
# opened reports `failed to connect to the hypervisor` instead
# (https://gitlab.com/libvirt/libvirt/-/blob/master/tools/virsh-util.c and
# tools/virsh.c). The message therefore proves the hypervisor was reached, not
# why the lookup failed, so a domain is only ever undefined when a listing the
# hypervisor completed does not name it either.
LOOKUP_REFUSED = "failed to get domain"

# SOCKET_TABLES are the kernel's TCP socket tables of PID 1's network namespace,
# with the hexadecimal digits each local address takes. The controller's quadlet
# unit runs with host networking, so its socket lives there whatever namespace
# this module runs in. The kernel lists every listening socket before any other
# entry (Documentation/networking/proc_net_tcp.rst, https://git.kernel.org/pub/
# scm/linux/kernel/git/torvalds/linux.git/tree/Documentation/networking/
# proc_net_tcp.rst?h=v6.18), so a table is read only as far as its first entry
# that is not listening.
SOCKET_TABLES = (("/proc/1/net/tcp", 8), ("/proc/1/net/tcp6", 32))
TCP_LISTEN = 0x0A
HEX_DIGITS = frozenset("0123456789ABCDEFabcdef")
ANY_IPV4 = ipaddress.IPv4Address("0.0.0.0")
ANY_IPV6 = ipaddress.IPv6Address("::")


def invoke(runner, argv, limit=MAX_OUTPUT):
    """Run one pinned executable with an exact argument vector and no shell."""
    if not argv or not os.path.isabs(argv[0]):
        raise ValueError("executable")
    code, out, _err = runner(argv, check_rc=False, environ_update=ENVIRONMENT)
    return code, out[:limit]


def virsh(runner, uri, *arguments):
    return invoke(runner, [VIRSH, "--connect", uri] + [str(value) for value in arguments])


def virsh_reason(runner, uri, *arguments):
    """Run virsh and keep the diagnosis it writes to standard error.

    A caller that reports an unknown answer rather than an error has nothing
    else to report with, because the refusal is never on standard output.
    """
    argv = [VIRSH, "--connect", uri] + [str(value) for value in arguments]
    code, out, err = runner(argv, check_rc=False, environ_update=ENVIRONMENT)
    return code, out[:MAX_OUTPUT], err[:MAX_OUTPUT]


def packages_present(runner, names):
    """Report whether every package of the closure is installed, by name."""
    for name in names:
        code, _output = invoke(runner, [RPM, "--query", str(name)])
        if code != 0:
            return False
    return bool(names)


def unit_state(runner, service):
    """Report the unit's state, or the empty string when it is not defined.

    systemd answers `inactive` for a unit it has never heard of, so the load
    state is what separates a unit that is stopped from one that is gone. A
    removal proves absence through this, and `inactive` would never prove it.
    """
    _code, shown = invoke(runner, [SYSTEMCTL, "show", "--property=LoadState", "--property=ActiveState", service], 256)
    properties = dict(line.split("=", 1) for line in shown.splitlines() if "=" in line)
    if properties.get("LoadState", "").strip() == "not-found":
        return ""
    active = properties.get("ActiveState", "").strip()
    return active if active in UNIT_STATES else ""


def unit_enabled(runner, service):
    """Report whether the unit starts with the host.

    A libvirt driver that only answers because something woke its socket takes
    the networks and pools it owns down with it, so what the host does at boot
    is the question, not whether the daemon happens to be running now.
    """
    _code, shown = invoke(runner, [SYSTEMCTL, "is-enabled", service], 256)
    return shown.strip() == "enabled"


def uri_answers(runner, uri):
    code, _output = virsh(runner, uri, "version")
    return code == 0


def listed(runner, uri, command, name):
    """Report whether a driver's own listing names the object, or None when it is silent.

    Each listing exits 0 only after it has collected and printed every object,
    one name per line, so its answer is complete or it is no answer at all. An
    output the bound truncated could have dropped the name, so it is none.
    """
    code, output = virsh(runner, uri, command, "--all", "--name")
    if code != 0 or len(output) >= MAX_OUTPUT:
        return None
    return name in [line.strip() for line in output.splitlines()]


def same_address(element, host, prefix):
    """Whether one `ip` element holds exactly this host address and prefix."""
    try:
        return (
            ipaddress.ip_address(element.get("address") or "") == ipaddress.ip_address(host)
            and int(element.get("prefix") or "") == int(prefix)
        )
    except ValueError:
        return False


def carries_definition(root, network, context):
    """Whether one network definition carries everything the request froze for it.

    Only what the network template writes is compared: the bridge with its
    zone and spanning tree, the forward mode or none, the resolver off, the one
    host address with its prefix and no DHCP, and the ownership naming this
    context and network. libvirt adds elements of its own, such as a UUID and a
    bridge MAC address, which are not compared, and formats each compared value
    as it was defined (virNetworkDefParseXML and virNetworkDefFormatBuf in
    https://gitlab.com/libvirt/libvirt/-/blob/master/src/conf/network_conf.c).
    """
    bridge = root.find("./bridge")
    if bridge is None or bridge.get("name") != network.get("bridge"):
        return False
    if any(bridge.get(name) != value for name, value in NETWORK_BRIDGE.items()):
        return False
    forward = root.find("./forward")
    if (forward.get("mode") if forward is not None else "none") != str(network.get("forward") or "none"):
        return False
    resolver = root.find("./dns")
    if resolver is None or resolver.get("enable") != "no":
        return False
    addresses = root.findall("./ip")
    host, _separator, prefix = str(network.get("address") or "").partition("/")
    if len(addresses) != 1 or not same_address(addresses[0], host, prefix) or addresses[0].find("./dhcp") is not None:
        return False
    owner = root.find("./metadata/{%s}owner" % OWNERSHIP)
    if owner is None:
        return False
    return (
        (owner.findtext("{%s}context" % OWNERSHIP) or "").strip() == context
        and (owner.findtext("{%s}attachment" % OWNERSHIP) or "").strip() == network.get("name")
    )


def keeps_definition(runner, uri, name, network, context):
    """Whether the definition libvirt starts the network from next carries it too.

    Defining an active network leaves the running definition alone and keeps
    the new one for when the network next starts (virNetworkObjUpdateAssignDef,
    https://gitlab.com/libvirt/libvirt/-/blob/master/src/conf/virnetworkobj.c),
    and `--inactive` reads that one (tools/virsh-network.c).
    """
    code, output = virsh(runner, uri, "net-dumpxml", "--inactive", name)
    if code != 0:
        return False
    try:
        return carries_definition(ElementTree.fromstring(output), network, context)
    except ElementTree.ParseError:
        return False


def network_state(runner, uri, name, frozen=None, context=""):
    """Report one network's state, ownership, bridge, identity and definition, unchanged.

    The UUID is read because libvirt refuses to define a name that already
    exists under a different one, so a repeated apply can only redefine a
    network by offering back the identity the host already carries.

    `definition` is whether the network runs, and keeps for its next start,
    everything the `frozen` request entry sets for this `context`, so a replay
    defines nothing a network already carries and a redefinition is proved.

    A network is read through the network driver, a daemon of its own that may
    be silent while the hypervisor the uri names answers, and virsh reports a
    lookup it failed the same way whatever the cause. `answered` is therefore
    true only when the driver returned the definition, or completed a listing
    that does not name the network, because only that proves it absent.
    """
    unanswered = {"answered": False, "definition": False, "state": "", "owned": False, "bridge": "", "uuid": ""}
    code, output = virsh(runner, uri, "net-dumpxml", name)
    if code != 0:
        return dict(unanswered, answered=listed(runner, uri, "net-list", name) is False)
    owned, bridge, uuid = False, "", ""
    try:
        root = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return unanswered
    for metadata in root.findall("./metadata/"):
        owned = owned or metadata.tag.startswith("{" + OWNERSHIP + "}")
    element = root.find("./bridge")
    if element is not None:
        bridge = element.get("name") or ""
    element = root.find("./uuid")
    if element is not None:
        uuid = (element.text or "").strip()
    code, active = virsh(runner, uri, "net-info", name)
    state = "active" if code == 0 and "Active:         yes" in active else "inactive"
    definition = (
        frozen is not None
        and carries_definition(root, frozen, context)
        and keeps_definition(runner, uri, name, frozen, context)
    )
    return {"answered": True, "definition": definition, "state": state, "owned": owned, "bridge": bridge, "uuid": uuid}


def bridge_present(name):
    return os.path.isdir("/sys/class/net/" + str(name))


def pool_state(runner, uri, name):
    """Report the pool's state and whether the storage driver answered for it.

    The storage driver is a daemon of its own too, so a pool it did not answer
    for reports no state without being absent, exactly as a network does.
    """
    code, output = virsh(runner, uri, "pool-info", name)
    if code != 0:
        return {"answered": listed(runner, uri, "pool-list", name) is False, "state": ""}
    return {"answered": True, "state": "active" if "State:          running" in output else "inactive"}


def domain_listed(runner, uri, name):
    """Report whether the hypervisor lists the domain, or None when it is silent."""
    return listed(runner, uri, "list", name)


def domain_metadata(runner, uri, name):
    """Report one domain's ownership and identity without changing it.

    `answered` separates a hypervisor that says it defines no such domain from
    one that did not answer, because only the first proves the domain absent.
    A domain that is not present and not answered for may be running.
    """
    code, output, err = virsh_reason(runner, uri, "dumpxml", name)
    if code != 0:
        undefined = LOOKUP_REFUSED in err and domain_listed(runner, uri, name) is False
        return {"answered": undefined, "present": False, "owned": False, "uuid": ""}
    try:
        root = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return {"answered": True, "present": True, "owned": False, "uuid": ""}
    owned = False
    for metadata in root.findall("./metadata/"):
        owned = owned or metadata.tag.startswith("{" + OWNERSHIP + "}")
    element = root.find("./uuid")
    return {
        "answered": True,
        "present": True,
        "owned": owned,
        "uuid": (element.text or "").strip() if element is not None else "",
    }


def domain_state(runner, uri, name):
    """Report what the domain is doing, or the empty string when it has no state.

    A domain the hypervisor does not define, and one whose state cannot be
    read, both answer the empty string: neither proves the domain is idle, and
    a removal never assumes it.
    """
    code, output = virsh(runner, uri, "domstate", name)
    if code != 0:
        return ""
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    state = lines[0] if lines else ""
    return state if state in DOMAIN_STATES else ""


def disk_size_gib(runner, path):
    """Report a disk's virtual size in whole GiB, or zero when it cannot be read.

    A running or paused QEMU holds a write lock on every image it opened, and
    qemu-img refuses to open a locked image unless it is told to share it, so
    a disk a live domain holds would otherwise read as zero.
    """
    if not os.path.isfile(path):
        return 0
    code, output = invoke(runner, [QEMU_IMG, "info", "--force-share", "--output", "json", path], 1 << 16)
    if code != 0:
        return 0
    try:
        return int(json.loads(output).get("virtual-size", 0)) // (1 << 30)
    except (ValueError, TypeError):
        return 0


def container_image(runner, name):
    code, output = invoke(runner, [PODMAN, "inspect", "--type", "container", "--format", "{{.ImageName}}", name], 4096)
    if code != 0 or not output.strip():
        return ""
    return output.strip().splitlines()[0].strip()


def socket_table_lines(path):
    """Yield one kernel socket table's text lines, opened read-only as ASCII."""
    with open(path, "r", encoding="ascii") as table:
        yield from table


def hexadecimal(text, digits):
    if len(text) != digits or set(text) - HEX_DIGITS:
        raise ValueError("socket table field")
    return text


def table_address(text, digits):
    """Decode one local address: the host's own words in order, then the port.

    The kernel prints each 32-bit word of the address as the integer it holds
    in host byte order, so every word is packed back in that order.
    """
    host, separator, port = text.partition(":")
    if not separator:
        raise ValueError("socket table address")
    host = hexadecimal(host, digits)
    packed = b"".join(int(host[at:at + 8], 16).to_bytes(4, sys.byteorder) for at in range(0, digits, 8))
    return ipaddress.ip_address(packed), int(hexadecimal(port, 4), 16)


def table_listeners(lines, digits):
    """Every listening local address of one table, read up to its first other entry.

    The bytes read are bounded, and a table without its header, a line that is
    not an entry and a table past the bound all raise, because a table that
    was not read in full proves nothing is listening.
    """
    lines = iter(lines)
    header = next(lines, "")
    consumed = len(header)
    if header.split()[:1] != ["sl"]:
        raise ValueError("socket table header")
    listeners = []
    for line in lines:
        consumed += len(line)
        if consumed > MAX_OUTPUT:
            raise ValueError("socket table bound")
        fields = line.split()
        if len(fields) < 4 or not fields[0].endswith(":"):
            raise ValueError("socket table entry")
        if int(hexadecimal(fields[3], 2), 16) != TCP_LISTEN:
            break
        listeners.append(table_address(fields[1], digits))
    return listeners


def holds(bind, local, digits):
    """Whether a listener on `local` in the table of that width holds `bind`.

    An IPv4 address is held by itself or the IPv4 wildcard, and by its
    IPv4-mapped form or the IPv6 wildcard, which also accepts IPv4 unless the
    socket was made IPv6-only; an IPv6 address only by itself or that wildcard.
    """
    if digits == 8:
        return bind.version == 4 and local in (bind, ANY_IPV4)
    if local == ANY_IPV6:
        return True
    if bind.version == 4:
        return local.ipv4_mapped == bind
    return local == bind


def listening(address, port, read=None):
    """Report whether anything listens on one TCP socket of the host.

    `read(path)` returns an iterable of a table's text lines. A missing IPv6
    table means the host has no IPv6 stack. Any other table that cannot be read
    or parsed raises, so the observation fails instead of reading the port free.
    """
    read = read or socket_table_lines
    bind, port = ipaddress.ip_address(str(address)), int(port)
    for path, digits in SOCKET_TABLES:
        try:
            listeners = table_listeners(read(path), digits)
        except FileNotFoundError:
            if digits == 8:
                raise
            continue
        for local, local_port in listeners:
            if local_port == port and holds(bind, local, digits):
                return True
    return False


def observe_host(runner, request):
    """Bounded read-only observation of everything the host block owns.

    `uri` reports whether the connection answered. Without an answer no
    network or pool is read, so their empty fields prove nothing. With one,
    each managed network's `answered` and `poolAnswered` report whether the
    driver that owns it answered for it, because the uri answering proves only
    that the hypervisor did. An external network is never read, so it is never
    answered for. The pool directory is observed by its path either way. Each
    managed network's `definition` is whether it carries its frozen entry, and
    an external network, never read, carries none.
    """
    networks = []
    answers = uri_answers(runner, request["uri"])
    context = str((request.get("identity") or {}).get("context") or "")
    for network in request.get("networks") or []:
        entry = {"name": network["name"], "managed": bool(network["managed"]), "bridge": bridge_present(network["bridge"])}
        entry["answered"], entry["definition"], entry["state"], entry["owned"], entry["uuid"] = False, False, "", False, ""
        if entry["managed"] and answers:
            state = network_state(runner, request["uri"], network["name"], network, context)
            entry["answered"], entry["definition"], entry["state"] = state["answered"], state["definition"], state["state"]
            entry["owned"], entry["uuid"] = state["owned"], state["uuid"]
        networks.append(entry)
    pool = pool_state(runner, request["uri"], request["poolName"]) if answers else {"answered": False, "state": ""}
    services = []
    for service in request.get("services") or []:
        services.append({
            "name": service,
            "state": unit_state(runner, service),
            "enabled": unit_enabled(runner, service),
        })
    return {
        "directory": os.path.lexists(request["poolPath"]),
        "hypervisor": packages_present(runner, request.get("packages") or []),
        "networks": networks,
        "pool": pool["state"],
        "poolAnswered": pool["answered"],
        "services": services,
        "uri": answers,
    }


def observe_machine(runner, request, read=None):
    """Bounded read-only observation of everything the machine block owns.

    An empty `domain` means no such domain only while `answered` is true; with
    a silent hypervisor it means nothing, and no reader may take it for absence.
    A disk is present while anything exists at its path, whatever its image
    reports, and `listener` is whether anything listens on the controller's
    socket; `read` reads the kernel's socket tables, as `listening` does.
    """
    controller = request["controller"]
    metadata = domain_metadata(runner, request["uri"], request["domain"])
    disks = []
    for disk in request.get("disks") or []:
        disks.append({
            "name": disk["name"],
            "present": os.path.lexists(disk["path"]),
            "sizeGiB": disk_size_gib(runner, disk["path"]),
        })
    return {
        "answered": metadata["answered"],
        "controller": container_image(runner, controller["unit"]),
        "disks": disks,
        "domain": request["domain"] if metadata["present"] else "",
        "listener": listening(controller["address"], controller["port"], read),
        "owned": metadata["owned"],
        "state": domain_state(runner, request["uri"], request["domain"]) if metadata["present"] else "",
        "unit": unit_state(runner, controller["unit"] + ".service"),
        "uuid": metadata["uuid"],
    }
