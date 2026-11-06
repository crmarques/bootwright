#!/usr/bin/python
"""Realize, read or take back host firewalld services on an installed machine through a fixed guest worker.

The module runs on the controller. It runs the fixed root worker over the
installation's pinned SSH session, the one the update worker uses, with only the
operation, the ownership mark and the services on its standard input. It returns
closed reasons, states and the evidence of each service only; no native output
reaches its result.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_firewall
short_description: Realize host firewalld services on an installed machine
version_added: "0.1.0"
description:
  - Runs one fixed worker as root through C(sudo -n) on the installed machine
    over the installation's pinned SSH session, built option for option as the
    update worker's is, with host-key algorithms the placement host's SSH
    policy permits.
  - C(apply) writes each service file the caller rendered to
    /etc/firewalld/services atomically, reloads firewalld, adds the service to
    the permanent configuration of the zone of every interface that carries an
    network contains the address of the service's first network (the virtual
    address as a single-host prefix), or, when none does, whose own network is
    one of its later networks, and reloads again. It refuses before
    any change when a file of that name is not the caller's, marked by
    C(mark) inside its description, or the service has no such interface.
  - C(remove) takes the service out of every permanent zone, deletes an owned
    file and reloads. C(inspect) reads and changes nothing.
  - Returns closed reasons only.
  - C(host-firewall-foreign) a service file of that name is not owned; C(detail)
    names the service and nothing was changed.
  - C(host-firewall-no-interface) the service has no interface;
    C(detail) names the service and nothing was changed.
  - C(host-firewall-inactive) firewalld is not running.
  - C(host-firewall-failed) a tool, the reload, a zone change, a file write or the
    machine's addresses failed, C(tooling), C(reload), C(zone), C(write) or C(address).
  - C(transport) the session failed.
options:
  operation:
    description: What the worker does.
    type: str
    required: true
    choices:
      - apply
      - inspect
      - remove
  address:
    description: The installed machine's address.
    type: str
    required: true
  user:
    description: The fleet account on the installed machine.
    type: str
    required: true
  identity:
    description: The lent fleet private key file.
    type: path
    required: true
  ssh_config:
    description: The reduced placement-host policy configuration file the role wrote.
    type: path
    required: true
  known_hosts:
    description: The pinned known_hosts file the role wrote.
    type: path
    required: true
  host_key_algorithms:
    description: The host-key algorithms the pinned key is accepted under.
    type: str
    required: true
  policy_host_key_algorithms:
    description: The host-key algorithms the placement host's SSH policy permits.
    type: str
    required: true
  mark:
    description: The ownership mark inside every service file's description, printable ASCII without markup characters, at most 256 bytes.
    type: str
    required: true
  services:
    description: The services, from 1 to 64, in request order.
    type: list
    elements: dict
    required: true
    suboptions:
      name:
        description: The firewalld service name.
        type: str
        required: true
      networks:
        description:
          - The CIDR networks the service is served on, from 1 to 16.
          - The first is the virtual address as a single-host prefix and selects the interfaces whose network contains it.
          - The later ones select interfaces only when none does.
        type: list
        elements: str
        required: true
      content:
        description: The exact bytes of the service file, ASCII starting with an XML declaration, at most 16384 bytes.
        type: str
        required: true
  timeout:
    description: Seconds the whole session may take, from 1 to 900.
    type: int
    default: 300
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Realize the host firewall services on the installed machine
  bootwright.core.managedos_firewall:
    operation: apply
    address: 192.0.2.10
    user: fleet
    identity: "{{ scratch }}/identity"
    ssh_config: "{{ scratch }}/ssh_config"
    known_hosts: "{{ scratch }}/known_hosts"
    host_key_algorithms: ssh-ed25519
    policy_host_key_algorithms: ssh-ed25519,ecdsa-sha2-nistp256
    mark: owner context=lab block=os-firewall-ceph-01 apply=op-0123
    services:
      - name: lab-ceph-ingress-rgw
        networks:
          - 192.0.2.60/32
          - 192.0.2.0/24
        content: "<?xml version=\"1.0\" encoding=\"utf-8\"?>..."
"""

RETURN = r"""
ok:
  description: Whether the worker answered with a result rather than a refusal.
  returned: always
  type: bool
reason:
  description: The closed refusal reason.
  returned: when ok is false
  type: str
  choices:
    - host-firewall-foreign
    - host-firewall-no-interface
    - host-firewall-inactive
    - host-firewall-failed
    - transport
cause:
  description: The closed cause of host-firewall-failed or a transport refusal.
  returned: when the reason has one
  type: str
  choices:
    - tooling
    - reload
    - zone
    - write
    - address
    - timeout
    - session
    - host-key
    - exit
    - answer
    - unreachable
detail:
  description: The service name of a foreign or interfaceless service; or fixed text naming a refused session check.
  returned: for host-firewall-foreign, host-firewall-no-interface and a refused session
  type: str
evidence:
  description: The services as the machine holds them after the operation.
  returned: when ok is true
  type: dict
  contains:
    services:
      description:
        - One entry per requested service in request order, each with C(name), C(state) (C(owned), C(absent) or C(foreign)),
          C(digest) of the file on disk, C(covered), and C(memberships) of C(interface) and C(zone) sorted by zone and interface.
      type: list
      elements: dict
"""

import inspect
import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_firewall_worker as worker, managedos_guest_ssh, remote_command
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

MAX_TIMEOUT = 900


class ArgumentError(ValueError):
    """An argument the module refuses; its text names the option, never a value."""


def worker_source():
    return inspect.getsource(worker) + "\nraise SystemExit(main())\n"


def check_service(service):
    if not isinstance(service, dict) or set(service) != worker.SERVICE_KEYS:
        raise ArgumentError("a service is not a name, networks and content")
    if not worker.valid_name(service["name"]):
        raise ArgumentError("a service name is not a firewalld service name")
    if not worker.valid_networks(service["networks"]):
        raise ArgumentError("a service has no network or a network is not a CIDR with its host bits clear")
    if not worker.valid_content(service["content"]):
        raise ArgumentError("a service file is not ASCII XML of at most %d bytes" % worker.MAX_CONTENT)


def check_arguments(params):
    if not worker.valid_mark(params["mark"]):
        raise ArgumentError("mark is not printable ASCII without markup characters of at most 256 bytes")
    services = params["services"]
    if not isinstance(services, list) or not 0 < len(services) <= worker.MAX_SERVICES:
        raise ArgumentError("services is not from 1 to 64 services")
    for service in services:
        check_service(service)
    if len({service["name"] for service in services}) != len(services):
        raise ArgumentError("a service name is repeated")
    if type(params["timeout"]) is not int or not 1 <= params["timeout"] <= MAX_TIMEOUT:  # pylint: disable=unidiomatic-typecheck
        raise ArgumentError("timeout is not between 1 and 900 seconds")


def refusal(reason, cause=None, detail=None):
    outcome = {"changed": False, "ok": False, "reason": reason}
    if cause is not None:
        outcome["cause"] = cause
    if detail is not None:
        outcome["detail"] = detail
    return outcome


def valid_result(operation, names, value):
    if not isinstance(value, dict) or set(value) != {"changed", "services"} or type(value["changed"]) is not bool:  # pylint: disable=unidiomatic-typecheck
        return False
    return (operation != "inspect" or value["changed"] is False) and worker.valid_evidence(value["services"], names)


def valid_refusal(value, names):
    if not {"ok", "reason"} <= set(value) <= {"ok", "reason", "cause", "detail"} or value["reason"] not in worker.REASONS:
        return False
    causes = worker.CAUSES.get(value["reason"], ())
    if ("cause" in value) != bool(causes) or ("cause" in value and value["cause"] not in causes):
        return False
    if "detail" in value:
        return value["reason"] in worker.DETAILED and value["detail"] in names
    return value["reason"] not in worker.DETAILED


def parse_answer(operation, names, stdout):
    """The worker's one closed answer, or None."""
    if not isinstance(stdout, bytes) or b"\n" in stdout.rstrip(b"\n"):
        return None
    try:
        value = json.loads(stdout.decode("utf-8"))
    except (ValueError, UnicodeError):
        return None
    if not isinstance(value, dict) or type(value.get("ok")) is not bool:  # pylint: disable=unidiomatic-typecheck
        return None
    if value["ok"]:
        return value if set(value) == {"ok", "result"} and valid_result(operation, names, value["result"]) else None
    return value if valid_refusal(value, names) else None


def interpret(operation, names, outcome):
    if outcome.timed_out:
        return refusal("transport", "timeout")
    if outcome.cause is not None:
        return refusal("transport", outcome.cause)
    if outcome.code == 255:
        return refusal("transport", "host-key" if outcome.host_key else "unreachable")
    if outcome.code != 0:
        return refusal("transport", "exit")
    value = parse_answer(operation, names, outcome.stdout)
    if value is None:
        return refusal("transport", "answer")
    if value["ok"]:
        return {"changed": value["result"]["changed"], "evidence": {"services": value["result"]["services"]}, "ok": True}
    if value["reason"] == "transport":
        return refusal("transport", "answer")
    return refusal(value["reason"], value.get("cause"), value.get("detail"))


def execute(params, run=managedos_guest_ssh.run):
    """The module result; only the operation, the mark and the services reach the worker."""
    check_arguments(params)
    names = [service["name"] for service in params["services"]]
    services = [{"content": service["content"], "name": service["name"], "networks": list(service["networks"])}
                for service in params["services"]]
    payload = canonical({"mark": params["mark"], "operation": params["operation"], "services": services})
    remote_argv = remote_command.fixed_worker_argv(worker_source())
    try:
        argv = managedos_guest_ssh.guest_argv(
            ssh_config=params["ssh_config"], known_hosts=params["known_hosts"],
            host_key_algorithms=params["host_key_algorithms"], policy_host_key_algorithms=params["policy_host_key_algorithms"],
            identity=params["identity"], user=params["user"], address=params["address"], remote_argv=remote_argv)
    except managedos_guest_ssh.SessionError as error:
        return refusal("transport", "session", str(error))
    return interpret(params["operation"], names, run(argv, payload, params["timeout"]))


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "inspect", "remove"]},
            "address": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "identity": {"type": "path", "required": True},
            "ssh_config": {"type": "path", "required": True},
            "known_hosts": {"type": "path", "required": True},
            "host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "policy_host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "mark": {"type": "str", "required": True, "no_log": False},
            "services": {"type": "list", "elements": "dict", "required": True, "options": {
                "name": {"type": "str", "required": True},
                "networks": {"type": "list", "elements": "str", "required": True},
                "content": {"type": "str", "required": True},
            }},
            "timeout": {"type": "int", "default": 300},
        },
        supports_check_mode=False,
    )
    try:
        outcome = execute(module.params)
    except ArgumentError as error:
        module.fail_json(msg=str(error))
    module.exit_json(**outcome)


if __name__ == "__main__":
    main()
