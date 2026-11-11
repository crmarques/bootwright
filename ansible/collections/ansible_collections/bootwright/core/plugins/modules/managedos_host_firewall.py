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


def offline_ssh_shape(guest):
    """A source zone, rich rule or host policy can override a port grant."""
    code, output = guest.run([OFFLINE, "--list-all-zones"])
    if code != 0:
        raise worker.failed("ssh-admission")
    rich = False
    for line in output.decode("ascii").splitlines():
        key, separator, value = line.strip().partition(":")
        if key in ("sources", "rich rules") and value.strip():
            raise worker.failed("ssh-admission")
        if rich and line.strip() and not separator:
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


def ssh_admitted(guest, address, port):
    """Before remote startup, prove SSH in the interface's configured NM zone."""
    offline_ssh_shape(guest)
    found = guest.addresses()
    interfaces, complete = guest.service_interfaces({"addresses": [address]}, found)
    if not complete:
        raise worker.failed("ssh-admission")
    code, output = guest.run(["/usr/bin/nmcli", "--get-values", "GENERAL.CONNECTION", "device", "show", interfaces[0]])
    connection = output.decode("utf-8").strip() if code == 0 else ""
    if not connection or "\n" in connection or len(connection) > 256:
        raise worker.failed("ssh-admission")
    code, output = guest.run(["/usr/bin/nmcli", "--get-values", "connection.zone", "connection", "show", connection])
    if code != 0:
        raise worker.failed("ssh-admission")
    zone = output.strip()
    if not zone:
        code, zone = guest.run([OFFLINE, "--get-default-zone"])
        if code != 0:
            raise worker.failed("ssh-admission")
    zone = worker.token(zone, worker.ZONE)
    code, _output = guest.run([OFFLINE, "--zone=" + zone, "--query-port=%d/tcp" % port])
    if code == 0:
        return
    if port == 22:
        code, _output = guest.run([OFFLINE, "--zone=" + zone, "--query-service=ssh"])
        if code == 0 and service_ssh_port(guest, port):
            return
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
    try:
        changed = False
        if params["operation"] == "apply":
            for service in params["services"]:
                if guest.ownership(service["name"], params["mark"])[0] == "foreign":
                    raise worker.Refused("host-firewall-foreign", detail=service["name"])
            changed = prepare(guest, params)
        result = worker.HANDLERS[params["operation"]](guest, payload)
        code, output = guest.run([SYSTEMCTL, "is-enabled", "firewalld.service"])
        ready = code == 0 and output.strip() == b"enabled"
        return {"changed": changed or result["changed"], "ok": True, "ready": ready, "evidence": {"services": result["services"]}}
    except worker.Refused as refused:
        return dict(worker.refusal(refused), changed=False)


def main():
    module = AnsibleModule(argument_spec={
        "operation": {"type": "str", "required": True, "choices": ["apply", "inspect", "remove"]},
        "mark": {"type": "str", "required": True},
        "services": {"type": "list", "elements": "dict", "required": True},
        "ssh_address": {"type": "str", "default": ""},
        "ssh_port": {"type": "int", "default": 0},
    }, supports_check_mode=False)
    try:
        result = execute(module.params)
    except ValueError:
        module.fail_json(msg="invalid host firewall request")
    module.exit_json(**result)


if __name__ == "__main__":
    main()
