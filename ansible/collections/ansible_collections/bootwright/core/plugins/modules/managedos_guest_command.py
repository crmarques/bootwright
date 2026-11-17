#!/usr/bin/python
"""Run one fixed command on a managed machine over its pinned guest SSH session.

The module runs on the placement host. Its argv is the one guest_argv builds
over the files managedos_guest_session wrote, and the remote command is the
caller's argv joined by remote_command, so no task file writes an ssh option
or quotes a remote word. The run is bounded in time and output and starts
with a clean environment; standard input is the only way data reaches the
command.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_guest_command
short_description: Run one fixed command over a pinned guest SSH session
version_added: "0.1.0"
description:
  - Runs C(argv) as the fleet account on the installed machine over one new
    SSH connection built option for option by the collection's guest session
    builder, on exactly the key the session's known_hosts holds.
  - Returns the ssh exit status and both streams, each without its trailing
    line ending. A run that outran C(timeout), wrote more than 1 MiB of output
    or 64 KiB of diagnostics, could not start, or was refused before it
    started returns no C(rc) and names why in C(stderr) and C(failure); it
    never fails the task, so the caller names what failed at its own step.
options:
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
    description: The reduced placement-host policy configuration managedos_guest_session wrote.
    type: path
    required: true
  known_hosts:
    description: The pinned known_hosts file managedos_guest_session wrote.
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
  argv:
    description: The remote command as argv words, each non-empty.
    type: list
    elements: str
    required: true
  stdin:
    description: Bytes the command reads on standard input, at most 1 MiB.
    type: str
  timeout:
    description: Seconds the whole session may take, from 1 to 1800.
    type: int
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Read the boot id over the pinned session
  bootwright.core.managedos_guest_command:
    address: 192.0.2.10
    user: fleet
    identity: "{{ scratch }}/identity"
    ssh_config: "{{ session.ssh_config }}"
    known_hosts: "{{ session.known_hosts }}"
    host_key_algorithms: "{{ session.host_key_algorithms }}"
    policy_host_key_algorithms: "{{ session.policy_host_key_algorithms }}"
    argv:
      - /usr/bin/cat
      - /proc/sys/kernel/random/boot_id
    timeout: 120
"""

RETURN = r"""
rc:
  description: The ssh exit status, 255 for a connection ssh itself failed.
  returned: when the command ran to its end
  type: int
stdout:
  description: The command's standard output.
  returned: always
  type: str
stderr:
  description: The command's standard error, or the fixed text naming why it did not run to its end.
  returned: always
  type: str
failure:
  description: Why the command did not run to its end.
  returned: when rc is not returned
  type: str
  choices:
    - session
    - timeout
    - output
    - command
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh

MAX_TIMEOUT = 1800
MAX_STDIN = 1 << 20
FAILURES = {
    "timeout": "the guest command did not finish within %d seconds",
    "output": "the guest command wrote more than its output bound",
    "command": "the guest session could not be started",
}


def text(data):
    return data.decode("utf-8", "replace").rstrip("\r\n")


def failed(kind, detail):
    return {"changed": False, "stdout": "", "stderr": detail, "failure": kind}


def execute(params, runner=None):
    """The module result; nothing but the argv words and standard input reaches the guest."""
    timeout = params["timeout"]
    if type(timeout) is not int or not 1 <= timeout <= MAX_TIMEOUT:  # pylint: disable=unidiomatic-typecheck
        return failed("session", "the guest command timeout is not between 1 and 1800 seconds")
    data = (params.get("stdin") or "").encode("utf-8", "surrogateescape")
    if len(data) > MAX_STDIN:
        return failed("session", "the guest command standard input exceeds 1 MiB")
    try:
        argv = managedos_guest_ssh.guest_argv(
            ssh_config=params["ssh_config"], known_hosts=params["known_hosts"],
            host_key_algorithms=params["host_key_algorithms"], policy_host_key_algorithms=params["policy_host_key_algorithms"],
            identity=params["identity"], user=params["user"], address=params["address"], remote_argv=list(params["argv"] or []))
    except managedos_guest_ssh.SessionError as error:
        return failed("session", str(error))
    answer = managedos_guest_ssh.command(argv, data, timeout, runner or managedos_guest_ssh.bounded_process.run)
    if answer.failure is not None:
        detail = FAILURES[answer.failure]
        return failed(answer.failure, detail % timeout if "%d" in detail else detail)
    return {"changed": False, "rc": answer.code, "stdout": text(answer.stdout), "stderr": text(answer.stderr)}


def main():
    module = AnsibleModule(
        argument_spec={
            "address": {"type": "str", "required": True},
            "user": {"type": "str", "required": True},
            "identity": {"type": "path", "required": True},
            "ssh_config": {"type": "path", "required": True},
            "known_hosts": {"type": "path", "required": True},
            "host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "policy_host_key_algorithms": {"type": "str", "required": True, "no_log": False},
            "argv": {"type": "list", "elements": "str", "required": True},
            "stdin": {"type": "str"},
            "timeout": {"type": "int", "required": True},
        },
        supports_check_mode=False,
    )
    module.exit_json(**execute(module.params))


if __name__ == "__main__":
    main()
