"""Ordered object execution with one deadline and read-only observation."""
from __future__ import annotations

import time

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model


def olm_ref(kind, namespace, name):
    return {"apiVersion": "operators.coreos.com/v1alpha1", "kind": kind,
            "namespace": namespace, "name": name, "namespaced": True}


def fields(obj):
    selected = list(obj.get("body", {}))
    selected = [key for key in selected if key != "metadata"]
    selected += ["metadata." + key for key in obj.get("body", {}).get("metadata", {})]
    return selected


class Refused(ValueError):
    """A bounded diagnostic containing only object identities and field names."""


class Engine:
    def __init__(self, request, read, write=None, clock=time.time, sleep=time.sleep):
        self.request, self.read, self.write = request, read, write
        self.clock, self.sleep = clock, sleep
        self.deadline = request["deadline"]
        self.state = {"receipts": [dict(receipt) for receipt in request.get("receipts", [])], "observations": [], "olm": [], "dependents": [], "checks": []}
        self.changed = False
        self.unknown = False
        self.not_ready = False
        self.plan_readings = {}
        self.csv_readings = {}
        self.snapshot = None
        self.projections = {}

    def pause(self, description):
        remaining = self.deadline - self.clock()
        if remaining <= 0:
            raise Refused("deadline reached: " + description)
        self.sleep(min(10, remaining))

    def probe(self, obj, selected=(), wait=False):
        key = model.identity(obj)
        if self.snapshot is not None and key in self.snapshot:
            return self.snapshot[key]
        while True:
            selected = sorted(set(selected) | set(self.projections.get(key, [])))
            result = self.read(obj, list(selected))
            if result.get("answered") is True:
                live = result.get("object")
                if self.snapshot is not None:
                    self.snapshot[key] = live
                return live
            if not wait:
                self.unknown = self.unknown or self.snapshot is not None
                if self.snapshot is not None:
                    self.snapshot[key] = False
                return False
            self.pause(model.label(obj) + " read unknown")

    def keep_receipt(self, receipt):
        key = model.identity(receipt)
        self.state["receipts"] = [r for r in self.state["receipts"] if model.identity(r) != key]
        self.state["receipts"].append(receipt)

    def record(self, obj, live, own, equal):
        observation = dict(model.ref(obj), ownership=own, equal=equal)
        self.state["observations"].append(observation)
        if own == "own" and equal:
            receipt = dict(model.ref(obj), uid=live["metadata"]["uid"],
                           desiredDigest=obj["desiredDigest"], disposition="unchanged")
            self.keep_receipt(receipt)
            if not any(model.identity(r) == model.identity(obj) for r in self.request.get("receipts", [])):
                self.request.setdefault("receipts", []).append(receipt)
        return observation

    def object(self, obj, observe=False):
        while True:
            live = self.probe(obj, fields(obj), wait=not observe)
            own = "unknown" if live is False else model.ownership(obj, live, self.request)
            differing = model.compare(obj, live) if own == "own" else ""
            if observe:
                self.record(obj, live, own, own == "own" and not differing)
                return
            if own == "own":
                if differing:
                    raise Refused(model.label(obj) + " differs at " + differing)
                self.record(obj, live, own, True)
                return
            if own == "unknown":
                self.pause(model.label(obj) + " ownership unknown")
                continue
            if own != "absent":
                raise Refused(model.label(obj) + " ownership " + own)
            result = self.write("create", obj, {
                "owner": model.mark(self.request["operation"], self.request["block"], obj),
                "manager": "bootwright-" + model.mark(self.request["operation"], self.request["block"])[:32],
                "fields": fields(obj)})
            if result.get("accepted") and result.get("object", {}).get("metadata", {}).get("uid"):
                created = result["object"]
                differing = model.compare(obj, created)
                if differing:
                    raise Refused(model.label(obj) + " differs at " + differing)
                receipt = dict(model.ref(obj), uid=created["metadata"]["uid"],
                               desiredDigest=obj["desiredDigest"], disposition="created")
                self.request.setdefault("receipts", []).append(receipt)
                self.changed = True
                self.keep_receipt(receipt)
                self.state["observations"].append(dict(model.ref(obj), ownership="own", equal=True))
                return
            live = self.probe(obj, fields(obj), wait=True)
            own = model.ownership(obj, live, self.request)
            if own == "own":
                if model.compare(obj, live):
                    raise Refused(model.label(obj) + " differs after create")
                self.record(obj, live, own, True)
                return
            if own != "absent" or not result.get("retryable"):
                raise Refused(model.label(obj) + " create unproved: " + own)
            self.pause(model.label(obj) + " create not ready")

    def place(self, placement, observe=False):
        obj = {"apiVersion": "v1", "kind": "Secret", "namespace": placement["namespace"],
               "name": placement["name"], "namespaced": True, "desiredDigest": placement["desiredDigest"],
               "strings": placement.get("strings", {})}
        live = self.probe(obj, ["type", "metadata.labels"], wait=not observe)
        own = "unknown" if live is False else model.ownership(obj, live, self.request)
        keys = sorted([entry["key"] for entry in placement.get("keys", [])] + list(placement.get("strings", {})))
        equal = bool(live and own == "own" and live.get("type") == placement["type"]
                     and sorted(live.get("dataKeys", [])) == keys
                     and all(live.get("stringsEqual", {}).get(key) is True for key in placement.get("strings", {}))
                     and not model.difference(placement.get("labels", {}), live["metadata"].get("labels", {})))
        if observe:
            self.record(obj, live, own, equal)
            return
        if own not in ("own", "absent"):
            raise Refused(model.label(obj) + " ownership " + own)
        result = self.write("place", obj, {
            "placement": placement, "materials": self.request.get("materials", {}),
            "owner": model.mark(self.request["operation"], self.request["block"], obj),
            "manager": "bootwright-" + model.mark(self.request["operation"], self.request["block"])[:32],
            "context": {key: self.request[key] for key in ("operation", "block", "registered", "receipts")}})
        if not result.get("accepted"):
            raise Refused(model.label(obj) + " differs at " + result.get("difference", "unproved placement"))
        receipt = dict(model.ref(obj), uid=result["uid"], desiredDigest=obj["desiredDigest"],
                       disposition="unchanged" if result.get("equal") else "created")
        self.changed = self.changed or not result.get("equal")
        self.keep_receipt(receipt)
        self.state["observations"].append(dict(model.ref(obj), ownership="own", equal=True))

    def check(self, check, index, observe=False):
        kind, value = next(iter(check.items()))
        if kind == "csvSucceeded":
            ready, last = self.csv(value, observe)
            return {"index": index, "satisfied": ready, "stale": False, "lastObservation": last}
        selected = ["status.conditions", "status.observedGeneration"] if kind == "condition" else []
        if kind == "statusPhase":
            selected = [value.get("path") or "status.phase"]
        live = self.probe(value, selected, wait=False)
        ready, stale, last, failed = False, False, "absent" if live is None else "unknown", False
        if live:
            if kind == "resourceExists":
                ready, last = True, "present"
            elif kind == "condition":
                ready, stale, last, failed = model.condition(value, live)
            elif kind == "statusPhase":
                phase = model.path_value(live, value.get("path") or "status.phase")
                ready = isinstance(phase, str) and phase in value["values"]
                last = model.native(phase)
        if failed and not observe:
            raise Refused(model.label(value) + " " + last)
        return {"index": index, "satisfied": ready, "stale": stale, "lastObservation": last, "unknown": live is False}

    def csv(self, value, observe):
        subscription = olm_ref("Subscription", value["namespace"], value["subscription"])
        live = self.probe(subscription, ["spec.startingCSV", "status.installedCSV"])
        if not live:
            return False, "absent" if live is None else "unknown"
        expected = value.get("csv") or live.get("spec", {}).get("startingCSV")
        installed = live.get("status", {}).get("installedCSV")
        if installed != expected or model.native(installed, "name") == "invalid":
            if observe and installed:
                self.unknown = True
            return False, model.native(installed or "pending", "name")
        csv = self.probe(olm_ref("ClusterServiceVersion", value["namespace"], installed), ["status.phase", "spec.version", "metadata.annotations"])
        if csv:
            self.csv_readings[(value["namespace"], installed)] = csv
        return bool(csv and csv.get("status", {}).get("phase") == "Succeeded"), "Succeeded" if csv else "pending"

    def checks(self, checks, observe=False, gate=False):
        offset = len(self.state["checks"])
        while True:
            results = [self.check(check, offset + index, observe) for index, check in enumerate(checks)]
            if observe or all(item["satisfied"] for item in results):
                self.not_ready = self.not_ready or not all(item["satisfied"] for item in results)
                if not gate:
                    self.state["checks"].extend(results)
                return
            last = "; ".join("check %d: %s" % (r["index"], r["lastObservation"])
                             for r in results if not r["satisfied"])
            self.pause(last)

    def dependent(self, expected, observe=False):
        obj = olm_ref("Subscription", expected["namespace"], expected["subscription"])
        creator = self.probe(expected["creator"], wait=not observe)
        creator_ownership = model.ownership(expected["creator"], creator, self.request) if creator is not False else "unknown"
        while True:
            live = self.probe(obj, ["spec", "status.conditions", "status.installedCSV", "status.currentCSV", "status.installPlanRef"], wait=not observe)
            if not live:
                if observe:
                    self.state["dependents"].append({"namespace": obj["namespace"], "subscription": obj["name"], "uid": ""})
                    return None
                self.pause(model.label(obj) + " absent")
                continue
            desired = {key: expected[key] for key in ("channel", "source", "sourceNamespace")}
            desired.update(name=expected["package"], startingCSV=expected["csv"], installPlanApproval="Manual")
            differing = model.difference(desired, live.get("spec", {}), "spec")
            try:
                older = (creator_ownership != "own" or not creator
                         or model.timestamp(live["metadata"]["creationTimestamp"])
                         < model.timestamp(creator["metadata"]["creationTimestamp"]))
            except (KeyError, TypeError, ValueError):
                older = True
            if differing or older:
                if observe:
                    self.unknown = True
                    return None
                raise Refused(model.label(obj) + " differs at " + (differing or "metadata.creationTimestamp"))
            return live

    def subscription(self, step, dependent=False, observe=False):
        expected = step if dependent else step["object"]
        obj = olm_ref("Subscription", expected["namespace"], expected["subscription"]) if dependent else expected
        csv = step["csv"]
        if not dependent:
            self.object(obj, observe)
        while True:
            selected = ["spec", "status.conditions", "status.installedCSV", "status.currentCSV", "status.installPlanRef"]
            live = self.dependent(step, observe) if dependent else self.probe(obj, selected, wait=not observe)
            if not live:
                if observe and not dependent:
                    self.state["olm"].append({"namespace": obj["namespace"], "subscription": obj["name"], "installedCSV": ""})
                return
            if not dependent and (model.ownership(obj, live, self.request) != "own" or model.compare(obj, live)):
                if observe:
                    self.unknown = True
                    return
                raise Refused(model.label(obj) + " differs at spec")
            for condition in (live.get("status", {}).get("conditions") or []):
                if condition.get("type") == "ResolutionFailed" and condition.get("status") == "True":
                    if observe:
                        self.unknown = True
                        return
                    raise Refused(model.label(obj) + " resolution failed: " + model.native(condition.get("message", "")))
            plan_name = (live.get("status", {}).get("installPlanRef") or {}).get("name", "")
            approved = self.plan(obj, live, csv, plan_name, observe) if plan_name else None
            installed = live.get("status", {}).get("installedCSV", "")
            succeeded, last = self.csv({"namespace": obj["namespace"], "subscription": obj["name"], "csv": csv}, observe)
            if observe or succeeded:
                self.not_ready = self.not_ready or not succeeded
                fact = {"namespace": obj["namespace"], "subscription": obj["name"],
                        "installedCSV": model.native(installed, "name") if installed else "", "installPlan": approved or {}}
                if dependent:
                    fact["uid"] = live["metadata"]["uid"]
                else:
                    fact.update(self.olm_facts(obj, live, approved, installed))
                    fact["upgradePending"] = live.get("status", {}).get("currentCSV", installed) != installed
                self.state["dependents" if dependent else "olm"].append(fact)
                return
            self.pause(model.label(obj) + " " + last)

    def plan(self, subscription, live, csv, name, observe):
        if model.native(name, "name") == "invalid":
            if observe:
                self.unknown = True
                return None
            raise Refused(model.label(subscription) + " invalid plan name")
        obj = olm_ref("InstallPlan", subscription["namespace"], name)
        plan = self.probe(obj, ["spec", "metadata.ownerReferences", "status.bundleLookups"], wait=not observe)
        if not plan:
            return None
        spec, metadata = plan.get("spec", {}), plan.get("metadata", {})
        references = (metadata.get("ownerReferences") or [])
        lookups = (plan.get("status", {}).get("bundleLookups") or [])
        catalog = live.get("spec", {})
        valid = (spec.get("approval") == "Manual" and spec.get("clusterServiceVersionNames") == [csv]
                 and any(r.get("uid") == live["metadata"]["uid"] and r.get("kind") == "Subscription" for r in references)
                 and bool(lookups) and all(lookup.get("catalogSourceRef", {}).get("name") == catalog.get("source")
                                           and lookup.get("catalogSourceRef", {}).get("namespace") == catalog.get("sourceNamespace")
                                           for lookup in lookups))
        if not valid:
            if observe:
                self.unknown = True
                return None
            raise Refused(model.label(obj) + " approval proof differs; CSVs " + model.native(",".join(spec.get("clusterServiceVersionNames", []))))
        if spec.get("approved") is not True and not observe:
            result = self.write("approve", obj, {"plan": plan})
            if not result.get("accepted"):
                return None
            self.changed = True
        self.plan_readings[(obj["namespace"], name)] = plan
        return {"name": name, "uid": metadata["uid"], "csvs": [csv]}

    def olm_facts(self, obj, subscription, approved, installed):
        csv = self.csv_readings.get((obj["namespace"], installed), {})
        spec = subscription["spec"]
        catalog = self.probe(olm_ref("CatalogSource", spec["sourceNamespace"], spec["source"]),
                             ["spec.image"], wait=self.snapshot is None)
        plan = self.plan_readings.get((obj["namespace"], (approved or {}).get("name")), {})
        bundles = (plan.get("status", {}).get("bundleLookups") or [])
        image = bundles[0].get("path", "") if bundles else ""
        maximum = csv.get("metadata", {}).get("annotations", {}).get("olm.maxOpenShiftVersion", "")
        return {"csvVersion": model.native(csv.get("spec", {}).get("version", "")),
                "maxOpenShiftVersion": model.native(maximum, "version") if maximum else "",
                "catalogImage": model.native(catalog.get("spec", {}).get("image", ""), "image") if catalog else "",
                "bundleImage": model.native(image, "image") if image else ""}

    def add_projection(self, obj, selected):
        key = model.identity(obj)
        self.projections[key] = sorted(set(self.projections.get(key, [])) | set(selected))

    def prepare_projections(self, program):
        checks = list(program.get("checks", []))
        for step in program["steps"]:
            if step["kind"] == "create":
                obj = step["create"]
                self.add_projection(obj, fields(obj))
            elif step["kind"] == "subscribe":
                obj = step["subscribe"]["object"]
                self.add_projection(obj, fields(obj) + ["spec", "status.conditions", "status.installedCSV", "status.currentCSV", "status.installPlanRef"])
            elif step["kind"] == "approveDependent":
                dependent = step["approveDependent"]
                obj = olm_ref("Subscription", dependent["namespace"], dependent["subscription"])
                self.add_projection(obj, ["spec", "status.conditions", "status.installedCSV", "status.currentCSV", "status.installPlanRef"])
            elif step["kind"] == "catalogReady":
                self.add_projection(step["catalogReady"], ["spec.image", "status.connectionState.lastObservedState"])
            elif step["kind"] == "wait":
                checks.extend(step["wait"])
        for check in checks:
            kind, value = next(iter(check.items()))
            if kind == "condition":
                self.add_projection(value, ["status.conditions", "status.observedGeneration"])
            elif kind == "statusPhase":
                self.add_projection(value, [value.get("path") or "status.phase"])
            elif kind == "csvSucceeded":
                obj = olm_ref("Subscription", value["namespace"], value["subscription"])
                self.add_projection(obj, ["spec.startingCSV", "status.installedCSV"])

    def execute(self, observe=False):
        program = self.request["program"]
        if observe:
            self.snapshot = {}
            self.prepare_projections(program)
        if program.get("version") != model.VERSION:
            raise Refused("unsupported cluster object program")
        for step in program["steps"]:
            kind = step["kind"]
            if kind == "create":
                self.object(step[kind], observe)
            elif kind == "place":
                self.place(step[kind], observe)
            elif kind == "subscribe":
                if step[kind]["approval"] != "exact":
                    raise Refused("operator-managed approval is unavailable")
                self.subscription(step[kind], observe=observe)
            elif kind == "approveDependent":
                self.subscription(step[kind], dependent=True, observe=observe)
            elif kind == "catalogReady":
                self.checks([{"statusPhase": dict(step[kind], path="status.connectionState.lastObservedState", values=["READY"])}], observe, gate=True)
            elif kind == "wait":
                self.checks(step[kind], observe)
            else:
                raise Refused("unsupported cluster object step")
        self.checks(program.get("checks", []), observe)
        readings = self.state["observations"]
        if self.unknown and readings:
            readings[0]["ownership"] = "unknown"
            readings[0]["equal"] = False
        unknown = self.unknown or any(r["ownership"] in ("unknown", "foreign") for r in readings)
        complete = (not self.not_ready and all(r["ownership"] == "own" and r["equal"] for r in readings)
                    and all(c["satisfied"] for c in self.state["checks"])
                    and all(d.get("uid") for d in self.state["dependents"]))
        absent = bool(readings) and all(r["ownership"] == "absent" for r in readings) and not self.state["dependents"]
        classification = "unknown" if unknown else "completed" if complete else "noEffect" if absent else "partial"
        self.state.update(classification=classification, postcondition=classification == "completed", absent=classification == "noEffect")
        return {"changed": self.changed, "state": self.state}
