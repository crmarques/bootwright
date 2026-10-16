"""Synthetic registry exchanges prove that only the broker receives credentials."""

from __future__ import annotations

import base64
import io
import json
import socket
import ssl
import subprocess
import time

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_pull_broker as broker

REGISTRY = "registry.example.test"
REPOSITORY = REGISTRY + "/vendor/ceph"
DIGEST = "a" * 64
PASSWORD = "private-registry-password"
TOKEN = "private-registry-token"
MATERIAL = {"url": REGISTRY, "username": "operator", "password": PASSWORD}
CHALLENGE = 'Bearer realm="https://' + REGISTRY + '/token",service="registry",scope="repository:vendor/ceph:pull"'


class Response(io.BytesIO):
    def __init__(self, status=200, body=b"{}", headers=None):
        super().__init__(body)
        self.status = status
        self.headers = {"Content-Length": str(len(body)), **(headers or {})}

    def getheader(self, name, default=None):
        return next((value for key, value in self.headers.items() if key.lower() == name.lower()), default)

    def getheaders(self):
        return list(self.headers.items())


class Connections:
    def __init__(self, rows):
        self.rows, self.calls, self.closed = list(rows), [], 0

    def __call__(self, host, **options):
        assert host == REGISTRY and options["port"] == 443
        assert options["context"].verify_mode == ssl.CERT_REQUIRED
        assert 0 < options["timeout"] <= 30
        owner = self

        class Connection:
            sock = None

            def request(self, method, path, headers):
                owner.calls.append((host, method, path, dict(headers)))

            def getresponse(self):
                return owner.rows.pop(0)

            def close(self):
                owner.closed += 1

        return Connection()


def upstream(rows):
    connections = Connections(rows)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    value = broker.Upstream(REGISTRY, REPOSITORY, MATERIAL, time.monotonic() + 60, connection=connections, context=context)
    return value, connections


def boundary(up):
    value = object.__new__(broker.Broker)
    value.namespace, value.digest, value.upstream = "vendor/ceph", DIGEST, up
    value.total = 0
    import threading
    value.lock = threading.Lock()
    value.stopping = threading.Event()
    value.deadline = time.monotonic() + 60
    return value


def test_registry_authentication_is_owned_by_broker_and_never_forwarded():
    up, connections = upstream([
        Response(401, headers={"WWW-Authenticate": CHALLENGE}), Response(body=json.dumps({"token": TOKEN}).encode()),
        Response(body=b"manifest", headers={"Content-Type": "application/json", "Set-Cookie": PASSWORD, "WWW-Authenticate": TOKEN}),
    ])
    output = io.BytesIO()
    up.fetch("GET", "/v2/vendor/ceph/manifests/sha256:" + DIGEST, lambda response: boundary(up).response(output, response, "GET", 1024))
    assert connections.calls[0][3].get("Authorization") is None
    assert connections.calls[1][3]["Authorization"] == "Basic " + base64.b64encode(b"operator:" + PASSWORD.encode()).decode()
    assert connections.calls[1][2].startswith("/token?")
    assert connections.calls[2][3]["Authorization"] == "Bearer " + TOKEN
    assert output.getvalue().endswith(b"manifest")
    assert PASSWORD.encode() not in output.getvalue() and TOKEN.encode() not in output.getvalue()
    assert b"Set-Cookie" not in output.getvalue() and b"WWW-Authenticate" not in output.getvalue()
    assert connections.closed == 3


@pytest.mark.parametrize("realm", ["https://foreign.example.test/token", "http://registry.example.test/token",
                                   "https://operator:password@registry.example.test/token", "https://registry.example.test:444/token"])
def test_wrong_token_realm_receives_neither_basic_nor_bearer(realm):
    challenge = CHALLENGE.replace("https://registry.example.test/token", realm)
    up, connections = upstream([Response(401, headers={"WWW-Authenticate": challenge})])
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", "/v2/", lambda unused: pytest.fail("unproved response"))
    assert len(connections.calls) == 1 and "Authorization" not in connections.calls[0][3]


def test_redirect_is_not_followed_and_private_error_is_redacted():
    up, connections = upstream([Response(307, ("redirect denied " + PASSWORD).encode(), {"Location": "https://foreign.example.test/"})])
    output = io.BytesIO()
    up.fetch("GET", "/v2/", lambda response: boundary(up).response(output, response, "GET", 1024))
    assert len(connections.calls) == 1 and b"Location" not in output.getvalue()
    assert b"HTTP/1.1 502" in output.getvalue() and b"redirect denied" in output.getvalue()
    assert PASSWORD.encode() not in output.getvalue()


@pytest.mark.parametrize("path", ["https://foreign.example.test/", "/v2/other/blobs/sha256:" + DIGEST,
                                  "/v2/vendor/ceph/manifests/latest", "/v2/vendor/ceph/blobs/../../token", "/token"])
def test_native_client_cannot_expand_the_frozen_repository(path):
    with pytest.raises(broker.BrokerError):
        boundary(None).path("GET", path)


@pytest.mark.parametrize("header", [b"Authorization: Basic secret\r\n", b"Proxy-Authorization: secret\r\n",
                                    b"Cookie: secret\r\n", b"Transfer-Encoding: chunked\r\n", b"Content-Length: 9\r\n"])
def test_downstream_secret_or_body_is_refused(header):
    with pytest.raises(broker.BrokerError):
        broker.read_request(io.BytesIO(b"GET /v2/ HTTP/1.1\r\n" + header + b"\r\n"))


def test_repeated_accept_headers_still_obey_total_byte_limit():
    header = b"Accept: application/vnd.oci.image.manifest.v1+json\r\n"
    request = b"GET /v2/ HTTP/1.1\r\n" + header * (broker.MAX_HEADERS // len(header) + 1) + b"\r\n"
    with pytest.raises(broker.BrokerError):
        broker.read_request(io.BytesIO(request))


def test_byte_budget_and_deadline_refuse_without_unbounded_forwarding():
    up, unused = upstream([])
    value = boundary(up)
    with pytest.raises(broker.BrokerError):
        value.response(io.BytesIO(), Response(body=b"oversized"), "GET", 2)
    value.deadline = time.monotonic() - 1
    with pytest.raises(broker.BrokerError):
        value.response(io.BytesIO(), Response(), "GET", 1024)
    up.deadline = time.monotonic() - 1
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", "/v2/", lambda unused: None)


@pytest.mark.parametrize("client_identity,extra_headers,permitted", [
    (True, "", True),
    (False, "", False),
    (True, "hOsT: " + REGISTRY + "\r\n", False),
    (True, "Content-Length: 0\r\ncOnTeNt-LeNgTh: 0\r\n", False),
    (True, "Transfer-Encoding: chunked\r\ntransfer-encoding: chunked\r\n", False),
    (True, "Authorization: secret\r\nauthorization: secret\r\n", False),
    (True, "Proxy-Authorization: secret\r\nproxy-authorization: secret\r\n", False),
    (True, "Cookie: secret\r\ncookie: secret\r\n", False),
    (True, "X-Other: value\r\nx-other: value\r\n", False),
])
def test_native_tls_client_uses_exact_reference_without_receiving_credentials(tmp_path, client_identity, extra_headers, permitted):
    """Real loopback TLS and synthetic upstream; no registry or container effects."""
    up, connections = upstream([Response(body=b"native-payload")])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}
    calls = []

    def command(argv, timeout, limit):
        assert argv[:2] == ["/usr/bin/openssl", "req"]
        calls.append(list(argv))
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=True, env={"PATH": "/usr/bin:/bin", "LANG": "C"})
        assert len(result.stdout) + len(result.stderr) <= limit
        return result.stdout

    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60, command, upstream=up) as service:
        environment = service.environment
        assert PASSWORD not in str(environment) and TOKEN not in str(environment)
        assert environment["NO_PROXY"] == environment["no_proxy"] == ""
        port = int(environment["HTTPS_PROXY"].rsplit(":", 1)[1])
        connection = socket.create_connection(("127.0.0.1", port), timeout=5)
        connection.sendall(("CONNECT " + REGISTRY + ":443 HTTP/1.1\r\nHost: " + REGISTRY + ":443\r\n\r\n").encode())
        assert connection.recv(4096) == b"HTTP/1.1 200 Connection Established\r\n\r\n"
        context = ssl.create_default_context(cafile=service.cert_directory + "/ca.crt")
        if client_identity:
            context.load_cert_chain(service.cert_directory + "/client.cert", service.cert_directory + "/client.key")

        def fetch():
            with context.wrap_socket(connection, server_hostname=REGISTRY) as secured:
                # containers/image emits separate Accept lines for manifest types.
                headers = ("Accept: application/vnd.oci.image.manifest.v1+json\r\n"
                           "aCcEpT: application/vnd.docker.distribution.manifest.v2+json\r\n"
                           "Accept: application/vnd.docker.distribution.manifest.list.v2+json\r\n")
                secured.sendall(("GET /v2/vendor/ceph/manifests/sha256:" + DIGEST + " HTTP/1.1\r\nHost: " + REGISTRY
                                + "\r\n" + headers + extra_headers + "\r\n").encode())
                with secured.makefile("rb") as stream:
                    return stream.read()

        if permitted:
            output = fetch()
            assert output.endswith(b"native-payload") and PASSWORD.encode() not in output
            assert connections.calls[0][2] == "/v2/vendor/ceph/manifests/sha256:" + DIGEST
            assert connections.calls[0][3]["Accept"] == (
                "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
        elif client_identity:
            assert fetch() == b""
            assert connections.calls == []
        else:
            with pytest.raises((ssl.SSLError, ConnectionResetError)):
                fetch()
            assert connections.calls == []
    assert not service.thread.is_alive() and all(not worker.is_alive() for worker in service.workers)
    assert all(PASSWORD.encode() not in path.read_bytes() for path in tmp_path.iterdir() if path.is_file())
    assert len(calls) == 1
    assert list(tmp_path.iterdir()) == []


def test_teardown_closes_partial_connect_without_leaving_workers(tmp_path):
    up, unused = upstream([])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}
    # No handshake is needed: the peer stalls while writing its CONNECT header.
    started = time.monotonic()
    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60,
                       lambda *args: pytest.fail("certificate effect"), upstream=up, tls=object()) as service:
        port = int(service.environment["HTTPS_PROXY"].rsplit(":", 1)[1])
        connection = socket.create_connection(("127.0.0.1", port), timeout=5)
        connection.sendall(b"CONNECT registry.example.test:443 HTTP/1.1\r\n")
        while not service.workers and time.monotonic() - started < 2:
            time.sleep(0.01)
        assert service.workers
    connection.close()
    assert time.monotonic() - started < 3
    assert all(not worker.is_alive() for worker in service.workers)


def test_foreign_connect_is_denied_before_upstream_access(tmp_path):
    up, connections = upstream([])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}
    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60,
                       lambda *args: pytest.fail("certificate effect"), upstream=up, tls=object()) as service:
        port = int(service.environment["HTTPS_PROXY"].rsplit(":", 1)[1])
        with socket.create_connection(("127.0.0.1", port), timeout=5) as connection:
            connection.sendall(b"CONNECT foreign.example.test:443 HTTP/1.1\r\nHost: foreign.example.test\r\n\r\n")
            assert connection.recv(1024) == b""
    assert connections.calls == []
