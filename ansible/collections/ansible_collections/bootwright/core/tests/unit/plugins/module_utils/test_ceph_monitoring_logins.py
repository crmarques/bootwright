"""An explicit administrator login update of a management gateway's Prometheus or Alertmanager login, or of the monitoring unit's
Grafana password, and the gateway's generated first login (B1630).

The scripted monitor answers the upstream v20.2.1 shapes the worker reads, beside those test_ceph_monitoring_credentials documents:
- `orch <service> set-credentials -i -` reads `{"username", "password"}` from standard input and only stores it
  (src/pybind/mgr/orchestrator/module.py, _get_credentials; src/pybind/mgr/cephadm/module.py, set_prometheus_access_info);
- cephadm's serve loop later reconfigures each daemon whose dependencies hash the login, which `orch ps` shows as a later
  `last_configured` (src/pybind/mgr/cephadm/services/monitoring.py, get_dependencies; src/pybind/mgr/orchestrator/_interface.py:1499-1502);
  the fixture's daemons serve the new login only from that reconfiguration on;
- Prometheus and Alertmanager behind a gateway serve under /prometheus/ and /alertmanager and answer a wrong basic-auth login with 401
  (src/pybind/mgr/cephadm/templates/services/prometheus/web.yml.j2, src/cephadm/cephadmlib/daemons/monitoring.py);
- Grafana behind a gateway serves under /grafana, `GET /api/user` answers 200 with the signed-in user and `PUT /api/user/password` takes
  `{"oldPassword", "newPassword", "confirmNew"}`; each daemon keeps its own database (grafana.ini.j2 `serve_from_sub_path`);
- `dashboard get-grafana-api-password` prints the dashboard's GRAFANA_API_PASSWORD option and `set-grafana-api-password -i -` reads it
  from standard input (src/pybind/mgr/dashboard/settings.py).
None of these shapes is yet observed on a live IBM Storage Ceph host.
"""

from __future__ import annotations

import base64
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_monitoring_logins_native as logins, ceph_native, ceph_native_observability as observability, ceph_native_units, ceph_transport,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

normalization = monitoring.normalization
ACTIVE = {"alertmanager": {"password": "am-Active.Secret42", "user": "admin"}, "prometheus": {"password": "pr-Active.Secret17", "user": "admin"}}
CANDIDATE = {"alertmanager": {"password": "am-Candidate.Value7", "user": "admin"}, "prometheus": {"password": "pr-Candidate.Value9", "user": "admin"}}
FOREIGN = {"password": "foreign-Login.Value3", "user": "admin"}
V1 = monitoring.GRAFANA_PASSWORD
V2 = "grafana-candidate-sentinel-W8q"
OTHER = "grafana-foreign-sentinel-N5d"
PAIR = {"cert": gateway.pem("CERTIFICATE", b"cephadm-signed client"), "key": gateway.pem("PRIVATE KEY", b"client key only this worker holds")}
AUTHORITY = "-----BEGIN CERTIFICATE-----\nc3ludGhldGljIGF1dGhvcml0eQ==\n-----END CERTIFICATE-----\n"
BEFORE, AFTER = "2026-11-19T10:00:00.000000Z", "2026-11-19T10:05:00.000000Z"
SECRETS = tuple(login["password"] for login in list(ACTIVE.values()) + list(CANDIDATE.values())) + (V1, V2, OTHER, FOREIGN["password"])
PORTS = {"alertmanager": [9093, 9094], "grafana": [3000], "prometheus": [9095]}


def row(kind, host, configured=BEFORE, status=1):
    value = {"daemon_type": kind, "daemon_id": host, "hostname": host, "service_name": kind, "status": status, "ports": list(PORTS.get(kind, []))}
    if configured is not None:
        value["last_configured"] = configured
    return value


class Logins(monitoring.Monitor):
    """The monitoring monitor, also keeping cephadm's stored logins, the dashboard's Grafana API password and every daemon's served
    login; a reconfiguration serves each stored login and moves `last_configured`."""

    def __init__(self, request, rows=None):
        super().__init__(request, rows=rows)
        self.stored = copy.deepcopy(ACTIVE)
        self.served = {}
        self.grafana_api = V1
        self.modules = ["dashboard", "prometheus"]
        self.asked, self.argv = [], []

    def reconfigure(self):
        for item in self.running:
            if item["daemon_type"] in self.stored:
                item["last_configured"] = AFTER
                self.served[item["hostname"], item["daemon_type"]] = dict(self.stored[item["daemon_type"]])

    def run(self, argv, data=b"", timeout=60):
        self.argv.append(list(argv))
        if "--format" not in argv:
            command = tuple(argv[argv.index("--keyring") + 2:])
            if command == ("dashboard", "get-grafana-api-password"):
                return 0, (self.grafana_api + "\n").encode()
            assert command == ("orch", "certmgr", "cert", "get", "cephadm_root_ca_cert")
            return 0, AUTHORITY.encode()
        command = tuple(argv[argv.index("--format") + 2:])
        if len(command) == 3 and command[0] == "orch" and command[2] == "get-credentials":
            self.asked.append(command[1])
            return 0, json.dumps(dict(self.stored[command[1]], certificate=AUTHORITY)).encode()
        if command[2:] == ("set-credentials", "-i", "-"):
            self.mutations.append(command)
            self.stdin.append(data)
            value = json.loads(data)
            assert set(value) == {"username", "password"}
            self.stored[command[1]] = {"password": value["password"], "user": value["username"]}
            return 0, b'"credentials updated correctly"'
        if command == ("orch", "certmgr", "generate-certificates", "prometheus"):
            return 0, json.dumps(PAIR).encode()
        if command == ("mgr", "module", "ls"):
            return 0, json.dumps({"always_on_modules": [], "enabled_modules": self.modules}).encode()
        if command == ("dashboard", "set-grafana-api-password", "-i", "-"):
            self.mutations.append(command)
            self.stdin.append(data)
            self.grafana_api = data.decode()
            return 0, b""
        return super().run(argv, data, timeout)


class Daemons:
    """Each daemon's HTTP endpoint: a gateway login is accepted only as its daemon serves it, Grafana's only as its database holds it."""

    def __init__(self, monitor, grafana=None, stale=False):
        self.monitor = monitor
        self.grafana = dict(grafana or {})
        self.stale = stale
        self.requests, self.bodies, self.headers = [], [], []
        # Grafana 12.3.1 answers a login its failed-login protection locked with the 401 and message of a wrong password.
        self.locked = False
        self.unreachable = 0
        self.refused = {}

    def __call__(self, target, method, path, authorization, body=b""):
        address, number, name, context = target
        self.requests.append((address, number, name, method, path))
        self.headers.append(authorization)
        self.bodies.append(body)
        assert context is not None and authorization.startswith(b"Basic ")
        if self.unreachable:
            self.unreachable -= 1
            return None
        user, unused, password = base64.b64decode(authorization[6:]).decode().partition(":")
        host = "node-" + address.rsplit(".", 1)[1]
        if path.startswith("/grafana/api/user"):
            accepted = user == "admin" and self.grafana.get(host) == password and not self.locked
            if not accepted:
                self.refused[host] = self.refused.get(host, 0) + 1
                return 401, b'{"message":"Invalid username or password","messageId":"password-auth.failed","statusCode":401}'
            if method == "PUT":
                change = json.loads(body)
                assert change["oldPassword"] == password and change["newPassword"] == change["confirmNew"]
                self.grafana[host] = change["newPassword"]
                return 200, b'{"message":"User password changed"}'
            return 200, b'{"id":1,"login":"admin"}'
        kind = "prometheus" if path == logins.ROUTES["prometheus"] else "alertmanager"
        assert path == logins.ROUTES[kind]
        served = [self.monitor.served.get((host, kind), ACTIVE[kind])] + ([ACTIVE[kind]] if self.stale else [])
        return (200, b"{}") if {"password": password, "user": user} in served else (401, b"")


@pytest.fixture(autouse=True)
def trust(monkeypatch):
    """A trust context records the CA and client pair it was built from; no TLS runs."""
    built = []
    monkeypatch.setattr(logins, "trust", lambda ca, pair=None: built.append((ca, dict(pair) if pair else None)) or "verified-context")
    return built


def monitored(services=("alertmanager", "grafana", "prometheus")):
    request = gateway.gateway_request()
    request["service"].update(endpoint="mgr.storage.example.test", monitoring=list(services))
    return request


def worker(monitor, markers, phase="login_update", material=None):
    native = logins.LoginWorker(monitor.request, "node-1", run=monitor.run, markers=markers)
    elapsed = [0.0]
    native.clock = lambda: elapsed[0]
    native.pause = lambda seconds: elapsed.__setitem__(0, elapsed[0] + seconds)
    native.bound = ceph_native_units.material(monitor.request, phase, dict(material or {}))
    return native


def gateway_rows(services=("alertmanager", "prometheus"), configured=BEFORE):
    rows = [gateway.row("mgmt-gateway", "mgmt-gateway", host, gateway.NGINX) for host in gateway.TWO]
    return rows + [row(kind, host, configured) for kind in services for host in gateway.TWO] + [row("grafana", "node-1")]


def applied_gateway(tmp_path, request=None):
    """A gateway unit this cluster applied, holding the active logins cephadm kept, its daemons not yet reconfigured."""
    request = monitored() if request is None else request
    markers = units.markers_for(tmp_path, request)
    monitor = Logins(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    monitor.stored, monitor.mutations, monitor.stdin, monitor.argv = copy.deepcopy(ACTIVE), [], [], []
    monitor.running = gateway_rows([service for service in ("alertmanager", "prometheus") if service in request["service"]["monitoring"]])
    return markers, monitor


def update(service, guard):
    return {"active": dict(ACTIVE[service]), "guard": guard, "password": CANDIDATE[service]["password"], "user": CANDIDATE[service]["user"]}


def prepared(monitor, markers, service):
    result = worker(monitor, markers, "login_prepare").login_prepare(service, "2026-01-01T00:00:00.000000Z")
    assert set(result) == {"private"} and not any(value in result["private"] for value in SECRETS)
    return json.loads(result["private"])["guard"]


def exposed(monitor, markers, *extra):
    return [json.dumps(monitor.argv), monitoring.recorded(markers).decode(), *(json.dumps(item) for item in extra)]


def test_gateway_login_set_credentials_takes_only_stdin(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "prometheus")
    assert guard == {"configured": {"prometheus.node-1": BEFORE, "prometheus.node-2": BEFORE}, "fsid": units.FSID,
                     "incarnation": "2026-01-01T00:00:00.000000Z", "login": "prometheus"}
    assert monitor.mutations == [] and monitor.asked == []
    native = worker(monitor, markers)
    native.pause = lambda seconds: monitor.reconfigure()
    daemons = Daemons(monitor)
    monkeypatch.setattr(logins, "exchange", daemons)
    assert native.login_update("prometheus", guard["incarnation"], update("prometheus", guard)) == {"proved": True}
    assert monitor.mutations == [("orch", "prometheus", "set-credentials", "-i", "-")]
    assert json.loads(monitor.stdin[0]) == {"password": CANDIDATE["prometheus"]["password"], "username": "admin"}
    assert monitor.stored["prometheus"] == CANDIDATE["prometheus"] and monitor.stored["alertmanager"] == ACTIVE["alertmanager"]
    assert not any(value in item for value in SECRETS for item in exposed(monitor, markers, daemons.requests))


def test_gateway_login_stored_candidate_is_skipped(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "alertmanager")
    monitor.stored["alertmanager"] = dict(CANDIDATE["alertmanager"])
    monitor.reconfigure()
    monkeypatch.setattr(logins, "exchange", Daemons(monitor))
    assert worker(monitor, markers).login_update("alertmanager", guard["incarnation"], update("alertmanager", guard)) == {"proved": True}
    assert monitor.mutations == [] and monitor.asked == ["alertmanager"]


def test_gateway_login_drift_refuses_without_a_value(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "prometheus")
    monitor.stored["prometheus"] = dict(FOREIGN)
    monkeypatch.setattr(logins, "exchange", Daemons(monitor))
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-drift$") as raised:
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    assert raised.value.native_error["cause"] == "prometheus holds neither the active nor the candidate login"
    assert monitor.mutations == []
    assert not any(value in item for value in SECRETS for item in exposed(monitor, markers, raised.value.native_error, str(raised.value)))


def test_gateway_login_proof_waits_for_reconfig_and_401_on_old_login(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "prometheus")
    daemons = Daemons(monitor)
    monkeypatch.setattr(logins, "exchange", daemons)
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$") as raised:
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    assert raised.value.native_error["cause"] == "prometheus daemon prometheus.node-1 does not yet serve the candidate login"
    assert daemons.requests == [] and monitor.stored["prometheus"] == CANDIDATE["prometheus"]
    # A reconfiguration that left the old login served proves nothing: the daemon must refuse it with 401.
    for item in monitor.running:
        item["last_configured"] = AFTER
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$"):
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    assert {request[4] for request in daemons.requests} == {logins.ROUTES["prometheus"]}
    # A daemon that accepts the candidate yet still the old login is not proved either.
    monitor.reconfigure()
    monkeypatch.setattr(logins, "exchange", Daemons(monitor, stale=True))
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$"):
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    monkeypatch.setattr(logins, "exchange", daemons)
    daemons.requests.clear()
    assert worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard)) == {"proved": True}
    addresses = ("192.0.2.1", "192.0.2.1", "192.0.2.2", "192.0.2.2")
    assert daemons.requests == [(address, 9095, address, "GET", logins.ROUTES["prometheus"]) for address in addresses]
    accepted = [base64.b64decode(header[6:]).decode() for header in daemons.headers[-4:]]
    assert accepted == ["admin:" + CANDIDATE["prometheus"]["password"], "admin:" + ACTIVE["prometheus"]["password"]] * 2
    assert len(monitor.mutations) == 1


def test_gateway_login_proof_with_oauth2_proxy_needs_no_basic_auth_refusal(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "alertmanager")
    monitor.running.append(gateway.row("oauth2-proxy", "oauth2-proxy", "node-1", "proxy"))
    monitor.reconfigure()
    seen = []
    monkeypatch.setattr(logins, "exchange", lambda target, method, path, authorization, body=b"": seen.append(authorization) or (200, b"{}"))
    assert worker(monitor, markers).login_update("alertmanager", guard["incarnation"], update("alertmanager", guard)) == {"proved": True}
    assert len(seen) == 2


def test_gateway_login_client_identity_stays_in_memory(tmp_path, monkeypatch, trust):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "prometheus")
    monitor.stored["prometheus"] = dict(CANDIDATE["prometheus"])
    monitor.reconfigure()
    monkeypatch.setattr(logins, "exchange", Daemons(monitor))
    worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    assert trust == [(AUTHORITY, PAIR)]
    assert not any(line in monitoring.recorded(markers).decode() for line in PAIR["key"].splitlines() if "-----" not in line)


def test_unlisted_services_are_never_asked(tmp_path, monkeypatch):
    request = monitored(("grafana", "prometheus"))
    markers, monitor = applied_gateway(tmp_path, request)
    for login in ("alertmanager", "grafana"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            logins.LoginWorker(request, "node-1", run=monitor.run, markers=markers).unit_login(login)
    monkeypatch.setattr(logins.LoginWorker, "verify_tool", lambda self: pytest.fail("verified before the login was admitted"))
    for login in ("alertmanager", "dashboard", "grafana"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            logins.dispatch({"request": request, "host": "node-1", "phase": "login_prepare", "private": {"incarnation": "x", "login": login}})
    guard = prepared(monitor, markers, "prometheus")
    assert monitor.asked == [] and set(guard["configured"]) == {"prometheus.node-1", "prometheus.node-2"}
    # Before a holder of the login is deployed cephadm may hold none, and asking would store admin/admin: nothing is asked.
    monitor.running = [item for item in monitor.running if item["daemon_type"] not in ("prometheus", "grafana")]
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$"):
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    monitor.running = [item for item in gateway_rows(("prometheus",)) if item["daemon_type"] != "mgmt-gateway"]
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$"):
        prepared(monitor, markers, "prometheus")
    assert monitor.asked == [] and monitor.mutations == []


def test_login_phases_lend_only_the_grafana_password():
    gateway_request, grafana_request = monitored(), monitoring.monitoring_request()
    for phase in logins.PHASES:
        assert ceph_native_units.material_names(gateway_request, phase) == frozenset()
        assert ceph_native_units.material_names(grafana_request, phase) == frozenset(("grafanaAdminPassword",))
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            ceph_native_units.material(grafana_request, phase, dict(monitoring.MATERIAL))
    assert "ceph_monitoring_logins_native" in ceph_transport.Transport.native_sources()


def test_dispatch_maps_the_private_envelope(tmp_path, monkeypatch):
    request = monitoring.monitoring_request()
    calls = []
    monkeypatch.setattr(logins.LoginWorker, "verify_tool", lambda self: calls.append("tool"))
    monkeypatch.setattr(logins.LoginWorker, "monmap_incarnation", lambda self: ("2026-01-01T00:00:00.000000Z", {}))
    monkeypatch.setattr(logins.LoginWorker, "login_prepare", lambda self, login, incarnation: calls.append((login, incarnation, self.bound)) or {})
    monkeypatch.setattr(logins.LoginWorker, "login_update", lambda self, login, incarnation, value: calls.append((login, value, self.bound)) or {})
    private = {"incarnation": "2026-01-01T00:00:00.000000Z", "login": "grafana", "grafanaAdminPassword": V1}
    ceph_native.dispatch({"request": request, "host": "node-1", "phase": "login_prepare", "private": dict(private)})
    ceph_native.dispatch({"request": request, "host": "node-1", "phase": "login_update", "private": dict(private, update={"guard": {}})})
    assert calls == ["tool", ("grafana", "2026-01-01T00:00:00.000000Z", {"grafanaAdminPassword": V1}),
                     "tool", ("grafana", {"guard": {}}, {"grafanaAdminPassword": V1})]
    for bad in (dict(private, incarnation="2025-01-01T00:00:00.000000Z"), {k: v for k, v in private.items() if k != "grafanaAdminPassword"}):
        with pytest.raises(ceph_native.Refusal, match="^(ownership|material)$"):
            ceph_native.dispatch({"request": request, "host": "node-1", "phase": "login_prepare", "private": bad})
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch({"request": request, "host": "node-1", "phase": "login_update", "private": dict(private)})


def applied_grafana(tmp_path, count=1):
    """A monitoring unit this cluster applied with V1 bound, its Grafana behind a running gateway."""
    request = monitoring.monitoring_request()
    request["service"]["documents"][2]["placement"]["count"] = count
    markers = units.markers_for(tmp_path, request)
    monitor = Logins(request)
    assert monitoring.applying(monitor, markers).unit() == {"changed": True}
    monitor.mutations, monitor.stdin, monitor.argv = [], [], []
    monitor.running = [gateway.row("mgmt-gateway", "mgmt-gateway", "node-1", gateway.NGINX), row("grafana", "node-1")]
    return markers, monitor


def grafana_update(monitor, markers, daemons, monkeypatch):
    monkeypatch.setattr(logins, "exchange", daemons)
    native = worker(monitor, markers, material={"grafanaAdminPassword": V1})
    guard = {"fsid": units.FSID, "incarnation": "2026-01-01T00:00:00.000000Z", "login": "grafana"}
    return native.login_update("grafana", guard["incarnation"], {"guard": guard, "password": V2, "user": "admin"})


def test_grafana_update_changes_the_daemon_the_spec_and_the_dashboard(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    guard = worker(monitor, markers, "login_prepare", {"grafanaAdminPassword": V1}).login_prepare("grafana", "2026-01-01T00:00:00.000000Z")
    assert json.loads(guard["private"]) == {"guard": {"fsid": units.FSID, "incarnation": "2026-01-01T00:00:00.000000Z", "login": "grafana"}}
    daemons = Daemons(monitor, {"node-1": V1})
    assert grafana_update(monitor, markers, daemons, monkeypatch) == {"proved": True}
    assert daemons.grafana == {"node-1": V2} and monitor.specs["grafana"]["spec"]["initial_admin_password"] == V2 and monitor.grafana_api == V2
    # Grafana 12.3.1 counts every failed login toward its lockout and a success does not reset it: a run costs a daemon one.
    assert daemons.refused == {"node-1": 1}
    assert monitor.mutations == [("orch", "apply", "-i", "-"), ("dashboard", "set-grafana-api-password", "-i", "-")]
    applied = json.loads(monitor.stdin[0])
    assert applied["service_type"] == "grafana" and applied["spec"]["initial_admin_password"] == V2 and monitor.stdin[1] == V2.encode()
    assert {request[4] for request in daemons.requests} == {"/grafana/api/user", "/grafana/api/user/password"}
    assert daemons.requests[0][:3] == ("192.0.2.1", 3000, "192.0.2.1")
    assert json.loads(daemons.bodies[daemons.requests.index(("192.0.2.1", 3000, "192.0.2.1", "PUT", "/grafana/api/user/password"))]) == {
        "confirmNew": V2, "newPassword": V2, "oldPassword": V1}
    assert markers.read("service-grafana-complete.json")["service"]["spec"]["initial_admin_password"] == "bootwright-bound:/spec/initial_admin_password"
    assert not any(value in item for value in (V1, V2) for item in exposed(monitor, markers, daemons.requests))


def test_grafana_update_recovers_per_daemon(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path, count=2)
    # Every placed daemon must run: a stopped one would keep the bound password in its own database.
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$") as raised:
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1}), monkeypatch)
    assert raised.value.native_error["cause"] == "grafana runs 1 of 2 daemons" and monitor.mutations == []
    monitor.running.append(row("grafana", "node-2"))
    # A lost reply left node-1 changed, the spec and the dashboard not: node-1 is skipped and node-2 changed.
    daemons = Daemons(monitor, {"node-1": V2, "node-2": V1})
    assert grafana_update(monitor, markers, daemons, monkeypatch) == {"proved": True}
    assert [request[:4] for request in daemons.requests if request[3] == "PUT"] == [("192.0.2.2", 3000, "192.0.2.2", "PUT")]
    assert daemons.refused == {"node-2": 1}
    assert daemons.grafana == {"node-1": V2, "node-2": V2}
    assert monitor.mutations == [("orch", "apply", "-i", "-"), ("dashboard", "set-grafana-api-password", "-i", "-")]
    # A repeat once everything holds the candidate writes nothing.
    monitor.mutations = []
    assert grafana_update(monitor, markers, Daemons(monitor, {"node-1": V2, "node-2": V2}), monkeypatch) == {"proved": True}
    assert monitor.mutations == []


def test_grafana_drift_refuses_without_a_value(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    monitor.specs["grafana"]["spec"]["initial_admin_password"] = OTHER
    leaked = []
    # A changed leaf while a daemon or the dashboard already holds the candidate: an earlier attempt placed it, so it stays retained.
    for daemon, dashboard in ((V2, V1), (V1, V2)):
        monitor.grafana_api = dashboard
        with pytest.raises(ceph_native.NativeFailure, match="^grafana-login-drift$") as raised:
            grafana_update(monitor, markers, Daemons(monitor, {"node-1": daemon}), monkeypatch)
        assert raised.value.native_error["cause"] == "grafana stored spec holds another password" and monitor.mutations == []
        leaked += exposed(monitor, markers, raised.value.native_error, str(raised.value))
    assert not any(value in item for value in (V1, V2, OTHER) for item in leaked)


def test_grafana_drift_before_any_effect_is_unapplied(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path, count=2)
    monitor.running.append(row("grafana", "node-2"))
    monitor.specs["grafana"]["spec"]["initial_admin_password"] = OTHER
    daemons = Daemons(monitor, {"node-1": V1, "node-2": V1})
    with pytest.raises(ceph_native.NativeFailure, match="^grafana-login-drift-unapplied$") as raised:
        grafana_update(monitor, markers, daemons, monkeypatch)
    assert raised.value.native_error["cause"] == "grafana stored spec holds another password and nothing holds the candidate"
    assert monitor.mutations == [] and daemons.grafana == {"node-1": V1, "node-2": V1}
    assert [base64.b64decode(header[6:]).decode() for header in daemons.headers] == ["admin:" + V1] * 2
    assert not any(value in item for value in (V1, V2, OTHER) for item in exposed(monitor, markers, raised.value.native_error))
    # The dashboard holds the drifted leaf a configuration generation sent it, and a failed read of it echoes that value: every
    # failure the drift path builds redacts it, though the path then reports the drift as found.
    built, initial, original = [], ceph_native.NativeFailure.__init__, monitor.run

    def recording(self, *args, **options):
        initial(self, *args, **options)
        built.append(json.dumps(self.native_error))

    def echoing(argv, data=b"", timeout=60):
        if argv[-1] == "get-grafana-api-password":
            return 1, ("dashboard echoed " + OTHER).encode()
        return original(argv, data, timeout)

    monkeypatch.setattr(ceph_native.NativeFailure, "__init__", recording)
    monitor.run = echoing
    with pytest.raises(ceph_native.NativeFailure, match="^grafana-login-drift$") as raised:
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1, "node-2": V1}), monkeypatch)
    assert len(built) >= 2 and not any(OTHER in item for item in built + exposed(monitor, markers, raised.value.native_error))
    monitor.run = original
    # A daemon that is not running may hold the candidate, so nothing is proved unapplied.
    monitor.running[-1]["status"] = 0
    with pytest.raises(ceph_native.NativeFailure, match="^grafana-login-drift$"):
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1, "node-2": V1}), monkeypatch)


def test_grafana_refusing_both_passwords_is_a_lockout_or_drift_never_retried(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    for daemons in (Daemons(monitor, {"node-1": OTHER}), Daemons(monitor, {"node-1": V1})):
        daemons.locked = daemons.grafana["node-1"] == V1
        with pytest.raises(ceph_native.NativeFailure, match="^grafana-login-refused$") as raised:
            grafana_update(monitor, markers, daemons, monkeypatch)
        assert raised.value.native_error["cause"] == "grafana daemon grafana.node-1 refuses both passwords"
        assert monitor.mutations == [] and len(daemons.requests) == 2
        assert not any(value in item for value in (V1, V2, OTHER) for item in exposed(monitor, markers, raised.value.native_error))
    daemons.locked = False
    assert grafana_update(monitor, markers, daemons, monkeypatch) == {"proved": True} and daemons.grafana == {"node-1": V2}


def test_grafana_update_waits_out_a_reconfiguration_and_reads_the_dashboard_again(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    daemon = monitor.running[-1]
    daemon.update(status=2, pending_daemon_config=True)
    daemons = Daemons(monitor, {"node-1": V1})
    native = worker(monitor, markers, material={"grafanaAdminPassword": V1})
    original, listings, steps, clock, landing = monitor.run, [], [], [0.0], {}
    write = ("dashboard", "set-grafana-api-password", "-i", "-")

    def land():
        """A reconfiguration whose configuration was generated before the splice: it sent the bound password to the dashboard, and
        cephadm records its later configuration time only once its deploy lands, ten seconds on."""
        landing["landed"] = True
        monitor.grafana_api = V1
        daemon["last_configured"] = AFTER

    def serve(argv, data=b"", timeout=60):
        """cephadm's serve loop as `orch ps` shows it: the reconfiguration a Prometheus update scheduled settles on the next listing;
        the first listing after the dashboard write finds the daemon restarting once more with the bound password back in the
        dashboard; a configuration generated before the splice lands ten seconds after the update's last dashboard write."""
        if "--format" in argv and tuple(argv[argv.index("--format") + 2:]) == ("orch", "ps"):
            listings.append(dict(daemon))
            if len(listings) > 1:
                daemon.update(status=1, pending_daemon_config=False)
            if write in monitor.mutations and not daemons.unreachable and len(steps) == 1:
                monitor.grafana_api, daemons.unreachable = V1, 1
            if "at" in landing and not landing.get("landed") and clock[0] >= landing["at"]:
                land()
        answer = original(argv, data, timeout)
        if tuple(argv[-4:]) == write and monitor.mutations.count(write) == 2:
            landing["at"] = clock[0] + 10
        return answer

    monitor.run = native.run = serve
    native.clock = lambda: clock[0]
    native.pause = lambda seconds: steps.append(seconds) or clock.__setitem__(0, clock[0] + seconds)
    monkeypatch.setattr(logins, "exchange", daemons)
    guard = {"fsid": units.FSID, "incarnation": "2026-01-01T00:00:00.000000Z", "login": "grafana"}
    assert native.login_update("grafana", guard["incarnation"], {"guard": guard, "password": V2, "user": "admin"}) == {"proved": True}
    if not landing.get("landed"):
        land()
    assert listings[0]["status"] == 2 and daemons.grafana == {"node-1": V2} and monitor.grafana_api == V2
    assert steps == [logins.PROOF_INTERVAL, logins.PROOF_INTERVAL, logins.SERVE_INTERVAL, logins.SERVE_INTERVAL]
    assert [mutation[0] for mutation in monitor.mutations] == ["orch", "dashboard", "dashboard", "dashboard"]
    # A daemon still reconfiguring when the bound runs out is not proved, and nothing was changed.
    markers, monitor = applied_grafana(tmp_path / "stuck")
    monitor.running[-1]["pending_daemon_config"] = True
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$") as raised:
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1}), monkeypatch)
    assert raised.value.native_error["cause"] == "grafana daemon grafana.node-1 is still being reconfigured" and monitor.mutations == []


def test_gateway_login_that_moves_after_its_set_is_unproved_not_drift(tmp_path, monkeypatch):
    markers, monitor = applied_gateway(tmp_path)
    guard = prepared(monitor, markers, "prometheus")
    original = monitor.run

    def moved(argv, data=b"", timeout=60):
        answer = original(argv, data, timeout)
        if b"username" in data:
            monitor.stored["prometheus"] = dict(FOREIGN)
        return answer

    monitor.run = moved
    monkeypatch.setattr(logins, "exchange", Daemons(monitor))
    with pytest.raises(ceph_native.NativeFailure, match="^monitoring-login-unproved$") as raised:
        worker(monitor, markers).login_update("prometheus", guard["incarnation"], update("prometheus", guard))
    assert raised.value.native_error["cause"] == "prometheus did not keep the candidate login"


def test_grafana_spec_splice_reads_back_the_candidate(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    original = monitor.run

    def foreign(argv, data=b"", timeout=60):
        answer = original(argv, data, timeout)
        if b"initial_admin_password" in data:
            monitor.specs["grafana"]["spec"]["initial_admin_password"] = OTHER
        return answer

    monitor.run = foreign
    with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$") as raised:
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1}), monkeypatch)
    assert not any(value in json.dumps(raised.value.native_error) for value in (V1, V2, OTHER))
    assert ("dashboard", "set-grafana-api-password", "-i", "-") not in monitor.mutations
    # A readback whose other members moved is not the retained document either.
    markers, monitor = applied_grafana(tmp_path / "moved")
    original = monitor.run

    def moved(argv, data=b"", timeout=60):
        answer = original(argv, data, timeout)
        if b"initial_admin_password" in data:
            monitor.specs["grafana"]["spec"]["port"] = 3001
        return answer

    monitor.run = moved
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1}), monkeypatch)
    assert ("dashboard", "set-grafana-api-password", "-i", "-") not in monitor.mutations


def test_grafana_dashboard_password_only_with_the_dashboard_module(tmp_path, monkeypatch):
    markers, monitor = applied_grafana(tmp_path)
    monitor.modules = ["prometheus"]
    assert grafana_update(monitor, markers, Daemons(monitor, {"node-1": V1}), monkeypatch) == {"proved": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")] and monitor.grafana_api == V1
    assert not any("get-grafana-api-password" in argv for argv in monitor.argv)


def test_gateway_claim_sets_generated_login_before_the_stream(tmp_path):
    request = monitored()
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "alertmanager", "set-credentials", "-i", "-"), ("orch", "prometheus", "set-credentials", "-i", "-"),
                                 ("orch", "apply", "-i", "-")]
    sent = [entry for entry in monitor.log if "set-credentials" in entry]
    assert len(sent) == 2 and all(entry[-2:] == ["-i", "-"] for entry in sent)


def test_gateway_claim_sends_each_login_only_on_stdin(tmp_path):
    request = monitored()
    markers = units.markers_for(tmp_path, request)
    monitor = Logins(request)
    monitor.stored = {}
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "alertmanager", "set-credentials", "-i", "-"), ("orch", "prometheus", "set-credentials", "-i", "-"),
                                 ("orch", "apply", "-i", "-")]
    generated = [json.loads(data) for data in monitor.stdin[:2]]
    assert all(set(value) == {"password", "username"} and value["username"] == "admin" and ceph_native.dashboard_policy(value["password"])
               for value in generated)
    assert generated[0]["password"] != generated[1]["password"]
    assert monitor.stored == {service: {"password": value["password"], "user": "admin"} for service, value in zip(("alertmanager", "prometheus"), generated)}
    passwords = [value["password"] for value in generated]
    assert not any(password in item for password in passwords for item in (json.dumps(monitor.argv), monitoring.recorded(markers).decode()))


def test_gateway_claim_sets_nothing_after_an_attempt_record(tmp_path):
    request = monitored()
    markers = units.markers_for(tmp_path, request)
    monitor = Logins(request)
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    monitor.mutations = []
    assert gateway.applying(monitor, markers).unit() == {"changed": False} and monitor.mutations == []
    # An attempt whose stream a lost reply hid still holds the record: the repeat streams again but generates no second login.
    request = monitored()
    markers = units.markers_for(tmp_path / "lost", request)
    monitor = Logins(request)
    monitor.before_store = ceph_native.Refusal("native-command")
    with pytest.raises(ceph_native.Refusal):
        gateway.applying(monitor, markers).unit()
    assert markers.read("service-mgmt-gateway-attempt.json") is not None
    monitor.before_store, monitor.mutations = None, []
    assert gateway.applying(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")]


def test_gateway_claim_asks_only_listed_services(tmp_path):
    for services, expected in ((("alertmanager", "grafana", "prometheus"), ["alertmanager", "prometheus"]), (("grafana", "prometheus"), ["prometheus"]),
                               (("alertmanager",), ["alertmanager"]), (("grafana",), []), (None, [])):
        request = monitored(services) if services else gateway.gateway_request()
        markers = units.markers_for(tmp_path / str(len(expected)) / "-".join(services or ("none",)), request)
        monitor = Logins(request)
        gateway.applying(monitor, markers).unit()
        assert [command[1] for command in monitor.mutations if command[2:3] == ("set-credentials",)] == expected
        assert monitor.asked == []
    assert observability.GENERATED_USER == "admin"
