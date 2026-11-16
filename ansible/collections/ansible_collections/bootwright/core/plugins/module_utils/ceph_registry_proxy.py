"""Explicit CONNECT transport; origin TLS and delivery address pins remain independent."""

from __future__ import annotations

import contextlib
import http.client
import socket
import ssl
import time
import urllib.parse


class ProxyFailure(ValueError):
    """One fixed outer-proxy refusal, without exception values or request URLs."""


def translated(error, library):
    """Recover only typed standard transport failures; never parse exception text."""
    pending, seen = [error], set()
    for _position in range(16):
        if not pending:
            break
        value = pending.pop(0)
        if id(value) in seen:
            continue
        seen.add(id(value))
        if isinstance(value, ssl.SSLCertVerificationError):
            if isinstance(error, library.exceptions.ProxyError):
                raise ProxyFailure("proxy-tls") from None
            raise value from None
        if isinstance(value, (OSError, http.client.HTTPException)):
            raise value from None
        for name in ("reason", "original_error", "__cause__", "_reason"):
            nested = getattr(value, name, None)
            if isinstance(nested, Exception):
                pending.append(nested)
        pending.extend(item for item in value.args if isinstance(item, Exception))
    # A NewConnectionError subclass is not necessarily a timed-out connection.
    # pylint: disable=unidiomatic-typecheck
    if type(error) in (library.exceptions.ConnectTimeoutError, library.exceptions.ReadTimeoutError):
        raise socket.timeout("declared proxy connection timed out") from None
    raise http.client.HTTPException("declared proxy request failed") from None


class ProxyConnection:
    """One bounded request over the frozen proxy, with no ambient configuration."""

    def __init__(self, proxy, host, context, timeout, trust, addresses=None):
        # The controller's immutable runtime already includes this dependency.
        import urllib3

        target = urllib.parse.urlsplit("https://" + host)
        self.library, self.proxy, self.authority = urllib3, proxy, host
        self.host, self.port = target.hostname, target.port or 443
        self.context, self.timeout, self.trust = context, timeout, trust
        self.addresses = [row[4][0] for row in addresses] if addresses is not None else [self.host]
        self.stack, self.manager, self.response = contextlib.ExitStack(), None, None

    def request(self, method, path, headers):
        if method != "GET" or any(key.lower() in ("proxy-authorization", "cookie") for key in headers):
            raise ProxyFailure("request")
        outer = None
        if urllib.parse.urlsplit(self.proxy).scheme == "https":
            outer, _prove = self.stack.enter_context(self.trust())
        self.manager = self.library.ProxyManager(
            self.proxy, ssl_context=self.context, proxy_ssl_context=outer,
            server_hostname=self.host, cert_reqs=ssl.CERT_REQUIRED,
            use_forwarding_for_https=False, proxy_headers={}, retries=False,
        )
        deadline = time.monotonic() + self.timeout
        for position, address in enumerate(self.addresses):
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise socket.timeout("declared proxy connection timed out")
            share = remaining / (len(self.addresses) - position)
            authority = "[" + address + "]" if ":" in address else address
            if self.port != 443:
                authority += ":" + str(self.port)
            try:
                self.response = self.manager.request(
                    "GET", "https://" + authority + path, headers=dict(headers, Host=self.authority),
                    redirect=False, retries=False, preload_content=False, decode_content=False,
                    timeout=self.library.Timeout(connect=share, read=remaining, total=remaining),
                )
                return
            except self.library.exceptions.HTTPError as error:
                if position + 1 == len(self.addresses):
                    translated(error, self.library)
                # Only an actual connection loss may try another pinned IP.
                try:
                    translated(error, self.library)
                except (OSError, http.client.HTTPException) as cause:
                    from ansible_collections.bootwright.core.plugins.module_utils import transport_faults

                    if not transport_faults.lost(cause):
                        raise

    def getresponse(self):
        return self

    @property
    def status(self):
        return self.response.status

    def getheaders(self):
        return list(self.response.headers.iteritems())

    def read(self, maximum):
        try:
            return self.response.read(maximum, decode_content=False)
        except self.library.exceptions.HTTPError as error:
            translated(error, self.library)

    def close(self):
        try:
            if self.response is not None:
                self.response.close()
            if self.manager is not None:
                self.manager.clear()
        finally:
            self.stack.close()
