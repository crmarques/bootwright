#!/usr/bin/python
"""Observe one managed network service or the artifact server and, when asked, prove it answers."""

from __future__ import annotations

DOCUMENTATION = r"""
module: infra_service_inspect
short_description: Observe one Bootwright managed network service or artifact server
version_added: "0.1.0"
description:
  - Reports the owned unit, container and content root, whether the container
    started after every file it runs from was last written and, when readiness
    is requested, the answer the service gives on each declared address or,
    for the artifact server, each declared listener.
  - The request's version names its kind; an artifact-server-nginx-v1 request
    is read as the artifact server, every other one as a managed network service.
  - Performs no change and is safe to repeat.
options:
  request:
    description: The frozen managed-service or artifact-server request.
    type: dict
    required: true
  runs_from:
    description:
      - Absolute paths of the files the daemon reads at its start. Its unit
        definition is always compared as well. The artifact server names its
        own from its listeners and takes none.
    type: list
    elements: str
    default: []
  readiness:
    description: Whether to prove the service answers.
    type: bool
    default: false
  foreign:
    description:
      - Whether to report every socket something other than this service holds
        on a port it binds, at an address its bind address overlaps. Read
        from the host's socket tables only while its unit is not active and
        no container of it exists, since otherwise the socket may be its own.
    type: bool
    default: false
  bind_address:
    description: Whether to prove an explicit bind address is usable on this host; wildcards need no assigned address.
    type: bool
    default: false
  attempts:
    description: Bounded readiness retries before the service is unproved.
    type: int
    default: 30
  authorization:
    description:
      - The base64 C(user:password) token a registry's readiness authenticates
        with. Never returned or logged; one opaque token, so the masking of a
        hidden argument cannot reach the evidence the module returns.
    type: str
    default: ''
  fingerprint:
    description:
      - The SHA-256 of the certificate a registry must present. A listener
        presenting another one, or a probe given none, is sent no credential.
      - For the artifact server, the bound serving certificate digest every
        HTTPS listener must present.
    type: str
    default: ''
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the managed proxy
  bootwright.core.infra_service_inspect:
    request: '{{ bootwright_managed_service_request }}'
"""

RETURN = r"""
failures:
  description: Classified readiness refusals, definite failures before timeouts.
  type: list
  elements: dict
  returned: always

observation:
  description: The owned host resources and their observed state.
  returned: always
  type: dict
answers:
  description:
    - One entry per declared address when readiness was requested; a registry's
      also names the SHA-256 fingerprint of the certificate it presented.
  returned: when readiness of a managed network service is requested
  type: list
  elements: dict
listeners:
  description: One entry per declared listener socket of the artifact server when readiness was requested.
  returned: when readiness of the artifact server is requested
  type: list
  elements: dict
foreign:
  description:
    - One entry per socket another daemon holds where this service binds, each
      with its transport, address and port, sorted and bounded.
  returned: when foreign is requested
  type: list
  elements: dict
bindAddressAssigned:
  description: Whether the explicit bind address is usable on this host, or the address is a wildcard.
  returned: when bind_address is requested
  type: bool
accepted:
  description:
    - Whether the active service answered every probe target, a registry with
      the credential it was handed and none of them presenting another
      certificate than the bound one.
  returned: when readiness of a managed network service is requested
  type: bool
unproved:
  description: Addresses or listener sockets that did not answer within the bounded window.
  returned: always
  type: list
  elements: str
"""

import socket
import time

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import artifact_server
from ansible_collections.bootwright.core.plugins.module_utils.host_addresses import bind_address_assigned
from ansible_collections.bootwright.core.plugins.module_utils.infra_service import (
    command_runner,
    foreign,
    observe,
    probe_dns,
    probe_failure,
    probe_http,
    probe_ntp,
    probe_registry,
    probe_targets,
    Refused,
)

MAX_ATTEMPTS = 120
RETRY_DELAY = 1
ARTIFACT_SERVER = "artifact-server-nginx-v1"


def answer_for(request, address, credential):
    """Prove the one answer this kind of service exists to give, and the certificate a registry presents."""
    kind, port = request["kind"], int(request["port"])
    if kind == "Proxy":
        return probe_http(address, port), None
    if kind == "DNSServer":
        return probe_dns(address, port, request.get("records") or []), None
    if kind == "NTPServer":
        return probe_ntp(address, port), None
    if kind == "Registry":
        return probe_registry(address, port, *credential)
    raise ValueError("kind")


def accepted(observation, failures, answers):
    """Whether the active service answered every probe target, none of them with a certificate other than the bound one."""
    return (observation.get("unit") == "active" and not failures and bool(answers)
            and not any(entry["answer"] == "certificate-mismatch" for entry in answers))


def service_readiness(params, request, attempts, observation):
    """What a managed network service answers on each declared address, and what it did not."""
    failures, answers, unproved = [], [], []
    credential = (params["authorization"], params["fingerprint"])
    for address in probe_targets(request):
        answer, fingerprint, last, cause = None, None, None, "listener-failed"
        for remaining in range(attempts):
            try:
                answer, fingerprint = answer_for(request, address, credential)
                break
            except (OSError, ValueError) as failure:
                last = probe_failure(failure)
                cause = "readiness-timeout" if isinstance(failure, socket.timeout) else "listener-failed"
                if isinstance(failure, Refused):
                    break
                if remaining + 1 < attempts:
                    time.sleep(RETRY_DELAY)
        if answer is None:
            failures.append({"port": int(request["port"]), "reason": cause})
            unproved.append("%s:%s (%s)" % (address, request["port"], last))
            continue
        entry = {"address": address, "answer": answer, "port": int(request["port"])}
        if fingerprint is not None:
            entry["fingerprint"] = fingerprint
        answers.append(entry)
    failures.sort(key=lambda failure: failure["reason"] == "readiness-timeout")
    return {"failures": failures, "answers": answers, "unproved": unproved, "accepted": accepted(observation, failures, answers)}


def certified(answer, target, fingerprint):
    """The listener's answer, or None and why, when an HTTPS listener did not prove the bound certificate."""
    if answer is None or target["protocol"] != "https" or not fingerprint:
        return answer, None, None
    observed = answer.get("fingerprint")
    if not isinstance(observed, str) or len(observed) != 64 or set(observed) - set("0123456789abcdef"):
        return None, "readiness-unproved", "certificate fingerprint was not proved"
    if observed != fingerprint:
        return None, "certificate-mismatch", "certificate differs from the bound fingerprint"
    return answer, None, None


def listener_readiness(params, request, attempts):
    """What each artifact-server listener answers, and what it did not."""
    failures, listeners, unproved = [], [], []
    for target in artifact_server.probe_targets(request):
        answer, last, cause = None, None, "listener-failed"
        for remaining in range(attempts):
            try:
                answer = artifact_server.probe(target)
                break
            except (OSError, ValueError) as failure:
                last = probe_failure(failure)
                cause = "readiness-timeout" if isinstance(failure, socket.timeout) else "listener-failed"
                if remaining + 1 < attempts:
                    time.sleep(RETRY_DELAY)
        answer, refused, why = certified(answer, target, params["fingerprint"])
        if refused is not None:
            cause, last = refused, why
        if answer is None:
            failures.append({"port": target["port"], "reason": cause})
            unproved.append("%s %s:%d (%s)" % (target["name"], target["address"], target["port"], last))
            continue
        listeners.append(answer)
    failures.sort(key=lambda failure: failure["reason"] in ("readiness-timeout", "readiness-unproved"))
    return {"failures": failures, "listeners": listeners, "unproved": unproved}


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "runs_from": {"type": "list", "elements": "str", "default": []},
            "readiness": {"type": "bool", "default": False},
            "foreign": {"type": "bool", "default": False},
            "bind_address": {"type": "bool", "default": False},
            "attempts": {"type": "int", "default": 30},
            "authorization": {"type": "str", "default": "", "no_log": True},
            "fingerprint": {"type": "str", "default": ""},
        },
        supports_check_mode=True,
    )
    request = module.params["request"]
    server = request.get("version") == ARTIFACT_SERVER
    attempts = max(1, min(int(module.params["attempts"]), MAX_ATTEMPTS))
    try:
        if server:
            observation = artifact_server.observe(command_runner(), request)
        else:
            observation = observe(command_runner(), request, module.params["runs_from"])
    except (OSError, ValueError) as failure:
        module.fail_json(msg="%s could not be observed: %s" % (
            "the artifact server" if server else "the managed service", type(failure).__name__))
        return
    result = {"changed": False, "observation": observation, "unproved": []}
    if module.params["bind_address"]:
        try:
            result["bindAddressAssigned"] = bind_address_assigned(request["bindAddress"])
        except (OSError, ValueError) as failure:
            module.fail_json(msg="the host's assigned addresses could not be proved: %s" % type(failure).__name__)
            return
    if module.params["foreign"]:
        try:
            result["foreign"] = (artifact_server.foreign if server else foreign)(request, observation)
        except (OSError, ValueError) as failure:
            module.fail_json(msg="the host's socket tables could not be read: %s" % type(failure).__name__)
            return
    if module.params["readiness"]:
        if server:
            result.update(listener_readiness(module.params, request, attempts))
        else:
            result.update(service_readiness(module.params, request, attempts, observation))
    module.exit_json(**result)


if __name__ == "__main__":
    main()
