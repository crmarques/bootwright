"""The refusing routes are owned, replay safely, never replace a foreign route and leave with the unit."""

import ipaddress
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import libvirt_vlan_refusal as refusal

NAME = "bootwright-lab-host-vlan-refusal.service"
V4 = "203.0.113.0/26"
V6 = "2001:db8:120::/64"
REQUEST = {"identity": {"context": "lab", "object": "host"}, "networks": [{"managed": True}],
           "vlanRefusalUnit": NAME, "refusedPrefixes": [V4, V6]}
EMPTY = dict(REQUEST, refusedPrefixes=[])


def route(destination, **fields):
    return dict({"dst": destination, "dev": "enp1s0", "protocol": "dhcp", "flags": []}, **fields)


# What ip -N prints by number; without it ip prints protocol 251 by the name this
# host's rt_protos gives it.
NUMERIC_TYPES = {"unicast": "1", "local": "2", "broadcast": "3", "unreachable": "7"}
NUMERIC_PROTOCOLS = {"kernel": "2", "boot": "3", "static": "4", "dhcp": "16"}
NUMERIC_TABLES = {"main": "254", "local": "255"}


def kernel_key(version, item):
    destination = item["dst"] if item["dst"] != "default" else ("0.0.0.0/0" if version == 4 else "::/0")
    metric = int(item.get("metric") or (1024 if version == 6 and item.get("type") == "unreachable" else 0))
    return str(item.get("table") or "main"), ipaddress.ip_network(destination, strict=False), metric


class Host:
    """A host whose kernel keys routes as Linux does and whose units run the ip commands their files name."""

    def __init__(self, root, v4=None, v6=None):
        self.root = root
        self.states = {}
        self.fail = ""
        self.calls = []
        self.routes = {4: [route("default", gateway="192.0.2.1")] + (v4 or []), 6: [route("default", gateway="fe80::1")] + (v6 or [])}
        self.dropins = ""

    @property
    def state(self):
        return self.states.get(NAME, "inactive")

    @state.setter
    def state(self, value):
        self.states[NAME] = value

    def refusals(self, version):
        return [item for item in self.routes[version] if item.get("type") == "unreachable"]

    def listing(self, argv, version):
        numeric = "-N" in argv
        found = []
        for item in self.routes[version]:
            shown = dict(item)
            if numeric:
                for key, names in (("type", NUMERIC_TYPES), ("protocol", NUMERIC_PROTOCOLS), ("table", NUMERIC_TABLES)):
                    if key in shown:
                        shown[key] = names.get(shown[key], shown[key])
            elif shown.get("protocol") == refusal.PROTOCOL:
                shown["protocol"] = "bootwright"
            found.append(shown)
        return json.dumps(found)

    def run(self, argv, **kwargs):
        assert kwargs["check_rc"] is False
        self.calls.append(argv)
        if argv[0] == refusal.IP and "-json" in argv:
            version = 4 if "-4" in argv else 6
            if self.fail == "ip":
                return 1, "", "injected"
            return 0, self.listing(argv, version), ""
        action, name = argv[1], argv[-1]
        if action == self.fail:
            return 1, "", "injected failure"
        state = self.states.get(name, "inactive")
        if action == "show":
            path = self.root / name
            present = path.exists()
            return 0, "LoadState=%s\nActiveState=%s\nFragmentPath=%s\nDropInPaths=%s\n" % (
                "loaded" if present else "not-found", state, str(path) if present else "", self.dropins), ""
        if action in ("stop", "restart") and state in ("active", "failed"):
            self.exec_lines(name, "ExecStopPost=-")
            self.states[name] = "inactive"
        if action in ("start", "restart"):
            if not all(self.ip(words) for words in self.exec_lines(name, "ExecStart=", run=False)):
                self.states[name] = "failed"
                self.exec_lines(name, "ExecStopPost=-")
                return 1, "", "a refusing route exists"
            self.states[name] = "active"
        elif action == "reset-failed":
            self.states[name] = "inactive" if state == "failed" else state
        return 0, "", ""

    def exec_lines(self, name, prefix, run=True):
        found = [line[len(prefix):].split() for line in (self.root / name).read_text().splitlines() if line.startswith(prefix)]
        if run:
            for words in found:
                self.ip(words)
        return found

    def ip(self, words):
        """One ip route add or del as the kernel does it: an add of an existing key fails, a del removes the first match."""
        version = 4 if words[1] == "-4" else 6
        assert words[0] == refusal.IP and words[2] == "route" and words[4] == "unreachable" and words[6] == "proto"
        item = {"type": "unreachable", "dst": words[5], "protocol": words[7], "flags": []}
        if "metric" in words:
            item["metric"] = int(words[words.index("metric") + 1])
        elif version == 6:
            item["metric"] = 1024
        if words[3] == "add":
            if any(kernel_key(version, other) == kernel_key(version, item) for other in self.routes[version]):
                return False
            self.routes[version].append(item)
            return True
        for index, other in enumerate(self.routes[version]):
            if (other.get("type") == "unreachable" and other["dst"] == item["dst"] and other["protocol"] == item["protocol"]
                    and ("metric" not in words or kernel_key(version, other)[2] == item["metric"])):
                del self.routes[version][index]
                return True
        return False

    def manage(self, state, request=None):
        return refusal.manage(self.run, request or REQUEST, state, str(self.root), os.geteuid())

    def observe(self, request=None):
        return refusal.observe(self.run, request or REQUEST, str(self.root), os.geteuid())

    def unreachable(self):
        return sorted(item["dst"] for version in (4, 6) for item in self.refusals(version))


def held(**fields):
    return dict({"name": NAME, "answered": True, "present": True, "owned": True, "enabled": True, "state": "active", "routes": [V4, V6]}, **fields)


def test_apply_replay_and_owned_removal_of_both_families(tmp_path):
    host = Host(tmp_path)
    unrelated = tmp_path / "another.service"
    unrelated.write_text("unrelated")
    assert host.manage("present")
    assert host.observe() == held()
    assert host.unreachable() == sorted([V4, V6])
    assert (tmp_path / "multi-user.target.wants" / NAME).is_symlink()
    host.calls.clear()
    assert not host.manage("present")
    assert not any(call[1] in ("start", "stop", "restart", "daemon-reload") for call in host.calls if call[0] == refusal.SYSTEMCTL)
    assert host.manage("absent")
    assert not host.manage("absent")
    assert host.unreachable() == []
    assert host.observe() == {"name": NAME, "answered": True, "present": False, "owned": False, "enabled": False, "state": "", "routes": []}
    assert unrelated.read_text() == "unrelated"
    assert not (tmp_path / NAME).exists()
    assert not (tmp_path / "multi-user.target.wants" / NAME).is_symlink()
    assert not any(call[1] in ("enable", "disable") for call in host.calls if call[0] == refusal.SYSTEMCTL)


def test_unit_adds_and_deletes_only_a_route_of_its_own_type_protocol_and_metric():
    content = refusal.unit_content(NAME, refusal.prefixes(REQUEST)).decode()
    metric = refusal.metric(NAME)
    assert content.startswith("# Bootwright owned VLAN refusal: " + NAME + "\n")
    assert "ExecStart=%s -4 route add unreachable %s proto %s metric %d\n" % (refusal.IP, V4, refusal.PROTOCOL, metric) in content
    assert "ExecStart=%s -6 route add unreachable %s proto %s metric %d\n" % (refusal.IP, V6, refusal.PROTOCOL, metric) in content
    assert "ExecStopPost=-%s -4 route del unreachable %s proto %s metric %d\n" % (refusal.IP, V4, refusal.PROTOCOL, metric) in content
    assert "ExecStopPost=-%s -6 route del unreachable %s proto %s metric %d\n" % (refusal.IP, V6, refusal.PROTOCOL, metric) in content
    assert "Type=oneshot" in content and "RemainAfterExit=yes" in content and "WantedBy=multi-user.target" in content


@pytest.mark.parametrize("existing, expected", [
    (route(V4), "unicast route 203.0.113.0/26"),
    (route("203.0.113.0/27", gateway="192.0.2.1"), "unicast route 203.0.113.0/27"),
    (route("203.0.113.0/24"), "unicast route 203.0.113.0/24"),
    (route("203.0.113.0/24", table="vpn", gateway="192.0.2.1"), "in table vpn"),
    ({"type": "unreachable", "dst": V4, "protocol": "static", "flags": []}, "unreachable route 203.0.113.0/26"),
    ({"type": "unreachable", "dst": V4, "protocol": "250", "flags": []}, "unreachable route 203.0.113.0/26"),
    ({"type": "local", "dst": "203.0.113.5", "dev": "enp1s0", "table": "local", "protocol": "kernel", "flags": []}, "local route 203.0.113.5/32"),
])
def test_a_foreign_ipv4_route_refuses_by_name_before_any_change(tmp_path, existing, expected):
    host = Host(tmp_path, v4=[existing])
    with pytest.raises(ValueError, match="never replaced") as failure:
        host.manage("present")
    assert expected in str(failure.value) and V4 in str(failure.value)
    assert not (tmp_path / NAME).exists()
    assert not [call for call in host.calls if call[0] == refusal.SYSTEMCTL and call[1] != "show"]
    assert host.unreachable() == ([existing["dst"]] if existing.get("type") == "unreachable" else [])


def test_a_foreign_ipv6_route_refuses_and_leaves_the_ipv4_subnet_unrefused(tmp_path):
    host = Host(tmp_path, v6=[route("2001:db8:120::/48", dev="enp1s0", table="vpn")])
    with pytest.raises(ValueError, match="2001:db8:120::/48"):
        host.manage("present")
    assert host.unreachable() == []
    assert not (tmp_path / NAME).exists()


def test_the_default_route_and_a_gateway_summary_are_what_the_refusal_overrides(tmp_path):
    host = Host(tmp_path, v4=[route("203.0.0.0/16", gateway="192.0.2.9")], v6=[route("2001:db8::/32", gateway="fe80::1")])
    assert host.manage("present")
    assert host.unreachable() == sorted([V4, V6])


def test_a_broader_on_link_route_is_foreign(tmp_path):
    host = Host(tmp_path, v4=[route("203.0.0.0/16")])
    with pytest.raises(ValueError, match="203.0.0.0/16"):
        host.manage("present")


def test_a_route_that_appears_beside_the_running_unit_refuses_the_replay(tmp_path):
    host = Host(tmp_path)
    host.manage("present")
    host.routes[4].append(route("203.0.113.0/28"))
    with pytest.raises(ValueError, match="203.0.113.0/28"):
        host.manage("present")
    assert host.unreachable() == sorted([V4, V6])


def test_an_unanswered_route_listing_proves_nothing(tmp_path):
    host = Host(tmp_path)
    host.fail = "ip"
    with pytest.raises(ValueError, match="did not answer"):
        host.manage("present")
    assert host.observe()["answered"] is False


def test_a_deleted_route_is_restored_by_the_replay(tmp_path):
    host = Host(tmp_path)
    host.manage("present")
    host.routes[4] = [item for item in host.routes[4] if item.get("type") != "unreachable"]
    assert host.manage("present")
    assert host.observe() == held()


def test_a_changed_subnet_set_swaps_the_routes_under_the_running_unit(tmp_path):
    host = Host(tmp_path)
    host.manage("present")
    narrowed = dict(REQUEST, refusedPrefixes=[V6])
    assert host.manage("present", narrowed)
    assert host.unreachable() == [V6]
    assert host.observe(narrowed) == held(routes=[V6])
    assert not host.manage("present", narrowed)


def test_no_subnet_removes_the_unit_installed_earlier(tmp_path):
    host = Host(tmp_path)
    host.manage("present")
    assert host.manage("present", EMPTY)
    assert host.unreachable() == []
    assert not (tmp_path / NAME).exists()
    assert not host.manage("present", EMPTY)


def test_a_provider_without_managed_networks_installs_nothing(tmp_path):
    host = Host(tmp_path)
    request = {"identity": REQUEST["identity"], "networks": [{"managed": False}]}
    assert not host.manage("present", request)
    assert not host.manage("absent", request)
    assert host.calls == []


def test_a_failed_start_leaves_the_owned_unit_for_a_replay(tmp_path):
    host = Host(tmp_path)
    host.fail = "start"
    with pytest.raises(ValueError):
        host.manage("present")
    assert (tmp_path / NAME).exists()
    host.fail = ""
    assert host.manage("present")
    assert host.observe() == held()


def test_a_failed_unit_is_removed_and_its_failure_cleared(tmp_path):
    host = Host(tmp_path)
    host.manage("present")
    host.state = "failed"
    assert host.manage("absent")
    assert [refusal.SYSTEMCTL, "reset-failed", NAME] in host.calls
    assert host.observe()["present"] is False


@pytest.mark.parametrize("collision", ["content", "symlink", "wants", "dropin", "mark"])
def test_a_foreign_unit_refuses_before_mutation(tmp_path, collision):
    host = Host(tmp_path)
    path = tmp_path / NAME
    if collision == "content":
        path.write_text("foreign")
    elif collision == "symlink":
        path.symlink_to(tmp_path / "outside")
    elif collision == "wants":
        directory = tmp_path / "multi-user.target.wants"
        directory.mkdir()
        (directory / NAME).symlink_to(tmp_path / "other.service")
    elif collision == "mark":
        path.write_text("# Bootwright owned VLAN refusal: other.service\n")
        path.chmod(0o644)
    else:
        (tmp_path / (NAME + ".d")).mkdir()
    for state in ("present", "absent"):
        with pytest.raises(ValueError, match="unknown or foreign"):
            host.manage(state)
    assert all(call[1] == "show" for call in host.calls if call[0] == refusal.SYSTEMCTL)
    assert host.unreachable() == []


@pytest.mark.parametrize("request_fields", [
    {"vlanRefusalUnit": "bootwright-other-host-vlan-refusal.service"},
    {"vlanRefusalUnit": ""},
    {"refusedPrefixes": ["203.0.113.5/26"]},
    {"refusedPrefixes": [V6, V4]},
    {"refusedPrefixes": [V4, V4]},
    {"refusedPrefixes": ["0.0.0.0/0"]},
    {"refusedPrefixes": ["224.0.0.0/24"]},
])
def test_a_request_that_does_not_match_the_provider_is_refused_unobserved(tmp_path, request_fields):
    host = Host(tmp_path)
    request = dict(REQUEST, **request_fields)
    with pytest.raises(ValueError):
        host.manage("present", request)
    assert host.observe(request)["answered"] is False
    assert host.calls[:1] != [[refusal.SYSTEMCTL, "daemon-reload"]]


def unit_request(context, provider, prefixes):
    return {"identity": {"context": context, "object": provider}, "networks": [{"managed": True}],
            "vlanRefusalUnit": "bootwright-%s-%s-vlan-refusal.service" % (context, provider), "refusedPrefixes": prefixes}


def test_each_unit_has_its_own_stable_metric():
    names = [NAME, "bootwright-lab-other-vlan-refusal.service", "bootwright-two-host-vlan-refusal.service"]
    metrics = [refusal.metric(name) for name in names]
    assert len(set(metrics)) == len(names)
    assert all(refusal.METRIC_BASE <= value < refusal.METRIC_BASE + refusal.METRIC_SPAN < (1 << 32) for value in metrics)
    assert refusal.metric(NAME) == 1073741824 + int.from_bytes(__import__("hashlib").sha256(NAME.encode()).digest()[:4], "big") % (1 << 30)


def test_two_providers_of_one_context_on_one_host_refuse_one_subnet_each_with_its_own_route(tmp_path):
    host = Host(tmp_path)
    first, second = unit_request("lab", "host", [V4]), unit_request("lab", "other", [V4])
    assert host.manage("present", first)
    assert host.manage("present", second)
    assert host.observe(first)["routes"] == [V4] and host.observe(second)["routes"] == [V4]
    assert host.unreachable() == [V4, V4]
    assert host.manage("absent", second)
    assert host.observe(first) == held(routes=[V4])
    assert host.manage("absent", first)
    assert host.unreachable() == []


def test_a_unit_that_starts_while_another_holds_its_subnet_never_deletes_that_route(tmp_path, monkeypatch):
    host = Host(tmp_path)
    first, second = unit_request("lab", "host", [V4]), unit_request("lab", "other", [V4])
    assert host.manage("present", first)
    monkeypatch.setattr(refusal, "conflicts", lambda *arguments: [])
    assert host.manage("present", second)
    assert host.observe(first) == held(routes=[V4])


def test_a_context_that_adds_a_subnet_another_context_refuses_leaves_that_route_alone(tmp_path):
    host = Host(tmp_path)
    other = unit_request("two", "host", [V6])
    assert host.manage("present", other)
    assert host.manage("present", dict(REQUEST, refusedPrefixes=[V4]))
    assert host.manage("present")
    assert host.observe() == held()
    assert host.observe(other)["routes"] == [V6]
    assert host.manage("absent")
    assert host.unreachable() == [V6]
    assert host.observe(other)["state"] == "active" and host.observe(other)["routes"] == [V6]


def test_a_route_under_the_units_metric_that_the_unit_does_not_hold_refuses(tmp_path):
    stale = {"type": "unreachable", "dst": V4, "protocol": refusal.PROTOCOL, "metric": refusal.metric(NAME), "flags": []}
    host = Host(tmp_path, v4=[stale])
    with pytest.raises(ValueError, match="never replaced") as failure:
        host.manage("present")
    assert "unreachable route 203.0.113.0/26" in str(failure.value)
    assert not (tmp_path / NAME).exists()


def test_a_subnet_change_inside_the_old_one_is_not_refused_by_the_units_own_route(tmp_path):
    host = Host(tmp_path)
    assert host.manage("present", dict(REQUEST, refusedPrefixes=["203.0.112.0/23"]))
    assert host.manage("present", dict(REQUEST, refusedPrefixes=[V4]))
    assert host.unreachable() == [V4]


def test_the_route_listing_is_numeric_so_a_named_protocol_251_is_still_recognized(tmp_path):
    host = Host(tmp_path)
    assert host.manage("present")
    listings = [call for call in host.calls if call[0] == refusal.IP]
    assert listings and all(call[1:3] == ["-N", "-json"] for call in listings)
    assert host.observe() == held()


def test_numeric_types_and_tables_read_as_their_names():
    listing = [{"type": "7", "dst": V4, "protocol": "251", "metric": 5, "flags": []},
               {"type": "2", "dst": "203.0.113.5", "dev": "enp1s0", "table": "255", "protocol": "2", "flags": []},
               {"dst": "default", "gateway": "192.0.2.1", "dev": "enp1s0", "table": "254", "protocol": "16", "flags": []}]
    routes = refusal.host_routes(lambda argv, **kwargs: (0, json.dumps(listing if "-4" in argv else []), ""))
    assert [(item["type"], item["table"], item["metric"]) for item in routes] == [("unreachable", "main", 5), ("local", "local", 0), ("unicast", "main", 0)]
    assert refusal.held(routes, refusal.prefixes(REQUEST), 5) == [refusal.prefixes(REQUEST)[0]]
