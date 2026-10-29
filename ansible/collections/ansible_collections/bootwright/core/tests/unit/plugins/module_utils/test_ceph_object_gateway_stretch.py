"""The object gateway of a stretch cluster: `.rgw.root` created on the stretch rule at 4/2 under the claim and before any radosgw-admin call,
a claimed one on another rule refused, rgw daemons at both data sites, and one ingress per data site, each proved at its virtual IP.

The scripted monitor extends test_ceph_object_gateway.py's: `osd crush rule dump <rule>` prints `rule_id` and `rule_name`, `osd pool create`
places the pool on the named rule at the default 3/2, and `osd pool set` and `osd pool application enable` change the listed pool, the shapes
`osd pool ls detail` lists (src/osd/osd_types.cc:1568-1653). Whether a pool created in stretch mode accepts `size 4` is recorded as unverified
in .agents/knowledge/ceph-stretch-native-facts.md.
"""

from __future__ import annotations

import copy

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_object
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gw
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_stretch as stretch

native_boundaries = gw.native_boundaries
RULE = {"rule_id": 1, "rule_name": "stretch-rule", "type": 1, "steps": []}
DATA = ["node-1", "node-2", "node-3", "node-4"]
SITE_A, SITE_B = ["node-1", "node-2"], ["node-3", "node-4"]


def stretch_ingress(identifier, hosts, vip):
    document = gw.ingress_document(virtual_ip=vip, ssl=False)
    return dict(document, service_id="rgw.objects-" + identifier, placement={"hosts": list(hosts)})


def stretch_gateway(ingresses=()):
    request = dict(stretch.request(), action="gateway", clusterSSHTransport="verifiedTunnel", fsid=gw.FSID, owner=gw.OWNER)
    document = gw.document()
    document["placement"] = {"hosts": list(DATA)}
    request["gateway"] = {"bindings": [], "dataExtraPool": "objects.rgw.buckets.non-ec", "dataPool": "objects.rgw.buckets.data",
                          "documents": [document] + list(ingresses), "endpoint": "", "indexPool": "objects.rgw.buckets.index", "name": "objects",
                          "realm": "objects", "zone": "objects", "zoneGroup": "objects", "zonePools": list(gw.DERIVED)}
    request["delegated"] = sorted([{"name": "rgw.objects", "hosts": list(DATA)}]
                                  + [{"name": "ingress." + ingress["service_id"], "hosts": ingress["placement"]["hosts"]} for ingress in ingresses],
                                  key=lambda entry: entry["name"])
    if ingresses:
        request["auxiliary"] = copy.deepcopy(settings.AUXILIARY)
    return request


def per_site():
    return [stretch_ingress("a", SITE_A, "192.0.2.50/24"), stretch_ingress("b", SITE_B, "192.0.2.51/24")]


class StretchMonitor(gw.Monitor):
    """The gateway monitor with the stretch rule and the pool commands the stretch gateway sends."""

    def __init__(self, request, **options):
        super().__init__(request, **options)
        self.pool_effects = []
        self.config = [settings.entry(settings.HAPROXY_KEY, settings.HAPROXY), settings.entry(settings.KEEPALIVED_KEY, settings.KEEPALIVED)]

    def run(self, argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:]) if "--format" in argv else ()
        if command == ("osd", "crush", "rule", "dump", "stretch-rule"):
            self.log.append(("ceph", command, data))
            return 0, gw.json.dumps(RULE).encode()
        if command[:2] == ("osd", "pool") and command[2] in ("create", "set", "application"):
            self.log.append(("ceph", command, data))
            self.pool_effects.append(command)
            if command[2] == "create":
                self.pools.append({"pool_name": command[3], "pool_id": 9, "crush_rule": 1 if command[7] == "stretch-rule" else 0, "size": 3, "min_size": 2})
            entry = next(pool for pool in self.pools if pool["pool_name"] == ".rgw.root")
            if command[2] == "set":
                entry[command[4]] = int(command[5])
            if command[2] == "application":
                entry["application_metadata"] = {command[5]: {}}
            return 0, b""
        return super().run(argv, data, timeout)


def test_a_stretch_gateway_creates_the_rgw_root_on_the_stretch_rule_before_radosgw_admin(tmp_path):
    request = stretch_gateway()
    markers, monitor = gw.markers_for(tmp_path, request), StretchMonitor(request)
    assert gw.worker(monitor, markers).gateway_zone()["changed"] is True
    tools = [tool for tool, unused, unused in monitor.log]
    first_radosgw = tools.index("radosgw-admin")
    created = next(i for i, entry in enumerate(monitor.log) if entry[1][:3] == ("osd", "pool", "create"))
    assert created < first_radosgw and markers.read(ceph_native_object.ROOT_CLAIM) is not None
    assert monitor.pool_effects == [("osd", "pool", "create", ".rgw.root", "32", "32", "replicated", "stretch-rule", "--autoscale_mode", "on"),
                                    ("osd", "pool", "set", ".rgw.root", "size", "4"), ("osd", "pool", "application", "enable", ".rgw.root", "rgw")]
    root = next(pool for pool in monitor.pools if pool["pool_name"] == ".rgw.root")
    assert (root["crush_rule"], root["size"], root["min_size"]) == (1, 4, 2)
    monitor.pool_effects.clear()
    gw.worker(monitor, markers).gateway_zone()
    assert monitor.pool_effects == []


def test_a_gateway_off_stretch_leaves_the_rgw_root_to_radosgw_admin(tmp_path):
    request = gw.gateway_request()
    markers, monitor = gw.markers_for(tmp_path, request), gw.Monitor(request)
    gw.worker(monitor, markers).gateway_zone()
    assert not any(command[:2] == ("osd", "pool") for unused, command, unused in monitor.log if command[:4] != ("osd", "pool", "ls", "detail"))
    assert gw.worker(monitor, markers).stretch_root_ready() is True


def test_a_claimed_rgw_root_on_another_rule_refuses_stretch_pools_before_any_write(tmp_path):
    request = stretch_gateway()
    markers = gw.markers_for(tmp_path, request)
    claim = {"owner": gw.OWNER, "fsid": gw.FSID, "pool": ".rgw.root"}
    markers.write(ceph_native_object.ROOT_CLAIM, claim)
    monitor = StretchMonitor(request, pools=[{"pool_name": ".rgw.root", "pool_id": 3, "crush_rule": 0, "size": 3, "min_size": 2}])
    with pytest.raises(ceph_native.Refusal) as caught:
        gw.worker(monitor, markers).gateway_zone()
    assert str(caught.value) == "stretch-pools" and monitor.pool_effects == [] and not [m for m in monitor.mutations if m[0] == "radosgw-admin"]


def test_an_interrupted_rgw_root_completes_its_protection_under_the_claim(tmp_path):
    request = stretch_gateway()
    markers = gw.markers_for(tmp_path, request)
    markers.write(ceph_native_object.ROOT_CLAIM, {"owner": gw.OWNER, "fsid": gw.FSID, "pool": ".rgw.root"})
    monitor = StretchMonitor(request, pools=[{"pool_name": ".rgw.root", "pool_id": 3, "crush_rule": 1, "size": 3, "min_size": 2}])
    gw.worker(monitor, markers).gateway_zone()
    assert monitor.pool_effects == [("osd", "pool", "set", ".rgw.root", "size", "4"), ("osd", "pool", "application", "enable", ".rgw.root", "rgw")]


def ready_stretch(tmp_path, request, hosts):
    markers, monitor = gw.markers_for(tmp_path, request), StretchMonitor(request)
    gw.apply_all(monitor, markers)
    monitor.running = [gw.daemon(request, host) for host in hosts]
    for ingress in request["gateway"]["documents"][1:]:
        name = "ingress." + ingress["service_id"]
        monitor.running += [dict(gw.ingress_daemon(request, kind, host, {"haproxy": settings.HAPROXY, "keepalived": settings.KEEPALIVED}[kind]),
                                 service_name=name, daemon_id=ingress["service_id"] + "." + host + ".xyzabc")
                            for kind in ("haproxy", "keepalived") for host in ingress["placement"]["hosts"]]
    return markers, monitor


def test_stretch_gateway_readiness_needs_rgw_daemons_at_both_data_sites_and_the_rgw_root_at_four_and_two(tmp_path):
    request = stretch_gateway()
    request["gateway"]["documents"][0]["placement"]["count"] = 3
    markers, monitor = ready_stretch(tmp_path, request, ["node-1", "node-2", "node-4"])
    ready, blocking, unused = gw.observe(monitor, markers)
    assert ready is True and blocking == []
    monitor.running = [gw.daemon(request, host) for host in ("node-1", "node-2", "node-2")]
    monitor.running[2]["daemon_id"] = "objects.node-2.second"
    ready, blocking, unused = gw.observe(monitor, markers)
    assert ready is False and "site@rgw.objects" in blocking
    monitor.running = [gw.daemon(request, host) for host in ("node-1", "node-2", "node-4")]
    next(pool for pool in monitor.pools if pool["pool_name"] == ".rgw.root")["size"] = 3
    ready, blocking, unused = gw.observe(monitor, markers)
    assert ready is False and blocking == ["stretch@.rgw.root"] and ceph_core.blocking_entry("stretch@.rgw.root")
    next(pool for pool in monitor.pools if pool["pool_name"] == ".rgw.root")["crush_rule"] = 0
    with pytest.raises(ceph_native.Refusal, match="stretch-pools"):
        gw.observe(monitor, markers)


def test_per_site_ingresses_are_admitted_one_per_data_site_and_a_stretched_vip_refuses():
    ceph_native_object.validate(stretch_gateway(per_site()), stretch.request()["hosts"])
    stretched = [stretch_ingress("a", ["node-1", "node-3"], "192.0.2.50/24"), stretch_ingress("b", SITE_B, "192.0.2.51/24")]
    one_site = [stretch_ingress("a", SITE_A, "192.0.2.50/24"), stretch_ingress("b", ["node-2"], "192.0.2.51/24")]
    ports = per_site()
    ports[1]["spec"]["frontend_port"] = 8443
    for ingresses in (stretched, one_site, per_site()[:1], ports):
        with pytest.raises(ceph_native.Refusal):
            ceph_native_object.validate(stretch_gateway(ingresses), stretch.request()["hosts"])
    secure = stretch_gateway([dict(ingress, spec=dict(ingress["spec"], ssl=True)) for ingress in per_site()])
    secure["gateway"].update(endpoint=gw.ENDPOINT, bindings=gw.bindings("ingress.rgw.objects-a") + gw.bindings("ingress.rgw.objects-b"))
    ceph_native_object.validate(secure, stretch.request()["hosts"])
    native = ceph_native_object.ObjectWorker(secure, "node-1", run=lambda argv, data=b"", timeout=60: (0, b"[]"), markers=None)
    native.tls = {"gatewayCertificate": gw.CERT, "gatewayKey": gw.KEY}
    groups = native.unit_groups()
    assert [group[0]["spec"].get("ssl_cert") for group in groups[1:]] == [ceph_native_object.BOUND["ssl_cert"]] * 2
    assert [native.splice(group[0])["spec"]["ssl_cert"] for group in groups[1:]] == [gw.CERT] * 2
    other = copy.deepcopy(secure)
    other["gateway"]["bindings"][2:] = gw.bindings("ingress.rgw.objects-b", secret="other-tls")
    with pytest.raises(ceph_native.Refusal):
        ceph_native_object.validate(other, stretch.request()["hosts"])
    plain = gw.gateway_request()
    plain["gateway"]["documents"] += [gw.ingress_document(ssl=False), gw.ingress_document(ssl=False, service_id="rgw.second")]
    with pytest.raises(ceph_native.Refusal):
        ceph_native_object.validate(plain, plain["hosts"])


def test_every_per_site_virtual_ip_answers_before_the_gateway_is_ready(tmp_path, monkeypatch):
    request = stretch_gateway(per_site())
    markers, monitor = ready_stretch(tmp_path, request, DATA)
    probed = []
    monkeypatch.setattr(ceph_native_object, "answers", lambda address, number: probed.append(address) or address != "192.0.2.51")
    ready, blocking, evidence = gw.observe(monitor, markers)
    assert ready is False and blocking == ["vip@ingress.rgw.objects-b"] and {"192.0.2.50", "192.0.2.51"} <= set(probed)
    assert evidence["endpoint"]["reachable"] is False
    monkeypatch.setattr(ceph_native_object, "answers", lambda address, number: True)
    ready, blocking, evidence = gw.observe(monitor, markers)
    assert ready is True and evidence["endpoint"]["reachable"] is True
    names = {daemon["service"] for daemon in evidence["daemons"]}
    assert names == {"rgw.objects", "ingress.rgw.objects-a", "ingress.rgw.objects-b"}


def test_the_firewall_check_covers_every_per_site_ingress_host_in_one_window(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = stretch_gateway(per_site())
    transport = gw.Transport(request, ready_after=1)
    ceph_core.ingress_firewall(request, transport, ceph_native_object.frontends(request), lambda: 0)
    assert [call for call in transport.calls if call[1] == "ingress_firewall"] == [(host, "ingress_firewall") for host in SITE_A + SITE_B]
    hosts = {host["name"]: host for host in request["hosts"]}
    for name, document in (("node-2", per_site()[0]), ("node-3", per_site()[1])):
        native = ceph_native_object.ObjectWorker(request, name, run=lambda argv, data=b"", timeout=60: (0, b"[]"), markers=None)
        assert native.ingress_frontend()[0]["service_id"] == document["service_id"] and hosts[name]
