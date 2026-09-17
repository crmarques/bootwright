"""A power request is not evidence, so the postcondition rules are tested here."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.action import machine_power_protocol

DIGEST = "b" * 64


def arguments(**overrides):
    values = {
        "digest": DIGEST,
        "machine": "node-a",
        "verb": "start",
        "power": "On",
        "previous": "Off",
        "changed": True,
    }
    values.update(overrides)
    return values


def test_each_verb_proves_only_the_state_it_converges_to():
    assert machine_power_protocol.evidence_for(arguments())["postcondition"]
    assert machine_power_protocol.evidence_for(arguments(verb="stop", power="Off", previous="On"))["postcondition"]
    assert machine_power_protocol.evidence_for(arguments(verb="restart", power="On", previous="On"))["postcondition"]
    assert not machine_power_protocol.evidence_for(arguments(power="Off"))["postcondition"]
    assert not machine_power_protocol.evidence_for(arguments(verb="stop", power="On"))["postcondition"]
    assert not machine_power_protocol.evidence_for(arguments(verb="restart", power="Off", previous="On"))["postcondition"]


# A controller that never answered reports nothing. That is not the state the
# operation asked for, so it can never satisfy a postcondition.
def test_an_unanswered_controller_proves_nothing():
    assert not machine_power_protocol.evidence_for(arguments(power=""))["postcondition"]
    assert machine_power_protocol.evidence_for(arguments(previous=""))["previous"] == ""


def test_the_published_state_is_the_reported_one_translated_exactly():
    evidence = machine_power_protocol.evidence_for(arguments(verb="stop", power="Off", previous="On"))
    assert evidence["power"] == "off"
    assert evidence["previous"] == "on"
    assert evidence["machine"] == "node-a"
    assert evidence["request"] == DIGEST


@pytest.mark.parametrize(
    "overrides",
    [
        {"digest": "short"},
        {"digest": "g" * 64},
        {"machine": ""},
        {"verb": "power-on"},
        {"power": "Paused"},
        {"previous": "Unknown"},
    ],
)
def test_an_evidence_value_outside_its_own_grammar_is_refused(overrides):
    with pytest.raises(ValueError):
        machine_power_protocol.evidence_for(arguments(**overrides))


def reads(*results):
    return {"digest": DIGEST, "readings": list(results)}


def read(name, **overrides):
    result = {"item": {"object": name}, "power": "On"}
    result.update(overrides)
    return result


# A reading reports what each controller said, under the machine its own survey
# entry named, in the order the survey named them.
def test_a_reading_answers_for_every_machine_its_survey_named():
    evidence = machine_power_protocol.reading_evidence_for(
        reads(read("node-a"), read("node-b", power="Off"))
    )
    assert evidence["machines"] == [
        {"machine": "node-a", "power": "on"},
        {"machine": "node-b", "power": "off"},
    ]
    assert evidence["request"] == DIGEST


# One controller that refused, timed out or answered with something this build
# cannot name leaves its own machine unknown. It never denies the answers the
# same run already has, and its silence is never published as a state. The
# refusal a reading actually sees is the last case: suppressing the task
# failure to keep polling rewrites `failed` to false, so what arrives is a
# result carrying no usable state.
@pytest.mark.parametrize(
    "overrides",
    [
        {"failed": True, "power": ""},
        {"unreachable": True, "power": ""},
        {"failed": True, "power": "On"},
        {"power": "Paused"},
        {"power": ""},
    ],
)
def test_a_controller_that_did_not_answer_leaves_its_machine_unknown(overrides):
    evidence = machine_power_protocol.reading_evidence_for(
        reads(read("node-a"), read("node-b", **overrides))
    )
    assert evidence["machines"] == [
        {"machine": "node-a", "power": "on"},
        {"machine": "node-b", "power": "unknown"},
    ]


# A suppressed module failure returns only its message, so the result a reading
# reads for that machine carries no power key at all.
def test_a_refusal_that_reports_no_state_at_all_is_unknown():
    refused = {"item": {"object": "node-b"}, "msg": "the controller did not answer"}
    evidence = machine_power_protocol.reading_evidence_for(reads(read("node-a"), refused))
    assert evidence["machines"] == [
        {"machine": "node-a", "power": "on"},
        {"machine": "node-b", "power": "unknown"},
    ]


@pytest.mark.parametrize(
    "published",
    [
        {"digest": DIGEST, "readings": []},
        {"digest": DIGEST, "readings": "node-a"},
        {"digest": DIGEST},
        {"digest": DIGEST, "readings": [{"power": "On"}]},
        {"digest": DIGEST, "readings": [{"item": {"object": ""}, "power": "On"}]},
        {"digest": DIGEST, "readings": ["node-a"]},
        {"digest": "short", "readings": [{"item": {"object": "node-a"}, "power": "On"}]},
    ],
)
def test_a_reading_outside_its_own_grammar_is_refused(published):
    with pytest.raises(ValueError):
        machine_power_protocol.reading_evidence_for(published)
