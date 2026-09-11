from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    prepare_tool,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            args = self._task.args
            if set(args) != {
                "bundle",
                "tool",
                "egress",
                "inspect_only",
            } or not isinstance(args["inspect_only"], bool):
                raise ValueError("request")
            result.update(
                prepare_tool(
                    args["bundle"], args["tool"], args["egress"], args["inspect_only"]
                )
            )
            return result
        except Exception:
            # Exceptions can include source/proxy paths. Evidence and diagnostics
            # cross the public boundary only through the enclosing fixed role.
            return dict(
                result,
                failed=True,
                msg="The exact target tool could not be prepared or verified.",
            )
