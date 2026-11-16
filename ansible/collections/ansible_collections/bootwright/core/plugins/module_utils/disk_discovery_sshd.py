"""The RAM-only SSH endpoint permits one authenticated inventory command."""

from __future__ import annotations

import ipaddress
import os
from pathlib import Path
import sys

if __package__:
    from ansible_collections.bootwright.core.plugins.module_utils import bounded_process as process
else:
    import bounded_process as process

ROOT = Path("/run/discovery")
MAX_REPORT = 48 << 10
REFUSED = 42
ENV = {"PATH": "/usr/bin:/usr/sbin:/bin:/sbin", "LC_ALL": "C", "PYTHONDONTWRITEBYTECODE": "1"}


def configuration(address, port):
    address = str(ipaddress.IPv4Address(address))
    if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
        raise ValueError("discovery SSH port")
    return ("Port " + str(port) + "\nListenAddress " + address + "\n" + """AddressFamily inet
Include /run/discovery/ssh-policy
HostKey /run/discovery/host-key
AuthorizedKeysFile /run/discovery/authorized_keys
PidFile /run/discovery/sshd.pid
AuthenticationMethods publickey
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
HostbasedAuthentication no
GSSAPIAuthentication no
KerberosAuthentication no
PermitRootLogin prohibit-password
UsePAM no
AllowUsers root
StrictModes yes
DisableForwarding yes
AllowTcpForwarding no
AllowStreamLocalForwarding no
AllowAgentForwarding no
X11Forwarding no
PermitTunnel no
PermitTTY no
PermitUserRC no
PermitUserEnvironment no
AuthorizedKeysCommand none
AuthorizedPrincipalsCommand none
TrustedUserCAKeys none
UseDNS no
PrintMotd no
PrintLastLog no
LoginGraceTime 10
MaxAuthTries 2
MaxSessions 1
MaxStartups 4:30:8
ForceCommand /usr/bin/python3 -I /run/discovery/disk_discovery_worker.py report
LogLevel QUIET
""").encode("ascii")


def serve(request, write, phase):
    phase("validating restricted inventory SSH runtime")
    write(ROOT / "sshd_config", configuration(request["network"]["address"], request["ssh"]["port"]))
    argv = ["/usr/sbin/sshd", "-f", str(ROOT / "sshd_config")]
    result = process.run(argv + ["-t"], timeout=20, limit=8192, environment=ENV, capture_stderr=False)
    if result.code:
        raise ValueError("discovery SSH server configuration")
    phase("starting restricted inventory SSH on port " + str(request["ssh"]["port"]))
    process.run(argv + ["-D", "-e"], timeout=request["deadlineSeconds"] + 300, limit=8192, environment=ENV, capture_stderr=False)
    raise ValueError("discovery SSH server stopped")


def report():
    if os.environ.get("SSH_ORIGINAL_COMMAND") != "inventory":
        raise SystemExit(64)
    if (ROOT / "refused").exists():
        raise SystemExit(REFUSED)
    with (ROOT / "report.json").open("rb") as stream:
        value = stream.read(MAX_REPORT + 1)
    if not 0 < len(value) <= MAX_REPORT:
        raise SystemExit(1)
    sys.stdout.buffer.write(value)
