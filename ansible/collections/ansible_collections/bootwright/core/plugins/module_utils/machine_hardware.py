"""Read the permanent hardware addresses of one host's network interfaces.

A context that uses a server as an OS-ready Machine over SSH claims it by the
address each network interface carries in hardware, so that a context driving
the same server by its BMC, which claims its declared MACs, meets it
(specs/substrates.md, Physical machine realization). Only an interface backed
by a device counts, and only by the address that device reports as its own:
a bond, VLAN, bridge, veth, tun or other software interface has no device, a
virtual function's address is assigned rather than carried, a NIC on USB,
such as a BMC's Redfish host interface, can carry one address on every server
of a model, and an address
that was randomized or set counts only through the permanent address the
driver reports beside it, as a bond member's does.
"""

from __future__ import annotations

import array
import errno
import fcntl
import os
import re
import socket
import struct

SYSFS = "/sys/class/net"
ARPHRD_ETHER = 1
NET_ADDR_PERM = 0
SIOCETHTOOL = 0x8946
ETHTOOL_GPERMADDR = 0x20
MAX_ADDR_LEN = 32
MAX_INTERFACES = 4096
NAME = re.compile(r"[^/\s]{1,15}")
TEXT = re.compile(r"[0-9a-f]{2}(:[0-9a-f]{2}){5}")


class Unreadable(Exception):
    """The host's interfaces could not be read in full, so no claim is proved."""


def canonical(raw):
    """The colon-separated lower-case form of one unicast six-byte address, or None."""
    if not isinstance(raw, (bytes, bytearray)) or len(raw) != 6 or not any(raw) or raw[0] & 1:
        return None
    return ":".join("%02x" % octet for octet in raw)


def parsed(text):
    """The canonical form of an address sysfs writes, or None."""
    value = text.strip().lower() if isinstance(text, str) else ""
    if not TEXT.fullmatch(value):
        return None
    return canonical(bytes(int(octet, 16) for octet in value.split(":")))


def permanent_address(interface):
    """The address one interface carries in hardware, or None when it carries none it reports."""
    if interface.get("type") != ARPHRD_ETHER or not interface.get("device") or interface.get("virtualFunction") or interface.get("usb"):
        return None
    reported = canonical(interface.get("permanent"))
    if reported is not None:
        return reported
    if interface.get("assignType") == NET_ADDR_PERM:
        return parsed(interface.get("address"))
    return None


def addresses(interfaces):
    """Every permanent hardware address the interfaces report, sorted and once each."""
    found = {permanent_address(interface) for interface in interfaces}
    found.discard(None)
    return sorted(found)


def ethtool_permanent(name):
    """The permanent address the driver reports for one interface, or None."""
    request = array.array("B", struct.pack("=II", ETHTOOL_GPERMADDR, MAX_ADDR_LEN) + bytes(MAX_ADDR_LEN))
    pointer = request.buffer_info()[0]
    ifreq = struct.pack("16sP16x", name.encode("ascii"), pointer)
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        fcntl.ioctl(sock.fileno(), SIOCETHTOOL, ifreq)
    except OSError:
        return None
    finally:
        sock.close()
    data = request.tobytes()
    size = struct.unpack_from("=I", data, 4)[0]
    return data[8:8 + size] if size == 6 else None


def attribute(directory, name):
    """One sysfs attribute's text, None when the device does not provide it."""
    try:
        with open(os.path.join(directory, name), encoding="ascii") as handle:
            return handle.read(64)
    except OSError as failure:
        if failure.errno in (errno.EINVAL, errno.EOPNOTSUPP, errno.ENOENT):
            return None
        raise


def number(text):
    """The integer one sysfs attribute holds, or None."""
    try:
        return int(text.strip()) if text is not None else None
    except ValueError:
        return None


def interface(root, name, permanent):
    """One interface as sysfs and its driver describe it, or None when it is gone."""
    directory = os.path.join(root, name)
    if not os.path.isdir(directory):
        return None
    kind = attribute(directory, "type")
    if kind is None:
        return None
    entry = {"name": name, "type": number(kind), "device": os.path.exists(os.path.join(directory, "device")),
             "virtualFunction": os.path.exists(os.path.join(directory, "device", "physfn")),
             "usb": os.path.basename(os.path.realpath(os.path.join(directory, "device", "subsystem"))) == "usb",
             "assignType": number(attribute(directory, "addr_assign_type")), "address": attribute(directory, "address")}
    counted = entry["type"] == ARPHRD_ETHER and entry["device"] and not entry["virtualFunction"] and not entry["usb"]
    entry["permanent"] = permanent(name) if counted else None
    return entry


def read(root=SYSFS, permanent=ethtool_permanent):
    """The permanent hardware addresses this host reports, refusing a host it cannot read in full."""
    try:
        names = sorted(os.listdir(root))
    except OSError as failure:
        raise Unreadable("the network interfaces could not be listed") from failure
    if len(names) > MAX_INTERFACES or not all(NAME.fullmatch(name) for name in names):
        raise Unreadable("the network interfaces could not be read in full")
    entries = []
    try:
        for name in names:
            entry = interface(root, name, permanent)
            if entry is not None:
                entries.append(entry)
    except OSError as failure:
        raise Unreadable("a network interface could not be read") from failure
    return addresses(entries)
