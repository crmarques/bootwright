"""The controller's SSH crypto policy for its direct Ceph sessions; it runs on the controller only.

Each session reads the controller's reduced policy configuration instead of
-F /dev/null, and pins, for the one key the frozen known_hosts holds for its
host, only the algorithms the policy that `ssh -G` evaluated admits. The
option set is the managed-OS guest session's, option for option. An empty
admission refuses before any host is contacted, naming the host and its key
type, both public.
"""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_tunnel, managedos_guest_ssh, ssh_key_policy

EXCLUDED = "ssh-policy-host-excluded"
UNAVAILABLE = "ssh-policy-unavailable"


class PolicyRefusal(ceph_native.Refusal):
    """A closed reason with an optional public detail: a host name and a key type."""

    def __init__(self, reason, detail=None):
        super().__init__(reason)
        self.detail = detail


def evaluate_controller_policy(read=ssh_key_policy.read_policy, evaluate=ssh_key_policy.effective_policy):
    """The controller's reduced policy configuration and the host-key algorithm list OpenSSH evaluates from it."""
    try:
        policy = evaluate(read())
    except ssh_key_policy.PolicyError:
        raise PolicyRefusal(UNAVAILABLE) from None
    return {"configuration": policy["configuration"], "hostKeyAlgorithms": policy["hostKeyAlgorithms"]}


def controller_policy():
    return evaluate_controller_policy()


def known_lines(known_bytes):
    """Every known_hosts entry as its three fields; no option, marker or comment field."""
    ceph_native.require(isinstance(known_bytes, bytes), "material")
    try:
        lines = known_bytes.decode("ascii").splitlines()
    except UnicodeError:
        raise ceph_native.Refusal("material") from None
    result = []
    for line in lines:
        if not line.strip():
            continue
        fields = line.split()
        ceph_native.require(len(fields) == 3, "material")
        result.append(fields)
    return result


def pinned(known_bytes, host):
    """Whether the frozen known_hosts holds an entry for the host's endpoint."""
    return any(fields[0] == ceph_tunnel.host_token(host) for fields in known_lines(known_bytes))


def pinned_key_type(known_bytes, host):
    """The type of the one key the frozen known_hosts pins for the host's endpoint."""
    found = [fields for fields in known_lines(known_bytes) if fields[0] == ceph_tunnel.host_token(host)]
    ceph_native.require(len(found) == 1 and found[0][1] in ceph_tunnel.HOST_KEY_ALGORITHMS, "material")
    return found[0][1]


def host_key_algorithms(policy, known, host):
    """The pinned key's algorithms the evaluated policy admits, in pinned order; none refuses naming the host and its key type."""
    key_type = pinned_key_type(known, host)
    algorithms = ceph_tunnel.pinned_host_key_algorithms(key_type, policy["hostKeyAlgorithms"])
    if not algorithms:
        raise PolicyRefusal(EXCLUDED, host["name"] + ":" + key_type)
    return ",".join(algorithms)


def session_argv(ssh_config, known_hosts, algorithms, identity, port):
    """The guest session's options around the reduced policy configuration; never -F /dev/null."""
    ceph_native.require(isinstance(ssh_config, str) and ssh_config != "/dev/null" and isinstance(algorithms, str) and algorithms != "", "transport")
    options = ["UserKnownHostsFile=" + known_hosts] + list(managedos_guest_ssh.OPTIONS)
    options.insert(managedos_guest_ssh.HOST_KEY_SLOT + 1, "HostKeyAlgorithms=" + algorithms)
    argv = ["/usr/bin/ssh", "-T", "-F", ssh_config, "-i", identity, "-p", str(port)]
    for option in options:
        argv.extend(["-o", option])
    return argv
