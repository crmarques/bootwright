"""Dedicated erase-only Anaconda entrypoint; it never returns to installation."""

from __future__ import annotations

import base64
import inspect
import json
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process
from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity

RAM = "/run/bootwright-erase"
RUNTIME = r'''
import hashlib
import json
import os
from pathlib import Path
import sys
import time
import physical_erase_worker as worker
import physical_erase_process as process

ROOT = Path("/run/bootwright-erase")

def command(argv):
    code, output = process.run(argv, timeout=10, limit=4096)
    if code:
        raise worker.Refusal("installer-boundary")
    return output.decode("ascii").strip()

def isolate():
    if command(["/usr/bin/findmnt", "-n", "-o", "FSTYPE", "--target", "/run"]) != "tmpfs":
        raise worker.Refusal("ram-filesystem")
    root = json.loads(command(["/usr/bin/findmnt", "--json", "-o", "FSTYPE,OPTIONS", "--target", "/"]))
    if set(root) != {"filesystems"} or len(root["filesystems"]) != 1:
        raise worker.Refusal("ram-root")
    filesystem = root["filesystems"][0]
    if filesystem["fstype"] == "overlay":
        options = dict(value.split("=", 1) for value in filesystem["options"].split(",") if "=" in value)
        for key in ("upperdir", "workdir"):
            path = options.get(key, "")
            if not path.startswith("/run/") or "\\" in path or command(["/usr/bin/findmnt", "-n", "-o", "FSTYPE", "--target", path]) != "tmpfs":
                raise worker.Refusal("ram-upper")
    elif filesystem["fstype"] not in ("rootfs", "tmpfs"):
        raise worker.Refusal("ram-root")
    required = {"bootwright.erase=1", "rd.live.ram=1", "rd.live.overlay.overlayfs=1", "inst.zram=0"}
    if not required.issubset(Path("/proc/cmdline").read_text().split()):
        raise worker.Refusal("boot-purpose")
    units = ("anaconda.service", "anaconda-direct.service", "anaconda-sshd.service", "sshd.service", "sshd.socket")
    loaded = {unit: command(["/usr/bin/systemctl", "show", "--property=LoadState", "--value", unit]) for unit in units}
    if loaded["anaconda.service"] != "loaded" or any(value not in ("loaded", "not-found") for value in loaded.values()):
        raise worker.Refusal("installer-units")
    for unit in units:
        command(["/usr/bin/systemctl", "mask", "--runtime", unit])
    # SIGKILL prevents any installer exit hook from initiating storage cleanup
    # or reboot. The synchronous %pre has not returned and no storage directives
    # exist in this image. Both frontend entrypoints must be fully stopped.
    for unit in units:
        if loaded[unit] == "not-found":
            continue
        process.run(["/usr/bin/systemctl", "kill", "--kill-whom=all", "--signal=KILL", unit], timeout=10, limit=4096)
        command(["/usr/bin/systemctl", "stop", unit])
        if command(["/usr/bin/systemctl", "show", "--property=ActiveState", "--value", unit]) not in ("inactive", "failed"):
            raise worker.Refusal("installer-running")
    # A stopped service with escaped descendants is not an offline boundary.
    for entry in Path("/proc").glob("[0-9]*/cmdline"):
        try:
            with entry.open("rb") as stream:
                data = stream.read(65537)
        except FileNotFoundError:
            continue
        if len(data) > 65536:
            raise worker.Refusal("process-inventory")
        argv = data.split(b"\0")
        if any(value in (b"anaconda", b"/usr/sbin/anaconda", b"/usr/bin/anaconda") or b"pyanaconda.modules." in value for value in argv):
            raise worker.Refusal("installer-process")

def status():
    with (ROOT / "status.json").open("rb") as stream:
        data = stream.read(32769)
    if len(data) > 32768:
        raise worker.Refusal("receipt-size")
    value = json.loads(data)
    if value.get("state") == "partial":
        if command(["/usr/bin/systemctl", "show", "--property=ActiveState", "--value", "bootwright-erase.service"]) != "active":
            value["state"] = "failed"
    return value

def serve():
    original = os.environ.get("SSH_ORIGINAL_COMMAND", "")
    if original == "receipt":
        sys.stdout.buffer.write(worker.canonical(status()))
    elif original == "ack":
        value = sys.stdin.buffer.read(65)
        receipt = json.loads((ROOT / "status.json").read_bytes())
        request = json.loads((ROOT / "request.json").read_bytes())
        worker.validate_receipt(request, receipt)
        if value.decode("ascii") != hashlib.sha256(worker.canonical(receipt)).hexdigest():
            raise worker.Refusal("ack")
        worker.atomic(ROOT / "ack.json", {"acknowledged": True})
    else:
        raise worker.Refusal("command")

def run():
    isolate()
    # This modifies only the verified RAM root. Password authentication stays
    # disabled; an unlocked shadow entry permits the one authorized public key.
    command(["/usr/sbin/usermod", "--password", "$6$bootwright$*", "root"])
    config = """Port 22
ListenAddress 0.0.0.0
ListenAddress ::
HostKey /run/bootwright-erase/host-key
AuthorizedKeysFile /run/bootwright-erase/authorized_keys
PidFile /run/bootwright-erase/sshd.pid
PasswordAuthentication no
KbdInteractiveAuthentication no
AuthenticationMethods publickey
PubkeyAuthentication yes
PermitEmptyPasswords no
HostbasedAuthentication no
GSSAPIAuthentication no
PermitRootLogin prohibit-password
UsePAM no
AllowUsers root
AllowTcpForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitTunnel no
PermitTTY no
ForceCommand /usr/bin/python3 /run/bootwright-erase/runtime.py serve
LogLevel QUIET
"""
    (ROOT / "sshd_config").write_text(config)
    command(["/usr/sbin/sshd", "-t", "-f", str(ROOT / "sshd_config")])
    command(["/usr/bin/systemctl", "start", "bootwright-erase-sshd.service"])
    if command(["/usr/bin/systemctl", "is-active", "bootwright-erase-sshd.service"]) != "active":
        raise worker.Refusal("receipt-server")
    request = json.loads((ROOT / "request.json").read_bytes())
    worker.execute(request, ROOT)
    # A successful write is not permission to disappear before its receipt is
    # durable on the controller. Cancellation leaves this RAM service running.
    while not (ROOT / "ack.json").exists():
        time.sleep(2)
    command(["/usr/bin/systemctl", "--no-block", "poweroff"])
    while True:
        time.sleep(60)

if __name__ == "__main__":
    try:
        serve() if len(sys.argv) == 2 and sys.argv[1] == "serve" else run()
    except BaseException:
        # No native values or exceptions enter the installer journal.
        sys.exit(1)
'''


def kickstart(network, request, private_key, authorized_key):
    files = {
        "physical_erase_worker.py": inspect.getsource(physical_erase_worker),
        "physical_erase_process.py": inspect.getsource(physical_erase_process),
        "physical_machine_identity.py": inspect.getsource(physical_machine_identity),
        "runtime.py": RUNTIME,
        "request.json": json.dumps(request),
        "host-key": private_key,
        "authorized_keys": "restrict " + authorized_key.strip() + "\n",
    }
    lines = [
        network.rstrip(),
        "%pre --erroronfail --interpreter=/bin/bash",
        "umask 077",
        "fail_closed() { trap '' HUP INT TERM; while :; do sleep 60; done; }",
        "trap fail_closed EXIT HUP INT TERM",
        "set -euo pipefail",
        "test ! -e /run/bootwright-erase",
        "mkdir -m 0700 /run/bootwright-erase",
    ]
    for name, data in files.items():
        encoded = base64.b64encode(data.encode()).decode()
        lines.append("base64 --decode > " + RAM + "/" + name + " <<'BOOTWRIGHT_ERASE_DATA'")
        lines.extend((encoded, "BOOTWRIGHT_ERASE_DATA"))
    lines.extend(
        (
            "cat > /run/systemd/system/bootwright-erase-sshd.service <<'BOOTWRIGHT_ERASE_SSH_UNIT'",
            "[Unit]",
            "Description=Bootwright RAM receipt transport",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/sbin/sshd -D -f /run/bootwright-erase/sshd_config",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_SSH_UNIT",
            "cat > /run/systemd/system/bootwright-erase.service <<'BOOTWRIGHT_ERASE_UNIT'",
            "[Unit]",
            "Description=Bootwright authorized offline logical overwrite",
            "[Service]",
            "Type=simple",
            "ExecStart=/usr/bin/python3 /run/bootwright-erase/runtime.py",
            "Restart=no",
            "UMask=0077",
            "StandardOutput=null",
            "StandardError=null",
            "BOOTWRIGHT_ERASE_UNIT",
            "systemctl daemon-reload",
            "systemctl start --no-block bootwright-erase.service",
            "fail_closed",
            "%end",
            "",
        )
    )
    return "\n".join(lines)
