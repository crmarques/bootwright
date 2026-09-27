"""The observation is assembled here, so it is tested without a hypervisor."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import (
    domain_metadata,
    domain_state,
    invoke,
    network_state,
    observe_host,
    observe_machine,
    packages_present,
    unit_enabled,
    unit_state,
)

OWNED_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
  <uuid>4c0a4300-aa43-458c-86d7-ac2256d1fc00</uuid>
  <bridge name="virbr-lab"/>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1"><bw:context>lab</bw:context></bw:owner>
  </metadata>
</network>"""

FOREIGN_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
  <bridge name="virbr-lab"/>
</network>"""

OWNED_DOMAIN = """<domain type="kvm">
  <name>bootwright-lab-rhel-01</name>
  <uuid>1ab52b3c-0000-8000-8000-000000000000</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1"><bw:machine>rhel-01</bw:machine></bw:owner>
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
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(owned, "qemu:///system", "bootwright-lab-guests") == {
        "state": "active", "owned": True, "bridge": "virbr-lab",
        "uuid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00",
    }
    foreign = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, FOREIGN_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(foreign, "qemu:///system", "bootwright-lab-guests")["owned"] is False


def test_an_absent_or_malformed_network_reports_no_state():
    assert network_state(runner_for({}), "qemu:///system", "gone")["state"] == ""
    malformed = runner_for({"net-dumpxml gone": (0, "not xml", "")})
    assert network_state(malformed, "qemu:///system", "gone") == {
        "state": "", "owned": False, "bridge": "", "uuid": "",
    }


def test_an_observed_network_carries_the_identity_libvirt_assigned_it():
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


def test_a_domain_reports_its_identity_and_ownership():
    owned = runner_for({"dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, "")})
    assert domain_metadata(owned, "qemu:///system", "bootwright-lab-rhel-01") == {
        "answered": True, "present": True, "owned": True, "uuid": "1ab52b3c-0000-8000-8000-000000000000",
    }
    assert domain_metadata(runner_for({}), "qemu:///system", "gone") == {
        "answered": False, "present": False, "owned": False, "uuid": "",
    }


def test_a_host_whose_connection_is_silent_reports_nothing_it_cannot_read():
    request = {
        "networks": [{"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True}],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "services": ["virtnetworkd.service"],
        "uri": "qemu:///system",
    }
    observation = observe_host(runner_for({}), request)
    assert observation["uri"] is False
    assert observation["pool"] == ""
    assert observation["networks"][0]["state"] == ""
    assert observation["networks"][0]["owned"] is False
    assert observation["networks"][0]["uuid"] == ""


# Every driver the provider depends on is observed the same way, because a
# daemon that is running but not enabled is the state this block converges.
def test_each_declared_driver_daemon_reports_its_state_and_enablement():
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
    assert domain_metadata(undefined, "qemu:///system", "bootwright-lab-rhel-01") == {
        "answered": True, "present": False, "owned": False, "uuid": "",
    }
    for name, answers in {
        "connection refused": {"dumpxml bootwright-lab-rhel-01": CONNECTION_REFUSED, "list --all --name": OTHER_DOMAINS},
        "listing refused": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (1, "", "error: Failed to list domains\n")},
        "listed after all": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (0, "bootwright-lab-rhel-01\n\n", "")},
        "listing truncated": {"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": (0, "x" * (1 << 20), "")},
        "another failure": {"dumpxml bootwright-lab-rhel-01": (1, "", "error: internal error\n"), "list --all --name": OTHER_DOMAINS},
    }.items():
        metadata = domain_metadata(runner_for(answers), "qemu:///system", "bootwright-lab-rhel-01")
        assert metadata == {"answered": False, "present": False, "owned": False, "uuid": ""}, name


def machine_request():
    return {
        "controller": {"unit": "bootwright-lab-bmc-rhel-01"},
        "disks": [],
        "domain": "bootwright-lab-rhel-01",
        "uri": "qemu:///system",
    }


# Both a domain that is not defined and a hypervisor that did not answer report
# no domain; only `answered` says which, and only the first proves absence.
def test_a_machine_observation_reports_whether_the_hypervisor_answered():
    undefined = runner_for({"dumpxml bootwright-lab-rhel-01": LOOKUP_REFUSED, "list --all --name": OTHER_DOMAINS})
    observation = observe_machine(undefined, machine_request())
    assert (observation["answered"], observation["domain"], observation["state"]) == (True, "", "")
    silent = runner_for({"dumpxml bootwright-lab-rhel-01": CONNECTION_REFUSED})
    observation = observe_machine(silent, machine_request())
    assert (observation["answered"], observation["domain"], observation["state"]) == (False, "", "")
    defined = runner_for({
        "dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, ""),
        "domstate bootwright-lab-rhel-01": (0, "running\n", ""),
    })
    observation = observe_machine(defined, machine_request())
    assert (observation["answered"], observation["domain"], observation["state"]) == (True, "bootwright-lab-rhel-01", "running")
