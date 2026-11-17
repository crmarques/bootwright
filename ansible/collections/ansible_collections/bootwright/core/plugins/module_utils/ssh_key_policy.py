"""Read effective placement-host SSH crypto policy without opening a session."""

from __future__ import annotations

import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process, ceph_tunnel

POLICY_PATH = "/etc/crypto-policies/back-ends/openssh.config"
MAX_POLICY = 64 * 1024
# ceph_tunnel runs alone on a host, so it owns the key-type table and the
# crypto-only boundary internal/machine/sshlocal/policy.go also holds.
ALGORITHMS = {key_type: ",".join(names) for key_type, names in ceph_tunnel.HOST_KEY_ALGORITHMS.items()}
DIRECTIVES = ceph_tunnel.CRYPTO_DIRECTIVES


class PolicyError(ValueError):
    """A bounded public refusal; never includes key material or native output."""

    def __init__(self, message, reason="ssh-policy-unavailable"):
        super().__init__(message)
        self.reason = reason


def read_policy():
    """The system-managed backend may be a symlink; read its regular target once."""
    try:
        descriptor = os.open(POLICY_PATH, os.O_RDONLY | os.O_NONBLOCK)
    except FileNotFoundError:
        if os.path.lexists(POLICY_PATH):
            raise PolicyError("the placement host SSH crypto policy target is missing") from None
        return ""
    except OSError as exc:
        raise PolicyError("cannot read the placement host SSH crypto policy") from exc
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise PolicyError("the placement host SSH crypto policy is not a regular file")
        with os.fdopen(descriptor, "rb", closefd=False) as source:
            data = source.read(MAX_POLICY + 1)
        if len(data) > MAX_POLICY:
            raise PolicyError("the placement host SSH crypto policy exceeds its byte limit")
        return data.decode("ascii")
    except (OSError, UnicodeError) as exc:
        raise PolicyError("cannot read the placement host SSH crypto policy") from exc
    finally:
        os.close(descriptor)


def configuration(data):
    """Keep only closed crypto directives, letting OpenSSH evaluate list modifiers."""
    if len(data) > MAX_POLICY:
        raise PolicyError("the placement host SSH crypto policy exceeds its byte limit")
    try:
        return ceph_tunnel.crypto_policy(data)
    except ceph_tunnel.TunnelRefusal:
        raise PolicyError("the placement host SSH crypto policy has unsupported syntax or a non-crypto directive") from None


def effective_policy(data, run=bounded_process.run):
    """OpenSSH -G computes policy/default lists; no endpoint, key or shell is used."""
    config = configuration(data)
    try:
        code, output, unused = run(
            ["/usr/bin/ssh", "-G", "-F", "/dev/stdin", "-o", "CanonicalizeHostname=no", "localhost"],
            config.encode("utf-8"), 10, 256 * 1024, environment={"PATH": "/usr/bin:/bin", "LC_ALL": "C"}, capture_stderr=False,
        )
        text = output.decode("utf-8")
    except (bounded_process.Failure, UnicodeError) as exc:
        raise PolicyError("cannot evaluate the placement host SSH crypto policy before installation") from exc
    if code != 0:
        raise PolicyError("cannot evaluate the placement host SSH crypto policy before installation")
    values = {}
    for line in text.splitlines():
        fields = line.split(None, 1)
        if len(fields) != 2:
            continue
        name, value = fields
        if name in ("hostkeyalgorithms", "pubkeyacceptedalgorithms", "pubkeyacceptedkeytypes"):
            if not re.fullmatch(r"[A-Za-z0-9@.,_-]+", value):
                raise PolicyError("the placement host returned an unsupported SSH algorithm list")
            values[name] = value
    host = values.get("hostkeyalgorithms", "")
    fleet = values.get("pubkeyacceptedalgorithms", values.get("pubkeyacceptedkeytypes", ""))
    if not host or not fleet:
        raise PolicyError("the placement host did not report both SSH key algorithm policies")
    return {"configuration": config, "hostKeyAlgorithms": host, "fleetKeyAlgorithms": fleet}


# ssh-rsa, the SHA-1 signature, is deliberately absent, unlike the controller's
# own host-key records (internal/trust/hostkeys.go), which accept it for hosts
# they did not install: an installed RHEL 9 sshd signs an rsa host key with
# SHA-2, and a controller under the FIPS crypto policy refuses SHA-1.
def pinned_algorithms(line, allowed=None):
    """The HostKeyAlgorithms value that accepts exactly the pinned key's type, within the allowed list when one is given."""
    fields = line.split() if isinstance(line, str) else []
    if not fields:
        raise PolicyError("the pinned SSH host key is empty or missing; restore its identity proof before connecting")
    key_type = fields[0]
    if key_type in ALGORITHMS:
        candidates = ALGORITHMS[key_type].split(",")
        if allowed is not None:
            candidates = [value for value in candidates if value in allowed.split(",")]
        if not candidates:
            raise PolicyError("the pinned SSH host key type %s is excluded by the placement host SSH policy" % key_type,
                              "ssh-policy-host-excluded")
        return ",".join(candidates)
    printable = "".join(character for character in key_type if character.isprintable())[:64]
    raise PolicyError("the pinned SSH host key's type %s is not one an installation pins" % printable)


def check_keys(policy, keys):
    """Validate actual bound public-key types, never a declared provider policy."""
    for key in keys:
        key_type, usage = key.get("type"), key.get("usage")
        if usage == "generated":
            check_generated(policy, key.get("machine"), key_type)
            continue
        secret = key.get("secret")
        if not isinstance(secret, str) or not re.fullmatch(r"[a-z0-9][a-z0-9-]*", secret) or key_type not in ALGORITHMS:
            raise PolicyError("the SSH policy check requires a Secret name and a qualified bound key type")
        candidates = ALGORITHMS[key_type].split(",")
        accepted = policy["hostKeyAlgorithms" if usage == "host" else "fleetKeyAlgorithms"].split(",")
        if not set(candidates).intersection(accepted):
            raise PolicyError("Secret/%s key type %s is excluded by the placement host SSH %s key policy; nothing was booted" %
                              (secret, key_type, usage), "ssh-policy-%s-excluded" % usage)


def check_generated(policy, machine, key_type):
    """The host key an installation generates must be one this host's client accepts."""
    if not isinstance(machine, str) or not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?", machine) or key_type not in ALGORITHMS:
        raise PolicyError("the SSH policy check requires a Machine name and a qualified generated key type")
    if not set(ALGORITHMS[key_type].split(",")).intersection(policy["hostKeyAlgorithms"].split(",")):
        raise PolicyError("the %s host key the installation of Machine/%s generates is excluded by the placement host SSH host key policy; nothing was booted" %
                          (key_type, machine), "ssh-policy-generated-excluded")
