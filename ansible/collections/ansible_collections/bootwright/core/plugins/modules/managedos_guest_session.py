#!/usr/bin/python
"""Open or close one pinned guest SSH connection's files on its placement host.

The module runs on the placement host the connection starts from. Opening
reads that host's effective SSH crypto policy, writes the one-connection
known_hosts that holds only the key the identity channel reported and the
reduced client configuration that keeps only the policy's crypto directives,
and names the host-key algorithms the key is accepted under. Closing removes
both files. Every pinned guest session of the collection is opened here and
run by managedos_guest_command or a fixed worker module over the same files.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: managedos_guest_session
short_description: Open or close one pinned guest SSH connection's files
version_added: "0.1.0"
description:
  - C(present) reads the placement host's effective SSH crypto policy, writes
    C(<directory>/known_hosts), holding C(<address> <host_key>) and nothing
    else, and C(<directory>/ssh_config), the policy's crypto directives only,
    each mode 0600, and returns their paths with the host-key algorithms the
    pinned key is accepted under within the policy.
  - C(absent) removes both files.
  - Fails, before any connection, on a policy the host cannot evaluate, a
    pinned key type the policy excludes or no installation pins, an address
    that is neither an IP literal nor a DNS name, or a directory that is not
    one; C(reason) names a policy refusal.
options:
  state:
    description: Whether the connection's files are written or removed.
    type: str
    required: true
    choices:
      - present
      - absent
  directory:
    description: The private directory that holds the connection's files.
    type: path
    required: true
  address:
    description: The guest's address, as the connection names it.
    type: str
  host_key:
    description: The pinned OpenSSH public key line the identity channel reported.
    type: str
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Open the pinned connection
  bootwright.core.managedos_guest_session:
    state: present
    directory: "{{ scratch }}"
    address: 192.0.2.10
    host_key: "{{ reported_host_key }}"
  register: session

- name: Close the pinned connection
  bootwright.core.managedos_guest_session:
    state: absent
    directory: "{{ scratch }}"
"""

RETURN = r"""
ssh_config:
  description: The reduced client configuration file.
  returned: when state is present
  type: str
known_hosts:
  description: The one-connection known_hosts file.
  returned: when state is present
  type: str
host_key_algorithms:
  description: The host-key algorithms the pinned key is accepted under.
  returned: when state is present
  type: str
policy_host_key_algorithms:
  description: The host-key algorithms the placement host's policy permits.
  returned: when state is present
  type: str
reason:
  description: The closed reason of a policy refusal.
  returned: on a policy refusal
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh, ssh_key_policy


def execute(params, policy=lambda: ssh_key_policy.effective_policy(ssh_key_policy.read_policy())):
    """The module result for one call."""
    if params["state"] == "absent":
        managedos_guest_ssh.close_session(params["directory"])
        return {"changed": False}
    if not params.get("address") or not params.get("host_key"):
        raise managedos_guest_ssh.SessionError("opening a session requires address and host_key")
    session = managedos_guest_ssh.open_session(params["directory"], params["address"], params["host_key"], policy())
    return dict(session, changed=False)


def main():
    module = AnsibleModule(
        argument_spec={
            "state": {"type": "str", "required": True, "choices": ["present", "absent"]},
            "directory": {"type": "path", "required": True},
            "address": {"type": "str"},
            "host_key": {"type": "str", "no_log": False},
        },
        supports_check_mode=False,
    )
    try:
        outcome = execute(module.params)
    except ssh_key_policy.PolicyError as refused:
        module.fail_json(msg=str(refused), reason=refused.reason)
    except managedos_guest_ssh.SessionError as refused:
        module.fail_json(msg=str(refused))
    module.exit_json(**outcome)


if __name__ == "__main__":
    main()
