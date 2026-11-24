package machine

import (
	"regexp"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	MaxEraseTargets = 16
	MaxClearTargets = 15
	MaxDataDisks    = 15
)

const MaxLogicalOverwriteBytes int64 = 896 << 30

var clearDevicePath = regexp.MustCompile(`^/dev/disk/by-id/wwn-0x([0-9a-f]{16}(?:[0-9a-f]{16})?)$`)

func validateDisks(o api.Object, physical bool) []api.Issue {
	physicalInstall := installed(o) && physical
	issues := validateRootRemoval(o, physicalInstall)
	if o.Spec().Has("hardware", "dataDisks") {
		issues = append(issues, validateDataDisks(o, physicalInstall)...)
	}
	return issues
}

func validateRootRemoval(o api.Object, physicalInstall bool) []api.Issue {
	root := substrate.MachineRootDisk(o)
	if !root.Has("onRemoval") {
		return nil
	}
	path := "$." + substrate.RootDiskPath + ".onRemoval"
	if api.MachineProvided(o) {
		return []api.Issue{invariant(path, "nothing removes the disks of an OS-ready Machine, so its root disk declares no onRemoval", "remove "+path[2:]+" from "+o.Identity())}
	}
	if !substrate.Erases(root) {
		return nil
	}
	if !physicalInstall {
		return []api.Issue{invariant(path, "erasing the root disk on removal requires a Bootwright-installed bare-metal Machine", "remove "+path[2:]+" to retain the root disk")}
	}
	if refusal, refused := eraseTrustRefusal(o, path); refused {
		return []api.Issue{refusal}
	}
	if !root.Has("serialNumber") || !root.Has("sizeBytes") {
		return []api.Issue{invariant(path, "erasing the root disk on removal names it by its serialNumber and sizeBytes, and "+o.Identity()+" declares "+missingIdentity(root),
			"declare "+substrate.RootDiskPath+".serialNumber and sizeBytes, the root disk's serial number and exact capacity, or set onRemoval to retain")}
	}
	for _, selector := range []string{"volumeRef", "bootVolume", "discover"} {
		if root.Has(selector) {
			return []api.Issue{invariant(path, "a root disk selected by "+selector+" has no declared identity to erase it by",
				"remove "+substrate.RootDiskPath+"."+selector+" and name the root by its wwn or by serialNumber and sizeBytes, or set onRemoval to retain")}
		}
	}
	return overwriteBound(root, "$."+substrate.RootDiskPath+".sizeBytes")
}

func missingIdentity(root api.Value) string {
	switch {
	case !root.Has("serialNumber") && !root.Has("sizeBytes"):
		return "neither"
	case !root.Has("serialNumber"):
		return "no serialNumber"
	}
	return "no sizeBytes"
}

func overwriteBound(disk api.Value, path string) []api.Issue {
	size, _ := disk.Get("sizeBytes").Int64()
	if substrate.RemovalAction(disk) != "logicalOverwrite" || size <= MaxLogicalOverwriteBytes {
		return nil
	}
	return []api.Issue{invariant(path, "a logicalOverwrite disk is at most 896 GiB, so its write and readback fit the four-hour erase deadline", "set onRemoval to metadataClear for this disk, or erase a disk of at most 896 GiB")}
}

func eraseTrustRefusal(o api.Object, path string) (api.Issue, bool) {
	bmc := substrate.MachineBMC(o)
	trust := bmc.Get("virtualMedia", "tls", "trust").Text()
	if substrate.BMCVerified(bmc) && (trust == "established" || trust == "importCertificate" || trust == "disableVerification") {
		return api.Issue{}, false
	}
	return invariant(path, "physical erase requires verified BMC TLS",
		"address "+substrate.BMCPath+" at its https:// endpoint with tls.verification absent, full or certificate on "+o.Identity()), true
}

func validateDataDisks(o api.Object, physicalInstall bool) []api.Issue {
	path := "$." + substrate.DataDisksPath
	if !physicalInstall {
		return []api.Issue{invariant(path, "data disks are declared only on a Bootwright-installed bare-metal Machine, the one whose installation clears and whose removal erases them", "remove "+path[2:]+" from "+o.Identity())}
	}
	disks := substrate.MachineDataDisks(o)
	issues := []api.Issue{}
	if len(disks) < 1 || len(disks) > MaxDataDisks {
		issues = append(issues, invariant(path, "a Machine declares 1 to 15 data disks", "declare each data disk Bootwright clears or erases once, and never the root disk"))
	}
	root := substrate.MachineRootDisk(o)
	rootSerial, rootWWN := root.Get("serialNumber").Text(), clearWWN(root.Get("wwn").Text())
	if hex := clearDevicePath.FindStringSubmatch(root.Get("hints", "deviceName").Text()); hex != nil {
		rootWWN = hex[1]
	}
	serials, wwns := map[string]bool{}, map[string]bool{}
	trustChecked := false
	for index, disk := range disks {
		field := func(name string) string { return "$." + substrate.DataDiskPath(index, name) }
		serial, wwn := disk.Get("serialNumber").Text(), disk.Get("wwn").Text()
		if !diskselection.ValidSerial(serial) {
			issues = append(issues, invariant(field("serialNumber"), "a data disk's serial number must be 1 to 256 ASCII letters, digits and ._:+/=- characters", "set serialNumber to the disk's serial number as its sysfs serial attribute or VPD page 0x80 reports it"))
		} else if serials[serial] {
			issues = append(issues, invariant(field("serialNumber"), "data disk serial numbers must be distinct", "list each data disk once"))
		}
		serials[serial] = true
		if disk.Has("wwn") && !substrate.ValidRootWWN(wwn) {
			issues = append(issues, invariant(field("wwn"), "a data disk's wwn is 0x followed by 16 or 32 lowercase hexadecimal digits", "set wwn to the disk's WWN in that form, as its /dev/disk/by-id/wwn-0x link names it"))
		} else if disk.Has("wwn") && wwns[clearWWN(wwn)] {
			issues = append(issues, invariant(field("wwn"), "data disk WWNs must be distinct", "list each data disk once"))
		}
		if disk.Has("wwn") {
			wwns[clearWWN(wwn)] = true
		}
		if size, ok := disk.Get("sizeBytes").Int64(); !ok || !diskselection.ValidSize(size) {
			issues = append(issues, invariant(field("sizeBytes"), "a data disk's sizeBytes is its exact capacity, aligned to 4096 bytes and at most 64 TiB", "set sizeBytes to the disk's exact logical capacity, as blockdev --getsize64 reports it"))
		}
		if serial != "" && serial == rootSerial || wwn != "" && rootWWN != "" && clearWWN(wwn) == rootWWN {
			issues = append(issues, invariant(field(""), "a data disk must not be the root disk, by serialNumber or wwn", "remove the root disk from "+path[2:]+"; "+substrate.RootDiskPath+" declares it and its onRemoval"))
		}
		if !substrate.ClearsOnInstall(disk) && !substrate.Erases(disk) {
			issues = append(issues, invariant(field(""), "data disk "+serial+" of "+o.Identity()+" declares no action: its onInstall and onRemoval both retain it",
				"set onInstall: metadataClear or an onRemoval other than retain on the entry, or remove it; a disk the Machine does not declare is retained"))
		}
		if !substrate.Erases(disk) {
			continue
		}
		if refusal, refused := eraseTrustRefusal(o, field("onRemoval")); refused && !trustChecked {
			issues = append(issues, refusal)
		}
		trustChecked = true
		issues = append(issues, overwriteBound(disk, field("sizeBytes"))...)
	}
	return issues
}

func RootEraseSelected(o api.Object) bool { return substrate.RootEraseSelected(o) }

func clearWWN(declared string) string {
	return strings.TrimPrefix(strings.ToLower(declared), "0x")
}
