"""A firmware declaration is compared with what a controller reports by pure functions.

The controller shapes that drive the whole step through urllib live with the
module, in tests/unit/plugins/modules/test_redfish_firmware.py.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_firmware


# JSON true and 1 are different answers, and Python's own equality does not
# tell them apart.
@pytest.mark.parametrize("declared, observed, equal", [
    (True, True, True),
    (True, 1, False),
    (1, True, False),
    (False, 0, False),
    (1, 1, True),
    ("1", 1, False),
    ("Enabled", "Enabled", True),
    ("Enabled", "enabled", False),
])
def test_typed_equality_tells_a_boolean_from_an_integer(declared, observed, equal):
    assert redfish_firmware.same(declared, observed) is equal


def test_a_boolean_attribute_read_back_as_one_is_a_difference():
    wanted = {redfish_firmware.BIOS + "TpmEnable": True, redfish_firmware.BIOS + "Cores": 8}
    bios = {"Attributes": {"TpmEnable": 1, "Cores": 8}}
    seen = redfish_firmware.observed(wanted, None, None, bios)
    assert redfish_firmware.differences(wanted, seen) == ["firmware.biosAttributes.TpmEnable"]


# The pending-settings resource is the one the controller links, never a
# conventional path built from the resource's own.
@pytest.mark.parametrize("resource, target", [
    ({"@odata.id": "/redfish/v1/Systems/1/Bios",
      "@Redfish.Settings": {"SettingsObject": {"@odata.id": "/redfish/v1/Systems/1/Bios/Pending"}}},
     "/redfish/v1/Systems/1/Bios/Pending"),
    ({"@odata.id": "/redfish/v1/Systems/1/Bios"}, ""),
    ({"@odata.id": "/redfish/v1/Systems/1/Bios", "@Redfish.Settings": {}}, ""),
    ({"@Redfish.Settings": {"SettingsObject": {"@odata.id": "  "}}}, ""),
    ({"@Redfish.Settings": {"SettingsObject": "/redfish/v1/Systems/1/Bios/Settings"}}, ""),
    (None, ""),
])
def test_the_settings_target_is_the_link_the_controller_names(resource, target):
    assert redfish_firmware.settings_target(resource) == target


def test_a_pending_setting_nobody_declared_is_foreign():
    current = {"TpmEnable": False, "BootMode": "Uefi", "Turbo": True}
    pending = {"TpmEnable": True, "BootMode": "Uefi", "Turbo": 1, "Fresh": "x"}
    assert redfish_firmware.foreign_pending(current, pending, {"TpmEnable"}) == ["Fresh", "Turbo"]
    assert redfish_firmware.foreign_pending(current, {"TpmEnable": True}, {"TpmEnable"}) == []
    assert redfish_firmware.foreign_pending(current, None, set()) == []


def test_a_pending_value_equal_to_the_current_one_is_not_foreign():
    current = {"Turbo": True, "Cores": 4}
    assert redfish_firmware.foreign_pending(current, {"Turbo": True, "Cores": 4}, set()) == []
    assert redfish_firmware.foreign_pending(current, {"Cores": "4"}, set()) == ["Cores"]


def test_a_declared_value_a_pending_setting_would_change_is_a_difference():
    wanted = {"firmware.biosAttributes.Cores": 8, "firmware.biosAttributes.Tpm": True}
    seen = {"firmware.biosAttributes.Cores": 8, "firmware.biosAttributes.Tpm": True}
    assert redfish_firmware.differences(wanted, seen, {"firmware.biosAttributes.Cores": 4}) == [
        "firmware.biosAttributes.Cores"]
    assert redfish_firmware.differences(wanted, seen, {"firmware.biosAttributes.Tpm": 1}) == [
        "firmware.biosAttributes.Tpm"]
    assert redfish_firmware.differences(wanted, seen, {"firmware.biosAttributes.Cores": 8}) == []


def test_differences_are_named_by_declared_field_path_in_declaration_order():
    mode, wanted = redfish_firmware.validated({
        "mode": "prove", "secureBoot": True, "bootMode": "UEFI",
        "biosAttributes": [{"name": "Zeta", "integer": 3}, {"name": "Alpha", "string": "On"}]})
    assert mode == "prove"
    assert list(wanted) == ["firmware.secureBoot", "firmware.bootMode", "firmware.biosAttributes.Alpha",
                            "firmware.biosAttributes.Zeta"]
    system = {"Boot": {"BootSourceOverrideMode": "Legacy"}}
    seen = redfish_firmware.observed(wanted, system, {"SecureBootEnable": True}, {"Attributes": {"Zeta": 3}})
    assert redfish_firmware.differences(wanted, seen) == ["firmware.bootMode", "firmware.biosAttributes.Alpha"]


def test_an_absent_mode_enforces():
    assert redfish_firmware.validated({"secureBoot": False})[0] == "enforce"


@pytest.mark.parametrize("firmware", [
    {"mode": "apply"},
    {"secureBoot": "true"},
    {"secureBoot": 1},
    {"bootMode": "Legacy"},
    {"unknown": True},
    {"biosAttributes": [{"name": "A1", "integer": True}]},
    {"biosAttributes": [{"name": "A1", "boolean": 1}]},
    {"biosAttributes": [{"name": "A1", "string": "x", "integer": 1}]},
    {"biosAttributes": [{"name": "A1"}]},
    {"biosAttributes": [{"name": "1A", "string": "x"}]},
    {"biosAttributes": [{"name": "A1", "string": "\x1b[31m"}]},
    {"biosAttributes": [{"name": "A1", "string": "x" * 257}]},
    {"biosAttributes": [{"name": "A1", "integer": 1 << 63}]},
    {"biosAttributes": [{"name": "A1", "float": 1.5}]},
    {"biosAttributes": [{"name": "A1", "string": "x"}, {"name": "A1", "string": "y"}]},
    {"biosAttributes": [{"name": "A%d" % index, "integer": index} for index in range(65)]},
    ["secureBoot"],
], ids=repr)
def test_a_declaration_of_another_shape_is_not_accepted(firmware):
    with pytest.raises(ValueError):
        redfish_firmware.validated(firmware)


def test_attribute_values_pass_verbatim_with_their_types():
    template = "{{ lookup('pipe', 'id') }}"
    wanted = redfish_firmware.validated({"biosAttributes": [
        {"name": "Banner", "string": template}, {"name": "Cores", "integer": -1}, {"name": "Tpm", "boolean": False}]})[1]
    assert wanted == {"firmware.biosAttributes.Banner": template, "firmware.biosAttributes.Cores": -1,
                      "firmware.biosAttributes.Tpm": False}


def test_an_advertised_value_list_without_the_value_excludes_it():
    boot = {"BootSourceOverrideMode@Redfish.AllowableValues": ["Legacy"]}
    assert redfish_firmware.excludes(boot, "BootSourceOverrideMode", "UEFI") is True
    assert redfish_firmware.excludes({}, "BootSourceOverrideMode", "UEFI") is False


def test_on_reset_is_used_only_where_it_is_advertised():
    advertised = {"@Redfish.Settings": {"SupportedApplyTimes": ["OnReset", "Immediate"]}}
    assert redfish_firmware.applies_on_reset(advertised, {}) is True
    allowed = {"@Redfish.SettingsApplyTime": {"ApplyTime@Redfish.AllowableValues": ["OnReset"]}}
    assert redfish_firmware.applies_on_reset({}, allowed) is True
    assert redfish_firmware.applies_on_reset({"@Redfish.Settings": {"SupportedApplyTimes": ["Immediate"]}}, {}) is False


def test_message_identifiers_are_read_from_settings_tasks_and_errors_only_when_bounded():
    body = {"@Redfish.Settings": {"Messages": [{"MessageId": "Bios.1.0.SettingsFailed", "Message": "value 7"}]},
            "Messages": {"MessageId": "Task.1.0.Failed"},
            "error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.PropertyModificationNeedPrivilege"},
                                                {"MessageId": "not an identifier"}]}}
    assert redfish_firmware.message_ids(body) == [
        "Task.1.0.Failed", "Bios.1.0.SettingsFailed", "Base.1.8.PropertyModificationNeedPrivilege"]
