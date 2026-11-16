"""The owner mark, field manager and creation-time skew are the values the Go engine freezes."""
from __future__ import annotations

from datetime import datetime, timedelta, timezone
import json
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import cluster_objects_model as model

VECTORS = json.loads((Path(__file__).resolve().parent / "goldens/cluster_objects_ownership.json").read_text(encoding="utf-8"))


@pytest.mark.parametrize("vector", VECTORS["vectors"], ids=lambda vector: vector["ref"]["kind"] + "-" + vector["operation"])
def test_the_owner_mark_and_field_manager_are_the_ones_go_freezes(vector):
    assert model.mark(vector["operation"], vector["block"], vector["ref"]) == vector["ownerMark"]
    assert model.field_manager(vector["operation"], vector["block"]) == vector["fieldManager"]


SKEW = VECTORS["clockSkewSeconds"]


@pytest.mark.parametrize("offset, expected", [(0, "own"), (-0.001, "own"), (SKEW, "own"), (SKEW + 0.001, "foreign")])
def test_an_object_created_within_the_clock_skew_before_registration_is_recovered(offset, expected):
    vector = VECTORS["vectors"][0]
    registered = datetime(2026, 1, 1, 0, 1, 0, tzinfo=timezone.utc)
    created = (registered - timedelta(seconds=offset)).isoformat().replace("+00:00", "Z")
    live = {"metadata": {"uid": "uid-1", "creationTimestamp": created, "annotations": {model.OWNER: vector["ownerMark"]}}}
    context = {"operation": vector["operation"], "block": vector["block"], "registered": registered.isoformat().replace("+00:00", "Z"), "receipts": []}
    assert model.ownership(vector["ref"], live, context) == expected
