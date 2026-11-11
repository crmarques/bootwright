"""Verified TLS trust anchors and endpoint-scoped exact certificate identity."""

from __future__ import annotations

import hmac
import http.client
import re
import ssl
import urllib.request

# OpenSSL X509_V_FLAG_PARTIAL_CHAIN, also supported by the managed Python 3.9
# floor whose ssl module does not expose the named constant.
PARTIAL_CHAIN = getattr(ssl, "VERIFY_X509_PARTIAL_CHAIN", 0x80000)
CERTIFICATE = re.compile(r"-----BEGIN CERTIFICATE-----\r?\n[A-Za-z0-9+/=\r\n]+-----END CERTIFICATE-----")


def anchors(data):
    """Public PEM anchors, with every non-whitespace byte accounted for."""
    if not isinstance(data, str) or not 0 < len(data.encode("utf-8")) <= 1 << 20:
        raise ValueError("invalid trust bundle")
    blocks = CERTIFICATE.findall(data)
    if not blocks or CERTIFICATE.sub("", data).strip():
        raise ValueError("invalid trust bundle")
    leaves = []
    for block in blocks:
        isolated = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        isolated.load_verify_locations(cadata=block)
        if isolated.cert_store_stats()["x509_ca"] == 0:
            leaves.append(block)
    return blocks, leaves


def load_anchors(context, data):
    """Load declared public anchors; only leaf trust needs partial chains."""
    _blocks, leaves = anchors(data)
    context.load_verify_locations(cadata=data)
    if leaves:
        context.verify_flags |= PARTIAL_CHAIN
    return context


def context(data, verification="full"):
    """An explicit bundle replaces system roots, without disabling validation."""
    if verification not in ("full", "certificate"):
        raise ValueError("unsupported TLS verification")
    _blocks, leaves = anchors(data)
    if verification == "certificate":
        if len(leaves) != 1:
            raise ValueError("certificate verification requires exactly one server certificate")
        selected = leaves[0]
        result = ssl.create_default_context(cadata=selected)
        result.check_hostname = False
        result.verify_flags |= PARTIAL_CHAIN
        return result, ssl.PEM_cert_to_DER_cert(selected)
    result = ssl.create_default_context(cadata=data)
    if leaves:
        result.verify_flags |= PARTIAL_CHAIN
    return result, None


class CertificateConnection(http.client.HTTPSConnection):
    """Compare the TLS leaf before HTTP, including authentication, is sent."""

    def __init__(self, host, certificate, **kwargs):
        self.certificate = certificate
        super().__init__(host, **kwargs)

    def connect(self):
        super().connect()
        if not hmac.compare_digest(self.sock.getpeercert(binary_form=True), self.certificate):
            self.close()
            raise ssl.SSLCertVerificationError("the server certificate does not match its declared identity")


class CertificateHandler(urllib.request.HTTPSHandler):
    def __init__(self, tls_context, certificate):
        super().__init__(context=tls_context)
        self.certificate = certificate

    def https_open(self, req):
        def connect(host, **kwargs):
            return CertificateConnection(host, self.certificate, **kwargs)
        return self.do_open(connect, req, context=self._context)
