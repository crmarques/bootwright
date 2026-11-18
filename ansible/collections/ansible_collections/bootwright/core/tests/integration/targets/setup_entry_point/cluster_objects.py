"""Run a capability built on the shared cluster object program, twice.

The add-on instance and cluster network roles each include the shared
`cluster_objects` role with their own request and material. This runs one of
them against the stub `oc` of `oc_stub.py`: the first run creates every object
of the program and publishes a changed `completed` record; the replay, handed
the recorded receipts as the runner hands them, finds each object owned and
unchanged, creates nothing and publishes the same observations.
"""

import json
from pathlib import Path

import entry_point


def receipts(evidence):
    """The objects a record's receipts name, without how the run proved them."""
    return [(r["kind"], r["namespace"], r["name"], r["uid"]) for r in evidence["receipts"]]


def run(root, playbook, variables, digest, version, objects):
    """Run the entry point twice; `variables(kubeconfig, oc, evidence)` builds each run's."""
    entry = entry_point.EntryPoint(root, playbook)
    client = entry_point.stub(entry.stubs, "oc", Path(__file__).with_name("oc_stub.py").read_text(encoding="utf-8"))
    kubeconfig = Path(root, "kubeconfig")
    kubeconfig.write_text("apiVersion: v1\n", encoding="utf-8")
    kubeconfig.chmod(0o600)

    def check(record):
        evidence = record["evidence"]
        if evidence["request"] != digest or evidence["version"] != version:
            raise entry_point.Failure("the completed evidence is not attributable to the request")
        if len(evidence["receipts"]) != objects:
            raise entry_point.Failure("the evidence does not hold a receipt per created object")
        # A receipt's basis and disposition say how its run proved it, so only
        # the objects it names and what was observed must hold across runs.
        return dict(evidence, receipts=receipts(evidence))

    replayed = entry_point.replay(
        entry,
        variables(str(kubeconfig), str(client), {}),
        check,
        recorded=lambda record: variables(str(kubeconfig), str(client), {"receipts": record["evidence"]["receipts"]}),
    )
    if {r["disposition"] for r in replayed["evidence"]["receipts"]} != {"unchanged"}:
        raise entry_point.Failure("the replay did not find every object unchanged")
    verbs = [call["verb"] for call in json.loads(Path(root, "cluster", "calls.json").read_text(encoding="utf-8"))]
    if verbs.count("create") != objects:
        raise entry_point.Failure("the runs created %d objects, want %d, all in the first run" % (verbs.count("create"), objects))
    return replayed
