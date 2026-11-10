"""The trunks and VLAN legs are owned NetworkManager profiles that replay safely, refuse collisions and leave in order."""

from __future__ import annotations

import copy
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_nm_connections as nm
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import host_conflicts

TRUNK = "bootwright-lab-host-trunk-br-lab"
PUBLIC = "bootwright-lab-ceph-public"
ARBITER = "bootwright-lab-ceph-arbiter"
UUIDS = {TRUNK: "0b1c2d3e-0000-8000-8000-000000000001", PUBLIC: "0b1c2d3e-0000-8000-8000-000000000002",
         ARBITER: "0b1c2d3e-0000-8000-8000-000000000003"}
REQUEST = {
    "identity": {"context": "lab", "object": "host"},
    "uri": "qemu:///system",
    "trunks": [{"bridge": "br-lab", "mac": "52:54:00:aa:bb:01", "name": TRUNK, "uuid": UUIDS[TRUNK]}],
    "legs": [
        {"address": "198.51.100.1/26", "device": "br-lab.110", "forward": "nat", "name": PUBLIC,
         "network": "ceph-public", "trunk": "br-lab", "uuid": UUIDS[PUBLIC], "vlan": 110},
        {"address": "198.51.100.65/26", "device": "br-lab.150", "name": ARBITER,
         "network": "ceph-arbiter", "trunk": "br-lab", "uuid": UUIDS[ARBITER], "vlan": 150},
    ],
}

TRUNK_KEYFILE = """[connection]
id=bootwright-lab-host-trunk-br-lab
uuid=0b1c2d3e-0000-8000-8000-000000000001
type=bridge
interface-name=br-lab
autoconnect=true
zone=trusted

[bridge]
mac-address=52:54:00:AA:BB:01
stp=false
vlan-filtering=false

[ipv4]
method=disabled

[ipv6]
method=disabled

[user]
bootwright.io.context=lab
bootwright.io.network=br-lab
bootwright.io.provider=host
bootwright.io.role=trunk
"""

PUBLIC_KEYFILE = """[connection]
id=bootwright-lab-ceph-public
uuid=0b1c2d3e-0000-8000-8000-000000000002
type=vlan
interface-name=br-lab.110
autoconnect=true
zone=trusted

[vlan]
id=110
parent=br-lab

[ipv4]
address1=198.51.100.1/26
method=manual
never-default=true

[ipv6]
method=disabled

[user]
bootwright.io.context=lab
bootwright.io.network=ceph-public
bootwright.io.provider=host
bootwright.io.role=leg
"""

# How nmcli reports a keyfile value it loaded: booleans as yes and no, the
# address as ipv4.addresses.
SHOWN = {("connection", "autoconnect"): "connection.autoconnect", ("ipv4", "address1"): "ipv4.addresses"}


def shown_value(value):
    return {"true": "yes", "false": "no"}.get(value, value)


class NetworkManager:
    """A host whose NetworkManager loads keyfiles by path, activates them and creates their software devices."""

    def __init__(self, root, net):
        self.root, self.net = root, net
        self.profiles = {}
        self.calls = []
        self.state = "active"
        self.fail = ""
        self.sticky = set()
        self.routes = []

    def profile(self, name, uuid, kind, device, filename="", active=False, properties=None):
        self.profiles[uuid] = {"name": name, "uuid": uuid, "type": kind, "interface": device, "filename": filename,
                               "active": active, "properties": dict(properties or {})}

    def load(self, path):
        groups = nm.parse_keyfile(open(path, "rb").read())
        connection = groups["connection"]
        properties = {"ipv4.gateway": ""}
        for section, values in groups.items():
            if section != "user":
                for key, value in values.items():
                    properties[SHOWN.get((section, key), "%s.%s" % (section, key))] = shown_value(value)
        previous = self.profiles.get(connection["uuid"], {})
        self.profile(connection["id"], connection["uuid"], connection["type"], connection.get("interface-name", ""), path,
                     previous.get("active", False), properties)

    def listing(self):
        def escaped(value):
            return value.replace("\\", "\\\\").replace(":", "\\:")
        return "".join("%s\n" % ":".join(escaped(value) for value in (
            profile["name"], profile["uuid"], profile["type"], profile["interface"] if profile["active"] else "",
            "yes" if profile["active"] else "no", profile["filename"])) for profile in self.profiles.values())

    def run(self, argv, **kwargs):
        assert kwargs["check_rc"] is False
        self.calls.append(argv)
        if argv[0] == nm.SYSTEMCTL:
            return 0, "LoadState=%s\nActiveState=%s\n" % ("not-found" if self.state == "missing" else "loaded", self.state), ""
        if argv[0] == "/usr/sbin/ip":
            return self.ip(argv)
        if argv[0] == "/usr/bin/virsh":
            return 0, "", ""
        words = argv[1:]
        if self.fail and self.fail in words:
            return 1, "", "Error: injected"
        if words[-2:] == ["connection", "show"]:
            return 0, self.listing(), ""
        if words[:2] == ["-t", "-f"] and words[-2] == "uuid":
            profile = self.profiles.get(words[-1])
            if profile is None:
                return 10, "", "Error: no such connection profile."
            values = dict(profile["properties"], **{"connection.interface-name": profile["interface"]})
            fields = words[2].split(",")
            return 0, "".join("%s:%s\n" % (key, value) for key, value in sorted(values.items())
                              if key in fields or key.split(".")[0] in fields), ""
        if words[:2] == ["connection", "load"]:
            self.load(words[2])
            return 0, "", ""
        if words[2:4] == ["connection", "up"]:
            profile = self.profiles[words[-1]]
            profile["active"] = True
            (self.net / profile["interface"]).mkdir(exist_ok=True)
            return 0, "Connection successfully activated", ""
        if words[2:4] == ["connection", "delete"]:
            profile = self.profiles.pop(words[-1])
            if profile["filename"] and os.path.lexists(profile["filename"]):
                os.unlink(profile["filename"])
            if profile["active"] and (self.net / profile["interface"]).exists() and profile["interface"] not in self.sticky:
                (self.net / profile["interface"]).rmdir()
            return 0, "", ""
        raise AssertionError(argv)

    def ip(self, argv):
        if argv[-2:] == ["link", "show"]:
            return 0, json.dumps([{"ifname": name} for name in sorted(os.listdir(self.net))]), ""
        family = 4 if "-4" in argv else 6
        return 0, json.dumps([route for route in self.routes if (":" in route["dst"]) == (family == 6)]), ""

    def effects(self):
        return [call for call in self.calls if call[0] == nm.NMCLI and ("load" in call or "up" in call or "delete" in call)]


@pytest.fixture
def host(tmp_path, monkeypatch):
    connections, net = tmp_path / "system-connections", tmp_path / "net"
    connections.mkdir(mode=0o700)
    net.mkdir()
    monkeypatch.setattr(nm, "executable", lambda path: True)
    monkeypatch.setattr(nm, "NET", str(net))
    return NetworkManager(connections, net)


def manage(host, state, request=None, guests=None):
    plugged = guests if guests is not None else {}

    def conflicts(networks, ours):
        return host_conflicts(host.run, networks, ours)

    return nm.manage(host.run, request or REQUEST, state, lambda bridge: plugged.get(bridge, []), conflicts,
                     str(host.root), os.geteuid(), sleep=lambda seconds: None)


def observe(host, request=None):
    return nm.observe(host.run, request or REQUEST, str(host.root), os.geteuid())


def held(name, role, device, **fields):
    return dict({"name": name, "role": role, "device": device, "answered": True, "present": True, "owned": True,
                 "active": True, "definition": True, "zone": "trusted"}, **fields)


HELD = [held(TRUNK, "trunk", "br-lab"), held(PUBLIC, "leg", "br-lab.110"), held(ARBITER, "leg", "br-lab.150")]


def test_apply_publishes_the_trunk_then_the_legs_and_a_replay_changes_nothing(host):
    assert manage(host, "present")
    assert (host.root / (TRUNK + ".nmconnection")).read_text() == TRUNK_KEYFILE
    assert (host.root / (PUBLIC + ".nmconnection")).read_text() == PUBLIC_KEYFILE
    assert all(oct(os.stat(host.root / (name + ".nmconnection")).st_mode & 0o777) == "0o600" for name in UUIDS)
    activated = [call[-1] for call in host.effects() if "up" in call]
    assert activated == [UUIDS[TRUNK], UUIDS[PUBLIC], UUIDS[ARBITER]]
    loads = [index for index, call in enumerate(host.calls) if "load" in call]
    ups = [index for index, call in enumerate(host.calls) if "up" in call]
    assert loads[0] < ups[0] < loads[1]
    observed = observe(host)
    assert observed["connections"] == HELD and observed["networkManager"] and observed["refusals"] == []
    assert observed["devices"] == ["br-lab", "br-lab.110", "br-lab.150"]
    host.calls.clear()
    assert not manage(host, "present")
    assert host.effects() == []


def test_the_leg_holds_its_address_without_a_default_route_and_no_ipv6():
    leg = nm.wanted(REQUEST)[1]
    assert dict(leg["sections"])["ipv4"] == [("address1", "198.51.100.1/26"), ("method", "manual"), ("never-default", "true")]
    assert dict(leg["sections"])["ipv6"] == [("method", "disabled")]
    assert leg["properties"]["ipv4.gateway"] == "" and leg["properties"]["vlan.parent"] == "br-lab"


@pytest.mark.parametrize("change", ["name", "type", "device", "filename", "user", "missing user key"])
def test_ownership_needs_the_uuid_name_type_device_file_and_every_user_data_key(host, change):
    manage(host, "present")
    profile = host.profiles[UUIDS[PUBLIC]]
    path = host.root / (PUBLIC + ".nmconnection")
    if change == "name":
        profile["name"] = "someone-else"
    elif change == "type":
        profile["type"] = "bridge"
    elif change == "device":
        profile["interface"] = "br-lab.111"
    elif change == "filename":
        profile["filename"] = "/run/NetworkManager/system-connections/x.nmconnection"
    elif change == "user":
        path.write_text(PUBLIC_KEYFILE.replace("bootwright.io.context=lab", "bootwright.io.context=other"))
    else:
        path.write_text(PUBLIC_KEYFILE.replace("bootwright.io.role=leg\n", ""))
    host.calls.clear()
    with pytest.raises(ValueError, match="not this provider's"):
        manage(host, "present")
    assert host.effects() == []
    assert [entry["owned"] for entry in observe(host)["connections"]] == [True, False, True]


@pytest.mark.parametrize("collision", ["name", "uuid", "interface", "keyfile", "link", "route", "unanswered", "not ready", "no nmcli"])
def test_each_collision_refuses_before_any_effect(host, monkeypatch, collision):
    if collision == "name":
        host.profile(PUBLIC, "ffffffff-0000-8000-8000-000000000009", "vlan", "br-lab.110")
    elif collision == "uuid":
        host.profile("foreign", UUIDS[ARBITER], "vlan", "br-lab.150")
    elif collision == "interface":
        host.profile("hand-made", "ffffffff-0000-8000-8000-00000000000a", "802-3-ethernet", "br-lab")
    elif collision == "keyfile":
        (host.root / (TRUNK + ".nmconnection")).write_text("[connection]\nid=other\n")
        (host.root / (TRUNK + ".nmconnection")).chmod(0o600)
    elif collision == "link":
        (host.net / "br-lab.150").mkdir()
    elif collision == "route":
        host.routes.append({"dst": "198.51.100.0/26", "dev": "eth1"})
    elif collision == "unanswered":
        host.fail = "NAME,UUID,TYPE,DEVICE,ACTIVE,FILENAME"
    elif collision == "not ready":
        host.state = "inactive"
    else:
        monkeypatch.setattr(nm, "executable", lambda path: False)
    with pytest.raises(ValueError):
        manage(host, "present")
    assert host.effects() == []
    assert sorted(os.listdir(host.root)) == ([TRUNK + ".nmconnection"] if collision == "keyfile" else [])


def test_a_drifted_trunk_a_machine_runs_on_refuses_and_an_idle_one_is_reactivated_with_its_legs(host):
    manage(host, "present")
    path = host.root / (TRUNK + ".nmconnection")
    path.write_text(TRUNK_KEYFILE.replace("stp=false", "stp=true"))
    host.load(str(path))
    assert observe(host)["trunks"] == [{"name": TRUNK, "bridge": "br-lab", "present": True, "drifted": True}]
    host.calls.clear()
    with pytest.raises(ValueError, match="Machine rhel-01"):
        manage(host, "present", guests={"br-lab": ["Machine rhel-01"]})
    assert host.effects() == []
    with pytest.raises(ValueError, match="did not answer"):
        nm.manage(host.run, REQUEST, "present", lambda bridge: None, lambda networks, ours: ([], [], True), str(host.root), os.geteuid())
    # NetworkManager takes a leg's device away with its activation.
    for uuid in (UUIDS[PUBLIC], UUIDS[ARBITER]):
        host.profiles[uuid]["active"] = False
        (host.net / host.profiles[uuid]["interface"]).rmdir()
    assert manage(host, "present")
    assert path.read_text() == TRUNK_KEYFILE
    assert [call[-1] for call in host.effects() if "up" in call] == [UUIDS[TRUNK], UUIDS[PUBLIC], UUIDS[ARBITER]]
    assert observe(host)["connections"] == HELD


def test_a_drifted_leg_is_published_again_and_reactivated(host):
    manage(host, "present")
    path = host.root / (PUBLIC + ".nmconnection")
    path.write_text(PUBLIC_KEYFILE.replace("198.51.100.1/26", "198.51.100.2/26"))
    host.load(str(path))
    assert observe(host)["connections"][1]["definition"] is False
    host.calls.clear()
    assert manage(host, "present", guests={"br-lab": ["Machine rhel-01"]})
    assert [call[-1] for call in host.effects() if "up" in call] == [UUIDS[PUBLIC]]
    assert path.read_text() == PUBLIC_KEYFILE


def test_removal_takes_the_legs_before_the_trunk_and_proves_every_one_gone(host):
    manage(host, "present")
    host.calls.clear()
    assert manage(host, "absent")
    deleted = [call[-1] for call in host.effects() if "delete" in call]
    assert deleted == [UUIDS[PUBLIC], UUIDS[ARBITER], UUIDS[TRUNK]]
    assert os.listdir(host.root) == [] and os.listdir(host.net) == []
    assert [entry["present"] for entry in observe(host)["connections"]] == [False, False, False]
    assert not manage(host, "absent")


def test_removal_refuses_while_a_machine_runs_on_the_trunk(host):
    manage(host, "present")
    host.calls.clear()
    with pytest.raises(ValueError, match="Machine rhel-01"):
        manage(host, "absent", guests={"br-lab": ["Machine rhel-01"]})
    assert host.effects() == []


def test_removal_never_deletes_a_foreign_profile_holding_a_frozen_uuid(host):
    host.profile("foreign", UUIDS[ARBITER], "vlan", "br-lab.150")
    with pytest.raises(ValueError, match="not this provider's"):
        manage(host, "absent")
    assert host.effects() == [] and UUIDS[ARBITER] in host.profiles


def test_removal_that_leaves_a_device_refuses(host):
    manage(host, "present")
    host.sticky.add("br-lab.110")
    with pytest.raises(ValueError, match="device remains"):
        manage(host, "absent")


def test_a_host_without_networkmanager_holds_no_connection(host, monkeypatch):
    host.state = "missing"
    monkeypatch.setattr(nm, "executable", lambda path: False)
    observed = observe(host)
    assert [entry["present"] for entry in observed["connections"]] == [False, False, False]
    assert all(entry["answered"] for entry in observed["connections"]) and not observed["networkManager"]
    assert not manage(host, "absent")
    with pytest.raises(ValueError, match="NetworkManager"):
        manage(host, "present")


def test_a_leg_and_a_trunk_reach_the_host_collision_check_as_their_devices():
    networks = nm.host_networks(nm.wanted(REQUEST))
    assert networks == [
        {"name": TRUNK, "bridge": "br-lab", "managed": True},
        {"name": "ceph-public", "bridge": "br-lab.110", "managed": True, "address": "198.51.100.1/26"},
        {"name": "ceph-arbiter", "bridge": "br-lab.150", "managed": True, "address": "198.51.100.65/26"},
    ]


def test_a_provider_without_a_tagged_network_holds_nothing(host):
    plain = {"identity": REQUEST["identity"], "networks": [{"managed": True}]}
    assert not manage(host, "present", plain) and not manage(host, "absent", plain)
    assert host.calls == []
    assert observe(host, plain)["connections"] == [] and observe(host, plain)["refusals"] == []


def mutated(path, value):
    request = copy.deepcopy(REQUEST)
    target = request
    for key in path[:-1]:
        target = target[key]
    if value is KeyError:
        del target[path[-1]]
    else:
        target[path[-1]] = value
    return request


@pytest.mark.parametrize("path, value", [
    (("trunks", 0, "name"), "bootwright-lab-other-trunk-br-lab"),
    (("trunks", 0, "mac"), "53:54:00:aa:bb:01"),
    (("trunks", 0, "uuid"), "not-a-uuid"),
    (("trunks", 0, "bridge"), "br-lab-with-a-long-name"),
    (("legs", 0, "name"), "bootwright-other-ceph-public"),
    (("legs", 0, "device"), "br-lab.111"),
    (("legs", 0, "trunk"), "br-other"),
    (("legs", 0, "vlan"), True),
    (("legs", 0, "vlan"), 4095),
    (("legs", 0, "address"), "2001:db8::1/64"),
    (("legs", 0, "address"), "198.51.100.1"),
    (("legs", 0, "forward"), "route"),
    (("legs", 0, "extra"), "x"),
    (("legs", 1, "uuid"), UUIDS[PUBLIC]),
    (("legs",), []),
])
def test_a_request_that_does_not_match_the_provider_is_refused_unobserved(host, path, value):
    request = mutated(path, value)
    with pytest.raises(ValueError):
        manage(host, "present", request)
    assert host.calls == []
    assert observe(host, request)["refusals"] == ["the frozen trunks and legs could not be read"]


def test_a_terse_line_splits_only_on_unescaped_colons():
    assert nm.split_terse("a\\:b:c\\\\:d") == ["a:b", "c\\", "d"]
