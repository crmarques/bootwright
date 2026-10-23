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
