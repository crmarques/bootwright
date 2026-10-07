"""Bounded read of the host's socket tables: what already holds a port a service binds.

A host reservation compares only the contexts Bootwright coordinates, so a
socket another daemon holds is proved from the kernel's own tables before the
first effect. The tables are PID 1's, because every managed unit runs with host
networking whatever namespace this module runs in. Each local address is its
32-bit words in host byte order, then its port in hexadecimal; the TCP tables
list every listening socket before any other entry
(Documentation/networking/proc_net_tcp.rst in the Linux tree,
https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/
Documentation/networking/proc_net_tcp.rst?h=v6.18), and a UDP socket bound and
not connected is state 7, TCP_CLOSE (include/net/tcp_states.h), wherever it
sits in its table.
"""

from __future__ import annotations

import ipaddress
import sys

TABLES = {
    "tcp": (("/proc/1/net/tcp", 8), ("/proc/1/net/tcp6", 32)),
    "udp": (("/proc/1/net/udp", 8), ("/proc/1/net/udp6", 32)),
}
TCP_LISTEN = 0x0A
UDP_BOUND = 0x07
MAX_TABLE = 1 << 20
MAX_FOREIGN = 16
HEX_DIGITS = frozenset("0123456789ABCDEFabcdef")
ANY_IPV4 = ipaddress.IPv4Address("0.0.0.0")
ANY_IPV6 = ipaddress.IPv6Address("::")


def table_lines(path):
    """Yield one kernel socket table's text lines, opened read-only as ASCII."""
    with open(path, "r", encoding="ascii") as table:
        yield from table


def hexadecimal(text, digits):
    if len(text) != digits or set(text) - HEX_DIGITS:
        raise ValueError("socket table field")
    return text


def table_address(text, digits):
    """Decode one local address: the host's own words in order, then the port."""
    host, separator, port = text.partition(":")
    if not separator:
        raise ValueError("socket table address")
    host = hexadecimal(host, digits)
    packed = b"".join(int(host[at:at + 8], 16).to_bytes(4, sys.byteorder) for at in range(0, digits, 8))
    return ipaddress.ip_address(packed), int(hexadecimal(port, 4), 16)


def table_sockets(lines, digits, transport):
    """Every listening TCP or bound UDP local address of one table.

    The bytes read are bounded, and a table without its header, a line that is
    not an entry and a table past the bound all raise, because a table that
    was not read in full proves nothing is listening.
    """
    wanted = TCP_LISTEN if transport == "tcp" else UDP_BOUND
    lines = iter(lines)
    header = next(lines, "")
    consumed = len(header)
    if header.split()[:1] != ["sl"]:
        raise ValueError("socket table header")
    found = []
    for line in lines:
        consumed += len(line)
        if consumed > MAX_TABLE:
            raise ValueError("socket table bound")
        fields = line.split()
        if len(fields) < 4 or not fields[0].endswith(":"):
            raise ValueError("socket table entry")
        local = table_address(fields[1], digits)
        state = int(hexadecimal(fields[3], 2), 16)
        if state == wanted:
            found.append(local)
        elif transport == "tcp":
            break
    return found


def overlaps(bind, local, digits):
    """Whether a socket on `local`, in the table of that width, collides with `bind`.

    The IPv6 wildcard accepts IPv4 as well unless the socket was made
    IPv6-only, so either wildcard side collides with every address of its
    reach; an IPv4 address collides with itself, the IPv4 wildcard and its
    IPv4-mapped form; an IPv6 address only with itself.
    """
    if bind == ANY_IPV6 or local == ANY_IPV6:
        return True
    if bind == ANY_IPV4:
        return digits == 8 or local.ipv4_mapped is not None
    if bind.version == 4:
        if digits == 8:
            return local in (bind, ANY_IPV4)
        return local.ipv4_mapped == bind
    return digits == 32 and local == bind


def foreign_sockets(bind_address, ports, transports, read=table_lines):
    """Every socket on one of `ports` whose address overlaps `bind_address`.

    `read(path)` returns an iterable of a table's text lines. A missing IPv6
    table means the host has no IPv6 stack; a missing IPv4 table, or any table
    that cannot be parsed, raises, so the check refuses rather than reading the
    port free.
    """
    bind = ipaddress.ip_address(str(bind_address))
    ports = {int(port) for port in ports}
    found = set()
    for transport in transports:
        for path, digits in TABLES[transport]:
            try:
                sockets = table_sockets(read(path), digits, transport)
            except FileNotFoundError:
                if digits == 8:
                    raise
                continue
            for local, port in sockets:
                if port in ports and overlaps(bind, local, digits):
                    found.add((transport, str(local), port))
    return [
        {"transport": transport, "address": address, "port": port}
        for transport, address, port in sorted(found)[:MAX_FOREIGN]
    ]
