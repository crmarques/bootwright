from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible.utils.display import Display
from ansible_collections.bootwright.core.plugins.module_utils.controller_batch import (
    ToolRefused,
    prepare_tools,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    MAX_DEADLINE,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_refusal import (
    name,
)


def tool_arguments(args):
    """The fixed argument set, with each tool's acquisition deadline in whole
    seconds as its request froze it, in the tools' order, and the trust anchor
    the stage lent."""
    if set(args) != {"bundle", "tools", "egress", "inspect_only", "acquisition", "trust"}:
        raise ValueError("request")
    if not isinstance(args["inspect_only"], bool):
        raise ValueError("request")
    tools, acquisition = args["tools"], args["acquisition"]
    if not isinstance(tools, list) or not isinstance(acquisition, list) or len(tools) != len(acquisition):
        raise ValueError("request")
    deadlines = []
    for tool, entry in zip(tools, acquisition):
        if not isinstance(entry, dict) or set(entry) != {"source", "seconds"}:
            raise ValueError("request")
        source = tool.get("source") if isinstance(tool, dict) else None
        if not isinstance(source, dict) or entry["source"] != source.get("id"):
            raise ValueError("request")
        deadline = entry["seconds"]
        if (
            not isinstance(deadline, int)
            or isinstance(deadline, bool)
            or not 1 <= deadline <= MAX_DEADLINE
        ):
            raise ValueError("deadline")
        deadlines.append(deadline)
    return (
        args["bundle"],
        tools,
        args["egress"],
        deadlines,
        args["inspect_only"],
        args["trust"],
    )


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            bundle, tools, egress, deadlines, inspect_only, trust = tool_arguments(
                self._task.args
            )
            result.update(
                prepare_tools(bundle, tools, egress, deadlines, inspect_only, trust)
            )
            return result
        except Exception as error:
            # Exceptions can include source/proxy paths. Evidence and diagnostics
            # cross the public boundary only through the enclosing fixed role;
            # the record carries a class and the source, never their text.
            source = None
            if isinstance(error, ToolRefused):
                error, source = error.error, error.source
            name(error, source, Display().warning)
            return dict(
                result,
                failed=True,
                msg="The exact target tool could not be prepared or verified.",
            )
