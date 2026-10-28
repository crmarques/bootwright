"""Declared firmware settings are applied or proved through the client's one request path.

A controller answers through urllib itself, as in test_redfish_boot.py, so
every case runs through the client's own requests: a system that links its
SecureBoot and Bios resources, each publishing a pending-settings object with
OnReset advertised, and applying pending settings at a power-on that stops in
BiosSetup. Cases vary it: a 202 task, a 412 precondition, a read-only 400 as
the captured BMC answers one (.agents/knowledge/redfish-physical-bmc.md),
settings that never apply, a setup boot that is not kept, no settings object,
a link to another host and a 401. These protocol cases do not establish
hardware qualification.
"""

from __future__ import annotations

import http.client
import io
import json
import signal
import socket
import threading
import urllib.error
from urllib.parse import urlsplit

import ssl

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.module_utils import redfish_firmware as firmware_steps
from ansible_collections.bootwright.core.plugins.modules import redfish_firmware

PASSWORD = "p4ssw0rd-never-shown"
SYSTEM = "/redfish/v1/Systems/1"
SECURE, SECURE_SETTINGS = SYSTEM + "/SecureBoot", SYSTEM + "/SecureBoot/Settings"
BIOS, BIOS_SETTINGS = SYSTEM + "/Bios", SYSTEM + "/Bios/Settings"
RESET = SYSTEM + "/Actions/ComputerSystem.Reset"
TASK = "/redfish/v1/TaskService/Tasks/7"
RESET_INFO = SYSTEM + "/ResetActionInfo"
STOPPED = "the step was asked to stop"
LATE = "the writes took longer than their share of the bound; the machine was not powered on"
REPORTED = "reported-banner-never-shown"
DECLARED = {"mode": "enforce", "secureBoot": True, "bootMode": "UEFI",
            "biosAttributes": [{"name": "TpmEnable", "boolean": True}, {"name": "Cores", "integer": 8}]}


class Answer:
    """A 2xx answer the way urllib hands one back."""

    def __init__(self, status, raw, headers):
        self.status, self.headers, self.stream = status, headers, io.BytesIO(raw)

    def read(self, amount=-1):
        return self.stream.read(amount)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def respond(url, status, body=None, headers=None):
    """Answer as urllib does: a 2xx as a response, anything else raised as HTTPError."""
    raw = b"" if body is None else json.dumps(body).encode("utf-8")
    message = http.client.HTTPMessage()
    for name, value in (headers or {}).items():
        message[name] = value
    if 200 <= status < 300:
        return Answer(status, raw, message)
    raise urllib.error.HTTPError(url, status, "HTTP %d" % status, message, io.BytesIO(raw))


class Controller:
    """A system with SecureBoot and Bios resources and their pending settings.

    Pending settings apply when the machine powers on with a one-time BiosSetup
    boot selected, unless `applies` is off. A script queues answers for one
    method and path ahead of the routing; a callable answer may change the
    controller first. Every request is recorded. Time is the client's pauses
    and, for a request `stalls` names, the whole timeout it was given; a request
    `slow` names answers only after the seconds it returns, as a BMC trickling
    its answer does whatever socket timeout the request was given.
    """

    def __init__(self, mode="Legacy", secure=False, attributes=None):
        self.endpoint = "https://bmc.test" + SYSTEM
        self.power, self.applies, self.keeps_setup = "Off", True, True
        self.boot = {"BootSourceOverrideEnabled": "Disabled", "BootSourceOverrideTarget": "None",
                     "BootSourceOverrideMode": mode}
        self.secure, self.secure_pending = secure, None
        self.attributes = dict({"TpmEnable": False, "Cores": 4, "Banner": REPORTED}, **(attributes or {}))
        self.pending = {}
        self.calls, self.pauses, self.scripts, self.tags = [], [], {}, {}
        self.extra, self.waiting = {}, []
        self.now, self.stalls, self.stalled, self.posted = 0.0, None, 0, []
        self.slow, self.moments, self.action_info = None, [], False

    def clock(self):
        return self.now

    def pause(self, delay):
        """The client's sleep: recorded, and the moment a queued effect may land."""
        self.pauses.append(delay)
        self.now += delay
        while self.waiting and self.waiting[0][0] == delay:
            self.waiting.pop(0)[1](self)

    def opener(self, verify=True, ca_data=""):
        return self

    def script(self, method, path, *answers):
        self.scripts.setdefault((method, path), []).extend(answers)

    def open(self, request, timeout=None):
        method, url = request.get_method(), request.full_url
        headers = {name.lower(): value for name, value in request.header_items()}
        payload = json.loads(request.data) if request.data else None
        self.calls.append((method, url, headers, payload))
        path = urlsplit(url).path
        if method == "POST":
            self.posted.append((self.now, (payload or {}).get("ResetType")))
        self.moments.append((self.now, method, path))
        if self.slow is not None:
            self.now += self.slow(self, method, path)
        if self.stalls is not None and self.stalls(self, method, path):
            self.stalled += 1
            self.now += timeout
            raise socket.timeout("timed out")
        queued = self.scripts.get((method, path))
        answer = queued.pop(0) if queued else None
        if answer is None:
            answer = self.route(path) if method == "GET" else self.write(method, path, payload)
        if callable(answer):
            answer = answer(self)
        return respond(url, *answer)

    def route(self, path):
        resources = {
            SYSTEM: {"@odata.id": SYSTEM, "PowerState": self.power, "UUID": "u-1",
                     "Boot": dict(self.boot, **{
                         "BootSourceOverrideTarget@Redfish.AllowableValues": ["None", "Cd", "Hdd", "BiosSetup"],
                         "BootSourceOverrideMode@Redfish.AllowableValues": ["Legacy", "UEFI"]}),
                     "SecureBoot": {"@odata.id": SECURE}, "Bios": {"@odata.id": BIOS},
                     "Actions": {"#ComputerSystem.Reset": {"target": RESET, "@Redfish.ActionInfo": RESET_INFO}
                                 if self.action_info else {
                         "target": RESET, "ResetType@Redfish.AllowableValues": ["On", "ForceOff", "ForceOn"]}}},
            RESET_INFO: {"@odata.id": RESET_INFO, "Parameters": [
                {"Name": "ResetType", "AllowableValues": ["On", "ForceOff"]}]},
            SECURE: {"@odata.id": SECURE, "SecureBootEnable": self.secure, "SecureBootCurrentBoot": "Disabled",
                     "@Redfish.Settings": {"SettingsObject": {"@odata.id": SECURE_SETTINGS},
                                           "SupportedApplyTimes": ["OnReset"]}},
            SECURE_SETTINGS: {"@odata.id": SECURE_SETTINGS, "SecureBootEnable":
                              self.secure if self.secure_pending is None else self.secure_pending},
            BIOS: {"@odata.id": BIOS, "Attributes": dict(self.attributes),
                   "@Redfish.Settings": {"SettingsObject": {"@odata.id": BIOS_SETTINGS},
                                         "SupportedApplyTimes": ["OnReset"],
                                         "Messages": [{"MessageId": "Bios.1.0.SettingsApplyFailed"}]}},
            BIOS_SETTINGS: {"@odata.id": BIOS_SETTINGS, "Attributes": dict(self.pending)},
            TASK: {"@odata.id": TASK, "TaskState": "Completed", "TaskStatus": "OK"},
        }
        resources.update(self.extra)
        if path not in resources:
            return 404, {"error": {"@Message.ExtendedInfo": [{"MessageId": "Base.1.8.ResourceMissingAtURI"}]}}, {}
        self.tags[path] = self.tags.get(path, 0) + 1
        return 200, resources[path], {"ETag": 'W/"%s-%d"' % (path.rsplit("/", 1)[1], self.tags[path])}

    def write(self, method, path, payload):
        if method == "PATCH" and path == SYSTEM:
            boot = payload["Boot"]
            if self.keeps_setup or boot.get("BootSourceOverrideTarget") != "BiosSetup":
                self.boot.update(boot)
        elif method == "PATCH" and path == SECURE_SETTINGS:
            self.secure_pending = payload["SecureBootEnable"]
        elif method == "PATCH" and path == BIOS_SETTINGS:
            self.pending.update(payload["Attributes"])
        elif method == "POST" and path == RESET:
            self.reset(payload["ResetType"])
        else:
            return 405, None, {}
        return 204, None, {}

    def reset(self, kind):
        if kind in ("On", "ForceOn"):
            self.power = "On"
            if self.boot["BootSourceOverrideTarget"] == "BiosSetup" and self.applies:
                self.attributes.update(self.pending)
                self.pending = {}
                if self.secure_pending is not None:
                    self.secure, self.secure_pending = self.secure_pending, None
            self.boot.update(BootSourceOverrideEnabled="Disabled", BootSourceOverrideTarget="None")
        else:
            self.power = "Off"


def drive(monkeypatch, controller, firmware=None):
    """One module invocation: a fresh client, as main() builds one."""
    monkeypatch.setattr(redfish_control, "_opener", controller.opener)
    client = redfish_control.Client(controller.endpoint, "operator", PASSWORD)
    client.sleep, client.clock = controller.pause, controller.clock
    return redfish_firmware.drive(client, DECLARED if firmware is None else firmware)


def writes(controller, since=0):
    return [(call[0], urlsplit(call[1]).path, call[3]) for call in controller.calls[since:] if call[0] != "GET"]


def resets(controller):
    return [payload["ResetType"] for method, path, payload in writes(controller) if path == RESET]


def patches(controller):
    return [path for method, path, _payload in writes(controller) if method == "PATCH"]


def applied():
    return Controller(mode="UEFI", secure=True, attributes={"TpmEnable": True, "Cores": 8})


def test_settings_that_already_read_as_declared_are_only_read(monkeypatch):
    controller = applied()
    result = drive(monkeypatch, controller)
    assert result["changed"] is False and result["refusal"] == "" and result["fields"] == []
    assert writes(controller) == [] and controller.pauses == []


def test_a_proof_that_finds_differences_writes_resets_and_powers_nothing(monkeypatch):
    controller = Controller()
    result = drive(monkeypatch, controller, dict(DECLARED, mode="prove"))
    assert writes(controller) == []
    assert result == {"changed": False, "refusal": "firmware-setting-mismatch", "power": "Off", "messages": [],
                      "reasons": [], "fields": ["firmware.secureBoot", "firmware.bootMode",
                                                "firmware.biosAttributes.Cores", "firmware.biosAttributes.TpmEnable"]}


def test_enforcing_boots_into_setup_writes_each_setting_powers_on_reads_and_powers_off(monkeypatch):
    controller = Controller()
    result = drive(monkeypatch, controller)
    assert result == {"changed": True, "refusal": "", "fields": [], "reasons": [], "messages": [], "power": "Off"}
    sent = writes(controller)
    assert [(method, path) for method, path, _payload in sent] == [
        ("PATCH", SYSTEM), ("PATCH", SYSTEM), ("PATCH", SECURE_SETTINGS), ("PATCH", BIOS_SETTINGS),
        ("POST", RESET), ("POST", RESET)]
    assert sent[0][2] == {"Boot": {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": "BiosSetup"}}
    assert sent[1][2] == {"Boot": {"BootSourceOverrideMode": "UEFI"}}
    assert sent[2][2] == {"SecureBootEnable": True, "@Redfish.SettingsApplyTime": {"ApplyTime": "OnReset"}}
    assert sent[3][2] == {"Attributes": {"Cores": 8, "TpmEnable": True},
                          "@Redfish.SettingsApplyTime": {"ApplyTime": "OnReset"}}
    assert resets(controller) == ["ForceOn", "ForceOff"]
    # Every write carries the tag of the read just before it.
    for index, call in enumerate(controller.calls):
        if call[0] == "PATCH" and urlsplit(call[1]).path != SYSTEM:
            before = controller.calls[index - 1]
            assert before[0] == "GET" and before[1] == call[1] and call[2]["if-match"].startswith('W/"')
    # The poll read the settings while the machine was on, and the proof read
    # them again after it was off.
    order = [(call[0], urlsplit(call[1]).path, (call[3] or {}).get("ResetType")) for call in controller.calls]
    on, off = order.index(("POST", RESET, "ForceOn")), order.index(("POST", RESET, "ForceOff"))
    assert ("GET", BIOS, None) in order[on:off] and ("GET", SECURE, None) in order[on:off]
    assert ("GET", BIOS, None) in order[off:] and ("GET", SECURE, None) in order[off:]
    assert controller.pauses.count(redfish_control.FIRMWARE_POLL_DELAY) == 1


def test_a_write_answered_with_a_task_is_polled_and_then_read_back(monkeypatch):
    controller = Controller()

    def accepted(firmware):
        firmware.pending.update({"Cores": 8, "TpmEnable": True})
        return 202, {}, {"Location": TASK + "/Monitor"}
    controller.script("PATCH", BIOS_SETTINGS, accepted)
    result = drive(monkeypatch, controller)
    assert result["refusal"] == ""
    assert ("GET", TASK) in [(call[0], urlsplit(call[1]).path) for call in controller.calls]


def test_a_refused_precondition_is_retried_once_with_the_tag_read_again(monkeypatch):
    controller = Controller()
    controller.script("PATCH", BIOS_SETTINGS, (412, {"error": {"@Message.ExtendedInfo": [
        {"MessageId": "Base.1.8.PreconditionFailed"}]}}, {}))
    result = drive(monkeypatch, controller)
    assert result["refusal"] == ""
    tags = [call[2].get("if-match") for call in controller.calls if call[0] == "PATCH" and urlsplit(call[1]).path == BIOS_SETTINGS]
    assert len(tags) == 2 and tags[0] != tags[1] and all(tag.startswith('W/"Settings-') for tag in tags)


def test_a_read_only_answer_is_not_applied_with_the_controllers_message(monkeypatch):
    controller = Controller()
    controller.script("PATCH", BIOS_SETTINGS, (400, {"error": {"@Message.ExtendedInfo": [
        {"MessageId": "iBMC.1.0.PropertyModificationNeedPrivilege"}]}}, {}))
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied" and result["changed"] is True
    assert "iBMC.1.0.PropertyModificationNeedPrivilege" in result["messages"]
    assert result["fields"] == ["firmware.biosAttributes.Cores", "firmware.biosAttributes.TpmEnable"]
    assert resets(controller) == []


def test_settings_that_never_apply_refuse_after_the_bound_and_the_machine_is_powered_off(monkeypatch):
    controller = Controller()
    controller.applies = False
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert result["fields"] == ["firmware.secureBoot", "firmware.biosAttributes.Cores",
                                "firmware.biosAttributes.TpmEnable"]
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off"
    assert controller.pauses.count(redfish_control.FIRMWARE_POLL_DELAY) == redfish_control.FIRMWARE_POLLS
    assert "Bios.1.0.SettingsApplyFailed" in result["messages"]
    assert result["power"] == "Off"


def test_a_setup_boot_that_does_not_read_back_refuses_before_any_setting_is_written(monkeypatch):
    controller = Controller()
    controller.keeps_setup = False
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "BiosSetup" in result["reasons"][0]
    assert patches(controller) == [SYSTEM] and resets(controller) == []


def test_a_bios_without_a_settings_object_refuses_before_any_write(monkeypatch):
    controller = Controller()
    controller.extra[BIOS] = {"@odata.id": BIOS, "Attributes": {"TpmEnable": False, "Cores": 4}}
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied" and result["changed"] is False
    assert "the Bios resource publishes no SettingsObject" in result["reasons"]
    assert writes(controller) == []


def test_a_pending_change_nobody_declared_refuses_before_any_write(monkeypatch):
    controller = Controller()
    controller.pending = {"Turbo": "Disabled"}
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert any("pending" in reason for reason in result["reasons"])
    assert writes(controller) == []


def test_a_mode_the_system_does_not_offer_refuses_before_any_write(monkeypatch):
    controller = Controller()
    controller.extra[SYSTEM] = dict(controller.route(SYSTEM)[1])
    controller.extra[SYSTEM]["Boot"] = dict(controller.extra[SYSTEM]["Boot"], **{
        "BootSourceOverrideMode@Redfish.AllowableValues": ["Legacy"]})
    result = drive(monkeypatch, controller, {"bootMode": "UEFI"})
    assert result["refusal"] == "firmware-setting-not-applied" and writes(controller) == []


def test_a_link_to_another_host_is_never_followed(monkeypatch):
    controller = Controller()
    controller.extra[SYSTEM] = dict(controller.route(SYSTEM)[1], Bios={"@odata.id": "https://other.test" + BIOS})
    with pytest.raises(redfish_control.ControllerError, match="not this controller's endpoint"):
        drive(monkeypatch, controller)
    assert all(urlsplit(call[1]).hostname == "bmc.test" for call in controller.calls)
    assert writes(controller) == []


def test_a_refused_credential_ends_the_step_at_once(monkeypatch):
    controller = Controller()
    controller.script("GET", SYSTEM, (401, None, {}))
    with pytest.raises(redfish_control.ControllerError, match="HTTP 401"):
        drive(monkeypatch, controller)
    assert len(controller.calls) == 1


def test_a_refused_write_privilege_ends_the_step_at_once(monkeypatch):
    controller = Controller()
    controller.script("PATCH", SECURE_SETTINGS, (403, None, {}))
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    refused = next(index for index, call in enumerate(controller.calls) if call[0] == "PATCH" and SECURE_SETTINGS in call[1])
    assert controller.calls[refused + 1:] == []


def test_a_boot_mode_alone_is_written_without_a_reset(monkeypatch):
    controller = applied()
    controller.boot["BootSourceOverrideMode"] = "Legacy"
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "" and result["changed"] is True
    assert patches(controller) == [SYSTEM] and resets(controller) == []


def test_a_refusal_names_no_reported_value_and_no_credential(monkeypatch):
    controller = Controller()
    controller.applies = False
    result = drive(monkeypatch, controller, dict(DECLARED, biosAttributes=[{"name": "Banner", "string": "declared"}]))
    text = json.dumps(result)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "firmware.biosAttributes.Banner" in result["fields"]
    for hidden in (REPORTED, PASSWORD, "Disabled"):
        assert hidden not in text


def requested(controller, method, path):
    return [index for index, call in enumerate(controller.calls) if call[0] == method and urlsplit(call[1]).path == path]


def refused_certificate(firmware):
    firmware.fired = True
    raise urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed"))


def test_a_machine_found_running_is_refused_before_any_write(monkeypatch):
    controller = Controller()
    controller.power = "On"
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied" and result["changed"] is False
    assert "the system does not report PowerState Off" in result["reasons"]
    assert writes(controller) == []


def test_a_pending_value_other_than_the_declared_one_is_a_mismatch_under_proof(monkeypatch):
    controller = applied()
    controller.pending = {"Cores": 4}
    result = drive(monkeypatch, controller, dict(DECLARED, mode="prove"))
    assert result["refusal"] == "firmware-setting-mismatch" and result["fields"] == ["firmware.biosAttributes.Cores"]
    assert writes(controller) == []


def test_a_pending_value_other_than_the_declared_one_is_written_under_enforcement(monkeypatch):
    controller = applied()
    controller.pending = {"Cores": 4}
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "" and result["changed"] is True
    bios = [payload for method, path, payload in writes(controller) if path == BIOS_SETTINGS]
    assert bios == [{"Attributes": {"Cores": 8}, "@Redfish.SettingsApplyTime": {"ApplyTime": "OnReset"}}]
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.attributes["Cores"] == 8


def test_a_setup_boot_a_later_write_cleared_is_refused_before_the_power_cycle(monkeypatch):
    controller = Controller()

    def clears(firmware):
        firmware.pending.update({"Cores": 8, "TpmEnable": True})
        firmware.boot.update(BootSourceOverrideEnabled="Disabled", BootSourceOverrideTarget="None")
        return 204, None, {}
    controller.script("PATCH", BIOS_SETTINGS, clears)
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "the system no longer reports the one-time BiosSetup boot before the power cycle" in result["reasons"]
    assert patches(controller)[-1] == BIOS_SETTINGS and resets(controller) == []
    assert requested(controller, "GET", SYSTEM)[-1] > requested(controller, "PATCH", BIOS_SETTINGS)[-1]


def test_a_machine_another_party_started_before_the_cycle_is_neither_powered_on_nor_off(monkeypatch):
    controller = Controller()

    def started(firmware):
        firmware.pending.update({"Cores": 8, "TpmEnable": True})
        firmware.power = "On"
        return 204, None, {}
    controller.script("PATCH", BIOS_SETTINGS, started)
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "the system reports another power state than Off before the power cycle" in result["reasons"]
    assert resets(controller) == [] and controller.power == "On"


def test_a_power_state_read_the_controller_does_not_answer_after_the_power_on_still_forces_off(monkeypatch):
    controller = Controller()
    controller.fired = False

    def busy(firmware):
        firmware.fired = firmware.power == "On"
        return 503, None, {}

    def started(firmware):
        firmware.reset("ForceOn")
        firmware.script("GET", SYSTEM, None, busy)
        return 204, None, {}

    def accepted(firmware):
        firmware.waiting.append((redfish_control.POLL_DELAY, lambda landed: landed.reset("ForceOff")))
        return 204, None, {}
    controller.script("POST", RESET, started, accepted)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    assert result["refusal"] == "" and result["changed"] is True


def test_a_power_on_that_never_reads_on_is_still_forced_off(monkeypatch):
    controller = Controller()
    controller.fired = False

    def ignored(firmware):
        firmware.fired = True
        return 204, None, {}
    controller.script("POST", RESET, ignored)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off"
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "powering the machine on into setup: resetting: the system does not report On" in result["reasons"]
    assert not any("not proved off" in reason for reason in result["reasons"])


def test_reset_types_withdrawn_before_the_power_cycle_refuse_in_fixed_text_without_power(monkeypatch):
    controller = Controller()
    controller.fired = False

    def withdrawn(firmware):
        firmware.fired = True
        firmware.pending.update({"Cores": 8, "TpmEnable": True})
        firmware.extra[SYSTEM] = dict(firmware.route(SYSTEM)[1], Actions={"#ComputerSystem.Reset": {
            "target": RESET, "ResetType@Redfish.AllowableValues": ["GracefulShutdown", "Nmi"]}})
        return 204, None, {}
    controller.script("PATCH", BIOS_SETTINGS, withdrawn)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert result["refusal"] == "firmware-setting-not-applied" and resets(controller) == []
    assert "the controller offers no On reset the power cycle can ask for" in result["reasons"]
    assert "the controller offers no ForceOff reset the power cycle can ask for" in result["reasons"]
    text = json.dumps(result["reasons"])
    assert "GracefulShutdown" not in text and "Nmi" not in text and "advertises" not in text


def test_a_power_on_that_fails_after_powering_the_machine_is_forced_off(monkeypatch):
    controller = Controller()
    controller.applies = False

    def failed(firmware):
        firmware.reset("ForceOn")
        return 500, None, {}
    controller.script("POST", RESET, failed)
    result = drive(monkeypatch, controller)
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    assert result["refusal"] == "firmware-setting-not-applied"
    assert any(reason.startswith("powering the machine on into setup: resetting") for reason in result["reasons"])


def test_a_machine_that_does_not_power_off_is_reported_not_proved_off(monkeypatch):
    controller = Controller()
    controller.script("POST", RESET, None, (500, None, {}))
    result = drive(monkeypatch, controller)
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "On"
    assert result["refusal"] == "firmware-setting-not-applied" and result["power"] == "On"
    assert any(reason.startswith("the machine this step powered on was not proved off") for reason in result["reasons"])
    assert result["fields"] == ["firmware.secureBoot", "firmware.biosAttributes.Cores", "firmware.biosAttributes.TpmEnable"]


def test_a_task_on_another_host_is_never_requested_and_the_read_back_decides(monkeypatch):
    controller = Controller()
    controller.script("PATCH", BIOS_SETTINGS, (202, {}, {"Location": "https://other.test" + TASK}))
    result = drive(monkeypatch, controller)
    assert all(urlsplit(call[1]).hostname == "bmc.test" for call in controller.calls)
    assert requested(controller, "GET", TASK) == []
    assert result["refusal"] == "firmware-setting-not-applied"
    assert "writing the BIOS attributes: the pending settings do not hold the declared values" in result["reasons"]
    assert resets(controller) == []


def test_a_refused_certificate_after_a_write_is_not_applied(monkeypatch):
    controller = Controller()
    controller.fired = False
    controller.script("GET", SECURE_SETTINGS, None, None, refused_certificate)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert result["refusal"] == "firmware-setting-not-applied" and result["changed"] is True
    assert any("did not verify" in reason for reason in result["reasons"])
    assert resets(controller) == []


def test_a_refused_certificate_in_the_cycle_keeps_the_power_off_outcome(monkeypatch):
    controller = Controller()
    controller.fired = False
    controller.script("GET", BIOS, None, refused_certificate)
    controller.script("POST", RESET, None, (500, None, {}))
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert result["refusal"] == "firmware-setting-not-applied" and result["power"] == "On"
    assert resets(controller) == ["ForceOn", "ForceOff"]
    assert any("did not verify" in reason for reason in result["reasons"])
    assert any(reason.startswith("the machine this step powered on was not proved off") for reason in result["reasons"])


def test_a_reset_type_the_controller_does_not_offer_refuses_in_fixed_text(monkeypatch):
    controller = Controller()
    controller.extra[SYSTEM] = dict(controller.route(SYSTEM)[1])
    controller.extra[SYSTEM]["Actions"] = {"#ComputerSystem.Reset": {
        "target": RESET, "ResetType@Redfish.AllowableValues": ["On", "GracefulShutdown"]}}
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "firmware-setting-not-applied" and writes(controller) == []
    assert "the controller offers no ForceOff reset the power cycle can ask for" in result["reasons"]
    assert not any("advertises" in reason or "GracefulShutdown" in reason for reason in result["reasons"])


@pytest.mark.parametrize("stops, proved", [(True, True), (False, False)])
def test_a_refused_force_off_passes_only_when_a_fresh_read_shows_off(monkeypatch, stops, proved):
    controller = Controller()

    def conflict(firmware):
        if stops:
            firmware.reset("ForceOff")
        return 409, None, {}
    controller.script("POST", RESET, None, conflict)
    result = drive(monkeypatch, controller)
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == ("Off" if stops else "On")
    assert requested(controller, "GET", SYSTEM)[-1] > requested(controller, "POST", RESET)[-1]
    assert result["refusal"] == ("" if proved else "firmware-setting-not-applied")
    assert any("not proved off" in reason for reason in result["reasons"]) is not proved


def posted_at(controller, kind):
    return [moment for moment, chosen in controller.posted if chosen == kind]


def test_settings_reads_a_bmc_stalls_end_at_the_polls_share_and_the_machine_is_forced_off(monkeypatch):
    controller = Controller()
    controller.stalls = lambda firmware, method, path: firmware.power == "On" and method == "GET" and path in (BIOS, SECURE)
    result = drive(monkeypatch, controller)
    assert controller.stalled > 0
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    [on], [off] = posted_at(controller, "ForceOn"), posted_at(controller, "ForceOff")
    assert off - on <= redfish_control.FIRMWARE_POLL_BUDGET
    assert not any("not proved off" in reason for reason in result["reasons"])


def test_system_reads_a_bmc_stalls_after_the_power_on_end_at_the_polls_share_and_force_off(monkeypatch):
    controller = Controller()
    controller.stalls = lambda firmware, method, path: firmware.power == "On" and method == "GET" and path == SYSTEM
    result = drive(monkeypatch, controller)
    assert controller.stalled > 0
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    [on], [off] = posted_at(controller, "ForceOn"), posted_at(controller, "ForceOff")
    assert off - on <= redfish_control.POWER_ON_BUDGET
    assert not any("not proved off" in reason for reason in result["reasons"])


def terminate(firmware):
    """A SIGTERM to this thread alone, which a parallel run's other threads never take."""
    firmware.fired += 1
    firmware.signalled = (firmware.now, len(firmware.calls))
    signal.pthread_kill(threading.get_ident(), signal.SIGTERM)


def terminated(monkeypatch, controller):
    """One step under a SIGTERM handler of the caller's: the result, the handler
    restored after it and whether that handler ever ran."""
    called = []
    previous = signal.signal(signal.SIGTERM, lambda *_: called.append(True))
    try:
        installed = signal.getsignal(signal.SIGTERM)
        result = drive(monkeypatch, controller)
        restored = signal.getsignal(signal.SIGTERM)
    finally:
        signal.signal(signal.SIGTERM, previous)
    return result, restored is installed, called == []


def test_a_sigterm_during_the_settings_poll_ends_it_forces_off_and_reports_the_stop(monkeypatch):
    controller = Controller()
    controller.fired = 0

    def polled(firmware):
        terminate(firmware)
        return firmware.route(SECURE)

    def on(firmware):
        firmware.reset("ForceOn")
        firmware.script("GET", SECURE, polled)
        return 204, None, {}
    controller.script("POST", RESET, on)
    result, restored, untouched = terminated(monkeypatch, controller)
    assert controller.fired == 1 and restored and untouched
    moment, landed = controller.signalled
    later = [urlsplit(call[1]).path for call in controller.calls[landed:]]
    assert BIOS not in later and SECURE not in later
    assert posted_at(controller, "ForceOff") == [moment]
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off"
    assert result == {"changed": True, "refusal": "firmware-setting-not-applied", "power": "Off", "messages": [],
                      "reasons": [STOPPED], "fields": ["firmware.secureBoot", "firmware.biosAttributes.Cores",
                                                       "firmware.biosAttributes.TpmEnable"]}


def test_a_sigterm_during_the_force_off_still_polls_it_off_and_reports_the_stop(monkeypatch):
    controller = Controller()
    controller.fired = 0

    def off(firmware):
        terminate(firmware)
        firmware.waiting.append((redfish_control.POLL_DELAY, lambda landed: landed.reset("ForceOff")))
        return 204, None, {}
    controller.script("POST", RESET, None, off)
    result, restored, untouched = terminated(monkeypatch, controller)
    assert controller.fired == 1 and restored and untouched
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    assert requested(controller, "GET", SYSTEM)[-1] > requested(controller, "POST", RESET)[-1]
    assert result["refusal"] == "firmware-setting-not-applied" and result["reasons"] == [STOPPED]


def test_a_sigterm_during_a_task_poll_refuses_before_the_power_on(monkeypatch):
    controller = Controller()
    controller.fired = 0

    def running(firmware):
        terminate(firmware)
        return 200, {"@odata.id": TASK, "TaskState": "Running"}, {}
    controller.script("PATCH", BIOS_SETTINGS, (202, {}, {"Location": TASK + "/Monitor"}))
    controller.script("GET", TASK, running)
    result, restored, untouched = terminated(monkeypatch, controller)
    assert controller.fired == 1 and restored and untouched
    assert len(requested(controller, "GET", TASK)) == 1
    assert controller.calls[controller.signalled[1]:] == []
    assert resets(controller) == [] and controller.power == "Off"
    assert result["refusal"] == "firmware-setting-not-applied" and result["reasons"] == [STOPPED]
    assert result["changed"] is True


def test_a_sigterm_during_a_synchronous_write_sends_no_later_write(monkeypatch):
    controller = Controller()
    controller.fired = 0

    def stopping(firmware):
        terminate(firmware)
        return firmware.write("PATCH", SYSTEM, firmware.calls[-1][3])
    controller.script("PATCH", SYSTEM, None, stopping)
    result, restored, untouched = terminated(monkeypatch, controller)
    assert controller.fired == 1 and restored and untouched
    assert patches(controller) == [SYSTEM, SYSTEM]
    assert resets(controller) == [] and controller.power == "Off"
    assert result["refusal"] == "firmware-setting-not-applied" and result["reasons"] == [STOPPED]


def test_another_exception_leaving_the_cycle_still_forces_off_and_restores_the_handler(monkeypatch):
    controller = Controller()
    controller.fired = False

    def interrupted(firmware):
        firmware.fired = True
        raise KeyboardInterrupt()

    def on(firmware):
        firmware.reset("ForceOn")
        firmware.script("GET", SECURE, interrupted)
        return 204, None, {}
    controller.script("POST", RESET, on)
    previous = signal.signal(signal.SIGTERM, signal.SIG_IGN)
    try:
        with pytest.raises(KeyboardInterrupt):
            drive(monkeypatch, controller)
        restored = signal.getsignal(signal.SIGTERM)
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert controller.fired is True and restored is signal.SIG_IGN
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off"


def test_the_handler_only_records_and_the_one_it_replaced_is_restored():
    installed = []

    def install(signum, handler):
        installed.append((signum, handler))
        return "previous"
    with firmware_steps.Termination(install) as termination:
        assert termination.received is False
        assert termination.handle(signal.SIGTERM, None) is None
        assert termination.received is True
    assert installed[0][0] == signal.SIGTERM and installed[1] == (signal.SIGTERM, "previous")
    with pytest.raises(RuntimeError):
        with firmware_steps.Termination(install):
            raise RuntimeError("leaving")
    assert installed[-1] == (signal.SIGTERM, "previous")
    assert not hasattr(firmware_steps, "Terminated")


def test_writes_that_spend_their_share_refuse_before_the_power_on(monkeypatch):
    controller = Controller()
    budget = redfish_control.FIRMWARE_WRITES_BUDGET
    controller.slow = lambda firmware, method, path: budget if method == "PATCH" and path == BIOS_SETTINGS else 0
    result = drive(monkeypatch, controller)
    assert controller.now == budget and controller.calls[-1][0] == "GET"
    assert resets(controller) == [] and controller.power == "Off" and result["power"] == "Off"
    assert result["refusal"] == "firmware-setting-not-applied" and result["changed"] is True
    assert result["reasons"] == [LATE]
    assert controller.boot["BootSourceOverrideTarget"] == "BiosSetup" and controller.pending


def test_writes_inside_their_share_power_on(monkeypatch):
    controller = Controller()
    budget = redfish_control.FIRMWARE_WRITES_BUDGET
    controller.slow = lambda firmware, method, path: budget - 1 if method == "PATCH" and path == BIOS_SETTINGS else 0
    result = drive(monkeypatch, controller)
    assert posted_at(controller, "ForceOn") == [budget - 1]
    assert result["refusal"] == "" and resets(controller) == ["ForceOn", "ForceOff"]


def test_a_boot_selection_poll_a_bmc_stalls_ends_at_its_share(monkeypatch):
    controller = Controller()
    controller.stalls = lambda firmware, method, path: (
        method == "GET" and path == SYSTEM and firmware.boot["BootSourceOverrideTarget"] == "BiosSetup")
    result = drive(monkeypatch, controller)
    assert controller.stalled > 1
    selected = requested(controller, "PATCH", SYSTEM)[0]
    assert controller.now - controller.moments[selected][0] <= redfish_control.BOOT_BUDGET
    assert result["refusal"] == "firmware-setting-not-applied" and resets(controller) == []
    assert patches(controller) == [SYSTEM]


def test_a_task_poll_a_bmc_stalls_ends_at_its_share(monkeypatch):
    controller = Controller()
    controller.script("PATCH", BIOS_SETTINGS, (202, {}, {"Location": TASK + "/Monitor"}))
    controller.stalls = lambda firmware, method, path: method == "GET" and path == TASK
    result = drive(monkeypatch, controller)
    assert controller.stalled > 1
    accepted = requested(controller, "PATCH", BIOS_SETTINGS)[0]
    assert controller.now - controller.moments[accepted][0] <= redfish_control.TASK_BUDGET
    assert result["refusal"] == "firmware-setting-not-applied" and resets(controller) == []
    assert any("the task is still unsettled" in reason for reason in result["reasons"])


def test_an_off_poll_a_bmc_stalls_ends_at_its_share_and_is_not_proved_off(monkeypatch):
    controller = Controller()
    controller.stalls = lambda firmware, method, path: (
        method == "GET" and path == SYSTEM and bool(posted_at(firmware, "ForceOff")))
    result = drive(monkeypatch, controller)
    assert controller.stalled > 1
    [off] = posted_at(controller, "ForceOff")
    assert controller.now - off <= redfish_control.POWER_ON_BUDGET
    assert result["refusal"] == "firmware-setting-not-applied"
    assert any(reason.startswith("the machine this step powered on was not proved off") for reason in result["reasons"])


def test_the_requests_outside_polls_are_the_ones_the_bound_counts(monkeypatch):
    """The worst case: reset metadata read for each reset, every write refused
    once with 412 and then answered with a task, each poll answered at once."""
    controller = Controller()
    controller.action_info = True
    refused = (412, None, {})

    def accepted(effect):
        def answer(firmware):
            effect(firmware)
            return 202, {}, {"Location": TASK + "/Monitor"}
        return answer
    controller.script("PATCH", SYSTEM, refused, None, refused,
                      accepted(lambda firmware: firmware.boot.update(BootSourceOverrideMode="UEFI")))
    controller.script("PATCH", SECURE_SETTINGS, refused,
                      accepted(lambda firmware: setattr(firmware, "secure_pending", True)))
    controller.script("PATCH", BIOS_SETTINGS, refused,
                      accepted(lambda firmware: firmware.pending.update({"Cores": 8, "TpmEnable": True})))
    result = drive(monkeypatch, controller)
    assert result["refusal"] == "" and resets(controller) == ["On", "ForceOff"]
    sent = [(call[0], urlsplit(call[1]).path) for call in controller.calls]
    on, off = (index for index, (method, path) in enumerate(sent) if method == "POST")
    assert sent.count(("GET", RESET_INFO)) == 4 and sent.count(("GET", TASK)) == 3
    boot_poll = sent.index(("PATCH", SYSTEM)) + 2
    assert sent[boot_poll - 1] == ("PATCH", SYSTEM) and sent[boot_poll] == ("GET", SYSTEM)
    before = [call for call in sent[:on] if call != ("GET", TASK)]
    assert len(before) - 1 == redfish_control.FIRMWARE_WRITE_REQUESTS
    polled = [("GET", SYSTEM), ("GET", SECURE), ("GET", BIOS)]
    assert sent[on + 1:off] == polled and sent[off + 1] == ("GET", SYSTEM)
    after = [sent[on], sent[off]] + sent[off + 2:]
    assert len(after) == redfish_control.FIRMWARE_CYCLE_REQUESTS


@pytest.mark.parametrize("from_start", [True, False])
def test_a_reset_target_on_another_host_refuses_before_any_power_request(monkeypatch, from_start):
    controller = Controller()
    controller.fired = False

    def moved(firmware):
        firmware.fired = True
        firmware.pending.update({"Cores": 8, "TpmEnable": True})
        firmware.extra[SYSTEM] = dict(firmware.route(SYSTEM)[1], Actions={"#ComputerSystem.Reset": {
            "target": "https://other.test" + RESET, "ResetType@Redfish.AllowableValues": ["On", "ForceOff"]}})
        return 204, None, {}
    if from_start:
        moved(controller)
        controller.pending = {}
    else:
        controller.script("PATCH", BIOS_SETTINGS, moved)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert result["refusal"] == "firmware-setting-not-applied" and resets(controller) == []
    assert all(urlsplit(call[1]).hostname == "bmc.test" for call in controller.calls)
    assert (writes(controller) == []) is from_start
    text = json.dumps(result)
    assert "other.test" not in text and "not proved off" not in text
    assert "the controller's On reset names a target that is not this controller's endpoint" in result["reasons"]


def test_a_refused_certificate_on_the_force_off_is_not_proved_off(monkeypatch):
    controller = Controller()
    controller.fired = False
    controller.script("POST", RESET, None, refused_certificate)
    result = drive(monkeypatch, controller)
    assert controller.fired is True and resets(controller) == ["ForceOn", "ForceOff"]
    assert result["refusal"] == "firmware-setting-not-applied"
    assert any(reason.startswith("the machine this step powered on was not proved off") and "did not verify" in reason
               for reason in result["reasons"])


def test_a_refused_certificate_in_the_off_poll_is_not_proved_off(monkeypatch):
    controller = Controller()
    controller.fired = False

    def off(firmware):
        firmware.reset("ForceOff")
        firmware.script("GET", SYSTEM, refused_certificate)
        return 204, None, {}
    controller.script("POST", RESET, None, off)
    result = drive(monkeypatch, controller)
    assert controller.fired is True and resets(controller) == ["ForceOn", "ForceOff"]
    assert result["refusal"] == "firmware-setting-not-applied"
    assert any(reason.startswith("the machine this step powered on was not proved off") and "did not verify" in reason
               for reason in result["reasons"])


@pytest.mark.parametrize("applies", [False, True])
def test_an_unexpected_failure_in_the_settings_poll_still_forces_off(monkeypatch, applies):
    controller = Controller()
    controller.applies, controller.fired = applies, False

    def broken(firmware):
        firmware.fired = True
        raise RuntimeError("an answer nobody expected")

    def on(firmware):
        firmware.reset("ForceOn")
        firmware.script("GET", BIOS, broken)
        return 204, None, {}
    controller.script("POST", RESET, on)
    result = drive(monkeypatch, controller)
    assert controller.fired is True
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off" and result["power"] == "Off"
    # The final read decides: settings the power-on applied pass.
    assert result["refusal"] == ("" if applies else "firmware-setting-not-applied")
    if not applies:
        assert "powering the machine on into setup: an unexpected RuntimeError" in result["reasons"]
        assert "an answer nobody expected" not in json.dumps(result)


def test_a_force_off_answered_5xx_passes_once_the_poll_reads_off(monkeypatch):
    controller = Controller()

    def failed(firmware):
        firmware.reset("ForceOff")
        return 500, None, {}
    controller.script("POST", RESET, None, failed)
    result = drive(monkeypatch, controller)
    assert resets(controller) == ["ForceOn", "ForceOff"] and controller.power == "Off"
    assert requested(controller, "GET", SYSTEM)[-1] > requested(controller, "POST", RESET)[-1]
    assert result["refusal"] == "" and not any("not proved off" in reason for reason in result["reasons"])
