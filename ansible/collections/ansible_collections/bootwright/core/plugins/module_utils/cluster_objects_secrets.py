"""Secret placement compares lent bytes privately and returns no value."""
from __future__ import annotations

import base64
import json
import os
import stat

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native

MAX_PART = 1 << 20


def material(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        status = os.fstat(descriptor)
        if not stat.S_ISREG(status.st_mode) or status.st_size > MAX_PART:
            raise ValueError("unsafe Secret material")
        with os.fdopen(descriptor, "rb", closefd=False) as stream:
            data = stream.read(MAX_PART + 1)
        if len(data) > MAX_PART:
            raise ValueError("oversized Secret material")
        return base64.b64encode(data).decode("ascii")
    finally:
        os.close(descriptor)


def desired(placement, paths, owner):
    if placement.get("produced"):
        source = placement["produced"]
        if source.get("kind") != "CephConsumer" or source.get("output") != "external-details" or placement.get("secret") or placement.get("keys"):
            raise ValueError("unsupported produced Secret source")
        data = {placement["producedKey"]: material(paths["produced:export:" + source["object"]]["value"])}
    else:
        parts = paths[placement["secret"]]
        data = {entry["key"]: material(parts[entry["part"]]) for entry in placement.get("keys", [])}
    for key, value in placement.get("strings", {}).items():
        if key in data:
            raise ValueError("duplicate Secret key")
        data[key] = base64.b64encode(value.encode()).decode("ascii")
    return {"apiVersion": "v1", "kind": "Secret", "metadata": {
        "namespace": placement["namespace"], "name": placement["name"],
        "labels": placement.get("labels", {}), "annotations": {model.OWNER: owner}},
        "type": placement["type"], "data": data}


def read_private(params):
    obj = params["object"]
    projection = native.JSON_TEMPLATE + native.template_tree({
        "metadata": {"uid": True, "creationTimestamp": True, "annotations": {model.OWNER: True}, "labels": True},
        "type": True, "data": True})
    argv = ["get", "Secret.v1", obj["name"], "--namespace", obj["namespace"],
            "--ignore-not-found=true", "-o", "go-template=" + projection]
    code, output, ignored_error = native.run(params["oc"], params["kubeconfig"], argv, params["deadline"])
    if code:
        return False
    if not output.strip():
        return None
    try:
        return json.loads(output)
    except ValueError:
        return False


def place(params):
    obj = params["object"]
    body = desired(params["placement"], params["materials"], params["owner"])
    live = read_private(params)
    if live is False:
        return {"accepted": False, "unknown": True}
    if live is not None:
        if model.ownership(obj, live, params["context"]) != "own":
            return {"accepted": False, "difference": "ownership"}
        for key in ("type", "data"):
            differing = model.difference(body[key], live.get(key), key, strict=True)
            if differing:
                return {"accepted": False, "difference": differing}
        differing = model.difference(body["metadata"].get("labels", {}), live["metadata"].get("labels", {}), "metadata.labels")
        if differing:
            return {"accepted": False, "difference": differing}
        return {"accepted": True, "equal": True, "uid": live["metadata"]["uid"], "basis": model.basis(obj, live, params["context"])}
    argv = ["create", "--filename=-", "--namespace", obj["namespace"], "--field-manager", params["manager"], "-o", "name"]
    code, ignored_output, ignored_error = native.run(params["oc"], params["kubeconfig"], argv, params["deadline"], json.dumps(body))
    # A read after any create resolves a response lost after the API persisted it.
    live = read_private(params)
    if live and model.ownership(obj, live, params["context"]) == "own":
        differing = model.difference(body["data"], live.get("data"), "data", strict=True)
        labels = model.difference(body["metadata"].get("labels", {}), live["metadata"].get("labels", {}), "metadata.labels")
        if not differing and not labels and live.get("type") == body["type"]:
            basis = "created" if code == 0 else model.basis(obj, live, params["context"])
            return {"accepted": True, "equal": code != 0, "uid": live["metadata"]["uid"], "basis": basis}
    return {"accepted": False, "unknown": live is False}
