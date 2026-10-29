"""Per-export Ceph principals and private external-cluster details.

Native keys remain in inode-bound custody. Public records hold only frozen
intent and the non-secret facts used for the first details assembly.
"""
from __future__ import annotations

# Exact Boolean and integer types reject coercible authored values.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import hashlib
import ipaddress
import json
import os
import struct
import time
import urllib.parse

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources, ceph_export_dns
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import Custody, Refusal, canonical, require, unique_object

VERSION = "ceph-export-request-v3"
EVIDENCE_VERSION = "ceph-export-evidence-v1"
PHASES = ("export_apply", "export_observe")
REASONS = ("gateway-dns", "export-foreign-principal", "export-key-type", "export-prerequisite", "export-identity")
RGW_CAPS = {"buckets": "*", "users": "*", "usage": "read", "metadata": "read", "zone": "read", "info": "read"}


def caps_for(request):
    health = "profile rbd-read-only"
    if "gateway" in request:
        gateway = request["gateway"]
        zone = gateway["zone"]
        health += (", allow rwx pool=" + zone + ".rgw.meta, allow r pool=.rgw.root, allow rw pool=" + zone
                   + ".rgw.control, allow rx pool=" + zone + ".rgw.log, allow x pool=" + gateway["indexPool"])
    return {
        "health": {"mon": "allow r, allow command quorum_status, allow command version", "mgr": "allow command config", "osd": health, "mds": "allow *"},
        "rbd-node": {"mon": "profile rbd, allow command 'osd blocklist'", "osd": "profile rbd pool=" + request["pool"]},
        "rbd-prov": {"mon": "profile rbd, allow command 'osd blocklist'", "mgr": "allow rw", "osd": "profile rbd pool=" + request["pool"]},
        "fs-node": {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw",
                    "osd": "allow rw tag cephfs *=" + request["filesystem"], "mds": "allow rw"},
        "fs-prov": {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw",
                    "osd": "allow rw tag cephfs metadata=" + request["filesystem"], "mds": "allow *"},
    }


def validate(request):
    required = {"cluster", "controller", "defaultDataPool", "filesystem", "keyType", "machines", "monitors",
                "monitoringHosts", "monitoringPort", "name", "physicalHosts", "pool", "principals", "providedMachines", "scope", "version"}
    require(isinstance(request, dict) and set(request) in (required, required | {"gateway"}))
    require(request["version"] == VERSION and request["keyType"] in ("aes", "aes256k"))
    require(all(ceph_native.named(request[key]) for key in ("cluster", "controller", "filesystem", "name", "pool", "defaultDataPool")))
    require(len(request["name"]) <= 51 and isinstance(request["machines"], list) and request["machines"] == sorted(set(request["machines"])))
    require(set(request["principals"]) == {"health", "rbd-node", "rbd-prov", "fs-node", "fs-prov"})
    require(all(isinstance(caps, dict) and caps and set(caps) <= {"mon", "mgr", "osd", "mds"}
                and all(isinstance(cap, str) and 0 < len(cap) < 2048 and "\n" not in cap and '"' not in cap for cap in caps.values())
                for caps in request["principals"].values()))
    scope = ceph_native.validate_scope(request["scope"])
    require(scope["cluster"] == request["cluster"] and sorted(host["machine"] for host in scope["hosts"]) == request["machines"])
    require(request["providedMachines"] == sorted(host["machine"] for host in scope["hosts"] if host.get("provided")))
    if "gateway" in request:
        gateway = request["gateway"]
        gateway_fields = {"addresses", "certificateRef", "fqdn", "indexPool", "name", "port", "realm", "tls", "zone", "zoneGroup"}
        require(isinstance(gateway, dict) and set(gateway) == gateway_fields)
        require(all(ceph_native.named(gateway[key]) for key in ("name", "realm", "zone", "zoneGroup", "indexPool")))
        require(type(gateway["tls"]) is bool and type(gateway["port"]) is int and 0 < gateway["port"] <= 65535)
        require(bool(gateway["certificateRef"]) == gateway["tls"])
        require(isinstance(gateway["fqdn"], str) and 0 < len(gateway["fqdn"]) <= 253)
        require(isinstance(gateway["addresses"], list) and bool(gateway["addresses"]))
        for address in gateway["addresses"]:
            ipaddress.ip_address(address)
    require(request["principals"] == caps_for(request))
    require(isinstance(request["monitors"], dict) and bool(request["monitors"]))
    require(isinstance(request["monitoringHosts"], list) and bool(request["monitoringHosts"])
            and request["monitoringHosts"] == sorted(set(request["monitoringHosts"])) and request["monitoringPort"] == 9283)
    for address in list(request["monitors"].values()) + request["monitoringHosts"]:
        ipaddress.ip_address(address)
    return request


def prefix(request):
    seed = "bootwright.storage-export.principal-v1\x00" + request["scope"]["owner"] + "\x00" + request["name"]
    return "bw-" + hashlib.sha256(seed.encode()).hexdigest()[:16]


def key_valid(key, kind):
    try:
        data = base64.b64decode(key, validate=True)
        size, tag = (16, 1) if kind == "aes" else (32, 2)
        return (len(data) == 12 + size and struct.unpack("<H", data[:2])[0] == tag
                and struct.unpack("<H", data[10:12])[0] == size and base64.b64encode(data).decode() == key)
    except (ValueError, TypeError):
        return False


def generated_key(kind):
    size, tag = (16, 1) if kind == "aes" else (32, 2)
    return base64.b64encode(struct.pack("<HIIH", tag, int(time.time()), 0, size) + os.urandom(size)).decode()


def dispatch(envelope):
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private"})
    request = validate(envelope["request"])
    require(envelope["host"] == request["scope"]["bootstrap"] and envelope["phase"] in PHASES)
    worker = ExportWorker(request, envelope["host"])
    worker.horizon = worker.clock() + ceph_native.MANAGER_HORIZON
    return worker.execute(envelope["phase"] == "export_apply", envelope["private"])


class ExportWorker(ceph_native_resources.ResourceWorker):
    admit = staticmethod(ceph_native.validate_scope)

    def __init__(self, request, host, run=ceph_native.bounded_command, markers=None):
        self.export = validate(request)
        super().__init__(request["scope"], host, run, markers)
        self.identifier = prefix(request)

    def recovery_scope(self):
        unused, staging = ceph_native.tunnel_helpers()
        expected = staging.recovery_scope(self.request)
        recorded = self.markers.read("bootstrap-scope.json")
        require(isinstance(recorded, dict) and isinstance(recorded.get("cluster"), dict), "ownership")
        cluster = dict(recorded["cluster"])
        cluster.pop("delegated", None)
        require(canonical(dict(recorded, cluster=cluster)) == canonical(expected), "ownership")
        return recorded

    def rgw(self, *args):
        gateway = self.export["gateway"]
        argv = ["/usr/bin/radosgw-admin", "--conf", os.path.join(self.markers.path, "ceph.conf"),
                "--keyring", os.path.join(self.markers.path, "ceph.client.admin.keyring"), "--format", "json", *args,
                "--rgw-realm", gateway["realm"], "--rgw-zonegroup", gateway["zoneGroup"], "--rgw-zone", gateway["zone"]]
        with self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verifiedTunnel" else contextlib.nullcontext():
            code, output = self.run(argv, timeout=60)
        require(code == 0, "export-prerequisite")
        try:
            return json.loads(output, object_pairs_hook=unique_object)
        except (ValueError, UnicodeError):
            raise Refusal("export-identity") from None

    def prerequisites(self):
        require(self.markers.owned() and self.ceph("fsid") == {"fsid": self.request["fsid"]}, "export-identity")
        self.verify_tool()
        pool = self.export["pool"]
        require(self.markers.read("pool-" + pool + ".json") is not None, "export-prerequisite")
        pool_id = self.markers.read("pool-id-" + pool + ".json")
        require(isinstance(pool_id, dict) and set(pool_id) == {"id"}, "export-prerequisite")
        pools = self.ceph("osd", "pool", "ls", "detail")
        selected = [entry for entry in pools if entry.get("pool_name") == pool]
        require(len(selected) == 1 and self.pool_id(selected[0]) == pool_id["id"]
                and "rbd" in selected[0].get("application_metadata", {}), "export-prerequisite")
        filesystem = self.export["filesystem"]
        require(self.markers.read("filesystem-" + filesystem + ".json") is not None, "export-prerequisite")
        filesystems = self.fs_dump()["filesystems"]
        selected_fs = [entry for entry in filesystems if entry["mdsmap"]["fs_name"] == filesystem]
        require(len(selected_fs) == 1 and self.markers.read("filesystem-id." + filesystem + ".json") == {"id": selected_fs[0]["id"]}, "export-prerequisite")
        groups = self.ceph("fs", "subvolumegroup", "ls", filesystem)
        require(isinstance(groups, list) and any(group.get("name") == "csi" for group in groups), "export-prerequisite")
        monmap = self.ceph("mon", "dump")
        allowed = monmap.get("auth_allowed_ciphers")
        require(isinstance(allowed, list) and bool(allowed), "export-key-type")
        require(all(isinstance(entry, dict) and set(entry) == {"name", "value"}
                    and (entry["name"], entry["value"]) in (("aes", 1), ("aes256k", 2)) for entry in allowed), "export-key-type")
        require(any(entry["name"] == self.export["keyType"] for entry in allowed), "export-key-type")
        services = self.ceph("mgr", "services")
        endpoint = services.get("prometheus")
        require(isinstance(endpoint, str) and bool(endpoint), "export-prerequisite")
        parsed = urllib.parse.urlsplit(endpoint)
        require(parsed.scheme in ("http", "https") and parsed.hostname and parsed.port, "export-prerequisite")
        quorum = self.ceph("quorum_status")
        leader = quorum.get("quorum_leader_name")
        require(leader in self.export["monitors"], "export-prerequisite")
        address = self.export["monitors"][leader]
        monitors = quorum.get("monmap", {}).get("mons", [])
        selected = [mon for mon in monitors if mon.get("name") == leader]
        require(len(selected) == 1, "export-prerequisite")
        native = selected[0].get("public_addrs", {}).get("addrvec", [])
        require(any(entry.get("type") == "v2" and entry.get("addr", "").split("/")[0] ==
                    ("[" + address + "]" if ":" in address else address) + ":3300" for entry in native), "export-prerequisite")
        require(parsed.hostname in self.export["monitoringHosts"] and parsed.port == self.export["monitoringPort"], "export-prerequisite")
        gateway = self.export.get("gateway")
        if gateway:
            ceph_export_dns.prove_gateway(gateway["fqdn"], gateway["addresses"], runner=self.run)
            for entity, key in (("realm", "realm"), ("zonegroup", "zoneGroup"), ("zone", "zone")):
                result = self.rgw(entity, "get")
                require(isinstance(result, dict) and result.get("name") == gateway[key]
                        and self.markers.read("gateway-" + gateway["name"] + "-" + entity + "-id.json") == {"id": result.get("id")}, "export-prerequisite")
        return {"leader": leader, "address": address, "monitors": leader + "=" + ("[" + address + "]" if ":" in address else address) + ":3300",
                "monitoringEndpoint": ",".join(self.export["monitoringHosts"]), "monitoringPort": str(self.export["monitoringPort"])}

    def decode_keys(self, data):
        try:
            value = json.loads(data, object_pairs_hook=unique_object)
        except (ValueError, UnicodeError, TypeError):
            raise Refusal("export-identity") from None
        require(isinstance(value, dict) and set(value) == set(self.export["principals"]), "export-identity")
        for suffix, entry in value.items():
            require(isinstance(entry, dict) and set(entry) == {"entity", "key", "caps"}
                    and entry["entity"] == "client." + self.identifier + "-" + suffix
                    and entry["caps"] == self.export["principals"][suffix]
                    and key_valid(entry["key"], self.export["keyType"]), "export-identity")
        require(canonical(value) == data, "export-identity")
        return value

    def generate(self):
        return canonical({suffix: {"entity": "client." + self.identifier + "-" + suffix,
                                   "key": generated_key(self.export["keyType"]), "caps": caps}
                          for suffix, caps in self.export["principals"].items()})

    def rgw_pair(self, info):
        uid = self.identifier + "-ops"
        require(isinstance(info, dict) and info.get("user_id") == uid, "export-identity")
        caps = info.get("caps", [])
        require(isinstance(caps, list) and len(caps) == len(RGW_CAPS)
                and {cap.get("type"): cap.get("perm") for cap in caps} == RGW_CAPS, "export-identity")
        keys = info.get("keys")
        require(isinstance(keys, list) and len(keys) == 1 and keys[0].get("user") == uid, "export-identity")
        require(all(isinstance(keys[0].get(key), str) and 0 < len(keys[0][key]) <= 1024 for key in ("access_key", "secret_key")), "export-identity")
        return {"accessKey": keys[0]["access_key"], "secretKey": keys[0]["secret_key"]}

    def execute(self, apply, private):
        gateway = self.export.get("gateway")
        require(isinstance(private, dict) and set(private) == ({"certificate"} if gateway and gateway["tls"] else set()), "material")
        facts = self.prerequisites()
        intent = {key: value for key, value in self.export.items() if key != "scope"}
        scope = {"owner": self.request["owner"], "fsid": self.request["fsid"], "export": intent}
        marker = "export-" + self.export["name"]
        recorded = self.markers.read(marker + ".json")
        auth = self.ceph("auth", "ls")
        require(isinstance(auth, dict) and isinstance(auth.get("auth_dump"), list), "export-identity")
        names = {"client." + self.identifier + "-" + suffix for suffix in self.export["principals"]}
        existing = {entry["entity"]: entry for entry in auth["auth_dump"] if isinstance(entry, dict) and entry.get("entity") in names}
        require(recorded is not None or not existing, "export-foreign-principal")
        require(recorded is None or recorded == scope, "export-identity")
        store = Custody(self, "client", "-export-" + self.export["name"], 65536, self.decode_keys)
        store.records(scope)
        if not apply:
            require(recorded == scope and self.markers.read(marker + "-complete.json") == scope, "export-identity")
        if apply:
            self.markers.write(marker + ".json", scope)
            keys = store.prepare(scope, self.generate)
        else:
            keys = store.material(scope)
        changed = False
        for suffix, entry in sorted(keys.items()):
            current = existing.get(entry["entity"])
            attempt = marker + "-" + suffix + ".json"
            if current is not None:
                require(self.markers.read(attempt) == scope, "export-foreign-principal")
                require(current == entry, "export-identity")
            else:
                require(apply, "export-identity")
                self.markers.write(attempt, scope)
                self.ceph("auth", "add", entry["entity"], "-i", "-", data=self.client_keyring([entry]), json_result=False)
                changed = True
            require(self.ceph("auth", "get", entry["entity"]) == [entry], "export-identity")
        pair = None
        if gateway:
            uid = self.identifier + "-ops"
            users = self.rgw("user", "list")
            require(isinstance(users, list), "export-identity")
            attempt = marker + "-rgw.json"
            require(uid not in users or self.markers.read(attempt) == scope, "export-foreign-principal")
            if uid not in users:
                require(apply, "export-identity")
                self.markers.write(attempt, scope)
                self.rgw("user", "create", "--uid=" + uid, "--display-name=" + uid,
                         "--caps=" + ";".join(key + "=" + value for key, value in RGW_CAPS.items()), "--gen-access-key", "--gen-secret")
                changed = True
            pair = self.rgw_pair(self.rgw("user", "info", "--uid=" + uid))

            def decode_pair(data):
                try:
                    value = json.loads(data, object_pairs_hook=unique_object)
                except (ValueError, UnicodeError, TypeError):
                    raise Refusal("export-identity") from None
                require(value == pair and canonical(value) == data, "export-identity")
                return value
            custody = Custody(self, "client", "-export-rgw-" + self.export["name"], 65536, decode_pair)
            pair = custody.prepare(scope, lambda: canonical(pair)) if apply else custody.material(scope)
        saved = self.markers.read(marker + "-facts.json")
        if saved is None:
            require(apply, "export-identity")
            self.markers.write(marker + "-facts.json", facts)
            saved = facts
        require(isinstance(saved, dict) and set(saved) == set(facts) and self.export["monitors"].get(saved["leader"]) == saved["address"]
                and saved["monitoringEndpoint"] == facts["monitoringEndpoint"] and saved["monitoringPort"] == facts["monitoringPort"]
                and saved["monitors"] == saved["leader"] + "="
                + ("[" + saved["address"] + "]" if ":" in saved["address"] else saved["address"]) + ":3300", "export-identity")
        details = self.details(keys, pair, saved, private)
        if apply:
            self.markers.write(marker + "-complete.json", scope)
        return {"changed": changed, "private": canonical(details).decode(), "leader": saved["leader"], "address": saved["address"]}

    def details(self, keys, pair, facts, private):
        def entry(kind, name, data):
            return {"kind": kind, "name": name, "data": data}
        entries = [entry("ConfigMap", "external-cluster-user-command", {"args": "bootwright-managed-export"}),
                   entry("ConfigMap", "rook-ceph-mon-endpoints", {"data": facts["monitors"], "maxMonId": "0", "mapping": "{}"}),
                   entry("Secret", "rook-ceph-mon", {"admin-secret": "admin-secret", "mon-secret": "mon-secret", "fsid": self.request["fsid"]}),
                   entry("Secret", "rook-ceph-operator-creds", {"userID": keys["health"]["entity"], "userKey": keys["health"]["key"]}),
                   entry("CephCluster", "monitoring-endpoint", {"MonitoringEndpoint": facts["monitoringEndpoint"], "MonitoringPort": facts["monitoringPort"]})]
        for suffix in ("rbd-node", "rbd-prov", "fs-node", "fs-prov"):
            entries.append(entry("Secret", "rook-" + self.identifier + "-" + suffix,
                                 {"userID": keys[suffix]["entity"].removeprefix("client."), "userKey": keys[suffix]["key"]}))
        for name, kind, data in (("ceph-rbd", "rbd", {"pool": self.export["pool"]}),
                                 ("cephfs", "fs", {"pool": self.export["defaultDataPool"], "fsName": self.export["filesystem"]})):
            for field, suffix in (("provisioner", "prov"), ("controller-expand", "prov"), ("node-stage", "node")):
                data["csi.storage.k8s.io/" + field + "-secret-name"] = "rook-" + self.identifier + "-" + kind + "-" + suffix
            entries.append(entry("StorageClass", name, data))
        gateway = self.export.get("gateway")
        if gateway:
            entries.extend([entry("StorageClass", "ceph-rgw", {"endpoint": gateway["fqdn"] + ":" + str(gateway["port"]), "poolPrefix": gateway["zone"]}),
                            entry("Secret", "rgw-admin-ops-user", pair)])
            if gateway["tls"]:
                certificate = private["certificate"]
                require(isinstance(certificate, str) and "-----BEGIN CERTIFICATE-----" in certificate and "PRIVATE KEY" not in certificate, "material")
                entries.append(entry("Secret", "ceph-rgw-tls-cert", {"cert": certificate}))
        return entries
