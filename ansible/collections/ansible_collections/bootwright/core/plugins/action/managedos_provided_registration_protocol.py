"""Publish a provided registration's bounded evidence."""

from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit
from ansible_collections.bootwright.core.plugins.module_utils.managedos_provided_registration import REFUSALS, STATES

VERSION = "provided-os-registration-evidence-v1"
CONSUMER = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")


def completion(arguments):
    answer = arguments.get("answer")
    if not isinstance(answer, dict) or answer.get("ok") is not True or answer.get("state") not in STATES:
        raise ValueError("provided registration answer")
    state, consumer, postcondition = answer["state"], answer.get("consumer"), answer.get("postcondition")
    if not isinstance(consumer, str) or type(postcondition) is not bool:  # pylint: disable=unidiomatic-typecheck
        raise ValueError("provided registration evidence")
    named = state in ("converged", "unchanged")
    if (state == "absent" and consumer) or (named and not consumer) or (consumer and not CONSUMER.fullmatch(consumer)) or postcondition != named:
        raise ValueError("provided registration postcondition")
    observed = arguments.get("observed") is True
    if not observed and not postcondition:
        return None, "the provided host registration did not prove completion"
    if observed and state == "unchanged":
        raise ValueError("provided registration observation")
    return {"version": VERSION, "request": adapter_protocol.request_digest(arguments.get("digest")),
            "state": state, "consumer": consumer, "postcondition": postcondition}, None


CAPABILITY = adapter_protocol.Capability(
    "provided-registration",
    "the provided host registration result could not be published",
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
