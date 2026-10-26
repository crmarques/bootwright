"""One argv builder for every fixed command an installation runs on its installed machine.

The command runs as the fleet account over the connection the role's guest.yml
and reachable.yml make, option for option: the placement host's reduced
policy configuration, the one known_hosts file that holds the key the identity
channel reported, strict host-key checking and host-key algorithms the
placement host's policy permits. It never reads -F /dev/null and never accepts
a first-use answer. Standard input is the only way data reaches the command.
"""

from __future__ import annotations

import collections
import ipaddress
import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, remote_command, ssh_key_policy

SSH = "/usr/bin/ssh"
OPTIONS = (
    "GlobalKnownHostsFile=none",
    "KnownHostsCommand=none",
    "VerifyHostKeyDNS=no",
    "UpdateHostKeys=no",
    "CheckHostIP=no",
    "StrictHostKeyChecking=yes",
    "IdentitiesOnly=yes",
    "IdentityAgent=none",
    "PasswordAuthentication=no",
    "KbdInteractiveAuthentication=no",
    "GSSAPIAuthentication=no",
    "HostbasedAuthentication=no",
    "ForwardAgent=no",
    "ProxyCommand=none",
    "ProxyJump=none",
    "ControlMaster=no",
    "ControlPath=none",
    "BatchMode=yes",
    "ConnectTimeout=10",
    "ServerAliveInterval=15",
    "ServerAliveCountMax=3",
)
HOST_KEY_SLOT = OPTIONS.index("StrictHostKeyChecking=yes") + 1
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
MAX_OUTPUT = 65536
MAX_DIAGNOSTIC = 65536
MAX_REMOTE = 1 << 20
HOST_KEY_MARKERS = (
    b"Host key verification failed.",
    b"REMOTE HOST IDENTIFICATION HAS CHANGED",
    b"no matching host key type found",
)
USER = re.compile(r"[a-z_][a-z0-9_-]{0,31}")
LABEL = re.compile(r"[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?")

Outcome = collections.namedtuple("Outcome", ("code", "stdout", "host_key", "timed_out", "cause"), defaults=(None,))


class SessionError(ValueError):
    """A session this builder refuses to open; its text names no value."""


def vocabulary():
    """Every host-key algorithm the placement-host policy can name."""
    return frozenset(name for value in ssh_key_policy.ALGORITHMS.values() for name in value.split(","))


def require_file(path, label):
    """An absolute, plain path to a regular file that ssh_config(5) reads literally."""
    if not isinstance(path, str) or not path or not os.path.isabs(path):
        raise SessionError("the " + label + " is not an absolute path")
    if any(character.isspace() or character == "%" or ord(character) < 0x20 or ord(character) == 0x7F for character in path):
        raise SessionError("the " + label + " path holds whitespace, a percent sign or a control character")
    try:
        status = os.lstat(path)
    except OSError:
        raise SessionError("the " + label + " is not a regular file") from None
    if not stat.S_ISREG(status.st_mode):
        raise SessionError("the " + label + " is not a regular file")


def require_algorithms(pinned, policy):
    """The pinned algorithms are known, distinct and all permitted by the placement host's policy."""
    if not isinstance(pinned, str) or not pinned:
        raise SessionError("the pinned host-key algorithms are empty")
    names = pinned.split(",")
    known = vocabulary()
    if len(set(names)) != len(names) or any(not name or name[0] in "+-^" or name not in known for name in names):
        raise SessionError("the pinned host-key algorithms are not a list of distinct known algorithms")
    if not isinstance(policy, str) or not policy:
        raise SessionError("the placement host SSH policy permits no host-key algorithm")
    permitted = set(policy.split(","))
    if any(name not in permitted for name in names):
        raise SessionError("the pinned host-key algorithms are not all permitted by the placement host SSH policy")


def require_destination(user, address):
    """A fleet account name and an IP literal or DNS name ssh cannot read as an option."""
    if not isinstance(user, str) or not USER.fullmatch(user):
        raise SessionError("the guest account name is not valid")
    if not isinstance(address, str) or not address or address.startswith("-"):
        raise SessionError("the guest address is not valid")
    try:
        ipaddress.ip_address(address)
        return
    except ValueError:
        pass
    if len(address) > 253 or not all(LABEL.fullmatch(label) for label in address.split(".")):
        raise SessionError("the guest address is not valid")


def guest_argv(*, ssh_config, known_hosts, host_key_algorithms, policy_host_key_algorithms, identity, user, address, remote_argv):
    """The pinned session's argv for one remote command, given as an argv that remote_command joins."""
    if ssh_config == "/dev/null":
        raise SessionError("the guest session requires the reduced placement-host policy configuration")
    require_file(ssh_config, "client configuration")
    require_file(known_hosts, "known-hosts file")
    require_file(identity, "fleet identity")
    require_algorithms(host_key_algorithms, policy_host_key_algorithms)
    require_destination(user, address)
    try:
        remote = remote_command.remote_command(remote_argv)
    except ValueError:
        raise SessionError("the remote command is not valid") from None
    if len(remote.encode("utf-8", "surrogatepass")) > MAX_REMOTE:
        raise SessionError("the remote command is not valid")
    options = ["UserKnownHostsFile=" + known_hosts] + list(OPTIONS)
    options.insert(HOST_KEY_SLOT + 1, "HostKeyAlgorithms=" + host_key_algorithms)
    argv = [SSH, "-F", ssh_config]
    for option in options:
        argv.extend(["-o", option])
    return argv + ["-i", identity, user + "@" + address, remote]


def run(argv, data, timeout, runner=bounded_process.run):
    """Run the session once; stderr only reports whether the pinned key refused it."""
    try:
        code, stdout, stderr = runner(argv, data, timeout, MAX_OUTPUT + MAX_DIAGNOSTIC, environment=dict(ENVIRONMENT), cwd="/")
    except bounded_process.Failure as failure:
        if failure.kind == "timeout":
            return Outcome(None, b"", False, True)
        # The runner drains stderr itself, so a refused key cannot be told from a start or overrun failure.
        return Outcome(None, None, False, False, "answer" if failure.kind == "output" else "session")
    host_key = any(marker in stderr for marker in HOST_KEY_MARKERS)
    return Outcome(code, None if len(stdout) > MAX_OUTPUT else stdout, host_key, False)
