"""Build the fixed inventory-only initramfs overlay; no installer entrypoint.

The kernel starts our PID1 before any storage activation or udev rules.
All executed code is in the verified initramfs. No block filesystem is mounted.
"""

from __future__ import annotations

import re
import inspect

from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline, bounded_process, canonical_json
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_worker, disk_inventory, physical_machine_identity
import stat

MAX_OVERLAY = 2 << 20
REQUIRED_INITRD = (
    "bin/sh",
    "usr/bin/mount",
    "usr/bin/python3",
    "usr/bin/findmnt",
    "usr/sbin/modprobe",
    "usr/sbin/ip",
    "usr/bin/sleep",
    "usr/bin/mkdir",
    "usr/bin/cp",
)
ARGUMENTS = (
    "rdinit=/init",
    "init=/bootwright-discovery/no-fallback",
    "root=/dev/bootwright-discovery-no-root",
    "rootfstype=bootwright-no-filesystem",
    "ro",
    "raid=noautodetect",
    "rd.auto=0",
    "rd.lvm=0",
    "rd.md=0",
    "rd.dm=0",
    "rd.luks=0",
    "systemd.gpt_auto=no",
    "rd.systemd.gpt_auto=no",
)

INIT = r"""#!/bin/sh
set -eu
export PATH=/usr/bin:/usr/sbin:/bin:/sbin LC_ALL=C
unset ENV BASH_ENV CDPATH LD_PRELOAD LD_LIBRARY_PATH PYTHONPATH PYTHONHOME
inert() {
    trap '' HUP INT TERM
    echo 'Bootwright discovery refused; no installation was started.' >/dev/console
    while :; do sleep 60; done
}
trap inert EXIT HUP INT TERM
mount -t proc -o nosuid,nodev,noexec proc /proc
mount -t sysfs -o nosuid,nodev,noexec sysfs /sys
mount -t devtmpfs -o nosuid devtmpfs /dev
mount -t tmpfs -o mode=0700,nosuid,nodev tmpfs /run
mkdir -p /run/discovery
printf '%s\n' /bin/false >/proc/sys/kernel/modprobe
# No modprobe configuration, install hook, udev daemon or storage generator.
for pass in 1 2 3; do
    count=0
    for file in /sys/bus/pci/devices/*/modalias /sys/bus/virtio/devices/*/modalias /sys/bus/scsi/devices/*/modalias /sys/bus/usb/devices/*/modalias; do
        [ -f "$file" ] || continue
        count=$((count + 1)); [ "$count" -le 4096 ] || inert
        [ ! -L "${file%/modalias}/driver" ] || continue
        read -r alias <"$file"
        case "$alias" in ''|*[!a-zA-Z0-9_:.*-]*) inert;; esac
        [ "${#alias}" -le 256 ] || inert
        modprobe --config /dev/null --ignore-install "$alias" || :
    done
    sleep 2
done
for device in /sys/bus/pci/devices/*; do
    [ -f "$device/class" ] || continue
    read -r class <"$device/class"
    case "$class" in 0x01*) [ -L "$device/driver" ] || inert;; esac
done
cp -R /bootwright-discovery/. /run/discovery/
python3 -I /run/discovery/disk_discovery_worker.py || inert
# PID1 reaps the synchronous worker and its process-group-owned children.
# The worker returns only after receipt acknowledgement and TLS cleanup.
echo o >/proc/sysrq-trigger
trap - EXIT HUP INT TERM
while :; do sleep 60; done
"""


def newc(files):
    """Encode only bounded regular files and explicit parents, without links."""
    entries = {"TRAILER!!!": (b"", 0)}
    for name, data, mode in files:
        if not re.fullmatch(r"[A-Za-z0-9_./-]+", name) or name.startswith("/") or any(p in ("", ".", "..") for p in name.split("/")):
            raise ValueError("initramfs path")
        if name in entries or not isinstance(data, bytes) or mode not in (0o600, 0o644, 0o700, 0o755):
            raise ValueError("initramfs entry")
        entries[name] = (data, stat.S_IFREG | mode)
        components = name.split("/")
        for length in range(1, len(components)):
            parent = "/".join(components[:length])
            entries.setdefault(parent, (b"", stat.S_IFDIR | 0o755))
    output = bytearray()
    ordered = sorted(name for name in entries if name != "TRAILER!!!") + ["TRAILER!!!"]
    for inode, name in enumerate(ordered, 1):
        data, mode = entries[name]
        encoded = name.encode("ascii") + b"\0"
        fields = (inode, mode, 0, 0, 1, 0, len(data), 0, 0, 0, 0, len(encoded), 0)
        output.extend(b"070701" + b"".join(("%08x" % value).encode() for value in fields))
        output.extend(encoded)
        output.extend(b"\0" * (-len(output) % 4))
        output.extend(data)
        output.extend(b"\0" * (-len(output) % 4))
        if len(output) > MAX_OVERLAY:
            raise ValueError("initramfs overlay exceeds bound")
    return bytes(output)


def overlay(payload):
    """Only trusted collection code and the private attempt enter the RAM image."""
    if not payload or any(not re.fullmatch(r"[a-z][a-z0-9_.-]{0,63}", name) for name in payload):
        raise ValueError("discovery image payload")
    files = [("init", INIT.encode("ascii"), 0o755)]
    files.extend(("bootwright-discovery/" + name, data, 0o600) for name, data in sorted(payload.items()))
    return newc(files)


def payload(binding, network, interfaces, token, certificate, private_key):
    """One exact source bundle is used by execution and offline qualification."""
    modules = (absolute_deadline, bounded_process, canonical_json, disk_inventory, physical_machine_identity, disk_discovery_worker)
    files = {module.__name__.rsplit(".", 1)[-1] + ".py": inspect.getsource(module).encode("utf-8") for module in modules}
    files["request.json"] = canonical_json.canonical({"binding": binding, "interfaces": interfaces, "network": network, "reportToken": token})
    files["server.pem"], files["server-key.pem"] = certificate, private_key
    return files
