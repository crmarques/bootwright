"""Every template a role renders writes the same bytes twice, and the bytes its golden holds.

A template task that writes other bytes from the same request reports a change
on every replay, and the restart that follows a changed configuration or unit
turns every repeated apply into an outage and every observation into drift.
Each template is rendered twice here for one request shaped as Go freezes it,
the way ansible.builtin.template renders (plugins/action/template.py at
v2.21.4: trim_blocks on, lstrip_blocks off, LF newlines, the template's own
directory as the search path), and the two renderings must be the same bytes.
The golden pins those bytes, so a template change is reviewed as what it
writes. To rewrite the goldens after an intended change, run this module with
BOOTWRIGHT_UPDATE_GOLDENS=1.

Rendering needs Ansible's controller (DataLoader and Templar), which
ansible-test does not offer to unit tests under tests/unit/plugins, so these
checks live here.
"""

from __future__ import annotations

import copy
import os
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
GOLDENS = pathlib.Path(__file__).resolve().parent / "goldens" / "templates"
LOADER = DataLoader()
UPDATE = os.environ.get("BOOTWRIGHT_UPDATE_GOLDENS") == "1"

DIGEST = "0123456789abcdef" * 4
CONTEXT = "lab-rhel"
EGRESS = {"noProxy": ["192.0.2.0/24", "lab.example.test"]}
LOCAL = {"connection": "local", "machine": "controller"}


def service(kind, slug, name, port, **fields):
    """A managed network service request, keyed as managedservice.Request is."""
    request = {
        "bindAddress": "192.0.2.1",
        "contentRoot": "/var/lib/bootwright-services/%s/%s/%s" % (CONTEXT, slug, name),
        "egress": EGRESS,
        "endpoints": [{"address": "192.0.2.1:%d" % port, "name": name}],
        "identity": {"block": "service-" + name, "context": CONTEXT, "service": name},
        "image": "registry.example.test/%s@sha256:%s" % (slug, DIGEST),
        "kind": kind,
        "placement": LOCAL,
        "port": port,
        "unit": "bootwright-%s-%s-%s" % (CONTEXT, slug, name),
        "version": "%s-v1" % slug,
    }
    request.update(fields)
    return request


ARTIFACT_SERVER = {
    "bindAddress": "192.0.2.1",
    "contentRoot": "/var/lib/bootwright-services/%s/artifact-server/media" % CONTEXT,
    "egress": EGRESS,
    "endpoints": [{"address": "http://192.0.2.1:8080", "name": "http"}, {"address": "https://192.0.2.1:8443", "name": "https"}],
    "identity": {"block": "service-media", "context": CONTEXT, "service": "media"},
    "image": "registry.example.test/nginx-124@sha256:" + DIGEST,
    "listeners": [{"name": "http", "port": 8080, "protocol": "http"}, {"name": "https", "port": 8443, "protocol": "https"}],
    "placement": LOCAL,
    "tls": {"fingerprint": DIGEST, "minVersion": "TLSv1.2", "secret": "media-serving"},
    "unit": "bootwright-%s-artifact-server-media" % CONTEXT,
    "version": "artifact-server-nginx-v1",
}

NETWORK = {"address": "192.0.2.1/24", "bridge": "virbr-lab", "forward": "nat", "managed": True,
           "name": "bootwright-lab-guests"}
HOST = {
    "identity": {"block": "provider-host", "context": CONTEXT, "object": "host"},
    "networks": [NETWORK],
    "packages": ["libvirt-daemon-kvm", "qemu-kvm"],
    "placement": LOCAL,
    "poolName": "bootwright-%s-host" % CONTEXT,
    "poolPath": "/var/lib/libvirt/images/bootwright/%s/host/pool" % CONTEXT,
    "provisioned": True,
    "services": ["virtnetworkd.socket", "virtqemud.socket", "virtstoraged.socket"],
    "uri": "qemu:///system",
    "version": "substrate-host-libvirt-v2",
}

MACHINE = {
    "controller": {"address": "192.0.2.1", "credentialsRef": "lab-bmc", "endpoint":
                   "http://192.0.2.1:8001/redfish/v1/Systems/7b9ec716-85d4-8e28-84d3-f0d571d55f15",
                   "image": "registry.example.test/sushy-tools@sha256:" + DIGEST, "port": 8001,
                   "unit": "bootwright-%s-rhel-01-bmc" % CONTEXT},
    "directory": "/var/lib/libvirt/images/bootwright/%s/rhel-01" % CONTEXT,
    "disks": [{"name": "root", "path": "/var/lib/libvirt/images/bootwright/%s/rhel-01/root.qcow2" % CONTEXT,
               "sizeGiB": 40, "target": "vda"},
              {"name": "data", "path": "/var/lib/libvirt/images/bootwright/%s/rhel-01/data.qcow2" % CONTEXT,
               "sizeGiB": 20, "target": "vdb"}],
    "domain": "bootwright-%s-rhel-01" % CONTEXT,
    "identity": {"block": "machine-rhel-01", "context": CONTEXT, "object": "rhel-01"},
    "interfaces": [{"bridge": "virbr-lab", "macAddress": "52:54:00:b9:31:5b", "name": "enp1s0",
                    "network": "bootwright-lab-guests"},
                   {"bridge": "br-external", "macAddress": "52:54:00:b9:31:5c", "name": "enp2s0", "network": ""}],
    "memoryMiB": 8192,
    "placement": LOCAL,
    "poolName": "bootwright-%s-host" % CONTEXT,
    "poolPath": "/var/lib/libvirt/images/bootwright/%s/host/pool" % CONTEXT,
    "tpm": True,
    "uri": "qemu:///system",
    "uuid": "7b9ec716-85d4-8e28-84d3-f0d571d55f15",
    "vcpu": 4,
    "version": "machine-libvirt-v1",
}

# Each rendered template with the variables its task sees beside the role's
# defaults: the request, and the task's own loop item or registered result.
SCOPES = {
    ("infra_artifact_server_nginx", "nginx.conf.j2"): {"bootwright_artifact_server_request": ARTIFACT_SERVER},
    ("infra_artifact_server_nginx", "unit.container.j2"): {"bootwright_artifact_server_request": ARTIFACT_SERVER},
    ("infra_dns_server_dnsmasq", "dnsmasq.conf.j2"): {"bootwright_dns_server_request": service(
        "DNSServer", "dns-server-dnsmasq", "dns", 53, forwarders=["198.51.100.53"], records=[
            {"addresses": ["192.0.2.11"], "name": "rhel-01.lab.example.test"},
            {"addresses": ["192.0.2.21", "192.0.2.22"], "name": "apps.sno.lab.example.test", "subtree": True}])},
    ("infra_dns_server_dnsmasq", "unit.container.j2"): {"bootwright_dns_server_request": service(
        "DNSServer", "dns-server-dnsmasq", "dns", 53)},
    ("infra_ntp_server_chrony", "chrony.conf.j2"): {"bootwright_ntp_server_request": service(
        "NTPServer", "ntp-server-chrony", "time", 123, clients=["192.0.2.0/24"], sources=["198.51.100.123"])},
    ("infra_ntp_server_chrony", "unit.container.j2"): {"bootwright_ntp_server_request": service(
        "NTPServer", "ntp-server-chrony", "time", 123)},
    ("infra_proxy_squid", "squid.conf.j2"): {"bootwright_proxy_request": service(
        "Proxy", "proxy-squid", "egress", 3128, clients=["192.0.2.0/24", "2001:db8::/64"])},
    ("infra_proxy_squid", "unit.container.j2"): {"bootwright_proxy_request": service(
        "Proxy", "proxy-squid", "egress", 3128)},
    ("substrate_libvirt_host", "network.xml.j2"): {
        "bootwright_substrate_host_request": HOST, "item": NETWORK,
        "substrate_libvirt_host_network_uuids": {NETWORK["name"]: "4c0a4300-aa43-458c-86d7-ac2256d1fc00"}},
    ("substrate_libvirt_machine", "domain.xml.j2"): {"bootwright_substrate_machine_request": MACHINE},
    ("substrate_libvirt_machine", "emulator.conf.j2"): {"bootwright_substrate_machine_request": MACHINE},
    ("substrate_libvirt_machine", "unit.container.j2"): {"bootwright_substrate_machine_request": MACHINE},
}


def rendered_templates():
    """Every (role, template) a task renders through ansible.builtin.template."""
    found = set()
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        for task in LOADER.load_from_file(str(path)) or []:
            source = (task.get("ansible.builtin.template") or {}).get("src") if isinstance(task, dict) else None
            if source:
                found.add((path.parent.parent.name, source))
    return found


def scope(role, variables):
    """What one template task sees: the role's defaults, then its own variables."""
    found = dict(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True) or {})
    found.update(copy.deepcopy(variables))
    return found


def render(role, template, variables):
    """One rendering, as ansible.builtin.template renders a source file."""
    source = ROLES / role / "templates" / template
    templar = Templar(loader=LOADER, variables=variables).copy_with_new_env(
        searchpath=[str(source.parent)], available_variables=variables)
    return templar.template(trust_as_template(source.read_text()), escape_backslashes=False,
                            overrides={"trim_blocks": True, "lstrip_blocks": False, "newline_sequence": "\n"})


def test_every_rendered_template_has_a_golden_scope():
    assert rendered_templates() == set(SCOPES)


@pytest.mark.parametrize("role, template", sorted(SCOPES))
def test_a_template_writes_the_same_bytes_twice_and_its_goldens_bytes(role, template):
    first = render(role, template, scope(role, SCOPES[role, template]))
    second = render(role, template, scope(role, SCOPES[role, template]))
    assert first == second, "%s/%s writes other bytes when rendered again" % (role, template)
    golden = GOLDENS / role / template[:-len(".j2")]
    if UPDATE:
        golden.parent.mkdir(parents=True, exist_ok=True)
        golden.write_text(first)
    assert golden.read_text() == first, "%s differs; rerun with BOOTWRIGHT_UPDATE_GOLDENS=1 if intended" % (
        golden.relative_to(GOLDENS).as_posix())


def test_the_golden_comparison_sees_a_change_in_what_a_template_writes():
    variables = scope("infra_proxy_squid", SCOPES["infra_proxy_squid", "squid.conf.j2"])
    written = render("infra_proxy_squid", "squid.conf.j2", variables)
    variables["bootwright_proxy_request"]["clients"] = ["198.51.100.0/24"]
    assert render("infra_proxy_squid", "squid.conf.j2", variables) != written
