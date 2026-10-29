"""Publish the core Ceph capability on the shared bounded result channel."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_core
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    observed = arguments.get("observed", False)
    removed = arguments.get("removed", False)
    if type(observed) is not bool or type(removed) is not bool:
        raise ValueError("operation")
    value = arguments.get("evidence")
    if isinstance(value, dict) and value.get("version") == ceph_core.TIEBREAKER_EVIDENCE:
        result = ceph_core.tiebreaker_evidence(value, arguments.get("digest"))
        if observed or removed and result["removed"]:
            return result, None
        return result, "the Ceph operation has not proved its postcondition"
    result = ceph_core.evidence(value, arguments.get("digest"))
    if observed or result["removed" if removed else "ready"]:
        return result, None
    return result, "the Ceph operation has not proved its postcondition"


REFUSALS = tuple(dict.fromkeys(ceph_core.HOST_REASONS + ceph_core.CLUSTER_REASONS))
CAPABILITY = adapter_protocol.Capability(
    "Ceph core", "the Ceph capability result could not be published", completion,
    phases=adapter_protocol.REFUSING_PHASES, refusals=REFUSALS, detail=True, native_error=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
