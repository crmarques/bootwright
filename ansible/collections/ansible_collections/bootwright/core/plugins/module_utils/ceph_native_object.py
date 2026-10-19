"""Fixed Ceph object gateway worker phases, shipped beside the core worker as source.

A gateway request carries one StorageObjectGateway: its realm, zone group and zone, created with radosgw-admin and never
with --default (src/rgw/radosgw-admin/radosgw-admin.cc:5110-5133), its zone's default-placement pools, the period
commit, then its one rgw document applied as one `orch apply -i -` stream. Every radosgw-admin access to the config
store creates the shared `.rgw.root` pool when it is absent (src/rgw/driver/rados/config/impl.h:93,
src/rgw/driver/rados/rgw_tools.cc:24-52), so the pool is proved absent or claimed through monitor commands, and the
claim and the gateway's record are written, before the first radosgw-admin call; an observation never calls
radosgw-admin while the pool is absent. A realm, zone group or zone of the gateway's names is adopted only under this
operation's attempt record. The gateway never reads or creates an object user. Like the core worker it imports only
the target Python standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import contextlib
import hashlib
import ipaddress
import os
import re
import socket
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_units
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, canonical, named, read_regular, require

APPLY_PHASES = ("gateway_zone", "gateway_placement", "gateway_service")
PHASES = frozenset(APPLY_PHASES)
RADOSGW_ADMIN = "/usr/bin/radosgw-admin"
RGW_ROOT = ".rgw.root"
ROOT_CLAIM = "rgw-root.json"
# cephadm's RGWSpec members the gateway realizes (src/python-common/ceph/deployment/service_spec.py, RGWSpec).
RGW_MEMBERS = frozenset(("only_bind_port_on_networks", "rgw_exit_timeout_secs", "rgw_frontend_port", "rgw_frontend_type", "rgw_realm", "rgw_zone",
                         "rgw_zonegroup"))
LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\Z")
ZONE_ID = ceph_native.SUBVOLUME_UUID
PLACEMENT = "default-placement"
# RGWSpec.get_port: 80 without ssl (src/python-common/ceph/deployment/service_spec.py).
HTTP_PORT = 80
PROBE_TIMEOUT = 5
PROBE_LIMIT = 256
PROBE_REQUEST = b"GET / HTTP/1.0\r\n\r\n"
STATUS_LINE = re.compile(rb"HTTP/1\.[01] [1-5][0-9]{2}(?: [^\r\n]*)?\r?\n")
PENDING = re.compile(r"(?:rgw|http)@rgw\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
MEMBERS = frozenset(("dataExtraPool", "dataPool", "documents", "endpoint", "indexPool", "name", "realm", "zone", "zoneGroup", "zonePools"))
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


def validate_spec(spec, gateway):
    """The rgw document's spec: realm, zone group and zone are the gateway's, and every other member is a plain HTTP member."""
    require(isinstance(spec, dict) and set(spec) <= RGW_MEMBERS)
    require(spec.get("rgw_realm") == gateway["realm"] and spec.get("rgw_zonegroup") == gateway["zoneGroup"] and spec.get("rgw_zone") == gateway["zone"])
    require("rgw_frontend_port" not in spec or type(spec["rgw_frontend_port"]) is int and 1 <= spec["rgw_frontend_port"] <= 65535)
    require("rgw_frontend_type" not in spec or spec["rgw_frontend_type"] == "beast")
    require("only_bind_port_on_networks" not in spec or type(spec["only_bind_port_on_networks"]) is bool)
    require("rgw_exit_timeout_secs" not in spec or type(spec["rgw_exit_timeout_secs"]) is int and spec["rgw_exit_timeout_secs"] >= 1)


def validate_document(document, gateway, request, hosts):
    """One placed rgw document, listed in `delegated` with exactly its hosts."""
    require(isinstance(document, dict) and {"placement", "service_id", "service_type", "spec"} <= set(document) <= ceph_native_units.DOCUMENT_MEMBERS)
    require(document["service_type"] == "rgw" and named(document["service_id"]))
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
    validate_spec(document["spec"], gateway)


def validate(request, hosts):
    """One gateway: its native names, its frozen pools and endpoint, and exactly one plain HTTP rgw document."""
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
    require(isinstance(documents, list) and len(documents) == 1)
    validate_document(documents[0], gateway, request, hosts)


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


def answers(address, number):
    """An HTTP status line from the frontend within the probe's bound; a bare connection proves nothing."""
    deadline = time.monotonic() + PROBE_TIMEOUT
    data = b""
    try:
        with socket.create_connection((address, number), timeout=PROBE_TIMEOUT) as connection:
            connection.sendall(PROBE_REQUEST)
            while len(data) < PROBE_LIMIT and b"\n" not in data:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                connection.settimeout(remaining)
                block = connection.recv(PROBE_LIMIT - len(data))
                if not block:
                    break
                data += block
    except OSError:
        return False
    return STATUS_LINE.match(data) is not None


class ObjectWorker(ceph_native_units.UnitWorker):
    """The core worker with one object gateway's zone, placement and service phases and its observation."""

    def gateway(self):
        return self.request["gateway"]

    def unit_key(self):
        return "gateway-" + self.gateway()["name"]

    def unit_groups(self):
        return [[document] for document in self.gateway()["documents"]]

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
        """Ownership and every proof before any write; the claim and record before any radosgw-admin call; then realm, zone group and zone."""
        self.owned_cluster()
        self.verify_tool()
        digest = self.tool_digest()
        tool = self.markers.read(self.record("tool"))
        require(tool is None or tool == {"sha256": digest}, "artifact")
        present = self.root_present()
        if not present:
            self.markers.write(ROOT_CLAIM, self.root_claim())
        self.markers.write(self.record("intent"), self.intent())
        self.markers.write(self.record("tool"), {"sha256": digest})
        names = self.listed()
        ids = self.foreign_check(names)
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
        values = [group[0] for group in self.normalized_services(self.unit_groups(), record=self.unit_key() + "-inputs.json")]
        scope = self.recovery_scope()
        observed = self.unit_specs()
        self.unit_records(values, observed, scope)
        names = self.unit_names()
        missing = [(name, value) for name, value in zip(names, values) if name not in observed]
        for name, value in missing:
            self.markers.write("service-" + name + "-attempt.json", {"scope": scope, "service": value})
        if missing:
            self.tunnel_effect("orch", "apply", "-i", "-", data=b"---\n".join(canonical(value) + b"\n" for unused, value in missing), json_result=False)
            observed = self.unit_specs()
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
        stored = self.unit_specs()
        specs = {name: stored[name] for name in services if name in stored}
        with self.manager_served():
            rows = self.ceph("orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        running = [row for row in rows if row.get("service_name") in services]
        require(owned or not specs and not running, "foreign-service")
        evidence["owned"] = owned
        evidence["exists"] = owned or any(self.frozen(kind) in names[kind] for kind in KINDS) or bool(specs) or bool(running)
        proved = owned and self.observe_gateway(evidence, ids, specs)
        daemons = self.gateway_daemons(running)
        evidence["daemons"] = sorted(({"host": row["hostname"], "name": row["daemon_type"] + "." + row["daemon_id"], "service": row["service_name"]}
                                      for row in running), key=lambda item: item["name"])[:ceph_native_units.MAX_DAEMONS]
        return ready and owned and proved and daemons

    def observe_gateway(self, evidence, ids, specs):
        """Read-only: the recorded IDs, the zone's pools and placement, membership, the committed period and every service record."""
        if len(ids) != len(KINDS):
            return False
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
        """The placement count of rgw daemons on the frozen Ceph image, each answering an HTTP status line on its frontend port."""
        document = self.gateway()["documents"][0]
        service = ceph_native_units.service_name(document)
        count = document["placement"].get("count", len(document["placement"]["hosts"]))
        number = document["spec"].get("rgw_frontend_port", HTTP_PORT)
        image = self.request["image"]
        started = []
        ready = True
        for row in running:
            require(named(row.get("hostname")) and isinstance(row.get("daemon_type"), str) and isinstance(row.get("daemon_id"), str), "native-output")
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
            if not self.http_ready(row, number):
                ready = False
                self.pending("http@" + service)
        return ready

    def http_ready(self, row, number):
        ports = row.get("ports")
        if isinstance(ports, list) and ports and number not in ports:
            return False
        address = row.get("ip")
        try:
            address = str(ipaddress.ip_address(address)) if isinstance(address, str) else None
        except ValueError:
            address = None
        if address is None:
            host = next((item for item in self.request["hosts"] if item["name"] == row["hostname"]), None)
            require(host is not None, "foreign-service")
            address = host["address"]
        return answers(address, number)
