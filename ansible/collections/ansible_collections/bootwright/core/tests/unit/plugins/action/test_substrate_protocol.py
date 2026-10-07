"""Evidence is shaped here, so the postcondition rules are tested directly."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import (
    managedos_install_protocol,
    substrate_host_protocol,
    substrate_machine_protocol,
)
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import observe_host

DIGEST = "a" * 64

# One managed network the host carries as frozen: answered for, owned, active,
# set to autostart and running, as it keeps for its next start, the definition
# its entry sets.
CARRIED = {"answered": True, "autostart": True, "bridge": True, "definition": True, "managed": True, "name": "n", "owned": True, "state": "active", "uuid": "u"}


def host_observation(**overrides):
    observation = {
        "directory": True,
        "hypervisor": True,
        "networks": [dict(CARRIED)],
        "pool": "active",
        "poolAnswered": True,
        "poolAutostart": True,
        "poolOwned": True,
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
        {"poolAnswered": False},
        {"poolAutostart": False},
        {"poolOwned": False},
        {"networks": [dict(CARRIED, autostart=False)]},
        {"networks": [dict(CARRIED, owned=False)]},
        {"networks": [dict(CARRIED, state="inactive")]},
        {"networks": [dict(CARRIED, answered=False)]},
        {"networks": [dict(CARRIED, definition=False)]},
        {"networks": [{"answered": False, "autostart": False, "bridge": False, "definition": False, "managed": False, "name": "n", "owned": False,
                       "state": "", "uuid": ""}]},
    ):
        assert not substrate_host_protocol.presence(host_observation(**overrides), DIGEST)["postcondition"]


def forgotten_network():
    return {
        "answered": True, "autostart": False, "bridge": False, "definition": False, "managed": True, "name": "n", "owned": False,
        "state": "", "uuid": "",
    }


# `managed` echoes the request and stays true after removal, so an absence proof
# that read it alone could never be satisfied for any context owning a network.
def test_a_provider_host_removal_proves_the_hypervisor_forgot_the_network():
    removed = host_observation(pool="", networks=[forgotten_network()], directory=False)
    assert substrate_host_protocol.absence(removed, DIGEST)["postcondition"]
    assert substrate_host_protocol.remaining(removed) == []


def test_a_provider_host_removal_names_what_is_still_defined():
    defined = host_observation(pool="", directory=False)
    assert not substrate_host_protocol.absence(defined, DIGEST)["postcondition"]
    assert substrate_host_protocol.remaining(defined) == ["networks"]
    assert substrate_host_protocol.remaining(host_observation()) == ["pool", "networks", "directory"]


def test_an_unmanaged_network_never_blocks_a_removal():
    foreign = {"answered": False, "autostart": False, "bridge": True, "definition": False, "managed": False, "name": "n", "owned": False,
               "state": "active", "uuid": ""}
    removed = host_observation(pool="", networks=[foreign], directory=False)
    assert substrate_host_protocol.absence(removed, DIGEST)["postcondition"]


def test_an_unmet_provider_host_realization_names_what_is_unproved():
    complete = substrate_host_protocol.presence(host_observation(), DIGEST)
    assert substrate_host_protocol.unproved(complete) == []
    for overrides, unmet in (
        ({"poolAutostart": False}, ["pool"]),
        ({"poolOwned": False}, ["pool"]),
        ({"networks": [dict(CARRIED, autostart=False)]}, ["networks"]),
    ):
        assert substrate_host_protocol.unproved(substrate_host_protocol.presence(host_observation(**overrides), DIGEST)) == unmet
    bare = substrate_host_protocol.presence(
        host_observation(
            hypervisor=False, uri=False, pool="", networks=[forgotten_network()],
            services=[{"enabled": False, "name": "virtnetworkd.service", "state": "failed"}],
        ),
        DIGEST,
    )
    assert substrate_host_protocol.unproved(bare) == ["hypervisor", "uri", "services", "pool", "networks"]


def test_a_provider_host_removal_proves_only_what_it_owns():
    gone = substrate_host_protocol.absence(host_observation(pool="", networks=[], directory=False), DIGEST)
    assert gone["postcondition"] and gone["absent"]
    remaining = substrate_host_protocol.absence(host_observation(pool="", directory=False), DIGEST)
    assert not remaining["postcondition"]
    # An external bridge is never this block's to remove, so it does not keep
    # a removal from completing.
    external = substrate_host_protocol.absence(
        host_observation(
            pool="", directory=False,
            networks=[{"answered": False, "autostart": False, "bridge": True, "definition": False, "managed": False, "name": "n",
                       "owned": False, "state": "", "uuid": ""}],
        ),
        DIGEST,
    )
    assert external["postcondition"]


def test_a_networks_identity_is_observed_but_never_reported_as_evidence():
    """Go rejects an evidence field it does not know, so the UUID stays here."""
    evidence = substrate_host_protocol.presence(host_observation(), DIGEST)
    assert set(evidence["networks"][0]) == {"answered", "autostart", "bridge", "definition", "managed", "name", "owned", "state"}
    # An observation that does not carry the identity is refused rather than
    # silently shaped into evidence, because a definition without it collides.
    with pytest.raises(ValueError):
        substrate_host_protocol.presence(
            host_observation(networks=[{key: value for key, value in CARRIED.items() if key != "uuid"}]),
            DIGEST,
        )


def machine_observation(**overrides):
    observation = {
        "answered": True,
        "controller": "quay.io/x@sha256:" + "0" * 64,
        "disks": [{"name": "root", "present": True, "sizeGiB": 60}],
        "domain": "bootwright-lab-rhel-01",
        "listener": True,
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


# A refused postcondition publishes no evidence, and its message names fields,
# never values. The completion runs under `no_log`, and the adapter's output
# callback prints nothing a hidden task raised (plugins/callback/censored.py),
# so the message reaches no output, and its names keep it safe should it be
# printed.
def test_an_unmet_machine_removal_names_what_is_still_there():
    assert substrate_machine_protocol.remaining({"domain": "", "unit": "", "controller": "", "disks": []}) == []
    assert substrate_machine_protocol.remaining(machine_observation()) == ["domain", "unit", "controller", "listener", "disks"]
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
        "reachable": True,
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
        # An installed machine nobody can log in to is not a finished
        # installation, and the observation that resolves an interrupted apply
        # proves it the same way the apply does.
        {"reachable": False},
    ):
        assert not managedos_install_protocol.presence(install_arguments(**overrides), DIGEST)["postcondition"]


def test_an_installation_removal_proves_the_published_content_is_gone():
    gone = managedos_install_protocol.absence({"observation": {"image": False, "tree": False}}, DIGEST)
    assert gone["postcondition"] and gone["absent"]
    remaining = managedos_install_protocol.absence({"observation": {"image": True, "tree": False}}, DIGEST)
    assert not remaining["postcondition"]


# A removal stopped while it deleted the package tree can leave the directory
# without its .treeinfo. That is content the removal still takes back, so it is
# carried as evidence, keeps the removal's absence unproved and is named.
def test_a_tree_left_without_its_marker_is_content_a_removal_still_takes_back():
    left = {"observation": {"image": False, "tree": False, "treeContent": True}}
    assert managedos_install_protocol.presence(dict(install_arguments(), **left), DIGEST)["treeContent"] is True
    evidence = managedos_install_protocol.absence(left, DIGEST)
    assert not evidence["postcondition"]
    assert evidence["treeContent"] is False
    assert managedos_install_protocol.remaining(left) == ["treeContent"]
    assert managedos_install_protocol.presence(install_arguments(), DIGEST)["treeContent"] is False


# An apply killed part way leaves the staging tree beneath the served root and
# its work area. Each is carried as evidence, keeps a removal's absence
# unproved and is named, so a removal that did not take one back fails.
@pytest.mark.parametrize("remnant", ["treeStaging", "work"])
def test_what_a_killed_apply_leaves_is_content_a_removal_still_takes_back(remnant):
    left = {"observation": {"image": False, "tree": False, remnant: True}}
    assert managedos_install_protocol.presence(dict(install_arguments(), **left), DIGEST)[remnant] is True
    assert managedos_install_protocol.presence(install_arguments(), DIGEST)[remnant] is False
    evidence = managedos_install_protocol.absence(left, DIGEST)
    assert not evidence["postcondition"]
    assert evidence[remnant] is False
    assert managedos_install_protocol.remaining(left) == [remnant]
    assert managedos_install_protocol.absence({"observation": {remnant: False}}, DIGEST)["postcondition"]


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
# absence reports no state at all, because a domain that is gone has none, and
# one that still reports a state is not gone.
def test_a_machine_carries_the_state_its_hypervisor_reports():
    evidence = substrate_machine_protocol.presence(machine_observation(), "Off", "uuid", DIGEST)
    assert evidence["state"] == "shut off"
    running = substrate_machine_protocol.presence(machine_observation(state="running"), "On", "uuid", DIGEST)
    assert running["state"] == "running"
    # A machine that is running is still completely realized, so the state is
    # evidence for the removal gate rather than another postcondition.
    assert running["postcondition"]
    assert substrate_machine_protocol.absence(GONE, None, None, DIGEST)["state"] == ""
    stated = substrate_machine_protocol.absence(dict(GONE, state="shut off"), None, None, DIGEST)
    assert (stated["state"], stated["postcondition"]) == ("shut off", False)
    with pytest.raises(ValueError):
        substrate_machine_protocol.presence(machine_observation(state="bananas"), "Off", "uuid", DIGEST)


GONE = {"answered": True, "controller": "", "disks": [], "domain": "", "listener": False, "owned": False, "state": "", "unit": ""}


# A silent hypervisor reports no domain either, so whether it answered travels
# with the evidence, and only an answer proves the domain gone.
def test_a_machine_removal_is_proved_only_when_the_hypervisor_answered():
    removed = substrate_machine_protocol.absence(GONE, None, None, DIGEST)
    assert removed["answered"] is True
    assert removed["postcondition"] is True
    silent = substrate_machine_protocol.absence(dict(GONE, answered=False), None, None, DIGEST)
    assert silent["answered"] is False
    assert silent["postcondition"] is False
    unreported = {key: value for key, value in GONE.items() if key != "answered"}
    assert substrate_machine_protocol.absence(unreported, None, None, DIGEST)["postcondition"] is False


# Removal evidence reports what the removal observed, so a leftover disk or a
# socket something still listens on reaches the engine, and the refusal names it.
def test_a_machine_absence_publishes_what_it_observed():
    leftover = dict(GONE, disks=[{"name": "root", "present": True, "sizeGiB": 60}], listener=True)
    evidence = substrate_machine_protocol.absence(leftover, None, None, DIGEST)
    assert evidence["disks"] == [{"name": "root", "present": True, "sizeGiB": 60}]
    assert evidence["listener"] is True
    assert evidence["postcondition"] is False
    assert substrate_machine_protocol.remaining(leftover) == ["listener", "disks"]
    whole = substrate_machine_protocol.absence(machine_observation(), "Off", "uuid", DIGEST)
    assert {name: whole[name] for name in ("controller", "domain", "owned", "power", "state", "system", "unit")} == {
        "controller": "quay.io/x@sha256:" + "0" * 64, "domain": "bootwright-lab-rhel-01", "owned": True,
        "power": "Off", "state": "shut off", "system": "uuid", "unit": "active",
    }
    assert whole["postcondition"] is False
    # A controller that still answers for a system or a power state is not gone.
    assert substrate_machine_protocol.absence(GONE, "On", None, DIGEST)["postcondition"] is False
    assert substrate_machine_protocol.absence(GONE, None, "uuid", DIGEST)["postcondition"] is False


def test_a_machine_observation_carries_whether_the_hypervisor_answered():
    answered = substrate_machine_protocol.presence(machine_observation(), "Off", "uuid", DIGEST)
    assert answered["answered"] is True
    silent = substrate_machine_protocol.presence(
        machine_observation(answered=False, domain="", owned=False, state=""), "On", "", DIGEST,
    )
    assert silent["answered"] is False
    assert silent["domain"] == ""
    # The controller's power state is what the engine reads instead.
    assert silent["power"] == "On"


def machine_action(args):
    module = substrate_machine_protocol.ActionModule.__new__(substrate_machine_protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module


def test_a_removal_the_hypervisor_did_not_answer_for_publishes_nothing(monkeypatch):
    published = []
    monkeypatch.setattr(substrate_machine_protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "removed": True}
    result = machine_action(dict(arguments, observation=dict(GONE, answered=False))).run(task_vars={})
    assert result["failed"] is True
    assert result["msg"].endswith("the hypervisor did not answer for: domain")
    assert published == []
    assert machine_action(dict(arguments, observation=GONE)).run(task_vars={}) == {"changed": False}
    assert published[0]["evidence"]["answered"] is True
    assert published[0]["evidence"]["postcondition"] is True


def observed(action, protocol, monkeypatch, observation, **extra):
    """The evidence one read-only observation publishes."""
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = dict({"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "observed": True}, **extra)
    assert action(dict(arguments, observation=observation)).run(task_vars={}) == {"changed": False}
    assert len(published) == 1
    return published[0]["evidence"]


# The observation, not its caller, chooses the form: absence only when the
# hypervisor answered and nothing is left, so disks, a held socket or a silent
# hypervisor reach the engine as what they are.
def test_a_machine_observation_is_absence_only_when_everything_is_gone(monkeypatch):
    def publish(observation, power=""):
        return observed(machine_action, substrate_machine_protocol, monkeypatch, observation, power=power, system="")

    clean = publish(GONE)
    assert (clean["absent"], clean["postcondition"]) == (True, True)
    disks = publish(dict(GONE, disks=[{"name": "root", "present": True, "sizeGiB": 60}]))
    assert (disks["absent"], disks["disks"][0]["present"]) == (False, True)
    held = publish(dict(GONE, listener=True))
    assert (held["absent"], held["listener"]) == (False, True)
    silent = publish(dict(GONE, answered=False), power="Off")
    assert (silent["absent"], silent["answered"], silent["power"]) == (False, False, "Off")


def host_action(args):
    module = substrate_host_protocol.ActionModule.__new__(substrate_host_protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module


HOST_GONE = host_observation(pool="", networks=[forgotten_network()], directory=False)


def test_a_provider_host_absence_needs_an_answer_and_no_directory(monkeypatch):
    published = []
    monkeypatch.setattr(substrate_host_protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "removed": True}
    silent = host_action(dict(arguments, observation=dict(HOST_GONE, uri=False))).run(task_vars={})
    assert silent["failed"] is True
    assert silent["msg"].endswith("the hypervisor did not answer for: networks, pool")
    kept = host_action(dict(arguments, observation=dict(HOST_GONE, directory=True))).run(task_vars={})
    assert kept["failed"] is True
    assert kept["msg"].endswith("still present: directory")
    defined = dict(HOST_GONE, networks=[dict(forgotten_network(), state="inactive")])
    still = host_action(dict(arguments, observation=defined)).run(task_vars={})
    assert still["failed"] is True
    assert still["msg"].endswith("still present: networks")
    # A pool the hypervisor still defines is published as observed, so it keeps
    # the removal from completing rather than reading as gone.
    pooled = substrate_host_protocol.absence(dict(HOST_GONE, pool="inactive"), DIGEST)
    assert (pooled["pool"], pooled["postcondition"]) == ("inactive", False)
    kept_pool = host_action(dict(arguments, observation=dict(HOST_GONE, pool="inactive"))).run(task_vars={})
    assert kept_pool["failed"] is True
    assert kept_pool["msg"].endswith("still present: pool")
    assert published == []
    # A managed network the hypervisor answered it forgot proves absence, and it
    # is published as observed rather than dropped.
    assert host_action(dict(arguments, observation=HOST_GONE)).run(task_vars={}) == {"changed": False}
    evidence = published[0]["evidence"]
    assert (evidence["absent"], evidence["postcondition"], evidence["uri"], evidence["directory"]) == (True, True, True, False)
    assert evidence["networks"] == [{
        "answered": True, "autostart": False, "bridge": False, "definition": False, "managed": True, "name": "n", "owned": False,
        "state": "",
    }]
    assert (evidence["hypervisor"], evidence["services"]) == (True, [{"enabled": True, "name": "virtnetworkd.service", "state": "active"}])


def test_a_provider_host_observation_is_absence_only_when_the_uri_answered(monkeypatch):
    def publish(observation):
        return observed(host_action, substrate_host_protocol, monkeypatch, observation)

    gone = publish(HOST_GONE)
    assert (gone["absent"], gone["postcondition"]) == (True, True)
    silent = publish(dict(HOST_GONE, uri=False))
    assert (silent["absent"], silent["uri"], silent["postcondition"]) == (False, False, False)
    directory = publish(dict(HOST_GONE, directory=True))
    assert (directory["absent"], directory["directory"]) == (False, True)
    pool = publish(dict(HOST_GONE, pool="inactive"))
    assert (pool["absent"], pool["pool"]) == (False, "inactive")
    defined = publish(dict(HOST_GONE, networks=[dict(forgotten_network(), owned=True)]))
    assert defined["absent"] is False


# The uri answering proves only that the hypervisor driver did. The network
# driver is a daemon of its own that may be silent meanwhile, and virsh reports
# the lookup it failed exactly as one that found nothing, so a removal over it
# is never proved and its observation never reads as absence.
def test_a_provider_host_a_driver_did_not_answer_for_proves_no_removal(monkeypatch, tmp_path):
    silent = (1, "", "error: failed to get network 'bootwright-lab-guests'\n"
              "error: Failed to connect socket to '/var/run/libvirt/virtnetworkd-sock': No such file or directory\n")
    undefined = (1, "", "error: failed to get pool 'bootwright-lab-p-vmedia'\nerror: Storage pool not found\n")

    def runner(answers):
        def run(argv, check_rc=False, environ_update=None):
            del check_rc, environ_update
            return answers.get(" ".join(argv[3:]) if argv[0].endswith("virsh") else argv[-1], (0, "", ""))
        return run

    request = {
        "networks": [{"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True}],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": [],
        "uri": "qemu:///system",
    }
    network_silent = observe_host(runner({
        "net-dumpxml bootwright-lab-guests": silent,
        "net-list --all --name": (1, "", "error: Failed to list networks\n"),
        "pool-info bootwright-lab-p-vmedia": undefined,
        "pool-list --all --name": (0, "\n", ""),
    }), request)
    pool_silent = observe_host(runner({
        "net-dumpxml bootwright-lab-guests": silent,
        "net-list --all --name": (0, "\n", ""),
        "pool-info bootwright-lab-p-vmedia": undefined,
        "pool-list --all --name": (1, "", "error: Failed to list pools\n"),
    }), request)

    def removal(observation):
        published = []
        monkeypatch.setattr(substrate_host_protocol, "emit", lambda message, **kwargs: published.append(message))
        arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "removed": True}
        return host_action(dict(arguments, observation=observation)).run(task_vars={}), published

    for observation, unanswered in ((network_silent, "networks"), (pool_silent, "pool")):
        assert (observation["uri"], observation["directory"], observation["pool"]) == (True, False, "")
        result, published = removal(observation)
        assert result["failed"] is True, unanswered
        assert result["msg"].endswith("the libvirt driver that owns it did not answer for: " + unanswered)
        assert published == []
        evidence = observed(host_action, substrate_host_protocol, monkeypatch, observation)
        assert (evidence["absent"], evidence["postcondition"]) == (False, False), unanswered
    # Both drivers answering that nothing is defined is the removal's proof.
    both = dict(network_silent, poolAnswered=True, networks=[dict(network_silent["networks"][0], answered=True)])
    result, published = removal(both)
    assert result == {"changed": False}
    assert published[0]["evidence"]["postcondition"] is True


# Evidence without the listener or the directory proves neither held nor free,
# so an observation that does not report one publishes nothing.
def test_an_observation_missing_the_listener_or_the_directory_is_not_published(monkeypatch):
    published = []
    for protocol in (substrate_host_protocol, substrate_machine_protocol):
        monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "observed": True, "power": "", "system": ""}
    for action, observation in (
        (machine_action, {key: value for key, value in GONE.items() if key != "listener"}),
        (machine_action, dict(GONE, listener="false")),
        (machine_action, {key: value for key, value in machine_observation().items() if key != "listener"}),
        (host_action, {key: value for key, value in HOST_GONE.items() if key != "directory"}),
        (host_action, dict(HOST_GONE, directory=0)),
        (host_action, {key: value for key, value in host_observation().items() if key != "directory"}),
    ):
        result = action(dict(arguments, observation=observation)).run(task_vars={})
        assert result["failed"] is True
        assert result["msg"].endswith("result could not be published")
    assert published == []
    for arguments in ((machine_observation(listener=None), "Off", "uuid", DIGEST), (dict(GONE, listener=None), None, None, DIGEST)):
        with pytest.raises(ValueError):
            substrate_machine_protocol.presence(*arguments)
        with pytest.raises(ValueError):
            substrate_machine_protocol.absence(*arguments)
    for protocol_call in (substrate_host_protocol.presence, substrate_host_protocol.absence):
        with pytest.raises(ValueError):
            protocol_call(host_observation(directory=None), DIGEST)


# An observation chooses its own form, so a caller asking it for a removal's is
# a malformed call rather than a way to publish unproved absence.
def test_an_observation_takes_no_removed_argument(monkeypatch):
    published = []
    for protocol in (substrate_host_protocol, substrate_machine_protocol):
        monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    arguments = {"phase": "completed", "outcome": "unchanged", "digest": DIGEST, "observed": True, "removed": True}
    for action, observation in ((machine_action, GONE), (host_action, HOST_GONE)):
        result = action(dict(arguments, observation=observation)).run(task_vars={})
        assert result["failed"] is True
        assert result["msg"].endswith("result could not be published")
    assert published == []
