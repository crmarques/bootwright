"""What a controller offers is read from its own metadata, by pure functions.

The firmware shapes that drive the client through these readings live with the
module that consumes them, in tests/unit/plugins/modules/test_redfish_boot.py.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery


# A completed task has done what it was asked; its TaskStatus reports a
# condition beside the outcome, and the device read back afterwards decides.
@pytest.mark.parametrize("task, verdict", [
    ({"TaskState": "Completed", "TaskStatus": "OK"}, (True, True)),
    ({"TaskState": "Completed", "TaskStatus": "Warning"}, (True, True)),
    ({"TaskState": "Completed"}, (True, True)),
    ({"TaskState": "Exception", "TaskStatus": "Critical"}, (True, False)),
    ({"TaskState": "Killed"}, (True, False)),
    ({"TaskState": "Cancelled"}, (True, False)),
    ({"TaskState": "Interrupted"}, (False, False)),
    ({"TaskState": "Suspended"}, (False, False)),
    ({"TaskState": "Running"}, (False, False)),
    ({"TaskState": "New"}, (False, False)),
    ({"TaskState": "Pending"}, (False, False)),
    ({}, (False, False)),
    (None, (False, False)),
    ([], (False, False)),
])
def test_a_task_settles_only_in_a_terminal_state(task, verdict):
    assert redfish_discovery.task_settled(task) == verdict


# A monitor is normalized to the task that carries the state, and the Location
# header is read under the lower-cased name the client gives every header.
def test_a_task_reference_names_the_task_itself():
    monitor = {"TaskMonitor": "/redfish/v1/TaskService/TaskMonitors/9/Monitor"}
    assert redfish_discovery.task_reference(monitor, {}) == "/redfish/v1/TaskService/Tasks/9"
    headers = {"location": "/redfish/v1/TaskService/TaskMonitors/4"}
    assert redfish_discovery.task_reference(None, headers) == "/redfish/v1/TaskService/Tasks/4"
    assert redfish_discovery.task_reference({}, {}) == ""


# The captured iBMC echoes an image without its non-default port, so an echo
# naming no port leaves the port uncompared; a port the echo names must be the
# requested one, or the scheme's default when the request names none.
@pytest.mark.parametrize("observed, expected, matches", [
    ("https://server.test/os/i.iso", "https://server.test:443/os/i.iso", True),
    ("http://server.test/os/i.iso", "http://server.test:80/os/i.iso", True),
    ("https://SERVER.test:8443/os/i.iso", "https://server.test:8443/os/i.iso", True),
    ("https://server.test/os/i.iso", "https://server.test:8443/os/i.iso", True),
    ("https://192.0.2.10/install.iso", "https://192.0.2.10:8443/install.iso", True),
    ("https://server.test:9443/os/i.iso", "https://server.test:8443/os/i.iso", False),
    ("https://server.test:8443/os/i.iso", "https://server.test/os/i.iso", False),
    ("http://server.test:8443/os/i.iso", "https://server.test:8443/os/i.iso", False),
    ("https://server.test:8443/os/other.iso", "https://server.test:8443/os/i.iso", False),
    ("https://server.test:port/os/i.iso", "https://server.test:8443/os/i.iso", False),
    ("", "https://server.test:8443/os/i.iso", False),
])
def test_an_echoed_image_matches_unless_it_names_another_port(observed, expected, matches):
    assert redfish_discovery.image_matches(observed, expected) is matches


def reset(**action):
    target = {"target": "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset"}
    return {"Actions": {"#ComputerSystem.Reset": dict(target, **action)}}


# The reset types come from the action, else from its ActionInfo, and None
# means the controller named none, which is not the same as an empty list.
def test_the_allowed_reset_types_are_what_the_controller_advertises():
    advertised = reset(**{"ResetType@Redfish.AllowableValues": ["On", "ForceOff", 7]})
    assert redfish_discovery.allowed_reset_types(advertised, None) == ["On", "ForceOff"]
    info = {"Parameters": [{"Name": "ResetType", "AllowableValues": ["ForceOn", "GracefulShutdown"]}]}
    described = reset(**{"@Redfish.ActionInfo": "/redfish/v1/Systems/1/ResetActionInfo"})
    assert redfish_discovery.allowed_reset_types(described, info) == ["ForceOn", "GracefulShutdown"]
    assert redfish_discovery.allowed_reset_types(advertised, info) == ["On", "ForceOff"]
    assert redfish_discovery.allowed_reset_types(reset(), None) is None
    assert redfish_discovery.allowed_reset_types({}, {"Parameters": "none"}) is None
    assert redfish_discovery.allowed_reset_types(reset(**{"ResetType@Redfish.AllowableValues": []}), None) == []


# Only a bounded identifier from the controller reaches a failure line, never
# the free text beside it.
@pytest.mark.parametrize("body, found", [
    ({"Messages": [{"MessageId": "ConnectionFailed", "Message": "free text"}]}, "ConnectionFailed"),
    ({"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.InsufficientPrivilege"}]}},
     "Base.1.8.InsufficientPrivilege"),
    ({"Messages": [{"MessageId": "Base.1.8.Error\n"}]}, ""),
    ({"Messages": [{"MessageId": "a message with spaces"}]}, ""),
    ({"Messages": [{"MessageId": "x" * 80}]}, "x" * 64),
    ({"Messages": []}, ""),
    ({"Messages": {"MessageId": "iBMC.1.0.ConnectionFailed", "Message": "free text"}}, "iBMC.1.0.ConnectionFailed"),
    ({"Messages": {"Message": "x"}}, ""),
    (None, ""),
])
def test_a_message_identifier_is_kept_only_when_it_is_one(body, found):
    assert redfish_discovery.message_id(body) == found


# A manager links its security service at the top level, or, as the captured
# iBMC does beside a null top-level property, under its vendor's Oem section.
@pytest.mark.parametrize("manager, found", [
    ({"SecurityService": {"@odata.id": "/redfish/v1/Managers/1/SecurityService"}},
     "/redfish/v1/Managers/1/SecurityService"),
    ({"SecurityService": None, "Oem": {"Acme": {"SecurityService": {"@odata.id": "/redfish/v1/Managers/1/Sec"}}}},
     "/redfish/v1/Managers/1/Sec"),
    ({"SecurityService": {"@odata.id": "/top"}, "Oem": {"Acme": {"SecurityService": {"@odata.id": "/oem"}}}}, "/top"),
    ({"Oem": {"Acme": {"SecurityService": {"@odata.id": ""}}, "Zeta": {"SecurityService": {"@odata.id": "/z"}}}}, "/z"),
    ({"SecurityService": {"@odata.id": 7}, "Oem": {"Acme": "none"}}, ""),
    ({"@odata.id": "/redfish/v1/Managers/1"}, ""),
    (None, ""),
])
def test_a_security_service_is_found_where_the_manager_links_it(manager, found):
    assert redfish_discovery.security_service(manager) == found


def test_power_on_prefers_a_type_that_does_not_wait_on_an_operating_system():
    assert redfish_discovery.power_on_reset_type(["On", "ForceOn"]) == "ForceOn"
    assert redfish_discovery.power_on_reset_type(["PushPowerButton", "On"]) == "On"
    assert redfish_discovery.power_on_reset_type(["ForceOff", "Nmi"]) == ""
