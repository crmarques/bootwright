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
the apply stream in memory only, and readiness proves the served leaf by SHA-256. A user request carries one StorageObjectUser in its
gateway's zone: `radosgw-admin user create` without key arguments generates its S3 key pair (src/rgw/radosgw-admin/radosgw-admin.cc:6872-6875),
so no key ever reaches an argument; the pair crosses only stdout and is retained in custody before it is produced. A bucket request carries one
StorageObjectBucket: radosgw-admin creates no bucket and puts no bucket policy (its command table, src/rgw/radosgw-admin/radosgw-admin.cc:946-977),
so the worker signs S3 CreateBucket and PutBucketPolicy with AWS Signature Version 4 under its owner's key pair, read in memory from `user info`
and never answered, logged or recorded, and sends them to an rgw daemon of the gateway's backend service; over TLS the daemon's leaf is pinned by
SHA-256 to the bound certificate before any byte is sent. Like the core worker it imports only the target Python standard library and the core
worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import hashlib
import hmac
import http.client
import ipaddress
import json
import os
import re
import socket
import ssl
import time
import unicodedata
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_units
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    Custody, NativeFailure, Refusal, canonical, named, read_regular, require, unique_object,
)
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native_observability import MAX_KEY, MAX_LEAF, PRIVATE_KEY, answers, leaf, served

APPLY_PHASES = ("gateway_zone", "gateway_placement", "gateway_service")
PHASES = frozenset(APPLY_PHASES + ("ingress_firewall",))
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
LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
ZONE_ID = ceph_native.SUBVOLUME_UUID
PLACEMENT = "default-placement"
# RGWSpec.get_port: 443 with ssl, else 80 (IBM python3-ceph-common 20.2.1 service_spec.py:1888-1909).
HTTP_PORT = 80
HTTPS_PORT = 443
PENDING = re.compile(r"(?:(?:rgw|http|tls)@rgw|tls@ingress)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
MEMBERS = frozenset(("bindings", "dataExtraPool", "dataPool", "documents", "endpoint", "indexPool", "name", "realm", "zone", "zoneGroup", "zonePools"))
KINDS = ("realm", "zonegroup", "zone")
LISTS = {"realm": "realms", "zonegroup": "zonegroups", "zone": "zones"}
MAX_NAMES = 1024
TOOL_LIMIT = 64 * 1024 * 1024
USER_APPLY_PHASES = ("object_user",)
USER_READ_ONLY_PHASES = frozenset(("object_user_read",))
USER_PHASES = frozenset(USER_APPLY_PHASES) | USER_READ_ONLY_PHASES
USER_MEMBERS = frozenset(("displayName", "gateway", "name", "realm", "zone", "zoneGroup"))
# the mgr dashboard module creates a system user of this ID in each realm (src/pybind/mgr/dashboard/services/rgw_client.py); a StorageObjectUser never takes it.
RESERVED_USERS = ("dashboard",)
MAX_USERS = 65536
MAX_DISPLAY = 256
KEY_LIMIT = 128
MAX_MATERIAL = 4096
MATERIAL_MEMBERS = frozenset(("accessKey", "secretKey", "uid"))
BUCKET_APPLY_PHASES = ("object_bucket",)
BUCKET_PHASES = frozenset(BUCKET_APPLY_PHASES)
BUCKET_MEMBERS = frozenset(("backend", "certificateRef", "endpoint", "gateway", "name", "owner", "policy", "port", "realm", "zone", "zoneGroup"))
MAX_BUCKETS = 65536
MAX_POLICY = 65536
MAX_RESPONSE = MAX_POLICY + 1
S3_SERVICE = "s3"
S3_TIMEOUT = 30
SIGNING = "AWS4-HMAC-SHA256"
UNRESERVED = "-_.~"
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()
S3_CODE = re.compile(rb"<Code>([A-Za-z0-9.]{1,64})</Code>")
S3_MESSAGE = re.compile(rb"<Message>([ -~]{0,256})</Message>")
BUCKET_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,255}\Z")


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


def display_name(value):
    """Text of at most 256 bytes without control characters, as admission admits, and not beginning with `-`, which radosgw-admin's
    argument parsing would take for an option."""
    return (isinstance(value, str) and 0 < len(value.encode("utf-8")) <= MAX_DISPLAY and not value.startswith("-")
            and not any(unicodedata.category(c) == "Cc" or c in "  " for c in value))


def visible(key):
    """An S3 key as Bootwright retains it: 1 to 128 visible ASCII characters other than `&`, `<` and `>`, which the controller's canonical
    JSON escapes. radosgw-admin generates 20 of 0-9A-Z and 40 of A-Za-z0-9 (src/rgw/driver/rados/rgw_user.h:22-23,
    src/common/random_string.cc); Bootwright never pins that format."""
    return isinstance(key, str) and 0 < len(key) <= KEY_LIMIT and all("!" <= c <= "~" and c not in "&<>" for c in key)


def validate_user(request, hosts):
    """One object user of one gateway: its native user ID, never the reserved `dashboard`, its display name and its gateway's names."""
    del hosts
    user = request.get("objectUser")
    require(isinstance(user, dict) and set(user) == USER_MEMBERS)
    require(named(user["name"]) and label(user["name"]) and user["name"] not in RESERVED_USERS)
    require(named(user["gateway"]) and label(user["gateway"]) and all(label(user[key]) for key in ("realm", "zoneGroup", "zone")))
    require(display_name(user["displayName"]))


def user_material(data, uid, reason="object-user-identity"):
    """The retained key pair: canonical JSON of exactly {accessKey, secretKey, uid} for this user."""
    try:
        require(isinstance(data, bytes) and 0 < len(data) <= MAX_MATERIAL, reason)
        value = json.loads(data, object_pairs_hook=unique_object)
        require(isinstance(value, dict) and set(value) == MATERIAL_MEMBERS and value["uid"] == uid, reason)
        require(visible(value["accessKey"]) and visible(value["secretKey"]) and canonical(value) == data, reason)
        return value
    except (ValueError, UnicodeError, TypeError):
        raise Refusal(reason) from None


def validate_bucket(request, hosts):
    """One bucket of one owner in one gateway's zone: its native name, its backend service and port, TLS only with an endpoint and a bound
    certificate, and its policy, empty or the canonical text of one JSON object."""
    del hosts
    bucket = request.get("objectBucket")
    require(isinstance(bucket, dict) and set(bucket) == BUCKET_MEMBERS)
    require(named(bucket["name"]) and label(bucket["name"]) and len(bucket["name"]) >= 3)
    require(named(bucket["owner"]) and label(bucket["owner"]) and bucket["owner"] not in RESERVED_USERS)
    require(named(bucket["gateway"]) and label(bucket["gateway"]) and all(label(bucket[key]) for key in ("realm", "zoneGroup", "zone")))
    require(isinstance(bucket["backend"], str) and bucket["backend"].startswith("rgw.") and named(bucket["backend"][4:]))
    require(port(bucket["port"]))
    endpoint, certificate = bucket["endpoint"], bucket["certificateRef"]
    require(isinstance(certificate, str) and (certificate == "" or named(certificate)))
    require(isinstance(endpoint, str) and (endpoint == "") == (certificate == ""))
    require(endpoint == "" or len(endpoint) <= 253 and all(label(part) for part in endpoint.split(".")))
    policy = bucket["policy"]
    require(isinstance(policy, str) and len(policy.encode("utf-8")) <= MAX_POLICY)
    if policy:
        try:
            require(isinstance(json.loads(policy, object_pairs_hook=unique_object), dict))
        except ValueError:
            raise Refusal("request") from None


def bucket_material(request, phase, private):
    """With TLS at the backend, the bound certificate each phase that sends S3 requests pins the daemon's leaf to; never a key."""
    require(isinstance(private, dict), "material")
    expected = {"gatewayCertificate"} if request["objectBucket"]["certificateRef"] and phase in BUCKET_PHASES | {"observe", "destroy"} else set()
    require(set(private) == expected, "material")
    if expected:
        leaf(private["gatewayCertificate"])
    return private


def amz_quote(value):
    return urllib.parse.quote(value, safe=UNRESERVED)


def canonical_query(query):
    """Each name=value pair URI-encoded and sorted; a name without `=` has an empty value."""
    pairs = []
    for part in query.split("&") if query else ():
        name, unused, value = part.partition("=")
        pairs.append((amz_quote(urllib.parse.unquote(name)), amz_quote(urllib.parse.unquote(value))))
    return "&".join(name + "=" + value for name, value in sorted(pairs))


def canonical_request(method, path, query, headers, payload_hash):
    """AWS Signature Version 4 canonical request: S3 paths are encoded once and never normalized; header names lowercased and sorted, values
    trimmed with inner runs of spaces collapsed."""
    entries = sorted((name.lower(), " ".join(value.split())) for name, value in headers.items())
    signed = ";".join(name for name, unused in entries)
    lines = [method, urllib.parse.quote(path, safe="/" + UNRESERVED), canonical_query(query)]
    lines += [name + ":" + value for name, value in entries]
    return "\n".join(lines + ["", signed, payload_hash]), signed


def sigv4(method, path, query, headers, payload_hash, pair, region, service, stamp):
    """The Authorization value of one request under Signature Version 4: headers must hold host and x-amz-date equal to stamp."""
    request, signed = canonical_request(method, path, query, headers, payload_hash)
    scope = stamp[:8] + "/" + region + "/" + service + "/aws4_request"
    text = "\n".join((SIGNING, stamp, scope, hashlib.sha256(request.encode("utf-8")).hexdigest()))
    key = ("AWS4" + pair["secretKey"]).encode("utf-8")
    for part in (stamp[:8], region, service, "aws4_request"):
        key = hmac.new(key, part.encode("utf-8"), hashlib.sha256).digest()
    signature = hmac.new(key, text.encode("utf-8"), hashlib.sha256).hexdigest()
    return SIGNING + " Credential=" + pair["accessKey"] + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + signature


def s3_exchange(address, number, method, path, query, body, pair, region, *, pinned=None, clock=time.time):
    """One signed S3 request to one daemon and its (status, code, message, body), or None when the daemon was not reached or, with pinned
    (SNI name, leaf SHA-256), served another leaf, which is checked before the request is sent."""
    host = ("[" + address + "]" if ":" in address else address) + ":" + str(number)
    stamp = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime(clock()))
    digest = hashlib.sha256(body).hexdigest()
    headers = {"host": host, "x-amz-content-sha256": digest, "x-amz-date": stamp}
    headers["authorization"] = sigv4(method, path, query, headers, digest, pair, region, S3_SERVICE, stamp)
    target = path + ("?" + query if query else "")
    lines = [method + " " + target + " HTTP/1.1"] + [name + ": " + value for name, value in sorted(headers.items())]
    data = ("\r\n".join(lines + ["content-length: " + str(len(body)), "connection: close", "", ""])).encode("ascii") + body
    try:
        with contextlib.ExitStack() as stack:
            channel = stack.enter_context(socket.create_connection((address, number), timeout=S3_TIMEOUT))
            if pinned is not None:
                context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                context.minimum_version = ssl.TLSVersion.TLSv1_2
                context.check_hostname = False
                context.verify_mode = ssl.CERT_NONE
                channel = stack.enter_context(context.wrap_socket(channel, server_hostname=pinned[0]))
                presented = channel.getpeercert(binary_form=True)
                if not isinstance(presented, bytes) or not hmac.compare_digest(hashlib.sha256(presented).hexdigest(), pinned[1]):
                    return None
            channel.sendall(data)
            response = http.client.HTTPResponse(channel, method=method)
            stack.callback(response.close)
            response.begin()
            answer = response.read(MAX_RESPONSE + 1)
    except (OSError, ValueError, http.client.HTTPException):
        return None
    if len(answer) > MAX_RESPONSE:
        return response.status, "", "", None
    code, message = S3_CODE.search(answer), S3_MESSAGE.search(answer)
    return (response.status, code[1].decode("ascii") if code else "", message[1].decode("ascii") if message else "",
            answer if 200 <= response.status < 300 else None)


def frontend(request):
    """The ingress that fronts the gateway and its client port, the ingress frontend port; None without one."""
    documents = request["gateway"]["documents"]
    return (documents[1], documents[1]["spec"]["frontend_port"]) if len(documents) == 2 else None


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


class ObjectWorker(ceph_native_units.UnitWorker):
    """The core worker with one object gateway's zone, placement and service phases and its observation."""

    tls = None

    def gateway(self):
        return self.request["gateway"]

    def unit_key(self):
        return "gateway-" + self.gateway()["name"]

    def ingress_frontend(self):
        return frontend(self.request)

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
            address = self.daemon_address(row, number)
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


class UserWorker(ObjectWorker):
    """One object user in its gateway's zone: created once by radosgw-admin, whose generated S3 key pair crosses only stdout into this
    worker and is retained in the root-only custody area before any result; a retry reads that pair back and never rotates it."""

    def user(self):
        return self.request["objectUser"]

    def gateway(self):
        user = self.user()
        return {"name": user["gateway"], "realm": user["realm"], "zoneGroup": user["zoneGroup"], "zone": user["zone"]}

    def recovery_scope(self):
        """The cluster's bootstrap scope: the user member is this block's own input, never part of the cluster's authoring."""
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "objectUser"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def user_record(self, suffix):
        """Its records hold a `.` that custody's `object-user-<record>-<user>.json` names never do."""
        return "object-user-" + suffix + "." + self.user()["name"] + ".json"

    def user_intent(self):
        user = self.user()
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "gateway": user["gateway"], "user": user["name"],
                "displayName": user["displayName"], "realm": user["realm"], "zoneGroup": user["zoneGroup"], "zone": user["zone"]}

    def user_scope(self, ids):
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"], "user": self.user_intent(),
                "zoneID": ids["zone"]}

    def user_store(self):
        return Custody(self, "object-user", "-" + self.user()["name"], MAX_MATERIAL, lambda data: user_material(data, self.user()["name"]))

    def user_foreign(self, detail):
        user = self.user()
        return NativeFailure("object-user-foreign", self.native_coordinate(), "user " + user["name"] + " in realm " + user["realm"] + " " + detail)

    def user_command(self, *args):
        """radosgw-admin user list, info or create in the gateway's zone. Its stdout can hold the user's keys, so it stays in this
        worker's memory and a failure reports only stderr."""
        argv = [RADOSGW_ADMIN, "--conf", os.path.join(self.markers.path, "ceph.conf"),
                "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"),
                "--format", "json", "user", *args, *self.site("realm", "zonegroup", "zone")]
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            code, output = self.run(argv, timeout=60)
        if code != 0:
            raise NativeFailure("native-command", self.native_coordinate(), getattr(output, "stderr", b"") or b"radosgw-admin user " + args[0].encode())
        try:
            return json.loads(output)
        except (ValueError, UnicodeError):
            raise Refusal("native-output") from None

    def users(self):
        """`user list` prints the metadata keys of every user as one JSON array (src/rgw/radosgw-admin/radosgw-admin.cc:9740-9797)."""
        value = self.user_command("list")
        require(isinstance(value, list) and len(value) <= MAX_USERS and all(isinstance(uid, str) for uid in value), "native-output")
        return set(value)

    def info(self):
        """`user info` prints RGWUserInfo::dump (src/rgw/rgw_common.cc:2837-2897); each S3 key as {user, access_key, secret_key, active,
        create_date} (:3036-3050)."""
        value = self.user_command("info", "--uid", self.user()["name"])
        require(isinstance(value, dict) and value.get("user_id") == self.user()["name"], "native-output")
        return value

    def native_pair(self, info):
        """The one active S3 key the user holds, as retained material; a user with another key set, or whose key an operator deactivated
        (`user info` always dumps `active`, src/rgw/rgw_common.cc:3036-3050), is not this operation's."""
        keys = info.get("keys")
        require(isinstance(keys, list) and len(keys) == 1 and isinstance(keys[0], dict) and keys[0].get("user") == self.user()["name"]
                and keys[0].get("active") is True, "object-user-identity")
        pair = {"accessKey": keys[0].get("access_key"), "secretKey": keys[0].get("secret_key"), "uid": self.user()["name"]}
        require(visible(pair["accessKey"]) and visible(pair["secretKey"]), "object-user-identity")
        return canonical(pair)

    def prove_user(self, info, material):
        """The user as created: its display name, unsuspended, one S3 key equal to the retained pair, no Swift key, subuser or
        capability, no system, admin or account role."""
        user = self.user()
        held = canonical(material)
        try:
            same = hmac.compare_digest(self.native_pair(info), held)
        finally:
            del held
        require(same and info.get("display_name") == user["displayName"] and info.get("suspended") == 0
                and info.get("subusers") == [] and info.get("swift_keys") == [] and info.get("caps") == []
                and info.get("system", False) is False and info.get("admin", False) is False
                and info.get("type") == "rgw" and info.get("account_id") == "", "object-user-identity")

    def proved_user_zone(self):
        """The gateway's records and IDs, its committed period and the radosgw-admin its record holds."""
        ids = self.proved_records()
        require(self.committed(ids), "not-ready")
        require(self.markers.read(self.record("tool")) == {"sha256": self.tool_digest()}, "artifact")
        return ids

    def object_user(self):
        """Every proof before any write; the user created once under its attempt record; its key pair retained before the result."""
        self.verify_tool()
        ids = self.proved_user_zone()
        intent = self.user_intent()
        record, attempt = self.markers.read(self.user_record("intent")), self.markers.read(self.user_record("attempt"))
        require(record is None or record == intent, "ownership")
        require(attempt is None or attempt == intent, "ownership")
        scope = self.user_scope(ids)
        store = self.user_store()
        unused, ready = store.records(scope)
        listed = self.user()["name"] in self.users()
        if listed and attempt is None:
            raise self.user_foreign("has no creation record of this operation")
        if not listed and (ready is not None or self.markers.read(self.user_record("complete")) is not None):
            raise self.user_foreign("that this operation created is gone")
        self.markers.write(self.user_record("intent"), intent)
        if not listed:
            self.markers.write(self.user_record("attempt"), intent)
            created = self.user_command("create", "--uid", self.user()["name"], "--display-name", self.user()["displayName"])
            require(isinstance(created, dict) and created.get("user_id") == self.user()["name"], "native-output")
            del created
        material = store.prepare(scope, lambda: self.native_pair(self.info()))
        self.prove_user(self.info(), material)
        self.markers.write(self.user_record("complete"), intent)
        return {"changed": not listed, "private": canonical(material).decode("ascii")}

    def object_user_read(self):
        """Read-only recapture of the retained pair, proved against the user it was created for."""
        self.verify_tool()
        ids = self.proved_user_zone()
        require(self.markers.read(self.user_record("intent")) == self.user_intent(), "ownership")
        if self.user()["name"] not in self.users():
            raise self.user_foreign("that this operation created is gone")
        material = self.user_store().material(self.user_scope(ids))
        self.prove_user(self.info(), material)
        return {"private": canonical(material).decode("ascii")}

    def unfinished(self, evidence):
        if self.request["action"] != "object-user":
            return super().unfinished(evidence)
        evidence["owned"] = self.markers.read(self.user_record("intent")) == self.user_intent()
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def observe_resource(self, evidence, ready):
        """Read-only: radosgw-admin runs only once the gateway's realm, zone group and zone are recorded and listed."""
        if self.request["action"] != "object-user":
            return super().observe_resource(evidence, ready)
        intent = self.user_intent()
        record = self.markers.read(self.user_record("intent"))
        require(record is None or record == intent, "ownership")
        owned = record is not None
        present = self.root_present()
        ids = self.foreign_check(self.listed() if present else {kind: set() for kind in KINDS})
        listed = len(ids) == len(KINDS) and self.user()["name"] in self.users()
        if listed and self.markers.read(self.user_record("attempt")) is None:
            raise self.user_foreign("has no creation record of this operation")
        if not listed and self.markers.read(self.user_record("complete")) is not None:
            raise self.user_foreign("that this operation created is gone")
        evidence["owned"] = owned
        evidence["exists"] = owned or listed
        proved = False
        if owned and listed:
            unused, retained = self.user_store().records(self.user_scope(ids))
            if retained is not None:
                self.prove_user(self.info(), self.user_store().material(self.user_scope(ids)))
                proved = True
        evidence["resource"] = {"id": self.user()["name"], "path": ""} if proved else {"id": "", "path": ""}
        return ready and proved


class BucketWorker(UserWorker):
    """One bucket of one object user this operation created, in that user's gateway zone: S3 creates it and puts its policy under the user's
    key pair, held in this worker's memory only, and radosgw-admin proves its name, owner, zone group and bucket ID."""

    pair = None
    wall = staticmethod(time.time)

    def bucket(self):
        return self.request["objectBucket"]

    def user(self):
        bucket = self.bucket()
        return {"name": bucket["owner"], "gateway": bucket["gateway"], "realm": bucket["realm"], "zoneGroup": bucket["zoneGroup"], "zone": bucket["zone"]}

    def recovery_scope(self):
        """The cluster's bootstrap scope: the bucket member is this block's own input, never part of the cluster's authoring."""
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "objectBucket"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def bucket_record(self, suffix):
        return "object-bucket-" + suffix + "." + self.bucket()["name"] + ".json"

    def bucket_intent(self):
        bucket = self.bucket()
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "gateway": bucket["gateway"], "bucket": bucket["name"], "user": bucket["owner"],
                "realm": bucket["realm"], "zoneGroup": bucket["zoneGroup"], "zone": bucket["zone"]}

    def bucket_foreign(self, detail):
        bucket = self.bucket()
        return NativeFailure("object-bucket-foreign", self.native_coordinate(), "bucket " + bucket["name"] + " in realm " + bucket["realm"] + " " + detail)

    def bucket_identity(self, detail):
        return NativeFailure("object-bucket-identity", self.native_coordinate(), "bucket " + self.bucket()["name"] + " " + detail)

    def bucket_command(self, *args):
        return self.radosgw("bucket", *args, *self.site("realm", "zonegroup", "zone"))

    def buckets(self):
        """`bucket list` without a bucket or user prints every bucket's metadata key as one JSON array (src/rgw/driver/rados/rgw_bucket.cc:1868-1887)."""
        value = self.bucket_command("list")
        require(isinstance(value, list) and len(value) <= MAX_BUCKETS and all(isinstance(name, str) for name in value), "native-output")
        return set(value)

    def stats(self, ids):
        """`bucket stats` (src/rgw/driver/rados/rgw_bucket.cc:1514-1583, owner as its string, src/rgw/rgw_basic_types.cc:207-229): the bucket
        under its name with no tenant, its frozen owner, the gateway's zone group and the bucket ID this operation recorded."""
        bucket = self.bucket()
        value = self.bucket_command("stats", "--bucket", bucket["name"])
        require(isinstance(value, dict) and value.get("bucket") == bucket["name"], "native-output")
        identifier = value.get("id")
        require(isinstance(identifier, str) and BUCKET_ID.fullmatch(identifier) is not None, "native-output")
        recorded = self.markers.read(self.bucket_record("id"))
        require(recorded is None or isinstance(recorded, dict) and set(recorded) == {"id"}, "ownership")
        if (value.get("tenant") != "" or value.get("owner") != bucket["owner"]) and recorded is None:
            raise self.bucket_foreign("is owned by " + text(value.get("tenant")) + "$" + text(value.get("owner")) + ", not " + bucket["owner"])
        if value.get("tenant") != "" or value.get("owner") != bucket["owner"]:
            raise self.bucket_identity("is owned by " + text(value.get("tenant")) + "$" + text(value.get("owner")) + ", not " + bucket["owner"])
        if value.get("zonegroup") != ids["zonegroup"]:
            raise self.bucket_identity("is in zone group " + text(value.get("zonegroup")) + ", not " + bucket["zoneGroup"])
        if recorded is not None and recorded["id"] != identifier:
            raise self.bucket_identity("now has bucket ID " + identifier + ", not the recorded " + text(recorded["id"]))
        return identifier

    def owned_user(self):
        """The owner is the object user this operation created in this gateway's zone: its completion record says so."""
        bucket = self.bucket()
        expected = {"owner": self.request["owner"], "fsid": self.request["fsid"], "gateway": bucket["gateway"], "user": bucket["owner"],
                    "realm": bucket["realm"], "zoneGroup": bucket["zoneGroup"], "zone": bucket["zone"]}
        record = self.markers.read(self.user_record("complete"))
        require(isinstance(record, dict) and isinstance(record.get("displayName"), str)
                and {key: value for key, value in record.items() if key != "displayName"} == expected, "ownership")

    def owner_pair(self):
        """The owner's one active S3 key pair as `user info` reports it; it stays in this worker's memory and signs requests only."""
        if self.pair is None:
            self.owned_user()
            self.pair = json.loads(self.native_pair(self.info()))
        return self.pair

    def targets(self):
        """Each running rgw daemon of the backend service, in daemon name order, by the address it serves the frozen port on."""
        bucket = self.bucket()
        with self.manager_served():
            rows = self.ceph("orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        found = []
        for row in rows:
            if row.get("service_name") != bucket["backend"] or row.get("daemon_type") != "rgw":
                continue
            require(named(row.get("hostname")) and isinstance(row.get("daemon_id"), str), "native-output")
            if type(row.get("status")) is int and row["status"] == 1:
                address = self.daemon_address(row, bucket["port"])
                if address is not None:
                    found.append((row["daemon_id"], address))
        return [address for unused, address in sorted(found)]

    def s3(self, method, query, body=b""):
        """The answer of the first backend daemon that answers below 500, or None; over TLS only a daemon that serves the bound leaf is sent a
        request."""
        bucket = self.bucket()
        pinned = None
        if bucket["certificateRef"]:
            require(isinstance(self.tls, dict) and "gatewayCertificate" in self.tls, "material")
            pinned = (bucket["endpoint"], hashlib.sha256(leaf(self.tls["gatewayCertificate"])).hexdigest())
        pair = self.owner_pair()
        for address in self.targets():
            answer = s3_exchange(address, bucket["port"], method, "/" + bucket["name"], query, body, pair, bucket["zoneGroup"], pinned=pinned, clock=self.wall)
            if answer is not None and answer[0] < 500:
                return answer
        self.pending(("tls@" if pinned else "http@") + bucket["backend"])
        return None

    def s3_refusal(self, method, query, answer):
        status, code, message, unused = answer
        target = method + " /" + self.bucket()["name"] + ("?" + query if query else "")
        return NativeFailure("object-bucket-s3", self.native_coordinate(), target + ": HTTP " + str(status) + " " + code + " " + message,
                             secrets=(self.pair["accessKey"], self.pair["secretKey"]))

    def policy_state(self):
        """The policy the owner reads (src/rgw/rgw_op.cc:8869-8905): the stored bytes, b"" for none, or None when no daemon answered. A
        suspended bucket refuses its owner (src/rgw/rgw_op.cc:387-400), so either answer also proves the bucket usable by its owner."""
        answer = self.s3("GET", "policy")
        if answer is None:
            return None
        status, code, unused, body = answer
        if status == 404 and code == "NoSuchBucketPolicy":
            return b""
        if status != 200 or body is None:
            raise self.s3_refusal("GET", "policy", answer)
        return body

    def converge_policy(self, completed):
        """Before completion the declared policy is put when the stored one differs; RGW stores the bytes sent (src/rgw/rgw_op.cc:8803-8840,
        src/rgw/rgw_iam_policy.cc:1865-1868). Any other policy, or one on a bucket that declares none, is not this operation's."""
        declared = self.bucket()["policy"].encode("utf-8")
        stored = self.policy_state()
        require(stored is not None, "not-ready")
        if stored == declared:
            return False
        if completed or not declared:
            raise self.bucket_identity("holds a bucket policy other than its declared one")
        answer = self.s3("PUT", "policy", declared)
        require(answer is not None, "not-ready")
        if not 200 <= answer[0] < 300:
            raise self.s3_refusal("PUT", "policy", answer)
        require(self.policy_state() == declared, "not-ready")
        return True

    def object_bucket(self):
        """Every proof before any write; the bucket created once under its attempt record and proved by `bucket stats`; then its policy."""
        self.verify_tool()
        ids = self.proved_user_zone()
        self.owner_pair()
        intent = self.bucket_intent()
        record, attempt, complete = (self.markers.read(self.bucket_record(suffix)) for suffix in ("intent", "attempt", "complete"))
        require(all(value is None or value == intent for value in (record, attempt, complete)), "ownership")
        listed = self.bucket()["name"] in self.buckets()
        if listed and attempt is None:
            raise self.bucket_foreign("has no creation record of this operation")
        if not listed and (complete is not None or self.markers.read(self.bucket_record("id")) is not None):
            raise self.bucket_foreign("that this operation created is gone")
        self.markers.write(self.bucket_record("intent"), intent)
        changed = False
        if not listed:
            self.markers.write(self.bucket_record("attempt"), intent)
            answer = self.s3("PUT", "")
            require(answer is not None, "not-ready")
            if not 200 <= answer[0] < 300:
                raise self.s3_refusal("PUT", "", answer)
            changed = True
        self.markers.write(self.bucket_record("id"), {"id": self.stats(ids)})
        changed = self.converge_policy(complete is not None) or changed
        self.markers.write(self.bucket_record("complete"), intent)
        return {"changed": changed}

    def unfinished(self, evidence):
        if self.request["action"] != "object-bucket":
            return super().unfinished(evidence)
        evidence["owned"] = self.markers.read(self.bucket_record("intent")) == self.bucket_intent()
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def observe_resource(self, evidence, ready):
        """Read-only: radosgw-admin runs once the gateway's realm, zone group and zone are recorded and listed; S3 reads the policy only."""
        if self.request["action"] != "object-bucket":
            return super().observe_resource(evidence, ready)
        record = self.markers.read(self.bucket_record("intent"))
        require(record is None or record == self.bucket_intent(), "ownership")
        owned = record is not None
        present = self.root_present()
        ids = self.foreign_check(self.listed() if present else {kind: set() for kind in KINDS})
        listed = len(ids) == len(KINDS) and self.bucket()["name"] in self.buckets()
        if listed and self.markers.read(self.bucket_record("attempt")) is None:
            raise self.bucket_foreign("has no creation record of this operation")
        complete = self.markers.read(self.bucket_record("complete"))
        if not listed and (complete is not None or self.markers.read(self.bucket_record("id")) is not None):
            raise self.bucket_foreign("that this operation created is gone")
        evidence["owned"] = owned
        evidence["exists"] = owned or listed
        proved = False
        if owned and listed and complete is not None:
            self.stats(ids)
            stored = self.policy_state()
            if stored is not None and stored != self.bucket()["policy"].encode("utf-8"):
                raise self.bucket_identity("holds a bucket policy other than its declared one")
            proved = stored is not None
        evidence["resource"] = {"id": self.bucket()["name"], "path": ""} if proved else {"id": "", "path": ""}
        return ready and proved
