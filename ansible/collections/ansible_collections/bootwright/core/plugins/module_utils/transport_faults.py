"""Which network failures are a connection lost in transport, the one cause a bounded retry may answer.

One classification serves every client that makes a request again after a
short drop of a link: the Ceph package acquisition and the Ceph registry
client.
"""

from __future__ import annotations

import errno
import http.client
import socket
import ssl
import urllib.error

# socket.timeout is not a TimeoutError on the managed-host floor interpreter.
FAULTS = (ConnectionError, TimeoutError, socket.timeout, http.client.IncompleteRead, ssl.SSLEOFError)
# A network or host without a route while a link is down; a name that does not exist is not one.
ERRNOS = (errno.ENETDOWN, errno.ENETUNREACH, errno.EHOSTUNREACH)


def lost(error):
    """A connection closed, reset, refused, aborted, timed out, ended by TLS EOF or
    truncated, a network or host without a route, or a name not yet resolvable,
    is lost in transport, whether urllib wrapped it or not. An HTTP status, a
    certificate or other TLS failure, a name that does not exist and every
    other error are not."""
    if isinstance(error, urllib.error.HTTPError):
        return False
    if isinstance(error, urllib.error.URLError):
        error = error.reason
    if isinstance(error, socket.gaierror):
        return error.errno == socket.EAI_AGAIN
    if isinstance(error, ssl.SSLError):
        return isinstance(error, ssl.SSLEOFError)
    return isinstance(error, FAULTS) or isinstance(error, OSError) and error.errno in ERRNOS
