"""The controller contract is exercised here, so it is tested without a BMC."""

from __future__ import annotations

import http.client
import io
import ssl
import urllib.error
import urllib.request

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

ENDPOINT = "https://bmc.test/redfish/v1/Systems/1"


class Reply:
    """A 2xx answer the way urllib hands one back, or one whose read fails."""

    def __init__(self, raw=b"{}", headers=None, failure=None):
        self.status, self.stream, self.failure = 200, io.BytesIO(raw), failure
        self.headers = http.client.HTTPMessage()
        for name, value in (headers or {}).items():
            self.headers[name] = value

    def read(self, amount=-1):
        if self.failure is not None:
            raise self.failure
        return self.stream.read(amount)

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class Recorder:
    """An opener that records every request it is handed and answers one way."""

    def __init__(self, answer=None):
        self.requests, self.answer = [], answer if answer is not None else Reply()

    def opener(self, verify=True):
        return self

    def open(self, request, timeout=None):
        self.requests.append(request)
        if isinstance(self.answer, BaseException):
            raise self.answer
        return self.answer


@pytest.fixture(name="built")
def built_fixture(monkeypatch):
    """Every Request the client constructs, whether or not it is ever sent."""
    built = []

    class Recording(urllib.request.Request):
        def __init__(self, url, *args, **kwargs):
            built.append(url)
            super().__init__(url, *args, **kwargs)

    monkeypatch.setattr(urllib.request, "Request", Recording)
    return built


def client(monkeypatch, recorder, endpoint=ENDPOINT):
    monkeypatch.setattr(redfish_control, "_opener", recorder.opener)
    return redfish_control.Client(endpoint, "operator", "p4ssw0rd")


# The credential goes to the one endpoint the frozen request names. A reference
# the controller returns that names anything else is refused before a request,
# and so before an Authorization header, exists.
@pytest.mark.parametrize("reference", [
    "https://elsewhere.test/redfish/v1/Systems/1",
    "https://bmc.test:8443/redfish/v1/Systems/1",
    "http://bmc.test/redfish/v1/Systems/1",
    "//elsewhere.test/redfish/v1/Systems/1",
    "https://intruder:secret@bmc.test/redfish/v1/Systems/1",
    "https://@bmc.test/redfish/v1/Systems/1",
    "https://bmc.test:99999/redfish/v1/Systems/1",
    "https://bmc.test:port/redfish/v1/Systems/1",
    "https://[fd00::1/redfish/v1/Systems/1",
    "ftp://bmc.test/redfish/v1/Systems/1",
    "https:/redfish/v1/Systems/1",
])
def test_a_reference_to_another_authority_is_refused_and_never_sent_the_credential(monkeypatch, built, reference):
    recorder = Recorder()
    with pytest.raises(redfish_control.ControllerError) as failure:
        client(monkeypatch, recorder).fetch(reference)
    assert not built and not recorder.requests
    assert "intruder" not in str(failure.value) and "secret" not in str(failure.value)


@pytest.mark.parametrize("endpoint, reference, requested", [
    (ENDPOINT, "https://BMC.test/redfish/v1/Managers/1", "https://BMC.test/redfish/v1/Managers/1"),
    (ENDPOINT, "https://bmc.test:443/redfish/v1/Managers/1", "https://bmc.test:443/redfish/v1/Managers/1"),
    (ENDPOINT, "//bmc.test/redfish/v1/Managers/1", "https://bmc.test/redfish/v1/Managers/1"),
    (ENDPOINT, "/redfish/v1/Managers/1", "https://bmc.test/redfish/v1/Managers/1"),
    ("http://bmc.test:80/redfish/v1/Systems/1", "http://bmc.test/redfish/v1/Managers/1",
     "http://bmc.test/redfish/v1/Managers/1"),
    ("https://[fd00::1]:8443/redfish/v1/Systems/1", "https://[FD00::1]:8443/redfish/v1/Managers/1",
     "https://[FD00::1]:8443/redfish/v1/Managers/1"),
    ("https://[fd00::1]:8443/redfish/v1/Systems/1", "/redfish/v1/Managers/1",
     "https://[fd00::1]:8443/redfish/v1/Managers/1"),
])
def test_a_reference_to_the_endpoint_itself_is_followed(monkeypatch, built, endpoint, reference, requested):
    recorder = Recorder()
    assert client(monkeypatch, recorder, endpoint).fetch(reference)[0] == 200
    assert built == [requested] and [request.full_url for request in recorder.requests] == [requested]
    assert recorder.requests[0].get_header("Authorization").startswith("Basic ")


# A transport that breaks mid-answer is no answer, reported as status 0 rather
# than escaping as a traceback past the module's failure handling.
@pytest.mark.parametrize("failure", [
    http.client.IncompleteRead(b"{", 10),
    http.client.LineTooLong("header line"),
    http.client.RemoteDisconnected("closed"),
    TimeoutError("timed out"),
    urllib.error.URLError("refused"),
])
def test_a_broken_transport_is_a_failure_not_a_traceback(monkeypatch, failure):
    assert client(monkeypatch, Recorder(failure)).fetch() == (0, None, {})
    assert client(monkeypatch, Recorder(Reply(failure=failure))).fetch() == (0, None, {})


def test_response_headers_are_read_without_case(monkeypatch):
    reply = Reply(headers={"ETag": 'W/"1"', "LOCATION": "/redfish/v1/TaskService/Tasks/1"})
    status, body, headers = client(monkeypatch, Recorder(reply)).fetch()
    assert (status, body) == (200, {}) and headers["etag"] == 'W/"1"'
    assert headers["location"] == "/redfish/v1/TaskService/Tasks/1"

    message = http.client.HTTPMessage()
    message["Location"] = "/redfish/v1/TaskService/Tasks/2"
    refused = urllib.error.HTTPError(ENDPOINT, 500, "failed", message, io.BytesIO(b""))
    assert client(monkeypatch, Recorder(refused)).fetch()[2] == {"location": "/redfish/v1/TaskService/Tasks/2"}


# A body is one JSON object or nothing: a list, a scalar, an empty read or a
# body past the bound never reads as an empty resource. The two bounded cases
# are valid JSON, so only the length decides them.
@pytest.mark.parametrize("method, raw, body", [
    ("GET", b'{"PowerState": "On"}', {"PowerState": "On"}),
    ("GET", b"", None),
    ("GET", b"[]", None),
    ("GET", b'"On"', None),
    ("GET", b"{", None),
    ("GET", b"[" * 100000, None),
    ("POST", b"", {}),
    ("PATCH", b"", {}),
    ("GET", b" " * (redfish_control.MAX_BODY - 2) + b"{}", {}),
    ("GET", b" " * (redfish_control.MAX_BODY - 1) + b"{}", None),
])
def test_a_body_is_one_object_or_nothing(monkeypatch, method, raw, body):
    assert client(monkeypatch, Recorder(Reply(raw))).fetch(method=method)[1] == body


def test_a_redirect_is_refused_rather_than_followed():
    handler = redfish_control.NoRedirect()
    with pytest.raises(Exception):
        handler.redirect_request(_Request(), None, 302, "moved", {}, "http://elsewhere/")


class _Request:
    full_url = "http://c/1"


# A management controller is addressed by the frozen request, so the client
# reaches it directly. urllib consults the ambient proxy environment by default
# and its bypass list does not cover a concrete controller address, so a
# proxy's own refusal would arrive looking like the controller's.
def test_the_client_never_consults_an_ambient_proxy(monkeypatch):
    monkeypatch.setenv("https_proxy", "http://proxy.example.test:3128")
    monkeypatch.setenv("http_proxy", "http://proxy.example.test:3128")

    def proxied(opener):
        return [h for h in opener.handlers
                if isinstance(h, urllib.request.ProxyHandler) and h.proxies]

    assert proxied(urllib.request.build_opener()), "the default opener is expected to proxy"
    assert not proxied(redfish_control._opener())


# Verification is on unless the declaration opts out, and the opt-out reaches
# exactly one opener rather than becoming a process-wide default.
def test_verification_is_opted_out_of_per_call():
    assert redfish_control._opener(True) is not redfish_control._opener(False)
    contexts = [h for h in redfish_control._opener(False).handlers
                if isinstance(h, urllib.request.HTTPSHandler)]
    assert contexts
    assert ssl.create_default_context().verify_mode == ssl.CERT_REQUIRED
