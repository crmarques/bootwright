"""Run the controller-local discovery procedure with live protocol progress."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.action import managedos_disk_discovery_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_progress as progress
from ansible_collections.bootwright.core.plugins.module_utils.disk_discovery_runtime import execute, validate_arguments


def report(group, status):
    if group not in progress.GROUPS or status not in adapter_protocol.GROUP_STATUSES:
        raise ValueError("discovery progress group")
    result = adapter_protocol.publish({"phase": "group", "group": group, "status": status}, protocol.CAPABILITY, protocol.emit)
    if result.get("failed"):
        raise ValueError("discovery progress channel")


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            validate_arguments(self._task.args)
            result.update(execute(self._task.args, report))
        except Exception:
            # No exception text, material path or native answer crosses the
            # action boundary. The enclosing role presents bounded causes.
            result.update(failed=True, discovery_failure="contract", discovery_phase="custody",
                          msg="disk discovery did not return a bounded result")
        return result
