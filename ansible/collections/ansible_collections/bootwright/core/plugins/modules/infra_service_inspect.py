#!/usr/bin/python
"""Observe one managed network service and, when asked, prove it answers."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infra_service_inspect
short_description: Observe one Bootwright managed network service
version_added: "0.1.0"
description:
  - Reports the owned unit, container and content root and, when readiness is
    requested, the answer the service gives on each declared address.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen managed-service request.
    type: dict
    required: true
  readiness:
    description: Whether to prove the service answers.
    type: bool
    default: false
  attempts:
    description: Bounded readiness retries before the service is unproved.
    type: int
    default: 30
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the managed proxy
  bootwright.core.infra_service_inspect:
    request: '{{ bootwright_proxy_request }}'
"""

RETURN = r"""
observation:
  description: The owned host resources and their observed state.
  returned: always
  type: dict
answers:
  description: One entry per declared address when readiness was requested.
  returned: when readiness is requested
  type: list
  elements: dict
unproved:
  description: Addresses that did not answer within the bounded window.
  returned: always
  type: list
  elements: str
"""

import time

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils.infra_service import (
    observe,
    probe_dns,
    probe_http,
    probe_ntp,
    probe_targets,
)

MAX_ATTEMPTS = 120
RETRY_DELAY = 1


def answer_for(request, address):
    """Prove the one answer this kind of service exists to give."""
    kind, port = request["kind"], int(request["port"])
    if kind == "Proxy":
        return probe_http(address, port)
    if kind == "DNSServer":
        records = request.get("records") or []
        if not records:
            raise ValueError("records")
        return probe_dns(address, port, records[0]["name"])
    if kind == "NTPServer":
        return probe_ntp(address, port)
    raise ValueError("kind")


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
        module.fail_json(msg="the managed service could not be observed: %s" % type(failure).__name__)
        return
    result = {"changed": False, "observation": observation, "unproved": []}
    if not module.params["readiness"]:
        module.exit_json(**result)
        return
    answers, unproved = [], []
    for address in probe_targets(request):
        answer, last = None, None
        for remaining in range(attempts):
            try:
                answer = answer_for(request, address)
                break
            except (OSError, ValueError) as failure:
                last = type(failure).__name__
                if remaining + 1 < attempts:
                    time.sleep(RETRY_DELAY)
        if answer is None:
            unproved.append("%s:%s (%s)" % (address, request["port"], last))
            continue
        answers.append({"address": address, "answer": answer, "port": int(request["port"])})
    result["answers"] = answers
    result["unproved"] = unproved
    module.exit_json(**result)


if __name__ == "__main__":
    main()
