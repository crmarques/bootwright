"""Publish a provided time synchronization's bounded evidence."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit
from ansible_collections.bootwright.core.plugins.module_utils.managedos_provided_time import REFUSALS, STATES, valid_servers

VERSION = "provided-os-time-evidence-v1"


def completion(arguments):
    servers, answer = arguments.get("servers"), arguments.get("answer")
    if not valid_servers(servers):
        raise ValueError("provided time servers")
    if not isinstance(answer, dict) or answer.get("ok") is not True or answer.get("state") not in STATES or answer.get("servers") != servers:
        raise ValueError("provided time answer")
    state, changed, postcondition = answer["state"], answer.get("changed"), answer.get("postcondition")
    observed = arguments.get("observed") is True
    if type(changed) is not bool or type(postcondition) is not bool:  # pylint: disable=unidiomatic-typecheck
        raise ValueError("provided time evidence")
    if postcondition != (state == "complete") or changed and (state != "complete" or observed):
        raise ValueError("provided time postcondition")
    if not observed and not postcondition:
        return None, "the provided host time synchronization did not prove completion"
    return {"version": VERSION, "request": adapter_protocol.request_digest(arguments.get("digest")),
            "servers": servers, "state": state, "postcondition": postcondition, "changed": changed}, None


CAPABILITY = adapter_protocol.Capability(
    "provided-time",
    "the provided host time synchronization result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
    detail=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
