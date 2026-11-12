"""Publish bounded discovery evidence through the shared adapter channel."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    evidence = arguments.get("evidence")
    if not isinstance(evidence, dict) or evidence.get("request") != adapter_protocol.request_digest(arguments.get("digest")):
        raise ValueError("disk discovery evidence")
    if arguments.get("observed") or evidence.get("cleaned") is True:
        return evidence, None
    return evidence, "disk discovery has not proved its inventory and cleanup"


CAPABILITY = adapter_protocol.Capability("disk discovery", "disk discovery evidence could not be published", completion)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
