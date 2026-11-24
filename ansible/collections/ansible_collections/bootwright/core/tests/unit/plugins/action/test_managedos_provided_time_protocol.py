"""The provided time protocol only publishes proof of the declared result."""

import pytest

from ansible_collections.bootwright.core.tests.unit.plugins.action.test_every_action_runs import running

PLUGIN = "managedos_provided_time_protocol"
DIGEST = "c" * 64
SERVERS = ["192.0.2.123", "ntp.example.test"]


def arguments(state, changed=False, observed=False, answered=None):
    answer = {"ok": True, "state": state, "changed": changed, "servers": SERVERS if answered is None else answered,
              "postcondition": state == "complete"}
    return {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "servers": SERVERS, "observed": observed, "answer": answer}


@pytest.mark.parametrize("changed", [True, False])
def test_completion_publishes_the_proved_clock(changed, monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("complete", changed), monkeypatch)
    assert answer == {"changed": False}
    (record, acknowledged), = records
    assert not acknowledged
    assert record["evidence"] == {"version": "provided-os-time-evidence-v1", "request": DIGEST, "servers": SERVERS,
                                  "state": "complete", "postcondition": True, "changed": changed}


@pytest.mark.parametrize("state", ["absent", "partial"])
def test_observation_can_report_an_unproved_clock(state, monkeypatch):
    _module, answer, records = running(PLUGIN, arguments(state, observed=True), monkeypatch)
    assert answer == {"changed": False}
    assert records[0][0]["evidence"]["state"] == state and records[0][0]["evidence"]["postcondition"] is False


@pytest.mark.parametrize("invalid", [
    dict(arguments("complete", True), servers=["bad server"]),
    dict(arguments("complete", True), servers=[]),
    dict(arguments("complete", True), answer=None),
    arguments("complete", True, answered=["192.0.2.1"]),
    arguments("complete", 1),
    arguments("absent", True, observed=True),
    arguments("complete", True, observed=True),
    arguments("surprise"),
    dict(arguments("complete"), digest="short"),
    dict(arguments("complete"), answer=dict(arguments("complete")["answer"], postcondition=False)),
    dict(arguments("partial", observed=True), answer=dict(arguments("partial")["answer"], postcondition=True)),
])
def test_invalid_evidence_is_never_published(invalid, monkeypatch):
    _module, answer, records = running(PLUGIN, invalid, monkeypatch)
    assert answer["failed"] is True
    assert records == []


@pytest.mark.parametrize("state", ["absent", "partial"])
def test_a_mutation_never_publishes_a_clock_that_is_not_proved(state, monkeypatch):
    _module, answer, records = running(PLUGIN, arguments(state), monkeypatch)
    assert answer["failed"] is True and records == []


def test_a_refusal_is_published_only_by_its_closed_reason_and_detail(monkeypatch):
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "time-unproved", "detail": "192.0.2.123@sU0_192.0.2.124@sS377"}, monkeypatch)
    assert answer == {"changed": False} and records[0][0]["reason"] == "time-unproved"
    assert records[0][0]["detail"] == "192.0.2.123@sU0_192.0.2.124@sS377"
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "time-service-conflict", "detail": "ntpd has stopped"}, monkeypatch)
    assert answer == {"changed": False} and "detail" not in records[0][0]
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "update-failed", "detail": ""}, monkeypatch)
    assert answer["failed"] is True and records == []
