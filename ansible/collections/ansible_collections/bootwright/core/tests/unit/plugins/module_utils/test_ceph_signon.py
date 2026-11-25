"""A CephSignOn block's oauth2-proxy unit: its credentials, TLS pair and identity provider CA bundle reach only the apply stream, each proxy
host reads the issuer's discovery document before any effect, the deployed proxy's rendered configuration is proved on each of its hosts,
the gateway's sign-on corrections are proved on each gateway host, and only then the unit's one dashboard write enables OAuth2 single
sign-on, after which readiness proves the proxies stayed running and the gateway enforces sign-on.

The fakes answer the upstream v20.2.1 and podman shapes the worker reads:
- OAuth2ProxySpec takes `https_address`, `provider_display_name`, `client_id`, `client_secret`, `oidc_issuer_url`, `redirect_url`,
  `cookie_secret`, `ssl_cert`, `ssl_key`, `allowlist_domains` and `custom_configs`, and `to_json` writes `custom_configs` as a top-level
  member of `{"content", "mount_path"}` entries (src/python-common/ceph/deployment/service_spec.py, OAuth2ProxySpec and CustomConfig);
- cephadm's oauth2-proxy daemon passes `--config=/etc/oauth2-proxy.conf` and mounts `<data_dir>/etc/oauth2-proxy.conf` there
  (src/cephadm/cephadmlib/daemons/oauth2_proxy.py:78-83, 138-151), and names its container `ceph-<fsid>-oauth2-proxy-<id>`;
- OAuth2ProxySpec.validate requires a non-empty `client_secret` and a `cookie_secret` of 16, 24 or 32 bytes, counted after
  `base64.urlsafe_b64decode` and, when that raises `binascii.Error`, as UTF-8 text (service_spec.py, OAuth2ProxySpec._validate_cookie_secret),
  which `upstream_oauth2_proxy` below mirrors;
- `dashboard sso enable oauth2` sets the dashboard module option `sso_oauth2` (src/pybind/mgr/dashboard/services/sso.py), which
  `ceph config get mgr mgr/dashboard/sso_oauth2` reads; it answers EPERM until the orchestrator lists an oauth2-proxy daemon whose
  status is running, which `ceph orch ps --daemon_type oauth2-proxy` reports as `status` 1;
- `orch ps` rows carry `started` and `last_refresh` (src/pybind/mgr/orchestrator/_interface.py:1471-1500), on cephadm's own clock;
- an unauthenticated `GET /` of the enforcing gateway is nginx's `auth_request /oauth2/auth` 401 turned into oauth2-proxy's sign-in,
  which with `skip_provider_button= true` is a 302 to the issuer's authorization endpoint;
- `podman ps --format json` lists `Id`, `Names` and `State`
  (https://github.com/containers/podman/blob/v5.2.0/pkg/domain/entities/types/container_ps.go), and `podman inspect --type container`
  reports `Args`, `Config.Env`, `Config.User` and `Mounts[].Source`/`Destination`
  (https://github.com/containers/podman/blob/v5.2.0/libpod/define/container_inspect.go).
The rendered configuration below is synthetic text in that file's flat TOML form, not a captured file.
"""

from __future__ import annotations

import base64
import binascii
import copy
import json
import os
import socket
import ssl
import threading

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_evidence, ceph_host_runtime, ceph_native, ceph_native_observability as observability, ceph_native_observe as observe,
    ceph_native_units, ceph_signon_native as signon,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

FSID = units.FSID
OAUTH2 = "registry.example.test/ceph/oauth2-proxy@sha256:" + "e" * 64
ISSUER = "https://idp.example.test/realms/ceph"
CA = gateway.pem("CERTIFICATE", b"synthetic identity provider authority for the proxy")
OTHER_CA = gateway.pem("CERTIFICATE", b"a bundle the bound Secret does not hold")
CLIENT = "client-secret-sentinel-0123456789"
COOKIE = "cookie-secret-sentinel-abcdefghi"
PROXY_CERT = gateway.pem("CERTIFICATE", b"synthetic oauth2-proxy leaf certificate")
PROXY_KEY = gateway.pem("PRIVATE KEY", b"oauth2-proxy private key material only")
SSO_MATERIAL = {"oauth2ProxyCABundle": CA, "oauth2ProxyClientSecret": CLIENT, "oauth2ProxyCookieSecret": COOKIE, "oauth2ProxyCertificate": PROXY_CERT,
                "oauth2ProxyKey": PROXY_KEY}
SECRET_MARKS = [line for text in (CA, PROXY_CERT, PROXY_KEY) for line in text.splitlines() if "-----" not in line] + [CLIENT, COOKIE]
CONFIG = "/etc/oauth2-proxy.conf"
CONTAINER = "ceph-" + FSID + "-oauth2-proxy-node-1"
PIN = gateway.LEAF_SHA256
STARTED = "2026-11-25T10:00:00.000000Z"
REFRESHED = "2026-11-25T10:01:00.000000Z"
# The gateway tests' identity normalization in place of the frozen image's SDK, autoused here too.
normalization = gateway.normalization


def proxy_document():
    return {"custom_configs": [{"mount_path": signon.CA_PATH}], "placement": {"count": 2, "hosts": list(gateway.TWO)},
            "service_type": "oauth2-proxy", "spec": {"client_id": "ceph", "oidc_issuer_url": ISSUER, "provider_display_name": "SSO"}}


def target(**changes):
    """The gateway the unit's enforcement and route proofs read, as Go froze it."""
    return dict({"certificateSHA256": PIN, "count": 2, "hosts": list(gateway.TWO), "port": gateway.PORT, "virtualIP": gateway.VIP}, **changes)


def bindings():
    return [{"path": path, "secretRef": ref, "serviceRef": "oauth2-proxy"} for path, ref in (
        ("/custom_configs/0/content", "sso-ca"), ("/spec/client_secret", "sso-client"), ("/spec/cookie_secret", "sso-cookie"),
        ("/spec/ssl_cert", "sso-tls"), ("/spec/ssl_key", "sso-tls"))]


def sso_request():
    service = {"bindings": bindings(), "documents": [proxy_document()], "id": "", "signOn": target(), "type": "oauth2-proxy"}
    request = dict(units.cluster(), action="service", service=service)
    request["delegated"] = [{"hosts": list(gateway.TWO), "name": "oauth2-proxy"}]
    request["auxiliary"] = [{"image": OAUTH2, "name": "oauth2_proxy"}]
    return request


class SSOMonitor(monitoring.Monitor):
    """The monitoring unit's scripted monitor, with the dashboard's sso_oauth2 option, its enable command, and this host's podman."""

    def __init__(self, request, enabled=False, containers=None, starting=0):
        super().__init__(request)
        self.enabled, self.containers, self.starting = enabled, list(containers or []), starting

    def run(self, argv, data=b"", timeout=60):
        if argv[0] == ceph_native_units.PODMAN:
            self.log.append(list(argv))
            if argv[1] == "ps":
                assert argv[2:] == ["--all", "--format", "json", "--filter", "name=^ceph-" + FSID + "-oauth2-proxy[.-]"]
                return 0, json.dumps([{"Id": "%064x" % index, "Names": [name], "State": state}
                                      for index, (name, state, unused) in enumerate(self.containers)]).encode()
            assert argv[1:4] == ["inspect", "--type", "container"]
            return 0, json.dumps([self.containers[int(argv[4], 16)][2]]).encode()
        command = tuple(argv[argv.index("--format") + 2:])
        if command == ("orch", "ps", "--daemon_type", "oauth2-proxy"):
            self.log.append(list(argv))
            status = 0 if self.starting else 1
            self.starting = max(self.starting - 1, 0)
            return 0, json.dumps([{"daemon_type": "oauth2-proxy", "service_name": "oauth2-proxy", "status": status}]).encode()
        if command == ("config", "get", "mgr", signon.SSO_OPTION):
            self.log.append(list(argv))
            return 0, json.dumps(self.enabled).encode()
        if command == ("dashboard", "ac-user-show"):
            self.log.append(list(argv))
            return 0, b'["bootwright-admin"]'
        if command == ("dashboard", "sso", "enable", "oauth2"):
            self.log.append(list(argv))
            self.mutations.append(command)
            if self.starting:
                return 1, b"Error EPERM: OAuth2 SSO prerequisite not met: oauth2-proxy service must be deployed and running."
            self.enabled = True
            return 0, b'SSO is "enabled" with "OAuth2" protocol.'
        return super().run(argv, data, timeout)


def rendered(root, relative, content, mode=0o600):
    path = os.path.join(root, FSID, relative)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(content)
    os.chmod(path, mode)
    return path


def inspected(config_source, ca_source, args=None, environment=()):
    mounts = [{"Source": config_source, "Destination": CONFIG}, {"Source": ca_source, "Destination": signon.CA_PATH}]
    return {"Args": ["--config=" + CONFIG, "--tls-cert-file=/etc/oauth2-proxy.crt"] if args is None else args,
            "Config": {"Env": ["PATH=/usr/bin"] + list(environment), "User": str(os.getuid())}, "Mounts": mounts}


@pytest.fixture
def host(tmp_path, monkeypatch):
    root = str(tmp_path / "ceph")
    monkeypatch.setattr(observability, "DATA_ROOT", root)
    conf = rendered(root, "oauth2-proxy.node-1/etc/oauth2-proxy.conf",
                    '# rendered\nprovider = "oidc"\nclient_secret = "' + CLIENT + '"\ncookie_secret = "' + COOKIE + '"\n')
    ca = rendered(root, "custom_config_files/oauth2-proxy.node-1/oauth2-proxy-idp-ca.pem", CA)
    return root, conf, ca


def applying(monitor, markers, material=None):
    return gateway.applying(monitor, markers, SSO_MATERIAL if material is None else material)


def proving(request, monitor, markers, bundle=CA):
    native = ceph_native_units.UnitWorker(request, "node-1", run=monitor.run, markers=markers)
    native.bound = ceph_native_units.material(request, "sso_config", {"oauth2ProxyCABundle": bundle})
    return native.sso_config()


def observing(monitor, markers):
    return gateway.observing(monitor, markers, {})


def row(host_name, **extra):
    return gateway.row("oauth2-proxy", "oauth2-proxy", host_name, OAUTH2, **dict({"started": STARTED, "last_refresh": REFRESHED}, **extra))


def sso_running(unused=None):
    return [row(node) for node in gateway.TWO]


class Probes:
    """The verdict each gateway endpoint's enforcement probe gives, and every probe's target."""

    def __init__(self, monkeypatch, verdicts):
        self.verdicts, self.targets = verdicts, []
        monkeypatch.setattr(signon, "probe", self.probe)

    def probe(self, address, number, name, host_header, pinned, issuer):
        self.targets.append((address, number, name, host_header, pinned, issuer))
        return self.verdicts[address] if isinstance(self.verdicts, dict) else self.verdicts


def test_the_request_is_valid_and_places_one_proxy_document():
    request = sso_request()
    assert ceph_native.validate(copy.deepcopy(request)) is not None
    unit = request["service"]
    assert signon.expected(unit) == {("oauth2-proxy", "oauth2-proxy"): 2} and ceph_native_units.frontend(request) is None
    unit["documents"][0]["placement"].pop("count")
    assert signon.expected(unit) == {("oauth2-proxy", "oauth2-proxy"): 2}
    assert ceph_native_units.material_names(request, "unit") == frozenset(SSO_MATERIAL)
    assert ceph_native_units.material_names(request, "signon_discovery") == ceph_native_units.material_names(request, "sso_config") == {
        "oauth2ProxyCABundle"}
    assert all(ceph_native_units.material_names(request, phase) == frozenset() for phase in ("observe", "destroy", "sso", "signon_routes"))
    unbound = sso_request()
    unbound["service"]["bindings"] = [item for item in unbound["service"]["bindings"] if not item["path"].startswith("/spec/ssl_")
                                      and not item["path"].startswith("/custom")]
    unbound["service"]["documents"][0].pop("custom_configs")
    assert ceph_native.validate(unbound) is not None


INVALID = {
    "no signOn": lambda request: request["service"].pop("signOn"),
    "signOn on a gateway": lambda request: request.update(service=dict(gateway.gateway_request()["service"], signOn=target())),
    "digest of the wrong length": lambda request: request["service"]["signOn"].update(certificateSHA256="ab" * 31),
    "upper case digest": lambda request: request["service"]["signOn"].update(certificateSHA256="AB" * 32),
    "signOn member outside the record": lambda request: request["service"]["signOn"].update(scheme="https"),
    "no hosts": lambda request: request["service"]["signOn"].pop("hosts"),
    "unsorted hosts": lambda request: request["service"]["signOn"].update(hosts=["node-2", "node-1"]),
    "host outside the cluster": lambda request: request["service"]["signOn"].update(hosts=["node-1", "node-9"]),
    "no gateway host": lambda request: request["service"]["signOn"].update(hosts=[]),
    "port zero": lambda request: request["service"]["signOn"].update(port=0),
    "port a bool": lambda request: request["service"]["signOn"].update(port=True),
    "non-canonical virtual IP": lambda request: request["service"]["signOn"].update(virtualIP="2001:DB8::60"),
    "endpoint not a DNS name": lambda request: request["service"]["signOn"].update(endpoint="mgr_storage.example.test"),
    "no oauth2_proxy image": lambda request: request.update(auxiliary=[]),
    "second document": lambda request: request["service"]["documents"].append(dict(proxy_document(), service_type="mgmt-gateway")),
    "service id on the document": lambda request: request["service"]["documents"][0].update(service_id="main"),
    "id on the unit": lambda request: request["service"].update(id="main"),
    "verification switch in the spec": lambda request: request["service"]["documents"][0]["spec"].update(ssl_insecure_skip_verify=True),
    "plaintext issuer": lambda request: request["service"]["documents"][0]["spec"].update(oidc_issuer_url="http://idp.example.test/realms/ceph"),
    "no client id": lambda request: request["service"]["documents"][0]["spec"].pop("client_id"),
    "another CA mount path": lambda request: request["service"]["documents"][0].update(custom_configs=[{"mount_path": "/etc/other.pem"}]),
    "two custom configs": lambda request: request["service"]["documents"][0].update(custom_configs=[{"mount_path": signon.CA_PATH}] * 2),
    "inline CA content": lambda request: request["service"]["documents"][0].update(custom_configs=[{"content": CA, "mount_path": signon.CA_PATH}]),
    "no client secret bound": lambda request: request["service"].update(bindings=[item for item in bindings() if item["path"] != "/spec/client_secret"]),
    "no cookie secret bound": lambda request: request["service"].update(bindings=[item for item in bindings() if item["path"] != "/spec/cookie_secret"]),
    "certificate without its key": lambda request: request["service"].update(bindings=[item for item in bindings() if item["path"] != "/spec/ssl_key"]),
    "CA bound without custom_configs": lambda request: request["service"]["documents"][0].pop("custom_configs"),
    "custom_configs without the CA bound": lambda request: request["service"].update(
        bindings=[item for item in bindings() if not item["path"].startswith("/custom")]),
    "binding for another service": lambda request: request["service"]["bindings"][0].update(serviceRef="mgmt-gateway"),
    "delegated hosts differ": lambda request: request["delegated"][0].update(hosts=["node-1"]),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_sign_on_request_validation(case):
    request = sso_request()
    assert ceph_native.validate(copy.deepcopy(request)) is not None
    INVALID[case](request)
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)


def test_signon_admits_the_optional_target_members():
    request = sso_request()
    request["service"]["signOn"].update(endpoint="mgr.storage.example.test", grafana=True, virtualIP="2001:db8::60")
    assert ceph_native.validate(request) is not None
    request = sso_request()
    request["service"]["signOn"].pop("virtualIP")
    assert ceph_native.validate(request) is not None


def test_signon_tables_agree():
    unit = ceph_native_units.UNIT_TYPES["oauth2-proxy"]
    assert unit.singletons == frozenset(("oauth2-proxy",)) and unit.daemon_types == signon.DAEMON_TYPES == ("oauth2-proxy",)
    assert unit.images == signon.IMAGES == {"oauth2-proxy": "oauth2_proxy"} and unit.endpoint is signon.endpoint and unit.leaves == {
        ("oauth2-proxy", "/custom_configs/0/content"): "oauth2ProxyCABundle", ("oauth2-proxy", "/spec/client_secret"): "oauth2ProxyClientSecret",
        ("oauth2-proxy", "/spec/cookie_secret"): "oauth2ProxyCookieSecret", ("oauth2-proxy", "/spec/ssl_cert"): "oauth2ProxyCertificate",
        ("oauth2-proxy", "/spec/ssl_key"): "oauth2ProxyKey"}
    assert set(unit.images.values()) <= set(ceph_native.AUXILIARY_NAMES) and set(unit.leaves.values()) <= ceph_native_units.MATERIAL_VARIABLES
    assert signon.CA_PATH == "/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem" and signon.CA_VARIABLE == "oauth2ProxyCABundle"
    assert signon.SSO_OPTION == "mgr/dashboard/sso_oauth2"
    assert observe.DELEGATED_TYPES["oauth2-proxy"] == "oauth2-proxy" and ceph_native.DELEGATED_SERVICE.fullmatch("oauth2-proxy")
    assert ceph_native.DAEMON_TYPES["oauth2-proxy"] == ("delegated", "delegated", False, "none") and "oauth2-proxy" in observe.SINGLETONS
    entries = ("oauth2-proxy@oauth2-proxy", "sso@oauth2-proxy", "stable@oauth2-proxy", "enforce@mgmt-gateway", "tls@mgmt-gateway")
    assert all(ceph_native_units.PENDING.fullmatch(entry) and ceph_core.blocking_entry(entry) for entry in entries)
    assert ceph_core.CLUSTER_REASONS.count("sign-on-provider-unreachable") == ceph_core.CLUSTER_REASONS.count("sign-on-unprotected") == 1
    assert "sign-on-unenforced" in ceph_core.CLUSTER_REASONS and "sso-unverified" in ceph_core.CLUSTER_REASONS
    assert {"signon_discovery", "signon_routes", "signon_route_proof", "sso_config", "sso"} <= ceph_native_units.PHASES
    assert {"signon_discovery", "signon_routes", "signon_route_proof"} <= ceph_native_units.READ_ONLY_PHASES
    assert not ({"sso_config", "sso"} & ceph_native_units.READ_ONLY_PHASES)


def test_oauth2_proxy_rendered_configuration_proves_idp_verification(tmp_path, host):
    root, conf, ca = host
    request = sso_request()
    markers = units.markers_for(tmp_path / "markers", request)

    def verdict(container, config=None):
        if config is not None:
            with open(conf, "w", encoding="utf-8") as handle:
                handle.write(config)
        monitor = SSOMonitor(request, containers=[(CONTAINER, "running", container), (CONTAINER + ".old", "exited", {})])
        result = proving(request, monitor, markers)
        assert monitor.mutations == []
        return result

    plain = open(conf, encoding="utf-8").read()
    assert verdict(inspected(conf, ca)) == {"changed": False, "daemons": 1, "proved": True}
    assert verdict(inspected(conf, ca), plain + "ssl_insecure_skip_verify = false\n")["proved"] is True
    insecure = (({}, plain + 'ssl_insecure_skip_verify = "true"\n', "configuration"), ({}, plain + "ssl_insecure_skip_verify = true\n", "configuration"),
                ({"args": ["--config=" + CONFIG, "--ssl-insecure-skip-verify"]}, plain, "argument"),
                ({"args": ["--config=" + CONFIG, "--ssl-insecure-skip-verify=true"]}, plain, "argument"),
                ({"environment": ["OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY=TRUE"]}, plain, "environment"))
    for options, config, where in insecure:
        with pytest.raises(ceph_native.NativeFailure, match="^sso-unverified$") as caught:
            verdict(inspected(conf, ca, **options), config)
        cause = caught.value.native_error["cause"]
        assert cause == "host node-1 daemon " + CONTAINER + ": oauth2-proxy skips identity provider certificate verification in its " + where
        assert not any(mark in cause for mark in SECRET_MARKS + [root])
    with open(conf, "w", encoding="utf-8") as handle:
        handle.write(plain)
    outside = rendered(root.replace("ceph", "elsewhere"), "oauth2-proxy.node-1/etc/oauth2-proxy.conf", plain)
    link = os.path.join(os.path.dirname(conf), "linked.conf")
    os.symlink(conf, link)
    writable = rendered(root, "oauth2-proxy.node-1/etc/writable.conf", plain, mode=0o620)
    unproved = [inspected(conf, ca, args=["--tls-cert-file=/etc/oauth2-proxy.crt"]), dict(inspected(conf, ca), Mounts=[]),
                inspected(outside, ca), inspected(link, ca), inspected(writable, ca)]
    for container in unproved:
        assert verdict(container) == {"changed": False, "daemons": 1, "proved": False}
    for config in (plain + "ssl_insecure_skip_verify = false\nssl_insecure_skip_verify = false\n", plain + "ssl_insecure_skip_verify = yes\n"):
        assert verdict(inspected(conf, ca), config)["proved"] is False
    assert signon.rendered_skip("ssl_insecure_skip_verify = 'false'") == "verified" and signon.config_path(["--config", CONFIG]) == CONFIG
    assert signon.insecure_flag(["--ssl-insecure-skip-verify=false"], ["OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY=false"]) is None


def test_ca_bundle_reaches_the_container_only_through_custom_configs(tmp_path, host, monkeypatch):
    root, conf, ca = host
    Probes(monkeypatch, "enforced")
    request = sso_request()
    markers = units.markers_for(tmp_path / "markers", request)
    monitor = SSOMonitor(request)
    assert applying(monitor, markers).unit() == {"changed": True}
    parts = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")]
    assert [ceph_native_units.service_name(part) for part in parts] == ["oauth2-proxy"]
    assert parts[0]["custom_configs"] == [{"content": CA, "mount_path": signon.CA_PATH}]
    ca_marks = [line for line in CA.splitlines() if "-----" not in line]
    stored = monitoring.recorded(markers).decode()
    assert "bootwright-bound:/custom_configs/0/content" in stored
    assert not any(mark in text for mark in ca_marks for text in (stored, json.dumps(monitor.log)))
    observed = units.worker(monitor, markers)
    observed.bound = ceph_native_units.material(request, "observe", {})
    evidence = observed.empty_evidence()
    observed.observe_resource(evidence, True)
    assert not any(mark in json.dumps([evidence, observed.blocking]) for mark in ca_marks)
    monitor.containers = [(CONTAINER, "running", inspected(conf, ca))]
    assert proving(request, monitor, markers)["proved"] is True
    assert proving(request, monitor, markers, bundle=OTHER_CA)["proved"] is False
    with pytest.raises(ceph_native.NativeFailure) as caught:
        monitor.containers = [(CONTAINER, "running", inspected(conf, ca, args=["--config=" + CONFIG, "--ssl-insecure-skip-verify"]))]
        proving(request, monitor, markers)
    assert not any(mark in json.dumps(caught.value.native_error) for mark in ca_marks)
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native_units.material(request, "sso_config", {"oauth2ProxyCABundle": CA, "oauth2ProxyClientSecret": CLIENT})
    assert ceph_native_units.material_names(request, "sso") == frozenset()


def test_client_secret_travels_only_on_stdin(tmp_path):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    native = applying(monitor, markers)
    assert native.unit() == {"changed": True}
    proxy = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")][0]
    assert proxy["spec"]["client_secret"] == CLIENT and proxy["spec"]["cookie_secret"] == COOKIE
    assert proxy["spec"]["ssl_cert"] == PROXY_CERT and proxy["spec"]["ssl_key"] == PROXY_KEY
    compared = native.bound_specs(native.unit_specs())
    visible = json.dumps([monitor.log, compared]) + monitoring.recorded(markers).decode()
    assert not any(secret in visible for secret in (CLIENT, COOKIE))
    assert compared["oauth2-proxy"]["spec"]["client_secret"] == "bootwright-bound:/spec/client_secret"
    assert applying(monitor, markers).unit() == {"changed": False} and len(monitor.stdin) == 1


def test_sso_evidence_has_no_secret(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    assert applying(monitor, markers).unit() == {"changed": True}
    monitor.running = sso_running()
    probes = Probes(monkeypatch, "enforced")
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is False and blocking == ["sso@oauth2-proxy"] and evidence["sso"] == ""
    assert evidence["endpoint"] == {"certificateSHA256": PIN, "reachable": True}
    assert probes.targets == [(gateway.VIP, gateway.PORT, gateway.VIP, gateway.VIP, PIN, ISSUER)]
    monitor.enabled = True
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is True and blocking == [] and evidence["sso"] == "oauth2"
    assert not any(mark in json.dumps(evidence) for mark in SECRET_MARKS)
    value = dict(evidence, request="d" * 64, ready=True, exists=True)
    assert ceph_evidence.evidence(copy.deepcopy(value), "d" * 64, request)["sso"] == "oauth2"
    assert ceph_evidence.evidence(copy.deepcopy(value), "d" * 64)["sso"] == "oauth2"
    for bad, against in ((dict(value, sso="saml2"), request), (value, gateway.gateway_request()),
                         (dict(value, endpoint={"certificateSHA256": "a" * 64, "reachable": True}), request),
                         (dict(value, endpoint={"certificateSHA256": PIN, "reachable": False}), request),
                         (dict(value, endpoint={"certificateSHA256": "", "reachable": True}), request)):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_evidence.evidence(copy.deepcopy(bad), "d" * 64, against)
    assert not any(command[-5:-1] == ["dashboard", "sso", "enable", "oauth2"] or "status" in command for command in monitor.log)


def test_readiness_names_each_unmet_sign_on_proof(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request, enabled=True)
    assert applying(monitor, markers).unit() == {"changed": True}
    monitor.running = sso_running()
    probes = Probes(monkeypatch, "enforced")
    assert observing(monitor, markers)[:2] == (True, [])
    monitor.running = [row("node-1", started="2026-11-25T10:00:31.000000Z"), row("node-2")]
    assert observing(monitor, markers)[:2] == (False, ["stable@oauth2-proxy"])
    monitor.running = sso_running()
    probes.verdicts = "pending"
    ready, blocking, evidence = observing(monitor, markers)
    assert ready is False and blocking == ["enforce@mgmt-gateway"] and evidence["endpoint"] == {"certificateSHA256": "", "reachable": False}
    probes.verdicts = "tls"
    assert observing(monitor, markers)[:2] == (False, ["tls@mgmt-gateway"])
    probes.verdicts = "enforced"
    monitor.running = [row("node-1")]
    ready, blocking, unused = observing(monitor, markers)
    assert ready is False and blocking == ["oauth2-proxy@oauth2-proxy"]
    monitor.enabled = False
    monitor.running = [row("node-1", started=REFRESHED), row("node-2")]
    assert observing(monitor, markers)[1] == ["stable@oauth2-proxy", "sso@oauth2-proxy"]


class FakeWorker:
    """What readiness reads: the daemon rows the observation listed and the dashboard option's answer."""

    def __init__(self, rows, option):
        self.unit_rows, self.option, self.sso_protocol = rows, option, ""

    def ceph(self, *args):
        assert args == ("config", "get", "mgr", signon.SSO_OPTION)
        return self.option


def test_stable_needs_thirty_seconds_between_start_and_cephadms_last_refresh_of_a_running_proxy():
    def proxy(started, refreshed, status=1, **extra):
        return dict(gateway.row("oauth2-proxy", "oauth2-proxy", "node-1", OAUTH2, started=started, last_refresh=refreshed, status=status), **extra)

    assert signon.stable([proxy("2026-11-25T10:00:00Z", "2026-11-25T10:00:30Z")]) is True
    assert signon.stable([proxy("2026-11-25T10:00:00.5Z", "2026-11-25T10:00:30.5Z"), proxy(STARTED, REFRESHED)]) is True
    for rows in ([], [proxy("2026-11-25T10:00:00Z", "2026-11-25T10:00:29.999999999Z")], [proxy(STARTED, REFRESHED, status=0)],
                 [proxy(STARTED, REFRESHED, status=2)], [proxy(STARTED, REFRESHED, status=True)], [proxy(STARTED, REFRESHED, status="1")],
                 [proxy(None, REFRESHED)], [proxy(STARTED, None)], [proxy("yesterday", REFRESHED)], [proxy(REFRESHED, STARTED)],
                 [proxy(STARTED, REFRESHED), proxy("2026-11-25T10:00:40Z", REFRESHED)],
                 [gateway.row("mgmt-gateway", "mgmt-gateway", "node-1", gateway.NGINX, started=STARTED, last_refresh=REFRESHED)]):
        assert signon.stable(rows) is False, rows
    others = [gateway.row("mgmt-gateway", "mgmt-gateway", "node-1", gateway.NGINX), proxy(STARTED, REFRESHED)]
    assert signon.stable(others) is True


def test_readiness_blocks_on_an_unstable_proxy_and_on_a_disabled_single_sign_on():
    stable = [gateway.row("oauth2-proxy", "oauth2-proxy", "node-1", OAUTH2, started=STARTED, last_refresh=REFRESHED)]
    young = [dict(stable[0], last_refresh="2026-11-25T10:00:05.000000Z")]
    for rows, option, entries, ready in ((stable, True, [], True), (stable, "true", [], True), (young, True, ["stable@oauth2-proxy"], False),
                                         (stable, False, ["sso@oauth2-proxy"], False), (stable, "False", ["sso@oauth2-proxy"], False),
                                         (young, False, ["stable@oauth2-proxy", "sso@oauth2-proxy"], False)):
        worker, blocking = FakeWorker(rows, option), []
        assert signon.readiness(worker, {}, blocking) is ready and blocking == entries
        assert worker.sso_protocol == ("oauth2" if option in (True, "true") else "")
    repeated = ["stable@oauth2-proxy", "sso@oauth2-proxy"]
    assert signon.readiness(FakeWorker(young, False), {}, repeated) is False and repeated == ["stable@oauth2-proxy", "sso@oauth2-proxy"]
    for option in ("maybe", 1, None, ["true"]):
        with pytest.raises(ceph_native.Refusal, match="^native-output$"):
            signon.sso_enabled(FakeWorker(stable, option))


def gateway_rows(*hosts, status=1):
    return [{"daemon_type": "mgmt-gateway", "hostname": host, "status": status, "ports": [gateway.PORT]} for host in hosts]


def with_gateway_rows(worker, rows):
    answer = worker.ceph

    def ceph(*args, **options):
        if args == ("orch", "ps", "--daemon_type", "mgmt-gateway"):
            return rows
        return answer(*args, **options)

    worker.ceph = ceph
    return worker


def test_endpoint_probes_the_virtual_ip_or_each_running_gateway_daemon(tmp_path, monkeypatch):
    request = sso_request()
    request["service"]["signOn"].update(endpoint="mgr.storage.example.test")
    markers = units.markers_for(tmp_path, request)
    worker = units.worker(SSOMonitor(request), markers)
    probes = Probes(monkeypatch, "enforced")
    assert signon.endpoint(worker, request["service"], []) == {"certificateSHA256": PIN, "reachable": True}
    assert probes.targets == [(gateway.VIP, gateway.PORT, gateway.VIP, gateway.VIP, PIN, ISSUER)]
    v6 = sso_request()
    v6["service"]["signOn"].update(virtualIP="2001:db8::60")
    probes = Probes(monkeypatch, "enforced")
    signon.endpoint(units.worker(SSOMonitor(v6), units.markers_for(tmp_path / "v6", v6)), v6["service"], [])
    assert probes.targets == [("2001:db8::60", gateway.PORT, "2001:db8::60", "[2001:db8::60]", PIN, ISSUER)]
    direct = sso_request()
    direct["service"]["signOn"].pop("virtualIP")
    addresses = {item["name"]: item["address"] for item in direct["hosts"]}
    probes = Probes(monkeypatch, {addresses["node-1"]: "enforced", addresses["node-2"]: "pending"})
    worker = with_gateway_rows(units.worker(SSOMonitor(direct), units.markers_for(tmp_path / "direct", direct)), gateway_rows(*gateway.TWO))
    assert signon.endpoint(worker, direct["service"], []) == {"certificateSHA256": "", "reachable": False}
    assert probes.targets == [(addresses[name], gateway.PORT, name, name, PIN, ISSUER) for name in gateway.TWO]
    assert worker.blocking == ["enforce@mgmt-gateway"]
    probes = Probes(monkeypatch, {addresses["node-1"]: "tls", addresses["node-2"]: "pending"})
    worker = with_gateway_rows(units.worker(SSOMonitor(direct), units.markers_for(tmp_path / "direct2", direct)), gateway_rows(*gateway.TWO))
    assert signon.endpoint(worker, direct["service"], [])["reachable"] is False and worker.blocking == ["tls@mgmt-gateway", "enforce@mgmt-gateway"]
    probes = Probes(monkeypatch, "enforced")
    one = with_gateway_rows(units.worker(SSOMonitor(direct), units.markers_for(tmp_path / "one", direct)),
                            gateway_rows(gateway.TWO[0]) + gateway_rows(gateway.TWO[1], status=0) + gateway_rows("node-9"))
    assert signon.endpoint(one, direct["service"], [])["reachable"] is (direct["service"]["signOn"]["count"] == 1)
    assert probes.targets == [(addresses[gateway.TWO[0]], gateway.PORT, gateway.TWO[0], gateway.TWO[0], PIN, ISSUER)]


def head(status, *headers):
    return ("\r\n".join(("HTTP/1.1 " + status,) + headers) + "\r\n\r\n").encode()


AUTHORIZE = ISSUER + "/protocol/openid-connect/auth?client_id=ceph&response_type=code"


def test_only_a_302_to_the_issuers_https_origin_is_enforcement():
    cases = (
        (head("302 Found", "Location: " + AUTHORIZE), True),
        (head("302 Found", "location:" + AUTHORIZE, "Content-Length: 0"), True),
        (head("302 Found", "Location: https://IDP.example.test:443/other"), True),
        (head("200 OK", "Content-Type: text/html"), False),
        (head("401 Unauthorized"), False),
        (head("403 Forbidden"), False),
        (head("301 Moved Permanently", "Location: " + AUTHORIZE), False),
        (head("307 Temporary Redirect", "Location: " + AUTHORIZE), False),
        (head("302 Found", "Location: https://evil.example.test/realms/ceph/auth"), False),
        (head("302 Found", "Location: https://idp.example.test:8443/realms/ceph/auth"), False),
        (head("302 Found", "Location: http://idp.example.test/realms/ceph/auth"), False),
        (head("302 Found", "Location: /oauth2/sign_in"), False),
        (head("302 Found", "Location: " + AUTHORIZE, "Location: " + AUTHORIZE), False),
        (head("302 Found", "Content-Length: 0"), False),
        (head("302 Found", "Location: "), False),
    )
    for data, passes in cases:
        found = signon.answer(data)
        assert found is not None and signon.enforced(found[0], found[1], ISSUER) is passes, data
    assert signon.answer(head("302 Found", "Location: " + AUTHORIZE)) == (302, AUTHORIZE)
    assert signon.answer(head("302 Found", "Location: a", "Location: b")) == (302, None)
    for data in (b"", b"HTTP/1.1 302 Found\r\nLocation: " + AUTHORIZE.encode(), b"garbage\r\n\r\n", b"HTTP/2 302\r\n\r\n",
                 b"HTTP/1.1 302 Found\r\nbroken\r\n\r\n"):
        assert signon.answer(data) is None, data
    assert signon.enforced(302, AUTHORIZE, "http://idp.example.test/realms/ceph") is False and signon.enforced(302, AUTHORIZE, "not a url") is False
    assert signon.enforced(302, None, ISSUER) is False


class Wire:
    """A TLS channel that presents one certificate and records the request it was sent, answering from a fixed reply."""

    def __init__(self, reply, certificate=gateway.LEAF, failure=None):
        self.reply, self.certificate, self.failure, self.sent = reply, certificate, failure, b""

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        return False

    def getpeercert(self, binary_form=False):
        assert binary_form
        return self.certificate

    def sendall(self, data):
        self.sent += data

    def settimeout(self, value):
        assert 0 < value <= max(signon.ENFORCE_TIMEOUT, signon.DISCOVERY_TIMEOUT)

    def recv(self, size):
        if self.failure is not None:
            raise self.failure
        reply, self.reply = self.reply[:size], self.reply[size:]
        return reply


class Context:
    """A TLS context that wraps without verifying, as the fake channels need."""

    def __init__(self, protocol=None):
        del protocol

    @staticmethod
    def wrap_socket(raw, server_hostname=None):
        del server_hostname
        return raw


def wired(monkeypatch, wire, failure=None):
    channels = []

    def connect(address, timeout):
        assert timeout == signon.ENFORCE_TIMEOUT
        if failure is not None:
            raise failure
        channels.append(wire)
        return wire

    monkeypatch.setattr(signon.socket, "create_connection", connect)
    monkeypatch.setattr(signon.ssl, "SSLContext", Context)
    return channels


def test_the_enforcement_probe_sends_one_cookieless_request_and_returns_only_its_verdict(monkeypatch):
    wire = Wire(head("302 Found", "Location: " + AUTHORIZE))
    wired(monkeypatch, wire)
    assert signon.probe(gateway.VIP, gateway.PORT, "node-1", gateway.VIP, PIN, ISSUER) == "enforced"
    assert wire.sent == b"GET / HTTP/1.1\r\nHost: " + gateway.VIP.encode() + b"\r\nAccept: text/html\r\nConnection: close\r\n\r\n"
    assert b"Cookie" not in wire.sent and b"Authorization" not in wire.sent
    for reply, verdict in ((head("200 OK", "Content-Type: text/html"), "pending"), (head("401 Unauthorized"), "pending"), (b"", "pending"),
                           (b"HTTP/1.1 302 Found\r\nLocation: " + AUTHORIZE.encode(), "pending")):
        wired(monkeypatch, Wire(reply))
        assert signon.probe(gateway.VIP, gateway.PORT, "node-1", gateway.VIP, PIN, ISSUER) == verdict
    wire = Wire(head("302 Found", "Location: " + AUTHORIZE), certificate=b"another leaf the pin does not name")
    wired(monkeypatch, wire)
    assert signon.probe(gateway.VIP, gateway.PORT, "node-1", gateway.VIP, PIN, ISSUER) == "tls" and wire.sent == b""
    wired(monkeypatch, Wire(b""), failure=OSError("refused"))
    assert signon.probe(gateway.VIP, gateway.PORT, "node-1", gateway.VIP, PIN, ISSUER) == "pending"
    wired(monkeypatch, Wire(b"", failure=socket.timeout("timed out")))
    assert signon.probe(gateway.VIP, gateway.PORT, "node-1", gateway.VIP, PIN, ISSUER) == "pending"


def document(**changes):
    body = {"authorization_endpoint": ISSUER + "/protocol/openid-connect/auth", "issuer": ISSUER, "token_endpoint": ISSUER + "/protocol/openid-connect/token"}
    return dict(body, **changes)


def answered(status="200 OK", body=None, types=("application/json",)):
    text = json.dumps(document()) if body is None else body
    headers = ["Content-Type: " + item for item in types] + ["Content-Length: " + str(len(text.encode()))]
    return head(status, *headers) + text.encode()


def test_discovery_answer_stages():
    assert signon.discovered(answered(), ISSUER) is None
    assert signon.discovered(answered(types=("application/json; charset=utf-8",)), ISSUER) is None
    cases = {
        "status 404": answered("404 Not Found"),
        "status 302": answered("302 Found"),
        "status 500": answered("500 Internal Server Error"),
        "content": answered(types=("text/html",)),
    }
    for stage, data in cases.items():
        assert signon.discovered(data, ISSUER) == stage, stage
    assert signon.discovered(head("200 OK") + json.dumps(document()).encode(), ISSUER) == "content"
    assert signon.discovered(answered(types=("application/json", "application/json")), ISSUER) == "content"
    assert signon.discovered(answered(body="<html>sign in</html>"), ISSUER) == "content"
    assert signon.discovered(answered(body=""), ISSUER) == "content"
    assert signon.discovered(answered(body="[]"), ISSUER) == "content"
    assert signon.discovered(answered(body="\"issuer\""), ISSUER) == "content"
    missing = {key: value for key, value in document().items() if key != "authorization_endpoint"}
    assert signon.discovered(answered(body=json.dumps(missing)), ISSUER) == "content"
    for endpoint in (7, "", "not a url", "/protocol/auth", "ftp://idp.example.test/auth"):
        assert signon.discovered(answered(body=json.dumps(document(authorization_endpoint=endpoint))), ISSUER) == "content", endpoint
    assert signon.discovered(answered(body=json.dumps(document(issuer=ISSUER + "/"))), ISSUER) == "issuer-mismatch"
    assert signon.discovered(answered(body=json.dumps(document(issuer="https://other.example.test/realms/ceph"))), ISSUER) == "issuer-mismatch"
    assert signon.discovered(answered(body=json.dumps({key: value for key, value in document().items() if key != "issuer"})), ISSUER) == "issuer-mismatch"
    for data in (b"", b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n", b"not http\r\n\r\n{}"):
        assert signon.discovered(data, ISSUER) == "content", data


def discovering(monkeypatch, wire=None, failure=None, bundle=None):
    monkeypatch.setattr(signon, "discovery_context", lambda unused: Context())
    calls = []

    def connect(address, timeout):
        calls.append((address, timeout))
        if failure is not None:
            raise failure
        return wire

    return signon.discover(ISSUER, bundle, connect=connect), calls


def test_discover_reads_the_one_document_and_names_the_stage_it_fails_at(monkeypatch):
    wire = Wire(answered())
    assert discovering(monkeypatch, wire) == (None, [(("idp.example.test", 443), signon.CONNECT_TIMEOUT)])
    assert wire.sent == b"GET /realms/ceph/.well-known/openid-configuration HTTP/1.0\r\nHost: idp.example.test\r\nAccept: application/json\r\n\r\n"
    assert discovering(monkeypatch, Wire(answered("404 Not Found")))[0] == "status 404"
    assert discovering(monkeypatch, Wire(answered(types=("text/plain",))))[0] == "content"
    assert discovering(monkeypatch, Wire(answered(body=json.dumps(document(issuer="https://x.example.test")))))[0] == "issuer-mismatch"
    assert discovering(monkeypatch, Wire(b"x" * (signon.DISCOVERY_LIMIT + 1)))[0] == "oversize"
    assert discovering(monkeypatch, Wire(b"x" * signon.DISCOVERY_LIMIT))[0] == "content"
    assert discovering(monkeypatch, failure=socket.gaierror(-2, "Name or service not known"))[0] == "dns"
    assert discovering(monkeypatch, failure=ConnectionRefusedError())[0] == "connect"
    assert discovering(monkeypatch, failure=OSError("unreachable"))[0] == "connect"
    assert discovering(monkeypatch, failure=socket.timeout("timed out"))[0] == "connect"
    assert discovering(monkeypatch, Wire(b"", failure=OSError("reset")))[0] == "connect"
    assert discovering(monkeypatch, Wire(b"", failure=ssl.SSLCertVerificationError("unknown ca")))[0] == "tls"
    assert discovering(monkeypatch, Wire(b"", failure=ssl.SSLError("handshake")))[0] == "tls"


def test_discover_without_a_usable_bundle_never_connects():
    def connect(address, timeout):
        pytest.fail("connected")

    assert signon.discover(ISSUER, "not a bundle", connect=connect) == "tls"
    context = signon.discovery_context(None)
    assert context.minimum_version == ssl.TLSVersion.TLSv1_2 and context.check_hostname is True and context.verify_mode == ssl.CERT_REQUIRED


def test_discovery_phase_refuses_the_stage_it_names_and_never_returns_a_byte(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    seen = []

    def discover(issuer, bundle):
        seen.append((issuer, bundle))
        return stage[0]

    stage = [None]
    monkeypatch.setattr(signon, "discover", discover)
    native = units.worker(SSOMonitor(request), markers)
    native.bound = ceph_native_units.material(request, "signon_discovery", {"oauth2ProxyCABundle": CA})
    assert native.signon_discovery() == {"changed": False} and seen == [(ISSUER, CA)]
    for failing in ("dns", "connect", "tls", "status 404", "content", "issuer-mismatch", "oversize"):
        stage[0] = failing
        with pytest.raises(ceph_native.NativeFailure, match="^sign-on-provider-unreachable$") as caught:
            native.signon_discovery()
        assert caught.value.native_error["cause"] == "host node-1: " + failing + " " + ISSUER
        assert not any(mark in json.dumps(caught.value.native_error) for mark in SECRET_MARKS)
    unbound = units.worker(SSOMonitor(request), markers)
    unbound.bound = {}
    stage[0] = None
    seen.clear()
    assert unbound.signon_discovery() == {"changed": False} and seen == [(ISSUER, None)]
    outside = units.worker(SSOMonitor(request), markers)
    outside.host = dict(outside.host, name="node-9")
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        outside.signon_discovery()
    gateway_worker = units.worker(monitoring.Monitor(gateway.gateway_request()), units.markers_for(tmp_path / "gateway", gateway.gateway_request()))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        gateway_worker.signon_discovery()


def test_sso_config_and_sso_are_proxy_unit_phases_only(tmp_path):
    request = gateway.gateway_request()
    markers = units.markers_for(tmp_path, request)
    for phase in ("sso_config", "sso"):
        native = units.worker(SSOMonitor(request), markers)
        native.bound = {}
        with pytest.raises(ceph_native.Refusal, match="^request$"):
            getattr(native, phase)()


class SSOTransport(units.Transport):
    """The coordinator's view of a sign-on: each phase in order with the material it held, each host's discovery and proof result, and the
    selected routes with each gateway host's proof of them."""

    def __init__(self, request, proofs, enables=None, *, routes=("cookie",), lacking=None, advance=None, daemons=1, **options):
        rows = [{"host": node, "name": "oauth2-proxy." + node, "service": "oauth2-proxy"} for node in gateway.TWO]
        super().__init__(request, observed={"daemons": rows, "endpoint": {"certificateSHA256": PIN, "reachable": True}, "sso": "oauth2"}, **options)
        self.proofs, self.private, self.routes, self.lacking, self.advance = list(proofs), [], list(routes), lacking or {}, advance
        self.daemons_per_host = daemons
        self.enables = list(enables or [{"changed": True, "pending": False}])
        self.refusals, self.lock = {}, threading.Lock()

    def unit_material(self, phase="unit"):
        return {name: SSO_MATERIAL[name] for name in ceph_native_units.material_names(self.request, phase)}

    def call(self, host, phase, private=None, *, timeout=None):  # pylint: disable=arguments-renamed
        with self.lock:
            self.private.append((host["name"], phase, sorted(private or {})))
        if phase == "signon_discovery":
            assert 0 < timeout <= ceph_core.SSO_CALL
            if host["name"] in self.refusals:
                raise self.refusals[host["name"]]
            return {"changed": False}
        if phase == "sso_config":
            assert 0 < timeout <= ceph_core.SSO_CALL
            result = self.proofs.pop(0)
            if isinstance(result, Exception):
                raise result
            return result
        if phase == "sso":
            assert timeout is None
            return self.enables.pop(0) if len(self.enables) > 1 else self.enables[0]
        if phase == "signon_routes":
            assert 0 < timeout <= ceph_core.READ_BUDGET
            return {"changed": False, "routes": list(self.routes)}
        if phase == "signon_route_proof":
            assert private is None and 0 < timeout <= ceph_core.ROUTE_CALL
            if self.advance is not None:
                self.advance()
            missing = self.lacking.get(host["name"], ())
            served = {name: int(name not in missing and self.daemons_per_host > 0) for name in observability.GATEWAY_ROUTES}
            return {"changed": False, "daemons": self.daemons_per_host, "served": served}
        if phase == "grafana_route":
            return {"changed": False, "routes": []}
        return super().call(host, phase, timeout=timeout)


def phases_of(transport, skip=("observe", "vip_holder")):
    return [(name, phase) for name, phase, unused in transport.private if phase not in skip]


def ticking():
    """A clock the pauses move forward, so a wait that never settles ends at its bound at once."""
    clock = [0.0]
    return (lambda: clock[0]), (lambda seconds: clock.__setitem__(0, clock[0] + seconds)), clock


def test_a_sign_on_apply_runs_discovery_then_the_unit_then_the_proofs_then_the_one_write():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    transport = SSOTransport(request, [{"changed": False, "daemons": 0, "proved": True}, one, one, one], routes=("cookie", "role"))
    pauses = []
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=pauses.append)
    assert result["changed"] is True and result["evidence"]["ready"] is True and result["evidence"]["sso"] == "oauth2" and pauses[0] == ceph_core.SSO_PAUSE
    phases = phases_of(transport)
    assert phases[0] == ("node-1", "prerequisites") and sorted(phases[1:3]) == [("node-1", "signon_discovery"), ("node-2", "signon_discovery")]
    assert phases[3] == ("node-1", "unit")
    assert sorted(phases[4:6]) == sorted(phases[6:8]) == [("node-1", "sso_config"), ("node-2", "sso_config")]
    assert phases[8] == ("node-1", "signon_routes") and sorted(phases[9:11]) == [("node-1", "signon_route_proof"), ("node-2", "signon_route_proof")]
    assert phases[11] == ("node-1", "sso") and "ingress_firewall" not in [phase for unused, phase in phases]
    assert sorted(phases[12:]) == [("node-1", "signon_route_proof"), ("node-2", "signon_route_proof")] and len(phases) == 14
    for name in ("node-1", "node-2"):
        assert (name, "signon_discovery", ["oauth2ProxyCABundle"]) in transport.private
    assert ("node-1", "unit", sorted(SSO_MATERIAL)) in transport.private and ("node-1", "sso_config", ["oauth2ProxyCABundle"]) in transport.private
    assert ("node-1", "sso", []) in transport.private and ("node-1", "signon_routes", []) in transport.private
    assert [phase for unused, phase, unused_private in transport.private[-5:]].count("observe") >= 1
    assert "grafana_route" not in [phase for unused, phase in phases]


def test_a_sign_on_apply_stops_before_any_effect_when_a_proxy_host_cannot_read_the_issuer():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    refusal = ceph_native.NativeFailure("sign-on-provider-unreachable", "image; host package 1", "host node-2: connect " + ISSUER)
    transport = SSOTransport(request, [one, one])
    transport.refusals["node-2"] = refusal
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-provider-unreachable$") as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert caught.value.native_error["cause"] == "host node-2: connect " + ISSUER
    called = [phase for unused, phase, unused_private in transport.private]
    assert called.count("signon_discovery") == 2 and called[0] == "prerequisites"
    assert not {"unit", "sso_config", "sso", "signon_routes", "signon_route_proof", "observe"} & set(called)
    transport = SSOTransport(request, [one, one])
    transport.refusals["node-1"] = ceph_native.RemoteRefusal("sign-on-provider-unreachable")
    with pytest.raises(ceph_native.Refusal):
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert "unit" not in [phase for unused, phase, unused_private in transport.private]
    transport = SSOTransport(request, [one, one])
    transport.call = lambda host, phase, private=None, *, timeout=None: {"changed": True} if phase == "signon_discovery" else None
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)


def test_sso_enable_stops_at_a_proof_that_never_settles_and_at_a_skip(tmp_path):
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    for proofs in ([ceph_native.RemoteRefusal("sso-unverified")], [one, dict(one, proved=False)] * 40):
        transport = SSOTransport(request, proofs)
        clock = iter(range(0, 10000, 31))
        with pytest.raises(ceph_native.Refusal):
            ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
        called = [phase for unused, phase, unused_private in transport.private]
        assert "sso" not in called and "signon_routes" not in called and "signon_route_proof" not in called and "unit" in called
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    native = applying(monitor, markers)
    native.unit()
    native.bound = ceph_native_units.material(request, "sso", {})
    assert native.sso() == {"changed": True, "pending": False} and native.sso() == {"changed": False, "pending": False}
    assert monitor.mutations.count(("dashboard", "sso", "enable", "oauth2")) == 1
    assert not any("status" in command or "disable" in command for command in monitor.log)
    fresh = units.markers_for(tmp_path / "fresh", request)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        applying(SSOMonitor(request), fresh).sso()


def test_sso_enable_waits_within_the_window_for_the_orchestrator_to_list_a_running_proxy(tmp_path):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request, starting=2)
    native = applying(monitor, markers)
    native.unit()
    native.bound = ceph_native_units.material(request, "sso", {})
    assert native.sso() == {"changed": False, "pending": True} and native.sso() == {"changed": False, "pending": True}
    assert ("dashboard", "sso", "enable", "oauth2") not in monitor.mutations and monitor.enabled is False
    assert native.sso() == {"changed": True, "pending": False} and monitor.enabled is True
    one = {"changed": False, "daemons": 1, "proved": True}
    pending = {"changed": False, "pending": True}
    transport = SSOTransport(request, [one, one], [pending, pending, {"changed": True, "pending": False}])
    pauses = []
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=pauses.append)
    assert result["changed"] is True and result["evidence"]["sso"] == "oauth2" and pauses[:2] == [ceph_core.SSO_PAUSE] * 2
    assert [phase for unused, phase, unused_private in transport.private].count("sso") == 3
    clock = iter(range(0, 100000, 7))
    transport = SSOTransport(request, [one, one], [pending])
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    for bad in ({"changed": True, "pending": True}, {"changed": False}, {"changed": False, "pending": 1}):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.execute(request, "apply", "d" * 64, SSOTransport(request, [one, one], [bad]), now=lambda: 0, pause=lambda seconds: None)


def test_sso_enable_whose_reply_a_restart_lost_is_read_back_and_never_sent_again(tmp_path):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    native = applying(monitor, markers)
    native.unit()
    native.bound = ceph_native_units.material(request, "sso", {})
    answer, state = native.run, {"gid": 4100, "fired": 0}

    def restarting(argv, data=b"", timeout=60):
        command = tuple(argv[argv.index("--format") + 2:]) if "--format" in argv else ()
        if command == ("mgr", "dump"):
            return 0, json.dumps({"available": True, "active_gid": state["gid"]}).encode()
        if command == ("dashboard", "sso", "enable", "oauth2") and not state["fired"]:
            state["fired"] += 1
            answer(argv, data, timeout)
            state["gid"] += 1
            return 1, ceph_native.NativeOutput(b"", b"Error ETIMEDOUT: the manager connection was reset")
        return answer(argv, data, timeout)

    native.run = restarting
    assert native.sso() == {"changed": True, "pending": False}
    assert state["fired"] == 1 and monitor.enabled is True
    assert monitor.mutations.count(("dashboard", "sso", "enable", "oauth2")) == 1


def test_a_sign_on_readiness_wait_that_ends_unenforced_refuses_sign_on_unenforced():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    for blocking in (["enforce@mgmt-gateway"], ["tls@mgmt-gateway"], ["tls@mgmt-gateway", "enforce@mgmt-gateway"]):
        now, pause, unused = ticking()
        transport = SSOTransport(request, [one, one], ready_after=10 ** 6, blocking=blocking)
        with pytest.raises(ceph_native.Refusal, match="^sign-on-unenforced$"):
            ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
        assert [phase for unused_name, phase, unused_private in transport.private].count("sso") == 1
    for blocking in (["stable@oauth2-proxy"], ["enforce@mgmt-gateway", "sso@oauth2-proxy"], ["enforce@mgmt-gateway", "stable@oauth2-proxy"], []):
        now, pause, unused = ticking()
        transport = SSOTransport(request, [one, one], ready_after=10 ** 6, blocking=blocking)
        with pytest.raises(ceph_core.ReadinessRefusal, match="^not-ready$") as refused:
            ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
        assert refused.value.checks == blocking


def prove(transport, request=None, now=lambda: 0, pause=lambda seconds: None):
    request = transport.request if request is None else request
    bootstrap = request["hosts"][0]
    routes = ceph_core.SignOnRoutes(request, bootstrap, transport)
    routes.prove(now, pause)
    return routes


def test_the_sign_on_route_proof_passes_when_every_gateway_daemon_renders_each_selected_correction():
    request = sso_request()
    transport = SSOTransport(request, [], routes=("cookie", "role"))
    routes = prove(transport)
    assert routes.routes == ["cookie", "role"] and routes.lacking is None
    assert phases_of(transport)[0] == ("node-1", "signon_routes")
    assert sorted(phases_of(transport)[1:]) == [("node-1", "signon_route_proof"), ("node-2", "signon_route_proof")]
    assert routes(100, lambda: 0) == [] and routes(0, lambda: 0) is None
    quiet = SSOTransport(request, [], routes=())
    prove(quiet)
    assert phases_of(quiet) == [("node-1", "signon_routes")]
    for produced in ({"changed": False, "routes": ["token"]}, {"changed": False, "routes": ["role", "cookie"]},
                     {"changed": False, "routes": ["cookie", "cookie"]}, {"changed": True, "routes": []}, {"routes": []}, {"changed": False}):
        bad = SSOTransport(request, [])
        bad.call = lambda host, phase, private=None, *, timeout=None, value=produced: value
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            prove(bad)


def test_the_sign_on_route_proof_refuses_sign_on_unprotected_naming_the_host_and_correction_after_the_window(tmp_path):
    request = sso_request()
    now, pause, clock = ticking()
    transport = SSOTransport(request, [], routes=("cookie", "role"), lacking={"node-2": ("role",), "node-1": ("cookie",)},
                             advance=lambda: clock.__setitem__(0, clock[0] + 100))
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-unprotected$") as caught:
        prove(transport, now=now, pause=pause)
    assert caught.value.native_error["cause"] == "host node-1: correction cookie"
    assert clock[0] > ceph_core.SSO_WINDOW and phases_of(transport).count(("node-1", "signon_routes")) == 1
    assert phases_of(transport).count(("node-2", "signon_route_proof")) >= 2
    now, pause, clock = ticking()
    transport = SSOTransport(request, [], routes=("cookie",), lacking={"node-2": ("cookie",)}, advance=lambda: clock.__setitem__(0, clock[0] + 100))
    with pytest.raises(ceph_native.NativeFailure) as caught:
        prove(transport, now=now, pause=pause)
    assert caught.value.native_error["cause"] == "host node-2: correction cookie"
    now, pause, clock = ticking()
    transport = SSOTransport(request, [], routes=("cookie",), daemons=0, advance=lambda: clock.__setitem__(0, clock[0] + 100))
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-unprotected$") as caught:
        prove(transport, now=now, pause=pause)
    assert caught.value.native_error["cause"] == "host none: correction daemon count"


def test_an_unprotected_gateway_is_refused_before_the_single_sign_on_write():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    now, pause, clock = ticking()
    transport = SSOTransport(request, [one, one], routes=("cookie",), lacking={"node-1": ("cookie",)},
                             advance=lambda: clock.__setitem__(0, clock[0] + 100))
    with pytest.raises(ceph_native.NativeFailure, match="^sign-on-unprotected$"):
        ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
    called = [phase for unused, phase, unused_private in transport.private]
    assert "sso" not in called and "observe" not in called and "unit" in called and "signon_route_proof" in called
    assert called.index("signon_routes") > max(index for index, phase in enumerate(called) if phase == "sso_config")


def test_a_gateway_that_renders_the_corrections_late_is_waited_for_within_the_window():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    now, pause, clock = ticking()
    transport = SSOTransport(request, [one, one], routes=("cookie",), lacking={"node-2": ("cookie",)})
    rounds = []

    def advance():
        rounds.append(len(rounds))
        if len(rounds) >= 6:
            transport.lacking = {}

    transport.advance = advance
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
    assert result["evidence"]["ready"] is True and len(rounds) >= 6
    called = [phase for unused, phase, unused_private in transport.private]
    assert called.index("sso") > max(index for index, phase in enumerate(called) if phase == "signon_route_proof" and index < called.index("sso"))


def test_a_ready_sign_on_whose_gateway_renders_nothing_is_refused_within_the_readiness_wait():
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    now, pause, clock = ticking()
    transport = SSOTransport(request, [one, one], routes=("cookie",))
    state = {"rounds": 0}
    base = transport.call

    def call(host, phase, private=None, *, timeout=None):
        if phase == "signon_route_proof":
            state["rounds"] += 1
            if state["rounds"] > 2:
                clock[0] += 100
                return {"changed": False, "daemons": 1, "served": dict.fromkeys(observability.GATEWAY_ROUTES, 0)}
        return base(host, phase, private, timeout=timeout)

    transport.call = call
    with pytest.raises(ceph_core.ReadinessRefusal) as refused:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=now, pause=pause)
    assert refused.value.checks == [ceph_core.ROUTE_ENTRY]


def upstream_oauth2_proxy(spec):
    """OAuth2ProxySpec.validate's bound-leaf rules, mirrored: a non-empty client_secret, and a cookie_secret whose URL-safe base64
    decoding, or its UTF-8 text when that decoding raises binascii.Error, is 16, 24 or 32 bytes; any other error propagates."""
    if not isinstance(spec.get("client_secret"), str) or not spec["client_secret"].strip():
        return False
    cookie = spec.get("cookie_secret")
    if cookie is None:
        return True
    if not isinstance(cookie, str):
        return False
    try:
        length = len(base64.urlsafe_b64decode(cookie))
    except binascii.Error:
        length = len(cookie.encode("utf-8"))
    return length in (16, 24, 32)


def test_every_bound_placeholder_passes_the_proxy_spec_validation(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    native = applying(monitor, markers)
    proxies = [group[0] for group in native.unit_groups() if group[0]["service_type"] == "oauth2-proxy"]
    assert len(proxies) == 1 and upstream_oauth2_proxy(proxies[0]["spec"])
    for kind, path, unused in signon.LEAVES:
        if kind == "oauth2-proxy" and path.startswith("/spec/"):
            assert upstream_oauth2_proxy({"client_secret": "-", path[len("/spec/"):]: ceph_native_units.placeholder(path)})

    def contained(self, image, user, script, arguments=(), **options):
        del self, image, user, script, options
        groups = json.loads(arguments[0])
        if isinstance(groups, dict):
            return groups  # the gateway template read: its paths stand in for template text and witness no defect
        if not all(upstream_oauth2_proxy(value["spec"]) for group in groups for value in group if value["service_type"] == "oauth2-proxy"):
            raise ceph_native.Refusal("native-output")
        return groups

    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", contained)
    assert native.unit() == {"changed": True}
    proxy = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")][0]
    assert upstream_oauth2_proxy(proxy["spec"]) and proxy["spec"]["cookie_secret"] == COOKIE


def test_a_cookie_secret_cephadm_refuses_refuses_as_material_before_any_effect():
    request = sso_request()
    admitted = ("x" * 32, base64.urlsafe_b64encode(b"k" * 32).decode(), base64.urlsafe_b64encode(b"k" * 16).decode(), "abc:defghijklmno", COOKIE)
    refused = ("0123456789abcdef", "x" * 31, "x" * 33, base64.urlsafe_b64encode(b"k" * 20).decode())
    for cookie in admitted + refused:
        assert upstream_oauth2_proxy({"client_secret": CLIENT, "cookie_secret": cookie}) is (cookie in admitted)
        material = dict(SSO_MATERIAL, oauth2ProxyCookieSecret=cookie)
        if cookie in admitted:
            assert ceph_native_units.material(request, "unit", material) == material
        else:
            with pytest.raises(ceph_native.Refusal, match="^material$"):
                ceph_native_units.material(request, "unit", material)
    with pytest.raises(ValueError):
        upstream_oauth2_proxy({"client_secret": CLIENT, "cookie_secret": "é" * 8})
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native_units.material(request, "unit", dict(SSO_MATERIAL, oauth2ProxyCookieSecret="é" * 8))


def test_a_line_naming_the_skip_that_is_not_its_bare_assignment_is_unproved():
    assert signon.rendered_skip('provider = "oidc"\nssl_insecure_skip_verify = false\n') == "verified"
    for line in ('"ssl_insecure_skip_verify" = true', "'ssl_insecure_skip_verify' = true", "SSL_INSECURE_SKIP_VERIFY = true",
                 "ssl-insecure-skip-verify = true", "ssl_insecure_skip_verify true", "[ssl_insecure_skip_verify]",
                 "ssl_insecure_skip_verify_x = false", "ssl_insecure_skip_verify = true # comment"):
        assert signon.rendered_skip('provider = "oidc"\n' + line + "\n") == "unproved", line


class Concurrent(SSOTransport):
    """A sign-on transport whose every proof call waits for the others of its round: a round that is not concurrent breaks the barrier."""

    def __init__(self, request, size, results, phase="sso_config"):
        super().__init__(request, [])
        self.barrier, self.results, self.timeouts, self.lock, self.phase = threading.Barrier(size, timeout=10), results, [], threading.Lock(), phase

    def call(self, host, phase, private=None, *, timeout=None):
        assert phase == self.phase
        self.barrier.wait()
        with self.lock:
            self.private.append((host["name"], phase, sorted(private or {})))
            self.timeouts.append(timeout)
            return dict(self.results)


def largest(bound=11):
    # The cluster execution budget's largest cluster, cluster.go's maxClusterHosts.
    request = sso_request()
    names = ["node-" + str(number) for number in range(1, bound + 1)]
    request["hosts"] = [dict(request["hosts"][0], name=name) for name in names]
    proxy = signon.document_of(request["service"])
    proxy["placement"] = {"count": bound, "hosts": names}
    return request, bound


def test_the_proof_of_the_largest_placement_is_one_concurrent_round_within_the_window():
    request, bound = largest()
    one = {"changed": False, "daemons": 1, "proved": True}
    transport = Concurrent(request, bound, one)
    deadline = ceph_core.sso_proof(request, transport, lambda: 0, lambda seconds: pytest.fail("a proved round pauses"))
    assert deadline == ceph_core.SSO_WINDOW and transport.timeouts == [ceph_core.SSO_CALL] * bound
    assert bound * ceph_core.SSO_CALL > ceph_core.SSO_WINDOW >= ceph_core.SSO_CALL
    unproved = Concurrent(request, bound, dict(one, proved=False))
    clock = iter(range(0, 10000, 150))
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        ceph_core.sso_proof(request, unproved, lambda: next(clock), lambda seconds: None)
    assert len(unproved.timeouts) % bound == 0 and len(unproved.timeouts) >= bound


def test_the_discovery_of_the_largest_placement_is_one_concurrent_round():
    request, bound = largest()
    transport = Concurrent(request, bound, {"changed": False}, phase="signon_discovery")
    ceph_core.signon_discovery(request, transport)
    assert transport.timeouts == [ceph_core.SSO_CALL] * bound
    assert [phase for unused, phase, unused_private in transport.private] == ["signon_discovery"] * bound
    assert all(private == ["oauth2ProxyCABundle"] for unused, unused_phase, private in transport.private)


def test_the_route_proof_of_the_largest_gateway_is_one_concurrent_round():
    request, bound = largest()
    names = ["node-" + str(number) for number in range(1, bound + 1)]
    request["service"]["signOn"].update(count=bound, hosts=sorted(names))
    served = {"changed": False, "daemons": 1, "served": dict.fromkeys(observability.GATEWAY_ROUTES, 1)}
    transport = Concurrent(request, bound, served, phase="signon_route_proof")
    routes = ceph_core.SignOnRoutes(request, request["hosts"][0], transport)
    routes.routes = ["cookie"]
    assert routes.round(ceph_core.ROUTE_CALL) is True and transport.timeouts == [ceph_core.ROUTE_CALL] * bound


def test_signon_phases_read_only_a_sign_on_unit(tmp_path):
    request = gateway.gateway_request()
    native = units.worker(monitoring.Monitor(request), units.markers_for(tmp_path, request))
    for phase in ("signon_routes", "signon_route_proof"):
        with pytest.raises(ceph_native.Refusal):
            getattr(native, phase)()
    proxy = sso_request()
    proxy["service"]["signOn"]["hosts"], proxy["service"]["signOn"]["count"] = ["node-2"], 1
    outside = units.worker(SSOMonitor(proxy), units.markers_for(tmp_path / "proxy", proxy))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        outside.signon_route_proof()


def every_host_slot(request):
    """The cluster's frozen oauth2-proxy entry: every cluster host, whether or not a CephSignOn exists (an added sign-on moves no request)."""
    request["delegated"] = [{"hosts": sorted(entry["name"] for entry in request["hosts"]), "name": "oauth2-proxy"}]
    return request


def test_a_proxy_placed_within_the_clusters_every_host_slot_is_valid_and_one_outside_it_is_not():
    request = every_host_slot(sso_request())
    assert len(request["delegated"][0]["hosts"]) > len(gateway.TWO)
    assert ceph_native.validate(copy.deepcopy(request)) is not None
    request["service"]["documents"][0]["placement"]["hosts"] = ["node-1", "node-9"]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(request)
    plain = gateway.gateway_request()
    plain["delegated"] = [dict(entry, hosts=sorted(host["name"] for host in plain["hosts"])) if entry["name"] == "mgmt-gateway" else entry
                          for entry in plain["delegated"]]
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(plain)


def test_the_cluster_tolerates_an_oauth2_proxy_only_on_a_host_its_sign_on_record_placed():
    request = every_host_slot(dict(units.cluster(), action="cluster"))
    daemon = {"daemon_type": "oauth2-proxy", "hostname": "node-1", "service_name": "oauth2-proxy"}
    for signed_on in (frozenset(), frozenset({"node-2"})):
        with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
            ceph_native.delegated_daemon(daemon, request, signed_on)
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        ceph_native.delegated_daemon(daemon, request)
    ceph_native.delegated_daemon(daemon, request, frozenset({"node-1"}))
    crash = {"daemon_type": "crash", "hostname": "node-1", "service_name": "crash"}
    ceph_native.delegated_daemon(crash, dict(request, delegated=[{"hosts": ["node-1"], "name": "crash"}]))


def test_the_sign_on_hosts_come_only_from_its_attempt_record_under_this_clusters_scope():
    scope = {"version": "ceph-bootstrap-recovery-v1", "cluster": {"fsid": FSID}}
    service = {"placement": {"hosts": ["node-1", "node-2"]}, "service_type": "oauth2-proxy", "spec": {"client_id": "ceph"}}
    assert ceph_native.signed_on_hosts(None, scope) == frozenset()
    assert ceph_native.signed_on_hosts({"scope": scope, "service": service}, scope) == frozenset({"node-1", "node-2"})
    for record in ({"scope": dict(scope, cluster={"fsid": "other"}), "service": service},
                   {"scope": scope, "service": dict(service, service_type="mgmt-gateway")},
                   {"scope": scope, "service": dict(service, placement={"hosts": []})}, {"scope": scope, "service": service, "extra": 1},
                   {"scope": scope, "service": dict(service, placement={"hosts": ["../node"]})}, ["not", "a", "record"]):
        with pytest.raises(ceph_native.Refusal, match="^ownership$"):
            ceph_native.signed_on_hosts(record, scope)


class SignedOnCluster:
    """The cluster observation's view of the bootstrap host: its request, its bootstrap scope and the sign-on's attempt record."""

    def __init__(self, record):
        self.request, self.record, self.reads = every_host_slot(dict(units.cluster(), action="cluster")), record, []
        self.markers = self

    def read(self, name):
        self.reads.append(name)
        return self.record

    def recovery_scope(self):
        return {"version": "ceph-bootstrap-recovery-v1", "cluster": {"fsid": FSID}}


def test_the_cluster_observation_reads_the_sign_on_record_only_for_an_oauth2_proxy_daemon():
    placed = {"scope": SignedOnCluster(None).recovery_scope(), "service": {"placement": {"hosts": ["node-1"]}, "service_type": "oauth2-proxy"}}
    proxy = {"daemon_type": "oauth2-proxy", "hostname": "node-1", "service_name": "oauth2-proxy"}
    owned = SignedOnCluster(placed)
    ceph_native.Worker.tolerate_delegated(owned, proxy)
    assert owned.reads == ["service-oauth2-proxy-attempt.json"]
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        ceph_native.Worker.tolerate_delegated(owned, dict(proxy, hostname="node-2"))
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        ceph_native.Worker.tolerate_delegated(SignedOnCluster(None), proxy)
    crash = SignedOnCluster(placed)
    crash.request["delegated"].append({"hosts": ["node-1"], "name": "crash"})
    ceph_native.Worker.tolerate_delegated(crash, {"daemon_type": "crash", "hostname": "node-1", "service_name": "crash"})
    assert crash.reads == []
