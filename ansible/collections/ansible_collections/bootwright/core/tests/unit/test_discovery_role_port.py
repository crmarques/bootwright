"""Discovery's production role entry points admit its frozen SSH connection."""

import pytest
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
from ansible_collections.bootwright.core.tests.unit.test_playbook_entry_points import first_task


def options(entry):
    task = first_task("managedos/discovery_" + entry + ".yml")
    assert task.action == "ansible.builtin.validate_argument_spec"
    assert task.args["validate_args_context"]["name"] == "managedos_disk_discovery"
    assert task.args["validate_args_context"]["argument_spec_name"] == entry
    return task.args["argument_spec"]


def request(ssh):
    return {
        "bootwright_disk_discovery_request": {
            "version": "os-disk-discovery-v1", "identity": {}, "bootMedia": {}, "controller": {},
            "interfaces": [], "network": {}, "private": {}, "inventory": "automatic",
            "deadlineSeconds": 1800, "placement": {}, "ssh": ssh,
        },
        "bootwright_disk_discovery_digest": "a" * 64,
        "bootwright_disk_discovery_material": {},
    }


@pytest.mark.parametrize("entry", ("apply", "observe", "destroy"))
@pytest.mark.parametrize("port", (22, 2222))
def test_ansible_accepts_the_frozen_ssh_connection(entry, port):
    result = ArgumentSpecValidator(options(entry)).validate(request({"port": port}), validate_role_argument_spec=True)
    assert not result.error_messages, result.error_messages


@pytest.mark.parametrize("entry", ("apply", "observe", "destroy"))
@pytest.mark.parametrize("change", ("missing-ssh", "missing-port", "extra-ssh"))
def test_ansible_refuses_incomplete_or_undeclared_ssh_fields(entry, change):
    values = request({"port": 22})
    if change == "missing-ssh":
        del values["bootwright_disk_discovery_request"]["ssh"]
    else:
        values["bootwright_disk_discovery_request"]["ssh"] = {} if change == "missing-port" else {"port": 22, "other": True}
    result = ArgumentSpecValidator(options(entry)).validate(values, validate_role_argument_spec=True)
    assert result.error_messages
