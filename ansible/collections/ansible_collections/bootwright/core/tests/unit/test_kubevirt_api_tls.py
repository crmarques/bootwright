"""The KubeVirt API client's real HTTPS exchange against a loopback TLS server with a throwaway certificate authority.

The certificates are built with cryptography at test time and no key is committed; the file runs in the controller context because
the managed-host floor interpreter the module-utility tests also run on carries no cryptography.
"""

from __future__ import annotations

import datetime
import http.server
import ipaddress
import json
import socket
import ssl
import threading

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi

PEM = serialization.Encoding.PEM
LOOPBACK = "127.0.0.1"


class Authority:
    """A certificate authority and the leaves it signs."""

    def __init__(self, name):
        self.key = ec.generate_private_key(ec.SECP256R1())
        self.subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
        self.certificate = (self.builder(self.subject, self.key.public_key(), self.subject)
                            .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
                            .add_extension(x509.KeyUsage(False, False, False, False, False, True, True, False, False), critical=True)
                            .add_extension(x509.SubjectKeyIdentifier.from_public_key(self.key.public_key()), critical=False)
                            .sign(self.key, hashes.SHA256()))

    @staticmethod
    def builder(subject, public, issuer):
        now = datetime.datetime.now(datetime.timezone.utc)
        return (x509.CertificateBuilder().subject_name(subject).issuer_name(issuer).public_key(public).serial_number(x509.random_serial_number())
                .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1)))

    def leaf(self, name, usage, alternative=None):
        key = ec.generate_private_key(ec.SECP256R1())
        builder = (self.builder(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)]), key.public_key(), self.subject)
                   .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
                   .add_extension(x509.KeyUsage(True, False, False, False, False, False, False, False, False), critical=True)
                   .add_extension(x509.ExtendedKeyUsage([usage]), critical=False)
                   .add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(self.key.public_key()), critical=False))
        if alternative is not None:
            builder = builder.add_extension(x509.SubjectAlternativeName([alternative]), critical=False)
        certificate = builder.sign(self.key, hashes.SHA256())
        private = key.private_bytes(PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption())
        return certificate.public_bytes(PEM).decode(), private.decode()

    @property
    def pem(self):
        return self.certificate.public_bytes(PEM).decode()


class Handler(http.server.BaseHTTPRequestHandler):
    """The API server: it answers by path and records what each request carried."""

    def setup(self):
        self.request.do_handshake()
        super().setup()

    def log_message(self, *arguments):
        return None

    def do_GET(self):  # noqa: N802 - the name http.server dispatches
        peer = self.connection.getpeercert()
        who = dict(pair[0] for pair in peer["subject"])["commonName"] if peer else ""
        self.server.seen.append({"path": self.path, "authorization": self.headers.get("Authorization", ""), "client": who})
        if self.path == "/redirect":
            self.send_response(302)
            self.send_header("Location", "https://%s:%d/after" % (LOOPBACK, self.server.server_port))
            self.send_header("Content-Length", "0")
            self.end_headers()
        elif self.path.startswith("/pad/"):
            body = self.padded(int(self.path.rpartition("/")[2]))
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            body = json.dumps({"kind": "Namespace", "path": self.path}).encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    @staticmethod
    def padded(size):
        head, tail = b'{"pad":"', b'"}'
        return head + b"x" * (size - len(head) - len(tail)) + tail


class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, context):
        try:
            super().__init__((LOOPBACK, 0), Handler)
        except OSError as error:
            pytest.skip("a loopback port cannot be bound here: %s" % error)
        self.context, self.seen, self.failures = context, [], []

    def get_request(self):
        connection, address = self.socket.accept()
        return self.context.wrap_socket(connection, server_side=True, do_handshake_on_connect=False), address

    def handle_error(self, request, client_address):
        self.failures.append(client_address)


@pytest.fixture(name="authority", scope="module")
def authority_fixture():
    return Authority("host-ca")


@pytest.fixture(name="serving")
def serving_fixture(authority, tmp_path):
    """A running TLS server whose certificate the authority signed for 127.0.0.1, asking a client for a certificate it trusts."""
    certificate, private = authority.leaf(LOOPBACK, ExtendedKeyUsageOID.SERVER_AUTH, x509.IPAddress(ipaddress.ip_address(LOOPBACK)))
    chain, key = tmp_path / "server.pem", tmp_path / "server.key"
    chain.write_text(certificate)
    key.write_text(private)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(key))
    context.load_verify_locations(cadata=authority.pem)
    context.verify_mode = ssl.CERT_OPTIONAL
    server = Server(context)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    yield server
    server.shutdown()
    server.server_close()
    thread.join(5)


def connection(serving, ca, **credential):
    return dict({"server": "https://%s:%d" % (LOOPBACK, serving.server_port), "ca": ca}, **credential)


def client(serving, ca, scratch, **credential):
    return kapi.Client(connection(serving, ca, **credential), scratch=str(scratch), sleep=lambda seconds: None)


def test_a_token_authenticated_get_succeeds_and_sends_the_bearer_header(serving, authority, tmp_path):
    answer = client(serving, authority.pem, tmp_path, token="sha256~token").get("/api/v1/namespaces/guests")
    assert answer == {"kind": "Namespace", "path": "/api/v1/namespaces/guests"}
    assert serving.seen == [{"path": "/api/v1/namespaces/guests", "authorization": "Bearer sha256~token", "client": ""}]
    assert not list(tmp_path.glob("kubevirt-*")) and not serving.failures


def test_a_client_certificate_kubeconfig_form_authenticates_without_a_token(serving, authority, tmp_path):
    certificate, private = authority.leaf("bootwright-client", ExtendedKeyUsageOID.CLIENT_AUTH)
    answer = client(serving, authority.pem, tmp_path, certificate=certificate, key=private).get("/api/v1/namespaces/guests")
    assert answer["kind"] == "Namespace"
    assert serving.seen == [{"path": "/api/v1/namespaces/guests", "authorization": "", "client": "bootwright-client"}]
    assert not list(tmp_path.glob("kubevirt-*"))


def test_a_client_certificate_another_authority_signed_is_not_authenticated(serving, authority, tmp_path):
    certificate, private = Authority("other-ca").leaf("intruder", ExtendedKeyUsageOID.CLIENT_AUTH)
    with pytest.raises(kapi.Unanswered):
        client(serving, authority.pem, tmp_path, certificate=certificate, key=private).get("/api/v1/namespaces/guests")
    assert [seen["client"] for seen in serving.seen] == []


def test_a_redirect_is_never_followed(serving, authority, tmp_path):
    status, body = client(serving, authority.pem, tmp_path, token="t").request("GET", "/redirect")
    assert status == 302 and body is None
    with pytest.raises(kapi.Answered) as refused:
        client(serving, authority.pem, tmp_path, token="t").get("/redirect")
    assert refused.value.status == 302
    assert [seen["path"] for seen in serving.seen] == ["/redirect", "/redirect"] and "/after" not in [seen["path"] for seen in serving.seen]


def test_a_body_above_the_cap_is_refused_and_one_at_it_is_read(serving, authority, tmp_path):
    held = client(serving, authority.pem, tmp_path, token="t")
    assert len(held.get("/pad/%d" % kapi.MAX_RESPONSE)["pad"]) > kapi.MAX_RESPONSE - 16
    with pytest.raises(kapi.Unanswered):
        held.get("/pad/%d" % (kapi.MAX_RESPONSE + 1))
    assert [seen["path"] for seen in serving.seen] == ["/pad/%d" % kapi.MAX_RESPONSE, "/pad/%d" % (kapi.MAX_RESPONSE + 1)]


def test_a_server_certificate_another_authority_signed_fails_verification_and_sends_no_credential(serving, authority, tmp_path):
    other = Authority("other-ca")
    with pytest.raises(kapi.Refused, match="not verified by the kubeconfig's certificate authority"):
        client(serving, other.pem, tmp_path, token="sha256~token").get("/api/v1/namespaces/guests")
    assert serving.seen == []
    with pytest.raises(kapi.Refused, match="not verified"):
        client(serving, other.pem, tmp_path, token="sha256~token").request("POST", "/api/v1/namespaces", {"kind": "Namespace"})
    assert serving.seen == [] and not list(tmp_path.glob("kubevirt-*"))


def test_a_certificate_for_another_address_fails_verification(authority, tmp_path):
    certificate, private = authority.leaf("elsewhere", ExtendedKeyUsageOID.SERVER_AUTH, x509.IPAddress(ipaddress.ip_address("192.0.2.1")))
    chain, key = tmp_path / "wrong.pem", tmp_path / "wrong.key"
    chain.write_text(certificate)
    key.write_text(private)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(key))
    server = Server(context)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    try:
        with pytest.raises(kapi.Refused, match="not verified"):
            client(server, authority.pem, tmp_path, token="t").get("/api/v1/namespaces/guests")
        assert server.seen == []
    finally:
        server.shutdown()
        server.server_close()
        thread.join(5)


def test_a_host_nothing_listens_on_is_unanswered_not_a_pass(authority, tmp_path):
    probe = socket.socket()
    try:
        probe.bind((LOOPBACK, 0))
    except OSError as error:
        pytest.skip("a loopback port cannot be bound here: %s" % error)
    port = probe.getsockname()[1]
    probe.close()
    refused = kapi.Client({"server": "https://%s:%d" % (LOOPBACK, port), "ca": authority.pem, "token": "t"}, scratch=str(tmp_path), sleep=lambda seconds: None)
    with pytest.raises(kapi.Unanswered):
        refused.get("/api/v1/namespaces/guests")
