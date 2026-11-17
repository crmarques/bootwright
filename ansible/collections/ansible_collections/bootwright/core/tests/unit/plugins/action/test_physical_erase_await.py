"""The erase wait polls the execution host once per round and reports progress the runner presents.

The poll itself is tested in test_physical_erase_controller.py. These cases
hold the wait: its clock and bounds, the running records it hands the runner,
and that a refusal or failure of the poll passes through unchanged.
"""

from __future__ import annotations

from types import SimpleNamespace

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.action import physical_erase_await as action

GiB = 1024**3
REQUEST = {"deadlineSeconds": 14400, "install": {"address": "192.0.2.10"},
           "targets": [{"deviceName": "/dev/sda", "method": "logicalOverwrite"}, {"deviceName": "/dev/sdb", "method": "metadataClear"}]}
RECEIPT = {"state": "complete"}


class Waiting:
    """A fake session stream and clock: each poll takes two seconds, each pause its seconds."""

    def __init__(self, answers):
        self.now, self.records, self.polls = 0.0, [], 0
        self.answers = iter(answers)

    def clock(self):
        return self.now

    def pause(self, seconds):
        self.now += seconds

    def poll(self):
        self.polls += 1
        self.now += 2
        answer = next(self.answers, None)
        return answer(self.now) if callable(answer) else answer or {"answer": "unanswered", "cause": "refused"}

    def channel(self, record, acknowledge=False):
        assert acknowledge is False
        self.records.append(record)

    def wait(self, request=None):
        return action.wait(self.poll, request or REQUEST, self.channel, clock=self.clock, pause=self.pause)

    def running(self, prefix=""):
        return [record for record in self.records if record["status"] == "running" and record["group"].startswith(prefix) and "detail" not in record]

    def notes(self):
        return [record for record in self.records if "detail" in record]


def partial(*counts):
    return {"answer": "partial", "progress": [{"completed": completed, "total": total} for completed, total in counts]}


def test_a_worker_that_never_answers_shows_its_silence_each_minute_and_refuses_after_the_startup_allowance():
    waiting = Waiting([])
    assert waiting.wait() == {"refused": "worker-unreachable", "detail": "refused:20min", "controller": ""}
    assert action.FIRST_ANSWER_SECONDS <= waiting.now < action.FIRST_ANSWER_SECONDS + 10
    shown = [(record["group"], record["detail"]) for record in waiting.notes()]
    assert shown == [(group, "unanswered:refused:%dmin" % minutes) for minutes in range(1, 20) for group in ("erase-1", "erase-2")]
    assert all(set(record) == {"phase", "group", "status", "detail"} and record["status"] == "running" for record in waiting.notes())
    assert "192.0.2.10" not in repr(waiting.records), "the adapter words the note; Go names the address"


def test_another_host_key_at_the_install_address_refuses_foreign_worker_at_once():
    answers = [partial((0, 2), (0, 2)), {"answer": "unanswered", "cause": "refused"}, {"answer": "unanswered", "cause": "host-key"}]
    waiting = Waiting(answers)
    assert waiting.wait() == {"refused": "foreign-worker", "detail": "host-key", "controller": ""}
    assert waiting.polls == 3 and waiting.now < 60, "the foreign host was waited on"
    assert Waiting([{"answer": "unanswered", "cause": "host-key"}]).wait()["refused"] == "foreign-worker"


def test_a_worker_that_answered_and_went_silent_refuses_after_ten_minutes():
    waiting = Waiting([partial((0, 16 * GiB), (0, 2 * GiB))] * 3)
    assert waiting.wait() == {"refused": "worker-unreachable", "detail": "refused:10min", "controller": ""}
    answered = 3 * (2 + action.ROUND_SECONDS) - action.ROUND_SECONDS
    assert answered + action.SILENCE_SECONDS <= waiting.now < answered + action.SILENCE_SECONDS + 10


def test_each_target_reports_its_bytes_when_they_move_at_most_once_a_minute_then_settles_with_the_receipt():
    rounds = [partial((completed * GiB, 16 * GiB), (0, 2 * GiB)) for completed in range(0, 15)]
    rounds += [partial((16 * GiB, 16 * GiB), (2 * GiB, 2 * GiB))] * 12
    rounds += [{"answer": "complete", "receipt": RECEIPT}]
    waiting = Waiting(rounds)
    assert waiting.wait() == {"receipt": RECEIPT}
    first = waiting.running("erase-1")
    assert first[0] == {"phase": "group", "group": "erase-1", "status": "running", "progress": {"completed": 0, "total": 16 * GiB}}
    assert [record["progress"]["completed"] for record in first] == [0, 9 * GiB, 16 * GiB]
    second = waiting.running("erase-2")
    assert [record["progress"] for record in second] == [{"completed": 0, "total": 2 * GiB}, {"completed": 2 * GiB, "total": 2 * GiB}]
    assert waiting.records[-2:] == [{"phase": "group", "group": "erase-1", "status": "ok"}, {"phase": "group", "group": "erase-2", "status": "ok"}]


def test_a_transient_silence_shows_once_and_the_next_answer_reports_at_once():
    answers = [partial((GiB, 16 * GiB), (0, 2 * GiB))] + [None] * 10 + [partial((GiB, 16 * GiB), (0, 2 * GiB)), {"answer": "complete", "receipt": RECEIPT}]
    waiting = Waiting(answers)
    assert waiting.wait() == {"receipt": RECEIPT}
    groups = [(record["group"], record.get("detail")) for record in waiting.records if record["status"] == "running"]
    assert groups == [("erase-1", None), ("erase-2", None), ("erase-1", "unanswered:refused:1min"), ("erase-2", "unanswered:refused:1min"),
                      ("erase-1", None), ("erase-2", None)]


def test_malformed_progress_is_ignored_and_never_refuses():
    malformed = [{"answer": "partial", "progress": value} for value in (
        None, "much", [], [{"completed": 1, "total": 2}], [{"completed": 3, "total": 2}, {"completed": 0, "total": 2}],
        [{"completed": -1, "total": 2}, {"completed": 0, "total": 2}], [{"completed": True, "total": 2}, {"completed": 0, "total": 2}],
        [{"completed": 1, "total": 2, "rate": 9}, "x"], [1, 2])]
    waiting = Waiting(malformed + [{"answer": "complete", "receipt": RECEIPT}])
    assert waiting.wait() == {"receipt": RECEIPT}
    assert waiting.running() == []


def test_a_refusal_or_failure_of_the_poll_passes_through_and_a_foreign_answer_fails():
    refusal = {"changed": True, "refused": "foreign-worker", "detail": "", "controller": ""}
    waiting = Waiting([partial((0, 2), (0, 2)), refusal])
    assert waiting.wait() == {"refused": "foreign-worker", "detail": "", "controller": ""}
    failed = Waiting([{"answer": "partial", "progress": None}, {"refused": "worker-failed"}]).wait()
    assert failed == {"refused": "worker-failed", "detail": "", "controller": ""}
    custody = {"failed": True, "msg": "physical erase custody is incomplete"}
    assert Waiting([custody]).wait() == {"failed": True, "msg": "physical erase custody is incomplete"}
    crash = {"failed": True, "msg": "MODULE FAILURE\nTraceback ... 192.0.2.10 token"}
    for answer in (crash, {"unreachable": True, "msg": "ssh: 192.0.2.10"}, {"answer": "maybe"}, ["not", "a", "result"]):
        assert Waiting([answer]).wait() == {"failed": True, "msg": action.UNPROVED}


def test_the_wait_ends_at_its_deadline_with_receipt_timeout():
    waiting = Waiting([lambda now: partial((int(now), 10**9), (0, 2))] * 10**5)
    assert waiting.wait(dict(REQUEST, deadlineSeconds=600)) == {"refused": "receipt-timeout", "detail": "", "controller": ""}
    assert waiting.now >= 600 + action.DEADLINE_MARGIN_SECONDS


def test_sixteen_targets_report_across_the_full_deadline_within_the_runners_bound():
    targets = [{"method": "logicalOverwrite"}] * 16
    moving = [lambda now: {"answer": "partial", "progress": [{"completed": int(now), "total": 10**9}] * 16}] * 10**6
    waiting = Waiting(moving)
    request = dict(REQUEST, targets=targets)
    assert waiting.wait(request) == {"refused": "receipt-timeout", "detail": "", "controller": ""}
    seconds = request["deadlineSeconds"] + action.DEADLINE_MARGIN_SECONDS
    every = action.interval(16, seconds, action.PRESENTATION_RECORDS - action.NOTE_RECORDS)
    assert every > action.PROGRESS_SECONDS, "the full deadline at sixteen targets must stretch the interval"
    assert len(waiting.running()) <= action.PRESENTATION_RECORDS - action.NOTE_RECORDS < 4096
    for index in range(16):
        reported = [record["progress"]["completed"] for record in waiting.running("erase-%d" % (index + 1))]
        assert reported[-1] >= seconds - every - 10, ("erase-%d stopped reporting" % (index + 1), reported[-1], seconds)
    assert action.interval(2, seconds, action.PRESENTATION_RECORDS - action.NOTE_RECORDS) == action.PROGRESS_SECONDS


def test_the_waiting_note_has_its_own_budget_and_a_channel_failure_never_fails_the_wait():
    waiting = Waiting([])
    reporter = action.Reporter(waiting.channel, 2, 3600, budget=10, notes=4)
    for minute in range(1, 5):
        reporter.waiting("timeout", minute * 60)
    for second in range(6):
        reporter.progress([{"completed": second, "total": 9}, {"completed": second, "total": 9}], second * 3600)
    reporter.settled()
    expected = [("erase-%d" % target, "unanswered:timeout:%dmin" % minute) for minute in (1, 2) for target in (1, 2)]
    assert [(record["group"], record.get("detail")) for record in waiting.notes()] == expected
    assert len(waiting.running()) == 6 and [record["status"] for record in waiting.records[-2:]] == ["ok", "ok"]

    def broken(record, acknowledge=False):
        raise BrokenPipeError("runner gone")

    assert action.wait(Waiting([partial((0, 2), (0, 2)), {"answer": "complete", "receipt": RECEIPT}]).poll, REQUEST, broken,
                       pause=lambda seconds: None) == {"receipt": RECEIPT}


def test_the_action_polls_the_module_with_the_task_arguments_and_hides_its_result(monkeypatch):
    calls, records = [], []
    answers = iter([partial((0, 2), (0, 2)), {"answer": "complete", "receipt": RECEIPT}])
    monkeypatch.setattr(ActionBase, "run", lambda self, tmp=None, task_vars=None: {})
    monkeypatch.setattr(action, "emit", lambda record, acknowledge=False: records.append(record))
    monkeypatch.setattr(action.time, "sleep", lambda seconds: None)
    plugin = action.ActionModule.__new__(action.ActionModule)
    arguments = {"request": REQUEST, "digest": "d" * 64, "material": {"fleetIdentity": "/run/bootwright/job/fleet-id"}}
    plugin._task = SimpleNamespace(args=arguments)

    def execute(module_name, module_args, task_vars):
        calls.append((module_name, module_args))
        return next(answers)

    plugin._execute_module = execute
    result = plugin.run(task_vars={})
    assert result == {"changed": True, "_ansible_no_log": True, "receipt": RECEIPT}
    assert calls == [("bootwright.core.physical_erase", dict(arguments, phase="poll"))] * 2
    assert [record["group"] for record in records] == ["erase-1", "erase-2", "erase-1", "erase-2"]
