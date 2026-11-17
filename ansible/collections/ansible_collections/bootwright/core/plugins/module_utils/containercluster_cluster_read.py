"""Read an installed cluster back through the kubeconfig this installation keeps.

One read of ClusterVersion answers who the cluster is, which release it
reports and whether its installation completed, and one read of the nodes
answers which declared nodes it holds. Both go through the bounded oc runner,
whose --request-timeout ends each request oc makes. A read that succeeds has
verified the serving certificate against the authority the kept kubeconfig
carries and been authenticated by its client certificate, which only this
build's cluster accepts, so it answers with the identity the apply took from
that file; an API that rejects it is foreign, and one that does not answer is
nothing.
"""

from __future__ import annotations

import json
import re
import time

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_native as native

CALL_SECONDS = 31
FOREIGN = "foreign"
# What oc prints when the API answered and refused this build's anchor: a
# serving certificate its authority does not sign, a client certificate it
# does not accept, or an identity it does not authorize.
REJECTED = re.compile(r"x509: |You must be logged in to the server|Error from server \(Forbidden\)")


def text(value):
    return value if isinstance(value, str) else ""


def completed(status, release):
    """ClusterVersion's own report that the installation finished: its Available condition is True,
    and the newest entry of its update history, which the cluster version operator opens Partial at
    the desired release when the cluster starts, is Completed at the declared release."""
    conditions = status.get("conditions") if isinstance(status.get("conditions"), list) else []
    available = " ".join(text(entry.get("status")) for entry in conditions
                         if isinstance(entry, dict) and entry.get("type") == "Available")
    history = status.get("history")
    newest = history[0] if isinstance(history, list) and history and isinstance(history[0], dict) else {}
    return available == "True" and newest.get("state") == "Completed" and newest.get("version") == release


def version_state(code, output, error, identity, release):
    """The cluster, release and completion one ClusterVersion read answers, and whether it was answered."""
    try:
        version = json.loads(output) if code == 0 else None
    except ValueError:
        version = None
    if not isinstance(version, dict):
        rejected = code != 0 and REJECTED.search(error) is not None
        return {"cluster": FOREIGN if rejected else "", "release": "", "completed": False}, False
    status = version.get("status") if isinstance(version.get("status"), dict) else {}
    desired = status.get("desired") if isinstance(status.get("desired"), dict) else {}
    found = {"cluster": identity or FOREIGN, "release": text(desired.get("version")).strip(),
             "completed": completed(status, release)}
    # An empty history answers no state, as oc's refusal of its first entry did.
    return found, status.get("history") != []


def read(oc, kubeconfig, request, identity):
    """The state {cluster, release, completed, missing} the cluster answers, and whether every read was answered."""
    release = request["release"]["version"]
    declared = [node["name"] for node in request["nodes"]]
    code, output, error = native.run(oc, kubeconfig, ["get", "clusterversion", "version", "--output", "json"],
                                     time.time() + CALL_SECONDS)
    state, answered = version_state(code, output, error, identity, release)
    nodes_code, listed, ignored = native.run(
        oc, kubeconfig, ["get", "nodes", "--output", "jsonpath={.items[*].metadata.name}"], time.time() + CALL_SECONDS)
    held = set(listed.split()) if nodes_code == 0 else set()
    state["missing"] = sorted(set(declared) - held)
    return state, answered and nodes_code == 0
