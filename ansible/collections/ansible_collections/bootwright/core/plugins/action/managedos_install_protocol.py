"""Emit the installation capability protocol on the runner's result channel.

This runs on the controller, never the artifact server's placement host,
because the inherited result and authorization descriptors belong to the
invoking Bootwright process.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

# The refusals this installation names to its runner before the run fails,
# each one the runner reports as the installation's own diagnostic for its
# Machine: those of the target's pre-boot proof (internal/substrate,
# preboot.go), and those of a store entry that no longer has the size and
# SHA-256 the operation froze (internal/managedos/installation, selection.go).
REFUSALS = ("hardware-mismatch", "identity-mismatch", "machine-running")
MEDIA_REFUSALS = ("media-changed-boot", "media-changed-tree")
POWER_STATES = ("", "On", "Off")
MAX_MARKER = 4096
MAX_HOST_KEY = 4096
# Every piece of content this installation publishes, as its inspection names
# it; treeContent is anything left at the package tree's path, which a removal
# stopped part way can leave without the marker that makes the tree complete.
# treeStaging and work are what an attempt killed part way leaves: the tree it
# was extracting beneath the served root and the area it built the image in.
CONTENT = ("image", "private", "tree", "treeContent", "treeStaging", "work")


def tree_identity(value):
    """The digest of the image the published tree was extracted from, or ''."""
    value = value or ""
    if value != "" and (not isinstance(value, str) or len(value) != 64 or set(value) - adapter_protocol.HEX):
        raise ValueError("tree identity")
    return value


def bounded(value, limit):
    value = str(value or "").strip()
    if len(value) > limit:
        raise ValueError("bounded value")
    return value


def flag(value):
    """A boolean argument, whether templating kept its type or rendered it."""
    if isinstance(value, bool):
        return value
    return str(value or "").strip().lower() in ("true", "yes", "1")


def presence(arguments, request_digest):
    """The evidence a completion proves.

    A private installation's image names the private URL in its Kickstart, so
    its completion has withdrawn the image with the key pair and proves
    neither published; a public installation's image stays in place.
    """
    observation = arguments.get("observation") or {}
    private_delivery = flag(arguments.get("privateDelivery"))
    power = arguments.get("power")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "address": bounded(arguments.get("address"), 128),
        "hostKey": bounded(arguments.get("hostKey"), MAX_HOST_KEY),
        "image": bool(observation.get("image")),
        "marker": bounded(arguments.get("marker"), MAX_MARKER),
        "media": bounded(arguments.get("media"), 1024),
        "postcondition": False,
        "power": str(power),
        "private": bool(observation.get("private")),
        "reachable": bool(arguments.get("reachable")),
        "request": adapter_protocol.request_digest(request_digest),
        "tree": bool(observation.get("tree")),
        "treeContent": bool(observation.get("treeContent")),
        "treeIdentity": tree_identity(observation.get("treeIdentity")),
        "treeStaging": bool(observation.get("treeStaging")),
        "work": bool(observation.get("work")),
    }
    evidence["postcondition"] = bool(
        evidence["marker"] and evidence["hostKey"] and evidence["address"]
        and not evidence["media"] and evidence["power"] == "On"
        and ((not evidence["image"]) if private_delivery else evidence["image"])
        and not evidence["private"] and evidence["reachable"]
    )
    return evidence


def remaining(arguments):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    observation = arguments.get("observation") or {}
    return [name for name in CONTENT if observation.get(name)]


def unproved(evidence, private_delivery=False):
    """What a completion proof still lacks, named for the same reason."""
    names = [name for name in ("marker", "hostKey", "address") if not evidence[name]]
    if evidence["media"]:
        names.append("media")
    if evidence["private"]:
        names.append("private material still published")
    if evidence["power"] != "On":
        names.append("power")
    if private_delivery and evidence["image"]:
        names.append("private installer image still published")
    if not private_delivery and not evidence["image"]:
        names.append("image")
    if not evidence["reachable"]:
        names.append("reachable")
    return names


def absence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    gone = not any(observation.get(name) for name in CONTENT)
    return {
        "absent": True,
        "address": "",
        "hostKey": "",
        "image": False,
        "marker": "",
        "media": "",
        "postcondition": bool(gone),
        "power": "",
        "private": False,
        "reachable": False,
        "request": adapter_protocol.request_digest(request_digest),
        "tree": False,
        "treeContent": False,
        "treeIdentity": "",
        "treeStaging": False,
        "work": False,
    }


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet."""
    request_digest = arguments.get("digest")
    if arguments.get("removed"):
        evidence = absence(arguments, request_digest)
        unmet, verb = remaining(arguments), "still present"
    else:
        evidence = presence(arguments, request_digest)
        unmet, verb = unproved(evidence, flag(arguments.get("privateDelivery"))), "not proved"
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, unreached("the installation", verb, unmet)


CAPABILITY = adapter_protocol.Capability(
    "installation",
    "the installation capability result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS + MEDIA_REFUSALS,
)
PHASES = CAPABILITY.phases


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
