"""The management gateway unit's monitoring logins: after its readiness, and on an observation an operation records, it reads the
basic-auth login cephadm keeps for each declared Prometheus and Alertmanager and writes them to its protected output only.

The scripted monitor answers `orch prometheus get-credentials` and `orch alertmanager get-credentials` in the upstream v20.2.1 shape:
`{"user", "password", "certificate"}` while cephadm's monitoring security is enabled, which a running management gateway enables, and `{}`
otherwise (src/pybind/mgr/orchestrator/module.py, _get_prometheus_access_info and _get_alertmanager_access_info;
src/pybind/mgr/cephadm/module.py, get_prometheus_access_info, _get_prometheus_credentials and _get_security_config). cephadm stores
admin/admin when it holds no login, so asking is a write (`orch <service> get-credentials` is a `_cli_write_command`). An observation
therefore asks only once cephadm must already hold each login: it stores both before it configures any Prometheus daemon
(PrometheusService.generate_config) and Prometheus's before it configures any Grafana daemon (GrafanaService.get_dependencies,
src/pybind/mgr/cephadm/services/monitoring.py). The shape is not yet observed on a live IBM Storage Ceph host.
"""

from __future__ import annotations

import contextlib
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_observability as observability
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

LOGINS = {"alertmanager": {"password": "am-Secret.42", "user": "am-admin"}, "prometheus": {"password": "pr-Secret.17", "user": "pr-admin"}}
AUTHORITY = "-----BEGIN CERTIFICATE-----\nc3ludGhldGljIGF1dGhvcml0eQ==\n-----END CERTIFICATE-----\n"
VALUES = ("am-Secret.42", "pr-Secret.17", "am-admin", "pr-admin")
SERVED = {"certificateSHA256": gateway.LEAF_SHA256, "reachable": True}


@pytest.fixture(autouse=True)
def held(monkeypatch):
    """The bootstrap host's admin custody, held without its lock."""
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())


def monitored(services=("alertmanager", "grafana", "prometheus")):
    request = gateway.gateway_request()
    request["service"].update(endpoint="mgr.storage.example.test", monitoring=list(services))
    return request


def canonical(services):
    return ceph_native.canonical({service: LOGINS[service] for service in services})


class Credentials(monitoring.Monitor):
    """The monitoring monitor, also answering each service's get-credentials with its scripted answer."""

    def __init__(self, request, answers=None, rows=None):
        super().__init__(request, rows=rows)
        self.answers = {service: dict(login, certificate=AUTHORITY) for service, login in LOGINS.items()} if answers is None else answers
        self.asked = []

    def run(self, argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:])
        if len(command) == 3 and command[0] == "orch" and command[2] == "get-credentials":
            self.asked.append(command[1])
            return 0, json.dumps(self.answers[command[1]]).encode()
        return super().run(argv, data, timeout)


def reader(tmp_path, request, answers=None, rows=None):
    monitor = Credentials(request, answers, rows)
    return units.worker(monitor, units.markers_for(tmp_path, request)), monitor


class CredentialTransport(gateway.MaterialTransport):
    """The coordinator's view of the bootstrap host: the gateway's observations, and its logins as the worker answers them."""

    def __init__(self, request, answer=None, **options):
        super().__init__(request, dict(SERVED), **options)
        self.answer = {"changed": False, "private": canonical(observability.credential_services(request["service"])).decode()} if answer is None else answer

    def call(self, host, phase, private=None, *, timeout=None):
        if phase in ("monitoring_credentials", "monitoring_credentials_held"):
            self.private.append((phase, sorted(private or {})))
            return copy.deepcopy(self.answer)
        return super().call(host, phase, private, timeout=timeout)


def phases(transport):
    return [phase for phase, unused in transport.private]


def run(request, operation, transport, outputs=None):
    return ceph_core.execute(request, operation, "d" * 64, transport, outputs, now=lambda: 0, pause=lambda seconds: None)


def test_gateway_apply_writes_monitoring_output(tmp_path):
    request = monitored()
    transport = CredentialTransport(request, ready_after=2)
    path = str(tmp_path / "monitoring")
    result = run(request, "apply", transport, {"monitoring": path})
    with open(path, "rb") as handle:
        assert handle.read() == canonical(("alertmanager", "prometheus"))
    assert phases(transport)[-3:] == ["vip_holder", "grafana_route", "monitoring_credentials"] and phases(transport).count("monitoring_credentials") == 1
    assert result["evidence"]["ready"] is True and not any(value in json.dumps(result) for value in VALUES)
    transport = CredentialTransport(request)
    recorded = str(tmp_path / "recorded")
    run(request, "observe", transport, {"monitoring": recorded})
    assert phases(transport) == ["observe", "monitoring_credentials_held"]
    with open(recorded, "rb") as handle:
        assert handle.read() == canonical(("alertmanager", "prometheus"))
    transport = CredentialTransport(request, {"changed": False})
    run(request, "observe", transport, {"monitoring": str(tmp_path / "unheld")})
    assert phases(transport) == ["observe", "monitoring_credentials_held"] and not (tmp_path / "unheld").exists()
    with pytest.raises(ceph_native.Refusal, match="^(material|evidence)$"):
        run(request, "apply", CredentialTransport(request, {"changed": False}), {"monitoring": str(tmp_path / "unheld-apply")})
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        run(request, "apply", CredentialTransport(request))
    for answer in ({}, {"changed": False, "private": canonical(("prometheus",)).decode()}, {"changed": False, "private": "not JSON"},
                   {"changed": True, "private": canonical(("alertmanager", "prometheus")).decode()}, {"private": "{}"}):
        transport = CredentialTransport(request, answer)
        with pytest.raises(ceph_native.Refusal, match="^(material|evidence)$"):
            run(request, "apply", transport, {"monitoring": str(tmp_path / ("refused" + str(len(transport.private)) + str(id(answer))))})


def test_unrecorded_observe_reads_no_credentials(tmp_path):
    request = monitored()
    transport = CredentialTransport(request)
    result = run(request, "observe", transport)
    assert phases(transport) == ["observe"] and result["evidence"]["ready"] is True
    transport = CredentialTransport(request)
    transport.observed["ready"] = False
    run(request, "observe", transport, {"monitoring": str(tmp_path / "unready")})
    assert "monitoring_credentials" not in phases(transport) and not (tmp_path / "unready").exists()
    removed = {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": [], "endpoint": {"certificateSHA256": "", "reachable": False}}
    transport = CredentialTransport(request)
    transport.observed.update(removed)
    run(request, "destroy", transport)
    assert "monitoring_credentials" not in phases(transport)


def test_credentials_read_only_for_declared_services(tmp_path):
    for services, asked in ((("alertmanager", "grafana", "prometheus"), ["alertmanager", "prometheus"]), (("alertmanager",), ["alertmanager"]),
                            (("grafana", "prometheus"), ["prometheus"])):
        request = monitored(services)
        native, monitor = reader(tmp_path / "-".join(services), request)
        result = native.monitoring_credentials()
        assert result == {"changed": False, "private": canonical(asked).decode()}
        assert monitor.asked == asked and monitor.mutations == []
    for request in (monitored(("grafana",)), gateway.gateway_request(), units.unit_request()):
        native, monitor = reader(tmp_path / ("none-" + str(len(request["service"].get("monitoring", [])))) / request["service"]["type"], request)
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            native.monitoring_credentials()
        assert monitor.asked == []
        transport = CredentialTransport(request, {"private": "{}"})
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            run(request, "observe", transport, {"monitoring": str(tmp_path / "declared")})
        assert "monitoring_credentials" not in phases(transport)
    assert "monitoring_credentials_held" in ceph_native.read_only_phases() and "monitoring_credentials" not in ceph_native.read_only_phases()


def daemon(kind):
    return {"daemon_id": "node-1", "daemon_type": kind, "hostname": "node-1", "service_name": kind, "status": 1}


def test_observation_reads_only_logins_cephadm_already_holds(tmp_path):
    cases = (
        (("alertmanager", "grafana", "prometheus"), ("prometheus",), ["alertmanager", "prometheus"]),
        (("alertmanager", "grafana", "prometheus"), ("grafana", "alertmanager"), None),
        (("alertmanager",), ("alertmanager",), None),
        (("alertmanager",), ("prometheus",), ["alertmanager"]),
        (("grafana", "prometheus"), ("grafana",), ["prometheus"]),
        (("prometheus",), (), None),
        (("prometheus",), ("mgmt-gateway", "node-exporter"), None),
    )
    for index, (services, deployed, asked) in enumerate(cases):
        native, monitor = reader(tmp_path / str(index), monitored(services), rows=[daemon(kind) for kind in deployed])
        result = native.monitoring_credentials_held()
        if asked is None:
            assert result == {"changed": False} and monitor.asked == []
        else:
            assert result == {"changed": False, "private": canonical(asked).decode()} and monitor.asked == asked
        assert monitor.mutations == []


def test_security_disabled_answer_refuses_material(tmp_path):
    login = dict(LOGINS["alertmanager"], certificate=AUTHORITY)
    unsupported = "monitoring-login-unsupported"
    cases = {"security disabled": ({}, "material"), "no certificate": (dict(LOGINS["alertmanager"]), None),
             "empty password": (dict(login, password=""), "material"), "space": (dict(login, password="am Secret"), unsupported),
             "escaped": (dict(login, user="am<admin"), unsupported), "long": (dict(login, password="p" * 129), unsupported),
             "non-ASCII": (dict(login, password="am-Sécret"), unsupported), "ampersand": (dict(login, password="am&Secret"), unsupported),
             "number": (dict(login, password=42), "material"), "no user": ({"password": "am-Secret.42", "certificate": AUTHORITY}, "native-output"),
             "extra": (dict(login, role="admin"), "native-output"), "list": ([login], "native-output")}
    for name, (answer, reason) in cases.items():
        answers = {"alertmanager": answer, "prometheus": dict(LOGINS["prometheus"], certificate=AUTHORITY)}
        native, monitor = reader(tmp_path / name.replace(" ", "-"), monitored(), answers)
        if reason is None:
            assert native.monitoring_credentials()["private"] == ceph_native.canonical(dict(LOGINS)).decode()
            continue
        with pytest.raises(ceph_native.Refusal, match="^" + reason + "$") as raised:
            native.monitoring_credentials()
        detail = str(raised.value) + repr(raised.value) + json.dumps(getattr(raised.value, "native_error", None))
        assert not any(value in detail for value in VALUES + ("am Secret", "am<admin", "am-Sécret", "am&Secret", "p" * 129))
        if reason == unsupported:
            assert raised.value.native_error["cause"] == "alertmanager " + ("user" if name == "escaped" else "password")
        assert monitor.asked == ["alertmanager"]


def test_logins_material_confines_the_written_output():
    services = ["alertmanager", "prometheus"]
    assert observability.logins_material(canonical(services), services) == canonical(services)
    for value in (canonical(["prometheus"]), canonical(["alertmanager"]) + b" ", json.dumps(LOGINS, indent=1).encode(),
                  ceph_native.canonical(dict(LOGINS, grafana=LOGINS["prometheus"])),
                  ceph_native.canonical({"alertmanager": {"user": "a"}, "prometheus": LOGINS["prometheus"]}),
                  b'{"alertmanager":{"password":"x","password":"y","user":"u"},"prometheus":{"password":"p","user":"u"}}', b"", b"[]"):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            observability.logins_material(value, services)
