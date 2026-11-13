"""Generated gateway TLS uses an explicit native CA and verifies identity before sending HTTP."""

from __future__ import annotations

import datetime
import socket
import ssl
import threading

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observability as gateway


def authority():
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "test authority")])
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(days=1))
            .not_valid_after(now + datetime.timedelta(days=1)).add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
            .sign(key, hashes.SHA256()))
    return key, cert


@pytest.mark.parametrize("case", ["valid", "foreign-ca", "wrong-name", "expired"])
def test_native_ca_authenticates_the_gateway_before_http(tmp_path, case):
    ca_key, ca = authority()
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "gateway.example.test")])
    end = now - datetime.timedelta(hours=1) if case == "expired" else now + datetime.timedelta(hours=1)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(ca.subject).public_key(key.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(hours=2)).not_valid_after(end)
            .add_extension(x509.SubjectAlternativeName([x509.DNSName("gateway.example.test")]), critical=False).sign(ca_key, hashes.SHA256()))
    certificate, private = tmp_path / "cert", tmp_path / "key"
    certificate.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(certificate, private)
    received = []
    with socket.create_server(("127.0.0.1", 0)) as listener:
        listener.settimeout(5)

        def serve():
            try:
                with listener.accept()[0] as raw:
                    raw.settimeout(5)
                    with context.wrap_socket(raw, server_side=True) as channel:
                        received.append(channel.recv(4096))
                        channel.sendall(b"HTTP/1.0 200 OK\r\nContent-Length: 0\r\n\r\n")
            except OSError:
                pass

        thread = threading.Thread(target=serve)
        thread.start()
        trust = authority()[1] if case == "foreign-ca" else ca
        tls_name = "other.example.test" if case == "wrong-name" else "gateway.example.test"
        try:
            leaf, ready = gateway.served("127.0.0.1", listener.getsockname()[1], tls_name,
                                         ca=trust.public_bytes(serialization.Encoding.PEM).decode())
        finally:
            thread.join(6)
        assert not thread.is_alive()
    if case == "valid":
        assert ready and leaf == cert.public_bytes(serialization.Encoding.DER)
        assert received and received[0].startswith(b"GET / HTTP/1.0\r\n")
    else:
        assert ready is False and leaf is None and received == []


def test_invalid_native_authority_never_contacts_the_endpoint(monkeypatch):
    def connect(*args, **kwargs):
        pytest.fail("invalid native CA must refuse before contact")

    monkeypatch.setattr(socket, "create_connection", connect)
    assert gateway.served("192.0.2.1", 443, "gateway.example.test", ca="invalid authority") == (None, False)
