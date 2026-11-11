"""Publish the artifact mirror fill's bounded lifecycle evidence."""
from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_FILES = 128


def file_evidence(requested, stat):
    """One requested file as its stat found it.

    It is held only as a regular file the stat did not follow a link to, and
    verified only at its pinned size and SHA-256.
    """
    if not isinstance(requested, dict) or not isinstance(stat, dict):
        raise ValueError("mirror file")
    path, digest, size = requested.get("path"), requested.get("sha256"), requested.get("bytes")
    if not isinstance(path, str) or not isinstance(digest, str) or isinstance(size, bool) or not isinstance(size, int):
        raise ValueError("mirror file")
    if not stat.get("exists"):
        return None
    held = bool(stat.get("isreg")) and not stat.get("islnk", True)
    return {"path": path, "verified": held and stat.get("size") == size and stat.get("checksum") == digest}


def completion(arguments):
    files, stats, root = arguments.get("files") or [], arguments.get("stats") or [], arguments.get("root") or {}
    if not isinstance(files, list) or not isinstance(stats, list) or len(files) != len(stats) or len(files) > MAX_FILES or not isinstance(root, dict):
        raise ValueError("mirror files")
    held = [entry for entry in (file_evidence(requested, stat) for requested, stat in zip(files, stats)) if entry is not None]
    removed, absent = bool(arguments.get("removed")), not root.get("exists")
    if removed:
        postcondition = not held
    else:
        postcondition = bool(files) and len(held) == len(files) and all(entry["verified"] for entry in held)
    evidence = {"absent": absent, "files": held, "postcondition": postcondition,
                "request": adapter_protocol.request_digest(arguments.get("digest")), "version": "mirror-files-evidence-v1"}
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the artifact mirror files", "not proved", ["files"])


# The refusals this publication names to its runner before the run fails, each
# one the runner reports as the fill's own diagnostic for its artifact server
# (internal/infrastructureservices/mirrorfill, capability.go): a publisher file
# whose SHA-256 is not the pinned one, and a publisher the host could not read.
REFUSALS = ("file-digest-mismatch", "file-unreachable")

CAPABILITY = adapter_protocol.Capability(
    "mirror-files",
    "the artifact mirror files result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
