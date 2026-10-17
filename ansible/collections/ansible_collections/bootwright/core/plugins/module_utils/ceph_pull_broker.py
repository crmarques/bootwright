"""An invocation-local registry credential boundary for a native Podman pull.

Only this broker holds the entitlement and registry bearer token. The native
client receives an empty authfile and an ephemeral CA, so a proxy bypass never
carries credentials. Every upstream connection uses the host TLS trust.
Authentication stays at the selected registry authority; only blob reads may
use one public HTTPS delivery endpoint without registry credentials. Foreign
token realms refuse.
"""

from __future__ import annotations

import base64
import contextlib
import http.client
import ipaddress
import json
import os
import re
import socket
import ssl
import stat
import sys
import threading
import time
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, native_diagnostics

MAX_HEADERS = 16384
MAX_METADATA = 1 << 20
MAX_BLOB = 16 << 30
MAX_TOTAL = 32 << 30
MAX_CONNECTIONS = 512
MAX_WORKERS = 8
HEX = re.compile(r"[0-9a-f]{64}\Z")


# A fresh exec owns potentially blocking libc/NSS resolution. No Python work
# runs after a bare fork of the threaded broker, and no URL or credential is an
# argument. The native command owner bounds pipes, time and kill/reap cleanup.
DNS_SOURCE = """import json, socket, sys
try:
    host = sys.stdin.buffer.read(254).decode('ascii')
    rows = socket.getaddrinfo(host, 443, type=socket.SOCK_STREAM, proto=socket.IPPROTO_TCP)
    if not 0 < len(rows) <= 16:
        raise ValueError()
    data = json.dumps([[family, kind, protocol, list(address)] for family, kind, protocol, _, address in rows]).encode()
    if len(data) > 8192:
        raise ValueError()
    sys.stdout.buffer.write(data)
except Exception:
    sys.exit(1)
"""


def delivery_location(response):
    locations = [value for name, value in response.getheaders() if name.lower() == "location"]
    require(len(locations) == 1)
    value = locations[0]
    require(isinstance(value, str) and 0 < len(value) <= 4096 and all(32 < ord(char) < 127 for char in value)
            and "\\" not in value)
    try:
        parsed = urllib.parse.urlsplit(value)
        require(parsed.scheme == "https" and parsed.hostname and parsed.port in (None, 443)
                and parsed.username is None and parsed.password is None and not parsed.fragment and "#" not in value)
        host = parsed.hostname
        require(len(host) <= 253 and all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) is not None
                                         for label in host.split(".")))
        require(parsed.netloc.lower() in (host, host + ":443"))
        path = parsed.path or "/"
        require(path.startswith("/") and not path.startswith("//"))
        return host, path + ("?" + parsed.query if parsed.query else "")
    except ValueError:
        raise BrokerError("blob delivery location") from None


def delivery_addresses(raw):
    # DNS framing integers must reject bool and float, not accept int subclasses.
    # pylint: disable=unidiomatic-typecheck
    require(isinstance(raw, bytes) and len(raw) <= 8192)
    rows = json.loads(raw)
    require(isinstance(rows, list) and 0 < len(rows) <= 16)
    addresses, seen = [], set()
    for row in rows:
        require(isinstance(row, list) and len(row) == 4 and all(type(value) is int for value in row[:3]))
        family, kind, protocol, address = row
        require(family in (socket.AF_INET, socket.AF_INET6) and kind == socket.SOCK_STREAM and protocol == socket.IPPROTO_TCP)
        require(isinstance(address, list) and len(address) == (2 if family == socket.AF_INET else 4)
                and isinstance(address[0], str) and all(type(value) is int for value in address[1:]) and address[1] == 443)
        require(family == socket.AF_INET or address[2:] == [0, 0])
        ip = ipaddress.ip_address(address[0])
        require(ip.version == (4 if family == socket.AF_INET else 6) and str(ip) == address[0]
                and ip.is_global and not ip.is_reserved and not ip.is_multicast and not ip.is_unspecified
                and not ip.is_loopback and not ip.is_link_local and not ip.is_private)
        require(ip.version == 4 or not ip.is_site_local and ip.ipv4_mapped is None and ip.sixtofour is None and ip.teredo is None)
        identity = (family, tuple(address))
        require(identity not in seen)
        seen.add(identity)
        addresses.append((family, kind, protocol, "", tuple(address)))
    return addresses


class DeliveryResponse:
    """Only blob bytes and a bounded framing length can cross this boundary."""

    _bootwright_delivery = True

    def __init__(self, response):
        self.response, self.status = response, response.status
        self.length = response.getheader("Content-Length") if self.status == 200 else None
        require(self.length is None or re.fullmatch(r"0|[1-9][0-9]{0,11}", self.length) is not None
                and int(self.length) <= MAX_BLOB)

    def getheader(self, name, default=None):
        return {"Content-Type": "application/octet-stream", "Content-Length": self.length}.get(name, default)

    def read(self, maximum):
        return self.response.read(maximum)


class BrokerError(ValueError):
    """No private value is included in the exception text."""


def require(value):
    if not value:
        raise BrokerError("registry credential boundary")


def authority(value):
    require(isinstance(value, str) and re.fullmatch(r"[a-z0-9][a-z0-9.:-]{0,255}", value) is not None)
    parsed = urllib.parse.urlsplit("https://" + value)
    require(parsed.hostname and parsed.username is None and parsed.password is None and not parsed.path)
    require(parsed.port is None or 1 <= parsed.port <= 65535)
    return parsed.hostname, parsed.port or 443


def certificate(directory, cert_directory, registry, command):
    """Use the signed native OpenSSL executable; no Python crypto dependency."""
    host, unused_port = authority(registry)
    try:
        ipaddress.ip_address(host)
        san = "IP:" + host
    except ValueError:
        san = "DNS:" + host
    config = ("[req]\nprompt=no\ndistinguished_name=dn\nx509_extensions=server\n"
              "[dn]\nCN=Bootwright ephemeral pull broker\n[server]\n"
              "basicConstraints=critical,CA:TRUE\nkeyUsage=critical,digitalSignature,keyEncipherment,keyCertSign\n"
              "extendedKeyUsage=serverAuth,clientAuth\nsubjectAltName=" + san + "\n")
    config_path = os.path.join(directory, "openssl.cnf")
    key, cert = os.path.join(directory, "broker.key"), os.path.join(directory, "ca.crt")
    with open(config_path, "x", encoding="ascii") as stream:
        stream.write(config)
    os.chmod(config_path, 0o600)
    command(["/usr/bin/openssl", "req", "-new", "-x509", "-newkey", "rsa:3072", "-nodes", "-days", "1",
             "-config", config_path, "-keyout", key, "-out", cert], 30, 65536)
    os.chmod(key, 0o600)
    os.chmod(cert, 0o600)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(cert, key)
    context.load_verify_locations(cafile=cert)
    context.verify_mode = ssl.CERT_REQUIRED
    # containers/image rejects every .key without a matching .cert. Keep
    # broker setup material outside the directory scanned by the native client.
    # This ephemeral identity permits the native process to use this broker;
    # it has no authority at the real registry.
    os.mkdir(cert_directory, 0o700)
    for source, name in ((cert, "ca.crt"), (cert, "client.cert"), (key, "client.key")):
        with open(source, "rb") as incoming, open(os.path.join(cert_directory, name), "xb") as outgoing:
            outgoing.write(incoming.read(65536))
        os.chmod(os.path.join(cert_directory, name), 0o600)
    return context


def read_request(stream):
    """One bounded, body-free HTTP request; no client headers are forwarded."""
    line = stream.readline(MAX_HEADERS + 1)
    require(len(line) <= MAX_HEADERS and line.endswith(b"\r\n"))
    fields = line[:-2].decode("ascii").split(" ")
    require(len(fields) == 3 and fields[2] in ("HTTP/1.0", "HTTP/1.1"))
    headers, total = {}, len(line)
    while True:
        line = stream.readline(MAX_HEADERS + 1)
        total += len(line)
        require(total <= MAX_HEADERS and line.endswith(b"\r\n"))
        if line == b"\r\n":
            break
        name, separator, value = line[:-2].partition(b":")
        require(separator and re.fullmatch(rb"[A-Za-z0-9-]+", name) is not None)
        key = name.decode("ascii").lower()
        # Native manifest requests repeat Accept. Its values stay local; the
        # upstream owner supplies its own Accept header. Other duplicates refuse.
        require(key == "accept" or key not in headers)
        headers[key] = value.strip().decode("ascii")
    require("transfer-encoding" not in headers and headers.get("content-length", "0") == "0")
    require("authorization" not in headers and "proxy-authorization" not in headers and "cookie" not in headers)
    return fields[0], fields[1], headers


class RegistryConnection(http.client.HTTPSConnection):
    """Resolved addresses keep DNS out of joinable worker threads; TLS names stay original."""

    def __init__(self, host, *, addresses, deadline, **options):
        super().__init__(host, **options)
        self.addresses, self.deadline = addresses, deadline
        self.cancelled = threading.Event()

    def connect(self):
        require(not self._tunnel_host)
        last = None
        deadline = min(self.deadline, time.monotonic() + self.timeout)
        for family, kind, protocol, unused_name, address in self.addresses:
            require(not self.cancelled.is_set() and time.monotonic() < deadline)
            try:
                self.sock = socket.socket(family, kind, protocol)
                self.sock.settimeout(max(0.01, deadline - time.monotonic()))
                self.sock.connect(address)
                self.sock = self._context.wrap_socket(self.sock, server_hostname=self.host, do_handshake_on_connect=False)
                self.sock.do_handshake()
                return
            except OSError as error:
                last = error
                if self.sock is not None:
                    self.sock.close()
                self.sock = None
        if last is not None:
            raise last
        raise BrokerError("registry connection unavailable")

    def close(self):
        self.cancelled.set()
        super().close()


class Upstream:
    """Scoped HTTPS registry transport. Basic credentials never leave this owner."""

    def __init__(self, registry, repository, credential, deadline, *, connection=None, clock=time.monotonic, context=None, resolver_runner=None):
        self.host, self.port = authority(registry)
        require(repository.startswith(registry + "/"))
        self.registry, self.namespace = registry, repository.split("/", 1)[1]
        self.deadline, self.clock = deadline, clock
        self.resolver_runner = resolver_runner or ceph_native.bounded_command
        self.stopping = threading.Event()
        if connection is None:
            # This synchronous setup remains under the owning remote worker's
            # hard deadline. No started broker thread performs name resolution.
            addresses = socket.getaddrinfo(self.host, self.port, type=socket.SOCK_STREAM)
            require(0 < len(addresses) <= 16)
            self.connection = lambda host, **options: RegistryConnection(host, addresses=addresses, deadline=deadline, **options)
        else:
            self.connection = connection
        require(isinstance(credential, dict) and set(credential) == {"url", "username", "password"} and credential["url"] == registry)
        user, password = credential["username"], credential["password"]
        require(all(isinstance(value, str) and 0 < len(value.encode()) <= 32768 and not any(char in value for char in "\x00\r\n")
                    for value in (user, password)) and ":" not in user)
        self.private = [user, password]
        self.basic = "Basic " + base64.b64encode((user + ":" + password).encode()).decode("ascii")
        self.private.append(self.basic)
        self.bearer = None
        self.lock = threading.Lock()
        self.connections = set()
        self.connections_lock = threading.Lock()
        self.context = context or ssl.create_default_context(cafile="/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem")
        self.context.minimum_version = ssl.TLSVersion.TLSv1_2

    def budget(self):
        remaining = self.deadline - self.clock()
        require(remaining > 0 and not self.stopping.is_set())
        return min(30, remaining)

    @contextlib.contextmanager
    def exchange(self, connection, method, path, headers):
        try:
            with self.connections_lock:
                require(not self.stopping.is_set())
                self.connections.add(connection)
            connection.request(method, path, headers=headers)
            response = connection.getresponse()
            rows = response.getheaders()
            require(len(rows) <= 64 and sum(len(name) + len(value) for name, value in rows) <= MAX_HEADERS)
            require(response.getheader("Content-Encoding", "identity") == "identity")
            yield response
        finally:
            connection.close()
            with self.connections_lock:
                self.connections.discard(connection)

    @contextlib.contextmanager
    def request(self, method, path, authorization=None):
        connection = self.connection(self.host, port=self.port, context=self.context, timeout=self.budget())
        headers = {"Accept-Encoding": "identity",
                   "Accept": "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json",
                   "User-Agent": "bootwright-pull-broker-v1"}
        if authorization:
            headers["Authorization"] = authorization
        with self.exchange(connection, method, path, headers) as response:
            yield response

    def deliver(self, method, response, consume):
        host, path = delivery_location(response)
        try:
            require(not self.stopping.is_set())
            code, raw = self.resolver_runner([sys.executable, "-I", "-B", "-c", DNS_SOURCE],
                                             data=host.encode("ascii"), timeout=min(5, self.budget()), limit=8192)
            require(code == 0 and not self.stopping.is_set())
            addresses = delivery_addresses(raw)
        except Exception:
            # Neither resolver output nor a signed URL can enter diagnostics.
            raise BrokerError("blob delivery resolution") from None
        connection = RegistryConnection(host, addresses=addresses, deadline=self.deadline,
                                        port=443, context=self.context, timeout=self.budget())
        headers = {"Accept-Encoding": "identity", "User-Agent": "bootwright-pull-broker-v1"}
        with self.exchange(connection, method, path, headers) as delivered:
            return consume(DeliveryResponse(delivered))

    def consume(self, method, path, response, consume):
        prefix = "/v2/" + self.namespace + "/blobs/sha256:"
        if (method in ("GET", "HEAD") and path.startswith(prefix) and HEX.fullmatch(path[len(prefix):]) is not None
                and response.status in (307, 308)):
            return self.deliver(method, response, consume)
        return consume(response)

    def close(self):
        with self.connections_lock:
            self.stopping.set()
            connections = list(self.connections)
        for connection in connections:
            if connection.sock is not None:
                with contextlib.suppress(OSError):
                    connection.sock.shutdown(socket.SHUT_RDWR)
            connection.close()

    def token_path(self, challenge):
        require(isinstance(challenge, str) and len(challenge) <= 4096 and challenge.startswith("Bearer "))
        fields, remaining = {}, challenge[7:]
        while remaining:
            match = re.match(r'([A-Za-z_]+)="([^"\\\x00-\x1f\x7f]*)"(?:,\s*|$)', remaining)
            require(match is not None and match[1] not in fields)
            fields[match[1]] = match[2]
            remaining = remaining[match.end():]
        scope = "repository:" + self.namespace + ":pull"
        require({"realm", "service"} <= set(fields) <= {"realm", "service", "scope"} and fields.get("scope", scope) == scope)
        realm = urllib.parse.urlsplit(fields["realm"])
        require(realm.scheme == "https" and realm.netloc == self.registry and realm.username is None and realm.password is None
                and realm.path.startswith("/") and not realm.query and not realm.fragment)
        return realm.path + "?" + urllib.parse.urlencode({"service": fields["service"], "scope": scope})

    def authenticate(self, challenge):
        path = self.token_path(challenge)
        with self.lock:
            with self.request("GET", path, self.basic) as response:
                require(response.status == 200)
                raw = response.read(65537)
                require(len(raw) <= 65536)
                value = json.loads(raw)
                bearer = value.get("token", value.get("access_token"))
                require(isinstance(bearer, str) and 0 < len(bearer) <= 32768 and re.fullmatch(r"[A-Za-z0-9._~+/=-]+", bearer) is not None)
                self.bearer = "Bearer " + bearer
                self.private.extend((bearer, self.bearer))

    def fetch(self, method, path, consume):
        with self.request(method, path, self.bearer) as response:
            if response.status != 401:
                return self.consume(method, path, response, consume)
            challenge = response.getheader("WWW-Authenticate")
        self.authenticate(challenge)
        with self.request(method, path, self.bearer) as response:
            return self.consume(method, path, response, consume)


class Broker:
    """Bounded concurrent CONNECT listeners, all joined before private cleanup."""

    def __init__(self, directory, request, private, deadline, command, *, upstream=None, tls=None):
        self.directory, self.request, self.deadline = directory, request, deadline
        info = os.stat(directory, follow_symlinks=False)
        require(stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.geteuid())
        self.authority = request["registry"]
        self.host, self.port = authority(self.authority)
        self.repository, separator, self.digest = request["image"].rpartition("@sha256:")
        require(separator and HEX.fullmatch(self.digest) is not None and self.repository.startswith(self.authority + "/"))
        self.namespace = self.repository.split("/", 1)[1]
        self.upstream = upstream or Upstream(self.authority, self.repository, json.loads(private), deadline)
        self.cert_directory = os.path.join(directory, "native-certs")
        self.tls = tls or certificate(directory, self.cert_directory, self.authority, command)
        self.home = os.path.join(directory, "home")
        os.mkdir(self.home, 0o700)
        self.registries = os.path.join(directory, "registries.conf")
        with open(self.registries, "x", encoding="ascii") as stream:
            stream.write('unqualified-search-registries = []\ncredential-helpers = ["containers-auth.json"]\n')
        os.chmod(self.registries, 0o600)
        self.listener = None
        self.connections, self.workers = set(), []
        self.lock = threading.Lock()
        self.stopping = threading.Event()
        self.slots = threading.BoundedSemaphore(MAX_WORKERS)
        self.total = self.accepted = 0
        self.failure = None

    def path(self, method, path):
        require(method in ("GET", "HEAD"))
        if path == "/v2/":
            return MAX_METADATA
        prefix = "/v2/" + self.namespace
        if path == prefix + "/manifests/sha256:" + self.digest:
            return MAX_METADATA
        blob = prefix + "/blobs/sha256:"
        require(path.startswith(blob) and HEX.fullmatch(path[len(blob):]) is not None)
        return MAX_BLOB

    @property
    def failure_cause(self):
        """Latest refusal observed in this invocation, including its native retries."""
        with self.lock:
            failure = self.failure
        if failure is None:
            return ""
        request_class, status = failure
        return "broker observed " + request_class + " upstream HTTP " + str(status)

    def response(self, stream, response, method, maximum, request_class):
        if response.status != 200:
            with self.lock:
                self.failure = (request_class, response.status)
            if getattr(response, "_bootwright_delivery", False):
                message = "blob delivery refused"
            else:
                raw = response.read(MAX_METADATA + 1)
                message = native_diagnostics.redact(raw[:MAX_METADATA], secrets=self.upstream.private)
            body = json.dumps({"errors": [{"code": "REGISTRY_REFUSED", "message": "upstream HTTP " + str(response.status) + ": " + message}]}).encode()
            stream.write(b"HTTP/1.1 502 Bad Gateway\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: "
                         + str(len(body)).encode() + b"\r\n\r\n" + body)
            return
        length = response.getheader("Content-Length")
        require(length is None or re.fullmatch(r"0|[1-9][0-9]{0,11}", length) is not None and int(length) <= maximum)
        headers = b"HTTP/1.1 200 OK\r\nConnection: close\r\n"
        for name in ("Content-Type", "Docker-Content-Digest", "Docker-Distribution-Api-Version", "Content-Length"):
            value = response.getheader(name)
            if value is not None:
                require(len(value) <= 512 and all(32 <= ord(char) < 127 for char in value))
                headers += name.encode() + b": " + value.encode() + b"\r\n"
        stream.write(headers + b"\r\n")
        if method == "HEAD":
            return
        count = 0
        while True:
            require(not self.stopping.is_set() and time.monotonic() < self.deadline)
            block = response.read(min(1 << 20, maximum - count + 1))
            if not block:
                break
            count += len(block)
            with self.lock:
                self.total += len(block)
                require(count <= maximum and self.total <= MAX_TOTAL)
            stream.write(block)
        require(length is None or count == int(length))

    def serve(self, connection):
        secured = None
        try:
            connection.settimeout(min(30, max(0.1, self.deadline - time.monotonic())))
            with connection.makefile("rwb", buffering=0) as stream:
                method, target, unused_headers = read_request(stream)
                require(method == "CONNECT" and target == self.host + ":" + str(self.port))
                stream.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            secured = self.tls.wrap_socket(connection, server_side=True, do_handshake_on_connect=False)
            with self.lock:
                self.connections.add(secured)
            secured.do_handshake()
            with secured.makefile("rwb", buffering=0) as stream:
                method, path, headers = read_request(stream)
                require(headers.get("host") in (self.authority, self.host + ":" + str(self.port)))
                maximum = self.path(method, path)
                request_class = "registry" if path == "/v2/" else "blob" if maximum == MAX_BLOB else "manifest"
                self.upstream.fetch(method, path, lambda response: self.response(stream, response, method, maximum, request_class))
        except (BrokerError, OSError, ValueError, http.client.HTTPException):
            # A closed connection is a native pull error. No exception text or
            # authentication challenge can cross the downstream boundary.
            pass
        finally:
            for item in (secured, connection):
                if item is not None:
                    item.close()
                    with self.lock:
                        self.connections.discard(item)
            self.slots.release()

    def accept(self):
        while not self.stopping.is_set() and time.monotonic() < self.deadline:
            try:
                connection, peer = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            if peer[0] != "127.0.0.1" or not self.slots.acquire(blocking=False):
                connection.close()
                continue
            self.accepted += 1
            if self.accepted > MAX_CONNECTIONS:
                connection.close()
                self.slots.release()
                break
            worker = threading.Thread(target=self.serve, args=(connection,), daemon=False)
            with self.lock:
                self.connections.add(connection)
                self.workers.append(worker)
            worker.start()

    def __enter__(self):
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            self.listener.bind(("127.0.0.1", 0))
            self.listener.listen(MAX_WORKERS)
            self.listener.settimeout(0.2)
            self.thread = threading.Thread(target=self.accept, daemon=False)
            self.thread.start()
            return self
        except BaseException:
            self.listener.close()
            raise

    @property
    def environment(self):
        proxy = "http://127.0.0.1:" + str(self.listener.getsockname()[1])
        return {"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "NO_PROXY": "", "https_proxy": proxy, "http_proxy": proxy, "no_proxy": "",
                "CONTAINERS_REGISTRIES_CONF": self.registries, "DOCKER_CONFIG": self.home, "HOME": self.home}

    def __exit__(self, unused_type, unused_value, unused_traceback):
        self.stopping.set()
        self.listener.close()
        self.thread.join(timeout=1)
        with self.lock:
            connections = list(self.connections)
        for connection in connections:
            with contextlib.suppress(OSError):
                connection.shutdown(socket.SHUT_RDWR)
        self.upstream.close()
        deadline = time.monotonic() + 31
        for worker in self.workers:
            worker.join(timeout=max(0, deadline - time.monotonic()))
        require(not self.thread.is_alive() and all(not worker.is_alive() for worker in self.workers))
        for name in ("ca.crt", "client.cert", "client.key"):
            with contextlib.suppress(FileNotFoundError):
                os.unlink(os.path.join(self.cert_directory, name))
        with contextlib.suppress(FileNotFoundError):
            os.rmdir(self.cert_directory)
        for name in ("openssl.cnf", "broker.key", "ca.crt", "registries.conf"):
            with contextlib.suppress(FileNotFoundError):
                os.unlink(os.path.join(self.directory, name))
        os.rmdir(self.home)
