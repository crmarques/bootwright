#!/usr/bin/python
"""Prove bound key types are eligible under the actual SSH placement policy."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ssh_key_policy
short_description: Check bound key types against placement-host SSH policy
version_added: "0.1.0"
description:
  - Reads the bounded system crypto backend on the machine that runs SSH.
  - Evaluates a closed crypto-only configuration with OpenSSH C(-G), without a connection.
  - Rejects excluded bound key types before installation effects or waits.
  - Missing backend files use native compiled defaults. This is algorithm policy evidence,
    not certification of a crypto provider or a target operating system.
options:
  keys:
    description:
      - Bound public key types and their owning Secret names, and the host key
        type an installation generates and the Machine it generates it for;
        no key bytes.
    type: list
    elements: dict
    default: []
    suboptions:
      secret:
        description: The Secret whose bound key is checked; required for C(host) and C(fleet).
        type: str
        required: false
      machine:
        description: The Machine whose installation generates the checked host key; required for C(generated).
        type: str
        required: false
      type:
        description: The bound or generated OpenSSH public key type.
        type: str
        required: true
      usage:
        description: Host authentication, fleet user authentication, or the host key an installation generates.
        type: str
        choices: [host, fleet, generated]
        required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Check a bound delivered host key before boot
  bootwright.core.ssh_key_policy:
    keys:
      - secret: installed-host-key
        type: ssh-ed25519
        usage: host

- name: Check the host key type a guest-agent installation generates before boot
  bootwright.core.ssh_key_policy:
    keys:
      - machine: rhel-01
        type: ecdsa-sha2-nistp256
        usage: generated
"""

RETURN = r"""
configuration:
  description: Validated crypto-only configuration for the subsequent connection.
  returned: success
  type: str
hostKeyAlgorithms:
  description: Effective native host key algorithm list.
  returned: success
  type: str
fleetKeyAlgorithms:
  description: Effective native user authentication algorithm list.
  returned: success
  type: str
"""

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.ssh_key_policy import (
    PolicyError, check_keys, effective_policy, read_policy,
)


def main():
    module = AnsibleModule(argument_spec={
        "keys": {"type": "list", "elements": "dict", "default": [], "no_log": False, "options": {
            "secret": {"type": "str", "required": False, "no_log": False},
            "machine": {"type": "str", "required": False, "no_log": False},
            "type": {"type": "str", "required": True},
            "usage": {"type": "str", "choices": ["host", "fleet", "generated"], "required": True},
        }},
    }, supports_check_mode=True)
    try:
        policy = effective_policy(read_policy())
        check_keys(policy, module.params["keys"])
    except PolicyError as exc:
        module.fail_json(msg=str(exc), reason=exc.reason)
    module.exit_json(changed=False, **policy)


if __name__ == "__main__":
    main()
