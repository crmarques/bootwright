"""Publish the image mirror fill's bounded lifecycle evidence."""
from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_IMAGES = 64


def held_image(requested, held):
    """One held image, which must be the frozen request's image at its order's place."""
    if not isinstance(held, dict) or not isinstance(requested, dict):
        raise ValueError("mirror image")
    repository, digest = held.get("repository"), held.get("digest")
    if not isinstance(repository, str) or not isinstance(digest, str):
        raise ValueError("mirror image")
    if (repository, digest) not in requested:
        raise ValueError("mirror image")
    return {"repository": repository, "digest": digest, "verified": held.get("verified") is True}


def completion(arguments):
    images, held = arguments.get("images") or [], arguments.get("held") or []
    if not isinstance(images, list) or not isinstance(held, list) or len(images) > MAX_IMAGES or len(held) > len(images):
        raise ValueError("mirror images")
    requested = {}
    for image in images:
        if not isinstance(image, dict):
            raise ValueError("mirror images")
        requested[(image.get("repository"), image.get("digest"))] = image
    entries = [held_image(requested, entry) for entry in held]
    removed = bool(arguments.get("removed"))
    if removed:
        postcondition = not entries
    else:
        postcondition = bool(images) and len(entries) == len(images) and all(entry["verified"] for entry in entries)
    evidence = {"absent": removed and not entries, "images": entries, "postcondition": postcondition,
                "request": adapter_protocol.request_digest(arguments.get("digest")), "version": "mirror-images-evidence-v1"}
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the image mirror", "not proved", ["images"])


# The refusals this publication names to its runner before the run fails, each
# one the runner reports as the fill's own diagnostic for its Registry
# (internal/infrastructureservices/mirrorfill, images.go): no skopeo on the
# mirror host, a digest the mirror holds other than the pinned one, an origin
# the host could not copy from, and a mirror it could not read.
REFUSALS = ("skopeo-missing", "image-digest-mismatch", "image-unreachable", "mirror-unreadable")

CAPABILITY = adapter_protocol.Capability(
    "mirror-images",
    "the image mirror result could not be published",
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
