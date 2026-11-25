"""Controller-local Ceph coordinator."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import concurrent.futures
import hashlib
import json
import re
import socket
import ssl
import time

from ansible_collections.bootwright.core.plugins.module_utils import (
    adapter_protocol, canonical_json, ceph_evidence, ceph_host_runtime, ceph_native, ceph_native_nfs, ceph_native_nvmeof,
    ceph_native_object, ceph_native_observability, ceph_native_resources, ceph_native_stretch, ceph_native_units, ceph_native_worker, ceph_smb_native,
    ceph_ssh_policy, ceph_transport, ceph_tunnel_staging, native_diagnostics, private_files,
)

READINESS_TIMEOUT = 900
READINESS_INTERVAL = 5
# One bounded read of a wait: the transport's ordinary call bound around a 640 s worker.
READ_BUDGET = 660
# The cluster apply's first readiness wait after the services call covers cephadm's OSD creation, which grows with the OSD count. It may run
# to the run's end less what may still follow it at its ceilings: each later native call (NATIVE_CALL, the Go nativeCall), each later
# readiness wait, and RUN_MARGIN for the playbook around this module. Its Go caller states the run deadline in whole seconds.
NATIVE_CALL = 660
RUN_MARGIN = 120
# Past its own bound that wait goes on only while cephadm visibly creates OSDs: the never-booted OSDs its observations name changed within
# CREATION_QUIET seconds. cephadm names a new one about every 21 s while it prepares a host's OSDs and pauses under a minute between hosts.
CREATION_QUIET = 300
RUN_DEADLINE = re.compile(r"[1-9][0-9]{0,4}\Z")
# Each ingress host's read-only firewall check: every host in one concurrent round, within one native call of the block's Go deadline.
FIREWALL_CALL = 120
HOLDER_CALL = 60
# The rendered-configuration proof of a management gateway's oauth2-proxy: every host in one concurrent round, rounds within a window of
# one native call of the block's Go deadline.
SSO_CALL = 120
SSO_WINDOW = 600
SSO_PAUSE = 10
# The Grafana route proof of a management gateway fronting Grafana runs inside the unit's readiness wait: every gateway host in one
# concurrent round of calls of at most ROUTE_CALL, each round after an observation reads the unit ready.
ROUTE_CALL = 120
ROUTE_ENTRY = "route@mgmt-gateway"
ROUTES = frozenset(("external", "nginx"))
# One diff observation's host reading (state-reconciliation.md, Reading the real side live): the `hosts` reading of a resource block reads
# each host's state once, and every resource block of the same scope replays it in place of its own host reads. Its Go caller
# (decodeHostReading in internal/storage/ceph) decodes the same closed members; it lives within that one diff and is never stored.
HOST_READING = "ceph-host-reading-v1"
HOST_READING_MEMBERS = frozenset(("version", "request", "scope", "hosts"))
UNRECORDED_READING = "unrecorded"
HOSTS_READING = "hosts"
# The produced-material output each action writes on apply and observe: its Go caller reads exactly that one.
OUTPUTS = {"client": "client", "cluster": "dashboard", "object-user": "object-user"}
# A service unit's one output: a management gateway's monitoring logins, written only when it fronts a Prometheus or an Alertmanager.
MONITORING_OUTPUT = "monitoring"
# The refusals each block's Go caller remedies by name (hostRefusals,
# adapterRefusals and smbRefusals in internal/storage/ceph); a test holds them equal.
HOST_REASONS = (
    "fips-policy", "fips-not-enabled", "stretch-intent",
    "unsupported-release", "image-pull", "image-platform", "image-profile", "route-probe", "device-identity", "runtime-profile", "not-ready",
    "device-serial-absent", "device-serial-ambiguous", "device-serial-mismatch", "device-alias", "device-root",
    "package-acquisition", "package-resolution", "package-signature", "package-transaction", "request", "artifact", "ownership", "evidence",
    "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "foreign-cluster", "native-command", "native-timeout", "native-output",
    "unknown", "transport", "remote-refusal", "adapter-exception", "ssh-policy-host-excluded", "ssh-policy-unavailable",
    "tiebreaker-foreign", "not-removed", "native-option-undeclared",
)
CLUSTER_REASONS = (
    "fips-policy", "cephx-policy",
    "unsupported-release", "runtime-profile", "device-identity", "cache-unproved", "enrollment-budget",
    "dashboard-identity", "dashboard-tls-identity", "monitoring-template-identity", "dashboard-drift", "dashboard-policy",
    "dashboard-verify", "pool-identity", "pool-drift", "client-quiescence-unqualified", "native-option-undeclared", "call-home-unqualified",
    "dashboard-manager", "hostname-mismatch", "time-unsynchronized",
    "request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready",
    "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport",
    "remote-refusal", "adapter-exception", "foreign-client", "foreign-pool", "foreign-profile", "foreign-rule", "crush-capacity", "pg-budget",
    "foreign-rbd-image", "rbd-image-identity", "foreign-filesystem", "filesystem-identity", "filesystem-multiple-disabled", "foreign-subvolume-group",
    "mds-coverage", "foreign-subvolume", "subvolume-identity", "foreign-image", "nfs-module-disabled", "nfs-pool-foreign", "object-realm-foreign",
    "object-zone-pools", "object-endpoint-certificate", "object-user-foreign", "object-user-identity", "object-bucket-foreign",
    "object-bucket-identity", "object-bucket-s3", "nfs-export-foreign", "nfs-export-apply", "nfs-export-path-escape", "nfs-export-realm-default",
    "nvmeof-native", "nvmeof-identity", "nvmeof-foreign", "nvmeof-budget",
    "monitoring-login-unsupported",
    "ingress-firewall", "service-credential-foreign", "ssh-policy-host-excluded", "ssh-policy-unavailable", "tunnel-host-key-excluded",
    "sso-unverified", "stretch-intent", "stretch-interrupted", "stretch-location", "stretch-weight", "stretch-rule", "stretch-foreign", "stretch-pools",
    "stretch-drift", "tiebreaker-foreign", "tiebreaker-unreachable", "device-not-empty", "foreign-osd",
    "osd-partial", "osd-clear-failed", "osd-clear-unproved", "osd-orphan-booted", "osd-orphan-unproved", "osd-class-drift",
)
# The refusals an administrator login's module may publish, each one its Go caller names (loginRefusals in internal/storage/ceph).
ADMIN_REASONS = (
    "dashboard-identity", "dashboard-drift", "dashboard-policy", "dashboard-verify",
    "monitoring-login-drift", "grafana-login-drift", "grafana-login-drift-unapplied", "grafana-login-refused", "monitoring-login-unproved",
    "monitoring-login-unsupported",
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


class SharedRefusal(ceph_native.Refusal):
    """A host read the shared host reading recorded as refused: the block publishes the reason, detail and native cause its own read of
    that host would have published."""

    def __init__(self, refused):
        super().__init__(refused["reason"])
        self.detail = refused.get("detail")
        if "nativeError" in refused:
            self.native_error = refused["nativeError"]


class UnsettledRefusal(ceph_native.RemoteRefusal):
    """A host whose native layout cephadm kept moving through every bounded snapshot: not ready, never a contradiction."""

    def __init__(self, detail, host):
        super().__init__(detail)
        self.host = host


def deadline_cut(error, budget):
    """A bare timeout, the worker's or the transport's own, of a read whose budget a wait's deadline shortened below a whole read's: the
    wait ran out. A timeout whose worker names the timed-out command, or a read that had its whole budget, keeps its own reason."""
    timeout = error.detail if isinstance(error, ceph_native.RemoteRefusal) else str(error) if type(error) is ceph_native.Refusal else None
    return budget is not None and budget < READ_BUDGET and timeout == "native-timeout"


def read_refusal(error, budget, host):
    """The refusal one read of a wait raises: a host's unsettled snapshot, the wait's own end for a read its deadline cut short, or the
    read's own refusal unchanged."""
    if unsettled(error):
        return UnsettledRefusal(error.detail, host["name"])
    if deadline_cut(error, budget):
        return ceph_native.Refusal("not-ready")
    return error


def unsettled(error):
    """Only a worker's exact not-ready refusal; every other remote refusal keeps its own meaning."""
    return isinstance(error, ceph_native.RemoteRefusal) and str(error.detail or "").partition("@")[0] == "native-refusal:not-ready"


def blocking_entry(value):
    """A blocking health check code, a daemon a host has not yet running, a never-booted OSD naming no device, an OSD no declared
    service places, a service unit's or gateway's unmet daemon count or endpoint, or a scoped health check's note."""
    return isinstance(value, str) and (ceph_native.HEALTH_CHECK.fullmatch(value) is not None or ceph_native.PENDING_DAEMON.fullmatch(value) is not None
                                       or ceph_native.HEALTH_NOTE.fullmatch(value) is not None
                                       or ceph_native.PENDING_OSD.fullmatch(value) is not None or ceph_native.UNPLACED_OSD.fullmatch(value) is not None
                                       or ceph_native_units.PENDING.fullmatch(value) is not None or ceph_native_object.PENDING.fullmatch(value) is not None)


def observed(result, operation, blocking):
    """An observation's result, with the checks that kept it from ready beside the closed evidence, for the read's notes only."""
    if operation == "observe" and blocking:
        result["pending"] = list(blocking)
    return result


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
    if isinstance(error, SharedRefusal):
        return str(error), error.detail
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


def recorded(observation, transport, request_digest, request):
    """The block's evidence naming the specialization ids this execution's native phases applied; absent when none."""
    applied = getattr(transport, "applied", None)
    if not applied:
        return observation
    return ceph_evidence.evidence(dict(observation, specializations=sorted(applied)), request_digest, request)


def user_output(path, value, request):
    """The produced key pair, exactly the retained canonical {accessKey, secretKey, uid} of this user, written to the protected output."""
    ceph_native.require(isinstance(value, str) and value.isascii(), "material")
    data = value.encode("ascii")
    ceph_native_object.user_material(data, request["objectUser"]["name"], reason="material")
    private_files.write(path, data)


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
    private_files.write(path, value.encode())


def reading_scope(request):
    """The digest of what a host's state reading may read of a request: all of it but the action and the block's own resource."""
    projection = {key: value for key, value in request.items() if key != "action" and key not in ceph_native.RESOURCE_MEMBERS}
    return hashlib.sha256(canonical_json.canonical(projection)).hexdigest()


def host_state(transport, host, **options):
    """One host's state read, the one call a shared host reading stands in for; both paths read through it, so a refusal names one site."""
    return transport.call(host, "host_state", **options)


def host_entry(host, state):
    """One host's validated state reading, and the manager daemons it names pending."""
    ceph_native.require(isinstance(state, dict), "evidence")
    state = dict(state)
    waiting = state.pop("pending", None)
    ceph_native.require(waiting is None or isinstance(waiting, list) and 0 < len(waiting) <= 16
                        and all(isinstance(name, str) and ceph_native.PENDING_DAEMON.fullmatch(name) is not None for name in waiting), "evidence")
    ceph_native.require(set(state) == {"name", "owned", "exists"} and state["name"] == host["name"]
                        and type(state["owned"]) is bool and type(state["exists"]) is bool, "evidence")
    return state, waiting or []


def refused_entry(value):
    reasons = set(CLUSTER_REASONS) | {"unknown", "remote-refusal", "adapter-exception"}
    return (isinstance(value, dict) and {"reason", "unsettled"} <= set(value) <= {"reason", "unsettled", "detail", "nativeError"}
            and value["reason"] in reasons and type(value["unsettled"]) is bool and (not value["unsettled"] or "detail" in value and "nativeError" not in value)
            and ("detail" not in value or isinstance(value["detail"], str) and 0 < len(value["detail"]) <= adapter_protocol.MAX_DETAIL)
            and ("nativeError" not in value or native_diagnostics.valid(value["nativeError"])))


def host_reading(value, request_digest=None, request=None):
    """A closed host reading: each host's raw state in request order, ending at the first host whose read refused, with that refusal.
    Against a request it must name that request's reading scope and its hosts, so a block never replays another scope's reading."""
    ceph_native.require(isinstance(value, dict) and HOST_READING_MEMBERS <= set(value) <= HOST_READING_MEMBERS | {"specializations"}
                        and value["version"] == HOST_READING and ceph_native.digest(value["request"]) and ceph_native.digest(value["scope"]), "evidence")
    ceph_native.require(request_digest is None or value["request"] == request_digest, "evidence")
    ceph_native.require("specializations" not in value or ceph_evidence.specialization_ids(value["specializations"]), "evidence")
    hosts = value["hosts"]
    ceph_native.require(isinstance(hosts, list) and 0 < len(hosts) <= 64, "evidence")
    for index, entry in enumerate(hosts):
        ceph_native.require(isinstance(entry, dict) and set(entry) in ({"name", "state"}, {"name", "refused"}) and isinstance(entry["name"], str), "evidence")
        ceph_native.require(isinstance(entry["state"], dict) if "state" in entry else index == len(hosts) - 1 and refused_entry(entry["refused"]),
                            "evidence")
    if request is not None:
        ceph_native.require(value["scope"] == reading_scope(request), "request")
        names = [host["name"] for host in request["hosts"]]
        ceph_native.require([entry["name"] for entry in hosts] == names[:len(hosts)] and (len(hosts) == len(names) or "refused" in hosts[-1]),
                            "evidence")
    return value


def read_hosts(request, request_digest, transport):
    """Each host's state once, in request order, for every resource block of this reading scope to replay. It stops at the first host
    whose read refuses and records that refusal as a block's own read of that host would publish it."""
    entries = []
    for host in request["hosts"]:
        try:
            state = host_state(transport, host)
            host_entry(host, state)
        except Exception as error:  # pylint: disable=broad-exception-caught
            if unsettled(error):
                # The observation's own read rewraps it so (observe_state); the replay raises the same type again.
                error = UnsettledRefusal(error.detail, host["name"])
            reason, detail = refusal(error, CLUSTER_REASONS)
            refused = {"reason": reason, "unsettled": isinstance(error, UnsettledRefusal)}
            if detail:
                refused["detail"] = detail
            native_error = getattr(error, "native_error", None)
            if native_diagnostics.valid(native_error):
                refused["nativeError"] = native_error
            entries.append({"name": host["name"], "refused": refused})
            break
        entries.append({"name": host["name"], "state": state})
    value = {"version": HOST_READING, "request": request_digest, "scope": reading_scope(request), "hosts": entries}
    applied = getattr(transport, "applied", None)
    if applied:
        value["specializations"] = sorted(applied)
    return host_reading(value, request_digest, request)


def replayed_host(shared, index, host):
    """The shared reading's state of the index-th host, or the refusal its read recorded."""
    entry = shared["hosts"][index]
    ceph_native.require(entry["name"] == host["name"], "evidence")
    if "refused" in entry and entry["refused"]["unsettled"]:
        raise UnsettledRefusal(entry["refused"]["detail"], host["name"])
    if "refused" in entry:
        raise SharedRefusal(entry["refused"])
    return host_entry(host, entry["state"])


def observe_state(request, request_digest, bootstrap, transport, deadline=None, now=time.monotonic, blocking=None, activation=None, *,
                  unrecorded=False, shared=None):
    """One validated snapshot; an optional deadline bounds all of its probes, an optional list receives the health check
    codes that kept readiness unproved, and another whether only a stretch cluster's activation is still missing. A shared host
    reading, admitted against this request, stands in for each host's own state read. A read-only probe whose budget the
    deadline shortened and that then runs out is the deadline's own end, never an unknown effect."""
    def read(host, phase):
        budget = None
        try:
            if deadline is None:
                return host_state(transport, host) if phase == "host_state" else transport.call(host, phase)
            started = now()
            remaining = deadline - started
            ceph_native.require(remaining > 0, "not-ready")
            budget = min(READ_BUDGET, remaining)
            result = host_state(transport, host, timeout=budget) if phase == "host_state" else transport.call(host, phase, timeout=budget)
        except ceph_native.Refusal as error:
            refusal = read_refusal(error, budget, host)
            if refusal is error:
                raise
            raise refusal from None
        ceph_native.require(now() < min(deadline, started + budget), "not-ready")
        return result

    ceph_native.require(not unrecorded or request["action"] == "cluster", "request")
    observed = read(bootstrap, ceph_native.UNRECORDED_OBSERVE if unrecorded else "observe")
    ceph_native.require(isinstance(observed, dict), "evidence")
    observed = dict(observed)
    checks = observed.pop("blocking", [])
    pending_activation = observed.pop("activation", False)
    ceph_native.require(type(pending_activation) is bool and (not pending_activation or "stretch" in request), "evidence")
    ceph_native.require(isinstance(checks, list) and len(checks) <= 16 and all(blocking_entry(check) for check in checks), "evidence")
    if blocking is not None:
        blocking[:] = checks
    observed["request"] = request_digest
    ceph_evidence.evidence(observed, request_digest, request)
    host_states, pending = [], []
    for index, host in enumerate(request["hosts"]):
        state, waiting = host_entry(host, read(host, "host_state")) if shared is None else replayed_host(shared, index, host)
        host_states.append(state)
        pending += [name for name in waiting if name not in checks + pending]
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
        pending_activation = False
    if activation is not None:
        activation[:] = [pending_activation and observed["exists"] and observed["owned"] and all(state["exists"] for state in host_states)]
    return ceph_evidence.evidence(observed, request_digest, request)


def run_deadline(value):
    """The run deadline its Go caller states beside the material, in whole seconds within Ceph's six-hour bound, or None when it states none."""
    if value is None:
        return None
    ceph_native.require(isinstance(value, str) and RUN_DEADLINE.fullmatch(value) is not None and int(value) <= 21600, "request")
    return int(value)


def creation_wait_end(request, run_end, now):
    """The latest end of the first readiness wait after the services call, given a known run end: after its own READINESS_TIMEOUT or at that
    end less every later call and wait at its ceiling, whichever is later. A stretch cluster's two activation calls and its final wait follow
    it, then the dashboard call and the wait after it. With no run end the wait keeps its own bound."""
    if run_end is None:
        return None
    later = 3 * NATIVE_CALL + 2 * READINESS_TIMEOUT if "stretch" in request else NATIVE_CALL + READINESS_TIMEOUT
    return max(now() + READINESS_TIMEOUT, run_end - later)


class CreationWindow:
    """A readiness wait's end: its own READINESS_TIMEOUT, extended toward `until` only while the never-booted OSDs its observations name keep
    changing, each change keeping it open CREATION_QUIET seconds more. A cluster that stops converging ends the wait as before."""

    def __init__(self, now, until=None):
        self.now, self.until = now, until
        self.own = now() + READINESS_TIMEOUT
        self.changed, self.seen = self.own - CREATION_QUIET, frozenset()

    def note(self, blocking):
        names = frozenset(entry for entry in blocking if ceph_native.PENDING_OSD.fullmatch(entry))
        if names != self.seen:
            self.changed = self.now()
        self.seen = names

    def end(self):
        return self.own if self.until is None else max(self.own, min(self.until, self.changed + CREATION_QUIET))


def wait_ready(request, request_digest, bootstrap, transport, now, pause, until=None):
    """Wait only on proved partial state, without repeating any native effect; a timeout names the health checks still blocking.

    A host whose snapshot never settled while cephadm converges is not ready yet: the wait polls again and names it.
    """
    window = CreationWindow(now, until)
    blocking = []
    while True:
        try:
            observed = observe_state(request, request_digest, bootstrap, transport, window.end(), now, blocking)
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
        window.note(blocking)
        remaining = window.end() - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))


def wait_activation(request, request_digest, bootstrap, transport, now, pause, until=None):
    """Wait, read-only, until a stretch cluster is ready but for its activation, or already ready; a timeout names what blocks it."""
    window = CreationWindow(now, until)
    blocking, pending = [], [False]
    while True:
        try:
            observed = observe_state(request, request_digest, bootstrap, transport, window.end(), now, blocking, pending)
        except UnsettledRefusal as error:
            blocking[:] = (["unsettled@" + error.host] + [entry for entry in blocking if not entry.startswith("unsettled@")])[:16]
            observed = None
        except ceph_native.Refusal as error:
            if type(error) is ceph_native.Refusal and str(error) == "not-ready":
                raise ReadinessRefusal(blocking) from None
            raise
        if observed is not None and (observed["ready"] or pending[0]):
            return observed
        ceph_native.require(observed is None or observed["owned"] and observed["exists"] and observed["partial"], "not-ready")
        window.note(blocking)
        remaining = window.end() - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))


def activate_stretch(request, request_digest, bootstrap, transport, now, pause, run_end=None):
    """Native stretch mode once every MON is in quorum with its location and every OSD up: two journaled calls, before any pool."""
    if wait_activation(request, request_digest, bootstrap, transport, now, pause, creation_wait_end(request, run_end, now))["ready"]:
        return False
    changed = False
    for phase, last in (("stretch_locate", "rule-present"), ("stretch_enable", "verified")):
        result = transport.call(bootstrap, phase)
        # A continuation finds later stages already complete: each call reports the last completed stage, at least its own.
        ceph_native.require(isinstance(result, dict) and set(result) == {"changed", "stage"} and type(result["changed"]) is bool
                            and result["stage"] in ceph_native_stretch.STAGES
                            and ceph_native_stretch.STAGES.index(result["stage"]) >= ceph_native_stretch.STAGES.index(last), "evidence")
        changed = result["changed"] or changed
    return changed


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


def host_round(transport, hosts, phase, private=None, *, timeout=None):
    """Join the whole bounded round before returning the first refusal in host order; every call of it has the one ``timeout`` when given."""
    if not hosts:
        return []
    prepare = getattr(transport, "prepare_round", None)
    if prepare is not None:
        prepare()
    args, options = ((private,), {}) if timeout is None else ((), dict({} if private is None else {"private": private}, timeout=timeout))
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(hosts)) as executor:
        calls = [executor.submit(transport.call, host, phase, *args, **options) for host in hosts]
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


def authorize_hosts(request, transport, stages, private):
    """Authorize each host not yet prepared; every other host's retained authfile gets the registry login this run bound, before any
    native pull, since a rebind may have changed it after that host's authorization."""
    authorization = {name: private[name] for name in ("clusterPublicKey", "registryJSON", "clusterPrivateKey", "knownHosts")}
    authorized = host_round(transport, [host for host in request["hosts"] if stages[host["name"]] in ("unclaimed", "claimed")],
                            "authorize", authorization)
    refreshed = host_round(transport, [host for host in request["hosts"] if stages[host["name"]] not in ("unclaimed", "claimed")],
                           "registry_refresh", {"registryJSON": private["registryJSON"]})
    return any(value["changed"] for value in authorized + refreshed)


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
    if any(host.get("physical") for host in request["hosts"]):
        device_identities(request, transport)
    host_round(transport, request["hosts"], "devices")
    changed = False
    private = transport.bootstrap_material()
    managers = next(service["placement"]["hosts"] for service in request["services"] if service["service_type"] == "mgr")
    host_round(transport, [host for host in request["hosts"] if host["name"] in managers
                           and stages[host["name"]] in ("unclaimed", "claimed")], "tunnel_preflight", {"knownHosts": private["knownHosts"]})
    claimed = host_round(transport, [host for host in request["hosts"] if stages[host["name"]] == "unclaimed"], "claim")
    changed = any(value["changed"] for value in claimed) or changed
    changed = authorize_hosts(request, transport, stages, private) or changed
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
    devices = device_identities(request, transport)
    names = {name for service in request["services"] if service["service_type"] == "osd" for name in service["placement"]["hosts"]}
    prepared = host_round(transport, [host for host in request["hosts"] if host["name"] in names], "osd_prepare")
    changed = any(value["changed"] for value in prepared) or changed
    # Every pending service document shares one owned apply and readback. The
    # native worker journals each child independently before sending stdin.
    orphans = prepared_orphans(prepared)
    extra = {"orphans": orphans} if orphans else {}
    changed = transport.call(bootstrap, "services", devices=devices, **extra)["changed"] or changed
    return changed


def prepared_orphans(prepared):
    """The OSD fsids each host's owned partial clears zapped volumes named, which the bootstrap host's services call purges."""
    for value in prepared:
        ceph_native.require(isinstance(value, dict) and set(value) in ({"changed"}, {"changed", "orphans"}) and type(value["changed"]) is bool
                            and isinstance(value.get("orphans", []), list)
                            and all(isinstance(osd, str) and ceph_native.OSD_UUID.fullmatch(osd) for osd in value.get("orphans", [])), "evidence")
    return sorted({osd for value in prepared for osd in value.get("orphans", [])})


def device_identities(request, transport):
    """Every host's retained SDK identities, in declared host and device order."""
    result = []
    for host, value in zip(request["hosts"], host_round(transport, request["hosts"], "host_devices")):
        ceph_native.require(isinstance(value, dict) and set(value) == {"devices"}, "device-identity")
        result.extend(ceph_host_runtime.rows(request, host["name"], value["devices"]))
    ceph_host_runtime.distinct_devices(result)
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


def tunnel_before(request, request_digest, bootstrap, state, preparations):
    """The bootstrap host's recovery stage and the evidence it proved, agreeing with every host's own stage."""
    stage = ceph_tunnel_staging.prove_recovery_record(request, bootstrap["name"], state, evidence=True)
    host_stages = recovery_preparations(request, preparations)
    before = state["evidence"]
    ceph_native.require(isinstance(before, dict), "evidence")
    before = dict(before, request=request_digest)
    ceph_evidence.evidence(before, request_digest, request)
    ceph_native.require(before["owned"] == (stage != "unclaimed") and before["exists"] == (stage != "unclaimed"), "evidence")
    # A stretch cluster is complete before its activation is: complete and partial until activation verifies.
    activating = "stretch" in request and stage == "complete" and not before["ready"]
    ceph_native.require((before["ready"] or activating) == (stage == "complete")
                        and before["partial"] == (stage not in ("unclaimed", "complete") or activating), "evidence")
    if stage == "complete":
        ceph_native.require(all(value == "complete" for value in host_stages.values()), "partial-cluster")
    return stage, before


def execute_tiebreaker(request, operation, request_digest, transport, outputs):
    """A provided tiebreaker's removal, or its read-only observation, over the fleet connection to that host alone (D203).

    Neither the bootstrap host nor MON quorum is needed. A host that does not answer leaves the removal unknown, never absent.
    """
    ceph_native.require(operation in ("observe", "destroy") and not outputs, "request")
    host = ceph_evidence.tiebreaker_host(request)
    try:
        result = transport.call(host, "tiebreaker_remove" if operation == "destroy" else "tiebreaker_observe")
    except ceph_native.RemoteRefusal:
        raise
    except ceph_native.Refusal as error:
        if str(error) == "transport":
            raise ceph_native.Refusal("tiebreaker-unreachable") from None
        raise
    members = {"changed", "removed", "attempted"} if operation == "destroy" else {"removed", "attempted"}
    ceph_native.require(isinstance(result, dict) and set(result) == members and all(type(result[key]) is bool for key in members), "evidence")
    # A removal proves absence; only one that changed nothing may have run no attempt, on a host the cluster never claimed.
    ceph_native.require(operation == "observe" or result["removed"] and (result["attempted"] or not result["changed"]), "not-removed")
    value = {"version": ceph_evidence.TIEBREAKER_EVIDENCE, "request": request_digest, "owner": request["owner"], "fsid": request["fsid"], "host": host["name"],
             "removed": result["removed"], "attempted": result["attempted"]}
    return {"changed": result.get("changed", False), "evidence": ceph_evidence.tiebreaker_evidence(value, request_digest, request)}


def admit_selectors(request, operation, outputs, retirement, reading, shared=None):
    """The closed selectors beside the material: a tiebreaker's retirement; the unrecorded reading only a cluster observation with no
    output takes; and the host reading a resource observation with no output either takes or replays, never both."""
    ceph_native.require(retirement in (None, "tiebreaker"), "request")
    ceph_native.require(reading in (None, HOSTS_READING) or reading == UNRECORDED_READING and operation == "observe" and retirement is None
                        and request["action"] == "cluster" and not outputs, "request")
    hosts = reading == HOSTS_READING or shared is not None
    ceph_native.require(not hosts or operation == "observe" and retirement is None and request["action"] in ceph_native_resources.ACTIONS
                        and not outputs and (reading is None) != (shared is None), "request")
    if shared is not None:
        host_reading(shared, None, request)


def shared_hosts(value):
    """The host reading a block replays, as its Go caller lends it beside the material: one bounded JSON object."""
    if value is None or isinstance(value, dict):
        return value
    ceph_native.require(isinstance(value, str) and 0 < len(value) <= 65536, "request")
    try:
        return json.loads(value)
    except ValueError:
        raise ceph_native.Refusal("request") from None


def execute(request, operation, request_digest, transport, outputs=None, *, now=time.monotonic, pause=time.sleep, connect=tls_certificate,
            retirement=None, reading=None, shared=None, deadline=None):
    """Keep read-only observation distinct from any preparation or mutation; only an observation no operation records reads unrecorded.
    A `hosts` reading reads only each host's state; a shared host reading replaces those reads of a resource observation. Only a cluster
    apply states its run deadline, which this module's run began within."""
    run_end = None if deadline is None else now() + deadline - RUN_MARGIN
    ceph_native.validate(request)
    ceph_native.require(operation in ("apply", "observe", "destroy"))
    ceph_native.require(deadline is None or request["action"] == "cluster" and operation == "apply", "request")
    ceph_native.require(ceph_native.digest(request_digest))
    admit_selectors(request, operation, outputs, retirement, reading, shared)
    if reading == HOSTS_READING:
        return {"changed": False, "evidence": read_hosts(request, request_digest, transport)}
    if shared is not None and shared.get("specializations"):
        with transport.applied_lock:
            transport.applied.update(shared["specializations"])
    if operation != "observe":
        ceph_native.require(ceph_native.supported_transport(request), "unsupported-release")
    if retirement is not None:
        return execute_tiebreaker(request, operation, request_digest, transport, outputs)
    bootstrap = next(h for h in request["hosts"] if h["name"] == request["bootstrap"])
    outputs = admit_outputs(request, operation, outputs)
    extra = {"run_end": run_end} if request["action"] == "cluster" else {}
    return PROCEDURES[request["action"]](request, operation, request_digest, transport, bootstrap, outputs, now=now, pause=pause, connect=connect,
                                         reading=reading, shared=shared, **extra)


def admit_outputs(request, operation, outputs):
    """The produced-material outputs an action's apply declares and its observation declares or omits; every other run declares none."""
    outputs = outputs or {}
    ceph_native.require(set(outputs) <= set(OUTPUTS.values()) | {MONITORING_OUTPUT})
    output = expected_output(request)
    if output is not None and operation == "apply":
        ceph_native.require(set(outputs) == {output}, "material")
    elif output is not None and operation == "observe":
        # An observation no operation records declares no output: it proves presence and takes over no material.
        ceph_native.require(set(outputs) in ({output}, set()), "material")
    elif operation in ("apply", "observe"):
        ceph_native.require(not outputs, "material")
    return outputs


def expected_output(request):
    """The one output an action writes; a service unit writes one only as a management gateway with a login to read."""
    if request["action"] == "service":
        return MONITORING_OUTPUT if ceph_native_observability.credential_services(request["service"]) else None
    return OUTPUTS.get(request["action"])


def monitoring_output(request, operation, bootstrap, transport, path):
    """The gateway's logins, read on the bootstrap host after readiness, reach only the runner's protected output file. An observation
    reads them only once cephadm already holds each, and otherwise writes no output."""
    produced = transport.call(bootstrap, "monitoring_credentials" if operation == "apply" else "monitoring_credentials_held")
    if operation != "apply" and produced == {"changed": False}:
        return
    ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "private"} and produced["changed"] is False, "evidence")
    value = produced["private"]
    ceph_native.require(isinstance(value, str) and value.isascii(), "material")
    data = ceph_native_observability.logins_material(value.encode("ascii"), ceph_native_observability.credential_services(request["service"]))
    private_files.write(path, data)


def execute_cluster(request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, connect, reading, shared, run_end=None):
    """The cluster prepares every host in one round and bootstraps through the tunnel, stretches before readiness, then proves its
    dashboard. Its first readiness wait, before activation or else its postcondition's, may use the run's unspent time."""
    changed = False
    if operation == "apply":
        preparations = host_round(transport, request["hosts"], "prerequisites")
        state = transport.call(bootstrap, "bootstrap_state")
        stage, before = tunnel_before(request, request_digest, bootstrap, state, preparations)
        if not before["ready"]:
            if stage != "complete":
                changed = apply_tunnel_cluster(request, bootstrap, transport, state, preparations) or changed
            if "stretch" in request:
                changed = activate_stretch(request, request_digest, bootstrap, transport, now, pause, run_end) or changed
    elif operation == "destroy":
        changed = transport.call(bootstrap, "destroy")["changed"]
    pending = []
    until = creation_wait_end(request, run_end, now) if operation == "apply" and "stretch" not in request else None
    observation = postcondition(request, operation, request_digest, transport, bootstrap, pending, now=now, pause=pause, reading=reading,
                                shared=shared, until=until)
    if operation in ("apply", "observe") and observation["ready"]:
        observation, effect = dashboard_phase(request, operation, request_digest, bootstrap, transport, observation, outputs.get("dashboard"),
                                              now=now, pause=pause, connect=connect)
        changed = effect or changed
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


def execute_resource(request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, reading, shared, **_):
    """A pool, policy, client, RBD image, filesystem, subvolume or settings block prepares each host, then runs its own phase on the
    bootstrap host; a client writes its key to its output."""
    changed = False
    if operation == "apply":
        for host in request["hosts"]:
            transport.call(host, "prerequisites")
        before = transport.call(bootstrap, "observe")
        if not before["ready"]:
            if request["action"] in ("pool", "policy", "rbd", "subvolume", "settings"):
                changed = transport.call(bootstrap, request["action"])["changed"] or changed
            elif request["action"] == "filesystem":
                changed = apply_filesystem(request, bootstrap, transport, now, pause) or changed
        if request["action"] == "client":
            produced = transport.call(bootstrap, "client")
            changed = produced["changed"] or changed
            value = produced.get("private")
            ceph_native.require(isinstance(value, str) and 0 < len(value.encode()) <= 65536, "material")
            private_files.write(outputs["client"], value.encode())
    elif operation == "destroy":
        changed = transport.call(bootstrap, "destroy")["changed"]
    pending = []
    observation = postcondition(request, operation, request_digest, transport, bootstrap, pending, now=now, pause=pause, reading=reading,
                                shared=shared)
    if operation == "observe" and request["action"] == "client" and observation["ready"] and outputs:
        value = transport.call(bootstrap, "client_read").get("private")
        ceph_native.require(isinstance(value, str) and 0 < len(value.encode()) <= 65536, "material")
        private_files.write(outputs["client"], value.encode())
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


def postcondition(request, operation, request_digest, transport, bootstrap, pending, *, now, pause, reading, shared, until=None):
    """An apply waits for readiness, until its own bound or the given end, and a destroy proves removal; an observation reads once, its
    blocking checks into pending."""
    bound = () if until is None else (until,)
    observation = (wait_ready(request, request_digest, bootstrap, transport, now, pause, *bound) if operation == "apply"
                   else observe_state(request, request_digest, bootstrap, transport, blocking=pending, unrecorded=reading == UNRECORDED_READING,
                                      shared=shared))
    # Read-only observations may return partial evidence. Mutation completion
    # is not claimed until the actual desired postcondition is observed.
    if operation == "apply":
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy":
        ceph_native.require(observation["removed"], "not-removed")
    return observation


def unit_observation_timeout(request):
    return ceph_native_nvmeof.phase_budget(request, "nvmeof_unit_observe") + 20 if ceph_transport.nvmeof_unit(request) else 660


def observe_unit(request, request_digest, bootstrap, transport, timeout=None, blocking=None, *, private=None):
    """One read-only observation of a service unit on the bootstrap host; no other host is called."""
    if timeout is None and ceph_transport.nvmeof_unit(request):
        timeout = unit_observation_timeout(request)
    options = {} if timeout is None else {"timeout": timeout}
    if private:
        options["private"] = private
    observed = transport.call(bootstrap, "nvmeof_unit_observe" if ceph_transport.nvmeof_unit(request) else "observe", **options)
    ceph_native.require(isinstance(observed, dict), "evidence")
    observed = dict(observed)
    checks = observed.pop("blocking", [])
    ceph_native.require(isinstance(checks, list) and len(checks) <= 16 and all(blocking_entry(check) for check in checks), "evidence")
    if blocking is not None:
        blocking[:] = checks
    observed["request"] = request_digest
    return ceph_evidence.evidence(observed, request_digest, request)


def ingress_fronts(request):
    """Each ingress that fronts a service unit or a gateway, one per data site on a stretch cluster, with its client port."""
    if request.get("action") == "gateway":
        return ceph_native_object.frontends(request)
    if request.get("action") == "service":
        return [found for found in (ceph_native_units.frontend(request),) if found is not None]
    return []


def ingress_name(document):
    return document["service_type"] + "." + document["service_id"] if "service_id" in document else document["service_type"]


def keepalived_hosts(observed, ingress):
    """The hosts the observation lists running this ingress's keepalived, the only ones that can hold its virtual IP."""
    name = ingress_name(ingress)
    return sorted({daemon["host"] for daemon in observed.get("daemons", [])
                   if daemon.get("service") == name and str(daemon.get("name", "")).startswith("keepalived.")})


def vip_holders(request, observed, transport, deadline, now):
    """Read-only, within the readiness window: each host running an ingress's keepalived says whether it holds the virtual IP now. Each
    virtual address needs exactly one holder; the entries name an address that none holds, or that several hold, as dropped VRRP leaves it
    split, and a keepalived host that does not answer. None when the window closes before every host answered."""
    hosts = {host["name"]: host for host in request["hosts"]}
    entries = []
    for ingress, unused in ingress_fronts(request):
        held, unanswered = 0, []
        for name in keepalived_hosts(observed, ingress):
            ceph_native.require(name in hosts, "evidence")
            remaining = deadline - now()
            if remaining <= 0:
                return None
            try:
                result = transport.call(hosts[name], "vip_holder", timeout=min(HOLDER_CALL, remaining))
            except ceph_native.RemoteRefusal:
                raise
            except ceph_native.Refusal as error:
                if str(error) != "transport":
                    raise
                unanswered.append("unreachable@" + name)
                continue
            ceph_native.require(isinstance(result, dict) and set(result) == {"changed", "holds"} and result["changed"] is False
                                and type(result["holds"]) is bool, "evidence")
            held += result["holds"]
        if unanswered:
            entries.extend(unanswered)
        elif held != 1:
            entries.append(("noholder@" if held == 0 else "holders@") + ingress_name(ingress))
    return entries


class ManagerPorts:
    """One apply's MGR port remedy, within the readiness window that names the lag: its first evaluated call may ask a target an
    earlier apply recorded once more, and every later call asks only an unrecorded one, so no target is asked twice in one apply."""

    def __init__(self, bootstrap, transport):
        self.bootstrap, self.transport = bootstrap, transport
        self.evaluated = self.changed = False

    def __call__(self, timeout):
        produced = self.transport.call(self.bootstrap, "manager_ports_unrecorded" if self.evaluated else "manager_ports", timeout=timeout)
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "pending"} and type(produced["changed"]) is bool
                            and type(produced["pending"]) is bool and not (produced["changed"] and produced["pending"]), "evidence")
        self.evaluated = self.evaluated or not produced["pending"]
        self.changed = self.changed or produced["changed"]


class GrafanaRoutes:
    """One apply's Grafana route proof, inside the readiness wait once the unit reads ready, so it never shortens or extends that wait:
    the bootstrap host names the selected routes once, then each call proves them on every gateway host. A running daemon without a
    selected route, as one cephadm rendered before the manager held the stored overrides, makes the bootstrap host ask cephadm once in
    this apply to reconfigure the gateway."""

    def __init__(self, request, bootstrap, transport):
        self.request, self.bootstrap, self.transport = request, bootstrap, transport
        self.routes, self.asked = None, False

    def __call__(self, deadline, now):
        """The blocking entries: none once every placed gateway daemon serves each selected route; None when the window closed first."""
        remaining = deadline - now()
        if remaining <= 0:
            return None
        if self.routes is None:
            produced = self.transport.call(self.bootstrap, "grafana_route", timeout=min(READ_BUDGET, remaining))
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "routes"} and produced["changed"] is False
                                and isinstance(produced["routes"], list) and set(produced["routes"]) <= ROUTES
                                and produced["routes"] == sorted(set(produced["routes"])), "evidence")
            self.routes = produced["routes"]
        if not self.routes:
            return []
        daemons, served = self.round(deadline, now)
        if daemons is None:
            return None
        gateway = ceph_native_observability.gateway_documents(self.request["service"])[1]
        if daemons == ceph_native_observability.placed_count(gateway) and all(served[name] == daemons for name in self.routes):
            return []
        remaining = deadline - now()
        if any(served[name] < daemons for name in self.routes) and not self.asked and remaining > 0:
            produced = self.transport.call(self.bootstrap, "grafana_reconfig", timeout=min(READ_BUDGET, remaining))
            ceph_native.require(produced == {"changed": True}, "evidence")
            self.asked = True
        return [ROUTE_ENTRY]

    def round(self, deadline, now):
        remaining = deadline - now()
        if remaining <= 0:
            return None, None
        hosts = {host["name"]: host for host in self.request["hosts"]}
        gateway = ceph_native_observability.gateway_documents(self.request["service"])[1]
        daemons, served = 0, dict.fromkeys(ROUTES, 0)
        for result in host_round(self.transport, [hosts[name] for name in gateway["placement"]["hosts"]], "grafana_route_proof",
                                 timeout=min(ROUTE_CALL, remaining)):
            ceph_native.require(isinstance(result, dict) and set(result) == {"changed", "daemons", "served"} and result["changed"] is False
                                and type(result["daemons"]) is int and result["daemons"] >= 0 and isinstance(result["served"], dict)
                                and set(result["served"]) == ROUTES and all(type(count) is int and 0 <= count <= result["daemons"]
                                                                            for count in result["served"].values()), "evidence")
            daemons += result["daemons"]
            for name in ROUTES:
                served[name] += result["served"][name]
        return daemons, served


def wait_unit(request, request_digest, bootstrap, transport, now, pause, *, private=None, remedy=None, routes=None):
    """Poll the unit's own observation until ready and, with an ingress, until each virtual IP has exactly one holder; a timeout names the
    daemons, endpoint, holder or health checks still blocking. While an observation names lagging MGR ports, an apply's remedy runs
    within the same window before the next poll, and a management gateway fronting Grafana proves its routes once the unit reads ready."""
    call_timeout = unit_observation_timeout(request)
    deadline = now() + READINESS_TIMEOUT + (call_timeout if ceph_transport.nvmeof_unit(request) else 0)
    blocking, unheld = [], []
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
            held = vip_holders(request, observed, transport, deadline, now)
            if held == [] and routes is not None:
                held = routes(deadline, now)
            if held == []:
                return observed
            if held is None:
                raise ReadinessRefusal(unheld or blocking)
            unheld = blocking[:] = held[:16]
        else:
            ceph_native.require(observed is None or observed["owned"] and observed["exists"] and observed["partial"], "not-ready")
        remaining = deadline - now()
        if remaining <= 0:
            raise ReadinessRefusal(blocking)
        if remedy is not None and observed is not None and ceph_native_observability.PORTS_ENTRY in blocking:
            remedy(min(READ_BUDGET, remaining))
            remaining = deadline - now()
            if remaining <= 0:
                raise ReadinessRefusal(blocking)
        pause(min(READINESS_INTERVAL, remaining))


def ingress_firewall(request, transport, fronts):
    """Before the first effect of an apply: each host of each ingress, one per data site on a stretch cluster, proves, read-only, that an
    active firewall passes VRRP and the client port. The hosts answer in one concurrent round of calls of at most FIREWALL_CALL, so the
    check lasts one call whatever the host count, within the block's one native call of its Go deadline."""
    hosts = {host["name"]: host for host in request["hosts"]}
    names = list(dict.fromkeys(name for found in fronts for name in found[0]["placement"]["hosts"]))
    results = host_round(transport, [hosts[name] for name in names], "ingress_firewall", timeout=FIREWALL_CALL)
    ceph_native.require(all(result == {"changed": False} for result in results), "evidence")


def window_call(call, deadline, now):
    """The timeout of one concurrent round's calls: the call bound, or what the window has left; an exhausted window refuses not-ready."""
    remaining = deadline - now()
    ceph_native.require(remaining > 0, "not-ready")
    return min(call, remaining)


def sso_round(proxy, hosts, held, transport, deadline, now):
    """One read-only proof round over the proxy's hosts, all called concurrently: the running proxy containers counted, and whether each
    one proved."""
    daemons, proved = 0, True
    results = host_round(transport, [hosts[name] for name in proxy["placement"]["hosts"]], "sso_config", held,
                         timeout=window_call(SSO_CALL, deadline, now))
    for result in results:
        ceph_native.require(isinstance(result, dict) and set(result) == {"changed", "daemons", "proved"} and result["changed"] is False
                            and type(result["daemons"]) is int and result["daemons"] >= 0 and type(result["proved"]) is bool, "evidence")
        daemons += result["daemons"]
        proved = proved and result["proved"]
    return daemons, proved


def sso_proof(request, transport, now, pause):
    """After the unit's apply, before the dashboard's single sign-on is enabled: every proxy host proves, read-only, that each running
    oauth2-proxy container of this cluster verifies its identity provider's certificate and mounts the bound CA bundle. Each round calls
    every host concurrently, so a round lasts one call whatever the host count; rounds repeat until the placed count runs proved, and one
    native call of the block's Go deadline covers the window, which the proof returns."""
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
    return ceph_evidence.evidence(observed, request_digest, request)


def execute_nvmeof(request, operation, request_digest, transport, bootstrap, *, now, pause):
    budgets = request["nvmeof"]["budgets"]
    ceph_native.require(budgets["apply"] <= ceph_native_nvmeof.MAX_BUDGET, "nvmeof-budget")
    changed = False
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        produced = transport.call(bootstrap, "nvmeof", timeout=ceph_native_nvmeof.phase_budget(request, "nvmeof") + 20)
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
    observation_budget = budgets["observation"]
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
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, blocking)


def execute_unit(request, operation, request_digest, transport, bootstrap, *, now, pause, phases=("unit",), outputs=None):
    """A service unit runs its apply phases only on the bootstrap host: its preparation proof, with an ingress the firewall check on each
    ingress host, its apply phases in order, with an oauth2-proxy the rendered-configuration proof on each proxy host and then the one
    single sign-on write, then its own readiness wait, which for a gateway fronting Grafana includes the route proof on each gateway host,
    and in which a monitoring unit with prometheus asks cephadm to reconfigure each MGR whose deployment ports lag the published module
    endpoints. Its apply phase holds every bound leaf; each observation and removal holds the observed ones, and a management gateway's
    served leaf must be the bound certificate's. A ready management gateway that declares its output then writes its monitoring logins
    there."""
    changed, pending = False, []
    bound_unit = request["action"] == "service" and bool(request["service"].get("bindings"))
    seen = transport.unit_material("observe") if bound_unit else {}
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        if request["action"] == "service":
            ingress_firewall(request, transport, ingress_fronts(request))
        bound = transport.unit_material("unit") if bound_unit else {}
        for phase in phases:
            produced = transport.call(bootstrap, phase, bound) if bound and phase == "unit" else transport.call(bootstrap, phase)
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed"} and type(produced["changed"]) is bool, "evidence")
            changed = produced["changed"] or changed
        if request["action"] == "service" and ceph_evidence.proxied(request):
            deadline = sso_proof(request, transport, now, pause)
            changed = sso_enable(bootstrap, transport, deadline, now, pause) or changed
        remedy = ManagerPorts(bootstrap, transport) if (
            request["action"] == "service" and ceph_native_observability.prometheus_declared(request["service"])) else None
        routes = GrafanaRoutes(request, bootstrap, transport) if (
            request["action"] == "service" and ceph_native_observability.grafana_fronted(request["service"])) else None
        observation = wait_unit(request, request_digest, bootstrap, transport, now, pause, private=seen, remedy=remedy, routes=routes)
        changed = remedy is not None and remedy.changed or routes is not None and routes.asked or changed
        ceph_native.require(observation["ready"], "not-ready")
    elif operation == "destroy" and ceph_transport.nvmeof_unit(request):
        observation = observe_unit(request, request_digest, bootstrap, transport)
        ceph_native.require(observation["removed"], "client-quiescence-unqualified")
    elif operation == "destroy":
        changed = (transport.call(bootstrap, "destroy", seen) if seen else transport.call(bootstrap, "destroy"))["changed"]
        observation = observe_unit(request, request_digest, bootstrap, transport, private=seen)
        ceph_native.require(observation["removed"], "not-removed")
    else:
        observation = observe_unit(request, request_digest, bootstrap, transport, blocking=pending, private=seen)
    if "mgmtGatewayCertificate" in seen:
        bound = hashlib.sha256(ceph_native_observability.leaf(seen["mgmtGatewayCertificate"])).hexdigest()
        ceph_native.require(observation["endpoint"]["certificateSHA256"] in ("", bound), "evidence")
    if outputs and observation["ready"]:
        monitoring_output(request, operation, bootstrap, transport, outputs[MONITORING_OUTPUT])
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


def execute_user(request, operation, request_digest, transport, bootstrap, outputs, *, now, pause):
    """An object user runs on the bootstrap host only: its preparation proof and its one phase, whose key pair reaches the protected
    output before its readiness wait; an observation that proves it ready recaptures the retained pair read-only when it declares the
    output, and reads presence alone when no operation records it. Removal is the core refusal."""
    changed, pending = False, []
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
        observation = observe_unit(request, request_digest, bootstrap, transport, blocking=pending)
        if observation["ready"] and outputs:
            produced = transport.call(bootstrap, "object_user_read")
            ceph_native.require(isinstance(produced, dict) and set(produced) == {"private"}, "evidence")
            user_output(outputs["object-user"], produced["private"], request)
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


def execute_bucket(request, operation, request_digest, transport, bootstrap, *, now, pause):
    """An object bucket runs on the bootstrap host only: its preparation proof and its one phase, then its readiness wait. With TLS at the
    backend the bound certificate, never a key, reaches every phase, which pins the daemon's leaf to it. Removal is the core refusal."""
    seen = transport.gateway_material(False) if request["objectBucket"]["certificateRef"] else None
    changed, pending = False, []
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
        observation = observe_unit(request, request_digest, bootstrap, transport, blocking=pending, private=seen)
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


def execute_gateway(request, operation, request_digest, transport, bootstrap, *, now, pause):
    """An object gateway runs as a service unit does, its phases on the bootstrap host only: with an ingress the firewall check on each ingress
    host first, then zone, placement and its services. With TLS the certificate reaches every observation and removal, and the private key only
    the service apply."""
    seen = transport.gateway_material(False) if request["gateway"]["bindings"] else None
    changed, pending = False, []
    if operation == "apply":
        transport.call(bootstrap, "prerequisites")
        ingress_firewall(request, transport, ingress_fronts(request))
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
        observation = observe_unit(request, request_digest, bootstrap, transport, blocking=pending, private=seen)
    if seen:
        bound = hashlib.sha256(ceph_native_observability.leaf(seen["gatewayCertificate"])).hexdigest()
        ceph_native.require(observation["endpoint"]["certificateSHA256"] in ("", bound), "evidence")
    return observed({"changed": changed, "evidence": recorded(observation, transport, request_digest, request)}, operation, pending)


# Each action's procedure; a unit's is looked up when it runs and takes only what it reads.
PROCEDURES = {
    "cluster": execute_cluster,
    "pool": execute_resource,
    "policy": execute_resource,
    "client": execute_resource,
    "rbd": execute_resource,
    "filesystem": execute_resource,
    "subvolume": execute_resource,
    "settings": execute_resource,
    "service": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_unit(
        request, operation, request_digest, transport, bootstrap, now=now, pause=pause, outputs=outputs),
    "nvmeof": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_nvmeof(
        request, operation, request_digest, transport, bootstrap, now=now, pause=pause),
    "nfs-export": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_unit(
        request, operation, request_digest, transport, bootstrap, now=now, pause=pause, phases=ceph_native_nfs.EXPORT_APPLY_PHASES),
    "gateway": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_gateway(
        request, operation, request_digest, transport, bootstrap, now=now, pause=pause),
    "object-user": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_user(
        request, operation, request_digest, transport, bootstrap, outputs, now=now, pause=pause),
    "object-bucket": lambda request, operation, request_digest, transport, bootstrap, outputs, *, now, pause, **_: execute_bucket(
        request, operation, request_digest, transport, bootstrap, now=now, pause=pause),
}


def dashboard_phase(request, operation, request_digest, bootstrap, transport, observation, output, *, now, pause, connect):
    """After readiness: enable the dashboard, or recapture its credential read-only, or with no output prove it present; then prove it from
    the controller."""
    changed = False
    if operation == "apply":
        produced = transport.call(bootstrap, "dashboard")
        ceph_native.require(isinstance(produced, dict) and set(produced) == {"changed", "private"} and type(produced["changed"]) is bool, "evidence")
        changed = produced["changed"]
        dashboard_output(output, produced["private"])
        # The MGR redeploy restarted every manager; readiness is proved again after it.
        observation = wait_ready(request, request_digest, bootstrap, transport, now, pause)
        ceph_native.require(observation["ready"], "not-ready")
    elif output is None:
        # With no output declared the bootstrap host proves the account against its retained password and returns presence only.
        produced = transport.call(bootstrap, "dashboard_present")
        present = isinstance(produced, dict) and set(produced) == {"present"} and produced["present"] is True
        ceph_native.require(produced == {} or present, "evidence")
        if not produced:
            observation = dict(observation, ready=False, partial=True)
            return ceph_evidence.evidence(observation, request_digest, request), False
    else:
        produced = transport.call(bootstrap, "dashboard_read")
        ceph_native.require(isinstance(produced, dict) and set(produced) <= {"private"}, "evidence")
        if not produced:
            # An unfinished dashboard phase is partial work that apply completes.
            observation = dict(observation, ready=False, partial=True)
            return ceph_evidence.evidence(observation, request_digest, request), False
        dashboard_output(output, produced["private"])
    dashboard = dict(observation["dashboard"], reachable=dashboard_reachable(request, observation, connect))
    return ceph_evidence.evidence(dict(observation, dashboard=dashboard), request_digest, request), changed


def host_request(value, *, removal=False):
    """A frozen host block request: its cluster's host scope and one of its hosts."""
    ceph_native.require(isinstance(value, dict) and set(value) == {"cluster", "host", "imagesSeconds", "pulls", "version"}
                        and value["version"] == "ceph-host-v1")
    ceph_native.require(type(value["imagesSeconds"]) is int and 0 < value["imagesSeconds"] <= max(ceph_transport.WORKER_CEILINGS))
    cluster = ceph_native.validate_scope(value["cluster"], delegated=removal)
    ceph_native.require("packages" in cluster)
    ceph_native.require(sum(host["name"] == value["host"] for host in cluster["hosts"]) == 1)
    ceph_native.require(sum(source["host"] == value["host"] for source in cluster["packages"]["sources"]) == 1)
    pulls = value["pulls"]
    ceph_native.require(isinstance(pulls, list) and all(isinstance(pull, dict) and set(pull) == {"group", "names"} for pull in pulls))
    frozen = [entry["name"] for entry in cluster.get("auxiliary", [])]
    named = []
    for index, pull in enumerate(pulls):
        names = pull["names"]
        ceph_native.require(isinstance(names, list) and 0 < len(names) <= ceph_host_runtime.PULL_CONCURRENCY
                            and (index == len(pulls) - 1 or len(names) == ceph_host_runtime.PULL_CONCURRENCY))
        ceph_native.require(pull["group"] == pull_group(names))
        named += names
    ceph_native.require(len(set(named)) == len(named) and all(name in frozen for name in named))
    ceph_native.require(sorted(named, key=frozen.index) == named)
    return value


CEPH_IMAGE_GROUP = "pull-ceph-image"


def pull_group(names):
    """The progress group the plan froze for one pull batch: its closed cephadm image names."""
    ceph_native.require(all(isinstance(name, str) for name in names))
    return "pull-" + "-".join(names).replace("_", "-")


def host_pull_names(request, pull):
    """The auxiliary names one images step pulls: none for the Ceph image group, else one frozen batch of this host."""
    if pull == CEPH_IMAGE_GROUP:
        return []
    batch = next((item["names"] for item in request["pulls"] if item["group"] == pull), None)
    ceph_native.require(batch is not None, "request")
    return list(batch)


def host_auxiliary_names(request):
    """Every auxiliary name this host pre-pulls, in name order."""
    return [name for pull in request["pulls"] for name in pull["names"]]


def released(request, request_digest):
    """Release makes no effect: the packages and the pulled image stay shared host state."""
    cluster = request["cluster"]
    return {"version": ceph_host_runtime.EVIDENCE_VERSION, "request": request_digest, "host": request["host"], "owner": cluster["owner"],
            "fsid": cluster["fsid"], "image": cluster["image"], "ready": False, "removed": True, "imageID": "", "uid": 0, "gid": 0, "label": "",
            "policySHA256": "", "cephadmSHA256": "", "planDigest": "", "route": False, "devices": []}


HOST_STEPS = ("packages", "images", "profile", "route", "devices", "verify")
HOST_STEP_PHASES = {"profile": "profile", "route": "route", "devices": "device_identity"}


def applied_ids(transport):
    """The specialization ids this transport's native phases applied, sorted."""
    return sorted(getattr(transport, "applied", None) or ())


def host_step(request, step, request_digest, transport, *, plan_digest="", applied=(), pull=None, seconds=None,
              clock=time.monotonic):
    """One phase of a host preparation apply, so its role reports each as a progress group.

    Each step resumes from the host's own journals. Only ``verify`` publishes evidence: it proves the package decision the
    ``packages`` step reported and names every specialization id the earlier steps applied.
    """
    host_request(request)
    cluster = request["cluster"]
    ceph_native.require(step in HOST_STEPS and ceph_native.digest(request_digest), "request")
    ceph_native.require((pull is not None) == (step == "images"), "request")
    ceiling = request["imagesSeconds"]
    seconds = ceiling if seconds is None else seconds
    ceph_native.require(type(seconds) is int and 0 < seconds <= ceiling, "image-pull" if step == "images" else "request")
    host = next(item for item in cluster["hosts"] if item["name"] == request["host"])
    if step == "packages" and ceph_host_runtime.observes_fips(cluster, host):
        observed = transport.call(host, "fips_observe")
        ceph_native.require(ceph_native.fips_proved(observed), "fips-not-enabled")
    ceph_native.require(ceph_native.supported_transport(cluster), "unsupported-release")
    if step == "packages":
        packages = transport.packages(host)
        return {"changed": packages["changed"], "planDigest": packages["planDigest"], "specializations": applied_ids(transport)}
    if step == "images":
        names = host_pull_names(request, pull)
        started = clock()
        result = transport.host_images(host, names, seconds, ceiling)
        ceph_native.require(isinstance(result, dict) and set(result) == {"changed"} and type(result["changed"]) is bool, "evidence")
        remaining = seconds - int(clock() - started + 1)
        return {"changed": result["changed"], "specializations": applied_ids(transport), "seconds": max(remaining, 0)}
    if step != "verify":
        names = host_auxiliary_names(request) if step == "profile" else None
        result = transport.call(host, HOST_STEP_PHASES[step], names=names) if names is not None else transport.call(host, HOST_STEP_PHASES[step])
        ceph_native.require(isinstance(result, dict) and set(result) == {"changed"} and type(result["changed"]) is bool, "evidence")
        return {"changed": result["changed"], "specializations": applied_ids(transport)}
    applied = list(applied)
    ceph_native.require(ceph_native.digest(plan_digest) and (not applied or ceph_evidence.specialization_ids(sorted(set(applied)))), "request")
    observed = transport.call(host, "host_observe")
    ceph_native.require(isinstance(observed, dict), "evidence")
    observation = ceph_evidence.host_evidence(dict(observed, request=request_digest), request_digest, request)
    ceph_native.require(observation["ready"] and observation["planDigest"] == plan_digest, "not-ready")
    named = sorted(set(applied) | set(applied_ids(transport)))
    if named:
        observation = ceph_evidence.evidence(dict(observation, specializations=named), request_digest, request)
    return {"changed": False, "evidence": observation}


def execute_host(request, operation, request_digest, transport):
    """An observation or a release; an apply is its role's ordered host steps, composed here in the same order."""
    host_request(request, removal=operation in ("destroy", "observe-remove"))
    cluster = request["cluster"]
    ceph_native.require(operation in ("apply", "observe", "destroy", "observe-remove"))
    ceph_native.require(ceph_native.digest(request_digest))
    host = next(item for item in cluster["hosts"] if item["name"] == request["host"])
    if host.get("physical") and host.get("retainRoot") and operation in ("destroy", "observe-remove"):
        phase = "physical_host_remove" if operation == "destroy" else "physical_host_observe"
        if operation == "destroy":
            transport.wait_for_guest(host)
        result = transport.call(host, phase)
        ceph_native.require(isinstance(result, dict) and type(result.get("removed")) is bool, "evidence")
        ceph_native.require(operation != "destroy" or result["removed"], "not-removed")
        observation = dict(released(request, request_digest), removed=result["removed"])
        if result["removed"]:
            observation["nativeRemoved"] = True
        return {"changed": result.get("changed", False), "evidence": ceph_evidence.host_evidence(observation, request_digest, request)}
    if operation == "destroy":
        return {"changed": False, "evidence": ceph_evidence.host_evidence(released(request, request_digest), request_digest, request)}
    if operation == "apply":
        changed, plan_digest, seconds = False, "", request["imagesSeconds"]
        pulls = [CEPH_IMAGE_GROUP] + [item["group"] for item in request["pulls"]]
        steps = [(step, None) for step in HOST_STEPS[:1]] + [("images", group) for group in pulls] + [(step, None) for step in HOST_STEPS[2:-1]]
        for step, group in steps:
            options = {"pull": group, "seconds": seconds} if step == "images" else {}
            result = host_step(request, step, request_digest, transport, **options)
            changed = result["changed"] or changed
            plan_digest = result.get("planDigest", plan_digest)
            seconds = result.get("seconds", seconds)
        verified = host_step(request, "verify", request_digest, transport, plan_digest=plan_digest)
        return {"changed": changed, "evidence": verified["evidence"]}
    host = next(item for item in cluster["hosts"] if item["name"] == request["host"])
    if ceph_host_runtime.observes_fips(cluster, host):
        observed = transport.call(host, "fips_observe")
        ceph_native.require(ceph_native.fips_proved(observed), "fips-not-enabled")
    observed = transport.call(host, "host_observe")
    ceph_native.require(isinstance(observed, dict), "evidence")
    observation = ceph_evidence.host_evidence(dict(observed, request=request_digest), request_digest, request)
    return {"changed": False, "evidence": recorded(observation, transport, request_digest, request)}


SMB_EVIDENCE_VERSION = ceph_smb_native.EVIDENCE_VERSION
SMB_EVIDENCE = frozenset(("daemons", "exists", "fsid", "owned", "owner", "partial", "ready", "removed", "request", "resource", "version"))
SMB_FSID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\Z")


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
        ceph_native.require(all(isinstance(daemon[key], str) for key in daemon) and ceph_evidence.UNIT_DAEMON.fullmatch(daemon["name"]) is not None, "evidence")
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
