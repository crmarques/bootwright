"""Synthetic native SDK objects. No test opens a network connection."""

from __future__ import annotations

import datetime
import io
import json
import ssl
from types import SimpleNamespace as NS
from unittest.mock import Mock

import pytest
from pyVmomi import vim

from ansible_collections.bootwright.core.plugins.module_utils import vsphere_hardware as hw
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_machine as machine
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_media as media
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_session as api
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

DEVICE = vim.vm.device
STORE = vim.Datastore("datastore-1")
DISK_ID = "6000c290-0000-0000-0000-000000000001"


def disk(key=2000, unit=0, disk_id=DISK_ID):
    backing = DEVICE.VirtualDisk.FlatVer2BackingInfo(datastore=STORE, fileName="[ds] node/disk%s.vmdk" % unit,
                                                     diskMode="persistent", uuid=disk_id, contentId="0123456789abcdef")
    return DEVICE.VirtualDisk(key=key, controllerKey=1000, unitNumber=unit, backing=backing, capacityInKB=2 << 20)


def guest():
    controller = DEVICE.ParaVirtualSCSIController(key=1000, busNumber=0)
    cd = hw.cdrom(vim, [])[0].device
    hardware = NS(device=[controller, disk(), cd])
    return NS(config=NS(instanceUuid="vm-uuid", changeVersion="1", files=NS(vmPathName="[ds] node/node.vmx"), hardware=hardware, extraConfig=[]),
              runtime=NS(powerState="poweredOff"), snapshot=None, _moId="vm-1")


def make_session(vm):
    session = Mock(vim=vim)
    session.vim = vim
    session.guard.side_effect = lambda v: v.config
    session.pinned_vm.return_value = vm
    return session


def set_options(vm, settings):
    current = api.options(vm.config)
    current.update(settings)
    vm.config.extraConfig = [vim.option.OptionValue(key=k, value=v) for k, v in current.items()]


def test_verified_sdk_receives_only_the_selected_origin_port_and_private_file_contents(monkeypatch, tmp_path):
    import pyVim.connect

    paths = []
    for name in ("principal", "password"):
        path = tmp_path / name
        path.write_text(name)
        path.chmod(0o600)
        paths.append(str(path))
    stub = Mock()
    stub.return_value.RetrieveContent.return_value.about = NS(instanceUuid="vc", apiType="VirtualCenter")
    monkeypatch.setattr(pyVim.connect, "Connect", stub)
    monkeypatch.setattr(pyVim.connect, "Disconnect", Mock())
    with api.connect("https://localhost:8443", *paths, "", True) as session:
        assert session.uuid == "vc"
    params = stub.call_args.kwargs
    assert (params["host"], params["port"], params["user"], params["pwd"]) == ("localhost", 8443, "principal", "password")
    assert params["sslContext"].verify_mode == ssl.CERT_REQUIRED and params["sslContext"].check_hostname
    with pytest.raises(api.Refused), api.connect("https://host:8443", *paths, "", False):
        pass
    assert stub.call_count == 1


@pytest.mark.parametrize("value", ["http://vc.example", "https://a:b@vc.example", "https://vc.example/path", "https://vc.example?token=bad"])
def test_invalid_origins_never_receive_a_credential(value):
    with pytest.raises(api.Refused):
        api.origin(value)


def test_duplicate_names_and_unproved_creation_refuse_without_adopting():
    session = Mock(vim=vim)
    view = session.content.viewManager.CreateContainerView.return_value
    view.view = [NS(name="node"), NS(name="node")]
    with pytest.raises(api.Refused, match="multiple VMs"):
        machine.creation_candidate(session, {"name": "node"})
    view.DestroyView.assert_called_once()
    vm = vim.VirtualMachine("vm-1")
    session.content.eventManager.QueryEvents.return_value = []
    with pytest.raises(api.Refused, match="creation"):
        machine.recovery(session, vm, "2026-01-01T00:00:00Z")
    session.wait.assert_not_called()


def test_creation_event_can_recover_only_before_any_reconfiguration():
    vm = vim.VirtualMachine("vm-8")
    session = Mock(vim=vim)
    session.vim = vim
    event = vim.event.VmCreatedEvent(vm=vim.event.VmEventArgument(vm=vm), createdTime=datetime.datetime.now(datetime.timezone.utc))
    session.content.eventManager.QueryEvents.return_value = [event]
    machine.recovery(session, vm, "2026-01-01T00:00:00Z")
    session.content.eventManager.QueryEvents.return_value = [event, vim.event.VmReconfiguredEvent()]
    with pytest.raises(api.Refused, match="uninterrupted"):
        machine.recovery(session, vm, "2026-01-01T00:00:00Z")


def test_disk_receipt_changes_when_the_backing_is_replaced_even_at_the_same_key_and_path():
    vm = guest()
    req = {"diskGiB": 2, "dataDisks": []}
    original = hw.observed_disks(vim, vm, req, STORE)
    vm.config.hardware.device[1].backing.uuid = "foreign"
    assert hw.observed_disks(vim, vm, req, STORE) != original
    assert original[0] == dict(key=2000, name="root", backing="[ds] node/disk0.vmdk", uuid=DISK_ID, datastore="datastore-1")


def test_destroy_refuses_replaced_disk_running_machine_and_snapshot_without_mutation(monkeypatch):
    monkeypatch.setattr(media, "resolve", lambda *_: (None, STORE))
    monkeypatch.setattr(media, "prove_empty", lambda *_: None)
    vm = guest()
    session = make_session(vm)
    receipts = [hw.disk_receipt(vim, "root", disk())]
    set_options(vm, {machine.DISKS: canonical(receipts).decode()})
    req = dict(datacenter="/dc", stagingDatastore="/dc/datastore/ds", stagingFolder="stage", name="node", owner="owner")
    vm.config.hardware.device[1].backing.uuid = "foreign"
    for reason in ("changed", "stopped", "snapshot"):
        if reason == "stopped":
            vm.runtime.powerState = "poweredOn"
        elif reason == "snapshot":
            vm.runtime.powerState, vm.snapshot = "poweredOff", object()
        with pytest.raises(api.Refused, match=reason):
            pin = dict(disks=receipts, stagingDatastoreRef=STORE._moId, vmHome=vm.config.files.vmPathName, owner="owner:op")
            machine.destroy(session, req, pin, vm)
    session.wait.assert_not_called()


def test_destroy_detaches_an_unowned_disk_without_file_deletion_before_destroy(monkeypatch):
    vm = guest()
    session = make_session(vm)
    receipts = [hw.disk_receipt(vim, "root", disk())]
    set_options(vm, {machine.DISKS: canonical(receipts).decode()})
    other = disk(2001, 1, "foreign")
    vm.config.hardware.device.append(other)
    changes = []

    def reconfigure(_session, _vm, actions, settings, revision):
        changes.extend(actions)
        assert revision == "1"
        vm.config.hardware.device = [dev for dev in vm.config.hardware.device if not isinstance(dev, DEVICE.VirtualDisk)]
        set_options(vm, settings)
    monkeypatch.setattr(api, "config_change", reconfigure)
    monkeypatch.setattr(media, "resolve", lambda *_: (None, STORE))
    monkeypatch.setattr(media, "prove_empty", lambda *_: None)
    monkeypatch.setattr(machine, "prove_absent", lambda *_: {"absent": True})
    vm.UnregisterVM = Mock()
    session.pinned_vm.return_value = None
    req = dict(datacenter="/dc", stagingDatastore="/dc/datastore/ds", stagingFolder="stage", name="node", owner="owner")
    pin = dict(disks=receipts, vmHome=vm.config.files.vmPathName, stagingDatastoreRef=STORE._moId, owner="owner:op")
    assert machine.destroy(session, req, pin, vm)["absent"]
    assert [(c.device.key, c.operation, c.fileOperation) for c in changes] == [(2000, "remove", "destroy"), (2001, "remove", None)]
    vm.UnregisterVM.assert_called_once()
    # Crash after the atomic disk retirement, before VM unregister.
    vm.UnregisterVM.reset_mock()
    changes.clear()
    assert machine.destroy(session, req, pin, vm)["absent"]
    assert changes == []
    vm.UnregisterVM.assert_called_once()


def test_exclusive_reservation_persists_intent_and_never_enters_an_occupied_directory(monkeypatch):
    vm = guest()
    set_options(vm, {api.OWNER: "owner:op"})
    session = make_session(vm)
    volume = dict(folder="stage", name="node")
    sender = Mock()
    monkeypatch.setattr(media, "transfer", sender)
    monkeypatch.setattr(api, "config_change", lambda _s, v, settings, revision: set_options(v, settings))
    manager = session.content.fileManager
    manager.MakeDirectory.side_effect = [None, vim.fault.FileAlreadyExists()]
    ds = NS(name="ds", _moId="datastore-1")
    with pytest.raises(vim.fault.FileAlreadyExists):
        media.stage(session, vm, volume, "image", io.BytesIO(b"iso"), 3, "f" * 64, None, ds)
    assert [call.kwargs["createParentDirectories"] for call in manager.MakeDirectory.call_args_list] == [True, False]
    first = media.read_record(vm, volume)
    assert first["phase"] == "intent"
    sender.assert_not_called()
    manager.MakeDirectory.side_effect = [None, None]
    sender.return_value = True  # Even identical bytes at the final name are foreign.
    with pytest.raises(api.Refused, match="final ISO path is occupied"):
        media.stage(session, vm, volume, "image", io.BytesIO(b"iso"), 3, "f" * 64, None, ds)
    second = media.read_record(vm, volume)
    assert first["reservation"] != second["reservation"] and second["phase"] == "reserved"
    assert first["path"] != second["path"] and second["reservation"] in second["path"]
    assert all(call.args[4] == "HEAD" for call in sender.call_args_list)
    session.wait.assert_not_called()


def test_unreceipted_move_and_changed_payload_refuse_without_using_or_deleting_file(monkeypatch):
    vm = guest()
    session = make_session(vm)
    volume = dict(folder="stage", name="node")
    record = {"size": 3, "sha256": "f" * 64, "phase": "uploading", "path": "reserved/image.iso", "upload": "reserved/image.upload"}
    monkeypatch.setattr(media, "transfer", lambda *_args, **_kwargs: True)
    with pytest.raises(api.Refused, match="successful recorded move"):
        media.upload_and_publish(session, vm, volume, record, io.BytesIO(b"iso"), None, STORE)
    session.content.fileManager.DeleteDatastoreFile_Task.assert_not_called()
    with pytest.raises(api.Refused, match="differs"):
        media.hash_stream(io.BytesIO(b"unrelated"), prefix=io.BytesIO(b"expected"))


def test_seed_is_bound_to_the_stopped_vm_then_erased_after_boot(monkeypatch, tmp_path):
    vm = guest()
    session = make_session(vm)
    monkeypatch.setattr(api, "config_change", lambda _s, v, settings, revision: set_options(v, settings))
    set_options(vm, {api.OWNER: "owner:op"})
    args = dict(seed_identity="digest")
    for key in ("metadata_path", "userdata_path"):
        path = tmp_path / key
        path.write_text("private-" + key)
        path.chmod(0o600)
        args[key] = str(path)
    assert media.seed(session, vm, args, "seed_insert") == {}
    assert media.seed(session, vm, args, "seed_read") == {"seed_state": "own", "power": "Off"}
    vm.runtime.powerState = "poweredOn"
    with pytest.raises(api.Refused, match="foreign"):
        media.seed(session, vm, dict(args, seed_identity="other"), "seed_eject")
    assert media.seed(session, vm, args, "seed_eject") == {}
    assert media.seed(session, vm, args, "seed_read") == {"seed_state": "none", "power": "On"}
    assert api.options(vm.config)["guestinfo.userdata"] == "---"


def test_full_clone_requires_plain_single_root_and_replaces_tpm(monkeypatch):
    vm = guest()
    vm.config.template = True
    vm.config.hardware.device.append(disk(2001, 1))
    with pytest.raises(api.Refused, match="exactly one"):
        hw.template_devices(vim, vm)
    vm.config.hardware.device.pop()
    vm.CloneVM_Task = Mock()
    session = make_session(vm)
    pool, folder, store = vim.ResourcePool("resgroup-2"), vim.Folder("group-v3"), vim.Datastore("datastore-1")
    monkeypatch.setattr(api, "placement", lambda *_: (None, None, pool, folder, store))
    session.lookup.return_value = vm
    req = dict(template="/dc/vm/template", identity={"block": "machine-node"}, name="node", cpu=2, memoryMiB=4096, diskGiB=2, dataDisks=[], interfaces=[])
    hw.provision(session, req, "owner:op")
    clone = vm.CloneVM_Task.call_args.kwargs["spec"]
    assert clone.powerOn is False and clone.template is False and clone.tpmProvisionPolicy == "replace"
    assert clone.location.diskMoveType == "moveAllDiskBackingsAndDisallowSharing" and clone.location.datastore == store
    assert clone.config.firmware == "efi" and clone.config.bootOptions.efiSecureBootEnabled is False
    options = api.options(clone.config)
    assert (options[api.OWNER], options[api.OPERATION], options["disk.EnableUUID"]) == ("owner:op", "machine-node", "TRUE")
    assert json.loads(options["bootwright.templateRoot"])["uuid"] == DISK_ID
    assert clone.config.deviceChange == []


def test_foreign_root_or_added_disk_refuses_the_machine_port_even_with_valid_vm_identity():
    vm = guest()
    root = hw.disk_receipt(vim, "root", disk())
    set_options(vm, {machine.DISKS: canonical([root]).decode(), "disk.EnableUUID": "TRUE"})
    vm.config.firmware = "efi"
    boot = vim.vm.BootOptions
    vm.config.bootOptions = boot(efiSecureBootEnabled=False, bootOrder=[boot.BootableDiskDevice(deviceKey=2000), boot.BootableCdromDevice()])
    hw.port_disks(vim, vm.config, {"disks": [root]})
    vm.config.hardware.device.append(disk(2001, 1, "foreign"))
    with pytest.raises(api.Refused, match="unexpected"):
        hw.port_disks(vim, vm.config, {"disks": [root]})
    vm.config.hardware.device.pop()
    vm.config.hardware.device[1].backing.uuid = "substituted"
    with pytest.raises(api.Refused, match="differs"):
        hw.port_disks(vim, vm.config, {"disks": [root]})


def test_clone_rejects_source_backed_root_before_resize():
    vm = guest()
    req = dict(diskGiB=4, dataDisks=[], interfaces=[])
    set_options(vm, {"bootwright.templateRoot": canonical(hw.disk_receipt(vim, "root", disk())).decode()})
    session = make_session(vm)
    with pytest.raises(api.Refused, match="distinct independent"):
        hw.clone_devices(session, req, vm, (None, None, None, None, STORE), "apply")
    session.wait.assert_not_called()
    assert vm.config.hardware.device[1].capacityInKB == 2 << 20


def test_tpm_allocation_is_part_of_vm_create_and_refuses_without_provider():
    session = make_session(guest())
    get_provider = session.content.cryptoManager.GetDefaultKmsCluster
    get_provider.return_value = None
    spec = vim.vm.ConfigSpec()
    with pytest.raises(api.Refused, match="key provider"):
        hw.tpm(session, spec, None, [], True)
    get_provider.return_value = vim.encryption.KeyProviderId(id="fixture-provider")
    hw.tpm(session, spec, None, [], True)
    assert spec.crypto.cryptoKeyId.providerId.id == "fixture-provider"
    assert isinstance(spec.deviceChange[0].device, DEVICE.VirtualTPM)
    session.content.cryptoManager.GenerateKey.assert_not_called()


def test_changed_config_revision_prevents_disk_removal_and_reconfiguration():
    vm = guest()
    vm.ReconfigVM_Task = Mock()
    session = make_session(vm)
    with pytest.raises(api.Refused, match="changed"):
        api.config_change(session, vm, revision="stale")
    vm.ReconfigVM_Task.assert_not_called()


def test_native_soap_fault_cannot_publish_private_arguments(monkeypatch):
    from ansible_collections.bootwright.core.plugins.action import substrate_vsphere_api as action

    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = NS(args={"operation": "apply"}, check_mode=False)

    def error(*_args):
        raise RuntimeError("private guestinfo content")
    monkeypatch.setattr(action, "execute", error)
    result = plugin.run()
    assert result["failed"] and result["_ansible_no_log"]
    assert "private guestinfo" not in str(result)


def test_trunk_resolution_checks_every_vlan_mtu_and_override_policy():
    class Group:
        pass
    group = Group()
    vlan = vim.dvs.VmwareDistributedVirtualSwitch.TrunkVlanSpec(inherited=False, vlanId=[vim.NumericRange(start=20, end=30)])
    group.key = "pg"
    group.config = NS(distributedVirtualSwitch=NS(config=NS(maxMtu=9000), uuid="switch"),
                      defaultPortConfig=NS(vlan=vlan), policy=NS(vlanOverrideAllowed=False))
    session = Mock()
    session.vim = NS(Network=object, dvs=NS(DistributedVirtualPortgroup=Group, VmwareDistributedVirtualSwitch=vim.dvs.VmwareDistributedVirtualSwitch,
                     PortConnection=vim.dvs.PortConnection), vm=vim.vm)
    session.lookup.return_value = group
    dc = cluster = NS(network=[group])
    iface = dict(portgroup="/dc/network/pg", taggedVLANs=[20, 30], mtu=9000)
    assert hw.network(session, {}, iface, dc, cluster).port.portgroupKey == "pg"
    for amended in (dict(iface, taggedVLANs=[31]), dict(iface, mtu=9200)):
        with pytest.raises(api.Refused):
            hw.network(session, {}, amended, dc, cluster)
    group.config.policy.vlanOverrideAllowed = True
    with pytest.raises(api.Refused):
        hw.network(session, {}, iface, dc, cluster)


def test_guestinfo_payload_replacement_and_foreign_vendordata_block_resume_or_clear(monkeypatch, tmp_path):
    vm = guest()
    set_options(vm, {api.OWNER: "owner:op"})
    session = make_session(vm)
    monkeypatch.setattr(api, "config_change", lambda _s, v, settings, revision: set_options(v, settings))
    path = tmp_path / "private"
    path.write_text("payload")
    path.chmod(0o600)
    args = {"seed_identity": "mine", "metadata_path": str(path), "userdata_path": str(path)}
    media.seed(session, vm, args, "seed_insert")
    set_options(vm, {"guestinfo.vendordata": "foreign"})
    assert media.seed_state(vm.config, "mine") == "foreign"
    set_options(vm, {"guestinfo.vendordata": "", "guestinfo.userdata": "altered"})
    assert media.seed_state(vm.config, "mine") == "foreign"
    with pytest.raises(api.Refused):
        media.seed(session, vm, args, "seed_eject")
    assert api.options(vm.config)["guestinfo.userdata"] == "altered"


def test_recursive_staging_check_retains_empty_directories_and_rejects_any_residual_file():
    vm = guest()
    session = make_session(vm)
    datastore = Mock(name="synthetic")
    datastore.name = "ds"
    folder = vim.host.DatastoreBrowser.FolderInfo(path="nonce")
    file = vim.host.DatastoreBrowser.FileInfo(path="image.iso")
    session.wait.return_value = [NS(file=[folder]), NS(file=[])]
    volume = {"folder": "staging", "name": "node"}
    media.prove_empty(session, datastore, volume)
    session.wait.return_value = [NS(file=[folder]), NS(file=[file])]
    with pytest.raises(api.Refused, match="still contains"):
        media.prove_empty(session, datastore, volume)
    session.content.fileManager.DeleteDatastoreFile_Task.assert_not_called()


def test_datastore_transfer_uses_only_the_verified_vcenter_including_authored_port(monkeypatch):
    vm = guest()
    session = make_session(vm)
    session.url, session.tls = api.origin("https://vc.example.test:8443"), ssl.create_default_context()
    session.service._stub.cookie = 'vmware_soap_session="private"; HttpOnly'
    factory = Mock()
    conn = factory.return_value
    conn.getresponse.return_value.status = 302
    monkeypatch.setattr(media.http.client, "HTTPSConnection", factory)
    with pytest.raises(api.Refused):
        media.transfer(session, {"datacenter": "/Lab DC"}, NS(name="private ds"), "stages/file.iso", "HEAD")
    factory.assert_called_once_with("vc.example.test", 8443, timeout=60, context=session.tls)
    conn.request.assert_called_once_with("HEAD", "/folder/stages/file.iso?dcPath=Lab+DC&dsName=private+ds",
                                         body=None, headers={"Cookie": 'vmware_soap_session="private"'})


def test_block_observes_an_interrupted_full_clone_without_writing_or_requiring_final_data_disks(monkeypatch):
    source = guest()
    source.config.template = True
    source._moId = "vm-10"
    source.config.instanceUuid = "source-uuid"
    source_root = source.config.hardware.device[1]
    source_root.backing.fileName = "[ds] source/source.vmdk"
    vm = guest()
    vm.config.template = False
    vm.config.hardware.device[1].backing.uuid = "new-disk"
    store = NS(name="ds", _moId="datastore-1")
    req = dict(version="machine-vsphere-v1", tlsVerify=True, identity={"block": "machine-node"}, owner="a" * 64, name="node",
               template="/dc/vm/source", cpu=2, memoryMiB=2048, diskGiB=4, dataDisks=[{"name": "data", "sizeGiB": 8}],
               interfaces=[{"name": "ens1", "mtu": 1500}], datacenter="/dc", stagingDatastore="/dc/datastore/ds", stagingFolder="stage")
    op = dict(operation="op-42", block="machine-node", registered="2026-01-01T00:00:00Z", pinned="{}")
    set_options(vm, {api.OWNER: req["owner"] + ":op-42", api.OPERATION: "machine-node",
                "bootwright.templateRoot": canonical(hw.template_receipt(vim, source, source_root)).decode()})
    session = make_session(vm)
    session.uuid = "vc-uuid"
    session.lookup.return_value = source
    monkeypatch.setattr(machine, "creation_candidate", lambda *_: vm)
    monkeypatch.setattr(machine, "recovery", lambda *_: None)
    monkeypatch.setattr(media, "resolve", lambda *_: (None, store))
    # Give the synthetic datastore a real SDK ref so typed device backing can
    # be compared directly, while its display name is served from a stub.
    object.__setattr__(STORE, "_stub", Mock())
    STORE._stub.InvokeAccessor.return_value = "ds"
    monkeypatch.setattr(api, "placement", lambda *_: (None, None, None, None, STORE))
    try:
        changed, obs = machine.block(session, "observe", req, op)
    finally:
        object.__setattr__(STORE, "_stub", None)
    assert not changed and obs["owned"] and not obs["postcondition"] and obs["power"] == "poweredOff"
    assert obs["disks"] == [hw.disk_receipt(vim, "root", vm.config.hardware.device[1])]
    session.wait.assert_not_called()


def test_boot_media_refuses_a_detached_but_staged_or_foreign_image_before_power(monkeypatch):
    vm = guest()
    session = make_session(vm)
    for answer in ({"media_state": "own", "media": "staged"}, {"media_state": "foreign", "media": "foreign"}):
        monkeypatch.setattr(machine, "state", lambda *_: answer)
        with pytest.raises(api.Refused, match="not connected"):
            machine.boot_media(session, vm, {}, {"media_identity": "expected"})
    session.wait.assert_not_called()


def run_action(module, args, check=False):
    action = module.ActionModule.__new__(module.ActionModule)
    action._task = NS(args=args, check_mode=check)
    return action.run()


def test_bounded_completion_publishes_only_projected_disk_fields_and_proved_mutations(monkeypatch):
    from ansible_collections.bootwright.core.plugins.action import substrate_vsphere_protocol as protocol

    emitted = []
    monkeypatch.setattr(protocol, "emit", emitted.append)
    receipt = hw.disk_receipt(vim, "root", disk())
    reading = dict(machine.evidence(), answered=True, disks=[dict(receipt, private="unpublished")])
    args = dict(phase="completed", digest="f" * 64, outcome="unchanged", observation=reading)
    assert run_action(protocol, args)["failed"]
    assert emitted == []
    assert run_action(protocol, dict(args, observed=True)) == {"changed": False}
    assert emitted[0]["evidence"] == dict(reading, disks=[receipt], request=args["digest"])
    for fields in ({"owner": "bad\nowner"}, {"answered": 1}, {"disks": [receipt] * 61}, {"disks": [dict(receipt, key=True)]}):
        assert run_action(protocol, dict(args, observed=True, observation=dict(reading, **fields)))["failed"]
    assert len(emitted) == 1


def test_action_dispatch_preserves_check_mode_and_refuses_unverified_origin_before_invoking_the_sdk(monkeypatch):
    from contextlib import nullcontext
    from ansible_collections.bootwright.core.plugins.action import substrate_vsphere_api as action

    refused = run_action(action, {"operation": "power"}, check=True)
    assert refused["failed"] and "check mode" in refused["msg"]
    vm = guest()
    session = make_session(vm)
    session.uuid = "vc-uuid"
    endpoint = "https://vc.example.test:8443"
    connect = Mock(return_value=nullcontext(session))
    monkeypatch.setattr(action, "connect", connect)
    block = Mock(return_value=(False, machine.evidence()))
    monkeypatch.setattr(machine, "block", block)
    req = dict(version="machine-vsphere-v1", server=endpoint, tlsVerify=True, identity={"block": "node"}, owner="a" * 64, name="node",
               cpu=2, memoryMiB=4096, diskGiB=2, dataDisks=[], interfaces=[{"mtu": 1500}])
    materials = dict(operation="op", block="node", controllerUser="u", controllerPassword="p", controllerCA="c")
    observe = dict(operation="observe", request=req, material=materials)
    assert run_action(action, observe, check=True) == {"changed": False, "_ansible_no_log": True, "observation": machine.evidence()}
    connect.assert_called_once_with(endpoint, "u", "p", "c", True)
    foreign = api.Refused("private", "identity-mismatch")
    block.side_effect = foreign
    assert run_action(action, observe, check=True)["observation"] == dict(machine.evidence(), foreign=True)
    port = {"endpoint": endpoint, "user": "u", "password": "p", "ca": "c", "verify": True}
    read = Mock(return_value={"power": "Off"})
    monkeypatch.setattr(machine, "port", read)
    assert run_action(action, dict(operation="state_read", port=port), check=True)["power"] == "Off"
    read.assert_called_once_with(session, "state_read", port)
    connect.reset_mock()
    assert not run_action(action, dict(observe, request=dict(req, tlsVerify=False)))["observation"]["answered"]
    connect.assert_not_called()


class DatastoreFiles:
    """An isolated vCenter HTTPS file service plus the corresponding no-clobber file manager."""

    def __init__(self):
        self.files = {}
        self.writes = []
        self.moves = []
        self.removals = []

    def connection(self, *_args, **_kwargs):
        from urllib.parse import unquote, urlsplit

        conn = Mock()

        def request(method, url, body, headers):
            path = unquote(urlsplit(url).path).removeprefix("/folder/")
            assert headers["Cookie"] == 'vmware_soap_session="fixture"'
            if method == "PUT":
                payload = body.read()
                assert len(payload) == int(headers["Content-Length"])
                self.files[path] = payload
                self.writes.append(path)
            stream = io.BytesIO(self.files.get(path, b""))
            stream.status = 200 if path in self.files else 404
            conn.getresponse.return_value = stream
        conn.request.side_effect = request
        return conn

    def move(self, sourceName, destinationName, force, **_kwargs):
        source, destination = sourceName.removeprefix("[ds] "), destinationName.removeprefix("[ds] ")
        assert force is False and destination not in self.files
        self.files[destination] = self.files.pop(source)
        self.moves.append((source, destination))
        return vim.Task("task-11")

    def remove(self, name, **_kwargs):
        path = name.removeprefix("[ds] ")
        self.removals.append(path)
        del self.files[path]

    def listing(self, **_kwargs):
        return [NS(file=[vim.host.DatastoreBrowser.FileInfo(path=p) for p in self.files])]


def media_machine(monkeypatch):
    # Reading a device property over SOAP returns a detached object. An edit
    # affects the registered VM only after its revision-bound task is applied.
    from copy import copy

    files = DatastoreFiles()
    browser = Mock()
    browser.SearchDatastoreSubFolders_Task.side_effect = files.listing
    properties = dict(name="ds", summary=NS(accessible=True), browser=browser)
    stub = Mock()
    stub.InvokeAccessor.side_effect = lambda _mo, info: properties[info.name]
    store = vim.Datastore("datastore-1", stub)
    vm = guest()
    vm.name = "node"
    vm.config.keyId = None
    vm.config.firmware = "efi"
    vm.config.bootOptions = hw.order_options(vim, 2000)
    vm.config.hardware.device[1].backing.datastore = store
    root = hw.disk_receipt(vim, "root", vm.config.hardware.device[1])
    set_options(vm, {api.OWNER: "owner:op", machine.DISKS: canonical([root]).decode(), "disk.EnableUUID": "TRUE"})
    session = make_session(vm)
    session.service._stub.cookie = 'vmware_soap_session="fixture"; HttpOnly'
    session.url, session.tls = api.origin("https://vc.example.test:8443"), ssl.create_default_context()
    dc = NS(datastore=[store])
    session.lookup.side_effect = lambda _path, kind: dc if kind == vim.Datacenter else store
    session.content.fileManager.MoveDatastoreFile_Task.side_effect = files.move
    session.content.fileManager.DeleteDatastoreFile_Task.side_effect = files.remove
    session.wait.side_effect = lambda task, **_kwargs: task
    original_drive = media.drive
    monkeypatch.setattr(media, "drive", lambda s, v: copy(original_drive(s, v)))
    monkeypatch.setattr(media.http.client, "HTTPSConnection", files.connection)

    def guard(checked):
        assert checked is vm
        session.check_hardware(vm.config)
        return vm.config

    def reconfigure(spec):
        assert spec.changeVersion == vm.config.changeVersion
        vm.config.changeVersion = str(int(spec.changeVersion) + 1)
        for change in spec.deviceChange:
            assert change.operation == "edit" and isinstance(change.device, DEVICE.VirtualCdrom)
            vm.config.hardware.device = [change.device if d.key == change.device.key else d for d in vm.config.hardware.device]
        set_options(vm, api.options(spec))
        if spec.bootOptions:
            vm.config.bootOptions = spec.bootOptions
    session.guard.side_effect = guard
    vm.ReconfigVM_Task = Mock(side_effect=reconfigure)
    vm.PowerOnVM_Task = Mock(side_effect=lambda: setattr(vm.runtime, "powerState", "poweredOn"))
    vm.PowerOffVM_Task = Mock(side_effect=lambda: setattr(vm.runtime, "powerState", "poweredOff"))
    return vm, session, root, files


def test_private_iso_attach_boot_and_eject_use_original_disk_pin_and_prove_staged_file_absent(monkeypatch, tmp_path):
    import base64

    vm, session, root, files = media_machine(monkeypatch)
    pin = dict(stagingDatastoreRef="datastore-1", instanceUUID=vm.config.instanceUuid, moRef=vm._moId, owner="owner:op", disks=[root])
    volume = dict(datacenter="/dc", datastore="/dc/datastore/ds", folder="stage", name="node", owner="owner")
    iso = tmp_path / "installer.iso"
    iso.write_bytes(b"synthetic installer")
    args = dict(pinned_uuid_base64=base64.b64encode(b"vc").decode(), pinned_serial_base64=base64.b64encode(canonical(pin)).decode(),
                media_volume=json.dumps(volume), media_identity="bound-digest", image_path=str(iso))
    assert machine.port(session, "media_insert", args) == {}
    record = media.read_record(vm, volume)
    assert record["phase"] == "published" and files.files == {record["path"]: iso.read_bytes()}
    assert files.moves == [(record["upload"], record["path"])] and files.writes == [record["upload"]]
    assert machine.port(session, "media_insert", args) == {} and len(files.moves) == len(files.writes) == 1
    assert machine.port(session, "state_read", args) == dict(power="Off", power_state="off", media="bound-digest", media_state="own")
    assert machine.port(session, "state_read", dict(args, media_identity="other"))["media_state"] == "foreign"
    with pytest.raises(api.Refused, match="another installation"):
        machine.port(session, "media_eject", dict(args, media_identity="other"))
    assert machine.port(session, "boot_media", args) == {"power": "On", "power_state": "on"}
    assert isinstance(vm.config.bootOptions.bootOrder[0], vim.vm.BootOptions.BootableCdromDevice)
    with pytest.raises(api.Refused, match="released"):
        machine.port(session, "boot_disk", args)
    machine.port(session, "boot_media", args)
    vm.PowerOffVM_Task.assert_called_once()
    machine.port(session, "power", dict(args, power="off", force=True))
    assert machine.port(session, "media_eject", args) == dict(power="Off", power_state="off", media="", media_state="none")
    assert not files.files and files.removals == [record["path"]]
    assert hw.boot_order(vim, vm.config, root["key"])
    assert machine.port(session, "boot_disk", dict(args, power_on=True)) == {"power": "On", "power_state": "on"}
    assert machine.port(session, "media_eject", args)["media_state"] == "none"
    assert files.removals == [record["path"]]


def test_empty_disk_creation_seals_original_receipt_and_replay_refuses_a_substituted_backing(monkeypatch):
    vm = guest()
    session = make_session(vm)
    session.uuid = "original-vcenter"
    session.wait.side_effect = lambda task: task
    session.content.viewManager.CreateContainerView.return_value.view = []
    stub = Mock()
    stub.InvokeAccessor.return_value = "ds"
    store = vim.Datastore("datastore-1", stub)
    network_stub = Mock()
    network_stub.InvokeAccessor.return_value = "guests"
    network = vim.Network("network-1", network_stub)
    host = NS(config=NS(network=NS(portgroup=[NS(spec=NS(name="guests", vswitchName="switch"))], vswitch=[NS(name="switch", mtu=1500)])))
    dc, cluster, pool, folder = NS(network=[network]), NS(network=[network], host=[host]), vim.ResourcePool("resgroup-1"), Mock()
    vm.parent, vm.resourcePool, vm.datastore = folder, pool, [store]
    session.lookup.return_value = network
    monkeypatch.setattr(api, "placement", lambda *_: (dc, cluster, pool, folder, store))
    monkeypatch.setattr(media, "resolve", lambda *_: (dc, store))
    creation_proof = Mock()
    monkeypatch.setattr(machine, "recovery", creation_proof)
    req = dict(version="machine-vsphere-v1", tlsVerify=True, identity={"block": "node"}, owner="a" * 64, name="node", template="", tpm=False,
               cpu=2, memoryMiB=4096, diskGiB=2, dataDisks=[], interfaces=[{"mtu": 1500, "macAddress": "00:50:56:00:00:01", "portgroup": "/dc/network/guests"}],
               datacenter="/dc", stagingDatastore="/dc/datastore/ds", stagingFolder="stage")
    material = dict(operation="allocate", block="node", registered="2026-01-01T00:00:00Z", pinned="{}")

    def create(config, pool):
        assert pool == vm.resourcePool and config.files.vmPathName == "[ds]"
        disks = [change for change in config.deviceChange if isinstance(change.device, DEVICE.VirtualDisk)]
        assert len(disks) == 1 and disks[0].fileOperation == "create" and disks[0].device.backing.fileName == "[ds]"
        vm.config.template = False
        vm.config.hardware.numCPU, vm.config.hardware.memoryMB = config.numCPUs, config.memoryMB
        vm.config.firmware, vm.config.bootOptions = config.firmware, config.bootOptions
        set_options(vm, api.options(config))
        vm.config.hardware.device.extend(d.device for d in config.deviceChange if isinstance(d.device, DEVICE.VirtualEthernetCard))
        return vm

    def reconfigure(spec):
        assert spec.changeVersion == vm.config.changeVersion
        vm.config.changeVersion = str(int(spec.changeVersion) + 1)
        set_options(vm, api.options(spec))
        if spec.bootOptions:
            vm.config.bootOptions = spec.bootOptions
    folder.CreateVM_Task.side_effect = create
    vm.ReconfigVM_Task = Mock(side_effect=reconfigure)
    changed, evidence = machine.block(session, "apply", req, material)
    assert changed and evidence["postcondition"] and evidence["power"] == "poweredOff"
    assert evidence["disks"] == [hw.disk_receipt(vim, "root", vm.config.hardware.device[1])]
    assert evidence["owner"] == req["owner"] + ":allocate" and creation_proof.call_count == 1
    saved = dict(material, pinned=json.dumps(evidence))
    for operation in ("observe", "apply"):
        assert machine.block(session, operation, req, saved) == (False, evidence)
    vm.config.hardware.device[1].backing.uuid = "different-backing"
    with pytest.raises(api.Refused, match="differ"):
        machine.block(session, "apply", req, saved)
    folder.CreateVM_Task.assert_called_once()
    assert vm.ReconfigVM_Task.call_count == 2
