"""Remaster only a qualified x86 RHEL installer into inventory-only media.

The inventory boot never starts the installer, so its image keeps only the
boot tree: no second stage, package repositories or other installer content.
"""

from __future__ import annotations

import hashlib
from contextlib import contextmanager
import os
from pathlib import Path
import shlex
import stat
import re
import posixpath
import shutil

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import discovery_capacity
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_ssh_sources as ssh_sources
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
BOOT_TREE = {"/": frozenset((".discinfo", ".treeinfo", "EFI", "images", "isolinux")), "/images": frozenset(("efiboot.img", "pxeboot"))}
MAX_REMOVED = 64
# The fixed native tools a failure may name; any other is reported as unknown.
TOOLS = ("xorriso", "lsinitrd", "mkksiso", "ssh-keygen", "unsquashfs", "debugfs", "readelf", "chcon")


class CommandFailure(ValueError):
    """Only a fixed tool name and its exit status may leave this boundary."""

    def __init__(self, tool, code):
        self.tool = tool if tool in TOOLS else "unknown"
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


def outside_boot_tree(held):
    """Every top-level or /images entry the inventory boot never reads."""
    removed = []
    for directory, kept in sorted(BOOT_TREE.items()):
        output = native(["/usr/bin/xorriso", "-indev", held, "-find", directory, "-maxdepth", "1", "-exec", "echo"])
        for entry in shlex.split(output.decode("ascii")):
            if entry != directory and posixpath.dirname(entry) == directory and posixpath.basename(entry) not in kept:
                removed.append(entry)
    if len(removed) > MAX_REMOVED or "/images/install.img" not in removed:
        raise ValueError("unqualified discovery initrd layout")
    return removed


def boot_tree(held, scratch):
    """Copy only the boot tree, replaying both El Torito boot records."""
    image = scratch / "boot-tree.iso"
    native(["/usr/bin/xorriso", "-indev", held, "-outdev", str(image), "-boot_image", "any", "replay", "-rm_r", *outside_boot_tree(held), "--"],
           timeout=900, temporary=scratch)
    return str(image)


def extract(held, source, destination):
    native(["/usr/bin/xorriso", "-osirrox", "on", "-indev", held, "-extract", source, str(destination)], timeout=300)


def qualified_initrd(path, libraries=()):
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

    required = (*boot.REQUIRED_INITRD, "lib64/ld-linux-x86-64.so.2", *("lib64/" + name for name in libraries))
    names.update(path for path in required if resolve(path) in names)
    if not set(required) <= names:
        raise ValueError("RHEL initrd lacks discovery bootstrap tools")


@contextmanager
def verified_source(request):
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
        yield held
    finally:
        os.close(fd)


def remaster(held, root, payload, source_profile):
    reap_intermediates(root)
    scratch = root / "media-work"
    private_records.directory(scratch, True)
    image = root / "discovery.iso"
    complete = False
    try:
        runtime, libraries = ssh_sources.extract(held, source_profile, scratch, native)
        payload = dict(payload, **runtime)
        held = boot_tree(held, scratch)
        additions = patched_initrds(held, scratch, payload, libraries)
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


def patched_initrds(held, scratch, payload, libraries):
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
        qualified_initrd(original, libraries)
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
def construction(lock_root, work, destination, source_size, ssh_peak=0):
    maximum = publication_bound(source_size)
    # Reserve each peak, while previous publications still count in statvfs.
    intermediates = source_size + len(SUPPORTED_INITRDS) * (MAX_INITRD + boot.MAX_OVERLAY) + MAX_BOOT_GROWTH
    budgets = {work.stat().st_dev: (work, max(maximum + intermediates + ssh_sources.MAX_BUNDLE, ssh_peak))}
    if destination.stat().st_dev not in budgets:
        budgets[destination.stat().st_dev] = (destination, maximum)
    with discovery_capacity.reserve(lock_root, budgets):
        yield
