"""Read-only bootstrap resolver proof before an attached export mints keys."""

from __future__ import annotations

import ipaddress
import json
import re
import sys

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native

# libc/NSS can block independently of socket timeouts. The process owner bounds
# resolution and reaps it; the FQDN is input, never executable source or argv.
RESOLVE = """import json, socket, sys
try:
    name = sys.stdin.buffer.read(254).decode('ascii')
    rows = socket.getaddrinfo(name, 443, type=socket.SOCK_STREAM)
    addresses = sorted(set(row[4][0] for row in rows))
    if not 0 < len(addresses) <= 64:
        raise ValueError()
    sys.stdout.buffer.write(json.dumps(addresses).encode())
except Exception:
    sys.exit(1)
"""


def prove_gateway(fqdn, addresses, runner=ceph_native.bounded_command):
    """Require the exact declared terminating set; never claim pod reachability."""
    ceph_native.require(isinstance(fqdn, str) and 0 < len(fqdn) <= 253
                        and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", part)
                                for part in fqdn.split(".")), "gateway-dns-request")
    reason = "gateway-dns"
    try:
        ceph_native.require(isinstance(addresses, list) and 0 < len(addresses) <= 64, reason)
        expected = {str(ipaddress.ip_address(value)) for value in addresses}
        code, output = runner([sys.executable, "-I", "-B", "-c", RESOLVE],
                              data=fqdn.encode("ascii"), timeout=15, limit=8192)
        ceph_native.require(code == 0 and isinstance(output, bytes) and len(output) <= 8192, reason)
        answer = json.loads(output)
        ceph_native.require(isinstance(answer, list) and 0 < len(answer) <= 64, reason)
        ceph_native.require(all(isinstance(value, str) for value in answer), reason)
        actual = {str(ipaddress.ip_address(value)) for value in answer}
        ceph_native.require(actual == expected, reason)
    except (ValueError, TypeError, OSError):
        raise ceph_native.Refusal(reason) from None
