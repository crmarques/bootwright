"""The observation is assembled here, so it is tested without a hypervisor."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import (
    domain_metadata,
    invoke,
    network_state,
    observe_host,
    packages_present,
    unit_state,
)

OWNED_NETWORK = """<network>
  <name>bootwright-lab-guests</name>
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


def test_only_a_known_unit_state_is_reported():
    assert unit_state(runner_for({"--value libvirtd.service": (0, "active\n", "")}), "libvirtd.service") == "active"
    assert unit_state(runner_for({"--value libvirtd.service": (0, "bananas\n", "")}), "libvirtd.service") == ""


def test_a_network_without_this_contexts_metadata_is_foreign():
    owned = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, OWNED_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(owned, "qemu:///system", "bootwright-lab-guests") == {
        "state": "active", "owned": True, "bridge": "virbr-lab",
    }
    foreign = runner_for({
        "net-dumpxml bootwright-lab-guests": (0, FOREIGN_NETWORK, ""),
        "net-info bootwright-lab-guests": (0, "Active:         yes\n", ""),
    })
    assert network_state(foreign, "qemu:///system", "bootwright-lab-guests")["owned"] is False


def test_an_absent_or_malformed_network_reports_no_state():
    assert network_state(runner_for({}), "qemu:///system", "gone")["state"] == ""
    malformed = runner_for({"net-dumpxml gone": (0, "not xml", "")})
    assert network_state(malformed, "qemu:///system", "gone") == {"state": "", "owned": False, "bridge": ""}


def test_a_domain_reports_its_identity_and_ownership():
    owned = runner_for({"dumpxml bootwright-lab-rhel-01": (0, OWNED_DOMAIN, "")})
    assert domain_metadata(owned, "qemu:///system", "bootwright-lab-rhel-01") == {
        "present": True, "owned": True, "uuid": "1ab52b3c-0000-8000-8000-000000000000",
    }
    assert domain_metadata(runner_for({}), "qemu:///system", "gone") == {
        "present": False, "owned": False, "uuid": "",
    }


def test_a_host_whose_connection_is_silent_reports_nothing_it_cannot_read():
    request = {
        "networks": [{"name": "bootwright-lab-guests", "bridge": "virbr-lab", "managed": True}],
        "packages": ["qemu-kvm"],
        "poolName": "bootwright-lab-p-vmedia",
        "service": "libvirtd.service",
        "uri": "qemu:///system",
    }
    observation = observe_host(runner_for({}), request)
    assert observation["uri"] is False
    assert observation["pool"] == ""
    assert observation["networks"][0]["state"] == ""
    assert observation["networks"][0]["owned"] is False
