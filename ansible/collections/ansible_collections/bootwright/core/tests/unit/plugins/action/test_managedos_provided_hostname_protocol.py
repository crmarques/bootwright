"""The provided hostname protocol only publishes proof of the declared result."""

import pytest

from ansible_collections.bootwright.core.tests.unit.plugins.action.test_every_action_runs import running

PLUGIN = "managedos_provided_hostname_protocol"
DIGEST = "c" * 64
NAME = "witness.example.test"


def arguments(state, changed, observed=False, hostname=NAME):
    return {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "hostname": NAME, "observed": observed,
            "answer": {"ok": True, "state": state, "changed": changed, "hostname": hostname}}


def test_completion_publishes_the_proved_hostname(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("complete", True), monkeypatch)
    assert answer == {"changed": False}
    (record, acknowledged), = records
    assert not acknowledged
    assert record["evidence"] == {"version": "provided-os-hostname-evidence-v1", "request": DIGEST, "hostname": NAME,
                                  "state": "complete", "postcondition": True, "changed": True}


def test_observation_can_report_a_different_hostname(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("absent", False, observed=True), monkeypatch)
    assert answer == {"changed": False}
    assert records[0][0]["evidence"]["postcondition"] is False


@pytest.mark.parametrize("invalid", [
    dict(arguments("complete", True), hostname="-bad"),
    dict(arguments("complete", True), answer=None),
    arguments("complete", True, hostname="other.example.test"),
    arguments("complete", 1),
    arguments("absent", True),
    arguments("absent", False),
    arguments("complete", True, observed=True),
])
def test_mutation_never_publishes_invalid_or_incomplete_evidence(invalid, monkeypatch):
    _module, answer, records = running(PLUGIN, invalid, monkeypatch)
    assert answer["failed"] is True
    assert records == []


def test_a_refusal_names_only_a_closed_reason(monkeypatch):
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "hostname-write"}, monkeypatch)
    assert answer.get("failed") is not True and len(records) == 1
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "update-failed"}, monkeypatch)
    assert answer["failed"] is True and records == []
