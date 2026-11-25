"""The management gateway's Grafana routes: an exact-anchor witness selects the two owned template overrides, they render one Host header
and a pinned Grafana under cephadm's Jinja settings, they complete before the gateway's stream, and every running gateway daemon proves
both in the files it mounts before readiness.

The templates below are synthetic text shaped at the exact anchors of cephadm's v20.2.1 mgmt-gateway templates
(src/pybind/mgr/cephadm/templates/services/mgmt-gateway/nginx.conf.j2:45-51 and external_server.conf.j2:116-147), never provider bytes.
cephadm renders a stored template through the same Jinja environment as its own (src/pybind/mgr/cephadm/template.py: trim_blocks,
lstrip_blocks, StrictUndefined), and its mgmt-gateway daemon mounts `<data_dir>/etc/nginx.conf` and `<data_dir>/etc/nginx_external_server.conf`
at `/etc/nginx/nginx.conf` and `/etc/nginx_external_server.conf` and runs as the files' owner (src/cephadm/cephadmlib/daemons/mgmt_gateway.py).
"""

from __future__ import annotations

import contextlib
import copy
import json
import os
import threading

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_monitoring_template as template, ceph_native, ceph_native_observability as observability, ceph_native_units,
    native_specializations,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

FSID = units.FSID
NGINX_SOURCE = """http {
{% if grafana_endpoints %}
    upstream grafana_servers {
     {% for ep in grafana_endpoints %}
     server {{ ep }};
     {% endfor %}
    }
{% endif %}
{% if prometheus_endpoints %}
    upstream prometheus_servers {
     ip_hash;
     {% for ep in prometheus_endpoints %}
     server {{ ep }};
     {% endfor %}
    }
{% endif %}
}
"""
EXTERNAL_SOURCE = """server {
{% if grafana_endpoints %}
    location /grafana {
        proxy_pass {{ grafana_scheme }}://grafana_servers;
        proxy_set_header Authorization "";
        proxy_buffering off;
        {% if enable_oauth2_proxy %}
        auth_request /oauth2/auth;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        {% endif %}
    }
{% endif %}
}
"""
SOURCES = {"nginx": NGINX_SOURCE, "external": EXTERNAL_SOURCE}
CONTEXT = {"grafana_endpoints": ["192.0.2.11:3000", "192.0.2.12:3000"], "prometheus_endpoints": ["192.0.2.11:9095"], "grafana_scheme": "https"}
FIXED_HOST = EXTERNAL_SOURCE.replace(template.PROXIED, "        proxy_set_header Host $host;\n" + template.PROXIED)
PINNED = NGINX_SOURCE.replace(template.UPSTREAM, template.UPSTREAM + "     hash $remote_addr consistent;\n")


def selection(value):
    return native_specializations.Selection({"image-gateway": lambda: value}, {"image-gateway": template.grafana_unrouted})


# What cephadm's Jinja environment renders from each template under CONTEXT, keyed by (file, corrected, oauth2-proxy declared); the
# rendering test proves each one where jinja2 is installed, so the managed-host floor without it still exercises the proofs.
UPSTREAM_NATIVE = "http {\n    upstream grafana_servers {\n     server 192.0.2.11:3000;\n     server 192.0.2.12:3000;\n    }\n" \
    "    upstream prometheus_servers {\n     ip_hash;\n     server 192.0.2.11:9095;\n    }\n}"
UPSTREAM_ROUTED = UPSTREAM_NATIVE.replace("grafana_servers {\n", "grafana_servers {\n     ip_hash;\n")
LOCATION_NATIVE = "server {\n    location /grafana {\n        proxy_pass https://grafana_servers;\n        proxy_set_header Authorization \"\";\n" \
    "        proxy_buffering off;\n    }\n}"
LOCATION_PROXIED = LOCATION_NATIVE.replace("off;\n", "off;\n        auth_request /oauth2/auth;\n        proxy_set_header Host $host;\n"
                                           "        proxy_set_header X-Forwarded-Proto $scheme;\n")
LOCATION_ROUTED = LOCATION_NATIVE.replace("grafana_servers;\n", "grafana_servers;\n        proxy_set_header Host $host;\n")
RENDERED = {("nginx", False, False): UPSTREAM_NATIVE, ("nginx", False, True): UPSTREAM_NATIVE, ("nginx", True, False): UPSTREAM_ROUTED,
            ("nginx", True, True): UPSTREAM_ROUTED, ("external", False, False): LOCATION_NATIVE, ("external", False, True): LOCATION_PROXIED,
            ("external", True, False): LOCATION_ROUTED, ("external", True, True): LOCATION_PROXIED}


def render(kind, corrected, proxied):
    return RENDERED[(kind, corrected, proxied)]


def test_each_witnessed_defect_selects_its_own_correction():
    chosen = selection(SOURCES)
    assert chosen.variant("ceph.gateway.grafana-route") == "host-and-affinity" and chosen.applied == {"ceph-gateway-grafana-sessions"}
    assert template.gateway_witness(SOURCES) == {"nginx": True, "external": True}
    assert template.gateway_witness(dict(SOURCES, external=FIXED_HOST)) == {"nginx": True, "external": False}
    assert template.gateway_witness(dict(SOURCES, nginx=PINNED)) == {"nginx": False, "external": True}
    assert template.grafana_unrouted(dict(SOURCES, external=FIXED_HOST)) is template.grafana_unrouted(dict(SOURCES, nginx=PINNED)) is True
    assert template.grafana_unrouted(dict(nginx=PINNED, external=FIXED_HOST)) is False
    unknown = {
        "repeated location": dict(SOURCES, external=EXTERNAL_SOURCE + template.LOCATION + template.PASS + template.BLOCK_END),
        "moved proxy_pass": dict(SOURCES, external=EXTERNAL_SOURCE.replace(template.PASS, "").replace(
            "proxy_buffering off;\n", "proxy_buffering off;\n" + template.PASS)),
        "nested branch": dict(SOURCES, external=EXTERNAL_SOURCE.replace("auth_request", "{% if x %}{% endif %}auth_request")),
        "second branch": dict(SOURCES, external=EXTERNAL_SOURCE.replace(
            "        proxy_buffering", "        {% if y %}\n        {% endif %}\n        proxy_buffering")),
    }
    for name, value in unknown.items():
        assert template.gateway_witness(value)["external"] is None and template.grafana_unrouted(value) is True, name
        assert template.grafana_unrouted(dict(value, nginx=PINNED)) is None, name
    absent = dict(SOURCES, nginx=NGINX_SOURCE.replace("upstream grafana_servers", "upstream grafana"))
    assert template.gateway_witness(absent)["nginx"] is None and template.grafana_unrouted(dict(absent, external=FIXED_HOST)) is None
    for value in (dict(nginx=PINNED, external=FIXED_HOST), dict(absent, external=FIXED_HOST), None, {}, {"nginx": NGINX_SOURCE},
                  dict.fromkeys(SOURCES, None), dict.fromkeys(SOURCES, "x" * (template.MAX_SOURCE + 1))):
        chosen = selection(value)
        assert chosen.variant("ceph.gateway.grafana-route") == "native" and not chosen.applied


@pytest.mark.parametrize("proxied", [False, True])
def test_cephadms_jinja_settings_render_the_corrected_templates_as_proved(proxied):
    jinja2 = pytest.importorskip("jinja2")
    environment = jinja2.Environment(autoescape=False, trim_blocks=True, lstrip_blocks=True, undefined=jinja2.StrictUndefined)
    for kind, native, corrected in (("nginx", NGINX_SOURCE, template.routed_upstream(NGINX_SOURCE)),
                                    ("external", EXTERNAL_SOURCE, template.routed_location(EXTERNAL_SOURCE))):
        for routed, text in ((False, native), (True, corrected.decode())):
            assert environment.from_string(text).render(dict(CONTEXT, enable_oauth2_proxy=proxied)) == render(kind, routed, proxied)


@pytest.mark.parametrize("proxied", [False, True])
def test_the_corrections_render_one_host_header_and_a_pinned_grafana(proxied):
    nginx, external = template.routed_upstream(NGINX_SOURCE), template.routed_location(EXTERNAL_SOURCE)
    assert nginx.decode().replace(template.AFFINITY, "", 1) == NGINX_SOURCE
    assert external.decode().replace(template.HOST, "", 1) == EXTERNAL_SOURCE
    rendered_nginx, rendered_external = render("nginx", True, proxied), render("external", True, proxied)
    assert observability.routes_grafana(rendered_nginx, rendered_external) == {"nginx": True, "external": True}
    location = observability.config_block(rendered_external, "location /grafana {")
    assert location.count("proxy_set_header Host $host;") == 1 and ("auth_request /oauth2/auth;" in location) is proxied
    assert observability.routes_grafana(render("nginx", False, proxied), render("external", False, proxied)) == {"nginx": False, "external": proxied}
    assert observability.routes_grafana(rendered_nginx, render("external", False, proxied)) == {"nginx": True, "external": proxied}


def test_an_ambiguous_or_oversized_template_refuses_before_any_write():
    for source in (NGINX_SOURCE + template.UPSTREAM + "    }\n", NGINX_SOURCE.replace(template.UPSTREAM, template.UPSTREAM + template.AFFINITY)):
        with pytest.raises(ValueError):
            template.routed_upstream(source)
    for source in (EXTERNAL_SOURCE + template.LOCATION + template.PASS, template.routed_location(EXTERNAL_SOURCE).decode()):
        with pytest.raises(ValueError, match="ambiguous"):
            template.routed_location(source)
    with pytest.raises(ValueError, match="bound"):
        template.routed_location(EXTERNAL_SOURCE + "#" * template.MAX_TEMPLATE)


class Records:
    def __init__(self):
        self.values = {}

    def read(self, name):
        return copy.deepcopy(self.values.get(name))

    def write(self, name, value):
        self.values[name] = copy.deepcopy(value)


class Worker:
    """The bootstrap worker's template view: its selection, records and the native config-key store."""

    def __init__(self, source=None):
        self.selection = selection(SOURCES if source is None else source)
        self.markers = Records()
        self.stored = {}
        self.writes = []

    def recovery_scope(self):
        return {"owner": "synthetic-operation", "fsid": FSID}

    def runtime_profile(self):
        return {"imageID": "a" * 64}

    def ceph(self, *args):
        assert args == ("config-key", "ls")
        return sorted(self.stored)

    def ceph_text(self, *args):
        assert args[:2] == ("config-key", "get")
        return self.stored[args[2]] + b"\n"

    def tunnel_effect(self, *args, data, json_result):
        assert json_result is False and args[:2] == ("config-key", "set") and args[3:] == ("-i", "-")
        descriptor = template.NGINX if args[2] == template.NGINX.key else template.EXTERNAL
        assert self.markers.read(descriptor.attempt) is not None
        self.writes.append(args[2])
        self.stored[args[2]] = data


FRONTED = {"monitoring": ["grafana", "prometheus"], "type": "mgmt-gateway"}


def test_the_overrides_exist_only_for_a_fronted_grafana_on_a_witnessed_build():
    worker = Worker()
    plans = dict(template.gateway_plans(worker, FRONTED))
    assert plans[template.NGINX][0] == template.routed_upstream(NGINX_SOURCE)
    assert plans[template.EXTERNAL][0] == template.routed_location(EXTERNAL_SOURCE)
    assert plans[template.NGINX][1] == plans[template.EXTERNAL][1] == template.source_digests(SOURCES)
    for unit, source in ((dict(FRONTED, monitoring=["prometheus"]), None), ({"type": "mgmt-gateway"}, None),
                         (FRONTED, dict(nginx=PINNED, external=FIXED_HOST))):
        assert all(plan is None for unused, plan in template.gateway_plans(Worker(source), unit))
    assert template.NGINX.attempt.endswith("-attempt.json") and template.EXTERNAL.complete.endswith("-complete.json")
    assert len({template.NGINX.attempt, template.NGINX.complete, template.EXTERNAL.attempt, template.EXTERNAL.complete}) == 4


@pytest.mark.parametrize("fixed, corrected", [("external", "nginx"), ("nginx", "external")])
def test_an_image_that_fixes_one_template_keeps_the_other_correction(fixed, corrected):
    source = dict(SOURCES, **{fixed: FIXED_HOST if fixed == "external" else PINNED})
    plans = {template.ROUTES[descriptor]: plan for descriptor, plan in template.gateway_plans(Worker(source), FRONTED)}
    assert plans[fixed] is None and plans[corrected] is not None
    worker = Worker(source)
    for descriptor, plan in template.gateway_plans(worker, FRONTED):
        template.prepare(worker, plan, descriptor)
    assert worker.writes == [{"nginx": template.NGINX, "external": template.EXTERNAL}[corrected].key]


def test_both_overrides_follow_the_owned_template_protocol():
    worker = Worker()
    plans = template.gateway_plans(worker, FRONTED)
    assert not any(template.ready(worker, plan, descriptor) for descriptor, plan in plans)
    assert all(template.prepare(worker, plan, descriptor) for descriptor, plan in plans)
    assert sorted(worker.writes) == sorted((template.NGINX.key, template.EXTERNAL.key))
    assert all(template.ready(worker, plan, descriptor) for descriptor, plan in plans)
    assert not any(template.prepare(worker, plan, descriptor) for descriptor, plan in plans) and len(worker.writes) == 2
    intent = worker.markers.read(template.NGINX.complete)
    assert intent["version"] == "ceph-monitoring-template-v1" and intent["sources"] == template.source_digests(SOURCES)
    native = template.gateway_plans(Worker(dict(SOURCES, external=FIXED_HOST)), FRONTED)
    stale = Worker(dict(SOURCES, external=FIXED_HOST))
    stale.markers, stale.stored = worker.markers, worker.stored
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.state(stale, native[0][1], native[0][0])


@pytest.mark.parametrize("key", [template.NGINX.key, "mgr/cephadm/node-1/services/mgmt-gateway/external_server.conf"])
def test_a_foreign_or_host_specific_gateway_override_refuses_without_writes(key):
    worker = Worker()
    worker.stored[key] = b"foreign"
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        for descriptor, plan in template.gateway_plans(worker, FRONTED):
            template.state(worker, plan, descriptor)
    assert not worker.writes and not worker.markers.values


@pytest.fixture
def sources(monkeypatch):
    """The held image's two gateway templates in place of its confined read, with the identity normalization the gateway tests use."""
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **unused: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())
    real = template.image_sources
    monkeypatch.setattr(template, "image_sources", lambda worker, paths=None: dict(SOURCES) if paths is template.GATEWAY_SOURCES else real(worker, paths))


class GatewayMonitor(monitoring.Monitor):
    """The scripted monitor with cephadm's config-key store, its gateway reconfigure, and this host's podman."""

    def __init__(self, request, containers=None):
        super().__init__(request)
        self.stored, self.containers = {}, list(containers or [])

    def run(self, argv, data=b"", timeout=60):
        if argv[0] == ceph_native_units.PODMAN:
            self.log.append(list(argv))
            if argv[1] == "ps":
                assert argv[2:] == ["--all", "--format", "json", "--filter", "name=^ceph-" + FSID + "-mgmt-gateway[.-]"]
                return 0, json.dumps([{"Id": "%064x" % index, "Names": [name], "State": state}
                                      for index, (name, state, unused) in enumerate(self.containers)]).encode()
            assert argv[1:4] == ["inspect", "--type", "container"]
            return 0, json.dumps([self.containers[int(argv[4], 16)][2]]).encode()
        if "config-key" in argv:
            command = tuple(argv[argv.index("config-key"):])
            self.log.append(list(command))
            if command[1] == "ls":
                return 0, json.dumps(sorted(self.stored)).encode()
            if command[1] == "get":
                return 0, self.stored[command[2]] + b"\n"
            assert command[1] == "set" and command[3:] == ("-i", "-")
            self.mutations.append(command)
            self.stored[command[2]] = data
            return 0, b""
        command = tuple(argv[argv.index("--format") + 2:])
        if command == ("orch", "ps", "--daemon_type", "mgmt-gateway"):
            self.log.append(list(command))
            return 0, json.dumps([gateway.row("mgmt-gateway", "mgmt-gateway", host, gateway.NGINX, daemon_name="mgmt-gateway." + host)
                                  for host in gateway.TWO]).encode()
        if command == ("orch", "reconfig", "mgmt-gateway"):
            self.log.append(list(command))
            if self.failures.get(command):
                self.failures[command] -= 1
                return 1, b"Error ETIMEDOUT: the manager restarted"
            self.mutations.append(command)
            return 0, b"Scheduled to reconfig mgmt-gateway.node-1 on host 'node-1'"
        return super().run(argv, data, timeout)


def fronting():
    request = gateway.gateway_request()
    request["service"]["monitoring"] = ["grafana"]
    return request


def test_a_fresh_gateway_completes_both_overrides_before_its_stream(tmp_path, sources):
    request = fronting()
    markers = units.markers_for(tmp_path, request)
    monitor = GatewayMonitor(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    sets = [index for index, command in enumerate(monitor.mutations) if command[:2] == ("config-key", "set")]
    applied = monitor.mutations.index(("orch", "apply", "-i", "-"))
    assert len(sets) == 2 and max(sets) < applied
    assert monitor.stored == {template.NGINX.key: template.routed_upstream(NGINX_SOURCE), template.EXTERNAL.key: template.routed_location(EXTERNAL_SOURCE)}
    assert all(markers.read(name) is not None for descriptor in (template.NGINX, template.EXTERNAL) for name in descriptor[1:])
    monitor.mutations.clear()
    assert gateway.applying(monitor, markers).unit() == {"changed": False} and not monitor.mutations
    native = units.worker(monitor, markers)
    assert native.grafana_route() == {"changed": False, "routes": ["external", "nginx"]}
    before = units.records(markers)
    assert native.grafana_reconfig() == {"changed": True} and monitor.mutations == [("orch", "reconfig", "mgmt-gateway")]
    assert units.records(markers) == before


def test_gateway_readiness_waits_for_both_overrides(tmp_path, sources):
    request = fronting()
    markers = units.markers_for(tmp_path, request)
    monitor = GatewayMonitor(request)
    native = units.worker(monitor, markers)
    assert observability.gateway_readiness(native, request["service"], []) is False
    plans = template.gateway_plans(native, request["service"])
    template.prepare(native, plans[0][1], plans[0][0])
    assert observability.gateway_readiness(units.worker(monitor, markers), request["service"], []) is False
    template.prepare(native, plans[1][1], plans[1][0])
    assert observability.gateway_readiness(units.worker(monitor, markers), request["service"], []) is True


def test_the_reconfigure_refuses_before_the_unit_and_its_overrides_complete_and_repeats_after_a_lost_reply(tmp_path, sources):
    request = fronting()
    markers = units.markers_for(tmp_path, request)
    monitor = GatewayMonitor(request)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        units.worker(monitor, markers).grafana_reconfig()
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    stored = dict(monitor.stored)
    markers_path = markers.path
    os.remove(os.path.join(markers_path, template.EXTERNAL.complete))
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        units.worker(monitor, markers).grafana_reconfig()
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        units.worker(monitor, markers).grafana_route()
    template.prepare(units.worker(monitor, markers), *reversed(template.gateway_plans(units.worker(monitor, markers), request["service"])[1]))
    assert monitor.stored == stored and not any(command[:2] == ("orch", "reconfig") for command in monitor.mutations)
    monitor.failures[("orch", "reconfig", "mgmt-gateway")] = 1
    with pytest.raises(ceph_native.Refusal):
        units.worker(monitor, markers).grafana_reconfig()
    assert units.worker(monitor, markers).grafana_reconfig() == {"changed": True}
    assert [command for command in monitor.mutations if command[:2] == ("orch", "reconfig")] == [("orch", "reconfig", "mgmt-gateway")]


def test_a_gateway_whose_image_fixed_its_location_overrides_only_its_upstream(tmp_path, sources, monkeypatch):
    monkeypatch.setattr(template, "image_sources", lambda worker, paths=None: dict(SOURCES, external=FIXED_HOST))
    request = fronting()
    markers = units.markers_for(tmp_path, request)
    monitor = GatewayMonitor(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    assert monitor.stored == {template.NGINX.key: template.routed_upstream(NGINX_SOURCE)}
    assert units.worker(monitor, markers).grafana_route() == {"changed": False, "routes": ["nginx"]}


def files(root, host, nginx, external):
    """A gateway daemon's two rendered files beneath the cluster's data directory, and its container as podman inspects it."""
    paths = []
    for name, content in (("nginx.conf", nginx), ("nginx_external_server.conf", external)):
        path = os.path.join(root, FSID, "mgmt-gateway." + host, "etc", name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(content)
        os.chmod(path, 0o600)
        os.utime(path, ns=(1764000000 * 10 ** 9, 1764000000 * 10 ** 9))
        paths.append(path)
    return {"Args": ["nginx", "-g", "daemon off;"], "Config": {"Env": [], "User": str(os.getuid())}, "State": {"StartedAt": "2025-11-24T16:00:00.5Z"},
            "Mounts": [{"Source": paths[0], "Destination": observability.GATEWAY_NGINX},
                       {"Source": paths[1], "Destination": observability.GATEWAY_EXTERNAL}]}


@pytest.fixture
def root(tmp_path, monkeypatch):
    value = str(tmp_path / "ceph")
    monkeypatch.setattr(observability, "DATA_ROOT", value)
    return value


def corrected_files(root, host="node-1", proxied=False):
    return files(root, host, render("nginx", True, proxied), render("external", True, proxied))


BOTH = {"nginx": True, "external": True}
NEITHER = {"nginx": False, "external": False}


def test_a_deployed_gateway_proves_each_line_in_the_files_it_started_with(root):
    assert observability.gateway_routed(FSID, corrected_files(root)) == BOTH
    assert observability.gateway_routed(FSID, corrected_files(root, proxied=True)) == BOTH
    assert observability.gateway_routed(FSID, files(root, "node-1", render("nginx", False, False), render("external", False, False))) == NEITHER
    assert observability.gateway_routed(FSID, files(root, "node-1", render("nginx", True, False), render("external", False, False))) == {
        "nginx": True, "external": False}
    inspected = corrected_files(root)
    for started in ("2025-11-24T15:59:59.999999999Z", "2025-11-24T16:59:59+01:00", "yesterday", None):
        assert observability.gateway_routed(FSID, dict(inspected, State={"StartedAt": started})) == NEITHER, started
    assert observability.gateway_routed(FSID, dict(inspected, State={"StartedAt": "2025-11-24T15:00:01-01:00"})) == BOTH
    for broken in ([], inspected["Mounts"][:1], inspected["Mounts"] + inspected["Mounts"][1:]):
        assert observability.gateway_routed(FSID, dict(inspected, Mounts=broken)) == NEITHER
    assert observability.gateway_routed(FSID, dict(inspected, Config={"Env": [], "User": str(os.getuid() + 1)})) == NEITHER
    os.chmod(inspected["Mounts"][1]["Source"], 0o666)
    assert observability.gateway_routed(FSID, inspected) == NEITHER


def test_each_gateway_host_counts_its_running_daemons_and_each_route_they_serve(tmp_path, root, sources):
    request = fronting()
    containers = [("ceph-" + FSID + "-mgmt-gateway-node-1", "running", corrected_files(root)),
                  ("ceph-" + FSID + "-mgmt-gateway-node-1-old", "exited", {}),
                  ("ceph-" + FSID + "-mgmt-gateway.node-1", "running", files(root, "node-1-stale", render("nginx", True, False),
                                                                             render("external", False, False)))]
    monitor = GatewayMonitor(request, containers)
    native = units.worker(monitor, units.markers_for(tmp_path, request))
    assert native.grafana_route_proof() == {"changed": False, "daemons": 2, "served": {"external": 1, "nginx": 2}}
    assert not monitor.mutations
    monitor.containers = [("ceph-other-mgmt-gateway-node-1", "running", {})]
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.grafana_route_proof()


class RouteTransport(gateway.MaterialTransport):
    """The coordinator's view of a gateway fronting Grafana: the scripted observations, the bootstrap host's selected routes and
    reconfigure, and each gateway host's proof, whose daemons serve the routes once reconfigured, at once, or never."""

    def __init__(self, request, mode, routes=("external", "nginx"), **options):
        super().__init__(request, {"certificateSHA256": gateway.LEAF_SHA256, "reachable": True}, **options)
        self.mode, self.routes, self.reconfigured, self.lock = mode, list(routes), 0, threading.Lock()

    def call(self, host, phase, private=None, *, timeout=None):
        if phase not in ("grafana_route", "grafana_route_proof", "grafana_reconfig"):
            return super().call(host, phase, private, timeout=timeout)
        assert private is None and 0 < timeout <= (ceph_core.ROUTE_CALL if phase == "grafana_route_proof" else ceph_core.READ_BUDGET)
        with self.lock:
            self.private.append((phase, []))
            if phase == "grafana_route":
                return {"changed": False, "routes": self.routes}
            if phase == "grafana_reconfig":
                self.reconfigured += 1
                return {"changed": True}
            daemons = 0 if self.mode == "short" and host["name"] == "node-2" else 1
            routed = self.mode in ("routed", "short") or self.mode == "stale" and self.reconfigured
            return {"changed": False, "daemons": daemons, "served": dict.fromkeys(("external", "nginx"), daemons if routed else 0)}


def applying(transport):
    clock = [0.0]
    return ceph_core.execute(transport.request, "apply", "d" * 64, transport, now=lambda: clock[0],
                             pause=lambda seconds: clock.__setitem__(0, clock[0] + seconds))


def phases(transport):
    return [phase for phase, unused in transport.private]


def test_an_apply_proves_the_routes_only_once_the_unit_reads_ready():
    transport = RouteTransport(fronting(), "routed", ready_after=3)
    result = applying(transport)
    assert result["evidence"]["ready"] is True and transport.reconfigured == 0
    called = phases(transport)
    assert called.count("observe") == 3 and called.index("grafana_route") > called.index("vip_holder") > max(
        index for index, phase in enumerate(called) if phase == "observe")
    assert called.count("grafana_route") == 1 and called.count("grafana_route_proof") == 2 and "grafana_reconfig" not in called


def test_a_stale_gateway_gets_one_reconfigure_and_the_apply_waits_until_it_serves_the_routes():
    transport = RouteTransport(fronting(), "stale")
    result = applying(transport)
    assert result["evidence"]["ready"] is True and transport.reconfigured == 1
    called = phases(transport)
    assert called.index("grafana_reconfig") > called.index("grafana_route_proof") and called.count("grafana_route_proof") == 4


def test_a_native_gateway_proves_nothing_and_an_unserved_route_times_out_naming_it():
    native = RouteTransport(fronting(), "never", routes=())
    assert applying(native)["evidence"]["ready"] is True and "grafana_route_proof" not in phases(native)
    for mode, asked in (("never", 1), ("short", 0)):
        transport = RouteTransport(fronting(), mode)
        with pytest.raises(ceph_core.ReadinessRefusal) as refused:
            applying(transport)
        assert transport.reconfigured == asked and refused.value.checks == [ceph_core.ROUTE_ENTRY], mode
        assert ceph_core.refusal(refused.value, ("not-ready",)) == ("not-ready", "not-ready:route@mgmt-gateway")
    assert ceph_core.blocking_entry(ceph_core.ROUTE_ENTRY)
