"""The management gateway's sign-on corrections: exact-anchor witnesses select them, they render no client cookie to the dashboard and the
fixed Grafana role Viewer under cephadm's Jinja settings, they compose with the Grafana routes in the one external_server.conf override, and
the route proof counts them. They need no oauth2-proxy: they sit inside the template's oauth2-proxy branches, inert until a CephSignOn's proxy
runs, so the gateway selects and completes them from its first apply, and the CephSignOn's block proves them once its proxy runs.

The template below is synthetic text shaped at the exact anchors of the IBM image's mgmt-gateway template
(/usr/share/ceph/mgr/cephadm/templates/services/mgmt-gateway/external_server.conf.j2:78-113 and :117-147 in
cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074), never provider bytes. In OAuth2 mode the dashboard reads a `token` cookie before the
X-Access-Token header (src/pybind/mgr/dashboard/services/auth/oauth2.py, get_token) and decodes it without verifying it, and Grafana's
auth proxy maps `Role:X-WEBAUTH-ROLE` (cephadm's grafana.ini.j2).
"""

from __future__ import annotations

import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_monitoring_template as template, ceph_native, ceph_native_observability as observability,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_gateway_grafana_routes as routes
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_signon as signon

SIGNED = """server {
{% if dashboard_endpoints %}
    location / {
        proxy_pass {{ dashboard_scheme }}://dashboard_servers;
        proxy_next_upstream error timeout;
        {% if enable_oauth2_proxy %}
        auth_request /oauth2/auth;
        auth_request_set $token $upstream_http_x_auth_request_access_token;
        proxy_set_header X-Access-Token $token;
        proxy_set_header Host $host;
        {% endif %}
    }
{% endif %}

{% if grafana_endpoints %}
    location /grafana {
        proxy_pass {{ grafana_scheme }}://grafana_servers;
        proxy_set_header Authorization "";
        {% if enable_oauth2_proxy %}
        auth_request /oauth2/auth;
        proxy_set_header X-WEBAUTH-USER $user;
        # Pass role header to Grafana
        proxy_set_header X-WEBAUTH-ROLE $http_x_auth_request_role;
        proxy_set_header Host $host;
        {% endif %}
    }
{% endif %}
}
"""
SOURCES = {"nginx": routes.NGINX_SOURCE, "external": SIGNED}
# The gateway tests' identity normalization, autoused here too, and the routes tests' held-image fixture.
normalization = gateway.normalization

sources = routes.sources
root = routes.root
CONTEXT = dict(routes.CONTEXT, dashboard_endpoints=["192.0.2.11:8443"], dashboard_scheme="https")
FIXED_COOKIE = SIGNED.replace(template.TOKEN, template.TOKEN + template.COOKIE)
FORWARDED_COOKIE = SIGNED.replace(template.TOKEN, template.TOKEN + "        proxy_set_header Cookie $http_cookie;\n")
FIXED_ROLE = SIGNED.replace(template.ROLE, "        proxy_set_header X-WEBAUTH-ROLE $role;\n")
DOCUMENTS = [{"service_type": "mgmt-gateway"}]
FRONTED = {"documents": DOCUMENTS, "monitoring": ["grafana", "prometheus"], "type": "mgmt-gateway"}
BARE = {"documents": DOCUMENTS, "type": "mgmt-gateway"}
# What cephadm's Jinja environment renders under CONTEXT with an oauth2-proxy, natively and with every external correction.
PROXIED_NATIVE = """server {
    location / {
        proxy_pass https://dashboard_servers;
        proxy_next_upstream error timeout;
        auth_request /oauth2/auth;
        auth_request_set $token $upstream_http_x_auth_request_access_token;
        proxy_set_header X-Access-Token $token;
        proxy_set_header Host $host;
    }

    location /grafana {
        proxy_pass https://grafana_servers;
        proxy_set_header Authorization "";
        auth_request /oauth2/auth;
        proxy_set_header X-WEBAUTH-USER $user;
        # Pass role header to Grafana
        proxy_set_header X-WEBAUTH-ROLE $http_x_auth_request_role;
        proxy_set_header Host $host;
    }
}"""
PROXIED_CORRECTED = PROXIED_NATIVE.replace("X-Access-Token $token;\n", "X-Access-Token $token;\n        proxy_set_header Cookie \"\";\n").replace(
    "X-WEBAUTH-ROLE $http_x_auth_request_role;", "X-WEBAUTH-ROLE \"Viewer\";")
UNPROXIED_NATIVE = """server {
    location / {
        proxy_pass https://dashboard_servers;
        proxy_next_upstream error timeout;
    }

    location /grafana {
        proxy_pass https://grafana_servers;
        proxy_set_header Authorization "";
    }
}"""
UNPROXIED_CORRECTED = UNPROXIED_NATIVE.replace("grafana_servers;\n", "grafana_servers;\n        proxy_set_header Host $host;\n")


def composed(text, *corrections):
    for correct in corrections:
        text = correct(text).decode()
    return text.encode()


def test_each_sign_on_defect_selects_its_own_correction():
    chosen = routes.selection(SOURCES)
    assert chosen.variant("ceph.gateway.sign-on-headers") == "gateway-identity" and chosen.applied == {"ceph-gateway-sign-on-headers"}
    assert template.sign_on_witness(SOURCES) == {"cookie": True, "role": True}
    assert template.sign_on_witness(dict(SOURCES, external=FIXED_COOKIE)) == {"cookie": False, "role": True}
    assert template.sign_on_witness(dict(SOURCES, external=FIXED_ROLE)) == {"cookie": True, "role": False}
    for text in (FORWARDED_COOKIE, SIGNED.replace(template.DASHBOARD_PASS, template.DASHBOARD_PASS + template.COOKIE),
                 FIXED_COOKIE.replace(template.COOKIE, template.COOKIE + template.COOKIE)):
        assert template.sign_on_witness(dict(SOURCES, external=text))["cookie"] is None
    both = FIXED_COOKIE.replace(template.ROLE, "        proxy_set_header X-WEBAUTH-ROLE $role;\n")
    assert template.gateway_facts(dict(SOURCES, external=both))["sign-on-forwarded"] is False
    unknown = {
        "no dashboard location": SIGNED.replace("    location / {\n", "    location /dashboard {\n"),
        "repeated token line": SIGNED.replace("        proxy_set_header Host $host;\n        {% endif %}\n    }\n{% endif %}\n\n{% if grafana",
                                              template.TOKEN + "        {% endif %}\n    }\n{% endif %}\n\n{% if grafana", 1),
        "token outside the branch": SIGNED.replace("        proxy_next_upstream error timeout;\n", template.TOKEN, 1).replace(
            "        auth_request_set $token $upstream_http_x_auth_request_access_token;\n" + template.TOKEN,
            "        auth_request_set $token $upstream_http_x_auth_request_access_token;\n"),
        "nested dashboard branch": SIGNED.replace("        auth_request /oauth2/auth;\n        auth_request_set $token",
                                                  "        {% if x %}{% endif %}\n        auth_request_set $token"),
    }
    for name, text in unknown.items():
        assert template.sign_on_witness(dict(SOURCES, external=text))["cookie"] is None, name
    no_role = SIGNED.replace(template.ROLE, "")
    moved_role = no_role.replace("        proxy_set_header Authorization \"\";\n", "        proxy_set_header Authorization \"\";\n" + template.ROLE)
    for name, text in {"no role line": no_role, "role outside the branch": moved_role,
                       "a second role line": SIGNED.replace(template.ROLE, template.ROLE + template.ROLE)}.items():
        assert template.sign_on_witness(dict(SOURCES, external=text))["role"] is None, name
    assert template.gateway_facts(dict(SOURCES, external=FIXED_COOKIE.replace(template.ROLE, "")))["sign-on-forwarded"] is None
    assert template.gateway_facts(routes.SOURCES)["sign-on-forwarded"] is None
    for value in (dict(SOURCES, external=both), routes.SOURCES, None, {}, {"external": SIGNED}):
        chosen = routes.selection(value)
        assert chosen.variant("ceph.gateway.sign-on-headers") == "native" and "ceph-gateway-sign-on-headers" not in chosen.applied


@pytest.mark.parametrize("proxied", [False, True])
def test_cephadms_jinja_settings_render_the_sign_on_corrections_only_with_an_oauth2_proxy(proxied):
    jinja2 = pytest.importorskip("jinja2")
    environment = jinja2.Environment(autoescape=False, trim_blocks=True, lstrip_blocks=True, undefined=jinja2.StrictUndefined)
    corrected = composed(SIGNED, template.routed_location, template.cleared_cookie, template.viewer_role).decode()
    for text, expected in ((SIGNED, (UNPROXIED_NATIVE, PROXIED_NATIVE)), (corrected, (UNPROXIED_CORRECTED, PROXIED_CORRECTED))):
        assert environment.from_string(text).render(dict(CONTEXT, enable_oauth2_proxy=proxied)) == expected[proxied]


def test_the_rendered_corrections_are_what_the_route_proof_counts():
    pinned = routes.render("nginx", True, True)
    assert observability.routes_grafana(pinned, PROXIED_CORRECTED) == dict.fromkeys(observability.GATEWAY_ROUTES, True)
    assert observability.routes_grafana(pinned, PROXIED_NATIVE) == {"cookie": False, "external": True, "nginx": True, "role": False}
    location = observability.config_block(PROXIED_CORRECTED, "location / {")
    assert location.count('proxy_set_header Cookie "";') == 1 and location.index('proxy_set_header Cookie "";') > location.index(
        "proxy_set_header X-Access-Token $token;")
    both = PROXIED_CORRECTED.replace('X-WEBAUTH-ROLE "Viewer";', 'X-WEBAUTH-ROLE "Viewer";\n        proxy_set_header X-Role $http_x_auth_request_role;')
    assert observability.routes_grafana(pinned, both)["role"] is False
    assert observability.routes_grafana(pinned, UNPROXIED_CORRECTED) == {"cookie": False, "external": True, "nginx": True, "role": False}
    for name, text in UNPROTECTED.items():
        assert observability.routes_grafana(pinned, text)["cookie"] is False, name
    for name, text in UNPINNED.items():
        assert observability.routes_grafana(pinned, text)["role"] is False, name
    spaced = PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header   Cookie   ""  ;  # cleared')
    assert observability.routes_grafana(pinned, spaced)["cookie"] is True


# Rendered gateway files a sign-on must never enable single sign-on over: a dashboard location that signs no request in, or forwards any
# client cookie, and a Grafana location that sends a client role.
UNPROTECTED = {
    "native template": PROXIED_NATIVE,
    "unknown shape": PROXIED_CORRECTED.replace("    location / {", "    location /dashboard {"),
    "no auth_request": PROXIED_CORRECTED.replace("auth_request /oauth2/auth;\n        auth_request_set", "auth_request_set"),
    "auth_request off": PROXIED_CORRECTED.replace("auth_request /oauth2/auth;\n        auth_request_set", "auth_request off;\n        auth_request_set"),
    "client cookie": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', "proxy_set_header Cookie $http_cookie;"),
    "a second cookie line": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";',
                                                      'proxy_set_header Cookie "";\n        proxy_set_header cookie $http_cookie;'),
    "two statements on a line": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header Cookie ""; proxy_set_header Cookie $http_cookie;'),
    "plain location": UNPROXIED_NATIVE,
    "a nested location": (
        PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header Cookie "";\n        location /api { proxy_pass https://x; }')),
    "an include": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header Cookie "";\n        include /etc/nginx/extra.conf;'),
    "satisfy any": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header Cookie "";\n        satisfy any;'),
    "a hash inside quotes": PROXIED_CORRECTED.replace('proxy_set_header Cookie "";', 'proxy_set_header Cookie ""; proxy_set_header A "x#"; auth_request off;'),
    "a client access token": PROXIED_CORRECTED.replace("proxy_set_header X-Access-Token $token;", "proxy_set_header X-Access-Token $http_x_access_token;"),
    "no access token": PROXIED_CORRECTED.replace("proxy_set_header X-Access-Token $token;", ""),
    "a second dashboard location": PROXIED_CORRECTED.replace(
        "    location /grafana {", "    location /api {\n        proxy_pass https://dashboard_servers;\n    }\n\n    location /grafana {"),
}
UNPINNED = {
    "client role": PROXIED_NATIVE,
    "a client role beside Viewer": PROXIED_CORRECTED.replace(
        'X-WEBAUTH-ROLE "Viewer";', 'X-WEBAUTH-ROLE "Viewer";\n        proxy_set_header X-WEBAUTH-ROLE $http_x_auth_request_role;'),
    "no auth_request": PROXIED_CORRECTED.replace("auth_request /oauth2/auth;\n        proxy_set_header X-WEBAUTH-USER", "proxy_set_header X-WEBAUTH-USER"),
}


@pytest.mark.parametrize("name", sorted(UNPROTECTED))
def test_a_sign_on_never_enables_single_sign_on_over_a_gateway_that_does_not_protect_the_dashboard(tmp_path, root, name):
    request = signon.sso_request()
    pinned = routes.render("nginx", True, True)
    containers = [("ceph-" + routes.FSID + "-mgmt-gateway-node-1", "running", routes.files(root, "node-1", pinned, UNPROTECTED[name]))]
    proof = units.worker(routes.GatewayMonitor(request, containers), units.markers_for(tmp_path, request)).signon_route_proof()
    assert proof["daemons"] == 1 and proof["served"]["cookie"] == 0
    one = {"changed": False, "daemons": 1, "proved": True}
    for corrected in ((), ("cookie",)):
        now, pause, clock = signon.ticking()
        transport = SignedGateway(request, [one, one], proof, routes=corrected, advance=lambda: clock.__setitem__(0, clock[0] + 100))
        with pytest.raises(ceph_native.NativeFailure, match="^sign-on-unprotected$") as caught:
            ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
        assert caught.value.native_error["cause"].startswith("host node-1: route cookie")
        called = [phase for unused, phase, unused_private in transport.private]
        assert "signon_route_proof" in called and "sso" not in called and "sso_withdraw" not in called and "observe" not in called


class SignedGateway(signon.SSOTransport):
    """A sign-on transport whose gateway hosts answer the route proof a real worker read from rendered files."""

    def __init__(self, request, proofs, proof, **options):
        super().__init__(request, proofs, **options)
        self.proof = proof

    def call(self, host, phase, private=None, *, timeout=None, authorization=None):
        if phase == "signon_route_proof":
            with self.lock:
                self.private.append((host["name"], phase, []))
            if self.advance is not None:
                self.advance()
            return dict(self.proof)
        return super().call(host, phase, private, timeout=timeout, authorization=authorization)


def test_an_ambiguous_or_oversized_template_refuses_each_sign_on_correction():
    for source in (SIGNED + template.TOKEN, FIXED_COOKIE, template.cleared_cookie(SIGNED).decode(), SIGNED.replace(template.DASHBOARD_PASS, "")):
        with pytest.raises(ValueError, match="ambiguous"):
            template.cleared_cookie(source)
    for source in (SIGNED + template.ROLE, template.viewer_role(SIGNED).decode(), SIGNED.replace(template.ROLE, "")):
        with pytest.raises(ValueError, match="ambiguous"):
            template.viewer_role(source)
    with pytest.raises(ValueError, match="bound"):
        template.viewer_role(SIGNED + "#" * template.MAX_TEMPLATE)


def test_the_sign_on_corrections_join_the_grafana_routes_in_one_override_without_any_oauth2_proxy():
    plans = dict(template.gateway_plans(routes.Worker(SOURCES), FRONTED))
    assert template.gateway_corrections(routes.Worker(SOURCES), FRONTED) == ["nginx", "external", "cookie", "role"]
    assert plans[template.NGINX][0] == template.routed_upstream(routes.NGINX_SOURCE)
    assert plans[template.EXTERNAL][0] == composed(SIGNED, template.routed_location, template.cleared_cookie, template.viewer_role)
    assert plans[template.EXTERNAL][1] == template.source_digests(SOURCES)
    bare = dict(template.gateway_plans(routes.Worker(SOURCES), BARE))
    assert template.gateway_corrections(routes.Worker(SOURCES), BARE) == ["cookie"]
    assert bare[template.NGINX] is None and bare[template.EXTERNAL][0] == template.cleared_cookie(SIGNED)
    fixed = routes.Worker(dict(SOURCES, external=FIXED_COOKIE))
    assert template.gateway_corrections(fixed, FRONTED) == ["nginx", "external", "role"]
    assert dict(template.gateway_plans(fixed, FRONTED))[template.EXTERNAL][0] == composed(FIXED_COOKIE, template.routed_location, template.viewer_role)
    assert template.gateway_corrections(routes.Worker(dict(SOURCES, external=FIXED_COOKIE)), BARE) == []
    assert all(plan is None for unused, plan in template.gateway_plans(routes.Worker(dict(SOURCES, external=FIXED_COOKIE)), BARE))


def test_a_native_gateway_template_selects_no_sign_on_correction_and_the_grafana_routes_follow_their_own_seam():
    worker = routes.Worker(routes.SOURCES)
    assert template.gateway_corrections(worker, BARE) == [] and all(plan is None for unused, plan in template.gateway_plans(worker, BARE))
    unsigned = routes.Worker(routes.SOURCES)
    assert template.gateway_corrections(unsigned, routes.FRONTED) == ["nginx", "external"]
    assert unsigned.selection.applied == {"ceph-gateway-grafana-sessions"}
    signed = routes.Worker(SOURCES)
    assert template.gateway_corrections(signed, routes.FRONTED) == ["nginx", "external", "cookie", "role"]
    assert signed.selection.applied == {"ceph-gateway-grafana-sessions", "ceph-gateway-sign-on-headers"}
    assert template.gateway_corrections(routes.Worker(SOURCES), {"documents": DOCUMENTS, "monitoring": ["prometheus"], "type": "mgmt-gateway"}) == [
        "cookie"]


def test_every_gateway_unit_owes_its_route_proof_and_a_non_gateway_unit_never_does():
    assert observability.routes_owed(BARE) is observability.routes_owed(FRONTED) is True
    assert observability.routes_owed(gateway.gateway_request()["service"]) is True
    assert observability.routes_owed(signon.sso_request()["service"]) is False
    assert observability.routes_owed(units.unit_request("direct")["service"]) is False


class SignOnMonitor(signon.SSOMonitor):
    """The sign-on's scripted monitor with cephadm's config-key store."""

    def __init__(self, request):
        super().__init__(request)
        self.stored = {}

    def run(self, argv, data=b"", timeout=60):
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
        return super().run(argv, data, timeout)


def test_a_gateway_completes_its_cookie_override_before_its_stream_without_any_oauth2_proxy(tmp_path, sources, monkeypatch):
    del sources
    monkeypatch.setattr(template, "image_sources", lambda worker, paths=None: dict(SOURCES))
    request = gateway.gateway_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SignOnMonitor(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    sets = [index for index, command in enumerate(monitor.mutations) if command[:2] == ("config-key", "set")]
    assert len(sets) == 1 and sets[0] < monitor.mutations.index(("orch", "apply", "-i", "-"))
    assert monitor.stored == {template.EXTERNAL.key: template.cleared_cookie(SIGNED)}
    assert units.worker(monitor, markers).grafana_route() == {"changed": False, "routes": []}


def test_the_gateways_grafana_route_phase_names_only_the_routes_it_renders_without_a_proxy(tmp_path, sources, monkeypatch):
    del sources
    monkeypatch.setattr(template, "image_sources", lambda worker, paths=None: dict(SOURCES))
    request = routes.fronting()
    markers = units.markers_for(tmp_path, request)
    monitor = SignOnMonitor(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    assert template.gateway_corrections(units.worker(monitor, markers), request["service"]) == ["nginx", "external", "cookie", "role"]
    assert units.worker(monitor, markers).grafana_route() == {"changed": False, "routes": ["external", "nginx"]}
    assert set(observability.RENDERED_ROUTES) | set(observability.SIGN_ON_ROUTES) == set(observability.GATEWAY_ROUTES)


def test_a_sign_ons_bootstrap_host_names_the_sign_on_corrections_this_build_selects(tmp_path, sources, monkeypatch):
    del sources
    request = signon.sso_request()
    for image, grafana, expected in ((SOURCES, False, ["cookie"]), (SOURCES, True, ["cookie", "role"]), (dict(SOURCES, external=FIXED_COOKIE), True, ["role"]),
                                     (dict(SOURCES, external=FIXED_COOKIE), False, []), (routes.SOURCES, True, []),
                                     (dict(SOURCES, external=FIXED_COOKIE.replace(template.ROLE, "")), True, [])):
        monkeypatch.setattr(template, "image_sources", lambda worker, paths=None, value=image: dict(value))
        request["service"]["signOn"]["grafana"] = grafana
        markers = units.markers_for(tmp_path / ("m" + str(len(expected)) + str(grafana) + str(len(image["external"]))), request)
        monitor = SignOnMonitor(request)
        assert units.worker(monitor, markers).signon_routes() == {"changed": False, "routes": expected}
        assert not monitor.mutations
    other = gateway.gateway_request()
    with pytest.raises(ceph_native.Refusal):
        units.worker(SignOnMonitor(other), units.markers_for(tmp_path / "other", other)).signon_routes()
    fresh = units.markers_for(tmp_path / "fresh", request)
    os.remove(os.path.join(fresh.path, "owner.json"))
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        units.worker(SignOnMonitor(request), fresh).signon_routes()


def test_each_gateway_host_of_a_sign_on_counts_its_daemons_and_each_sign_on_correction_they_render(tmp_path, root):
    request = signon.sso_request()
    pinned = routes.render("nginx", True, True)
    containers = [("ceph-" + routes.FSID + "-mgmt-gateway-node-1", "running", routes.files(root, "node-1", pinned, PROXIED_CORRECTED)),
                  ("ceph-" + routes.FSID + "-mgmt-gateway-node-1-old", "exited", {}),
                  ("ceph-" + routes.FSID + "-mgmt-gateway.node-1", "running", routes.files(root, "node-1-stale", pinned, PROXIED_NATIVE))]
    monitor = routes.GatewayMonitor(request, containers)
    native = units.worker(monitor, units.markers_for(tmp_path, request))
    assert native.signon_route_proof() == {"changed": False, "daemons": 2, "served": {"cookie": 1, "external": 2, "nginx": 2, "role": 1}}
    assert not monitor.mutations
    monitor.containers = [("ceph-other-mgmt-gateway-node-1", "running", {})]
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        native.signon_route_proof()


def test_a_sign_on_apply_refuses_a_gateway_that_does_not_render_the_corrections_before_its_one_write():
    request = signon.sso_request()
    request["service"]["signOn"]["grafana"] = True
    one = {"changed": False, "daemons": 1, "proved": True}
    now, pause, clock = signon.ticking()
    transport = signon.SSOTransport(request, [one, one], routes=("cookie", "role"), lacking={"node-2": ("role",)},
                                    advance=lambda: clock.__setitem__(0, clock[0] + 100))
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-unprotected$") as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
    assert caught.value.native_error["cause"] == "host node-2: route role"
    called = [phase for unused, phase, unused_private in transport.private]
    assert "sso" not in called and "observe" not in called and "grafana_route" not in called


def test_a_sign_on_apply_proves_the_corrections_on_every_gateway_host_once_before_and_once_after_its_write():
    request = signon.sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    transport = signon.SSOTransport(request, [one, one], routes=("cookie", "role"))
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["ready"] is True
    called = [phase for unused, phase, unused_private in transport.private]
    assert called.count("signon_routes") == 1 and called.count("signon_route_proof") == 4 and "grafana_route" not in called
    assert called.index("sso") > max(index for index, phase in enumerate(called) if phase == "signon_route_proof" and index < called.index("sso"))
    assert called.index("observe") > called.index("sso") and called.index("signon_route_proof", called.index("observe")) > called.index("observe")
