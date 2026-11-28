"""Run the vSphere Machine adapter inside the locked controller runtime."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_machine as machine
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_session as api
from ansible_collections.bootwright.core.plugins.module_utils.vsphere_session import connect

BLOCKS = ("apply", "observe", "destroy")
READS = ("observe", "pre_boot", "state_read", "seed_read")


def execute(operation, args, check_mode):
    api.require(not check_mode or operation in READS, "the vCenter mutation cannot run in check mode")
    if operation in BLOCKS:
        req, materials = args["request"], args["material"]
        machine.validate_request(req, materials)
        with connect(req["server"], materials["controllerUser"], materials["controllerPassword"],
                     materials.get("controllerCA"), req["tlsVerify"]) as session:
            changed, observed = machine.block(session, operation, req, materials)
            return {"changed": changed, "observation": observed}
    port = args["port"]
    with connect(port["endpoint"], port["user"], port["password"], port.get("ca"), port["verify"]) as session:
        return dict(machine.port(session, operation, port), changed=False)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        args = dict(self._task.args)
        operation = args.get("operation")
        try:
            api.require(operation in (*BLOCKS, *READS, "boot_disk", "boot_media", "media_insert", "media_eject", "power", "seed_insert", "seed_eject"),
                        "unsupported vSphere operation")
            result = execute(operation, args, self._task.check_mode)
        except Exception as error:
            # SOAP faults can carry guestinfo, paths and private URLs. Only
            # locally constructed diagnoses cross the action boundary.
            if operation == "observe":
                result = {"changed": False, "observation": dict(machine.evidence(), foreign=getattr(error, "code", "") == "identity-mismatch")}
            elif isinstance(error, api.Refused):
                result = {"failed": True, "msg": str(error), "refusal": error.code}
            else:
                reason = {
                    "NoPermission": "the vCenter principal lacks a privilege on the selected inventory objects",
                    "InvalidLogin": "vCenter refused the provisioning credential",
                    "SSLCertVerificationError": "vCenter TLS certificate verification failed",
                    "AlreadyExists": "the selected inventory or datastore name is already occupied",
                    "FileAlreadyExists": "the selected datastore path is already occupied",
                    "InvalidPowerState": "the VM power state changed during the operation",
                    "ConcurrentAccess": "the VM configuration changed concurrently; observe before retry",
                }.get(type(error).__name__, "vCenter did not prove this operation; check verified access, inventory, privileges and task state")
                result = {"failed": True, "msg": reason}
        return dict(result, _ansible_no_log=True)
