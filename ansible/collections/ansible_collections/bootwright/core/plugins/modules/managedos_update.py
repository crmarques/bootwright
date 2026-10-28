#!/usr/bin/python
"""Update an installed machine within its release, or read that update, through a fixed guest worker.

The module runs on the controller. It runs the fixed root worker over the
installation's pinned SSH session, the one the registration worker uses, with
only the operation, the request digest, the release and the release pin on its
standard input.
It returns closed reasons, states and counts only; no native output and no
repository text reaches its result.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_update
short_description: Update an installed machine within its release
version_added: "0.1.0"
description:
  - Runs one fixed worker as root through C(sudo -n) on the installed machine
    over the installation's pinned SSH session, built option for option as the
    registration worker's is, with host-key algorithms the placement host's
    SSH policy permits.
  - C(apply) proves that VERSION_ID in /etc/os-release equals the release,
    resolves with the guest's own dnf the transaction C(dnf upgrade) would run
    and refuses one that moves redhat-release to another version, then runs
    C(dnf upgrade --refresh --assumeyes --comment=bootwright:<request>) and
    records the history transaction in the root-only record
    /etc/bootwright/update.json; C(complete) proves the release again after
    the caller rebooted the machine and records the update complete.
    C(inspect) reads the record, the release and the history and changes
    nothing.
  - Returns closed reasons only. The operator texts the controller renders for
    them name C(Machine/<m>), the reason and the next step.
  - C(update-release-mismatch) the machine reports another VERSION_ID, which
    C(detail) names.
  - C(update-release-drift) the transaction would move redhat-release to the
    version and from the repository C(detail) names as C(<found>@<repository>).
  - C(update-no-repository) the machine has no enabled repository.
  - C(update-foreign) the machine holds an update record of another
    installation; nothing was changed.
  - C(update-unavailable) a repository did not answer, C(metadata),
    C(download) or, with too little of the session left to start the upgrade,
    C(timeout).
  - C(update-failed) the transaction failed, C(resolution), C(transaction),
    C(timeout), C(history) or C(tooling).
  - C(transport) the update session failed.
options:
  operation:
    description: What the worker does.
    type: str
    required: true
    choices: [apply, complete, inspect]
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
  release:
    description: The frozen release, <major>.<minor>.
    type: str
    required: true
  pin:
    description:
      - The release pin the registration left, C(release), C(contentView) or
        C(variable), for apply and complete; the record keeps it. Empty for
        inspect, which reports the recorded one.
    type: str
    default: ""
  timeout:
    description: Seconds the whole session may take, from 1 to 1800.
    type: int
    default: 1800
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Update the installed machine within its release
  bootwright.core.managedos_update:
    operation: apply
    address: 192.0.2.10
    user: fleet
    identity: "{{ scratch }}/identity"
    ssh_config: "{{ scratch }}/ssh_config"
    known_hosts: "{{ scratch }}/known_hosts"
    host_key_algorithms: ssh-ed25519
    policy_host_key_algorithms: ssh-ed25519,ecdsa-sha2-nistp256
    request: "{{ request_digest }}"
    release: "9.8"
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
  choices: [update-release-mismatch, update-release-drift, update-no-repository, update-foreign,
            update-unavailable, update-failed, transport]
cause:
  description: The closed cause of update-unavailable, update-failed or a transport refusal.
  returned: when the reason has one
  type: str
  choices: [metadata, download, resolution, transaction, timeout, history, tooling, session, host-key, exit, answer, unreachable]
detail:
  description: The version found, or C(<found>@<repository>), of a closed character set; or fixed text naming a refused session check.
  returned: for update-release-mismatch, update-release-drift and a refused session
  type: str
updated:
  description: Whether the worker ran or resumed an update, which a caller's changed_when does not override.
  returned: when ok is true
  type: bool
evidence:
  description: The worker's result, the fields the installation evidence carries as its update.
  returned: when ok is true
  type: dict
  contains:
    state:
      description: pending, upgraded or complete; inspect also absent or foreign.
      type: str
    release:
      description: The release proved, or an empty string for absent or foreign.
      type: str
    pin:
      description: The release pin recorded, or an empty string for absent or foreign.
      type: str
    transaction:
      description: The dnf history transaction carrying the request's comment, 0 when nothing changed.
      type: int
    packages:
      description: The packages that transaction installed or upgraded.
      type: int
    rebooted:
      description: Whether a complete update rebooted the machine, which it does when the transaction changed a package.
      type: bool
    kernel:
      description: The kernel the machine runs.
      type: str
"""

import inspect
import json
import re

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh, managedos_update_worker as worker, remote_command
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

MAX_TIMEOUT = 1800
DETAIL = re.compile(r"[A-Za-z0-9._+~-]{1,64}(@[A-Za-z0-9._:-]{1,90})?")
RESULT_KEYS = frozenset(("changed", "kernel", "packages", "pin", "rebooted", "release", "state", "transaction"))


class ArgumentError(ValueError):
    """An argument the module refuses; its text names the option, never a value."""


def worker_source():
    return inspect.getsource(worker) + "\nraise SystemExit(main())\n"


def check_arguments(params):
    if not isinstance(params["request"], str) or not worker.DIGEST.fullmatch(params["request"]):
        raise ArgumentError("request is not 64 lowercase hexadecimal characters")
    if not isinstance(params["release"], str) or not worker.RELEASE.fullmatch(params["release"]):
        raise ArgumentError("release is not a <major>.<minor> release")
    if type(params["timeout"]) is not int or not 1 <= params["timeout"] <= MAX_TIMEOUT:  # pylint: disable=unidiomatic-typecheck
        raise ArgumentError("timeout is not between 1 and 1800 seconds")
    if params.get("pin", "") not in (("",) if params["operation"] == "inspect" else worker.PINS):
        raise ArgumentError("pin is release, contentView or variable for apply and complete, and empty for inspect")


def refusal(reason, cause=None, detail=None):
    outcome = {"changed": False, "ok": False, "reason": reason}
    if cause is not None:
        outcome["cause"] = cause
    if detail is not None:
        outcome["detail"] = detail
    return outcome


def counted(value):
    return type(value) is int and value >= 0  # pylint: disable=unidiomatic-typecheck


def valid_pin(operation, recorded, value, pin):
    if operation == "inspect":
        return value in (worker.PINS if recorded else ("",))
    return value == pin


def valid_result(operation, release, value, pin=""):
    if not isinstance(value, dict) or set(value) != RESULT_KEYS or value["state"] not in worker.STATES[operation]:
        return False
    recorded = value["state"] in worker.RECORD_STATES
    if not valid_pin(operation, recorded, value["pin"], pin):
        return False
    return (type(value["changed"]) is bool and type(value["rebooted"]) is bool  # pylint: disable=unidiomatic-typecheck
            and counted(value["transaction"]) and counted(value["packages"])
            and value["rebooted"] is (value["state"] == "complete" and value["transaction"] != 0)
            and isinstance(value["kernel"], str) and bool(worker.KERNEL.fullmatch(value["kernel"]))
            and value["release"] == (release if recorded else "")
            and (recorded or (value["transaction"], value["packages"]) == (0, 0)))


def valid_refusal(value):
    if not {"ok", "reason"} <= set(value) <= {"ok", "reason", "cause", "detail"} or value["reason"] not in worker.REASONS:
        return False
    causes = worker.CAUSES.get(value["reason"], ())
    if ("cause" in value) != bool(causes) or ("cause" in value and value["cause"] not in causes):
        return False
    if "detail" in value:
        detail = value["detail"]
        return value["reason"] in worker.DETAILED and isinstance(detail, str) and bool(DETAIL.fullmatch(detail)) \
            and ("@" in detail) == (value["reason"] == "update-release-drift")
    return True


def parse_answer(operation, release, stdout, pin=""):
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
        return value if set(value) == {"ok", "result"} and valid_result(operation, release, value["result"], pin) else None
    return value if valid_refusal(value) else None


def interpret(operation, release, outcome, pin=""):
    if outcome.timed_out:
        return refusal("transport", "timeout")
    if outcome.cause is not None:
        return refusal("transport", outcome.cause)
    if outcome.code == 255:
        return refusal("transport", "host-key" if outcome.host_key else "unreachable")
    if outcome.code != 0:
        return refusal("transport", "exit")
    value = parse_answer(operation, release, outcome.stdout, pin)
    if value is None:
        return refusal("transport", "answer")
    if value["ok"]:
        update = dict(value["result"])
        updated = update.pop("changed")
        return {"changed": updated, "evidence": update, "ok": True, "updated": updated}
    return refusal(value["reason"], value.get("cause"), value.get("detail"))


def execute(params, run=managedos_guest_ssh.run):
    """The module result; only the operation, the digest, the release and the pin reach the worker."""
    check_arguments(params)
    pin = params.get("pin", "")
    payload = canonical({"operation": params["operation"], "pin": pin, "release": params["release"], "request": params["request"]})
    remote_argv = remote_command.fixed_worker_argv(worker_source())
    try:
        argv = managedos_guest_ssh.guest_argv(
            ssh_config=params["ssh_config"], known_hosts=params["known_hosts"],
            host_key_algorithms=params["host_key_algorithms"], policy_host_key_algorithms=params["policy_host_key_algorithms"],
            identity=params["identity"], user=params["user"], address=params["address"], remote_argv=remote_argv)
    except managedos_guest_ssh.SessionError as error:
        return refusal("transport", "session", str(error))
    return interpret(params["operation"], params["release"], run(argv, payload, params["timeout"]), pin)


def main():
    module = AnsibleModule(
        argument_spec={
            "operation": {"type": "str", "required": True, "choices": ["apply", "complete", "inspect"]},
            "address": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "identity": {"type": "path", "required": True},
            "ssh_config": {"type": "path", "required": True},
            "known_hosts": {"type": "path", "required": True},
            "host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "policy_host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "request": {"type": "str", "required": True},
            "release": {"type": "str", "required": True},
            "pin": {"type": "str", "default": ""},
            "timeout": {"type": "int", "default": 1800},
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
