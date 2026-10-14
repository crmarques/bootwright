"""A read-only address dump proves explicit binds only after complete kernel evidence."""

from __future__ import annotations

import ipaddress
import socket
import struct

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import host_addresses as addresses


def attribute(kind, payload):
    length = 4 + len(payload)
    return struct.pack("=HH", length, kind) + payload + b"\0" * ((-length) & 3)


def address_message(local="192.0.2.1", peer=None, flags=0, extended=None):
    address = ipaddress.ip_address(local)
    family = socket.AF_INET if address.version == 4 else socket.AF_INET6
    payload = struct.pack("=BBBBI", family, 24 if address.version == 4 else 64, flags, 0, 2)
    payload += attribute(1, ipaddress.ip_address(peer or local).packed)
    if peer is not None or address.version == 4:
        payload += attribute(2, address.packed)
    if extended is not None:
        payload += attribute(8, struct.pack("=I", extended))
    return message(20, payload)


def message(kind, payload=b"", flags=2, sequence=1):
    length = 16 + len(payload)
    return struct.pack("=IHHII", length, kind, flags, sequence, 0) + payload + b"\0" * ((-length) & 3)


DONE = message(3, struct.pack("=i", 0))


class Channel:
    def __init__(self, datagrams):
        self.datagrams, self.closed, self.sent = iter(datagrams), False, []

    def __enter__(self):
        return self

    def __exit__(self, *_arguments):
        self.closed = True

    def sendto(self, payload, destination):
        assert destination == (0, 0)
        assert payload == struct.pack("=IHHII", 24, 22, 0x301, 1, 0) + struct.pack("=BBBBI", 0, 0, 0, 0, 0)
        self.sent.append(payload)

    def settimeout(self, timeout):
        assert 0 < timeout <= 5

    def recvmsg(self, limit):
        assert limit == 65536
        datagram = next(self.datagrams, None)
        if datagram is None:
            raise socket.timeout("incomplete dump")
        return datagram if isinstance(datagram, tuple) else (datagram, [], 0, (0, 0))


def query(monkeypatch, datagrams):
    channel = Channel(datagrams)

    def opened(family, kind, protocol):
        assert (family, kind, protocol) == (socket.AF_NETLINK, socket.SOCK_RAW, 0)
        return channel

    monkeypatch.setattr(addresses.socket, "socket", opened)
    monkeypatch.setattr(addresses.os, "readlink", lambda _path: "net:[1]")
    return channel


@pytest.mark.parametrize("bind, present", [
    ("192.0.2.1", True), ("192.0.2.2", False), ("192.0.2.3", False),
    ("2001:db8::1", True), ("2001:0db8:0:0:0:0:0:1", True),
    ("::ffff:192.0.2.1", True),
])
def test_only_usable_local_addresses_prove_an_explicit_bind(monkeypatch, bind, present):
    channel = query(monkeypatch, [
        address_message("192.0.2.1", peer="192.0.2.2"),
        address_message("2001:db8::1") + DONE,
    ])
    assert addresses.bind_address_assigned(bind) is present
    assert len(channel.sent) == 1 and channel.closed


@pytest.mark.parametrize("flags, extended, present", [
    (0x40, None, False), (0x08, None, False), (0, 0x40, False), (0, 0x08, False),
    (0x40, 0x80, True), (0x20, None, True),
])
def test_tentative_and_failed_addresses_are_unusable_and_extended_flags_take_precedence(monkeypatch, flags, extended, present):
    query(monkeypatch, [address_message("2001:db8::1", flags=flags, extended=extended) + DONE])
    assert addresses.bind_address_assigned("2001:db8::1") is present


@pytest.mark.parametrize("bind", ["0.0.0.0", "::", "0:0:0:0:0:0:0:0"])
def test_a_wildcard_opens_no_address_query(monkeypatch, bind):
    monkeypatch.setattr(addresses, "assigned_addresses", lambda: pytest.fail("wildcard queried the host"))
    assert addresses.bind_address_assigned(bind) is True


@pytest.mark.parametrize("tail", [
    [], [message(3, flags=0x12)], [message(3, struct.pack("=i", -1))],
    [message(2, struct.pack("=i", -1))], [message(4)], [message(3, sequence=2)],
    [(DONE, [], socket.MSG_TRUNC, (0, 0))], [(DONE, [], 0, (42, 0))],
    [b""], [b"bad"], [DONE + address_message()],
])
def test_an_incomplete_failed_or_foreign_dump_proves_no_presence_or_absence(monkeypatch, tail):
    channel = query(monkeypatch, [address_message()] + tail)
    with pytest.raises((OSError, ValueError)):
        addresses.bind_address_assigned("192.0.2.1")
    assert channel.closed


def test_a_dump_past_the_bound_proves_nothing(monkeypatch):
    channel = query(monkeypatch, [address_message(), DONE])
    monkeypatch.setattr(addresses, "MAX_DUMP", 16)
    with pytest.raises(ValueError):
        addresses.bind_address_assigned("192.0.2.1")
    assert channel.closed


@pytest.mark.parametrize("payload", [
    b"", struct.pack("=BBBBI", 2, 24, 0, 0, 2),
    struct.pack("=BBBBI", 2, 24, 0, 0, 2) + attribute(1, b"\xc0\x00\x02\x01"),
    struct.pack("=BBBBI", 2, 24, 0, 0, 2) + attribute(1, b"bad"),
    struct.pack("=BBBBI", 2, 24, 0, 0, 2) + attribute(1, b"\xc0\x00\x02\x01") * 2,
    struct.pack("=BBBBI", 2, 24, 0, 0, 2) + attribute(1, b"\xc0\x00\x02\x01") + attribute(8, b"bad"),
    struct.pack("=BBBBI", 2, 24, 0, 0, 2) + b"\x01\x00\x01\x00",
    struct.pack("=BBBBI", 2, 33, 0, 0, 2) + attribute(2, b"\xc0\x00\x02\x01"),
    struct.pack("=BBBBI", 2, 24, 0, 0, 0) + attribute(2, b"\xc0\x00\x02\x01"),
    struct.pack("=BBBBI", 10, 129, 0, 0, 2) + attribute(1, ipaddress.ip_address("2001:db8::1").packed),
])
def test_malformed_address_messages_prove_nothing(monkeypatch, payload):
    query(monkeypatch, [message(20, payload) + DONE])
    with pytest.raises(ValueError):
        addresses.bind_address_assigned("192.0.2.1")


def test_another_network_namespace_opens_no_query(monkeypatch):
    monkeypatch.setattr(addresses.os, "readlink", lambda path: "self" if "/self/" in path else "host")
    monkeypatch.setattr(addresses.socket, "socket", lambda *_args: pytest.fail("foreign namespace queried"))
    with pytest.raises(ValueError):
        addresses.bind_address_assigned("192.0.2.1")
