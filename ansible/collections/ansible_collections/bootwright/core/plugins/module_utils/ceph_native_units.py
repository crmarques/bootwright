"""Fixed generic Ceph service-unit worker phase, shipped beside the core worker as source.

A service-unit request carries one unit: the native service documents of one unit type, applied in their
document order as one `orch apply -i -` stream (src/pybind/mgr/orchestrator/module.py:1826-1846). The generic
flow owns ownership, the unit's normalized inputs record, per-service attempt and completion records, the one
batch, its readback and observation; each type's hooks own its checks, pre-apply claims, post-apply settling,
expected daemons and readiness. Like the core worker it imports only the target Python standard library and the
core worker modules.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import collections
import re

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_nfs, ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import canonical, named, require

UnitType = collections.namedtuple("UnitType", ("validate", "check", "claim", "settle", "expected", "readiness", "daemon_types"))
UNIT_TYPES = {
    "nfs": UnitType(ceph_native_nfs.validate, ceph_native_nfs.check, ceph_native_nfs.claim, ceph_native_nfs.settle, ceph_native_nfs.expected,
                    ceph_native_nfs.readiness, ceph_native_nfs.DAEMON_TYPES),
}
PHASES = frozenset(("unit",))
DOCUMENT_MEMBERS = frozenset(("networks", "placement", "service_id", "service_type", "spec"))
INPUTS_VERSION = "ceph-service-inputs-v1"
MAX_DAEMONS = 256
PENDING = re.compile(r"(?:" + "|".join(sorted({name for unit in UNIT_TYPES.values() for name in unit.daemon_types} | {"vip"}))
                     + r")@(?:nfs|ingress)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")


def service_name(document):
    return document["service_type"] + "." + document["service_id"]


def validate(request, hosts):
    """One unit of a known type: 1 to 8 placed documents, each listed in `delegated` with exactly its hosts."""
    unit = request.get("service")
    require(isinstance(unit, dict) and set(unit) == {"documents", "id", "type"})
    require(isinstance(unit["type"], str) and unit["type"] in UNIT_TYPES)
    require(unit["id"] == "" or isinstance(unit["id"], str) and ceph_native.NAME.fullmatch(unit["id"]) is not None)
    documents = unit["documents"]
    require(isinstance(documents, list) and 1 <= len(documents) <= 8)
    names = [host["name"] for host in hosts]
    delegated = {entry["name"]: entry["hosts"] for entry in request.get("delegated", [])}
    seen = set()
    for document in documents:
        require(isinstance(document, dict) and {"placement", "service_id", "service_type"} <= set(document) <= DOCUMENT_MEMBERS)
        require(named(document["service_type"]) and named(document["service_id"]))
        placement = document["placement"]
        require(isinstance(placement, dict) and "hosts" in placement and set(placement) <= {"hosts", "count"})
        listed = placement["hosts"]
        require(isinstance(listed, list) and bool(listed) and all(isinstance(host, str) and host in names for host in listed))
        require(len(set(listed)) == len(listed))
        require("count" not in placement or type(placement["count"]) is int and placement["count"] >= 1)
        name = service_name(document)
        require(name not in seen and delegated.get(name) == listed)
        seen.add(name)
    UNIT_TYPES[unit["type"]].validate(unit, request, hosts)


class UnitWorker(ceph_native_resources.ResourceWorker):
    """The core worker with one service unit's apply phase and observation."""

    @staticmethod
    def image_key(name):
        """The settings block's manager option for one auxiliary image; a unit only reads it."""
        return ceph_native_resources.IMAGE_KEY + name

    def unit_key(self):
        unit = self.request["service"]
        return "unit-" + unit["type"] + ("-" + unit["id"] if unit["id"] else "")

    def unit_groups(self):
        return [[document] for document in self.request["service"]["documents"]]

    def unit_names(self):
        return [service_name(document) for document in self.request["service"]["documents"]]

    def recovery_scope(self):
        """The cluster's bootstrap scope: the unit member is this block's own input, never part of the cluster's authoring."""
        unused, staging = ceph_native.tunnel_helpers()
        scope = staging.recovery_scope({key: value for key, value in self.request.items() if key != "service"})
        require(canonical(self.markers.read("bootstrap-scope.json")) == canonical(scope), "ownership")
        return scope

    def unit_specs(self):
        limit = (len(self.request["services"]) + len(self.request.get("delegated", []))) * len(self.request["hosts"])
        return self.service_specs(limit=limit)

    def unit_inputs(self):
        """The unit's retained normalized documents, or None before its first apply; another scope is not this unit's."""
        record = self.markers.read(self.unit_key() + "-inputs.json")
        if record is None:
            return None
        scope = {"owner": self.request["owner"], "fsid": self.request["fsid"], "image": self.request["image"], "inputs": self.unit_groups()}
        require(isinstance(record, dict) and set(record) == {"version", "scope", "services"} and record["version"] == INPUTS_VERSION
                and record["scope"] == scope, "ownership")
        services = record["services"]
        require(isinstance(services, list) and len(services) == len(scope["inputs"]), "ownership")
        require(all(isinstance(group, list) and len(group) == 1 and isinstance(group[0], dict) for group in services), "ownership")
        return [group[0] for group in services]

    def unit_records(self, values, observed, scope):
        """Each document's attempt and completion equal its intent; a stored specification needs this operation's attempt."""
        for name, value in zip(self.unit_names(), values):
            prefix = "service-" + name
            intent = {"scope": scope, "service": value}
            attempt, complete = self.markers.read(prefix + "-attempt.json"), self.markers.read(prefix + "-complete.json")
            require(attempt is None or attempt == intent, "ownership")
            require(complete is None or complete == intent and attempt == intent, "ownership")
            present = observed.get(name)
            require(present is None or attempt == intent and present == value, "foreign-service")
            require(complete is None or present == value, "foreign-service")

    def unit(self):
        """Ownership and the type's checks before any write; then the inputs record, record checks, attempts, one batch, readback, completions."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        unit = self.request["service"]
        hooks = UNIT_TYPES[unit["type"]]
        checked = hooks.check(self, unit)
        values = [group[0] for group in self.normalized_services(self.unit_groups(), record=self.unit_key() + "-inputs.json")]
        scope = self.recovery_scope()
        observed = self.unit_specs()
        self.unit_records(values, observed, scope)
        hooks.claim(self, unit, checked)
        names = self.unit_names()
        missing = [(name, value) for name, value in zip(names, values) if name not in observed]
        for name, value in missing:
            if self.markers.read("service-" + name + "-attempt.json") is None:
                self.markers.write("service-" + name + "-attempt.json", {"scope": scope, "service": value})
        changed = bool(missing)
        if missing:
            self.tunnel_effect("orch", "apply", "-i", "-", data=b"---\n".join(canonical(value) + b"\n" for unused, value in missing), json_result=False)
            observed = self.unit_specs()
            require(all(observed.get(name) == value for name, value in zip(names, values)), "not-ready")
        for name, value in zip(names, values):
            if self.markers.read("service-" + name + "-complete.json") is None:
                self.markers.write("service-" + name + "-complete.json", {"scope": scope, "service": value})
        hooks.settle(self, unit)
        return {"changed": changed}

    def unfinished(self, evidence):
        if self.request["action"] != "service":
            return super().unfinished(evidence)
        evidence["owned"] = self.unit_inputs() is not None
        evidence["exists"] = evidence["owned"]
        evidence["partial"] = evidence["owned"]
        return evidence

    def observe_resource(self, evidence, ready):
        if self.request["action"] != "service":
            return super().observe_resource(evidence, ready)
        unit = self.request["service"]
        hooks = UNIT_TYPES[unit["type"]]
        names = self.unit_names()
        values = self.unit_inputs()
        owned = values is not None
        stored = self.unit_specs()
        present = {name: stored[name] for name in names if name in stored}
        with self.manager_served():
            rows = self.ceph("orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        running = [row for row in rows if row.get("service_name") in names]
        require(owned or not present and not running, "foreign-service")
        evidence["owned"] = owned
        evidence["exists"] = owned or bool(present) or bool(running)
        completed = owned
        if owned:
            self.unit_records(values, present, self.recovery_scope())
            completed = all(self.markers.read("service-" + name + "-complete.json") is not None for name in names)
        running_ready = self.unit_daemons(hooks.expected(unit), running)
        for row in running:
            require(named(row.get("hostname")) and isinstance(row.get("daemon_type"), str) and isinstance(row.get("daemon_id"), str), "native-output")
        evidence["daemons"] = sorted(({"host": row["hostname"], "name": row["daemon_type"] + "." + row["daemon_id"], "service": row["service_name"]}
                                      for row in running), key=lambda item: item["name"])[:MAX_DAEMONS]
        unit_ready = owned and hooks.readiness(self, unit, self.blocking)
        return ready and completed and running_ready and unit_ready

    def pending(self, entry):
        if entry not in self.blocking:
            self.blocking.append(entry)

    def unit_daemons(self, expected, running):
        """Each expected (service, daemon type) runs its count of daemons on its frozen image; a row on another image refuses."""
        auxiliary = {entry["name"]: entry["image"] for entry in self.request.get("auxiliary", [])}
        ready = True
        for (service, kind), count in expected.items():
            frozen = self.request["image"] if kind == "nfs" else auxiliary.get(kind)
            started = 0
            for row in running:
                if row["service_name"] != service or row.get("daemon_type") != kind:
                    continue
                images = row.get("container_image_digests") or []
                require(isinstance(images, list) and all(isinstance(image, str) for image in images), "native-output")
                require(not images or frozen in images, "artifact")
                started += type(row.get("status")) is int and row["status"] == 1 and frozen in images
            if started != count:
                ready = False
                self.pending(kind + "@" + service)
        for row in running:
            if (row["service_name"], row.get("daemon_type")) not in expected:
                ready = False
                self.pending(str(row.get("daemon_type")) + "@" + row["service_name"])
        return ready
