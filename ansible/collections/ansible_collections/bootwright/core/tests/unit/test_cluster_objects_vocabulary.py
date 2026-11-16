"""The engine runs every step and check kind of the program vocabulary Go freezes."""
from __future__ import annotations

import copy
import json
from pathlib import Path

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine

PROGRAM = json.loads((Path(__file__).resolve().parent / "goldens/cluster_objects_vocabulary.json").read_text(encoding="utf-8"))


def observed(program):
    request = {"program": program, "operation": "operation-1", "block": "block-1", "registered": "2026-01-01T00:00:00Z",
               "receipts": [], "deadline": 100}
    reads = []

    def read(obj, fields):
        reads.append(model.label(obj))
        return {"answered": True, "object": None}

    result = Engine(request, read, clock=lambda: 0).execute(observe=True)
    return result["state"], reads


def test_the_program_is_the_version_the_engine_runs():
    assert PROGRAM["version"] == model.VERSION


def test_the_engine_reaches_every_step_and_check_kind_the_vocabulary_names():
    state, reads = observed(copy.deepcopy(PROGRAM))
    waited = sum(len(step["wait"]) for step in PROGRAM["steps"] if step["kind"] == "wait")
    assert len(state["checks"]) == len(PROGRAM["checks"]) + waited
    assert {step["kind"] for step in PROGRAM["steps"]} == {"create", "place", "wait", "approveDependent"}
    assert {next(iter(check)) for check in PROGRAM["checks"]} == {"csvSucceeded", "condition", "resourceExists", "statusPhase"}
    assert any("ConfigMap" in read for read in reads) and any("Secret" in read for read in reads)
    assert any("Subscription" in read for read in reads)
    assert len(state["observations"]) >= 1 and state["dependents"] != []


def test_the_engine_refuses_a_step_kind_the_vocabulary_does_not_name():
    program = copy.deepcopy(PROGRAM)
    program["steps"].append({"kind": "notAStep"})
    try:
        observed(program)
    except ValueError as refusal:
        assert "unsupported cluster object step" in str(refusal)
    else:
        raise AssertionError("an unknown step kind ran")
