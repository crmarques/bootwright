#!/usr/bin/python
"""Read one bounded guest file through the hypervisor's own agent channel.

This is the substrate's identity operation: it proves what a guest holds
without trusting the network, so an installation's marker and a guest's SSH
host key are read out of band rather than from a first-use answer.
"""

from __future__ import annotations

DOCUMENTATION = r"""
module: guest_agent_read
short_description: Read one bounded guest file through the QEMU guest agent
version_added: "0.1.0"
description:
  - Returns the bytes of exactly one allowed guest file, and nothing else.
  - A guest without the agent, or a file outside the allowed set, is unknown
    rather than an error, because absence of an answer proves nothing.
  - Performs no change on the guest and is safe to repeat.
options:
  uri:
    description: The libvirt connection the domain is defined on.
    type: str
    required: true
  domain:
    description: The domain to read from.
    type: str
    required: true
  path:
    description: The guest file to read.
    type: str
    required: true
  limit:
    description: The maximum number of bytes to return.
    type: int
    default: 4096
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Read the install marker
  bootwright.core.guest_agent_read:
    uri: qemu:///system
    domain: bootwright-lab-rhel-01
    path: /etc/bootwright/install-marker.json
"""

RETURN = r"""
content:
  description: The file's bytes as text, or the empty string when unknown.
  returned: always
  type: str
answered:
  description: Whether the guest agent answered at all.
  returned: always
  type: bool
"""

import base64
import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import virsh

# ALLOWED is the closed set this operation may read. A consumer that needs
# another file adds it here deliberately, so the channel can never be used to
# exfiltrate arbitrary guest state.
ALLOWED = (
    "/etc/bootwright/install-marker.json",
    "/etc/ssh/ssh_host_ed25519_key.pub",
    "/etc/ssh/ssh_host_rsa_key.pub",
)

MAX_LIMIT = 1 << 16


def agent(runner, uri, domain, payload):
    code, output = virsh(runner, uri, "qemu-agent-command", domain, json.dumps(payload))
    if code != 0 or not output.strip():
        raise ValueError("agent")
    return json.loads(output).get("return")


def read_file(runner, uri, domain, path, limit):
    handle = agent(runner, uri, domain, {"execute": "guest-file-open", "arguments": {"path": path, "mode": "r"}})
    if not isinstance(handle, int):
        raise ValueError("handle")
    try:
        answer = agent(runner, uri, domain, {
            "execute": "guest-file-read", "arguments": {"handle": handle, "count": limit},
        })
    finally:
        try:
            agent(runner, uri, domain, {"execute": "guest-file-close", "arguments": {"handle": handle}})
        except (ValueError, OSError):
            pass
    encoded = (answer or {}).get("buf-b64") or ""
    return base64.b64decode(encoded).decode("utf-8", "replace")


def main():
    module = AnsibleModule(
        argument_spec={
            "uri": {"type": "str", "required": True},
            "domain": {"type": "str", "required": True},
            "path": {"type": "str", "required": True},
            "limit": {"type": "int", "default": 4096},
        },
        supports_check_mode=True,
    )
    path = module.params["path"]
    if path not in ALLOWED:
        module.fail_json(msg="the identity operation may not read %s" % path)
        return
    limit = max(1, min(int(module.params["limit"]), MAX_LIMIT))
    try:
        content = read_file(module.run_command, module.params["uri"], module.params["domain"], path, limit)
    except (OSError, ValueError, TypeError):
        module.exit_json(changed=False, answered=False, content="")
        return
    module.exit_json(changed=False, answered=True, content=content)


if __name__ == "__main__":
    main()
