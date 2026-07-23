from __future__ import annotations

import importlib.util
import pathlib
import unittest


_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_redfish_filter",
    _FILTER_DIR / "redfish.py",
)
assert _spec and _spec.loader, "could not locate redfish.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_vmedia_action_descriptors = _module.bootwright_vmedia_action_descriptors
bootwright_redfish_ethernet_macs = _module.bootwright_redfish_ethernet_macs
bootwright_redfish_mac_validation = _module.bootwright_redfish_mac_validation
bootwright_redfish_power_on_reset_type = _module.bootwright_redfish_power_on_reset_type
bootwright_redfish_url = _module.bootwright_redfish_url
bootwright_redfish_vmedia_attached = _module.bootwright_redfish_vmedia_attached
bootwright_redfish_vmm_control_actions = _module.bootwright_redfish_vmm_control_actions


class RedfishActionDescriptors(unittest.TestCase):
    def test_finds_top_level_and_direct_oem_actions(self):
        resource = {
            "Actions": {
                "#VirtualMedia.VmmControl": {
                    "target": "/top/oem/vmm",
                    "@Redfish.ActionInfo": "/top/action-info",
                },
            },
            "Oem": {
                "VendorA": {
                    "Actions": {
                        "#VirtualMedia.VmmControl": {
                            "target": "/vendor-a/vmm",
                            "@Redfish.ActionInfo": "/vendor-a/action-info",
                        },
                    },
                },
                "VendorB": {
                    "Deep": [
                        {
                            "Actions": {
                                "#VirtualMedia.VmmControl": {"target": "/vendor-b/vmm"},
                            },
                        },
                    ],
                },
            },
        }

        got = bootwright_vmedia_action_descriptors(resource, "#VirtualMedia.VmmControl")

        self.assertEqual(
            got,
            [
                {
                    "target": "/top/oem/vmm",
                    "path": "Actions.#VirtualMedia.VmmControl",
                    "source": "standard",
                    "actionInfo": "/top/action-info",
                },
                {
                    "target": "/vendor-a/vmm",
                    "path": "Oem.VendorA.Actions.#VirtualMedia.VmmControl",
                    "source": "oem",
                    "actionInfo": "/vendor-a/action-info",
                    "vendor": "VendorA",
                },
            ],
        )

    def test_ignores_missing_or_invalid_targets(self):
        resource = {
            "Actions": {
                "#VirtualMedia.VmmControl": {},
            },
            "Oem": {
                "Vendor": {
                    "Actions": {
                        "#VirtualMedia.VmmControl": {"target": 10},
                    },
                },
            },
        }

        got = bootwright_vmedia_action_descriptors(resource, "#VirtualMedia.VmmControl")

        self.assertEqual(got, [])

    def test_bad_input_returns_empty_list(self):
        self.assertEqual(bootwright_vmedia_action_descriptors([], "#VirtualMedia.VmmControl"), [])
        self.assertEqual(bootwright_vmedia_action_descriptors({}, ""), [])


class RedfishVMMControlActions(unittest.TestCase):
    def test_keeps_vmm_actions_with_compatible_action_info(self):
        actions = [
            {
                "target": "/vendor-a/vmm",
                "path": "Oem.VendorA.Actions.#VirtualMedia.VmmControl",
                "source": "oem",
                "actionInfo": "/vendor-a/action-info",
                "vendor": "VendorA",
            },
            {
                "target": "/vendor-b/vmm",
                "path": "Oem.VendorB.Actions.#VirtualMedia.VmmControl",
                "source": "oem",
                "actionInfo": "/vendor-b/action-info",
                "vendor": "VendorB",
            },
        ]
        results = [
            {
                "status": 200,
                "item": actions[0],
                "json": {
                    "Parameters": [
                        {"Name": "Image", "Required": True},
                        {
                            "Name": "VmmControlType",
                            "AllowableValues": ["Connect", "Disconnect"],
                        },
                    ],
                },
            },
            {
                "status": 200,
                "item": actions[1],
                "json": {
                    "Parameters": [
                        {"Name": "Image", "Required": True},
                        {
                            "Name": "VmmControlType",
                            "AllowableValues": ["Mount", "Unmount"],
                        },
                    ],
                },
            },
        ]

        got = bootwright_redfish_vmm_control_actions(actions, results)

        self.assertEqual(got, [dict(actions[0], bodyStyle="vmm-control")])

    def test_rejects_unadvertised_or_unreachable_action_info(self):
        actions = [
            {
                "target": "/vendor-a/vmm",
                "path": "Oem.VendorA.Actions.#VirtualMedia.VmmControl",
                "source": "oem",
                "vendor": "VendorA",
            },
            {
                "target": "/vendor-b/vmm",
                "path": "Oem.VendorB.Actions.#VirtualMedia.VmmControl",
                "source": "oem",
                "actionInfo": "/vendor-b/action-info",
                "vendor": "VendorB",
            },
        ]
        results = [
            {
                "status": 404,
                "item": actions[1],
                "json": {},
            },
        ]

        got = bootwright_redfish_vmm_control_actions(actions, results)

        self.assertEqual(got, [])


class RedfishPowerOnResetType(unittest.TestCase):
    def test_prefers_advertised_force_on(self):
        resource = {
            "Actions": {
                "#ComputerSystem.Reset": {
                    "target": "/systems/1/reset",
                    "ResetType@Redfish.AllowableValues": ["On", "ForceOn", "ForceOff"],
                },
            },
        }

        got = bootwright_redfish_power_on_reset_type(resource)

        self.assertEqual(got, "ForceOn")

    def test_uses_action_info_when_inline_values_are_absent(self):
        resource = {
            "Actions": {
                "#ComputerSystem.Reset": {
                    "target": "/systems/1/reset",
                    "@Redfish.ActionInfo": "/systems/1/reset-action-info",
                },
            },
        }
        results = {
            "results": [
                {
                    "status": 200,
                    "json": {
                        "Parameters": [
                            {
                                "Name": "ResetType",
                                "AllowableValues": ["PushPowerButton", "ForceOff"],
                            },
                        ],
                    },
                },
            ],
        }

        got = bootwright_redfish_power_on_reset_type(resource, results)

        self.assertEqual(got, "PushPowerButton")

    def test_defaults_to_on_without_metadata(self):
        self.assertEqual(bootwright_redfish_power_on_reset_type({}), "On")


class RedfishURL(unittest.TestCase):
    def test_preserves_absolute_http_urls(self):
        self.assertEqual(
            bootwright_redfish_url(
                "https://bmc.example/redfish/v1/Managers/1",
                "https://other.example",
            ),
            "https://bmc.example/redfish/v1/Managers/1",
        )

    def test_joins_rooted_references_to_base_without_duplicate_slashes(self):
        self.assertEqual(
            bootwright_redfish_url(
                "/redfish/v1/Managers/1",
                "https://bmc.example/",
            ),
            "https://bmc.example/redfish/v1/Managers/1",
        )

    def test_joins_relative_references_to_base(self):
        self.assertEqual(
            bootwright_redfish_url(
                "redfish/v1/Managers/1",
                "https://bmc.example/redfish-root",
            ),
            "https://bmc.example/redfish-root/redfish/v1/Managers/1",
        )

    def test_bad_input_returns_empty_string(self):
        self.assertEqual(bootwright_redfish_url(None, "https://bmc.example"), "")
        self.assertEqual(bootwright_redfish_url("", "https://bmc.example"), "")


class RedfishVirtualMediaAttached(unittest.TestCase):
    def test_accepts_exact_image_match(self):
        resource = {
            "Inserted": True,
            "Image": "https://bastion.example:8443/agent.iso",
            "TransferProtocolType": "HTTPS",
        }

        self.assertTrue(
            bootwright_redfish_vmedia_attached(
                resource,
                "https://bastion.example:8443/agent.iso",
                "HTTPS",
            )
        )

    def test_accepts_bmc_image_url_with_elided_port(self):
        resource = {
            "Inserted": True,
            "Image": "https://artifact.example.test/agent-managed-cluster.iso",
            "ImageName": "agent-managed-cluster.iso",
            "TransferProtocolType": "HTTPS",
        }

        self.assertTrue(
            bootwright_redfish_vmedia_attached(
                resource,
                "https://artifact.example.test:8443/agent-managed-cluster.iso",
                "HTTPS",
            )
        )

    def test_accepts_inserted_media_without_reported_image(self):
        resource = {
            "Inserted": True,
            "TransferProtocolType": "HTTPS",
        }

        self.assertTrue(
            bootwright_redfish_vmedia_attached(
                resource,
                "https://bastion.example:8443/agent.iso",
                "HTTPS",
            )
        )

    def test_rejects_wrong_host_or_protocol(self):
        self.assertFalse(
            bootwright_redfish_vmedia_attached(
                {
                    "Inserted": True,
                    "Image": "https://other.example/agent.iso",
                    "TransferProtocolType": "HTTPS",
                },
                "https://bastion.example:8443/agent.iso",
                "HTTPS",
            )
        )
        self.assertFalse(
            bootwright_redfish_vmedia_attached(
                {
                    "Inserted": True,
                    "Image": "https://bastion.example/agent.iso",
                    "TransferProtocolType": "CIFS",
                },
                "https://bastion.example:8443/agent.iso",
                "HTTPS",
            )
        )

    def test_rejects_not_inserted_media(self):
        self.assertFalse(
            bootwright_redfish_vmedia_attached(
                {
                    "Inserted": False,
                    "Image": "https://bastion.example:8443/agent.iso",
                    "TransferProtocolType": "HTTPS",
                },
                "https://bastion.example:8443/agent.iso",
                "HTTPS",
            )
        )


class RedfishMACInventory(unittest.TestCase):
    def test_collects_mac_fields_from_successful_members(self):
        results = [
            {"status": 200, "json": {"MACAddress": "AA-BB-CC-DD-EE-01"}},
            {"status": 200, "json": {"PermanentMACAddress": "aa:bb:cc:dd:ee:02"}},
            {"status": 200, "json": {"MACAddress": "AABBCCDDEE03"}},
            {"status": 404, "json": {"MACAddress": "aa:bb:cc:dd:ee:03"}},
            {"status": 200, "json": {"MACAddress": "not-a-mac"}},
        ]

        got = bootwright_redfish_ethernet_macs(results)

        self.assertEqual(
            got,
            ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02", "aa:bb:cc:dd:ee:03"],
        )

    def test_reports_missing_declared_macs_when_inventory_is_supported(self):
        declared = [
            {"name": "ens65f0", "macAddress": "AA:BB:CC:DD:EE:01"},
            {"name": "ens66f0", "macAddress": "aa:bb:cc:dd:ee:02"},
        ]
        results = [
            {"status": 200, "json": {"MACAddress": "aa:bb:cc:dd:ee:01"}},
        ]

        got = bootwright_redfish_mac_validation(declared, results)

        self.assertTrue(got["supported"])
        self.assertEqual(got["observed"], ["aa:bb:cc:dd:ee:01"])
        self.assertEqual(
            got["missing"],
            [
                {
                    "name": "ens66f0",
                    "macAddress": "aa:bb:cc:dd:ee:02",
                    "display": "ens66f0=aa:bb:cc:dd:ee:02",
                }
            ],
        )

    def test_marks_inventory_unsupported_when_redfish_exposes_no_macs(self):
        got = bootwright_redfish_mac_validation(
            [{"name": "ens65f0", "macAddress": "aa:bb:cc:dd:ee:01"}],
            [{"status": 200, "json": {"Name": "NIC.Slot.1"}}],
        )

        self.assertFalse(got["supported"])
        self.assertEqual(got["observed"], [])


class FilterRegistration(unittest.TestCase):
    def test_filter_module_exposes_filter(self):
        registered = _module.FilterModule().filters()
        self.assertIn("bootwright_vmedia_action_descriptors", registered)
        self.assertIn("bootwright_redfish_mac_validation", registered)
        self.assertIn("bootwright_redfish_url", registered)
        self.assertIn("bootwright_redfish_vmedia_attached", registered)
        self.assertIn("bootwright_redfish_vmm_control_actions", registered)
        self.assertIs(
            registered["bootwright_vmedia_action_descriptors"],
            bootwright_vmedia_action_descriptors,
        )
        self.assertIs(
            registered["bootwright_redfish_mac_validation"],
            bootwright_redfish_mac_validation,
        )
        self.assertIs(registered["bootwright_redfish_url"], bootwright_redfish_url)
        self.assertIs(
            registered["bootwright_redfish_vmedia_attached"],
            bootwright_redfish_vmedia_attached,
        )
        self.assertIs(
            registered["bootwright_redfish_vmm_control_actions"],
            bootwright_redfish_vmm_control_actions,
        )


if __name__ == "__main__":
    unittest.main()
