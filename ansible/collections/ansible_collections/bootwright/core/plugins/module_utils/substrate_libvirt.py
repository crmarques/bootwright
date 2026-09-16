"""Bounded host observation and argument vectors for one libvirt substrate.

Every effect reaches the hypervisor through virsh with an exact argument
vector: the module chooses no target, workflow or implementation, and the
frozen Go request alone decides what is realized.
"""

from __future__ import annotations

import json
import os
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
MAX_DOMAINS = 4096

# OWNERSHIP is the metadata namespace a realized object carries. A same-named
# object without it is foreign and is never changed or removed.
OWNERSHIP = "https://bootwright.io/substrate/v1"


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


def uri_answers(runner, uri):
    code, _output = virsh(runner, uri, "version")
    return code == 0


def network_state(runner, uri, name):
    """Report one network's state, ownership, bridge and identity, unchanged.

    The UUID is read because libvirt refuses to define a name that already
    exists under a different one, so a repeated apply can only redefine a
    network by offering back the identity the host already carries.
    """
    code, output = virsh(runner, uri, "net-dumpxml", name)
    if code != 0:
        return {"state": "", "owned": False, "bridge": "", "uuid": ""}
    owned, bridge, uuid = False, "", ""
    try:
        root = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return {"state": "", "owned": False, "bridge": "", "uuid": ""}
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
    return {"state": state, "owned": owned, "bridge": bridge, "uuid": uuid}


def bridge_present(name):
    return os.path.isdir("/sys/class/net/" + str(name))


def pool_state(runner, uri, name):
    code, output = virsh(runner, uri, "pool-info", name)
    if code != 0:
        return ""
    return "active" if "State:          running" in output else "inactive"


def domain_metadata(runner, uri, name):
    """Report one domain's ownership and identity without changing it."""
    code, output = virsh(runner, uri, "dumpxml", name)
    if code != 0:
        return {"present": False, "owned": False, "uuid": ""}
    try:
        root = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return {"present": True, "owned": False, "uuid": ""}
    owned = False
    for metadata in root.findall("./metadata/"):
        owned = owned or metadata.tag.startswith("{" + OWNERSHIP + "}")
    element = root.find("./uuid")
    return {"present": True, "owned": owned, "uuid": (element.text or "").strip() if element is not None else ""}


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


def running_domains(runner, uri):
    """Name every domain the hypervisor currently runs, this context's or not."""
    code, output = virsh(runner, uri, "list", "--name", "--state-running")
    if code != 0:
        return []
    return [line.strip() for line in output.splitlines() if line.strip()][:MAX_DOMAINS]


def domain_bridges(runner, uri, name):
    """Name every bridge one domain attaches an interface to."""
    code, output = virsh(runner, uri, "dumpxml", name)
    if code != 0:
        return []
    try:
        root = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return []
    bridges = []
    for source in root.findall("./devices/interface/source"):
        bridge = source.get("bridge")
        if bridge:
            bridges.append(bridge)
    return bridges


def busy_bridges(runner, uri):
    """Every bridge a running domain is attached to.

    A network carrying a running guest is in use whoever owns the guest, so
    this reads the whole hypervisor rather than only this context's domains.
    """
    bridges = set()
    for name in running_domains(runner, uri):
        bridges.update(domain_bridges(runner, uri, name))
    return bridges


def disk_size_gib(runner, path):
    """Report a disk's virtual size in whole GiB, or zero when it is absent."""
    if not os.path.isfile(path):
        return 0
    code, output = invoke(runner, [QEMU_IMG, "info", "--output", "json", path], 1 << 16)
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


def observe_host(runner, request):
    """Bounded read-only observation of everything the host block owns."""
    networks = []
    answers = uri_answers(runner, request["uri"])
    busy = busy_bridges(runner, request["uri"]) if answers else set()
    for network in request.get("networks") or []:
        entry = {"name": network["name"], "managed": bool(network["managed"]), "bridge": bridge_present(network["bridge"])}
        entry["busy"] = network["bridge"] in busy
        if entry["managed"] and answers:
            state = network_state(runner, request["uri"], network["name"])
            entry["state"], entry["owned"], entry["uuid"] = state["state"], state["owned"], state["uuid"]
        else:
            entry["state"], entry["owned"], entry["uuid"] = "", False, ""
        networks.append(entry)
    return {
        "hypervisor": packages_present(runner, request.get("packages") or []),
        "networks": networks,
        "pool": pool_state(runner, request["uri"], request["poolName"]) if answers else "",
        "service": unit_state(runner, request["service"]),
        "uri": answers,
    }


def observe_machine(runner, request):
    """Bounded read-only observation of everything the machine block owns."""
    controller = request["controller"]
    metadata = domain_metadata(runner, request["uri"], request["domain"])
    disks = []
    for disk in request.get("disks") or []:
        size = disk_size_gib(runner, disk["path"])
        disks.append({"name": disk["name"], "present": size > 0, "sizeGiB": size})
    return {
        "controller": container_image(runner, controller["unit"]),
        "disks": disks,
        "domain": request["domain"] if metadata["present"] else "",
        "owned": metadata["owned"],
        "state": domain_state(runner, request["uri"], request["domain"]) if metadata["present"] else "",
        "unit": unit_state(runner, controller["unit"] + ".service"),
        "uuid": metadata["uuid"],
    }
