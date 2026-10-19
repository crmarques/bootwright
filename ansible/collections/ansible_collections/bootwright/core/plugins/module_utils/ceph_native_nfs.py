"""Fixed NFS service-unit hooks, shipped beside the core worker as source.

An NFS unit deploys one `nfs` service, after the `ingress` that fronts it when one is declared. cephadm creates
the shared `.nfs` pool and enables its `nfs` application when it configures the service
(src/pybind/mgr/cephadm/services/nfs.py:64-68, src/pybind/mgr/nfs/cluster.py:45-54); the hooks claim that pool
before the first apply, record its native ID after it, and never adopt, rewrite or delete it. The hooks never
enable a manager module or write an image setting: the cluster's settings block is their single writer.
Like the core worker it imports only the target Python standard library and the core worker.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import ipaddress
import socket

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, Refusal, named, require

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
    ingress = ingress_of(unit)
    if ingress is not None:
        spec = ingress["spec"]
        if spec.get("keepalive_only"):
            number = unit["documents"][-1].get("spec", {}).get("port", NFS_PORT)
        else:
            number = spec.get("frontend_port", NFS_PORT)
        if not reachable(spec["virtual_ip"].partition("/")[0], number):
            blocking.append("vip@ingress.nfs." + unit["id"])
            ready = False
    return ready
