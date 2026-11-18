"""Native bootstrap: SSH authorization, the verified tunnel, the seed, its configuration and the manager mounts."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import hashlib
import inspect
import json
import os
import pwd
import re
import stat
import tempfile
import uuid

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, held_directory, private_files
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    BOOTSTRAP_TIMEOUT, CEPHADM_TIME_UNITS, MAX_INPUT, MAX_OUTPUT, NativeFailure, PENDING_DAEMON, Refusal, SOURCE_LINKS, bootstrap_config,
    daemon_identity_host, held, publish_regular, registry_material, require, tunnel_helpers,
)


class Bootstrap:
    """Native bootstrap: SSH authorization, the verified tunnel, the seed, its configuration and the manager mounts."""

    def __init__(self, worker):
        self.worker = worker

    def prove_authorization(self):
        """Read the owned exact key line without rewriting an operator account."""
        helper, unused = tunnel_helpers()
        value = self.worker.markers.read("ssh-authorization.json")
        require(isinstance(value, dict) and set(value) == {"uid", "home", "line"}, "ownership")
        account = pwd.getpwnam(self.worker.request["clusterSSHUser"])
        require(type(value["uid"]) is int and value["uid"] == account.pw_uid > 0 and value["home"] == account.pw_dir, "ownership")
        require(isinstance(value["line"], str) and len(value["line"].split()) == 4, "ownership")
        public = " ".join(value["line"].split()[1:3])
        require(value["line"] == helper.authorization_line(public, self.worker.request["owner"]), "ownership")
        with self.worker.native_source(os.path.join(value["home"], ".ssh", "authorized_keys"), private=False, allowed_uid=account.pw_uid) as handle:
            content = handle.read(MAX_INPUT + 1)
            lines = content.decode("utf-8").splitlines()
            require([line for line in lines if public.split()[1] in line.split()] == [value["line"]], "ownership")

    def prove_preparation(self):
        scope = self.worker.recovery_scope()
        require(canonical(self.worker.markers.read("bootstrap-prepared.json")) == canonical(scope), "ownership")
        self.worker.prove_registry()
        self.prove_authorization()
        self.prove_tunnel_policy()
        managers = next(service["placement"]["hosts"] for service in self.worker.request["services"] if service["service_type"] == "mgr")
        if self.worker.host["name"] in managers:
            self.worker.verify_transport_bundle()

    def authorization_result(self, changed):
        if self.worker.recovery_enabled():
            self.worker.markers.write("bootstrap-prepared.json", self.worker.recovery_scope())
            self.worker.prove_preparation()
        return {"changed": changed}

    def preparation_state(self):
        unused, staging = tunnel_helpers()
        owned, daemons = self.worker.markers.owned(), self.worker.daemons()
        require(owned or not daemons, "foreign-cluster")
        if not owned:
            require(self.worker.markers.read("bootstrap-scope.json") is None and self.worker.markers.read("bootstrap-protocol.json") is None, "ownership")
            stage = "unclaimed"
        else:
            self.worker.recovery_scope()
            if daemons:
                stage = "complete"
            elif self.worker.markers.read("bootstrap-prepared.json") is None:
                stage = "claimed"
            else:
                self.worker.prove_preparation()
                stage = "prepared"
        return staging.recovery_record(self.worker.request, self.worker.host["name"], stage)

    def authorize(self, private):
        """Add only the separately owned cluster key to an existing account."""
        require(self.worker.markers.owned(), "ownership")
        require(set(private) == {"clusterPublicKey", "registryJSON", "clusterPrivateKey", "knownHosts"}, "material")
        self.tunnel_host_key_preflight(private)
        registry = registry_material(self.worker.request, private["registryJSON"])
        public = private["clusterPublicKey"]
        require(isinstance(public, str) and len(public) <= 32768 and len(public.strip().splitlines()) == 1, "material")
        parts = public.split()
        require(len(parts) >= 2 and parts[0] in ("ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"), "material")
        try:
            blob = base64.b64decode(parts[1], validate=True)
            length = int.from_bytes(blob[:4], "big")
            require(blob[4:4 + length].decode("ascii") == parts[0], "material")
        except (ValueError, UnicodeError):
            raise Refusal("material") from None
        account = pwd.getpwnam(self.worker.request["clusterSSHUser"])
        require(account.pw_uid > 0 and os.path.isabs(account.pw_dir), "cluster-account")
        helper, unused = tunnel_helpers()
        line = helper.authorization_line(public, self.worker.request["owner"])
        identity = {"uid": account.pw_uid, "home": account.pw_dir, "line": line}
        recorded = self.worker.markers.read("ssh-authorization.json")
        require(recorded is None or recorded == identity, "ownership")
        home = os.open(account.pw_dir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        directory = None
        try:
            info = os.fstat(home)
            require(info.st_uid == account.pw_uid and info.st_mode & 0o022 == 0, "cluster-account")
            try:
                directory = os.open(".ssh", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=home)
            except FileNotFoundError:
                self.worker.registry_preflight(registry)
                self.worker.tunnel_policy_preflight()
                # Ownership is durable before even this non-Ceph account effect.
                self.worker.markers.write("ssh-authorization.json", identity)
                os.mkdir(".ssh", 0o700, dir_fd=home)
                os.chown(".ssh", account.pw_uid, account.pw_gid, dir_fd=home, follow_symlinks=False)
                directory = os.open(".ssh", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=home)
            info = os.fstat(directory)
            require(info.st_uid == account.pw_uid and info.st_mode & 0o077 == 0, "cluster-account")
            try:
                fd = os.open("authorized_keys", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
            except FileNotFoundError:
                current = b""
            else:
                with os.fdopen(fd, "rb") as handle:
                    info = os.fstat(handle.fileno())
                    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid in (0, account.pw_uid) and info.st_mode & 0o022 == 0
                            and info.st_size <= MAX_OUTPUT, "cluster-account")
                    current = handle.read(MAX_OUTPUT + 1)
                    require(len(current) == info.st_size, "cluster-account")
            text = current.decode("utf-8")
            matching = [entry for entry in text.splitlines() if parts[1] in entry.split()]
            require(not matching or recorded == identity and matching == [line], "foreign-authorization")
            self.worker.registry_preflight(registry)
            self.worker.tunnel_policy_preflight()
            route_changed = self.worker.prepare_route()
            registry_changed = self.worker.prepare_registry(registry) or route_changed
            managers = next(s["placement"]["hosts"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
            if self.worker.host["name"] in managers:
                self.worker.transport({name: private[name] for name in ("clusterPrivateKey", "clusterPublicKey", "knownHosts")})
            self.worker.tunnel_guest_policy()
            if matching:
                return self.authorization_result(registry_changed)
            self.worker.markers.write("ssh-authorization.json", identity)
            contents = current + (b"\n" if current and not current.endswith(b"\n") else b"") + line.encode() + b"\n"
            stage = ".bootwright-" + uuid.uuid4().hex
            fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
            try:
                with os.fdopen(fd, "wb") as handle:
                    os.fchown(handle.fileno(), account.pw_uid, account.pw_gid)
                    handle.write(contents)
                    handle.flush()
                    os.fsync(handle.fileno())
                os.rename(stage, "authorized_keys", src_dir_fd=directory, dst_dir_fd=directory)
                os.fsync(directory)
            finally:
                with contextlib.suppress(FileNotFoundError):
                    os.unlink(stage, dir_fd=directory)
            return self.authorization_result(True)
        finally:
            if directory is not None:
                os.close(directory)
            os.close(home)

    def tunnel_guest_policy(self):
        helper, unused = tunnel_helpers()
        content = helper.guest_policy(self.worker.request["clusterSSHUser"])
        token = hashlib.sha256((self.worker.request["fsid"] + "\x00" + self.worker.request["owner"]).encode()).hexdigest()
        name = "05-bootwright-ceph-" + token + ".conf"
        record = {"name": name, "sha256": hashlib.sha256(content).hexdigest()}
        owned = self.worker.markers.read("sshd-policy.json")
        require(owned is None or owned == record, "ownership")
        directory = os.open("/etc/ssh/sshd_config.d", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(directory)
            require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "cluster-account")
            ceph_native.read_regular("/etc/ssh/sshd_config", maximum=256 * 1024)
            try:
                existing = ceph_native.read_regular("/etc/ssh/sshd_config.d/" + name)
            except FileNotFoundError:
                existing = None
            require(existing is None or owned == record and existing == content, "foreign-authorization")
            self.worker.markers.write("sshd-policy.json", record)
            if existing is None:
                publish_regular(directory, name, content, 0)
            try:
                self.prove_tunnel_policy()
            except Exception:
                if existing is None:
                    os.unlink(name, dir_fd=directory)
                    os.fsync(directory)
                raise
            self.worker.command(["/usr/bin/systemctl", "reload", "sshd.service"], json_result=False)
            self.prove_tunnel_policy()
        finally:
            os.close(directory)

    def prove_tunnel_policy(self):
        helper, unused = tunnel_helpers()
        self.worker.tunnel_policy_preflight()
        for client, address in sorted({(h["name"], h["address"]) for h in self.worker.request["hosts"]} | {("localhost", "127.0.0.1")}):
            context = ("user=" + self.worker.request["clusterSSHUser"] + ",host=" + client + ",addr=" + address +
                       ",laddr=" + self.worker.host["address"] + ",lport=" + str(self.worker.host["port"]))
            helper.verify_guest_policy(self.worker.command(["/usr/sbin/sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", context], json_result=False))

    def tunnel_policy_preflight(self):
        helper, staging = tunnel_helpers()
        policy = helper.guest_policy(self.worker.request["clusterSSHUser"])
        owned = self.worker.markers.read("sshd-policy.json")
        token = hashlib.sha256((self.worker.request["fsid"] + "\x00" + self.worker.request["owner"]).encode()).hexdigest()
        expected = {"name": "05-bootwright-ceph-" + token + ".conf", "sha256": hashlib.sha256(policy).hexdigest()}
        require(owned is None or owned == expected, "ownership")
        pending = ["/etc/ssh/sshd_config"]
        seen, total = set(), 0
        while pending:
            path = pending.pop()
            require(path not in seen and len(seen) < 128, "cluster-account")
            seen.add(path)
            content = self.sshd_policy_file(path)
            total += len(content)
            require(total <= MAX_INPUT, "cluster-account")
            permitted = policy if (isinstance(owned, dict) and path == "/etc/ssh/sshd_config.d/" + owned.get("name", "") and
                                   hashlib.sha256(content).hexdigest() == owned.get("sha256")) else None
            for include in staging.sshd_includes(content, owned_policy=permitted):
                if include == "/etc/ssh/sshd_config.d/*.conf":
                    directory = os.open("/etc/ssh/sshd_config.d", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                    try:
                        info = os.fstat(directory)
                        require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "cluster-account")
                        pending.extend("/etc/ssh/sshd_config.d/" + name for name in sorted(os.listdir(directory)) if name.endswith(".conf"))
                    finally:
                        os.close(directory)
                else:
                    pending.append(include)
        self.worker.command(["/usr/sbin/sshd", "-t", "-f", "/etc/ssh/sshd_config"], json_result=False)

    def sshd_policy_file(self, path):
        return self.worker.native_file(path, maximum=256 * 1024, private=False, root_only=True)

    def transport_scope(self):
        helper, unused = tunnel_helpers()
        uid = self.worker.manager_uid()
        label = self.worker.manager_label()
        require(type(uid) is int and uid > 0 and isinstance(label, str) and label, "native-tunnel-unproved")
        return {"owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"], "host": self.worker.host["name"],
                "image": self.worker.request["image"], "uid": uid, "label": label,
                "nativeUser": self.worker.request["clusterSSHUser"], "hosts": helper.targets(self.worker.request)}

    def tunnel_preflight(self, private):
        """Read only: the coordinator asks each manager host before it claims or authorizes any host."""
        require(set(private) == {"knownHosts"}, "material")
        self.tunnel_host_key_preflight(private)
        return {"changed": False}

    def tunnel_host_key_preflight(self, private):
        """Before any authorization effect, a manager host proves its tunnel client admits an algorithm of every pinned host key."""
        managers = next(s["placement"]["hosts"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
        if self.worker.host["name"] not in managers:
            return
        helper, unused = tunnel_helpers()
        require(isinstance(private["knownHosts"], str), "material")
        hosts = helper.targets(self.worker.request)
        self.worker._tunnel_host_keys(
            lambda: helper.outer_configuration(self.worker.manager_policy(), hosts, helper.known_hosts(private["knownHosts"].encode(), hosts)))

    def _tunnel_host_keys(self, render):
        """Render tunnel material; a pinned key the image's policy excludes is a named native failure naming its host and key type."""
        helper, unused = tunnel_helpers()
        try:
            return render()
        except helper.HostKeyRefusal as error:
            raise NativeFailure("tunnel-host-key-excluded", self.worker.native_coordinate(),
                                "host " + error.host + " pins a " + error.key_type
                                + " host key, and the HostKeyAlgorithms of the selected image's SSH crypto policy admit none of its algorithms") from None

    def transport(self, private):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        require(self.worker.markers.owned(), "ownership")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        scope = self.worker.transport_scope()
        policy = self.worker.manager_policy()
        helper, unused = tunnel_helpers()
        source = getattr(helper, "_BOOTWRIGHT_SOURCE", None) or inspect.getsource(helper)
        files = self.worker._tunnel_host_keys(
            lambda: helper.bundle(self.worker.request, private["clusterPrivateKey"].encode(), private["knownHosts"].encode(),
                                  source.encode(), policy, public=private["clusterPublicKey"]))
        original = custody.retain(self.worker.markers, "transport-original", scope, files)
        record = {"scope": scope, "custodyID": original["custodyID"], "manifest": json.loads(files["manifest.json"])}
        owned = self.worker.markers.read("transport.json")
        require(owned is None or canonical(owned) == canonical(record), "ownership")
        self.worker.markers.write("transport.json", record)
        with custody.hold(self.worker.markers, "transport-original", scope) as (unused, material):
            directory = custody.manager_directory(self.worker.markers, scope["uid"])
            try:
                require(set(os.listdir(directory)) <= set(material), "ownership")
                for name, data in sorted(material.items()):
                    custody.materialize(self.worker.markers, directory, name, data, scope["uid"])
                custody.relabel_intent(self.worker.markers, scope, scope["uid"], scope["label"])
                custody.prove_materialized(self.worker.markers, directory, scope, material, scope["uid"], scope["label"])
                helper.verify_bundle(os.path.join(self.worker.markers.path, "transport"), expected=material, uid=scope["uid"])
                self.worker.markers.write("transport-ready.json", record)
            finally:
                os.close(directory)
        return {"changed": owned is None}

    def bootstrap(self, private):
        require(ceph_native.supported_transport(self.worker.request), "unsupported-release")
        require(self.worker.markers.owned(), "ownership")
        require(self.worker.host["name"] == self.worker.request["bootstrap"])
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.worker.prove_registry()
        return self.worker.tunnel_bootstrap(private)

    def tunnel_bootstrap(self, private):
        unused, staging = tunnel_helpers()
        intent = staging.stage_intent(self.worker.request)
        require(not self.worker.daemons(), "partial-cluster")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.worker.prove_registry()
        require(self.worker.markers.read("transport-ready.json") is not None, "native-tunnel-unproved")
        scope = self.worker.recovery_scope()
        self.worker.prove_preparation()
        require(self.worker.markers.read("bootstrap-attempt.json") is None, "partial-cluster")
        require(self.worker.markers.read("tunnel-stage.json") is None or self.worker.markers.read("bootstrap-protocol.json") == scope, "ownership")
        self.worker.prove_bootstrap_absence()
        self.worker.markers.write("bootstrap-protocol.json", scope)
        self.worker.markers.write("tunnel-stage.json", intent)
        with tempfile.TemporaryDirectory(prefix=".bootstrap-", dir=self.worker.markers.path) as scratch:
            private_files.write(os.path.join(scratch, "ceph.conf"), bootstrap_config(self.worker.request))
            with self.worker.transport_hold() as original:
                helper, unused = tunnel_helpers()
                # The bundle holds known_hosts in the form transport() rendered, not the controller's bytes.
                known = helper.known_hosts(private["knownHosts"].encode(), helper.targets(self.worker.request))
                require(private["clusterPrivateKey"].encode() == original["identity"] and known == original["known_hosts"], "ownership")
                require(helper.public_key(private["clusterPublicKey"]) == json.loads(original["manifest.json"])["identityPublicKey"], "ownership")
                # A refusal here leaves no attempt record, so the bootstrap may still be retried.
                self.worker.prove_inert_host_preparation()
                argv = staging.bootstrap_argv(self.worker.request, scratch, self.worker.selection)
                self.worker.require_call_home_path()
                self.worker.require_declared_options(argv)
                self.worker.markers.write("bootstrap-attempt.json", scope)
                self.worker.command(argv, timeout=BOOTSTRAP_TIMEOUT, json_result=False, recover=self.license_call_continues)
        self.worker.capture_bootstrap_outputs()
        return {"changed": True}

    def license_call_continues(self, code, output):
        """Only an IBM build's own zero-host license-call refusal, with the exact owned seed in place, continues bootstrap."""
        unused, staging = tunnel_helpers()
        if not staging.license_call_refusal(self.worker.request, code, output):
            return False
        if self.worker.selection.variant("ceph.bootstrap.license-call-refusal") != "continue-seed":
            return False
        try:
            self.worker.prove_seed(self.worker.daemons())
        except (ValueError, OSError):
            return False
        return True

    def prove_inert_host_preparation(self):
        """Prove host preparation inert before invoking the selected provider's host check."""
        require(os.path.isfile("/usr/sbin/lvcreate") and os.access("/usr/sbin/lvcreate", os.X_OK), "host-prerequisite")
        for unit in CEPHADM_TIME_UNITS:
            enabled = self.worker.run(["/usr/bin/systemctl", "is-enabled", unit], timeout=30)[1].decode("ascii", "replace").strip()
            active = self.worker.run(["/usr/bin/systemctl", "is-active", unit], timeout=30)[1].decode("ascii", "replace").strip()
            if enabled == "enabled" and active == "active":
                break
            # systemd prints nothing on stdout for a unit it does not know, or "not-found"; cephadm treats both as absent.
            require(enabled in ("", "not-found"), "host-time")
        else:
            raise Refusal("host-time")
        self.worker.command([self.worker.tool, "check-host", "--expect-hostname", self.worker.host["name"]], timeout=60, json_result=False)

    def prove_bootstrap_absence(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        require(not self.worker.daemons(), "partial-cluster")
        for name in ("bootstrap-output-source.json", "bootstrap-original-reserved.json", "bootstrap-original-ready.json",
                     "tunnel-config-intent.json", "tunnel-configured.json", "tunnel-redeploy.json", "tunnel-mounted.json", "bootstrap-enrollment.json"):
            require(self.worker.markers.read(name) is None, "ownership")
        paths = list(self.worker.bootstrap_sources().values()) + [os.path.join("/var/lib/ceph", self.worker.request["fsid"])]
        paths += ["/etc/systemd/system/ceph-" + self.worker.request["fsid"] + ".target"]
        for path in paths:
            try:
                custody.source_identity(path, 0)
            except FileNotFoundError:
                continue
            raise Refusal("partial-cluster")
        custody.source_identity("/etc/systemd/system", 0)
        directory = os.open("/etc/systemd/system", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(directory)
            require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "ownership")
            names = os.listdir(directory)
            require(len(names) <= 8192 and not any(name.startswith("ceph-" + self.worker.request["fsid"] + "@") for name in names), "partial-cluster")
        finally:
            os.close(directory)

    def prove_seed(self, daemons, observing=False):
        unused, staging = tunnel_helpers()
        scope = self.worker.recovery_scope()
        require(self.worker.host["name"] == self.worker.request["bootstrap"], "ownership")
        require(canonical(self.worker.markers.read("bootstrap-protocol.json")) == canonical(scope) and
                canonical(self.worker.markers.read("bootstrap-attempt.json")) == canonical(scope), "ownership")
        intent = staging.prove_seed_daemons(self.worker.request, daemons, observing=observing)
        require(self.worker.markers.read("tunnel-stage.json") == intent and self.worker.markers.read("bootstrap-enrollment.json") is None, "ownership")
        self.worker.prove_preparation()
        if self.worker.markers.read("bootstrap-output-source.json") is None:
            # Uncaptured outputs keep cephadm's own modes; capture makes them private, and observation never does.
            self.worker.bootstrap_outputs(tighten=False)
        else:
            for path in self.worker.bootstrap_sources().values():
                with self.worker.native_source(path, root_only=True):
                    pass
        if self.worker.markers.read("bootstrap-original-ready.json") is not None:
            require(self.worker.ceph("fsid") == {"fsid": self.worker.request["fsid"]}, "ownership")
            require(self.worker.ceph("orch", "host", "ls") == [] and self.worker.services_inventory() == [] and self.worker.ceph("osd", "ls") == [],
                    "partial-cluster")
            quorum = self.worker.ceph("quorum_status")
            require(isinstance(quorum, dict) and quorum.get("quorum_names") == [intent["monID"]], "partial-cluster")
            mgr = self.worker.ceph("mgr", "dump")
            require(isinstance(mgr, dict) and mgr.get("active_name") == intent["mgrID"] and mgr.get("standbys") == [], "partial-cluster")
            self.worker.provider_registry_absent()
        return intent

    def bootstrap_state(self):
        """Read-only admission proof; an attempted empty seed never starts again."""
        unused, staging = tunnel_helpers()
        require(self.worker.request["action"] == "cluster" and self.worker.host["name"] == self.worker.request["bootstrap"], "request")
        daemons, owned = self.worker.daemons(), self.worker.markers.owned()
        require(owned or not daemons, "foreign-cluster")
        evidence = self.worker.empty_evidence()
        if self.worker.markers.read("bootstrap-enrollment.json") is not None:
            require(canonical(self.worker.markers.read("bootstrap-enrollment.json")) == canonical(self.worker.recovery_scope()), "ownership")
            evidence = self.worker.observe()
            # A stretch cluster is complete once everything but its activation is ready; activation continues from there.
            stage = "complete" if evidence["ready"] or self.worker.base_ready else "enrolling"
        elif not daemons:
            require(self.worker.markers.read("bootstrap-attempt.json") is None, "partial-cluster")
            if owned:
                scope = self.worker.recovery_scope()
                protocol = self.worker.markers.read("bootstrap-protocol.json")
                require(protocol is None or canonical(protocol) == canonical(scope), "ownership")
                require(self.worker.markers.read("tunnel-stage.json") is None or protocol is not None
                        and self.worker.markers.read("tunnel-stage.json") == staging.stage_intent(self.worker.request), "ownership")
            self.worker.prove_bootstrap_absence()
            stage = self.worker.preparation_state()["stage"]
            if owned:
                evidence.update(exists=True, partial=True)
        else:
            self.worker.prove_seed(daemons, observing=True)
            evidence.update(exists=True, partial=True)
            stage = "seed"
            if self.worker.markers.read("tunnel-configured.json") is not None:
                require(self.worker.configuration_complete(), "native-tunnel-unproved")
                stage = "configured"
            if self.worker.markers.read("tunnel-redeploy.json") is not None:
                self.worker.blocking = self.worker.prove_mounted_seed(daemons, observing=True)
                if self.worker.markers.read("tunnel-mounted.json") is not None and self.worker.markers.read("tunnel-manager-files.json") is not None:
                    stage = "mounted"
        result = staging.recovery_record(self.worker.request, self.worker.host["name"], stage)
        result["evidence"] = evidence
        return result

    def tunnel_effect(self, *arguments, **options):
        proof = self.worker.transport_hold()
        with proof, self.worker.cache_hold():
            return self.worker.ceph(*arguments, **options)

    def cache_hold(self):
        """Once published, every native SSH effect holds the exact cephadm cache inode."""
        if self.worker.markers.read("cache-ready.json") is None:
            return contextlib.nullcontext()
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        return runtime.Cache(self.worker).execution_proof()

    def cache(self):
        """Publish and prove the exact cephadm bytes on this host before enrollment."""
        require(self.worker.markers.owned(), "ownership")
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        if self.worker.host["name"] == self.worker.request["bootstrap"]:
            require(self.worker.markers.read("tunnel-mounted.json") is not None, "native-tunnel-unproved")
        return {"changed": runtime.Cache(self.worker).prepare()}

    def configuration_intent(self):
        helper, unused = tunnel_helpers()
        transport = self.worker.markers.read("transport-ready.json")
        require(isinstance(transport, dict) and isinstance(transport.get("custodyID"), str), "ownership")
        return {"scope": self.worker.recovery_scope(), "custodyID": transport["custodyID"],
                "config": hashlib.sha256(helper.native_configuration(helper.targets(self.worker.request))).hexdigest()}

    def configuration_keys(self):
        keys = self.worker.ceph("config-key", "ls")
        require(isinstance(keys, list) and len(keys) <= 8192 and all(isinstance(key, str) for key in keys) and len(set(keys)) == len(keys), "native-output")
        require("mgr/cephadm/ssh_identity_cert" not in keys, "native-tunnel-unproved")
        return set(keys)

    def configure_setting(self, name, wanted, command, mutate):
        intent = dict(self.worker.configuration_intent(), step=name)
        attempted = self.worker.markers.read("tunnel-config-attempt-" + name + ".json")
        completed = self.worker.markers.read("tunnel-config-complete-" + name + ".json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        key = "mgr/cephadm/" + name
        present = key in self.worker.configuration_keys()
        # A matching unbound secret is foreign; never read it to infer custody.
        require(not present or attempted is not None, "ownership")
        if not present:
            require(attempted is None and completed is None, "partial-cluster")
            if not mutate:
                return False, False
            self.worker.markers.write("tunnel-config-attempt-" + name + ".json", intent)
            self.worker.tunnel_effect(*command, data=wanted if name != "ssh_user" else b"", json_result=False)
            require(key in self.worker.configuration_keys(), "native-tunnel-unproved")
        # The CLI prints a stored value as it is; asking for JSON would prepend a blank line.
        value = self.worker.ceph_text("config-key", "get", key)
        require(isinstance(value, bytes) and value in (wanted, wanted + b"\n"), "native-tunnel-unproved")
        if mutate:
            self.worker.markers.write("tunnel-config-complete-" + name + ".json", intent)
        return True, not present

    def call_home_acceptance(self, optional=False):
        values = self.worker.ceph("config", "dump")
        require(isinstance(values, list), "native-output")
        selected = [value for value in values if isinstance(value, dict) and value.get("name") == "mgr/cephadm/call_home_needs_acceptance"]
        if not selected and optional:
            return None
        require(len(selected) == 1 and selected[0].get("section") == "mgr" and selected[0].get("mask") == "", "call-home-drift")
        # The manager stores Python's str(bool); the MON normalizes a known option's spelling, so either one is the same flag.
        value = selected[0].get("value")
        require(isinstance(value, str) and value.lower() in ("true", "false"), "call-home-drift")
        return value.lower() == "true"

    def call_home_reissuable(self, needed):
        """An attempted enable-call-home only sets values and enables a module: before any acceptance, a missing result repeats it."""
        if needed is False or any(self.worker.markers.read(name) is not None for name in (
                "tunnel-config-attempt-call_home.json", "tunnel-config-complete-call_home.json", "call-home-accepted.json", "tunnel-configured.json")):
            return False
        if needed is None:
            return True
        try:
            self.worker.call_home_state()
        except Refusal as error:
            if type(error) is not Refusal or str(error) != "call-home-drift":
                raise
            return True
        return False

    def initialize_call_home(self, mutate):
        """Finish the native seed's missing Call Home initialization without re-bootstrapping."""
        if not self.worker.call_home_applicable():
            self.worker.call_home_state()
            return True, False
        intent = dict(self.worker.configuration_intent(), step="call_home_initialize")
        prefix = "tunnel-config-"
        attempted = self.worker.markers.read(prefix + "attempt-call_home_initialize.json")
        completed = self.worker.markers.read(prefix + "complete-call_home_initialize.json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        needed = self.worker.call_home_acceptance(optional=True)
        changed = False
        if attempted is None:
            if needed is not None:
                return True, False
            require(self.worker.markers.read("tunnel-config-attempt-call_home.json") is None
                    and self.worker.markers.read("tunnel-config-complete-call_home.json") is None
                    and self.worker.markers.read("call-home-accepted.json") is None
                    and self.worker.markers.read("tunnel-configured.json") is None, "call-home-drift")
            if not mutate:
                return False, False
            self.worker.markers.write(prefix + "attempt-call_home_initialize.json", intent)
            self.worker.manager_retry(self.worker.tunnel_effect, "orch", "enable-call-home", "--raise-automation-warning", json_result=False)
            needed = self.worker.call_home_acceptance()
            require(needed, "call-home-drift")
            changed = True
        else:
            if mutate and self.call_home_reissuable(needed):
                self.worker.manager_retry(self.worker.tunnel_effect, "orch", "enable-call-home", "--raise-automation-warning", json_result=False)
                needed = self.worker.call_home_acceptance()
                require(needed, "call-home-drift")
                changed = True
            require(needed is not None, "partial-cluster")
            acceptance = dict(self.worker.configuration_intent(), step="call_home")
            require(needed or canonical(self.worker.markers.read("tunnel-config-attempt-call_home.json")) == canonical(acceptance), "call-home-drift")
        self.worker.call_home_state()
        if mutate and completed is None:
            self.worker.markers.write(prefix + "complete-call_home_initialize.json", intent)
        return True, changed

    def configure_call_home(self, mutate):
        if not self.worker.call_home_applicable():
            return True, False
        intent = dict(self.worker.configuration_intent(), step="call_home")
        attempted = self.worker.markers.read("tunnel-config-attempt-call_home.json")
        completed = self.worker.markers.read("tunnel-config-complete-call_home.json")
        require(attempted is None or canonical(attempted) == canonical(intent), "ownership")
        require(completed is None or attempted is not None and canonical(completed) == canonical(intent), "ownership")
        needed = self.worker.call_home_acceptance()
        if attempted is None:
            require(needed and completed is None and self.worker.markers.read("call-home-accepted.json") is None, "call-home-drift")
            if not mutate:
                return False, False
            self.worker.markers.write("tunnel-config-attempt-call_home.json", intent)
            self.worker.accept_call_home()
            require(not self.worker.call_home_acceptance(), "call-home-drift")
        else:
            require(not needed, "partial-cluster")
        if mutate:
            self.worker.markers.write("call-home-accepted.json", {"callHome": "enabled"})
            self.worker.markers.write("tunnel-config-complete-call_home.json", intent)
        return True, attempted is None

    def configure_values(self, mutate):
        helper, unused = tunnel_helpers()
        self.worker.provider_registry_absent()
        options = self.worker.ceph("config", "dump")
        require(isinstance(options, list), "native-output")
        require(not any(isinstance(option, dict) and option.get("name") == "mgr/cephadm/ssh_config_file"
                        and option.get("value") not in ("", None) for option in options), "native-tunnel-unproved")
        config = helper.native_configuration(helper.targets(self.worker.request))
        changed, complete = False, True
        pre_steps = ("--skip-pre-steps",) if self.worker.selection.variant("ceph.mgr.set-user-pre-steps") == "skip" else ()
        with self.worker.transport_hold() as original:
            public = json.loads(original["manifest.json"])["identityPublicKey"].encode()
            values = (("ssh_identity_key", original["identity"], ("cephadm", "set-priv-key", "-i", "-")),
                      ("ssh_identity_pub", public, ("cephadm", "set-pub-key", "-i", "-")),
                      ("ssh_config", config, ("cephadm", "set-ssh-config", "-i", "-")),
                      ("ssh_user", self.worker.request["clusterSSHUser"].encode(), ("cephadm", "set-user", self.worker.request["clusterSSHUser"]) + pre_steps))
            for name, wanted, command in values:
                proved, effect = self.worker.configure_setting(name, wanted, command, mutate)
                complete, changed = complete and proved, changed or effect
            proved, effect = self.worker.initialize_call_home(mutate)
            complete, changed = complete and proved, changed or effect
            if proved:
                proved, effect = self.worker.configure_call_home(mutate)
                complete, changed = complete and proved, changed or effect
        self.worker.config_database()
        return complete, changed

    def configuration_complete(self):
        intent = self.worker.configuration_intent()
        require(canonical(self.worker.markers.read("tunnel-config-intent.json")) == canonical(intent), "ownership")
        require(canonical(self.worker.markers.read("tunnel-configured.json")) == canonical(intent), "ownership")
        for name in ("ssh_identity_key", "ssh_identity_pub", "ssh_config", "ssh_user") + (("call_home",) if self.worker.call_home_applicable() else ()):
            require(canonical(self.worker.markers.read("tunnel-config-complete-" + name + ".json")) == canonical(dict(intent, step=name)), "ownership")
        return self.configure_values(False)[0]

    def tunnel_configure(self, private):
        helper, staging = tunnel_helpers()
        require(self.worker.markers.owned() and self.worker.host["name"] == self.worker.request["bootstrap"], "ownership")
        require(self.worker.markers.read("tunnel-stage.json") == staging.stage_intent(self.worker.request), "ownership")
        require(set(private) == {"clusterPrivateKey", "clusterPublicKey", "knownHosts"}, "material")
        self.worker.prove_seed(self.worker.daemons())
        if self.worker.markers.read("bootstrap-original-ready.json") is None:
            self.worker.capture_bootstrap_outputs()
        require(self.worker.ceph("fsid") == {"fsid": self.worker.request["fsid"]}, "ownership")
        require(self.worker.manager_retry(self.worker.ceph, "orch", "host", "ls") == [] and self.worker.manager_retry(self.worker.services_inventory) == []
                and self.worker.ceph("osd", "ls") == [], "foreign-host")
        self.worker.prove_registry()
        with self.worker.transport_hold() as original:
            require(private["clusterPrivateKey"].encode() == original["identity"], "ownership")
            require(helper.public_key(private["clusterPublicKey"]) == json.loads(original["manifest.json"])["identityPublicKey"], "ownership")
        intent = self.worker.configuration_intent()
        self.worker.markers.write("tunnel-config-intent.json", intent)
        complete, changed = self.configure_values(True)
        require(complete, "native-tunnel-unproved")
        self.worker.markers.write("tunnel-configured.json", intent)
        return {"changed": changed}

    def manager_scope(self, plan):
        return {"owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"], "host": self.worker.host["name"],
                "image": self.worker.request["image"], "plan": plan}

    def retain_manager_material(self, plan, files):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        scope = self.worker.manager_scope(plan)
        if self.worker.markers.read("initial-mgr-original-ready.json") is not None:
            return scope
        with self.worker.native_source(files["config"]) as config, self.worker.native_source(files["keyring"]) as keyring:
            sources = {"config": custody.metadata(os.fstat(config.fileno())), "keyring": custody.metadata(os.fstat(keyring.fileno()))}
            record = {"scope": scope, "files": sources}
            previous = self.worker.markers.read("tunnel-mgr-source.json")
            require(previous is None or canonical(previous) == canonical(record), "ownership")
            self.worker.markers.write("tunnel-mgr-source.json", record)
            # Durable native source identities precede the first private read.
            material = {"config": config.read(MAX_INPUT + 1), "keyring": keyring.read(MAX_INPUT + 1)}
            for name, handle in (("config", config), ("keyring", keyring)):
                require(custody.metadata(os.fstat(handle.fileno())) == sources[name], "ownership")
                require(len(material[name]) == sources[name]["size"], "ownership")
            custody.retain(self.worker.markers, "initial-mgr-original", scope, material)
        return scope

    def prove_manager_material(self, files, material, original=False, transition=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        source = self.worker.markers.read("tunnel-mgr-source.json")
        require(isinstance(source, dict) and set(source) == {"scope", "files"}, "ownership")
        completed = self.worker.markers.read("tunnel-manager-files.json")
        if completed is not None:
            require(isinstance(completed, dict) and set(completed) == {"scope", "custodyID", "files"}, "ownership")
            require(canonical({key: completed[key] for key in ("scope", "custodyID")}) == canonical(self.worker.markers.read("tunnel-redeploy.json")),
                    "ownership")
        require(original or completed is not None or transition, "ownership")
        expected = source if original else completed
        observed = {}
        for name in ("config", "keyring"):
            with self.worker.native_source(files[name]) as handle:
                before = custody.metadata(os.fstat(handle.fileno()))
                require(expected is None or canonical(before) == canonical(expected["files"].get(name)), "ownership")
                require(handle.read(MAX_INPUT + 1) == material[name], "ownership")
                require(custody.metadata(os.fstat(handle.fileno())) == before, "ownership")
                observed[name] = before
        return observed

    def tunnel_mount(self):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        unused, staging = tunnel_helpers()
        require(self.worker.markers.owned() and self.worker.host["name"] == self.worker.request["bootstrap"], "ownership")
        intent = self.worker.markers.read("tunnel-stage.json")
        require(intent == staging.stage_intent(self.worker.request), "ownership")
        require(self.worker.markers.read("tunnel-configured.json") is not None, "ownership")
        if self.worker.recovery_enabled():
            self.worker.prove_seed(self.worker.daemons())
            require(self.worker.configuration_complete(), "native-tunnel-unproved")
        require(self.worker.ceph("fsid") == {"fsid": self.worker.request["fsid"]}, "ownership")
        self.worker.prove_registry()
        self.worker.provider_registry_absent()
        files = staging.initial_files(self.worker.request)
        mon_meta = self.worker.native_file(files["mon"]["unit.meta"], maximum=4096, private=False)
        mgr_meta = self.worker.native_file(files["mgr"]["unit.meta"], maximum=4096, private=False)
        plan = staging.initial_redeploy_plan(self.worker.request, intent, self.worker.daemons(),
                                             self.worker.manager_retry(self.worker.ceph, "orch", "host", "ls"), mon_meta, mgr_meta)
        scope = self.retain_manager_material(plan, files["mgr"])
        with custody.hold(self.worker.markers, "initial-mgr-original", scope) as (original, material), self.worker.transport_hold(settle=True):
            record = {"scope": scope, "custodyID": original["custodyID"]}
            previous = self.worker.markers.read("tunnel-redeploy.json")
            require(previous is None or canonical(previous) == canonical(record), "ownership")
            restarted = None
            if previous is None:
                self.prove_manager_material(files["mgr"], material, original=True)
                manager = self.worker.ceph("mgr", "dump")
                require(isinstance(manager, dict) and type(manager.get("active_gid")) is int, "native-output")
                restarted = manager["active_gid"]
                self.worker.markers.write("tunnel-redeploy.json", record)
                with tempfile.TemporaryDirectory(prefix=".tunnel-stage-", dir=self.worker.markers.path) as scratch:
                    private_files.write(os.path.join(scratch, "mgr.config"), material["config"])
                    private_files.write(os.path.join(scratch, "mgr.keyring"), material["keyring"])
                    self.worker.command(staging.redeploy_argv(self.worker.request, plan, scratch), timeout=600, json_result=False)
            # An ambiguous previous deployment permits observation only.
            self.worker.verify_manager_mounts(self.worker.daemons())
            # The redeploy restarted the only manager; its orchestrator answers again before the proof that needs it.
            self.worker.manager_wait(previous=restarted, name=intent["mgrID"])
            self.worker.manager_retry(self.worker.verify_tunnel_configuration)
            observed = self.prove_manager_material(files["mgr"], material, transition=True)
            self.worker.markers.write("tunnel-manager-files.json", dict(record, files=observed))
            self.worker.markers.write("tunnel-mounted.json", {"intent": intent, "custodyID": original["custodyID"]})
        return {"changed": previous is None}

    def prove_mounted_seed(self, daemons, observing=False):
        """The mounted seed; an observation returns the seed manager pending while cephadm restarts its container."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        unused, staging = tunnel_helpers()
        require(self.worker.configuration_complete(), "native-tunnel-unproved")
        files = staging.initial_files(self.worker.request)
        plan = staging.initial_redeploy_plan(self.worker.request, staging.stage_intent(self.worker.request), daemons, [],
                                             self.worker.native_file(files["mon"]["unit.meta"], maximum=4096, private=False),
                                             self.worker.native_file(files["mgr"]["unit.meta"], maximum=4096, private=False), observing=observing)
        scope = self.worker.manager_scope(plan)
        with custody.hold(self.worker.markers, "initial-mgr-original", scope) as (original, material), self.worker.transport_hold():
            require(canonical(self.worker.markers.read("tunnel-redeploy.json")) == canonical({"scope": scope, "custodyID": original["custodyID"]}), "ownership")
            mounted = self.worker.markers.read("tunnel-mounted.json")
            require(mounted is None
                    or canonical(mounted) == canonical({"intent": staging.stage_intent(self.worker.request), "custodyID": original["custodyID"]}),
                    "ownership")
            pending = self.worker.verify_manager_mounts(daemons, observing=observing)
            self.worker.verify_tunnel_configuration()
            self.prove_manager_material(files["mgr"], material, transition=True)
        return pending

    @contextlib.contextmanager
    def native_source(self, path, maximum=MAX_INPUT, private=True, root_only=False, allowed_uid=None):
        """Open one proved file: every directory on the way owned by an admitted account and closed to others, and the
        file regular, admitted, closed and bounded. A public root-only source follows a root-owned symbolic link, as the
        crypto-policy back-ends sshd includes are, through the same proof and a bound, and may carry a package's hard
        links; any other source refuses a link and is the only name of its inode."""
        uid = self.worker.manager_uid()
        require(type(uid) is int and uid > 0, "native-tunnel-unproved")
        owners = (ceph_native.ROOT_UID,) if root_only else (ceph_native.ROOT_UID, uid if allowed_uid is None else allowed_uid)
        shared = root_only and not private
        followed = 0
        while True:
            components = path.strip("/").split("/")
            require(os.path.isabs(path) and all(c not in ("", ".", "..") for c in components), "file")
            parent = "/" + "/".join(components[:-1])
            try:
                with held(parent, "file", owners, follow=lambda entry: shared and entry.st_uid == ceph_native.ROOT_UID) as directory:
                    entry = os.stat(components[-1], dir_fd=directory, follow_symlinks=False)
                    if stat.S_ISLNK(entry.st_mode):
                        require(shared and entry.st_uid == ceph_native.ROOT_UID, "file")
                        raise held_directory.Linked(os.path.normpath(os.path.join(parent, os.readlink(components[-1], dir_fd=directory))))
                    fd = os.open(components[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
                    with os.fdopen(fd, "rb") as handle:
                        info = os.fstat(handle.fileno())
                        require(stat.S_ISREG(info.st_mode) and (info.st_nlink == 1 or shared) and info.st_uid in owners
                                and info.st_mode & (0o077 if private else 0o022) == 0 and 0 < info.st_size <= maximum, "file")
                        yield handle
                    return
            except held_directory.Linked as link:
                require(followed < SOURCE_LINKS, "file")
                path = link.path
                followed += 1

    def native_file(self, path, maximum=MAX_INPUT, private=True, root_only=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        with self.worker.native_source(path, maximum, private, root_only) as handle:
            before = custody.metadata(os.fstat(handle.fileno()))
            content = handle.read(maximum + 1)
            require(len(content) == before["size"] and custody.metadata(os.fstat(handle.fileno())) == before, "file")
            return content

    @contextlib.contextmanager
    def transport_hold(self, settle=False):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        helper, unused = tunnel_helpers()
        scope = self.worker.transport_scope()
        record = self.worker.markers.read("transport-ready.json")
        # The wrapped context closes its descriptors on any exit; the proof after the yield is deliberately success-only.
        # pylint: disable-next=contextmanager-generator-missing-cleanup
        with custody.hold(self.worker.markers, "transport-original", scope) as (original, material):
            wanted = {"scope": scope, "custodyID": original["custodyID"], "manifest": json.loads(material["manifest.json"])}
            require(canonical(record) == canonical(wanted) and canonical(self.worker.markers.read("transport.json")) == canonical(wanted), "ownership")
            directory = custody.manager_directory(self.worker.markers, scope["uid"], create=False)
            try:
                custody.prove_materialized(self.worker.markers, directory, scope, material, scope["uid"], scope["label"], settle=settle)
                helper.verify_bundle(os.path.join(self.worker.markers.path, "transport"), expected=material, uid=scope["uid"])
                yield material
                custody.prove_materialized(self.worker.markers, directory, scope, material, scope["uid"], scope["label"], settle=settle)
            finally:
                os.close(directory)

    def verify_transport_bundle(self):
        with self.worker.transport_hold():
            pass

    def prove_mount_source(self, source, exact):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_custody as custody
        scope = self.worker.transport_scope()
        identity = custody.source_identity(source, scope["uid"])
        if exact:
            recorded = self.worker.markers.read("transport-directory.json")
            require(isinstance(recorded, dict) and set(recorded) == custody.BASE_FIELDS and all(type(item) is int for item in recorded.values()), "ownership")
            require(recorded["uid"] == recorded["gid"] == scope["uid"] and recorded["mode"] == 0o700 and identity == (recorded["dev"], recorded["inode"]),
                    "ownership")
        else:
            require(identity not in custody.protected_identities(self.worker.markers.path), "native-tunnel-unproved")

    def verify_manager_mounts(self, daemons, observing=False):
        """Prove each running manager container of this host mounts the bundle exactly. An observation names a manager whose
        container is absent or not running, as cephadm's --rm containers are while it restarts them, as pending; an action
        requires it running. A running container that contradicts the request refuses either way."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observe
        helper, unused = tunnel_helpers()
        require(isinstance(daemons, list) and bool(daemons), "native-tunnel-unproved")
        seen, pending = set(), []
        managers = next(s["placement"]["hosts"] for s in self.worker.request["services"] if s["service_type"] == "mgr")
        for daemon in daemons:
            require(isinstance(daemon, dict) and daemon.get("fsid") == self.worker.request["fsid"], "native-tunnel-unproved")
            # Another block owns a delegated daemon's image; the host layout already proved its listing.
            if str(daemon.get("name", "")).split(".", 1)[0] in ceph_native_observe.DELEGATED_TYPES:
                continue
            # A daemon cephadm has not yet written unit files for names no image and runs no container.
            images = (self.worker.request["image"],) if daemon.get("container_id") != "" else (self.worker.request["image"], "")
            require(daemon.get("container_image_name") in images, "native-tunnel-unproved")
            if not str(daemon.get("name", "")).startswith("mgr."):
                continue
            name = daemon["name"][4:]
            require(name not in seen and daemon["name"] not in pending and daemon_identity_host("mgr", name, managers) == self.worker.host["name"],
                    "native-tunnel-unproved")
            if daemon.get("state") != "running" or daemon.get("container_id") == "":
                require(observing and PENDING_DAEMON.fullmatch(daemon["name"]) is not None, "native-tunnel-unproved")
                pending.append(daemon["name"])
                continue
            seen.add(name)
            identifier = daemon.get("container_id")
            require(isinstance(identifier, str) and re.fullmatch(r"[0-9a-f]{12,64}", identifier) is not None, "native-tunnel-unproved")
            containers = self.worker.command(["/usr/bin/podman", "container", "inspect", identifier])
            require(isinstance(containers, list) and len(containers) == 1 and isinstance(containers[0], dict), "native-tunnel-unproved")
            container = containers[0]
            require(isinstance(container.get("HostConfig"), dict) and container["HostConfig"].get("Privileged") is False, "native-tunnel-unproved")
            require(isinstance(container.get("Id"), str) and container["Id"].startswith(identifier), "native-tunnel-unproved")
            require(isinstance(container.get("Mounts"), list) and len(container["Mounts"]) <= 64, "native-tunnel-unproved")
            for mount in container["Mounts"]:
                require(isinstance(mount, dict) and isinstance(mount.get("Destination"), str), "native-tunnel-unproved")
                destination = mount["Destination"]
                source = mount.get("Source")
                if mount.get("Type") == "bind":
                    require(isinstance(source, str) and source.startswith("/") and source == os.path.normpath(source), "native-tunnel-unproved")
                    protected = self.worker.markers.path
                    overlaps = source == protected or source.startswith(protected + "/") or protected.startswith(source.rstrip("/") + "/")
                    exact = source == protected + "/transport" and destination == helper.CONTAINER_ROOT and mount.get("RW") is False
                    require(not overlaps or exact, "native-tunnel-unproved")
                    self.worker.prove_mount_source(source, exact)
                require(destination == os.path.normpath(destination) and destination.startswith("/"), "native-tunnel-unproved")
                require(not destination.startswith(helper.CONTAINER_ROOT + "/"), "native-tunnel-unproved")
                require(destination == helper.CONTAINER_ROOT or not helper.CONTAINER_ROOT.startswith(destination.rstrip("/") + "/"), "native-tunnel-unproved")
            mounts = [m for m in container["Mounts"] if isinstance(m, dict) and m.get("Destination") == helper.CONTAINER_ROOT]
            require(len(mounts) == 1 and mounts[0].get("Type") == "bind" and mounts[0].get("Source") == os.path.join(self.worker.markers.path, "transport")
                    and mounts[0].get("RW") is False, "native-tunnel-unproved")
        require(bool(seen) or bool(pending), "native-tunnel-unproved")
        return sorted(pending)

    def verify_tunnel_configuration(self):
        helper, unused = tunnel_helpers()
        wanted = helper.native_configuration(helper.targets(self.worker.request))
        require(self.worker.ceph_text("cephadm", "get-ssh-config") in (wanted, wanted + b"\n"), "native-tunnel-unproved")
        require(self.worker.ceph_text("cephadm", "get-user").strip() == self.worker.request["clusterSSHUser"].encode(), "native-tunnel-unproved")
