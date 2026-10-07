"""A lifecycle supervisor dies with its invocation and takes its whole tree.

Either supervisor ends its whole tree when it is terminated. The seams stand in
for prctl, /proc and signal delivery, so the decisions hold without privilege;
real runs prove main() arms the parent-death signal in lifecycle mode only.
"""

from __future__ import annotations

import os
import signal
import subprocess
import sys
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    controller_supervisor as supervisor,
)


class Prctl:
    """Records each prctl call, also in a shared log when given one, and
    answers with a fixed result."""

    def __init__(self, result=0, log=None):
        self.result = result
        self.calls = []
        self.log = log

    def __call__(self, *arguments):
        self.calls.append(arguments)
        if self.log is not None:
            self.log.append(("prctl",) + arguments)
        return self.result


def parent(pid, log):
    """A getppid answering pid that records each call in a shared log."""

    def getppid():
        log.append("getppid")
        return pid

    return getppid


class Installer:
    """Records the handler installed for each signal."""

    def __init__(self):
        self.handlers = {}

    def __call__(self, signum, handler):
        self.handlers[signum] = handler


class Tree:
    """A process table under a child subreaper: a killed process's children
    are reparented to the supervisor, as the kernel does."""

    def __init__(self, parents, ancestry=(42,), spawn=None, unkillable=(), log=None):
        self.table = dict(parents)
        self.ancestry = list(ancestry)
        self.spawn = dict(spawn or {})
        self.unkillable = set(unkillable)
        self.killed, self.groups, self.ended = [], [], None
        self.now = 0.0
        self.log = [] if log is None else log

    def getpid(self):
        return 100

    def getppid(self):
        self.log.append("getppid")
        # Each call reads the next recorded parent; the last one persists.
        return self.ancestry.pop(0) if len(self.ancestry) > 1 else self.ancestry[0]

    def getpgrp(self):
        return 100

    def parents(self):
        return dict(self.table)

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds

    def reap(self):
        pass

    def kill(self, pid, signum):
        assert signum == signal.SIGKILL
        self.killed.append(pid)
        if pid in self.spawn:
            # A fork that completed just before the kill landed.
            self.table[self.spawn.pop(pid)] = pid
        if pid in self.unkillable or pid not in self.table:
            return
        del self.table[pid]
        for child, parent in list(self.table.items()):
            if parent == pid:
                self.table[child] = 100

    def killpg(self, group, signum):
        self.groups.append((group, signum, sorted(self.killed)))

    def end(self, status):
        self.ended = status


# The supervisor 100 runs ansible-playbook 101, whose worker 102 moved into a
# session of its own and runs module 103. Process 200 belongs to someone else.
TREE = {101: 100, 102: 101, 103: 102, 200: 1}


def test_only_the_lifecycle_runner_ties_the_supervisor_to_its_invocation():
    argv = ["-c", "--lifecycle", "-i", "inventory.json", "apply.yml"]
    assert supervisor.lifecycle_mode(argv)
    assert argv == ["-c", "-i", "inventory.json", "apply.yml"]
    for controller in (
        ["-c", "-i", "inventory.json", "setup.yml"],
        ["-c", "-i", "--lifecycle", "setup.yml"],
    ):
        unchanged = list(controller)
        assert not supervisor.lifecycle_mode(controller)
        assert controller == unchanged


def test_the_parent_death_signal_is_armed_before_the_parent_is_rechecked():
    # A check before the arming misses a parent that dies between the two,
    # and that parent sends no signal either.
    log = []
    assert supervisor.parent_guarded(
        Prctl(log=log), parent(42, log), 42, signal.SIGTERM
    )
    assert log == [("prctl", 1, int(signal.SIGTERM), 0, 0, 0), "getppid"]
    log = []
    assert supervisor.bind_child(Prctl(log=log), parent(100, log), 100, Installer())
    assert log == [("prctl", 1, int(signal.SIGKILL), 0, 0, 0), "getppid"]
    # The supervisor records its parent before arming, then re-checks it.
    log = []
    supervisor.guard_parent(Prctl(log=log), Tree(TREE, log=log), Installer())
    assert log == ["getppid", ("prctl", 1, int(signal.SIGTERM), 0, 0, 0), "getppid"]
    # A parent that died before the signal was armed sends none: only the
    # re-check sees it, as a new parent.
    assert not supervisor.parent_guarded(Prctl(), lambda: 1, 42, signal.SIGTERM)
    assert not supervisor.parent_guarded(Prctl(-1), lambda: 42, 42, signal.SIGTERM)


def test_parent_death_kills_every_descendant_then_the_group():
    tree, installer = Tree(TREE), Installer()
    supervisor.guard_parent(Prctl(), tree, installer)
    assert tree.killed == [] and tree.groups == []
    installer.handlers[signal.SIGTERM](signal.SIGTERM, None)
    # The worker in its own session and its module are gone before the group
    # kill ends the supervisor; nothing outside the tree is touched.
    assert tree.groups == [(100, signal.SIGKILL, [101, 102, 103])]
    assert 200 not in tree.killed and 100 not in tree.killed
    assert tree.ended == 128 + signal.SIGKILL


def test_a_parent_that_died_before_arming_ends_the_tree_at_once():
    tree = Tree(TREE, ancestry=(42, 1))
    supervisor.guard_parent(Prctl(), tree, Installer())
    assert tree.groups == [(100, signal.SIGKILL, [101, 102, 103])]
    assert tree.ended == 128 + signal.SIGKILL


def test_a_child_forked_during_termination_is_found_on_the_next_pass():
    tree = Tree(TREE, spawn={103: 104})
    supervisor.terminate(tree)
    assert tree.groups == [(100, signal.SIGKILL, [101, 102, 103, 104])]


def test_termination_is_bounded_when_a_descendant_cannot_die_yet():
    tree = Tree(TREE, unkillable={103})
    supervisor.terminate(tree)
    assert tree.now >= supervisor.TERMINATION_BOUND
    assert tree.groups and tree.groups[0][:2] == (100, signal.SIGKILL)
    assert tree.ended == 128 + signal.SIGKILL


def test_descendants_follow_parent_chains_from_the_root_only():
    assert supervisor.descendants(100, TREE) == {101, 102, 103}
    assert supervisor.descendants(102, TREE) == {103}
    assert supervisor.descendants(100, {}) == set()


def test_running_parents_reads_proc_and_skips_what_runs_nothing(tmp_path):
    stats = {
        "10": b"10 (ansible-playbook) S 1 10 10 0",
        "11": b"11 (a) S (b)) R 10 10 10 0",
        "12": b"12 (python3) Z 10 10 10 0",
        "13": b"13 (python3) X 10 10 10 0",
    }
    for name, stat in stats.items():
        (tmp_path / name).mkdir()
        (tmp_path / name / "stat").write_bytes(stat)
    (tmp_path / "14").mkdir()
    (tmp_path / "self").mkdir()
    assert supervisor.running_parents(str(tmp_path)) == {10: 1, 11: 10}


def test_the_playbook_child_dies_with_its_supervisor():
    prctl, installer = Prctl(), Installer()
    assert supervisor.bind_child(prctl, lambda: 100, 100, installer)
    # The inherited handler would end the supervisor's own tree from the child.
    assert installer.handlers == {signal.SIGTERM: signal.SIG_DFL}
    assert prctl.calls == [(1, int(signal.SIGKILL), 0, 0, 0)]
    assert not supervisor.bind_child(Prctl(), lambda: 1, 100, Installer())


def test_only_a_lifecycle_playbook_child_is_bound_and_an_orphan_never_loads_ansible():
    ended, prctl, installer = [], Prctl(), Installer()
    supervisor.enter_child(True, prctl, lambda: 100, 100, installer, ended.append)
    assert prctl.calls == [(1, int(signal.SIGKILL), 0, 0, 0)]
    assert installer.handlers == {signal.SIGTERM: signal.SIG_DFL}
    assert ended == []
    # Its supervisor died before the binding: the child ends before Ansible.
    supervisor.enter_child(True, Prctl(), lambda: 1, 100, Installer(), ended.append)
    assert ended == [125]
    # A controller child is never bound, whatever became of its supervisor,
    # but it drops the supervisor's termination handler.
    ended, prctl, installer = [], Prctl(), Installer()
    supervisor.enter_child(False, prctl, lambda: 1, 100, installer, ended.append)
    assert prctl.calls == []
    assert installer.handlers == {signal.SIGTERM: signal.SIG_DFL}
    assert ended == []


PLAYBOOK = r"""
import importlib.util, os, sys, time, types
source, record = sys.argv.pop(1), sys.argv.pop(1)
module = types.ModuleType("ansible.cli.playbook")
def playbook():
    if os.fork() == 0:
        os.setsid()
        task = os.fork()
        if task == 0:
            time.sleep(30)
            os._exit(0)
        with open(record, "a") as stream:
            stream.write("%d %d %d\n" % (os.getppid(), os.getpid(), task))
        time.sleep(30)
        os._exit(0)
    time.sleep(30)
module.main = playbook
sys.modules["ansible.cli.playbook"] = module
spec = importlib.util.spec_from_file_location("controller_supervisor_run", source)
implementation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(implementation)
implementation.main()
"""

INVOCATION = r"""
import subprocess, sys, time
child = subprocess.Popen(sys.argv[1:], start_new_session=True)
print(child.pid, flush=True)
time.sleep(30)
"""


def running(pid):
    try:
        with open("/proc/%d/stat" % pid, "rb") as stream:
            stat = stream.read()
    except OSError:
        return False
    return stat[stat.rfind(b")") + 2 :][:1] not in (b"Z", b"X")


def kill_invocation(invocation, _signum):
    invocation.kill()
    invocation.wait()


def signal_supervisor(signum):
    return lambda _invocation, pid: os.kill(pid, signum)


def stopped_tree_runs(tmp_path, marker, stop, watched, settle):
    """Run a supervisor whose worker left its session under an invocation,
    stop it with stop(invocation, supervisor pid), and report whether the
    supervisor, playbook, worker and task each still run once the watched ones
    have ended or settle seconds pass."""
    record = tmp_path / "record"
    record.touch()
    command = [sys.executable, "-I", "-B", "-S", "-c", PLAYBOOK]
    command += [supervisor.__file__, str(record)] + marker
    invocation = subprocess.Popen(
        [sys.executable, "-I", "-c", INVOCATION] + command, stdout=subprocess.PIPE
    )
    with invocation.stdout:
        pids = [int(invocation.stdout.readline())]
    try:
        deadline = time.monotonic() + 10
        while not record.read_text() and time.monotonic() < deadline:
            time.sleep(0.02)
        pids += [int(value) for value in record.read_text().split()]
        assert len(pids) == 4, "the supervised tree did not start"
        stop(invocation, pids[0])
        deadline = time.monotonic() + settle
        while any(running(pids[i]) for i in watched) and time.monotonic() < deadline:
            time.sleep(0.02)
        return [running(pid) for pid in pids]
    finally:
        invocation.kill()
        invocation.wait()
        for pid in filter(running, pids):
            os.kill(pid, signal.SIGKILL)


LINUX = pytest.mark.skipif(not os.path.isdir("/proc/self"), reason="needs /proc")


ALL = range(4)


@LINUX
def test_a_killed_invocation_takes_its_lifecycle_supervisor_tree(tmp_path):
    runs = stopped_tree_runs(tmp_path, ["--lifecycle"], kill_invocation, ALL, 5)
    assert runs == [False] * 4


@LINUX
def test_a_terminated_lifecycle_supervisor_ends_its_tree(tmp_path):
    # The lifecycle runner cancels and refuses this way, not by a group kill,
    # which never reaches the worker in its own session.
    stop = signal_supervisor(signal.SIGTERM)
    assert stopped_tree_runs(tmp_path, ["--lifecycle"], stop, ALL, 5) == [False] * 4


@LINUX
def test_a_lifecycle_playbook_dies_with_its_killed_supervisor(tmp_path):
    # A supervisor killed outright runs no handler, but its playbook child
    # still cannot outlive it.
    stop = signal_supervisor(signal.SIGKILL)
    runs = stopped_tree_runs(tmp_path, ["--lifecycle"], stop, (0, 1), 3)
    assert runs[:2] == [False, False]


@LINUX
def test_a_controller_supervisor_outlives_its_invocation(tmp_path):
    # Acknowledgement gating, not parent death, stops a controller run: its
    # authorized native transaction must be able to finish.
    assert stopped_tree_runs(tmp_path, [], kill_invocation, ALL, 1) == [True] * 4


@LINUX
def test_a_terminated_controller_supervisor_ends_its_tree(tmp_path):
    # The controller runner cancels this way before a native transaction is
    # authorized, so a worker in its own session ends with the tree.
    stop = signal_supervisor(signal.SIGTERM)
    assert stopped_tree_runs(tmp_path, [], stop, ALL, 5) == [False] * 4


@LINUX
def test_a_controller_playbook_outlives_its_killed_supervisor(tmp_path):
    stop = signal_supervisor(signal.SIGKILL)
    runs = stopped_tree_runs(tmp_path, [], stop, (1,), 1)
    assert runs == [False, True, True, True]
