"""Native dashboard: its credential and TLS pair custody, port repair, redeploy and observation."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import errno
import hashlib
import hmac
import importlib
import json
import re

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    Custody, DASHBOARD_BUDGET, DASHBOARD_CERTIFICATE, DASHBOARD_INTERVAL, DASHBOARD_PORT, DASHBOARD_URL, DASHBOARD_USER, ManagerRefusal, Refusal,
    certificate_sha256, daemon_identity_host, dashboard_decode, dashboard_tls_decode, require,
)


class Dashboard:
    """Native dashboard: its credential and TLS pair custody, port repair, redeploy and observation."""

    def __init__(self, worker):
        self.worker = worker

    def dashboard_scope(self):
        """Bind the dashboard credential to the owned cluster incarnation and its bootstrap host."""
        require(self.worker.request["action"] == "cluster" and self.worker.markers.owned() and self.worker.host["name"] == self.worker.request["bootstrap"],
                "ownership")
        require(self.worker.ceph("fsid") == {"fsid": self.worker.request["fsid"]}, "ownership")
        return {"owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"], "host": self.worker.host["name"], "user": DASHBOARD_USER}

    def dashboard_store(self):
        return Custody(self.worker, "dashboard", "", 64, dashboard_decode)

    def ceph_status(self, *args, data=b""):
        """Only the exit status: no native output, and no input it echoes, leaves this call."""
        with self.worker.bootstrap_hold():
            code, unused = self.worker.run([*self.worker.admin_argv("/usr/bin/ceph"), *args], data=data, timeout=60)
        return code

    def dashboard_settings(self):
        """Enable the module with TLS on the fixed port; repeat nothing already in place."""
        changed = False
        modules = self.worker.ceph("mgr", "module", "ls")
        require(isinstance(modules, dict) and isinstance(modules.get("enabled_modules"), list) and len(modules["enabled_modules"]) <= 128, "native-output")
        if "dashboard" not in modules["enabled_modules"]:
            self.worker.ceph("mgr", "module", "enable", "dashboard", json_result=False)
            changed = True
        observed = self.worker.ceph("config", "dump")
        require(isinstance(observed, list) and all(isinstance(entry, dict) for entry in observed), "native-output")
        current = {entry.get("name"): entry.get("value") for entry in observed if entry.get("section") == "mgr"}
        for name, value in (("mgr/dashboard/ssl", "true"), ("mgr/dashboard/ssl_server_port", str(DASHBOARD_PORT))):
            if current.get(name) != value:
                self.worker.ceph("config", "set", "mgr", name, value, json_result=False)
                changed = True
        return changed

    def dashboard_certificate(self):
        """The SHA-256 of the stored certificate's DER bytes, or empty while none is stored."""
        keys = self.worker.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 65536 and all(isinstance(key, str) for key in keys), "native-output")
        if DASHBOARD_CERTIFICATE not in keys:
            return ""
        return certificate_sha256(self.worker.ceph_text("config-key", "get", DASHBOARD_CERTIFICATE))

    def dashboard_tls_private(self, *args, data=b""):
        """Private native output never enters NativeFailure, even when a command fails."""
        with self.worker.bootstrap_hold():
            code, output = self.worker.run([*self.worker.admin_argv("/usr/bin/ceph"), *args], data=data, timeout=60)
        require(code == 0, "native-command")
        require(isinstance(output, bytes) and len(output) <= 65536, "native-output")
        return output

    def dashboard_tls_keys(self):
        keys = self.worker.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 65536 and all(isinstance(key, str) for key in keys), "native-output")
        selected = {key for key in keys if key.startswith("mgr/dashboard/") and key.rsplit("/", 1)[-1] in ("crt", "key")}
        require(selected <= {"mgr/dashboard/crt", "mgr/dashboard/key"}, "dashboard-tls-identity")
        return selected

    def dashboard_gateway_certify(self, scope):
        """Retain a cephadm-signed client identity before installing it; never adopt or reset native TLS."""
        store = Custody(self.worker, "dashboard-tls", "", 65536, dashboard_tls_decode)
        keys = self.dashboard_tls_keys()
        attempted = self.worker.markers.read(store.name("attempt"))
        require(not keys or attempted is not None, "dashboard-tls-identity")
        pair = store.prepare(scope, lambda: self.dashboard_tls_private("orch", "certmgr", "generate-certificates", "dashboard"))
        binding = store.binding(scope, "ceph-dashboard-tls-v1")
        applied = self.worker.markers.read(store.name("applied"))
        require(attempted is None or canonical(attempted) == canonical(binding), "dashboard-tls-identity")
        require(applied is None or attempted is not None and canonical(applied) == canonical(binding), "dashboard-tls-identity")
        # Prove the whole observed pair before any setter: a matching half cannot authorize overwriting the other half.
        for part, name in (("key", "key"), ("cert", "crt")):
            key = "mgr/dashboard/" + name
            require(key in keys or applied is None, "dashboard-tls-identity")
            if key in keys:
                observed = self.dashboard_tls_private("config-key", "get", key)
                require(hmac.compare_digest(observed, pair[part].encode("utf-8")), "dashboard-tls-identity")
        self.worker.markers.write(store.name("attempt"), binding)
        changed = False
        for part, name, command in (("key", "key", "set-ssl-certificate-key"), ("cert", "crt", "set-ssl-certificate")):
            if "mgr/dashboard/" + name in keys:
                continue
            require(store.material(scope) == pair, "dashboard-tls-identity")
            with self.worker.manager_served():
                self.dashboard_tls_private("dashboard", command, "-i", "-", data=pair[part].encode("utf-8"))
            changed = True
        require(self.dashboard_tls_keys() == {"mgr/dashboard/crt", "mgr/dashboard/key"}, "dashboard-tls-identity")
        for part, name in (("key", "key"), ("cert", "crt")):
            require(hmac.compare_digest(self.dashboard_tls_private("config-key", "get", "mgr/dashboard/" + name),
                                        pair[part].encode("utf-8")), "dashboard-tls-identity")
        self.worker.markers.write(store.name("applied"), binding)
        return changed

    def dashboard_certify(self, deadline):
        """Create Ceph's self-signed certificate only while none is stored, waiting for the module to load."""
        while True:
            fingerprint = self.dashboard_certificate()
            if fingerprint:
                return fingerprint
            try:
                # Enabling the module restarts the manager; until it serves the module again its command fails or hangs.
                with self.worker.manager_served():
                    code = self.worker.ceph_status("dashboard", "create-self-signed-cert")
            except ManagerRefusal:
                code = None
            if code != 0:
                require(self.worker.clock() < deadline, "native-timeout")
                self.worker.pause(DASHBOARD_INTERVAL)

    def manager_containers(self, refresh_times=None):
        """Refreshed declared MGR containers, running state and dashboard port publication."""
        daemons = self.manager_daemons(refresh_times=refresh_times)
        return {daemon: (container, up, DASHBOARD_PORT in ports) for daemon, (container, up, ports) in daemons.items()}

    def manager_daemons(self, refresh=True, refresh_times=None):
        """Declared MGR containers, running state and deployment ports from cephadm's daemon cache. A refresh asks cephadm to reread every
        host's daemons, which only a later read shows."""
        with self.worker.manager_served():
            rows = self.worker.ceph("orch", "ps", "--daemon_type", "mgr", *(("--refresh",) if refresh else ()))
        require(isinstance(rows, list) and len(rows) <= 128, "native-output")
        managers = next(s["placement"]["hosts"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
        result = {}
        for row in rows:
            require(isinstance(row, dict) and row.get("daemon_type") == "mgr" and isinstance(row.get("daemon_id"), str), "native-output")
            require(daemon_identity_host("mgr", row["daemon_id"], managers) == row.get("hostname") and row["daemon_id"] not in result, "foreign-service")
            container = row.get("container_id")
            require(container is None or isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container) is not None, "native-output")
            ports = row.get("ports", [])
            require(isinstance(ports, list) and len(ports) <= 128
                    and all(type(port) is int and 1 <= port <= 65535 for port in ports), "native-output")
            if refresh_times is not None and row.get("last_refresh") is not None:
                refresh_times[row["daemon_id"]] = ceph_native.monmap_time(row["last_refresh"])
            result[row["daemon_id"]] = (container or "", type(row.get("status")) is int and row["status"] == 1, tuple(ports))
        require(len({row["hostname"] for row in rows}) == len(rows), "foreign-service")
        containers = [container for container, unused_up, unused_ports in result.values() if container]
        require(len(set(containers)) == len(containers), "foreign-service")
        return result

    def dashboard_published(self):
        try:
            with self.worker.manager_served():
                services = self.worker.ceph("mgr", "services")
        except ManagerRefusal:
            return False
        require(isinstance(services, dict), "native-output")
        url = services.get("dashboard", "")
        return isinstance(url, str) and DASHBOARD_URL.fullmatch(url) is not None

    def dashboard_wait_publication(self, deadline):
        while True:
            require(self.worker.clock() < deadline, "native-timeout")
            if self.worker.dashboard_published():
                return
            self.worker.pause(DASHBOARD_INTERVAL)

    def dashboard_schedule_redeploy(self, record, count, deadline):
        """Schedule once every declared MGR runs its recorded container; a container that left the record schedules nothing."""
        while True:
            self.dashboard_wait_publication(deadline)
            running = self.worker.manager_retry(self.worker.manager_containers)
            if any(container and container not in record["containers"] for container, unused_up, unused_port in running.values()):
                return False
            ready = len(running) == count and all(container and up for container, up, unused_port in running.values())
            if ready and self.worker.dashboard_published():
                require(self.worker.clock() < deadline, "native-timeout")
                self.worker.tunnel_effect("orch", "redeploy", "mgr", json_result=False)
                return True
            self.worker.pause(DASHBOARD_INTERVAL)

    def dashboard_original_action(self, daemon, container):
        """The selected original's pending action, or None when the daemon is no longer that running container."""
        placement = next(s["placement"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
        host = daemon_identity_host("mgr", daemon, placement["hosts"])
        key = "mgr/cephadm/host." + host.lower()
        # IBM cephadm splits a host's JSON across numbered keys. One prefix read excludes device records and other hosts.
        try:
            parts = self.worker.ceph("config-key", "dump", key + ";")
            require(isinstance(parts, dict), "native-output")
            if parts:
                keys = [key + ";" + str(index) for index in range(len(parts))]
                require(set(keys) == set(parts) and all(isinstance(parts[k], str) for k in keys), "native-output")
                cache = json.loads("".join(parts[k] for k in keys), object_pairs_hook=ceph_native.unique_object)
            else:
                cache = self.worker.ceph("config-key", "get", key)
        except ceph_native.NativeFailure:
            raise ceph_native.NativeFailure("native-command", "Ceph dashboard redeploy", "cannot read cephadm host cache") from None
        except (json.JSONDecodeError, RecursionError):
            raise Refusal("native-output") from None
        require(isinstance(cache, dict) and isinstance(cache.get("daemons"), dict)
                and isinstance(cache.get("scheduled_daemon_actions", {}), dict), "native-output")
        name = "mgr." + daemon
        row = cache["daemons"].get(name)
        if row is None:
            return None
        require(isinstance(row, dict) and row.get("daemon_type") == "mgr" and row.get("daemon_id") == daemon
                and row.get("hostname") == host, "foreign-service")
        if type(row.get("status")) is not int or row["status"] != 1 or row.get("container_id") != container:
            return None
        actions = cache.get("scheduled_daemon_actions", {})
        require(name not in actions or isinstance(actions[name], str) and actions[name], "native-output")
        return actions.get(name, "")

    def dashboard_redeploy_next(self, running, missing, submitted, failed_over, deadline, intents):
        """Redeploy a declared standby, or hand off the last active target before reobserving publication and containers."""
        if not missing - submitted:
            return False
        manager = self.worker.ceph("mgr", "dump")
        require(isinstance(manager, dict) and isinstance(manager.get("active_name"), str)
                and type(manager.get("active_gid")) is int and isinstance(manager.get("standbys"), list)
                and all(isinstance(row, dict) and isinstance(row.get("name"), str) for row in manager["standbys"]), "native-output")
        active = manager["active_name"]
        if manager.get("available") is not True or active not in running:
            return False
        require(manager["active_gid"] > 0, "native-output")
        standbys = {row["name"] for row in manager["standbys"]}
        require(standbys <= running.keys() and active not in standbys, "foreign-service")
        targets = (missing - submitted) & standbys
        for target in sorted(targets, key=lambda daemon: (daemon in intents and intents[daemon][2] is not None, daemon)):
            with self.worker.transport_hold(), self.worker.cache_hold():
                if not intents and self.dashboard_original_action(target, running[target][0]) != "":
                    continue
                require(self.worker.clock() < deadline, "native-timeout")
                if intents:
                    filename, intent, unused_attempt = intents[target]
                    self.worker.markers.write(filename, intent)
                self.worker.ceph("orch", "daemon", "redeploy", "mgr." + target, json_result=False)
            submitted.add(target)
            return True
        if missing == {active} and active not in failed_over:
            require(len(running) >= 2, "dashboard-manager")
            if standbys and (intents or self.dashboard_original_action(active, running[active][0]) in ("", "redeploy")):
                require(self.worker.clock() < deadline, "native-timeout")
                failed_over.add(active)
                self.worker.ceph("mgr", "fail", str(manager["active_gid"]), json_result=False)
                try:
                    self.worker.manager_wait(previous=manager["active_gid"])
                except Refusal as error:
                    if str(error) != "native-timeout":
                        raise
                    raise ceph_native.NativeFailure("native-timeout", "Ceph dashboard redeploy", "new active manager or orchestrator unavailable") from None
                return True
        return False

    def dashboard_repair_ports(self, scope, running, count, deadline):
        """One frozen repair pass. A target still running its recorded container without the port is requested at most once per
        call, standbys and then unattempted targets first: cephadm coalesces a pending redeploy, so a repeat restarts it at most once.
        The active MGR is failed over first, since cephadm would prepare its redeploy from the services map its failover clears, and
        only once every other missing MGR is seen replaced: cephadm runs a scheduled redeploy later, on whichever MGR is then active."""
        name = "dashboard-port-repair.json"
        record = self.worker.markers.read(name)
        if record is None:
            record = {"version": "ceph-dashboard-port-repair-v1", "scope": scope,
                      "containers": {daemon: row[0] for daemon, row in running.items()},
                      "missing": sorted(daemon for daemon, row in running.items() if not row[2])}
            self.worker.markers.write(name, record)
        require(isinstance(record, dict) and set(record) == {"version", "scope", "containers", "missing"}
                and record["version"] == "ceph-dashboard-port-repair-v1" and canonical(record["scope"]) == canonical(scope)
                and isinstance(record["containers"], dict) and len(record["containers"]) == count
                and all(isinstance(daemon, str) and isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container)
                        for daemon, container in record["containers"].items())
                and len(set(record["containers"].values())) == count
                and isinstance(record["missing"], list) and record["missing"]
                and all(isinstance(daemon, str) and daemon in record["containers"] for daemon in record["missing"])
                and len(set(record["missing"])) == len(record["missing"]), "dashboard-manager")
        changed, submitted, failed_over = False, set(), set()
        while True:
            intents = {}
            for daemon in record["missing"]:
                target = {"version": "ceph-dashboard-port-attempt-v1", "scope": scope,
                          "daemon": daemon, "container": record["containers"][daemon]}
                filename = "dashboard-port-" + hashlib.sha256(daemon.encode()).hexdigest() + ".json"
                attempted = self.worker.markers.read(filename)
                require(attempted is None or canonical(attempted) == canonical(target), "dashboard-manager")
                intents[daemon] = (filename, target, attempted)
            self.dashboard_wait_publication(deadline)
            try:
                running = self.worker.manager_containers()
            except ManagerRefusal:
                running = {}
            require(set(running) <= set(record["containers"]), "dashboard-manager")
            if not self.worker.dashboard_published():
                self.worker.pause(DASHBOARD_INTERVAL)
                continue
            if len(running) == count and all(container and up for container, up, unused_port in running.values()):
                missing = {daemon for daemon, row in running.items() if not row[2]}
                require(missing <= set(record["missing"]), "dashboard-manager")
                require(all(running[daemon][0] == record["containers"][daemon] for daemon in missing), "dashboard-manager")
                if not missing:
                    return changed
                if self.dashboard_redeploy_next(running, missing, submitted, failed_over, deadline, intents):
                    changed = True
                    continue
            require(self.worker.clock() < deadline, "native-timeout")
            self.worker.pause(DASHBOARD_INTERVAL)

    def dashboard_redeploy(self, scope, deadline):
        """Gate redeployment on publication and prove every running MGR advertises its TLS port."""
        name, done = "dashboard-redeploy.json", "dashboard-redeployed.json"
        placement = next(s["placement"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
        count = placement.get("count", len(placement["hosts"]))
        record = self.worker.markers.read(name)
        changed = False
        if record is None:
            running = self.worker.manager_retry(self.worker.manager_containers)
            require(len(running) == count and all(container and up for container, up, unused_port in running.values()), "not-ready")
            record = {"version": "ceph-dashboard-redeploy-v1", "scope": scope,
                      "containers": sorted(container for container, unused_up, unused_port in running.values())}
            self.worker.markers.write(name, record)
            require(self.dashboard_schedule_redeploy(record, count, deadline), "dashboard-manager")
            changed = True
        require(isinstance(record, dict) and set(record) == {"version", "scope", "containers"} and record["version"] == "ceph-dashboard-redeploy-v1"
                and canonical(record["scope"]) == canonical(scope) and isinstance(record["containers"], list)
                and len(record["containers"]) == count
                and all(isinstance(container, str) and re.fullmatch(r"[0-9a-f]{12,64}", container) for container in record["containers"])
                and len(set(record["containers"])) == count, "dashboard-manager")
        completed = self.worker.markers.read(done)
        require(completed is None or canonical(completed) == canonical(record), "dashboard-manager")
        changed = self.dashboard_wait_redeploy(scope, record, count, deadline, completed=completed is not None, scheduled=changed) or changed
        self.worker.markers.write(done, record)
        self.worker.verify_transport_bundle()
        self.worker.manager_retry(self.worker.verify_tunnel_configuration)
        self.worker.verify_manager_mounts(self.worker.daemons())
        return changed

    def dashboard_wait_redeploy(self, scope, record, count, deadline, *, completed, scheduled):
        """Wait for the service action and recover only its original containers after independent host refreshes."""
        submitted, failed_over = set(), set()
        changed = False
        first_refresh = {}
        while True:
            refresh_times = {}
            try:
                running = self.manager_containers(refresh_times) if not completed else self.worker.manager_containers()
            except ManagerRefusal:
                if self.worker.clock() >= deadline:
                    raise ceph_native.NativeFailure("native-timeout", "Ceph dashboard redeploy", "manager inventory unavailable") from None
                first_refresh.clear()
                self.worker.pause(DASHBOARD_INTERVAL)
                continue
            live = {daemon: container for daemon, (container, up, unused_port) in running.items() if container and up}
            remaining = {daemon for daemon, container in live.items() if container in record["containers"]}
            replaced = len(live) - len(remaining)
            ready = len(running) == len(live) == count
            if ready and self.worker.dashboard_published():
                repair = self.worker.markers.read("dashboard-port-repair.json")
                if not remaining:
                    if repair is not None or not all(row[2] for row in running.values()):
                        changed = self.dashboard_repair_ports(scope, running, count, deadline) or changed
                    return changed
                if not completed and len(refresh_times) == count and (replaced or not scheduled):
                    require(repair is None, "dashboard-manager")
                    # --refresh queues new observations but returns the current cache. Keep the first stamps until *every*
                    # host has refreshed the same set of container IDs, even when different hosts refresh on different polls.
                    if set(first_refresh) != set(live.items()):
                        first_refresh = {(daemon, container): refresh_times[daemon] for daemon, container in live.items()}
                    elif all(refresh_times[daemon] > stamp for (daemon, unused_container), stamp in first_refresh.items()):
                        if count == 1 and not submitted:
                            target, = remaining
                            if self.dashboard_original_action(target, live[target]) == "" and self.dashboard_schedule_redeploy(record, count, deadline):
                                submitted.update(remaining)
                                changed = True
                        elif count > 1 and self.dashboard_redeploy_next(running, remaining, submitted, failed_over, deadline, {}):
                            changed = True
                            first_refresh.clear()
                            continue
            else:
                first_refresh.clear()
            if self.worker.clock() >= deadline:
                cause = (f"{len(live)} of {count} managers running, {replaced} of {count} containers replaced"
                         if not ready or remaining else "all manager containers replaced, dashboard endpoint unpublished")
                raise ceph_native.NativeFailure("native-timeout", "Ceph dashboard redeploy", cause)
            self.worker.pause(DASHBOARD_INTERVAL)

    def dashboard_users(self):
        users = self.worker.ceph("dashboard", "ac-user-show")
        require(isinstance(users, list) and len(users) <= 4096 and all(isinstance(user, str) for user in users), "native-output")
        return users

    def dashboard_verify(self, password):
        """Ceph keeps only a bcrypt hash; it is checked here and never leaves this worker."""
        user = self.worker.ceph("dashboard", "ac-user-show", DASHBOARD_USER)
        require(isinstance(user, dict) and user.get("username") == DASHBOARD_USER and isinstance(user.get("password"), str), "native-output")
        try:
            # The pinned managed OS's Python 3.9 ships crypt over libxcrypt, which reads bcrypt.
            hasher = importlib.import_module("crypt")
            computed = hasher.crypt(password, user["password"])
        except (ImportError, OSError, ValueError, AttributeError):
            raise Refusal("dashboard-verify") from None
        require(isinstance(computed, str) and computed.startswith("$2"), "dashboard-verify")
        require(hmac.compare_digest(computed, user["password"]), "dashboard-drift")
        require(user.get("roles") == ["administrator"] and user.get("enabled") is True and user.get("pwdUpdateRequired") is False, "dashboard-drift")

    def dashboard(self):
        """Enable the dashboard after readiness and create its administrator from retained custody."""
        self.worker.verify_tool()
        scope = self.worker.dashboard_scope()
        store = self.worker.dashboard_store()
        deadline = self.worker.clock() + DASHBOARD_BUDGET
        password = store.prepare(scope, lambda: ceph_native.dashboard_password().encode("ascii"))
        binding = store.binding(scope, "ceph-dashboard-user-v1")
        attempt, applied = self.worker.markers.read(store.name("attempt")), self.worker.markers.read(store.name("applied"))
        require(attempt is None or canonical(attempt) == canonical(binding), "dashboard-identity")
        require(applied is None or canonical(applied) == canonical(binding), "dashboard-identity")
        changed = self.dashboard_settings()
        if any(entry["name"] == "mgmt-gateway" for entry in self.worker.request.get("delegated", [])):
            changed = self.worker.manager_retry(lambda: self.dashboard_gateway_certify(scope)) or changed
        elif not self.dashboard_certificate():
            self.dashboard_certify(deadline)
            changed = True
        changed = self.worker.dashboard_redeploy(scope, deadline) or changed
        present = DASHBOARD_USER in self.dashboard_users()
        require(not present or attempt is not None, "dashboard-identity")
        require(present or applied is None, "dashboard-identity")
        if not present:
            self.worker.markers.write(store.name("attempt"), binding)
            require(store.material(scope) == password, "dashboard-identity")
            code = self.worker.ceph_status("dashboard", "ac-user-create", DASHBOARD_USER, "-i", "-", "administrator", data=password.encode("ascii"))
            require(code != errno.EINVAL, "dashboard-policy")
            require(code == 0, "native-command")
            changed = True
        self.worker.dashboard_verify(password)
        self.worker.markers.write(store.name("applied"), binding)
        # The password is an opaque protected output; it never enters public evidence.
        return {"changed": changed, "private": password}

    def dashboard_retained(self):
        """The retained password once the phase completed, proved against Ceph's account read-only; None before completion."""
        self.worker.verify_tool()
        scope = self.worker.dashboard_scope()
        store = self.worker.dashboard_store()
        if self.worker.markers.read(store.name("applied")) is None:
            return None
        store.prove_binding(store.binding(scope, "ceph-dashboard-user-v1"))
        password = store.material(scope)
        require(DASHBOARD_USER in self.dashboard_users(), "dashboard-identity")
        self.worker.dashboard_verify(password)
        return password

    def dashboard_read(self):
        """Recapture the dashboard password read-only; an unfinished phase offers nothing."""
        password = self.worker.dashboard_retained()
        return {} if password is None else {"private": password}

    def dashboard_present(self):
        """The dashboard_read proof for an observation no operation records: the password is checked here and never returned."""
        return {} if self.worker.dashboard_retained() is None else {"present": True}

    def dashboard_observation(self):
        """The active MGR's dashboard URL and the stored certificate's fingerprint; reachability is the controller's proof."""
        services = self.worker.ceph("mgr", "services")
        require(isinstance(services, dict), "native-output")
        url = services.get("dashboard", "")
        if not isinstance(url, str) or DASHBOARD_URL.fullmatch(url) is None:
            url = ""
        return {"url": url, "certificateSHA256": self.dashboard_certificate(), "reachable": False}
