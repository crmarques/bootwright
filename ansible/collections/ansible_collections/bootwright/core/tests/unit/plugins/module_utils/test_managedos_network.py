"""Network convergence reads one guest state, takes one step and compares a subset.

The checkpoint shapes follow nmstatectl(8) 2.2.62, whose --no-commit prints the
checkpoint as the last line of its output
(https://www.mankier.com/8/nmstatectl), and busctl(1) get-property, which
prints an object-path array as its signature, its count and each quoted path
(https://www.freedesktop.org/software/systemd/man/latest/busctl.html).
"""

from __future__ import annotations

import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.managedos_network import (
    KEYS,
    bounded_differences,
    checkpoint_from_log,
    compare,
    guest_state,
    listed_checkpoints,
    next_step,
)

D = "a" * 64
OTHER = "b" * 64
B1 = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
B0 = "11111111-2222-3333-4444-555555555555"
C = "/org/freedesktop/NetworkManager/Checkpoint/3"


def read(**values):
    fields = {"nmstatectl": "yes", "bootId": B1, "unit": "inactive", "rc": "", "checkpoint": "",
              "listed": "ao 0", "marker": "", "error": ""}
    fields.update(values)
    return "".join("%s=%s\n" % (key, fields[key]) for key in KEYS)


def marker(boot, request):
    return json.dumps({"bootId": boot, "request": request})


MATRIX = {
    "nothing applied yet": (read(), "apply"),
    "the unit is active": (read(unit="active"), "wait"),
    "the unit is activating": (read(unit="activating"), "wait"),
    "a checkpoint still listed": (read(checkpoint=C, listed='ao 1 "%s"' % C), "rollback"),
    "a checkpoint no longer listed": (read(checkpoint=C, listed="ao 0"), "apply"),
    "committed in this boot": (read(marker=marker(B1, D)), "reboot"),
    "committed before this boot": (read(marker=marker(B0, D)), "inspect"),
    "power lost before anything was written": (read(rc="", unit="failed"), "apply"),
    "a marker of another request": (read(marker=marker(B1, OTHER)), "apply"),
    "a checkpoint whose listing is unreadable": (read(checkpoint=C, listed="garbage"), "unreadable"),
    "an invalid read": ("bootId=%s\n" % B1, "unreadable"),
}


@pytest.mark.parametrize("stdout,step", list(MATRIX.values()), ids=list(MATRIX))
def test_next_step_interruption_matrix(stdout, step):
    assert next_step(guest_state(stdout), D) == step


def test_a_committed_marker_wins_over_a_running_unit():
    assert next_step(guest_state(read(unit="active", marker=marker(B0, D))), D) == "inspect"


def test_checkpoint_from_log_accepts_only_the_nm_path():
    assert checkpoint_from_log("[INFO] applying\n[INFO] verified\n%s\n" % C) == C
    assert checkpoint_from_log("log\n%s   \n\n" % C) == C
    for refused in (
        "/org/freedesktop/NetworkManager/Checkpoint/1x",
        "/org/freedesktop/NetworkManager/Checkpoint/",
        "%s\n[INFO] after" % C,
        "",
        "/org/freedesktop/NetworkManager/Checkpoint/1;rm",
    ):
        assert checkpoint_from_log(refused) == ""


def test_listed_checkpoints_reads_only_the_busctl_array():
    second = "/org/freedesktop/NetworkManager/Checkpoint/12"
    assert listed_checkpoints("ao 0") == []
    assert listed_checkpoints('ao 2 "%s" "%s"\n' % (second, C)) == sorted([second, C])
    for refused in ('ao 2 "%s"' % C, 'ao 1 "/org/example/Other/1"', 'as 1 "x"', "", None):
        assert listed_checkpoints(refused) is None


@pytest.mark.parametrize("stdout,named", [
    (read().replace("bootId=%s\n" % B1, ""), "bootId"),
    (read() + "unit=active\n", "unit"),
    (read() + "extra=1\n", "extra"),
    (read(bootId="not-a-boot-id"), "bootId"),
], ids=["missing", "duplicate", "unknown", "not a uuid"])
def test_guest_state_refuses_a_malformed_read(stdout, named):
    state = guest_state(stdout)
    assert state["valid"] is False
    assert named in state["reason"]
    assert set(state) >= {"valid", "reason"} | set(KEYS) | {"running"}


def test_guest_state_reads_every_field():
    state = guest_state(read(rc="0", unit="active", checkpoint="x", error="bad\x1b[0m"))
    assert state["valid"] and state["nmstatectl"] and state["running"] and state["rc"] == 0
    assert state["listed"] == [] and state["marker"] == {} and state["error"] == "bad[0m"


def desired_document():
    return {
        "interfaces": [
            {"name": "nic0", "type": "ethernet", "state": "up"},
            {"name": "nic1", "type": "ethernet", "state": "up"},
            {"name": "bond0", "type": "bond", "state": "up", "mtu": 1400,
             "link-aggregation": {"mode": "active-backup", "port": ["nic0", "nic1"], "options": {"miimon": 100}}},
            {"name": "bond0.130", "type": "vlan", "state": "up", "vlan": {"base-iface": "bond0", "id": 130},
             "ipv4": {"enabled": True, "dhcp": False, "address": [{"ip": "198.51.100.11", "prefix-length": 24}]}},
            {"name": "nic2", "type": "ethernet", "state": "up",
             "ipv4": {"enabled": True, "dhcp": False, "address": [{"ip": "203.0.113.5", "prefix-length": 24}]},
             "ipv6": {"enabled": False}},
        ],
        "routes": {"config": [
            {"next-hop-interface": "bond0.130", "state": "absent"},
            {"next-hop-interface": "nic2", "state": "absent"},
            {"destination": "0.0.0.0/0", "next-hop-address": "198.51.100.1", "next-hop-interface": "bond0.130"},
            {"destination": "192.0.2.0/24", "next-hop-address": "203.0.113.1", "next-hop-interface": "nic2",
             "metric": 100},
        ]},
        "dns-resolver": {"config": {"server": ["198.51.100.53"], "search": ["example.test"]}},
    }


def running_document():
    return {
        "hostname": {"running": "node.example.test"},
        "interfaces": [
            {"name": "lo", "type": "loopback", "state": "up"},
            {"name": "nic0", "type": "ethernet", "state": "up", "mtu": 1400, "controller": "bond0"},
            {"name": "nic1", "type": "ethernet", "state": "up", "mtu": 1400, "controller": "bond0"},
            {"name": "bond0", "type": "bond", "state": "up", "mtu": 1400, "mac-address": "02:00:00:00:00:01",
             "link-aggregation": {"mode": "active-backup", "port": ["nic1", "nic0"],
                                  "options": {"miimon": "100", "updelay": 0}}},
            {"name": "bond0.130", "type": "vlan", "state": "up", "mtu": 1400,
             "vlan": {"base-iface": "bond0", "id": 130, "protocol": "802.1q"},
             "ipv4": {"enabled": True, "dhcp": False, "address": [{"ip": "198.51.100.11", "prefix-length": 24}]},
             "ipv6": {"enabled": True, "autoconf": False, "dhcp": False,
                      "address": [{"ip": "fe80::1", "prefix-length": 64}]}},
            {"name": "nic2", "type": "ethernet", "state": "up", "mtu": 1500,
             "ipv4": {"enabled": True, "dhcp": False, "address": [{"ip": "203.0.113.5", "prefix-length": 24}]},
             "ipv6": {"enabled": False}},
            {"name": "virbr0", "type": "linux-bridge", "state": "up"},
        ],
        "routes": {
            "running": [{"destination": "198.51.100.0/24", "next-hop-interface": "bond0.130", "table-id": 254}],
            "config": [
                {"destination": "0.0.0.0/0", "next-hop-address": "198.51.100.1", "next-hop-interface": "bond0.130",
                 "metric": 300, "table-id": 254},
                {"destination": "192.0.2.0/24", "next-hop-address": "203.0.113.1", "next-hop-interface": "nic2",
                 "metric": 100, "table-id": 254},
                {"destination": "192.168.122.0/24", "next-hop-interface": "virbr0"},
            ],
        },
        "dns-resolver": {"running": {"server": ["198.51.100.53"]},
                         "config": {"server": ["198.51.100.53"], "search": ["example.test"]}},
    }


def test_compare_matches_declared_subset():
    assert compare(desired_document(), running_document()) == []


def test_compare_names_each_difference_bounded():
    running = running_document()
    interfaces = {item["name"]: item for item in running["interfaces"]}
    interfaces["bond0"]["mtu"] = 1500
    interfaces["bond0"]["link-aggregation"]["port"] = ["nic0", "nic2"]
    running["routes"]["config"].pop(1)
    running["routes"]["config"].append({"destination": "10.0.0.0/8", "next-hop-interface": "nic2"})
    running["dns-resolver"]["config"]["server"] = ["198.51.100.54", "198.51.100.53"]
    desired = desired_document()
    desired["dns-resolver"]["config"]["server"] = ["198.51.100.53", "198.51.100.54"]
    found = compare(desired, running)
    assert found == [
        "interface bond0: mtu is 1500, declared 1400",
        "interface bond0: link-aggregation ports are [nic0, nic2], declared [nic0, nic1]",
        "route 192.0.2.0/24 via 203.0.113.1 on nic2: not configured",
        "route 10.0.0.0/8 via - on nic2: configured but not declared",
        "dns server is [198.51.100.54, 198.51.100.53], declared [198.51.100.53, 198.51.100.54]",
    ]
    desired = {"interfaces": [{"name": "gone%02d" % index + "\x1b" * 200, "state": "absent"} for index in range(20)]}
    present = {"interfaces": [{"name": item["name"]} for item in desired["interfaces"]]}
    found = compare(desired, present)
    assert len(found) == 20
    bounded = bounded_differences(found)
    assert len(bounded) == 16 and bounded[0].startswith("interface gone00????")
    assert all(len(entry.encode("utf-8")) <= 200 and entry.isascii() and entry.isprintable() for entry in bounded)


def test_compare_compares_ports_as_a_set():
    running = running_document()
    assert running["interfaces"][3]["link-aggregation"]["port"] == ["nic1", "nic0"]
    assert compare(desired_document(), running) == []


def test_differences_are_bounded_in_count_bytes_and_characters():
    long_name = "\x1b" + "n" * 300 + "é"
    differences = ["interface %s%d: not present" % (long_name, index) for index in range(20)]
    bounded = bounded_differences(differences)
    assert len(bounded) == 16
    for entry in bounded:
        assert len(entry.encode("utf-8")) <= 200
        assert all(" " <= character <= "~" for character in entry)
    assert bounded[0].startswith("interface ?nnn")


def test_compare_ignores_link_local_ipv6():
    desired = desired_document()
    desired["interfaces"][3]["ipv6"] = {"enabled": True, "autoconf": False, "dhcp": False,
                                        "address": [{"ip": "2001:db8::11", "prefix-length": 64}]}
    running = running_document()
    running["interfaces"][4]["ipv6"]["address"].append({"ip": "2001:0db8:0::11", "prefix-length": 64})
    assert compare(desired, running) == []
    running["interfaces"][4]["ipv6"]["address"] = [{"ip": "fe80::1", "prefix-length": 64}]
    assert compare(desired, running) == [
        "interface bond0.130: ipv6 addresses are [], declared [2001:db8::11/64]",
    ]


def test_compare_refuses_an_absent_interface_that_is_present():
    desired = desired_document()
    desired["interfaces"].append({"name": "virbr0", "type": "linux-bridge", "state": "absent"})
    desired["interfaces"].append({"name": "gone0", "state": "absent"})
    assert compare(desired, running_document()) == ["interface virbr0: declared absent but present"]


def test_compare_names_vlan_bond_and_family_fields():
    running = copy.deepcopy(running_document())
    interfaces = {item["name"]: item for item in running["interfaces"]}
    interfaces["bond0"]["link-aggregation"]["mode"] = "802.3ad"
    interfaces["bond0"]["link-aggregation"]["options"]["miimon"] = 200
    interfaces["bond0.130"]["vlan"] = {"base-iface": "nic0", "id": "131"}
    interfaces["nic2"]["ipv6"] = {"enabled": True}
    interfaces["nic1"]["state"] = "down"
    running["dns-resolver"]["config"]["search"] = []
    assert compare(desired_document(), running) == [
        "interface nic1: state is down, declared up",
        "interface bond0: link-aggregation mode is 802.3ad, declared active-backup",
        "interface bond0: link-aggregation option miimon is 200, declared 100",
        "interface bond0.130: vlan base-iface is nic0, declared bond0",
        "interface bond0.130: vlan id is 131, declared 130",
        "interface nic2: ipv6 enabled is true, declared false",
        "dns search is [], declared [example.test]",
    ]


def test_a_running_network_without_interfaces_is_one_difference():
    assert compare(desired_document(), {"routes": {}}) == ["the running network lists no interfaces"]
    assert compare(desired_document(), []) == ["the running network lists no interfaces"]


def running_route(document, destination):
    return next(item for item in document["routes"]["config"] if item["destination"] == destination)


def wrong_gateway(desired, running):
    running_route(running, "0.0.0.0/0")["next-hop-address"] = "198.51.100.2"


def wrong_metric(desired, running):
    running_route(running, "192.0.2.0/24")["metric"] = 101


def wrong_table(desired, running):
    running_route(running, "0.0.0.0/0")["table-id"] = 100


def dhcp_address(desired, running):
    desired["interfaces"][4]["ipv4"].update(dhcp=True, address=[{"ip": "203.0.113.9", "prefix-length": 24}])


def autoconf_address(desired, running):
    desired["interfaces"][3]["ipv6"] = {"enabled": True, "autoconf": True,
                                        "address": [{"ip": "2001:db8::11", "prefix-length": 64}]}


def down_interface(desired, running):
    desired["interfaces"].append({"name": "nic9", "type": "ethernet", "state": "down"})


def running_named(running, name):
    return next(item for item in running["interfaces"] if item["name"] == name)


def vlan_as_ethernet(desired, running):
    running_named(running, "bond0.130")["type"] = "ethernet"


def other_vlan_protocol(desired, running):
    desired["interfaces"][3]["vlan"]["protocol"] = "802.1ad"


def route_on_another_interface(desired, running):
    running_route(running, "192.0.2.0/24")["next-hop-interface"] = "bond0.130"


def same_name_of_another_type_first(desired, running):
    running["interfaces"].insert(0, {"name": "bond0", "type": "ovs-interface", "state": "down"})


def reversed_search(desired, running):
    desired["dns-resolver"]["config"]["search"] = ["example.test", "lab.example.test"]
    running["dns-resolver"]["config"]["search"] = ["lab.example.test", "example.test"]


def uncanonical_server(desired, running):
    desired["dns-resolver"]["config"]["server"] = ["2001:db8::0:53"]
    running["dns-resolver"]["config"]["server"] = ["2001:db8::53"]


ONE_FIELD = {
    "a running route via another gateway": (wrong_gateway, [
        "route 0.0.0.0/0 via 198.51.100.1 on bond0.130: not configured",
        "route 0.0.0.0/0 via 198.51.100.2 on bond0.130: configured but not declared",
    ]),
    "a running route of another metric": (wrong_metric, [
        "route 192.0.2.0/24 via 203.0.113.1 on nic2: not configured",
        "route 192.0.2.0/24 via 203.0.113.1 on nic2: configured but not declared",
    ]),
    "a running route in another table": (wrong_table, [
        "route 0.0.0.0/0 via 198.51.100.1 on bond0.130: not configured",
        "route 0.0.0.0/0 via 198.51.100.1 on bond0.130: configured but not declared",
    ]),
    "a dhcp family with an address the running side lacks": (dhcp_address, [
        "interface nic2: ipv4 dhcp is false, declared true",
    ]),
    "an autoconf family with an address the running side lacks": (autoconf_address, [
        "interface bond0.130: ipv6 autoconf is false, declared true",
    ]),
    "an interface declared down and missing": (down_interface, []),
    "a vlan running as another type": (vlan_as_ethernet, [
        "interface bond0.130: type is ethernet, declared vlan",
    ]),
    "a vlan running another protocol": (other_vlan_protocol, [
        "interface bond0.130: vlan protocol is 802.1q, declared 802.1ad",
    ]),
    "a running route on another interface": (route_on_another_interface, [
        "route 192.0.2.0/24 via 203.0.113.1 on nic2: not configured",
        "route 192.0.2.0/24 via 203.0.113.1 on bond0.130: configured but not declared",
    ]),
    "a running name shared by another type first": (same_name_of_another_type_first, []),
    "a running search list in another order": (reversed_search, [
        "dns search is [lab.example.test, example.test], declared [example.test, lab.example.test]",
    ]),
    "a declared server in another spelling": (uncanonical_server, []),
}


@pytest.mark.parametrize("change,expected", list(ONE_FIELD.values()), ids=list(ONE_FIELD))
def test_compare_names_one_changed_field(change, expected):
    desired, running = desired_document(), running_document()
    change(desired, running)
    assert compare(desired, running) == expected
