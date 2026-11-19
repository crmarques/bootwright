"""Update a management gateway's Prometheus or Alertmanager basic-auth login, or the monitoring unit's Grafana administrator password, in
the running cluster; shipped beside the core worker as source.

Upstream v20.2.1 facts this worker relies on:
- `orch prometheus set-credentials` and `orch alertmanager set-credentials` read `{"username", "password"}` from `-i` and only store the
  login (src/pybind/mgr/orchestrator/module.py, _get_credentials and _set_prometheus_access_info; src/pybind/mgr/cephadm/module.py,
  set_prometheus_access_info and set_alertmanager_access_info). Each daemon's dependencies hash its login
  (src/pybind/mgr/cephadm/services/monitoring.py, PrometheusService.get_dependencies and AlertmanagerService.get_dependencies), so
  cephadm's serve loop reconfigures it, re-rendering web.yml, within a serve interval (src/pybind/mgr/cephadm/serve.py, _check_daemons
  and _serve_sleep). `orch ps` reports each daemon's `last_configured` as an ISO 8601 UTC text with microseconds
  (src/pybind/mgr/orchestrator/_interface.py:1499-1502, src/python-common/ceph/utils.py, datetime_to_str).
- Behind a running management gateway each daemon's web.yml requires a client certificate the cephadm root CA verifies and, unless an
  oauth2-proxy daemon runs, basic auth (src/pybind/mgr/cephadm/templates/services/prometheus/web.yml.j2 and alertmanager/web.yml.j2;
  monitoring.py `enable_mtls` and `enable_basic_auth`); each serves under its route prefix /prometheus/ or /alertmanager
  (src/cephadm/cephadmlib/daemons/monitoring.py, `use_url_prefix`). Its server certificate names its host's inventory address
  (monitoring.py, get_prometheus_certificates and get_alertmanager_certificates). `orch certmgr generate-certificates prometheus` returns
  a cephadm-signed pair and stores nothing (cephadm/module.py, generate_certificates).
- Grafana applies `initial_admin_password` only when it creates its database, and every Grafana configuration generation sends the spec's
  value to `dashboard set-grafana-api-password` (monitoring.py, GrafanaService.generate_config;
  src/pybind/mgr/cephadm/templates/services/grafana/grafana.ini.j2). Behind a gateway Grafana serves from the /grafana sub path
  (grafana.ini.j2 `serve_from_sub_path`); oauth2-proxy disables only its login form, not basic auth. Each Grafana daemon keeps its own
  database. The dashboard's `get-grafana-api-password` is the generated getter of its GRAFANA_API_PASSWORD option
  (src/pybind/mgr/dashboard/settings.py, _options_command_map).
- The dashboard reads the Prometheus and Alertmanager logins from cephadm per request, cached one minute
  (src/pybind/mgr/dashboard/controllers/prometheus.py, get_access_info), so no dashboard write follows their update.
- Grafana's dependencies hash the Prometheus login (monitoring.py, GrafanaService.get_dependencies), so a Prometheus update has
  cephadm reconfigure Grafana in a later serve pass. A daemon cephadm (re)configures is cached `starting` with its post-deploy
  configuration pending until a later pass runs it (src/pybind/mgr/cephadm/serve.py, _create_daemon and _check_daemons), and
  `orch ps` shows both as `status` 2 and `pending_daemon_config` (src/pybind/mgr/orchestrator/_interface.py, to_json).
- Grafana 12.3.1, cephadm's default image (src/python-common/ceph/cephadm/images.py), locks a user out for five minutes after five
  failed logins and answers a locked basic-auth login with the same 401 and message as a wrong password
  (pkg/services/authn/clients/password.go, pkg/services/loginattempt/loginattemptimpl/login_attempt.go, conf/defaults.ini), so
  a daemon that refuses both passwords is either locked or changed by someone else, and only time tells them apart.

Every value reaches cephadm only on standard input and a daemon only in an HTTP header or body; refusals name the service or daemon. The
shapes are not yet observed on a live IBM Storage Ceph host.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import hmac
import json
import os
import re
import socket
import ssl
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native as native
from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_observability as observability
from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_units as units
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import NativeFailure, canonical, require

PHASES = ("login_prepare", "login_update")
GATEWAY_LOGINS = observability.CREDENTIAL_SERVICES
GRAFANA = "grafana"
GRAFANA_PATH = "/spec/initial_admin_password"
GRAFANA_VARIABLE = "grafanaAdminPassword"
GRAFANA_USER = "admin"
GRAFANA_PORT = 3000
# Each gateway login's probe path under its route prefix: an authenticated read that answers 200.
ROUTES = {"alertmanager": "/alertmanager/api/v2/status", "prometheus": "/prometheus/api/v1/status/buildinfo"}
MAX_GUARD = 4096
MAX_DAEMONS = 64
MAX_REPLY = 65536
PROBE_TIMEOUT = 5
PROOF_INTERVAL = 5
PROOF_BUDGET = 300
PROOF_RESERVE = 20
# cephadm's shortest serve sleep (src/pybind/mgr/cephadm/serve.py, _serve_sleep): a reconfiguration in flight lands within it.
SERVE_INTERVAL = 30
CONFIGURED = native.INCARNATION
STATUS = re.compile(rb"HTTP/1\.[01] ([1-5][0-9]{2})(?: [^\r\n]*)?\r?\n")


def same(first, second):
    """Equal texts, compared in constant time."""
    return isinstance(first, str) and isinstance(second, str) and hmac.compare_digest(first.encode("utf-8", "surrogatepass"),
                                                                                      second.encode("utf-8", "surrogatepass"))


def same_login(first, second):
    return same(first["user"], second["user"]) and same(first["password"], second["password"])


def basic(user, password):
    """The Authorization header's credentials; the value travels only in this header."""
    return b"Basic " + base64.b64encode((user + ":" + password).encode("utf-8"))


def reply(channel, data, deadline):
    """Send one HTTP/1.0 request and read its bounded reply before the deadline: (status, body), or None when nothing was proved."""
    channel.sendall(data)
    received = bytearray()
    while len(received) <= MAX_REPLY:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return None
        channel.settimeout(remaining)
        block = channel.recv(MAX_REPLY + 1 - len(received))
        if not block:
            break
        received += block
    found = STATUS.match(bytes(received))
    if found is None or len(received) > MAX_REPLY:
        return None
    head, separator, body = bytes(received).partition(b"\r\n\r\n")
    return int(found.group(1)), body if separator else b""


def exchange(target, method, path, authorization, body=b""):
    """One HTTP exchange with a daemon at (address, port, TLS name, context): TLS is verified against the context's CA for the name; a
    context of None speaks plain HTTP. The request buffer is cleared after it is sent."""
    address, number, name, context = target
    host = "[" + name + "]" if ":" in name else name
    data = bytearray(method.encode("ascii") + b" " + path.encode("ascii") + b" HTTP/1.0\r\nHost: " + host.encode("ascii") + b":"
                     + str(number).encode("ascii") + b"\r\nAccept: application/json\r\nAuthorization: ")
    data += authorization
    if body:
        data += b"\r\nContent-Type: application/json\r\nContent-Length: " + str(len(body)).encode("ascii")
    data += b"\r\nConnection: close\r\n\r\n" + body
    deadline = time.monotonic() + PROBE_TIMEOUT
    try:
        with socket.create_connection((address, number), timeout=PROBE_TIMEOUT) as raw:
            if context is None:
                return reply(raw, bytes(data), deadline)
            with context.wrap_socket(raw, server_hostname=name) as channel:
                return reply(channel, bytes(data), deadline)
    except (OSError, ValueError):
        return None
    finally:
        data[:] = bytes(len(data))


def trust(ca, pair=None):
    """A verifying TLS client context for the cephadm root CA; with a pair, its client identity, loaded from an anonymous memory file
    so the private key never reaches a filesystem."""
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    try:
        context.load_verify_locations(cadata=ca)
        if pair is not None:
            identity(context, pair)
    except (OSError, ValueError):
        raise native.Refusal("native-output") from None
    return context


def identity(context, pair):
    descriptor = os.memfd_create("monitoring-client", os.MFD_CLOEXEC)
    data = bytearray((pair["cert"] + "\n" + pair["key"]).encode("ascii"))
    try:
        view = memoryview(data)
        while view:
            view = view[os.write(descriptor, view):]
        context.load_cert_chain("/proc/self/fd/" + str(descriptor))
    finally:
        os.close(descriptor)
        data[:] = bytes(len(data))


def login_value(value):
    return type(value) is dict and set(value) == observability.LOGIN_MEMBERS and all(observability.login_value(value[key]) for key in value)


def rows_of(rows, service):
    """The `orch ps` rows of one singleton monitoring service, each with a host, a daemon name and a status."""
    found = [row for row in rows if row.get("service_name") == service and row.get("daemon_type") == service]
    require(len(found) <= MAX_DAEMONS, "native-output")
    for row in found:
        require(native.named(row.get("hostname")) and isinstance(row.get("daemon_id"), str) and native.named(row["daemon_id"]), "native-output")
    return sorted(found, key=lambda row: row["daemon_id"])


def daemon_name(row):
    return row["daemon_type"] + "." + row["daemon_id"]


def configured(row):
    """The daemon's last configuration time in cephadm's one UTC text form, or empty while it reports none."""
    value = row.get("last_configured")
    if value is None:
        return ""
    require(isinstance(value, str) and CONFIGURED.fullmatch(value) is not None, "native-output")
    return value


def running(row):
    return type(row.get("status")) is int and row["status"] == 1


class LoginWorker(units.UnitWorker):
    """The unit worker of a management gateway or monitoring unit, with the two login phases on the bootstrap host."""

    def unit_login(self, login):
        """The login names one of this unit's administrator logins: a gateway's listed Prometheus or Alertmanager, or the monitoring unit's
        bound Grafana password."""
        unit = self.request["service"]
        if login == GRAFANA:
            require(unit["type"] == "monitoring" and any(binding["serviceRef"] == GRAFANA and binding["path"] == GRAFANA_PATH
                                                         for binding in unit.get("bindings", [])), "request")
        else:
            require(login in observability.credential_services(unit), "request")

    def owned_unit(self, service):
        """The owner marker, the cluster's fsid, the unit's retained inputs and the service's completion record of this cluster's scope:
        its normalized document."""
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        values = self.unit_inputs()
        require(values is not None, "ownership")
        value = values[self.unit_names().index(service)]
        intent = {"scope": self.recovery_scope(), "service": value}
        require(self.markers.read("service-" + service + "-complete.json") == intent, "ownership")
        return value

    def daemon_rows(self):
        rows = self.manager_retry(self.ceph, "orch", "ps")
        require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "native-output")
        return rows

    def gateway_rows(self, login):
        """The orchestrator's daemons once a gateway daemon runs, which turns cephadm's monitoring security on, and a deployed daemon proves
        cephadm already holds the login, so asking for it stores none."""
        rows = self.daemon_rows()
        if not any(running(row) for row in rows_of(rows, "mgmt-gateway")):
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), login + ": no mgmt-gateway daemon runs")
        if not units.LOGIN_HOLDERS[login] & {row.get("daemon_type") for row in rows}:
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), login + ": cephadm does not yet hold the login")
        return rows

    def guard(self, login, incarnation, extra=None):
        value = dict(extra or {}, fsid=self.request["fsid"], incarnation=incarnation, login=login)
        require(len(canonical(value)) <= MAX_GUARD, "native-output")
        return value

    def login_prepare(self, login, incarnation):
        """Read-only: prove the unit and the service own this login, and return the non-secret guard its update checks. A gateway login's
        guard records each daemon's last configuration, which the update's proof needs to see move."""
        if login == GRAFANA:
            value = self.owned_unit(GRAFANA)
            self.grafana_leaf(self.manager_retry(self.unit_specs), value, None)
            return {"private": canonical({"guard": self.guard(login, incarnation)}).decode("ascii")}
        self.owned_unit("mgmt-gateway")
        rows = rows_of(self.gateway_rows(login), login)
        marks = {daemon_name(row): configured(row) for row in rows}
        return {"private": canonical({"guard": self.guard(login, incarnation, {"configured": marks})}).decode("ascii")}

    def checked_guard(self, login, incarnation, guard):
        if login == GRAFANA:
            require(guard == self.guard(login, incarnation), "material")
            return guard
        require(type(guard) is dict and set(guard) == {"configured", "fsid", "incarnation", "login"}, "material")
        marks = guard["configured"]
        require(type(marks) is dict and len(marks) <= MAX_DAEMONS and all(
            isinstance(name, str) and isinstance(mark, str) and (mark == "" or CONFIGURED.fullmatch(mark)) for name, mark in marks.items()), "material")
        require(guard == self.guard(login, incarnation, {"configured": marks}), "material")
        return guard

    def login_update(self, login, incarnation, update):
        """Update the login natively when cephadm still holds the active one, skip it when it already holds the candidate, then prove it."""
        require(type(update) is dict, "material")
        if login == GRAFANA:
            require(set(update) == {"guard", "password", "user"} and update["user"] == GRAFANA_USER, "material")
            require(units.material_value(GRAFANA_VARIABLE, update["password"]), "material")
            self.checked_guard(login, incarnation, update["guard"])
            return self.grafana_update(update["password"])
        require(set(update) == {"active", "guard", "password", "user"} and login_value(update["active"]), "material")
        candidate = {"password": update["password"], "user": update["user"]}
        require(login_value(candidate) and not same_login(candidate, update["active"]), "material")
        guard = self.checked_guard(login, incarnation, update["guard"])
        self.owned_unit("mgmt-gateway")
        self.gateway_rows(login)
        self.set_login(login, candidate, update["active"])
        self.prove_login(login, guard["configured"], candidate, update["active"])
        return {"proved": True}

    def stored_login(self, login, secrets):
        answer = self.manager_retry(self.ceph, "orch", login, "get-credentials", secrets=secrets)
        return observability.native_login(answer, login, self.native_coordinate())

    def set_login(self, login, candidate, active):
        secrets = (candidate["password"], active["password"])
        stored = self.stored_login(login, secrets)
        if same_login(stored, candidate):
            return
        if not same_login(stored, active):
            raise NativeFailure("monitoring-login-drift", self.native_coordinate(), login + " holds neither the active nor the candidate login",
                                secrets=secrets + (stored["password"],))
        data = bytearray(canonical({"password": candidate["password"], "username": candidate["user"]}))
        try:
            self.manager_declare(self.tunnel_effect, "orch", login, "set-credentials", "-i", "-", data=bytes(data), json_result=False, secrets=secrets)
        finally:
            data[:] = bytes(len(data))
        if not same_login(self.stored_login(login, secrets), candidate):
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), login + " did not keep the candidate login", secrets=secrets)

    def root_ca(self):
        with self.manager_served():
            raw = self.ceph_text("orch", "certmgr", "cert", "get", "cephadm_root_ca_cert")
            require(isinstance(raw, bytes) and 0 < len(raw) <= observability.MAX_CERTIFICATE, "native-output")
        return raw.decode("ascii")

    def client_pair(self):
        """A cephadm-signed client identity the daemons' web configuration verifies; held only in this worker's memory."""
        pair = self.manager_retry(self.ceph, "orch", "certmgr", "generate-certificates", "prometheus")
        require(type(pair) is dict and set(pair) == {"cert", "key"}, "native-output")
        require(all(isinstance(pair[key], str) and pair[key].isascii() for key in pair), "native-output")
        require(observability.CERTIFICATE.search(pair["cert"]) is not None and observability.PRIVATE_KEY.search(pair["key"]) is not None, "native-output")
        return pair

    def proof_deadline(self):
        deadline = self.clock() + PROOF_BUDGET
        if self.execution_deadline is not None:
            deadline = min(deadline, self.execution_deadline - PROOF_RESERVE)
        return deadline

    def target(self, row, context, fallback):
        """A daemon's (address, port, TLS name, context): its listening address, and its host's inventory address as the name its
        cephadm-signed certificate carries."""
        ports = row.get("ports")
        number = ports[0] if isinstance(ports, list) and ports and type(ports[0]) is int else fallback
        require(type(number) is int and 0 < number < 65536, "native-output")
        host = next((item for item in self.request["hosts"] if item["name"] == row["hostname"]), None)
        require(host is not None, "foreign-service")
        return self.daemon_address(row, number), number, host["address"], context

    def prove_login(self, login, marks, candidate, active):
        """Each running daemon of the service was reconfigured after the guard's mark and answers the candidate, and, where its web
        configuration enforces basic auth, refuses the active login with 401; repeated within the proof's bound."""
        pair = self.client_pair()
        try:
            context = trust(self.root_ca(), pair)
        finally:
            pair.clear()
        deadline = self.proof_deadline()
        while True:
            pending = self.login_pending(login, self.daemon_rows(), marks, context, candidate, active)
            if pending is None:
                return
            if self.clock() + PROOF_INTERVAL >= deadline:
                raise NativeFailure("monitoring-login-unproved", self.native_coordinate(),
                                    login + " daemon " + pending + " does not yet serve the candidate login",
                                    secrets=(candidate["password"], active["password"]))
            self.pause(PROOF_INTERVAL)

    def login_pending(self, login, rows, marks, context, candidate, active):
        """The first daemon of the service not yet proved, or None once every one is; an oauth2-proxy daemon turns basic auth off."""
        enforced = not rows_of(rows, "oauth2-proxy")
        daemons = rows_of(rows, login)
        if not daemons:
            return "none"
        for row in daemons:
            name = daemon_name(row)
            if not running(row) or configured(row) <= marks.get(name, ""):
                return name
            target = self.target(row, context, None)
            answer = exchange(target, "GET", ROUTES[login], basic(candidate["user"], candidate["password"]))
            if answer is None or answer[0] != 200:
                return name
            if enforced:
                answer = exchange(target, "GET", ROUTES[login], basic(active["user"], active["password"]))
                if answer is None or answer[0] != 401:
                    return name
        return None

    def grafana_leaf(self, stored, value, candidate):
        """The stored Grafana document of this unit: its placeholder form is the retained one, and its password leaf, when exported, is the
        bound password or the candidate: "bound", "candidate", or None when the export omits it."""
        bound = self.bound[GRAFANA_VARIABLE]
        document = stored.get(GRAFANA)
        require(isinstance(document, dict) and self.markers.read("service-" + GRAFANA + "-attempt.json") is not None, "foreign-service")
        present, current = units.leaf(document, GRAFANA_PATH)
        state = None
        if present and same(current, bound):
            state = "bound"
        elif present and candidate is not None and same(current, candidate):
            state = "candidate"
        elif present:
            secrets = (bound, candidate or "", current if isinstance(current, str) else "")
            drift = NativeFailure("grafana-login-drift", self.native_coordinate(), "grafana stored spec holds another password", secrets=secrets)
            drift.redacted = secrets
            raise drift
        require(units.placed(document, [GRAFANA_PATH]) == value, "foreign-service")
        return state

    def grafana_targets(self, rows, value):
        """Each running Grafana daemon's target, and the path prefix it serves under: /grafana once a gateway daemon is deployed."""
        protocol = value.get("spec", {}).get("protocol", "https")
        context = trust(self.root_ca()) if protocol == "https" else None
        expected = observability.expected({"documents": [value]})[(GRAFANA, GRAFANA)]
        daemons = [row for row in rows_of(rows, GRAFANA) if running(row)]
        if len(daemons) != expected:
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(),
                                "grafana runs " + str(len(daemons)) + " of " + str(expected) + " daemons")
        prefix = "/grafana" if rows_of(rows, "mgmt-gateway") else ""
        return [(daemon_name(row), self.target(row, context, value.get("spec", {}).get("port", GRAFANA_PORT))) for row in daemons], prefix

    def grafana_update(self, candidate):
        """Change each Grafana daemon's administrator password from the bound one to the candidate, then the stored spec leaf and the
        dashboard's Grafana API password, each skipped where it already holds the candidate. The daemons are awaited settled before the
        change and after it, since a Prometheus update earlier in the run has cephadm reconfigure them, and the dashboard is read again
        once every daemon proves the candidate and again once no daemon's configuration moved over a serve interval, since each Grafana
        configuration generation sends the stored leaf to it."""
        bound = self.bound[GRAFANA_VARIABLE]
        secrets = (bound, candidate)
        value = self.owned_unit(GRAFANA)
        deadline = self.proof_deadline()
        try:
            state = self.grafana_leaf(self.manager_retry(self.unit_specs), value, candidate)
        except NativeFailure as drift:
            if str(drift) != "grafana-login-drift":
                raise
            raise self.grafana_drift(drift, value, secrets) from None
        targets, prefix = self.grafana_settled(value, deadline)
        for name, target in targets:
            self.grafana_daemon(name, target, prefix, secrets, deadline)
        if state != "candidate":
            self.grafana_spec(value, candidate)
        self.dashboard_password(bound, candidate)
        self.grafana_proved(value, secrets, deadline)
        self.dashboard_password(bound, candidate)
        self.grafana_quiet(value, secrets, deadline)
        self.dashboard_password(bound, candidate)
        return {"proved": True}

    def grafana_proved(self, value, secrets, deadline):
        """Every placed daemon, once settled, accepts the candidate."""
        targets, prefix = self.grafana_settled(value, deadline)
        for name, target in targets:
            if self.grafana_user(name, target, prefix, secrets[1], secrets, deadline) != 200:
                raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + name + " does not accept the candidate",
                                    secrets=secrets)

    def grafana_marks(self):
        rows = rows_of(self.daemon_rows(), GRAFANA)
        return {daemon_name(row): (configured(row), row.get("status"), row.get("pending_daemon_config") is True) for row in rows}

    def grafana_quiet(self, value, secrets, deadline):
        """A reconfiguration that read the spec before the splice sends the bound password to the dashboard before cephadm marks its
        daemon (monitoring.py, GrafanaService.generate_config, ahead of serve.py's post-deploy marking), so it can look settled: the
        update waits until no daemon's configuration, status or pending mark moved over a serve interval, proving every daemon again
        after each move, within the proof's bound."""
        while True:
            marks = self.grafana_marks()
            if self.clock() + SERVE_INTERVAL >= deadline:
                raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemons did not stay settled for a serve interval",
                                    secrets=secrets)
            self.pause(SERVE_INTERVAL)
            if self.grafana_marks() == marks:
                return
            self.grafana_proved(value, secrets, deadline)

    def grafana_settled(self, value, deadline):
        """Each placed Grafana daemon's target once none is starting or awaiting its post-deploy configuration, polled within the proof's
        bound; a daemon that is stopped or missing is not awaited."""
        while True:
            rows = self.daemon_rows()
            unsettled = [daemon_name(row) for row in rows_of(rows, GRAFANA) if row.get("status") == 2 or row.get("pending_daemon_config") is True]
            if not unsettled:
                return self.grafana_targets(rows, value)
            if self.clock() + PROOF_INTERVAL >= deadline:
                raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + unsettled[0] + " is still being reconfigured")
            self.pause(PROOF_INTERVAL)

    def grafana_drift(self, drift, value, secrets):
        """A stored leaf that is neither password, found before this update changed anything: unapplied when nothing holds the candidate,
        every placed daemon running and accepting the bound password, since a daemon keeps one, and the dashboard's Grafana API password
        not the candidate; otherwise the drift as found, since an earlier attempt may have placed the candidate. Nothing is retried."""
        bound, candidate = secrets
        redacted = tuple(getattr(drift, "redacted", secrets))
        try:
            targets, prefix = self.grafana_targets(self.daemon_rows(), value)
            held = any(self.grafana_user(name, target, prefix, bound, redacted, self.clock()) != 200 for name, target in targets)
            held = held or self.dashboard_holds(candidate, redacted)
        except native.Refusal:
            return drift
        if held:
            return drift
        return NativeFailure("grafana-login-drift-unapplied", self.native_coordinate(),
                             "grafana stored spec holds another password and nothing holds the candidate", secrets=redacted)

    def grafana_user(self, name, target, prefix, password, secrets, deadline):
        """GET <prefix>/api/user as admin: 200 for this administrator, 401 refused, polled within the deadline while the daemon is
        unreachable or answers 5xx, as one restarting after a reconfiguration does; anything else is not proved."""
        while True:
            answer = exchange(target, "GET", prefix + "/api/user", basic(GRAFANA_USER, password))
            if answer is not None and answer[0] < 500:
                break
            if self.clock() + PROOF_INTERVAL >= deadline:
                raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + name + " did not answer", secrets=secrets)
            self.pause(PROOF_INTERVAL)
        if answer[0] not in (200, 401):
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + name + " did not answer", secrets=secrets)
        if answer[0] == 200:
            try:
                user = json.loads(answer[1])
            except (ValueError, UnicodeError):
                user = None
            if not isinstance(user, dict) or user.get("login") != GRAFANA_USER:
                raise NativeFailure("grafana-login-drift", self.native_coordinate(), "grafana daemon " + name + " answers another user", secrets=secrets)
        return answer[0]

    def grafana_daemon(self, name, target, prefix, secrets, deadline):
        """One daemon's database: skipped when it accepts the candidate, changed when it accepts the bound password; one that refuses both
        is locked out or was changed by someone else. Grafana keeps one password per user, so the candidate's 200 after the change proves
        the bound password gone, and no further failed login counts toward the lockout, which a success does not reset."""
        bound, candidate = secrets
        if self.grafana_user(name, target, prefix, candidate, secrets, deadline) == 200:
            return
        if self.grafana_user(name, target, prefix, bound, secrets, deadline) != 200:
            raise NativeFailure("grafana-login-refused", self.native_coordinate(), "grafana daemon " + name + " refuses both passwords", secrets=secrets)
        body = bytearray(canonical({"confirmNew": candidate, "newPassword": candidate, "oldPassword": bound}))
        try:
            answer = exchange(target, "PUT", prefix + "/api/user/password", basic(GRAFANA_USER, bound), bytes(body))
        finally:
            body[:] = bytes(len(body))
        if answer is None or answer[0] != 200:
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + name + " did not change the password",
                                secrets=secrets)
        if self.grafana_user(name, target, prefix, candidate, secrets, deadline) != 200:
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "grafana daemon " + name + " did not keep the candidate",
                                secrets=secrets)

    def grafana_spec(self, value, candidate):
        """Store the candidate as the Grafana document's password leaf through one `orch apply -i -` of the unit's retained document, then
        read it back holding the candidate; placement and every other member are unchanged, so cephadm redeploys nothing."""
        held = self.bound
        self.bound = {GRAFANA_VARIABLE: candidate}
        try:
            data = bytearray(canonical(self.splice_bound(value)) + b"\n")
            try:
                self.manager_declare(self.tunnel_effect, "orch", "apply", "-i", "-", data=bytes(data), json_result=False,
                                     secrets=(held[GRAFANA_VARIABLE], candidate))
            finally:
                data[:] = bytes(len(data))
            observed = self.bound_specs(self.manager_retry(self.unit_specs))
            require(observed.get(GRAFANA) == value, "not-ready")
        finally:
            self.bound = held

    def grafana_api_password(self, secrets):
        def read():
            with self.bootstrap_hold():
                return self.command([*self.admin_argv("/usr/bin/ceph"), "dashboard", "get-grafana-api-password"], json_result=False, secrets=secrets)
        output = self.manager_retry(read)
        try:
            return bytes(output).decode("utf-8").rstrip("\n")
        except UnicodeError:
            return None

    def dashboard_enabled(self):
        modules = self.ceph("mgr", "module", "ls")
        require(isinstance(modules, dict) and isinstance(modules.get("enabled_modules"), list), "native-output")
        return "dashboard" in modules["enabled_modules"]

    def dashboard_holds(self, candidate, secrets):
        """Whether the dashboard's Grafana API password may be the candidate: an unreadable one may."""
        if not self.dashboard_enabled():
            return False
        held = self.grafana_api_password(secrets)
        return held is None or same(held, candidate)

    def dashboard_password(self, bound, candidate):
        """With the dashboard module enabled, its Grafana API password becomes the candidate, read back and compared in memory."""
        if not self.dashboard_enabled():
            return
        secrets = (bound, candidate)
        if same(self.grafana_api_password(secrets), candidate):
            return
        data = bytearray(candidate.encode("utf-8"))
        try:
            self.manager_declare(self.tunnel_effect, "dashboard", "set-grafana-api-password", "-i", "-", data=bytes(data), json_result=False, secrets=secrets)
        finally:
            data[:] = bytes(len(data))
        if not same(self.grafana_api_password(secrets), candidate):
            raise NativeFailure("monitoring-login-unproved", self.native_coordinate(), "the dashboard did not keep the grafana password", secrets=secrets)


def dispatch(envelope):
    """One login phase of a gateway or monitoring unit request: the incarnation and login Go froze, the bound leaves the phase holds, and,
    on update, the retained candidate."""
    worker = LoginWorker(envelope["request"], envelope["host"])
    worker.horizon = worker.clock() + native.MANAGER_HORIZON
    worker.execution_deadline = native.WORKER_DEADLINE
    require(worker.request["action"] == "service", "request")
    phase = envelope["phase"]
    private = dict(envelope["private"])
    incarnation, login, update = private.pop("incarnation", None), private.pop("login", None), private.pop("update", None)
    require(login == GRAFANA or login in GATEWAY_LOGINS, "request")
    worker.unit_login(login)
    require((update is None) == (phase == "login_prepare"), "material")
    if phase == "login_update":
        require(native.supported_transport(worker.request), "unsupported-release")
    worker.bound = units.material(worker.request, phase, private)
    worker.verify_tool()
    require(incarnation is not None and worker.monmap_incarnation()[0] == incarnation, "ownership")
    if phase == "login_prepare":
        return worker.login_prepare(login, incarnation)
    return worker.login_update(login, incarnation, update)
