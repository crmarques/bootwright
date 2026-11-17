"""What one read of an installed cluster resolves, for every answer oc gives it.

The install role reads the cluster back after its waits, before it settles,
and in every observation (cluster_state.yml). Each case below is what oc
answers for the cluster's ClusterVersion and nodes, and the state and
answered verdict it resolves to. The verdicts were pinned on the four oc reads
and the Jinja that classified them before the read moved into the
inspection's one ClusterVersion read and one nodes read (B1513); the missing
nodes, which that Jinja left in set order and every reader sorts (the
evidence's names() in the install protocol, the release gate's message), are
now sorted.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins.
"""

from __future__ import annotations

import json
import pathlib
import time

import pytest
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_cluster_read as reading
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect as inspection
from ansible_collections.bootwright.core.plugins.modules.containercluster_install_inspect import KEPT
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
INSPECT = "bootwright.core.containercluster_install_inspect"
IDENTITY = "a" * 64
RELEASE = "4.21.15"
NODES = ["master-0", "master-1", "master-2"]
REQUEST = {"release": {"version": RELEASE}, "nodes": [{"name": name} for name in NODES]}
UNKNOWN_AUTHORITY = ("Unable to connect to the server: tls: failed to verify certificate: "
                     "x509: certificate signed by unknown authority")
UNAUTHORIZED = "error: You must be logged in to the server (Unauthorized)"
FORBIDDEN = 'Error from server (Forbidden): clusterversions.config.openshift.io "version" is forbidden: User "x"'
TIMEOUT = "Unable to connect to the server: dial tcp 192.0.2.10:6443: i/o timeout"


def case(identity=IDENTITY, code=0, stderr="", available=("True",), history=(("Completed", RELEASE),),
         desired=RELEASE, nodes_code=0, nodes=tuple(NODES)):
    return {"identity": identity, "code": code, "stderr": stderr, "available": list(available),
            "history": None if history is None else [list(entry) for entry in history], "desired": desired,
            "nodes_code": nodes_code, "nodes": list(nodes)}


def state(cluster=IDENTITY, release=RELEASE, completed=True, missing=()):
    return {"cluster": cluster, "release": release, "completed": completed, "missing": list(missing)}


CASES = [
    ("whole", case(), state(), True),
    ("partial history", case(history=(("Partial", RELEASE),)), state(completed=False), True),
    ("another release completed", case(history=(("Completed", "4.21.14"),)), state(completed=False), True),
    ("newest entry partial", case(history=(("Partial", RELEASE), ("Completed", "4.21.14"))),
     state(completed=False), True),
    ("not available", case(available=("False",)), state(completed=False), True),
    ("no available condition", case(available=()), state(completed=False), True),
    ("absent history", case(history=None), state(completed=False), True),
    ("empty history", case(history=()), state(completed=False), False),
    ("another desired release", case(desired="4.22.0"), state(release="4.22.0"), True),
    ("no desired release", case(desired=None), state(release=""), True),
    ("no anchor", case(identity=""), state(cluster="foreign"), True),
    ("unknown authority", case(code=1, stderr=UNKNOWN_AUTHORITY),
     state(cluster="foreign", release="", completed=False), False),
    ("unauthorized", case(code=1, stderr=UNAUTHORIZED), state(cluster="foreign", release="", completed=False), False),
    ("forbidden", case(code=1, stderr=FORBIDDEN), state(cluster="foreign", release="", completed=False), False),
    ("unreachable", case(code=1, stderr=TIMEOUT), state(cluster="", release="", completed=False), False),
    ("unreachable without an anchor", case(identity="", code=1, stderr=TIMEOUT),
     state(cluster="", release="", completed=False), False),
    ("a node missing", case(nodes=("master-0", "master-2")), state(missing=["master-1"]), True),
    ("an undeclared node", case(nodes=NODES + ["worker-9"]), state(), True),
    ("no node", case(nodes=()), state(missing=NODES), True),
    ("nodes unread", case(nodes_code=1, nodes=()), state(missing=NODES), False),
    ("nothing answers", case(code=1, stderr=TIMEOUT, nodes_code=1, nodes=()),
     state(cluster="", release="", completed=False, missing=NODES), False),
]


def clusterversion(answer):
    """The ClusterVersion oc prints as JSON, holding what the case declares."""
    status = {"conditions": [{"type": "Available", "status": value} for value in answer["available"]]
              + [{"type": "Progressing", "status": "False"}]}
    if answer["desired"] is not None:
        status["desired"] = {"version": answer["desired"], "image": "quay.io/openshift-release-dev/ocp-release@sha256:" + "0" * 64}
    if answer["history"] is not None:
        status["history"] = [{"state": state_, "version": version} for state_, version in answer["history"]]
    return json.dumps({"apiVersion": "config.openshift.io/v1", "kind": "ClusterVersion",
                       "metadata": {"name": "version"}, "status": status})


def oc(answer, calls):
    """A runner answering the two reads as oc would for this case, recording each argv."""
    def run(client, kubeconfig, argv, deadline, body=None):
        calls.append((client, kubeconfig, argv, round(deadline - time.time())))
        assert body is None
        if argv[:3] == ["get", "clusterversion", "version"]:
            return answer["code"], clusterversion(answer) if answer["code"] == 0 else "", answer["stderr"]
        assert argv[:2] == ["get", "nodes"]
        return answer["nodes_code"], " ".join(answer["nodes"]) if answer["nodes_code"] == 0 else "", ""
    return run


def resolved(answer, monkeypatch):
    """The state cluster_state.yml resolves from the inspection's read, and whether every read was answered."""
    calls = []
    monkeypatch.setattr(reading.native, "run", oc(answer, calls))
    found, answered = reading.read("/usr/bin/oc", "/work/" + KEPT, REQUEST, answer["identity"])
    assert [call[2] for call in calls] == [["get", "clusterversion", "version", "--output", "json"],
                                           ["get", "nodes", "--output", "jsonpath={.items[*].metadata.name}"]]
    assert all(call[:2] == ("/usr/bin/oc", "/work/" + KEPT) and 0 < call[3] <= 31 for call in calls)
    registered = {"changed": False, "state": found, "answered": answered}
    templar = Templar(loader=LOADER, variables=dict(
        bootwright_cluster_install_request=REQUEST, containercluster_install_agent_cluster_read=registered))
    assert templar.template(resolve()["ansible.builtin.set_fact"]["containercluster_install_agent_state"]) == found
    return found, answered


def tasks():
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / "cluster_state.yml"), trusted_as_template=True)
            if isinstance(task, dict)]


def resolve():
    return [task for task in tasks() if "ansible.builtin.set_fact" in task][0]


@pytest.mark.parametrize("answer, expected, answered", [entry[1:] for entry in CASES], ids=[entry[0] for entry in CASES])
def test_each_answer_resolves_its_pinned_state(answer, expected, answered, monkeypatch):
    found, read = resolved(answer, monkeypatch)
    assert found == expected
    assert read is answered


# The read is one task through the kept kubeconfig, and a removal's
# observation, which reads none of the cluster, resolves every declared node
# missing and nothing answered, as a read that never ran proves.
def test_the_role_reads_once_through_the_inspection_and_a_removal_reads_nothing():
    read, resolving = tasks()
    assert read[INSPECT] == {
        "request": "{{ bootwright_cluster_install_request }}", "cluster": True,
        "client": "{{ containercluster_install_agent_client }}",
        "identity": '{{ containercluster_install_agent_before.observation.identity | default("") }}'}
    assert read["register"] == "containercluster_install_agent_cluster_read"
    assert read["when"] == "not (containercluster_install_agent_observes_removal | bool)"
    assert resolving == resolve()
    templar = Templar(loader=LOADER, variables=dict(
        bootwright_cluster_install_request=REQUEST,
        containercluster_install_agent_cluster_read={"changed": False, "skipped": True}))
    assert templar.template(resolve()["ansible.builtin.set_fact"]["containercluster_install_agent_state"]) == state(
        cluster="", release="", completed=False, missing=NODES)


def test_an_unexpected_clusterversion_answer_proves_nothing(monkeypatch):
    monkeypatch.setattr(reading.native, "run", lambda *arguments, **keywords: (0, "not json", ""))
    found, answered = reading.read("/usr/bin/oc", "/work/" + KEPT, REQUEST, IDENTITY)
    assert found["cluster"] == "" and found["completed"] is False and answered is False


def test_the_inspection_reads_through_the_kept_copy_alone(run_module, monkeypatch, tmp_path):
    calls = []
    monkeypatch.setattr(reading.native, "run", oc(case(), calls))
    request = dict(REQUEST, workRoot=str(tmp_path), image={"path": str(tmp_path / "served"), "url": "https://192.0.2.1:8443"})
    result = run_module(inspection, {"request": request, "cluster": True, "client": "/usr/bin/oc", "identity": IDENTITY})
    assert result["changed"] is False and result["state"] == state() and result["answered"] is True
    assert {call[:2] for call in calls} == {("/usr/bin/oc", str(tmp_path / KEPT))}
    refused = run_module(inspection, {"request": request, "cluster": True, "registered": True, "client": "/usr/bin/oc"})
    assert refused["failed"] is True and "exclusive" in refused["msg"]
