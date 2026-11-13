"""Remaster only a qualified x86 RHEL installer into inventory-only media."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shlex
import stat
import re
import posixpath

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import private_records

ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
MEDIA = Path("/var/lib/bootwright/media")
# Exact integer checks reject Boolean sizes and process statuses.
# pylint: disable=unidiomatic-typecheck
MAX_INITRD = 256 << 20


class CommandFailure(ValueError):
    """Only a fixed tool name and its exit status may leave this boundary."""

    def __init__(self, tool, code):
        self.tool = tool if tool in ("xorriso", "lsinitrd", "mkksiso", "openssl", "chcon") else "unknown"
        self.code = code if type(code) is int and -128 <= code <= 255 else -1
        super().__init__("discovery native command refused")


def native(argv, timeout=60, limit=65536):
    result = bounded_process.run(argv, timeout=timeout, limit=limit, environment=ENV, capture_stderr=False)
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
    if not paths or len(paths) != len(set(paths)) or set(paths) - {"/images/pxeboot/initrd.img", "/isolinux/initrd.img"}:
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
    overlay = boot.overlay(payload)
    paths = initrd_paths(held)
    arguments = ["/usr/bin/xorriso", "-indev", held, "-outdev", str(root / "patched.iso")]
    for index, path in enumerate(paths):
        original = root / ("initrd-" + str(index))
        extract(held, path, original)
        digest_file(original, MAX_INITRD)
        qualified_initrd(original)
        with original.open("ab") as destination:
            # A compressed initramfs followed by an uncompressed newc archive
            # is the kernel's supported concatenated initramfs format.
            destination.write(b"\0" * (-destination.tell() % 4))
            destination.write(overlay)
        arguments.extend(("-map", str(original), path))
    arguments.extend(("-boot_image", "any", "replay", "-commit"))
    native(arguments, timeout=900)
    # Lorax updates the BIOS/GRUB configurations and the embedded EFI image.
    native(
        [
            "/usr/bin/mkksiso",
            "--cmdline",
            " ".join(boot.ARGUMENTS),
            "--rm-args",
            "init rdinit root rootfstype inst.stage2 inst.ks inst.rescue rd.break rd.live.overlay rd.overlay rootflags rd.writable.fsimg",
            str(root / "patched.iso"),
            str(root / "discovery.iso"),
        ],
        timeout=900,
    )
    return root / "discovery.iso"
