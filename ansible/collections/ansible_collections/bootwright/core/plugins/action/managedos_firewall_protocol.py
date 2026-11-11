"""Publish the host firewall's bounded lifecycle evidence."""
from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, managedos_firewall_worker as worker
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import emit

VERSION = "os-host-firewall-evidence-v2"


def proves(services, removed):
    """Whether every service ends as the operation promised: absent and held by no zone for a removal, else owned, covered and held."""
    if removed:
        return all(entry["state"] == "absent" and not entry["memberships"] for entry in services)
    return all(entry["state"] == "owned" and entry["covered"] and entry["memberships"] for entry in services)


def completion(arguments):
    services = arguments.get("services")
    if not worker.valid_evidence(services):
        raise ValueError("host firewall services")
    removed = bool(arguments.get("removed"))
    ready = arguments.get("ready", True)
    if type(ready) is not bool:  # pylint: disable=unidiomatic-typecheck
        raise ValueError("host firewall readiness")
    evidence = {"ready": ready, "absent": removed, "postcondition": (ready or removed) and proves(services, removed),
                "request": adapter_protocol.request_digest(arguments.get("digest")),
                "services": [dict(entry, memberships=[dict(item) for item in entry["memberships"]]) for entry in services],
                "version": VERSION}
    if adapter_protocol.publishes(evidence, arguments.get("observed")):
        return evidence, None
    return evidence, adapter_protocol.unreached("the host firewall", "not proved", ["services"])


# The refusals this publication names to its runner before the run fails, each
# one the runner reports as the publication's own diagnostic
# (internal/managedos/installation): a service file another owner holds, a
# network without an interface, a firewalld that is not running and a change
# firewalld did not accept. A detail names the service, never a value.
REFUSALS = ("host-firewall-foreign", "host-firewall-no-interface", "host-firewall-inactive", "host-firewall-failed")

CAPABILITY = adapter_protocol.Capability(
    "host-firewall",
    "the host firewall result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=REFUSALS,
    detail=True,
)


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        return adapter_protocol.publish(self._task.args, CAPABILITY, emit)
