"""A monitoring login probe verifies the daemon against the native CA, presents a CA-signed client identity held only in memory, and
carries the login only in its Authorization header, as a daemon behind the management gateway requires
(src/pybind/mgr/cephadm/templates/services/prometheus/web.yml.j2: RequireAndVerifyClientCert against the cephadm root CA)."""

from __future__ import annotations

import base64
import datetime
import ipaddress
import os
import socket
import ssl
import threading

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID

from ansible_collections.bootwright.core.plugins.module_utils import ceph_monitoring_logins_native as logins

PASSWORD = "pr-Candidate.Value9"


def issued(ca_key, ca, name, address=None):
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    names = [x509.IPAddress(ipaddress.ip_address(address))] if address else [x509.DNSName(name)]
    cert = (x509.CertificateBuilder().subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])).issuer_name(ca.subject)
            .public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(hours=1))
            .not_valid_after(now + datetime.timedelta(hours=1)).add_extension(x509.SubjectAlternativeName(names), critical=False)
            .sign(ca_key, hashes.SHA256()))
    return (cert.public_bytes(serialization.Encoding.PEM).decode(),
            key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode())


def authority():
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "cephadm root")])
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(days=1))
            .not_valid_after(now + datetime.timedelta(days=1)).add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
            .sign(key, hashes.SHA256()))
    return key, cert


@pytest.mark.parametrize("case", ["valid", "no-client", "foreign-ca"])
def test_probe_presents_the_in_memory_client_identity(tmp_path, monkeypatch, case):
    ca_key, ca = authority()
    pem = ca.public_bytes(serialization.Encoding.PEM).decode()
    server_cert, server_key = issued(ca_key, ca, "127.0.0.1", "127.0.0.1")
    client_cert, client_key = issued(ca_key, ca, "client")
    (tmp_path / "cert").write_text(server_cert)
    (tmp_path / "key").write_text(server_key)
    server = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server.load_cert_chain(tmp_path / "cert", tmp_path / "key")
    server.verify_mode = ssl.CERT_REQUIRED
    server.load_verify_locations(cadata=pem)
    written = []
    real_open = os.open
    monkeypatch.setattr(os, "open", lambda path, *args, **kwargs: written.append(path) or real_open(path, *args, **kwargs))
    received = []
    with socket.create_server(("127.0.0.1", 0)) as listener:
        listener.settimeout(5)

        def serve():
            try:
                with listener.accept()[0] as raw:
                    raw.settimeout(5)
                    with server.wrap_socket(raw, server_side=True) as channel:
                        received.append(channel.recv(4096))
                        channel.sendall(b"HTTP/1.0 401 Unauthorized\r\nContent-Length: 0\r\n\r\n")
            except OSError:
                pass

        thread = threading.Thread(target=serve)
        thread.start()
        pair = None if case == "no-client" else {"cert": client_cert, "key": client_key}
        trusted = authority()[1].public_bytes(serialization.Encoding.PEM).decode() if case == "foreign-ca" else pem
        context = logins.trust(trusted, pair)
        try:
            answer = logins.exchange(("127.0.0.1", listener.getsockname()[1], "127.0.0.1", context), "GET", logins.ROUTES["prometheus"],
                                     logins.basic("admin", PASSWORD))
        finally:
            thread.join(6)
        assert not thread.is_alive()
    assert written == []
    if case == "valid":
        assert answer == (401, b"")
        request = received[0]
        assert request.startswith(b"GET /prometheus/api/v1/status/buildinfo HTTP/1.0\r\n")
        assert b"Authorization: Basic " + base64.b64encode(("admin:" + PASSWORD).encode()) + b"\r\n" in request
        assert PASSWORD.encode() not in request
    else:
        assert answer is None and received == []


def test_trust_refuses_an_invalid_authority_before_contact(monkeypatch):
    monkeypatch.setattr(socket, "create_connection", lambda *args, **kwargs: pytest.fail("contacted"))
    with pytest.raises(logins.native.Refusal, match="^native-output$"):
        logins.trust("not a certificate")
