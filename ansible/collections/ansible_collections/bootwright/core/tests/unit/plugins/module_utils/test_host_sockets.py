"""A service refuses a socket another daemon holds where it binds, read from the kernel's tables.

Host reservations compare only Bootwright's contexts, so the apply reads PID 1's
socket tables before its first effect. The synthetic tables below keep the
kernel's layout as this test host's Linux 7.2 printed it on 2026-10-07: a
header, then one line per socket whose local address is its 32-bit words in
host byte order and its port in hexadecimal, listening TCP sockets first, with
a bound UDP socket in state 07 and a connected one in 01
(Documentation/networking/proc_net_tcp.rst; udp4_format_sock in
net/ipv4/udp.c). The planted cases bind a real socket and read this process's
own tables, so the parser meets what the kernel writes.
"""

from __future__ import annotations

import ipaddress
import socket
import sys

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import artifact_server, host_sockets, infra_service
from ansible_collections.bootwright.core.plugins.module_utils.host_sockets import foreign_sockets, overlaps, table_lines

HEADER = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n"
ENTRY = "%5d: %s %s:0000 %s 00000000:00000000 00:00000000 00000000     0        0 %d 2 0000000000000000 0\n"
LISTEN, ESTABLISHED, BOUND = "0A", "01", "07"


def table_word(address):
    packed = ipaddress.ip_address(address).packed
    return "".join("%08X" % int.from_bytes(packed[at:at + 4], sys.byteorder) for at in range(0, len(packed), 4))


def table_entry(index, address, port, state):
    remote = "0.0.0.0" if ipaddress.ip_address(address).version == 4 else "::"
    return ENTRY % (index, "%s:%04X" % (table_word(address), port), table_word(remote), state, 30000 + index)


def reader(**tables):
    """PID 1's tables holding these (address, port, state) entries; a table given as None is missing."""
    paths = {"/proc/1/net/" + name: entries for name, entries in tables.items()}

    def read(path):
        entries = paths.get(path, ())
        if entries is None:
            raise FileNotFoundError(path)
        return [HEADER] + [table_entry(index, *entry) for index, entry in enumerate(entries)]

    return read


def held(bind, tcp=(), tcp6=(), port=53):
    return foreign_sockets(bind, [port], ("tcp",), reader(tcp=tcp, tcp6=tcp6))


def test_a_listener_overlaps_by_address_family_wildcard_and_ipv4_mapped_form():
    for tcp, tcp6, address in (
        ([("192.0.2.1", 53, LISTEN)], [], "192.0.2.1"),
        ([("0.0.0.0", 53, LISTEN)], [], "0.0.0.0"),
        ([], [("::ffff:192.0.2.1", 53, LISTEN)], "::ffff:192.0.2.1"),
        ([], [("::", 53, LISTEN)], "::"),
    ):
        assert held("192.0.2.1", tcp, tcp6) == [{"transport": "tcp", "address": address, "port": 53}], (tcp, tcp6)
    assert held("192.0.2.1", [("192.0.2.2", 53, LISTEN)], [("::ffff:192.0.2.2", 53, LISTEN)]) == []
    assert held("fd00::1", [("192.0.2.1", 53, LISTEN), ("0.0.0.0", 53, LISTEN)], [("fd00::2", 53, LISTEN)]) == []
    assert held("fd00::1", [], [("fd00::1", 53, LISTEN)]) == [{"transport": "tcp", "address": "fd00::1", "port": 53}]


def test_a_wildcard_bind_overlaps_every_listener_on_its_port():
    found = held("0.0.0.0", [("192.0.2.7", 53, LISTEN)], [("::ffff:192.0.2.7", 53, LISTEN), ("fd00::1", 53, LISTEN)])
    assert [entry["address"] for entry in found] == ["192.0.2.7", "::ffff:192.0.2.7"]
    found = held("::", [("192.0.2.7", 53, LISTEN)], [("fd00::1", 53, LISTEN)])
    assert [entry["address"] for entry in found] == ["192.0.2.7", "fd00::1"]
    any4, any6 = ipaddress.ip_address("0.0.0.0"), ipaddress.ip_address("::")
    assert overlaps(any4, ipaddress.ip_address("192.0.2.7"), 8)
    assert overlaps(any6, ipaddress.ip_address("fd00::1"), 32)
    assert not overlaps(any4, ipaddress.ip_address("fd00::1"), 32)


def test_another_port_or_a_connected_udp_socket_is_not_foreign():
    assert held("192.0.2.1", [("192.0.2.1", 5353, LISTEN)]) == []
    connected = reader(udp=[("192.0.2.1", 53, ESTABLISHED)], udp6=[])
    assert foreign_sockets("192.0.2.1", [53], ("udp",), connected) == []
    bound = reader(udp=[("192.0.2.9", 123, ESTABLISHED), ("192.0.2.1", 53, BOUND)], udp6=[])
    assert foreign_sockets("192.0.2.1", [53], ("udp",), bound) == [{"transport": "udp", "address": "192.0.2.1", "port": 53}]
    after_listeners = reader(tcp=[("192.0.2.9", 80, ESTABLISHED), ("192.0.2.1", 53, LISTEN)], tcp6=[])
    assert foreign_sockets("192.0.2.1", [53], ("tcp",), after_listeners) == []


def test_an_unreadable_table_fails_closed():
    with pytest.raises(FileNotFoundError):
        foreign_sockets("192.0.2.1", [53], ("udp",), reader(udp=None, udp6=[]))
    assert foreign_sockets("192.0.2.1", [53], ("udp",), reader(udp=[("192.0.2.1", 53, BOUND)], udp6=None)) != []
    for lines in (
        ["garbage\n"],
        [HEADER, "   0: 0100000G:0035 00000000:0000 0A\n"],
        [HEADER, "   0: 01000000\n"],
        [HEADER] + [table_entry(0, "192.0.2.9", 80, LISTEN)] * (host_sockets.MAX_TABLE // len(HEADER)),
    ):
        with pytest.raises(ValueError):
            foreign_sockets("192.0.2.1", [53], ("tcp",), lambda path, lines=lines: lines if path.endswith("/tcp") else [HEADER])


def own_tables(path):
    return table_lines(path.replace("/proc/1/", "/proc/self/"))


def planted(transport):
    """A real socket on 127.0.0.1 this process holds, listening for TCP and bound for UDP."""
    kind = socket.SOCK_STREAM if transport == "tcp" else socket.SOCK_DGRAM
    sock = socket.socket(socket.AF_INET, kind)
    sock.bind(("127.0.0.1", 0))
    if transport == "tcp":
        sock.listen(1)
    return sock


IDLE = {"unit": "", "containerPresent": False}


def managed(kind, port):
    return {"kind": kind, "bindAddress": "127.0.0.1", "port": port}


def artifact(ports):
    return {"bindAddress": "127.0.0.1", "listeners": [{"name": "l%d" % at, "port": port} for at, port in enumerate(ports)]}


@pytest.mark.parametrize("kind, transport", [
    ("DNSServer", "udp"), ("DNSServer", "tcp"), ("NTPServer", "udp"), ("Proxy", "tcp"), ("ArtifactServer", "tcp"),
])
def test_a_planted_listener_is_foreign_to_each_kind_that_binds_it(kind, transport):
    sock, spare = planted(transport), socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    spare.bind(("127.0.0.1", 0))
    try:
        port, other = sock.getsockname()[1], spare.getsockname()[1]
        expected = [{"transport": transport, "address": "127.0.0.1", "port": port}]
        if kind == "ArtifactServer":
            assert artifact_server.foreign(artifact([other, port]), IDLE, own_tables) == expected
            assert artifact_server.foreign(artifact([other]), IDLE, own_tables) == []
            return
        assert infra_service.foreign(managed(kind, port), IDLE, own_tables) == expected
        assert infra_service.foreign(managed(kind, other), IDLE, own_tables) == []
    finally:
        sock.close()
        spare.close()


@pytest.mark.parametrize("observation", [
    {"unit": "active", "containerPresent": False}, {"unit": "failed", "containerPresent": True},
], ids=["active unit", "present container"])
def test_a_socket_the_service_may_hold_itself_is_never_read_as_foreign(observation):
    sock = planted("tcp")
    try:
        port = sock.getsockname()[1]
        assert infra_service.foreign(managed("Proxy", port), observation, own_tables) == []
        assert artifact_server.foreign(artifact([port]), observation, own_tables) == []
    finally:
        sock.close()
