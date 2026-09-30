"""The management-controller trust cases that need a certificate authority.

They build their authorities with cryptography at test time and reach an
in-process TLS server on 127.0.0.1, so no key is committed. They run in the
controller context because the managed-host floor interpreter, which also runs
the module utility tests, carries no cryptography; the rest of the client's
contract is in tests/unit/plugins/module_utils/test_redfish_control.py.
"""

from __future__ import annotations

import datetime
import http.server
import ipaddress
import json
import ssl
import threading
import urllib.request

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

ENDPOINT = "https://bmc.test/redfish/v1/Systems/1"


class Recorder:
    """An opener factory that records every trust it is asked for and every
    request it is handed."""

    def __init__(self):
        self.requests, self.trust = [], []

    def opener(self, verify=True, ca_data=""):
        self.trust.append((verify, ca_data))
        return self

    def open(self, request, timeout=None):
        self.requests.append(request)
        raise AssertionError("no request is expected")


def _name(common):
    return x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, common)])


def authority(common):
    """A CA key and certificate, shaped as a strict verifier requires."""
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = (
        x509.CertificateBuilder().subject_name(_name(common)).issuer_name(_name(common))
        .public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=False,
                                     data_encipherment=False, key_agreement=False, key_cert_sign=True,
                                     crl_sign=True, encipher_only=False, decipher_only=False), critical=True)
        .add_extension(x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False)
        .sign(key, hashes.SHA256()))
    return key, certificate


def issued(issuer_key, issuer):
    """A server key and the certificate the given CA issues it for 127.0.0.1."""
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = (
        x509.CertificateBuilder().subject_name(_name("127.0.0.1")).issuer_name(issuer.subject)
        .public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]), critical=False)
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False)
        .add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(issuer_key.public_key()), critical=False)
        .sign(issuer_key, hashes.SHA256()))
    return key, certificate


def pem(certificate):
    return certificate.public_bytes(serialization.Encoding.PEM).decode("ascii")


class Answering(http.server.BaseHTTPRequestHandler):
    """A controller whose system answers every read."""

    def do_GET(self):
        body = json.dumps({"PowerState": "On"}).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        return


@pytest.fixture(name="served")
def served_fixture(request, tmp_path):
    """A controller on 127.0.0.1 presenting a certificate its own CA issued:
    its endpoint, and that CA as PEM."""
    issuer_key, issuer = authority("controller authority")
    key, certificate = issued(issuer_key, issuer)
    chain, private = tmp_path / "server.pem", tmp_path / "server.key"
    chain.write_text(pem(certificate))
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                          serialization.NoEncryption()))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    server = http.server.HTTPServer(("127.0.0.1", 0), Answering)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    def stop():
        server.shutdown()
        server.server_close()
        thread.join()

    request.addfinalizer(stop)
    return "https://127.0.0.1:%d/redfish/v1/Systems/1" % server.server_address[1], pem(issuer)


# A declared bundle is the one anchor the controller's transport verifies
# against: the system store is never loaded beside it, and the context keeps
# requiring a certificate and checking the host name.
def test_a_trust_bundle_is_the_only_anchor(monkeypatch):
    _key, issuer = authority("controller authority")

    def forbidden(*args, **kwargs):
        raise AssertionError("the system trust store was loaded beside a declared bundle")

    monkeypatch.setattr(ssl.SSLContext, "load_default_certs", forbidden)
    handlers = [h for h in redfish_control._opener(True, pem(issuer)).handlers
                if isinstance(h, urllib.request.HTTPSHandler)]
    assert len(handlers) == 1
    context = handlers[0]._context
    assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname
    assert context.get_ca_certs(binary_form=True) == [issuer.public_bytes(serialization.Encoding.DER)]


# The bundle decides the handshake: a controller whose certificate it anchors
# answers, and one it does not anchor, like one the system store does not, is
# refused as an unverified certificate rather than read as silence.
def test_a_controller_its_bundle_anchors_is_verified_and_another_is_refused(served):
    endpoint, anchor = served
    assert redfish_control.Client(endpoint, "operator", "p4ssw0rd", ca_data=anchor).fetch()[:2] == (
        200, {"PowerState": "On"})
    _key, other = authority("another authority")
    for bundle, named in ((pem(other), "its declared trust bundle"), ("", "the system trust store")):
        with pytest.raises(redfish_control.UnverifiedCertificate) as failure:
            redfish_control.Client(endpoint, "operator", "p4ssw0rd", ca_data=bundle).fetch()
        assert str(failure.value) == (
            "reading /redfish/v1/Systems/1: the controller's certificate did not verify against " + named)


# A bundle beside verification turned off contradicts itself, and one ssl
# cannot load anchors nothing, so both refuse when the client is built, before
# any request, rather than surfacing as a controller that did not answer.
def test_a_bundle_with_verification_disabled_refuses(monkeypatch):
    recorder = Recorder()
    monkeypatch.setattr(redfish_control, "_opener", recorder.opener)
    _key, issuer = authority("controller authority")
    with pytest.raises(redfish_control.ControllerError, match="requires verification"):
        redfish_control.Client(ENDPOINT, "operator", "p4ssw0rd", verify=False, ca_data=pem(issuer))
    assert not recorder.trust and not recorder.requests
