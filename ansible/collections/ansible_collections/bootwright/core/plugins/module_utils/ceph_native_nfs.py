"""Fixed NFS service-unit hooks and the NFS export worker, shipped beside the core worker as source.

An NFS unit deploys one `nfs` service, after the `ingress` that fronts it when one is declared. cephadm creates
the shared `.nfs` pool and enables its `nfs` application when it configures the service
(src/pybind/mgr/cephadm/services/nfs.py:64-68, src/pybind/mgr/nfs/cluster.py:45-54); the hooks claim that pool
before the first apply, record its native ID after it, and never adopt, rewrite or delete it. The hooks never
enable a manager module or write an image setting: the cluster's settings block is their single writer.
An NFS export applies one native export JSON with `nfs export apply <service> -i -` (src/pybind/mgr/nfs/export.py:830-915), which
upserts by pseudo path, so the worker proves the pseudo path free or its own before the first apply and never sends an export ID, cluster
ID or CephFS principal. It records the generated export ID and the derived principal once, and drops every FSAL secret
`nfs export info` may print (src/pybind/mgr/nfs/ganesha_conf.py:261-271, :314-322) before any comparison, record or error.
Like the core worker it imports only the target Python standard library, the core worker and its resource worker.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import ipaddress
import posixpath
import re
import socket

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, canonical, named, require

DAEMON_TYPES = ("nfs", "haproxy", "keepalived")
POOL = ".nfs"
CLAIM = "nfs-pool.json"
IDENTITY = "nfs-pool-id.json"
NFS_POOL_WAIT = 120
NFS_POOL_INTERVAL = 5
VIP_TIMEOUT = 5
NFS_PORT = 2049
NFS_MEMBERS = frozenset(("enable_haproxy_protocol", "monitoring_port", "port", "virtual_ip"))
INGRESS_MEMBERS = frozenset(("backend_service", "enable_haproxy_protocol", "first_virtual_router_id", "frontend_port", "haproxy_peer_communication_port",
                             "monitor_port", "health_check_interval", "keepalive_only", "virtual_ip", "virtual_interface_networks"))
PORTS = ("frontend_port", "haproxy_peer_communication_port", "monitor_port", "monitoring_port", "port")
EXPORT_APPLY_PHASES = ("nfs_export",)
EXPORT_PHASES = frozenset(EXPORT_APPLY_PHASES)
EXPORT_MEMBERS = frozenset(("filesystem", "group", "name", "native", "service", "subvolume"))
NATIVE_REQUIRED = frozenset(("access_type", "clients", "fsal", "protocols", "pseudo", "security_label", "squash", "transports"))
NATIVE_OPTIONAL = frozenset(("path", "sectype"))
CLIENT_MEMBERS = frozenset(("access_type", "addresses", "squash"))
ACCESS = ("RW", "RO", "NONE")
# The squash tokens the NFS manager admits (src/pybind/mgr/nfs/ganesha_conf.py:28-54), in admission's order.
SQUASH = ("root", "root_squash", "rootsquash", "rootid", "root_id_squash", "rootidsquash", "all", "all_squash", "allsquash", "all_anomnymous",
          "allanonymous", "no_root_squash", "none", "noidsquash")
FSAL_SECRETS = ("cephx_key", "access_key_id", "secret_access_key")
EXPORT_ID = re.compile(r"[1-9][0-9]{0,18}\Z")
USER_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,255}\Z")
MAX_CLUSTERS = 1024


def port(value):
    return type(value) is int and 1 <= value <= 65535


def networks(value):
    require(isinstance(value, list) and 1 <= len(value) <= 8)
    for network in value:
        try:
            require(isinstance(network, str) and str(ipaddress.ip_network(network, strict=True)) == network)
        except ValueError:
            raise Refusal("request") from None


def address(value, prefix):
    try:
        require(isinstance(value, str) and ("/" in value) == prefix)
        parsed = ipaddress.ip_interface(value) if prefix else ipaddress.ip_address(value)
    except ValueError:
        raise Refusal("request") from None
    require(str(parsed) == value)


def spec_members(spec, members):
    require(isinstance(spec, dict) and set(spec) <= members)
    for name in ("enable_haproxy_protocol", "keepalive_only"):
        require(name not in spec or type(spec[name]) is bool)
    require(all(port(spec[name]) for name in PORTS if name in spec))
    if "virtual_interface_networks" in spec:
        networks(spec["virtual_interface_networks"])


def validate(unit, request, hosts):
    """One nfs document, after the ingress that fronts it when declared; every member within the realized records."""
    del hosts
    identifier = unit["id"]
    documents = unit["documents"]
    require(named(identifier) and len(documents) in (1, 2))
    nfs, ingress = documents[-1], documents[0] if len(documents) == 2 else None
    require(nfs["service_type"] == "nfs" and nfs["service_id"] == identifier)
    spec = nfs.get("spec", {})
    spec_members(spec, NFS_MEMBERS)
    if "virtual_ip" in spec:
        address(spec["virtual_ip"], False)
    if "networks" in nfs:
        networks(nfs["networks"])
    if ingress is None:
        return
    require(ingress["service_type"] == "ingress" and ingress["service_id"] == "nfs." + identifier and "networks" not in ingress)
    spec = ingress.get("spec")
    spec_members(spec, INGRESS_MEMBERS)
    require(spec.get("backend_service") == "nfs." + identifier and "virtual_ip" in spec)
    address(spec["virtual_ip"], True)
    require("first_virtual_router_id" not in spec or type(spec["first_virtual_router_id"]) is int and 1 <= spec["first_virtual_router_id"] <= 255)
    require("health_check_interval" not in spec or isinstance(spec["health_check_interval"], str) and 0 < len(spec["health_check_interval"]) <= 16)
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(set(needed_images(unit)) <= frozen)


def ingress_of(unit):
    return unit["documents"][0] if len(unit["documents"]) == 2 else None


def frontend(unit):
    """The ingress that fronts the unit and its client port: the nfs port when keepalive-only, else the ingress frontend port; None without one."""
    ingress = ingress_of(unit)
    if ingress is None:
        return None
    if ingress["spec"].get("keepalive_only"):
        return ingress, unit["documents"][-1].get("spec", {}).get("port", NFS_PORT)
    return ingress, ingress["spec"].get("frontend_port", NFS_PORT)


def needed_images(unit):
    """The auxiliary images the unit's ingress runs: keepalived, and haproxy unless keepalive-only."""
    ingress = ingress_of(unit)
    if ingress is None:
        return ()
    return ("keepalived",) if ingress["spec"].get("keepalive_only") else ("haproxy", "keepalived")


def pool_claim(worker):
    return {"owner": worker.request["owner"], "fsid": worker.request["fsid"], "pool": POOL}


def pool_entries(worker):
    entries = [entry for entry in worker.pools() if isinstance(entry, dict) and entry.get("pool_name") == POOL]
    require(len(entries) <= 1, "native-output")
    return entries


def check(worker, unit):
    """Read-only, before any write: the derived module, the `.nfs` pool's claim and identity, and the ingress image settings."""
    if "nfs" not in worker.manager_modules():
        raise Refusal("nfs-module-disabled")
    entries = pool_entries(worker)
    claim, identity = worker.markers.read(CLAIM), worker.markers.read(IDENTITY)
    if entries:
        require(claim == pool_claim(worker), "nfs-pool-foreign")
        require(identity is None or identity == {"id": worker.pool_id(entries[0])}, "nfs-pool-foreign")
    else:
        require(identity is None, "nfs-pool-foreign")
        require(claim is None or claim == pool_claim(worker), "nfs-pool-foreign")
    needed = needed_images(unit)
    if needed:
        unused, held = worker.image_settings()
        missing = sorted(worker.image_key(name) for name in needed if worker.image_key(name) not in held)
        if missing:
            raise NativeFailure("foreign-image", worker.native_coordinate(), ", ".join(missing))
    return {"entries": entries, "claim": claim}


def claim(worker, unit, checked):
    """Claim the `.nfs` pool cephadm is about to create; every NFS unit of the cluster shares the claim."""
    del unit
    if not checked["entries"] and checked["claim"] is None:
        worker.markers.write(CLAIM, pool_claim(worker))


def settle(worker, unit):
    """Wait, bounded, for cephadm's asynchronous `.nfs` creation, then record its native ID once."""
    del unit
    deadline = worker.clock() + NFS_POOL_WAIT
    if worker.horizon is not None:
        deadline = min(deadline, worker.horizon)
    while True:
        entries = pool_entries(worker)
        if entries and "nfs" in (entries[0].get("application_metadata") or {}):
            break
        require(worker.clock() + NFS_POOL_INTERVAL < deadline, "not-ready")
        worker.pause(NFS_POOL_INTERVAL)
    require(worker.markers.read(CLAIM) == pool_claim(worker), "nfs-pool-foreign")
    identity, wanted = worker.markers.read(IDENTITY), {"id": worker.pool_id(entries[0])}
    require(identity is None or identity == wanted, "nfs-pool-foreign")
    if identity is None:
        worker.markers.write(IDENTITY, wanted)


def expected(unit):
    """Each (service, daemon type) the unit runs, with its daemon count."""
    nfs = unit["documents"][-1]
    result = {("nfs." + unit["id"], "nfs"): nfs["placement"].get("count", len(nfs["placement"]["hosts"]))}
    ingress = ingress_of(unit)
    if ingress is not None:
        count = ingress["placement"].get("count", len(ingress["placement"]["hosts"]))
        for name in needed_images(unit):
            result[("ingress.nfs." + unit["id"], name)] = count
    return result


def reachable(host, number):
    try:
        with socket.create_connection((host, number), timeout=VIP_TIMEOUT):
            return True
    except OSError:
        return False


def readiness(worker, unit, blocking):
    """Read-only: `.nfs` with its application and recorded ID, the cluster listed by the nfs module, and the virtual IP answering."""
    entries = pool_entries(worker)
    identity = worker.markers.read(IDENTITY)
    ready = bool(entries) and identity is not None
    if entries:
        require(worker.markers.read(CLAIM) == pool_claim(worker), "nfs-pool-foreign")
        require(identity is None or identity == {"id": worker.pool_id(entries[0])}, "nfs-pool-foreign")
        ready = ready and "nfs" in (entries[0].get("application_metadata") or {})
    with worker.manager_served():
        clusters = worker.ceph("nfs", "cluster", "ls")
    require(isinstance(clusters, list) and len(clusters) <= 1024 and all(isinstance(name, str) for name in clusters), "native-output")
    ready = ready and unit["id"] in clusters
    found = frontend(unit)
    if found is not None:
        ingress, number = found
        if not reachable(ingress["spec"]["virtual_ip"].partition("/")[0], number):
            blocking.append("vip@ingress.nfs." + unit["id"])
            ready = False
    return ready


def clean(path, root=True):
    """A clean absolute path, as admission's cleanAbsolute: never `//`, which POSIX normalization keeps; `/` only where a root may stand."""
    return (isinstance(path, str) and path.startswith("/") and not path.startswith("//") and posixpath.normpath(path) == path
            and not any(char in path for char in "\x00\r\n") and (root or path != "/"))


def literal(address):
    """One canonical client literal: an address, or a network with no host bits."""
    try:
        if "/" in address:
            return str(ipaddress.ip_network(address, strict=True)) == address
        return str(ipaddress.ip_address(address)) == address
    except ValueError:
        return False


def validate_clients(clients):
    require(isinstance(clients, list))
    seen = set()
    for client in clients:
        require(isinstance(client, dict) and set(client) == CLIENT_MEMBERS and client["access_type"] in ACCESS and client["squash"] in SQUASH)
        addresses = client["addresses"]
        require(isinstance(addresses, list) and bool(addresses) and all(isinstance(address, str) and literal(address) for address in addresses))
        require(not seen & set(addresses) and len(set(addresses)) == len(addresses))
        seen |= set(addresses)


def validate_native(native, filesystem):
    """The frozen native export: NFSv4 over TCP with sys security, a CephFS FSAL on the bound filesystem, and no generated member."""
    require(isinstance(native, dict) and NATIVE_REQUIRED <= set(native) <= NATIVE_REQUIRED | NATIVE_OPTIONAL)
    require(clean(native["pseudo"], root=False) and ("path" not in native or clean(native["path"])))
    require(isinstance(native["access_type"], str) and native["access_type"] in ACCESS)
    require(isinstance(native["squash"], str) and native["squash"] in SQUASH and type(native["security_label"]) is bool)
    require(canonical(native["protocols"]) == b"[4]" and canonical(native["transports"]) == b'["TCP"]')
    require("sectype" not in native or canonical(native["sectype"]) == b'["sys"]')
    fsal = native["fsal"]
    require(isinstance(fsal, dict) and set(fsal) == {"cmount_path", "fs_name", "name"} and fsal["fs_name"] == filesystem and fsal["name"] == "CEPH")
    require(clean(fsal["cmount_path"]))
    validate_clients(native["clients"])


def validate_export(request, hosts):
    """One export of a declared NFS service, bound to a filesystem path or to the path its subvolume's creation proof reports."""
    del hosts
    nfs = request.get("nfsExport")
    require(isinstance(nfs, dict) and EXPORT_MEMBERS <= set(nfs) <= EXPORT_MEMBERS | {"subvolumePath"})
    require(named(nfs["name"]) and named(nfs["service"]) and named(nfs["filesystem"]))
    require(nfs["subvolume"] == "" or named(nfs["subvolume"]))
    require(nfs["group"] == "" or named(nfs["group"]) and nfs["subvolume"] != "")
    require("nfs." + nfs["service"] in [entry["name"] for entry in request.get("delegated", [])])
    validate_native(nfs["native"], nfs["filesystem"])
    if nfs["subvolume"] == "":
        require("path" in nfs["native"] and "subvolumePath" not in nfs)
        return
    root = nfs.get("subvolumePath")
    match = ceph_native.SUBVOLUME_PATH.fullmatch(root) if isinstance(root, str) else None
    require(match is not None and root == "/volumes/" + (nfs["group"] or "_nogroup") + "/" + nfs["subvolume"] + "/" + match.group(1))


class ExportWorker(ceph_native_resources.ResourceWorker):
    """The core worker with one NFS export's apply phase and its read-only observation."""

    def export(self):
        return self.request["nfsExport"]

    def recovery_scope(self):
        """The cluster's bootstrap scope: the export member is this block's own input, never part of the cluster's authoring."""
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "nfsExport"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def records(self):
        name = self.export()["name"]
        return "nfs-export-attempt." + name + ".json", "nfs-export-id." + name + ".json"

    def resolved(self, escape=True):
        """The path to export: authored, or the subvolume's proved path; within that subvolume and below its CephFS mount."""
        nfs = self.export()
        native = nfs["native"]
        if nfs["subvolume"] == "":
            path = native["path"]
        else:
            root = nfs["subvolumePath"]
            path = native.get("path") or root
            require(not escape or path == root or path.startswith(root + "/"), "nfs-export-path-escape")
        cmount = native["fsal"]["cmount_path"]
        require(not escape or cmount in ("/", path) or path.startswith(cmount + "/"), "nfs-export-path-escape")
        return path

    def intent(self, path):
        nfs = self.export()
        return {"export": nfs["name"], "fsid": self.request["fsid"], "native": dict(nfs["native"], path=path), "owner": self.request["owner"],
                "root": nfs.get("subvolumePath", ""), "service": nfs["service"]}

    def held(self, intent):
        """This operation's attempt and identity records, each its own or absent; a moved subvolume path is the subvolume's identity."""
        attempt_name, identity_name = self.records()
        attempt, identity = self.markers.read(attempt_name), self.markers.read(identity_name)
        if attempt is not None and attempt != intent:
            moved = (isinstance(attempt, dict) and isinstance(attempt.get("native"), dict)
                     and dict(attempt, root=intent["root"], native=dict(attempt["native"], path=intent["native"]["path"])) == intent)
            require(not moved, "subvolume-identity")
            raise Refusal("ownership")
        require(identity is None or attempt is not None and isinstance(identity, dict) and set(identity) == {"exportId", "userId"}
                and type(identity["exportId"]) is int and EXPORT_ID.fullmatch(str(identity["exportId"])) is not None
                and isinstance(identity["userId"], str) and USER_ID.fullmatch(identity["userId"]) is not None, "ownership")
        return attempt, identity

    def info(self):
        """`nfs export info` (src/pybind/mgr/nfs/export.py:508-520): {} when absent, else Export.to_dict, its FSAL secrets dropped first."""
        nfs = self.export()
        info = self.manager_retry(self.ceph, "nfs", "export", "info", nfs["service"], nfs["native"]["pseudo"])
        require(isinstance(info, dict), "native-output")
        if isinstance(info.get("fsal"), dict):
            info = dict(info, fsal={key: value for key, value in info["fsal"].items() if key not in FSAL_SECRETS})
        if not info:
            return info
        fields = NATIVE_REQUIRED | {"export_id", "cluster_id", "path"}
        fsal = info.get("fsal")
        require(fields <= set(info) <= fields | {"sectype"} and type(info["export_id"]) is int and EXPORT_ID.fullmatch(str(info["export_id"])) is not None
                and info["cluster_id"] == nfs["service"] and info["pseudo"] == nfs["native"]["pseudo"], "native-output")
        require(isinstance(fsal, dict) and isinstance(fsal.get("name"), str), "native-output")
        require(fsal["name"] != "CEPH" or isinstance(fsal.get("user_id"), str) and USER_ID.fullmatch(fsal["user_id"]) is not None, "native-output")
        return info

    @staticmethod
    def projection(native):
        """The stored export as authored: no generated ID, cluster or principal, its protocol and transport lists in one order."""
        result = {key: value for key, value in native.items() if key not in ("export_id", "cluster_id")}
        result["fsal"] = {key: value for key, value in native["fsal"].items() if key != "user_id"}
        for key in ("protocols", "transports"):
            if isinstance(result[key], list):
                result[key] = sorted(result[key], key=canonical)
        return result

    def matches(self, info, intent):
        return canonical(self.projection(info)) == canonical(self.projection(intent["native"]))

    @staticmethod
    def identity_of(info):
        return {"exportId": info["export_id"], "userId": info["fsal"].get("user_id")}

    def foreign(self, detail):
        nfs = self.export()
        return NativeFailure("nfs-export-foreign", self.native_coordinate(), "export " + nfs["native"]["pseudo"] + " on nfs." + nfs["service"] + " " + detail)

    def unlisted(self):
        """The NFS service the export joins, as the nfs module lists it (src/pybind/mgr/nfs/module.py:151-155)."""
        service = self.export()["service"]
        clusters = self.manager_retry(self.ceph, "nfs", "cluster", "ls")
        require(isinstance(clusters, list) and len(clusters) <= MAX_CLUSTERS and all(isinstance(name, str) for name in clusters), "native-output")
        if service not in clusters:
            raise NativeFailure("nfs-export-apply", self.native_coordinate(), "nfs." + service + " is not listed by ceph nfs cluster ls")

    def subvolume_path(self):
        """`fs subvolume getpath` (src/pybind/mgr/volumes/fs/volume.py:484-498) prints the path as text; never `--group_name _nogroup`."""
        nfs = self.export()
        group = ("--group_name", nfs["group"]) if nfs["group"] else ()
        output = self.manager_retry(self.ceph, "fs", "subvolume", "getpath", nfs["filesystem"], nfs["subvolume"], *group, json_result=False)
        try:
            text = bytes(output).decode("utf-8")
        except UnicodeError:
            raise Refusal("native-output") from None
        return text[:-1] if text.endswith("\n") else text

    def applied(self, intent, expected):
        """One declarative apply of the frozen JSON on stdin; a failure, a warning or another state is the NFS manager's answer, never retried."""
        nfs = self.export()
        payload = canonical(intent["native"])
        try:
            answer = self.tunnel_effect("nfs", "export", "apply", nfs["service"], "-i", "-", data=payload)
        except NativeFailure as error:
            if str(error) != "native-command":
                raise
            refused = Refusal("nfs-export-apply")
            refused.native_error = error.native_error
            raise refused from None
        require(isinstance(answer, list) and len(answer) == 1 and isinstance(answer[0], dict) and answer[0].get("pseudo") == nfs["native"]["pseudo"],
                "native-output")
        if answer[0].get("state") != expected:
            raise NativeFailure("nfs-export-apply", self.native_coordinate(), canonical(answer), secrets=(payload,))

    def readback(self, intent, identity=None):
        info = self.info()
        if not info or not self.matches(info, intent) or identity is not None and self.identity_of(info) != identity:
            raise NativeFailure("nfs-export-apply", self.native_coordinate(), "export " + intent["native"]["pseudo"] + " on nfs." + intent["service"]
                                + " reads back other than applied")
        return info

    def nfs_export(self):
        """Prove ownership, the module, the service, the bound path and the pseudo path's owner before any write; then apply or adopt once."""
        nfs = self.export()
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        if "nfs" not in self.manager_modules():
            raise Refusal("nfs-module-disabled")
        self.unlisted()
        if nfs["subvolume"]:
            require(self.subvolume_path() == nfs["subvolumePath"], "subvolume-identity")
        intent = self.intent(self.resolved())
        attempt, identity = self.held(intent)
        info = self.info()
        attempt_name, identity_name = self.records()
        if identity is not None:
            if not info or self.identity_of(info) != identity:
                raise self.foreign("that this operation created is gone or changed")
            if self.matches(info, intent):
                return {"changed": False}
            self.applied(intent, "updated")
            self.readback(intent, identity)
            return {"changed": True}
        if info:
            if attempt is None:
                raise self.foreign("has no creation record of this operation")
            if not self.matches(info, intent):
                raise self.foreign("is not the export this operation attempted")
            self.markers.write(identity_name, self.identity_of(info))
            return {"changed": True}
        if attempt is None:
            self.markers.write(attempt_name, intent)
        self.applied(intent, "added")
        self.markers.write(identity_name, self.identity_of(self.readback(intent)))
        return {"changed": True}

    def unfinished(self, evidence):
        if self.request["action"] != "nfs-export":
            return super().unfinished(evidence)
        owned = self.markers.read(self.records()[0]) == self.intent(self.resolved(escape=False))
        evidence["owned"] = owned
        evidence["exists"] = owned
        evidence["partial"] = owned
        return evidence

    def observe_resource(self, evidence, ready):
        """Read-only: a recorded export of this operation's ID and principal; a drifted one is not ready and the next apply re-applies it."""
        if self.request["action"] != "nfs-export":
            return super().observe_resource(evidence, ready)
        if "nfs" not in self.manager_modules():
            raise Refusal("nfs-module-disabled")
        intent = self.intent(self.resolved(escape=False))
        attempt, identity = self.held(intent)
        info = self.info()
        if attempt is None and info:
            raise self.foreign("has no creation record of this operation")
        if identity is not None and info and self.identity_of(info) != identity:
            raise self.foreign("that this operation created is gone or changed")
        recorded = identity is not None and bool(info)
        evidence["owned"] = attempt is not None
        evidence["exists"] = evidence["owned"] or bool(info)
        evidence["resource"] = {"id": str(identity["exportId"]), "path": intent["native"]["pseudo"]} if recorded else {"id": "", "path": ""}
        return ready and recorded and self.matches(info, intent)
