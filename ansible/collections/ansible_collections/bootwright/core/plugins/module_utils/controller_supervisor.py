"""Invocation-local child supervision for authorized controller actions."""

from __future__ import annotations

import ctypes
import os
import signal
import sys
import time

PR_SET_PDEATHSIG = 1
PR_SET_CHILD_SUBREAPER = 36

# Only the lifecycle runner passes this marker, ahead of the playbook
# arguments. A controller run never does: its authorized native transaction
# outlives the invocation and the adapter stops at its next acknowledgement.
LIFECYCLE = "--lifecycle"

# The signal a lifecycle supervisor receives when its invocation dies. The Go
# runner arms the same signal before exec, which covers this process's startup.
PARENT_DEATH = signal.SIGTERM

# How long termination keeps finding descendants that a kill has not ended yet,
# such as one in an uninterruptible wait, before it ends its own group.
TERMINATION_BOUND = 5.0


def lifecycle_mode(argv):
    """Report and consume the lifecycle marker; ansible-playbook never sees it."""
    if argv[1:2] == [LIFECYCLE]:
        del argv[1]
        return True
    return False


def parent_guarded(prctl, getppid, parent, signum):
    """Arm the parent-death signal, then prove the original parent still lives.

    A parent that died before the signal was armed sends none, so only the
    re-check closes that window.
    """
    if prctl(PR_SET_PDEATHSIG, int(signum), 0, 0, 0) != 0:
        return False
    return getppid() == parent


def descendants(root, parents):
    """Every process whose parent chain reaches root, from {pid: parent}."""
    children = {}
    for pid, parent in parents.items():
        children.setdefault(parent, []).append(pid)
    found, pending = set(), [root]
    while pending:
        for pid in children.get(pending.pop(), ()):
            if pid not in found:
                found.add(pid)
                pending.append(pid)
    return found


def running_parents(proc="/proc"):
    """{pid: parent} of every process still running; a zombie runs nothing."""
    parents = {}
    for name in os.listdir(proc):
        if not name.isdigit():
            continue
        try:
            with open(proc + "/" + name + "/stat", "rb") as stream:
                stat = stream.read()
        except OSError:
            continue
        # The command name is parenthesized and may hold anything, so the state
        # and the parent are read after its last closing parenthesis.
        fields = stat[stat.rfind(b")") + 1 :].split()
        if len(fields) > 1 and fields[0] not in (b"Z", b"X"):
            parents[int(name)] = int(fields[1])
    return parents


class System:
    """The process operations termination uses; tests substitute their own."""

    getpid = staticmethod(os.getpid)
    getppid = staticmethod(os.getppid)
    getpgrp = staticmethod(os.getpgrp)
    killpg = staticmethod(os.killpg)
    parents = staticmethod(running_parents)
    monotonic = staticmethod(time.monotonic)
    sleep = staticmethod(time.sleep)
    end = staticmethod(os._exit)

    @staticmethod
    def kill(pid, signum):
        try:
            os.kill(pid, signum)
        except ProcessLookupError:
            pass

    @staticmethod
    def reap():
        """Collect every exited child, including orphans this subreaper inherited."""
        while True:
            try:
                descendant, _status = os.waitpid(-1, os.WNOHANG)
            except ChildProcessError:
                return
            if descendant == 0:
                return


def terminate(system):
    """End the whole owned tree, then this supervisor with its process group.

    Ansible workers move into sessions of their own, beyond a group kill, but a
    child subreaper inherits every orphan, so each descendant stays in this
    process's tree until it is found and killed. Each pass kills what the last
    one left, including a child forked meanwhile.
    """
    own = system.getpid()
    deadline = system.monotonic() + TERMINATION_BOUND
    remaining = descendants(own, system.parents())
    while remaining and system.monotonic() < deadline:
        for pid in sorted(remaining):
            system.kill(pid, signal.SIGKILL)
        system.reap()
        system.sleep(0.01)
        remaining = descendants(own, system.parents())
    system.killpg(system.getpgrp(), signal.SIGKILL)
    system.end(128 + signal.SIGKILL)


def guard_parent(prctl, system, install):
    """Tie a lifecycle supervisor to the invocation that started it."""
    parent = system.getppid()
    install(PARENT_DEATH, lambda *_: terminate(system))
    if not parent_guarded(prctl, system.getppid, parent, PARENT_DEATH):
        terminate(system)


def bind_child(prctl, getppid, supervisor, install):
    """Keep the ansible-playbook child from outliving its supervisor.

    The child drops the supervisor's termination handler, which would end the
    supervisor's own tree, and dies with the supervisor; a supervisor already
    gone refuses before Ansible loads.
    """
    install(PARENT_DEATH, signal.SIG_DFL)
    return parent_guarded(prctl, getppid, supervisor, signal.SIGKILL)


def enter_child(lifecycle, prctl, getppid, supervisor, install, end):
    """Prepare the forked ansible-playbook child before Ansible loads.

    A lifecycle child is bound to its supervisor and ends at once if the
    supervisor is already gone. A controller child is left unbound, so an
    authorized native transaction it runs can finish.
    """
    if lifecycle and not bind_child(prctl, getppid, supervisor, install):
        end(125)


def main():
    root = os.path.dirname(os.path.dirname(sys.executable))
    version = str(sys.version_info.major) + "." + str(sys.version_info.minor)
    stdlib = root + "/lib/python" + version
    sys.path[:] = [
        root + "/lib/python" + version.replace(".", "") + ".zip",
        stdlib,
        stdlib + "/lib-dynload",
        stdlib + "/site-packages",
    ]
    if not (sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode):
        raise RuntimeError("qualified interpreter flags required")
    lifecycle = lifecycle_mode(sys.argv)
    libc = ctypes.CDLL(None, use_errno=True)
    libc.prctl.argtypes = [
        ctypes.c_int,
        ctypes.c_ulong,
        ctypes.c_ulong,
        ctypes.c_ulong,
        ctypes.c_ulong,
    ]
    libc.prctl.restype = ctypes.c_int
    if libc.prctl(PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) != 0:
        os._exit(125)
    if lifecycle:
        guard_parent(libc.prctl, System(), signal.signal)
    supervisor = os.getpid()
    child = os.fork()
    if child == 0:
        enter_child(
            lifecycle, libc.prctl, os.getppid, supervisor, signal.signal, os._exit
        )
        try:
            # This controller-only supervisor deliberately invokes the pinned CLI.
            # pylint: disable=ansible-bad-module-import
            from ansible.cli.playbook import main

            # pylint: enable=ansible-bad-module-import

            sys.argv[0] = "ansible-playbook"
            main()
        except SystemExit as failure:
            os._exit(
                failure.code
                if isinstance(failure.code, int)
                else int(bool(failure.code))
            )
        except BaseException:
            os._exit(1)
        os._exit(0)
    status = 125
    while True:
        try:
            descendant, result = os.waitpid(-1, 0)
        except InterruptedError:
            continue
        except ChildProcessError:
            break
        if descendant == child:
            status = os.waitstatus_to_exitcode(result)
            if status < 0:
                status = 128 - status
    os._exit(status)


if __name__ == "__main__":
    main()
