"""The KubeVirt API client's bounded retry of transient host answers, over a fake host."""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_api as kapi
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm

CA = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
SERVER = "https://api.host.example.test:6443"
OBJECT = json.dumps({"kind": "Namespace"}).encode()


class Host:
    """A fake API answering each call from a script of (status, body[, headers]) or an exception to raise."""

    def __init__(self, *script):
        self.script, self.calls = list(script), []

    def __call__(self, method, target, headers, payload):
        self.calls.append((method, target))
        step = self.script.pop(0)
        if isinstance(step, Exception):
            raise step
        return step


def client(api, sleeps=None):
    waits = [] if sleeps is None else sleeps
    return kapi.Client({"server": SERVER, "ca": CA, "token": "t"}, transport=api, sleep=waits.append)


def test_a_get_answered_503_then_200_returns_the_body_after_one_retry():
    api, waits = Host((503, b""), (200, OBJECT)), []
    assert client(api, waits).get("/api/v1/namespaces/a") == {"kind": "Namespace"}
    assert len(api.calls) == 2 and waits == [1]


def test_a_429_with_retry_after_waits_that_long_then_succeeds():
    api, waits = Host((429, b"", "3"), (200, OBJECT)), []
    assert client(api, waits).get("/api/v1/namespaces/a") == {"kind": "Namespace"}
    assert len(api.calls) == 2 and waits == [3]


def test_retry_after_is_capped_and_ignored_when_not_a_number():
    api, waits = Host((503, b"", "600"), (503, b"", "Wed, 21 Oct 2026 07:28:00 GMT"), (200, OBJECT)), []
    assert client(api, waits).get("/x") == {"kind": "Namespace"}
    assert len(api.calls) == 3 and waits == [10, 2]


def test_three_503s_raise_answered_after_exactly_three_attempts():
    api, waits = Host((503, b""), (503, b""), (503, b""), (200, OBJECT)), []
    with pytest.raises(kapi.Answered) as error:
        client(api, waits).get("/x")
    assert error.value.status == 503 and len(api.calls) == 3 and waits == [1, 2]


@pytest.mark.parametrize("status", [429, 502, 503, 504])
def test_each_transient_status_is_retried(status):
    api = Host((status, b""), (200, OBJECT))
    assert client(api).get("/x") == {"kind": "Namespace"}
    assert len(api.calls) == 2


@pytest.mark.parametrize("status", [400, 401, 403, 500, 501])
def test_other_statuses_are_answers_not_retried(status):
    api = Host((status, b""), (200, OBJECT))
    with pytest.raises(kapi.Answered):
        client(api).get("/x")
    assert len(api.calls) == 1


def test_a_reset_connection_then_200_succeeds():
    api, waits = Host(kapi.Unanswered(transient=True), (200, OBJECT)), []
    assert client(api, waits).get("/x") == {"kind": "Namespace"}
    assert len(api.calls) == 2 and waits == [1]


def test_a_transient_unanswered_after_three_attempts_is_raised():
    api = Host(*[kapi.Unanswered(transient=True)] * 3)
    with pytest.raises(kapi.Unanswered) as error:
        client(api).get("/x")
    assert error.value.transient and len(api.calls) == 3


def test_a_timeout_is_not_retried():
    api = Host(kapi.Unanswered(), (200, OBJECT))
    with pytest.raises(kapi.Unanswered) as error:
        client(api).get("/x")
    assert not error.value.transient and len(api.calls) == 1


def test_a_post_that_creates_an_object_is_never_retried():
    api = Host((503, b""), (201, OBJECT))
    assert client(api).request("POST", "/x", {"a": 1}) == (503, None)
    assert len(api.calls) == 1
    api = Host(kapi.Unanswered(transient=True), (201, OBJECT))
    with pytest.raises(kapi.Unanswered):
        client(api).request("POST", "/x", {"a": 1})
    assert len(api.calls) == 1


@pytest.mark.parametrize("method", ["DELETE", "PATCH", "PUT"])
def test_no_write_is_retried(method):
    api = Host((503, b""), (200, OBJECT))
    assert client(api).request(method, "/x", {"a": 1}) == (503, None)
    assert len(api.calls) == 1


def test_an_idempotent_post_is_retried():
    api = Host((503, b""), (201, OBJECT))
    assert client(api).request("POST", "/x", {"a": 1}, idempotent=True) == (201, {"kind": "Namespace"})
    assert len(api.calls) == 2


class Response:
    status = 200

    def __init__(self, headers=None):
        self.headers = headers or {}

    def read(self, count):
        return OBJECT

    def getheader(self, name, default=None):
        return self.headers.get(name, default)


def exchange(monkeypatch, fault=None, headers=None):
    """The real HTTPS exchange over a fake connection that raises fault on request."""

    class Connection:
        def __init__(self, *args, **kwargs):
            pass

        def request(self, *args, **kwargs):
            if fault is not None:
                raise fault

        def getresponse(self):
            return Response(headers)

        def close(self):
            pass

    monkeypatch.setattr(kapi.http.client, "HTTPSConnection", Connection)
    subject = kapi.Client({"server": SERVER, "ca": CA, "token": "t"}, sleep=lambda seconds: None)
    monkeypatch.setattr(subject, "_context", lambda directory: None)
    return subject


@pytest.mark.parametrize("fault", [ConnectionRefusedError(), ConnectionResetError(), BrokenPipeError(), ConnectionAbortedError(),
                                   kapi.http.client.RemoteDisconnected("closed")])
def test_a_connection_refused_or_broken_before_an_answer_is_transient(monkeypatch, fault):
    with pytest.raises(kapi.Unanswered) as error:
        exchange(monkeypatch, fault)._exchange("GET", "/x", {}, None)
    assert error.value.transient


@pytest.mark.parametrize("fault", [TimeoutError(), OSError("unreachable"), kapi.http.client.IncompleteRead(b"")])
def test_a_timeout_or_other_failure_is_not_transient(monkeypatch, fault):
    with pytest.raises(kapi.Unanswered) as error:
        exchange(monkeypatch, fault)._exchange("GET", "/x", {}, None)
    assert not error.value.transient


def test_the_exchange_returns_the_retry_after_header(monkeypatch):
    assert exchange(monkeypatch, headers={"Retry-After": "4"})._exchange("GET", "/x", {}, None) == (200, OBJECT, "4")


def host(api, seconds=60):
    clock = {"now": 0.0}
    return arm.Host(client(api), "guests", seconds, clock=lambda: clock["now"], sleep=lambda s: clock.update(now=clock["now"] + s))


def test_wait_continues_through_a_transient_reading_and_returns_when_done():
    readings = iter([kapi.Unanswered(transient=True), kapi.Answered(503), "ready"])
    fired = []

    def read():
        value = next(readings)
        fired.append(value)
        if isinstance(value, Exception):
            raise value
        return value

    assert host(Host()).wait(read, lambda value: value == "ready", 60) == "ready"
    assert len(fired) == 3 and isinstance(fired[0], kapi.Unanswered) and fired[1].status == 503


def test_wait_re_raises_a_transient_error_when_the_bound_ends_after_it():
    calls = []

    def read():
        calls.append(1)
        raise kapi.Answered(503)

    with pytest.raises(kapi.Answered) as error:
        host(Host()).wait(read, lambda value: True, 20)
    assert error.value.status == 503 and len(calls) > 1


def test_wait_re_raises_the_last_transient_unanswered_not_a_stale_value():
    readings = iter(["stale", kapi.Unanswered(transient=True), kapi.Unanswered(transient=True), kapi.Unanswered(transient=True),
                     kapi.Unanswered(transient=True), kapi.Unanswered(transient=True)])

    def read():
        value = next(readings)
        if isinstance(value, Exception):
            raise value
        return value

    with pytest.raises(kapi.Unanswered):
        host(Host()).wait(read, lambda value: False, 20)


@pytest.mark.parametrize("error", [kapi.Unanswered(), kapi.Answered(403), kapi.Answered(500)])
def test_wait_does_not_swallow_a_non_transient_error(error):
    calls = []

    def read():
        calls.append(1)
        raise error

    with pytest.raises(type(error)):
        host(Host()).wait(read, lambda value: True, 60)
    assert len(calls) == 1
