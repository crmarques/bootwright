"""Emit the installation capability protocol on the runner's result channel.

This runs on the controller, never the artifact server's placement host,
because the inherited result and authorization descriptors belong to the
invoking Bootwright process.
"""

from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.adapter_protocol import publishes, unreached
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)

# The refusals this installation names to its runner before the run fails,
# each one the runner reports as the installation's own diagnostic for its
# Machine: those of the target's pre-boot proof (internal/substrate,
# preboot.go), and that of a boot image that no longer has the size and
# SHA-256 the operation froze (internal/managedos/installation, selection.go),
# and those of the update within the release (update.go), whose detail names
# the version found, the repository offering it or the closed cause.
# The tree image is the shared package-tree publication's to prove.
REFUSALS = ("hardware-mismatch", "identity-mismatch", "machine-running", "tpm-absent", "firmware-setting-mismatch",
            "firmware-setting-not-applied", "foreign-installation")
MEDIA_REFUSALS = ("media-changed-boot",)
POLICY_REFUSALS = ("ssh-policy-unavailable", "ssh-policy-host-excluded", "ssh-policy-fleet-excluded", "ssh-policy-generated-excluded")
FIPS_REFUSALS = ("fips-not-enabled",)
ENCRYPTION_REFUSALS = ("root-encryption-unproved",)
REGISTRATION_REFUSALS = (
    "registration-input", "registration-foreign", "registration-refused", "registration-identity-collision",
    "registration-unavailable", "registration-repositories", "registration-unreachable",
)
UPDATE_REFUSALS = (
    "update-release-unavailable", "update-release-mismatch", "update-release-drift", "update-no-repository",
    "update-foreign", "update-unavailable", "update-failed",
)
# The update states: a record's, a none request's and an unread or foreign
# one's; only a record's names a release, a transaction and a kernel.
UPDATE_STATES = ("", "none", "absent", "pending", "upgraded", "complete", "foreign")
RECORD_STATES = ("pending", "upgraded", "complete")
UPDATE_KEYS = frozenset(("kernel", "packages", "rebooted", "release", "state", "transaction"))
RELEASE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
KERNEL = re.compile(r"[A-Za-z0-9._+~-]{1,128}")
EMPTY_UPDATE = {"kernel": "", "packages": 0, "rebooted": False, "release": "", "state": "", "transaction": 0}
# The ownership states the registration worker reads or a removal leaves; only
# an owned, a pending or an unregistered registration names its consumer.
REGISTRATION_STATES = ("", "owned", "pending", "absent", "foreign", "unregistered", "retained")
NAMED_STATES = ("owned", "pending", "unregistered")
# What a removal proves of the machine: no registration it created is left.
RELEASED_STATES = ("unregistered", "absent", "retained")
CONSUMER = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
POWER_STATES = ("", "On", "Off")
MAX_MARKER = 4096
MAX_HOST_KEY = 4096
MAX_DIFFERENCES = 16
MAX_DIFFERENCE_BYTES = 200
# Shared package tree presence does not belong to a machine installation.
CONTENT = ("image", "private", "work")


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


def differences(value):
    """The bounded differences of a running network from its declaration."""
    value = [] if value in (None, "") else value
    if not isinstance(value, list) or len(value) > MAX_DIFFERENCES:
        raise ValueError("network differences")
    for item in value:
        if not isinstance(item, str) or not item.isprintable() or len(item.encode("utf-8")) > MAX_DIFFERENCE_BYTES:
            raise ValueError("network difference")
    return list(value)


def registration(value):
    """The registration the worker read, or none; the same rules as Go's decode."""
    if value in (None, "", {}):
        return {"consumer": "", "state": ""}
    if not isinstance(value, dict) or set(value) != {"consumer", "state"}:
        raise ValueError("registration")
    consumer, state = value["consumer"], value["state"]
    if not isinstance(consumer, str) or state not in REGISTRATION_STATES:
        raise ValueError("registration")
    named = CONSUMER.fullmatch(consumer) is not None if state in NAMED_STATES else consumer == ""
    if not named:
        raise ValueError("registration")
    return {"consumer": consumer, "state": state}


def update(value, required=True):
    """The update the worker reported, or none read; the same rules as Go's decode.

    A request that updates nothing reads nothing and records state none.
    """
    if value in (None, "", {}):
        return dict(EMPTY_UPDATE, state="" if required else "none")
    if not isinstance(value, dict) or set(value) != UPDATE_KEYS or value["state"] not in UPDATE_STATES:
        raise ValueError("update")
    state = value["state"]
    if state not in RECORD_STATES:
        if value != dict(EMPTY_UPDATE, state=state):
            raise ValueError("update")
        return dict(value)
    counts = all(type(value[key]) is int and value[key] >= 0 for key in ("packages", "transaction"))  # pylint: disable=unidiomatic-typecheck
    if (not counts or not isinstance(value["release"], str) or not RELEASE.fullmatch(value["release"])
            or not isinstance(value["kernel"], str) or not KERNEL.fullmatch(value["kernel"])
            or value["rebooted"] is not (state == "complete" and value["transaction"] != 0)):
        raise ValueError("update")
    return dict(value)


def update_proved(evidence, arguments):
    """A complete update for a request updating within its release, none for any other."""
    if flag(arguments.get("updateRequired")):
        return evidence["update"]["state"] == "complete"
    return evidence["update"]["state"] == "none"


def presence(arguments, request_digest):
    """The evidence a completion proves.

    A private installation's image names the private URL in its Kickstart, so
    its completion has withdrawn the image with the private material, a
    delivered key pair or a recovery passphrase, and proves neither published;
    a public installation's image stays in place.
    """
    observation = arguments.get("observation") or {}
    private_delivery = flag(arguments.get("privateDelivery"))
    power = arguments.get("power")
    if power not in POWER_STATES:
        raise ValueError("power state")
    evidence = {
        "absent": False,
        "address": bounded(arguments.get("address"), 128),
        "fips": flag(arguments.get("fips")),
        "hostKey": bounded(arguments.get("hostKey"), MAX_HOST_KEY),
        "image": bool(observation.get("image")),
        "marker": bounded(arguments.get("marker"), MAX_MARKER),
        "media": bounded(arguments.get("media"), 1024),
        "network": flag(arguments.get("network")),
        "networkDifferences": differences(arguments.get("networkDifferences")),
        "networkReboot": flag(arguments.get("networkReboot")),
        "postcondition": False,
        "power": str(power),
        "private": bool(observation.get("private")),
        "reachable": bool(arguments.get("reachable")),
        "registration": registration(arguments.get("registration")),
        "request": adapter_protocol.request_digest(request_digest),
        "rootEncryption": flag(arguments.get("rootEncryption")),
        "tree": bool(observation.get("tree")),
        "treeContent": bool(observation.get("treeContent")),
        "treeIdentity": tree_identity(observation.get("treeIdentity")),
        "treeStaging": bool(observation.get("treeStaging")),
        "update": update(arguments.get("update"), flag(arguments.get("updateRequired"))),
        "work": bool(observation.get("work")),
    }
    evidence["postcondition"] = bool(
        evidence["marker"] and evidence["hostKey"] and evidence["address"]
        and not evidence["media"] and evidence["power"] == "On"
        and ((not evidence["image"]) if private_delivery else evidence["image"])
        and not evidence["private"] and evidence["reachable"]
        and evidence["network"] and evidence["networkReboot"]
        and (evidence["fips"] or not flag(arguments.get("fipsRequired")))
        and (evidence["rootEncryption"] or not flag(arguments.get("rootEncryptionRequired")))
        and (evidence["registration"]["state"] == "owned" or not flag(arguments.get("registrationRequired")))
        and update_proved(evidence, arguments)
    )
    return evidence


def remaining(arguments):
    """What an absence proof still sees, named so the refusal can say so.

    These are field names, never values, so naming them is safe in a message
    that `no_log` would otherwise censor along with the evidence.
    """
    observation = arguments.get("observation") or {}
    return [name for name in CONTENT if observation.get(name)]


def released(evidence, arguments):
    """Whether a removal proved its machine holds no registration it created."""
    return evidence["registration"]["state"] in RELEASED_STATES or not flag(arguments.get("registrationRequired"))


def unproved(evidence, private_delivery=False, fips_required=False, encryption_required=False, registration_required=False,
             update_required=False):
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
    names += [name for name in ("network", "networkReboot") if not evidence[name]]
    if fips_required and not evidence["fips"]:
        names.append("fips")
    if encryption_required and not evidence["rootEncryption"]:
        names.append("rootEncryption")
    if registration_required and evidence["registration"]["state"] != "owned":
        names.append("registration")
    if evidence["update"]["state"] != ("complete" if update_required else "none"):
        names.append("update")
    return names


def absence(arguments, request_digest):
    observation = arguments.get("observation") or {}
    gone = not any(observation.get(name) for name in CONTENT)
    evidence = {
        "absent": True,
        "address": "",
        "fips": False,
        "hostKey": "",
        "image": False,
        "marker": "",
        "media": "",
        "network": False,
        "networkDifferences": [],
        "networkReboot": False,
        "postcondition": False,
        "power": "",
        "private": False,
        "reachable": False,
        "registration": registration(arguments.get("registration")),
        "request": adapter_protocol.request_digest(request_digest),
        "rootEncryption": False,
        "tree": False,
        "treeContent": False,
        "treeIdentity": "",
        "treeStaging": False,
        "update": dict(EMPTY_UPDATE),
        "work": False,
    }
    evidence["postcondition"] = bool(gone and released(evidence, arguments))
    return evidence


def completion(arguments):
    """The evidence one completion publishes, or what it names when unmet."""
    request_digest = arguments.get("digest")
    if arguments.get("removed"):
        evidence = absence(arguments, request_digest)
        unmet, verb = remaining(arguments), "still present"
        if not released(evidence, arguments):
            unmet.append("registration")
    else:
        evidence = presence(arguments, request_digest)
        unmet = unproved(
            evidence, flag(arguments.get("privateDelivery")), flag(arguments.get("fipsRequired")),
            flag(arguments.get("rootEncryptionRequired")), flag(arguments.get("registrationRequired")),
            flag(arguments.get("updateRequired")),
        )
        verb = "not proved"
    if publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, unreached("the installation", verb, unmet)


def preparation(arguments):
    """The before-state a registering apply publishes once it proved its
    marker on the machine: the public marker alone, never registration input."""
    marker = bounded(arguments.get("marker"), MAX_MARKER)
    if not marker:
        raise ValueError("marker")
    return {"marker": marker}


CAPABILITY = adapter_protocol.Capability(
    "installation",
    "the installation capability result could not be published",
    completion,
    phases=adapter_protocol.PREPARING_PHASES,
    refusals=REFUSALS + MEDIA_REFUSALS + POLICY_REFUSALS + FIPS_REFUSALS + ENCRYPTION_REFUSALS + REGISTRATION_REFUSALS + UPDATE_REFUSALS,
    detail=True,
    preparation=preparation,
)
PHASES = CAPABILITY.phases


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
