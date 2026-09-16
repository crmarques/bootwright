"""The client is driven by what a controller advertises, never by its vendor.

Three firmware shapes are exercised through one fake transport: the emulator,
which keeps virtual media under the system and attaches synchronously; a
manager-scoped controller that attaches through a vendor extension and reports
the outcome in a task; and one that exposes the same device under both views
and demands a precondition on a write. No test names a vendor in the client.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control


class Firmware:
    """A controller that answers from a routing table and records writes."""

    def __init__(self, resources, writes=None):
        self.resources = resources
        self.writes = writes if writes is not None else []
        self.inserted = ""

    def __call__(self, url="", method="GET", payload=None, headers=None, timeout=None):
        url = url or ENDPOINT
        path = url.replace("https://bmc.test", "")
        if method != "GET":
            self.writes.append((method, path, payload, dict(headers or {})))
            return self.write(path, payload)
        answer = self.resources.get(path)
        if answer is None:
            return 404, {}, {}
        return 200, answer(self) if callable(answer) else answer, {}

    def write(self, path, payload):
        """A controller that performs the standard actions it advertises."""
        if path.endswith("VirtualMedia.InsertMedia"):
            self.inserted = payload["Image"]
        if path.endswith("VirtualMedia.EjectMedia"):
            self.inserted = ""
        return 204, {}, {}


ENDPOINT = "https://bmc.test/redfish/v1/Systems/1"


def client(firmware, endpoint=ENDPOINT):
    instance = redfish_control.Client(endpoint, "u", "p")
    instance.fetch = firmware
    return instance


SYSTEM = "/redfish/v1/Systems/1"


def emulator():
    """Virtual media under the system, attaching synchronously."""
    return Firmware({
        SYSTEM: {"PowerState": "Off", "UUID": "abc", "Model": "Standard PC"},
        SYSTEM + "/VirtualMedia": {"Members": [{"@odata.id": SYSTEM + "/VirtualMedia/Cd"}]},
        SYSTEM + "/VirtualMedia/Cd": lambda f: {
            "@odata.id": SYSTEM + "/VirtualMedia/Cd", "MediaTypes": ["CD"],
            "Inserted": bool(f.inserted), "Image": f.inserted,
            "Actions": {"#VirtualMedia.InsertMedia": {
                "target": SYSTEM + "/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia"}},
        },
    })


def manager_scoped():
    """Virtual media only under the manager, attached by a vendor extension."""
    manager = "/redfish/v1/Managers/1"
    firmware = Firmware({
        SYSTEM: {"PowerState": "Off", "Links": {"ManagedBy": [{"@odata.id": manager}]}},
        SYSTEM + "/VirtualMedia": None,
        manager: {"@odata.id": manager, "VirtualMedia": {"@odata.id": manager + "/VirtualMedia"}},
        manager + "/VirtualMedia": {"Members": [{"@odata.id": manager + "/VirtualMedia/CD"}]},
        manager + "/VirtualMedia/CD": lambda f: {
            "@odata.id": manager + "/VirtualMedia/CD", "MediaTypes": ["DVD"],
            "Inserted": bool(f.inserted), "Image": f.inserted,
            "Oem": {"Acme": {"Actions": {"#VirtualMedia.VmmControl": {
                "target": manager + "/VirtualMedia/CD/Actions/Oem/VmmControl",
                "@Redfish.ActionInfo": manager + "/VirtualMedia/CD/VmmControlInfo"}}}},
        },
        manager + "/VirtualMedia/CD/VmmControlInfo": {"Parameters": [
            {"Name": "Image"},
            {"Name": "VmmControlType", "AllowableValues": ["Connect", "Disconnect"]}]},
        "/redfish/v1/TaskService/Tasks/9": {"TaskState": "Completed", "TaskStatus": "OK"},
    })

    def write(path, payload):
        if path.endswith("/Oem/VmmControl"):
            firmware.inserted = payload["Image"]
            return 202, {"TaskMonitor": "/redfish/v1/TaskService/TaskMonitors/9/Monitor"}, {}
        return 204, {}, {}

    firmware.write = write
    return firmware


def dual_view():
    """The same device under both views, and a write that needs a precondition."""
    manager = "/redfish/v1/Managers/bmc"
    firmware = Firmware({
        SYSTEM: {"PowerState": "On", "@odata.etag": "W/tag",
                 "Links": {"ManagedBy": [{"@odata.id": manager}]},
                 "Actions": {"#ComputerSystem.Reset": {
                     "target": SYSTEM + "/Actions/ComputerSystem.Reset",
                     "ResetType@Redfish.AllowableValues": ["On", "ForceOn", "ForceOff"]}}},
        SYSTEM + "/VirtualMedia": {"Members": [{"@odata.id": manager + "/VirtualMedia/Cd"}]},
        manager: {"@odata.id": manager, "VirtualMedia": {"@odata.id": manager + "/VirtualMedia"}},
        manager + "/VirtualMedia": {"Members": [{"@odata.id": manager + "/VirtualMedia/Cd"}]},
        manager + "/VirtualMedia/Cd": lambda f: {
            "@odata.id": manager + "/VirtualMedia/Cd", "MediaTypes": ["CD"],
            "Inserted": bool(f.inserted), "Image": f.inserted,
            "Actions": {"#VirtualMedia.InsertMedia": {
                "target": manager + "/VirtualMedia/Cd/Actions/VirtualMedia.InsertMedia"}},
        },
    })

    def write(path, payload):
        if path == SYSTEM and not firmware.writes[-1][3].get("If-Match"):
            return 412, {}, {}
        if path.endswith("VirtualMedia.InsertMedia"):
            firmware.inserted = payload["Image"]
        if path.endswith("VirtualMedia.EjectMedia"):
            firmware.inserted = ""
        return 204, {}, {}

    firmware.write = write
    return firmware


SHAPES = {"emulator": emulator, "manager-scoped": manager_scoped, "dual-view": dual_view}


# Whichever view a controller keeps its optical device under, one member is
# found, and a controller exposing it twice is acted on once.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_the_optical_device_is_found_wherever_it_lives(shape):
    firmware = SHAPES[shape]()
    member = client(firmware).media_member()
    assert member and member.endswith(("/Cd", "/CD"))


# An attach is proved by reading the device back, so a controller that reports
# its outcome in a task and one that answers directly are both confirmed.
@pytest.mark.parametrize("shape", sorted(SHAPES))
def test_an_attach_is_confirmed_on_the_device_itself(shape):
    firmware = SHAPES[shape]()
    connected = client(firmware)
    attached, reason = connected.insert("https://server.test/os/m/install.iso", sleep=lambda _: None)
    assert attached, reason
    assert connected.inserted() == "https://server.test/os/m/install.iso"


# A vendor extension is used only where no standard action exists, and only
# because its own metadata declared what it accepts.
def test_a_vendor_extension_is_used_only_when_its_metadata_proves_it_fits():
    firmware = manager_scoped()
    client(firmware).insert("https://server.test/i.iso", sleep=lambda _: None)
    attaches = [w for w in firmware.writes if "VmmControl" in w[1]]
    assert attaches and attaches[0][2] == {"Image": "https://server.test/i.iso", "VmmControlType": "Connect"}

    firmware = manager_scoped()
    firmware.resources["/redfish/v1/Managers/1/VirtualMedia/CD/VmmControlInfo"] = {
        "Parameters": [{"Name": "Image"}]}
    attached, reason = client(firmware).insert("https://server.test/i.iso", sleep=lambda _: None)
    assert not attached and reason


# A controller that demands its current entity tag on a write is retried with
# one rather than being reported as refusing the boot selection.
def test_a_write_precondition_is_carried_and_retried():
    firmware = dual_view()
    assert client(firmware).boot_once("Cd")
    preconditions = [w[3].get("If-Match") for w in firmware.writes if w[0] == "PATCH"]
    assert preconditions and preconditions[-1]


# Power asks for the reset type the controller advertises, preferring the one
# that applies power without waiting on an operating system.
def test_power_on_uses_the_reset_type_the_controller_advertises():
    firmware = dual_view()
    firmware.resources[SYSTEM] = dict(firmware.resources[SYSTEM], PowerState="Off")
    connected = client(firmware)
    connected.power("On", "On", attempts=1, sleep=lambda _: None)
    resets = [w[2]["ResetType"] for w in firmware.writes if w[1].endswith("ComputerSystem.Reset")]
    assert resets == ["ForceOn"]


# An interface collection that cannot be read in full proves nothing, so a
# member that fails is reported rather than quietly shrinking the address set.
def test_an_unreadable_interface_member_is_a_failure_not_an_absence():
    firmware = emulator()
    firmware.resources[SYSTEM + "/EthernetInterfaces"] = {"Members": [
        {"@odata.id": SYSTEM + "/EthernetInterfaces/1"},
        {"@odata.id": SYSTEM + "/EthernetInterfaces/2"}]}
    firmware.resources[SYSTEM + "/EthernetInterfaces/1"] = {"MACAddress": "AA-BB-CC-DD-EE-01"}
    observed, failures = client(firmware).hardware_addresses()
    assert observed == ["aa:bb:cc:dd:ee:01"]
    assert failures

    firmware.resources[SYSTEM + "/EthernetInterfaces/2"] = {"PermanentMACAddress": "aabbccddee02"}
    observed, failures = client(firmware).hardware_addresses()
    assert observed == ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"] and not failures


# An ejected device is one that reports nothing, and a controller that removes
# the device entirely has ejected it too.
def test_an_eject_is_proved_by_the_device_reporting_nothing():
    firmware = emulator()
    firmware.inserted = "https://server.test/i.iso"
    assert client(firmware).eject(sleep=lambda _: None)

    vanishing = emulator()
    vanishing.inserted = "https://server.test/i.iso"
    connected = client(vanishing)
    connected.media_member()
    vanishing.resources[SYSTEM + "/VirtualMedia/Cd"] = None
    assert connected.eject(sleep=lambda _: None)
