"""vSphere machine lifecycle with independent identity, backing and power proofs."""

from __future__ import annotations

import base64
import datetime
import json
import re
import time

from ansible_collections.bootwright.core.plugins.module_utils import vsphere_hardware as hw
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_media as media
from ansible_collections.bootwright.core.plugins.module_utils import vsphere_session as api
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical

DISKS = "bootwright.disks"


def evidence():
    return dict(answered=False, absent=False, retiring=False, owned=False, foreign=False, postcondition=False,
                vcenterUUID="", instanceUUID="", moRef="", owner="", power="", disks=[], stagingDatastoreRef="", vmHome="")


def creation_candidate(session, req):
    # A vCenter-wide collision refuses even if the other VM is outside the
    # selected folder. Later operations use exclusively the recorded MoRef.
    view = session.content.viewManager.CreateContainerView(session.content.rootFolder, [session.vim.VirtualMachine], True)
    try:
        matches = [vm for vm in view.view if vm.name == req["name"]]
    finally:
        view.DestroyView()
    api.require(len(matches) <= 1, "multiple VMs have the requested name", "identity-mismatch")
    return matches[0] if matches else None


def validate_request(req, material):
    api.require(req["version"] == "machine-vsphere-v1" and req["tlsVerify"] is True and req["identity"]["block"] == material["block"],
                "invalid frozen vSphere request or operation attribution")
    for name, limit in (("cpu", 1024), ("memoryMiB", 16777216), ("diskGiB", 65536)):
        api.require(api.integer(req[name]) and 0 < req[name] <= limit, "invalid vSphere resource capacity")
    api.require(re.fullmatch(r"[A-Za-z0-9_.-]+", req["name"]) and re.fullmatch(r"[0-9a-f]{64}", req["owner"])
                and isinstance(material["operation"], str) and 0 < len(material["operation"]) <= 128,
                "invalid Machine name or creation identity")
    api.require(req["interfaces"] and all(api.integer(d["sizeGiB"]) and 0 < d["sizeGiB"] <= 65536
                                          for d in req["dataDisks"]), "invalid VMDK capacity or empty interface list")
    for interface in req["interfaces"]:
        tags = interface.get("taggedVLANs", [])
        api.require(api.integer(interface["mtu"]) and 576 <= interface["mtu"] <= 65536
                    and all(api.integer(t) and 1 <= t <= 4094 for t in tags), "invalid portgroup MTU or VLAN declaration")
    return req["owner"] + ":" + material["operation"]


def recovery(session, vm, registered, customized=False):
    # Without a disk receipt, a substituted disk in a valid slot could be
    # mistaken for the initial backing. Require the creation event and absence
    # of any intervening reconfiguration, relocation or snapshot.
    ev = session.vim.event
    spec = ev.EventFilterSpec(entity=ev.EventFilterSpec.ByEntity(entity=vm, recursion="self"),
                              eventTypeId=["VmCreatedEvent", "VmClonedEvent", "VmDeployedEvent", "VmReconfiguredEvent",
                                           "VmRelocatedEvent", "VmMigratedEvent", "VmRemovedEvent", "VmRegisteredEvent"],
                              disableFullMessage=True, maxCount=8)
    events = session.content.eventManager.QueryEvents(filter=spec)
    if customized:
        edits = [e for e in events if isinstance(e, ev.VmReconfiguredEvent)]
        api.require(len(edits) == 1 and edits[0].configSpec and api.options(edits[0].configSpec).get("bootwright.cloneRoot")
                    == api.options(vm.config).get("bootwright.cloneRoot"), "the clone has an unaccounted hardware reconfiguration")
        events = [e for e in events if not isinstance(e, ev.VmReconfiguredEvent)]
    since = datetime.datetime.fromisoformat(registered.replace("Z", "+00:00"))
    api.require(len(events) == 1 and isinstance(events[0], (ev.VmCreatedEvent, ev.VmClonedEvent, ev.VmDeployedEvent))
                and events[0].createdTime >= since and events[0].vm.vm == vm,
                "the original creation and uninterrupted disk ownership cannot be recovered")


def pin_for(session, vm, owner):
    config = vm.config
    api.require(config and config.instanceUuid and not config.template and api.options(config).get(api.OWNER) == owner,
                "the VM's creation mark or identity differs", "identity-mismatch")
    return dict(vcenterUUID=session.uuid, instanceUUID=config.instanceUuid, moRef=vm._moId, owner=owner)


def same_network(vim, actual, expected):
    ethernet = vim.vm.device.VirtualEthernetCard
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, ethernet.DistributedVirtualPortBackingInfo):
        return (actual.port.switchUuid, actual.port.portgroupKey) == (expected.port.switchUuid, expected.port.portgroupKey)
    return actual.network == expected.network


def hardware(session, vm, req, where, receipts):
    config = vm.config
    dc, cluster, pool, folder, store = where
    device = session.vim.vm.device
    api.require(vm.parent == folder and vm.resourcePool == pool and store in vm.datastore, "VM placement differs from the frozen inventory")
    api.require(config.firmware == "efi" and config.bootOptions and config.bootOptions.efiSecureBootEnabled is False
                and api.options(config).get("disk.EnableUUID", "").upper() == "TRUE", "VM EFI, secure boot or disk.EnableUUID drifted")
    api.require(config.hardware.numCPU == req["cpu"] and config.hardware.memoryMB == req["memoryMiB"], "VM compute capacity drifted")
    cards = [d for d in config.hardware.device if isinstance(d, device.VirtualEthernetCard)]
    api.require(len(cards) == len(req["interfaces"]), "VM interface count drifted")
    for iface in req["interfaces"]:
        matches = [d for d in cards if d.macAddress == iface["macAddress"] and d.addressType == "manual"]
        expected = hw.network(session, req, iface, dc, cluster)
        api.require(len(matches) == 1 and same_network(session.vim, matches[0].backing, expected), "VM interface binding drifted")
    tpms = [d for d in config.hardware.device if isinstance(d, device.VirtualTPM)]
    api.require(len(tpms) == int(req["tpm"]) and (not tpms or config.keyId and config.keyId.keyId and not getattr(tpms[0], "readOnly", False)),
                "VM persistent vTPM or encryption identity drifted")
    media.drive(session, vm)
    return hw.authorized_order(session.vim, config, receipts[0]["key"])


def volume_for(req):
    return dict(datacenter=req["datacenter"], datastore=req["stagingDatastore"], folder=req["stagingFolder"],
                name=req["name"], owner=req["owner"])


def prove_absent(session, req, pin):
    volume = volume_for(req)
    dc, staging = media.resolve(session, volume)
    api.require(not pin.get("stagingDatastoreRef") or pin["stagingDatastoreRef"] == staging._moId,
                "the original staging datastore identity changed", "identity-mismatch")
    media.prove_empty(session, staging, volume)
    if pin.get("disks"):
        prove_disks_absent(session, req, pin, dc)
    return dict(evidence(), answered=True, absent=True, postcondition=True, vcenterUUID=session.uuid,
                stagingDatastoreRef=staging._moId, vmHome=pin.get("vmHome", ""))


def prove_disks_absent(session, req, pin, dc):
    store = session.lookup(req["datastore"], session.vim.Datastore)
    api.require(store in dc.datastore and store.summary.accessible
                and [s for s in dc.datastore if s.name == store.name] == [store], "recorded disk datastore is unavailable or ambiguous")
    for disk in pin["disks"]:
        api.require(store._moId == disk["datastore"] and disk["backing"].startswith("[%s] " % store.name),
                    "the recorded disk datastore changed", "identity-mismatch")
        try:
            session.content.virtualDiskManager.QueryVirtualDiskUuid(name=disk["backing"], datacenter=dc)
        except session.vim.fault.FileNotFound:
            continue
        raise api.Refused("a recorded VMDK still exists after VM removal")


def retiring(session, req, pin, vm):
    config = vm.config
    api.off(vm)
    api.require(pin.get("moRef") and api.options(config).get("bootwright.removed") == pin["owner"]
                and api.options(config).get(DISKS) == canonical(pin["disks"]).decode()
                and config.files.vmPathName == pin["vmHome"] and not vm.snapshot,
                "the VM cannot prove the original guarded disk removal")
    owned = {d["key"] for d in pin["disks"]}
    api.require(not any(d.key in owned for _slot, d in hw.disks(session.vim, config)),
                "an original VMDK key is present after disk retirement")
    volume = volume_for(req)
    dc, stage = media.resolve(session, volume)
    api.require(stage._moId == pin["stagingDatastoreRef"] and not api.options(config).get(api.STAGE)
                and media.seed_state(config, "") == "none", "retirement cannot prove cleanup of the original boot material")
    media.prove_empty(session, stage, volume)
    prove_disks_absent(session, req, pin, dc)
    return dict(evidence(), answered=True, retiring=True, owned=True, vcenterUUID=session.uuid, moRef=vm._moId,
                instanceUUID=config.instanceUuid, owner=pin["owner"], power="poweredOff", disks=pin["disks"],
                stagingDatastoreRef=stage._moId, vmHome=config.files.vmPathName)


def destroy(session, req, pin, vm):
    api.off(vm)
    api.require(pin.get("disks") and not vm.snapshot, "removal requires pinned disk receipts and a snapshot-free Machine")
    options = api.options(vm.config)
    api.require(options.get(DISKS) == canonical(pin["disks"]).decode(), "the VM disk receipt differs from the original operation")
    volume = volume_for(req)
    _dc, staging = media.resolve(session, volume)
    api.require(staging._moId == pin["stagingDatastoreRef"], "the staging datastore was replaced")
    if options.get(api.STAGE) or media.attachment(session, vm)[0]:
        media.eject(session, vm, volume)
    media.prove_empty(session, staging, volume)
    if options.get(api.SEED):
        api.require(media.checked_seed(vm) is not None, "guestinfo seed custody is unproved")
        media.clear_seed(session, vm)
    api.require(media.seed_state(vm.config, "") == "none", "a foreign guestinfo payload remains on the VM")
    session.guard(vm)
    api.off(vm)
    config = vm.config
    api.require(config.files.vmPathName == pin["vmHome"] and not vm.snapshot, "the registered VM home or snapshot state changed")
    revision = config.changeVersion
    owned = {d["key"]: d for d in pin["disks"]}
    attached = hw.disks(session.vim, config)
    remaining = {d.key: d for _slot, d in attached if d.key in owned}
    removed = api.options(config).get("bootwright.removed") == pin["owner"]
    if removed:
        api.require(not remaining, "a disk appeared at an owned device key after removal")
    else:
        api.require(remaining.keys() == owned.keys() and all(hw.disk_receipt(session.vim, item["name"], remaining[key]) == item
                                                             for key, item in owned.items()), "a recorded disk key or backing identity changed")
        # Unconditional DestroyVM can delete a disk attached after inspection.
        # Only the proven keys request backing destruction, in the very
        # configuration revision from which those identities were read.
        changes = [hw.device_change(session.vim, d, "remove", "destroy" if d.key in owned else None) for _slot, d in attached]
        api.config_change(session, vm, changes, settings={"bootwright.removed": pin["owner"]}, revision=revision)
    session.guard(vm)
    api.off(vm)
    config = vm.config
    api.require(api.options(config).get("bootwright.removed") == pin["owner"] and not any(
        d.key in owned for _slot, d in hw.disks(session.vim, config)), "vCenter did not prove the guarded disk removal")
    # Unregister preserves all backing files, including disks attached after
    # the guarded removal, along with the original VMX, NVRAM and logs.
    vm.UnregisterVM()
    api.require(session.pinned_vm(pin) is None, "the original VM is still registered")
    return prove_absent(session, req, pin)


def block(session, operation, req, material):
    owner = validate_request(req, material)
    pin = json.loads(material.get("pinned", "{}"))
    if pin.get("moRef"):
        api.require(owner == pin["owner"], "the receipt belongs to a different creation", "identity-mismatch")
        vm = session.pinned_vm(pin)
    else:
        api.require(not pin.get("vcenterUUID") or pin["vcenterUUID"] == session.uuid, "the vCenter identity changed", "identity-mismatch")
        vm = creation_candidate(session, req)
    changed = False
    if vm is None:
        if operation != "apply":
            return False, prove_absent(session, req, pin)
        api.require(not pin.get("moRef"), "the originally recorded VM is absent; re-creation requires a fresh operation")
        vm = hw.provision(session, req, owner)
        changed = True
    live = pin_for(session, vm, owner)
    api.require(session.pinned_vm(live) == vm, "VM identity changed during the operation", "identity-mismatch")
    api.require(api.options(vm.config).get(api.OPERATION) == req["identity"]["block"], "the VM belongs to a different creation block")
    if operation != "destroy" and api.options(vm.config).get("bootwright.removed"):
        api.require(operation == "observe", "the VM is retiring; complete its guarded removal before provisioning")
        return False, retiring(session, req, pin, vm)
    if operation == "destroy":
        api.require(pin.get("moRef"), "removal requires the original VM and VMDK receipts")
        return True, destroy(session, req, pin, vm)
    where = api.placement(session, req)
    _dc, staging = media.resolve(session, volume_for(req))
    home = vm.config.files.vmPathName
    api.require(isinstance(home, str) and home.endswith(".vmx") and home.startswith("[%s] " % where[-1].name), "the VM home is unproved")
    api.require(not pin.get("moRef") or (pin["vmHome"], pin["stagingDatastoreRef"]) == (home, staging._moId),
                "the VM home or staging datastore changed", "identity-mismatch")
    config = vm.config
    stored = api.options(config).get(DISKS)
    if req.get("template"):
        def prove_initial():
            recovery(session, vm, material["registered"])
        clone = hw.clone_devices(session, req, vm, where, operation, prove_initial)
        if isinstance(clone, dict):
            api.require(not pin.get("disks") or pin["disks"] == [clone], "the creating clone changed its root backing")
            return False, dict(evidence(), **live, answered=True, owned=True, disks=[clone], power=vm.runtime.powerState,
                               vmHome=home, stagingDatastoreRef=staging._moId)
    # Clone device configuration may have advanced the native version. Prove
    # its one expected event and the disk set against the exact revision sealed.
    config = vm.config
    revision = config.changeVersion
    if not stored:
        recovery(session, vm, material["registered"], bool(api.options(config).get("bootwright.cloneRoot")))
    receipts = hw.observed_disks(session.vim, vm, req, where[-1], config=config)
    stored = api.options(vm.config).get(DISKS)
    if req.get("template"):
        api.require(api.options(vm.config).get("bootwright.cloneRoot") == canonical(receipts[0]).decode(),
                    "the cloned root backing differs from its initial independent receipt")
    if stored:
        api.require(stored == canonical(receipts).decode(), "the live disk identities differ from the original VM receipt")
    else:
        api.off(vm)
        directory = home.rsplit("/", 1)[0] + "/"
        api.require("/" in home and len(hw.disks(session.vim, config)) == len(receipts)
                    and all(disk["backing"].startswith(directory) and "/" not in disk["backing"][len(directory):] for disk in receipts),
                    "initial VM contains unexpected disks or a backing outside its own VM home")
    if pin.get("disks"):
        same = receipts == pin["disks"]
        if req.get("template") and not pin.get("postcondition") and not pin.get("retiring"):
            same = pin["disks"] == receipts[:1] or same
        api.require(same, "a recorded disk identity changed", "identity-mismatch")
    proved = hardware(session, vm, req, where, receipts)
    if operation == "apply" and not stored:
        api.config_change(session, vm, settings={DISKS: canonical(receipts).decode()}, revision=revision)
        stored, changed = api.options(vm.config).get(DISKS), True
    if operation == "apply" and not proved:
        hw.set_boot_order(session, vm, receipts[0]["key"])
        proved, changed = hardware(session, vm, req, where, receipts), True
    return changed, dict(evidence(), **live, answered=True, owned=True, postcondition=bool(proved and stored),
                         power=vm.runtime.powerState, disks=receipts, vmHome=home, stagingDatastoreRef=staging._moId)


def port_pin(args):
    def decoded(name):
        return base64.b64decode(args[name], validate=True).decode("utf-8")
    pin = json.loads(decoded("pinned_serial_base64"))
    pin["vcenterUUID"] = decoded("pinned_uuid_base64")
    return pin


def change_power(session, pin, verb, force):
    api.require(verb in ("on", "off", "restart") and isinstance(force, bool), "invalid vSphere power request")
    vm = session.pinned_vm(pin)
    session.guard(vm)
    if api.power(vm) == "On" and verb in ("off", "restart"):
        if force:
            session.wait(vm.PowerOffVM_Task(), seconds=180)
        else:
            vm.ShutdownGuest()
            deadline = time.monotonic() + 120
            while api.power(vm) == "On" and time.monotonic() < deadline:
                time.sleep(2)
            api.require(api.power(vm) == "Off", "guest shutdown did not finish; stop the Machine explicitly with force")
    vm = session.pinned_vm(pin)
    session.guard(vm)
    if api.power(vm) == "Off" and verb in ("on", "restart"):
        session.wait(vm.PowerOnVM_Task(), seconds=180)
    state = api.power(session.pinned_vm(pin))
    api.require(state == ("Off" if verb == "off" else "On"), "the VM did not reach the requested power state")
    return dict(power=state, power_state=state.lower())


def state(session, vm, args):
    result = dict(power=api.power(vm), power_state=api.power(vm).lower(), media="", media_state="unknown")
    if not args.get("media", True):
        return result
    path, backing = media.attachment(session, vm)
    raw = api.options(vm.config).get(api.STAGE)
    receipt = json.loads(raw) if raw else {}
    if not path:
        if not raw:
            return dict(result, media_state="none")
        own = receipt.get("identity") == args.get("media_identity") and receipt.get("owner") == api.options(vm.config).get(api.OWNER)
        return dict(result, media="staged" if own else "foreign", media_state="own" if own else "foreign")
    same = (receipt.get("identity") == args.get("media_identity") and receipt.get("vm") == vm.config.instanceUuid
            and backing and backing._moId == receipt.get("datastore") and path == media.datastore_path(backing, receipt["path"]))
    cd = media.drive(session, vm)
    same = (same and receipt.get("phase") == "published" and receipt.get("owner") == api.options(vm.config).get(api.OWNER)
            and cd.connectable.startConnected and (result["power"] == "Off" or cd.connectable.connected))
    return dict(result, media=receipt["identity"] if same else "foreign", media_state="own" if same else "foreign")


def port(session, operation, args):
    pin = port_pin(args)
    vm = session.pinned_vm(pin)
    api.require(vm is not None, "the Machine is absent", "identity-mismatch")

    def validate_hardware(config):
        hw.port_disks(session.vim, config, pin)
        if args.get("prove_macs"):
            live = [d.macAddress for d in config.hardware.device if isinstance(d, session.vim.vm.device.VirtualEthernetCard)]
            api.require(sorted(live) == sorted(args["prove_macs"]), "the frozen interface set changed", "identity-mismatch")
    session.check_hardware = validate_hardware
    session.guard(vm)
    if operation == "state_read":
        return state(session, vm, args)
    if operation.startswith("seed_"):
        return media.seed(session, vm, args, operation)
    if operation == "pre_boot":
        api.off(vm)
        devices = vm.config.hardware.device
        types = session.vim.vm.device
        api.require(not args.get("firmware") and not args.get("root_disk") and not args.get("root_volume"),
                    "physical drive or firmware declarations are invalid on this VM")
        expected = args.get("expected", [])
        api.require(expected and sorted(expected) == sorted(d.macAddress for d in devices if isinstance(d, types.VirtualEthernetCard)),
                    "live VM interfaces differ from the installation target", "identity-mismatch")
        tpm = vm.config.keyId and any(isinstance(d, types.VirtualTPM) and not getattr(d, "readOnly", False) for d in devices)
        api.require(not args.get("require_tpm") or tpm, "the VM does not present a persistent vTPM", "tpm-absent")
        return dict(power="Off", power_state="off")
    if operation == "power":
        return change_power(session, pin, args["power"], args.get("force", False))
    if operation == "boot_disk":
        api.require(not media.attachment(session, vm)[0] and not api.options(vm.config).get(api.STAGE),
                    "the disk boot requires the CD-ROM and staged ISO to be released")
        return change_power(session, pin, "on", False) if args.get("power_on") else {}
    if operation == "boot_media":
        return boot_media(session, vm, pin, args)
    volume = json.loads(args["media_volume"])
    api.require(pin["owner"].startswith(volume["owner"] + ":") and vm.name == volume["name"], "the staging request identifies a different VM")
    _dc, staging = media.resolve(session, volume)
    api.require(staging._moId == pin["stagingDatastoreRef"], "the staging datastore identity changed", "identity-mismatch")
    if operation == "media_insert":
        media.insert(session, vm, args, volume)
    elif operation == "media_eject":
        api.require((media.checked_seed(vm) or media.seed_state(vm.config, "") == "none"), "a foreign guestinfo seed is present")
        record = media.read_record(vm, volume)
        api.require(not record or record["identity"] == args.get("media_identity"), "another installation owns the staged ISO")
        media.eject(session, vm, volume)
        return state(session, vm, args)
    else:
        raise api.Refused("unsupported vSphere machine port operation")
    return {}


def boot_media(session, vm, pin, args):
    identity = args.get("media_identity")
    reading = state(session, vm, args)
    api.require(identity and reading["media_state"] == "own" and reading["media"] == identity and media.attachment(session, vm)[0],
                "the requested owned ISO is not connected")
    if api.power(vm) == "On":
        change_power(session, pin, "off", True)
    config = session.guard(vm)
    api.off(vm)
    api.require(state(session, vm, args)["media"] == identity, "the requested ISO changed before boot")
    root = next(r for r in pin["disks"] if r["name"] == "root")
    boot = hw.order_options(session.vim, root["key"], media_first=True)
    api.config_change(session, vm, settings={"bootwright.bootISO": identity}, revision=config.changeVersion, boot=boot)
    return change_power(session, pin, "on", False)
