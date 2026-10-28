"""Read and add the labels and taints a completed installation places on its nodes.

Placement goes through the cluster API with the kept administrator
kubeconfig, because NodeRestriction forbids a kubelet its role labels and
every taint. It is one JSON patch per node, never a server-side apply: an
apply to a node deleted since the read would create one, and ``spec.taints``
is an atomic list an apply would own whole. Each patch adds only what the node
lacks and is pinned by ``test`` operations on the uid, the whole label map and
the whole spec it read, so another manager's label or taint is never removed,
taken over or replaced, and a node that changed meanwhile refuses.
"""

from __future__ import annotations

import json
import re
import time

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native

MANAGER = "bootwright-cluster-install"
EFFECTS = ("NoSchedule", "PreferNoSchedule", "NoExecute")
MAX_NODES = 128
MAX_LABELS = 64
CALL_SECONDS = 31


class Refused(Exception):
    """A placement that would overwrite or race another writer; the text names nodes and keys only."""


def _text(value, limit=317):
    if not isinstance(value, str) or len(value) > limit or any(not c.isprintable() for c in value):
        raise ValueError("bounded text")
    return value


def declared(nodes):
    """The nodes that declare a placement, each with its labels and taints, in request order."""
    if not isinstance(nodes, list) or len(nodes) > MAX_NODES:
        raise ValueError("declared nodes")
    placed = []
    for node in nodes:
        labels, taints = node.get("labels") or {}, node.get("taints") or []
        if not isinstance(labels, dict) or not isinstance(taints, list) or len(labels) > MAX_LABELS or len(taints) > MAX_LABELS:
            raise ValueError("declared placement")
        if not labels and not taints:
            continue
        name = _text(node["name"], 253)
        if not re.fullmatch(r"[a-z0-9]([a-z0-9.-]*[a-z0-9])?", name):
            raise ValueError("node name")
        placed.append({
            "name": name,
            "labels": {_text(key): _text(value, 63) for key, value in labels.items()},
            "taints": [taint_of(taint) for taint in taints],
        })
    return placed


def taint_of(taint):
    if not isinstance(taint, dict) or taint.get("effect") not in EFFECTS:
        raise ValueError("taint")
    return {"key": _text(taint.get("key")), "value": _text(taint.get("value") or "", 63), "effect": taint["effect"]}


def read(oc, kubeconfig):
    """Every node's uid, labels and spec by name, or None when the API does not answer them."""
    code, output, ignored_error = native.run(oc, kubeconfig, ["get", "nodes", "--output", "json"], time.time() + CALL_SECONDS)
    if code:
        return None
    try:
        items = json.loads(output)["items"]
        found = {}
        for item in items:
            metadata = item["metadata"]
            found[metadata["name"]] = {"uid": metadata["uid"], "labels": metadata.get("labels"), "spec": item.get("spec", {})}
        return found
    except (ValueError, TypeError, KeyError):
        return None


def carried(node, live):
    """What one node carries of its declared placement: the values of its declared label keys and its taints of a declared key."""
    labels = live.get("labels") or {}
    keys = {taint["key"] for taint in node["taints"]}
    taints = []
    for taint in live.get("spec", {}).get("taints") or []:
        if isinstance(taint, dict) and taint.get("key") in keys and taint.get("effect") in EFFECTS:
            taints.append(taint_of({"key": taint["key"], "value": taint.get("value"), "effect": taint.get("effect")}))
    taints.sort(key=lambda taint: (taint["key"], taint["effect"], taint["value"]))
    return {"name": node["name"], "labels": {key: labels[key] for key in node["labels"] if isinstance(labels.get(key), str)}, "taints": taints}


def lacking(node, seen):
    """The declared labels and taints a node lacks, and those it carries with another value."""
    absent_labels, conflicts = {}, []
    for key, value in node["labels"].items():
        if key not in seen["labels"]:
            absent_labels[key] = value
        elif seen["labels"][key] != value:
            conflicts.append("label " + key)
    absent_taints = []
    for taint in node["taints"]:
        same = [found for found in seen["taints"] if found["key"] == taint["key"] and found["effect"] == taint["effect"]]
        if not same:
            absent_taints.append(taint)
        elif all(found["value"] != taint["value"] for found in same):
            conflicts.append("taint %s:%s" % (taint["key"], taint["effect"]))
    return absent_labels, absent_taints, conflicts


def observe(nodes, live):
    """The evidence of what each declared node carries, and the nodes short of their placement."""
    placement, unplaced = [], []
    for node in nodes:
        if live is None or node["name"] not in live:
            unplaced.append(node["name"])
            continue
        seen = carried(node, live[node["name"]])
        placement.append(seen)
        labels, taints, conflicts = lacking(node, seen)
        if labels or taints or conflicts:
            unplaced.append(node["name"])
    return {"answered": live is not None, "placement": placement, "unplaced": unplaced}


def pointer(key):
    return key.replace("~", "~0").replace("/", "~1")


def patch_of(live, labels, taints):
    """One JSON patch adding exactly the absent labels and taints, pinned to what was read."""
    operations = [{"op": "test", "path": "/metadata/uid", "value": live["uid"]}]
    if labels:
        if live["labels"] is None:
            operations.append({"op": "add", "path": "/metadata/labels", "value": labels})
        else:
            operations.append({"op": "test", "path": "/metadata/labels", "value": live["labels"]})
            operations += [{"op": "add", "path": "/metadata/labels/" + pointer(key), "value": value} for key, value in sorted(labels.items())]
    if taints:
        operations.append({"op": "test", "path": "/spec", "value": live["spec"]})
        added = [dict([("key", taint["key"])] + ([("value", taint["value"])] if taint["value"] else []) + [("effect", taint["effect"])])
                 for taint in taints]
        if live["spec"].get("taints"):
            operations += [{"op": "add", "path": "/spec/taints/-", "value": taint} for taint in added]
        else:
            operations.append({"op": "add", "path": "/spec/taints", "value": added})
    return operations


def place(oc, kubeconfig, nodes):
    """Add every declared label and taint a node lacks, then read the placement back.

    Every node is read before any is written, and a conflict on any node
    refuses the whole step with nothing written. A patch refused part way
    leaves the nodes before it placed, which the next attempt reads back."""
    live = read(oc, kubeconfig)
    if live is None:
        raise Refused("the cluster did not answer the read of its nodes, so no node was labelled or tainted; repeat the apply")
    plans, conflicts = [], []
    for node in nodes:
        if node["name"] not in live:
            raise Refused("node %s is not in the cluster, so no node was labelled or tainted" % node["name"])
        labels, taints, found = lacking(node, carried(node, live[node["name"]]))
        conflicts += ["node %s carries %s with another value" % (node["name"], what) for what in found]
        if labels or taints:
            plans.append((node["name"], patch_of(live[node["name"]], labels, taints)))
    if conflicts:
        raise Refused("; ".join(conflicts)[:900] + "; another manager's value is never overwritten, so no node was labelled or tainted:"
                      " remove or correct the value on the node, or change the declaration")
    for name, operations in plans:
        argv = ["patch", "node", name, "--type=json", "--field-manager", MANAGER, "--patch", json.dumps(operations), "--output", "name"]
        code, ignored_output, ignored_error = native.run(oc, kubeconfig, argv, time.time() + CALL_SECONDS)
        if code:
            raise Refused("node %s changed since it was read, or the API refused its labels and taints; repeat the apply to read it again" % name)
    result = observe(nodes, read(oc, kubeconfig))
    result["changed"] = bool(plans)
    return result
