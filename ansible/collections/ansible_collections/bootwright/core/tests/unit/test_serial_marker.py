"""Only a test marked serial may fork or signal its own process, because the gate runs those alone.

Under pytest-xdist each worker has an I/O thread of its own, so a fork in a test
may deadlock in the child and a signal sent to the process may reach that
thread. The gate's plain loop runs the tests marked serial alone, in process;
conftest.py refuses the same effects from every other test, so a test that
adds one fails at once where it is written and not only when a worker is busy.
"""

from __future__ import annotations

import os
import signal
import subprocess

import pytest


def test_a_test_without_the_marker_cannot_fork_arm_a_timer_or_signal_its_own_process():
    for effect in (os.fork, lambda: signal.setitimer(signal.ITIMER_REAL, 60), lambda: signal.alarm(60),
                   lambda: os.kill(os.getpid(), signal.SIGUSR1), lambda: signal.raise_signal(signal.SIGUSR1)):
        with pytest.raises(AssertionError, match="needs @pytest.mark.serial"):
            effect()
    assert signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0)


def test_a_test_without_the_marker_may_clear_a_timer_and_probe_or_signal_another_process():
    assert signal.setitimer(signal.ITIMER_REAL, 0) == (0.0, 0.0)
    assert signal.alarm(0) == 0
    os.kill(os.getpid(), 0)
    child = subprocess.Popen(["/usr/bin/sleep", "60"])
    try:
        os.kill(child.pid, signal.SIGTERM)
    finally:
        assert child.wait() == -signal.SIGTERM


@pytest.mark.serial
def test_a_serial_test_may_arm_and_clear_a_timer():
    previous = signal.setitimer(signal.ITIMER_REAL, 60)
    try:
        assert signal.getitimer(signal.ITIMER_REAL)[0] > 0
    finally:
        signal.setitimer(signal.ITIMER_REAL, *previous)
    assert signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0)
