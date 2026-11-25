"""An owned native monitoring template; version differences affect only its pure rendering seam.

The fixed sources are read inside the held image, through HostRuntime's confined executor. A conservative fingerprint
recognizes the complete qualified native rendering/discovery/exporter contract; unfamiliar source bytes stay general.
The management gateway's templates use the same ownership protocol under exact-anchor witnesses instead.
No provider source is shipped, and no release label or component-version pairing selects the correction.
"""

from __future__ import annotations

import hashlib
import json
import re
from collections import namedtuple

from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

MAX_SOURCE = 1 << 20
MAX_TEMPLATE = 65536
KEY = "mgr/cephadm/services/prometheus/prometheus.yml"
ATTEMPT = "monitoring-template-attempt.json"
COMPLETE = "monitoring-template-complete.json"
Template = namedtuple("Template", "key attempt complete")
PROMETHEUS = Template(KEY, ATTEMPT, COMPLETE)
HAPROXY = Template("mgr/cephadm/services/ingress/haproxy.cfg", "monitoring-haproxy-attempt.json", "monitoring-haproxy-complete.json")
HAPROXY_ANCHOR = "{% endif %}\n    monitor-uri /health\n"
EXPORTER = "    http-request use-service prometheus-exporter if { path /metrics }\n"
SOURCES = {
    "template": "/usr/share/ceph/mgr/cephadm/templates/services/prometheus/prometheus.yml.j2",
    "discovery": "/usr/share/ceph/mgr/cephadm/services/service_discovery.py",
    "serviceSpec": "@purelib/ceph/deployment/service_spec.py",
    "haproxy": "/usr/share/ceph/mgr/cephadm/templates/services/ingress/haproxy.cfg.j2",
    "nfs": "/usr/share/ceph/mgr/cephadm/services/nfs.py",
    "smb": "/usr/share/ceph/mgr/cephadm/services/smb.py",
}
# Qualified source bytes of the immutable native artifact, not an image tag or version comparator.
QUALIFIED = {
    'template': '4d2df8cb2afdcd35de5da1bb25c83fbc01649d6f1bd93e1f961a337922cefbbb',
    'discovery': '0c059d38817e0955f1a20b4b160e4d8aa011169c439ba136aad142ee4cdb2915',
    'serviceSpec': '9bf96caa421d680b3bd550897ddc8490309a83b8aa870c4a2b6e5d6621f11dde',
    'haproxy': '5f5142705ab6daf20c822115baae6ac4f447dec3edf4a765e967688f63e0f0f9',
    'nfs': 'df37113bb7cf533a997060597a5674f2573ea93f5c4d88740ef04626dd9defdb',
    'smb': 'aba61b29639d66c731d24d1e349eee7972ad810f3d67799d4b723266fc01a4ab',
}
ANCHOR = "    {% if service == 'ceph' %}\n"
# The management gateway's two templates, owned by its unit. A structural witness on exact anchors, never a digest, selects each
# correction of a template, so every build still carrying a defect is corrected and any other bytes keep the native template.


def owned(key):
    """An owned override's records, named after its key in the declared attempt and completion record families, as a unit's inputs
    record is named after its unit."""
    name = key.replace("/", ".")
    return Template(key, name + "-attempt.json", name + "-complete.json")


NGINX = owned("mgr/cephadm/services/mgmt-gateway/nginx.conf")
EXTERNAL = owned("mgr/cephadm/services/mgmt-gateway/external_server.conf")
# Each override's source name.
ROUTES = {NGINX: "nginx", EXTERNAL: "external"}
GATEWAY_SOURCES = {
    "nginx": "/usr/share/ceph/mgr/cephadm/templates/services/mgmt-gateway/nginx.conf.j2",
    "external": "/usr/share/ceph/mgr/cephadm/templates/services/mgmt-gateway/external_server.conf.j2",
}
UPSTREAM = "    upstream grafana_servers {\n"
AFFINITY = "     ip_hash;\n"
LOCATION = "    location /grafana {\n"
PASS = "        proxy_pass {{ grafana_scheme }}://grafana_servers;\n"
HOST = "        {% if not enable_oauth2_proxy %}\n        proxy_set_header Host $host;\n        {% endif %}\n"
PROXIED = "        {% if enable_oauth2_proxy %}\n"
BRANCH_END = "        {% endif %}\n"
BLOCK_END = "\n    }\n"
BALANCING = ("ip_hash", "hash ", "sticky", "least_conn", "random")
# The sign-on corrections of external_server.conf's two oauth2-proxy branches: the dashboard location forwards no client cookie, since
# in OAuth2 mode the dashboard reads a `token` cookie before the gateway's X-Access-Token and decodes it unverified, and Grafana's
# location sends a fixed role instead of the client's own X-Auth-Request-Role header.
DASHBOARD = "    location / {\n"
DASHBOARD_PASS = "        proxy_pass {{ dashboard_scheme }}://dashboard_servers;\n"
TOKEN = "        proxy_set_header X-Access-Token $token;\n"
COOKIE = '        proxy_set_header Cookie "";\n'
ROLE = "        proxy_set_header X-WEBAUTH-ROLE $http_x_auth_request_role;\n"
VIEWER = '        proxy_set_header X-WEBAUTH-ROLE "Viewer";\n'
SOURCE_SCRIPT = """import json,os,stat,sys,sysconfig
paths=json.loads(sys.argv[1]); result={}
for name,path in paths.items():
    path=path.replace('@purelib',sysconfig.get_path('purelib'))
    try:
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    except FileNotFoundError:
        result[name]=None
        continue
    with os.fdopen(fd,'rb') as handle:
        info=os.fstat(handle.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > 1048576:
            raise ValueError("source bound")
        data=handle.read(1048577)
        if len(data) > 1048576:
            raise ValueError("source bound")
        result[name]=data.decode('utf-8')
print(json.dumps(result))
"""


def source_shape(value):
    """Raw source has its own bounded parser; never pretend it is an argparse zipapp."""
    if not isinstance(value, dict) or set(value) != set(SOURCES):
        raise ValueError("source shape")
    if not all(isinstance(text, str) and 0 < len(text.encode()) <= MAX_SOURCE for text in value.values()):
        raise ValueError("source bound")
    return True


def source_digests(value):
    return {name: hashlib.sha256(text.encode()).hexdigest() for name, text in value.items()}


def blanket_https(value):
    """Only the qualified source contract witnesses this defect; a changed or absent source is unknown."""
    source_shape(value)
    return True if source_digests(value) == QUALIFIED else None


def relabels(ingress):
    """Exact service labels: HTTP statistics or no exporter; every other ingress retains native behavior."""
    lines = ["    {% if service in ['nfs', 'smb'] %}", "    - target_label: __scheme__", "      replacement: http", "    {% endif %}"]
    for mode in ("http", "drop"):
        names = sorted(name for name, value in ingress.items() if value == mode)
        if not names:
            continue
        pattern = "(?:" + "|".join(re.escape(name) for name in names) + ")"
        lines.extend(["    {% if service == 'ingress' %}", "    - source_labels: [ingress]", "      regex: " + json.dumps(pattern)])
        lines.extend(["      target_label: __scheme__", "      replacement: http"] if mode == "http" else ["      action: drop"])
        lines.append("    {% endif %}")
    return "\n".join(lines) + "\n"


def corrected(template, ingress):
    """One insertion, preserving discovery credentials/TLS, secured targets, alerting, federation and remote write."""
    if template.count(ANCHOR) != 1 or "__scheme__" in template:
        raise ValueError("ambiguous native template")
    data = template.replace(ANCHOR, relabels(ingress) + ANCHOR).encode()
    if len(data) > MAX_TEMPLATE:
        raise ValueError("template bound")
    return data


def corrected_haproxy(template, ingress):
    """Enable only the named ingress metrics, keeping the native stats UI/auth and every unlisted service unchanged."""
    names = sorted(name for name, mode in ingress.items() if mode == "http")
    if template.count(HAPROXY_ANCHOR) != 1 or template.count(EXPORTER) != 1:
        raise ValueError("ambiguous native ingress template")
    addition = ("{% if not spec.enable_stats and spec.service_name() in " + json.dumps(names) + " %}\n"
                + EXPORTER + "{% endif %}\n")
    data = template.replace(HAPROXY_ANCHOR, "{% endif %}\n" + addition + "    monitor-uri /health\n").encode()
    if len(data) > MAX_TEMPLATE:
        raise ValueError("template bound")
    return data


def plans(worker, unit):
    prometheus = desired(worker, unit)
    haproxy = None
    if prometheus is not None and "http" in unit.get("scrapeIngress", {}).values():
        sources, unused = worker.selection.source("image-monitoring")
        haproxy = corrected_haproxy(sources["haproxy"], unit["scrapeIngress"]), source_digests(sources)
    return ((PROMETHEUS, prometheus), (HAPROXY, haproxy))


def block(text, anchor):
    """The body after the one exact anchor up to its block's first closing line; None when the anchor is absent, repeated or unclosed."""
    if text.count(anchor) != 1 or BLOCK_END not in text.partition(anchor)[2]:
        return None
    return text.partition(anchor)[2].partition(BLOCK_END)[0] + "\n"


def gateway_shape(value):
    if not isinstance(value, dict) or set(value) != set(GATEWAY_SOURCES):
        raise ValueError("source shape")
    if not all(isinstance(text, str) and 0 < len(text.encode()) <= MAX_SOURCE for text in value.values()):
        raise ValueError("source bound")


def upstream_unpinned(template):
    """True when Grafana's one upstream names no balancing directive, False when it names one, None when its anchor is absent or repeated."""
    upstream = block(template, UPSTREAM)
    if upstream is None:
        return None
    return not any(line.strip().lower().startswith(BALANCING) for line in upstream.splitlines())


def proxied_branch(template, anchor, opening):
    """(the location's lines outside its branch, the branch) of the one location an exact anchor opens with its one proxy_pass and one
    unnested oauth2-proxy branch; None for any other shape."""
    location = block(template, anchor)
    if location is None or not location.startswith(opening) or location.count(opening) != 1 or location.count(PROXIED) != 1:
        return None
    before, unused, rest = location.partition(PROXIED)
    branch, closed, after = rest.partition(BRANCH_END)
    if not closed or "{%" in branch or "{%" in before + after:
        return None
    return before + after, branch


def location_unhosted(template):
    """True when Grafana's one location opens with its proxy_pass and sets Host only inside its one oauth2-proxy branch, False when it
    sets Host outside it, None for any other shape."""
    shape = proxied_branch(template, LOCATION, PASS)
    if shape is None:
        return None
    return "proxy_set_header host" not in shape[0].lower()


def cookie_forwarded(template):
    """True when the dashboard's one location forwards the client's cookies to the dashboard: it sets no Cookie header and its oauth2-proxy
    branch holds the template's one X-Access-Token line. False only when the one Cookie header the location sets is, inside that branch,
    the empty one; None for any other shape, another Cookie value included."""
    shape = proxied_branch(template, DASHBOARD, DASHBOARD_PASS)
    if shape is None:
        return None
    outside, branch = shape
    cookies = [line for line in (outside + branch).splitlines() if "proxy_set_header cookie" in line.lower()]
    if cookies:
        return False if cookies == [COOKIE.rstrip("\n")] and branch.count(COOKIE) == 1 else None
    return True if branch.count(TOKEN) == 1 and template.count(TOKEN) == 1 else None


def role_chosen(template):
    """True when Grafana's one location sends, inside its oauth2-proxy branch, the client's own X-Auth-Request-Role header as the Grafana
    role, on the template's one such line. False when every role line it sets takes no client header, None for any other shape."""
    shape = proxied_branch(template, LOCATION, PASS)
    if shape is None:
        return None
    outside, branch = shape
    roles = [line for line in (outside + branch).splitlines() if "x-webauth-role" in line.lower()]
    if len(roles) == 1 and branch.count(ROLE) == 1 and template.count(ROLE) == 1:
        return True
    return False if roles and not any("$http_" in line.lower() for line in roles) else None


def gateway_witness(value):
    """Each gateway template's own Grafana routing defect witness, by source name: True, False or None (unknown)."""
    gateway_shape(value)
    return {"nginx": upstream_unpinned(value["nginx"]), "external": location_unhosted(value["external"])}


def sign_on_witness(value):
    """Each sign-on defect witness of external_server.conf, by correction name: True, False or None (unknown)."""
    gateway_shape(value)
    return {"cookie": cookie_forwarded(value["external"]), "role": role_chosen(value["external"])}


def either(witness):
    """True when any witness holds its defect, False when none does and every anchor was read, None otherwise."""
    if True in witness.values():
        return True
    return False if None not in witness.values() else None


def gateway_facts(value):
    """The gateway source's two facts, each from its own witnesses."""
    return {"grafana-unrouted": grafana_unrouted(value), "sign-on-forwarded": either(sign_on_witness(value))}


def grafana_unrouted(value):
    """True when either template witnesses its own defect; each is then corrected alone. False when neither does and both anchors were
    read; None otherwise, which keeps the native templates."""
    return either(gateway_witness(value))


def routed_upstream(template):
    """Pin each client to one Grafana: one affinity line opens Grafana's upstream; every other byte stays native."""
    upstream = block(template, UPSTREAM)
    if upstream is None or "ip_hash" in upstream:
        raise ValueError("ambiguous native gateway template")
    data = template.replace(UPSTREAM, UPSTREAM + AFFINITY).encode()
    if len(data) > MAX_TEMPLATE:
        raise ValueError("template bound")
    return data


def routed_location(template):
    """Forward the client's Host to Grafana without an oauth2-proxy, whose native branch already sets it, so one Host header is ever sent."""
    if template.count(LOCATION + PASS) != 1 or template.count(HOST) != 0:
        raise ValueError("ambiguous native gateway template")
    data = template.replace(LOCATION + PASS, LOCATION + PASS + HOST).encode()
    if len(data) > MAX_TEMPLATE:
        raise ValueError("template bound")
    return data


def cleared_cookie(template):
    """Forward no client cookie to the dashboard: one empty Cookie header follows the X-Access-Token line of its oauth2-proxy branch, so the
    dashboard reads only the token the gateway's oauth2-proxy issued."""
    if template.count(DASHBOARD + DASHBOARD_PASS) != 1 or template.count(TOKEN) != 1 or "proxy_set_header cookie" in template.lower():
        raise ValueError("ambiguous native gateway template")
    return bounded(template.replace(TOKEN, TOKEN + COOKIE))


def viewer_role(template):
    """Send Grafana the fixed role Viewer in place of the client's own role header, so no signed-in user chooses their Grafana role."""
    if template.count(ROLE) != 1 or template.count(VIEWER) != 0:
        raise ValueError("ambiguous native gateway template")
    return bounded(template.replace(ROLE, VIEWER))


def bounded(text):
    data = text.encode()
    if len(data) > MAX_TEMPLATE:
        raise ValueError("template bound")
    return data


# Every correction by name: the override it changes and its insertion, applied in this order to one template.
CORRECTIONS = {"nginx": (NGINX, routed_upstream), "external": (EXTERNAL, routed_location), "cookie": (EXTERNAL, cleared_cookie),
               "role": (EXTERNAL, viewer_role)}


def gateway_corrections(worker, unit):
    """The selected corrections by name, in CORRECTIONS order. The Grafana routes need a fronted Grafana and their seam; the sign-on
    corrections need only their own seam, the role one a fronted Grafana too: they sit inside the template's oauth2-proxy branches, inert until a
    CephSignOn's oauth2-proxy runs, and a sign-on added later finds them already in place. Each needs its own witness."""
    fronted, chosen = "grafana" in unit.get("monitoring", []), set()
    if fronted and worker.selection.variant("ceph.gateway.grafana-route") == "host-and-affinity":
        witness = gateway_witness(worker.selection.source("image-gateway")[0])
        chosen.update(name for name in ("nginx", "external") if witness[name] is True)
    if worker.selection.variant("ceph.gateway.sign-on-headers") == "gateway-identity":
        witness = sign_on_witness(worker.selection.source("image-gateway")[0])
        chosen.update(name for name in ("cookie", "role") if witness[name] is True and (fronted or name == "cookie"))
    return [name for name in CORRECTIONS if name in chosen]


def gateway_plans(worker, unit):
    """The gateway's two owned overrides: each template, when a selected correction changes it, with every selected correction of that
    template applied in order; every other byte stays native."""
    names, wanted = gateway_corrections(worker, unit), {NGINX: None, EXTERNAL: None}
    if names:
        sources, unused = worker.selection.source("image-gateway")
        digests = source_digests(sources)
        for descriptor in wanted:
            corrections = [correct for name, (target, correct) in CORRECTIONS.items() if name in names and target == descriptor]
            text = sources[ROUTES[descriptor]]
            for correct in corrections:
                text = correct(text).decode()
            if corrections:
                wanted[descriptor] = text.encode(), digests
    return ((NGINX, wanted[NGINX]), (EXTERNAL, wanted[EXTERNAL]))


def image_sources(worker, sources=None):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
    profile = worker.runtime_profile()
    cluster = {key: value for key, value in worker.request.items() if key not in ("service", "gateway")}
    runner = runtime.HostRuntime(dict(cluster, action="cluster"), worker.host["name"], command=worker.run)
    paths = canonical(SOURCES if sources is None else sources).decode()
    return runner.contained(profile["imageID"], (profile["uid"], profile["gid"]), SOURCE_SCRIPT, (paths,), limit=8 << 20)


def desired(worker, unit):
    """The only version decision: normal artifacts need no override. Ownership is checked outside this seam."""
    if not any(document["service_type"] == "prometheus" for document in unit["documents"]):
        return None
    if worker.selection.variant("ceph.monitoring.scrape-template") == "native":
        return None
    sources, unused = worker.selection.source("image-monitoring")
    return corrected(sources["template"], unit.get("scrapeIngress", {})), source_digests(sources)


def native_value(worker, descriptor=PROMETHEUS):
    from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import require
    keys = worker.ceph("config-key", "ls")
    require(isinstance(keys, list) and len(keys) <= 65536 and all(isinstance(key, str) for key in keys), "native-output")
    overrides = {key for key in keys if key.startswith("mgr/cephadm/") and key.endswith("/" + descriptor.key.removeprefix("mgr/cephadm/"))}
    require(overrides <= {descriptor.key}, "monitoring-template-identity")
    if descriptor.key not in overrides:
        return None
    value = worker.ceph_text("config-key", "get", descriptor.key)
    require(isinstance(value, bytes) and len(value) <= MAX_TEMPLATE + 1, "native-output")
    # config-key get adds one newline to its raw value. Accept that CLI framing only.
    return value


def same(value, wanted):
    return value in (wanted, wanted + b"\n")


def state(worker, plan, descriptor=PROMETHEUS):
    from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import require
    attempted, completed = worker.markers.read(descriptor.attempt), worker.markers.read(descriptor.complete)
    if plan is None:
        require(attempted is None and completed is None, "monitoring-template-identity")
        return None
    wanted, sources = plan
    intent = {"version": "ceph-monitoring-template-v1", "scope": worker.recovery_scope(), "sources": sources,
              "imageID": worker.runtime_profile()["imageID"], "sha256": hashlib.sha256(wanted).hexdigest()}
    require(attempted is None or attempted == intent, "monitoring-template-identity")
    require(completed is None or completed == intent and attempted == intent, "monitoring-template-identity")
    present = native_value(worker, descriptor)
    require(present is None or attempted == intent and same(present, wanted), "monitoring-template-identity")
    require(completed is None or same(present, wanted), "monitoring-template-identity")
    return intent, wanted, present, attempted, completed


def prepare(worker, plan, descriptor=PROMETHEUS):
    """Attempt before write; retry absence only while incomplete; exact readback settles a lost reply."""
    from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import Refusal, require
    found = state(worker, plan, descriptor)
    if found is None:
        return False
    intent, wanted, present, attempted, completed = found
    if attempted is None:
        worker.markers.write(descriptor.attempt, intent)
    present = state(worker, plan, descriptor)[2]  # reprove at the write boundary
    changed = present is None
    if changed:
        try:
            worker.tunnel_effect("config-key", "set", descriptor.key, "-i", "-", data=wanted, json_result=False)
        except Refusal:
            if not same(native_value(worker, descriptor), wanted):
                raise
        require(same(native_value(worker, descriptor), wanted), "monitoring-template-identity")
    if completed is None:
        worker.markers.write(descriptor.complete, intent)
    return changed


def ready(worker, plan, descriptor=PROMETHEUS):
    found = state(worker, plan, descriptor)
    return found is None or found[4] is not None
