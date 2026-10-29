"""Run one KubeVirt arm operation against the host cluster, on the controller.

The controller is every KubeVirt effect's placement: it holds the lent
kubeconfig, and this plugin reads it, accepts or refuses its form before any
request, and speaks the host's API through the collection's bounded client. The
one process the arm runs, virtctl image-upload, runs through the paired upload
module. Nothing this returns carries the kubeconfig, a token or a key; every
result is censored.
"""

from __future__ import annotations

import base64
import os
import tempfile
import time

from ansible.module_utils.common.yaml import yaml_load
from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Client, Refused, Unanswered, load

BLOCKS = ("proof", "apply", "observe", "destroy")
PORTS = ("pre_boot", "state_read", "power", "boot_media", "boot_disk", "media_insert", "media_eject")
UPLOAD_SECONDS = 1200


def connect(kubeconfig, scratch, transport=None):
    return Client(load(kubeconfig, yaml_load), scratch=scratch, transport=transport)


def block(operation, client, arguments, clock=time.time, sleep=time.sleep):
    """One block operation: its evidence and whether it changed anything."""
    request = arguments["request"]
    host = arm.Host(client, request["namespace"], clock() + int(arguments.get("seconds", 600)), clock, sleep)
    if operation == "proof":
        return False, arm.proof(client, request, clock)
    context = {"operation": arguments["apply_operation"], "block": arguments["block"], "registered": arguments["registered"],
               "pinned": arguments["pinned"]}
    if operation == "observe":
        return False, arm.observe(host, request, context)
    if operation == "apply":
        return arm.apply(host, request, context)
    return arm.destroy(host, request, context)


def port(operation, client, arguments, upload, clock=time.time, sleep=time.sleep):
    """One machine port entry point over the VirtualMachine the endpoint names."""
    namespace, vm = arm.endpoint(arguments["endpoint"])
    host = arm.Host(client, namespace, clock() + int(arguments.get("seconds", 900)), clock, sleep, pin=pinned(arguments))
    if operation == "pre_boot":
        return arm.pre_boot(host, vm, bool(arguments.get("require_tpm")), arguments.get("expected") or (), arguments.get("firmware"))
    if operation == "state_read":
        return arm.state_read(host, vm, arguments.get("media_identity") or "", bool(arguments.get("media", True)))
    if operation == "power":
        return arm.change_power(host, vm, arguments["power"], bool(arguments.get("force")))
    if operation == "boot_media" or operation == "boot_disk" and arguments.get("power_on"):
        return arm.change_power(host, vm, "on")
    if operation == "boot_disk":
        return {}
    if operation == "media_eject":
        return arm.eject(host, vm)
    image, virtctl = arguments.get("image_path") or "", arguments.get("virtctl") or ""
    if not image or not virtctl:
        raise Refused("a KubeVirt machine is pushed its image: media_insert takes image_path and the controller's virtctl")
    reuse, ignored_value = arm.upload_volume(host, vm, arguments.get("media_identity") or "", os.path.getsize(image))
    if not reuse:
        proxy, authority = arm.upload_trust(host)
        upload(namespace, vm + "-media", image, proxy, authority, max(1, min(UPLOAD_SECONDS, int(host.remaining()))))
        done = host.wait(lambda: client.get(host.dv(vm + "-media")),
                         lambda value: value is not None and (value.get("status") or {}).get("phase") == "Succeeded", arm.VOLUME_SECONDS)
        if done is None or (done.get("status") or {}).get("phase") != "Succeeded":
            raise Refused("the upload to DataVolume %s/%s-media did not succeed" % (namespace, vm))
    return arm.attach(host, vm)


def pinned(arguments):
    """The cluster and VirtualMachine UIDs the consumer's pin carries, decoded only to compare."""
    decoded = {}
    for key, name in (("pinned_uuid_base64", "cluster"), ("pinned_serial_base64", "vm")):
        try:
            decoded[name] = base64.b64decode(str(arguments.get(key) or ""), validate=True).decode("ascii").strip()
        except (ValueError, UnicodeError):
            decoded[name] = ""
    return decoded


def failed(error):
    if isinstance(error, arm.PortRefused):
        return {"failed": True, "msg": str(error), "refusal": error.code}
    if isinstance(error, Refused):
        return {"failed": True, "msg": str(error)}
    if isinstance(error, Answered):
        return {"failed": True, "msg": arm.answered_reason(error)}
    return {"failed": True, "msg": "the host cluster did not answer"}


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        arguments = dict(self._task.args)
        operation = arguments.get("operation")
        try:
            if operation not in BLOCKS and operation not in PORTS:
                result = {"failed": True, "msg": "unsupported KubeVirt operation"}
            elif operation in BLOCKS:
                changed, evidence = block(operation, connect(arguments["kubeconfig"], arguments["scratch"]), arguments)
                result = {"changed": changed, "observation": evidence}
            else:
                client = connect(arguments["kubeconfig"], arguments["scratch"])
                result = dict(port(operation, client, arguments, self._upload(arguments, task_vars)), changed=False)
        except (Refused, Answered, Unanswered) as error:
            result = failed(error)
        except (KeyError, TypeError, ValueError, OSError):
            result = {"failed": True, "msg": "the KubeVirt request or the host's answer is invalid"}
        result["_ansible_no_log"] = True
        return result

    def _upload(self, arguments, task_vars):
        def upload(namespace, volume, image, proxy, authority, seconds):
            descriptor, trust = tempfile.mkstemp(prefix="kubevirt-ingress-", suffix=".pem", dir=arguments["scratch"])
            with os.fdopen(descriptor, "w") as handle:
                handle.write(authority)
            try:
                answer = self._execute_module(module_name="bootwright.core.substrate_kubevirt_upload", module_args={
                    "virtctl": arguments["virtctl"], "kubeconfig": arguments["kubeconfig"], "trust": trust,
                    "namespace": namespace, "volume": volume, "image": image, "proxy": proxy, "seconds": seconds,
                }, task_vars=task_vars)
            finally:
                os.unlink(trust)
            if answer.get("failed"):
                raise Refused(str(answer.get("msg") or "virtctl image-upload failed")[:512])
        return upload
