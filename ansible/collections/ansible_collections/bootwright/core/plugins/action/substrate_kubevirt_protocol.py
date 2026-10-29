"""Emit the KubeVirt provider and machine protocol on the runner's result channel.

This runs on the controller, because the inherited result descriptor belongs to
the invoking Bootwright process. A completion publishes exactly the fields the
Go capability decodes, each bounded, and only what the observation proved: a
mutation's evidence publishes only when its postcondition holds, while an
observation reports what it found, partial and unanswered included.
"""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

MAX_ITEMS = 64
MAX_TEXT = 256


def text(value, limit=MAX_TEXT):
    value = "" if value is None else value
    if not isinstance(value, str) or len(value) > limit or not value.isprintable():
        raise ValueError("bounded text")
    return value


def items(values, shape):
    if not isinstance(values, list) or len(values) > MAX_ITEMS:
        raise ValueError("bounded list")
    return [shape(value) for value in values]


def host(observation, digest):
    return {
        "absent": bool(observation.get("absent")),
        "failures": items(observation.get("failures") or [], lambda value: text(value, 512)),
        "kubeSystemUID": text(observation.get("kubeSystemUID")),
        "macPool": text(observation.get("macPool") or "unknown", 16),
        "networks": items(observation.get("networks") or [], lambda value: {
            "attachment": text(value["attachment"], 63), "present": bool(value["present"])}),
        "postcondition": bool(observation.get("postcondition")),
        "readWriteMany": bool(observation.get("readWriteMany")),
        "request": adapter_protocol.request_digest(digest),
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
        "server": text(observation.get("server")),
        "version": "substrate-access-kubevirt-evidence-v1",
    }


def completion(arguments):
    if arguments.get("subject") == "lend":
        evidence = lend(arguments.get("observation") or {}, arguments.get("digest"))
        return evidence, None if evidence["postcondition"] else "the KubeVirt host access was not lent"
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
