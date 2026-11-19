"""Publish only bounded administrator credential proof."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_core
from ansible_collections.bootwright.core.plugins.module_utils.admin_credentials_protocol import completion
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

CAPABILITY = adapter_protocol.Capability(
    "Cluster administrator credentials", "credential proof publication failed", completion,
    phases=adapter_protocol.REFUSING_PHASES, refusals=ceph_core.ADMIN_REASONS,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
