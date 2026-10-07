"""The observation is assembled here, so it is tested without a hypervisor."""

from __future__ import annotations

import ipaddress
import sys

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import (
    MAX_OUTPUT,
    domain_metadata,
    domain_state,
    invoke,
    listening,
    network_guests,
    network_state,
    observe_host,
    observe_machine,
    packages_present,
    pool_state,
    unit_enabled,
    unit_state,
)

OWNED_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
  <uuid>4c0a4300-aa43-458c-86d7-ac2256d1fc00</uuid>
  <bridge name="virbr-lab"/>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>lab</bw:context><bw:attachment>bootwright-lab-guests</bw:attachment>
    </bw:owner>
  </metadata>
</network>"""

FOREIGN_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
  <bridge name="virbr-lab"/>
</network>"""

DOMAIN_UUID = "1ab52b3c-0000-8000-8000-000000000000"
OWNED_DOMAIN = """<domain type="kvm">
  <name>bootwright-lab-rhel-01</name>
  <uuid>1ab52b3c-0000-8000-8000-000000000000</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1"><bw:context>lab</bw:context><bw:machine>rhel-01</bw:machine></bw:owner>
  </metadata>
</domain>"""


def runner_for(answers):
    """A runner that replies to exact argument vectors and refuses anything else."""

    def run(argv, check_rc=False, environ_update=None):
        del check_rc, environ_update
        key = " ".join(argv)
        for prefix, answer in answers.items():
            if key.endswith(prefix):
                return answer
        return 1, "", ""

    return run


def test_an_absolute_executable_is_required():
    with pytest.raises(ValueError):
        invoke(runner_for({}), ["virsh", "version"])


def test_a_closure_is_present_only_when_every_package_is():
    complete = runner_for({"--query qemu-kvm": (0, "", ""), "--query swtpm": (0, "", "")})
    assert packages_present(complete, ["qemu-kvm", "swtpm"])
    assert not packages_present(complete, ["qemu-kvm", "absent"])
    # An empty closure proves nothing, so it is never present.
    assert not packages_present(complete, [])


def loaded(state):
    return (0, "LoadState=loaded\nActiveState=%s\n" % state, "")


def test_only_a_known_unit_state_is_reported():
    assert unit_state(runner_for({"libvirtd.service": loaded("active")}), "libvirtd.service") == "active"
    assert unit_state(runner_for({"libvirtd.service": loaded("bananas")}), "libvirtd.service") == ""


# A socket-activated driver answers `indirect`: it runs when something asks for
# it and takes the networks and pools it owns down with it, which is exactly
# the host that loses its guest network across a restart.
def test_only_an_enabled_unit_starts_with_the_host():
    for reported in ("enabled\n", "enabled-runtime\n", "indirect\n", "disabled\n", "static\n"):
        runner = runner_for({"is-enabled virtnetworkd.service": (0, reported, "")})
        assert unit_enabled(runner, "virtnetworkd.service") == (reported.strip() == "enabled")
    assert not unit_enabled(runner_for({}), "virtnetworkd.service")


# systemd reports `inactive` for a unit it has never heard of, so a removal that
# read the active state alone could never prove the unit gone.
def test_a_unit_systemd_does_not_know_is_absent_rather_than_inactive():
    removed = runner_for({
        "bootwright-lab-bmc-rhel-01.service": (0, "LoadState=not-found\nActiveState=inactive\n", ""),
    })
    assert unit_state(removed, "bootwright-lab-bmc-rhel-01.service") == ""


def test_a_stopped_unit_that_still_exists_is_still_reported():
    stopped = runner_for({"bootwright-lab-bmc-rhel-01.service": loaded("inactive")})
    assert unit_state(stopped, "bootwright-lab-bmc-rhel-01.service") == "inactive"


def test_a_network_without_this_contexts_metadata_is_foreign():
    owned = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, OWNED_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\nAutostart:      yes\n", ""),
    })
    assert network_state(owned, "qemu:///system", "bootwright-lab-guests", context="lab") == {
        "answered": True, "autostart": True, "definition": False, "drifted": False, "state": "active", "owned": True,
        "bridge": "virbr-lab", "uuid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00",
    }
    assert network_state(owned, "qemu:///system", "bootwright-lab-guests", context="other")["owned"] is False
    foreign = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, FOREIGN_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(foreign, "qemu:///system", "bootwright-lab-guests", context="lab")["owned"] is False


def test_an_absent_or_malformed_network_reports_no_state():
    assert network_state(runner_for({}), "qemu:///system", "gone")["state"] == ""
    malformed = runner_for({"net-dumpxml gone": (0, "not xml", "")})
    assert network_state(malformed, "qemu:///system", "gone") == {
        "answered": False, "autostart": False, "definition": False, "drifted": False, "state": "", "owned": False, "bridge": "",
        "uuid": "",
    }


# The network and storage drivers are daemons of their own, reached through the
# hypervisor's connection only when a network or pool call is made, and virsh
# reports a lookup it failed the same way whatever the cause
# (tools/virsh-network.c, tools/virsh-pool.c). A network or pool is therefore
# absent only when its driver completes a listing that does not name it.
NETWORK_DRIVER_SILENT = (1, "", "error: failed to get network 'bootwright-lab-guests'\n"
                         "error: Failed to connect socket to '/var/run/libvirt/virtnetworkd-sock': No such file or directory\n")
NETWORK_UNDEFINED = (1, "", "error: failed to get network 'bootwright-lab-guests'\n"
                     "error: Network not found: no network with matching name 'bootwright-lab-guests'\n")
POOL_UNDEFINED = (1, "", "error: failed to get pool 'p'\nerror: Storage pool not found: no storage pool with matching name 'p'\n")


def test_a_network_or_pool_is_absent_only_when_its_driver_answered_for_it():
    undefined = runner_for({"net-dumpxml bootwright-lab-guests": NETWORK_UNDEFINED, "net-list --all --name": (0, "default\n\n", "")})
    assert network_state(undefined, "qemu:///system", "bootwright-lab-guests") == {
        "answered": True, "autostart": False, "definition": False, "drifted": False, "state": "", "owned": False, "bridge": "",
        "uuid": "",
    }
    for name, answers in {
        "driver silent": {"net-dumpxml bootwright-lab-guests": NETWORK_DRIVER_SILENT,
                          "net-list --all --name": (1, "", "error: Failed to list networks\n")},
        "listed after all": {"net-dumpxml bootwright-lab-guests": NETWORK_UNDEFINED,
                             "net-list --all --name": (0, "bootwright-lab-guests\n\n", "")},
        "listing truncated": {"net-dumpxml bootwright-lab-guests": NETWORK_UNDEFINED, "net-list --all --name": (0, "x" * MAX_OUTPUT, "")},
    }.items():
        state = network_state(runner_for(answers), "qemu:///system", "bootwright-lab-guests")
        assert state == {
            "answered": False, "autostart": False, "definition": False, "drifted": False, "state": "", "owned": False,
            "bridge": "", "uuid": "",
        }, name
    undefined = runner_for({"pool-info p": POOL_UNDEFINED, "pool-list --all --name": (0, "default\n\n", "")})
    assert pool_state(undefined, "qemu:///system", "p", "/pool") == {"answered": True, "state": "", "autostart": False, "owned": False}
    for name, answers in {
        "driver silent": {"pool-info p": POOL_UNDEFINED, "pool-list --all --name": (1, "", "error: Failed to list pools\n")},
        "listed after all": {"pool-info p": POOL_UNDEFINED, "pool-list --all --name": (0, "p\n\n", "")},
    }.items():
        assert pool_state(runner_for(answers), "qemu:///system", "p", "/pool") == {
            "answered": False, "state": "", "autostart": False, "owned": False,
        }, name
    running = runner_for({
        "pool-info p": (0, "Name:           p\nState:          running\nAutostart:      yes\n", ""),
        "pool-dumpxml p": (0, "<pool type='dir'><name>p</name><target><path>/pool</path></target></pool>", ""),
    })
    assert pool_state(running, "qemu:///system", "p", "/pool") == {"answered": True, "state": "active", "autostart": True, "owned": True}


# The uri answering proves only that the hypervisor did: with the network driver
# silent, the observation reports the network unanswered rather than absent.
def test_a_host_whose_network_driver_is_silent_reports_the_network_unanswered(tmp_path):
    runner = runner_for({
        "version": (0, "", ""),
        "net-dumpxml bootwright-lab-guests": NETWORK_DRIVER_SILENT,
        "net-list --all --name": (1, "", "error: Failed to list networks\n"),
        "pool-info bootwright-lab-p-vmedia": POOL_UNDEFINED,
        "pool-list --all --name": (0, "\n", ""),
    })
    request = {
        "networks": [
            {"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True},
            {"name": "external", "bridge": "br0", "managed": False},
        ],
        "packages": [],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": [],
        "uri": "qemu:///system",
    }
    observation = observe_host(runner, request)
    assert observation["uri"] is True
    assert [entry["answered"] for entry in observation["networks"]] == [False, False]
    assert (observation["pool"], observation["poolAnswered"]) == ("", True)


def test_an_observed_network_carries_the_identity_libvirt_assigned_it(tmp_path):
    """libvirt refuses to redefine a name under a new UUID, so apply reads it."""
    runner = runner_for({
        "version": (0, "", ""),
        "net-dumpxml bootwright-lab-guests": (0, OWNED_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
        "--query qemu-kvm": (0, "", ""),
    })
    request = {
        "networks": [{"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True}],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": ["virtnetworkd.service"],
        "uri": "qemu:///system",
    }
    observation = observe_host(runner, request)
    assert observation["networks"][0]["uuid"] == "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
    # A network without one reports no identity rather than inventing it.
    without = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, FOREIGN_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(without, "qemu:///system", "bootwright-lab-guests")["uuid"] == ""


# A managed network as libvirt reports it once the network template defined it:
# every value the template wrote, formatted as defined, with the UUID and the
# bridge MAC address libvirt adds of its own (virNetworkDefParseXML and
# virNetworkDefFormatBuf in src/conf/network_conf.c, virNetworkSetBridgeMacAddr
# from networkValidate in src/network/bridge_driver.c,
# https://gitlab.com/libvirt/libvirt).
FROZEN_NETWORK = {"name": "bootwright-lab-guests", "bridge": "virbr-lab", "address": "198.51.100.1/24", "forward": "nat", "managed": True}
CARRIED_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
  <uuid>4c0a4300-aa43-458c-86d7-ac2256d1fc00</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>lab</bw:context>
      <bw:attachment>bootwright-lab-guests</bw:attachment>
    </bw:owner>
  </metadata>
  <forward mode='nat'/>
  <bridge name='virbr-lab' zone='trusted' stp='on' delay='0'/>
  <mac address='52:54:00:1c:2d:3e'/>
  <dns enable='no'/>
  <ip address='198.51.100.1' prefix='24'/>
</network>"""
# The same network running another host address, as it does after the request
# moved the address while the network stayed active.
READDRESSED_NETWORK = CARRIED_NETWORK.replace("198.51.100.1", "198.51.100.254")


def definition_of(live, kept=None):
    """The definition a network reports while it runs `live` and keeps `kept` for its next start."""
    answers = {
        "net-dumpxml bootwright-lab-guests": (0, live, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    }
    if kept is not None:
        answers["net-dumpxml --inactive bootwright-lab-guests"] = kept
    return network_state(runner_for(answers), "qemu:///system", "bootwright-lab-guests", FROZEN_NETWORK, "lab")["definition"]


# Defining an active network changes only the definition it next starts from,
# so a network carries its frozen entry only while it runs it and keeps it.
def test_a_network_carries_its_frozen_definition_only_while_it_runs_and_keeps_it():
    assert definition_of(CARRIED_NETWORK, (0, CARRIED_NETWORK, "")) is True
    for name, (live, kept) in {
        "running another address": (READDRESSED_NETWORK, (0, CARRIED_NETWORK, "")),
        "keeping another address": (CARRIED_NETWORK, (0, READDRESSED_NETWORK, "")),
        "keeping nothing it answered": (CARRIED_NETWORK, (1, "", "error: failed to get network 'bootwright-lab-guests'\n")),
        "keeping a definition that is not XML": (CARRIED_NETWORK, (0, "not xml", "")),
        "not asked what it keeps": (CARRIED_NETWORK, None),
    }.items():
        assert definition_of(live, kept) is False, name


def test_a_network_that_drifted_in_any_frozen_value_does_not_carry_its_definition():
    kept = (0, CARRIED_NETWORK, "")
    for name, (old, new) in {
        "bridge": ("name='virbr-lab'", "name='virbr-other'"),
        "firewall zone": ("zone='trusted'", "zone='libvirt'"),
        "spanning tree": ("stp='on'", "stp='off'"),
        "forward mode": ("<forward mode='nat'/>", "<forward mode='route'/>"),
        "forwarding dropped": ("<forward mode='nat'/>", ""),
        "resolver": ("<dns enable='no'/>", "<dns/>"),
        "prefix": ("prefix='24'", "prefix='16'"),
        "second address": ("<ip address='198.51.100.1' prefix='24'/>",
                           "<ip address='198.51.100.1' prefix='24'/><ip address='203.0.113.1' prefix='24'/>"),
        "DHCP enabled": ("<ip address='198.51.100.1' prefix='24'/>",
                         "<ip address='198.51.100.1' prefix='24'><dhcp>"
                         "<range start='198.51.100.2' end='198.51.100.254'/></dhcp></ip>"),
        "owning context": ("<bw:context>lab</bw:context>", "<bw:context>other</bw:context>"),
        "owning attachment": ("<bw:attachment>bootwright-lab-guests</bw:attachment>", ""),
    }.items():
        drifted = CARRIED_NETWORK.replace(old, new)
        assert drifted != CARRIED_NETWORK, name
        assert definition_of(drifted, kept) is False, name
    isolated = CARRIED_NETWORK.replace("<forward mode='nat'/>", "")
    answers = {
        "net-dumpxml bootwright-lab-guests": (0, isolated, ""),
        "net-dumpxml --inactive bootwright-lab-guests": (0, isolated, ""),
        "net-info bootwright-lab-guests": (0, "Active:         no\n", ""),
    }
    frozen = dict(FROZEN_NETWORK, forward="none")
    assert network_state(runner_for(answers), "qemu:///system", "bootwright-lab-guests", frozen, "lab")["definition"] is True


def test_an_observed_managed_network_reports_whether_it_carries_its_frozen_entry(tmp_path):
    request = {
        "identity": {"context": "lab"},
        "networks": [FROZEN_NETWORK, {"name": "external", "bridge": "br0", "managed": False}],
        "packages": [],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": [],
        "uri": "qemu:///system",
    }
    answers = {
        "version": (0, "", ""),
        "net-dumpxml bootwright-lab-guests": (0, CARRIED_NETWORK, ""),
        "net-dumpxml --inactive bootwright-lab-guests": (0, CARRIED_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    }
    observation = observe_host(runner_for(answers), request)
    assert [entry["definition"] for entry in observation["networks"]] == [True, False]
    other = observe_host(runner_for(answers), dict(request, identity={"context": "other"}))
    assert other["networks"][0]["definition"] is False


def drifted_of(live, kept, active="yes"):
    """Whether a network running `live`, keeping `kept`, reads as one only a restart converges."""
    answers = {
        "net-dumpxml bootwright-lab-guests": (0, live, ""),
        "net-dumpxml --inactive bootwright-lab-guests": (0, kept, ""),
        "net-info bootwright-lab-guests": (0, "Active:         %s\n" % active, ""),
    }
    return network_state(runner_for(answers), "qemu:///system", "bootwright-lab-guests", FROZEN_NETWORK, "lab")["drifted"]


# Defining a network again replaces what it keeps for its next start, so only
# one that runs another definition while active needs a restart. One that is
# stopped, or keeps another definition while it runs the frozen one, does not.
def test_only_an_owned_active_network_running_another_definition_has_drifted():
    assert drifted_of(READDRESSED_NETWORK, CARRIED_NETWORK) is True
    assert drifted_of(READDRESSED_NETWORK, READDRESSED_NETWORK) is True
    assert drifted_of(CARRIED_NETWORK, READDRESSED_NETWORK) is False
    assert drifted_of(CARRIED_NETWORK, CARRIED_NETWORK) is False
    assert drifted_of(READDRESSED_NETWORK, CARRIED_NETWORK, active="no") is False
    unowned = READDRESSED_NETWORK.replace(CARRIED_NETWORK[CARRIED_NETWORK.index("  <metadata>"):CARRIED_NETWORK.index("  <forward")], "")
    assert "bw:owner" not in unowned
    assert drifted_of(unowned, CARRIED_NETWORK) is False


# A running domain as `virsh dumpxml` prints it: an interface on a libvirt
# network names it and, while the domain runs, the port and the bridge the
# network gave it (https://libvirt.org/formatdomain.html#virtual-network); one
# bridged to the LAN names the bridge alone (#bridge-to-lan there). The
# ownership metadata is what the machine role's domain template writes.
RUNNING_MACHINE = """<domain type='kvm' id='3'>
  <name>bootwright-lab-rhel-01</name>
  <uuid>1ab52b3c-0000-8000-8000-000000000000</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>lab</bw:context>
      <bw:machine>rhel-01</bw:machine>
    </bw:owner>
  </metadata>
  <devices>
    <interface type='network'>
      <mac address='52:54:00:6b:3c:58'/>
      <source network='bootwright-lab-guests' portid='0b2a3c4d-5e6f-4a1b-8c2d-3e4f5a6b7c8d' bridge='virbr-lab'/>
      <target dev='vnet0'/>
      <model type='virtio'/>
      <alias name='net0'/>
    </interface>
  </devices>
</domain>"""
BRIDGED_DOMAIN = """<domain type='kvm' id='4'>
  <name>workstation</name>
  <devices>
    <interface type='bridge'>
      <mac address='52:54:00:11:22:33'/>
      <source bridge='virbr-lab'/>
      <target dev='vnet1'/>
    </interface>
  </devices>
</domain>"""
ELSEWHERE_DOMAIN = """<domain type='kvm' id='5'>
  <name>bootwright-lab-rhel-02</name>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>lab</bw:context>
      <bw:machine>rhel-02</bw:machine>
    </bw:owner>
  </metadata>
  <devices>
    <interface type='network'>
      <source network='bootwright-lab-uplink' portid='1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e5f' bridge='virbr-up'/>
    </interface>
  </devices>
</domain>"""
ACTIVE_DOMAINS = (0, "bootwright-lab-rhel-01\nworkstation\nbootwright-lab-rhel-02\n\n", "")


def guests_answers(**replaced):
    answers = {
        "list --name": ACTIVE_DOMAINS,
        "dumpxml bootwright-lab-rhel-01": (0, RUNNING_MACHINE, ""),
        "dumpxml workstation": (0, BRIDGED_DOMAIN, ""),
        "dumpxml bootwright-lab-rhel-02": (0, ELSEWHERE_DOMAIN, ""),
    }
    answers.update(replaced)
    return answers


def guests_of(answers, bridge="virbr-lab", context="lab"):
    return network_guests(runner_for(answers), "qemu:///system", "bootwright-lab-guests", bridge, context)


def test_every_running_domain_plugged_into_the_network_is_named():
    assert guests_of(guests_answers()) == ["Machine rhel-01", "domain workstation"]
    # Another context's Machine is not this context's to stop, so it is named by its domain.
    assert guests_of(guests_answers(), context="other") == ["domain bootwright-lab-rhel-01", "domain workstation"]
    assert guests_of(guests_answers(**{"list --name": (0, "\n", "")})) == []
    # The bridge a restart removes is the one the network runs, whatever the
    # frozen entry names, so a domain bridged to another one is not cut off.
    assert guests_of(guests_answers(), bridge="virbr-new") == ["Machine rhel-01"]


def test_a_listing_or_definition_the_hypervisor_did_not_answer_proves_nothing_idle():
    for name, replaced in {
        "listing refused": {"list --name": (1, "", "error: failed to connect to the hypervisor\n")},
        "definition refused": {"dumpxml workstation": (1, "", "error: failed to get domain 'workstation'\n")},
        "definition not XML": {"dumpxml workstation": (0, "not xml", "")},
        # Cut in its trailing blanks, a definition still parses: only its length tells.
        "definition truncated": {"dumpxml workstation": (0, BRIDGED_DOMAIN.ljust(MAX_OUTPUT), "")},
    }.items():
        assert guests_of(guests_answers(**replaced)) is None, name


# The bound cuts a longer listing wherever it falls, here on a line boundary,
# so every name it kept is complete and resolves to a domain elsewhere: only
# the listing's length tells that the active domains past the cut were never
# read. One name fewer fits under the bound and is answered.
def test_a_listing_the_bound_cut_proves_nothing_idle():
    line = "a" * 15 + "\n"
    assert MAX_OUTPUT % len(line) == 0
    fitting = MAX_OUTPUT // len(line)
    elsewhere = {"dumpxml " + line.strip(): (0, ELSEWHERE_DOMAIN, "")}
    assert guests_of({"list --name": (0, line * (fitting - 1), ""), **elsewhere}) == []
    assert guests_of({"list --name": (0, line * (fitting + 1), ""), **elsewhere}) is None


def drift_request(tmp_path):
    return {
        "identity": {"context": "lab"},
        "networks": [FROZEN_NETWORK],
        "packages": [],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": [],
        "uri": "qemu:///system",
    }


def test_the_observation_names_what_runs_on_each_drifted_network_alone(tmp_path):
    answers = guests_answers(**{
        "version": (0, "", ""),
        "net-dumpxml bootwright-lab-guests": (0, READDRESSED_NETWORK, ""),
        "net-dumpxml --inactive bootwright-lab-guests": (0, CARRIED_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    observation = observe_host(runner_for(answers), drift_request(tmp_path))
    assert observation["drifted"] == [
        {"name": "bootwright-lab-guests", "guests": ["Machine rhel-01", "domain workstation"], "guestsAnswered": True},
    ]
    assert observation["networks"][0]["definition"] is False
    # A network still running the bridge an earlier entry named is read by that bridge.
    renamed = dict(answers, **{
        "net-dumpxml bootwright-lab-guests": (0, READDRESSED_NETWORK.replace("name='virbr-lab'", "name='virbr-old'"), ""),
        "dumpxml workstation": (0, BRIDGED_DOMAIN.replace("virbr-lab", "virbr-old"), ""),
    })
    assert observe_host(runner_for(renamed), drift_request(tmp_path))["drifted"][0]["guests"] == [
        "Machine rhel-01", "domain workstation",
    ]
    silent = dict(answers, **{"list --name": (1, "", "error: failed to connect to the hypervisor\n")})
    assert observe_host(runner_for(silent), drift_request(tmp_path))["drifted"] == [
        {"name": "bootwright-lab-guests", "guests": [], "guestsAnswered": False},
    ]
    # A network that runs the frozen definition is never restarted, so what
    # runs on it is not read at all.
    carried = dict(answers, **{"net-dumpxml bootwright-lab-guests": (0, CARRIED_NETWORK, ""), "list --name": (1, "", "")})
    assert observe_host(runner_for(carried), drift_request(tmp_path))["drifted"] == []


def test_a_domain_reports_its_identity_and_ownership():
    owned = runner_for({"dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, "")})
    assert domain_metadata(owned, "qemu:///system", "bootwright-lab-rhel-01", "lab", "rhel-01", DOMAIN_UUID) == {
        "answered": True, "present": True, "owned": True, "uuid": "1ab52b3c-0000-8000-8000-000000000000",
    }
    assert domain_metadata(runner_for({}), "qemu:///system", "gone", "lab", "gone", DOMAIN_UUID) == {
        "answered": False, "present": False, "owned": False, "uuid": "",
    }


def test_a_host_whose_connection_is_silent_reports_nothing_it_cannot_read(tmp_path):
    request = {
        "networks": [{"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True}],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": ["virtnetworkd.service"],
        "uri": "qemu:///system",
    }
    observation = observe_host(runner_for({}), request)
    assert observation["uri"] is False
    assert (observation["pool"], observation["poolAnswered"]) == ("", False)
    assert observation["networks"][0]["answered"] is False
    assert observation["networks"][0]["state"] == ""
    assert observation["networks"][0]["owned"] is False
    assert observation["networks"][0]["uuid"] == ""


# Every driver the provider depends on is observed the same way, because a
# daemon that is running but not enabled is the state this block converges.
def test_each_declared_driver_daemon_reports_its_state_and_enablement(tmp_path):
    runner = runner_for({
        "is-enabled virtnetworkd.service": (0, "disabled\n", ""),
        "virtnetworkd.service": loaded("active"),
        "version": (0, "", ""),
        "--query qemu-kvm": (0, "", ""),
    })
    request = {
        "networks": [],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "poolPath": str(tmp_path / "pool"),
        "services": ["virtnetworkd.service"],
        "uri": "qemu:///system",
    }
    assert observe_host(runner, request)["services"] == [
        {"name": "virtnetworkd.service", "state": "active", "enabled": False},
    ]


# Only `shut off` means removing a domain interrupts nothing. Every other state
# holds the memory and disks the removal would delete, and a domain whose state
# cannot be read proves nothing at all.
def test_only_a_shut_off_domain_reports_itself_idle():
    for reported in ("running", "paused", "pmsuspended", "crashed", "shut off"):
        answers = runner_for({"domstate bootwright-lab-rhel-01": (0, reported + "\n", "")})
        assert domain_state(answers, "qemu:///system", "bootwright-lab-rhel-01") == reported
    unreadable = runner_for({"domstate bootwright-lab-rhel-01": (1, "", "error: failed to get domain")})
    assert domain_state(unreadable, "qemu:///system", "bootwright-lab-rhel-01") == ""
    invented = runner_for({"domstate bootwright-lab-rhel-01": (0, "bananas\n", "")})
    assert domain_state(invented, "qemu:///system", "bootwright-lab-rhel-01") == ""


# virsh resolves every domain argument through virshLookupDomainInternal, which
# discards libvirt's reason and exits 1 with `error: failed to get domain
# '<name>'`; a connection that never opened reports `failed to connect to the
# hypervisor` instead (libvirt tools/virsh-util.c and tools/virsh.c). `virsh
# list --name` prints one name per line and exits 0 only once every domain is
# listed (tools/virsh-domain-monitor.c).
LOOKUP_REFUSED = (1, "", "error: failed to get domain 'bootwright-lab-rhel-01'\n")
CONNECTION_REFUSED = (1, "", "error: failed to connect to the hypervisor\n"
                      "error: Failed to connect socket to '/var/run/libvirt/virtqemud-sock': No such file or directory\n")
OTHER_DOMAINS = (0, "bootwright-lab-rhel-02\n\n", "")


def test_a_domain_the_hypervisor_does_not_define_is_told_apart_from_no_answer():
    undefined = runner_for({"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": OTHER_DOMAINS})
    assert domain_metadata(undefined, "qemu:///system", "bootwright-lab-rhel-01", "lab", "rhel-01", DOMAIN_UUID) == {
        "answered": True, "present": False, "owned": False, "uuid": "",
    }
    for name, answers in {
        "connection refused": {"dumpxml bootwright-lab-rhel-01": CONNECTION_REFUSED, "list --all --name": OTHER_DOMAINS},
        "listing refused": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (1, "", "error: Failed to list domains\n")},
        "listed after all": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (0, "bootwright-lab-rhel-01\n\n", "")},
        "listing truncated": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (0, "x" * (1 << 20), "")},
        "another failure": {"dumpxml bootwright-lab-rhel-01": (1, "", "error: internal error\n"), "list --all --name": OTHER_DOMAINS},
    }.items():
        metadata = domain_metadata(runner_for(answers), "qemu:///system", "bootwright-lab-rhel-01", "lab", "rhel-01", DOMAIN_UUID)
        assert metadata == {"answered": False, "present": False, "owned": False, "uuid": ""}, name


def machine_request(disks=None):
    return {
        "controller": {"address": "192.0.2.1", "port": 8000, "unit": "bootwright-lab-bmc-rhel-01"},
        "disks": disks or [],
        "domain": "bootwright-lab-rhel-01",
        "identity": {"block": "substrate-machine-rhel-01", "context": "lab", "object": "rhel-01"},
        "uri": "qemu:///system",
        "uuid": DOMAIN_UUID,
    }


# Both a domain that is not defined and a hypervisor that did not answer report
# no domain; only `answered` says which, and only the first proves absence.
def test_a_machine_observation_reports_whether_the_hypervisor_answered():
    undefined = runner_for({"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": OTHER_DOMAINS})
    observation = observe_machine(undefined, machine_request(), reader())
    assert (observation["answered"], observation["domain"], observation["state"]) == (True, "", "")
    silent = runner_for({"dumpxml bootwright-lab-rhel-01": CONNECTION_REFUSED})
    observation = observe_machine(silent, machine_request(), reader())
    assert (observation["answered"], observation["domain"], observation["state"]) == (False, "", "")
    defined = runner_for({
        "dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, ""),
        "domstate bootwright-lab-rhel-01": (0, "running\n", ""),
    })
    observation = observe_machine(defined, machine_request(), reader())
    assert (observation["answered"], observation["domain"], observation["state"]) == (True, "bootwright-lab-rhel-01", "running")


# The observation decides ownership from the request's own context, Machine and
# UUID: this context's domain is owned, and the same domain read under another
# context or another frozen UUID is not.
def test_a_machine_observation_owns_exactly_the_requests_domain():
    answers = {
        "dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, ""),
        "domstate bootwright-lab-rhel-01": (0, "running\n", ""),
    }
    observation = observe_machine(runner_for(answers), machine_request(), reader())
    assert (observation["owned"], observation["uuid"]) == (True, DOMAIN_UUID)
    other_context = machine_request()
    other_context["identity"] = dict(other_context["identity"], context="other")
    assert observe_machine(runner_for(answers), other_context, reader())["owned"] is False
    other_machine = machine_request()
    other_machine["identity"] = dict(other_machine["identity"], object="rhel-02")
    assert observe_machine(runner_for(answers), other_machine, reader())["owned"] is False
    other_uuid = dict(machine_request(), uuid="2bc63c4d-0000-8000-8000-000000000000")
    observation = observe_machine(runner_for(answers), other_uuid, reader())
    assert (observation["owned"], observation["uuid"]) == (False, DOMAIN_UUID)


# The kernel's layout of /proc/net/tcp and tcp6: a header, then one line per
# socket with every listening socket first, each local address printed as its
# 32-bit words in host byte order and its port in hexadecimal
# (Documentation/networking/proc_net_tcp.rst in the Linux tree).
TCP_HEADER = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
TCP6_HEADER = (
    "  sl  local_address                         remote_address                        "
    "st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
)
LISTEN, ESTABLISHED = "0A", "01"
ENTRY = "%4d: %s %s:0000 %s 00000000:00000000 00:00000000 00000000     0        0 %d 1 0000000000000000 100 0 0 10 0\n"


def table_word(address):
    packed = ipaddress.ip_address(address).packed
    return "".join("%08X" % int.from_bytes(packed[at:at + 4], sys.byteorder) for at in range(0, len(packed), 4))


def table_entry(index, address, port, state=LISTEN):
    remote = "0.0.0.0" if ipaddress.ip_address(address).version == 4 else "::"
    local = "%s:%04X" % (table_word(address), port)
    return ENTRY % (index, local, table_word(remote), state, 20000 + index)


def reader(tcp=(), tcp6=(), asked=None):
    """PID 1's socket tables holding these (address, port[, state]) entries.

    A table given as None is missing, and every path asked for is recorded.
    """
    tables = {"/proc/1/net/tcp": (TCP_HEADER, tcp), "/proc/1/net/tcp6": (TCP6_HEADER, tcp6)}

    def read(path):
        if asked is not None:
            asked.append(path)
        header, entries = tables.get(path, (None, None))
        if entries is None:
            raise FileNotFoundError(path)
        return [header] + [table_entry(index, *entry) for index, entry in enumerate(entries)]

    return read


# The controller binds its address with host networking, so its port is taken by
# a listener on that address or its family's wildcard and, for an IPv4 address,
# on its IPv4-mapped form or the IPv6 wildcard, which accepts IPv4 too.
def test_a_listener_holds_the_port_by_its_address_or_its_family_wildcard():
    for tcp, tcp6 in (
        ([("192.0.2.1", 8000)], []),
        ([("0.0.0.0", 8000)], []),
        ([], [("::ffff:192.0.2.1", 8000)]),
        ([], [("::", 8000)]),
    ):
        assert listening("192.0.2.1", 8000, reader(tcp, tcp6)), (tcp, tcp6)
    for tcp6 in ([("2001:db8::1", 8000)], [("::", 8000)]):
        assert listening("2001:db8::1", 8000, reader([], tcp6)), tcp6
    others = reader([("192.0.2.2", 8000)], [("::ffff:192.0.2.2", 8000), ("2001:db8::1", 8000)])
    assert not listening("192.0.2.1", 8000, others)
    assert not listening("2001:db8::1", 8000, reader([("0.0.0.0", 8000)], [("2001:db8::2", 8000)]))


# /proc/1/net/tcp on a little-endian Fedora 43 host printed systemd-resolved's
# stub listener, 127.0.0.53:53, with the local address 3500007F:0035.
@pytest.mark.skipif(sys.byteorder != "little", reason="the address was recorded on a little-endian host")
def test_a_recorded_local_address_is_read_as_the_kernel_printed_it():
    recorded = ENTRY % (0, "3500007F:0035", "00000000", LISTEN, 20000)
    tables = {"/proc/1/net/tcp": [TCP_HEADER, recorded], "/proc/1/net/tcp6": [TCP6_HEADER]}
    assert listening("127.0.0.53", 53, tables.get)
    assert not listening("127.0.0.53", 5353, tables.get)


def test_an_established_connection_or_another_port_is_not_a_listener():
    read = reader(
        [("192.0.2.1", 8001), ("192.0.2.1", 8000, ESTABLISHED)],
        [("::", 9000), ("::ffff:192.0.2.1", 8000, ESTABLISHED)],
    )
    assert not listening("192.0.2.1", 8000, read)


def long_table(header, entries, state):
    """A table whose entries, all in one state, run past the observation bound."""
    yield header
    for index in range(entries):
        yield table_entry(index, "192.0.2.9", 30000 + index % 1000, state)


def test_a_socket_table_that_cannot_be_read_fails_the_observation():
    past = MAX_OUTPUT // len(table_entry(0, "192.0.2.9", 30000)) + 2

    def refused(path):
        raise PermissionError(path)

    for read, failure in (
        (reader(tcp=None), OSError),
        (refused, OSError),
        (lambda path: [TCP_HEADER, "garbage\n"], ValueError),
        (lambda path: [], ValueError),
        (lambda path: long_table(TCP_HEADER, past, LISTEN), ValueError),
    ):
        with pytest.raises(failure):
            listening("192.0.2.1", 8000, read)
    # A host without an IPv6 stack has no tcp6 table at all.
    assert not listening("192.0.2.1", 8000, reader(tcp=[("192.0.2.1", 8001)], tcp6=None))
    # Only the listeners are read, so a busy table stays within the bound.
    busy = {"/proc/1/net/tcp": long_table(TCP_HEADER, past, ESTABLISHED), "/proc/1/net/tcp6": [TCP6_HEADER]}
    assert not listening("192.0.2.1", 8000, busy.get)


# The controller's quadlet unit runs with host networking, so its socket is in
# PID 1's network namespace, whichever namespace the observation runs in.
def test_the_listener_is_read_where_the_controller_unit_listens():
    asked = []
    assert not listening("192.0.2.1", 8000, reader(asked=asked))
    assert asked == ["/proc/1/net/tcp", "/proc/1/net/tcp6"]
    asked = []
    observation = observe_machine(runner_for({}), machine_request(), reader([("192.0.2.1", 8000)], asked=asked))
    assert observation["listener"] is True
    assert asked == ["/proc/1/net/tcp"]


# A leftover disk the apply would skip recreating must never read absent, so
# presence is whatever exists at the path; its image decides only the size.
def test_a_disk_that_exists_is_present_whatever_its_image_reports(tmp_path):
    (tmp_path / "file").write_bytes(b"not an image")
    (tmp_path / "directory").mkdir()
    (tmp_path / "dangling").symlink_to(tmp_path / "nowhere")
    for name, present in (("file", True), ("directory", True), ("dangling", True), ("missing", False)):
        request = machine_request([{"name": "root", "path": str(tmp_path / name), "sizeGiB": 60}])
        observation = observe_machine(runner_for({}), request, reader())
        assert observation["disks"] == [{"name": "root", "present": present, "sizeGiB": 0}], name


# A running or paused QEMU holds a lock on its images, and `qemu-img info`
# exits 1 with 'Failed to get shared "write" lock' on one unless it shares the
# image (-U, --force-share), as observed against an image `qemu-system-x86_64 -S`
# held open.
def test_a_disk_a_running_domain_holds_reads_its_size(tmp_path):
    disk = tmp_path / "root.qcow2"
    disk.write_bytes(b"")
    runner = runner_for({"info --force-share --output json " + str(disk): (0, '{"virtual-size": 64424509440}', "")})
    observation = observe_machine(runner, machine_request([{"name": "root", "path": str(disk), "sizeGiB": 60}]), reader())
    assert observation["disks"] == [{"name": "root", "present": True, "sizeGiB": 60}]


def pool_request(path):
    return {"networks": [], "packages": [], "poolName": "p", "poolPath": str(path), "services": [], "uri": "qemu:///system"}


# A removal deletes the pool directory after it undefines the pool, so the
# directory is read by its path whether or not the connection answered.
def test_the_pool_directory_is_observed_by_its_path(tmp_path):
    answers = runner_for({"version": (0, "", "")})
    (tmp_path / "directory").mkdir()
    (tmp_path / "file").write_bytes(b"")
    (tmp_path / "dangling").symlink_to(tmp_path / "nowhere")
    for name, present in (("directory", True), ("file", True), ("dangling", True), ("missing", False)):
        assert observe_host(answers, pool_request(tmp_path / name))["directory"] is present, name
    observation = observe_host(runner_for({}), pool_request(tmp_path / "directory"))
    assert (observation["uri"], observation["directory"]) == (False, True)
