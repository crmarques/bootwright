"""Run the shipped add-on instance apply through ansible-core, then replay it.

`playbooks/addons/instance_apply.yml` runs the add-on instance role and the
shared cluster object program it includes over the real runner protocol,
against a stub `oc` holding the cluster's objects under a temporary root; the
shared harness asserts the changed first run and the unchanged replay.
"""

import json
from pathlib import Path
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "setup_entry_point"))
import cluster_objects  # noqa: E402  pylint: disable=wrong-import-position

DIGEST = "a" * 64


def created(kind, name, namespace=""):
    body = {"apiVersion": "v1", "kind": kind, "metadata": {"name": name}}
    if namespace:
        body["metadata"]["namespace"] = namespace
        body["data"] = {"mode": "example"}
    return {
        "kind": "create",
        "create": {
            "apiVersion": "v1",
            "kind": kind,
            "name": name,
            "namespace": namespace,
            "namespaced": bool(namespace),
            "body": body,
            "desiredDigest": "d" * 64,
        },
    }


def variables(kubeconfig, oc, evidence):
    return {
        "bootwright_addon_instance_request": {
            "version": "addon-instance-request-v1",
            "binding": "example",
            "budgetSeconds": 300,
            "cluster": "example",
            "descriptor": "example",
            "package": "example",
            "placement": {"machine": "target", "connection": "local"},
            "placementNamespaces": ["example"],
            "producedInputs": [],
            "program": {
                "version": "cluster-objects-v1",
                "steps": [created("Namespace", "example"), created("ConfigMap", "example", "example")],
                "checks": [],
            },
            "secrets": [],
            "tool": {},
        },
        "bootwright_addon_instance_material": {
            "operation": "operation",
            "block": "block",
            "registered": "2026-01-01T00:00:00Z",
            "oc": oc,
            "kubeconfig": kubeconfig,
            "evidence": json.dumps(evidence),
        },
        "bootwright_addon_instance_digest": DIGEST,
    }


with tempfile.TemporaryDirectory(prefix="bootwright-addon-instance-") as work:
    cluster_objects.run(work, "addons/instance_apply.yml", variables, DIGEST, "addon-instance-evidence-v1", 2)
print("PASS add-on instance apply published its objects and a replay changed nothing")
