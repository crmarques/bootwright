"""Controller-only operation keys and strictly pinned inventory SSH sessions."""

from __future__ import annotations

from itertools import islice
import os
import re
import secrets

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh as ssh
from ansible_collections.bootwright.core.plugins.module_utils import private_records as records
from ansible_collections.bootwright.core.plugins.module_utils import ssh_key_policy

FILES = frozenset(("host-key", "host-key.pub", "client-key", "client-key.pub", "ssh_config", "known_hosts"))
ALGORITHM = "ecdsa-sha2-nistp256"
REFUSED = 42
MAX_REPORT = 48 << 10
ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
CAUSES = (
    (b"Connection timed out", "ssh-timeout"), (b"Connection refused", "ssh-refused"),
    (b"No route to host", "ssh-unreachable"), (b"Network is unreachable", "ssh-unreachable"),
)


def policy():
    value = ssh_key_policy.effective_policy(ssh_key_policy.read_policy())
    ssh_key_policy.check_keys(value, [
        {"secret": "discovery-host", "type": ALGORITHM, "usage": "host"},
        {"secret": "discovery-client", "type": ALGORITHM, "usage": "fleet"},
    ])
    return value


def public_key(path, native):
    records.read(path)
    value = native(["/usr/bin/ssh-keygen", "-y", "-f", str(path)]).decode("ascii").strip()
    if re.fullmatch(r"ecdsa-sha2-nistp256 [A-Za-z0-9+/]+=*", value) is None:
        raise ValueError("discovery SSH key")
    return value


def known_hosts(address, port, key):
    host = address if port == 22 else "[" + address + "]:" + str(port)
    return (host + " " + key + "\n").encode("ascii")


def reap_stages(root):
    with os.scandir(root) as entries:
        names = [entry.name for entry in islice(entries, 4097)]
    if len(names) > 4096:
        raise ValueError("discovery custody entry bound")
    for name in names:
        if re.fullmatch(r"\.ssh-stage-[0-9a-f]{32}", name) is None:
            continue
        remove(root / name)


def remove(directory):
    records.directory(directory, False)
    with os.scandir(directory) as entries:
        names = [entry.name for entry in islice(entries, len(FILES) + 1)]
    if set(names) - FILES or len(names) > len(FILES):
        raise ValueError("foreign discovery SSH material")
    for name in names:
        records.read(directory / name)
        (directory / name).unlink()
    directory.rmdir()


def stage_file(path, data):
    # The outer directory is not published yet. Avoid an inner rename stage
    # whose abandoned name could prevent recovery after process death.
    held = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        if os.write(held, data) != len(data):
            raise ValueError("discovery SSH custody")
        os.fsync(held)
    finally:
        os.close(held)


def prepare(root, request, native):
    selected = policy()  # Prove local crypto policy before building or booting.
    destination = root / "ssh"
    reap_stages(root)
    if not destination.exists():
        stage = root / (".ssh-stage-" + secrets.token_hex(16))
        records.directory(stage, True)
        try:
            for name in ("host-key", "client-key"):
                native(["/usr/bin/ssh-keygen", "-q", "-t", "ecdsa", "-b", "256", "-N", "", "-C", "", "-f", str(stage / name)])
                for path in (stage / name, stage / (name + ".pub")):
                    os.chmod(path, 0o600)
                    records.read(path)
                    with path.open("rb") as stream:
                        os.fsync(stream.fileno())
            key = public_key(stage / "host-key", native)
            stage_file(stage / "ssh_config", selected["configuration"].encode("ascii"))
            stage_file(stage / "known_hosts", known_hosts(request["network"]["address"], request["ssh"]["port"], key))
            held = os.open(stage, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(held)
            finally:
                os.close(held)
            os.rename(stage, destination)
            held = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(held)
            finally:
                os.close(held)
        finally:
            if stage.exists():
                remove(stage)
    validate(destination, request, selected, native)


def validate(directory, request, selected, native):
    records.directory(directory, False)
    with os.scandir(directory) as entries:
        names = {entry.name for entry in islice(entries, len(FILES) + 1)}
    if names != FILES:
        raise ValueError("discovery SSH custody")
    keys = [public_key(directory / name, native) for name in ("host-key", "client-key")]
    for name, key in zip(("host-key.pub", "client-key.pub"), keys):
        if records.read(directory / name).decode("ascii").strip() != key:
            raise ValueError("discovery SSH custody")
    if keys[0] == keys[1] or records.read(directory / "ssh_config") != selected["configuration"].encode("ascii"):
        raise ValueError("discovery SSH custody")
    if records.read(directory / "known_hosts") != known_hosts(request["network"]["address"], request["ssh"]["port"], keys[0]):
        raise ValueError("discovery SSH custody")


def pull(root, request):
    directory, selected = root / "ssh", policy()
    if records.read(directory / "ssh_config") != selected["configuration"].encode("ascii"):
        raise ValueError("discovery SSH policy drift")
    argv = ssh.guest_argv(
        ssh_config=str(directory / "ssh_config"), known_hosts=str(directory / "known_hosts"),
        host_key_algorithms=ALGORITHM, policy_host_key_algorithms=selected["hostKeyAlgorithms"],
        identity=str(directory / "client-key"), user="root", address=request["network"]["address"],
        port=request["ssh"]["port"], remote_argv=["inventory"],
    )
    try:
        result = bounded_process.run(argv, timeout=30, limit=MAX_REPORT + (64 << 10), environment=ENV, cwd="/")
    except bounded_process.Failure as error:
        if error.kind == "timeout":
            return None, "ssh-timeout"
        raise ValueError("discovery SSH client failed") from None
    if len(result.stdout) > MAX_REPORT:
        raise ValueError("discovery report size")
    if result.code == 0:
        if not result.stdout:
            raise ValueError("discovery SSH answer")
        return result.stdout, ""
    if result.code == REFUSED:
        raise ValueError("discovery RAM inventory refused")
    if any(marker in result.stderr for marker in ssh.HOST_KEY_MARKERS):
        raise ValueError("a foreign service answers the discovery address")
    if b"Permission denied" in result.stderr:
        raise ValueError("discovery SSH authentication refused")
    if result.code != 255:
        raise ValueError("discovery SSH answer")
    return None, next((cause for text, cause in CAUSES if text in result.stderr), "ssh-not-ready")
