"""The management gateway's oauth2-proxy: its credentials, TLS pair and identity provider CA bundle reach only the apply stream, the deployed
proxy's rendered configuration is proved on each of its hosts, and only then the unit's one dashboard write enables OAuth2 single sign-on.

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

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_native, ceph_native_observability as observability, ceph_native_units,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_mgmt_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_monitoring as monitoring
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_service_units as units

FSID = units.FSID
OAUTH2 = "registry.example.test/ceph/oauth2-proxy@sha256:" + "e" * 64
CA = gateway.pem("CERTIFICATE", b"synthetic identity provider authority for the proxy")
OTHER_CA = gateway.pem("CERTIFICATE", b"a bundle the bound Secret does not hold")
CLIENT = "client-secret-sentinel-0123456789"
COOKIE = "cookie-secret-sentinel-abcdefghi"
PROXY_CERT = gateway.pem("CERTIFICATE", b"synthetic oauth2-proxy leaf certificate")
PROXY_KEY = gateway.pem("PRIVATE KEY", b"oauth2-proxy private key material only")
SSO_MATERIAL = dict(gateway.MATERIAL, oauth2ProxyCABundle=CA, oauth2ProxyClientSecret=CLIENT, oauth2ProxyCookieSecret=COOKIE,
                    oauth2ProxyCertificate=PROXY_CERT, oauth2ProxyKey=PROXY_KEY)
SECRET_MARKS = [line for text in (CA, PROXY_CERT, PROXY_KEY) for line in text.splitlines() if "-----" not in line] + [CLIENT, COOKIE] + gateway.MARKS
CONFIG = "/etc/oauth2-proxy.conf"
CONTAINER = "ceph-" + FSID + "-oauth2-proxy-node-1"
# The gateway tests' identity normalization in place of the frozen image's SDK, autoused here too.
normalization = gateway.normalization


def proxy_document():
    return {"custom_configs": [{"mount_path": observability.OAUTH2_PROXY_CA_PATH}], "placement": {"count": 2, "hosts": list(gateway.TWO)},
            "service_type": "oauth2-proxy",
            "spec": {"client_id": "ceph", "oidc_issuer_url": "https://idp.example.test/realms/ceph", "provider_display_name": "SSO"}}


def sso_request():
    documents = gateway.documents()
    documents[1]["spec"]["enable_auth"] = True
    documents.append(proxy_document())
    request = gateway.gateway_request(unit_documents=documents)
    request["service"]["bindings"] = gateway.bindings() + [{"path": path, "secretRef": ref, "serviceRef": "oauth2-proxy"} for path, ref in (
        ("/custom_configs/0/content", "sso-ca"), ("/spec/client_secret", "sso-client"), ("/spec/cookie_secret", "sso-cookie"),
        ("/spec/ssl_cert", "sso-tls"), ("/spec/ssl_key", "sso-tls"))]
    request["auxiliary"] = copy.deepcopy(gateway.AUXILIARY) + [{"image": OAUTH2, "name": "oauth2_proxy"}]
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
        if command == ("config", "get", "mgr", observability.SSO_OPTION):
            self.log.append(list(argv))
            return 0, json.dumps(self.enabled).encode()
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
    mounts = [{"Source": config_source, "Destination": CONFIG}, {"Source": ca_source, "Destination": observability.OAUTH2_PROXY_CA_PATH}]
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


def proving(request, monitor, markers, bundle=CA):
    native = ceph_native_units.UnitWorker(request, "node-1", run=monitor.run, markers=markers)
    native.bound = ceph_native_units.material(request, "sso_config", {"oauth2ProxyCABundle": bundle})
    return native.sso_config()


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
    assert observability.rendered_skip("ssl_insecure_skip_verify = 'false'") == "verified" and observability.config_path(["--config", CONFIG]) == CONFIG


def test_ca_bundle_reaches_the_container_only_through_custom_configs(tmp_path, host):
    root, conf, ca = host
    request = sso_request()
    markers = units.markers_for(tmp_path / "markers", request)
    monitor = SSOMonitor(request)
    assert gateway.applying(monitor, markers, SSO_MATERIAL).unit() == {"changed": True}
    parts = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")]
    assert [ceph_native_units.service_name(part) for part in parts] == ["ingress.mgmt", "mgmt-gateway", "oauth2-proxy"]
    assert parts[2]["custom_configs"] == [{"content": CA, "mount_path": observability.OAUTH2_PROXY_CA_PATH}]
    assert "custom_configs" not in parts[0] and "custom_configs" not in parts[1]
    ca_marks = [line for line in CA.splitlines() if "-----" not in line]
    stored = monitoring.recorded(markers).decode()
    assert "bootwright-bound:/custom_configs/0/content" in stored
    assert not any(mark in text for mark in ca_marks for text in (stored, json.dumps(monitor.log)))
    observed = units.worker(monitor, markers)
    observed.bound = ceph_native_units.material(request, "observe", dict(gateway.SEEN))
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
    native = gateway.applying(monitor, markers, SSO_MATERIAL)
    assert native.unit() == {"changed": True}
    proxy = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")][2]
    assert proxy["spec"]["client_secret"] == CLIENT and proxy["spec"]["cookie_secret"] == COOKIE
    assert proxy["spec"]["ssl_cert"] == PROXY_CERT and proxy["spec"]["ssl_key"] == PROXY_KEY
    compared = native.bound_specs(native.unit_specs())
    visible = json.dumps([monitor.log, compared]) + monitoring.recorded(markers).decode()
    assert not any(secret in visible for secret in (CLIENT, COOKIE))
    assert compared["oauth2-proxy"]["spec"]["client_secret"] == "bootwright-bound:/spec/client_secret"
    assert gateway.applying(monitor, markers, SSO_MATERIAL).unit() == {"changed": False} and len(monitor.stdin) == 1


def sso_running(request):
    rows = gateway.running(request)
    rows += [gateway.row("keepalived", "ingress.mgmt", node, units.settings.KEEPALIVED, daemon_id="mgmt." + node + ".abcdef") for node in gateway.TWO]
    return rows + [gateway.row("oauth2-proxy", "oauth2-proxy", node, OAUTH2) for node in gateway.TWO]


def test_sso_evidence_has_no_secret(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    assert gateway.applying(monitor, markers, SSO_MATERIAL).unit() == {"changed": True}
    monitor.running = sso_running(request)
    gateway.Probes(monkeypatch, (gateway.LEAF, True))
    ready, blocking, evidence = gateway.observing(monitor, markers)
    assert ready is False and blocking == ["sso@oauth2-proxy"] and evidence["sso"] == ""
    monitor.enabled = True
    ready, blocking, evidence = gateway.observing(monitor, markers)
    assert ready is True and blocking == [] and evidence["sso"] == "oauth2"
    assert not any(mark in json.dumps(evidence) for mark in SECRET_MARKS)
    value = dict(evidence, request="d" * 64, ready=True, exists=True)
    assert ceph_core.evidence(copy.deepcopy(value), "d" * 64, request)["sso"] == "oauth2"
    for bad, against in ((dict(value, sso="saml2"), request), (value, gateway.gateway_request())):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(copy.deepcopy(bad), "d" * 64, against)
    assert not any(command[-5:-1] == ["dashboard", "sso", "enable", "oauth2"] or "status" in command for command in monitor.log)


class SSOTransport(units.Transport):
    """The coordinator's view of an SSO gateway: each phase in order with the material it held, and each host's proof result."""

    def __init__(self, request, proofs, enables=None):
        rows = [{"host": "node-1", "name": "mgmt-gateway.node-1", "service": "mgmt-gateway"},
                {"host": "node-2", "name": "keepalived.mgmt.node-2.abcdef", "service": "ingress.mgmt"},
                {"host": "node-2", "name": "oauth2-proxy.node-2", "service": "oauth2-proxy"}]
        super().__init__(request, observed={"daemons": rows, "endpoint": {"certificateSHA256": gateway.LEAF_SHA256, "reachable": True}, "sso": "oauth2"})
        self.proofs, self.private = list(proofs), []
        self.enables = list(enables or [{"changed": True, "pending": False}])

    def unit_material(self, phase="unit"):
        return {name: SSO_MATERIAL[name] for name in ceph_native_units.material_names(self.request, phase)}

    def call(self, host, phase, private=None, *, timeout=None):  # pylint: disable=arguments-renamed
        self.private.append((host["name"], phase, sorted(private or {})))
        if phase == "sso_config":
            assert 0 < timeout <= ceph_core.SSO_CALL
            result = self.proofs.pop(0)
            if isinstance(result, Exception):
                raise result
            return result
        if phase == "sso":
            assert timeout is None
            return self.enables.pop(0) if len(self.enables) > 1 else self.enables[0]
        result = super().call(host, phase, timeout=timeout)
        return {"changed": False} if phase == "ingress_firewall" else result


def test_sso_enable_runs_once_after_the_proof(tmp_path, monkeypatch):
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
    request = sso_request()
    one = {"changed": False, "daemons": 1, "proved": True}
    transport = SSOTransport(request, [{"changed": False, "daemons": 0, "proved": True}, one, one, one])
    pauses = []
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=pauses.append)
    assert result["evidence"]["ready"] is True and result["evidence"]["sso"] == "oauth2" and pauses[0] == ceph_core.SSO_PAUSE
    phases = [(name, phase) for name, phase, unused in transport.private]
    assert phases[:9] == [("node-1", "prerequisites"), ("node-1", "ingress_firewall"), ("node-2", "ingress_firewall"), ("node-1", "unit"),
                          ("node-1", "sso_config"), ("node-2", "sso_config"), ("node-1", "sso_config"), ("node-2", "sso_config"), ("node-1", "sso")]
    assert ("node-1", "sso_config", ["oauth2ProxyCABundle"]) in transport.private and ("node-1", "sso", []) in transport.private
    for proofs in ([ceph_native.RemoteRefusal("sso-unverified")], [one, dict(one, proved=False)] * 40):
        transport = SSOTransport(request, proofs)
        clock = iter(range(0, 10000, 7))
        with pytest.raises(ceph_native.Refusal):
            ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
        assert "sso" not in [phase for unused, phase, unused_private in transport.private]
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request)
    native = gateway.applying(monitor, markers, SSO_MATERIAL)
    native.unit()
    native.bound = ceph_native_units.material(request, "sso", {})
    assert native.sso() == {"changed": True, "pending": False} and native.sso() == {"changed": False, "pending": False}
    assert monitor.mutations.count(("dashboard", "sso", "enable", "oauth2")) == 1
    assert not any("status" in command or "disable" in command for command in monitor.log)
    fresh = units.markers_for(tmp_path / "fresh", request)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        gateway.applying(SSOMonitor(request), fresh, SSO_MATERIAL).sso()


def test_sso_enable_waits_within_the_window_for_the_orchestrator_to_list_a_running_proxy(tmp_path, monkeypatch):
    request = sso_request()
    markers = units.markers_for(tmp_path, request)
    monitor = SSOMonitor(request, starting=2)
    native = gateway.applying(monitor, markers, SSO_MATERIAL)
    native.unit()
    native.bound = ceph_native_units.material(request, "sso", {})
    assert native.sso() == {"changed": False, "pending": True} and native.sso() == {"changed": False, "pending": True}
    assert ("dashboard", "sso", "enable", "oauth2") not in monitor.mutations and monitor.enabled is False
    assert native.sso() == {"changed": True, "pending": False} and monitor.enabled is True
    monkeypatch.setattr(ceph_native, "supported_transport", lambda value: True)
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
    native = gateway.applying(monitor, markers, SSO_MATERIAL)
    proxies = [group[0] for group in native.unit_groups() if group[0]["service_type"] == "oauth2-proxy"]
    assert len(proxies) == 1 and upstream_oauth2_proxy(proxies[0]["spec"])
    for kind, path, unused in observability.GATEWAY_LEAVES:
        if kind == "oauth2-proxy" and path.startswith("/spec/"):
            assert upstream_oauth2_proxy({"client_secret": "-", path[len("/spec/"):]: ceph_native_units.placeholder(path)})

    def contained(self, image, user, script, arguments=(), **options):
        del self, image, user, script, options
        groups = json.loads(arguments[0])
        if not all(upstream_oauth2_proxy(value["spec"]) for group in groups for value in group if value["service_type"] == "oauth2-proxy"):
            raise ceph_native.Refusal("native-output")
        return groups

    monkeypatch.setattr(ceph_host_runtime.HostRuntime, "contained", contained)
    assert native.unit() == {"changed": True}
    proxy = [json.loads(part) for part in monitor.stdin[0].split(b"---\n")][2]
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
        upstream_oauth2_proxy({"client_secret": CLIENT, "cookie_secret": "\u00e9" * 8})
    with pytest.raises(ceph_native.Refusal, match="^material$"):
        ceph_native_units.material(request, "unit", dict(SSO_MATERIAL, oauth2ProxyCookieSecret="\u00e9" * 8))


def test_a_line_naming_the_skip_that_is_not_its_bare_assignment_is_unproved():
    assert observability.rendered_skip('provider = "oidc"\nssl_insecure_skip_verify = false\n') == "verified"
    for line in ('"ssl_insecure_skip_verify" = true', "'ssl_insecure_skip_verify' = true", "SSL_INSECURE_SKIP_VERIFY = true",
                 "ssl-insecure-skip-verify = true", "ssl_insecure_skip_verify true", "[ssl_insecure_skip_verify]",
                 "ssl_insecure_skip_verify_x = false", "ssl_insecure_skip_verify = true # comment"):
        assert observability.rendered_skip('provider = "oidc"\n' + line + "\n") == "unproved", line
