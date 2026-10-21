"""Publish the shared cluster object handoff and bounded evidence."""
from __future__ import annotations

import time
from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    state = arguments["state"]
    version = arguments["version"]
    if not isinstance(version, str) or len(version) > 96 or any(c not in adapter_protocol.DETAIL for c in version):
        raise ValueError("evidence version")
    found = dict(state, request=adapter_protocol.request_digest(arguments["digest"]), version=version)
    return found, None


CAPABILITY = adapter_protocol.Capability("cluster objects", "cluster object result could not be published", completion)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        arguments = self._task.args
        if arguments.get("phase") == "loaded":
            budget = arguments.get("budget", 0)
            if not isinstance(budget, int) or isinstance(budget, bool) or not 1 <= budget <= 21000:
                return {"failed": True, "msg": "cluster object budget is invalid"}
            deadline = time.time() + budget
        result = adapter_protocol.publish(arguments, CAPABILITY, emit)
        if (arguments.get("phase") == "completed" and not result.get("failed")
                and not adapter_protocol.publishes(arguments["state"], arguments.get("observed"))):
            result.update(failed=True, msg="cluster objects did not reach their postcondition")
        if arguments.get("phase") == "loaded" and not result.get("failed"):
            result["deadline"] = deadline
        return result
