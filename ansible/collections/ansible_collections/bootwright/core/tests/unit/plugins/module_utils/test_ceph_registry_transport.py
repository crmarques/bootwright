"""A registry request whose transport was lost is made again on a new connection to the same host, a bounded number of times.

HTTP statuses, TLS verification, protocol and bound failures, a name that does
not exist and every refusal fail at once, and an exhausted retry raises the
last refusal unchanged, so the resolution's projection is the one a single
attempt gives.
"""

from __future__ import annotations

import errno
import http.client
import json
import pathlib
import socket
import ssl
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_package_sources
from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_discovery as discovery

COLLECTION = pathlib.Path(__file__).resolve().parents[4]
REQUEST = json.loads((COLLECTION / "tests" / "fixtures" / "ceph-image-call.golden").read_text())["request"]
REQUEST["version"] = "ceph-image-v1"
REGISTRY = REQUEST["registry"]
NAMESPACE = REQUEST["base"].split("/", 1)[1]
MANIFEST = "/v2/" + NAMESPACE + "/manifests/" + REQUEST["tag"]
BODY = b'{"schemaVersion": 2}'

LOSSES = {
    "remote-disconnected": lambda: ("request", http.client.RemoteDisconnected("Remote end closed connection without response")),
    "reset": lambda: ("request", ConnectionResetError(errno.ECONNRESET, "Connection reset by peer")),
    "refused": lambda: ("request", ConnectionRefusedError(errno.ECONNREFUSED, "Connection refused")),
    "aborted": lambda: ("response", ConnectionAbortedError(errno.ECONNABORTED, "Software caused connection abort")),
    "socket-timeout": lambda: ("response", socket.timeout("timed out")),
    "timeout": lambda: ("request", TimeoutError("timed out")),
    "tls-eof": lambda: ("request", ssl.SSLEOFError(8, "EOF occurred in violation of protocol")),
    "incomplete-read": lambda: ("read", http.client.IncompleteRead(b"{", 10)),
    "truncated-body": lambda: ("short", None),
    "name-not-yet-resolvable": lambda: ("request", socket.gaierror(socket.EAI_AGAIN, "Temporary failure in name resolution")),
    "network-unreachable": lambda: ("request", OSError(errno.ENETUNREACH, "Network is unreachable")),
    "host-unreachable": lambda: ("request", OSError(errno.EHOSTUNREACH, "No route to host")),
    "network-down": lambda: ("request", OSError(errno.ENETDOWN, "Network is down")),
}

FAILURES = {
    "certificate": lambda: ("request", ssl.SSLCertVerificationError(1, "certificate verify failed")),
    "tls-protocol": lambda: ("request", ssl.SSLError(1, "wrong version number")),
    "no-such-name": lambda: ("request", socket.gaierror(socket.EAI_NONAME, "Name or service not known")),
    "bad-status-line": lambda: ("response", http.client.BadStatusLine("garbage")),
    "permission": lambda: ("request", PermissionError(errno.EACCES, "Permission denied")),
    "encoded-body": lambda: ("encoded", None),
    "oversized-body": lambda: ("oversized", None),
    "budget-alarm": lambda: ("request", discovery.DiscoveryRefusal("timeout")),
}


class Connection:
    """Each connection takes the next step of script: a (stage, exception) to raise, or None to answer cleanly."""

    def __init__(self, script, opened):
        self.script, self.opened = script, opened

    def __call__(self, host, **options):
        self.opened.append(SimpleNamespace(host=host, options=options, sent=None, closed=False))
        record, step = self.opened[-1], self.script.pop(0) if self.script else None
        stage, fault = step if step else (None, None)

        def request(method, path, headers):
            record.sent = (method, path, dict(headers))
            if stage == "request":
                raise fault

        def getresponse():
            if stage == "response":
                raise fault
            body = BODY[:5] if stage == "short" else b"x" * (discovery.MAX_BODY + 1) if stage == "oversized" else BODY
            rows = [("Content-Length", str(len(BODY)))] if stage != "oversized" else []
            if stage == "encoded":
                rows.append(("Content-Encoding", "gzip"))

            def read(limit):
                if stage == "read":
                    raise fault
                return body[:limit]

            return SimpleNamespace(status=200, getheaders=lambda: rows, read=read)

        def close():
            record.closed = True

        return SimpleNamespace(request=request, getresponse=getresponse, close=close)


@pytest.fixture(name="pauses")
def pauses_fixture(monkeypatch):
    slept = []
    monkeypatch.setattr(discovery.time, "sleep", slept.append)
    return slept


@pytest.fixture(name="raised")
def raised_fixture(monkeypatch):
    """Every refusal a lost or failed transport produced, in order."""
    made, original = [], discovery.lost

    def recorded(cause):
        made.append(original(cause))
        return made[-1]

    monkeypatch.setattr(discovery, "lost", recorded)
    return made


def registry(monkeypatch, *script):
    opened = []
    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", Connection(list(script), opened))
    return discovery.Registry(object(), REQUEST), opened


def test_the_attempts_and_pause_are_the_package_precedent_bounded_by_the_budget():
    assert ceph_package_sources.TRANSPORT_ATTEMPTS == 3
    assert discovery.TRANSPORT_PAUSE * sum(range(1, ceph_package_sources.TRANSPORT_ATTEMPTS)) + 30 * 3 < discovery.TOTAL_SECONDS


@pytest.mark.parametrize("loss", sorted(LOSSES))
def test_a_lost_request_is_made_again_on_a_new_connection_to_the_same_host(monkeypatch, pauses, loss):
    client, opened = registry(monkeypatch, LOSSES[loss](), LOSSES[loss]())
    status, unused_headers, raw = client.get(MANIFEST, "Bearer synthetic-token")
    assert (status, raw) == (200, BODY)
    assert len(opened) == 3 and pauses == [discovery.TRANSPORT_PAUSE, 2 * discovery.TRANSPORT_PAUSE]
    assert all(row.host == REGISTRY and row.closed for row in opened)
    assert len({json.dumps(row.sent, sort_keys=True) for row in opened if row.sent}) == 1
    assert all(row.sent[2]["Authorization"] == "Bearer synthetic-token" for row in opened if row.sent)


@pytest.mark.parametrize("loss", sorted(LOSSES))
def test_persistent_loss_raises_the_last_refusal_unchanged_after_the_bounded_attempts(monkeypatch, pauses, raised, loss):
    client, opened = registry(monkeypatch, *[LOSSES[loss]() for _attempt in range(ceph_package_sources.TRANSPORT_ATTEMPTS + 2)])
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$") as failure:
        client.get(MANIFEST)
    assert len(opened) == ceph_package_sources.TRANSPORT_ATTEMPTS and len(pauses) == ceph_package_sources.TRANSPORT_ATTEMPTS - 1
    assert failure.value is raised[-1] and len(raised) == ceph_package_sources.TRANSPORT_ATTEMPTS
    assert discovery.refusal(failure.value) == ("discovery", None)


@pytest.mark.parametrize("name", sorted(FAILURES))
def test_a_failure_that_is_not_transport_loss_fails_at_once(monkeypatch, pauses, name):
    client, opened = registry(monkeypatch, FAILURES[name](), None)
    with pytest.raises(discovery.DiscoveryRefusal) as failure:
        client.get(MANIFEST)
    assert len(opened) == 1 and pauses == [] and opened[0].closed
    assert str(failure.value) == ("timeout" if name == "budget-alarm" else "discovery")
    assert bool(getattr(failure.value, "native_error", None)) is (name == "certificate")


def test_an_http_status_is_returned_without_another_request(monkeypatch, pauses):
    opened = []

    def connection(host, **options):
        opened.append(host)
        return SimpleNamespace(request=lambda *args, **kwargs: None, close=lambda: None,
                               getresponse=lambda: SimpleNamespace(status=503, getheaders=lambda: [], read=lambda limit: b""))

    monkeypatch.setattr(discovery.http.client, "HTTPSConnection", connection)
    assert discovery.Registry(object(), REQUEST).get(MANIFEST)[0] == 503
    assert opened == [REGISTRY] and pauses == []


def test_a_pause_the_registry_deadline_cannot_hold_is_not_taken(monkeypatch, pauses):
    client, opened = registry(monkeypatch, LOSSES["reset"](), None)
    client.deadline = client.now() + discovery.TRANSPORT_PAUSE - 1
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        client.get(MANIFEST)
    assert len(opened) == 1 and pauses == []


def test_a_pause_the_whole_call_alarm_cannot_hold_is_not_taken(monkeypatch, pauses):
    client, opened = registry(monkeypatch, LOSSES["reset"](), None)
    monkeypatch.setattr(discovery.signal, "getitimer", lambda which: (discovery.TRANSPORT_PAUSE - 1.0, 0.0))
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        client.get(MANIFEST)
    assert len(opened) == 1 and pauses == []


def test_a_lost_token_exchange_is_made_again_only_to_the_registry_with_its_own_credential(monkeypatch, pauses):
    client, opened = registry(monkeypatch, LOSSES["remote-disconnected"](), None)
    client.token_path = "/oauth/token?service=registry&scope=repository%3A" + NAMESPACE.replace("/", "%2F") + "%3Apull"
    assert client.get(client.token_path, "Basic c3ludGhldGljOnNlY3JldA==", discovery.MAX_TOKEN * 2)[0] == 200
    assert [row.host for row in opened] == [REGISTRY, REGISTRY] and pauses == [discovery.TRANSPORT_PAUSE]
    assert [row.sent[2]["Authorization"] for row in opened] == ["Basic c3ludGhldGljOnNlY3JldA=="] * 2


def delivery(monkeypatch, resolutions, steps):
    """A real Registry's delivery hop whose resolver and socket are synthetic."""
    client = discovery.Registry(SimpleNamespace(check_hostname=True), REQUEST)
    resolved, opened, script = [], [], list(steps)
    row = [socket.AF_INET, socket.SOCK_STREAM, socket.IPPROTO_TCP, ["8.8.8.8", 443]]

    def resolver(host):
        resolved.append(host)
        step = resolutions.pop(0) if resolutions else None
        if step is not None:
            raise step
        return json.dumps([row]).encode()

    def connection(host, **options):
        assert options["port"] == 443 and 0 < options["timeout"] <= 30
        return Connection(script, opened)(host, **options)

    client.resolver, client.delivery = resolver, connection
    return client, resolved, opened


LOCATION = {"location": "https://delivery.example.com/blob?signature=synthetic"}


def test_a_lost_delivery_hop_is_resolved_and_made_again_without_credentials(monkeypatch, pauses):
    client, resolved, opened = delivery(monkeypatch, [socket.gaierror(socket.EAI_AGAIN, "Temporary failure in name resolution")],
                                        [LOSSES["reset"]()])
    assert client.deliver(LOCATION) == (200, BODY)
    assert resolved == ["delivery.example.com"] * 3 and [row.host for row in opened] == ["delivery.example.com"] * 2
    assert pauses == [discovery.TRANSPORT_PAUSE, 2 * discovery.TRANSPORT_PAUSE]
    assert all("Authorization" not in row.sent[2] for row in opened)


@pytest.mark.parametrize("fault", [socket.gaierror(socket.EAI_NONAME, "Name or service not known"), ValueError("private answer")],
                         ids=["no-such-name", "refused-answer"])
def test_a_delivery_name_that_does_not_resolve_fails_at_once(monkeypatch, pauses, fault):
    client, resolved, opened = delivery(monkeypatch, [fault], [])
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        client.deliver(LOCATION)
    assert resolved == ["delivery.example.com"] and opened == [] and pauses == []


def test_a_delivery_location_refusal_resolves_nothing(monkeypatch, pauses):
    client, resolved, opened = delivery(monkeypatch, [], [])
    with pytest.raises(discovery.DiscoveryRefusal, match="^discovery$"):
        client.deliver({"location": "http://delivery.example.com/blob"})
    assert resolved == [] and opened == [] and pauses == []
