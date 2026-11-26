"""Blank one kept machine's root disk under the frozen request, and read its mark.

The domain, its MACs and UUID, its controller and every data disk stay as the
apply left them: only the root image is created again, empty, at its frozen
path and size, the way the apply first created it. The mark beside it names
the staged destroy and the step whose removal blanked it, so an observation
tells a completed reset from one that never ran.
"""

from __future__ import annotations

import os
import re
import stat
from xml.etree import ElementTree

from ansible_collections.bootwright.core.plugins.module_utils import substrate_libvirt

MARK_NAME = "root-reset.mark"
MARK_LIMIT = 256
# A mark is `<operation>/<step>`: the staged destroy's operation identity and the step's block identity.
MARK = re.compile(r"op-[0-9a-f]{32}/[a-z0-9](?:[a-z0-9-]{0,251}[a-z0-9])?\Z")
NAME = re.compile(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\Z")
SHUT_OFF = "shut off"
# The media pool every libvirt request names; a Machine's disks lie in its own directory beneath it, as the apply derives it.
IMAGES = "/var/lib/libvirt/images/bootwright/"
ROOT_IMAGE = "root.qcow2"
ROOT_TARGET = "vda"


class Refused(Exception):
    """A reset the observed machine does not admit, named for the operator."""


def mark_of(removal, step):
    mark = "%s/%s" % (removal, step)
    if not MARK.fullmatch(mark):
        raise ValueError("root reset mark")
    return mark


def root_disk(request):
    disks = request.get("disks") or []
    if not disks or disks[0].get("name") != "root":
        raise ValueError("root disk")
    return disks[0]


def mark_path(request):
    directory = request["directory"]
    if not os.path.isabs(directory) or os.path.normpath(directory) != directory:
        raise ValueError("disk directory")
    return os.path.join(directory, MARK_NAME)


def read_mark(request):
    """The mark beside the root disk, or the empty string when none reads as one."""
    path = mark_path(request)
    if os.path.islink(path) or not os.path.isfile(path):
        return ""
    with open(path, "rb") as handle:
        content = handle.read(MARK_LIMIT + 1)
    text = content.decode("ascii", "replace").strip()
    return text if len(content) <= MARK_LIMIT and MARK.fullmatch(text) else ""


def observe(runner, request):
    """The domain identity and state, the root disk and its mark, changing nothing."""
    identity = request["identity"]
    metadata = substrate_libvirt.domain_metadata(
        runner, request["uri"], request["domain"], identity["context"], identity["object"], request["uuid"], request.get("disks"))
    root = root_disk(request)
    return {
        "absent": metadata["answered"] and not metadata["present"],
        "answered": metadata["answered"],
        "disks": [{"name": root["name"], "present": os.path.lexists(root["path"]), "sizeGiB": substrate_libvirt.disk_size_gib(runner, root["path"])}],
        "domain": request["domain"] if metadata["present"] else "",
        "mark": read_mark(request),
        "owned": metadata["owned"],
        "state": substrate_libvirt.domain_state(runner, request["uri"], request["domain"]) if metadata["owned"] else "",
        "uuid": metadata["uuid"],
    }


def completed(observation, request, mark):
    """Whether the root disk reads blank under this mark: present at its frozen size, its own domain shut off."""
    root = root_disk(request)
    disk = observation["disks"][0]
    return (observation["answered"] and observation["owned"] and observation["state"] == SHUT_OFF and observation["mark"] == mark
            and disk["present"] and disk["sizeGiB"] == root["sizeGiB"])


def root_source(runner, uri, name):
    """The file the domain's root target reads, or the empty string when its definition names no one file there."""
    code, output = substrate_libvirt.virsh(runner, uri, "dumpxml", name)
    if code != 0:
        return ""
    try:
        definition = ElementTree.fromstring(output)
    except ElementTree.ParseError:
        return ""
    found = [item for item in definition.findall("./devices/disk[@device='disk']")
             if item.find("target") is not None and item.find("target").get("dev") == ROOT_TARGET]
    if len(found) != 1 or found[0].get("type") != "file" or found[0].find("source") is None:
        return ""
    return found[0].find("source").get("file", "")


def prove_target(runner, request):
    """Refuse a root disk that is not the one the apply created for this context's Machine.

    The disk directory is the one the apply derives from the frozen context and
    Machine and resolves to itself; the root image is `root.qcow2` in it, a
    regular file or absent, never a link; and the domain's root target reads
    exactly that image.
    """
    identity = request.get("identity") or {}
    context, machine = identity.get("context"), identity.get("object")
    if not isinstance(context, str) or not isinstance(machine, str) or not NAME.fullmatch(context) or not NAME.fullmatch(machine):
        raise Refused("the frozen request names no context and Machine of its own, so its root disk was not reset")
    directory = request.get("directory")
    if directory != IMAGES + context + "/machines/" + machine:
        raise Refused("the frozen disk directory is not the Machine's own, so its root disk was not reset")
    root = root_disk(request)
    if root.get("path") != os.path.join(directory, ROOT_IMAGE) or root.get("target") != ROOT_TARGET:
        raise Refused("the frozen root disk is not the Machine's own image, so it was not reset")
    if os.path.realpath(directory) != directory:
        raise Refused("the disk directory resolves elsewhere, so the root disk was not reset")
    try:
        mode = os.lstat(root["path"]).st_mode
    except FileNotFoundError:
        mode = None
    if mode is not None and not stat.S_ISREG(mode):
        raise Refused("the root disk is not a regular file, so it was not reset")
    if root_source(runner, request["uri"], request["domain"]) != root["path"]:
        raise Refused("the domain's root target does not read the frozen root disk, so it was not reset")


def write_mark(request, mark):
    path = mark_path(request)
    staged = path + ".tmp"
    if os.path.lexists(staged):
        os.unlink(staged)
    descriptor = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="ascii") as handle:
        handle.write(mark + "\n")
        handle.flush()
        os.fsync(handle.fileno())
    os.replace(staged, path)


def admitted(runner, request):
    """The observation a reset starts from, once the domain is this Machine's own, shut off, over its own root image."""
    prove_target(runner, request)
    before = observe(runner, request)
    if not before["answered"]:
        raise Refused("the hypervisor did not answer for the domain, so its root disk was not reset")
    if not before["domain"] or not before["owned"]:
        raise Refused("the domain is not this context's own Machine, so its root disk was not reset")
    if before["state"] != SHUT_OFF:
        raise Refused("the domain is not shut off, so its root disk was not reset")
    return before


def reset(runner, request, removal, step):
    """Blank the root disk once, marking it; return the observation and whether it changed.

    Only this context's own domain, shut off, is touched, and only its own root
    image. A disk that already carries this removal's mark at its frozen size
    is left as it is, so a repeated reset changes nothing. Otherwise any
    earlier mark goes first, so a reset cut short never leaves a mark over a
    disk it did not blank, and the mark is written only once the blank disk
    reads at its frozen size under a domain still shut off.
    """
    mark = mark_of(removal, step)
    before = admitted(runner, request)
    if completed(before, request, mark):
        return before, False
    root = root_disk(request)
    path = mark_path(request)
    if os.path.lexists(path):
        os.unlink(path)
    if os.path.lexists(root["path"]):
        os.unlink(root["path"])
    code, _output = substrate_libvirt.invoke(runner, [substrate_libvirt.QEMU_IMG, "create", "-f", "qcow2", root["path"], "%dG" % int(root["sizeGiB"])])
    if code != 0:
        raise Refused("qemu-img could not create the blank root disk")
    created = observe(runner, request)
    if created["state"] != SHUT_OFF:
        raise Refused("the domain did not stay shut off while its root disk was blanked, so the reset was not marked")
    if created["disks"][0]["sizeGiB"] != root["sizeGiB"]:
        raise Refused("the blank root disk does not read at its frozen size")
    write_mark(request, mark)
    return observe(runner, request), True
