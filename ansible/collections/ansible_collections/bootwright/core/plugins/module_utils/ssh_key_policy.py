"""Read effective placement-host SSH crypto policy without opening a session."""

from __future__ import annotations

import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process

POLICY_PATH = "/etc/crypto-policies/back-ends/openssh.config"
MAX_POLICY = 64 * 1024
ALGORITHMS = {
    "ssh-ed25519": "ssh-ed25519",
    "ssh-rsa": "rsa-sha2-512,rsa-sha2-256",
    "ecdsa-sha2-nistp256": "ecdsa-sha2-nistp256",
    "ecdsa-sha2-nistp384": "ecdsa-sha2-nistp384",
    "ecdsa-sha2-nistp521": "ecdsa-sha2-nistp521",
}
# Same crypto-only boundary as internal/machine/sshlocal/policy.go. Reject
# everything else rather than evaluate Include, Match exec or connection data.
DIRECTIVES = frozenset((
    "ciphers", "macs", "kexalgorithms", "gssapikexalgorithms",
    "hostkeyalgorithms", "pubkeyacceptedalgorithms", "pubkeyacceptedkeytypes",
    "hostbasedacceptedalgorithms", "hostbasedkeytypes", "casignaturealgorithms",
    "requiredrsasize", "rsaminsize", "rekeylimit",
))


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
    lines = []
    for raw in data.splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        match = re.fullmatch(r"([A-Za-z]+)\s*(?:=\s*|\s+)([A-Za-z0-9@.,_+^*?!/ -]+)", line)
        if match is None or match.group(1).lower() not in DIRECTIVES:
            raise PolicyError("the placement host SSH crypto policy has unsupported syntax or a non-crypto directive")
        lines.append(line)
    return "\n".join(lines) + ("\n" if lines else "")


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
