"""The containercluster_install_placement module's main() runs against a stubbed oc, as a task would run it."""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_node_placement as placement
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_placement as module

NODES = [{"name": "worker-0", "labels": {"example.com/pool": "gpu"}, "taints": [{"key": "example.com/gpu", "value": "true", "effect": "NoSchedule"}]},
         {"name": "worker-1"}]


@pytest.fixture(name="cluster")
def stubbed_oc(monkeypatch):
    """The oc executable answering a node list and recording each patch; `answers` False makes the API silent."""
    state = {"labels": {"worker-0": None}, "answers": True, "patches": []}

    def run(oc, kubeconfig, argv, deadline):
        assert oc == "/usr/local/bin/oc" and kubeconfig == "/run/lent/kubeconfig"
        if argv[:2] == ["get", "nodes"]:
            if not state["answers"]:
                return 1, "", "connection refused"
            items = [{"metadata": {"name": name, "uid": "uid-" + name, "labels": labels}, "spec": {}} for name, labels in state["labels"].items()]
            return 0, json.dumps({"items": items}), ""
        state["patches"].append(argv)
        state["labels"][argv[2]] = {"example.com/pool": "gpu"}
        return 0, argv[2], ""

    monkeypatch.setattr(placement.native, "run", run)
    return state


def arguments(operation):
    return {"client": "/usr/local/bin/oc", "kubeconfig": "/run/lent/kubeconfig", "nodes": NODES, "operation": operation}


def test_observe_reports_the_declared_nodes_short_of_their_placement_and_patches_nothing(cluster, run_module):
    result = run_module(module, arguments("observe"))
    assert result["changed"] is False and result["answered"] is True
    assert result["unplaced"] == ["worker-0"]
    assert result["placement"] == [{"name": "worker-0", "labels": {}, "taints": []}]
    assert cluster["patches"] == []


def test_place_adds_what_a_node_lacks_once_and_reads_the_placement_back(cluster, run_module):
    placed = run_module(module, arguments("place"))
    assert placed["changed"] is True and placed["answered"] is True
    assert placed["placement"][0]["labels"] == {"example.com/pool": "gpu"}
    assert [patch[2] for patch in cluster["patches"]] == ["worker-0"]
    assert "--field-manager" in cluster["patches"][0]


def test_a_value_another_manager_set_is_refused_without_a_patch_and_a_silent_api_is_not_an_answer(cluster, run_module):
    cluster["labels"]["worker-0"] = {"example.com/pool": "cpu"}
    refused = run_module(module, arguments("place"))
    assert refused["changed"] is False and "carries label example.com/pool with another value" in refused["refused"]
    assert cluster["patches"] == []
    cluster["answers"] = False
    silent = run_module(module, arguments("observe"))
    assert silent["answered"] is False and silent["unplaced"] == ["worker-0"]


def test_a_malformed_declaration_fails_the_task_before_oc_runs(cluster, run_module):
    result = run_module(module, dict(arguments("observe"), nodes=[{"name": "Not A Name", "labels": {"a": "b"}}]))
    assert result["failed"] is True and result["msg"] == "the node placement request is malformed"
