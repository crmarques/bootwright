"""Publish the Ceph export capability on the shared bounded result channel."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_core, ceph_export_native, ceph_native
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def export_evidence(value, digest):
    fields = {"version", "name", "owner", "fsid", "request", "ready", "leader", "address", "keyType", "principals", "rgwUser", "entries"}
    ceph_native.require(isinstance(value, dict) and set(value) == fields, "evidence")
    ceph_native.require(value["version"] == ceph_export_native.EVIDENCE_VERSION and value["request"] == digest
                        and ceph_native.digest(digest) and type(value["ready"]) is bool, "evidence")
    ceph_native.require(all(ceph_native.named(value[key]) for key in ("name", "owner", "fsid")), "evidence")
    return value


def completion(arguments):
    observed = arguments.get("observed", False)
    removed = arguments.get("removed", False)
    if type(observed) is not bool or type(removed) is not bool:
        raise ValueError("operation")
    result = export_evidence(arguments.get("evidence"), arguments.get("digest"))
    if observed or result["removed" if removed else "ready"]:
        return result, None
    return result, "the Ceph export operation has not proved its postcondition"


CAPABILITY = adapter_protocol.Capability(
    "Ceph export", "the Ceph export capability result could not be published", completion,
    phases=("loaded", "refused", "completed"), refusals=ceph_core.CLUSTER_REASONS + ceph_export_native.REASONS, detail=True, native_error=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
