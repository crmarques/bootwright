"""A controller write lost in transport is sent again only when sending it again cannot do anything twice.

urllib wraps in URLError a failure raised before it handed the request over
(a refused, unreachable or timed-out connection, a TLS handshake cut short), so
the controller never received it, and such a reset or boot selection is sent
again. A boot-selection PATCH sets the same values again, so it is sent again
whatever the loss. A reset whose answer was lost after its request was written
may have reached the controller and is never sent again. Every resend comes
from the write's own read-back poll, after a read that answered shows the state
not reached, at most WRITE_ATTEMPTS sends in all, within the poll's budget, so
the poll's bound and pauses are the ones a single send gives (B1046).
"""

from __future__ import annotations

import errno
import http.client
import io
import json
import socket
import ssl
import urllib.error

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_firmware

ENDPOINT = "https://bmc.test/redfish/v1/Systems/1"
RESET = ENDPOINT + "/Actions/ComputerSystem.Reset"

UNSENT = {
    "refused": lambda: urllib.error.URLError(ConnectionRefusedError(errno.ECONNREFUSED, "Connection refused")),
    "network-unreachable": lambda: urllib.error.URLError(OSError(errno.ENETUNREACH, "Network is unreachable")),
    "host-unreachable": lambda: urllib.error.URLError(OSError(errno.EHOSTUNREACH, "No route to host")),
    "connect-timeout": lambda: urllib.error.URLError(socket.timeout("timed out")),
    "name-not-yet-resolvable": lambda: urllib.error.URLError(socket.gaierror(socket.EAI_AGAIN, "Temporary failure")),
    "tls-eof": lambda: urllib.error.URLError(ssl.SSLEOFError(8, "EOF occurred in violation of protocol")),
}
# Lost after urllib wrote the request: the controller may have acted on it.
SENT = {
    "closed-without-response": lambda: http.client.RemoteDisconnected("Remote end closed connection without response"),
    "reset": lambda: ConnectionResetError(errno.ECONNRESET, "Connection reset by peer"),
    "read-timeout": lambda: socket.timeout("timed out"),
}
# Not transport loss: never sent again.
KEPT = {
    "no-such-name": lambda: urllib.error.URLError(socket.gaierror(socket.EAI_NONAME, "Name or service not known")),
    "tls-protocol": lambda: urllib.error.URLError(ssl.SSLError(1, "wrong version number")),
    "permission": lambda: urllib.error.URLError(PermissionError(errno.EACCES, "Permission denied")),
    "bad-status-line": lambda: http.client.BadStatusLine("garbage"),
}
REFUSED = urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed"))


class Reply:
    def __init__(self, body=None):
        self.status, self.stream, self.headers = 200, io.BytesIO(json.dumps(body or {}).encode()), http.client.HTTPMessage()

    def read(self, amount=-1):
        return self.stream.read(amount)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


def refused(status):
    return urllib.error.HTTPError(ENDPOINT, status, "refused", http.client.HTTPMessage(), io.BytesIO(b"{}"))


class Controller:
    """Answers each request with the next scripted answer, recording method, URL, If-Match and timeout."""

    def __init__(self, *answers):
        self.answers, self.requests = list(answers), []

    def opener(self, verify=True, ca_data=""):
        return self

    def open(self, request, timeout=None):
        self.requests.append((request.get_method(), request.full_url, request.get_header("If-match"), timeout))
        answer = self.answers.pop(0)
        if isinstance(answer, BaseException):
            raise answer
        return answer

    def writes(self):
        return [row[:3] for row in self.requests if row[0] != "GET"]


def system(power="Off", target=None, tag='W/"1"'):
    body = {"PowerState": power, "@odata.etag": tag,
            "Actions": {"#ComputerSystem.Reset": {"target": RESET, "ResetType@Redfish.AllowableValues": ["ForceOn", "ForceOff"]}}}
    if target:
        body["Boot"] = {"BootSourceOverrideEnabled": "Once", "BootSourceOverrideTarget": target}
    return Reply(body)


def client(monkeypatch, controller):
    monkeypatch.setattr(redfish_control, "_opener", controller.opener)
    value = redfish_control.Client(ENDPOINT, "operator", "p4ssw0rd")
    value.pauses = []
    value.sleep = value.pauses.append
    return value


POST = ("POST", RESET, None)


def test_the_write_bound_is_the_package_precedent():
    assert redfish_control.WRITE_ATTEMPTS == 3


@pytest.mark.parametrize("loss", sorted(UNSENT))
def test_a_reset_that_never_reached_the_controller_is_sent_again_once_a_read_shows_it_not_done(monkeypatch, loss):
    controller = Controller(system(), UNSENT[loss](), system("Off"), Reply(), system("On"))
    driven = client(monkeypatch, controller)
    assert driven.power("On", "On", attempts=3) is True
    assert controller.writes() == [POST, POST] and driven.pauses == [redfish_control.POLL_DELAY]
    assert controller.answers == []


def test_a_read_the_drop_also_lost_sends_nothing(monkeypatch):
    lost = UNSENT["network-unreachable"]
    controller = Controller(system(), lost(), lost(), system("Off"), Reply(), system("On"))
    driven = client(monkeypatch, controller)
    assert driven.power("On", "On", attempts=4) is True
    assert controller.writes() == [POST, POST] and driven.pauses == [redfish_control.POLL_DELAY] * 2


def test_a_reset_that_never_reaches_the_controller_is_sent_at_most_the_bound(monkeypatch):
    lost = UNSENT["refused"]
    controller = Controller(system(), lost(), system("Off"), lost(), system("Off"), lost(), system("Off"), system("Off"))
    driven = client(monkeypatch, controller)
    with pytest.raises(redfish_control.ControllerError, match="does not report On"):
        driven.power("On", "On", attempts=4)
    assert controller.writes() == [POST] * redfish_control.WRITE_ATTEMPTS and controller.answers == []
    assert driven.pauses == [redfish_control.POLL_DELAY] * 3


@pytest.mark.parametrize("loss", sorted(SENT) + sorted(KEPT))
def test_a_reset_that_may_have_reached_the_controller_is_never_sent_again(monkeypatch, loss):
    fault = SENT.get(loss, KEPT.get(loss))()
    controller = Controller(system(), fault, system("Off"), system("On"))
    driven = client(monkeypatch, controller)
    assert driven.power("On", "On", attempts=3) is True
    assert controller.writes() == [POST] and driven.pauses == [redfish_control.POLL_DELAY]


@pytest.mark.parametrize("answer", [refused(409), SENT["closed-without-response"]()], ids=["answered", "lost-after-send"])
def test_a_resend_that_was_answered_or_may_have_arrived_ends_the_resends(monkeypatch, answer):
    controller = Controller(system(), UNSENT["refused"](), system("Off"), answer, system("Off"), system("On"))
    assert client(monkeypatch, controller).power("On", "On", attempts=3) is True
    assert controller.writes() == [POST, POST]


@pytest.mark.parametrize("status", [400, 403, 409, 500])
def test_a_reset_the_controller_answered_is_never_sent_again(monkeypatch, status):
    controller = Controller(system(), refused(status))
    with pytest.raises(redfish_control.ControllerError, match="HTTP %d" % status):
        client(monkeypatch, controller).power("On", "On", attempts=3)
    assert controller.writes() == [POST]


def test_a_refused_certificate_on_a_resend_is_raised_at_once(monkeypatch):
    controller = Controller(system(), UNSENT["refused"](), system("Off"), REFUSED, system("On"))
    with pytest.raises(redfish_control.UnverifiedCertificate):
        client(monkeypatch, controller).power("On", "On", attempts=3)
    assert controller.writes() == [POST, POST]


@pytest.mark.parametrize("loss", sorted(UNSENT) + sorted(SENT))
def test_a_boot_selection_lost_in_transport_is_patched_again_with_the_read_backs_tag(monkeypatch, loss):
    fault = UNSENT.get(loss, SENT.get(loss))
    controller = Controller(system(), fault(), system(tag='W/"2"'), Reply(), system(target="Cd", tag='W/"3"'))
    driven = client(monkeypatch, controller)
    driven.boot_once("Cd")
    assert controller.writes() == [("PATCH", ENDPOINT, 'W/"1"'), ("PATCH", ENDPOINT, 'W/"2"')]
    assert driven.pauses == [redfish_control.BOOT_POLL_DELAY] and controller.answers == []


def test_a_boot_selection_that_landed_although_its_answer_was_lost_is_not_patched_again(monkeypatch):
    controller = Controller(system(), SENT["read-timeout"](), system(target="Cd"))
    driven = client(monkeypatch, controller)
    driven.boot_once("Cd")
    assert len(controller.writes()) == 1 and driven.pauses == []


@pytest.mark.parametrize("loss", sorted(KEPT))
def test_a_boot_selection_failure_that_is_not_transport_loss_is_only_read_back(monkeypatch, loss):
    controller = Controller(system(), KEPT[loss](), system(), system(target="Cd"))
    driven = client(monkeypatch, controller)
    driven.boot_once("Cd")
    assert len(controller.writes()) == 1


def test_a_boot_selection_lost_at_every_send_is_decided_by_its_read_back(monkeypatch):
    lost = UNSENT["refused"]
    answers = [system(), lost()]
    for poll in range(redfish_control.BOOT_POLLS):
        answers.append(system())
        if poll < redfish_control.WRITE_ATTEMPTS - 1:
            answers.append(lost())
    controller = Controller(*answers)
    driven = client(monkeypatch, controller)
    with pytest.raises(redfish_control.ControllerError, match="does not report Cd"):
        driven.boot_once("Cd")
    assert len(controller.writes()) == redfish_control.WRITE_ATTEMPTS and controller.answers == []
    assert driven.pauses == [redfish_control.BOOT_POLL_DELAY] * (redfish_control.BOOT_POLLS - 1)


def test_a_stop_after_the_read_sends_nothing(monkeypatch):
    controller = Controller(system(), UNSENT["refused"](), system())
    driven = client(monkeypatch, controller)
    answers = iter([False, True, True, True])
    with pytest.raises(redfish_control.ControllerError, match="does not report Cd"):
        driven.boot_once("Cd", budget=600, stop=lambda: next(answers))
    assert len(controller.writes()) == 1


def test_a_resend_takes_only_what_the_poll_budget_leaves(monkeypatch):
    controller = Controller(system(), UNSENT["refused"](), system(), Reply(), system(target="Cd"))
    driven = client(monkeypatch, controller)
    now = [1000.0]
    driven.clock = lambda: now[0]
    driven.boot_once("Cd", budget=12)
    assert [row[3] for row in controller.requests if row[0] == "PATCH"] == [redfish_control.REQUEST_TIMEOUT, 12]


def firmware(monkeypatch, controller):
    step = redfish_firmware.Convergence(client(monkeypatch, controller), "enforce", {})
    return step


def test_the_firmware_steps_power_on_is_sent_again_only_when_it_never_arrived(monkeypatch):
    controller = Controller(UNSENT["host-unreachable"](), system("Off"), Reply(), system("On"))
    firmware(monkeypatch, controller).reset((RESET, "ForceOn"), "On")
    assert controller.writes() == [("POST", RESET, None)] * 2

    controller = Controller(SENT["reset"](), system("Off"), system("On"))
    firmware(monkeypatch, controller).reset((RESET, "ForceOn"), "On")
    assert controller.writes() == [("POST", RESET, None)]


def test_the_firmware_steps_force_off_is_sent_again_only_when_it_never_arrived(monkeypatch):
    controller = Controller(UNSENT["refused"](), system("On"), Reply(), system("Off"))
    assert firmware(monkeypatch, controller).power_off((RESET, "ForceOff")) == ""
    assert controller.writes() == [("POST", RESET, None)] * 2
