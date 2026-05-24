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

import os
import socket
import struct
import sys


TYPE_A = 1
TYPE_CNAME = 5
CLASS_IN = 1


def _canonical_name(name):
    return name.rstrip(".").lower()


def _encode_name(hostname):
    qname = b""
    for label in hostname.rstrip(".").split("."):
        if not label:
            continue
        b = label.encode("idna")
        if len(b) > 63:
            raise ValueError("DNS label exceeds 63 octets")
        qname += bytes([len(b)]) + b
    return qname + b"\x00"


def _read_name(packet, offset):
    labels = []
    jumped = False
    next_offset = offset
    seen = set()
    while True:
        if offset >= len(packet):
            raise ValueError("short DNS name")
        length = packet[offset]
        if length & 0xC0 == 0xC0:
            if offset + 1 >= len(packet):
                raise ValueError("short DNS compression pointer")
            pointer = ((length & 0x3F) << 8) | packet[offset + 1]
            if pointer in seen:
                raise ValueError("cyclic DNS compression pointer")
            seen.add(pointer)
            if not jumped:
                next_offset = offset + 2
                jumped = True
            offset = pointer
            continue
        if length & 0xC0:
            raise ValueError("unsupported DNS label type")
        offset += 1
        if length == 0:
            if not jumped:
                next_offset = offset
            break
        if offset + length > len(packet):
            raise ValueError("short DNS label")
        labels.append(packet[offset : offset + length].decode("idna"))
        offset += length
    return ".".join(labels), next_offset


def _build_query(hostname):
    qid = struct.unpack(">H", os.urandom(2))[0]
    flags = 0x0100  # standard query, recursion desired
    header = struct.pack(">HHHHHH", qid, flags, 1, 0, 0, 0)
    question = _encode_name(hostname) + struct.pack(">HH", TYPE_A, CLASS_IN)
    return qid, header + question


def _exchange(server, packet, timeout):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.settimeout(timeout)
    try:
        sock.sendto(packet, (server, 53))
        response, _ = sock.recvfrom(4096)
    finally:
        sock.close()
    return response


def _parse_records(response, qid):
    if len(response) < 12:
        raise ValueError("short DNS response")
    response_id = struct.unpack(">H", response[:2])[0]
    if response_id != qid:
        raise ValueError("DNS response id mismatch")
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
    qdcount, ancount, nscount, arcount = struct.unpack(">HHHH", response[4:12])
    i = 12
    for _ in range(qdcount):
        _, i = _read_name(response, i)
        if i + 4 > len(response):
            raise ValueError("short DNS question")
        i += 4
    records = []
    for _ in range(ancount + nscount + arcount):
        name, i = _read_name(response, i)
        if i + 10 > len(response):
            raise ValueError("short DNS resource record")
        rtype, _rclass, _ttl, rdlength = struct.unpack(">HHIH", response[i : i + 10])
        i += 10
        rdata_start = i
        rdata_end = i + rdlength
        if rdata_end > len(response):
            raise ValueError("short DNS rdata")
        if rtype == TYPE_A and _rclass == CLASS_IN and rdlength == 4:
            records.append((_canonical_name(name), "A", ".".join(str(b) for b in response[i:rdata_end])))
        elif rtype == TYPE_CNAME and _rclass == CLASS_IN:
            target, _ = _read_name(response, rdata_start)
            records.append((_canonical_name(name), "CNAME", _canonical_name(target)))
        i += rdlength
    return records


def _dedupe(values):
    out = []
    seen = set()
    for value in values:
        if value in seen:
            continue
        seen.add(value)
        out.append(value)
    return out


def _resolve(server, hostname, timeout, seen):
    hostname = _canonical_name(hostname)
    if hostname in seen:
        return []
    seen.add(hostname)
    qid, packet = _build_query(hostname)
    response = _exchange(server, packet, timeout)
    records = _parse_records(response, qid)
    names = set([hostname])
    changed = True
    while changed:
        changed = False
        for name, rtype, value in records:
            if rtype == "CNAME" and name in names and value not in names:
                names.add(value)
                changed = True
    ips = [value for name, rtype, value in records if rtype == "A" and name in names]
    if ips:
        return _dedupe(ips)
    for name in sorted(names):
        if name == hostname:
            continue
        ips.extend(_resolve(server, name, timeout, seen))
    return ips


def query(server, hostname, timeout=3.0):
    return _dedupe(_resolve(server, hostname, timeout, set()))


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
