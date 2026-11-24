"""The provided registration protocol only publishes proof of the declared result."""

import pytest

from ansible_collections.bootwright.core.tests.unit.plugins.action.test_every_action_runs import running

PLUGIN = "managedos_provided_registration_protocol"
DIGEST = "c" * 64
CONSUMER = "11111111-2222-4333-8444-555555555555"


def arguments(state, postcondition=True, consumer=CONSUMER, observed=False):
    return {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "observed": observed,
            "answer": {"ok": True, "state": state, "consumer": consumer, "postcondition": postcondition}}


@pytest.mark.parametrize("state", ["converged", "unchanged"])
def test_completion_publishes_a_proved_registration(state, monkeypatch):
    _module, answer, records = running(PLUGIN, arguments(state), monkeypatch)
    assert answer == {"changed": False}
    (record, acknowledged), = records
    assert not acknowledged
    assert record["evidence"] == {"version": "provided-os-registration-evidence-v1", "request": DIGEST, "state": state,
                                  "consumer": CONSUMER, "postcondition": True}


def test_observation_can_prove_partial_or_absent_registration(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("diverged", False, observed=True), monkeypatch)
    assert answer == {"changed": False} and records[0][0]["evidence"]["postcondition"] is False
    _module, answer, records = running(PLUGIN, arguments("absent", False, consumer="", observed=True), monkeypatch)
    assert answer == {"changed": False} and records[0][0]["evidence"]["consumer"] == ""


def test_an_observation_of_an_outstanding_record_without_a_consumer_publishes_diverged(monkeypatch):
    _module, answer, records = running(PLUGIN, arguments("diverged", False, consumer="", observed=True), monkeypatch)
    assert answer == {"changed": False}
    assert records[0][0]["evidence"] == {"version": "provided-os-registration-evidence-v1", "request": DIGEST, "state": "diverged",
                                         "consumer": "", "postcondition": False}


def test_a_mutation_never_publishes_a_registration_that_is_not_proved(monkeypatch):
    for state, consumer in (("diverged", CONSUMER), ("absent", "")):
        _module, answer, records = running(PLUGIN, arguments(state, False, consumer=consumer), monkeypatch)
        assert answer["failed"] is True and records == []


@pytest.mark.parametrize("invalid", [
    dict(arguments("converged"), answer=None),
    dict(arguments("converged"), digest="short"),
    arguments("converged", consumer=""),
    arguments("converged", consumer="not-a-uuid"),
    arguments("converged", postcondition=False),
    arguments("diverged", True, observed=True),
    arguments("absent", False, consumer=CONSUMER, observed=True),
    arguments("unchanged", consumer=""),
    arguments("diverged", False, consumer="not-a-uuid", observed=True),
    arguments("surprise"),
    arguments("unchanged", observed=True),
    arguments("converged", postcondition=1),
])
def test_invalid_evidence_is_never_published(invalid, monkeypatch):
    _module, answer, records = running(PLUGIN, invalid, monkeypatch)
    assert answer["failed"] is True
    assert records == []


def test_a_refusal_is_published_only_by_its_closed_reason(monkeypatch):
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "registration-content-root", "detail": ""}, monkeypatch)
    assert answer == {"changed": False} and records[0][0]["reason"] == "registration-content-root"
    _module, answer, records = running(PLUGIN, {"phase": "refused", "reason": "registration-refused", "detail": ""}, monkeypatch)
    assert answer["failed"] is True and records == []
