"""Public native failures retain their cause without lending private material."""

from __future__ import annotations

import base64
import json
import sys
import unicodedata

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


@pytest.mark.parametrize("cause", [
    "Error: open /var/lib/containers/storage/overlay/layers: no space left on device",
    "pull failed for image sha256:" + "0123456789abcdef" * 4 + ": manifest unknown",
])
def test_public_paths_and_digests_survive_opaque_redaction(cause):
    assert native_diagnostics.error_detail("registry.example.test/ceph:requested", cause)["cause"] == cause


@pytest.mark.parametrize("opaque", [
    "A" * 48,
    "AQ" + "Bv1kZ2xQ9rT3mN8sW4yC6eF0hJ5aL7pD2gK1x4" + "==",
    "0123456789abcdef" * 4,
    "/abcd/efgh+ijkl/mnop/qrstuvwxyz0123456789",
    "/abc/def/" + "G" * 32,
    "/abcdefghijklmnop/qrstuvwxyz012345/6789ABCDEFGHIJ==",
    "/" + "a" * 20 + "/" + "b" * 20,
])
def test_opaque_material_outside_the_public_shapes_is_still_redacted(opaque):
    value = native_diagnostics.error_detail("registry.example.test/ceph:requested", "denied " + opaque + " end")
    assert value["cause"] == "denied [redacted] end"


def test_known_credential_redaction_keeps_native_cause():
    encoded = base64.b64encode(b"named-user:registry-password").decode()
    private = native_diagnostics.private_values({"registryJSON": '{"auths":{"registry.example.test":{"auth":"' + encoded + '"}}}'})
    value = native_diagnostics.error_detail("registry.example.test/ceph@sha256:" + "a" * 64,
                                            "manifest unknown for named-user registry-password " + encoded, secrets=private)
    assert value["cause"].startswith("manifest unknown for named-user ")
    assert "registry-password" not in value["cause"] and encoded not in value["cause"]
    assert value["coordinate"].endswith("a" * 64)


def test_registry_identity_is_public_and_only_the_password_is_redacted():
    coordinate = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-27200"
    private = native_diagnostics.private_values({"registryJSON": json.dumps({"url": "cp.icr.io", "username": "cp", "password": "P"})})
    value = native_diagnostics.error_detail(coordinate, "Error: initializing source docker://" + coordinate + ": manifest unknown for P", secrets=private)
    assert value["coordinate"] == coordinate
    assert "cp.icr.io/cp/ibm-ceph" in value["cause"]
    assert "P" not in value["cause"] and value["cause"].endswith("for [redacted]")


def test_utf8_limits_control_removal_and_empty_fallback():
    value = native_diagnostics.error_detail("é" * 512, "\x00\n" + "é" * 1024)
    assert native_diagnostics.valid(value)
    assert len(value["coordinate"].encode()) <= 512
    assert len(value["cause"].encode()) <= 1024
    assert native_diagnostics.error_detail("coordinate", "\x00")["cause"]
    assert not native_diagnostics.valid({"coordinate": "selected", "cause": "bad\nline"})
    assert not native_diagnostics.valid({"coordinate": "selected", "cause": "é" * 1024})


def test_characters_newer_than_the_runner_tables_become_replacement():
    tables = tuple(int(part) for part in unicodedata.unidata_version.split(".")[:2])
    assert tables >= (15, 1) or sys.version_info < (3, 13)
    newer = chr(0x1C89) if tables >= (16, 0) else chr(0x2EBF0) if tables >= (15, 1) else chr(0x1F600)
    assert newer.isprintable()
    value = native_diagnostics.error_detail("selected", "a" + newer + "b")
    assert value["cause"] == "a\ufffdb"
    assert native_diagnostics.valid(value)
    assert not native_diagnostics.valid({"coordinate": "selected", "cause": newer})
    assert native_diagnostics.error_detail("selected", "é\x00\U0001F600")["cause"] == "é \ufffd"


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


TERMINAL = "RuntimeError: image registry.example.test/ceph:requested not found"
SECRET = "Zq7xW9pL3mK8vN2b"


def _traceback(frames):
    lines = ["Traceback (most recent call last):"]
    for index in range(frames):
        lines.append('  File "/usr/lib/python3.9/site-packages/native/step%d.py", line %d, in run_step_%d' % (index, index + 10, index))
        lines.append("    result = invoke_native_command(arguments, timeout=%d)" % index)
    return "\n".join(lines + [TERMINAL])


def _fragments(value, size=4):
    return [value[start:start + size] for start in range(len(value) - size + 1)]


@pytest.mark.parametrize("encode", [False, True])
def test_long_traceback_keeps_first_line_and_terminal_cause(encode):
    output = _traceback(40)
    assert len(output.encode()) > native_diagnostics.MAX_CAUSE
    cause = native_diagnostics.error_detail("selected", output.encode() if encode else output)["cause"]
    assert len(cause.encode()) <= native_diagnostics.MAX_CAUSE
    assert cause.startswith("Traceback (most recent call last): ... ")
    assert cause.endswith(TERMINAL)


def test_input_beyond_the_input_window_keeps_its_terminal_cause():
    output = "pull failed\n" + "frame\n" * 20000 + TERMINAL
    assert len(output.encode()) > native_diagnostics.MAX_INPUT
    for value in (output, output.encode()):
        cause = native_diagnostics.error_detail("selected", value)["cause"]
        assert cause.startswith("pull failed ... ") and cause.endswith(TERMINAL)
        assert len(cause.encode()) <= native_diagnostics.MAX_CAUSE


@pytest.mark.parametrize("size", [300, 30000])
def test_multibyte_characters_at_every_cut_stay_valid_utf8(size):
    output = "€" * 301 + "\n" + "€é" * size + "\U0001F600"
    for value in (output, output.encode()):
        detail = native_diagnostics.error_detail("selected", value)
        encoded = detail["cause"].encode()
        assert len(encoded) <= native_diagnostics.MAX_CAUSE and len(encoded) > native_diagnostics.MAX_CAUSE - 8
        assert encoded.decode() == detail["cause"]
        assert native_diagnostics.valid(detail)


def test_known_secret_at_the_input_window_seam_leaves_no_fragment():
    prefix = "pull failed\n" + "frame\n" * 20000
    window = native_diagnostics.MAX_INPUT * 3 // 4
    output = prefix + SECRET + " " * (window + 8 - len(SECRET) - len(TERMINAL)) + TERMINAL
    assert len(output.encode()) - window == len(prefix) + 8
    cause = native_diagnostics.error_detail("selected", output, secrets=[SECRET])["cause"]
    assert cause.endswith(TERMINAL) and len(cause.encode()) <= native_diagnostics.MAX_CAUSE
    assert not [fragment for fragment in _fragments(SECRET) if fragment in cause]


def _words(size):
    rest = (size - 1) % 5 or 5
    return "word " * ((size - 1 - rest) // 5) + "x" * rest + " "


@pytest.mark.parametrize("inside", range(4, len(SECRET)))
def test_known_secret_at_the_cause_seam_leaves_no_fragment(inside):
    first = "native failure"
    room = native_diagnostics.MAX_CAUSE - len(first) - len(native_diagnostics.SEAM)
    filler = _words(room - inside - 1 - len(TERMINAL))
    output = first + "\n" + "frame\n" * 400 + SECRET + " " + filler + TERMINAL
    assert len(" " + filler + TERMINAL) == room - inside
    exposed = native_diagnostics.error_detail("selected", output)["cause"]
    assert exposed == first + native_diagnostics.SEAM + SECRET[-inside:] + " " + filler + TERMINAL
    cause = native_diagnostics.error_detail("selected", output, secrets=[SECRET])["cause"]
    assert len(cause.encode()) <= native_diagnostics.MAX_CAUSE and cause.endswith(TERMINAL)
    assert not [fragment for fragment in _fragments(SECRET) if fragment in cause]


def test_short_cause_is_unchanged_and_coordinate_keeps_its_head():
    assert native_diagnostics.error_detail("selected", "manifest unknown: " + TERMINAL)["cause"] == "manifest unknown: " + TERMINAL
    coordinate = "registry.example.test/" + "ceph-segment/" * 60
    assert native_diagnostics.error_detail(coordinate, "denied")["coordinate"] == coordinate[:native_diagnostics.MAX_COORDINATE]


def test_small_limit_keeps_the_tail_within_the_limit():
    cause = native_diagnostics.redact("word " * 60 + "\n" + _traceback(4), limit=64)
    assert len(cause.encode()) <= 64 and cause.endswith("not found")


def test_a_str_cause_with_a_lone_surrogate_keeps_its_named_failure():
    raised = b"transaction failed: package caf\xe9 summary".decode("utf-8", "surrogateescape")
    value = native_diagnostics.error_detail("Ceph package transaction", raised)
    assert native_diagnostics.valid(value)
    assert value == {"coordinate": "Ceph package transaction", "cause": "transaction failed: package caf summary"}


def test_a_secret_with_a_lone_surrogate_is_still_removed():
    secret = b"hunter\xe9two".decode("utf-8", "surrogateescape")
    value = native_diagnostics.error_detail("selected", "login hunter" + "\udce9" + "two refused", secrets=[secret])
    assert native_diagnostics.valid(value)
    assert value["cause"] == "login [redacted] refused"


def test_a_bytes_secret_is_removed_from_a_str_cause_with_a_lone_surrogate():
    value = native_diagnostics.error_detail("selected", "login hunter" + "\udce9" + "two refused", secrets=[b"hunter\xe9two"])
    assert native_diagnostics.valid(value)
    assert value["cause"] == "login [redacted] refused"


def test_a_str_secret_with_a_lone_surrogate_is_removed_from_a_bytes_cause():
    secret = b"hunter\xe9two".decode("utf-8", "surrogateescape")
    value = native_diagnostics.error_detail("selected", b"login hunter\xe9two refused", secrets=[secret])
    assert native_diagnostics.valid(value)
    assert value["cause"] == "login [redacted] refused"


def test_a_bytes_secret_is_removed_from_a_cause_decoded_with_replacement():
    cause = b"login hunter\xe9two refused".decode("utf-8", "replace")
    value = native_diagnostics.error_detail("selected", cause, secrets=[b"hunter\xe9two"])
    assert native_diagnostics.valid(value)
    assert value["cause"] == "login [redacted] refused"


@pytest.mark.parametrize("cause", [b"login hunter\xe2\x82two refused", b"login hunter\xe2\x82two refused".decode("utf-8", "replace"),
                                   b"login hunter\xe2\x82two refused".decode("utf-8", "surrogateescape")])
@pytest.mark.parametrize("secret", [b"hunter\xe2\x82two", b"hunter\xe2\x82two".decode("utf-8", "replace"),
                                    b"hunter\xe2\x82two".decode("utf-8", "surrogateescape")])
def test_a_truncated_sequence_secret_is_removed_whatever_the_decoder(cause, secret):
    value = native_diagnostics.error_detail("selected", cause, secrets=[secret])
    assert native_diagnostics.valid(value)
    assert value["cause"] == "login [redacted] refused"


@pytest.mark.parametrize("secret", ["\udce9", "\udce9\udcea", b"\xe9", " \udce9 ", "�", "�\udce9"])
def test_a_secret_of_only_lone_surrogates_redacts_nothing(secret):
    value = native_diagnostics.error_detail("selected", "login ok then refused", secrets=[secret])
    assert value["cause"] == "login ok then refused"
