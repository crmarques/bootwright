"""RAM-only logical overwrite or metadata clear through exclusively held, re-proved block FDs.

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
import threading
import time

if __package__:
    from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process
    from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity
else:
    import physical_erase_process
    import physical_machine_identity

CHUNK = 4 * 1024 * 1024
BLKGETSIZE64 = 0x80081272
BLKGETDISKSEQ = 0x80081280
BLKFLSBUF = 0x1261
VERSION = "physical-erase-v10"
METHODS = ("logicalOverwrite", "metadataClear")
# BlueStore keeps its replica bdev labels at these offsets of the OSD's LV;
# the window also covers the few MiB a ceph-volume LV starts into the disk.
WINDOW = 128 * 1024**2
REPLICAS = (1024**3, 10 * 1024**3, 100 * 1024**3, 1000 * 1024**3)
DMSETUP = "/usr/sbin/dmsetup"
DM_DEV = re.compile(r"[0-9]{1,10}:[0-9]{1,10}")
DM_NAME = re.compile(r"[A-Za-z0-9#+.:=@_][A-Za-z0-9#+.:=@_-]{0,126}")
MAX_HOLDERS = 64
# Write and readback of 896 GiB at a sustained 160 MB/s, with the per-chunk
# recheck, fit the four-hour deadline with about 14% to spare; Go's
# machine.MaxLogicalOverwriteBytes is the same bound.
MAX_OVERWRITE = 896 * 1024**3


class Refusal(Exception):
    """A closed, non-sensitive refusal code."""


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def metadata_regions(size):
    """The merged, sorted [start, end) intervals metadataClear zeroes; Go's MetadataClearRegions twins it."""
    spans = [(0, min(WINDOW, size)), (max(size - WINDOW, 0), size)]
    spans.extend((offset, min(offset + WINDOW, size)) for offset in REPLICAS if offset < size)
    merged = []
    for start, end in sorted(spans):
        if merged and start <= merged[-1][1]:
            merged[-1] = (merged[-1][0], max(merged[-1][1], end))
        else:
            merged.append((start, end))
    return merged


def regions(target):
    if target["method"] == "metadataClear":
        return metadata_regions(target["sizeBytes"])
    if target["method"] == "logicalOverwrite":
        return [(0, target["sizeBytes"])]
    raise Refusal("target-method")


def covered(target):
    return sum(end - start for start, end in regions(target))


def chunks(disk, expected, checkpoint, spans, step):
    """Recheck the run and the disk's identity before each chunk, then let step act on it."""
    total = 0
    for start, end in spans:
        offset = start
        while offset < end:
            checkpoint()
            disk.identify(expected)
            count = min(CHUNK, end - offset)
            step(offset, count)
            offset += count
            total += count
    return total


def overwrite(disk, expected, checkpoint):
    """Zero, flush and independently read back every byte of the method's regions through one handle."""
    disk.prove(expected)
    checkpoint()
    spans = regions(expected)
    zero = bytes(CHUNK)

    def write(offset, count):
        if disk.write(offset, zero[:count]) != count:
            raise Refusal("short-write")

    def read(offset, count):
        if disk.read(offset, count) != zero[:count]:
            raise Refusal("readback")

    written = chunks(disk, expected, checkpoint, spans, write)
    disk.flush()
    verified = chunks(disk, expected, checkpoint, spans, read)
    disk.prove(expected)
    return {"written": written, "verified": verified}


def receipt_for(request, coverage):
    if len(coverage) != len(request["targets"]):
        raise Refusal("coverage")
    targets = []
    for target, result in zip(request["targets"], coverage):
        if result != {"written": covered(target), "verified": covered(target)}:
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
    coverage = [{"written": covered(target), "verified": covered(target)} for target in request["targets"]]
    if receipt != receipt_for(request, coverage):
        raise Refusal("receipt")


def number_ioctl(fd, command):
    value = array.array("Q", [0])
    fcntl.ioctl(fd, command, value, True)
    return value[0]


def text(path, code="identity"):
    with open(path, "rb") as stream:
        data = stream.read(258)
    try:
        value = data.decode("ascii").strip()
    except UnicodeDecodeError:
        raise Refusal(code) from None
    if not value or len(value) > 256 or not value.isprintable():
        raise Refusal(code)
    return value


def sequence_number(path):
    value = text(path)
    if not value.isdecimal():
        raise Refusal("identity")
    return int(value)


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
    try:
        return data.decode("ascii").splitlines()
    except UnicodeDecodeError:
        raise Refusal("usage-inventory") from None


def unused(nodes, proc=Path("/proc")):
    """Mounted child partitions, holders and active swap all refuse."""
    devices = {text(node / "dev") for node in nodes}
    for node in nodes:
        if any((node / "holders").iterdir()) or any((node / "slaves").iterdir()):
            raise Refusal("disk-in-use")
    unmounted(devices, proc)
    swapless(proc)


def unmounted(devices, proc):
    """No mount namespace mounts any of the device numbers."""
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


def swapless(proc):
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
        return text(native, "disk-serial")
    # SCSI exposes the kernel's VPD page 0x80, rather than a serial text
    # attribute. Validate its complete length before decoding the payload.
    with (node / "device/vpd_pg80").open("rb") as source:
        data = source.read(261)
    if not 5 <= len(data) <= 260 or data[1] != 0x80 or int.from_bytes(data[2:4], "big") != len(data) - 4:
        raise Refusal("disk-serial")
    try:
        value = data[4:].decode("ascii").strip()
    except UnicodeDecodeError:
        raise Refusal("disk-serial") from None
    if not value or not value.isprintable():
        raise Refusal("disk-serial")
    return value


def flag(path):
    try:
        return text(path)
    except (Refusal, OSError):
        return None


def resolve_serial(serial, root=Path("/sys")):
    """The one fixed, writable whole sd or nvme disk reporting serial."""
    # A disk whose serial cannot be read is skipped, so it never matches; two
    # paths of one multipath disk, namespaces of one NVMe controller and a
    # cloned serial all match twice, and the worker never chooses. A hidden
    # NVMe path node (nvme0c0n1) is its head node's own disk, so only a
    # hidden attribute reading 1 skips it; a missing, unreadable or other
    # value counts it, which can only add a match and refuse.
    matches = []
    for node in sorted((root / "class/block").iterdir()):
        if (node / "partition").exists() or flag(node / "hidden") == "1":
            continue
        try:
            observed = disk_serial(node)
        except (Refusal, OSError):
            continue
        if observed == serial:
            matches.append(node)
    if not matches:
        raise Refusal("disk-serial-absent")
    if len(matches) != 1:
        raise Refusal("disk-serial-ambiguous")
    node = matches[0]
    if not re.fullmatch(r"sd[a-z]+|nvme[0-9]+n[0-9]+", node.name) or flag(node / "removable") != "0" or flag(node / "ro") != "0":
        raise Refusal("whole-block-device")
    return "/dev/" + node.name


class BlockDisk:
    """Linux block identity includes diskseq, so a recycled dev_t cannot pass."""

    def __init__(self, expected, exclusive=True):
        named = expected["deviceName"] if "deviceName" in expected else resolve_serial(expected["serialNumber"])
        path = Path(named).resolve(strict=True)
        self.buffer = None
        # The read-only probe proves identity before stacked holders release.
        access = os.O_RDWR | os.O_EXCL | os.O_DIRECT if exclusive else os.O_RDONLY
        self.fd = os.open(path, access | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(self.fd)
            if not stat.S_ISBLK(info.st_mode):
                raise Refusal("whole-block-device")
            self.dev = info.st_rdev
            self.sys = Path("/sys/dev/block") / (str(os.major(self.dev)) + ":" + str(os.minor(self.dev)))
            self.sequence = number_ioctl(self.fd, BLKGETDISKSEQ)
            if not self.sequence or (self.sys / "partition").exists():
                raise Refusal("whole-block-device")
            if exclusive:
                self.prove(expected)
            else:
                self.identify(expected)
        except BaseException:
            os.close(self.fd)
            raise

    def close(self):
        if self.buffer is not None:
            self.buffer.close()
        os.close(self.fd)

    def nodes(self):
        return [self.sys, *(candidate.parent for candidate in self.sys.glob("*/partition"))]

    def identify(self, expected):
        before = number_ioctl(self.fd, BLKGETDISKSEQ)
        if before != self.sequence or sequence_number(self.sys / "diskseq") != before:
            raise Refusal("disk-replaced")
        if number_ioctl(self.fd, BLKGETSIZE64) != expected["sizeBytes"]:
            raise Refusal("disk-size")
        # These sysfs attributes are hardware-derived. Missing attributes are
        # unknown, never filled from a stale udev database or a guessed path.
        # A target declared by serial number alone declares no WWN, so none
        # is read.
        if "wwn" in expected:
            wwn = text(self.sys / "wwid") if (self.sys / "wwid").exists() else text(self.sys / "device/wwid")
            if not matches_wwn(wwn, expected["wwn"]):
                raise Refusal("disk-identity")
        if disk_serial(self.sys) != expected["serialNumber"]:
            raise Refusal("disk-identity")
        if number_ioctl(self.fd, BLKGETDISKSEQ) != before:
            raise Refusal("disk-replaced")
        return before

    def idle(self):
        unused(self.nodes())

    def prove(self, expected):
        before = self.identify(expected)
        self.idle()
        if number_ioctl(self.fd, BLKGETDISKSEQ) != before:
            raise Refusal("disk-replaced")

    def aligned(self, size):
        # One page-aligned buffer per disk; a fresh mapping per chunk costs
        # more in page faults than the copy it carries.
        if self.buffer is None:
            self.buffer = mmap.mmap(-1, CHUNK)
        if not 0 < size <= CHUNK:
            raise Refusal("chunk")
        return self.buffer

    def write(self, offset, data):
        buffer = self.aligned(len(data))
        buffer[: len(data)] = data
        with memoryview(buffer) as view, view[: len(data)] as chunk:
            return os.pwritev(self.fd, [chunk], offset)

    def flush(self):
        os.fsync(self.fd)
        fcntl.ioctl(self.fd, BLKFLSBUF)

    def read(self, offset, size):
        buffer = self.aligned(size)
        with memoryview(buffer) as view, view[:size] as chunk:
            count = os.preadv(self.fd, [chunk], offset)
        return buffer[:count]


def stacked(nodes):
    """Every device-mapper node stacked over the targets, each after all of its own holders."""
    found, pending = {}, list(nodes)
    while pending:
        for entry in sorted((pending.pop() / "holders").iterdir()):
            holder = entry.resolve(strict=True)
            if holder in found:
                continue
            # An md, bcache or any other holder is not this worker's to release.
            if len(found) >= MAX_HOLDERS or not (holder / "dm/name").is_file():
                raise Refusal("disk-in-use")
            found[holder] = text(holder / "dm/name", "disk-in-use")
            if not DM_NAME.fullmatch(found[holder]):
                raise Refusal("disk-in-use")
            pending.append(holder)
    # Each slave is a target, a target's partition or another stacked node, so
    # no released node spans a disk the frozen request does not declare.
    devices = {text(node / "dev") for node in [*nodes, *found]}
    for holder in found:
        slaves = [entry.resolve(strict=True) for entry in (holder / "slaves").iterdir()]
        if not slaves or any(text(slave / "dev") not in devices for slave in slaves):
            raise Refusal("disk-in-use")
    order = []
    while len(order) < len(found):
        ready = [holder for holder in found if holder not in order and all(entry.resolve(strict=True) in order for entry in (holder / "holders").iterdir())]
        if not ready:
            raise Refusal("disk-in-use")
        order.extend(sorted(ready))
    stack = [(holder, found[holder], text(holder / "dev", "disk-in-use")) for holder in order]
    if not all(DM_DEV.fullmatch(dev) for holder, name, dev in stack):
        raise Refusal("disk-in-use")
    return stack


def dmsetup(run, *arguments):
    try:
        code, output = run([DMSETUP, *arguments], timeout=30, limit=4096)
    except physical_erase_process.Refusal:
        raise Refusal("disk-in-use") from None
    if code:
        raise Refusal("disk-in-use")
    return output.strip()


def release_holders(nodes, run=physical_erase_process.run, proc=Path("/proc")):
    """Remove unmounted, unopened device-mapper nodes over only the targets, deepest first."""
    stack = stacked(nodes)
    if not stack:
        return
    unmounted({dev for holder, name, dev in stack}, proc)
    swapless(proc)
    for holder, name, dev in stack:
        if text(holder / "dm/name", "disk-in-use") != name or text(holder / "dev", "disk-in-use") != dev or any((holder / "holders").iterdir()):
            raise Refusal("disk-in-use")
        # The device number, never the renameable name, selects the node.
        major, minor = dev.split(":")
        if dmsetup(run, "info", "--columns", "--noheadings", "--options", "open", "-j", major, "-m", minor) != b"0":
            raise Refusal("disk-in-use")
        dmsetup(run, "remove", "-j", major, "-m", minor)
    if any(any((node / "holders").iterdir()) for node in nodes):
        raise Refusal("disk-in-use")


def prepare(targets, opener=BlockDisk, release=release_holders):
    """Prove every target read-only, then release device-mapper stacks over them."""
    probes = []
    try:
        for target in targets:
            probes.append(opener(target, exclusive=False))
        if len({probe.dev for probe in probes}) != len(probes):
            raise Refusal("duplicate-device")
        release([node for probe in probes for node in probe.nodes()])
        for probe, target in zip(probes, targets):
            probe.identify(target)
    finally:
        for probe in probes:
            probe.close()


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
        if set(target) not in ({"deviceName", "method", "wwn", "serialNumber", "sizeBytes"}, {"method", "serialNumber", "sizeBytes"}):
            raise Refusal("target")
        if target["method"] not in METHODS:
            raise Refusal("target-method")
        for index, key in enumerate(("deviceName", "wwn", "serialNumber")):
            if key not in target:
                continue
            value = target[key]
            if not isinstance(value, str) or not value or len(value) > 256 or not value.isprintable() or value in seen[index]:
                raise Refusal("target-identity")
            seen[index].add(value)
        if "deviceName" in target and (not target["deviceName"].startswith("/dev/") or ".." in target["deviceName"].split("/")):
            raise Refusal("target-path")
        size = target["sizeBytes"]
        if not isinstance(size, int) or isinstance(size, bool) or size < 4096 or size % 4096 or size > 64 * 1024**4:
            raise Refusal("target-size")
        if target["method"] == "logicalOverwrite" and size > MAX_OVERWRITE:
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


class Pace:
    """One deadline, a shared stop, and the system proof and usage scan at most once per interval across every disk."""

    def __init__(self, request, prove_hardware, clock=time.monotonic, interval=1.0, scan=lambda: None):
        self.request, self.prove_hardware, self.clock, self.interval, self.scan = request, prove_hardware, clock, interval, scan
        self.deadline = clock() + request["deadlineSeconds"]
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.proved = None

    def prove(self, due=True):
        if self.clock() >= self.deadline:
            raise Refusal("deadline")
        with self.lock:
            if due or self.proved is None or self.clock() - self.proved >= self.interval:
                self.prove_hardware(self.request)
                self.scan()
                self.proved = self.clock()

    def checkpoint(self):
        if self.stop.is_set():
            raise Refusal("stopped")
        self.prove(due=False)


def concurrently(disks, targets, pace, run=overwrite):
    """Run every target on its own thread; the first failure stops the rest at their next checkpoint."""
    results, failures, guard = [None] * len(disks), [], threading.Lock()

    def work(index):
        try:
            results[index] = run(disks[index], targets[index], pace.checkpoint)
        except BaseException as error:
            with guard:
                failures.append(error)
            pace.stop.set()

    threads = []
    try:
        for index in range(len(disks)):
            thread = threading.Thread(target=work, args=(index,), daemon=True)
            thread.start()
            threads.append(thread)
    finally:
        if len(threads) != len(disks):
            pace.stop.set()
        # No descriptor closes while a thread may still write through it.
        for thread in threads:
            thread.join()
    if failures:
        raise failures[0]
    return results


def usage(disks):
    """One holders, mounts and swap pass over every target's nodes together."""
    unused([node for disk in disks for node in disk.nodes()])


def execute(request, directory, opener=BlockDisk, prove_hardware=hardware, release=prepare, clock=time.monotonic, scan=usage):
    validate_request(request)
    pace = Pace(request, prove_hardware, clock)
    pace.prove()
    release(request["targets"])
    pace.prove()
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
        # The exclusive holds already refuse new mounts and dm or md claims, so
        # the usage scan of every disk shares the paced system proof.
        pace.scan = lambda: scan(disks)
        pace.prove()
        coverage = concurrently(disks, request["targets"], pace)
        pace.prove()
        result = receipt_for(request, coverage)
        atomic(Path(directory) / "status.json", result)
        return result
    finally:
        for disk in disks:
            disk.close()
