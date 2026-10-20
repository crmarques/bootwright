"""The management gateway service unit: its keepalive-only ingress first in one apply stream, its TLS leaves bound to that stream only,
and readiness proved by the leaf the endpoint serves.

The scripted monitor is the monitoring unit's, answering the upstream v20.2.1 shapes the worker reads:
- MgmtGatewaySpec carries `port`, `ssl`, `ssl_cert`, `ssl_key`, `ssl_protocols` (TLSv1.2 or TLSv1.3), `ssl_ciphers`, `enable_auth`,
  `enable_health_check_endpoint` and `virtual_ip` (src/python-common/ceph/deployment/service_spec.py, MgmtGatewaySpec), and its daemons
  run `container_image_nginx`;
- the gateway's virtual IP needs a keepalive-only ingress whose `backend_service` is `mgmt-gateway`
  (https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=gateway-enabling-high-availability-ceph-management);
- the fixture names the singleton service `mgmt-gateway` and its daemons `mgmt-gateway.<host short name>` by cephadm's singleton
  naming rule (src/pybind/mgr/cephadm/module.py:922-966), a layout not yet observed on a live host;
- `orch ps` rows carry `daemon_type`, `daemon_id`, `hostname`, `service_name`, `status`, `ip`, `ports` and `container_image_digests`
  (src/pybind/mgr/orchestrator/_interface.py:1471-1500).
The PEM fixtures wrap fixed bytes: the probe compares SHA-256 of the first block's DER bytes, so no real certificate is needed.
"""

from __future__ import annotations

import base64
import contextlib
import copy
import hashlib
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_native, ceph_native_observability as observability, ceph_native_observe as observe, ceph_native_units,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_settings as settings


def pem(kind, payload):
    """Synthetic PEM text around arbitrary bytes, in short lines no opaque-run redaction would catch."""
    body = base64.b64encode(payload).decode()
    return "-----BEGIN " + kind + "-----\n" + "\n".join(body[i:i + 24] for i in range(0, len(body), 24)) + "\n-----END " + kind + "-----\n"


NGINX = "registry.example.test/ceph/nginx@sha256:" + "d" * 64
AUXILIARY = [{"image": settings.KEEPALIVED, "name": "keepalived"}, {"image": NGINX, "name": "nginx"}]
LEAF = b"synthetic management gateway leaf certificate"
OTHER_LEAF = b"another leaf the Secret does not hold"
CERT = pem("CERTIFICATE", LEAF) + pem("CERTIFICATE", b"synthetic issuing authority")
KEY = pem("PRIVATE KEY", b"private key material only the gateway holds")
MATERIAL = {"mgmtGatewayCertificate": CERT, "mgmtGatewayKey": KEY}
SEEN = {"mgmtGatewayCertificate": CERT}
MARKS = [line for text in (CERT, KEY) for line in text.splitlines() if "-----" not in line]
LEAF_SHA256 = hashlib.sha256(LEAF).hexdigest()
VIP = "192.0.2.60"
PORT = 9443
TWO = ["node-1", "node-2"]


def documents(vip=VIP, ingress_vip=None):
    spec = {"enable_health_check_endpoint": True, "port": PORT, "ssl_ciphers": ["ECDHE-RSA-AES256-GCM-SHA384"], "ssl_protocols": ["TLSv1.2", "TLSv1.3"]}
    if vip is not None:
        spec["virtual_ip"] = vip
    gateway = {"placement": {"count": 2, "hosts": list(TWO)}, "service_type": "mgmt-gateway", "spec": spec}
    if vip is None:
        return [gateway]
    ingress = {"placement": {"count": 2, "hosts": list(TWO)}, "service_id": "mgmt", "service_type": "ingress",
               "spec": {"backend_service": "mgmt-gateway", "first_virtual_router_id": 60, "keepalive_only": True,
                        "virtual_ip": vip if ingress_vip is None else ingress_vip}}
    return [ingress, gateway]


def bindings():
    return [{"path": "/spec/ssl_cert", "secretRef": "mgmt-tls", "serviceRef": "mgmt-gateway"},
            {"path": "/spec/ssl_key", "secretRef": "mgmt-tls", "serviceRef": "mgmt-gateway"}]


def gateway_request(vip=VIP, bound=True, unit_documents=None):
    unit_documents = documents(vip) if unit_documents is None else unit_documents
    service = {"documents": unit_documents, "id": "", "type": "mgmt-gateway"}
    if bound:
        service["bindings"] = bindings()
    request = dict(units.cluster(), action="service", service=service)
    request["delegated"] = sorted(({"name": ceph_native_units.service_name(document), "hosts": document["placement"]["hosts"]}
                                   for document in unit_documents), key=lambda entry: entry["name"])
    request["auxiliary"] = copy.deepcopy(AUXILIARY)
    return request


def row(kind, service, host, image, **extra):
    return dict({"daemon_type": kind, "daemon_id": host, "hostname": host, "service_name": service, "status": 1, "container_image_digests": [image]},
                **extra)


def running(request):
    rows = [row("mgmt-gateway", "mgmt-gateway", "node-2", NGINX), row("mgmt-gateway", "mgmt-gateway", "node-1", NGINX, ip="192.0.2.11", ports=[PORT])]
    if len(request["service"]["documents"]) == 2:
        rows += [row("keepalived", "ingress.mgmt", host, settings.KEEPALIVED, daemon_id="mgmt." + host + ".abcdef") for host in TWO]
    return rows


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


class Probes:
    """The served leaf and status line each endpoint answers with, and every probe's target."""

    def __init__(self, monkeypatch, answers):
        self.answers, self.targets = answers, []
        monkeypatch.setattr(observability, "served", self.served)

    def served(self, address, number, name, *, host=None):
        self.targets.append((address, number, name, host))
        return self.answers[address] if isinstance(self.answers, dict) else self.answers


def applying(monitor, markers, material=None):
    native = units.worker(monitor, markers)
    native.bound = ceph_native_units.material(monitor.request, "unit", dict(MATERIAL if material is None else material))
    return native


def observing(monitor, markers, seen=None):
    native = units.worker(monitor, markers)
    held = (SEEN if monitor.request["service"].get("bindings") else {}) if seen is None else seen
    native.bound = ceph_native_units.material(monitor.request, "observe", dict(held))
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def applied(tmp_path, request):
    markers = units.markers_for(tmp_path, request)
    monitor = monitoring.Monitor(request)
    assert applying(monitor, markers, MATERIAL if request["service"].get("bindings") else {}).unit() == {"changed": True}
    monitor.running = running(request)
    return markers, monitor


def test_gateway_proof_compares_bound_leaf(tmp_path, monkeypatch):
    markers, monitor = applied(tmp_path, gateway_request())
    probes = Probes(monkeypatch, (OTHER_LEAF, True))
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is False and blocking == ["tls@mgmt-gateway"] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}
    assert probes.targets == [(VIP, PORT, VIP, None)]
    probes.answers = (LEAF, True)
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is True and blocking == [] and evidence["endpoint"] == {"certificateSHA256": LEAF_SHA256, "reachable": True}
    probes.answers = (LEAF, False)
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is False and blocking == ["http@mgmt-gateway"] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}
    probes.answers = (None, False)
    assert observing(monitor, markers)[:2] == (False, ["tls@mgmt-gateway"])
    v6 = "2001:db8::60"
    markers, monitor = applied(tmp_path / "v6", gateway_request(v6))
    probes = Probes(monkeypatch, (LEAF, True))
    assert observing(monitor, markers)[:2] == (True, []) and probes.targets == [(v6, PORT, v6, "[" + v6 + "]")]


def test_gateway_without_binding_reports_cephadm_issuer(tmp_path, monkeypatch):
    request = gateway_request(None, bound=False)
    markers, monitor = applied(tmp_path, request)
    first, second = b"cephadm-signed leaf for node-1", b"cephadm-signed leaf for node-2"
    probes = Probes(monkeypatch, {"192.0.2.11": (first, True), "192.0.2.2": (second, True)})
    ready, blocking, evidence = observing(monitor, markers)
    endpoint = {"certificateSHA256": hashlib.sha256(first).hexdigest(), "reachable": True}
    assert ready is True and blocking == [] and evidence["endpoint"] == endpoint
    assert probes.targets == [("192.0.2.11", PORT, "node-1", None), ("192.0.2.2", PORT, "node-2", None)]
    value = dict(evidence, request="d" * 64, ready=True, exists=True, hosts=["node-1"])
    assert ceph_core.evidence(copy.deepcopy(value), "d" * 64, request)["endpoint"] == endpoint
    for bad, against in ((dict(value, endpoint={"certificateSHA256": "", "reachable": True}), request),
                         (dict(value, endpoint={"certificateSHA256": endpoint["certificateSHA256"], "reachable": False}), request),
                         (value, None), (dict(value, daemons=[]), units.unit_request("direct"))):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64, against)
    probes.answers["192.0.2.2"] = (second, False)
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is False and blocking == ["http@mgmt-gateway"] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}
    monitor.running = [item for item in running(request) if item["hostname"] == "node-1"]
    monitor.running[0]["ports"] = [PORT + 1]
    ready, blocking, unused = observing(monitor, markers)
    assert ready is False and blocking == ["mgmt-gateway@mgmt-gateway", "http@mgmt-gateway"]


def test_gateway_document_applied_with_binding_first(tmp_path, normalization, monkeypatch):
    request = gateway_request()
    markers, monitor = applied(tmp_path, request)
    assert monitor.mutations == [("orch", "apply", "-i", "-")] and len(monitor.stdin) == 1
    parts = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")]
    assert [ceph_native_units.service_name(part) for part in parts] == ["ingress.mgmt", "mgmt-gateway"]
    assert parts[1]["spec"]["ssl_cert"] == CERT and parts[1]["spec"]["ssl_key"] == KEY and "ssl_cert" not in parts[0]["spec"]
    stored = monitoring.recorded(markers).decode()
    assert "bootwright-bound:/spec/ssl_cert" in stored and "bootwright-bound:/spec/ssl_key" in stored
    held = json.dumps(normalization)
    assert "bootwright-bound:/spec/ssl_key" in held
    assert not any(mark in item for mark in MARKS for item in (stored, held, json.dumps(monitor.log)))
    assert markers.read("service-mgmt-gateway-complete.json")["service"]["spec"]["ssl_key"] == "bootwright-bound:/spec/ssl_key"
    assert applying(monitor, markers).unit() == {"changed": False} and len(monitor.stdin) == 1
    reached = []
    monkeypatch.setattr(ceph_native_units.UnitWorker, "unit", lambda self: reached.append(self.bound) or {"changed": False})
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native.dispatch({"request": request, "host": "node-1", "phase": "unit", "private": dict(SEEN)})
    assert reached == []
    assert ceph_native.dispatch({"request": request, "host": "node-1", "phase": "unit", "private": dict(MATERIAL)}) == {"changed": False}
    assert reached == [MATERIAL]


def test_stored_gateway_leaf_of_another_value_refuses_service_credential_foreign(tmp_path, monkeypatch):
    markers, monitor = applied(tmp_path, gateway_request())
    Probes(monkeypatch, (LEAF, True))
    assert observing(monitor, markers)[0] is True
    monitor.specs["mgmt-gateway"] = copy.deepcopy(monitor.specs["mgmt-gateway"])
    monitor.specs["mgmt-gateway"]["spec"]["ssl_cert"] = pem("CERTIFICATE", OTHER_LEAF)
    with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$") as caught:
        observing(monitor, markers)
    with pytest.raises(ceph_native.NativeFailure, match="^service-credential-foreign$"):
        applying(monitor, markers).unit()
    assert len(monitor.stdin) == 1 and not any(mark in json.dumps(caught.value.native_error) for mark in MARKS)


INVALID_VIP = {
    "ingress at another address": lambda: gateway_request(unit_documents=documents(VIP, "192.0.2.61")),
    "non-canonical address on both": lambda: gateway_request("2001:DB8::60"),
    "non-canonical ingress address": lambda: gateway_request(unit_documents=documents("2001:db8::60", "2001:DB8::60")),
    "address with a prefix on both": lambda: gateway_request("192.0.2.60/24"),
    "address without its ingress": lambda: gateway_request(unit_documents=documents()[1:]),
    "ingress without an address": lambda: gateway_request(unit_documents=[documents()[0], documents(None)[0]]),
}


@pytest.mark.parametrize("case", sorted(INVALID_VIP))
def test_gateway_and_ingress_render_identical_canonical_vip(case):
    for vip in (VIP, "2001:db8::60", None):
        assert ceph_native.validate(gateway_request(vip)) is not None
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID_VIP[case]())


INVALID = {
    "service_id on the gateway": lambda request: request["service"]["documents"][1].update(service_id="main"),
    "id on the unit": lambda request: request["service"].update(id="main"),
    "single sign-on": lambda request: request["service"]["documents"][1]["spec"].update(enable_auth=True),
    "plaintext": lambda request: request["service"]["documents"][1]["spec"].update(ssl=False),
    "no port": lambda request: request["service"]["documents"][1]["spec"].pop("port"),
    "unknown protocol": lambda request: request["service"]["documents"][1]["spec"].update(ssl_protocols=["TLSv1.1"]),
    "repeated protocol": lambda request: request["service"]["documents"][1]["spec"].update(ssl_protocols=["TLSv1.3", "TLSv1.3"]),
    "empty ciphers": lambda request: request["service"]["documents"][1]["spec"].update(ssl_ciphers=[]),
    "member outside the record": lambda request: request["service"]["documents"][1]["spec"].update(server_tokens="off"),
    "inline leaf": lambda request: request["service"]["documents"][1]["spec"].update(ssl_cert=CERT),
    "ingress with haproxy": lambda request: request["service"]["documents"][0]["spec"].update(keepalive_only=False),
    "ingress for another backend": lambda request: request["service"]["documents"][0]["spec"].update(backend_service="rgw.objects"),
    "ingress frontend port": lambda request: request["service"]["documents"][0]["spec"].update(frontend_port=443),
    "one leaf bound": lambda request: request["service"].update(bindings=bindings()[:1]),
    "unselected nginx": lambda request: request.update(auxiliary=AUXILIARY[:1]),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_gateway_request_validation(case):
    request = gateway_request()
    assert ceph_native.validate(copy.deepcopy(request)) is not None
    INVALID[case](request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


def test_gateway_material_reaches_apply_and_observation_only_as_bound():
    request = gateway_request()
    assert ceph_native_units.material(request, "unit", dict(MATERIAL)) == MATERIAL
    for phase in ("observe", "destroy"):
        assert ceph_native_units.material(request, phase, dict(SEEN)) == SEEN
    assert ceph_native_units.material(request, "prerequisites", {}) == {}
    chain = pem("CERTIFICATE", LEAF) + pem("CERTIFICATE", bytes(range(256)) * 14)
    assert 5000 <= len(chain) <= observability.MAX_CERTIFICATE and len(chain) > ceph_native_units.MAX_MATERIAL
    assert ceph_native_units.material(request, "observe", {"mgmtGatewayCertificate": chain}) == {"mgmtGatewayCertificate": chain}
    oversized = pem("CERTIFICATE", LEAF) + pem("CERTIFICATE", bytes(52000))
    assert len(oversized) >= 70000
    for phase, private in (("observe", dict(MATERIAL)), ("unit", dict(SEEN)), ("prerequisites", dict(SEEN)), ("observe", {}),
                           ("observe", {"mgmtGatewayCertificate": oversized}), ("unit", dict(MATERIAL, mgmtGatewayKey="not a key")),
                           ("unit", dict(MATERIAL, mgmtGatewayCertificate="no certificate block")), ("unit", dict(MATERIAL, mgmtGatewayKey=KEY + "\x00"))):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            ceph_native_units.material(request, phase, private)
    assert ceph_native_units.material_names(gateway_request(bound=False), "unit") == frozenset()
    observed = monitoring.monitoring_request()
    assert ceph_native_units.material_names(observed, "unit") == set(monitoring.MATERIAL)
    assert all(ceph_native_units.material_names(observed, phase) == frozenset() for phase in ("observe", "destroy", "prerequisites"))


def test_transport_reads_each_phase_material(tmp_path):
    def write(name, data):
        path = str(tmp_path / name)
        ceph_native.private_write(path, data.encode())
        return path

    request = gateway_request()
    material = {"sshKey": write("identity", "key"), "knownHosts": write("known", "192.0.2.1 ssh-ed25519 PUBLIC"),
                "mgmtGatewayCertificate": write("certificate", CERT), "mgmtGatewayKey": write("private", KEY)}
    transport = ceph_core.Transport(request, material, run=lambda argv, data=b"", timeout=None: (0, b'{"ok": true, "result": {}}'))
    assert transport.unit_material("unit") == MATERIAL and transport.unit_material("observe") == SEEN and transport.unit_material() == MATERIAL
    assert transport.unit_material("prerequisites") == {}


class MaterialTransport(units.Transport):
    """The coordinator's view: which private material each phase received, and the endpoint each observation reports."""

    def __init__(self, request, endpoint, **options):
        rows = [{"host": "node-1", "name": "mgmt-gateway.node-1", "service": "mgmt-gateway"},
                {"host": "node-2", "name": "keepalived.mgmt.node-2.abcdef", "service": "ingress.mgmt"}]
        super().__init__(request, observed={"daemons": rows, "endpoint": endpoint}, **options)
        self.private = []

    def unit_material(self, phase="unit"):
        return {name: MATERIAL[name] for name in ceph_native_units.material_names(self.request, phase)}

    def call(self, host, phase, private=None, *, timeout=None):  # pylint: disable=arguments-renamed
        self.private.append((phase, sorted(private or {})))
        result = super().call(host, phase, timeout=timeout)
        return {"changed": False} if phase == "ingress_firewall" else result


def test_execute_unit_checks_the_bound_digest(monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = gateway_request()
    served = {"certificateSHA256": LEAF_SHA256, "reachable": True}
    transport = MaterialTransport(request, served, ready_after=2)
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["ready"] is True and result["evidence"]["endpoint"] == served
    cert, both = ["mgmtGatewayCertificate"], sorted(MATERIAL)
    assert transport.private == [("prerequisites", []), ("ingress_firewall", []), ("ingress_firewall", []), ("unit", both), ("observe", cert),
                                 ("observe", cert)]
    removed = {"exists": False, "ready": False, "removed": True, "partial": False, "daemons": [], "endpoint": {"certificateSHA256": "", "reachable": False}}
    transport = MaterialTransport(request, served)
    transport.observed.update(removed)
    ceph_core.execute(request, "destroy", "d" * 64, transport)
    assert transport.private == [("destroy", cert), ("observe", cert)]
    transport = MaterialTransport(request, served)
    assert ceph_core.execute(request, "observe", "d" * 64, transport)["evidence"]["endpoint"] == served and transport.private == [("observe", cert)]
    for operation in ("observe", "apply"):
        transport = MaterialTransport(request, {"certificateSHA256": hashlib.sha256(OTHER_LEAF).hexdigest(), "reachable": True})
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.execute(request, operation, "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    unbound = gateway_request(None, bound=False)
    transport = MaterialTransport(unbound, {"certificateSHA256": hashlib.sha256(OTHER_LEAF).hexdigest(), "reachable": True})
    transport.observed["daemons"] = transport.observed["daemons"][:1]
    assert ceph_core.execute(unbound, "observe", "d" * 64, transport)["evidence"]["endpoint"]["reachable"] is True
    assert transport.private == [("observe", [])]


def test_gateway_unit_refuses_an_absent_image_setting_before_any_write(tmp_path):
    for vip, name in ((VIP, "nginx"), (VIP, "keepalived"), (None, "nginx")):
        request = gateway_request(vip)
        markers = units.markers_for(tmp_path / (name + str(vip)), request)
        before = units.records(markers)
        monitor = monitoring.Monitor(request)
        key = "mgr/cephadm/container_image_" + name
        monitor.config = [entry for entry in monitor.config if entry["name"] != key]
        with pytest.raises(ceph_native.NativeFailure, match="^foreign-image$") as caught:
            applying(monitor, markers).unit()
        assert caught.value.native_error["cause"] == key
        assert monitor.mutations == [] and monitor.stdin == [] and units.records(markers) == before
    request = gateway_request(None)
    monitor = monitoring.Monitor(request)
    monitor.config = [entry for entry in monitor.config if not entry["name"].endswith("keepalived")]
    assert applying(monitor, units.markers_for(tmp_path / "direct", request)).unit() == {"changed": True}


def test_gateway_frontend_is_the_gateway_port_on_the_virtual_ip(tmp_path, monkeypatch):
    request = gateway_request()
    assert ceph_native_units.frontend(request) == (request["service"]["documents"][0], PORT)
    assert ceph_native_units.frontend(gateway_request(None)) is None
    checked = []
    monkeypatch.setattr(ceph_native_units, "firewall_check", lambda worker, spec, port: checked.append((worker.host["name"], spec["virtual_ip"], port)))
    monitor = monitoring.Monitor(request)
    assert units.worker(monitor, units.markers_for(tmp_path, request)).ingress_firewall() == {"changed": False}
    assert checked == [("node-1", VIP, PORT)] and monitor.mutations == []
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    transport = MaterialTransport(request, {"certificateSHA256": LEAF_SHA256, "reachable": True})
    ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert transport.calls[1:3] == [("node-1", "ingress_firewall"), ("node-2", "ingress_firewall")]
    transport = MaterialTransport(gateway_request(None), {"certificateSHA256": LEAF_SHA256, "reachable": True})
    transport.observed["daemons"] = transport.observed["daemons"][:1]
    ceph_core.execute(gateway_request(None), "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert "ingress_firewall" not in [phase for unused, phase in transport.calls]


def test_gateway_unit_never_writes_a_dashboard_setting(tmp_path, monkeypatch):
    markers, monitor = applied(tmp_path, gateway_request())
    Probes(monkeypatch, (LEAF, True))
    assert observing(monitor, markers)[0] is True
    assert applying(monitor, markers).unit() == {"changed": False}
    assert monitor.mutations == [("orch", "apply", "-i", "-")]
    commands = [argv[argv.index("--format") + 2:] for argv in monitor.log]
    assert not any(command[:1] == ["dashboard"] or command[:2] == ["mgr", "module"] or "set" in command[:2] for command in commands)
    assert not any("mgr/dashboard" in " ".join(command) for command in commands)


def test_gateway_tables_agree():
    assert observability.GATEWAY_TYPES == ("mgmt-gateway",) and set(observability.GATEWAY_TYPES) <= observe.SINGLETONS <= observe.LISTED_SINGLETONS
    assert observe.DELEGATED_TYPES["mgmt-gateway"] == "mgmt-gateway" and observe.DELEGATED_TYPES["keepalived"] == "ingress"
    assert all(ceph_native.DAEMON_TYPES[kind] == ("delegated", "delegated", False, "none") for kind in observability.GATEWAY_DAEMON_TYPES)
    assert ceph_native.DELEGATED_SERVICE.fullmatch("mgmt-gateway") and ceph_native.DELEGATED_SERVICE.fullmatch("ingress.mgmt")
    assert not any(ceph_native.DELEGATED_SERVICE.fullmatch(name) for name in ("mgmt-gateway.main", "oauth2-proxy"))
    gateway = ceph_native_units.UNIT_TYPES["mgmt-gateway"]
    assert gateway.singletons == frozenset(("mgmt-gateway",)) and gateway.endpoint is observability.gateway_endpoint
    assert gateway.daemon_types == observability.GATEWAY_DAEMON_TYPES and gateway.images == observability.GATEWAY_IMAGES
    assert set(gateway.images) == set(gateway.daemon_types) and set(gateway.images.values()) <= set(ceph_native.AUXILIARY_NAMES)
    assert gateway.leaves == {("mgmt-gateway", "/spec/ssl_cert"): "mgmtGatewayCertificate", ("mgmt-gateway", "/spec/ssl_key"): "mgmtGatewayKey"}
    assert observability.OBSERVED_LEAVES < set(gateway.leaves.values()) <= ceph_native_units.MATERIAL_VARIABLES
    entries = ("mgmt-gateway@mgmt-gateway", "keepalived@ingress.mgmt", "tls@mgmt-gateway", "http@mgmt-gateway")
    assert all(ceph_native_units.PENDING.fullmatch(entry) and ceph_core.blocking_entry(entry) for entry in entries)
    assert not any(ceph_native_units.PENDING.fullmatch(entry) for entry in ("tls@ingress.mgmt", "http@grafana", "haproxy@mgmt-gateway.x"))
    request = gateway_request()
    request["delegated"] = [entry for entry in request["delegated"] if entry["name"] == "mgmt-gateway"]
    daemon = {"daemon_type": "mgmt-gateway", "hostname": "node-1", "service_name": "mgmt-gateway"}
    ceph_native.delegated_daemon(daemon, request)
    for bad in (dict(daemon, hostname="node-3"), dict(daemon, service_name="ingress.mgmt"), dict(daemon, daemon_type="keepalived")):
        with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
            ceph_native.delegated_daemon(bad, request)
    assert observe.delegated_service("mgmt-gateway.node-1", None, ["mgmt-gateway", "ingress.mgmt"]) == "mgmt-gateway"


class Channel:
    """A TLS channel that presents one leaf and records the request it was sent."""

    def __init__(self, reply):
        self.reply, self.sent = reply, b""

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        return False

    def getpeercert(self, binary_form=False):
        assert binary_form
        return LEAF

    def sendall(self, data):
        self.sent += data

    def settimeout(self, value):
        assert 0 < value <= observability.PROBE_TIMEOUT

    def recv(self, size):
        reply, self.reply = self.reply[:size], self.reply[size:]
        return reply


def test_served_names_the_host_header(monkeypatch):
    channels, names = [], []

    class Context:
        def __init__(self, protocol):
            del protocol

        def wrap_socket(self, raw, server_hostname=None):
            names.append(server_hostname)
            return raw

    def connect(address, timeout):
        assert timeout == observability.PROBE_TIMEOUT and address[1] == PORT
        channels.append(Channel(b"HTTP/1.1 200 OK\r\n"))
        return channels[-1]

    monkeypatch.setattr(observability.socket, "create_connection", connect)
    monkeypatch.setattr(observability.ssl, "SSLContext", Context)
    assert observability.served("192.0.2.11", PORT, "node-1") == (LEAF, True)
    assert observability.served("2001:db8::60", PORT, "2001:db8::60", host="[2001:db8::60]") == (LEAF, True)
    assert names == ["node-1", "2001:db8::60"]
    assert [channel.sent for channel in channels] == [b"GET / HTTP/1.0\r\nHost: node-1\r\n\r\n", b"GET / HTTP/1.0\r\nHost: [2001:db8::60]\r\n\r\n"]
