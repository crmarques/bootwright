"""Native monitoring rendering uses the controller's Jinja dependency; recovery stays on the managed-host floor."""

from __future__ import annotations

import pytest
import yaml
from jinja2 import Environment, StrictUndefined

from ansible_collections.bootwright.core.plugins.module_utils import ceph_monitoring_template as template
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_ceph_monitoring_template import HAPROXY_SOURCE, INGRESS, SOURCE


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
