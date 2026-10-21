"""Fixed Ceph monitoring service-unit hooks, shipped beside the core worker as source.

A monitoring unit deploys every declared prometheus, alertmanager, grafana, node-exporter, ceph-exporter, snmp-gateway, loki,
alloy and promtail service as singleton documents in one `orch apply -i -` stream. cephadm names each such service by its type and each daemon
`<type>.<host short name>` (src/pybind/mgr/cephadm/module.py:922-966); every daemon runs its frozen auxiliary image, and
ceph-exporter the Ceph image. The hooks only read the frozen image settings, whose single writer is the cluster's
settings block. cephadm itself enables the prometheus manager module for a declared prometheus service
(src/pybind/mgr/cephadm/services/monitoring.py:480-489), so readiness observes it in `ceph mgr services`, which the MON
answers from the MgrMap (src/mon/MgrMonitor.cc:1085-1094); the hooks never enable a module or write a setting. The grafana
password and SNMP credentials are bound leaves the generic unit worker splices into the apply stream only.

The management gateway unit applies its keepalive-only ingress, when it has a virtual IP, before the singleton mgmt-gateway
document in the same stream, and its oauth2-proxy after it; their TLS leaves, the proxy's credentials and the identity
provider's CA bundle, which only the proxy's `custom_configs` entry carries, are bound the same way, and readiness probes the
served leaf from the bootstrap host. cephadm sets the dashboard's standby behaviour itself for a declared gateway
(src/pybind/mgr/cephadm/services/mgmt_gateway.py:49-52) and no cephadm code writes dashboard single sign-on, so the unit's own
`sso` phase runs `dashboard sso enable oauth2`, which sets the dashboard module option sso_oauth2
(src/pybind/mgr/dashboard/services/sso.py); readiness reads that option, never `dashboard sso status`, which answers EPERM
without python3-saml. Before that write the deployed proxy's arguments, environment and mounted `--config` file, which cephadm's
daemon form passes and mounts (src/cephadm/cephadmlib/daemons/oauth2_proxy.py:78-83, 138-151), must not skip the identity
provider's certificate verification on any host. The TLS probe here is shared with the object gateway worker. Like the core
worker it imports only the target Python standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import hmac
import ipaddress
import os
import re
import socket
import ssl
import stat
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nfs
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, require

MONITORING_TYPES = (
    "alertmanager",
    "alloy",
    "ceph-exporter",
    "grafana",
    "loki",
    "node-exporter",
    "prometheus",
    "promtail",
    "snmp-gateway",
)
DAEMON_TYPES = MONITORING_TYPES
# Each bound credential leaf: (service type, JSON pointer, runner material variable).
MONITORING_LEAVES = (
    ("grafana", "/spec/initial_admin_password", "grafanaAdminPassword"),
    ("snmp-gateway", "/spec/credentials/snmp_community", "snmpCommunity"),
    ("snmp-gateway", "/spec/credentials/snmp_v3_auth_password", "snmpAuthPassword"),
    ("snmp-gateway", "/spec/credentials/snmp_v3_auth_username", "snmpAuthUsername"),
    ("snmp-gateway", "/spec/credentials/snmp_v3_priv_password", "snmpPrivPassword"),
)
# Each daemon type's auxiliary image name; None runs the frozen Ceph image.
IMAGES = {"alertmanager": "alertmanager", "alloy": "alloy", "ceph-exporter": None, "grafana": "grafana", "loki": "loki", "node-exporter": "node_exporter",
          "prometheus": "prometheus", "promtail": "promtail", "snmp-gateway": "snmp_gateway"}
# The spec members each type realizes, as the Go planner's unitSpecMembers rows.
MEMBERS = {
    "alertmanager": frozenset(("only_bind_port_on_networks", "port", "user_data")),
    "alloy": frozenset(("port",)),
    "ceph-exporter": frozenset(("port", "prio_limit", "stats_period")),
    "grafana": frozenset(("anonymous_access", "only_bind_port_on_networks", "port", "protocol")),
    "loki": frozenset(("port",)),
    "node-exporter": frozenset(("port",)),
    "prometheus": frozenset(("only_bind_port_on_networks", "port", "retention_size", "retention_time")),
    "promtail": frozenset(("port",)),
    "snmp-gateway": frozenset(("auth_protocol", "engine_id", "port", "privacy_protocol", "snmp_destination", "snmp_version")),
}
# The types whose cephadm ServiceSpec has no networks field, as admission refuses them.
UNNETWORKED = frozenset(("ceph-exporter", "snmp-gateway"))
CHOICES = {"auth_protocol": ("MD5", "SHA"), "privacy_protocol": ("DES", "AES"), "protocol": ("http", "https"), "snmp_version": ("V2c", "V3")}
FLAGS = frozenset(("anonymous_access", "only_bind_port_on_networks"))
MAX_TEXT = 256
MAX_WEBHOOKS = 16
MAX_MANAGER_SERVICES = 64
MODULE = "prometheus"
GATEWAY_TYPES = (
    "mgmt-gateway",
    "oauth2-proxy",
)
# Each bound leaf of the management gateway and its oauth2-proxy: (service type, JSON pointer, runner material variable).
GATEWAY_LEAVES = (
    ("mgmt-gateway", "/spec/ssl_cert", "mgmtGatewayCertificate"),
    ("mgmt-gateway", "/spec/ssl_key", "mgmtGatewayKey"),
    ("oauth2-proxy", "/custom_configs/0/content", "oauth2ProxyCABundle"),
    ("oauth2-proxy", "/spec/client_secret", "oauth2ProxyClientSecret"),
    ("oauth2-proxy", "/spec/cookie_secret", "oauth2ProxyCookieSecret"),
    ("oauth2-proxy", "/spec/ssl_cert", "oauth2ProxyCertificate"),
    ("oauth2-proxy", "/spec/ssl_key", "oauth2ProxyKey"),
)
# The bound leaves observation and removal also hold: the certificate the endpoint proof compares.
OBSERVED_LEAVES = frozenset(("mgmtGatewayCertificate",))
GATEWAY_DAEMON_TYPES = ("mgmt-gateway", "keepalived", "oauth2-proxy")
GATEWAY_IMAGES = {"mgmt-gateway": "nginx", "keepalived": "keepalived", "oauth2-proxy": "oauth2_proxy"}
# Where the identity provider's CA bundle is mounted into the oauth2-proxy container: a file in a directory Go's system trust store
# reads, as the Go planner's storage.OAuth2ProxyCABundlePath names it.
OAUTH2_PROXY_CA_PATH = "/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem"
OAUTH2_MEMBERS = frozenset(("allowlist_domains", "client_id", "https_address", "oidc_issuer_url", "provider_display_name", "redirect_url"))
OAUTH2_PORT = ":4180"
SSO_OPTION = "mgr/dashboard/sso_oauth2"
# The deployed proxy's verification switch as oauth2-proxy reads it: a flag, its environment form, and its configuration key.
INSECURE_FLAG = "--ssl-insecure-skip-verify"
INSECURE_ENVIRONMENT = "OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY"
INSECURE_KEY = "ssl_insecure_skip_verify"
# Rendered files are read only beneath the cluster's data directory, bounded.
DATA_ROOT = "/var/lib/ceph"
MAX_RENDERED = 65536
GATEWAY_MEMBERS = frozenset(("enable_auth", "enable_health_check_endpoint", "port", "ssl", "ssl_ciphers", "ssl_protocols", "virtual_ip"))
GATEWAY_INGRESS_MEMBERS = frozenset(("backend_service", "first_virtual_router_id", "health_check_interval", "keepalive_only", "virtual_interface_networks",
                                     "virtual_ip"))
TLS_PROTOCOLS = ("TLSv1.2", "TLSv1.3")
MAX_CERTIFICATE = 65536
MAX_KEY = 16384
MAX_LEAF = 16384
CERTIFICATE = re.compile(r"-----BEGIN CERTIFICATE-----\r?\n.*?-----END CERTIFICATE-----", re.S)
PRIVATE_KEY = re.compile(r"^-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----\r?$", re.M)
PROBE_TIMEOUT = 5
PROBE_LIMIT = 256
PROBE_REQUEST = b"GET / HTTP/1.0\r\n\r\n"
STATUS_LINE = re.compile(rb"HTTP/1\.[01] [1-5][0-9]{2}(?: [^\r\n]*)?\r?\n")


def text(value):
    return isinstance(value, str) and 0 < len(value.encode("utf-8", "surrogatepass")) <= MAX_TEXT


def member(name, value):
    """One realized spec member's value, as admission's closed records bound it."""
    if name == "port":
        return ceph_native_nfs.port(value)
    if name in FLAGS:
        return type(value) is bool
    if name in CHOICES:
        return isinstance(value, str) and value in CHOICES[name]
    if name == "prio_limit":
        return type(value) is int and value >= 0
    if name == "stats_period":
        return type(value) is int and value >= 1
    if name == "user_data":
        urls = value.get("default_webhook_urls") if isinstance(value, dict) else None
        return (set(value) == {"default_webhook_urls"} and isinstance(urls, list) and 1 <= len(urls) <= MAX_WEBHOOKS
                and all(text(url) for url in urls))
    return text(value)


def needed_images(unit):
    """The auxiliary image names the unit's daemons run, in name order; ceph-exporter needs none."""
    return sorted({IMAGES[document["service_type"]] for document in unit["documents"]} - {None})


def validate(unit, request, hosts):
    """Singleton documents of distinct monitoring types in name order, each spec within its realized record."""
    del hosts
    documents = unit["documents"]
    require(unit["id"] == "" and 1 <= len(documents) <= len(MONITORING_TYPES))
    kinds = [document["service_type"] for document in documents]
    require(all(kind in MEMBERS for kind in kinds) and kinds == sorted(set(kinds)))
    for document in documents:
        require("service_id" not in document)
        spec = document.get("spec", {})
        require(isinstance(spec, dict) and set(spec) <= MEMBERS[document["service_type"]])
        require(all(member(name, value) for name, value in spec.items()))
        require(document["service_type"] != "snmp-gateway" or {"snmp_destination", "snmp_version"} <= set(spec))
        if "networks" in document:
            require(document["service_type"] not in UNNETWORKED)
            ceph_native_nfs.networks(document["networks"])
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(set(needed_images(unit)) <= frozen)


def check_images(worker, needed):
    """Read-only, before any write: the settings block holds the frozen image setting of each needed auxiliary image."""
    if needed:
        unused, held = worker.image_settings()
        missing = sorted(worker.image_key(name) for name in needed if worker.image_key(name) not in held)
        if missing:
            raise NativeFailure("foreign-image", worker.native_coordinate(), ", ".join(missing))


def check(worker, unit):
    """Read-only, before any write: the settings block holds the frozen image setting of every auxiliary image the unit runs."""
    check_images(worker, needed_images(unit))


def claim(worker, unit, checked):
    """The monitoring unit claims nothing before its apply."""
    del worker, unit, checked


def settle(worker, unit):
    """The monitoring unit settles nothing after its apply."""
    del worker, unit


def expected(unit):
    """Each singleton service with its daemon count; cephadm's daemon type is the service type."""
    return {(document["service_type"], document["service_type"]): document["placement"].get("count", len(document["placement"]["hosts"]))
            for document in unit["documents"]}


def readiness(worker, unit, blocking):
    """Read-only: with prometheus, an available manager whose map lists the prometheus module's endpoint; nothing enters evidence."""
    if not any(document["service_type"] == "prometheus" for document in unit["documents"]):
        return True
    manager = worker.ceph("mgr", "dump")
    require(isinstance(manager, dict) and type(manager.get("available")) is bool, "native-output")
    services = worker.ceph("mgr", "services")
    require(isinstance(services, dict) and len(services) <= MAX_MANAGER_SERVICES
            and all(isinstance(name, str) and isinstance(value, str) for name, value in services.items()), "native-output")
    if manager["available"] and services.get(MODULE):
        return True
    entry = "module@" + MODULE
    if entry not in blocking:
        blocking.append(entry)
    return False


def frontend(unit):
    """A monitoring unit has no ingress."""
    del unit


def leaf(certificate):
    """The DER bytes of a bound certificate's first block."""
    require(isinstance(certificate, str) and len(certificate.encode("utf-8")) <= MAX_CERTIFICATE, "material")
    found = CERTIFICATE.search(certificate)
    require(found is not None, "material")
    try:
        der = ssl.PEM_cert_to_DER_cert(found[0])
    except ValueError:
        raise Refusal("material") from None
    require(1 <= len(der) <= MAX_LEAF, "material")
    return der


def replied(connection, request, deadline):
    """Send one request and read at most PROBE_LIMIT bytes before the deadline: an HTTP status line, or nothing proved."""
    data = b""
    connection.sendall(request)
    while len(data) < PROBE_LIMIT and b"\n" not in data:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return False
        connection.settimeout(remaining)
        block = connection.recv(PROBE_LIMIT - len(data))
        if not block:
            break
        data += block
    return STATUS_LINE.match(data) is not None


def answers(address, number):
    """An HTTP status line from the frontend within the probe's bound; a bare connection proves nothing."""
    deadline = time.monotonic() + PROBE_TIMEOUT
    try:
        with socket.create_connection((address, number), timeout=PROBE_TIMEOUT) as connection:
            return replied(connection, PROBE_REQUEST, deadline)
    except OSError:
        return False


def served(address, number, name, *, host=None):
    """The DER leaf a TLS frontend serves for SNI name and whether an HTTP status line follows; trust is the caller's exact fingerprint.
    The Host header names the SNI name unless host is given."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    deadline, presented = time.monotonic() + PROBE_TIMEOUT, None
    try:
        with socket.create_connection((address, number), timeout=PROBE_TIMEOUT) as raw:
            with context.wrap_socket(raw, server_hostname=name) as channel:
                presented = channel.getpeercert(binary_form=True)
                header = (name if host is None else host).encode("ascii")
                return presented, replied(channel, b"GET / HTTP/1.0\r\nHost: " + header + b"\r\n\r\n", deadline)
    except (OSError, ValueError):
        return presented, False


def canonical_address(value):
    try:
        return isinstance(value, str) and str(ipaddress.ip_address(value)) == value
    except ValueError:
        return False


def gateway_documents(unit):
    """(ingress or None, mgmt-gateway document, oauth2-proxy document or None), each found by its service type."""
    found = {}
    for document in unit["documents"]:
        found.setdefault(document["service_type"], document)
    return found.get("ingress"), found.get("mgmt-gateway"), found.get("oauth2-proxy")


def gateway_needed(unit):
    """nginx, keepalived with the keepalive-only ingress, and oauth2_proxy with the proxy, in name order."""
    ingress, unused, proxy = gateway_documents(unit)
    return (["keepalived"] if ingress is not None else []) + ["nginx"] + (["oauth2_proxy"] if proxy is not None else [])


def gateway_spec(spec, proxied=False):
    """The mgmt-gateway spec within its realized record: TLS on, single sign-on exactly with the proxy, a port, closed protocol and cipher
    lists."""
    require(isinstance(spec, dict) and set(spec) <= GATEWAY_MEMBERS and ceph_native_nfs.port(spec.get("port")))
    require(spec.get("ssl", True) is True and spec.get("enable_auth", False) is proxied)
    require(type(spec.get("enable_health_check_endpoint", False)) is bool)
    if "ssl_protocols" in spec:
        protocols = spec["ssl_protocols"]
        require(isinstance(protocols, list) and protocols and all(isinstance(item, str) and item in TLS_PROTOCOLS for item in protocols))
        require(len(set(protocols)) == len(protocols))
    if "ssl_ciphers" in spec:
        ciphers = spec["ssl_ciphers"]
        require(isinstance(ciphers, list) and ciphers and all(isinstance(item, str) and item for item in ciphers))
    require("virtual_ip" not in spec or canonical_address(spec["virtual_ip"]))


def gateway_ingress(ingress, vip):
    """The keepalive-only ingress fronting the gateway, at the gateway's exact virtual IP text."""
    require(ingress["service_type"] == "ingress" and vip is not None)
    spec = ingress.get("spec")
    ceph_native_nfs.spec_members(spec, GATEWAY_INGRESS_MEMBERS)
    require(spec.get("backend_service") == "mgmt-gateway" and spec.get("keepalive_only") is True and spec.get("virtual_ip") == vip)
    require("first_virtual_router_id" not in spec or type(spec["first_virtual_router_id"]) is int and 1 <= spec["first_virtual_router_id"] <= 255)
    require("health_check_interval" not in spec or isinstance(spec["health_check_interval"], str) and 0 < len(spec["health_check_interval"]) <= 16)


def oauth2_spec(spec):
    """The oauth2-proxy spec within its closed realized record: never a verification switch, a scope or an email domain."""
    require(isinstance(spec, dict) and set(spec) <= OAUTH2_MEMBERS)
    require(all(text(spec.get(name)) for name in ("provider_display_name", "client_id", "oidc_issuer_url")))
    require(all(name not in spec or text(spec[name]) and spec[name].startswith("https://") for name in ("oidc_issuer_url", "redirect_url")))
    require("https_address" not in spec or text(spec["https_address"]) and spec["https_address"].endswith(OAUTH2_PORT))
    domains = spec.get("allowlist_domains", ["-"])
    require(isinstance(domains, list) and domains and all(text(domain) for domain in domains))


def gateway_bindings(unit, proxy):
    """The gateway's TLS pair bound together or not at all; with the proxy its two credentials, its TLS pair together or not at all, and
    its CA bundle exactly with its custom_configs entry; without the proxy no proxy leaf."""
    bound = {(binding["serviceRef"], binding["path"]) for binding in unit.get("bindings", [])}
    require((("mgmt-gateway", "/spec/ssl_cert") in bound) == (("mgmt-gateway", "/spec/ssl_key") in bound))
    if proxy is None:
        require(all(ref != "oauth2-proxy" for ref, unused in bound))
        return
    require({("oauth2-proxy", "/spec/client_secret"), ("oauth2-proxy", "/spec/cookie_secret")} <= bound)
    require((("oauth2-proxy", "/spec/ssl_cert") in bound) == (("oauth2-proxy", "/spec/ssl_key") in bound))
    require((("oauth2-proxy", "/custom_configs/0/content") in bound) == ("custom_configs" in proxy))


def gateway_validate(unit, request, hosts):
    """The singleton mgmt-gateway document after its keepalive-only ingress, present exactly with a virtual IP, and before its oauth2-proxy,
    present exactly with enable_auth; each in name order, bound as gateway_bindings requires."""
    del hosts
    documents = unit["documents"]
    require(unit["id"] == "" and 1 <= len(documents) <= 3)
    kinds = [document["service_type"] for document in documents]
    require(kinds == sorted(set(kinds)) and set(kinds) <= {"ingress", "mgmt-gateway", "oauth2-proxy"} and "mgmt-gateway" in kinds)
    ingress, gateway, proxy = gateway_documents(unit)
    require("service_id" not in gateway)
    spec = gateway.get("spec")
    gateway_spec(spec, proxy is not None)
    vip = spec.get("virtual_ip")
    require((ingress is None) == (vip is None))
    if ingress is not None:
        gateway_ingress(ingress, vip)
    if proxy is not None:
        require("service_id" not in proxy and proxy.get("custom_configs", [{"mount_path": OAUTH2_PROXY_CA_PATH}]) == [{"mount_path": OAUTH2_PROXY_CA_PATH}])
        oauth2_spec(proxy.get("spec"))
    for document in documents:
        require("custom_configs" not in document or document is proxy)
        if "networks" in document:
            ceph_native_nfs.networks(document["networks"])
    gateway_bindings(unit, proxy)
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(set(gateway_needed(unit)) <= frozen)


def gateway_check(worker, unit):
    """Read-only, before any write: the frozen nginx setting and, with the ingress, keepalived's."""
    check_images(worker, gateway_needed(unit))


def gateway_claim(worker, unit, checked):
    """The management gateway unit claims nothing before its apply."""
    del worker, unit, checked


def gateway_settle(worker, unit):
    """The management gateway unit settles nothing after its apply."""
    del worker, unit


def placed_count(document):
    return document["placement"].get("count", len(document["placement"]["hosts"]))


def gateway_expected(unit):
    """The gateway daemons, with the ingress its keepalived daemons, and with the proxy its daemons; a keepalive-only ingress runs no haproxy."""
    ingress, gateway, proxy = gateway_documents(unit)
    result = {("mgmt-gateway", "mgmt-gateway"): placed_count(gateway)}
    if ingress is not None:
        result[("ingress." + ingress["service_id"], "keepalived")] = placed_count(ingress)
    if proxy is not None:
        result[("oauth2-proxy", "oauth2-proxy")] = placed_count(proxy)
    return result


def sso_enabled(worker):
    """Read-only: the dashboard module's sso_oauth2 option, which `dashboard sso enable oauth2` sets; a JSON boolean or its text."""
    value = worker.ceph("config", "get", "mgr", SSO_OPTION)
    if type(value) is bool:
        return value
    require(isinstance(value, str) and value.lower() in ("true", "false"), "native-output")
    return value.lower() == "true"


def gateway_readiness(worker, unit, blocking):
    """Beyond the daemons, whose served leaf the endpoint hook proves: with the proxy, the dashboard's OAuth2 single sign-on is enabled.
    It never writes."""
    if gateway_documents(unit)[2] is None:
        return True
    if sso_enabled(worker):
        worker.sso_protocol = "oauth2"
        return True
    if "sso@oauth2-proxy" not in blocking:
        blocking.append("sso@oauth2-proxy")
    return False


def gateway_frontend(unit):
    """The keepalive-only ingress and the gateway port, the port clients reach on the virtual IP; None without an ingress."""
    ingress, gateway, unused = gateway_documents(unit)
    return None if ingress is None else (ingress, gateway["spec"]["port"])


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


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def opened_beneath(parts):
    """Descriptors of DATA_ROOT and each directory of parts below it, opened without following a link; None when one is missing."""
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    descriptors = []
    try:
        descriptors.append(os.open(DATA_ROOT, flags))
        for part in parts:
            descriptors.append(os.open(part, flags, dir_fd=descriptors[-1]))
    except OSError:
        for descriptor in descriptors:
            os.close(descriptor)
        return None
    return descriptors


def rendered_file(fsid, source, owners, limit=MAX_RENDERED):
    """A host file's bytes beneath DATA_ROOT/<fsid>/, opened component by component without following a link: a regular file with one
    link, owned by one of owners, writable by its owner alone and at most limit bytes, whose identity holds across the read; else None."""
    prefix = DATA_ROOT + "/" + fsid + "/"
    if not isinstance(source, str) or not source.startswith(prefix):
        return None
    parts = source[len(DATA_ROOT) + 1:].split("/")
    if any(part in ("", ".", "..") for part in parts):
        return None
    descriptors = opened_beneath(parts[:-1])
    if descriptors is None:
        return None
    try:
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=descriptors[-1])
        with os.fdopen(descriptor, "rb") as handle:
            before = os.fstat(handle.fileno())
            if not (stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid in owners and before.st_mode & 0o022 == 0
                    and 0 < before.st_size <= limit):
                return None
            value = handle.read(limit + 1)
            after = os.stat(parts[-1], dir_fd=descriptors[-1], follow_symlinks=False)
            same = file_identity(os.fstat(handle.fileno())) == file_identity(before) == file_identity(after)
            if len(value) != before.st_size or not same:
                return None
            return value
    except OSError:
        return None
    finally:
        for descriptor in descriptors:
            os.close(descriptor)


def proxy_owners(user):
    """Root and, when the container names a decimal user, that user: the owners a rendered file may have."""
    name = user.partition(":")[0] if isinstance(user, str) else ""
    return {0} | ({int(name)} if name.isdigit() and name.isascii() else set())


def proxy_container(inspected):
    """The inspected container's arguments, environment, user and (destination, source) mounts, each of the shape podman reports."""
    config = inspected.get("Config") if isinstance(inspected, dict) else None
    require(isinstance(config, dict), "native-output")
    args, environment, user, mounts = inspected.get("Args"), config.get("Env") or [], config.get("User") or "", inspected.get("Mounts") or []
    require(isinstance(args, list) and all(isinstance(item, str) for item in args), "native-output")
    require(isinstance(environment, list) and all(isinstance(item, str) for item in environment) and isinstance(user, str), "native-output")
    require(isinstance(mounts, list) and len(mounts) <= 64, "native-output")
    pairs = []
    for mount in mounts:
        require(isinstance(mount, dict) and isinstance(mount.get("Destination"), str) and isinstance(mount.get("Source"), str), "native-output")
        pairs.append((mount["Destination"], mount["Source"]))
    return args, environment, user, pairs


def proxy_verdict(fsid, inspected, bundle):
    """(verdict, where) for one deployed proxy container: "insecure" naming where it skips verification, "verified" when its arguments,
    environment and mounted configuration do not skip it and, with a bound CA bundle, its CA mount holds exactly that bundle, else
    "unproved". No path, byte, argument or environment value leaves this function."""
    args, environment, user, mounts = proxy_container(inspected)
    where = insecure_flag(args, environment)
    if where is not None:
        return "insecure", where
    owners = proxy_owners(user)
    path = config_path(args)
    sources = [source for destination, source in mounts if destination == path]
    rendered = rendered_file(fsid, sources[0], owners) if path is not None and len(sources) == 1 else None
    try:
        verdict = "unproved" if rendered is None else rendered_skip(rendered.decode("utf-8"))
    except UnicodeError:
        verdict = "unproved"
    if verdict != "verified":
        return verdict, "configuration"
    if bundle is not None:
        sources = [source for destination, source in mounts if destination == OAUTH2_PROXY_CA_PATH]
        mounted = rendered_file(fsid, sources[0], owners) if len(sources) == 1 else None
        if mounted is None or not hmac.compare_digest(mounted, bundle.encode("utf-8")):
            return "unproved", "CA bundle mount"
    return "verified", ""


def gateway_targets(worker, unit, running):
    """(address, TLS name, Host header) of each endpoint: the virtual IP, else every running gateway daemon in host name order."""
    spec = gateway_documents(unit)[1]["spec"]
    vip = spec.get("virtual_ip")
    if vip is not None:
        return [(vip, vip, "[" + vip + "]" if ipaddress.ip_address(vip).version == 6 else None)]
    rows = sorted((row for row in running if row.get("daemon_type") == "mgmt-gateway" and type(row.get("status")) is int and row["status"] == 1),
                  key=lambda row: row["hostname"])
    return [(worker.daemon_address(row, spec["port"]), row["hostname"], None) for row in rows]


def gateway_endpoint(worker, unit, running):
    """Each endpoint serves the bound leaf, or without a binding a leaf, and an HTTP status line; the digest is the bound leaf's, or
    unbound the first endpoint's cephadm-signed leaf's, and only when every endpoint passed."""
    number = gateway_documents(unit)[1]["spec"]["port"]
    held = worker.bound.get("mgmtGatewayCertificate") if isinstance(worker.bound, dict) else None
    bound = None if held is None else hashlib.sha256(leaf(held)).hexdigest()
    targets = gateway_targets(worker, unit, running)
    digests = []
    for address, name, host in targets:
        if address is None:
            worker.pending("http@mgmt-gateway")
            continue
        presented, status = served(address, number, name, host=host)
        if not isinstance(presented, bytes) or not 0 < len(presented) <= MAX_LEAF:
            worker.pending("tls@mgmt-gateway")
            continue
        digest = hashlib.sha256(presented).hexdigest()
        if bound is not None and not hmac.compare_digest(digest, bound):
            worker.pending("tls@mgmt-gateway")
        elif not status:
            worker.pending("http@mgmt-gateway")
        else:
            digests.append(digest)
    if not targets:
        worker.pending("http@mgmt-gateway")
    reachable = bool(targets) and len(digests) == len(targets)
    return {"certificateSHA256": (bound or digests[0]) if reachable else "", "reachable": reachable}
