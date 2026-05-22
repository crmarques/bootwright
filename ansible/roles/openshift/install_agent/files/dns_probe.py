#!/usr/bin/env python3
# Pure stdlib DNS A-record probe used by install_agent's node-side
# preflight. Replaces `dig +short` so the controller bastion only needs
# Python (already required by Ansible) — no bind-utils install.
#
# Usage:  dns_probe.py <server> <hostname>
# stdout: one IPv4 per line; empty if no A answer
# rc:     0 on a usable response (including NOERROR/empty and NXDOMAIN);
#         1 on transport failure (timeout, unreachable, malformed).
#
# Matching dig's convention: positive probes pass when the expected
# address appears in stdout; negative (no-wildcard) probes pass when
# stdout is empty AND rc == 0.

import socket
import struct
import sys


def query(server: str, hostname: str, timeout: float = 3.0) -> list[str]:
    qid = 0x4257  # arbitrary 16-bit id
    flags = 0x0100  # standard query, recursion desired
    header = struct.pack(">HHHHHH", qid, flags, 1, 0, 0, 0)
    qname = b""
    for label in hostname.rstrip(".").split("."):
        if not label:
            continue
        b = label.encode("idna")
        if len(b) > 63:
            return []
        qname += bytes([len(b)]) + b
    qname += b"\x00"
    question = qname + struct.pack(">HH", 1, 1)  # QTYPE=A, QCLASS=IN
    packet = header + question
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    try:
        sock.sendto(packet, (server, 53))
        response, _ = sock.recvfrom(4096)
    finally:
        sock.close()
    if len(response) < 12:
        raise ValueError("short DNS response")
    rcode = response[3] & 0x0F
    # NOERROR(0), NXDOMAIN(3), REFUSED(5) all mean "server understood
    # us; there is no A answer for this name." For a dnsmasq with
    # `no-resolv` (the bootwright default), unknown names get REFUSED
    # because there's no upstream — semantically the same as NXDOMAIN
    # for our preflight. Other rcodes (FORMERR, SERVFAIL, NOTIMP,
    # NOTAUTH, NOTZONE) are real protocol errors and stay loud.
    if rcode in (3, 5):
        return []
    if rcode != 0:
        raise ValueError(f"DNS rcode {rcode}")
    ancount = struct.unpack(">H", response[6:8])[0]
    if ancount == 0:
        return []
    i = 12
    # Skip question section (we sent exactly one).
    while i < len(response) and response[i] != 0:
        if response[i] & 0xC0 == 0xC0:
            i += 2
            break
        i += response[i] + 1
    else:
        i += 1  # null terminator
    i += 4  # QTYPE (2) + QCLASS (2)
    ips: list[str] = []
    for _ in range(ancount):
        if i >= len(response):
            break
        # Resource record NAME: either a 2-byte pointer or a label sequence.
        if response[i] & 0xC0 == 0xC0:
            i += 2
        else:
            while i < len(response) and response[i] != 0:
                i += response[i] + 1
            i += 1
        if i + 10 > len(response):
            break
        rtype, _rclass, _ttl, rdlength = struct.unpack(">HHIH", response[i : i + 10])
        i += 10
        if rtype == 1 and rdlength == 4:
            ips.append(".".join(str(b) for b in response[i : i + rdlength]))
        i += rdlength
    return ips


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: dns_probe.py <server> <hostname>", file=sys.stderr)
        return 2
    server, hostname = sys.argv[1], sys.argv[2]
    try:
        for ip in query(server, hostname):
            print(ip)
        return 0
    except (socket.timeout, socket.gaierror, OSError, ValueError) as err:
        print(f"{type(err).__name__}: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
