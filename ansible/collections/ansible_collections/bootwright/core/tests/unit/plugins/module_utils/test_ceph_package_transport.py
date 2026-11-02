"""A package artifact whose connection is lost in transport is fetched again from its first byte, a bounded number of times.

Verification refusals, TLS failures and HTTP statuses are never retried, and a
retried artifact is accounted and verified exactly as a first fetch.
"""

from __future__ import annotations

import errno
import hashlib
import http.client
import io
import socket
import ssl
import urllib.error

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_package_sources as sources

BASE = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64"
URL = BASE + "/thrift-0.24.0-1.el9cp.x86_64.rpm"
PAYLOAD = bytes(range(256)) * 9000
DIGEST = hashlib.sha256(PAYLOAD).hexdigest()


class Response:
    def __init__(self, url, data, fault):
        self.status, self.url, self.data, self.fault, self.headers, self.offset, self.closed = 200, url, data, fault, {}, 0, False

    def geturl(self):
        return self.url

    def read(self, amount):
        if self.fault is not None and self.offset >= len(self.data) // 2:
            raise self.fault
        block = self.data[self.offset:self.offset + min(amount, 1 << 16)]
        self.offset += len(block)
        return block

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.closed = True
        return False


class Opener:
    """Each entry of script is an exception the open raises or ("read", exception) raised mid-body; an exhausted script answers cleanly."""

    def __init__(self, *script, data=PAYLOAD):
        self.script, self.data, self.calls, self.responses = list(script), data, 0, []

    def open(self, request, timeout):
        self.calls += 1
        assert timeout > 0 and request.get_header("Accept-encoding") == "identity"
        fault = None
        if self.script:
            step = self.script.pop(0)
            if not isinstance(step, tuple):
                raise step
            fault = step[1]
        self.responses.append(Response(request.full_url, self.data, fault))
        return self.responses[-1]


@pytest.fixture(name="pauses")
def pauses_fixture(monkeypatch):
    slept = []
    monkeypatch.setattr(sources.time, "sleep", slept.append)
    return slept


def acquisition(opener, budget=600):
    value = object.__new__(sources.Acquisition)
    value.source = {"url": "https://192.0.2.10:8443/os/rhel/tree"}
    value.repository_base, value.repository_key = BASE, "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/RPM-GPG-KEY-IBM-CEPH"
    value.repository_opener = value.public = value.local = opener
    value.anchor, value.total, value.expanded = "the host trust store", 0, 0
    value.deadline = sources.time.monotonic() + budget
    return value


def fetch(tmp_path, value, expected=DIGEST, size=len(PAYLOAD)):
    path = tmp_path / "native-artifact.rpm"
    return value.file(URL, str(path), 256 << 20, expected, size), path


@pytest.mark.parametrize("fault", [
    http.client.RemoteDisconnected("Remote end closed connection without response"),
    ConnectionResetError(104, "Connection reset by peer"),
    socket.timeout("timed out"),
    ssl.SSLEOFError(8, "EOF occurred in violation of protocol"),
    urllib.error.URLError(ConnectionRefusedError(111, "Connection refused")),
    urllib.error.URLError(socket.timeout("timed out")),
    urllib.error.URLError(socket.gaierror(socket.EAI_AGAIN, "Temporary failure in name resolution")),
    urllib.error.URLError(OSError(errno.ENETUNREACH, "Network is unreachable")),
    urllib.error.URLError(OSError(errno.EHOSTUNREACH, "No route to host")),
    urllib.error.URLError(OSError(errno.ENETDOWN, "Network is down")),
    OSError(errno.ENETUNREACH, "Network is unreachable"),
], ids=["closed-without-response", "reset", "socket-timeout", "tls-eof", "refused", "connect-timeout",
        "name-not-yet-resolvable", "network-unreachable", "host-unreachable", "network-down", "raw-network-unreachable"])
def test_a_connection_lost_before_the_response_is_fetched_again(tmp_path, pauses, fault):
    opener = Opener(fault)
    value = acquisition(opener)
    digest, path = fetch(tmp_path, value)
    assert digest == DIGEST and path.read_bytes() == PAYLOAD
    assert opener.calls == 2 and pauses == [sources.TRANSPORT_PAUSE] and value.total == len(PAYLOAD)


@pytest.mark.parametrize("fault", [http.client.IncompleteRead(b"partial"), socket.timeout("timed out"), ConnectionResetError(104, "reset")],
                         ids=["incomplete-read", "read-timeout", "read-reset"])
def test_a_body_lost_mid_transfer_restarts_from_its_first_byte(tmp_path, pauses, fault):
    opener = Opener(("read", fault), ("read", fault))
    value = acquisition(opener)
    digest, path = fetch(tmp_path, value)
    assert digest == DIGEST and path.read_bytes() == PAYLOAD and value.total == len(PAYLOAD)
    assert opener.calls == 3 and pauses == [sources.TRANSPORT_PAUSE, 2 * sources.TRANSPORT_PAUSE]
    assert [response.closed for response in opener.responses] == [True, True, True]


def test_an_in_memory_artifact_restarts_without_its_lost_prefix(pauses):
    opener = Opener(("read", http.client.IncompleteRead(b"")))
    value = acquisition(opener)
    assert value.bytes(URL, 4 << 20) == PAYLOAD and value.total == len(PAYLOAD) and opener.calls == 2 and len(pauses) == 1


def test_persistent_transport_loss_raises_the_last_cause_after_the_bounded_attempts(tmp_path, pauses):
    faults = [http.client.RemoteDisconnected("closed %d" % n) for n in range(sources.TRANSPORT_ATTEMPTS + 2)]
    opener = Opener(*faults)
    with pytest.raises(http.client.RemoteDisconnected, match="^closed %d$" % (sources.TRANSPORT_ATTEMPTS - 1)):
        fetch(tmp_path, acquisition(opener))
    assert opener.calls == sources.TRANSPORT_ATTEMPTS == 3 and len(pauses) == sources.TRANSPORT_ATTEMPTS - 1


def test_no_retry_starts_when_the_acquisition_budget_cannot_cover_its_pause(tmp_path, pauses):
    opener = Opener(http.client.RemoteDisconnected("closed"))
    with pytest.raises(http.client.RemoteDisconnected):
        fetch(tmp_path, acquisition(opener, budget=sources.TRANSPORT_PAUSE / 2))
    assert opener.calls == 1 and pauses == []


@pytest.mark.parametrize("fault,raised", [
    (urllib.error.HTTPError(URL, 404, "Not Found", {}, io.BytesIO()), urllib.error.HTTPError),
    (urllib.error.HTTPError(URL, 503, "Service Unavailable", {}, io.BytesIO()), urllib.error.HTTPError),
    (urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed")), sources.RepositoryTrustFailure),
    (ssl.SSLCertVerificationError(1, "certificate verify failed"), sources.RepositoryTrustFailure),
    (ssl.SSLError(1, "wrong version number"), ssl.SSLError),
    (urllib.error.URLError(socket.gaierror(-2, "Name or service not known")), urllib.error.URLError),
    (urllib.error.URLError(OSError(errno.EACCES, "Permission denied")), urllib.error.URLError),
    (PermissionError(errno.EACCES, "Permission denied"), PermissionError),
], ids=["not-found", "unavailable", "wrapped-verification", "verification", "tls-protocol", "name-resolution",
        "wrapped-permission", "permission"])
def test_status_trust_and_resolution_failures_are_never_retried(tmp_path, pauses, fault, raised):
    opener = Opener(fault)
    with pytest.raises(raised):
        fetch(tmp_path, acquisition(opener))
    assert opener.calls == 1 and pauses == []


@pytest.mark.parametrize("expected,size", [("0" * 64, len(PAYLOAD)), (DIGEST, len(PAYLOAD) - 1)], ids=["digest", "size"])
def test_a_content_refusal_after_a_retry_is_not_retried_again(tmp_path, pauses, expected, size):
    opener = Opener(http.client.RemoteDisconnected("closed"))
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        fetch(tmp_path, acquisition(opener), expected, size)
    assert opener.calls == 2 and len(pauses) == 1


@pytest.mark.parametrize("fault", [urllib.error.URLError(ssl.SSLCertVerificationError(1, "certificate verify failed")),
                                   ssl.SSLCertVerificationError(1, "certificate verify failed")], ids=["wrapped", "raw"])
def test_an_unverified_artifact_server_connection_is_not_retried(tmp_path, pauses, fault):
    opener = Opener(fault)
    value = acquisition(opener)
    with pytest.raises(type(fault)):
        value.file("https://192.0.2.10:8443/os/rhel/tree/.treeinfo", str(tmp_path / "treeinfo"), 1 << 20)
    assert opener.calls == 1 and pauses == []
