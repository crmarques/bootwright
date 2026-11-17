"""The closed public evidence schema of a Ceph block: every projection is checked before any result publication."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import ipaddress
import re

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_host_runtime, ceph_native, ceph_native_nfs, ceph_native_object, ceph_native_observability, ceph_native_stretch, ceph_native_units,
    native_specializations,
)


# Every id the shipped specialization registry declares; a worker answer or a record naming any other id is refused.
SPECIALIZATION_IDS = frozenset(record.id for record in native_specializations.SPECIALIZATIONS)


# A provided tiebreaker's removal evidence (ceph_native_stretch.TiebreakerRemoval); its Go caller decodes the same closed members.
TIEBREAKER_EVIDENCE = "ceph-tiebreaker-evidence-v1"


TIEBREAKER_MEMBERS = frozenset(("version", "request", "owner", "fsid", "host", "removed", "attempted"))


UNIT_DAEMON = re.compile(r"[a-z][a-z0-9-]*\.[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\Z")


def subvolume_resource(resource, request):
    """A subvolume's resource is empty, or its uuid with the exact path the frozen subvolume's group and name place it at."""
    if resource == {"id": "", "path": ""}:
        return
    match = ceph_native.SUBVOLUME_PATH.fullmatch(resource["path"])
    ceph_native.require(ceph_native.SUBVOLUME_UUID.fullmatch(resource["id"]) is not None and match is not None and match.group(1) == resource["id"],
                        "evidence")
    if request is not None:
        subvolume = request["subvolume"]
        path = "/volumes/" + (subvolume["group"] or "_nogroup") + "/" + subvolume["name"] + "/" + resource["id"]
        ceph_native.require(resource["path"] == path, "evidence")


def gateway_resource(resource, request):
    """A gateway's resource is empty, or its zone's native ID with the endpoint FQDN the frozen gateway records."""
    if resource == {"id": "", "path": ""}:
        return
    path = resource["path"]
    ceph_native.require(ceph_native.SUBVOLUME_UUID.fullmatch(resource["id"]) is not None and len(path) <= 253
                        and (path == "" or all(ceph_native_object.label(part) for part in path.split("."))), "evidence")
    ceph_native.require(request is None or request["action"] == "gateway" and path == request["gateway"]["endpoint"], "evidence")


def nfs_export_resource(resource, request):
    """An NFS export's resource is empty, or its generated export ID with the pseudo path the frozen export records."""
    if resource == {"id": "", "path": ""}:
        return
    ceph_native.require(ceph_native_nfs.EXPORT_ID.fullmatch(resource["id"]) is not None and ceph_native_nfs.clean(resource["path"], root=False), "evidence")
    ceph_native.require(request is None or request["action"] == "nfs-export" and resource["path"] == request["nfsExport"]["native"]["pseudo"], "evidence")


def object_user_resource(resource, request):
    """An object user's resource is empty, or its native user ID once its retained key pair is proved; never a key."""
    if resource == {"id": "", "path": ""}:
        return
    ceph_native.require(ceph_native_object.label(resource["id"]) and resource["path"] == "", "evidence")
    ceph_native.require(request is None or request["action"] == "object-user" and resource["id"] == request["objectUser"]["name"], "evidence")


def object_bucket_resource(resource, request):
    """An object bucket's resource is empty, or its native bucket name once its identity and policy are proved."""
    if resource == {"id": "", "path": ""}:
        return
    ceph_native.require(ceph_native_object.label(resource["id"]) and resource["path"] == "", "evidence")
    ceph_native.require(request is None or request["action"] == "object-bucket" and resource["id"] == request["objectBucket"]["name"], "evidence")


def specialization_ids(value):
    """A sorted, duplicate-free list of 1 or more ids the shipped registry declares."""
    return (isinstance(value, list) and 0 < len(value) <= len(SPECIALIZATION_IDS)
            and all(isinstance(name, str) and name in SPECIALIZATION_IDS for name in value) and value == sorted(set(value)))


def evidence(value, request_digest, request=None):
    """The closed public projection, checked before any result publication."""
    if isinstance(value, dict) and value.get("version") == ceph_host_runtime.EVIDENCE_VERSION:
        return host_evidence(value, request_digest, request)
    fields = {"version", "request", "action", "fsid", "owner", "owned", "exists", "ready", "removed", "partial", "health", "hosts", "monitors", "monAddrs",
              "mgrActive", "mgrStandbys", "osds", "resource", "client", "dashboard", "endpoint", "daemons", "sso", "incarnation"}
    ceph_native.require(isinstance(value, dict) and fields <= set(value) <= fields | {"specializations", "stretch", "pool", "inventory"}, "evidence")
    ceph_native_stretch.evidence(value, request)
    reading_evidence(value, request)
    ceph_native.require("specializations" not in value or specialization_ids(value["specializations"]), "evidence")
    ceph_native.require(value["version"] == ceph_native.EVIDENCE_VERSION and value["request"] == request_digest and ceph_native.digest(request_digest),
                        "evidence")
    dashboard_evidence(value)
    endpoint_evidence(value, request)
    sso_evidence(value, request)
    actions = ("cluster", "pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings", "service", "gateway", "nfs-export", "object-user",
               "object-bucket", "nvmeof")
    ceph_native.require(value["action"] in actions, "evidence")
    daemon_evidence(value, request)
    ceph_native.require(all(type(value[k]) is bool for k in ("owned", "exists", "ready", "removed", "partial")), "evidence")
    ceph_native.require(value["health"] in ("", "HEALTH_OK", "HEALTH_WARN", "HEALTH_ERR"), "evidence")
    ceph_native.require(ceph_native.named(value["owner"]), "evidence")
    for field in ("hosts", "monitors", "mgrStandbys"):
        ceph_native.require(isinstance(value[field], list) and len(value[field]) <= 128 and all(ceph_native.named(n) for n in value[field]), "evidence")
        ceph_native.require(len(set(value[field])) == len(value[field]), "evidence")
    incarnation = value["incarnation"]
    ceph_native.require(incarnation == "" or isinstance(incarnation, str) and ceph_native.INCARNATION.fullmatch(incarnation) is not None, "evidence")
    for field in ("mgrActive", "client"):
        ceph_native.require(value[field] == "" or ceph_native.named(value[field]), "evidence")
    resource = value["resource"]
    ceph_native.require(isinstance(resource, dict) and set(resource) == {"id", "path"} and all(isinstance(resource[k], str) for k in ("id", "path")),
                        "evidence")
    if value["action"] == "subvolume":
        subvolume_resource(resource, request)
    elif value["action"] == "gateway":
        gateway_resource(resource, request)
    elif value["action"] == "nfs-export":
        nfs_export_resource(resource, request)
        ceph_native.require(value["client"] == "", "evidence")
    elif value["action"] == "object-user":
        object_user_resource(resource, request)
        ceph_native.require(value["client"] == "", "evidence")
    elif value["action"] == "object-bucket":
        object_bucket_resource(resource, request)
        ceph_native.require(value["client"] == "", "evidence")
    else:
        ceph_native.require(resource["path"] == "", "evidence")
    ceph_native.require(resource["id"] == "" or value["action"] in ("subvolume", "gateway", "nfs-export", "object-user", "object-bucket")
                        or value["action"] in ("pool", "policy", "filesystem") and ceph_native.RESOURCE_ID.fullmatch(resource["id"]) is not None
                        or value["action"] == "nvmeof" and re.fullmatch(r"bw[0-9a-f]{18}", resource["id"]) is not None
                        or value["action"] == "rbd" and ceph_native.RBD_IMAGE_ID.fullmatch(resource["id"]) is not None, "evidence")
    ceph_native.require(isinstance(value["osds"], list) and len(value["osds"]) <= 8192, "evidence")
    for osd in value["osds"]:
        ceph_native.require(isinstance(osd, dict) and set(osd) == {"host", "path", "id", "uuid"}, "evidence")
        ceph_native.require(ceph_native.named(osd["host"]) and isinstance(osd["path"], str) and ceph_native.DEVICE.fullmatch(osd["path"]) is not None,
                            "evidence")
        ceph_native.require(type(osd["id"]) is int and osd["id"] >= 0 and isinstance(osd["uuid"], str), "evidence")
    if request is not None:
        ceph_native.require(all(value[k] == request[k] for k in ("owner", "fsid", "action")), "evidence")
        wanted = {h["name"] for h in request["hosts"]}
        ceph_native.require(set(value["hosts"]) <= wanted and set(value["monitors"]) <= wanted, "evidence")
        ceph_native.require({item["host"] for item in value["monAddrs"]} <= wanted, "evidence")
    ceph_native.require(not value["ready"] or value["owned"] and value["exists"] and not value["partial"] and not value["removed"], "evidence")
    ceph_native.require(not value["removed"] or value["owned"] and not value["exists"] and not value["ready"], "evidence")
    ceph_native.require(len(ceph_native.canonical(value)) <= 60000, "evidence")
    return value


POOL_READING = frozenset(("applications", "autoscale", "maxBytes", "maxObjects", "minSize", "name", "size", "type"))


def pool_evidence(pool):
    """One pool's read values: a bounded name, its protection, replica pair, applications, autoscale mode and quota."""
    ceph_native.require(isinstance(pool, dict) and set(pool) == POOL_READING and isinstance(pool["name"], str)
                        and ceph_native.POOL_NAME.fullmatch(pool["name"]) is not None, "evidence")
    ceph_native.require(pool["type"] in ("", "replicated", "erasure") and pool["autoscale"] in ("", *ceph_native.POOL_AUTOSCALE), "evidence")
    ceph_native.require(all(type(pool[k]) is int and 0 <= pool[k] <= ceph_native.INT64_MAX for k in ("size", "minSize", "maxBytes", "maxObjects")), "evidence")
    applications = pool["applications"]
    ceph_native.require(isinstance(applications, list) and len(applications) <= ceph_native.POOL_APPLICATIONS
                        and all(isinstance(name, str) and ceph_native.POOL_APPLICATION.fullmatch(name) for name in applications)
                        and applications == sorted(set(applications)), "evidence")


def reading_evidence(value, request=None):
    """A pool block's own read values, and a read-only cluster observation's pool inventory and OSDs no declared service places."""
    if "pool" in value:
        ceph_native.require(value["action"] == "pool" and value["exists"], "evidence")
        pool_evidence(value["pool"])
        ceph_native.require(request is None or value["pool"]["name"] == request["pool"]["name"], "evidence")
    if "inventory" not in value:
        return
    inventory = value["inventory"]
    ceph_native.require(value["action"] == "cluster" and isinstance(inventory, dict) and set(inventory) == {"pools", "omitted", "osds"},
                        "evidence")
    pools, osds = inventory["pools"], inventory["osds"]
    ceph_native.require(type(inventory["omitted"]) is int and 0 <= inventory["omitted"] <= ceph_native.POOL_LISTING, "evidence")
    ceph_native.require(isinstance(pools, list) and len(pools) <= ceph_native.POOL_LIMIT, "evidence")
    for pool in pools:
        pool_evidence(pool)
    names = [pool["name"] for pool in pools]
    ceph_native.require(names == sorted(set(names)), "evidence")
    ceph_native.require(isinstance(osds, list) and len(osds) <= ceph_native.UNPLACED_LIMIT, "evidence")
    wanted = None if request is None else {host["name"] for host in request["hosts"]}
    for osd in osds:
        # An unplaced OSD's host, data device and OSD fsid are each empty when the observation could not name them.
        ceph_native.require(isinstance(osd, dict) and set(osd) == {"host", "path", "id", "uuid"}
                            and (osd["host"] == "" or ceph_native.named(osd["host"]) and (wanted is None or osd["host"] in wanted)), "evidence")
        ceph_native.require(osd["path"] == "" or isinstance(osd["path"], str) and osd["host"] != "" and osd["path"].startswith("/dev/")
                            and ceph_native.KERNEL_DEVICE.fullmatch(osd["path"][len("/dev/"):]) is not None, "evidence")
        ceph_native.require(type(osd["id"]) is int and 0 <= osd["id"] <= 99999 and isinstance(osd["uuid"], str)
                            and (osd["uuid"] == "" or ceph_native.OSD_UUID.fullmatch(osd["uuid"]) is not None), "evidence")
    ids = [osd["id"] for osd in osds] + [osd["id"] for osd in value["osds"]]
    ceph_native.require(len(set(ids)) == len(ids), "evidence")


def daemon_evidence(value, request=None):
    """A service unit's or gateway's daemons: unique names, each on a host of the document it belongs to; every other action has none."""
    daemons = value["daemons"]
    if value["action"] not in ("service", "gateway"):
        ceph_native.require(daemons == [], "evidence")
        return
    ceph_native.require(isinstance(daemons, list) and len(daemons) <= 256, "evidence")
    hosts = None
    if request is not None:
        ceph_native.require(request["action"] == value["action"], "evidence")
        documents = request[value["action"]]["documents"]
        hosts = {ceph_native_units.service_name(document): document["placement"]["hosts"] for document in documents}
    names = set()
    for daemon in daemons:
        ceph_native.require(isinstance(daemon, dict) and set(daemon) == {"host", "name", "service"} and ceph_native.named(daemon["host"]), "evidence")
        ceph_native.require(isinstance(daemon["name"], str) and UNIT_DAEMON.fullmatch(daemon["name"]) is not None and daemon["name"] not in names, "evidence")
        ceph_native.require(isinstance(daemon["service"], str) and ceph_native.DELEGATED_SERVICE.fullmatch(daemon["service"]) is not None, "evidence")
        ceph_native.require(hosts is None or daemon["host"] in hosts.get(daemon["service"], ()), "evidence")
        names.add(daemon["name"])


def dashboard_evidence(value):
    """Monitor addresses and the dashboard's public access facts: never its password or hash."""
    addresses = value["monAddrs"]
    ceph_native.require(isinstance(addresses, list) and len(addresses) <= 128, "evidence")
    for item in addresses:
        ceph_native.require(isinstance(item, dict) and set(item) == {"host", "address"} and ceph_native.named(item["host"]), "evidence")
        try:
            ceph_native.require(isinstance(item["address"], str) and str(ipaddress.ip_address(item["address"])) == item["address"], "evidence")
        except ValueError:
            raise ceph_native.Refusal("evidence") from None
    ceph_native.require(len({item["host"] for item in addresses}) == len(addresses), "evidence")
    dashboard = value["dashboard"]
    ceph_native.require(isinstance(dashboard, dict) and set(dashboard) == {"url", "certificateSHA256", "reachable"}, "evidence")
    ceph_native.require(isinstance(dashboard["url"], str) and (dashboard["url"] == "" or ceph_native.DASHBOARD_URL.fullmatch(dashboard["url"]) is not None),
                        "evidence")
    ceph_native.require(dashboard["certificateSHA256"] == "" or ceph_native.digest(dashboard["certificateSHA256"]), "evidence")
    proved = dashboard["url"] and dashboard["certificateSHA256"]
    ceph_native.require(type(dashboard["reachable"]) is bool and (not dashboard["reachable"] or proved), "evidence")
    if value["action"] != "cluster":
        ceph_native.require(not addresses and dashboard == {"url": "", "certificateSHA256": "", "reachable": False}, "evidence")


def endpoint_evidence(value, request=None):
    """Whether a gateway's terminating service answered and, with TLS, the SHA-256 of the bound leaf it served; a management gateway unit's
    served leaf SHA-256, present exactly when it answered; every other action has none."""
    endpoint = value["endpoint"]
    ceph_native.require(isinstance(endpoint, dict) and set(endpoint) == {"certificateSHA256", "reachable"}, "evidence")
    ceph_native.require(endpoint["certificateSHA256"] == "" or ceph_native.digest(endpoint["certificateSHA256"]), "evidence")
    ceph_native.require(type(endpoint["reachable"]) is bool, "evidence")
    if value.get("action") == "service" and request is not None and request.get("action") == "service" and request["service"]["type"] == "mgmt-gateway":
        ceph_native.require((endpoint["certificateSHA256"] != "") == endpoint["reachable"], "evidence")
        return
    # The result publisher holds no request: a management gateway unit is the service whose own daemon rows name mgmt-gateway.
    if value.get("action") == "service" and request is None and endpoint != {"certificateSHA256": "", "reachable": False}:
        rows = value.get("daemons")
        ceph_native.require(isinstance(rows, list) and any(isinstance(row, dict) and row.get("service") == "mgmt-gateway" for row in rows)
                            and (endpoint["certificateSHA256"] != "") == endpoint["reachable"], "evidence")
        return
    if value.get("action") != "gateway":
        ceph_native.require(endpoint == {"certificateSHA256": "", "reachable": False}, "evidence")
        return
    if request is not None:
        tls = bool(request["gateway"]["bindings"])
        ceph_native.require(tls or endpoint["certificateSHA256"] == "", "evidence")
        ceph_native.require(not (endpoint["reachable"] and tls) or endpoint["certificateSHA256"] != "", "evidence")


def proxied(request):
    """Whether a request is a management gateway unit with an oauth2-proxy document."""
    return (request["action"] == "service" and request["service"]["type"] == "mgmt-gateway"
            and ceph_native_observability.gateway_documents(request["service"])[2] is not None)


def sso_evidence(value, request=None):
    """Whether the dashboard's OAuth2 single sign-on is enabled: only a management gateway unit with an oauth2-proxy reports it."""
    ceph_native.require(value["sso"] in ("", "oauth2"), "evidence")
    if value["sso"] == "oauth2":
        ceph_native.require(value.get("action") == "service" and (request is None or proxied(request)), "evidence")


def tiebreaker_host(request):
    """The one provided host of a stretch cluster, which is its tiebreaker."""
    stretch = request.get("stretch")
    hosts = [host for host in request["hosts"] if host.get("provided") is True]
    ceph_native.require(request["action"] == "cluster" and isinstance(stretch, dict) and len(hosts) == 1 and hosts[0]["name"] == stretch["tiebreaker"],
                        "request")
    return hosts[0]


def tiebreaker_evidence(value, request_digest, request=None):
    """The closed evidence of a provided tiebreaker's removal or its observation; never a host's native output."""
    ceph_native.require(isinstance(value, dict) and set(value) == TIEBREAKER_MEMBERS and value["version"] == TIEBREAKER_EVIDENCE, "evidence")
    ceph_native.require(ceph_native.digest(request_digest) and value["request"] == request_digest, "evidence")
    ceph_native.require(all(ceph_native.named(value[key]) for key in ("owner", "host")) and isinstance(value["fsid"], str), "evidence")
    ceph_native.require(type(value["removed"]) is bool and type(value["attempted"]) is bool, "evidence")
    if request is not None:
        ceph_native.require(value["owner"] == request["owner"] and value["fsid"] == request["fsid"] and value["host"] == tiebreaker_host(request)["name"],
                            "evidence")
    return value


def host_evidence(value, request_digest, request=None):
    """The closed host block projection; no credential, path or vendor text."""
    fields = {"version", "request", "host", "owner", "fsid", "image", "ready", "removed", "imageID", "uid", "gid", "label", "policySHA256",
              "cephadmSHA256", "planDigest", "route", "devices"}
    ceph_native.require(isinstance(value, dict) and fields <= set(value) <= fields | {"specializations", "fips", "nativeRemoved"}, "evidence")
    ceph_native.require("nativeRemoved" not in value or value["nativeRemoved"] is True and value["removed"], "evidence")
    ceph_native.require("specializations" not in value or specialization_ids(value["specializations"]), "evidence")
    ceph_native.require("fips" not in value or ceph_native.fips_proved(value["fips"]), "evidence")
    ceph_native.require(value["version"] == ceph_host_runtime.EVIDENCE_VERSION and value["request"] == request_digest and ceph_native.digest(request_digest),
                        "evidence")
    ceph_native.require(all(type(value[key]) is bool for key in ("ready", "removed", "route")), "evidence")
    ceph_native.require(all(type(value[key]) is int and 0 <= value[key] < 65536 for key in ("uid", "gid")), "evidence")
    ceph_native.require(ceph_native.named(value["host"]) and ceph_native.named(value["owner"]) and isinstance(value["image"], str), "evidence")
    digests = ("imageID", "policySHA256", "cephadmSHA256", "planDigest")
    ceph_native.require(all(value[key] == "" or ceph_native.digest(value[key]) for key in digests), "evidence")
    ceph_native.require(value["label"] in ("", ceph_host_runtime.SHARED_LABEL), "evidence")
    ceph_native.require(isinstance(value["devices"], list) and len(value["devices"]) <= 128, "evidence")
    for device in value["devices"]:
        ceph_native.require(isinstance(device, dict) and set(device) in ({"path", "serial", "deviceID"},
                            {"path", "serial", "deviceID", "wwn", "kernelPath", "sizeBytes", "rotational"},
                            {"path", "serial", "deviceID", "wwn", "kernelPath", "sizeBytes", "rotational", "alias"}), "evidence")
        ceph_native.require(all(isinstance(device[key], str) for key in ("path", "serial", "deviceID"))
                            and ceph_host_runtime.DEVICE_ID.fullmatch(device["deviceID"]) is not None, "evidence")
        if "wwn" in device:
            ceph_native.require(request is not None, "evidence")
            ceph_host_runtime.row(dict(device, host=value["host"]), request["cluster"], value["host"])
    ceph_native.require(not (value["ready"] and value["removed"]), "evidence")
    if request is not None:
        cluster = request["cluster"]
        ceph_native.require(value["host"] == request["host"] and value["owner"] == cluster["owner"] and value["fsid"] == cluster["fsid"]
                            and value["image"] == cluster["image"], "evidence")
        host = next(item for item in cluster["hosts"] if item["name"] == request["host"])
        ceph_native.require(("fips" in value) == (ceph_host_runtime.observes_fips(cluster, host) and not value["removed"]), "evidence")
    # Fifteen serial selections, each with its longest serial, device ID, observed WWN and alias, fit below Go's 24 KiB decoder.
    ceph_native.require(len(ceph_native.canonical(value)) <= 24000, "evidence")
    return value
