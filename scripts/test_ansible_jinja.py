from pathlib import Path
import unittest

from jinja2 import Environment, StrictUndefined
import yaml


class KubeVirtNADGateTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        root = Path(__file__).resolve().parents[1]
        path = root / "ansible/collections/ansible_collections/bootwright/core/roles/machine_substrate_kubevirt/tasks/main.yml"
        tasks = yaml.safe_load(path.read_text(encoding="utf-8"))
        task = next(item for item in tasks if item["name"] == "Validate KubeVirt substrate inputs")
        conditions = task["ansible.builtin.assert"]["that"]
        condition = next(item for item in conditions if "networkAttachment.kubevirt.nad" in item)
        cls.template = Environment(undefined=StrictUndefined).from_string("{{ " + condition + " }}")

    def assert_gate(self, expected, interfaces):
        rendered = self.template.render(bootwright_component={"interfaces": interfaces})
        self.assertEqual(rendered, str(expected))

    def test_accepts_every_resolved_nad(self):
        self.assert_gate(
            True,
            [
                {"name": "primary", "networkAttachment": {"kubevirt": {"nad": "child/machine"}}},
                {"name": "ceph-public", "networkAttachment": {"kubevirt": {"nad": "child/ceph-public"}}},
            ],
        )

    def test_rejects_missing_nested_nad_without_undefined_error(self):
        for interface in [
            {"name": "primary"},
            {"name": "primary", "networkAttachment": {}},
            {"name": "primary", "networkAttachment": {"kubevirt": {}}},
            {"name": "primary", "networkAttachment": {"kubevirt": {"nad": ""}}},
        ]:
            with self.subTest(interface=interface):
                self.assert_gate(False, [interface])


class InfraComponentFirewalldGateTest(unittest.TestCase):
    roles = [
        "infra_component_artifact_server_http",
        "infra_component_load_balancer_haproxy",
        "infra_component_name_resolution_dnsmasq",
        "infra_component_ntp_chrony",
        "infra_component_proxy_squid",
        "infra_component_registry_mirror",
    ]

    @classmethod
    def setUpClass(cls):
        root = Path(__file__).resolve().parents[1]
        environment = Environment(undefined=StrictUndefined)
        environment.filters["bool"] = bool
        cls.gates = {}
        for role in cls.roles:
            path = root / (
                "ansible/collections/ansible_collections/bootwright/core/roles/"
                + role
                + "/tasks/destroy.yml"
            )
            tasks = yaml.safe_load(path.read_text(encoding="utf-8"))
            task = next(
                item
                for item in tasks
                if "conclusive firewalld probe" in item["name"]
            )
            cls.gates[role] = [
                environment.compile_expression(condition, undefined_to_none=False)
                for condition in task["ansible.builtin.assert"]["that"]
            ]

    def assert_gate(self, expected, probe, state, available):
        context = {
            "bootwright_artifacts_selected_listener_ports": [8443],
            "bootwright_dnsmasq_selected_ports": [53],
            "bootwright_firewalld_available": available,
            "bootwright_firewalld_probe": {"stat": probe},
            "bootwright_firewalld_state": state,
            "bootwright_infra_destroy_global_ports": ["8443/tcp"],
            "bootwright_squid_selected_ports": [3128],
        }
        for role, conditions in self.gates.items():
            with self.subTest(role=role):
                result = all(bool(condition(**context)) for condition in conditions)
                self.assertEqual(result, expected)

    def test_accepts_absent_firewalld(self):
        self.assert_gate(True, {"exists": False}, {}, False)

    def test_accepts_stopped_firewalld(self):
        self.assert_gate(
            True,
            {"exists": True, "isreg": True, "islnk": False, "executable": True},
            {"rc": 252, "stdout": "not running\n", "stderr": ""},
            False,
        )

    def test_accepts_running_firewalld(self):
        self.assert_gate(
            True,
            {"exists": True, "isreg": True, "islnk": False, "executable": True},
            {"rc": 0, "stdout": "running\n", "stderr": ""},
            True,
        )

    def test_rejects_inconclusive_firewalld(self):
        cases = [
            (
                {"exists": True, "isreg": True, "islnk": False, "executable": True},
                {"rc": 1, "stdout": "failed", "stderr": "failure"},
                False,
            ),
            (
                {"exists": True, "isreg": True, "islnk": True, "executable": True},
                {"rc": 0, "stdout": "running", "stderr": ""},
                True,
            ),
            (
                {"exists": True, "isreg": True, "islnk": False, "executable": True},
                {},
                False,
            ),
        ]
        for probe, state, available in cases:
            with self.subTest(probe=probe, state=state, available=available):
                self.assert_gate(False, probe, state, available)

    def test_rejects_inconsistent_availability(self):
        self.assert_gate(
            False,
            {"exists": True, "isreg": True, "islnk": False, "executable": True},
            {"rc": 252, "stdout": "not running", "stderr": ""},
            True,
        )


if __name__ == "__main__":
    unittest.main()
