"""Read the qualified RHEL SSH runtime from the held ISO, never executing it."""

from __future__ import annotations

import os
import posixpath
import re
import shutil
import stat

from ansible_collections.bootwright.core.plugins.module_utils import private_records as records
from ansible_collections.bootwright.core.plugins.module_utils import ssh_key_policy

MAX_ROOTFS = 8 << 30
MAX_COMPONENT = 4 << 20
MAX_BUNDLE = 8 << 20
PATHS = {
    "sshd": "/usr/sbin/sshd",
    "sshd-session": "/usr/libexec/openssh/sshd-session",
    "sshd-auth": "/usr/libexec/openssh/sshd-auth",
}
TOOLS = {"unsquashfs": ("/usr/bin/unsquashfs", "/usr/sbin/unsquashfs"),
         "debugfs": ("/usr/sbin/debugfs",), "readelf": ("/usr/bin/readelf",)}


def tool(name):
    for candidate in TOOLS[name]:
        try:
            info = os.stat(candidate)
        except FileNotFoundError:
            continue
        if stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022 and info.st_mode & 0o111:
            return candidate
        raise ValueError("discovery SSH source tool")
    raise ValueError("discovery SSH source tool")


def inspect(held, source_size, native):
    text = native(["/usr/bin/xorriso", "-indev", held, "-find", "/images/install.img", "-maxdepth", "0", "-exec", "report_lba", "--"]).decode("ascii")
    rows = re.findall(r"^File data lba:\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*, '/images/install.img'\s*$", text, re.M)
    if len(rows) != 1:
        raise ValueError("discovery SSH source layout")
    section, lba, blocks, size = map(int, rows[0])
    offset = lba * 2048
    if section != 0 or not 0 < size <= source_size or blocks != (size + 2047) // 2048 or offset + blocks * 2048 > source_size:
        raise ValueError("discovery SSH source extent")
    text = native([tool("unsquashfs"), "-offset", str(offset), "-ll", held, "LiveOS/rootfs.img"], timeout=120, limit=1 << 20).decode("ascii")
    sizes = re.findall(r"^-[rwxStTs-]{9}\s+\S+/\S+\s+(\d+)\s+\S+\s+\S+\s+squashfs-root/LiveOS/rootfs.img\s*$", text, re.M)
    if len(sizes) != 1 or not 0 < int(sizes[0]) <= MAX_ROOTFS:
        raise ValueError("discovery SSH source filesystem")
    return {"offset": offset, "size": size, "rootSize": int(sizes[0])}


def read_file(image, path, native, maximum=MAX_COMPONENT, optional=False):
    for _unused in range(16):
        if re.fullmatch(r"/[A-Za-z0-9_./+-]+", path) is None:
            raise ValueError("discovery SSH source path")
        text = native([tool("debugfs"), "-R", "stat " + path, str(image)], limit=8192).decode("ascii")
        if not text and optional:
            return None
        info = re.search(r"^Inode:\s*\d+\s+Type:\s*(regular|symlink)\s+Mode:\s*0([0-7]+)", text, re.M)
        size = re.search(r"\bSize:\s*(\d+)", text)
        if info is None or size is None or not 0 < int(size[1]) <= maximum:
            raise ValueError("discovery SSH source file")
        if info[1] == "symlink":
            link = re.search(r'^Fast link dest: "([^"\n]+)"$', text, re.M)
            if link is None:
                raise ValueError("discovery SSH source link")
            path = posixpath.normpath(posixpath.join(posixpath.dirname(path), link[1]))
            continue
        data = native([tool("debugfs"), "-R", "cat " + path, str(image)], limit=maximum)
        if len(data) != int(size[1]):
            raise ValueError("discovery SSH source file")
        return data
    raise ValueError("discovery SSH source link")


def libraries(path, native):
    text = native([tool("readelf"), "-h", "-l", "-d", str(path)], limit=65536).decode("ascii")
    if (re.search(r"Class:\s+ELF64", text) is None or "little endian" not in text
            or "Advanced Micro Devices X86-64" not in text or "(RPATH)" in text or "(RUNPATH)" in text):
        raise ValueError("discovery SSH source ELF")
    interpreters = re.findall(r"\[Requesting program interpreter: ([^\]]+)\]", text)
    needed = re.findall(r"\(NEEDED\).*Shared library: \[([^\]]+)\]", text)
    if (interpreters != ["/lib64/ld-linux-x86-64.so.2"] or not 1 <= len(needed) <= 64
            or any(re.fullmatch(r"[A-Za-z0-9_.+-]{1,128}", name) is None for name in needed)):
        raise ValueError("discovery SSH source ELF")
    return set(needed)


def secure_rootfs(destination, size):
    for directory in (destination, destination / "LiveOS"):
        held = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            if os.fstat(held).st_uid != 0:
                raise ValueError("discovery SSH source ownership")
            os.fchmod(held, 0o700)
        finally:
            os.close(held)
    image = destination / "LiveOS/rootfs.img"
    held = os.open(image, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(held)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1 or info.st_size != size:
            raise ValueError("discovery SSH source filesystem")
        os.fchmod(held, 0o600)
    finally:
        os.close(held)
    return image


def extract(held, profile, scratch, native):
    if not shutil.rmtree.avoids_symlink_attacks:
        raise ValueError("discovery SSH scratch cleanup")
    destination = scratch / "ssh-source"
    records.directory(scratch, False)
    # Let unsquashfs create its destination. Older host tools refuse an existing
    # directory without overwrite; the proven private parent protects creation.
    if os.path.lexists(destination):
        raise ValueError("discovery SSH source filesystem")
    try:
        native([tool("unsquashfs"), "-no-xattrs", "-no-progress", "-processors", "1", "-offset", str(profile["offset"]),
                "-d", str(destination), held, "LiveOS/rootfs.img"], timeout=600, limit=65536)
        image = secure_rootfs(destination, profile["rootSize"])
        files, needed = {}, set()
        for name, path in PATHS.items():
            data = read_file(image, path, native, optional=name != "sshd")
            if data is None:
                continue
            if not data.startswith(b"\x7fELF"):
                raise ValueError("discovery SSH source ELF")
            component = destination / name
            records.write(component, data)
            needed.update(libraries(component, native))
            files[name] = data
        for name, path in PATHS.items():
            if name not in files and any(path.encode("ascii") + b"\0" in data for data in files.values()):
                raise ValueError("discovery SSH source helper")
        policy = read_file(image, "/etc/crypto-policies/back-ends/opensshserver.config", native, 65536).decode("ascii")
        files["ssh-policy"] = ssh_key_policy.configuration(policy).encode("ascii")
        if sum(map(len, files.values())) > MAX_BUNDLE:
            raise ValueError("discovery SSH source size")
        return files, needed
    finally:
        # This is one newly created private subtree, never a mounted filesystem.
        # Linux's descriptor-based rmtree refuses substituted directory links.
        if destination.exists():
            shutil.rmtree(destination)
