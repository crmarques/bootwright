"""Wait for the offline erase receipt and report each target's progress to the runner.

Each round runs the physical_erase module's one-shot poll on the execution
host, which holds custody and the pinned session to the RAM worker. This
plugin owns the wait: its clock, the worker-unreachable and receipt-timeout
bounds, and the running group records the runner presents. A refusal the poll
returns is handed back unchanged for the role to name. Progress is
presentation only: a malformed count is left out, a record the channel does
not take is dropped, and neither ever refuses or proves anything.
"""

from __future__ import annotations

import re
import time

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.action import physical_erase_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

UNPROVED = "physical erase did not prove its bounded custody or receipt contract"
# The fixed failures the module itself words; any other failure, such as an
# interpreter crash, is reported as the generic one.
FAILURES = (UNPROVED, "physical erase custody is incomplete")
ARGUMENTS = {"request", "digest", "material"}
# A worker that never answers is unreachable once the RAM startup allowance
# and a margin have passed since this wait began, and one that answered is
# once it then stays silent this long. Both are timed within this one wait,
# which follows start: nothing is persisted, so a repeated wait opens a fresh
# bounded window and never inherits a stale clock.
FIRST_ANSWER_SECONDS = 900 + 300
SILENCE_SECONDS = 600
DEADLINE_MARGIN_SECONDS = 900
ROUND_SECONDS = 5
# A target's count is reported when it moved, at most once a minute, and a
# run hands at most this many running records, within the runner's bound of
# 4096 (internal/adapterprotocol).
PROGRESS_SECONDS = 60
PRESENTATION_RECORDS = 4000
CAUSES = ("timeout", "refused", "no-route", "host-key", "auth", "exit")
ADDRESS = re.compile(r"[A-Za-z0-9.:-]{1,253}")


def group(index):
    """The group the plan declares for the frozen request's target at index."""
    return "erase-%d" % (index + 1)


class Reporter:
    """The running records of one wait, within its budget; a refused record is dropped."""

    def __init__(self, channel, address, targets, budget=PRESENTATION_RECORDS):
        self.channel, self.address, self.targets, self.budget = channel, address, targets, budget
        self.last = {}
        self.waited = None

    def record(self, name, status, progress=None):
        if status == "running":
            if self.budget <= 0:
                return
            self.budget -= 1
        arguments = {"phase": "group", "group": name, "status": status}
        if progress is not None:
            arguments["progress"] = progress
        adapter_protocol.publish(arguments, protocol.CAPABILITY, self.channel)

    def progress(self, measured, now):
        """Each target's count that moved, at most once a minute, or at once after a silence was shown."""
        resumed, self.waited = self.waited is not None, None
        if not isinstance(measured, list) or len(measured) != self.targets:
            return
        measured = [adapter_protocol.progress_of(value) for value in measured]
        if None in measured:
            return
        for index, value in enumerate(measured):
            seen = self.last.get(index)
            if not resumed and seen is not None and (seen[1] == value or now - seen[0] < PROGRESS_SECONDS):
                continue
            self.last[index] = (now, value)
            self.record(group(index), "running", value)

    def waiting(self, cause, silent):
        """The visible status of a worker no session reaches, once per whole minute of silence."""
        minutes = int(silent // 60)
        if minutes < 1 or minutes == self.waited:
            return
        self.waited = minutes
        self.record("waiting for the erase worker at %s, no answer for %d min (%s)" % (self.address, minutes, cause), "running")

    def settled(self):
        for index in range(self.targets):
            self.record(group(index), "ok")


def refused(code, detail=""):
    return {"refused": code, "detail": detail, "controller": ""}


def wait(poll, request, channel, clock=time.monotonic, pause=time.sleep):
    """Poll until a complete receipt, a refusal or a bound, reporting progress on the way.

    The result is the module's own shape: the receipt, or a named refusal
    with its detail and controller line, or the module's generic failure.
    """
    address = request["install"]["address"]
    if not isinstance(address, str) or not ADDRESS.fullmatch(address):
        address = "its install address"
    reporter = Reporter(channel, address, len(request["targets"]))
    began = clock()
    deadline = began + request["deadlineSeconds"] + DEADLINE_MARGIN_SECONDS
    answered, cause = None, "exit"
    while clock() < deadline:
        answer = poll()
        if not isinstance(answer, dict) or answer.get("failed") or answer.get("unreachable"):
            message = answer.get("msg") if isinstance(answer, dict) else None
            return {"failed": True, "msg": message if message in FAILURES else UNPROVED}
        if "refused" in answer:
            return {key: answer.get(key) or "" for key in ("refused", "detail", "controller")}
        kind, now = answer.get("answer"), clock()
        if kind == "complete" and isinstance(answer.get("receipt"), dict):
            reporter.settled()
            return {"receipt": answer["receipt"]}
        if kind in ("partial", "answered"):
            answered = now
            if kind == "partial":
                reporter.progress(answer.get("progress"), now)
        elif kind == "unanswered":
            cause = answer.get("cause") if answer.get("cause") in CAUSES else cause
            silent = now - (began if answered is None else answered)
            if silent >= (FIRST_ANSWER_SECONDS if answered is None else SILENCE_SECONDS):
                return refused("worker-unreachable", "%s:%dmin" % (cause, silent // 60))
            reporter.waiting(cause, silent)
        else:
            return {"failed": True, "msg": UNPROVED}
        pause(ROUND_SECONDS)
    return refused("receipt-timeout")


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=True, _ansible_no_log=True)
        try:
            arguments = dict(self._task.args)
            if set(arguments) != ARGUMENTS:
                raise ValueError("arguments")

            def poll():
                return self._execute_module(module_name="bootwright.core.physical_erase", module_args=dict(arguments, phase="poll"),
                                            task_vars=task_vars)

            result.update(wait(poll, arguments["request"], emit))
        except (ValueError, TypeError, KeyError, AttributeError):
            result.update(failed=True, msg=UNPROVED)
        return result
