"""Run the shipped cluster network apply through ansible-core, then replay it.

`playbooks/containercluster/network_apply.yml` runs the cluster network role
and the shared cluster object program it includes over the real runner
protocol. The program is the compiler-rendered networking program the unit
tests run (`tests/unit/fixtures/cluster_objects/network.json`): it waits for
two served types, creates two node network policies, waits for them and two
user-defined networks, and checks the networks ready. A stub `oc` holds the
cluster under a temporary root; the shared harness asserts the changed first
run and the unchanged replay.
"""

import json
from pathlib import Path
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "setup_entry_point"))
import cluster_objects  # noqa: E402  pylint: disable=wrong-import-position
import entry_point  # noqa: E402  pylint: disable=wrong-import-position

DIGEST = "b" * 64
PROGRAM = json.loads(
    (entry_point.collection() / "tests/unit/fixtures/cluster_objects/network.json").read_text(encoding="utf-8")
)


def variables(kubeconfig, oc, evidence):
    return {
        "bootwright_cluster_network_request": {
            "version": "cluster-network-request-v1",
            "budgetSeconds": 300,
            "cluster": "example",
            "placement": {"machine": "target", "connection": "local"},
            "producedInputs": [],
            "program": PROGRAM,
            "tool": {},
        },
        "bootwright_cluster_network_material": {
            "operation": "operation",
            "block": "block",
            "registered": "2026-01-01T00:00:00Z",
            "oc": oc,
            "kubeconfig": kubeconfig,
            "evidence": json.dumps(evidence),
        },
        "bootwright_cluster_network_digest": DIGEST,
    }


with tempfile.TemporaryDirectory(prefix="bootwright-cluster-network-") as work:
    replayed = cluster_objects.run(work, "containercluster/network_apply.yml", variables, DIGEST, "cluster-network-evidence-v1", 4)
    # Each of the three waits and the final check of both networks is recorded.
    checks = replayed["evidence"]["checks"]
    if len(checks) != 6 or not all(check["satisfied"] for check in checks):
        raise entry_point.Failure("the replay did not prove every wait and check satisfied")
print("PASS cluster network apply published its objects and a replay changed nothing")
