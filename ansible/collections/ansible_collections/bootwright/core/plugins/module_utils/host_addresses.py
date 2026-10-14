"""Read usable local addresses through Linux's read-only RTM_GETADDR dump.

Wire layouts and flags follow Linux UAPI linux/{netlink,rtnetlink,if_addr}.h.
An incomplete or interrupted dump proves no address absent. No address is
assigned and no service socket is opened by this query.
"""

from __future__ import annotations

import ipaddress
import os
import socket
import struct
import sys
import time

HEADER = struct.Struct("=IHHII")
ADDRESS = struct.Struct("=BBBBI")
ATTRIBUTE = struct.Struct("=HH")
SEQUENCE = 1
RTM_NEWADDR = 20
RTM_GETADDR = 22
NLMSG_ERROR = 2
NLMSG_DONE = 3
NLM_F_DUMP_INTR = 0x10
NLM_F_REQUEST_DUMP = 0x301
IFA_ADDRESS = 1
IFA_LOCAL = 2
IFA_FLAGS = 8
UNUSABLE = 0x08 | 0x40  # IFA_F_DADFAILED | IFA_F_TENTATIVE
MAX_DATAGRAM = 1 << 16
MAX_DUMP = 1 << 20
QUERY_TIMEOUT = 5


def normalized(address):
    address = ipaddress.ip_address(address)
    return address.ipv4_mapped or address if address.version == 6 else address


def attributes(payload):
    found, at = {}, ADDRESS.size
    while at < len(payload):
        if len(payload) - at < ATTRIBUTE.size:
            raise ValueError("address attribute header")
        length, kind = ATTRIBUTE.unpack_from(payload, at)
        if length < ATTRIBUTE.size or at + length > len(payload):
            raise ValueError("address attribute length")
        if kind in (IFA_ADDRESS, IFA_LOCAL, IFA_FLAGS):
            if kind in found:
                raise ValueError("duplicate address attribute")
            found[kind] = payload[at + ATTRIBUTE.size:at + length]
        at += (length + 3) & ~3
    if at != len(payload):
        raise ValueError("address attribute padding")
    return found


def local_address(payload):
    if len(payload) < ADDRESS.size:
        raise ValueError("address message")
    family, prefix, flags, _scope, index = ADDRESS.unpack_from(payload)
    values = attributes(payload)
    if IFA_FLAGS in values:
        if len(values[IFA_FLAGS]) != 4:
            raise ValueError("address flags")
        flags = struct.unpack("=I", values[IFA_FLAGS])[0]
    if family not in (socket.AF_INET, socket.AF_INET6):
        raise ValueError("address family")
    width = 4 if family == socket.AF_INET else 16
    if prefix > width * 8 or index == 0:
        raise ValueError("address prefix or interface")
    for kind in (IFA_ADDRESS, IFA_LOCAL):
        if kind in values and len(values[kind]) != width:
            raise ValueError("address width")
    packed = values.get(IFA_LOCAL)
    if packed is None and family == socket.AF_INET6:
        packed = values.get(IFA_ADDRESS)
    if packed is None:
        raise ValueError("missing local address")
    return None if flags & UNUSABLE else normalized(packed)


def messages(datagram):
    at = 0
    while at < len(datagram):
        if len(datagram) - at < HEADER.size:
            raise ValueError("netlink message header")
        length, kind, flags, sequence, _pid = HEADER.unpack_from(datagram, at)
        if length < HEADER.size or at + length > len(datagram) or sequence != SEQUENCE:
            raise ValueError("netlink message length or sequence")
        if flags & NLM_F_DUMP_INTR:
            raise ValueError("interrupted address dump")
        yield kind, datagram[at + HEADER.size:at + length]
        at += (length + 3) & ~3
    if at != len(datagram):
        raise ValueError("netlink message padding")


def assigned_addresses():
    if not sys.platform.startswith("linux"):
        raise ValueError("Linux address query required")
    if os.readlink("/proc/self/ns/net") != os.readlink("/proc/1/ns/net"):
        raise ValueError("host network namespace required")
    query = HEADER.pack(HEADER.size + ADDRESS.size, RTM_GETADDR, NLM_F_REQUEST_DUMP, SEQUENCE, 0)
    query += ADDRESS.pack(socket.AF_UNSPEC, 0, 0, 0, 0)
    deadline, consumed, found = time.monotonic() + QUERY_TIMEOUT, 0, set()
    with socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, 0) as channel:
        channel.settimeout(QUERY_TIMEOUT)
        channel.sendto(query, (0, 0))
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("address dump deadline")
            channel.settimeout(remaining)
            datagram, _ancillary, flags, sender = channel.recvmsg(MAX_DATAGRAM)
            consumed += len(datagram)
            if not datagram or flags & socket.MSG_TRUNC or sender != (0, 0) or consumed > MAX_DUMP:
                raise ValueError("unproved address dump")
            done = False
            for kind, payload in messages(datagram):
                if done:
                    raise ValueError("message after address dump completion")
                if kind == NLMSG_DONE:
                    if len(payload) not in (0, 4) or (payload and struct.unpack("=i", payload)[0]):
                        raise ValueError("failed address dump")
                    done = True
                elif kind == RTM_NEWADDR:
                    address = local_address(payload)
                    if address is not None:
                        found.add(address)
                elif kind == NLMSG_ERROR:
                    raise ValueError("failed address query")
                else:
                    raise ValueError("unexpected address message")
            if done:
                return found


def bind_address_assigned(address):
    """Wildcards need no assigned address; explicit binds need a complete proof."""
    address = normalized(address)
    return address.is_unspecified or address in assigned_addresses()
