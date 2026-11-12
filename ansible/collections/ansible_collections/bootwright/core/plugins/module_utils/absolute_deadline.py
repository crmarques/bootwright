"""A total deadline for one main-thread native exchange, including slow I/O."""

from __future__ import annotations

from contextlib import contextmanager
import signal


def expired(signum, frame):
    del signum, frame
    raise TimeoutError("native exchange exceeded its total deadline")


@contextmanager
def limited(seconds):
    """Never replace another owner's timer; each module/worker is a process."""
    if not isinstance(seconds, (float, int)) or isinstance(seconds, bool) or not 0 < seconds <= 1800:
        raise ValueError("invalid native exchange deadline")
    if signal.getitimer(signal.ITIMER_REAL) != (0.0, 0.0):
        raise ValueError("another owner already holds the process deadline")
    previous = signal.signal(signal.SIGALRM, expired)
    try:
        signal.setitimer(signal.ITIMER_REAL, seconds)
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)
