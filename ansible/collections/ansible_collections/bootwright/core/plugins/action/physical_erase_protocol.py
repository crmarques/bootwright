"""Publish and durably acknowledge the offline erase receipt."""

from __future__ import annotations
from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    evidence = arguments.get("evidence")
    if not isinstance(evidence, dict) or evidence.get("request") != adapter_protocol.request_digest(arguments.get("digest")):
        raise ValueError("erase evidence")
    if arguments.get("observed") or evidence.get("removed") is True:
        return evidence, None
    return evidence, "offline erase has not proved its complete receipt and cleanup"


# The refusals an erase phase names to its runner before the run fails, each
# one the runner reports as the erase's own diagnostic for its Machine
# (internal/managedos/installation, erase_refusals.go); the detail is the
# bounded value the controller or the worker reported.
REFUSALS = (
    "machine-running", "identity-mismatch", "hardware-mismatch", "target-unanswered", "controller-refused", "controller-deadline",
    "poweroff-unproved", "worker-failed", "worker-unreachable", "foreign-worker", "receipt-timeout", "foreign-custody", "custody-drift",
    "boot-media-mismatch", "image-build-failed", "install-network-unsupported",
)

CAPABILITY = adapter_protocol.Capability(
    "physical erase",
    "physical erase evidence could not be published",
    completion,
    phases=adapter_protocol.PREPARING_PHASES,
    refusals=REFUSALS,
    detail=True,
    preparation=lambda args: args["receipt"],
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
