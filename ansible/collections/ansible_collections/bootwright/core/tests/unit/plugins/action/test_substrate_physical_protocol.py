"""A physical machine is proved, and an incomplete inventory proves nothing."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_physical_protocol

DIGEST = "c" * 64
DECLARED = ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"]
ENDPOINT = "https://bmc.example.test/redfish/v1/Systems/1"


def arguments(**overrides):
    observation = {
        "addresses": list(DECLARED),
        "failures": [],
        "manufacturer": "Acme",
        "model": "R740",
        "power": "Off",
        "serial": "SN1",
        "uuid": "uuid-1",
    }
    observation.update(overrides.pop("observation", {}))
    values = {"digest": DIGEST, "endpoint": ENDPOINT, "observation": observation, "expected": list(DECLARED)}
    values.update(overrides)
    return values


def test_the_declared_machine_is_proved_by_its_complete_inventory():
    evidence = substrate_physical_protocol.presence(arguments(), DIGEST)
    assert evidence["postcondition"]
    assert evidence["uuid"] == "uuid-1" and evidence["power"] == "Off"


# Extra hardware is not a mismatch: a server may carry more NICs than the
# declaration names, and every declared one being present is the proof.
def test_hardware_beyond_the_declaration_still_proves_it():
    evidence = substrate_physical_protocol.presence(
        arguments(observation={"addresses": DECLARED + ["aa:bb:cc:dd:ee:09"]}), DIGEST)
    assert evidence["postcondition"]


# An inventory that could not be read in full proves nothing about which
# machine this is, so data-loss authorization must never reach past it.
@pytest.mark.parametrize("observation,reason", [
    ({"failures": ["member[1] could not be read"]}, "a complete hardware inventory"),
    ({"addresses": ["aa:bb:cc:dd:ee:01"]}, "1 of 2 declared addresses"),
    ({"power": ""}, "a power state"),
    ({"uuid": "", "serial": ""}, "a system identity"),
])
def test_an_unproved_machine_names_what_is_missing(observation, reason):
    values = arguments(observation=observation)
    evidence = substrate_physical_protocol.presence(values, DIGEST)
    assert not evidence["postcondition"]
    assert reason in substrate_physical_protocol.unproved(evidence, values)


# A declaration with no address to compare against is never satisfied, because
# there is nothing for the inventory to prove.
def test_a_machine_with_no_declared_address_is_never_proved():
    values = arguments(expected=[])
    assert not substrate_physical_protocol.presence(values, DIGEST)["postcondition"]


# A removal reports nothing about the machine, because it took back only a
# claim and left everything else exactly as it was.
def test_a_removal_that_retains_the_machine_reports_nothing_about_it():
    evidence = substrate_physical_protocol.absence(DIGEST)
    assert evidence["absent"] and evidence["postcondition"]
    assert evidence["addresses"] == [] and evidence["uuid"] == "" and evidence["power"] == ""


@pytest.mark.parametrize("bad", ["short", "g" * 64, None])
def test_evidence_outside_its_own_grammar_is_refused(bad):
    with pytest.raises(ValueError):
        substrate_physical_protocol.presence(arguments(), bad)


def run(values):
    module = substrate_physical_protocol.ActionModule.__new__(substrate_physical_protocol.ActionModule)
    module._task = SimpleNamespace(args=values)
    return module.run(task_vars={})


def completion(**observation):
    return arguments(phase="completed", outcome="unchanged", observation=observation)


# A refusal of the reported identity names the controller the publication is
# handed, so a proof whose controller endpoint was lost publishes nothing,
# however printable its identity, rather than one day name no controller.
@pytest.mark.parametrize("endpoint", [None, "", "  "], ids=["absent", "empty", "blank"])
def test_a_proof_without_its_controller_endpoint_is_refused(endpoint, monkeypatch):
    published = []
    monkeypatch.setattr(substrate_physical_protocol, "emit", lambda message, **kwargs: published.append(message))
    values = completion()
    if endpoint is None:
        del values["endpoint"]
    else:
        values["endpoint"] = endpoint
    assert run(values) == {"failed": True, "msg": "the physical machine result could not be published"}
    assert not published


# A release reports nothing about the machine, so it names no controller.
def test_a_release_needs_no_controller_endpoint(monkeypatch):
    published = []
    monkeypatch.setattr(substrate_physical_protocol, "emit", lambda message, **kwargs: published.append(message))
    values = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "released": True}
    assert run(values) == {"changed": False}
    assert published == [{"phase": "completed", "outcome": "changed",
                          "evidence": substrate_physical_protocol.absence(DIGEST)}]


# Each character a reported identity may not hold: a C0 control, DEL, a C1
# control, and three Unicode format characters that print as nothing.
UNPRINTABLE = {
    "a C0 control": "\x1b",
    "DEL": "\x7f",
    "a C1 control": "\x9b",
    "a bidi override": "\u202e",
    "a zero-width space": "\u200b",
    "a byte-order mark": "\ufeff",
}
NOT_PRINTABLE = "holding a character that is not printable"
TOO_LONG = "longer than 128 characters"
REFUSED = {
    "a UUID of 129 characters": ("uuid", "u" * 129, "UUID", TOO_LONG),
    "a serial of 129 characters": ("serial", "s" * 129, "SerialNumber", TOO_LONG),
    "a serial of 129 two-byte letters": ("serial", "\xe9" * 129, "SerialNumber", TOO_LONG),
    "a tab inside a serial": ("serial", "SN\t1", "SerialNumber", NOT_PRINTABLE),
}
REFUSED.update({"a UUID holding " + name: ("uuid", "uuid-" + character + "1", "UUID", NOT_PRINTABLE)
                for name, character in UNPRINTABLE.items()})
REFUSED.update({"a serial holding " + name: ("serial", "SN" + character + "1", "SerialNumber", NOT_PRINTABLE)
                for name, character in UNPRINTABLE.items()})


# A reported UUID or serial is what a pin later carries into every comparison
# and refusal, so once its surrounding space is trimmed it must be printable
# and at most 128 characters. The refusal names the field and the controller,
# never the value, and nothing is published.
@pytest.mark.parametrize("field, value, reported, why", list(REFUSED.values()), ids=list(REFUSED))
def test_a_reported_identity_the_evidence_may_not_carry_is_refused(field, value, reported, why, monkeypatch):
    published = []
    monkeypatch.setattr(substrate_physical_protocol, "emit", lambda message, **kwargs: published.append(message))
    result = run(completion(**{field: value}))
    assert result == {"failed": True, "msg": "the management controller at %s reported a %s %s" % (ENDPOINT, reported, why)}
    assert value not in result["msg"]
    assert not published


ACCEPTED = {
    "a serial with inner spaces": ({"serial": "SN 12 34"}, {"serial": "SN 12 34"}),
    "128 characters each": ({"uuid": "u" * 128, "serial": "s" * 128}, {"uuid": "u" * 128, "serial": "s" * 128}),
    "128 two-byte letters": ({"serial": "\xe9" * 128}, {"serial": "\xe9" * 128}),
    "surrounding space": ({"uuid": " \t" + "u" * 128 + "\n ", "serial": " SN1\xa0"}, {"uuid": "u" * 128, "serial": "SN1"}),
    "a manufacturer and model keep only their bound": (
        {"manufacturer": "Acme\x1b", "model": "R740\u202e"}, {"manufacturer": "Acme\x1b", "model": "R740\u202e"}),
}


@pytest.mark.parametrize("observation, recorded", list(ACCEPTED.values()), ids=list(ACCEPTED))
def test_a_printable_identity_is_published_trimmed(observation, recorded, monkeypatch):
    published = []
    monkeypatch.setattr(substrate_physical_protocol, "emit", lambda message, **kwargs: published.append(message))
    assert run(completion(**observation)) == {"changed": False}
    evidence = published[0]["evidence"]
    assert evidence["postcondition"]
    assert {field: evidence[field] for field in recorded} == recorded
