"""The monitoring service unit: bound credentials reach only the apply stream, singleton daemons are tolerated only where listed, and the
prometheus manager module is observed, never enabled.

The scripted monitor answers the upstream v20.2.1 shapes the worker reads, beside those test_ceph_service_units documents:
- cephadm names a monitoring service by its type and each daemon `<type>.<host short name>` without a suffix
  (src/pybind/mgr/cephadm/module.py:922-966), so `orch ps` lists `daemon_id` as the host's short name;
- GrafanaSpec keeps `initial_admin_password` and SNMPGatewaySpec a `credentials` object of `snmp_community` or
  `snmp_v3_auth_username`, `snmp_v3_auth_password` and `snmp_v3_priv_password` in the stored specification
  (src/python-common/ceph/deployment/service_spec.py:2559-2627, :2679-2830); an export that omits them is admitted too;
- `mgr dump` reports `available` and `mgr services` the active manager's module endpoints, answered by the MON from the MgrMap
  (src/mon/MgrMonitor.cc:1085-1094); cephadm enables the prometheus module itself for a prometheus service
  (src/pybind/mgr/cephadm/services/monitoring.py:480-489), after which the MON respawns the active manager;
- a monitoring daemon's directory is 0700 and owned by its image's user, while cephadm writes `unit.meta` as root
  (src/cephadm/cephadm.py:483-494, :601-660, :1338-1341).
"""

from __future__ import annotations

import contextlib
import copy
import json
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_native, ceph_native_observability as observability, ceph_native_observe as observe, ceph_native_units,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

FSID = units.FSID
REGISTRY = "registry.example.test/ceph/"
AUXILIARY = [{"name": name, "image": REGISTRY + name + "@sha256:" + digit * 64}
             for name, digit in (("alertmanager", "1"), ("grafana", "2"), ("node_exporter", "3"), ("prometheus", "4"), ("snmp_gateway", "5"))]
GRAFANA_PASSWORD = "grafana-admin-sentinel-Q7v"
AUTH_USERNAME = "snmp-user-sentinel-K2m"
AUTH_PASSWORD = "snmp-auth-sentinel-P9x"
PRIV_PASSWORD = "snmp-priv-sentinel-Z4w"
MATERIAL = {"grafanaAdminPassword": GRAFANA_PASSWORD, "snmpAuthPassword": AUTH_PASSWORD, "snmpAuthUsername": AUTH_USERNAME,
            "snmpPrivPassword": PRIV_PASSWORD}
SECRETS = tuple(MATERIAL.values())
FOREIGN = "foreign-credential-sentinel-R3t"
WEBHOOKS = ["https://alerts.example.test/hook", "https://pager.example.test/hook"]
TWO = ["node-1", "node-2"]


def documents():
    return [
        {"placement": {"count": 1, "hosts": list(TWO)}, "service_type": "alertmanager", "spec": {"user_data": {"default_webhook_urls": list(WEBHOOKS)}}},
        {"placement": {"hosts": list(TWO)}, "service_type": "ceph-exporter", "spec": {"prio_limit": 5, "stats_period": 10}},
        {"placement": {"count": 1, "hosts": list(TWO)}, "service_type": "grafana", "spec": {"anonymous_access": False, "protocol": "https"}},
        {"placement": {"hosts": list(TWO)}, "service_type": "node-exporter", "spec": {"port": 9100}},
        {"placement": {"count": 1, "hosts": list(TWO)}, "service_type": "prometheus", "spec": {"retention_time": "15d"}},
        {"placement": {"count": 1, "hosts": list(TWO)}, "service_type": "snmp-gateway",
         "spec": {"auth_protocol": "SHA", "engine_id": "8000c53f00000000", "privacy_protocol": "AES", "snmp_destination": "192.0.2.80:162",
                  "snmp_version": "V3"}},
    ]


def bindings():
    return [{"path": "/spec/initial_admin_password", "secretRef": "grafana-admin", "serviceRef": "grafana"},
            {"path": "/spec/credentials/snmp_v3_auth_password", "secretRef": "snmp-auth", "serviceRef": "snmp-gateway"},
            {"path": "/spec/credentials/snmp_v3_auth_username", "secretRef": "snmp-auth", "serviceRef": "snmp-gateway"},
            {"path": "/spec/credentials/snmp_v3_priv_password", "secretRef": "snmp-priv", "serviceRef": "snmp-gateway"}]


def monitoring_request(unit_documents=None):
    unit_documents = documents() if unit_documents is None else unit_documents
    request = dict(units.cluster(), action="service", service={"bindings": bindings(), "documents": unit_documents, "id": "", "type": "monitoring"})
    request["delegated"] = [{"name": document["service_type"], "hosts": document["placement"]["hosts"]} for document in unit_documents]
    request["auxiliary"] = copy.deepcopy(AUXILIARY)
    return request


def frozen(request, kind):
    name = observability.IMAGES[kind]
    return request["image"] if name is None else {entry["name"]: entry["image"] for entry in request["auxiliary"]}[name]


def daemon(request, kind, host, image=None, status=1):
    return {"daemon_type": kind, "daemon_id": host, "hostname": host, "service_name": kind, "status": status,
            "container_image_digests": [image or frozen(request, kind)]}


def running(request):
    rows = []
    for document in request["service"]["documents"]:
        hosts = document["placement"]["hosts"]
        rows.extend(daemon(request, document["service_type"], host) for host in hosts[:document["placement"].get("count", len(hosts))])
    return rows


def without_leaves(value):
    value = copy.deepcopy(value)
    value.get("spec", {}).pop("initial_admin_password", None)
    value.get("spec", {}).pop("credentials", None)
    return value


class Monitor:
    """A scripted monitor: reads answer from its tables; `orch apply` stores its documents as cephadm keeps them."""

    def __init__(self, request, specs=None, rows=None, omit=False):
        self.request = request
        self.config = units.held_images(request)
        self.specs = dict(specs or {})
        self.running = list(rows or [])
        self.omit = omit
        self.manager_services = {"dashboard": "https://192.0.2.1:8443/", "prometheus": "http://192.0.2.1:9283/"}
        self.unavailable = 0
        self.failures = {}
        self.before_store = self.after_store = None
        self.log, self.mutations, self.stdin = [], [], []

    def exported(self):
        return [dict(without_leaves(value) if self.omit else value, service_name=name) for name, value in self.specs.items()]

    def run(self, argv, data=b"", timeout=60):
        del timeout
        command = tuple(argv[argv.index("--format") + 2:])
        self.log.append(list(argv))
        if self.failures.get(command):
            self.failures[command] -= 1
            return 1, b"Error ENOTSUP: Module 'orchestrator' is not enabled/loaded"
        if command == ("mgr", "dump"):
            self.unavailable = max(self.unavailable - 1, -1)
            return 0, json.dumps({"active_name": "node-1.abcdef", "available": self.unavailable < 0, "standbys": []}).encode()
        answers = {("fsid",): {"fsid": FSID}, ("config", "dump"): self.config, ("orch", "ls", "--export"): self.exported(),
                   ("orch", "ps"): self.running, ("mgr", "services"): self.manager_services}
        if command in answers:
            return 0, json.dumps(answers[command]).encode()
        self.mutations.append(command)
        if command == ("orch", "apply", "-i", "-"):
            self.stdin.append(data)
            if self.before_store is not None:
                raise self.before_store
            for part in data.split(b"---\n"):
                value = json.loads(part)
                self.specs[ceph_native_units.service_name(value)] = value
            if self.after_store is not None:
                raise self.after_store
        return 0, b""


@pytest.fixture(autouse=True)
def normalization(monkeypatch):
    """Identity normalization in place of the frozen image's SDK, recording every argument it received."""
    arguments = []

    def contained(self, image, user, script, arguments_=(), **options):
        del self, image, user, script, options
        arguments.extend(arguments_)
        return json.loads(arguments_[0])

    monkeypatch.setattr(ceph_native.Worker, "runtime_profile", lambda self: {"imageID": "a" * 64, "uid": 167, "gid": 167})
    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", lambda self, image, user, script, arguments=(), **options: contained(
        self, image, user, script, arguments, **options))
    monkeypatch.setattr(ceph_native.Worker, "bootstrap_hold", lambda self: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "transport_hold", lambda self, settle=False: contextlib.nullcontext())
    monkeypatch.setattr(ceph_native.Worker, "cache_hold", lambda self: contextlib.nullcontext())
    return arguments


def applying(monitor, markers, material=None):
    native = units.worker(monitor, markers)
    native.bound = ceph_native_units.material(monitor.request, "unit", dict(MATERIAL if material is None else material))
    return native


def observing(monitor, markers):
    native = units.worker(monitor, markers)
    native.bound = ceph_native_units.material(monitor.request, "observe", {})
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def recorded(markers):
    found = b""
    for directory, unused, names in os.walk(markers.path):
        for name in names:
            with open(os.path.join(directory, name), "rb") as handle:
                found += handle.read()
    return found


def stream(data):
    return {ceph_native_units.service_name(value): value for value in (json.loads(part) for part in data.split(b"---\n"))}


def test_secret_leaves_reach_stdin_only(tmp_path, normalization):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request)
    assert applying(monitor, markers).unit() == {"changed": True}
    assert monitor.mutations == [("orch", "apply", "-i", "-")] and len(monitor.stdin) == 1
    sent = stream(monitor.stdin[0])
    assert sent["grafana"]["spec"]["initial_admin_password"] == GRAFANA_PASSWORD
    assert sent["snmp-gateway"]["spec"]["credentials"] == {"snmp_v3_auth_password": AUTH_PASSWORD, "snmp_v3_auth_username": AUTH_USERNAME,
                                                           "snmp_v3_priv_password": PRIV_PASSWORD}
    assert all(secret.encode() in monitor.stdin[0] for secret in SECRETS)
    held = json.dumps(normalization)
    assert "bootwright-bound:/spec/initial_admin_password" in held and "bootwright-bound:/spec/credentials/snmp_v3_priv_password" in held
    monitor.running = running(request)
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is True and blocking == []
    exposed = [json.dumps(monitor.log), held, recorded(markers).decode(), json.dumps(evidence)]
    assert not any(secret in item for secret in SECRETS for item in exposed)
    assert markers.read("service-grafana-complete.json")["service"]["spec"]["initial_admin_password"] == "bootwright-bound:/spec/initial_admin_password"
    monitor.specs["grafana"]["spec"]["initial_admin_password"] = FOREIGN
    with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$") as caught:
        applying(monitor, markers).unit()
    assert not any(secret in json.dumps(caught.value.native_error) for secret in SECRETS + (FOREIGN,))


def test_alertmanager_document_carries_default_webhook_urls(tmp_path):
    request = monitoring_request()
    monitor = Monitor(request)
    applying(monitor, units.markers_for(tmp_path, request)).unit()
    assert stream(monitor.stdin[0])["alertmanager"]["spec"]["user_data"] == {"default_webhook_urls": WEBHOOKS}


def test_monitoring_unit_refuses_an_absent_image_setting_before_any_write(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    before = units.records(markers)
    monitor = Monitor(request)
    grafana = "mgr/cephadm/container_image_grafana"
    monitor.config = [entry for entry in monitor.config if entry["name"] != grafana]
    with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
        applying(monitor, markers).unit()
    assert caught.value.native_error["cause"] == grafana
    assert monitor.mutations == [] and monitor.stdin == [] and units.records(markers) == before
    exporter = monitoring_request([documents()[1]])
    del exporter["service"]["bindings"]
    monitor = Monitor(exporter)
    monitor.config = []
    assert applying(monitor, units.markers_for(tmp_path / "exporter", exporter), {}).unit() == {"changed": True}
    assert not any(argv[argv.index("--format") + 2:] == ["config", "dump"] for argv in monitor.log)


@pytest.mark.parametrize("stored", [True, False])
def test_monitoring_apply_continuation_reuses_material(tmp_path, stored):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request)
    lost = ceph_native.Refusal("native-timeout")
    if stored:
        monitor.after_store = lost
    else:
        monitor.before_store = lost
    with pytest.raises(ceph_native.Refusal, match="^native-timeout$"):
        applying(monitor, markers).unit()
    assert not any(name.endswith("-complete.json") for name in os.listdir(markers.path))
    monitor.after_store = monitor.before_store = None
    assert applying(monitor, markers).unit() == {"changed": not stored}
    assert len(monitor.stdin) == (1 if stored else 2) and monitor.stdin[-1] == monitor.stdin[0]
    names = [observability.MONITORING_TYPES[index] for index in range(6)]
    assert all(markers.read("service-" + name + "-complete.json") == markers.read("service-" + name + "-attempt.json") for name in names)
    assert applying(monitor, markers).unit() == {"changed": False} and len(monitor.stdin) == (1 if stored else 2)


def test_stored_credential_mismatch_refuses_service_credential_foreign(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request)
    applying(monitor, markers).unit()
    for change in ("other", "partial", "empty"):
        monitor.specs = {name: copy.deepcopy(value) for name, value in monitor.specs.items()}
        spec = monitor.specs["snmp-gateway"]["spec"]
        spec["credentials"] = {"snmp_v3_auth_password": AUTH_PASSWORD, "snmp_v3_auth_username": AUTH_USERNAME, "snmp_v3_priv_password": PRIV_PASSWORD}
        if change == "other":
            spec["credentials"]["snmp_v3_auth_password"] = FOREIGN
        elif change == "partial":
            del spec["credentials"]["snmp_v3_priv_password"]
        else:
            spec["credentials"]["snmp_v3_auth_username"] = ""
        before = len(monitor.mutations)
        with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$") as caught:
            applying(monitor, markers).unit()
        assert len(monitor.mutations) == before
        detail = json.dumps(caught.value.native_error)
        assert "snmp-gateway /spec/credentials/" in detail and not any(secret in detail for secret in SECRETS + (FOREIGN,))
        if change == "other":
            monitor.running = running(request)
            assert observing(monitor, markers)[0] is True
        else:
            with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$"):
                observing(monitor, markers)


def test_export_without_bound_leaves_is_accepted(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request, omit=True)
    assert applying(monitor, markers).unit() == {"changed": True}
    assert all(name + "-complete.json" in os.listdir(markers.path) for name in ("service-grafana", "service-snmp-gateway"))
    assert applying(monitor, markers).unit() == {"changed": False} and len(monitor.stdin) == 1
    monitor.running = running(request)
    assert observing(monitor, markers)[0] is True


@pytest.mark.parametrize("name,leaves", [
    ("grafana", {"initial_admin_password": FOREIGN}),
    ("grafana", {"initial_admin_password": ""}),
    ("snmp-gateway", {"credentials": {"snmp_v3_auth_username": AUTH_USERNAME}}),
])
def test_foreign_service_with_its_own_credential_refuses_foreign_service(tmp_path, name, leaves):
    request = monitoring_request()
    document = copy.deepcopy(next(item for item in documents() if item["service_type"] == name))
    document["spec"].update(leaves)
    monitor = Monitor(request, specs={name: document})
    markers = units.markers_for(tmp_path / "apply", request)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
        applying(monitor, markers).unit()
    assert not isinstance(caught.value, ceph_native.NativeFailure) and monitor.mutations == []
    assert not any(entry.endswith("-attempt.json") for entry in os.listdir(markers.path))
    for held in (markers, units.markers_for(tmp_path / "observe", request)):
        with pytest.raises(ceph_native.Refusal, match="^foreign-service$") as caught:
            observing(monitor, held)
        assert not isinstance(caught.value, ceph_native.NativeFailure)


def test_prometheus_module_observed_through_mgr_services(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request, rows=running(request))
    monitor.manager_services = {"dashboard": "https://192.0.2.1:8443/"}
    applying(monitor, markers).unit()
    ready, blocking, unused = observing(monitor, markers)
    assert ready is False and blocking == ["module@prometheus"]
    monitor.manager_services["prometheus"] = ""
    assert observing(monitor, markers)[:2] == (False, ["module@prometheus"])
    monitor.manager_services["prometheus"] = "http://192.0.2.1:9283/"
    monitor.unavailable = 1
    assert observing(monitor, markers)[:2] == (False, ["module@prometheus"])
    assert observing(monitor, markers)[:2] == (True, [])
    assert monitor.mutations == [("orch", "apply", "-i", "-")]
    assert not any("module" in argv for argv in monitor.log)


def test_unit_waits_for_available_mgr_after_module_respawn(tmp_path, monkeypatch):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request, rows=running(request))
    applying(monitor, markers).unit()

    def observe_cluster(self, evidence, daemons):
        del daemons
        evidence["exists"] = True
        evidence["ready"] = self.observe_resource(evidence, True)
        evidence["partial"] = not evidence["ready"]
        return evidence

    monkeypatch.setattr(ceph_native.Worker, "daemons", lambda self: [{"fsid": FSID, "name": "mgr.node-1.abcdef"}])
    monkeypatch.setattr(ceph_native.Worker, "observe_cluster", observe_cluster)

    class Transport:
        calls = 0

        def call(self, host, phase, **options):
            assert host["name"] == "node-1" and phase == "observe" and set(options) <= {"timeout"}
            self.calls += 1
            native = units.worker(monitor, markers)
            native.bound = ceph_native_units.material(request, phase, {})
            return dict(native.observe(), blocking=list(native.blocking))

    monitor.failures = {("orch", "ls", "--export"): 2, ("orch", "ps"): 1}
    monitor.unavailable = 3
    clock = iter(range(0, 100000, 5))
    transport = Transport()
    observed = ceph_core.wait_unit(request, "d" * 64, request["hosts"][0], transport, lambda: next(clock), lambda seconds: None)
    assert observed["ready"] is True and transport.calls == 7
    assert {item["service"] for item in observed["daemons"]} == set(observability.MONITORING_TYPES)


def test_observe_refuses_foreign_monitoring_daemon():
    request = monitoring_request([document for document in documents() if document["service_type"] != "grafana"])
    row = {"daemon_type": "grafana", "daemon_id": "node-1", "hostname": "node-1", "service_name": "grafana"}
    assert ceph_native.DAEMON_TYPES["grafana"] == ("delegated", "delegated", False, "none")
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        ceph_native.delegated_daemon(row, request)
    listed = monitoring_request()
    ceph_native.delegated_daemon(row, listed)
    for bad in (dict(row, hostname="node-3"), dict(row, service_name="prometheus"), dict(row, daemon_type="node-exporter")):
        with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
            ceph_native.delegated_daemon(bad, listed)
    assert "loki" not in ceph_native.DAEMON_TYPES and "oauth2-proxy" not in ceph_native.DAEMON_TYPES


def test_observe_refuses_wrong_aux_digest(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request, rows=running(request))
    applying(monitor, markers).unit()
    assert observing(monitor, markers)[0] is True
    for kind, image in (("prometheus", REGISTRY + "prometheus@sha256:" + "9" * 64), ("ceph-exporter", frozen(request, "prometheus"))):
        monitor.running = [dict(row, container_image_digests=[image]) if row["daemon_type"] == kind else row for row in running(request)]
        with pytest.raises(ceph_native.Refusal, match="^artifact$"):
            observing(monitor, markers)


def test_observe_partial_when_count_short(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request)
    applying(monitor, markers).unit()
    rows = running(request)
    monitor.running = [row for row in rows if not (row["daemon_type"] == "node-exporter" and row["hostname"] == "node-2")]
    ready, blocking, unused = observing(monitor, markers)
    assert ready is False and blocking == ["node-exporter@node-exporter"]
    monitor.running = rows + [daemon(request, "grafana", "node-2")]
    ready, blocking, unused = observing(monitor, markers)
    assert ready is False and blocking == ["grafana@grafana"]
    monitor.running = rows
    assert observing(monitor, markers)[:2] == (True, [])


def test_orch_ls_spec_never_leaves_worker(tmp_path):
    request = monitoring_request()
    markers = units.markers_for(tmp_path, request)
    monitor = Monitor(request, rows=running(request))
    applying(monitor, markers).unit()
    monitor.specs["rgw.foreign"] = {"service_type": "rgw", "service_id": "foreign", "placement": {"hosts": ["node-3"]},
                                    "spec": {"rgw_frontend_ssl_certificate": FOREIGN}}
    ready, unused, evidence = observing(monitor, markers)
    assert ready is True and set(evidence["daemons"][0]) == {"host", "name", "service"}
    published = json.dumps(evidence)
    assert not any(secret in published for secret in SECRETS + (FOREIGN,))
    assert "initial_admin_password" not in published and "credentials" not in published and "user_data" not in published


class Owned:
    """A stat result whose owner reads as another user's, for a layout this unprivileged test cannot chown."""

    def __init__(self, info, uid):
        self.info, self.st_uid = info, uid

    def __getattr__(self, name):
        return getattr(self.info, name)


@pytest.fixture
def layout(tmp_path, monkeypatch):
    """A managed host layout under a fixed root: the cluster directory and a daemon directory per case, owners overridden by inode."""
    root = tmp_path / "ceph"
    root.mkdir(mode=0o700)
    monkeypatch.setattr(observe, "ROOT_UID", os.getuid())
    monkeypatch.setattr(observe, "SETTLE_PAUSE", 0)

    @contextlib.contextmanager
    def root_directory(uid=None):
        del uid
        descriptor = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            yield descriptor
        finally:
            os.close(descriptor)

    monkeypatch.setattr(observe, "root_directory", root_directory)
    owners = {}
    real_stat, real_fstat = os.stat, os.fstat

    def owned(info):
        return Owned(info, owners[(info.st_dev, info.st_ino)]) if (info.st_dev, info.st_ino) in owners else info

    monkeypatch.setattr(os, "stat", lambda *args, **options: owned(real_stat(*args, **options)))
    monkeypatch.setattr(os, "fstat", lambda descriptor: owned(real_fstat(descriptor)))
    request = {"fsid": FSID, "image": "registry.example.test/ceph@sha256:" + "a" * 64}
    (root / FSID).mkdir(mode=0o700)
    return root / FSID, request, owners


def place(cluster, name, service, image, mode=0o700):
    directory = cluster / name
    directory.mkdir(mode=0o700)
    (directory / "unit.meta").write_text(json.dumps({"service_name": service}))
    (directory / "unit.image").write_text(image + "\n")
    directory.chmod(mode)
    return directory


def not_loaded(fsid):
    def command(argv, **options):
        del options
        if argv[0] == "/usr/bin/systemctl":
            unit = argv[-1]
            return ("Id=" + unit + "\nLoadState=not-found\nActiveState=inactive\nSubState=dead\nUnitFileState=\nFragmentPath=\nMainPID=0\n").encode()
        assert argv[:2] == ["/usr/bin/podman", "ps"] and fsid in argv[-1]
        return []
    return command


def inode(path):
    info = os.lstat(path)
    return info.st_dev, info.st_ino


@pytest.mark.parametrize("case", ["listed", "unlisted", "group-writable", "unit-meta-owner", "other-kind"])
def test_host_layout_admits_listed_monitoring_daemon(layout, case):
    cluster, request, owners = layout
    image = frozen({"auxiliary": AUXILIARY}, "prometheus")
    if case == "other-kind":
        directory = place(cluster, "haproxy.rgw.objects.node-1.abcdef", "ingress.rgw.objects", image)
        request["delegated"] = [{"name": "ingress.rgw.objects", "hosts": ["node-1"]}]
    else:
        directory = place(cluster, "prometheus.node-1", "prometheus", image, mode=0o770 if case == "group-writable" else 0o700)
        request["delegated"] = [] if case == "unlisted" else [{"name": "prometheus", "hosts": ["node-1"]}]
    owners[inode(directory)] = 65534
    if case == "unit-meta-owner":
        owners[inode(directory / "unit.meta")] = 65534
    if case != "listed":
        with pytest.raises(observe.ObservationRefusal):
            observe.daemons(request, 167, not_loaded(FSID), owned=True, host="node-1")
        return
    result = observe.daemons(request, 167, not_loaded(FSID), owned=True, host="node-1")
    assert [item["name"] for item in result] == ["prometheus.node-1"] and result[0]["container_image_name"] == image
    assert stat.S_IMODE(os.lstat(directory).st_mode) == 0o700


def test_monitoring_tables_agree():
    types = set(observability.MONITORING_TYPES)
    assert list(observability.MONITORING_TYPES) == sorted(types) and observability.DAEMON_TYPES == observability.MONITORING_TYPES
    gateways = set(observability.GATEWAY_TYPES)
    assert set(observability.IMAGES) == types == set(observability.MEMBERS) and observe.LISTED_SINGLETONS == types | gateways | {"rbd-mirror"}
    assert observe.SINGLETONS == types - {"ceph-exporter"} | gateways and all(observe.DELEGATED_TYPES[kind] == kind for kind in observe.SINGLETONS)
    assert all(ceph_native.DAEMON_TYPES[kind] == ("delegated", "delegated", False, "none") for kind in observe.SINGLETONS)
    assert ceph_native.DAEMON_TYPES["ceph-exporter"] == ("cluster", "request", True, "ceph") and "ceph-exporter" in observe.CLUSTER_TYPES
    assert all(ceph_native.DELEGATED_SERVICE.fullmatch(kind) for kind in types)
    assert not any(ceph_native.DELEGATED_SERVICE.fullmatch(name) for name in ("grafana.main", "mgmt-gateway.main", "oauth2-proxy", "loki", "prometheus.x"))
    assert {AUXILIARY_NAME for AUXILIARY_NAME in observability.IMAGES.values() if AUXILIARY_NAME} <= set(ceph_native.AUXILIARY_NAMES)
    monitoring = ceph_native_units.UNIT_TYPES["monitoring"]
    assert monitoring.singletons == types and monitoring.endpoint is None
    assert monitoring.daemon_types == observability.MONITORING_TYPES and monitoring.images == observability.IMAGES
    leaves = observability.MONITORING_LEAVES + observability.GATEWAY_LEAVES
    assert ceph_native_units.MATERIAL_VARIABLES == {variable for unused, unused_path, variable in leaves}
    assert all(ceph_native_units.PENDING.fullmatch(entry) for entry in ("module@prometheus", "grafana@grafana", "nfs@nfs.files", "vip@ingress.nfs.files"))
    assert not any(ceph_native_units.PENDING.fullmatch(entry) for entry in ("module@grafana.main", "grafana@loki", "module@nfs"))
    assert ceph_native_units.UNIT_TYPES["nfs"].singletons == frozenset() and ceph_native_units.UNIT_TYPES["nfs"].leaves == {}
    assert "service-credential-foreign" in ceph_core.CLUSTER_REASONS


INVALID = {
    "service_id on a singleton": lambda request: request["service"]["documents"][0].update(service_id="main"),
    "id on the unit": lambda request: request["service"].update(id="main"),
    "types out of name order": lambda request: request["service"]["documents"].reverse(),
    "member outside the record": lambda request: request["service"]["documents"][3]["spec"].update(targets=["192.0.2.9:9100"]),
    "inline credential": lambda request: request["service"]["documents"][2]["spec"].update(initial_admin_password="inline"),
    "user_data key": lambda request: request["service"]["documents"][0]["spec"].update(user_data={"default_webhook_urls": WEBHOOKS, "x": 1}),
    "empty webhooks": lambda request: request["service"]["documents"][0]["spec"].update(user_data={"default_webhook_urls": []}),
    "snmp version": lambda request: request["service"]["documents"][5]["spec"].update(snmp_version="V1"),
    "stats period": lambda request: request["service"]["documents"][1]["spec"].update(stats_period=0),
    "port": lambda request: request["service"]["documents"][3]["spec"].update(port=70000),
    "binding to an undeclared service": lambda request: request["service"]["bindings"][0].update(serviceRef="loki"),
    "binding to an unrealized leaf": lambda request: request["service"]["bindings"][0].update(path="/spec/protocol"),
    "bindings out of order": lambda request: request["service"]["bindings"].reverse(),
    "unselected auxiliary": lambda request: request.update(auxiliary=AUXILIARY[:3]),
    "networks on ceph-exporter": lambda request: request["service"]["documents"][1].update(networks=["192.0.2.0/24"]),
    "networks on snmp-gateway": lambda request: request["service"]["documents"][5].update(networks=["192.0.2.0/24"]),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_monitoring_request_validation(case):
    request = monitoring_request()
    assert ceph_native.validate(copy.deepcopy(request)) is not None
    INVALID[case](request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


def test_monitoring_service_networks_validate_where_cephadm_has_the_field():
    request = monitoring_request()
    for index in (0, 2, 3, 4):
        request["service"]["documents"][index]["networks"] = ["192.0.2.0/24"]
    assert ceph_native.validate(request) is not None


def test_unit_material_reaches_only_the_apply_phase():
    request = monitoring_request()
    assert ceph_native_units.material(request, "unit", dict(MATERIAL)) == MATERIAL
    for phase, private in (("observe", dict(MATERIAL)), ("unit", {}), ("unit", dict(MATERIAL, extra="x")), ("unit", dict(MATERIAL, snmpPrivPassword="")),
                           ("unit", dict(MATERIAL, snmpPrivPassword="a\x00b")), ("unit", dict(MATERIAL, snmpPrivPassword="a" * 4097))):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            ceph_native_units.material(request, phase, private)
    assert ceph_native_units.material(request, "observe", {}) == {}


class MaterialTransport(units.Transport):
    """The coordinator's view: which private material each phase received."""

    def __init__(self, request, **options):
        rows = [{"host": "node-1", "name": "grafana.node-1", "service": "grafana"},
                {"host": "node-2", "name": "ceph-exporter.node-2", "service": "ceph-exporter"}]
        super().__init__(request, observed={"daemons": rows}, **options)
        self.private = []

    def unit_material(self, phase="unit"):
        return dict(MATERIAL) if phase == "unit" else {}

    def call(self, host, phase, private=None, *, timeout=None):  # pylint: disable=arguments-renamed
        self.private.append((phase, sorted(private or {})))
        return super().call(host, phase, timeout=timeout)


def test_execute_unit_passes_bound_material_to_its_apply_phase_only(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = monitoring_request()
    transport = MaterialTransport(request, ready_after=2)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["ready"] is True and [row["service"] for row in result["evidence"]["daemons"]] == ["grafana", "ceph-exporter"]
    assert transport.private == [("prerequisites", []), ("unit", sorted(MATERIAL)), ("observe", []), ("observe", [])]
    for operation, observed in (("observe", {}), ("destroy", {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": []})):
        transport = MaterialTransport(request)
        transport.observed.update(observed)
        ceph_core.execute(request, operation, "d" * 64, transport)
        assert all(private == [] for unused, private in transport.private)
    unbound = monitoring_request()
    del unbound["service"]["bindings"]
    transport = units.Transport(unbound, observed={"daemons": []})
    ceph_core.execute(unbound, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert [phase for unused, phase in transport.calls] == ["prerequisites", "unit", "observe"]


def test_transport_admits_the_unit_material(tmp_path):
    def write(name, data=None):
        path = str(tmp_path / name)
        ceph_native.private_write(path, name.encode() if data is None else data)
        return path

    request = monitoring_request()
    material = {"sshKey": write("identity"), "knownHosts": write("known", b"192.0.2.1 ssh-ed25519 PUBLIC")}
    material.update({name: write(name) for name in MATERIAL})
    calls = []

    def run(argv, data=b"", timeout=None):
        del timeout
        calls.append((argv, data))
        return 0, json.dumps({"ok": True, "result": {"changed": False}}).encode()

    transport = ceph_core.Transport(request, material, run=run)
    assert transport.unit_material() == {name: name for name in MATERIAL}
    assert transport.call(request["hosts"][0], "unit", transport.unit_material()) == {"changed": False}
    assert not any(name in " ".join(argv) for argv, unused in calls for name in MATERIAL)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.Transport(request, dict(material, grafanaViewerPassword=write("viewer")), run=run).call(request["hosts"][0], "observe")
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.Transport(request, {key: value for key, value in material.items() if key != "snmpPrivPassword"}, run=run).unit_material()
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_core.Transport(request, dict(material, snmpPrivPassword=write("latin", b"\xff\xfe")), run=run).unit_material()


def test_dispatch_lends_the_bound_material_to_the_unit_phase_only(monkeypatch):
    reached = []
    monkeypatch.setattr(ceph_native_units.UnitWorker, "unit", lambda self: reached.append(self.bound) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = monitoring_request()
    assert ceph_native.dispatch({"request": request, "host": "node-1", "phase": "unit", "private": dict(MATERIAL)}) == {"changed": False}
    assert reached == [MATERIAL]
    for phase, private in (("unit", {}), ("observe", dict(MATERIAL)), ("prerequisites", {"grafanaAdminPassword": GRAFANA_PASSWORD})):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": private})
