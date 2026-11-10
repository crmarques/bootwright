"""The provider host request and evidence Go freezes for tagged managed Networks, as the collection reads them.

goldens/libvirt_host_tagged.json is written by Go
(TestTheCollectionHoldsItsHostTestsToTheTaggedRequestAndEvidence in
internal/substrate/libvirt): its request is the one the planner freezes for a
provider with two VLAN legs on one trunk and its evidence the completed
evidence Go's ValidateHostPresence accepts. A field either side renames, adds
or drops alone fails here or there. goldens/libvirt_routing_unit.txt is the
routing unit this collection renders for that request; Go holds it byte for
byte to the unit and ruleset specs/substrates.md "Routing table" states
(TestTheRoutingUnitTheCollectionRendersMatchesSpec). Rewrite it after an
intended change with BOOTWRIGHT_UPDATE_GOLDENS=1.
"""

from __future__ import annotations

import copy
import json
import os
import pathlib

from ansible_collections.bootwright.core.plugins.action import substrate_host_protocol
from ansible_collections.bootwright.core.plugins.module_utils import libvirt_firewall_ready
from ansible_collections.bootwright.core.plugins.module_utils import libvirt_nm_connections as nm
from ansible_collections.bootwright.core.plugins.module_utils import libvirt_routing
from ansible_collections.bootwright.core.plugins.module_utils import libvirt_vlan_refusal

GOLDEN = pathlib.Path(__file__).resolve().parent / "goldens" / "libvirt_host_tagged.json"
ROUTING_UNIT = GOLDEN.parent / "libvirt_routing_unit.txt"
UPDATE = os.environ.get("BOOTWRIGHT_UPDATE_GOLDENS") == "1"


def shared():
    return json.loads(GOLDEN.read_text(encoding="utf-8"))


def test_every_frozen_trunk_and_leg_is_accepted_and_rendered_as_an_owned_keyfile():
    request = shared()["request"]
    wanted = nm.wanted(request)
    assert [entry["name"] for entry in wanted] == [trunk["name"] for trunk in request["trunks"]] + [leg["name"] for leg in request["legs"]]
    for entry in wanted:
        groups = nm.parse_keyfile(nm.keyfile(entry))
        assert nm.marked(groups, entry) and nm.carries(groups, entry)
        assert groups["connection"]["uuid"] == entry["uuid"] and groups["user"]["bootwright.io.role"] == entry["role"]
        # The collection derives the UUID Go froze, which proves an owned connection the request no longer freezes.
        assert nm.connection_uuid(request["identity"]["context"], entry["name"]) == entry["uuid"]
        assert nm.owned_identity(groups, request["identity"]["context"], request["identity"]["object"], entry["name"])["device"] == entry["device"]
    assert libvirt_vlan_refusal.unit_name(request) == request["vlanRefusalUnit"]
    assert libvirt_firewall_ready.unit_name(request) == request["firewallReadyUnit"]


def test_the_evidence_go_accepts_is_what_the_protocol_publishes_from_its_observation():
    evidence = shared()["evidence"]
    observation = copy.deepcopy({key: value for key, value in evidence.items() if key not in ("absent", "postcondition", "request")})
    for network in observation["networks"]:
        network["uuid"] = "00000000-0000-4000-8000-000000000000"
    published = substrate_host_protocol.presence(observation, evidence["request"])
    assert published == evidence


def test_the_frozen_routing_table_is_accepted_and_rendered_as_the_unit_go_holds_to_the_spec():
    request = shared()["request"]
    table = libvirt_routing.model(request)
    assert table["unit"] == request["routing"]["unit"] and table["forwarding"] is request["routing"]["forwarding"]
    assert table["routed"] == [(pair["from"], pair["to"]) for pair in request["routing"]["routed"]]
    content = libvirt_routing.unit_content(table)
    if UPDATE:
        ROUTING_UNIT.write_bytes(content)
    assert ROUTING_UNIT.read_bytes() == content
