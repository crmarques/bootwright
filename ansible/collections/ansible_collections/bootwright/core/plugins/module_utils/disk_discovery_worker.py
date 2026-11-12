"""The dedicated RAM worker: bounded inventory, static networking and receipt.

This source is bundled with its pure helpers into the private discovery image.
It never starts the installer, invokes storage utilities or mounts a disk.
"""

from __future__ import annotations

import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import hmac
import http.server
import ssl
import time

if __package__:
    from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline
    from ansible_collections.bootwright.core.plugins.module_utils import bounded_process as process
    from ansible_collections.bootwright.core.plugins.module_utils import canonical_json
    from ansible_collections.bootwright.core.plugins.module_utils import disk_inventory
    from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity as identity
else:
    sys.path.insert(0, "/run/discovery")
    import absolute_deadline
    import bounded_process as process
    import canonical_json
    import disk_inventory
    import physical_machine_identity as identity

ROOT = Path("/run/discovery")
ENV = {"PATH": "/usr/bin:/usr/sbin:/bin:/sbin", "LC_ALL": "C", "PYTHONDONTWRITEBYTECODE": "1"}
MAX_REPORT = 48 << 10
NAME = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}\Z")


def command(argv):
    result = process.run(argv, timeout=20, limit=8192, environment=ENV, capture_stderr=False)
    if result.code:
        raise ValueError("discovery runtime command refused")
    return result.stdout


def name(value):
    if not isinstance(value, str) or NAME.fullmatch(value) is None:
        raise ValueError("discovery interface name")
    return value


def network_commands(network, interfaces, observed):
    """Translate only the frozen IPv4 ethernet/bond/VLAN install topology."""
    required = {"address", "prefix", "interface", "type"}
    optional = {"gateway", "vlanBase", "vlanID", "bondPorts", "bondOptions"}
    if not isinstance(network, dict) or not required <= set(network) or set(network) - required - optional:
        raise ValueError("discovery network shape")
    address = ipaddress.IPv4Address(network["address"])
    if not isinstance(network["prefix"], int) or isinstance(network["prefix"], bool) or not 1 <= network["prefix"] <= 32:
        raise ValueError("discovery IPv4 prefix")
    interface, kind = name(network["interface"]), network["type"]
    if kind not in ("ethernet", "bond", "vlan") or not 1 <= len(interfaces) <= 32:
        raise ValueError("discovery network type")
    argv, renamed = [], {}
    for index, nic in enumerate(interfaces):
        desired, mac = name(nic["name"]), nic["macAddress"].lower()
        actual = name(observed[mac])
        if desired in renamed or len(set(observed.values())) != len(observed):
            raise ValueError("discovery network is ambiguous")
        temporary = "bwdisco" + str(index)
        argv.extend((["link", "set", "dev", actual, "down"], ["link", "set", "dev", actual, "name", temporary]))
        renamed[desired] = temporary
    argv.extend(["link", "set", "dev", temporary, "name", desired] for desired, temporary in renamed.items())
    bond = network.get("vlanBase", "") if kind == "vlan" else interface
    ports = network.get("bondPorts", [])
    if kind == "bond" or ports:
        argv.extend(bond_commands(name(bond), ports, network.get("bondOptions", []), renamed))
    if kind == "vlan":
        base, number = name(network["vlanBase"]), network["vlanID"]
        if not isinstance(number, int) or isinstance(number, bool) or not 1 <= number <= 4094 or (not ports and base not in renamed):
            raise ValueError("discovery VLAN")
        argv.extend((["link", "set", "dev", base, "up"], ["link", "add", "link", base, "name", interface, "type", "vlan", "id", str(number)]))
    elif kind == "ethernet" and interface not in renamed:
        raise ValueError("discovery ethernet identity")
    argv.extend((["link", "set", "dev", interface, "up"], ["address", "add", str(address) + "/" + str(network["prefix"]), "dev", interface]))
    if network.get("gateway"):
        gateway = str(ipaddress.IPv4Address(network["gateway"]))
        argv.append(["route", "add", "default", "via", gateway, "dev", interface])
    return [["/usr/sbin/ip", *args] for args in argv]


def bond_commands(bond, ports, options, physical):
    if not isinstance(ports, list) or not 1 <= len(ports) <= 32 or len(set(ports)) != len(ports) or any(name(port) not in physical for port in ports):
        raise ValueError("discovery bond ports")
    if not isinstance(options, list) or not 1 <= len(options) <= 32:
        raise ValueError("discovery bond options")
    settings = []
    for option in options:
        if not isinstance(option, str) or re.fullmatch(r"[a-z][a-z0-9_]{0,31}=[A-Za-z0-9_.:-]{1,64}", option) is None:
            raise ValueError("discovery bond option")
        key, value = option.split("=", 1)
        settings.extend((key, value))
    if settings[0] != "mode":
        raise ValueError("discovery bond mode")
    commands = [["link", "add", "name", bond, "type", "bond", *settings]]
    for port in ports:
        commands.extend((["link", "set", "dev", port, "master", bond], ["link", "set", "dev", port, "up"]))
    return commands


def read_json(path, limit):
    with path.open("rb") as stream:
        data = stream.read(limit + 1)
    if len(data) > limit:
        raise ValueError("discovery record exceeds bound")
    return json.loads(data)


def write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        if os.write(fd, data) != len(data):
            raise ValueError("RAM record write")
        os.fsync(fd)
    finally:
        os.close(fd)


class Server(http.server.HTTPServer):
    """One bounded TLS connection at a time; no request is logged."""

    allow_reuse_address = False

    def get_request(self):
        connection, address = self.socket.accept()
        connection.settimeout(10)
        try:
            return self.tls.wrap_socket(connection, server_side=True), address
        except BaseException:
            connection.close()
            raise

    def handle_error(self, request, client_address):
        pass


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def authorized(self):
        return hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + self.server.token)

    def answer(self, code, body=b""):
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path != "/report" or not self.authorized():
            return self.answer(403)
        self.answer(200, self.server.report)

    def do_POST(self):
        if self.path != "/ack" or not self.authorized() or self.headers.get("Content-Length") != "64":
            return self.answer(403)
        data = self.rfile.read(64)
        if not hmac.compare_digest(data, hashlib.sha256(self.server.report).hexdigest().encode("ascii")):
            return self.answer(403)
        self.server.acknowledged = True
        self.answer(200)


def receipt_server(request, report):
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls.minimum_version = ssl.TLSVersion.TLSv1_2
    tls.load_cert_chain(str(ROOT / "server.pem"), str(ROOT / "server-key.pem"))
    with Server((request["network"]["address"], 9443), Handler) as server:
        server.tls, server.report, server.token = tls, report, request["reportToken"]
        server.acknowledged, server.timeout = False, 1
        deadline = time.monotonic() + 1800
        while not server.acknowledged:
            if time.monotonic() >= deadline:
                raise ValueError("discovery receipt not acknowledged")
            try:
                with absolute_deadline.limited(min(10, deadline - time.monotonic())):
                    server.handle_request()
            except TimeoutError:
                continue


def ram_boundary():
    """Only our custom PID1 and its RAM payload may run."""
    if Path("/proc/1/comm").read_text().strip() not in ("init", "sh", "bash"):
        raise ValueError("discovery PID1")
    if Path("/proc/1/cmdline").read_bytes().split(b"\0")[:2] not in ([b"/bin/sh", b"/init"], [b"/init"], [b"/bin/bash", b"/init"]):
        raise ValueError("discovery init identity")
    mounts = command(["/usr/bin/findmnt", "--json", "-o", "TARGET,FSTYPE,OPTIONS"]).decode("ascii")

    def inspect(entries):
        for mount in entries:
            if mount["fstype"] not in ("tmpfs", "devtmpfs", "proc", "sysfs", "rootfs"):
                raise ValueError("unexpected discovery filesystem")
            inspect(mount.get("children", []))

    inspect(json.loads(mounts)["filesystems"])
    if command(["/usr/bin/findmnt", "-n", "-o", "FSTYPE", "--target", "/run"]).strip() != b"tmpfs":
        raise ValueError("discovery RAM filesystem")


def run():
    ram_boundary()
    request = read_json(ROOT / "request.json", 32768)
    binding = request["binding"]
    identity.prove(binding["uuid"], binding["macs"])
    observed = {
        path.joinpath("address").read_text().strip().lower(): path.name for path in Path("/sys/class/net").iterdir() if path.joinpath("device").exists()
    }
    if request["network"].get("bondPorts"):
        command(["/usr/sbin/modprobe", "--config", "/dev/null", "--ignore-install", "bonding", "max_bonds=0"])
    if request["network"]["type"] == "vlan":
        command(["/usr/sbin/modprobe", "--config", "/dev/null", "--ignore-install", "8021q"])
    for argv in network_commands(request["network"], request["interfaces"], observed):
        command(argv)
    identity.prove(binding["uuid"], binding["macs"])
    report = canonical_json.canonical({"version": "machine-disk-inventory-v1", "binding": binding, "disks": disk_inventory.collect()})
    if len(report) > MAX_REPORT:
        raise ValueError("discovery inventory exceeds bound")
    write(ROOT / "report.json", report)
    receipt_server(request, report)
    identity.prove(binding["uuid"], binding["macs"])


if __name__ == "__main__":
    try:
        run()
    except BaseException:
        # No hardware values, keys or exception strings enter the boot console.
        raise SystemExit(1)
