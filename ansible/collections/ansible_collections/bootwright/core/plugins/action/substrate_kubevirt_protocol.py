"""Emit the KubeVirt provider and machine protocol on the runner's result channel.

This runs on the controller, because the inherited result descriptor belongs to
the invoking Bootwright process. A completion publishes exactly the fields the
Go capability decodes, each bounded, and only what the observation proved: a
mutation's evidence publishes only when its postcondition holds, while an
observation reports what it found, partial and unanswered included.
"""

from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_ITEMS = 64
MAX_TEXT = 256
# ROOT_MARK is the mark a kept machine's blanked root disk carries: the staged destroy and the step whose removal blanked it.
ROOT_MARK = re.compile(r"op-[0-9a-f]{32}/[a-z0-9](?:[a-z0-9-]{0,251}[a-z0-9])?\Z")


def text(value, limit=MAX_TEXT):
    value = "" if value is None else value
    if not isinstance(value, str) or len(value) > limit or not value.isprintable():
        raise ValueError("bounded text")
    return value


def items(values, shape):
    if not isinstance(values, list) or len(values) > MAX_ITEMS:
        raise ValueError("bounded list")
    return [shape(value) for value in values]


def count(value, limit=1 << 20):
    if not isinstance(value, int) or isinstance(value, bool) or not 0 <= value <= limit:
        raise ValueError("bounded count")
    return value


def network(value):
    published = {"attachment": text(value["attachment"], 63), "cniType": text(value.get("cniType"), 63), "present": bool(value["present"])}
    if "topology" in value:
        published["topology"] = text(value["topology"], 63)
    if "mtu" in value:
        published["mtu"] = count(value["mtu"])
    if "macSpoofCheck" in value:
        if not isinstance(value["macSpoofCheck"], bool):
            raise ValueError("bounded flag")
        published["macSpoofCheck"] = value["macSpoofCheck"]
    return published


def host(observation, digest):
    return {
        "absent": bool(observation.get("absent")),
        "failures": items(observation.get("failures") or [], lambda value: text(value, 512)),
        "kubeSystemUID": text(observation.get("kubeSystemUID")),
        "macPool": text(observation.get("macPool") or "unknown", 16),
        "networks": items(observation.get("networks") or [], network),
        "postcondition": bool(observation.get("postcondition")),
        "readWriteMany": bool(observation.get("readWriteMany")),
        "request": adapter_protocol.request_digest(digest),
        "rootAccessModes": items(observation.get("rootAccessModes") or [], lambda value: text(value, 32)),
        "rootVolumeMode": text(observation.get("rootVolumeMode"), 16),
        "schedulableNodes": count(observation.get("schedulableNodes", 0)),
        "server": text(observation.get("server")),
        "storageClass": text(observation.get("storageClass"), 253),
        "uploadProxy": text(observation.get("uploadProxy")),
        "version": text(observation.get("version"), 64),
    }


def machine(observation, digest):
    return {
        "absent": bool(observation.get("absent")),
        "answered": bool(observation.get("answered")),
        "foreign": bool(observation.get("foreign")),
        "instance": text(observation.get("instance"), 32),
        "kubeSystemUID": text(observation.get("kubeSystemUID")),
        "owned": bool(observation.get("owned")),
        "postcondition": bool(observation.get("postcondition")),
        "receipts": items(observation.get("receipts") or [], lambda value: {
            "basis": text(value["basis"], 32), "kind": text(value["kind"], 63),
            "name": text(value["name"], 253), "uid": text(value["uid"], 128)}),
        "request": adapter_protocol.request_digest(digest),
        "server": text(observation.get("server")),
        "virtualMachine": text(observation.get("virtualMachine"), 63),
        "volumes": items(observation.get("volumes") or [], lambda value: {
            "kind": text(value["kind"], 63), "name": text(value["name"], 253), "present": bool(value["present"])}),
    }


def lend(observation, digest):
    seconds = observation.get("expirationSeconds")
    if not isinstance(seconds, int) or isinstance(seconds, bool) or not 0 <= seconds <= 1 << 31:
        raise ValueError("bounded lifetime")
    return {
        "expirationSeconds": seconds,
        "postcondition": bool(observation.get("postcondition")),
        "request": adapter_protocol.request_digest(digest),
        "secret": text(observation.get("secret"), 253),
        "server": text(observation.get("server")),
        "version": "substrate-access-kubevirt-evidence-v1",
    }


def root_reset(arguments):
    """The evidence of a root disk's reset or its reading, and whether it is blank under this mark.

    Only the VirtualMachine's identity and power, its root DataVolume and the
    mark it carries are published; a reset proves its postcondition only for
    this context's own VirtualMachine, stopped, whose blank root carries this mark.
    """
    expected = arguments.get("root_reset")
    if not isinstance(expected, str) or not ROOT_MARK.fullmatch(expected):
        raise ValueError("root reset mark")
    observed = arguments.get("observation") or {}
    root, mark = observed.get("root") or {}, text(observed.get("rootReset"))
    if mark and not ROOT_MARK.fullmatch(mark):
        raise ValueError("root reset mark")
    evidence = {
        "answered": observed.get("answered") is True, "instance": text(observed.get("instance"), 32),
        "kubeSystemUID": text(observed.get("kubeSystemUID")), "owned": observed.get("owned") is True, "postcondition": False,
        "request": adapter_protocol.request_digest(arguments.get("digest")),
        "root": {"blank": root.get("blank") is True, "name": text(root.get("name"), 253), "phase": text(root.get("phase"), 32),
                 "present": root.get("present") is True, "storage": text(root.get("storage"), 32),
                 "storageClass": text(root.get("storageClass"), 253), "uid": text(root.get("uid"), 128),
                 "volume": text(root.get("volume"), 253)},
        "rootReset": mark, "virtualMachine": text(observed.get("virtualMachine"), 128),
    }
    evidence["postcondition"] = (observed.get("postcondition") is True and evidence["answered"] and evidence["owned"] and mark == expected
                                 and evidence["root"]["present"] and evidence["root"]["blank"] and bool(evidence["root"]["volume"]))
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the root disk", "not proved blank under", ["rootReset"])


def completion(arguments):
    if arguments.get("subject") == "root-reset":
        return root_reset(arguments)
    if arguments.get("subject") in ("lend", "revoke"):
        evidence = lend(arguments.get("observation") or {}, arguments.get("digest"))
        if evidence["postcondition"]:
            return evidence, None
        return evidence, "the KubeVirt host access was not %s" % ("lent" if arguments["subject"] == "lend" else "revoked")
    if arguments.get("subject") == "host" and arguments.get("released"):
        return host({"absent": True, "postcondition": True}, arguments.get("digest")), None
    shape = host if arguments.get("subject") == "host" else machine
    evidence = shape(arguments.get("observation") or {}, arguments.get("digest"))
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    if shape is host and evidence["failures"]:
        return evidence, "the KubeVirt provider was not proved: " + evidence["failures"][0]
    return evidence, "the KubeVirt %s did not reach its postcondition" % ("provider" if shape is host else "machine")


CAPABILITY = adapter_protocol.Capability(
    "KubeVirt substrate",
    "the KubeVirt substrate result could not be published",
    completion,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
