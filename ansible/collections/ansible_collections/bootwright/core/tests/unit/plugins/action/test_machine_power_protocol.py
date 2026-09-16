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
