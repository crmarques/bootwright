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


if __name__ == "__main__":
    unittest.main()
