"""Publish the release fill's bounded lifecycle evidence."""
from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_COMPONENTS = 512
DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
NAME = re.compile(r"[a-z0-9]([a-z0-9._-]*[a-z0-9])?\Z")


def component(entry):
    """One component the mirror holds, by its name and digest."""
    if not isinstance(entry, dict):
        raise ValueError("mirror release component")
    name, digest = entry.get("name"), entry.get("digest")
    if not isinstance(name, str) or NAME.match(name) is None or len(name) > 128 or not isinstance(digest, str) or DIGEST.match(digest) is None:
        raise ValueError("mirror release component")
    return {"digest": digest, "name": name}


def completion(arguments):
    release, held = arguments.get("release"), arguments.get("held") or {}
    if not isinstance(release, dict) or not isinstance(held, dict):
        raise ValueError("mirror release")
    digest, missing, components = held.get("digest") or "", held.get("missing") or 0, held.get("components") or []
    if digest and DIGEST.match(digest) is None or not isinstance(missing, int) or isinstance(missing, bool) or not 0 <= missing <= MAX_COMPONENTS:
        raise ValueError("mirror release")
    if not isinstance(components, list) or len(components) > MAX_COMPONENTS:
        raise ValueError("mirror release")
    entries = [component(entry) for entry in components]
    removed = bool(arguments.get("removed"))
    pinned = release.get("digest") or ""
    if removed:
        postcondition = not digest and not entries
    else:
        postcondition = bool(digest) and bool(entries) and missing == 0 and (not pinned or digest == pinned)
    evidence = {"absent": removed and not digest and not entries, "components": entries, "digest": digest, "missing": missing,
                "postcondition": postcondition, "request": adapter_protocol.request_digest(arguments.get("digest")), "version": "mirror-release-evidence-v1"}
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the release copy", "not proved", ["digest", "components", "missing"])


# The refusals this publication names to its runner before the run fails, each
# one the runner reports as the fill's own diagnostic for its cluster
# (internal/infrastructureservices/mirrorfill, releases.go): a client archive
# the host could not read, or whose SHA-256 is not the publisher's, a copy that
# failed, a mirror lacking the release or a component after it, a release
# held at another digest than the pin, and a mirror it could not read.
REFUSALS = ("client-unreachable", "client-digest-mismatch", "release-unreachable", "release-incomplete", "release-digest-mismatch", "mirror-unreadable")

CAPABILITY = adapter_protocol.Capability(
    "mirror-release",
    "the release copy result could not be published",
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
