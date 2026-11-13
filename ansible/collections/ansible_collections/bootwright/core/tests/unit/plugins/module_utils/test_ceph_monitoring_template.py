"""Owned native-template recovery and target-only rendering; synthetic sources, never provider source bytes."""

from __future__ import annotations

import contextlib
import copy
import json

import pytest
import yaml
from jinja2 import Environment, StrictUndefined

from ansible_collections.bootwright.core.plugins.module_utils import ceph_monitoring_template as template, ceph_native, native_specializations
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring, test_ceph_service_units as units

# A small synthetic template exercises the native context and relabel insertion without copying a provider template.
SOURCE = """alerting:
  alertmanagers:
    - scheme: https
      tls_config: {ca_file: alert-root.pem}
scrape_configs:
{% for service in services %}
  - job_name: {{ service }}
    relabel_configs:
    - target_label: cluster
      replacement: example
    {% if service == 'ceph' %}
    - target_label: instance
      replacement: example
    {% endif %}
    scheme: https
    tls_config: {ca_file: root.pem, cert_file: client.crt, key_file: client.key}
    http_sd_configs:
      - url: https://discovery.example.test/sd
        basic_auth: {username: user, password: sentinel}
        tls_config: {ca_file: root.pem}
{% endfor %}
remote_write:
  - url: https://remote.example.test/
"""
INGRESS = {"ingress.objects": "http", "ingress.shares": "http", "ingress.management": "drop"}


def sources():
    return {name: SOURCE if name == "template" else "synthetic:" + name for name in template.SOURCES}


@pytest.fixture
def qualified(monkeypatch):
    monkeypatch.setattr(template, "QUALIFIED", template.source_digests(sources()))
    return sources()


def selection(value):
    return native_specializations.Selection({"image-monitoring": lambda: value}, {"image-monitoring": template.blanket_https})


def test_artifact_contract_selects_without_a_release_or_image_tag(qualified):
    chosen = selection(qualified)
    assert chosen.variant("ceph.monitoring.scrape-template") == "plaintext-exporters"
    assert chosen.applied == {"ceph-monitoring-plaintext-exporters"}
    for name in qualified:
        altered = dict(qualified, **{name: qualified[name] + "changed"})
        assert selection(altered).variant("ceph.monitoring.scrape-template") == "native"


@pytest.mark.parametrize("value", [None, {}, {"template": SOURCE}, dict.fromkeys(template.SOURCES, None),
                                  dict.fromkeys(template.SOURCES, "x" * (template.MAX_SOURCE + 1))])
def test_unreadable_unrecognized_and_oversized_sources_stay_general(value):
    chosen = selection(value)
    assert chosen.variant("ceph.monitoring.scrape-template") == "native" and not chosen.applied


def test_target_relabels_preserve_discovery_credentials_and_other_tls():
    patched = template.corrected(SOURCE, INGRESS).decode()
    assert patched.replace(template.relabels(INGRESS), "", 1) == SOURCE
    rendered = Environment(trim_blocks=True, lstrip_blocks=True, undefined=StrictUndefined).from_string(patched).render(
        services=["ceph", "node-exporter", "ceph-exporter", "nfs", "smb", "ingress", "nvmeof"])
    config = yaml.safe_load(rendered)
    for job in config["scrape_configs"]:
        assert job["scheme"] == "https"
        assert job["tls_config"]["key_file"] == "client.key"
        assert job["http_sd_configs"][0]["url"].startswith("https://")
        assert job["http_sd_configs"][0]["basic_auth"]["password"] == "sentinel"
        rules = job["relabel_configs"]
        scheme = [rule for rule in rules if rule.get("target_label") == "__scheme__"]
        if job["job_name"] in ("nfs", "smb"):
            assert scheme == [{"target_label": "__scheme__", "replacement": "http"}]
        elif job["job_name"] == "ingress":
            import re
            assert len(scheme) == 1
            assert re.fullmatch(scheme[0]["regex"], "ingress.objects")
            assert not re.fullmatch(scheme[0]["regex"], "ingressXobjects")
            assert not re.fullmatch(scheme[0]["regex"], "ingress.secure")
            drops = [rule for rule in rules if rule.get("action") == "drop"]
            assert len(drops) == 1 and re.fullmatch(drops[0]["regex"], "ingress.management")
            assert not re.fullmatch(drops[0]["regex"], "ingress.shares")
        else:
            assert not scheme
    assert config["alerting"]["alertmanagers"][0]["scheme"] == "https"
    assert config["remote_write"][0]["url"].startswith("https://")


@pytest.mark.parametrize("source", [SOURCE + template.ANCHOR, SOURCE.replace(template.ANCHOR, ""), SOURCE + "__scheme__"])
def test_ambiguous_native_rendering_refuses(source):
    with pytest.raises(ValueError):
        template.corrected(source, INGRESS)


class Records:
    def __init__(self):
        self.values = {}
        self.fail = None

    def read(self, name):
        return copy.deepcopy(self.values.get(name))

    def write(self, name, value):
        if self.fail == name:
            raise ceph_native.Refusal("native-command")
        self.values[name] = copy.deepcopy(value)


class Worker:
    def __init__(self, source):
        self.selection = selection(source)
        self.markers = Records()
        self.value = None
        self.keys = []
        self.writes = []
        self.failure = None
        self.request = {}

    def recovery_scope(self):
        return {"owner": "synthetic-operation", "fsid": units.FSID}

    def runtime_profile(self):
        return {"imageID": "a" * 64}

    def ceph(self, *args):
        assert args == ("config-key", "ls")
        return self.keys + ([template.KEY] if self.value is not None else [])

    def ceph_text(self, *args):
        assert args == ("config-key", "get", template.KEY)
        return self.value + b"\n"

    def tunnel_effect(self, *args, data, json_result):
        assert self.markers.read(template.ATTEMPT) is not None
        assert args == ("config-key", "set", template.KEY, "-i", "-") and json_result is False
        self.writes.append(args)
        if self.failure != "before":
            self.value = data
        if self.failure:
            raise ceph_native.Refusal("native-command")


UNIT = {"documents": [{"service_type": "prometheus"}], "scrapeIngress": INGRESS}


def test_attempt_write_readback_completion_and_read_only_replay(qualified):
    worker = Worker(qualified)
    assert not template.ready(worker, template.desired(worker, UNIT)) and not worker.markers.values
    assert template.prepare(worker, template.desired(worker, UNIT))
    assert template.ready(worker, template.desired(worker, UNIT)) and len(worker.writes) == 1
    before = copy.deepcopy(worker.markers.values)
    assert not template.prepare(worker, template.desired(worker, UNIT))
    assert template.ready(worker, template.desired(worker, UNIT)) and worker.markers.values == before and len(worker.writes) == 1


@pytest.mark.parametrize("value", [b"foreign", template.corrected(SOURCE, INGRESS)])
def test_unowned_override_refuses_even_when_bytes_match(qualified, value):
    worker = Worker(qualified)
    worker.value = value
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.prepare(worker, template.desired(worker, UNIT))
    assert not worker.writes and not worker.markers.values


@pytest.mark.parametrize("drift", [None, b"foreign", b""])
def test_completed_missing_or_changed_value_refuses_without_writes(qualified, drift):
    worker = Worker(qualified)
    template.prepare(worker, template.desired(worker, UNIT))
    worker.value = drift
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.ready(worker, template.desired(worker, UNIT))
    assert len(worker.writes) == 1


def test_host_specific_override_refuses(qualified):
    worker = Worker(qualified)
    worker.keys = ["mgr/cephadm/node-a/services/prometheus/prometheus.yml"]
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.prepare(worker, template.desired(worker, UNIT))
    assert not worker.writes


@pytest.mark.parametrize("failure", ["before", "after", "completion"])
def test_interrupted_effect_resumes_only_its_exact_attempt(qualified, failure):
    worker = Worker(qualified)
    worker.failure = failure if failure != "completion" else None
    worker.markers.fail = template.COMPLETE if failure == "completion" else None
    if failure != "after":
        with pytest.raises(ceph_native.Refusal):
            template.prepare(worker, template.desired(worker, UNIT))
        assert worker.markers.read(template.ATTEMPT) is not None and worker.markers.read(template.COMPLETE) is None
    else:
        assert template.prepare(worker, template.desired(worker, UNIT))  # a lost reply settles through readback
    worker.failure = worker.markers.fail = None
    template.prepare(worker, template.desired(worker, UNIT))
    assert template.ready(worker, template.desired(worker, UNIT))
    assert len(worker.writes) == (2 if failure == "before" else 1)


def test_different_image_scope_or_ingress_intent_never_adopts_attempt(qualified):
    worker = Worker(qualified)
    template.prepare(worker, template.desired(worker, UNIT))
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.prepare(worker, template.desired(worker, dict(UNIT, scrapeIngress={})))
    worker.runtime_profile = lambda: {"imageID": "b" * 64}
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.ready(worker, template.desired(worker, UNIT))


def test_source_change_with_owned_override_refuses_instead_of_masking_new_native_template(qualified):
    worker = Worker(qualified)
    template.prepare(worker, template.desired(worker, UNIT))
    worker.selection = selection(dict(qualified, template=SOURCE + "fixed"))
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        template.prepare(worker, template.desired(worker, UNIT))


def test_unit_does_not_apply_before_template_is_completed(tmp_path, monkeypatch):
    request = monitoring.monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    # The existing fixture supplies native SDK normalization and held-image proofs.
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, source, arguments=(), **unused: json.loads(arguments[0]))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    worker = monitoring.applying(monitor, markers)
    monkeypatch.setattr(template, "state", lambda *unused: None)

    def refuse(*unused):
        assert not monitor.mutations
        raise ceph_native.Refusal("monitoring-template-identity")

    monkeypatch.setattr(template, "prepare", refuse)
    with pytest.raises(ceph_native.Refusal, match="^monitoring-template-identity$"):
        worker.unit()
    assert not monitor.mutations


def test_completed_template_survives_a_crash_before_monitoring_apply(tmp_path, monkeypatch, qualified):
    request = monitoring.monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    current = [None]
    original = monitor.run

    def run(argv, data=b"", timeout=60):
        if "config-key" not in argv:
            return original(argv, data=data, timeout=timeout)
        command = tuple(argv[argv.index("config-key"):])
        if command[1] == "ls":
            return 0, json.dumps([] if current[0] is None else [template.KEY]).encode()
        if command[1] == "get":
            return 0, current[0] + b"\n"
        assert command == ("config-key", "set", template.KEY, "-i", "-")
        assert markers.read(template.ATTEMPT) is not None
        monitor.mutations.append(command)
        current[0] = data
        return 0, b""

    monitor.run = run
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, source, arguments=(), **unused: json.loads(arguments[0]))
    monitor.before_store = ceph_native.Refusal("native-command")
    worker = monitoring.applying(monitor, markers)
    worker.selection = selection(qualified)
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        worker.unit()
    assert markers.read(template.COMPLETE) is not None
    monitor.before_store = None
    assert worker.unit() == {"changed": True}
    assert sum(command[:2] == ("config-key", "set") for command in monitor.mutations) == 1
    assert markers.read("service-prometheus-complete.json") is not None


HAPROXY_SOURCE = """frontend stats
{% if spec.enable_stats %}
    stats enable
    stats auth synthetic:synthetic
    http-request use-service prometheus-exporter if { path /metrics }
{% endif %}
    monitor-uri /health
frontend frontend
    default_backend backend
"""


@pytest.mark.parametrize("name", ["ingress.objects", "ingress.unlisted", "ingress.management"])
@pytest.mark.parametrize("enabled", [None, False, True])
def test_ingress_metrics_preserve_stats_auth_and_unlisted_native_behavior(name, enabled):
    class Spec:
        enable_stats = enabled

        def service_name(self):
            return name

    engine = Environment(trim_blocks=True, lstrip_blocks=True, undefined=StrictUndefined)
    original = engine.from_string(HAPROXY_SOURCE).render(spec=Spec())
    corrected = engine.from_string(template.corrected_haproxy(HAPROXY_SOURCE, INGRESS).decode()).render(spec=Spec())
    exporter = template.EXPORTER.strip()
    assert corrected.count(exporter) == int(bool(enabled) or name == "ingress.objects")
    assert ("stats enable" in corrected) is bool(enabled)
    assert ("stats auth synthetic:synthetic" in corrected) is bool(enabled)
    if name != "ingress.objects" or enabled:
        assert corrected == original


def test_two_template_attempts_resume_independently(qualified):
    worker = Worker(qualified)
    stored = {}
    worker.ceph = lambda *args: list(stored)
    worker.ceph_text = lambda *args: stored[args[-1]] + b"\n"

    def effect(*args, data, json_result):
        assert json_result is False
        if args[2] == template.HAPROXY.key and not stored.get("retry"):
            raise ceph_native.Refusal("native-command")
        stored[args[2]] = data

    worker.tunnel_effect = effect
    prometheus = template.desired(worker, UNIT)
    ingress = template.corrected_haproxy(HAPROXY_SOURCE, INGRESS), template.source_digests(qualified)
    assert template.prepare(worker, prometheus, template.PROMETHEUS)
    with pytest.raises(ceph_native.Refusal, match="^native-command$"):
        template.prepare(worker, ingress, template.HAPROXY)
    assert worker.markers.read(template.COMPLETE) is not None
    assert worker.markers.read(template.HAPROXY.complete) is None
    stored["retry"] = b"true"
    assert not template.prepare(worker, prometheus, template.PROMETHEUS)
    assert template.prepare(worker, ingress, template.HAPROXY)
    assert template.ready(worker, prometheus, template.PROMETHEUS) and template.ready(worker, ingress, template.HAPROXY)


def test_ingress_template_refuses_oversized_output_before_any_write():
    with pytest.raises(ValueError, match="^template bound$"):
        template.corrected_haproxy(HAPROXY_SOURCE, {"ingress." + "x" * template.MAX_TEMPLATE: "http"})
