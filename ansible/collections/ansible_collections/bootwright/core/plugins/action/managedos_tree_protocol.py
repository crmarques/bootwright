"""Publish the shared package tree's bounded lifecycle evidence."""
from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit


def completion(arguments):
    observation = arguments.get("observation") or {}
    source = observation.get("identity", "")
    if source and (not isinstance(source, str) or len(source) != 64 or set(source) - adapter_protocol.HEX):
        raise ValueError("tree source identity")
    removed = bool(arguments.get("removed"))
    evidence = {"absent": removed, "content": bool(observation.get("content")), "identity": source,
                "postcondition": False, "request": adapter_protocol.request_digest(arguments.get("digest")),
                "staging": bool(observation.get("staging")), "tree": bool(observation.get("tree")),
                "version": "os-package-tree-evidence-v1"}
    left = evidence["content"] or evidence["staging"] or evidence["tree"] or source
    evidence["postcondition"] = not left if removed else bool(evidence["tree"] and not evidence["staging"])
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the package tree", "not proved", ["publication"])


CAPABILITY = adapter_protocol.Capability("package-tree", "the package tree result could not be published", completion)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
