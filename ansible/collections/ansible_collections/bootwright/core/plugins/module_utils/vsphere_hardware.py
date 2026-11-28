"""Construct the powered-off machine configuration and read its VMDK receipts."""

from __future__ import annotations

import json
import re

from ansible_collections.bootwright.core.plugins.module_utils import vsphere_session as api
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical


def device_change(vim, device, operation="add", file_operation=None):
    spec = vim.vm.device.VirtualDeviceSpec(device=device, operation=operation)
    if file_operation:
        spec.fileOperation = file_operation
    return spec


def connected(vim, enabled=True):
    return vim.vm.device.VirtualDevice.ConnectInfo(startConnected=enabled, connected=enabled, allowGuestControl=False)


def network(session, req, interface, dc, cluster):
    vim = session.vim
    group = session.lookup(interface["portgroup"], vim.Network)
    api.require(group in dc.network and group in cluster.network, "the selected portgroup is outside the placement")
    tags = interface.get("taggedVLANs", [])
    if isinstance(group, vim.dvs.DistributedVirtualPortgroup):
        switch = group.config.distributedVirtualSwitch
        api.require(switch.config.maxMtu and switch.config.maxMtu >= interface["mtu"], "distributed switch MTU is below the selected segment MTU")
        if tags:
            vlan = group.config.defaultPortConfig.vlan
            if not vlan or vlan.inherited:
                vlan = switch.config.defaultPortConfig.vlan
            api.require(isinstance(vlan, vim.dvs.VmwareDistributedVirtualSwitch.TrunkVlanSpec) and not vlan.inherited
                        and not group.config.policy.vlanOverrideAllowed
                        and all(any(r.start <= tag <= r.end for r in vlan.vlanId) for tag in tags),
                        "tagged VLANs require a verified distributed portgroup trunk covering every declared VLAN")
        port = vim.dvs.PortConnection(switchUuid=switch.uuid, portgroupKey=group.key)
        return vim.vm.device.VirtualEthernetCard.DistributedVirtualPortBackingInfo(port=port)
    api.require(not tags and isinstance(group, vim.Network) and not isinstance(group, vim.OpaqueNetwork), "this network cannot prove the declared VLAN trunk")
    api.require(cluster.host, "the compute cluster has no hosts for portgroup MTU proof")
    for host in cluster.host:
        config = host.config.network
        groups = [p for p in config.portgroup if p.spec.name == group.name]
        api.require(len(groups) == 1, "the standard portgroup is missing or ambiguous on a cluster host")
        switches = [v for v in config.vswitch if v.name == groups[0].spec.vswitchName]
        api.require(len(switches) == 1 and switches[0].mtu >= interface["mtu"], "standard switch MTU is below the selected segment MTU")
    return vim.vm.device.VirtualEthernetCard.NetworkBackingInfo(network=group, deviceName=group.name)


def disk_layout(req):
    sizes = [("root", req["diskGiB"])] + [(d["name"], d["sizeGiB"]) for d in req.get("dataDisks", [])]
    api.require(len(sizes) <= 60 and len({name for name, _size in sizes}) == len(sizes),
                "SCSI disk names must be distinct and fit four controllers")
    result = []
    for i, (name, size) in enumerate(sizes):
        bus, unit = divmod(i, 15)
        result.append((name, size, bus, unit if unit < 7 else unit + 1))
    return result


def disks(vim, config):
    devices = config.hardware.device
    api.require(devices, "vCenter did not return a complete hardware device list")
    controllers = {d.key: d.busNumber for d in devices if isinstance(d, vim.vm.device.VirtualSCSIController)}
    return [((controllers.get(d.controllerKey), d.unitNumber), d) for d in devices if isinstance(d, vim.vm.device.VirtualDisk)]


def flat(vim, disk):
    backing = disk.backing
    return (isinstance(backing, vim.vm.device.VirtualDisk.FlatVer2BackingInfo) and not backing.parent
            and backing.diskMode == "persistent" and backing.sharing in (None, "sharingNone"))


def disk_receipt(vim, name, disk):
    api.require(flat(vim, disk) and disk.key > 0 and disk.backing.datastore and disk.backing.fileName and disk.backing.uuid,
                "vCenter did not prove a private full VMDK with a stable backing identity")
    return {"name": name, "key": disk.key, "backing": disk.backing.fileName,
            "datastore": disk.backing.datastore._moId, "uuid": disk.backing.uuid}


def observed_disks(vim, vm, req, store, config=None):
    actual = dict(disks(vim, config or vm.config))
    result = []
    for name, size, bus, unit in disk_layout(req):
        disk = actual.get((bus, unit))
        api.require(disk and disk.capacityInKB == size * 1024 * 1024 and disk.backing.datastore == store,
                    "a disk differs from its selected VMDK capacity or datastore")
        result.append(disk_receipt(vim, name, disk))
    api.require(len({r["uuid"] for r in result}) == len(result), "vCenter returned duplicate VMDK identities")
    return result


def new_scsi_disks(vim, req, store, root=None, root_controller=None):
    device = vim.vm.device
    changes = []
    for i, (name, size, bus, unit) in enumerate(disk_layout(req)):
        key = root_controller.key if root_controller and bus == 0 else -100 - bus
        if unit == 0 and (bus or not root_controller):
            scsi = device.ParaVirtualSCSIController(key=key, busNumber=bus, sharedBus="noSharing")
            changes.append(device_change(vim, scsi))
        if root and i == 0:
            api.require(root.capacityInKB <= size * 1024 * 1024, "the template root exceeds the requested root capacity")
            if root.capacityInKB < size * 1024 * 1024:
                root.capacityInKB = size * 1024 * 1024
                root.capacityInBytes = size << 30
                changes.append(device_change(vim, root, "edit"))
            continue
        backing = device.VirtualDisk.FlatVer2BackingInfo(fileName="[%s]" % store.name, diskMode="persistent", thinProvisioned=True)
        disk = device.VirtualDisk(key=-1000 - i, controllerKey=key, unitNumber=unit, capacityInKB=size * 1024 * 1024, backing=backing)
        changes.append(device_change(vim, disk, file_operation="create"))
    return changes


def template_devices(vim, template):
    config = template.config
    api.require(config and config.template and template.runtime.powerState == "poweredOff" and not template.snapshot,
                "the full clone source must be a stopped template without snapshots")
    options = api.options(config)
    api.require(not any(v for k, v in options.items() if k.startswith(("bootwright.", "guestinfo."))),
                "the source template contains inherited guestinfo or ownership material")
    source = disks(vim, config)
    api.require(len(source) == 1 and source[0][0] == (0, 0) and flat(vim, source[0][1]),
                "the template requires exactly one standalone SCSI root disk on bus zero, unit zero")
    root = source[0][1]
    devs = config.hardware.device
    controller = next(d for d in devs if d.key == root.controllerKey)
    api.require(sum(isinstance(d, vim.vm.device.VirtualSCSIController) for d in devs) == 1,
                "the source template must have one SCSI controller")
    return devs, root, controller


def cdrom(vim, devices):
    types = vim.vm.device
    changes = []
    cd = [d for d in devices if isinstance(d, types.VirtualCdrom)]
    api.require(len(cd) <= 1, "the template has more than one CD-ROM")
    if cd:
        drive = cd[0]
    else:
        # Every new x86 VM has the two native IDE controllers, keys 200 and 201.
        drive = types.VirtualCdrom(key=-500, controllerKey=200, unitNumber=0)
    drive.backing = types.VirtualCdrom.RemotePassthroughBackingInfo(deviceName="", useAutoDetect=False)
    drive.connectable = connected(vim, False)
    changes.append(device_change(vim, drive, "edit" if cd else "add"))
    return changes


def tpm(session, spec, cluster, devices, requested):
    vim = session.vim
    found = [d for d in devices if isinstance(d, vim.vm.device.VirtualTPM)]
    api.require(len(found) <= 1 and (not found or requested), "the template vTPM conflicts with the selected machine profile")
    if not requested:
        return
    manager = session.content.cryptoManager
    provider = manager.GetDefaultKmsCluster(entity=cluster, defaultsToParent=True) if manager else None
    api.require(provider and provider.id, "a persistent vTPM requires a default vCenter key provider for this cluster")
    if not found:
        # vCenter allocates the key in the VM provisioning operation. A
        # standalone GenerateKey call would leave an unreceipted key on loss.
        key = vim.encryption.CryptoKeyId(keyId="", providerId=provider)
        spec.crypto = vim.encryption.CryptoSpecEncrypt(cryptoKeyId=key)
        spec.deviceChange.append(device_change(vim, vim.vm.device.VirtualTPM(key=-800)))


def template_receipt(vim, template, disk):
    receipt = disk_receipt(vim, "root", disk)
    api.require(template._moId and template.config.instanceUuid and disk.backing.contentId,
                "the stopped template must publish a stable VM identity and root backing content revision")
    return dict(receipt, moRef=template._moId, instanceUUID=template.config.instanceUuid, contentId=disk.backing.contentId)


def device_spec(session, req, where, devices, root=None, controller=None):
    vim = session.vim
    dc, cluster, _pool, _folder, store = where
    changes = new_scsi_disks(vim, req, store, root, controller) + cdrom(vim, devices)
    changes.extend(device_change(vim, d, "remove") for d in devices if isinstance(d, vim.vm.device.VirtualEthernetCard))
    for i, iface in enumerate(req["interfaces"]):
        backing = network(session, req, iface, dc, cluster)
        mac = iface["macAddress"]
        api.require(re.fullmatch(r"00:50:56:[0-3][0-9a-f]:[0-9a-f]{2}:[0-9a-f]{2}", mac), "invalid manual VMware MAC address")
        nic = vim.vm.device.VirtualVmxnet3(key=-2000 - i, backing=backing, connectable=connected(vim), addressType="manual", macAddress=mac)
        changes.append(device_change(vim, nic))
    return changes


def provision(session, req, mark):
    vim = session.vim
    where = api.placement(session, req)
    dc, cluster, pool, folder, store = where
    template = session.lookup(req["template"], vim.VirtualMachine) if req.get("template") else None
    ext = {api.OWNER: mark, api.OPERATION: req["identity"]["block"], "disk.EnableUUID": "TRUE"}
    spec = vim.vm.ConfigSpec(numCPUs=req["cpu"], memoryMB=req["memoryMiB"], firmware="efi",
                             bootOptions=vim.vm.BootOptions(efiSecureBootEnabled=False))
    if template:
        devices, source_root, _controller = template_devices(vim, template)
        api.require(source_root.capacityInKB <= req["diskGiB"] * 1024 * 1024, "the template root exceeds the selected capacity")
        ext["bootwright.templateRoot"] = canonical(template_receipt(vim, template, source_root)).decode()
        # Qualify every NIC and the selected key provider before allocation.
        for iface in req["interfaces"]:
            network(session, req, iface, dc, cluster)
        tpm(session, spec, cluster, devices, req.get("tpm", False))
        spec.extraConfig = [vim.option.OptionValue(key=k, value=v) for k, v in ext.items()]
        location = vim.vm.RelocateSpec(datastore=store, pool=pool, diskMoveType="moveAllDiskBackingsAndDisallowSharing")
        clone = vim.vm.CloneSpec(location=location, config=spec, powerOn=False, template=False, tpmProvisionPolicy="replace")
        return session.wait(template.CloneVM_Task(folder=folder, name=req["name"], spec=clone))
    spec.deviceChange = device_spec(session, req, where, [])
    tpm(session, spec, cluster, [], req.get("tpm", False))
    spec.extraConfig = [vim.option.OptionValue(key=k, value=v) for k, v in ext.items()]
    spec.name, spec.guestId = req["name"], "rhel9_64Guest"
    spec.files = vim.vm.FileInfo(vmPathName="[%s]" % store.name)
    return session.wait(folder.CreateVM_Task(config=spec, pool=pool))


def clone_devices(session, req, vm, where, operation, prove_initial=None):
    config = vm.config
    opts = api.options(config)
    if opts.get("bootwright.cloneRoot"):
        return True
    api.off(vm)
    if prove_initial:
        prove_initial()
    source = json.loads(opts["bootwright.templateRoot"])
    live = disks(session.vim, config)
    api.require(len(live) == 1 and live[0][0] == (0, 0), "the full clone has unexpected disks before customization")
    root = live[0][1]
    actual = disk_receipt(session.vim, "root", root)
    home = config.files.vmPathName.rsplit("/", 1)[0] + "/"
    api.require(root.backing.datastore == where[-1] and actual["backing"].startswith(home)
                and (source["datastore"], source["backing"]) != (actual["datastore"], actual["backing"])
                and source["uuid"] != actual["uuid"], "the full clone root is not a distinct independent VMDK")
    template = session.lookup(req["template"], session.vim.VirtualMachine)
    _devices, template_root, _controller = template_devices(session.vim, template)
    api.require(source == template_receipt(session.vim, template, template_root) and root.backing.contentId == source["contentId"],
                "the stopped template or independently cloned root no longer proves its original content revision")
    if operation == "observe":
        return actual
    controller = next(d for d in config.hardware.device if d.key == root.controllerKey)
    changes = device_spec(session, req, where, config.hardware.device, root, controller)
    api.config_change(session, vm, changes, settings={"bootwright.cloneRoot": canonical(actual).decode()}, revision=config.changeVersion)
    return True


def boot_order(vim, config, key):
    order = config.bootOptions.bootOrder if config.bootOptions else []
    return (len(order) == 2 and isinstance(order[0], vim.vm.BootOptions.BootableDiskDevice)
            and order[0].deviceKey == key and isinstance(order[1], vim.vm.BootOptions.BootableCdromDevice))


def order_options(vim, key, media_first=False):
    boot = vim.vm.BootOptions
    devices = [boot.BootableDiskDevice(deviceKey=key), boot.BootableCdromDevice()]
    return boot(efiSecureBootEnabled=False, bootOrder=list(reversed(devices)) if media_first else devices)


def authorized_order(vim, config, root_key):
    options = api.options(config)
    if not options.get("bootwright.bootISO"):
        return boot_order(vim, config, root_key)
    receipt = json.loads(options.get(api.STAGE) or "{}")
    if receipt.get("phase") != "published" or receipt.get("identity") != options["bootwright.bootISO"]:
        return False
    cd = [d for d in config.hardware.device if isinstance(d, vim.vm.device.VirtualCdrom)]
    if (len(cd) != 1 or not isinstance(cd[0].backing, vim.vm.device.VirtualCdrom.IsoBackingInfo)
            or not cd[0].connectable.startConnected or not cd[0].backing.datastore):
        return False
    backing = cd[0].backing
    order = config.bootOptions.bootOrder
    return (backing.datastore._moId == receipt["datastore"] and backing.fileName == "[%s] %s" % (backing.datastore.name, receipt["path"])
            and len(order) == 2 and isinstance(order[0], vim.vm.BootOptions.BootableCdromDevice)
            and isinstance(order[1], vim.vm.BootOptions.BootableDiskDevice) and order[1].deviceKey == root_key)


def set_boot_order(session, vm, key):
    session.guard(vm)
    api.off(vm)
    opts = order_options(session.vim, key)
    session.wait(vm.ReconfigVM_Task(spec=session.vim.vm.ConfigSpec(changeVersion=vm.config.changeVersion, bootOptions=opts)))


def port_disks(vim, config, pin):
    records = pin.get("disks")
    api.require(isinstance(records, list) and records and len(records) <= 60 and api.options(config).get("bootwright.disks") == canonical(records).decode(),
                "the Machine port has no matching original VMDK receipts", "identity-mismatch")
    api.require(config.firmware == "efi" and config.bootOptions and config.bootOptions.efiSecureBootEnabled is False
                and api.options(config).get("disk.EnableUUID", "").upper() == "TRUE",
                "the recorded EFI or disk UUID presentation changed", "identity-mismatch")
    current = {disk.key: (slot, disk) for slot, disk in disks(vim, config)}
    api.require(len(current) == len(records), "an unexpected or missing VMDK prevents use of the Machine port", "identity-mismatch")
    for item in records:
        entry = current.get(item["key"])
        api.require(entry and disk_receipt(vim, item["name"], entry[1]) == item,
                    "a VMDK differs from its original backing identity", "identity-mismatch")
    root = next((d for d in records if d["name"] == "root"), None)
    api.require(root and current[root["key"]][0] == (0, 0) and authorized_order(vim, config, root["key"]),
                "the original root disk is no longer first in the Machine boot order", "identity-mismatch")
