"""Publish a provided operating system update's bounded evidence."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit
from ansible_collections.bootwright.core.plugins.module_utils.managedos_provided_update import REFUSALS, STATES
from ansible_collections.bootwright.core.plugins.module_utils.managedos_update_worker import RELEASE

VERSION = "provided-os-update-evidence-v1"


def completion(arguments):
    release = arguments.get("release")
    answer = arguments.get("answer")
    if not isinstance(release, str) or len(release) > 64 or not RELEASE.fullmatch(release):
        raise ValueError("provided update release")
    if not isinstance(answer, dict) or answer.get("ok") is not True or answer.get("state") not in STATES:
        raise ValueError("provided update answer")
    state, packages, rebooted = answer["state"], answer.get("packages"), answer.get("rebooted")
    if type(packages) is not int or not 0 <= packages <= 2147483647 or type(rebooted) is not bool:  # pylint: disable=unidiomatic-typecheck
        raise ValueError("provided update evidence")
    if (rebooted != (state == "complete" and packages > 0)
            or (state in ("absent", "pending") and packages != 0) or (state == "upgraded" and packages == 0)):
        raise ValueError("provided update postcondition")
    if arguments.get("observed") is not True and state != "complete":
        return None, "the provided host update did not prove completion"
    return {"version": VERSION, "request": adapter_protocol.request_digest(arguments.get("digest")),
            "state": state, "release": release, "packages": packages, "rebooted": rebooted,
            "postcondition": state == "complete"}, None


CAPABILITY = adapter_protocol.Capability(
    "provided-update",
    "the provided host update result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
    detail=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
