#!/usr/bin/python
"""Observe one managed artifact server and, when asked, prove its listeners."""

from __future__ import annotations

DOCUMENTATION = r"""
module: artifact_server_inspect
short_description: Observe one Bootwright managed artifact server
version_added: "0.1.0"
description:
  - Reports the owned unit, container, content root and, when readiness is
    requested, the answer each declared listener gives.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen artifact-server request.
    type: dict
    required: true
  readiness:
    description: Whether to prove every declared listener answers.
    type: bool
    default: false
  attempts:
    description: Bounded readiness retries before the listener is unproved.
    type: int
    default: 30
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the managed artifact server
  bootwright.core.artifact_server_inspect:
    request: '{{ bootwright_artifact_server_request }}'
"""

RETURN = r"""
observation:
  description: The owned host resources and their observed state.
  returned: always
  type: dict
listeners:
  description: One entry per declared listener socket when readiness was requested.
  returned: when readiness is requested
  type: list
  elements: dict
unproved:
  description: Listener sockets that did not answer within the bounded window.
  returned: always
  type: list
  elements: str
"""

import time

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.artifact_server import (
    observe,
    probe,
    probe_targets,
)

MAX_ATTEMPTS = 120
RETRY_DELAY = 1


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "readiness": {"type": "bool", "default": False},
            "attempts": {"type": "int", "default": 30},
        },
        supports_check_mode=True,
    )
    request = module.params["request"]
    attempts = max(1, min(int(module.params["attempts"]), MAX_ATTEMPTS))
    try:
        observation = observe(module.run_command, request)
    except (OSError, ValueError) as failure:
        module.fail_json(msg="the artifact server could not be observed: %s" % type(failure).__name__)
        return
    result = {"changed": False, "observation": observation, "unproved": []}
    if not module.params["readiness"]:
        module.exit_json(**result)
        return
    listeners, unproved = [], []
    for target in probe_targets(request):
        answer, last = None, None
        for remaining in range(attempts):
            try:
                answer = probe(target)
                break
            except (OSError, ValueError) as failure:
                last = type(failure).__name__
                if remaining + 1 < attempts:
                    time.sleep(RETRY_DELAY)
        if answer is None:
            unproved.append("%s %s:%d (%s)" % (target["name"], target["address"], target["port"], last))
            continue
        listeners.append(answer)
    result["listeners"] = listeners
    result["unproved"] = unproved
    module.exit_json(**result)


if __name__ == "__main__":
    main()
