"""Read-only inventory through the supplied native package manager."""

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_native import (
    inspection,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            if set(self._task.args) != {"platform", "plan"}:
                raise ValueError("request")
            observed = inspection(self._task.args["platform"], self._task.args["plan"])
            if set(observed) != {"inventory", "inventorySHA256", "rootsReady"}:
                raise ValueError("native inventory result")
            return dict(result, **observed)
        except (KeyError, TypeError, ValueError, OSError):
            return dict(
                result,
                failed=True,
                msg="Native package inventory or selected root verification was refused.",
            )
