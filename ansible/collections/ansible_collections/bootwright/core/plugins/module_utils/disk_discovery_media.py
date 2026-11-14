"""Remaster only a qualified x86 RHEL installer into inventory-only media."""

from __future__ import annotations

import hashlib
import errno
import fcntl
from contextlib import contextmanager
import os
from pathlib import Path
import shlex
import stat
import re
import posixpath
import shutil

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import private_records

ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
MEDIA = Path("/var/lib/bootwright/media")
# Exact integer checks reject Boolean sizes and process statuses.
# pylint: disable=unidiomatic-typecheck
MAX_INITRD = 256 << 20
MAX_SOURCE = 32 << 30
MAX_BOOT_GROWTH = 64 << 20
SUPPORTED_INITRDS = frozenset(("/images/pxeboot/initrd.img", "/isolinux/initrd.img"))


class CommandFailure(ValueError):
    """Only a fixed tool name and its exit status may leave this boundary."""

    def __init__(self, tool, code):
        self.tool = tool if tool in ("xorriso", "lsinitrd", "mkksiso", "openssl", "chcon") else "unknown"
        self.code = code if type(code) is int and -128 <= code <= 255 else -1
        super().__init__("discovery native command refused")


def publication_bound(source_size):
    if type(source_size) is not int or not 0 < source_size <= MAX_SOURCE:
        raise ValueError("discovery source size bound")
    return source_size + len(SUPPORTED_INITRDS) * (MAX_INITRD + boot.MAX_OVERLAY) + MAX_BOOT_GROWTH


def native(argv, timeout=60, limit=65536, temporary=None):
    environment = dict(ENV)
    if temporary is not None:
        environment["TMPDIR"] = str(temporary)
    result = bounded_process.run(argv, timeout=timeout, limit=limit, environment=environment, capture_stderr=False)
    if result.code:
        raise CommandFailure(Path(argv[0]).name, result.code)
    return result.stdout


def digest_file(path, maximum):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= maximum:
        raise ValueError("discovery image component size")
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        while data := stream.read(1 << 20):
            digest.update(data)
    return digest.hexdigest()


def initrd_paths(held):
    output = native(["/usr/bin/xorriso", "-indev", held, "-find", "/", "-name", "*initrd*", "-exec", "echo"])
    paths = shlex.split(output.decode("ascii"))
    if not paths or len(paths) != len(set(paths)) or set(paths) - SUPPORTED_INITRDS:
        raise ValueError("unqualified discovery initrd layout")
    return sorted(paths)


def extract(held, source, destination):
    native(["/usr/bin/xorriso", "-osirrox", "on", "-indev", held, "-extract", source, str(destination)], timeout=300)


def qualified_initrd(path):
    # The qualified archive supplies the shell and fixed tools our PID1 uses.
    # Listing performs no extraction or execution of source-image content.
    listing = native(["/usr/bin/lsinitrd", str(path)], timeout=120, limit=1 << 20).decode("ascii")
    names = set()
    links = {}
    for line in listing.splitlines():
        match = re.match(r"^[dl-][rwxStTs-]{9}\s+\d+\s+\S+\s+\S+\s+\d+\s+\S+\s+\d+\s+[0-9:]+\s+(.+)", line)
        if not match:
            continue
        path, separator, target = match.group(1).partition(" -> ")
        path = path.removeprefix("./")
        names.add(path)
        if separator:
            links[path] = target

    def resolve(path):
        for unused in range(16):
            parts = path.split("/")
            for length in range(1, len(parts) + 1):
                prefix = "/".join(parts[:length])
                if prefix in links:
                    target = links[prefix]
                    path = posixpath.normpath(posixpath.join(posixpath.dirname(prefix), target, *parts[length:]))
                    break
            else:
                return path
        raise ValueError("initrd link cycle")

    names.update(path for path in boot.REQUIRED_INITRD if resolve(path) in names)
    if not set(boot.REQUIRED_INITRD) <= names:
        raise ValueError("RHEL initrd lacks discovery bootstrap tools")


def build(request, root, payload):
    media = request["bootMedia"]
    source = MEDIA / media["name"]
    if source.parent != MEDIA:
        raise ValueError("discovery source image path")
    private_records.directory(MEDIA, False, None)
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size != media["size"]:
            raise ValueError("discovery source image identity")
        digest = hashlib.sha256()
        while data := os.read(fd, 1 << 20):
            digest.update(data)
        if digest.hexdigest() != media["sha256"]:
            raise ValueError("discovery source image digest")
        held = "/proc/" + str(os.getpid()) + "/fd/" + str(fd)
        return remaster(held, root, payload)
    finally:
        os.close(fd)


def remaster(held, root, payload):
    reap_intermediates(root)
    scratch = root / "media-work"
    private_records.directory(scratch, True)
    image = root / "discovery.iso"
    complete = False
    try:
        additions = patched_initrds(held, scratch, payload)
        # Lorax merges these directories and updates BIOS, GRUB and embedded
        # EFI configuration in one supported --add operation. No intermediate ISO.
        native(
            [
                "/usr/bin/mkksiso",
                "--cmdline", " ".join(boot.ARGUMENTS),
                "--rm-args",
                "init rdinit root rootfstype inst.stage2 inst.ks inst.rescue rd.break rd.live.overlay rd.overlay rootflags rd.writable.fsimg",
                *[value for path in additions for value in ("--add", str(path))],
                held, str(image),
            ],
            timeout=900, temporary=scratch,
        )
        complete = True
        return image
    finally:
        reap_intermediates(root)
        if not complete:
            image.unlink(missing_ok=True)


def patched_initrds(held, scratch, payload):
    overlay = boot.overlay(payload)
    paths = initrd_paths(held)
    additions = set()
    for path in paths:
        original = scratch / path.lstrip("/")
        for parent in reversed(original.relative_to(scratch).parents):
            if parent != Path("."):
                private_records.directory(scratch / parent, True)
        extract(held, path, original)
        digest_file(original, MAX_INITRD)
        qualified_initrd(original)
        with original.open("ab") as destination:
            # A compressed initramfs followed by an uncompressed newc archive
            # is the kernel's supported concatenated initramfs format.
            destination.write(b"\0" * (-destination.tell() % 4))
            destination.write(overlay)
        additions.add(scratch / original.relative_to(scratch).parts[0])
    return sorted(additions)


def reap_intermediates(root):
    scratch = root / "media-work"
    if scratch.exists() or scratch.is_symlink():
        private_records.directory(scratch, False)
        # Linux's descriptor-based rmtree never follows a substituted link.
        if not shutil.rmtree.avoids_symlink_attacks:
            raise ValueError("discovery scratch cleanup is unsupported")
        shutil.rmtree(scratch)
    for name in ("patched.iso", "initrd-0", "initrd-1"):
        (root / name).unlink(missing_ok=True)


@contextmanager
def construction(lock_root, work, destination, source_size):
    # Serialize only construction/publication across contexts. Published images
    # remain allocated while other nodes boot, so the next check includes them.
    private_records.directory(lock_root, False)
    fd = os.open(lock_root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        maximum = publication_bound(source_size)
        intermediates = len(SUPPORTED_INITRDS) * (MAX_INITRD + boot.MAX_OVERLAY) + MAX_BOOT_GROWTH
        budgets = {work.stat().st_dev: (work, maximum + intermediates)}
        if destination.stat().st_dev not in budgets:
            budgets[destination.stat().st_dev] = (destination, maximum)
        for path, required in budgets.values():
            capacity = os.statvfs(path)
            if capacity.f_bavail * (capacity.f_frsize or capacity.f_bsize) < required:
                raise OSError(errno.ENOSPC, "discovery construction capacity")
        yield
    finally:
        os.close(fd)
