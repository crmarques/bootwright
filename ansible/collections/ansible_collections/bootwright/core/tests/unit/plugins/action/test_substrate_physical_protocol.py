"""A physical machine is proved, and an incomplete inventory proves nothing."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_physical_protocol

DIGEST = "c" * 64
DECLARED = ["aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"]


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
    values = {"digest": DIGEST, "observation": observation, "expected": list(DECLARED)}
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
