"""A completed cluster's nodes are placed once, by a read and then a pinned JSON patch.

NodeRestriction forbids a kubelet its ``node-role.kubernetes.io/`` labels and
every taint change
(https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#noderestriction),
so the administrator kubeconfig writes them. ``NodeSpec.Taints`` is
``+listType=atomic`` (k8s.io/api v0.36.2 core/v1/types.go), so an apply would
own the whole list; the patch adds entries instead and never removes one.
"""

from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_node_placement as placement

OC, KUBECONFIG = "/opt/clients/oc", "/var/lib/bootwright-clusters/ocp/.bootwright-kubeconfig"
INFRA = "node-role.kubernetes.io/infra"


def request_nodes():
    return [
        {"name": "master-0", "machine": "ocp-01"},
        {"name": "worker-0", "machine": "ocp-02", "labels": {"example.test/rack": "rack-a"}},
        {"name": "infra-0", "machine": "ocp-03", "labels": {INFRA: ""},
         "taints": [{"key": "example.test/reserved", "value": "storage", "effect": "NoSchedule"}]},
    ]


def node(name, labels, taints=None, uid=None):
    spec = {"podCIDR": "10.128.0.0/23"}
    if taints is not None:
        spec["taints"] = taints
    return {"metadata": {"name": name, "uid": uid or name + "-uid", "labels": labels}, "spec": spec}


class Cluster:
    """The nodes oc reads and patches, applying JSON patches as the API server does for the operations used here."""

    def __init__(self, nodes, refuse_patch=False, answer=True):
        self.nodes = {item["metadata"]["name"]: item for item in nodes}
        self.refuse_patch, self.answer = refuse_patch, answer
        self.calls = []

    def run(self, oc, kubeconfig, argv, deadline, body=None):
        assert (oc, kubeconfig) == (OC, KUBECONFIG) and body is None
        self.calls.append(argv)
        if argv[:2] == ["get", "nodes"]:
            if not self.answer:
                return 1, "", "error: the server is unavailable"
            return 0, json.dumps({"items": list(self.nodes.values())}), ""
        assert argv[:2] == ["patch", "node"] and "--field-manager" in argv
        assert argv[argv.index("--field-manager") + 1] == "bootwright-cluster-install" and "--type=json" in argv
        assert "--force-conflicts" not in argv and "--overwrite" not in argv
        if self.refuse_patch:
            return 1, "", 'The request is invalid: the server rejected our request due to an error in our request'
        target = self.nodes[argv[2]]
        for operation in json.loads(argv[argv.index("--patch") + 1]):
            parts = [part.replace("~1", "/").replace("~0", "~") for part in operation["path"].split("/")[1:]]
            parent = target
            for part in parts[:-1]:
                parent = parent[part]
            if operation["op"] == "test":
                assert parent[parts[-1]] == operation["value"]
            elif parts[-1] == "-":
                parent.append(operation["value"])
            else:
                parent[parts[-1]] = operation["value"]
        return 0, "node/" + argv[2], ""

    def patches(self):
        return [call for call in self.calls if call[0] == "patch"]


@pytest.fixture
def cluster(monkeypatch):
    def install(*nodes, **options):
        fake = Cluster(nodes, **options)
        monkeypatch.setattr(placement.native, "run", fake.run)
        return fake
    return install


def unplaced_cluster():
    return (node("master-0", {"kubernetes.io/hostname": "master-0"}),
            node("worker-0", {"kubernetes.io/hostname": "worker-0", "node-role.kubernetes.io/worker": ""}),
            node("infra-0", {"kubernetes.io/hostname": "infra-0", "node-role.kubernetes.io/worker": ""},
                 [{"key": "node.kubernetes.io/unschedulable", "effect": "NoSchedule"}]))


def test_only_nodes_that_declare_a_placement_are_placed():
    assert [item["name"] for item in placement.declared(request_nodes())] == ["worker-0", "infra-0"]


def test_placing_adds_what_each_node_lacks_and_reads_it_back(cluster):
    fake = cluster(*unplaced_cluster())
    result = placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    assert result["changed"] is True and result["unplaced"] == []
    assert result["placement"] == [
        {"name": "worker-0", "labels": {"example.test/rack": "rack-a"}, "taints": []},
        {"name": "infra-0", "labels": {INFRA: ""},
         "taints": [{"key": "example.test/reserved", "value": "storage", "effect": "NoSchedule"}]},
    ]
    infra = fake.nodes["infra-0"]
    assert infra["metadata"]["labels"] == {"kubernetes.io/hostname": "infra-0", "node-role.kubernetes.io/worker": "", INFRA: ""}
    assert infra["spec"]["taints"] == [{"key": "node.kubernetes.io/unschedulable", "effect": "NoSchedule"},
                                       {"key": "example.test/reserved", "value": "storage", "effect": "NoSchedule"}]
    assert [call[2] for call in fake.patches()] == ["worker-0", "infra-0"]
    assert fake.nodes["master-0"]["metadata"]["labels"] == {"kubernetes.io/hostname": "master-0"}


def test_every_patch_is_pinned_to_the_uid_labels_and_spec_it_read(cluster):
    fake = cluster(*unplaced_cluster())
    before = copy.deepcopy(fake.nodes["infra-0"])
    placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    operations = json.loads(fake.patches()[1][fake.patches()[1].index("--patch") + 1])
    tests = {operation["path"]: operation["value"] for operation in operations if operation["op"] == "test"}
    assert tests == {"/metadata/uid": "infra-0-uid", "/metadata/labels": before["metadata"]["labels"], "/spec": before["spec"]}
    assert {operation["op"] for operation in operations} == {"test", "add"}
    assert {"op": "add", "path": "/metadata/labels/node-role.kubernetes.io~1infra", "value": ""} in operations


def test_a_node_without_taints_receives_the_whole_list(cluster):
    fake = cluster(node("infra-0", {"kubernetes.io/hostname": "infra-0"}))
    nodes = [item for item in placement.declared(request_nodes()) if item["name"] == "infra-0"]
    placement.place(OC, KUBECONFIG, nodes)
    assert fake.nodes["infra-0"]["spec"]["taints"] == [{"key": "example.test/reserved", "value": "storage", "effect": "NoSchedule"}]


def test_a_continuation_after_a_lost_reply_reads_the_placement_back_and_writes_nothing(cluster):
    fake = cluster(*unplaced_cluster())
    placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    fake.calls = []
    result = placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    assert result["changed"] is False and result["unplaced"] == []
    assert fake.patches() == [] and [call[:2] for call in fake.calls] == [["get", "nodes"], ["get", "nodes"]]


@pytest.mark.parametrize("labels, taints, named", [
    ({"example.test/rack": "rack-b"}, None, "node worker-0 carries label example.test/rack with another value"),
    ({}, [{"key": "example.test/reserved", "value": "other", "effect": "NoSchedule"}],
     "node infra-0 carries taint example.test/reserved:NoSchedule with another value"),
])
def test_another_writers_value_refuses_before_any_node_is_written(cluster, labels, taints, named):
    nodes = list(unplaced_cluster())
    if labels:
        nodes[1]["metadata"]["labels"].update(labels)
    if taints:
        nodes[2]["spec"]["taints"] = taints
    fake = cluster(*nodes)
    with pytest.raises(placement.Refused) as refused:
        placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    assert named in str(refused.value) and "never overwritten" in str(refused.value)
    assert fake.patches() == []


def test_a_taint_of_a_declared_key_and_another_effect_is_left_beside_it(cluster):
    nodes = list(unplaced_cluster())
    nodes[2]["spec"]["taints"] = [{"key": "example.test/reserved", "effect": "NoExecute"}]
    fake = cluster(*nodes)
    result = placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    assert result["unplaced"] == []
    assert fake.nodes["infra-0"]["spec"]["taints"][0] == {"key": "example.test/reserved", "effect": "NoExecute"}


def test_a_node_changed_between_read_and_write_refuses_with_a_repeat(cluster):
    cluster(*unplaced_cluster(), refuse_patch=True)
    with pytest.raises(placement.Refused, match="changed since it was read.*repeat the apply"):
        placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))


def test_a_cluster_that_does_not_answer_is_neither_written_nor_placed(cluster):
    fake = cluster(*unplaced_cluster(), answer=False)
    with pytest.raises(placement.Refused, match="did not answer"):
        placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    assert fake.patches() == []
    observed = placement.observe(placement.declared(request_nodes()), placement.read(OC, KUBECONFIG))
    assert observed == {"answered": False, "placement": [], "unplaced": ["worker-0", "infra-0"]}


def test_an_observation_reports_a_removed_label_and_never_patches(cluster):
    fake = cluster(*unplaced_cluster())
    placement.place(OC, KUBECONFIG, placement.declared(request_nodes()))
    del fake.nodes["worker-0"]["metadata"]["labels"]["example.test/rack"]
    fake.calls = []
    observed = placement.observe(placement.declared(request_nodes()), placement.read(OC, KUBECONFIG))
    assert observed["unplaced"] == ["worker-0"]
    assert observed["placement"][0] == {"name": "worker-0", "labels": {}, "taints": []}
    assert fake.patches() == [] and "example.test/rack" not in fake.nodes["worker-0"]["metadata"]["labels"]


@pytest.mark.parametrize("nodes", [
    [{"name": "Bad_Name", "labels": {"a": "b"}}],
    [{"name": "worker-0", "taints": [{"key": "a", "effect": "Sometimes"}]}],
    [{"name": "worker-0", "labels": {"a": "b\n"}}],
])
def test_a_malformed_request_is_refused_before_any_read(nodes):
    with pytest.raises(ValueError):
        placement.declared(nodes)
