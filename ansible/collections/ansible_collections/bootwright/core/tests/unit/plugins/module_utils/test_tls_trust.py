"""Real loopback TLS proofs on the controller and managed Python floor.

Fixture keys are public test scalars 1 and 3 on P-256. Their certificates
cover 2020 to 2120 unless the fixture explicitly exercises invalid dates.
No cryptography package, native process or external endpoint is needed.
"""

from __future__ import annotations

import contextlib
import http.server
from pathlib import Path
import ssl
import threading

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, tls_trust

FIXTURES = Path(__file__).parent / "fixtures" / "tls-trust"


def pem(name):
    return (FIXTURES / (name + ".pem")).read_text(encoding="ascii")


@contextlib.contextmanager
def serving(name):
    received = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            received.append(self.headers.get("Authorization"))
            self.send_response(200)
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"{}")

        def log_message(self, *args):
            return

    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    key = "other-server.key" if name == "other-leaf" else "server.key"
    context.load_cert_chain(str(FIXTURES / (name + ".pem")), str(FIXTURES / key))
    server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield "https://127.0.0.1:%d/redfish/v1/Systems/1" % server.server_address[1], received
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        assert not thread.is_alive()


@pytest.mark.parametrize("name", ["self-signed", "issued"])
def test_full_verification_accepts_direct_leaf_trust_on_every_python(name):
    with serving(name) as (endpoint, received):
        client = redfish_control.Client(endpoint, "test-user", "test-password", ca_data=pem(name))
        assert client.fetch()[0] == 200 and len(received) == 1
    context, identity = tls_trust.context(pem(name))
    assert identity is None and context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED


@pytest.mark.parametrize("name", ["wrong-name", "no-san"])
def test_only_explicit_certificate_identity_accepts_mismatching_or_absent_names(name):
    with serving(name) as (endpoint, received):
        with pytest.raises(redfish_control.UnverifiedCertificate):
            redfish_control.Client(endpoint, "test-user", "test-password", ca_data=pem(name)).fetch()
        assert not received
        client = redfish_control.Client(endpoint, "test-user", "test-password", ca_data=pem(name), verification="certificate")
        assert client.fetch()[0] == 200 and len(received) == 1
    context, identity = tls_trust.context(pem(name), "certificate")
    assert identity and not context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED


@pytest.mark.parametrize("name", ["self-signed", "issued", "wrong-name", "other-leaf", "renewed"])
def test_another_certificate_never_receives_credentials_even_with_the_same_key_or_issuer(name):
    with serving(name) as (endpoint, received):
        # A CA beside the selected leaf must not authorize another leaf.
        client = redfish_control.Client(endpoint, "test-user", "test-password",
                                        ca_data=pem("issued") + pem("ca"), verification="certificate")
        if name == "issued":
            assert client.fetch()[0] == 200 and len(received) == 1
        else:
            with pytest.raises(redfish_control.UnverifiedCertificate):
                client.fetch()
            assert not received


@pytest.mark.parametrize("name", ["expired", "future", "client-only"])
def test_certificate_identity_preserves_date_and_server_purpose_checks(name):
    with serving(name) as (endpoint, received):
        with pytest.raises(redfish_control.UnverifiedCertificate):
            redfish_control.Client(endpoint, "test-user", "test-password", ca_data=pem(name), verification="certificate").fetch()
        assert not received


def test_disabling_verification_is_explicit_and_requires_no_trust_bundle():
    with serving("wrong-name") as (endpoint, received):
        assert redfish_control.Client(endpoint, "test-user", "test-password", verify=False, verification="none").fetch()[0] == 200
        assert len(received) == 1
        with pytest.raises(redfish_control.ControllerError, match="requires verification"):
            redfish_control.Client(endpoint, "test-user", "test-password", verify=False, ca_data=pem("wrong-name"), verification="none")


@pytest.mark.parametrize("bundle", ["", "ca", "two-leaves"])
def test_certificate_mode_requires_exactly_one_server_certificate(bundle):
    data = "" if not bundle else pem("ca") if bundle == "ca" else pem("issued") + pem("other-leaf")
    with pytest.raises((ValueError, redfish_control.ControllerError)):
        redfish_control.Client("https://bmc.example.test/redfish/v1/Systems/1", "test-user", "test-password",
                               ca_data=data, verification="certificate")


def test_certificate_mode_cannot_silently_use_plain_http():
    with pytest.raises(redfish_control.ControllerError, match="HTTPS"):
        redfish_control.Client("http://bmc.example.test/redfish/v1/Systems/1", "test-user", "test-password",
                               ca_data=pem("issued"), verification="certificate")
