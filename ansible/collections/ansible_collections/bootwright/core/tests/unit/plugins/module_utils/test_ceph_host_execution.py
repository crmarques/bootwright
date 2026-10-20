"""The host block runs packages, then the one long image call, then route and identities.

Transports here are protocol fixtures; nothing reaches a guest or a registry.
"""

from __future__ import annotations

import copy
import inspect
import json
import subprocess
import sys

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import (
    ceph_core, ceph_host_runtime, ceph_host_worker, ceph_native, ceph_native_worker, ceph_package_worker, ceph_packages, native_diagnostics,
)
from ansible_collections.bootwright.core.plugins.modules import ceph_host
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_host_runtime as host_fixtures

DIGEST = "d" * 64
PASSWORD = "REGISTRY-PASSWORD-SENTINEL"

# The frozen replicated rbd pool the Go side sends with every optional member unset.
POOL = {"application": "rbd", "autoscale": {"bulk": False, "maxPGs": 0, "minPGs": 0, "mode": "on", "targetBytes": 0, "targetRatio": ""},
        "compression": {"algorithm": "", "maxBlob": 0, "minBlob": 0, "mode": "", "requiredRatio": ""}, "crushRule": "replicated_rule", "minSize": 2,
        "name": "volumes", "overwrites": False, "pgNum": 32, "quota": {"maxBytes": 0, "maxObjects": 0}, "role": "rbd", "size": 2, "type": "replicated"}


@pytest.fixture
def host_request(request_value):
    cluster = copy.deepcopy(request_value)
    del cluster["action"]
    cluster["packages"] = {"version": "ceph-packages-v2", "licenseAccepted": True, "sources": [
        {"host": host["name"], "profile": "rhel", "media": "rhel-dvd.iso", "mediaSHA256": "e" * 64, "mediaSize": 1024,
         "publicationPath": "/var/lib/bootwright-services/example/public/os/rhel/tree", "url": "https://192.0.2.10:8443/os/rhel/tree",
         "certificateRef": "serving-tls"}
        for host in cluster["hosts"]]}
    return {"version": "ceph-host-v4", "cluster": cluster, "host": "node-2"}


request_value = host_fixtures.request_value


def prepared_evidence(request, **changes):
    cluster = request["cluster"]
    value = {"version": ceph_host_runtime.EVIDENCE_VERSION, "host": request["host"], "owner": cluster["owner"], "fsid": cluster["fsid"],
             "image": cluster["image"], "ready": True, "removed": False, "imageID": "c" * 64, "uid": 167, "gid": 167,
             "label": ceph_host_runtime.SHARED_LABEL, "policySHA256": "f" * 64, "cephadmSHA256": "e" * 64,
             "planDigest": "a" * 64, "route": True,
             "devices": [{"path": "/dev/sda", "serial": "drive-scsi2", "deviceID": "QEMU_QEMU_HARDDISK_drive-scsi2"}]}
    value.update(changes)
    return value


class Transport:
    def __init__(self, request, observed=None):
        self.request = request
        self.observed = observed or prepared_evidence(request)
        self.calls = []

    def packages(self, host):
        self.calls.append((host["name"], "packages"))
        return {"changed": True, "ready": True, "planDigest": "a" * 64}

    def host_profile(self, host):
        self.calls.append((host["name"], "profile"))
        return {"changed": True}

    def call(self, host, phase, private=None, *, timeout=660):
        assert private is None and timeout == 660
        self.calls.append((host["name"], phase))
        if phase == "host_observe":
            return copy.deepcopy(self.observed)
        return {"changed": False}


def test_apply_runs_packages_then_the_long_call_then_route_and_identities(host_request):
    transport = Transport(host_request)
    result = ceph_core.execute_host(host_request, "apply", DIGEST, transport)
    assert transport.calls == [("node-2", "packages"), ("node-2", "profile"), ("node-2", "route"), ("node-2", "device_identity"),
                               ("node-2", "host_observe")]
    assert result["changed"] is True and result["evidence"]["ready"] is True and result["evidence"]["request"] == DIGEST


def test_observation_makes_only_the_read_only_call(host_request):
    transport = Transport(host_request, prepared_evidence(host_request, ready=False, imageID="", uid=0, gid=0, label="", policySHA256="",
                                                          cephadmSHA256="", route=False, devices=[]))
    result = ceph_core.execute_host(host_request, "observe", DIGEST, transport)
    assert transport.calls == [("node-2", "host_observe")] and result["evidence"]["ready"] is False and result["changed"] is False


@pytest.mark.parametrize("operation", ["observe", "destroy"])
def test_reads_and_release_need_no_bound_package_media(host_request, operation):
    for source in host_request["cluster"]["packages"]["sources"]:
        del source["mediaSHA256"], source["mediaSize"]
    transport = Transport(host_request)
    assert ceph_core.execute_host(host_request, operation, DIGEST, transport)["changed"] is False
    assert transport.calls == ([("node-2", "host_observe")] if operation == "observe" else [])


def test_release_makes_no_effect_and_contacts_no_host(host_request):
    transport = Transport(host_request)
    result = ceph_core.execute_host(host_request, "destroy", DIGEST, transport)
    assert transport.calls == [] and result["changed"] is False
    assert result["evidence"]["removed"] is True and result["evidence"]["ready"] is False and result["evidence"]["devices"] == []


@pytest.mark.parametrize("change", [{"ready": False}, {"planDigest": "b" * 64}])
def test_apply_refuses_an_unproved_or_other_package_plan(host_request, change):
    transport = Transport(host_request, prepared_evidence(host_request, **change))
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        ceph_core.execute_host(host_request, "apply", DIGEST, transport)


@pytest.mark.parametrize("change", [{"owner": "apply-other"}, {"image": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:" + "b" * 64}, {"host": "node-3"},
                                    {"password": PASSWORD}, {"label": "unconfined_u:object_r:user_tmp_t:s0"}, {"uid": True}, {"ready": True, "removed": True},
                                    {"devices": [{"path": "/dev/sda", "serial": "x", "deviceID": "has spaces"}]}])
def test_host_evidence_is_closed_and_bound_to_the_frozen_request(host_request, change):
    value = dict(prepared_evidence(host_request, **change), request=DIGEST)
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.host_evidence(value, DIGEST, host_request)


def test_the_shared_protocol_publishes_host_evidence(host_request):
    value = dict(prepared_evidence(host_request), request=DIGEST)
    assert ceph_core.evidence(value, DIGEST) == value


@pytest.mark.parametrize("change", [lambda r: r.update(host="node-9"), lambda r: r.update(version="ceph-host-v0"), lambda r: r["cluster"].pop("packages"),
                                    lambda r: r["cluster"].update(action="pool"), lambda r: r.update(extra=True)])
def test_a_host_request_outside_its_cluster_refuses(host_request, change):
    change(host_request)
    with pytest.raises(ceph_native.Refusal):
        ceph_core.host_request(host_request)


def test_the_long_call_carries_the_credential_only_on_protected_stdin(host_request, tmp_path):
    key = tmp_path / "key"
    known = tmp_path / "known"
    registry = tmp_path / "registry"
    key.write_text("PRIVATE-KEY-SENTINEL")
    known.write_text("node-2 ssh-ed25519 PUBLIC")
    registry.write_text(json.dumps({"url": "cp.icr.io", "username": "registry-user", "password": PASSWORD}))
    for path in (key, known, registry):
        path.chmod(0o600)
    calls = []

    def run(argv, data, timeout):
        calls.append((argv, data, timeout))
        return 0, b'{"ok":true,"result":{"changed":true}}'

    material = {"sshKey": str(key), "knownHosts": str(known), "registryJSON": str(registry)}
    transport = ceph_core.Transport(host_request["cluster"], material, run=run, scope=True)
    host = host_request["cluster"]["hosts"][1]
    assert transport.host_profile(host) == {"changed": True}
    argv, data, timeout = calls[0]
    assert PASSWORD not in " ".join(argv) and "PRIVATE-KEY-SENTINEL" not in " ".join(argv)
    assert timeout == ceph_host_worker.REMOTE_LIMIT + 60 and argv[-1].endswith(" " + str(ceph_host_worker.REMOTE_LIMIT))
    envelope = json.loads(data)
    assert envelope["budget"] == ceph_host_worker.REMOTE_LIMIT and set(envelope["sources"]) == set(ceph_native_worker.MODULES)
    assert envelope["payload"]["phase"] == "profile" and set(envelope["payload"]["private"]) == {"registryJSON"}
    with pytest.raises(ceph_native.Refusal, match="^transport$"):
        transport.call(host, "profile")
    assert len(calls) == 1


def test_every_other_host_phase_keeps_the_ordinary_ceiling(host_request, tmp_path):
    key = tmp_path / "key"
    known = tmp_path / "known"
    key.write_text("PRIVATE-KEY-SENTINEL")
    known.write_text("node-2 ssh-ed25519 PUBLIC")
    key.chmod(0o600)
    known.chmod(0o600)
    calls = []

    def run(argv, data, timeout):
        calls.append((argv, data, timeout))
        return 0, b'{"ok":true,"result":{"changed":false}}'

    transport = ceph_core.Transport(host_request["cluster"], {"sshKey": str(key), "knownHosts": str(known)}, run=run, scope=True)
    for phase in ("route", "device_identity", "host_observe", "host_devices"):
        transport.call(host_request["cluster"]["hosts"][1], phase)
    assert all(timeout == 660 and json.loads(data)["budget"] <= 640 for unused, data, timeout in calls)


class Native:
    class Refusal(ValueError):
        pass

    def __init__(self):
        self.envelopes = []

    def dispatch(self, envelope):
        self.envelopes.append(envelope)
        return {"changed": True}


@pytest.mark.parametrize("phase", ["route", "device_identity", "host_observe", "bootstrap", "enroll", None])
def test_the_long_launcher_admits_only_the_profile_phase(phase):
    native = Native()
    restricted = ceph_host_worker.restricted(native)
    with pytest.raises(Native.Refusal):
        restricted.dispatch({"phase": phase})
    assert native.envelopes == []
    assert restricted.dispatch({"phase": "profile"}) == {"changed": True} and restricted.Refusal is Native.Refusal


def test_the_long_launcher_raises_only_its_own_ceiling():
    sources = {name: '"""Fixed fixture."""\n' for name in ceph_native_worker.MODULES}
    sources["native_diagnostics"] = inspect.getsource(native_diagnostics)
    sources["ceph_native"] = "class Refusal(ValueError): pass\ndef dispatch(value):\n    return {'phase': value['phase']}\n"
    source = ceph_host_worker.launcher(inspect.getsource(ceph_native_worker), inspect.getsource(ceph_host_worker))
    for phase, expected in (("profile", {"ok": True, "result": {"phase": "profile"}}), ("route", {"ok": False, "reason": "native-refusal"})):
        payload = {"sources": sources, "payload": {"phase": phase}, "budget": 3}
        result = subprocess.run([sys.executable, "-I", "-c", source, "3"], input=json.dumps(payload).encode(), capture_output=True, timeout=10, check=False)
        assert result.returncode == 0 and result.stderr == b""
        value = json.loads(result.stdout)
        value.pop("cause", None)
        assert value == expected
    payload = {"sources": sources, "payload": {"phase": "profile"}, "budget": 4286}
    result = subprocess.run([sys.executable, "-I", "-c", source, "4286"], input=json.dumps(payload).encode(), capture_output=True, timeout=10, check=False)
    assert json.loads(result.stdout) == {"ok": False, "reason": "native-unknown"}


def package_transport(host_request, tmp_path, output):
    material = {}
    for name, value in (("sshKey", "PRIVATE-KEY-SENTINEL"), ("knownHosts", "node-2 ssh-ed25519 PUBLIC"), ("packageCA1", "PUBLIC-CERTIFICATE")):
        path = tmp_path / name
        path.write_text(value)
        path.chmod(0o600)
        material[name] = str(path)
    return ceph_core.Transport(host_request["cluster"], material, run=lambda argv, data, timeout: (0, output), scope=True)


def unaccepted_license():
    with ceph_packages.accepted_license(False):
        pytest.fail("installed under an unaccepted license")


def unsigned_payload():
    with ceph_packages.phase("package-signature"):
        raise ValueError("native approved publisher signature " + PASSWORD)


@pytest.mark.parametrize("token, refuse", [("package-license", unaccepted_license), ("package-signature", unsigned_payload)])
def test_a_package_refusal_reaches_the_block_by_its_token_and_site(host_request, tmp_path, token, refuse):
    with pytest.raises(ceph_native.Refusal) as raised:
        refuse()
    failure = ceph_package_worker.failure(raised.value)
    assert failure["reason"] == token and failure["cause"].startswith(token + "@ceph_packages.")
    transport = package_transport(host_request, tmp_path, json.dumps(failure).encode())
    with pytest.raises(ceph_native.RemoteRefusal) as remote:
        transport.packages(host_request["cluster"]["hosts"][1])
    assert remote.value.detail == token + ":" + failure["cause"] and PASSWORD not in remote.value.detail
    assert ceph_core.refusal(remote.value, ceph_core.HOST_REASONS) == (token, remote.value.detail)


@pytest.mark.parametrize("failure", [{"ok": False, "reason": "package-" + PASSWORD.lower()}, {"ok": False, "reason": "native-refusal"},
                                     {"ok": False, "reason": "package-license", "cause": "package-license@ceph_packages.unshipped:1"}])
def test_a_package_failure_outside_the_workers_closed_set_is_transport(host_request, tmp_path, failure):
    transport = package_transport(host_request, tmp_path, json.dumps(failure).encode())
    with pytest.raises(ceph_native.Refusal, match="^transport$"):
        transport.packages(host_request["cluster"]["hosts"][1])


def test_the_package_worker_names_only_its_closed_tokens_and_their_site():
    bundle = {name: '"""Fixed test helper."""\n' for name in ceph_package_worker.MODULES}
    bundle["native_diagnostics"] = inspect.getsource(native_diagnostics)
    bundle["ceph_native_worker"] = inspect.getsource(ceph_native_worker)
    bundle["ceph_packages"] += "class Refusal(ValueError):\n pass\n\ndef entrypoint(payload):\n raise Refusal(payload)\n"
    source = inspect.getsource(ceph_package_worker)
    for payload, reason, cause in (("package-license", "package-license", "package-license@ceph_packages.entrypoint:"),
                                   (PASSWORD, "packages", "Refusal@ceph_packages.entrypoint:")):
        envelope = {"sources": bundle, "payload": payload}
        result = subprocess.run([sys.executable, "-I", "-c", source], input=json.dumps(envelope).encode(), capture_output=True, check=False, timeout=10)
        value = json.loads(result.stdout)
        assert result.stderr == b"" and value["reason"] == reason and value.pop("cause").startswith(cause) and value == {"ok": False, "reason": reason}
        assert PASSWORD.encode() not in result.stdout


@pytest.mark.parametrize("change", [
    {"action": "cluster"}, {"pool": copy.deepcopy(POOL)},
    {"client": {"name": "consumer", "pool": "volumes", "access": "read-only"}},
])
def test_host_request_admits_only_the_cluster_scope(host_request, change):
    assert ceph_core.host_request(copy.deepcopy(host_request)) == host_request
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.host_request(dict(host_request, cluster=dict(host_request["cluster"], **change)))
    with pytest.raises(ceph_native.Refusal, match="^request$"):
        ceph_core.host_request(dict(host_request, version="ceph-host-v3"))


@pytest.mark.parametrize("operation", ["apply", "observe", "destroy"])
def test_the_host_module_binds_its_transport_to_the_frozen_scope(host_request, monkeypatch, operation):
    reached = {}

    class Module:
        def __init__(self, **options):
            self.params = {"request": copy.deepcopy(host_request), "operation": operation, "digest": DIGEST, "material": {}}

        def fail_json(self, **failure):
            raise AssertionError(failure)

        def exit_json(self, **result):
            reached["result"] = result

    def execute_host(request, operation_name, digest, transport):
        reached["call"] = (request, operation_name, digest, transport.request)
        return {"changed": False}

    monkeypatch.setattr(ceph_host, "AnsibleModule", Module)
    monkeypatch.setattr(ceph_core, "execute_host", execute_host)
    ceph_host.main()
    assert reached == {"result": {"changed": False}, "call": (host_request, operation, DIGEST, host_request["cluster"])}
