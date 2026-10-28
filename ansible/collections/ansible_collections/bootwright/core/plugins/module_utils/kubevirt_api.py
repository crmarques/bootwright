"""The KubeVirt host cluster's Kubernetes API, reached through one lent kubeconfig.

A host is reached only at the server its kubeconfig names, over TLS verified
against the certificate authority that kubeconfig embeds, authenticated by its
client certificate or its token. Nothing ambient takes part: no proxy, no
redirect, no system trust store and no file the kubeconfig would name. Every
request is bounded in time and in the bytes it reads, and an answer is told
apart from silence: a status the API returned is an answer, a connection that
failed or ran out of time is not, and only a 404 is absence.
"""

from __future__ import annotations

import base64
import http.client
import json
import os
import shutil
import ssl
import tempfile
from urllib.parse import quote, urlsplit

MAX_KUBECONFIG = 65536
MAX_RESPONSE = 4 * 1024 * 1024
REQUEST_SECONDS = 30
# The members one accepted kubeconfig may carry, by level. A member outside
# them, such as insecure-skip-tls-verify, proxy-url, tls-server-name, a file
# reference, an exec or auth-provider entry, or a username and password, would
# let the connection trust or run something the declaration does not name.
TOP = frozenset(("apiVersion", "kind", "clusters", "contexts", "users", "current-context", "preferences"))
CLUSTER = frozenset(("server", "certificate-authority-data"))
CONTEXT = frozenset(("cluster", "user", "namespace"))
USER = frozenset(("client-certificate-data", "client-key-data", "token"))


class Refused(ValueError):
    """A request this client will not send or an answer it will not read, as one printable line."""


class Unanswered(Exception):
    """The host did not answer: the connection failed, timed out or answered past its bound."""


def _named(entries, kind):
    if not isinstance(entries, list) or len(entries) != 1 or not isinstance(entries[0], dict):
        raise Refused("the kubeconfig names other than exactly one %s" % kind)
    entry = entries[0]
    if set(entry) != {"name", kind} or not isinstance(entry["name"], str) or not isinstance(entry[kind], dict):
        raise Refused("the kubeconfig's %s entry carries a member it may not" % kind)
    return entry["name"], entry[kind]


def _decoded(value, member):
    try:
        data = base64.b64decode(value.encode("ascii"), validate=True)
    except (AttributeError, ValueError, UnicodeError):
        raise Refused("the kubeconfig's %s is not base64" % member) from None
    if b"-----BEGIN " not in data:
        raise Refused("the kubeconfig's %s holds no PEM block" % member)
    return data.decode("ascii", "strict")


def accepted(config):
    """The connection one parsed kubeconfig names, or Refused naming why its form is not accepted.

    It names exactly one cluster, one user and one context, which
    current-context selects; an https server with no path, query or user
    information; an embedded certificate authority; and either a client
    certificate and key or a token, never both.
    """
    if not isinstance(config, dict) or set(config) - TOP:
        raise Refused("the kubeconfig carries a top-level member it may not")
    if config.get("preferences") not in (None, {}):
        raise Refused("the kubeconfig declares preferences")
    cluster_name, cluster = _named(config.get("clusters"), "cluster")
    user_name, user = _named(config.get("users"), "user")
    context_name, context = _named(config.get("contexts"), "context")
    if set(cluster) - CLUSTER or set(context) - CONTEXT or set(user) - USER:
        raise Refused("the kubeconfig names a member outside one server, an embedded authority and one credential")
    if config.get("current-context") != context_name or context.get("cluster") != cluster_name or context.get("user") != user_name:
        raise Refused("the kubeconfig's current context does not select its one cluster and user")
    server = cluster.get("server")
    parts = urlsplit(server) if isinstance(server, str) else None
    if (parts is None or parts.scheme != "https" or not parts.hostname or parts.username is not None
            or parts.password is not None or parts.path not in ("", "/") or parts.query or parts.fragment):
        raise Refused("the kubeconfig's server is not one https URL with no path, query or user information")
    if "certificate-authority-data" not in cluster:
        raise Refused("the kubeconfig embeds no certificate authority")
    connection = {"server": server.rstrip("/"), "ca": _decoded(cluster["certificate-authority-data"], "certificate-authority-data")}
    if "token" in user:
        if set(user) != {"token"} or not isinstance(user["token"], str) or not user["token"].strip() or not user["token"].isprintable():
            raise Refused("the kubeconfig's user carries a token beside another credential, or an empty one")
        connection["token"] = user["token"].strip()
    elif set(user) == {"client-certificate-data", "client-key-data"}:
        connection["certificate"] = _decoded(user["client-certificate-data"], "client-certificate-data")
        connection["key"] = _decoded(user["client-key-data"], "client-key-data")
    else:
        raise Refused("the kubeconfig's user authenticates by neither a client certificate and key nor a token")
    return connection


def load(path, parse):
    """Read one kubeconfig file within its bound and return its accepted connection."""
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as handle:
            data = handle.read(MAX_KUBECONFIG + 1)
    except OSError:
        raise Refused("the lent kubeconfig cannot be read") from None
    if len(data) > MAX_KUBECONFIG:
        raise Refused("the kubeconfig is larger than %d bytes" % MAX_KUBECONFIG)
    try:
        config = parse(data.decode("utf-8"))
    except Exception:  # noqa: BLE001 - every parser failure is one refusal naming no content
        raise Refused("the kubeconfig is not a YAML or JSON document") from None
    return accepted(config)


def path(group, version, resource, namespace="", name="", subresource=""):
    """One resource path, every segment escaped, so no value can name another resource."""
    base = "/api/" + version if not group else "/apis/" + group + "/" + version
    if namespace:
        base += "/namespaces/" + quote(namespace, safe="")
    base += "/" + resource
    if name:
        base += "/" + quote(name, safe="")
    if subresource:
        base += "/" + subresource
    return base


class Client:
    """One host cluster's API at exactly the server its kubeconfig names.

    transport, when given, replaces the HTTPS exchange with a callable taking
    (method, path, headers, body) and returning (status, body bytes) or raising
    Unanswered, so the rules above are tested over a fake API.
    """

    def __init__(self, connection, scratch="", transport=None):
        self.connection = connection
        self.server = connection["server"]
        self.scratch = scratch
        self.transport = transport

    def request(self, method, resource, body=None, content_type="application/json", query=""):
        headers = {"Accept": "application/json"}
        payload = None
        if body is not None:
            payload = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = content_type
        if "token" in self.connection:
            headers["Authorization"] = "Bearer " + self.connection["token"]
        target = resource + ("?" + query if query else "")
        if self.transport is not None:
            status, data = self.transport(method, target, headers, payload)
        else:
            status, data = self._exchange(method, target, headers, payload)
        if len(data) > MAX_RESPONSE:
            raise Unanswered()
        if not data:
            return status, None
        try:
            return status, json.loads(data.decode("utf-8"))
        except (UnicodeError, ValueError):
            if 200 <= status < 300:
                raise Unanswered() from None
            return status, None

    def _context(self, directory):
        context = ssl.create_default_context(cadata=self.connection["ca"])
        if "certificate" in self.connection:
            certificate = os.path.join(directory, "client.crt")
            key = os.path.join(directory, "client.key")
            for name, value in ((certificate, self.connection["certificate"]), (key, self.connection["key"])):
                descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                with os.fdopen(descriptor, "w") as handle:
                    handle.write(value)
            context.load_cert_chain(certificate, key)
        return context

    def _exchange(self, method, target, headers, payload):
        parts = urlsplit(self.server)
        directory = tempfile.mkdtemp(prefix="kubevirt-", dir=self.scratch or None)
        try:
            try:
                context = self._context(directory)
            except (OSError, ValueError, ssl.SSLError):
                raise Refused("the kubeconfig's certificate authority or client certificate cannot be loaded") from None
            connection = http.client.HTTPSConnection(parts.hostname, parts.port or 443, timeout=REQUEST_SECONDS, context=context)
            try:
                connection.request(method, target, body=payload, headers=headers)
                response = connection.getresponse()
                data = response.read(MAX_RESPONSE + 1)
                return response.status, data
            except ssl.SSLCertVerificationError:
                raise Refused("the host's API server certificate is not verified by the kubeconfig's certificate authority") from None
            except (OSError, http.client.HTTPException):
                raise Unanswered() from None
            finally:
                connection.close()
        finally:
            shutil.rmtree(directory, ignore_errors=True)

    def get(self, resource, query=""):
        """The object at resource, None when the API answers 404, or Unanswered."""
        status, body = self.request("GET", resource, query=query)
        if status == 404:
            return None
        if status == 200 and isinstance(body, dict):
            return body
        raise Answered(status, body)


class Answered(Exception):
    """The API answered with a status other than the one the caller needs."""

    def __init__(self, status, body=None):
        super().__init__(status)
        self.status = status
        self.reason = body.get("reason", "") if isinstance(body, dict) else ""
