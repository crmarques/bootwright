"""Fixed Ceph monitoring service-unit hooks, shipped beside the core worker as source.

A monitoring unit deploys every declared prometheus, alertmanager, grafana, node-exporter, ceph-exporter and snmp-gateway
service as singleton documents in one `orch apply -i -` stream. cephadm names each such service by its type and each daemon
`<type>.<host short name>` (src/pybind/mgr/cephadm/module.py:922-966); every daemon runs its frozen auxiliary image, and
ceph-exporter the Ceph image. The hooks only read the frozen image settings, whose single writer is the cluster's
settings block. cephadm itself enables the prometheus manager module for a declared prometheus service
(src/pybind/mgr/cephadm/services/monitoring.py:480-489), so readiness observes it in `ceph mgr services`, which the MON
answers from the MgrMap (src/mon/MgrMonitor.cc:1085-1094); the hooks never enable a module or write a setting. The grafana
password and SNMP credentials are bound leaves the generic unit worker splices into the apply stream only. Like the core
worker it imports only the target Python standard library and the core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_nfs
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, require

MONITORING_TYPES = (
    "alertmanager",
    "ceph-exporter",
    "grafana",
    "node-exporter",
    "prometheus",
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
IMAGES = {"alertmanager": "alertmanager", "ceph-exporter": None, "grafana": "grafana", "node-exporter": "node_exporter", "prometheus": "prometheus",
          "snmp-gateway": "snmp_gateway"}
# The spec members each type realizes, as the Go planner's unitSpecMembers rows.
MEMBERS = {
    "alertmanager": frozenset(("only_bind_port_on_networks", "port", "user_data")),
    "ceph-exporter": frozenset(("port", "prio_limit", "stats_period")),
    "grafana": frozenset(("anonymous_access", "only_bind_port_on_networks", "port", "protocol")),
    "node-exporter": frozenset(("port",)),
    "prometheus": frozenset(("only_bind_port_on_networks", "port", "retention_size", "retention_time")),
    "snmp-gateway": frozenset(("auth_protocol", "engine_id", "port", "privacy_protocol", "snmp_destination", "snmp_version")),
}
CHOICES = {"auth_protocol": ("MD5", "SHA"), "privacy_protocol": ("DES", "AES"), "protocol": ("http", "https"), "snmp_version": ("V2c", "V3")}
FLAGS = frozenset(("anonymous_access", "only_bind_port_on_networks"))
MAX_TEXT = 256
MAX_WEBHOOKS = 16
MAX_MANAGER_SERVICES = 64
MODULE = "prometheus"


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
            ceph_native_nfs.networks(document["networks"])
    frozen = {entry["name"] for entry in request.get("auxiliary", [])}
    require(set(needed_images(unit)) <= frozen)


def check(worker, unit):
    """Read-only, before any write: the settings block holds the frozen image setting of every auxiliary image the unit runs."""
    needed = needed_images(unit)
    if needed:
        unused, held = worker.image_settings()
        missing = sorted(worker.image_key(name) for name in needed if worker.image_key(name) not in held)
        if missing:
            raise NativeFailure("foreign-image", worker.native_coordinate(), ", ".join(missing))


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
