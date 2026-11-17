package substrate

import (
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	HardwarePath   = "spec.hardware"
	ProviderPath   = "spec.hardware.providerRef"
	ProfilePath    = "spec.hardware.profileRef"
	BMCPath        = "spec.hardware.bmc"
	NICsPath       = "spec.hardware.nics"
	RootDiskPath   = "spec.hardware.rootDisk"
	DataDisksPath  = "spec.hardware.dataDisks"
	VirtualRootDev = "/dev/vda"
)

func MachineProviderRef(machine api.Object) string { return api.MachineProviderRef(machine) }

func MachineArm(catalog api.Catalog, machine api.Object) Arm {
	if reference := MachineProviderRef(machine); reference != "" {
		if provider, found := catalog.Find(api.InfraProvider, reference); found {
			return Variant(provider)
		}
		return ""
	}
	if DeclaresPhysicalHardware(machine) {
		return ArmBaremetal
	}
	return ""
}

func DeclaresPhysicalHardware(machine api.Object) bool {
	hardware := machine.Spec().Get("hardware")
	if hardware.Has("bmc") || hardware.Has("firmware") || hardware.Has("dataDisks") {
		return true
	}
	for _, nic := range hardware.Get("nics").Items() {
		if nic.Has("macAddress") {
			return true
		}
	}
	for _, key := range []string{"wwn", "serialNumber", "sizeBytes", "volumeRef", "bootVolume", "discover"} {
		if hardware.Has("rootDisk", key) {
			return true
		}
	}
	return false
}

func MachineProfileRef(machine api.Object) string {
	return machine.Spec().Get("hardware", "profileRef").Text()
}

func MachineProfilePath(api.Object) string { return ProfilePath }

func MachineBMC(machine api.Object) api.Value {
	return machine.Spec().Get("hardware", "bmc")
}

func BMCVerified(bmc api.Value) bool { return bmc.Get("tls", "verification").Text() != "none" }

func MachineRootDisk(machine api.Object) api.Value {
	return machine.Spec().Get("hardware", "rootDisk")
}

func MachineDataDisks(machine api.Object) []api.Value {
	return machine.Spec().Get("hardware", "dataDisks").Items()
}

func MachineMAC(machine api.Object, nic string) string {
	if entry, found := machine.Spec().Get("hardware", "nics").Named("name", nic); found {
		return entry.Get("macAddress").Text()
	}
	return ""
}

func RemovalAction(disk api.Value) string {
	if action := disk.Get("onRemoval").Text(); action != "" {
		return action
	}
	return "retain"
}

func Erases(disk api.Value) bool { return RemovalAction(disk) != "retain" }

func ClearsOnInstall(disk api.Value) bool { return disk.Get("onInstall").Text() == "metadataClear" }

type DiskTarget struct {
	DeviceName   string
	WWN          string
	SerialNumber string
	SizeBytes    int64
	Method       string
}

func diskTarget(disk api.Value, method string) DiskTarget {
	size, _ := disk.Get("sizeBytes").Int64()
	wwn := disk.Get("wwn").Text()
	return DiskTarget{DeviceName: WWNDevicePath(wwn), WWN: wwn, SerialNumber: disk.Get("serialNumber").Text(), SizeBytes: size, Method: method}
}

func EraseTargets(machine api.Object) []DiskTarget {
	var targets []DiskTarget
	if root := MachineRootDisk(machine); Erases(root) {
		targets = append(targets, diskTarget(root, RemovalAction(root)))
	}
	for _, disk := range MachineDataDisks(machine) {
		if Erases(disk) {
			targets = append(targets, diskTarget(disk, RemovalAction(disk)))
		}
	}
	return targets
}

func ClearTargets(machine api.Object) []DiskTarget {
	var targets []DiskTarget
	for _, disk := range MachineDataDisks(machine) {
		if ClearsOnInstall(disk) {
			targets = append(targets, diskTarget(disk, ""))
		}
	}
	return targets
}

func RootEraseSelected(machine api.Object) bool { return Erases(MachineRootDisk(machine)) }

func RootDevicePath(catalog api.Catalog, machine api.Object) string {
	if VirtualArm(MachineArm(catalog, machine)) {
		return VirtualRootDev
	}
	return DeclaredRootDevicePath(machine)
}

func DataDiskPath(index int, field string) string {
	path := DataDisksPath + "[" + strconv.Itoa(index) + "]"
	if field != "" {
		path += "." + field
	}
	return path
}
