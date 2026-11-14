"""Validate frozen bare-metal requests before any lifecycle task or BMC call."""

from __future__ import annotations

import copy
from pathlib import Path

import pytest
from ansible.module_utils.common.arg_spec import ArgumentSpecValidator
from ansible.parsing.dataloader import DataLoader


ROLE = Path(__file__).resolve().parents[2] / "roles" / "substrate_baremetal_machine"
REQUEST = {
    "controller": {"credentialsRef": "bmc-login", "endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "tlsVerify": True},
    "hardware": [{"macAddress": "02:00:00:00:00:01", "name": "eno1"}],
    "identity": {"block": "machine-server", "context": "example", "object": "server"},
    "placement": {"connection": "local", "machine": "controller"},
    "provider": "floor",
    "version": "machine-baremetal-v1",
}


def validate(entry, request):
    spec = DataLoader().load_from_file(str(ROLE / "meta" / "argument_specs.yml"))["argument_specs"][entry]["options"]
    return ArgumentSpecValidator(spec).validate({
        "bootwright_substrate_physical_request": request,
        "bootwright_substrate_physical_digest": "a" * 64,
        "bootwright_substrate_physical_material": {},
    }, validate_role_argument_spec=True)


@pytest.mark.parametrize("entry", ["apply", "observe", "destroy"])
@pytest.mark.parametrize("reference", [None, {"firmware": True}, {"storageId": "RAID", "volumeId": "OS"}])
def test_baremetal_lifecycle_accepts_the_frozen_root_volume_selector(entry, reference):
    request = copy.deepcopy(REQUEST)
    if reference is not None:
        request["rootVolumeRef"] = reference
    result = validate(entry, request)
    assert not result.error_messages, result.error_messages
    admitted = result.validated_parameters["bootwright_substrate_physical_request"].get("rootVolumeRef")
    if admitted is not None:
        admitted = {key: value for key, value in admitted.items() if value is not None}
    assert admitted == reference


@pytest.mark.parametrize("entry", ["apply", "observe", "destroy"])
@pytest.mark.parametrize("unknown", ["request", "selector"])
def test_baremetal_lifecycle_still_refuses_unknown_request_and_selector_fields(entry, unknown):
    request = copy.deepcopy(REQUEST)
    request["rootVolumeRef"] = {"firmware": True}
    target = request if unknown == "request" else request["rootVolumeRef"]
    target["unexpected"] = True
    result = validate(entry, request)
    assert any("unexpected" in message for message in result.error_messages), result.error_messages
