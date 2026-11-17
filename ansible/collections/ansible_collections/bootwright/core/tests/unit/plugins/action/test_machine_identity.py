"""The one identity verdict machine power, the bare-metal target proof and the physical protocol refuse a wrong target from.

Where machine power's Jinja, the target proof's Jinja and presence() disagreed,
the verdict takes the refusing side; each such case is named below.
"""

from __future__ import annotations

import base64

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_physical_protocol
from ansible_collections.bootwright.core.plugins.filter.machine_identity_verdict import machine_identity_verdict
from ansible_collections.bootwright.core.plugins.module_utils import machine_identity

UUID = "4c4c4544-0042-3510-8052-b4c04f4d4e31"
SERIAL = "CZJ2440ABC"
HOST_UUID = "8e1b6a3c-0d7f-4c2a-9a51-3f0c2d4b5e61"
HOST_MACS = "aa:bb:cc:00:00:01,aa:bb:cc:00:00:02"
DECLARED = ["52:54:00:9a:1b:01", "52:54:00:9a:1b:02"]
DIGEST = "d" * 64


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def reported(uuid=UUID, serial=SERIAL, addresses=None, failures=None, **more):
    value = {"addresses": list(DECLARED) if addresses is None else addresses, "failures": [] if failures is None else failures,
             "manufacturer": "Dell Inc.", "model": "PowerEdge R740", "power": "Off", "serial": serial, "uuid": uuid}
    value.update(more)
    return value


PINNED = {
    "the same system": (reported(), UUID, SERIAL, True),
    "another uuid": (reported(uuid=UUID[:-1] + "2"), UUID, SERIAL, False),
    "another serial": (reported(serial="CZJ2440ABD"), UUID, SERIAL, False),
    "the uuid in other case and space": (reported(uuid=" " + UUID.upper() + "\n"), UUID, SERIAL, True),
    "a pin in upper case": (reported(), UUID.upper(), SERIAL, True),
    "the serial in surrounding space": (reported(serial=" " + SERIAL + " "), UUID, SERIAL, True),
    "the serial in other case": (reported(serial=SERIAL.lower()), UUID, SERIAL, False),
    "no pinned serial": (reported(serial="another"), UUID, "", True),
    "no pinned uuid": (reported(uuid="another"), "", SERIAL, True),
    "no pin": (reported(uuid="", serial=""), "", "", True),
    "a pinned uuid reported empty": (reported(uuid=""), UUID, SERIAL, False),
    "a pinned serial reported missing": ({"uuid": UUID}, UUID, SERIAL, False),
    "a template pin against its value": (reported(uuid="42", serial="42"), "{{ 6 * 7 }}", "{{ 6 * 7 }}", False),
    "a template pin against its text": (reported(uuid="{{ 6 * 7 }}", serial="{{ 6 * 7 }}"), "{{ 6 * 7 }}", "{{ 6 * 7 }}", True),
}


@pytest.mark.parametrize("observation, uuid, serial, same", PINNED.values(), ids=PINNED.keys())
def test_a_pin_holds_only_for_the_same_system(observation, uuid, serial, same):
    assert machine_identity.pinned(observation, encoded(uuid) if uuid else "", encoded(serial) if serial else "") is same


def test_a_pin_that_does_not_decode_never_matches():
    assert machine_identity.pinned(reported(), "not base64!", "") is False


HOST = {
    "a foreign machine": (reported(addresses=["52:54:00:9a:1b:01"]), False),
    "the host's uuid in other case and space": (reported(uuid=" " + HOST_UUID.upper()), True),
    "one of the host's MACs": (reported(addresses=["52:54:00:9a:1b:01", "aa:bb:cc:00:00:02"]), True),
}


@pytest.mark.parametrize("observation, host", HOST.values(), ids=HOST.keys())
def test_the_controller_host_is_recognized_by_its_uuid_or_any_mac(observation, host):
    assert machine_identity.controller_host(observation, encoded(HOST_UUID), encoded(HOST_MACS)) is host


def test_no_host_identity_recognizes_nothing_and_an_undecodable_one_refuses():
    assert machine_identity.controller_host(reported(), "", "") is False
    assert machine_identity.controller_host(reported(), "%%%", "") is True


DECLARED_CASES = {
    "exactly the declared set": (reported(addresses=list(reversed(DECLARED))), True),
    "a duplicated report of the declared set": (reported(addresses=DECLARED + DECLARED[:1]), True),
    "only a serial names the system": (reported(uuid=""), True),
    "an extra reported address": (reported(addresses=DECLARED + ["52:54:00:9a:1b:09"]), False),
    "a declared address missing": (reported(addresses=DECLARED[:1]), False),
    "an incomplete inventory": (reported(failures=["member[2] could not be read"]), False),
    "no system identity": (reported(uuid="", serial=""), False),
    "no observation": ({}, False),
    # Machine power read a missing failures list as unread and the target
    # proof failed on it, but presence() read it as complete: refused.
    "no failures list": ({key: value for key, value in reported().items() if key != "failures"}, False),
    # The target proof counted a blank identity as one; presence() trims it: refused.
    "only blank identities": (reported(uuid=" ", serial="\t"), False),
    # Machine power did not require an identity at all: refused.
    "an identity past its bound": (reported(uuid="U" * 129, serial=""), False),
    # Neither Jinja copy bounded identities; presence() refused them: refused.
    "an identity that is not printable": (reported(uuid=UUID + "\x1b", serial=""), False),
    # Neither Jinja copy bounded an address; presence() refused one past 32: refused.
    "an address past its bound": (reported(addresses=DECLARED + ["x" * 33]), False),
    # presence() trimmed an address, the Jinja copies compared it exactly: refused.
    "an address in surrounding space": (reported(addresses=[" " + DECLARED[0], DECLARED[1]]), False),
    "more addresses than the bound": (reported(addresses=["52:54:00:9a:%02x:%02x" % (n // 256, n % 256) for n in range(65)]), False),
}


@pytest.mark.parametrize("observation, proved", DECLARED_CASES.values(), ids=DECLARED_CASES.keys())
def test_a_complete_inventory_must_report_exactly_the_declared_set(observation, proved):
    assert machine_identity.declared(observation, DECLARED) is proved
    assert machine_identity_verdict(observation, expected=DECLARED)["declared"] is proved
    assert machine_identity_verdict(observation, expected_base64=encoded(",".join(DECLARED)))["declared"] is proved


def test_the_physical_protocol_publishes_its_proof_from_the_same_rule():
    for observation, proved in DECLARED_CASES.values():
        arguments = {"endpoint": "https://bmc.example.test", "expected": DECLARED, "observation": observation}
        try:
            evidence = substrate_physical_protocol.presence(arguments, DIGEST)
        except ValueError:
            assert not proved
            continue
        assert evidence["postcondition"] is (proved and bool(evidence["power"]))


@pytest.mark.parametrize("expected", [[], [""], None])
def test_no_declared_address_proves_nothing(expected):
    # Machine power accepted an empty decoded list (a value of only commas)
    # against an empty report; the target proof and presence() refused: refused.
    assert machine_identity.declared(reported(addresses=[]), expected) is False
    assert machine_identity_verdict(reported(addresses=[]), expected_base64=encoded(","))["declared"] is False


def test_a_declared_list_that_does_not_decode_proves_nothing():
    assert machine_identity_verdict(reported(), expected_base64="%%%")["declared"] is False


def test_the_verdict_names_every_rule():
    verdict = machine_identity_verdict(reported(), expected=DECLARED, pinned_uuid_base64=encoded(UUID),
                                       pinned_serial_base64=encoded(SERIAL), host_uuid_base64=encoded(HOST_UUID),
                                       host_macs_base64=encoded(HOST_MACS))
    assert verdict == {"pinned": True, "controllerHost": False, "declared": True}
