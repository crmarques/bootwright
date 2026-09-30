"""A managed network definition is offered back under the identity it holds.

libvirt refuses `net-define` for a name that already exists under a different
UUID, so a definition that omits one is accepted on a host that has never seen
the network and refused on every apply after that. The template is rendered
here because neither the syntax check nor ansible-lint can see the collision.
"""

from __future__ import annotations

import pathlib
from xml.etree import ElementTree

import jinja2

from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import carries_definition

TEMPLATE = (
    pathlib.Path(__file__).resolve().parents[2]
    / "roles/substrate_libvirt_host/templates/network.xml.j2"
)

NETWORK = {"name": "bootwright-lab-guests", "bridge": "virbr-lab", "address": "198.51.100.1/24", "managed": True}
REQUEST = {"identity": {"context": "lab-rhel"}}


def render(uuids, network=None):
    environment = jinja2.Environment(
        loader=jinja2.FileSystemLoader(str(TEMPLATE.parent)),
        trim_blocks=False,
        keep_trailing_newline=True,
    )
    return environment.get_template(TEMPLATE.name).render(
        item=network or NETWORK,
        bootwright_substrate_host_request=REQUEST,
        substrate_libvirt_host_network_uuids=uuids,
    )


def test_a_known_identity_is_written_into_the_definition():
    root = ElementTree.fromstring(render({NETWORK["name"]: "4c0a4300-aa43-458c-86d7-ac2256d1fc00"}))
    assert root.findtext("./uuid") == "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
    assert root.findtext("./name") == NETWORK["name"]


def test_an_unknown_identity_leaves_the_element_out_for_libvirt_to_assign():
    root = ElementTree.fromstring(render({}))
    assert root.find("./uuid") is None
    assert root.findtext("./name") == NETWORK["name"]


def test_the_definition_still_carries_everything_the_host_block_depends_on():
    root = ElementTree.fromstring(render({NETWORK["name"]: "4c0a4300-aa43-458c-86d7-ac2256d1fc00"}))
    bridge = root.find("./bridge")
    assert bridge.get("name") == "virbr-lab"
    # The guests exist to reach the controller's services, and the zone libvirt
    # would otherwise choose rejects them.
    assert bridge.get("zone") == "trusted"
    assert root.find("./dns").get("enable") == "no"
    address = root.find("./ip")
    assert (address.get("address"), address.get("prefix")) == ("198.51.100.1", "24")
    owner = root.find("./metadata/{https://bootwright.io/substrate/v1}owner")
    assert owner is not None


# The observation decides whether a network needs defining by comparing what
# the host carries with what this template writes, so the two must agree: a
# definition the comparison refused would be defined again on every replay.
def test_the_observation_reads_every_rendered_definition_as_carried():
    for forward in ("", "none", "nat"):
        network = dict(NETWORK, forward=forward)
        for uuids in ({}, {NETWORK["name"]: "4c0a4300-aa43-458c-86d7-ac2256d1fc00"}):
            root = ElementTree.fromstring(render(uuids, network))
            assert carries_definition(root, network, REQUEST["identity"]["context"]), forward
        assert not carries_definition(ElementTree.fromstring(render({}, network)), dict(network, bridge="virbr-other"), "lab-rhel")
    nat = ElementTree.fromstring(render({}, dict(NETWORK, forward="nat")))
    assert not carries_definition(nat, dict(NETWORK, forward="none"), "lab-rhel")
    assert not carries_definition(nat, dict(NETWORK, forward="nat"), "another-context")
