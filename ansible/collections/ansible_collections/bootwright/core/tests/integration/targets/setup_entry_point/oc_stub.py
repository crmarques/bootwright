"""A stub `oc` holding cluster objects in one file under the temporary root.

It answers the cluster object transport's three verbs: `get` prints the
stored object, or nothing for an absent one; `create` stores the body it reads
with a server uid; `patch` accepts. Every object it holds reports the readiness
condition its kind waits on as True, and a CustomResourceDefinition it does not
hold reads as an established external one, as a cluster that serves the type.
It prints plain JSON: the transport decodes a value that is not its tagged
projection as the value itself. Each call is appended to the call log.
"""

import json
from pathlib import Path
import sys

STATE = Path(__file__).resolve().parent.parent / "cluster"
CONDITIONS = {"CustomResourceDefinition": "Established", "ClusterUserDefinedNetwork": "NetworkCreated"}


def option(argv, name):
    return argv[argv.index(name) + 1] if name in argv else ""


def ready(live):
    condition = CONDITIONS.get(live["kind"], "Available")
    return dict(live, status={"conditions": [{"type": condition, "status": "True", "reason": "Ready"}]})


def main(argv):
    STATE.mkdir(mode=0o700, exist_ok=True)
    store, log = STATE / "objects.json", STATE / "calls.json"
    objects = json.loads(store.read_text()) if store.exists() else {}
    calls = json.loads(log.read_text()) if log.exists() else []
    if argv[:1] != ["--kubeconfig"] or not Path(argv[1]).is_file() or argv[2] != "--request-timeout=30s":
        sys.stderr.write("error: the stub reads only the transport's fixed client options\n")
        return 2
    verb, rest = argv[3], argv[4:]
    calls.append({"verb": verb, "argv": rest})
    log.write_text(json.dumps(calls))
    namespace = option(rest, "--namespace")
    if verb == "get":
        kind, name = rest[0].split(".")[0], rest[1]
        live = objects.get("/".join((kind, namespace, name)))
        if live is None and kind == "CustomResourceDefinition":
            live = {"apiVersion": "apiextensions.k8s.io/v1", "kind": kind, "metadata": {"name": name, "uid": "external-" + name}}
        if live is not None:
            sys.stdout.write(json.dumps(ready(live)))
        return 0
    if verb == "create":
        body = json.loads(sys.stdin.read())
        metadata = body["metadata"]
        key = "/".join((body["kind"], namespace or metadata.get("namespace", ""), metadata["name"]))
        if key in objects:
            sys.stderr.write("Error from server (AlreadyExists): exists\n")
            return 1
        metadata.update(uid="uid-%d" % (len(objects) + 1), creationTimestamp="2026-01-01T00:00:01Z", generation=1)
        objects[key] = body
        store.write_text(json.dumps(objects))
        sys.stdout.write(json.dumps(ready(body)))
        return 0
    if verb == "patch":
        return 0
    sys.stderr.write("error: unknown verb\n")
    return 2


sys.exit(main(sys.argv[1:]))
