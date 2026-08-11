from __future__ import annotations

import copy
import importlib.util
import json
import pathlib
import unittest


_FILTER_DIR = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "filter"
_spec = importlib.util.spec_from_file_location(
    "_bootwright_provider_safety_filter",
    _FILTER_DIR / "provider_safety.py",
)
assert _spec and _spec.loader, "could not locate provider_safety.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)

bootwright_libvirt_resource_identity = (
    _module.bootwright_libvirt_resource_identity
)
bootwright_kubernetes_object_probe = _module.bootwright_kubernetes_object_probe
bootwright_qemu_image_virtual_size = _module.bootwright_qemu_image_virtual_size
bootwright_vsphere_vmedia_record_authorized = (
    _module.bootwright_vsphere_vmedia_record_authorized
)


class FilterRegistration(unittest.TestCase):
    def test_exposes_every_provider_safety_filter(self):
        filters = _module.FilterModule().filters()
        self.assertIs(
            filters["bootwright_kubernetes_object_probe"],
            bootwright_kubernetes_object_probe,
        )
        self.assertIs(
            filters["bootwright_libvirt_resource_identity"],
            bootwright_libvirt_resource_identity,
        )
        self.assertIs(
            filters["bootwright_qemu_image_virtual_size"],
            bootwright_qemu_image_virtual_size,
        )
        self.assertIs(
            filters["bootwright_vsphere_vmedia_record_authorized"],
            bootwright_vsphere_vmedia_record_authorized,
        )


class LibvirtResourceIdentity(unittest.TestCase):
    def test_reads_one_exact_network_identity(self):
        value = """<network>
          <metadata>
            <bootwright:resource xmlns:bootwright="https://bootwright.io/libvirt/metadata/1.0">
              <bootwright:context>lab</bootwright:context>
              <bootwright:cluster>edge</bootwright:cluster>
            </bootwright:resource>
          </metadata>
        </network>"""
        self.assertEqual(
            bootwright_libvirt_resource_identity(value),
            {"context": "lab", "cluster": "edge"},
        )

    def test_reads_one_exact_domain_identity(self):
        value = """<domain>
          <metadata>
            <bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0">
              <bw:context>lab</bw:context>
              <bw:cluster>edge</bw:cluster>
              <bw:machine>worker-0</bw:machine>
            </bw:resource>
          </metadata>
        </domain>"""
        self.assertEqual(
            bootwright_libvirt_resource_identity(value),
            {"context": "lab", "cluster": "edge", "machine": "worker-0"},
        )

    def test_rejects_malformed_missing_duplicate_and_ambiguous_identity(self):
        cases = [
            "",
            "<network>",
            "<network/>",
            """<network><metadata><resource><context>lab</context></resource></metadata></network>""",
            """<network><metadata><bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:context>other</bw:context><bw:cluster>edge</bw:cluster></bw:resource></metadata></network>""",
            """<network><metadata><bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:cluster>edge</bw:cluster></bw:resource><bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>other</bw:context><bw:cluster>edge</bw:cluster></bw:resource></metadata></network>""",
            """<network><metadata><bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context> </bw:context><bw:cluster>edge</bw:cluster></bw:resource></metadata></network>""",
            """<network><metadata><bw:resource xmlns:bw="https://bootwright.io/libvirt/metadata/1.0"><bw:context>lab</bw:context><bw:cluster>edge</bw:cluster><bw:owner>bootwright</bw:owner></bw:resource></metadata></network>""",
        ]
        for value in cases:
            with self.subTest(value=value):
                self.assertEqual(bootwright_libvirt_resource_identity(value), {})


class QemuImageVirtualSize(unittest.TestCase):
    def test_accepts_nonnegative_integer_virtual_size(self):
        self.assertEqual(
            bootwright_qemu_image_virtual_size('{"virtual-size": 10737418240}'),
            10737418240,
        )
        self.assertEqual(bootwright_qemu_image_virtual_size('{"virtual-size": 0}'), 0)

    def test_rejects_failed_probe_payload_shapes(self):
        for value in [
            "",
            "not-json",
            "[]",
            "{}",
            '{"virtual-size": true}',
            '{"virtual-size": "10737418240"}',
            '{"virtual-size": -1}',
        ]:
            with self.subTest(value=value):
                self.assertEqual(bootwright_qemu_image_virtual_size(value), -1)


class KubernetesObjectProbe(unittest.TestCase):
    def setUp(self):
        self.document = {
            "apiVersion": "v1",
            "kind": "PersistentVolumeClaim",
            "metadata": {
                "name": "edge-worker-0-root",
                "namespace": "guests",
                "uid": "pvc-uid",
                "labels": {"bootwright.io/managed-by": "bootwright"},
                "ownerReferences": [
                    {
                        "apiVersion": "cdi.kubevirt.io/v1beta1",
                        "kind": "DataVolume",
                        "name": "edge-worker-0-root",
                        "uid": "dv-uid",
                    }
                ],
            },
        }

    def test_accepts_a_kubernetes_object_envelope(self):
        self.assertEqual(
            bootwright_kubernetes_object_probe(json.dumps(self.document)),
            {"valid": True, "value": self.document, "reason": ""},
        )

    def test_accepts_optional_labels_and_owner_references_as_absent(self):
        document = copy.deepcopy(self.document)
        document["metadata"].pop("labels")
        document["metadata"].pop("ownerReferences")
        self.assertTrue(
            bootwright_kubernetes_object_probe(json.dumps(document))["valid"]
        )

    def test_rejects_empty_malformed_and_non_object_success_payloads(self):
        for value in [None, "", " ", "not-json", "null", "[]", '"value"', "1", "true", "{}"]:
            with self.subTest(value=value):
                result = bootwright_kubernetes_object_probe(value)
                self.assertFalse(result["valid"])
                self.assertEqual(result["value"], {})
                self.assertTrue(result["reason"])

    def test_rejects_each_missing_or_invalid_object_envelope_field(self):
        mutations = {
            "apiVersion": None,
            "kind": [],
            "metadata": "metadata",
            "metadata.name": "",
            "metadata.namespace": None,
            "metadata.uid": 12,
            "metadata.labels": [],
            "metadata.ownerReferences": {},
        }
        for field, value in mutations.items():
            with self.subTest(field=field):
                document = copy.deepcopy(self.document)
                if field.startswith("metadata."):
                    document["metadata"][field.removeprefix("metadata.")] = value
                else:
                    document[field] = value
                result = bootwright_kubernetes_object_probe(json.dumps(document))
                self.assertFalse(result["valid"])
                self.assertEqual(result["value"], {})
                self.assertTrue(result["reason"])

    def test_rejects_each_invalid_owner_reference_field(self):
        mutations = {
            "entry": "owner",
            "apiVersion": None,
            "kind": [],
            "name": "",
            "uid": 12,
        }
        for field, value in mutations.items():
            with self.subTest(field=field):
                document = copy.deepcopy(self.document)
                if field == "entry":
                    document["metadata"]["ownerReferences"] = [value]
                else:
                    document["metadata"]["ownerReferences"][0][field] = value
                result = bootwright_kubernetes_object_probe(json.dumps(document))
                self.assertFalse(result["valid"])
                self.assertEqual(result["value"], {})
                self.assertTrue(result["reason"])


_UNSET = object()


class VSphereVMediaRecordAuthority(unittest.TestCase):
    def setUp(self):
        self.record = {
            "apiVersion": "bootwright.io/ownership/v1alpha1",
            "kind": "vsphere-vmedia",
            "name": "edge-worker-0",
            "owner": "bootwright",
            "context": "lab",
            "provider": "vsphere-prod",
            "cluster": "edge",
            "machine": "worker-0",
            "paths": [
                "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/old-token"
            ],
            "attributes": {
                "server": "vcenter.example.test",
                "datacenter": "dc1",
                "datastore": "datastore1",
                "path": "bootwright-vmedia/old-token/agent-edge.iso",
            },
        }
        self.arguments = (
            "lab",
            "edge",
            "worker-0",
            "edge-worker-0",
            "vsphere-prod",
            "vcenter.example.test",
            "dc1",
            "datastore1",
            "bootwright-vmedia/new-token/agent-edge.iso",
            "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/new-token/agent-edge.iso",
        )

    def authorized(self, record=_UNSET):
        return bootwright_vsphere_vmedia_record_authorized(
            self.record if record is _UNSET else record,
            *self.arguments,
        )

    def test_accepts_exact_identity_with_a_rotated_publish_token(self):
        self.assertTrue(self.authorized())

    def test_accepts_the_current_publish_token(self):
        record = copy.deepcopy(self.record)
        record["attributes"]["path"] = (
            "bootwright-vmedia/new-token/agent-edge.iso"
        )
        record["paths"] = [
            "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/new-token"
        ]
        self.assertTrue(self.authorized(record))

    def test_rejects_each_mutated_identity_field(self):
        mutations = {
            "apiVersion": "bootwright.io/ownership/v9",
            "kind": "vsphere-machine",
            "name": "edge-worker-1",
            "owner": "someone-else",
            "context": "foreign",
            "provider": "vsphere-other",
            "cluster": "other",
            "machine": "worker-1",
            "role": "reference",
        }
        for field, value in mutations.items():
            with self.subTest(field=field):
                record = copy.deepcopy(self.record)
                record[field] = value
                self.assertFalse(self.authorized(record))

    def test_rejects_empty_or_whitespace_expected_authority(self):
        for index in range(len(self.arguments)):
            with self.subTest(index=index):
                arguments = list(self.arguments)
                arguments[index] = "" if index % 2 == 0 else " unsafe "
                self.assertFalse(
                    bootwright_vsphere_vmedia_record_authorized(
                        self.record,
                        *arguments,
                    )
                )

    def test_rejects_each_mutated_remote_authority_field(self):
        mutations = {
            "server": "foreign-vcenter.example.test",
            "datacenter": "dc2",
            "datastore": "datastore2",
            "path": "foreign-root/old-token/agent-edge.iso",
        }
        for field, value in mutations.items():
            with self.subTest(field=field):
                record = copy.deepcopy(self.record)
                record["attributes"][field] = value
                self.assertFalse(self.authorized(record))

    def test_rejects_remote_path_escape_and_wrong_file_identity(self):
        for value in [
            "bootwright-vmedia/../foreign/agent-edge.iso",
            "/bootwright-vmedia/old-token/agent-edge.iso",
            "bootwright-vmedia/old-token/other.iso",
            "bootwright-vmedia/nested/old-token/agent-edge.iso",
            "bootwright-vmedia\\old-token\\agent-edge.iso",
        ]:
            with self.subTest(value=value):
                record = copy.deepcopy(self.record)
                record["attributes"]["path"] = value
                self.assertFalse(self.authorized(record))

    def test_rejects_local_path_escape_and_wrong_scope(self):
        for value in [
            "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/../foreign",
            "/var/lib/bootwright/vsphere/vsphere-other/vmedia/old-token",
            "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/old-token/nested",
            "relative/vsphere-prod/vmedia/old-token",
        ]:
            with self.subTest(value=value):
                record = copy.deepcopy(self.record)
                record["paths"] = [value]
                self.assertFalse(self.authorized(record))

    def test_rejects_disagreeing_local_and_remote_publish_tokens(self):
        record = copy.deepcopy(self.record)
        record["paths"] = [
            "/var/lib/bootwright/vsphere/vsphere-prod/vmedia/different-token"
        ]
        self.assertFalse(self.authorized(record))

    def test_rejects_missing_or_ambiguous_destructive_paths(self):
        for value in [None, [], ["/one", "/two"], "not-a-list"]:
            with self.subTest(value=value):
                record = copy.deepcopy(self.record)
                if value is None:
                    record.pop("paths")
                else:
                    record["paths"] = value
                self.assertFalse(self.authorized(record))

    def test_rejects_non_mapping_records_and_attributes(self):
        for value in [None, [], "record"]:
            with self.subTest(value=value):
                self.assertFalse(self.authorized(value))
        record = copy.deepcopy(self.record)
        record["attributes"] = []
        self.assertFalse(self.authorized(record))


if __name__ == "__main__":
    unittest.main()
