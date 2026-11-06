"""Serial-selected agent roots are proved uniquely before any write."""

from __future__ import annotations

import copy
from urllib.parse import urlsplit

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_root_disk
from ansible_collections.bootwright.core.plugins.modules import redfish_system_inspect


ROOT = "/redfish/v1/Systems/1/Storage"
STORAGE = ROOT + "/1"
DRIVE = STORAGE + "/Drives/1"
EXPECTED = {"serialNumber": "SYNTHETIC-ROOT-01", "sizeBytes": 8192}


class Inventory:
    def __init__(self):
        self.native = redfish_control.Client("https://bmc.example.test/redfish/v1/Systems/1", "operator", "never-expose")
        self.body = {"Storage": {"@odata.id": ROOT}}
        self.resources = {
            ROOT: {"Members": [{"@odata.id": STORAGE}], "Members@odata.count": 1},
            STORAGE: {"Drives": [{"@odata.id": DRIVE}]},
            DRIVE: {"@odata.type": "#Drive.v1_19_0.Drive", "SerialNumber": EXPECTED["serialNumber"], "CapacityBytes": 8192},
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


def test_one_whole_drive_with_exact_serial_and_size_is_proved_without_writes():
    client = Inventory()
    before = copy.deepcopy(client.resources)
    redfish_root_disk.prove(client, EXPECTED)
    assert len(client.reads) == 3 and client.resources == before


@pytest.mark.parametrize("second_size", [8192, 16384])
def test_a_second_serial_match_refuses_even_when_its_size_differs(second_size):
    client = Inventory()
    client.resources[STORAGE]["Drives"].append({"@odata.id": DRIVE + "-other"})
    client.resources[DRIVE + "-other"] = dict(client.resources[DRIVE], CapacityBytes=second_size)
    with pytest.raises(redfish_root_disk.Refused, match="proved uniquely"):
        redfish_root_disk.prove(client, EXPECTED)


@pytest.mark.parametrize("change", [
    {"SerialNumber": "OTHER"}, {"SerialNumber": ""}, {"SerialNumber": None},
    {"CapacityBytes": 16384}, {"CapacityBytes": True}, {"CapacityBytes": "8192"},
    {"@odata.type": "#Volume.v1_0_0.Volume"},
])
def test_missing_identity_wrong_capacity_or_non_drive_never_proves_the_root(change):
    client = Inventory()
    client.resources[DRIVE].update(change)
    with pytest.raises(redfish_root_disk.Refused):
        redfish_root_disk.prove(client, EXPECTED)


@pytest.mark.parametrize("change", [
    {"Members": []}, {"Members@odata.count": 2},
    {"Members@odata.nextLink": ROOT + "?page=2"}, {"@odata.nextLink": ROOT + "?page=2"},
    {"Members@odata.nextLink": None}, {"@odata.nextLink": ""},
])
def test_an_incomplete_storage_inventory_refuses(change):
    client = Inventory()
    client.resources[ROOT].update(change)
    with pytest.raises(redfish_root_disk.Refused):
        redfish_root_disk.prove(client, EXPECTED)


def test_an_unreadable_drive_or_foreign_link_never_grants_a_proof():
    client = Inventory()
    del client.resources[DRIVE]
    with pytest.raises(redfish_control.ControllerError):
        redfish_root_disk.prove(client, EXPECTED)
    client = Inventory()
    client.resources[STORAGE]["Drives"] = [{"@odata.id": "https://foreign.example.test/drive"}]
    with pytest.raises(redfish_control.ControllerError):
        redfish_root_disk.prove(client, EXPECTED)
    assert len(client.reads) == 2


@pytest.mark.parametrize("change", [
    {"Drives@odata.count": 2}, {"Drives@odata.count": True}, {"Drives@odata.count": None},
    {"Drives@odata.nextLink": None}, {"Drives@odata.nextLink": ""}, {"@odata.nextLink": False},
])
def test_a_truncated_or_malformed_drive_list_refuses(change):
    client = Inventory()
    client.resources[STORAGE].update(change)
    with pytest.raises(redfish_root_disk.Refused):
        redfish_root_disk.prove(client, EXPECTED)


def test_oversized_and_duplicated_storage_inventories_are_not_complete_proofs():
    for members in [[{"@odata.id": STORAGE}] * 2, [{"@odata.id": STORAGE}] * 33]:
        client = Inventory()
        client.resources[ROOT] = {"Members": members, "Members@odata.count": len(members)}
        with pytest.raises(redfish_root_disk.Refused):
            redfish_root_disk.prove(client, EXPECTED)


def test_the_same_drive_linked_by_two_storage_resources_is_counted_once():
    client = Inventory()
    client.resources[ROOT]["Members"].append({"@odata.id": STORAGE + "-other"})
    client.resources[ROOT]["Members@odata.count"] = 2
    client.resources[STORAGE + "-other"] = copy.deepcopy(client.resources[STORAGE])
    redfish_root_disk.prove(client, EXPECTED)
    assert len([url for url in client.reads if url.endswith("/Drives/1")]) == 1


@pytest.mark.parametrize("expected", [
    {}, {"serialNumber": "SYNTHETIC-ROOT-01", "sizeBytes": True},
    {"serialNumber": "SYNTHETIC-ROOT-01", "sizeBytes": 8193},
    {"serialNumber": "SYNTHETIC-ROOT-01", "sizeBytes": 8192, "extra": "refused"},
    {"serialNumber": "bad serial", "sizeBytes": 8192},
])
def test_malformed_frozen_proofs_refuse_before_any_read(expected):
    client = Inventory()
    with pytest.raises(redfish_root_disk.Refused):
        redfish_root_disk.prove(client, expected)
    assert not client.reads


def test_the_module_reports_a_sanitized_disk_refusal_without_an_effect(monkeypatch):
    client = Inventory()
    client.resources[DRIVE]["CapacityBytes"] = 16384
    params = dict(endpoint=client.native.endpoint, user="operator", password="never-expose", verify=True,
                  ca_data="", root_disk=EXPECTED)
    failed = {}

    class Module:
        def __init__(self, **kwargs):
            self.params = params

        def fail_json(self, **kwargs):
            failed.update(kwargs)

        def exit_json(self, **kwargs):
            pytest.fail("a refused disk reached module success")

    monkeypatch.setattr(redfish_system_inspect, "AnsibleModule", Module)
    monkeypatch.setattr(redfish_control, "Client", lambda *args, **kwargs: client)
    monkeypatch.setattr(redfish_system_inspect, "observe", lambda _client: {})
    redfish_system_inspect.main()
    assert failed == {"msg": redfish_root_disk.REFUSAL, "root_disk_refused": True, "unanswered": False}
