"""Erase-owned total phase budgets around the shared qualified Redfish client."""

from __future__ import annotations

import re
import time

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker

INSPECT_SECONDS = 180
INSERT_SECONDS = 1200
BOOT_SECONDS = 180
POWER_SECONDS = 180
STOP_SECONDS = 600
EJECT_SECONDS = 300


class Client(redfish_control.Client):
    def __init__(self, endpoint, user, password, ca_data, deadline, clock=time.monotonic, pause=time.sleep, verification=""):
        super().__init__(endpoint, user, password, verify=True, ca_data=ca_data, verification=verification)
        self.deadline, self.clock, self.pause = deadline, clock, pause
        self.sleep = self.bounded_sleep

    def remaining(self):
        value = self.deadline - self.clock()
        if value <= 0:
            raise worker.Refusal("controller-deadline")
        return value

    def bounded_sleep(self, seconds):
        if seconds >= self.remaining():
            raise worker.Refusal("controller-deadline")
        self.pause(seconds)

    def fetch(self, reference="", method="GET", payload=None, headers=None, timeout=redfish_control.REQUEST_TIMEOUT, doing=""):
        result = super().fetch(reference, method, payload, headers, min(timeout, self.remaining()), doing)
        self.remaining()
        return result


DETAIL = re.compile(r"[A-Za-z0-9_.:@-]{1,64}")
STATUS = re.compile(r"\bHTTP ([0-9]{1,3})\b")


class Refused(worker.Refusal):
    """A named erase refusal: its closed code, a bounded observed value and the controller's own line.

    The detail is only what the controller or the session to the RAM worker
    reported, such as its power state, the system UUID it answered with, how
    many hardware addresses differ or the class of an unanswered session,
    never a value of the request or its material. The line is the shared
    client's printable failure, which names no credential, image or token.
    """

    def __init__(self, code, detail="", line=""):
        super().__init__(code)
        self.detail = detail if DETAIL.fullmatch(detail) else ""
        self.line = line


def observed(value):
    """An observed value in the protocol's detail alphabet, or unknown."""
    value = str(value or "").strip()
    return value if DETAIL.fullmatch(value) else "unknown"


def refused_by(step, failure):
    """A controller that refused an effect step, with the HTTP status its line names."""
    found = STATUS.search(str(failure))
    return Refused("controller-refused", step + (":http:" + found.group(1) if found else ""), str(failure))


def addresses_differ(addresses, failures, expected):
    """How the reported hardware addresses differ from the frozen set, in the detail alphabet."""
    if failures:
        return "unread"
    missing, extra = len(set(expected) - set(addresses)), len(set(addresses) - set(expected))
    parts = [name + ":" + str(count) for name, count in (("missing", missing), ("extra", extra)) if count]
    return ".".join(parts) or "duplicate"


def target(client, request, off):
    try:
        identity = client.identity()
        addresses, failures = client.hardware_addresses()
        power = client.power_state()
    except redfish_control.ControllerError as failure:
        raise Refused("target-unanswered", "", str(failure)) from None
    uuid = identity["UUID"].strip().lower()
    if uuid != request["uuid"].strip().lower():
        raise Refused("identity-mismatch", "uuid:" + observed(uuid))
    if failures or addresses != request["macs"]:
        raise Refused("hardware-mismatch", addresses_differ(addresses, failures, request["macs"]))
    if off and power != "Off":
        raise Refused("machine-running", "power:" + observed(power))
    return power


def step(name, call):
    try:
        call()
    except redfish_control.ControllerError as failure:
        raise refused_by(name, failure) from None


def start(factory, request, image, trust, certificate, enable_verification=False):
    target(factory(INSPECT_SECONDS), request, True)
    step("insert", lambda: factory(INSERT_SECONDS).insert(image, trust, certificate, private=True, enable_verification=enable_verification))
    step("boot", lambda: factory(BOOT_SECONDS).boot_once("Cd"))
    step("power", lambda: factory(POWER_SECONDS).power("On", "On"))


def finish(factory, request, media, certificate, clock=time.monotonic, pause=time.sleep):
    deadline = clock() + STOP_SECONDS
    while True:
        power = target(factory(max(0, deadline - clock())), request, False)
        if power == "Off":
            break
        if clock() + 5 >= deadline:
            raise Refused("poweroff-unproved", "power:" + observed(power))
        pause(5)
    step("eject", lambda: factory(EJECT_SECONDS).eject(media["restoreVerification"], media["removeCertificate"], certificate))
