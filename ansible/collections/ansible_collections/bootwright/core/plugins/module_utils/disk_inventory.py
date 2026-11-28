"""Bounded read-only whole-disk inventory for the dedicated RAM discovery boot."""

from __future__ import annotations

from contextlib import contextmanager
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
WHITESPACE = " \t\n\v\f\r"
BLKGETSIZE64 = 0x80081272
BLKGETDISKSEQ = 0x80081280
NAME = re.compile(r"(?:sd[a-z]+|nvme[0-9]+n[0-9]+)\Z")
RAM_DEVICE = re.compile(r"(?:loop|ram|zram)[0-9]+\Z")
OPTICAL = re.compile(r"sr[0-9]+\Z")
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
        value = data.decode("ascii").strip(WHITESPACE)
    except UnicodeDecodeError:
        raise Refused("disk attribute is not qualified ASCII") from None
    if len(value) > MAX_TEXT or any(ord(c) < 32 or ord(c) > 126 or c in "<>&" for c in value):
        raise Refused("disk attribute has unsupported text")
    return value


def serial_number(device):
    native = device / "device/serial"
    if native.exists():
        value = attribute(native, optional=True)
    else:
        value = vpd_serial(device / "device/vpd_pg80")
    if value and SERIAL.fullmatch(value) is None:
        raise Refused("disk serial is outside its qualified grammar")
    return value


def vpd_serial(path):
    try:
        with path.open("rb") as stream:
            data = stream.read(MAX_TEXT + 5)
    except FileNotFoundError:
        return ""
    if len(data) < 4 or data[1] != 0x80 or int.from_bytes(data[2:4], "big") != len(data) - 4 or len(data) > MAX_TEXT + 4:
        raise Refused("disk VPD serial is malformed")
    try:
        return data[4:].decode("ascii").strip(WHITESPACE)
    except UnicodeDecodeError:
        raise Refused("disk VPD serial is not ASCII") from None


def wwid(device):
    disk = device / "wwid"
    return attribute(disk if disk.exists() else device / "device/wwid", optional=True)


def canonical_wwn(reported):
    match = NAA.fullmatch(reported)
    return "0x" + match.group(1) if match and int(match.group(1), 16) else ""


def number_ioctl(descriptor, request):
    """Read the block value through libc (the RHEL initrd omits fcntl)."""
    libc = ctypes.CDLL(None, use_errno=True)
    ioctl = libc.ioctl
    ioctl.argtypes = (ctypes.c_int, ctypes.c_ulong, ctypes.c_void_p)
    ioctl.restype = ctypes.c_int
    size = ctypes.c_uint64()
    if ioctl(descriptor, request, ctypes.byref(size)) != 0:
        raise Refused("read-only disk ioctl was not answered")
    return size.value


@contextmanager
def block_descriptor(path):
    """Keep the read-only descriptor tied to the device during the entire read."""
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        if not stat.S_ISBLK(before.st_mode):
            raise Refused("inventory path is not a block device")
        yield fd, before.st_rdev
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_rdev) != (after.st_dev, after.st_ino, after.st_rdev):
            raise Refused("disk descriptor changed")
    finally:
        os.close(fd)


def descriptor_size(fd):
    capacity = number_ioctl(fd, BLKGETSIZE64)
    if not 4096 <= capacity <= MAX_SIZE or capacity % 4096:
        raise Refused("disk capacity is outside the qualified bound")
    return capacity


def topology(device):
    path = device.resolve(strict=True)
    parts = path.parts
    controller = next((part for part in reversed(parts) if PCI.fullmatch(part)), "")
    usb = any(re.fullmatch(r"usb[0-9]+", part) for part in parts)
    transport = "usb" if usb else "nvme" if device.name.startswith("nvme") else "scsi"
    return controller, transport


def read_disk(device, devroot, single_disk=False):
    major_minor = attribute(device / "dev")
    if re.fullmatch(r"[0-9]+:[0-9]+", major_minor) is None:
        raise Refused("disk device number is malformed")
    with block_descriptor(devroot / device.name) as (fd, number):
        if major_minor != "%d:%d" % (os.major(number), os.minor(number)):
            raise Refused("sysfs and block descriptor disagree")
        sequence = 0
        if single_disk:
            sequence = number_ioctl(fd, BLKGETDISKSEQ)
            if sequence == 0 or attribute(device / "diskseq") != str(sequence):
                raise Refused("sysfs and block descriptor disk sequence disagree")
        return read_identity(device, fd, major_minor, sequence)


def read_identity(device, fd, major_minor, sequence):
    capacity = descriptor_size(fd)
    removable, readonly = attribute(device / "removable"), attribute(device / "ro")
    if removable not in ("0", "1") or readonly not in ("0", "1"):
        raise Refused("disk eligibility attributes are unknown")
    reported = wwid(device)
    wwn = canonical_wwn(reported)
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
    if reported != wwid(device):
        raise Refused("disk WWN changed during its read")
    if sequence and (
            number_ioctl(fd, BLKGETDISKSEQ) != sequence or attribute(device / "diskseq") != str(sequence)
            or descriptor_size(fd) != capacity or attribute(device / "removable") != removable or attribute(device / "ro") != readonly):
        raise Refused("disk identity or eligibility changed during its read")
    return disk


def collect(sysroot=Path("/sys/class/block"), devroot=Path("/dev"), single_disk=False):
    before = sorted(path.name for path in islice(sysroot.iterdir(), 4 * MAX_DISKS + 1))
    if len(before) > 4 * MAX_DISKS:
        raise Refused("block inventory exceeds its bound")
    disks = []
    for name in before:
        device = sysroot / name
        if (device / "partition").exists():
            continue
        if NAME.fullmatch(name) is None:
            if single_disk and not (RAM_DEVICE.fullmatch(name) or (OPTICAL.fullmatch(name) and attribute(device / "device/type") == "5")):
                raise Refused("unexpected block device in single-disk preflight")
            continue
        hidden = attribute(device / "hidden", optional=True)
        if hidden not in ("", "0", "1"):
            raise Refused("NVMe path identity is unknown")
        if hidden == "1":
            if single_disk:
                raise Refused("multipath is outside single-disk preflight")
            continue
        disks.append(read_disk(device, devroot, single_disk))
        if len(disks) > MAX_DISKS:
            raise Refused("whole-disk inventory exceeds its bound")
    if sorted(path.name for path in islice(sysroot.iterdir(), 4 * MAX_DISKS + 1)) != before:
        raise Refused("block inventory changed during enumeration")
    if not disks:
        raise Refused("Linux reports no supported whole disks")
    return disks
