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
import os
import selectors
import signal
import subprocess
import time

MAX_OUTPUT = 1024 * 1024
CHUNK = 65536
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


def run(argv, data=b"", timeout=60, limit=MAX_OUTPUT, *, environment, cwd=None, session=True, capture_stderr=True):
    """Run argv once with exactly the environment mapping given, which a caller states and never inherits.
    A session owns its process group, which is killed before the leader is reaped; without one, only
    the direct child is killed. Stdout and stderr together stay within limit bytes.
    A bad argument is a command failure before any process starts."""
    data = b"" if data is None else data
    require(argv and os.path.isabs(argv[0]) and all(isinstance(value, str) and "\x00" not in value for value in argv), "command")
    require(isinstance(data, bytes), "command")
    require(isinstance(timeout, (int, float)) and not isinstance(timeout, bool) and 0 < timeout < float("inf"), "command")
    require(isinstance(limit, int) and not isinstance(limit, bool) and limit >= 0, "command")
    require(isinstance(environment, dict) and all(isinstance(key, str) and isinstance(value, str) for key, value in environment.items()), "command")
    try:
        # The bounded fixed-argv runner every caller shares; run_command offers neither group kill nor a byte bound.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(
            argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE if capture_stderr else subprocess.DEVNULL,
            env=environment, cwd=cwd, close_fds=True, start_new_session=session,
        )
    except (OSError, ValueError):
        raise Failure("command") from None
    try:
        output = bytearray()
        stderr = bytearray()
        total = 0
        pending = memoryview(data)
        deadline = time.monotonic() + timeout
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
                remaining = deadline - time.monotonic()
                require(remaining > 0, "timeout")
                for key, unused in selector.select(min(remaining, 1)):
                    if key.fileobj is process.stdin:
                        try:
                            count = os.write(key.fd, pending[:CHUNK])
                        except BrokenPipeError:
                            count = len(pending)
                        pending = pending[count:]
                        if not pending:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                    else:
                        block = os.read(key.fd, CHUNK)
                        if not block:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                            continue
                        total += len(block)
                        require(total <= limit, "output")
                        if key.fileobj is process.stdout:
                            output.extend(block)
                        else:
                            stderr.extend(block)
        # Observe exit without reaping: an exited leader still reserves the
        # group identifier until cleanup stops every inherited descendant.
        while True:
            remaining = deadline - time.monotonic()
            require(remaining > 0, "timeout")
            status = os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            if status is not None:
                require(status.si_pid == process.pid, "command")
                code = status.si_status if status.si_code == os.CLD_EXITED else -status.si_status
                require(time.monotonic() < deadline, "timeout")
                return Result(code, bytes(output), bytes(stderr))
            time.sleep(min(remaining, 0.02))
    except (subprocess.TimeoutExpired, OSError):
        raise Failure("command") from None
    finally:
        stop(process, session)


def stop(process, session):
    """Kill the process, or its whole group, and reap it; the leader stays unreaped until the kill."""
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
    except (subprocess.TimeoutExpired, OSError):
        raise Failure("command") from None
    finally:
        for pipe in (process.stdin, process.stdout, process.stderr):
            if pipe is not None:
                with contextlib.suppress(OSError):
                    pipe.close()
