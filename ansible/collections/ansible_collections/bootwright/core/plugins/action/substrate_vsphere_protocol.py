"""Publish bounded vSphere machine evidence on the inherited result channel."""

from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit
from ansible_collections.bootwright.core.plugins.module_utils.vsphere_session import integer


def bounded(value, size):
    if not isinstance(value, str) or len(value) > size or any(ord(ch) < 32 for ch in value):
        raise ValueError("invalid evidence string")
    return value


def completion(args):
    raw = args["observation"]
    obs = {"request": adapter_protocol.request_digest(args["digest"])}
    for field in ("answered", "absent", "owned", "foreign", "postcondition", "retiring"):
        if not isinstance(raw[field], bool):
            raise ValueError("invalid evidence status")
        obs[field] = raw[field]
    for field in ("vcenterUUID", "instanceUUID", "moRef", "owner", "power"):
        obs[field] = bounded(raw[field], 256)
    obs["vmHome"] = bounded(raw["vmHome"], 4096)
    obs["stagingDatastoreRef"] = bounded(raw["stagingDatastoreRef"], 128)
    if not isinstance(raw["disks"], list) or len(raw["disks"]) > 60:
        raise ValueError("invalid disk evidence")
    obs["disks"] = []
    for d in raw["disks"]:
        if not integer(d["key"]) or not 0 < d["key"] <= (1 << 31) - 1:
            raise ValueError("invalid disk key")
        obs["disks"].append(dict(name=bounded(d["name"], 253), key=d["key"], backing=bounded(d["backing"], 4096),
                                 uuid=bounded(d["uuid"], 128), datastore=bounded(d["datastore"], 128)))
    if adapter_protocol.publishes(obs, args.get("observed")):
        return obs, None
    return obs, "the vSphere Machine did not reach its postcondition"


CAPABILITY = adapter_protocol.Capability("vSphere Machine", "the vSphere Machine result could not be published", completion)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp, task_vars
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
