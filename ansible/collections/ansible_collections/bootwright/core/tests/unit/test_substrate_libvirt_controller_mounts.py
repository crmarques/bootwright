"""A machine's emulated BMC mounts no pool directory.

The controller stores inserted media as a volume in the provider's pool
through its libvirt connection, so its unit mounts its own configuration and
password file read-only and the host's libvirt socket directory, and nothing
else, as the substrates spec states.

Rendering the role's templates over its defaults needs Ansible's controller
(DataLoader and Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_libvirt_machine"
LOADER = DataLoader()

REQUEST = {
    "controller": {
        "address": "198.51.100.1",
        "image": "quay.io/metal3-io/sushy-tools@sha256:" + "0" * 64,
        "port": 8000,
        "unit": "bootwright-lab-bmc-rhel-01",
    },
    "identity": {"context": "lab", "object": "rhel-01"},
    "poolName": "bootwright-lab-hypervisor-vmedia",
    "poolPath": "/var/lib/libvirt/images/bootwright/lab/hypervisor/vmedia",
    "uri": "qemu:///system",
    "uuid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00",
}


def render(template):
    variables = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables["bootwright_substrate_machine_request"] = REQUEST
    templar = Templar(loader=LOADER, variables=variables)
    return str(templar.template(trust_as_template((ROLE / "templates" / template).read_text())))


def settings(text, key):
    return [value for name, _separator, value in (line.partition("=") for line in text.splitlines()) if name.strip() == key]


def test_the_unit_mounts_its_own_files_and_the_libvirt_socket_directory_alone():
    unit = render("unit.container.j2")
    state = "/var/lib/bootwright-substrate/lab/rhel-01"
    assert sorted(settings(unit, "Volume")) == sorted([
        state + "/conf.py:/etc/sushy/conf.py:ro",
        state + "/htpasswd:/etc/sushy/htpasswd:ro",
        "/run/libvirt:/run/libvirt:rw",
    ])
    assert settings(unit, "Mount") == []
    assert REQUEST["poolPath"] not in unit


def test_the_controller_reaches_the_pool_by_name_through_its_libvirt_connection():
    configuration = render("emulator.conf.j2")
    assert settings(configuration, "SUSHY_EMULATOR_LIBVIRT_URI") == [' "qemu:///system"']
    assert settings(configuration, "SUSHY_EMULATOR_STORAGE_POOL") == [' "%s"' % REQUEST["poolName"]]
    assert REQUEST["poolPath"] not in configuration
