"""Report only the read-only placement privilege proof to the runner."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        phase = self._task.args.get("phase")
        try:
            if set(self._task.args) == {"phase"} and phase == "loaded":
                emit({"phase": phase}, acknowledge=True)
            elif set(self._task.args) == {"phase", "outcome", "uid"} and phase == "completed":
                uid, outcome = self._task.args["uid"], self._task.args["outcome"]
                if isinstance(uid, bool) or not isinstance(uid, int) or uid != 0 or outcome != "unchanged":
                    raise ValueError("privilege proof")
                emit({"phase": phase, "outcome": outcome, "evidence": {"uid": uid}})
            else:
                raise ValueError("phase")
        except (ValueError, OSError):
            result.update(failed=True, msg="The privilege proof could not report its result")
        return result
