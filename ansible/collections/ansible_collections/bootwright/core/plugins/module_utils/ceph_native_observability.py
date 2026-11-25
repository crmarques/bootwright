"""Fixed Ceph monitoring service-unit hooks, shipped beside the core worker as source.

A monitoring unit deploys every declared prometheus, alertmanager, grafana, node-exporter, ceph-exporter, snmp-gateway, loki,
alloy and promtail service as singleton documents in one `orch apply -i -` stream. cephadm names each such service by its type and each daemon
`<type>.<host short name>` (src/pybind/mgr/cephadm/module.py:922-966); every daemon runs its frozen auxiliary image, and
ceph-exporter the Ceph image. The hooks only read the frozen image settings, whose single writer is the cluster's
settings block. cephadm itself enables the prometheus manager module for a declared prometheus service
(src/pybind/mgr/cephadm/services/monitoring.py:480-489), so readiness observes it in `ceph mgr services`, which the MON
answers from the MgrMap (src/mon/MgrMonitor.cc:1085-1094); the hooks never enable a module or write an image setting. cephadm
derives a MGR's deployment ports, which it opens in firewalld, from `mgr services` only when it deploys or reconfigures that MGR
(src/pybind/mgr/cephadm/services/cephadmservice.py, MgrService.prepare_create), and enabling the module redeploys none, so the
unit's `manager_ports` phase asks cephadm to reconfigure each running MGR whose ports lack one it publishes. A log agent
derives the two global file-logging options, whose own attempt and completion records guard writes and drift. The grafana
password and SNMP credentials are bound leaves the generic unit worker splices into the apply stream only.

The management gateway unit applies its keepalive-only ingress, when it has a virtual IP, before the singleton mgmt-gateway
document in the same stream; its TLS leaves are bound the same way, and readiness probes the served leaf from the bootstrap host.
cephadm sets the dashboard's standby behaviour itself for a declared gateway (src/pybind/mgr/cephadm/services/mgmt_gateway.py:49-52).
The gateway's oauth2-proxy and the dashboard's single sign-on belong to a CephSignOn's own block (ceph_signon_native); cephadm
reconfigures this gateway for it without a change to this unit. The TLS probe here is shared with the object gateway worker. Like
the core worker it imports only the target Python standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import calendar
import hashlib
import hmac
import ipaddress
import json
import os
import re
import socket
import ssl
import stat
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_monitoring_template, ceph_native_nfs
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    ManagerRefusal, NativeFailure, Refusal, canonical, dashboard_password, require,
)

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
# Readiness names by this entry a running declared MGR whose deployment ports lack a port `mgr services` publishes; a published URL's port
# is its first `:<digits>/`, as MgrService.prepare_create reads it.
PORTS_ENTRY = "ports@mgr"
PUBLISHED_PORT = re.compile(r":([0-9]+)/")
PORTS_VERSION = "ceph-manager-ports-attempt-v1"
GATEWAY_TYPES = (
    "mgmt-gateway",
)
# Each bound leaf of the management gateway: (service type, JSON pointer, runner material variable).
GATEWAY_LEAVES = (
    ("mgmt-gateway", "/spec/ssl_cert", "mgmtGatewayCertificate"),
    ("mgmt-gateway", "/spec/ssl_key", "mgmtGatewayKey"),
)
# The bound leaves observation and removal also hold: the certificate the endpoint proof compares.
OBSERVED_LEAVES = frozenset(("mgmtGatewayCertificate",))
GATEWAY_DAEMON_TYPES = ("mgmt-gateway", "keepalived")
GATEWAY_IMAGES = {"mgmt-gateway": "nginx", "keepalived": "keepalived"}
# The monitored services behind a management gateway whose basic-auth login cephadm keeps and answers through `orch <service>
# get-credentials` as {user, password, certificate} once its monitoring security is enabled, which a running gateway enables, and as {}
# before (src/pybind/mgr/orchestrator/module.py, _get_prometheus_access_info and _get_alertmanager_access_info; src/pybind/mgr/cephadm/
# module.py, get_prometheus_access_info and _get_security_config). cephadm stores admin/admin when it holds none, so an answer is a write.
CREDENTIAL_SERVICES = ("alertmanager", "prometheus")
# The user of the login the gateway's first apply generates for each of them.
GENERATED_USER = "admin"
LOGIN_MEMBERS = frozenset(("password", "user"))
ACCESS_MEMBERS = LOGIN_MEMBERS | {"certificate"}
MAX_LOGIN = 128
MAX_LOGINS = 4096
# The two dashboard options cephadm's mgmt-gateway service sets itself, in a later serve-loop pass after the gateway daemons deploy, so that
# a standby dashboard answers the gateway with an error it skips instead of redirecting the browser to the active manager's address.
STANDBY_OPTIONS = (("mgr/dashboard/standby_behaviour", "error"), ("mgr/dashboard/standby_error_status_code", "503"))
# Rendered files are read only beneath the cluster's data directory, bounded.
DATA_ROOT = "/var/lib/ceph"
MAX_RENDERED = 65536
# Where cephadm's mgmt-gateway daemon form mounts its rendered nginx files (src/cephadm/cephadmlib/daemons/mgmt_gateway.py,
# customize_container_mounts), and the two lines its corrected templates render inside Grafana's upstream and location.
GATEWAY_NGINX = "/etc/nginx/nginx.conf"
GATEWAY_EXTERNAL = "/etc/nginx_external_server.conf"
GRAFANA_AFFINITY = "ip_hash;"
GRAFANA_HOST = "proxy_set_header Host $host;"
# The lines the sign-on corrections render inside the dashboard's and Grafana's oauth2-proxy branches, and the client header they replace.
DASHBOARD_COOKIE = 'proxy_set_header Cookie "";'
GRAFANA_ROLE = 'proxy_set_header X-WEBAUTH-ROLE "Viewer";'
CLIENT_ROLE = "$http_x_auth_request_role"
# The statement the template's oauth2-proxy branch opens each protected location with, and the two statements the protected routes allow
# for the headers they pin, each compared with its whitespace collapsed and its semicolon removed.
SIGN_IN = "auth_request /oauth2/auth"
# The dashboard location's token pin: the access token oauth2-proxy answered, never a client's X-Access-Token header.
TOKEN_SET = "auth_request_set $token $upstream_http_x_auth_request_access_token"
TOKEN_PIN = "proxy_set_header X-Access-Token $token"
# Statements that would let a protected location skip its own statements: another file's, or a satisfied access phase.
UNPROVABLE = ("include", "satisfy")
GATEWAY_ROUTES = ("cookie", "external", "nginx", "role")
# The routes a gateway renders without an oauth2-proxy, which its own readiness proves; the sign-on routes render only inside the
# oauth2-proxy branches, so the CephSignOn's block proves them once its proxy runs.
RENDERED_ROUTES = ("external", "nginx")
SIGN_ON_ROUTES = ("cookie", "role")
STARTED_AT = re.compile(r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]{1,9}))?(Z|[+-][0-9]{2}:[0-9]{2})")
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
    wanted = {"global": {"log_to_file": "true", "mon_cluster_log_to_file": "true"}} if set(kinds) & {"alloy", "promtail"} else None
    require(unit.get("config") == wanted)
    ingress = unit.get("scrapeIngress", {})
    delegated = {entry["name"] for entry in request.get("delegated", []) if entry["name"].startswith("ingress.")}
    require(isinstance(ingress, dict) and set(ingress) == (delegated if "prometheus" in kinds else set()))
    require(all(isinstance(mode, str) and mode in ("http", "drop") for mode in ingress.values()))
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
    logging_state(worker, unit)
    for descriptor, plan in ceph_monitoring_template.plans(worker, unit):
        ceph_monitoring_template.state(worker, plan, descriptor)


def logging_state(worker, unit):
    """Only this operation's attempt owns the two exact global logging settings; native overrides and drift refuse."""
    config = unit.get("config")
    if config is None:
        return None
    wanted = config["global"]
    intent = {"scope": worker.recovery_scope(), "config": config}
    attempted = worker.markers.read("monitoring-log-config-attempt.json")
    completed = worker.markers.read("monitoring-log-config-complete.json")
    require(attempted is None or attempted == intent, "ownership")
    require(completed is None or completed == intent and attempted == intent, "ownership")
    rows = worker.ceph("config", "dump")
    require(isinstance(rows, list) and len(rows) <= 8192 and all(isinstance(row, dict) for row in rows), "native-output")
    present = {}
    for row in rows:
        name = row.get("name")
        require(isinstance(name, str), "native-output")
        if name not in wanted:
            continue
        require(attempted == intent and row.get("section") == "global" and row.get("mask") == "" and name not in present
                and not any(field in row for field in ("location_type", "location_value", "device_class")), "config-drift")
        require(row.get("value") == wanted[name], "config-drift")
        present[name] = row["value"]
    require(completed is None or present == wanted, "config-drift")
    return intent, present, attempted, completed


def claim(worker, unit, checked):
    """Freeze the logging configuration before any native write, after the unit's other ownership proofs."""
    del checked
    state = logging_state(worker, unit)
    if state is not None and state[2] is None:
        worker.markers.write("monitoring-log-config-attempt.json", state[0])
    changed = False
    for descriptor, plan in ceph_monitoring_template.plans(worker, unit):
        changed = ceph_monitoring_template.prepare(worker, plan, descriptor) or changed
    return changed


def settle(worker, unit):
    """Set each missing owned option once; settle a lost reply by readback and retain completion only after both match."""
    state = logging_state(worker, unit)
    if state is None:
        return False
    intent, present, unused, completed = state
    changed = False
    for name, wanted in unit["config"]["global"].items():
        if name not in present:
            worker.tunnel_effect("config", "set", "global", name, wanted, json_result=False)
            changed = True
            present = logging_state(worker, unit)[1]
            require(present.get(name) == wanted, "config-drift")
    if completed is None:
        worker.markers.write("monitoring-log-config-complete.json", intent)
    return changed


def expected(unit):
    """Each singleton service with its daemon count; cephadm's daemon type is the service type."""
    return {(document["service_type"], document["service_type"]): document["placement"].get("count", len(document["placement"]["hosts"]))
            for document in unit["documents"]}


def prometheus_declared(unit):
    return any(document["service_type"] == "prometheus" for document in unit["documents"])


def module_endpoints(worker):
    """The MGR map's availability and active manager, and the module endpoints `mgr services` lists, once the prometheus module's is
    among them; None before."""
    manager = worker.ceph("mgr", "dump")
    require(isinstance(manager, dict) and type(manager.get("available")) is bool, "native-output")
    services = worker.ceph("mgr", "services")
    require(isinstance(services, dict) and len(services) <= MAX_MANAGER_SERVICES
            and all(isinstance(name, str) and isinstance(value, str) for name, value in services.items()), "native-output")
    return (manager, services) if manager["available"] and services.get(MODULE) else None


def published_ports(services):
    ports = set()
    for url in services.values():
        found = PUBLISHED_PORT.search(url)
        if found is not None:
            require(1 <= int(found.group(1)) <= 65535, "native-output")
            ports.add(int(found.group(1)))
    return ports


def lagging(daemons, services):
    """The running declared MGRs whose deployment ports lack a port `mgr services` publishes: cephadm opens in firewalld only a
    MGR's deployment ports, so a scraper on another host never reaches its exporter."""
    published = published_ports(services)
    return sorted(daemon for daemon, (container, up, ports) in daemons.items() if container and up and not published <= set(ports))


def readiness(worker, unit, blocking):
    """Read-only: with prometheus, an available manager whose map lists the prometheus module's endpoint, and every running declared
    MGR deployed with each port the map publishes, as cephadm's daemon cache lists it unrefreshed; nothing enters evidence."""
    if not all(ceph_monitoring_template.ready(worker, plan, descriptor) for descriptor, plan in ceph_monitoring_template.plans(worker, unit)):
        return False
    state = logging_state(worker, unit)
    if state is not None and state[3] is None:
        return False
    if not prometheus_declared(unit):
        return True
    published = module_endpoints(worker)
    entry = "module@" + MODULE if published is None else PORTS_ENTRY
    if published is not None and not lagging(worker.manager_daemons(refresh=False), published[1]):
        return True
    if entry not in blocking:
        blocking.append(entry)
    return False


def reconfigure_ports(worker, repeat):
    """Once the prometheus module's endpoint is published: ask cephadm to reconfigure each running declared MGR whose refreshed
    deployment ports lack a published port. cephadm's reconfigure recomputes the ports from `mgr services`, persists them and opens them
    in firewalld without restarting the MGR; it schedules the action durably and coalesces a repeat. The intent record of a MGR
    container, published port set and active manager instance precedes its first request. A recorded target is asked again only when
    `repeat` holds, which the controller grants to an apply's first evaluated call alone: within one apply a lost reply repeats
    nothing, and each later apply asks a still-lagging target once more, so a lost request never strands readiness. Whether anything
    was asked, and whether nothing could be evaluated yet."""
    try:
        published = module_endpoints(worker)
        daemons = worker.manager_daemons() if published is not None else {}
    except ManagerRefusal:
        return False, True
    if published is None:
        return False, True
    manager, services = published
    require(type(manager.get("active_gid")) is int, "native-output")
    changed, scope = False, worker.recovery_scope()
    for daemon in lagging(daemons, services):
        intent = {"version": PORTS_VERSION, "scope": scope, "daemon": daemon, "container": daemons[daemon][0],
                  "ports": sorted(published_ports(services)), "manager": manager["active_gid"]}
        name = "manager-ports-" + hashlib.sha256(canonical(intent)).hexdigest() + ".json"
        if worker.markers.read(name) is not None and not repeat:
            continue
        proof = worker.transport_hold()
        with proof, worker.cache_hold():
            worker.markers.write(name, intent)
            worker.ceph("orch", "daemon", "reconfig", "mgr." + daemon, json_result=False)
        changed = True
    return changed, False


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


def served(address, number, name, *, host=None, ca=None):
    """The DER leaf a TLS frontend serves for SNI name and whether an HTTP status line follows; trust is the caller's exact fingerprint or its explicit CA.
    The Host header names the SNI name unless host is given."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    if ca is None:
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
    deadline, presented = time.monotonic() + PROBE_TIMEOUT, None
    try:
        if ca is not None:
            context.load_verify_locations(cadata=ca)
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
    """(ingress or None, mgmt-gateway document), each found by its service type."""
    found = {}
    for document in unit["documents"]:
        found.setdefault(document["service_type"], document)
    return found.get("ingress"), found.get("mgmt-gateway")


def gateway_needed(unit):
    """nginx, and keepalived with the keepalive-only ingress, in name order."""
    ingress, unused = gateway_documents(unit)
    return (["keepalived"] if ingress is not None else []) + ["nginx"]


def gateway_spec(spec):
    """The mgmt-gateway spec within its realized record: TLS on, a port, closed protocol and cipher lists. enable_auth is admitted either
    way: cephadm reads it nowhere, and sign-on is on exactly while a CephSignOn's oauth2-proxy runs."""
    require(isinstance(spec, dict) and set(spec) <= GATEWAY_MEMBERS and ceph_native_nfs.port(spec.get("port")))
    require(spec.get("ssl", True) is True and type(spec.get("enable_auth", False)) is bool)
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


def gateway_bindings(unit):
    """The gateway's TLS pair bound together or not at all, and no other leaf."""
    bound = {(binding["serviceRef"], binding["path"]) for binding in unit.get("bindings", [])}
    require((("mgmt-gateway", "/spec/ssl_cert") in bound) == (("mgmt-gateway", "/spec/ssl_key") in bound))
    require(all(ref == "mgmt-gateway" for ref, unused in bound))


def gateway_validate(unit, request, hosts):
    """The singleton mgmt-gateway document after its keepalive-only ingress, present exactly with a virtual IP, in name order, bound as
    gateway_bindings requires. A gateway unit never carries an oauth2-proxy: a CephSignOn realizes it."""
    del hosts
    documents = unit["documents"]
    require(unit["id"] == "" and 1 <= len(documents) <= 2)
    kinds = [document["service_type"] for document in documents]
    require(kinds == sorted(set(kinds)) and set(kinds) <= {"ingress", "mgmt-gateway"} and "mgmt-gateway" in kinds)
    ingress, gateway = gateway_documents(unit)
    require("service_id" not in gateway)
    spec = gateway.get("spec")
    gateway_spec(spec)
    vip = spec.get("virtual_ip")
    require((ingress is None) == (vip is None))
    if ingress is not None:
        gateway_ingress(ingress, vip)
    for document in documents:
        require("custom_configs" not in document)
        if "networks" in document:
            ceph_native_nfs.networks(document["networks"])
    gateway_bindings(unit)
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(set(gateway_needed(unit)) <= frozen)


def gateway_check(worker, unit):
    """Read-only, before any write: the frozen nginx setting and, with the ingress, keepalived's, and the Grafana routing overrides'
    ownership proofs."""
    check_images(worker, gateway_needed(unit))
    for descriptor, plan in ceph_monitoring_template.gateway_plans(worker, unit):
        ceph_monitoring_template.state(worker, plan, descriptor)


def gateway_claim(worker, unit, checked):
    """Before the gateway's first stream, while its attempt record is absent: set a generated login, user admin and a 32-character
    password of the dashboard alphabet, for each declared service cephadm keeps one for. Once a gateway daemon runs, Prometheus and
    Alertmanager require the login cephadm holds, which is admin/admin when none was set (src/pybind/mgr/cephadm/module.py,
    _get_prometheus_credentials and _get_alertmanager_credentials), so a cluster never serves the default. `set-credentials` reads its
    {"username", "password"} only from standard input (src/pybind/mgr/orchestrator/module.py, _get_credentials) and stores it; cephadm
    keeps it until the unit's post-readiness read captures it. A repeat after the attempt record is written sets nothing. Then, before the
    stream too, the owned Grafana routing overrides the seam selects complete, so a fresh gateway renders with them."""
    del checked
    changed = False
    first = worker.markers.read("service-" + GATEWAY_TYPES[0] + "-attempt.json") is None
    for service in credential_services(unit) if first else []:
        password = dashboard_password()
        data = bytearray(canonical({"password": password, "username": GENERATED_USER}))
        try:
            worker.manager_declare(worker.tunnel_effect, "orch", service, "set-credentials", "-i", "-", data=bytes(data), json_result=False,
                                   secrets=(password,))
        finally:
            data[:] = bytes(len(data))
        changed = True
    for descriptor, plan in ceph_monitoring_template.gateway_plans(worker, unit):
        changed = ceph_monitoring_template.prepare(worker, plan, descriptor) or changed
    return changed


def gateway_settle(worker, unit):
    """The management gateway unit settles nothing after its apply."""
    del worker, unit


def placed_count(document):
    return document["placement"].get("count", len(document["placement"]["hosts"]))


def gateway_expected(unit):
    """The gateway daemons, and with the ingress its keepalived daemons; a keepalive-only ingress runs no haproxy."""
    ingress, gateway = gateway_documents(unit)
    result = {("mgmt-gateway", "mgmt-gateway"): placed_count(gateway)}
    if ingress is not None:
        result[("ingress." + ingress["service_id"], "keepalived")] = placed_count(ingress)
    return result


def standby_settled(worker):
    """Read-only: both dashboard options read the values cephadm sets for a gateway, `error` and 503; `ceph config get` answers the module
    default (`redirect`, 500) while a value is unset. The MON answers one key as its bare value and a newline whatever --format asks
    (src/mon/ConfigMonitor.cc:352-365), so the answer is read as one ASCII token, never as JSON."""
    settled = True
    for option, wanted in STANDBY_OPTIONS:
        output = worker.ceph("config", "get", "mgr", option, json_result=False)
        try:
            value = output.decode("ascii") if isinstance(output, bytes) else output
        except UnicodeError:
            raise Refusal("native-output") from None
        require(isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_.-]{1,64}", value.strip()) is not None, "native-output")
        settled = settled and value.strip() == wanted
    return settled


def gateway_readiness(worker, unit, blocking):
    """Beyond the daemons, whose served leaf the endpoint hook proves: cephadm's dashboard standby options are set. It never writes."""
    if not all(ceph_monitoring_template.ready(worker, plan, descriptor) for descriptor, plan in ceph_monitoring_template.gateway_plans(worker, unit)):
        return False
    ready = True
    if not standby_settled(worker):
        if "standby@mgmt-gateway" not in blocking:
            blocking.append("standby@mgmt-gateway")
        ready = False
    return ready


def gateway_frontend(unit):
    """The keepalive-only ingress and the gateway port, the port clients reach on the virtual IP; None without an ingress."""
    ingress, gateway = gateway_documents(unit)
    return None if ingress is None else (ingress, gateway["spec"]["port"])


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
    found = stamped_file(fsid, source, owners, limit)
    return None if found is None else found[0]


def stamped_file(fsid, source, owners, limit=MAX_RENDERED):
    """rendered_file's bytes with the file's modification time in nanoseconds; else None."""
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
            return value, before.st_mtime_ns
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


def grafana_fronted(unit):
    """Whether a management gateway unit fronts a declared Grafana, the only case its Grafana routing overrides can apply."""
    return unit.get("type") == "mgmt-gateway" and "grafana" in unit.get("monitoring", [])


def routes_owed(unit):
    """Whether a unit may own a gateway template override, so its readiness proves the routes: every management gateway unit, whose
    sign-on corrections need no fronted Grafana."""
    return unit.get("type") == "mgmt-gateway"


def config_block(text, opening):
    """The stripped lines of the one nginx block a line reading exactly `opening` opens, up to its closing brace; None when it is absent,
    repeated or unclosed."""
    lines = text.splitlines()
    starts = [index for index, line in enumerate(lines) if line.strip() == opening]
    if len(starts) != 1:
        return None
    depth, body = 1, []
    for line in lines[starts[0] + 1:]:
        depth += line.count("{") - line.count("}")
        if depth <= 0:
            return body if depth == 0 and line.strip() == "}" else None
        body.append(line.strip())
    return None


def statements(lines):
    """A flat block's statements, each with its comment removed, its whitespace collapsed and no semicolon; None for a block that nests
    another, holds a `#` inside quotes, or names a statement in UNPROVABLE, whose effect this reading cannot prove."""
    kept = []
    for line in lines:
        code, mark, unused = line.partition("#")
        if "{" in code or "}" in code or mark and code.count('"') % 2:
            return None
        kept.append(code)
    found = [" ".join(part.split()) for part in "\n".join(kept).split(";") if part.strip()]
    return None if any(entry.split()[0].lower() in UNPROVABLE for entry in found) else found


def pinned(lines, header, wanted, extra=()):
    """Whether a flat location signs every request in through oauth2-proxy, sets the header only to the one wanted value, sets each extra
    statement's header only so, and names no client role header."""
    found = statements(lines)
    if found is None:
        return False
    signed = [entry for entry in found if entry.split()[0] == "auth_request"]
    for statement in (wanted.rstrip(";"),) + tuple(extra):
        key = [part.lower() for part in statement.split()[:2]]
        if [entry for entry in found if [part.lower() for part in entry.split()[:2]] == key] != [statement]:
            return False
    return signed == [SIGN_IN] and not any(CLIENT_ROLE in entry.lower() for entry in found)


def dashboard_only(external):
    """Whether the dashboard's servers are proxied from no location but `location /`, which the dashboard route proves."""
    passes = [line for line in external.splitlines() if "dashboard_servers" in line.partition("#")[0]]
    dashboard = config_block(external, "location / {")
    return len(passes) == 1 and dashboard is not None and passes[0].strip() in dashboard


def routes_grafana(nginx, external):
    """Which routes a deployed gateway's two rendered files carry, by correction name: `nginx` when Grafana's upstream pins each client to
    one Grafana, `external` when its location forwards the client's Host exactly once, `cookie` when the dashboard's servers are proxied
    from `location /` alone, which signs every request in through oauth2-proxy, sends the dashboard only the access token oauth2-proxy
    answered as X-Access-Token and whose only Cookie header is the empty one, `role` when Grafana's location signs every request in and
    its only role header is the fixed Viewer, with no client role header anywhere in it. A sign-on route needs no correction of this
    build: a template that already renders it so passes, and any other shape fails."""
    upstream, location = config_block(nginx, "upstream grafana_servers {"), config_block(external, "location /grafana {")
    dashboard = config_block(external, "location / {")
    return {"nginx": upstream is not None and GRAFANA_AFFINITY in upstream,
            "external": location is not None and location.count(GRAFANA_HOST) == 1,
            "cookie": dashboard is not None and dashboard_only(external) and pinned(dashboard, "cookie", DASHBOARD_COOKIE, (TOKEN_SET, TOKEN_PIN)),
            "role": location is not None and pinned(location, "x-webauth-role", GRAFANA_ROLE)}


def sign_on_routes(target):
    """The routes every running gateway container of a sign-on must render, whatever this build's corrections selected: the dashboard's,
    and Grafana's when the gateway fronts it."""
    return ["cookie", "role"] if target.get("grafana") else ["cookie"]


def started_ns(value):
    """podman's RFC 3339 StartedAt as nanoseconds since the epoch; None for any other form."""
    found = STARTED_AT.fullmatch(value) if isinstance(value, str) else None
    if found is None:
        return None
    year, month, day, hour, minute, second, fraction, zone = found.groups()
    try:
        seconds = calendar.timegm(time.struct_time((int(year), int(month), int(day), int(hour), int(minute), int(second), 0, 0, 0)))
    except (OverflowError, ValueError):
        return None
    if zone != "Z":
        seconds -= (1 if zone[0] == "+" else -1) * (int(zone[1:3]) * 3600 + int(zone[4:6]) * 60)
    return seconds * 1000000000 + int((fraction or "").ljust(9, "0"))


def gateway_routed(fsid, inspected):
    """Which routes one running gateway container serves (routes_grafana), read from the two files it mounts beneath
    DATA_ROOT/<fsid>/ without following a link, each owned by root or the container's user and writable by its owner alone; none unless
    the container started after both were written, since a file bind mount keeps the inode it started with and cephadm's reconfigure
    rewrites the files before it restarts the daemon. No path or byte leaves this function."""
    unserved = dict.fromkeys(GATEWAY_ROUTES, False)
    unused, unused_environment, user, mounts = proxy_container(inspected)
    state = inspected.get("State")
    started = started_ns(state.get("StartedAt")) if isinstance(state, dict) else None
    found = []
    for destination in (GATEWAY_NGINX, GATEWAY_EXTERNAL):
        sources = [source for target, source in mounts if target == destination]
        read = stamped_file(fsid, sources[0], proxy_owners(user)) if len(sources) == 1 else None
        if read is None:
            return unserved
        found.append(read)
    try:
        nginx, external = (value.decode("utf-8") for value, unused in found)
    except UnicodeError:
        return unserved
    if started is None or not all(stamp <= started for unused, stamp in found):
        return unserved
    return routes_grafana(nginx, external)


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
    """Each endpoint serves the bound leaf, or a valid native-CA leaf for its TLS identity, and an HTTP status line; the digest is the bound leaf's, or
    unbound the first endpoint's cephadm-signed leaf's, and only when every endpoint passed."""
    number = gateway_documents(unit)[1]["spec"]["port"]
    held = worker.bound.get("mgmtGatewayCertificate") if isinstance(worker.bound, dict) else None
    bound = None if held is None else hashlib.sha256(leaf(held)).hexdigest()
    ca = None
    if bound is None:
        with worker.manager_served():
            raw = worker.ceph_text("orch", "certmgr", "cert", "get", "cephadm_root_ca_cert")
            require(isinstance(raw, bytes) and 0 < len(raw) <= MAX_CERTIFICATE, "native-output")
        ca = raw.decode("ascii")
    targets = gateway_targets(worker, unit, running)
    digests = []
    for address, name, host in targets:
        if address is None:
            worker.pending("http@mgmt-gateway")
            continue
        presented, status = served(address, number, name, host=host, ca=ca)
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


def credential_services(unit):
    """The declared services whose login a management gateway unit reads: its frozen monitored services cephadm keeps a login for."""
    if unit.get("type") != "mgmt-gateway":
        return []
    return [service for service in unit.get("monitoring", []) if service in CREDENTIAL_SERVICES]


def login_value(value):
    """A user or password as Bootwright retains it: 1 to 128 visible ASCII characters other than `&`, `<` and `>`, which the controller's
    canonical JSON escapes."""
    return isinstance(value, str) and 0 < len(value) <= MAX_LOGIN and all("!" <= c <= "~" and c not in "&<>" for c in value)


def native_login(answer, service, coordinate):
    """One get-credentials answer as its {password, user}; the answer of a cluster whose monitoring security is off holds no login. A
    login cephadm accepted outside the retained alphabet refuses naming only the service and member, never a value."""
    require(isinstance(answer, dict), "native-output")
    require(answer != {}, "material")
    require(LOGIN_MEMBERS <= set(answer) <= ACCESS_MEMBERS, "native-output")
    require(all(isinstance(answer[member], str) and answer[member] for member in LOGIN_MEMBERS), "material")
    for member in ("user", "password"):
        if not login_value(answer[member]):
            raise NativeFailure("monitoring-login-unsupported", coordinate, service + " " + member, secrets=(answer["user"], answer["password"]))
    return {"password": answer["password"], "user": answer["user"]}


def unique_members(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "material")
        result[key] = value
    return result


def logins_material(data, services):
    """The produced output: the canonical JSON of exactly one {password, user} per declared service, nothing else."""
    try:
        require(isinstance(data, bytes) and 0 < len(data) <= MAX_LOGINS, "material")
        value = json.loads(data, object_pairs_hook=unique_members)
    except (ValueError, UnicodeError, TypeError):
        raise Refusal("material") from None
    require(isinstance(value, dict) and sorted(value) == sorted(services) and canonical(value) == data, "material")
    for login in value.values():
        require(isinstance(login, dict) and set(login) == LOGIN_MEMBERS and login_value(login["user"]) and login_value(login["password"]), "material")
    return data
