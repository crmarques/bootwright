"""The one pinned managed-machine SSH session: its files, its argv and its run.

A session runs one command as the fleet account over the placement host's
reduced policy configuration, the one known_hosts file that holds the key the
identity channel reported, strict host-key checking and host-key algorithms
the placement host's policy permits. open_session writes the two files for one
connection and names the algorithms; guest_argv builds the argv over them;
run and command run it once. It never reads -F /dev/null and never accepts a
first-use answer. Standard input is the only way data reaches the command.
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
MAX_COMMAND_OUTPUT = 1 << 20
SESSION_FILES = ("known_hosts", "ssh_config")
HOST_KEY_MARKERS = (
    b"Host key verification failed.",
    b"REMOTE HOST IDENTIFICATION HAS CHANGED",
    b"no matching host key type found",
)
USER = re.compile(r"[a-z_][a-z0-9_-]{0,31}")
LABEL = re.compile(r"[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?")

Outcome = collections.namedtuple("Outcome", ("code", "stdout", "host_key", "timed_out", "cause"), defaults=(None,))
Answer = collections.namedtuple("Answer", ("code", "stdout", "stderr", "failure"))


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
    require_address(address)


def require_address(address):
    """An IP literal or DNS name ssh cannot read as an option and a known_hosts line holds as one field."""
    if not isinstance(address, str) or not address or address.startswith("-"):
        raise SessionError("the guest address is not valid")
    try:
        ipaddress.ip_address(address)
        return
    except ValueError:
        pass
    if len(address) > 253 or not all(LABEL.fullmatch(label) for label in address.split(".")):
        raise SessionError("the guest address is not valid")


def guest_argv(*, ssh_config, known_hosts, host_key_algorithms, policy_host_key_algorithms, identity, user, address, remote_argv, port=22):
    """The pinned session's argv for one remote command, given as an argv that remote_command joins."""
    if ssh_config == "/dev/null":
        raise SessionError("the guest session requires the reduced placement-host policy configuration")
    require_file(ssh_config, "client configuration")
    require_file(known_hosts, "known-hosts file")
    require_file(identity, "fleet identity")
    require_algorithms(host_key_algorithms, policy_host_key_algorithms)
    require_destination(user, address)
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise SessionError("the guest SSH port is not valid")
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
    if port != 22:
        argv.extend(["-p", str(port)])
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


def require_directory(directory):
    """An absolute directory, not a link, that holds one connection's files."""
    if not isinstance(directory, str) or not os.path.isabs(directory):
        raise SessionError("the session directory is not an absolute path")
    try:
        status = os.lstat(directory)
    except OSError:
        raise SessionError("the session directory is not a directory") from None
    if not stat.S_ISDIR(status.st_mode):
        raise SessionError("the session directory is not a directory")


def write_private(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="ascii", closefd=False) as target:
            target.write(text)
    finally:
        os.close(descriptor)


def open_session(directory, address, host_key, policy):
    """Write one connection's known_hosts and reduced configuration under directory; the guest_argv arguments they make.

    The policy is the placement host's effective policy (ssh_key_policy.effective_policy).
    The pinned algorithms are the key's own within that policy, so pinning never
    re-enables an algorithm the policy excludes; ssh_key_policy.PolicyError
    names a refused key. The known_hosts file holds the one reported key for
    the one address, and nothing else.
    """
    require_directory(directory)
    require_address(address)
    line = host_key.strip() if isinstance(host_key, str) else ""
    if not line:
        raise SessionError("the pinned SSH host key is empty or missing; restore its identity proof before connecting")
    if not line.isprintable() or not line.isascii():
        raise SessionError("the pinned host key is not one printable line")
    algorithms = ssh_key_policy.pinned_algorithms(line, policy["hostKeyAlgorithms"])
    paths = {name: os.path.join(directory, name) for name in SESSION_FILES}
    try:
        write_private(paths["known_hosts"], address + " " + line)
        write_private(paths["ssh_config"], policy["configuration"])
    except (OSError, UnicodeError):
        close_session(directory)
        raise SessionError("the session files could not be written") from None
    return {"ssh_config": paths["ssh_config"], "known_hosts": paths["known_hosts"],
            "host_key_algorithms": algorithms, "policy_host_key_algorithms": policy["hostKeyAlgorithms"]}


def close_session(directory):
    """Remove one connection's files; a file already gone is removed."""
    require_directory(directory)
    for name in SESSION_FILES:
        try:
            os.unlink(os.path.join(directory, name))
        except FileNotFoundError:
            pass


def command(argv, data, timeout, runner=bounded_process.run):
    """Run one guest command once: its exit status and both streams, or the failure kind that ended it."""
    try:
        code, stdout, stderr = runner(argv, data, timeout, MAX_COMMAND_OUTPUT + MAX_DIAGNOSTIC, environment=dict(ENVIRONMENT), cwd="/")
    except bounded_process.Failure as failure:
        return Answer(None, b"", b"", failure.kind)
    if len(stdout) > MAX_COMMAND_OUTPUT or len(stderr) > MAX_DIAGNOSTIC:
        return Answer(None, b"", b"", "output")
    return Answer(code, stdout, stderr, None)
