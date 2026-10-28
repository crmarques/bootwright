#!/usr/bin/python
"""Register, unregister or inspect an installed machine's subscription through a fixed guest worker.

The module runs on the controller. It reads the organization and activation key
from their lent material files, refuses a value that is not one line of
printable ASCII without whitespace before any connection opens, and passes
them only on the standard input of the fixed root worker it runs over the
installation's pinned SSH session. It returns closed reasons and states only;
no material value, no native output and no service text reaches its result.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_registration
short_description: Register an installed machine with its subscription service
version_added: "0.1.0"
description:
  - Runs one fixed worker as root through C(sudo -n) on the installed machine
    over the installation's pinned SSH session, built option for option as the
    role's other guest commands are, with host-key algorithms the placement
    host's SSH policy permits.
  - The worker registers through the RHSM D-Bus API with empty connection
    options, records the consumer it created under the installation request's
    digest, applies the subscription repository changes in one
    C(subscription-manager repos) call, and unregisters only a consumer it
    created.
  - The organization and activation key are read from their files by this
    module and reach the worker only on its standard input; they never enter an
    argument, the environment, the result or a message.
  - A value that is not one line of printable ASCII without whitespace is
    refused with C(registration-input) before any connection opens.
  - Returns closed reasons only. The operator texts the controller renders for
    them name C(Machine/<m>), the reason and the next step, as follows.
  - C(registration-input) the <organization|activation key> Secret value of
    Entitlement/<e> is not one line of printable ASCII without whitespace; set
    it again with bootwright secret set --name <secret> --context <c>
    --value-file <path>, then run bootwright apply --context <c> again.
  - C(registration-foreign) it is registered with a subscription consumer this
    installation did not create; it is left registered; unregister
    Machine/<m> from its subscription service yourself, or destroy and install
    it again, then run bootwright apply --context <c> again.
  - C(registration-refused) the subscription service refused registration with
    Entitlement/<e>; check the organization and activation key Secrets of
    Entitlement/<e>, then run bootwright apply --context <c> again.
  - C(registration-identity-collision) the subscription service reports
    another registered host with this Machine's hardware identity; remove the
    subscription service's stale consumer for Machine/<m> (the earlier
    installation that was not unregistered), then run bootwright apply
    --context <c> again.
  - C(registration-unavailable) the subscription service or its native tooling
    did not answer; restore the Machine's route to the subscription service,
    then run bootwright apply --context <c> again.
  - C(registration-repositories) the subscription service did not accept
    spec.customizations.repositories.subscription of <profile>; correct the
    repository IDs in <profile>, then destroy and install Machine/<m> again.
  - C(update-release-unavailable) the subscription service offers no release
    <release> to Machine/<m>; set spec.os.version on <profile> to a release it
    offers, or set spec.updates.policy to none, then destroy and apply again.
  - C(registration-unreachable) the installed system did not answer over its
    pinned SSH connection; start Machine/<m> with bootwright machine start
    --context <c> --name <m>, then run bootwright destroy --context <c> again.
  - C(transport) the registration session failed; run bootwright apply
    --context <c> again.
options:
  operation:
    description: What the worker does.
    type: str
    required: true
    choices: [register, unregister, inspect]
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
  request:
    description: The frozen installation request digest, 64 lowercase hexadecimal characters.
    type: str
    required: true
  organization_file:
    description: The lent organization material file; required for register and refused otherwise.
    type: path
  activation_key_file:
    description: The lent activation key material file; required for register and refused otherwise.
    type: path
  enable:
    description: Subscription repository IDs to enable; register only.
    type: list
    elements: str
    default: []
  disable:
    description: Subscription repository IDs to disable before enabling, C(*) for all; register only.
    type: list
    elements: str
    default: []
  release:
    description:
      - The release a request updating within it sets with
        C(subscription-manager release --set) after proving
        C(subscription-manager release --list) offers it, before the
        repositories; empty sets none. Register only.
    type: str
    default: ""
  timeout:
    description: Seconds the whole session may take, from 1 to 900.
    type: int
    default: 630
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Register the installed machine
  bootwright.core.managedos_registration:
    operation: register
    address: 192.0.2.10
    user: fleet
    identity: "{{ scratch }}/identity"
    ssh_config: "{{ scratch }}/ssh_config"
    known_hosts: "{{ scratch }}/known_hosts"
    host_key_algorithms: ssh-ed25519
    policy_host_key_algorithms: ssh-ed25519,ecdsa-sha2-nistp256
    request: "{{ request_digest }}"
    organization_file: "{{ material.organization }}"
    activation_key_file: "{{ material.activationKey }}"
    disable: ["*"]
    enable: [rhel-9-for-x86_64-baseos-rpms]
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
  choices: [registration-foreign, registration-refused, registration-identity-collision, registration-input,
            registration-unavailable, registration-repositories, update-release-unavailable, registration-unreachable,
            transport]
cause:
  description: The closed cause of registration-unavailable or of a transport refusal.
  returned: when the reason has one
  type: str
  choices: [service, timeout, tooling, unregister, session, host-key, exit, answer]
detail:
  description: Fixed text naming which input or session check refused; never a value.
  returned: for registration-input and a refused session
  type: str
registration:
  description: The worker's result.
  returned: when ok is true
  type: dict
  contains:
    changed:
      description: Whether the worker changed the machine.
      type: bool
    consumer:
      description: The consumer UUID this installation owns, or an empty string.
      type: str
    owned:
      description: Whether the machine is registered with a consumer this installation created.
      type: bool
    state:
      description: registered, adopted or unchanged; unregistered, absent or retained; owned, pending, absent or foreign.
      type: str
    repositories:
      description: The subscription repository changes applied.
      type: dict
"""

import inspect
import json
import os
import stat

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh, managedos_rhsm_worker as worker, remote_command
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

MAX_MATERIAL = 257
MAX_TIMEOUT = 900
MATERIAL = (("organization_file", "organization", "organization"), ("activation_key_file", "activationKey", "activation key"))
MODULE_CAUSES = ("session", "timeout", "host-key", "exit", "answer")
RESULT_KEYS = frozenset(("changed", "consumer", "owned", "repositories", "state"))


class ArgumentError(ValueError):
    """An argument the module refuses; its text names the option, never a value."""


def worker_source():
    return inspect.getsource(worker) + "\nraise SystemExit(main())\n"


def check_arguments(params):
    if params["operation"] == "register":
        for option in (entry[0] for entry in MATERIAL):
            if not params.get(option):
                raise ArgumentError(option + " is required for the register operation")
    else:
        for option in (entry[0] for entry in MATERIAL):
            if params.get(option) is not None:
                raise ArgumentError(option + " is accepted only for the register operation")
        for option in ("enable", "disable", "release"):
            if params.get(option):
                raise ArgumentError(option + " is accepted only for the register operation")
    if not isinstance(params["request"], str) or not worker.DIGEST.fullmatch(params["request"]):
        raise ArgumentError("request is not 64 lowercase hexadecimal characters")
    if type(params["timeout"]) is not int or not 1 <= params["timeout"] <= MAX_TIMEOUT:  # pylint: disable=unidiomatic-typecheck
        raise ArgumentError("timeout is not between 1 and 900 seconds")


def read_material(path, name):
    """One lent material value, bounded and validated; it lives only in memory."""
    refused = worker.Refused("registration-input", detail="the " + name + " is not one line of printable ASCII without whitespace")
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    except (OSError, TypeError, ValueError):
        raise refused from None
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise refused
        data = worker.read_bounded(descriptor, MAX_MATERIAL)
    finally:
        os.close(descriptor)
    if len(data) > MAX_MATERIAL:
        raise refused
    return worker.secret_value(data, name)


def payload_of(params):
    payload = {"operation": params["operation"], "request": params["request"]}
    if params["operation"] == "register":
        for option, key, name in MATERIAL:
            payload[key] = read_material(params[option], name)
        payload["enable"], payload["disable"] = worker.repositories(list(params["enable"] or []), list(params["disable"] or []))
        payload["release"] = params.get("release") or ""
        if not worker.RELEASE.fullmatch(payload["release"]) and payload["release"] != "":
            raise worker.Refused("registration-input", detail="the release is not a <major>.<minor> release")
    return canonical(payload)


def refusal(reason, cause=None):
    outcome = {"changed": False, "ok": False, "reason": reason}
    if cause is not None:
        outcome["cause"] = cause
    return outcome


def string_list(value):
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


def valid_result(operation, value):
    if not isinstance(value, dict) or set(value) != RESULT_KEYS:
        return False
    repositories = value["repositories"]
    return (type(value["changed"]) is bool and type(value["owned"]) is bool  # pylint: disable=unidiomatic-typecheck
            and value["state"] in worker.STATES[operation]
            and value["owned"] == (value["state"] in worker.OWNED_STATES)
            and isinstance(value["consumer"], str) and (value["consumer"] == "" or bool(worker.UUID.fullmatch(value["consumer"])))
            and isinstance(repositories, dict) and set(repositories) == {"disable", "enable"}
            and string_list(repositories["disable"]) and string_list(repositories["enable"]))


def valid_refusal(value):
    if not {"ok", "reason"} <= set(value) <= {"ok", "reason", "cause"} or value["reason"] not in worker.REASONS:
        return False
    causes = worker.CAUSES.get(value["reason"], ())
    return value["cause"] in causes if "cause" in value else not causes


def parse_answer(operation, stdout):
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
        return value if set(value) == {"ok", "result"} and valid_result(operation, value["result"]) else None
    return value if valid_refusal(value) else None


def interpret(operation, outcome):
    if outcome.timed_out:
        return refusal("transport", "timeout")
    if outcome.cause is not None:
        return refusal("transport", outcome.cause)
    if outcome.code == 255:
        return refusal("transport", "host-key") if outcome.host_key else refusal("registration-unreachable")
    if outcome.code != 0:
        return refusal("transport", "exit")
    value = parse_answer(operation, outcome.stdout)
    if value is None:
        return refusal("transport", "answer")
    if value["ok"]:
        return {"changed": value["result"]["changed"], "ok": True, "registration": value["result"]}
    return refusal(value["reason"], value.get("cause"))


def execute(params, run=managedos_guest_ssh.run):
    """The module result; material is refused before any session opens."""
    check_arguments(params)
    try:
        payload = payload_of(params)
    except worker.Refused as refused:
        return {"changed": False, "ok": False, "reason": refused.reason, "detail": refused.detail}
    remote_argv = remote_command.fixed_worker_argv(worker_source())
    try:
        argv = managedos_guest_ssh.guest_argv(
            ssh_config=params["ssh_config"], known_hosts=params["known_hosts"],
            host_key_algorithms=params["host_key_algorithms"], policy_host_key_algorithms=params["policy_host_key_algorithms"],
            identity=params["identity"], user=params["user"], address=params["address"], remote_argv=remote_argv)
    except managedos_guest_ssh.SessionError as error:
        return dict(refusal("transport", "session"), detail=str(error))
    return interpret(params["operation"], run(argv, payload, params["timeout"]))


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["register", "unregister", "inspect"]},
            "address": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "identity": {"type": "path", "required": True},
            "ssh_config": {"type": "path", "required": True},
            "known_hosts": {"type": "path", "required": True},
            "host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "policy_host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "request": {"type": "str", "required": True},
            "organization_file": {"type": "path"},
            "activation_key_file": {"type": "path"},
            "enable": {"type": "list", "elements": "str", "default": []},
            "disable": {"type": "list", "elements": "str", "default": []},
            "release": {"type": "str", "default": ""},
            "timeout": {"type": "int", "default": 630},
        },
        required_if=[("operation", "register", ("organization_file", "activation_key_file"))],
        supports_check_mode=False,
    )
    try:
        outcome = execute(module.params)
    except ArgumentError as error:
        module.fail_json(msg=str(error))
    module.exit_json(**outcome)


if __name__ == "__main__":
    main()
