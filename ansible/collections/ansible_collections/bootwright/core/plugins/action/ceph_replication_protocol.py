"""Publish the Ceph replication capability on the shared bounded result channel."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_replication
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    observed = arguments.get("observed", False)
    removed = arguments.get("removed", False)
    if type(observed) is not bool or removed is not False:
        raise ValueError("operation")
    result = ceph_replication.evidence(arguments.get("evidence"), arguments.get("digest"))
    if observed or result["ready"]:
        return result, None
    return result, "the Ceph replication operation has not proved its postcondition"


CAPABILITY = adapter_protocol.Capability(
    "Ceph replication", "the Ceph replication capability result could not be published", completion,
    phases=("loaded", "refused", "completed"), refusals=ceph_replication.REASONS, detail=True, native_error=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
