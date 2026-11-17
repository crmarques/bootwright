"""An explicit package CONNECT route preserves origin TLS and ignores ambient routes."""

from __future__ import annotations

import socket
import ssl
import subprocess
import threading
import urllib.error
import urllib.request

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_package_sources as sources


@pytest.mark.parametrize("endpoint", [None, True, "https://proxy.example.test:8443", "http://user:secret@proxy.example.test:3128",
                                      "http://proxy.example.test/path", "http://proxy.example.test?query", "http://proxy.example.test#fragment",
                                      "http://proxy.example.test:0", "http://proxy.example.test:99999", "http://proxy.example.test\n",
                                      "http://proxy.example.test%2f", "http://proxy.example.test\\path"])
def test_invalid_frozen_proxy_refuses_without_connecting(endpoint, monkeypatch):
    monkeypatch.setattr(socket, "create_connection", lambda *args, **kwargs: pytest.fail("invalid route connected"))
    with pytest.raises(ceph_native.Refusal):
        ceph_native.package_proxy(endpoint)


def read_headers(stream):
    data = b""
    while not data.endswith(b"\r\n\r\n"):
        block = stream.recv(1)
        assert block and len(data) < 8192
        data += block
    return data


@pytest.fixture(name="certificate")
def certificate_fixture(tmp_path):
    key, cert = tmp_path / "server.key", tmp_path / "server.pem"
    subprocess.run(["/usr/bin/openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                    "-subj", "/CN=packages.example.test", "-addext", "subjectAltName=DNS:packages.example.test",
                    "-keyout", str(key), "-out", str(cert)],
                   check=True, timeout=15, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    return key, cert


@pytest.mark.parametrize("failure", [None, "hostname", "trust", "denied"])
def test_connect_authenticates_the_origin_without_environment_bypass(certificate, monkeypatch, failure):
    key, cert = certificate
    observed, errors = {}, []
    server_tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    server_tls.load_cert_chain(str(cert), str(key))
    server_tls.set_servername_callback(lambda _socket, name, _context: observed.update(sni=name))
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(1)
    listener.settimeout(6)

    def serve():
        try:
            with listener.accept()[0] as stream:
                stream.settimeout(5)
                observed["connect"] = read_headers(stream)
                if failure == "denied":
                    stream.sendall(b"HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
                    return
                stream.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
                with server_tls.wrap_socket(stream, server_side=True) as secure:
                    observed["request"] = read_headers(secure)
                    secure.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
        except OSError as error:
            errors.append(type(error).__name__)

    thread = threading.Thread(target=serve)
    thread.start()
    monkeypatch.setenv("HTTPS_PROXY", "http://ambient.invalid:1234")
    monkeypatch.setenv("NO_PROXY", "*")
    context = ssl.create_default_context() if failure == "trust" else ssl.create_default_context(cafile=str(cert))
    endpoint = "http://127.0.0.1:" + str(listener.getsockname()[1])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), sources.NoRedirect(), sources.RepositoryHTTPS(endpoint, context))
    host = "wrong.example.test" if failure == "hostname" else "packages.example.test"
    try:
        if failure:
            with pytest.raises(urllib.error.URLError) as caught:
                opener.open("https://" + host + ":8443/repodata/repomd.xml", timeout=5)
            if failure != "denied":
                assert isinstance(caught.value.reason, ssl.SSLCertVerificationError)
        else:
            with opener.open("https://" + host + ":8443/repodata/repomd.xml", timeout=5) as response:
                assert response.status == 200 and response.read(3) == b"ok"
    finally:
        thread.join(7)
        listener.close()
    assert not thread.is_alive()
    assert observed["connect"].startswith(("CONNECT " + host + ":8443 HTTP/").encode())
    assert b"Authorization" not in observed["connect"]
    assert context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED
    if failure:
        assert "request" not in observed
    else:
        assert not errors and b"GET /repodata/repomd.xml HTTP/" in observed["request"]
        assert b"Host: packages.example.test:8443" in observed["request"]
        assert observed["sni"] == host


def test_proxy_failure_has_no_direct_fallback(monkeypatch):
    connected = []

    def refused(address, *args, **kwargs):
        connected.append(address)
        raise ConnectionRefusedError("fixed proxy refusal")

    monkeypatch.setattr(socket, "create_connection", refused)
    context = ssl.create_default_context()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), sources.RepositoryHTTPS("http://proxy.example.test:3128", context))
    with pytest.raises(urllib.error.URLError):
        opener.open("https://packages.example.test/metadata", timeout=1)
    assert connected == [("proxy.example.test", 3128)]
