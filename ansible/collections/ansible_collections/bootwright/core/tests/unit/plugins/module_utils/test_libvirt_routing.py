"""The routing table is exactly the frozen one, owned by its unit, replays safely and leaves with the unit."""

from __future__ import annotations

import copy
import ipaddress
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_routing as routing

UNIT = "bootwright-lab-host-routing.service"
TABLE = "bootwright-lab-host-routing"
REQUEST = {
    "identity": {"context": "lab", "object": "host"},
    "networks": [{"address": "192.0.2.1/24", "bridge": "virbr-user", "forward": "nat", "managed": True, "name": "bootwright-lab-user"},
                 {"bridge": "br-external", "managed": False, "name": "external"}],
    "trunks": [{"bridge": "br-lab", "mac": "52:54:00:aa:bb:01", "name": "bootwright-lab-host-trunk-br-lab",
                "uuid": "0b1c2d3e-0000-8000-8000-000000000001"}],
    "legs": [
        {"address": "198.51.100.1/26", "device": "br-lab.110", "forward": "nat", "name": "bootwright-lab-ceph-public",
         "network": "ceph-public", "trunk": "br-lab", "uuid": "0b1c2d3e-0000-8000-8000-000000000002", "vlan": 110},
        {"address": "198.51.100.65/26", "device": "br-lab.150", "forward": "nat", "name": "bootwright-lab-ceph-arbiter",
         "network": "ceph-arbiter", "trunk": "br-lab", "uuid": "0b1c2d3e-0000-8000-8000-000000000003", "vlan": 150},
    ],
    "routing": {
        "forwarding": True,
        "keptPrefixes": ["192.0.2.0/24", "198.51.100.0/26", "198.51.100.64/26", "203.0.113.0/26"],
        "managedDevices": ["br-lab", "br-lab.110", "br-lab.150", "virbr-user"],
        "natPrefixes": ["198.51.100.0/26", "198.51.100.64/26"],
        "routed": [{"from": "br-lab.110", "to": "br-lab.150"}, {"from": "br-lab.150", "to": "br-lab.110"}],
        "unit": UNIT,
    },
}

# The exact nft input of the two-leg example, as nft 1.1.6 loaded it into a
# scratch network namespace and listed it back as CAPTURED below.
RULESET = """table inet bootwright-lab-host-routing
delete table inet bootwright-lab-host-routing
table inet bootwright-lab-host-routing {
comment "Bootwright owned routing: bootwright-lab-host-routing.service"
set trunks { type ifname; elements = { "br-lab" }; }
set legs { type ifname; elements = { "br-lab.110", "br-lab.150" }; }
set nat_legs { type ifname; elements = { "br-lab.110", "br-lab.150" }; }
set managed { type ifname; elements = { "br-lab", "br-lab.110", "br-lab.150", "virbr-user" }; }
set routed { type ifname . ifname; elements = { "br-lab.110" . "br-lab.150", "br-lab.150" . "br-lab.110" }; }
set nat_prefixes { type ipv4_addr; flags interval; auto-merge; elements = { 198.51.100.0/26, 198.51.100.64/26 }; }
set kept_prefixes { type ipv4_addr; flags interval; auto-merge; elements = { 192.0.2.0/24, 198.51.100.0/26, 198.51.100.64/26, 203.0.113.0/26 }; }
chain input {
type filter hook input priority filter - 10; policy accept;
iifname @trunks drop
}
chain forward {
type filter hook forward priority filter - 10; policy accept;
iifname @trunks drop
oifname @trunks drop
meta nfproto ipv6 iifname @legs reject
meta nfproto ipv6 oifname @legs reject
ct state established,related accept
iifname . oifname @routed accept
oifname @legs reject
iifname @nat_legs oifname != @managed ip daddr != @kept_prefixes accept
iifname @legs reject
}
chain postrouting {
type nat hook postrouting priority srcnat - 10; policy accept;
ip saddr @nat_prefixes ip daddr != @kept_prefixes oifname != @managed masquerade
}
}
"""

# `nft -j list table inet bootwright-lab-host-routing` after loading RULESET,
# each rule's expressions left out.
CAPTURED = json.loads(
    '{"nftables":[{"metainfo":{"version":"1.1.6","release_name":"Commodore Bullmoose #7","json_schema_version":1}},'
    '{"table":{"family":"inet","name":"bootwright-lab-host-routing","handle":3,'
    '"comment":"Bootwright owned routing: bootwright-lab-host-routing.service"}},'
    '{"chain":{"family":"inet","table":"bootwright-lab-host-routing","name":"input","handle":1,"type":"filter","hook":"input",'
    '"prio":-10,"policy":"accept"}},'
    '{"chain":{"family":"inet","table":"bootwright-lab-host-routing","name":"forward","handle":2,"type":"filter","hook":"forward",'
    '"prio":-10,"policy":"accept"}},'
    '{"chain":{"family":"inet","table":"bootwright-lab-host-routing","name":"postrouting","handle":3,"type":"nat",'
    '"hook":"postrouting","prio":90,"policy":"accept"}},'
    '{"set":{"family":"inet","name":"trunks","table":"bootwright-lab-host-routing","type":"ifname","handle":4,"elem":["br-lab"]}},'
    '{"set":{"family":"inet","name":"legs","table":"bootwright-lab-host-routing","type":"ifname","handle":5,"elem":["br-lab.110","br-lab.150"]}},'
    '{"set":{"family":"inet","name":"nat_legs","table":"bootwright-lab-host-routing","type":"ifname","handle":6,"elem":["br-lab.110","br-lab.150"]}},'
    '{"set":{"family":"inet","name":"managed","table":"bootwright-lab-host-routing","type":"ifname","handle":7,'
    '"elem":["br-lab","br-lab.110","br-lab.150","virbr-user"]}},'
    '{"set":{"family":"inet","name":"routed","table":"bootwright-lab-host-routing","type":["ifname","ifname"],"handle":8,'
    '"elem":[{"concat":["br-lab.150","br-lab.110"]},{"concat":["br-lab.110","br-lab.150"]}]}},'
    '{"set":{"family":"inet","name":"nat_prefixes","table":"bootwright-lab-host-routing","type":"ipv4_addr","handle":9,"flags":["interval"],"auto-merge":true,'
    '"elem":[{"prefix":{"addr":"198.51.100.0","len":25}}]}},'
    '{"set":{"family":"inet","name":"kept_prefixes","table":"bootwright-lab-host-routing","type":"ipv4_addr","handle":10,'
    '"flags":["interval"],"auto-merge":true,'
    '"elem":[{"prefix":{"addr":"192.0.2.0","len":24}},{"prefix":{"addr":"198.51.100.0","len":25}},{"prefix":{"addr":"203.0.113.0","len":26}}]}},'
    + ",".join('{"rule":{"family":"inet","table":"bootwright-lab-host-routing","chain":"%s","handle":%d,"expr":[]}}' % (chain, 11 + index)
               for index, chain in enumerate(["input"] + ["forward"] * 9 + ["postrouting"]))
    + "]}"
)["nftables"]


def request(**routing_fields):
    changed = copy.deepcopy(REQUEST)
    changed["routing"].update(routing_fields)
    return changed


def listing(table):
    """What nft lists for a loaded frozen table, in the shape of CAPTURED."""
    name = table["table"]
    items = [{"metainfo": {"json_schema_version": 1}},
             {"table": {"family": "inet", "name": name, "handle": 1, "comment": routing.TABLE_MARK + table["unit"]}}]
    for chain, (kind, hook, prio, _count) in routing.CHAINS.items():
        items.append({"chain": {"family": "inet", "table": name, "name": chain, "type": kind, "hook": hook, "prio": prio, "policy": "accept"}})
    for set_name in ("trunks", "legs", "nat_legs", "managed"):
        entry = {"family": "inet", "name": set_name, "table": name, "type": "ifname"}
        if table[set_name]:
            entry["elem"] = list(table[set_name])
        items.append({"set": entry})
    entry = {"family": "inet", "name": "routed", "table": name, "type": ["ifname", "ifname"]}
    if table["routed"]:
        entry["elem"] = [{"concat": list(pair)} for pair in table["routed"]]
    items.append({"set": entry})
    for set_name in ("nat_prefixes", "kept_prefixes"):
        entry = {"family": "inet", "name": set_name, "table": name, "type": "ipv4_addr", "flags": ["interval"], "auto-merge": True}
        merged = routing.intervals(table[set_name])
        if merged:
            entry["elem"] = [{"range": [str(ipaddress.IPv4Address(first)), str(ipaddress.IPv4Address(last))]} for first, last in merged]
        items.append({"set": entry})
    for chain, (_kind, _hook, _prio, count) in routing.CHAINS.items():
        items += [{"rule": {"family": "inet", "table": name, "chain": chain, "expr": []}} for _index in range(count)]
    return items


def earlier_unit(table):
    """The earlier unit format, with its ruleset as StandardInputText= lines."""
    lines = [routing.MARK + table["unit"], "[Unit]", "Description=Bootwright routes and isolates the VLAN legs on owned trunks",
             "Before=network-pre.target NetworkManager.service", "Wants=network-pre.target", "",
             "[Service]", "Type=oneshot", "RemainAfterExit=yes", "StandardInput=data"]
    lines += ["StandardInputText=" + line for line in routing.ruleset(table).splitlines()]
    lines += ["ExecStart=/usr/sbin/nft -f -", "ExecStartPost=/usr/sbin/sysctl -w net.ipv4.ip_forward=1", "ExecReload=/usr/sbin/nft -f -",
              "ExecReload=/usr/sbin/sysctl -w net.ipv4.ip_forward=1", "ExecStopPost=-/usr/sbin/nft delete table inet " + table["table"],
              "", "[Install]", "WantedBy=multi-user.target", ""]
    return "\n".join(lines)


class Host:
    """A host whose systemd runs the unit's commands and whose nft keeps tables as the kernel does.

    As under SELinux, an `nft -f -` the unit feeds from StandardInputText=
    reads nothing and exits 0; `nft -f <file>` loads the file it names.
    """

    def __init__(self, root, rules, forwarding):
        self.root = root
        self.rules = rules
        self.forwarding = forwarding
        forwarding.write_text("0\n")
        self.state = ""
        self.tables = {}
        self.calls = []
        self.dropins = ""
        self.fail = ""
        self.loadable = {}
        self.stale = False

    def expect(self, frozen):
        table = routing.model(frozen)
        self.loadable[routing.rules_content(table)] = table

    def unit_lines(self, prefix):
        path = self.root / UNIT
        return [line[len(prefix):] for line in path.read_text().splitlines() if line.startswith(prefix)] if path.exists() else []

    def load(self, command):
        """One `nft -f`: True when it exited 0, having loaded the file it names or, from standard input, nothing."""
        source = command[len(routing.NFT + " -f "):]
        if source == "-":
            return True
        try:
            content = open(source, "rb").read()
        except OSError:
            return False
        if self.fail == "load" or content not in self.loadable:
            return False
        self.tables[TABLE] = listing(self.loadable[content])
        return True

    def steps(self, commands):
        """Run the unit's commands in order, as systemd does, until one fails."""
        for command in commands:
            if command.startswith(routing.NFT + " -f "):
                if not self.load(command):
                    return False
            elif command == "%s list table inet %s" % (routing.NFT, TABLE):
                if TABLE not in self.tables:
                    return False
            elif command.endswith("net.ipv4.ip_forward=1"):
                self.forwarding.write_text("1\n")
        return True

    def start(self):
        if not self.steps(self.unit_lines("ExecStart=") + self.unit_lines("ExecStartPost=")):
            self.state = "failed"
            self.tables.pop(TABLE, None)
            return 1, "", "Job for the unit failed"
        self.state = "active"
        return 0, "", ""

    def reload(self):
        """A reload loads the unit's file in one transaction: a refused one leaves the table it would replace."""
        if self.state != "active":
            return 1, "", "Job for the unit failed: not active"
        if not self.steps(self.unit_lines("ExecReload=")):
            return 1, "", "Job for the unit failed"
        return 0, "", ""

    def stop(self):
        if self.state in ("active", "failed"):
            self.tables.pop(TABLE, None)
            self.state = "inactive"

    def run(self, argv, **kwargs):
        assert kwargs["check_rc"] is False
        self.calls.append(argv)
        if argv[0] == routing.NFT:
            return self.nft(argv[1:])
        action = argv[1]
        if action == self.fail:
            return 1, "", "injected failure"
        if action == "show":
            present = (self.root / UNIT).exists()
            return 0, "LoadState=%s\nActiveState=%s\nFragmentPath=%s\nDropInPaths=%s\nNeedDaemonReload=%s\n" % (
                "loaded" if present else "not-found", self.state or "inactive", str(self.root / UNIT) if present else "", self.dropins,
                "yes" if self.stale else "no"), ""
        if action == "daemon-reload":
            self.stale = False
        if action == "stop":
            self.stop()
        elif action == "restart":
            self.stop()
            return self.start()
        elif action == "reload":
            return self.reload()
        elif action == "start" and self.state != "active":
            return self.start()
        elif action == "reset-failed" and self.state == "failed":
            self.state = "inactive"
        return 0, "", ""

    def nft(self, words):
        if self.fail == "nft":
            return 1, "", "injected"
        if words == ["-j", "list", "tables"]:
            return 0, json.dumps({"nftables": [{"metainfo": {}}] + [{"table": {"family": "inet", "name": name}} for name in self.tables]}), ""
        if words[:3] == ["-j", "list", "table"] and words[4] in self.tables:
            return 0, json.dumps({"nftables": self.tables[words[4]]}), ""
        if words[:3] == ["delete", "table", "inet"] and words[3] in self.tables:
            del self.tables[words[3]]
            return 0, "", ""
        return 1, "", "Error: No such file or directory"

    def manage(self, state, frozen=None):
        return routing.manage(self.run, frozen or REQUEST, state, str(self.root), os.geteuid(), str(self.rules))

    def observe(self, frozen=None):
        return routing.observe(self.run, frozen or REQUEST, str(self.root), os.geteuid(), str(self.rules))

    def rules_file(self):
        return self.rules / (TABLE + ".nft")

    def effects(self):
        return [call for call in self.calls if call[0] != routing.NFT and call[1] not in ("show",)
                or call[0] == routing.NFT and call[1] == "delete"]


@pytest.fixture
def host(tmp_path, monkeypatch):
    forwarding = tmp_path / "ip_forward"
    monkeypatch.setattr(routing, "FORWARDING", str(forwarding))
    monkeypatch.setattr(routing, "executable", lambda path: True)
    systemd = tmp_path / "systemd"
    systemd.mkdir()
    (tmp_path / "etc").mkdir()
    found = Host(systemd, tmp_path / "etc" / "bootwright" / "routing", forwarding)
    found.expect(REQUEST)
    return found


def held(**fields):
    return dict({"name": UNIT, "table": TABLE, "answered": True, "present": True, "owned": True, "enabled": True,
                 "state": "active", "loaded": True, "forwarding": True}, **fields)


def test_the_ruleset_is_exactly_the_frozen_text():
    assert routing.ruleset(routing.model(REQUEST)) == RULESET


def test_the_captured_table_of_the_ruleset_is_the_loaded_frozen_one():
    table = routing.model(REQUEST)
    assert routing.loaded(CAPTURED, table)
    assert routing.loaded(listing(table), table)
    without_rule = [item for item in CAPTURED if (item.get("rule") or {}).get("chain") != "input"]
    assert not routing.loaded(without_rule, table)
    marked = copy.deepcopy(CAPTURED)
    marked[1]["table"]["comment"] = "someone else's"
    assert not routing.loaded(marked, table)
    lowered = copy.deepcopy(CAPTURED)
    lowered[3]["chain"]["prio"] = 0
    assert not routing.loaded(lowered, table)
    extra = CAPTURED + [{"chain": {"family": "inet", "table": TABLE, "name": "output", "type": "filter", "hook": "output", "prio": 0, "policy": "accept"}}]
    assert not routing.loaded(extra, table)


def test_an_undeclared_pair_of_legs_is_never_accepted():
    table = routing.model(REQUEST)
    assert table["routed"] == [("br-lab.110", "br-lab.150"), ("br-lab.150", "br-lab.110")]
    alone = routing.ruleset(routing.model(request(routed=[], forwarding=True)))
    assert "set routed { type ifname . ifname; }\n" in alone
    assert alone.count(" accept\n") == 3
    forward = alone[alone.index("chain forward {"):alone.index("chain postrouting {")].splitlines()
    assert forward.index("iifname . oifname @routed accept") < forward.index("oifname @legs reject") < forward.index("iifname @legs reject")
    assert forward[-2:] == ["iifname @legs reject", "}"]
    with pytest.raises(ValueError, match="routed pair"):
        routing.model(request(routed=[{"from": "br-lab.110", "to": "virbr-user"}]))
    with pytest.raises(ValueError, match="routed pair"):
        routing.model(request(routed=[{"from": "br-lab.110", "to": "br-lab.110"}]))
    with pytest.raises(ValueError, match="reverse"):
        routing.model(request(routed=[{"from": "br-lab.110", "to": "br-lab.150"}]))


def test_masquerade_translates_only_the_nat_legs_and_never_towards_a_kept_prefix_or_a_managed_interface():
    text = routing.ruleset(routing.model(REQUEST))
    assert text.count("masquerade") == 1
    assert "ip saddr @nat_prefixes ip daddr != @kept_prefixes oifname != @managed masquerade\n" in text
    assert "elements = { 192.0.2.0/24, 198.51.100.0/26, 198.51.100.64/26, 203.0.113.0/26 };" in text
    assert "iifname @nat_legs oifname != @managed ip daddr != @kept_prefixes accept" in text
    isolated = copy.deepcopy(REQUEST)
    isolated["legs"][1]["forward"] = "none"
    isolated["routing"].update(natPrefixes=["198.51.100.0/26"])
    table = routing.model(isolated)
    assert table["nat_legs"] == ["br-lab.110"] and [str(network) for network in table["nat_prefixes"]] == ["198.51.100.0/26"]
    with pytest.raises(ValueError, match="NAT prefixes"):
        routing.model(request(natPrefixes=["198.51.100.0/26"]))
    with pytest.raises(ValueError, match="kept prefixes omit"):
        routing.model(request(keptPrefixes=["192.0.2.0/24"]))


def test_the_unit_loads_the_table_before_the_network_and_deletes_it_when_stopped():
    content = routing.unit_content(routing.model(REQUEST)).decode()
    lines = content.splitlines()
    assert lines[0] == "# Bootwright owned routing: " + UNIT
    load = "/usr/sbin/nft -f /etc/bootwright/routing/%s.nft" % TABLE
    listed = "/usr/sbin/nft list table inet " + TABLE
    for line in ("Type=oneshot", "RemainAfterExit=yes", "ExecStart=" + load,
                 "ExecStopPost=-/usr/sbin/nft delete table inet " + TABLE,
                 "Wants=network-pre.target", "Before=network-pre.target NetworkManager.service",
                 "WantedBy=multi-user.target"):
        assert line in lines, line
    assert [line for line in lines if line.startswith("ExecStartPost=")] == [
        "ExecStartPost=" + listed, "ExecStartPost=/usr/sbin/sysctl -w net.ipv4.ip_forward=1"]
    assert [line for line in lines if line.startswith("ExecReload=")] == [
        "ExecReload=" + load, "ExecReload=" + listed, "ExecReload=/usr/sbin/sysctl -w net.ipv4.ip_forward=1"]
    without = routing.unit_content(routing.model(dict(copy.deepcopy(REQUEST), legs=[dict(leg, forward="none") for leg in REQUEST["legs"]],
                                                      routing=dict(REQUEST["routing"], forwarding=False, natPrefixes=[], routed=[])))).decode()
    assert "sysctl" not in without
    assert [line for line in without.splitlines() if line.startswith("ExecReload=")] == ["ExecReload=" + load, "ExecReload=" + listed]


def test_the_unit_reads_no_standard_input_and_its_ruleset_file_is_the_mark_then_the_ruleset():
    """B1335: under SELinux nft may not read systemd's StandardInputData memfd, so it loaded nothing and exited 0."""
    table = routing.model(REQUEST)
    content = routing.unit_content(table).decode()
    assert "StandardInput" not in content and " -f -" not in content
    assert routing.rules_path(table) == "/etc/bootwright/routing/%s.nft" % TABLE
    assert routing.rules_content(table).decode() == "# Bootwright owned routing: %s\n" % UNIT + RULESET


def test_apply_replay_and_owned_removal(host):
    unrelated = host.root / "another.service"
    unrelated.write_text("unrelated")
    assert host.manage("present")
    assert host.observe() == (held(), [])
    assert (host.root / "multi-user.target.wants" / UNIT).is_symlink()
    host.calls.clear()
    assert not host.manage("present")
    assert host.effects() == []
    assert host.manage("absent")
    assert not host.manage("absent")
    assert host.tables == {}
    assert host.observe()[0] == held(table="", present=False, owned=False, enabled=False, state="", loaded=False)
    assert unrelated.read_text() == "unrelated"
    assert not (host.root / UNIT).exists()
    assert not host.rules_file().exists() and not host.rules.exists() and host.rules.parent.is_dir()


def test_the_ruleset_file_and_its_directories_are_owned_and_written_atomically(host):
    host.manage("present")
    path = host.rules_file()
    assert path.read_bytes() == routing.rules_content(routing.model(REQUEST))
    assert oct(path.stat().st_mode & 0o7777) == "0o644" and path.stat().st_nlink == 1
    for directory in (host.rules, host.rules.parent):
        assert directory.is_dir() and not directory.is_symlink() and oct(directory.stat().st_mode & 0o7777) == "0o755"
    assert sorted(entry.name for entry in host.rules.iterdir()) == [TABLE + ".nft"]


def test_a_host_holding_the_earlier_standard_input_unit_with_no_table_converges_by_reloading(host):
    """B1335: an earlier unit can be enabled and active without a table or ruleset file."""
    table = routing.model(REQUEST)
    (host.root / UNIT).write_text(earlier_unit(table))
    (host.root / UNIT).chmod(0o644)
    (host.root / "multi-user.target.wants").mkdir()
    (host.root / "multi-user.target.wants" / UNIT).symlink_to(host.root / UNIT)
    assert host.start() == (0, "", "") and host.state == "active" and host.tables == {}
    evidence, refusals = host.observe()
    assert refusals == [] and evidence == held(table="", loaded=False, forwarding=True)
    host.calls.clear()
    assert host.manage("present")
    systemctl = [call[1:] for call in host.calls if call[0] == routing.SYSTEMCTL and call[1] != "show"]
    assert systemctl == [["daemon-reload"], ["reload", UNIT]]
    assert host.observe() == (held(), [])
    assert host.rules_file().read_bytes() == routing.rules_content(table)
    assert not host.manage("present")


def test_an_apply_cut_off_before_its_daemon_reload_repeats_it(host):
    host.manage("present")
    del host.tables[TABLE]
    host.stale = True
    host.calls.clear()
    assert host.manage("present")
    systemctl = [call[1:] for call in host.calls if call[0] == routing.SYSTEMCTL and call[1] != "show"]
    assert systemctl == [["daemon-reload"], ["reload", UNIT]]
    assert host.observe() == (held(), [])


def test_a_unit_whose_load_leaves_no_table_fails_rather_than_succeeding(host):
    host.manage("present")
    host.stop()
    host.load = lambda command: True
    assert host.start()[0] == 1 and host.state == "failed" and host.tables == {}


@pytest.mark.parametrize("drift", ["deleted", "edited", "forwarding"])
def test_a_drifted_table_is_reloaded_in_place_and_never_stopped(host, drift):
    host.manage("present")
    if drift == "deleted":
        del host.tables[TABLE]
    elif drift == "edited":
        host.tables[TABLE] = [item for item in host.tables[TABLE] if (item.get("rule") or {}).get("chain") != "input"]
    else:
        host.forwarding.write_text("0\n")
    host.calls.clear()
    assert host.manage("present")
    assert [routing.SYSTEMCTL, "reload", UNIT] in host.calls
    assert not any(call[1] in ("restart", "stop") for call in host.calls if call[0] == routing.SYSTEMCTL)
    assert host.observe() == (held(), [])


def test_a_changed_request_rewrites_the_ruleset_file_and_reloads_it_without_dropping_the_table(host):
    host.manage("present")
    unit = (host.root / UNIT).read_bytes()
    narrowed = request(routed=[])
    host.expect(narrowed)
    host.calls.clear()
    assert host.manage("present", narrowed)
    systemctl = [call[1:] for call in host.calls if call[0] == routing.SYSTEMCTL and call[1] != "show"]
    assert systemctl == [["reload", UNIT]]
    assert (host.root / UNIT).read_bytes() == unit
    assert host.rules_file().read_bytes() == routing.rules_content(routing.model(narrowed))
    assert host.observe(narrowed) == (held(), [])
    assert not host.manage("present", narrowed)


def test_a_failed_start_leaves_no_table_and_a_replay_converges(host):
    host.fail = "load"
    with pytest.raises(ValueError):
        host.manage("present")
    assert TABLE not in host.tables and host.state == "failed"
    host.fail = ""
    assert host.manage("present")
    assert host.observe() == (held(), [])


def test_a_failed_reload_keeps_the_table_it_would_replace_and_a_replay_converges(host):
    host.manage("present")
    before = copy.deepcopy(host.tables[TABLE])
    narrowed = request(routed=[])
    host.expect(narrowed)
    host.fail = "load"
    with pytest.raises(ValueError):
        host.manage("present", narrowed)
    assert host.tables[TABLE] == before and host.state == "active"
    host.fail = ""
    assert host.manage("present", narrowed)
    assert host.observe(narrowed) == (held(), [])


@pytest.mark.parametrize("state", ["present", "absent"])
def test_a_foreign_table_of_the_units_name_refuses_before_any_effect(host, state):
    host.tables[TABLE] = [{"table": {"family": "inet", "name": TABLE}}]
    with pytest.raises(ValueError, match="nft delete table inet " + TABLE):
        host.manage(state)
    assert host.effects() == [] and not (host.root / UNIT).exists()
    assert host.observe()[0]["present"] and not host.observe()[0]["owned"]


def test_the_owned_table_while_its_unit_is_stopped_refuses_too(host):
    host.manage("present")
    host.state = "inactive"
    host.calls.clear()
    with pytest.raises(ValueError, match="does not run"):
        host.manage("present")
    assert host.effects() == []


@pytest.mark.parametrize("collision", ["content", "mark", "symlink", "wants", "dropin", "vendor"])
def test_a_foreign_unit_refuses_before_mutation(host, collision):
    path = host.root / UNIT
    if collision == "content":
        path.write_text("foreign")
        path.chmod(0o644)
    elif collision == "mark":
        path.write_text("# Bootwright owned routing: other.service\n")
        path.chmod(0o644)
    elif collision == "symlink":
        path.symlink_to(host.root / "outside")
    elif collision == "wants":
        (host.root / "multi-user.target.wants").mkdir()
        (host.root / "multi-user.target.wants" / UNIT).symlink_to(host.root / "other.service")
    elif collision == "dropin":
        (host.root / (UNIT + ".d")).mkdir()
    else:
        host.dropins = "/etc/systemd/system/service.d/override.conf"
    for state in ("present", "absent"):
        with pytest.raises(ValueError, match="not this provider's"):
            host.manage(state)
    assert host.effects() == [] and host.tables == {}


@pytest.mark.parametrize("collision", ["content", "mark", "symlink", "mode", "hardlink", "directory"])
def test_a_foreign_file_at_the_ruleset_path_refuses_before_any_effect(host, collision):
    host.rules.mkdir(parents=True)
    path = host.rules_file()
    owned = routing.rules_content(routing.model(REQUEST))
    if collision == "content":
        path.write_text("flush ruleset\n")
    elif collision == "mark":
        path.write_bytes(owned.replace(UNIT.encode(), b"other.service", 1))
    elif collision == "symlink":
        (host.rules / "elsewhere").write_bytes(owned)
        path.symlink_to(host.rules / "elsewhere")
    elif collision == "mode":
        path.write_bytes(owned)
        path.chmod(0o666)
    elif collision == "hardlink":
        path.write_bytes(owned)
        os.link(path, host.rules / "second")
    else:
        path.mkdir()
    if collision not in ("mode", "symlink", "directory"):
        path.chmod(0o644)
    before = os.lstat(path)
    for state in ("present", "absent"):
        with pytest.raises(ValueError, match="ruleset file .* is not this provider's"):
            host.manage(state)
    assert host.effects() == [] and host.tables == {} and not (host.root / UNIT).exists()
    assert os.lstat(path).st_ino == before.st_ino
    evidence = host.observe()[0]
    assert evidence["present"] and not evidence["owned"]


def test_an_unsafe_ruleset_directory_refuses_unread(host):
    host.rules.mkdir(parents=True)
    host.rules.chmod(0o777)
    with pytest.raises(ValueError, match="unsafe routing ruleset directory"):
        host.manage("present")
    assert host.effects() == [] and not (host.root / UNIT).exists()
    evidence, refusals = host.observe()
    assert evidence["answered"] is False and refusals


def test_an_owned_ruleset_file_left_without_its_unit_is_removed_or_adopted(host):
    host.manage("present")
    host.manage("absent")
    host.rules.mkdir()
    host.rules_file().write_bytes(routing.rules_content(routing.model(REQUEST)))
    host.rules_file().chmod(0o644)
    evidence = host.observe()[0]
    assert evidence["present"] and evidence["owned"] and not evidence["enabled"]
    assert host.manage("absent")
    assert not host.rules_file().exists() and host.observe()[0]["present"] is False
    host.rules.mkdir()
    host.rules_file().write_bytes(routing.rules_content(routing.model(REQUEST)))
    host.rules_file().chmod(0o644)
    assert host.manage("present")
    assert host.observe() == (held(), [])


def test_an_edited_ruleset_file_is_rewritten_and_reloaded_without_rewriting_the_unit(host):
    host.manage("present")
    unit = (host.root / UNIT).read_bytes()
    host.rules_file().write_bytes(routing.rules_content(routing.model(REQUEST)) + b"flush ruleset\n")
    host.calls.clear()
    assert host.manage("present")
    systemctl = [call[1:] for call in host.calls if call[0] == routing.SYSTEMCTL and call[1] != "show"]
    assert systemctl == [["reload", UNIT]]
    assert (host.root / UNIT).read_bytes() == unit
    assert host.observe() == (held(), [])


def test_an_unanswered_nft_read_proves_nothing(host):
    host.fail = "nft"
    with pytest.raises(ValueError, match="did not answer"):
        host.manage("present")
    assert host.observe()[0]["answered"] is False
    assert host.effects() == []


def test_nft_missing_refuses(host, monkeypatch):
    monkeypatch.setattr(routing, "executable", lambda path: path != routing.NFT)
    with pytest.raises(ValueError, match="/usr/sbin/nft"):
        host.manage("present")


def test_removal_deletes_a_table_its_stop_left_and_proves_absence(host):
    host.manage("present")
    original = host.stop

    def leaves_the_table():
        host.state = "inactive"

    host.stop = leaves_the_table
    assert host.manage("absent")
    assert [routing.NFT, "delete", "table", "inet", TABLE] in host.calls
    assert host.tables == {}
    host.stop = original


def test_removal_that_cannot_prove_the_table_gone_refuses(host, monkeypatch):
    host.manage("present")
    host.stop = lambda: setattr(host, "state", "inactive")
    host.nft = lambda words: (0, json.dumps({"nftables": [{"table": {"family": "inet", "name": TABLE}}]}), "") if words[1:3] == ["list", "tables"] else (
        (1, "", "Operation not permitted") if words[0] == "delete" else (0, json.dumps({"nftables": host.tables.get(TABLE, [])}), ""))
    with pytest.raises(ValueError):
        host.manage("absent")


def test_a_provider_without_a_vlan_leg_holds_no_table(host):
    plain = {"identity": REQUEST["identity"], "networks": [{"managed": True}]}
    assert not host.manage("present", plain)
    assert not host.manage("absent", plain)
    assert host.calls == []
    assert routing.observe(host.run, plain) == (routing.blank(), [])


@pytest.mark.parametrize("fields", [
    {"unit": "bootwright-other-host-routing.service"},
    {"forwarding": False},
    {"forwarding": "yes"},
    {"managedDevices": ["br-lab", "br-lab.110"]},
    {"managedDevices": ["br-lab", "br-lab", "br-lab.110", "br-lab.150", "virbr-user"]},
    {"managedDevices": ["br-external", "br-lab", "br-lab.110", "br-lab.150", "virbr-user"]},
    {"managedDevices": ["br-lab", "br-lab.110", "br-lab.150", "virbr-user", "eth0"]},
    {"managedDevices": ["br-lab", "br-lab.110", "br-lab.150", 'virbr-user" }; flush ruleset; set x { type ifname; elements = { "y']},
    {"keptPrefixes": ["192.0.2.0/24", "198.51.100.0/26", "198.51.100.64/26", "2001:db8::/64"]},
    {"keptPrefixes": ["198.51.100.64/26", "198.51.100.0/26"]},
    {"natPrefixes": ["198.51.100.1/26"]},
    {"extra": True},
])
def test_a_routing_request_that_does_not_match_the_provider_is_refused_unobserved(host, fields):
    frozen = request(**fields)
    with pytest.raises(ValueError):
        host.manage("present", frozen)
    evidence, refusals = host.observe(frozen)
    assert evidence["answered"] is False and refusals
    assert host.calls == []


def test_a_managed_device_that_would_inject_a_statement_never_reaches_the_ruleset():
    injected = 'virbr-user" }; flush ruleset; set x { type ifname; elements = { "y'
    frozen = request(managedDevices=["br-lab", "br-lab.110", "br-lab.150", injected])
    frozen["networks"][0]["bridge"] = injected
    with pytest.raises(ValueError, match="interface names"):
        routing.model(frozen)


def test_legs_without_a_routing_table_or_a_table_without_legs_is_refused():
    with pytest.raises(ValueError):
        routing.model(dict(REQUEST, routing=None))
    with pytest.raises(ValueError):
        routing.model({"identity": REQUEST["identity"], "routing": REQUEST["routing"]})
