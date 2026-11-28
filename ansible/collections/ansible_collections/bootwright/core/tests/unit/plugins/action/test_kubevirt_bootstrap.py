"""A clone seed references private HTTPS delivery and proves the guest over SSH."""

from __future__ import annotations

import base64
import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import substrate_kubevirt_api as action
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_bootstrap as bootstrap
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_machine as arm
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_seed as seed
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh as guest
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Refused, Unanswered
from ansible_collections.bootwright.core.tests.unit.plugins.action.test_kubevirt_template import clone_inputs
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils.test_kubevirt_machine import VM_PATH, client, host

INTENT = {"address": "192.0.2.10/24", "gateway": "192.0.2.1", "growRootFilesystem": True, "hostname": "guest.example.test",
          "mac": "52:54:00:25:10:e0", "marker": "a" * 64, "nameservers": ["192.0.2.53"], "release": "9.8"}
ED25519 = b"\x00\x00\x00\x0bssh-ed25519\x00\x00\x00\x20"


def key(value):
    return "ssh-ed25519 " + base64.b64encode(ED25519 + bytes([value]) * 32).decode("ascii")


CERTIFICATE = "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----\n"
URL = "https://artifacts.example.test/private/bootstrap/" + "a" * 64 + "/identity"


def payload():
    return bootstrap.seed(INTENT, key(1), key(2), CERTIFICATE, URL)


def test_seed_contains_a_verified_fetch_and_binds_the_declared_network():
    data = payload()
    header, body = base64.b64decode(data["userdata"]).split(b"\n", 1)
    assert header == b"#cloud-config"
    config = json.loads(body)
    assert config["users"][0]["ssh_authorized_keys"] == [key(1)]
    assert config["ssh_pwauth"] is False and config["ssh_genkeytypes"] == []
    assert config["growpart"]["mode"] == "auto" and config["resize_rootfs"] is True
    files = {entry["path"].split("/")[-1]: entry for entry in config["write_files"]}
    assert files["fetch.conf"]["permissions"] == "0600" and URL in files["fetch.conf"]["content"]
    assert '\nproto = "=https"\n' in files["fetch.conf"]["content"] and "cacert" in files["fetch.conf"]["content"]
    assert URL not in repr(config["runcmd"]) and "ssh_keys" not in config
    network = json.loads(base64.b64decode(data["networkdata"]))
    assert network["ethernets"]["eth0"]["match"] == {"macaddress": INTENT["mac"]}
    assert network["ethernets"]["eth0"]["addresses"] == [INTENT["address"]]


@pytest.mark.parametrize("url", ["http://artifacts.example.test/token/identity", URL + "?secret=data",
                                 "https://user:secret@artifacts.example.test/identity", URL + '\noutput="somewhere"',
                                 URL.replace("/bootstrap/", "/../")])
def test_seed_refuses_an_unqualified_fetch(url):
    with pytest.raises(Refused, match="private HTTPS"):
        bootstrap.seed(INTENT, key(1), key(2), CERTIFICATE, url)


def answer(marker=INTENT["marker"], release=INTENT["release"], address=INTENT["address"], machine_id="a" * 32):
    lines = (marker, "x86_64", "rhel", release, INTENT["hostname"], machine_id, address)
    return guest.Outcome(0, ("\n".join(lines) + "\n").encode("ascii"), False, False)


def test_first_boot_requires_the_pinned_key_marker_release_and_address(tmp_path):
    identity = tmp_path / "fleet"
    identity.write_text("fixture")
    identity.chmod(0o600)
    checked = []

    def ssh(argv, data, timeout):
        assert data is None and timeout == 45
        assert "StrictHostKeyChecking=yes" in argv and "HostKeyAlgorithms=ssh-ed25519" in argv
        assert "bootwright@192.0.2.10" in argv
        pin = next(value for value in argv if value.startswith("UserKnownHostsFile="))
        assert Path(pin.split("=", 1)[1]).read_text().strip() == "192.0.2.10 " + key(2)
        checked.append(True)
        return answer()

    result = bootstrap.verify_guest(INTENT, str(tmp_path), str(identity), key(2),
                                    policy=lambda: {"hostKeyAlgorithms": "ssh-ed25519", "configuration": "Host *\n"}, run=ssh)
    assert result["postcondition"] and checked == [True]
    assert list(tmp_path.iterdir()) == [identity]


@pytest.mark.parametrize("failure", [
    guest.Outcome(255, b"", True, False), guest.Outcome(None, b"", False, True),
    answer(marker="b" * 64), answer(release="9.7"), answer(address="192.0.2.11/24"),
])
def test_first_boot_refuses_wrong_identity_or_unproved_guest(failure):
    with pytest.raises(Refused):
        bootstrap.verify_answer(failure, INTENT)


OPERATION = "op-" + "d" * 32
BLOCK = "bootstrap-master-0"


def realized_clone():
    api, frozen, proof = clone_inputs()
    ignored, evidence = arm.apply(host(api), frozen, proof)
    assert evidence["postcondition"]
    data_volume = arm.receipt_for(evidence["receipts"], "DataVolume", frozen["rootVolume"])["uid"]
    claim = arm.receipt_for(evidence["receipts"], "PersistentVolumeClaim", frozen["rootVolume"])["uid"]
    vm = api.objects[VM_PATH]
    vm["metadata"]["resourceVersion"] = "1"
    identity = {"cluster": proof["pinned"]["kubeSystemUID"], "vm": vm["metadata"]["uid"], "rootVolume": data_volume, "rootClaim": claim}
    target = host(api, pin=identity)
    api.requests.clear()
    return api, target, frozen["name"]


def test_seed_attach_recovers_its_secret_and_withdraws_only_its_mount():
    api, target, vm = realized_clone()
    api.lose_create_reply = True
    data = payload()
    assert seed.attach(target, vm, OPERATION, BLOCK, data, INTENT["mac"])["changed"]
    assert len([item for item in api.requests if item[0] == "POST"]) == 1
    mounted = api.objects[VM_PATH]["spec"]["template"]["spec"]["volumes"][-1]
    assert mounted == seed.volume(vm + "-seed") and URL not in repr(api.objects[VM_PATH])
    assert not seed.attach(target, vm, OPERATION, BLOCK, data, INTENT["mac"])["changed"]
    assert seed.withdraw(target, vm, OPERATION, BLOCK)["changed"]
    assert not seed.withdraw(target, vm, OPERATION, BLOCK)["changed"]
    assert all("/start" not in item[1] for item in api.requests)


def test_seed_refuses_live_vm_and_foreign_secret():
    api, target, vm = realized_clone()
    api.objects[target.vmi(vm)] = {"status": {"phase": "Running"}}
    with pytest.raises(Refused, match="not stopped"):
        seed.attach(target, vm, OPERATION, BLOCK, payload(), INTENT["mac"])
    assert all(item[0] == "GET" for item in api.requests)
    del api.objects[target.vmi(vm)]
    assert seed.attach(target, vm, OPERATION, BLOCK, payload(), INTENT["mac"])["changed"]
    location, meta = seed.identity(target, vm, api.objects[VM_PATH], OPERATION, BLOCK)
    api.objects[location]["metadata"]["annotations"] = {}
    api.requests.clear()
    with pytest.raises(Refused, match="foreign"):
        seed.withdraw(target, vm, OPERATION, BLOCK)
    assert api.objects[location]["metadata"]["name"] == meta["name"]
    assert all(item[0] == "GET" for item in api.requests)


def test_check_mode_refuses_seed_mutation_without_reading_material(monkeypatch):
    def never_connect(*ignored):
        raise Unanswered()

    monkeypatch.setattr(action, "connect", never_connect)
    instance = object.__new__(action.ActionModule)
    instance._task = SimpleNamespace(check_mode=True, args={"operation": "seed_attach"})
    assert "check mode" in instance.run()["msg"]


def test_first_boot_is_bound_to_the_same_running_vm_instance(monkeypatch):
    api, target, vm = realized_clone()
    mounted = seed.attach(target, vm, OPERATION, BLOCK, payload(), INTENT["mac"])
    uid = target.pin["vm"]
    running = {"metadata": {"uid": "instance-1", "ownerReferences": [{"controller": True, "uid": uid}]},
               "status": {"phase": "Running"}}
    api.objects[target.vmi(vm)] = running
    api.requests.clear()
    arguments = {
        "endpoint": "kubevirt:guests/" + vm, "pinned_uuid_base64": base64.b64encode(target.pin["cluster"].encode()).decode(),
        "pinned_serial_base64": base64.b64encode(uid.encode()).decode(), "bootstrap": INTENT,
        "root_volume_uid": target.pin["rootVolume"], "root_claim_uid": target.pin["rootClaim"],
        "scratch": "scratch", "bootstrap_fleet_key": "key", "host_public_key": key(2),
        "apply_operation": OPERATION, "block": BLOCK, "seed_uid": mounted["secretUID"],
    }
    monkeypatch.setattr(bootstrap, "verify_guest", lambda *unused, seeded: {"postcondition": True})
    assert action.bootstrap_verify(client(api), arguments)["instanceUID"] == "instance-1"

    def replaced(*unused, seeded):
        running["metadata"]["uid"] = "instance-2"
        return {"postcondition": True}

    monkeypatch.setattr(bootstrap, "verify_guest", replaced)
    with pytest.raises(Refused, match="changed during first-boot verification"):
        action.bootstrap_verify(client(api), arguments)
    assert all(entry[0] == "GET" for entry in api.requests)


def test_seed_start_checks_its_recorded_uid_before_power_on(monkeypatch):
    api, target, vm = realized_clone()
    mounted = seed.attach(target, vm, OPERATION, BLOCK, payload(), INTENT["mac"])
    api.requests.clear()

    def start(bound, requested, verb):
        assert bound is target and requested == vm and verb == "on"
        return {"power": "On"}

    monkeypatch.setattr(arm, "change_power", start)
    with pytest.raises(Refused, match="recorded seed"):
        seed.start(target, vm, OPERATION, BLOCK, "wrong-uid")
    assert all(entry[0] == "GET" for entry in api.requests)
    assert seed.start(target, vm, OPERATION, BLOCK, mounted["secretUID"]) == {"power": "On"}


def test_seed_stop_and_seedless_boot_require_the_proved_instance_and_withdrawn_secret(monkeypatch, tmp_path):
    api, target, vm = realized_clone()
    certificate, url = tmp_path / "certificate", tmp_path / "url"
    for path, value in ((certificate, CERTIFICATE), (url, URL)):
        path.write_text(value)
        path.chmod(0o600)
    args = {
        "endpoint": "kubevirt:guests/" + vm, "apply_operation": OPERATION, "block": BLOCK,
        "pinned_uuid_base64": base64.b64encode(target.pin["cluster"].encode()).decode(),
        "pinned_serial_base64": base64.b64encode(target.pin["vm"].encode()).decode(),
        "root_volume_uid": target.pin["rootVolume"], "root_claim_uid": target.pin["rootClaim"],
        "bootstrap": INTENT, "fleet_public_key": key(1), "host_public_key": key(2),
        "bootstrap_certificate": str(certificate), "bootstrap_url": str(url),
    }
    endpoint = client(api)
    with pytest.raises(Refused):
        action.bootstrap_seed("seed_attach", endpoint, dict(args, endpoint="https://vc.example.test:443"))
    assert not api.requests
    mounted = action.bootstrap_seed("seed_attach", endpoint, args)
    args.update(seed_uid=mounted["secretUID"], first_instance_uid="seeded-1")
    assert URL not in repr(api.objects[VM_PATH])
    monkeypatch.setattr(arm, "change_power", lambda *unused: {"power": "On"})
    assert action.bootstrap_seed("seed_start", endpoint, args) == {"power": "On"}
    api.objects[target.vmi(vm)] = {
        "metadata": {"uid": "seeded-1", "ownerReferences": [{"controller": True, "uid": target.pin["vm"]}]},
        "status": {"phase": "Running"},
    }
    api.requests.clear()
    with pytest.raises(Refused, match="does not match the first-boot proof"):
        action.bootstrap_seed("seed_stop", endpoint, dict(args, first_instance_uid="another-instance"))
    with pytest.raises(Refused, match="still has seed material"):
        action.bootstrap_seed("seedless_start", endpoint, args)
    assert all(entry[0] == "GET" for entry in api.requests)
    assert action.bootstrap_seed("seed_stop", endpoint, args)["changed"]
    assert not action.bootstrap_seed("seed_stop", endpoint, args)["changed"]
    assert action.bootstrap_seed("seed_withdraw", endpoint, args)["postcondition"]
    assert action.bootstrap_seed("seedless_start", endpoint, args) == {"power": "On"}


def test_seedless_vm_must_keep_the_first_boot_guest_identity(monkeypatch):
    api, target, vm = realized_clone()
    api.objects[target.vmi(vm)] = {
        "metadata": {"uid": "new-instance", "ownerReferences": [{"controller": True, "uid": target.pin["vm"]}]},
        "status": {"phase": "Running"},
    }
    first = bootstrap.verify_answer(answer(), INTENT)
    arguments = {
        "endpoint": "kubevirt:guests/" + vm, "pinned_uuid_base64": base64.b64encode(target.pin["cluster"].encode()).decode(),
        "pinned_serial_base64": base64.b64encode(target.pin["vm"].encode()).decode(),
        "root_volume_uid": target.pin["rootVolume"], "root_claim_uid": target.pin["rootClaim"],
        "bootstrap": INTENT, "apply_operation": OPERATION, "block": BLOCK, "scratch": "scratch",
        "bootstrap_fleet_key": "key", "host_public_key": key(2),
        "first_instance_uid": "old-instance", "first_guest_identity": first["guestIdentity"],
    }

    def read(*unused, seeded):
        assert not seeded
        return first

    monkeypatch.setattr(bootstrap, "verify_guest", read)
    assert action.bootstrap_verify(client(api), arguments, seeded=False)["seedWithdrawn"]
    arguments["first_instance_uid"] = "new-instance"
    with pytest.raises(Refused, match="requires a different instance"):
        action.bootstrap_verify(client(api), arguments, seeded=False)
    arguments["first_instance_uid"] = "old-instance"
    arguments["first_guest_identity"] = bootstrap.verify_answer(answer(machine_id="b" * 32), INTENT)["guestIdentity"]
    with pytest.raises(Refused, match="did not prove the same guest identity"):
        action.bootstrap_verify(client(api), arguments, seeded=False)


def test_seed_refuses_a_clone_whose_root_claim_was_replaced():
    api, target, vm = realized_clone()
    root = api.objects[VM_PATH]["spec"]["dataVolumeTemplates"][0]["metadata"]["name"]
    api.objects[target.pvc(root)]["metadata"]["uid"] = "another-claim"
    with pytest.raises(Refused, match="recorded snapshot-backed root disk"):
        seed.attach(target, vm, OPERATION, BLOCK, payload(), INTENT["mac"])
    assert all(item[0] == "GET" for item in api.requests)
