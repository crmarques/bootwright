"""Public native failures retain their cause without lending private material."""

from __future__ import annotations

import base64
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_native, ceph_native_worker, ceph_package_worker, native_diagnostics


@pytest.mark.parametrize("output,private", [
    ("https://operator:my-password@registry.example.test denied", "my-password"),
    ('{"password": "a secret with spaces", "error": "denied"}', "a secret with spaces"),
    ('token=some-secret&error=denied', "some-secret"),
    ('Authorization: Bearer bearer-secret\nmanifest unknown', "bearer-secret"),
    ('-----BEGIN OPENSSH PRIVATE KEY-----\nprivate partial data', "private partial data"),
    ('failed key: ' + 'A' * 48, 'A' * 48),
])
def test_structural_redaction(output, private):
    value = native_diagnostics.error_detail("registry.example.test/ceph:requested", output)
    assert native_diagnostics.valid(value)
    assert private not in value["cause"]
    assert value["coordinate"] == "registry.example.test/ceph:requested"


def test_known_credential_redaction_keeps_native_cause():
    encoded = base64.b64encode(b"named-user:registry-password").decode()
    private = native_diagnostics.private_values({"registryJSON": '{"auths":{"registry.example.test":{"auth":"' + encoded + '"}}}'})
    value = native_diagnostics.error_detail("registry.example.test/ceph@sha256:" + "a" * 64,
                                            "manifest unknown for named-user registry-password " + encoded, secrets=private)
    assert value["cause"].startswith("manifest unknown")
    assert "named-user" not in value["cause"] and "registry-password" not in value["cause"] and encoded not in value["cause"]
    assert value["coordinate"].endswith("a" * 64)


def test_utf8_limits_control_removal_and_empty_fallback():
    value = native_diagnostics.error_detail("é" * 512, "\x00\n" + "é" * 1024)
    assert native_diagnostics.valid(value)
    assert len(value["coordinate"].encode()) <= 512
    assert len(value["cause"].encode()) <= 1024
    assert native_diagnostics.error_detail("coordinate", "\x00")["cause"]
    assert not native_diagnostics.valid({"coordinate": "selected", "cause": "bad\nline"})
    assert not native_diagnostics.valid({"coordinate": "selected", "cause": "é" * 1024})


def test_native_worker_preserves_only_typed_projected_error():
    error = ceph_native.NativeFailure("native-command", "selected:version", b"package not found password=hidden")
    value = ceph_native_worker.failure("native-refusal", error)
    assert value["nativeError"]["cause"] == "package not found password=[redacted]"
    ceph_native_worker.result_bytes(value)
    assert "nativeError" not in ceph_native_worker.failure("native-refusal", ValueError("arbitrary secret"))


def test_protocol_requires_explicit_native_error_capability():
    arguments = {"phase": "refused", "reason": "native-command", "native_error": native_diagnostics.error_detail("chosen", "not found")}
    records = []
    common = ("test", "unpublished", lambda _: ({}, None))
    refused = adapter_protocol.Capability(*common, phases=adapter_protocol.REFUSING_PHASES, refusals=("native-command",))
    assert adapter_protocol.publish(arguments, refused, records.append)["failed"]
    assert records == []
    approved = adapter_protocol.Capability(*common, phases=adapter_protocol.REFUSING_PHASES, refusals=("native-command",), native_error=True)
    assert adapter_protocol.publish(arguments, approved, records.append) == {"changed": False}
    assert records == [{"phase": "refused", "reason": "native-command", "nativeError": arguments["native_error"]}]


def test_package_result_budget_contains_maximum_escaped_unicode_projection():
    error = ceph_native.NativeFailure("package-signature", "é" * 256, "é" * 512)
    value = ceph_package_worker.failure(error)
    assert value["reason"] == "package-signature"
    assert value["nativeError"] == {"coordinate": "é" * 256, "cause": "é" * 512}
    assert len(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()) <= ceph_package_worker.RESULT_LIMIT
