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
        "services": [{"enabled": True, "name": "virtnetworkd.service", "state": "active"}],
        "uri": True,
    }
    observation.update(overrides)
    return observation


def test_a_provider_host_postcondition_needs_every_proof():
    assert substrate_host_protocol.presence(host_observation(), DIGEST)["postcondition"]
    for overrides in (
        {"hypervisor": False},
        {"services": [{"enabled": True, "name": "virtnetworkd.service", "state": "failed"}]},
        {"services": [{"enabled": False, "name": "virtnetworkd.service", "state": "active"}]},
        {"uri": False},
        {"pool": ""},
        {"networks": [{"bridge": True, "managed": True, "name": "n", "owned": False, "state": "active", "uuid": "u"}]},
        {"networks": [{"bridge": True, "managed": True, "name": "n", "owned": True, "state": "inactive", "uuid": "u"}]},
        {"networks": [{"bridge": False, "managed": False, "name": "n", "owned": False, "state": "", "uuid": ""}]},
    ):
        assert not substrate_host_protocol.presence(host_observation(**overrides), DIGEST)["postcondition"]


def forgotten_network():
    return {"bridge": False, "managed": True, "name": "n", "owned": False, "state": "", "uuid": ""}


# `managed` echoes the request and stays true after removal, so an absence proof
# that read it alone could never be satisfied for any context owning a network.
def test_a_provider_host_removal_proves_the_hypervisor_forgot_the_network():
    removed = host_observation(pool="", networks=[forgotten_network()])
    assert substrate_host_protocol.absence(removed, DIGEST)["postcondition"]
    assert substrate_host_protocol.remaining(removed) == []


def test_a_provider_host_removal_names_what_is_still_defined():
    defined = host_observation(pool="")
    assert not substrate_host_protocol.absence(defined, DIGEST)["postcondition"]
    assert substrate_host_protocol.remaining(defined) == ["networks"]
    assert substrate_host_protocol.remaining(host_observation()) == ["pool", "networks"]


def test_an_unmanaged_network_never_blocks_a_removal():
    foreign = {"bridge": True, "managed": False, "name": "n", "owned": False, "state": "active", "uuid": ""}
    removed = host_observation(pool="", networks=[foreign])
    assert substrate_host_protocol.absence(removed, DIGEST)["postcondition"]


def test_an_unmet_provider_host_realization_names_what_is_unproved():
    complete = substrate_host_protocol.presence(host_observation(), DIGEST)
    assert substrate_host_protocol.unproved(complete) == []
    bare = substrate_host_protocol.presence(
        host_observation(
            hypervisor=False, uri=False, pool="", networks=[forgotten_network()],
            services=[{"enabled": False, "name": "virtnetworkd.service", "state": "failed"}],
        ),
        DIGEST,
    )
    assert substrate_host_protocol.unproved(bare) == ["hypervisor", "uri", "services", "pool", "networks"]


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
        "state": "shut off",
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


# A refused postcondition publishes no evidence, and `no_log` censors the result,
# so the field names are the only thing an operator can be told.
def test_an_unmet_machine_removal_names_what_is_still_there():
    assert substrate_machine_protocol.remaining({"domain": "", "unit": "", "controller": "", "disks": []}) == []
    assert substrate_machine_protocol.remaining(machine_observation()) == ["domain", "unit", "controller", "disks"]
    only_unit = {"domain": "", "unit": "inactive", "controller": "", "disks": [{"present": False}]}
    assert substrate_machine_protocol.remaining(only_unit) == ["unit"]


def test_an_unmet_machine_realization_names_what_is_unproved():
    complete = substrate_machine_protocol.presence(machine_observation(), "Off", "uuid", DIGEST)
    assert substrate_machine_protocol.unproved(complete) == []
    bare = substrate_machine_protocol.presence(
        machine_observation(domain="", owned=False, unit="failed", controller="", disks=[]), "Off", "uuid", DIGEST,
    )
    assert substrate_machine_protocol.unproved(bare) == [
        "domain", "ownership", "unit", "controller", "disks",
    ]


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


def test_an_unmet_installation_names_what_is_unproved():
    assert managedos_install_protocol.unproved(
        managedos_install_protocol.presence(install_arguments(), DIGEST)
    ) == []
    # The media staying inserted is the failure an operator most needs named,
    # because every other proof can hold while the eject silently did not.
    inserted = managedos_install_protocol.presence(
        install_arguments(media="http://s/install.iso"), DIGEST,
    )
    assert managedos_install_protocol.unproved(inserted) == ["media"]
    assert managedos_install_protocol.remaining({"observation": {"image": True, "tree": False}}) == ["image"]
    assert managedos_install_protocol.remaining({"observation": {"image": False, "tree": False}}) == []


def test_bounded_values_refuse_anything_oversized():
    with pytest.raises(ValueError):
        managedos_install_protocol.bounded("x" * (managedos_install_protocol.MAX_MARKER + 1), managedos_install_protocol.MAX_MARKER)


# A mutation that cannot prove its postcondition fails, because an unproved
# effect is never reported as success. An observation is the exception: the
# engine resolves an unknown effect from exactly the evidence an observation
# reports, and a target part way realized is what it most needs to see.
def test_only_an_observation_publishes_an_unmet_postcondition():
    for protocol in (substrate_host_protocol, substrate_machine_protocol, managedos_install_protocol):
        assert protocol.publishes({"postcondition": True}, False)
        assert protocol.publishes({"postcondition": True}, True)
        assert protocol.publishes({"postcondition": False}, True)
        assert not protocol.publishes({"postcondition": False}, False)
        assert not protocol.publishes({"postcondition": False}, None)


# The domain's own state is what a removal reads to prove the machine is not in
# use, so it is carried as evidence and never invented. A removal proving
# absence reports no state at all, because a domain that is gone has none.
def test_a_machine_carries_the_state_its_hypervisor_reports():
    evidence = substrate_machine_protocol.presence(machine_observation(), "Off", "uuid", DIGEST)
    assert evidence["state"] == "shut off"
    running = substrate_machine_protocol.presence(machine_observation(state="running"), "On", "uuid", DIGEST)
    assert running["state"] == "running"
    # A machine that is running is still completely realized, so the state is
    # evidence for the removal gate rather than another postcondition.
    assert running["postcondition"]
    assert substrate_machine_protocol.absence(machine_observation(), DIGEST)["state"] == ""
    with pytest.raises(ValueError):
        substrate_machine_protocol.presence(machine_observation(state="bananas"), "Off", "uuid", DIGEST)
