"""The bounded oc process and projected native object transport."""
from __future__ import annotations

import copy
import json
import os
import re
import subprocess
import time

from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_model import OWNER

# Kubernetes Go templates have no JSON encoder. This closed recursive template
# emits only the subtrees selected below, with strings escaped by printf.
JSON_TEMPLATE = ('{{define "json"}}{{$t := printf "%T" .}}'
                 '{{if eq $t "map[string]interface {}"}}{ '
                 '{{$comma := false}}{{range $k,$v := .}}{{if $comma}},{{end}}{{$comma = true}}'
                 '{{printf "%q" $k}}:{{template "json" $v}}{{end}}}'
                 '{{else if eq $t "[]interface {}"}}[{{range $i,$v := .}}{{if $i}},{{end}}'
                 '{{template "json" $v}}{{end}}]'
                 '{{else if eq $t "string"}}{{printf "%q" .}}'
                 '{{else if eq $t "<nil>"}}null{{else}}{{.}}{{end}}{{end}}')


def resource(value, listing=False):
    allowed_listing = listing and not value["name"] and value["kind"] in ("InstallPlan", "ClusterServiceVersion")
    if (not re.fullmatch(r"[A-Za-z][A-Za-z0-9]*", value["kind"])
            or not re.fullmatch(r"(?:[a-z0-9.-]+/)?[a-z][a-z0-9]*", value["apiVersion"])
            or not allowed_listing and not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", value["name"])
            or (value.get("namespace") and not re.fullmatch(r"[a-z0-9][a-z0-9-]*", value["namespace"]))):
        raise ValueError("invalid object identity")
    version = value["apiVersion"].split("/")
    return ".".join([value["kind"], version[-1]] + version[:-1])


def projection(value, fields):
    metadata = {key: True for key in ("name", "namespace", "uid", "creationTimestamp", "generation")}
    metadata["annotations"] = {OWNER: True}
    tree = {"apiVersion": True, "kind": True, "metadata": metadata}
    for path in fields:
        if path == "metadata.annotations" and value["kind"] == "ClusterServiceVersion":
            metadata["annotations"]["olm.maxOpenShiftVersion"] = True
            continue
        cursor = tree
        segments = path.split(".")
        for part in segments[:-1]:
            if cursor.get(part) is True:
                break
            cursor = cursor.setdefault(part, {})
        else:
            cursor[segments[-1]] = True
    if value["kind"] == "Secret":
        tree.pop("data", None)
        tree.pop("stringData", None)
        tree["type"] = True
    suffix = secret_keys() + secret_strings(value.get("strings", {})) if value["kind"] == "Secret" else ""
    return JSON_TEMPLATE + template_tree(tree) + suffix


def template_tree(tree, expression="."):
    parts = []
    for key, subtree in tree.items():
        child = '(index %s %s)' % (expression, json.dumps(key))
        rendered = '{{template "json" %s}}' % child if subtree is True else '{{with %s}}%s{{else}}{}{{end}}' % (child, template_tree(subtree))
        parts.append(json.dumps(key) + ":" + rendered)
    return "{" + ",".join(parts) + "}"


def secret_keys():
    # A second JSON value holds keys only. Never serialize a Secret's data map.
    return '\n[{{$comma := false}}{{range $k,$v := .data}}{{if $comma}},{{end}}{{$comma = true}}{{printf "%q" $k}}{{end}}]'


def secret_strings(strings):
    import base64
    comparisons = []
    for key, value in strings.items():
        encoded = base64.b64encode(value.encode()).decode("ascii")
        comparisons.append(json.dumps(key) + ':{{if .data}}{{eq (index .data %s) %s}}{{else}}false{{end}}' % (json.dumps(key), json.dumps(encoded)))
    return "\n{" + ",".join(comparisons) + "}"


def run(oc, kubeconfig, argv, deadline, body=None):
    if not os.path.isabs(oc) or not os.path.isabs(kubeconfig):
        raise ValueError("absolute client and material paths required")
    remaining = deadline - time.time()
    if remaining <= 0:
        return 1, "", ""
    command = [oc, "--kubeconfig", kubeconfig, "--request-timeout=30s"] + argv
    environment = {"HOME": "/nonexistent", "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8"}
    try:
        # A process timeout must kill oc, including when its HTTP timeout cannot progress.
        # pylint: disable-next=ansible-bad-function
        result = subprocess.run(command, input=body, stdin=subprocess.DEVNULL if body is None else None,
                                capture_output=True, text=True,
                                timeout=min(30, remaining), env=environment, check=False)
    except (OSError, subprocess.TimeoutExpired):
        return 1, "", ""
    return result.returncode, result.stdout, result.stderr


def decode(output, secret=False):
    decoder = json.JSONDecoder()
    value, end = decoder.raw_decode(output.strip())
    if secret:
        remainder = output.strip()[end:].strip()
        value["dataKeys"], end = decoder.raw_decode(remainder)
        value["stringsEqual"] = json.loads(remainder[end:])
    uid = value.get("metadata", {}).get("uid", "")
    if not isinstance(uid, str) or not uid or len(uid.encode()) > 256 or any(not c.isprintable() for c in uid):
        raise ValueError("invalid native uid")
    return value


def get(params):
    obj = params["object"]
    listing = not obj["name"]
    if listing and obj["kind"] not in ("InstallPlan", "ClusterServiceVersion"):
        raise ValueError("unsupported object listing")
    argv = ["get", resource(obj, listing=listing)] + ([obj["name"], "--ignore-not-found=true"] if not listing else [])
    if obj.get("namespace"):
        argv += ["--namespace", obj["namespace"]]
    template = projection(obj, params.get("fields", []))
    if listing:
        template = JSON_TEMPLATE + '{"items":[{{range $i,$v := .items}}{{if $i}},{{end}}{{with $v}}' + template[len(JSON_TEMPLATE):] + '{{end}}{{end}}]}'
    argv += ["-o", "go-template=" + template]
    code, output, ignored_error = run(params["oc"], params["kubeconfig"], argv, params["deadline"])
    if code:
        return {"answered": False}
    if not output.strip():
        return {"answered": True, "object": None}
    try:
        value = json.loads(output) if listing else decode(output, obj["kind"] == "Secret")
        if listing and (not isinstance(value, dict) or not isinstance(value.get("items"), list)):
            raise ValueError("invalid object listing")
    except (ValueError, TypeError):
        return {"answered": False}
    return {"answered": True, "object": value}


def create(params):
    obj = params["object"]
    resource(obj)
    if obj["kind"] == "Secret" or obj["body"].get("apiVersion") != obj["apiVersion"] or obj["body"].get("kind") != obj["kind"]:
        raise ValueError("invalid create body")
    body = copy.deepcopy(obj["body"])
    body.setdefault("metadata", {}).setdefault("annotations", {})[OWNER] = params["owner"]
    argv = ["create", "--filename=-", "--field-manager", params["manager"],
            "-o", "go-template=" + projection(obj, params.get("fields", []))]
    if obj.get("namespace"):
        argv += ["--namespace", obj["namespace"]]
    code, output, error = run(params["oc"], params["kubeconfig"], argv, params["deadline"], json.dumps(body))
    if code:
        reason = ""
        for text in (output, error):
            try:
                status = json.loads(text)
                if status.get("kind") == "Status":
                    reason = status.get("reason", "")
            except (ValueError, AttributeError):
                pass
        return {"accepted": False, "retryable": reason in ("InternalError", "ServiceUnavailable")}
    try:
        return {"accepted": True, "object": decode(output)}
    except (ValueError, TypeError):
        return {"accepted": False, "retryable": False}


def approve(params):
    plan = params["plan"]
    patches = [{"op": "test", "path": path, "value": value} for path, value in (
        ("/metadata/uid", plan["metadata"]["uid"]),
        ("/metadata/ownerReferences", plan["metadata"]["ownerReferences"]),
        ("/spec/clusterServiceVersionNames", plan["spec"]["clusterServiceVersionNames"]),
        ("/spec/approval", "Manual"),
        ("/status/bundleLookups", plan["status"]["bundleLookups"]))]
    patches += [{"op": "add", "path": "/spec/approved", "value": True}]
    argv = ["patch", "InstallPlan.v1alpha1.operators.coreos.com", params["object"]["name"],
            "--namespace", params["object"]["namespace"], "--type=json", "--patch", json.dumps(patches), "-o", "name"]
    code, ignored_output, ignored_error = run(params["oc"], params["kubeconfig"], argv, params["deadline"])
    return {"accepted": code == 0}
