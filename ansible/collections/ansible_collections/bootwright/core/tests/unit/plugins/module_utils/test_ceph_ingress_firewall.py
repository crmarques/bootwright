"""The ingress firewall check: read-only, before any effect, on each ingress host; an active firewalld must permit VRRP and the client port.

The scripted host answers only the six read-only forms the check runs, in the shapes the native tools print:
- `systemctl is-active firewalld.service` prints the unit's state, with a nonzero exit unless it is active;
- `ip -json address show` lists `ifname` with `addr_info` entries holding `family`, `local` and `prefixlen` (observed from
  iproute2 6.17.0 on the loopback interface);
- `firewall-cmd --get-zone-of-interface=<if>` prints the runtime zone, or `no zone` on stderr with exit 2 for an unbound
  interface (firewalld 2.4.4 /usr/bin/firewall-cmd:3499-3511), whose traffic the default zone takes (`--get-default-zone`);
- `--info-zone=<zone>` prints the zone line, then two-space `key: value` lines, with firewalld 2.x's `ingress-priority` and
  `egress-priority` and without them in 1.x, and the rich rules each on a tab line after `rich rules:`
  (firewall/command.py:501-607); `--info-service=<service>` prints `ports`, `protocols`, `destination` and `includes`
  (command.py:638-666);
- the shipped `vrrp` service is protocol 112 with the multicast destination 224.0.0.18 (/usr/lib/firewalld/services/vrrp.xml).
cephadm opens a deployed daemon's TCP ports only in the default zone and never VRRP (src/cephadm/cephadm.py:976-983,
src/cephadm/cephadmlib/firewalld.py:84-119), and puts the virtual IP on the interface whose network holds it, else on one of
`virtual_interface_networks` (src/pybind/mgr/cephadm/services/ingress.py:306-340).
"""

from __future__ import annotations

import copy
import ipaddress
import json
import os
import pathlib

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_object, ceph_native_units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as mgmt
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_nfs_exports as exports
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateways
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings

NativeOutput = ceph_native.NativeOutput
SYSTEMCTL = ["/usr/bin/systemctl", "is-active", "firewalld.service"]
ADDRESSES = ["/usr/sbin/ip", "-json", "address", "show"]
FORBIDDEN = ("--permanent", "--add", "--remove", "--reload", "--set", "--change", "--query")
VRRP = "VRRP (IP protocol 112)"
INTERFACES = [("lo", "127.0.0.1", 8), ("eth0", "192.0.2.12", 24), ("eth1", "198.51.100.12", 24)]


def zone_text(name="public", target="default", services="dhcpv6-client ssh", ports="", protocols="", rules=(), priorities=True):
    """`firewall-cmd --info-zone` as firewalld prints it; 2.x adds the priority lines."""
    lines = [name + " (default, active)", "  target: " + target]
    if priorities:
        lines += ["  ingress-priority: 0", "  egress-priority: 0"]
    lines += ["  icmp-block-inversion: no", "  interfaces: eth0", "  sources: ", "  services: " + services, "  ports: " + ports,
              "  protocols: " + protocols, "  forward: yes", "  masquerade: no", "  forward-ports: ", "  source-ports: ", "  icmp-blocks: ",
              "  rich rules: "]
    lines += ["\t" + rule for rule in rules]
    return "\n".join(lines) + "\n"


def service_text(name, ports="", protocols="", destination="", includes=""):
    return "\n".join([name, "  ports: " + ports, "  protocols: " + protocols, "  source-ports: ", "  modules: ", "  destination: " + destination,
                      "  includes: " + includes, "  helpers: "]) + "\n"


SERVICES = {
    "ssh": service_text("ssh", ports="22/tcp"),
    "dhcpv6-client": service_text("dhcpv6-client", ports="546/udp", destination="ipv6:fe80::/64"),
    "nfs": service_text("nfs", ports="2049/tcp"),
    "nfs-bundle": service_text("nfs-bundle", includes="nfs"),
    "keepalived": service_text("keepalived", protocols="112"),
    "vrrp": service_text("vrrp", protocols="112", destination="ipv4:224.0.0.18 ipv6:FF02:0:0:0:0:0:0:12"),
}


class Host:
    """A scripted ingress host answering only the read-only forms; any other argv fails the test."""

    def __init__(self, state="active", interfaces=None, zones=None, default="public", infos=None):
        self.state = state
        self.interfaces = INTERFACES if interfaces is None else interfaces
        self.zones = {"eth0": "public", "eth1": "internal"} if zones is None else zones
        self.default = default
        self.infos = {"public": zone_text(), "internal": zone_text("internal", target="ACCEPT")}
        self.infos.update(infos or {})
        self.log = []

    def addresses(self):
        rows = {}
        for name, local, prefix in self.interfaces:
            rows.setdefault(name, {"ifindex": len(rows) + 1, "ifname": name, "flags": ["UP"], "addr_info": []})
            rows[name]["addr_info"].append({"family": "inet6" if ":" in local else "inet", "local": local, "prefixlen": prefix, "scope": "global"})
        return list(rows.values())

    def run(self, argv, data=b"", timeout=60):
        assert data == b"" and 0 < timeout <= 20
        self.log.append(tuple(argv))
        if argv == SYSTEMCTL:
            return (0 if self.state == "active" else 3), NativeOutput(self.state if isinstance(self.state, bytes) else self.state.encode() + b"\n")
        if argv == ADDRESSES:
            return 0, NativeOutput(json.dumps(self.addresses()).encode())
        if len(argv) != 2 or argv[0] != "/usr/bin/firewall-cmd":
            pytest.fail("the check ran " + repr(argv))
        option, unused, value = argv[1].partition("=")
        if option == "--get-zone-of-interface":
            zone = self.zones.get(value)
            if zone is None:
                return 2, NativeOutput(b"", b"no zone\n")
            return zone if isinstance(zone, tuple) else (0, NativeOutput(zone.encode() + b"\n"))
        if argv[1] == "--get-default-zone":
            return 0, NativeOutput(self.default.encode() + b"\n")
        if option == "--info-zone":
            return 0, NativeOutput(self.infos[value].encode())
        if option == "--info-service":
            return 0, NativeOutput(SERVICES[value].encode())
        return pytest.fail("the check ran " + repr(argv))


def nfs_request(mode="haproxy"):
    return units.unit_request(mode)


def gateway_request():
    return gateways.tls_request(True)


def plain_ingress():
    """A gateway behind an ingress without TLS or an endpoint name, as the scripted transport observes it."""
    request = gateways.gateway_request()
    request["gateway"]["documents"].append(gateways.ingress_document(ssl=False))
    request["delegated"] = [{"name": gateways.INGRESS, "hosts": list(gateways.HOSTS)}, {"name": "rgw.objects", "hosts": list(gateways.HOSTS)}]
    request["auxiliary"] = copy.deepcopy(settings.AUXILIARY)
    return ceph_native.validate(request)


def native(request, host, name, markers=None):
    kind = ceph_native_object.ObjectWorker if request["action"] == "gateway" else ceph_native_units.UnitWorker
    return kind(request, name, run=host.run, markers=markers)


def refused(request, host, name="node-2"):
    with pytest.raises(ceph_native.NativeFailure) as caught:
        native(request, host, name).ingress_firewall()
    return caught.value


def with_ingress(request, **spec):
    documents = request["gateway"]["documents"] if request["action"] == "gateway" else request["service"]["documents"]
    documents[0 if request["action"] == "service" else 1]["spec"].update(spec)
    return ceph_native.validate(request)


def allowed(argv):
    if argv in (tuple(SYSTEMCTL), tuple(ADDRESSES), ("/usr/bin/firewall-cmd", "--get-default-zone")):
        return True
    queries = ("--get-zone-of-interface", "--info-zone", "--info-service")
    return len(argv) == 2 and argv[0] == "/usr/bin/firewall-cmd" and argv[1].partition("=")[0] in queries


@pytest.mark.parametrize("priorities", [True, False])
def test_ingress_refuses_when_firewalld_is_active_and_vrrp_is_not_permitted(priorities):
    nfs = Host(infos={"public": zone_text(ports="2049/tcp", services="ssh vrrp", priorities=priorities)})
    error = refused(nfs_request(), nfs, "node-1")
    assert str(error) == "ingress-firewall" and error.native_error["cause"].endswith("does not permit " + VRRP)
    gateway = Host(infos={"public": zone_text(ports="443/tcp", priorities=priorities)})
    error = refused(gateway_request(), gateway, "node-2")
    assert str(error) == "ingress-firewall" and error.native_error["cause"].endswith("does not permit " + VRRP)


@pytest.mark.parametrize(("request_of", "port", "covering"), [
    (lambda: nfs_request("haproxy"), 2049, "2000-2100/tcp"),
    (lambda: nfs_request("keepalive"), 2050, "2000-2100/tcp"),
    (gateway_request, 443, "400-500/tcp"),
])
def test_ingress_refuses_when_a_frontend_port_is_not_permitted(request_of, port, covering):
    others = "9049/tcp 1967/tcp 12049/tcp" + ("" if port == 2049 else " 2049/tcp")
    host = Host(infos={"public": zone_text(ports=others, protocols="vrrp")})
    error = refused(request_of(), host)
    assert str(error) == "ingress-firewall"
    assert error.native_error["cause"] == "host node-2: interface eth0 in firewalld zone public does not permit " + str(port) + "/tcp"
    host = Host(infos={"public": zone_text(ports=covering, protocols="vrrp")})
    assert native(request_of(), host, "node-2").ingress_firewall() == {"changed": False}


def test_ingress_firewall_check_is_read_only(tmp_path):
    request = nfs_request()
    markers = units.markers_for(tmp_path, request)
    before = units.records(markers)
    refusing = Host(zones={"eth0": "public"}, infos={"public": zone_text(services="ssh nfs-bundle dhcpv6-client")})
    with pytest.raises(ceph_native.NativeFailure, match="^ingress-firewall$"):
        native(request, refusing, "node-1", markers).ingress_firewall()
    passing = Host(zones={}, infos={"public": zone_text(services="keepalived nfs")})
    assert native(request, passing, "node-2", markers).ingress_firewall() == {"changed": False}
    logs = refusing.log + passing.log
    forms = {argv[1].partition("=")[0] for argv in logs if len(argv) == 2}
    assert forms == {"--get-zone-of-interface", "--get-default-zone", "--info-zone", "--info-service"}
    assert all(allowed(argv) for argv in logs)
    assert not [argv for argv in logs if any(word.startswith(FORBIDDEN) for word in argv)]
    assert not [argv for argv in logs if os.path.basename(argv[0]) in ("ceph", "cephadm", "radosgw-admin")]
    assert units.records(markers) == before


def test_ingress_firewall_refusal_names_host_protocol_and_port():
    host = Host()
    error = refused(nfs_request(), host)
    cause = error.native_error["cause"]
    assert cause == "host node-2: interface eth0 in firewalld zone public does not permit VRRP (IP protocol 112) and 2049/tcp"
    for word in ("host node-2", "interface eth0", "firewalld zone public", VRRP, "2049/tcp"):
        assert word in cause
    assert "192.0.2." not in cause and "198.51.100." not in cause
    assert ceph_core.refusal(error, ceph_core.CLUSTER_REASONS)[0] == "ingress-firewall"


class Calls:
    """Wraps a scripted transport to record each firewall call's timeout."""

    def __init__(self, transport, refuse=None):
        self.transport, self.timeouts, self.refuse = transport, [], refuse
        self.calls = transport.calls

    def gateway_material(self, key):
        return self.transport.gateway_material(key)

    def call(self, host, phase, private=None, *, timeout=None):
        if phase == "ingress_firewall":
            self.timeouts.append(timeout)
            if host["name"] == self.refuse:
                self.calls.append((host["name"], phase))
                raise ceph_native.RemoteRefusal("native-refusal:ingress-firewall@" + host["name"])
        if private is None:
            return self.transport.call(host, phase, timeout=timeout)
        return self.transport.call(host, phase, private, timeout=timeout)


def execute(request, transport, now=lambda: 0):
    return ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=lambda seconds: None)


@pytest.mark.parametrize("state", ["inactive", "failed"])
def test_inactive_firewalld_plans_and_applies(monkeypatch, state):
    for request, name in ((nfs_request(), "node-1"), (gateway_request(), "node-3")):
        host = Host(state=state)
        assert native(request, host, name).ingress_firewall() == {"changed": False}
        assert host.log == [tuple(SYSTEMCTL)]
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = nfs_request()
    transport = Calls(units.Transport(request))
    execute(request, transport)
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "ingress_firewall"), ("node-2", "ingress_firewall"), ("node-1", "unit"),
                               ("node-1", "observe"), ("node-1", "vip_holder"), ("node-2", "vip_holder")]
    assert transport.timeouts == [120, 120]
    request = plain_ingress()
    transport = Calls(gateways.Transport(request))
    execute(request, transport)
    assert transport.calls == [("node-1", "prerequisites")] + [(name, "ingress_firewall") for name in gateways.HOSTS] + [
        ("node-1", "gateway_zone"), ("node-1", "gateway_placement"), ("node-1", "gateway_service"), ("node-1", "observe")] + [
        (name, "vip_holder") for name in gateways.HOSTS]
    assert transport.timeouts == [120, 120, 120]


@pytest.mark.parametrize(("zone", "missing"), [
    ({"target": "ACCEPT"}, None),
    ({"protocols": "vrrp", "ports": "2049/tcp"}, None),
    ({"protocols": "112", "ports": "2049/tcp"}, None),
    ({"services": "ssh keepalived", "ports": "2049/tcp"}, None),
    ({"services": "nfs", "protocols": "vrrp"}, None),
    ({"rules": ['rule protocol value="vrrp" accept'], "ports": "2049/tcp"}, None),
    ({"rules": ['rule family="ipv4" protocol value="vrrp" accept'], "ports": "2049/tcp"}, None),
    ({"rules": ['rule port port="2049" protocol="tcp" accept'], "protocols": "vrrp"}, None),
    ({"rules": ['rule family="ipv4" port port="2000-2100" protocol="tcp" accept'], "protocols": "vrrp"}, None),
    ({"services": "nfs-bundle", "protocols": "vrrp"}, None),
    ({"rules": ['rule family="ipv6" protocol value="vrrp" accept'], "ports": "2049/tcp"}, VRRP),
    ({"rules": ['rule family="ipv4" source address="192.0.2.0/24" protocol value="vrrp" accept'], "ports": "2049/tcp"}, VRRP),
    ({"rules": ['rule family="ipv6" port port="2049" protocol="tcp" accept'], "protocols": "vrrp"}, "2049/tcp"),
    ({"services": "ssh vrrp", "ports": "2049/tcp"}, VRRP),
    ({"protocols": "vrrp", "ports": "2049/udp"}, "2049/tcp"),
    ({"rules": ['rule port port="2049" protocol="udp" accept'], "protocols": "vrrp"}, "2049/tcp"),
])
def test_ingress_firewall_accepts_each_permitting_form(zone, missing):
    host = Host(infos={"public": zone_text(**zone)})
    if missing is None:
        assert native(nfs_request(), host, "node-1").ingress_firewall() == {"changed": False}
        return
    error = refused(nfs_request(), host, "node-1")
    assert error.native_error["cause"] == "host node-1: interface eth0 in firewalld zone public does not permit " + missing


def test_ingress_firewall_reads_the_default_zone_for_an_unbound_interface():
    host = Host(zones={}, default="trusted", infos={"trusted": zone_text("trusted", protocols="vrrp", ports="2049/tcp")})
    assert native(nfs_request(), host, "node-1").ingress_firewall() == {"changed": False}
    assert host.log[2:] == [("/usr/bin/firewall-cmd", "--get-zone-of-interface=eth0"), ("/usr/bin/firewall-cmd", "--get-default-zone"),
                            ("/usr/bin/firewall-cmd", "--info-zone=trusted")]
    host = Host(zones={}, default="public")
    assert "in firewalld zone public does not permit" in refused(nfs_request(), host).native_error["cause"]
    for answer in ((1, NativeOutput(b"", b"Error: INVALID_INTERFACE: eth0\n")), (2, NativeOutput(b"", b"Error: NOT_RUNNING\n")),
                   (0, NativeOutput(b"no zone\n"))):
        host = Host(zones={"eth0": answer})
        with pytest.raises(ceph_native.NativeFailure, match="^native-command$"):
            native(nfs_request(), host, "node-1").ingress_firewall()
        assert not [argv for argv in host.log if argv[-1] == "--get-default-zone"]


def test_ingress_firewall_finds_the_vip_interface_like_cephadm():
    host = Host(zones={"eth0": "public", "eth1": "drop"})
    refused(nfs_request(), host)
    checked = [argv[1] for argv in host.log if argv[0] == "/usr/bin/firewall-cmd"]
    assert checked == ["--get-zone-of-interface=eth0", "--info-zone=public", "--info-service=dhcpv6-client", "--info-service=ssh"]
    elsewhere = with_ingress(nfs_request(), virtual_ip="203.0.113.50/24", virtual_interface_networks=["198.51.100.0/24"])
    host = Host(zones={"eth0": "drop", "eth1": "public"})
    assert "interface eth1 in firewalld zone public" in refused(elsewhere, host).native_error["cause"]
    assert ("/usr/bin/firewall-cmd", "--get-zone-of-interface=eth0") not in host.log
    for request in (with_ingress(nfs_request(), virtual_ip="203.0.113.50/24"), with_ingress(gateway_request(), virtual_ip="203.0.113.50/24")):
        host = Host(zones={"eth0": "public"})
        assert native(request, host, "node-2").ingress_firewall() == {"changed": False}
        assert host.log == [tuple(SYSTEMCTL), tuple(ADDRESSES)]
    many = [("eth" + str(index), "192.0.2." + str(20 + index), 24) for index in range(9)]
    for interfaces in (many, many[:7] + [("bad/name", "192.0.2.40", 24)]):
        with pytest.raises(ceph_native.Refusal, match="^native-output$"):
            native(nfs_request(), Host(interfaces=interfaces), "node-1").ingress_firewall()
    assert native(nfs_request(), Host(interfaces=many[:8], zones={}, default="internal"), "node-1").ingress_firewall() == {"changed": False}


def test_ingress_firewall_refuses_an_unsettled_firewalld():
    host = Host(state="activating")
    error = refused(nfs_request(), host)
    assert str(error) == "ingress-firewall" and error.native_error["cause"] == "host node-2: firewalld.service is activating"
    assert host.log == [tuple(SYSTEMCTL)]
    error = refused(nfs_request(), Host(state=b"Failed to connect to bus\n"))
    assert error.native_error["cause"] == "host node-2: firewalld.service is unknown"


def test_dispatch_admits_the_firewall_phase_only_on_an_ingress_host(monkeypatch):
    reached = []
    monkeypatch.setattr(ceph_native_units, "firewall_check", lambda worker, spec, ports: reached.append((type(worker), ports)))
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)

    def dispatch(request, host, private=None):
        return ceph_native.dispatch({"request": request, "host": host, "phase": "ingress_firewall", "private": private or {}})

    assert dispatch(nfs_request(), "node-2") == {"changed": False}
    assert dispatch(gateway_request(), "node-3") == {"changed": False}
    assert reached == [(ceph_native_units.UnitWorker, [2049]), (ceph_native_object.ObjectWorker, [443])]
    export = exports.export_request()
    for request, host in ((nfs_request(), "node-3"), (nfs_request("direct"), "node-1"), (gateways.gateway_request(), "node-1")):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            dispatch(request, host)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.dispatch({"request": export, "host": "node-1", "phase": "ingress_firewall", "private": {}})
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        dispatch(gateway_request(), "node-1", {"gatewayCertificate": gateways.CERT})
    assert len(reached) == 2


def test_core_skips_the_firewall_check_without_an_ingress(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = nfs_request("direct")
    transport = Calls(units.Transport(request))
    execute(request, transport)
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "unit"), ("node-1", "observe")]
    request = gateways.gateway_request()
    transport = Calls(gateways.Transport(request))
    execute(request, transport)
    assert transport.calls == [("node-1", "prerequisites"), ("node-1", "gateway_zone"), ("node-1", "gateway_placement"),
                               ("node-1", "gateway_service"), ("node-1", "observe")]
    for request, make in ((nfs_request(), units.Transport), (plain_ingress(), gateways.Transport)):
        transport = Calls(make(request), refuse="node-2")
        with pytest.raises(ceph_native.RemoteRefusal) as caught:
            execute(request, transport)
        assert transport.calls == [("node-1", "prerequisites"), ("node-1", "ingress_firewall"), ("node-2", "ingress_firewall")]
        assert ceph_core.refusal(caught.value, ceph_core.CLUSTER_REASONS)[0] == "ingress-firewall"
        clock = iter([0, 0, 601])
        transport = Calls(make(request))
        with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
            execute(request, transport, now=lambda: next(clock))
        assert transport.calls == [("node-1", "prerequisites"), ("node-1", "ingress_firewall")]


GRANTS = json.loads((pathlib.Path(__file__).resolve().parents[2] / "goldens" / "ingress_firewall_grants.json").read_text(encoding="utf-8"))


class ZoneReader:
    """Answers only `--info-zone` and `--info-service` from the shared grant corpus, which the Go preflight evaluation also reads."""

    host = {"name": "node-1"}

    def __init__(self, zone):
        self.zone, self.log = zone, []

    def native_coordinate(self):
        return {"host": "node-1"}

    def command(self, argv, json_result=True, timeout=60):
        assert json_result is False and 0 < timeout <= 20 and argv[0] == "/usr/bin/firewall-cmd" and len(argv) == 2
        self.log.append(argv[1])
        option, unused, value = argv[1].partition("=")
        lines = self.zone if option == "--info-zone" else GRANTS["services"][value]
        return NativeOutput(("\n".join(lines) + "\n").encode())


@pytest.mark.parametrize("case", GRANTS["cases"], ids=[case["case"] for case in GRANTS["cases"]])
def test_zone_grants_follow_the_shared_corpus(case):
    assert case["protocols"] == ["vrrp"] and all(transport == "tcp" for unused, transport in case["ports"])
    vip = ipaddress.ip_address("192.0.2.50" if case["family"] == "ipv4" else "2001:db8::50")
    reader = ZoneReader(case["zone"])
    missing = ceph_native_units.zone_missing(reader, "public", vip, [number for number, unused in case["ports"]], {})
    assert missing == case["missing"]
    assert reader.log[0] == "--info-zone=public" and all(entry.startswith("--info-service=") for entry in reader.log[1:])


def management_request():
    return mgmt.gateway_request()


def test_a_management_ingress_zone_must_permit_the_internal_port():
    request = management_request()
    assert request["service"]["ingressPorts"] == [29443]
    host = Host(infos={"public": zone_text(protocols="vrrp", ports="9443/tcp")})
    error = refused(request, host, "node-2")
    assert str(error) == "ingress-firewall"
    assert error.native_error["cause"] == "host node-2: interface eth0 in firewalld zone public does not permit 29443/tcp"
    host = Host(infos={"public": zone_text()})
    assert refused(request, host, "node-2").native_error["cause"].endswith("does not permit " + VRRP + " and 9443/tcp and 29443/tcp")
    host = Host(infos={"public": zone_text(protocols="vrrp", ports="9443/tcp 29443/tcp")})
    assert native(request, host, "node-2").ingress_firewall() == {"changed": False}


def test_a_named_service_in_the_zone_grants_the_ingress_requirements():
    name = "bootwright-example-os-firewall-node-1"
    SERVICES[name] = "\n".join(GRANTS["services"][name]) + "\n"
    try:
        host = Host(infos={"public": zone_text(services="ssh " + name)})
        assert native(management_request(), host, "node-1").ingress_firewall() == {"changed": False}
        assert ("/usr/bin/firewall-cmd", "--info-service=" + name) in host.log
        error = refused(nfs_request(), Host(infos={"public": zone_text(services="ssh " + name)}), "node-1")
        assert error.native_error["cause"].endswith("does not permit 2049/tcp")
    finally:
        del SERVICES[name]


def test_ingress_ports_are_carried_only_by_a_management_gateway_with_its_ingress():
    for change in (lambda unit: unit.update(ingressPorts=[]), lambda unit: unit.update(ingressPorts=[29443, 29443]),
                   lambda unit: unit.update(ingressPorts=[0]), lambda unit: unit.update(ingressPorts=["29443"])):
        request = management_request()
        change(request["service"])
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate(request)
    direct = mgmt.gateway_request(None)
    direct["service"]["ingressPorts"] = [29443]
    nfs = nfs_request()
    nfs["service"]["ingressPorts"] = [29443]
    for request in (direct, nfs):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_native.validate(request)


def vip_of(request):
    return ceph_core.ingress_fronts(request)[0][0]["spec"]["virtual_ip"].partition("/")[0]


def test_vip_holder_reads_whether_this_host_carries_the_virtual_ip():
    request = nfs_request()
    holding = Host(interfaces=INTERFACES + [("eth0", vip_of(request), 24)])
    assert native(request, holding, "node-1").vip_holder() == {"changed": False, "holds": True}
    standby = Host()
    assert native(request, standby, "node-2").vip_holder() == {"changed": False, "holds": False}
    assert holding.log == [tuple(ADDRESSES)] and standby.log == [tuple(ADDRESSES)]
    gateway = Host(interfaces=INTERFACES + [("eth1", vip_of(gateway_request()), 32)])
    assert native(gateway_request(), gateway, "node-2").vip_holder() == {"changed": False, "holds": True}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        native(nfs_request(), Host(), "node-3").vip_holder()


class Holders(units.Transport):
    """A scripted unit transport whose ingress hosts each say whether they hold the virtual IP."""

    def __init__(self, request, holders):
        super().__init__(request)
        self.holders = holders

    def call(self, host, phase, timeout=None):
        if phase == "vip_holder":
            self.calls.append((host["name"], phase))
            assert 0 < timeout <= ceph_core.HOLDER_CALL
            return {"changed": False, "holds": host["name"] in self.holders}
        return super().call(host, phase, timeout=timeout)


@pytest.mark.parametrize(("holders", "entry"), [((), "noholder@ingress.nfs.files"), (("node-1", "node-2"), "holders@ingress.nfs.files")])
def test_readiness_needs_exactly_one_holder_per_virtual_ip(monkeypatch, holders, entry):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = nfs_request()
    assert ceph_native_units.PENDING.fullmatch(entry)
    clock = iter(range(0, 100000, 100))
    transport = Holders(request, holders)
    with pytest.raises(ceph_core.ReadinessRefusal) as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    assert caught.value.checks == [entry]
    assert ("node-1", "vip_holder") in transport.calls and ("node-2", "vip_holder") in transport.calls
    assert ceph_core.readiness_detail(caught.value.checks) == "not-ready:" + entry


def test_readiness_passes_with_one_holder_and_keeps_the_endpoint_proof(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = nfs_request()
    transport = Holders(request, ("node-2",))
    result = execute(request, transport)
    assert result["evidence"]["ready"] is True
    assert transport.calls[-2:] == [("node-1", "vip_holder"), ("node-2", "vip_holder")]
    transport = Holders(nfs_request("direct"), ())
    assert execute(nfs_request("direct"), transport)["evidence"]["ready"] is True
    assert "vip_holder" not in [phase for unused, phase in transport.calls]


def test_the_holder_proof_asks_only_the_hosts_running_the_ingress_keepalived():
    request = nfs_request()
    ingress = ceph_core.ingress_fronts(request)[0][0]
    running = [row for row in units.keepalived_rows(request) if row["host"] == "node-2"]
    idle = [name for name in ingress["placement"]["hosts"] if name != "node-2"]
    asked = []

    class Down:
        def call(self, host, phase, timeout=None):
            asked.append(host["name"])
            if host["name"] in idle:
                raise ceph_native.Refusal("transport")
            return {"changed": False, "holds": True}

    assert idle and ceph_core.vip_holders(request, {"daemons": running}, Down(), 900, lambda: 0) == []
    assert asked == ["node-2"]
    assert ceph_core.vip_holders(request, {"daemons": []}, Down(), 900, lambda: 0) == ["noholder@" + ceph_core.ingress_name(ingress)]
    every = units.keepalived_rows(request)
    assert ceph_core.vip_holders(request, {"daemons": every}, Down(), 900, lambda: 0) == ["unreachable@" + name for name in idle]
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.vip_holders(request, {"daemons": [dict(running[0], host="node-9")]}, Down(), 900, lambda: 0)


def test_an_unreachable_keepalived_host_blocks_readiness_naming_it(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = nfs_request()

    class Unreachable(units.Transport):
        def call(self, host, phase, timeout=None):
            if phase == "vip_holder" and host["name"] == "node-2":
                self.calls.append((host["name"], phase))
                raise ceph_native.Refusal("transport")
            return super().call(host, phase, timeout=timeout)

    clock = iter(range(0, 100000, 100))
    with pytest.raises(ceph_core.ReadinessRefusal) as caught:
        execute(request, Unreachable(request), now=lambda: next(clock))
    assert caught.value.checks == ["unreachable@node-2"]
    assert ceph_core.readiness_detail(caught.value.checks) == "not-ready:unreachable@node-2"


class AddressReader:
    """Answers only `ip -json address show` from one shared selection case."""

    host = {"name": "node-1"}

    def __init__(self, addresses):
        self.rows = []
        for name, local, prefix in addresses:
            self.rows.append({"ifname": name, "addr_info": [{"family": "inet6" if ":" in local else "inet", "local": local, "prefixlen": prefix}]})

    def command(self, argv, timeout=60):
        assert argv == ADDRESSES and 0 < timeout <= 20
        return copy.deepcopy(self.rows)


@pytest.mark.parametrize("case", GRANTS["selection"], ids=[case["case"] for case in GRANTS["selection"]])
def test_interface_selection_follows_the_shared_corpus(case):
    vip, networks = case["networks"][0], case["networks"][1:]
    assert vip.endswith("/32") or vip.endswith("/128")
    spec = {"virtual_ip": vip.partition("/")[0]}
    if networks:
        spec["virtual_interface_networks"] = networks
    assert ceph_native_units.vip_interfaces(AddressReader(case["addresses"]), spec)[1] == case["interfaces"]
