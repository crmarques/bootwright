"""Fixed Ceph pool, placement policy, client, RBD image, CephFS filesystem, subvolume and settings worker phases, shipped beside the core worker as source.

A cluster request and a host scope never load this module: only a pool, policy, client,
RBD image, filesystem, subvolume or settings action reaches its validation and its worker. Like the core worker it imports
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
    Custody, NativeFailure, Refusal, canonical, named, read_regular, require, unique_object,
)

ACTIONS = ("pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings")
PHASES = frozenset(("pool", "client", "client_read", "policy", "rbd", "filesystem", "filesystem_ranks", "filesystem_groups", "subvolume", "settings"))
READ_ONLY_PHASES = frozenset(("client_read", "filesystem_ranks"))

POOL_ATTEMPT = "ceph-pool-attempt-v2"
POLICY_ATTEMPT = "ceph-policy-attempt-v1"
RBD_ATTEMPT = "ceph-rbd-image-attempt-v1"
FILESYSTEM_ATTEMPT = "ceph-filesystem-attempt-v1"
GROUP_ATTEMPT = "ceph-subvolume-group-attempt-v1"
SUBVOLUME_ATTEMPT = "ceph-subvolume-attempt-v1"
# The manager option cephadm registers per auxiliary image, read in section mgr (src/pybind/mgr/cephadm/module.py:499-500).
IMAGE_KEY = "mgr/cephadm/container_image_"
# The manager modules a declared service family derives; internal/storage/services.go holds the same set.
DERIVED_MODULES = (
    "nfs",
)
SUBVOLUME_MEMBERS = frozenset(("filesystem", "gid", "group", "mode", "name", "pool", "sizeBytes", "uid"))
# A subvolume's user metadata key: printable ASCII, stored lower case (doc/cephfs/fs-volumes.rst:572-575).
OWNER_KEY = "bootwright-owner"
FILESYSTEM_MEMBERS = frozenset(("activeCount", "dataPools", "defaultDataPool", "groups", "mdsService", "metadataPool", "name", "standbyCountWanted",
                                "standbyReplay"))
GROUP_MEMBERS = frozenset(("gid", "mode", "name", "pool", "sizeBytes", "uid"))
# The volumes module's own directories under /volumes (src/pybind/mgr/volumes/fs/operations/group.py, fs/volume.py).
RESERVED_GROUPS = ("_nogroup", "_index", "_deleting", "_legacy")
FILE_MODE = re.compile(r"[0-7]{3}")
# The options `fs new` sets on its metadata pool in the proposal that records the pool's application key
# (src/mon/FSCommands.cc:315-331; option names src/mon/OSDMonitor.cc:6215-6232).
FS_NEW_METADATA_OPTIONS = (("pg_num_min", 16), ("recovery_priority", 5), ("pg_autoscale_bias", 4.0))
RBD_MEMBERS = frozenset(("dataPool", "features", "name", "pool", "sizeBytes"))
# librbd's feature-bit order (src/tools/rbd/ArgumentTypes.cc:20-33), the order `rbd info` lists them in.
RBD_FEATURES = ("layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling")
# librbd refuses an object map over 256000000 objects (src/librbd/ObjectMap.cc:73-76); rbd_default_order 22 makes them 4 MiB.
RBD_OBJECT_MAP_BYTES = 256000000 << 22
# The plugins whose stored profile profile_matches proves: isa, and jerasure with its two frozen defaults.
ERASURE_PLUGINS = frozenset(("isa", "jerasure"))
APPLICATIONS = {"rbd": "rbd", "cephfs-metadata": "cephfs", "cephfs-data": "cephfs", "rgw": "rgw"}
POOL_MEMBERS = frozenset(("application", "autoscale", "compression", "crushRule", "filesystem", "minSize", "name", "overwrites", "pgNum", "quota", "role",
                          "size", "type"))
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
    """Admit one pool, placement policy, client, RBD image or filesystem resource beside its validated cluster scope."""
    osd_hosts = sum(bool(host["devices"]) for host in hosts)
    require(request["action"] == "rbd" or "rbd" not in request)
    require(request["action"] == "filesystem" or "filesystem" not in request)
    require(request["action"] == "subvolume" or "subvolume" not in request)
    require(request["action"] == "settings" or "settings" not in request)
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
    elif request["action"] == "filesystem":
        require("pool" not in request and "client" not in request and "policy" not in request)
        validate_filesystem(request.get("filesystem"), request["services"])
    elif request["action"] == "subvolume":
        require("pool" not in request and "client" not in request and "policy" not in request)
        validate_subvolume(request.get("subvolume"))
    elif request["action"] == "settings":
        require(not {"pool", "client", "policy", "rbd", "filesystem", "subvolume"} & set(request))
        validate_settings(request)


def validate_settings(request):
    """The derived manager modules, in name order, beside the frozen auxiliary images; a settings block always has something to write."""
    settings = request.get("settings")
    require(isinstance(settings, dict) and set(settings) == {"modules"})
    modules = settings["modules"]
    require(isinstance(modules, list) and all(isinstance(module, str) and module in DERIVED_MODULES for module in modules))
    require(modules == sorted(set(modules)))
    require(bool(modules) or bool(request.get("auxiliary")))


def validate_subvolume(subvolume):
    """The frozen subvolume the Go admission proved: its name fits its block identity, its group is "" (the native default) or a declared one."""
    require(isinstance(subvolume, dict) and set(subvolume) == SUBVOLUME_MEMBERS)
    name, filesystem, group = subvolume["name"], subvolume["filesystem"], subvolume["group"]
    require(named(name) and "." not in name and len(name) <= 48)
    require(named(filesystem) and "." not in filesystem and len(filesystem) <= 47)
    require(group == "" or named(group) and group not in RESERVED_GROUPS)
    require(named(subvolume["pool"]) and isinstance(subvolume["mode"], str) and FILE_MODE.fullmatch(subvolume["mode"]) is not None)
    require(bounded(subvolume["uid"], 2147483647) and bounded(subvolume["gid"], 2147483647) and bounded(subvolume["sizeBytes"], INT64))


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


def mds_service(filesystem, services):
    """The one projected mds service whose service_id is the filesystem's name."""
    matches = [service for service in services if service["service_type"] == "mds" and service.get("service_id") == filesystem["name"]]
    require(len(matches) == 1)
    return matches[0]


def mds_needed(filesystem):
    """Active ranks plus the larger of the wanted standbys and the standby-replay followers, one per active rank."""
    active = filesystem["activeCount"]
    return active + max(filesystem["standbyCountWanted"], active if filesystem["standbyReplay"] else 0)


def validate_filesystem(filesystem, services):
    """The frozen filesystem the Go admission proved: its own MDS service covers it, its default data pool leads its data pools."""
    require(isinstance(filesystem, dict) and set(filesystem) == FILESYSTEM_MEMBERS)
    name = filesystem["name"]
    require(named(name) and "." not in name and not name[0].isdigit() and len(name) <= 47)
    require(filesystem["mdsService"] == "mds." + name)
    service = mds_service(filesystem, services)
    pools = filesystem["dataPools"]
    require(isinstance(pools, list) and bool(pools) and all(named(pool) for pool in pools) and len(set(pools)) == len(pools))
    require(named(filesystem["metadataPool"]) and filesystem["metadataPool"] not in pools and filesystem["defaultDataPool"] == pools[0])
    require(type(filesystem["activeCount"]) is int and 1 <= filesystem["activeCount"] <= 256 and type(filesystem["standbyReplay"]) is bool)
    require(bounded(filesystem["standbyCountWanted"], 2147483647))
    require(service["placement"].get("count", len(service["placement"]["hosts"])) >= mds_needed(filesystem))
    groups = filesystem["groups"]
    require(isinstance(groups, list) and all(isinstance(group, dict) and set(group) == GROUP_MEMBERS for group in groups))
    names = [group["name"] for group in groups]
    require(names == sorted(set(names)) and all(named(group) and group not in RESERVED_GROUPS for group in names))
    for group in groups:
        require(group["pool"] in pools and isinstance(group["mode"], str) and FILE_MODE.fullmatch(group["mode"]) is not None)
        require(bounded(group["uid"], 2147483647) and bounded(group["gid"], 2147483647) and bounded(group["sizeBytes"], INT64))


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
    require(pool["filesystem"] == "" or pool["role"] in ("cephfs-metadata", "cephfs-data") and named(pool["filesystem"]))
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
    require(all(type(value) is int and value > 0 for value in row) and osd_hosts >= row[0] + row[1])
    require(isinstance(erasure["plugin"], str) and erasure["plugin"] in ERASURE_PLUGINS)
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
            require(completed is None or "rbd" in current.get("application_metadata", {}), "pool-drift")
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
        require(complete is None or current[field] == wanted, "pool-drift")
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
        require(hosts >= erasure["dataChunks"] + erasure["codingChunks"], "crush-capacity")
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
        sent again; a completed setting whose live value differs refuses pool-drift before any write. Returns whether the completion was absent."""
        current = self.owned_pool(pool["name"])
        prefix = "pool-setting-" + pool["name"] + "-" + field
        intent = {"owner": self.request["owner"], "fsid": self.request["fsid"], "id": self.pool_id(current), "field": field, "value": wanted}
        attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
        require(attempt is None or attempt == intent, "pool-identity")
        require(complete is None or complete == intent and attempt == intent, "pool-identity")
        if complete is not None:
            require(observed(current), "pool-drift")
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
        elif action == "filesystem":
            ready = self.observe_filesystem(evidence, ready)
        elif action == "subvolume":
            ready = self.observe_subvolume(evidence, ready)
        elif action == "settings":
            ready = self.observe_settings(ready)
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

    def image_settings(self):
        """Each frozen auxiliary image's setting: held only as the one mgr-section entry of the frozen value; any other entry is foreign."""
        wanted = {IMAGE_KEY + entry["name"]: entry["image"] for entry in self.request.get("auxiliary", [])}
        observed = self.ceph("config", "dump")
        require(isinstance(observed, list) and all(isinstance(entry, dict) for entry in observed), "native-output")
        held, foreign = set(), set()
        for entry in observed:
            name = entry.get("name")
            if name not in wanted:
                continue
            located = any(field in entry for field in ("location_type", "location_value", "device_class"))
            if entry.get("section") == "mgr" and entry.get("mask") == "" and not located and name not in held and entry.get("value") == wanted[name]:
                held.add(name)
            else:
                foreign.add(name)
        if foreign:
            raise NativeFailure("foreign-image", self.native_coordinate(), ", ".join(sorted(foreign)))
        return wanted, held

    def manager_modules(self):
        """The enabled and always-on manager modules `mgr module ls` lists."""
        modules = self.ceph("mgr", "module", "ls")
        require(isinstance(modules, dict), "native-output")
        present = set()
        for field in ("enabled_modules", "always_on_modules"):
            value = modules.get(field)
            require(isinstance(value, list) and len(value) <= 128 and all(named(name) for name in value), "native-output")
            require(len(set(value)) == len(value), "native-output")
            present |= set(value)
        return present

    def settings(self):
        """Write each absent frozen image setting and enable each missing derived module, after every refusal is proved and before none is."""
        require(self.markers.owned(), "ownership")
        wanted, held = self.image_settings()
        present = self.manager_modules()
        changed = False
        for entry in self.request.get("auxiliary", []):
            key = IMAGE_KEY + entry["name"]
            if key not in held:
                self.ceph("config", "set", "mgr", key, wanted[key], json_result=False)
                changed = True
        for module in self.request["settings"]["modules"]:
            if module not in present:
                self.ceph("mgr", "module", "enable", module, json_result=False)
                changed = True
        wanted, held = self.image_settings()
        require(held == set(wanted) and set(self.request["settings"]["modules"]) <= self.manager_modules(), "native-output")
        return {"changed": changed}

    def observe_settings(self, ready):
        """Read-only: ready once every frozen image setting is held and every derived module is enabled."""
        wanted, held = self.image_settings()
        return ready and held == set(wanted) and set(self.request["settings"]["modules"]) <= self.manager_modules()

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
        metadata = entry.get("application_metadata")
        return ready and self.options_match(entry.get("options", {}), pool, metadata) and self.application_matches(metadata, pool)

    @staticmethod
    def options_match(options, pool, metadata=None):
        """Each owned option is absent when its frozen value is unset, else equal; other owners' options are not read.

        A CephFS metadata pool also carries the three options `fs new` sets, exactly their values, once its application key names the
        pool's own filesystem; `fs new`'s pg_num_min then replaces a frozen one. Before that none of them, beyond a frozen pg_num_min."""
        if not isinstance(options, dict):
            return False
        settled = set()
        if pool["role"] == "cephfs-metadata":
            applied = pool["filesystem"] != "" and metadata == {"cephfs": {"metadata": pool["filesystem"]}}
            for option, value in FS_NEW_METADATA_OPTIONS:
                observed = options.get(option)
                exact = type(observed) is int and observed == value or type(value) is float and type(observed) in (int, float) and float(observed) == value
                if option in options and applied and exact:
                    settled.add(option)
                elif option in options and option != "pg_num_min":
                    return False
        return all(option in settled or ResourceWorker.option_matches(options, option, pool[block][member]) for option, block, member in OWNED_OPTIONS)

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
        """Exactly the derived application; a CephFS pool may carry only the one key `fs new` records for its role, naming its own filesystem."""
        if not isinstance(metadata, dict) or set(metadata) != {pool["application"]}:
            return False
        if pool["application"] != "cephfs":
            return True
        value = metadata["cephfs"]
        key = "data" if pool["role"] == "cephfs-data" else "metadata"
        return value == {} or pool["filesystem"] != "" and value == {key: pool["filesystem"]}

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

    def owned_pool(self, name, entries=None):
        """An image or filesystem is only ever created in a pool this operation created and still holds under its recorded ID."""
        pool = self.markers.read("pool-" + name + ".json")
        require(isinstance(pool, dict) and pool.get("name") == name, "pool-identity")
        identifier = self.markers.read("pool-id-" + name + ".json")
        require(isinstance(identifier, dict) and set(identifier) == {"id"} and type(identifier["id"]) is int and identifier["id"] >= 0, "pool-identity")
        selected = [entry for entry in (self.pools() if entries is None else entries) if isinstance(entry, dict) and entry.get("pool_name") == name]
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

    def filesystem_marks(self):
        name = self.request["filesystem"]["name"]
        return "filesystem-" + name + ".json", "filesystem-attempt." + name + ".json", "filesystem-id." + name + ".json"

    def filesystem_attempt(self):
        return {"version": FILESYSTEM_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "filesystem": self.request["filesystem"]}

    def fs_dump(self):
        """`ceph fs dump` (src/mds/FSMap.cc:131-176, src/mds/MDSMap.cc:190-273): the members read here, each of its native type."""
        dump = self.ceph("fs", "dump")
        require(isinstance(dump, dict) and isinstance(dump.get("feature_flags"), dict) and type(dump["feature_flags"].get("enable_multiple")) is bool,
                "native-output")
        standbys, filesystems = dump.get("standbys"), dump.get("filesystems")
        require(isinstance(standbys, list) and all(isinstance(item, dict) and isinstance(item.get("name"), str) for item in standbys), "native-output")
        require(isinstance(filesystems, list) and len(filesystems) <= 65536, "native-output")
        for entry in filesystems:
            mdsmap = entry.get("mdsmap") if isinstance(entry, dict) else None
            require(type(entry.get("id") if isinstance(entry, dict) else None) is int and entry["id"] >= 0 and isinstance(mdsmap, dict), "native-output")
            require(isinstance(mdsmap.get("fs_name"), str) and type(mdsmap.get("metadata_pool")) is int and isinstance(mdsmap.get("data_pools"), list)
                    and all(type(pool) is int for pool in mdsmap["data_pools"]) and type(mdsmap.get("max_mds")) is int
                    and type(mdsmap.get("standby_count_wanted")) is int and isinstance(mdsmap.get("flags_state"), dict)
                    and type(mdsmap["flags_state"].get("allow_standby_replay")) is bool and isinstance(mdsmap.get("info"), dict), "native-output")
            require(all(isinstance(key, str) and key.startswith("gid_") and isinstance(info, dict) and isinstance(info.get("name"), str)
                        and type(info.get("rank")) is int and isinstance(info.get("state"), str) for key, info in mdsmap["info"].items()), "native-output")
        names = [entry["mdsmap"]["fs_name"] for entry in filesystems]
        require(len(set(names)) == len(names), "native-output")
        return dump

    def fs_entry(self, dump):
        return next((entry for entry in dump["filesystems"] if entry["mdsmap"]["fs_name"] == self.request["filesystem"]["name"]), None)

    def bound_mds(self, name):
        """cephadm's MDS daemon ID <service_id>.<host short name>.<six lowercase letters> on one of the bound service's hosts."""
        filesystem = self.request["filesystem"]
        hosts = mds_service(filesystem, self.request["services"])["placement"]["hosts"]
        return any(re.fullmatch(re.escape(filesystem["name"]) + r"\." + re.escape(host.split(".", 1)[0]) + r"\.[a-z]{6}", name) is not None
                   for host in hosts)

    def filesystem_pools(self):
        """Every pool of the filesystem this operation created and holds by its recorded ID, carrying no other filesystem's key."""
        filesystem = self.request["filesystem"]
        entries, ids = self.pools(), {}
        for name in [filesystem["metadataPool"]] + filesystem["dataPools"]:
            entry = self.owned_pool(name, entries)
            application = entry.get("application_metadata")
            require(isinstance(application, dict) and set(application) == {"cephfs"} and isinstance(application["cephfs"], dict), "pool-identity")
            require(all(value == filesystem["name"] for value in application["cephfs"].values()), "foreign-filesystem")
            key = "metadata" if name == filesystem["metadataPool"] else "data"
            require(application["cephfs"] in ({}, {key: filesystem["name"]}), "pool-identity")
            ids[name] = self.pool_id(entry)
        return ids

    def created_filesystem(self, entry, ids):
        """The pools `fs new` bound: the frozen metadata pool and the frozen default data pool first, by their recorded IDs."""
        filesystem, mdsmap = self.request["filesystem"], entry["mdsmap"]
        return (mdsmap["metadata_pool"] == ids[filesystem["metadataPool"]] and bool(mdsmap["data_pools"])
                and mdsmap["data_pools"][0] == ids[filesystem["defaultDataPool"]])

    def filesystem_proved(self, write=False):
        """The marker, the pools, then the filesystem of the name: never another's, and once recorded always the recorded ID."""
        require(self.request["action"] == "filesystem" and self.markers.owned(), "ownership")
        filesystem = self.request["filesystem"]
        mark, attempt_name, id_name = self.filesystem_marks()
        marker = self.markers.read(mark)
        require(marker is None or marker == filesystem, "filesystem-identity")
        ids = self.filesystem_pools()
        dump = self.fs_dump()
        entry = self.fs_entry(dump)
        require(marker is not None or entry is None, "foreign-filesystem")
        if not write:
            require(marker is not None and entry is not None and self.markers.read(id_name) == {"id": entry["id"]}, "filesystem-identity")
            return dump, entry, ids
        self.markers.write(mark, filesystem)
        identity = self.markers.read(id_name)
        if entry is not None and identity is None:
            # A lost creation reply resumes only from this operation's own attempt record and the frozen pools.
            require(canonical(self.markers.read(attempt_name)) == canonical(self.filesystem_attempt()), "foreign-filesystem")
            require(self.created_filesystem(entry, ids), "filesystem-identity")
            self.markers.write(id_name, {"id": entry["id"]})
        elif entry is not None:
            require(identity == {"id": entry["id"]}, "filesystem-identity")
        else:
            require(identity is None, "filesystem-identity")
        return dump, entry, ids

    def filesystem(self):
        """Create one owned filesystem with its MDS settings in the one `fs new`, then attach its extra data pools; never forced."""
        filesystem = self.request["filesystem"]
        recorded = self.markers.read(self.filesystem_marks()[2]) is not None
        dump, entry, ids = self.filesystem_proved(write=True)
        changed = entry is not None and not recorded
        if entry is None:
            require(not dump["filesystems"] or dump["feature_flags"]["enable_multiple"] is True, "filesystem-multiple-disabled")
            require(sum(self.bound_mds(item["name"]) for item in dump["standbys"]) >= mds_needed(filesystem), "mds-coverage")
            self.markers.write(self.filesystem_marks()[1], self.filesystem_attempt())
            self.ceph(*self.fs_new_args(), json_result=False)
            entry = self.fs_entry(self.fs_dump())
            require(entry is not None and self.created_filesystem(entry, ids), "filesystem-identity")
            self.markers.write(self.filesystem_marks()[2], {"id": entry["id"]})
            changed = True
        for pool in filesystem["dataPools"][1:]:
            if ids[pool] not in entry["mdsmap"]["data_pools"]:
                self.ceph("fs", "add_data_pool", filesystem["name"], pool, json_result=False)
                changed = True
        return {"changed": changed}

    def fs_new_args(self):
        """`fs new` with its trailing `set` pairs applies them in the creating proposal (src/mon/MonCommands.h:356-367,
        src/mon/FSCommands.cc:251-307); `fs set max_mds` would refuse under any filesystem health warning."""
        filesystem = self.request["filesystem"]
        return ("fs", "new", filesystem["name"], filesystem["metadataPool"], filesystem["defaultDataPool"], "set", "max_mds", str(filesystem["activeCount"]),
                "allow_standby_replay", "true" if filesystem["standbyReplay"] else "false", "standby_count_wanted", str(filesystem["standbyCountWanted"]))

    def ranks_active(self, entry):
        """Each rank below max_mds held by an up:active daemon of the bound service."""
        infos = entry["mdsmap"]["info"].values()
        return all(any(info["rank"] == rank and info["state"] == "up:active" and self.bound_mds(info["name"]) for info in infos)
                   for rank in range(self.request["filesystem"]["activeCount"]))

    def filesystem_ranks(self):
        unused, entry, unused_ids = self.filesystem_proved()
        return {"active": self.ranks_active(entry)}

    def group_marks(self, group):
        name = self.request["filesystem"]["name"]
        return "group." + name + "." + group["name"] + ".json", "group-attempt." + name + "." + group["name"] + ".json"

    def group_attempt(self, group):
        return {"version": GROUP_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "filesystem": self.request["filesystem"]["name"],
                "group": group}

    def group_names(self, filesystem):
        """`fs subvolumegroup ls` (src/pybind/mgr/volumes/fs/volume.py:50-60): one {"name"} per group, served by the active manager."""
        result = self.manager_retry(self.ceph, "fs", "subvolumegroup", "ls", filesystem)
        require(isinstance(result, list) and len(result) <= 65536 and all(isinstance(item, dict) and isinstance(item.get("name"), str) for item in result),
                "native-output")
        return {item["name"] for item in result}

    def group_matches(self, group):
        """`fs subvolumegroup info` (src/pybind/mgr/volumes/fs/operations/group.py:113-152): mode is the full st_mode, no quota is "infinite"."""
        info = self.manager_retry(self.ceph, "fs", "subvolumegroup", "info", self.request["filesystem"]["name"], group["name"])
        require(isinstance(info, dict) and all(type(info.get(key)) is int for key in ("uid", "gid", "mode")) and isinstance(info.get("data_pool"), str)
                and (type(info.get("bytes_quota")) is int or info.get("bytes_quota") == "infinite"), "native-output")
        return (info["uid"] == group["uid"] and info["gid"] == group["gid"] and info["mode"] & 0o7777 == int(group["mode"], 8)
                and info["data_pool"] == group["pool"] and info["bytes_quota"] == (group["sizeBytes"] or "infinite"))

    def group_create(self, group):
        """Every attribute explicit (doc/cephfs/fs-volumes.rst; src/pybind/mgr/volumes/module.py:86-100); a repeat on an existing group
        rewrites each of them (src/pybind/mgr/volumes/fs/volume.py:1093-1111)."""
        args = ["fs", "subvolumegroup", "create", self.request["filesystem"]["name"], group["name"], "--pool_layout", group["pool"], "--uid",
                str(group["uid"]), "--gid", str(group["gid"]), "--mode", group["mode"]]
        if group["sizeBytes"]:
            args += ["--size", str(group["sizeBytes"])]
        self.manager_retry(self.ceph, *args, json_result=False)

    def filesystem_groups(self):
        """Create each declared group only after proving it absent, then converge an owned group's attributes with one more create."""
        self.filesystem_proved()
        names, changed = self.group_names(self.request["filesystem"]["name"]), False
        for group in self.request["filesystem"]["groups"]:
            mark, attempt_name = self.group_marks(group)
            marker = self.markers.read(mark)
            require(marker is None or marker == group, "filesystem-identity")
            present = group["name"] in names
            require(marker is not None or not present, "foreign-subvolume-group")
            self.markers.write(mark, group)
            if present:
                require(canonical(self.markers.read(attempt_name)) == canonical(self.group_attempt(group)), "foreign-subvolume-group")
            else:
                self.markers.write(attempt_name, self.group_attempt(group))
                self.group_create(group)
                changed = True
            if not self.group_matches(group):
                self.group_create(group)
                changed = True
        return {"changed": changed}

    def observe_filesystem(self, evidence, ready):
        filesystem = self.request["filesystem"]
        mark, attempt_name, id_name = self.filesystem_marks()
        marker = self.markers.read(mark)
        require(marker is None or marker == filesystem, "filesystem-identity")
        dump = self.fs_dump()
        entry = self.fs_entry(dump)
        require(marker is not None or entry is None, "foreign-filesystem")
        identity = self.markers.read(id_name)
        # This operation's own lost creation reply is unfinished work the apply resumes, never a refusal.
        unrecorded = entry is not None and identity is None and canonical(self.markers.read(attempt_name)) == canonical(self.filesystem_attempt())
        require(unrecorded or (identity is None if entry is None else identity == {"id": entry["id"]}), "filesystem-identity")
        evidence["owned"] = marker is not None
        evidence["exists"] = marker is not None or entry is not None
        evidence["resource"] = {"id": "" if unrecorded or entry is None else str(entry["id"]), "path": ""}
        return ready and entry is not None and not unrecorded and self.filesystem_ready(dump, entry)

    def filesystem_ready(self, dump, entry):
        """The recorded pools in frozen order, the frozen MDS settings, every rank active with its follower, enough standbys, then the groups."""
        filesystem, mdsmap = self.request["filesystem"], entry["mdsmap"]
        recorded = [self.markers.read("pool-id-" + name + ".json") for name in [filesystem["metadataPool"]] + filesystem["dataPools"]]
        if not all(isinstance(item, dict) and set(item) == {"id"} for item in recorded):
            return False
        ids = [item["id"] for item in recorded]
        active = filesystem["activeCount"]
        if (mdsmap["metadata_pool"] != ids[0] or mdsmap["data_pools"] != ids[1:] or mdsmap["max_mds"] != active
                or mdsmap["flags_state"]["allow_standby_replay"] is not filesystem["standbyReplay"]
                or mdsmap["standby_count_wanted"] != filesystem["standbyCountWanted"] or not self.ranks_active(entry)):
            return False
        followers = [info["rank"] for info in mdsmap["info"].values() if info["state"] == "up:standby-replay" and self.bound_mds(info["name"])]
        if filesystem["standbyReplay"] and not all(rank in followers for rank in range(active)):
            return False
        if len(followers) + sum(self.bound_mds(item["name"]) for item in dump["standbys"]) < filesystem["standbyCountWanted"]:
            return False
        return self.groups_ready()

    def groups_ready(self):
        """Every declared group present with its marker and its frozen attributes; a group the filesystem holds beyond them is not read."""
        groups = self.request["filesystem"]["groups"]
        if not groups:
            return True
        names = self.group_names(self.request["filesystem"]["name"])
        for group in groups:
            require(self.markers.read(self.group_marks(group)[0]) is not None or group["name"] not in names, "foreign-subvolume-group")
        return all(group["name"] in names and self.group_matches(group) for group in groups)

    def subvolume_marks(self):
        name = self.request["subvolume"]["name"]
        return "subvolume-" + name + ".json", "subvolume-attempt." + name + ".json", "subvolume-path." + name + ".json"

    def subvolume_attempt(self):
        return {"version": SUBVOLUME_ATTEMPT, "owner": self.request["owner"], "fsid": self.request["fsid"], "subvolume": self.request["subvolume"]}

    def subvolume_cli(self, verb, positional=(), options=(), json_result=True):
        """One `fs subvolume` command, in the frozen group only when one is named: an explicit `--group_name _nogroup` refuses EPERM
        (src/pybind/mgr/volumes/fs/operations/group.py:21-32), and omitting it selects the default group."""
        subvolume = self.request["subvolume"]
        group = ("--group_name", subvolume["group"]) if subvolume["group"] else ()
        return self.manager_retry(self.ceph, "fs", "subvolume", *(verb + (subvolume["filesystem"],) + positional + group + options), json_result=json_result)

    def subvolume_names(self):
        """`fs subvolume ls` (src/pybind/mgr/volumes/fs/volume.py:637-649): one {"name"} per subvolume; [] for a default group never created
        (src/pybind/mgr/volumes/fs/operations/group.py:61-68)."""
        result = self.subvolume_cli(("ls",))
        require(isinstance(result, list) and len(result) <= 1048576 and all(isinstance(item, dict) and isinstance(item.get("name"), str) for item in result),
                "native-output")
        return {item["name"] for item in result}

    def subvolume_info(self):
        """`fs subvolume info` (src/pybind/mgr/volumes/fs/operations/versions/subvolume_base.py:473-547): mode is the full st_mode, no quota
        is "infinite"."""
        info = self.subvolume_cli(("info",), (self.request["subvolume"]["name"],))
        require(isinstance(info, dict) and all(isinstance(info.get(key), str) for key in ("path", "type", "state", "data_pool", "pool_namespace"))
                and all(type(info.get(key)) is int for key in ("uid", "gid", "mode"))
                and (type(info.get("bytes_quota")) is int or info.get("bytes_quota") == "infinite"), "native-output")
        return info

    def subvolume_owner(self):
        """`fs subvolume metadata ls` answers every user key; `metadata get` of a missing key fails like any other failure."""
        result = self.subvolume_cli(("metadata", "ls"), (self.request["subvolume"]["name"],))
        require(isinstance(result, dict) and len(result) <= 4096 and all(isinstance(k, str) and isinstance(v, str) for k, v in result.items()),
                "native-output")
        return result.get(OWNER_KEY)

    def subvolume_create(self):
        """Every attribute explicit (src/pybind/mgr/volumes/module.py:139-157); never namespace isolation, earmark, normalization or case
        sensitivity. A create on an existing subvolume rewrites its attributes (src/pybind/mgr/volumes/fs/volume.py:249-290)."""
        subvolume = self.request["subvolume"]
        args = ("--pool_layout", subvolume["pool"], "--uid", str(subvolume["uid"]), "--gid", str(subvolume["gid"]), "--mode", subvolume["mode"])
        if subvolume["sizeBytes"]:
            args += ("--size", str(subvolume["sizeBytes"]))
        self.subvolume_cli(("create",), (subvolume["name"],), args, json_result=False)

    def subvolume_matches(self, info, mode=True):
        """The frozen shape as observed; the earmark, normalization and case sensitivity a consumer may set later are never read."""
        subvolume = self.request["subvolume"]
        prefix = "/volumes/" + (subvolume["group"] or "_nogroup") + "/" + subvolume["name"] + "/"
        return (info["type"] == "subvolume" and info["state"] == "complete" and info["path"].startswith(prefix)
                and ceph_native.SUBVOLUME_UUID.fullmatch(info["path"][len(prefix):]) is not None and info["data_pool"] == subvolume["pool"]
                and info["pool_namespace"] == "" and info["bytes_quota"] == (subvolume["sizeBytes"] or "infinite") and info["uid"] == subvolume["uid"]
                and info["gid"] == subvolume["gid"] and (not mode or info["mode"] & 0o7777 == int(subvolume["mode"], 8)))

    def subvolume_parent(self):
        """The filesystem, the layout pool and the group this operation created, proved before any `fs subvolume` command."""
        subvolume = self.request["subvolume"]
        name = subvolume["filesystem"]
        marker = self.markers.read("filesystem-" + name + ".json")
        require(isinstance(marker, dict) and marker.get("name") == name, "filesystem-identity")
        entries = self.pools()
        entry = next((item for item in self.fs_dump()["filesystems"] if item["mdsmap"]["fs_name"] == name), None)
        require(entry is not None and self.markers.read("filesystem-id." + name + ".json") == {"id": entry["id"]}, "filesystem-identity")
        pool = self.owned_pool(subvolume["pool"], entries)
        require(self.pool_id(pool) in entry["mdsmap"]["data_pools"], "filesystem-identity")
        if subvolume["group"]:
            require(self.markers.read("group." + name + "." + subvolume["group"] + ".json") is not None, "foreign-subvolume-group")
            require(subvolume["group"] in self.group_names(name), "filesystem-identity")

    def subvolume_created(self):
        """Create, then once more only when the mode alone differs: a fresh create sets it through mkdirs under the manager's umask
        (src/pybind/mgr/volumes/fs/operations/versions/subvolume_v2.py:157-183), a repeat with lchmod (subvolume_base.py:296-300)."""
        info = self.subvolume_info()
        require(self.subvolume_matches(info, mode=False), "subvolume-identity")
        if not self.subvolume_matches(info):
            self.subvolume_create()
            info = self.subvolume_info()
        require(self.subvolume_matches(info), "subvolume-identity")
        self.markers.write(self.subvolume_marks()[2], {"path": info["path"]})

    def subvolume(self):
        """Create one owned subvolume only after proving it absent, or adopt this operation's own after a lost reply; never another's."""
        require(self.markers.owned(), "ownership")
        subvolume = self.request["subvolume"]
        mark, attempt_name, path_name = self.subvolume_marks()
        marker = self.markers.read(mark)
        require(marker is None or marker == subvolume, "subvolume-identity")
        self.subvolume_parent()
        present = subvolume["name"] in self.subvolume_names()
        require(marker is not None or not present, "foreign-subvolume")
        self.markers.write(mark, subvolume)
        recorded = canonical(self.markers.read(attempt_name)) == canonical(self.subvolume_attempt())
        path, changed = self.markers.read(path_name), False
        if present and path is None:
            require(recorded, "foreign-subvolume")
            self.subvolume_created()
            changed = True
        elif present:
            require(path == {"path": self.subvolume_info()["path"]}, "subvolume-identity")
        else:
            require(path is None, "subvolume-identity")
            if not recorded:
                self.markers.write(attempt_name, self.subvolume_attempt())
            self.subvolume_create()
            self.subvolume_created()
            changed = True
        owner = self.subvolume_owner()
        require(owner in (None, self.request["owner"]), "subvolume-identity")
        if owner is None:
            self.subvolume_cli(("metadata", "set"), (subvolume["name"], OWNER_KEY, self.request["owner"]), json_result=False)
            require(self.subvolume_owner() == self.request["owner"], "subvolume-identity")
            changed = True
        return {"changed": changed}

    def observe_subvolume(self, evidence, ready):
        subvolume = self.request["subvolume"]
        mark, attempt_name, path_name = self.subvolume_marks()
        marker = self.markers.read(mark)
        require(marker is None or marker == subvolume, "subvolume-identity")
        owned = marker is not None
        name = subvolume["filesystem"]
        entry = next((item for item in self.fs_dump()["filesystems"] if item["mdsmap"]["fs_name"] == name), None)
        # Listing a missing filesystem or group fails, so absence of either is absence of the subvolume.
        present = (entry is not None and (not subvolume["group"] or subvolume["group"] in self.group_names(name))
                   and subvolume["name"] in self.subvolume_names())
        require(owned or not present, "foreign-subvolume")
        info = self.subvolume_info() if present else None
        path = self.markers.read(path_name)
        # This operation's own lost creation reply is unfinished work the apply resumes, never a refusal.
        unrecorded = present and path is None and canonical(self.markers.read(attempt_name)) == canonical(self.subvolume_attempt())
        require(not present or unrecorded or path == {"path": info["path"]}, "subvolume-identity")
        evidence["owned"] = owned
        evidence["exists"] = owned or present
        recorded = present and not unrecorded
        evidence["resource"] = {"id": info["path"].rsplit("/", 1)[1], "path": info["path"]} if recorded else {"id": "", "path": ""}
        return (ready and recorded and self.markers.read("filesystem-id." + name + ".json") == {"id": entry["id"]} and self.subvolume_matches(info)
                and self.subvolume_owner() == self.request["owner"])
