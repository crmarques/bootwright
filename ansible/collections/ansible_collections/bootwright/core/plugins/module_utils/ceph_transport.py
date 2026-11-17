"""The controller's isolated SSH transport to the fixed Ceph workers: material admission, the session argv and the shipped bundle."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import functools
import inspect
import json
import os
import tempfile
import threading

from ansible_collections.bootwright.core.plugins.module_utils import (
    bounded_process, canonical_json, ceph_admin_credentials_native, ceph_crypto_policy, ceph_evidence, ceph_export_dns, ceph_export_native,
    ceph_host_device_probe, ceph_host_image_probe, ceph_host_label_probe, ceph_host_route_probe, ceph_host_runtime, ceph_host_worker,
    ceph_monitoring_template, ceph_native, ceph_native_bootstrap, ceph_native_dashboard,
    ceph_native_nfs, ceph_native_nvmeof, ceph_native_object, ceph_native_observability, ceph_native_observe, ceph_native_osd, ceph_native_registry,
    ceph_native_resources, ceph_native_stretch, ceph_native_units, ceph_native_validate, ceph_native_worker, ceph_package_sources,
    ceph_package_worker, ceph_packages, ceph_pull_broker, ceph_registry_discovery, ceph_replication_native, ceph_smb_native, ceph_ssh_policy,
    ceph_tunnel, ceph_tunnel_custody, ceph_tunnel_staging, held_directory, native_apply, native_diagnostics, native_resolution, native_signatures,
    native_specializations, physical_erase_worker, physical_machine_identity, private_files, registry_auth, remote_command, tls_trust,
    transport_faults,
)


WORKER_CEILINGS = (640, 1920, 21600)


# The bootstrap call's own launcher ceiling and worker budget; the cluster block's Go deadline (cephDeadline) adds the call.
BOOTSTRAP_CEILING = 1920


BOOTSTRAP_BUDGET = 1800


BOOTSTRAP_CALL = BOOTSTRAP_BUDGET + 60


NATIVE_FAILURES = ("native-unknown", "native-refusal", "native-timeout")


def bootstrap_launcher(supervisor_source, host_source):
    """The long host launcher's fixed code, admitting only the bootstrap phase under the bootstrap ceiling."""
    return (ceph_native_worker.launcher(supervisor_source)
            + "f={'__name__':'fixed_ceph_host'};exec(compile(" + repr(host_source) + ",'<fixed-ceph-host>','exec'),f);"
            "f.update(REMOTE_LIMIT=" + str(BOOTSTRAP_CEILING) + ",PHASE='bootstrap');f['main'](s)")


def ssh_argv(host, key, known_hosts, source, worker_budget=None, *, worker_ceiling=640, frozen_ceiling=False, ssh_config, host_key_algorithms):
    """SSH remote execution requires one string; quote only fixed worker code."""
    argv = ceph_ssh_policy.session_argv(ssh_config, known_hosts, host_key_algorithms, key, host["port"])
    arguments = ()
    if worker_budget is not None:
        fixed = worker_ceiling in WORKER_CEILINGS or frozen_ceiling and type(worker_ceiling) is int and 0 < worker_ceiling <= max(WORKER_CEILINGS)
        ceph_native.require(fixed and type(worker_budget) in (int, float) and 0 < worker_budget <= worker_ceiling, "transport")
        arguments = (str(worker_budget),)
    argv.extend(["--", host["user"] + "@" + host["address"], remote_command.fixed_worker(source, *arguments)])
    return argv


@functools.cache
def shipped_native_sources():
    modules = (native_diagnostics, native_specializations, canonical_json, ceph_monitoring_template, bounded_process, tls_trust,
               private_files, registry_auth, held_directory, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
               ceph_native_resources, ceph_native_stretch, ceph_native_nfs, ceph_native_nvmeof, ceph_native_observability,
               ceph_native_units, ceph_native_object, ceph_smb_native,
               ceph_replication_native, ceph_export_dns, ceph_export_native,
               ceph_native_registry, ceph_native_osd, ceph_native_dashboard, ceph_native_bootstrap, ceph_native_validate,
               ceph_host_device_probe, ceph_host_image_probe, ceph_host_label_probe, ceph_host_route_probe, ceph_pull_broker,
               physical_machine_identity, physical_erase_worker,
               ceph_host_runtime, ceph_admin_credentials_native)
    sources = {module.__name__.rsplit(".", 1)[-1]: inspect.getsource(module) for module in modules}
    ceph_native.require(tuple(sources) == ceph_native_worker.MODULES, "transport")
    return tuple(sources.items())


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

    def _call(self, host, source, payload, timeout, worker_budget=None, *, worker_ceiling=640, frozen_ceiling=False, failures=NATIVE_FAILURES):
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
            private_files.write(key, ceph_native.read_regular(self.material["sshKey"], private=True))
            private_files.write(known, known_bytes)
            private_files.write(config, self.ssh_policy()["configuration"].encode())
            argv = ssh_argv(host, key, known, source, worker_budget, worker_ceiling=worker_ceiling, frozen_ceiling=frozen_ceiling,
                            ssh_config=config, host_key_algorithms=algorithms)
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
            ceph_native.require(ceph_evidence.specialization_ids(result["specializations"]), "transport")
            with self.applied_lock:
                self.applied.update(result["specializations"])
        return result["result"]

    @staticmethod
    def native_sources():
        return dict(shipped_native_sources())

    def call(self, host, phase, private=None, *, timeout=660, devices=None, service=None, names=None, orphans=None):
        nvme_long = (self.request.get("action") == "nvmeof" and phase in ("nvmeof", "nvmeof_observe")
                     or nvmeof_unit(self.request) and phase == "nvmeof_unit_observe")
        ceiling = ceph_native_nvmeof.phase_budget(self.request, phase) if nvme_long else 640
        ceph_native.require(type(timeout) in (int, float) and 0 < timeout <= ceiling + 20 and ceiling <= 21600, "transport")
        ceph_native.require(phase != "images" and (devices is None or phase == "services"), "transport")
        ceph_native.require(service is None or phase == "services" and type(service) is int and 0 <= service < len(self.request["services"]), "transport")
        ceph_native.require(orphans is None or phase == "services" and isinstance(orphans, list) and orphans, "transport")
        ceph_native.require((names is not None) == (phase == "profile") and (names is None or isinstance(names, list)), "transport")
        budget = min(ceiling, timeout - 2) if timeout > 2 else timeout / 2
        payload = {"request": self.request, "host": host["name"], "phase": phase, "private": private or {}}
        if names is not None:
            payload["names"] = names
        if devices is not None:
            payload["devices"] = devices
        if service is not None:
            payload["service"] = service
        if orphans is not None:
            payload["orphans"] = orphans
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

    def host_images(self, host, names, seconds, ceiling):
        """One long pull call under the share ``seconds`` of the frozen images budget ``ceiling``: the Ceph digest, or a batch of auxiliary digests."""
        ceph_native.require(isinstance(names, list) and type(ceiling) is int and type(seconds) is int and 0 < seconds <= ceiling, "transport")
        materials = ("registryJSON", "registryCA") if "registryTrustBundleRef" in self.request else ("registryJSON",)
        ceph_native.require(all(name in self.material for name in materials), "material")
        try:
            private = {name: ceph_native.read_regular(self.material[name], private=True).decode("utf-8") for name in materials}
        except UnicodeError:
            raise ceph_native.Refusal("material") from None
        if "registryCA" in private:
            private["registryCA"] = self.trust_bundle(private["registryCA"])
        payload = {"request": self.request, "host": host["name"], "phase": "images", "private": private,
                   "pull": {"names": names, "seconds": seconds, "ceiling": ceiling}}
        source = ceph_host_worker.launcher(inspect.getsource(ceph_native_worker), inspect.getsource(ceph_host_worker), ceiling)
        return self._call(host, source, {"sources": self.native_sources(), "payload": payload, "budget": seconds}, seconds + 60,
                          worker_budget=seconds, worker_ceiling=ceiling, frozen_ceiling=True)

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
        modules = (native_diagnostics, canonical_json, bounded_process, tls_trust, ceph_native_worker,
                   private_files, registry_auth, held_directory, ceph_tunnel, ceph_tunnel_staging, ceph_tunnel_custody, ceph_native_observe, ceph_native,
                   ceph_native_stretch, ceph_native_validate, native_resolution, native_signatures, native_apply, transport_faults,
                   ceph_package_sources,
                   ceph_crypto_policy, ceph_packages)
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


def nvmeof_unit(request):
    return request.get("action") == "service" and request.get("service", {}).get("type") == "nvmeof"
