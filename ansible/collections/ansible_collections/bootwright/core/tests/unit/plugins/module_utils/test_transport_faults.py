"""One classification of a connection lost in transport, shared by every client that retries one."""

from __future__ import annotations

import errno
import http.client
import io
import socket
import ssl
import urllib.error

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_package_sources, ceph_registry_discovery, transport_faults

LOST = {
    "closed-without-response": http.client.RemoteDisconnected("Remote end closed connection without response"),
    "reset": ConnectionResetError(errno.ECONNRESET, "Connection reset by peer"),
    "refused": ConnectionRefusedError(errno.ECONNREFUSED, "Connection refused"),
    "aborted": ConnectionAbortedError(errno.ECONNABORTED, "Software caused connection abort"),
    "broken-pipe": BrokenPipeError(errno.EPIPE, "Broken pipe"),
    "timeout": TimeoutError("timed out"),
    "socket-timeout": socket.timeout("timed out"),
    "tls-eof": ssl.SSLEOFError(8, "EOF occurred in violation of protocol"),
    "incomplete-read": http.client.IncompleteRead(b"{", 10),
    "name-not-yet-resolvable": socket.gaierror(socket.EAI_AGAIN, "Temporary failure in name resolution"),
    "network-unreachable": OSError(errno.ENETUNREACH, "Network is unreachable"),
    "host-unreachable": OSError(errno.EHOSTUNREACH, "No route to host"),
    "network-down": OSError(errno.ENETDOWN, "Network is down"),
}

KEPT = {
    "certificate": ssl.SSLCertVerificationError(1, "certificate verify failed"),
    "tls-protocol": ssl.SSLError(1, "wrong version number"),
    "no-such-name": socket.gaierror(socket.EAI_NONAME, "Name or service not known"),
    "bad-status-line": http.client.BadStatusLine("garbage"),
    "permission": PermissionError(errno.EACCES, "Permission denied"),
    "no-such-file": FileNotFoundError(errno.ENOENT, "No such file or directory"),
    "value": ValueError("refused"),
    "status": urllib.error.HTTPError("https://192.0.2.1/", 503, "Service Unavailable", {}, io.BytesIO()),
    "reason-text": urllib.error.URLError("unknown url type: ftp"),
}


@pytest.mark.parametrize("name", sorted(LOST))
def test_a_connection_lost_in_transport_is_lost_bare_or_wrapped(name):
    assert transport_faults.lost(LOST[name]) is True
    assert transport_faults.lost(urllib.error.URLError(LOST[name])) is True


@pytest.mark.parametrize("name", sorted(KEPT))
def test_every_other_failure_is_not_lost_bare_or_wrapped(name):
    assert transport_faults.lost(KEPT[name]) is False
    assert transport_faults.lost(urllib.error.URLError(KEPT[name])) is False


@pytest.mark.parametrize("name", sorted(LOST) + sorted(KEPT))
def test_the_package_and_registry_clients_classify_as_the_shared_helper_does(name):
    error = LOST.get(name, KEPT.get(name))
    assert ceph_package_sources.transport_loss(error) is transport_faults.lost(error)
    assert ceph_registry_discovery.transport_lost(error) is transport_faults.lost(error)
