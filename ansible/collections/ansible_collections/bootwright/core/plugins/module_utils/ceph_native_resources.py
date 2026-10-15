"""Fixed Ceph pool and client worker phases, shipped beside the core worker as source.

A cluster request and a host scope never load this module: only a pool or client
action reaches its validation and its worker. Like the core worker it imports
only the target Python standard library and the core worker, and its native
output stays private until projected into evidence.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import hashlib
import json
import os

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    Custody, Refusal, canonical, named, read_regular, require, unique_object,
)

ACTIONS = ("pool", "client")
PHASES = frozenset(("pool", "client", "client_read"))
READ_ONLY_PHASES = frozenset(("client_read",))


def validate(request, hosts):
    """Admit one pool or client resource beside its validated cluster scope."""
    if request["action"] == "pool":
        pool = request.get("pool")
        require(isinstance(pool, dict) and set(pool) == {"name", "size", "minSize", "pgNum", "crushRule"})
        require(named(pool["name"]) and named(pool["crushRule"]))
        osd_hosts = sum(bool(host["devices"]) for host in hosts)
        require(type(pool["size"]) is int and type(pool["minSize"]) is int and 2 <= pool["minSize"] <= pool["size"] < osd_hosts)
        require(type(pool["pgNum"]) is int and 1 <= pool["pgNum"] <= 32768)
        require("client" not in request)
    elif request["action"] == "client":
        client = request.get("client")
        require(isinstance(client, dict) and set(client) == {"name", "pool", "access"})
        require(named(client["name"]) and named(client["pool"]))
        require(client["access"] in ("read-only", "read-write"))
        require("pool" not in request)


class ResourceWorker(ceph_native.Worker):
    """The core worker with one pool or client resource's phases and observation."""

    def caps(self):
        client = self.request["client"]
        profile = "rbd-read-only" if client["access"] == "read-only" else "rbd"
        return {"mon": "profile rbd", "mgr": "profile " + profile + " pool=" + client["pool"], "osd": "profile " + profile + " pool=" + client["pool"]}

    def pools(self):
        result = self.ceph("osd", "pool", "ls", "detail")
        require(isinstance(result, list), "native-output")
        return result

    @staticmethod
    def pool_id(entry):
        """`osd pool ls detail` names a pool's ID pool_id; only `osd dump` names it pool."""
        identifier = entry.get("pool_id") if isinstance(entry, dict) else None
        require(type(identifier) is int and identifier >= 0, "native-output")
        return identifier

    def pool(self):
        require(self.markers.owned(), "ownership")
        pool = self.request["pool"]
        name = pool["name"]
        mark = "pool-" + name + ".json"
        owned = self.markers.read(mark)
        require(owned is None or owned == pool, "ownership")
        entries = [p for p in self.pools() if p.get("pool_name") == name]
        require(owned is not None or not entries, "foreign-pool")
        self.markers.write(mark, pool)
        attempt = {"version": "ceph-pool-attempt-v1", "owner": self.request["owner"], "fsid": self.request["fsid"], "pool": pool}
        identity = self.markers.read("pool-id-" + name + ".json")
        if entries and identity is None:
            # A lost creation response resumes only from this operation's own
            # attempt record and one same-name pool with the frozen parameters.
            require(canonical(self.markers.read("pool-attempt-" + name + ".json")) == canonical(attempt), "pool-identity")
            require(len(entries) == 1 and self.created_pool(entries[0], pool), "pool-identity")
            self.markers.write("pool-id-" + name + ".json", {"id": self.pool_id(entries[0])})
            identity = self.markers.read("pool-id-" + name + ".json")
        if entries:
            require(identity is not None and identity == {"id": self.pool_id(entries[0])}, "pool-identity")
        else:
            require(identity is None, "pool-identity")
            self.markers.write("pool-attempt-" + name + ".json", attempt)
            self.ceph("osd", "pool", "create", name, str(pool["pgNum"]), "replicated", pool["crushRule"], json_result=False)
            entries = [p for p in self.pools() if p.get("pool_name") == name]
            require(len(entries) == 1, "pool-identity")
            self.markers.write("pool-id-" + name + ".json", {"id": self.pool_id(entries[0])})
        for field, wanted in (("size", pool["size"]), ("min_size", pool["minSize"])):
            self.pool_setting(pool, field, wanted)
        init = {"owner": self.request["owner"], "fsid": self.request["fsid"], "pool": pool,
                "id": self.markers.read("pool-id-" + name + ".json")["id"]}
        attempt_name, complete_name = "pool-rbd-attempt-" + name + ".json", "pool-rbd-complete-" + name + ".json"
        attempted, completed = self.markers.read(attempt_name), self.markers.read(complete_name)
        require(attempted is None or attempted == init, "pool-identity")
        require(completed is None or completed == init and attempted == init, "pool-identity")
        current = self.owned_pool(pool)
        require(completed is None or "rbd" in current.get("application_metadata", {}), "pool-identity")
        if completed is None:
            self.markers.write(attempt_name, init)
            proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
            with proof:
                self.command(["/usr/bin/rbd", "--conf", os.path.join(self.markers.path, "ceph.conf"), "--keyring",
                              os.path.join(self.markers.path, "ceph.client.admin.keyring"), "pool", "init", name], json_result=False)
            require("rbd" in self.owned_pool(pool).get("application_metadata", {}), "not-ready")
            self.markers.write(complete_name, init)
        return {"changed": completed is None}

    def owned_pool(self, pool):
        entries = [entry for entry in self.pools() if entry.get("pool_name") == pool["name"]]
        require(len(entries) == 1 and self.markers.read("pool-id-" + pool["name"] + ".json") == {"id": self.pool_id(entries[0])}, "pool-identity")
        return entries[0]

    def pool_setting(self, pool, field, wanted):
        current = self.owned_pool(pool)
        require(type(current.get(field)) is int and current[field] > 0, "native-output")
        prefix = "pool-setting-" + pool["name"] + "-" + field
        intent = {"owner": self.request["owner"], "fsid": self.request["fsid"], "id": self.pool_id(current), "field": field, "value": wanted}
        attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
        require(attempt is None or attempt == intent, "pool-identity")
        require(complete is None or complete == intent and attempt == intent, "pool-identity")
        require(complete is None or current[field] == wanted, "pool-identity")
        if attempt is None:
            self.markers.write(prefix + "-attempt.json", intent)
        if current[field] != wanted:
            self.ceph("osd", "pool", "set", pool["name"], field, str(wanted), json_result=False)
            require(self.owned_pool(pool).get(field) == wanted, "not-ready")
        if complete is None:
            self.markers.write(prefix + "-complete.json", intent)

    def created_pool(self, entry, pool):
        """The frozen creation parameters: a replicated pool on the frozen rule with the frozen PG count, which the PG autoscaler
        may already have moved when it is on, as it is by default."""
        rule = self.ceph("osd", "crush", "rule", "dump", pool["crushRule"])
        require(isinstance(rule, dict) and type(rule.get("rule_id")) is int, "native-output")
        self.pool_id(entry)
        autoscaled = entry.get("pg_autoscale_mode") == "on" and type(entry.get("pg_num")) is int and entry["pg_num"] >= 1
        return (entry.get("type") == 1 and entry.get("crush_rule") == rule["rule_id"]
                and (entry.get("pg_num") == pool["pgNum"] or autoscaled))

    def client(self):
        require(self.markers.owned(), "ownership")
        self.verify_tool()
        client = self.request["client"]
        name = "client." + client["name"]
        mark = "client-" + client["name"] + ".json"
        owned = self.markers.read(mark)
        require(owned is None or owned == client, "ownership")
        scope = self.client_scope()
        # Refuse old hash bindings and unbound material names without opening
        # their contents. Matching private bytes cannot create ownership.
        self.client_custody(scope)
        entries = self.ceph("auth", "ls")
        require(isinstance(entries, dict) and isinstance(entries.get("auth_dump"), list), "native-output")
        matching = [entry for entry in entries["auth_dump"] if isinstance(entry, dict) and entry.get("entity") == name]
        require(len(matching) <= 1, "client-identity")
        attempt_name = "client-attempt-" + client["name"] + ".json"
        complete_name = "client-applied-" + client["name"] + ".json"
        attempt, complete = self.markers.read(attempt_name), self.markers.read(complete_name)
        require(owned is not None or not matching, "foreign-client")
        require(attempt is not None or not matching, "foreign-client")
        require(complete is None or matching, "client-identity")
        self.markers.write(mark, client)
        material = self.prepare_client_material(scope)
        binding = self.client_binding(scope)
        require(attempt is None or canonical(attempt) == canonical(binding), "client-identity")
        require(complete is None or canonical(complete) == canonical(binding), "client-identity")
        if matching:
            require(matching == material, "client-identity")
        else:
            self.markers.write(attempt_name, binding)
            # Only the exact retained keyring reaches stdin. Native auth add
            # creates or checks an exact key/caps match; auth import would
            # overwrite an existing identity and is deliberately not used.
            self.client_material(scope)
            self.ceph("auth", "add", name, "-i", "-", data=self.client_keyring(material), json_result=False)
        result = self.ceph("auth", "get", name)
        require(result == material, "client-identity")
        self.client_material(scope)
        self.markers.write(complete_name, binding)
        # JSON is an opaque protected output; it never enters public evidence.
        return {"changed": not matching, "private": canonical(material).decode("utf-8")}

    def client_scope(self):
        """Bind confidential custody to the exact owned native pool incarnation."""
        require(self.request["action"] == "client" and self.markers.owned() and self.host["name"] == self.request["bootstrap"], "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        client = self.request["client"]
        pool = self.markers.read("pool-" + client["pool"] + ".json")
        require(isinstance(pool, dict) and pool.get("name") == client["pool"], "pool-unowned")
        identifier = self.markers.read("pool-id-" + client["pool"] + ".json")
        require(isinstance(identifier, dict) and set(identifier) == {"id"} and type(identifier["id"]) is int and identifier["id"] >= 0, "pool-identity")
        pools = self.ceph("osd", "pool", "ls", "detail")
        require(isinstance(pools, list) and len(pools) <= 65536, "native-output")
        selected = [entry for entry in pools if isinstance(entry, dict) and entry.get("pool_name") == client["pool"]]
        require(len(selected) == 1 and self.pool_id(selected[0]) == identifier["id"], "pool-identity")
        require(isinstance(selected[0].get("application_metadata"), dict) and "rbd" in selected[0]["application_metadata"], "pool-identity")
        return {"owner": self.request["owner"], "fsid": self.request["fsid"], "host": self.host["name"], "client": dict(client), "poolID": identifier["id"]}

    def client_store(self):
        return Custody(self, "client", "-" + self.request["client"]["name"], 65536, self.client_material_decode)

    def client_custody(self, scope):
        return self.client_store().records(scope)

    def client_material_decode(self, data):
        try:
            result = json.loads(data, object_pairs_hook=unique_object)
            require(isinstance(result, list) and len(result) == 1 and isinstance(result[0], dict) and set(result[0]) == {"entity", "key", "caps"},
                    "client-identity")
            entry = result[0]
            require(entry["entity"] == "client." + self.request["client"]["name"] and entry["caps"] == self.caps(), "client-identity")
            key = entry["key"]
            require(isinstance(key, str) and 0 < len(key) <= 1024, "client-identity")
            decoded = base64.b64decode(key, validate=True)
            require(0 < len(decoded) <= 256 and base64.b64encode(decoded).decode("ascii") == key and canonical(result) == data, "client-identity")
            return result
        except (ValueError, UnicodeError, TypeError, KeyError):
            raise Refusal("client-identity") from None

    def verify_client_generator(self):
        self.verify_tool()
        path = "/usr/bin/ceph-authtool"
        owned = self.command(["/usr/bin/rpm", "-qf", "--qf", "%{NAME} " + ceph_native.PACKAGE_QUERY, path], json_result=False)
        require(owned == b"ceph-common " + self.decided_package("ceph-common"), "artifact")
        content = read_regular(path, maximum=64 * 1024 * 1024)
        identity = {"path": path, "packageVersion": self.request["cephadm"]["packageVersion"], "sha256": hashlib.sha256(content).hexdigest()}
        self.markers.write("client-generator.json", identity)

    def generate_client_key(self):
        name = self.request["client"]["name"]
        self.verify_client_generator()
        output = self.command(["/usr/bin/ceph-authtool", "--gen-print-key", "--name", "client." + name], json_result=False)
        try:
            require(isinstance(output, bytes) and len(output) <= 1025 and output.endswith(b"\n") and output.count(b"\n") == 1, "client-identity")
            key = output[:-1].decode("ascii")
        except UnicodeError:
            raise Refusal("client-identity") from None
        return canonical([{"entity": "client." + name, "key": key, "caps": self.caps()}])

    def prepare_client_material(self, scope):
        return self.client_store().prepare(scope, self.generate_client_key)

    def client_material(self, scope):
        return self.client_store().material(scope)

    def client_binding(self, scope):
        return self.client_store().binding(scope, "ceph-client-auth-v1")

    def prove_client_auth_binding(self, scope):
        self.client_store().prove_binding(self.client_binding(scope))

    @staticmethod
    def client_keyring(material):
        entry = material[0]
        result = "[" + entry["entity"] + "]\n\tkey = " + entry["key"] + "\n"
        for name, value in sorted(entry["caps"].items()):
            result += "\tcaps " + name + ' = "' + value + '"\n'
        return result.encode("ascii")

    def client_read(self):
        """Recover produced material read-only after an interrupted handoff."""
        self.verify_tool()
        require(self.request["action"] == "client" and self.markers.owned(), "ownership")
        client = self.request["client"]
        require(self.markers.read("client-" + client["name"] + ".json") == client, "ownership")
        scope = self.client_scope()
        self.prove_client_auth_binding(scope)
        material = self.client_material(scope)
        result = self.ceph("auth", "get", "client." + client["name"])
        require(result == material, "client-identity")
        self.client_material(scope)
        return {"private": canonical(material).decode("utf-8")}

    def observe_resource(self, evidence, ready):
        if self.request["action"] not in ACTIONS:
            return super().observe_resource(evidence, ready)
        action = self.request["action"]
        if action == "pool":
            pool = self.request["pool"]
            entries = [p for p in self.pools() if p.get("pool_name") == pool["name"]]
            owned = self.markers.read("pool-" + pool["name"] + ".json") == pool
            require(owned or not entries, "foreign-pool")
            native_id = self.pool_id(entries[0]) if entries else -1
            identity = self.markers.read("pool-id-" + pool["name"] + ".json")
            attempt = {"version": "ceph-pool-attempt-v1", "owner": self.request["owner"], "fsid": self.request["fsid"], "pool": pool}
            # A creation whose reply was lost has this operation's attempt record and no recorded ID yet: unfinished work the
            # apply resumes, never a refusal; any other unrecorded or different ID still refuses.
            unrecorded = (bool(entries) and identity is None
                          and canonical(self.markers.read("pool-attempt-" + pool["name"] + ".json")) == canonical(attempt))
            require(not entries or unrecorded or identity == {"id": native_id}, "pool-identity")
            evidence["owned"] = owned
            # Native absence does not remove retained owned cleanup work.
            evidence["exists"] = owned or bool(entries)
            evidence["resource"] = {"id": "" if unrecorded or not entries else str(native_id), "path": ""}
            ready = ready and not unrecorded
            ready = (ready and bool(entries) and entries[0].get("size") == pool["size"] and entries[0].get("min_size") == pool["minSize"] and
                     "rbd" in entries[0].get("application_metadata", {}))
        elif action == "client":
            client = self.request["client"]
            auth = self.ceph("auth", "ls")
            require(isinstance(auth, dict) and isinstance(auth.get("auth_dump"), list), "native-output")
            entries = [e for e in auth["auth_dump"] if e.get("entity") == "client." + client["name"]]
            owned = self.markers.read("client-" + client["name"] + ".json") == client
            require(owned or not entries, "foreign-client")
            require(len(entries) <= 1, "client-identity")
            if entries:
                scope = self.client_scope()
                self.prove_client_auth_binding(scope)
                require(entries == self.client_material(scope), "client-identity")
            evidence["owned"] = owned
            # Retained ownership and key custody still require a complete inverse.
            evidence["exists"] = owned or bool(entries)
            evidence["client"] = "client." + client["name"] if entries else ""
            ready = ready and bool(entries) and entries[0].get("caps") == self.caps()
        return ready
