"""Bounded private commands shared by erase custody and its RAM image."""

from __future__ import annotations

import contextlib
import os
import selectors
import signal
import subprocess
import time

ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}


class Refusal(ValueError):
    """A native process did not finish within its private byte/time budget."""


def run(argv, data=b"", timeout=60, limit=65536):
    if not argv or not os.path.isabs(argv[0]) or len(data) > 65536:
        raise Refusal("native-command")
    # A fresh process group owns descendants; neither native output nor argv
    # reaches an Ansible result, and capture never grows beyond the byte cap.
    # pylint: disable-next=ansible-bad-function
    process = subprocess.Popen(
        argv,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        env=dict(ENV),
        cwd="/",
        close_fds=True,
        start_new_session=True,
        umask=0o077,
    )
    output, pending = bytearray(), memoryview(data)
    deadline = time.monotonic() + timeout
    try:
        with selectors.DefaultSelector() as selector:
            os.set_blocking(process.stdout.fileno(), False)
            selector.register(process.stdout, selectors.EVENT_READ)
            if pending:
                os.set_blocking(process.stdin.fileno(), False)
                selector.register(process.stdin, selectors.EVENT_WRITE)
            else:
                process.stdin.close()
            while selector.get_map():
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise Refusal("native-timeout")
                for key, unused in selector.select(min(remaining, 1)):
                    if key.fileobj is process.stdin:
                        try:
                            count = os.write(key.fd, pending)
                        except BrokenPipeError:
                            count = len(pending)
                        pending = pending[count:]
                        if not pending:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                    else:
                        block = os.read(key.fd, min(65536, limit + 1 - len(output)))
                        if not block:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                            continue
                        output.extend(block)
                        if len(output) > limit:
                            raise Refusal("native-output")
        while time.monotonic() < deadline:
            status = os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            if status is not None:
                code = status.si_status if status.si_code == os.CLD_EXITED else -status.si_status
                return code, bytes(output)
            time.sleep(0.02)
        raise Refusal("native-timeout")
    finally:
        # Reserve the leader PID until every descendant has been killed.
        # poll()/wait() before killpg could release and recycle its group ID.
        with contextlib.suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
        for stream in (process.stdin, process.stdout):
            stream.close()
