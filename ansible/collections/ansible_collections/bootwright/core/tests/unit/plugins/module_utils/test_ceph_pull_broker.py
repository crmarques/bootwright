"""Synthetic registry exchanges prove that only the broker receives credentials."""

from __future__ import annotations

import base64
import io
import json
import os
import socket
import ssl
import stat
import subprocess
import sys
import threading
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
    value.failure = None
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
    up.fetch("GET", "/v2/vendor/ceph/manifests/sha256:" + DIGEST, lambda response: boundary(up).response(output, response, "GET", 1024, "manifest"))
    assert connections.calls[0][3].get("Authorization") is None
    assert connections.calls[1][3]["Authorization"] == "Basic " + base64.b64encode(b"operator:" + PASSWORD.encode()).decode()
    assert connections.calls[1][2].startswith("/token?")
    assert connections.calls[2][3]["Authorization"] == "Bearer " + TOKEN
    assert output.getvalue().endswith(b"manifest")
    assert PASSWORD.encode() not in output.getvalue() and TOKEN.encode() not in output.getvalue()
    assert b"Set-Cookie" not in output.getvalue() and b"WWW-Authenticate" not in output.getvalue()
    assert connections.closed == 3


@pytest.mark.parametrize("request_class,status", [
    (kind, status) for kind in ("registry", "manifest", "blob")
    for status in (301, 302, 303, 307, 308, 401, 403, 404, 429, 500, 502, 503)
    if kind != "blob" or status not in (301, 302, 303, 307, 308)
])
def test_final_http_failure_retains_only_bounded_class_and_status(request_class, status):
    rows = [Response(status, b"private upstream body", {"Location": "https://foreign.example.test/?token=" + TOKEN})]
    if status == 401:
        rows[:0] = [Response(401, headers={"WWW-Authenticate": CHALLENGE}), Response(body=json.dumps({"token": TOKEN}).encode())]
    up, connections = upstream(rows)
    value = boundary(up)
    output = io.BytesIO()
    paths = {"registry": "/v2/", "manifest": "/v2/vendor/ceph/manifests/sha256:" + DIGEST,
             "blob": "/v2/vendor/ceph/blobs/sha256:" + DIGEST}
    path = paths[request_class]
    maximum = value.path("GET", path)
    up.fetch("GET", path, lambda response: value.response(output, response, "GET", maximum, request_class))
    assert value.failure == (request_class, status)
    assert value.failure_cause == "broker observed " + request_class + " upstream HTTP " + str(status)
    assert output.getvalue().startswith(b"HTTP/1.1 502 Bad Gateway")
    assert b"Location" not in output.getvalue() and TOKEN.encode() not in output.getvalue()
    assert len(connections.calls) == (3 if status == 401 else 1)
    assert boundary(up).failure_cause == ""


def test_successful_response_does_not_record_a_failure_and_retention_stays_bounded():
    up, unused = upstream([])
    value = boundary(up)
    value.response(io.BytesIO(), Response(), "GET", broker.MAX_METADATA, "registry")
    assert value.failure_cause == ""
    for status in range(400, 512):
        value.response(io.BytesIO(), Response(status), "GET", broker.MAX_METADATA, "manifest")
    assert value.failure == ("manifest", 511)
    value.response(io.BytesIO(), Response(), "GET", broker.MAX_METADATA, "registry")
    assert value.failure_cause == "broker observed manifest upstream HTTP 511"


@pytest.mark.parametrize("realm", ["https://foreign.example.test/token", "http://registry.example.test/token",
                                   "https://operator:password@registry.example.test/token", "https://registry.example.test:444/token"])
def test_wrong_token_realm_receives_neither_basic_nor_bearer(realm):
    challenge = CHALLENGE.replace("https://registry.example.test/token", realm)
    up, connections = upstream([Response(401, headers={"WWW-Authenticate": challenge})])
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", "/v2/", lambda unused: pytest.fail("unproved response"))
    assert len(connections.calls) == 1 and "Authorization" not in connections.calls[0][3]


EXPIRED = CHALLENGE + ',error="invalid_token",error_description="The access token expired"'


def test_expired_bearer_challenge_reauthenticates_once():
    """RFC 6750 section 3 lets a Bearer challenge carry error attributes for an expired token."""
    up, connections = upstream([
        Response(401, headers={"WWW-Authenticate": EXPIRED}), Response(body=json.dumps({"token": TOKEN}).encode()),
        Response(body=b"manifest", headers={"Content-Type": "application/json"}),
    ])
    up.bearer = "Bearer stale-registry-token"
    output = io.BytesIO()
    value = boundary(up)
    up.fetch("GET", "/v2/vendor/ceph/manifests/sha256:" + DIGEST, lambda response: value.response(output, response, "GET", 1024, "manifest"))
    basic = "Basic " + base64.b64encode(b"operator:" + PASSWORD.encode()).decode()
    assert len(connections.calls) == 3
    assert connections.calls[0][3]["Authorization"] == "Bearer stale-registry-token"
    assert connections.calls[1][3]["Authorization"] == basic and connections.calls[1][2].startswith("/token?")
    assert connections.calls[2][3]["Authorization"] == "Bearer " + TOKEN
    assert [call for call in connections.calls if call[3].get("Authorization") == basic] == [connections.calls[1]]
    assert output.getvalue().endswith(b"manifest") and value.failure_cause == ""


@pytest.mark.parametrize("challenge", [
    EXPIRED.replace("repository:vendor/ceph:pull", "repository:vendor/other:pull"),
    EXPIRED.replace("https://registry.example.test/token", "https://foreign.example.test/token"),
    EXPIRED + ',unknown="value"',
    EXPIRED + ',error="invalid_token"',
    CHALLENGE + ',error_uri="https://foreign.example.test/",error_uri="https://foreign.example.test/"',
    'Bearer error="invalid_token",service="registry",scope="repository:vendor/ceph:pull"',
])
def test_challenge_error_attributes_do_not_relax_scope_or_realm(challenge):
    up, connections = upstream([Response(401, headers={"WWW-Authenticate": challenge})])
    up.bearer = "Bearer stale-registry-token"
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", "/v2/", lambda unused: pytest.fail("unproved response"))
    assert len(connections.calls) == 1


def test_token_endpoint_refusal_is_retained_as_status_only(tmp_path):
    private_body = b"private token refusal for operator " + PASSWORD.encode()
    up, connections = upstream([Response(401, headers={"WWW-Authenticate": EXPIRED}), Response(401, private_body)])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}

    def command(argv, timeout, limit):
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=True, env={"PATH": "/usr/bin:/bin", "LANG": "C"})
        assert len(result.stdout) + len(result.stderr) <= limit
        return result.stdout

    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60, command, upstream=up) as service:
        port = int(service.environment["HTTPS_PROXY"].rsplit(":", 1)[1])
        connection = socket.create_connection(("127.0.0.1", port), timeout=5)
        connection.sendall(("CONNECT " + REGISTRY + ":443 HTTP/1.1\r\n\r\n").encode())
        assert connection.recv(4096) == b"HTTP/1.1 200 Connection Established\r\n\r\n"
        context = ssl.create_default_context(cafile=service.cert_directory + "/ca.crt")
        context.load_cert_chain(service.cert_directory + "/client.cert", service.cert_directory + "/client.key")
        with context.wrap_socket(connection, server_hostname=REGISTRY) as secured:
            secured.sendall(("GET /v2/vendor/ceph/manifests/sha256:" + DIGEST + " HTTP/1.1\r\nHost: " + REGISTRY + "\r\n\r\n").encode())
            with secured.makefile("rb") as stream:
                assert stream.read() == b""
    cause = service.failure_cause
    assert cause == "broker observed token upstream HTTP 401"
    basic = base64.b64encode(b"operator:" + PASSWORD.encode()).decode()
    assert all(secret not in cause for secret in (PASSWORD, basic, "private token refusal", "operator", "invalid_token"))
    assert len(connections.calls) == 2


def test_redirect_is_not_followed_and_private_error_is_redacted():
    up, connections = upstream([Response(307, ("redirect denied " + PASSWORD).encode(), {"Location": "https://foreign.example.test/"})])
    output = io.BytesIO()
    up.fetch("GET", "/v2/", lambda response: boundary(up).response(output, response, "GET", 1024, "manifest"))
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
        value.response(io.BytesIO(), Response(body=b"oversized"), "GET", 2, "blob")
    value.deadline = time.monotonic() - 1
    with pytest.raises(broker.BrokerError):
        value.response(io.BytesIO(), Response(), "GET", 1024, "manifest")
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
        assert sorted(os.listdir(os.path.join(environment["HOME"], ".config", "containers", "registries.d"))) == []
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


def test_native_environment_has_empty_lookaside_configuration(tmp_path):
    """containers/image v5.34.3 docker/registries_d.go:93-105 prefers $HOME/.config/containers/registries.d when it exists."""
    up, unused = upstream([])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}
    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60,
                       lambda *args: pytest.fail("certificate effect"), upstream=up, tls=object()) as service:
        home = service.environment["HOME"]
        for relative in (".config", ".config/containers", ".config/containers/registries.d"):
            info = os.stat(os.path.join(home, relative), follow_symlinks=False)
            assert stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700
        assert os.listdir(os.path.join(home, ".config", "containers", "registries.d")) == []
        assert os.listdir(os.path.join(home, ".config", "containers")) == ["registries.d"]
    assert not os.path.exists(home)
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


DELIVERY = "delivery.example.test"
SIGNED = "https://" + DELIVERY + "/object?signature=private-delivery-capability"
BLOB_PATH = "/v2/vendor/ceph/blobs/sha256:" + DIGEST


def dns_row(ip="8.8.8.8", family=socket.AF_INET):
    return [family, socket.SOCK_STREAM, socket.IPPROTO_TCP, [ip, 443] + ([0, 0] if family == socket.AF_INET6 else [])]


def delivery_setup(monkeypatch, status=200, method="GET", redirect=307):
    up, registry = upstream([Response(401, headers={"WWW-Authenticate": CHALLENGE}),
                             Response(body=json.dumps({"token": TOKEN}).encode()),
                             Response(redirect, headers={"Location": SIGNED, "Set-Cookie": PASSWORD})])
    calls, resolved = [], []
    delivered = Response(status, b"payload" if status == 200 else SIGNED.encode(),
                         {"Location": SIGNED, "Set-Cookie": PASSWORD, "WWW-Authenticate": CHALLENGE,
                          "Content-Type": SIGNED, "Docker-Content-Digest": SIGNED, "Docker-Distribution-Api-Version": SIGNED})

    def resolver(argv, *, data, timeout, limit):
        assert argv == [sys.executable, "-I", "-B", "-c", broker.DNS_SOURCE]
        assert data == DELIVERY.encode() and 0 < timeout <= 5 and limit == 8192
        resolved.append(data)
        return 0, json.dumps([dns_row()]).encode()

    class DeliveryConnection:
        sock = None

        def __init__(self, host, **options):
            assert host == DELIVERY and options["port"] == 443
            assert options["addresses"] == [(socket.AF_INET, socket.SOCK_STREAM, socket.IPPROTO_TCP, "", ("8.8.8.8", 443))]
            assert options["context"] is up.context and options["context"].check_hostname
            assert 0 < options["timeout"] <= 30 and options["deadline"] == up.deadline

        def request(self, selected_method, path, headers):
            assert selected_method == method
            calls.append((path, headers))

        def getresponse(self):
            return delivered

        def close(self):
            pass

    up.resolver_runner = resolver
    monkeypatch.setattr(broker, "RegistryConnection", DeliveryConnection)
    return up, registry, delivered, calls, resolved


@pytest.mark.parametrize("method", ["GET", "HEAD"])
@pytest.mark.parametrize("redirect", [301, 302, 303, 307, 308])
def test_blob_delivery_is_one_hop_without_registry_credentials(monkeypatch, method, redirect):
    up, registry, delivered, calls, resolved = delivery_setup(monkeypatch, method=method, redirect=redirect)
    output = io.BytesIO()
    value = boundary(up)
    up.fetch(method, BLOB_PATH, lambda response: value.response(output, response, method, broker.MAX_BLOB, "blob"))
    assert len(registry.calls) == 3 and registry.calls[-1][3]["Authorization"] == "Bearer " + TOKEN
    assert resolved == [DELIVERY.encode()] and len(calls) == 1
    assert calls[0] == ("/object?signature=private-delivery-capability", {"Accept-Encoding": "identity", "User-Agent": "bootwright-pull-broker-v1"})
    assert output.getvalue().startswith(b"HTTP/1.1 200")
    assert output.getvalue().endswith(b"payload") if method == "GET" else delivered.tell() == 0
    assert all(secret.encode() not in output.getvalue() for secret in (PASSWORD, TOKEN, SIGNED, "private-delivery-capability"))
    assert b"Location" not in output.getvalue() and b"Set-Cookie" not in output.getvalue()
    assert not up.connections


@pytest.mark.parametrize("status", [301, 302, 307, 308, 401, 403, 500])
def test_delivery_error_neither_authenticates_nor_follows_and_never_reflects_body(monkeypatch, status):
    up, registry, delivered, calls, unused = delivery_setup(monkeypatch, status)
    output = io.BytesIO()
    value = boundary(up)
    up.fetch("GET", BLOB_PATH, lambda response: value.response(output, response, "GET", broker.MAX_BLOB, "blob"))
    assert len(registry.calls) == 3 and len(calls) == 1 and delivered.tell() == 0
    assert value.failure_cause == "broker observed blob upstream HTTP " + str(status)
    assert b"blob delivery refused" in output.getvalue()
    assert all(secret.encode() not in output.getvalue() for secret in (PASSWORD, TOKEN, SIGNED, "private-delivery-capability"))


@pytest.mark.parametrize("location", ["", "/relative", "http://delivery.example.test/x", "https://delivery.example.test:444/x",
                                      "https://user:password@delivery.example.test/x", "https://delivery.example.test/x#fragment",
                                      "https://delivery.example.test/x#", "https://delivery.example.test/\\x", "https://delivery.example.test/ x",
                                      "https://delivery.example.test/é", "https://-bad.example/x", "https://bad..example/x",
                                      "https://delivery.example.test//path", "https://delivery.example.test/" + "a" * 4096])
def test_malformed_delivery_location_refuses_without_resolution(location):
    up, calls = upstream([Response(307, headers={"Location": location})])
    up.resolver_runner = lambda *args, **kwargs: pytest.fail("unexpected resolution")
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", BLOB_PATH, lambda response: pytest.fail("unexpected delivery"))
    assert len(calls.calls) == 1


def test_duplicate_location_and_token_redirect_refuse():
    response = Response(307)
    response.getheaders = lambda: [("Location", SIGNED), ("location", SIGNED)]
    with pytest.raises(broker.BrokerError):
        broker.delivery_location(response)
    up, calls = upstream([Response(401, headers={"WWW-Authenticate": CHALLENGE}), Response(307, headers={"Location": SIGNED})])
    up.resolver_runner = lambda *args, **kwargs: pytest.fail("unexpected resolution")
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", BLOB_PATH, lambda response: pytest.fail("unexpected delivery"))
    assert len(calls.calls) == 2


@pytest.mark.parametrize("ip", [
    "0.0.0.0", "10.0.0.1", "127.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.1",
    "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
    "::", "::1", "fc00::1", "fec0::1", "feff::1", "fe80::1", "ff02::1", "2001:db8::1", "::ffff:8.8.8.8",
    "2002:808:808::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2"
])
def test_any_nonpublic_or_tunnel_answer_refuses_entire_delivery(ip):
    family = socket.AF_INET6 if ":" in ip else socket.AF_INET
    with pytest.raises((broker.BrokerError, ValueError)):
        broker.delivery_addresses(json.dumps([dns_row(), dns_row(ip, family)]).encode())


@pytest.mark.parametrize("rows", [
    [], [dns_row()] * 17, [dns_row(), dns_row()], [[True, 1, 6, ["8.8.8.8", 443]]],
    [[2, 1, 6, ["8.8.8.8", True]]], [[2, 1, 6, ["8.8.8.8", 80]]],
    [[10, 1, 6, ["2606:4700:4700::1111", 443, 0, 1]]],
    [[10, 1, 6, ["2606:4700:4700:0:0:0:0:1111", 443, 0, 0]]],
    [[2, 2, 17, ["8.8.8.8", 443]]], {"unexpected": []}
])
def test_invalid_or_ambiguous_dns_payload_refuses(rows):
    with pytest.raises((broker.BrokerError, ValueError)):
        broker.delivery_addresses(json.dumps(rows).encode())


def test_dns_output_limit_and_public_ipv6():
    with pytest.raises(broker.BrokerError):
        broker.delivery_addresses(b" " * 8193)
    assert broker.delivery_addresses(json.dumps([dns_row("2606:4700:4700::1111", socket.AF_INET6)]).encode()) == [
        (socket.AF_INET6, socket.SOCK_STREAM, socket.IPPROTO_TCP, "", ("2606:4700:4700::1111", 443, 0, 0))]


def test_resolution_stop_race_cannot_admit_connection(monkeypatch):
    up, unused = upstream([Response(307, headers={"Location": SIGNED})])
    entered, release = threading.Event(), threading.Event()
    errors = []

    def resolver(*args, **kwargs):
        entered.set()
        assert release.wait(2)
        return 0, json.dumps([dns_row()]).encode()

    def fetch():
        try:
            up.fetch("GET", BLOB_PATH, lambda response: pytest.fail("late response"))
        except broker.BrokerError as error:
            errors.append(str(error))

    up.resolver_runner = resolver
    monkeypatch.setattr(broker, "RegistryConnection", lambda *args, **kwargs: pytest.fail("late connection"))
    worker = threading.Thread(target=fetch)
    worker.start()
    assert entered.wait(2)
    up.close()
    release.set()
    worker.join(2)
    assert not worker.is_alive() and errors == ["blob delivery resolution"] and not up.connections


def test_fixed_dns_exec_uses_bounded_owner_without_live_dns():
    fake = "import socket\nsocket.getaddrinfo = lambda *a, **k: [(2, 1, 6, '', ('8.8.8.8', 443))]\n"
    code, raw = broker.ceph_native.bounded_command(
        [sys.executable, "-I", "-B", "-c", fake + broker.DNS_SOURCE], data=DELIVERY.encode(), timeout=2, limit=8192)
    assert code == 0 and broker.delivery_addresses(raw)[0][-1] == ("8.8.8.8", 443)


def test_hung_dns_exec_is_killed_and_reaped(monkeypatch):
    fake = "import socket, time\nsocket.getaddrinfo = lambda *a, **k: time.sleep(60)\n"
    processes = []
    popen = broker.ceph_native.subprocess.Popen

    def tracked(*args, **kwargs):
        process = popen(*args, **kwargs)
        processes.append(process)
        return process

    monkeypatch.setattr(broker.ceph_native.subprocess, "Popen", tracked)
    started = time.monotonic()
    with pytest.raises(broker.ceph_native.Refusal):
        broker.ceph_native.bounded_command([sys.executable, "-I", "-B", "-c", fake + broker.DNS_SOURCE],
                                           data=DELIVERY.encode(), timeout=0.1, limit=8192)
    assert time.monotonic() - started < 2
    assert len(processes) == 1 and processes[0].returncode is not None
    assert all(pipe.closed for pipe in (processes[0].stdin, processes[0].stdout, processes[0].stderr))


def test_numeric_connection_pins_address_and_preserves_tls_name(monkeypatch):
    calls = []

    class Socket:
        def settimeout(self, value):
            assert value > 0

        def connect(self, address):
            calls.append(address)

        def do_handshake(self):
            calls.append("handshake")

        def close(self):
            pass

    class TLS:
        def wrap_socket(self, sock, *, server_hostname, do_handshake_on_connect):
            assert server_hostname == DELIVERY and not do_handshake_on_connect
            return sock

    monkeypatch.setattr(socket, "getaddrinfo", lambda *args, **kwargs: pytest.fail("DNS repeated"))
    monkeypatch.setattr(socket, "socket", lambda *args: Socket())
    connection = broker.RegistryConnection(DELIVERY, addresses=broker.delivery_addresses(json.dumps([dns_row()]).encode()),
                                           deadline=time.monotonic() + 5, port=443, timeout=5)
    connection._context = TLS()
    connection.connect()
    connection.close()
    assert calls == [("8.8.8.8", 443), "handshake"]


def test_connect_splits_budget_so_a_black_holed_first_address_falls_back(monkeypatch):
    now, timeouts, attempts = [100.0], [], []

    class Clock:
        @staticmethod
        def monotonic():
            return now[0]

    class Socket:
        def __init__(self, family):
            self.family, self.timeout = family, None

        def settimeout(self, value):
            self.timeout = value
            timeouts.append((self.family, value))

        def connect(self, address):
            attempts.append(address)
            if self.family == socket.AF_INET6:
                now[0] += self.timeout
                raise socket.timeout("black-holed")

        def do_handshake(self):
            attempts.append("handshake")

        def close(self):
            pass

    class TLS:
        def wrap_socket(self, sock, *, server_hostname, do_handshake_on_connect):
            return sock

    addresses = [(socket.AF_INET6, socket.SOCK_STREAM, 6, "", ("2001:db8::1", 443, 0, 0)),
                 (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.0.2.1", 443))]
    monkeypatch.setattr(broker, "time", Clock)
    monkeypatch.setattr(socket, "socket", lambda family, kind, protocol: Socket(family))
    connection = broker.RegistryConnection(REGISTRY, addresses=addresses, deadline=130.0, port=443, timeout=30)
    connection._context = TLS()
    connection.connect()
    assert timeouts[0][0] == socket.AF_INET6 and timeouts[0][1] <= 15
    assert attempts[:2] == [("2001:db8::1", 443, 0, 0), ("192.0.2.1", 443)] and attempts[-1] == "handshake"
    assert all(row[1] > 0 for row in timeouts)


def test_connect_split_bounds_a_stalled_handshake_so_the_next_address_is_tried(monkeypatch):
    now, handshakes, attempts = [100.0], [], []

    class Clock:
        @staticmethod
        def monotonic():
            return now[0]

    class Socket:
        def __init__(self, family):
            self.family, self.timeout = family, None

        def settimeout(self, value):
            self.timeout = value

        def connect(self, address):
            attempts.append(address)

        def do_handshake(self):
            handshakes.append((self.family, self.timeout))
            if self.family == socket.AF_INET6:
                now[0] += self.timeout
                raise socket.timeout("stalled TLS server")

        def close(self):
            pass

    class TLS:
        def wrap_socket(self, sock, *, server_hostname, do_handshake_on_connect):
            return sock

    addresses = [(socket.AF_INET6, socket.SOCK_STREAM, 6, "", ("2001:db8::1", 443, 0, 0)),
                 (socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.0.2.1", 443))]
    monkeypatch.setattr(broker, "time", Clock)
    monkeypatch.setattr(socket, "socket", lambda family, kind, protocol: Socket(family))
    connection = broker.RegistryConnection(REGISTRY, addresses=addresses, deadline=130.0, port=443, timeout=30)
    connection._context = TLS()
    connection.connect()
    assert handshakes[0][0] == socket.AF_INET6 and handshakes[0][1] <= 15
    assert attempts == [("2001:db8::1", 443, 0, 0), ("192.0.2.1", 443)]
    assert [row[0] for row in handshakes] == [socket.AF_INET6, socket.AF_INET] and connection.sock.family == socket.AF_INET


@pytest.mark.parametrize("black_holed", [0, 3])
def test_connect_share_bounds_only_connect_and_handshake(monkeypatch, black_holed):
    now = [100.0]

    class Clock:
        @staticmethod
        def monotonic():
            return now[0]

    class Socket:
        timeout = None

        def settimeout(self, value):
            self.timeout = value

        def connect(self, address):
            if address[1] < black_holed:
                now[0] += self.timeout
                raise socket.timeout("black-holed")
            now[0] += 0.5

        def do_handshake(self):
            now[0] += 0.5

        def close(self):
            pass

    class TLS:
        def wrap_socket(self, sock, *, server_hostname, do_handshake_on_connect):
            return sock

    addresses = [(socket.AF_INET, socket.SOCK_STREAM, 6, "", ("192.0.2.1", port)) for port in range(8)]
    monkeypatch.setattr(broker, "time", Clock)
    monkeypatch.setattr(socket, "socket", lambda family, kind, protocol: Socket())
    connection = broker.RegistryConnection(REGISTRY, addresses=addresses, deadline=200.0, port=443, timeout=30)
    connection._context = TLS()
    connection.connect()
    assert connection.sock.timeout == pytest.approx(130.0 - now[0])
    assert connection.sock.timeout > 30.0 / 8


@pytest.mark.parametrize("result", [(1, b"private resolver failure"), (0, b"not JSON"), (0, b" " * 8193)])
def test_resolver_failure_is_closed_and_private(monkeypatch, result):
    up, unused = upstream([Response(307, headers={"Location": SIGNED})])
    up.resolver_runner = lambda *args, **kwargs: result
    monkeypatch.setattr(broker, "RegistryConnection", lambda *args, **kwargs: pytest.fail("unexpected delivery"))
    with pytest.raises(broker.BrokerError, match="^blob delivery resolution$"):
        up.fetch("GET", BLOB_PATH, lambda response: pytest.fail("unexpected response"))
    assert not up.connections


def test_stop_between_connection_creation_and_registration_cannot_request(monkeypatch):
    up, unused = upstream([Response(307, headers={"Location": SIGNED})])
    up.resolver_runner = lambda *args, **kwargs: (0, json.dumps([dns_row()]).encode())
    closed = []

    class Connection:
        sock = None

        def __init__(self, *args, **kwargs):
            up.close()

        def request(self, *args, **kwargs):
            pytest.fail("request after shutdown")

        def close(self):
            closed.append(True)

    monkeypatch.setattr(broker, "RegistryConnection", Connection)
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", BLOB_PATH, lambda response: pytest.fail("response after shutdown"))
    assert closed == [True] and not up.connections


def test_native_wire_blob_delegation_keeps_private_metadata_inside_broker(tmp_path, monkeypatch):
    up, unused, delivered, calls, unused_resolved = delivery_setup(monkeypatch)
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}

    def command(argv, timeout, limit):
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=True, env={"PATH": "/usr/bin:/bin", "LANG": "C"})
        assert len(result.stdout) + len(result.stderr) <= limit
        return result.stdout

    with broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60, command, upstream=up) as service:
        port = int(service.environment["HTTPS_PROXY"].rsplit(":", 1)[1])
        connection = socket.create_connection(("127.0.0.1", port), timeout=5)
        connection.sendall(("CONNECT " + REGISTRY + ":443 HTTP/1.1\r\n\r\n").encode())
        assert connection.recv(4096) == b"HTTP/1.1 200 Connection Established\r\n\r\n"
        context = ssl.create_default_context(cafile=service.cert_directory + "/ca.crt")
        context.load_cert_chain(service.cert_directory + "/client.cert", service.cert_directory + "/client.key")
        with context.wrap_socket(connection, server_hostname=REGISTRY) as secured:
            secured.sendall(("GET " + BLOB_PATH + " HTTP/1.1\r\nHost: " + REGISTRY + "\r\n\r\n").encode())
            with secured.makefile("rb") as stream:
                output = stream.read()
        assert output.endswith(b"payload") and b"Content-Type: application/octet-stream" in output
        assert all(secret.encode() not in output for secret in (PASSWORD, TOKEN, SIGNED, "private-delivery-capability"))
        assert delivered.tell() == len(b"payload") and len(calls) == 1
    assert not up.connections and all(not worker.is_alive() for worker in service.workers)
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("length", ["-1", "01", "1, 1", str(broker.MAX_BLOB + 1), SIGNED])
def test_delivery_framing_refuses_without_reflecting_private_header(length):
    with pytest.raises(broker.BrokerError):
        broker.DeliveryResponse(Response(headers={"Content-Length": length}))
    failed = broker.DeliveryResponse(Response(403, headers={"Content-Length": length}))
    assert failed.status == 403 and failed.getheader("Content-Length") is None


def test_delegated_stream_still_obeys_total_budget(monkeypatch):
    up, unused, unused_response, unused_calls, unused_resolved = delivery_setup(monkeypatch)
    monkeypatch.setattr(broker, "MAX_TOTAL", 3)
    value = boundary(up)
    with pytest.raises(broker.BrokerError):
        up.fetch("GET", BLOB_PATH, lambda response: value.response(io.BytesIO(), response, "GET", broker.MAX_BLOB, "blob"))
    assert not up.connections


class StuckWorker(threading.Thread):
    def __init__(self, release):
        super().__init__(target=release.wait, daemon=True)

    def join(self, timeout=None):
        super().join(min(timeout or 0, 0.05))


SETUP_FILES = ("ca.crt", "broker.key", "native-certs/client.key", "native-certs/ca.crt", "registries.conf")


def stuck_broker(tmp_path, release):
    up, unused = upstream([])
    request = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST}
    service = broker.Broker(str(tmp_path), request, json.dumps(MATERIAL), time.monotonic() + 60,
                            lambda *args: pytest.fail("certificate effect"), upstream=up, tls=object())
    (tmp_path / "native-certs").mkdir(mode=0o700)
    for name in SETUP_FILES:
        if not (tmp_path / name).exists():
            (tmp_path / name).write_bytes(b"private setup material")
    worker = StuckWorker(release)
    worker.start()
    service.workers.append(worker)
    return service


def test_stuck_worker_does_not_mask_native_failure_and_cleans_setup_files(tmp_path):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
    release = threading.Event()
    service = stuck_broker(tmp_path, release)
    try:
        with pytest.raises(ceph_native.NativeFailure):
            with service:
                raise ceph_native.NativeFailure("native-command", "synthetic image", b"synthetic pull refusal")
        assert all(not (tmp_path / name).exists() for name in SETUP_FILES)
        assert not (tmp_path / "home").exists() and not (tmp_path / "native-certs").exists()
    finally:
        release.set()


def test_stuck_worker_still_refuses_teardown_without_inflight_error(tmp_path):
    release = threading.Event()
    service = stuck_broker(tmp_path, release)
    try:
        with pytest.raises(broker.BrokerError):
            with service:
                pass
        assert all(not (tmp_path / name).exists() for name in SETUP_FILES)
        assert not (tmp_path / "home").exists() and not (tmp_path / "native-certs").exists()
    finally:
        release.set()


def test_refusal_message_keeps_coordinate_when_username_is_a_coordinate_segment():
    registry, password = "cp.icr.io", "synthetic-entitlement-key"
    up = broker.Upstream(registry, registry + "/cp/ibm-ceph/ceph-9-rhel9", {"url": registry, "username": "cp", "password": password},
                         time.monotonic() + 60, connection=lambda host, **options: pytest.fail("no upstream call"),
                         context=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT))
    value = boundary(up)
    coordinate = registry + "/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-27200"
    output = io.BytesIO()
    value.response(output, Response(404, ("manifest unknown " + coordinate + " for " + password).encode()), "GET", 1024, "manifest")
    assert coordinate.encode() in output.getvalue()
    assert password.encode() not in output.getvalue() and up.basic[6:].encode() not in output.getvalue()
    assert value.failure_cause == "broker observed manifest upstream HTTP 404"


HAPROXY = REGISTRY + "/vendor/haproxy-synthetic@sha256:" + "b" * 64
KEEPALIVED = REGISTRY + "/vendor/keepalived-synthetic@sha256:" + "c" * 64
FROZEN_SET = {"registry": REGISTRY, "image": REPOSITORY + "@sha256:" + DIGEST,
              "auxiliary": [{"name": "haproxy", "image": HAPROXY}, {"name": "keepalived", "image": KEEPALIVED}]}


def manifest_path(reference):
    repository, unused, digest = reference.rpartition("@")
    return "/v2/" + repository.split("/", 1)[1] + "/manifests/" + digest


def test_broker_admits_exactly_the_frozen_set(tmp_path):
    references = [FROZEN_SET["image"], HAPROXY, KEEPALIVED]
    for index, reference in enumerate(references):
        directory = tmp_path / str(index)
        directory.mkdir(mode=0o700)
        image = None if index == 0 else reference
        value = broker.Broker(str(directory), FROZEN_SET, json.dumps(MATERIAL), time.monotonic() + 60,
                              lambda *args: pytest.fail("certificate effect"), upstream=object(), tls=object(), image=image)
        assert value.repository + "@sha256:" + value.digest == reference
        assert value.path("GET", manifest_path(reference)) == broker.MAX_METADATA
        repository = reference.rpartition("@")[0].split("/", 1)[1]
        assert value.path("HEAD", "/v2/" + repository + "/blobs/sha256:" + "f" * 64) == broker.MAX_BLOB
        for other in references:
            if other != reference:
                with pytest.raises(broker.BrokerError):
                    value.path("GET", manifest_path(other))


@pytest.mark.parametrize("reference", [REGISTRY + "/vendor/foreign@sha256:" + "d" * 64, "foreign.example.test/vendor/haproxy-synthetic@sha256:" + "b" * 64,
                                       REGISTRY + "/vendor/haproxy-synthetic@sha256:" + "e" * 64])
def test_broker_refuses_a_repository_outside_the_frozen_set(tmp_path, monkeypatch, reference):
    monkeypatch.setattr(broker, "Upstream", lambda *args, **kwargs: pytest.fail("upstream before admission"))
    monkeypatch.setattr(broker, "certificate", lambda *args, **kwargs: pytest.fail("certificate before admission"))
    tmp_path.chmod(0o700)
    with pytest.raises(broker.BrokerError):
        broker.Broker(str(tmp_path), FROZEN_SET, json.dumps(MATERIAL), time.monotonic() + 60,
                      lambda *args: pytest.fail("command before admission"), image=reference)
    assert list(tmp_path.iterdir()) == []
