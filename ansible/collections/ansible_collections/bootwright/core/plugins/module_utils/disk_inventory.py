"""Bounded read-only whole-disk inventory for the dedicated RAM discovery boot."""

from __future__ import annotations

import ctypes
import errno
from itertools import islice
import os
from pathlib import Path
import re
import stat

MAX_DISKS = 256
MAX_SIZE = 64 << 40
MAX_TEXT = 256
BLKGETSIZE64 = 0x80081272
NAME = re.compile(r"(?:sd[a-z]+|nvme[0-9]+n[0-9]+)\Z")
PCI = re.compile(r"[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]\Z")
NAA = re.compile(r"(?:naa\.|0x)([0-9a-f]{16}(?:[0-9a-f]{16})?)\Z")
SERIAL = re.compile(r"[A-Za-z0-9._:+/=-]{1,256}\Z")


class Refused(Exception):
    """Unknown or unstable hardware supplies no discovery evidence."""


def attribute(path, optional=False):
    """Read one sysfs scalar within its bound; never execute a device command."""
    try:
        with path.open("rb") as stream:
            data = stream.read(MAX_TEXT + 3)
    except OSError as error:
        if optional and error.errno in (errno.ENOENT, errno.EINVAL, errno.ENODATA, errno.EOPNOTSUPP):
            return ""
        if error.errno == errno.ENOENT:
            raise Refused("missing disk attribute") from None
        raise Refused("disk attribute could not be read") from None
    if len(data) > MAX_TEXT + 2:
        raise Refused("disk attribute exceeds its bound")
    try:
        value = data.decode("ascii").strip(" \t\r\n")
    except UnicodeDecodeError:
        raise Refused("disk attribute is not qualified ASCII") from None
    if len(value) > MAX_TEXT or any(ord(c) < 32 or ord(c) > 126 or c in "<>&" for c in value):
        raise Refused("disk attribute has unsupported text")
    return value


def serial_number(device):
    value = attribute(device / "device/serial", optional=True)
    if not value:
        value = attribute(device / "serial", optional=True)
    if not value:
        path = device / "device/vpd_pg80"
        try:
            with path.open("rb") as stream:
                data = stream.read(MAX_TEXT + 5)
        except FileNotFoundError:
            return ""
        if len(data) < 4 or data[1] != 0x80 or int.from_bytes(data[2:4], "big") != len(data) - 4 or len(data) > MAX_TEXT + 4:
            raise Refused("disk VPD serial is malformed")
        try:
            value = data[4:].decode("ascii").strip()
        except UnicodeDecodeError:
            raise Refused("disk VPD serial is not ASCII") from None
    if value and SERIAL.fullmatch(value) is None:
        raise Refused("disk serial is outside its qualified grammar")
    return value


def size_ioctl(descriptor):
    """The one read-only ioctl, through libc (RHEL initrd omits fcntl)."""
    libc = ctypes.CDLL(None, use_errno=True)
    ioctl = libc.ioctl
    ioctl.argtypes = (ctypes.c_int, ctypes.c_ulong, ctypes.c_void_p)
    ioctl.restype = ctypes.c_int
    size = ctypes.c_uint64()
    if ioctl(descriptor, BLKGETSIZE64, ctypes.byref(size)) != 0:
        raise Refused("disk size ioctl was not answered")
    return size.value


def descriptor_size(path):
    """Only O_RDONLY descriptors and the read-only size ioctl reach data disks."""
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        if not stat.S_ISBLK(before.st_mode):
            raise Refused("inventory path is not a block device")
        capacity = size_ioctl(fd)
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_rdev) != (after.st_dev, after.st_ino, after.st_rdev):
            raise Refused("disk descriptor changed")
        if not 4096 <= capacity <= MAX_SIZE or capacity % 4096:
            raise Refused("disk capacity is outside the qualified bound")
        return capacity, before.st_rdev
    finally:
        os.close(fd)


def topology(device):
    path = device.resolve(strict=True)
    parts = path.parts
    controller = next((part for part in reversed(parts) if PCI.fullmatch(part)), "")
    usb = any(re.fullmatch(r"usb[0-9]+", part) for part in parts)
    transport = "usb" if usb else "nvme" if device.name.startswith("nvme") else "scsi"
    return controller, transport


def read_disk(device, devroot):
    major_minor = attribute(device / "dev")
    if re.fullmatch(r"[0-9]+:[0-9]+", major_minor) is None:
        raise Refused("disk device number is malformed")
    capacity, number = descriptor_size(devroot / device.name)
    if major_minor != "%d:%d" % (os.major(number), os.minor(number)):
        raise Refused("sysfs and block descriptor disagree")
    removable, readonly = attribute(device / "removable"), attribute(device / "ro")
    if removable not in ("0", "1") or readonly not in ("0", "1"):
        raise Refused("disk eligibility attributes are unknown")
    wwid = attribute(device / "wwid", optional=True) or attribute(device / "device/wwid", optional=True)
    match = NAA.fullmatch(wwid.lower())
    wwn = "0x" + match.group(1) if match and int(match.group(1), 16) else ""
    serial = serial_number(device)
    controller, transport = topology(device)
    disk = {
        "sizeBytes": capacity,
        "model": attribute(device / "device/model", optional=True),
        "controllerPCIAddress": controller,
        "transport": transport,
        "fixed": removable == "0",
        "writable": readonly == "0",
        "whole": True,
        "hidden": False,
    }
    if wwn:
        disk["wwn"] = wwn
    if serial:
        disk["serialNumber"] = serial
    # Read the identity twice around the descriptor proof; hotplug is unknown.
    if major_minor != attribute(device / "dev") or serial != serial_number(device):
        raise Refused("disk inventory changed during its read")
    if wwid != (attribute(device / "wwid", optional=True) or attribute(device / "device/wwid", optional=True)):
        raise Refused("disk WWN changed during its read")
    return disk


def collect(sysroot=Path("/sys/class/block"), devroot=Path("/dev")):
    before = sorted(path.name for path in islice(sysroot.iterdir(), 4 * MAX_DISKS + 1))
    if len(before) > 4 * MAX_DISKS:
        raise Refused("block inventory exceeds its bound")
    disks = []
    for name in before:
        device = sysroot / name
        if NAME.fullmatch(name) is None or (device / "partition").exists():
            continue
        hidden = attribute(device / "hidden", optional=True)
        if hidden not in ("", "0", "1"):
            raise Refused("NVMe path identity is unknown")
        if hidden == "1":
            continue
        disks.append(read_disk(device, devroot))
        if len(disks) > MAX_DISKS:
            raise Refused("whole-disk inventory exceeds its bound")
    if sorted(path.name for path in islice(sysroot.iterdir(), 4 * MAX_DISKS + 1)) != before:
        raise Refused("block inventory changed during enumeration")
    if not disks:
        raise Refused("Linux reports no supported whole disks")
    return disks
