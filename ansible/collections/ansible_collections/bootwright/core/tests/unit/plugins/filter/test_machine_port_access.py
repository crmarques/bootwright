"""The machine_port_access filter resolves a frozen machinePort object against its material.

The object names material variables and never carries a value, so the account,
the trust bundle and the pins reach a port entry point only from the
operation's material; a variable the object names but the material lacks
refuses without naming any material value.
"""

from __future__ import annotations

import pytest
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.machine_port_access import machine_port_access

PORT = {"endpoint": "https://bmc.example.test/redfish/v1/Systems/1", "verify": True, "userVariable": "controllerUserNode1",
        "passwordVariable": "controllerPasswordNode1", "caVariable": "controllerCANode1",
        "pinnedUUIDVariable": "pinnedUUIDBase64Node1", "pinnedSerialVariable": "pinnedSerialBase64Node1"}
MATERIAL = {"controllerUserNode1": "/run/user-1", "controllerPasswordNode1": "/run/password-1", "controllerCANode1": "/run/ca-1",
            "controllerUserNode0": "/run/user-0", "pinnedUUIDBase64Node1": "dXVpZA==", "pinnedSerialBase64Node0": "b3RoZXI="}


def test_the_access_reads_only_the_material_its_port_names():
    assert machine_port_access(PORT, MATERIAL) == {
        "endpoint": PORT["endpoint"], "verify": True, "user": "/run/user-1", "password": "/run/password-1", "ca": "/run/ca-1",
        "pinned_uuid_base64": "dXVpZA==", "pinned_serial_base64": ""}


def test_verification_and_the_media_volume_are_handed_only_when_frozen():
    plain = machine_port_access(PORT, MATERIAL)
    assert "verification" not in plain and "media_volume" not in plain
    frozen = machine_port_access(dict(PORT, verification="certificate", mediaVolume="vm-1-media"), MATERIAL)
    assert (frozen["verification"], frozen["media_volume"]) == ("certificate", "vm-1-media")


def test_a_port_that_names_no_password_or_bundle_hands_empty_ones():
    port = {key: value for key, value in PORT.items() if key not in ("passwordVariable", "caVariable")}
    access = machine_port_access(port, MATERIAL)
    assert (access["password"], access["ca"]) == ("", "")


@pytest.mark.parametrize("missing", ["controllerUserNode1", "controllerPasswordNode1", "controllerCANode1"])
def test_a_named_credential_the_material_lacks_refuses_without_a_value(missing):
    material = {key: value for key, value in MATERIAL.items() if key != missing}
    with pytest.raises(AnsibleFilterError) as refused:
        machine_port_access(PORT, material)
    assert not any(value in str(refused.value) for value in MATERIAL.values())


@pytest.mark.parametrize("port", [None, {}, {"endpoint": "x", "verify": True}])
def test_a_port_that_names_no_account_refuses(port):
    with pytest.raises(AnsibleFilterError):
        machine_port_access(port, MATERIAL)
