"""The substrate_kubevirt_upload module's main() runs with the bounded runner recorded, as a task would run it.

No virtctl runs: the one bounded runner answers each run from the test.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.modules import substrate_kubevirt_upload as module

ARGUMENTS = {
    "virtctl": "/usr/local/bin/virtctl", "kubeconfig": "/run/lent/kubeconfig", "trust": "/run/lent/ingress.pem",
    "namespace": "guests", "volume": "node-0-0a1b2c3d-media", "image": "/var/lib/example/node-0.iso",
    "proxy": "https://cdi-uploadproxy.apps.example.com", "seconds": 1200,
}


@pytest.fixture(name="runs")
def recorded_runs(monkeypatch):
    runs, answer = [], {"result": (0, b"", b""), "failure": None}

    def run(argv, data=b"", timeout=60, limit=bounded_process.MAX_OUTPUT, *, environment, **options):
        runs.append({"argv": argv, "timeout": timeout, "environment": environment})
        if answer["failure"]:
            raise bounded_process.Failure(answer["failure"])
        return answer["result"]

    monkeypatch.setattr(bounded_process, "run", run)
    return runs, answer


def test_the_image_is_uploaded_by_one_argument_list_trusting_only_the_ingress_authority(runs, run_module):
    recorded, _answer = runs
    result = run_module(module, ARGUMENTS)
    assert result["changed"] is True and not result.get("failed")
    (call,) = recorded
    assert call["argv"][:4] == ["/usr/local/bin/virtctl", "image-upload", "dv", "node-0-0a1b2c3d-media"]
    assert "--no-create" in call["argv"] and "--insecure" not in call["argv"]
    assert call["argv"][call["argv"].index("--uploadproxy-url") + 1] == ARGUMENTS["proxy"]
    assert call["argv"][call["argv"].index("--wait-secs") + 1] == "300"
    assert call["timeout"] == 1200
    assert call["environment"]["SSL_CERT_FILE"] == "/run/lent/ingress.pem" and "https_proxy" not in call["environment"]


def test_a_proxy_that_is_not_one_https_url_is_refused_before_virtctl_runs(runs, run_module):
    recorded, _answer = runs
    result = run_module(module, dict(ARGUMENTS, proxy="http://cdi-uploadproxy.apps.example.com"))
    assert result["failed"] is True and result["msg"] == "the upload proxy is not one https URL"
    assert recorded == []


def test_a_failed_upload_reports_the_last_printable_line_and_a_run_past_its_bound_reports_its_kind(runs, run_module):
    _recorded, answer = runs
    answer["result"] = (1, b"", b"uploading\nx509: certificate signed by unknown authority\x07\n")
    failed = run_module(module, ARGUMENTS)
    assert failed["failed"] is True
    assert failed["msg"] == "virtctl image-upload failed: x509: certificate signed by unknown authority"
    answer["failure"] = "timeout"
    timed_out = run_module(module, ARGUMENTS)
    assert timed_out["failed"] is True and timed_out["msg"] == "virtctl image-upload did not finish: timeout"
