"""An object gateway's LDAP authentication: the derived options and files in the rgw document, the bind password and CA bundle spliced into the
apply stream only, the stored leaves compared without ever being recorded, the foreign-option check before the first attempt, each running
daemon's readback with local users still last, and the host proof that every container mounts the derived files.

The scripted monitor extends the gateway monitor with what cephadm does with a document's `config` map (`_apply_service_config` sets each
option at `client.rgw.<service_id>` before it deploys a daemon, image cephadm/serve.py:841-905) and a manager's single-option `config show`,
which answers the value as one line and exits 2 (ENOENT) for an option it does not hold.
"""

from __future__ import annotations

import copy
import json
import os
import threading
import uuid

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_native, ceph_native_object, ceph_native_observability, ceph_native_units,
)
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_object_gateway as gateway

FSID = gateway.FSID
PASSWORD = "Synthetic bind pass phrase 2026"
OTHER_PASSWORD = "Another synthetic bind phrase"
CA = gateway.pem("CERTIFICATE", b"synthetic directory issuing authority")
SECTION = "client.rgw.objects"
FILTER = "(&(memberOf=CN=ceph,DC=example,DC=test)(sAMAccountName=@USERNAME@))"
MARKS = [PASSWORD] + [line for line in CA.splitlines() if "-----" not in line]
native_boundaries = gateway.native_boundaries


def options(searched=True):
    value = {"rgw_ldap_binddn": "CN=svc ceph,OU=Service Accounts,DC=example,DC=test", "rgw_ldap_dnattr": "sAMAccountName",
             "rgw_ldap_searchdn": "OU=People,DC=example,DC=test", "rgw_ldap_secret": ceph_native_object.LDAP_BIND,
             "rgw_ldap_uri": "ldaps://dc-01.example.test:636 ldaps://dc-02.example.test", "rgw_s3_auth_use_ldap": "true"}
    if searched:
        value["rgw_ldap_searchfilter"] = FILTER
    return value


def mounts(trusted=True):
    value = [{"mount_path": ceph_native_object.LDAP_BIND}, {"content": ceph_native_object.ldap_conf(trusted), "mount_path": ceph_native_object.LDAP_CONF}]
    return value + ([{"mount_path": ceph_native_object.LDAP_CA}] if trusted else [])


def ldap_request(trusted=True, ingress=False):
    request = gateway.tls_request(ingress)
    rgw = request["gateway"]["documents"][0]
    rgw["spec"]["config"] = options()
    rgw["custom_configs"] = mounts(trusted)
    request["gateway"]["authentication"] = {"ldap": dict({"bindSecretRef": "directory-bind", "server": "directory"},
                                                         **({"trustSecretRef": "directory-ca"} if trusted else {}))}
    return request


def held(request, phase):
    material = {"gateway_service": {"gatewayCertificate": gateway.CERT, "gatewayKey": gateway.KEY},
                "observe": {"gatewayCertificate": gateway.CERT}}.get(phase, {})
    if phase == "gateway_service":
        trusted = "trustSecretRef" in request["gateway"]["authentication"]["ldap"]
        material = dict(material, rgwLDAPBindPassword=PASSWORD, **({"rgwLDAPCABundle": CA} if trusted else {}))
    return material


def leaked(value):
    if isinstance(value, bytes):
        value = value.decode("utf-8", errors="replace")
    return [mark for mark in MARKS if mark in value or json.dumps(mark)[1:-1] in value]


class LDAPMonitor(gateway.Monitor):
    """The gateway monitor with cephadm's spec.config options and each running daemon's reported configuration."""

    def __init__(self, request):
        super().__init__(request)
        self.reported, self.shows = {}, []

    def run(self, argv, data=b"", timeout=60):
        if "--format" not in argv and "config" in argv:
            command = tuple(argv[argv.index("config"):])
            assert command[:2] == ("config", "show") and len(command) == 4
            self.shows.append(command[2:])
            value = self.reported.get(command[2], {}).get(command[3])
            return (2, ceph_native.NativeOutput(b"", b"Error ENOENT")) if value is None else (0, (value + "\n").encode())
        code, output = super().run(argv, data, timeout)
        if self.batches and self.batches[-1] is not None:
            for value in self.batches[-1]:
                for name, option in sorted(value.get("spec", {}).get("config", {}).items()):
                    if not any(entry["name"] == name and entry["section"] == SECTION for entry in self.config):
                        self.config.append(gateway.settings.entry(name, option, section=SECTION))
            self.batches.append(None)
        return code, output

    def deploy(self, hosts=("node-1", "node-2")):
        """Running rgw daemons that read the options cephadm set for their service."""
        self.running = [gateway.daemon(self.request, host) for host in hosts]
        for row in self.running:
            self.reported["client.rgw." + row["daemon_id"]] = {entry["name"]: entry["value"] for entry in self.config if entry["section"] == SECTION}


def ldap_worker(monitor, markers, phase="observe"):
    native = gateway.worker(monitor, markers)
    native.tls = held(monitor.request, phase)
    return native


def apply_ldap(monitor, markers):
    return [getattr(ldap_worker(monitor, markers, phase), phase)() for phase in ceph_native_object.APPLY_PHASES]


def observe_ldap(monitor, markers):
    native = ldap_worker(monitor, markers)
    evidence = native.empty_evidence()
    return native.observe_resource(evidence, True), native.blocking, evidence


@pytest.fixture
def served(monkeypatch):
    monkeypatch.setattr(ceph_native_object, "served", lambda address, number, name: (gateway.LEAF, True))


def ready_ldap(tmp_path, request=None):
    request = request or ldap_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(LDAPMonitor(request))
    apply_ldap(monitor, markers)
    monitor.deploy()
    if len(request["gateway"]["documents"]) == 2:
        monitor.running += gateway.ingress_rows(request)
    return markers, monitor


def with_rgw(change, request=None):
    request = request or ldap_request()
    change(request["gateway"]["documents"][0], request)
    return request


INVALID = {
    "custom configs without authentication": lambda: with_rgw(lambda rgw, request: request["gateway"].pop("authentication")),
    "authentication without its config": lambda: with_rgw(lambda rgw, request: rgw["spec"].pop("config")),
    "authentication without its mounts": lambda: with_rgw(lambda rgw, request: rgw.pop("custom_configs")),
    "a CA mount without a trust bundle": lambda: with_rgw(lambda rgw, request: request["gateway"]["authentication"]["ldap"].pop("trustSecretRef")),
    "a trust bundle without its CA mount": lambda: with_rgw(lambda rgw, request: rgw["custom_configs"].pop()),
    "a bind file with content": lambda: with_rgw(lambda rgw, request: rgw["custom_configs"][0].update(content=PASSWORD)),
    "another ldap.conf": lambda: with_rgw(lambda rgw, request: rgw["custom_configs"][1].update(content="TLS_REQCERT never\n")),
    "another bind file path": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_ldap_secret="/etc/ceph/secret")),
    "LDAP sign-in off": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_s3_auth_use_ldap="false")),
    "a cleartext directory": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_ldap_uri="ldap://dc-01.example.test")),
    "a filter without the user token": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_ldap_searchfilter="(memberOf=x)")),
    "an option Bootwright does not derive": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_s3_auth_order="external")),
    "a bound bind password in the config": lambda: with_rgw(lambda rgw, request: rgw["spec"]["config"].update(rgw_ldap_binddn="")),
    "an extra authentication member": lambda: with_rgw(lambda rgw, request: request["gateway"]["authentication"]["ldap"].update(extra="x")),
    "custom configs on the ingress": lambda: with_rgw(lambda rgw, request: request["gateway"]["documents"][1].update(custom_configs=mounts()),
                                                      ldap_request(ingress=True)),
    "a plaintext endpoint": lambda: with_rgw(lambda rgw, request: (rgw["spec"].pop("ssl"), request["gateway"].update(bindings=[]))),
}


@pytest.mark.parametrize("case", sorted(INVALID))
def test_ldap_request_validation(case):
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_native.validate(INVALID[case]())


@pytest.mark.parametrize("trusted", [True, False])
@pytest.mark.parametrize("ingress", [False, True])
def test_ldap_request_admits_the_frozen_shapes(trusted, ingress):
    request = ldap_request(trusted, ingress)
    assert ceph_native.validate(request) is request
    unfiltered = ldap_request(trusted, ingress)
    unfiltered["gateway"]["documents"][0]["spec"]["config"] = options(searched=False)
    assert ceph_native.validate(unfiltered) is unfiltered


def test_ldap_conf_verifies_the_directory_against_its_bundle_or_the_system_store():
    assert ceph_native_object.ldap_conf(True) == "TLS_CACERT /etc/bootwright/rgw-ldap-ca.pem\nTLS_REQCERT demand\nNETWORK_TIMEOUT 3\nTIMEOUT 3\n"
    assert ceph_native_object.ldap_conf(False).startswith("TLS_CACERT /etc/pki/tls/certs/ca-bundle.crt\n")


def test_ldap_material_is_admitted_only_to_the_service_apply(monkeypatch):
    reached = []
    for phase in ceph_native_object.APPLY_PHASES:
        monkeypatch.setattr(ceph_native_object.ObjectWorker, phase, lambda self, phase=phase: reached.append((phase, self.tls)) or {"changed": False})
    request = ldap_request()

    def dispatch(phase, private):
        return ceph_native.dispatch({"request": request, "host": "node-1", "phase": phase, "private": private})

    applied = held(request, "gateway_service")
    dispatch("gateway_service", applied)
    assert reached == [("gateway_service", applied)]
    for phase, private in (("gateway_service", held(request, "observe")), ("observe", applied), ("gateway_zone", {"rgwLDAPBindPassword": PASSWORD}),
                           ("gateway_service", dict(applied, rgwLDAPBindPassword=" " + PASSWORD)),
                           ("gateway_service", dict(applied, rgwLDAPBindPassword=PASSWORD + "\n")),
                           ("gateway_service", dict(applied, rgwLDAPBindPassword="a" * 1024)),
                           ("gateway_service", dict(applied, rgwLDAPBindPassword="")),
                           ("gateway_service", dict(applied, rgwLDAPCABundle="not a certificate")),
                           ("gateway_service", {key: value for key, value in applied.items() if key != "rgwLDAPCABundle"})):
        with pytest.raises(ceph_native.Refusal, match="^material$"):
            dispatch(phase, private)


@pytest.mark.parametrize("ingress", [False, True])
def test_ldap_leaves_reach_only_the_apply_stream(tmp_path, monkeypatch, ingress):
    contained = []
    monkeypatch.setattr(gateway.ceph_host_runtime.HostRuntime, "contained",
                        lambda self, image, user, script, arguments=(), **options: contained.append(arguments) or json.loads(arguments[0]))
    request = ldap_request(ingress=ingress)
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(LDAPMonitor(request))
    assert apply_ldap(monitor, markers)[-1] == {"changed": True}
    applies = [data for tool, command, data in monitor.log if command[:2] == ("orch", "apply")]
    assert len(applies) == 1 and leaked(applies[0]) == MARKS
    stream = [json.loads(part) for part in applies[0].split(b"---\n")]
    assert stream[0]["custom_configs"][0]["content"] == PASSWORD and stream[0]["custom_configs"][2]["content"] == CA
    assert stream[0]["custom_configs"][1] == mounts()[1] and stream[0]["spec"]["config"] == options()
    assert not any(leaked(" ".join(command)) for tool, command, unused in monitor.log)
    assert contained and not any(leaked(" ".join(arguments)) for arguments in contained)
    for root, unused, files in os.walk(str(tmp_path)):
        for name in files:
            with open(os.path.join(root, name), "rb") as handle:
                assert not leaked(handle.read()), name
    for name in ("service-rgw.objects-attempt.json", "service-rgw.objects-complete.json"):
        configs = markers.read(name)["service"]["custom_configs"]
        assert [entry.get("content") for entry in configs] == ["bootwright-bound:/custom_configs/0/content", ceph_native_object.ldap_conf(True),
                                                               "bootwright-bound:/custom_configs/2/content"]


def test_ldap_replay_compares_the_stored_leaves_and_applies_nothing(tmp_path):
    request = ldap_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(LDAPMonitor(request))
    apply_ldap(monitor, markers)
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    monitor.mutations.clear()
    assert ldap_worker(monitor, markers, "gateway_service").gateway_service() == {"changed": False}
    assert monitor.mutations == [] and markers.read("service-rgw.objects-complete.json") is not None


@pytest.mark.parametrize("leaf", ["bind password", "CA bundle"])
def test_a_stored_leaf_other_than_the_bound_one_refuses_redacted(tmp_path, served, leaf):
    request = ldap_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(LDAPMonitor(request))
    apply_ldap(monitor, markers)
    monitor.specs["rgw.objects"]["custom_configs"][0 if leaf == "bind password" else 2]["content"] = OTHER_PASSWORD
    os.unlink(os.path.join(markers.path, "service-rgw.objects-complete.json"))
    monitor.mutations.clear()
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-credential$") as caught:
        ldap_worker(monitor, markers, "gateway_service").gateway_service()
    assert monitor.mutations == [] and not leaked(json.dumps(caught.value.native_error)) and OTHER_PASSWORD not in json.dumps(caught.value.native_error)
    assert caught.value.native_error["cause"].startswith("rgw.objects /custom_configs/")


def test_observation_holds_no_ldap_leaf_and_compares_presence_only(tmp_path, served):
    markers, monitor = ready_ldap(tmp_path)
    monitor.specs["rgw.objects"]["custom_configs"][0]["content"] = OTHER_PASSWORD
    ready, blocking, unused = observe_ldap(monitor, markers)
    assert ready is True and blocking == []
    del monitor.specs["rgw.objects"]["custom_configs"][0]["content"]
    with pytest.raises(ceph_native.Refusal, match="^foreign-service$"):
        observe_ldap(monitor, markers)


def test_a_foreign_option_before_the_first_attempt_refuses_without_a_record(tmp_path):
    request = ldap_request()
    markers = gateway.markers_for(tmp_path, request)
    monitor = gateway.images_held(LDAPMonitor(request))
    for phase in ceph_native_object.APPLY_PHASES[:2]:
        getattr(ldap_worker(monitor, markers, phase), phase)()
    monitor.mutations.clear()
    monitor.config.append(gateway.settings.entry("rgw_ldap_uri", "ldaps://elsewhere.example.test", section=SECTION + ".node-1.abcdef"))
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$") as caught:
        ldap_worker(monitor, markers, "gateway_service").gateway_service()
    assert caught.value.native_error["cause"] == "rgw_ldap_uri at client.rgw.objects.node-1.abcdef"
    assert markers.read("service-rgw.objects-attempt.json") is None and monitor.mutations == []
    monitor.config.pop()
    monitor.config.append(gateway.settings.entry("rgw_ldap_uri", "ldaps://elsewhere.example.test", section="client.rgw.other"))
    assert ldap_worker(monitor, markers, "gateway_service").gateway_service() == {"changed": True}


def test_readiness_reads_every_option_back_from_each_running_daemon(tmp_path, served):
    markers, monitor = ready_ldap(tmp_path)
    ready, blocking, evidence = observe_ldap(monitor, markers)
    assert ready is True and blocking == []
    shown = {who for who, unused in monitor.shows}
    assert shown == {"client.rgw.objects.node-1.abcdef", "client.rgw.objects.node-2.abcdef"}
    assert {option for unused, option in monitor.shows} == set(options()) | {"rgw_s3_auth_order", "rgw_s3_auth_use_rados"}
    assert not leaked(json.dumps(evidence))
    del monitor.reported["client.rgw.objects.node-2.abcdef"]["rgw_ldap_uri"]
    ready, blocking, unused = observe_ldap(monitor, markers)
    assert ready is False and blocking == ["ldap@rgw.objects"]


@pytest.mark.parametrize("order, rados", [("sts, external", None), ("local", "false")])
def test_a_daemon_whose_sign_in_order_lost_local_users_refuses(tmp_path, served, order, rados):
    markers, monitor = ready_ldap(tmp_path)
    reported = monitor.reported["client.rgw.objects.node-1.abcdef"]
    reported["rgw_s3_auth_order"] = order
    if rados is not None:
        reported["rgw_s3_auth_use_rados"] = rados
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$"):
        observe_ldap(monitor, markers)
    reported.update(rgw_s3_auth_order="sts, external, local", rgw_s3_auth_use_rados="true")
    assert observe_ldap(monitor, markers)[0] is True


@pytest.mark.parametrize("change", ["another value", "a daemon section", "a mask"])
def test_a_stored_option_other_than_the_frozen_one_refuses(tmp_path, served, change):
    markers, monitor = ready_ldap(tmp_path)
    entry = next(entry for entry in monitor.config if entry["name"] == "rgw_ldap_searchdn")
    {"another value": lambda: entry.update(value="DC=elsewhere"), "a daemon section": lambda: entry.update(section=SECTION + ".node-1.abcdef"),
     "a mask": lambda: entry.update(mask="host:node-1")}[change]()
    with pytest.raises(ceph_native.NativeFailure, match="^object-auth-config$"):
        observe_ldap(monitor, markers)


def test_an_option_cephadm_has_not_set_yet_is_pending(tmp_path, served):
    markers, monitor = ready_ldap(tmp_path)
    monitor.config = [entry for entry in monitor.config if entry["name"] != "rgw_s3_auth_use_ldap"]
    ready, blocking, unused = observe_ldap(monitor, markers)
    assert ready is False and blocking == ["ldap@rgw.objects"]
    assert ceph_core.blocking_entry("ldap@rgw.objects") and ceph_core.blocking_entry("mount@rgw.objects")


DAEMON = "objects.node-1.abcdef"


def host_files(root, trusted=True, started="2026-11-25T12:00:00.5Z"):
    """The daemon's keyring and the files cephadm writes for its custom configs, and its container as podman inspects it."""
    base = os.path.join(root, FSID)
    contents = {"rgw-ldap-bind": PASSWORD, "ldap.conf": ceph_native_object.ldap_conf(trusted)}
    if trusted:
        contents["rgw-ldap-ca.pem"] = CA
    paths = {}
    for directory, name, content in [("rgw." + DAEMON, "keyring", "[client.rgw]\n"), ("rgw." + DAEMON, "config", "[global]\n")] + [
            ("custom_config_files/rgw." + DAEMON, name, content) for name, content in contents.items()]:
        path = os.path.join(base, directory, name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(content)
        os.chmod(path, 0o600)
        os.utime(path, ns=(1764000000 * 10 ** 9, 1764000000 * 10 ** 9))
        paths[name] = path
    destinations = {"rgw-ldap-bind": ceph_native_object.LDAP_BIND, "ldap.conf": ceph_native_object.LDAP_CONF, "rgw-ldap-ca.pem": ceph_native_object.LDAP_CA}
    mounted = [{"Source": paths["config"], "Destination": "/etc/ceph/ceph.conf"}]
    mounted += [{"Source": paths[name], "Destination": destinations[name]} for name in contents]
    return paths, {"Args": ["radosgw"], "Config": {"Env": [], "User": "ceph"}, "State": {"StartedAt": started}, "Mounts": mounted}


class HostMonitor(LDAPMonitor):
    def __init__(self, request, containers):
        super().__init__(request)
        self.containers = containers

    def run(self, argv, data=b"", timeout=60):
        if argv[0] == ceph_native_units.PODMAN:
            if argv[1] == "ps":
                assert argv[2:] == ["--all", "--format", "json", "--filter", "name=^ceph-" + FSID + "-rgw[.-]"]
                return 0, json.dumps([{"Id": "%064x" % index, "Names": ["ceph-" + FSID + "-rgw-" + str(index)], "State": state}
                                      for index, (state, unused) in enumerate(self.containers)]).encode()
            assert argv[1:4] == ["inspect", "--type", "container"]
            return 0, json.dumps([self.containers[int(argv[4], 16)][1]]).encode()
        raise AssertionError(argv)


@pytest.fixture
def host_root(tmp_path, monkeypatch):
    value = str(tmp_path / "ceph")
    monkeypatch.setattr(ceph_native_observability, "DATA_ROOT", value)
    monkeypatch.setattr(ceph_native_object, "CEPH_UID", os.getuid())
    return value


def mount_round(request, containers):
    native = ceph_native_object.ObjectWorker(request, "node-1", run=HostMonitor(request, containers).run)
    return native.gateway_auth_mounts()


def test_the_mount_proof_counts_each_container_that_mounts_every_file(host_root):
    request = ldap_request()
    unused, inspected = host_files(host_root)
    other = copy.deepcopy(inspected)
    other["Mounts"][0]["Source"] = other["Mounts"][0]["Source"].replace("rgw." + DAEMON, "rgw.more.node-1.abcdef")
    result = mount_round(request, [("running", inspected), ("exited", inspected), ("running", other)])
    assert result == {"changed": False, "daemons": 1, "proved": 1}
    assert not leaked(json.dumps(result)) and host_root not in json.dumps(result)


def files_changed(root, change):
    paths, inspected = host_files(root)
    change(paths, inspected)
    return inspected


UNPROVED = {
    "a missing CA mount": lambda paths, inspected: inspected["Mounts"].pop(),
    "a file mounted from elsewhere": lambda paths, inspected: inspected["Mounts"][1].update(Source=paths["keyring"]),
    "a group-readable file": lambda paths, inspected: os.chmod(paths["rgw-ldap-bind"], 0o640),
    "an empty bind file": lambda paths, inspected: open(paths["rgw-ldap-bind"], "w", encoding="utf-8").close(),
    "a file linked twice": lambda paths, inspected: os.link(paths["ldap.conf"], paths["ldap.conf"] + ".second"),
    "another ldap.conf": lambda paths, inspected: open(paths["ldap.conf"], "a", encoding="utf-8").write("TLS_REQCERT never\n"),
    "a container started before its files": lambda paths, inspected: inspected["State"].update(StartedAt="2025-11-24T15:59:59Z"),
    "a symbolic link": lambda paths, inspected: (os.unlink(paths["rgw-ldap-ca.pem"]), os.symlink(paths["keyring"], paths["rgw-ldap-ca.pem"])),
    "no keyring": lambda paths, inspected: os.unlink(paths["keyring"]),
}


@pytest.mark.parametrize("case", sorted(UNPROVED))
def test_a_container_without_every_proved_mount_is_unproved(host_root, case):
    inspected = files_changed(host_root, UNPROVED[case])
    assert mount_round(ldap_request(), [("running", inspected)]) == {"changed": False, "daemons": 1, "proved": 0}


def test_a_file_labelled_unlike_the_keyring_is_unproved(host_root, monkeypatch):
    paths, inspected = host_files(host_root)
    assert mount_round(ldap_request(), [("running", inspected)])["proved"] == 1
    real = ceph_native_object.selinux_label
    monkeypatch.setattr(ceph_native_object, "selinux_label",
                        lambda descriptor: b"other" if os.readlink("/proc/self/fd/%d" % descriptor).endswith("rgw-ldap-bind") else real(descriptor))
    assert mount_round(ldap_request(), [("running", inspected)])["proved"] == 0


def test_an_owner_other_than_the_ceph_user_is_unproved(host_root, monkeypatch):
    unused, inspected = host_files(host_root)
    monkeypatch.setattr(ceph_native_object, "CEPH_UID", os.getuid() + 1)
    assert mount_round(ldap_request(), [("running", inspected)])["proved"] == 0


class RoundTransport(gateway.Transport):
    def __init__(self, request, rounds):
        super().__init__(request, ready_after=1, observed={
            "endpoint": {"certificateSHA256": gateway.LEAF_SHA256, "reachable": True}, "resource": {"id": str(uuid.UUID(int=7)), "path": gateway.ENDPOINT}})
        self.rounds, self.private, self.lock = rounds, [], threading.Lock()

    def call(self, host, phase, private=None, *, timeout=None):
        if phase == "gateway_auth_mounts":
            assert private is None and 0 < timeout <= ceph_core.MOUNT_CALL
            with self.lock:
                self.calls.append((host["name"], phase))
                asked = sum(call[1] == phase for call in self.calls) - 1
            return self.rounds[min(asked // len(gateway.HOSTS), len(self.rounds) - 1)][host["name"]]
        if phase == "gateway_service":
            self.private.append(private)
        return super().call(host, phase, None, timeout=timeout)

    def gateway_material(self, key):
        return {"gatewayCertificate": gateway.CERT, "gatewayKey": gateway.KEY} if key else {"gatewayCertificate": gateway.CERT}

    def gateway_auth_material(self):
        return {"rgwLDAPBindPassword": PASSWORD, "rgwLDAPCABundle": CA}


def mounted(*proved):
    return {host: {"changed": False, "daemons": int(host in ("node-1", "node-2")), "proved": int(host in proved)} for host in gateway.HOSTS}


def test_apply_proves_the_mounts_on_every_rgw_host_inside_its_readiness_wait():
    request = ldap_request()
    transport = RoundTransport(request, [mounted("node-1"), mounted("node-1", "node-2")])
    result = ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: 0, pause=lambda seconds: None)
    assert result["evidence"]["ready"] is True
    assert sorted(call for call in transport.calls if call[1] == "gateway_auth_mounts") == sorted([(host, "gateway_auth_mounts") for host in gateway.HOSTS] * 2)
    assert transport.private == [dict(held(request, "gateway_service"))]
    clock = iter(range(0, 100000, 300))
    transport = RoundTransport(request, [mounted("node-1")])
    with pytest.raises(ceph_core.ReadinessRefusal) as caught:
        ceph_core.execute(request, "apply", "d" * 64, transport, now=lambda: next(clock), pause=lambda seconds: None)
    assert ceph_core.refusal(caught.value, ceph_core.CLUSTER_REASONS) == ("not-ready", "not-ready:mount@rgw.objects")
