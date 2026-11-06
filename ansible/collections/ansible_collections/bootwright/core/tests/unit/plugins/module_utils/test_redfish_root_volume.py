"""Existing and firmware-selected logical roots require positive OS correlation."""

from __future__ import annotations

import copy
from urllib.parse import urlsplit

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_root_volume


SYSTEM = "/redfish/v1/Systems/1"
STORAGE = SYSTEM + "/Storage"
CONTROLLER = STORAGE + "/RAID"
VOLUMES = CONTROLLER + "/Volumes"
VOLUME = VOLUMES + "/OS"
OPTIONS = SYSTEM + "/BootOptions"
OPTION = OPTIONS + "/1"
WWN = "5000000000000042"
REF = {"storageId": "RAID", "volumeId": "OS"}
PROOF = dict(REF, wwn="0x" + WWN, sizeBytes=8192)


class Inventory:
    def __init__(self):
        self.native = redfish_control.Client("https://bmc.example.test" + SYSTEM, "operator", "never-expose")
        self.body = {"Storage": {"@odata.id": STORAGE}, "Boot": {
            "BootOrder": ["Boot0001"], "BootOptions": {"@odata.id": OPTIONS}}}
        self.resources = {
            STORAGE: {"Members": [{"@odata.id": CONTROLLER}]},
            CONTROLLER: {"@odata.type": "#Storage.v1_15_0.Storage", "Id": "RAID", "Drives": [], "Volumes": {"@odata.id": VOLUMES}},
            VOLUMES: {"Members": [{"@odata.id": VOLUME}]},
            VOLUME: {"@odata.type": "#Volume.v1_10_0.Volume", "Id": "OS", "CapacityBytes": 8192,
                     "Identifiers": [{"DurableNameFormat": "NAA", "DurableName": WWN}]},
            OPTIONS: {"Members": [{"@odata.id": OPTION}]},
            OPTION: {"@odata.type": "#BootOption.v1_0_6.BootOption", "BootOptionReference": "Boot0001",
                     "BootOptionEnabled": True, "Alias": "Hdd", "RelatedItem": [{"@odata.id": VOLUME}]},
        }
        self.reads = []

    def system(self):
        return self.body

    def _own(self, reference, doing):
        return self.native._own(reference, doing)

    def _read(self, reference, doing):
        url = self._own(reference, doing)
        self.reads.append(url)
        if urlsplit(url).path not in self.resources:
            raise redfish_control.ControllerError("unanswered inventory")
        return self.resources[urlsplit(url).path], {}


@pytest.mark.parametrize("selector", [REF, {"firmware": True}])
def test_selected_volume_is_discovered_without_writes_or_member_serials(selector):
    client = Inventory()
    before = copy.deepcopy(client.resources)
    assert redfish_root_volume.discover(client, selector) == PROOF
    assert client.resources == before


@pytest.mark.parametrize("change", [
    {"CapacityBytes": True}, {"CapacityBytes": "8192"}, {"CapacityBytes": 8193}, {"CapacityBytes": 0},
    {"Identifiers": []}, {"Identifiers": [{"DurableNameFormat": "UUID", "DurableName": WWN}]},
    {"Identifiers": [{"DurableNameFormat": "NAA", "DurableName": "not-an-identifier"}]},
    {"Identifiers": [{"DurableNameFormat": "NAA", "DurableName": WWN}] * 2},
    {"@odata.type": "#Drive.v1_19_0.Drive"},
])
def test_incomplete_unmappable_or_ambiguous_volume_refuses(change):
    client = Inventory()
    client.resources[VOLUME].update(change)
    with pytest.raises(redfish_root_volume.Refused):
        redfish_root_volume.discover(client, REF)


@pytest.mark.parametrize("collection", [STORAGE, VOLUMES, OPTIONS])
def test_partial_inventory_never_discovers_a_default(collection):
    client = Inventory()
    client.resources[collection]["Members@odata.nextLink"] = collection + "?page=2"
    with pytest.raises(redfish_root_volume.Refused):
        redfish_root_volume.discover(client, {"firmware": True})


@pytest.mark.parametrize("change", [
    {"BootOptionEnabled": None}, {"BootOptionEnabled": False}, {"Alias": None}, {"Alias": "RemoteDrive"},
    {"RelatedItem": []}, {"RelatedItem": [{"@odata.id": CONTROLLER}]},
    {"RelatedItem": [{"@odata.id": "https://other.example.test" + VOLUME}]},
    {"RelatedItem": [{"@odata.id": VOLUME}] * 2},
])
def test_generic_unproved_or_disabled_boot_options_never_select_a_root(change):
    client = Inventory()
    client.resources[OPTION].update(change)
    with pytest.raises(redfish_control.ControllerError):
        redfish_root_volume.discover(client, {"firmware": True})


def test_transient_override_and_boot_capability_never_choose_default():
    client = Inventory()
    client.body["Boot"].update(BootNext="Boot0002", BootSourceOverrideTarget="Hdd")
    client.resources[VOLUME]["IsBootCapable"] = True
    client.body["Boot"].pop("BootOrder")
    with pytest.raises(redfish_root_volume.Refused):
        redfish_root_volume.discover(client, {"firmware": True})


def test_firmware_skips_known_network_options_but_never_an_unresolved_prior_disk():
    client = Inventory()
    first = OPTION + "-pxe"
    client.body["Boot"]["BootOrder"].insert(0, "Boot0000")
    client.resources[OPTIONS]["Members"].append({"@odata.id": first})
    client.resources[first] = dict(client.resources[OPTION], BootOptionReference="Boot0000", Alias="Pxe", RelatedItem=[])
    assert redfish_root_volume.discover(client, {"firmware": True}) == PROOF
    client.resources[first]["Alias"] = "Hdd"
    with pytest.raises(redfish_root_volume.Refused):
        redfish_root_volume.discover(client, {"firmware": True})


def test_duplicate_naa_in_another_volume_or_a_physical_drive_refuses():
    for kind in ("Volume", "Drive"):
        client = Inventory()
        other = VOLUME + "-other"
        client.resources[other] = dict(client.resources[VOLUME], Id="OTHER", **{"@odata.type": "#" + kind + ".v1_0_0." + kind})
        if kind == "Volume":
            client.resources[VOLUMES]["Members"].append({"@odata.id": other})
        else:
            client.resources[CONTROLLER]["Drives"].append({"@odata.id": other})
        with pytest.raises(redfish_root_volume.Refused):
            redfish_root_volume.discover(client, REF)


@pytest.mark.parametrize("selector", [{"firmware": 1}, {"firmware": False}, {"firmware": "true"}, {},
                                      {"storageId": "RAID", "volumeId": "OS", "firmware": True}])
def test_selector_modes_are_closed_and_require_exact_types(selector):
    client = Inventory()
    with pytest.raises(redfish_root_volume.Refused):
        redfish_root_volume.discover(client, selector)
    assert client.reads == []
