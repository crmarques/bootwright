"""Bounded host observation and readiness proof for one managed artifact server.

The unit, container and content root are observed exactly as every managed
service's are; only the listeners and their probe are the artifact server's.
"""

from __future__ import annotations

import hashlib
import os
import socket
import ssl

from ansible_collections.bootwright.core.plugins.module_utils import host_sockets, infra_service


def digest_file(path):
    if not os.path.isfile(path):
        return ""
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


def probe(target):
    """Prove one listener answers, and for HTTPS which certificate it presents.

    A refused or unanswered socket raises; the caller decides whether that is a
    failure or an unproved outcome.
    """
    address, port, protocol = target["address"], int(target["port"]), target["protocol"]
    with socket.create_connection((address, port), timeout=infra_service.PROBE_TIMEOUT) as raw:
        raw.settimeout(infra_service.PROBE_TIMEOUT)
        if protocol == "http":
            return {"address": address, "fingerprint": "", "name": target["name"], "port": port,
                    "protocol": protocol, "status": infra_service.read_status_line(raw, address)}
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        with context.wrap_socket(raw) as secured:
            presented = secured.getpeercert(binary_form=True) or b""
            fingerprint = hashlib.sha256(presented).hexdigest() if presented else ""
            return {"address": address, "fingerprint": fingerprint, "name": target["name"], "port": port,
                    "protocol": protocol, "status": infra_service.read_status_line(secured, address)}


def foreign(request, observation, read=None):
    """Every socket another daemon holds on a port this server binds.

    A unit that is active, or a container that exists, may hold the socket
    itself: Quadlet runs its containers with --rm, so a present container is a
    running one. Only a server with neither proves the socket foreign.
    """
    if observation.get("unit") == "active" or observation.get("containerPresent"):
        return []
    ports = sorted({int(listener["port"]) for listener in request["listeners"]})
    return host_sockets.foreign_sockets(request["bindAddress"], ports, ("tcp",), read or host_sockets.table_lines)


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
    runs_from = [request["contentRoot"] + "/config/nginx.conf"]
    if any(listener["protocol"] == "https" for listener in request["listeners"]):
        runs_from.extend(request["contentRoot"] + "/tls/" + name for name in ("server.crt", "server.key"))
    observation = infra_service.observe(runner, request, runs_from)
    observation["unitDigest"] = digest_file(infra_service.UNIT_DIRECTORY + request["unit"] + ".container")
    return observation
