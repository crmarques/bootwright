"""An object gateway's STS trust of web identities: the switch frozen in the rgw document, the key bound and spliced into the apply stream only,
the stored key compared in constant time and never recorded, the configuration database's and each running daemon's key compared without
echo, and no host mount round. Every refusal, native diagnostic, record, evidence and command line is checked for the key.

The scripted monitor is the LDAP one: cephadm's spec.config options set at `client.rgw.<service_id>` and a manager's single-option
`config show`.
"""

from __future__ import annotations

import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_object
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateway
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway_ldap as ldap

KEY = "Sts0Synthetic1Ky"
OTHER_KEY = "Another0StsKey16"
SECTION = ldap.SECTION
PLACEHOLDER = "bootwright-bound:/spec/config/rgw_sts_key"
PROVIDERS = [{"issuerURL": "https://sso.example.test/realms/ceph", "name": "public-sso"}]
native_boundaries = gateway.native_boundaries
served = ldap.served


def sts_request(ingress=False, providers=True, with_ldap=False):
    request = ldap.ldap_request(ingress=ingress) if with_ldap else gateway.tls_request(ingress)
    rgw = request["gateway"]["documents"][0]
    rgw["spec"]["config"] = dict(rgw["spec"].get("config", {}), rgw_s3_auth_use_sts="true")
    request["gateway"].setdefault("authentication", {})["sts"] = dict({"keySecretRef": "gateway-sts-key"}, **({"providers": PROVIDERS} if providers else {}))
    return request


def held(request, phase):
    material = {"gateway_service": {"gatewayCertificate": gateway.CERT, "gatewayKey": gateway.KEY},
                "observe": {"gatewayCertificate": gateway.CERT}}.get(phase, {})
    if phase == "gateway_service":
        material = dict(material, rgwSTSKey=KEY)
        if "ldap" in request["gateway"]["authentication"]:
            material = dict(ldap.held(request, phase), rgwSTSKey=KEY)
    return material


def leaked(value):
    if isinstance(value, bytes):
        value = value.decode("utf-8", errors="replace")
    return [mark for mark in (KEY, OTHER_KEY) if mark in value]


def sts_worker(monitor, markers, phase="observe"):
    native = gateway.worker(monitor, markers)
    native.tls = held(monitor.request, phase)
    return native


def apply_sts(monitor, markers):
    return [getattr(sts_worker(monitor, markers, phase), phase)() for phase in ceph_native_object.APPLY_PHASES]


def observe_sts(monitor, markers):
    native = sts_worker(monitor, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


def ready_sts(tmp_path, request=None):
    request = request or sts_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(ldap.LDAPMonitor(request))
    apply_sts(monitor, markers)
    monitor.deploy()
    return markers, monitor


def no_key_on_disk(root):
    for directory, unused, files in os.walk(str(root)):
        for name in files:
            with open(os.path.join(directory, name), "rb") as handle:
                assert not leaked(handle.read()), name


def with_sts(change, request=None):
    request = request or sts_request()
    change(request["gateway"]["documents"][0], request)
    return request


INVALID = {
    "an empty authentication": lambda: with_sts(lambda rgw, request: request["gateway"].update(authentication={})),
    "STS without its switch": lambda: with_sts(lambda rgw, request: rgw["spec"].pop("config")),
    "the switch off": lambda: with_sts(lambda rgw, request: rgw["spec"]["config"].update(rgw_s3_auth_use_sts="false")),
    "the switch without STS": lambda: with_sts(lambda rgw, request: request["gateway"].pop("authentication")),
    "a frozen STS key": lambda: with_sts(lambda rgw, request: rgw["spec"]["config"].update(rgw_sts_key=KEY)),
    "an LDAP option without LDAP": lambda: with_sts(lambda rgw, request: rgw["spec"]["config"].update(rgw_s3_auth_use_ldap="true")),
    "custom configs without LDAP": lambda: with_sts(lambda rgw, request: rgw.update(custom_configs=ldap.mounts())),
    "an extra STS member": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].update(key=KEY)),
    "no key Secret": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].pop("keySecretRef")),
    "an empty provider list": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].update(providers=[])),
    "a cleartext issuer": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].update(
        providers=[{"issuerURL": "http://sso.example.test", "name": "public-sso"}])),
    "unsorted providers": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].update(providers=[
        {"issuerURL": "https://b.example.test", "name": "b"}, {"issuerURL": "https://a.example.test", "name": "a"}])),
    "a provider member Bootwright does not freeze": lambda: with_sts(lambda rgw, request: request["gateway"]["authentication"]["sts"].update(
        providers=[dict(PROVIDERS[0], thumbprint="x")])),
    "a plaintext endpoint": lambda: with_sts(lambda rgw, request: (rgw["spec"].pop("ssl"), request["gateway"].update(bindings=[]))),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_sts_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


@pytest.mark.parametrize("ingress", [False, True])
@pytest.mark.parametrize("providers", [False, True])
@pytest.mark.parametrize("with_ldap", [False, True])
def test_sts_request_admits_the_frozen_shapes(ingress, providers, with_ldap):
    request = sts_request(ingress, providers, with_ldap)
    assert ceph_native.validate(request) is request


def test_the_sts_key_is_admitted_only_to_the_service_apply(monkeypatch):
    reached = []
    for phase in ceph_native_object.APPLY_PHASES:
        monkeypatch.setattr(ceph_native_object.ObjectWorker, phase, lambda self, phase=phase: reached.append((phase, self.tls)) or {"changed": False})
    request = sts_request()

    def dispatch(phase, private):
        return ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": private})

    applied = held(request, "gateway_service")
    dispatch("gateway_service", applied)
    assert reached == [("gateway_service", applied)]
    for phase, private in (("gateway_service", held(request, "observe")), ("observe", applied), ("gateway_zone", {"rgwSTSKey": KEY}),
                           ("gateway_service", dict(applied, rgwSTSKey=KEY[:15])), ("gateway_service", dict(applied, rgwSTSKey=KEY + "a")),
                           ("gateway_service", dict(applied, rgwSTSKey=KEY[:15] + "-")), ("gateway_service", dict(applied, rgwSTSKey=KEY + "\n")),
                           ("gateway_service", dict(applied, rgwSTSKey=KEY[:15] + "é"))):
        with pytest.raises(ceph_native.Refusal, match="^material$") as caught:
            dispatch(phase, private)
        assert not leaked(str(caught.value))


@pytest.mark.parametrize("ingress", [False, True])
def test_the_sts_key_reaches_only_the_apply_stream(tmp_path, monkeypatch, ingress):
    contained = []
    monkeypatch.setattr(gateway.ceph_host_runtime.HostRuntime, "contained",
                        lambda self, image, user, script, arguments=(), **options: contained.append(arguments) or json.loads(arguments[0]))
    request = sts_request(ingress=ingress)
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(ldap.LDAPMonitor(request))
    assert apply_sts(monitor, markers)[-1] == {"changed": True}
    applies = [data for tool, command, data in monitor.log if command[:2] == ("orch", "apply")]
    assert len(applies) == 1 and leaked(applies[0]) == [KEY]
    stream = [json.loads(part) for part in applies[0].split(b"---\n")]
    assert stream[0]["spec"]["config"] == {"rgw_s3_auth_use_sts": "true", "rgw_sts_key": KEY} and "custom_configs" not in stream[0]
    assert not any(leaked(" ".join(command)) for tool, command, unused in monitor.log)
    assert contained and not any(leaked(" ".join(arguments)) for arguments in contained)
    no_key_on_disk(tmp_path)
    for name in ("service-rgw.objects-attempt.json", "service-rgw.objects-complete.json", "gateway-objects-inputs.json"):
        assert PLACEHOLDER in json.dumps(markers.read(name)), name


def test_sts_replay_compares_the_stored_key_and_applies_nothing(tmp_path):
    request = sts_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(ldap.LDAPMonitor(request))
    apply_sts(monitor, markers)
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    monitor.mutations.clear()
    assert sts_worker(monitor, markers, "gateway_service").gateway_service() == {"changed": False}
    assert monitor.mutations == [] and markers.read("service-rgw.objects-complete.json") is not None


@pytest.mark.parametrize("phase, stored", [("gateway_service", OTHER_KEY), ("observe", "short"), ("observe", "sixteen-chars-x!")])
def test_a_stored_key_other_than_the_bound_one_refuses_redacted(tmp_path, served, phase, stored):
    markers, monitor = ready_sts(tmp_path)
    monitor.specs["rgw.objects"]["spec"]["config"]["rgw_sts_key"] = stored
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    monitor.mutations.clear()
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-credential$") as caught:
        native = sts_worker(monitor, markers, phase)
        native.gateway_service() if phase == "gateway_service" else native.observe_resource(native.empty_evidence(), True)
    text = json.dumps(caught.value.native_error)
    assert monitor.mutations == [] and not leaked(text) and stored not in text
    assert caught.value.native_error["cause"] == "rgw.objects /spec/config/rgw_sts_key"


def test_readiness_compares_the_stored_and_running_key_without_echo(tmp_path, served):
    markers, monitor = ready_sts(tmp_path)
    assert [entry["value"] for entry in monitor.config if entry["name"] == "rgw_sts_key"] == [KEY]
    ready, blocking, evidence = observe_sts(monitor, markers)
    assert ready is True and blocking == []
    assert {option for unused, option in monitor.shows} == {"rgw_s3_auth_use_sts", "rgw_sts_key", "rgw_s3_auth_order", "rgw_s3_auth_use_rados"}
    assert not leaked(json.dumps(evidence))
    monitor.reported["client.rgw.objects.node-2.abcdef"]["rgw_sts_key"] = "sts"
    ready, blocking, unused = observe_sts(monitor, markers)
    assert ready is False and blocking == ["sts@rgw.objects"]
    del monitor.reported["client.rgw.objects.node-2.abcdef"]["rgw_sts_key"]
    assert observe_sts(monitor, markers)[:2] == (False, ["sts@rgw.objects"])


@pytest.mark.parametrize("change", ["another key", "a daemon section", "a mask", "twice"])
def test_a_stored_key_option_other_than_the_bound_one_refuses_without_echo(tmp_path, served, change):
    markers, monitor = ready_sts(tmp_path)
    entry = next(entry for entry in monitor.config if entry["name"] == "rgw_sts_key")
    {"another key": lambda: entry.update(value=OTHER_KEY), "a daemon section": lambda: entry.update(section=SECTION + ".node-1.abcdef"),
     "a mask": lambda: entry.update(mask="host:node-1"), "twice": lambda: monitor.config.append(dict(entry))}[change]()
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$") as caught:
        observe_sts(monitor, markers)
    text = json.dumps(caught.value.native_error)
    assert not leaked(text) and caught.value.native_error["cause"].startswith("rgw_sts_key at client.rgw.objects")


def test_an_sts_option_cephadm_has_not_set_yet_is_pending(tmp_path, served):
    markers, monitor = ready_sts(tmp_path)
    monitor.config = [entry for entry in monitor.config if entry["name"] != "rgw_sts_key"]
    assert observe_sts(monitor, markers)[:2] == (False, ["sts@rgw.objects"])
    assert ceph_core.blocking_entry("sts@rgw.objects")


@pytest.mark.parametrize("order", ["external, local", "sts, external"])
def test_a_daemon_whose_sign_in_order_lost_sts_or_local_users_refuses(tmp_path, served, order):
    markers, monitor = ready_sts(tmp_path)
    monitor.reported["client.rgw.objects.node-1.abcdef"]["rgw_s3_auth_order"] = order
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$"):
        observe_sts(monitor, markers)
    monitor.reported["client.rgw.objects.node-1.abcdef"]["rgw_s3_auth_order"] = "sts, external, local"
    assert observe_sts(monitor, markers)[0] is True


def test_a_foreign_sts_key_before_the_first_attempt_refuses_by_name_only(tmp_path):
    request = sts_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(ldap.LDAPMonitor(request))
    for phase in ceph_native_object.APPLY_PHASES[:2]:
        getattr(sts_worker(monitor, markers, phase), phase)()
    monitor.mutations.clear()
    monitor.config.append(gateway.settings.entry("rgw_sts_key", OTHER_KEY, section=SECTION))
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$") as caught:
        sts_worker(monitor, markers, "gateway_service").gateway_service()
    assert caught.value.native_error["cause"] == "rgw_sts_key at client.rgw.objects" and not leaked(json.dumps(caught.value.native_error))
    assert markers.read("service-rgw.objects-attempt.json") is None and monitor.mutations == []


class FailingShow(ldap.LDAPMonitor):
    """A manager whose `config show` of the key fails with the key on stdout and nothing on stderr."""

    failed = None

    def run(self, argv, data=b"", timeout=60):
        if "--format" not in argv and argv[-1] == "rgw_sts_key":
            self.failed.append(argv[-2])
            return 1, ceph_native.NativeOutput((KEY + "\n").encode(), b"")
        return super().run(argv, data, timeout)


def test_a_failing_native_read_of_the_key_is_redacted(tmp_path, served):
    request = sts_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(FailingShow(request))
    monitor.failed = []
    apply_sts(monitor, markers)
    monitor.deploy()
    with pytest.raises(ceph_native.ManagerRefusal, match="^native-command$") as caught:
        observe_sts(monitor, markers)
    cause = caught.value.native_cause
    assert isinstance(cause, ceph_native.NativeFailure) and cause.native_error and not leaked(json.dumps(cause.native_error))
    assert monitor.failed == ["client.rgw.objects.node-1.abcdef"]


class KeyTransport(ldap.RoundTransport):
    def gateway_auth_material(self):
        return {"rgwSTSKey": KEY}


def test_apply_lends_the_key_to_the_service_apply_and_runs_no_mount_round():
    request = sts_request()
    transport = KeyTransport(request, [ldap.mounted()])
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["ready"] is True and not leaked(json.dumps(result))
    assert not any(call[1] == "gateway_auth_mounts" for call in transport.calls)
    assert transport.private == [held(request, "gateway_service")]


def test_sts_with_ldap_lends_every_leaf_and_proves_the_ldap_mounts(tmp_path, served):
    request = sts_request(with_ldap=True)
    assert set(ceph_native_object.auth_variables(request["gateway"]).values()) == {"rgwLDAPBindPassword", "rgwLDAPCABundle", "rgwSTSKey"}
    markers, monitor = ready_sts(tmp_path, request)
    ready, blocking, unused = observe_sts(monitor, markers)
    assert ready is True and blocking == []
    no_key_on_disk(tmp_path)
