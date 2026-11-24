"""Set and prove the static and kernel hostname of one provided RHEL host."""

from __future__ import annotations

import os
import re

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process

HOSTNAMECTL = "/usr/bin/hostnamectl"
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
TIMEOUT = 30
MAX_OUTPUT = 4096
DIGEST = re.compile(r"[0-9a-f]{64}")
LABEL = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?")
STATES = ("absent", "complete")
REFUSALS = ("hostname-write", "hostname-unproved")


class Refused(Exception):
    """A closed refusal; the reason is one of REFUSALS."""

    def __init__(self, reason):
        super().__init__(reason)
        self.reason = reason


def valid_hostname(value):
    return (isinstance(value, str) and 0 < len(value) <= 253 and all(LABEL.fullmatch(label) for label in value.split(".")))


def hostnamectl(*arguments):
    """Run hostnamectl once, bounded; return its exit code and trimmed output."""
    try:
        code, output, _errors = bounded_process.run(
            [HOSTNAMECTL, *arguments], timeout=TIMEOUT, limit=MAX_OUTPUT, environment=ENVIRONMENT, cwd="/")
    except bounded_process.Failure:
        return None, ""
    return code, output.decode("utf-8", "replace").strip()


def current():
    """The kernel nodename and the static hostname, each as the host reports it."""
    code, static = hostnamectl("--static")
    if code != 0:
        raise Refused("hostname-unproved")
    return os.uname().nodename, static


def holds(hostname):
    return current() == (hostname, hostname)


def execute(operation, digest, hostname):
    if (operation not in ("apply", "observe") or not isinstance(digest, str) or not DIGEST.fullmatch(digest)
            or not valid_hostname(hostname) or os.geteuid() != 0):
        raise Refused("hostname-write")
    if holds(hostname):
        return {"changed": False, "state": "complete", "hostname": hostname}
    if operation == "observe":
        return {"changed": False, "state": "absent", "hostname": hostname}
    code, _output = hostnamectl("set-hostname", hostname)
    if code != 0:
        raise Refused("hostname-write")
    if not holds(hostname):
        raise Refused("hostname-unproved")
    return {"changed": True, "state": "complete", "hostname": hostname}
