"""Publish only the pinned Ceph image resolution on the shared channel."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_registry_discovery as discovery
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    if arguments.get("outcome") != "unchanged":
        raise ValueError("outcome")
    return discovery.evidence(arguments.get("evidence"), adapter_protocol.request_digest(arguments.get("digest"))), None


CAPABILITY = adapter_protocol.Capability(
    "Ceph image", "the Ceph image resolution could not be published", completion,
    phases=("loaded", "refused", "completed"), refusals=discovery.REASONS, detail=True, native_error=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
