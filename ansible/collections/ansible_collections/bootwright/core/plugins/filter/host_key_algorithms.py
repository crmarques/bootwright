"""Name the host-key algorithms one pinned SSH host key is accepted under.

A connection pinned to one host key negotiates the algorithm the server signs
with. The client's own default list, which a crypto policy narrows, may
prefer a type the pin does not hold and fail the connection although the
machine presents exactly the pinned key. The optional effective policy list
restricts the pin's algorithms without re-enabling a policy-excluded signature.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: host_key_algorithms
short_description: Name the host-key algorithms a pinned SSH host key is accepted under
version_added: "0.1.0"
description:
  - Reads the key type, the first whitespace-separated field of an OpenSSH
    public key line, and returns the value of the ssh client's
    C(HostKeyAlgorithms) option that accepts exactly that key.
  - An rsa key is accepted under its SHA-2 signature algorithms only.
  - When an allowed policy list is supplied, retains only its intersection
    with the pinned type; an empty intersection refuses.
  - Any other type, an empty line included, is refused, and the refusal never
    repeats the key itself.
options:
  _input:
    description: The pinned OpenSSH public key line.
    type: str
    required: true
  allowed:
    description: The effective native host key algorithm list on the SSH placement host.
    type: str
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Name the algorithms the pinned key is accepted under
  ansible.builtin.set_fact:
    pinned_algorithms: "{{ pinned_host_key | bootwright.core.host_key_algorithms }}"
"""

RETURN = r"""
_value:
  description: The comma-separated C(HostKeyAlgorithms) value for the key's type.
  type: str
"""

from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.module_utils.ssh_key_policy import ALGORITHMS


# ssh-rsa, the SHA-1 signature, is deliberately absent, unlike the controller's
# own host-key records (internal/trust/hostkeys.go), which accept it for hosts
# they did not install: an installed RHEL 9 sshd signs an rsa host key with
# SHA-2, and a controller under the FIPS crypto policy refuses SHA-1.
def host_key_algorithms(line, allowed=None):
    """The HostKeyAlgorithms value that accepts exactly the pinned key's type."""
    fields = line.split() if isinstance(line, str) else []
    if not fields:
        raise AnsibleFilterError("the pinned SSH host key is empty or missing; restore its identity proof before connecting")
    key_type = fields[0]
    if key_type in ALGORITHMS:
        candidates = ALGORITHMS[key_type].split(",")
        if allowed is not None:
            candidates = [value for value in candidates if value in allowed.split(",")]
        if not candidates:
            raise AnsibleFilterError("the pinned SSH host key type %s is excluded by the placement host SSH policy" % key_type)
        return ",".join(candidates)
    printable = "".join(character for character in key_type if character.isprintable())[:64]
    raise AnsibleFilterError("the pinned SSH host key's type %s is not one an installation pins" % printable)


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"host_key_algorithms": host_key_algorithms}
