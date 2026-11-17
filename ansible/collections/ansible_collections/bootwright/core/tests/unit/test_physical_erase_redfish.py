"""Every erase controller phase has a total budget and proves before effects."""

import unittest
from unittest.mock import Mock, patch

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_redfish as erase


class RedfishTests(unittest.TestCase):
    def test_total_budget_caps_native_timeouts_and_stops_subsequent_discovery(self):
        now = [0]
        with patch.object(erase.redfish_control, "_trust_context"):
            client = erase.Client(
                "https://bmc.example.test/redfish/v1/Systems/1",
                "user",
                "private",
                "",
                12,
                clock=lambda: now[0],
                pause=lambda seconds: now.__setitem__(0, now[0] + seconds),
            )
        with patch.object(erase.redfish_control.Client, "fetch", return_value=(200, {}, {})) as fetch:
            client.fetch(timeout=300)
            self.assertEqual(fetch.call_args.args[4], 12)
            client.sleep(10)
            client.fetch(timeout=300)
            self.assertEqual(fetch.call_args.args[4], 2)
            now[0] = 12
            with self.assertRaises(erase.worker.Refusal):
                client.fetch()
            self.assertEqual(fetch.call_count, 2)

    def test_wrong_complete_identity_or_running_machine_precedes_insert(self):
        request = {"uuid": "example-system", "macs": ["02:00:00:00:00:01"]}
        client = Mock()
        client.identity.return_value = {"UUID": request["uuid"]}
        client.hardware_addresses.return_value = (request["macs"], [])
        client.power_state.return_value = "On"
        factory = Mock(return_value=client)
        with self.assertRaises(erase.worker.Refusal):
            erase.start(factory, request, "https://artifact.example.test/private", "established", "certificate")
        client.insert.assert_not_called()
        client.power_state.return_value = "Off"
        client.hardware_addresses.return_value = (request["macs"] + ["02:00:00:00:00:02"], [])
        with self.assertRaises(erase.worker.Refusal):
            erase.start(factory, request, "https://artifact.example.test/private", "established", "certificate")
        client.insert.assert_not_called()

    def test_phase_caps_and_transport_build_cleanup_fit_the_runner_margin(self):
        native = erase.INSPECT_SECONDS + erase.INSERT_SECONDS + erase.BOOT_SECONDS + erase.POWER_SECONDS + erase.STOP_SECONDS + erase.EJECT_SECONDS
        build, startup, transport, cleanup = 900, 900, 3 * 30 + 35 + 30, 600
        self.assertLessEqual(native + build + startup + transport + cleanup, 2 * 3600)


class RefusalTests(unittest.TestCase):
    """Each controller refusal names its closed code, what the controller reported and its own line."""

    REQUEST = {"uuid": "example-system", "macs": ["02:00:00:00:00:01", "02:00:00:00:00:02"]}

    def client(self, uuid="example-system", macs=None, failures=(), power="Off"):
        client = Mock()
        client.identity.return_value = {"UUID": uuid}
        client.hardware_addresses.return_value = (self.REQUEST["macs"] if macs is None else macs, list(failures))
        client.power_state.return_value = power
        return client

    def refused(self, call):
        with self.assertRaises(erase.Refused) as caught:
            call()
        return str(caught.exception), caught.exception.detail, caught.exception.line

    def test_the_target_proof_names_what_the_controller_reported(self):
        cases = (
            (self.client(power="On"), ("machine-running", "power:On", "")),
            (self.client(power=""), ("machine-running", "power:unknown", "")),
            (self.client(uuid="Another-System "), ("identity-mismatch", "uuid:another-system", "")),
            (self.client(macs=["02:00:00:00:00:01"]), ("hardware-mismatch", "missing:1", "")),
            (self.client(macs=self.REQUEST["macs"] + ["02:00:00:00:00:03"]), ("hardware-mismatch", "extra:1", "")),
            (self.client(macs=["02:00:00:00:00:01", "02:00:00:00:00:03"]), ("hardware-mismatch", "missing:1.extra:1", "")),
            (self.client(macs=[], failures=["the interface collection could not be read"]), ("hardware-mismatch", "unread", "")),
        )
        for client, expected in cases:
            with self.subTest(expected=expected):
                self.assertEqual(self.refused(lambda client=client: erase.target(client, self.REQUEST, True)), expected)
        unanswered = self.client()
        unanswered.identity.side_effect = erase.redfish_control.ControllerError("reading /redfish/v1/Systems/1: HTTP 0")
        self.assertEqual(self.refused(lambda: erase.target(unanswered, self.REQUEST, True)),
                         ("target-unanswered", "", "reading /redfish/v1/Systems/1: HTTP 0"))

    def test_an_effect_step_the_controller_refused_names_the_step_and_its_status(self):
        client = self.client()
        client.insert.side_effect = erase.redfish_control.ControllerError("attaching /redfish/v1/Managers/1/VirtualMedia/CD: HTTP 409 (Base.Conflict)")
        image = "https://artifact.example.test/private/" + "f" * 64 + "/erase.iso"
        code, detail, line = self.refused(lambda: erase.start(Mock(return_value=client), self.REQUEST, image, "established", "certificate"))
        self.assertEqual((code, detail), ("controller-refused", "insert:http:409"))
        self.assertNotIn("f" * 64, line)
        client.boot_once.assert_not_called()
        client.insert.side_effect = None
        client.power.side_effect = erase.redfish_control.ControllerError("the reset was not proved")
        self.assertEqual(self.refused(lambda: erase.start(Mock(return_value=client), self.REQUEST, image, "established", "certificate"))[:2],
                         ("controller-refused", "power"))

    def test_finish_names_a_machine_that_stayed_on_and_an_eject_the_controller_refused(self):
        now = [0]
        client = self.client(power="On")
        media = {"restoreVerification": False, "removeCertificate": False}
        code, detail, _line = self.refused(lambda: erase.finish(Mock(return_value=client), self.REQUEST, media, "certificate", clock=lambda: now[0],
                                                                pause=lambda seconds: now.__setitem__(0, now[0] + seconds)))
        self.assertEqual((code, detail), ("poweroff-unproved", "power:On"))
        client.power_state.return_value = "Off"
        client.eject.side_effect = erase.redfish_control.ControllerError("detaching /redfish/v1/Managers/1/VirtualMedia/CD: HTTP 500")
        self.assertEqual(self.refused(lambda: erase.finish(Mock(return_value=client), self.REQUEST, media, "certificate"))[:2],
                         ("controller-refused", "eject:http:500"))
