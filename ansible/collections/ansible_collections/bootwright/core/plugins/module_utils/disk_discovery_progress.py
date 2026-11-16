"""Fixed, value-free presentation phases; none is completion evidence."""

from __future__ import annotations

from contextlib import contextmanager

GROUPS = (
    "inspect-target", "read-bmc-inventory", "prepare-ssh", "build-media", "publish-media", "verify-delivery", "insert-media",
    "select-boot", "power-on", "await-answer", "receive-inventory", "power-off", "eject-media", "seal-cleanup",
)
RETRIES = ("ssh-timeout", "ssh-refused", "ssh-unreachable", "ssh-not-ready")


def retry(report, cause):
    if cause not in RETRIES:
        raise ValueError("discovery SSH retry cause")
    if report is not None:
        # A bounded observation notice, never a declared work/completion group.
        report(cause, "failed")


@contextmanager
def step(report, group):
    if group not in GROUPS:
        raise ValueError("discovery progress group")
    settled = False

    def finish():
        nonlocal settled
        if not settled and report is not None:
            report(group, "ok")
        settled = True

    if report is not None:
        report(group, "running")
    try:
        yield finish
    except BaseException:
        if not settled and report is not None:
            report(group, "failed")
        raise
    else:
        finish()


def skip(report, groups):
    for group in groups:
        if group not in GROUPS:
            raise ValueError("discovery progress group")
        if report is not None:
            report(group, "skipped")
