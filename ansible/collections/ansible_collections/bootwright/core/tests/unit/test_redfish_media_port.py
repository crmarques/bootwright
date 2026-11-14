"""Both substrate ports preserve the complete Redfish media security request."""

from __future__ import annotations

import pathlib

import pytest
import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"


@pytest.mark.parametrize("arm", ["baremetal", "libvirt"])
@pytest.mark.parametrize("operation", ["insert", "eject"])
def test_each_media_port_enters_the_shared_translation_and_returns_its_answer(arm, operation):
    tasks = yaml.safe_load((ROLES / ("substrate_" + arm + "_machine") / "tasks" / ("media_" + operation + ".yml")).read_text())
    entry = next(task["ansible.builtin.include_role"] for task in tasks if "ansible.builtin.include_role" in task)
    assert entry == {"name": "bootwright.core.substrate_machine", "tasks_from": "redfish_media_" + operation}
    returned = next(task["ansible.builtin.set_fact"] for task in tasks if "ansible.builtin.set_fact" in task)
    assert returned["substrate_" + arm + "_machine_port_result"] == "{{ substrate_machine_redfish_port_result }}"
    shared = yaml.safe_load((ROLES / "substrate_machine/tasks" / (entry["tasks_from"] + ".yml")).read_text())
    call = next(task["bootwright.core.redfish_boot"] for task in shared if "bootwright.core.redfish_boot" in task)
    fields = {"verify", "verification", "ca_data", "certificate"}
    fields |= {"trust", "enable_verification", "private_delivery"} if operation == "insert" else {"restore_verification", "remove_certificate"}
    assert fields <= call.keys()
    if operation == "insert":
        specification = yaml.safe_load((ROLES / ("substrate_" + arm + "_machine") / "meta/argument_specs.yml").read_text())
        options = specification["argument_specs"]["media_insert"]["options"]["bootwright_machine_port"]["options"]
        assert options["enable_verification"]["type"] == "bool"
    assert all("bootwright_machine_port." in call[key] for key in fields)
