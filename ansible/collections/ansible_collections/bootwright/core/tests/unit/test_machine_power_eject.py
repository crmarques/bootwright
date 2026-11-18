"""An operator's eject reaches every arm's media_eject entry point with exactly its arguments.

ansible-core validates a role's argument spec when the entry point is
included, so a port dictionary missing a key an arm requires, or carrying one
it does not declare, fails every eject on that arm before any request. Each
arm's port is the one machine_port_access derives from the frozen machine port,
combined as tasks/eject.yml combines it, and is checked against that arm's own
media_eject spec.
"""

from __future__ import annotations

import pathlib

import pytest
import yaml
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.filter.machine_port_access import machine_port_access
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
EJECT = ROLES / "machine_power" / "tasks" / "eject.yml"

MATERIAL = {"bmc-user": "/run/user", "bmc-password": "/run/password", "bmc-ca": "/run/ca"}
REDFISH = {"endpoint": "https://bmc.example.test", "verify": True, "verification": "full",
           "userVariable": "bmc-user", "passwordVariable": "bmc-password", "caVariable": "bmc-ca"}
KUBEVIRT = {"endpoint": "kubevirt:lab/guest", "verify": True, "userVariable": "bmc-user", "mediaVolume": "guest-media"}
PORTS = {"libvirt": REDFISH, "baremetal": REDFISH, "kubevirt": KUBEVIRT}


def media_eject_include():
    tasks = [task for task in LOADER.load_from_file(str(EJECT), trusted_as_template=True) if isinstance(task, dict)]
    found = [task for task in tasks if (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "media_eject"]
    assert len(found) == 1, "%d media_eject includes" % len(found)
    return found[0]


def spec(arm):
    loaded = yaml.safe_load((ROLES / ("substrate_%s_machine" % arm) / "meta" / "argument_specs.yml").read_text())
    return loaded["argument_specs"]["media_eject"]["options"]["bootwright_machine_port"]["options"]


@pytest.mark.parametrize("arm", sorted(PORTS))
def test_each_arm_receives_every_argument_its_media_eject_requires_and_no_other(arm):
    template = media_eject_include()["vars"]["bootwright_machine_port"]
    access = machine_port_access(PORTS[arm], MATERIAL)
    port = Templar(loader=LOADER, variables={"machine_power_access": access}).template(trust_as_template(template))
    declared = spec(arm)
    required = {name for name, option in declared.items() if option.get("required")}
    assert required <= set(port), "missing %s" % sorted(required - set(port))
    assert set(port) <= set(declared), "undeclared %s" % sorted(set(port) - set(declared))
    assert port["restore_verification"] is False and port["remove_certificate"] is False
