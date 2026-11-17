package substrate

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	rootWWN = "0x5000c500a1b2c3d4"
	dataWWN = "0x5000c500a1b2c3e5"
)

func armCatalog() api.Catalog {
	return api.NewCatalog([]api.Object{
		obj(api.InfraProvider, "lab", m("libvirt", m())),
		obj(api.InfraProvider, "cluster", m("kubevirt", m())),
		obj(api.InfraProvider, "vc", m("vsphere", m())),
		obj(api.InfraProvider, "floor", m("baremetal", m())),
	})
}

func machineWith(hardware api.Value) api.Object {
	return obj(api.Machine, "node", m("hardware", hardware))
}

func TestMachineArmFollowsItsProvider(t *testing.T) {
	catalog := armCatalog()
	for provider, want := range map[string]Arm{
		"lab": ArmLibvirt, "cluster": ArmKubeVirt, "vc": ArmVSphere, "floor": ArmBaremetal, "absent": "",
	} {
		t.Run(provider, func(t *testing.T) {
			if got := MachineArm(catalog, machineWith(m("providerRef", provider))); got != want {
				t.Fatalf("arm = %q, want %q", got, want)
			}
		})
	}
	physical := machineWith(m("providerRef", "lab", "bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1")))
	if got := MachineArm(catalog, physical); got != ArmLibvirt {
		t.Fatalf("a declared provider was overridden by declared hardware: %q", got)
	}
	unresolved := machineWith(m("providerRef", "absent", "bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1")))
	if got := MachineArm(catalog, unresolved); got != "" {
		t.Fatalf("an unresolved provider gave arm %q", got)
	}
}

func TestMachineArmWithNoProviderIsBaremetalOnlyForPhysicalHardware(t *testing.T) {
	catalog := armCatalog()
	for name, test := range map[string]struct {
		hardware api.Value
		want     Arm
	}{
		"nothing":                 {m(), ""},
		"an empty NIC":            {m("nics", list(m("name", "eno1"))), ""},
		"a NIC segment":           {m("nics", list(m("name", "eno1", "segmentRef", "net"))), ""},
		"root disk hints only":    {m("rootDisk", m("hints", m("deviceName", "/dev/sda", "model", "x"))), ""},
		"a root disk, no fields":  {m("rootDisk", m()), ""},
		"a root retain":           {m("rootDisk", m("onRemoval", "retain")), ""},
		"a bmc":                   {m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1")), ArmBaremetal},
		"firmware":                {m("firmware", m("secureBoot", true)), ArmBaremetal},
		"a NIC macAddress":        {m("nics", list(m("name", "eno1", "macAddress", "aa:bb:cc:dd:ee:01"))), ArmBaremetal},
		"a root wwn":              {m("rootDisk", m("wwn", rootWWN)), ArmBaremetal},
		"a root serialNumber":     {m("rootDisk", m("serialNumber", "S1")), ArmBaremetal},
		"a root sizeBytes":        {m("rootDisk", m("sizeBytes", api.IntegerValue("480103981056"))), ArmBaremetal},
		"a root volumeRef":        {m("rootDisk", m("volumeRef", m("storageId", "s", "volumeId", "v"))), ArmBaremetal},
		"a root bootVolume":       {m("rootDisk", m("bootVolume", m())), ArmBaremetal},
		"a root discover":         {m("rootDisk", m("discover", m())), ArmBaremetal},
		"data disks":              {m("dataDisks", list(m("serialNumber", "D1", "sizeBytes", api.IntegerValue("4096"), "onInstall", "metadataClear"))), ArmBaremetal},
		"hints beside a identity": {m("rootDisk", m("wwn", rootWWN, "hints", m("model", "x"))), ArmBaremetal},
	} {
		t.Run(name, func(t *testing.T) {
			if got := MachineArm(catalog, machineWith(test.hardware)); got != test.want {
				t.Fatalf("arm = %q, want %q", got, test.want)
			}
		})
	}
	if got := MachineArm(catalog, obj(api.Machine, "node", m("os", m("provided", m())))); got != "" {
		t.Fatalf("a Machine with no hardware has arm %q", got)
	}
}

func TestRootDevicePathIsDerivedOnVirtualProvidersAndDeclaredOnPhysicalOnes(t *testing.T) {
	catalog := armCatalog()
	for name, test := range map[string]struct {
		hardware api.Value
		want     string
	}{
		"libvirt":                   {m("providerRef", "lab", "profileRef", "small"), "/dev/vda"},
		"kubevirt":                  {m("providerRef", "cluster", "profileRef", "small"), "/dev/vda"},
		"vsphere declares none":     {m("providerRef", "vc", "profileRef", "small"), ""},
		"a bare-metal wwn":          {m("providerRef", "floor", "rootDisk", m("wwn", rootWWN)), "/dev/disk/by-id/wwn-" + rootWWN},
		"a bare-metal 128-bit wwn":  {m("providerRef", "floor", "rootDisk", m("wwn", "0x5000c500a1b2c3d45000c500a1b2c3d4")), "/dev/disk/by-id/wwn-0x5000c500a1b2c3d45000c500a1b2c3d4"},
		"a bare-metal deviceName":   {m("providerRef", "floor", "rootDisk", m("hints", m("deviceName", "/dev/sda"))), "/dev/sda"},
		"a deviceName beside a wwn": {m("providerRef", "floor", "rootDisk", m("wwn", rootWWN, "hints", m("deviceName", "/dev/sda"))), "/dev/sda"},
		"a bare-metal serial only":  {m("providerRef", "floor", "rootDisk", m("serialNumber", "S1", "sizeBytes", api.IntegerValue("4096"))), ""},
		"a malformed wwn":           {m("providerRef", "floor", "rootDisk", m("wwn", "0xABC")), ""},
		"no provider, a wwn":        {m("rootDisk", m("wwn", rootWWN)), "/dev/disk/by-id/wwn-" + rootWWN},
	} {
		t.Run(name, func(t *testing.T) {
			if got := RootDevicePath(catalog, machineWith(test.hardware)); got != test.want {
				t.Fatalf("root device = %q, want %q", got, test.want)
			}
		})
	}
}

func diskValue(serial, size string, fields ...any) api.Value {
	return m(append([]any{"serialNumber", serial, "sizeBytes", api.IntegerValue(size)}, fields...)...)
}

func disksMachine() api.Object {
	return machineWith(m("providerRef", "floor",
		"rootDisk", diskValue("ROOT1", "480103981056", "wwn", rootWWN, "onRemoval", "logicalOverwrite"),
		"dataDisks", list(
			diskValue("DATA1", "1000204886016", "wwn", dataWWN, "onRemoval", "metadataClear"),
			diskValue("DATA2", "8192", "onInstall", "metadataClear"),
			diskValue("DATA3", "4096", "onRemoval", "retain"),
			diskValue("DATA4", "12288", "onInstall", "metadataClear", "onRemoval", "logicalOverwrite"),
			diskValue("DATA5", "16384", "wwn", "0x5000c500a1b2c3f6", "onInstall", "metadataClear", "onRemoval", "retain"),
		)))
}

func TestEraseTargetsAreDerivedFromTheDisksTheMachineDeclares(t *testing.T) {
	want := []DiskTarget{
		{DeviceName: "/dev/disk/by-id/wwn-" + rootWWN, WWN: rootWWN, SerialNumber: "ROOT1", SizeBytes: 480103981056, Method: "logicalOverwrite"},
		{DeviceName: "/dev/disk/by-id/wwn-" + dataWWN, WWN: dataWWN, SerialNumber: "DATA1", SizeBytes: 1000204886016, Method: "metadataClear"},
		{SerialNumber: "DATA4", SizeBytes: 12288, Method: "logicalOverwrite"},
	}
	if got := EraseTargets(disksMachine()); !slices.Equal(got, want) {
		t.Fatalf("erase targets = %+v, want %+v", got, want)
	}
	retainedRoot := machineWith(m("providerRef", "floor", "rootDisk", diskValue("ROOT1", "4096", "wwn", rootWWN, "onRemoval", "retain"),
		"dataDisks", list(diskValue("DATA1", "4096", "onInstall", "metadataClear"))))
	if got := EraseTargets(retainedRoot); got != nil {
		t.Fatalf("a retained root and a clear-only data disk erase %+v", got)
	}
	dataOnly := machineWith(m("providerRef", "floor", "rootDisk", diskValue("ROOT1", "4096"),
		"dataDisks", list(diskValue("DATA1", "4096", "onRemoval", "metadataClear"))))
	if got := EraseTargets(dataOnly); len(got) != 1 || got[0].SerialNumber != "DATA1" || got[0].DeviceName != "" || got[0].Method != "metadataClear" {
		t.Fatalf("data-only erase targets = %+v", got)
	}
	if got := EraseTargets(machineWith(m())); got != nil {
		t.Fatalf("a Machine with no disks erases %+v", got)
	}
	if RootEraseSelected(machineWith(m("rootDisk", m("onRemoval", "retain")))) || RootEraseSelected(machineWith(m())) {
		t.Fatal("a root that is not erased was selected")
	}
	if !RootEraseSelected(disksMachine()) {
		t.Fatal("an erased root was not selected")
	}
}

func TestClearTargetsAreTheDataDisksClearedOnInstall(t *testing.T) {
	want := []DiskTarget{
		{SerialNumber: "DATA2", SizeBytes: 8192},
		{SerialNumber: "DATA4", SizeBytes: 12288},
		{DeviceName: "/dev/disk/by-id/wwn-0x5000c500a1b2c3f6", WWN: "0x5000c500a1b2c3f6", SerialNumber: "DATA5", SizeBytes: 16384},
	}
	if got := ClearTargets(disksMachine()); !slices.Equal(got, want) {
		t.Fatalf("clear targets = %+v, want %+v", got, want)
	}
	rootOnly := machineWith(m("providerRef", "floor", "rootDisk", diskValue("ROOT1", "4096", "onRemoval", "metadataClear")))
	if got := ClearTargets(rootOnly); got != nil {
		t.Fatalf("a root disk is cleared on install: %+v", got)
	}
	if got := ClearTargets(machineWith(m())); got != nil {
		t.Fatalf("a Machine with no disks clears %+v", got)
	}
}

func TestSerialEraseTargetReadsTheDeclaredDisks(t *testing.T) {
	machine := disksMachine()
	for name, test := range map[string]struct {
		serial   string
		size     int64
		byDevice bool
		found    bool
	}{
		"the root":                {"ROOT1", 480103981056, true, true},
		"a data disk with a wwn":  {"DATA1", 1000204886016, true, true},
		"a data disk without one": {"DATA4", 12288, false, true},
		"a clear-only data disk":  {"DATA2", 0, false, false},
		"a retained data disk":    {"DATA3", 0, false, false},
		"an undeclared serial":    {"OTHER", 0, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			size, byDevice, found := SerialEraseTarget(machine, test.serial)
			if size != test.size || byDevice != test.byDevice || found != test.found {
				t.Fatalf("SerialEraseTarget(%q) = %d, %v, %v; want %d, %v, %v", test.serial, size, byDevice, found, test.size, test.byDevice, test.found)
			}
		})
	}
}

func TestRemovalActionDefaultsToRetain(t *testing.T) {
	for action, want := range map[string]string{"": "retain", "retain": "retain", "metadataClear": "metadataClear", "logicalOverwrite": "logicalOverwrite"} {
		disk := m()
		if action != "" {
			disk = disk.With("onRemoval", api.StringValue(action))
		}
		if got := RemovalAction(disk); got != want || Erases(disk) != (want != "retain") {
			t.Errorf("RemovalAction(%q) = %q, erases = %v", action, got, Erases(disk))
		}
	}
	if !ClearsOnInstall(m("onInstall", "metadataClear")) || ClearsOnInstall(m("onInstall", "retain")) || ClearsOnInstall(m()) {
		t.Error("ClearsOnInstall misread onInstall")
	}
}

func TestEachRootDeviceHintNamesTheFieldThatDeclaresIt(t *testing.T) {
	for name, want := range map[string]string{
		"serialNumber": "spec.hardware.rootDisk.serialNumber", "sizeBytes": "spec.hardware.rootDisk.sizeBytes", "wwn": "spec.hardware.rootDisk.wwn",
		"volumeRef": "spec.hardware.rootDisk.volumeRef", "bootVolume": "spec.hardware.rootDisk.bootVolume", "discover": "spec.hardware.rootDisk.discover",
		"onRemoval": "spec.hardware.rootDisk.onRemoval",
		"hctl":      "spec.hardware.rootDisk.hints.hctl", "deviceName": "spec.hardware.rootDisk.hints.deviceName", "minSizeGigabytes": "spec.hardware.rootDisk.hints.minSizeGigabytes",
		"model": "spec.hardware.rootDisk.hints.model", "vendor": "spec.hardware.rootDisk.hints.vendor", "rotational": "spec.hardware.rootDisk.hints.rotational",
	} {
		if got := RootDeviceHintPath(name); got != want {
			t.Errorf("RootDeviceHintPath(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDataDiskPathNamesTheEntryAndItsField(t *testing.T) {
	for _, test := range []struct {
		index int
		field string
		want  string
	}{
		{0, "", "spec.hardware.dataDisks[0]"}, {3, "serialNumber", "spec.hardware.dataDisks[3].serialNumber"}, {14, "onRemoval", "spec.hardware.dataDisks[14].onRemoval"},
	} {
		if got := DataDiskPath(test.index, test.field); got != test.want {
			t.Errorf("DataDiskPath(%d, %q) = %q, want %q", test.index, test.field, got, test.want)
		}
	}
}
