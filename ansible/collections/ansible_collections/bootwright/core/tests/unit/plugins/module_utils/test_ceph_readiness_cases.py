"""The readiness facts the Ceph worker publishes for Go to judge, beside the verdict its own convergence wait reaches.

goldens/ceph_readiness_cases.json records, for each recorded health and PG case of the core tests, the native input, the facts the
worker publishes and the verdict its wait reaches; internal/storage/ceph's TestGoJudgesTheRecordedReadinessCasesAsTheWorkerDid feeds
the same facts to Go's predicate and requires the same verdict.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native

CASES = json.loads((Path(__file__).resolve().parents[2] / "goldens/ceph_readiness_cases.json").read_text(encoding="utf-8"))


class DetailOnly:
    """The worker state readiness_health reads: its request and one `ceph health detail` answer."""

    def __init__(self, request, detail):
        self.request, self.reading, self.blocking, self.detail = request, False, [], detail

    def ceph(self, *argv):
        assert argv == ("health", "detail") and self.detail is not None
        return self.detail


@pytest.mark.parametrize("case", CASES["health"], ids=lambda case: case["id"])
def test_the_worker_publishes_the_recorded_health_facts_beside_its_recorded_verdict(case):
    worker = DetailOnly(dict(case["request"], action=case["action"]), case["detail"])
    assert ceph_native.Worker.readiness_health(worker, case["health"]) == (case["status"], case["healthy"])
    assert {key: value for key, value in worker.health_facts.items() if key not in ("pgs", "license")} == case["facts"]
    assert set(worker.health_facts) == set(ceph_native.empty_readiness())


@pytest.mark.parametrize("case", CASES["pgs"], ids=lambda case: case["action"] + "-" + str(case["pgmap"]["num_pgs"]) + "-" + str(case["ready"]))
def test_the_worker_publishes_the_recorded_pg_counts_beside_its_recorded_verdict(case):
    status = {"pgmap": case["pgmap"]}
    assert ceph_native.placement_groups_ready(status, case["action"]) is case["ready"]
    assert ceph_native.placement_group_counts(status) == case["counts"]


def test_the_cases_cover_every_scoped_check_and_both_outcomes():
    assert {code for case in CASES["health"] for code in case["facts"]["scoped"]} == set(ceph_native.SCOPED_HEALTH)
    assert {case["healthy"] for case in CASES["health"]} == {True, False} and {case["ready"] for case in CASES["pgs"]} == {True, False}
