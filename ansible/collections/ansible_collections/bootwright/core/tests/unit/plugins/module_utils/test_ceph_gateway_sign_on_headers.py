"""The management gateway's sign-on corrections: exact-anchor witnesses select them, they render no client cookie to the dashboard and the
fixed Grafana role Viewer under cephadm's Jinja settings, they compose with the Grafana routes in the one external_server.conf override, and
the route proof counts them.

The template below is synthetic text shaped at the exact anchors of the IBM image's mgmt-gateway template
(/usr/share/ceph/mgr/cephadm/templates/services/mgmt-gateway/external_server.conf.j2:78-113 and :117-147 in
cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074), never provider bytes. In OAuth2 mode the dashboard reads a `token` cookie before the
X-Access-Token header (src/pybind/mgr/dashboard/services/auth/oauth2.py, get_token) and decodes it without verifying it, and Grafana's
auth proxy maps `Role:X-WEBAUTH-ROLE` (cephadm's grafana.ini.j2).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_monitoring_template as template, ceph_native, ceph_native_observability as observability,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_gateway_grafana_routes as routes
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway_sso as sso
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

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
CONTEXT = dict(routes.CONTEXT, dashboard_endpoints=["192.0.2.11:8443"], dashboard_scheme="https")
FIXED_COOKIE = SIGNED.replace(template.TOKEN, template.TOKEN + "        proxy_set_header Cookie $filtered;\n")
FIXED_ROLE = SIGNED.replace(template.ROLE, "        proxy_set_header X-WEBAUTH-ROLE $role;\n")
DOCUMENTS = [{"service_type": "mgmt-gateway"}, {"service_type": "oauth2-proxy"}]
SIGNED_ON = {"documents": DOCUMENTS, "monitoring": ["grafana", "prometheus"], "type": "mgmt-gateway"}
PROXY_ONLY = {"documents": DOCUMENTS, "type": "mgmt-gateway"}
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


def test_an_ambiguous_or_oversized_template_refuses_each_sign_on_correction():
    for source in (SIGNED + template.TOKEN, FIXED_COOKIE, template.cleared_cookie(SIGNED).decode(), SIGNED.replace(template.DASHBOARD_PASS, "")):
        with pytest.raises(ValueError, match="ambiguous"):
            template.cleared_cookie(source)
    for source in (SIGNED + template.ROLE, template.viewer_role(SIGNED).decode(), SIGNED.replace(template.ROLE, "")):
        with pytest.raises(ValueError, match="ambiguous"):
            template.viewer_role(source)
    with pytest.raises(ValueError, match="bound"):
        template.viewer_role(SIGNED + "#" * template.MAX_TEMPLATE)


def test_the_sign_on_corrections_join_the_grafana_routes_in_one_override_only_with_an_oauth2_proxy():
    plans = dict(template.gateway_plans(routes.Worker(SOURCES), SIGNED_ON))
    assert template.gateway_corrections(routes.Worker(SOURCES), SIGNED_ON) == ["nginx", "external", "cookie", "role"]
    assert plans[template.NGINX][0] == template.routed_upstream(routes.NGINX_SOURCE)
    assert plans[template.EXTERNAL][0] == composed(SIGNED, template.routed_location, template.cleared_cookie, template.viewer_role)
    assert plans[template.EXTERNAL][1] == template.source_digests(SOURCES)
    proxy_only = dict(template.gateway_plans(routes.Worker(SOURCES), PROXY_ONLY))
    assert template.gateway_corrections(routes.Worker(SOURCES), PROXY_ONLY) == ["cookie"]
    assert proxy_only[template.NGINX] is None and proxy_only[template.EXTERNAL][0] == template.cleared_cookie(SIGNED)
    worker = routes.Worker(SOURCES)
    unsigned = dict(template.gateway_plans(worker, routes.FRONTED))
    assert unsigned[template.EXTERNAL][0] == template.routed_location(SIGNED) and template.gateway_corrections(worker, routes.FRONTED) == [
        "nginx", "external"]
    assert worker.selection.applied == {"ceph-gateway-grafana-sessions"}
    fixed = routes.Worker(dict(SOURCES, external=FIXED_COOKIE))
    assert template.gateway_corrections(fixed, SIGNED_ON) == ["nginx", "external", "role"]
    assert dict(template.gateway_plans(fixed, SIGNED_ON))[template.EXTERNAL][0] == composed(FIXED_COOKIE, template.routed_location,
                                                                                            template.viewer_role)
    assert all(plan is None for unused, plan in template.gateway_plans(routes.Worker(SOURCES), {"documents": DOCUMENTS[:1], "type": "mgmt-gateway"}))


class SignOnMonitor(sso.SSOMonitor):
    """The SSO gateway's scripted monitor with cephadm's config-key store."""

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


def test_a_signed_on_gateway_completes_its_cookie_override_before_its_stream(tmp_path, sources, monkeypatch):
    del sources
    monkeypatch.setattr(template, "image_sources", lambda worker, paths=None: dict(SOURCES))
    request = sso.sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SignOnMonitor(request)
    assert gateway.applying(monitor, markers, sso.SSO_MATERIAL).unit() == {"changed": True}
    sets = [index for index, command in enumerate(monitor.mutations) if command[:2] == ("config-key", "set")]
    assert len(sets) == 1 and sets[0] < monitor.mutations.index(("orch", "apply", "-i", "-"))
    assert monitor.stored == {template.EXTERNAL.key: template.cleared_cookie(SIGNED)}
    assert units.worker(monitor, markers).grafana_route() == {"changed": False, "routes": ["cookie"]}


def test_a_signed_on_gateway_without_grafana_still_proves_its_routes():
    transport = sso.SSOTransport(sso.sso_request(), [{"changed": False, "daemons": 1, "proved": True}] * 3)
    ceph_core.execute(transport.request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert ("node-1", "grafana_route") in [(name, phase) for name, phase, unused in transport.private]


def test_a_stale_gateway_is_reconfigured_until_it_serves_the_sign_on_routes():
    transport = routes.RouteTransport(routes.fronting(), "stale", routes=("cookie", "role"))
    assert routes.applying(transport)["evidence"]["ready"] is True and transport.reconfigured == 1
    for named in (["role", "cookie"], ["cookie", "token"]):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            routes.applying(routes.RouteTransport(routes.fronting(), "routed", routes=named))
