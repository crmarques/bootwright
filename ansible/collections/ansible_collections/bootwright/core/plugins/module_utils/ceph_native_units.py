"""Fixed generic Ceph service-unit worker phase, shipped beside the core worker as source.

A service-unit request carries one unit: the native service documents of one unit type, applied in their
document order as one `orch apply -i -` stream (src/pybind/mgr/orchestrator/module.py:1826-1846). The generic
flow owns ownership, the unit's normalized inputs record, per-service attempt and completion records, the one
batch, its readback and observation; each type's hooks own its checks, pre-apply claims, post-apply settling,
expected daemons and readiness. Like the core worker it imports only the target Python standard library and the
core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import binascii
import collections
import hmac
import ipaddress
import json
import re

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_monitoring_template, ceph_native, ceph_native_nfs, ceph_native_nvmeof, ceph_native_observability, ceph_native_resources, ceph_signon_native,
)
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, canonical, named, require

# Each unit type's hooks, its daemon types' frozen image names (None runs the Ceph image), its bound credential leaves keyed by
# (service type, JSON pointer) with their runner material variable, the service types whose documents are singletons without a
# service_id, and the hook that proves its endpoint into evidence, if any.
UnitType = collections.namedtuple("UnitType", ("validate", "check", "claim", "settle", "expected", "readiness", "daemon_types", "frontend", "images",
                                               "leaves", "singletons", "endpoint"))
# The RBD mirror daemon runs the Ceph image as a singleton service without a service_id, spec member or listener.
MIRROR_TYPES = ("rbd-mirror",)
# The crash collector runs the Ceph image on every cluster host as a singleton service without a service_id, spec member or listener.
CRASH_TYPES = ("crash",)


def mirror_validate(unit, request, hosts):
    """The one rbd-mirror document without a service_id, spec or binding; its networks, when declared, are exact networks."""
    del request, hosts
    require(unit["id"] == "" and "bindings" not in unit and len(unit["documents"]) == 1)
    document = unit["documents"][0]
    require(document["service_type"] == "rbd-mirror" and "service_id" not in document and "spec" not in document)
    if "networks" in document:
        ceph_native_nfs.networks(document["networks"])


def mirror_check(worker, unit):
    """A Ceph-image singleton unit reads no auxiliary image setting before its apply."""
    del worker, unit


def mirror_claim(worker, unit, checked):
    """A Ceph-image singleton unit claims nothing before its apply."""
    del worker, unit, checked


def mirror_settle(worker, unit):
    """A Ceph-image singleton unit settles nothing after its apply."""
    del worker, unit


def mirror_expected(unit):
    """The rbd-mirror daemons the one document places."""
    placement = unit["documents"][0]["placement"]
    return {("rbd-mirror", "rbd-mirror"): placement.get("count", len(placement["hosts"]))}


def mirror_readiness(worker, unit, blocking):
    """Nothing beyond the daemons running on the frozen Ceph image."""
    del worker, unit, blocking
    return True


def mirror_frontend(unit):
    """A Ceph-image singleton unit has no ingress."""
    del unit


def crash_validate(unit, request, hosts):
    """The one crash document: no service_id, spec, networks or binding, placed on every request host in request order."""
    del request
    require(unit["id"] == "" and "bindings" not in unit and len(unit["documents"]) == 1)
    document = unit["documents"][0]
    require(set(document) == {"placement", "service_type"} and document["service_type"] == "crash")
    require(document["placement"] == {"hosts": [host["name"] for host in hosts]})


def crash_expected(unit):
    """One crash daemon per placed host."""
    return {("crash", "crash"): len(unit["documents"][0]["placement"]["hosts"])}


def crash_readiness(worker, unit, blocking):
    """Every placed host runs one crash daemon on the frozen Ceph image; a count alone never proves it."""
    hosts = unit["documents"][0]["placement"]["hosts"]
    started = [row["hostname"] for row in worker.unit_rows if row.get("service_name") == "crash" and row.get("daemon_type") == "crash"
               and type(row.get("status")) is int and row["status"] == 1 and worker.request["image"] in (row.get("container_image_digests") or [])]
    if sorted(started) == sorted(hosts):
        return True
    if "crash@crash" not in blocking:
        blocking.append("crash@crash")
    return False


UNIT_TYPES = {
    "nvmeof": UnitType(ceph_native_nvmeof.validate_unit, ceph_native_nvmeof.check_unit, mirror_claim, mirror_settle,
                       ceph_native_nvmeof.expected_unit, ceph_native_nvmeof.readiness_unit, ("nvmeof",), mirror_frontend,
                       {"nvmeof": "nvmeof"}, {("nvmeof", "/spec/" + field): "nvmeof_" + field for field in ceph_native_nvmeof.TLS_FIELDS}, frozenset(), None),
    "crash": UnitType(crash_validate, mirror_check, mirror_claim, mirror_settle, crash_expected, crash_readiness, CRASH_TYPES, mirror_frontend,
                      {"crash": None}, {}, frozenset(CRASH_TYPES), None),
    "monitoring": UnitType(ceph_native_observability.validate, ceph_native_observability.check, ceph_native_observability.claim,
                           ceph_native_observability.settle, ceph_native_observability.expected, ceph_native_observability.readiness,
                           ceph_native_observability.DAEMON_TYPES, ceph_native_observability.frontend, ceph_native_observability.IMAGES,
                           {(kind, path): variable for kind, path, variable in ceph_native_observability.MONITORING_LEAVES},
                           frozenset(ceph_native_observability.MONITORING_TYPES), None),
    "nfs": UnitType(ceph_native_nfs.validate, ceph_native_nfs.check, ceph_native_nfs.claim, ceph_native_nfs.settle, ceph_native_nfs.expected,
                    ceph_native_nfs.readiness, ceph_native_nfs.DAEMON_TYPES, ceph_native_nfs.frontend,
                    {"nfs": None, "haproxy": "haproxy", "keepalived": "keepalived"}, {}, frozenset(), None),
    "mgmt-gateway": UnitType(ceph_native_observability.gateway_validate, ceph_native_observability.gateway_check, ceph_native_observability.gateway_claim,
                             ceph_native_observability.gateway_settle, ceph_native_observability.gateway_expected,
                             ceph_native_observability.gateway_readiness, ceph_native_observability.GATEWAY_DAEMON_TYPES,
                             ceph_native_observability.gateway_frontend, ceph_native_observability.GATEWAY_IMAGES,
                             {(kind, path): variable for kind, path, variable in ceph_native_observability.GATEWAY_LEAVES},
                             frozenset(ceph_native_observability.GATEWAY_TYPES), ceph_native_observability.gateway_endpoint),
    "oauth2-proxy": UnitType(ceph_signon_native.validate, ceph_signon_native.check, ceph_signon_native.claim, ceph_signon_native.settle,
                             ceph_signon_native.expected, ceph_signon_native.readiness, ceph_signon_native.DAEMON_TYPES, ceph_signon_native.frontend,
                             ceph_signon_native.IMAGES, {(kind, path): variable for kind, path, variable in ceph_signon_native.LEAVES},
                             frozenset(ceph_signon_native.DAEMON_TYPES), ceph_signon_native.endpoint),
    "rbd-mirror": UnitType(mirror_validate, mirror_check, mirror_claim, mirror_settle, mirror_expected, mirror_readiness, MIRROR_TYPES, mirror_frontend,
                           {"rbd-mirror": None}, {}, frozenset(MIRROR_TYPES), None),
}
PHASES = frozenset(("unit", "ingress_firewall", "vip_holder", "signon_discovery", "signon_routes", "signon_route_proof", "sso_config", "sso",
                    "signon_baseline", "signon_remove", "signon_state", "signon_sso_disable", "signon_local", "signon_resume_unit",
                    "manager_ports", "manager_ports_unrecorded",
                    "nvmeof_unit_observe", "monitoring_credentials", "monitoring_credentials_held", "grafana_route", "grafana_route_proof",
                    "grafana_reconfig"))
READ_ONLY_PHASES = frozenset(("nvmeof_unit_observe", "monitoring_credentials_held", "signon_discovery", "signon_routes", "signon_route_proof",
                              "signon_baseline", "signon_state", "signon_local"))
# The daemon types whose deployment proves cephadm v20.2.1 already stores a service's login: it stores both logins before it configures a
# Prometheus daemon (PrometheusService.generate_config) and Prometheus's before it configures a Grafana daemon
# (GrafanaService.get_dependencies), src/pybind/mgr/cephadm/services/monitoring.py.
LOGIN_HOLDERS = {"alertmanager": frozenset(("prometheus",)), "prometheus": frozenset(("grafana", "prometheus"))}
# The ingress firewall check reads only these host tools, never with a write, reload or query form.
SYSTEMCTL = "/usr/bin/systemctl"
FIREWALL_CMD = "/usr/bin/firewall-cmd"
IP = "/usr/sbin/ip"
FIREWALL_UNIT = "firewalld.service"
FIREWALL_TIMEOUT = 20
MAX_INTERFACES = 256
MAX_ADDRESSES = 64
MAX_CANDIDATES = 8
MAX_SERVICES = 64
MAX_INGRESS_PORTS = 4
VRRP = "VRRP (IP protocol 112)"
VRRP_TOKENS = frozenset(("vrrp", "112"))
UNIT_STATE = re.compile(r"[a-z-]{1,32}\Z")
INTERFACE = re.compile(r"[A-Za-z0-9_.-]{1,15}\Z")
ZONE_NAME = re.compile(r"[A-Za-z0-9_.+-]{1,64}\Z")
PORT_TOKEN = re.compile(r"([0-9]{1,5})(?:-([0-9]{1,5}))?/tcp\Z")
RICH_VRRP = re.compile(r'rule (?:family="(ipv4|ipv6)" )?protocol value="(?:vrrp|112)" accept\Z')
RICH_PORT = re.compile(r'rule (?:family="(ipv4|ipv6)" )?port port="([0-9]{1,5})(?:-([0-9]{1,5}))?" protocol="tcp" accept\Z')
DNS_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
GATEWAY_MONITORED = ("alertmanager", "grafana", "prometheus")
DOCUMENT_MEMBERS = frozenset(("networks", "placement", "service_id", "service_type", "spec"))
# A sign-on's oauth2-proxy document alone also carries its one custom_configs entry, the OIDCProvider's trust bundle mount.
PROXY_MEMBERS = DOCUMENT_MEMBERS | {"custom_configs"}
INPUTS_VERSION = "ceph-service-inputs-v1"
MAX_DAEMONS = 256
ENDPOINT_ENTRIES = frozenset(("vip", "noholder", "holders", "module", "sso", "standby", "route", "stable", "enforce"))
PENDING = re.compile(r"(?:(?:" + "|".join(sorted({name for unit in UNIT_TYPES.values() for name in unit.daemon_types} | ENDPOINT_ENTRIES))
                     + r")@(?:(?:nfs|ingress)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}|"
                     + "|".join(ceph_native_observability.MONITORING_TYPES + ceph_native_observability.GATEWAY_TYPES + ceph_signon_native.DAEMON_TYPES
                                + MIRROR_TYPES + CRASH_TYPES)
                     + r")|(?:tls|http)@(?:" + "|".join(ceph_native_observability.GATEWAY_TYPES) + r")|"
                     + re.escape(ceph_native_observability.PORTS_ENTRY) + r")\Z")
BINDING_MEMBERS = frozenset(("path", "secretRef", "serviceRef"))
# The non-secret stand-in a bound leaf holds wherever the worker normalizes, records or compares a document.
BOUND = "bootwright-bound:"
# A leaf cephadm's ServiceSpec validates by length holds a stand-in of an admitted length: OAuth2ProxySpec admits a cookie_secret of 16,
# 24 or 32 bytes, counted after URL-safe base64 decoding when it decodes (src/python-common/ceph/deployment/service_spec.py,
# OAuth2ProxySpec._validate_cookie_secret); this 32-byte text does not decode.
PLACEHOLDERS = {"/spec/cookie_secret": BOUND + "/spec/cookie_se"}
COOKIE_SECRET_LENGTHS = (16, 24, 32)
MATERIAL_VARIABLES = frozenset(variable for unit in UNIT_TYPES.values() for variable in unit.leaves.values())
MAX_MATERIAL = 4096
MATERIAL_LIMITS = {"mgmtGatewayCertificate": ceph_native_observability.MAX_CERTIFICATE, "mgmtGatewayKey": ceph_native_observability.MAX_KEY,
                   "oauth2ProxyCABundle": ceph_native_observability.MAX_CERTIFICATE, "oauth2ProxyCertificate": ceph_native_observability.MAX_CERTIFICATE,
                   "oauth2ProxyKey": ceph_native_observability.MAX_KEY}
CERTIFICATE_VARIABLES = frozenset(("mgmtGatewayCertificate", "oauth2ProxyCABundle", "oauth2ProxyCertificate"))
KEY_VARIABLES = frozenset(("mgmtGatewayKey", "oauth2ProxyKey"))
# The rendered-configuration proof reads only this cluster's oauth2-proxy containers, through these host tools, never with a write form.
PODMAN = "/usr/bin/podman"
SSO_TIMEOUT = 30
MAX_PROXY_CONTAINERS = 16
CONTAINER_ID = re.compile(r"[0-9a-f]{12,64}\Z")
MAX_GATEWAY_CONTAINERS = 16
INDEX = re.compile(r"(?:0|[1-9][0-9]{0,3})\Z")


CERTIFICATE_VARIABLES = CERTIFICATE_VARIABLES | {"nvmeof_root_ca_cert", "nvmeof_server_cert", "nvmeof_client_cert"}
KEY_VARIABLES = KEY_VARIABLES | {"nvmeof_server_key", "nvmeof_client_key"}


def service_name(document):
    """cephadm's service name: the type alone for a singleton without a service_id."""
    return document["service_type"] + ("." + document["service_id"] if "service_id" in document else "")


def validate_bindings(unit):
    """No bindings, or each a bound leaf of a document of this unit, once, in (service, path) order."""
    if "bindings" not in unit:
        return
    bindings = unit["bindings"]
    leaves = UNIT_TYPES[unit["type"]].leaves
    types = {service_name(document): document["service_type"] for document in unit["documents"]}
    require(isinstance(bindings, list) and 1 <= len(bindings) <= len(leaves))
    for binding in bindings:
        require(isinstance(binding, dict) and set(binding) == BINDING_MEMBERS and named(binding["secretRef"]))
        require(isinstance(binding["serviceRef"], str) and binding["serviceRef"] in types and isinstance(binding["path"], str))
        require((types[binding["serviceRef"]], binding["path"]) in leaves)
    keys = [(binding["serviceRef"], binding["path"]) for binding in bindings]
    require(keys == sorted(set(keys)))


def material_names(request, phase="unit"):
    """The runner material variables the unit's bindings name that a phase holds: every one on its apply phase, the observed leaves on
    observation and removal, the bound Grafana password on a monitoring unit's login phases, none otherwise."""
    unit = request["service"]
    leaves = UNIT_TYPES[unit["type"]].leaves
    types = {service_name(document): document["service_type"] for document in unit["documents"]}
    names = frozenset(leaves[(types[binding["serviceRef"]], binding["path"])] for binding in unit.get("bindings", []))
    if phase in ("unit", "signon_resume_unit"):
        return names
    if phase in ("sso_config", "signon_discovery"):
        return names & {ceph_signon_native.CA_VARIABLE}
    if phase in ("login_prepare", "login_update"):
        return names & {"grafanaAdminPassword"}
    return names & ceph_native_observability.OBSERVED_LEAVES if phase in ("observe", "destroy") else frozenset()


def placeholder(path):
    """The non-secret stand-in a bound leaf at this pointer holds."""
    return PLACEHOLDERS.get(path, BOUND + path)


def cookie_secret(value):
    """Whether cephadm admits an oauth2-proxy cookie_secret: its URL-safe base64 decoding, else its UTF-8 text, is 16, 24 or 32 bytes."""
    try:
        length = len(base64.urlsafe_b64decode(value))
    except binascii.Error:
        length = len(value.encode("utf-8"))
    except ValueError:
        return False
    return length in COOKIE_SECRET_LENGTHS


def material_value(variable, value):
    """A bound value within its variable's bound: a certificate chain whose first block decodes, a PEM private key, a cookie secret
    cephadm admits, else a short text."""
    limit = MATERIAL_LIMITS.get(variable, MAX_MATERIAL)
    try:
        size = len(value.encode("utf-8")) if isinstance(value, str) else 0
    except UnicodeError:
        size = 0
    if not 0 < size <= limit or "\x00" in value:
        return False
    if variable in CERTIFICATE_VARIABLES:
        return bool(ceph_native_observability.leaf(value))
    if variable == "oauth2ProxyCookieSecret":
        return cookie_secret(value)
    return variable not in KEY_VARIABLES or ceph_native_observability.PRIVATE_KEY.search(value) is not None


def material(request, phase, private):
    """The bound leaves a phase may hold: exactly those the unit's bindings name that the phase holds."""
    require(isinstance(private, dict), "material")
    expected = material_names(request, phase)
    require(set(private) == expected and all(material_value(name, value) for name, value in private.items()), "material")
    return dict(private)


def step(container, part):
    """(present, child) of one JSON pointer step: an object's member, or a list's item at a decimal index within its length."""
    if isinstance(container, list):
        if INDEX.fullmatch(part) is not None and int(part) < len(container):
            return True, container[int(part)]
        return False, None
    if isinstance(container, dict) and part in container:
        return True, container[part]
    return False, None


def placed(document, paths):
    """A copy of the document with each bound leaf's placeholder at its pointer; a missing object member is created, a list item never."""
    if not paths:
        return document
    value = json.loads(canonical(document))
    for path in paths:
        parts = path.split("/")[1:]
        target = value
        for part in parts[:-1]:
            if isinstance(target, dict):
                target.setdefault(part, {})
            present, target = step(target, part)
            require(present)
        require(isinstance(target, dict))
        target[parts[-1]] = placeholder(path)
    return value


def leaf(value, path):
    """(present, leaf) at a JSON pointer; an ancestor that is neither an object nor a list is present and holds no leaf."""
    for part in path.split("/")[1:]:
        if not isinstance(value, (dict, list)):
            return True, None
        present, value = step(value, part)
        if not present:
            return False, None
    return True, value


def validate(request, hosts):
    """One unit of a known type: 1 to 8 placed documents, each listed in `delegated` with exactly its hosts; an oauth2-proxy within
    the hosts the cluster lists for it."""
    unit = request.get("service")
    require(isinstance(unit, dict) and {"documents", "id", "type"} <= set(unit) <=
            {"bindings", "config", "documents", "endpoint", "id", "ingressPorts", "monitoring", "observeBudget", "scrapeIngress", "signOn", "type"})
    require(isinstance(unit["type"], str) and unit["type"] in UNIT_TYPES)
    require("config" not in unit or unit["type"] == "monitoring")
    require("scrapeIngress" not in unit or unit["type"] == "monitoring")
    require(("signOn" in unit) == (unit["type"] == ceph_signon_native.TYPE))
    gateway_access(unit)
    # Go freezes the NVMe-oF unit observer's read bound (service_unit.go unitDeadline).
    require(("observeBudget" in unit) == (unit["type"] == "nvmeof"))
    require("observeBudget" not in unit or type(unit["observeBudget"]) is int and 0 < unit["observeBudget"] <= ceph_native_nvmeof.MAX_BUDGET)
    ingress_ports(unit)
    singletons = UNIT_TYPES[unit["type"]].singletons
    require(unit["id"] == "" or isinstance(unit["id"], str) and ceph_native.NAME.fullmatch(unit["id"]) is not None)
    documents = unit["documents"]
    require(isinstance(documents, list) and 1 <= len(documents) <= 8)
    names = [host["name"] for host in hosts]
    delegated = {entry["name"]: entry["hosts"] for entry in request.get("delegated", [])}
    seen = set()
    for document in documents:
        proxy = unit["type"] == ceph_signon_native.TYPE
        require(isinstance(document, dict) and {"placement", "service_type"} <= set(document) <= (PROXY_MEMBERS if proxy else DOCUMENT_MEMBERS))
        singleton = document["service_type"] in singletons
        require(named(document["service_type"]) and ("service_id" not in document if singleton else named(document.get("service_id"))))
        placement = document["placement"]
        require(isinstance(placement, dict) and "hosts" in placement and set(placement) <= {"hosts", "count"})
        listed = placement["hosts"]
        require(isinstance(listed, list) and bool(listed) and all(isinstance(host, str) and host in names for host in listed))
        require(len(set(listed)) == len(listed))
        require("count" not in placement or type(placement["count"]) is int and placement["count"] >= 1)
        name = service_name(document)
        # The cluster lists oauth2-proxy on every host, so a sign-on added after its apply moves no frozen request.
        require(name not in seen and (set(listed) <= set(delegated.get(name, ())) if proxy else delegated.get(name) == listed))
        seen.add(name)
    validate_bindings(unit)
    UNIT_TYPES[unit["type"]].validate(unit, request, hosts)


def gateway_access(unit):
    """A management gateway's client DNS name and the monitoring services it fronts, as Go froze them: only on a mgmt-gateway unit, a
    DNS name and a sorted, nonempty subset of the monitoring services."""
    endpoint, monitoring = unit.get("endpoint"), unit.get("monitoring")
    require(endpoint is None and monitoring is None or unit["type"] == "mgmt-gateway")
    require(endpoint is None or isinstance(endpoint, str) and 0 < len(endpoint) <= 253 and all(DNS_LABEL.fullmatch(part) for part in endpoint.split(".")))
    require(monitoring is None or isinstance(monitoring, list) and bool(monitoring) and all(service in GATEWAY_MONITORED for service in monitoring) and
            monitoring == sorted(set(monitoring)))


def frontend(request):
    """The unit's ingress document and client port, as its type names them; None without an ingress."""
    return UNIT_TYPES[request["service"]["type"]].frontend(request["service"])


def ingress_ports(unit):
    """The TCP ports beside the client port that the unit's ingress hosts must also permit, as the request carries them: only a management
    gateway with its ingress carries them, its internal port among them."""
    ports = unit.get("ingressPorts")
    if ports is None:
        return []
    documents = unit["documents"] if isinstance(unit["documents"], list) else []
    require(unit["type"] == "mgmt-gateway" and any(isinstance(document, dict) and document.get("service_type") == "ingress" for document in documents))
    require(isinstance(ports, list) and 1 <= len(ports) <= MAX_INGRESS_PORTS and all(ceph_native_nfs.port(port) for port in ports))
    require(len(set(ports)) == len(ports))
    return list(ports)


def text(output):
    return bytes(output).decode("utf-8", "replace")


def firewall_active(worker):
    """Whether firewalld filters here: inactive or failed does not, active or reloading does; any other state is unsettled and refuses."""
    unused, output = worker.run([SYSTEMCTL, "is-active", FIREWALL_UNIT], timeout=FIREWALL_TIMEOUT)
    state = bytes(output).decode("ascii", "replace").strip()
    if state in ("inactive", "failed"):
        return False
    if state in ("active", "reloading"):
        return True
    shown = state if UNIT_STATE.fullmatch(state) else "unknown"
    raise NativeFailure("ingress-firewall", worker.native_coordinate(), "host " + worker.host["name"] + ": " + FIREWALL_UNIT + " is " + shown)


def interface_addresses(rows):
    """Each interface name with each of its addresses and prefix length, from `ip -json address show`."""
    require(isinstance(rows, list) and len(rows) <= MAX_INTERFACES, "native-output")
    found = []
    for row in rows:
        require(isinstance(row, dict) and isinstance(row.get("ifname"), str), "native-output")
        entries = row.get("addr_info")
        require(isinstance(entries, list) and len(entries) <= MAX_ADDRESSES, "native-output")
        for entry in entries:
            require(isinstance(entry, dict) and entry.get("family") in ("inet", "inet6") and isinstance(entry.get("local"), str)
                    and type(entry.get("prefixlen")) is int, "native-output")
            try:
                found.append((row["ifname"], ipaddress.ip_interface(entry["local"] + "/" + str(entry["prefixlen"]))))
            except ValueError:
                raise Refusal("native-output") from None
    return found


def address_networks(rows):
    """Each interface name with the networks of its addresses, from `ip -json address show`."""
    return [(name, address.network) for name, address in interface_addresses(rows)]


def vip_held(worker, spec):
    """Whether this host holds the virtual IP now: one of its interfaces carries the address, as keepalived adds it to its master."""
    vip = ipaddress.ip_interface(spec["virtual_ip"]).ip
    rows = worker.command([IP, "-json", "address", "show"], timeout=FIREWALL_TIMEOUT)
    return any(address.ip == vip for unused, address in interface_addresses(rows))


def vip_interfaces(worker, spec):
    """The interfaces cephadm puts the virtual IP and VRRP on: those whose network holds it, else those on a listed virtual interface network."""
    vip = ipaddress.ip_interface(spec["virtual_ip"]).ip
    networks = address_networks(worker.command([IP, "-json", "address", "show"], timeout=FIREWALL_TIMEOUT))
    candidates = {name for name, network in networks if vip in network}
    if not candidates and "virtual_interface_networks" in spec:
        candidates = {name for name, network in networks if str(network) in spec["virtual_interface_networks"]}
    require(len(candidates) <= MAX_CANDIDATES and all(INTERFACE.fullmatch(name) is not None for name in candidates), "native-output")
    return vip, sorted(candidates)


def interface_zones(worker, candidates):
    """Each candidate's runtime zone; an interface bound to no zone is in the default zone, read once."""
    zones, default = {}, []
    for name in candidates:
        code, output = worker.run([FIREWALL_CMD, "--get-zone-of-interface=" + name], timeout=FIREWALL_TIMEOUT)
        stdout, stderr = text(output).strip(), text(getattr(output, "stderr", b"")).strip()
        if code == 0 and ZONE_NAME.fullmatch(stdout) is not None:
            zones[name] = stdout
        elif code != 0 and "no zone" in (stdout, stderr):
            if not default:
                zone = text(worker.command([FIREWALL_CMD, "--get-default-zone"], json_result=False, timeout=FIREWALL_TIMEOUT)).strip()
                require(ZONE_NAME.fullmatch(zone) is not None, "native-output")
                default.append(zone)
            zones[name] = default[0]
        else:
            raise NativeFailure("native-command", worker.native_coordinate(), getattr(output, "stderr", b"") or output)
    return zones


def zone_fields(value):
    """firewall-cmd's `--info-zone` or `--info-service` text: each two-space `key: value` line, and the tab lines after `rich rules`."""
    fields, rules, rich = {}, [], False
    for line in value.splitlines()[1:]:
        if line.startswith("\t"):
            if rich:
                rules.append(line[1:].strip())
            continue
        rich = False
        if line.startswith("  ") and not line.startswith("   ") and ":" in line:
            key, unused, member = line[2:].partition(":")
            fields[key] = member.strip()
            rich = key == "rich rules"
    return fields, rules


def in_range(port, low, high):
    return int(low) <= port <= int(high or low)


def grants_vrrp(fields, rules, family):
    return bool(VRRP_TOKENS & set(fields.get("protocols", "").split())) or any(
        found is not None and found.group(1) in (None, family) for found in (RICH_VRRP.fullmatch(rule) for rule in rules))


def grants_port(fields, rules, family, port):
    tokens = (PORT_TOKEN.fullmatch(token) for token in fields.get("ports", "").split())
    if any(found is not None and in_range(port, found.group(1), found.group(2)) for found in tokens):
        return True
    return any(found is not None and found.group(1) in (None, family) and in_range(port, found.group(2), found.group(3))
               for found in (RICH_PORT.fullmatch(rule) for rule in rules))


def service_grants(worker, zone, names, ports, read, needs):
    """Follow the zone's services and their includes while a need is unmet; only a service without a destination grants."""
    queue, seen = list(names), set()
    while queue and not all(needs):
        name = queue.pop(0)
        if name in seen:
            continue
        seen.add(name)
        require(ZONE_NAME.fullmatch(name) is not None, "native-output")
        if name not in read:
            if len(read) >= MAX_SERVICES:
                raise NativeFailure("ingress-firewall", worker.native_coordinate(),
                                    "host " + worker.host["name"] + ": firewalld zone " + zone + " lists more than " + str(MAX_SERVICES) + " services")
            argv = [FIREWALL_CMD, "--info-service=" + name]
            read[name] = zone_fields(text(worker.command(argv, json_result=False, timeout=FIREWALL_TIMEOUT)))[0]
        fields = read[name]
        if not fields.get("destination"):
            needs = [needs[0] or bool(VRRP_TOKENS & set(fields.get("protocols", "").split()))] + [
                met or grants_port(fields, [], None, port) for met, port in zip(needs[1:], ports)]
        queue.extend(fields.get("includes", "").split())
    return needs


def zone_missing(worker, zone, vip, ports, read):
    """What the zone's runtime state does not permit of VRRP and the ingress's ports; target ACCEPT permits everything."""
    fields, rules = zone_fields(text(worker.command([FIREWALL_CMD, "--info-zone=" + zone], json_result=False, timeout=FIREWALL_TIMEOUT)))
    require("target" in fields, "native-output")
    if fields["target"] == "ACCEPT":
        return []
    family = "ipv4" if vip.version == 4 else "ipv6"
    needs = [grants_vrrp(fields, rules, family)] + [grants_port(fields, rules, family, port) for port in ports]
    needs = service_grants(worker, zone, fields.get("services", "").split(), ports, read, needs)
    return ([] if needs[0] else [VRRP]) + [str(port) + "/tcp" for met, port in zip(needs[1:], ports) if not met]


def firewall_check(worker, spec, ports):
    """Read-only: where an active firewalld filters, each interface that carries the virtual IP permits VRRP and each of the ingress's
    ports: the client port, then any port the request adds."""
    if not firewall_active(worker):
        return
    vip, candidates = vip_interfaces(worker, spec)
    zones = interface_zones(worker, candidates)
    verdicts, read, clauses = {}, {}, []
    for name in candidates:
        zone = zones[name]
        if zone not in verdicts:
            verdicts[zone] = zone_missing(worker, zone, vip, ports, read)
        if verdicts[zone]:
            clauses.append("interface " + name + " in firewalld zone " + zone + " does not permit " + " and ".join(verdicts[zone]))
    if clauses:
        raise NativeFailure("ingress-firewall", worker.native_coordinate(), "host " + worker.host["name"] + ": " + "; ".join(clauses))


class UnitWorker(ceph_native_resources.ResourceWorker):
    """The core worker with one service unit's apply phase and observation."""

    OWN_MEMBER = "service"

    bound = None
    unit_rows = ()
    sso_protocol = ""

    @staticmethod
    def image_key(name):
        """The settings block's manager option for one auxiliary image; a unit only reads it."""
        return ceph_native_resources.IMAGE_KEY + name

    def unit_key(self):
        unit = self.request["service"]
        return "unit-" + unit["type"] + ("-" + unit["id"] if unit["id"] else "")

    def bound_leaves(self):
        """Each bound document's (pointer, material variable) pairs, by service name."""
        unit = self.request["service"]
        leaves = UNIT_TYPES[unit["type"]].leaves
        types = {service_name(document): document["service_type"] for document in unit["documents"]}
        result = {}
        for binding in unit.get("bindings", []):
            result.setdefault(binding["serviceRef"], []).append((binding["path"], leaves[(types[binding["serviceRef"]], binding["path"])]))
        return result

    def unit_groups(self):
        """Each document; a bound one holds the placeholders, so the SDK, the records and every comparison see no leaf."""
        bound = self.bound_leaves()
        return [[placed(document, [path for path, unused in bound.get(service_name(document), ())])] for document in self.request["service"]["documents"]]

    def splice_bound(self, value):
        """The apply stream's copy of one normalized document, the bound leaves in place of their placeholders; never recorded."""
        pairs = self.bound_leaves().get(service_name(value), ())
        if not pairs:
            return value
        value = json.loads(canonical(value))
        for path, variable in pairs:
            parts = path.split("/")[1:]
            present, found = leaf(value, path)
            require(present and found == placeholder(path), "native-output")
            require(isinstance(self.bound, dict) and variable in self.bound, "material")
            target = value
            for part in parts[:-1]:
                unused, target = step(target, part)
            target[parts[-1]] = self.bound[variable]
        return value

    def bound_specs(self, stored):
        """The stored specifications with each bound document in placeholder form: its bound leaves all omitted, or each a string
        equal to the bound one where this phase holds it; any other stored leaf is not its Secret's. Only a document this unit's
        attempt record covers is compared: another stored one stays as stored, so the ownership checks refuse it as foreign first."""
        result = dict(stored)
        held = self.bound if isinstance(self.bound, dict) else {}
        for name, pairs in self.bound_leaves().items():
            value = stored.get(name)
            if not isinstance(value, dict) or self.markers.read("service-" + name + "-attempt.json") is None:
                continue
            found = [(path, variable) + leaf(value, path) for path, variable in pairs]
            for path, variable, present, current in found:
                same = isinstance(current, str) and bool(current) and (variable not in held or hmac.compare_digest(
                    current.encode("utf-8", "surrogatepass"), held[variable].encode("utf-8", "surrogatepass")))
                if any(item[2] for item in found) and not (present and same):
                    raise NativeFailure("service-credential-foreign", self.native_coordinate(), name + " " + path, secrets=tuple(held.values()))
            result[name] = placed(value, [path for path, unused in pairs])
        return result

    def unit_names(self):
        return [service_name(document) for document in self.request["service"]["documents"]]

    def unit_specs(self):
        limit = (len(self.request["services"]) + len(self.request.get("delegated", []))) * len(self.request["hosts"])
        return self.service_specs(limit=limit)

    def unit_inputs(self):
        """The unit's retained normalized documents, or None before its first apply; another scope is not this unit's."""
        record = self.markers.read(self.unit_key() + "-inputs.json")
        if record is None:
            return None
        scope = {"owner": self.request["owner"], "fsid": self.request["fsid"], "image": self.request["image"], "inputs": self.unit_groups()}
        require(isinstance(record, dict) and set(record) == {"version", "scope", "services"} and record["version"] == INPUTS_VERSION
                and record["scope"] == scope, "ownership")
        services = record["services"]
        require(isinstance(services, list) and len(services) == len(scope["inputs"]), "ownership")
        require(all(isinstance(group, list) and len(group) == 1 and isinstance(group[0], dict) for group in services), "ownership")
        return [group[0] for group in services]

    def unit_records(self, values, observed, scope, resume=False):
        """Each document's attempt and completion equal its intent; a stored specification needs this operation's attempt. Only a sign-on's
        resume admits a completed document no longer stored, which its suspension removed."""
        for name, value in zip(self.unit_names(), values):
            prefix = "service-" + name
            intent = {"scope": scope, "service": value}
            attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
            require(attempt is None or attempt == intent, "ownership")
            require(complete is None or complete == intent and attempt == intent, "ownership")
            present = observed.get(name)
            require(present is None or attempt == intent and present == value, "foreign-service")
            require(complete is None or present == value or resume and present is None, "foreign-service")

    def unit(self, resume=False):
        """Ownership and the type's checks before any write; then the inputs record, record checks, attempts, one batch, readback, completions."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        unit = self.request["service"]
        hooks = UNIT_TYPES[unit["type"]]
        checked = hooks.check(self, unit)
        values = [group[0] for group in self.normalized_services(self.unit_groups(), record=self.unit_key() + "-inputs.json")]
        scope = self.recovery_scope()
        observed = self.bound_specs(self.manager_retry(self.unit_specs))
        self.unit_records(values, observed, scope, resume)
        claimed = hooks.claim(self, unit, checked)
        names = self.unit_names()
        missing = [(name, value) for name, value in zip(names, values) if name not in observed]
        for name, value in missing:
            if self.markers.read("service-" + name + "-attempt.json") is None:
                self.markers.write("service-" + name + "-attempt.json", {"scope": scope, "service": value})
        changed = bool(missing)
        if missing:
            data = b"---\n".join(canonical(self.splice_bound(value)) + b"\n" for unused, value in missing)
            self.manager_declare(self.tunnel_effect, "orch", "apply", "-i", "-", data=data, json_result=False)
            observed = self.bound_specs(self.manager_retry(self.unit_specs))
            require(all(observed.get(name) == value for name, value in zip(names, values)), "not-ready")
        for name, value in zip(names, values):
            if self.markers.read("service-" + name + "-complete.json") is None:
                self.markers.write("service-" + name + "-complete.json", {"scope": scope, "service": value})
        settled = hooks.settle(self, unit)
        return {"changed": changed or claimed is True or settled is True}

    def ingress_frontend(self):
        return frontend(self.request)

    def ingress_ports(self):
        return ingress_ports(self.request["service"])

    def ingress_firewall(self):
        """Read-only, before any effect of this apply: on this ingress host an active firewalld permits VRRP, the client port and each port
        the request adds where the virtual IP lands. This check never changes a firewall; the managed OS realizes the same requirements
        on a Machine it installs with its firewall enabled."""
        found = self.ingress_frontend()
        require(found is not None and self.host["name"] in found[0]["placement"]["hosts"])
        firewall_check(self, found[0]["spec"], [found[1]] + self.ingress_ports())
        return {"changed": False}

    def vip_holder(self):
        """Read-only, during readiness: whether this ingress host holds its ingress's virtual IP now; readiness needs exactly one holder."""
        found = self.ingress_frontend()
        require(found is not None and self.host["name"] in found[0]["placement"]["hosts"])
        return {"changed": False, "holds": vip_held(self, found[0]["spec"])}

    def sso_proxy(self):
        """The unit's oauth2-proxy document: only a sign-on unit has one."""
        unit = self.request["service"]
        require(unit["type"] == ceph_signon_native.TYPE)
        return ceph_signon_native.document_of(unit)

    def signon_discovery(self):
        """Read-only, before any effect of a sign-on's apply, on one oauth2-proxy host: its issuer's discovery document proves the issuer."""
        return ceph_signon_native.discovery(self)

    def sso_config(self):
        """Read-only, after the unit's apply and on one oauth2-proxy host: each running oauth2-proxy container of this cluster here neither
        skips the identity provider's certificate verification in its arguments, environment or mounted configuration nor, with a bound
        CA bundle, mounts another bundle. A skip refuses; anything not yet proved is reported unproved. No path, byte, argument or
        environment value is returned."""
        proxy = self.sso_proxy()
        require(self.host["name"] in proxy["placement"]["hosts"])
        fsid = self.request["fsid"]
        prefix = "ceph-" + fsid + "-oauth2-proxy"
        rows = self.command([PODMAN, "ps", "--all", "--format", "json", "--filter", "name=^" + prefix + "[.-]"], timeout=SSO_TIMEOUT)
        require(isinstance(rows, list) and len(rows) <= MAX_PROXY_CONTAINERS and all(isinstance(row, dict) for row in rows), "native-output")
        bundle = self.bound.get(ceph_signon_native.CA_VARIABLE) if isinstance(self.bound, dict) else None
        daemons, proved = 0, True
        for row in rows:
            names = row.get("Names")
            require(isinstance(names, list) and len(names) == 1 and isinstance(names[0], str)
                    and re.fullmatch(re.escape(prefix) + r"[.-][A-Za-z0-9][A-Za-z0-9_.-]{0,127}", names[0]) is not None, "native-output")
            if row.get("State") != "running":
                continue
            identifier = row.get("Id")
            require(isinstance(identifier, str) and CONTAINER_ID.fullmatch(identifier) is not None, "native-output")
            inspected = self.command([PODMAN, "inspect", "--type", "container", identifier], timeout=SSO_TIMEOUT)
            require(isinstance(inspected, list) and len(inspected) == 1 and isinstance(inspected[0], dict), "native-output")
            verdict, where = ceph_signon_native.proxy_verdict(fsid, inspected[0], bundle)
            if verdict == "insecure":
                raise NativeFailure("sso-unverified", self.native_coordinate(), "host " + self.host["name"] + " daemon " + names[0]
                                    + ": oauth2-proxy skips identity provider certificate verification in its " + where)
            daemons += 1
            proved = proved and verdict == "verified"
        return {"changed": False, "daemons": daemons, "proved": proved}

    def sso(self):
        """The unit's one dashboard write, on the bootstrap host after every proxy host proved its rendered configuration: enable the
        dashboard's OAuth2 single sign-on unless it already is, then read it back. The dashboard refuses the enable until the
        orchestrator's daemon list shows a running oauth2-proxy, so until then nothing is written and the answer is pending. A repeat
        across a manager restart first reads the option back, so an enable the lost reply hid is never sent again."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.sso_proxy()
        require(all(self.markers.read("service-" + name + "-complete.json") is not None for name in self.unit_names()), "not-ready")
        if ceph_signon_native.sso_enabled(self):
            return {"changed": False, "pending": False}
        rows = self.manager_retry(self.ceph, "orch", "ps", "--daemon_type", "oauth2-proxy")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        if not any(row.get("daemon_type") == "oauth2-proxy" and type(row.get("status")) is int and row["status"] == 1 for row in rows):
            return {"changed": False, "pending": True}
        sent = []

        def enable():
            if sent and ceph_signon_native.sso_enabled(self):
                return
            sent.append(True)
            self.tunnel_effect("dashboard", "sso", "enable", "oauth2", json_result=False)

        self.manager_declare(enable, module=("dashboard", "ac-user-show"))
        require(ceph_signon_native.sso_enabled(self), "not-ready")
        return {"changed": True, "pending": False}

    def signon_resume_unit(self):
        """A sign-on resume's one stream: the frozen oauth2-proxy document again, its completion record kept from the apply."""
        self.sso_proxy()
        require(self.unit_inputs() is not None, "ownership")
        return self.unit(resume=True)

    def signon_baseline(self):
        """Read-only, on the bootstrap host before a suspension's first effect: each dependent daemon's last configuration time, which
        the controller records before it asks for the removal."""
        ceph_signon_native.require_signon(self)
        return {"changed": False, "daemons": ceph_signon_native.dependents(self.manager_retry(self.ceph, "orch", "ps"))}

    def signon_remove(self):
        """A suspension's one orchestrator effect: `ceph orch rm oauth2-proxy`, only of the document this block's own inputs and attempt
        records prove it applied; an absent service writes nothing and another one refuses sign-on-foreign."""
        ceph_signon_native.require_signon(self)
        values = self.unit_inputs()
        require(values is not None, "ownership")
        name = self.unit_names()[0]
        present = self.bound_specs(self.manager_retry(self.unit_specs)).get(name)
        if present is None:
            return {"changed": False, "configured": []}
        intent = {"scope": self.recovery_scope(), "service": values[0]}
        require(self.markers.read("service-" + name + "-attempt.json") == intent and present == values[0], "sign-on-foreign")
        configured = ceph_signon_native.dependents(self.manager_retry(self.ceph, "orch", "ps"))
        self.manager_declare(self.tunnel_effect, "orch", "rm", name, json_result=False)
        return {"changed": True, "configured": configured}

    def signon_state(self):
        """Read-only: what a suspension waits for, ceph_signon_native.suspension_state."""
        return ceph_signon_native.suspension_state(self)

    def signon_sso_disable(self):
        """A suspension's one dashboard write, only once cephadm lists no oauth2-proxy daemon and sso_oauth2 still reads true:
        `dashboard sso disable`, read back. A repeat across a manager restart reads the option first, so it is never sent twice."""
        ceph_signon_native.require_signon(self)
        rows = self.manager_retry(self.ceph, "orch", "ps", "--daemon_type", ceph_signon_native.TYPE)
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        if any(row.get("daemon_type") == ceph_signon_native.TYPE for row in rows):
            return {"changed": False, "pending": True}
        if not ceph_signon_native.sso_enabled(self):
            return {"changed": False, "pending": False}
        sent = []

        def disable():
            if sent and not ceph_signon_native.sso_enabled(self):
                return
            sent.append(True)
            self.tunnel_effect("dashboard", "sso", "disable", json_result=False)

        self.manager_declare(disable, module=("dashboard", "ac-user-show"))
        require(not ceph_signon_native.sso_enabled(self), "not-ready")
        return {"changed": True, "pending": False}

    def signon_local(self):
        """Read-only: local sign-in through the gateway, ceph_signon_native.local_sign_in."""
        return ceph_signon_native.local_sign_in(self)

    def grafana_gateway(self):
        """The unit's mgmt-gateway document: only a management gateway unit fronting a declared Grafana or declaring an oauth2-proxy has
        routes."""
        unit = self.request["service"]
        require(ceph_native_observability.routes_owed(unit))
        return ceph_native_observability.gateway_documents(unit)[1]

    def grafana_route(self):
        """Read-only, on the bootstrap host once the unit reads ready: which corrections the seams selected, by name, whose overrides
        are then complete, so every running gateway daemon must serve each of them."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.grafana_gateway()
        plans = [(descriptor, plan) for descriptor, plan in ceph_monitoring_template.gateway_plans(self, self.request["service"]) if plan is not None]
        require(all(ceph_monitoring_template.ready(self, plan, descriptor) for descriptor, plan in plans), "not-ready")
        selected = ceph_monitoring_template.gateway_corrections(self, self.request["service"])
        return {"changed": False, "routes": sorted(name for name in selected if name in ceph_native_observability.RENDERED_ROUTES)}

    def grafana_route_proof(self):
        """Read-only, on one gateway host: the running mgmt-gateway containers of this cluster here, and for each route how many
        serve it in the files they mount and started after those files were written. No path or byte is returned."""
        gateway = self.grafana_gateway()
        require(self.host["name"] in gateway["placement"]["hosts"])
        return self.gateway_served()

    def signon_routes(self):
        """Read-only, on the bootstrap host of a sign-on: the sign-on corrections this build's gateway-identity record selects for the
        cluster's management gateway, which every running gateway daemon must then render."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        unit = self.request["service"]
        require(unit["type"] == ceph_signon_native.TYPE)
        gateway = {"type": "mgmt-gateway", "documents": [], "monitoring": ["grafana"] if unit["signOn"].get("grafana") else []}
        selected = ceph_monitoring_template.gateway_corrections(self, gateway)
        return {"changed": False, "routes": sorted(name for name in selected if name in ceph_native_observability.SIGN_ON_ROUTES)}

    def signon_route_proof(self):
        """Read-only, on one management gateway host of a sign-on: what grafana_route_proof reads, for the sign-on routes."""
        unit = self.request["service"]
        require(unit["type"] == ceph_signon_native.TYPE and self.host["name"] in unit["signOn"]["hosts"])
        return self.gateway_served()

    def gateway_served(self):
        fsid = self.request["fsid"]
        prefix = "ceph-" + fsid + "-mgmt-gateway"
        rows = self.command([PODMAN, "ps", "--all", "--format", "json", "--filter", "name=^" + prefix + "[.-]"], timeout=SSO_TIMEOUT)
        require(isinstance(rows, list) and len(rows) <= MAX_GATEWAY_CONTAINERS and all(isinstance(row, dict) for row in rows), "native-output")
        daemons, served = 0, dict.fromkeys(ceph_native_observability.GATEWAY_ROUTES, 0)
        for row in rows:
            names = row.get("Names")
            require(isinstance(names, list) and len(names) == 1 and isinstance(names[0], str)
                    and re.fullmatch(re.escape(prefix) + r"[.-][A-Za-z0-9][A-Za-z0-9_.-]{0,127}", names[0]) is not None, "native-output")
            if row.get("State") != "running":
                continue
            identifier = row.get("Id")
            require(isinstance(identifier, str) and CONTAINER_ID.fullmatch(identifier) is not None, "native-output")
            inspected = self.command([PODMAN, "inspect", "--type", "container", identifier], timeout=SSO_TIMEOUT)
            require(isinstance(inspected, list) and len(inspected) == 1 and isinstance(inspected[0], dict), "native-output")
            daemons += 1
            for name, routed in ceph_native_observability.gateway_routed(fsid, inspected[0]).items():
                served[name] += routed
        return {"changed": False, "daemons": daemons, "served": served}

    def grafana_reconfig(self):
        """The Grafana routes' one effect, on the bootstrap host once a gateway host proved a running daemon without them: with the unit's
        documents and every selected override complete, ask cephadm to reconfigure the mgmt-gateway service, which re-renders each
        daemon's files from the stored overrides and restarts it. cephadm schedules the action durably and coalesces a repeat, so the
        request keeps no record; the controller asks it at most once per apply, and only on that proof."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        self.grafana_gateway()
        require(all(self.markers.read("service-" + name + "-complete.json") is not None for name in self.unit_names()), "not-ready")
        plans = [(descriptor, plan) for descriptor, plan in ceph_monitoring_template.gateway_plans(self, self.request["service"]) if plan is not None]
        require(bool(plans) and all(ceph_monitoring_template.ready(self, plan, descriptor) for descriptor, plan in plans), "not-ready")
        proof = self.transport_hold()
        with proof, self.cache_hold():
            self.ceph("orch", "reconfig", "mgmt-gateway", json_result=False)
        return {"changed": True}

    def manager_ports(self):
        """An apply's first evaluated call of the MGR port remedy, which may ask a recorded target again."""
        return self.manager_port_requests(True)

    def manager_ports_unrecorded(self):
        """Every later call of the same apply, which asks only a target no intent record names."""
        return self.manager_port_requests(False)

    def manager_port_requests(self, repeat):
        """The monitoring unit's one MGR effect, on the bootstrap host while its readiness names a MGR whose deployment ports lag the
        published module endpoints: the reconfigure requests ceph_native_observability.reconfigure_ports makes. It never enables a
        module; while the manager restarts or the module is unpublished nothing is evaluated and the answer is pending."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        unit = self.request["service"]
        require(unit["type"] == "monitoring" and ceph_native_observability.prometheus_declared(unit))
        require(all(self.markers.read("service-" + name + "-complete.json") is not None for name in self.unit_names()), "not-ready")
        changed, pending = ceph_native_observability.reconfigure_ports(self, repeat)
        return {"changed": changed, "pending": pending}

    def monitoring_credentials(self):
        """After the unit's readiness, on the bootstrap host and writing no record of its own: the basic-auth login cephadm keeps for each
        declared Prometheus and Alertmanager behind this management gateway, as the canonical JSON its protected output holds. Only a
        declared service is asked, because cephadm stores a default login for one it is asked about; the logins travel only in the
        result's private member, never in a refusal or a record."""
        return self.read_logins(self.login_services())

    def monitoring_credentials_held(self):
        """An observation's read of the same logins, only once cephadm must already hold each: a deployed daemon of a type whose
        configuration stores that login proves it, so the read stores none. Otherwise it reads none and answers no private member. The
        gateway's first apply sets a generated login for each declared service before its stream (gateway_claim), so the login read here
        is that one, or the one a later credential update set, never cephadm's admin/admin default."""
        services = self.login_services()
        rows = self.manager_retry(self.ceph, "orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        deployed = {row.get("daemon_type") for row in rows}
        if not all(LOGIN_HOLDERS[service] & deployed for service in services):
            return {"changed": False}
        return self.read_logins(services)

    def login_services(self):
        services = ceph_native_observability.credential_services(self.request["service"])
        require(self.request["action"] == "service" and bool(services), "request")
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        return services

    def read_logins(self, services):
        logins = {}
        for service in services:
            answer = self.manager_retry(self.ceph, "orch", service, "get-credentials")
            logins[service] = ceph_native_observability.native_login(answer, service, self.native_coordinate())
        return {"changed": False, "private": canonical(logins).decode("ascii")}

    def unfinished(self, evidence):
        if self.request["action"] != "service":
            return super().unfinished(evidence)
        evidence["owned"] = self.unit_inputs() is not None
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def nvmeof_unit_observe(self):
        require(self.request["service"]["type"] == "nvmeof")
        result = self.observe()
        return dict(result, blocking=list(self.blocking))

    def observe_resource(self, evidence, ready):
        if self.request["action"] != "service":
            return super().observe_resource(evidence, ready)
        unit = self.request["service"]
        hooks = UNIT_TYPES[unit["type"]]
        names = self.unit_names()
        values = self.unit_inputs()
        owned = values is not None
        with self.manager_served():
            stored = self.bound_specs(self.unit_specs())
            rows = self.ceph("orch", "ps")
        present = {name: stored[name] for name in names if name in stored}
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        running = [row for row in rows if row.get("service_name") in names]
        self.unit_rows = running
        require(owned or not present and not running, "foreign-service")
        evidence["owned"] = owned
        evidence["exists"] = owned or bool(present) or bool(running)
        completed = owned
        if owned:
            self.unit_records(values, present, self.recovery_scope())
            completed = all(self.markers.read("service-" + name + "-complete.json") is not None for name in names)
        running_ready = self.unit_daemons(hooks.expected(unit), running)
        for row in running:
            require(named(row.get("hostname")) and isinstance(row.get("daemon_type"), str) and isinstance(row.get("daemon_id"), str), "native-output")
        evidence["daemons"] = sorted(({"host": row["hostname"], "name": row["daemon_type"] + "." + row["daemon_id"], "service": row["service_name"]}
                                      for row in running), key=lambda item: item["name"])[:MAX_DAEMONS]
        unit_ready = owned and hooks.readiness(self, unit, self.blocking)
        evidence["sso"] = self.sso_protocol
        if hooks.endpoint is not None and owned:
            evidence["endpoint"] = hooks.endpoint(self, unit, running)
            unit_ready = unit_ready and evidence["endpoint"]["reachable"]
        return ready and completed and running_ready and unit_ready

    def pending(self, entry):
        if entry not in self.blocking:
            self.blocking.append(entry)

    def daemon_address(self, row, number):
        """The daemon's address when it listens on the frontend port: its reported IP, else its host's address."""
        ports = row.get("ports")
        if isinstance(ports, list) and ports and number not in ports:
            return None
        address = row.get("ip")
        try:
            address = str(ipaddress.ip_address(address)) if isinstance(address, str) else None
        except ValueError:
            address = None
        if address is None:
            host = next((item for item in self.request["hosts"] if item["name"] == row["hostname"]), None)
            require(host is not None, "foreign-service")
            address = host["address"]
        return address

    def unit_daemons(self, expected, running):
        """Each expected (service, daemon type) runs its count of daemons on its frozen image; a row on another image refuses."""
        auxiliary = {entry["name"]: entry["image"] for entry in self.request.get("auxiliary", [])}
        frozen_names = UNIT_TYPES[self.request["service"]["type"]].images if "service" in self.request else {}
        ready = True
        for (service, kind), count in expected.items():
            name = frozen_names.get(kind, kind)
            frozen = self.request["image"] if name is None else auxiliary.get(name)
            started = 0
            for row in running:
                if row["service_name"] != service or row.get("daemon_type") != kind:
                    continue
                images = row.get("container_image_digests") or []
                require(isinstance(images, list) and all(isinstance(image, str) for image in images), "native-output")
                require(not images or frozen in images, "artifact")
                started += type(row.get("status")) is int and row["status"] == 1 and frozen in images
            if started != count:
                ready = False
                self.pending(kind + "@" + service)
        for row in running:
            if (row["service_name"], row.get("daemon_type")) not in expected:
                ready = False
                self.pending(str(row.get("daemon_type")) + "@" + row["service_name"])
        return ready
