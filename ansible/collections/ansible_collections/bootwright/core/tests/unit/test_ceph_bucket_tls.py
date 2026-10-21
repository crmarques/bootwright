"""A TLS rgw backend is sent a signed S3 request only when it serves the bound leaf.

The leaf is built with cryptography at test time and served by an in-process TLS server on 127.0.0.1, so no key is committed; it runs in
the controller context because the managed-host floor interpreter carries no cryptography. The bucket worker's other cases are in
tests/unit/plugins/module_utils/test_ceph_object_buckets.py.
"""

from __future__ import annotations

import datetime
import hashlib
import socket
import ssl
import threading

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_object

PAIR = {"accessKey": "Q7ZK2M9X4B1R8T5W3N6P", "secretKey": "u8Rj2Lq5Zx1Vb7Nc4Hk9Tg3Ws6Fd0Ym2Pa8Ee5Oi"}


def leaf(name):
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
    certificate = (x509.CertificateBuilder().subject_name(subject).issuer_name(subject).public_key(key.public_key())
                   .serial_number(x509.random_serial_number()).not_valid_before(now - datetime.timedelta(minutes=5))
                   .not_valid_after(now + datetime.timedelta(days=1)).add_extension(x509.SubjectAlternativeName([x509.DNSName(name)]), critical=False)
                   .sign(key, hashes.SHA256()))
    return key, certificate


@pytest.fixture(name="backend")
def backend_fixture(tmp_path):
    """A TLS listener that records every byte it receives and answers one S3 status line."""
    key, certificate = leaf("s3.example.test")
    chain, private = tmp_path / "rgw.pem", tmp_path / "rgw.key"
    chain.write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    listener = socket.create_server(("127.0.0.1", 0))
    received, handled = [], threading.Event()

    def serve():
        while True:
            try:
                raw, unused = listener.accept()
            except OSError:
                return
            try:
                with context.wrap_socket(raw, server_side=True) as channel:
                    channel.settimeout(5)
                    data = b""
                    while b"\r\n\r\n" not in data:
                        block = channel.recv(4096)
                        if not block:
                            break
                        data += block
                    received.append(data)
                    if data:
                        channel.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
            except (OSError, ssl.SSLError):
                received.append(b"")
            handled.set()

    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    yield listener.getsockname()[1], hashlib.sha256(certificate.public_bytes(serialization.Encoding.DER)).hexdigest(), received, handled
    listener.close()
    thread.join(5)


def test_a_backend_serving_the_bound_leaf_receives_the_signed_request(backend):
    port, digest, received, handled = backend
    answer = ceph_native_object.s3_exchange("127.0.0.1", port, "PUT", "/app-data", "", b"", PAIR, "objects", pinned=("s3.example.test", digest))
    assert answer == (200, "", "", b"") and handled.wait(5)
    assert received[0].startswith(b"PUT /app-data HTTP/1.1\r\n") and b"authorization: AWS4-HMAC-SHA256 Credential=" + PAIR["accessKey"].encode() in received[0]
    assert PAIR["secretKey"].encode() not in received[0]


def test_a_backend_serving_another_leaf_is_sent_nothing(backend):
    port, digest, received, handled = backend
    other = hashlib.sha256(leaf("s3.example.test")[1].public_bytes(serialization.Encoding.DER)).hexdigest()
    assert other != digest
    assert ceph_native_object.s3_exchange("127.0.0.1", port, "PUT", "/app-data", "", b"", PAIR, "objects", pinned=("s3.example.test", other)) is None
    assert handled.wait(5) and received == [b""]
