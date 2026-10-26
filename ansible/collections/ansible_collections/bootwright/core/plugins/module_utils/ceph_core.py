"""Controller-local Ceph coordinator and isolated SSH material transport."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import concurrent.futures
import hashlib
import inspect
import ipaddress
import json
import os
import re
import socket
import ssl
import tempfile
import threading
import time

from ansible_collections.bootwright.core.plugins.module_utils import (
    adapter_protocol, bounded_process, canonical_json,
    ceph_crypto_policy, ceph_host_route_probe, ceph_host_runtime, ceph_host_worker, ceph_native, ceph_native_nfs, ceph_native_nvmeof,
    ceph_native_observability,
    ceph_native_resources, ceph_native_observe, ceph_native_units, ceph_native_object, ceph_native_worker, ceph_package_sources, ceph_package_worker,
    ceph_packages, ceph_registry_discovery, ceph_smb_native, ceph_replication_native, ceph_ssh_policy, ceph_tunnel,
    ceph_tunnel_staging, ceph_tunnel_custody, native_apply, native_diagnostics, native_specializations, native_resolution, native_signatures, ceph_pull_broker,
    physical_machine_identity, ceph_export_native, ceph_export_dns, remote_command,
)

# Every id the shipped specialization registry declares; a worker answer or a record naming any other id is refused.
SPECIALIZATION_IDS = frozenset(record.id for record in native_specializations.SPECIALIZATIONS)
READINESS_TIMEOUT = 900
READINESS_INTERVAL = 5
# Each ingress host's read-only firewall check, all within one native call of the block's Go deadline.
FIREWALL_CALL = 120
FIREWALL_WINDOW = 600
# The rendered-configuration proof of a management gateway's oauth2-proxy: one call per host per round, rounds within one native call
# of the block's Go deadline.
SSO_CALL = 120
SSO_WINDOW = 600
SSO_PAUSE = 10
WORKER_CEILINGS = (640, 1920, 4285, 21600)
# The bootstrap call's own launcher ceiling and worker budget; the cluster block's Go deadline (cephDeadline) adds the call.
BOOTSTRAP_CEILING = 1920
BOOTSTRAP_BUDGET = 1800
BOOTSTRAP_CALL = BOOTSTRAP_BUDGET + 60
NATIVE_FAILURES = ("native-unknown", "native-refusal", "native-timeout")
# The produced-material output each action writes on apply and observe: its Go caller reads exactly that one.
OUTPUTS = {"client": "client", "cluster": "dashboard", "object-user": "object-user"}
UNIT_DAEMON = re.compile(r"[a-z][a-z0-9-]*\.[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\Z")
# The refusals each block's Go caller remedies by name (hostRefusals,
# adapterRefusals and smbRefusals in internal/storage/ceph); a test holds them equal.
HOST_REASONS = (
    "fips-policy",
    "unsupported-release", "image-pull", "image-platform", "image-profile", "route-probe", "device-identity", "runtime-profile", "not-ready",
    "package-resolution", "package-signature", "package-transaction", "request", "artifact", "ownership", "evidence", "material", "packages",
    "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "foreign-cluster", "native-command", "native-timeout", "native-output",
    "unknown", "transport", "remote-refusal", "adapter-exception", "ssh-policy-host-excluded", "ssh-policy-unavailable",
)
CLUSTER_REASONS = (
    "fips-policy", "cephx-policy",
    "unsupported-release", "runtime-profile", "device-identity", "cache-unproved", "enrollment-budget",
    "dashboard-identity", "dashboard-drift", "dashboard-policy",
    "dashboard-verify", "pool-identity", "pool-drift", "client-quiescence-unqualified", "native-option-undeclared", "call-home-unqualified",
    "dashboard-manager",
    "request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready",
    "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport",
    "remote-refusal", "adapter-exception", "foreign-client", "foreign-pool", "foreign-profile", "foreign-rule", "crush-capacity", "pg-budget",
    "foreign-rbd-image", "rbd-image-identity", "foreign-filesystem", "filesystem-identity", "filesystem-multiple-disabled", "foreign-subvolume-group",
    "mds-coverage", "foreign-subvolume", "subvolume-identity", "foreign-image", "nfs-module-disabled", "nfs-pool-foreign", "object-realm-foreign",
    "object-zone-pools", "object-endpoint-certificate", "object-user-foreign", "object-user-identity", "object-bucket-foreign",
    "object-bucket-identity", "object-bucket-s3", "nfs-export-foreign", "nfs-export-apply", "nfs-export-path-escape", "nfs-export-realm-default",
    "nvmeof-native", "nvmeof-identity", "nvmeof-foreign", "nvmeof-budget",
    "ingress-firewall", "service-credential-foreign", "ssh-policy-host-excluded", "ssh-policy-unavailable", "tunnel-host-key-excluded",
    "sso-unverified",
)
SMB_REASONS = (
    "request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command",
    "native-timeout", "native-output", "unknown", "foreign-cluster", "foreign-image", "foreign-service", "smb-identity", "smb-result",
    "smb-unavailable", "smb-directory", "smb-module-disabled", "ssh-policy-host-excluded", "ssh-policy-unavailable",
)


class ReadinessRefusal(ceph_native.Refusal):
    """Readiness unproved by its deadline, with the native health check codes that still blocked it."""

    def __init__(self, checks):
        super().__init__("not-ready")
        self.checks = list(checks)


class UnsettledRefusal(ceph_native.RemoteRefusal):
    """A host whose native layout cephadm kept moving through every bounded snapshot: not ready, never a contradiction."""

    def __init__(self, detail, host):
        super().__init__(detail)
        self.host = host


def unsettled(error):
    """Only a worker's exact not-ready refusal; every other remote refusal keeps its own meaning."""
    return isinstance(error, ceph_native.RemoteRefusal) and str(error.detail or "").partition("@")[0] == "native-refusal:not-ready"


def blocking_entry(value):
    """A blocking health check code, a daemon a host has not yet running, or a service unit's or gateway's unmet daemon count or endpoint."""
    return isinstance(value, str) and (ceph_native.HEALTH_CHECK.fullmatch(value) is not None or ceph_native.PENDING_DAEMON.fullmatch(value) is not None
                                       or ceph_native_units.PENDING.fullmatch(value) is not None or ceph_native_object.PENDING.fullmatch(value) is not None)


def readiness_detail(checks):
    """The blocking check codes, whole ones only, within the published detail's bound."""
    detail = "not-ready"
    for index, check in enumerate(checks):
        if len(detail) + 1 + len(check) > adapter_protocol.MAX_DETAIL:
            break
        detail += (":" if index == 0 else ".") + check
    return detail


def refusal(error, reasons):
    """The closed reason and non-secret detail of any exit; never a value.

    A native worker's refusal is named by the token its endpoint raised when
    the block names that token. Any other refusal is `unknown`, or
    `remote-refusal` from a worker, and its detail still carries the token.
    A readiness timeout's detail names the native health checks that blocked it.
    """
    if isinstance(error, ReadinessRefusal) and error.checks:
        return (str(error) if str(error) in reasons else "unknown"), readiness_detail(error.checks)
    if isinstance(error, ceph_native.RemoteRefusal):
        named = str(error.detail or "").split(":", 1)
        token = named[-1].partition("@")[0] if named[0] == "native-refusal" else named[0]
        return (token if token in reasons else "remote-refusal"), error.detail or None
    if isinstance(error, ceph_ssh_policy.PolicyRefusal) and error.detail:
        return (str(error) if str(error) in reasons else "unknown"), error.detail
    detail = ceph_native_worker.cause(error)
    if isinstance(error, ceph_native.Refusal):
        return (str(error) if str(error) in reasons else "unknown"), detail
    return "adapter-exception", detail


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


def user_output(path, value, request):
    """The produced key pair, exactly the retained canonical {accessKey, secretKey, uid} of this user, written to the protected output."""
    ceph_native.require(isinstance(value, str) and value.isascii(), "material")
    data = value.encode("ascii")
    ceph_native_object.user_material(data, request["objectUser"]["name"], reason="material")
    ceph_native.private_write(path, data)


def specialization_ids(value):
    """A sorted, duplicate-free list of 1 or more ids the shipped registry declares."""
    return (isinstance(value, list) and 0 < len(value) <= len(SPECIALIZATION_IDS)
            and all(isinstance(name, str) and name in SPECIALIZATION_IDS for name in value) and value == sorted(set(value)))


def recorded(observation, transport, request_digest, request):
    """The block's evidence naming the specialization ids this execution's native phases applied; absent when none."""
    applied = getattr(transport, "applied", None)
    if not applied:
        return observation
    return evidence(dict(observation, specializations=sorted(applied)), request_digest, request)


def evidence(value, request_digest, request=None):
    """The closed public projection, checked before any result publication."""
    if isinstance(value, dict) and value.get("version") == ceph_host_runtime.EVIDENCE_VERSION:
        return host_evidence(value, request_digest, request)
    fields = {"version", "request", "action", "fsid", "owner", "owned", "exists", "ready", "removed", "partial", "health", "hosts", "monitors", "monAddrs",
              "mgrActive", "mgrStandbys", "osds", "resource", "client", "dashboard", "endpoint", "daemons", "sso"}
    ceph_native.require(isinstance(value, dict) and set(value) in (fields, fields | {"specializations"}), "evidence")
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


def tls_certificate(address, port, timeout=10):
    """The DER certificate a TLS endpoint presents; trust is the caller's fingerprint comparison."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    with socket.create_connection((address, port), timeout=timeout) as raw:
        with context.wrap_socket(raw) as channel:
            return channel.getpeercert(binary_form=True)


def dashboard_reachable(request, observation, connect=tls_certificate):
    """A controller-side TLS connect to the active MGR, proved by the stored certificate's SHA-256."""
    dashboard = observation["dashboard"]
    host = next((item for item in request["hosts"] if item["name"] == observation["mgrActive"]), None)
    if not dashboard["url"] or not dashboard["certificateSHA256"] or host is None:
        return False
    try:
        presented = connect(host["address"], ceph_native.DASHBOARD_PORT)
    except (OSError, ValueError):
        return False
    return isinstance(presented, bytes) and 0 < len(presented) <= 16384 and hashlib.sha256(presented).hexdigest() == dashboard["certificateSHA256"]


def dashboard_output(path, value):
    """The password reaches only the runner's protected output file."""
    ceph_native.require(isinstance(value, str) and 0 < len(value.encode()) <= 128, "material")
    ceph_native.require(ceph_native.dashboard_policy(value), "material")
    ceph_native.private_write(path, value.encode())


def bootstrap_launcher(supervisor_source, host_source):
    """The long host launcher's fixed code, admitting only the bootstrap phase under the bootstrap ceiling."""
    return (ceph_native_worker.launcher(supervisor_source)
            + "f={'__name__':'fixed_ceph_host'};exec(compile(" + repr(host_source) + ",'<fixed-ceph-host>','exec'),f);"
            "f.update(REMOTE_LIMIT=" + str(BOOTSTRAP_CEILING) + ",PHASE='bootstrap');f['main'](s)")


def ssh_argv(host, key, known_hosts, source, worker_budget=None, *, worker_ceiling=640, ssh_config, host_key_algorithms):
    """SSH remote execution requires one string; quote only fixed worker code."""
    argv = ceph_ssh_policy.session_argv(ssh_config, known_hosts, host_key_algorithms, key, host["port"])
    arguments = ()
    if worker_budget is not None:
        ceph_native.require(worker_ceiling in WORKER_CEILINGS and type(worker_budget) in (int, float) and 0 < worker_budget <= worker_ceiling, "transport")
        arguments = (str(worker_budget),)
    argv.extend(["--", host["user"] + "@" + host["address"], remote_command.fixed_worker(source, *arguments)])
    return argv


class Transport:
    """No material value appears in arguments, environment, errors or logs."""

    def __init__(self, request, material, run=ceph_native.bounded_command, *, scope=False, smb_secrets=0, policy=None):
        self.request = ceph_native.validate_scope(request) if scope else ceph_native.validate(request)
        ceph_native.require(type(smb_secrets) is int and 0 <= smb_secrets <= 10000, "material")
        self.material = material
        self.run = run
        self.smb_secrets = smb_secrets
        self.policy = policy
        self.preflighted = False
        self.applied = set()
        self.applied_lock = threading.Lock()

    def ssh_policy(self):
        """The controller's SSH crypto policy, evaluated once on first use; a block that opens no session never evaluates it."""
        if self.policy is None:
            self.policy = ceph_ssh_policy.controller_policy()
        return self.policy

    def preflight(self, known):
        """Before the first session: every request host the frozen known_hosts pins has an algorithm the policy admits."""
        if self.preflighted:
            return
        for host in self.request["hosts"]:
            if ceph_ssh_policy.pinned(known, host):
                ceph_ssh_policy.host_key_algorithms(self.ssh_policy(), known, host)
        self.preflighted = True

    def prepare_round(self):
        ceph_native.require(isinstance(self.material, dict) and "knownHosts" in self.material, "material")
        self.ssh_policy()
        self.preflight(ceph_native.read_regular(self.material["knownHosts"], private=True))

    def _call(self, host, source, payload, timeout, worker_budget=None, *, worker_ceiling=640, failures=NATIVE_FAILURES):
        ceph_native.require(isinstance(self.material, dict), "material")
        package_material = {"packageCA" + str(i) for i in range(len(self.request.get("packages", {}).get("sources", [])))}
        allowed = {"sshKey", "knownHosts", "clusterPrivateKey", "clusterPublicKey", "registryJSON", "gatewayCertificate", "gatewayKey"}
        allowed |= ceph_native_units.MATERIAL_VARIABLES
        allowed |= {"smbSecret" + str(index) for index in range(self.smb_secrets)}
        allowed |= {"registryCA"} if "registryTrustBundleRef" in self.request else set()
        allowed |= {"packageRepositoryCA"} if "packageRepositoryTrustBundleRef" in self.request.get("packages", {}) else set()
        ceph_native.require(set(self.material) <= allowed | package_material, "material")
        ceph_native.require(set(self.material) >= {"sshKey", "knownHosts"}, "material")
        known_bytes = ceph_native.read_regular(self.material["knownHosts"], private=True)
        self.preflight(known_bytes)
        algorithms = ceph_ssh_policy.host_key_algorithms(self.ssh_policy(), known_bytes, host)
        with tempfile.TemporaryDirectory(prefix="bootwright-ceph-") as scratch:
            key = os.path.join(scratch, "identity")
            known = os.path.join(scratch, "known_hosts")
            config = os.path.join(scratch, "ssh_config")
            ceph_native.private_write(key, ceph_native.read_regular(self.material["sshKey"], private=True))
            ceph_native.private_write(known, known_bytes)
            ceph_native.private_write(config, self.ssh_policy()["configuration"].encode())
            argv = ssh_argv(host, key, known, source, worker_budget, worker_ceiling=worker_ceiling, ssh_config=config, host_key_algorithms=algorithms)
            sources = dict(payload.get("sources", {})) if isinstance(payload, dict) else {}
            private = payload.get("payload", {}).get("private", {}) if isinstance(payload, dict) else {}
            private = native_diagnostics.private_values(private)
            private.extend(ceph_native.read_regular(path, private=True) for path in self.material.values())
            payload = ceph_native.canonical(payload)
            ceph_native.require(len(payload) <= ceph_native.MAX_INPUT, "material")
            code, output = self.run(argv, data=payload, timeout=timeout)
        ceph_native.require(code == 0, "transport")
        try:
            result = json.loads(output)
        except (ValueError, UnicodeError):
            raise ceph_native.Refusal("transport") from None
        ceph_native.require(isinstance(result, dict) and type(result.get("ok")) is bool, "transport")
        if not result["ok"]:
            # Native reasons are not arbitrary strings to be reflected: only a
            # closed reason and a cause the shipped sources attest travel.
            ceph_native.require({"ok", "reason"} <= set(result) <= {"ok", "reason", "cause", "nativeError"}
                                and result["reason"] in failures, "transport")
            shipped = dict(sources, ceph_native_worker=inspect.getsource(ceph_native_worker))
            ceph_native.require("cause" not in result or ceph_native_worker.attested(result["cause"], shipped), "transport")
            error = ceph_native.RemoteRefusal(result["reason"] + (":" + result["cause"] if "cause" in result else ""))
            if "nativeError" in result:
                ceph_native.require(native_diagnostics.valid(result["nativeError"]), "transport")
                value = result["nativeError"]
                error.native_error = native_diagnostics.error_detail(value["coordinate"], value["cause"], secrets=private)
            raise error
        ceph_native.require(set(result) in ({"ok", "result"}, {"ok", "result", "specializations"}) and isinstance(result["result"], dict), "transport")
        if "specializations" in result:
            ceph_native.require(specialization_ids(result["specializations"]), "transport")
            with self.applied_lock:
                self.applied.update(result["specializations"])
        return result["result"]

    @staticmethod
    def native_sources():
        modules = (native_diagnostics, native_specializations, canonical_json, bounded_process,
                   ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
                   ceph_native_resources, ceph_native_nfs, ceph_native_nvmeof, ceph_native_observability,
                   ceph_native_units, ceph_native_object, ceph_smb_native,
                   ceph_replication_native, ceph_export_dns, ceph_export_native,
                   ceph_host_route_probe, ceph_pull_broker, physical_machine_identity, ceph_host_runtime)
        sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}
        ceph_native.require(tuple(sources) == ceph_native_worker.MODULES, "transport")
        return sources

    def call(self, host, phase, private=None, *, timeout=660, devices=None, service=None):
        nvme_long = (self.request.get("action") == "nvmeof" and phase in ("nvmeof", "nvmeof_observe")
                     or nvmeof_unit(self.request) and phase == "nvmeof_unit_observe")
        ceiling = ceph_native_nvmeof.phase_budget(self.request, phase) if nvme_long else 640
        ceph_native.require(type(timeout) in (int, float) and 0 < timeout <= ceiling + 20 and ceiling <= 21600, "transport")
        ceph_native.require(phase != "profile" and (devices is None or phase == "services"), "transport")
        ceph_native.require(service is None or phase == "services" and type(service) is int and 0 <= service < len(self.request["services"]), "transport")
        budget = min(ceiling, timeout - 2) if timeout > 2 else timeout / 2
        payload = {"request": self.request, "host": host["name"], "phase": phase, "private": private or {}}
        if devices is not None:
            payload["devices"] = devices
        if service is not None:
            payload["service"] = service
        if phase == "bootstrap":
            # The cluster's one long call: cephadm restarts its manager several times before bootstrap returns.
            source = bootstrap_launcher(inspect.getsource(ceph_native_worker), inspect.getsource(ceph_host_worker))
            return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": BOOTSTRAP_BUDGET}, BOOTSTRAP_CALL,
                              worker_budget=BOOTSTRAP_BUDGET, worker_ceiling=BOOTSTRAP_CEILING)
        if nvme_long:
            source = (ceph_native_worker.launcher(inspect.getsource(ceph_native_worker))
                      + "f={'__name__':'fixed_ceph_nvmeof'};exec(compile(" + repr(inspect.getsource(ceph_host_worker))
                      + ",'<fixed-ceph-nvmeof>','exec'),f);f.update(REMOTE_LIMIT=21600"
                      + ",PHASE=" + repr(phase) + ");f['main'](s)")
            return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout,
                              worker_budget=budget, worker_ceiling=21600)
        source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker)) + "s['main']()"
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout,
                          worker_budget=budget)

    def export(self, host, phase, request, private=None, *, timeout=600):
        """One bounded export phase; only private stdin and a private reply carry keys."""
        ceph_native.require(phase in ceph_export_native.PHASES and 0 < timeout <= 660, "transport")
        budget = min(640, timeout - 2)
        payload = {"request": request, "host": host["name"], "phase": phase, "private": private or {}}
        source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker)) + "s['main']()"
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout, worker_budget=budget)

    def smb(self, host, phase, request, private=None, *, timeout=660):
        """One SMB phase on the bootstrap host; the bound passwords travel only in the worker's stdin payload."""
        ceph_native.require(type(timeout) in (int, float) and 0 < timeout <= 660, "transport")
        ceph_native.require(phase in ceph_smb_native.PHASES, "transport")
        budget = min(640, timeout - 2) if timeout > 2 else timeout / 2
        payload = {"request": request, "host": host["name"], "phase": phase, "private": private or {}}
        source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker)) + "s['main']()"
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout,
                          worker_budget=budget)

    def replication(self, host, phase, request, side, index=-1, private=None, *, timeout=660):
        """A fixed replication phase; bootstrap credentials exist only in this bounded worker payload."""
        ceph_native.require(phase in ceph_replication_native.PHASES and 0 < timeout <= 660, "transport")
        budget = min(640, timeout - 2) if timeout > 2 else timeout / 2
        payload = {"request": request, "host": host["name"], "phase": phase, "side": side, "image": index, "private": private or {}}
        source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker)) + "s['main']()"
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout, worker_budget=budget)

    def smb_material(self):
        """Every bound SMB password, read from the runner's private files: one line of 1 to 1024 bytes of UTF-8 each."""
        result = {}
        for name in ("smbSecret" + str(index) for index in range(self.smb_secrets)):
            ceph_native.require(isinstance(self.material, dict) and name in self.material, "material")
            data = ceph_native.read_regular(self.material[name], private=True)
            try:
                value = data.decode("utf-8")
            except UnicodeError:
                raise ceph_native.Refusal("material") from None
            ceph_native.require(1 <= len(data) <= ceph_smb_native.MAX_SECRET and not any(char in value for char in "\x00\r\n"), "material")
            result[name] = value
        return result

    def gateway_material(self, key):
        """A gateway's bound certificate, and its private key for the service apply, read from the runner's private files."""
        names = ("gatewayCertificate", "gatewayKey") if key else ("gatewayCertificate",)
        ceph_native.require(isinstance(self.material, dict) and all(name in self.material for name in names), "material")
        try:
            return {name: ceph_native.read_regular(self.material[name], private=True).decode("utf-8") for name in names}
        except UnicodeError:
            raise ceph_native.Refusal("material") from None

    def unit_material(self, phase="unit"):
        """A service unit's bound leaves that a phase holds, of those its bindings name, read from the runner's private files."""
        names = sorted(ceph_native_units.material_names(self.request, phase))
        ceph_native.require(isinstance(self.material, dict) and all(name in self.material for name in names), "material")
        try:
            return {name: ceph_native.read_regular(self.material[name], private=True).decode("utf-8") for name in names}
        except UnicodeError:
            raise ceph_native.Refusal("material") from None

    @staticmethod
    def trust_bundle(text):
        """The certificate blocks resolution verified, proven as the host broker will load them, before any host effect."""
        data = text.encode("utf-8")
        ceph_native.require(0 < len(data) <= ceph_pull_broker.MAX_TRUST, "material")
        try:
            projection = "".join(ceph_registry_discovery.bundle_blocks(data))
            ceph_pull_broker.bundle_context(projection)
        except (ceph_registry_discovery.DiscoveryRefusal, ceph_pull_broker.BrokerError):
            raise ceph_native.Refusal("material") from None
        return projection

    def host_profile(self, host):
        """The one long call: pull the frozen digest, then prove its contained runtime."""
        names = ("registryJSON", "registryCA") if "registryTrustBundleRef" in self.request else ("registryJSON",)
        ceph_native.require(all(name in self.material for name in names), "material")
        try:
            private = {name: ceph_native.read_regular(self.material[name], private=True).decode("utf-8") for name in names}
        except UnicodeError:
            raise ceph_native.Refusal("material") from None
        if "registryCA" in private:
            private["registryCA"] = self.trust_bundle(private["registryCA"])
        payload = {"request": self.request, "host": host["name"], "phase": "profile", "private": private}
        source = ceph_host_worker.launcher(inspect.getsource(ceph_native_worker), inspect.getsource(ceph_host_worker))
        budget = ceph_host_worker.REMOTE_LIMIT
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, budget + 60,
                          worker_budget=budget, worker_ceiling=budget)

    def packages(self, host):
        ceph_package_sources.source_for(self.request, host["name"])
        index = next(i for i, source in enumerate(self.request["packages"]["sources"]) if source["host"] == host["name"])
        material = "packageCA" + str(index)
        ceph_native.require(material in self.material, "material")
        certificate = ceph_native.read_regular(self.material[material], private=True).decode("utf-8")
        private = {"certificate": certificate}
        if "packageRepositoryTrustBundleRef" in self.request["packages"]:
            ceph_native.require("packageRepositoryCA" in self.material, "material")
            try:
                bundle = ceph_native.read_regular(self.material["packageRepositoryCA"], private=True).decode("utf-8")
            except UnicodeError:
                raise ceph_native.Refusal("material") from None
            private["packageRepositoryCA"] = self.trust_bundle(bundle)
        modules = (native_diagnostics, canonical_json, bounded_process, ceph_native_worker,
                   ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
                   native_resolution, native_signatures, native_apply, ceph_package_sources, ceph_crypto_policy, ceph_packages)
        sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}
        result = self._call(host, inspect.getsource(ceph_package_worker),
                            {"sources": sources, "payload": {"request": self.request, "host": host["name"], "private": private}}, 1980,
                            failures=ceph_package_worker.REASONS)
        ceph_native.require(set(result) == {"changed", "ready", "planDigest"} and type(result["changed"]) is bool and result["ready"] is True
                            and ceph_native.digest(result["planDigest"]), "packages")
        return result

    def bootstrap_material(self):
        result = {}
        for name in ("clusterPrivateKey", "clusterPublicKey", "registryJSON", "knownHosts"):
            ceph_native.require(name in self.material, "material")
            try:
                result[name] = ceph_native.read_regular(self.material[name], private=True).decode("utf-8")
            except UnicodeError:
                raise ceph_native.Refusal("material") from None
        return result


def observe_state(request, request_digest, bootstrap, transport, deadline=None, now=time.monotonic, blocking=None):
    """One validated snapshot; an optional deadline bounds all of its probes, and an optional list receives the health check
    codes that kept readiness unproved."""
    def read(host, phase):
        try:
            if deadline is None:
                return transport.call(host, phase)
            started = now()
            remaining = deadline - started
            ceph_native.require(remaining > 0, "not-ready")
            budget = min(660, remaining)
            result = transport.call(host, phase, timeout=budget)
        except ceph_native.RemoteRefusal as error:
            if unsettled(error):
                raise UnsettledRefusal(error.detail, host["name"]) from None
            raise
        ceph_native.require(now() < min(deadline, started + budget), "not-ready")
        return result

    observed = read(bootstrap, "observe")
    ceph_native.require(isinstance(observed, dict), "evidence")
    observed = dict(observed)
    checks = observed.pop("blocking", [])
    ceph_native.require(isinstance(checks, list) and len(checks) <= 16 and all(blocking_entry(check) for check in checks), "evidence")
    if blocking is not None:
        blocking[:] = checks
    observed["request"] = request_digest
    evidence(observed, request_digest, request)
    host_states, pending = [], []
    for host in request["hosts"]:
        state = read(host, "host_state")
        ceph_native.require(isinstance(state, dict), "evidence")
        state = dict(state)
        waiting = state.pop("pending", None)
        ceph_native.require(waiting is None or isinstance(waiting, list) and 0 < len(waiting) <= 16
                            and all(isinstance(name, str) and ceph_native.PENDING_DAEMON.fullmatch(name) is not None for name in waiting), "evidence")
        ceph_native.require(set(state) == {"name", "owned", "exists"} and state["name"] == host["name"]
                            and type(state["owned"]) is bool and type(state["exists"]) is bool, "evidence")
        host_states.append(state)
        pending += [name for name in waiting or [] if name not in checks + pending]
    if request["action"] == "cluster":
        if any(state["exists"] for state in host_states):
            observed["exists"] = True
            observed["removed"] = False
            observed["owned"] = observed["owned"] and all(state["owned"] for state in host_states)
            observed["ready"] = observed["ready"] and all(state["exists"] for state in host_states)
            observed["partial"] = not observed["ready"]
        else:
            observed["owned"] = any(state["owned"] for state in host_states)
            observed["exists"] = observed["owned"]
            observed["partial"] = observed["owned"]
            observed["removed"] = False
            observed["hosts"] = [host["name"] for host in request["hosts"]]
    elif not observed["exists"] and not observed["hosts"]:
        ceph_native.require(not any(state["exists"] for state in host_states), "cluster-unavailable")
        observed["owned"] = observed["owned"] and all(state["owned"] for state in host_states)
        observed["exists"] = observed["owned"]
        observed["partial"] = observed["owned"]
        observed["removed"] = False
        observed["hosts"] = [host["name"] for host in request["hosts"]]
    if pending:
        # A manager container a host is restarting is unfinished, never ready, and named beside the blocking checks.
        if blocking is not None:
            blocking[:] = (pending + checks)[:16]
        observed["ready"] = False
        observed["partial"] = observed["exists"]
    return evidence(observed, request_digest, request)


def wait_ready(request, request_digest, bootstrap, transport, now, pause):
    """Wait only on proved partial state, without repeating any native effect; a timeout names the health checks still blocking.

    A host whose snapshot never settled while cephadm converges is not ready yet: the wait polls again and names it.
    """
    deadline = now() + READINESS_TIMEOUT
    blocking = []
    while True:
        try:
            observed = observe_state(request, request_digest, bootstrap, transport, deadline, now, blocking)
        except UnsettledRefusal as error:
            blocking[:] = (["unsettled@" + error.host] + [entry for entry in blocking if not entry.startswith("unsettled@")])[:16]
            observed = None
        except ceph_native.Refusal as error:
            if type(error) is ceph_native.Refusal and str(error) == "not-ready":
                raise ReadinessRefusal(blocking) from None
            raise
        if observed is not None and observed["ready"]:
            return observed
        ceph_native.require(observed is None or observed["owned"] and observed["exists"] and observed["partial"], "not-ready")
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))


def recovery_preparations(request, values):
    """Every host answers in the existing prerequisite round trip."""
    stages = {}
    for host, value in zip(request["hosts"], values):
        ceph_native.require(isinstance(value, dict) and set(value) == {"name", "owned", "exists", "recovery"}, "evidence")
        ceph_native.require(value["name"] == host["name"] and type(value["owned"]) is bool and type(value["exists"]) is bool, "evidence")
        stage = ceph_tunnel_staging.prove_recovery_record(request, host["name"], value["recovery"])
        ceph_native.require(value["owned"] == (stage != "unclaimed") and value["exists"] == (stage == "complete"), "evidence")
        stages[host["name"]] = stage
    ceph_native.require(len(stages) == len(request["hosts"]), "evidence")
    return stages


def host_round(transport, hosts, phase, private=None):
    """Join the whole bounded round before returning the first refusal in host order."""
    if not hosts:
        return []
    prepare = getattr(transport, "prepare_round", None)
    if prepare is not None:
        prepare()
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(hosts)) as executor:
        calls = [executor.submit(transport.call, host, phase, private) for host in hosts]
        concurrent.futures.wait(calls)
        return [call.result() for call in calls]


def enroll_hosts(request, bootstrap, transport):
    changed = False
    for unused in request["hosts"]:
        result = transport.call(bootstrap, "enroll")
        ceph_native.require(isinstance(result, dict) and {"changed"} <= set(result) <= {"changed", "remaining"}
                            and type(result["changed"]) is bool and type(result.get("remaining", False)) is bool, "evidence")
        changed = result["changed"] or changed
        if not result.get("remaining", False):
            return changed
    raise ceph_native.Refusal("enrollment-budget")


def apply_tunnel_cluster(request, bootstrap, transport, state, preparations):
    """Resume only exact owned native transitions with their retained intents."""
    stage = ceph_tunnel_staging.prove_recovery_record(request, bootstrap["name"], state, evidence=True)
    ceph_native.require(stage != "complete", "partial-cluster")
    stages = recovery_preparations(request, preparations)
    fresh = stage in ("unclaimed", "claimed", "prepared")
    if fresh:
        ceph_native.require(all(value in ("unclaimed", "claimed", "prepared") for value in stages.values()), "partial-cluster")
    else:
        ceph_native.require(stages[bootstrap["name"]] == "complete"
                            and all(stages[host["name"]] == "prepared" for host in request["hosts"] if host != bootstrap), "partial-cluster")
    host_round(transport, request["hosts"], "devices")
    changed = False
    private = transport.bootstrap_material()
    managers = next(service["placement"]["hosts"] for service in request["services"] if service["service_type"] == "mgr")
    host_round(transport, [host for host in request["hosts"] if host["name"] in managers
                           and stages[host["name"]] in ("unclaimed", "claimed")], "tunnel_preflight", {"knownHosts": private["knownHosts"]})
    claimed = host_round(transport, [host for host in request["hosts"] if stages[host["name"]] == "unclaimed"], "claim")
    changed = any(value["changed"] for value in claimed) or changed
    authorization = {name: private[name] for name in ("clusterPublicKey", "registryJSON", "clusterPrivateKey", "knownHosts")}
    authorized = host_round(transport, [host for host in request["hosts"] if stages[host["name"]] in ("unclaimed", "claimed")],
                            "authorize", authorization)
    changed = any(value["changed"] for value in authorized) or changed
    ssh = {name: private[name] for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")}
    if fresh:
        changed = transport.call(bootstrap, "bootstrap", ssh)["changed"] or changed
    if fresh or stage == "seed":
        changed = transport.call(bootstrap, "tunnel_configure", ssh)["changed"] or changed
    if stage not in ("mounted", "enrolling"):
        changed = transport.call(bootstrap, "tunnel_mount")["changed"] or changed
    changed = transport.call(bootstrap, "cache")["changed"] or changed
    cached = host_round(transport, [host for host in request["hosts"] if host != bootstrap], "cache")
    changed = any(value["changed"] for value in cached) or changed
    changed = enroll_hosts(request, bootstrap, transport) or changed
    changed = transport.call(bootstrap, "license")["changed"] or changed
    host_round(transport, request["hosts"], "devices")
    devices = device_identities(request, transport, concurrent=True)
    names = {name for service in request["services"] if service["service_type"] == "osd" for name in service["placement"]["hosts"]}
    prepared = host_round(transport, [host for host in request["hosts"] if host["name"] in names], "osd_prepare")
    changed = any(value["changed"] for value in prepared) or changed
    # Every pending service document shares one owned apply and readback. The
    # native worker journals each child independently before sending stdin.
    changed = transport.call(bootstrap, "services", devices=devices)["changed"] or changed
    return changed


def device_identities(request, transport, *, concurrent=False):
    """Every host's retained SDK identities, in declared host and device order."""
    result = []
    values = (host_round(transport, request["hosts"], "host_devices") if concurrent
              else [transport.call(host, "host_devices") for host in request["hosts"]])
    for host, value in zip(request["hosts"], values):
        ceph_native.require(isinstance(value, dict) and set(value) == {"devices"}, "device-identity")
        result.extend(ceph_host_runtime.rows(request, host["name"], value["devices"]))
    return result


def apply_filesystem(request, bootstrap, transport, now, pause):
    """Create the filesystem; its subvolume groups only once every active rank is held, which the volumes module needs to mount it."""
    changed = transport.call(bootstrap, "filesystem")["changed"]
    if not request["filesystem"]["groups"]:
        return changed
    deadline = now() + READINESS_TIMEOUT
    while True:
        ranks = transport.call(bootstrap, "filesystem_ranks")
        ceph_native.require(isinstance(ranks, dict) and set(ranks) == {"active"} and type(ranks["active"]) is bool, "evidence")
        if ranks["active"]:
            break
        remaining = deadline - now()
        ceph_native.require(remaining > 0, "not-ready")
        pause(min(READINESS_INTERVAL, remaining))
    return transport.call(bootstrap, "filesystem_groups")["changed"] or changed


def execute(request, operation, request_digest, transport, outputs=None, *, now=time.monotonic, pause=time.sleep, connect=tls_certificate):
    """Keep read-only observation distinct from any preparation or mutation."""
    ceph_native.validate(request)
    ceph_native.require(operation in ("apply", "observe", "destroy"))
    ceph_native.require(ceph_native.digest(request_digest))
    if operation != "observe":
        ceph_native.require(ceph_native.supported_transport(request), "unsupported-release")
    bootstrap = next(h for h in request["hosts"] if h["name"] == request["bootstrap"])
    outputs = outputs or {}
    ceph_native.require(set(outputs) <= set(OUTPUTS.values()))
    if request["action"] in OUTPUTS and operation in ("apply", "observe"):
        ceph_native.require(set(outputs) == {OUTPUTS[request["action"]]}, "material")
    elif operation in ("apply", "observe"):
        ceph_native.require(not outputs, "material")
    if request["action"] == "service":
        return execute_unit(request, operation, request_digest, transport, bootstrap, now=now, pause=pause)
    if request["action"] == "nvmeof":
        return execute_nvmeof(request, operation, request_digest, transport, bootstrap, now=now, pause=pause)
    if request["action"] == "nfs-export":
        return execute_unit(request, operation, request_digest, transport, bootstrap, now=now, pause=pause, phases=ceph_native_nfs.EXPORT_APPLY_PHASES)
    if request["action"] == "gateway":
        return execute_gateway(request, operation, request_digest, transport, bootstrap, now=now, pause=pause)
    if request["action"] == "object-user":
        return execute_user(request, operation, request_digest, transport, bootstrap, outputs, now=now, pause=pause)
    if request["action"] == "object-bucket":
        return execute_bucket(request, operation, request_digest, transport, bootstrap, now=now, pause=pause)
    changed = False
    if operation == "apply":
        preparations = (host_round(transport, request["hosts"], "prerequisites") if request["action"] == "cluster"
                        else [transport.call(host, "prerequisites") for host in request["hosts"]])
        tunnel = request["action"] == "cluster" and request["clusterSSHTransport"] == "verified-tunnel"
        state = transport.call(bootstrap, "bootstrap_state") if tunnel else None
        if tunnel:
            stage = ceph_tunnel_staging.prove_recovery_record(request, bootstrap["name"], state, evidence=True)
            host_stages = recovery_preparations(request, preparations)
            before = state["evidence"]
            ceph_native.require(isinstance(before, dict), "evidence")
            before = dict(before, request=request_digest)
            evidence(before, request_digest, request)
            ceph_native.require(before["owned"] == (stage != "unclaimed") and before["exists"] == (stage != "unclaimed"), "evidence")
            ceph_native.require(before["ready"] == (stage == "complete") and before["partial"] == (stage not in ("unclaimed", "complete")), "evidence")
            if stage == "complete":
                ceph_native.require(all(value == "complete" for value in host_stages.values()), "partial-cluster")
        else:
            before = transport.call(bootstrap, "observe")
        if not before["ready"]:
            if request["action"] == "cluster":
                if tunnel:
                    changed = apply_tunnel_cluster(request, bootstrap, transport, state, preparations) or changed
                else:
                    # Devices precede ownership; an existing native default path
                    # retains its original partial-cluster refusal.
                    ceph_native.require(not before["exists"], "partial-cluster")
                    for host in request["hosts"]:
                        transport.call(host, "devices")
                    for host in request["hosts"]:
                        changed = transport.call(host, "claim")["changed"] or changed
                    private = transport.bootstrap_material()
                    for host in request["hosts"]:
                        authorization = {name: private[name] for name in ("clusterPublicKey", "registryJSON")}
                        changed = transport.call(host, "authorize", authorization)["changed"] or changed
                    ssh_material = {name: private[name] for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")}
                    changed = transport.call(bootstrap, "bootstrap", ssh_material)["changed"] or changed
                    changed = transport.call(bootstrap, "enroll")["changed"] or changed
                    changed = transport.call(bootstrap, "license")["changed"] or changed
                    for host in request["hosts"]:
                        transport.call(host, "devices")
                    changed = transport.call(bootstrap, "services")["changed"] or changed
            elif request["action"] in ("pool", "policy", "rbd", "subvolume", "settings"):
                changed = transport.call(bootstrap, request["action"])["changed"] or changed
            elif request["action"] == "filesystem":
                changed = apply_filesystem(request, bootstrap, transport, now, pause) or changed
        if request["action"] == "client":
            produced = transport.call(bootstrap, "client")
            changed = produced["changed"] or changed
            value = produced.get("private")
            ceph_native.require(isinstance(value, str) and 0 < len(value.encode()) <= 65536, "material")
            ceph_native.private_write(outputs["client"], value.encode())
    elif operation == "destroy":
        changed = transport.call(bootstrap, "destroy")["changed"]
    observation = (wait_ready(request, request_digest, bootstrap, transport, now, pause) if operation == "apply"
                   else observe_state(request, request_digest, bootstrap, transport))
    # Read-only observations may return partial evidence. Mutation completion
    # is not claimed until the actual desired postcondition is observed.
    if operation == "apply":
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy":
        ceph_native.require(observation["removed"], "not-removed")
    if operation == "observe" and request["action"] == "client" and observation["ready"]:
        value = transport.call(bootstrap, "client_read").get("private")
        ceph_native.require(isinstance(value, str) and 0 < len(value.encode()) <= 65536, "material")
        ceph_native.private_write(outputs["client"], value.encode())
    if request["action"] == "cluster" and operation in ("apply", "observe") and observation["ready"]:
        observation, effect = dashboard_phase(request, operation, request_digest, bootstrap, transport, observation, outputs["dashboard"],
                                              now=now, pause=pause, connect=connect)
        changed = effect or changed
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def nvmeof_unit(request):
    return request.get("action") == "service" and request.get("service", {}).get("type") == "nvmeof"


def unit_observation_timeout(request):
    return ceph_native_nvmeof.phase_budget(request, "nvmeof_unit_observe") + 20 if nvmeof_unit(request) else 660


def observe_unit(request, request_digest, bootstrap, transport, timeout=None, blocking=None, *, private=None):
    """One read-only observation of a service unit on the bootstrap host; no other host is called."""
    if timeout is None and nvmeof_unit(request):
        timeout = unit_observation_timeout(request)
    options = {} if timeout is None else {"timeout": timeout}
    if private:
        options["private"] = private
    observed = transport.call(bootstrap, "nvmeof_unit_observe" if nvmeof_unit(request) else "observe", **options)
    ceph_native.require(isinstance(observed, dict), "evidence")
    observed = dict(observed)
    checks = observed.pop("blocking", [])
    ceph_native.require(isinstance(checks, list) and len(checks) <= 16 and all(blocking_entry(check) for check in checks), "evidence")
    if blocking is not None:
        blocking[:] = checks
    observed["request"] = request_digest
    return evidence(observed, request_digest, request)


def wait_unit(request, request_digest, bootstrap, transport, now, pause, *, private=None):
    """Poll the unit's own observation until ready; a timeout names the daemons, endpoint or health checks still blocking."""
    call_timeout = unit_observation_timeout(request)
    deadline = now() + READINESS_TIMEOUT + (call_timeout if nvmeof_unit(request) else 0)
    blocking = []
    while True:
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        try:
            observed = observe_unit(request, request_digest, bootstrap, transport, min(call_timeout, remaining), blocking, private=private)
        except ceph_native.RemoteRefusal as error:
            if not unsettled(error):
                raise
            blocking[:] = (["unsettled@" + bootstrap["name"]] + [entry for entry in blocking if not entry.startswith("unsettled@")])[:16]
            observed = None
        if observed is not None and observed["ready"]:
            return observed
        ceph_native.require(observed is None or observed["owned"] and observed["exists"] and observed["partial"], "not-ready")
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))


def ingress_firewall(request, transport, found, now):
    """Before the first effect of an apply: each host of the ingress proves, read-only, that an active firewall passes VRRP and the client
    port. One native call of the block's Go deadline covers every host's check."""
    if found is None:
        return
    hosts = {host["name"]: host for host in request["hosts"]}
    deadline = now() + FIREWALL_WINDOW
    for name in found[0]["placement"]["hosts"]:
        remaining = deadline - now()
        ceph_native.require(remaining > 0, "not-ready")
        result = transport.call(hosts[name], "ingress_firewall", timeout=min(FIREWALL_CALL, remaining))
        ceph_native.require(result == {"changed": False}, "evidence")


def sso_round(proxy, hosts, held, transport, deadline, now):
    """One read-only proof round over the proxy's hosts: the running proxy containers counted, and whether each one proved."""
    daemons, proved = 0, True
    for name in proxy["placement"]["hosts"]:
        remaining = deadline - now()
        ceph_native.require(remaining > 0, "not-ready")
        result = transport.call(hosts[name], "sso_config", held, timeout=min(SSO_CALL, remaining))
        ceph_native.require(isinstance(result, dict) and set(result) == {"changed", "daemons", "proved"} and result["changed"] is False
                            and type(result["daemons"]) is int and result["daemons"] >= 0 and type(result["proved"]) is bool, "evidence")
        daemons += result["daemons"]
        proved = proved and result["proved"]
    return daemons, proved


def sso_proof(request, transport, now, pause):
    """After the unit's apply, before the dashboard's single sign-on is enabled: every proxy host proves, read-only, that each running
    oauth2-proxy container of this cluster verifies its identity provider's certificate and mounts the bound CA bundle. Rounds repeat
    until the placed count runs proved; one native call of the block's Go deadline covers the window, which the proof returns."""
    proxy = ceph_native_observability.gateway_documents(request["service"])[2]
    hosts = {host["name"]: host for host in request["hosts"]}
    held = transport.unit_material("sso_config")
    deadline = now() + SSO_WINDOW
    while True:
        daemons, proved = sso_round(proxy, hosts, held, transport, deadline, now)
        if proved and daemons == ceph_native_observability.placed_count(proxy):
            return deadline
        remaining = deadline - now()
        ceph_native.require(remaining > 0, "not-ready")
        pause(min(SSO_PAUSE, remaining))


def sso_enable(bootstrap, transport, deadline, now, pause):
    """The one single sign-on write: calls 10 s apart while the orchestrator does not yet list a running oauth2-proxy, which the
    dashboard needs before it enables, each begun within the proof's window, so the block's one call beyond that window bounds the
    last; whether the write changed anything."""
    while True:
        produced = transport.call(bootstrap, "sso")
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "pending"} and type(produced["changed"]) is bool
                            and type(produced["pending"]) is bool and not (produced["changed"] and produced["pending"]), "evidence")
        if not produced["pending"]:
            return produced["changed"]
        remaining = deadline - now()
        ceph_native.require(remaining > 0, "not-ready")
        pause(min(SSO_PAUSE, remaining))


def observe_nvmeof(request, request_digest, bootstrap, transport, timeout, now, blocking):
    deadline = now() + timeout
    observed = transport.call(bootstrap, "nvmeof_cluster", timeout=min(660, timeout))
    ceph_native.require(isinstance(observed, dict), "evidence")
    observed = dict(observed)
    checks = observed.pop("blocking", [])
    ceph_native.require(isinstance(checks, list) and len(checks) <= 16 and all(blocking_entry(check) for check in checks), "evidence")
    blocking[:] = checks
    remaining = deadline - now()
    ceph_native.require(remaining > 0, "not-ready")
    resource = transport.call(bootstrap, "nvmeof_observe", timeout=min(ceph_native_nvmeof.phase_budget(request, "nvmeof_observe") + 20, remaining))
    ceph_native.require(isinstance(resource, dict) and set(resource) == {"owned", "exists", "ready", "partial", "removed", "resource"}, "evidence")
    cluster_ready = observed["ready"]
    observed.update(resource)
    observed["ready"] = cluster_ready and resource["ready"]
    observed["partial"] = observed["exists"] and not observed["ready"]
    observed["request"] = request_digest
    return evidence(observed, request_digest, request)


def execute_nvmeof(request, operation, request_digest, transport, bootstrap, *, now, pause):
    ceph_native.require(ceph_native_nvmeof.execution_budget(request, "apply") <= 21600, "nvmeof-budget")
    changed = False
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        produced = transport.call(bootstrap, "nvmeof", timeout=ceph_native_nvmeof.phase_budget(request, "nvmeof") + 20)
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
    observation_budget = 660 + ceph_native_nvmeof.phase_budget(request, "nvmeof_observe") + 20
    deadline = now() + observation_budget + (READINESS_TIMEOUT if operation == "apply" else 0)
    blocking = []
    while True:
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        try:
            observation = observe_nvmeof(request, request_digest, bootstrap, transport, min(observation_budget, remaining), now, blocking)
        except ceph_native.RemoteRefusal as error:
            if operation != "apply" or not unsettled(error):
                raise
            observation = None
            blocking[:] = ["unsettled@" + bootstrap["name"]]
        if operation != "apply" or observation is not None and observation["ready"]:
            break
        ceph_native.require(observation is None or observation["owned"] and observation["exists"] and observation["partial"], "not-ready")
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))
    if operation == "destroy":
        ceph_native.require(observation["removed"], "client-quiescence-unqualified")
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def execute_unit(request, operation, request_digest, transport, bootstrap, *, now, pause, phases=("unit",)):
    """A service unit runs its apply phases only on the bootstrap host: its preparation proof, with an ingress the firewall check on each
    ingress host, its apply phases in order, with an oauth2-proxy the rendered-configuration proof on each proxy host and then the one
    single sign-on write, then its own readiness wait. Its apply phase holds every bound leaf; each observation and
    removal holds the observed ones, and a management gateway's served leaf must be the bound certificate's."""
    changed = False
    bound_unit = request["action"] == "service" and bool(request["service"].get("bindings"))
    seen = transport.unit_material("observe") if bound_unit else {}
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        if request["action"] == "service":
            ingress_firewall(request, transport, ceph_native_units.frontend(request), now)
        bound = transport.unit_material("unit") if bound_unit else {}
        for phase in phases:
            produced = transport.call(bootstrap, phase, bound) if bound and phase == "unit" else transport.call(bootstrap, phase)
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
            changed = produced["changed"] or changed
        if request["action"] == "service" and proxied(request):
            deadline = sso_proof(request, transport, now, pause)
            changed = sso_enable(bootstrap, transport, deadline, now, pause) or changed
        observation = wait_unit(request, request_digest, bootstrap, transport, now, pause, private=seen)
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy" and nvmeof_unit(request):
        observation = observe_unit(request, request_digest, bootstrap, transport)
        ceph_native.require(observation["removed"], "client-quiescence-unqualified")
    elif operation == "destroy":
        changed = (transport.call(bootstrap, "destroy", seen) if seen else transport.call(bootstrap, "destroy"))["changed"]
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
        ceph_native.require(observation["removed"], "not-removed")
    else:
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
    if "mgmtGatewayCertificate" in seen:
        bound = hashlib.sha256(ceph_native_observability.leaf(seen["mgmtGatewayCertificate"])).hexdigest()
        ceph_native.require(observation["endpoint"]["certificateSHA256"] in ("", bound), "evidence")
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def execute_user(request, operation, request_digest, transport, bootstrap, outputs, *, now, pause):
    """An object user runs on the bootstrap host only: its preparation proof and its one phase, whose key pair reaches the protected
    output before its readiness wait; an observation that proves it ready recaptures the retained pair read-only. Removal is the core
    refusal."""
    changed = False
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        produced = transport.call(bootstrap, "object_user")
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "private"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
        user_output(outputs["object-user"], produced["private"], request)
        observation = wait_unit(request, request_digest, bootstrap, transport, now, pause)
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy":
        changed = transport.call(bootstrap, "destroy")["changed"]
        observation = observe_unit(request, request_digest, bootstrap, transport)
        ceph_native.require(observation["removed"], "not-removed")
    else:
        observation = observe_unit(request, request_digest, bootstrap, transport)
        if observation["ready"]:
            produced = transport.call(bootstrap, "object_user_read")
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"private"}, "evidence")
            user_output(outputs["object-user"], produced["private"], request)
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def execute_bucket(request, operation, request_digest, transport, bootstrap, *, now, pause):
    """An object bucket runs on the bootstrap host only: its preparation proof and its one phase, then its readiness wait. With TLS at the
    backend the bound certificate, never a key, reaches every phase, which pins the daemon's leaf to it. Removal is the core refusal."""
    seen = transport.gateway_material(False) if request["objectBucket"]["certificateRef"] else None
    changed = False
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        produced = transport.call(bootstrap, "object_bucket", seen) if seen else transport.call(bootstrap, "object_bucket")
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
        observation = wait_unit(request, request_digest, bootstrap, transport, now, pause, private=seen)
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy":
        changed = (transport.call(bootstrap, "destroy", seen) if seen else transport.call(bootstrap, "destroy"))["changed"]
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
        ceph_native.require(observation["removed"], "not-removed")
    else:
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def execute_gateway(request, operation, request_digest, transport, bootstrap, *, now, pause):
    """An object gateway runs as a service unit does, its phases on the bootstrap host only: with an ingress the firewall check on each ingress
    host first, then zone, placement and its services. With TLS the certificate reaches every observation and removal, and the private key only
    the service apply."""
    seen = transport.gateway_material(False) if request["gateway"]["bindings"] else None
    changed = False
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        ingress_firewall(request, transport, ceph_native_object.frontend(request), now)
        for phase in ceph_native_object.APPLY_PHASES:
            private = transport.gateway_material(True) if seen and phase == "gateway_service" else None
            produced = transport.call(bootstrap, phase, private) if private else transport.call(bootstrap, phase)
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
            changed = produced["changed"] or changed
        observation = wait_unit(request, request_digest, bootstrap, transport, now, pause, private=seen)
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy":
        changed = (transport.call(bootstrap, "destroy", seen) if seen else transport.call(bootstrap, "destroy"))["changed"]
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
        ceph_native.require(observation["removed"], "not-removed")
    else:
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
    if seen:
        bound = hashlib.sha256(ceph_native_observability.leaf(seen["gatewayCertificate"])).hexdigest()
        ceph_native.require(observation["endpoint"]["certificateSHA256"] in ("", bound), "evidence")
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


def dashboard_phase(request, operation, request_digest, bootstrap, transport, observation, output, *, now, pause, connect):
    """After readiness: enable the dashboard, or recapture its credential read-only; then prove it from the controller."""
    changed = False
    if operation == "apply":
        produced = transport.call(bootstrap, "dashboard")
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "private"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
        dashboard_output(output, produced["private"])
        # The MGR redeploy restarted every manager; readiness is proved again after it.
        observation = wait_ready(request, request_digest, bootstrap, transport, now, pause)
        ceph_native.require(observation["ready"], "not-ready")
    else:
        produced = transport.call(bootstrap, "dashboard_read")
        ceph_native.require(isinstance(produced, dict) and set(produced) <= {"private"}, "evidence")
        if not produced:
            # An unfinished dashboard phase is partial work that apply completes.
            observation = dict(observation, ready=False, partial=True)
            return evidence(observation, request_digest, request), False
        dashboard_output(output, produced["private"])
    dashboard = dict(observation["dashboard"], reachable=dashboard_reachable(request, observation, connect))
    return evidence(dict(observation, dashboard=dashboard), request_digest, request), changed


def host_request(value):
    """A frozen host block request: its cluster's host scope and one of its hosts."""
    ceph_native.require(isinstance(value, dict) and set(value) == {"cluster", "host", "version"} and value["version"] == "ceph-host-v7")
    cluster = ceph_native.validate_scope(value["cluster"])
    ceph_native.require("packages" in cluster)
    ceph_native.require(sum(host["name"] == value["host"] for host in cluster["hosts"]) == 1)
    ceph_native.require(sum(source["host"] == value["host"] for source in cluster["packages"]["sources"]) == 1)
    return value


def host_evidence(value, request_digest, request=None):
    """The closed host block projection; no credential, path or vendor text."""
    fields = {"version", "request", "host", "owner", "fsid", "image", "ready", "removed", "imageID", "uid", "gid", "label", "policySHA256",
              "cephadmSHA256", "planDigest", "route", "devices"}
    ceph_native.require(isinstance(value, dict) and set(value) in (fields, fields | {"specializations"}), "evidence")
    ceph_native.require("specializations" not in value or specialization_ids(value["specializations"]), "evidence")
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
                            {"path", "serial", "deviceID", "wwn", "kernelPath", "sizeBytes", "rotational"}), "evidence")
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
    ceph_native.require(len(ceph_native.canonical(value)) <= 16000, "evidence")
    return value


def released(request, request_digest):
    """Release makes no effect: the packages and the pulled image stay shared host state."""
    cluster = request["cluster"]
    return {"version": ceph_host_runtime.EVIDENCE_VERSION, "request": request_digest, "host": request["host"], "owner": cluster["owner"],
            "fsid": cluster["fsid"], "image": cluster["image"], "ready": False, "removed": True, "imageID": "", "uid": 0, "gid": 0, "label": "",
            "policySHA256": "", "cephadmSHA256": "", "planDigest": "", "route": False, "devices": []}


def execute_host(request, operation, request_digest, transport):
    """Packages, then the long pull and profile call, then route and device identities."""
    host_request(request)
    cluster = request["cluster"]
    ceph_native.require(operation in ("apply", "observe", "destroy"))
    ceph_native.require(ceph_native.digest(request_digest))
    if operation == "destroy":
        return {"changed": False, "evidence": host_evidence(released(request, request_digest), request_digest, request)}
    host = next(item for item in cluster["hosts"] if item["name"] == request["host"])
    changed = False
    packages = None
    if operation == "apply":
        ceph_native.require(ceph_native.supported_transport(cluster), "unsupported-release")
        packages = transport.packages(host)
        changed = packages["changed"]
        for phase in ("profile", "route", "device_identity"):
            result = transport.host_profile(host) if phase == "profile" else transport.call(host, phase)
            ceph_native.require(isinstance(result, dict) and set(result) == {"changed"} and type(result["changed"]) is bool, "evidence")
            changed = result["changed"] or changed
    observed = transport.call(host, "host_observe")
    ceph_native.require(isinstance(observed, dict), "evidence")
    observation = host_evidence(dict(observed, request=request_digest), request_digest, request)
    if operation == "apply":
        ceph_native.require(observation["ready"] and observation["planDigest"] == packages["planDigest"], "not-ready")
    return {"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}


SMB_VERSION = ceph_smb_native.VERSION
SMB_EVIDENCE_VERSION = ceph_smb_native.EVIDENCE_VERSION
SMB_TYPES = ceph_smb_native.TYPES
SMB_SERVED = ceph_smb_native.SERVED
SMB_REQUIRED = ceph_smb_native.REQUIRED
SMB_OPTIONAL = ceph_smb_native.OPTIONAL
SMB_EVIDENCE = frozenset(("daemons", "exists", "fsid", "owned", "owner", "partial", "ready", "removed", "request", "resource", "version"))
SMB_USER_PASSWORD = ceph_smb_native.USER_PASSWORD
SMB_FSID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")
SMB_IMAGE = ceph_smb_native.IMAGE


def smb_request(value):
    """A completed SMB block request: the frozen resource with its cluster scope; every failure is the request's."""
    return ceph_smb_native.validate(value)


def smb_evidence(value, request_digest, request=None):
    """The closed SMB block projection: flags, ownership, the native resource name and a share's daemons; never a credential."""
    ceph_native.require(isinstance(value, dict) and set(value) == SMB_EVIDENCE, "evidence")
    ceph_native.require(value["version"] == SMB_EVIDENCE_VERSION and ceph_native.digest(request_digest) and value["request"] == request_digest, "evidence")
    ceph_native.require(all(type(value[key]) is bool for key in ("exists", "owned", "partial", "ready", "removed")), "evidence")
    ceph_native.require(ceph_native.named(value["owner"]) and isinstance(value["fsid"], str) and SMB_FSID.fullmatch(value["fsid"]) is not None, "evidence")
    ceph_native.require(isinstance(value["resource"], str) and len(value["resource"].encode()) <= 256, "evidence")
    ceph_native.require(isinstance(value["daemons"], list) and len(value["daemons"]) <= 128, "evidence")
    for daemon in value["daemons"]:
        ceph_native.require(isinstance(daemon, dict) and set(daemon) == {"host", "name", "service"}, "evidence")
        ceph_native.require(all(isinstance(daemon[key], str) for key in daemon) and UNIT_DAEMON.fullmatch(daemon["name"]) is not None, "evidence")
    ceph_native.require(not (value["ready"] and value["removed"]), "evidence")
    if request is not None:
        scope = request["scope"]
        ceph_native.require(value["owner"] == scope["owner"] and value["fsid"] == scope["fsid"], "evidence")
    ceph_native.require(len(ceph_native.canonical(value)) <= 16000, "evidence")
    return value


def observe_smb(request, request_digest, bootstrap, transport, timeout=660):
    """One SMB observation: the closed evidence, the pending generated daemons or addresses, and whether its cluster joins a directory."""
    observed = transport.smb(bootstrap, "smb_observe", request, timeout=timeout)
    ceph_native.require(isinstance(observed, dict) and "request" not in observed, "evidence")
    observed = dict(observed)
    blocking, directory = observed.pop("blocking", None), observed.pop("directory", None)
    ceph_native.require(isinstance(blocking, list) and len(blocking) <= 16 and type(directory) is bool, "evidence")
    ceph_native.require(all(isinstance(entry, str) and ceph_smb_native.PENDING.fullmatch(entry) is not None for entry in blocking), "evidence")
    observed["request"] = request_digest
    return smb_evidence(observed, request_digest, request), blocking, directory


def wait_smb(request, request_digest, bootstrap, transport, now, pause):
    """Poll a share until its generated service runs; a timeout names only whether the SMB cluster joins a directory, never a native message."""
    deadline = now() + READINESS_TIMEOUT
    directory = False
    while True:
        remaining = deadline - now()
        if remaining > 0:
            observed, unused, directory = observe_smb(request, request_digest, bootstrap, transport, min(660, remaining))
            if observed["ready"]:
                return observed
            ceph_native.require(observed["owned"] and observed["exists"], "smb-identity")
            remaining = deadline - now()
        if remaining <= 0:
            raise ceph_native.Refusal("smb-directory" if directory else "smb-unavailable")
        pause(min(READINESS_INTERVAL, remaining))


def execute_smb(request, operation, request_digest, transport, *, now=time.monotonic, pause=time.sleep):
    """An SMB block runs on its cluster's bootstrap host: apply then prove the resource, a share until its generated service runs; or observe.
    Removal is the cluster's retirement, never this block's."""
    request = smb_request(request)
    ceph_native.require(ceph_native.digest(request_digest) and operation in ("apply", "observe"), "request")
    scope = request["scope"]
    bootstrap = next((host for host in scope["hosts"] if host["name"] == scope["bootstrap"]), None)
    ceph_native.require(bootstrap is not None, "request")
    if operation == "observe":
        return {"changed": False, "evidence": observe_smb(request, request_digest, bootstrap, transport)[0]}
    private = transport.smb_material() if request["bindings"] else {}
    produced = transport.smb(bootstrap, "smb_apply", request, private)
    ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
    if request["resourceType"] == "ceph.smb.share":
        observation = wait_smb(request, request_digest, bootstrap, transport, now, pause)
    else:
        observation = observe_smb(request, request_digest, bootstrap, transport)[0]
        ceph_native.require(observation["ready"], "smb-result")
    return {"changed": produced["changed"], "evidence": observation}
