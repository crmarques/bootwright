"""Bounded host observation and readiness proof for one managed artifact server."""

from __future__ import annotations

import hashlib
import os
import socket
import ssl

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


def digest_file(path):
    if not os.path.isfile(path):
        return ""
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


def status_line(sock, host):
    request = b"GET / HTTP/1.0\r\nHost: " + host.encode("idna") + b"\r\nConnection: close\r\n\r\n"
    sock.sendall(request)
    data = bytearray()
    while len(data) < MAX_STATUS and b"\n" not in data:
        part = sock.recv(1)
        if not part:
            break
        data.extend(part)
    line = bytes(data).split(b"\r\n")[0].decode("ascii", "replace")
    return "".join(c for c in line if 0x20 <= ord(c) < 0x7F)


def probe(target):
    """Prove one listener answers, and for HTTPS which certificate it presents.

    A refused or unanswered socket raises; the caller decides whether that is a
    failure or an unproved outcome.
    """
    address, port, protocol = target["address"], int(target["port"]), target["protocol"]
    with socket.create_connection((address, port), timeout=PROBE_TIMEOUT) as raw:
        raw.settimeout(PROBE_TIMEOUT)
        if protocol == "http":
            return {"address": address, "fingerprint": "", "name": target["name"], "port": port,
                    "protocol": protocol, "status": status_line(raw, address)}
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        with context.wrap_socket(raw) as secured:
            presented = secured.getpeercert(binary_form=True) or b""
            fingerprint = hashlib.sha256(presented).hexdigest() if presented else ""
            return {"address": address, "fingerprint": fingerprint, "name": target["name"], "port": port,
                    "protocol": protocol, "status": status_line(secured, address)}


def probe_targets(request):
    """Every socket readiness must answer on, matching the Go contract exactly."""
    wildcard = request["bindAddress"] in ("0.0.0.0", "::")
    targets = []
    for listener in request["listeners"]:
        addresses = [request["bindAddress"]]
        if wildcard:
            addresses = sorted({
                endpoint["address"]
                for endpoint in request["endpoints"]
                if endpoint["listener"] == listener["name"]
            })
        for address in addresses:
            targets.append({
                "address": address, "name": listener["name"],
                "port": listener["port"], "protocol": listener["protocol"],
            })
    return sorted(targets, key=lambda item: (item["name"], item["address"]))


def observe(runner, request):
    """Bounded read-only observation of everything this capability owns."""
    unit_path = "/etc/containers/systemd/" + request["unit"] + ".container"
    service = request["unit"] + ".service"
    return {
        "unit": unit_state(runner, unit_path, service),
        "unitDigest": digest_file(unit_path),
        "container": container_image(runner, request["unit"]),
        "containerPresent": container_present(runner, request["unit"]),
        "contentRoot": os.path.isdir(request["contentRoot"]),
    }
