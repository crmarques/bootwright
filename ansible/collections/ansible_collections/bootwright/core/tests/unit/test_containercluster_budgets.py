"""Every cluster budget bounds its phase in wall-clock time.

The media request freezes the build budget and the install request the boot,
bootstrap and install budgets (internal/containercluster/agentinstall,
requests.go), and each run's deadline is derived from them. A phase that only
stops starting new attempts once its budget is spent overruns it by a whole
installer timeout, and a read that never gives up holds the run until its
deadline, so these checks pin how each role spends what it froze: the image
build and every installer wait run under timeout with what their budget has
left, recomputed for each attempt, a stopped one is diagnosed as the budget
spent, the boot phase gives every node only what is left of one budget, and
every cluster read gives up on a request after a bounded time.

ansible-core templates a task's arguments once, before its first attempt, and
retries it with those arguments (ansible/executor/task_executor.py in
ansible-core 2.21: post_validate before the attempt loop), which is why the
waits are a looped include rather than an until loop. GNU timeout exits 124
once it stopped the command, and a kill after --kill-after also ends timeout's
own process group, which a shell reports as 137 and Python's subprocess, so the
command module, as -9 (the timeout invocation in the GNU coreutils manual,
https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html,
and subprocess.Popen.returncode in the Python documentation).

How a wait goes on from one attempt to the next is checked by running wait.yml
against a scripted installer and a simulated clock (Wait below), so the gate,
the pause, the remaining time and the record step are proved together rather
than one expression at a time.

Rendering the task files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import datetime
import pathlib
import time

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
INSTALL = ROLES / "containercluster_install_agent"
MEDIA = ROLES / "containercluster_media_agent"
LOADER = DataLoader()

# Distinct values, so each phase is proved to read its own budget.
INSTALL_BUDGETS = {"bootSeconds": 311, "bootstrapSeconds": 4703, "installSeconds": 5903}
MEDIA_BUDGETS = {"buildSeconds": 1709}
SPENT = (124, 137, -9)
CLIENT = "{{ containercluster_install_agent_client }}"
TIMED_OUT = "level=fatal msg=\"bootstrap process timed out\""
STALLED = "level=fatal msg=\"failed to progress after all hosts available\""
HOST_ERROR = "level=info msg=\"cluster has hosts in error\"\n" + TIMED_OUT


def trusted(path):
    return [task for task in LOADER.load_from_file(str(path), trusted_as_template=True) if isinstance(task, dict)]


def walk(tasks):
    for task in tasks:
        yield task
        for section in ("block", "rescue", "always"):
            yield from walk([child for child in task.get(section) or [] if isinstance(child, dict)])


def found(role, name, predicate):
    matches = [task for task in walk(trusted(role / "tasks" / name)) if predicate(task)]
    assert len(matches) == 1, "%s has %d tasks that match" % (name, len(matches))
    return matches[0]


def argv(task):
    """A command's arguments as loaded, so each templated one still renders."""
    command = task.get("ansible.builtin.command")
    return list((command or {}).get("argv") or []) if isinstance(command, dict) else []


def scope(role, **variables):
    """The role's defaults, a request freezing the test budgets, and variables."""
    values = dict(LOADER.load_from_file(str(role / "defaults" / "main.yml"), trusted_as_template=True))
    values["bootwright_cluster_install_request"] = {
        "budgets": INSTALL_BUDGETS, "identity": {"cluster": "sno"}, "workRoot": "/var/lib/bootwright-clusters/lab/sno"}
    values["bootwright_cluster_install_material"] = {"openshiftinstall": "/installer", "oc": "/oc"}
    values["bootwright_cluster_media_request"] = {
        "budgets": MEDIA_BUDGETS, "identity": {"cluster": "sno"}, "workRoot": "/var/lib/bootwright-clusters/lab/sno"}
    values["bootwright_cluster_media_material"] = {"installer": "/installer"}
    values.update(variables)
    return Templar(loader=LOADER, variables=values)


def now():
    return int(time.time())


def test_every_budget_a_request_freezes_is_read_by_its_role():
    for role, block in ((INSTALL, "install"), (MEDIA, "media")):
        specs = LOADER.load_from_file(str(role / "meta" / "argument_specs.yml"))["argument_specs"]
        budgets = {entry: set(spec["options"]["bootwright_cluster_%s_request" % block]["options"]["budgets"]["options"])
                   for entry, spec in specs.items()}
        assert set(budgets) == {"apply", "observe", "destroy"}
        expected = set(INSTALL_BUDGETS if block == "install" else MEDIA_BUDGETS)
        assert all(declared == expected for declared in budgets.values()), budgets
        text = "".join(path.read_text() for path in sorted((role / "tasks").glob("*.yml")))
        for name in expected:
            assert "bootwright_cluster_%s_request.budgets.%s" % (block, name) in text, name


def test_each_wait_reads_its_own_phases_budget():
    for milestone, budget in (("bootstrap-complete", "bootstrapSeconds"), ("install-complete", "installSeconds")):
        wait = found(INSTALL, "apply.yml", lambda task, milestone=milestone: (task.get("vars") or {}).get(
            "containercluster_install_agent_milestone") == milestone)
        assert wait["ansible.builtin.include_tasks"] == "wait.yml"
        assert int(scope(INSTALL).template(wait["vars"]["containercluster_install_agent_budget"])) == INSTALL_BUDGETS[budget]


def attempt():
    return found(INSTALL, "wait_attempt.yml", lambda task: "wait-for" in argv(task))


def test_each_installer_wait_runs_every_attempt_under_what_its_budget_has_left():
    wait = trusted(INSTALL / "tasks" / "wait.yml")
    # The deadline is set once, before the attempts, and the attempts are a
    # bounded loop of includes rather than retries of one templated task.
    assert "containercluster_install_agent_deadline" in wait[0]["ansible.builtin.set_fact"]
    looped = wait[1]
    assert looped["ansible.builtin.include_tasks"] == "wait_attempt.yml"
    assert scope(INSTALL).template(looped["loop"]) == list(range(41))
    for name in ("wait.yml", "wait_attempt.yml"):
        for task in walk(trusted(INSTALL / "tasks" / name)):
            assert not {"until", "retries"} & set(task), "%s: %s" % (name, task.get("name"))
    # What is left is taken in the attempt, from the deadline, and nowhere else.
    tasks = trusted(INSTALL / "tasks" / "wait_attempt.yml")[0]["block"]
    taking = [index for index, task in enumerate(tasks)
              if "containercluster_install_agent_remaining" in (task.get("ansible.builtin.set_fact") or {})]
    running = tasks.index(attempt())
    assert len(taking) == 1 and taking[0] < running
    # Every attempt that runs takes it afresh: a condition on it would leave a
    # later attempt the time an earlier one was given.
    assert "when" not in tasks[taking[0]]
    assert "containercluster_install_agent_remaining" not in (INSTALL / "tasks" / "wait.yml").read_text()
    # The deadline is the wait's, set once in wait.yml: an attempt that set it
    # again would hand each attempt a fresh budget.
    assert not [task for task in walk(trusted(INSTALL / "tasks" / "wait_attempt.yml"))
                if "containercluster_install_agent_deadline" in (task.get("ansible.builtin.set_fact") or {})]
    assert "containercluster_install_agent_deadline:" not in (INSTALL / "tasks" / "wait_attempt.yml").read_text()
    remaining = tasks[taking[0]]["ansible.builtin.set_fact"]["containercluster_install_agent_remaining"]
    for left in (4703, 17, -5):
        rendered = int(scope(INSTALL, containercluster_install_agent_deadline=now() + left).template(remaining))
        assert left - 2 <= rendered <= left
    command = argv(attempt())
    assert command[0] == "/usr/bin/timeout"
    assert command[4:7] == ["agent", "wait-for", "{{ containercluster_install_agent_milestone }}"]
    rendering = scope(INSTALL, containercluster_install_agent_remaining=17)
    grace = rendering.template(command[1])
    assert grace.startswith("--kill-after=") and grace.endswith("s")
    assert 0 < int(grace[len("--kill-after="):-1]) <= 60
    assert rendering.template(command[2]) == "17s"
    assert rendering.template(command[3]) == "/installer"
    assert not scope(INSTALL, containercluster_install_agent_remaining=0).evaluate_conditional(attempt()["when"])


def record(rc, stderr="", skipped=False):
    """What the attempt records for one result: the wait's result and whether another follows."""
    task = found(INSTALL, "wait_attempt.yml",
                 lambda task: "containercluster_install_agent_waiting" in (task.get("ansible.builtin.set_fact") or {}))
    result = {"skipped": True} if skipped else {"rc": rc, "stderr": stderr, "changed": rc == 0}
    rendering = scope(INSTALL, containercluster_install_agent_attempted=result,
                      containercluster_install_agent_wait={"rc": 1, "stderr": "earlier"})
    facts = task["ansible.builtin.set_fact"]
    return (rendering.template(facts["containercluster_install_agent_wait"]),
            rendering.template(facts["containercluster_install_agent_waiting"]))


def failure(wait, stalled=False):
    """The message the wait fails with for its last result, or None when it reached the milestone."""
    task = found(INSTALL, "wait.yml", lambda task: "ansible.builtin.fail" in task)
    rendering = scope(INSTALL, containercluster_install_agent_wait=wait, containercluster_install_agent_budget=4703,
                      containercluster_install_agent_milestone="bootstrap-complete",
                      containercluster_install_agent_stalled=stalled, **(task.get("vars") or {}))
    if not rendering.evaluate_conditional(task["when"]):
        return None
    return rendering.template(task["ansible.builtin.fail"]["msg"])


@pytest.mark.parametrize("rc", SPENT)
def test_an_attempt_the_budget_stopped_is_the_budget_spent_and_never_resumed(rc):
    # Its output carries a give-up the installer could be resumed after, so
    # only the exit status keeps the wait from going on.
    assert record(rc)[1] is False
    assert record(rc, TIMED_OUT)[1] is False
    message = failure({"rc": rc, "stderr": TIMED_OUT})
    assert message.startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")


def test_only_a_give_up_the_installer_can_resume_from_is_resumed():
    assert record(1, TIMED_OUT)[1] is True
    assert record(1, STALLED)[1] is True
    # Success ends the wait even when its output recorded an earlier give-up.
    assert record(0, TIMED_OUT)[1] is False
    # A host in error ends it although the same output carries a resumable give-up.
    assert record(1, HOST_ERROR)[1] is False
    assert record(1, "level=fatal msg=\"failed to fetch the cluster\"")[1] is False


def test_the_budget_spent_is_told_apart_from_the_installers_own_give_ups():
    timed_out = TIMED_OUT
    assert record(1, timed_out)[1] is True
    assert failure({"rc": 1, "stderr": timed_out}).startswith("the installer stopped watching on its own compiled deadline")
    # No attempt ran because nothing of the budget was left.
    assert failure({"rc": None, "stderr": ""}).startswith("this wait's budget of 4703 seconds was spent")
    # A host in error stopped the installation whatever stopped the wait.
    for rc in (1,) + SPENT:
        assert failure({"rc": rc, "stderr": "cluster has hosts in error"}).startswith(
            "the assisted service moved a declared host into error")
    assert failure({"rc": 0, "stderr": ""}) is None
    assert record(0) == ({"rc": 0, "stderr": "", "changed": True}, False)
    # An attempt skipped for want of time keeps the one before it as the result.
    assert record(None, skipped=True) == ({"rc": 1, "stderr": "earlier"}, False)


class Clock:
    """The now() the rendered templates read, so a simulated wait takes no time."""

    def __init__(self):
        self.seconds = 1_000_000

    def __call__(self, utc=False, fmt=None):
        moment = datetime.datetime.fromtimestamp(self.seconds, datetime.timezone.utc)
        return moment.strftime(fmt) if fmt else moment


class Failed(Exception):
    """A task failed the wait, with its message."""


def conditions(task):
    when = task.get("when")
    return [] if when is None else list(when) if isinstance(when, list) else [when]


class Wait:
    """Runs wait.yml as ansible-core would, against a scripted installer and a simulated clock.

    A block's condition is evaluated again for every task in it, a looped
    include expands every iteration before the first runs, a skipped task
    still registers, and every value of one set_fact is templated before any
    of them is set. Each scripted installer run is (rc, stderr, seconds); one
    that would run past the time it was given is stopped there, exiting with
    `stopped`, 124 at the deadline or, for a kill, one grace later.
    """

    def __init__(self, script, budget=4703, stopped=124):
        self.clock = Clock()
        self.script = list(script)
        self.budget = budget
        self.stopped = stopped
        self.facts = {}
        self.runs = []
        self.pauses = []
        self.started = None

    def run(self, milestone="bootstrap-complete"):
        """Wait for one milestone; the message it failed with, or None when it was reached."""
        self.started = self.clock.seconds
        local = {"containercluster_install_agent_milestone": milestone, "containercluster_install_agent_budget": self.budget}
        try:
            for task in trusted(INSTALL / "tasks" / "wait.yml"):
                self.task(task, [], local)
        except Failed as failed:
            return str(failed)
        return None

    def rendering(self, local):
        return scope(INSTALL, now=self.clock, **dict(self.facts, **local))

    def task(self, task, inherited, local):
        local = dict(local, **(task.get("vars") or {}))
        when = inherited + conditions(task)
        if "block" in task:
            assert not task.get("rescue") and not task.get("always"), task["name"]
            for child in task["block"]:
                self.task(child, when, local)
            return
        if "ansible.builtin.include_tasks" in task:
            self.include(task, when, local)
            return
        rendering = self.rendering(local)
        if not all(rendering.evaluate_conditional(condition) for condition in when):
            if "register" in task:
                self.facts[task["register"]] = {"changed": False, "skipped": True}
            return
        if "ansible.builtin.set_fact" in task:
            self.facts.update({name: rendering.template(value) for name, value in task["ansible.builtin.set_fact"].items()})
        elif "ansible.builtin.wait_for" in task:
            seconds = int(rendering.template(task["ansible.builtin.wait_for"]["timeout"]))
            self.pauses.append(seconds)
            self.clock.seconds += seconds
        elif "ansible.builtin.command" in task:
            self.facts[task["register"]] = self.installer(task, rendering)
        elif "ansible.builtin.fail" in task:
            raise Failed(rendering.template(task["ansible.builtin.fail"]["msg"]))
        else:
            raise AssertionError("the simulation has no model of task %r" % task.get("name"))

    def include(self, task, when, local):
        loop_var = task["loop_control"]["loop_var"]
        rendering = self.rendering(local)
        items = [item for item in rendering.template(task["loop"])
                 if all(self.rendering(dict(local, **{loop_var: item})).evaluate_conditional(condition)
                        for condition in when)]
        included = trusted(INSTALL / "tasks" / task["ansible.builtin.include_tasks"])
        for item in items:
            for child in included:
                self.task(child, [], dict(local, **{loop_var: item}))

    def installer(self, task, rendering):
        command = [rendering.template(value) for value in argv(task)]
        assert command[0] == "/usr/bin/timeout" and command[4:6] == ["agent", "wait-for"], command
        assert command[1].startswith("--kill-after=") and command[1].endswith("s") and command[2].endswith("s"), command
        grace, given = int(command[1][len("--kill-after="):-1]), int(command[2][:-1])
        assert self.script, "the installer was run more often than the %d times scripted" % len(self.runs)
        rc, stderr, seconds = self.script.pop(0)
        self.runs.append((self.clock.seconds - self.started, given))
        if seconds >= given:
            rc = self.stopped
            seconds = given if rc == 124 else given + grace
        self.clock.seconds += seconds
        result = {"rc": rc, "stderr": stderr, "changed": rc == 0}
        failed_when = task.get("failed_when", "rc != 0")
        checks = conditions({"when": failed_when})
        failing = scope(INSTALL, **{task["register"]: result})
        if failed_when is not False and all(failing.evaluate_conditional(check) for check in checks):
            raise Failed("the installer's own failure failed the task")
        return result


def test_a_wait_that_reaches_its_milestone_runs_the_installer_once():
    wait = Wait([(0, "", 600)])
    assert wait.run() is None
    assert wait.runs == [(0, 4703)] and wait.pauses == []


@pytest.mark.parametrize("stderr, hint", [
    (HOST_ERROR, "the assisted service moved a declared host into error"),
    ("level=fatal msg=\"failed to fetch the cluster\"", "this give-up is not one the installer can be resumed after"),
], ids=["host-error", "not-resumable"])
def test_a_give_up_the_installer_cannot_resume_from_runs_it_once(stderr, hint):
    wait = Wait([(1, stderr, 600)])
    assert wait.run().startswith(hint)
    assert len(wait.runs) == 1 and wait.pauses == []


def test_each_attempt_is_given_what_the_budget_has_left_when_it_starts():
    wait = Wait([(1, TIMED_OUT, 600), (1, STALLED, 1200), (0, "", 100)])
    assert wait.run() is None
    # 30 seconds apart, and each given the budget less what the attempts and
    # pauses before it took.
    assert wait.pauses == [30, 30]
    assert wait.runs == [(0, 4703), (630, 4073), (1860, 2843)]


def test_a_give_up_near_the_deadline_pauses_only_for_what_is_left_and_runs_no_more():
    wait = Wait([(1, TIMED_OUT, 4690)])
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert wait.runs == [(0, 4703)] and wait.pauses == [13]
    assert wait.clock.seconds - wait.started == 4703


def test_repeated_give_ups_end_with_the_budget():
    wait = Wait([(1, TIMED_OUT, 20)] * 41, budget=100)
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert wait.runs == [(0, 100), (50, 50)] and wait.pauses == [30, 30]


def test_quick_give_ups_end_with_the_last_attempt():
    wait = Wait([(1, TIMED_OUT, 1)] * 41)
    assert wait.run().startswith("the installer stopped watching on its own compiled deadline")
    assert len(wait.runs) == 41 and wait.pauses == [30] * 40


@pytest.mark.parametrize("stopped", SPENT)
def test_a_wait_the_budget_stops_ends_within_it_and_one_grace(stopped):
    wait = Wait([(0, "", 10**6)], stopped=stopped)
    assert wait.run().startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")
    assert wait.runs == [(0, 4703)]
    assert wait.clock.seconds - wait.started <= 4703 + 30


@pytest.mark.parametrize("stopped", SPENT)
def test_a_stall_stays_the_diagnosis_when_the_budget_stops_the_attempt_after_it(stopped):
    wait = Wait([(1, STALLED, 60), (0, "", 10**6)], stopped=stopped)
    message = wait.run()
    assert message.startswith("the cluster reached ready but the rendezvous node never started installing")
    assert "this wait's budget of 4703 seconds stopped that attempt" in message
    assert wait.runs == [(0, 4703), (90, 4613)]


def test_a_later_give_up_of_another_kind_replaces_the_stall():
    wait = Wait([(1, STALLED, 60), (1, TIMED_OUT, 60), (0, "", 10**6)])
    assert wait.run().startswith("this wait's budget of 4703 seconds was spent while the installer was still watching")


def test_a_host_in_error_is_the_diagnosis_even_after_a_stall():
    wait = Wait([(1, STALLED, 60), (1, HOST_ERROR, 60)])
    assert wait.run().startswith("the assisted service moved a declared host into error")
    assert len(wait.runs) == 2


def test_the_next_wait_starts_afresh():
    wait = Wait([(1, STALLED, 60), (0, "", 600), (1, TIMED_OUT, 100), (0, "", 100)])
    assert wait.run("bootstrap-complete") is None
    assert wait.run("install-complete") is None
    assert len(wait.runs) == 4 and wait.runs[2:] == [(0, 4703), (130, 4573)]
    # A stall the first wait recovered from is not the second one's diagnosis.
    wait = Wait([(1, STALLED, 60), (0, "", 600), (0, "", 10**6)])
    assert wait.run("bootstrap-complete") is None
    assert wait.run("install-complete").startswith("this wait's budget of 4703 seconds was spent")


def build():
    return found(MEDIA, "build.yml", lambda task: argv(task)[4:7] == ["agent", "create", "image"])


def test_the_image_is_built_under_timeout_with_the_budget_its_request_froze():
    builds = [task for path in sorted((MEDIA / "tasks").glob("*.yml")) for task in walk(trusted(path))
              if "create" in argv(task) and "image" in argv(task)]
    assert builds == [build()]
    command = argv(build())
    block = found(MEDIA, "build.yml", lambda task: build() in (task.get("block") or []))
    rendering = scope(MEDIA, **block["vars"])
    assert command[0] == "/usr/bin/timeout"
    grace = rendering.template(command[1])
    assert grace.startswith("--kill-after=") and 0 < int(grace[len("--kill-after="):-1]) <= 60
    assert rendering.template(command[2]) == "%ds" % MEDIA_BUDGETS["buildSeconds"]
    assert command[3] == "{{ containercluster_media_agent_installer }}"


@pytest.mark.parametrize("rc, stopped", [(0, False), (1, False), (2, False)] + [(rc, True) for rc in SPENT])
def test_a_build_the_budget_stopped_fails_as_the_budget_spent_and_any_other_as_the_installers(rc, stopped):
    block = found(MEDIA, "build.yml", lambda task: build() in (task.get("block") or []))
    tasks = block["block"]
    refusal = tasks[tasks.index(build()) + 1]
    rendering = scope(MEDIA, containercluster_media_agent_built={"rc": rc}, **block["vars"])
    failed = all(rendering.evaluate_conditional(condition) for condition in build()["failed_when"])
    assert failed is (rc != 0 and not stopped)
    assert rendering.evaluate_conditional(refusal["when"]) is stopped
    assert rendering.template(refusal["ansible.builtin.fail"]["msg"]).startswith(
        "the installer was still building the boot image of cluster sno when its build budget of 1709 seconds was spent")


def test_every_cluster_read_gives_up_on_a_request_after_a_bounded_time():
    reads = [task for task in walk(trusted(INSTALL / "tasks" / "state.yml")) if argv(task)[:1] == [CLIENT]]
    assert len(reads) >= 4, "the walk stopped seeing the cluster reads"
    for task in reads:
        bounds = [value for value in argv(task) if value.startswith("--request-timeout")]
        assert len(bounds) == 1, task["name"]
        rendered = scope(INSTALL).template(bounds[0])
        assert rendered.startswith("--request-timeout=") and rendered.endswith("s"), rendered
        assert 0 < int(rendered[len("--request-timeout="):-1]) <= 60, rendered
    for path in sorted((INSTALL / "tasks").glob("*.yml")):
        if path.name != "state.yml":
            assert not [task for task in walk(trusted(path)) if argv(task)[:1] == [CLIENT]], path.name


def skip():
    return found(INSTALL, "boot.yml", lambda task: "containercluster_install_agent_state.powered" in str(
        task.get("when", "")))


def bounded():
    return found(INSTALL, "boot.yml", lambda task: "timeout" in task)


def test_the_boot_phase_deadline_is_set_once_before_the_first_node():
    apply = trusted(INSTALL / "tasks" / "apply.yml")
    setting = [index for index, task in enumerate(apply)
               if "containercluster_install_agent_boot_deadline" in (task.get("ansible.builtin.set_fact") or {})]
    booting = [index for index, task in enumerate(apply) if task.get("ansible.builtin.include_tasks") == "boot.yml"]
    marking = [index for index, task in enumerate(apply)
               if str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith("/.bootwright-booted")]
    assert len(setting) == len(booting) == len(marking) == 1
    assert marking[0] < setting[0] < booting[0]
    assert "loop" in apply[booting[0]] and "loop" not in apply[setting[0]]
    deadline = apply[setting[0]]["ansible.builtin.set_fact"]["containercluster_install_agent_boot_deadline"]
    rendered = int(scope(INSTALL).template(deadline))
    assert now() + INSTALL_BUDGETS["bootSeconds"] - 2 <= rendered <= now() + INSTALL_BUDGETS["bootSeconds"]
    assert "containercluster_install_agent_boot_deadline:" not in (INSTALL / "tasks" / "boot.yml").read_text()


def test_every_boot_step_runs_under_what_the_boot_phase_has_left():
    steps = [task for task in walk(bounded()["block"])
             if "bootwright.core.redfish_boot" in task or "ansible.builtin.include_role" in task]
    effects = [task for task in walk(skip()["block"])
               if "bootwright.core.redfish_boot" in task or "ansible.builtin.include_role" in task]
    assert steps == effects and len(steps) == 3
    assert bounded() in skip()["block"]
    # ansible-core reads a timeout of 0 as none at all (_alarm_timeout.py:
    # `if not timeout: return`), so a step reached at or past the deadline
    # still gets one second rather than no bound.
    for left, want in ((297, 297), (1, 1), (0, 1), (-40, 1)):
        rendering = scope(INSTALL, containercluster_install_agent_boot_deadline=now() + left)
        assert max(1, want - 2) <= int(rendering.template(bounded()["timeout"])) <= want
    stopped, other = bounded()["rescue"]
    rendering = scope(INSTALL, containercluster_install_agent_node={"machine": "sno-01"},
                      ansible_failed_result={"failed": True, "timedout": {"period": 12}})
    assert rendering.evaluate_conditional(stopped["when"])
    assert rendering.template(stopped["ansible.builtin.fail"]["msg"]).startswith(
        "the boot phase's budget of 311 seconds was spent before every node was booted")
    assert not scope(INSTALL, ansible_failed_result={"failed": True, "msg": "refused"}).evaluate_conditional(
        stopped["when"])
    assert "when" not in other and "ansible.builtin.fail" in other


@pytest.mark.parametrize("left", [120, -30])
def test_a_node_the_skip_leaves_alone_takes_none_of_the_boot_budget(left):
    guard = found(INSTALL, "boot.yml", lambda task: "ansible.builtin.fail" in task
                  and "containercluster_install_agent_boot_deadline" in str(task.get("when", "")))
    assert guard in skip()["block"] and skip()["block"].index(guard) < skip()["block"].index(bounded())
    for powered, boots in ((["sno-01"], False), ([], True)):
        rendering = scope(INSTALL, containercluster_install_agent_boot_deadline=now() + left,
                          containercluster_install_agent_node={"machine": "sno-01"},
                          containercluster_install_agent_state={"powered": powered, "ownMedia": ["sno-01"]})
        # A node already running from this image runs nothing, so a spent
        # budget neither stops it nor is spent on it.
        assert rendering.evaluate_conditional(skip()["when"]) is boots
        assert (boots and rendering.evaluate_conditional(guard["when"])) is (boots and left < 0)
