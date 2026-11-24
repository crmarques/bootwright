"""Publish a provided operating system hostname's bounded evidence."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit
from ansible_collections.bootwright.core.plugins.module_utils.managedos_provided_hostname import REFUSALS, STATES, valid_hostname

VERSION = "provided-os-hostname-evidence-v1"


def completion(arguments):
    hostname = arguments.get("hostname")
    answer = arguments.get("answer")
    if not valid_hostname(hostname):
        raise ValueError("provided hostname")
    if (not isinstance(answer, dict) or answer.get("ok") is not True or answer.get("state") not in STATES
            or answer.get("hostname") != hostname):
        raise ValueError("provided hostname answer")
    state, changed = answer["state"], answer.get("changed")
    if type(changed) is not bool or changed and (state != "complete" or arguments.get("observed") is True):  # pylint: disable=unidiomatic-typecheck
        raise ValueError("provided hostname postcondition")
    if arguments.get("observed") is not True and state != "complete":
        return None, "the provided host hostname did not prove completion"
    return {"version": VERSION, "request": adapter_protocol.request_digest(arguments.get("digest")),
            "hostname": hostname, "state": state, "postcondition": state == "complete", "changed": changed}, None


CAPABILITY = adapter_protocol.Capability(
    "provided-hostname",
    "the provided host hostname result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
