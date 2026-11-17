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

import math
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
# A run hands at most this many running records, within the runner's bound
# of 4096 (internal/adapterprotocol), and keeps NOTE_RECORDS of them for the
# waiting note. A target's count is reported when it moved, at most once per
# interval: a minute, or longer when that many targets over the whole wait
# would outrun the rest of the budget.
PROGRESS_SECONDS = 60
PRESENTATION_RECORDS = 4000
NOTE_RECORDS = 800
# An unanswered session's closed failure classes. A host key other than the
# pinned erase-only key means another host answered at the install address,
# such as the installed system after a boot that skipped the erase media.
CAUSES = ("timeout", "refused", "no-route", "host-key", "auth", "exit")
FOREIGN_CAUSES = ("host-key",)


def group(index):
    """The group the plan declares for the frozen request's target at index."""
    return "erase-%d" % (index + 1)


def interval(targets, seconds, budget):
    """The seconds between two reports of one target, so every target reports across the whole wait within budget."""
    return max(PROGRESS_SECONDS, math.ceil(seconds * max(targets, 1) / max(budget, 1)))


class Reporter:
    """The running records of one wait, within its budgets; a refused record is dropped.

    Every record names a group the plan declares, one per target: progress
    and the waiting note alike, the note as the closed detail
    unanswered:<class>:<minutes>min that Go words for the operator.
    """

    def __init__(self, channel, targets, seconds, budget=PRESENTATION_RECORDS, notes=NOTE_RECORDS):
        self.channel, self.targets = channel, targets
        self.budget, self.notes = budget - notes, notes
        self.every = interval(targets, seconds, self.budget)
        self.last = {}
        self.waited = None

    def record(self, name, status, progress=None, detail=None):
        arguments = {"phase": "group", "group": name, "status": status}
        if progress is not None:
            arguments["progress"] = progress
        if detail is not None:
            arguments["detail"] = detail
        adapter_protocol.publish(arguments, protocol.CAPABILITY, self.channel)

    def running(self, name, progress):
        if self.budget <= 0:
            return
        self.budget -= 1
        self.record(name, "running", progress)

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
            if not resumed and seen is not None and (seen[1] == value or now - seen[0] < self.every):
                continue
            self.last[index] = (now, value)
            self.running(group(index), value)

    def waiting(self, cause, silent):
        """The visible note on every target of a worker no session reaches, once per whole minute of silence."""
        minutes = int(silent // 60)
        if minutes < 1 or minutes == self.waited or self.notes < self.targets:
            return
        self.waited = minutes
        self.notes -= self.targets
        for index in range(self.targets):
            self.record(group(index), "running", detail="unanswered:%s:%dmin" % (cause, minutes))

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
    seconds = request["deadlineSeconds"] + DEADLINE_MARGIN_SECONDS
    reporter = Reporter(channel, len(request["targets"]), seconds)
    began = clock()
    deadline = began + seconds
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
            if answer.get("cause") in FOREIGN_CAUSES:
                return refused("foreign-worker", answer["cause"])
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
