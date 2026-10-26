"""The one quoting of the command string an SSH session runs on a remote host.

The SSH protocol carries a remote command as one string, which the remote
account's shell interprets. Every site that builds one joins a fixed argv here,
so each token is a literal or a closed validated token quoted once, the same
way. ``test_python_sites.py`` refuses shell quoting anywhere else under plugins.
"""

from __future__ import annotations

import shlex

# The admitted shape of a remote fixed worker: an absolute interpreter run through sudo -n, its code shipped with the automation.
FIXED_WORKER = ("/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c")


def remote_command(argv):
    """The remote command string of a non-empty argv of non-empty strings without NUL."""
    if not isinstance(argv, (list, tuple)) or not argv or \
            any(not isinstance(token, str) or not token or "\0" in token for token in argv):
        raise ValueError("the remote command is not an argv of non-empty strings")
    return shlex.join(argv)


def fixed_worker_argv(source, *arguments):
    """The argv that runs the shipped worker source as root, with literal arguments after it."""
    return FIXED_WORKER + (source,) + arguments


def fixed_worker(source, *arguments):
    """The remote command that runs the shipped worker source as root, with literal arguments after it."""
    return remote_command(fixed_worker_argv(source, *arguments))
