"""A power request is not evidence, so the postcondition rules are tested here."""

from __future__ import annotations

from types import SimpleNamespace

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


def ejecting(**overrides):
    values = arguments(verb="eject-media", power="On", previous="On", media="none", previous_media="foreign")
    values.update(overrides)
    return values


# An eject proves only a device read back presenting nothing, at whatever power
# the machine was; whose image it presented is never published.
def test_an_eject_proves_only_a_device_read_back_empty():
    evidence = machine_power_protocol.evidence_for(ejecting())
    assert evidence["postcondition"]
    assert evidence["media"] == "none" and evidence["previousMedia"] == "present"
    assert machine_power_protocol.evidence_for(ejecting(power="Off", previous="Off", previous_media="none"))["postcondition"]
    assert machine_power_protocol.evidence_for(ejecting(previous_media="own"))["previousMedia"] == "present"
    assert machine_power_protocol.evidence_for(ejecting(previous_media="unknown"))["previousMedia"] == "unknown"
    for unproved in ({"media": "foreign"}, {"media": "own"}, {"media": "unknown"}, {"power": ""}):
        assert not machine_power_protocol.evidence_for(ejecting(**unproved))["postcondition"], unproved


def test_an_unproved_eject_names_the_media_not_the_power():
    evidence, refusal = machine_power_protocol.completion(ejecting(media="foreign"))
    assert not evidence["postcondition"]
    assert refusal == "the management controller did not prove the virtual-media device presents nothing"
    assert machine_power_protocol.completion(ejecting())[1] is None


@pytest.mark.parametrize(
    "overrides",
    [
        {"media": "https://media.test/other.iso"},
        {"previous_media": ""},
        {"media": None},
    ],
)
def test_an_eject_media_value_outside_its_grammar_is_refused(overrides):
    with pytest.raises(ValueError):
        machine_power_protocol.evidence_for(ejecting(**overrides))


def test_a_power_verb_publishes_no_media():
    assert "media" not in machine_power_protocol.evidence_for(arguments())
    with pytest.raises(ValueError):
        machine_power_protocol.evidence_for(arguments(media="none"))


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


def test_a_media_read_publishes_only_normalized_device_state():
    survey = reads(
        read("node-a", media_state="foreign", media="https://media.test/image.iso?token=hidden"),
        read("node-b", media_state="none"),
        read("node-c", media_state="none", failed=True),
        read("node-d", media_state="unknown"),
        read("node-e"),
    )
    evidence = machine_power_protocol.reading_evidence_for(dict(survey, read_media=True))
    assert evidence == {"request": DIGEST, "machines": [
        {"machine": "node-a", "media": "present"},
        {"machine": "node-b", "media": "none"},
        {"machine": "node-c", "media": "unknown"},
        {"machine": "node-d", "media": "unknown"},
        {"machine": "node-e", "media": "unknown"},
    ]}


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


def run(values, monkeypatch):
    published = []
    monkeypatch.setattr(machine_power_protocol, "emit", lambda message, **kwargs: published.append(message))
    module = machine_power_protocol.ActionModule.__new__(machine_power_protocol.ActionModule)
    module._task = SimpleNamespace(args=values)
    return module.run(task_vars={}), published


# A power run names to its runner the one refusal the power service remedies
# by name, so the operator's diagnostic carries the Machine and the remedy.
def test_the_identity_refusal_is_named_to_the_runner(monkeypatch):
    result, published = run({"phase": "refused", "reason": "identity-mismatch"}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": "identity-mismatch"}]


# A reason the runner has no remedy for would break its protocol, so the task
# fails before it publishes anything.
@pytest.mark.parametrize("reason", [None, "", "release-stamp", "identity-mismatch "])
def test_a_refusal_the_power_service_does_not_name_is_never_published(reason, monkeypatch):
    values = {"phase": "refused"}
    if reason is not None:
        values["reason"] = reason
    result, published = run(values, monkeypatch)
    assert result == {"failed": True, "msg": "the machine power result could not be published"}
    assert not published
