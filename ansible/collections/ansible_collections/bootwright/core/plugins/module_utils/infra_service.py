"""Bounded host observation and readiness proof for one managed network service.

The observation is identical for every managed service; only the readiness
probe differs, because only the answer a daemon gives is kind-specific.
"""

from __future__ import annotations

import hashlib
import ipaddress
import os
import socket
import ssl
import struct
import time

from ansible_collections.bootwright.core.plugins.module_utils import host_sockets

SYSTEMCTL = "/usr/bin/systemctl"
PODMAN = "/usr/bin/podman"
UNIT_DIRECTORY = "/etc/containers/systemd/"

MAX_OUTPUT = 4096
MAX_STATUS = 128
MAX_HEADERS = 8192
PROBE_REASON = 120
PROBE_TIMEOUT = 5
DNS_A, DNS_AAAA = 1, 28
UNIT_STATES = ("active", "activating", "deactivating", "inactive", "failed")
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}
TRANSPORTS = {"DNSServer": ("tcp", "udp"), "NTPServer": ("udp",), "Proxy": ("tcp",), "Registry": ("tcp",)}


def probe_failure(error):
    """Why a readiness probe did not answer, bounded to one line.

    The exception class alone cannot tell a refused port from an address this
    host does not hold, and those need opposite repairs, so the system's own
    number and message are carried with it. A timeout is socket.timeout, which
    Python 3.10 made TimeoutError but the 3.9 floor still names timeout, so it
    is named TimeoutError on every interpreter.
    """
    name = "TimeoutError" if isinstance(error, socket.timeout) else type(error).__name__
    number = getattr(error, "errno", None)
    detail = getattr(error, "strerror", None) or str(error)
    if number is not None:
        return ("%s(%d): %s" % (name, number, detail))[:PROBE_REASON]
    if detail:
        return ("%s: %s" % (name, detail))[:PROBE_REASON]
    return name


def invoke(runner, argv):
    """Run one pinned executable with an exact argument vector and no shell."""
    if not argv or not os.path.isabs(argv[0]):
        raise ValueError("executable")
    code, out, _err = runner(argv, check_rc=False, environ_update=ENVIRONMENT)
    return code, out[:MAX_OUTPUT].strip()


def podman(runner, arguments):
    """Run podman, reading a host that has none as holding no container.

    Nothing installs podman on a Machine reached over SSH, where an apply
    refuses before it is needed, so a removal or an observation there finds no
    container instead of failing on the missing executable.
    """
    try:
        code, out, _err = runner(
            [PODMAN] + arguments, check_rc=False, environ_update=ENVIRONMENT, handle_exceptions=False
        )
    except FileNotFoundError:
        return 127, ""
    return code, out[:MAX_OUTPUT].strip()


def unit_state(runner, unit_path, service):
    """Report the unit's state; a unit with no definition is reported only while systemd still lists it failed, which a removal must take back."""
    _code, shown = invoke(runner, [SYSTEMCTL, "show", "--property=ActiveState", "--value", service])
    if not os.path.exists(unit_path):
        return "failed" if shown == "failed" else ""
    return shown if shown in UNIT_STATES else ""


def container_image(runner, name):
    """Report the exact image a running container was created from."""
    code, output = podman(runner, ["inspect", "--type", "container", "--format", "{{.ImageName}}", name])
    if code != 0 or not output:
        return ""
    return output.splitlines()[0].strip()


def container_present(runner, name):
    code, _output = podman(runner, ["container", "exists", name])
    return code == 0


def container_started(runner, name):
    """Report when the container last started, in nanoseconds, or None when that cannot be read."""
    code, output = podman(runner, ["inspect", "--type", "container", "--format", "{{.State.StartedAt.UnixNano}}", name])
    if code != 0 or not output:
        return None
    try:
        return int(output.splitlines()[0].strip())
    except ValueError:
        return None


def started_after(runner, name, paths):
    """Whether the container started no earlier than every file it runs from was last written.

    A daemon keeps what it read at its start, so a file published after that
    start is one the service does not run. A start or a file that cannot be
    read proves nothing, and the file is read as the apply's stat reads it,
    without following a link.
    """
    if not all(os.path.isabs(path) for path in paths):
        raise ValueError("path")
    started = container_started(runner, name)
    if started is None:
        return False
    for path in paths:
        try:
            modified = os.lstat(path).st_mtime_ns
        except OSError:
            return False
        if modified > started:
            return False
    return True


def observe(runner, request, runs_from=()):
    """Bounded read-only observation of everything this capability owns.

    `runs_from` names the files the daemon reads at its start besides its unit
    definition, which is always compared.
    """
    unit_path = UNIT_DIRECTORY + request["unit"] + ".container"
    service = request["unit"] + ".service"
    return {
        "unit": unit_state(runner, unit_path, service),
        "container": container_image(runner, request["unit"]),
        "containerPresent": container_present(runner, request["unit"]),
        "contentRoot": os.path.isdir(request["contentRoot"]),
        "startedAfterFiles": started_after(runner, request["unit"], [unit_path] + list(runs_from)),
    }


def foreign(request, observation, read=None):
    """Every socket another daemon holds where this service binds.

    A unit that is active, or a container that exists, may hold the socket
    itself: Quadlet runs its containers with --rm, so a present container is a
    running one. Only a service with neither proves the socket foreign.
    """
    if observation.get("unit") == "active" or observation.get("containerPresent"):
        return []
    return host_sockets.foreign_sockets(
        request["bindAddress"], [int(request["port"])], TRANSPORTS[request["kind"]],
        read or host_sockets.table_lines,
    )


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


def registry_head(stream):
    """The status line and headers of one response, bounded, without its body."""
    data = bytearray()
    while b"\r\n\r\n" not in data:
        if len(data) >= MAX_HEADERS:
            raise ValueError("response headers")
        part = stream.recv(MAX_HEADERS - len(data))
        if not part:
            raise ValueError("truncated response")
        data.extend(part)
    lines = bytes(data).split(b"\r\n\r\n", 1)[0].decode("latin-1").split("\r\n")
    headers = {}
    for line in lines[1:]:
        name, _separator, value = line.partition(":")
        headers[name.strip().lower()] = value.strip()
    return "".join(c for c in lines[0] if 0x20 <= ord(c) < 0x7F), headers


class Refused(ValueError):
    """A definite answer that repeating the probe would not change."""


CERTIFICATE_MISMATCH = "certificate-mismatch"


def probe_registry(address, port, token, expected):
    """Prove a registry answers its API over TLS to the bound client credential.

    The credential goes only to a listener presenting the certificate the
    operation bound: its SHA-256 is compared here, before anything is sent, and
    again in Go. Another certificate answers certificate-mismatch with the one
    presented, which Go refuses, and a probe given no expected certificate or
    no credential sends nothing. Only an authenticated 200 with the
    distribution API header proves the registry serves and accepts the
    credential; any other status is refused at once, never retried.
    """
    if not token or not expected:
        raise Refused("no bound certificate or credential to prove")
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    host = "[%s]" % address if ":" in address else address
    with socket.create_connection((address, port), timeout=PROBE_TIMEOUT) as raw:
        raw.settimeout(PROBE_TIMEOUT)
        with context.wrap_socket(raw) as secured:
            presented = secured.getpeercert(binary_form=True) or b""
            if not presented:
                raise Refused("no certificate")
            fingerprint = hashlib.sha256(presented).hexdigest()
            if fingerprint != expected:
                return CERTIFICATE_MISMATCH, fingerprint
            secured.sendall(("GET /v2/ HTTP/1.1\r\nHost: %s:%d\r\nAuthorization: Basic %s\r\nConnection: close\r\n\r\n"
                             % (host, port, token)).encode("ascii"))
            status, headers = registry_head(secured)
    parts = status.split(" ", 2)
    if len(parts) < 2 or not parts[0].startswith("HTTP/1."):
        raise ValueError("status line")
    if parts[1] != "200":
        raise Refused("status %s" % parts[1][:3])
    if headers.get("docker-distribution-api-version") != "registry/2.0":
        raise Refused("not a distribution API")
    return status, fingerprint


def dns_query(name, record_type=1):
    header = struct.pack(">HHHHHH", 0x4257, 0x0100, 1, 0, 0, 0)
    labels = b"".join(bytes([len(part)]) + part.encode("idna") for part in name.split("."))
    return header + labels + b"\x00" + struct.pack(">HH", record_type, 1)


def dns_answers(payload, response):
    """Count the answers a reply carries without decoding its record data."""
    if len(response) < 12 or response[:2] != payload[:2]:
        raise ValueError("response")
    return struct.unpack(">H", response[6:8])[0]


def skip_name(response, offset):
    """The offset after one name, whose labels may end in a compression pointer."""
    while True:
        if offset >= len(response):
            raise ValueError("response")
        length = response[offset]
        if length & 0xC0 == 0xC0:
            return offset + 2
        offset += 1 + length
        if length == 0:
            return offset


def dns_addresses(payload, response, record_type):
    """The addresses of the given type a reply carries, normalized."""
    dns_answers(payload, response)
    questions, answers = struct.unpack(">HH", response[4:8])
    offset = 12
    for index in range(questions):
        offset = skip_name(response, offset) + 4
    found = []
    for index in range(answers):
        offset = skip_name(response, offset)
        if offset + 10 > len(response):
            raise ValueError("response")
        kind, _klass, _ttl, length = struct.unpack(">HHIH", response[offset:offset + 10])
        data = response[offset + 10:offset + 10 + length]
        offset += 10 + length
        if len(data) != length:
            raise ValueError("response")
        if kind == record_type:
            family = socket.AF_INET6 if record_type == DNS_AAAA else socket.AF_INET
            found.append(ipaddress.ip_address(socket.inet_ntop(family, data)).compressed)
    return found


def dns_expected(records):
    """Every address each name must answer, by record type, merged across the records that name it."""
    expected = {}
    for record in records:
        for address in record["addresses"]:
            value = ipaddress.ip_address(address)
            kind = DNS_AAAA if value.version == 6 else DNS_A
            expected.setdefault((record["name"], kind), set()).add(value.compressed)
    return expected


def dns_exchange(address, port, payload):
    datagram = socket.socket(socket.AF_INET6 if ":" in address else socket.AF_INET, socket.SOCK_DGRAM)
    datagram.settimeout(PROBE_TIMEOUT)
    try:
        datagram.sendto(payload, (address, port))
        return datagram.recv(512)
    finally:
        datagram.close()


def dns_stream_read(stream, count, deadline):
    data = bytearray()
    while len(data) < count:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise socket.timeout("DNS TCP readiness deadline")
        stream.settimeout(remaining)
        part = stream.recv(count - len(data))
        if not part:
            raise ValueError("truncated DNS answer")
        data.extend(part)
    return bytes(data)


def dns_tcp_exchange(address, port, payload):
    deadline = time.monotonic() + PROBE_TIMEOUT
    with socket.create_connection((address, port), timeout=PROBE_TIMEOUT) as stream:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise socket.timeout("DNS TCP readiness deadline")
        stream.settimeout(remaining)
        stream.sendall(struct.pack(">H", len(payload)) + payload)
        length = struct.unpack(">H", dns_stream_read(stream, 2, deadline))[0]
        return dns_stream_read(stream, length, deadline)


def probe_dns(address, port, records):
    """Prove every declared address over both transports."""
    if not records:
        raise ValueError("records")
    expected = dns_expected(records)
    for exchange in (dns_exchange, dns_tcp_exchange):
        missing = []
        for (name, kind), declared in sorted(expected.items()):
            payload = dns_query(name, kind)
            answered = set(dns_addresses(payload, exchange(address, port, payload), kind))
            missing.extend("%s %s" % (name, value) for value in sorted(declared - answered))
        if missing:
            raise ValueError("unanswered " + ", ".join(missing))
    return "answers=%d" % sum(len(values) for values in expected.values())


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
