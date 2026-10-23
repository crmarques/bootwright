"""External DNS is a prerequisite, and failures disclose only its declared name."""

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_export_dns as dns


def test_gateway_resolution_is_bounded_read_only_and_order_independent():
    calls = []

    def run(argv, **kwargs):
        calls.append((argv, kwargs))
        return 0, b'["2001:db8::2", "192.0.2.8", "192.0.2.8"]'

    dns.prove_gateway("s3.example.test", ["192.0.2.8", "2001:db8::2"], runner=run)
    argv, kwargs = calls[0]
    assert argv[1:] == ["-I", "-B", "-c", dns.RESOLVE]
    assert kwargs == {"data": b"s3.example.test", "timeout": 15, "limit": 8192}


@pytest.mark.parametrize("code,output", [
    (1, b"private native diagnostic"), (0, b"[]"), (0, b"invalid"),
    (0, b'["192.0.2.9"]'), (0, b'["192.0.2.8", "192.0.2.9"]'),
    (0, b'[true]'), (0, b'{"address":"192.0.2.8"}'),
])
def test_missing_foreign_or_malformed_resolution_has_a_closed_refusal(code, output):
    with pytest.raises(dns.ceph_native.Refusal, match=r"^gateway-dns$"):
        dns.prove_gateway("s3.example.test", ["192.0.2.8"], runner=lambda *args, **kwargs: (code, output))


def test_timeout_is_normalized_without_native_diagnostic():
    def run(*args, **kwargs):
        raise dns.ceph_native.Refusal("private timeout diagnostic")

    with pytest.raises(dns.ceph_native.Refusal, match=r"^gateway-dns$"):
        dns.prove_gateway("s3.example.test", ["192.0.2.8"], runner=run)


def test_invalid_name_refuses_before_any_process():
    def run(*args, **kwargs):
        pytest.fail("invalid name reached process execution")

    with pytest.raises(dns.ceph_native.Refusal, match="gateway-dns-request"):
        dns.prove_gateway("bad\nname", ["192.0.2.8"], runner=run)
