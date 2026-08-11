from __future__ import annotations

import json
import posixpath
import xml.etree.ElementTree as ET


_LIBVIRT_METADATA_NAMESPACE = "https://bootwright.io/libvirt/metadata/1.0"


def bootwright_libvirt_resource_identity(value):
    if not isinstance(value, str) or not value.strip():
        return {}
    try:
        root = ET.fromstring(value)
    except (ET.ParseError, TypeError, ValueError):
        return {}
    resources = root.findall(
        f"./metadata/{{{_LIBVIRT_METADATA_NAMESPACE}}}resource"
    )
    if len(resources) != 1 or resources[0].attrib:
        return {}
    resource = resources[0]
    allowed = {
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}context": "context",
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}cluster": "cluster",
        f"{{{_LIBVIRT_METADATA_NAMESPACE}}}machine": "machine",
    }
    identity = {}
    for child in list(resource):
        key = allowed.get(child.tag)
        if key is None or key in identity or child.attrib or list(child):
            return {}
        text = child.text.strip() if isinstance(child.text, str) else ""
        if not text:
            return {}
        identity[key] = text
    return identity


def bootwright_qemu_image_virtual_size(value):
    if not isinstance(value, str) or not value.strip():
        return -1
    try:
        document = json.loads(value)
    except (TypeError, ValueError):
        return -1
    if not isinstance(document, dict):
        return -1
    size = document.get("virtual-size")
    if isinstance(size, bool) or not isinstance(size, int) or size < 0:
        return -1
    return size


def bootwright_kubernetes_object_probe(value):
    invalid = {"valid": False, "value": {}, "reason": ""}
    if not isinstance(value, str) or not value.strip():
        return invalid | {"reason": "kubectl returned empty or non-text output"}
    try:
        document = json.loads(value)
    except (json.JSONDecodeError, TypeError, ValueError):
        return invalid | {"reason": "kubectl returned malformed JSON"}
    if not isinstance(document, dict) or not document:
        return invalid | {"reason": "kubectl did not return a JSON object"}
    for field in ("apiVersion", "kind"):
        if not isinstance(document.get(field), str) or not document[field]:
            return invalid | {"reason": f"kubectl JSON has no usable {field}"}
    metadata = document.get("metadata")
    if not isinstance(metadata, dict):
        return invalid | {"reason": "kubectl JSON metadata is not an object"}
    for field in ("name", "namespace", "uid"):
        if not isinstance(metadata.get(field), str) or not metadata[field]:
            return invalid | {"reason": f"kubectl JSON metadata has no usable {field}"}
    if "labels" in metadata and not isinstance(metadata["labels"], dict):
        return invalid | {"reason": "kubectl JSON metadata labels are not an object"}
    owner_references = metadata.get("ownerReferences", [])
    if not isinstance(owner_references, list):
        return invalid | {"reason": "kubectl JSON ownerReferences are not a list"}
    for reference in owner_references:
        if not isinstance(reference, dict):
            return invalid | {"reason": "kubectl JSON ownerReference is not an object"}
        for field in ("apiVersion", "kind", "name", "uid"):
            if not isinstance(reference.get(field), str) or not reference[field]:
                return invalid | {
                    "reason": f"kubectl JSON ownerReference has no usable {field}"
                }
    return {"valid": True, "value": document, "reason": ""}


def bootwright_vsphere_vmedia_record_authorized(
    record,
    context,
    cluster,
    machine,
    name,
    provider,
    server,
    datacenter,
    datastore,
    datastore_path,
    stage_path,
):
    if not isinstance(record, dict):
        return False
    required = [
        context,
        cluster,
        machine,
        name,
        provider,
        server,
        datacenter,
        datastore,
        datastore_path,
        stage_path,
    ]
    if any(
        not isinstance(value, str) or not value or value != value.strip()
        for value in required
    ):
        return False
    expected = {
        "apiVersion": "bootwright.io/ownership/v1alpha1",
        "kind": "vsphere-vmedia",
        "name": name,
        "owner": "bootwright",
        "context": context,
        "provider": provider,
        "cluster": cluster,
        "machine": machine,
    }
    if any(record.get(key) != value for key, value in expected.items()):
        return False
    if record.get("role", "") != "":
        return False
    attributes = record.get("attributes")
    if not isinstance(attributes, dict):
        return False
    if attributes.get("server") != server:
        return False
    if attributes.get("datacenter") != datacenter:
        return False
    if attributes.get("datastore") != datastore:
        return False
    recorded_remote_parts = _safe_parts(attributes.get("path"), False)
    expected_remote_parts = _safe_parts(datastore_path, False)
    if recorded_remote_parts is None or expected_remote_parts is None:
        return False
    if len(recorded_remote_parts) < 3 or len(recorded_remote_parts) != len(
        expected_remote_parts
    ):
        return False
    if recorded_remote_parts[:-2] != expected_remote_parts[:-2]:
        return False
    if recorded_remote_parts[-1] != expected_remote_parts[-1]:
        return False
    paths = record.get("paths")
    if not isinstance(paths, list) or len(paths) != 1:
        return False
    expected_stage_parts = _safe_parts(stage_path, True)
    recorded_stage_parts = _safe_parts(paths[0], True)
    if expected_stage_parts is None or recorded_stage_parts is None:
        return False
    expected_stage_dir = expected_stage_parts[:-1]
    if len(expected_stage_dir) < 2 or len(recorded_stage_parts) != len(
        expected_stage_dir
    ):
        return False
    return (
        recorded_stage_parts[:-1] == expected_stage_dir[:-1]
        and recorded_stage_parts[-1] == recorded_remote_parts[-2]
        and expected_stage_dir[-1] == expected_remote_parts[-2]
    )


def _safe_parts(value, absolute):
    if not isinstance(value, str) or not value or value != value.strip():
        return None
    if "\\" in value or "\x00" in value or posixpath.isabs(value) != absolute:
        return None
    if posixpath.normpath(value) != value:
        return None
    parts = value.split("/")
    if absolute:
        parts = parts[1:]
    if not parts or any(part in {"", ".", ".."} for part in parts):
        return None
    return parts


class FilterModule:
    def filters(self):
        return {
            "bootwright_kubernetes_object_probe": bootwright_kubernetes_object_probe,
            "bootwright_libvirt_resource_identity": bootwright_libvirt_resource_identity,
            "bootwright_qemu_image_virtual_size": bootwright_qemu_image_virtual_size,
            "bootwright_vsphere_vmedia_record_authorized": bootwright_vsphere_vmedia_record_authorized,
        }
