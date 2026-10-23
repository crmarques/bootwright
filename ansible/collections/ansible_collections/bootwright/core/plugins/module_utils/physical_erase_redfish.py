"""Erase-owned total phase budgets around the shared qualified Redfish client."""

from __future__ import annotations

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
    def __init__(self, endpoint, user, password, ca_data, deadline, clock=time.monotonic, pause=time.sleep):
        super().__init__(endpoint, user, password, verify=True, ca_data=ca_data)
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


def target(client, request, off):
    identity = client.identity()
    addresses, failures = client.hardware_addresses()
    if failures or identity["UUID"].strip().lower() != request["uuid"].strip().lower() or addresses != request["macs"]:
        raise worker.Refusal("controller-identity")
    power = client.power_state()
    if off and power != "Off":
        raise worker.Refusal("machine-running")
    return power


def start(factory, request, image, trust, certificate):
    target(factory(INSPECT_SECONDS), request, True)
    factory(INSERT_SECONDS).insert(image, trust, certificate, private=True)
    factory(BOOT_SECONDS).boot_once("Cd")
    factory(POWER_SECONDS).power("On", "On")


def finish(factory, request, media, certificate, clock=time.monotonic, pause=time.sleep):
    deadline = clock() + STOP_SECONDS
    while True:
        if target(factory(max(0, deadline - clock())), request, False) == "Off":
            break
        if clock() + 5 >= deadline:
            raise worker.Refusal("poweroff-unproved")
        pause(5)
    factory(EJECT_SECONDS).eject(media["restoreVerification"], media["removeCertificate"], certificate)
