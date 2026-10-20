"""Fixed Ceph object gateway worker phases, shipped beside the core worker as source.

A gateway request carries one StorageObjectGateway: its realm, zone group and zone, created with radosgw-admin and never
with --default (src/rgw/radosgw-admin/radosgw-admin.cc:5110-5133), its zone's default-placement pools, the period
commit, then its rgw document and the ingress that fronts it applied as one `orch apply -i -` stream. Every radosgw-admin access to the config
store creates the shared `.rgw.root` pool when it is absent (src/rgw/driver/rados/config/impl.h:93,
src/rgw/driver/rados/rgw_tools.cc:24-52), so the pool is proved absent or claimed through monitor commands, and the
claim is written, before the first radosgw-admin call; no radosgw-admin call runs while the pool is absent, so a
recorded realm that is gone refuses with no effect. The gateway's record is written only after every name proof
passes, before its first create, so a refused name leaves no record. A realm, zone group or zone of the gateway's
names is adopted only under this operation's attempt record. The gateway never reads or creates an object user. A TLS service's
`ssl_cert` and `ssl_key` are bound Secret leaves: the documents are normalized and recorded with fixed placeholders, the leaves are spliced into
the apply stream in memory only, and readiness proves the served leaf by SHA-256. Like the core worker it imports only the target Python
standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import hashlib
import hmac
import ipaddress
import os
import re
import socket
import ssl
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_units
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, canonical, named, read_regular, require

APPLY_PHASES = ("gateway_zone", "gateway_placement", "gateway_service")
PHASES = frozenset(APPLY_PHASES)
RADOSGW_ADMIN = "/usr/bin/radosgw-admin"
RGW_ROOT = ".rgw.root"
ROOT_CLAIM = "rgw-root.json"
# cephadm's RGWSpec members the gateway realizes (src/python-common/ceph/deployment/service_spec.py, RGWSpec).
RGW_MEMBERS = frozenset(("certificate_source", "only_bind_port_on_networks", "rgw_exit_timeout_secs", "rgw_frontend_port", "rgw_frontend_type",
                         "rgw_realm", "rgw_zone", "rgw_zonegroup", "ssl"))
# IngressSpec members an rgw backend's ingress carries (IngressSpec, IBM python3-ceph-common 20.2.1 service_spec.py:2736-2800).
INGRESS_MEMBERS = frozenset(("backend_service", "certificate_source", "first_virtual_router_id", "frontend_port", "health_check_interval", "monitor_port",
                             "ssl", "virtual_interface_networks", "virtual_ip"))
LEAVES = {"/spec/ssl_cert": ("ssl_cert", "gatewayCertificate"), "/spec/ssl_key": ("ssl_key", "gatewayKey")}
# Non-secret stand-ins the SDK normalizes and the records hold in place of the bound leaves.
BOUND = {"ssl_cert": "bootwright-bound:/spec/ssl_cert", "ssl_key": "bootwright-bound:/spec/ssl_key"}
IMAGES = ("haproxy", "keepalived")
MAX_CERTIFICATE = 65536
MAX_KEY = 16384
MAX_LEAF = 16384
CERTIFICATE = re.compile(r"-----BEGIN CERTIFICATE-----\r?\n.*?-----END CERTIFICATE-----", re.S)
PRIVATE_KEY = re.compile(r"^-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----\r?$", re.M)
LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
ZONE_ID = ceph_native.SUBVOLUME_UUID
PLACEMENT = "default-placement"
# RGWSpec.get_port: 443 with ssl, else 80 (IBM python3-ceph-common 20.2.1 service_spec.py:1888-1909).
HTTP_PORT = 80
HTTPS_PORT = 443
PROBE_TIMEOUT = 5
PROBE_LIMIT = 256
PROBE_REQUEST = b"GET / HTTP/1.0\r\n\r\n"
STATUS_LINE = re.compile(rb"HTTP/1\.[01] [1-5][0-9]{2}(?: [^\r\n]*)?\r?\n")
PENDING = re.compile(r"(?:(?:rgw|http|tls)@rgw|tls@ingress)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
MEMBERS = frozenset(("bindings", "dataExtraPool", "dataPool", "documents", "endpoint", "indexPool", "name", "realm", "zone", "zoneGroup", "zonePools"))
KINDS = ("realm", "zonegroup", "zone")
LISTS = {"realm": "realms", "zonegroup": "zonegroups", "zone": "zones"}
MAX_NAMES = 1024
TOOL_LIMIT = 64 * 1024 * 1024


def label(value):
    return isinstance(value, str) and LABEL.fullmatch(value) is not None


def networks(value):
    require(isinstance(value, list) and 1 <= len(value) <= 8)
    for network in value:
        try:
            require(isinstance(network, str) and str(ipaddress.ip_network(network, strict=True)) == network)
        except ValueError:
            raise Refusal("request") from None


def port(value):
    return type(value) is int and 1 <= value <= 65535


def tls_members(spec):
    """`ssl` is a bool; an inline certificate source needs `ssl`; the leaves themselves are never frozen, only bound."""
    require("ssl" not in spec or type(spec["ssl"]) is bool)
    require("certificate_source" not in spec or spec["certificate_source"] == "inline" and spec.get("ssl") is True)


def validate_spec(spec, gateway):
    """The rgw document's spec: realm, zone group and zone are the gateway's, its frontend members, and TLS only through bindings."""
    require(isinstance(spec, dict) and set(spec) <= RGW_MEMBERS)
    tls_members(spec)
    require(spec.get("rgw_realm") == gateway["realm"] and spec.get("rgw_zonegroup") == gateway["zoneGroup"] and spec.get("rgw_zone") == gateway["zone"])
    require("rgw_frontend_port" not in spec or type(spec["rgw_frontend_port"]) is int and 1 <= spec["rgw_frontend_port"] <= 65535)
    require("rgw_frontend_type" not in spec or spec["rgw_frontend_type"] == "beast")
    require("only_bind_port_on_networks" not in spec or type(spec["only_bind_port_on_networks"]) is bool)
    require("rgw_exit_timeout_secs" not in spec or type(spec["rgw_exit_timeout_secs"]) is int and spec["rgw_exit_timeout_secs"] >= 1)


def validate_ingress(spec, backend):
    """The ingress that fronts the rgw document: its backend, a virtual IP interface, its ports and optional keepalived members."""
    require(isinstance(spec, dict) and set(spec) <= INGRESS_MEMBERS)
    require(spec.get("backend_service") == ceph_native_units.service_name(backend))
    try:
        virtual = spec.get("virtual_ip")
        require(isinstance(virtual, str) and "/" in virtual and str(ipaddress.ip_interface(virtual)) == virtual)
    except ValueError:
        raise Refusal("request") from None
    require(port(spec.get("frontend_port")) and port(spec.get("monitor_port")))
    require("first_virtual_router_id" not in spec or type(spec["first_virtual_router_id"]) is int and 1 <= spec["first_virtual_router_id"] <= 255)
    require("health_check_interval" not in spec or isinstance(spec["health_check_interval"], str) and 1 <= len(spec["health_check_interval"]) <= 16)
    if "virtual_interface_networks" in spec:
        networks(spec["virtual_interface_networks"])
    tls_members(spec)


def validate_bindings(gateway, documents):
    """No binding, or both leaves of the one TLS service, one Secret each, with an endpoint its certificate names."""
    bindings = gateway["bindings"]
    require(isinstance(bindings, list) and len(bindings) in (0, 2))
    for binding in bindings:
        require(isinstance(binding, dict) and set(binding) == {"path", "secretRef", "serviceRef"} and binding["path"] in LEAVES)
        require(named(binding["secretRef"]) and isinstance(binding["serviceRef"], str))
    secure = [ceph_native_units.service_name(document) for document in documents if document["spec"].get("ssl") is True]
    if not bindings:
        require(not secure)
        return
    require(bindings == sorted(bindings, key=lambda binding: (binding["serviceRef"], binding["path"])))
    require({binding["path"] for binding in bindings} == set(LEAVES))
    require(len({binding["serviceRef"] for binding in bindings}) == 1 and len({binding["secretRef"] for binding in bindings}) == 1)
    require(secure == [bindings[0]["serviceRef"]] and gateway["endpoint"] != "")


def validate_document(document, kind, request, hosts):
    """One placed document of its type, listed in `delegated` with exactly its hosts."""
    require(isinstance(document, dict) and {"placement", "service_id", "service_type", "spec"} <= set(document) <= ceph_native_units.DOCUMENT_MEMBERS)
    require(document["service_type"] == kind and named(document["service_id"]))
    placement = document["placement"]
    require(isinstance(placement, dict) and "hosts" in placement and set(placement) <= {"hosts", "count"})
    listed = placement["hosts"]
    names = [host["name"] for host in hosts]
    require(isinstance(listed, list) and bool(listed) and all(isinstance(host, str) and host in names for host in listed))
    require(len(set(listed)) == len(listed))
    require("count" not in placement or type(placement["count"]) is int and placement["count"] >= 1)
    delegated = {entry["name"]: entry["hosts"] for entry in request.get("delegated", [])}
    require(delegated.get(ceph_native_units.service_name(document)) == listed)
    if "networks" in document:
        networks(document["networks"])


def validate(request, hosts):
    """One gateway: its native names, its frozen pools and endpoint, its rgw document, the ingress that fronts it and their TLS bindings."""
    gateway = request.get("gateway")
    require(isinstance(gateway, dict) and set(gateway) == MEMBERS)
    require(named(gateway["name"]) and label(gateway["name"]))
    require(all(label(gateway[key]) for key in ("realm", "zoneGroup", "zone")))
    placement = [gateway[key] for key in ("indexPool", "dataPool", "dataExtraPool")]
    require(all(named(pool) for pool in placement))
    pools = gateway["zonePools"]
    require(isinstance(pools, list) and 1 <= len(pools) <= 16 and all(named(pool) for pool in pools) and pools == sorted(set(pools)))
    require(set(placement) <= set(pools))
    endpoint = gateway["endpoint"]
    require(isinstance(endpoint, str) and (endpoint == "" or len(endpoint) <= 253 and all(label(part) for part in endpoint.split("."))))
    documents = gateway["documents"]
    require(isinstance(documents, list) and 1 <= len(documents) <= 2)
    validate_document(documents[0], "rgw", request, hosts)
    validate_spec(documents[0]["spec"], gateway)
    if len(documents) == 2:
        validate_document(documents[1], "ingress", request, hosts)
        validate_ingress(documents[1]["spec"], documents[0])
        require(documents[0]["spec"].get("ssl") is not True)
        auxiliary = request.get("auxiliary", [])
        require(isinstance(auxiliary, list) and set(IMAGES) <= {entry.get("name") for entry in auxiliary if isinstance(entry, dict)})
    validate_bindings(gateway, documents)


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


def material(request, phase, private):
    """The bound leaves a phase may hold: certificate and key to the service apply, the certificate to observation and removal."""
    require(isinstance(private, dict), "material")
    expected = set()
    if request["gateway"]["bindings"]:
        expected = {"gateway_service": {"gatewayCertificate", "gatewayKey"}, "observe": {"gatewayCertificate"},
                    "destroy": {"gatewayCertificate"}}.get(phase, set())
    require(set(private) == expected, "material")
    if "gatewayCertificate" in private:
        leaf(private["gatewayCertificate"])
    if "gatewayKey" in private:
        key = private["gatewayKey"]
        require(isinstance(key, str) and len(key.encode("utf-8")) <= MAX_KEY and PRIVATE_KEY.search(key) is not None, "material")
    return private


def zone_pools(value):
    """Every pool a zone names: each non-empty value of a `*_pool` or `domain_root` member, cut at its namespace."""
    found = set()
    if isinstance(value, dict):
        for key, item in value.items():
            if isinstance(item, str) and isinstance(key, str) and (key.endswith("_pool") or key == "domain_root"):
                if item:
                    found.add(item.partition(":")[0])
            else:
                found |= zone_pools(item)
    elif isinstance(value, list):
        for item in value:
            found |= zone_pools(item)
    return found


def text(value):
    return value if isinstance(value, str) else ""


def default_placement(zone):
    """The default-placement index, STANDARD data and data-extra pools; `placement_pools` reads as a list of key/val entries or a mapping."""
    entries = zone.get("placement_pools") if isinstance(zone, dict) else None
    if isinstance(entries, list):
        entries = {entry.get("key"): entry.get("val") for entry in entries if isinstance(entry, dict) and isinstance(entry.get("key"), str)}
    target = entries.get(PLACEMENT) if isinstance(entries, dict) else None
    if not isinstance(target, dict):
        return "", "", ""
    classes = target.get("storage_classes")
    standard = classes.get("STANDARD") if isinstance(classes, dict) else None
    data = text(standard.get("data_pool")) if isinstance(standard, dict) else ""
    return text(target.get("index_pool")), data, text(target.get("data_extra_pool"))


def identities(value):
    """The IDs of a zone group's zones or a period's zone groups: a list of objects or IDs, or a mapping of them."""
    entries = list(value.values()) if isinstance(value, dict) else value if isinstance(value, list) else []
    return {entry.get("id") if isinstance(entry, dict) else entry for entry in entries if isinstance(entry, (dict, str))}


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


def served(address, number, name):
    """The DER leaf a TLS frontend serves for SNI name and whether an HTTP status line follows; trust is the caller's exact fingerprint."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    deadline, presented = time.monotonic() + PROBE_TIMEOUT, None
    try:
        with socket.create_connection((address, number), timeout=PROBE_TIMEOUT) as raw:
            with context.wrap_socket(raw, server_hostname=name) as channel:
                presented = channel.getpeercert(binary_form=True)
                return presented, replied(channel, b"GET / HTTP/1.0\r\nHost: " + name.encode("ascii") + b"\r\n\r\n", deadline)
    except (OSError, ValueError):
        return presented, False


class ObjectWorker(ceph_native_units.UnitWorker):
    """The core worker with one object gateway's zone, placement and service phases and its observation."""

    tls = None

    def gateway(self):
        return self.request["gateway"]

    def unit_key(self):
        return "gateway-" + self.gateway()["name"]

    def bound_service(self):
        bindings = self.gateway()["bindings"]
        return bindings[0]["serviceRef"] if bindings else None

    def unit_groups(self):
        """Each document; the TLS service's holds the placeholders, so the SDK, the records and every comparison see no leaf."""
        bound = self.bound_service()
        return [[dict(document, spec=dict(document["spec"], **BOUND)) if ceph_native_units.service_name(document) == bound else document]
                for document in self.gateway()["documents"]]

    def leaves(self):
        require(isinstance(self.tls, dict) and "gatewayCertificate" in self.tls, "material")
        return self.tls

    def leaf_digest(self):
        return hashlib.sha256(leaf(self.leaves()["gatewayCertificate"])).hexdigest()

    def splice(self, value):
        """The apply stream's copy of one normalized document, the bound leaves in place of their placeholders; never recorded."""
        if ceph_native_units.service_name(value) != self.bound_service():
            return value
        spec = value.get("spec")
        require(isinstance(spec, dict) and all(spec.get(member) == placeholder for member, placeholder in BOUND.items()), "native-output")
        bound = self.leaves()
        require("gatewayKey" in bound, "material")
        return dict(value, spec=dict(spec, ssl_cert=bound["gatewayCertificate"], ssl_key=bound["gatewayKey"]))

    def tls_specs(self, stored):
        """The stored TLS service in placeholder form: its leaves omitted, or equal to the bound ones; any other leaf is not this Secret's."""
        bound = self.bound_service()
        value = stored.get(bound) if bound is not None else None
        spec = value.get("spec") if isinstance(value, dict) else None
        if not isinstance(spec, dict):
            return stored
        held = self.leaves()

        def same(member, name):
            return isinstance(spec.get(member), str) and hmac.compare_digest(spec[member].encode("utf-8"), held[name].encode("utf-8"))

        omitted = "ssl_cert" not in spec and "ssl_key" not in spec
        key = same("ssl_key", "gatewayKey") if "gatewayKey" in held else isinstance(spec.get("ssl_key"), str)
        if not omitted and not (same("ssl_cert", "gatewayCertificate") and key):
            raise NativeFailure("object-endpoint-certificate", self.native_coordinate(), bound)
        return dict(stored, **{bound: dict(value, spec=dict(spec, **BOUND))})

    def ingress_images(self):
        """With an ingress, the settings block's frozen haproxy and keepalived settings hold; the gateway only reads them."""
        if len(self.gateway()["documents"]) < 2:
            return
        unused, held = self.image_settings()
        missing = sorted(self.image_key(name) for name in IMAGES if self.image_key(name) not in held)
        if missing:
            raise NativeFailure("foreign-image", self.native_coordinate(), ", ".join(missing))

    def unit_names(self):
        return [ceph_native_units.service_name(document) for document in self.gateway()["documents"]]

    def recovery_scope(self):
        """The cluster's bootstrap scope: the gateway member is this block's own input, never part of the cluster's authoring."""
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "gateway"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def record(self, suffix):
        return self.unit_key() + "-" + suffix + ".json"

    def frozen(self, kind):
        return self.gateway()[{"realm": "realm", "zonegroup": "zoneGroup", "zone": "zone"}[kind]]

    def site(self, *kinds):
        options = {"realm": "--rgw-realm", "zonegroup": "--rgw-zonegroup", "zone": "--rgw-zone"}
        return tuple(token for kind in kinds for token in (options[kind], self.frozen(kind)))

    def intent(self):
        gateway = self.gateway()
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "gateway": gateway["name"], "realm": gateway["realm"],
                "zoneGroup": gateway["zoneGroup"], "zone": gateway["zone"]}

    def placement_intent(self):
        gateway = self.gateway()
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "gateway": gateway["name"], "indexPool": gateway["indexPool"],
                "dataPool": gateway["dataPool"], "dataExtraPool": gateway["dataExtraPool"], "zonePools": gateway["zonePools"]}

    def root_claim(self):
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "pool": RGW_ROOT}

    def foreign(self, detail):
        return NativeFailure("object-realm-foreign", self.native_coordinate(), detail)

    def radosgw(self, *args, json_result=True):
        """radosgw-admin with the cluster's original admin custody, as `ceph` runs; never a user, key, caps or metadata command."""
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            return self.command([
                RADOSGW_ADMIN, "--conf", os.path.join(self.markers.path, "ceph.conf"),
                "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"),
                "--format", "json", *args,
            ], json_result=json_result)

    def tool_digest(self):
        require(os.path.isfile(RADOSGW_ADMIN) and os.access(RADOSGW_ADMIN, os.X_OK), "host-prerequisite")
        return hashlib.sha256(read_regular(RADOSGW_ADMIN, maximum=TOOL_LIMIT)).hexdigest()

    def owned_cluster(self):
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")

    def root_present(self):
        """`.rgw.root` exists, by monitor command only; when it does, it carries the claim the first gateway wrote."""
        present = any(isinstance(entry, dict) and entry.get("pool_name") == RGW_ROOT for entry in self.pools())
        if present and self.markers.read(ROOT_CLAIM) != self.root_claim():
            raise self.foreign(RGW_ROOT)
        return present

    def listed(self):
        names = {}
        for kind in KINDS:
            value = self.radosgw(kind, "list")
            entries = value.get(LISTS[kind]) if isinstance(value, dict) else None
            require(isinstance(entries, list) and len(entries) <= MAX_NAMES and all(isinstance(name, str) for name in entries), "native-output")
            names[kind] = set(entries)
        return names

    def recorded(self):
        """This gateway's recorded native IDs by kind; an attempt record is always this gateway's intent."""
        ids = {}
        for kind in KINDS:
            attempt = self.markers.read(self.record(kind + "-attempt"))
            require(attempt is None or attempt == self.intent(), "ownership")
            identity = self.markers.read(self.record(kind + "-id"))
            if identity is not None:
                require(isinstance(identity, dict) and set(identity) == {"id"} and isinstance(identity["id"], str)
                        and ZONE_ID.fullmatch(identity["id"]) is not None, "ownership")
                ids[kind] = identity["id"]
        return ids

    def foreign_check(self, names):
        """Before any create: a listed name needs this gateway's record, and a recorded one must still be listed."""
        ids = self.recorded()
        for kind in KINDS:
            name = self.frozen(kind)
            if name in names[kind] and kind not in ids and self.markers.read(self.record(kind + "-attempt")) is None:
                raise self.foreign(kind + " " + name)
            if name not in names[kind] and kind in ids:
                raise self.foreign(kind + " " + name + " is gone")
        return ids

    def get(self, kind):
        arguments = {"realm": ("realm",), "zonegroup": ("realm", "zonegroup"), "zone": ("realm", "zone")}[kind]
        value = self.radosgw(kind, "get", *self.site(*arguments))
        require(isinstance(value, dict), "native-output")
        return value

    def proved(self, kind, value, ids):
        """The entity under its frozen name in its recorded realm, with the recorded ID when one is recorded."""
        identifier = value.get("id")
        require(isinstance(identifier, str) and ZONE_ID.fullmatch(identifier) is not None, "native-output")
        name = self.frozen(kind)
        if value.get("name") != name or kind != "realm" and value.get("realm_id") != ids.get("realm") or ids.get(kind, identifier) != identifier:
            raise self.foreign(kind + " " + name + " changed")
        return identifier

    def master_zone(self, names, ids):
        """Realm, zone group and zone in order: proved by recorded ID, adopted under this gateway's attempt record, or created without --default."""
        creates = {"realm": ("realm", "create") + self.site("realm"),
                   "zonegroup": ("zonegroup", "create") + self.site("realm", "zonegroup") + ("--master",),
                   "zone": ("zone", "create") + self.site("realm", "zonegroup", "zone") + ("--master",)}
        changed = False
        for kind in KINDS:
            if self.frozen(kind) not in names[kind]:
                self.markers.write(self.record(kind + "-attempt"), self.intent())
                self.radosgw(*creates[kind], json_result=False)
                changed = True
            identifier = self.proved(kind, self.get(kind), ids)
            self.markers.write(self.record(kind + "-id"), {"id": identifier})
            ids[kind] = identifier
        return changed

    def member(self, ids):
        value = self.get("zonegroup")
        self.proved("zonegroup", value, ids)
        return ids["zone"] in identities(value.get("zones")) and value.get("master_zone") == ids["zone"]

    def gateway_zone(self):
        """Every proof before any write; the claim before any radosgw-admin call; the record once no name refuses; then realm, zone group and zone."""
        self.owned_cluster()
        self.ingress_images()
        self.verify_tool()
        digest = self.tool_digest()
        tool = self.markers.read(self.record("tool"))
        require(tool is None or tool == {"sha256": digest}, "artifact")
        present = self.root_present()
        names = self.listed() if present else {kind: set() for kind in KINDS}
        ids = self.foreign_check(names)
        if not present:
            self.markers.write(ROOT_CLAIM, self.root_claim())
        self.markers.write(self.record("intent"), self.intent())
        self.markers.write(self.record("tool"), {"sha256": digest})
        changed = self.master_zone(names, ids)
        if not self.member(ids):
            self.radosgw("zonegroup", "add", *self.site("realm", "zonegroup", "zone"), "--master", json_result=False)
            changed = True
            require(self.member(ids), "not-ready")
        return {"changed": changed}

    def proved_records(self):
        """The owned cluster, this gateway's record and all three recorded IDs, each still listed under its name."""
        self.owned_cluster()
        require(self.markers.read(self.record("intent")) == self.intent(), "ownership")
        ids = self.foreign_check(self.listed() if self.root_present() else {kind: set() for kind in KINDS})
        require(len(ids) == len(KINDS), "ownership")
        return ids

    def zone(self, ids):
        value = self.get("zone")
        self.proved("zone", value, ids)
        return value

    def zone_check(self, zone):
        """Every pool the zone names is planned, and default-placement holds exactly the frozen pools; never the zone's content."""
        gateway = self.gateway()
        unplanned = sorted(zone_pools(zone) - set(gateway["zonePools"]))
        if unplanned:
            raise NativeFailure("object-zone-pools", self.native_coordinate(), ", ".join(unplanned))
        if default_placement(zone) != (gateway["indexPool"], gateway["dataPool"], gateway["dataExtraPool"]):
            raise NativeFailure("object-zone-pools", self.native_coordinate(), PLACEMENT)

    def committed(self, ids):
        period = self.radosgw("period", "get", *self.site("realm"))
        require(isinstance(period, dict), "native-output")
        if (period.get("realm_id"), period.get("master_zonegroup"), period.get("master_zone")) != (ids["realm"], ids["zonegroup"], ids["zone"]):
            return False
        groups = period.get("period_map", {}).get("zonegroups") if isinstance(period.get("period_map"), dict) else None
        entries = list(groups.values()) if isinstance(groups, dict) else groups if isinstance(groups, list) else []
        return any(isinstance(group, dict) and group.get("id") == ids["zonegroup"] and ids["zone"] in identities(group.get("zones"))
                   for group in entries)

    def gateway_placement(self):
        """Bind the frozen pools to default-placement, refuse an unplanned zone pool, then commit the period; RGW is not applied yet."""
        ids = self.proved_records()
        gateway = self.gateway()
        zone = self.zone(ids)
        flags = []
        for flag, current, wanted in zip(("--index-pool", "--data-pool", "--data-extra-pool"), default_placement(zone),
                                         (gateway["indexPool"], gateway["dataPool"], gateway["dataExtraPool"])):
            if current != wanted:
                flags += [flag, wanted]
        changed = bool(flags)
        if flags:
            self.radosgw("zone", "placement", "modify", *self.site("realm", "zonegroup", "zone"), "--placement-id", PLACEMENT, *flags, json_result=False)
            zone = self.zone(ids)
        self.zone_check(zone)
        self.markers.write(self.record("placement"), self.placement_intent())
        if not self.committed(ids):
            self.radosgw("period", "update", "--commit", *self.site("realm", "zonegroup", "zone"), json_result=False)
            changed = True
            require(self.committed(ids), "not-ready")
        return {"changed": changed}

    def gateway_service(self):
        """After the committed period only: the rgw document's inputs record, attempts, one stdin batch, readback and completions."""
        ids = self.proved_records()
        self.zone(ids)
        placement = self.markers.read(self.record("placement"))
        require(placement is None or placement == self.placement_intent(), "ownership")
        require(placement is not None and self.committed(ids), "not-ready")
        self.ingress_images()
        values = [group[0] for group in self.normalized_services(self.unit_groups(), record=self.unit_key() + "-inputs.json")]
        scope = self.recovery_scope()
        observed = self.tls_specs(self.unit_specs())
        self.unit_records(values, observed, scope)
        names = self.unit_names()
        missing = [(name, value) for name, value in zip(names, values) if name not in observed]
        for name, value in missing:
            self.markers.write("service-" + name + "-attempt.json", {"scope": scope, "service": value})
        if missing:
            data = b"---\n".join(canonical(self.splice(value)) + b"\n" for unused, value in missing)
            self.tunnel_effect("orch", "apply", "-i", "-", data=data, json_result=False)
            observed = self.tls_specs(self.unit_specs())
            require(all(observed.get(name) == value for name, value in zip(names, values)), "not-ready")
        for name, value in zip(names, values):
            self.markers.write("service-" + name + "-complete.json", {"scope": scope, "service": value})
        return {"changed": bool(missing)}

    def unfinished(self, evidence):
        if self.request["action"] != "gateway":
            return super().unfinished(evidence)
        evidence["owned"] = self.markers.read(self.record("intent")) == self.intent()
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def observe_resource(self, evidence, ready):
        if self.request["action"] != "gateway":
            return super().observe_resource(evidence, ready)
        record = self.markers.read(self.record("intent"))
        require(record is None or record == self.intent(), "ownership")
        owned = record is not None
        present = self.root_present()
        names = self.listed() if present else {kind: set() for kind in KINDS}
        ids = self.foreign_check(names)
        services = self.unit_names()
        stored = self.tls_specs(self.unit_specs())
        specs = {name: stored[name] for name in services if name in stored}
        with self.manager_served():
            rows = self.ceph("orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        running = [row for row in rows if row.get("service_name") in services]
        for row in running:
            require(named(row.get("hostname")) and isinstance(row.get("daemon_type"), str) and isinstance(row.get("daemon_id"), str), "native-output")
        require(owned or not specs and not running, "foreign-service")
        evidence["owned"] = owned
        evidence["exists"] = owned or any(self.frozen(kind) in names[kind] for kind in KINDS) or bool(specs) or bool(running)
        proved = owned and self.observe_gateway(evidence, ids, specs)
        documents = self.gateway()["documents"]
        backend = self.gateway_daemons([row for row in running if row["service_name"] == services[0]])
        ingress, reachable = True, backend
        if len(documents) == 2:
            ingress = self.ingress_daemons(documents[1], [row for row in running if row["service_name"] == services[1]])
            reachable = self.endpoint_ready(documents[1])
        tls = self.bound_service() is not None
        evidence["endpoint"] = {"certificateSHA256": self.leaf_digest() if tls and reachable else "", "reachable": reachable}
        evidence["daemons"] = sorted(({"host": row["hostname"], "name": row["daemon_type"] + "." + row["daemon_id"], "service": row["service_name"]}
                                      for row in running), key=lambda item: item["name"])[:ceph_native_units.MAX_DAEMONS]
        return ready and owned and proved and backend and ingress and reachable

    def observe_gateway(self, evidence, ids, specs):
        """Read-only: the recorded IDs, the zone's pools and placement, membership, the committed period and every service record."""
        if len(ids) != len(KINDS):
            return False
        self.ingress_images()
        zone = self.zone(ids)
        evidence["resource"] = {"id": ids["zone"], "path": self.gateway()["endpoint"]}
        placement = self.markers.read(self.record("placement"))
        require(placement is None or placement == self.placement_intent(), "ownership")
        if placement is not None:
            self.zone_check(zone)
        ready = placement is not None and self.member(ids) and self.committed(ids)
        values = self.unit_inputs()
        if values is None:
            return False
        self.unit_records(values, specs, self.recovery_scope())
        return ready and all(self.markers.read("service-" + name + "-complete.json") is not None for name in self.unit_names())

    def gateway_daemons(self, running):
        """The placement count of rgw daemons on the frozen Ceph image, each answering on its frontend port: over TLS with the bound leaf
        when the rgw service terminates TLS, else an HTTP status line."""
        document = self.gateway()["documents"][0]
        service = ceph_native_units.service_name(document)
        count = document["placement"].get("count", len(document["placement"]["hosts"]))
        secure = document["spec"].get("ssl") is True
        number = document["spec"].get("rgw_frontend_port", HTTPS_PORT if secure else HTTP_PORT)
        image = self.request["image"]
        started = []
        ready = True
        for row in running:
            images = row.get("container_image_digests") or []
            require(isinstance(images, list) and all(isinstance(item, str) for item in images), "native-output")
            if row["daemon_type"] != "rgw":
                ready = False
                self.pending("rgw@" + service)
                continue
            require(not images or image in images, "artifact")
            if type(row.get("status")) is int and row["status"] == 1 and image in images:
                started.append(row)
        if len(started) != count:
            ready = False
            self.pending("rgw@" + service)
        for row in started:
            address = self.frontend(row, number)
            blocked = "http" if address is None else self.secured(address, number) if secure else None if answers(address, number) else "http"
            if blocked is not None:
                ready = False
                self.pending(blocked + "@" + service)
        return ready

    def secured(self, address, number):
        """None when the TLS frontend served the bound leaf for the endpoint's name and an HTTP status line, else what blocked."""
        presented, status = served(address, number, self.gateway()["endpoint"])
        if not isinstance(presented, bytes) or not 0 < len(presented) <= MAX_LEAF:
            return "tls"
        if not hmac.compare_digest(hashlib.sha256(presented).hexdigest(), self.leaf_digest()):
            return "tls"
        return None if status else "http"

    def ingress_daemons(self, ingress, running):
        """The ingress placement count of haproxy and keepalived daemons, each on its frozen auxiliary image."""
        name = ceph_native_units.service_name(ingress)
        count = ingress["placement"].get("count", len(ingress["placement"]["hosts"]))
        return self.unit_daemons({(name, "haproxy"): count, (name, "keepalived"): count}, running)

    def endpoint_ready(self, ingress):
        """The virtual IP at the frontend port: the bound leaf and a status line over TLS, else a status line."""
        name, spec = ceph_native_units.service_name(ingress), ingress["spec"]
        address, number = spec["virtual_ip"].split("/")[0], spec["frontend_port"]
        blocked = self.secured(address, number) if spec.get("ssl") is True else None if answers(address, number) else "http"
        if blocked is not None:
            self.pending(("tls@" if blocked == "tls" else "vip@") + name)
        return blocked is None

    def frontend(self, row, number):
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
