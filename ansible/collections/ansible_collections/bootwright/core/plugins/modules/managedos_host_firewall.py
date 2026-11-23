#!/usr/bin/python
"""Run the shared firewall worker directly on an explicitly managed host."""
from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_host_firewall
short_description: Prepare an explicitly managed host firewall and its owned services
version_added: "0.1.0"
description:
  - Runs the shared host firewall worker on the selected lifecycle placement.
  - Apply enables and starts firewalld. Removal retains daemon enablement.
  - Refuses a reload that would discard unrelated runtime configuration.
  - Before starting firewalld locally, proves that every zone a connected
    interface uses admits every port a running sshd listens on.
options:
  operation:
    description: The selected operation.
    type: str
    required: true
    choices:
      - apply
      - inspect
      - remove
  mark:
    description: The frozen operation's ownership mark.
    type: str
    required: true
  services:
    description: The bounded services rendered by the caller.
    type: list
    elements: dict
    required: true
  ssh_address:
    description: The address of this placement's SSH connection, empty for local execution.
    type: str
    default: ''
  ssh_sources:
    description: Canonical source prefixes for the SSH ingress filter.
    type: list
    elements: str
    default: []
  ssh_port:
    description: The SSH port which must remain admitted before starting a remote firewall.
    type: int
    default: 0
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Prepare the selected host's firewall
  bootwright.core.managedos_host_firewall:
    operation: apply
    mark: owner context=lab block=os-firewall-controller apply=op-0123
    services: []
"""
RETURN = r"""
ok:
  description: Whether the operation answered successfully.
  returned: always
  type: bool
ready:
  description: Whether firewalld is enabled and answering.
  returned: on success
  type: bool
evidence:
  description: The shared worker's bounded service evidence.
  returned: on success
  type: dict
reason:
  description: The shared worker's closed refusal reason.
  returned: on refusal
  type: str
cause:
  description: The closed cause of a failed host operation.
  returned: on refusal
  type: str
detail:
  description: The service name of an ownership or interface refusal.
  returned: on refusal
  type: str
"""

import re

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_firewall_worker as worker

SYSTEMCTL = "/usr/bin/systemctl"
OFFLINE = "/usr/bin/firewall-offline-cmd"
NMCLI = "/usr/bin/nmcli"
SSHD = "/usr/sbin/sshd"
ZONE_HEADER = re.compile(worker.ZONE.pattern + r"(?: \((?:default|active)(?:, (?:default|active))?\))?")


def harmless_offline_policy(guest, name):
    if not worker.ZONE.fullmatch(name):
        return False
    code, output = guest.run([OFFLINE, "--info-policy=" + name])
    if code != 0:
        return False
    fields = {}
    known = {"disable", "priority", "target", "ingress-zones", "egress-zones", "services", "ports", "protocols",
             "masquerade", "forward-ports", "source-ports", "icmp-blocks", "rich rules"}
    for line in output.decode("ascii").splitlines()[1:]:
        stripped = line.strip()
        if stripped.startswith("rule "):
            if not re.fullmatch(r'rule family="ipv6" icmp-type name="[a-z0-9-]+" accept', stripped):
                return False
            continue
        key, separator, value = stripped.partition(":")
        if not separator or key not in known or key in fields:
            return False
        fields[key] = value.strip()
    return (fields.get("target") == "CONTINUE" and fields.get("masquerade") == "no"
            and all(key in fields and fields[key] == "" for key in
                    ("services", "ports", "protocols", "forward-ports", "source-ports", "icmp-blocks", "rich rules")))


def offline_ssh_shape(guest, carrying, interfaces):
    """A source anywhere, an SSH interface bound to another zone, or a rich rule where SSH arrives overrides a grant."""
    code, output = guest.run([OFFLINE, "--list-all-zones"])
    if code != 0:
        raise worker.failed("ssh-admission")
    try:
        lines = output.decode("ascii").splitlines()
    except UnicodeError:
        raise worker.failed("ssh-admission") from None
    zone, rich = None, False
    for line in lines:
        stripped = line.strip()
        if not stripped:
            zone, rich = None, False
            continue
        if not line[0].isspace():
            if not ZONE_HEADER.fullmatch(line):
                raise worker.failed("ssh-admission")
            zone, rich = line.split(" ", 1)[0], False
            continue
        if rich:
            if zone in carrying or not stripped.startswith("rule "):
                raise worker.failed("ssh-admission")
            continue
        # firewalld prints each rich rule on its own indented line after the
        # field, and a rule may itself contain a colon.
        key, separator, value = stripped.partition(":")
        bound = key == "interfaces" and zone not in carrying and bool(set(value.split()) & interfaces)
        if zone is None or not separator or bound or (key == "sources" and value.strip()) or (
                key == "rich rules" and value.strip() and zone in carrying):
            raise worker.failed("ssh-admission")
        rich = key == "rich rules"
    for flag in ("--get-all-rules", "--get-all-passthroughs", "--get-all-chains"):
        code, output = guest.run([OFFLINE, "--direct", flag])
        if code != 0 or output.strip():
            raise worker.failed("ssh-admission")
    code, output = guest.run([OFFLINE, "--get-policies"])
    if code != 0:
        raise worker.failed("ssh-admission")
    names = output.decode("ascii").split()
    if len(names) > 64 or not all(harmless_offline_policy(guest, name) for name in names):
        raise worker.failed("ssh-admission")


def service_ssh_port(guest, port):
    code, output = guest.run([OFFLINE, "--info-service=ssh"])
    if code != 0:
        return False
    fields = {}
    for line in output.decode("ascii").splitlines()[1:]:
        key, separator, value = line.strip().partition(":")
        if not separator or key in fields:
            return False
        fields[key] = value.strip()
    if fields.get("destination") or fields.get("includes") or "ports" not in fields:
        return False
    return "%d/tcp" % port in fields["ports"].split()


def zone_of(guest, interface):
    """The firewalld zone an interface's NetworkManager connection configures."""
    code, output = guest.run([NMCLI, "--get-values", "GENERAL.CONNECTION", "device", "show", interface])
    connection = output.decode("utf-8").strip() if code == 0 else ""
    if not connection or "\n" in connection or len(connection) > 256:
        raise worker.failed("ssh-admission")
    code, output = guest.run([NMCLI, "--get-values", "connection.zone", "connection", "show", connection])
    if code != 0:
        raise worker.failed("ssh-admission")
    zone = output.strip()
    if not zone and shared(guest, connection):
        zone = b"nm-shared"
    if not zone:
        code, zone = guest.run([OFFLINE, "--get-default-zone"])
        if code != 0:
            raise worker.failed("ssh-admission")
    return worker.token(zone, worker.ZONE)


def shared(guest, connection):
    """NetworkManager puts a sharing connection that names no zone in nm-shared."""
    for field in ("ipv4.method", "ipv6.method"):
        code, output = guest.run([NMCLI, "--get-values", field, "connection", "show", connection])
        if code != 0:
            raise worker.failed("ssh-admission")
        if output.strip() == b"shared":
            return True
    return False


def admits(guest, zone, port):
    code, _output = guest.run([OFFLINE, "--zone=" + zone, "--query-port=%d/tcp" % port])
    if code == 0:
        return True
    if port == 22:
        code, _output = guest.run([OFFLINE, "--zone=" + zone, "--query-service=ssh"])
        return code == 0 and service_ssh_port(guest, port)
    return False


def ssh_admitted(guest, address, port):
    """Before remote startup, prove SSH in the interface's configured NM zone."""
    found = guest.addresses()
    interfaces, complete = guest.service_interfaces({"addresses": [address]}, found)
    if not complete:
        raise worker.failed("ssh-admission")
    zone = zone_of(guest, interfaces[0])
    offline_ssh_shape(guest, {zone}, {interfaces[0]})
    if not admits(guest, zone, port):
        raise worker.failed("ssh-admission")


def listening_ports(guest):
    """The ports a running sshd listens on, as its effective configuration says."""
    code, _output = guest.run([SYSTEMCTL, "is-active", "sshd.service"])
    if code != 0:
        return []
    code, output = guest.run([SSHD, "-T"])
    if code != 0:
        raise worker.failed("ssh-admission")
    ports = set()
    for line in output.decode("ascii", "replace").splitlines():
        key, _separator, value = line.partition(" ")
        if key == "port":
            if not value.strip().isdigit() or not 0 < int(value) < 65536:
                raise worker.failed("ssh-admission")
            ports.add(int(value))
    if not ports:
        raise worker.failed("ssh-admission")
    return sorted(ports)


def local_ssh_admitted(guest):
    """Before a local startup, prove every port sshd listens on in every zone a connected interface uses."""
    ports = listening_ports(guest)
    if not ports:
        return
    code, output = guest.run([NMCLI, "--get-values", "DEVICE,STATE", "device"])
    if code != 0:
        raise worker.failed("ssh-admission")
    devices = []
    for line in output.decode("utf-8", "replace").splitlines():
        device, _separator, state = line.rpartition(":")
        if state == "connected" and device and device != "lo":
            devices.append(device)
    if not devices:
        raise worker.failed("ssh-admission")
    zones = {zone_of(guest, device) for device in devices}
    offline_ssh_shape(guest, zones, set(devices))
    for zone in sorted(zones):
        if not all(admits(guest, zone, port) for port in ports):
            raise worker.failed("ssh-admission")


def prepare(guest, params):
    code, output = guest.command("--state")
    running = code == 0 and output.strip() == b"running"
    enabled_code, enabled_output = guest.run([SYSTEMCTL, "is-enabled", "firewalld.service"])
    enabled = enabled_code == 0 and enabled_output.strip() == b"enabled"
    if not running:
        for unit in ("nftables.service", "iptables.service", "ip6tables.service", "ufw.service"):
            state, _output = guest.run([SYSTEMCTL, "is-active", unit])
            if state == 0:
                raise worker.failed("enable")
    changed = not running or not enabled
    if changed and params["ssh_port"]:
        ssh_admitted(guest, params["ssh_address"], params["ssh_port"])
    elif changed:
        local_ssh_admitted(guest)
    if changed:
        code, _output = guest.run([SYSTEMCTL, "enable", "--now", "firewalld.service"])
        if code != 0:
            raise worker.failed("enable")
    return changed


def execute(params, guest=None):
    guest = guest if guest is not None else worker.Guest(preserve_runtime=True)
    if not worker.valid_mark(params["mark"]) or not worker.valid_services(params["services"]):
        raise ValueError("invalid host firewall request")
    payload = {"operation": params["operation"], "mark": params["mark"], "services": params["services"]}
    sources = params.get("ssh_sources", [])
    if sources:
        if not worker.valid_ssh_sources(sources):
            raise ValueError("invalid SSH source prefixes")
        payload["sshSources"] = sources
    try:
        changed = False
        if params["operation"] == "apply":
            for service in params["services"]:
                if guest.ownership(service["name"], params["mark"])[0] == "foreign":
                    raise worker.Refused("host-firewall-foreign", detail=service["name"])
            changed = prepare(guest, params)
        result = worker.converge(guest, payload, remote=bool(params["ssh_port"]))
        code, output = guest.run([SYSTEMCTL, "is-enabled", "firewalld.service"])
        ready = code == 0 and output.strip() == b"enabled"
        evidence = {"services": result["services"]}
        if "sshPolicy" in result:
            evidence["sshPolicy"] = result["sshPolicy"]
        return {"changed": changed or result["changed"], "ok": True, "ready": ready, "evidence": evidence}
    except worker.Refused as refused:
        return dict(worker.refusal(refused), changed=False)


def main():
    module = AnsibleModule(argument_spec={
        "operation": {"type": "str", "required": True, "choices": ["apply", "inspect", "remove"]},
        "mark": {"type": "str", "required": True},
        "services": {"type": "list", "elements": "dict", "required": True},
        "ssh_address": {"type": "str", "default": ""},
        "ssh_port": {"type": "int", "default": 0},
        "ssh_sources": {"type": "list", "elements": "str", "default": []},
    }, supports_check_mode=False)
    try:
        result = execute(module.params)
    except ValueError:
        module.fail_json(msg="invalid host firewall request")
    module.exit_json(**result)


if __name__ == "__main__":
    main()
