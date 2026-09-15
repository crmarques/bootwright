"""Evidence is shaped here, so the postcondition rules are tested directly."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.action import (
    managedos_install_protocol,
    substrate_host_protocol,
    substrate_machine_protocol,
)

DIGEST = "a" * 64


def host_observation(**overrides):
    observation = {
        "hypervisor": True,
        "networks": [{"bridge": True, "managed": True, "name": "n", "owned": True, "state": "active", "uuid": "u"}],
        "pool": "active",
        "service": "active",
        "uri": True,
    }
    observation.update(overrides)
    return observation


def test_a_provider_host_postcondition_needs_every_proof():
    assert substrate_host_protocol.presence(host_observation(), DIGEST)["postcondition"]
    for overrides in (
        {"hypervisor": False},
        {"service": "failed"},
        {"uri": False},
        {"pool": ""},
        {"networks": [{"bridge": True, "managed": True, "name": "n", "owned": False, "state": "active", "uuid": "u"}]},
        {"networks": [{"bridge": True, "managed": True, "name": "n", "owned": True, "state": "inactive", "uuid": "u"}]},
        {"networks": [{"bridge": False, "managed": False, "name": "n", "owned": False, "state": "", "uuid": ""}]},
    ):
        assert not substrate_host_protocol.presence(host_observation(**overrides), DIGEST)["postcondition"]


def test_a_provider_host_removal_proves_only_what_it_owns():
    gone = substrate_host_protocol.absence(host_observation(pool="", networks=[]), DIGEST)
    assert gone["postcondition"] and gone["absent"]
    remaining = substrate_host_protocol.absence(host_observation(pool=""), DIGEST)
    assert not remaining["postcondition"]
    # An external bridge is never this block's to remove, so it does not keep
    # a removal from completing.
    external = substrate_host_protocol.absence(
        host_observation(pool="", networks=[{"bridge": True, "managed": False, "name": "n", "owned": False, "state": "", "uuid": ""}]),
        DIGEST,
    )
    assert external["postcondition"]


def test_a_networks_identity_is_observed_but_never_reported_as_evidence():
    """Go rejects an evidence field it does not know, so the UUID stays here."""
    evidence = substrate_host_protocol.presence(host_observation(), DIGEST)
    assert set(evidence["networks"][0]) == {"bridge", "managed", "name", "owned", "state"}
    # An observation that does not carry the identity is refused rather than
    # silently shaped into evidence, because a definition without it collides.
    with pytest.raises(ValueError):
        substrate_host_protocol.presence(
            host_observation(networks=[{"bridge": True, "managed": True, "name": "n", "owned": True, "state": "active"}]),
            DIGEST,
        )


def machine_observation(**overrides):
    observation = {
        "controller": "quay.io/x@sha256:" + "0" * 64,
        "disks": [{"name": "root", "present": True, "sizeGiB": 60}],
        "domain": "bootwright-lab-rhel-01",
        "owned": True,
        "unit": "active",
    }
    observation.update(overrides)
    return observation


def test_a_machine_postcondition_needs_every_proof():
    assert substrate_machine_protocol.presence(machine_observation(), "Off", "uuid", DIGEST)["postcondition"]
    for observation, power, system in (
        (machine_observation(domain=""), "Off", "uuid"),
        (machine_observation(owned=False), "Off", "uuid"),
        (machine_observation(unit="failed"), "Off", "uuid"),
        (machine_observation(controller=""), "Off", "uuid"),
        (machine_observation(disks=[{"name": "root", "present": False, "sizeGiB": 0}]), "Off", "uuid"),
        (machine_observation(disks=[]), "Off", "uuid"),
        (machine_observation(), "", "uuid"),
        (machine_observation(), "Off", ""),
    ):
        assert not substrate_machine_protocol.presence(observation, power, system, DIGEST)["postcondition"]


def test_a_power_state_the_controller_never_reported_is_refused():
    with pytest.raises(ValueError):
        substrate_machine_protocol.presence(machine_observation(), "Spinning", "uuid", DIGEST)


def test_evidence_must_name_a_well_formed_digest():
    for value in ("", "abc", "z" * 64, None):
        with pytest.raises(ValueError):
            substrate_host_protocol.digest(value)


def install_arguments(**overrides):
    arguments = {
        "observation": {"image": True, "tree": True},
        "marker": '{"context":"lab"}',
        "hostKey": "ssh-ed25519 AAAA",
        "address": "198.51.100.11",
        "media": "",
        "power": "On",
    }
    arguments.update(overrides)
    return arguments


def test_an_installation_postcondition_needs_every_proof():
    assert managedos_install_protocol.presence(install_arguments(), DIGEST)["postcondition"]
    for overrides in (
        {"marker": ""},
        {"hostKey": ""},
        {"address": ""},
        {"media": "http://s/install.iso"},
        {"power": "Off"},
        {"observation": {"image": False, "tree": True}},
    ):
        assert not managedos_install_protocol.presence(install_arguments(**overrides), DIGEST)["postcondition"]


def test_an_installation_removal_proves_the_published_content_is_gone():
    gone = managedos_install_protocol.absence({"observation": {"image": False, "tree": False}}, DIGEST)
    assert gone["postcondition"] and gone["absent"]
    remaining = managedos_install_protocol.absence({"observation": {"image": True, "tree": False}}, DIGEST)
    assert not remaining["postcondition"]


def test_bounded_values_refuse_anything_oversized():
    with pytest.raises(ValueError):
        managedos_install_protocol.bounded("x" * (managedos_install_protocol.MAX_MARKER + 1), managedos_install_protocol.MAX_MARKER)
