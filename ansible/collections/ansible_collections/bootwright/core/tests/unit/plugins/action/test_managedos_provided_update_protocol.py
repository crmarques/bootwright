"""The provided update protocol only publishes proof of the declared result."""

import pytest

from ansible_collections.bootwright.core.tests.unit.plugins.action.test_every_action_runs import running

PLUGIN = "managedos_provided_update_protocol"
DIGEST = "c" * 64


def arguments(state, packages, rebooted, observed=False):
    return {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "release": "9.8",
            "observed": observed, "answer": {"ok": True, "state": state, "packages": packages, "rebooted": rebooted}}


def test_completion_requires_a_new_boot_when_packages_changed(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("complete", 3, True), monkeypatch)
    assert answer == {"changed": False}
    (record, acknowledged), = records
    assert not acknowledged
    assert record["evidence"] == {"version": "provided-os-update-evidence-v1", "request": DIGEST, "release": "9.8",
                                  "postcondition": True, "state": "complete", "packages": 3, "rebooted": True}


def test_observation_can_prove_partial_convergence(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("upgraded", 3, False, observed=True), monkeypatch)
    assert answer == {"changed": False}
    assert records[0][0]["evidence"]["postcondition"] is False


@pytest.mark.parametrize("invalid", [
    dict(arguments("complete", 3, True), release="9"),
    dict(arguments("complete", 3, True), answer=None),
    arguments("complete", True, True),
    arguments("upgraded", 3, True),
    arguments("upgraded", 3, False),
])
def test_mutation_never_publishes_invalid_or_incomplete_evidence(invalid, monkeypatch):
    _module, answer, records = running(PLUGIN, invalid, monkeypatch)
    assert answer["failed"] is True
    assert records == []
