"""Frozen routes use CONNECT without weakening origin TLS or public delivery pins."""

from __future__ import annotations

import contextlib
import http.client
import json
import pathlib
import select
import socket
import ssl
import subprocess
import threading

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_pull_broker as broker
from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_discovery as discovery
from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_proxy as proxy, transport_faults

# urllib3 belongs to the execution closure, which the plain lane runs; the
# ansible-test units environment does not carry it.
urllib3 = pytest.importorskip("urllib3")

COLLECTION = pathlib.Path(__file__).resolve().parents[4]
REQUEST = json.loads((COLLECTION / "tests" / "fixtures" / "ceph-image-call.golden").read_text())["request"]
HOST = "registry.example.test"
PUBLIC_IP = "93.184.216.34"
AUTH = "Bearer fixture-private-bearer-value"


def addresses():
    return [(socket.AF_INET, socket.SOCK_STREAM, 6, "", (PUBLIC_IP, 443))]


def test_proxy_manager_keeps_origin_and_outer_trust_separate_and_pins_delivery(monkeypatch):
    calls, seen = [], []
    origin, outer = object(), object()

    @contextlib.contextmanager
    def trust():
        seen.append("opened")
        yield outer, lambda: None
        seen.append("proved")

    class Manager:
        def __init__(self, url, **options):
            calls.append((url, options))

        def request(self, method, url, **options):
            calls.append((method, url, options))

        def clear(self):
            seen.append("closed")

    monkeypatch.setattr(urllib3, "ProxyManager", Manager)
    connection = proxy.ProxyConnection("https://proxy.example.test:8443", HOST, origin, 10, trust, addresses())
    try:
        connection.request("GET", "/private/config?signature=private", {"Accept": "application/json"})
    finally:
        connection.close()
    _url, settings = calls[0]
    assert settings["ssl_context"] is origin and settings["proxy_ssl_context"] is outer
    assert settings["server_hostname"] == HOST and settings["cert_reqs"] == ssl.CERT_REQUIRED
    assert settings["use_forwarding_for_https"] is False and settings["proxy_headers"] == {} and settings["retries"] is False
    method, url, settings = calls[1]
    assert method == "GET" and url == "https://" + PUBLIC_IP + "/private/config?signature=private"
    assert settings["headers"] == {"Accept": "application/json", "Host": HOST}
    assert all(settings[name] is False for name in ("redirect", "retries", "preload_content", "decode_content"))
    assert seen == ["opened", "closed", "proved"]


@pytest.mark.parametrize("rule,expected", [(".icr.io", False), ("cp.icr.io:443", False), ("*.icr.io", False),
                                           ("10.0.0.0/8", True), (".internal.test", True), ("*", False)])
def test_registry_route_uses_only_frozen_bypass_rules(monkeypatch, rule, expected):
    monkeypatch.setenv("HTTPS_PROXY", "http://ambient.invalid:9999")
    monkeypatch.setenv("NO_PROXY", "*")
    seen = []
    monkeypatch.setattr(proxy, "ProxyConnection", lambda *args: seen.append("proxy"))
    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", lambda *args, **kwargs: seen.append("direct"))
    client = discovery.Registry(object(), REQUEST)
    client.egress = {"httpProxy": "", "httpsProxy": "http://proxy.example.test:3128", "noProxy": [rule]}
    client.connection(REQUEST["registry"], 10)
    assert seen == ["proxy" if expected else "direct"]


def certificate(directory, host):
    directory.mkdir()

    def command(argv, timeout, maximum):
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=True, env={"PATH": "/usr/bin:/bin", "LANG": "C"})
        assert len(result.stdout) + len(result.stderr) <= maximum
        return result.stdout

    context = broker.certificate(str(directory), str(directory / "native"), host, command)
    context.verify_mode = ssl.CERT_NONE
    return context, str(directory / "ca.crt")


def headers(stream):
    value = b""
    while not value.endswith(b"\r\n\r\n"):
        part = stream.recv(1)
        if not part:
            return value
        value += part
        assert len(value) < 8192
    return value


@pytest.mark.parametrize("secure_proxy,wrong_hostname,target_port,outer_failure", [
    (secure, wrong, port, None) for secure in (False, True) for wrong in (False, True) for port in (443, 5443)
] + [(True, False, 443, "ca"), (True, False, 443, "hostname")])
def test_real_connect_keeps_bearer_inside_verified_origin_tls(tmp_path, secure_proxy, wrong_hostname, target_port, outer_failure):
    origin_tls, origin_ca = certificate(tmp_path / "origin", HOST)
    outer_tls, outer_ca = certificate(tmp_path / "outer", "127.0.0.1") if secure_proxy else (None, None)
    stopping, errors, observed = threading.Event(), [], {"connect": b"", "origin": b"", "sni": [], "tunnel": b""}
    origin_tls.set_servername_callback(lambda _socket, name, _context: observed["sni"].append(name))
    origin_listener, proxy_listener = socket.socket(), socket.socket()
    for listener in (origin_listener, proxy_listener):
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(0.2)

    def accept(listener):
        while not stopping.is_set():
            try:
                connection, _peer = listener.accept()
                connection.settimeout(5)
                return connection
            except socket.timeout:
                continue
        raise TimeoutError()

    def origin():
        try:
            with accept(origin_listener) as raw:
                with origin_tls.wrap_socket(raw, server_side=True) as stream:
                    observed["origin"] = headers(stream)
                    stream.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
        except (OSError, TimeoutError) as error:
            errors.append(type(error).__name__)

    def relay():
        try:
            with accept(proxy_listener) as raw:
                with contextlib.ExitStack() as stack:
                    stream = stack.enter_context(outer_tls.wrap_socket(raw, server_side=True)) if secure_proxy else raw
                    observed["connect"] = headers(stream)
                    remote = stack.enter_context(socket.create_connection(origin_listener.getsockname(), timeout=5))
                    stream.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                    while not stopping.is_set():
                        ready = select.select([stream, remote], [], [], 0.2)[0]
                        for source in ready:
                            data = source.recv(65536)
                            if not data:
                                return
                            if source is stream:
                                observed["tunnel"] += data
                            (remote if source is stream else stream).sendall(data)
        except OSError as error:
            errors.append(type(error).__name__)

    threads = [threading.Thread(target=origin), threading.Thread(target=relay)]
    for thread in threads:
        thread.start()
    context = ssl.create_default_context(cafile=origin_ca)

    @contextlib.contextmanager
    def outer_trust():
        yield ssl.create_default_context(cafile=origin_ca if outer_failure == "ca" else outer_ca), lambda: None

    proxy_host = "localhost" if outer_failure == "hostname" else "127.0.0.1"
    endpoint = ("https" if secure_proxy else "http") + "://" + proxy_host + ":" + str(proxy_listener.getsockname()[1])
    hostname = "wrong.example.test" if wrong_hostname else HOST
    authority = hostname if target_port == 443 else hostname + ":" + str(target_port)
    connection = proxy.ProxyConnection(endpoint, authority, context, 5, outer_trust, addresses())
    try:
        if outer_failure:
            with pytest.raises(proxy.ProxyFailure, match="^proxy-tls$"):
                connection.request("GET", "/v2/", {"Authorization": AUTH})
        elif wrong_hostname:
            with pytest.raises(ssl.SSLCertVerificationError):
                connection.request("GET", "/v2/", {"Authorization": AUTH})
        else:
            connection.request("GET", "/v2/", {"Authorization": AUTH})
            assert connection.getresponse().status == 200 and connection.read(3) == b"{}"
    finally:
        connection.close()
        stopping.set()
        for thread in threads:
            thread.join(6)
        for listener in (origin_listener, proxy_listener):
            listener.close()
    assert all(not thread.is_alive() for thread in threads)
    if outer_failure:
        assert observed["connect"] == observed["origin"] == b"" and observed["sni"] == []
        return
    assert observed["connect"].startswith(("CONNECT " + PUBLIC_IP + ":" + str(target_port) + " ").encode())
    assert b"Authorization" not in observed["connect"] and AUTH.encode() not in observed["tunnel"]
    assert observed["sni"] == [hostname] and context.check_hostname is True
    assert (AUTH.encode() in observed["origin"]) is (not wrong_hostname)
    if not wrong_hostname:
        assert ("Host: " + authority + "\r\n").encode() in observed["origin"]


@pytest.mark.parametrize("error,retry", [
    (urllib3.exceptions.ConnectTimeoutError("private"), True),
    (urllib3.exceptions.ReadTimeoutError(None, "/private", "private"), True),
    (urllib3.exceptions.ProxyError("private", ConnectionRefusedError("private")), True),
    (urllib3.exceptions.ProxyError("private", OSError("HTTP 407 private")), False),
    (urllib3.exceptions.ProtocolError("private", http.client.IncompleteRead(b"private", 10)), True),
    (urllib3.exceptions.ProtocolError("private"), False),
    (urllib3.exceptions.SSLError(ssl.SSLCertVerificationError("private")), False),
    (urllib3.exceptions.SSLError(ssl.SSLError("private")), False),
    (urllib3.exceptions.NameResolutionError(HOST, None, socket.gaierror(socket.EAI_NONAME, "private")), False),
    (urllib3.exceptions.NameResolutionError(HOST, None, socket.gaierror(socket.EAI_AGAIN, "private")), True),
    (urllib3.exceptions.NewConnectionError(None, "private"), False),
])
def test_proxy_errors_preserve_the_existing_retry_relation(error, retry):
    with pytest.raises((OSError, http.client.HTTPException)) as caught:
        proxy.translated(error, urllib3)
    assert transport_faults.lost(caught.value) is retry
