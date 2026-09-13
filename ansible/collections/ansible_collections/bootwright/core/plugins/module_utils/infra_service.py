"""Bounded host observation and readiness proof for one managed network service.

The observation is identical for every managed service; only the readiness
probe differs, because only the answer a daemon gives is kind-specific.
"""

from __future__ import annotations

import os
import socket
import struct

SYSTEMCTL = "/usr/bin/systemctl"
PODMAN = "/usr/bin/podman"

MAX_OUTPUT = 4096
MAX_STATUS = 128
PROBE_TIMEOUT = 5
UNIT_STATES = ("active", "activating", "deactivating", "inactive", "failed")
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}


def invoke(runner, argv):
    """Run one pinned executable with an exact argument vector and no shell."""
    if not argv or not os.path.isabs(argv[0]):
        raise ValueError("executable")
    code, out, _err = runner(argv, check_rc=False, environ_update=ENVIRONMENT)
    return code, out[:MAX_OUTPUT].strip()


def unit_state(runner, unit_path, service):
    """Report the unit's state, or the empty string when it is not defined."""
    if not os.path.exists(unit_path):
        return ""
    _code, shown = invoke(runner, [SYSTEMCTL, "show", "--property=ActiveState", "--value", service])
    return shown if shown in UNIT_STATES else ""


def container_image(runner, name):
    """Report the exact image a running container was created from."""
    code, output = invoke(runner, [PODMAN, "inspect", "--type", "container", "--format", "{{.ImageName}}", name])
    if code != 0 or not output:
        return ""
    return output.splitlines()[0].strip()


def container_present(runner, name):
    code, _output = invoke(runner, [PODMAN, "container", "exists", name])
    return code == 0


def observe(runner, request):
    """Bounded read-only observation of everything this capability owns."""
    unit_path = "/etc/containers/systemd/" + request["unit"] + ".container"
    service = request["unit"] + ".service"
    return {
        "unit": unit_state(runner, unit_path, service),
        "container": container_image(runner, request["unit"]),
        "containerPresent": container_present(runner, request["unit"]),
        "contentRoot": os.path.isdir(request["contentRoot"]),
    }


def probe_targets(request):
    """Every address readiness must answer on, matching the Go contract."""
    if request["bindAddress"] not in ("0.0.0.0", "::"):
        return [request["bindAddress"]]
    return sorted({endpoint["address"] for endpoint in request.get("endpoints", [])})


def probe_http(address, port):
    """Prove a proxy answers: any well-formed status line is a live listener."""
    with socket.create_connection((address, port), timeout=PROBE_TIMEOUT) as raw:
        raw.settimeout(PROBE_TIMEOUT)
        raw.sendall(b"GET / HTTP/1.0\r\nHost: " + address.encode("idna") + b"\r\n\r\n")
        data = bytearray()
        while len(data) < MAX_STATUS and b"\n" not in data:
            part = raw.recv(1)
            if not part:
                break
            data.extend(part)
    line = bytes(data).split(b"\r\n")[0].decode("ascii", "replace")
    line = "".join(c for c in line if 0x20 <= ord(c) < 0x7F)
    if not line.startswith("HTTP/1."):
        raise ValueError("status line")
    return line


def dns_query(name):
    header = struct.pack(">HHHHHH", 0x4257, 0x0100, 1, 0, 0, 0)
    labels = b"".join(bytes([len(part)]) + part.encode("idna") for part in name.split("."))
    return header + labels + b"\x00" + struct.pack(">HH", 1, 1)


def dns_answers(payload, response):
    """Count the answers a reply carries without decoding its record data."""
    if len(response) < 12 or response[:2] != payload[:2]:
        raise ValueError("response")
    return struct.unpack(">H", response[6:8])[0]


def probe_dns(address, port, name):
    """Prove a resolver answers its own record over both transports."""
    payload = dns_query(name)
    datagram = socket.socket(socket.AF_INET6 if ":" in address else socket.AF_INET, socket.SOCK_DGRAM)
    datagram.settimeout(PROBE_TIMEOUT)
    try:
        datagram.sendto(payload, (address, port))
        answers = dns_answers(payload, datagram.recv(512))
    finally:
        datagram.close()
    with socket.create_connection((address, port), timeout=PROBE_TIMEOUT) as stream:
        stream.settimeout(PROBE_TIMEOUT)
        stream.sendall(struct.pack(">H", len(payload)) + payload)
        length = struct.unpack(">H", stream.recv(2))[0]
        streamed = dns_answers(payload, stream.recv(min(length, 512)))
    if answers < 1 or streamed < 1:
        raise ValueError("answers")
    return "answers=%d" % answers


def probe_ntp(address, port):
    """Prove a time service replies; an unsynchronized stratum is not failure."""
    request = bytearray(48)
    request[0] = 0x1B
    client = socket.socket(socket.AF_INET6 if ":" in address else socket.AF_INET, socket.SOCK_DGRAM)
    client.settimeout(PROBE_TIMEOUT)
    try:
        client.sendto(bytes(request), (address, port))
        response = client.recv(48)
    finally:
        client.close()
    if len(response) < 48 or (response[0] & 0x7) != 4:
        raise ValueError("mode")
    return "stratum=%d" % response[1]
