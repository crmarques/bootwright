"""Fixed Ceph pool, placement policy, client and RBD image worker phases, shipped beside the core worker as source.

A cluster request and a host scope never load this module: only a pool, policy, client or
RBD image action reaches its validation and its worker. Like the core worker it imports
only the target Python standard library and the core worker, and its native
output stays private until projected into evidence.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import functools
import hashlib
import json
import os
import re

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    Custody, Refusal, canonical, named, read_regular, require, unique_object,
)

ACTIONS = ("pool", "client", "policy", "rbd")
PHASES = frozenset(("pool", "client", "client_read", "policy", "rbd"))
READ_ONLY_PHASES = frozenset(("client_read",))

POOL_ATTEMPT = "ceph-pool-attempt-v2"
POLICY_ATTEMPT = "ceph-policy-attempt-v1"
RBD_ATTEMPT = "ceph-rbd-image-attempt-v1"
RBD_MEMBERS = frozenset(("dataPool", "features", "name", "pool", "sizeBytes"))
# librbd's feature-bit order (src/tools/rbd/ArgumentTypes.cc:20-33), the order `rbd info` lists them in.
RBD_FEATURES = ("layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling")
# librbd refuses an object map over 256000000 objects (src/librbd/ObjectMap.cc:73-76); rbd_default_order 22 makes them 4 MiB.
RBD_OBJECT_MAP_BYTES = 256000000 << 22
# Each supported erasure-code profile row and the OSD hosts it needs; jerasure only on its three rows.
ERASURE_ROWS = {(2, 2): 5, (4, 2): 6, (5, 2): 7, (6, 2): 8, (8, 3): 11, (8, 4): 12}
JERASURE_ROWS = frozenset(((4, 2), (8, 3), (8, 4)))
APPLICATIONS = {"rbd": "rbd", "cephfs-metadata": "cephfs", "cephfs-data": "cephfs", "rgw": "rgw"}
POOL_MEMBERS = frozenset(("application", "autoscale", "compression", "crushRule", "minSize", "name", "overwrites", "pgNum", "quota", "role", "size", "type"))
ERASURE_MEMBERS = frozenset(("codingChunks", "dataChunks", "deviceClass", "failureDomain", "plugin", "profile", "profileDefaults", "root", "stripeUnit",
                             "technique"))
PROFILE_DEFAULTS = frozenset(("crush-device-class", "crush-num-failure-domains", "crush-osds-per-failure-domain", "w", "jerasure-per-chunk-alignment"))
DEVICE_CLASS = re.compile(r"[A-Za-z0-9_.-]{1,64}")
PROFILE_VALUE = re.compile(r"[A-Za-z0-9_.-]{0,32}")
DECIMAL = re.compile(r"(?:0|[1-9][0-9]*)(?:\.[0-9]+)?")
INT64 = (1 << 63) - 1
# The pool options `osd pool ls detail` lists by name once set, each with the frozen member it must equal; unset means absent.
OWNED_OPTIONS = (("pg_num_min", "autoscale", "minPGs"), ("pg_num_max", "autoscale", "maxPGs"), ("target_size_bytes", "autoscale", "targetBytes"),
                 ("target_size_ratio", "autoscale", "targetRatio"), ("compression_mode", "compression", "mode"),
                 ("compression_algorithm", "compression", "algorithm"), ("compression_required_ratio", "compression", "requiredRatio"),
                 ("compression_min_blob_size", "compression", "minBlob"), ("compression_max_blob_size", "compression", "maxBlob"))
RATIO_OPTIONS = frozenset(("target_size_ratio", "compression_required_ratio"))
COMPRESSION_SETTERS = (("mode", "compression_mode"), ("algorithm", "compression_algorithm"), ("requiredRatio", "compression_required_ratio"),
                       ("minBlob", "compression_min_blob_size"), ("maxBlob", "compression_max_blob_size"))


def validate(request, hosts):
    """Admit one pool, placement policy, client or RBD image resource beside its validated cluster scope."""
    osd_hosts = sum(bool(host["devices"]) for host in hosts)
    require(request["action"] == "rbd" or "rbd" not in request)
    if request["action"] == "pool":
        require("client" not in request and "policy" not in request)
        validate_pool(request.get("pool"), osd_hosts)
    elif request["action"] == "policy":
        require("pool" not in request and "client" not in request)
        validate_policy(request.get("policy"), osd_hosts)
    elif request["action"] == "client":
        client = request.get("client")
        require(isinstance(client, dict) and set(client) == {"name", "pool", "access"})
        require(named(client["name"]) and named(client["pool"]))
        require(client["access"] in ("read-only", "read-write"))
        require("pool" not in request and "policy" not in request)
    elif request["action"] == "rbd":
        require("pool" not in request and "client" not in request and "policy" not in request)
        validate_rbd(request.get("rbd"))


def validate_rbd(image):
    """The frozen image the Go admission proved: known features in bit order, librbd's dependencies, the object-map bound."""
    require(isinstance(image, dict) and set(image) == RBD_MEMBERS)
    require(named(image["name"]) and "." not in image["name"] and named(image["pool"]))
    require(image["dataPool"] == "" or named(image["dataPool"]) and image["dataPool"] != image["pool"])
    require(type(image["sizeBytes"]) is int and 1 <= image["sizeBytes"] <= INT64)
    features = image["features"]
    require(isinstance(features, list) and features == [feature for feature in RBD_FEATURES if feature in features])
    require("fast-diff" not in features or "object-map" in features)
    require("object-map" not in features or "exclusive-lock" in features and "fast-diff" in features)
    require("journaling" not in features or "exclusive-lock" in features)
    require("object-map" not in features or image["sizeBytes"] <= RBD_OBJECT_MAP_BYTES)


def bounded(value, maximum):
    return type(value) is int and 0 <= value <= maximum


def device_class(value):
    return value == "" or isinstance(value, str) and DEVICE_CLASS.fullmatch(value) is not None


def ratio(value, maximum=None):
    """A frozen decimal ratio: unset as "", else positive and within its native bound."""
    if value == "":
        return True
    return isinstance(value, str) and DECIMAL.fullmatch(value) is not None and 0 < float(value) and (maximum is None or float(value) <= maximum)


def validate_policy(policy, osd_hosts):
    require(isinstance(policy, dict) and set(policy) == {"deviceClass", "failureDomain", "name", "root", "rule", "size"})
    require(named(policy["name"]) and named(policy["rule"]) and policy["rule"] != "replicated_rule")
    require(policy["failureDomain"] == "host" and policy["root"] == "default" and device_class(policy["deviceClass"]))
    require(type(policy["size"]) is int and 2 <= policy["size"] <= osd_hosts)


def validate_pool(pool, osd_hosts):
    require(isinstance(pool, dict) and pool.get("type") in ("replicated", "erasure"))
    erasure = pool["type"] == "erasure"
    require(set(pool) == POOL_MEMBERS | ({"erasure"} if erasure else set()))
    require(named(pool["name"]) and named(pool["crushRule"]) and isinstance(pool["role"], str) and pool["role"] in APPLICATIONS)
    require(pool["application"] == APPLICATIONS[pool["role"]])
    require(type(pool["size"]) is int and type(pool["minSize"]) is int and type(pool["overwrites"]) is bool)
    if erasure:
        validate_erasure(pool, osd_hosts)
    else:
        require(2 <= pool["minSize"] <= pool["size"] < osd_hosts and pool["overwrites"] is False)
    validate_autoscale(pool)
    compression = pool["compression"]
    require(isinstance(compression, dict) and set(compression) == {"algorithm", "maxBlob", "minBlob", "mode", "requiredRatio"})
    require(compression["mode"] in ("", "none", "passive", "aggressive", "force") and compression["algorithm"] in ("", "lz4", "snappy", "zlib", "zstd"))
    require(bounded(compression["minBlob"], INT64) and bounded(compression["maxBlob"], INT64) and ratio(compression["requiredRatio"], 1.0))
    unset = compression["algorithm"] == compression["requiredRatio"] == "" and compression["minBlob"] == compression["maxBlob"] == 0
    require(compression["mode"] != "" or unset)
    quota = pool["quota"]
    require(isinstance(quota, dict) and set(quota) == {"maxBytes", "maxObjects"} and bounded(quota["maxBytes"], INT64) and bounded(quota["maxObjects"], INT64))


def validate_erasure(pool, osd_hosts):
    erasure = pool["erasure"]
    require(isinstance(erasure, dict) and set(erasure) == ERASURE_MEMBERS)
    row = (erasure["dataChunks"], erasure["codingChunks"])
    require(all(type(value) is int for value in row) and row in ERASURE_ROWS and osd_hosts >= ERASURE_ROWS[row])
    require(erasure["plugin"] == "isa" or erasure["plugin"] == "jerasure" and row in JERASURE_ROWS)
    require(erasure["technique"] == "reed_sol_van" and erasure["failureDomain"] == "host" and erasure["root"] == "default")
    require(erasure["profile"] == pool["name"] == pool["crushRule"] and device_class(erasure["deviceClass"]))
    require(type(erasure["stripeUnit"]) is int and erasure["stripeUnit"] > 0 and erasure["stripeUnit"] % 4096 == 0)
    defaults = erasure["profileDefaults"]
    require(isinstance(defaults, dict) and set(defaults) <= PROFILE_DEFAULTS)
    require(all(isinstance(value, str) and PROFILE_VALUE.fullmatch(value) is not None for value in defaults.values()))
    require(pool["size"] == row[0] + row[1] and pool["minSize"] == row[0] + 1 and pool["overwrites"] == (pool["role"] in ("rbd", "cephfs-data")))


def validate_autoscale(pool):
    autoscale = pool["autoscale"]
    require(isinstance(autoscale, dict) and set(autoscale) == {"bulk", "maxPGs", "minPGs", "mode", "targetBytes", "targetRatio"})
    require(type(autoscale["bulk"]) is bool and autoscale["mode"] in ("on", "off"))
    require(bounded(autoscale["minPGs"], 32768) and bounded(autoscale["maxPGs"], 32768) and bounded(autoscale["targetBytes"], INT64))
    require(ratio(autoscale["targetRatio"]) and not (autoscale["targetBytes"] and autoscale["targetRatio"]))
    pgs = 32
    if autoscale["minPGs"] and pgs < autoscale["minPGs"]:
        pgs = autoscale["minPGs"]
    if autoscale["maxPGs"] and pgs > autoscale["maxPGs"]:
        pgs = autoscale["maxPGs"]
    require(type(pool["pgNum"]) is int and 1 <= pool["pgNum"] <= 32768 and pool["pgNum"] == pgs)


def take_name(device_class_name):
    """The CRUSH root a class-restricted rule takes: the class's shadow root of `default`."""
    return "default" + ("~" + device_class_name if device_class_name else "")


class ResourceWorker(ceph_native.Worker):
    """The core worker with one pool, placement policy or client resource's phases and observation."""

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
        recorded = canonical(self.markers.read("pool-attempt-" + name + ".json")) == canonical(self.pool_attempt())
        identity = self.markers.read("pool-id-" + name + ".json")
        created = False
        if entries and identity is None:
            # A lost creation response resumes only from this operation's own
            # attempt record and one same-name pool with the frozen parameters.
            require(recorded, "pool-identity")
            require(len(entries) == 1 and self.created_pool(entries[0], pool), "pool-identity")
            self.markers.write("pool-id-" + name + ".json", {"id": self.pool_id(entries[0])})
            identity = self.markers.read("pool-id-" + name + ".json")
        if entries:
            require(identity is not None and identity == {"id": self.pool_id(entries[0])}, "pool-identity")
        else:
            require(identity is None, "pool-identity")
            if pool["type"] == "erasure":
                self.erasure_profile(pool, recorded)
            elif not recorded:
                self.markers.write("pool-attempt-" + name + ".json", self.pool_attempt())
            self.create_pool(pool)
            created = True
            entries = [p for p in self.pools() if p.get("pool_name") == name]
            require(len(entries) == 1, "pool-identity")
            self.markers.write("pool-id-" + name + ".json", {"id": self.pool_id(entries[0])})
        changed = created
        if pool["type"] == "replicated":
            for field, wanted in (("size", pool["size"]), ("min_size", pool["minSize"])):
                changed = self.markers.read("pool-setting-" + name + "-" + field + "-complete.json") is None or changed
                self.pool_setting(pool, field, wanted)
        for field, wanted, observed, argv in self.pool_options(pool):
            changed = self.pool_option(pool, field, wanted, observed, argv) or changed
        if pool["type"] == "replicated" and pool["application"] == "rbd":
            init = {"owner": self.request["owner"], "fsid": self.request["fsid"], "pool": pool,
                    "id": self.markers.read("pool-id-" + name + ".json")["id"]}
            attempt_name, complete_name = "pool-rbd-attempt-" + name + ".json", "pool-rbd-complete-" + name + ".json"
            attempted, completed = self.markers.read(attempt_name), self.markers.read(complete_name)
            require(attempted is None or attempted == init, "pool-identity")
            require(completed is None or completed == init and attempted == init, "pool-identity")
            current = self.owned_pool(pool["name"])
            require(completed is None or "rbd" in current.get("application_metadata", {}), "pool-identity")
            if completed is None:
                self.markers.write(attempt_name, init)
                proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
                with proof:
                    self.command(["/usr/bin/rbd", "--conf", os.path.join(self.markers.path, "ceph.conf"), "--keyring",
                                  os.path.join(self.markers.path, "ceph.client.admin.keyring"), "pool", "init", name], json_result=False)
                require("rbd" in self.owned_pool(pool["name"]).get("application_metadata", {}), "not-ready")
                self.markers.write(complete_name, init)
            changed = completed is None or changed
        return {"changed": changed}

    def pool_setting(self, pool, field, wanted):
        current = self.owned_pool(pool["name"])
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
            require(self.owned_pool(pool["name"]).get(field) == wanted, "not-ready")
        if complete is None:
            self.markers.write(prefix + "-complete.json", intent)

    def pool_attempt(self):
        return {"version": POOL_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "pool": self.request["pool"]}

    def erasure_profile(self, pool, recorded):
        """Prove the pool's own profile and derived rule are absent, or this operation's, before the create that derives the rule."""
        name, erasure = pool["name"], pool["erasure"]
        hosts = self.osd_tree_hosts(erasure["deviceClass"])
        require(hosts >= ERASURE_ROWS[(erasure["dataChunks"], erasure["codingChunks"])], "crush-capacity")
        profiles, rules = self.profile_names(), self.rule_names()
        if not recorded:
            require(name not in profiles, "foreign-profile")
            require(name not in rules, "foreign-rule")
            self.markers.write("pool-attempt-" + name + ".json", self.pool_attempt())
        elif name in rules:
            require(self.erasure_rule_matches(self.rule_dump(name), pool), "foreign-rule")
        if name not in profiles:
            self.ceph("osd", "erasure-code-profile", "set", name, *(key + "=" + value for key, value in self.profile_passed(pool)), json_result=False)
        require(self.profile_matches(pool), "native-output")

    @staticmethod
    def profile_passed(pool):
        """The profile keys the set passes, in argv order; never --force."""
        erasure = pool["erasure"]
        passed = [("k", str(erasure["dataChunks"])), ("m", str(erasure["codingChunks"])), ("plugin", erasure["plugin"]),
                  ("technique", erasure["technique"]), ("crush-failure-domain", erasure["failureDomain"]), ("crush-root", erasure["root"])]
        if erasure["deviceClass"]:
            passed.append(("crush-device-class", erasure["deviceClass"]))
        return passed + [("stripe_unit", str(erasure["stripeUnit"]))]

    def profile_names(self):
        result = self.ceph("osd", "erasure-code-profile", "ls")
        require(isinstance(result, list) and len(result) <= 65536 and all(isinstance(item, str) for item in result), "native-output")
        return result

    def profile_matches(self, pool):
        """The monitor stores the normalized profile: exactly the passed keys plus the plugin's frozen defaults."""
        stored = self.ceph("osd", "erasure-code-profile", "get", pool["name"])
        require(isinstance(stored, dict) and len(stored) <= 64 and all(isinstance(key, str) and isinstance(value, str) for key, value in stored.items()),
                "native-output")
        expected = dict(self.profile_passed(pool))
        expected.update(pool["erasure"]["profileDefaults"])
        return stored == expected

    def create_pool(self, pool):
        """One create carrying every frozen creation parameter; the monitor's PG refusal is its own reason."""
        name, autoscale = pool["name"], pool["autoscale"]
        args = ["osd", "pool", "create", name, str(pool["pgNum"]), str(pool["pgNum"])]
        args += ["erasure", name] if pool["type"] == "erasure" else ["replicated", pool["crushRule"]]
        args += ["--autoscale_mode", autoscale["mode"]]
        for flag, member in (("--pg_num_min", "minPGs"), ("--pg_num_max", "maxPGs"), ("--target_size_bytes", "targetBytes")):
            if autoscale[member]:
                args += [flag, str(autoscale[member])]
        if autoscale["bulk"]:
            args.append("--bulk")
        if autoscale["targetRatio"]:
            args += ["--target_size_ratio", autoscale["targetRatio"]]
        code = self.ceph_status(*args)
        require(code != 34, "pg-budget")
        require(code == 0, "native-command")

    @staticmethod
    def pool_options(pool):
        """Each setting beyond creation and the replicated size pair: its record field, frozen value, observed-value check and argv."""
        name, quota = pool["name"], pool["quota"]
        settings = []
        if pool["overwrites"]:
            settings.append(("allow_ec_overwrites", True, ResourceWorker.ec_overwrites, ("set", name, "allow_ec_overwrites", "true")))
        for field, wanted in (("quota_max_bytes", quota["maxBytes"]), ("quota_max_objects", quota["maxObjects"])):
            settings.append((field, wanted, functools.partial(ResourceWorker.field_equals, field=field, wanted=wanted),
                             ("set-quota", name, field[len("quota_"):], str(wanted))))
        for member, option in COMPRESSION_SETTERS:
            wanted = pool["compression"][member]
            if wanted:
                settings.append((option, wanted, functools.partial(ResourceWorker.option_equals, option=option, wanted=wanted),
                                 ("set", name, option, str(wanted))))
        if pool["type"] != "replicated" or pool["application"] != "rbd":
            application = pool["application"]
            settings.append(("application", application, lambda entry: application in ResourceWorker.applications(entry),
                             ("application", "enable", name, application)))
        return settings

    @staticmethod
    def field_equals(entry, field, wanted):
        return entry.get(field) == wanted

    @staticmethod
    def option_equals(entry, option, wanted):
        return ResourceWorker.option_matches(entry.get("options"), option, wanted)

    @staticmethod
    def ec_overwrites(entry):
        flags = entry.get("flags_names")
        return isinstance(flags, str) and "ec_overwrites" in flags.split(",")

    @staticmethod
    def applications(entry):
        metadata = entry.get("application_metadata")
        return metadata if isinstance(metadata, dict) else {}

    def pool_option(self, pool, field, wanted, observed, argv):
        """One setting under the size pair's records, keyed by the numeric pool ID: the attempt before the effect, the setter only while
        the completion is absent and the observed value differs, the completion after the readback. A completed setting is never
        sent again; its later drift reads as not ready. Returns whether the completion was absent."""
        current = self.owned_pool(pool["name"])
        prefix = "pool-setting-" + pool["name"] + "-" + field
        intent = {"owner": self.request["owner"], "fsid": self.request["fsid"], "id": self.pool_id(current), "field": field, "value": wanted}
        attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
        require(attempt is None or attempt == intent, "pool-identity")
        require(complete is None or complete == intent and attempt == intent, "pool-identity")
        if complete is not None:
            return False
        if attempt is None:
            self.markers.write(prefix + "-attempt.json", intent)
        if not observed(current):
            self.ceph("osd", "pool", *argv, json_result=False)
            require(observed(self.owned_pool(pool["name"])), "not-ready")
        self.markers.write(prefix + "-complete.json", intent)
        return True

    def created_pool(self, entry, pool):
        """The frozen creation parameters: the frozen type on the frozen rule with the frozen PG count, which the PG autoscaler
        may already have moved when it is on, as it is by default."""
        rule = self.rule_dump(pool["crushRule"])
        self.pool_id(entry)
        autoscaled = entry.get("pg_autoscale_mode") == "on" and type(entry.get("pg_num")) is int and entry["pg_num"] >= 1
        return (entry.get("type") == (3 if pool["type"] == "erasure" else 1) and entry.get("crush_rule") == rule["rule_id"]
                and (entry.get("pg_num") == pool["pgNum"] or autoscaled)
                and (pool["type"] != "erasure" or entry.get("erasure_code_profile") == pool["name"]))

    def rule_names(self):
        result = self.ceph("osd", "crush", "rule", "ls")
        require(isinstance(result, list) and len(result) <= 65536 and all(isinstance(item, str) for item in result), "native-output")
        return result

    def rule_dump(self, name):
        rule = self.ceph("osd", "crush", "rule", "dump", name)
        require(isinstance(rule, dict) and type(rule.get("rule_id")) is int and rule["rule_id"] >= 0 and rule.get("rule_name") == name
                and type(rule.get("type")) is int and isinstance(rule.get("steps"), list), "native-output")
        return rule

    @staticmethod
    def rule_steps(rule, prefix, take, chooseleaf):
        """The frozen steps around one take of the expected root; the take's numeric item is the monitor's."""
        steps = rule["steps"]
        tail = [{"op": chooseleaf, "num": 0, "type": "host"}, {"op": "emit"}]
        if len(steps) != len(prefix) + 1 + len(tail):
            return False
        head = steps[len(prefix)]
        return (steps[:len(prefix)] == prefix and steps[len(prefix) + 1:] == tail and isinstance(head, dict)
                and set(head) == {"op", "item", "item_name"} and head["op"] == "take" and type(head["item"]) is int and head["item_name"] == take)

    def replicated_rule_matches(self, rule, policy):
        return rule["type"] == 1 and self.rule_steps(rule, [], take_name(policy["deviceClass"]), "chooseleaf_firstn")

    def erasure_rule_matches(self, rule, pool):
        prefix = [{"op": "set_chooseleaf_tries", "num": 5}, {"op": "set_choose_tries", "num": 100}]
        return rule["type"] == 3 and self.rule_steps(rule, prefix, take_name(pool["erasure"]["deviceClass"]), "chooseleaf_indep")

    def osd_tree_hosts(self, device_class_name):
        """The hosts under root `default` holding at least one OSD of the class, or of any class when none is named."""
        tree = self.ceph("osd", "tree")
        require(isinstance(tree, dict) and isinstance(tree.get("nodes"), list) and len(tree["nodes"]) <= 65536, "native-output")
        nodes = {}
        for node in tree["nodes"]:
            require(isinstance(node, dict) and type(node.get("id")) is int and isinstance(node.get("type"), str) and isinstance(node.get("name"), str),
                    "native-output")
            require(node["id"] not in nodes, "native-output")
            children = node.get("children", [])
            require(isinstance(children, list) and all(type(child) is int for child in children), "native-output")
            nodes[node["id"]] = node
        roots = [node["id"] for node in nodes.values() if node["type"] == "root" and node["name"] == "default"]
        require(len(roots) == 1, "native-output")
        hosts, pending, seen = set(), roots, set()
        while pending:
            identifier = pending.pop()
            require(identifier in nodes, "native-output")
            node = nodes[identifier]
            if identifier in seen or node["type"] == "osd":
                continue
            seen.add(identifier)
            children = node.get("children", [])
            if node["type"] != "host":
                pending.extend(children)
                continue
            require(all(child in nodes for child in children), "native-output")
            if any(nodes[child]["type"] == "osd" and (not device_class_name or nodes[child].get("device_class") == device_class_name) for child in children):
                hosts.add(node["name"])
        return len(hosts)

    def policy(self):
        """Create one owned replicated rule, or adopt this operation's own after a lost reply; never another's of the same name."""
        require(self.markers.owned(), "ownership")
        policy = self.request["policy"]
        name, rule = policy["name"], policy["rule"]
        mark = "policy-" + name + ".json"
        owned = self.markers.read(mark)
        require(owned is None or owned == policy, "ownership")
        existed = rule in self.rule_names()
        attempt = {"version": POLICY_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "policy": policy}
        recorded = canonical(self.markers.read("policy-attempt-" + name + ".json")) == canonical(attempt)
        require(recorded or not existed, "foreign-rule")
        require(existed or self.osd_tree_hosts(policy["deviceClass"]) >= policy["size"], "crush-capacity")
        self.markers.write(mark, policy)
        identity = self.markers.read("policy-id-" + name + ".json")
        if existed:
            dump = self.rule_dump(rule)
            if identity is None:
                require(self.replicated_rule_matches(dump, policy), "foreign-rule")
                self.markers.write("policy-id-" + name + ".json", {"id": dump["rule_id"]})
            else:
                require(identity == {"id": dump["rule_id"]}, "foreign-rule")
            return {"changed": False}
        require(identity is None, "foreign-rule")
        self.markers.write("policy-attempt-" + name + ".json", attempt)
        self.ceph("osd", "crush", "rule", "create-replicated", rule, policy["root"], policy["failureDomain"],
                  *([policy["deviceClass"]] if policy["deviceClass"] else []), json_result=False)
        dump = self.rule_dump(rule)
        require(self.replicated_rule_matches(dump, policy), "foreign-rule")
        self.markers.write("policy-id-" + name + ".json", {"id": dump["rule_id"]})
        return {"changed": True}

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
            ready = self.observe_pool(evidence, ready)
        elif action == "policy":
            ready = self.observe_policy(evidence, ready)
        elif action == "rbd":
            ready = self.observe_rbd(evidence, ready)
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

    def observe_pool(self, evidence, ready):
        pool = self.request["pool"]
        entries = [p for p in self.pools() if p.get("pool_name") == pool["name"]]
        owned = self.markers.read("pool-" + pool["name"] + ".json") == pool
        require(owned or not entries, "foreign-pool")
        native_id = self.pool_id(entries[0]) if entries else -1
        identity = self.markers.read("pool-id-" + pool["name"] + ".json")
        # A creation whose reply was lost has this operation's attempt record and no recorded ID yet: unfinished work the
        # apply resumes, never a refusal; any other unrecorded or different ID still refuses.
        unrecorded = (bool(entries) and identity is None
                      and canonical(self.markers.read("pool-attempt-" + pool["name"] + ".json")) == canonical(self.pool_attempt()))
        require(not entries or unrecorded or identity == {"id": native_id}, "pool-identity")
        evidence["owned"] = owned
        # Native absence does not remove retained owned cleanup work.
        evidence["exists"] = owned or bool(entries)
        evidence["resource"] = {"id": "" if unrecorded or not entries else str(native_id), "path": ""}
        return ready and not unrecorded and bool(entries) and self.pool_ready(entries[0], pool)

    def pool_ready(self, entry, pool):
        """Every frozen attribute as observed; a mismatch is not ready, never a refusal."""
        flags = entry.get("flags_names")
        flags = set(flags.split(",")) if isinstance(flags, str) else set()
        erasure = pool.get("erasure")
        ready = (entry.get("type") == (3 if erasure else 1) and entry.get("crush_rule") == self.rule_dump(pool["crushRule"])["rule_id"]
                 and entry.get("size") == pool["size"] and entry.get("min_size") == pool["minSize"] and ("ec_overwrites" in flags) == pool["overwrites"])
        if ready and erasure:
            ready = (entry.get("erasure_code_profile") == pool["name"] and entry.get("stripe_width") == erasure["dataChunks"] * erasure["stripeUnit"]
                     and self.profile_matches(pool))
        autoscale = pool["autoscale"]
        ready = (ready and ("bulk" in flags) == autoscale["bulk"] and entry.get("pg_autoscale_mode") == autoscale["mode"]
                 and (autoscale["mode"] != "off" or entry.get("pg_num") == pool["pgNum"]))
        ready = (ready and entry.get("quota_max_bytes") == pool["quota"]["maxBytes"] and entry.get("quota_max_objects") == pool["quota"]["maxObjects"])
        return ready and self.options_match(entry.get("options", {}), pool) and self.application_matches(entry.get("application_metadata"), pool)

    @staticmethod
    def options_match(options, pool):
        """Each owned option is absent when its frozen value is unset, else equal; other owners' options are not read."""
        if not isinstance(options, dict):
            return False
        return all(ResourceWorker.option_matches(options, option, pool[block][member]) for option, block, member in OWNED_OPTIONS)

    @staticmethod
    def option_matches(options, option, frozen):
        """One owned option: absent when its frozen value is unset, else equal by its type."""
        if not isinstance(options, dict):
            return False
        observed = options.get(option)
        if frozen in (0, ""):
            return option not in options
        if option in RATIO_OPTIONS:
            return type(observed) in (int, float) and float(observed) == float(frozen)
        if type(frozen) is int:
            return type(observed) is int and observed == frozen
        return observed == frozen

    @staticmethod
    def application_matches(metadata, pool):
        """Exactly the derived application; a CephFS pool may carry only the one key `fs new` records for its role."""
        if not isinstance(metadata, dict) or set(metadata) != {pool["application"]}:
            return False
        if pool["application"] != "cephfs":
            return True
        value = metadata["cephfs"]
        key = "data" if pool["role"] == "cephfs-data" else "metadata"
        return value == {} or isinstance(value, dict) and set(value) == {key} and named(value[key])

    def observe_policy(self, evidence, ready):
        policy = self.request["policy"]
        name = policy["name"]
        exists = policy["rule"] in self.rule_names()
        owned = self.markers.read("policy-" + name + ".json") == policy
        require(owned or not exists, "foreign-rule")
        dump = self.rule_dump(policy["rule"]) if exists else None
        identity = self.markers.read("policy-id-" + name + ".json")
        attempt = {"version": POLICY_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "policy": policy}
        # As for a pool: this operation's own lost creation reply is unfinished work; any other unrecorded or different ID refuses.
        unrecorded = exists and identity is None and canonical(self.markers.read("policy-attempt-" + name + ".json")) == canonical(attempt)
        require(not exists or unrecorded or identity == {"id": dump["rule_id"]}, "foreign-rule")
        evidence["owned"] = owned
        evidence["exists"] = owned or exists
        evidence["resource"] = {"id": "" if unrecorded or not exists else str(dump["rule_id"]), "path": ""}
        return ready and exists and not unrecorded and self.replicated_rule_matches(dump, policy)

    def rbd_cli(self, *args, json_result=True):
        """One rbd command with the operation's own admin custody, under the bootstrap hold as every native call."""
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        with proof:
            return self.command(["/usr/bin/rbd", "--conf", os.path.join(self.markers.path, "ceph.conf"), "--keyring",
                                 os.path.join(self.markers.path, "ceph.client.admin.keyring"), *args], json_result=json_result)

    def rbd_names(self, pool):
        result = self.rbd_cli("ls", "--format", "json", pool)
        require(isinstance(result, list) and len(result) <= 1048576 and all(isinstance(item, str) for item in result), "native-output")
        return result

    def rbd_info(self, image):
        """`rbd info --format json` (src/tools/rbd/action/Info.cc:226-238): the image's own name and its lowercase-hex ID."""
        info = self.rbd_cli("info", "--format", "json", image["pool"] + "/" + image["name"])
        require(isinstance(info, dict) and info.get("name") == image["name"] and isinstance(info.get("id"), str)
                and ceph_native.RBD_IMAGE_ID.fullmatch(info["id"]) is not None, "native-output")
        return info

    def owned_pool(self, name):
        """An image is only ever created in a pool this operation created and still holds under its recorded ID."""
        pool = self.markers.read("pool-" + name + ".json")
        require(isinstance(pool, dict) and pool.get("name") == name, "pool-identity")
        identifier = self.markers.read("pool-id-" + name + ".json")
        require(isinstance(identifier, dict) and set(identifier) == {"id"} and type(identifier["id"]) is int and identifier["id"] >= 0, "pool-identity")
        selected = [entry for entry in self.pools() if isinstance(entry, dict) and entry.get("pool_name") == name]
        require(len(selected) == 1 and self.pool_id(selected[0]) == identifier["id"], "pool-identity")
        return selected[0]

    def rbd_attempt(self):
        return {"version": RBD_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "rbd": self.request["rbd"]}

    @staticmethod
    def expected_features(image):
        """librbd adds data-pool (bit 128, after every admitted feature) to an image with a data pool."""
        return image["features"] + (["data-pool"] if image["dataPool"] else [])

    def image_matches(self, info, image):
        """The frozen shape as observed; a consumer's later snapshot never changes it."""
        size = info.get("size")
        data_pool = "data_pool" not in info if image["dataPool"] == "" else info.get("data_pool") == image["dataPool"]
        return (type(size) is int and size == image["sizeBytes"] and info.get("format") == 2 and info.get("features") == self.expected_features(image)
                and data_pool and "parent" not in info)

    def created_image(self, info, image):
        """At creation or lost-reply adoption the image is also untouched: no snapshot yet."""
        return self.image_matches(info, image) and info.get("snapshot_count") == 0

    def rbd_marks(self, image):
        name = image["name"]
        return "rbd-" + name + ".json", "rbd-attempt." + name + ".json", "rbd-id." + name + ".json"

    def rbd(self):
        """Create one owned image with exact bytes and features, or adopt this operation's own after a lost reply; never another's."""
        require(self.markers.owned(), "ownership")
        image = self.request["rbd"]
        mark, attempt_name, id_name = self.rbd_marks(image)
        marker = self.markers.read(mark)
        require(marker is None or marker == image, "rbd-image-identity")
        self.owned_pool(image["pool"])
        if image["dataPool"]:
            self.owned_pool(image["dataPool"])
        exists = image["name"] in self.rbd_names(image["pool"])
        require(marker is not None or not exists, "foreign-rbd-image")
        self.markers.write(mark, image)
        recorded = canonical(self.markers.read(attempt_name)) == canonical(self.rbd_attempt())
        identity = self.markers.read(id_name)
        if exists:
            info = self.rbd_info(image)
            if identity is None:
                require(recorded, "foreign-rbd-image")
                require(self.created_image(info, image), "rbd-image-identity")
                self.markers.write(id_name, {"id": info["id"]})
                return {"changed": True}
            require(identity == {"id": info["id"]}, "rbd-image-identity")
            return {"changed": False}
        require(identity is None, "rbd-image-identity")
        if not recorded:
            self.markers.write(attempt_name, self.rbd_attempt())
        self.rbd_cli(*self.rbd_create_args(image), json_result=False)
        info = self.rbd_info(image)
        require(self.created_image(info, image), "rbd-image-identity")
        self.markers.write(id_name, {"id": info["id"]})
        return {"changed": True}

    @staticmethod
    def rbd_create_args(image):
        """Exact bytes (a bare number would be MiB), image format 2 and every feature explicit: '' passes the empty set, since an
        absent option takes rbd_default_features (src/tools/rbd/ArgumentTypes.cc:404-421, :472-496)."""
        args = ["create", "--size", str(image["sizeBytes"]) + "B", "--image-format", "2"]
        for feature in image["features"] or [""]:
            args += ["--image-feature", feature]
        if image["dataPool"]:
            args += ["--data-pool", image["dataPool"]]
        return args + [image["pool"] + "/" + image["name"]]

    def observe_rbd(self, evidence, ready):
        image = self.request["rbd"]
        mark, attempt_name, id_name = self.rbd_marks(image)
        marker = self.markers.read(mark)
        require(marker is None or marker == image, "rbd-image-identity")
        owned = marker is not None
        present = any(isinstance(entry, dict) and entry.get("pool_name") == image["pool"] for entry in self.pools())
        exists = present and image["name"] in self.rbd_names(image["pool"])
        require(owned or not exists, "foreign-rbd-image")
        identity = self.markers.read(id_name)
        info = self.rbd_info(image) if exists else None
        # This operation's own lost creation reply is unfinished work the apply resumes, never a refusal.
        unrecorded = exists and identity is None and canonical(self.markers.read(attempt_name)) == canonical(self.rbd_attempt())
        require(not exists or unrecorded or identity == {"id": info["id"]}, "rbd-image-identity")
        evidence["owned"] = owned
        evidence["exists"] = owned or exists
        evidence["resource"] = {"id": "" if unrecorded or not exists else info["id"], "path": ""}
        return ready and exists and not unrecorded and self.image_matches(info, image)
