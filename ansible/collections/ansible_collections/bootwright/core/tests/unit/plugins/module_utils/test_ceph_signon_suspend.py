"""A CephSignOn's suspension and resume: the before-state read before any effect, the removal of only the service the block's own records
prove it applied, the wait for cephadm's post_remove and reconfiguration, local sign-in proved on the bootstrap host without the password
leaving it, no call toward the identity provider while suspending, and a resume that re-runs the sign-on's apply phases and refuses an
unreachable provider before any effect.

The fakes answer the upstream v20.2.1 shapes the worker reads: `ceph orch rm <service>` removes a stored service, after which cephadm's
oauth2-proxy post_remove disables the dashboard's OAuth2 single sign-on when its last daemon goes
(src/pybind/mgr/cephadm/services/oauth2_proxy.py); `ceph dashboard sso disable` sets the dashboard option sso_oauth2 false
(src/pybind/mgr/dashboard/services/sso.py); `orch ps` rows carry `last_configured` as ISO 8601 UTC text with microseconds
(src/pybind/mgr/orchestrator/_interface.py, src/python-common/ceph/utils.py datetime_to_str).
"""

from __future__ import annotations

import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_evidence, ceph_native, ceph_native_units, ceph_signon_native as signon
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_signon as signon_tests

DIGEST = "d" * 64
BEFORE = "2026-11-25T10:00:00.000000Z"
AFTER = "2026-11-25T10:05:00.000000Z"
BASELINE = [{"configured": BEFORE, "name": "grafana.node-1"}, {"configured": BEFORE, "name": "mgmt-gateway.node-1"}]
normalization = gateway.normalization


def configured(when):
    return [dict(entry, configured=when) for entry in BASELINE]


def state(service=False, daemons=0, sso=False, when=AFTER):
    return {"changed": False, "configured": configured(when), "daemons": daemons, "service": service, "sso": sso}


class SuspendTransport:
    """The coordinator's view of a suspension: every phase it calls on each host with the material it held, and scripted answers."""

    def __init__(self, request, states=(), local=(True,), removed=True, disables=None, configured=()):
        self.request, self.calls, self.configured = request, [], list(configured)
        self.states, self.local, self.removed = list(states), list(local), removed
        self.disables = list(disables or [{"changed": True, "pending": False}])

    def unit_material(self, phase="unit"):
        return {name: signon_tests.SSO_MATERIAL[name] for name in ceph_native_units.material_names(self.request, phase)}

    def call(self, host, phase, private=None, *, timeout=None):
        self.calls.append((host["name"], phase, sorted(private or {})))
        assert timeout is None or 0 < timeout <= ceph_core.READ_BUDGET
        if phase == "prerequisites":
            return {"name": host["name"], "owned": True, "exists": True}
        if phase == "signon_baseline":
            return {"changed": False, "daemons": BASELINE}
        if phase == "signon_remove":
            return {"changed": self.removed, "configured": self.configured}
        if phase == "signon_state":
            return self.states.pop(0) if len(self.states) > 1 else self.states[0]
        if phase == "signon_sso_disable":
            return self.disables.pop(0)
        if phase == "signon_local":
            return {"changed": False, "local": self.local.pop(0) if len(self.local) > 1 else self.local[0]}
        raise AssertionError("the suspension called " + phase)


def phases(transport):
    return [phase for unused, phase, unused_private in transport.calls]


def test_a_suspension_preparation_reads_the_dependent_daemons_and_changes_nothing():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request)
    result = ceph_core.execute(request, "suspend_prepare", DIGEST, transport)
    assert result == {"changed": False, "evidence": {"version": ceph_evidence.SIGNON_SUSPENSION_EVIDENCE, "request": DIGEST, "state": "prepared",
                                                     "changed": False, "baseline": BASELINE}}
    assert transport.calls == [("node-1", "prerequisites", []), ("node-1", "signon_baseline", [])]


def test_a_suspension_removes_the_proxy_then_waits_for_cephadm_then_proves_local_sign_in_on_the_bootstrap_host_alone():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=[state(service=True, daemons=2, sso=True, when=BEFORE), state(sso=True, when=BEFORE),
                                                  state(when=BEFORE), state()], local=(False, True))
    now, pause, unused = signon_tests.ticking()
    result = ceph_core.execute(request, "suspend", DIGEST, transport, now=now, pause=pause, baseline=BASELINE)
    assert result["changed"] is True and result["evidence"]["state"] == "suspended" and result["evidence"]["baseline"] == []
    assert phases(transport) == ["prerequisites", "signon_remove", "signon_state", "signon_state", "signon_sso_disable", "signon_state",
                                 "signon_state", "signon_local", "signon_state", "signon_local"]
    assert {name for name, unused, unused_private in transport.calls} == {"node-1"}
    assert all(private == [] for unused, unused_phase, private in transport.calls)
    assert "signon_discovery" not in phases(transport)


def test_a_suspension_asks_the_dashboard_to_disable_its_single_sign_on_only_once_and_only_without_a_proxy():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=[state(sso=True, daemons=1), state(sso=True), state(sso=True), state()])
    now, pause, unused = signon_tests.ticking()
    ceph_core.execute(request, "suspend", DIGEST, transport, now=now, pause=pause, baseline=BASELINE)
    assert phases(transport).count("signon_sso_disable") == 1
    assert phases(transport).index("signon_sso_disable") == phases(transport).index("signon_state") + 2


def test_a_suspension_over_a_suspended_cluster_settles_unchanged():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=[state()], removed=False)
    result = ceph_core.execute(request, "suspend", DIGEST, transport, now=lambda: 0, pause=lambda seconds: None, baseline=BASELINE)
    assert result["changed"] is False and result["evidence"]["state"] == "suspended"
    assert phases(transport) == ["prerequisites", "signon_remove", "signon_state", "signon_local"]


@pytest.mark.parametrize("states, local, waited", [
    ([state(service=True, daemons=1)], (True,), "the oauth2-proxy removal"),
    ([state(sso=True)], (True,), "the dashboard's single sign-on to turn off"),
    ([state(when=BEFORE)], (True,), "cephadm to reconfigure grafana.node-1"),
    ([state()], (False,), "local sign-in through the management gateway"),
])
def test_a_suspension_that_never_settles_refuses_naming_what_it_waited_for(states, local, waited):
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=states, local=local, disables=[{"changed": False, "pending": False}])
    now, pause, unused = signon_tests.ticking()
    with pytest.raises(ceph_native.NativeFailure) as refused:
        ceph_core.execute(request, "suspend", DIGEST, transport, now=now, pause=pause, baseline=BASELINE)
    assert str(refused.value) == "sign-on-suspend-unproved" and waited in json.dumps(refused.value.native_error)


def test_a_proxy_that_came_back_after_the_recorded_baseline_is_waited_out_from_the_removal():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=[state()], configured=configured(AFTER))
    now, pause, unused = signon_tests.ticking()
    with pytest.raises(ceph_native.NativeFailure) as refused:
        ceph_core.execute(request, "suspend", DIGEST, transport, now=now, pause=pause, baseline=BASELINE)
    assert "cephadm to reconfigure grafana.node-1" in json.dumps(refused.value.native_error) and "signon_local" not in phases(transport)
    later = "2026-11-25T10:09:00.000000Z"
    transport = SuspendTransport(request, states=[state(), state(when=later)], configured=configured(AFTER))
    now, pause, unused = signon_tests.ticking()
    assert ceph_core.execute(request, "suspend", DIGEST, transport, now=now, pause=pause, baseline=BASELINE)["evidence"]["state"] == "suspended"
    assert signon.latest(BASELINE, [{"configured": AFTER, "name": "grafana.node-1"}, {"configured": AFTER, "name": "osd.0"}]) == [
        {"configured": AFTER, "name": "grafana.node-1"}, {"configured": BEFORE, "name": "mgmt-gateway.node-1"}]


def test_a_daemon_the_baseline_does_not_name_needs_no_reconfiguration():
    assert signon.lagging(BASELINE, configured(AFTER) + [{"configured": BEFORE, "name": "prometheus.node-2"}]) is None
    assert signon.lagging(BASELINE, configured(BEFORE)[:1]) == "grafana.node-1"
    assert signon.lagging(BASELINE, [{"configured": "", "name": "grafana.node-1"}]) == "grafana.node-1"
    assert signon.lagging(BASELINE, []) is None


def test_a_suspension_runs_only_with_its_recorded_baseline_and_only_on_a_sign_on_unit():
    request = signon_tests.sso_request()
    for operation, baseline in (("suspend", None), ("suspend_prepare", BASELINE), ("resume", BASELINE)):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_core.execute(request, operation, DIGEST, SuspendTransport(request), baseline=baseline)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(gateway.gateway_request(), "suspend_prepare", DIGEST, SuspendTransport(request))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.execute(request, "observe", DIGEST, SuspendTransport(request), signon="active")
    for text in ('{"baseline":[],"x":1}', "[]", '{"baseline":[{"configured":"","name":"Grafana"}]}', "x" * (ceph_core.MAX_PREPARATION + 1)):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            ceph_core.suspension_baseline(text)
    assert ceph_core.suspension_baseline(json.dumps({"baseline": BASELINE})) == BASELINE and ceph_core.suspension_baseline(None) is None


def test_a_suspended_sign_on_observes_its_suspended_state_and_refuses_one_that_came_back():
    request = signon_tests.sso_request()
    transport = SuspendTransport(request, states=[state()])
    result = ceph_core.execute(request, "observe", DIGEST, transport, signon="suspended")
    assert result["evidence"]["state"] == "suspended" and phases(transport) == ["signon_state"]
    for back in (state(service=True), state(daemons=1), state(sso=True)):
        with pytest.raises(ceph_native.Refusal, match="^sign-on-suspension-drift$"):
            ceph_core.execute(request, "observe", DIGEST, SuspendTransport(request, states=[back]), signon="suspended")


def test_a_resume_refuses_an_unreachable_provider_before_any_effect():
    request = signon_tests.sso_request()
    transport = signon_tests.SSOTransport(request, [])
    transport.refusals["node-2"] = ceph_native.NativeFailure("sign-on-provider-unreachable", "discovery", "host node-2: connect " + signon_tests.ISSUER)
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-provider-unreachable$"):
        ceph_core.execute(request, "resume", DIGEST, transport, now=lambda: 0, pause=lambda seconds: None)
    assert [phase for unused, phase in signon_tests.phases_of(transport)] == ["prerequisites", "signon_discovery", "signon_discovery"]


def test_a_resume_runs_the_sign_on_apply_phases_with_its_resume_stream():
    request = signon_tests.sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    transport = signon_tests.SSOTransport(request, [one, one], enables=[{"changed": False, "pending": False}])
    result = ceph_core.execute(request, "resume", DIGEST, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["sso"] == "oauth2" and result["evidence"]["ready"] is True
    called = signon_tests.phases_of(transport)
    assert ("node-1", "signon_resume_unit") in called and ("node-1", "unit") not in called
    assert ("node-1", "signon_resume_unit", sorted(signon_tests.SSO_MATERIAL)) in transport.private


class SuspendMonitor(signon_tests.SSOMonitor):
    """The sign-on monitor with `orch rm`, `dashboard sso disable` and the listed oauth2-proxy daemons."""

    def __init__(self, request, **options):
        super().__init__(request, **options)
        self.proxies = []

    def run(self, argv, data=b"", timeout=60):
        if argv[0] != ceph_native_units.PODMAN:
            command = tuple(argv[argv.index("--format") + 2:])
            if command == ("orch", "rm", "oauth2-proxy"):
                self.log.append(list(argv))
                self.mutations.append(command)
                self.specs.pop("oauth2-proxy", None)
                return 0, b"Removed service oauth2-proxy"
            if command == ("dashboard", "sso", "disable"):
                self.log.append(list(argv))
                self.mutations.append(command)
                self.enabled = False
                return 0, b'SSO is "disabled".'
            if command == ("orch", "ps", "--daemon_type", "oauth2-proxy"):
                self.log.append(list(argv))
                return 0, json.dumps(self.proxies).encode()
        return super().run(argv, data, timeout)


def suspending(monitor, markers):
    native = units.worker(monitor, markers)
    native.bound = ceph_native_units.material(monitor.request, "signon_remove", {})
    return native


def test_the_removal_removes_only_the_service_this_block_applied_and_repeats_as_nothing(tmp_path):
    request = signon_tests.sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SuspendMonitor(request)
    assert signon_tests.applying(monitor, markers).unit() == {"changed": True}
    assert suspending(monitor, markers).signon_remove()["changed"] is True
    assert monitor.mutations.count(("orch", "rm", "oauth2-proxy")) == 1 and "oauth2-proxy" not in monitor.specs
    assert suspending(monitor, markers).signon_remove() == {"changed": False, "configured": []}
    assert monitor.mutations.count(("orch", "rm", "oauth2-proxy")) == 1
    assert not any(mark in json.dumps(monitor.log) for mark in signon_tests.SECRET_MARKS)


def test_the_removal_refuses_a_service_this_block_did_not_apply(tmp_path):
    request = signon_tests.sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SuspendMonitor(request)
    assert signon_tests.applying(monitor, markers).unit() == {"changed": True}
    monitor.specs["oauth2-proxy"] = dict(monitor.specs["oauth2-proxy"], spec=dict(monitor.specs["oauth2-proxy"]["spec"], client_id="another"))
    with pytest.raises(ceph_native.Refusal, match="^(sign-on-foreign|foreign-service)$"):
        suspending(monitor, markers).signon_remove()
    assert ("orch", "rm", "oauth2-proxy") not in monitor.mutations
    fresh = units.markers_for(tmp_path / "fresh", request)
    with pytest.raises(ceph_native.Refusal, match="^ownership$"):
        suspending(SuspendMonitor(request), fresh).signon_remove()


def test_the_dashboard_single_sign_on_is_disabled_only_once_no_proxy_runs(tmp_path):
    request = signon_tests.sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SuspendMonitor(request, enabled=True)
    monitor.proxies = [{"daemon_type": "oauth2-proxy", "service_name": "oauth2-proxy", "status": 1}]
    assert suspending(monitor, markers).signon_sso_disable() == {"changed": False, "pending": True}
    monitor.proxies = []
    assert suspending(monitor, markers).signon_sso_disable() == {"changed": True, "pending": False}
    assert suspending(monitor, markers).signon_sso_disable() == {"changed": False, "pending": False}
    assert monitor.mutations.count(("dashboard", "sso", "disable")) == 1 and monitor.enabled is False


def test_a_resume_stream_restores_the_removed_document_while_a_plain_unit_refuses_it(tmp_path):
    request = signon_tests.sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SuspendMonitor(request)
    assert signon_tests.applying(monitor, markers).unit() == {"changed": True}
    assert suspending(monitor, markers).signon_remove()["changed"] is True
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        signon_tests.applying(monitor, markers).unit()
    native = units.worker(monitor, markers)
    native.bound = ceph_native_units.material(request, "signon_resume_unit", dict(signon_tests.SSO_MATERIAL))
    assert native.signon_resume_unit() == {"changed": True}
    assert "oauth2-proxy" in monitor.specs and signon_tests.CLIENT.encode() in monitor.stdin[-1]
    assert signon_tests.applying(monitor, markers).unit() == {"changed": False}


def test_the_suspension_phases_hold_no_sign_on_leaf():
    request = signon_tests.sso_request()
    for phase in ("signon_baseline", "signon_remove", "signon_state", "signon_sso_disable", "signon_local"):
        assert ceph_native_units.material_names(request, phase) == frozenset()
        assert phase in ceph_native_units.PHASES
    assert ceph_native_units.material_names(request, "signon_resume_unit") == ceph_native_units.material_names(request, "unit")
    assert {"signon_baseline", "signon_state", "signon_local"} <= ceph_native_units.READ_ONLY_PHASES
    assert not {"signon_remove", "signon_sso_disable", "signon_resume_unit"} & ceph_native_units.READ_ONLY_PHASES
    for reason in ("sign-on-foreign", "sign-on-suspend-unproved", "sign-on-suspension-drift"):
        assert reason in ceph_core.CLUSTER_REASONS


def test_the_suspension_phases_run_only_on_the_bootstrap_host_of_a_sign_on_unit(tmp_path):
    request = signon_tests.sso_request()
    markers = units.markers_for(tmp_path, request)
    other = ceph_native_units.UnitWorker(request, "node-2", run=SuspendMonitor(request).run, markers=markers)
    other.bound = {}
    for phase in ("signon_baseline", "signon_remove", "signon_state", "signon_sso_disable", "signon_local"):
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            getattr(other, phase)()
    plain = gateway.gateway_request()
    native = units.worker(SuspendMonitor(plain), units.markers_for(tmp_path / "plain", plain))
    native.bound = {}
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        native.signon_baseline()


def test_only_the_gateways_own_sign_in_proves_local_sign_in():
    assert signon.local(200, None, signon_tests.ISSUER)
    assert signon.local(302, "/#/login", signon_tests.ISSUER)
    assert not signon.local(302, "https://idp.example.test/realms/ceph/protocol/openid-connect/auth?x=1", signon_tests.ISSUER)
    assert not signon.local(302, "/oauth2/sign_in", signon_tests.ISSUER)
    assert not signon.local(302, "//idp.example.test/realms/ceph/protocol/openid-connect/auth", signon_tests.ISSUER)
    assert not signon.local(302, "http://idp.example.test/x", signon_tests.ISSUER)
    assert not signon.local(302, "relative", signon_tests.ISSUER)
    assert signon.local(303, "https://gateway.lab.test:9443/#/login", signon_tests.ISSUER)
    assert not signon.local(302, None, signon_tests.ISSUER)
    for status in (401, 403, 500, 502):
        assert not signon.local(status, None, signon_tests.ISSUER)


class LocalWorker:
    """The bootstrap host's dashboard custody and gateway as local_sign_in reads them; the password is checked here only."""

    def __init__(self, request, enabled=False, verified=True):
        self.request, self.host = request, {"name": "node-1"}
        self.enabled, self.verified, self.checked = enabled, verified, []
        self.markers = self

    def owned(self):
        return True

    def read(self, name):
        return {"applied": True} if name == "dashboard-applied.json" else None

    def ceph(self, *args, **options):
        if args == ("fsid",):
            return {"fsid": self.request["fsid"]}
        assert args == ("config", "get", "mgr", signon.SSO_OPTION)
        return self.enabled

    def dashboard_store(self):
        worker = self

        class Store:
            def name(self, record):
                return "dashboard-" + record + ".json"

            def binding(self, scope, version):
                assert scope == {"owner": worker.request["owner"], "fsid": worker.request["fsid"], "host": "node-1", "user": "admin"}
                return {"scope": scope, "version": version}

            def prove_binding(self, binding):
                del binding

            def material(self, scope):
                del scope
                return "custodied-dashboard-password-0123456789"

        return Store()

    def dashboard_verify(self, password):
        self.checked.append(password)
        if not self.verified:
            raise ceph_native.Refusal("dashboard-drift")


def test_local_sign_in_checks_the_custodied_password_on_the_host_and_reads_the_gateway_without_it(monkeypatch):
    request = signon_tests.sso_request()
    sent = []

    def probe_answer(address, number, name, host_header, pinned):
        sent.append((address, number, name, host_header, pinned))
        return (200, None)

    monkeypatch.setattr(signon, "probe_answer", probe_answer)
    worker = LocalWorker(request)
    assert signon.local_sign_in(worker) == {"changed": False, "local": True}
    assert worker.checked == ["custodied-dashboard-password-0123456789"]
    assert sent == [(gateway.VIP, gateway.PORT, gateway.VIP, gateway.VIP, signon_tests.PIN)]
    monkeypatch.setattr(signon, "probe_answer", lambda *unused: (302, signon_tests.ISSUER + "/protocol/openid-connect/auth"))
    assert signon.local_sign_in(LocalWorker(request)) == {"changed": False, "local": False}
    monkeypatch.setattr(signon, "probe_answer", lambda *unused: "tls")
    assert signon.local_sign_in(LocalWorker(request)) == {"changed": False, "local": False}
    with pytest.raises(ceph_native.Refusal, match="^dashboard-drift$"):
        signon.local_sign_in(LocalWorker(request, verified=False))
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        signon.local_sign_in(LocalWorker(request, enabled=True))


def test_the_dependents_and_baselines_are_closed():
    rows = [{"daemon_type": "grafana", "daemon_id": "node-1", "last_configured": BEFORE}, {"daemon_type": "mgmt-gateway", "daemon_id": "node-1"},
            {"daemon_type": "osd", "daemon_id": "0", "last_configured": BEFORE}]
    assert signon.dependents(rows) == [{"configured": BEFORE, "name": "grafana.node-1"}, {"configured": "", "name": "mgmt-gateway.node-1"}]
    with pytest.raises(ceph_native.Refusal, match="^native-output$"):
        signon.dependents([{"daemon_type": "grafana", "daemon_id": "node-1", "last_configured": "yesterday"}])
    for bad in ([BASELINE[1], BASELINE[0]], [dict(BASELINE[0], extra=1)], [BASELINE[0], BASELINE[0]], "x"):
        with pytest.raises(ceph_native.Refusal):
            signon.baseline(bad)


def test_the_suspension_evidence_is_closed_and_publishes():
    value = {"version": ceph_evidence.SIGNON_SUSPENSION_EVIDENCE, "request": DIGEST, "state": "prepared", "changed": False, "baseline": BASELINE}
    assert ceph_evidence.suspension_evidence(dict(value), DIGEST) == value
    for change in ({"request": "e" * 64}, {"state": "resumed"}, {"extra": 1}, {"state": "suspended"}, {"changed": "no"}):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_evidence.suspension_evidence(dict(value, **change), DIGEST)
