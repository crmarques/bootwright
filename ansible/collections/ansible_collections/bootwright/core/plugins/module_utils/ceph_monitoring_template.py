"""An owned native monitoring template; version differences affect only its pure rendering seam.

The fixed sources are read inside the held image, through HostRuntime's confined executor. A conservative fingerprint
recognizes the complete qualified native rendering/discovery/exporter contract; unfamiliar source bytes stay general.
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


def image_sources(worker):
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
    profile = worker.runtime_profile()
    cluster = {key: value for key, value in worker.request.items() if key not in ("service", "gateway")}
    runner = runtime.HostRuntime(dict(cluster, action="cluster"), worker.host["name"], command=worker.run)
    return runner.contained(profile["imageID"], (profile["uid"], profile["gid"]), SOURCE_SCRIPT, (canonical(SOURCES).decode(),), limit=8 << 20)


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
