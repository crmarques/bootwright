"""The one bounded process runner of the collection's controller and managed-host sites.

It starts an absolute executable with an argv list and never a shell, feeds
it bytes, drains both pipes within one byte and time budget, and stops every
descendant before it returns. Each caller maps the three failure kinds to its
own refusal and keeps the argv, environment and output it passes private. A
shipped script that runs alone is a named exception of ``test_python_sites.py``.
"""

from __future__ import annotations

import collections
import contextlib
import errno
import os
import selectors
import signal
import subprocess
import time

MAX_OUTPUT = 1024 * 1024
CHUNK = 65536
# Seconds a descendant that inherited a pipe may keep it open once the leader has exited.
DRAIN = 5
# The kinds of failure: the process could not start or be reaped, it outran its time, or it wrote more than its byte limit.
KINDS = ("command", "timeout", "output")

Result = collections.namedtuple("Result", ("code", "stdout", "stderr"))


class Failure(Exception):
    """A bounded process did not finish within its budget; the kind is the only text."""

    def __init__(self, kind):
        super().__init__(kind)
        self.kind = kind


def require(condition, kind):
    if not condition:
        raise Failure(kind)


def run(argv, data=b"", timeout=60, limit=MAX_OUTPUT, *, environment, cwd=None, session=True, capture_stderr=True, cancel=None, umask=-1):
    """Run argv once with exactly the environment mapping given, which a caller states and never inherits.
    A session owns its process group, which is killed before the leader is reaped; without one, only
    the direct child is killed. Stdout and stderr together stay within limit bytes. Once the leader
    exits, a descendant holding a pipe delays the result by at most DRAIN seconds.
    A bad argument is a command failure before any process starts. A set cancel event, which another
    thread may set, ends the run as a timeout within a second, so the same cleanup stops the process.
    A umask other than -1 is the child's file mode creation mask."""
    data = b"" if data is None else data
    require(argv and os.path.isabs(argv[0]) and all(isinstance(value, str) and "\x00" not in value for value in argv), "command")
    require(isinstance(data, bytes), "command")
    require(isinstance(timeout, (int, float)) and not isinstance(timeout, bool) and 0 < timeout < float("inf"), "command")
    require(isinstance(limit, int) and not isinstance(limit, bool) and limit >= 0, "command")
    require(isinstance(environment, dict) and all(isinstance(key, str) and isinstance(value, str) for key, value in environment.items()), "command")
    require(cancel is None or callable(getattr(cancel, "is_set", None)), "command")
    require(isinstance(umask, int) and not isinstance(umask, bool) and -1 <= umask <= 0o777, "command")
    require(cancel is None or not cancel.is_set(), "timeout")
    try:
        # The bounded fixed-argv runner every caller shares; run_command offers neither group kill nor a byte bound.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(
            argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE if capture_stderr else subprocess.DEVNULL,
            env=environment, cwd=cwd, close_fds=True, start_new_session=session, umask=umask,
        )
    except (OSError, ValueError):
        raise Failure("command") from None
    try:
        result = collect(process, data, time.monotonic() + timeout, limit, cancel)
    except BaseException as failure:
        # A reap that fails here never replaces the failure already in flight.
        stop(process, session)
        if isinstance(failure, OSError):
            raise Failure("command") from None
        raise
    require(stop(process, session), "command")
    return result


def runner(timeout, limit=MAX_OUTPUT):
    """A run with the call shape of Ansible's module.run_command, for the modules whose helpers take that shape.
    Its environ_update is the whole environment, never merged into the module's own, and check_rc must be false.
    It returns the exit code and both streams as text; a run that does not finish raises an OSError, which is
    FileNotFoundError for a missing executable and TimeoutError past its time, whatever handle_exceptions says."""
    def call(argv, check_rc=False, environ_update=None, handle_exceptions=True):
        del handle_exceptions
        require(check_rc is False, "command")
        argv = list(argv)
        try:
            code, output, errors = run(argv, b"", timeout, limit, environment=dict(environ_update or {}))
        except Failure as failure:
            if failure.kind == "command" and argv and isinstance(argv[0], str) and os.path.isabs(argv[0]) and not os.path.lexists(argv[0]):
                raise FileNotFoundError(errno.ENOENT, os.strerror(errno.ENOENT), argv[0]) from None
            if failure.kind == "timeout":
                raise TimeoutError(errno.ETIMEDOUT, "the command outran its %s seconds" % timeout) from None
            raise OSError("the command did not finish within its bounds: %s" % failure.kind) from None
        return code, output.decode("utf-8", "surrogateescape"), errors.decode("utf-8", "surrogateescape")

    return call


def collect(process, data, deadline, limit, cancel):
    """Feed and drain the pipes until both close, or until DRAIN after the leader exits while a descendant holds one."""
    output, stderr, total, pending = bytearray(), bytearray(), 0, memoryview(data)
    status, drained = None, deadline
    with selectors.DefaultSelector() as selector:
        for pipe in (process.stdout, process.stderr):
            if pipe is not None:
                os.set_blocking(pipe.fileno(), False)
                selector.register(pipe, selectors.EVENT_READ)
        if pending:
            os.set_blocking(process.stdin.fileno(), False)
            selector.register(process.stdin, selectors.EVENT_WRITE)
        else:
            process.stdin.close()
        while selector.get_map():
            now = time.monotonic()
            require(deadline > now and (cancel is None or not cancel.is_set()), "timeout")
            if status is None:
                status = exited(process)
                drained = deadline if status is None else min(deadline, now + DRAIN)
            elif now >= drained:
                break
            for key, unused in selector.select(min(drained - now, 1)):
                if key.fileobj is process.stdin:
                    try:
                        count = os.write(key.fd, pending[:CHUNK])
                    except BrokenPipeError:
                        count = len(pending)
                    pending = pending[count:]
                    if not pending:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                    continue
                block = os.read(key.fd, CHUNK)
                if not block:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                total += len(block)
                require(total <= limit, "output")
                (output if key.fileobj is process.stdout else stderr).extend(block)
    while status is None:
        remaining = deadline - time.monotonic()
        require(remaining > 0 and (cancel is None or not cancel.is_set()), "timeout")
        status = exited(process)
        if status is None:
            time.sleep(min(remaining, 0.02))
    require(time.monotonic() < deadline, "timeout")
    return Result(status, bytes(output), bytes(stderr))


def exited(process):
    """The leader's exit code once it has exited, observed without reaping: an exited leader still
    reserves the group identifier until cleanup stops every inherited descendant."""
    status = os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
    if status is None:
        return None
    require(status.si_pid == process.pid, "command")
    return status.si_status if status.si_code == os.CLD_EXITED else -status.si_status


def stop(process, session):
    """Kill the process, or its whole group, and reap it; the leader stays unreaped until the kill.
    It reports whether the leader was reaped and leaves the refusal to the caller."""
    try:
        if session:
            # No poll/wait may release this PID before killpg. Verify that
            # it remains our child even on time/output/refusal paths.
            os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            with contextlib.suppress(ProcessLookupError):
                os.killpg(process.pid, signal.SIGKILL)
        else:
            # A supervisor owns the shared group and stops its descendants; this kills only the command.
            with contextlib.suppress(ProcessLookupError):
                process.kill()
        process.wait(timeout=1)
        return True
    except (subprocess.TimeoutExpired, OSError):
        return False
    finally:
        for pipe in (process.stdin, process.stdout, process.stderr):
            if pipe is not None:
                with contextlib.suppress(OSError):
                    pipe.close()
