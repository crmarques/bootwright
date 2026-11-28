"""The vSphere Machine accepts the private, pinned controller inputs its installers actually compose."""

from __future__ import annotations

import pathlib

import pytest
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
from ansible.template import Templar
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
HOST = {"endpoint": "https://vc.example.test:8443", "verify": True, "user": "/private/user", "password": "/private/password",
        "ca": "/private/ca", "pinned_uuid_base64": "dmM=", "pinned_serial_base64": "cGlu", "media_volume": "fixture",
        "prove_macs": ["00:50:56:01:02:03"]}
DIGEST = "a" * 64


def port(taskfile, entry):
    tasks = LOADER.load_from_file(str(ROLES / "managedos_install_anaconda" / "tasks" / taskfile), trusted_as_template=True)
    include = next(t for t in role_tasks.flatten(tasks) if t.get("ansible.builtin.include_role", {}).get("tasks_from") == entry)
    variables = {"managedos_install_anaconda_port_access": dict(HOST, media_identity=DIGEST), "managedos_install_anaconda_expected": HOST["prove_macs"],
                 "bootwright_os_install_request": {"target": {"substrate": "vsphere", "tpm2": True, "machinePort": {"mediaVolume": "fixture"}}},
                 "bootwright_os_install_material": {}, "bootwright_os_install_digest": DIGEST, "bootwright_run_scratch": "/private",
                 "managedos_install_anaconda_scratch": "/private", "managedos_install_anaconda_image_path": "/private/image.iso",
                 "managedos_install_anaconda_media_trust": {"trust": "verified", "enableVerification": True}}
    variables.update(include["vars"])
    value = Templar(loader=LOADER, variables=variables).template(include["vars"]["bootwright_machine_port"])
    declared = LOADER.load_from_file(str(ROLES / "substrate_vsphere_machine/meta/argument_specs.yml"))["argument_specs"]
    answer = ArgumentSpecValidator(declared[entry]["options"]).validate({"bootwright_machine_port": value}, validate_role_argument_spec=True)
    assert not answer.error_messages, answer.error_messages
    assert all(value[k] == v for k, v in HOST.items())
    return value


@pytest.mark.parametrize("file,operation", [
    ("clone_read.yml", "seed_read"), ("clone_boot.yml", "seed_insert"), ("clone_boot.yml", "boot_disk"),
    ("clone_boot.yml", "pre_boot"), ("boot.yml", "media_insert"), ("boot.yml", "boot_media"),
])
def test_installer_tasks_hand_the_exact_pinned_origin_and_arguments_to_the_native_port(file, operation):
    value = port(file, operation)
    if operation in ("seed_read", "seed_insert"):
        assert value["seed_identity"] == DIGEST
    if operation == "seed_insert":
        assert value["metadata_path"] == "/private/metadata" and value["userdata_path"] == "/private/userdata"
    if operation == "media_insert":
        assert value["media_identity"] == DIGEST and value["image_path"] == "/private/image.iso"
    if operation == "pre_boot":
        assert value["expected"] == HOST["prove_macs"] and value["require_tpm"] is True
