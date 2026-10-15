"""Controller-local Ceph coordinator and isolated SSH material transport."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import inspect
import ipaddress
import json
import os
import shlex
import socket
import ssl
import tempfile
import time

from ansible_collections.bootwright.core.plugins.module_utils import (
    adapter_protocol, ceph_crypto_policy, ceph_host_route_probe, ceph_host_runtime, ceph_host_worker, ceph_native, ceph_native_resources, ceph_native_observe,
    ceph_native_worker, ceph_package_sources, ceph_package_worker, ceph_packages, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody,
    native_apply, native_diagnostics, native_specializations, native_resolution, native_signatures, ceph_pull_broker,
)

READINESS_TIMEOUT = 900
READINESS_INTERVAL = 5
WORKER_CEILINGS = (640, 1920, 4285)
# The bootstrap call's own launcher ceiling and worker budget; the cluster block's Go deadline (cephDeadline) adds the call.
BOOTSTRAP_CEILING = 1920
BOOTSTRAP_BUDGET = 1800
BOOTSTRAP_CALL = BOOTSTRAP_BUDGET + 60
NATIVE_FAILURES = ("native-unknown", "native-refusal", "native-timeout")
# The refusals each block's Go caller remedies by name (hostRefusals and
# adapterRefusals in internal/storage/ceph); a test holds them equal.
HOST_REASONS = (
    "unsupported-release", "image-pull", "image-platform", "image-profile", "route-probe", "device-identity", "runtime-profile", "not-ready",
    "package-resolution", "package-signature", "package-transaction", "request", "artifact", "ownership", "evidence", "material", "packages",
    "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "foreign-cluster", "native-command", "native-timeout", "native-output",
    "unknown", "transport", "remote-refusal", "adapter-exception",
)
CLUSTER_REASONS = (
    "unsupported-release", "runtime-profile", "device-identity", "cache-unproved", "dashboard-identity", "dashboard-drift", "dashboard-policy",
    "dashboard-verify", "pool-identity", "client-quiescence-unqualified", "native-option-undeclared", "call-home-unqualified",
    "dashboard-manager",
    "request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license",
    "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready",
    "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport",
    "remote-refusal", "adapter-exception", "foreign-pool", "foreign-profile", "foreign-rule", "crush-capacity", "pg-budget",
    "foreign-rbd-image", "rbd-image-identity",
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
    """A blocking health check code, or a daemon a host has not yet running."""
    return isinstance(value, str) and (ceph_native.HEALTH_CHECK.fullmatch(value) is not None or ceph_native.PENDING_DAEMON.fullmatch(value) is not None)


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
    detail = ceph_native_worker.cause(error)
    if isinstance(error, ceph_native.Refusal):
        return (str(error) if str(error) in reasons else "unknown"), detail
    return "adapter-exception", detail


def evidence(value, request_digest, request=None):
    """The closed public projection, checked before any result publication."""
    if isinstance(value, dict) and value.get("version") == ceph_host_runtime.EVIDENCE_VERSION:
        return host_evidence(value, request_digest, request)
    fields = {"version", "request", "action", "fsid", "owner", "owned", "exists", "ready", "removed", "partial", "health", "hosts", "monitors", "monAddrs",
              "mgrActive", "mgrStandbys", "osds", "resource", "client", "dashboard"}
    ceph_native.require(isinstance(value, dict) and set(value) == fields, "evidence")
    ceph_native.require(value["version"] == ceph_native.EVIDENCE_VERSION and value["request"] == request_digest and ceph_native.digest(request_digest),
                        "evidence")
    dashboard_evidence(value)
    ceph_native.require(value["action"] in ("cluster", "pool", "client", "policy", "rbd"), "evidence")
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
    ceph_native.require(resource["path"] == "", "evidence")
    ceph_native.require(resource["id"] == "" or value["action"] in ("pool", "policy") and ceph_native.RESOURCE_ID.fullmatch(resource["id"]) is not None
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


def ssh_argv(host, key, known_hosts, source, worker_budget=None, *, worker_ceiling=640):
    """SSH remote execution requires one string; quote only fixed worker code."""
    options = {
        "UserKnownHostsFile": known_hosts,
        "GlobalKnownHostsFile": "/dev/null",
        "KnownHostsCommand": "none",
        "VerifyHostKeyDNS": "no",
        "UpdateHostKeys": "no",
        "CheckHostIP": "no",
        "StrictHostKeyChecking": "yes",
        "IdentitiesOnly": "yes",
        "PasswordAuthentication": "no",
        "KbdInteractiveAuthentication": "no",
        "GSSAPIAuthentication": "no",
        "HostbasedAuthentication": "no",
        "IdentityAgent": "none",
        "ForwardAgent": "no",
        "ProxyCommand": "none",
        "ProxyJump": "none",
        "BatchMode": "yes",
        "ConnectTimeout": "10",
        "ServerAliveInterval": "15",
        "ServerAliveCountMax": "3",
    }
    argv = ["/usr/bin/ssh", "-T", "-F", "/dev/null", "-i", key, "-p", str(host["port"])]
    for name, value in options.items():
        argv.extend(["-o", name + "=" + value])
    python = ["/usr/bin/sudo", "-n", "/usr/bin/python3", "-I", "-c", source]
    if worker_budget is not None:
        ceph_native.require(worker_ceiling in WORKER_CEILINGS and type(worker_budget) in (int, float) and 0 < worker_budget <= worker_ceiling, "transport")
        python.append(str(worker_budget))
    argv.extend(["--", host["user"] + "@" + host["address"], shlex.join(python)])
    return argv


class Transport:
    """No material value appears in arguments, environment, errors or logs."""

    def __init__(self, request, material, run=ceph_native.bounded_command, *, scope=False):
        self.request = ceph_native.validate_scope(request) if scope else ceph_native.validate(request)
        self.material = material
        self.run = run

    def _call(self, host, source, payload, timeout, worker_budget=None, *, worker_ceiling=640, failures=NATIVE_FAILURES):
        ceph_native.require(isinstance(self.material, dict), "material")
        package_material = {"packageCA" + str(i) for i in range(len(self.request.get("packages", {}).get("sources", [])))}
        ceph_native.require(set(self.material) <= {"sshKey", "knownHosts", "clusterPrivateKey", "clusterPublicKey", "registryJSON"} | package_material,
                            "material")
        ceph_native.require(set(self.material) >= {"sshKey", "knownHosts"}, "material")
        with tempfile.TemporaryDirectory(prefix="bootwright-ceph-") as scratch:
            key = os.path.join(scratch, "identity")
            known = os.path.join(scratch, "known_hosts")
            ceph_native.private_write(key, ceph_native.read_regular(self.material["sshKey"], private=True))
            ceph_native.private_write(known, ceph_native.read_regular(self.material["knownHosts"], private=True))
            argv = ssh_argv(host, key, known, source, worker_budget, worker_ceiling=worker_ceiling)
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
        ceph_native.require(set(result) == {"ok", "result"} and isinstance(result["result"], dict), "transport")
        return result["result"]

    @staticmethod
    def native_sources():
        modules = (native_diagnostics, native_specializations, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
                   ceph_native_resources, ceph_host_route_probe, ceph_pull_broker, ceph_host_runtime)
        sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}
        ceph_native.require(tuple(sources) == ceph_native_worker.MODULES, "transport")
        return sources

    def call(self, host, phase, private=None, *, timeout=660, devices=None, service=None):
        ceph_native.require(type(timeout) in (int, float) and 0 < timeout <= 660, "transport")
        ceph_native.require(phase != "profile" and (devices is None or phase == "services"), "transport")
        ceph_native.require(service is None or phase == "services" and type(service) is int and 0 <= service < len(self.request["services"]), "transport")
        budget = min(640, timeout - 2) if timeout > 2 else timeout / 2
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
        source = ceph_native_worker.launcher(inspect.getsource(ceph_native_worker)) + "s['main']()"
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": budget}, timeout,
                          worker_budget=budget)

    def host_profile(self, host):
        """The one long call: pull the frozen digest, then prove its contained runtime."""
        ceph_native.require("registryJSON" in self.material, "material")
        try:
            registry = ceph_native.read_regular(self.material["registryJSON"], private=True).decode("utf-8")
        except UnicodeError:
            raise ceph_native.Refusal("material") from None
        payload = {"request": self.request, "host": host["name"], "phase": "profile", "private": {"registryJSON": registry}}
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
        modules = (native_diagnostics, ceph_native_worker, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
                   native_resolution, native_signatures, native_apply, ceph_package_sources, ceph_crypto_policy, ceph_packages)
        sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}
        result = self._call(host, inspect.getsource(ceph_package_worker),
                            {"sources": sources, "payload": {"request": self.request, "host": host["name"], "private": {"certificate": certificate}}}, 1980,
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
    for host in request["hosts"]:
        transport.call(host, "devices")
    changed = False
    private = transport.bootstrap_material()
    for host in request["hosts"]:
        if stages[host["name"]] == "unclaimed":
            changed = transport.call(host, "claim")["changed"] or changed
    for host in request["hosts"]:
        if stages[host["name"]] in ("unclaimed", "claimed"):
            authorization = {name: private[name] for name in ("clusterPublicKey", "registryJSON", "clusterPrivateKey", "knownHosts")}
            changed = transport.call(host, "authorize", authorization)["changed"] or changed
    ssh = {name: private[name] for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")}
    if fresh:
        changed = transport.call(bootstrap, "bootstrap", ssh)["changed"] or changed
    if fresh or stage == "seed":
        changed = transport.call(bootstrap, "tunnel_configure", ssh)["changed"] or changed
    if stage not in ("mounted", "enrolling"):
        changed = transport.call(bootstrap, "tunnel_mount")["changed"] or changed
    for host in [bootstrap] + [host for host in request["hosts"] if host != bootstrap]:
        changed = transport.call(host, "cache")["changed"] or changed
    changed = transport.call(bootstrap, "enroll")["changed"] or changed
    for host in request["hosts"]:
        transport.call(host, "devices")
    devices = device_identities(request, transport)
    for index, service in enumerate(request["services"]):
        if service["service_type"] == "osd":
            names = set(service["placement"]["hosts"])
            for host in request["hosts"]:
                if host["name"] in names:
                    changed = transport.call(host, "osd_prepare")["changed"] or changed
        changed = transport.call(bootstrap, "services", devices=devices, service=index)["changed"] or changed
    return changed


def device_identities(request, transport):
    """Every host's retained SDK identities, in declared host and device order."""
    result = []
    for host in request["hosts"]:
        value = transport.call(host, "host_devices")
        ceph_native.require(isinstance(value, dict) and set(value) == {"devices"}, "device-identity")
        result.extend(ceph_host_runtime.rows(request, host["name"], value["devices"]))
    return result


def execute(request, operation, request_digest, transport, outputs=None, *, now=time.monotonic, pause=time.sleep, connect=tls_certificate):
    """Keep read-only observation distinct from any preparation or mutation."""
    ceph_native.validate(request)
    ceph_native.require(operation in ("apply", "observe", "destroy"))
    ceph_native.require(ceph_native.digest(request_digest))
    if operation != "observe":
        ceph_native.require(ceph_native.supported_transport(request), "unsupported-release")
    bootstrap = next(h for h in request["hosts"] if h["name"] == request["bootstrap"])
    outputs = outputs or {}
    ceph_native.require(set(outputs) <= {"client", "dashboard"})
    if request["action"] in ("client", "cluster") and operation in ("apply", "observe"):
        ceph_native.require(set(outputs) == {"client" if request["action"] == "client" else "dashboard"}, "material")
    elif operation in ("apply", "observe"):
        ceph_native.require(not outputs, "material")
    changed = False
    if operation == "apply":
        preparations = [transport.call(host, "prerequisites") for host in request["hosts"]]
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
                    for host in request["hosts"]:
                        transport.call(host, "devices")
                    changed = transport.call(bootstrap, "services")["changed"] or changed
            elif request["action"] == "pool":
                changed = transport.call(bootstrap, "pool")["changed"] or changed
            elif request["action"] == "policy":
                changed = transport.call(bootstrap, "policy")["changed"] or changed
            elif request["action"] == "rbd":
                changed = transport.call(bootstrap, "rbd")["changed"] or changed
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
    return {"changed": changed, "evidence": observation}


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
    ceph_native.require(isinstance(value, dict) and set(value) == {"cluster", "host", "version"} and value["version"] == "ceph-host-v2")
    cluster = ceph_native.validate_scope(value["cluster"])
    ceph_native.require("packages" in cluster)
    ceph_native.require(sum(host["name"] == value["host"] for host in cluster["hosts"]) == 1)
    ceph_native.require(sum(source["host"] == value["host"] for source in cluster["packages"]["sources"]) == 1)
    return value


def host_evidence(value, request_digest, request=None):
    """The closed host block projection; no credential, path or vendor text."""
    fields = {"version", "request", "host", "owner", "fsid", "image", "ready", "removed", "imageID", "uid", "gid", "label", "policySHA256",
              "cephadmSHA256", "planDigest", "route", "devices"}
    ceph_native.require(isinstance(value, dict) and set(value) == fields, "evidence")
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
        ceph_native.require(isinstance(device, dict) and set(device) == {"path", "serial", "deviceID"}, "evidence")
        ceph_native.require(all(isinstance(device[key], str) for key in device) and ceph_host_runtime.DEVICE_ID.fullmatch(device["deviceID"]) is not None,
                            "evidence")
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
    return {"changed": changed, "evidence": observation}
