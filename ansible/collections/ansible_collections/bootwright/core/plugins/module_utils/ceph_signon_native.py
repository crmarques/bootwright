"""Fixed Ceph management sign-on hooks, shipped beside the core worker as source.

A CephSignOn block runs one service unit of type oauth2-proxy: its one oauth2-proxy document, which cephadm names `oauth2-proxy`
and whose daemons run the frozen oauth2_proxy image. cephadm turns the management gateway's sign-on on exactly while an oauth2-proxy
daemon exists (src/pybind/mgr/cephadm/services/mgmt_gateway.py, `enable_oauth2_proxy`), and reconfigures the gateway, Grafana,
Prometheus and Alertmanager itself, so the gateway's own unit never changes. The proxy's credentials, its TLS pair and the
OIDCProvider's trust bundle, which only the document's one `custom_configs` entry carries, are bound leaves the generic unit worker
splices into the apply stream only.

Before any effect each proxy host reads the issuer's discovery document over TLS verified with the provider's bundle alone, or the
host's system store without one, and proves its `issuer` is the frozen issuer URL: oauth2-proxy discovers its issuer when it starts
and exits when it cannot (upstream providers/oidc). The container adds the bundle to its image's own store, so the host-side check
approximates the container's trust; the readiness proof that every proxy daemon kept running for STABLE_SECONDS closes the gap.

After the apply each proxy host proves, read-only, that no running proxy skips the identity provider's certificate verification in its
arguments, environment or mounted `--config` file (src/cephadm/cephadmlib/daemons/oauth2_proxy.py:78-83, 138-151); then the bootstrap host
runs `dashboard sso enable oauth2` once, which sets the dashboard option sso_oauth2 (src/pybind/mgr/dashboard/services/sso.py).
Readiness reads that option and proves the gateway enforces sign-on: an unauthenticated `GET /` of the gateway answers what the image's
templates make it answer, nginx's `auth_request /oauth2/auth` 401 turned by `error_page 401 = /oauth2/sign_in` into oauth2-proxy's
sign-in, which with `skip_provider_button= true` (templates/services/oauth2-proxy/oauth2-proxy.conf.j2) is a 302 to the issuer's
authorization endpoint (templates/services/mgmt-gateway/external_server.conf.j2, `location /`). Any other answer is pending, never a
pass. Like the core worker it imports only the target Python standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import hmac
import ipaddress
import json
import re
import socket
import ssl
import time
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nfs, ceph_native_observability
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, named, require

TYPE = "oauth2-proxy"
DAEMON_TYPES = (TYPE,)
IMAGES = {TYPE: "oauth2_proxy"}
# Each bound leaf: (service type, JSON pointer, runner material variable). The CA bundle leaf is the OIDCProvider's trust bundle.
LEAVES = (
    (TYPE, "/custom_configs/0/content", "oauth2ProxyCABundle"),
    (TYPE, "/spec/client_secret", "oauth2ProxyClientSecret"),
    (TYPE, "/spec/cookie_secret", "oauth2ProxyCookieSecret"),
    (TYPE, "/spec/ssl_cert", "oauth2ProxyCertificate"),
    (TYPE, "/spec/ssl_key", "oauth2ProxyKey"),
)
CA_VARIABLE = "oauth2ProxyCABundle"
# Where the OIDCProvider's trust bundle is mounted into the container: a file in a directory Go's system trust store reads, as the Go
# planner's cephcluster.OAuth2ProxyCABundlePath names it.
CA_PATH = "/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem"
MEMBERS = frozenset(("allowlist_domains", "client_id", "https_address", "oidc_issuer_url", "provider_display_name", "redirect_url"))
DOCUMENT_MEMBERS = frozenset(("custom_configs", "networks", "placement", "service_type", "spec"))
TARGET_MEMBERS = frozenset(("certificateSHA256", "count", "endpoint", "grafana", "hosts", "port", "virtualIP"))
PORT = ":4180"
SSO_OPTION = "mgr/dashboard/sso_oauth2"
# The deployed proxy's verification switch as oauth2-proxy reads it: a flag, its environment form, and its configuration key.
INSECURE_FLAG = "--ssl-insecure-skip-verify"
INSECURE_ENVIRONMENT = "OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY"
INSECURE_KEY = "ssl_insecure_skip_verify"
# A proxy daemon proves it did not exit on discovery once cephadm's last refresh saw it running this long after it started.
STABLE_SECONDS = 30
# The discovery read: one connection, each step within CONNECT_TIMEOUT, the whole exchange within DISCOVERY_TIMEOUT, at most
# DISCOVERY_LIMIT bytes; no proxy, no redirect.
CONNECT_TIMEOUT = 5
DISCOVERY_TIMEOUT = 15
DISCOVERY_LIMIT = 65536
DISCOVERY_PATH = "/.well-known/openid-configuration"
# The enforcement read: the status line and headers of one unauthenticated request, at most ENFORCE_LIMIT bytes.
ENFORCE_TIMEOUT = 5
ENFORCE_LIMIT = 8192
ENFORCED_STATUS = 302
STATUS = re.compile(rb"HTTP/1\.[01] ([1-5][0-9]{2})(?: [^\r\n]*)?\Z")
MAX_TEXT = 256
MAX_ISSUER = 2048
DIGEST = re.compile(r"[0-9a-f]{64}\Z")
DNS_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")


def text(value, limit=MAX_TEXT):
    return isinstance(value, str) and 0 < len(value.encode("utf-8", "surrogatepass")) <= limit


def document_of(unit):
    """The unit's one oauth2-proxy document."""
    return unit["documents"][0]


def issuer_of(unit):
    return document_of(unit)["spec"]["oidc_issuer_url"]


def oauth2_spec(spec):
    """The oauth2-proxy spec within its closed realized record: never a verification switch, a scope or an email domain."""
    require(isinstance(spec, dict) and set(spec) <= MEMBERS)
    require(all(text(spec.get(name)) for name in ("provider_display_name", "client_id")) and text(spec.get("oidc_issuer_url"), MAX_ISSUER))
    require(all(name not in spec or spec[name].startswith("https://") for name in ("oidc_issuer_url", "redirect_url")))
    require("redirect_url" not in spec or text(spec["redirect_url"], MAX_ISSUER))
    require("https_address" not in spec or text(spec["https_address"]) and spec["https_address"].endswith(PORT))
    domains = spec.get("allowlist_domains", ["-"])
    require(isinstance(domains, list) and domains and all(text(domain) for domain in domains))


def canonical_address(value):
    try:
        return isinstance(value, str) and str(ipaddress.ip_address(value)) == value
    except ValueError:
        return False


def target(value, hosts):
    """The gateway the enforcement proof reads, as Go froze it and pinned its certificate: its hosts, port, virtual IP and DNS name."""
    require(isinstance(value, dict) and {"certificateSHA256", "count", "hosts", "port"} <= set(value) <= TARGET_MEMBERS)
    require(type(value["count"]) is int and 1 <= value["count"] <= len(value["hosts"]) if isinstance(value.get("hosts"), list) else False)
    require(type(value.get("grafana", False)) is bool)
    require(isinstance(value["certificateSHA256"], str) and DIGEST.fullmatch(value["certificateSHA256"]) is not None)
    names = [host["name"] for host in hosts]
    listed = value["hosts"]
    require(isinstance(listed, list) and bool(listed) and listed == sorted(set(listed)) and all(named(host) and host in names for host in listed))
    require(ceph_native_nfs.port(value["port"]))
    require("virtualIP" not in value or canonical_address(value["virtualIP"]))
    endpoint = value.get("endpoint")
    require(endpoint is None or isinstance(endpoint, str) and 0 < len(endpoint) <= 253 and all(DNS_LABEL.fullmatch(part) for part in endpoint.split(".")))


def bindings(unit, document):
    """Both credentials, the TLS pair together or not at all, and the CA bundle exactly with the custom_configs entry."""
    bound = {binding["path"] for binding in unit.get("bindings", []) if binding["serviceRef"] == TYPE}
    require({"/spec/client_secret", "/spec/cookie_secret"} <= bound)
    require(("/spec/ssl_cert" in bound) == ("/spec/ssl_key" in bound))
    require(("/custom_configs/0/content" in bound) == ("custom_configs" in document))


def validate(unit, request, hosts):
    """The one oauth2-proxy document, its CA mount exactly with the provider's bundle, bound as bindings requires, and its gateway target."""
    require(unit["id"] == "" and len(unit["documents"]) == 1)
    document = document_of(unit)
    require(document["service_type"] == TYPE and "service_id" not in document and set(document) <= DOCUMENT_MEMBERS)
    require(document.get("custom_configs", [{"mount_path": CA_PATH}]) == [{"mount_path": CA_PATH}])
    oauth2_spec(document.get("spec"))
    if "networks" in document:
        ceph_native_nfs.networks(document["networks"])
    bindings(unit, document)
    target(unit.get("signOn"), hosts)
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(IMAGES[TYPE] in frozen)


def check(worker, unit):
    """Read-only, before any write: the settings block holds the frozen oauth2_proxy image setting."""
    del unit
    ceph_native_observability.check_images(worker, [IMAGES[TYPE]])


def claim(worker, unit, checked):
    """A sign-on claims nothing before its apply."""
    del worker, unit, checked


def settle(worker, unit):
    """A sign-on settles nothing after its apply."""
    del worker, unit


def expected(unit):
    """The oauth2-proxy daemons the one document places."""
    placement = document_of(unit)["placement"]
    return {(TYPE, TYPE): placement.get("count", len(placement["hosts"]))}


def frontend(unit):
    """A sign-on has no ingress."""
    del unit


def sso_enabled(worker):
    """Read-only: the dashboard module's sso_oauth2 option, which `dashboard sso enable oauth2` sets; a JSON boolean or its text."""
    value = worker.ceph("config", "get", "mgr", SSO_OPTION)
    if type(value) is bool:
        return value
    require(isinstance(value, str) and value.lower() in ("true", "false"), "native-output")
    return value.lower() == "true"


def stable(rows):
    """Whether each running proxy daemon ran STABLE_SECONDS before cephadm's last refresh of it, on cephadm's own clock."""
    running = [row for row in rows if row.get("service_name") == TYPE and row.get("daemon_type") == TYPE]
    for row in running:
        started = ceph_native_observability.started_ns(row.get("started"))
        refreshed = ceph_native_observability.started_ns(row.get("last_refresh"))
        if type(row.get("status")) is not int or row["status"] != 1 or started is None or refreshed is None:
            return False
        if refreshed - started < STABLE_SECONDS * 1000000000:
            return False
    return bool(running)


def readiness(worker, unit, blocking):
    """Beyond the daemons on the frozen image: each stayed running past its issuer discovery, and the dashboard's OAuth2 single sign-on is
    enabled. It never writes."""
    del unit
    ready = True
    if not stable(worker.unit_rows):
        ready = False
        if "stable@" + TYPE not in blocking:
            blocking.append("stable@" + TYPE)
    if sso_enabled(worker):
        worker.sso_protocol = "oauth2"
    else:
        ready = False
        if "sso@" + TYPE not in blocking:
            blocking.append("sso@" + TYPE)
    return ready


def origin(url):
    """(scheme, lower-case host, port) of an absolute URL; None for any other value."""
    try:
        parts = urllib.parse.urlsplit(url)
        port = parts.port
    except ValueError:
        return None
    if parts.scheme not in ("http", "https") or not parts.hostname:
        return None
    return parts.scheme, parts.hostname.lower(), port or (443 if parts.scheme == "https" else 80)


def answer(data):
    """(status, Location) of a response's status line and headers; None when they are not one well-formed HTTP head."""
    head, separator, unused = data.partition(b"\r\n\r\n")
    if not separator:
        return None
    lines = head.split(b"\r\n")
    found = STATUS.fullmatch(lines[0])
    if found is None:
        return None
    locations = []
    for line in lines[1:]:
        name, colon, value = line.partition(b":")
        if not colon:
            return None
        if name.strip().lower() == b"location":
            locations.append(value.strip().decode("ascii", "replace"))
    return int(found.group(1)), (locations[0] if len(locations) == 1 else None)


def enforced(status, location, issuer):
    """The one answer the pinned image's gateway and oauth2-proxy give an unauthenticated `GET /` when sign-on is enforced: a 302 to an
    https URL on the issuer's origin, its authorization endpoint."""
    wanted = origin(issuer)
    return status == ENFORCED_STATUS and location is not None and wanted is not None and wanted[0] == "https" and origin(location) == wanted


def read_head(channel, deadline):
    data = b""
    while len(data) < ENFORCE_LIMIT and b"\r\n\r\n" not in data:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return None
        channel.settimeout(remaining)
        block = channel.recv(ENFORCE_LIMIT - len(data))
        if not block:
            break
        data += block
    return data


def probe(address, number, name, host, pinned, issuer):
    """One unauthenticated `GET /` of a gateway endpoint without a cookie or an authorization header: "tls" unless it serves the leaf whose
    SHA-256 is pinned, "enforced" for the enforced answer, else "pending". No byte of the answer leaves this function."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    deadline = time.monotonic() + ENFORCE_TIMEOUT
    try:
        with socket.create_connection((address, number), timeout=ENFORCE_TIMEOUT) as raw:
            with context.wrap_socket(raw, server_hostname=name) as channel:
                presented = channel.getpeercert(binary_form=True)
                if not isinstance(presented, bytes) or not hmac.compare_digest(hashlib.sha256(presented).hexdigest(), pinned):
                    return "tls"
                channel.sendall(b"GET / HTTP/1.1\r\nHost: " + host.encode("ascii") + b"\r\nAccept: text/html\r\nConnection: close\r\n\r\n")
                data = read_head(channel, deadline)
    except (OSError, ValueError):
        return "pending"
    parsed = answer(data) if data is not None else None
    return "enforced" if parsed is not None and enforced(parsed[0], parsed[1], issuer) else "pending"


def gateway_targets(worker, gateway):
    """(address, TLS name, Host header) of each gateway endpoint, as the gateway's own endpoint proof reads them: the virtual IP, else
    every running mgmt-gateway daemon in host name order at its own address; a daemon on no frozen gateway host is not this gateway's."""
    vip = gateway.get("virtualIP")
    if vip is not None:
        return [(vip, vip, "[" + vip + "]" if ipaddress.ip_address(vip).version == 6 else vip)]
    with worker.manager_served():
        rows = worker.ceph("orch", "ps", "--daemon_type", "mgmt-gateway")
    require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
    running = sorted((row for row in rows if row.get("daemon_type") == "mgmt-gateway" and type(row.get("status")) is int and row["status"] == 1
                      and row.get("hostname") in gateway["hosts"]), key=lambda row: row["hostname"])
    return [(worker.daemon_address(row, gateway["port"]), row["hostname"], row["hostname"]) for row in running]


def endpoint(worker, unit, running):
    """Read-only, on the bootstrap host: every gateway endpoint, the virtual IP or each running gateway daemon, answers an unauthenticated
    `GET /` with the enforced answer while serving the certificate the gateway's own proof pinned; the digest is that pin, only when the
    gateway's placed count of endpoints passed."""
    del running
    gateway = unit["signOn"]
    targets = gateway_targets(worker, gateway)
    wanted = 1 if gateway.get("virtualIP") is not None else gateway["count"]
    passed = 0
    for address, name, host in targets:
        verdict = "pending" if address is None else probe(address, gateway["port"], name, host, gateway["certificateSHA256"], issuer_of(unit))
        if verdict == "tls":
            worker.pending("tls@mgmt-gateway")
        elif verdict != "enforced":
            worker.pending("enforce@mgmt-gateway")
        else:
            passed += 1
    if len(targets) < wanted:
        worker.pending("enforce@mgmt-gateway")
    reachable = passed == len(targets) >= wanted
    return {"certificateSHA256": gateway["certificateSHA256"] if reachable else "", "reachable": reachable}


def discovery_context(bundle):
    """TLS verified with the provider's bundle as the only anchor, else the host's system store; hostnames always checked."""
    context = ssl.create_default_context(cadata=bundle) if bundle is not None else ssl.create_default_context()
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    return context


def discovered(data, issuer):
    """The stage a discovery answer fails at, or None for a 200 JSON document whose issuer is exactly the frozen one and whose
    authorization endpoint is an https URL."""
    head, separator, body = data.partition(b"\r\n\r\n")
    lines = head.split(b"\r\n")
    found = STATUS.fullmatch(lines[0]) if separator else None
    if found is None:
        return "content"
    if found.group(1) != b"200":
        return "status " + found.group(1).decode("ascii")
    types = [line.partition(b":")[2].strip().lower() for line in lines[1:] if line.partition(b":")[0].strip().lower() == b"content-type"]
    if len(types) != 1 or not types[0].startswith(b"application/json"):
        return "content"
    try:
        value = json.loads(body)
    except (ValueError, UnicodeError):
        return "content"
    found = origin(value.get("authorization_endpoint")) if isinstance(value, dict) and isinstance(value.get("authorization_endpoint"), str) else None
    if found is None or found[0] != "https":
        return "content"
    return None if value.get("issuer") == issuer else "issuer-mismatch"


def discover(issuer, bundle, *, connect=socket.create_connection):
    """The stage at which reading `<issuer>/.well-known/openid-configuration` fails: dns, connect, tls, status <n>, content, issuer-mismatch
    or oversize; None when the document proves the issuer. One HTTP/1.0 request, no proxy, no redirect followed."""
    parts = urllib.parse.urlsplit(issuer)
    host, port = parts.hostname, parts.port or 443
    request = ("GET " + parts.path + DISCOVERY_PATH + " HTTP/1.0\r\nHost: " + parts.netloc + "\r\nAccept: application/json\r\n\r\n").encode("ascii")
    deadline = time.monotonic() + DISCOVERY_TIMEOUT
    try:
        context = discovery_context(bundle)
    except (ssl.SSLError, ValueError):
        return "tls"
    try:
        raw = connect((host, port), timeout=CONNECT_TIMEOUT)
    except socket.gaierror:
        return "dns"
    except OSError:
        return "connect"
    data = b""
    with raw:
        try:
            with context.wrap_socket(raw, server_hostname=host) as channel:
                channel.sendall(request)
                while len(data) <= DISCOVERY_LIMIT:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        return "connect"
                    channel.settimeout(min(CONNECT_TIMEOUT, remaining))
                    block = channel.recv(DISCOVERY_LIMIT + 1 - len(data))
                    if not block:
                        break
                    data += block
        except (ssl.SSLError, ssl.CertificateError):
            return "tls"
        except OSError:
            return "connect"
    if len(data) > DISCOVERY_LIMIT:
        return "oversize"
    return discovered(data, issuer)


def discovery(worker):
    """Read-only, before any effect, on one proxy host: the issuer's discovery document proves the issuer, else the named refusal names
    the host, the stage and the issuer URL, never a byte of the bundle or the answer."""
    unit = worker.request["service"]
    require(unit["type"] == TYPE and worker.host["name"] in document_of(unit)["placement"]["hosts"])
    bundle = worker.bound.get(CA_VARIABLE) if isinstance(worker.bound, dict) else None
    stage = discover(issuer_of(unit), bundle)
    if stage is not None:
        raise NativeFailure("sign-on-provider-unreachable", worker.native_coordinate(), "host " + worker.host["name"] + ": " + stage + " " + issuer_of(unit))
    return {"changed": False}


def insecure_flag(args, environment):
    """Where the deployed proxy's arguments or environment skip certificate verification: "argument", "environment" or None."""
    for argument in args:
        if argument == INSECURE_FLAG or argument.startswith(INSECURE_FLAG + "=") and argument.partition("=")[2].lower() != "false":
            return "argument"
    for entry in environment:
        name, unused, value = entry.partition("=")
        if name == INSECURE_ENVIRONMENT and value.lower() != "false":
            return "environment"
    return None


def config_path(args):
    """The one configuration file the proxy's arguments name, else None."""
    found = []
    for index, argument in enumerate(args):
        if argument.startswith("--config="):
            found.append(argument[len("--config="):])
        elif argument == "--config":
            found.append(args[index + 1] if index + 1 < len(args) else "")
    return found[0] if len(found) == 1 and found[0] else None


def rendered_skip(value):
    """Whether a rendered oauth2-proxy configuration skips verification: "insecure", "verified" or "unproved". It reads the flat TOML
    subset cephadm renders, line by line, and returns no other line. A line that names the key in any spelling yet is not the bare key's
    assignment leaves it unproved."""
    found = []
    for line in value.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, separator, setting = line.partition("=")
        if INSECURE_KEY not in line.lower().replace("-", "_"):
            continue
        if not separator or key.strip() != INSECURE_KEY:
            return "unproved"
        setting = setting.strip()
        if len(setting) >= 2 and setting[0] == setting[-1] and setting[0] in "\"'":
            setting = setting[1:-1]
        found.append(setting.strip().lower())
    if not found:
        return "verified"
    if len(found) > 1 or found[0] not in ("true", "false"):
        return "unproved"
    return "insecure" if found[0] == "true" else "verified"


def proxy_verdict(fsid, inspected, bundle):
    """(verdict, where) for one deployed proxy container: "insecure" naming where it skips verification, "verified" when its arguments,
    environment and mounted configuration do not skip it and, with a bound CA bundle, its CA mount holds exactly that bundle, else
    "unproved". No path, byte, argument or environment value leaves this function."""
    args, environment, user, mounts = ceph_native_observability.proxy_container(inspected)
    where = insecure_flag(args, environment)
    if where is not None:
        return "insecure", where
    owners = ceph_native_observability.proxy_owners(user)
    path = config_path(args)
    sources = [source for destination, source in mounts if destination == path]
    rendered = ceph_native_observability.rendered_file(fsid, sources[0], owners) if path is not None and len(sources) == 1 else None
    try:
        verdict = "unproved" if rendered is None else rendered_skip(rendered.decode("utf-8"))
    except UnicodeError:
        verdict = "unproved"
    if verdict != "verified":
        return verdict, "configuration"
    if bundle is not None:
        sources = [source for destination, source in mounts if destination == CA_PATH]
        mounted = ceph_native_observability.rendered_file(fsid, sources[0], owners) if len(sources) == 1 else None
        if mounted is None or not hmac.compare_digest(mounted, bundle.encode("utf-8")):
            return "unproved", "CA bundle mount"
    return "verified", ""
