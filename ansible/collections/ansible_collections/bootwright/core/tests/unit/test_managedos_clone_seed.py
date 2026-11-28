"""The protected first-boot documents are delivered before disk boot, once per VM."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
import yaml

from ansible_collections.bootwright.core.tests.unit import role_tasks

ROLE = Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
GOLDENS = json.loads((Path(__file__).parent / "goldens" / "managedos_request_goldens.json").read_text())["requests"]
DIGEST = "b" * 64
INSTANCE = "438e485f-77c4-41fd-a02c-4667c2549fff"


@pytest.mark.parametrize("state,power", [("none", "Off"), ("own", "Off"), ("own", "On")])
def test_seed_is_checked_before_disk_boot_and_resumed_without_seeding_a_running_vm(tmp_path, state, power):
    key = tmp_path / "host-identity"
    key.write_text("private test fixture\n")
    request = GOLDENS["request-vsphere-clone"]
    variables = {"managedos_install_anaconda_port_access": {}, "managedos_install_anaconda_scratch": str(tmp_path),
                 "bootwright_os_install_digest": DIGEST, "bootwright_os_install_request": request,
                 "managedos_install_anaconda_instance_id": INSTANCE,
                 "bootwright_os_install_material": {"authorizedKey": "fleet-public", "hostKey": "host-public",
                                                    "marker": "request-marker", "hostIdentity": str(key)}}
    documents, operations = {}, []

    def execute(task, templar):
        nonlocal state
        if "bootwright.core.private_file" in task:
            assert task["diff"] is False and task["no_log"] is True
            args = templar.template(task["bootwright.core.private_file"])
            documents[Path(args["path"]).name] = args["content"]
            return {}
        include = task.get("ansible.builtin.include_role")
        if not include:
            return None
        entry = include["tasks_from"]
        operations.append(entry)
        if entry == "seed_read":
            variables["substrate_machine_port_result"] = {"seed_state": state, "power": power}
        if entry == "seed_insert":
            assert power == "Off" and documents.keys() == {"metadata", "userdata"}
            state = "own"
        return {}

    role_tasks.Interpreter(ROLE, variables, execute).run("clone_boot.yml")
    if power == "On":
        assert operations == ["seed_read"] and not documents
        return
    assert operations == ["seed_read", "pre_boot", "seed_insert", "seed_read", "boot_disk"]
    metadata = json.loads(documents["metadata"])
    assert metadata == {"instance-id": INSTANCE, "local-hostname": request["hostname"], "network": request["cloudInit"]["network"]}
    assert documents["userdata"].startswith("#cloud-config\n")
    user = yaml.safe_load(documents["userdata"])
    assert user["ssh_keys"] == {"ed25519_private": key.read_text(), "ed25519_public": "host-public"}
    assert user["users"][0]["ssh_authorized_keys"] == ["fleet-public"]
    assert user["users"][0]["lock_passwd"] and not user["ssh_pwauth"]
    assert [(file["path"], file["content"]) for file in user["write_files"]][-2:] == [
        (request["markerPath"], "request-marker"), (request["hostKeyPath"], "host-public")]


@pytest.mark.parametrize("clone", [False, True], ids=["iso", "clone"])
def test_final_eject_returns_the_power_and_positive_media_proof_the_installation_publishes(monkeypatch, clone):
    from base64 import b64encode

    from ansible.template import Templar
    from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol
    from ansible_collections.bootwright.core.plugins.module_utils import vsphere_machine, vsphere_media
    from ansible_collections.bootwright.core.tests.unit.plugins.action.test_managedos_install_protocol import need, proved
    from ansible_collections.bootwright.core.tests.unit.plugins.action.test_vsphere_machine import STORE, guest, make_session

    vm = guest()
    vm.name, vm.runtime.powerState = "node", "poweredOn"
    session = make_session(vm)
    monkeypatch.setattr(vsphere_media, "resolve", lambda *_: (None, STORE))
    pin = {"owner": "owner:op", "stagingDatastoreRef": STORE._moId}
    port = {"media_volume": json.dumps({"owner": "owner", "name": vm.name}), "media_identity": DIGEST,
            "pinned_serial_base64": b64encode(json.dumps(pin).encode()).decode(), "pinned_uuid_base64": b64encode(b"vc").decode()}
    ejected = vsphere_machine.port(session, "media_eject", port)
    assert ejected["media_state"] == "none"
    variables = {"managedos_install_anaconda_ejected": ejected}
    templar = Templar(loader=role_tasks.LOADER, variables=variables)
    applied = role_tasks.LOADER.load_from_file(str(ROLE / "tasks/apply.yml"), trusted_as_template=True)
    publish = next(t["bootwright.core.managedos_install_protocol"] for t in role_tasks.flatten(applied)
                   if t.get("bootwright.core.managedos_install_protocol", {}).get("phase") == "completed")
    result = {key: templar.template(publish[key]) for key in ("media", "power")}
    evidence = managedos_install_protocol.presence(proved(**result, observation={"private": False}, privateDelivery=True,
                                                          requires=need(cloudInit=clone, tpm2=True), cloudInit=clone, tpm2=True), DIGEST)
    assert evidence["postcondition"] and evidence["power"] == "On"
