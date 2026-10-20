"""Session-free resolution of the authored IBM Ceph image and its auxiliary image set.

The approved Python runtime talks only to the selected HTTPS registry authority.
No ambient proxy is followed. Only the config blob read follows one delivery
redirect, to a public HTTPS endpoint and without credentials; manifest and token
redirects refuse. Tags and direct digests select their own manifest; a
manifest's config proves the platform where no index does.
Observation and removal read nothing remote, and continuation uses frozen evidence.
"""

from __future__ import annotations

import base64
import contextlib
import hashlib
import http.client
import json
import os
import re
import signal
import socket
import ssl
import stat
import time
import types
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_worker, ceph_pull_broker, native_diagnostics

PLATFORM = "linux/amd64"
VERSION = "ceph-image-v4"
OPERATIONS = ("apply", "observe", "destroy")
REQUEST = ("base", "cluster", "controller", "image", "registry", "registryRef", "release", "tag", "version")
# cephadm's container_image_<name> settings an auxiliary image may fill, in name order.
AUXILIARY_NAMES = (
    "alertmanager", "alloy", "grafana", "haproxy", "keepalived", "loki", "nginx", "node_exporter",
    "nvmeof", "oauth2_proxy", "prometheus", "promtail", "samba", "samba_metrics", "snmp_gateway",
)
REASONS = ("request", "artifact", "material", "discovery", "timeout", "unknown", "adapter-exception", "image-digest-mismatch")
TOTAL_SECONDS = 180
MAX_BODY = 1 << 20
MAX_REGISTRY = 64 << 10
MAX_TOKEN = 32768
CA_PATH = "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"
MAX_CA = 8 << 20
MAX_CERTIFICATES = 2048
MAX_CERTIFICATE = 64 << 10
CA_UID = 0
ID = re.compile(r"[0-9a-f]{64}\Z")
INDEX_TYPES = ("application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json")
MANIFEST_TYPES = ("application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json")
CONFIG_TYPES = ("application/vnd.oci.image.config.v1+json", "application/vnd.docker.container.image.v1+json")
LAYER_TYPES = (
    "application/vnd.oci.image.layer.v1.tar",
    "application/vnd.oci.image.layer.v1.tar+gzip",
    "application/vnd.oci.image.layer.v1.tar+zstd",
    "application/vnd.oci.image.layer.nondistributable.v1.tar",
    "application/vnd.oci.image.layer.nondistributable.v1.tar+gzip",
    "application/vnd.oci.image.layer.nondistributable.v1.tar+zstd",
    "application/vnd.docker.image.rootfs.diff.tar.gzip",
    "application/vnd.docker.image.rootfs.foreign.diff.tar.gzip",
)
ACCEPT = ", ".join(INDEX_TYPES + MANIFEST_TYPES)
META = ("st_dev", "st_ino", "st_uid", "st_gid", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")


class DiscoveryRefusal(ValueError):
    """A fixed reason only, without URLs, native output or credential facts."""


def require(value, reason="request"):
    if not value:
        raise DiscoveryRefusal(reason)


def refusal(error):
    """The closed reason and non-secret detail of any exit; never a value."""
    if isinstance(error, DiscoveryRefusal) and str(error) in REASONS:
        return str(error), None
    return ("unknown" if isinstance(error, DiscoveryRefusal) else "adapter-exception"), ceph_native_worker.cause(error)


def decode(data, reason):
    def pairs(rows):
        value, folded = {}, set()
        for key, item in rows:
            require(key.lower() not in folded, reason)
            folded.add(key.lower())
            value[key] = item
        return value

    try:
        return json.loads(data, object_pairs_hook=pairs, parse_constant=lambda unused_constant: require(False, reason))
    except (ValueError, UnicodeError, TypeError, RecursionError):
        raise DiscoveryRefusal(reason) from None


def digest(value):
    return isinstance(value, str) and ID.fullmatch(value) is not None and value != "0" * 64


def name(value):
    return isinstance(value, str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}", value) is not None


def validate_request(value):
    require(isinstance(value, dict) and set(REQUEST) <= set(value) <= set(REQUEST) | {"auxiliary", "registryTrustBundleRef"})
    require(value["version"] == VERSION and isinstance(value["release"], str) and bool(value["release"]))
    validate_selection(value)
    require(name(value["cluster"]) and name(value["controller"]) and name(value["registryRef"]))
    require("registryTrustBundleRef" not in value or name(value["registryTrustBundleRef"]))
    if "auxiliary" in value:
        entries = value["auxiliary"]
        require(isinstance(entries, list) and 1 <= len(entries) <= len(AUXILIARY_NAMES))
        require(all(isinstance(entry, dict) and set(entry) == {"name", "base", "tag", "image"} for entry in entries))
        require(all(isinstance(entry["name"], str) and entry["name"] in AUXILIARY_NAMES for entry in entries))
        order = [AUXILIARY_NAMES.index(entry["name"]) for entry in entries]
        require(order == sorted(set(order)))
        for entry in entries:
            validate_selection(auxiliary_request(value, entry))
    return value


def validate_selection(value):
    """One repository beneath the registry authority and its authored tag or digest."""
    require(all(isinstance(value[key], str) and "\x00" not in value[key] for key in ("base", "tag", "image", "registry")))
    require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9.:-]*", value["registry"]) is not None)
    require(value["base"].startswith(value["registry"] + "/") and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./:_-]+", value["base"]) is not None)
    require(all(part not in ("", ".", "..") for part in value["base"].split("/")))
    authored = authored_digest(value)
    require(value["image"] == value["base"] + ":" + value["tag"] and bool(value["tag"])
            or authored is not None and value["tag"] == "" and authored.startswith("sha256:") and ID.fullmatch(authored[7:]) is not None)


def auxiliary_request(request, entry):
    """The Ceph request's members with one auxiliary image's coordinates in place of the Ceph image's."""
    value = {key: request[key] for key in REQUEST}
    if "registryTrustBundleRef" in request:
        value["registryTrustBundleRef"] = request["registryTrustBundleRef"]
    value.update(base=entry["base"], tag=entry["tag"], image=entry["image"])
    return value


def authored_digest(request):
    """The digest an operator authored in place of the tag, or None."""
    prefix = request["base"] + "@"
    return request["image"][len(prefix) :] if request["image"].startswith(prefix) else None


def evidence(value, request_digest):
    """Exactly the presence or absence one resolution may publish."""
    require(isinstance(request_digest, str) and digest(request_digest), "request")
    require(isinstance(value, dict) and value.get("request") == request_digest and value.get("postcondition") is True, "discovery")
    if set(value) == {"request", "postcondition", "absent"}:
        require(value["absent"] is True, "discovery")
        return value
    keys = {"request", "postcondition", "release", "base", "tag", "digest", "platform"}
    require(set(value) in (keys, keys | {"auxiliary"}), "discovery")
    require(all(isinstance(value[key], str) for key in ("release", "base", "tag")) and value["platform"] == PLATFORM, "discovery")
    require(isinstance(value["digest"], str) and value["digest"].startswith("sha256:") and digest(value["digest"][7:]), "discovery")
    if "auxiliary" in value:
        entries = value["auxiliary"]
        require(isinstance(entries, list) and 1 <= len(entries) <= len(AUXILIARY_NAMES), "discovery")
        for entry in entries:
            require(isinstance(entry, dict) and set(entry) == {"name", "base", "tag", "digest", "platform"}, "discovery")
            require(all(isinstance(entry[key], str) for key in ("name", "base", "tag")) and entry["platform"] == PLATFORM, "discovery")
            require(isinstance(entry["digest"], str) and entry["digest"].startswith("sha256:") and digest(entry["digest"][7:]), "discovery")
            require(entry["name"] in AUXILIARY_NAMES, "discovery")
        order = [AUXILIARY_NAMES.index(entry["name"]) for entry in entries]
        require(order == sorted(set(order)), "discovery")
    return value


def metadata(info):
    return tuple(getattr(info, name) for name in META)


def directory_metadata(info):
    return info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode


@contextlib.contextmanager
def protected_file(path, maximum, *, private=False, root=False):
    """Hold and reprove the original file and every named ancestor."""
    reason = "artifact" if root else "material"
    require(isinstance(path, str) and path.startswith("/") and len(path) <= 4096 and not any(ord(character) < 32 for character in path), reason)
    parts = path.split("/")[1:]
    require(parts and all(part not in ("", ".", "..") for part in parts), reason)
    parent = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    held, names, handle = [parent], [], None
    try:
        for index, part in enumerate(parts[:-1]):
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
            held.append(child)
            info = os.fstat(child)
            sticky = index == 0 and part == "tmp" and info.st_uid == 0 and info.st_mode & stat.S_ISVTX
            allowed = info.st_uid == CA_UID if root else info.st_uid in (0, os.geteuid())
            require(sticky and not root or allowed and info.st_mode & 0o022 == 0, reason)
            names.append((parent, part, child, directory_metadata(info)))
            parent = child
        info = os.fstat(parent)
        require(not private or info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, reason)
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
        handle = os.fdopen(descriptor, "rb")
        info = os.fstat(descriptor)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == (CA_UID if root else os.geteuid()) and 0 < info.st_size <= maximum, reason)
        require(stat.S_IMODE(info.st_mode) == 0o600 if private else info.st_mode & 0o022 == 0, reason)
        original = metadata(info)

        def prove():
            require(metadata(os.fstat(handle.fileno())) == original and metadata(os.stat(parts[-1], dir_fd=parent, follow_symlinks=False)) == original, reason)
            for ancestor, name, child, identity in names:
                require(
                    directory_metadata(os.fstat(child)) == identity and directory_metadata(os.stat(name, dir_fd=ancestor, follow_symlinks=False)) == identity,
                    reason,
                )

        prove()
        yield handle, prove
        prove()
    except OSError:
        raise DiscoveryRefusal(reason) from None
    finally:
        if handle is not None:
            handle.close()
        for descriptor in reversed(held):
            os.close(descriptor)


def basic_material(raw, registry):
    """The Basic credential of the selected registry entitlement."""
    value = decode(raw, "material")
    require(isinstance(value, dict) and set(value) == {"url", "username", "password"}
            and value["url"] == registry and isinstance(value["username"], str) and bool(value["username"]), "material")
    require(len(value["username"].encode()) <= 256 and not any(character in value["username"] for character in ":\x00\r\n"), "material")
    password = value["password"]
    require(isinstance(password, str) and 0 < len(password.encode()) <= 32768 and not any(character in password for character in "\x00\r\n"), "material")
    return "Basic " + base64.b64encode((value["username"] + ":" + password).encode()).decode("ascii")


def certificates(data):
    """Project only complete ASCII certificate blocks from the public bundle."""
    require(isinstance(data, bytes) and 0 < len(data) <= MAX_CA, "artifact")
    try:
        lines = data.decode("utf-8").splitlines()
    except UnicodeError:
        raise DiscoveryRefusal("artifact") from None
    blocks, current, length = [], None, 0
    for line in lines:
        stripped = line.strip()
        if current is None:
            if not stripped or stripped.startswith("#"):
                continue
            require(stripped == "-----BEGIN CERTIFICATE-----" and len(blocks) < MAX_CERTIFICATES, "artifact")
            current = []
            length = 0
        elif stripped == "-----END CERTIFICATE-----":
            require(current, "artifact")
            encoded = "".join(current)
            try:
                der = base64.b64decode(encoded, validate=True)
            except ValueError:
                raise DiscoveryRefusal("artifact") from None
            require(der and base64.b64encode(der).decode("ascii") == encoded, "artifact")
            blocks.append("-----BEGIN CERTIFICATE-----\n" + "\n".join(current) + "\n-----END CERTIFICATE-----\n")
            current = None
        else:
            require(line.isascii() and re.fullmatch(r"[A-Za-z0-9+/]{1,76}={0,2}", stripped) is not None, "artifact")
            length += len(stripped)
            require(length <= MAX_CERTIFICATE, "artifact")
            current.append(stripped)
    require(current is None and blocks, "artifact")
    return blocks


@contextlib.contextmanager
def tls_proof():
    """Reuse the native owner's fixed OS trust path, held through discovery."""
    try:
        with protected_file(CA_PATH, MAX_CA, root=True) as (handle, prove):
            data = handle.read(MAX_CA + 1)
            require(len(data) == os.fstat(handle.fileno()).st_size, "artifact")
            prove()
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
            context.minimum_version = ssl.TLSVersion.TLSv1_2
            # Native parsing of each block makes a malformed certificate fatal
            # even when another certificate in the same bundle is valid.
            for block in certificates(data):
                context.load_verify_locations(cadata=block)
            require(context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname and context.cert_store_stats()["x509_ca"] > 0, "artifact")
            yield context, prove
            prove()
    except DiscoveryRefusal:
        raise
    except (OSError, ValueError, UnicodeError):
        raise DiscoveryRefusal("artifact") from None


def bundle_blocks(data):
    """The registry trust bundle's certificate blocks; hosts receive exactly their concatenation, never the comments around them."""
    try:
        blocks = certificates(data)
    except DiscoveryRefusal:
        raise DiscoveryRefusal("material") from None
    require(len("".join(blocks)) <= ceph_pull_broker.MAX_TRUST, "material")
    return blocks


@contextlib.contextmanager
def bundle_proof(path):
    """Only the bound registry trust bundle anchors verification; the host trust path is never opened."""
    try:
        with protected_file(path, ceph_pull_broker.MAX_TRUST, private=True) as (handle, prove):
            data = handle.read(ceph_pull_broker.MAX_TRUST + 1)
            require(len(data) == os.fstat(handle.fileno()).st_size, "material")
            prove()
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
            context.minimum_version = ssl.TLSVersion.TLSv1_2
            for block in bundle_blocks(data):
                context.load_verify_locations(cadata=block)
            require(context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname and context.cert_store_stats()["x509_ca"] > 0, "material")
            yield context, prove
            prove()
    except DiscoveryRefusal as error:
        if str(error) == "artifact":
            raise DiscoveryRefusal("material") from None
        raise
    except (OSError, ValueError, UnicodeError):
        raise DiscoveryRefusal("material") from None


@contextlib.contextmanager
def bounded_lifetime(seconds=TOTAL_SECONDS):
    """The dedicated module owns a finite budget including DNS and TLS work."""
    require(type(seconds) in (int, float) and 0 < seconds <= TOTAL_SECONDS, "timeout")
    signals = (signal.SIGALRM, signal.SIGTERM, signal.SIGHUP, signal.SIGINT)
    handlers = {number: signal.getsignal(number) for number in signals}
    require(signal.getitimer(signal.ITIMER_REAL) == (0.0, 0.0), "timeout")

    def interrupted(_number, _frame):
        raise DiscoveryRefusal("timeout")

    try:
        for number in signals:
            signal.signal(number, interrupted)
        signal.setitimer(signal.ITIMER_REAL, seconds)
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        for number, handler in handlers.items():
            signal.signal(number, handler)


def challenge(value, registry, namespace):
    scope = "repository:" + namespace + ":pull"
    require(isinstance(value, str) and len(value) <= 4096 and value.startswith("Bearer "), "discovery")
    remaining, parameters = value[7:], {}
    pattern = re.compile(r'([A-Za-z_]+)="([^"\\\x00-\x1f\x7f]*)"(?:,\s*|$)')
    while remaining:
        match = pattern.match(remaining)
        require(match is not None and match[1] not in parameters, "discovery")
        parameters[match[1]] = match[2]
        remaining = remaining[match.end() :]
    require(
        {"realm", "service"} <= set(parameters) <= {"realm", "service", "scope"}
        and bool(parameters["service"])
        and parameters.get("scope", scope) == scope,
        "discovery",
    )
    realm = urllib.parse.urlsplit(parameters["realm"])
    require(realm.scheme == "https" and realm.netloc == registry and realm.username is None and realm.password is None
            and realm.path.startswith("/") and not realm.query and not realm.fragment, "discovery")
    return realm.path + "?" + urllib.parse.urlencode({"service": parameters["service"], "scope": scope})


def token(raw):
    # Exact JSON numbers exclude bool and float at this boundary.
    # pylint: disable=unidiomatic-typecheck
    value = decode(raw, "discovery")
    require(isinstance(value, dict) and set(value) <= {"token", "access_token", "expires_in", "issued_at"}, "discovery")
    selected = value.get("token", value.get("access_token"))
    require(isinstance(selected, str) and 0 < len(selected) <= MAX_TOKEN and re.fullmatch(r"[A-Za-z0-9._~+/=-]+", selected) is not None, "discovery")
    require("token" not in value or "access_token" not in value or value["token"] == value["access_token"], "discovery")
    require("expires_in" not in value or type(value["expires_in"]) is int and 0 < value["expires_in"] <= 86400, "discovery")
    return "Bearer " + selected


def delivery_answers(host):
    rows = socket.getaddrinfo(host, 443, type=socket.SOCK_STREAM, proto=socket.IPPROTO_TCP)
    return json.dumps([[family, kind, protocol, list(address)] for family, kind, protocol, unused_name, address in rows]).encode()


class Registry:
    """No generic URL, proxy or arbitrary request method constructor; one credential-free delivery hop."""

    def __init__(self, context, request, now=time.monotonic):
        self.request = validate_request(request)
        self.registry = request["registry"]
        self.namespace = request["base"].split("/", 1)[1]
        self.token_path = None
        self.context, self.now = context, now
        self.deadline = now() + TOTAL_SECONDS
        self.resolver, self.delivery = delivery_answers, ceph_pull_broker.RegistryConnection
        self.anchor = ceph_pull_broker.anchor(request)

    def untrusted(self, error, cause):
        if getattr(error, "untrusted", False):
            error.native_error = native_diagnostics.error_detail(self.request["image"], cause + " failed against " + self.anchor)

    def get(self, path, authorization=None, maximum=MAX_BODY):
        # Exact byte budgets exclude bool and float.
        # pylint: disable=unidiomatic-typecheck
        require(isinstance(path, str), "discovery")
        manifest = "/v2/" + self.namespace + "/manifests/"
        allowed = (path == manifest + urllib.parse.quote(self.request["tag"], safe=":")
                   or path.startswith(manifest + "sha256:") and ID.fullmatch(path[len(manifest + "sha256:") :]) is not None)
        token_path = self.token_path
        blob = "/v2/" + self.namespace + "/blobs/sha256:"
        require(allowed or path.startswith(blob) and digest(path[len(blob):]) or path == token_path, "discovery")
        require(
            authorization is None
            or isinstance(authorization, str)
            and len(authorization) <= 65536
            and not any(character in authorization for character in "\r\n\x00"),
            "material",
        )
        require(authorization is None or authorization.startswith("Basic " if path == token_path else "Bearer "), "material")
        require(type(maximum) is int and 0 < maximum <= MAX_BODY, "discovery")
        remaining = self.deadline - self.now()
        require(remaining > 0, "timeout")
        connection = http.client.HTTPSConnection(self.registry, timeout=min(remaining, 30), context=self.context)
        headers = {
            "Accept": ACCEPT if path.startswith(manifest) else "application/json",
            "Accept-Encoding": "identity",
            "User-Agent": "bootwright-ceph-metadata-v1",
        }
        if authorization is not None:
            headers["Authorization"] = authorization
        try:
            return self.exchange(connection, path, headers, maximum)
        except DiscoveryRefusal as error:
            self.untrusted(error, "TLS verification of the registry " + self.registry)
            raise

    def deliver(self, values):
        """Follow one validated Location to public addresses only; its value never enters a refusal."""
        remaining = self.deadline - self.now()
        require(remaining > 0, "timeout")
        try:
            host, path = ceph_pull_broker.delivery_location(types.SimpleNamespace(getheaders=lambda: list(values.items())))
            addresses = ceph_pull_broker.delivery_addresses(self.resolver(host))
        except (ValueError, TypeError, OSError):
            raise DiscoveryRefusal("discovery") from None
        connection = self.delivery(host, addresses=addresses, deadline=time.monotonic() + remaining, port=443,
                                   context=self.context, timeout=min(remaining, 30))
        headers = {"Accept-Encoding": "identity", "User-Agent": "bootwright-ceph-metadata-v1"}
        try:
            status, unused_values, raw = self.exchange(connection, path, headers, MAX_BODY)
        except DiscoveryRefusal as error:
            self.untrusted(error, "TLS verification of the registry's configuration blob delivery endpoint")
            raise
        return status, raw

    def exchange(self, connection, path, headers, maximum):
        try:
            connection.request("GET", path, headers=headers)
            response = connection.getresponse()
            rows = response.getheaders()
            require(len(rows) <= 32 and sum(len(name) + len(value) for name, value in rows) <= 8192, "discovery")
            values = {}
            for name, value in rows:
                key = name.lower()
                require(key not in values, "discovery")
                values[key] = value
            require(values.get("content-encoding", "identity") == "identity", "discovery")
            length = values.get("content-length")
            require(length is None or re.fullmatch(r"0|[1-9][0-9]{0,9}", length) is not None and int(length) <= maximum, "discovery")
            raw = response.read(maximum + 1)
            require(len(raw) <= maximum and (length is None or len(raw) == int(length)), "discovery")
            require(self.now() < self.deadline, "timeout")
            return response.status, values, raw
        except ssl.SSLCertVerificationError:
            error = DiscoveryRefusal("discovery")
            error.untrusted = True
            raise error from None
        except (OSError, http.client.HTTPException):
            raise DiscoveryRefusal("discovery") from None
        finally:
            connection.close()


def verified(raw, expected, size=None):
    require(isinstance(expected, str) and expected.startswith("sha256:") and digest(expected[7:]), "discovery")
    require("sha256:" + hashlib.sha256(raw).hexdigest() == expected and (size is None or len(raw) == size), "discovery")
    return decode(raw, "discovery")


def descriptor(value, media_types):
    # Exact JSON sizes exclude bool and float.
    # pylint: disable=unidiomatic-typecheck
    require(
        isinstance(value, dict)
        and {"mediaType", "digest", "size"} <= set(value)
        and set(value) <= {"mediaType", "digest", "size", "platform", "annotations", "urls", "data", "artifactType"},
        "discovery",
    )
    require(
        value["mediaType"] in media_types and isinstance(value["digest"], str) and value["digest"].startswith("sha256:") and digest(value["digest"][7:]),
        "discovery",
    )
    require(type(value["size"]) is int and 0 < value["size"] <= MAX_BODY, "discovery")
    optional_descriptor(value)
    return value


def optional_descriptor(value):
    """Standard hints are bounded data; none supplies a URL or artifact authority."""
    for name in ("data", "artifactType"):
        require(name not in value or isinstance(value[name], str) and len(value[name]) <= MAX_BODY, "discovery")
    if "annotations" in value:
        require(
            isinstance(value["annotations"], dict)
            and len(value["annotations"]) <= 128
            and all(isinstance(key, str) and isinstance(item, str) for key, item in value["annotations"].items()),
            "discovery",
        )
    if "urls" in value:
        require(
            isinstance(value["urls"], list) and len(value["urls"]) <= 16 and all(isinstance(item, str) and len(item) <= 4096 for item in value["urls"]),
            "discovery",
        )


def image_layers(value):
    # Exact JSON sizes exclude bool and float.
    # pylint: disable=unidiomatic-typecheck
    require(isinstance(value, list) and len(value) <= 256, "discovery")
    for row in value:
        require(
            isinstance(row, dict)
            and {"mediaType", "digest", "size"} <= set(row)
            and set(row) <= {"mediaType", "digest", "size", "annotations", "urls", "data", "artifactType"},
            "discovery",
        )
        require(
            row["mediaType"] in LAYER_TYPES and isinstance(row["digest"], str) and row["digest"].startswith("sha256:") and digest(row["digest"][7:]),
            "discovery",
        )
        require(type(row["size"]) is int and 0 <= row["size"] <= 16 << 30, "discovery")
        optional_descriptor(row)


def select(index):
    # Schema version is an exact JSON integer, never bool or float.
    # pylint: disable=unidiomatic-typecheck
    require(
        isinstance(index, dict)
        and type(index.get("schemaVersion")) is int
        and index["schemaVersion"] == 2
        and ("mediaType" not in index or index["mediaType"] in INDEX_TYPES),
        "discovery",
    )
    rows = index.get("manifests")
    require(isinstance(rows, list) and 0 < len(rows) <= 128, "discovery")
    selected = []
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get("platform"), dict), "discovery")
        platform = row["platform"]
        if platform.get("os") == "linux" and platform.get("architecture") == "amd64":
            require(set(platform) <= {"os", "architecture", "variant", "os.version", "os.features"} and platform.get("variant", "") == "", "discovery")
            require("os.version" not in platform or isinstance(platform["os.version"], str), "discovery")
            require(
                "os.features" not in platform or isinstance(platform["os.features"], list) and all(isinstance(item, str) for item in platform["os.features"]),
                "discovery",
            )
            selected.append(descriptor(row, MANIFEST_TYPES))
    require(len(selected) == 1, "discovery")
    return selected[0]


def http_success(status, raw, coordinate, secrets=()):
    if status != 200:
        private = list(secrets)
        for authorization in secrets:
            if isinstance(authorization, str) and authorization.startswith("Basic "):
                private.append(authorization[6:])
                try:
                    private.append(base64.b64decode(authorization[6:], validate=True).decode("utf-8").split(":", 1)[-1])
                except (ValueError, UnicodeError):
                    pass
            elif isinstance(authorization, str) and authorization.startswith("Bearer "):
                private.append(authorization[7:])
        error = DiscoveryRefusal("discovery")
        error.native_error = native_diagnostics.error_detail(coordinate, "HTTP " + str(status) + ": " + raw.decode("utf-8", errors="replace"), secrets=private)
        raise error


def successful(client, path, authorization, expected=None, size=None):
    status, headers, raw = client.get(path, authorization)
    http_success(status, raw, path, (authorization,) if authorization else ())
    actual = "sha256:" + hashlib.sha256(raw).hexdigest()
    require("docker-content-digest" not in headers or headers["docker-content-digest"] == actual, "discovery")
    return verified(raw, actual if expected is None else expected, size), headers


def configuration_blob(client, namespace, config, authorization):
    """A registry may delegate the config blob once; delivery bytes still match the descriptor."""
    path = "/v2/" + namespace + "/blobs/" + config["digest"]
    status, headers, raw = client.get(path, authorization)
    if status not in ceph_pull_broker.DELIVERY_REDIRECTS:
        http_success(status, raw, path, (authorization,) if authorization else ())
        require("docker-content-digest" not in headers or headers["docker-content-digest"] == "sha256:" + hashlib.sha256(raw).hexdigest(), "discovery")
        return verified(raw, config["digest"], config["size"])
    status, raw = client.deliver(headers)
    if status != 200:
        error = DiscoveryRefusal("discovery")
        error.native_error = native_diagnostics.error_detail(path, "config delivery answered HTTP " + str(int(status)))
        raise error
    return verified(raw, config["digest"], config["size"])


def resolve(basic, client, request):
    """Resolve the authored tag or digest without consulting another coordinate."""
    # Schema version is an exact JSON integer, never bool or float.
    # pylint: disable=unidiomatic-typecheck
    namespace = request["base"].split("/", 1)[1]
    selected_reference = authored_digest(request) or request["tag"]
    path = "/v2/" + namespace + "/manifests/" + urllib.parse.quote(selected_reference, safe=":")
    status, headers, raw = client.get(path)
    authorization = None
    if status == 401:
        token_path = challenge(headers.get("www-authenticate"), request["registry"], namespace)
        client.token_path = token_path
        status, unused_headers, raw = client.get(token_path, basic, MAX_TOKEN * 2)
        http_success(status, raw, request["image"], (basic,))
        authorization = token(raw)
        status, headers, raw = client.get(path, authorization)
    http_success(status, raw, request["image"], (basic, authorization) if authorization else (basic,))
    tag_digest = "sha256:" + hashlib.sha256(raw).hexdigest()
    require("docker-content-digest" not in headers or headers["docker-content-digest"] == tag_digest, "discovery")
    document = verified(raw, authored_digest(request) or tag_digest)
    media = headers.get("content-type", "").split(";", 1)[0]
    require(media in INDEX_TYPES + MANIFEST_TYPES and isinstance(document, dict) and document.get("mediaType", media) == media, "discovery")
    if media in INDEX_TYPES:
        selected = select(document)
        manifest, headers = successful(client, "/v2/" + namespace + "/manifests/" + selected["digest"], authorization, selected["digest"], selected["size"])
        require(headers.get("content-type", "").split(";", 1)[0] == selected["mediaType"], "discovery")
    else:
        selected = {"digest": tag_digest, "mediaType": media}
        manifest = document
    require(
        isinstance(manifest, dict)
        and type(manifest.get("schemaVersion")) is int
        and manifest["schemaVersion"] == 2
        and manifest.get("mediaType", selected["mediaType"]) == selected["mediaType"],
        "discovery",
    )
    image_layers(manifest.get("layers"))
    config = descriptor(manifest.get("config"), CONFIG_TYPES)
    if media in MANIFEST_TYPES:
        configuration = configuration_blob(client, namespace, config, authorization)
        require(isinstance(configuration, dict) and configuration.get("os") == "linux" and configuration.get("architecture") == "amd64", "discovery")
    return selected["digest"]


def resolution(request, request_digest, child, auxiliary=()):
    """The presence of one resolution, refusing an authored index digest of the image or of an auxiliary image."""
    authored = authored_digest(request)
    require(authored is None or authored == child, "image-digest-mismatch")
    value = {"request": request_digest, "postcondition": True, "release": request["release"], "base": request["base"],
             "tag": request["tag"], "digest": child, "platform": PLATFORM}
    entries = request.get("auxiliary", [])
    require(len(auxiliary) == len(entries), "discovery")
    for entry, resolved in zip(entries, auxiliary):
        authored = authored_digest(entry)
        require(authored is None or authored == resolved, "image-digest-mismatch")
    if entries:
        value["auxiliary"] = [{"name": entry["name"], "base": entry["base"], "tag": entry["tag"], "digest": resolved, "platform": PLATFORM}
                              for entry, resolved in zip(entries, auxiliary)]
    return evidence(value, request_digest)


def execute(operation, request, digest_value, material):
    require(operation in OPERATIONS)
    original = validate_request(request)
    require(isinstance(digest_value, str) and digest(digest_value))
    expected = {"registryJSON"} | ({"registryCA"} if "registryTrustBundleRef" in request else set())
    require(isinstance(material, dict) and (set(material) == expected if operation == "apply" else not material), "material")
    if operation != "apply":
        return {"changed": False, "evidence": evidence({"request": digest_value, "postcondition": True, "absent": True}, digest_value)}
    trust = bundle_proof(material["registryCA"]) if "registryCA" in material else tls_proof()
    with bounded_lifetime(), trust as (context, prove_tls):
        with protected_file(material["registryJSON"], MAX_REGISTRY, private=True) as (loan, prove):
            basic = basic_material(loan.read(MAX_REGISTRY + 1), request["registry"])
            prove()
            prove_tls()
            child = resolve(basic, Registry(context, request), request)
            prove()
            prove_tls()
            auxiliary = []
            for entry in request.get("auxiliary", []):
                sub = auxiliary_request(request, entry)
                auxiliary.append(resolve(basic, Registry(context, sub), sub))
                prove()
                prove_tls()
    return {"changed": False, "evidence": resolution(original, digest_value, child, auxiliary)}
