from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    MAX_DEADLINE,
    prepare_tool,
)


def tool_arguments(args):
    """The fixed argument set, with the source's acquisition deadline in whole
    seconds as its request froze it."""
    if set(args) != {"bundle", "tool", "egress", "inspect_only", "deadline"}:
        raise ValueError("request")
    if not isinstance(args["inspect_only"], bool):
        raise ValueError("request")
    deadline = args["deadline"]
    if (
        not isinstance(deadline, int)
        or isinstance(deadline, bool)
        or not 1 <= deadline <= MAX_DEADLINE
    ):
        raise ValueError("deadline")
    return args["bundle"], args["tool"], args["egress"], deadline, args["inspect_only"]


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            bundle, tool, egress, deadline, inspect_only = tool_arguments(
                self._task.args
            )
            result.update(prepare_tool(bundle, tool, egress, deadline, inspect_only))
            return result
        except Exception:
            # Exceptions can include source/proxy paths. Evidence and diagnostics
            # cross the public boundary only through the enclosing fixed role.
            return dict(
                result,
                failed=True,
                msg="The exact target tool could not be prepared or verified.",
            )
