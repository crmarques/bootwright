"""Rotate only the existing bootstrap console account, pinned by UID and hash."""

from __future__ import annotations

import base64
import datetime
import json
import time

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native


def read(client, kubeconfig):
    code, output, unused = native.run(client, kubeconfig, ["get", "secret", "kubeadmin", "--namespace", "kube-system", "--output", "json"], time.time() + 31)
    if code != 0 or len(output) > 16384:
        raise ValueError("original account unavailable")
    value = json.loads(output)
    code, output, unused = native.run(client, kubeconfig, ["get", "namespace", "kube-system", "--output", "json"], time.time() + 31)
    if code != 0 or len(output) > 16384:
        raise ValueError("bootstrap namespace unavailable")
    namespace = json.loads(output)["metadata"]
    if namespace.get("deletionTimestamp") or value["metadata"].get("deletionTimestamp"):
        raise ValueError("bootstrap account deleting")
    created = datetime.datetime.fromisoformat(value["metadata"]["creationTimestamp"].replace("Z", "+00:00"))
    bootstrap = datetime.datetime.fromisoformat(namespace["creationTimestamp"].replace("Z", "+00:00"))
    if not 0 <= (created - bootstrap).total_seconds() <= 3600:
        raise ValueError("bootstrap account disabled")
    metadata = value["metadata"]
    guard = {"namespaceUID": namespace["uid"], "uid": metadata["uid"], "resourceVersion": metadata["resourceVersion"], "hash": value["data"]["kubeadmin"]}
    if value.get("kind") != "Secret" or metadata.get("name") != "kubeadmin" or metadata.get("namespace") != "kube-system":
        raise ValueError("original account identity")
    if any(not isinstance(item, str) or not item or len(item) > 256 for item in guard.values()):
        raise ValueError("original account identity")
    hashed = base64.b64decode(guard["hash"], validate=True)
    if not hashed.startswith(b"$2") or len(hashed) != 60:
        raise ValueError("bootstrap password hash")
    return guard


def update(client, kubeconfig, candidate):
    guard = candidate["guard"]
    desired = base64.b64encode(candidate["hash"].encode("ascii")).decode("ascii")
    current = read(client, kubeconfig)
    if current["uid"] != guard["uid"] or current["namespaceUID"] != guard["namespaceUID"]:
        raise ValueError("bootstrap account replaced")
    if current["hash"] != desired:
        if current != guard:
            raise ValueError("bootstrap account changed")
        patch = [{"op": "test", "path": "/metadata/uid", "value": guard["uid"]},
                 {"op": "test", "path": "/metadata/resourceVersion", "value": guard["resourceVersion"]},
                 {"op": "test", "path": "/data/kubeadmin", "value": guard["hash"]},
                 {"op": "replace", "path": "/data/kubeadmin", "value": desired}]
        argv = ["patch", "secret", "kubeadmin", "--namespace", "kube-system", "--type", "json", "--patch-file", "/dev/stdin"]
        code, unused_output, unused_error = native.run(client, kubeconfig, argv, time.time() + 31, json.dumps(patch))
        if code != 0:
            raise ValueError("bootstrap account update unproved")
    proved = read(client, kubeconfig)
    if proved["uid"] != guard["uid"] or proved["namespaceUID"] != guard["namespaceUID"] or proved["hash"] != desired:
        raise ValueError("bootstrap account update unproved")
