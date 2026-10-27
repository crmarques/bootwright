"""RAM-only logical overwrite through exclusively held, re-proved block FDs.

No command in this module discovers an erase target. The controller supplies a
frozen, authorized set; the worker refuses incomplete identities and receipts.
"""

from __future__ import annotations

import array
import fcntl
import json
import mmap
import os
from pathlib import Path
import re
import stat
import time

if __package__:
    from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity
else:
    import physical_machine_identity

CHUNK = 1024 * 1024
BLKGETSIZE64 = 0x80081272
BLKGETDISKSEQ = 0x80081280
BLKFLSBUF = 0x1261
VERSION = "physical-erase-v2"


class Refusal(Exception):
    """A closed, non-sensitive refusal code."""


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def overwrite(disk, expected, checkpoint):
    """Overwrite and independently read every logical byte through one handle."""
    disk.prove(expected)
    checkpoint()
    size = expected["sizeBytes"]
    zero = bytes(CHUNK)
    written = 0
    while written < size:
        checkpoint()
        disk.prove(expected)
        count = min(CHUNK, size - written)
        if disk.write(written, zero[:count]) != count:
            raise Refusal("short-write")
        written += count
    disk.flush()
    verified = 0
    while verified < size:
        checkpoint()
        disk.prove(expected)
        count = min(CHUNK, size - verified)
        if disk.read(verified, count) != zero[:count]:
            raise Refusal("readback")
        verified += count
    disk.prove(expected)
    return {"written": written, "verified": verified}


def receipt_for(request, coverage):
    if len(coverage) != len(request["targets"]):
        raise Refusal("coverage")
    targets = []
    for target, result in zip(request["targets"], coverage):
        if result != {"written": target["sizeBytes"], "verified": target["sizeBytes"]}:
            raise Refusal("coverage")
        targets.append(dict(target, **result))
    return {
        "version": VERSION,
        "state": "complete",
        "originalAttempt": request["originalAttempt"],
        "request": request["request"],
        "uuid": request["uuid"],
        "macs": request["macs"],
        "targets": targets,
    }


def validate_receipt(request, receipt):
    coverage = [{"written": target["sizeBytes"], "verified": target["sizeBytes"]} for target in request["targets"]]
    if receipt != receipt_for(request, coverage):
        raise Refusal("receipt")


def number_ioctl(fd, command):
    value = array.array("Q", [0])
    fcntl.ioctl(fd, command, value, True)
    return value[0]


def text(path):
    with open(path, "rb") as stream:
        data = stream.read(258)
    value = data.decode("ascii").strip()
    if not value or len(value) > 256 or not value.isprintable():
        raise Refusal("identity")
    return value


def hardware(request, root=Path("/sys")):
    try:
        physical_machine_identity.prove(request["uuid"], request["macs"], root)
    except ValueError:
        raise Refusal("system-identity") from None


def lines(path, limit=1024 * 1024):
    with open(path, "rb") as stream:
        data = stream.read(limit + 1)
    if len(data) > limit:
        raise Refusal("usage-inventory")
    return data.decode("ascii").splitlines()


def unused(nodes, proc=Path("/proc")):
    """Mounted child partitions, holders and active swap all refuse."""
    devices = {text(node / "dev") for node in nodes}
    for node in nodes:
        if any((node / "holders").iterdir()) or any((node / "slaves").iterdir()):
            raise Refusal("disk-in-use")
    # This RAM service runs in the host mount namespace and creates none.
    # Checking every extant namespace also rejects an installer descendant
    # which retained a mounted target after its ordinary unit was killed.
    namespaces = set()
    for process in proc.glob("[0-9]*"):
        try:
            identity = (process / "ns/mnt").stat().st_ino
            if identity in namespaces:
                continue
            namespaces.add(identity)
            for line in lines(process / "mountinfo"):
                fields = line.split()
                if len(fields) < 7 or ":" not in fields[2]:
                    raise Refusal("usage-inventory")
                if fields[2] in devices:
                    raise Refusal("disk-mounted")
        except FileNotFoundError:
            continue
    if not namespaces:
        raise Refusal("usage-inventory")
    swaps = lines(proc / "swaps")
    if not swaps or swaps[0].split() != ["Filename", "Type", "Size", "Used", "Priority"]:
        raise Refusal("usage-inventory")
    for line in swaps[1:]:
        fields = line.split()
        if len(fields) != 5:
            raise Refusal("usage-inventory")
        # Any active swap means this boot is not an isolated RAM worker.
        # A file may be backed by a target through another device stack.
        raise Refusal("active-swap")


def matches_wwn(observed, declared):
    # Linux SCSI VPD NAA and udev's wwn-0x alias encode the same exact bytes.
    # NVMe eui./uuid. and every other native form remain distinct.
    if re.fullmatch(r"0x(?:[0-9a-f]{16}|[0-9a-f]{32})", declared) and re.fullmatch(r"naa\.(?:[0-9a-f]{16}|[0-9a-f]{32})", observed):
        return observed[4:] == declared[2:]
    return observed == declared


def disk_serial(node):
    native = node / "device/serial"
    if native.exists():
        return text(native)
    # SCSI exposes the kernel's VPD page 0x80, rather than a serial text
    # attribute. Validate its complete length before decoding the payload.
    with (node / "device/vpd_pg80").open("rb") as source:
        data = source.read(261)
    if not 5 <= len(data) <= 260 or data[1] != 0x80 or int.from_bytes(data[2:4], "big") != len(data) - 4:
        raise Refusal("disk-serial")
    value = data[4:].decode("ascii").strip()
    if not value or not value.isprintable():
        raise Refusal("disk-serial")
    return value


class BlockDisk:
    """Linux block identity includes diskseq, so a recycled dev_t cannot pass."""

    def __init__(self, expected):
        path = Path(expected["deviceName"]).resolve(strict=True)
        self.fd = os.open(path, os.O_RDWR | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_DIRECT)
        try:
            info = os.fstat(self.fd)
            if not stat.S_ISBLK(info.st_mode):
                raise Refusal("whole-block-device")
            self.dev = info.st_rdev
            self.sys = Path("/sys/dev/block") / (str(os.major(self.dev)) + ":" + str(os.minor(self.dev)))
            self.sequence = number_ioctl(self.fd, BLKGETDISKSEQ)
            if not self.sequence or (self.sys / "partition").exists():
                raise Refusal("whole-block-device")
            self.prove(expected)
        except BaseException:
            os.close(self.fd)
            raise

    def close(self):
        os.close(self.fd)

    def prove(self, expected):
        before = number_ioctl(self.fd, BLKGETDISKSEQ)
        if before != self.sequence or int(text(self.sys / "diskseq")) != before:
            raise Refusal("disk-replaced")
        if number_ioctl(self.fd, BLKGETSIZE64) != expected["sizeBytes"]:
            raise Refusal("disk-size")
        # These sysfs attributes are hardware-derived. Missing attributes are
        # unknown, never filled from a stale udev database or a guessed path.
        wwn = text(self.sys / "wwid") if (self.sys / "wwid").exists() else text(self.sys / "device/wwid")
        serial = disk_serial(self.sys)
        if not matches_wwn(wwn, expected["wwn"]) or serial != expected["serialNumber"]:
            raise Refusal("disk-identity")
        unused([self.sys, *(candidate.parent for candidate in self.sys.glob("*/partition"))])
        if number_ioctl(self.fd, BLKGETDISKSEQ) != before:
            raise Refusal("disk-replaced")

    def write(self, offset, data):
        with mmap.mmap(-1, len(data)) as aligned:
            aligned[:] = data
            return os.pwritev(self.fd, [aligned], offset)

    def flush(self):
        os.fsync(self.fd)
        fcntl.ioctl(self.fd, BLKFLSBUF)

    def read(self, offset, size):
        with mmap.mmap(-1, size) as aligned:
            count = os.preadv(self.fd, [aligned], offset)
            return aligned[:count]


def validate_request(request):
    if set(request) != {"version", "originalAttempt", "request", "uuid", "macs", "targets", "deadlineSeconds"}:
        raise Refusal("request")
    if request["version"] != VERSION or not isinstance(request["originalAttempt"], str) or not request["originalAttempt"]:
        raise Refusal("request")
    if not isinstance(request["request"], str) or not re.fullmatch("[0-9a-f]{64}", request["request"]):
        raise Refusal("request")
    if not isinstance(request["deadlineSeconds"], int) or isinstance(request["deadlineSeconds"], bool) or not 60 <= request["deadlineSeconds"] <= 14400:
        raise Refusal("deadline")
    if not isinstance(request["uuid"], str) or not request["uuid"] or not isinstance(request["macs"], list) or not request["macs"]:
        raise Refusal("system-identity")
    if (
        any(not isinstance(mac, str) or not re.fullmatch("[0-9a-f]{2}(:[0-9a-f]{2}){5}", mac) for mac in request["macs"])
        or sorted(set(request["macs"])) != request["macs"]
    ):
        raise Refusal("system-identity")
    if not 1 <= len(request["targets"]) <= 16:
        raise Refusal("targets")
    seen = [set(), set(), set()]
    for target in request["targets"]:
        if set(target) != {"deviceName", "wwn", "serialNumber", "sizeBytes"}:
            raise Refusal("target")
        for index, key in enumerate(("deviceName", "wwn", "serialNumber")):
            value = target[key]
            if not isinstance(value, str) or not value or len(value) > 256 or not value.isprintable() or value in seen[index]:
                raise Refusal("target-identity")
            seen[index].add(value)
        if not target["deviceName"].startswith("/dev/") or ".." in target["deviceName"].split("/"):
            raise Refusal("target-path")
        size = target["sizeBytes"]
        if not isinstance(size, int) or isinstance(size, bool) or size < 4096 or size % 4096 or size > 64 * 1024**4:
            raise Refusal("target-size")


def atomic(path, value):
    temporary = str(path) + ".new"
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        data = canonical(value)
        if os.write(fd, data) != len(data):
            raise Refusal("receipt-write")
        os.fsync(fd)
    finally:
        os.close(fd)
    os.replace(temporary, path)
    parent = os.open(str(Path(path).parent), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def execute(request, directory, opener=BlockDisk, prove_hardware=hardware):
    validate_request(request)
    deadline = time.monotonic() + request["deadlineSeconds"]

    def checkpoint():
        if time.monotonic() >= deadline:
            raise Refusal("deadline")
        prove_hardware(request)

    checkpoint()
    disks = []
    try:
        for target in request["targets"]:
            disk = opener(target)
            disks.append(disk)
        if len({disk.dev for disk in disks}) != len(disks):
            raise Refusal("duplicate-device")
        # Acquire and prove the complete set before the first disk is written.
        for disk, target in zip(disks, request["targets"]):
            disk.prove(target)
        atomic(Path(directory) / "status.json", {"state": "partial", "request": request["request"], "originalAttempt": request["originalAttempt"]})
        coverage = [overwrite(disk, target, checkpoint) for disk, target in zip(disks, request["targets"])]
        result = receipt_for(request, coverage)
        atomic(Path(directory) / "status.json", result)
        return result
    finally:
        for disk in disks:
            disk.close()
