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
reason:
  description: The agent's own refusal when it did not answer, bounded to one line.
  returned: always
  type: str
"""

import base64
import json

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import virsh_reason

# ALLOWED is the closed set this operation may read. A consumer that needs
# another file adds it here deliberately, so the channel can never be used to
# exfiltrate arbitrary guest state. Both entries are written by the
# installation itself: a confined guest agent cannot read sshd's own key
# directory, and the policy that would permit it also reaches the private
# halves, so the installation republishes the public key it owns instead.
ALLOWED = (
    "/etc/bootwright/install-marker.json",
    "/etc/bootwright/host-key.pub",
)

MAX_LIMIT = 1 << 16
REASON_LIMIT = 200


class Unanswered(ValueError):
    """An agent that did not answer, carrying its own refusal."""

    def __init__(self, reason):
        super().__init__(reason)
        self.reason = reason


def diagnosis(text):
    """The first meaningful line the agent refused with, bounded."""
    for line in (text or "").splitlines():
        stripped = line.strip()
        if stripped:
            return stripped[:REASON_LIMIT]
    return "the guest agent returned nothing"


def agent(runner, uri, domain, payload):
    code, output, error = virsh_reason(runner, uri, "qemu-agent-command", domain, json.dumps(payload))
    if code != 0 or not output.strip():
        raise Unanswered(diagnosis(error))
    return json.loads(output).get("return")


def read_file(runner, uri, domain, path, limit):
    handle = agent(runner, uri, domain, {"execute": "guest-file-open", "arguments": {"path": path, "mode": "r"}})
    if not isinstance(handle, int):
        raise Unanswered("the guest agent opened no handle")
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
    except Unanswered as unanswered:
        module.exit_json(changed=False, answered=False, content="", reason=unanswered.reason)
        return
    except (OSError, ValueError, TypeError):
        module.exit_json(changed=False, answered=False, content="", reason="the guest agent answer could not be read")
        return
    module.exit_json(changed=False, answered=True, content=content, reason="")


if __name__ == "__main__":
    main()
